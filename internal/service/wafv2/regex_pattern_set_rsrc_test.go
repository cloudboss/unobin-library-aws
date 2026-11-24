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
)

type regexPatternSetCall struct {
	request any
	result  any
	err     error
}

type regexPatternSetStub struct {
	t       *testing.T
	calls   []regexPatternSetCall
	observe func(context.Context, any)
}

func (s *regexPatternSetStub) call(request any) regexPatternSetCall {
	s.t.Helper()
	require.NotEmpty(s.t, s.calls, "unexpected %T", request)
	call := s.calls[0]
	s.calls = s.calls[1:]
	require.Equal(s.t, call.request, request)
	return call
}

func regexPatternSetResult[T any](
	s *regexPatternSetStub, ctx context.Context, request any,
) (*T, error) {
	s.t.Helper()
	if s.observe != nil {
		s.observe(ctx, request)
	}
	call := s.call(request)
	result, _ := call.result.(*T)
	return result, call.err
}

func (s *regexPatternSetStub) CreateRegexPatternSet(
	ctx context.Context, in *awssvc.CreateRegexPatternSetInput, _ ...func(*awssvc.Options),
) (*awssvc.CreateRegexPatternSetOutput, error) {
	return regexPatternSetResult[awssvc.CreateRegexPatternSetOutput](s, ctx, in)
}

func (s *regexPatternSetStub) GetRegexPatternSet(
	ctx context.Context, in *awssvc.GetRegexPatternSetInput, _ ...func(*awssvc.Options),
) (*awssvc.GetRegexPatternSetOutput, error) {
	return regexPatternSetResult[awssvc.GetRegexPatternSetOutput](s, ctx, in)
}

func (s *regexPatternSetStub) UpdateRegexPatternSet(
	ctx context.Context, in *awssvc.UpdateRegexPatternSetInput, _ ...func(*awssvc.Options),
) (*awssvc.UpdateRegexPatternSetOutput, error) {
	return regexPatternSetResult[awssvc.UpdateRegexPatternSetOutput](s, ctx, in)
}

func (s *regexPatternSetStub) DeleteRegexPatternSet(
	ctx context.Context, in *awssvc.DeleteRegexPatternSetInput, _ ...func(*awssvc.Options),
) (*awssvc.DeleteRegexPatternSetOutput, error) {
	return regexPatternSetResult[awssvc.DeleteRegexPatternSetOutput](s, ctx, in)
}

func (s *regexPatternSetStub) ListTagsForResource(
	ctx context.Context, in *awssvc.ListTagsForResourceInput, _ ...func(*awssvc.Options),
) (*awssvc.ListTagsForResourceOutput, error) {
	return regexPatternSetResult[awssvc.ListTagsForResourceOutput](s, ctx, in)
}

func (s *regexPatternSetStub) TagResource(
	ctx context.Context, in *awssvc.TagResourceInput, _ ...func(*awssvc.Options),
) (*awssvc.TagResourceOutput, error) {
	return regexPatternSetResult[awssvc.TagResourceOutput](s, ctx, in)
}

func (s *regexPatternSetStub) UntagResource(
	ctx context.Context, in *awssvc.UntagResourceInput, _ ...func(*awssvc.Options),
) (*awssvc.UntagResourceOutput, error) {
	return regexPatternSetResult[awssvc.UntagResourceOutput](s, ctx, in)
}

func expectRegexPatternSetCalls(t *testing.T, calls ...regexPatternSetCall) *regexPatternSetStub {
	t.Helper()
	stub := &regexPatternSetStub{t: t, calls: calls}
	t.Cleanup(func() { require.Empty(t, stub.calls) })
	return stub
}

func testRegexPatternSet() RegexPatternSetResourceOutput {
	return RegexPatternSetResourceOutput{
		ID: "id", Name: "set", Scope: "REGIONAL", Region: "us-east-2", LockToken: "saved",
		ARN:                   "arn:aws:wafv2:us-east-2:123456789012:regional/regexpatternset/set/id",
		Description:           aws.String("old"),
		RegularExpressionList: regexPatternInputs("a", "b"),
		Tags:                  map[string]string{"old": "remove", "keep": "same", "edit": "old"},
	}
}

