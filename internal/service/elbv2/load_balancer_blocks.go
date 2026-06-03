package elbv2

import (
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// LoadBalancerAccessLogs is the load balancer's S3 access-logging configuration.
// Enabled turns access logs on or off; the default is off. Bucket names the S3
// bucket the logs are delivered to and is required when logging is on; Prefix is
// an optional key prefix under that bucket. The three fields are not their own
// API object: each rides the attribute list as one access_logs.s3.* attribute,
// so the block is folded into ModifyLoadBalancerAttributes rather than written
// through a call of its own. Access logs apply to Application and Network Load
// Balancers; the resource only sends them for those types.
type LoadBalancerAccessLogs struct {
	Enabled *bool   `ub:"enabled"`
	Bucket  *string `ub:"bucket"`
	Prefix  *string `ub:"prefix"`
}

// LoadBalancerConnectionLogs is the load balancer's S3 connection-logging
// configuration. Its fields match access logs but are delivered through the
// connection_logs.s3.* attributes. Connection logs apply to Application Load
// Balancers only; the resource only sends them for that type.
type LoadBalancerConnectionLogs struct {
	Enabled *bool   `ub:"enabled"`
	Bucket  *string `ub:"bucket"`
	Prefix  *string `ub:"prefix"`
}

// LoadBalancerSubnetMapping maps the load balancer to one subnet, optionally
// pinning the address it uses there. SubnetId names the subnet. The remaining
// fields apply to Network Load Balancers: AllocationId attaches an Elastic IP,
// PrivateIPv4Address pins a private IPv4 for an internal load balancer,
// IPv6Address pins an IPv6 address, and SourceNatIPv6Prefix sets the source-NAT
// prefix for a UDP listener. A subnet mapping is part of the create call and is
// reconciled on update by SetSubnets, so the block holds the same fields as the
// SDK SubnetMapping.
type LoadBalancerSubnetMapping struct {
	SubnetId            string  `ub:"subnet-id"`
	AllocationId        *string `ub:"allocation-id"`
	PrivateIPv4Address  *string `ub:"private-ipv4-address"`
	IPv6Address         *string `ub:"ipv6-address"`
	SourceNatIPv6Prefix *string `ub:"source-nat-ipv6-prefix"`
}

// subnetMappings converts the desired subnet-mapping blocks into the SDK type
// for the create and SetSubnets calls, sending only the pinned-address fields
// the user set so an omitted one lets AWS choose.
func subnetMappings(mappings []LoadBalancerSubnetMapping) []elbv2types.SubnetMapping {
	if len(mappings) == 0 {
		return nil
	}
	out := make([]elbv2types.SubnetMapping, 0, len(mappings))
	for i := range mappings {
		m := mappings[i]
		sm := elbv2types.SubnetMapping{SubnetId: aws.String(m.SubnetId)}
		if m.AllocationId != nil {
			sm.AllocationId = m.AllocationId
		}
		if m.PrivateIPv4Address != nil {
			sm.PrivateIPv4Address = m.PrivateIPv4Address
		}
		if m.IPv6Address != nil {
			sm.IPv6Address = m.IPv6Address
		}
		if m.SourceNatIPv6Prefix != nil {
			sm.SourceNatIpv6Prefix = m.SourceNatIPv6Prefix
		}
		out = append(out, sm)
	}
	return out
}

// accessLogAttributes turns the access-logs block into its attribute entries.
// The enabled flag is always sent so the block can switch logging off; bucket
// and prefix ride only when set. A nil block contributes nothing, leaving the
// load balancer's current logging untouched.
func accessLogAttributes(logs *LoadBalancerAccessLogs) []elbv2types.LoadBalancerAttribute {
	if logs == nil {
		return nil
	}
	attrs := []elbv2types.LoadBalancerAttribute{
		boolAttribute("access_logs.s3.enabled", logs.Enabled),
	}
	if logs.Bucket != nil {
		attrs = append(attrs, stringAttribute("access_logs.s3.bucket", logs.Bucket))
	}
	if logs.Prefix != nil {
		attrs = append(attrs, stringAttribute("access_logs.s3.prefix", logs.Prefix))
	}
	return attrs
}

// connectionLogAttributes turns the connection-logs block into its attribute
// entries, like the access-logs block but under the connection_logs.s3.* keys.
func connectionLogAttributes(
	logs *LoadBalancerConnectionLogs,
) []elbv2types.LoadBalancerAttribute {
	if logs == nil {
		return nil
	}
	attrs := []elbv2types.LoadBalancerAttribute{
		boolAttribute("connection_logs.s3.enabled", logs.Enabled),
	}
	if logs.Bucket != nil {
		attrs = append(attrs, stringAttribute("connection_logs.s3.bucket", logs.Bucket))
	}
	if logs.Prefix != nil {
		attrs = append(attrs, stringAttribute("connection_logs.s3.prefix", logs.Prefix))
	}
	return attrs
}

// boolAttribute builds one load balancer attribute from a *bool, formatting it
// as the lowercase "true"/"false" the API expects. A nil value formats as the
// API default of false, but the caller only adds an attribute it means to send.
func boolAttribute(key string, value *bool) elbv2types.LoadBalancerAttribute {
	return elbv2types.LoadBalancerAttribute{
		Key:   aws.String(key),
		Value: aws.String(strconv.FormatBool(aws.ToBool(value))),
	}
}

// stringAttribute builds one load balancer attribute from a *string.
func stringAttribute(key string, value *string) elbv2types.LoadBalancerAttribute {
	return elbv2types.LoadBalancerAttribute{
		Key:   aws.String(key),
		Value: value,
	}
}

// intAttribute builds one load balancer attribute from an *int64, formatting it
// as the decimal string the API expects.
func intAttribute(key string, value *int64) elbv2types.LoadBalancerAttribute {
	return elbv2types.LoadBalancerAttribute{
		Key:   aws.String(key),
		Value: aws.String(strconv.FormatInt(aws.ToInt64(value), 10)),
	}
}
