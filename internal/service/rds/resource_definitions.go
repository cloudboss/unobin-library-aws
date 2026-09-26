package rds

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *ClusterInstanceResource) ResourceDefinition() runtime.ResourceDefinition[ClusterInstanceResource, *ClusterInstanceResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ClusterInstanceResource, *ClusterInstanceResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ClusterInstanceResource, *ClusterInstanceResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ClusterInstanceResource]{
				runtime.InputField(func(input *ClusterInstanceResource) **string { return &input.AvailabilityZone }),
				runtime.InputField(func(input *ClusterInstanceResource) *string { return &input.ClusterIdentifier }),
				runtime.InputField(func(input *ClusterInstanceResource) **string { return &input.CustomIamInstanceProfile }),
				runtime.InputField(func(input *ClusterInstanceResource) **string { return &input.DBSubnetGroupName }),
				runtime.InputField(func(input *ClusterInstanceResource) *string { return &input.Engine }),
				runtime.InputField(func(input *ClusterInstanceResource) *string { return &input.Identifier }),
			},
		},
	}
}

func (r *ClusterParameterGroupResource) ResourceDefinition() runtime.ResourceDefinition[ClusterParameterGroupResource, *ClusterParameterGroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ClusterParameterGroupResource, *ClusterParameterGroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ClusterParameterGroupResource, *ClusterParameterGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ClusterParameterGroupResource]{
				runtime.InputField(func(input *ClusterParameterGroupResource) *string { return &input.Name }),
				runtime.InputField(func(input *ClusterParameterGroupResource) *string { return &input.Family }),
				runtime.InputField(func(input *ClusterParameterGroupResource) *string { return &input.Description }),
			},
		},
	}
}

func (r *ClusterResource) ResourceDefinition() runtime.ResourceDefinition[ClusterResource, *ClusterResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ClusterResource, *ClusterResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ClusterResource, *ClusterResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ClusterResource]{
				runtime.InputField(func(input *ClusterResource) **[]string { return &input.AvailabilityZones }),
				runtime.InputField(func(input *ClusterResource) *string { return &input.ClusterIdentifier }),
				runtime.InputField(func(input *ClusterResource) **string { return &input.ClusterScalabilityType }),
				runtime.InputField(func(input *ClusterResource) **string { return &input.DatabaseName }),
				runtime.InputField(func(input *ClusterResource) **string { return &input.DbSubnetGroupName }),
				runtime.InputField(func(input *ClusterResource) **string { return &input.DbSystemId }),
				runtime.InputField(func(input *ClusterResource) *string { return &input.Engine }),
				runtime.InputField(func(input *ClusterResource) **string { return &input.EngineMode }),
				runtime.InputField(func(input *ClusterResource) **string { return &input.KmsKeyId }),
				runtime.InputField(func(input *ClusterResource) **string { return &input.MasterUsername }),
				runtime.InputField(func(input *ClusterResource) **ClusterRestoreToPointInTime { return &input.RestoreToPointInTime }),
				runtime.InputField(func(input *ClusterResource) **ClusterS3Import { return &input.S3Import }),
				runtime.InputField(func(input *ClusterResource) **string { return &input.SnapshotIdentifier }),
				runtime.InputField(func(input *ClusterResource) **string { return &input.SourceRegion }),
				runtime.InputField(func(input *ClusterResource) **bool { return &input.StorageEncrypted }),
			},
		},
	}
}

func (r *InstanceResource) ResourceDefinition() runtime.ResourceDefinition[InstanceResource, *InstanceResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[InstanceResource, *InstanceResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[InstanceResource, *InstanceResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[InstanceResource]{
				runtime.InputField(func(input *InstanceResource) **string { return &input.AvailabilityZone }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.BackupTarget }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.CharacterSetName }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.DbName }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.Engine }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.KmsKeyId }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.NcharCharacterSetName }),
				runtime.InputField(func(input *InstanceResource) **bool { return &input.StorageEncrypted }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.Timezone }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.Username }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.CustomIamInstanceProfile }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.SnapshotIdentifier }),
				runtime.InputField(func(input *InstanceResource) **InstanceRestoreToPointInTime { return &input.RestoreToPointInTime }),
				runtime.InputField(func(input *InstanceResource) **InstanceS3Import { return &input.S3Import }),
			},
		},
	}
}

func (r *ParameterGroupResource) ResourceDefinition() runtime.ResourceDefinition[ParameterGroupResource, *ParameterGroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ParameterGroupResource, *ParameterGroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ParameterGroupResource, *ParameterGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ParameterGroupResource]{
				runtime.InputField(func(input *ParameterGroupResource) *string { return &input.Name }),
				runtime.InputField(func(input *ParameterGroupResource) *string { return &input.Family }),
				runtime.InputField(func(input *ParameterGroupResource) *string { return &input.Description }),
			},
		},
	}
}

func (r *SubnetGroupResource) ResourceDefinition() runtime.ResourceDefinition[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[SubnetGroupResource, *SubnetGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SubnetGroupResource]{
				runtime.InputField(func(input *SubnetGroupResource) *string { return &input.Name }),
			},
		},
	}
}
