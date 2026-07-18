package wafv2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/cloudboss/unobin/pkg/runtime"

	awsretry "github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

func (r *WebACLResource) update(
	ctx context.Context,
	client wafClient,
	prior runtime.Prior[WebACLResource, *WebACLResourceOutput],
	retryOptions ...awsretry.Option,
) (*WebACLResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	identity, err := priorWebACLIdentity(prior.Outputs)
	if err != nil {
		return nil, err
	}
	if r.configurationChanged(prior.Inputs) {
		updatePrior, err := webACLUpdatePriorOutput(prior, identity)
		if err != nil {
			return nil, err
		}
		input, err := r.updateInput(updatePrior)
		if err != nil {
			return nil, fmt.Errorf("build web ACL update input: %w", err)
		}
		input, err = prepareWebACLUpdateInput(ctx, client, input)
		if err != nil {
			return nil, err
		}
		initialErr := updateWebACLWithUnavailableRetry(
			ctx,
			client,
			input,
			retryOptions...,
		)
		if initialErr != nil {
			if !isWebACLLockConflict(initialErr) {
				return nil, fmt.Errorf("update web ACL %s: %w", identity.ID, initialErr)
			}
			refreshedToken, err := refreshWebACLUpdateLockToken(ctx, client, identity)
			if err != nil {
				return nil, err
			}
			if refreshedToken == aws.ToString(input.LockToken) {
				return nil, fmt.Errorf("update web ACL %s: %w", identity.ID, initialErr)
			}
			input.LockToken = aws.String(refreshedToken)
			if err := updateWebACLAfterRefreshWithRetry(
				ctx,
				client,
				input,
				retryOptions...,
			); err != nil {
				if isWebACLLockConflict(err) {
					return nil, fmt.Errorf(
						"update web ACL %s: resource changed again after refresh; plan again: %w",
						identity.ID,
						err,
					)
				}
				return nil, fmt.Errorf("update web ACL %s: %w", identity.ID, err)
			}
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client, identity.ARN); err != nil {
			return nil, err
		}
	}
	return readWebACL(ctx, client, identity, true)
}

func webACLUpdatePriorOutput(
	prior runtime.Prior[WebACLResource, *WebACLResourceOutput],
	identity webACLIdentity,
) (*WebACLResourceOutput, error) {
	observed := prior.Observed
	if observed == nil || observed.LockToken == "" {
		return prior.Outputs, nil
	}
	observedIdentity, err := priorWebACLIdentity(observed)
	if err != nil {
		return nil, fmt.Errorf("validate observed web ACL identity: %w", err)
	}
	if observedIdentity != identity || observed.ID != identity.ID {
		return nil, fmt.Errorf("observed web ACL identity does not match prior output")
	}
	selected := *prior.Outputs
	selected.LockToken = observed.LockToken
	return &selected, nil
}

func updateWebACLWithUnavailableRetry(
	ctx context.Context,
	client wafClient,
	input *awssvc.UpdateWebACLInput,
	retryOptions ...awsretry.Option,
) error {
	options := append(
		[]awsretry.Option{awsretry.WithTimeout(webACLRetryTimeout)},
		retryOptions...,
	)
	return awsretry.OnError(
		ctx,
		isWebACLUnavailable,
		func(ctx context.Context) error {
			_, err := client.UpdateWebACL(ctx, input)
			return err
		},
		options...,
	)
}

func updateWebACLAfterRefreshWithRetry(
	ctx context.Context,
	client wafClient,
	input *awssvc.UpdateWebACLInput,
	retryOptions ...awsretry.Option,
) error {
	options := append(
		[]awsretry.Option{awsretry.WithTimeout(webACLRetryTimeout)},
		retryOptions...,
	)
	return awsretry.OnError(
		ctx,
		isWebACLUpdateAfterRefreshRetryable,
		func(ctx context.Context) error {
			_, err := client.UpdateWebACL(ctx, input)
			return err
		},
		options...,
	)
}

func isWebACLUpdateAfterRefreshRetryable(err error) bool {
	return isWebACLUnavailable(err) || isWebACLAssociated(err)
}

