package wafv2

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLUpdateConflictRefreshesLockToken(t *testing.T) {
	resource, prior, expectedFirst := webACLInitialRetryFixture(t)
	expectedSecond := cloneWebACLUpdateInput(expectedFirst)
	expectedSecond.LockToken = aws.String("token-2")
	conflict := &awstypes.WAFOptimisticLockException{}
	calls := []string{}
	updateInputs := []*awssvc.UpdateWebACLInput{}
	updateCalls := 0
	getCalls := 0
	client := &fakeWAFClient{
		update: func(
			_ context.Context,
			in *awssvc.UpdateWebACLInput,
		) (*awssvc.UpdateWebACLOutput, error) {
			updateInputs = append(updateInputs, cloneWebACLUpdateInput(in))
			calls = append(calls, "update "+aws.ToString(in.LockToken))
			updateCalls++
			if updateCalls == 1 {
				return nil, conflict
			}
			return &awssvc.UpdateWebACLOutput{}, nil
		},
		get: func(
			_ context.Context,
			in *awssvc.GetWebACLInput,
		) (*awssvc.GetWebACLOutput, error) {
			assertWebACLUpdateGetIdentity(t, in)
			getCalls++
			if getCalls == 1 {
				calls = append(calls, "get refresh")
				return shieldSnapshot("token-2", nil), nil
			}
			calls = append(calls, "get final state")
			return shieldSnapshot("token-final", nil), nil
		},
	}

	out, err := resource.update(
		context.Background(),
		client,
		prior,
		fastWebACLUpdateRetryOptions()...,
	)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "token-final", out.LockToken)
	assert.Equal(t, []string{
		"update token-1",
		"get refresh",
		"update token-2",
		"get final state",
	}, calls)
	assert.Equal(t, []*awssvc.UpdateWebACLInput{expectedFirst, expectedSecond}, updateInputs)
}

func TestWebACLUpdateConflictComparesPreparedShieldToken(t *testing.T) {
	desiredRules := []WebACLRule{webACLUpdateTestRule("desired-rule", "desired-rule", 1)}
	resource := validWebACLResource()
	resource.Description = aws.String("changed")
	resource.Rules = &desiredRules
	prior := validWebACLUpdatePrior(*validWebACLResource())
	shieldRule := awstypes.Rule{Name: aws.String(testShieldRuleName), Priority: 2}

	expectedFirst, err := resource.updateInput(prior.Outputs)
	require.NoError(t, err)
	expectedFirst.LockToken = aws.String("token-A")
	expectedFirst.Rules = append(expectedFirst.Rules, shieldRule)
	expectedSecond := cloneWebACLUpdateInput(expectedFirst)
	expectedSecond.LockToken = aws.String("token-1")

	conflict := &awstypes.WAFOptimisticLockException{}
	calls := []string{}
	updateInputs := []*awssvc.UpdateWebACLInput{}
	updateCalls := 0
	getCalls := 0
	client := &fakeWAFClient{
		update: func(
			_ context.Context,
			in *awssvc.UpdateWebACLInput,
		) (*awssvc.UpdateWebACLOutput, error) {
			updateInputs = append(updateInputs, cloneWebACLUpdateInput(in))
			calls = append(calls, "update "+aws.ToString(in.LockToken))
			updateCalls++
			if updateCalls == 1 {
				return nil, conflict
			}
			return &awssvc.UpdateWebACLOutput{}, nil
		},
		get: func(
			_ context.Context,
			in *awssvc.GetWebACLInput,
		) (*awssvc.GetWebACLOutput, error) {
			assertWebACLUpdateGetIdentity(t, in)
			getCalls++
			switch getCalls {
			case 1:
				calls = append(calls, "get shield snapshot")
				return shieldSnapshot("token-A", []awstypes.Rule{shieldRule}), nil
			case 2:
				calls = append(calls, "get refresh")
				return shieldSnapshot("token-1", nil), nil
			default:
				calls = append(calls, "get final state")
				return shieldSnapshot("token-final", nil), nil
			}
		},
	}

	out, err := resource.update(
		context.Background(),
		client,
		prior,
		fastWebACLUpdateRetryOptions()...,
	)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, []string{
		"get shield snapshot",
		"update token-A",
		"get refresh",
		"update token-1",
		"get final state",
	}, calls)
	assert.Equal(t, []*awssvc.UpdateWebACLInput{expectedFirst, expectedSecond}, updateInputs)
}

func cloneWebACLUpdateInput(input *awssvc.UpdateWebACLInput) *awssvc.UpdateWebACLInput {
	copyInput := *input
	copyInput.Rules = append([]awstypes.Rule(nil), input.Rules...)
	return &copyInput
}

func assertWebACLUpdateGetIdentity(t *testing.T, input *awssvc.GetWebACLInput) {
	t.Helper()
	assert.Equal(t, &awssvc.GetWebACLInput{
		Id:    aws.String("id-1"),
		Name:  aws.String("example"),
		Scope: awstypes.ScopeRegional,
	}, input)
}
