package eks

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	associationPrincipal     = "arn:aws:iam::123456789012:role/demo"
	associationPolicy        = "arn:aws:eks::aws:cluster-access-policy/AmazonEKSViewPolicy"
	principalNotFoundMessage = "The specified principalArn could not be found"
)

var associationInput = AccessPolicyAssociationResource{
	ClusterName: "demo", PrincipalARN: associationPrincipal, PolicyARN: associationPolicy,
	AccessScope: AccessPolicyAssociationScope{Type: "cluster"},
}

type associationPage struct {
	policies []ekstypes.AssociatedAccessPolicy
	next     string
	err      error
}

type associationClientStub struct {
	pages        map[string]associationPage
	calls        []string
	associate    func(context.Context, *eks.AssociateAccessPolicyInput) error
	disassociate func(*eks.DisassociateAccessPolicyInput) error
}

func (c *associationClientStub) AssociateAccessPolicy(
	ctx context.Context, req *eks.AssociateAccessPolicyInput, _ ...func(*eks.Options),
) (*eks.AssociateAccessPolicyOutput, error) {
	c.calls = append(c.calls, "associate")
	if c.associate != nil {
		if err := c.associate(ctx, req); err != nil {
			return nil, err
		}
	}
	return &eks.AssociateAccessPolicyOutput{
		ClusterName:            aws.String("mutation-cluster"),
		PrincipalArn:           aws.String("mutation-principal"),
		AssociatedAccessPolicy: &ekstypes.AssociatedAccessPolicy{PolicyArn: aws.String("mutation")},
	}, nil
}

func (c *associationClientStub) ListAssociatedAccessPolicies(
	_ context.Context, req *eks.ListAssociatedAccessPoliciesInput, _ ...func(*eks.Options),
) (*eks.ListAssociatedAccessPoliciesOutput, error) {
	token := aws.ToString(req.NextToken)
	c.calls = append(c.calls, "list:"+aws.ToString(req.ClusterName)+
		"/"+aws.ToString(req.PrincipalArn)+":"+token)
	page, ok := c.pages[token]
	if !ok {
		return nil, fmt.Errorf("unexpected page token %q", token)
	}
	if page.err != nil {
		return nil, page.err
	}
	return &eks.ListAssociatedAccessPoliciesOutput{
		AssociatedAccessPolicies: page.policies, NextToken: aws.String(page.next),
	}, nil
}

func (c *associationClientStub) DisassociateAccessPolicy(
	_ context.Context, req *eks.DisassociateAccessPolicyInput, _ ...func(*eks.Options),
) (*eks.DisassociateAccessPolicyOutput, error) {
	c.calls = append(c.calls, "disassociate")
	if c.disassociate != nil {
		return &eks.DisassociateAccessPolicyOutput{}, c.disassociate(req)
	}
	return nil, errors.New("unexpected disassociate")
}

func TestAccessPolicyAssociationCreateReadsEveryPage(t *testing.T) {
	stamp := time.Date(2026, time.November, 22, 11, 12, 13, 123456789,
		time.FixedZone("west", -5*60*60))
	policy := ekstypes.AssociatedAccessPolicy{
		PolicyArn: aws.String(associationPolicy),
		AccessScope: &ekstypes.AccessScope{
			Type: "NAMESPACE", Namespaces: []string{"team", "Team", "team", ""},
		},
		AssociatedAt: &stamp,
	}
	api := &associationClientStub{pages: map[string]associationPage{
		"": {
			policies: []ekstypes.AssociatedAccessPolicy{{
				PolicyArn: aws.String("arn:aws:eks::aws:cluster-access-policy/amazoneksviewpolicy"),
			}},
			next: "later",
		},
		"later": {policies: []ekstypes.AssociatedAccessPolicy{policy}, next: "last"},
		"last":  {},
	}}
	out, err := associationInput.createAccessPolicyAssociation(t.Context(), api, newFakeClusterClock())
	require.NoError(t, err)
	assert.Equal(t, []string{
		"associate", "list:demo/" + associationPrincipal + ":",
		"list:demo/" + associationPrincipal + ":later",
		"list:demo/" + associationPrincipal + ":last",
	}, api.calls)
	assert.Equal(t, &AccessPolicyAssociationResourceOutput{
		ClusterName: "demo", PrincipalARN: associationPrincipal, PolicyARN: associationPolicy,
		AccessScope: &AccessPolicyAssociationScopeOutput{
			Type: "NAMESPACE", Namespaces: []string{"", "Team", "team"},
		},
		AssociatedAt: "2026-11-22T16:12:13.123456789Z",
	}, out)
}

