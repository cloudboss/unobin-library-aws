package ec2

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeeringAccepterAdoptsAndAccepts(t *testing.T) {
	for _, method := range []string{"create", "update"} {
		for _, tc := range []struct {
			name, status string
			auto         *bool
			accept       bool
		}{
			{"omitted", "pending-acceptance", nil, false},
			{"false", "pending-acceptance", aws.Bool(false), false},
			{"accept pending", "pending-acceptance", aws.Bool(true), true},
			{"active", "active", aws.Bool(true), false},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				f := newFakeEC2(t)
				f.on(describePeering, func(n int, _ url.Values) (int, string) {
					status := tc.status
					if tc.accept && n > 1 {
						status = "active"
					}
					return peeringPage(peeringDefaultResult(testPeeringID, status), "")
				})
				const accept = "AcceptVpcPeeringConnection"
				f.on(accept, func(int, url.Values) (int, string) { return peeringSuccess(accept) })
				r := &VpcPeeringConnectionAccepterResource{
					VpcPeeringConnectionId: testPeeringID, AutoAccept: tc.auto,
				}
				client := peeringTestClient(t, f)
				var got *VpcPeeringConnectionAccepterResourceOutput
				var err error
				if method == "create" {
					got, err = r.create(t.Context(), client)
				} else {
					r.VpcPeeringConnectionId = "pcx-desired-ignored"
					got, err = r.update(t.Context(), client, peeringAccepterPrior{
						Inputs:  VpcPeeringConnectionAccepterResource{AutoAccept: aws.Bool(true)},
						Outputs: &VpcPeeringConnectionAccepterResourceOutput{VpcPeeringConnectionId: testPeeringID},
					})
				}
				require.NoError(t, err)
				status := tc.status
				if tc.accept {
					status = "active"
				}
				assert.Equal(t, &VpcPeeringConnectionAccepterResourceOutput{
					VpcPeeringConnectionId: testPeeringID, VpcId: testPeeringRequester,
					PeerVpcId: testPeeringAccepter, PeerOwnerId: testPeeringAccount,
					PeerRegion: "us-east-1", Region: "us-east-1", AcceptStatus: status,
					Tags: map[string]string{},
				}, got)
				want := []string{describePeering}
				if tc.accept {
					want = append(want, accept, describePeering, describePeering)
					assert.Equal(t, []url.Values{peeringForm(accept, url.Values{
						"VpcPeeringConnectionId": {testPeeringID},
					})}, f.sent(accept))
				}
				want = append(want, describePeering)
				assert.Equal(t, want, peeringActions(f))
			})
		}
	}
}

func TestPeeringAccepterMissingAndEmptyAdoption(t *testing.T) {
	for _, id := range []string{testPeeringID, ""} {
		t.Run(id, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on(describePeering, func(int, url.Values) (int, string) { return peeringPage("", "") })
			r := &VpcPeeringConnectionAccepterResource{VpcPeeringConnectionId: id}
			_, err := r.create(t.Context(), peeringTestClient(t, f))
			require.ErrorContains(t, err, "adopt VPC peering connection "+id)
			require.ErrorIs(t, err, runtime.ErrNotFound)
			assert.Equal(t, []url.Values{peeringForm(describePeering, url.Values{
				"VpcPeeringConnectionId.1": {id},
			})}, f.sent(describePeering))
			assert.Equal(t, []string{describePeering}, peeringActions(f))
		})
	}
}

