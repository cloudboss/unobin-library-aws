package ec2

import (
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prefixTestClient(t *testing.T, f *fakeEC2) *managedPrefixListClient {
	t.Helper()
	c, err := newManagedPrefixListClient(t.Context(), f.configuration())
	require.NoError(t, err)
	clock := &prefixWaitClock{}
	c.waiter = managedPrefixListWaiter{clock.now, clock.sleep, 15 * time.Minute}
	return c
}

func prefixItem(id, state string, version int) string {
	return fmt.Sprintf("<item><prefixListId>%s</prefixListId><state>%s</state>"+
		"<stateMessage>remote message</stateMessage><version>%d</version>"+
		"<prefixListName>remote</prefixListName><maxEntries>8</maxEntries>"+
		"<addressFamily>IPv6</addressFamily><ownerId>owner</ownerId>"+
		"<prefixListArn>api-arn</prefixListArn><tagSet>"+
		"<item><key>aws:reserved</key><value>x</value></item>"+
		"<item><key>blank</key><value></value></item></tagSet></item>", id, state, version)
}

func prefixPage(items, next string) (int, string) {
	return 200, "<DescribeManagedPrefixListsResponse><prefixListSet>" + items +
		"</prefixListSet>" + next + "</DescribeManagedPrefixListsResponse>"
}

func prefixEntries(entries, next string) (int, string) {
	return 200, "<GetManagedPrefixListEntriesResponse><entrySet>" + entries +
		"</entrySet>" + next + "</GetManagedPrefixListEntriesResponse>"
}

func TestManagedPrefixListReadPagesAndClassifications(t *testing.T) {
	f := newFakeEC2(t)
	f.on("DescribeManagedPrefixLists", func(n int, in url.Values) (int, string) {
		assert.Empty(t, in.Get("MaxResults"))
		assert.Equal(t, "pl-one", in.Get("PrefixListId.1"))
		if n == 1 {
			return prefixPage("", "<nextToken>metadata</nextToken>")
		}
		assert.Equal(t, "metadata", in.Get("NextToken"))
		return prefixPage(prefixItem("pl-one", "modify-complete", 42), "")
	})
	f.on("GetManagedPrefixListEntries", func(n int, in url.Values) (int, string) {
		assert.Empty(t, in.Get("MaxResults"))
		assert.Empty(t, in.Get("TargetVersion"))
		assert.Equal(t, "pl-one", in.Get("PrefixListId"))
		if n == 1 {
			return prefixEntries("<item><cidr>2001:DB8::1/064</cidr></item>",
				"<nextToken>entries</nextToken>")
		}
		assert.Equal(t, "entries", in.Get("NextToken"))
		return prefixEntries("<item><cidr>10.0.0.1/8</cidr><description>api</description></item>", "")
	})
	c := prefixTestClient(t, f)
	out, err := c.read(t.Context(), "pl-one")
	require.NoError(t, err)
	assert.Equal(t, &ManagedPrefixListResourceOutput{
		PrefixListId: "pl-one", AddressFamily: "IPv6", Arn: "api-arn", MaxEntries: 8,
		Name: "remote", OwnerId: "owner", Version: 42, Region: "us-east-1",
		Tags: map[string]string{"blank": ""}, Entries: []ManagedPrefixListEntry{
			{Cidr: "10.0.0.1/8", Description: aws.String("api")}, {Cidr: "2001:DB8::1/064"},
		},
	}, out)
	for _, items := range []string{"", prefixItem("wrong", "create-complete", 1),
		prefixItem("pl-one", "delete-complete", 1),
		prefixItem("pl-one", "create-complete", 1) + prefixItem("pl-one", "create-complete", 1)} {
		f.on("DescribeManagedPrefixLists", func(int, url.Values) (int, string) {
			return prefixPage(items, "")
		})
		_, err := c.read(t.Context(), "pl-one")
		require.ErrorIs(t, err, runtime.ErrNotFound)
	}
	f.on("DescribeManagedPrefixLists", func(int, url.Values) (int, string) {
		return prefixPage(prefixItem("pl-one", "create-complete", 1), "")
	})
	f.on("GetManagedPrefixListEntries", func(int, url.Values) (int, string) {
		return prefixAPIError("InvalidPrefixListID.NotFound")
	})
	_, err = c.read(t.Context(), "pl-one")
	require.Error(t, err)
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
}

func TestManagedPrefixListCreateCleanupAndToken(t *testing.T) {
	for _, failure := range []string{"", "wait", "describe", "entries", "cleanup", "malformed"} {
		t.Run(failure, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("CreateManagedPrefixList", func(int, url.Values) (int, string) {
				if failure == "malformed" {
					return 200, "<CreateManagedPrefixListResponse/>"
				}
				return 200, "<CreateManagedPrefixListResponse><prefixList><prefixListId>pl-one" +
					"</prefixListId></prefixList></CreateManagedPrefixListResponse>"
			})
			deleted := false
			f.on("DescribeManagedPrefixLists", func(n int, _ url.Values) (int, string) {
				if deleted {
					return prefixPage("", "")
				}
				if failure == "describe" && n > 1 {
					return prefixAPIError("DescribeFailure")
				}
				if failure == "wait" {
					return prefixPage(prefixItem("pl-one", "create-failed", 1), "")
				}
				return prefixPage(prefixItem("pl-one", "create-complete", 1), "")
			})
			f.on("GetManagedPrefixListEntries", func(int, url.Values) (int, string) {
				if failure == "entries" || failure == "cleanup" {
					return prefixAPIError("EntryFailure")
				}
				return prefixEntries("", "")
			})
			f.on("DeleteManagedPrefixList", func(int, url.Values) (int, string) {
				if failure == "cleanup" {
					return prefixAPIError("CleanupFailure")
				}
				deleted = true
				return 200, "<DeleteManagedPrefixListResponse/>"
			})
			c := prefixTestClient(t, f)
			r := &ManagedPrefixListResource{AddressFamily: "IPv4", MaxEntries: 1, Name: "name"}
			out, err := r.create(t.Context(), c, r.prepareRequest("token"))
			if failure == "" {
				require.NoError(t, err)
				assert.Equal(t, "pl-one", out.PrefixListId)
			} else {
				require.Error(t, err)
				assert.Nil(t, out)
				assert.Equal(t, failure != "malformed" && failure != "cleanup", deleted)
				if failure == "cleanup" {
					require.ErrorContains(t, err, "EntryFailure")
					require.ErrorContains(t, err, "CleanupFailure")
				}
			}
		})
	}
}

