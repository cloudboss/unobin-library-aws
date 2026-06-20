package ec2

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// Subnet is an EC2 subnet: an IP range carved out of a VPC in one Availability
// Zone. The VPC, the zone, the IPv4 and IPv6 ranges, and an Outpost ARN are
// fixed when the subnet is created, so a change to any of them replaces the
// subnet; the launch-time options are reconciled in place. CreateSubnet accepts
// only the address and placement fields. The launch-time options each have no
// create-time setting and are applied after the subnet exists, one
// ModifySubnetAttribute call per option. A nil option is never sent: the value
// is AWS's to decide, the default for a new subnet or whatever an earlier
// apply set. EC2 has no reset call, so restoring a default after an apply set
// the option means setting the default explicitly.
type Subnet struct {
	VpcId              string            `ub:"vpc-id"`
	AvailabilityZone   *string           `ub:"availability-zone"`
	AvailabilityZoneId *string           `ub:"availability-zone-id"`
	CidrBlock          *string           `ub:"cidr-block"`
	Ipv4IpamPoolId     *string           `ub:"ipv4-ipam-pool-id"`
	Ipv4NetmaskLength  *int64            `ub:"ipv4-netmask-length"`
	Ipv6CidrBlock      *string           `ub:"ipv6-cidr-block"`
	Ipv6IpamPoolId     *string           `ub:"ipv6-ipam-pool-id"`
	Ipv6Native         *bool             `ub:"ipv6-native"`
	Ipv6NetmaskLength  *int64            `ub:"ipv6-netmask-length"`
	OutpostArn         *string           `ub:"outpost-arn"`
	Tags               map[string]string `ub:"tags"`
	// The remaining fields each back a ModifySubnetAttribute call after create.
	AssignIpv6AddressOnCreation *bool  `ub:"assign-ipv6-address-on-creation"`
	EnableDns64                 *bool  `ub:"enable-dns64"`
	EnableLniAtDeviceIndex      *int64 `ub:"enable-lni-at-device-index"`

	EnableResourceNameDnsAAAARecordOnLaunch *bool `ub:"enable-resource-name-dns-aaaa-record-on-launch"`
	EnableResourceNameDnsARecordOnLaunch    *bool `ub:"enable-resource-name-dns-a-record-on-launch"`

	MapPublicIpOnLaunch            *bool   `ub:"map-public-ip-on-launch"`
	PrivateDnsHostnameTypeOnLaunch *string `ub:"private-dns-hostname-type-on-launch"`
	CustomerOwnedIpv4Pool          *string `ub:"customer-owned-ipv4-pool"`
	MapCustomerOwnedIpOnLaunch     *bool   `ub:"map-customer-owned-ip-on-launch"`
}

// SubnetOutput holds the values EC2 computes for a subnet. The ARN and id are
// the subnet's handles. The owner id is the account that owns it. The zone, the
// IPv4 CIDR block, and the IPv6 block and its association id are filled by EC2
// when any of them is left for it to assign, so the settled values come from a
// describe, not the request.
type SubnetOutput struct {
	Arn                        string `ub:"arn"`
	Id                         string `ub:"id"`
	OwnerId                    string `ub:"owner-id"`
	AvailabilityZone           string `ub:"availability-zone"`
	AvailabilityZoneId         string `ub:"availability-zone-id"`
	CidrBlock                  string `ub:"cidr-block"`
	Ipv6CidrBlock              string `ub:"ipv6-cidr-block"`
	Ipv6CidrBlockAssociationId string `ub:"ipv6-cidr-block-association-id"`
}

func (r *Subnet) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when a subnet is created. The VPC,
// the zone, the IPv4 and IPv6 address sources, the IPv6-only flag, and an
// Outpost ARN cannot change on an existing subnet, so a change to any of them
// requires a new subnet. The IPv6 CIDR block is updatable in place when
// auto-assign was off, but a change while auto-assign was on forces a new
// subnet; unobin replaces unconditionally, so the block is listed here as a
// safe over-approximation. This diverges from Terraform, which updates the
// block in place when auto-assign was off rather than always replacing.
func (r *Subnet) ReplaceFields() []string {
	return []string{
		"vpc-id",
		"cidr-block",
		"availability-zone",
		"availability-zone-id",
		"outpost-arn",
		"ipv4-ipam-pool-id",
		"ipv4-netmask-length",
		"ipv6-ipam-pool-id",
		"ipv6-native",
		"ipv6-netmask-length",
		"ipv6-cidr-block",
	}
}