func TestPeeringAccepterDNSChanges(t *testing.T) {
	for _, tc := range []struct {
		name, status, currentAccepter, currentRequester string
		desired                                         VpcPeeringConnectionAccepterResource
		params                                          url.Values
		fail                                            bool
	}{
		{name: "both drift", currentAccepter: "false", currentRequester: "true",
			desired: VpcPeeringConnectionAccepterResource{
				Accepter: peeringDns(aws.Bool(true)), Requester: peeringDns(nil),
			}, params: url.Values{
				"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc":  {"true"},
				"RequesterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"false"},
			}},
		{name: "one drift", currentAccepter: "false", currentRequester: "true",
			desired: VpcPeeringConnectionAccepterResource{
				Accepter: peeringDns(aws.Bool(true)), Requester: peeringDns(aws.Bool(true)),
			}, params: url.Values{
				"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"true"},
			}},
		{name: "explicit false", currentAccepter: "true",
			desired: VpcPeeringConnectionAccepterResource{Accepter: peeringDns(aws.Bool(false))},
			params: url.Values{
				"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"false"},
			}},
		{name: "empty block absent description", currentAccepter: "absent",
			desired: VpcPeeringConnectionAccepterResource{Accepter: peeringDns(nil)},
			params: url.Values{
				"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"false"},
			}},
		{name: "null flag matching false", currentAccepter: "false",
			desired: VpcPeeringConnectionAccepterResource{Accepter: peeringDns(nil)}},
		{name: "removed sides", currentAccepter: "true", currentRequester: "true"},
		{name: "omitted drift", currentAccepter: "false", currentRequester: "false"},
		{name: "matching", currentAccepter: "true", currentRequester: "false",
			desired: VpcPeeringConnectionAccepterResource{
				Accepter: peeringDns(aws.Bool(true)), Requester: peeringDns(aws.Bool(false)),
			}},
		{name: "pending unmanaged", status: "pending-acceptance"},
		{name: "pending matching", status: "pending-acceptance", currentRequester: "false",
			desired: VpcPeeringConnectionAccepterResource{Requester: peeringDns(nil)}},
		{name: "pending configured", status: "pending-acceptance", fail: true,
			currentRequester: "true",
			desired:          VpcPeeringConnectionAccepterResource{Requester: peeringDns(nil)}},
		{name: "provisioning", status: "provisioning", currentRequester: "false",
			desired: VpcPeeringConnectionAccepterResource{Requester: peeringDns(aws.Bool(true))},
			params: url.Values{
				"RequesterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"true"},
			}},
	} {
		for _, method := range []string{"create", "update"} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				f := newFakeEC2(t)
				status := tc.status
				if status == "" {
					status = "active"
				}
				f.on(describePeering, func(n int, _ url.Values) (int, string) {
					result := peeringDefaultResult(testPeeringID, status)
					if n == 1 || tc.params == nil {
						result = peeringSideOptions(tc.currentAccepter, tc.currentRequester, status)
					}
					return peeringPage(result, "")
				})
				f.on(modifyPeering, func(int, url.Values) (int, string) {
					return peeringSuccess(modifyPeering)
				})
				priorInputs := tc.desired
				if tc.name == "removed sides" {
					priorInputs.Accepter, priorInputs.Requester = peeringDns(aws.Bool(true)), peeringDns(nil)
				}
				prior := peeringAccepterPrior{Inputs: priorInputs,
					Outputs: &VpcPeeringConnectionAccepterResourceOutput{VpcPeeringConnectionId: testPeeringID},
				}
				var err error
				if method == "create" {
					tc.desired.VpcPeeringConnectionId = testPeeringID
					_, err = tc.desired.create(t.Context(), peeringTestClient(t, f))
				} else {
					_, err = tc.desired.update(t.Context(), peeringTestClient(t, f), prior)
				}
				want := []string{describePeering}
				if tc.fail {
					require.ErrorContains(t, err, "must be active or provisioning")
				} else {
					require.NoError(t, err)
					if tc.params != nil {
						tc.params.Set("VpcPeeringConnectionId", testPeeringID)
						assert.Equal(t, []url.Values{peeringForm(modifyPeering, tc.params)}, f.sent(modifyPeering))
						want = append(want, modifyPeering, describePeering)
					} else {
						assert.Empty(t, f.sent(modifyPeering))
					}
					want = append(want, describePeering)
				}
				assert.Equal(t, want, peeringActions(f))
			})
		}
	}
}

func TestPeeringAccepterDeleteRelinquishes(t *testing.T) {
	f := newFakeEC2(t)
	cfg := f.configuration()
	r := &VpcPeeringConnectionAccepterResource{}
	for _, output := range []*VpcPeeringConnectionAccepterResourceOutput{
		nil, {VpcPeeringConnectionId: testPeeringID, Tags: map[string]string{"keep": "value"}},
	} {
		require.NoError(t, r.Delete(t.Context(), cfg, peeringAccepterPrior{Outputs: output}))
	}
	assert.Empty(t, peeringActions(f))
}