func regexPatternSetGetCall(state RegexPatternSetResourceOutput) *awssvc.GetRegexPatternSetInput {
	return &awssvc.GetRegexPatternSetInput{
		Id: aws.String(state.ID), Name: aws.String(state.Name), Scope: awstypes.Scope(state.Scope),
	}
}

func regexPatternSetGetResult(
	state RegexPatternSetResourceOutput,
) *awssvc.GetRegexPatternSetOutput {
	return &awssvc.GetRegexPatternSetOutput{
		RegexPatternSet: &awstypes.RegexPatternSet{
			Id: aws.String(state.ID), Name: aws.String(state.Name), ARN: aws.String(state.ARN),
			RegularExpressionList: regexPatternAWSFromInput(state.RegularExpressionList),
			Description:           state.Description,
		},
		LockToken: aws.String(state.LockToken),
	}
}

func regexPatternSetListCall(state RegexPatternSetResourceOutput) *awssvc.ListTagsForResourceInput {
	return &awssvc.ListTagsForResourceInput{ResourceARN: aws.String(state.ARN)}
}

func TestRegexPatternSetCreateNamesAndReadback(t *testing.T) {
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
			state := testRegexPatternSet()
			state.Name = tc.serviceName
			state.NamePrefix = tc.readPrefix
			state.ARN = fmt.Sprintf(
				"arn:aws:wafv2:us-east-2:123456789012:regional/regexpatternset/%s/id", state.Name,
			)
			state.LockToken = "get-token"
			state.Tags = map[string]string{"AWS:uppercase": "kept", "user": "observed"}
			observed := regexPatternSetGetResult(state)
			observed.RegexPatternSet.RegularExpressionList = regexPatternAWS("b", "a", "a")
			client := expectRegexPatternSetCalls(t,
				regexPatternSetCall{
					request: &awssvc.CreateRegexPatternSetInput{
						Name: aws.String(state.Name), Scope: "REGIONAL",
						RegularExpressionList: regexPatternAWS("a+"),
						Description:           aws.String("configured"),
						Tags: []awstypes.Tag{
							{Key: aws.String("user"), Value: aws.String("desired")},
						},
					},
					result: &awssvc.CreateRegexPatternSetOutput{
						Summary: &awstypes.RegexPatternSetSummary{Id: aws.String(state.ID)},
					},
				},
				regexPatternSetCall{request: regexPatternSetGetCall(state), result: observed},
				regexPatternSetCall{
					request: regexPatternSetListCall(state),
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
			input := RegexPatternSetResource{
				Name: tc.explicit, NamePrefix: tc.prefix, Scope: "REGIONAL",
				Description:           aws.String("configured"),
				RegularExpressionList: new(regexPatternInputs("a+", "a+")),
				Tags:                  &map[string]string{"user": "desired", "aws:system": "filtered"},
			}
			output, err := input.createRegexPatternSet(
				t.Context(), client, state.Region, bytes.NewReader(bytes.Repeat([]byte{1}, 13)),
			)
			require.NoError(t, err)
			require.Equal(t, &state, output)
		})
	}
}

func TestRegexPatternSetCreateFailure(t *testing.T) {
	malformed := &awstypes.WAFInvalidParameterException{Message: aws.String("malformed")}
	create := &awssvc.CreateRegexPatternSetInput{
		Name: aws.String("set"), Scope: "REGIONAL",
		RegularExpressionList: regexPatternAWS("a"),
	}
	client := expectRegexPatternSetCalls(t, regexPatternSetCall{request: create, err: malformed})
	r := RegexPatternSetResource{
		Name: create.Name, Scope: "REGIONAL", RegularExpressionList: new(regexPatternInputs("a")),
	}
	out, err := r.createRegexPatternSet(t.Context(), client, "us-east-2", bytes.NewReader(nil))
	require.Nil(t, out)
	require.ErrorIs(t, err, malformed)
	require.ErrorContains(t, err, "create regex pattern set set")
}

