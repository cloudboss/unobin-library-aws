package kinesis

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r StreamResource) create(
	ctx context.Context,
	client streamClient,
	options streamOperationOptions,
) (*StreamResourceOutput, error) {
	options = options.withDefaults()
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := r.validateCreateLimits(ctx, client); err != nil {
		return nil, err
	}
	input, err := r.createInput()
	if err != nil {
		return nil, err
	}
	if _, err := client.CreateStream(ctx, input); err != nil {
		return nil, fmt.Errorf("create stream %s: %w", r.Name, err)
	}
	fail := func(err error) (*StreamResourceOutput, error) {
		return r.compensateCreate(ctx, client, options, err)
	}
	created, err := waitStreamCreated(ctx, client, r.Name, options)
	if err != nil {
		return fail(err)
	}
	arn := aws.ToString(created.StreamARN)
	if arn == "" {
		return fail(fmt.Errorf("create stream %s: active stream has no ARN", r.Name))
	}
	_, err = client.IncreaseStreamRetentionPeriod(
		ctx,
		&awssdk.IncreaseStreamRetentionPeriodInput{
			StreamARN:            aws.String(arn),
			RetentionPeriodHours: aws.Int32(int32(r.RetentionPeriod)),
		},
	)
	if err != nil {
		return fail(fmt.Errorf("set stream %s retention: %w", r.Name, err))
	}
	if _, err := waitStreamCreateUpdated(ctx, client, arn, options); err != nil {
		return fail(err)
	}
	metrics := metricsForRequest(r.ShardLevelMetrics)
	if len(metrics) > 0 {
		_, err = client.EnableEnhancedMonitoring(ctx, &awssdk.EnableEnhancedMonitoringInput{
			StreamARN:         aws.String(arn),
			ShardLevelMetrics: metrics,
		})
		if err != nil {
			return fail(fmt.Errorf("enable stream %s monitoring: %w", r.Name, err))
		}
		if _, err := waitStreamCreateUpdated(ctx, client, arn, options); err != nil {
			return fail(err)
		}
	}
	if r.EncryptionType == streamEncryptionKMS {
		_, err = client.StartStreamEncryption(ctx, &awssdk.StartStreamEncryptionInput{
			StreamARN:      aws.String(arn),
			EncryptionType: awstypes.EncryptionTypeKms,
			KeyId:          r.KMSKeyID,
		})
		if err != nil {
			return fail(fmt.Errorf("start stream %s encryption: %w", r.Name, err))
		}
		if _, err := waitStreamCreateUpdated(ctx, client, arn, options); err != nil {
			return fail(err)
		}
	}
	output, err := r.read(ctx, client, &StreamResourceOutput{Name: r.Name})
	if err != nil {
		return fail(err)
	}
	return output, nil
}

func (r StreamResource) compensateCreate(
	ctx context.Context,
	client streamClient,
	options streamOperationOptions,
	originalErr error,
) (*StreamResourceOutput, error) {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		options.deleteTimeout,
	)
	defer cancel()
	cleanupErr := r.delete(
		cleanupCtx,
		client,
		&StreamResourceOutput{Name: r.Name},
		options,
	)
	if cleanupErr != nil {
		return nil, errors.Join(
			originalErr,
			fmt.Errorf("clean up stream %s after failed create: %w", r.Name, cleanupErr),
		)
	}
	return nil, originalErr
}

func (r StreamResource) read(
	ctx context.Context,
	client streamClient,
	prior *StreamResourceOutput,
) (*StreamResourceOutput, error) {
	name := streamPriorName(r.Name, prior)
	summary, err := describeStream(ctx, client, streamAddress{name: name})
	if err != nil {
		return nil, err
	}
	return streamOutput(summary), nil
}

