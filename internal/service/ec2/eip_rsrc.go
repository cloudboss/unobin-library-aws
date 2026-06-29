package ec2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// Eip is an Elastic IP allocation: a public IPv4 address that EC2 reserves for
// an account through AllocateAddress and releases through ReleaseAddress. This
// construct covers allocation only; associating the address with an instance or
// network interface is a separate concern. Every allocation input is fixed when
// the address is allocated -- the address to recover, the network, an IPAM
// pool, a network border group, and the public or customer-owned pool to draw
// from -- so a change to any of them allocates a new address. Tags are the one
// input reconciled in place. The network border group is also threaded into the
// release call, since EC2 needs it to release an address scoped to a location.
type Eip struct {
	Address               *string            `ub:"address"`
	Domain                *string            `ub:"domain"`
	IpamPoolId            *string            `ub:"ipam-pool-id"`
	NetworkBorderGroup    *string            `ub:"network-border-group"`
	PublicIpv4Pool        *string            `ub:"public-ipv4-pool"`
	CustomerOwnedIpv4Pool *string            `ub:"customer-owned-ipv4-pool"`
	Tags                  *map[string]string `ub:"tags"`
}

// EipOutput holds the values EC2 computes for an allocation. The allocation id
// is the address's handle, the value a NAT gateway or other consumer
// references. The public IP is the actual IPv4 address. The association id and
// private IP are filled only when something associates the address out of band
// (a NAT gateway, another actor); the association id is what Delete needs to
// disassociate before it releases.
type EipOutput struct {
	AllocationId  string `ub:"allocation-id"`
	PublicIp      string `ub:"public-ip"`
	AssociationId string `ub:"association-id"`
	PrivateIp     string `ub:"private-ip"`
}

func (r *Eip) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when the address is allocated. None
// of the allocation arguments can change on an existing address: the address to
// recover, the network, the IPAM pool, the network border group, and the public
// or customer-owned pool are all settled at allocation time, so a change to any
// of them requires a new allocation. Only tags reconcile in place, so they are
// not listed here.
func (r *Eip) ReplaceFields() []string {
	return []string{
		"address",
		"domain",
		"ipam-pool-id",
		"network-border-group",
		"public-ipv4-pool",
		"customer-owned-ipv4-pool",
	}
}

// Constraints declares the one rule on an allocation's inputs: the network is
// one of the two values AllocateAddress accepts. AWS rejects standard
// post-Classic, but the enum still admits it, so the value is validated against
// the enum and only when domain is given.
func (r Eip) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.Domain)).
			Require(constraint.OneOf(r.Domain, "vpc", "standard")).
			Message("domain must be vpc or standard"),
	}
}

func (r *Eip) Create(ctx context.Context, cfg *awsCfg) (*EipOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &ec2.AllocateAddressInput{
		Address:               r.Address,
		Domain:                ec2types.DomainType(aws.ToString(r.Domain)),
		IpamPoolId:            r.IpamPoolId,
		NetworkBorderGroup:    r.NetworkBorderGroup,
		PublicIpv4Pool:        r.PublicIpv4Pool,
		CustomerOwnedIpv4Pool: r.CustomerOwnedIpv4Pool,
		TagSpecifications:     tagSpecifications(ec2types.ResourceTypeElasticIp, ptr.Value(r.Tags)),
	}
	resp, err := client.AllocateAddress(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("allocate address: %w", err)
	}
	id := aws.ToString(resp.AllocationId)
	// DescribeAddresses is eventually consistent right after AllocateAddress: the
	// just-allocated address can read as not-found, or a stale replica can answer
	// with a different allocation id, for a short window. Wait until the address
	// is visible under its own allocation id before reading the settled values.
	// The allocate response has the allocation id and public IP but not the
	// association id or private IP, so those come from the read.
	if err := r.waitExists(ctx, client, id); err != nil {
		return nil, err
	}
	return r.read(ctx, client, id)
}

func (r *Eip) Read(ctx context.Context, cfg *awsCfg, prior *EipOutput) (*EipOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.AllocationId)
}

// read fetches the address by allocation id and returns its computed outputs. A
// genuinely missing address maps to runtime.ErrNotFound, which drives the
// recreate fork.
func (r *Eip) read(ctx context.Context, client *ec2.Client, id string) (*EipOutput, error) {
	addr, err := r.describe(ctx, client, id)
	if err != nil {
		return nil, err
	}
	return &EipOutput{
		AllocationId:  aws.ToString(addr.AllocationId),
		PublicIp:      aws.ToString(addr.PublicIp),
		AssociationId: aws.ToString(addr.AssociationId),
		PrivateIp:     aws.ToString(addr.PrivateIpAddress),
	}, nil
}

func (r *Eip) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Eip, *EipOutput],
) (*EipOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Every allocation argument is replace-only, so tags are the only input an
	// update reconciles; reconcile them as a set whenever they changed.
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := syncTags(ctx, client, prior.Outputs.AllocationId, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, prior.Outputs.AllocationId)
}

