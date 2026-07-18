package opensearch

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainEBSVolumeTypeProjection(t *testing.T) {
	tests := []struct {
		volumeType     string
		wantIOPS       bool
		wantThroughput bool
	}{
		{volumeType: "standard"},
		{volumeType: "gp2"},
		{volumeType: "io1", wantIOPS: true},
		{volumeType: "gp3", wantIOPS: true, wantThroughput: true},
	}

	for _, test := range tests {
		t.Run(test.volumeType, func(t *testing.T) {
			options := domainEBSOptions(&DomainEBSOptions{
				EBSEnabled: true,
				IOPS:       int64Pointer(3000),
				Throughput: int64Pointer(125),
				VolumeSize: int64Pointer(20),
				VolumeType: &test.volumeType,
			})
			require.NotNil(t, options)
			assert.True(t, aws.ToBool(options.EBSEnabled))
			assert.Equal(t, int32(20), aws.ToInt32(options.VolumeSize))
			assert.Equal(t, test.volumeType, string(options.VolumeType))
			if test.wantIOPS {
				assert.Equal(t, int32(3000), aws.ToInt32(options.Iops))
			} else {
				assert.Nil(t, options.Iops)
			}
			if test.wantThroughput {
				assert.Equal(t, int32(125), aws.ToInt32(options.Throughput))
			} else {
				assert.Nil(t, options.Throughput)
			}
		})
	}
}

func TestDomainColdStorageVersionProjection(t *testing.T) {
	tests := []struct {
		name          string
		engineVersion *string
		wantIncluded  bool
	}{
		{
			name:          "Elasticsearch 7.8",
			engineVersion: stringPointer("Elasticsearch_7.8"),
		},
		{
			name:          "Elasticsearch 7.9",
			engineVersion: stringPointer("Elasticsearch_7.9"),
			wantIncluded:  true,
		},
		{
			name:          "OpenSearch",
			engineVersion: stringPointer("OpenSearch_1.0"),
			wantIncluded:  true,
		},
		{name: "service default", wantIncluded: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := domainClusterConfig(&DomainClusterConfig{
				ColdStorageOptions: &DomainColdStorageOptions{Enabled: aws.Bool(true)},
			}, test.engineVersion)
			require.NotNil(t, config)
			if test.wantIncluded {
				require.NotNil(t, config.ColdStorageOptions)
				assert.True(t, aws.ToBool(config.ColdStorageOptions.Enabled))
			} else {
				assert.Nil(t, config.ColdStorageOptions)
			}
		})
	}
}