func (r StreamResource) update(
	ctx context.Context,
	client streamClient,
	prior runtime.Prior[StreamResource, *StreamResourceOutput, *awsCfg],
	options streamOperationOptions,
) (*StreamResourceOutput, error) {
	options = options.withDefaults()
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := r.validateUpdateLimits(ctx, client, prior); err != nil {
		return nil, err
	}
	arn, err := streamPriorARN(prior.Outputs, prior.Observed)
	if err != nil {
		return nil, err
	}
	if !r.EquivalentInput("tags", prior.Inputs, r) {
		if err := reconcileStreamTags(ctx, client, arn, r.Tags); err != nil {
			return nil, err
		}
	}
	priorMode := effectiveStreamMode(prior.Inputs.StreamModeDetails)
	currentMode := effectiveStreamMode(r.StreamModeDetails)
	if priorMode != currentMode {
		_, err = client.UpdateStreamMode(ctx, &awssdk.UpdateStreamModeInput{
			StreamARN: aws.String(arn),
			StreamModeDetails: &awstypes.StreamModeDetails{
				StreamMode: awstypes.StreamMode(currentMode),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("update stream %s mode: %w", r.Name, err)
		}
		if _, err := waitStreamUpdated(ctx, client, arn, options); err != nil {
			return nil, err
		}
	}
	if currentMode == streamModeProvisioned &&
		runtime.Changed(prior.Inputs.ShardCount, r.ShardCount) {
		_, err = client.UpdateShardCount(ctx, &awssdk.UpdateShardCountInput{
			StreamARN:        aws.String(arn),
			TargetShardCount: streamInt32Pointer(r.ShardCount),
			ScalingType:      awstypes.ScalingTypeUniformScaling,
		})
		if err != nil {
			return nil, fmt.Errorf("update stream %s shard count: %w", r.Name, err)
		}
		if _, err := waitStreamUpdated(ctx, client, arn, options); err != nil {
			return nil, err
		}
	}
	if prior.Inputs.RetentionPeriod != r.RetentionPeriod {
		if err := r.updateRetention(ctx, client, arn, prior.Inputs.RetentionPeriod); err != nil {
			return nil, err
		}
		if _, err := waitStreamUpdated(ctx, client, arn, options); err != nil {
			return nil, err
		}
	}
	removed := metricDifference(prior.Inputs.ShardLevelMetrics, r.ShardLevelMetrics)
	if len(removed) > 0 {
		_, err = client.DisableEnhancedMonitoring(
			ctx,
			&awssdk.DisableEnhancedMonitoringInput{
				StreamARN:         aws.String(arn),
				ShardLevelMetrics: removed,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("disable stream %s monitoring: %w", r.Name, err)
		}
		if _, err := waitStreamUpdated(ctx, client, arn, options); err != nil {
			return nil, err
		}
	}
	added := metricDifference(r.ShardLevelMetrics, prior.Inputs.ShardLevelMetrics)
	if len(added) > 0 {
		_, err = client.EnableEnhancedMonitoring(ctx, &awssdk.EnableEnhancedMonitoringInput{
			StreamARN:         aws.String(arn),
			ShardLevelMetrics: added,
		})
		if err != nil {
			return nil, fmt.Errorf("enable stream %s monitoring: %w", r.Name, err)
		}
		if _, err := waitStreamUpdated(ctx, client, arn, options); err != nil {
			return nil, err
		}
	}
	if prior.Inputs.EncryptionType != r.EncryptionType ||
		runtime.Changed(prior.Inputs.KMSKeyID, r.KMSKeyID) {
		if err := r.updateEncryption(ctx, client, arn); err != nil {
			return nil, err
		}
		if _, err := waitStreamUpdated(ctx, client, arn, options); err != nil {
			return nil, err
		}
	}
	if r.MaxRecordSizeInKiB != nil &&
		runtime.Changed(prior.Inputs.MaxRecordSizeInKiB, r.MaxRecordSizeInKiB) {
		_, err = client.UpdateMaxRecordSize(ctx, &awssdk.UpdateMaxRecordSizeInput{
			StreamARN:          aws.String(arn),
			MaxRecordSizeInKiB: streamInt32Pointer(r.MaxRecordSizeInKiB),
		})
		if err != nil {
			return nil, fmt.Errorf("update stream %s maximum record size: %w", r.Name, err)
		}
		if _, err := waitStreamUpdated(ctx, client, arn, options); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.WarmThroughputMiBps, r.WarmThroughputMiBps) {
		warm := int32(0)
		if r.WarmThroughputMiBps != nil {
			warm = int32(*r.WarmThroughputMiBps)
		}
		_, err = client.UpdateStreamWarmThroughput(
			ctx,
			&awssdk.UpdateStreamWarmThroughputInput{
				StreamARN:           aws.String(arn),
				WarmThroughputMiBps: aws.Int32(warm),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("update stream %s warm throughput: %w", r.Name, err)
		}
		if _, err := waitStreamUpdated(ctx, client, arn, options); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, &StreamResourceOutput{
		Name: streamPriorName(r.Name, prior.Outputs),
	})
}

func (r StreamResource) updateRetention(
	ctx context.Context,
	client streamClient,
	arn string,
	prior int64,
) error {
	if r.RetentionPeriod > prior {
		_, err := client.IncreaseStreamRetentionPeriod(
			ctx,
			&awssdk.IncreaseStreamRetentionPeriodInput{
				StreamARN:            aws.String(arn),
				RetentionPeriodHours: aws.Int32(int32(r.RetentionPeriod)),
			},
		)
		if err != nil {
			return fmt.Errorf("increase stream %s retention: %w", r.Name, err)
		}
		return nil
	}
	_, err := client.DecreaseStreamRetentionPeriod(
		ctx,
		&awssdk.DecreaseStreamRetentionPeriodInput{
			StreamARN:            aws.String(arn),
			RetentionPeriodHours: aws.Int32(int32(r.RetentionPeriod)),
		},
	)
	if err != nil {
		return fmt.Errorf("decrease stream %s retention: %w", r.Name, err)
	}
	return nil
}

func (r StreamResource) updateEncryption(
	ctx context.Context,
	client streamClient,
	arn string,
) error {
	if r.EncryptionType == streamEncryptionKMS {
		_, err := client.StartStreamEncryption(ctx, &awssdk.StartStreamEncryptionInput{
			StreamARN:      aws.String(arn),
			EncryptionType: awstypes.EncryptionTypeKms,
			KeyId:          r.KMSKeyID,
		})
		if err != nil {
			return fmt.Errorf("start stream %s encryption: %w", r.Name, err)
		}
		return nil
	}
	summary, err := describeStream(ctx, client, streamAddress{arn: arn})
	if err != nil {
		return err
	}
	if summary.EncryptionType == awstypes.EncryptionTypeNone {
		return nil
	}
	if summary.KeyId == nil || *summary.KeyId == "" {
		return fmt.Errorf("stop stream %s encryption: observed KMS key is empty", r.Name)
	}
	_, err = client.StopStreamEncryption(ctx, &awssdk.StopStreamEncryptionInput{
		StreamARN:      aws.String(arn),
		EncryptionType: summary.EncryptionType,
		KeyId:          summary.KeyId,
	})
	if err != nil {
		return fmt.Errorf("stop stream %s encryption: %w", r.Name, err)
	}
	return nil
}

func (r StreamResource) delete(
	ctx context.Context,
	client streamClient,
	prior *StreamResourceOutput,
	options streamOperationOptions,
) error {
	name := streamPriorName(r.Name, prior)
	_, err := client.DeleteStream(ctx, &awssdk.DeleteStreamInput{
		StreamName:              aws.String(name),
		EnforceConsumerDeletion: aws.Bool(r.EnforceConsumerDeletion),
	})
	if isStreamNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete stream %s: %w", name, err)
	}
	if err := waitStreamDeleted(ctx, client, name, options); err != nil {
		return err
	}
	return nil
}
