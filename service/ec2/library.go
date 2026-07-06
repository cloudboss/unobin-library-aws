package ec2

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/ec2"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
}

type dataSourcePtr[T, Out any] interface {
	*T
	runtime.TypedDataSource[Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT]()
}

func makeDataSource[T, Out any, PT dataSourcePtr[T, Out]]() runtime.DataSourceRegistration {
	return runtime.MakeDataSource[T, Out, *awscfg.Configuration, PT]()
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-ec2",
		Description:   "AWS EC2 library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"vpc":            makeResource[svc.VpcResource, *svc.VpcResourceOutput](),
			"security-group": makeResource[svc.SecurityGroupResource, *svc.SecurityGroupResourceOutput](),
			"security-group-ingress-rule": makeResource[
				svc.SecurityGroupIngressRuleResource, *svc.SecurityGroupIngressRuleResourceOutput](),
			"security-group-egress-rule": makeResource[
				svc.SecurityGroupEgressRuleResource, *svc.SecurityGroupEgressRuleResourceOutput](),
			"subnet": makeResource[svc.SubnetResource, *svc.SubnetResourceOutput](),
			"volume": makeResource[svc.VolumeResource, *svc.VolumeResourceOutput](),
			"launch-template": makeResource[
				svc.LaunchTemplateResource,
				*svc.LaunchTemplateResourceOutput](),
			"internet-gateway": makeResource[
				svc.InternetGatewayResource,
				*svc.InternetGatewayResourceOutput](),
			"route-table": makeResource[svc.RouteTableResource, *svc.RouteTableResourceOutput](),
			"route":       makeResource[svc.RouteResource, *svc.RouteResourceOutput](),
			"route-table-association": makeResource[
				svc.RouteTableAssociationResource, *svc.RouteTableAssociationResourceOutput](),
			"eip":          makeResource[svc.EipResource, *svc.EipResourceOutput](),
			"nat-gateway":  makeResource[svc.NatGatewayResource, *svc.NatGatewayResourceOutput](),
			"vpc-endpoint": makeResource[svc.VpcEndpointResource, *svc.VpcEndpointResourceOutput](),
			"key-pair":     makeResource[svc.KeyPairResource, *svc.KeyPairResourceOutput](),
			"instance":     makeResource[svc.InstanceResource, *svc.InstanceResourceOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"ami": makeDataSource[svc.AMIDataSource, *svc.AMIDataSourceOutput](),
			"availability-zones": makeDataSource[
				svc.AvailabilityZonesDataSource,
				*svc.AvailabilityZonesDataSourceOutput](),
			"subnets": makeDataSource[svc.SubnetsDataSource, *svc.SubnetsDataSourceOutput](),
			"subnet":  makeDataSource[svc.SubnetDataSource, *svc.SubnetDataSourceOutput](),
			"security-group": makeDataSource[
				svc.SecurityGroupDataSource,
				*svc.SecurityGroupDataSourceOutput](),
			"vpc": makeDataSource[svc.VpcDataSource, *svc.VpcDataSourceOutput](),
		},
	}
}