// Defaults marks the collection inputs a subnet may omit.
func (r Subnet) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the cross-field rules EC2 enforces on a subnet's inputs.
// A subnet's zone is named by name or by id, never both. The IPv4 range comes
// from an explicit CIDR block, an IPAM pool, or an Outpost's customer-owned
// pool, and a netmask length pairs with an IPAM pool; these sources cannot mix.
// The customer-owned pool and its map-on-launch toggle go together and only on
// an Outpost. The IPv6 range likewise comes from a block or a netmask length
// paired with an IPAM pool. The launch-time hostname type is one of two values,
// and a local-network-interface device index is a positive position.
func (r Subnet) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.AvailabilityZone, r.AvailabilityZoneId),
		constraint.ForbiddenWith(r.Ipv4NetmaskLength, r.CidrBlock, r.CustomerOwnedIpv4Pool),
		constraint.RequiredWith(r.Ipv4NetmaskLength, r.Ipv4IpamPoolId),
		constraint.AtMostOneOf(r.Ipv4IpamPoolId, r.CustomerOwnedIpv4Pool),
		constraint.RequiredWith(r.CustomerOwnedIpv4Pool, r.MapCustomerOwnedIpOnLaunch, r.OutpostArn),
		constraint.RequiredWith(r.MapCustomerOwnedIpOnLaunch, r.CustomerOwnedIpv4Pool, r.OutpostArn),
		constraint.ForbiddenWith(r.Ipv6NetmaskLength, r.Ipv6CidrBlock),
		constraint.RequiredWith(r.Ipv6NetmaskLength, r.Ipv6IpamPoolId),
		constraint.When(constraint.Present(r.PrivateDnsHostnameTypeOnLaunch)).
			Require(constraint.OneOf(r.PrivateDnsHostnameTypeOnLaunch, "ip-name", "resource-name")).
			Message("private-dns-hostname-type-on-launch must be ip-name or resource-name"),
		constraint.When(constraint.Present(r.EnableLniAtDeviceIndex)).
			Require(constraint.Above(r.EnableLniAtDeviceIndex, 0)).
			Message("enable-lni-at-device-index must be a positive device position"),
	}
}

func (r *Subnet) Create(ctx context.Context, cfg *awsCfg) (*SubnetOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &ec2.CreateSubnetInput{
		VpcId:              aws.String(r.VpcId),
		AvailabilityZone:   r.AvailabilityZone,
		AvailabilityZoneId: r.AvailabilityZoneId,
		CidrBlock:          r.CidrBlock,
		Ipv4IpamPoolId:     r.Ipv4IpamPoolId,
		Ipv4NetmaskLength:  ptr.Int32(r.Ipv4NetmaskLength),
		Ipv6CidrBlock:      r.Ipv6CidrBlock,
		Ipv6IpamPoolId:     r.Ipv6IpamPoolId,
		Ipv6Native:         r.Ipv6Native,
		Ipv6NetmaskLength:  ptr.Int32(r.Ipv6NetmaskLength),
		OutpostArn:         r.OutpostArn,
		TagSpecifications:  tagSpecifications(ec2types.ResourceTypeSubnet, r.Tags),
	}
	resp, err := client.CreateSubnet(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("create subnet: %w", err)
	}
	id := aws.ToString(resp.Subnet.SubnetId)
	// CreateSubnet returns before the subnet is usable. Wait for it to settle to
	// available and reconcile the launch-time options against that settled
	// description, not the create response, so each option is modified only when
	// it actually differs from what EC2 already set.
	subnet, err := r.waitAvailable(ctx, client, id)
	if err != nil {
		return nil, err
	}
	if err := r.reconcileOnCreate(ctx, client, id, subnet); err != nil {
		return nil, err
	}
	// The subnet's IPv6 block -- computed for an IPv6-only or IPAM-pool subnet,
	// or given explicitly to CreateSubnet -- can still be associating when the
	// subnet reaches available; wait for it to settle so the post-create read
	// returns the block and its association id.
	if r.Ipv6CidrBlock != nil || r.computedIpv6Block() {
		if err := r.waitIpv6Block(ctx, client, id); err != nil {
			return nil, err
		}
	}
	// Read settles the values EC2 fills in: the ARN, owner id, the assigned zone,
	// and a computed IPv6 block and its association id. It retries the brief
	// post-create window where a describe can still report the subnet not-found.
	return r.read(ctx, client, id, true)
}

func (r *Subnet) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *SubnetOutput) (*SubnetOutput,
	error,
) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Id, false)
}

