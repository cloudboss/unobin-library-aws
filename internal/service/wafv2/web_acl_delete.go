package wafv2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awsretry "github.com/cloudboss/unobin-library-aws/internal/retry"
)

func (r *WebACLResource) delete(
	ctx context.Context,
	client wafClient,
	prior *WebACLResourceOutput,
	retryOptions ...awsretry.Option,
) error {
	identity, err := priorWebACLIdentity(prior)
	if err != nil {
		return err
	}
	if prior.LockToken == "" {
		return fmt.Errorf("prior web ACL output has no lock-token")
	}
	input := &awssvc.DeleteWebACLInput{
		Id:        aws.String(identity.ID),
		LockToken: aws.String(prior.LockToken),
		Name:      aws.String(identity.Name),
		Scope:     identity.Scope,
	}

	initialErr := deleteWebACLWithRetry(ctx, client, input, retryOptions...)
	if initialErr == nil || isWebACLNotFound(initialErr) {
		return nil
	}
	if !isWebACLLockConflict(initialErr) {
		return fmt.Errorf("delete web ACL %s: %w", identity.ID, initialErr)
	}

	refreshed, err := client.GetWebACL(ctx, &awssvc.GetWebACLInput{
		Id:    aws.String(identity.ID),
		Name:  aws.String(identity.Name),
		Scope: identity.Scope,
	})
	if err != nil {
		return fmt.Errorf("refresh web ACL %s after delete conflict: %w", identity.ID, err)
	}
	refreshedToken := ""
	if refreshed != nil {
		refreshedToken = aws.ToString(refreshed.LockToken)
	}
	if refreshedToken == "" {
		return fmt.Errorf(
			"refresh web ACL %s after delete conflict: response has no lock-token",
			identity.ID,
		)
	}
	if refreshedToken == prior.LockToken {
		return fmt.Errorf("delete web ACL %s: %w", identity.ID, initialErr)
	}

	input.LockToken = aws.String(refreshedToken)
	refreshedErr := deleteWebACLWithRetry(ctx, client, input, retryOptions...)
	if refreshedErr == nil || isWebACLNotFound(refreshedErr) {
		return nil
	}
	if isWebACLLockConflict(refreshedErr) {
		return fmt.Errorf(
			"delete web ACL %s: resource changed again after refresh; "+
				"run a new plan before applying again: %w",
			identity.ID,
			refreshedErr,
		)
	}
	return fmt.Errorf("delete web ACL %s: %w", identity.ID, refreshedErr)
}

func deleteWebACLWithRetry(
	ctx context.Context,
	client wafClient,
	input *awssvc.DeleteWebACLInput,
	retryOptions ...awsretry.Option,
) error {
	options := append(
		[]awsretry.Option{awsretry.WithTimeout(webACLRetryTimeout)},
		retryOptions...,
	)
	return awsretry.OnError(
		ctx,
		isWebACLDeleteRetryable,
		func(ctx context.Context) error {
			_, err := client.DeleteWebACL(ctx, input)
			return err
		},
		options...,
	)
}

func isWebACLDeleteRetryable(err error) bool {
	return isWebACLUnavailable(err) || isWebACLAssociated(err)
}
