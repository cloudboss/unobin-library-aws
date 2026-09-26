package ec2

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *EipResource) ResourceDefinition() runtime.ResourceDefinition[EipResource, *EipResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[EipResource, *EipResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[EipResource, *EipResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[EipResource]{
				runtime.InputField(func(input *EipResource) **string { return &input.Address }),
				runtime.InputField(func(input *EipResource) **string { return &input.Domain }),
				runtime.InputField(func(input *EipResource) **string { return &input.IpamPoolId }),
				runtime.InputField(func(input *EipResource) **string { return &input.NetworkBorderGroup }),
				runtime.InputField(func(input *EipResource) **string { return &input.PublicIpv4Pool }),
				runtime.InputField(func(input *EipResource) **string { return &input.CustomerOwnedIpv4Pool }),
			},
		},
	}
}

func (r *InstanceResource) ResourceDefinition() runtime.ResourceDefinition[InstanceResource, *InstanceResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[InstanceResource, *InstanceResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[InstanceResource, *InstanceResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[InstanceResource]{
				runtime.InputField(func(input *InstanceResource) **string { return &input.Ami }),
				runtime.InputField(func(input *InstanceResource) **bool { return &input.AssociatePublicIpAddress }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.AvailabilityZone }),
				runtime.InputField(func(input *InstanceResource) **[]InstanceEbsBlockDevice { return &input.EbsBlockDevice }),
				runtime.InputField(func(input *InstanceResource) **[]InstanceEphemeralBlockDevice { return &input.EphemeralBlockDevice }),
				runtime.InputField(func(input *InstanceResource) **bool { return &input.EbsOptimized }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.KeyName }),
				runtime.InputField(func(input *InstanceResource) **InstanceLaunchTemplate { return &input.LaunchTemplate }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.PrivateIp }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.SubnetId }),
				runtime.InputField(func(input *InstanceResource) **string { return &input.Tenancy }),
			},
		},
	}
}

func (r *InternetGatewayResource) ResourceDefinition() runtime.ResourceDefinition[InternetGatewayResource, *InternetGatewayResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[InternetGatewayResource, *InternetGatewayResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
	}
}

func (r *KeyPairResource) ResourceDefinition() runtime.ResourceDefinition[KeyPairResource, *KeyPairResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[KeyPairResource, *KeyPairResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[KeyPairResource, *KeyPairResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[KeyPairResource]{
				runtime.InputField(func(input *KeyPairResource) *string { return &input.KeyName }),
				runtime.InputField(func(input *KeyPairResource) *string { return &input.PublicKey }),
			},
		},
	}
}

func (r *LaunchTemplateResource) ResourceDefinition() runtime.ResourceDefinition[LaunchTemplateResource, *LaunchTemplateResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[LaunchTemplateResource, *LaunchTemplateResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[LaunchTemplateResource, *LaunchTemplateResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[LaunchTemplateResource]{
				runtime.InputField(func(input *LaunchTemplateResource) *string { return &input.Name }),
			},
		},
	}
}

func (r *NatGatewayResource) ResourceDefinition() runtime.ResourceDefinition[NatGatewayResource, *NatGatewayResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[NatGatewayResource, *NatGatewayResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[NatGatewayResource, *NatGatewayResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[NatGatewayResource]{
				runtime.InputField(func(input *NatGatewayResource) **string { return &input.AllocationId }),
				runtime.InputField(func(input *NatGatewayResource) **string { return &input.ConnectivityType }),
				runtime.InputField(func(input *NatGatewayResource) **string { return &input.PrivateIp }),
				runtime.InputField(func(input *NatGatewayResource) *string { return &input.SubnetId }),
				runtime.InputField(func(input *NatGatewayResource) **int64 { return &input.SecondaryPrivateIpAddressCount }),
			},
		},
	}
}

