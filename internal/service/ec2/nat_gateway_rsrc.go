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
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// NatGateway is a zonal (single-AZ) NAT gateway: a managed device in one subnet
// that lets instances reach the internet (public connectivity) or other VPCs
// (private connectivity) without exposing them to inbound traffic. The subnet,
// the connectivity type, the Elastic IP allocation, and the primary private
// address are fixed when the gateway is created, so a change to any of them
// replaces the gateway. The secondary Elastic IP allocations (public) and
// secondary private addresses (private) are reconciled in place after create,
// each by its own EC2 call with a per-address settle wait; the secondary private
// address count is a create-time alternative to listing the addresses and is
// replaced rather than reconciled. A nil optional field is never sent: EC2
// applies its own default and fills the computed outputs.
//
// Only the zonal NAT gateway is modeled here. The regional (multi-AZ) variant --
// the availability-mode, vpc-id, and per-Availability-Zone address fields -- is a
// separable addition; omitting availability-mode defaults the gateway to zonal.
type NatGateway struct {
	SubnetId         string  `ub:"subnet-id"`
	ConnectivityType *string `ub:"connectivity-type"`
	AllocationId     *string `ub:"allocation-id"`
	PrivateIp        *string `ub:"private-ip"`
	// SecondaryAllocationIds adds further Elastic IP allocations to a public
	// gateway. It is reconciled in place on Update by AssociateNatGatewayAddress
	// and DisassociateNatGatewayAddress.
	SecondaryAllocationIds []string `ub:"secondary-allocation-ids"`
	// SecondaryPrivateIpAddresses adds further private addresses. On a private
	// gateway it is reconciled in place by AssignPrivateNatGatewayAddress and
	// UnassignPrivateNatGatewayAddress; on a public gateway the added entries
	// accompany an Associate as the paired private IPs, so a change to this
	// list alone, with no newly added secondary-allocation-ids, sends nothing.
	SecondaryPrivateIpAddresses []string `ub:"secondary-private-ip-addresses"`
	// SecondaryPrivateIpAddressCount asks EC2 to assign that many private
	// addresses to a private gateway at create time, instead of listing them. It
	// is a create-only alternative to the addresses list; changing it replaces the
	// gateway rather than reconciling in place.
	SecondaryPrivateIpAddressCount *int64            `ub:"secondary-private-ip-address-count"`
	Tags                           map[string]string `ub:"tags"`
}

// NatGatewayOutput holds the values EC2 computes for the gateway's primary
// address. The id is the gateway's handle. The network interface, the public IP
// (public gateways only), and the private IP come from the primary
// NatGatewayAddress of the settled gateway after the create wait; the private IP
// is computed when the input left it for EC2 to assign, so it is reported here
// even though it shares the input name.
type NatGatewayOutput struct {
	NatGatewayId       string `ub:"nat-gateway-id"`
	NetworkInterfaceId string `ub:"network-interface-id"`
	PublicIp           string `ub:"public-ip"`
	PrivateIp          string `ub:"private-ip"`
}

func (r *NatGateway) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when a NAT gateway is created. The
// connectivity type, the Elastic IP allocation, the primary private address, and
// the subnet cannot change on an existing gateway, so a change to any of them
// requires a new gateway. The secondary private address count is also listed:
// it only rides create, and the count is legal solely for a private gateway,
// where changing it after the fact replaces the gateway rather than reconciling
// addresses in place.
func (r *NatGateway) ReplaceFields() []string {
	return []string{
		"allocation-id",
		"connectivity-type",
		"private-ip",
		"subnet-id",
		"secondary-private-ip-address-count",
	}
}

// Defaults marks the collection inputs a NAT gateway may omit.
func (r NatGateway) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.SecondaryAllocationIds),
		defaults.Optional(r.SecondaryPrivateIpAddresses),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the cross-field rules EC2 enforces on a zonal NAT
