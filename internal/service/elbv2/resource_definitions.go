package elbv2

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *ListenerCertificateResource) ResourceDefinition() runtime.ResourceDefinition[ListenerCertificateResource, *ListenerCertificateResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ListenerCertificateResource, *ListenerCertificateResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ListenerCertificateResource, *ListenerCertificateResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ListenerCertificateResource]{
				runtime.InputField(func(input *ListenerCertificateResource) *string { return &input.ListenerArn }),
				runtime.InputField(func(input *ListenerCertificateResource) *string { return &input.CertificateArn }),
			},
		},
	}
}

func (r *ListenerResource) ResourceDefinition() runtime.ResourceDefinition[ListenerResource, *ListenerResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ListenerResource, *ListenerResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ListenerResource, *ListenerResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ListenerResource]{
				runtime.InputField(func(input *ListenerResource) *string { return &input.LoadBalancerArn }),
			},
		},
	}
}

func (r *ListenerRuleResource) ResourceDefinition() runtime.ResourceDefinition[ListenerRuleResource, *ListenerRuleResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[ListenerRuleResource, *ListenerRuleResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[ListenerRuleResource, *ListenerRuleResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ListenerRuleResource]{
				runtime.InputField(func(input *ListenerRuleResource) *string { return &input.ListenerArn }),
			},
		},
	}
}

func (r *LoadBalancerResource) ResourceDefinition() runtime.ResourceDefinition[LoadBalancerResource, *LoadBalancerResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[LoadBalancerResource, *LoadBalancerResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[LoadBalancerResource, *LoadBalancerResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[LoadBalancerResource]{
				runtime.InputField(func(input *LoadBalancerResource) *string { return &input.Name }),
				runtime.InputField(func(input *LoadBalancerResource) **bool { return &input.Internal }),
				runtime.InputField(func(input *LoadBalancerResource) **string { return &input.LoadBalancerType }),
				runtime.InputField(func(input *LoadBalancerResource) **string { return &input.CustomerOwnedIpv4Pool }),
			},
		},
	}
}

func (r *TargetGroupAttachmentResource) ResourceDefinition() runtime.ResourceDefinition[TargetGroupAttachmentResource, *TargetGroupAttachmentResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[TargetGroupAttachmentResource, *TargetGroupAttachmentResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[TargetGroupAttachmentResource, *TargetGroupAttachmentResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[TargetGroupAttachmentResource]{
				runtime.InputField(func(input *TargetGroupAttachmentResource) *string { return &input.TargetGroupArn }),
				runtime.InputField(func(input *TargetGroupAttachmentResource) *string { return &input.TargetId }),
				runtime.InputField(func(input *TargetGroupAttachmentResource) **string { return &input.AvailabilityZone }),
				runtime.InputField(func(input *TargetGroupAttachmentResource) **int64 { return &input.Port }),
				runtime.InputField(func(input *TargetGroupAttachmentResource) **string { return &input.QuicServerId }),
			},
		},
	}
}

func (r *TargetGroupResource) ResourceDefinition() runtime.ResourceDefinition[TargetGroupResource, *TargetGroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[TargetGroupResource, *TargetGroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[TargetGroupResource, *TargetGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[TargetGroupResource]{
				runtime.InputField(func(input *TargetGroupResource) *string { return &input.Name }),
				runtime.InputField(func(input *TargetGroupResource) **int64 { return &input.Port }),
				runtime.InputField(func(input *TargetGroupResource) **string { return &input.Protocol }),
				runtime.InputField(func(input *TargetGroupResource) **string { return &input.ProtocolVersion }),
				runtime.InputField(func(input *TargetGroupResource) **string { return &input.VpcId }),
				runtime.InputField(func(input *TargetGroupResource) **string { return &input.TargetType }),
				runtime.InputField(func(input *TargetGroupResource) **string { return &input.IpAddressType }),
				runtime.InputField(func(input *TargetGroupResource) **int64 { return &input.TargetControlPort }),
			},
		},
	}
}