// read fetches the subnet by id and returns its computed outputs. When created
// is true the subnet was just made, so a not-found means it has not propagated
// yet and read waits for it to appear; otherwise a not-found is drift and maps
// to runtime.ErrNotFound at once.
func (r *Subnet) read(
	ctx context.Context, client *ec2.Client, id string, created bool,
) (*SubnetOutput, error) {
	var subnet *ec2types.Subnet
	err := wait.Until(ctx, fmt.Sprintf("subnet %s", id),
		func(ctx context.Context) (bool, error) {
			s, err := describeSubnet(ctx, client, id)
			if err != nil {
				if err == runtime.ErrNotFound {
					if created {
						return false, nil
					}
					return false, runtime.ErrNotFound
				}
				return false, err
			}
			subnet = s
			return true, nil
		}, wait.WithTimeout(5*time.Minute))
	if err != nil {
		return nil, err
	}
	out := &SubnetOutput{
		Arn:                aws.ToString(subnet.SubnetArn),
		Id:                 aws.ToString(subnet.SubnetId),
		OwnerId:            aws.ToString(subnet.OwnerId),
		AvailabilityZone:   aws.ToString(subnet.AvailabilityZone),
		AvailabilityZoneId: aws.ToString(subnet.AvailabilityZoneId),
		CidrBlock:          aws.ToString(subnet.CidrBlock),
	}
	// A subnet has at most one associated IPv6 block; report the block and
	// its association id from the entry that is associated.
	for _, assoc := range subnet.Ipv6CidrBlockAssociationSet {
		if assoc.Ipv6CidrBlockState != nil &&
			assoc.Ipv6CidrBlockState.State == ec2types.SubnetCidrBlockStateCodeAssociated {
			out.Ipv6CidrBlock = aws.ToString(assoc.Ipv6CidrBlock)
			out.Ipv6CidrBlockAssociationId = aws.ToString(assoc.AssociationId)
			break
		}
	}
	return out, nil
}

func (r *Subnet) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Subnet, *SubnetOutput],
) (*SubnetOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.Id
	if err := r.reconcileOnUpdate(ctx, client, id, prior); err != nil {
		return nil, err
	}
	// ModifySubnetAttribute does not touch tags, so reconcile them as a set
	// whenever they changed, the same as the other EC2 resources.
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, id, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, id, false)
}

func (r *Subnet) Delete(ctx context.Context, cfg *awsCfg, prior *SubnetOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// A subnet cannot be deleted while a network interface, NAT gateway, or
	// other resource still sits in it; EC2 reports that as DependencyViolation,
	// which clears once those resources are gone. Retry the delete through it
	// over a generous window. The GuardDuty VPC-endpoint dissociation Terraform
	// attempts here is not ported (see the report); the retry covers the
	// ordinary case where a dependency is removed around the same time.
	err = retry.OnError(ctx, isDependencyViolation, func(ctx context.Context) error {
		_, err := client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
			SubnetId: aws.String(prior.Id),
		})
		return err
	}, retry.WithTimeout(20*time.Minute))
	if err != nil {
		if isNotFound(err, "InvalidSubnetID.NotFound") {
			return nil
		}
		return fmt.Errorf("delete subnet: %w", err)
	}
	return nil
}

