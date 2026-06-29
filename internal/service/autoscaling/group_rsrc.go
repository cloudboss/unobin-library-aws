package autoscaling

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// Group is an EC2 Auto Scaling group: a fleet of instances kept at a target
// size from a launch template. The group is keyed by name, the one field fixed
// at create time; everything else is reconciled in place. CreateAutoScalingGroup
// accepts the size, placement, health-check, and policy fields plus the
// structured tags inline; the suspended processes, enabled metrics, and target
// groups are each reconciled by their own follow-on calls in Create and Update.
// Because a real instance launches, Create waits for the group to reach its
// target capacity before returning, and Delete drains the group to zero before
// removing it. Not-found is an empty describe result, never a typed exception.
//
// Several large sub-features of the CloudFormation resource are out of scope for
// this port and are not modeled: mixed-instances-policy and launch-configuration
// (so the launch-template block is required), warm-pool, instance-refresh,
// traffic-source, the initial-lifecycle-hook two-step create, classic-ELB
// load-balancers, availability-zone-distribution, and
// capacity-reservation-specification. The reserved context field, the
// deprecated launch-configuration ELB-health wait variant, and the deletion of
// failed scaling activities by an ignore flag are likewise omitted; the
// failed-activity short-circuit itself is always applied.
type Group struct {
	Name                      string                          `ub:"name"`
	MinSize                   int64                           `ub:"min-size"`
	MaxSize                   int64                           `ub:"max-size"`
	DesiredCapacity           *int64                          `ub:"desired-capacity"`
	DesiredCapacityType       *string                         `ub:"desired-capacity-type"`
	LaunchTemplate            GroupLaunchTemplate             `ub:"launch-template"`
	AvailabilityZones         *[]string                       `ub:"availability-zones"`
	VPCZoneIdentifier         *[]string                       `ub:"vpc-zone-identifier"`
	DefaultCooldown           *int64                          `ub:"default-cooldown"`
	DefaultInstanceWarmup     *int64                          `ub:"default-instance-warmup"`
	HealthCheckType           *string                         `ub:"health-check-type"`
	HealthCheckGracePeriod    *int64                          `ub:"health-check-grace-period"`
	CapacityRebalance         *bool                           `ub:"capacity-rebalance"`
	MaxInstanceLifetime       *int64                          `ub:"max-instance-lifetime"`
	PlacementGroup            *string                         `ub:"placement-group"`
	ServiceLinkedRoleArn      *string                         `ub:"service-linked-role-arn"`
	ProtectFromScaleIn        *bool                           `ub:"protect-from-scale-in"`
	TerminationPolicies       *[]string                       `ub:"termination-policies"`
	InstanceMaintenancePolicy *GroupInstanceMaintenancePolicy `ub:"instance-maintenance-policy"`
	Tags                      *[]GroupTag                     `ub:"tags"`
	SuspendedProcesses        *[]string                       `ub:"suspended-processes"`
	EnabledMetrics            *[]string                       `ub:"enabled-metrics"`
	MetricsGranularity        *string                         `ub:"metrics-granularity"`
	TargetGroupArns           *[]string                       `ub:"target-group-arns"`
	ForceDelete               *bool                           `ub:"force-delete"`
	WaitForCapacityTimeout    *string                         `ub:"wait-for-capacity-timeout"`
}

// GroupOutput holds the values the API computes or fills for an Auto Scaling
// group. The ARN is the group's handle. The Availability Zones and VPC zone
// identifier are filled with whichever was not supplied, the default cooldown,
// health-check type, desired capacity, and service-linked role are filled when
// omitted -- so a consumer reads the real values the cloud settled on.
type GroupOutput struct {
	Arn                  string   `ub:"arn"`
	AvailabilityZones    []string `ub:"availability-zones"`
	VPCZoneIdentifier    []string `ub:"vpc-zone-identifier"`
	DefaultCooldown      int64    `ub:"default-cooldown"`
	HealthCheckType      string   `ub:"health-check-type"`
	DesiredCapacity      int64    `ub:"desired-capacity"`
	ServiceLinkedRoleArn string   `ub:"service-linked-role-arn"`
}

func (r *Group) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs the API fixes when a group is created. Only the
// name is immutable; a change to it requires a new group. Every other field --
// capacity, launch template, zones, health check -- is updatable in place.
func (r *Group) ReplaceFields() []string {
	return []string{"name"}
}

