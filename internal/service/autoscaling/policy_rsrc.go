package autoscaling

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// policyTypeSimple is the policy type the API defaults to when none is given.
// Several fields are valid only for it, and its required-field rules apply when
// the policy type is omitted as well as when it is named explicitly.
const policyTypeSimple = "SimpleScaling"

// policyTypeStep is the step-scaling policy type.
const policyTypeStep = "StepScaling"

// policyTypeTargetTracking is the target-tracking policy type.
const policyTypeTargetTracking = "TargetTrackingScaling"

// PolicyResource is an EC2 Auto Scaling scaling policy: a rule that changes an Auto
// Scaling group's size in response to a CloudWatch alarm or a tracked target.
// The whole resource is one PutScalingPolicy upsert, used for both create and
// update, with no waiters or transient retries. Its complexity is the policy
// type, which gates which fields are valid: SimpleScaling uses a single scaling
// adjustment, StepScaling a set of stepped adjustments, and TargetTrackingScaling
// a metric held to a target value. The policy is keyed by the group name plus
// the policy name, both fixed at create time; a change to either makes a new
// policy.
//
// PredictiveScaling remains a valid policy type, but its
// predictive-scaling-configuration block is not modeled here and is left for a
// future addition.
type PolicyResource struct {
	AutoScalingGroupName    string                             `ub:"autoscaling-group-name"`
	Name                    string                             `ub:"name"`
	PolicyType              *string                            `ub:"policy-type"`
	Enabled                 *bool                              `ub:"enabled"`
	AdjustmentType          *string                            `ub:"adjustment-type"`
	Cooldown                *int64                             `ub:"cooldown"`
	EstimatedInstanceWarmup *int64                             `ub:"estimated-instance-warmup"`
	MetricAggregationType   *string                            `ub:"metric-aggregation-type"`
	MinAdjustmentMagnitude  *int64                             `ub:"min-adjustment-magnitude"`
	ScalingAdjustment       *int64                             `ub:"scaling-adjustment"`
	StepAdjustments         *[]PolicyStepAdjustment            `ub:"step-adjustments"`
	TargetTracking          *PolicyTargetTrackingConfiguration `ub:"target-tracking-configuration"`
}

// PolicyStepAdjustment is one step in a step-scaling policy: a scaling
// adjustment that applies when the alarm's metric falls in a bounded interval
// relative to the breach threshold. The scaling adjustment is required. A null
// lower bound means negative infinity and a null upper bound positive infinity,
// so at most one step may omit each bound.
type PolicyStepAdjustment struct {
	ScalingAdjustment        int64    `ub:"scaling-adjustment"`
	MetricIntervalLowerBound *float64 `ub:"metric-interval-lower-bound"`
	MetricIntervalUpperBound *float64 `ub:"metric-interval-upper-bound"`
}

// PolicyResourceOutput holds the values the API computes or fills for a scaling policy.
// The ARN is the policy's handle. The group and policy names are kept so a
// replacement's delete, which receives the prior outputs, targets the old
// policy. The metric aggregation type is filled with Average for a step policy
// that omits it, so a consumer reads the value the cloud settled on.
type PolicyResourceOutput struct {
	Arn                   string `ub:"arn"`
	AutoScalingGroupName  string `ub:"autoscaling-group-name"`
	Name                  string `ub:"name"`
	MetricAggregationType string `ub:"metric-aggregation-type"`
}