func TestManagedPrefixListUpdateSequence(t *testing.T) {
	for _, mode := range []string{"growth", "narrow", "shrink", "overlap", "clear", "unmanaged",
		"name", "tags", "version", "fallback"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("ModifyManagedPrefixList", func(int, url.Values) (int, string) {
				return 200, "<ModifyManagedPrefixListResponse/>"
			})
			f.on("DeleteTags", func(int, url.Values) (int, string) { return 200, "<DeleteTagsResponse/>" })
			f.on("CreateTags", func(int, url.Values) (int, string) { return 200, "<CreateTagsResponse/>" })
			f.on("DescribeManagedPrefixLists", func(n int, _ url.Values) (int, string) {
				return prefixPage(prefixItem("pl-one", "modify-complete", 49+n), "")
			})
			f.on("GetManagedPrefixListEntries", func(int, url.Values) (int, string) {
				return prefixEntries("", "")
			})
			known := &ManagedPrefixListResourceOutput{PrefixListId: "pl-one", Name: "desired",
				MaxEntries: 8, Version: 42, Tags: map[string]string{},
				Entries: []ManagedPrefixListEntry{{Cidr: "10.0.0.0/8", Description: aws.String("old")}}}
			r := &ManagedPrefixListResource{Name: "desired", MaxEntries: 8,
				Entries: &[]ManagedPrefixListEntry{{Cidr: "192.0.2.1/24"}}}
			switch mode {
			case "growth":
				r.MaxEntries = 16
			case "narrow":
				r.MaxEntries = 2147483648
			case "shrink":
				r.MaxEntries = 1
			case "overlap":
				r.Entries = &[]ManagedPrefixListEntry{{Cidr: "10.0.0.0/8"}}
			case "clear":
				r.Entries = &[]ManagedPrefixListEntry{}
			case "unmanaged", "name", "tags", "version", "fallback":
				r.Entries = nil
			}
			if mode == "name" {
				known.Name = "drift"
			}
			if mode == "tags" {
				known.Tags = map[string]string{"old": "gone"}
				r.Tags = &map[string]string{"blank": ""}
			}
			prior := managedPrefixListPrior{Outputs: &ManagedPrefixListResourceOutput{
				PrefixListId: "pl-one"}, Observed: known}
			if mode == "fallback" {
				prior.Outputs, prior.Observed = known, nil
			}
			_, err := r.update(t.Context(), prefixTestClient(t, f), prior)
			require.NoError(t, err)
			base := url.Values{"Action": {"ModifyManagedPrefixList"},
				"Version": {"2016-11-15"}, "PrefixListId": {"pl-one"}}
			entry := maps.Clone(base)
			entry.Set("PrefixListName", "desired")
			entry.Set("CurrentVersion", "42")
			entry.Set("AddEntry.1.Cidr", "192.0.2.1/24")
			entry.Set("RemoveEntry.1.Cidr", "10.0.0.0/8")
			capacity := maps.Clone(base)
			capacity.Set("MaxEntries", fmt.Sprint(int32(r.MaxEntries)))
			var expected []url.Values
			switch mode {
			case "growth", "narrow":
				expected = []url.Values{capacity, entry}
			case "shrink":
				expected = []url.Values{entry, capacity}
			case "overlap":
				remove := maps.Clone(base)
				remove.Set("CurrentVersion", "42")
				remove.Set("RemoveEntry.1.Cidr", "10.0.0.0/8")
				entry.Set("CurrentVersion", "50")
				entry.Set("AddEntry.1.Cidr", "10.0.0.0/8")
				entry.Del("RemoveEntry.1.Cidr")
				expected = []url.Values{remove, entry}
			case "clear":
				entry.Del("AddEntry.1.Cidr")
				expected = []url.Values{entry}
			case "name":
				entry = maps.Clone(base)
				entry.Set("PrefixListName", "desired")
				expected = []url.Values{entry}
			}
			assert.Equal(t, expected, f.sent("ModifyManagedPrefixList"))
			if mode == "tags" {
				assert.Equal(t, []string{"DeleteTags", "CreateTags",
					"DescribeManagedPrefixLists", "GetManagedPrefixListEntries"}, peeringActions(f))
			}
		})
	}
}