// Constraints declares the rules the API enforces on a group's inputs. Sizes are
// non-negative. Exactly one of the Availability Zones and the VPC zone
// identifier is given; the API fills the other. The launch template names its
// template by id or by name, never both, and one of them is required. The
// optional enums -- desired-capacity unit, health-check type, metrics
// granularity -- are checked only when present.
func (r Group) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.AtLeast(r.MinSize, 0)).
			Message("min-size must be zero or greater"),
		constraint.Must(constraint.AtLeast(r.MaxSize, 0)).
			Message("max-size must be zero or greater"),
		constraint.Must(constraint.Any(
			constraint.Not(constraint.NotEmpty(r.AvailabilityZones)),
			constraint.Not(constraint.NotEmpty(r.VPCZoneIdentifier)))).
			Message("availability-zones and vpc-zone-identifier are mutually exclusive"),
		constraint.Must(constraint.Any(
			constraint.NotEmpty(r.AvailabilityZones),
			constraint.NotEmpty(r.VPCZoneIdentifier))).
			Message("one of availability-zones or vpc-zone-identifier is required"),
		constraint.AtMostOneOf(r.LaunchTemplate.Id, r.LaunchTemplate.Name),
		constraint.Must(constraint.Any(
			constraint.Present(r.LaunchTemplate.Id),
			constraint.Present(r.LaunchTemplate.Name))).
			Message("launch-template requires one of id or name"),
		constraint.When(constraint.Present(r.DesiredCapacityType)).
			Require(constraint.OneOf(r.DesiredCapacityType, "units", "vcpu", "memory-mib")).
			Message("desired-capacity-type must be units, vcpu, or memory-mib"),
		constraint.When(constraint.Present(r.HealthCheckType)).
			Require(constraint.OneOf(r.HealthCheckType, "EC2", "ELB", "VPC_LATTICE")).
			Message("health-check-type must be EC2, ELB, or VPC_LATTICE"),
		constraint.When(constraint.Present(r.MetricsGranularity)).
			Require(constraint.OneOf(r.MetricsGranularity, "1Minute")).
			Message("metrics-granularity must be 1Minute"),
	}
}

