package elbv2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

// targetTypeLambda is the one target type that takes no port, protocol, or VPC
// and accepts only the lambda attribute, so the resource branches on it
// throughout.
const targetTypeLambda = "lambda"

// elbv2PropagationTimeout bounds the wait for a just-created target group to
// become consistently readable. CreateTargetGroup can return before a
// DescribeTargetGroups by the new ARN finds it, and the read-consistency
// window can run several minutes.
const elbv2PropagationTimeout = 5 * time.Minute

// TargetGroup manages an ELBv2 target group as one resource, the way
// CloudFormation models AWS::ElasticLoadBalancingV2::TargetGroup. The name,
// port, protocol, protocol version, VPC, target type, IP address type, and
// target control port are fixed at creation, so a change to any of them
// replaces the group; the health check, stickiness, the scalar attributes, and
// tags reconcile in place. CreateTargetGroup takes the create-time fields and
// the whole health check; the remaining attributes are reconciled by a
// follow-on ModifyTargetGroupAttributes, and a health-check change on update by
// ModifyTargetGroup. A nil health-check or stickiness block leaves ELBv2's
// defaults in place.
type TargetGroup struct {
	Name                           string                  `ub:"name"`
	TargetType                     *string                 `ub:"target-type"`
	Port                           *int64                  `ub:"port"`
	Protocol                       *string                 `ub:"protocol"`
	ProtocolVersion                *string                 `ub:"protocol-version"`
	VpcId                          *string                 `ub:"vpc-id"`
	IpAddressType                  *string                 `ub:"ip-address-type"`
	TargetControlPort              *int64                  `ub:"target-control-port"`
	HealthCheck                    *TargetGroupHealthCheck `ub:"health-check"`
	Stickiness                     *TargetGroupStickiness  `ub:"stickiness"`
	DeregistrationDelay            *int64                  `ub:"deregistration-delay"`
	SlowStart                      *int64                  `ub:"slow-start"`
	LoadBalancingAlgorithmType     *string                 `ub:"load-balancing-algorithm-type"`
	LoadBalancingCrossZoneEnabled  *string                 `ub:"load-balancing-cross-zone-enabled"`
	PreserveClientIp               *bool                   `ub:"preserve-client-ip"`
	ProxyProtocolV2                *bool                   `ub:"proxy-protocol-v2"`
	ConnectionTermination          *bool                   `ub:"connection-termination"`
	LambdaMultiValueHeadersEnabled *bool                   `ub:"lambda-multi-value-headers-enabled"`
	Tags                           map[string]string       `ub:"tags"`
}

// TargetGroupOutput holds the values ELBv2 computes for a target group. The ARN
// is the group's stable handle and CloudFormation primary identifier. The ARN
// suffix is the trailing identifier the CloudWatch metrics for the group are
// keyed by. The load balancer ARNs name the load balancers routing traffic to
// the group, which ELBv2 fills as listeners attach.
type TargetGroupOutput struct {
	Arn              string   `ub:"arn"`
	ArnSuffix        string   `ub:"arn-suffix"`
	LoadBalancerArns []string `ub:"load-balancer-arns"`
}

func (r *TargetGroup) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs ELBv2 fixes when a target group is created.
// The name is baked into the group's ARN, and the port, protocol, protocol
// version, VPC, target type, IP address type, and target control port cannot be
// changed on an existing group, so a change to any of them requires a new
// group. The health check, stickiness, scalar attributes, and tags reconcile in
// place.
func (r *TargetGroup) ReplaceFields() []string {
	return []string{
		"name",
		"port",
		"protocol",
		"protocol-version",
		"vpc-id",
		"target-type",
		"ip-address-type",
		"target-control-port",
	}
}