func (r *RouteResource) ResourceDefinition() runtime.ResourceDefinition[RouteResource, *RouteResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RouteResource, *RouteResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RouteResource, *RouteResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RouteResource]{
				runtime.InputField(func(input *RouteResource) *string { return &input.RouteTableId }),
				runtime.InputField(func(input *RouteResource) **string { return &input.DestinationCidrBlock }),
				runtime.InputField(func(input *RouteResource) **string { return &input.DestinationIpv6CidrBlock }),
				runtime.InputField(func(input *RouteResource) **string { return &input.DestinationPrefixListId }),
			},
		},
	}
}

func (r *RouteTableAssociationResource) ResourceDefinition() runtime.ResourceDefinition[RouteTableAssociationResource, *RouteTableAssociationResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RouteTableAssociationResource, *RouteTableAssociationResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RouteTableAssociationResource, *RouteTableAssociationResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RouteTableAssociationResource]{
				runtime.InputField(func(input *RouteTableAssociationResource) **string { return &input.SubnetId }),
				runtime.InputField(func(input *RouteTableAssociationResource) **string { return &input.GatewayId }),
			},
		},
	}
}

func (r *RouteTableResource) ResourceDefinition() runtime.ResourceDefinition[RouteTableResource, *RouteTableResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[RouteTableResource, *RouteTableResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[RouteTableResource, *RouteTableResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RouteTableResource]{
				runtime.InputField(func(input *RouteTableResource) *string { return &input.VpcId }),
			},
		},
	}
}

func (r *SecurityGroupEgressRuleResource) ResourceDefinition() runtime.ResourceDefinition[SecurityGroupEgressRuleResource, *SecurityGroupEgressRuleResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[SecurityGroupEgressRuleResource, *SecurityGroupEgressRuleResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[SecurityGroupEgressRuleResource, *SecurityGroupEgressRuleResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SecurityGroupEgressRuleResource]{
				runtime.InputField(func(input *SecurityGroupEgressRuleResource) *string { return &input.SecurityGroupId }),
				runtime.InputField(func(input *SecurityGroupEgressRuleResource) **string { return &input.CidrIpv4 }),
				runtime.InputField(func(input *SecurityGroupEgressRuleResource) **string { return &input.CidrIpv6 }),
				runtime.InputField(func(input *SecurityGroupEgressRuleResource) **string { return &input.PrefixListId }),
				runtime.InputField(func(input *SecurityGroupEgressRuleResource) **string { return &input.ReferencedSecurityGroupId }),
			},
		},
	}
}

func (r *SecurityGroupIngressRuleResource) ResourceDefinition() runtime.ResourceDefinition[SecurityGroupIngressRuleResource, *SecurityGroupIngressRuleResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[SecurityGroupIngressRuleResource, *SecurityGroupIngressRuleResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[SecurityGroupIngressRuleResource, *SecurityGroupIngressRuleResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SecurityGroupIngressRuleResource]{
				runtime.InputField(func(input *SecurityGroupIngressRuleResource) *string { return &input.SecurityGroupId }),
				runtime.InputField(func(input *SecurityGroupIngressRuleResource) **string { return &input.CidrIpv4 }),
				runtime.InputField(func(input *SecurityGroupIngressRuleResource) **string { return &input.CidrIpv6 }),
				runtime.InputField(func(input *SecurityGroupIngressRuleResource) **string { return &input.PrefixListId }),
				runtime.InputField(func(input *SecurityGroupIngressRuleResource) **string { return &input.ReferencedSecurityGroupId }),
			},
		},
	}
}

func (r *SecurityGroupResource) ResourceDefinition() runtime.ResourceDefinition[SecurityGroupResource, *SecurityGroupResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[SecurityGroupResource, *SecurityGroupResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[SecurityGroupResource, *SecurityGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SecurityGroupResource]{
				runtime.InputField(func(input *SecurityGroupResource) **string { return &input.Name }),
				runtime.InputField(func(input *SecurityGroupResource) **string { return &input.NamePrefix }),
				runtime.InputField(func(input *SecurityGroupResource) *string { return &input.Description }),
				runtime.InputField(func(input *SecurityGroupResource) **string { return &input.VpcId }),
			},
		},
	}
}

