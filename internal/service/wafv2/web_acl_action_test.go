package wafv2

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRuleGroupARN = "arn:aws:wafv2:us-east-1:123456789012:regional/rulegroup/example/id-1"

func TestExpandRuleActions(t *testing.T) {
	bodyKey := "denied"
	responseHeaders := []WebACLCustomHTTPHeader{{Name: "reason", Value: "blocked"}}
	tests := []struct {
		name     string
		input    WebACLRuleAction
		expected *awstypes.RuleAction
	}{
		{
			name: "allow",
			input: WebACLRuleAction{Allow: &WebACLAllowAction{
				CustomRequestHandling: testCustomRequestHandling(),
			}},
			expected: &awstypes.RuleAction{Allow: &awstypes.AllowAction{
				CustomRequestHandling: testSDKCustomRequestHandling(),
			}},
		},
		{
			name: "block",
			input: WebACLRuleAction{Block: &WebACLBlockAction{
				CustomResponse: &WebACLCustomResponse{
					ResponseCode:          403,
					CustomResponseBodyKey: &bodyKey,
					ResponseHeaders:       &responseHeaders,
				},
			}},
			expected: &awstypes.RuleAction{Block: &awstypes.BlockAction{
				CustomResponse: &awstypes.CustomResponse{
					ResponseCode:          aws.Int32(403),
					CustomResponseBodyKey: aws.String("denied"),
					ResponseHeaders: []awstypes.CustomHTTPHeader{{
						Name:  aws.String("reason"),
						Value: aws.String("blocked"),
					}},
				},
			}},
		},
		{
			name: "captcha",
			input: WebACLRuleAction{Captcha: &WebACLCaptchaAction{
				CustomRequestHandling: testCustomRequestHandling(),
			}},
			expected: &awstypes.RuleAction{Captcha: &awstypes.CaptchaAction{
				CustomRequestHandling: testSDKCustomRequestHandling(),
			}},
		},
		{
			name: "challenge",
			input: WebACLRuleAction{Challenge: &WebACLChallengeAction{
				CustomRequestHandling: testCustomRequestHandling(),
			}},
			expected: &awstypes.RuleAction{Challenge: &awstypes.ChallengeAction{
				CustomRequestHandling: testSDKCustomRequestHandling(),
			}},
		},
		{
			name: "count",
			input: WebACLRuleAction{Count: &WebACLCountAction{
				CustomRequestHandling: testCustomRequestHandling(),
			}},
			expected: &awstypes.RuleAction{Count: &awstypes.CountAction{
				CustomRequestHandling: testSDKCustomRequestHandling(),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := expandRuleAction(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
			assert.Nil(t, actual.Monetize)
		})
	}
}

func TestValidateRuleAction(t *testing.T) {
	bodyKey := "bad key"
	tests := []struct {
		name    string
		action  WebACLRuleAction
		wantErr string
	}{
		{name: "zero", wantErr: "exactly one"},
		{
			name: "multiple",
			action: WebACLRuleAction{
				Allow: &WebACLAllowAction{},
				Count: &WebACLCountAction{},
			},
			wantErr: "exactly one",
		},
		{
			name: "missing insert header",
			action: WebACLRuleAction{Allow: &WebACLAllowAction{
				CustomRequestHandling: &WebACLCustomRequestHandling{},
			}},
			wantErr: "at least one",
		},
		{
			name: "duplicate insert header",
			action: WebACLRuleAction{Count: &WebACLCountAction{
				CustomRequestHandling: &WebACLCustomRequestHandling{
					InsertHeaders: []WebACLCustomHTTPHeader{
						{Name: "same", Value: "one"},
						{Name: "Same", Value: "two"},
					},
				},
			}},
			wantErr: "unique names",
		},
		{
			name: "response code below minimum",
			action: WebACLRuleAction{Block: &WebACLBlockAction{
				CustomResponse: &WebACLCustomResponse{ResponseCode: 199},
			}},
			wantErr: "200..600",
		},
		{
			name: "response code above maximum",
			action: WebACLRuleAction{Block: &WebACLBlockAction{
				CustomResponse: &WebACLCustomResponse{ResponseCode: 601},
			}},
			wantErr: "200..600",
		},
		{
			name: "invalid response body key",
			action: WebACLRuleAction{Block: &WebACLBlockAction{
				CustomResponse: &WebACLCustomResponse{
					ResponseCode:          403,
					CustomResponseBodyKey: &bodyKey,
				},
			}},
			wantErr: "custom-response-body-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuleAction(tt.action)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateRuleActionCompositeBounds(t *testing.T) {
	empty := ""
	longBodyKey := strings.Repeat("x", 129)
	duplicateHeaders := []WebACLCustomHTTPHeader{
		{Name: "same", Value: "one"},
		{Name: "Same", Value: "two"},
	}
	contentTypeHeaders := []WebACLCustomHTTPHeader{{
		Name:  "Content-Type",
		Value: "text/plain",
	}}
	tests := []struct {
		name    string
		action  WebACLRuleAction
		wantErr string
	}{
		{
			name: "empty insert header name",
			action: ruleActionWithInsertHeader(WebACLCustomHTTPHeader{
				Value: "value",
			}),
			wantErr: "name must be 1..64",
		},
		{
			name: "long insert header name",
			action: ruleActionWithInsertHeader(WebACLCustomHTTPHeader{
				Name:  strings.Repeat("x", 65),
				Value: "value",
			}),
			wantErr: "name must be 1..64",
		},
		{
			name: "invalid insert header name",
			action: ruleActionWithInsertHeader(WebACLCustomHTTPHeader{
				Name:  "bad name",
				Value: "value",
			}),
			wantErr: "name must be 1..64",
		},
		{
			name: "empty insert header value",
			action: ruleActionWithInsertHeader(WebACLCustomHTTPHeader{
				Name: "name",
			}),
			wantErr: "value must be 1..255",
		},
		{
			name: "long insert header value",
			action: ruleActionWithInsertHeader(WebACLCustomHTTPHeader{
				Name:  "name",
				Value: strings.Repeat("x", 256),
			}),
			wantErr: "value must be 1..255",
		},
		{
			name: "empty response body key",
			action: ruleActionWithCustomResponse(&WebACLCustomResponse{
				ResponseCode:          200,
				CustomResponseBodyKey: &empty,
			}),
			wantErr: "custom-response-body-key",
		},
		{
			name: "long response body key",
			action: ruleActionWithCustomResponse(&WebACLCustomResponse{
				ResponseCode:          200,
				CustomResponseBodyKey: &longBodyKey,
			}),
			wantErr: "custom-response-body-key",
		},
		{
			name: "duplicate response header",
			action: ruleActionWithCustomResponse(&WebACLCustomResponse{
				ResponseCode:    200,
				ResponseHeaders: &duplicateHeaders,
			}),
			wantErr: "unique names",
		},
		{
			name: "content type response header",
			action: ruleActionWithCustomResponse(&WebACLCustomResponse{
				ResponseCode:    200,
				ResponseHeaders: &contentTypeHeaders,
			}),
			wantErr: "content-type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateRuleAction(tt.action), tt.wantErr)
		})
	}
}

func TestValidateRuleActionCompositeBoundaries(t *testing.T) {
	bodyKey := strings.Repeat("b", 128)
	headers := []WebACLCustomHTTPHeader{{
		Name:  strings.Repeat("h", 64),
		Value: strings.Repeat("v", 255),
	}}
	require.NoError(t, validateRuleAction(ruleActionWithInsertHeader(headers[0])))
	for _, responseCode := range []int64{200, 600} {
		action := ruleActionWithCustomResponse(&WebACLCustomResponse{
			ResponseCode:          responseCode,
			CustomResponseBodyKey: &bodyKey,
			ResponseHeaders:       &headers,
		})
		require.NoError(t, validateRuleAction(action))
	}
}

func TestExpandOverrideActions(t *testing.T) {
	tests := []struct {
		name     string
		input    WebACLOverrideAction
		expected *awstypes.OverrideAction
	}{
		{
			name:     "count",
			input:    WebACLOverrideAction{Count: &WebACLEmpty{}},
			expected: &awstypes.OverrideAction{Count: &awstypes.CountAction{}},
		},
		{
			name:     "none",
			input:    WebACLOverrideAction{None: &WebACLNoneAction{}},
			expected: &awstypes.OverrideAction{None: &awstypes.NoneAction{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := expandOverrideAction(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestValidateOverrideActionRequiresExactlyOne(t *testing.T) {
	for _, action := range []WebACLOverrideAction{
		{},
		{Count: &WebACLEmpty{}, None: &WebACLNoneAction{}},
	} {
		assert.ErrorContains(t, validateOverrideAction(action), "exactly one")
	}
}

func TestExpandRuleGroupReferenceStatement(t *testing.T) {
	excluded := []WebACLExcludedRule{{Name: "legacy-rule"}}
	overrides := []WebACLRuleActionOverride{{
		Name:        "override-rule",
		ActionToUse: WebACLRuleAction{Count: &WebACLCountAction{}},
	}}
	input := &WebACLRuleGroupReferenceStatement{
		ARN:                 testRuleGroupARN,
		ExcludedRules:       &excluded,
		RuleActionOverrides: &overrides,
	}
	expected := &awstypes.RuleGroupReferenceStatement{
		ARN: aws.String(testRuleGroupARN),
		ExcludedRules: []awstypes.ExcludedRule{{
			Name: aws.String("legacy-rule"),
		}},
		RuleActionOverrides: []awstypes.RuleActionOverride{{
			Name: aws.String("override-rule"),
			ActionToUse: &awstypes.RuleAction{
				Count: &awstypes.CountAction{},
			},
		}},
	}

	actual, err := expandRuleGroupReferenceStatement(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)

	root, err := expandStatementLevel3(WebACLStatementLevel3{
		RuleGroupReferenceStatement: input,
	})
	require.NoError(t, err)
	assert.Equal(t, &awstypes.Statement{RuleGroupReferenceStatement: expected}, root)
}

func TestValidateRuleGroupReferenceStatement(t *testing.T) {
	longName := strings.Repeat("x", 129)
	tests := []struct {
		name    string
		input   *WebACLRuleGroupReferenceStatement
		wantErr string
	}{
		{
			name:    "missing ARN",
			input:   &WebACLRuleGroupReferenceStatement{},
			wantErr: "ARN",
		},
		{
			name: "wrong ARN resource",
			input: &WebACLRuleGroupReferenceStatement{
				ARN: testWebACLARN,
			},
			wantErr: "rule group ARN",
		},
		{
			name: "missing excluded name",
			input: ruleGroupReferenceWithExcluded(
				[]WebACLExcludedRule{{Name: ""}},
			),
			wantErr: "excluded rule name",
		},
		{
			name: "long excluded name",
			input: ruleGroupReferenceWithExcluded(
				[]WebACLExcludedRule{{Name: longName}},
			),
			wantErr: "excluded rule name",
		},
		{
			name: "invalid excluded name",
			input: ruleGroupReferenceWithExcluded(
				[]WebACLExcludedRule{{Name: "bad name"}},
			),
			wantErr: "excluded rule name",
		},
		{
			name: "duplicate excluded name",
			input: ruleGroupReferenceWithExcluded([]WebACLExcludedRule{
				{Name: "same"},
				{Name: "same"},
			}),
			wantErr: "unique",
		},
		{
			name: "too many overrides",
			input: ruleGroupReferenceWithOverrides(
				testRuleActionOverrides(101),
			),
			wantErr: "0..100",
		},
		{
			name: "missing override name",
			input: ruleGroupReferenceWithOverrides([]WebACLRuleActionOverride{{
				ActionToUse: WebACLRuleAction{Count: &WebACLCountAction{}},
			}}),
			wantErr: "override name",
		},
		{
			name: "long override name",
			input: ruleGroupReferenceWithOverrides([]WebACLRuleActionOverride{{
				Name:        longName,
				ActionToUse: WebACLRuleAction{Count: &WebACLCountAction{}},
			}}),
			wantErr: "override name",
		},
		{
			name: "duplicate override name",
			input: ruleGroupReferenceWithOverrides([]WebACLRuleActionOverride{
				{Name: "same", ActionToUse: WebACLRuleAction{Count: &WebACLCountAction{}}},
				{Name: "same", ActionToUse: WebACLRuleAction{Allow: &WebACLAllowAction{}}},
			}),
			wantErr: "unique",
		},
		{
			name: "invalid override action",
			input: ruleGroupReferenceWithOverrides([]WebACLRuleActionOverride{{
				Name: "invalid",
			}}),
			wantErr: "exactly one",
		},
		{
			name:  "100 overrides",
			input: ruleGroupReferenceWithOverrides(testRuleActionOverrides(100)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuleGroupReferenceStatement(tt.input)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestWebACLRuleActionDoesNotExposeMonetize(t *testing.T) {
	_, exists := reflect.TypeFor[WebACLRuleAction]().FieldByName("Monetize")
	assert.False(t, exists)
}

func testCustomRequestHandling() *WebACLCustomRequestHandling {
	return &WebACLCustomRequestHandling{
		InsertHeaders: []WebACLCustomHTTPHeader{{Name: "source", Value: "web-acl"}},
	}
}

func testSDKCustomRequestHandling() *awstypes.CustomRequestHandling {
	return &awstypes.CustomRequestHandling{
		InsertHeaders: []awstypes.CustomHTTPHeader{{
			Name:  aws.String("source"),
			Value: aws.String("web-acl"),
		}},
	}
}

func ruleActionWithInsertHeader(header WebACLCustomHTTPHeader) WebACLRuleAction {
	return WebACLRuleAction{Allow: &WebACLAllowAction{
		CustomRequestHandling: &WebACLCustomRequestHandling{
			InsertHeaders: []WebACLCustomHTTPHeader{header},
		},
	}}
}

func ruleActionWithCustomResponse(response *WebACLCustomResponse) WebACLRuleAction {
	return WebACLRuleAction{Block: &WebACLBlockAction{CustomResponse: response}}
}

func ruleGroupReferenceWithExcluded(
	excluded []WebACLExcludedRule,
) *WebACLRuleGroupReferenceStatement {
	return &WebACLRuleGroupReferenceStatement{
		ARN:           testRuleGroupARN,
		ExcludedRules: &excluded,
	}
}

func ruleGroupReferenceWithOverrides(
	overrides []WebACLRuleActionOverride,
) *WebACLRuleGroupReferenceStatement {
	return &WebACLRuleGroupReferenceStatement{
		ARN:                 testRuleGroupARN,
		RuleActionOverrides: &overrides,
	}
}

func testRuleActionOverrides(count int) []WebACLRuleActionOverride {
	out := make([]WebACLRuleActionOverride, count)
	for index := range count {
		out[index] = WebACLRuleActionOverride{
			Name:        fmt.Sprintf("rule-%d", index),
			ActionToUse: WebACLRuleAction{Count: &WebACLCountAction{}},
		}
	}
	return out
}