// Constraints declares the cross-field rules ELBv2 places on a target group's
// inputs. A lambda target takes no port, protocol, protocol version, or VPC,
// while every other target type requires a port, protocol, and VPC. The
// protocol version applies only to an HTTP or HTTPS group. The numeric ranges
// match ELBv2's accepted bounds.
//
// The health-check and stickiness blocks have their own enums and bounds,
// enforced in the resource code and documented on those types, since a nested
// block's fields do not derive unobin constraints.
func (r TargetGroup) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Equals(r.TargetType, "lambda")).
			Require(constraint.Absent(r.Port), constraint.Absent(r.Protocol),
				constraint.Absent(r.ProtocolVersion), constraint.Absent(r.VpcId)).
			Message("a lambda target group takes no port, protocol, protocol-version, or vpc-id"),
		constraint.When(constraint.NotEquals(r.TargetType, "lambda")).
			Require(constraint.Present(r.Port), constraint.Present(r.Protocol),
				constraint.Present(r.VpcId)).
			Message("a non-lambda target group requires port, protocol, and vpc-id"),
		constraint.When(constraint.Present(r.ProtocolVersion)).
			Require(constraint.OneOf(r.Protocol, "HTTP", "HTTPS")).
			Message("protocol-version applies only when protocol is HTTP or HTTPS"),
		constraint.When(constraint.Present(r.TargetType)).
			Require(constraint.OneOf(r.TargetType, "instance", "ip", "lambda", "alb")).
			Message("target-type must be instance, ip, lambda, or alb"),
		constraint.When(constraint.Present(r.IpAddressType)).
			Require(constraint.OneOf(r.IpAddressType, "ipv4", "ipv6")).
			Message("ip-address-type must be ipv4 or ipv6"),
		constraint.When(constraint.Present(r.LoadBalancingAlgorithmType)).
			Require(constraint.OneOf(r.LoadBalancingAlgorithmType,
				"round_robin", "least_outstanding_requests", "weighted_random")).
			Message("load-balancing-algorithm-type must be round_robin, " +
				"least_outstanding_requests, or weighted_random"),
		constraint.When(constraint.Present(r.LoadBalancingCrossZoneEnabled)).
			Require(constraint.OneOf(r.LoadBalancingCrossZoneEnabled,
				"true", "false", "use_load_balancer_configuration")).
			Message("load-balancing-cross-zone-enabled must be true, false, or " +
				"use_load_balancer_configuration"),
		constraint.When(constraint.Present(r.Port)).
			Require(constraint.AtLeast(r.Port, 1), constraint.AtMost(r.Port, 65535)).
			Message("port must be between 1 and 65535"),
		constraint.When(constraint.Present(r.TargetControlPort)).
			Require(constraint.AtLeast(r.TargetControlPort, 1),
				constraint.AtMost(r.TargetControlPort, 65535)).
			Message("target-control-port must be between 1 and 65535"),
		constraint.When(constraint.Present(r.DeregistrationDelay)).
			Require(constraint.AtLeast(r.DeregistrationDelay, 0),
				constraint.AtMost(r.DeregistrationDelay, 3600)).
			Message("deregistration-delay must be between 0 and 3600"),
		constraint.When(constraint.Present(r.SlowStart)).
			Require(constraint.AtLeast(r.SlowStart, 0), constraint.AtMost(r.SlowStart, 900)).
			Message("slow-start must be 0 or between 30 and 900"),
	}
}

func (r *TargetGroup) Create(ctx context.Context, cfg any) (*TargetGroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	in := r.createInput()
	// Some partitions (the ISO partitions) cannot tag a target group as it is
	// created, and some protocols (GENEVE) reject tag-on-create with a validation
	// error. When the tagged create fails for either reason, create the group
	// untagged and apply the tags with a separate call once it is visible.
	taggedSeparately := false
	arn, err := r.create(ctx, client, in)
	if err != nil && in.Tags != nil &&
		(partition.UnsupportedOperation(region(client), err) || isTagOnCreateUnsupported(err)) {
		in.Tags = nil
		taggedSeparately = true
		arn, err = r.create(ctx, client, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create target group: %w", err)
	}
	// CreateTargetGroup returns before a DescribeTargetGroups by the new ARN
	// finds it, so wait for the group to become readable before reconciling its
	// attributes and computing outputs.
	if err := r.waitVisible(ctx, client, arn); err != nil {
		return nil, err
	}
	if attrs := r.attributes(); len(attrs) > 0 {
		if err := r.modifyAttributes(ctx, client, arn, attrs); err != nil {
			return nil, err
		}
	}
	if taggedSeparately && len(r.Tags) > 0 {
		if err := syncTags(ctx, client, arn, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, arn)
}

func (r *TargetGroup) Read(
	ctx context.Context, cfg any, prior *TargetGroupOutput,
) (*TargetGroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Arn)
}

// read fetches the target group by ARN and pairs its describe with its
// attributes to compute the output. A group that has gone missing, an empty
// describe, or a describe that returns a different ARN than requested are all
// drift, mapped to runtime.ErrNotFound so the runtime recreates the group.
func (r *TargetGroup) read(
	ctx context.Context, client *elbv2.Client, arn string,
) (*TargetGroupOutput, error) {
	group, err := findTargetGroupByARN(ctx, client, arn)
	if err != nil {
		return nil, err
	}
	// The attributes are read to confirm the group is fully readable; a typed
	// not-found here is the same drift as a missing describe.
	_, err = client.DescribeTargetGroupAttributes(ctx,
		&elbv2.DescribeTargetGroupAttributesInput{TargetGroupArn: aws.String(arn)})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe target group attributes: %w", err)
	}
	return &TargetGroupOutput{
		Arn:              aws.ToString(group.TargetGroupArn),
		ArnSuffix:        targetGroupARNSuffix(aws.ToString(group.TargetGroupArn)),
		LoadBalancerArns: group.LoadBalancerArns,
	}, nil
}

