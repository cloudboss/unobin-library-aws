package wafv2

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandManagedRuleGroupConfigCoexistingMembers(t *testing.T) {
	loginPath := "/login"
	payloadType := "JSON"
	enableMachineLearning := false
	configs := []WebACLManagedRuleGroupConfig{{
		AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{
			EnableMachineLearning: &enableMachineLearning,
			InspectionLevel:       "TARGETED",
		},
		LoginPath:     &loginPath,
		PasswordField: &WebACLManagedRuleGroupIdentifierField{Identifier: "/password"},
		PayloadType:   &payloadType,
		UsernameField: &WebACLManagedRuleGroupIdentifierField{Identifier: "/username"},
	}}
	input := validManagedRuleGroupStatement()
	input.ManagedRuleGroupConfigs = &configs
	expected := &awstypes.ManagedRuleGroupStatement{
		ManagedRuleGroupConfigs: []awstypes.ManagedRuleGroupConfig{{
			AWSManagedRulesBotControlRuleSet: &awstypes.AWSManagedRulesBotControlRuleSet{
				EnableMachineLearning: aws.Bool(false),
				InspectionLevel:       awstypes.InspectionLevelTargeted,
			},
			LoginPath:     aws.String("/login"),
			PasswordField: &awstypes.PasswordField{Identifier: aws.String("/password")},
			PayloadType:   awstypes.PayloadTypeJson,
			UsernameField: &awstypes.UsernameField{Identifier: aws.String("/username")},
		}},
		Name:       aws.String(input.Name),
		VendorName: aws.String(input.VendorName),
	}

	actual, err := expandManagedRuleGroupStatement(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
	assert.False(t, *actual.ManagedRuleGroupConfigs[0].
		AWSManagedRulesBotControlRuleSet.EnableMachineLearning)
}

func TestExpandManagedRuleGroupConfigsPreservesItems(t *testing.T) {
	loginPath := "/login"
	payloadType := "FORM_ENCODED"
	configs := []WebACLManagedRuleGroupConfig{
		{LoginPath: &loginPath},
		{PayloadType: &payloadType},
		{PasswordField: &WebACLManagedRuleGroupIdentifierField{Identifier: "password"}},
		{UsernameField: &WebACLManagedRuleGroupIdentifierField{Identifier: "username"}},
		{AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{
			InspectionLevel: "COMMON",
		}},
		{AWSManagedRulesAntiDDoSRuleSet: validManagedAntiDDoS("DISABLED")},
		{},
	}
	expected := []awstypes.ManagedRuleGroupConfig{
		{LoginPath: aws.String("/login")},
		{PayloadType: awstypes.PayloadTypeFormEncoded},
		{PasswordField: &awstypes.PasswordField{Identifier: aws.String("password")}},
		{UsernameField: &awstypes.UsernameField{Identifier: aws.String("username")}},
		{AWSManagedRulesBotControlRuleSet: &awstypes.AWSManagedRulesBotControlRuleSet{
			InspectionLevel: awstypes.InspectionLevelCommon,
		}},
		{AWSManagedRulesAntiDDoSRuleSet: &awstypes.AWSManagedRulesAntiDDoSRuleSet{
			ClientSideActionConfig: &awstypes.ClientSideActionConfig{
				Challenge: &awstypes.ClientSideAction{
					UsageOfAction: awstypes.UsageOfActionDisabled,
				},
			},
		}},
		{},
	}

	actual, err := expandManagedRuleGroupConfigs(&configs)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
	assert.Nil(t, actual[4].AWSManagedRulesBotControlRuleSet.EnableMachineLearning)
}

func TestExpandManagedRuleGroupConfigsOmitsNilAndEmpty(t *testing.T) {
	empty := []WebACLManagedRuleGroupConfig{}
	tests := []struct {
		name  string
		input *[]WebACLManagedRuleGroupConfig
	}{
		{name: "nil"},
		{name: "empty", input: &empty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := expandManagedRuleGroupConfigs(tt.input)
			require.NoError(t, err)
			assert.Nil(t, actual)
		})
	}
}