func (r *Group) Create(ctx context.Context, cfg *awsCfg) (*GroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	startTime := time.Now()
	if err := r.create(ctx, client); err != nil {
		return nil, err
	}
	// A new group launches instances, so block until it reaches its target
	// capacity. The create target is a minimum: the desired capacity when set
	// above zero, otherwise the minimum size.
	target := r.MinSize
	if r.DesiredCapacity != nil && *r.DesiredCapacity > 0 {
		target = *r.DesiredCapacity
	}
	if err := r.waitCapacity(ctx, client, startTime, target, false); err != nil {
		return nil, err
	}
	if err := r.suspendProcesses(ctx, client, ptr.Value(r.SuspendedProcesses)); err != nil {
		return nil, err
	}
	if err := r.enableMetrics(ctx, client, ptr.Value(r.EnabledMetrics)); err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// create issues CreateAutoScalingGroup with the create-time fields and inline
// tags. The launch template's instance profile or the service-linked role may
// not have propagated yet, so it retries on the matching ValidationError. On a
// partition that cannot tag at create time it retries once without tags and
// applies them afterward.
func (r *Group) create(ctx context.Context, client *autoscaling.Client) error {
	in := r.createInput()
	err := retry.OnError(ctx, isInvalidInstanceProfile, func(ctx context.Context) error {
		_, err := client.CreateAutoScalingGroup(ctx, in)
		return err
	}, retry.WithTimeout(2*time.Minute), retry.WithInterval(10*time.Second))
	if err == nil {
		return nil
	}
	if !partition.UnsupportedOperation(region(client), err) {
		return fmt.Errorf("create auto scaling group: %w", err)
	}
	untagged := *in
	untagged.Tags = nil
	err = retry.OnError(ctx, isInvalidInstanceProfile, func(ctx context.Context) error {
		_, err := client.CreateAutoScalingGroup(ctx, &untagged)
		return err
	}, retry.WithTimeout(2*time.Minute), retry.WithInterval(10*time.Second))
	if err != nil {
		return fmt.Errorf("create auto scaling group: %w", err)
	}
	if len(ptr.Value(r.Tags)) > 0 {
		if _, err := client.CreateOrUpdateTags(ctx, &autoscaling.CreateOrUpdateTagsInput{
			Tags: expandTags(r.Name, ptr.Value(r.Tags)),
		}); err != nil {
			return fmt.Errorf("tag auto scaling group: %w", err)
		}
	}
	return nil
}

// createInput builds the CreateAutoScalingGroup request. The desired capacity
// is sent only when set above zero; scale-in protection is always sent so the
// group's setting is explicit.
func (r *Group) createInput() *autoscaling.CreateAutoScalingGroupInput {
	in := &autoscaling.CreateAutoScalingGroupInput{
		AutoScalingGroupName:             aws.String(r.Name),
		MinSize:                          ptr.Int32(aws.Int64(r.MinSize)),
		MaxSize:                          ptr.Int32(aws.Int64(r.MaxSize)),
		LaunchTemplate:                   expandLaunchTemplate(r.LaunchTemplate),
		NewInstancesProtectedFromScaleIn: aws.Bool(aws.ToBool(r.ProtectFromScaleIn)),
		DesiredCapacityType:              r.DesiredCapacityType,
		DefaultCooldown:                  ptr.Int32(r.DefaultCooldown),
		DefaultInstanceWarmup:            ptr.Int32(r.DefaultInstanceWarmup),
		HealthCheckType:                  r.HealthCheckType,
		HealthCheckGracePeriod:           ptr.Int32(r.HealthCheckGracePeriod),
		CapacityRebalance:                r.CapacityRebalance,
		MaxInstanceLifetime:              ptr.Int32(r.MaxInstanceLifetime),
		PlacementGroup:                   r.PlacementGroup,
		ServiceLinkedRoleARN:             r.ServiceLinkedRoleArn,
		TerminationPolicies:              ptr.Value(r.TerminationPolicies),
		TargetGroupARNs:                  ptr.Value(r.TargetGroupArns),
	}
	if r.DesiredCapacity != nil && *r.DesiredCapacity > 0 {
		in.DesiredCapacity = ptr.Int32(r.DesiredCapacity)
	}
	if len(ptr.Value(r.AvailabilityZones)) > 0 {
		in.AvailabilityZones = ptr.Value(r.AvailabilityZones)
	}
	if len(ptr.Value(r.VPCZoneIdentifier)) > 0 {
		in.VPCZoneIdentifier = aws.String(strings.Join(ptr.Value(r.VPCZoneIdentifier), ","))
	}
	if r.InstanceMaintenancePolicy != nil {
		in.InstanceMaintenancePolicy = expandMaintenancePolicy(*r.InstanceMaintenancePolicy)
	}
	if len(ptr.Value(r.Tags)) > 0 {
		in.Tags = expandTags(r.Name, ptr.Value(r.Tags))
	}
	return in
}

func (r *Group) Read(ctx context.Context, cfg *awsCfg, prior *GroupOutput) (*GroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the group by name and maps it to outputs, returning
// runtime.ErrNotFound when it is gone.
func (r *Group) read(ctx context.Context, client *autoscaling.Client) (*GroupOutput, error) {
	g, err := findGroup(ctx, client, r.Name)
	if err != nil {
		return nil, err
	}
	out := &GroupOutput{
		Arn:                  aws.ToString(g.AutoScalingGroupARN),
		AvailabilityZones:    g.AvailabilityZones,
		DefaultCooldown:      int64(aws.ToInt32(g.DefaultCooldown)),
		HealthCheckType:      aws.ToString(g.HealthCheckType),
		DesiredCapacity:      int64(aws.ToInt32(g.DesiredCapacity)),
		ServiceLinkedRoleArn: aws.ToString(g.ServiceLinkedRoleARN),
	}
	if id := aws.ToString(g.VPCZoneIdentifier); id != "" {
		out.VPCZoneIdentifier = strings.Split(id, ",")
	}
	return out, nil
}

func (r *Group) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Group, *GroupOutput],
) (*GroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	startTime := time.Now()
	if r.groupChanged(prior) {
		if err := r.update(ctx, client, prior); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, ptr.Value(prior.Inputs.Tags)); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.TargetGroupArns), ptr.Value(r.TargetGroupArns)) {
		if err := r.syncTargetGroups(ctx, client, ptr.Value(prior.Inputs.TargetGroupArns)); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.SuspendedProcesses), ptr.Value(r.SuspendedProcesses)) {
		if err := r.syncSuspendedProcesses(ctx, client, ptr.Value(prior.Inputs.SuspendedProcesses)); err != nil {
			return nil, err
		}
	}
	if r.metricsChanged(prior) {
		if err := r.syncMetrics(ctx, client, ptr.Value(prior.Inputs.EnabledMetrics)); err != nil {
			return nil, err
		}
	}
	// Re-confirm capacity when a size knob changed, including the unit the
	// desired capacity is expressed in. The update target is exact: the group
	// settles at the larger of its minimum and desired sizes.
	if runtime.Changed(prior.Inputs.MinSize, r.MinSize) ||
		runtime.Changed(prior.Inputs.DesiredCapacity, r.DesiredCapacity) ||
		runtime.Changed(prior.Inputs.DesiredCapacityType, r.DesiredCapacityType) {
		target := r.MinSize
		if r.DesiredCapacity != nil && *r.DesiredCapacity > target {
			target = *r.DesiredCapacity
		}
		if err := r.waitCapacity(ctx, client, startTime, target, true); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

// groupChanged reports whether any field reconciled by UpdateAutoScalingGroup
// differs from the prior inputs. The set-reconciled fields -- tags, target
// groups, suspended processes, metrics -- are handled by their own calls and
// are not tested here.
func (r *Group) groupChanged(prior runtime.Prior[Group, *GroupOutput]) bool {
	p := prior.Inputs
	return runtime.Changed(p.MinSize, r.MinSize) ||
		runtime.Changed(p.MaxSize, r.MaxSize) ||
		runtime.Changed(p.DesiredCapacity, r.DesiredCapacity) ||
		runtime.Changed(p.DesiredCapacityType, r.DesiredCapacityType) ||
		runtime.Changed(p.LaunchTemplate, r.LaunchTemplate) ||
		runtime.Changed(ptr.Value(p.AvailabilityZones), ptr.Value(r.AvailabilityZones)) ||
		runtime.Changed(ptr.Value(p.VPCZoneIdentifier), ptr.Value(r.VPCZoneIdentifier)) ||
		runtime.Changed(p.DefaultCooldown, r.DefaultCooldown) ||
		runtime.Changed(p.DefaultInstanceWarmup, r.DefaultInstanceWarmup) ||
		runtime.Changed(p.HealthCheckType, r.HealthCheckType) ||
		runtime.Changed(p.HealthCheckGracePeriod, r.HealthCheckGracePeriod) ||
		runtime.Changed(p.CapacityRebalance, r.CapacityRebalance) ||
		runtime.Changed(p.MaxInstanceLifetime, r.MaxInstanceLifetime) ||
		runtime.Changed(p.PlacementGroup, r.PlacementGroup) ||
		runtime.Changed(p.ServiceLinkedRoleArn, r.ServiceLinkedRoleArn) ||
		runtime.Changed(p.ProtectFromScaleIn, r.ProtectFromScaleIn) ||
		runtime.Changed(ptr.Value(p.TerminationPolicies), ptr.Value(r.TerminationPolicies)) ||
		runtime.Changed(p.InstanceMaintenancePolicy, r.InstanceMaintenancePolicy)
}

// metricsChanged reports whether the enabled metrics or their granularity
// differ from the prior inputs; a granularity change re-enables the metrics.
func (r *Group) metricsChanged(prior runtime.Prior[Group, *GroupOutput]) bool {
	return runtime.Changed(ptr.Value(prior.Inputs.EnabledMetrics), ptr.Value(r.EnabledMetrics)) ||
		runtime.Changed(prior.Inputs.MetricsGranularity, r.MetricsGranularity)
}

// update issues one UpdateAutoScalingGroup that sends only the fields whose
// inputs changed, retried on the transient ValidationError. Scale-in
// protection is always sent, since the API folds it into every update. A
// removed scalar is simply not sent, so the cloud keeps its value; the
// exceptions are capacity-rebalance, termination-policies, and the
// instance-maintenance block, whose removal sends the API's documented reset
// sentinel, and only when that field itself changed. A health-check-type
// change re-sends a grace period in the same call, which the API requires
// present when the type changes.
func (r *Group) update(
	ctx context.Context, client *autoscaling.Client, prior runtime.Prior[Group, *GroupOutput],
) error {
	p := prior.Inputs
	in := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName:             aws.String(r.Name),
		NewInstancesProtectedFromScaleIn: aws.Bool(aws.ToBool(r.ProtectFromScaleIn)),
	}
	if runtime.Changed(p.MinSize, r.MinSize) {
		in.MinSize = ptr.Int32(aws.Int64(r.MinSize))
	}
	if runtime.Changed(p.MaxSize, r.MaxSize) {
		in.MaxSize = ptr.Int32(aws.Int64(r.MaxSize))
	}
	if runtime.Changed(p.DesiredCapacity, r.DesiredCapacity) {
		in.DesiredCapacity = ptr.Int32(r.DesiredCapacity)
	}
	if runtime.Changed(p.DesiredCapacityType, r.DesiredCapacityType) {
		in.DesiredCapacityType = r.DesiredCapacityType
	}
	if runtime.Changed(p.LaunchTemplate, r.LaunchTemplate) {
		in.LaunchTemplate = expandLaunchTemplate(r.LaunchTemplate)
	}
	if runtime.Changed(ptr.Value(p.AvailabilityZones), ptr.Value(r.AvailabilityZones)) &&
		len(ptr.Value(r.AvailabilityZones)) > 0 {
		in.AvailabilityZones = ptr.Value(r.AvailabilityZones)
	}
	if runtime.Changed(ptr.Value(p.VPCZoneIdentifier), ptr.Value(r.VPCZoneIdentifier)) &&
		len(ptr.Value(r.VPCZoneIdentifier)) > 0 {
		in.VPCZoneIdentifier = aws.String(strings.Join(ptr.Value(r.VPCZoneIdentifier), ","))
	}
	if runtime.Changed(p.DefaultCooldown, r.DefaultCooldown) {
		in.DefaultCooldown = ptr.Int32(r.DefaultCooldown)
	}
	if runtime.Changed(p.DefaultInstanceWarmup, r.DefaultInstanceWarmup) {
		in.DefaultInstanceWarmup = ptr.Int32(r.DefaultInstanceWarmup)
	}
	if runtime.Changed(p.MaxInstanceLifetime, r.MaxInstanceLifetime) {
		in.MaxInstanceLifetime = ptr.Int32(r.MaxInstanceLifetime)
	}
	if runtime.Changed(p.PlacementGroup, r.PlacementGroup) {
		in.PlacementGroup = r.PlacementGroup
	}
	if runtime.Changed(p.ServiceLinkedRoleArn, r.ServiceLinkedRoleArn) {
		in.ServiceLinkedRoleARN = r.ServiceLinkedRoleArn
	}
	if runtime.Changed(p.HealthCheckGracePeriod, r.HealthCheckGracePeriod) {
		in.HealthCheckGracePeriod = ptr.Int32(r.HealthCheckGracePeriod)
	}
	if runtime.Changed(p.HealthCheckType, r.HealthCheckType) && r.HealthCheckType != nil {
		if err := r.setHealthCheck(ctx, client, in); err != nil {
			return err
		}
	}
	if runtime.Changed(p.CapacityRebalance, r.CapacityRebalance) {
		r.setCapacityRebalance(in)
	}
	if runtime.Changed(ptr.Value(p.TerminationPolicies), ptr.Value(r.TerminationPolicies)) {
		r.setTerminationPolicies(in)
	}
	if runtime.Changed(p.InstanceMaintenancePolicy, r.InstanceMaintenancePolicy) {
		r.setMaintenancePolicy(in)
	}
	err := retry.OnError(ctx, isValidationError, func(ctx context.Context) error {
		_, err := client.UpdateAutoScalingGroup(ctx, in)
		return err
	}, retry.WithTimeout(2*time.Minute), retry.WithInterval(10*time.Second))
	if err != nil {
		return fmt.Errorf("update auto scaling group: %w", err)
	}
	return nil
}

