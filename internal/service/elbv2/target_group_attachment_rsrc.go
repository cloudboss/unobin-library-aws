package elbv2

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

var (
	validARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	validARNRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	validARNAccountPattern   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`)
)

// TargetGroupAttachment registers one target in an ELBv2 target group. The
// target group ARN and target id name the required target tuple, and the
// optional availability-zone, port, and QUIC server id participate in that
// tuple only when non-empty or non-zero. ELBv2 exposes this as RegisterTargets
// and DeregisterTargets, with DescribeTargetHealth confirming presence; every
// input is replace-only.
type TargetGroupAttachment struct {
	TargetGroupArn   string  `ub:"target-group-arn"`
	TargetId         string  `ub:"target-id"`
	AvailabilityZone *string `ub:"availability-zone"`
	Port             *int64  `ub:"port"`
	QuicServerId     *string `ub:"quic-server-id"`
}

// TargetGroupAttachmentOutput stores the registered target tuple. Read and
// Delete use it as the old handle during replacement, so optional fields remain
// absent unless the original registration sent a non-empty or non-zero value.
type TargetGroupAttachmentOutput struct {
	TargetGroupArn   string  `ub:"target-group-arn"`
	TargetId         string  `ub:"target-id"`
	AvailabilityZone *string `ub:"availability-zone"`
	Port             *int64  `ub:"port"`
	QuicServerId     *string `ub:"quic-server-id"`
}

func (r *TargetGroupAttachment) SchemaVersion() int { return 1 }

// ReplaceFields lists the full target tuple. ELBv2 cannot update any member of
// a target registration in place; changing one member deregisters the old tuple
// and registers a new one.
func (r *TargetGroupAttachment) ReplaceFields() []string {
	return []string{
		"target-group-arn",
		"target-id",
		"availability-zone",
		"port",
		"quic-server-id",
	}
}

func (r *TargetGroupAttachment) Create(
	ctx context.Context, cfg *awsCfg,
) (*TargetGroupAttachmentOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	tuple := r.effectiveTuple()
	err = retry.OnError(ctx, isInvalidTarget, func(ctx context.Context) error {
		_, err := client.RegisterTargets(ctx, tuple.registerInput())
		return err
	}, retry.WithTimeout(10*time.Minute))
	if err != nil {
		return nil, fmt.Errorf("register target: %w", err)
	}
	return &tuple, nil
}

func (r *TargetGroupAttachment) Read(
	ctx context.Context, cfg *awsCfg, prior *TargetGroupAttachmentOutput,
) (*TargetGroupAttachmentOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	tuple := r.tupleWithFallback(prior)
	resp, err := client.DescribeTargetHealth(ctx, tuple.describeInput())
	if err != nil {
		if isInvalidTarget(err) || isTargetGroupNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe target health: %w", err)
	}
	if _, err := selectedTargetHealthDescription(resp); err != nil {
		return nil, err
	}
	return &tuple, nil
}

// Update has no work to do. Every field is replace-only, so a real input change
// bypasses Update and recreates the registration.
func (r *TargetGroupAttachment) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[TargetGroupAttachment, *TargetGroupAttachmentOutput],
) (*TargetGroupAttachmentOutput, error) {
	tuple := r.tupleWithFallback(prior.Outputs)
	return &tuple, nil
}

func (r *TargetGroupAttachment) Delete(
	ctx context.Context, cfg *awsCfg, prior *TargetGroupAttachmentOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	tuple := r.tupleWithFallback(prior)
	_, err = client.DeregisterTargets(ctx, tuple.deregisterInput())
	if err != nil {
		if isLoadBalancerNotFound(err) {
			return nil
		}
		return fmt.Errorf("deregister target: %w", err)
	}
	return nil
}

func (r *TargetGroupAttachment) effectiveTuple() TargetGroupAttachmentOutput {
	return TargetGroupAttachmentOutput{
		TargetGroupArn:   r.TargetGroupArn,
		TargetId:         r.TargetId,
		AvailabilityZone: effectiveString(r.AvailabilityZone),
		Port:             effectiveInt64(r.Port),
		QuicServerId:     effectiveString(r.QuicServerId),
	}
}

func (r *TargetGroupAttachment) tupleWithFallback(
	prior *TargetGroupAttachmentOutput,
) TargetGroupAttachmentOutput {
	if prior != nil && prior.usable() {
		return prior.effectiveTuple()
	}
	return r.effectiveTuple()
}

func (o TargetGroupAttachmentOutput) usable() bool {
	return o.TargetGroupArn != "" && o.TargetId != ""
}

func (o TargetGroupAttachmentOutput) effectiveTuple() TargetGroupAttachmentOutput {
	return TargetGroupAttachmentOutput{
		TargetGroupArn:   o.TargetGroupArn,
		TargetId:         o.TargetId,
		AvailabilityZone: effectiveString(o.AvailabilityZone),
		Port:             effectiveInt64(o.Port),
		QuicServerId:     effectiveString(o.QuicServerId),
	}
}

func (o TargetGroupAttachmentOutput) registerInput() *elbv2.RegisterTargetsInput {
	return &elbv2.RegisterTargetsInput{
		TargetGroupArn: aws.String(o.TargetGroupArn),
		Targets:        []elbv2types.TargetDescription{o.targetDescription()},
	}
}

func (o TargetGroupAttachmentOutput) describeInput() *elbv2.DescribeTargetHealthInput {
	return &elbv2.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(o.TargetGroupArn),
		Targets:        []elbv2types.TargetDescription{o.targetDescription()},
	}
}

func (o TargetGroupAttachmentOutput) deregisterInput() *elbv2.DeregisterTargetsInput {
	return &elbv2.DeregisterTargetsInput{
		TargetGroupArn: aws.String(o.TargetGroupArn),
		Targets:        []elbv2types.TargetDescription{o.targetDescription()},
	}
}

func (o TargetGroupAttachmentOutput) targetDescription() elbv2types.TargetDescription {
	tuple := o.effectiveTuple()
	return elbv2types.TargetDescription{
		Id:               aws.String(tuple.TargetId),
		AvailabilityZone: tuple.AvailabilityZone,
		Port:             ptr.Int32(tuple.Port),
		QuicServerId:     tuple.QuicServerId,
	}
}

func effectiveString(v *string) *string {
	if v == nil || *v == "" {
		return nil
	}
	return aws.String(*v)
}

func effectiveInt64(v *int64) *int64 {
	if v == nil || *v == 0 {
		return nil
	}
	return aws.Int64(*v)
}

func (r *TargetGroupAttachment) validate() error {
	if !validARN(r.TargetGroupArn) {
		return fmt.Errorf("target-group-arn must be a valid ARN")
	}
	return nil
}

func validARN(s string) bool {
	if s == "" {
		return true
	}
	parsed, err := awsarn.Parse(s)
	if err != nil {
		return false
	}
	if !validARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !validARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" && !validARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}

func selectedTargetHealthDescription(
	resp *elbv2.DescribeTargetHealthOutput,
) (elbv2types.TargetHealthDescription, error) {
	if resp == nil {
		return elbv2types.TargetHealthDescription{}, runtime.ErrNotFound
	}
	eligible := eligibleTargetHealthDescriptions(resp.TargetHealthDescriptions)
	if len(eligible) != 1 {
		return elbv2types.TargetHealthDescription{}, runtime.ErrNotFound
	}
	if eligible[0].Target == nil {
		return elbv2types.TargetHealthDescription{},
			fmt.Errorf("target health description has nil target")
	}
	return eligible[0], nil
}

func eligibleTargetHealthDescriptions(
	descriptions []elbv2types.TargetHealthDescription,
) []elbv2types.TargetHealthDescription {
	out := make([]elbv2types.TargetHealthDescription, 0, len(descriptions))
	for _, desc := range descriptions {
		if targetHealthDescriptionEligible(desc) {
			out = append(out, desc)
		}
	}
	return out
}

func targetHealthDescriptionEligible(desc elbv2types.TargetHealthDescription) bool {
	if desc.TargetHealth == nil {
		return false
	}
	switch desc.TargetHealth.Reason {
	case elbv2types.TargetHealthReasonEnumDeregistrationInProgress,
		elbv2types.TargetHealthReasonEnumNotRegistered:
		return false
	default:
		return true
	}
}
