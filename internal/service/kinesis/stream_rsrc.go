package kinesis

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *StreamResource) SchemaVersion() int { return 1 }

func (r *StreamResource) ReplaceFields() []string { return []string{"name"} }

func (r StreamResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.EncryptionType, "NONE"),
		defaults.Value(r.EnforceConsumerDeletion, false),
		defaults.Value(r.RetentionPeriod, int64(24)),
	}
}

func (r StreamResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.EncryptionType, "NONE", "KMS")).
			Message("encryption-type must be NONE or KMS"),
		constraint.When(constraint.Equals(r.EncryptionType, "KMS")).
			Require(constraint.NotEmpty(r.KMSKeyID)).
			Message("KMS encryption requires a non-empty kms-key-id"),
		constraint.Must(
			constraint.AtLeast(r.RetentionPeriod, 24),
			constraint.AtMost(r.RetentionPeriod, 8760),
		).
			Message("retention-period must be between 24 and 8760"),
		constraint.When(constraint.Present(r.MaxRecordSizeInKiB)).
			Require(
				constraint.AtLeast(r.MaxRecordSizeInKiB, 1024),
				constraint.AtMost(r.MaxRecordSizeInKiB, 10240),
			).
			Message("max-record-size-in-kib must be between 1024 and 10240"),
		constraint.When(constraint.Absent(r.StreamModeDetails)).
			Require(constraint.Present(r.ShardCount)).
			Message("PROVISIONED mode requires shard-count"),
		constraint.When(constraint.Present(r.StreamModeDetails)).
			Require(constraint.OneOf(
				r.StreamModeDetails.StreamMode,
				"PROVISIONED",
				"ON_DEMAND",
			)).
			Message("stream-mode must be PROVISIONED or ON_DEMAND"),
		constraint.When(constraint.Equals(
			r.StreamModeDetails.StreamMode,
			"PROVISIONED",
		)).
			Require(constraint.Present(r.ShardCount)).
			Message("PROVISIONED mode requires shard-count"),
		constraint.When(constraint.Equals(
			r.StreamModeDetails.StreamMode,
			"ON_DEMAND",
		)).
			Require(constraint.Absent(r.ShardCount)).
			Message("ON_DEMAND mode forbids shard-count"),
		constraint.When(constraint.Present(r.ShardCount)).
			Require(constraint.AtLeast(r.ShardCount, 1)).
			Message("shard-count must be at least 1"),
		constraint.ForbiddenWith(r.WarmThroughputMiBps, r.ShardCount).
			Message("warm-throughput-mib-ps conflicts with shard-count"),
		constraint.ForEach(r.ShardLevelMetrics, func(metric string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(
					metric,
					"IncomingBytes",
					"IncomingRecords",
					"OutgoingBytes",
					"OutgoingRecords",
					"WriteProvisionedThroughputExceeded",
					"ReadProvisionedThroughputExceeded",
					"IteratorAgeMilliseconds",
					"ALL",
				)).Message("shard-level-metrics contains an unsupported metric"),
			}
		}),
	}
}

func (r *StreamResource) EquivalentInput(
	field string,
	prior StreamResource,
	current StreamResource,
) bool {
	switch field {
	case "stream-mode-details":
		return effectiveStreamMode(prior.StreamModeDetails) ==
			effectiveStreamMode(current.StreamModeDetails)
	case "shard-level-metrics":
		return equalStringSets(prior.ShardLevelMetrics, current.ShardLevelMetrics)
	case "tags":
		return equalStringMaps(userStreamTags(prior.Tags), userStreamTags(current.Tags))
	default:
		return false
	}
}

func (r *StreamResource) ValidateInputs(context.Context, *awsCfg) error {
	return r.validate()
}

func (r *StreamResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*StreamResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, defaultStreamOperationOptions())
}

func (r *StreamResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[StreamResource, *StreamResourceOutput, *awsCfg],
) (*StreamResourceOutput, error) {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior)
}

func (r *StreamResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[StreamResource, *StreamResourceOutput, *awsCfg],
) (*StreamResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior, defaultStreamOperationOptions())
}

func (r *StreamResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[StreamResource, *StreamResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior, defaultStreamOperationOptions())
}