func TestManagedPrefixListDeleteAndMissingIdentity(t *testing.T) {
	r := &ManagedPrefixListResource{}
	require.NoError(t, r.Delete(t.Context(), nil, managedPrefixListPrior{}))
	_, err := r.Read(t.Context(), nil, managedPrefixListPrior{})
	require.ErrorIs(t, err, runtime.ErrNotFound)
	_, err = r.Update(t.Context(), nil, managedPrefixListPrior{})
	require.ErrorContains(t, err, "saved")
	for _, failure := range []string{"", "InvalidPrefixListID.NotFound", "UnauthorizedOperation"} {
		f := newFakeEC2(t)
		f.on("DeleteManagedPrefixList", func(int, url.Values) (int, string) {
			if failure != "" {
				return prefixAPIError(failure)
			}
			return http.StatusOK, "<DeleteManagedPrefixListResponse/>"
		})
		f.on("DescribeManagedPrefixLists", func(int, url.Values) (int, string) {
			return prefixPage("", "")
		})
		err := prefixTestClient(t, f).delete(context.Background(), "pl-one")
		if failure == "UnauthorizedOperation" {
			require.ErrorContains(t, err, failure)
		} else {
			require.NoError(t, err)
		}
	}
}

func prefixAPIError(code string) (int, string) { return 400, ec2ErrorXML(code, "injected") }