func TestAccessPolicyAssociationReadClassifiesLaterResults(t *testing.T) {
	match := ekstypes.AssociatedAccessPolicy{PolicyArn: aws.String(associationPolicy)}
	missing := &ekstypes.ResourceNotFoundException{Message: aws.String("missing principal")}
	denied := &ekstypes.AccessDeniedException{Message: aws.String("denied")}
	for _, tc := range []struct {
		name         string
		first, later associationPage
		want         string
		cause        error
		absent       bool
	}{
		{name: "no match", want: "no matching policy", absent: true},
		{
			name:  "duplicate",
			first: associationPage{policies: []ekstypes.AssociatedAccessPolicy{match}},
			later: associationPage{policies: []ekstypes.AssociatedAccessPolicy{match}, next: "last"},
			want:  "2 matching policies", absent: true,
		},
		{
			name:  "typed absence",
			first: associationPage{policies: []ekstypes.AssociatedAccessPolicy{match}},
			later: associationPage{err: fmt.Errorf("page failed: %w", missing)},
			want:  "missing principal", cause: missing, absent: true,
		},
		{
			name:  "access denied",
			first: associationPage{policies: []ekstypes.AssociatedAccessPolicy{match}},
			later: associationPage{err: denied},
			want:  "denied", cause: denied,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.first.next = "later"
			api := &associationClientStub{pages: map[string]associationPage{
				"": tc.first, "later": tc.later, "last": {},
			}}
			key := accessPolicyAssociationKey{"demo", associationPrincipal, associationPolicy}
			wantReads := []string{
				"list:demo/" + associationPrincipal + ":", "list:demo/" + associationPrincipal + ":later",
			}
			if tc.later.next != "" {
				wantReads = append(wantReads, "list:demo/"+associationPrincipal+":last")
			}
			out, err := readAccessPolicyAssociation(t.Context(), api, key)
			assert.Nil(t, out)
			if tc.absent {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.ErrorIs(t, err, tc.cause)
				assert.ErrorContains(t, err, "list associated EKS access policies")
				assert.ErrorContains(t, err, associationPolicy)
			}
			assert.Equal(t, wantReads, api.calls)
			api.calls = nil

			out, err = associationInput.createAccessPolicyAssociation(
				t.Context(), api, newFakeClusterClock(),
			)
			assert.Nil(t, out)
			require.ErrorContains(t, err, tc.want)
			assert.ErrorContains(t, err, associationPrincipal)
			assert.ErrorContains(t, err, associationPolicy)
			assert.NotErrorIs(t, err, runtime.ErrNotFound)
			if tc.cause != nil {
				assert.ErrorIs(t, err, tc.cause)
			}
			assert.Equal(t, append([]string{"associate"}, wantReads...), api.calls)
		})
	}
}

func TestAccessPolicyAssociationPrincipalRetry(t *testing.T) {
	retryable := &ekstypes.ResourceNotFoundException{
		Message: aws.String("prefix " + principalNotFoundMessage + " suffix"),
	}
	other := errors.New(principalNotFoundMessage)
	missing := &ekstypes.ResourceNotFoundException{Message: aws.String("missing cluster")}
	lowercase := &ekstypes.ResourceNotFoundException{
		Message: aws.String("the specified principalArn could not be found"),
	}
	for _, tc := range []struct {
		name         string
		first        error
		cancel       bool
		succeed      bool
		wantAttempts []time.Duration
		want         error
	}{
		{
			name: "wrapped then success", first: fmt.Errorf("wrapped: %w", retryable), succeed: true,
			wantAttempts: []time.Duration{0, 500 * time.Millisecond},
		},
		{
			name: "exhaustion", first: retryable, want: retryable,
			wantAttempts: []time.Duration{
				0, 500 * time.Millisecond, 1500 * time.Millisecond, 3500 * time.Millisecond,
				7500 * time.Millisecond, 15500 * time.Millisecond, 25500 * time.Millisecond,
				35500 * time.Millisecond, 45500 * time.Millisecond, 55500 * time.Millisecond,
				65500 * time.Millisecond, 75500 * time.Millisecond, 85500 * time.Millisecond,
				95500 * time.Millisecond, 105500 * time.Millisecond, 115500 * time.Millisecond,
				2 * time.Minute,
			},
		},
		{
			name: "cancel", first: retryable, cancel: true, want: context.Canceled,
			wantAttempts: []time.Duration{0},
		},
		{name: "different case", first: lowercase, want: lowercase, wantAttempts: []time.Duration{0}},
		{name: "unrelated absence", first: missing, want: missing, wantAttempts: []time.Duration{0}},
		{name: "untyped error", first: other, want: other, wantAttempts: []time.Duration{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := newFakeClusterClock()
			var attempts []time.Duration
			var last error
			api := &associationClientStub{
				pages: map[string]associationPage{
					"": {policies: []ekstypes.AssociatedAccessPolicy{{
						PolicyArn: aws.String(associationPolicy),
					}}},
				},
				associate: func(_ context.Context, req *eks.AssociateAccessPolicyInput) error {
					attempts = append(attempts, clock.elapsed())
					assert.Equal(t, "demo", aws.ToString(req.ClusterName))
					assert.Equal(t, associationPrincipal, aws.ToString(req.PrincipalArn))
					assert.Equal(t, associationPolicy, aws.ToString(req.PolicyArn))
					if tc.cancel {
						cancel()
					}
					if tc.succeed && len(attempts) > 1 {
						return nil
					}
					last = fmt.Errorf("attempt %d: %w", len(attempts), tc.first)
					return last
				},
			}
			out, err := associationInput.createAccessPolicyAssociation(ctx, api, clock)
			assert.Equal(t, tc.wantAttempts, attempts)
			wantCalls := make([]string, len(tc.wantAttempts))
			for i := range wantCalls {
				wantCalls[i] = "associate"
			}
			if tc.succeed {
				require.NoError(t, err)
				assert.Equal(t, &AccessPolicyAssociationResourceOutput{
					ClusterName: "demo", PrincipalARN: associationPrincipal,
					PolicyARN: associationPolicy,
				}, out)
				wantCalls = append(wantCalls, "list:demo/"+associationPrincipal+":")
			} else {
				require.ErrorIs(t, err, tc.want)
				if !tc.cancel {
					assert.ErrorIs(t, err, last)
				}
			}
			assert.Equal(t, wantCalls, api.calls)
		})
	}
}

