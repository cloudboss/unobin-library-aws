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
	entryPrincipal = "arn:aws:iam::123456789012:role/example:with:colon"
	entryARN       = "arn:aws:eks:us-east-1:123456789012:access-entry/demo/example"
)

type entryCall struct {
	request any
	reply   any
	err     error
}

type entryScript struct {
	t     *testing.T
	calls []entryCall
}

func newEntryScript(t *testing.T, calls ...entryCall) *entryScript {
	t.Helper()
	s := &entryScript{t: t, calls: calls}
	t.Cleanup(func() { assert.Empty(t, s.calls, "unconsumed EKS calls") })
	return s
}

func nextEntryCall[T any](s *entryScript, request any) (*T, error) {
	s.t.Helper()
	require.NotEmpty(s.t, s.calls, "unexpected EKS call: %#v", request)
	call := s.calls[0]
	s.calls = s.calls[1:]
	require.Equal(s.t, call.request, request)
	reply, _ := call.reply.(*T)
	return reply, call.err
}

func (s *entryScript) CreateAccessEntry(
	_ context.Context, input *eks.CreateAccessEntryInput, _ ...func(*eks.Options),
) (*eks.CreateAccessEntryOutput, error) {
	return nextEntryCall[eks.CreateAccessEntryOutput](s, input)
}

func (s *entryScript) DescribeAccessEntry(
	_ context.Context, input *eks.DescribeAccessEntryInput, _ ...func(*eks.Options),
) (*eks.DescribeAccessEntryOutput, error) {
	return nextEntryCall[eks.DescribeAccessEntryOutput](s, input)
}

func (s *entryScript) UpdateAccessEntry(
	_ context.Context, input *eks.UpdateAccessEntryInput, _ ...func(*eks.Options),
) (*eks.UpdateAccessEntryOutput, error) {
	return nextEntryCall[eks.UpdateAccessEntryOutput](s, input)
}

func (s *entryScript) DeleteAccessEntry(
	_ context.Context, input *eks.DeleteAccessEntryInput, _ ...func(*eks.Options),
) (*eks.DeleteAccessEntryOutput, error) {
	return nextEntryCall[eks.DeleteAccessEntryOutput](s, input)
}

func (s *entryScript) ListTagsForResource(
	_ context.Context, input *eks.ListTagsForResourceInput, _ ...func(*eks.Options),
) (*eks.ListTagsForResourceOutput, error) {
	return nextEntryCall[eks.ListTagsForResourceOutput](s, input)
}

func (s *entryScript) TagResource(
	_ context.Context, input *eks.TagResourceInput, _ ...func(*eks.Options),
) (*eks.TagResourceOutput, error) {
	return nextEntryCall[eks.TagResourceOutput](s, input)
}

func (s *entryScript) UntagResource(
	_ context.Context, input *eks.UntagResourceInput, _ ...func(*eks.Options),
) (*eks.UntagResourceOutput, error) {
	return nextEntryCall[eks.UntagResourceOutput](s, input)
}

func entryInput() AccessEntryResource {
	return AccessEntryResource{ClusterName: "demo", PrincipalARN: entryPrincipal}
}

func entryCreateRequest() *eks.CreateAccessEntryInput {
	return &eks.CreateAccessEntryInput{
		ClusterName: aws.String("demo"), PrincipalArn: aws.String(entryPrincipal),
		ClientRequestToken: aws.String("request-token"), Type: aws.String("STANDARD"),
	}
}

func entryToken() (string, error) { return "request-token", nil }