func TestManagedPrefixListDeleteRequiresAbsence(t *testing.T) {
	tests := []struct {
		name    string
		items   []string
		wantErr string
		sleeps  []time.Duration
	}{
		{name: "omitted state", items: []string{
			"<item><prefixListId>pl-one</prefixListId></item>"},
			wantErr: `wait for managed prefix list pl-one delete: unexpected state ""; expected ""`},
		{name: "empty state", items: []string{prefixItem("pl-one", "", 1)},
			wantErr: `wait for managed prefix list pl-one delete: unexpected state ""; expected ""`},
		{name: "first absence", items: []string{""}},
		{name: "pending then absence", items: []string{
			prefixItem("pl-one", "delete-in-progress", 1), ""},
			sleeps: []time.Duration{200 * time.Millisecond}},
		{name: "deleted", items: []string{prefixItem("pl-one", "delete-complete", 1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DeleteManagedPrefixList", func(int, url.Values) (int, string) {
				return 200, "<DeleteManagedPrefixListResponse/>"
			})
			f.on("DescribeManagedPrefixLists", func(n int, _ url.Values) (int, string) {
				if !assert.LessOrEqual(t, n, len(tt.items)) {
					return prefixAPIError("UnexpectedDescribe")
				}
				return prefixPage(tt.items[n-1], "")
			})
			clock := &prefixWaitClock{}
			c := prefixTestClient(t, f)
			c.waiter = managedPrefixListWaiter{clock.now, clock.sleep, 15 * time.Minute}
			err := c.delete(t.Context(), "pl-one")
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
			actions := []string{"DeleteManagedPrefixList"}
			requests := make([]url.Values, 0, len(tt.items))
			for range tt.items {
				actions = append(actions, "DescribeManagedPrefixLists")
				requests = append(requests, url.Values{"Action": {"DescribeManagedPrefixLists"},
					"Version": {"2016-11-15"}, "PrefixListId.1": {"pl-one"}})
			}
			assert.Equal(t, actions, peeringActions(f))
			assert.Equal(t, requests, f.sent("DescribeManagedPrefixLists"))
			assert.Equal(t, []url.Values{{"Action": {"DeleteManagedPrefixList"},
				"Version": {"2016-11-15"}, "PrefixListId": {"pl-one"}}},
				f.sent("DeleteManagedPrefixList"))
			assert.Equal(t, tt.sleeps, clock.sleeps)
		})
	}
}

func TestManagedPrefixListCreatePreservesPresentEmptyStateCleanupFailure(t *testing.T) {
	f := newFakeEC2(t)
	f.on("CreateManagedPrefixList", func(int, url.Values) (int, string) {
		return 200, "<CreateManagedPrefixListResponse><prefixList><prefixListId>pl-one" +
			"</prefixListId></prefixList></CreateManagedPrefixListResponse>"
	})
	f.on("DescribeManagedPrefixLists", func(n int, _ url.Values) (int, string) {
		if n == 1 {
			return prefixPage(prefixItem("pl-one", "create-failed", 1), "")
		}
		return prefixPage("<item><prefixListId>pl-one</prefixListId></item>", "")
	})
	f.on("DeleteManagedPrefixList", func(int, url.Values) (int, string) {
		return 200, "<DeleteManagedPrefixListResponse/>"
	})
	clock := &prefixWaitClock{}
	c := prefixTestClient(t, f)
	c.waiter = managedPrefixListWaiter{clock.now, clock.sleep, 15 * time.Minute}
	r := &ManagedPrefixListResource{AddressFamily: "IPv4", MaxEntries: 1, Name: "name"}
	out, err := r.create(t.Context(), c, r.prepareRequest("token"))
	assert.Nil(t, out)
	assert.ErrorContains(t, err, `wait for managed prefix list pl-one create: unexpected state`)
	assert.ErrorContains(t, err, `"create-failed"; expected "create-complete": remote message`)
	assert.ErrorContains(t, err, "compensating delete managed prefix list pl-one: "+
		`wait for managed prefix list pl-one delete: unexpected state ""; expected ""`)
	assert.Equal(t, []string{"CreateManagedPrefixList", "DescribeManagedPrefixLists",
		"DeleteManagedPrefixList", "DescribeManagedPrefixLists"}, peeringActions(f))
	assert.Empty(t, clock.sleeps)
}

