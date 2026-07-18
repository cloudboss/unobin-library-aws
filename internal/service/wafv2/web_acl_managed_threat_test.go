package wafv2

import (
	"math"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandManagedACFPRuleSet(t *testing.T) {
	enableRegex := false
	addresses := []string{"/address/line-1", "/address/line-2"}
	phones := []string{"/phone/primary", "/phone/secondary"}
	successCodes := []int64{200, 201}
	failureCodes := []int64{400, 409}
	input := validManagedACFP()
	input.EnableRegexInPath = &enableRegex
	input.RequestInspection.AddressFields = managedIdentifierList(addresses...)
	input.RequestInspection.EmailField = managedIdentifierField("/email")
	input.RequestInspection.PasswordField = managedIdentifierField("/password")
	input.RequestInspection.PhoneNumberFields = managedIdentifierList(phones...)
	input.RequestInspection.UsernameField = managedIdentifierField("/username")
	input.ResponseInspection = &WebACLManagedResponseInspection{
		StatusCode: &WebACLManagedResponseInspectionStatusCode{
			FailureCodes: &failureCodes,
			SuccessCodes: &successCodes,
		},
	}
	expected := &awstypes.AWSManagedRulesACFPRuleSet{
		CreationPath:         aws.String("/create"),
		RegistrationPagePath: aws.String("/register"),
		RequestInspection: &awstypes.RequestInspectionACFP{
			AddressFields: []awstypes.AddressField{
				{Identifier: aws.String("/address/line-1")},
				{Identifier: aws.String("/address/line-2")},
			},
			EmailField:    &awstypes.EmailField{Identifier: aws.String("/email")},
			PasswordField: &awstypes.PasswordField{Identifier: aws.String("/password")},
			PayloadType:   awstypes.PayloadTypeJson,
			PhoneNumberFields: []awstypes.PhoneNumberField{
				{Identifier: aws.String("/phone/primary")},
				{Identifier: aws.String("/phone/secondary")},
			},
			UsernameField: &awstypes.UsernameField{Identifier: aws.String("/username")},
		},
		ResponseInspection: &awstypes.ResponseInspection{
			StatusCode: &awstypes.ResponseInspectionStatusCode{
				FailureCodes: []int32{400, 409},
				SuccessCodes: []int32{200, 201},
			},
		},
	}

	actual, err := expandManagedACFPRuleSet(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
	assert.False(t, actual.EnableRegexInPath)
}

func TestExpandManagedATPRuleSet(t *testing.T) {
	enableRegex := true
	success := []string{"accepted"}
	failure := []string{"rejected"}
	input := validManagedATP()
	input.EnableRegexInPath = &enableRegex
	input.RequestInspection = &WebACLManagedRequestInspection{
		PasswordField: managedIdentifierField("password"),
		PayloadType:   "FORM_ENCODED",
		UsernameField: managedIdentifierField("username"),
	}
	input.ResponseInspection = &WebACLManagedResponseInspection{
		Header: &WebACLManagedResponseInspectionHeader{
			FailureValues: &failure,
			Name:          "X-Login-Result",
			SuccessValues: &success,
		},
	}
	expected := &awstypes.AWSManagedRulesATPRuleSet{
		EnableRegexInPath: true,
		LoginPath:         aws.String("/login"),
		RequestInspection: &awstypes.RequestInspection{
			PasswordField: &awstypes.PasswordField{Identifier: aws.String("password")},
			PayloadType:   awstypes.PayloadTypeFormEncoded,
			UsernameField: &awstypes.UsernameField{Identifier: aws.String("username")},
		},
		ResponseInspection: &awstypes.ResponseInspection{
			Header: &awstypes.ResponseInspectionHeader{
				FailureValues: []string{"rejected"},
				Name:          aws.String("X-Login-Result"),
				SuccessValues: []string{"accepted"},
			},
		},
	}

	actual, err := expandManagedATPRuleSet(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestExpandManagedThreatConfigsCoexist(t *testing.T) {
	legacyLogin := "/legacy-login"
	configs := []WebACLManagedRuleGroupConfig{{
		AWSManagedRulesACFPRuleSet:     validManagedACFP(),
		AWSManagedRulesAntiDDoSRuleSet: validManagedAntiDDoS("DISABLED"),
		AWSManagedRulesATPRuleSet:      validManagedATP(),
		AWSManagedRulesBotControlRuleSet: &WebACLAWSManagedRulesBotControlRuleSet{
			InspectionLevel: "COMMON",
		},
		LoginPath: &legacyLogin,
	}}
	expected := []awstypes.ManagedRuleGroupConfig{{
		AWSManagedRulesACFPRuleSet: &awstypes.AWSManagedRulesACFPRuleSet{
			CreationPath:         aws.String("/create"),
			RegistrationPagePath: aws.String("/register"),
			RequestInspection: &awstypes.RequestInspectionACFP{
				PayloadType: awstypes.PayloadTypeJson,
			},
		},
		AWSManagedRulesAntiDDoSRuleSet: &awstypes.AWSManagedRulesAntiDDoSRuleSet{
			ClientSideActionConfig: &awstypes.ClientSideActionConfig{
				Challenge: &awstypes.ClientSideAction{
					UsageOfAction: awstypes.UsageOfActionDisabled,
				},
			},
		},
		AWSManagedRulesATPRuleSet: &awstypes.AWSManagedRulesATPRuleSet{
			LoginPath: aws.String("/login"),
		},
		AWSManagedRulesBotControlRuleSet: &awstypes.AWSManagedRulesBotControlRuleSet{
			InspectionLevel: awstypes.InspectionLevelCommon,
		},
		LoginPath: aws.String("/legacy-login"),
	}}

	actual, err := expandManagedRuleGroupConfigs(&configs)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestExpandManagedThreatRegexNilAndFalse(t *testing.T) {
	falseValue := false
	acfp := validManagedACFP()
	atp := validManagedATP()
	atp.EnableRegexInPath = &falseValue

	actualACFP, err := expandManagedACFPRuleSet(acfp)
	require.NoError(t, err)
	actualATP, err := expandManagedATPRuleSet(atp)
	require.NoError(t, err)
	assert.False(t, actualACFP.EnableRegexInPath)
	assert.False(t, actualATP.EnableRegexInPath)
}

func TestExpandManagedResponseInspectionVariants(t *testing.T) {
	successStrings := []string{"success"}
	failureStrings := []string{"failure"}
	successCodes := []int64{200}
	failureCodes := []int64{400}
	tests := []struct {
		name     string
		input    *WebACLManagedResponseInspection
		expected *awstypes.ResponseInspection
	}{
		{
			name: "body contains",
			input: &WebACLManagedResponseInspection{
				BodyContains: &WebACLManagedResponseInspectionBodyContains{
					FailureStrings: &failureStrings,
					SuccessStrings: &successStrings,
				},
			},
			expected: &awstypes.ResponseInspection{
				BodyContains: &awstypes.ResponseInspectionBodyContains{
					FailureStrings: []string{"failure"},
					SuccessStrings: []string{"success"},
				},
			},
		},
		{
			name: "header",
			input: &WebACLManagedResponseInspection{
				Header: &WebACLManagedResponseInspectionHeader{
					FailureValues: &failureStrings,
					Name:          "X-Result",
					SuccessValues: &successStrings,
				},
			},
			expected: &awstypes.ResponseInspection{
				Header: &awstypes.ResponseInspectionHeader{
					FailureValues: []string{"failure"},
					Name:          aws.String("X-Result"),
					SuccessValues: []string{"success"},
				},
			},
		},
		{
			name: "JSON",
			input: &WebACLManagedResponseInspection{
				JSON: &WebACLManagedResponseInspectionJSON{
					FailureValues: &failureStrings,
					Identifier:    "/result",
					SuccessValues: &successStrings,
				},
			},
			expected: &awstypes.ResponseInspection{
				Json: &awstypes.ResponseInspectionJson{
					FailureValues: []string{"failure"},
					Identifier:    aws.String("/result"),
					SuccessValues: []string{"success"},
				},
			},
		},
		{
			name: "status code",
			input: &WebACLManagedResponseInspection{
				StatusCode: &WebACLManagedResponseInspectionStatusCode{
					FailureCodes: &failureCodes,
					SuccessCodes: &successCodes,
				},
			},
			expected: &awstypes.ResponseInspection{
				StatusCode: &awstypes.ResponseInspectionStatusCode{
					FailureCodes: []int32{400},
					SuccessCodes: []int32{200},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := expandManagedResponseInspection(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestValidateManagedThreatRuleSets(t *testing.T) {
	longPath := strings.Repeat("p", 257)
	longIdentifier := strings.Repeat("i", 513)
	blank := " \t"
	tests := []struct {
		name    string
		config  WebACLManagedRuleGroupConfig
		wantErr string
	}{
		{
			name: "ACFP missing creation path",
			config: managedConfigWithACFP(func(in *WebACLAWSManagedRulesACFPRuleSet) {
				in.CreationPath = ""
			}),
			wantErr: "creation-path must be 1..256 nonblank",
		},
		{
			name: "ACFP blank registration path",
			config: managedConfigWithACFP(func(in *WebACLAWSManagedRulesACFPRuleSet) {
				in.RegistrationPagePath = blank
			}),
			wantErr: "registration-page-path must be 1..256 nonblank",
		},
		{
			name: "ACFP long creation path",
			config: managedConfigWithACFP(func(in *WebACLAWSManagedRulesACFPRuleSet) {
				in.CreationPath = longPath
			}),
			wantErr: "creation-path must be 1..256 nonblank",
		},
		{
			name: "ACFP missing request",
			config: managedConfigWithACFP(func(in *WebACLAWSManagedRulesACFPRuleSet) {
				in.RequestInspection = nil
			}),
			wantErr: "request-inspection is required",
		},
		{
			name: "ACFP invalid payload",
			config: managedConfigWithACFP(func(in *WebACLAWSManagedRulesACFPRuleSet) {
				in.RequestInspection.PayloadType = "XML"
			}),
			wantErr: "payload-type must be JSON or FORM_ENCODED",
		},
		{
			name: "ACFP empty addresses",
			config: managedConfigWithACFP(func(in *WebACLAWSManagedRulesACFPRuleSet) {
				in.RequestInspection.AddressFields = managedIdentifierList()
			}),
			wantErr: "address-fields identifiers must contain at least one",
		},
		{
			name: "ACFP empty phones",
			config: managedConfigWithACFP(func(in *WebACLAWSManagedRulesACFPRuleSet) {
				in.RequestInspection.PhoneNumberFields = managedIdentifierList()
			}),
			wantErr: "phone-number-fields identifiers must contain at least one",
		},
		{
			name: "ACFP invalid email",
			config: managedConfigWithACFP(func(in *WebACLAWSManagedRulesACFPRuleSet) {
				in.RequestInspection.EmailField = managedIdentifierField(blank)
			}),
			wantErr: "email-field identifier must be 1..512 nonblank",
		},
		{
			name: "ACFP long username",
			config: managedConfigWithACFP(func(in *WebACLAWSManagedRulesACFPRuleSet) {
				in.RequestInspection.UsernameField = managedIdentifierField(longIdentifier)
			}),
			wantErr: "username-field identifier must be 1..512 nonblank",
		},
		{
			name: "ATP missing login path",
			config: managedConfigWithATP(func(in *WebACLAWSManagedRulesATPRuleSet) {
				in.LoginPath = ""
			}),
			wantErr: "login-path must be 1..256 nonblank",
		},
		{
			name: "ATP long login path",
			config: managedConfigWithATP(func(in *WebACLAWSManagedRulesATPRuleSet) {
				in.LoginPath = longPath
			}),
			wantErr: "login-path must be 1..256 nonblank",
		},
		{
			name: "ATP request missing payload",
			config: managedConfigWithATP(func(in *WebACLAWSManagedRulesATPRuleSet) {
				in.RequestInspection = validManagedATPRequest()
				in.RequestInspection.PayloadType = ""
			}),
			wantErr: "payload-type must be JSON or FORM_ENCODED",
		},
		{
			name: "ATP request missing password",
			config: managedConfigWithATP(func(in *WebACLAWSManagedRulesATPRuleSet) {
				in.RequestInspection = validManagedATPRequest()
				in.RequestInspection.PasswordField = nil
			}),
			wantErr: "password-field is required",
		},
		{
			name: "ATP request missing username",
			config: managedConfigWithATP(func(in *WebACLAWSManagedRulesATPRuleSet) {
				in.RequestInspection = validManagedATPRequest()
				in.RequestInspection.UsernameField = nil
			}),
			wantErr: "username-field is required",
		},
		{
			name: "ATP request blank password",
			config: managedConfigWithATP(func(in *WebACLAWSManagedRulesATPRuleSet) {
				in.RequestInspection = validManagedATPRequest()
				in.RequestInspection.PasswordField = managedIdentifierField(blank)
			}),
			wantErr: "password-field identifier must be 1..512 nonblank",
		},
		{
			name: "ATP request long username",
			config: managedConfigWithATP(func(in *WebACLAWSManagedRulesATPRuleSet) {
				in.RequestInspection = validManagedATPRequest()
				in.RequestInspection.UsernameField = managedIdentifierField(longIdentifier)
			}),
			wantErr: "username-field identifier must be 1..512 nonblank",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs := []WebACLManagedRuleGroupConfig{tt.config}
			assert.ErrorContains(t, validateManagedRuleGroupConfigs(&configs), tt.wantErr)
		})
	}
}

func TestValidateManagedResponseInspectionUnionAndBounds(t *testing.T) {
	stringsSuccess := []string{"success"}
	stringsFailure := []string{"failure"}
	longIdentifier := strings.Repeat("i", 257)
	tests := []struct {
		name    string
		input   *WebACLManagedResponseInspection
		wantErr string
	}{
		{name: "zero", input: &WebACLManagedResponseInspection{}, wantErr: "exactly one"},
		{name: "multiple", input: &WebACLManagedResponseInspection{
			BodyContains: &WebACLManagedResponseInspectionBodyContains{},
			Header:       &WebACLManagedResponseInspectionHeader{},
		}, wantErr: "exactly one"},
		{name: "empty header name", input: &WebACLManagedResponseInspection{
			Header: &WebACLManagedResponseInspectionHeader{
				FailureValues: &stringsFailure,
				SuccessValues: &stringsSuccess,
			},
		}, wantErr: "header name must be 1..256"},
		{name: "long JSON identifier", input: &WebACLManagedResponseInspection{
			JSON: &WebACLManagedResponseInspectionJSON{
				FailureValues: &stringsFailure,
				Identifier:    longIdentifier,
				SuccessValues: &stringsSuccess,
			},
		}, wantErr: "JSON identifier must be 1..256"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateManagedResponseInspection(tt.input), tt.wantErr)
		})
	}
}

func TestValidateManagedResponseInspectionCollections(t *testing.T) {
	success := []string{"success"}
	failure := []string{"failure"}
	codesSuccess := []int64{200}
	codesFailure := []int64{400}
	tests := []struct {
		name    string
		input   *WebACLManagedResponseInspection
		wantErr string
	}{
		{name: "body missing success", input: &WebACLManagedResponseInspection{
			BodyContains: &WebACLManagedResponseInspectionBodyContains{
				FailureStrings: &failure,
			},
		}, wantErr: "success-strings is required"},
		{name: "header missing failure", input: &WebACLManagedResponseInspection{
			Header: &WebACLManagedResponseInspectionHeader{
				Name:          "X-Result",
				SuccessValues: &success,
			},
		}, wantErr: "failure-values is required"},
		{name: "JSON missing success", input: &WebACLManagedResponseInspection{
			JSON: &WebACLManagedResponseInspectionJSON{
				FailureValues: &failure,
				Identifier:    "/result",
			},
		}, wantErr: "success-values is required"},
		{name: "status missing failure", input: &WebACLManagedResponseInspection{
			StatusCode: &WebACLManagedResponseInspectionStatusCode{
				SuccessCodes: &codesSuccess,
			},
		}, wantErr: "failure-codes is required"},
		{name: "body duplicate across collections", input: &WebACLManagedResponseInspection{
			BodyContains: &WebACLManagedResponseInspectionBodyContains{
				FailureStrings: &success,
				SuccessStrings: &success,
			},
		}, wantErr: "unique across success and failure"},
		{name: "header duplicate in success", input: &WebACLManagedResponseInspection{
			Header: &WebACLManagedResponseInspectionHeader{
				FailureValues: &failure,
				Name:          "X-Result",
				SuccessValues: stringSlicePointer("success", "success"),
			},
		}, wantErr: "unique across success and failure"},
		{name: "JSON duplicate across collections", input: &WebACLManagedResponseInspection{
			JSON: &WebACLManagedResponseInspectionJSON{
				FailureValues: &success,
				Identifier:    "/result",
				SuccessValues: &success,
			},
		}, wantErr: "unique across success and failure"},
		{name: "status duplicate across collections", input: &WebACLManagedResponseInspection{
			StatusCode: &WebACLManagedResponseInspectionStatusCode{
				FailureCodes: &codesSuccess,
				SuccessCodes: &codesSuccess,
			},
		}, wantErr: "unique across success and failure"},
		{name: "status code overflow", input: &WebACLManagedResponseInspection{
			StatusCode: &WebACLManagedResponseInspectionStatusCode{
				FailureCodes: &codesFailure,
				SuccessCodes: int64SlicePointer(math.MaxInt32 + 1),
			},
		}, wantErr: "32-bit integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateManagedResponseInspection(tt.input), tt.wantErr)
		})
	}
}

func TestValidateManagedResponseInspectionAllowsEmptyCollections(t *testing.T) {
	emptyStrings := []string{}
	input := &WebACLManagedResponseInspection{
		BodyContains: &WebACLManagedResponseInspectionBodyContains{
			FailureStrings: &emptyStrings,
			SuccessStrings: &emptyStrings,
		},
	}
	require.NoError(t, validateManagedResponseInspection(input))

	actual, err := expandManagedResponseInspection(input)
	require.NoError(t, err)
	require.NotNil(t, actual.BodyContains.FailureStrings)
	require.NotNil(t, actual.BodyContains.SuccessStrings)
}

func validManagedACFP() *WebACLAWSManagedRulesACFPRuleSet {
	return &WebACLAWSManagedRulesACFPRuleSet{
		CreationPath:         "/create",
		RegistrationPagePath: "/register",
		RequestInspection: &WebACLManagedRequestInspectionACFP{
			PayloadType: "JSON",
		},
	}
}

func validManagedATP() *WebACLAWSManagedRulesATPRuleSet {
	return &WebACLAWSManagedRulesATPRuleSet{LoginPath: "/login"}
}

func validManagedATPRequest() *WebACLManagedRequestInspection {
	return &WebACLManagedRequestInspection{
		PasswordField: managedIdentifierField("password"),
		PayloadType:   "JSON",
		UsernameField: managedIdentifierField("username"),
	}
}

func managedConfigWithACFP(
	mutate func(*WebACLAWSManagedRulesACFPRuleSet),
) WebACLManagedRuleGroupConfig {
	in := validManagedACFP()
	mutate(in)
	return WebACLManagedRuleGroupConfig{AWSManagedRulesACFPRuleSet: in}
}

func managedConfigWithATP(
	mutate func(*WebACLAWSManagedRulesATPRuleSet),
) WebACLManagedRuleGroupConfig {
	in := validManagedATP()
	mutate(in)
	return WebACLManagedRuleGroupConfig{AWSManagedRulesATPRuleSet: in}
}

func managedIdentifierList(values ...string) *WebACLManagedRuleGroupIdentifierList {
	return &WebACLManagedRuleGroupIdentifierList{Identifiers: values}
}

func stringSlicePointer(values ...string) *[]string {
	return &values
}

func int64SlicePointer(values ...int64) *[]int64 {
	return &values
}