func TestPeeringAccepterDNSPropagation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []string
		err     string
	}{
		{name: "retry then final read", results: []string{
			peeringSideOptions("false", "false", "active"),
			peeringSideOptions("false", "false", "active"),
			peeringSideOptions("true", "false", "active"),
			peeringSideOptions("true", "true", "active"),
		}},
		{name: "missing description", results: []string{
			peeringDefaultResult(testPeeringID, "active"),
			peeringDefaultResult(testPeeringID, "active"),
			peeringDefaultResult(testPeeringID, "active"),
		}},
		{name: "lookup error", results: []string{
			peeringDefaultResult(testPeeringID, "active"), "error",
		}, err: "UnauthorizedOperation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on(describePeering, func(n int, _ url.Values) (int, string) {
				require.LessOrEqual(t, n, len(tc.results))
				if tc.results[n-1] == "error" {
					return http.StatusBadRequest, ec2ErrorXML("UnauthorizedOperation", "detail")
				}
				return peeringPage(tc.results[n-1], "")
			})
			f.on(modifyPeering, func(int, url.Values) (int, string) { return peeringSuccess(modifyPeering) })
			r := &VpcPeeringConnectionAccepterResource{
				VpcPeeringConnectionId: testPeeringID, Accepter: peeringDns(aws.Bool(true)),
			}
			got, err := r.create(t.Context(), peeringTestClient(t, f))
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
			} else {
				require.NoError(t, err)
				if tc.name == "retry then final read" {
					assert.Equal(t, peeringDns(aws.Bool(true)), got.Requester)
				}
				if tc.name == "missing description" {
					assert.Nil(t, got.Accepter)
				}
			}
			want := []string{describePeering, modifyPeering}
			for range tc.results[1:] {
				want = append(want, describePeering)
			}
			assert.Equal(t, want, peeringActions(f))
		})
	}
}

func TestPeeringAccepterDNSHonorsDeadline(t *testing.T) {
	f := newFakeEC2(t)
	f.on(describePeering, func(int, url.Values) (int, string) {
		return peeringPage(peeringSideOptions("false", "false", "active"), "")
	})
	f.on(modifyPeering, func(int, url.Values) (int, string) { return peeringSuccess(modifyPeering) })
	c := peeringTestClient(t, f)
	c.pollInterval = time.Hour
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	r := &VpcPeeringConnectionAccepterResource{
		VpcPeeringConnectionId: testPeeringID, Accepter: peeringDns(aws.Bool(true)),
	}
	_, err := r.create(ctx, c)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, []string{describePeering, modifyPeering, describePeering}, peeringActions(f))
}

func TestPeeringAccepterCreateTags(t *testing.T) {
	for _, tc := range []struct {
		name, code     string
		tags           map[string]string
		retry, options bool
	}{
		{name: "empty"},
		{name: "reserved", tags: map[string]string{"aws:reserved": "ignore"}},
		{name: "add only", tags: map[string]string{"stage": "", "aws:reserved": "ignore"}},
		{name: "retry", tags: map[string]string{"stage": ""}, code: "Resource.NotFound", retry: true},
		{name: "failure", tags: map[string]string{"stage": ""}, code: "UnauthorizedOperation"},
		{name: "options precede tags", tags: map[string]string{"stage": ""}, options: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on(describePeering, func(int, url.Values) (int, string) {
				tags := "<tagSet><item><key>unrelated</key><value>preserved</value></item></tagSet>"
				return peeringPage(peeringResult(testPeeringID, "active",
					peeringSide("requester", testPeeringRequester, testPeeringAccount, "us-east-1", ""),
					peeringSide("accepter", testPeeringAccepter, testPeeringAccount, "us-east-1", ""), tags), "")
			})
			const put = "CreateTags"
			f.on(put, func(n int, _ url.Values) (int, string) {
				if tc.code != "" && (!tc.retry || n == 1) {
					return http.StatusBadRequest, ec2ErrorXML(tc.code, "detail")
				}
				return peeringSuccess(put)
			})
			f.on(modifyPeering, func(int, url.Values) (int, string) { return peeringSuccess(modifyPeering) })
			r := &VpcPeeringConnectionAccepterResource{VpcPeeringConnectionId: testPeeringID, Tags: &tc.tags}
			if tc.options {
				r.Accepter = peeringDns(nil)
			}
			got, err := r.create(t.Context(), peeringTestClient(t, f))
			want := []string{describePeering}
			if tc.options {
				want = append(want, modifyPeering, describePeering)
			}
			if _, writes := tc.tags["stage"]; writes {
				requests := []url.Values{peeringForm(put, url.Values{
					"ResourceId.1": {testPeeringID}, "Tag.1.Key": {"stage"}, "Tag.1.Value": {""},
				})}
				want = append(want, put)
				if tc.retry {
					requests = append(requests, requests[0])
					want = append(want, put)
				}
				assert.Equal(t, requests, f.sent(put))
			}
			if tc.code != "" && !tc.retry {
				require.ErrorContains(t, err, tc.code)
			} else {
				require.NoError(t, err)
				assert.Equal(t, map[string]string{"unrelated": "preserved"}, got.Tags)
				want = append(want, describePeering)
			}
			assert.Equal(t, want, peeringActions(f))
		})
	}
}

