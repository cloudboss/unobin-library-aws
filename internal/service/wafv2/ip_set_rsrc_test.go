package wafv2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"

	awsretry "github.com/cloudboss/unobin-library-aws/internal/retry"
)

type ipSetCall struct {
	request any
	result  any
	err     error
}

type ipSetStub struct {
	t     *testing.T
	calls []ipSetCall
}

func (s *ipSetStub) call(request any) ipSetCall {
	s.t.Helper()
	require.NotEmpty(s.t, s.calls, "unexpected %T", request)
	call := s.calls[0]
	s.calls = s.calls[1:]
	require.Equal(s.t, call.request, request)
	return call
}

func ipSetResult[T any](s *ipSetStub, request any) (*T, error) {
	s.t.Helper()
	call := s.call(request)
	result, _ := call.result.(*T)
	return result, call.err
}

func (s *ipSetStub) CreateIPSet(
	_ context.Context, in *awssvc.CreateIPSetInput, _ ...func(*awssvc.Options),
) (*awssvc.CreateIPSetOutput, error) {
	return ipSetResult[awssvc.CreateIPSetOutput](s, in)
}

func (s *ipSetStub) GetIPSet(
	_ context.Context, in *awssvc.GetIPSetInput, _ ...func(*awssvc.Options),
) (*awssvc.GetIPSetOutput, error) {
	return ipSetResult[awssvc.GetIPSetOutput](s, in)
}

func (s *ipSetStub) UpdateIPSet(
	_ context.Context, in *awssvc.UpdateIPSetInput, _ ...func(*awssvc.Options),
) (*awssvc.UpdateIPSetOutput, error) {
	return ipSetResult[awssvc.UpdateIPSetOutput](s, in)
}

func (s *ipSetStub) DeleteIPSet(
	_ context.Context, in *awssvc.DeleteIPSetInput, _ ...func(*awssvc.Options),
) (*awssvc.DeleteIPSetOutput, error) {
	return ipSetResult[awssvc.DeleteIPSetOutput](s, in)
}

func (s *ipSetStub) ListTagsForResource(
	_ context.Context, in *awssvc.ListTagsForResourceInput, _ ...func(*awssvc.Options),
) (*awssvc.ListTagsForResourceOutput, error) {
	return ipSetResult[awssvc.ListTagsForResourceOutput](s, in)
}

func (s *ipSetStub) TagResource(
	_ context.Context, in *awssvc.TagResourceInput, _ ...func(*awssvc.Options),
) (*awssvc.TagResourceOutput, error) {
	return ipSetResult[awssvc.TagResourceOutput](s, in)
}

func (s *ipSetStub) UntagResource(
	_ context.Context, in *awssvc.UntagResourceInput, _ ...func(*awssvc.Options),
) (*awssvc.UntagResourceOutput, error) {
	return ipSetResult[awssvc.UntagResourceOutput](s, in)
}

func expectIPSetCalls(t *testing.T, calls ...ipSetCall) *ipSetStub {
	t.Helper()
	stub := &ipSetStub{t: t, calls: calls}
	t.Cleanup(func() { require.Empty(t, stub.calls) })
	return stub
}

func testIPSet() IPSetResourceOutput {
	return IPSetResourceOutput{
		ID: "id", Name: "set", Scope: "REGIONAL", Region: "us-east-2", LockToken: "saved",
		ARN:              "arn:aws:wafv2:us-east-2:123456789012:regional/ipset/set/id",
		IPAddressVersion: "IPV4", Description: aws.String("old"),
		Addresses: []string{"192.0.2.0/24", "198.51.100.0/24"},
		Tags:      map[string]string{"old": "remove", "keep": "same", "edit": "old"},
	}
}

func ipSetGetCall(state IPSetResourceOutput) *awssvc.GetIPSetInput {
	return &awssvc.GetIPSetInput{
		Id: aws.String(state.ID), Name: aws.String(state.Name), Scope: awstypes.Scope(state.Scope),
	}
}

