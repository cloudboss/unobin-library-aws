package ec2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// VpcEndpointResource is a private connection from a VPC to an AWS service or an
// endpoint service. The VPC, the service it targets, and the endpoint type are
// fixed when the endpoint is created, so a change to any of them replaces it;
// everything else is reconciled in place by a single ModifyVpcEndpoint call.
// CreateVpcEndpoint accepts every input here. After create the endpoint moves
// through a pending state to available -- or to pending-acceptance for a
// service that requires the owner to accept the connection, which is also a
// settled state -- before its DNS entries, network interfaces, and prefix list
// fill in, so Create waits and then reads to return those settled values.
//
// vpc-endpoint-type is one of Gateway (the server-side default), Interface, or
// GatewayLoadBalancer. ip-address-type is one of ipv4, dualstack, or ipv6. The
// route-table, security-group, and subnet id lists are reconciled as set
// differences against the prior apply, sending only the ids that were added or
// removed. policy is the endpoint access policy as authored; removing it on a
// later apply resets the endpoint to the default full-access policy.
// private-dns-enabled associates a private hosted zone for an interface
// endpoint and is reconciled in place rather than forcing a replacement.
type VpcEndpointResource struct {
	VpcId             string                 `ub:"vpc-id"`
	ServiceName       string                 `ub:"service-name"`
	VpcEndpointType   *string                `ub:"vpc-endpoint-type"`
	PrivateDnsEnabled *bool                  `ub:"private-dns-enabled"`
	IpAddressType     *string                `ub:"ip-address-type"`
	Policy            *string                `ub:"policy"`
	RouteTableIds     *[]string              `ub:"route-table-ids"`
	SecurityGroupIds  *[]string              `ub:"security-group-ids"`
	SubnetIds         *[]string              `ub:"subnet-ids"`
	DnsOptions        *VpcEndpointDnsOptions `ub:"dns-options"`
	Tags              *map[string]string     `ub:"tags"`
}

// VpcEndpointResourceOutput holds the values EC2 computes for an endpoint. The id is the
// endpoint's handle and the not-found subject. state is its lifecycle state and
// owner-id the account that owns it. dns-entries are the names an interface
// endpoint publishes, and network-interface-ids the interfaces it owns; both
// settle after the endpoint becomes available. prefix-list-id and cidr-blocks
// describe a gateway endpoint's service prefix list, read separately and left
// empty for an interface endpoint that has none. policy is the effective access
// policy the API returns, which includes the default full-access policy when no
// policy was set, so a reader sees the cloud's value under the same name the
// input uses.
type VpcEndpointResourceOutput struct {
	VpcEndpointId       string                `ub:"vpc-endpoint-id"`
	State               string                `ub:"state"`
	OwnerId             string                `ub:"owner-id"`
	DnsEntries          []VpcEndpointDnsEntry `ub:"dns-entries"`
	NetworkInterfaceIds []string              `ub:"network-interface-ids"`
	PrefixListId        string                `ub:"prefix-list-id"`
	CidrBlocks          []string              `ub:"cidr-blocks"`
	Policy              string                `ub:"policy"`
}

