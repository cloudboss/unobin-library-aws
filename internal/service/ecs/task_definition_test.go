package ecs

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/stretchr/testify/assert"
)

func TestTaskDefinitionFamilyRegexp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{name: "simple family", input: "app", matches: true},
		{name: "all allowed characters", input: "App-1_b", matches: true},
		{name: "single character", input: "a", matches: true},
		{name: "max length", input: strings255(), matches: true},
		{name: "over max length", input: strings255() + "a", matches: false},
		{name: "empty", input: "", matches: false},
		{name: "dot", input: "app.prod", matches: false},
		{name: "colon", input: "app:1", matches: false},
		{name: "space", input: "app prod", matches: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.matches, taskDefinitionFamilyRegexp.MatchString(tt.input))
		})
	}
}

func TestTaskDefinitionARNWithoutRevision(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "pinned arn",
			input:    "arn:aws:ecs:us-east-1:123456789012:task-definition/app:7",
			expected: "arn:aws:ecs:us-east-1:123456789012:task-definition/app",
		},
		{
			name:     "iso partition pinned arn",
			input:    "arn:aws-iso:ecs:us-iso-east-1:123456789012:task-definition/app:1",
			expected: "arn:aws-iso:ecs:us-iso-east-1:123456789012:task-definition/app",
		},
		{
			name:     "already family level",
			input:    "arn:aws:ecs:us-east-1:123456789012:task-definition/app",
			expected: "arn:aws:ecs:us-east-1:123456789012:task-definition/app",
		},
		{
			name:     "extra colon in resource",
			input:    "arn:aws:ecs:us-east-1:123456789012:task-definition/app:1:x",
			expected: "arn:aws:ecs:us-east-1:123456789012:task-definition/app:1:x",
		},
		{name: "not an arn", input: "app:7", expected: "app:7"},
		{name: "empty", input: "", expected: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 5 {
				assert.Equal(t, tt.expected, taskDefinitionARNWithoutRevision(tt.input))
			}
		})
	}
}

func TestTaskDefinitionGone(t *testing.T) {
	badRequest := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{
			Response: &http.Response{StatusCode: http.StatusBadRequest},
		},
		Err: &ecstypes.ClientException{
			Message: aws.String("Unable to describe task definition."),
		},
	}
	serverError := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{
			Response: &http.Response{StatusCode: http.StatusInternalServerError},
		},
		Err: &ecstypes.ServerException{},
	}
	tests := []struct {
		name string
		err  error
		gone bool
	}{
		{name: "bad request", err: badRequest, gone: true},
		{name: "wrapped bad request", err: fmt.Errorf("describe: %w", badRequest), gone: true},
		{name: "server error", err: serverError, gone: false},
		{name: "bare client exception", err: &ecstypes.ClientException{}, gone: false},
		{name: "plain error", err: errors.New("dial tcp: timeout"), gone: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.gone, taskDefinitionGone(tt.err))
		})
	}
}

func TestTaskDefinitionVolumeDockerSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    *TaskDefinitionVolumeDocker
		expected *ecstypes.DockerVolumeConfiguration
	}{
		{name: "nil block", input: nil, expected: nil},
		{
			name:     "empty block",
			input:    &TaskDefinitionVolumeDocker{},
			expected: &ecstypes.DockerVolumeConfiguration{},
		},
		{
			name: "autoprovision false with task scope is suppressed",
			input: &TaskDefinitionVolumeDocker{
				Autoprovision: aws.Bool(false),
				Scope:         aws.String("task"),
			},
			expected: &ecstypes.DockerVolumeConfiguration{Scope: ecstypes.ScopeTask},
		},
		{
			name:     "autoprovision false with scope omitted is sent",
			input:    &TaskDefinitionVolumeDocker{Autoprovision: aws.Bool(false)},
			expected: &ecstypes.DockerVolumeConfiguration{Autoprovision: aws.Bool(false)},
		},
		{
			name: "autoprovision false with shared scope is sent",
			input: &TaskDefinitionVolumeDocker{
				Autoprovision: aws.Bool(false),
				Scope:         aws.String("shared"),
			},
			expected: &ecstypes.DockerVolumeConfiguration{
				Autoprovision: aws.Bool(false),
				Scope:         ecstypes.ScopeShared,
			},
		},
		{
			name: "autoprovision true with task scope is sent",
			input: &TaskDefinitionVolumeDocker{
				Autoprovision: aws.Bool(true),
				Scope:         aws.String("task"),
			},
			expected: &ecstypes.DockerVolumeConfiguration{
				Autoprovision: aws.Bool(true),
				Scope:         ecstypes.ScopeTask,
			},
		},
		{
			name: "driver options and labels",
			input: &TaskDefinitionVolumeDocker{
				Driver:     aws.String("local"),
				DriverOpts: &map[string]string{"type": "nfs"},
				Labels:     &map[string]string{"app": "web"},
			},
			expected: &ecstypes.DockerVolumeConfiguration{
				Driver:     aws.String("local"),
				DriverOpts: map[string]string{"type": "nfs"},
				Labels:     map[string]string{"app": "web"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.input.sdk())
		})
	}
}

func TestTaskDefinitionVolumeEfsSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    *TaskDefinitionVolumeEfs
		expected *ecstypes.EFSVolumeConfiguration
	}{
		{name: "nil block", input: nil, expected: nil},
		{
			name:  "omitted port stays unset",
			input: &TaskDefinitionVolumeEfs{FileSystemId: "fs-1"},
			expected: &ecstypes.EFSVolumeConfiguration{
				FileSystemId: aws.String("fs-1"),
			},
		},
		{
			name: "all fields",
			input: &TaskDefinitionVolumeEfs{
				FileSystemId: "fs-1",
				AuthorizationConfig: &TaskDefinitionVolumeEfsAuthorization{
					AccessPointId: aws.String("fsap-1"),
					Iam:           aws.String("ENABLED"),
				},
				RootDirectory:         aws.String("/data"),
				TransitEncryption:     aws.String("ENABLED"),
				TransitEncryptionPort: aws.Int64(2999),
			},
			expected: &ecstypes.EFSVolumeConfiguration{
				FileSystemId: aws.String("fs-1"),
				AuthorizationConfig: &ecstypes.EFSAuthorizationConfig{
					AccessPointId: aws.String("fsap-1"),
					Iam:           ecstypes.EFSAuthorizationConfigIAMEnabled,
				},
				RootDirectory:         aws.String("/data"),
				TransitEncryption:     ecstypes.EFSTransitEncryptionEnabled,
				TransitEncryptionPort: aws.Int32(2999),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.input.sdk())
		})
	}
}

func TestTaskDefinitionKeyValuePairs(t *testing.T) {
	tests := []struct {
		name     string
		input    *map[string]string
		expected []ecstypes.KeyValuePair
	}{
		{name: "nil map", input: nil, expected: nil},
		{name: "empty map", input: &map[string]string{}, expected: nil},
		{
			name:  "keys ordered",
			input: &map[string]string{"B": "2", "A": "1", "C": "3"},
			expected: []ecstypes.KeyValuePair{
				{Name: aws.String("A"), Value: aws.String("1")},
				{Name: aws.String("B"), Value: aws.String("2")},
				{Name: aws.String("C"), Value: aws.String("3")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 5 {
				assert.Equal(t, tt.expected, taskDefinitionKeyValuePairs(tt.input))
			}
		})
	}
}

func TestTaskDefinitionContainerSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    TaskDefinitionContainerDefinition
		expected ecstypes.ContainerDefinition
	}{
		{
			name:  "name and image only",
			input: TaskDefinitionContainerDefinition{Name: "web", Image: "nginx:1"},
			expected: ecstypes.ContainerDefinition{
				Name:  aws.String("web"),
				Image: aws.String("nginx:1"),
			},
		},
		{
			name: "environment ordered and enums converted",
			input: TaskDefinitionContainerDefinition{
				Name:               "web",
				Image:              "nginx:1",
				Cpu:                aws.Int64(256),
				Memory:             aws.Int64(512),
				Environment:        &map[string]string{"B": "2", "A": "1"},
				Essential:          aws.Bool(true),
				VersionConsistency: aws.String("disabled"),
				PortMappings: &[]TaskDefinitionContainerPortMapping{{
					ContainerPort: aws.Int64(80),
					Protocol:      aws.String("tcp"),
					AppProtocol:   aws.String("http"),
				}},
				Ulimits: &[]TaskDefinitionContainerUlimit{{
					Name:      "nofile",
					HardLimit: 4096,
					SoftLimit: 1024,
				}},
				HealthCheck: &TaskDefinitionContainerHealthCheck{
					Command:  []string{"CMD-SHELL", "curl -f http://localhost/"},
					Interval: aws.Int64(10),
				},
				LogConfiguration: &TaskDefinitionContainerLogConfiguration{
					LogDriver: "awslogs",
					Options:   &map[string]string{"awslogs-group": "g"},
				},
			},
			expected: ecstypes.ContainerDefinition{
				Name:   aws.String("web"),
				Image:  aws.String("nginx:1"),
				Cpu:    256,
				Memory: aws.Int32(512),
				Environment: []ecstypes.KeyValuePair{
					{Name: aws.String("A"), Value: aws.String("1")},
					{Name: aws.String("B"), Value: aws.String("2")},
				},
				Essential:          aws.Bool(true),
				VersionConsistency: ecstypes.VersionConsistencyDisabled,
				PortMappings: []ecstypes.PortMapping{{
					ContainerPort: aws.Int32(80),
					Protocol:      ecstypes.TransportProtocolTcp,
					AppProtocol:   ecstypes.ApplicationProtocolHttp,
				}},
				Ulimits: []ecstypes.Ulimit{{
					Name:      ecstypes.UlimitNameNofile,
					HardLimit: 4096,
					SoftLimit: 1024,
				}},
				HealthCheck: &ecstypes.HealthCheck{
					Command:  []string{"CMD-SHELL", "curl -f http://localhost/"},
					Interval: aws.Int32(10),
				},
				LogConfiguration: &ecstypes.LogConfiguration{
					LogDriver: ecstypes.LogDriverAwslogs,
					Options:   map[string]string{"awslogs-group": "g"},
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

func TestTagsSDK(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected []ecstypes.Tag
	}{
		{name: "nil map", input: nil, expected: nil},
		{name: "empty map", input: map[string]string{}, expected: nil},
		{
			name:  "keys ordered",
			input: map[string]string{"b": "2", "a": "1"},
			expected: []ecstypes.Tag{
				{Key: aws.String("a"), Value: aws.String("1")},
				{Key: aws.String("b"), Value: aws.String("2")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 5 {
				assert.Equal(t, tt.expected, tagsSDK(tt.input))
			}
		})
	}
}
