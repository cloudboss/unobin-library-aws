package ec2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// Route is one route in a route table. It matches a destination -- an IPv4 or
// IPv6 CIDR block, or a prefix list -- to exactly one target, such as a gateway,
// NAT gateway, network interface, or peering connection. There is no route id;
// a route is addressed forever by its table and its destination, both fixed at
// create. Each field maps to the AWS SDK field that holds it on CreateRoute
// and ReplaceRoute.
type Route struct {
	RouteTableId                string  `ub:"route-table-id"`
	DestinationCidrBlock        *string `ub:"destination-cidr-block"`
	DestinationIpv6CidrBlock    *string `ub:"destination-ipv6-cidr-block"`
	DestinationPrefixListId     *string `ub:"destination-prefix-list-id"`
	CarrierGatewayId            *string `ub:"carrier-gateway-id"`
	CoreNetworkArn              *string `ub:"core-network-arn"`
	EgressOnlyInternetGatewayId *string `ub:"egress-only-gateway-id"`
	GatewayId                   *string `ub:"gateway-id"`
	LocalGatewayId              *string `ub:"local-gateway-id"`
	NatGatewayId                *string `ub:"nat-gateway-id"`
	NetworkInterfaceId          *string `ub:"network-interface-id"`
	TransitGatewayId            *string `ub:"transit-gateway-id"`
	VpcEndpointId               *string `ub:"vpc-endpoint-id"`
	VpcPeeringConnectionId      *string `ub:"vpc-peering-connection-id"`
}

// RouteOutput holds the one value EC2 computes for a route. A route has no
// server id -- its identity is its table and destination, both inputs -- so the
// only observable worth recording is its state: active normally, blackhole when
// its target stops existing, which then reads back as drift.
type RouteOutput struct {
	State string `ub:"state"`
}

// routeCreateRetryCodes are the codes CreateRoute may return while a freshly
// created table or target is still becoming visible. The create call retries on
// them over the create window.
var routeCreateRetryCodes = []string{
	"InvalidParameterException",
	"InvalidTransitGatewayID.NotFound",
}

// routeTableNotFoundCode is the code DescribeRouteTables returns when the route
// table itself is gone. The finder maps it to runtime.ErrNotFound, so a table
// deleted out from under a route reads the same as a route that was removed.
const routeTableNotFoundCode = "InvalidRouteTableID.NotFound"

// routeDeleteNotFoundCode is the code DeleteRoute returns when the route is
// already gone. Delete treats it as success.
const routeDeleteNotFoundCode = "InvalidRoute.NotFound"

// routeReadyTimeout bounds both the create retry and the route-found wait. A
// just-created target or table can take minutes to become visible, so the
// window is generous.
const routeReadyTimeout = 5 * time.Minute

// routeWaitInterval paces the route-found and route-gone waits. A route settles
// within seconds of CreateRoute, ReplaceRoute, or DeleteRoute returning, so a
// short interval confirms it without sleeping a full poll cycle.
const routeWaitInterval = 2 * time.Second

func (r *Route) SchemaVersion() int { return 1 }

// ReplaceFields lists the properties EC2 cannot change in place. The table a
// route lives in and the destination it matches are fixed at create; changing
// any of them recreates the route. The target updates in place via ReplaceRoute.
func (r *Route) ReplaceFields() []string {
	return []string{
		"route-table-id",
		"destination-cidr-block",
		"destination-ipv6-cidr-block",
		"destination-prefix-list-id",
	}
}

// Constraints declares the rules EC2 enforces on a route's inputs. A route
// matches exactly one destination and points at exactly one target. Three
// targets only make sense for one address family: a carrier gateway and an
// egress-only internet gateway and a VPC endpoint cannot pair with the wrong
// destination form. The gateway-id value local names the route table's own
// local route, which this resource does not manage, so it is rejected here.
func (r Route) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.DestinationCidrBlock, r.DestinationIpv6CidrBlock,
			r.DestinationPrefixListId),
		constraint.ExactlyOneOf(r.CarrierGatewayId, r.CoreNetworkArn,
			r.EgressOnlyInternetGatewayId, r.GatewayId, r.LocalGatewayId, r.NatGatewayId,
			r.NetworkInterfaceId, r.TransitGatewayId, r.VpcEndpointId,
			r.VpcPeeringConnectionId),
		constraint.ForbiddenWith(r.CarrierGatewayId, r.DestinationIpv6CidrBlock),
		constraint.ForbiddenWith(r.EgressOnlyInternetGatewayId, r.DestinationCidrBlock),
		constraint.ForbiddenWith(r.VpcEndpointId, r.DestinationPrefixListId),
		constraint.When(constraint.Present(r.GatewayId)).
			Require(constraint.NotEquals(r.GatewayId, "local")).
			Message("gateway-id cannot be local; the local route is not managed here"),
	}
}

