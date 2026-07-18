package wafv2

import (
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

func validateManagedACFPRuleSet(in *WebACLAWSManagedRulesACFPRuleSet) error {
	if in == nil {
		return fmt.Errorf("ACFP rule set is required")
	}
	if err := validateManagedConfigText("creation-path", in.CreationPath, 256); err != nil {
		return err
	}
	if err := validateManagedConfigText(
		"registration-page-path",
		in.RegistrationPagePath,
		256,
	); err != nil {
		return err
	}
	if in.RequestInspection == nil {
		return fmt.Errorf("request-inspection is required")
	}
	if err := validateManagedRequestInspectionACFP(in.RequestInspection); err != nil {
		return fmt.Errorf("request-inspection: %w", err)
	}
	if in.ResponseInspection != nil {
		if err := validateManagedResponseInspection(in.ResponseInspection); err != nil {
			return fmt.Errorf("response-inspection: %w", err)
		}
	}
	return nil
}

func expandManagedACFPRuleSet(
	in *WebACLAWSManagedRulesACFPRuleSet,
) (*awstypes.AWSManagedRulesACFPRuleSet, error) {
	if err := validateManagedACFPRuleSet(in); err != nil {
		return nil, err
	}
	request := expandManagedRequestInspectionACFP(in.RequestInspection)
	response, err := expandManagedResponseInspection(in.ResponseInspection)
	if err != nil {
		return nil, err
	}
	return &awstypes.AWSManagedRulesACFPRuleSet{
		CreationPath:         aws.String(in.CreationPath),
		EnableRegexInPath:    in.EnableRegexInPath != nil && *in.EnableRegexInPath,
		RegistrationPagePath: aws.String(in.RegistrationPagePath),
		RequestInspection:    request,
		ResponseInspection:   response,
	}, nil
}

func validateManagedATPRuleSet(in *WebACLAWSManagedRulesATPRuleSet) error {
	if in == nil {
		return fmt.Errorf("ATP rule set is required")
	}
	if err := validateManagedConfigText("login-path", in.LoginPath, 256); err != nil {
		return err
	}
	if in.RequestInspection != nil {
		if err := validateManagedRequestInspection(in.RequestInspection); err != nil {
			return fmt.Errorf("request-inspection: %w", err)
		}
	}
	if in.ResponseInspection != nil {
		if err := validateManagedResponseInspection(in.ResponseInspection); err != nil {
			return fmt.Errorf("response-inspection: %w", err)
		}
	}
	return nil
}

func expandManagedATPRuleSet(
	in *WebACLAWSManagedRulesATPRuleSet,
) (*awstypes.AWSManagedRulesATPRuleSet, error) {
	if err := validateManagedATPRuleSet(in); err != nil {
		return nil, err
	}
	response, err := expandManagedResponseInspection(in.ResponseInspection)
	if err != nil {
		return nil, err
	}
	return &awstypes.AWSManagedRulesATPRuleSet{
		EnableRegexInPath:  in.EnableRegexInPath != nil && *in.EnableRegexInPath,
		LoginPath:          aws.String(in.LoginPath),
		RequestInspection:  expandManagedRequestInspection(in.RequestInspection),
		ResponseInspection: response,
	}, nil
}

func validateManagedRequestInspectionACFP(in *WebACLManagedRequestInspectionACFP) error {
	if err := validateManagedPayloadType(in.PayloadType); err != nil {
		return err
	}
	if in.AddressFields != nil && len(in.AddressFields.Identifiers) == 0 {
		return fmt.Errorf("address-fields identifiers must contain at least one member")
	}
	if in.PhoneNumberFields != nil && len(in.PhoneNumberFields.Identifiers) == 0 {
		return fmt.Errorf("phone-number-fields identifiers must contain at least one member")
	}
	identifiers := []struct {
		field string
		value *WebACLManagedRuleGroupIdentifierField
	}{
		{field: "email-field identifier", value: in.EmailField},
		{field: "password-field identifier", value: in.PasswordField},
		{field: "username-field identifier", value: in.UsernameField},
	}
	for _, identifier := range identifiers {
		if identifier.value != nil {
			if err := validateManagedConfigText(
				identifier.field,
				identifier.value.Identifier,
				512,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func expandManagedRequestInspectionACFP(
	in *WebACLManagedRequestInspectionACFP,
) *awstypes.RequestInspectionACFP {
	out := &awstypes.RequestInspectionACFP{
		PayloadType: awstypes.PayloadType(in.PayloadType),
	}
	if in.AddressFields != nil {
		out.AddressFields = make(
			[]awstypes.AddressField,
			len(in.AddressFields.Identifiers),
		)
		for index, identifier := range in.AddressFields.Identifiers {
			out.AddressFields[index] = awstypes.AddressField{
				Identifier: aws.String(identifier),
			}
		}
	}
	if in.EmailField != nil {
		out.EmailField = &awstypes.EmailField{
			Identifier: aws.String(in.EmailField.Identifier),
		}
	}
	if in.PasswordField != nil {
		out.PasswordField = &awstypes.PasswordField{
			Identifier: aws.String(in.PasswordField.Identifier),
		}
	}
	if in.PhoneNumberFields != nil {
		out.PhoneNumberFields = make(
			[]awstypes.PhoneNumberField,
			len(in.PhoneNumberFields.Identifiers),
		)
		for index, identifier := range in.PhoneNumberFields.Identifiers {
			out.PhoneNumberFields[index] = awstypes.PhoneNumberField{
				Identifier: aws.String(identifier),
			}
		}
	}
	if in.UsernameField != nil {
		out.UsernameField = &awstypes.UsernameField{
			Identifier: aws.String(in.UsernameField.Identifier),
		}
	}
	return out
}

func validateManagedRequestInspection(in *WebACLManagedRequestInspection) error {
	if err := validateManagedPayloadType(in.PayloadType); err != nil {
		return err
	}
	if in.PasswordField == nil {
		return fmt.Errorf("password-field is required")
	}
	if err := validateManagedConfigText(
		"password-field identifier",
		in.PasswordField.Identifier,
		512,
	); err != nil {
		return err
	}
	if in.UsernameField == nil {
		return fmt.Errorf("username-field is required")
	}
	return validateManagedConfigText(
		"username-field identifier",
		in.UsernameField.Identifier,
		512,
	)
}

func expandManagedRequestInspection(
	in *WebACLManagedRequestInspection,
) *awstypes.RequestInspection {
	if in == nil {
		return nil
	}
	return &awstypes.RequestInspection{
		PasswordField: &awstypes.PasswordField{
			Identifier: aws.String(in.PasswordField.Identifier),
		},
		PayloadType: awstypes.PayloadType(in.PayloadType),
		UsernameField: &awstypes.UsernameField{
			Identifier: aws.String(in.UsernameField.Identifier),
		},
	}
}

func validateManagedPayloadType(value string) error {
	if value != string(awstypes.PayloadTypeJson) &&
		value != string(awstypes.PayloadTypeFormEncoded) {
		return fmt.Errorf("payload-type must be JSON or FORM_ENCODED")
	}
	return nil
}

func validateManagedResponseInspection(in *WebACLManagedResponseInspection) error {
	if in == nil {
		return nil
	}
	if countSet(
		in.BodyContains != nil,
		in.Header != nil,
		in.JSON != nil,
		in.StatusCode != nil,
	) != 1 {
		return fmt.Errorf("response-inspection must contain exactly one member")
	}
	switch {
	case in.BodyContains != nil:
		return validateManagedResponseStrings(
			"body-contains",
			"success-strings",
			in.BodyContains.SuccessStrings,
			"failure-strings",
			in.BodyContains.FailureStrings,
		)
	case in.Header != nil:
		if err := validateManagedResponseIdentifier("header name", in.Header.Name); err != nil {
			return err
		}
		return validateManagedResponseStrings(
			"header",
			"success-values",
			in.Header.SuccessValues,
			"failure-values",
			in.Header.FailureValues,
		)
	case in.JSON != nil:
		if err := validateManagedResponseIdentifier(
			"JSON identifier",
			in.JSON.Identifier,
		); err != nil {
			return err
		}
		return validateManagedResponseStrings(
			"JSON",
			"success-values",
			in.JSON.SuccessValues,
			"failure-values",
			in.JSON.FailureValues,
		)
	default:
		return validateManagedResponseCodes(in.StatusCode)
	}
}

func expandManagedResponseInspection(
	in *WebACLManagedResponseInspection,
) (*awstypes.ResponseInspection, error) {
	if in == nil {
		return nil, nil
	}
	if err := validateManagedResponseInspection(in); err != nil {
		return nil, err
	}
	out := &awstypes.ResponseInspection{}
	switch {
	case in.BodyContains != nil:
		out.BodyContains = &awstypes.ResponseInspectionBodyContains{
			FailureStrings: copyManagedStrings(in.BodyContains.FailureStrings),
			SuccessStrings: copyManagedStrings(in.BodyContains.SuccessStrings),
		}
	case in.Header != nil:
		out.Header = &awstypes.ResponseInspectionHeader{
			FailureValues: copyManagedStrings(in.Header.FailureValues),
			Name:          aws.String(in.Header.Name),
			SuccessValues: copyManagedStrings(in.Header.SuccessValues),
		}
	case in.JSON != nil:
		out.Json = &awstypes.ResponseInspectionJson{
			FailureValues: copyManagedStrings(in.JSON.FailureValues),
			Identifier:    aws.String(in.JSON.Identifier),
			SuccessValues: copyManagedStrings(in.JSON.SuccessValues),
		}
	case in.StatusCode != nil:
		out.StatusCode = &awstypes.ResponseInspectionStatusCode{
			FailureCodes: copyManagedCodes(in.StatusCode.FailureCodes),
			SuccessCodes: copyManagedCodes(in.StatusCode.SuccessCodes),
		}
	}
	return out, nil
}

func validateManagedResponseStrings(
	field string,
	successName string,
	success *[]string,
	failureName string,
	failure *[]string,
) error {
	if success == nil {
		return fmt.Errorf("%s is required", successName)
	}
	if failure == nil {
		return fmt.Errorf("%s is required", failureName)
	}
	return validateManagedResponseUnique(field, *success, *failure)
}

func validateManagedResponseCodes(in *WebACLManagedResponseInspectionStatusCode) error {
	if in.SuccessCodes == nil {
		return fmt.Errorf("success-codes is required")
	}
	if in.FailureCodes == nil {
		return fmt.Errorf("failure-codes is required")
	}
	for _, values := range [][]int64{*in.SuccessCodes, *in.FailureCodes} {
		for _, value := range values {
			if value < math.MinInt32 || value > math.MaxInt32 {
				return fmt.Errorf("status codes must fit a 32-bit integer")
			}
		}
	}
	return validateManagedResponseUnique(
		"status-code",
		*in.SuccessCodes,
		*in.FailureCodes,
	)
}

func validateManagedResponseUnique[T comparable](
	field string,
	success []T,
	failure []T,
) error {
	seen := make(map[T]struct{}, len(success)+len(failure))
	for _, values := range [][]T{success, failure} {
		for _, value := range values {
			if _, exists := seen[value]; exists {
				return fmt.Errorf(
					"%s values must be unique across success and failure collections",
					field,
				)
			}
			seen[value] = struct{}{}
		}
	}
	return nil
}

func validateManagedResponseIdentifier(field, value string) error {
	length := utf8.RuneCountInString(value)
	if length < 1 || length > 256 {
		return fmt.Errorf("%s must be 1..256 characters", field)
	}
	return nil
}

func copyManagedStrings(values *[]string) []string {
	out := make([]string, len(*values))
	copy(out, *values)
	return out
}

func copyManagedCodes(values *[]int64) []int32 {
	out := make([]int32, len(*values))
	for index, value := range *values {
		out[index] = int32(value)
	}
	return out
}