// gateway's inputs. The connectivity type is public or private and defaults to
// public when omitted. An Elastic IP allocation is required for a public gateway
// (including the default) and forbidden for a private one. The secondary
// allocation list applies only to a public gateway. The secondary private
// address count applies only to a private gateway, and it cannot combine with an
// explicit list of secondary private addresses.
func (r NatGateway) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.ConnectivityType)).
			Require(constraint.OneOf(r.ConnectivityType, "public", "private")).
			Message("connectivity-type must be public or private"),
		constraint.When(constraint.Any(
			constraint.Equals(r.ConnectivityType, "public"),
			constraint.Absent(r.ConnectivityType))).
			Require(constraint.Present(r.AllocationId)).
			Message("allocation-id is required for a public NAT gateway"),
		constraint.When(constraint.Equals(r.ConnectivityType, "private")).
			Require(constraint.Absent(r.AllocationId)).
			Message("allocation-id is not supported with connectivity-type private"),
		constraint.When(constraint.Equals(r.ConnectivityType, "private")).
			Require(constraint.Absent(r.SecondaryAllocationIds)).
			Message("secondary-allocation-ids is not supported with connectivity-type private"),
		constraint.When(constraint.Present(r.SecondaryPrivateIpAddressCount)).
			Require(constraint.Equals(r.ConnectivityType, "private")).
			Message("secondary-private-ip-address-count is supported only with connectivity-type private"),
		constraint.AtMostOneOf(r.SecondaryPrivateIpAddressCount, r.SecondaryPrivateIpAddresses),
	}
}

func (r *NatGateway) Create(ctx context.Context, cfg *awsCfg) (*NatGatewayOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &ec2.CreateNatGatewayInput{
		SubnetId:                       aws.String(r.SubnetId),
		ConnectivityType:               ec2types.ConnectivityType(aws.ToString(r.ConnectivityType)),
		AllocationId:                   r.AllocationId,
		PrivateIpAddress:               r.PrivateIp,
		SecondaryAllocationIds:         r.SecondaryAllocationIds,
		SecondaryPrivateIpAddresses:    r.SecondaryPrivateIpAddresses,
		SecondaryPrivateIpAddressCount: ptr.Int32(r.SecondaryPrivateIpAddressCount),
		TagSpecifications:              tagSpecifications(ec2types.ResourceTypeNatgateway, r.Tags),
	}
	// The SDK fills the idempotency token when ClientToken is unset, so a retried
	// create does not double-provision; it is left for the SDK to supply.
	resp, err := client.CreateNatGateway(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("create nat gateway: %w", err)
	}
	id := aws.ToString(resp.NatGateway.NatGatewayId)
	// CreateNatGateway returns while the gateway is still pending and its addresses
	// are not yet succeeded; wait for it to settle to available, then read so the
	// network interface and the public and private addresses come from the settled
	// record rather than the unsettled create response.
	if err := r.waitCreated(ctx, client, id); err != nil {
		return nil, err
	}
	return r.read(ctx, client, id)
}

func (r *NatGateway) Read(
	ctx context.Context, cfg *awsCfg, prior *NatGatewayOutput,
) (*NatGatewayOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.NatGatewayId)
}

func (r *NatGateway) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[NatGateway, *NatGatewayOutput],
) (*NatGatewayOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.NatGatewayId
	// Only the secondary-address sets are reconciled in place, and which call does
	// it depends on the connectivity type. A public gateway adds and removes
	// Elastic IP allocations; a private gateway adds and removes private addresses.
	// Each set is reconciled only when its input actually changed.
	if aws.ToString(r.ConnectivityType) == "private" {
		if runtime.Changed(prior.Inputs.SecondaryPrivateIpAddresses,
			r.SecondaryPrivateIpAddresses) {
			if err := r.reconcilePrivateAddresses(ctx, client, id,
				prior.Inputs.SecondaryPrivateIpAddresses); err != nil {
				return nil, err
			}
		}
	} else {
		if runtime.Changed(prior.Inputs.SecondaryAllocationIds, r.SecondaryAllocationIds) ||
			runtime.Changed(prior.Inputs.SecondaryPrivateIpAddresses,
				r.SecondaryPrivateIpAddresses) {
			if err := r.reconcileSecondaryAllocations(ctx, client, id,
				prior.Inputs.SecondaryAllocationIds,
				prior.Inputs.SecondaryPrivateIpAddresses); err != nil {
				return nil, err
			}
		}
	}
	// The address calls do not touch tags, so reconcile them as a set whenever they
	// changed, the same as the other EC2 resources.
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, id, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, id)
}

