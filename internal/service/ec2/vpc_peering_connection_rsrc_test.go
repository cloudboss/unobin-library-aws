package ec2

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeeringCreateRequest(t *testing.T) {
	testTags := map[string]string{"stage": "", "aws:reserved": "ignored"}
	for _, tt := range []struct {
		name  string
		input VpcPeeringConnectionResource
		want  ec2.CreateVpcPeeringConnectionInput
	}{
		{
			name:  "omitted",
			input: VpcPeeringConnectionResource{VpcId: "vpc-a", PeerVpcId: "vpc-b"},
			want: ec2.CreateVpcPeeringConnectionInput{
				VpcId: aws.String("vpc-a"), PeerVpcId: aws.String("vpc-b"),
			},
		},
		{
			name: "empty peer fields and required IDs",
			input: VpcPeeringConnectionResource{
				PeerOwnerId: aws.String(""), PeerRegion: aws.String(""),
			},
			want: ec2.CreateVpcPeeringConnectionInput{
				VpcId: aws.String(""), PeerVpcId: aws.String(""),
			},
		},
		{
			name: "explicit peer fields and managed tags",
			input: VpcPeeringConnectionResource{
				VpcId: "vpc-a", PeerVpcId: "vpc-b", PeerOwnerId: aws.String("123456789012"),
				PeerRegion: aws.String("us-west-2"), Tags: &testTags,
			},
			want: ec2.CreateVpcPeeringConnectionInput{
				VpcId: aws.String("vpc-a"), PeerVpcId: aws.String("vpc-b"),
				PeerOwnerId: aws.String("123456789012"), PeerRegion: aws.String("us-west-2"),
				TagSpecifications: []ec2types.TagSpecification{{
					ResourceType: ec2types.ResourceTypeVpcPeeringConnection,
					Tags:         []ec2types.Tag{{Key: aws.String("stage"), Value: aws.String("")}},
				}},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, &tt.want, tt.input.createRequest())
		})
	}
}

const (
	testPeeringID        = "pcx-managed"
	testPeeringRequester = "vpc-requester"
	testPeeringAccepter  = "vpc-accepter"
	testPeeringAccount   = "123456789012"
	testPeeringActive    = "active"
)

func peeringResult(id, status, requester, accepter, tags string) string {
	return fmt.Sprintf(
		"<item><vpcPeeringConnectionId>%s</vpcPeeringConnectionId>%s%s%s<status>"+
			"<code>%s</code><message>status detail</message></status></item>",
		id, requester, accepter, tags, status,
	)
}

func peeringSide(side, id, account, region, options string) string {
	return fmt.Sprintf(
		"<%sVpcInfo><vpcId>%s</vpcId><ownerId>%s</ownerId><region>%s</region>%s</%sVpcInfo>",
		side, id, account, region, options, side,
	)
}

func peeringDefaultResult(id, status string) string {
	return peeringResult(id, status,
		peeringSide("requester", testPeeringRequester, testPeeringAccount, "us-east-1", ""),
		peeringSide("accepter", testPeeringAccepter, testPeeringAccount, "us-east-1", ""), "")
}

func peeringPage(items, next string) (int, string) {
	return http.StatusOK, "<DescribeVpcPeeringConnectionsResponse><vpcPeeringConnectionSet>" +
		items + "</vpcPeeringConnectionSet>" + next + "</DescribeVpcPeeringConnectionsResponse>"
}

func peeringSuccess(action string) (int, string) {
	return http.StatusOK, "<" + action + "Response><return>true</return></" + action + "Response>"
}

func peeringForm(action string, params url.Values) url.Values {
	want := url.Values{"Action": {action}, "Version": {"2016-11-15"}}
	maps.Copy(want, params)
	return want
}

func peeringActions(f *fakeEC2) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for _, form := range f.requests {
		names = append(names, form.Get("Action"))
	}
	return names
}

func peeringTestClient(t *testing.T, f *fakeEC2) *peeringClient {
	t.Helper()
	client, err := newPeeringClient(t.Context(), f.configuration())
	require.NoError(t, err)
	client.pollInterval = time.Nanosecond
	return client
}

