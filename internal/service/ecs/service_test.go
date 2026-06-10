package ecs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	smithy "github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
)

func TestServiceDeploymentConfigurationSDK(t *testing.T) {
	breaker := &ServiceDeploymentCircuitBreaker{Enable: true, Rollback: true}
	breakerSDK := &ecstypes.DeploymentCircuitBreaker{Enable: true, Rollback: true}
	clearSDK := &ecstypes.DeploymentCircuitBreaker{}

	tests := []struct {
		name      string
		block     *ServiceDeploymentConfiguration
		daemon    bool
		reconcile bool
		expected  *ecstypes.DeploymentConfiguration
	}{
		{
			name:  "replica nil block creates with defaults",
			block: nil,
			expected: &ecstypes.DeploymentConfiguration{
				MaximumPercent:        aws.Int32(200),
				MinimumHealthyPercent: aws.Int32(100),
			},
		},
		{
			name:  "replica empty block creates with defaults",
			block: &ServiceDeploymentConfiguration{},
			expected: &ecstypes.DeploymentConfiguration{
				MaximumPercent:        aws.Int32(200),
				MinimumHealthyPercent: aws.Int32(100),
			},
		},
		{
			name: "replica explicit values",
			block: &ServiceDeploymentConfiguration{
				MaximumPercent:        aws.Int64(150),
				MinimumHealthyPercent: aws.Int64(50),
			},
			expected: &ecstypes.DeploymentConfiguration{
				MaximumPercent:        aws.Int32(150),
				MinimumHealthyPercent: aws.Int32(50),
			},
		},
		{
			name: "replica partial block keeps the other default",
			block: &ServiceDeploymentConfiguration{
				MaximumPercent: aws.Int64(400),
			},
			expected: &ecstypes.DeploymentConfiguration{
				MaximumPercent:        aws.Int32(400),
				MinimumHealthyPercent: aws.Int32(100),
			},
		},
		{
			name: "replica with circuit breaker at create",
			block: &ServiceDeploymentConfiguration{
				DeploymentCircuitBreaker: breaker,
			},
			expected: &ecstypes.DeploymentConfiguration{
				MaximumPercent:           aws.Int32(200),
				MinimumHealthyPercent:    aws.Int32(100),
				DeploymentCircuitBreaker: breakerSDK,
			},
		},
		{
			name:      "replica reconcile without breaker sends the clear",
			block:     &ServiceDeploymentConfiguration{MaximumPercent: aws.Int64(150)},
			reconcile: true,
			expected: &ecstypes.DeploymentConfiguration{
				MaximumPercent:           aws.Int32(150),
				MinimumHealthyPercent:    aws.Int32(100),
				DeploymentCircuitBreaker: clearSDK,
			},
		},
		{
			name:      "replica reconcile of a removed block restores defaults",
			block:     nil,
			reconcile: true,
			expected: &ecstypes.DeploymentConfiguration{
				MaximumPercent:           aws.Int32(200),
				MinimumHealthyPercent:    aws.Int32(100),
				DeploymentCircuitBreaker: clearSDK,
			},
		},
		{
			name: "replica reconcile keeps a set breaker",
			block: &ServiceDeploymentConfiguration{
				DeploymentCircuitBreaker: breaker,
			},
			reconcile: true,
			expected: &ecstypes.DeploymentConfiguration{
				MaximumPercent:           aws.Int32(200),
				MinimumHealthyPercent:    aws.Int32(100),
				DeploymentCircuitBreaker: breakerSDK,
			},
		},
		{
			name:     "daemon nil block creates nothing",
			block:    nil,
			daemon:   true,
			expected: nil,
		},
		{
			name: "daemon never includes maximum percent",
			block: &ServiceDeploymentConfiguration{
				MaximumPercent: aws.Int64(150),
			},
			daemon:   true,
			expected: nil,
		},
		{
			name: "daemon includes a minimum other than 100",
			block: &ServiceDeploymentConfiguration{
				MinimumHealthyPercent: aws.Int64(50),
			},
			daemon: true,
			expected: &ecstypes.DeploymentConfiguration{
				MinimumHealthyPercent: aws.Int32(50),
			},
		},
		{
			name: "daemon omits a minimum of exactly 100",
			block: &ServiceDeploymentConfiguration{
				MinimumHealthyPercent: aws.Int64(100),
			},
			daemon:   true,
			expected: nil,
		},
		{
			name: "daemon with circuit breaker at create",
			block: &ServiceDeploymentConfiguration{
				DeploymentCircuitBreaker: breaker,
			},
			daemon: true,
			expected: &ecstypes.DeploymentConfiguration{
				DeploymentCircuitBreaker: breakerSDK,
			},
		},
		{
			name:      "daemon reconcile of a removed block restores the default minimum",
			block:     nil,
			daemon:    true,
			reconcile: true,
			expected: &ecstypes.DeploymentConfiguration{
				MinimumHealthyPercent:    aws.Int32(100),
				DeploymentCircuitBreaker: clearSDK,
			},
		},
		{
			name: "daemon reconcile sends an explicit minimum of 100",
			block: &ServiceDeploymentConfiguration{
				MinimumHealthyPercent: aws.Int64(100),
			},
			daemon:    true,
			reconcile: true,
			expected: &ecstypes.DeploymentConfiguration{
				MinimumHealthyPercent:    aws.Int32(100),
				DeploymentCircuitBreaker: clearSDK,
			},
		},
		{
			name: "daemon reconcile with minimum and breaker",
			block: &ServiceDeploymentConfiguration{
				MinimumHealthyPercent:    aws.Int64(0),
				DeploymentCircuitBreaker: breaker,
			},
			daemon:    true,
			reconcile: true,
			expected: &ecstypes.DeploymentConfiguration{
				MinimumHealthyPercent:    aws.Int32(0),
				DeploymentCircuitBreaker: breakerSDK,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serviceDeploymentConfigurationSDK(tt.block, tt.daemon, tt.reconcile)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestServiceNetworkConfigurationSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    *ServiceNetworkConfiguration
		expected *ecstypes.NetworkConfiguration
	}{
		{name: "nil block", input: nil, expected: nil},
		{
			name:  "subnets only",
			input: &ServiceNetworkConfiguration{Subnets: []string{"subnet-1"}},
			expected: &ecstypes.NetworkConfiguration{
				AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
					Subnets: []string{"subnet-1"},
				},
			},
		},
		{
			name: "all fields",
			input: &ServiceNetworkConfiguration{
				Subnets:        []string{"subnet-1", "subnet-2"},
				SecurityGroups: &[]string{"sg-1"},
				AssignPublicIp: aws.String("ENABLED"),
			},
			expected: &ecstypes.NetworkConfiguration{
				AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
					Subnets:        []string{"subnet-1", "subnet-2"},
					SecurityGroups: []string{"sg-1"},
					AssignPublicIp: ecstypes.AssignPublicIpEnabled,
				},
			},
		},
		{
			name: "assign public ip disabled is sent",
			input: &ServiceNetworkConfiguration{
				Subnets:        []string{"subnet-1"},
				AssignPublicIp: aws.String("DISABLED"),
			},
			expected: &ecstypes.NetworkConfiguration{
				AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
					Subnets:        []string{"subnet-1"},
					AssignPublicIp: ecstypes.AssignPublicIpDisabled,
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

func TestServiceLoadBalancersSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    []ServiceLoadBalancer
		expected []ecstypes.LoadBalancer
	}{
		{name: "nil list is an explicit empty list", input: nil, expected: []ecstypes.LoadBalancer{}},
		{
			name: "entries",
			input: []ServiceLoadBalancer{
				{ContainerName: "web", ContainerPort: 80, TargetGroupArn: "arn-1"},
				{ContainerName: "api", ContainerPort: 8080, TargetGroupArn: "arn-2"},
			},
			expected: []ecstypes.LoadBalancer{
				{
					ContainerName:  aws.String("web"),
					ContainerPort:  aws.Int32(80),
					TargetGroupArn: aws.String("arn-1"),
				},
				{
					ContainerName:  aws.String("api"),
					ContainerPort:  aws.Int32(8080),
					TargetGroupArn: aws.String("arn-2"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serviceLoadBalancersSDK(tt.input)
			assert.NotNil(t, got)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestServicePlacementConstraintsSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    []ServicePlacementConstraint
		expected []ecstypes.PlacementConstraint
	}{
		{
			name:     "nil list is an explicit empty list",
			input:    nil,
			expected: []ecstypes.PlacementConstraint{},
		},
		{
			name: "entries",
			input: []ServicePlacementConstraint{
				{Type: "distinctInstance"},
				{Type: "memberOf", Expression: aws.String("attribute:ecs.os-type == linux")},
			},
			expected: []ecstypes.PlacementConstraint{
				{Type: ecstypes.PlacementConstraintTypeDistinctInstance},
				{
					Type:       ecstypes.PlacementConstraintTypeMemberOf,
					Expression: aws.String("attribute:ecs.os-type == linux"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := servicePlacementConstraintsSDK(tt.input)
			assert.NotNil(t, got)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestServicePlacementStrategySDK(t *testing.T) {
	tests := []struct {
		name     string
		input    []ServicePlacementStrategy
		expected []ecstypes.PlacementStrategy
	}{
		{
			name:     "nil list is an explicit empty list",
			input:    nil,
			expected: []ecstypes.PlacementStrategy{},
		},
		{
			name: "order preserved",
			input: []ServicePlacementStrategy{
				{Type: "spread", Field: aws.String("attribute:ecs.availability-zone")},
				{Type: "binpack", Field: aws.String("memory")},
				{Type: "random"},
			},
			expected: []ecstypes.PlacementStrategy{
				{
					Type:  ecstypes.PlacementStrategyTypeSpread,
					Field: aws.String("attribute:ecs.availability-zone"),
				},
				{Type: ecstypes.PlacementStrategyTypeBinpack, Field: aws.String("memory")},
				{Type: ecstypes.PlacementStrategyTypeRandom},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := servicePlacementStrategySDK(tt.input)
			assert.NotNil(t, got)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestServiceStrategySDK(t *testing.T) {
	tests := []struct {
		name     string
		input    []ServiceCapacityProviderStrategyItem
		expected []ecstypes.CapacityProviderStrategyItem
	}{
		{
			name:     "nil list is an explicit empty strategy",
			input:    nil,
			expected: []ecstypes.CapacityProviderStrategyItem{},
		},
		{
			name:  "omitted base and weight ride as zero",
			input: []ServiceCapacityProviderStrategyItem{{CapacityProvider: "FARGATE"}},
			expected: []ecstypes.CapacityProviderStrategyItem{{
				CapacityProvider: aws.String("FARGATE"),
				Base:             0,
				Weight:           0,
			}},
		},
		{
			name: "base and weight set",
			input: []ServiceCapacityProviderStrategyItem{
				{CapacityProvider: "FARGATE", Base: aws.Int64(1), Weight: aws.Int64(2)},
			},
			expected: []ecstypes.CapacityProviderStrategyItem{
				{CapacityProvider: aws.String("FARGATE"), Base: 1, Weight: 2},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serviceStrategySDK(tt.input)
			assert.NotNil(t, got)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestServiceCreateRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{name: "cluster not found", err: &ecstypes.ClusterNotFoundException{}, retryable: true},
		{
			name:      "wrapped cluster not found",
			err:       fmt.Errorf("create: %w", &ecstypes.ClusterNotFoundException{}),
			retryable: true,
		},
		{
			name: "service role permissions",
			err: &ecstypes.InvalidParameterException{Message: aws.String("Please verify" +
				" that the ECS service role being passed has the proper permissions.")},
			retryable: true,
		},
		{
			name: "target group without load balancer",
			err: &ecstypes.InvalidParameterException{Message: aws.String(
				"The target group does not have an associated load balancer.")},
			retryable: true,
		},
		{
			name: "service linked role race",
			err: &ecstypes.InvalidParameterException{Message: aws.String(
				"Unable to assume the service linked role.")},
			retryable: true,
		},
		{
			name:      "other invalid parameter",
			err:       &ecstypes.InvalidParameterException{Message: aws.String("bad subnet")},
			retryable: false,
		},
		{name: "service not found", err: &ecstypes.ServiceNotFoundException{}, retryable: false},
		{name: "plain error", err: errors.New("dial tcp: timeout"), retryable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.retryable, serviceCreateRetryable(tt.err))
		})
	}
}

func TestServiceUpdateRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name: "service role permissions",
			err: &ecstypes.InvalidParameterException{Message: aws.String("Please verify" +
				" that the ECS service role being passed has the proper permissions.")},
			retryable: true,
		},
		{
			name: "target group without load balancer",
			err: &ecstypes.InvalidParameterException{Message: aws.String(
				"The target group does not have an associated load balancer.")},
			retryable: true,
		},
		{
			name: "service linked role race is create-only",
			err: &ecstypes.InvalidParameterException{Message: aws.String(
				"Unable to assume the service linked role.")},
			retryable: false,
		},
		{name: "cluster not found", err: &ecstypes.ClusterNotFoundException{}, retryable: false},
		{name: "plain error", err: errors.New("dial tcp: timeout"), retryable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.retryable, serviceUpdateRetryable(tt.err))
		})
	}
}

func TestServiceDeleteRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name: "deployments still active",
			err: &ecstypes.InvalidParameterException{Message: aws.String(
				"The service cannot be stopped while deployments are active.")},
			retryable: true,
		},
		{
			name: "dependency violation with dependent object",
			err: &smithy.GenericAPIError{
				Code:    "DependencyViolation",
				Message: "The service has a dependent object.",
			},
			retryable: true,
		},
		{
			name: "wrapped dependency violation",
			err: fmt.Errorf("delete: %w", &smithy.GenericAPIError{
				Code:    "DependencyViolation",
				Message: "The service has a dependent object.",
			}),
			retryable: true,
		},
		{
			name: "dependency violation with another message",
			err: &smithy.GenericAPIError{
				Code:    "DependencyViolation",
				Message: "something else",
			},
			retryable: false,
		},
		{
			name: "dependent object message under another code",
			err: &smithy.GenericAPIError{
				Code:    "SomethingElse",
				Message: "The service has a dependent object.",
			},
			retryable: false,
		},
		{
			name:      "other invalid parameter",
			err:       &ecstypes.InvalidParameterException{Message: aws.String("bad input")},
			retryable: false,
		},
		{name: "plain error", err: errors.New("dial tcp: timeout"), retryable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.retryable, serviceDeleteRetryable(tt.err))
		})
	}
}

func TestDrainSkippable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		skippable bool
	}{
		{name: "service not active", err: &ecstypes.ServiceNotActiveException{}, skippable: true},
		{name: "service not found", err: &ecstypes.ServiceNotFoundException{}, skippable: true},
		{name: "cluster not found", err: &ecstypes.ClusterNotFoundException{}, skippable: true},
		{
			name:      "wrapped service not active",
			err:       fmt.Errorf("drain: %w", &ecstypes.ServiceNotActiveException{}),
			skippable: true,
		},
		{
			name:      "invalid parameter",
			err:       &ecstypes.InvalidParameterException{Message: aws.String("bad input")},
			skippable: false,
		},
		{name: "plain error", err: errors.New("dial tcp: timeout"), skippable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.skippable, drainSkippable(tt.err))
		})
	}
}

func TestServiceClusterInactive(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		inactive bool
	}{
		{
			name: "inactive cluster",
			err: &ecstypes.InvalidParameterException{Message: aws.String(
				"The specified cluster is inactive. Specify an active cluster and try again.")},
			inactive: true,
		},
		{
			name: "wrapped inactive cluster",
			err: fmt.Errorf("list tags for resource: %w", &ecstypes.InvalidParameterException{
				Message: aws.String("The specified cluster is inactive." +
					" Specify an active cluster and try again."),
			}),
			inactive: true,
		},
		{
			name:     "other invalid parameter",
			err:      &ecstypes.InvalidParameterException{Message: aws.String("bad input")},
			inactive: false,
		},
		{name: "cluster not found", err: &ecstypes.ClusterNotFoundException{}, inactive: false},
		{name: "plain error", err: errors.New("dial tcp: timeout"), inactive: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.inactive, serviceClusterInactive(tt.err))
		})
	}
}

func TestNewClientToken(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		token, err := newClientToken()
		assert.NoError(t, err)
		// CreateService allows up to 36 ASCII characters in the range 33-126.
		assert.Len(t, token, 32)
		for _, c := range token {
			assert.True(t, c >= 33 && c <= 126)
		}
		assert.False(t, seen[token], "token %s repeated", token)
		seen[token] = true
	}
}