// reconcileOnCreate applies the launch-time options after create, comparing
// each desired value against the settled subnet's current value so only the
// ones EC2 did not already match are written. The order around the IPv6 CIDR is
// deliberate: an auto-assign or dns64 disable runs before the block changes, and
// an enable runs after, so neither is set against a block that is not there yet.
func (r *Subnet) reconcileOnCreate(
	ctx context.Context, client *ec2.Client, id string, subnet *ec2types.Subnet,
) error {
	if r.AssignIpv6AddressOnCreation != nil &&
		!*r.AssignIpv6AddressOnCreation &&
		aws.ToBool(subnet.AssignIpv6AddressOnCreation) {
		if err := r.modifyAssignIpv6(ctx, client, id); err != nil {
			return err
		}
	}
	if r.EnableDns64 != nil && !*r.EnableDns64 && aws.ToBool(subnet.EnableDns64) {
		if err := r.modifyEnableDns64(ctx, client, id); err != nil {
			return err
		}
	}
	// EC2 assigns a computed IPv6 block for an IPv6-only or IPAM-pool subnet
	// without an explicit block; do not try to associate one in that case.
	if r.Ipv6CidrBlock != nil && !r.computedIpv6Block() {
		if !subnetHasIpv6Block(subnet, *r.Ipv6CidrBlock) {
			if err := r.associateIpv6Block(ctx, client, id, subnet); err != nil {
				return err
			}
		}
	}
	if r.EnableDns64 != nil && *r.EnableDns64 && !aws.ToBool(subnet.EnableDns64) {
		if err := r.modifyEnableDns64(ctx, client, id); err != nil {
			return err
		}
	}
	if r.AssignIpv6AddressOnCreation != nil &&
		*r.AssignIpv6AddressOnCreation &&
		!aws.ToBool(subnet.AssignIpv6AddressOnCreation) {
		if err := r.modifyAssignIpv6(ctx, client, id); err != nil {
			return err
		}
	}
	if r.EnableLniAtDeviceIndex != nil && *r.EnableLniAtDeviceIndex != 0 &&
		!int64PtrEqual(r.EnableLniAtDeviceIndex, subnet.EnableLniAtDeviceIndex) {
		if err := r.modifyEnableLni(ctx, client, id); err != nil {
			return err
		}
	}
	if r.EnableResourceNameDnsAAAARecordOnLaunch != nil &&
		!boolEqual(r.EnableResourceNameDnsAAAARecordOnLaunch,
			subnetDnsAAAA(subnet)) {
		if err := r.modifyDnsAAAA(ctx, client, id); err != nil {
			return err
		}
	}
	if r.EnableResourceNameDnsARecordOnLaunch != nil &&
		!boolEqual(r.EnableResourceNameDnsARecordOnLaunch, subnetDnsA(subnet)) {
		if err := r.modifyDnsA(ctx, client, id); err != nil {
			return err
		}
	}
	if r.MapPublicIpOnLaunch != nil &&
		!boolEqual(r.MapPublicIpOnLaunch, subnet.MapPublicIpOnLaunch) {
		if err := r.modifyMapPublicIp(ctx, client, id); err != nil {
			return err
		}
	}
	if r.PrivateDnsHostnameTypeOnLaunch != nil &&
		!stringEqual(r.PrivateDnsHostnameTypeOnLaunch, subnetHostnameType(subnet)) {
		if err := r.modifyHostnameType(ctx, client, id); err != nil {
			return err
		}
	}
	// The customer-owned pool and its map-on-launch toggle are the one pair EC2
	// accepts in a single ModifySubnetAttribute call; send them together when
	// either differs from the current subnet.
	if (r.CustomerOwnedIpv4Pool != nil &&
		!stringEqual(r.CustomerOwnedIpv4Pool, subnet.CustomerOwnedIpv4Pool)) ||
		(r.MapCustomerOwnedIpOnLaunch != nil &&
			!boolEqual(r.MapCustomerOwnedIpOnLaunch, subnet.MapCustomerOwnedIpOnLaunch)) {
		if err := r.modifyOutpostPair(ctx, client, id); err != nil {
			return err
		}
	}
	return nil
}

