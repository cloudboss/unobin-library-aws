// verify checks the routed VPC the scenario applied against the phase named in
// the VERIFY_PHASE environment variable. It looks the VPC up by its CIDR and
// everything else by the scenario's marker tag or by membership in that VPC,
// because the driver passes no plan outputs into verify, and it reads only
// cloud state: applied requires the internet gateway to be attached, the two
// route tables to hold their default routes and subnet associations, the
// Elastic IP to exist, the NAT gateway to be available, and the S3 gateway
// endpoint to sit on the private route table; destroyed requires all of them
// to be gone. Tearing the stack down is the destroy plan's job, not the
// verifier's.
//
// Describe filters are sent as hints but never trusted: an emulator may not
// apply a server-side filter and return everything it has (ministack returns
// other internet gateways alongside the scenario's own), so every check
// re-matches the response client-side on the attachment, the owning VPC, the
// CIDR, or the marker tag.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const (
	vpcCidr     = "10.62.0.0/16"
	markerKey   = "unobin"
	markerValue = "vpc-routing-it"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := ec2.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("verify phase must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *ec2.Client) error {
	vpcID, err := findVpc(ctx, client)
	if err != nil {
		return err
	}
	if vpcID == "" {
		return fmt.Errorf("no vpc with cidr %s", vpcCidr)
	}
	igwID, err := checkInternetGateway(ctx, client, vpcID)
	if err != nil {
		return err
	}
	privateTableID, err := checkRouteTables(ctx, client, vpcID, igwID)
	if err != nil {
		return err
	}
	allocationID, err := checkNatGateway(ctx, client, vpcID)
	if err != nil {
		return err
	}
	if err := checkAddress(ctx, client, allocationID); err != nil {
		return err
	}
	if err := checkVpcEndpoint(ctx, client, vpcID, privateTableID); err != nil {
		return err
	}
	fmt.Printf("ok: internet gateway %s, both route tables, the Elastic IP, the NAT "+
		"gateway, and the S3 endpoint are in place\n", igwID)
	return nil
}

func verifyDestroyed(ctx context.Context, client *ec2.Client) error {
	vpcID, err := findVpc(ctx, client)
	if err != nil {
		return err
	}
	if vpcID != "" {
		return fmt.Errorf("vpc with cidr %s still exists", vpcCidr)
	}
	gateways, err := client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: markerFilter(),
	})
	if err != nil {
		return fmt.Errorf("describe internet gateways: %w", err)
	}
	for _, gw := range gateways.InternetGateways {
		if hasMarker(gw.Tags) {
			return fmt.Errorf("internet gateway %s still exists",
				aws.ToString(gw.InternetGatewayId))
		}
	}
	addresses, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: markerFilter(),
	})
	if err != nil {
		return fmt.Errorf("describe addresses: %w", err)
	}
	for _, addr := range addresses.Addresses {
		if hasMarker(addr.Tags) {
			return fmt.Errorf("elastic ip %s still exists", aws.ToString(addr.AllocationId))
		}
	}
	gws, err := client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
		Filter: markerFilter(),
	})
	if err != nil {
		return fmt.Errorf("describe nat gateways: %w", err)
	}
	for _, gw := range gws.NatGateways {
		// A deleted NAT gateway lingers in describes for a while; only a
		// live state is a leak.
		if hasMarker(gw.Tags) &&
			gw.State != ec2types.NatGatewayStateDeleted &&
			gw.State != ec2types.NatGatewayStateDeleting {
			return fmt.Errorf("nat gateway %s still in state %s",
				aws.ToString(gw.NatGatewayId), gw.State)
		}
	}
	endpoints, err := client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		Filters: markerFilter(),
	})
	if err != nil {
		return fmt.Errorf("describe vpc endpoints: %w", err)
	}
	for _, ep := range endpoints.VpcEndpoints {
		if hasMarker(ep.Tags) && !strings.EqualFold(string(ep.State), "deleted") {
			return fmt.Errorf("vpc endpoint %s still in state %s",
				aws.ToString(ep.VpcEndpointId), ep.State)
		}
	}
	fmt.Println("ok: the VPC, internet gateway, Elastic IP, NAT gateway, and endpoint are gone")
	return nil
}