func entryDescribeCall() entryCall {
	return entryCall{
		request: &eks.DescribeAccessEntryInput{
			ClusterName: aws.String("demo"), PrincipalArn: aws.String(entryPrincipal),
		},
		reply: &eks.DescribeAccessEntryOutput{AccessEntry: &ekstypes.AccessEntry{
			ClusterName: aws.String("demo"), PrincipalArn: aws.String(entryPrincipal),
			AccessEntryArn: aws.String(entryARN), Type: aws.String("STANDARD"),
			Username:         aws.String("remote-user"),
			KubernetesGroups: []string{"z", "a", "z"},
			CreatedAt:        aws.Time(time.Date(2026, 9, 2, 3, 4, 5, 678, time.UTC)),
			Tags:             map[string]string{"empty": "", "aws:system": "reserved"},
		}},
	}
}

func TestAccessEntryCreateRequestsAndOutputs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		groups     *[]string
		user       *string
		tags       *map[string]string
		wantGroups []string
		wantUser   *string
		wantTags   map[string]string
	}{
		{name: "omitted"},
		{name: "empty", groups: &[]string{}, user: aws.String(""), tags: &map[string]string{}},
		{
			name: "members", groups: &[]string{"z", "", "z", "a"}, user: aws.String("configured"),
			tags:       &map[string]string{"empty": "", "aws:system": "ignore"},
			wantGroups: []string{"a", "z"}, wantUser: aws.String("configured"),
			wantTags: map[string]string{"empty": ""},
		},
		{name: "all filtered", groups: &[]string{""}, wantGroups: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := entryInput()
			input.KubernetesGroups, input.UserName, input.Tags = tc.groups, tc.user, tc.tags
			request := entryCreateRequest()
			request.KubernetesGroups, request.Username, request.Tags =
				tc.wantGroups, tc.wantUser, tc.wantTags
			client := newEntryScript(t, entryCall{request: request}, entryDescribeCall())
			result, err := input.createAccessEntry(t.Context(), client, newFakeClusterClock(),
				entryToken)
			require.NoError(t, err)
			assert.Equal(t, &AccessEntryResourceOutput{
				ClusterName: "demo", PrincipalARN: entryPrincipal, ARN: entryARN,
				Type: "STANDARD", UserName: "remote-user", KubernetesGroups: []string{"a", "z"},
				CreatedAt: "2026-09-02T03:04:05Z", ModifiedAt: "0001-01-01T00:00:00Z",
				Tags: map[string]string{"empty": ""},
			}, result)
		})
	}
}

func TestAccessEntryARNPreparation(t *testing.T) {
	for _, value := range []string{
		"arn", "arn:other:iam::123456789012:role/x", "arn:aws:iam:us-east:123456789012:x",
		"arn:aws:iam::123:x", "arn:aws:iam::123456789012:",
	} {
		t.Run(value, func(t *testing.T) {
			input := entryInput()
			input.PrincipalARN = value
			_, err := input.createAccessEntry(t.Context(), newEntryScript(t),
				newFakeClusterClock(), entryToken)
			require.ErrorContains(t, err, "principal-arn")
		})
	}
	for _, value := range []string{entryPrincipal, "arn:aws-cn:x:cn-north-1:cw0123456789:a:b",
		"arn:aws:x::partner-managed:a", "arn:aws:x:::a"} {
		t.Run(value, func(t *testing.T) {
			input := entryInput()
			input.PrincipalARN = value
			request, err := input.prepareAccessEntryCreate()
			require.NoError(t, err)
			assert.Equal(t, aws.String(value), request.PrincipalArn)
		})
	}

	input := entryInput()
	input.PrincipalARN = ""
	request := entryCreateRequest()
	request.PrincipalArn = aws.String("")
	apiErr := &ekstypes.InvalidParameterException{Message: aws.String("principal is empty")}
	client := newEntryScript(t, entryCall{request: request, err: apiErr})
	_, err := input.createAccessEntry(t.Context(), client, newFakeClusterClock(), entryToken)
	require.ErrorIs(t, err, apiErr)
}

type cancelEntryRetryClock struct {
	*fakeClusterClock
	cancel context.CancelFunc
}

func (c cancelEntryRetryClock) Sleep(ctx context.Context, delay time.Duration) error {
	c.cancel()
	return c.fakeClusterClock.Sleep(ctx, delay)
}