func refreshWebACLUpdateLockToken(
	ctx context.Context,
	client wafClient,
	identity webACLIdentity,
) (string, error) {
	response, err := client.GetWebACL(ctx, &awssvc.GetWebACLInput{
		Id:    aws.String(identity.ID),
		Name:  aws.String(identity.Name),
		Scope: identity.Scope,
	})
	if err != nil {
		return "", fmt.Errorf(
			"refresh web ACL %s after update conflict: %w",
			identity.ID,
			err,
		)
	}
	if response == nil {
		return "", fmt.Errorf(
			"refresh web ACL %s after update conflict: empty response",
			identity.ID,
		)
	}
	if response.WebACL == nil {
		return "", fmt.Errorf(
			"refresh web ACL %s after update conflict: response has no web ACL",
			identity.ID,
		)
	}
	token := aws.ToString(response.LockToken)
	if token == "" {
		return "", fmt.Errorf(
			"refresh web ACL %s after update conflict: response has no lock-token",
			identity.ID,
		)
	}
	if _, err := webACLOutput(identity, response); err != nil {
		return "", fmt.Errorf(
			"refresh web ACL %s after update conflict: %w",
			identity.ID,
			err,
		)
	}
	return token, nil
}

func (r *WebACLResource) configurationChanged(prior WebACLResource) bool {
	return runtime.Changed(prior.AssociationConfig, r.AssociationConfig) ||
		runtime.Changed(prior.CaptchaConfig, r.CaptchaConfig) ||
		runtime.Changed(prior.ChallengeConfig, r.ChallengeConfig) ||
		runtime.Changed(prior.CustomResponseBodies, r.CustomResponseBodies) ||
		runtime.Changed(prior.DataProtectionConfig, r.DataProtectionConfig) ||
		runtime.Changed(prior.OnSourceDDoSConfig, r.OnSourceDDoSConfig) ||
		runtime.Changed(prior.DefaultAction, r.DefaultAction) ||
		runtime.Changed(prior.VisibilityConfig, r.VisibilityConfig) ||
		runtime.Changed(prior.Description, r.Description) ||
		runtime.Changed(prior.Rules, r.Rules) ||
		runtime.Changed(prior.TokenDomains, r.TokenDomains)
}

func (r *WebACLResource) syncTags(
	ctx context.Context,
	client wafClient,
	arn string,
) error {
	var desired map[string]string
	if r.Tags != nil {
		desired = *r.Tags
	}
	return tagsync.Sync(
		ctx,
		desired,
		func(ctx context.Context) (map[string]string, error) {
			return listWebACLTags(ctx, client, arn)
		},
		func(ctx context.Context, tags map[string]string) error {
			_, err := client.TagResource(ctx, &awssvc.TagResourceInput{
				ResourceARN: aws.String(arn),
				Tags:        expandTags(tags),
			})
			if err != nil {
				return fmt.Errorf("tag web ACL %s: %w", arn, err)
			}
			return nil
		},
		func(ctx context.Context, keys []string) error {
			_, err := client.UntagResource(ctx, &awssvc.UntagResourceInput{
				ResourceARN: aws.String(arn),
				TagKeys:     keys,
			})
			if err != nil {
				return fmt.Errorf("untag web ACL %s: %w", arn, err)
			}
			return nil
		},
	)
}

func listWebACLTags(
	ctx context.Context,
	client wafClient,
	arn string,
) (map[string]string, error) {
	current := map[string]string{}
	var marker *string
	for {
		output, err := client.ListTagsForResource(ctx, &awssvc.ListTagsForResourceInput{
			ResourceARN: aws.String(arn),
			NextMarker:  marker,
		})
		if err != nil {
			return nil, fmt.Errorf("list web ACL tags %s: %w", arn, err)
		}
		if output == nil {
			return current, nil
		}
		if output.TagInfoForResource != nil {
			for _, tag := range output.TagInfoForResource.TagList {
				current[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
			}
		}
		if aws.ToString(output.NextMarker) == "" {
			return current, nil
		}
		marker = output.NextMarker
	}
}
