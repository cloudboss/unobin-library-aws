package elbv2

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// loadBalancerTypeApplication is the load balancer type AWS assumes when none is
// given. It decides which follow-on attributes apply, so the resource resolves
// the type to this default before gating any attribute by it.
const loadBalancerTypeApplication = "application"

// loadBalancerActiveTimeout bounds the wait for a load balancer to leave the
// provisioning state. A load balancer takes a few minutes to come up, longer
// than the shared wait default, so the active wait is given ten minutes.
const loadBalancerActiveTimeout = 10 * time.Minute

// unsupportedAttributeKey matches the validation message ELBv2 returns when an
// attribute key does not apply to the load balancer's type or its partition. The
// message names the key in single quotes and ends "is not recognized" or "is not
// supported", so the offending key is removed from the batch and the modify is
// retried without it.
var unsupportedAttributeKey = regexp.MustCompile(
	`attribute key '([^']+)' is not (recognized|supported)`)

// LoadBalancer manages an Elastic Load Balancing v2 load balancer, the way
// CloudFormation models AWS::ElasticLoadBalancingV2::LoadBalancer. The name,
// scheme (internal vs internet-facing), and type are fixed at creation, as is
// the Outposts customer-owned address pool, so a change to any of them replaces
// the load balancer; everything else reconciles in place. CreateLoadBalancer
// takes the name, type, scheme, IP address type, subnets or subnet mappings,
// security groups, and tags; the remaining settings are attributes applied by a
// follow-on ModifyLoadBalancerAttributes, with the subnets, security groups, and
// IP address type reconciled on update by SetSubnets, SetSecurityGroups, and
// SetIpAddressType. The access-logs and connection-logs blocks fold into the
// attribute list. Each attribute applies only to certain load balancer types,
// so the resource sends an attribute only for its supported types.
//
// AWS enforces the name's own bounds, so they are not expressed as constraints:
// the name is at most 32 characters matching ^[0-9A-Za-z-]+$, must not begin or
// end with a hyphen, and must not begin with "internal-".
type LoadBalancer struct {
	Name                  string                      `ub:"name"`
	LoadBalancerType      *string                     `ub:"load-balancer-type"`
	Internal              *bool                       `ub:"internal"`
	IpAddressType         *string                     `ub:"ip-address-type"`
	CustomerOwnedIpv4Pool *string                     `ub:"customer-owned-ipv4-pool"`
	SecurityGroups        []string                    `ub:"security-groups"`
	Subnets               []string                    `ub:"subnets"`
	SubnetMappings        []LoadBalancerSubnetMapping `ub:"subnet-mappings"`
	AccessLogs            *LoadBalancerAccessLogs     `ub:"access-logs"`
	ConnectionLogs        *LoadBalancerConnectionLogs `ub:"connection-logs"`
	Tags                  map[string]string           `ub:"tags"`

	IdleTimeout                           *int64  `ub:"idle-timeout"`
	EnableDeletionProtection              *bool   `ub:"enable-deletion-protection"`
	EnableHttp2                           *bool   `ub:"enable-http2"`
	EnableCrossZoneLoadBalancing          *bool   `ub:"enable-cross-zone-load-balancing"`
	DesyncMitigationMode                  *string `ub:"desync-mitigation-mode"`
	DropInvalidHeaderFields               *bool   `ub:"drop-invalid-header-fields"`
	PreserveHostHeader                    *bool   `ub:"preserve-host-header"`
	EnableXffClientPort                   *bool   `ub:"enable-xff-client-port"`
	XffHeaderProcessingMode               *string `ub:"xff-header-processing-mode"`
	ClientKeepAlive                       *int64  `ub:"client-keep-alive"`
	EnableTlsVersionAndCipherSuiteHeaders *bool   `ub:"enable-tls-version-and-cipher-suite-headers"`
	DnsRecordClientRoutingPolicy          *string `ub:"dns-record-client-routing-policy"`
}