func (r *VpcEndpointResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when an endpoint is created. The VPC,
// the service it targets, and the endpoint type cannot change on an existing
// endpoint, so a change to any of them requires a new one. private-dns-enabled
// is deliberately not here: EC2 updates it in place for an interface endpoint
// through ModifyVpcEndpoint, and a gateway or Gateway Load Balancer endpoint
// cannot enable it at all, so reconciling it in Update is correct for every
// type.
func (r *VpcEndpointResource) ReplaceFields() []string {
	return []string{"vpc-id", "service-name", "vpc-endpoint-type"}
}

// Constraints declares the value rules EC2 enforces on an endpoint's inputs. The
// endpoint type is one of the three supported kinds, the IP address type is one
// of the three address families, and the DNS-options record-ip type is one of
// the four record forms; all are optional, so each rule applies only when its
// field is set. The structural exclusivity among service-name and the
// VPC Lattice arn fields does not apply here: this resource targets only a named
// service, so service-name is required and the arn fields are out of scope.
func (r VpcEndpointResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.VpcEndpointType)).
			Require(constraint.OneOf(r.VpcEndpointType,
				"Gateway", "Interface", "GatewayLoadBalancer")).
			Message("vpc-endpoint-type must be Gateway, Interface, or GatewayLoadBalancer"),
		constraint.When(constraint.Present(r.IpAddressType)).
			Require(constraint.OneOf(r.IpAddressType, "ipv4", "dualstack", "ipv6")).
			Message("ip-address-type must be ipv4, dualstack, or ipv6"),
		constraint.When(constraint.Present(r.DnsOptions.DnsRecordIpType)).
			Require(constraint.OneOf(r.DnsOptions.DnsRecordIpType,
				"ipv4", "dualstack", "ipv6", "service-defined")).
			Message("dns-options dns-record-ip-type must be ipv4, dualstack, ipv6, " +
				"or service-defined"),
	}
}

