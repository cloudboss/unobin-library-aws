package ec2

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

type Vpc struct {
	CidrBlock                       *string `ub:"cidr-block"`
	InstanceTenancy                 *string `ub:"instance-tenancy"`
	AmazonProvidedIpv6CidrBlock     *bool   `ub:"amazon-provided-ipv6-cidr-block"`
	Ipv4IpamPoolId                  *string `ub:"ipv4-ipam-pool-id"`
	Ipv4NetmaskLength               *int64  `ub:"ipv4-netmask-length"`
	Ipv6CidrBlock                   *string `ub:"ipv6-cidr-block"`
	Ipv6CidrBlockNetworkBorderGroup *string `ub:"ipv6-cidr-block-network-border-group"`
	Ipv6IpamPoolId                  *string `ub:"ipv6-ipam-pool-id"`
	Ipv6NetmaskLength               *int64  `ub:"ipv6-netmask-length"`
}

type VpcOutput struct {
	VpcId         string `ub:"vpc-id"`
	DhcpOptionsId string `ub:"dhcp-options-id"`
	OwnerId       string `ub:"owner-id"`
}

func (r *Vpc) SchemaVersion() int { return 1 }

func (r *Vpc) ReplaceFields() []string {
	return []string{
		"cidr-block",
		"instance-tenancy",
		"amazon-provided-ipv6-cidr-block",
		"ipv4-ipam-pool-id",
		"ipv4-netmask-length",
		"ipv6-cidr-block",
		"ipv6-cidr-block-network-border-group",
		"ipv6-ipam-pool-id",
		"ipv6-netmask-length",
	}
}

// Constraints declares the cross-field rules the EC2 CreateVpc API
// enforces on its inputs. The IPv4 range comes from a CIDR block or an
// IPAM pool, never both. The IPv6 range comes from one of three sources
// that cannot mix: an Amazon-provided block, an explicit block from an
// IPAM pool, or a netmask length from an IPAM pool. A network border
// group only applies to an Amazon-provided block, and tenancy is one of
// the two values CreateVpc accepts.
func (r Vpc) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.InstanceTenancy)).
			Require(constraint.OneOf(r.InstanceTenancy, "default", "dedicated")).
			Message("instance-tenancy must be default or dedicated"),
		constraint.AtMostOneOf(r.CidrBlock, r.Ipv4NetmaskLength),
		constraint.RequiredWith(r.Ipv4NetmaskLength, r.Ipv4IpamPoolId),
		constraint.AtMostOneOf(r.Ipv6CidrBlock, r.Ipv6NetmaskLength),
		constraint.RequiredWith(r.Ipv6CidrBlock, r.Ipv6IpamPoolId),
		constraint.RequiredWith(r.Ipv6NetmaskLength, r.Ipv6IpamPoolId),
		constraint.When(constraint.IsTrue(r.AmazonProvidedIpv6CidrBlock)).
			Require(constraint.Absent(r.Ipv6CidrBlock), constraint.Absent(r.Ipv6IpamPoolId)).
			Message("amazon-provided-ipv6-cidr-block cannot combine with an explicit ipv6 block or pool"),
		constraint.When(constraint.Present(r.Ipv6CidrBlockNetworkBorderGroup)).
			Require(constraint.IsTrue(r.AmazonProvidedIpv6CidrBlock)).
			Message("ipv6-cidr-block-network-border-group requires amazon-provided-ipv6-cidr-block"),
	}
}

func (r *Vpc) Create(ctx context.Context, cfg any) (*VpcOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &ec2.CreateVpcInput{
		CidrBlock:                       r.CidrBlock,
		InstanceTenancy:                 ec2types.Tenancy(aws.ToString(r.InstanceTenancy)),
		AmazonProvidedIpv6CidrBlock:     r.AmazonProvidedIpv6CidrBlock,
		Ipv4IpamPoolId:                  r.Ipv4IpamPoolId,
		Ipv4NetmaskLength:               ptr.Int32(r.Ipv4NetmaskLength),
		Ipv6CidrBlock:                   r.Ipv6CidrBlock,
		Ipv6CidrBlockNetworkBorderGroup: r.Ipv6CidrBlockNetworkBorderGroup,
		Ipv6IpamPoolId:                  r.Ipv6IpamPoolId,
		Ipv6NetmaskLength:               ptr.Int32(r.Ipv6NetmaskLength),
	}
	resp, err := client.CreateVpc(ctx, in)
	if err != nil {
		return nil, err
	}
	id := aws.ToString(resp.Vpc.VpcId)
	// A describe right after CreateVpc can answer InvalidVpcID.NotFound from a
	// lagging replica. The SDK's VpcAvailable waiter fails on the first
	// not-found rather than riding the window out, so poll through
	// internal/wait instead, treating not-found as not ready, until the VPC
	// reads as available.
	what := fmt.Sprintf("vpc %s to become available", id)
	err = wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		vpc, err := describeVpc(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		return vpc.State == ec2types.VpcStateAvailable, nil
	}, wait.WithTimeout(5*time.Minute), wait.WithInterval(time.Second))
	if err != nil {
		return nil, err
	}
	return &VpcOutput{
		VpcId:         id,
		DhcpOptionsId: aws.ToString(resp.Vpc.DhcpOptionsId),
		OwnerId:       aws.ToString(resp.Vpc.OwnerId),
	}, nil
}

func (r *Vpc) Read(ctx context.Context, cfg any, prior *VpcOutput) (*VpcOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	vpc, err := describeVpc(ctx, client, prior.VpcId)
	if err != nil {
		return nil, err
	}
	return &VpcOutput{
		VpcId:         aws.ToString(vpc.VpcId),
		DhcpOptionsId: aws.ToString(vpc.DhcpOptionsId),
		OwnerId:       aws.ToString(vpc.OwnerId),
	}, nil
}

func (r *Vpc) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Vpc, *VpcOutput],
) (*VpcOutput, error) {
	return prior.Outputs, nil
}

func (r *Vpc) Delete(ctx context.Context, cfg any, prior *VpcOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &ec2.DeleteVpcInput{
		VpcId: aws.String(prior.VpcId),
	}
	_, err = client.DeleteVpc(ctx, in)
	return err
}

// describeVpc fetches the VPC with the given id. EC2 reports a missing VPC by
// service code on an HTTP 400, never a 404, so the not-found code maps to
// runtime.ErrNotFound; an empty result or an id mismatch means the same.
func describeVpc(ctx context.Context, client *ec2.Client, id string) (*ec2types.Vpc, error) {
	resp, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, "InvalidVpcID.NotFound") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe vpcs: %w", err)
	}
	if len(resp.Vpcs) == 0 {
		return nil, runtime.ErrNotFound
	}
	vpc := resp.Vpcs[0]
	if aws.ToString(vpc.VpcId) != id {
		return nil, runtime.ErrNotFound
	}
	return &vpc, nil
}
