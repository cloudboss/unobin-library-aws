package iam

import (
	"context"
	"errors"
	"strings"

	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for iam, configured from
// cfg. It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*iam.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return iam.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is IAM's NoSuchEntity error. IAM models
// each failure as its own error type, so a resource Read matches the type
// to turn a read of a gone entity into runtime.ErrNotFound. This is the
// same condition the Terraform provider tests with its typed error check.
func isNotFound(err error) bool {
	var notFound *iamtypes.NoSuchEntityException
	return errors.As(err, &notFound)
}

// region returns the region the client is configured for. A resource reads
// it to decide partition-specific behavior, such as whether a create that
// sends tags must retry without them on a partition that cannot tag a
// resource at create time.
func region(client *iam.Client) string {
	return client.Options().Region
}

// isConcurrentModification reports whether err is IAM's
// ConcurrentModification error. IAM raises it when two changes to one entity
// race, such as attaching several policies to a role at once. It clears on
// its own, so a caller retries the operation.
func isConcurrentModification(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) &&
		apiErr.ErrorCode() == (&iamtypes.ConcurrentModificationException{}).ErrorCode()
}

// isDeleteConflict reports whether err is IAM's DeleteConflict error. IAM
// raises it when a delete is refused because the entity still has something
// attached; right after detaching, the attachment can linger a moment, so a
// caller retries the delete until the view catches up.
func isDeleteConflict(err error) bool {
	var conflict *iamtypes.DeleteConflictException
	return errors.As(err, &conflict)
}

// isEntityTemporarilyUnmodifiable reports whether an IAM entity is briefly
// locked against updates or deletion. The condition clears on its own, so a
// caller retries the operation that hit it.
func isEntityTemporarilyUnmodifiable(err error) bool {
	var unmodifiable *iamtypes.EntityTemporarilyUnmodifiableException
	return errors.As(err, &unmodifiable)
}

// isUnpropagatedPrincipal reports whether err is the malformed-policy error
// IAM returns when a trust policy names a principal that was created moments
// earlier and has not propagated. The role create or its trust-policy update
// succeeds once the principal is visible, so a caller retries.
func isUnpropagatedPrincipal(err error) bool {
	var malformed *iamtypes.MalformedPolicyDocumentException
	return errors.As(err, &malformed) &&
		strings.Contains(malformed.ErrorMessage(), "Invalid principal in policy")
}

// isRoleNotYetPropagated reports whether err is the transient error
// AddRoleToInstanceProfile returns when the just-created instance profile or
// its role has not propagated yet. IAM gives no clean code for this, so the
// match is on the message: an InvalidParameterValue naming the profile, or a
// NoSuchEntity naming the role. Both clear on their own, so a caller retries.
func isRoleNotYetPropagated(err error) bool {
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