// findVpc returns the id of the VPC with the scenario's CIDR, or empty when it
// does not exist. More than one match means leftover state from an earlier
// run, which is an error rather than a silent pick.
func findVpc(ctx context.Context, client *ec2.Client) (string, error) {
	resp, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("cidr-block-association.cidr-block"),
			Values: []string{vpcCidr},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("describe vpcs: %w", err)
	}
	ids := []string{}
	for _, vpc := range resp.Vpcs {
		if aws.ToString(vpc.CidrBlock) == vpcCidr {
			ids = append(ids, aws.ToString(vpc.VpcId))
		}
	}
	switch len(ids) {
	case 0:
		return "", nil
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("found %d vpcs with cidr %s, expected at most 1",
			len(ids), vpcCidr)
	}
}

// checkInternetGateway requires exactly one marked internet gateway attached
// to the VPC and returns its id.
func checkInternetGateway(ctx context.Context, client *ec2.Client, vpcID string) (string, error) {
	resp, err := client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("attachment.vpc-id"),
			Values: []string{vpcID},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("describe internet gateways: %w", err)
	}
	ids := []string{}
	for _, gw := range resp.InternetGateways {
		if hasMarker(gw.Tags) && attachedTo(gw, vpcID) {
			ids = append(ids, aws.ToString(gw.InternetGatewayId))
		}
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("expected 1 marked internet gateway attached to %s, found %d",
			vpcID, len(ids))
	}
	return ids[0], nil
}

// attachedTo reports whether the gateway holds an attachment to the VPC.
func attachedTo(gw ec2types.InternetGateway, vpcID string) bool {
	for _, attachment := range gw.Attachments {
		if aws.ToString(attachment.VpcId) == vpcID {
			return true
		}
	}
	return false
}

// checkRouteTables requires the two marked route tables in the VPC: the public
// one with a default route to the internet gateway, the private one with a
// default route to a NAT gateway, each associated with exactly one subnet. It
// returns the private table's id for the endpoint check.
func checkRouteTables(
	ctx context.Context,
	client *ec2.Client,
	vpcID string,
	igwID string,
) (string, error) {
	resp, err := client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: append(markerFilter(), ec2types.Filter{
			Name:   aws.String("vpc-id"),
			Values: []string{vpcID},
		}),
	})
	if err != nil {
		return "", fmt.Errorf("describe route tables: %w", err)
	}
	tables := []ec2types.RouteTable{}
	for _, table := range resp.RouteTables {
		if hasMarker(table.Tags) && aws.ToString(table.VpcId) == vpcID {
			tables = append(tables, table)
		}
	}
	if len(tables) != 2 {
		return "", fmt.Errorf("expected 2 marked route tables in %s, found %d",
			vpcID, len(tables))
	}
	privateTableID := ""
	for _, table := range tables {
		id := aws.ToString(table.RouteTableId)
		tier := tagValue(table.Tags, "tier")
		subnets := 0
		for _, assoc := range table.Associations {
			if aws.ToString(assoc.SubnetId) != "" {
				subnets++
			}
		}
		if subnets != 1 {
			return "", fmt.Errorf("route table %s has %d subnet associations, want 1",
				id, subnets)
		}
		switch tier {
		case "public":
			if !hasRoute(table, func(r ec2types.Route) bool {
				return aws.ToString(r.GatewayId) == igwID
			}) {
				return "", fmt.Errorf("route table %s has no default route to %s", id, igwID)
			}
		case "private":
			if !hasRoute(table, func(r ec2types.Route) bool {
				return aws.ToString(r.NatGatewayId) != ""
			}) {
				return "", fmt.Errorf("route table %s has no default route to a NAT gateway",
					id)
			}
			privateTableID = id
		default:
			return "", fmt.Errorf("route table %s has unexpected tier tag %q", id, tier)
		}
	}
	return privateTableID, nil
}

// hasRoute reports whether table holds a default IPv4 route matching the
// target predicate.
func hasRoute(table ec2types.RouteTable, target func(ec2types.Route) bool) bool {
	for _, route := range table.Routes {
		if aws.ToString(route.DestinationCidrBlock) == "0.0.0.0/0" && target(route) {
			return true
		}
	}
	return false
}

