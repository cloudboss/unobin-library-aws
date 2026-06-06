package ec2

import (
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// VpcEndpointDnsOptions configures how a VPC endpoint publishes DNS records.
// Both members are reconciled in place through ModifyVpcEndpoint, so a change
// to either updates the endpoint rather than replacing it. dns-record-ip-type
// selects which address family the endpoint's DNS records resolve to and is one
// of ipv4, dualstack, ipv6, or service-defined; that enum is checked in the
// resource's create and update paths rather than as a derived constraint,
// because a rule on a nested block field does not derive.
// private-dns-only-for-inbound-resolver-endpoint, when true, routes only
// inbound resolver-endpoint traffic to a service that offers both gateway and
// interface endpoints. Each member is sent only when set.
type VpcEndpointDnsOptions struct {
	DnsRecordIpType                          *string `ub:"dns-record-ip-type"`
	PrivateDnsOnlyForInboundResolverEndpoint *bool   `ub:"private-dns-only-for-inbound-resolver-endpoint"`
}

// to converts the block into the SDK DNS-options specification, returning nil
// for an absent block so an endpoint without DNS options sends none. The string
// record-ip type maps onto the SDK enum, and the inbound-resolver flag passes
// through as a pointer so an unset value stays absent from the request.
func (b *VpcEndpointDnsOptions) to() *ec2types.DnsOptionsSpecification {
	if b == nil {
		return nil
	}
	spec := &ec2types.DnsOptionsSpecification{
		PrivateDnsOnlyForInboundResolverEndpoint: b.PrivateDnsOnlyForInboundResolverEndpoint,
	}
	if b.DnsRecordIpType != nil {
		spec.DnsRecordIpType = ec2types.DnsRecordIpType(*b.DnsRecordIpType)
	}
	return spec
}

// VpcEndpointDnsEntry is one published DNS name for an interface endpoint and
// the hosted zone that serves it. Both values are computed by EC2 and settle
// after the endpoint becomes available, so they appear only in the output.
type VpcEndpointDnsEntry struct {
	DnsName      string `ub:"dns-name"`
	HostedZoneId string `ub:"hosted-zone-id"`
}