func TestAccessPolicyAssociationRetryGraceAfterSlowRequest(t *testing.T) {
	clock := newFakeClusterClock()
	missing := &ekstypes.ResourceNotFoundException{Message: aws.String(principalNotFoundMessage)}
	var attempts []time.Duration
	api := &associationClientStub{
		associate: func(context.Context, *eks.AssociateAccessPolicyInput) error {
			attempts = append(attempts, clock.elapsed())
			clock.now = clock.now.Add(121 * time.Second)
			return missing
		},
	}
	_, err := associationInput.createAccessPolicyAssociation(t.Context(), api, clock)
	require.ErrorIs(t, err, missing)
	assert.Equal(t, []time.Duration{0, 121 * time.Second}, attempts)
	assert.Equal(t, []string{"associate", "associate"}, api.calls)
}

func TestAccessPolicyAssociationRejectsIncompletePriorIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output *AccessPolicyAssociationResourceOutput
		want   string
	}{
		{"missing", nil, "prior EKS access policy association is missing"},
		{"cluster", &AccessPolicyAssociationResourceOutput{
			PrincipalARN: associationPrincipal, PolicyARN: associationPolicy,
		}, "prior association has no cluster name"},
		{"principal", &AccessPolicyAssociationResourceOutput{
			ClusterName: "old", PolicyARN: associationPolicy,
		}, "prior association has no principal ARN"},
		{"policy", &AccessPolicyAssociationResourceOutput{
			ClusterName: "old", PrincipalARN: associationPrincipal,
		}, "prior association has no policy ARN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := accessPolicyAssociationPrior{Outputs: tc.output}
			cfg := &awsCfg{Profile: aws.String("nonexistent-association-test-profile")}
			desired := associationInput
			_, err := desired.Read(t.Context(), cfg, prior)
			require.EqualError(t, err, tc.want)
			require.EqualError(t, desired.Delete(t.Context(), cfg, prior), tc.want)
		})
	}
}

func TestAccessPolicyAssociationDeleteClassifiesTypedAbsence(t *testing.T) {
	missing := &ekstypes.ResourceNotFoundException{Message: aws.String("missing")}
	untyped := errors.New("ResourceNotFoundException: missing")
	denied := &ekstypes.AccessDeniedException{Message: aws.String("denied")}
	for _, tc := range []struct {
		name           string
		response, want error
	}{
		{"success", nil, nil},
		{"typed absence", fmt.Errorf("operation: %w", missing), nil},
		{"untyped absence", untyped, untyped},
		{"access denied", denied, denied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &associationClientStub{
				disassociate: func(req *eks.DisassociateAccessPolicyInput) error {
					assert.Equal(t, &eks.DisassociateAccessPolicyInput{
						ClusterName: aws.String("old"), PrincipalArn: aws.String(associationPrincipal),
						PolicyArn: aws.String(associationPolicy),
					}, req)
					return tc.response
				},
			}
			key := accessPolicyAssociationKey{"old", associationPrincipal, associationPolicy}
			err := disassociateAccessPolicy(t.Context(), api, key)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				assert.ErrorContains(t, err, "disassociate EKS access policy old / ")
				assert.ErrorContains(t, err, associationPolicy)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, []string{"disassociate"}, api.calls)
		})
	}
}