func TestAccessEntryCreateRetry(t *testing.T) {
	const message = "The specified principalArn is invalid: invalid principal"
	transient := fmt.Errorf("wrapped: %w", &ekstypes.InvalidParameterException{
		Message: aws.String("error: " + message + " (propagating)"),
	})
	t.Run("recovers", func(t *testing.T) {
		request := entryCreateRequest()
		client := newEntryScript(t, entryCall{request: request, err: transient},
			entryCall{request: request}, entryDescribeCall())
		output, err := entryInput().createAccessEntry(t.Context(), client, newFakeClusterClock(),
			entryToken)
		require.NoError(t, err)
		assert.Equal(t, entryARN, output.ARN)
	})
	for _, other := range []error{
		errors.New(message),
		&ekstypes.InvalidParameterException{Message: aws.String("Invalid principal")},
	} {
		t.Run(other.Error(), func(t *testing.T) {
			client := newEntryScript(t, entryCall{request: entryCreateRequest(), err: other})
			_, err := entryInput().createAccessEntry(t.Context(), client, newFakeClusterClock(),
				entryToken)
			require.ErrorIs(t, err, other)
			require.ErrorContains(t, err, "create EKS access entry demo/"+entryPrincipal)
		})
	}
	t.Run("budget", func(t *testing.T) {
		clock := newFakeClusterClock()
		client := newEntryScript(t)
		for range 16 {
			client.calls = append(client.calls, entryCall{request: entryCreateRequest(), err: transient})
		}
		_, err := entryInput().createAccessEntry(t.Context(), client, clock, entryToken)
		require.ErrorContains(t, err, "timed out")
		assert.ErrorIs(t, err, transient)
		assert.Equal(t, 2*time.Minute, clock.elapsed())
	})
	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		clock := cancelEntryRetryClock{newFakeClusterClock(), cancel}
		client := newEntryScript(t, entryCall{request: entryCreateRequest(), err: transient})
		_, err := entryInput().createAccessEntry(ctx, client, clock, entryToken)
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "create EKS access entry demo/"+entryPrincipal)
	})
}

