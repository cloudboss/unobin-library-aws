package autoscaling

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	smithy "github.com/aws/smithy-go"

	"github.com/cloudboss/unobin-library-aws/internal/config"
)

// tagResourceType is the only resource type the Auto Scaling tag operations
// accept; every tag the resource sends includes it alongside the group's name.
const tagResourceType = "auto-scaling-group"

// newClient returns the AWS SDK Go v2 client for Auto Scaling, configured
// from cfg. cfg is the *config.Configuration the runtime hands every
// lifecycle method; the helper unwraps it and builds an aws.Config via
// config.LoadAWSConfig.
func newClient(ctx context.Context, cfg any) (*autoscaling.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("autoscalingclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := config.LoadAWSConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	return autoscaling.NewFromConfig(awsCfg), nil
}

// region returns the region the client is configured for. A create that
// sends tags consults it to decide whether a failure means the partition
// cannot tag at create time and the create must retry untagged.
func region(client *autoscaling.Client) string {
	return client.Options().Region
}

// isValidationError reports whether err is a ValidationError. Auto Scaling
// raises it for several transient races: an instance profile or launch
// template version created moments earlier may not have propagated yet, so a
// create or update retries through it.
func isValidationError(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "ValidationError"
}

// isInvalidInstanceProfile reports whether err is the ValidationError raised
// when a launch template's IAM instance profile has not yet propagated. A
// create retries on it, and the capacity wait skips a scaling activity that
// fails with it rather than failing the whole wait, since the profile usually
// catches up.
func isInvalidInstanceProfile(err error) bool {
	return isValidationError(err) && messageContains(err, "Invalid IAM Instance Profile")
}

// isDeleteConflict reports whether a DeleteAutoScalingGroup error is one of the
// conflicts that clears once the group settles: an in-progress scaling activity
// or another operation against the group. The delete retries through both.
func isDeleteConflict(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.(type) {
	case *autoscalingtypes.ResourceInUseFault, *autoscalingtypes.ScalingActivityInProgressFault:
		return true
	}
	return false
}

// isNotFoundValidation reports whether err is a ValidationError whose message
// says the group is gone. The delete call can return this when the group has
// already been removed, which the resource treats as success.
func isNotFoundValidation(err error) bool {
	return isValidationError(err) && messageContains(err, "not found")
}

// isNotInGroup reports whether err is the ValidationError SetInstanceProtection
// raises for an instance that is no longer part of the group. Draining swallows
// it: the instance terminating out from under the protection clear is the
// outcome the drain wants.
func isNotInGroup(err error) bool {
	return isValidationError(err) && messageContains(err, "not part of Auto Scaling group")
}

// messageContains reports whether err is an AWS API error whose message
// contains substr.
func messageContains(err error, substr string) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && strings.Contains(apiErr.ErrorMessage(), substr)
}
