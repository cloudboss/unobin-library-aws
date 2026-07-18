package kinesis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamValidation(t *testing.T) {
	valid := func() StreamResource {
		return StreamResource{
			Name:            "events",
			EncryptionType:  "NONE",
			RetentionPeriod: 24,
			ShardCount:      new(int64(1)),
		}
	}
	tests := []struct {
		name    string
		mutate  func(*StreamResource)
		wantErr string
	}{
		{name: "provisioned omitted mode"},
		{
			name: "explicit provisioned",
			mutate: func(resource *StreamResource) {
				resource.StreamModeDetails = &StreamModeDetails{StreamMode: "PROVISIONED"}
			},
		},
		{
			name: "on demand",
			mutate: func(resource *StreamResource) {
				resource.StreamModeDetails = &StreamModeDetails{StreamMode: "ON_DEMAND"}
				resource.ShardCount = nil
			},
		},
		{
			name: "missing shard count",
			mutate: func(resource *StreamResource) {
				resource.ShardCount = nil
			},
			wantErr: "requires shard-count",
		},
		{
			name: "zero shards",
			mutate: func(resource *StreamResource) {
				resource.ShardCount = new(int64(0))
			},
			wantErr: "at least 1",
		},
		{
			name: "on demand shard conflict",
			mutate: func(resource *StreamResource) {
				resource.StreamModeDetails = &StreamModeDetails{StreamMode: "ON_DEMAND"}
			},
			wantErr: "forbids shard-count",
		},
		{
			name: "warm throughput shard conflict",
			mutate: func(resource *StreamResource) {
				resource.WarmThroughputMiBps = new(int64(10))
			},
			wantErr: "conflicts with shard-count",
		},
		{
			name: "retention lower boundary",
			mutate: func(resource *StreamResource) {
				resource.RetentionPeriod = 24
			},
		},
		{
			name: "retention lower bound",
			mutate: func(resource *StreamResource) {
				resource.RetentionPeriod = 23
			},
			wantErr: "between 24 and 8760",
		},
		{
			name: "retention upper boundary",
			mutate: func(resource *StreamResource) {
				resource.RetentionPeriod = 8760
			},
		},
		{
			name: "retention upper bound",
			mutate: func(resource *StreamResource) {
				resource.RetentionPeriod = 8761
			},
			wantErr: "between 24 and 8760",
		},
		{
			name: "record size lower boundary",
			mutate: func(resource *StreamResource) {
				resource.MaxRecordSizeInKiB = new(int64(1024))
			},
		},
		{
			name: "record size upper boundary",
			mutate: func(resource *StreamResource) {
				resource.MaxRecordSizeInKiB = new(int64(10240))
			},
		},
		{
			name: "record size lower bound",
			mutate: func(resource *StreamResource) {
				resource.MaxRecordSizeInKiB = new(int64(1023))
			},
			wantErr: "between 1024 and 10240",
		},
		{
			name: "record size upper bound",
			mutate: func(resource *StreamResource) {
				resource.MaxRecordSizeInKiB = new(int64(10241))
			},
			wantErr: "between 1024 and 10240",
		},
		{
			name: "KMS key required",
			mutate: func(resource *StreamResource) {
				resource.EncryptionType = "KMS"
			},
			wantErr: "requires a non-empty kms-key-id",
		},
		{
			name: "invalid metric",
			mutate: func(resource *StreamResource) {
				metrics := []string{"NoSuchMetric"}
				resource.ShardLevelMetrics = &metrics
			},
			wantErr: "unsupported shard-level metric",
		},
		{
			name: "duplicate metric",
			mutate: func(resource *StreamResource) {
				metrics := []string{"ALL", "ALL"}
				resource.ShardLevelMetrics = &metrics
			},
			wantErr: "duplicate",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := valid()
			if test.mutate != nil {
				test.mutate(&resource)
			}
			err := resource.validate()
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestStreamEquivalentInputs(t *testing.T) {
	resource := &StreamResource{}
	prior := StreamResource{}
	explicitProvisioned := StreamResource{
		StreamModeDetails: &StreamModeDetails{StreamMode: "PROVISIONED"},
	}
	assert.True(t, resource.EquivalentInput(
		"stream-mode-details",
		prior,
		explicitProvisioned,
	))

	leftMetrics := []string{"ALL", "IncomingBytes"}
	rightMetrics := []string{"IncomingBytes", "ALL"}
	assert.True(t, resource.EquivalentInput(
		"shard-level-metrics",
		StreamResource{ShardLevelMetrics: &leftMetrics},
		StreamResource{ShardLevelMetrics: &rightMetrics},
	))

	leftTags := map[string]string{"env": "test", "aws:owner": "one"}
	rightTags := map[string]string{"env": "test", "aws:owner": "two"}
	assert.True(t, resource.EquivalentInput(
		"tags",
		StreamResource{Tags: &leftTags},
		StreamResource{Tags: &rightTags},
	))
	assert.False(t, resource.EquivalentInput("name", prior, prior))
}