func (r *VpcEndpointResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*VpcEndpointResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// An omitted private-dns-enabled is sent as an explicit false: the
	// declared default is false, and leaving the field unset would let EC2
	// pick its own default for an Interface endpoint.
	privateDns := r.PrivateDnsEnabled
	if privateDns == nil {
		privateDns = aws.Bool(false)
	}
	in := &ec2.CreateVpcEndpointInput{
		VpcId:             aws.String(r.VpcId),
		ServiceName:       aws.String(r.ServiceName),
		PrivateDnsEnabled: privateDns,
		PolicyDocument:    r.Policy,
		RouteTableIds:     ptr.Value(r.RouteTableIds),
		SecurityGroupIds:  ptr.Value(r.SecurityGroupIds),
		SubnetIds:         ptr.Value(r.SubnetIds),
		DnsOptions:        r.DnsOptions.to(),
		TagSpecifications: tagSpecifications(ec2types.ResourceTypeVpcEndpoint, ptr.Value(r.Tags)),
	}
	if r.VpcEndpointType != nil {
		in.VpcEndpointType = ec2types.VpcEndpointType(*r.VpcEndpointType)
	}
	if r.IpAddressType != nil {
		in.IpAddressType = ec2types.IpAddressType(*r.IpAddressType)
	}
	resp, err := client.CreateVpcEndpoint(ctx, in)
	// Some partitions, such as the ISO partitions, cannot tag an endpoint as it
	// is created. When the tagged create fails for that reason, create the
	// endpoint without tags and apply them with a separate call below.
	taggedSeparately := false
	if err != nil && in.TagSpecifications != nil &&
		partition.UnsupportedOperation(region(client), err) {
		in.TagSpecifications = nil
		taggedSeparately = true
		resp, err = client.CreateVpcEndpoint(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create vpc endpoint: %w", err)
	}
	id := aws.ToString(resp.VpcEndpoint.VpcEndpointId)
	if taggedSeparately && len(ptr.Value(r.Tags)) > 0 {
		if err := syncTags(ctx, client, id, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	if err := r.waitAvailable(ctx, client, id); err != nil {
		return nil, err
	}
	// CreateVpcEndpoint returns before the endpoint settles: its DNS entries,
	// network interfaces, effective policy, and a gateway endpoint's prefix list
	// all fill in during or after the wait, so read them from a describe.
	return r.read(ctx, client, id)
}

func (r *VpcEndpointResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *VpcEndpointResourceOutput,
) (*VpcEndpointResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.VpcEndpointId)
}

func (r *VpcEndpointResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[VpcEndpointResource, *VpcEndpointResourceOutput],
) (*VpcEndpointResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.VpcEndpointId
	in := r.buildModify(id, prior)
	if in != nil {
		if _, err := client.ModifyVpcEndpoint(ctx, in); err != nil {
			return nil, fmt.Errorf("modify vpc endpoint: %w", err)
		}
		if err := r.waitAvailable(ctx, client, id); err != nil {
			return nil, err
		}
	}
	// Tags ride their own calls, so reconcile them whenever they changed, the
	// same as the other EC2 resources.
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := syncTags(ctx, client, id, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, id)
}

func (r *VpcEndpointResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *VpcEndpointResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.VpcEndpointId
	resp, err := client.DeleteVpcEndpoints(ctx, &ec2.DeleteVpcEndpointsInput{
		VpcEndpointIds: []string{id},
	})
	if err != nil {
		// An endpoint that is already gone is a successful delete. EC2 uses the
		// non-Id-suffixed code for this on the batch delete.
		if isNotFound(err, "InvalidVpcEndpoint.NotFound") {
			return nil
		}
		return fmt.Errorf("delete vpc endpoints: %w", err)
	}
	// DeleteVpcEndpoints answers 200 with a per-item Unsuccessful list rather
	// than an error, so a per-endpoint failure is reported there. A not-found
	// item means the endpoint is already gone.
	for _, item := range resp.Unsuccessful {
		if aws.ToString(item.ResourceId) != id || item.Error == nil {
			continue
		}
		code := aws.ToString(item.Error.Code)
		if code == "InvalidVpcEndpoint.NotFound" {
			return nil
		}
		return fmt.Errorf("delete vpc endpoint %s: %s: %s",
			id, code, aws.ToString(item.Error.Message))
	}
	return r.waitDeleted(ctx, client, id)
}

// buildModify assembles the single ModifyVpcEndpoint request for an update,
// setting only the sub-parts whose input changed since the prior apply. It
// returns nil when nothing changed, so an unchanged endpoint takes no call at
// all and the reconcile stays idempotent. The id lists go as add and remove set
// differences. A policy that changed to absent resets the endpoint to the
// default policy; a policy that changed to a value sends that document. Enabling
// private DNS re-sends the DNS options alongside the toggle, the pairing EC2
// expects.
func (r *VpcEndpointResource) buildModify(
	id string, prior runtime.Prior[VpcEndpointResource, *VpcEndpointResourceOutput],
) *ec2.ModifyVpcEndpointInput {
	in := &ec2.ModifyVpcEndpointInput{VpcEndpointId: aws.String(id)}
	changed := false
	if ptr.Value(r.RouteTableIds) != nil &&
		runtime.Changed(ptr.Value(prior.Inputs.RouteTableIds), ptr.Value(r.RouteTableIds)) {
		add, remove := stringSetDelta(ptr.Value(prior.Inputs.RouteTableIds),
			ptr.Value(r.RouteTableIds))
		in.AddRouteTableIds = add
		in.RemoveRouteTableIds = remove
		changed = changed || len(add) > 0 || len(remove) > 0
	}
	if ptr.Value(r.SecurityGroupIds) != nil &&
		runtime.Changed(ptr.Value(prior.Inputs.SecurityGroupIds), ptr.Value(r.SecurityGroupIds)) {
		add, remove := stringSetDelta(ptr.Value(prior.Inputs.SecurityGroupIds),
			ptr.Value(r.SecurityGroupIds))
		in.AddSecurityGroupIds = add
		in.RemoveSecurityGroupIds = remove
		changed = changed || len(add) > 0 || len(remove) > 0
	}
	if ptr.Value(r.SubnetIds) != nil &&
		runtime.Changed(ptr.Value(prior.Inputs.SubnetIds), ptr.Value(r.SubnetIds)) {
		add, remove := stringSetDelta(ptr.Value(prior.Inputs.SubnetIds), ptr.Value(r.SubnetIds))
		in.AddSubnetIds = add
		in.RemoveSubnetIds = remove
		changed = changed || len(add) > 0 || len(remove) > 0
	}
	if runtime.Changed(prior.Inputs.IpAddressType, r.IpAddressType) && r.IpAddressType != nil {
		in.IpAddressType = ec2types.IpAddressType(*r.IpAddressType)
		changed = true
	}
	if runtime.Changed(prior.Inputs.Policy, r.Policy) {
		if r.Policy == nil {
			in.ResetPolicy = aws.Bool(true)
		} else {
			in.PolicyDocument = r.Policy
		}
		changed = true
	}
	// ModifyVpcEndpoint has no reset for the DNS options, so a removed block
	// leaves the last-set options in place; only a present block is sent.
	if runtime.Changed(prior.Inputs.DnsOptions, r.DnsOptions) && r.DnsOptions != nil {
		in.DnsOptions = r.DnsOptions.to()
		changed = true
	}
	if runtime.Changed(prior.Inputs.PrivateDnsEnabled, r.PrivateDnsEnabled) &&
		r.PrivateDnsEnabled != nil {
		in.PrivateDnsEnabled = r.PrivateDnsEnabled
		changed = true
		// EC2 expects the DNS options resent when private DNS is turned on, so
		// include them even if the options block itself did not change.
		if *r.PrivateDnsEnabled && in.DnsOptions == nil {
			in.DnsOptions = r.DnsOptions.to()
		}
	}
	if !changed {
		return nil
	}
	return in
}

// read fetches the endpoint by id and returns its computed outputs. A gateway
// endpoint's prefix list is read by a second describe keyed on the service name;
// that lookup swallows its own not-found, since an interface endpoint has no
// prefix list, but any other error fails the read.
func (r *VpcEndpointResource) read(
	ctx context.Context, client *ec2.Client, id string,
) (*VpcEndpointResourceOutput, error) {
	endpoint, err := describeVpcEndpoint(ctx, client, id)
	if err != nil {
		return nil, err
	}
	out := &VpcEndpointResourceOutput{
		VpcEndpointId:       aws.ToString(endpoint.VpcEndpointId),
		State:               string(endpoint.State),
		OwnerId:             aws.ToString(endpoint.OwnerId),
		NetworkInterfaceIds: endpoint.NetworkInterfaceIds,
		Policy:              aws.ToString(endpoint.PolicyDocument),
	}
	for _, entry := range endpoint.DnsEntries {
		out.DnsEntries = append(out.DnsEntries, VpcEndpointDnsEntry{
			DnsName:      aws.ToString(entry.DnsName),
			HostedZoneId: aws.ToString(entry.HostedZoneId),
		})
	}
	serviceName := aws.ToString(endpoint.ServiceName)
	if serviceName != "" {
		prefixID, cidrs, err := vpcEndpointPrefixList(ctx, client, serviceName)
		if err != nil {
			return nil, err
		}
		out.PrefixListId = prefixID
		out.CidrBlocks = cidrs
	}
	return out, nil
}

// waitAvailable polls the endpoint until it reaches a settled state. Both
// available and pending-acceptance are success: an endpoint to a service that
// requires the owner to accept the connection settles at pending-acceptance and
// goes no further on its own. A just-created endpoint can briefly describe as
// not-found, which keeps the poll going rather than aborting it. An endpoint
// that enters the failed state stops the wait with the reason EC2 records, so
// the caller sees why instead of a bare timeout.
func (r *VpcEndpointResource) waitAvailable(
	ctx context.Context,
	client *ec2.Client,
	id string,
) error {
	what := fmt.Sprintf("vpc endpoint %s to become available", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		endpoint, err := describeVpcEndpoint(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		switch strings.ToLower(string(endpoint.State)) {
		case "available", "pendingacceptance":
			return true, nil
		case "failed":
			return false, vpcEndpointFailure(endpoint)
		default:
			return false, nil
		}
	}, wait.WithTimeout(10*time.Minute), wait.WithInterval(5*time.Second))
}

// waitDeleted polls the endpoint until it is gone. The describe maps both a
// not-found and a deleted-state body to runtime.ErrNotFound, so that error is
// the gone signal; EC2 reads are eventually consistent, so the gone
// observation must hold twice in a row before the wait succeeds.
func (r *VpcEndpointResource) waitDeleted(
	ctx context.Context,
	client *ec2.Client,
	id string,
) error {
	what := fmt.Sprintf("vpc endpoint %s to be deleted", id)
	return wait.UntilStable(ctx, what, 2, func(ctx context.Context) (bool, error) {
		_, err := describeVpcEndpoint(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, wait.WithTimeout(10*time.Minute), wait.WithInterval(5*time.Second))
}

// vpcEndpointFailure builds the error for an endpoint that reached the failed
// state, preferring the last recorded error so the caller sees EC2's reason.
func vpcEndpointFailure(endpoint *ec2types.VpcEndpoint) error {
	id := aws.ToString(endpoint.VpcEndpointId)
	if endpoint.LastError != nil {
		return fmt.Errorf("vpc endpoint %s failed: %s: %s", id,
			aws.ToString(endpoint.LastError.Code),
			aws.ToString(endpoint.LastError.Message))
	}
	return fmt.Errorf("vpc endpoint %s entered state failed", id)
}

// vpcEndpointPrefixList reads the service prefix list a gateway endpoint routes
// to, returning its id and CIDR blocks. The lookup is keyed on the service name;
// a service with no prefix list, as for an interface endpoint, is not an error
// and yields an empty id and no blocks.
func vpcEndpointPrefixList(
	ctx context.Context, client *ec2.Client, serviceName string,
) (string, []string, error) {
	resp, err := client.DescribePrefixLists(ctx, &ec2.DescribePrefixListsInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("prefix-list-name"),
			Values: []string{serviceName},
		}},
	})
	if err != nil {
		return "", nil, fmt.Errorf("describe prefix lists: %w", err)
	}
	if len(resp.PrefixLists) == 0 {
		return "", nil, nil
	}
	pl := resp.PrefixLists[0]
	return aws.ToString(pl.PrefixListId), pl.Cidrs, nil
}

// describeVpcEndpoint fetches the endpoint with the given id. EC2 reports a
// missing endpoint by service code on an HTTP 400, never a 404, so the not-found
// code maps to runtime.ErrNotFound; an empty result, an id mismatch from a
// lagging replica, or a deleted-state body means the same.
func describeVpcEndpoint(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.VpcEndpoint, error) {
	resp, err := client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		VpcEndpointIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, "InvalidVpcEndpointId.NotFound") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe vpc endpoints: %w", err)
	}
	if len(resp.VpcEndpoints) == 0 {
		return nil, runtime.ErrNotFound
	}
	endpoint := resp.VpcEndpoints[0]
	if aws.ToString(endpoint.VpcEndpointId) != id {
		return nil, runtime.ErrNotFound
	}
	if strings.ToLower(string(endpoint.State)) == "deleted" {
		return nil, runtime.ErrNotFound
	}
	return &endpoint, nil
}

// stringSetDelta returns the values to add and remove to turn the prior id set
// into the desired one. A value in desired but not prior is added; a value in
// prior but not desired is removed. Order is not significant to the API, so the
// result follows the order of desired for adds and prior for removes.
func stringSetDelta(prior, desired []string) (add, remove []string) {
	priorSet := make(map[string]struct{}, len(prior))
	for _, v := range prior {
		priorSet[v] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desired))
	for _, v := range desired {
		desiredSet[v] = struct{}{}
	}
	for _, v := range desired {
		if _, ok := priorSet[v]; !ok {
			add = append(add, v)
		}
	}
	for _, v := range prior {
		if _, ok := desiredSet[v]; !ok {
			remove = append(remove, v)
		}
	}
	return add, remove
}