func TestPeeringLookupAbsence(t *testing.T) {
	valid := peeringDefaultResult(testPeeringID, testPeeringActive)
	for _, tc := range []struct {
		name   string
		result string
		code   string
		base   bool
	}{
		{name: "service absence", code: peeringNotFound},
		{name: "no results"},
		{name: "multiple results", result: valid + valid},
		{name: "requester missing", result: removePeeringElement(valid, "requesterVpcInfo")},
		{name: "accepter missing", result: removePeeringElement(valid, "accepterVpcInfo")},
		{name: "status missing", result: removePeeringElement(valid, "status")},
		{name: "wrong id", result: peeringDefaultResult("pcx-elsewhere", "active"), base: true},
		{name: "deleted", result: peeringDefaultResult(testPeeringID, "deleted"), base: true},
		{name: "expired", result: peeringDefaultResult(testPeeringID, "expired"), base: true},
		{name: "failed", result: peeringDefaultResult(testPeeringID, "failed"), base: true},
		{name: "rejected", result: peeringDefaultResult(testPeeringID, "rejected"), base: true},
		{name: "unrelated error", code: "UnauthorizedOperation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DescribeVpcPeeringConnections", func(int, url.Values) (int, string) {
				if tc.code != "" {
					return http.StatusBadRequest, ec2ErrorXML(tc.code, "response detail")
				}
				return peeringPage(tc.result, "")
			})
			c := peeringTestClient(t, f)
			_, err := c.read(t.Context(), testPeeringID)
			if tc.code == "UnauthorizedOperation" {
				require.ErrorContains(t, err, tc.code)
				require.NotErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			}
			if tc.base {
				got, err := c.lookup(t.Context(), testPeeringID, false)
				require.NoError(t, err)
				require.Contains(t, tc.result, "<code>"+string(got.Status.Code)+"</code>")
				require.Contains(t, tc.result,
					"<vpcPeeringConnectionId>"+aws.ToString(got.VpcPeeringConnectionId)+
						"</vpcPeeringConnectionId>")
			}
		})
	}
}

func removePeeringElement(xml, name string) string {
	lo := strings.Index(xml, "<"+name+">")
	end := "</" + name + ">"
	hi := strings.Index(xml, end)
	return xml[:lo] + xml[hi+len(end):]
}

func TestPeeringLookupPaginates(t *testing.T) {
	for _, firstPageHasResult := range []bool{false, true} {
		t.Run(strconv.FormatBool(firstPageHasResult), func(t *testing.T) {
			f := newFakeEC2(t)
			const action = "DescribeVpcPeeringConnections"
			item := peeringDefaultResult(testPeeringID, testPeeringActive)
			f.on(action, func(n int, _ url.Values) (int, string) {
				if n == 1 {
					first := ""
					if firstPageHasResult {
						first = item
					}
					return peeringPage(first, "<nextToken>page-two</nextToken>")
				}
				return peeringPage(item, "")
			})
			c := peeringTestClient(t, f)
			got, err := c.lookup(t.Context(), testPeeringID, true)
			if firstPageHasResult {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testPeeringID, aws.ToString(got.VpcPeeringConnectionId))
			}
			assert.Equal(t, []url.Values{
				peeringForm(action, url.Values{"VpcPeeringConnectionId.1": {testPeeringID}}),
				peeringForm(action, url.Values{
					"VpcPeeringConnectionId.1": {testPeeringID}, "NextToken": {"page-two"},
				}),
			}, f.sent(action))
		})
	}
}

func TestPeeringLookupPropagatesPageErrorAfterMultipleResults(t *testing.T) {
	f := newFakeEC2(t)
	const action = "DescribeVpcPeeringConnections"
	item := peeringDefaultResult(testPeeringID, testPeeringActive)
	f.on(action, func(n int, _ url.Values) (int, string) {
		if n == 1 {
			return peeringPage(item+item, "<nextToken>page-two</nextToken>")
		}
		return http.StatusBadRequest, ec2ErrorXML("UnauthorizedOperation", "access denied")
	})

	_, err := peeringTestClient(t, f).read(t.Context(), testPeeringID)
	assert.Equal(t, []url.Values{
		peeringForm(action, url.Values{"VpcPeeringConnectionId.1": {testPeeringID}}),
		peeringForm(action, url.Values{
			"VpcPeeringConnectionId.1": {testPeeringID}, "NextToken": {"page-two"},
		}),
	}, f.sent(action))
	var apiErr smithy.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "UnauthorizedOperation", apiErr.ErrorCode())
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
}

func TestPeeringWaitCreated(t *testing.T) {
	for _, tc := range []struct {
		name     string
		statuses []string
		want     string
		error    string
	}{
		{
			name: "miss preserves target and pending resets it",
			statuses: []string{"initiating-request", "active", "provisioning",
				"pending-acceptance", "", "active"},
			want: "active",
		},
		{
			name: "twenty misses, sighting, and twenty further misses",
			statuses: slices.Concat(make([]string, 20), []string{"active"},
				make([]string, 20), []string{"pending-acceptance"}),
			want: "pending-acceptance",
		},
		{name: "permanent absence", statuses: make([]string, 21), error: "21 consecutive reads"},
		{name: "unexpected terminal", statuses: []string{"failed"},
			error: `unexpected status "failed": status detail`},
		{name: "unrelated service error", statuses: []string{"error"}, error: "UnauthorizedOperation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			const action = "DescribeVpcPeeringConnections"
			f.on(action, func(n int, _ url.Values) (int, string) {
				if n > len(tc.statuses) {
					t.Errorf("unexpected lookup %d", n)
					return peeringPage(peeringDefaultResult(testPeeringID, "failed"), "")
				}
				switch status := tc.statuses[n-1]; status {
				case "":
					return peeringPage("", "")
				case "error":
					return http.StatusBadRequest, ec2ErrorXML(tc.error, "response detail")
				default:
					return peeringPage(peeringDefaultResult(testPeeringID, status), "")
				}
			})
			c := peeringTestClient(t, f)
			conn, err := c.waitCreated(t.Context(), testPeeringID)
			if tc.error != "" {
				require.ErrorContains(t, err, tc.error)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, string(conn.Status.Code))
			}
			want := peeringForm(action, url.Values{"VpcPeeringConnectionId.1": {testPeeringID}})
			expected := make([]url.Values, len(tc.statuses))
			for i := range expected {
				expected[i] = want
			}
			assert.Equal(t, expected, f.sent(action))
		})
	}
}

