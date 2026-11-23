package ec2

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	describePeering = "DescribeVpcPeeringConnections"
	modifyPeering   = "ModifyVpcPeeringConnectionOptions"
	deletePeering   = "DeleteVpcPeeringConnection"
)

func peeringDns(flag *bool) *VpcPeeringDnsOptions {
	return &VpcPeeringDnsOptions{AllowRemoteVpcDnsResolution: flag}
}

func peeringDnsXML(flag string) string {
	if flag == "absent" {
		return ""
	}
	if flag == "" {
		return "<peeringOptions/>"
	}
	return "<peeringOptions><allowDnsResolutionFromRemoteVpc>" + flag +
		"</allowDnsResolutionFromRemoteVpc></peeringOptions>"
}

func peeringSideOptions(accepter, requester, status string) string {
	return peeringResult(testPeeringID, status,
		peeringSide("requester", testPeeringRequester, testPeeringAccount, "us-east-1",
			peeringDnsXML(requester)),
		peeringSide("accepter", testPeeringAccepter, testPeeringAccount, "us-east-1",
			peeringDnsXML(accepter)), "")
}

func TestPeeringReconcilesDNSOptions(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		accepter, requester         *VpcPeeringDnsOptions
		describes                   []string
		modify                      url.Values
		wantAccepter, wantRequester *VpcPeeringDnsOptions
		diagnostic                  string
	}{
		{
			name:     "both sides with stale and settled readback",
			accepter: peeringDns(nil), requester: peeringDns(aws.Bool(true)),
			describes: []string{
				peeringSideOptions("true", "false", "provisioning"),
				peeringSideOptions("true", "true", "active"),
				peeringSideOptions("false", "true", "active"),
				peeringSideOptions("", "true", "active"),
			},
			modify: url.Values{
				"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc":  {"false"},
				"RequesterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"true"},
			},
			wantAccepter: peeringDns(nil), wantRequester: peeringDns(aws.Bool(true)),
		},
		{
			name:      "empty requester block and missing propagation description",
			requester: peeringDns(nil),
			describes: []string{
				peeringSideOptions("true", "absent", "active"),
				peeringSideOptions("false", "absent", "active"),
				peeringSideOptions("true", "absent", "active"),
			},
			modify: url.Values{
				"RequesterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"false"},
			},
			wantAccepter: peeringDns(aws.Bool(true)),
		},
		{
			name:     "accepter true to explicit false and nil flag propagation",
			accepter: peeringDns(aws.Bool(false)),
			describes: []string{
				peeringSideOptions("true", "true", "active"),
				peeringSideOptions("", "true", "active"),
				peeringSideOptions("", "true", "active"),
			},
			modify: url.Values{
				"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"false"},
			},
			wantAccepter: peeringDns(nil), wantRequester: peeringDns(aws.Bool(true)),
		},
		{
			name: "block removal leaves both options alone",
			describes: []string{
				peeringSideOptions("true", "false", "active"),
				peeringSideOptions("true", "false", "active"),
			},
			wantAccepter: peeringDns(aws.Bool(true)), wantRequester: peeringDns(aws.Bool(false)),
		},
		{
			name:     "matching options leave pending connection alone",
			accepter: peeringDns(aws.Bool(false)), requester: peeringDns(aws.Bool(true)),
			describes: []string{
				peeringSideOptions("false", "true", "pending-acceptance"),
				peeringSideOptions("false", "true", "pending-acceptance"),
			},
			wantAccepter: peeringDns(aws.Bool(false)), wantRequester: peeringDns(aws.Bool(true)),
		},
		{
			name: "pending connection needs to be accepted first", accepter: peeringDns(aws.Bool(true)),
			describes:  []string{peeringSideOptions("false", "false", "pending-acceptance")},
			diagnostic: `status "pending-acceptance" must be active or provisioning`,
		},
		{
			name: "strict absence ends propagation immediately", accepter: peeringDns(aws.Bool(true)),
			describes: []string{peeringSideOptions("false", "false", "active"), ""},
			modify: url.Values{
				"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"true"},
			},
			diagnostic: "DNS options propagation",
		},
		{
			name:      "unrelated service error ends propagation",
			accepter:  peeringDns(aws.Bool(true)),
			describes: []string{peeringSideOptions("false", "false", "active"), "unauthorized"},
			modify: url.Values{
				"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"true"},
			},
			diagnostic: "UnauthorizedOperation",
		},
		{
			name:       "missing initial observation fails update",
			describes:  []string{""},
			diagnostic: "for update",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			c := peeringTestClient(t, f)
			f.on(describePeering, func(n int, _ url.Values) (int, string) {
				if n > len(tc.describes) {
					t.Errorf("unexpected describe %d", n)
					return http.StatusBadRequest, ec2ErrorXML("UnauthorizedOperation", "unexpected")
				}
				if tc.describes[n-1] == "unauthorized" {
					return http.StatusBadRequest, ec2ErrorXML("UnauthorizedOperation", "describe")
				}
				return peeringPage(tc.describes[n-1], "")
			})
			f.on(modifyPeering, func(int, url.Values) (int, string) {
				return peeringSuccess(modifyPeering)
			})
			prior := peeringPrior{
				Inputs: VpcPeeringConnectionResource{
					Accepter: peeringDns(aws.Bool(true)), Requester: peeringDns(aws.Bool(true)),
				},
				Outputs: &VpcPeeringConnectionResourceOutput{VpcPeeringConnectionId: testPeeringID},
			}
			resource := &VpcPeeringConnectionResource{Accepter: tc.accepter, Requester: tc.requester}
			updated, err := resource.update(t.Context(), c, prior)
			if tc.diagnostic == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.wantAccepter, updated.Accepter)
				assert.Equal(t, tc.wantRequester, updated.Requester)
			} else {
				require.ErrorContains(t, err, tc.diagnostic)
			}
			expected := []string{describePeering}
			if tc.modify != nil {
				tc.modify.Set("VpcPeeringConnectionId", testPeeringID)
				assert.Equal(t, []url.Values{peeringForm(modifyPeering, tc.modify)},
					f.sent(modifyPeering))
				expected = append(expected, modifyPeering)
			}
			for range tc.describes[1:] {
				expected = append(expected, describePeering)
			}
			assert.Equal(t, expected, peeringActions(f))
		})
	}
}