func (r *Eip) Delete(ctx context.Context, cfg *awsCfg, prior *EipOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.AllocationId
	// Recover the current association at delete time rather than trust the prior
	// outputs: a NAT gateway or other actor can associate the address after the
	// last apply, so the association id on the prior outputs goes stale. A
	// not-found here means the address is already released, which is a successful
	// delete with nothing to do.
	addr, err := r.describe(ctx, client, id)
	if err != nil {
		if err == runtime.ErrNotFound {
			return nil
		}
		return err
	}
	// An out-of-band association must be cleared before the address can be
	// released. The association can disappear between the describe and the
	// disassociate call, so a not-found association id is treated as already
	// cleared.
	if assoc := aws.ToString(addr.AssociationId); assoc != "" {
		_, err := client.DisassociateAddress(ctx, &ec2.DisassociateAddressInput{
			AssociationId: aws.String(assoc),
		})
		if err != nil && !isNotFound(err, "InvalidAssociationID.NotFound") {
			return fmt.Errorf("disassociate address: %w", err)
		}
	}
	// ReleaseAddress needs the network border group when the address is scoped to
	// a location, so thread the input value into the release call. An address that
	// is already gone is a successful delete.
	_, err = client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
		AllocationId:       aws.String(id),
		NetworkBorderGroup: r.NetworkBorderGroup,
	})
	if err != nil && !isNotFound(err, "InvalidAllocationID.NotFound") {
		return fmt.Errorf("release address: %w", err)
	}
	// An address drawn from an IPAM pool releases the pool allocation
	// asynchronously, so the pool can still report the allocation for this address
	// after ReleaseAddress returns. Wait for the allocation to disappear before
	// reporting the delete done, so an immediate reallocation from the same pool
	// does not race a stale allocation.
	if r.IpamPoolId != nil {
		if err := r.waitIpamReleased(ctx, client, *r.IpamPoolId, id); err != nil {
			return err
		}
	}
	return nil
}

// waitExists polls DescribeAddresses until the address is visible under its own
// allocation id. The post-allocate window where the address reads as not-found,
// or where a stale replica answers with a different allocation id, counts as not
// ready rather than an error, so the wait rides the window out.
func (r *Eip) waitExists(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("elastic ip %s to become visible", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := r.describe(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}, wait.WithTimeout(5*time.Minute), wait.WithInterval(time.Second))
}

// waitIpamReleased polls the IPAM pool until it no longer lists an allocation
// for the released address. The pool's own not-found codes -- for a missing
// allocation or a missing pool -- are the success signal, so they end the wait
// rather than stop it.
func (r *Eip) waitIpamReleased(
	ctx context.Context, client *ec2.Client, poolID, allocationID string,
) error {
	what := fmt.Sprintf("ipam pool %s to release allocation for %s", poolID, allocationID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		found, err := ipamPoolHasAllocation(ctx, client, poolID, allocationID)
		if err != nil {
			if isNotFound(err, "InvalidIpamPoolAllocationId.NotFound",
				"InvalidIpamPoolId.NotFound") {
				return true, nil
			}
			return false, err
		}
		return !found, nil
	}, wait.WithTimeout(10*time.Minute), wait.WithInterval(5*time.Second))
}

// describe fetches the address with the given allocation id. EC2 reports a
// missing address by service code on an HTTP 400, never a 404, so the not-found
// codes map to runtime.ErrNotFound. A released or foreign address answers with
// AuthFailure rather than a not-found code, which is treated as not-found only
// when the message says the address does not belong to the account. An empty
// result, or a stale replica answering with a different allocation id, means the
// same as not-found.
func (r *Eip) describe(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.Address, error) {
	resp, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		AllocationIds: []string{id},
	})
	if err != nil {
		if isEipNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe addresses: %w", err)
	}
	if len(resp.Addresses) == 0 {
		return nil, runtime.ErrNotFound
	}
	addr := resp.Addresses[0]
	if aws.ToString(addr.AllocationId) != id {
		return nil, runtime.ErrNotFound
	}
	return &addr, nil
}

// ipamPoolHasAllocation reports whether the IPAM pool still lists an allocation
// for the given resource id, paging through the allocations. It propagates the
// pool's not-found codes so the delete wait can read them as the released
// signal.
func ipamPoolHasAllocation(
	ctx context.Context, client *ec2.Client, poolID, resourceID string,
) (bool, error) {
	in := &ec2.GetIpamPoolAllocationsInput{
		IpamPoolId: aws.String(poolID),
		Filters: []ec2types.Filter{{
			Name:   aws.String("resource-id"),
			Values: []string{resourceID},
		}},
	}
	for {
		resp, err := client.GetIpamPoolAllocations(ctx, in)
		if err != nil {
			return false, fmt.Errorf("get ipam pool allocations: %w", err)
		}
		for _, alloc := range resp.IpamPoolAllocations {
			if aws.ToString(alloc.ResourceId) == resourceID {
				return true, nil
			}
		}
		if resp.NextToken == nil {
			return false, nil
		}
		in.NextToken = resp.NextToken
	}
}

// isEipNotFound reports whether err means the Elastic IP is gone. EC2 answers
// with InvalidAllocationID.NotFound or InvalidAddress.NotFound by service code
// for an address it cannot find, and with AuthFailure for a released or foreign
// address; the AuthFailure case counts only when the message says the address
// does not belong to the account, so an unrelated authorization failure still
// propagates.
func isEipNotFound(err error) bool {
	if isNotFound(err, "InvalidAllocationID.NotFound", "InvalidAddress.NotFound") {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "AuthFailure" {
		return strings.Contains(apiErr.ErrorMessage(), "does not belong to you")
	}
	return false
}