func TestRegexPatternSetReadFailures(t *testing.T) {
	state := testRegexPatternSet()
	notFound := fmt.Errorf("SDK operation: %w", &awstypes.WAFNonexistentItemException{})
	denied := &smithy.GenericAPIError{Code: "AccessDenied", Message: "policy"}
	wrongID := regexPatternSetGetResult(state)
	wrongID.RegexPatternSet.Id = aws.String("other")
	noToken := regexPatternSetGetResult(state)
	noToken.LockToken = nil
	for _, tc := range []struct {
		name    string
		result  *awssvc.GetRegexPatternSetOutput
		err     error
		missing bool
		message string
	}{
		{name: "typed absence", err: notFound, missing: true, message: "get regex pattern set id"},
		{name: "nil response", missing: true, message: "response has no regex pattern set"},
		{
			name: "absent set", result: &awssvc.GetRegexPatternSetOutput{},
			missing: true, message: "response has no regex pattern set",
		},
		{name: "permission", err: denied, message: "get regex pattern set id"},
		{name: "wrong identity", result: wrongID, message: "identity"},
		{name: "missing token", result: noToken, message: "lock token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			get := regexPatternSetCall{
				request: regexPatternSetGetCall(state), result: tc.result, err: tc.err,
			}
			client := expectRegexPatternSetCalls(t, get)
			out, err := readRegexPatternSet(t.Context(), client, state, true)
			require.Nil(t, out)
			if tc.missing {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.NotErrorIs(t, err, runtime.ErrNotFound)
				require.ErrorContains(t, err, tc.message)
			}

		})
	}
}

func TestRegexPatternSetUpdateTagsAndSpecification(t *testing.T) {
	saved := testRegexPatternSet()
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
			prior := regexPatternSetPrior{Outputs: &saved}
			if tc.observed {
				current = observed
				prior.Observed = &observed
			}
			out := current
			out.Tags = map[string]string{"keep": "same", "edit": "new", "new": ""}
			desired := RegexPatternSetResource{
				RegularExpressionList: &current.RegularExpressionList,
				Description:           current.Description,
				Tags:                  &out.Tags,
			}
			calls := []regexPatternSetCall{
				{request: &awssvc.UntagResourceInput{
					ResourceARN: aws.String(saved.ARN), TagKeys: []string{"old"},
				}},
				{
					request: &awssvc.TagResourceInput{
						ResourceARN: aws.String(saved.ARN),
						Tags:        expandTags(map[string]string{"edit": "new", "new": ""}),
					},
					err: tc.tagErr,
				},
			}
			if tc.core && tc.tagErr == nil {
				desired.RegularExpressionList = nil
				desired.Description = nil
				out.RegularExpressionList = []RegexPattern{}
				out.Description = nil
				out.LockToken = "read-after-update"
				calls = append(calls, regexPatternSetCall{request: &awssvc.UpdateRegexPatternSetInput{
					Id: aws.String(saved.ID), Name: aws.String(saved.Name), Scope: "REGIONAL",
					LockToken: aws.String(current.LockToken), RegularExpressionList: []awstypes.Regex{},
				}})
			}
			if tc.tagErr == nil {
				calls = append(calls,
					regexPatternSetCall{
						request: regexPatternSetGetCall(saved), result: regexPatternSetGetResult(out),
					},
					regexPatternSetCall{
						request: regexPatternSetListCall(saved),
						result: &awssvc.ListTagsForResourceOutput{
							TagInfoForResource: &awstypes.TagInfoForResource{
								TagList: expandTags(out.Tags),
							},
						},
					},
				)
			}
			got, err := desired.updateRegexPatternSet(
				t.Context(), expectRegexPatternSetCalls(t, calls...), prior,
			)
			if tc.tagErr != nil {
				require.ErrorIs(t, err, tc.tagErr)
				require.ErrorContains(t, err, "tag regex pattern set")
				return
			}
			require.NoError(t, err)
			require.Equal(t, &out, got)
		})
	}
}