// setHealthCheck sets the health-check type on the update and re-sends a grace
// period in the same call, which the API requires present when the type
// changes. When the input declares no period, the group's current value is
// fetched and re-sent unchanged, since there is no stored default to fall back
// on.
func (r *Group) setHealthCheck(
	ctx context.Context, client *autoscaling.Client, in *autoscaling.UpdateAutoScalingGroupInput,
) error {
	in.HealthCheckType = r.HealthCheckType
	if r.HealthCheckGracePeriod != nil {
		in.HealthCheckGracePeriod = ptr.Int32(r.HealthCheckGracePeriod)
		return nil
	}
	g, err := findGroup(ctx, client, r.Name)
	if err != nil {
		return err
	}
	in.HealthCheckGracePeriod = g.HealthCheckGracePeriod
	return nil
}

// setCapacityRebalance sets capacity rebalance on the update, sending an
// explicit false when it is omitted, since a null does not reset it at the API.
func (r *Group) setCapacityRebalance(in *autoscaling.UpdateAutoScalingGroupInput) {
	if r.CapacityRebalance != nil {
		in.CapacityRebalance = r.CapacityRebalance
		return
	}
	in.CapacityRebalance = aws.Bool(false)
}

// setTerminationPolicies sets the termination policies on the update, sending
// the default policy when they are omitted, since a null does not reset them.
func (r *Group) setTerminationPolicies(in *autoscaling.UpdateAutoScalingGroupInput) {
	if len(ptr.Value(r.TerminationPolicies)) > 0 {
		in.TerminationPolicies = ptr.Value(r.TerminationPolicies)
		return
	}
	in.TerminationPolicies = []string{"Default"}
}