func ipSetGetResult(state IPSetResourceOutput) *awssvc.GetIPSetOutput {
	return &awssvc.GetIPSetOutput{
		IPSet: &awstypes.IPSet{
			Id: aws.String(state.ID), Name: aws.String(state.Name), ARN: aws.String(state.ARN),
			IPAddressVersion: awstypes.IPAddressVersion(state.IPAddressVersion),
			Addresses:        state.Addresses, Description: state.Description,
		},
		LockToken: aws.String(state.LockToken),
	}
}

func ipSetListCall(state IPSetResourceOutput) *awssvc.ListTagsForResourceInput {
	return &awssvc.ListTagsForResourceInput{ResourceARN: aws.String(state.ARN)}
}

func TestIPSetCreateNamesAndReadback(t *testing.T) {
	const suffix = "01010101010101010101010101"
	for _, tc := range []struct {
		name        string
		explicit    *string
		prefix      *string
		serviceName string
		readPrefix  *string
	}{
		{name: "explicit", explicit: aws.String("set"), serviceName: "set"},
		{
			name: "prefix", prefix: aws.String("pref-"),
			serviceName: "pref-" + suffix, readPrefix: aws.String("pref-"),
		},
		{
			name: "default", serviceName: "unobin-" + suffix, readPrefix: aws.String("unobin-"),
		},
		{
			name: "matching explicit", explicit: aws.String("given-" + suffix),
			serviceName: "given-" + suffix, readPrefix: aws.String("given-"),
		},
		{
			name: "suffix alone", explicit: aws.String(suffix), serviceName: suffix,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := testIPSet()
			state.Name = tc.serviceName
			state.NamePrefix = tc.readPrefix
			state.ARN = fmt.Sprintf(
				"arn:aws:wafv2:us-east-2:123456789012:regional/ipset/%s/id", state.Name,
			)
			state.LockToken = "get-token"
			state.Tags = map[string]string{"AWS:uppercase": "kept", "user": "observed"}
			observed := ipSetGetResult(state)
			observed.IPSet.Addresses = []string{"198.51.100.0/24", "192.0.2.0/24"}
			client := expectIPSetCalls(t,
				ipSetCall{
					request: &awssvc.CreateIPSetInput{
						Name: aws.String(state.Name), Scope: "REGIONAL", IPAddressVersion: "IPV4",
						Addresses:   []string{"192.0.2.1/32"},
						Description: aws.String("configured"),
						Tags:        []awstypes.Tag{{Key: aws.String("user"), Value: aws.String("desired")}},
					},
					result: &awssvc.CreateIPSetOutput{
						Summary: &awstypes.IPSetSummary{Id: aws.String(state.ID)},
					},
				},
				ipSetCall{request: ipSetGetCall(state), result: observed},
				ipSetCall{
					request: ipSetListCall(state),
					result: &awssvc.ListTagsForResourceOutput{
						NextMarker: aws.String("ignored-next-page"),
						TagInfoForResource: &awstypes.TagInfoForResource{
							TagList: expandTags(map[string]string{
								"user": "observed", "AWS:uppercase": "kept", "aws:system": "discarded",
							}),
						},
					},
				},
			)
			input := IPSetResource{
				Name: tc.explicit, NamePrefix: tc.prefix, Scope: "REGIONAL", IPAddressVersion: "IPV4",
				Description: aws.String("configured"),
				Addresses:   &[]string{"", "192.0.2.1/32", "192.0.2.1/32"},
				Tags:        &map[string]string{"user": "desired", "aws:system": "filtered"},
			}
			output, err := input.createIPSet(
				t.Context(), client, state.Region, bytes.NewReader(bytes.Repeat([]byte{1}, 13)),
			)
			require.NoError(t, err)
			require.Equal(t, &state, output)
		})
	}
}

func TestIPSetCreateFailure(t *testing.T) {
	malformed := &awstypes.WAFInvalidParameterException{Message: aws.String("malformed")}
	create := &awssvc.CreateIPSetInput{
		Name: aws.String("set"), Scope: "REGIONAL", IPAddressVersion: "IPV4",
		Addresses: []string{"bad CIDR"},
	}
	client := expectIPSetCalls(t, ipSetCall{request: create, err: malformed})
	r := IPSetResource{
		Name: create.Name, Scope: "REGIONAL", IPAddressVersion: "IPV4", Addresses: &create.Addresses,
	}
	out, err := r.createIPSet(t.Context(), client, "us-east-2", bytes.NewReader(nil))
	require.Nil(t, out)
	require.ErrorIs(t, err, malformed)
	require.ErrorContains(t, err, "create IP set set")
}