func TestManagedPrefixListUpdateStopsAfterFailure(t *testing.T) {
	for _, stage := range []string{"tags", "growth API", "growth wait", "overlap API", "overlap wait",
		"final API", "final wait", "shrink API", "shrink wait"} {
		t.Run(stage, func(t *testing.T) {
			f := newFakeEC2(t)
			var actions []string
			f.on("DeleteTags", func(int, url.Values) (int, string) {
				actions = append(actions, "tags")
				if stage == "tags" {
					return prefixAPIError("TagFailure")
				}
				return 200, "<DeleteTagsResponse/>"
			})
			modify := 0
			f.on("ModifyManagedPrefixList", func(_ int, form url.Values) (int, string) {
				modify++
				operation := "final"
				if form.Has("MaxEntries") {
					operation = "shrink"
					if form.Get("MaxEntries") == "16" {
						operation = "growth"
					}
				} else if !form.Has("PrefixListName") {
					operation = "overlap"
				}
				actions = append(actions, operation+" API")
				if stage == operation+" API" {
					return prefixAPIError("InvalidPrefixListVersion")
				}
				return 200, "<ModifyManagedPrefixListResponse/>"
			})
			f.on("DescribeManagedPrefixLists", func(int, url.Values) (int, string) {
				operation := strings.TrimSuffix(actions[len(actions)-1], " API")
				actions = append(actions, operation+" wait")
				if stage == operation+" wait" {
					return prefixPage(prefixItem("pl-one", "modify-failed", 52), "")
				}
				return prefixPage(prefixItem("pl-one", "modify-complete", 52), "")
			})
			known := &ManagedPrefixListResourceOutput{PrefixListId: "pl-one", Name: "old", MaxEntries: 8,
				Version: 42, Tags: map[string]string{"old": "remove"},
				Entries: []ManagedPrefixListEntry{{Cidr: "10.0.0.0/8", Description: aws.String("old")}}}
			r := &ManagedPrefixListResource{Name: "desired", MaxEntries: 1,
				Entries: &[]ManagedPrefixListEntry{{Cidr: "10.0.0.0/8"}}}
			if strings.HasPrefix(stage, "growth") {
				r.MaxEntries = 16
			}
			_, err := r.update(t.Context(), prefixTestClient(t, f), managedPrefixListPrior{Outputs: known})
			require.Error(t, err)
			assert.Equal(t, stage, actions[len(actions)-1])
			if strings.HasSuffix(stage, "wait") {
				require.ErrorContains(t, err, "remote message")
			}
			assert.Empty(t, f.sent("DeleteManagedPrefixList"))
			assert.Empty(t, f.sent("GetManagedPrefixListEntries"))
		})
	}
}

func TestManagedPrefixListMultipleOverlapAndLargeUnbatchedDifference(t *testing.T) {
	f := newFakeEC2(t)
	f.on("ModifyManagedPrefixList", func(int, url.Values) (int, string) {
		return 200, "<ModifyManagedPrefixListResponse/>"
	})
	f.on("DescribeManagedPrefixLists", func(int, url.Values) (int, string) {
		return prefixPage(prefixItem("pl-one", "modify-complete", 99), "")
	})
	f.on("GetManagedPrefixListEntries", func(int, url.Values) (int, string) {
		return prefixEntries("", "")
	})
	entries := []ManagedPrefixListEntry{{Cidr: "10.0.0.0/8", Description: aws.String("new")},
		{Cidr: "192.0.2.0/24"}}
	current := []ManagedPrefixListEntry{{Cidr: "10.0.0.0/8", Description: aws.String("old")},
		{Cidr: "192.0.2.0/24", Description: aws.String("old")}, {Cidr: "198.51.100.0/24"}}
	for n := range 150 {
		entries = append(entries, ManagedPrefixListEntry{
			Cidr: fmt.Sprintf("172.16.%d.1/24", n)})
	}
	r := &ManagedPrefixListResource{Name: "desired", MaxEntries: 8, Entries: &entries}
	_, err := r.update(t.Context(), prefixTestClient(t, f), managedPrefixListPrior{
		Outputs: &ManagedPrefixListResourceOutput{PrefixListId: "pl-one", Name: "desired", MaxEntries: 8,
			Version: 42, Entries: current}})
	require.NoError(t, err)
	requests := f.sent("ModifyManagedPrefixList")
	expected := []url.Values{{"Action": {"ModifyManagedPrefixList"}, "Version": {"2016-11-15"},
		"PrefixListId": {"pl-one"}, "CurrentVersion": {"42"},
		"RemoveEntry.1.Cidr": {"10.0.0.0/8"}, "RemoveEntry.2.Cidr": {"192.0.2.0/24"}},
		{"Action": {"ModifyManagedPrefixList"}, "Version": {"2016-11-15"}, "PrefixListId": {"pl-one"},
			"CurrentVersion": {"99"}, "PrefixListName": {"desired"},
			"RemoveEntry.1.Cidr": {"198.51.100.0/24"}}}
	for i, entry := range managedPrefixListEntries(entries) {
		expected[1].Set(fmt.Sprintf("AddEntry.%d.Cidr", i+1), entry.Cidr)
		if entry.Description != nil {
			expected[1].Set(fmt.Sprintf("AddEntry.%d.Description", i+1),
				*entry.Description)
		}
	}
	assert.Equal(t, expected, requests)
}