func (r *PolicyResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs the API fixes when a policy is created. The
// group name and the policy name together form the policy's identity; a change
// to either requires a new policy. Every other field is reconciled in place by
// re-issuing PutScalingPolicy.
func (r *PolicyResource) ReplaceFields() []string {
	return []string{"autoscaling-group-name", "name"}
}

// Constraints declares the rules the API enforces on a policy's inputs, keyed
// on the policy type. The simple-scaling rules also apply when the policy type
// is omitted, since the API defaults it to SimpleScaling. A scaling adjustment
// and a step-adjustment set are mutually exclusive, and each field that is valid
// for only some policy types is admitted only for those types. The
// min-adjustment-magnitude lower bound is checked when present. The deep-nested
// rules inside the target-tracking block (the predefined-versus-customized
// metric choice, the metric-math-versus-inline choice, and the period and
// string-length bounds) stay enforced by the API, matching the constraint
// vocabulary's reach.
func (r PolicyResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.PolicyType)).
			Require(constraint.OneOf(r.PolicyType, "SimpleScaling", "StepScaling",
				"TargetTrackingScaling", "PredictiveScaling")).
			Message("policy-type must be SimpleScaling, StepScaling, " +
				"TargetTrackingScaling, or PredictiveScaling"),
		constraint.Must(constraint.Any(
			constraint.Absent(r.ScalingAdjustment),
			constraint.Not(constraint.NotEmpty(r.StepAdjustments)))).
			Message("scaling-adjustment and step-adjustments are mutually exclusive"),
		constraint.When(constraint.Any(constraint.Absent(r.PolicyType),
			constraint.Equals(r.PolicyType, "SimpleScaling"))).
			Require(constraint.Present(r.ScalingAdjustment)).
			Message("scaling-adjustment is required when policy-type is SimpleScaling"),
		constraint.When(constraint.Present(r.ScalingAdjustment)).
			Require(constraint.Any(constraint.Absent(r.PolicyType),
				constraint.Equals(r.PolicyType, "SimpleScaling"))).
			Message("scaling-adjustment is valid only when policy-type is SimpleScaling"),
		constraint.When(constraint.Equals(r.PolicyType, "StepScaling")).
			Require(constraint.NotEmpty(r.StepAdjustments)).
			Message("step-adjustments is required when policy-type is StepScaling"),
		constraint.When(constraint.NotEmpty(r.StepAdjustments)).
			Require(constraint.Equals(r.PolicyType, "StepScaling")).
			Message("step-adjustments is valid only when policy-type is StepScaling"),
		constraint.When(constraint.Equals(r.PolicyType, "TargetTrackingScaling")).
			Require(constraint.Present(r.TargetTracking)).
			Message("target-tracking-configuration is required when policy-type " +
				"is TargetTrackingScaling"),
		constraint.When(constraint.Present(r.TargetTracking)).
			Require(constraint.Equals(r.PolicyType, "TargetTrackingScaling")).
			Message("target-tracking-configuration is valid only when policy-type " +
				"is TargetTrackingScaling"),
		constraint.When(constraint.Present(r.Cooldown)).
			Require(constraint.Any(constraint.Absent(r.PolicyType),
				constraint.Equals(r.PolicyType, "SimpleScaling"))).
			Message("cooldown is valid only when policy-type is SimpleScaling"),
		constraint.When(constraint.Present(r.EstimatedInstanceWarmup)).
			Require(constraint.OneOf(r.PolicyType, "StepScaling", "TargetTrackingScaling")).
			Message("estimated-instance-warmup is valid only when policy-type is " +
				"StepScaling or TargetTrackingScaling"),
		constraint.When(constraint.Present(r.MinAdjustmentMagnitude)).
			Require(constraint.AtLeast(r.MinAdjustmentMagnitude, 1)).
			Message("min-adjustment-magnitude must be at least 1"),
	}
}

func (r *PolicyResource) Create(ctx context.Context, cfg *awsCfg) (*PolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := client.PutScalingPolicy(ctx, r.putInput()); err != nil {
		return nil, fmt.Errorf("put scaling policy: %w", err)
	}
	return r.read(ctx, client, true)
}

