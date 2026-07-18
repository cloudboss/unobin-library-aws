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

func (r NodeGroupResource) updateNodeGroup(
	ctx context.Context,
	client nodeGroupClient,
	prior runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput],
	clock clusterClock,
) (*NodeGroupResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	clusterName, nodeGroupName, err := nodeGroupNames(prior.Outputs)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		arn, err := nodeGroupTagARN(prior)
		if err != nil {
			return nil, err
		}
		if err := syncNodeGroupTags(ctx, client, arn, valueMapOrNil(r.Tags)); err != nil {
			return nil, err
		}
	}
	versionInput, versionNeeded := r.nodeGroupVersionInput(
		prior.Inputs, clusterName, nodeGroupName, "",
	)
	if versionNeeded {
		token, err := nodeGroupClientRequestToken()
		if err != nil {
			return nil, err
		}
		versionInput.ClientRequestToken = aws.String(token)
		if err := runNodeGroupUpdate(
			ctx, client, clusterName, nodeGroupName, "version", clock,
			func(ctx context.Context) (*ekstypes.Update, error) {
				output, err := client.UpdateNodegroupVersion(ctx, versionInput)
				if output == nil {
					return nil, err
				}
				return output.Update, err
			},
		); err != nil {
			return nil, err
		}
	}
	configInput, configNeeded := r.nodeGroupConfigInput(
		prior.Inputs, clusterName, nodeGroupName, "",
	)
	if configNeeded {
		token, err := nodeGroupClientRequestToken()
		if err != nil {
			return nil, err
		}
		configInput.ClientRequestToken = aws.String(token)
		if err := runNodeGroupUpdate(
			ctx, client, clusterName, nodeGroupName, "configuration", clock,
			func(ctx context.Context) (*ekstypes.Update, error) {
				output, err := client.UpdateNodegroupConfig(ctx, configInput)
				if output == nil {
					return nil, err
				}
				return output.Update, err
			},
		); err != nil {
			return nil, err
		}
	}
	return readNodeGroupByName(ctx, client, clusterName, nodeGroupName)
}

func runNodeGroupUpdate(
	ctx context.Context,
	client nodeGroupClient,
	clusterName string,
	nodeGroupName string,
	description string,
	clock clusterClock,
	update func(context.Context) (*ekstypes.Update, error),
) error {
	result, err := update(ctx)
	if err != nil {
		return fmt.Errorf("update node group %s/%s %s: %w",
			clusterName, nodeGroupName, description, err)
	}
	if result == nil || aws.ToString(result.Id) == "" {
		return fmt.Errorf("update node group %s/%s %s: response has no update ID",
			clusterName, nodeGroupName, description)
	}
	updateID := aws.ToString(result.Id)
	if err := waitNodeGroupUpdate(
		ctx, client, clusterName, nodeGroupName, updateID, clock,
	); err != nil {
		return fmt.Errorf("wait for node group %s/%s %s update %s: %w",
			clusterName, nodeGroupName, description, updateID, err)
	}
	return nil
}

func syncNodeGroupTags(
	ctx context.Context,
	client nodeGroupClient,
	arn string,
	desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			output, err := client.ListTagsForResource(ctx, &eks.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("list node group tags: %w", err)
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
				return fmt.Errorf("tag node group: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &eks.UntagResourceInput{
				ResourceArn: aws.String(arn), TagKeys: remove,
			})
			if err != nil {
				return fmt.Errorf("untag node group: %w", err)
			}
			return nil
		},
	)
}

func nodeGroupTagARN(
	prior runtime.Prior[NodeGroupResource, *NodeGroupResourceOutput],
) (string, error) {
	if prior.Observed != nil && prior.Observed.ARN != "" {
		return prior.Observed.ARN, nil
	}
	if prior.Outputs != nil && prior.Outputs.ARN != "" {
		return prior.Outputs.ARN, nil
	}
	return "", fmt.Errorf("node group output has no ARN for tag update")
}