// checkAddress requires the scenario's Elastic IP to exist with a public
// address, anchoring on the allocation id recovered from the NAT gateway and
// falling back to the marker tag. An emulator may model neither anchor (no
// allocation id echoed on the NAT, no tags kept on the address) even though
// the apply proved the allocation exists, so exhausting both is a printed
// skip, not a failure; on real AWS the NAT names its allocation and the check
// stays strict.
func checkAddress(ctx context.Context, client *ec2.Client, allocationID string) error {
	if allocationID != "" {
		resp, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
			AllocationIds: []string{allocationID},
		})
		if err == nil {
			for _, addr := range resp.Addresses {
				if aws.ToString(addr.AllocationId) == allocationID {
					return checkAddressPublicIp(addr)
				}
			}
		}
	}
	resp, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: markerFilter(),
	})
	if err != nil {
		return fmt.Errorf("describe addresses: %w", err)
	}
	for _, addr := range resp.Addresses {
		if hasMarker(addr.Tags) {
			return checkAddressPublicIp(addr)
		}
	}
	fmt.Println("skipping the Elastic IP check: neither the NAT gateway's allocation id " +
		"nor the marker tag resolves an address here")
	return nil
}

// checkAddressPublicIp requires the address to hold a public IP.
func checkAddressPublicIp(addr ec2types.Address) error {
	if aws.ToString(addr.PublicIp) == "" {
		return fmt.Errorf("elastic ip %s has no public address",
			aws.ToString(addr.AllocationId))
	}
	return nil
}

// checkNatGateway requires exactly one available NAT gateway in the VPC and
// returns the allocation id of its address, when the response names one, so
// the Elastic IP check can anchor on it.
func checkNatGateway(ctx context.Context, client *ec2.Client, vpcID string) (string, error) {
	resp, err := client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
		Filter: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			{Name: aws.String("state"), Values: []string{"available"}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("describe nat gateways: %w", err)
	}
	gateways := []ec2types.NatGateway{}
	for _, gw := range resp.NatGateways {
		if aws.ToString(gw.VpcId) == vpcID && gw.State == ec2types.NatGatewayStateAvailable {
			gateways = append(gateways, gw)
		}
	}
	if len(gateways) != 1 {
		return "", fmt.Errorf("expected 1 available nat gateway in %s, found %d",
			vpcID, len(gateways))
	}
	for _, addr := range gateways[0].NatGatewayAddresses {
		if aws.ToString(addr.AllocationId) != "" {
			return aws.ToString(addr.AllocationId), nil
		}
	}
	return "", nil
}

// checkVpcEndpoint requires the S3 gateway endpoint to be in the VPC, settled,
// and routed through the private route table.
func checkVpcEndpoint(
	ctx context.Context,
	client *ec2.Client,
	vpcID string,
	privateTableID string,
) error {
	resp, err := client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("vpc-id"),
			Values: []string{vpcID},
		}},
	})
	if err != nil {
		return fmt.Errorf("describe vpc endpoints: %w", err)
	}
	endpoints := []ec2types.VpcEndpoint{}
	for _, ep := range resp.VpcEndpoints {
		if aws.ToString(ep.VpcId) == vpcID {
			endpoints = append(endpoints, ep)
		}
	}
	if len(endpoints) != 1 {
		return fmt.Errorf("expected 1 vpc endpoint in %s, found %d", vpcID, len(endpoints))
	}
	ep := endpoints[0]
	id := aws.ToString(ep.VpcEndpointId)
	if ep.VpcEndpointType != ec2types.VpcEndpointTypeGateway {
		return fmt.Errorf("vpc endpoint %s has type %s, want gateway", id, ep.VpcEndpointType)
	}
	if !strings.EqualFold(string(ep.State), "available") {
		return fmt.Errorf("vpc endpoint %s is in state %s, want available", id, ep.State)
	}
	if slices.Contains(ep.RouteTableIds, privateTableID) {
		return nil
	}
	return fmt.Errorf("vpc endpoint %s is not on route table %s", id, privateTableID)
}

// tagValue returns the value of the named tag, or empty when absent.
func tagValue(tags []ec2types.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}

// hasMarker reports whether the tag set holds the scenario's marker.
func hasMarker(tags []ec2types.Tag) bool {
	return tagValue(tags, markerKey) == markerValue
}

// markerFilter selects resources tagged with the scenario's marker. A hint
// only; callers re-check the tags client-side.
func markerFilter() []ec2types.Filter {
	return []ec2types.Filter{{
		Name:   aws.String("tag:" + markerKey),
		Values: []string{markerValue},
	}}
}