// putInput builds the PutScalingPolicy request, the upsert used for both create
// and update. The group name, policy name, policy type, and enabled flag are
// always sent; the enabled flag defaults to true. Every other field is sent only
// for the policy types it is valid for, so a value left over from a different
// type never reaches the API.
func (r *PolicyResource) putInput() *autoscaling.PutScalingPolicyInput {
	in := &autoscaling.PutScalingPolicyInput{
		AutoScalingGroupName: aws.String(r.AutoScalingGroupName),
		PolicyName:           aws.String(r.Name),
		PolicyType:           aws.String(r.policyType()),
		Enabled:              aws.Bool(r.enabled()),
	}
	policyType := r.policyType()
	if policyType == policyTypeSimple || policyType == policyTypeStep {
		in.AdjustmentType = r.AdjustmentType
		if r.MinAdjustmentMagnitude != nil {
			in.MinAdjustmentMagnitude = ptr.Int32(r.MinAdjustmentMagnitude)
		}
	}
	switch policyType {
	case policyTypeSimple:
		in.ScalingAdjustment = ptr.Int32(r.ScalingAdjustment)
		in.Cooldown = ptr.Int32(r.Cooldown)
	case policyTypeStep:
		in.StepAdjustments = expandStepAdjustments(ptr.Value(r.StepAdjustments))
		in.MetricAggregationType = r.MetricAggregationType
		in.EstimatedInstanceWarmup = ptr.Int32(r.EstimatedInstanceWarmup)
	case policyTypeTargetTracking:
		if r.TargetTracking != nil {
			in.TargetTrackingConfiguration = expandTargetTracking(*r.TargetTracking)
		}
		in.EstimatedInstanceWarmup = ptr.Int32(r.EstimatedInstanceWarmup)
	}
	return in
}

// policyType returns the effective policy type, defaulting to SimpleScaling when
// none is given, the same default the API applies.
func (r *PolicyResource) policyType() string {
	if r.PolicyType != nil {
		return *r.PolicyType
	}
	return policyTypeSimple
}

// enabled returns the effective enabled flag, defaulting to true, and is always
// sent so the policy's state is explicit.
func (r *PolicyResource) enabled() bool {
	if r.Enabled != nil {
		return *r.Enabled
	}
	return true
}

func (r *PolicyResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *PolicyResourceOutput) (*PolicyResourceOutput, error,
) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, false)
}

// read fetches the policy by its group and policy names and maps it to outputs.
// Just after a create the policy can briefly read as absent through eventual
// consistency; when created is true read waits past that miss rather than
// reporting the resource gone, so a transient miss does not make a fresh policy
// look like it needs recreating. In the steady state a missing policy is drift
// and maps to runtime.ErrNotFound.
func (r *PolicyResource) read(
	ctx context.Context, client *autoscaling.Client, created bool,
) (*PolicyResourceOutput, error) {
	var policy *autoscalingtypes.ScalingPolicy
	probe := func(ctx context.Context) (bool, error) {
		p, err := findPolicy(ctx, client, r.AutoScalingGroupName, r.Name)
		if err != nil {
			if err == runtime.ErrNotFound {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			return false, err
		}
		policy = p
		return true, nil
	}
	if created {
		what := fmt.Sprintf("scaling policy %s", r.Name)
		if err := wait.Until(ctx, what, probe); err != nil {
			return nil, err
		}
	} else if _, err := probe(ctx); err != nil {
		return nil, err
	}
	return &PolicyResourceOutput{
		Arn:                   aws.ToString(policy.PolicyARN),
		AutoScalingGroupName:  aws.ToString(policy.AutoScalingGroupName),
		Name:                  aws.ToString(policy.PolicyName),
		MetricAggregationType: aws.ToString(policy.MetricAggregationType),
	}, nil
}

func (r *PolicyResource) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[PolicyResource, *PolicyResourceOutput],
) (*PolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// PutScalingPolicy is a full upsert that can resend every parameter without
	// recreating the policy, so the whole call is gated on any input change
	// rather than each field on its own.
	if r.changed(prior) {
		if _, err := client.PutScalingPolicy(ctx, r.putInput()); err != nil {
			return nil, fmt.Errorf("put scaling policy: %w", err)
		}
	}
	return r.read(ctx, client, false)
}