// reconcileOnUpdate applies the launch-time options that changed since the last
// apply, gating each call on a real change to its field and on the field being
// present, and following the same order around the IPv6 CIDR as create. A nil
// option is never sent -- the value is AWS's to decide -- so a removed option
// produces no call at all: not an explicit false for the bools, and not the
// attribute-less request the device-index and hostname-type fields would
// otherwise serialize.
func (r *Subnet) reconcileOnUpdate(
	ctx context.Context, client *ec2.Client, id string, prior runtime.Prior[Subnet, *SubnetOutput],
) error {
	if runtime.Changed(prior.Inputs.AssignIpv6AddressOnCreation, r.AssignIpv6AddressOnCreation) &&
		r.AssignIpv6AddressOnCreation != nil && !*r.AssignIpv6AddressOnCreation {
		if err := r.modifyAssignIpv6(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.Ipv6CidrBlock, r.Ipv6CidrBlock) {
		if err := r.reassociateIpv6Block(ctx, client, id, prior.Outputs); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.AssignIpv6AddressOnCreation, r.AssignIpv6AddressOnCreation) &&
		r.AssignIpv6AddressOnCreation != nil && *r.AssignIpv6AddressOnCreation {
		if err := r.modifyAssignIpv6(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.EnableDns64, r.EnableDns64) && r.EnableDns64 != nil {
		if err := r.modifyEnableDns64(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.EnableLniAtDeviceIndex, r.EnableLniAtDeviceIndex) &&
		r.EnableLniAtDeviceIndex != nil {
		if err := r.modifyEnableLni(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.EnableResourceNameDnsAAAARecordOnLaunch,
		r.EnableResourceNameDnsAAAARecordOnLaunch) &&
		r.EnableResourceNameDnsAAAARecordOnLaunch != nil {
		if err := r.modifyDnsAAAA(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.EnableResourceNameDnsARecordOnLaunch,
		r.EnableResourceNameDnsARecordOnLaunch) &&
		r.EnableResourceNameDnsARecordOnLaunch != nil {
		if err := r.modifyDnsA(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.MapPublicIpOnLaunch, r.MapPublicIpOnLaunch) &&
		r.MapPublicIpOnLaunch != nil {
		if err := r.modifyMapPublicIp(ctx, client, id); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.PrivateDnsHostnameTypeOnLaunch,
		r.PrivateDnsHostnameTypeOnLaunch) && r.PrivateDnsHostnameTypeOnLaunch != nil {
		if err := r.modifyHostnameType(ctx, client, id); err != nil {
			return err
		}
	}
	if (runtime.Changed(prior.Inputs.CustomerOwnedIpv4Pool, r.CustomerOwnedIpv4Pool) &&
		r.CustomerOwnedIpv4Pool != nil) ||
		(runtime.Changed(prior.Inputs.MapCustomerOwnedIpOnLaunch, r.MapCustomerOwnedIpOnLaunch) &&
			r.MapCustomerOwnedIpOnLaunch != nil) {
		if err := r.modifyOutpostPair(ctx, client, id); err != nil {
			return err
		}
	}
	return nil
}

// waitAvailable polls the subnet until it reports state available and returns
// the settled description. A subnet that enters a failed state stops the wait
// with an error, since it will not become available.
func (r *Subnet) waitAvailable(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.Subnet, error) {
	var subnet *ec2types.Subnet
	err := wait.Until(ctx, fmt.Sprintf("subnet %s to become available", id),
		func(ctx context.Context) (bool, error) {
			s, err := describeSubnet(ctx, client, id)
			if err != nil {
				if err == runtime.ErrNotFound {
					return false, nil
				}
				return false, err
			}
			switch s.State {
			case ec2types.SubnetStateAvailable:
				subnet = s
				return true, nil
			case ec2types.SubnetStateFailed,
				ec2types.SubnetStateFailedInsufficientCapacity:
				return false, fmt.Errorf("subnet %s entered state %s", id, s.State)
			default:
				return false, nil
			}
		}, wait.WithTimeout(10*time.Minute))
	if err != nil {
		return nil, err
	}
	return subnet, nil
}

// computedIpv6Block reports whether EC2 assigns the IPv6 block itself, which it
// does for an IPv6-only subnet or one drawing from an IPv6 IPAM pool when no
// explicit block is given. In that case Create must not associate a block.
func (r *Subnet) computedIpv6Block() bool {
	return aws.ToBool(r.Ipv6Native) || r.Ipv6IpamPoolId != nil
}

// associateIpv6Block associates the desired IPv6 block with the subnet and
// waits for the association to settle. It first removes any block EC2 already
// associated, since a subnet holds at most one.
func (r *Subnet) associateIpv6Block(
	ctx context.Context, client *ec2.Client, id string, subnet *ec2types.Subnet,
) error {
	for _, assoc := range subnet.Ipv6CidrBlockAssociationSet {
		if err := r.disassociateIpv6Block(ctx, client, id,
			aws.ToString(assoc.AssociationId)); err != nil {
			return err
		}
	}
	resp, err := client.AssociateSubnetCidrBlock(ctx, &ec2.AssociateSubnetCidrBlockInput{
		SubnetId:      aws.String(id),
		Ipv6CidrBlock: r.Ipv6CidrBlock,
	})
	if err != nil {
		return fmt.Errorf("associate subnet cidr block: %w", err)
	}
	assocID := aws.ToString(resp.Ipv6CidrBlockAssociation.AssociationId)
	return r.waitIpv6Associated(ctx, client, id, assocID)
}

// reassociateIpv6Block moves the subnet's IPv6 block to the desired value on
// update, removing the old association and adding the new one. A change that
// clears the block only removes the old one.
func (r *Subnet) reassociateIpv6Block(
	ctx context.Context, client *ec2.Client, id string, prior *SubnetOutput,
) error {
	if prior.Ipv6CidrBlockAssociationId != "" {
		if err := r.disassociateIpv6Block(ctx, client, id,
			prior.Ipv6CidrBlockAssociationId); err != nil {
			return err
		}
	}
	if r.Ipv6CidrBlock == nil {
		return nil
	}
	resp, err := client.AssociateSubnetCidrBlock(ctx, &ec2.AssociateSubnetCidrBlockInput{
		SubnetId:      aws.String(id),
		Ipv6CidrBlock: r.Ipv6CidrBlock,
	})
	if err != nil {
		return fmt.Errorf("associate subnet cidr block: %w", err)
	}
	assocID := aws.ToString(resp.Ipv6CidrBlockAssociation.AssociationId)
	return r.waitIpv6Associated(ctx, client, id, assocID)
}

// disassociateIpv6Block removes the IPv6 block association named by assocID and
// waits for it to clear from the subnet.
func (r *Subnet) disassociateIpv6Block(
	ctx context.Context, client *ec2.Client, id, assocID string,
) error {
	_, err := client.DisassociateSubnetCidrBlock(ctx, &ec2.DisassociateSubnetCidrBlockInput{
		AssociationId: aws.String(assocID),
	})
	if err != nil {
		return fmt.Errorf("disassociate subnet cidr block: %w", err)
	}
	what := fmt.Sprintf("subnet %s ipv6 block %s disassociation", id, assocID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		subnet, err := describeSubnet(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		return !subnetHasAssociation(subnet, assocID), nil
	}, wait.WithTimeout(3*time.Minute))
}

// waitIpv6Associated polls the subnet until the IPv6 block named by assocID
// reads as associated. A block that reaches a failed state stops the wait with
// the status message EC2 reports.
func (r *Subnet) waitIpv6Associated(
	ctx context.Context, client *ec2.Client, id, assocID string,
) error {
	what := fmt.Sprintf("subnet %s ipv6 block %s association", id, assocID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		subnet, err := describeSubnet(ctx, client, id)
		if err != nil {
			return false, err
		}
		for _, assoc := range subnet.Ipv6CidrBlockAssociationSet {
			if aws.ToString(assoc.AssociationId) != assocID {
				continue
			}
			if assoc.Ipv6CidrBlockState == nil {
				return false, nil
			}
			switch assoc.Ipv6CidrBlockState.State {
			case ec2types.SubnetCidrBlockStateCodeAssociated:
				return true, nil
			case ec2types.SubnetCidrBlockStateCodeFailed:
				return false, fmt.Errorf("ipv6 block association failed: %s",
					aws.ToString(assoc.Ipv6CidrBlockState.StatusMessage))
			default:
				return false, nil
			}
		}
		return false, nil
	}, wait.WithTimeout(3*time.Minute))
}

// waitIpv6Block waits for the subnet's IPv6 block to finish associating,
// whether EC2 computed it for an IPv6-only or IPAM-pool subnet or it was given
// explicitly at create. EC2 can report the subnet available while the block is
// still associating, so without this wait the post-create read can return an
// empty IPv6 block and association id.
func (r *Subnet) waitIpv6Block(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("subnet %s ipv6 block", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		subnet, err := describeSubnet(ctx, client, id)
		if err != nil {
			return false, err
		}
		for _, assoc := range subnet.Ipv6CidrBlockAssociationSet {
			if assoc.Ipv6CidrBlockState == nil {
				continue
			}
			switch assoc.Ipv6CidrBlockState.State {
			case ec2types.SubnetCidrBlockStateCodeAssociated:
				return true, nil
			case ec2types.SubnetCidrBlockStateCodeFailed:
				return false, fmt.Errorf("ipv6 block association failed: %s",
					aws.ToString(assoc.Ipv6CidrBlockState.StatusMessage))
			}
		}
		return false, nil
	}, wait.WithTimeout(3*time.Minute))
}

// modifyAssignIpv6 sets whether new network interfaces in the subnet receive an
// IPv6 address, then waits until a describe reflects the new value.
func (r *Subnet) modifyAssignIpv6(ctx context.Context, client *ec2.Client, id string) error {
	want := aws.ToBool(r.AssignIpv6AddressOnCreation)
	_, err := client.ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
		SubnetId:                    aws.String(id),
		AssignIpv6AddressOnCreation: &ec2types.AttributeBooleanValue{Value: aws.Bool(want)},
	})
	if err != nil {
		return fmt.Errorf("modify assign ipv6 on creation: %w", err)
	}
	return r.waitAttribute(ctx, client, id, "assign-ipv6-address-on-creation",
		func(s *ec2types.Subnet) bool {
			return aws.ToBool(s.AssignIpv6AddressOnCreation) == want
		})
}

// modifyEnableDns64 sets whether the subnet's resolver returns synthetic IPv6
// addresses for IPv4-only destinations, then waits until it is observed.
func (r *Subnet) modifyEnableDns64(ctx context.Context, client *ec2.Client, id string) error {
	want := aws.ToBool(r.EnableDns64)
	_, err := client.ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
		SubnetId:    aws.String(id),
		EnableDns64: &ec2types.AttributeBooleanValue{Value: aws.Bool(want)},
	})
	if err != nil {
		return fmt.Errorf("modify enable dns64: %w", err)
	}
	return r.waitAttribute(ctx, client, id, "enable-dns64",
		func(s *ec2types.Subnet) bool { return aws.ToBool(s.EnableDns64) == want })
}