// setMaintenancePolicy sets the instance-maintenance policy on the update,
// sending the removal sentinel when the block is omitted, since a null does not
// clear a previously set policy.
func (r *Group) setMaintenancePolicy(in *autoscaling.UpdateAutoScalingGroupInput) {
	if r.InstanceMaintenancePolicy != nil {
		in.InstanceMaintenancePolicy = expandMaintenancePolicy(*r.InstanceMaintenancePolicy)
		return
	}
	in.InstanceMaintenancePolicy = removedMaintenancePolicy()
}

func (r *Group) Delete(ctx context.Context, cfg *awsCfg, prior *GroupOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	g, err := findGroup(ctx, client, r.Name)
	if err != nil {
		if err == runtime.ErrNotFound {
			return nil
		}
		return err
	}
	force := aws.ToBool(r.ForceDelete)
	if !force {
		if err := r.drain(ctx, client, g); err != nil {
			return err
		}
	}
	if err := r.deleteGroup(ctx, client, force); err != nil {
		return err
	}
	return r.waitGone(ctx, client)
}

// drain brings the group to zero before deletion: it sets the sizes to zero,
// clears scale-in protection on any protected instance, then waits for the
// instance count to reach zero. A live group cannot be deleted, so this
// precedes the delete call when force-delete is off.
func (r *Group) drain(
	ctx context.Context, client *autoscaling.Client, g *autoscalingtypes.AutoScalingGroup,
) error {
	_, err := client.UpdateAutoScalingGroup(ctx, &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: aws.String(r.Name),
		MinSize:              aws.Int32(0),
		MaxSize:              aws.Int32(0),
		DesiredCapacity:      aws.Int32(0),
	})
	if err != nil {
		return fmt.Errorf("drain auto scaling group: %w", err)
	}
	if err := r.clearScaleInProtection(ctx, client, g); err != nil {
		return err
	}
	return r.waitDrained(ctx, client)
}