func (r *NatGateway) Delete(ctx context.Context, cfg *awsCfg, prior *NatGatewayOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.NatGatewayId
	// Any security or proxy appliance attached to the gateway must detach before
	// the gateway can be deleted; wait for that first. A zonal gateway has none, so
	// the wait passes immediately.
	if err := r.waitAppliancesDetached(ctx, client, id); err != nil {
		return err
	}
	_, err = client.DeleteNatGateway(ctx, &ec2.DeleteNatGatewayInput{
		NatGatewayId: aws.String(id),
	})
	if err != nil {
		// A gateway that is already gone is a successful delete with nothing to do.
		if isNotFound(err, natGatewayNotFoundCode) {
			return nil
		}
		return fmt.Errorf("delete nat gateway: %w", err)
	}
	return r.waitDeleted(ctx, client, id)
}

// read fetches the gateway by id and returns the computed outputs from its
// primary address. The primary address is the one EC2 marks IsPrimary, falling
// back to the sole address when no entry is marked.
func (r *NatGateway) read(
	ctx context.Context, client *ec2.Client, id string,
) (*NatGatewayOutput, error) {
	gw, err := describeNatGateway(ctx, client, id)
	if err != nil {
		return nil, err
	}
	out := &NatGatewayOutput{NatGatewayId: aws.ToString(gw.NatGatewayId)}
	if addr := natGatewayPrimaryAddress(gw); addr != nil {
		out.NetworkInterfaceId = aws.ToString(addr.NetworkInterfaceId)
		out.PublicIp = aws.ToString(addr.PublicIp)
		out.PrivateIp = aws.ToString(addr.PrivateIp)
	}
	return out, nil
}

