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

func TestWebACLUpdateConfigurationPreservesShieldRules(t *testing.T) {
	desiredRules := []WebACLRule{webACLUpdateTestRule("desired-rule", "desired-rule", 1)}
	resource := validWebACLResource()
	resource.Description = aws.String("changed")
	resource.Rules = &desiredRules
	prior := validWebACLUpdatePrior(*validWebACLResource())
	shieldRule := awstypes.Rule{Name: aws.String(testShieldRuleName), Priority: 2}

	expected, err := resource.updateInput(prior.Outputs)
	require.NoError(t, err)
	expected.LockToken = aws.String("token-2")
	expected.Rules = append(expected.Rules, shieldRule)

	calls := []string{}
	getCalls := 0
	client := &fakeWAFClient{
		get: func(
			ctx context.Context,
			in *awssvc.GetWebACLInput,
		) (*awssvc.GetWebACLOutput, error) {
			assert.Equal(t, &awssvc.GetWebACLInput{
				Id:    aws.String("id-1"),
				Name:  aws.String("example"),
				Scope: awstypes.ScopeRegional,
			}, in)
			getCalls++
			if getCalls == 1 {
				calls = append(calls, "get shield snapshot")
				return shieldSnapshot("token-2", []awstypes.Rule{shieldRule}), nil
			}
			calls = append(calls, "get final state")
			return successfulWebACLGet(ctx, in)
		},
		update: func(
			_ context.Context,
			in *awssvc.UpdateWebACLInput,
		) (*awssvc.UpdateWebACLOutput, error) {
			calls = append(calls, "update")
			assert.Equal(t, expected, in)
			return &awssvc.UpdateWebACLOutput{}, nil
		},
	}

	out, err := resource.update(context.Background(), client, prior)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "token-1", out.LockToken)
	assert.Equal(t, []string{"get shield snapshot", "update", "get final state"}, calls)
}

func TestWebACLUpdateConfigurationSkipsShieldGetForDesiredShieldRule(t *testing.T) {
	desiredRules := []WebACLRule{
		webACLUpdateTestRule(testShieldRuleName, "desired-shield-rule", 1),
	}
	resource := validWebACLResource()
	resource.Description = aws.String("changed")
	resource.Rules = &desiredRules
	prior := validWebACLUpdatePrior(*validWebACLResource())
	prior.Observed = &WebACLResourceOutput{
		ARN:       testWebACLARN,
		ID:        "id-1",
		LockToken: "token-observed",
	}

	expected, err := resource.updateInput(prior.Outputs)
	require.NoError(t, err)
	expected.LockToken = aws.String("token-observed")
	calls := []string{}
	updated := false
	client := &fakeWAFClient{
		get: func(
			ctx context.Context,
			in *awssvc.GetWebACLInput,
		) (*awssvc.GetWebACLOutput, error) {
			require.True(t, updated, "unexpected Shield snapshot GetWebACL call")
			calls = append(calls, "get final state")
			return successfulWebACLGet(ctx, in)
		},
		update: func(
			_ context.Context,
			in *awssvc.UpdateWebACLInput,
		) (*awssvc.UpdateWebACLOutput, error) {
			updated = true
			calls = append(calls, "update")
			assert.Equal(t, expected, in)
			return &awssvc.UpdateWebACLOutput{}, nil
		},
	}

	out, err := resource.update(context.Background(), client, prior)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, []string{"update", "get final state"}, calls)
}

func TestWebACLUpdateRejectsMismatchedObservedIdentity(t *testing.T) {
	desiredRules := []WebACLRule{
		webACLUpdateTestRule(testShieldRuleName, "desired-shield-rule", 1),
	}
	resource := validWebACLResource()
	resource.Description = aws.String("changed")
	resource.Rules = &desiredRules
	prior := validWebACLUpdatePrior(*validWebACLResource())
	prior.Observed = &WebACLResourceOutput{
		ARN: "arn:aws:wafv2:us-east-1:123456789012:" +
			"regional/webacl/other/id-2",
		ID:        "id-2",
		LockToken: "token-observed",
	}
	client := &fakeWAFClient{update: func(
		context.Context,
		*awssvc.UpdateWebACLInput,
	) (*awssvc.UpdateWebACLOutput, error) {
		t.Fatal("unexpected UpdateWebACL call")
		return nil, nil
	}}

	out, err := resource.update(context.Background(), client, prior)
	assert.Nil(t, out)
	assert.ErrorContains(t, err, "observed web ACL identity does not match prior output")
}

func webACLUpdateTestRule(name, metric string, priority int64) WebACLRule {
	return WebACLRule{
		Action:    &WebACLRuleAction{Count: &WebACLCountAction{}},
		Name:      name,
		Priority:  priority,
		Statement: ruleLeafLevel3("request:update"),
		VisibilityConfig: WebACLVisibilityConfig{
			CloudWatchMetricsEnabled: true,
			MetricName:               metric,
			SampledRequestsEnabled:   true,
		},
	}
}