func (r *Route) Create(ctx context.Context, cfg *awsCfg) (*RouteOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.checkNotExists(ctx, client); err != nil {
		return nil, err
	}
	if err := r.create(ctx, client); err != nil {
		return nil, err
	}
	route, err := r.waitReady(ctx, client)
	if err != nil {
		return nil, err
	}
	return &RouteOutput{State: string(route.State)}, nil
}

func (r *Route) Read(ctx context.Context, cfg *awsCfg, prior *RouteOutput) (*RouteOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	route, err := r.find(ctx, client)
	if err != nil {
		return nil, err
	}
	return &RouteOutput{State: string(route.State)}, nil
}

func (r *Route) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Route, *RouteOutput],
) (*RouteOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if r.targetChanged(prior.Inputs) {
		if err := r.replace(ctx, client); err != nil {
			return nil, err
		}
	}
	route, err := r.waitReady(ctx, client)
	if err != nil {
		return nil, err
	}
	return &RouteOutput{State: string(route.State)}, nil
}

func (r *Route) Delete(ctx context.Context, cfg *awsCfg, prior *RouteOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	if err := r.delete(ctx, client); err != nil {
		return err
	}
	return r.waitDeleted(ctx, client)
}

// checkNotExists runs the destination finder once before create. EC2 rejects a
// second CreateRoute for a destination that already has a manually created
// route, so a route already present with Origin CreateRoute is reported as an
// error here rather than left to a confusing API failure. A not-found result
// means the destination is free; any other error fails the create.
func (r *Route) checkNotExists(ctx context.Context, client *ec2.Client) error {
	route, err := r.find(ctx, client)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		return err
	}
	if route.Origin == ec2types.RouteOriginCreateRoute {
		return fmt.Errorf("route for the destination already exists in table %s",
			r.RouteTableId)
	}
	return nil
}

// create calls CreateRoute, retrying while a freshly created table or target is
// still propagating. A failure naming the gateway local is turned into guidance:
// the local route belongs to the route table and is not created here.
func (r *Route) create(ctx context.Context, client *ec2.Client) error {
	in := &ec2.CreateRouteInput{RouteTableId: aws.String(r.RouteTableId)}
	r.applyDestination(&in.DestinationCidrBlock, &in.DestinationIpv6CidrBlock,
		&in.DestinationPrefixListId)
	in.CarrierGatewayId = r.CarrierGatewayId
	in.CoreNetworkArn = r.CoreNetworkArn
	in.EgressOnlyInternetGatewayId = r.EgressOnlyInternetGatewayId
	in.GatewayId = r.GatewayId
	in.LocalGatewayId = r.LocalGatewayId
	in.NatGatewayId = r.NatGatewayId
	in.NetworkInterfaceId = r.NetworkInterfaceId
	in.TransitGatewayId = r.TransitGatewayId
	in.VpcEndpointId = r.VpcEndpointId
	in.VpcPeeringConnectionId = r.VpcPeeringConnectionId
	err := retry.OnError(ctx, routeCreateRetryable, func(ctx context.Context) error {
		_, err := client.CreateRoute(ctx, in)
		return err
	}, retry.WithTimeout(routeReadyTimeout), retry.WithInterval(routeWaitInterval))
	if err != nil {
		if routeIsLocalGatewayError(err) {
			return fmt.Errorf("the local route cannot be created; gateway-id local "+
				"names the route table's built-in local route: %w", err)
		}
		return fmt.Errorf("create route: %w", err)
	}
	return nil
}