func (r *TargetGroup) Update(
	ctx context.Context, cfg any, prior runtime.Prior[TargetGroup, *TargetGroupOutput],
) (*TargetGroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	// ModifyTargetGroup reconciles the health check, so it runs only when a
	// health-check field changed; ELBv2 has no separate call for it. A block
	// removed entirely is left alone, since ELBv2 has no way to clear a health
	// check, only to change its fields.
	if r.HealthCheck != nil && runtime.Changed(prior.Inputs.HealthCheck, r.HealthCheck) {
		if err := r.modifyHealthCheck(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	// ModifyTargetGroupAttributes reconciles the scalar attributes and the
	// stickiness block. It runs only on the attributes that changed; a removed
	// stickiness block is cleared by sending stickiness.enabled=false rather than
	// omitting it.
	if attrs := r.changedAttributes(prior.Inputs); len(attrs) > 0 {
		if err := r.modifyAttributes(ctx, client, arn, attrs); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, arn, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, arn)
}

func (r *TargetGroup) Delete(ctx context.Context, cfg any, prior *TargetGroupOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// A target group still attached to a listener or rule cannot be deleted; while
	// that dependency is torn down concurrently ELBv2 reports the group as in use,
	// so retry the delete through that window.
	err = retry.OnError(ctx, isResourceInUse, func(ctx context.Context) error {
		_, err := client.DeleteTargetGroup(ctx, &elbv2.DeleteTargetGroupInput{
			TargetGroupArn: aws.String(prior.Arn),
		})
		return err
	}, retry.WithTimeout(2*time.Minute))
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete target group: %w", err)
	}
	return nil
}

// validate enforces the cross-field rules on the nested health-check and
// stickiness blocks that those blocks' fields cannot express as unobin
// constraints: the health-check protocol must be one ELBv2 accepts, a TCP
// health check is rejected for an HTTP/HTTPS group or a lambda target, and a
// stickiness block must name its type.
func (r *TargetGroup) validate() error {
	if hc := r.HealthCheck; hc != nil && hc.Protocol != nil {
		switch upperProtocol(*hc.Protocol) {
		case "HTTP", "HTTPS", "TCP":
		default:
			return fmt.Errorf("health-check protocol must be HTTP, HTTPS, or TCP")
		}
		if hc.usesTCP() && (r.isHTTP() || r.isLambda()) {
			return fmt.Errorf("a TCP health check is not valid for an HTTP/HTTPS or lambda group")
		}
	}
	if r.Stickiness != nil && r.Stickiness.Type == nil {
		return fmt.Errorf("stickiness requires a type")
	}
	return nil
}

// createInput builds the CreateTargetGroup request from the create-time inputs
// and the health-check block. The port, protocol, protocol version, and VPC are
// left off for a lambda target, which does not accept them, even though the
// constraints already forbid setting them there.
func (r *TargetGroup) createInput() *elbv2.CreateTargetGroupInput {
	in := &elbv2.CreateTargetGroupInput{
		Name:              aws.String(r.Name),
		TargetControlPort: ptr.Int32(r.TargetControlPort),
		Tags:              tagList(r.Tags),
	}
	if r.TargetType != nil {
		in.TargetType = elbv2types.TargetTypeEnum(*r.TargetType)
	}
	if r.IpAddressType != nil {
		in.IpAddressType = elbv2types.TargetGroupIpAddressTypeEnum(*r.IpAddressType)
	}
	if !r.isLambda() {
		in.Port = ptr.Int32(r.Port)
		in.VpcId = r.VpcId
		if r.Protocol != nil {
			in.Protocol = elbv2types.ProtocolEnum(upperProtocol(*r.Protocol))
		}
		if r.ProtocolVersion != nil {
			in.ProtocolVersion = aws.String(upperProtocol(*r.ProtocolVersion))
		}
	}
	r.HealthCheck.applyToCreate(in, r.isGRPC())
	return in
}

// create issues CreateTargetGroup and returns the new group's ARN. It is the
// single create attempt the caller retries without tags on a tag-on-create
// failure.
func (r *TargetGroup) create(
	ctx context.Context, client *elbv2.Client, in *elbv2.CreateTargetGroupInput,
) (string, error) {
	resp, err := client.CreateTargetGroup(ctx, in)
	if err != nil {
		return "", err
	}
	if len(resp.TargetGroups) == 0 {
		return "", fmt.Errorf("create target group: empty response")
	}
	return aws.ToString(resp.TargetGroups[0].TargetGroupArn), nil
}

// modifyHealthCheck reconciles the health-check fields on an existing group
// with ModifyTargetGroup, the only call that updates them.
func (r *TargetGroup) modifyHealthCheck(
	ctx context.Context, client *elbv2.Client, arn string,
) error {
	in := &elbv2.ModifyTargetGroupInput{TargetGroupArn: aws.String(arn)}
	r.HealthCheck.applyToModify(in, r.isGRPC())
	if _, err := client.ModifyTargetGroup(ctx, in); err != nil {
		return fmt.Errorf("modify target group: %w", err)
	}
	return nil
}

// modifyAttributes applies the given target group attributes with
// ModifyTargetGroupAttributes.
func (r *TargetGroup) modifyAttributes(
	ctx context.Context, client *elbv2.Client, arn string,
	attrs []elbv2types.TargetGroupAttribute,
) error {
	_, err := client.ModifyTargetGroupAttributes(ctx,
		&elbv2.ModifyTargetGroupAttributesInput{
			TargetGroupArn: aws.String(arn),
			Attributes:     attrs,
		})
	if err != nil {
		return fmt.Errorf("modify target group attributes: %w", err)
	}
	return nil
}

// waitVisible polls DescribeTargetGroups by the new ARN until the just-created
// group is found, since CreateTargetGroup returns before the group is
// consistently readable. A not-found read means the create is still
// propagating, so the wait retries; the settled describe is read afterward
// through the resource's own Read.
func (r *TargetGroup) waitVisible(ctx context.Context, client *elbv2.Client, arn string) error {
	notYetVisible := func(err error) bool { return errors.Is(err, runtime.ErrNotFound) }
	err := retry.OnError(ctx, notYetVisible, func(ctx context.Context) error {
		_, err := findTargetGroupByARN(ctx, client, arn)
		return err
	}, retry.WithTimeout(elbv2PropagationTimeout), retry.WithInterval(time.Second))
	if err != nil {
		return fmt.Errorf("wait for target group %s: %w", r.Name, err)
	}
	return nil
}

// attributes builds the full attribute list to apply at create, including the
// stickiness block. Only attributes valid for the group's target type are
// emitted, so a lambda group sends only its lambda attribute and an instance or
// IP group sends the rest.
func (r *TargetGroup) attributes() []elbv2types.TargetGroupAttribute {
	var attrs []elbv2types.TargetGroupAttribute
	if r.isLambda() {
		if r.LambdaMultiValueHeadersEnabled != nil {
			attrs = append(attrs, attribute("lambda.multi_value_headers.enabled",
				boolValue(r.LambdaMultiValueHeadersEnabled)))
		}
		return attrs
	}
	// Only instance and IP target groups take the deregistration, slow-start,
	// load-balancing, and stickiness attributes; an ALB target group takes none
	// of them, so leave them off rather than have ELBv2 reject the modify.
	if !r.isInstanceOrIp() {
		return attrs
	}
	if r.DeregistrationDelay != nil {
		attrs = append(attrs, attribute("deregistration_delay.timeout_seconds",
			int64String(r.DeregistrationDelay)))
	}
	if r.SlowStart != nil {
		attrs = append(attrs, attribute("slow_start.duration_seconds",
			int64String(r.SlowStart)))
	}
	if r.LoadBalancingAlgorithmType != nil {
		attrs = append(attrs, attribute("load_balancing.algorithm.type",
			*r.LoadBalancingAlgorithmType))
	}
	if r.LoadBalancingCrossZoneEnabled != nil {
		attrs = append(attrs, attribute("load_balancing.cross_zone.enabled",
			*r.LoadBalancingCrossZoneEnabled))
	}
	if r.PreserveClientIp != nil {
		attrs = append(attrs, attribute("preserve_client_ip.enabled",
			boolValue(r.PreserveClientIp)))
	}
	if r.ProxyProtocolV2 != nil {
		attrs = append(attrs, attribute("proxy_protocol_v2.enabled",
			boolValue(r.ProxyProtocolV2)))
	}
	if r.ConnectionTermination != nil {
		attrs = append(attrs, attribute("deregistration_delay.connection_termination.enabled",
			boolValue(r.ConnectionTermination)))
	}
	attrs = append(attrs, r.Stickiness.attributes(r.isHTTP())...)
	return attrs
}

// changedAttributes builds the attribute list for an update, including only the
// attributes whose input changed from the prior. A removed stickiness block is
// cleared by sending stickiness.enabled=false, the empty sentinel, rather than
// leaving stickiness untouched.
func (r *TargetGroup) changedAttributes(prior TargetGroup) []elbv2types.TargetGroupAttribute {
	var attrs []elbv2types.TargetGroupAttribute
	if r.isLambda() {
		if runtime.Changed(prior.LambdaMultiValueHeadersEnabled,
			r.LambdaMultiValueHeadersEnabled) && r.LambdaMultiValueHeadersEnabled != nil {
			attrs = append(attrs, attribute("lambda.multi_value_headers.enabled",
				boolValue(r.LambdaMultiValueHeadersEnabled)))
		}
		return attrs
	}
	if !r.isInstanceOrIp() {
		return attrs
	}
	if runtime.Changed(prior.DeregistrationDelay, r.DeregistrationDelay) &&
		r.DeregistrationDelay != nil {
		attrs = append(attrs, attribute("deregistration_delay.timeout_seconds",
			int64String(r.DeregistrationDelay)))
	}
	if runtime.Changed(prior.SlowStart, r.SlowStart) && r.SlowStart != nil {
		attrs = append(attrs, attribute("slow_start.duration_seconds",
			int64String(r.SlowStart)))
	}
	if runtime.Changed(prior.LoadBalancingAlgorithmType, r.LoadBalancingAlgorithmType) &&
		r.LoadBalancingAlgorithmType != nil {
		attrs = append(attrs, attribute("load_balancing.algorithm.type",
			*r.LoadBalancingAlgorithmType))
	}
	if runtime.Changed(prior.LoadBalancingCrossZoneEnabled, r.LoadBalancingCrossZoneEnabled) &&
		r.LoadBalancingCrossZoneEnabled != nil {
		attrs = append(attrs, attribute("load_balancing.cross_zone.enabled",
			*r.LoadBalancingCrossZoneEnabled))
	}
	if runtime.Changed(prior.PreserveClientIp, r.PreserveClientIp) && r.PreserveClientIp != nil {
		attrs = append(attrs, attribute("preserve_client_ip.enabled",
			boolValue(r.PreserveClientIp)))
	}
	if runtime.Changed(prior.ProxyProtocolV2, r.ProxyProtocolV2) && r.ProxyProtocolV2 != nil {
		attrs = append(attrs, attribute("proxy_protocol_v2.enabled",
			boolValue(r.ProxyProtocolV2)))
	}
	if runtime.Changed(prior.ConnectionTermination, r.ConnectionTermination) &&
		r.ConnectionTermination != nil {
		attrs = append(attrs, attribute("deregistration_delay.connection_termination.enabled",
			boolValue(r.ConnectionTermination)))
	}
	attrs = append(attrs, r.changedStickiness(prior)...)
	return attrs
}

// changedStickiness returns the stickiness attributes to apply when the
// stickiness block changed. A block that was removed is cleared by disabling
// stickiness; otherwise the new block's attributes are emitted.
func (r *TargetGroup) changedStickiness(prior TargetGroup) []elbv2types.TargetGroupAttribute {
	if !runtime.Changed(prior.Stickiness, r.Stickiness) {
		return nil
	}
	if r.Stickiness == nil {
		return []elbv2types.TargetGroupAttribute{attribute("stickiness.enabled", "false")}
	}
	return r.Stickiness.attributes(r.isHTTP())
}

// isLambda reports whether the target type is lambda, which takes no port,
// protocol, or VPC and only the lambda attribute.
func (r *TargetGroup) isLambda() bool {
	return r.TargetType != nil && *r.TargetType == targetTypeLambda
}

// isInstanceOrIp reports whether the target type is instance or IP, the two
// types that take the deregistration, slow-start, load-balancing, and
// stickiness attributes. An unset target type defaults to instance.
func (r *TargetGroup) isInstanceOrIp() bool {
	if r.TargetType == nil {
		return true
	}
	switch *r.TargetType {
	case "instance", "ip":
		return true
	default:
		return false
	}
}

// isHTTP reports whether the group's protocol is HTTP or HTTPS, the only
// protocols for which the cookie-based stickiness attributes apply.
func (r *TargetGroup) isHTTP() bool {
	if r.Protocol == nil {
		return false
	}
	switch upperProtocol(*r.Protocol) {
	case "HTTP", "HTTPS":
		return true
	default:
		return false
	}
}

// isGRPC reports whether the group's protocol version is gRPC, which decides
// whether a health-check matcher holds a gRPC code or an HTTP code.
func (r *TargetGroup) isGRPC() bool {
	return r.ProtocolVersion != nil && upperProtocol(*r.ProtocolVersion) == "GRPC"
}

// findTargetGroupByARN reads a single target group by ARN through the paginated
// DescribeTargetGroups. A typed not-found, an empty result, or a returned ARN
// that differs from the one requested are all drift, mapped to
// runtime.ErrNotFound; the ARN guard catches a stale read against an
// eventually-consistent replica.
func findTargetGroupByARN(
	ctx context.Context, client *elbv2.Client, arn string,
) (*elbv2types.TargetGroup, error) {
	pager := elbv2.NewDescribeTargetGroupsPaginator(client,
		&elbv2.DescribeTargetGroupsInput{TargetGroupArns: []string{arn}})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe target groups: %w", err)
		}
		for i := range page.TargetGroups {
			group := page.TargetGroups[i]
			if aws.ToString(group.TargetGroupArn) == arn {
				return &group, nil
			}
		}
	}
	return nil, runtime.ErrNotFound
}

// targetGroupARNSuffix derives the ARN suffix the CloudWatch metrics for a
// group are keyed by, the part of the ARN from targetgroup/ onward. An ARN that
// does not contain that marker yields an empty suffix rather than an error.
func targetGroupARNSuffix(arn string) string {
	const marker = ":targetgroup/"
	if i := strings.Index(arn, marker); i >= 0 {
		return arn[i+1:]
	}
	return ""
}

// upperProtocol upper-cases an ELBv2 protocol or protocol-version value, which
// ELBv2 accepts in any case and stores upper-cased, so the request matches what
// a later read returns and the protocol checks compare against the canonical
// form.
func upperProtocol(s string) string {
	return strings.ToUpper(s)
}

// boolValue renders a bool pointer as the "true" or "false" string an ELBv2
// attribute value takes. A nil pointer reads as false, the default for the
// boolean attributes.
func boolValue(b *bool) string {
	if aws.ToBool(b) {
		return "true"
	}
	return "false"
}

// int64String renders an int64 pointer as the decimal string an ELBv2 numeric
// attribute value takes. A nil pointer reads as "0".
func int64String(n *int64) string {
	return fmt.Sprintf("%d", aws.ToInt64(n))
}