func TestIPSetReadFailures(t *testing.T) {
	state := testIPSet()
	notFound := fmt.Errorf("SDK operation: %w", &awstypes.WAFNonexistentItemException{})
	denied := &smithy.GenericAPIError{Code: "AccessDenied", Message: "policy"}
	wrongID := ipSetGetResult(state)
	wrongID.IPSet.Id = aws.String("other")
	noToken := ipSetGetResult(state)
	noToken.LockToken = nil
	for _, tc := range []struct {
		name    string
		result  *awssvc.GetIPSetOutput
		err     error
		missing bool
		message string
	}{
		{name: "typed absence", err: notFound, missing: true, message: "get IP set id"},
		{name: "nil response", missing: true, message: "response has no IP set"},
		{
			name: "absent set", result: &awssvc.GetIPSetOutput{},
			missing: true, message: "response has no IP set",
		},
		{name: "permission", err: denied, message: "get IP set id"},
		{name: "wrong identity", result: wrongID, message: "identity"},
		{name: "missing token", result: noToken, message: "lock token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			get := ipSetCall{request: ipSetGetCall(state), result: tc.result, err: tc.err}
			client := expectIPSetCalls(t, get)
			out, err := readIPSet(t.Context(), client, state, true)
			require.Nil(t, out)
			if tc.missing {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.NotErrorIs(t, err, runtime.ErrNotFound)
				require.ErrorContains(t, err, tc.message)
			}

			client = expectIPSetCalls(t, ipSetCall{
				request: &awssvc.CreateIPSetInput{
					Name: aws.String("set"), Scope: "REGIONAL", IPAddressVersion: "IPV4",
					Addresses: []string{},
				},
				result: &awssvc.CreateIPSetOutput{
					Summary: &awstypes.IPSetSummary{Id: aws.String(state.ID)},
				},
			}, get)
			input := IPSetResource{Name: aws.String("set"), Scope: "REGIONAL", IPAddressVersion: "IPV4"}
			out, err = input.createIPSet(t.Context(), client, state.Region, bytes.NewReader(nil))
			require.Nil(t, out)
			require.NotErrorIs(t, err, runtime.ErrNotFound)
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestIPSetUpdateTagsAndSpecification(t *testing.T) {
	saved := testIPSet()
	observed := saved
	observed.LockToken = "latest"
	observed.Description = aws.String("drifted")
	observed.Tags = map[string]string{
		"old": "remove", "keep": "same", "edit": "old", "aws:managed": "preserve",
	}
	for _, tc := range []struct {
		name     string
		core     bool
		observed bool
		tagErr   error
	}{
		{name: "tags then clear", core: true, observed: true},
		{name: "tags only", observed: true},
		{name: "fallback to saved", core: true},
		{name: "tag failure stops core", core: true, observed: true, tagErr: errors.New("tag error")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := saved
			prior := ipSetPrior{Outputs: &saved}
			if tc.observed {
				current = observed
				prior.Observed = &observed
			}
			out := current
			out.Tags = map[string]string{"keep": "same", "edit": "new", "new": "add"}
			desired := IPSetResource{
				Addresses:   &current.Addresses,
				Description: current.Description,
				Tags:        &out.Tags,
			}
			calls := []ipSetCall{
				{request: &awssvc.UntagResourceInput{
					ResourceARN: aws.String(saved.ARN), TagKeys: []string{"old"},
				}},
				{
					request: &awssvc.TagResourceInput{
						ResourceARN: aws.String(saved.ARN),
						Tags:        expandTags(map[string]string{"edit": "new", "new": "add"}),
					},
					err: tc.tagErr,
				},
			}
			if tc.core && tc.tagErr == nil {
				desired.Addresses = nil
				desired.Description = nil
				out.Addresses = []string{}
				out.Description = nil
				out.LockToken = "read-after-update"
				calls = append(calls, ipSetCall{request: &awssvc.UpdateIPSetInput{
					Id: aws.String(saved.ID), Name: aws.String(saved.Name), Scope: "REGIONAL",
					LockToken: aws.String(current.LockToken), Addresses: []string{},
				}})
			}
			if tc.tagErr == nil {
				calls = append(calls,
					ipSetCall{request: ipSetGetCall(saved), result: ipSetGetResult(out)},
					ipSetCall{
						request: ipSetListCall(saved),
						result: &awssvc.ListTagsForResourceOutput{
							TagInfoForResource: &awstypes.TagInfoForResource{
								TagList: expandTags(out.Tags),
							},
						},
					},
				)
			}
			got, err := desired.updateIPSet(t.Context(), expectIPSetCalls(t, calls...), prior)
			if tc.tagErr != nil {
				require.ErrorIs(t, err, tc.tagErr)
				require.ErrorContains(t, err, "tag IP set")
				return
			}
			require.NoError(t, err)
			require.Equal(t, &out, got)
		})
	}
}

func TestIPSetMutationToken(t *testing.T) {
	saved := testIPSet()
	observed := saved
	observed.LockToken = "latest"
	prior := ipSetPrior{Outputs: &saved, Observed: &observed}
	r := IPSetResource{
		Description: aws.String("changed"), Addresses: &saved.Addresses, Tags: &saved.Tags,
	}
	conflict := &awstypes.WAFOptimisticLockException{}
	client := expectIPSetCalls(t, ipSetCall{
		request: &awssvc.UpdateIPSetInput{
			Id: aws.String(saved.ID), Name: aws.String(saved.Name), Scope: "REGIONAL",
			LockToken: aws.String(observed.LockToken), Description: r.Description,
			Addresses: saved.Addresses,
		},
		err: conflict,
	})
	_, err := r.updateIPSet(t.Context(), client, prior)
	require.ErrorIs(t, err, conflict)

	observed.ID = "different"
	client = expectIPSetCalls(t)
	_, err = r.updateIPSet(t.Context(), client, prior)
	require.ErrorContains(t, err, "identity")
	require.ErrorContains(t, deleteIPSet(t.Context(), client, prior), "identity")
}

func TestIPSetDeleteRetries(t *testing.T) {
	associated := fmt.Errorf("operation: %w", &awstypes.WAFAssociatedItemException{})
	absent := fmt.Errorf("operation: %w", &awstypes.WAFNonexistentItemException{})
	lock := &awstypes.WAFOptimisticLockException{}
	unavailable := &awstypes.WAFUnavailableEntityException{}
	for _, tc := range []struct {
		name    string
		token   string
		replies []error
		want    error
		timeout time.Duration
		cancel  bool
	}{
		{
			name: "associated then success", token: "observed",
			replies: []error{associated, nil}, timeout: time.Second,
		},
		{
			name: "associated then absent", token: "observed",
			replies: []error{associated, absent}, timeout: time.Second,
		},
		{name: "persisted token", token: "saved", replies: []error{nil}},
		{name: "association expiry", token: "saved", replies: []error{associated}, want: associated},
		{
			name: "canceled", token: "saved", replies: []error{}, want: context.Canceled, cancel: true,
		},
		{name: "lock conflict", token: "saved", replies: []error{lock}, want: lock},
		{name: "unavailable", token: "saved", replies: []error{unavailable}, want: unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := testIPSet()
			observed := saved
			observed.LockToken = tc.token
			prior := ipSetPrior{Outputs: &saved}
			if tc.token != "saved" {
				prior.Observed = &observed
			}
			calls := make([]ipSetCall, 0, len(tc.replies))
			for _, err := range tc.replies {
				calls = append(calls, ipSetCall{
					request: &awssvc.DeleteIPSetInput{
						Id: aws.String(saved.ID), Name: aws.String(saved.Name), Scope: "REGIONAL",
						LockToken: aws.String(tc.token),
					},
					err: err,
				})
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			err := deleteIPSet(
				ctx, expectIPSetCalls(t, calls...), prior,
				awsretry.WithTimeout(tc.timeout), awsretry.WithInterval(time.Millisecond),
			)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestIPSetTagErrorsByPartition(t *testing.T) {
	tagError := fmt.Errorf("operation: %w", &smithy.GenericAPIError{Code: "AccessDeniedException"})
	for _, tc := range []struct {
		region, partition string
		unsupported       bool
	}{
		{region: "us-east-2", partition: "aws"},
		{region: "us-gov-east-1", partition: "aws-us-gov", unsupported: true},
	} {
		t.Run(tc.region, func(t *testing.T) {
			saved := testIPSet()
			saved.Region = tc.region
			saved.ARN = fmt.Sprintf(
				"arn:%s:wafv2:%s:123456789012:regional/ipset/set/id", tc.partition, tc.region,
			)
			list := ipSetCall{request: ipSetListCall(saved), err: tagError}
			get := ipSetCall{request: ipSetGetCall(saved), result: ipSetGetResult(saved)}
			out, err := readIPSet(t.Context(), expectIPSetCalls(t, get, list), saved, true)
			if tc.unsupported {
				require.NoError(t, err)
				require.Equal(t, map[string]string{}, out.Tags)
			} else {
				require.ErrorIs(t, err, tagError)
				require.ErrorContains(t, err, "list IP set tags "+saved.ARN)
			}

			upsert := map[string]string{"new": "tag"}
			desired := IPSetResource{
				Addresses: &saved.Addresses, Description: aws.String("updated"), Tags: &upsert,
			}
			remove := &awssvc.UntagResourceInput{
				ResourceARN: aws.String(saved.ARN), TagKeys: []string{"edit", "keep", "old"},
			}
			tag := &awssvc.TagResourceInput{
				ResourceARN: aws.String(saved.ARN), Tags: expandTags(upsert),
			}
			prior := ipSetPrior{Outputs: &saved}
			updated := saved
			updated.Description = desired.Description
			updated.LockToken = "read-after-update"
			updated.Tags = map[string]string{}
			for _, mutation := range []struct {
				operation string
				calls     []ipSetCall
			}{
				{operation: "untag", calls: []ipSetCall{{request: remove, err: tagError}}},
				{operation: "tag", calls: []ipSetCall{
					{request: remove}, {request: tag, err: tagError},
				}},
			} {
				t.Run(mutation.operation, func(t *testing.T) {
					calls := mutation.calls
					if tc.unsupported {
						calls = append(calls,
							ipSetCall{request: &awssvc.UpdateIPSetInput{
								Id: aws.String(saved.ID), Name: aws.String(saved.Name),
								Scope: "REGIONAL", LockToken: aws.String(saved.LockToken),
								Description: desired.Description, Addresses: saved.Addresses,
							}},
							ipSetCall{request: ipSetGetCall(saved), result: ipSetGetResult(updated)},
							list,
						)
					}
					got, err := desired.updateIPSet(t.Context(), expectIPSetCalls(t, calls...), prior)
					if tc.unsupported {
						require.NoError(t, err)
						require.Equal(t, &updated, got)
						return
					}
					require.Nil(t, got)
					require.ErrorIs(t, err, tagError)
					require.ErrorContains(t, err, mutation.operation+" IP set "+saved.ARN)
				})
			}
		})
	}
}

func TestIPSetPostUpdateReadFailure(t *testing.T) {
	saved := testIPSet()
	missing := &awstypes.WAFNonexistentItemException{}
	client := expectIPSetCalls(t,
		ipSetCall{request: &awssvc.UpdateIPSetInput{
			Id: aws.String(saved.ID), Name: aws.String(saved.Name), Scope: "REGIONAL",
			LockToken: aws.String(saved.LockToken), Addresses: []string{},
		}},
		ipSetCall{request: ipSetGetCall(saved), err: missing},
	)
	desired := IPSetResource{Tags: &saved.Tags}
	output, err := desired.updateIPSet(t.Context(), client, ipSetPrior{Outputs: &saved})
	require.Nil(t, output)
	require.ErrorIs(t, err, missing)
	require.NotErrorIs(t, err, runtime.ErrNotFound)
}

func TestIPSetAddressEqualityDoesNotConfuseMalformedStrings(t *testing.T) {
	require.False(t, ipSetAddressesEqual(
		[]string{"192.0.2.1/24"}, []string{"192.0.2.1|192.0.2.0/24"},
	))
}