func TestPeeringDNSPropagationStopsOnMismatchTimeout(t *testing.T) {
	f := newFakeEC2(t)
	c := peeringTestClient(t, f)
	c.pollInterval = time.Hour
	f.on(describePeering, func(int, url.Values) (int, string) {
		return peeringPage(peeringSideOptions("false", "false", "active"), "")
	})
	f.on(modifyPeering, func(int, url.Values) (int, string) { return peeringSuccess(modifyPeering) })
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	r := &VpcPeeringConnectionResource{Accepter: peeringDns(aws.Bool(true))}
	prior := peeringPrior{
		Outputs: &VpcPeeringConnectionResourceOutput{VpcPeeringConnectionId: testPeeringID},
	}
	_, err := r.update(ctx, c, prior)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "DNS options propagation")
	assert.Equal(t, []string{describePeering, modifyPeering, describePeering}, peeringActions(f))
}

func TestPeeringReadUsesCallerAccountAndAPISides(t *testing.T) {
	const otherAccount = "210987654321"
	for _, tc := range []struct {
		name, requesterAccount, accepterAccount, caller string
		sts, reversed                                   bool
	}{
		{"same owners", testPeeringAccount, testPeeringAccount, "", false, false},
		{"distinct requester from credential",
			testPeeringAccount, otherAccount, testPeeringAccount, false, false},
		{"distinct accepter from credential",
			testPeeringAccount, otherAccount, otherAccount, false, true},
		{"distinct accepter from configured STS",
			testPeeringAccount, otherAccount, otherAccount, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			sts := newFakeEC2(t)
			cfg := f.configuration()
			cfg.Endpoints = &awscfg.Endpoints{STS: aws.String(sts.server.URL)}
			client, err := newPeeringClient(t.Context(), cfg)
			require.NoError(t, err)
			credential := aws.Credentials{
				AccessKeyID: "AKIAFAKEFAKEFAKEFAKE", SecretAccessKey: "fake-secret-key",
			}
			if !tc.sts {
				credential.AccountID = tc.caller
			}
			client.config.Credentials = aws.CredentialsProviderFunc(
				func(context.Context) (aws.Credentials, error) { return credential, nil })
			sts.on("GetCallerIdentity", func(int, url.Values) (int, string) {
				return http.StatusOK, "<GetCallerIdentityResponse><GetCallerIdentityResult>" +
					"<Account>" + tc.caller + "</Account></GetCallerIdentityResult>" +
					"</GetCallerIdentityResponse>"
			})
			f.on(describePeering, func(int, url.Values) (int, string) {
				tags := "<tagSet><item><key>stage</key><value/></item><item>" +
					"<key>aws:reserved</key><value>service</value></item></tagSet>"
				return peeringPage(peeringResult(testPeeringID, "provisioning",
					peeringSide("requester", testPeeringRequester, tc.requesterAccount,
						"us-east-1", peeringDnsXML("")),
					peeringSide("accepter", testPeeringAccepter, tc.accepterAccount,
						"us-west-2", peeringDnsXML("true")), tags), "")
			})
			result, err := client.read(t.Context(), testPeeringID)
			require.NoError(t, err)
			expected := VpcPeeringConnectionResourceOutput{
				Region: "us-east-1", PeerRegion: "us-west-2",
				AcceptStatus: "provisioning", VpcPeeringConnectionId: testPeeringID,
				VpcId: testPeeringRequester, PeerVpcId: testPeeringAccepter,
				PeerOwnerId: tc.accepterAccount, Accepter: peeringDns(aws.Bool(true)),
				Requester: peeringDns(nil), Tags: map[string]string{"stage": ""},
			}
			if tc.reversed {
				expected.VpcId, expected.PeerVpcId = expected.PeerVpcId, expected.VpcId
				expected.PeerOwnerId = tc.requesterAccount
			}
			assert.Equal(t, &expected, result)
			if tc.sts {
				assert.Equal(t, []url.Values{{
					"Action": {"GetCallerIdentity"}, "Version": {"2011-06-15"},
				}}, sts.sent("GetCallerIdentity"))
			} else {
				assert.Empty(t, peeringActions(sts))
			}
		})
	}
}