// replace calls ReplaceRoute with the unchanged destination and the new target.
// Only the target can change in place; the destination is part of the identity.
func (r *Route) replace(ctx context.Context, client *ec2.Client) error {
	in := &ec2.ReplaceRouteInput{RouteTableId: aws.String(r.RouteTableId)}
	r.applyDestination(&in.DestinationCidrBlock, &in.DestinationIpv6CidrBlock,
		&in.DestinationPrefixListId)
	in.CarrierGatewayId = r.CarrierGatewayId
	in.CoreNetworkArn = r.CoreNetworkArn
	in.EgressOnlyInternetGatewayId = r.EgressOnlyInternetGatewayId
	in.GatewayId = r.GatewayId
	in.LocalGatewayId = r.LocalGatewayId
	in.NatGatewayId = r.NatGatewayId
	in.NetworkInterfaceId = r.NetworkInterfaceId
	in.TransitGatewayId = r.TransitGatewayId
	in.VpcEndpointId = r.VpcEndpointId
	in.VpcPeeringConnectionId = r.VpcPeeringConnectionId
	if _, err := client.ReplaceRoute(ctx, in); err != nil {
		return fmt.Errorf("replace route: %w", err)
	}
	return nil
}

// delete calls DeleteRoute, retrying while a transient parameter error clears.
// A route already gone, or the API's refusal to remove a local route, both count
// as a finished delete with nothing left to do.
func (r *Route) delete(ctx context.Context, client *ec2.Client) error {
	in := &ec2.DeleteRouteInput{RouteTableId: aws.String(r.RouteTableId)}
	r.applyDestination(&in.DestinationCidrBlock, &in.DestinationIpv6CidrBlock,
		&in.DestinationPrefixListId)
	err := retry.OnError(ctx, routeDeleteRetryable, func(ctx context.Context) error {
		_, err := client.DeleteRoute(ctx, in)
		return err
	}, retry.WithTimeout(routeReadyTimeout), retry.WithInterval(routeWaitInterval))
	if err != nil {
		if isNotFound(err, routeDeleteNotFoundCode) || routeIsLocalRemovalError(err) {
			return nil
		}
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

// applyDestination fills exactly one of the three destination pointers from the
// route's set destination. The same destination feeds CreateRoute, ReplaceRoute,
// and DeleteRoute, so the three operations share this.
func (r *Route) applyDestination(cidr, ipv6, prefixList **string) {
	switch {
	case r.DestinationCidrBlock != nil:
		*cidr = r.DestinationCidrBlock
	case r.DestinationIpv6CidrBlock != nil:
		*ipv6 = r.DestinationIpv6CidrBlock
	case r.DestinationPrefixListId != nil:
		*prefixList = r.DestinationPrefixListId
	}
}

// targetChanged reports whether any target field differs from the prior inputs.
// The destination cannot change in place, so only the targets are compared; a
// destination change forces a replace of the whole route instead.
func (r *Route) targetChanged(prior Route) bool {
	return runtime.Changed(prior.CarrierGatewayId, r.CarrierGatewayId) ||
		runtime.Changed(prior.CoreNetworkArn, r.CoreNetworkArn) ||
		runtime.Changed(prior.EgressOnlyInternetGatewayId, r.EgressOnlyInternetGatewayId) ||
		runtime.Changed(prior.GatewayId, r.GatewayId) ||
		runtime.Changed(prior.LocalGatewayId, r.LocalGatewayId) ||
		runtime.Changed(prior.NatGatewayId, r.NatGatewayId) ||
		runtime.Changed(prior.NetworkInterfaceId, r.NetworkInterfaceId) ||
		runtime.Changed(prior.TransitGatewayId, r.TransitGatewayId) ||
		runtime.Changed(prior.VpcEndpointId, r.VpcEndpointId) ||
		runtime.Changed(prior.VpcPeeringConnectionId, r.VpcPeeringConnectionId)
}

// find locates this route by describing its table and scanning the table's
// routes for the one whose destination matches. There is no DescribeRoutes, so
// the route is found only through its parent table. A table that no longer
// exists and a table that no longer holds the route both yield
// runtime.ErrNotFound: the route is gone either way.
func (r *Route) find(ctx context.Context, client *ec2.Client) (*ec2types.Route, error) {
	resp, err := client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		RouteTableIds: []string{r.RouteTableId},
	})
	if err != nil {
		if isNotFound(err, routeTableNotFoundCode) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe route tables: %w", err)
	}
	if len(resp.RouteTables) == 0 {
		return nil, runtime.ErrNotFound
	}
	for i := range resp.RouteTables[0].Routes {
		route := &resp.RouteTables[0].Routes[i]
		if r.matches(route) {
			return route, nil
		}
	}
	return nil, runtime.ErrNotFound
}