func TestRegexPatternSetMutationToken(t *testing.T) {
	saved := testRegexPatternSet()
	observed := saved
	observed.LockToken = "latest"
	prior := regexPatternSetPrior{Outputs: &saved, Observed: &observed}
	r := RegexPatternSetResource{
		Description:           aws.String("changed"),
		RegularExpressionList: &saved.RegularExpressionList, Tags: &saved.Tags,
	}
	conflict := &awstypes.WAFOptimisticLockException{}
	client := expectRegexPatternSetCalls(t, regexPatternSetCall{
		request: &awssvc.UpdateRegexPatternSetInput{
			Id: aws.String(saved.ID), Name: aws.String(saved.Name), Scope: "REGIONAL",
			LockToken: aws.String(observed.LockToken), Description: r.Description,
			RegularExpressionList: regexPatternAWSFromInput(saved.RegularExpressionList),
		},
		err: conflict,
	})
	_, err := r.updateRegexPatternSet(t.Context(), client, prior)
	require.ErrorIs(t, err, conflict)

	observed.ID = "different"
	client = expectRegexPatternSetCalls(t)
	_, err = r.updateRegexPatternSet(t.Context(), client, prior)
	require.ErrorContains(t, err, "identity")
	require.ErrorContains(t, deleteRegexPatternSet(t.Context(), client, prior), "identity")
}

func TestRegexPatternSetTagErrorsByPartition(t *testing.T) {
	tagError := fmt.Errorf("operation: %w", &smithy.GenericAPIError{Code: "AccessDeniedException"})
	for _, tc := range []struct {
		region, partition string
		unsupported       bool
	}{
		{region: "us-east-2", partition: "aws"},
		{region: "us-gov-east-1", partition: "aws-us-gov", unsupported: true},
	} {
		t.Run(tc.region, func(t *testing.T) {
			saved := testRegexPatternSet()
			saved.Region = tc.region
			saved.ARN = fmt.Sprintf(
				"arn:%s:wafv2:%s:123456789012:regional/regexpatternset/set/id", tc.partition, tc.region,
			)
			list := regexPatternSetCall{request: regexPatternSetListCall(saved), err: tagError}
			get := regexPatternSetCall{
				request: regexPatternSetGetCall(saved), result: regexPatternSetGetResult(saved),
			}
			out, err := readRegexPatternSet(
				t.Context(), expectRegexPatternSetCalls(t, get, list), saved, true,
			)
			if tc.unsupported {
				require.NoError(t, err)
				require.Equal(t, map[string]string{}, out.Tags)
			} else {
				require.ErrorIs(t, err, tagError)
				require.ErrorContains(t, err, "list regex pattern set tags "+saved.ARN)
			}

			upsert := map[string]string{"new": "tag"}
			desired := RegexPatternSetResource{
				RegularExpressionList: &saved.RegularExpressionList,
				Description:           aws.String("updated"), Tags: &upsert,
			}
			remove := &awssvc.UntagResourceInput{
				ResourceARN: aws.String(saved.ARN), TagKeys: []string{"edit", "keep", "old"},
			}
			tag := &awssvc.TagResourceInput{
				ResourceARN: aws.String(saved.ARN), Tags: expandTags(upsert),
			}
			prior := regexPatternSetPrior{Outputs: &saved}
			updated := saved
			updated.Description = desired.Description
			updated.LockToken = "read-after-update"
			updated.Tags = map[string]string{}
			for _, mutation := range []struct {
				operation string
				calls     []regexPatternSetCall
			}{
				{operation: "untag", calls: []regexPatternSetCall{{request: remove, err: tagError}}},
				{operation: "tag", calls: []regexPatternSetCall{
					{request: remove}, {request: tag, err: tagError},
				}},
			} {
				t.Run(mutation.operation, func(t *testing.T) {
					calls := mutation.calls
					if tc.unsupported {
						calls = append(calls,
							regexPatternSetCall{request: &awssvc.UpdateRegexPatternSetInput{
								Id: aws.String(saved.ID), Name: aws.String(saved.Name),
								Scope: "REGIONAL", LockToken: aws.String(saved.LockToken),
								Description:           desired.Description,
								RegularExpressionList: regexPatternAWSFromInput(saved.RegularExpressionList),
							}},
							regexPatternSetCall{
								request: regexPatternSetGetCall(saved),
								result:  regexPatternSetGetResult(updated),
							},
							list,
						)
					}
					got, err := desired.updateRegexPatternSet(
						t.Context(), expectRegexPatternSetCalls(t, calls...), prior,
					)
					if tc.unsupported {
						require.NoError(t, err)
						require.Equal(t, &updated, got)
						return
					}
					require.Nil(t, got)
					require.ErrorIs(t, err, tagError)
					require.ErrorContains(t, err, mutation.operation+" regex pattern set "+saved.ARN)
				})
			}
		})
	}
}

