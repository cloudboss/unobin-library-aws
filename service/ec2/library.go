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
			"vpc":            makeResource[svc.Vpc, *svc.VpcOutput](),
			"security-group": makeResource[svc.SecurityGroup, *svc.SecurityGroupOutput](),
			"security-group-ingress-rule": makeResource[
				svc.SecurityGroupIngressRule, *svc.SecurityGroupIngressRuleOutput](),
			"security-group-egress-rule": makeResource[
				svc.SecurityGroupEgressRule, *svc.SecurityGroupEgressRuleOutput](),
			"subnet":           makeResource[svc.Subnet, *svc.SubnetOutput](),
			"volume":           makeResource[svc.Volume, *svc.VolumeOutput](),
			"launch-template":  makeResource[svc.LaunchTemplate, *svc.LaunchTemplateOutput](),
			"internet-gateway": makeResource[svc.InternetGateway, *svc.InternetGatewayOutput](),
			"route-table":      makeResource[svc.RouteTable, *svc.RouteTableOutput](),
			"route":            makeResource[svc.Route, *svc.RouteOutput](),
			"route-table-association": makeResource[
				svc.RouteTableAssociation, *svc.RouteTableAssociationOutput](),
			"eip":          makeResource[svc.Eip, *svc.EipOutput](),
			"nat-gateway":  makeResource[svc.NatGateway, *svc.NatGatewayOutput](),
			"vpc-endpoint": makeResource[svc.VpcEndpoint, *svc.VpcEndpointOutput](),
			"key-pair":     makeResource[svc.KeyPair, *svc.KeyPairOutput](),
			"instance":     makeResource[svc.Instance, *svc.InstanceOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"ami":                 makeDataSource[svc.AMI, *svc.AMIOutput](),
			"availability-zones":  makeDataSource[svc.AvailabilityZones, *svc.AvailabilityZonesOutput](),
			"subnets":             makeDataSource[svc.Subnets, *svc.SubnetsOutput](),
			"subnet-data":         makeDataSource[svc.SubnetData, *svc.SubnetDataOutput](),
			"security-group-data": makeDataSource[svc.SecurityGroupData, *svc.SecurityGroupDataOutput](),
			"vpc-data":            makeDataSource[svc.VpcData, *svc.VpcDataOutput](),
		},
	}
}
