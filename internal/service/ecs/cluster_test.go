package ecs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/stretchr/testify/assert"
)

func TestClusterNameRegexp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{name: "simple name", input: "app", matches: true},
		{name: "all allowed characters", input: "App-1_b", matches: true},
		{name: "single character", input: "a", matches: true},
		{name: "max length", input: strings255(), matches: true},
		{name: "over max length", input: strings255() + "a", matches: false},
		{name: "empty", input: "", matches: false},
		{name: "dot", input: "app.prod", matches: false},
		{name: "space", input: "app prod", matches: false},
		{name: "slash", input: "app/prod", matches: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.matches, clusterNameRegexp.MatchString(tt.input))
		})
	}
}

func strings255() string {
	out := make([]byte, 255)
	for i := range out {
		out[i] = 'x'
	}
	return string(out)
}

func TestClusterConfigurationSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    *ClusterConfiguration
		expected *ecstypes.ClusterConfiguration
	}{
		{name: "nil block", input: nil, expected: nil},
		{
			name:     "empty block",
			input:    &ClusterConfiguration{},
			expected: &ecstypes.ClusterConfiguration{},
		},
		{
			name: "execute command with log configuration",
			input: &ClusterConfiguration{
				ExecuteCommandConfiguration: &ClusterExecuteCommandConfiguration{
					KmsKeyId: aws.String("key-1"),
					Logging:  aws.String("OVERRIDE"),
					LogConfiguration: &ClusterExecuteCommandLogConfiguration{
						CloudWatchEncryptionEnabled: aws.Bool(true),
						CloudWatchLogGroupName:      aws.String("group"),
						S3BucketName:                aws.String("bucket"),
						S3KeyPrefix:                 aws.String("prefix"),
					},
				},
			},
			expected: &ecstypes.ClusterConfiguration{
				ExecuteCommandConfiguration: &ecstypes.ExecuteCommandConfiguration{
					KmsKeyId: aws.String("key-1"),
					Logging:  ecstypes.ExecuteCommandLoggingOverride,
					LogConfiguration: &ecstypes.ExecuteCommandLogConfiguration{
						CloudWatchEncryptionEnabled: true,
						CloudWatchLogGroupName:      aws.String("group"),
						S3BucketName:                aws.String("bucket"),
						S3EncryptionEnabled:         false,
						S3KeyPrefix:                 aws.String("prefix"),
					},
				},
			},
		},
		{
			name: "execute command without logging",
			input: &ClusterConfiguration{
				ExecuteCommandConfiguration: &ClusterExecuteCommandConfiguration{
					KmsKeyId: aws.String("key-1"),
				},
			},
			expected: &ecstypes.ClusterConfiguration{
				ExecuteCommandConfiguration: &ecstypes.ExecuteCommandConfiguration{
					KmsKeyId: aws.String("key-1"),
				},
			},
		},
		{
			name: "managed storage only",
			input: &ClusterConfiguration{
				ManagedStorageConfiguration: &ClusterManagedStorageConfiguration{
					KmsKeyId:                        aws.String("key-1"),
					FargateEphemeralStorageKmsKeyId: aws.String("key-2"),
				},
			},
			expected: &ecstypes.ClusterConfiguration{
				ManagedStorageConfiguration: &ecstypes.ManagedStorageConfiguration{
					KmsKeyId:                        aws.String("key-1"),
					FargateEphemeralStorageKmsKeyId: aws.String("key-2"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.input.sdk())
		})
	}
}

func TestClusterServiceConnectDefaultsSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    *ClusterServiceConnectDefaults
		expected *ecstypes.ClusterServiceConnectDefaultsRequest
	}{
		{name: "nil block", input: nil, expected: nil},
		{
			name:  "namespace set",
			input: &ClusterServiceConnectDefaults{Namespace: "ns"},
			expected: &ecstypes.ClusterServiceConnectDefaultsRequest{
				Namespace: aws.String("ns"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.input.sdk())
		})
	}
}

func TestClusterSettingsSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    []ClusterSetting
		expected []ecstypes.ClusterSetting
	}{
		{name: "nil list", input: nil, expected: nil},
		{name: "empty list", input: []ClusterSetting{}, expected: nil},
		{
			name:  "container insights",
			input: []ClusterSetting{{Name: "containerInsights", Value: "enhanced"}},
			expected: []ecstypes.ClusterSetting{{
				Name:  ecstypes.ClusterSettingNameContainerInsights,
				Value: aws.String("enhanced"),
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, clusterSettingsSDK(tt.input))
		})
	}
}