func TestRegexPatternSetPostUpdateReadFailure(t *testing.T) {
	saved := testRegexPatternSet()
	missing := &awstypes.WAFNonexistentItemException{}
	client := expectRegexPatternSetCalls(t,
		regexPatternSetCall{request: &awssvc.UpdateRegexPatternSetInput{
			Id: aws.String(saved.ID), Name: aws.String(saved.Name), Scope: "REGIONAL",
			LockToken: aws.String(saved.LockToken), RegularExpressionList: []awstypes.Regex{},
		}},
		regexPatternSetCall{request: regexPatternSetGetCall(saved), err: missing},
	)
	desired := RegexPatternSetResource{Tags: &saved.Tags}
	output, err := desired.updateRegexPatternSet(
		t.Context(), client, regexPatternSetPrior{Outputs: &saved},
	)
	require.Nil(t, output)
	require.ErrorIs(t, err, missing)
	require.NotErrorIs(t, err, runtime.ErrNotFound)
}

func regexPatternInputs(values ...string) []RegexPattern {
	patterns := make([]RegexPattern, 0, len(values))
	for _, value := range values {
		patterns = append(patterns, RegexPattern{RegexString: value})
	}
	return patterns
}

func regexPatternAWS(values ...string) []awstypes.Regex {
	return regexPatternAWSFromInput(regexPatternInputs(values...))
}

func regexPatternAWSFromInput(patterns []RegexPattern) []awstypes.Regex {
	result := make([]awstypes.Regex, 0, len(patterns))
	for _, pattern := range patterns {
		result = append(result, awstypes.Regex{RegexString: aws.String(pattern.RegexString)})
	}
	return result
}

func TestRegexPatternSetPreparation(t *testing.T) {
	r := RegexPatternSetResource{
		RegularExpressionList: new(regexPatternInputs("b+", "a", "b+", "a ", `a\d`)),
	}
	patterns, description, tags := r.prepareRegexPatternSet()
	require.Equal(t, regexPatternAWS("b+", "a", "a ", `a\d`), patterns)
	require.Nil(t, description)
	require.Equal(t, map[string]string{}, tags)
	patterns, _, _ = (&RegexPatternSetResource{}).prepareRegexPatternSet()
	require.Equal(t, []awstypes.Regex{}, patterns)
	_, err := (&RegexPatternSetResource{}).createRegexPatternSet(
		t.Context(), expectRegexPatternSetCalls(t), "us-east-2", bytes.NewReader(nil),
	)
	require.ErrorContains(t, err, "generate regex pattern set name")
}