func TestPeeringUpdateUsesKnownTagsBeforeLookup(t *testing.T) {
	const remove = "DeleteTags"
	const put = "CreateTags"
	for _, fail := range []string{"", remove, put} {
		t.Run(fmt.Sprintf("fail %q", fail), func(t *testing.T) {
			f := newFakeEC2(t)
			c := peeringTestClient(t, f)
			for _, action := range []string{remove, put} {
				f.on(action, func(int, url.Values) (int, string) {
					if fail == action {
						return http.StatusBadRequest, ec2ErrorXML("UnauthorizedOperation", action)
					}
					return peeringSuccess(action)
				})
			}
			f.on(describePeering, func(int, url.Values) (int, string) {
				tags := "<tagSet><item><key>keep</key><value>same</value></item>" +
					"<item><key>empty</key><value/></item>" +
					"<item><key>aws:reserved</key><value>keep</value></item></tagSet>"
				return peeringPage(peeringResult(testPeeringID, "active",
					peeringSide("requester", testPeeringRequester, testPeeringAccount, "us-east-1", ""),
					peeringSide("accepter", testPeeringAccepter, testPeeringAccount, "us-east-1", ""),
					tags), "")
			})
			desired := map[string]string{"empty": "", "keep": "same", "aws:reserved": "ignore"}
			r := &VpcPeeringConnectionResource{Tags: &desired, AutoAccept: aws.Bool(false)}
			prior := peeringPrior{
				Inputs: VpcPeeringConnectionResource{AutoAccept: aws.Bool(true)},
				Outputs: &VpcPeeringConnectionResourceOutput{
					VpcPeeringConnectionId: testPeeringID, Tags: map[string]string{"stale": "discard"},
				},
				Observed: &VpcPeeringConnectionResourceOutput{
					Tags: map[string]string{"drop": "old", "keep": "same", "aws:reserved": "keep"},
				},
			}
			output, err := r.update(t.Context(), c, prior)
			expected := []string{remove}
			if fail != remove {
				expected = append(expected, put)
				assert.Equal(t, []url.Values{peeringForm(put, url.Values{
					"ResourceId.1": {testPeeringID}, "Tag.1.Key": {"empty"}, "Tag.1.Value": {""},
				})}, f.sent(put))
			}
			if fail != "" {
				require.ErrorContains(t, err, "UnauthorizedOperation")
			} else {
				require.NoError(t, err)
				expected = append(expected, describePeering, describePeering)
				assert.Equal(t, map[string]string{"keep": "same", "empty": ""}, output.Tags)
				assert.Equal(t, "active", output.AcceptStatus)
			}
			assert.Equal(t, expected, peeringActions(f))
			assert.Equal(t, []url.Values{peeringForm(remove, url.Values{
				"ResourceId.1": {testPeeringID}, "Tag.1.Key": {"drop"},
			})}, f.sent(remove))
		})
	}
}

func TestPeeringDeleteLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, code, message string
		statuses            []string
		diagnostic          string
	}{
		{name: "AWS not found", code: peeringNotFound},
		{name: "in transition", code: "InvalidStateTransition", message: "cannot move to deleting"},
		{name: "unexpected service code", code: "Other.InvalidStateTransition",
			message: "cannot move to deleting", diagnostic: "Other.InvalidStateTransition"},
		{name: "different transition", code: "InvalidStateTransition",
			message: "cannot move to rejected", diagnostic: "cannot move to rejected"},
		{name: "different not found", code: "InvalidVpcPeeringConnection.NotFound",
			diagnostic: "InvalidVpcPeeringConnection.NotFound"},
		{name: "first strict absence",
			statuses: []string{"active", "pending-acceptance", "deleting", ""}},
		{name: "strict deleted status", statuses: []string{"deleting", "deleted"}},
		{name: "unexpected status", statuses: []string{"provisioning"},
			diagnostic: `unexpected status "provisioning": status detail`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			c := peeringTestClient(t, f)
			f.on(deletePeering, func(int, url.Values) (int, string) {
				if tc.code != "" {
					return http.StatusBadRequest, ec2ErrorXML(tc.code, tc.message)
				}
				return peeringSuccess(deletePeering)
			})
			f.on(describePeering, func(n int, _ url.Values) (int, string) {
				if n > len(tc.statuses) {
					t.Errorf("unexpected describe call %d", n)
					return peeringPage("", "")
				}
				if tc.statuses[n-1] == "" {
					return peeringPage("", "")
				}
				return peeringPage(peeringDefaultResult(testPeeringID, tc.statuses[n-1]), "")
			})
			err := c.delete(t.Context(), testPeeringID)
			if tc.diagnostic != "" {
				require.ErrorContains(t, err, tc.diagnostic)
				require.NotErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.NoError(t, err)
			}
			actions := []string{deletePeering}
			for range tc.statuses {
				actions = append(actions, describePeering)
			}
			assert.Equal(t, actions, peeringActions(f))
			assert.Equal(t, []url.Values{peeringForm(deletePeering, url.Values{
				"VpcPeeringConnectionId": {testPeeringID},
			})}, f.sent(deletePeering))
		})
	}
}

func TestPeeringUpdateAcceptsPendingConnectionBySavedID(t *testing.T) {
	f := newFakeEC2(t)
	c := peeringTestClient(t, f)
	const accept = "AcceptVpcPeeringConnection"
	f.on(accept, func(int, url.Values) (int, string) { return peeringSuccess(accept) })
	f.on(describePeering, func(n int, _ url.Values) (int, string) {
		status := "active"
		if n == 1 {
			status = "pending-acceptance"
		}
		return peeringPage(peeringDefaultResult(testPeeringID, status), "")
	})
	r := &VpcPeeringConnectionResource{AutoAccept: aws.Bool(true)}
	prior := peeringPrior{
		Inputs:  VpcPeeringConnectionResource{AutoAccept: aws.Bool(false)},
		Outputs: &VpcPeeringConnectionResourceOutput{VpcPeeringConnectionId: testPeeringID},
	}
	output, err := r.update(t.Context(), c, prior)
	require.NoError(t, err)
	assert.Equal(t, testPeeringID, output.VpcPeeringConnectionId)
	assert.Equal(t, "active", output.AcceptStatus)
	assert.Equal(t, []url.Values{peeringForm(accept, url.Values{
		"VpcPeeringConnectionId": {testPeeringID},
	})}, f.sent(accept))
	assert.Equal(t, []string{
		describePeering, accept, describePeering, describePeering, describePeering,
	}, peeringActions(f))
}

func TestPeeringCreateAppliesOptionsAfterAcceptance(t *testing.T) {
	f := newFakeEC2(t)
	c := peeringTestClient(t, f)
	const create = "CreateVpcPeeringConnection"
	const accept = "AcceptVpcPeeringConnection"
	f.on(create, func(int, url.Values) (int, string) {
		return http.StatusOK, "<CreateVpcPeeringConnectionResponse><vpcPeeringConnection>" +
			"<vpcPeeringConnectionId>" + testPeeringID + "</vpcPeeringConnectionId>" +
			"</vpcPeeringConnection></CreateVpcPeeringConnectionResponse>"
	})
	f.on(accept, func(int, url.Values) (int, string) { return peeringSuccess(accept) })
	f.on(modifyPeering, func(int, url.Values) (int, string) { return peeringSuccess(modifyPeering) })
	f.on(describePeering, func(n int, _ url.Values) (int, string) {
		status, flag := "active", "false"
		if n <= 2 {
			status = "pending-acceptance"
		}
		if n >= 5 {
			flag = "true"
		}
		return peeringPage(peeringSideOptions(flag, "absent", status), "")
	})
	r := &VpcPeeringConnectionResource{
		VpcId: testPeeringRequester, PeerVpcId: testPeeringAccepter, AutoAccept: aws.Bool(true),
		Accepter: peeringDns(aws.Bool(true)),
	}
	output, err := r.create(t.Context(), c)
	require.NoError(t, err)
	assert.Equal(t, peeringDns(aws.Bool(true)), output.Accepter)
	assert.Equal(t, testPeeringID, output.VpcPeeringConnectionId)
	assert.Equal(t, []url.Values{peeringForm(modifyPeering, url.Values{
		"VpcPeeringConnectionId": {testPeeringID},
		"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"true"},
	})}, f.sent(modifyPeering))
	assert.Equal(t, []string{
		create, describePeering, describePeering, accept, describePeering, describePeering,
		modifyPeering, describePeering, describePeering,
	}, peeringActions(f))
}
