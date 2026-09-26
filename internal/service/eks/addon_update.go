package eks

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

func (r AddonResource) updateAddon(
	ctx context.Context,
	client addonClient,
	prior runtime.Prior[AddonResource, *AddonResourceOutput, *awsCfg],
	clock clusterClock,
	tokens addonTokenSource,
) (*AddonResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	clusterName, addonName, err := addonNames(prior.Outputs)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		arn, err := addonTagARN(prior)
		if err != nil {
			return nil, err
		}
		if err := syncAddonTags(ctx, client, arn, valueMapOrNil(r.Tags)); err != nil {
			return nil, err
		}
	}
	input, needed := r.addonUpdateInput(
		prior.Inputs, clusterName, addonName, "",
	)
	if needed {
		token, err := tokens()
		if err != nil {
			return nil, err
		}
		input.ClientRequestToken = aws.String(token)
		response, err := client.UpdateAddon(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("update add-on %s/%s: %w", clusterName, addonName, err)
		}
		if response == nil || response.Update == nil ||
			aws.ToString(response.Update.Id) == "" {
			return nil, fmt.Errorf("update add-on %s/%s: response has no update ID",
				clusterName, addonName)
		}
		updateID := aws.ToString(response.Update.Id)
		if err := waitAddonUpdate(
			ctx, client, clusterName, addonName, updateID, clock,
		); err != nil {
			if input.ResolveConflicts != ekstypes.ResolveConflictsOverwrite {
				return nil, fmt.Errorf(
					"wait for add-on %s/%s update %s: %w; consider setting "+
						"resolve-conflicts-on-update to OVERWRITE",
					clusterName, addonName, updateID, err,
				)
			}
			return nil, fmt.Errorf("wait for add-on %s/%s update %s: %w",
				clusterName, addonName, updateID, err)
		}
	}
	return readAddonByName(ctx, client, clusterName, addonName)
}

func syncAddonTags(
	ctx context.Context,
	client addonClient,
	arn string,
	desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			output, err := client.ListTagsForResource(ctx, &eks.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("list add-on tags: %w", err)
			}
			if output == nil {
				return map[string]string{}, nil
			}
			return output.Tags, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &eks.TagResourceInput{
				ResourceArn: aws.String(arn), Tags: upsert,
			})
			if err != nil {
				return fmt.Errorf("tag add-on: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &eks.UntagResourceInput{
				ResourceArn: aws.String(arn), TagKeys: remove,
			})
			if err != nil {
				return fmt.Errorf("untag add-on: %w", err)
			}
			return nil
		},
	)
}

func addonTagARN(
	prior runtime.Prior[AddonResource, *AddonResourceOutput, *awsCfg],
) (string, error) {
	if prior.Observed != nil && prior.Observed.ARN != "" {
		return prior.Observed.ARN, nil
	}
	if prior.Outputs != nil && prior.Outputs.ARN != "" {
		return prior.Outputs.ARN, nil
	}
	return "", fmt.Errorf("add-on output has no ARN for tag update")
}