// LoadBalancerOutput holds the values ELBv2 computes for a load balancer once it
// is active. The ARN is its stable handle, used to read, modify, and delete it;
// the DNS name and canonical hosted zone id are what listeners and Route 53
// aliases point at. The ARN suffix is the form a CloudWatch metric dimension
// takes. The VPC id, IP address type, name, and scheme are filled by AWS, so
// they are reported rather than echoed from the inputs.
type LoadBalancerOutput struct {
	Arn                   string `ub:"arn"`
	DNSName               string `ub:"dns-name"`
	CanonicalHostedZoneId string `ub:"canonical-hosted-zone-id"`
	ArnSuffix             string `ub:"arn-suffix"`
	VpcId                 string `ub:"vpc-id"`
	IpAddressType         string `ub:"ip-address-type"`
	Name                  string `ub:"name"`
	Scheme                string `ub:"scheme"`
}

func (r *LoadBalancer) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs ELBv2 fixes when a load balancer is created.
// The name is baked into the ARN, the scheme and type cannot be changed on an
// existing load balancer, and the Outposts customer-owned address pool is set
// once, so a change to any of them requires a new load balancer. The subnets,
// security groups, and IP address type are reconciled in place by Update rather
// than replaced, which is legal for Application and Gateway load balancers; a
// change ELBv2 forbids in place on a Network Load Balancer comes back as an API
// error.
func (r *LoadBalancer) ReplaceFields() []string {
	return []string{
		"name",
		"internal",
		"load-balancer-type",
		"customer-owned-ipv4-pool",
	}
}

// Constraints declares the rules ELBv2 places on a load balancer's inputs. A
// load balancer's subnets are given as either a plain subnet list or a list of
// subnet mappings, never both. The type and the several enum-valued attributes
// each accept a fixed set of values; an unset one lets ELBv2 apply its own
// default. Enabled access or connection logs need a bucket. The remaining
// bounds, such as the idle timeout range and the per-type applicability of
// each attribute, are enforced by the API and in code.
func (r LoadBalancer) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.Subnets, r.SubnetMappings),
		constraint.When(constraint.Present(r.LoadBalancerType)).
			Require(constraint.OneOf(r.LoadBalancerType, "application", "network", "gateway")).
			Message("load-balancer-type must be application, network, or gateway"),
		constraint.When(constraint.Present(r.IpAddressType)).
			Require(constraint.OneOf(r.IpAddressType,
				"ipv4", "dualstack", "dualstack-without-public-ipv4")).
			Message("ip-address-type must be ipv4, dualstack, or dualstack-without-public-ipv4"),
		constraint.When(constraint.Present(r.DesyncMitigationMode)).
			Require(constraint.OneOf(r.DesyncMitigationMode, "monitor", "defensive", "strictest")).
			Message("desync-mitigation-mode must be monitor, defensive, or strictest"),
		constraint.When(constraint.Present(r.XffHeaderProcessingMode)).
			Require(constraint.OneOf(r.XffHeaderProcessingMode, "append", "preserve", "remove")).
			Message("xff-header-processing-mode must be append, preserve, or remove"),
		constraint.When(constraint.Present(r.DnsRecordClientRoutingPolicy)).
			Require(constraint.OneOf(r.DnsRecordClientRoutingPolicy,
				"availability_zone_affinity",
				"partial_availability_zone_affinity",
				"any_availability_zone")).
			Message("dns-record-client-routing-policy must be a valid routing policy"),
		constraint.When(constraint.IsTrue(r.AccessLogs.Enabled)).
			Require(constraint.Present(r.AccessLogs.Bucket)).
			Message("enabled access-logs require a bucket"),
		constraint.When(constraint.IsTrue(r.ConnectionLogs.Enabled)).
			Require(constraint.Present(r.ConnectionLogs.Bucket)).
			Message("enabled connection-logs require a bucket"),
	}
}