// clearScaleInProtection removes scale-in protection from every protected
// instance so it can terminate during the drain. The instances are cleared in
// batches; an instance that has already left the group raises a ValidationError
// that is swallowed, since its departure is the outcome the drain wants.
func (r *Group) clearScaleInProtection(
	ctx context.Context, client *autoscaling.Client, g *autoscalingtypes.AutoScalingGroup,
) error {
	var protected []string
	for _, inst := range g.Instances {
		if aws.ToBool(inst.ProtectedFromScaleIn) {
			protected = append(protected, aws.ToString(inst.InstanceId))
		}
	}
	for _, batch := range batches(protected, 50) {
		_, err := client.SetInstanceProtection(ctx, &autoscaling.SetInstanceProtectionInput{
			AutoScalingGroupName: aws.String(r.Name),
			InstanceIds:          batch,
			ProtectedFromScaleIn: aws.Bool(false),
		})
		if err != nil && !isNotInGroup(err) {
			return fmt.Errorf("clear scale-in protection: %w", err)
		}
	}
	return nil
}

// deleteGroup issues DeleteAutoScalingGroup, retrying through an in-progress
// scaling activity or another conflicting operation. A group already gone
// reports a ValidationError saying so, which is treated as success.
func (r *Group) deleteGroup(ctx context.Context, client *autoscaling.Client, force bool) error {
	err := retry.OnError(ctx, isDeleteConflict, func(ctx context.Context) error {
		_, err := client.DeleteAutoScalingGroup(ctx, &autoscaling.DeleteAutoScalingGroupInput{
			AutoScalingGroupName: aws.String(r.Name),
			ForceDelete:          aws.Bool(force),
		})
		return err
	}, retry.WithTimeout(10*time.Minute), retry.WithInterval(time.Second))
	if err != nil && !isNotFoundValidation(err) {
		return fmt.Errorf("delete auto scaling group: %w", err)
	}
	return nil
}

// suspendProcesses suspends the named scaling processes when any are given.
func (r *Group) suspendProcesses(
	ctx context.Context, client *autoscaling.Client, processes []string,
) error {
	if len(processes) == 0 {
		return nil
	}
	_, err := client.SuspendProcesses(ctx, &autoscaling.SuspendProcessesInput{
		AutoScalingGroupName: aws.String(r.Name),
		ScalingProcesses:     processes,
	})
	if err != nil {
		return fmt.Errorf("suspend processes: %w", err)
	}
	return nil
}

// resumeProcesses resumes the named scaling processes when any are given.
func (r *Group) resumeProcesses(
	ctx context.Context, client *autoscaling.Client, processes []string,
) error {
	if len(processes) == 0 {
		return nil
	}
	_, err := client.ResumeProcesses(ctx, &autoscaling.ResumeProcessesInput{
		AutoScalingGroupName: aws.String(r.Name),
		ScalingProcesses:     processes,
	})
	if err != nil {
		return fmt.Errorf("resume processes: %w", err)
	}
	return nil
}

// syncSuspendedProcesses reconciles the suspended-process set to the declared
// set: it resumes the processes no longer listed and suspends the newly listed.
func (r *Group) syncSuspendedProcesses(
	ctx context.Context, client *autoscaling.Client, prior []string,
) error {
	added, removed := stringSetDiff(prior, ptr.Value(r.SuspendedProcesses))
	if err := r.resumeProcesses(ctx, client, removed); err != nil {
		return err
	}
	return r.suspendProcesses(ctx, client, added)
}

// enableMetrics enables the named group metrics at the configured granularity
// when any are given.
func (r *Group) enableMetrics(
	ctx context.Context, client *autoscaling.Client, metrics []string,
) error {
	if len(metrics) == 0 {
		return nil
	}
	_, err := client.EnableMetricsCollection(ctx, &autoscaling.EnableMetricsCollectionInput{
		AutoScalingGroupName: aws.String(r.Name),
		Granularity:          r.MetricsGranularity,
		Metrics:              metrics,
	})
	if err != nil {
		return fmt.Errorf("enable metrics collection: %w", err)
	}
	return nil
}

// disableMetrics disables the named group metrics when any are given.
func (r *Group) disableMetrics(
	ctx context.Context, client *autoscaling.Client, metrics []string,
) error {
	if len(metrics) == 0 {
		return nil
	}
	_, err := client.DisableMetricsCollection(ctx, &autoscaling.DisableMetricsCollectionInput{
		AutoScalingGroupName: aws.String(r.Name),
		Metrics:              metrics,
	})
	if err != nil {
		return fmt.Errorf("disable metrics collection: %w", err)
	}
	return nil
}

