package iamhelpers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	smithy "github.com/aws/smithy-go"

	"github.com/cloudboss/unobin-library-aws/library/config"
)

// NewClient returns the AWS SDK Go v2 client for iam, configured from
// cfg. cfg is the *config.Configuration the runtime hands every
// lifecycle method; the helper unwraps it and builds an aws.Config via
// config.LoadAWSConfig.
func NewClient(ctx context.Context, cfg any) (*iam.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("iamclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := config.LoadAWSConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	return iam.NewFromConfig(awsCfg), nil
}

// IsNotFound reports whether err is IAM's NoSuchEntity error. IAM models
// each failure as its own error type, so a resource Read matches the type
// to turn a read of a gone entity into runtime.ErrNotFound. This is the
// same condition the Terraform provider tests with its typed error check.
func IsNotFound(err error) bool {
	var notFound *iamtypes.NoSuchEntityException
	return errors.As(err, &notFound)
}

// Region returns the region the client is configured for. A resource reads
// it to decide partition-specific behavior, such as whether a create that
// sends tags must retry without them on a partition that cannot tag a
// resource at create time.
func Region(client *iam.Client) string {
	return client.Options().Region
}

// IsConcurrentModification reports whether err is IAM's
// ConcurrentModification error. IAM raises it when two changes to one entity
// race, such as attaching several policies to a role at once. It clears on
// its own, so a caller retries the operation.
func IsConcurrentModification(err error) bool {
	var concurrent *iamtypes.ConcurrentModificationException
	return errors.As(err, &concurrent)
}

// IsDeleteConflict reports whether err is IAM's DeleteConflict error. IAM
// raises it when a delete is refused because the entity still has something
// attached; right after detaching, the attachment can linger a moment, so a
// caller retries the delete until the view catches up.
func IsDeleteConflict(err error) bool {
	var conflict *iamtypes.DeleteConflictException
	return errors.As(err, &conflict)
}

// IsUnpropagatedPrincipal reports whether err is the malformed-policy error
// IAM returns when a trust policy names a principal that was created moments
// earlier and has not propagated. The role create or its trust-policy update
// succeeds once the principal is visible, so a caller retries.
func IsUnpropagatedPrincipal(err error) bool {
	var malformed *iamtypes.MalformedPolicyDocumentException
	return errors.As(err, &malformed) &&
		strings.Contains(malformed.ErrorMessage(), "Invalid principal in policy")
}

// IsRoleNotYetPropagated reports whether err is the transient error
// AddRoleToInstanceProfile returns when the just-created instance profile or
// its role has not propagated yet. IAM gives no clean code for this, so the
// match is on the message: an InvalidParameterValue naming the profile, or a
// NoSuchEntity naming the role. Both clear on their own, so a caller retries.
func IsRoleNotYetPropagated(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) &&
		apiErr.ErrorCode() == "InvalidParameterValue" &&
		strings.Contains(apiErr.ErrorMessage(), "Invalid IAM Instance Profile name") {
		return true
	}
	var notFound *iamtypes.NoSuchEntityException
	return errors.As(err, &notFound) &&
		strings.Contains(notFound.ErrorMessage(), "The role with name")
}