func (r *LoadBalancer) Create(ctx context.Context, cfg any) (*LoadBalancerOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := r.createInput()
	// Some partitions, such as the ISO partitions, cannot tag a load balancer as
	// it is created. When the tagged create fails for that reason, create it
	// without tags and apply them with a separate call once it is active.
	taggedSeparately := false
	resp, err := client.CreateLoadBalancer(ctx, in)
	if err != nil && in.Tags != nil &&
		(partition.UnsupportedOperation(region(client), err) || isTagOnCreateUnsupported(err)) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = client.CreateLoadBalancer(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create load balancer: %w", err)
	}
	if len(resp.LoadBalancers) == 0 {
		return nil, fmt.Errorf("create load balancer: empty response")
	}
	arn := aws.ToString(resp.LoadBalancers[0].LoadBalancerArn)
	// CreateLoadBalancer returns while the load balancer is still provisioning,
	// so wait for it to become active before applying attributes or reading the
	// settled DNS name and hosted-zone id.
	if err := r.waitActive(ctx, client, arn); err != nil {
		return nil, err
	}
	if taggedSeparately && len(r.Tags) > 0 {
		if err := syncTags(ctx, client, arn, r.Tags); err != nil {
			return nil, err
		}
	}
	// The bool, int, string, and log-block settings are applied by a separate
	// ModifyLoadBalancerAttributes, gated by load balancer type. When any apply,
	// wait active again before reading, since modifying attributes can return the
	// load balancer to provisioning.
	attrs := r.attributes()
	if len(attrs) > 0 {
		if err := r.modifyAttributes(ctx, client, arn, attrs); err != nil {
			return nil, err
		}
		if err := r.waitActive(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, arn)
}

func (r *LoadBalancer) Read(
	ctx context.Context, cfg any, prior *LoadBalancerOutput,
) (*LoadBalancerOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Arn)
}

// read describes the load balancer by ARN and returns its computed outputs. A
// load balancer that has gone missing is drift, which DescribeLoadBalancers
// reports as the typed not-found exception; an empty result or an ARN mismatch
// also means it is gone, the eventual-consistency guard for a just-created
// load balancer whose describe has not caught up. All three map to
// runtime.ErrNotFound so the runtime recreates it.
func (r *LoadBalancer) read(
	ctx context.Context, client *elbv2.Client, arn string,
) (*LoadBalancerOutput, error) {
	resp, err := client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{
		LoadBalancerArns: []string{arn},
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe load balancer: %w", err)
	}
	if len(resp.LoadBalancers) == 0 {
		return nil, runtime.ErrNotFound
	}
	lb := resp.LoadBalancers[0]
	if aws.ToString(lb.LoadBalancerArn) != arn {
		return nil, runtime.ErrNotFound
	}
	return loadBalancerOutput(lb), nil
}