func TestRegexPatternSetInitializationCleanup(t *testing.T) {
	failure := errors.New("initialization failed")
	cleanupFailure := errors.New("cleanup failed")
	for _, mode := range []string{
		"get", "missing", "nil", "empty", "identity", "arn", "token", "tags", "cancel", "join",
		"summary-token", "empty-read-token",
	} {
		t.Run(mode, func(t *testing.T) {
			state := testRegexPatternSet()
			state.LockToken = "fresh"
			get := regexPatternSetCall{
				request: regexPatternSetGetCall(state), result: regexPatternSetGetResult(state),
			}
			summaryToken := "summary"
			wantToken := summaryToken
			switch mode {
			case "get", "cancel", "join":
				get.err = failure
			case "missing":
				get.err = &awstypes.WAFNonexistentItemException{}
			case "nil":
				get.result = nil
			case "empty":
				get.result = &awssvc.GetRegexPatternSetOutput{}
			case "identity":
				get.result.(*awssvc.GetRegexPatternSetOutput).RegexPatternSet.Id = aws.String("wrong")
			case "arn":
				get.result.(*awssvc.GetRegexPatternSetOutput).RegexPatternSet.ARN = aws.String("bad")
				wantToken = state.LockToken
			case "token":
				get.result.(*awssvc.GetRegexPatternSetOutput).LockToken = nil
				summaryToken = ""
				wantToken = state.LockToken
			case "summary-token":
				get.result.(*awssvc.GetRegexPatternSetOutput).LockToken = nil
			case "empty-read-token":
				get.result.(*awssvc.GetRegexPatternSetOutput).LockToken = aws.String("")
			case "tags":
				wantToken = state.LockToken
			}
			calls := []regexPatternSetCall{{
				request: &awssvc.CreateRegexPatternSetInput{
					Name: aws.String("set"), Scope: "REGIONAL", RegularExpressionList: []awstypes.Regex{},
				},
				result: &awssvc.CreateRegexPatternSetOutput{Summary: &awstypes.RegexPatternSetSummary{
					Id: aws.String(state.ID), LockToken: aws.String(summaryToken),
				}},
			}, get}
			if mode == "tags" {
				calls = append(calls, regexPatternSetCall{
					request: regexPatternSetListCall(state), err: failure,
				})
			}
			if mode == "token" {
				calls = append(calls, regexPatternSetCall{
					request: regexPatternSetGetCall(state), result: regexPatternSetGetResult(state),
				})
			}
			deletion := regexPatternSetCall{request: &awssvc.DeleteRegexPatternSetInput{
				Id: aws.String(state.ID), Name: aws.String(state.Name), Scope: "REGIONAL",
				LockToken: aws.String(wantToken),
			}}
			if mode == "join" {
				deletion.err = cleanupFailure
			}
			calls = append(calls, deletion)
			client := expectRegexPatternSetCalls(t, calls...)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client.observe = func(callCtx context.Context, request any) {
				if _, ok := request.(*awssvc.GetRegexPatternSetInput); ok && mode == "cancel" {
					cancel()
				}
				if _, ok := request.(*awssvc.DeleteRegexPatternSetInput); ok {
					require.NoError(t, callCtx.Err())
					deadline, ok := callCtx.Deadline()
					require.True(t, ok)
					require.InDelta(t, 120, time.Until(deadline).Seconds(), 1)
				}
			}
			r := RegexPatternSetResource{Name: aws.String("set"), Scope: "REGIONAL"}
			out, err := r.createRegexPatternSet(ctx, client, state.Region, bytes.NewReader(nil))
			require.Nil(t, out)
			require.Error(t, err)
			if mode == "token" || mode == "summary-token" || mode == "empty-read-token" {
				require.ErrorContains(t, err, "response has no lock token")
			}
			if get.err == failure || mode == "tags" {
				require.ErrorIs(t, err, failure)
			}
			if mode == "join" {
				require.ErrorIs(t, err, cleanupFailure)
			}
		})
	}
}

