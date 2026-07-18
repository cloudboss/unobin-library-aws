package kinesis

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r StreamResource) validateCreateLimits(
	ctx context.Context,
	client streamClient,
) error {
	limits, err := streamLimits(ctx, client)
	if err != nil || limits == nil {
		return nil
	}
	if effectiveStreamMode(r.StreamModeDetails) == streamModeOnDemand {
		if limits.OnDemandStreamCount == nil || limits.OnDemandStreamCountLimit == nil {
			return nil
		}
		if aws.ToInt32(limits.OnDemandStreamCount)+1 >
			aws.ToInt32(limits.OnDemandStreamCountLimit) {
			return fmt.Errorf(
				"create stream %s: on-demand stream limit %d would be exceeded",
				r.Name,
				aws.ToInt32(limits.OnDemandStreamCountLimit),
			)
		}
		return nil
	}
	if limits.OpenShardCount == nil || limits.ShardLimit == nil || r.ShardCount == nil {
		return nil
	}
	if int64(aws.ToInt32(limits.OpenShardCount))+*r.ShardCount >
		int64(aws.ToInt32(limits.ShardLimit)) {
		return fmt.Errorf(
			"create stream %s: shard limit %d would be exceeded",
			r.Name,
			aws.ToInt32(limits.ShardLimit),
		)
	}
	return nil
}

func (r StreamResource) validateUpdateLimits(
	ctx context.Context,
	client streamClient,
	prior runtime.Prior[StreamResource, *StreamResourceOutput],
) error {
	if effectiveStreamMode(r.StreamModeDetails) != streamModeProvisioned {
		return nil
	}
	observed := streamObserved(prior.Outputs, prior.Observed)
	if observed != nil &&
		effectiveStreamMode(observed.StreamModeDetails) == streamModeOnDemand {
		return nil
	}
	limits, err := streamLimits(ctx, client)
	if err != nil || limits == nil || limits.OpenShardCount == nil ||
		limits.ShardLimit == nil || r.ShardCount == nil {
		return nil
	}
	priorCount := int64(0)
	if observed != nil {
		priorCount = observed.OpenShardCount
	} else if prior.Inputs.ShardCount != nil {
		priorCount = *prior.Inputs.ShardCount
	}
	projected := int64(aws.ToInt32(limits.OpenShardCount)) - priorCount + *r.ShardCount
	if projected > int64(aws.ToInt32(limits.ShardLimit)) {
		return fmt.Errorf(
			"update stream %s: shard limit %d would be exceeded",
			r.Name,
			aws.ToInt32(limits.ShardLimit),
		)
	}
	return nil
}

func streamLimits(
	ctx context.Context,
	client streamClient,
) (*awssdk.DescribeLimitsOutput, error) {
	return client.DescribeLimits(ctx, &awssdk.DescribeLimitsInput{})
}