func TestClusterStrategySDK(t *testing.T) {
	tests := []struct {
		name     string
		input    []ClusterCapacityProviderStrategyItem
		expected []ecstypes.CapacityProviderStrategyItem
	}{
		{
			name:     "nil list is an explicit empty strategy",
			input:    nil,
			expected: []ecstypes.CapacityProviderStrategyItem{},
		},
		{
			name:     "empty list is an explicit empty strategy",
			input:    []ClusterCapacityProviderStrategyItem{},
			expected: []ecstypes.CapacityProviderStrategyItem{},
		},
		{
			name:  "omitted base and weight ride as zero",
			input: []ClusterCapacityProviderStrategyItem{{CapacityProvider: "FARGATE"}},
			expected: []ecstypes.CapacityProviderStrategyItem{{
				CapacityProvider: aws.String("FARGATE"),
				Base:             0,
				Weight:           0,
			}},
		},
		{
			name: "base and weight set",
			input: []ClusterCapacityProviderStrategyItem{
				{CapacityProvider: "FARGATE", Base: aws.Int64(1), Weight: aws.Int64(2)},
				{CapacityProvider: "FARGATE_SPOT", Weight: aws.Int64(4)},
			},
			expected: []ecstypes.CapacityProviderStrategyItem{
				{CapacityProvider: aws.String("FARGATE"), Base: 1, Weight: 2},
				{CapacityProvider: aws.String("FARGATE_SPOT"), Base: 0, Weight: 4},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clusterStrategySDK(tt.input)
			assert.NotNil(t, got)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestClusterTags(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected []ecstypes.Tag
	}{
		{name: "nil map", input: nil, expected: nil},
		{name: "empty map", input: map[string]string{}, expected: nil},
		{
			name:  "keys ordered",
			input: map[string]string{"b": "2", "a": "1", "c": "3"},
			expected: []ecstypes.Tag{
				{Key: aws.String("a"), Value: aws.String("1")},
				{Key: aws.String("b"), Value: aws.String("2")},
				{Key: aws.String("c"), Value: aws.String("3")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 5 {
				assert.Equal(t, tt.expected, clusterTags(tt.input))
			}
		})
	}
}

func TestClusterCreateRetryable(t *testing.T) {
	roleRace := &ecstypes.InvalidParameterException{
		Message: aws.String("Unable to assume the service linked role." +
			" Please verify that the ECS service linked role exists."),
	}
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{name: "service linked role race", err: roleRace, retryable: true},
		{
			name:      "wrapped service linked role race",
			err:       fmt.Errorf("create: %w", roleRace),
			retryable: true,
		},
		{
			name:      "other invalid parameter",
			err:       &ecstypes.InvalidParameterException{Message: aws.String("bad setting")},
			retryable: false,
		},
		{name: "other typed error", err: &ecstypes.ClusterNotFoundException{}, retryable: false},
		{name: "plain error", err: errors.New("dial tcp: timeout"), retryable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.retryable, clusterCreateRetryable(tt.err))
		})
	}
}

func TestClusterDeleteRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "contains container instances",
			err:       &ecstypes.ClusterContainsContainerInstancesException{},
			retryable: true,
		},
		{name: "contains services", err: &ecstypes.ClusterContainsServicesException{}, retryable: true},
		{name: "contains tasks", err: &ecstypes.ClusterContainsTasksException{}, retryable: true},
		{name: "update in progress", err: &ecstypes.UpdateInProgressException{}, retryable: true},
		{
			name:      "wrapped contains tasks",
			err:       fmt.Errorf("delete: %w", &ecstypes.ClusterContainsTasksException{}),
			retryable: true,
		},
		{name: "not found", err: &ecstypes.ClusterNotFoundException{}, retryable: false},
		{name: "plain error", err: errors.New("dial tcp: timeout"), retryable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.retryable, clusterDeleteRetryable(tt.err))
		})
	}
}

func TestClusterPutRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name: "cluster not active",
			err: &ecstypes.ClientException{
				Message: aws.String("The specified Cluster was not ACTIVE."),
			},
			retryable: true,
		},
		{
			name:      "other client exception",
			err:       &ecstypes.ClientException{Message: aws.String("some other problem")},
			retryable: false,
		},
		{name: "resource in use", err: &ecstypes.ResourceInUseException{}, retryable: true},
		{name: "update in progress", err: &ecstypes.UpdateInProgressException{}, retryable: true},
		{name: "not found", err: &ecstypes.ClusterNotFoundException{}, retryable: false},
		{name: "plain error", err: errors.New("dial tcp: timeout"), retryable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.retryable, clusterPutRetryable(tt.err))
		})
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		codes    []string
		notFound bool
	}{
		{
			name:     "matching code",
			err:      &ecstypes.ClusterNotFoundException{},
			codes:    []string{clusterNotFoundCode},
			notFound: true,
		},
		{
			name:     "wrapped matching code",
			err:      fmt.Errorf("describe: %w", &ecstypes.ClusterNotFoundException{}),
			codes:    []string{clusterNotFoundCode},
			notFound: true,
		},
		{
			name:     "non-matching code",
			err:      &ecstypes.ClientException{},
			codes:    []string{clusterNotFoundCode},
			notFound: false,
		},
		{
			name:     "plain error",
			err:      errors.New("dial tcp: timeout"),
			codes:    []string{clusterNotFoundCode},
			notFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.notFound, isNotFound(tt.err, tt.codes...))
		})
	}
}
