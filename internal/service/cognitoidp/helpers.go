package cognitoidp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/cloudboss/unobin/pkg/awscfg"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

type awsCfg = awscfg.Configuration

type cognitoClient interface {
	AddCustomAttributes(context.Context, *cognitoidentityprovider.AddCustomAttributesInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.AddCustomAttributesOutput, error)
	CreateUserPool(context.Context, *cognitoidentityprovider.CreateUserPoolInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.CreateUserPoolOutput, error)
	DeleteUserPool(context.Context, *cognitoidentityprovider.DeleteUserPoolInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.DeleteUserPoolOutput, error)
	DescribeUserPool(context.Context, *cognitoidentityprovider.DescribeUserPoolInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.DescribeUserPoolOutput, error)
	GetUserPoolMfaConfig(context.Context, *cognitoidentityprovider.GetUserPoolMfaConfigInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.GetUserPoolMfaConfigOutput, error)
	ListTagsForResource(context.Context, *cognitoidentityprovider.ListTagsForResourceInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.ListTagsForResourceOutput, error)
	SetUserPoolMfaConfig(context.Context, *cognitoidentityprovider.SetUserPoolMfaConfigInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.SetUserPoolMfaConfigOutput, error)
	TagResource(context.Context, *cognitoidentityprovider.TagResourceInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.TagResourceOutput, error)
	UntagResource(context.Context, *cognitoidentityprovider.UntagResourceInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.UntagResourceOutput, error)
	UpdateUserPool(context.Context, *cognitoidentityprovider.UpdateUserPoolInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.UpdateUserPoolOutput, error)
}

func newClient(ctx context.Context, cfg *awsCfg) (*cognitoidentityprovider.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return cognitoidentityprovider.NewFromConfig(loaded), nil
}

func isUserPoolNotFound(err error) bool {
	var notFound *cognitotypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func isUserPoolRetryable(err error) bool {
	var trust *cognitotypes.InvalidSmsRoleTrustRelationshipException
	if errors.As(err, &trust) {
		return strings.Contains(
			trust.ErrorMessage(),
			"Role does not have a trust relationship allowing Cognito to assume the role",
		)
	}
	var policy *cognitotypes.InvalidSmsRoleAccessPolicyException
	return errors.As(err, &policy) &&
		strings.Contains(
			policy.ErrorMessage(),
			"Role does not have permission to publish with SNS",
		)
}

func cognitoProviderName(region, id string) string {
	suffix := "amazonaws.com"
	switch partition.Of(region) {
	case "aws-cn":
		suffix = "amazonaws.com.cn"
	case "aws-eusc":
		suffix = "amazonaws.eu"
	case "aws-iso":
		suffix = "c2s.ic.gov"
	case "aws-iso-b":
		suffix = "sc2s.sgov.gov"
	case "aws-iso-e":
		suffix = "cloud.adc-e.uk"
	case "aws-iso-f":
		suffix = "csp.hci.ic.gov"
	}
	return fmt.Sprintf("cognito-idp.%s.%s/%s", region, suffix, id)
}