func TestPeeringAccepterUpdateTags(t *testing.T) {
	for _, tc := range []struct {
		name, fail string
		desired    *map[string]string
	}{
		{name: "clear"},
		{name: "delete fails", fail: "DeleteTags"},
		{name: "empty values", desired: &map[string]string{
			"keep": "same", "empty": "", "aws:reserved": "ignore",
		}},
		{name: "put fails", fail: "CreateTags", desired: &map[string]string{"keep": "same", "empty": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			for _, action := range []string{"CreateTags", "DeleteTags"} {
				f.on(action, func(int, url.Values) (int, string) {
					if action == tc.fail {
						return http.StatusBadRequest, ec2ErrorXML("Denied", "detail")
					}
					return peeringSuccess(action)
				})
			}
			f.on(describePeering, func(int, url.Values) (int, string) {
				return peeringPage(peeringSideOptions("true", "true", "active"), "")
			})
			oldTags := map[string]string{"old": "prior"}
			r := &VpcPeeringConnectionAccepterResource{
				Tags: tc.desired, Accepter: peeringDns(aws.Bool(true)),
			}
			prior := peeringAccepterPrior{
				Inputs: VpcPeeringConnectionAccepterResource{
					Tags: &oldTags, Accepter: peeringDns(aws.Bool(true)),
				},
				Outputs: &VpcPeeringConnectionAccepterResourceOutput{VpcPeeringConnectionId: testPeeringID},
				Observed: &VpcPeeringConnectionAccepterResourceOutput{Tags: map[string]string{
					"drop": "old", "keep": "same", "aws:reserved": "keep",
				}},
			}
			_, err := r.update(t.Context(), peeringTestClient(t, f), prior)
			removed := url.Values{"ResourceId.1": {testPeeringID}, "Tag.1.Key": {"drop"}}
			if tc.desired == nil {
				removed.Set("Tag.2.Key", "keep")
			}
			assert.Equal(t, []url.Values{peeringForm("DeleteTags", removed)}, f.sent("DeleteTags"))
			want := []string{"DeleteTags"}
			if tc.fail != "DeleteTags" && tc.desired != nil {
				want = append(want, "CreateTags")
				assert.Equal(t, []url.Values{peeringForm("CreateTags", url.Values{
					"ResourceId.1": {testPeeringID}, "Tag.1.Key": {"empty"}, "Tag.1.Value": {""},
				})}, f.sent("CreateTags"))
			}
			if tc.fail != "" {
				require.ErrorContains(t, err, "Denied")
			} else {
				require.NoError(t, err)
				want = append(want, describePeering, describePeering)
			}
			assert.Equal(t, want, peeringActions(f))
		})
	}
}

func TestPeeringAccepterErrorsStopBeforeTags(t *testing.T) {
	for _, failure := range []string{"AcceptVpcPeeringConnection", modifyPeering, describePeering} {
		t.Run(failure, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on(describePeering, func(n int, _ url.Values) (int, string) {
				if failure == describePeering {
					return http.StatusBadRequest, ec2ErrorXML("Denied", "detail")
				}
				status := "active"
				if failure == "AcceptVpcPeeringConnection" && n == 1 {
					status = "pending-acceptance"
				}
				return peeringPage(peeringDefaultResult(testPeeringID, status), "")
			})
			if failure != describePeering {
				f.on(failure, func(int, url.Values) (int, string) {
					return http.StatusBadRequest, ec2ErrorXML("Denied", "detail")
				})
			}
			r := &VpcPeeringConnectionAccepterResource{
				VpcPeeringConnectionId: testPeeringID, AutoAccept: aws.Bool(true), Accepter: peeringDns(nil),
				Tags: &map[string]string{"stage": "value"},
			}
			_, err := r.create(t.Context(), peeringTestClient(t, f))
			require.ErrorContains(t, err, "Denied")
			require.ErrorContains(t, err, testPeeringID)
			want := []string{describePeering}
			if failure != describePeering {
				want = append(want, failure)
			}
			assert.Equal(t, want, peeringActions(f))
		})
	}
	f := newFakeEC2(t)
	f.on(describePeering, func(int, url.Values) (int, string) {
		return peeringPage(peeringDefaultResult(testPeeringID, "pending-acceptance"), "")
	})
	r := &VpcPeeringConnectionAccepterResource{
		VpcPeeringConnectionId: testPeeringID, Requester: peeringDns(nil),
		Tags: &map[string]string{"stage": "value"},
	}
	_, err := r.create(t.Context(), peeringTestClient(t, f))
	require.ErrorContains(t, err, "must be active or provisioning")
	assert.Equal(t, []string{describePeering}, peeringActions(f))
}