// modifyEnableLni sets the device index at which local network interfaces are
// enabled, then waits until it is observed.
func (r *Subnet) modifyEnableLni(ctx context.Context, client *ec2.Client, id string) error {
	want := ptr.Int32(r.EnableLniAtDeviceIndex)
	_, err := client.ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
		SubnetId:               aws.String(id),
		EnableLniAtDeviceIndex: want,
	})
	if err != nil {
		return fmt.Errorf("modify enable lni at device index: %w", err)
	}
	return r.waitAttribute(ctx, client, id, "enable-lni-at-device-index",
		func(s *ec2types.Subnet) bool {
			return aws.ToInt32(s.EnableLniAtDeviceIndex) == aws.ToInt32(want)
		})
}

// modifyDnsAAAA sets whether instance-hostname DNS queries return AAAA records,
// then waits until it is observed.
func (r *Subnet) modifyDnsAAAA(ctx context.Context, client *ec2.Client, id string) error {
	want := aws.ToBool(r.EnableResourceNameDnsAAAARecordOnLaunch)
	_, err := client.ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
		SubnetId: aws.String(id),
		EnableResourceNameDnsAAAARecordOnLaunch: &ec2types.AttributeBooleanValue{
			Value: aws.Bool(want),
		},
	})
	if err != nil {
		return fmt.Errorf("modify resource name dns aaaa record on launch: %w", err)
	}
	return r.waitAttribute(ctx, client, id, "enable-resource-name-dns-aaaa-record-on-launch",
		func(s *ec2types.Subnet) bool { return boolEqual(&want, subnetDnsAAAA(s)) })
}

