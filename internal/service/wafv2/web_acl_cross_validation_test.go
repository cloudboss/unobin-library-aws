package wafv2

import (
	"context"
	"testing"

	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLValidateCustomResponseBodyReferences(t *testing.T) {
	locations := []struct {
		name  string
		apply func(*WebACLResource, *string)
	}{
		{name: "default block", apply: setDefaultBlockBodyReference},
		{name: "ordinary rule block", apply: addOrdinaryBlockBodyReference},
		{name: "managed group override block", apply: addManagedOverrideBodyReference},
		{name: "custom group override block", apply: addCustomOverrideBodyReference},
	}
	bodyCases := []struct {
		name    string
		bodies  *map[string]WebACLCustomResponseBody
		wantErr bool
	}{
		{name: "nil bodies", wantErr: true},
		{name: "empty bodies", bodies: customResponseBodies(), wantErr: true},
		{
			name:    "missing body",
			bodies:  customResponseBodies("other"),
			wantErr: true,
		},
		{name: "existing body", bodies: customResponseBodies("referenced")},
	}

	for _, location := range locations {
		for _, bodyCase := range bodyCases {
			t.Run(location.name+"/"+bodyCase.name, func(t *testing.T) {
				resource := crossValidationResource()
				key := "referenced"
				location.apply(resource, &key)
				resource.CustomResponseBodies = bodyCase.bodies

				err := resource.ValidateInputs(context.Background(), nil)
				if bodyCase.wantErr {
					assert.ErrorContains(
						t,
						err,
						`custom-response-body-key "referenced" does not exist`,
					)
					return
				}
				require.NoError(t, err)
			})
		}
	}
}

func TestWebACLValidateNilCustomResponseBodyKeyNeedsNoBody(t *testing.T) {
	locations := []struct {
		name  string
		apply func(*WebACLResource, *string)
	}{
		{name: "default block", apply: setDefaultBlockBodyReference},
		{name: "ordinary rule block", apply: addOrdinaryBlockBodyReference},
		{name: "managed group override block", apply: addManagedOverrideBodyReference},
		{name: "custom group override block", apply: addCustomOverrideBodyReference},
	}

	for _, location := range locations {
		t.Run(location.name, func(t *testing.T) {
			resource := crossValidationResource()
			location.apply(resource, nil)
			require.NoError(t, resource.ValidateInputs(context.Background(), nil))
		})
	}
}

func TestWebACLValidateManagedResponseInspectionScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		configs func() []WebACLManagedRuleGroupConfig
		wantErr bool
	}{
		{
			name:    "regional ACFP in later config",
			scope:   "REGIONAL",
			configs: configsWithACFPResponseInspection,
			wantErr: true,
		},
		{
			name:    "cloudfront ACFP in later config",
			scope:   "CLOUDFRONT",
			configs: configsWithACFPResponseInspection,
		},
		{
			name:    "regional ATP coexisting with ACFP",
			scope:   "REGIONAL",
			configs: configsWithATPResponseInspection,
			wantErr: true,
		},
		{
			name:    "cloudfront ATP coexisting with ACFP",
			scope:   "CLOUDFRONT",
			configs: configsWithATPResponseInspection,
		},
		{
			name:    "regional configs without response inspection",
			scope:   "REGIONAL",
			configs: configsWithoutResponseInspection,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := crossValidationResource()
			resource.Scope = tt.scope
			addManagedConfigs(resource, tt.configs())

			err := resource.ValidateInputs(context.Background(), nil)
			if tt.wantErr {
				assert.ErrorContains(
					t,
					err,
					"response-inspection requires CLOUDFRONT scope",
				)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWebACLValidateRegionalOrdinaryRules(t *testing.T) {
	resource := crossValidationResource()
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestWebACLCrossValidationRunsBeforeCreateAPI(t *testing.T) {
	tests := []struct {
		name     string
		resource func() *WebACLResource
		wantErr  string
	}{
		{
			name: "missing custom response body",
			resource: func() *WebACLResource {
				resource := crossValidationResource()
				key := "missing"
				addCustomOverrideBodyReference(resource, &key)
				return resource
			},
			wantErr: `custom-response-body-key "missing" does not exist`,
		},
		{
			name: "regional response inspection",
			resource: func() *WebACLResource {
				resource := crossValidationResource()
				addManagedConfigs(resource, configsWithATPResponseInspection())
				return resource
			},
			wantErr: "response-inspection requires CLOUDFRONT scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			client := &fakeWAFClient{create: func(
				context.Context,
				*awssvc.CreateWebACLInput,
			) (*awssvc.CreateWebACLOutput, error) {
				called = true
				return nil, assert.AnError
			}}

			_, err := tt.resource().create(context.Background(), client, nil)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.False(t, called)
		})
	}
}

func crossValidationResource() *WebACLResource {
	resource := validWebACLResource()
	rule := validWebACLRule(ruleLeafLevel3("baseline"))
	rule.Name = "baseline"
	rule.VisibilityConfig.MetricName = "baseline"
	resource.Rules = &[]WebACLRule{rule}
	return resource
}

func customResponseBodies(keys ...string) *map[string]WebACLCustomResponseBody {
	bodies := make(map[string]WebACLCustomResponseBody, len(keys))
	for _, key := range keys {
		bodies[key] = WebACLCustomResponseBody{
			Content:     "denied",
			ContentType: "TEXT_PLAIN",
		}
	}
	return &bodies
}

func customBlockAction(key *string) *WebACLBlockAction {
	return &WebACLBlockAction{CustomResponse: &WebACLCustomResponse{
		ResponseCode:          403,
		CustomResponseBodyKey: key,
	}}
}

func setDefaultBlockBodyReference(resource *WebACLResource, key *string) {
	resource.DefaultAction = WebACLDefaultAction{Block: customBlockAction(key)}
}

func addOrdinaryBlockBodyReference(resource *WebACLResource, key *string) {
	rule := validWebACLRule(ruleLeafLevel3("ordinary"))
	rule.Action = &WebACLRuleAction{Block: customBlockAction(key)}
	rule.Name = "ordinary"
	rule.Priority = 1
	rule.VisibilityConfig.MetricName = "ordinary"
	appendCrossValidationRule(resource, rule)
}

func addManagedOverrideBodyReference(resource *WebACLResource, key *string) {
	overrides := []WebACLRuleActionOverride{{
		ActionToUse: WebACLRuleAction{Block: customBlockAction(key)},
		Name:        "managed-override",
	}}
	statement := validRuleManagedGroup()
	statement.RuleActionOverrides = &overrides
	addGroupCrossValidationRule(resource, "managed", WebACLStatementLevel3{
		ManagedRuleGroupStatement: statement,
	})
}

func addCustomOverrideBodyReference(resource *WebACLResource, key *string) {
	overrides := []WebACLRuleActionOverride{{
		ActionToUse: WebACLRuleAction{Block: customBlockAction(key)},
		Name:        "custom-override",
	}}
	addGroupCrossValidationRule(resource, "custom", WebACLStatementLevel3{
		RuleGroupReferenceStatement: &WebACLRuleGroupReferenceStatement{
			ARN:                 testRuleGroupARN,
			RuleActionOverrides: &overrides,
		},
	})
}

func addGroupCrossValidationRule(
	resource *WebACLResource,
	name string,
	statement WebACLStatementLevel3,
) {
	rule := validWebACLRule(statement)
	rule.Action = nil
	rule.Name = name
	rule.OverrideAction = validRuleOverride()
	rule.Priority = 1
	rule.VisibilityConfig.MetricName = name
	appendCrossValidationRule(resource, rule)
}

func appendCrossValidationRule(resource *WebACLResource, rule WebACLRule) {
	rules := append([]WebACLRule(nil), (*resource.Rules)...)
	rules = append(rules, rule)
	resource.Rules = &rules
}

func addManagedConfigs(
	resource *WebACLResource,
	configs []WebACLManagedRuleGroupConfig,
) {
	statement := validRuleManagedGroup()
	statement.ManagedRuleGroupConfigs = &configs
	addGroupCrossValidationRule(resource, "managed", WebACLStatementLevel3{
		ManagedRuleGroupStatement: statement,
	})
}

func configsWithACFPResponseInspection() []WebACLManagedRuleGroupConfig {
	acfp := validManagedACFP()
	acfp.ResponseInspection = validManagedResponseInspection()
	return []WebACLManagedRuleGroupConfig{
		{
			AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{
				InspectionLevel: "COMMON",
			},
		},
		{AWSManagedRulesACFPRuleSet: acfp},
	}
}

func configsWithATPResponseInspection() []WebACLManagedRuleGroupConfig {
	atp := validManagedATP()
	atp.ResponseInspection = validManagedResponseInspection()
	return []WebACLManagedRuleGroupConfig{{
		AWSManagedRulesACFPRuleSet: validManagedACFP(),
		AWSManagedRulesATPRuleSet:  atp,
	}}
}

func configsWithoutResponseInspection() []WebACLManagedRuleGroupConfig {
	return []WebACLManagedRuleGroupConfig{{
		AWSManagedRulesACFPRuleSet: validManagedACFP(),
		AWSManagedRulesATPRuleSet:  validManagedATP(),
	}}
}

func validManagedResponseInspection() *WebACLManagedResponseInspection {
	success := []int64{200}
	failure := []int64{400}
	return &WebACLManagedResponseInspection{
		StatusCode: &WebACLManagedResponseInspectionStatusCode{
			FailureCodes: &failure,
			SuccessCodes: &success,
		},
	}
}