// reconcileSecondaryAllocations brings a public gateway's secondary Elastic IP
// allocations to the desired set. Added allocations are associated, along with
// any newly added private addresses as the paired private IPs, and each waited
// until succeeded. Removed allocations are disassociated by their association
// ids, which a fresh read maps from the allocation ids, and each waited until
// gone.
func (r *NatGateway) reconcileSecondaryAllocations(
	ctx context.Context, client *ec2.Client, id string, priorAllocations, priorPrivate []string,
) error {
	added := natGatewayStringsAdded(priorAllocations, r.SecondaryAllocationIds)
	removed := natGatewayStringsAdded(r.SecondaryAllocationIds, priorAllocations)
	if len(added) > 0 {
		in := &ec2.AssociateNatGatewayAddressInput{
			NatGatewayId:  aws.String(id),
			AllocationIds: added,
		}
		// Pass any newly added private addresses as the paired private IPs of the
		// association, the way EC2 accepts them for a public gateway.
		if priv := natGatewayStringsAdded(
			priorPrivate, r.SecondaryPrivateIpAddresses,
		); len(priv) > 0 {
			in.PrivateIpAddresses = priv
		}
		if _, err := client.AssociateNatGatewayAddress(ctx, in); err != nil {
			return fmt.Errorf("associate nat gateway address: %w", err)
		}
		for _, alloc := range added {
			if err := r.waitAddressAssociated(ctx, client, id, alloc); err != nil {
				return err
			}
		}
	}
	if len(removed) > 0 {
		assocIDs, err := natGatewayAssociationIds(ctx, client, id, removed)
		if err != nil {
			return err
		}
		if len(assocIDs) > 0 {
			_, err := client.DisassociateNatGatewayAddress(ctx,
				&ec2.DisassociateNatGatewayAddressInput{
					NatGatewayId:   aws.String(id),
					AssociationIds: assocIDs,
				})
			if err != nil {
				return fmt.Errorf("disassociate nat gateway address: %w", err)
			}
			for _, alloc := range removed {
				if err := r.waitAddressDisassociated(ctx, client, id, alloc); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// reconcilePrivateAddresses brings a private gateway's secondary private
// addresses to the desired set. Added addresses are assigned and each waited
// until succeeded; removed addresses are unassigned and each waited until gone.
func (r *NatGateway) reconcilePrivateAddresses(
	ctx context.Context, client *ec2.Client, id string, prior []string,
) error {
	added := natGatewayStringsAdded(prior, r.SecondaryPrivateIpAddresses)
	removed := natGatewayStringsAdded(r.SecondaryPrivateIpAddresses, prior)
	if len(added) > 0 {
		_, err := client.AssignPrivateNatGatewayAddress(ctx,
			&ec2.AssignPrivateNatGatewayAddressInput{
				NatGatewayId:       aws.String(id),
				PrivateIpAddresses: added,
			})
		if err != nil {
			return fmt.Errorf("assign private nat gateway address: %w", err)
		}
		for _, ip := range added {
			if err := r.waitAddressAssigned(ctx, client, id, ip); err != nil {
				return err
			}
		}
	}
	if len(removed) > 0 {
		_, err := client.UnassignPrivateNatGatewayAddress(ctx,
			&ec2.UnassignPrivateNatGatewayAddressInput{
				NatGatewayId:       aws.String(id),
				PrivateIpAddresses: removed,
			})
		if err != nil {
			return fmt.Errorf("unassign private nat gateway address: %w", err)
		}
		for _, ip := range removed {
			if err := r.waitAddressUnassigned(ctx, client, id, ip); err != nil {
				return err
			}
		}
	}
	return nil
}

// waitCreated polls the gateway until it reports state available. A gateway that
// enters the failed state stops the wait with the failure code and message EC2
// records. A describe that cannot yet find the just-created gateway is tolerated
// for a bounded number of consecutive polls, covering the post-create describe
// lag, before the wait gives up.
func (r *NatGateway) waitCreated(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("nat gateway %s to become available", id)
	notFound := 0
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		gw, err := describeNatGateway(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				notFound++
				if notFound > natGatewayCreateNotFoundLimit {
					return false, runtime.ErrNotFound
				}
				return false, nil
			}
			return false, err
		}
		notFound = 0
		switch gw.State {
		case ec2types.NatGatewayStateAvailable:
			return true, nil
		case ec2types.NatGatewayStateFailed:
			return false, fmt.Errorf("nat gateway %s entered state failed: %s: %s",
				id, aws.ToString(gw.FailureCode), aws.ToString(gw.FailureMessage))
		default:
			return false, nil
		}
	}, wait.WithTimeout(10*time.Minute))
}

// waitDeleted polls the gateway until a describe no longer finds it. A gateway
// that reads as deleted, or as not-found, has finished deleting. NAT gateway
// deletion is slow, so the wait runs over a long window at a ten-second pace.
func (r *NatGateway) waitDeleted(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("nat gateway %s deletion", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := describeNatGateway(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, wait.WithTimeout(30*time.Minute), wait.WithInterval(10*time.Second))
}

// waitAppliancesDetached polls the gateway until no attached appliance remains
// in a non-detached state, the precondition for deleting the gateway. A zonal
// gateway lists none, so the first poll passes. A gateway that is already gone
// has nothing attached, so a not-found completes the wait.
func (r *NatGateway) waitAppliancesDetached(
	ctx context.Context, client *ec2.Client, id string,
) error {
	what := fmt.Sprintf("nat gateway %s attached appliances to detach", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		gw, err := describeNatGateway(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		for _, app := range gw.AttachedAppliances {
			switch app.AttachmentState {
			case ec2types.NatGatewayApplianceStateAttaching,
				ec2types.NatGatewayApplianceStateAttached,
				ec2types.NatGatewayApplianceStateDetaching:
				return false, nil
			}
		}
		return true, nil
	}, wait.WithTimeout(30*time.Minute), wait.WithInterval(10*time.Second))
}

// waitAddressAssociated polls the gateway until the address for the given
// allocation id reports succeeded on several consecutive observations, so a
// single read against a caught-up replica does not end the wait early. An address
// that reaches failed stops the wait with the message EC2 records. A missing
// address is tolerated as still associating.
func (r *NatGateway) waitAddressAssociated(
	ctx context.Context, client *ec2.Client, id, allocationID string,
) error {
	what := fmt.Sprintf("nat gateway %s address %s association", id, allocationID)
	return wait.UntilStable(ctx, what, natGatewayAddressConfirmations,
		func(ctx context.Context) (bool, error) {
			addr, err := natGatewayAddressByAllocation(ctx, client, id, allocationID)
			if err != nil {
				if err == runtime.ErrNotFound {
					return false, nil
				}
				return false, err
			}
			return natGatewayAddressReady(addr,
				ec2types.NatGatewayAddressStatusSucceeded)
		}, wait.WithTimeout(10*time.Minute), wait.WithInterval(10*time.Second))
}

// waitAddressDisassociated polls the gateway until the address for the given
// allocation id is gone on several consecutive observations. The address stays in
// the succeeded or disassociating state while it transitions out, so neither is
// treated as done; the address being absent is.
func (r *NatGateway) waitAddressDisassociated(
	ctx context.Context, client *ec2.Client, id, allocationID string,
) error {
	what := fmt.Sprintf("nat gateway %s address %s disassociation", id, allocationID)
	return wait.UntilStable(ctx, what, natGatewayAddressConfirmations,
		func(ctx context.Context) (bool, error) {
			addr, err := natGatewayAddressByAllocation(ctx, client, id, allocationID)
			if err != nil {
				if err == runtime.ErrNotFound {
					return true, nil
				}
				return false, err
			}
			if addr.Status == ec2types.NatGatewayAddressStatusFailed {
				return false, fmt.Errorf("nat gateway %s address %s failed: %s",
					id, allocationID, aws.ToString(addr.FailureMessage))
			}
			return false, nil
		}, wait.WithTimeout(10*time.Minute), wait.WithInterval(10*time.Second))
}

// waitAddressAssigned polls the gateway until the private address reports
// succeeded. An address that reaches failed stops the wait with its message. A
// missing address is tolerated as still assigning.
func (r *NatGateway) waitAddressAssigned(
	ctx context.Context, client *ec2.Client, id, privateIP string,
) error {
	what := fmt.Sprintf("nat gateway %s address %s assignment", id, privateIP)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		addr, err := natGatewayAddressByPrivateIP(ctx, client, id, privateIP)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		return natGatewayAddressReady(addr, ec2types.NatGatewayAddressStatusSucceeded)
	}, wait.WithTimeout(10*time.Minute), wait.WithInterval(10*time.Second))
}