// syncMetrics reconciles the enabled-metric set to the declared set. When the
// granularity changed it re-enables the whole set, since the granularity rides
// the enable call; otherwise it disables the removed metrics and enables the
// added ones.
func (r *Group) syncMetrics(
	ctx context.Context, client *autoscaling.Client, prior []string,
) error {
	if runtime.Changed(prior, ptr.Value(r.EnabledMetrics)) {
		added, removed := stringSetDiff(prior, ptr.Value(r.EnabledMetrics))
		if err := r.disableMetrics(ctx, client, removed); err != nil {
			return err
		}
		return r.enableMetrics(ctx, client, added)
	}
	// Only the granularity changed; re-enable the full set under it.
	return r.enableMetrics(ctx, client, ptr.Value(r.EnabledMetrics))
}

// syncTags reconciles the structured tag set to the declared set: it deletes
// the tags removed and creates or updates the tags added or changed.
func (r *Group) syncTags(ctx context.Context, client *autoscaling.Client, prior []GroupTag) error {
	remove, upsert := diffTags(prior, ptr.Value(r.Tags))
	if len(remove) > 0 {
		tags := make([]autoscalingtypes.Tag, 0, len(remove))
		for _, t := range remove {
			tags = append(tags, tagToSDK(r.Name, t))
		}
		if _, err := client.DeleteTags(ctx, &autoscaling.DeleteTagsInput{Tags: tags}); err != nil {
			return fmt.Errorf("delete tags: %w", err)
		}
	}
	if len(upsert) > 0 {
		if _, err := client.CreateOrUpdateTags(ctx, &autoscaling.CreateOrUpdateTagsInput{
			Tags: expandTags(r.Name, upsert),
		}); err != nil {
			return fmt.Errorf("create or update tags: %w", err)
		}
	}
	return nil
}

// syncTargetGroups reconciles the attached target groups to the declared set,
// detaching the ARNs removed and attaching the ARNs added. Each direction is
// batched, and each batch is waited until the count in transition reaches zero
// so a later read does not race the attach or detach settling.
func (r *Group) syncTargetGroups(
	ctx context.Context, client *autoscaling.Client, prior []string,
) error {
	added, removed := stringSetDiff(prior, ptr.Value(r.TargetGroupArns))
	for _, batch := range batches(removed, 10) {
		_, err := client.DetachLoadBalancerTargetGroups(ctx,
			&autoscaling.DetachLoadBalancerTargetGroupsInput{
				AutoScalingGroupName: aws.String(r.Name),
				TargetGroupARNs:      batch,
			})
		if err != nil {
			return fmt.Errorf("detach target groups: %w", err)
		}
		if err := r.waitTargetGroupsSettled(ctx, client, "Removing"); err != nil {
			return err
		}
	}
	for _, batch := range batches(added, 10) {
		_, err := client.AttachLoadBalancerTargetGroups(ctx,
			&autoscaling.AttachLoadBalancerTargetGroupsInput{
				AutoScalingGroupName: aws.String(r.Name),
				TargetGroupARNs:      batch,
			})
		if err != nil {
			return fmt.Errorf("attach target groups: %w", err)
		}
		if err := r.waitTargetGroupsSettled(ctx, client, "Adding"); err != nil {
			return err
		}
	}
	return nil
}

// waitTargetGroupsSettled polls until no attached target group is still in the
// given transitional state, so an attach or detach is fully settled before the
// next batch.
func (r *Group) waitTargetGroupsSettled(
	ctx context.Context, client *autoscaling.Client, state string,
) error {
	what := fmt.Sprintf("group %s target groups to leave %s", r.Name, state)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		resp, err := client.DescribeLoadBalancerTargetGroups(ctx,
			&autoscaling.DescribeLoadBalancerTargetGroupsInput{
				AutoScalingGroupName: aws.String(r.Name),
			})
		if err != nil {
			return false, fmt.Errorf("describe target groups: %w", err)
		}
		for _, tg := range resp.LoadBalancerTargetGroups {
			if aws.ToString(tg.State) == state {
				return false, nil
			}
		}
		return true, nil
	}, wait.WithTimeout(10*time.Minute), wait.WithInterval(time.Second))
}

// waitCapacity blocks until the group reaches its target capacity. exact picks
// the predicate: a create requires at least the target in service, an update
// requires exactly the target. A scaling activity that fails fast aborts the
// wait so a doomed launch does not run out the timeout. The wait is governed by
// wait-for-capacity-timeout, defaulting to ten minutes; a zero value disables
// it.
func (r *Group) waitCapacity(
	ctx context.Context, client *autoscaling.Client, since time.Time, target int64, exact bool,
) error {
	timeout, err := r.capacityTimeout()
	if err != nil {
		return err
	}
	if timeout == 0 {
		return nil
	}
	what := fmt.Sprintf("group %s to reach capacity %d", r.Name, target)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		if err := r.checkScalingActivities(ctx, client, since); err != nil {
			return false, err
		}
		g, err := findGroup(ctx, client, r.Name)
		if err != nil {
			return false, err
		}
		count := inServiceCapacity(g)
		if exact {
			return count == target, nil
		}
		return count >= target, nil
	}, wait.WithTimeout(timeout), wait.WithInterval(10*time.Second))
}