// changed reports whether any input that rides PutScalingPolicy differs from the
// prior inputs. The group name and policy name are not tested: a change to
// either replaces the policy rather than updating it.
func (r *PolicyResource) changed(prior runtime.Prior[PolicyResource, *PolicyResourceOutput]) bool {
	p := prior.Inputs
	return runtime.Changed(p.PolicyType, r.PolicyType) ||
		runtime.Changed(p.Enabled, r.Enabled) ||
		runtime.Changed(p.AdjustmentType, r.AdjustmentType) ||
		runtime.Changed(p.Cooldown, r.Cooldown) ||
		runtime.Changed(p.EstimatedInstanceWarmup, r.EstimatedInstanceWarmup) ||
		runtime.Changed(p.MetricAggregationType, r.MetricAggregationType) ||
		runtime.Changed(p.MinAdjustmentMagnitude, r.MinAdjustmentMagnitude) ||
		runtime.Changed(p.ScalingAdjustment, r.ScalingAdjustment) ||
		runtime.Changed(ptr.Value(p.StepAdjustments), ptr.Value(r.StepAdjustments)) ||
		runtime.Changed(p.TargetTracking, r.TargetTracking)
}

func (r *PolicyResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *PolicyResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// The delete keys off the prior outputs so a replacement removes the old
	// policy. A ValidationError saying the group is gone, or an already-removed
	// policy, is treated as success.
	_, err = client.DeletePolicy(ctx, &autoscaling.DeletePolicyInput{
		AutoScalingGroupName: aws.String(prior.AutoScalingGroupName),
		PolicyName:           aws.String(prior.Name),
	})
	if err != nil && !isNotFoundValidation(err) {
		return fmt.Errorf("delete policy: %w", err)
	}
	return nil
}

// expandStepAdjustments converts the step-adjustment blocks into the SDK
// adjustments. A null interval bound is passed through as null, which the API
// reads as the corresponding infinity.
func expandStepAdjustments(steps []PolicyStepAdjustment) []autoscalingtypes.StepAdjustment {
	if len(steps) == 0 {
		return nil
	}
	out := make([]autoscalingtypes.StepAdjustment, 0, len(steps))
	for _, s := range steps {
		out = append(out, autoscalingtypes.StepAdjustment{
			ScalingAdjustment:        aws.Int32(int32(s.ScalingAdjustment)),
			MetricIntervalLowerBound: s.MetricIntervalLowerBound,
			MetricIntervalUpperBound: s.MetricIntervalUpperBound,
		})
	}
	return out
}

// findPolicy describes the policy by its group and policy names and returns it.
// A ValidationError whose message says the resource is not found means the group
// itself is gone and maps to runtime.ErrNotFound. An empty result means the
// policy is gone, the common deletion case, and likewise maps to
// runtime.ErrNotFound. More than one match is an error rather than not-found,
// since the name filter should return at most one.
func findPolicy(
	ctx context.Context, client *autoscaling.Client, group, name string,
) (*autoscalingtypes.ScalingPolicy, error) {
	in := &autoscaling.DescribePoliciesInput{
		AutoScalingGroupName: aws.String(group),
		PolicyNames:          []string{name},
	}
	var found []autoscalingtypes.ScalingPolicy
	paginator := autoscaling.NewDescribePoliciesPaginator(client, in)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isNotFoundValidation(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe policies: %w", err)
		}
		found = append(found, page.ScalingPolicies...)
	}
	switch len(found) {
	case 0:
		return nil, runtime.ErrNotFound
	case 1:
		return &found[0], nil
	default:
		return nil, fmt.Errorf("describe policies: found %d policies named %q in group %q",
			len(found), name, group)
	}
}
