package ecs

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *CapacityProviderResource) ResourceDefinition() runtime.ResourceDefinition[CapacityProviderResource, *CapacityProviderResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[CapacityProviderResource, *CapacityProviderResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[CapacityProviderResource, *CapacityProviderResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[CapacityProviderResource]{
				runtime.InputField(func(input *CapacityProviderResource) *string { return &input.Name }),
				runtime.InputField(func(input *CapacityProviderResource) **string { return &input.Cluster }),
			},
		},
	}
}

func (r *ClusterResource) ResourceDefinition() runtime.ResourceDefinition[ClusterResource, *ClusterResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ClusterResource, *ClusterResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ClusterResource, *ClusterResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ClusterResource]{
				runtime.InputField(func(input *ClusterResource) *string { return &input.Name }),
			},
		},
	}
}

func (r *ServiceResource) ResourceDefinition() runtime.ResourceDefinition[ServiceResource, *ServiceResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ServiceResource, *ServiceResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ServiceResource, *ServiceResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ServiceResource]{
				runtime.InputField(func(input *ServiceResource) **string { return &input.Cluster }),
				runtime.InputField(func(input *ServiceResource) *string { return &input.Name }),
				runtime.InputField(func(input *ServiceResource) **string { return &input.SchedulingStrategy }),
				runtime.InputField(func(input *ServiceResource) **string { return &input.LaunchType }),
			},
		},
	}
}

func (r *TaskDefinitionResource) ResourceDefinition() runtime.ResourceDefinition[TaskDefinitionResource, *TaskDefinitionResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[TaskDefinitionResource, *TaskDefinitionResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[TaskDefinitionResource, *TaskDefinitionResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[TaskDefinitionResource]{
				runtime.InputField(func(input *TaskDefinitionResource) *string { return &input.Family }),
				runtime.InputField(func(input *TaskDefinitionResource) *[]TaskDefinitionContainerDefinition {
					return &input.ContainerDefinitions
				}),
				runtime.InputField(func(input *TaskDefinitionResource) **string { return &input.Cpu }),
				runtime.InputField(func(input *TaskDefinitionResource) **bool { return &input.EnableFaultInjection }),
				runtime.InputField(func(input *TaskDefinitionResource) **TaskDefinitionEphemeralStorage { return &input.EphemeralStorage }),
				runtime.InputField(func(input *TaskDefinitionResource) **string { return &input.ExecutionRoleArn }),
				runtime.InputField(func(input *TaskDefinitionResource) **string { return &input.IpcMode }),
				runtime.InputField(func(input *TaskDefinitionResource) **string { return &input.Memory }),
				runtime.InputField(func(input *TaskDefinitionResource) **string { return &input.NetworkMode }),
				runtime.InputField(func(input *TaskDefinitionResource) **string { return &input.PidMode }),
				runtime.InputField(func(input *TaskDefinitionResource) **[]TaskDefinitionPlacementConstraint {
					return &input.PlacementConstraints
				}),
				runtime.InputField(func(input *TaskDefinitionResource) **TaskDefinitionProxyConfiguration {
					return &input.ProxyConfiguration
				}),
				runtime.InputField(func(input *TaskDefinitionResource) **[]string { return &input.RequiresCompatibilities }),
				runtime.InputField(func(input *TaskDefinitionResource) **TaskDefinitionRuntimePlatform { return &input.RuntimePlatform }),
				runtime.InputField(func(input *TaskDefinitionResource) **string { return &input.TaskRoleArn }),
				runtime.InputField(func(input *TaskDefinitionResource) **[]TaskDefinitionVolume { return &input.Volumes }),
			},
		},
	}
}
