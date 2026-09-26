package lambda

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *AliasResource) ResourceDefinition() runtime.ResourceDefinition[AliasResource, *AliasResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[AliasResource, *AliasResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Equality: []runtime.InputEqualityRule[AliasResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *AliasResource) *string { return &input.FunctionName }),
				func(prior, desired string) bool {
					return r.EquivalentInput("function-name", AliasResource{FunctionName: prior}, AliasResource{FunctionName: desired})
				},
			),
		},
		Replace: runtime.Replacement[AliasResource, *AliasResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[AliasResource]{
				runtime.InputField(func(input *AliasResource) *string { return &input.FunctionName }),
				runtime.InputField(func(input *AliasResource) *string { return &input.Name }),
			},
		},
	}
}

func (r *EventSourceMappingResource) ResourceDefinition() runtime.ResourceDefinition[EventSourceMappingResource, *EventSourceMappingResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[EventSourceMappingResource, *EventSourceMappingResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[EventSourceMappingResource, *EventSourceMappingResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[EventSourceMappingResource]{
				runtime.InputField(func(input *EventSourceMappingResource) **string { return &input.EventSourceArn }),
				runtime.InputField(func(input *EventSourceMappingResource) **EventSourceMappingSelfManagedEventSource {
					return &input.SelfManagedEventSource
				}),
				runtime.InputField(func(input *EventSourceMappingResource) **EventSourceMappingAmazonManagedKafka {
					return &input.AmazonManagedKafkaEventSourceConfig
				}),
				runtime.InputField(func(input *EventSourceMappingResource) **EventSourceMappingSelfManagedKafka {
					return &input.SelfManagedKafkaEventSourceConfig
				}),
				runtime.InputField(func(input *EventSourceMappingResource) **string { return &input.StartingPosition }),
				runtime.InputField(func(input *EventSourceMappingResource) **string { return &input.StartingPositionTimestamp }),
				runtime.InputField(func(input *EventSourceMappingResource) **[]string { return &input.Queues }),
				runtime.InputField(func(input *EventSourceMappingResource) **[]string { return &input.Topics }),
			},
		},
	}
}

func (r *FunctionResource) ResourceDefinition() runtime.ResourceDefinition[FunctionResource, *FunctionResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[FunctionResource, *FunctionResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[FunctionResource, *FunctionResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[FunctionResource]{
				runtime.InputField(func(input *FunctionResource) *string { return &input.FunctionName }),
				runtime.InputField(func(input *FunctionResource) **string { return &input.PackageType }),
			},
		},
	}
}

func (r *FunctionUrlResource) ResourceDefinition() runtime.ResourceDefinition[FunctionUrlResource, *FunctionUrlResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[FunctionUrlResource, *FunctionUrlResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[FunctionUrlResource, *FunctionUrlResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[FunctionUrlResource]{
				runtime.InputField(func(input *FunctionUrlResource) *string { return &input.FunctionName }),
				runtime.InputField(func(input *FunctionUrlResource) **string { return &input.Qualifier }),
			},
		},
	}
}

func (r *PermissionResource) ResourceDefinition() runtime.ResourceDefinition[PermissionResource, *PermissionResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[PermissionResource, *PermissionResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[PermissionResource, *PermissionResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[PermissionResource]{
				runtime.InputField(func(input *PermissionResource) *string { return &input.Action }),
				runtime.InputField(func(input *PermissionResource) *string { return &input.FunctionName }),
				runtime.InputField(func(input *PermissionResource) *string { return &input.Principal }),
				runtime.InputField(func(input *PermissionResource) **string { return &input.StatementId }),
				runtime.InputField(func(input *PermissionResource) **string { return &input.Qualifier }),
				runtime.InputField(func(input *PermissionResource) **string { return &input.EventSourceToken }),
				runtime.InputField(func(input *PermissionResource) **string { return &input.FunctionUrlAuthType }),
				runtime.InputField(func(input *PermissionResource) **bool { return &input.InvokedViaFunctionUrl }),
				runtime.InputField(func(input *PermissionResource) **string { return &input.PrincipalOrgID }),
				runtime.InputField(func(input *PermissionResource) **string { return &input.SourceAccount }),
				runtime.InputField(func(input *PermissionResource) **string { return &input.SourceArn }),
			},
		},
	}
}