func (r *LoadBalancer) Update(
	ctx context.Context, cfg any, prior runtime.Prior[LoadBalancer, *LoadBalancerOutput],
) (*LoadBalancerOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	// Each follow-on call runs only when an input it reconciles changed, so the
	// same config applied twice makes no write ELBv2 does not need.
	if r.attributesChanged(prior.Inputs) {
		attrs := r.attributes()
		if len(attrs) > 0 {
			if err := r.modifyAttributes(ctx, client, arn, attrs); err != nil {
				return nil, err
			}
		}
	}
	if runtime.Changed(prior.Inputs.SecurityGroups, r.SecurityGroups) {
		if err := r.setSecurityGroups(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Subnets, r.Subnets) ||
		runtime.Changed(prior.Inputs.SubnetMappings, r.SubnetMappings) {
		if err := r.setSubnets(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.IpAddressType, r.IpAddressType) {
		if err := r.setIpAddressType(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, arn, r.Tags); err != nil {
			return nil, err
		}
	}
	// Several of the calls above can return the load balancer to provisioning,
	// so wait for it to settle before reading its outputs.
	if err := r.waitActive(ctx, client, arn); err != nil {
		return nil, err
	}
	return r.read(ctx, client, arn)
}

func (r *LoadBalancer) Delete(ctx context.Context, cfg any, prior *LoadBalancerOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// DeleteLoadBalancer is the whole delete. ELBv2 detaches the load balancer's
	// network interfaces on its own afterward; waiting for those ENIs to clear is
	// a best-effort EC2-side concern with no bearing on whether the delete
	// succeeded, so it is left out.
	_, err = client.DeleteLoadBalancer(ctx, &elbv2.DeleteLoadBalancerInput{
		LoadBalancerArn: aws.String(prior.Arn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete load balancer: %w", err)
	}
	return nil
}

// createInput builds the CreateLoadBalancer request from the create-time fields.
// The scheme is internal when the input asks for it and internet-facing
// otherwise, matching AWS's default. The type, IP address type, subnets or
// subnet mappings, security groups, customer-owned pool, and tags ride only when
// set so an omitted one takes AWS's default.
func (r *LoadBalancer) createInput() *elbv2.CreateLoadBalancerInput {
	in := &elbv2.CreateLoadBalancerInput{
		Name:                  aws.String(r.Name),
		CustomerOwnedIpv4Pool: r.CustomerOwnedIpv4Pool,
		SecurityGroups:        r.SecurityGroups,
		Subnets:               r.Subnets,
		SubnetMappings:        subnetMappings(r.SubnetMappings),
		Tags:                  tagList(r.Tags),
	}
	if r.LoadBalancerType != nil {
		in.Type = elbv2types.LoadBalancerTypeEnum(*r.LoadBalancerType)
	}
	if r.Internal != nil {
		if *r.Internal {
			in.Scheme = elbv2types.LoadBalancerSchemeEnumInternal
		} else {
			in.Scheme = elbv2types.LoadBalancerSchemeEnumInternetFacing
		}
	}
	if r.IpAddressType != nil {
		in.IpAddressType = elbv2types.IpAddressType(*r.IpAddressType)
	}
	return in
}

// resolvedType returns the load balancer type, applying AWS's default of
// application when none is set. Attribute applicability turns on the type, so it
// is resolved once here.
func (r *LoadBalancer) resolvedType() string {
	if r.LoadBalancerType == nil {
		return loadBalancerTypeApplication
	}
	return *r.LoadBalancerType
}

// attributes assembles the ModifyLoadBalancerAttributes list from the
// attribute fields and the log blocks, sending each only for the load balancer
// types it applies to. Deletion protection applies to every type. Cross-zone
// load balancing is sent only for Network and Gateway load balancers, since for
// an Application Load Balancer it is always on and cannot be changed, so it is
// never sent there. Access logs apply to Application and Network load balancers;
// the connection-logs block and the rest of the HTTP-routing attributes apply to
// Application load balancers only; the DNS routing policy applies to Network load
// balancers only. An attribute is added only when its field is set, so an
// omitted field leaves ELBv2's current value alone.
func (r *LoadBalancer) attributes() []elbv2types.LoadBalancerAttribute {
	lbType := r.resolvedType()
	application := lbType == "application"
	network := lbType == "network"
	gateway := lbType == "gateway"

	var attrs []elbv2types.LoadBalancerAttribute
	add := func(applies bool, attr elbv2types.LoadBalancerAttribute) {
		if applies {
			attrs = append(attrs, attr)
		}
	}
	addBool := func(applies bool, key string, value *bool) {
		if applies && value != nil {
			attrs = append(attrs, boolAttribute(key, value))
		}
	}
	addInt := func(applies bool, key string, value *int64) {
		if applies && value != nil {
			attrs = append(attrs, intAttribute(key, value))
		}
	}
	addString := func(applies bool, key string, value *string) {
		if applies && value != nil {
			attrs = append(attrs, stringAttribute(key, value))
		}
	}

	addBool(true, "deletion_protection.enabled", r.EnableDeletionProtection)
	addBool(network || gateway,
		"load_balancing.cross_zone.enabled", r.EnableCrossZoneLoadBalancing)

	addInt(application, "idle_timeout.timeout_seconds", r.IdleTimeout)
	addInt(application, "client_keep_alive.seconds", r.ClientKeepAlive)
	addBool(application, "routing.http2.enabled", r.EnableHttp2)
	addString(application, "routing.http.desync_mitigation_mode", r.DesyncMitigationMode)
	addBool(application,
		"routing.http.drop_invalid_header_fields.enabled", r.DropInvalidHeaderFields)
	addBool(application, "routing.http.preserve_host_header.enabled", r.PreserveHostHeader)
	addBool(application, "routing.http.xff_client_port.enabled", r.EnableXffClientPort)
	addString(application,
		"routing.http.xff_header_processing.mode", r.XffHeaderProcessingMode)
	addBool(application, "routing.http.x_amzn_tls_version_and_cipher_suite.enabled",
		r.EnableTlsVersionAndCipherSuiteHeaders)

	addString(network, "dns_record.client_routing_policy", r.DnsRecordClientRoutingPolicy)

	if application || network {
		for _, attr := range accessLogAttributes(r.AccessLogs) {
			add(true, attr)
		}
	}
	if application {
		for _, attr := range connectionLogAttributes(r.ConnectionLogs) {
			add(true, attr)
		}
	}
	return attrs
}

// attributesChanged reports whether any field the attribute list includes
// differs from the prior inputs, so the modify runs on update only when it has
// work to do. The log blocks are compared by value, so a changed bucket or
// toggle inside one counts.
func (r *LoadBalancer) attributesChanged(prior LoadBalancer) bool {
	return runtime.Changed(prior.IdleTimeout, r.IdleTimeout) ||
		runtime.Changed(prior.EnableDeletionProtection, r.EnableDeletionProtection) ||
		runtime.Changed(prior.EnableHttp2, r.EnableHttp2) ||
		runtime.Changed(prior.EnableCrossZoneLoadBalancing, r.EnableCrossZoneLoadBalancing) ||
		runtime.Changed(prior.DesyncMitigationMode, r.DesyncMitigationMode) ||
		runtime.Changed(prior.DropInvalidHeaderFields, r.DropInvalidHeaderFields) ||
		runtime.Changed(prior.PreserveHostHeader, r.PreserveHostHeader) ||
		runtime.Changed(prior.EnableXffClientPort, r.EnableXffClientPort) ||
		runtime.Changed(prior.XffHeaderProcessingMode, r.XffHeaderProcessingMode) ||
		runtime.Changed(prior.ClientKeepAlive, r.ClientKeepAlive) ||
		runtime.Changed(prior.EnableTlsVersionAndCipherSuiteHeaders,
			r.EnableTlsVersionAndCipherSuiteHeaders) ||
		runtime.Changed(prior.DnsRecordClientRoutingPolicy, r.DnsRecordClientRoutingPolicy) ||
		runtime.Changed(prior.AccessLogs, r.AccessLogs) ||
		runtime.Changed(prior.ConnectionLogs, r.ConnectionLogs)
}

// modifyAttributes sends the attribute batch, tolerating an attribute ELBv2
// rejects as not applying to the load balancer's type or partition. On such a
// rejection it removes the named key and retries, until the batch succeeds or
// every offending key has been removed. Any other error stops it.
func (r *LoadBalancer) modifyAttributes(
	ctx context.Context, client *elbv2.Client, arn string,
	attrs []elbv2types.LoadBalancerAttribute,
) error {
	for len(attrs) > 0 {
		_, err := client.ModifyLoadBalancerAttributes(ctx,
			&elbv2.ModifyLoadBalancerAttributesInput{
				LoadBalancerArn: aws.String(arn),
				Attributes:      attrs,
			})
		if err == nil {
			return nil
		}
		key, ok := unsupportedAttribute(err)
		if !ok {
			return fmt.Errorf("modify load balancer attributes: %w", err)
		}
		attrs = removeAttribute(attrs, key)
	}
	return nil
}

// setSubnets reconciles the load balancer's subnets, sending the IP address
// type alongside so a subnet change that also moves between IP address types is
// applied in one call. Subnets are given as a plain list or as subnet mappings,
// whichever the input uses.
func (r *LoadBalancer) setSubnets(ctx context.Context, client *elbv2.Client, arn string) error {
	in := &elbv2.SetSubnetsInput{
		LoadBalancerArn: aws.String(arn),
		Subnets:         r.Subnets,
		SubnetMappings:  subnetMappings(r.SubnetMappings),
	}
	if r.IpAddressType != nil {
		in.IpAddressType = elbv2types.IpAddressType(*r.IpAddressType)
	}
	if _, err := client.SetSubnets(ctx, in); err != nil {
		return fmt.Errorf("set subnets: %w", err)
	}
	return nil
}

// setSecurityGroups reconciles the load balancer's security groups.
func (r *LoadBalancer) setSecurityGroups(
	ctx context.Context, client *elbv2.Client, arn string,
) error {
	_, err := client.SetSecurityGroups(ctx, &elbv2.SetSecurityGroupsInput{
		LoadBalancerArn: aws.String(arn),
		SecurityGroups:  r.SecurityGroups,
	})
	if err != nil {
		return fmt.Errorf("set security groups: %w", err)
	}
	return nil
}

// setIpAddressType reconciles the load balancer's IP address type.
func (r *LoadBalancer) setIpAddressType(
	ctx context.Context, client *elbv2.Client, arn string,
) error {
	if r.IpAddressType == nil {
		return nil
	}
	_, err := client.SetIpAddressType(ctx, &elbv2.SetIpAddressTypeInput{
		LoadBalancerArn: aws.String(arn),
		IpAddressType:   elbv2types.IpAddressType(*r.IpAddressType),
	})
	if err != nil {
		return fmt.Errorf("set ip address type: %w", err)
	}
	return nil
}

// waitActive polls DescribeLoadBalancers until the load balancer leaves the
// provisioning state for active. A load balancer that settles into the failed
// state will never become active, so the wait stops at once and reports the
// failure reason ELBv2 gives.
func (r *LoadBalancer) waitActive(ctx context.Context, client *elbv2.Client, arn string) error {
	what := fmt.Sprintf("load balancer %s", r.Name)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		resp, err := client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{
			LoadBalancerArns: []string{arn},
		})
		if err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("describe load balancer: %w", err)
		}
		if len(resp.LoadBalancers) == 0 || resp.LoadBalancers[0].State == nil {
			return false, nil
		}
		state := resp.LoadBalancers[0].State
		switch state.Code {
		case elbv2types.LoadBalancerStateEnumActive:
			return true, nil
		case elbv2types.LoadBalancerStateEnumFailed:
			return false, fmt.Errorf("load balancer %s failed to provision: %s",
				r.Name, aws.ToString(state.Reason))
		default:
			return false, nil
		}
	}, wait.WithTimeout(loadBalancerActiveTimeout))
}

