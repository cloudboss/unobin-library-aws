package wafv2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLModifyResourcePlanMutableConfiguration(t *testing.T) {
	lockTokenUnknown := map[string]bool{"lock-token": true}
	rulesOutputsUnknown := map[string]bool{
		"application-integration-url": true,
		"capacity":                    true,
		"lock-token":                  true,
	}
	tests := []struct {
		name     string
		mutate   func(*WebACLResource)
		expected map[string]bool
	}{
		{
			name: "association config",
			mutate: func(resource *WebACLResource) {
				resource.AssociationConfig = &WebACLAssociationConfig{}
			},
			expected: lockTokenUnknown,
		},
		{
			name: "captcha config",
			mutate: func(resource *WebACLResource) {
				resource.CaptchaConfig = &WebACLCaptchaConfig{}
			},
			expected: lockTokenUnknown,
		},
		{
			name: "challenge config",
			mutate: func(resource *WebACLResource) {
				resource.ChallengeConfig = &WebACLChallengeConfig{}
			},
			expected: lockTokenUnknown,
		},
		{
			name: "custom response bodies",
			mutate: func(resource *WebACLResource) {
				bodies := map[string]WebACLCustomResponseBody{}
				resource.CustomResponseBodies = &bodies
			},
			expected: lockTokenUnknown,
		},
		{
			name: "data protection config",
			mutate: func(resource *WebACLResource) {
				resource.DataProtectionConfig = &WebACLDataProtectionConfig{}
			},
			expected: lockTokenUnknown,
		},
		{
			name: "on-source DDoS config",
			mutate: func(resource *WebACLResource) {
				resource.OnSourceDDoSConfig = &WebACLOnSourceDDoSConfig{}
			},
			expected: lockTokenUnknown,
		},
		{
			name: "default action",
			mutate: func(resource *WebACLResource) {
				resource.DefaultAction = WebACLDefaultAction{Block: &WebACLBlockAction{}}
			},
			expected: lockTokenUnknown,
		},
		{
			name: "visibility config",
			mutate: func(resource *WebACLResource) {
				resource.VisibilityConfig.MetricName = "changed"
			},
			expected: lockTokenUnknown,
		},
		{
			name: "description",
			mutate: func(resource *WebACLResource) {
				resource.Description = aws.String("changed")
			},
			expected: lockTokenUnknown,
		},
		{
			name: "rules",
			mutate: func(resource *WebACLResource) {
				rules := []WebACLRule{}
				resource.Rules = &rules
			},
			expected: rulesOutputsUnknown,
		},
		{
			name: "token domains",
			mutate: func(resource *WebACLResource) {
				domains := []string{}
				resource.TokenDomains = &domains
			},
			expected: lockTokenUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prior := *validWebACLResource()
			current := prior
			tt.mutate(&current)
			var response runtime.ResourcePlanResponse
			err := current.ModifyResourcePlan(runtime.ResourcePlanRequest[
				WebACLResource, *WebACLResourceOutput, *awsCfg,
			]{
				PriorInputs:   prior,
				CurrentInputs: current,
				PriorOutputs:  validWebACLUpdatePriorOutput(),
				HasPriorState: true,
			}, &response)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, response.UnknownOutputs)
		})
	}
}

func TestWebACLModifyResourcePlanMarksNothingForExcludedChanges(t *testing.T) {
	tests := []struct {
		name          string
		hasPriorState bool
		mutate        func(*WebACLResource)
		priorOutput   *WebACLResourceOutput
	}{
		{
			name: "no prior state",
			mutate: func(resource *WebACLResource) {
				resource.Description = aws.String("changed")
			},
		},
		{
			name:          "name replacement with mutable change",
			hasPriorState: true,
			mutate: func(resource *WebACLResource) {
				resource.Name = aws.String("replacement")
				resource.Description = aws.String("changed")
			},
		},
		{
			name:          "scope replacement with rules change",
			hasPriorState: true,
			mutate: func(resource *WebACLResource) {
				resource.Scope = "CLOUDFRONT"
				rules := []WebACLRule{}
				resource.Rules = &rules
			},
		},
		{
			name:          "application config replacement with rules change",
			hasPriorState: true,
			mutate: func(resource *WebACLResource) {
				resource.ApplicationConfig = &WebACLApplicationConfig{}
				rules := []WebACLRule{}
				resource.Rules = &rules
			},
		},
		{
			name:          "tags only",
			hasPriorState: true,
			mutate: func(resource *WebACLResource) {
				tags := map[string]string{"team": "edge"}
				resource.Tags = &tags
			},
		},
		{
			name:          "no change",
			hasPriorState: true,
			mutate:        func(*WebACLResource) {},
		},
		{
			name:          "drift only",
			hasPriorState: true,
			mutate:        func(*WebACLResource) {},
			priorOutput: &WebACLResourceOutput{
				Capacity:                  99,
				LockToken:                 "drifted-token",
				ApplicationIntegrationURL: aws.String("https://example.com/drifted"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prior := *validWebACLResource()
			current := prior
			tt.mutate(&current)
			var response runtime.ResourcePlanResponse
			err := current.ModifyResourcePlan(runtime.ResourcePlanRequest[
				WebACLResource, *WebACLResourceOutput, *awsCfg,
			]{
				PriorInputs:   prior,
				CurrentInputs: current,
				PriorOutputs:  tt.priorOutput,
				HasPriorState: tt.hasPriorState,
			}, &response)
			require.NoError(t, err)
			assert.Nil(t, response.UnknownOutputs)
		})
	}
}