func TestManagedPrefixListFreshTokenRetainedAcrossSDKRetry(t *testing.T) {
	f := newFakeEC2(t)
	f.on("CreateManagedPrefixList", func(n int, _ url.Values) (int, string) {
		if n == 1 {
			return 503, ec2ErrorXML("ServiceUnavailable", "retry")
		}
		return 200, "<CreateManagedPrefixListResponse><prefixList><prefixListId>pl-one" +
			"</prefixListId></prefixList></CreateManagedPrefixListResponse>"
	})
	f.on("DescribeManagedPrefixLists", func(int, url.Values) (int, string) {
		return prefixPage(prefixItem("pl-one", "create-complete", 1), "")
	})
	f.on("GetManagedPrefixListEntries", func(int, url.Values) (int, string) {
		return prefixEntries("", "")
	})
	cfg := f.configuration()
	c := prefixTestClient(t, f)
	options := c.ec2.Options()
	options.Retryer = retry.NewStandard(func(o *retry.StandardOptions) {
		o.MaxAttempts = 2
		o.Backoff = retry.BackoffDelayerFunc(func(int, error) (time.Duration, error) { return 0, nil })
	})
	c.ec2 = ec2.New(options)
	r := &ManagedPrefixListResource{AddressFamily: "IPv4", Name: "name", MaxEntries: 1}
	first := r.prepareRequest(rand.Text())
	_, err := r.create(t.Context(), c, first)
	require.NoError(t, err)
	_, err = r.Create(t.Context(), cfg)
	require.NoError(t, err)
	requests := f.sent("CreateManagedPrefixList")
	require.Equal(t, []string{"CreateManagedPrefixList", "CreateManagedPrefixList",
		"DescribeManagedPrefixLists", "DescribeManagedPrefixLists", "GetManagedPrefixListEntries",
		"CreateManagedPrefixList", "DescribeManagedPrefixLists", "DescribeManagedPrefixLists",
		"GetManagedPrefixListEntries"}, peeringActions(f))
	assert.NotEmpty(t, requests[0].Get("ClientToken"))
	assert.Equal(t, requests[0], requests[1])
	assert.NotEqual(t, requests[0].Get("ClientToken"), requests[2].Get("ClientToken"))
	expected := url.Values{"Action": {"CreateManagedPrefixList"}, "Version": {"2016-11-15"},
		"AddressFamily": {"IPv4"}, "MaxEntries": {"1"}, "PrefixListName": {"name"},
		"ClientToken": {*first.ClientToken}}
	assert.Equal(t, expected, requests[0])
}

