package meta

import (
	"context"
	"errors"
)

// ServicePrincipal builds the service principal name for a service in the
// configured region's partition.
type ServicePrincipal struct {
	Region      *string `ub:"region"`
	ServiceName string  `ub:"service-name"`
}

// ServicePrincipalOutput contains the principal name and domain suffix.
type ServicePrincipalOutput struct {
	Name   string `ub:"name"`
	Region string `ub:"region"`
	Suffix string `ub:"suffix"`
}

func (d *ServicePrincipal) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*ServicePrincipalOutput, error) {
	if d.ServiceName == "" {
		return nil, errors.New("service-name must not be empty")
	}
	region := stringValue(d.Region)
	if region == "" {
		var err error
		region, err = configuredRegion(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	info, err := findRegionByName(region)
	if err != nil {
		return nil, err
	}
	suffix := servicePrincipalSuffix(d.ServiceName, info.Partition.ID(), info.Partition.DNSSuffix())
	return &ServicePrincipalOutput{
		Name:   d.ServiceName + "." + suffix,
		Region: region,
		Suffix: suffix,
	}, nil
}

func servicePrincipalSuffix(service, partitionID, dnsSuffix string) string {
	if service == "" || partitionID == "aws" {
		return "amazonaws.com"
	}
	switch partitionID {
	case "aws-iso":
		switch service {
		case "cloudhsm", "config", "logs", "workspaces":
			return dnsSuffix
		}
	case "aws-iso-b":
		switch service {
		case "dms", "logs":
			return dnsSuffix
		}
	case "aws-cn":
		switch service {
		case "codedeploy", "elasticmapreduce", "logs", "ec2", "s3":
			return dnsSuffix
		}
	}
	return "amazonaws.com"
}