// waitAddressUnassigned polls the gateway until the private address is gone. The
// address stays in the succeeded or unassigning state while it transitions out,
// so neither is treated as done; the address being absent is.
func (r *NatGateway) waitAddressUnassigned(
	ctx context.Context, client *ec2.Client, id, privateIP string,
) error {
	what := fmt.Sprintf("nat gateway %s address %s unassignment", id, privateIP)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		addr, err := natGatewayAddressByPrivateIP(ctx, client, id, privateIP)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		if addr.Status == ec2types.NatGatewayAddressStatusFailed {
			return false, fmt.Errorf("nat gateway %s address %s failed: %s",
				id, privateIP, aws.ToString(addr.FailureMessage))
		}
		return false, nil
	}, wait.WithTimeout(10*time.Minute), wait.WithInterval(10*time.Second))
}

// natGatewayAssociationIds reads the gateway and maps each removed allocation id
// to its current association id, which DisassociateNatGatewayAddress takes in
// place of the allocation ids. This read is not best-effort: a describe error
// stops the Update.
func natGatewayAssociationIds(
	ctx context.Context, client *ec2.Client, id string, allocationIDs []string,
) ([]string, error) {
	gw, err := describeNatGateway(ctx, client, id)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(allocationIDs))
	for _, a := range allocationIDs {
		want[a] = true
	}
	var assocIDs []string
	for _, addr := range gw.NatGatewayAddresses {
		if want[aws.ToString(addr.AllocationId)] && addr.AssociationId != nil {
			assocIDs = append(assocIDs, aws.ToString(addr.AssociationId))
		}
	}
	return assocIDs, nil
}