// matches reports whether route has this route's destination. An IPv4 or IPv6
// destination matches by parsed-CIDR equality, so a block the API returns in a
// different but equal spelling still matches; a prefix-list destination matches
// by string equality.
func (r *Route) matches(route *ec2types.Route) bool {
	switch {
	case r.DestinationCidrBlock != nil:
		return routeCIDREqual(aws.ToString(route.DestinationCidrBlock),
			*r.DestinationCidrBlock)
	case r.DestinationIpv6CidrBlock != nil:
		return routeCIDREqual(aws.ToString(route.DestinationIpv6CidrBlock),
			*r.DestinationIpv6CidrBlock)
	case r.DestinationPrefixListId != nil:
		return aws.ToString(route.DestinationPrefixListId) == *r.DestinationPrefixListId
	}
	return false
}

// waitReady polls the finder until the route is present on two consecutive
// reads, then returns the matched route. A route can be absent from its table
// for many polls right after CreateRoute or ReplaceRoute returns, so a not-found
// keeps the wait going rather than ending it; the last found route is returned.
func (r *Route) waitReady(ctx context.Context, client *ec2.Client) (*ec2types.Route, error) {
	var found *ec2types.Route
	what := fmt.Sprintf("route in table %s", r.RouteTableId)
	err := wait.UntilStable(ctx, what, 2, func(ctx context.Context) (bool, error) {
		route, err := r.find(ctx, client)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		found = route
		return true, nil
	}, wait.WithTimeout(routeReadyTimeout), wait.WithInterval(routeWaitInterval))
	if err != nil {
		return nil, err
	}
	return found, nil
}

// waitDeleted polls the finder until the route reads as gone on two consecutive
// reads. A route still present keeps the wait going; a not-found is the success
// the wait is waiting for, so the finder's ErrNotFound counts as ready here.
func (r *Route) waitDeleted(ctx context.Context, client *ec2.Client) error {
	what := fmt.Sprintf("route in table %s to be removed", r.RouteTableId)
	return wait.UntilStable(ctx, what, 2, func(ctx context.Context) (bool, error) {
		_, err := r.find(ctx, client)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, wait.WithTimeout(routeReadyTimeout), wait.WithInterval(routeWaitInterval))
}

// routeCreateRetryable reports whether a CreateRoute error is one of the
// transient codes worth retrying while a new table or target propagates.
func routeCreateRetryable(err error) bool {
	return isNotFound(err, routeCreateRetryCodes...)
}

// routeDeleteRetryable reports whether a DeleteRoute error is the transient
// parameter error worth retrying.
func routeDeleteRetryable(err error) bool {
	return isNotFound(err, "InvalidParameterException")
}

// routeIsLocalGatewayError reports whether err is the EC2 failure raised when a
// create targets the gateway id local, which does not exist as a real gateway.
func routeIsLocalGatewayError(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "InvalidGatewayID.NotFound" &&
			strings.Contains(apiErr.ErrorMessage(), "local")
	}
	return false
}

// routeIsLocalRemovalError reports whether err is EC2's refusal to delete a
// local route. A local route is never produced by this resource, so the refusal
// is treated as a delete that has nothing to do.
func routeIsLocalRemovalError(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "InvalidParameterValue" &&
			strings.Contains(apiErr.ErrorMessage(), "cannot remove local route")
	}
	return false
}

// routeCIDREqual reports whether two CIDR strings name the same block, comparing
// the parsed network rather than the raw text. EC2 may echo a destination in a
// canonical form that differs character by character from the configured value
// yet names the same network, so the route is matched on parsed equality. A
// value that does not parse falls back to string equality.
func routeCIDREqual(a, b string) bool {
	if a == b {
		return true
	}
	aip, anet, err := net.ParseCIDR(a)
	if err != nil {
		return false
	}
	bip, bnet, err := net.ParseCIDR(b)
	if err != nil {
		return false
	}
	return aip.String() == bip.String() && anet.String() == bnet.String()
}