// loadBalancerOutput maps a described load balancer to its computed outputs.
func loadBalancerOutput(lb elbv2types.LoadBalancer) *LoadBalancerOutput {
	arn := aws.ToString(lb.LoadBalancerArn)
	return &LoadBalancerOutput{
		Arn:                   arn,
		DNSName:               aws.ToString(lb.DNSName),
		CanonicalHostedZoneId: aws.ToString(lb.CanonicalHostedZoneId),
		ArnSuffix:             arnSuffix(arn),
		VpcId:                 aws.ToString(lb.VpcId),
		IpAddressType:         string(lb.IpAddressType),
		Name:                  aws.ToString(lb.LoadBalancerName),
		Scheme:                string(lb.Scheme),
	}
}

// arnSuffix returns the load balancer's full name, the trailing portion of its
// ARN that names it in a CloudWatch metric dimension. A load balancer ARN ends
// in loadbalancer/<app|net|gwy>/<name>/<id>, and the suffix is everything after
// the loadbalancer/ segment. An ARN that does not contain that segment yields an
// empty suffix rather than a partial one.
func arnSuffix(arn string) string {
	_, suffix, ok := strings.Cut(arn, "loadbalancer/")
	if !ok {
		return ""
	}
	return suffix
}

// unsupportedAttribute reports whether err is ELBv2 rejecting an attribute that
// does not apply to the load balancer's type or partition, returning the key it
// named. ELBv2 gives no typed exception for this, so the match is on the
// validation message.
func unsupportedAttribute(err error) (string, bool) {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return "", false
	}
	m := unsupportedAttributeKey.FindStringSubmatch(apiErr.ErrorMessage())
	if m == nil {
		return "", false
	}
	return m[1], true
}

// removeAttribute returns the attribute list with the entry for key removed,
// used when ELBv2 rejects that key so the batch can be retried without it.
func removeAttribute(
	attrs []elbv2types.LoadBalancerAttribute, key string,
) []elbv2types.LoadBalancerAttribute {
	out := make([]elbv2types.LoadBalancerAttribute, 0, len(attrs))
	for _, a := range attrs {
		if aws.ToString(a.Key) == key {
			continue
		}
		out = append(out, a)
	}
	return out
}