func TestValidateManagedRuleGroupConfig(t *testing.T) {
	empty := ""
	blank := " \t\n"
	longLogin := strings.Repeat("l", 257)
	invalidPayload := "XML"
	longIdentifier := strings.Repeat("i", 513)
	enabled := true
	tests := []struct {
		name    string
		config  WebACLManagedRuleGroupConfig
		wantErr string
	}{
		{name: "empty login", config: WebACLManagedRuleGroupConfig{
			LoginPath: &empty,
		}, wantErr: "login-path must be 1..256 nonblank"},
		{name: "blank login", config: WebACLManagedRuleGroupConfig{
			LoginPath: &blank,
		}, wantErr: "login-path must be 1..256 nonblank"},
		{name: "long login", config: WebACLManagedRuleGroupConfig{
			LoginPath: &longLogin,
		}, wantErr: "login-path must be 1..256 nonblank"},
		{name: "invalid payload", config: WebACLManagedRuleGroupConfig{
			PayloadType: &invalidPayload,
		}, wantErr: "payload-type must be JSON or FORM_ENCODED"},
		{name: "empty password identifier", config: WebACLManagedRuleGroupConfig{
			PasswordField: &WebACLManagedRuleGroupIdentifierField{},
		}, wantErr: "password-field identifier must be 1..512 nonblank"},
		{name: "blank username identifier", config: WebACLManagedRuleGroupConfig{
			UsernameField: &WebACLManagedRuleGroupIdentifierField{Identifier: blank},
		}, wantErr: "username-field identifier must be 1..512 nonblank"},
		{name: "long password identifier", config: WebACLManagedRuleGroupConfig{
			PasswordField: &WebACLManagedRuleGroupIdentifierField{
				Identifier: longIdentifier,
			},
		}, wantErr: "password-field identifier must be 1..512 nonblank"},
		{name: "missing bot inspection level", config: WebACLManagedRuleGroupConfig{
			AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{},
		}, wantErr: "inspection-level must be COMMON or TARGETED"},
		{name: "invalid bot inspection level", config: WebACLManagedRuleGroupConfig{
			AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{
				InspectionLevel: "FULL",
			},
		}, wantErr: "inspection-level must be COMMON or TARGETED"},
		{name: "common machine learning enabled", config: WebACLManagedRuleGroupConfig{
			AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{
				EnableMachineLearning: &enabled,
				InspectionLevel:       "COMMON",
			},
		}, wantErr: "enable-machine-learning requires TARGETED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs := []WebACLManagedRuleGroupConfig{tt.config}
			assert.ErrorContains(t, validateManagedRuleGroupConfigs(&configs), tt.wantErr)
		})
	}
}

func TestValidateManagedRuleGroupConfigBoundaries(t *testing.T) {
	falseValue := false
	trueValue := true
	tests := []WebACLManagedRuleGroupConfig{
		{},
		{
			LoginPath:     stringPointer("x"),
			PasswordField: managedIdentifierField("x"),
			PayloadType:   stringPointer("JSON"),
			UsernameField: managedIdentifierField("x"),
		},
		{
			LoginPath:     stringPointer(strings.Repeat("l", 256)),
			PasswordField: managedIdentifierField(strings.Repeat("p", 512)),
			PayloadType:   stringPointer("FORM_ENCODED"),
			UsernameField: managedIdentifierField(strings.Repeat("u", 512)),
		},
		{AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{
			InspectionLevel: "COMMON",
		}},
		{AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{
			EnableMachineLearning: &falseValue,
			InspectionLevel:       "COMMON",
		}},
		{AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{
			EnableMachineLearning: &trueValue,
			InspectionLevel:       "TARGETED",
		}},
	}

	require.NoError(t, validateManagedRuleGroupConfigs(&tests))
}

func stringPointer(value string) *string {
	return &value
}

func managedIdentifierField(value string) *WebACLManagedRuleGroupIdentifierField {
	return &WebACLManagedRuleGroupIdentifierField{Identifier: value}
}
