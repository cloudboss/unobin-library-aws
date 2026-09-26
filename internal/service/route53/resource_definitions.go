package route53

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *HostedZoneResource) ResourceDefinition() runtime.ResourceDefinition[HostedZoneResource, *HostedZoneResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[HostedZoneResource, *HostedZoneResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[HostedZoneResource, *HostedZoneResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[HostedZoneResource]{
				runtime.InputField(func(input *HostedZoneResource) *string { return &input.Name }),
				runtime.InputField(func(input *HostedZoneResource) **string { return &input.DelegationSetId }),
			},
		},
	}
}

func (r *RecordSetResource) ResourceDefinition() runtime.ResourceDefinition[RecordSetResource, *RecordSetResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RecordSetResource, *RecordSetResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RecordSetResource, *RecordSetResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RecordSetResource]{
				runtime.InputField(func(input *RecordSetResource) *string { return &input.ZoneId }),
				runtime.InputField(func(input *RecordSetResource) *string { return &input.Name }),
				runtime.InputField(func(input *RecordSetResource) *string { return &input.Type }),
				runtime.InputField(func(input *RecordSetResource) **string { return &input.SetIdentifier }),
			},
		},
	}
}