func (r *SubnetResource) ResourceDefinition() runtime.ResourceDefinition[SubnetResource, *SubnetResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[SubnetResource, *SubnetResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[SubnetResource, *SubnetResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SubnetResource]{
				runtime.InputField(func(input *SubnetResource) *string { return &input.VpcId }),
				runtime.InputField(func(input *SubnetResource) **string { return &input.CidrBlock }),
				runtime.InputField(func(input *SubnetResource) **string { return &input.AvailabilityZone }),
				runtime.InputField(func(input *SubnetResource) **string { return &input.AvailabilityZoneId }),
				runtime.InputField(func(input *SubnetResource) **string { return &input.OutpostArn }),
				runtime.InputField(func(input *SubnetResource) **string { return &input.Ipv4IpamPoolId }),
				runtime.InputField(func(input *SubnetResource) **int64 { return &input.Ipv4NetmaskLength }),
				runtime.InputField(func(input *SubnetResource) **string { return &input.Ipv6IpamPoolId }),
				runtime.InputField(func(input *SubnetResource) **bool { return &input.Ipv6Native }),
				runtime.InputField(func(input *SubnetResource) **int64 { return &input.Ipv6NetmaskLength }),
				runtime.InputField(func(input *SubnetResource) **string { return &input.Ipv6CidrBlock }),
			},
		},
	}
}

func (r *VolumeResource) ResourceDefinition() runtime.ResourceDefinition[VolumeResource, *VolumeResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[VolumeResource, *VolumeResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[VolumeResource, *VolumeResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[VolumeResource]{
				runtime.InputField(func(input *VolumeResource) *string { return &input.AvailabilityZone }),
				runtime.InputField(func(input *VolumeResource) **bool { return &input.Encrypted }),
				runtime.InputField(func(input *VolumeResource) **string { return &input.KmsKeyId }),
				runtime.InputField(func(input *VolumeResource) **bool { return &input.MultiAttachEnabled }),
				runtime.InputField(func(input *VolumeResource) **string { return &input.OutpostArn }),
				runtime.InputField(func(input *VolumeResource) **string { return &input.SnapshotId }),
			},
		},
	}
}

func (r *VpcEndpointResource) ResourceDefinition() runtime.ResourceDefinition[VpcEndpointResource, *VpcEndpointResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[VpcEndpointResource, *VpcEndpointResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[VpcEndpointResource, *VpcEndpointResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[VpcEndpointResource]{
				runtime.InputField(func(input *VpcEndpointResource) *string { return &input.VpcId }),
				runtime.InputField(func(input *VpcEndpointResource) *string { return &input.ServiceName }),
				runtime.InputField(func(input *VpcEndpointResource) **string { return &input.VpcEndpointType }),
			},
		},
	}
}

func (r *VpcResource) ResourceDefinition() runtime.ResourceDefinition[VpcResource, *VpcResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[VpcResource, *VpcResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[VpcResource, *VpcResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[VpcResource]{
				runtime.InputField(func(input *VpcResource) **string { return &input.CidrBlock }),
				runtime.InputField(func(input *VpcResource) **string { return &input.InstanceTenancy }),
				runtime.InputField(func(input *VpcResource) **bool { return &input.AmazonProvidedIpv6CidrBlock }),
				runtime.InputField(func(input *VpcResource) **string { return &input.Ipv4IpamPoolId }),
				runtime.InputField(func(input *VpcResource) **int64 { return &input.Ipv4NetmaskLength }),
				runtime.InputField(func(input *VpcResource) **string { return &input.Ipv6CidrBlock }),
				runtime.InputField(func(input *VpcResource) **string { return &input.Ipv6CidrBlockNetworkBorderGroup }),
				runtime.InputField(func(input *VpcResource) **string { return &input.Ipv6IpamPoolId }),
				runtime.InputField(func(input *VpcResource) **int64 { return &input.Ipv6NetmaskLength }),
			},
		},
	}
}
