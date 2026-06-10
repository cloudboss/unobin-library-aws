package ecs

import (
	"maps"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// TaskDefinitionEphemeralStorage expands the ephemeral storage available to
// the task beyond the default, for tasks hosted on Fargate. The size is in
// GiB, between 21 and 200.
type TaskDefinitionEphemeralStorage struct {
	SizeInGiB int64 `ub:"size-in-gib"`
}

// TaskDefinitionPlacementConstraint is one placement rule for tasks run from
// this task definition, at most 10 per task definition. The only type ECS
// accepts here is memberOf, which requires a cluster query language
// expression naming the group of valid candidates.
type TaskDefinitionPlacementConstraint struct {
	Type       string  `ub:"type"`
	Expression *string `ub:"expression"`
}

// TaskDefinitionProxyConfiguration is the App Mesh proxy configuration for
// the task. The container name names the proxy container in
// container-definitions; properties is the set of network configuration
// parameters handed to the Container Network Interface plugin, sent to the
// API as its key-value pair list ordered by name. The only type is APPMESH,
// which is also the server default.
type TaskDefinitionProxyConfiguration struct {
	ContainerName string             `ub:"container-name"`
	Properties    *map[string]string `ub:"properties"`
	Type          *string            `ub:"type"`
}

// TaskDefinitionRuntimePlatform is the operating system and CPU architecture
// the task runs on: cpu-architecture is X86_64 or ARM64, and
// operating-system-family is LINUX or a WINDOWS_SERVER family.
type TaskDefinitionRuntimePlatform struct {
	CpuArchitecture       *string `ub:"cpu-architecture"`
	OperatingSystemFamily *string `ub:"operating-system-family"`
}

// sdk converts the ephemeral storage block to its SDK type, returning nil
// for a nil block so an absent block stays out of the request.
func (e *TaskDefinitionEphemeralStorage) sdk() *ecstypes.EphemeralStorage {
	if e == nil {
		return nil
	}
	return &ecstypes.EphemeralStorage{SizeInGiB: int32(e.SizeInGiB)}
}

// taskDefinitionPlacementConstraintsSDK converts the placement constraint
// list to its SDK type, returning nil for an empty list so the member stays
// out of the request.
func taskDefinitionPlacementConstraintsSDK(
	constraints []TaskDefinitionPlacementConstraint,
) []ecstypes.TaskDefinitionPlacementConstraint {
	if len(constraints) == 0 {
		return nil
	}
	out := make([]ecstypes.TaskDefinitionPlacementConstraint, 0, len(constraints))
	for _, c := range constraints {
		out = append(out, ecstypes.TaskDefinitionPlacementConstraint{
			Type:       ecstypes.TaskDefinitionPlacementConstraintType(c.Type),
			Expression: c.Expression,
		})
	}
	return out
}

// sdk converts the proxy configuration block to its SDK type, returning nil
// for a nil block so an absent block stays out of the request.
func (p *TaskDefinitionProxyConfiguration) sdk() *ecstypes.ProxyConfiguration {
	if p == nil {
		return nil
	}
	out := &ecstypes.ProxyConfiguration{
		ContainerName: aws.String(p.ContainerName),
		Properties:    taskDefinitionKeyValuePairs(p.Properties),
	}
	if p.Type != nil {
		out.Type = ecstypes.ProxyConfigurationType(*p.Type)
	}
	return out
}

// sdk converts the runtime platform block to its SDK type, returning nil for
// a nil block so an absent block stays out of the request.
func (p *TaskDefinitionRuntimePlatform) sdk() *ecstypes.RuntimePlatform {
	if p == nil {
		return nil
	}
	out := &ecstypes.RuntimePlatform{}
	if p.CpuArchitecture != nil {
		out.CpuArchitecture = ecstypes.CPUArchitecture(*p.CpuArchitecture)
	}
	if p.OperatingSystemFamily != nil {
		out.OperatingSystemFamily = ecstypes.OSFamily(*p.OperatingSystemFamily)
	}
	return out
}

// taskDefinitionKeyValuePairs converts an optional map input into the SDK's
// key-value pair list, ordered by name so requests are deterministic. A nil
// or empty map returns nil so the member stays out of the request.
func taskDefinitionKeyValuePairs(m *map[string]string) []ecstypes.KeyValuePair {
	if m == nil || len(*m) == 0 {
		return nil
	}
	out := make([]ecstypes.KeyValuePair, 0, len(*m))
	for _, k := range slices.Sorted(maps.Keys(*m)) {
		out = append(out, ecstypes.KeyValuePair{
			Name:  aws.String(k),
			Value: aws.String((*m)[k]),
		})
	}
	return out
}
