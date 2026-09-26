package efs

import (
	"context"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *AccessPointResource) ResourceDefinition() runtime.ResourceDefinition[AccessPointResource, *AccessPointResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[AccessPointResource, *AccessPointResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input AccessPointResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[AccessPointResource, *AccessPointResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[AccessPointResource]{
				runtime.InputField(func(input *AccessPointResource) *string { return &input.FileSystemId }),
				runtime.InputField(func(input *AccessPointResource) **string { return &input.ClientToken }),
				runtime.InputField(func(input *AccessPointResource) **AccessPointPosixUser { return &input.PosixUser }),
				runtime.InputField(func(input *AccessPointResource) **AccessPointRootDirectory { return &input.RootDirectory }),
			},
		},
	}
}

func (r *FileSystemResource) ResourceDefinition() runtime.ResourceDefinition[FileSystemResource, *FileSystemResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[FileSystemResource, *FileSystemResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input FileSystemResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[FileSystemResource, *FileSystemResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[FileSystemResource]{
				runtime.InputField(func(input *FileSystemResource) **string { return &input.AvailabilityZoneName }),
				runtime.InputField(func(input *FileSystemResource) **bool { return &input.Encrypted }),
				runtime.InputField(func(input *FileSystemResource) **string { return &input.KmsKeyId }),
				runtime.InputField(func(input *FileSystemResource) **string { return &input.PerformanceMode }),
			},
		},
	}
}

func (r *MountTargetResource) ResourceDefinition() runtime.ResourceDefinition[MountTargetResource, *MountTargetResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[MountTargetResource, *MountTargetResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Validate: func(ctx context.Context, input MountTargetResource, cfg *awsCfg) error {
			return (&input).ValidateInputs(ctx, cfg)
		},
		Replace: runtime.Replacement[MountTargetResource, *MountTargetResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[MountTargetResource]{
				runtime.InputField(func(input *MountTargetResource) *string { return &input.FileSystemId }),
				runtime.InputField(func(input *MountTargetResource) *string { return &input.SubnetId }),
				runtime.InputField(func(input *MountTargetResource) **string { return &input.IpAddress }),
				runtime.InputField(func(input *MountTargetResource) **string { return &input.IpAddressType }),
				runtime.InputField(func(input *MountTargetResource) **string { return &input.Ipv6Address }),
			},
		},
	}
}
