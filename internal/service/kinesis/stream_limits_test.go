package kinesis

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamCreateLimitChecks(t *testing.T) {
	tests := []struct {
		name     string
		resource StreamResource
		limits   *awssdk.DescribeLimitsOutput
		fail     error
		wantErr  string
	}{
		{
			name:     "provisioned accepted",
			resource: validProvisionedStream(2),
			limits:   streamLimitOutput(5, 10, 0, 10),
		},
		{
			name:     "provisioned rejected",
			resource: validProvisionedStream(2),
			limits:   streamLimitOutput(9, 10, 0, 10),
			wantErr:  "shard limit 10 would be exceeded",
		},
		{
			name:     "on demand rejected",
			resource: validOnDemandStream(),
			limits:   streamLimitOutput(0, 10, 10, 10),
			wantErr:  "on-demand stream limit 10 would be exceeded",
		},
		{
			name:     "failed lookup is advisory",
			resource: validProvisionedStream(2),
			fail:     errors.New("unavailable"),
		},
		{
			name:     "nil lookup is advisory",
			resource: validProvisionedStream(2),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeStreamClient{
				limits: test.limits,
				fail:   map[string]error{"DescribeLimits": test.fail},
			}
			err := test.resource.validateCreateLimits(context.Background(), client)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestStreamUpdateLimitUsesObservedShardDelta(t *testing.T) {
	resource := validProvisionedStream(4)
	prior := runtime.Prior[StreamResource, *StreamResourceOutput, *awsCfg]{
		Inputs:  validProvisionedStream(3),
		Outputs: &StreamResourceOutput{OpenShardCount: 3},
		Observed: &StreamResourceOutput{
			OpenShardCount:    3,
			StreamModeDetails: &StreamModeDetails{StreamMode: "PROVISIONED"},
		},
	}
	client := &fakeStreamClient{limits: streamLimitOutput(9, 10, 0, 10)}

	err := resource.validateUpdateLimits(context.Background(), client, prior)
	require.NoError(t, err)

	*resource.ShardCount = 5
	err = resource.validateUpdateLimits(context.Background(), client, prior)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shard limit 10 would be exceeded")
}

func TestStreamUpdateLimitSkipsExistingOnDemandStream(t *testing.T) {
	resource := validProvisionedStream(2)
	prior := runtime.Prior[StreamResource, *StreamResourceOutput, *awsCfg]{
		Observed: &StreamResourceOutput{
			StreamModeDetails: &StreamModeDetails{StreamMode: "ON_DEMAND"},
		},
	}
	client := &fakeStreamClient{limits: streamLimitOutput(10, 10, 10, 10)}

	require.NoError(t, resource.validateUpdateLimits(context.Background(), client, prior))
	assert.Empty(t, client.calls)
}

func validProvisionedStream(shards int64) StreamResource {
	return StreamResource{
		Name:            "events",
		EncryptionType:  "NONE",
		RetentionPeriod: 24,
		ShardCount:      new(shards),
	}
}

func validOnDemandStream() StreamResource {
	return StreamResource{
		Name:              "events",
		EncryptionType:    "NONE",
		RetentionPeriod:   24,
		StreamModeDetails: &StreamModeDetails{StreamMode: "ON_DEMAND"},
	}
}

func streamLimitOutput(
	open,
	shardLimit,
	onDemand,
	onDemandLimit int32,
) *awssdk.DescribeLimitsOutput {
	return &awssdk.DescribeLimitsOutput{
		OpenShardCount:           aws.Int32(open),
		ShardLimit:               aws.Int32(shardLimit),
		OnDemandStreamCount:      aws.Int32(onDemand),
		OnDemandStreamCountLimit: aws.Int32(onDemandLimit),
	}
}
