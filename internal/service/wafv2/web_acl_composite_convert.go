package wafv2

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

var applicationAttributePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateApplicationConfig(config *WebACLApplicationConfig) error {
	if config == nil {
		return nil
	}
	if len(config.Attributes) < 1 || len(config.Attributes) > 10 {
		return fmt.Errorf("application-config attributes must contain 1..10 items")
	}
	for attributeIndex, attribute := range config.Attributes {
		nameLength := utf8.RuneCountInString(attribute.Name)
		if nameLength < 1 || nameLength > 64 {
			return fmt.Errorf(
				"application attribute name must be 1..64 characters at index %d",
				attributeIndex,
			)
		}
		if !applicationAttributePattern.MatchString(attribute.Name) {
			return fmt.Errorf(
				"application attribute name must match ^[A-Za-z0-9_-]+$ at index %d",
				attributeIndex,
			)
		}
		if len(attribute.Values) < 1 || len(attribute.Values) > 10 {
			return fmt.Errorf(
				"application attribute values must contain 1..10 items at index %d",
				attributeIndex,
			)
		}
		for valueIndex, value := range attribute.Values {
			valueLength := utf8.RuneCountInString(value)
			if valueLength < 1 || valueLength > 64 {
				return fmt.Errorf(
					"application attribute value must be 1..64 characters at index %d/%d",
					attributeIndex,
					valueIndex,
				)
			}
			if !applicationAttributePattern.MatchString(value) {
				return fmt.Errorf(
					"application attribute value must match ^[A-Za-z0-9_-]+$ at index %d/%d",
					attributeIndex,
					valueIndex,
				)
			}
		}
	}
	return nil
}

func expandApplicationConfig(config *WebACLApplicationConfig) *awstypes.ApplicationConfig {
	if config == nil {
		return nil
	}
	attributes := make([]awstypes.ApplicationAttribute, len(config.Attributes))
	for index, attribute := range config.Attributes {
		attributes[index] = awstypes.ApplicationAttribute{
			Name:   aws.String(attribute.Name),
			Values: append([]string(nil), attribute.Values...),
		}
	}
	return &awstypes.ApplicationConfig{Attributes: attributes}
}

func validateAssociationConfig(config *WebACLAssociationConfig) error {
	if config == nil {
		return nil
	}
	requestBody := config.RequestBody
	items := []struct {
		name   string
		config *WebACLAssociationRequestBodyConfig
	}{
		{name: "api-gateway", config: requestBody.APIGateway},
		{name: "app-runner-service", config: requestBody.AppRunnerService},
		{name: "cloudfront", config: requestBody.CloudFront},
		{name: "cognito-user-pool", config: requestBody.CognitoUserPool},
		{name: "verified-access-instance", config: requestBody.VerifiedAccessInstance},
	}
	for _, item := range items {
		if item.config == nil {
			continue
		}
		if !validSizeInspectionLimit(item.config.DefaultSizeInspectionLimit) {
			return fmt.Errorf(
				"%s default-size-inspection-limit must be one of "+
					"KB_16, KB_32, KB_48, KB_64",
				item.name,
			)
		}
	}
	return nil
}

func validSizeInspectionLimit(value string) bool {
	switch awstypes.SizeInspectionLimit(value) {
	case awstypes.SizeInspectionLimitKb16,
		awstypes.SizeInspectionLimitKb32,
		awstypes.SizeInspectionLimitKb48,
		awstypes.SizeInspectionLimitKb64:
		return true
	default:
		return false
	}
}

func expandAssociationConfig(config *WebACLAssociationConfig) *awstypes.AssociationConfig {
	if config == nil {
		return nil
	}
	requestBody := make(map[string]awstypes.RequestBodyAssociatedResourceTypeConfig)
	expandAssociationRequestBodyItem(
		requestBody,
		string(awstypes.AssociatedResourceTypeApiGateway),
		config.RequestBody.APIGateway,
	)
	expandAssociationRequestBodyItem(
		requestBody,
		string(awstypes.AssociatedResourceTypeAppRunnerService),
		config.RequestBody.AppRunnerService,
	)
	expandAssociationRequestBodyItem(
		requestBody,
		string(awstypes.AssociatedResourceTypeCloudfront),
		config.RequestBody.CloudFront,
	)
	expandAssociationRequestBodyItem(
		requestBody,
		string(awstypes.AssociatedResourceTypeCognitoUserPool),
		config.RequestBody.CognitoUserPool,
	)
	expandAssociationRequestBodyItem(
		requestBody,
		string(awstypes.AssociatedResourceTypeVerifiedAccessInstance),
		config.RequestBody.VerifiedAccessInstance,
	)
	return &awstypes.AssociationConfig{RequestBody: requestBody}
}

func expandAssociationRequestBodyItem(
	out map[string]awstypes.RequestBodyAssociatedResourceTypeConfig,
	resourceType string,
	config *WebACLAssociationRequestBodyConfig,
) {
	if config == nil {
		return
	}
	out[resourceType] = awstypes.RequestBodyAssociatedResourceTypeConfig{
		DefaultSizeInspectionLimit: awstypes.SizeInspectionLimit(
			config.DefaultSizeInspectionLimit,
		),
	}
}

func validateCustomResponseBodies(bodies *map[string]WebACLCustomResponseBody) error {
	if bodies == nil {
		return nil
	}
	for key, body := range *bodies {
		keyLength := utf8.RuneCountInString(key)
		if keyLength < 1 || keyLength > 128 {
			return fmt.Errorf("custom response body key must be 1..128 characters")
		}
		if !customResponseBodyKeyPattern.MatchString(key) {
			return fmt.Errorf("custom response body key must match ^[A-Za-z0-9_-]+$")
		}
		contentLength := utf8.RuneCountInString(body.Content)
		if contentLength < 1 || contentLength > 10240 {
			return fmt.Errorf("custom response body content must be 1..10240 characters")
		}
		switch awstypes.ResponseContentType(body.ContentType) {
		case awstypes.ResponseContentTypeTextPlain,
			awstypes.ResponseContentTypeTextHtml,
			awstypes.ResponseContentTypeApplicationJson:
		default:
			return fmt.Errorf(
				"custom response body content-type must be TEXT_PLAIN, TEXT_HTML, or " +
					"APPLICATION_JSON",
			)
		}
	}
	return nil
}

func expandCustomResponseBodies(
	bodies *map[string]WebACLCustomResponseBody,
) map[string]awstypes.CustomResponseBody {
	if bodies == nil {
		return nil
	}
	out := make(map[string]awstypes.CustomResponseBody, len(*bodies))
	for key, body := range *bodies {
		out[key] = awstypes.CustomResponseBody{
			Content:     aws.String(body.Content),
			ContentType: awstypes.ResponseContentType(body.ContentType),
		}
	}
	return out
}