func TestPeeringWaitHonorsCancellationAndTimeout(t *testing.T) {
	for _, phase := range []string{"create", "delete"} {
		for _, expire := range []bool{false, true} {
			t.Run(phase+"/"+strconv.FormatBool(expire), func(t *testing.T) {
				f := newFakeEC2(t)
				c := peeringTestClient(t, f)
				c.pollInterval = time.Hour
				var ctx context.Context
				var cancel context.CancelFunc
				if expire {
					ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
				} else {
					ctx, cancel = context.WithCancel(t.Context())
				}
				defer cancel()
				action := "DescribeVpcPeeringConnections"
				f.on(action, func(int, url.Values) (int, string) {
					if !expire {
						cancel()
					}
					status := "provisioning"
					if phase == "delete" {
						status = "deleting"
					}
					return peeringPage(peeringDefaultResult(testPeeringID, status), "")
				})
				var err error
				if phase == "create" {
					_, err = c.waitCreated(ctx, testPeeringID)
				} else {
					err = c.waitDeleted(ctx, testPeeringID)
				}
				require.ErrorIs(t, err, ctx.Err())
				assert.Equal(t, []url.Values{
					peeringForm(action, url.Values{"VpcPeeringConnectionId.1": {testPeeringID}}),
				}, f.sent(action))
			})
		}
	}
}

func TestPeeringCreateLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		auto   *bool
		status string
		final  string
		accept bool
		err    string
	}{
		{name: "pending without acceptance", status: "pending-acceptance", final: "pending-acceptance"},
		{name: "pending with acceptance", auto: aws.Bool(true), status: "pending-acceptance",
			final: "active", accept: true},
		{name: "already active", auto: aws.Bool(true), status: "active", final: "active"},
		{name: "final read absent", status: "active", err: "final read"},
		{name: "base sees a mismatched ID", status: "active", err: "final read"},
		{name: "missing create ID", err: "returned no connection ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			c := peeringTestClient(t, f)
			const create = "CreateVpcPeeringConnection"
			const accept = "AcceptVpcPeeringConnection"
			const describe = "DescribeVpcPeeringConnections"
			f.on(create, func(int, url.Values) (int, string) {
				item := ""
				if tc.status != "" {
					item = "<vpcPeeringConnection><vpcPeeringConnectionId>" + testPeeringID +
						"</vpcPeeringConnectionId></vpcPeeringConnection>"
				}
				return http.StatusOK, "<" + create + "Response>" + item + "</" + create + "Response>"
			})
			f.on(accept, func(int, url.Values) (int, string) { return peeringSuccess(accept) })
			f.on(describe, func(n int, _ url.Values) (int, string) {
				if n <= 2 {
					id := testPeeringID
					if tc.name == "base sees a mismatched ID" {
						id = "pcx-other"
					}
					return peeringPage(peeringDefaultResult(id, tc.status), "")
				}
				if tc.final == "" {
					return peeringPage("", "")
				}
				return peeringPage(peeringDefaultResult(testPeeringID, tc.final), "")
			})
			r := &VpcPeeringConnectionResource{
				VpcId: testPeeringRequester, PeerVpcId: testPeeringAccepter, AutoAccept: tc.auto,
			}
			got, err := r.create(t.Context(), c)
			if tc.err == "" {
				require.NoError(t, err)
				assert.Equal(t, &VpcPeeringConnectionResourceOutput{
					VpcPeeringConnectionId: testPeeringID, VpcId: testPeeringRequester,
					PeerVpcId: testPeeringAccepter, PeerOwnerId: testPeeringAccount,
					PeerRegion: "us-east-1", AcceptStatus: tc.final, Region: "us-east-1",
					Tags: map[string]string{},
				}, got)
			} else {
				require.ErrorContains(t, err, tc.err)
				if tc.status != "" {
					require.ErrorContains(t, err, testPeeringID)
				}
			}
			assert.Equal(t, []url.Values{peeringForm(create, url.Values{
				"VpcId": {testPeeringRequester}, "PeerVpcId": {testPeeringAccepter},
			})}, f.sent(create))
			expected := []string{create}
			if tc.status != "" {
				expected = append(expected, describe, describe)
				if tc.accept {
					expected = append(expected, accept, describe, describe)
					assert.Equal(t, []url.Values{peeringForm(accept, url.Values{
						"VpcPeeringConnectionId": {testPeeringID},
					})}, f.sent(accept))
				}
				expected = append(expected, describe)
			}
			assert.Equal(t, expected, peeringActions(f))
		})
	}
}