// capacityTimeout parses the wait-for-capacity-timeout knob. It defaults to ten
// minutes when unset; a value of zero disables the capacity wait.
func (r *Group) capacityTimeout() (time.Duration, error) {
	if r.WaitForCapacityTimeout == nil {
		return 10 * time.Minute, nil
	}
	d, err := time.ParseDuration(*r.WaitForCapacityTimeout)
	if err != nil {
		return 0, fmt.Errorf("parse wait-for-capacity-timeout %q: %w",
			*r.WaitForCapacityTimeout, err)
	}
	return d, nil
}

// checkScalingActivities fails the capacity wait if a scaling activity started
// since the operation began has failed outright, so a doomed launch fails fast.
// An activity failing on an unpropagated IAM instance profile is skipped, since
// the create retry covers that race and the profile usually catches up.
func (r *Group) checkScalingActivities(
	ctx context.Context, client *autoscaling.Client, since time.Time,
) error {
	paginator := autoscaling.NewDescribeScalingActivitiesPaginator(client,
		&autoscaling.DescribeScalingActivitiesInput{
			AutoScalingGroupName: aws.String(r.Name),
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("describe scaling activities: %w", err)
		}
		for _, a := range page.Activities {
			if a.StartTime != nil && a.StartTime.Before(since) {
				continue
			}
			if a.StatusCode != autoscalingtypes.ScalingActivityStatusCodeFailed {
				continue
			}
			if aws.ToInt32(a.Progress) != 100 {
				continue
			}
			msg := aws.ToString(a.StatusMessage)
			if strings.Contains(msg, "Invalid IAM Instance Profile") {
				continue
			}
			return fmt.Errorf("scaling activity %s failed: %s",
				aws.ToString(a.ActivityId), msg)
		}
	}
	return nil
}

// waitDrained polls until the group holds no instances, the precondition for
// deleting it without a force delete.
func (r *Group) waitDrained(ctx context.Context, client *autoscaling.Client) error {
	what := fmt.Sprintf("group %s to drain to zero instances", r.Name)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		g, err := findGroup(ctx, client, r.Name)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		return len(g.Instances) == 0, nil
	}, wait.WithTimeout(10*time.Minute), wait.WithInterval(5*time.Second))
}

// waitGone polls until the group no longer describes. The API keeps returning a
// deleted group for a while after the delete call accepts, so the delete is not
// complete until the describe goes empty.
func (r *Group) waitGone(ctx context.Context, client *autoscaling.Client) error {
	what := fmt.Sprintf("group %s to be deleted", r.Name)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := findGroup(ctx, client, r.Name)
		if err == runtime.ErrNotFound {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	}, wait.WithTimeout(10*time.Minute), wait.WithInterval(time.Second))
}

// findGroup describes the group by name and returns it. The API returns an
// empty list for a missing name rather than an error, so an empty result maps
// to runtime.ErrNotFound; a returned group whose name does not match the
// request is likewise treated as not-found, guarding against a stale match.
func findGroup(
	ctx context.Context, client *autoscaling.Client, name string,
) (*autoscalingtypes.AutoScalingGroup, error) {
	resp, err := client.DescribeAutoScalingGroups(ctx,
		&autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []string{name},
		})
	if err != nil {
		return nil, fmt.Errorf("describe auto scaling groups: %w", err)
	}
	if len(resp.AutoScalingGroups) == 0 {
		return nil, runtime.ErrNotFound
	}
	g := resp.AutoScalingGroups[0]
	if aws.ToString(g.AutoScalingGroupName) != name {
		return nil, runtime.ErrNotFound
	}
	return &g, nil
}

// inServiceCapacity counts the capacity in service in the group toward the wait
// target. An instance counts only when it is healthy and in service; an
// instance with a weighted capacity contributes its weight, otherwise one.
func inServiceCapacity(g *autoscalingtypes.AutoScalingGroup) int64 {
	var total int64
	for _, inst := range g.Instances {
		if aws.ToString(inst.HealthStatus) != "Healthy" {
			continue
		}
		if inst.LifecycleState != autoscalingtypes.LifecycleStateInService {
			continue
		}
		if w := aws.ToString(inst.WeightedCapacity); w != "" {
			if n, err := strconv.ParseInt(w, 10, 64); err == nil {
				total += n
				continue
			}
		}
		total++
	}
	return total
}