func TestAccessEntryReadAndCreateDescribeFailures(t *testing.T) {
	missing := fmt.Errorf("wrapped: %w", &ekstypes.ResourceNotFoundException{})
	denied := &ekstypes.AccessDeniedException{}
	for _, tc := range []struct {
		name  string
		reply *eks.DescribeAccessEntryOutput
		err   error
		miss  bool
	}{
		{name: "nil response", miss: true},
		{name: "missing object", reply: &eks.DescribeAccessEntryOutput{}, miss: true},
		{name: "not found", err: missing, miss: true},
		{name: "ordinary error", err: denied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			describe := entryDescribeCall()
			describe.reply, describe.err = tc.reply, tc.err
			input := AccessEntryResource{ClusterName: "another", PrincipalARN: "another-principal"}
			prior := &AccessEntryResourceOutput{ClusterName: "demo", PrincipalARN: entryPrincipal}
			_, err := input.readAccessEntry(t.Context(), newEntryScript(t, describe), prior)
			if tc.miss {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
			_, err = entryInput().createAccessEntry(t.Context(),
				newEntryScript(t, entryCall{request: entryCreateRequest()}, describe),
				newFakeClusterClock(), entryToken)
			require.ErrorContains(t, err, "describe EKS access entry demo/"+entryPrincipal)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}

	describe := entryDescribeCall()
	apiEntry := describe.reply.(*eks.DescribeAccessEntryOutput).AccessEntry
	apiEntry.KubernetesGroups = nil
	apiEntry.ModifiedAt = aws.Time(time.Date(2026, 9, 2, 3, 4, 59, 999, time.UTC))
	result, err := entryInput().readAccessEntry(t.Context(), newEntryScript(t, describe), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{}, result.KubernetesGroups)
	assert.Equal(t, "2026-09-02T03:04:59Z", result.ModifiedAt)
}

func TestAccessEntryUpdateMembership(t *testing.T) {
	for _, tc := range []struct {
		name       string
		groups     *[]string
		user       *string
		fallback   bool
		wantGroups []string
		wantUser   string
	}{
		{
			name: "groups only", groups: &[]string{"b", "", "b"}, wantGroups: []string{"b"},
			wantUser: "observed",
		},
		{
			name: "username only", user: aws.String("new"), wantGroups: []string{"live"},
			wantUser: "new",
		},
		{
			name: "stored fallback", user: aws.String("new"), fallback: true,
			wantGroups: []string{"old"}, wantUser: "new",
		},
		{
			name: "clear groups only", groups: &[]string{},
			wantGroups: []string{}, wantUser: "observed",
		},
		{
			name: "explicit clearing", groups: &[]string{}, user: aws.String(""),
			wantGroups: []string{}, wantUser: "",
		},
		{
			name: "both changed", groups: &[]string{"y", "x"}, user: aws.String("new"),
			wantGroups: []string{"x", "y"}, wantUser: "new",
		},
		{name: "relinquish both"},
		{name: "same set", groups: &[]string{"live", "live"}, user: aws.String("observed")},
		{
			name: "configured drift", groups: &[]string{"old"}, user: aws.String("stored"),
			wantGroups: []string{"old"}, wantUser: "stored",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := entryInput()
			input.ClusterName = "do-not-use-for-update"
			input.KubernetesGroups, input.UserName = tc.groups, tc.user
			prior := accessEntryPrior{
				Inputs: AccessEntryResource{
					KubernetesGroups: &[]string{"old"}, UserName: aws.String("stored"),
				},
				Outputs: &AccessEntryResourceOutput{
					ClusterName: "demo", PrincipalARN: entryPrincipal,
					KubernetesGroups: []string{"old"}, UserName: "stored",
				},
				Observed: &AccessEntryResourceOutput{
					KubernetesGroups: []string{"live"}, UserName: "observed",
				},
			}
			if tc.fallback {
				prior.Observed = nil
			}
			var calls []entryCall
			if tc.wantGroups != nil {
				calls = append(calls, entryCall{request: &eks.UpdateAccessEntryInput{
					ClusterName: aws.String("demo"), PrincipalArn: aws.String(entryPrincipal),
					KubernetesGroups: tc.wantGroups, Username: aws.String(tc.wantUser),
				}})
			}
			calls = append(calls, entryDescribeCall())
			result, err := input.updateAccessEntry(t.Context(), newEntryScript(t, calls...), prior)
			require.NoError(t, err)
			assert.Equal(t, entryARN, result.ARN)
		})
	}
}

func TestAccessEntryTagReconciliation(t *testing.T) {
	tagFailure := &ekstypes.AccessDeniedException{}
	for _, tc := range []struct {
		name     string
		desired  *map[string]string
		user     *string
		removals []string
		upsert   map[string]string
		err      error
	}{
		{
			name: "tags only",
			desired: &map[string]string{
				"add": "", "change": "new", "keep": "1", "aws:system": "ignore",
			},
			removals: []string{"remove"}, upsert: map[string]string{"add": "", "change": "new"},
		},
		{
			name: "tags before membership", desired: &map[string]string{},
			user: aws.String("editor"), removals: []string{"change", "keep", "remove"},
		},
		{
			name: "tag failure", desired: &map[string]string{}, user: aws.String("editor"),
			removals: []string{"change", "keep", "remove"}, err: tagFailure,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			liveTags := map[string]string{
				"change": "old", "keep": "1", "remove": "yes", "aws:system": "marker",
			}
			input := entryInput()
			input.Tags, input.UserName = tc.desired, tc.user
			prior := accessEntryPrior{
				Inputs: AccessEntryResource{Tags: &liveTags},
				Outputs: &AccessEntryResourceOutput{
					ClusterName: "demo", PrincipalARN: entryPrincipal, ARN: "stored",
				},
				Observed: &AccessEntryResourceOutput{
					ARN: entryARN, KubernetesGroups: []string{"group"}, UserName: "live",
				},
			}
			calls := []entryCall{
				{
					request: &eks.ListTagsForResourceInput{ResourceArn: aws.String(entryARN)},
					reply:   &eks.ListTagsForResourceOutput{Tags: liveTags},
				},
				{request: &eks.UntagResourceInput{
					ResourceArn: aws.String(entryARN), TagKeys: tc.removals,
				}, err: tc.err},
			}
			if tc.upsert != nil {
				calls = append(calls, entryCall{request: &eks.TagResourceInput{
					ResourceArn: aws.String(entryARN), Tags: tc.upsert,
				}})
			}
			if tc.err == nil {
				if tc.user != nil {
					calls = append(calls, entryCall{request: &eks.UpdateAccessEntryInput{
						ClusterName: aws.String("demo"), PrincipalArn: aws.String(entryPrincipal),
						KubernetesGroups: []string{"group"}, Username: tc.user,
					}})
				}
				calls = append(calls, entryDescribeCall())
			}
			_, err := input.updateAccessEntry(t.Context(), newEntryScript(t, calls...), prior)
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestAccessEntryUpdateDisappearance(t *testing.T) {
	input := entryInput()
	input.UserName = aws.String("new")
	prior := accessEntryPrior{Outputs: &AccessEntryResourceOutput{
		ClusterName: "demo", PrincipalARN: entryPrincipal,
	}}
	notFound := &ekstypes.ResourceNotFoundException{}
	request := &eks.UpdateAccessEntryInput{
		ClusterName: aws.String("demo"), PrincipalArn: aws.String(entryPrincipal),
		Username: aws.String("new"), KubernetesGroups: []string{},
	}
	for _, failure := range []string{"update", "describe"} {
		t.Run(failure, func(t *testing.T) {
			calls := []entryCall{{request: request, err: notFound}}
			if failure == "describe" {
				describe := entryDescribeCall()
				describe.err = notFound
				calls = []entryCall{{request: request}, describe}
			}
			_, err := input.updateAccessEntry(t.Context(), newEntryScript(t, calls...), prior)
			require.ErrorIs(t, err, notFound)
			require.ErrorContains(t, err, failure+" EKS access entry")
		})
	}
}

func TestAccessEntryDeleteAndCorruptIdentity(t *testing.T) {
	request := &eks.DeleteAccessEntryInput{
		ClusterName: aws.String("demo"), PrincipalArn: aws.String(entryPrincipal),
	}
	prior := &AccessEntryResourceOutput{ClusterName: "demo", PrincipalARN: entryPrincipal}
	denied := &ekstypes.AccessDeniedException{}
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{name: "success"},
		{name: "missing", err: fmt.Errorf("wrapped: %w", &ekstypes.ResourceNotFoundException{})},
		{name: "failure", err: denied, want: denied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := deleteAccessEntry(t.Context(),
				newEntryScript(t, entryCall{request: request, err: tc.err}), prior)
			require.ErrorIs(t, err, tc.want)
		})
	}
	for _, corrupt := range []*AccessEntryResourceOutput{
		{ClusterName: "demo"}, {PrincipalARN: entryPrincipal},
	} {
		client := newEntryScript(t)
		_, err := entryInput().readAccessEntry(t.Context(), client, corrupt)
		require.ErrorContains(t, err, "prior EKS access entry output")
		_, err = entryInput().updateAccessEntry(t.Context(), client,
			accessEntryPrior{Outputs: corrupt})
		require.ErrorContains(t, err, "prior EKS access entry output")
		require.ErrorContains(t, deleteAccessEntry(t.Context(), client, corrupt),
			"prior EKS access entry output")
	}
}