// natGatewayAddressByAllocation returns the gateway's address for the given
// allocation id, mapping a missing address to runtime.ErrNotFound so a wait can
// treat it as still transitioning or already gone.
func natGatewayAddressByAllocation(
	ctx context.Context, client *ec2.Client, id, allocationID string,
) (*ec2types.NatGatewayAddress, error) {
	gw, err := describeNatGateway(ctx, client, id)
	if err != nil {
		return nil, err
	}
	for i := range gw.NatGatewayAddresses {
		if aws.ToString(gw.NatGatewayAddresses[i].AllocationId) == allocationID {
			return &gw.NatGatewayAddresses[i], nil
		}
	}
	return nil, runtime.ErrNotFound
}

// natGatewayAddressByPrivateIP returns the gateway's address for the given
// private IP, mapping a missing address to runtime.ErrNotFound.
func natGatewayAddressByPrivateIP(
	ctx context.Context, client *ec2.Client, id, privateIP string,
) (*ec2types.NatGatewayAddress, error) {
	gw, err := describeNatGateway(ctx, client, id)
	if err != nil {
		return nil, err
	}
	for i := range gw.NatGatewayAddresses {
		if aws.ToString(gw.NatGatewayAddresses[i].PrivateIp) == privateIP {
			return &gw.NatGatewayAddresses[i], nil
		}
	}
	return nil, runtime.ErrNotFound
}

// natGatewayAddressReady reports whether the address has reached the wanted
// status, returning a descriptive error when it has failed instead.
func natGatewayAddressReady(
	addr *ec2types.NatGatewayAddress, want ec2types.NatGatewayAddressStatus,
) (bool, error) {
	if addr.Status == ec2types.NatGatewayAddressStatusFailed {
		return false, fmt.Errorf("nat gateway address failed: %s",
			aws.ToString(addr.FailureMessage))
	}
	return addr.Status == want, nil
}

// natGatewayPrimaryAddress returns the gateway's primary address: the entry EC2
// marks IsPrimary, or the sole address when none is marked.
func natGatewayPrimaryAddress(gw *ec2types.NatGateway) *ec2types.NatGatewayAddress {
	for i := range gw.NatGatewayAddresses {
		if aws.ToBool(gw.NatGatewayAddresses[i].IsPrimary) {
			return &gw.NatGatewayAddresses[i]
		}
	}
	if len(gw.NatGatewayAddresses) > 0 {
		return &gw.NatGatewayAddresses[0]
	}
	return nil
}

// natGatewayStringsAdded returns the entries in want that are not in have,
// preserving the order of want, so an add or remove set is computed by passing
// the two sides in the right order.
func natGatewayStringsAdded(have, want []string) []string {
	if len(want) == 0 {
		return nil
	}
	existing := make(map[string]bool, len(have))
	for _, h := range have {
		existing[h] = true
	}
	var added []string
	for _, w := range want {
		if !existing[w] {
			added = append(added, w)
		}
	}
	return added
}

// describeNatGateway fetches the gateway with the given id. EC2 reports a missing
// gateway by service code on an HTTP 400, never a 404, so the not-found code maps
// to runtime.ErrNotFound. A record that reads as deleted, an empty result, or a
// returned id that does not match the requested one means the same: the gateway
// the caller asked for is gone.
func describeNatGateway(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.NatGateway, error) {
	resp, err := client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
		NatGatewayIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, natGatewayNotFoundCode) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe nat gateways: %w", err)
	}
	if len(resp.NatGateways) == 0 {
		return nil, runtime.ErrNotFound
	}
	gw := resp.NatGateways[0]
	if gw.State == ec2types.NatGatewayStateDeleted {
		return nil, runtime.ErrNotFound
	}
	if aws.ToString(gw.NatGatewayId) != id {
		return nil, runtime.ErrNotFound
	}
	return &gw, nil
}

// natGatewayNotFoundCode is the EC2 service error code for a NAT gateway that
// does not exist, returned on an HTTP 400 rather than a 404.
const natGatewayNotFoundCode = "NatGatewayNotFound"

// natGatewayCreateNotFoundLimit is how many consecutive not-found describes the
// create wait tolerates before giving up, covering the brief window where a
// just-created gateway is not yet visible to a describe.
const natGatewayCreateNotFoundLimit = 20

// natGatewayAddressConfirmations is how many consecutive matching observations
// an associate or disassociate wait requires before it is done, since an address
// status read is eventually consistent across replicas.
const natGatewayAddressConfirmations = 5