func TestPeeringAccepterSavedIdentityPreconditions(t *testing.T) {
	r := &VpcPeeringConnectionAccepterResource{VpcPeeringConnectionId: "pcx-desired"}
	for _, saved := range []*VpcPeeringConnectionAccepterResourceOutput{nil, {}} {
		prior := peeringAccepterPrior{Outputs: saved}
		_, err := r.Read(t.Context(), nil, prior)
		require.ErrorIs(t, err, runtime.ErrNotFound)
		_, err = r.Update(t.Context(), nil, prior)
		require.ErrorContains(t, err, "requires a saved connection ID")
		require.NoError(t, r.Delete(t.Context(), nil, prior))
	}
}

func TestPeeringAccepterReconcilesUnchangedTags(t *testing.T) {
	for _, tc := range []struct {
		name        string
		desired     *map[string]string
		current     map[string]string
		fallback    bool
		remove, put url.Values
	}{
		{name: "drift", desired: &map[string]string{"stage": "", "aws:reserved": "ignore"},
			current: map[string]string{"stage": "drift", "obsolete": "remove", "aws:reserved": "keep"},
			remove:  url.Values{"Tag.1.Key": {"obsolete"}},
			put:     url.Values{"Tag.1.Key": {"stage"}, "Tag.1.Value": {""}}},
		{name: "saved fallback", fallback: true, desired: &map[string]string{"stage": ""},
			current: map[string]string{"stage": "drift"},
			put:     url.Values{"Tag.1.Key": {"stage"}, "Tag.1.Value": {""}}},
		{name: "omitted", current: map[string]string{"obsolete": "remove", "aws:reserved": "keep"},
			remove: url.Values{"Tag.1.Key": {"obsolete"}}},
		{name: "empty map", desired: &map[string]string{},
			current: map[string]string{"obsolete": "remove", "aws:reserved": "keep"},
			remove:  url.Values{"Tag.1.Key": {"obsolete"}}},
		{name: "matching", desired: &map[string]string{"stage": "", "aws:reserved": "ignore"},
			current: map[string]string{"stage": "", "aws:reserved": "keep"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			for _, action := range []string{"DeleteTags", "CreateTags"} {
				f.on(action, func(int, url.Values) (int, string) { return peeringSuccess(action) })
			}
			f.on(describePeering, func(int, url.Values) (int, string) {
				return peeringPage(peeringSideOptions("true", "true", "active"), "")
			})
			r := &VpcPeeringConnectionAccepterResource{
				Tags: tc.desired, Accepter: peeringDns(aws.Bool(true)),
			}
			prior := peeringAccepterPrior{Inputs: *r,
				Outputs: &VpcPeeringConnectionAccepterResourceOutput{
					VpcPeeringConnectionId: testPeeringID, Tags: tc.current,
				},
			}
			if !tc.fallback {
				prior.Outputs.Tags = map[string]string{"wrong": "saved"}
				prior.Observed = &VpcPeeringConnectionAccepterResourceOutput{Tags: tc.current}
			}
			_, err := r.update(t.Context(), peeringTestClient(t, f), prior)
			require.NoError(t, err)
			var want []string
			for _, call := range []struct {
				action string
				params url.Values
			}{
				{"DeleteTags", tc.remove}, {"CreateTags", tc.put},
			} {
				if call.params != nil {
					call.params.Set("ResourceId.1", testPeeringID)
					assert.Equal(t, []url.Values{peeringForm(call.action, call.params)}, f.sent(call.action))
					want = append(want, call.action)
				} else {
					assert.Empty(t, f.sent(call.action))
				}
			}
			want = append(want, describePeering, describePeering)
			assert.Equal(t, want, peeringActions(f))
		})
	}
}