func TestRegexPatternSetCleanupWithoutToken(t *testing.T) {
	denied := &smithy.GenericAPIError{Code: "AccessDenied", Message: "policy"}
	for _, tc := range []struct {
		name   string
		result *awssvc.GetRegexPatternSetOutput
		err    error
	}{
		{name: "lookup denied", err: denied},
		{name: "lookup lacks token", result: &awssvc.GetRegexPatternSetOutput{
			RegexPatternSet: &awstypes.RegexPatternSet{},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := testRegexPatternSet()
			identity.LockToken = ""
			client := expectRegexPatternSetCalls(t, regexPatternSetCall{
				request: regexPatternSetGetCall(identity), result: tc.result, err: tc.err,
			})
			err := cleanupRegexPatternSet(t.Context(), client, identity)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.ErrorContains(t, err, "missing lock token")
			}
		})
	}
}

func TestRegexPatternSetDeleteCadence(t *testing.T) {
	state := testRegexPatternSet()
	associated := &awstypes.WAFAssociatedItemException{}
	for _, mode := range []string{
		"success", "expiry", "exact expiry", "grace success", "canceled", "absent", "lock", "unavailable",
	} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			now := time.Unix(0, 0)
			var delays []time.Duration
			clock := regexPatternSetClock{now: func() time.Time { return now },
				sleep: func(_ context.Context, delay time.Duration) error {
					delays = append(delays, delay)
					now = now.Add(delay)
					if len(delays) == 7 {
						switch mode {
						case "expiry", "grace success":
							now = time.Unix(0, 0).Add(5*time.Minute + time.Second)
						case "exact expiry":
							now = time.Unix(0, 0).Add(5 * time.Minute)
						}
					}
					if mode == "canceled" {
						cancel()
						return ctx.Err()
					}
					return nil
				},
			}
			deleteInput := &awssvc.DeleteRegexPatternSetInput{
				Id: aws.String(state.ID), Name: aws.String(state.Name), Scope: "REGIONAL",
				LockToken: aws.String(state.LockToken),
			}
			var replies []error
			switch mode {
			case "success":
				replies = []error{associated, associated, associated, associated,
					associated, associated, associated, nil}
			case "expiry", "exact expiry":
				replies = []error{associated, associated, associated, associated,
					associated, associated, associated, associated, associated}
			case "grace success":
				replies = []error{associated, associated, associated, associated,
					associated, associated, associated, associated, nil}
			case "canceled":
				replies = []error{associated}
			case "absent":
				replies = []error{&awstypes.WAFNonexistentItemException{}}
			case "lock":
				replies = []error{&awstypes.WAFOptimisticLockException{}}
			case "unavailable":
				replies = []error{&awstypes.WAFUnavailableEntityException{}}
			}
			var calls []regexPatternSetCall
			for _, err := range replies {
				calls = append(calls, regexPatternSetCall{request: deleteInput, err: err})
			}
			err := deleteRegexPatternSet(ctx, expectRegexPatternSetCalls(t, calls...),
				regexPatternSetPrior{Outputs: &state}, clock)
			switch mode {
			case "success", "grace success", "absent":
				require.NoError(t, err)
			case "canceled":
				require.ErrorIs(t, err, context.Canceled)
			default:
				require.ErrorIs(t, err, replies[len(replies)-1])
			}
			switch mode {
			case "success":
				require.Equal(t, []time.Duration{
					500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
					8 * time.Second, 10 * time.Second, 10 * time.Second,
				}, delays)
			case "expiry", "exact expiry", "grace success":
				require.Equal(t, []time.Duration{
					500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
					8 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
				}, delays)
				elapsed := 5*time.Minute + 10*time.Second
				if mode != "exact expiry" {
					elapsed += time.Second
				}
				require.Equal(t, time.Unix(0, 0).Add(elapsed), now)
			case "canceled":
				require.Equal(t, []time.Duration{500 * time.Millisecond}, delays)
			default:
				require.Empty(t, delays)
			}
		})
	}
}