// modifyDnsA sets whether instance-hostname DNS queries return A records, then
// waits until it is observed.
func (r *Subnet) modifyDnsA(ctx context.Context, client *ec2.Client, id string) error {
	want := aws.ToBool(r.EnableResourceNameDnsARecordOnLaunch)
	_, err := client.ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
		SubnetId: aws.String(id),
		EnableResourceNameDnsARecordOnLaunch: &ec2types.AttributeBooleanValue{
			Value: aws.Bool(want),
		},
	})
	if err != nil {
		return fmt.Errorf("modify resource name dns a record on launch: %w", err)
	}
	return r.waitAttribute(ctx, client, id, "enable-resource-name-dns-a-record-on-launch",
		func(s *ec2types.Subnet) bool { return boolEqual(&want, subnetDnsA(s)) })
}

// modifyMapPublicIp sets whether instances launched in the subnet receive a
// public IPv4 address, then waits until it is observed.
func (r *Subnet) modifyMapPublicIp(ctx context.Context, client *ec2.Client, id string) error {
	want := aws.ToBool(r.MapPublicIpOnLaunch)
	_, err := client.ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
		SubnetId:            aws.String(id),
		MapPublicIpOnLaunch: &ec2types.AttributeBooleanValue{Value: aws.Bool(want)},
	})
	if err != nil {
		return fmt.Errorf("modify map public ip on launch: %w", err)
	}
	return r.waitAttribute(ctx, client, id, "map-public-ip-on-launch",
		func(s *ec2types.Subnet) bool { return aws.ToBool(s.MapPublicIpOnLaunch) == want })
}

// modifyHostnameType sets the launch-time hostname type, then waits until it is
// observed.
func (r *Subnet) modifyHostnameType(ctx context.Context, client *ec2.Client, id string) error {
	want := aws.ToString(r.PrivateDnsHostnameTypeOnLaunch)
	_, err := client.ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
		SubnetId:                       aws.String(id),
		PrivateDnsHostnameTypeOnLaunch: ec2types.HostnameType(want),
	})
	if err != nil {
		return fmt.Errorf("modify private dns hostname type on launch: %w", err)
	}
	return r.waitAttribute(ctx, client, id, "private-dns-hostname-type-on-launch",
		func(s *ec2types.Subnet) bool { return stringEqual(&want, subnetHostnameType(s)) })
}

// modifyOutpostPair sets the customer-owned IPv4 pool and its map-on-launch
// toggle together, the one attribute pair EC2 accepts in a single call, then
// waits until both are observed.
func (r *Subnet) modifyOutpostPair(ctx context.Context, client *ec2.Client, id string) error {
	wantPool := aws.ToString(r.CustomerOwnedIpv4Pool)
	wantMap := aws.ToBool(r.MapCustomerOwnedIpOnLaunch)
	in := &ec2.ModifySubnetAttributeInput{
		SubnetId: aws.String(id),
		MapCustomerOwnedIpOnLaunch: &ec2types.AttributeBooleanValue{
			Value: aws.Bool(wantMap),
		},
	}
	if r.CustomerOwnedIpv4Pool != nil {
		in.CustomerOwnedIpv4Pool = r.CustomerOwnedIpv4Pool
	}
	_, err := client.ModifySubnetAttribute(ctx, in)
	if err != nil {
		return fmt.Errorf("modify customer-owned ip on launch: %w", err)
	}
	return r.waitAttribute(ctx, client, id, "customer-owned-ip-on-launch",
		func(s *ec2types.Subnet) bool {
			return stringEqual(&wantPool, s.CustomerOwnedIpv4Pool) &&
				aws.ToBool(s.MapCustomerOwnedIpOnLaunch) == wantMap
		})
}