func TestManagedPrefixListEntryPageErrorsAndWaitFailureMessages(t *testing.T) {
	for _, operation := range []string{"create", "modify", "delete"} {
		f := newFakeEC2(t)
		f.on("DescribeManagedPrefixLists", func(int, url.Values) (int, string) {
			return prefixPage(prefixItem("pl-one", operation+"-failed", 42), "")
		})
		_, err := prefixTestClient(t, f).wait(t.Context(), "pl-one", operation)
		require.ErrorContains(t, err, "pl-one "+operation)
		require.ErrorContains(t, err, operation+"-failed")
		require.ErrorContains(t, err, "remote message")
	}
	f := newFakeEC2(t)
	f.on("DescribeManagedPrefixLists", func(int, url.Values) (int, string) {
		return prefixPage(prefixItem("pl-one", "create-complete", 1), "")
	})
	f.on("GetManagedPrefixListEntries", func(n int, _ url.Values) (int, string) {
		if n == 1 {
			return prefixEntries("<item><cidr>10.0.0.0/8</cidr></item>",
				"<nextToken>next</nextToken>")
		}
		return prefixAPIError("PageFailure")
	})
	out, err := prefixTestClient(t, f).read(t.Context(), "pl-one")
	require.ErrorContains(t, err, "PageFailure")
	assert.Nil(t, out)
	requests := f.sent("GetManagedPrefixListEntries")
	assert.Equal(t, []url.Values{{"Action": {"GetManagedPrefixListEntries"}, "Version": {"2016-11-15"},
		"PrefixListId": {"pl-one"}}, {"Action": {"GetManagedPrefixListEntries"},
		"Version": {"2016-11-15"}, "PrefixListId": {"pl-one"}, "NextToken": {"next"}}}, requests)
}

func TestManagedPrefixListSavedEffectiveRegionAndConfiguration(t *testing.T) {
	old, desired := newFakeEC2(t), newFakeEC2(t)
	old.on("DescribeManagedPrefixLists", func(int, url.Values) (int, string) {
		return prefixPage(prefixItem("pl-one", "modify-complete", 42), "")
	})
	old.on("GetManagedPrefixListEntries", func(int, url.Values) (int, string) {
		return prefixEntries("", "")
	})
	old.on("DeleteTags", func(int, url.Values) (int, string) { return 200, "<DeleteTagsResponse/>" })
	old.on("DeleteManagedPrefixList", func(int, url.Values) (int, string) {
		return prefixAPIError("InvalidPrefixListID.NotFound")
	})
	oldCfg, desiredCfg := old.configuration(), desired.configuration()
	oldCfg.Region = nil
	t.Setenv("AWS_REGION", "us-west-2")
	r := &ManagedPrefixListResource{Name: "remote", MaxEntries: 8}
	prior := managedPrefixListPrior{Configuration: oldCfg, Outputs: &ManagedPrefixListResourceOutput{
		PrefixListId: "pl-one", Region: "us-east-1", Name: "remote", MaxEntries: 8}}
	output, err := r.Read(t.Context(), desiredCfg, prior)
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", output.Region)
	output, err = r.Update(t.Context(), desiredCfg, prior)
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", output.Region)
	require.NoError(t, r.Delete(t.Context(), desiredCfg, prior))
	assert.Nil(t, oldCfg.Region)
	assert.Empty(t, peeringActions(desired))
	assert.Equal(t, []string{"DescribeManagedPrefixLists", "GetManagedPrefixListEntries",
		"DescribeManagedPrefixLists", "GetManagedPrefixListEntries", "DeleteManagedPrefixList"},
		peeringActions(old))
}

func TestManagedPrefixListRemoteNameErrorPreservesAPIIdentity(t *testing.T) {
	f := newFakeEC2(t)
	f.on("CreateManagedPrefixList", func(int, url.Values) (int, string) {
		return prefixAPIError("InvalidPrefixListName")
	})
	r := &ManagedPrefixListResource{AddressFamily: "IPv4", MaxEntries: 1,
		Name: "aws:remote-restriction"}
	out, err := r.create(t.Context(), prefixTestClient(t, f), r.prepareRequest("token"))
	require.ErrorContains(t, err, "InvalidPrefixListName")
	assert.True(t, isNotFound(err, "InvalidPrefixListName"))
	assert.Nil(t, out)
	assert.Equal(t, []string{"CreateManagedPrefixList"}, peeringActions(f))
}