// waitAttribute polls the subnet until matched reports the modified attribute
// has taken effect, so a later read does not race the modify's propagation.
func (r *Subnet) waitAttribute(
	ctx context.Context, client *ec2.Client, id, attr string,
	matched func(*ec2types.Subnet) bool,
) error {
	what := fmt.Sprintf("subnet %s attribute %s", id, attr)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		subnet, err := describeSubnet(ctx, client, id)
		if err != nil {
			return false, err
		}
		return matched(subnet), nil
	}, wait.WithTimeout(5*time.Minute), wait.WithInterval(10*time.Second))
}

// describeSubnet fetches the subnet with the given id. EC2 reports a missing
// subnet by service code on an HTTP 400, never a 404, so the not-found code
// maps to runtime.ErrNotFound; an empty result or an id mismatch means the same.
func describeSubnet(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.Subnet, error) {
	resp, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		SubnetIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, "InvalidSubnetID.NotFound") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe subnets: %w", err)
	}
	if len(resp.Subnets) == 0 {
		return nil, runtime.ErrNotFound
	}
	subnet := resp.Subnets[0]
	if aws.ToString(subnet.SubnetId) != id {
		return nil, runtime.ErrNotFound
	}
	return &subnet, nil
}

// subnetHasIpv6Block reports whether the subnet already has the given IPv6 block
// associated, so create does not re-associate a block EC2 set up.
func subnetHasIpv6Block(subnet *ec2types.Subnet, block string) bool {
	for _, assoc := range subnet.Ipv6CidrBlockAssociationSet {
		if aws.ToString(assoc.Ipv6CidrBlock) == block {
			return true
		}
	}
	return false
}

// subnetHasAssociation reports whether the subnet still lists the IPv6 block
// association named by assocID, used to wait out a disassociation.
func subnetHasAssociation(subnet *ec2types.Subnet, assocID string) bool {
	for _, assoc := range subnet.Ipv6CidrBlockAssociationSet {
		if aws.ToString(assoc.AssociationId) == assocID {
			return true
		}
	}
	return false
}

// subnetDnsAAAA returns the subnet's current AAAA-record-on-launch setting from
// its launch options, or nil when the options are unset.
func subnetDnsAAAA(subnet *ec2types.Subnet) *bool {
	if subnet.PrivateDnsNameOptionsOnLaunch == nil {
		return nil
	}
	return subnet.PrivateDnsNameOptionsOnLaunch.EnableResourceNameDnsAAAARecord
}

// subnetDnsA returns the subnet's current A-record-on-launch setting from its
// launch options, or nil when the options are unset.
func subnetDnsA(subnet *ec2types.Subnet) *bool {
	if subnet.PrivateDnsNameOptionsOnLaunch == nil {
		return nil
	}
	return subnet.PrivateDnsNameOptionsOnLaunch.EnableResourceNameDnsARecord
}

// subnetHostnameType returns the subnet's current launch-time hostname type as
// a string pointer, or nil when the options are unset.
func subnetHostnameType(subnet *ec2types.Subnet) *string {
	if subnet.PrivateDnsNameOptionsOnLaunch == nil {
		return nil
	}
	s := string(subnet.PrivateDnsNameOptionsOnLaunch.HostnameType)
	return &s
}

// boolEqual reports whether a desired bool pointer matches an observed one,
// treating a nil observed value as false so an unset cloud value compares equal
// to a desired false.
func boolEqual(want, got *bool) bool {
	return aws.ToBool(want) == aws.ToBool(got)
}

// stringEqual reports whether a desired string pointer matches an observed one,
// treating nil as the empty string.
func stringEqual(want, got *string) bool {
	return aws.ToString(want) == aws.ToString(got)
}

// int64PtrEqual reports whether a desired *int64 matches an observed *int32,
// treating nil on either side as zero.
func int64PtrEqual(want *int64, got *int32) bool {
	return aws.ToInt32(ptr.Int32(want)) == aws.ToInt32(got)
}

// isDependencyViolation reports whether a DeleteSubnet error is a dependency
// conflict that clears once the resources in the subnet are gone. EC2 raises it
// by service code on an HTTP 400, so it is matched the same way as a not-found.
func isDependencyViolation(err error) bool {
	return isNotFound(err, "DependencyViolation")
}
