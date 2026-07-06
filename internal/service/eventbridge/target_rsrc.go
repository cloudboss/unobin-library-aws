package eventbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	eventbridge "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

// listTargetsLimit is the page size for ListTargetsByRule, its maximum, so a
// rule's targets are read in as few calls as possible.
const listTargetsLimit = 100

// TargetResource attaches one destination to an EventBridge rule, the way
// AWS::Events::Target's underlying model treats a single entry in the rule's
// target list. There is no separate target API object: a target is written,
// read, and removed as a member of its rule through PutTargets,
// ListTargetsByRule, and RemoveTargets. Every parameter is a field on that one
// target reconciled by PutTargets, which is an upsert keyed by the target id,
// so an update is the same call with no follow-on operations. The rule, event
// bus, and target id form the identity, so a change to any of them replaces the
// target; the destination ARN and every parameter block update in place. The
// input or input transformation comes from at most one of three mutually
// exclusive forms.
type TargetResource struct {
	Rule                        string                             `ub:"rule"`
	Arn                         string                             `ub:"arn"`
	EventBusName                *string                            `ub:"event-bus-name"`
	TargetId                    *string                            `ub:"target-id"`
	RoleArn                     *string                            `ub:"role-arn"`
	Input                       *string                            `ub:"input"`
	InputPath                   *string                            `ub:"input-path"`
	InputTransformer            *TargetInputTransformer            `ub:"input-transformer"`
	RetryPolicy                 *TargetRetryPolicy                 `ub:"retry-policy"`
	DeadLetterConfig            *TargetDeadLetterConfig            `ub:"dead-letter-config"`
	EcsParameters               *TargetEcsParameters               `ub:"ecs-parameters"`
	BatchParameters             *TargetBatchParameters             `ub:"batch-parameters"`
	KinesisParameters           *TargetKinesisParameters           `ub:"kinesis-parameters"`
	SqsParameters               *TargetSqsParameters               `ub:"sqs-parameters"`
	HttpParameters              *TargetHttpParameters              `ub:"http-parameters"`
	RedshiftDataParameters      *TargetRedshiftDataParameters      `ub:"redshift-data-parameters"`
	RunCommandParameters        *TargetRunCommandParameters        `ub:"run-command-parameters"`
	SageMakerPipelineParameters *TargetSageMakerPipelineParameters `ub:"sage-maker-pipeline-parameters"`
	AppSyncParameters           *TargetAppSyncParameters           `ub:"app-sync-parameters"`
	ForceDestroy                *bool                              `ub:"force-destroy"`
}

// TargetResourceOutput holds the settled target id. When the input omits it, a unique
// id is generated and recorded here so it becomes the target's stable identity:
// the next plan reads the target by this id, and an omitted input does not
// regenerate the id and force a replace.
type TargetResourceOutput struct {
	TargetId string `ub:"target-id"`
}

func (r *TargetResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that form the target's identity. The rule a
// target belongs to, the event bus that rule lives on, and the target id are
// fixed once written; a change to any of them removes the old target and adds a
// new one. The destination ARN and every parameter update in place.
func (r *TargetResource) ReplaceFields() []string {
	return []string{"event-bus-name", "rule", "target-id"}
}

// Constraints declares the rules EventBridge places on a target's inputs: the
// input passed to the destination comes from at most one of the three mutually
// exclusive forms (a static JSON input, a JSONPath into the event, or an input
// transformer), and the parameter blocks have the enums, bounds, required
// members, and collection counts their doc comments state. String length
// limits and the reserved AWS input-path key prefix are left to the
// EventBridge API.
func (r TargetResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.Input, r.InputPath, r.InputTransformer),
		constraint.Must(constraint.MaxItems(r.InputTransformer.InputPaths, 100)).
			Message("input-paths holds at most 100 entries"),
		constraint.When(constraint.Present(r.RetryPolicy.MaximumEventAgeInSeconds)).
			Require(constraint.AtLeast(r.RetryPolicy.MaximumEventAgeInSeconds, 0),
				constraint.AtMost(r.RetryPolicy.MaximumEventAgeInSeconds, 86400)).
			Message("maximum-event-age-in-seconds must be between 0 and 86400"),
		constraint.When(constraint.Present(r.RetryPolicy.MaximumRetryAttempts)).
			Require(constraint.AtLeast(r.RetryPolicy.MaximumRetryAttempts, 0),
				constraint.AtMost(r.RetryPolicy.MaximumRetryAttempts, 185)).
			Message("maximum-retry-attempts must be between 0 and 185"),
		constraint.When(constraint.Present(r.BatchParameters.ArraySize)).
			Require(constraint.AtLeast(r.BatchParameters.ArraySize, 2),
				constraint.AtMost(r.BatchParameters.ArraySize, 10000)).
			Message("array-size must be between 2 and 10000"),
		constraint.When(constraint.Present(r.BatchParameters.JobAttempts)).
			Require(constraint.AtLeast(r.BatchParameters.JobAttempts, 1),
				constraint.AtMost(r.BatchParameters.JobAttempts, 10)).
			Message("job-attempts must be between 1 and 10"),
		constraint.When(constraint.Present(r.EcsParameters.LaunchType)).
			Require(constraint.OneOf(r.EcsParameters.LaunchType,
				"EC2", "FARGATE", "EXTERNAL")).
			Message("launch-type must be EC2, FARGATE, or EXTERNAL"),
		constraint.When(constraint.Present(r.EcsParameters.PropagateTags)).
			Require(constraint.OneOf(r.EcsParameters.PropagateTags, "TASK_DEFINITION")).
			Message("propagate-tags must be TASK_DEFINITION"),
		constraint.When(constraint.Present(r.EcsParameters.NetworkConfiguration)).
			Require(constraint.Present(r.EcsParameters.NetworkConfiguration.Subnets)).
			Message("an ECS network configuration requires subnets"),
		constraint.ForEach(r.EcsParameters.CapacityProviderStrategy,
			func(s TargetEcsParametersCapacityProviderStrategy) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(s.Base)).
						Require(constraint.AtLeast(s.Base, 0),
							constraint.AtMost(s.Base, 100000)).
						Message("a capacity provider base must be between 0 and 100000"),
					constraint.When(constraint.Present(s.Weight)).
						Require(constraint.AtLeast(s.Weight, 0),
							constraint.AtMost(s.Weight, 1000)).
						Message("a capacity provider weight must be between 0 and 1000"),
				}
			}),
		constraint.ForEach(r.EcsParameters.PlacementConstraints,
			func(c TargetEcsParametersPlacementConstraint) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(c.Type,
						"distinctInstance", "memberOf")).
						Message("a placement constraint type must be distinctInstance or memberOf"),
					constraint.When(constraint.Equals(c.Type, "memberOf")).
						Require(constraint.Present(c.Expression)).
						Message("a memberOf placement constraint requires an expression"),
				}
			}),
		constraint.ForEach(r.EcsParameters.PlacementStrategy,
			func(s TargetEcsParametersPlacementStrategy) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(s.Type, "random", "spread", "binpack")).
						Message("a placement strategy type must be random, spread, or binpack"),
				}
			}),
		constraint.Must(
			constraint.MaxItems(r.RunCommandParameters.RunCommandTargets, 5)).
			Message("run-command-targets holds at most 5 entries"),
		constraint.ForEach(r.RunCommandParameters.RunCommandTargets,
			func(t TargetRunCommandParametersTarget) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.NotEmpty(t.Values),
						constraint.MaxItems(t.Values, 50)).
						Message("a run command target takes 1 to 50 values"),
				}
			}),
		constraint.Must(constraint.MaxItems(
			r.SageMakerPipelineParameters.PipelineParameterList, 200)).
			Message("pipeline-parameter-list holds at most 200 entries"),
	}
}

func (r *TargetResource) Create(ctx context.Context, cfg *awsCfg) (*TargetResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	targetID, err := r.resolveTargetID()
	if err != nil {
		return nil, err
	}
	if err := r.putTargets(ctx, client, targetID); err != nil {
		return nil, err
	}
	return r.read(ctx, client, targetID)
}

func (r *TargetResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *TargetResourceOutput) (*TargetResourceOutput, error,
) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.TargetId)
}

func (r *TargetResource) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[TargetResource, *TargetResourceOutput],
) (*TargetResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	targetID := prior.Outputs.TargetId
	// PutTargets upserts the whole target by id, so a single call reconciles
	// every changed parameter at once. It runs only when a non-replace input
	// changed; force-destroy is a delete-time switch and is excluded, so a
	// change to it alone is a no-op here.
	if r.targetChanged(prior.Inputs) {
		if err := r.putTargets(ctx, client, targetID); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, targetID)
}

func (r *TargetResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *TargetResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &eventbridge.RemoveTargetsInput{
		Rule:         aws.String(r.Rule),
		EventBusName: r.EventBusName,
		Ids:          []string{prior.TargetId},
		Force:        aws.ToBool(r.ForceDestroy),
	}
	// Concurrent writes to one rule race, so a remove retries through the
	// conflict EventBridge raises while another target write to the same rule is
	// in flight. A rule already gone takes its targets with it, so a not-found
	// on remove is success.
	var out *eventbridge.RemoveTargetsOutput
	err = retry.OnError(ctx, isConcurrentModification, func(ctx context.Context) error {
		var err error
		out, err = client.RemoveTargets(ctx, in)
		return err
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("remove target %s from rule %s: %w", prior.TargetId, r.Rule, err)
	}
	if err := removeFailures(out.FailedEntries); err != nil {
		return fmt.Errorf("remove target %s from rule %s: %w", prior.TargetId, r.Rule, err)
	}
	return nil
}

// putTargets writes the single target under the given id and checks the result
// for a per-target failure. PutTargets can answer with a success status and a
// non-empty failed-entry list, a silent failure channel, so the entries are
// inspected after every call and turned into an error. Concurrent writes to one
// rule race, so the call retries through the conflict EventBridge raises.
func (r *TargetResource) putTargets(
	ctx context.Context,
	client *eventbridge.Client,
	id string,
) error {
	in := &eventbridge.PutTargetsInput{
		Rule:         aws.String(r.Rule),
		EventBusName: r.EventBusName,
		Targets:      []eventbridgetypes.Target{r.target(id)},
	}
	var out *eventbridge.PutTargetsOutput
	err := retry.OnError(ctx, isConcurrentModification, func(ctx context.Context) error {
		var err error
		out, err = client.PutTargets(ctx, in)
		return err
	})
	if err != nil {
		return fmt.Errorf("put target %s on rule %s: %w", id, r.Rule, err)
	}
	if err := putFailures(out.FailedEntries); err != nil {
		return fmt.Errorf("put target %s on rule %s: %w", id, r.Rule, err)
	}
	return nil
}

// read finds the target by id among the rule's targets and returns its output.
// A read maps to runtime.ErrNotFound, so a plan recreates the target, when the
// rule itself is gone or when no target on the rule has the id.
func (r *TargetResource) read(
	ctx context.Context, client *eventbridge.Client, id string,
) (*TargetResourceOutput, error) {
	found, err := r.findTarget(ctx, client, id)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, runtime.ErrNotFound
	}
	return &TargetResourceOutput{TargetId: aws.ToString(found.Id)}, nil
}

// findTarget pages ListTargetsByRule for the rule and returns the target whose
// id matches, or nil when none does. A rule that does not exist comes back as a
// not-found rather than an error, so the caller treats it as a gone target. The
// EventBridge SDK has no paginator for this call, so the pages are walked by the
// next-token directly.
func (r *TargetResource) findTarget(
	ctx context.Context, client *eventbridge.Client, id string,
) (*eventbridgetypes.Target, error) {
	in := &eventbridge.ListTargetsByRuleInput{
		Rule:         aws.String(r.Rule),
		EventBusName: r.EventBusName,
		Limit:        aws.Int32(listTargetsLimit),
	}
	for {
		out, err := client.ListTargetsByRule(ctx, in)
		if err != nil {
			if isRuleGone(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("list targets for rule %s: %w", r.Rule, err)
		}
		for i := range out.Targets {
			if aws.ToString(out.Targets[i].Id) == id {
				return &out.Targets[i], nil
			}
		}
		if out.NextToken == nil {
			return nil, nil
		}
		in.NextToken = out.NextToken
	}
}

// target builds the SDK target from the inputs, assembling every parameter
// block. The destination ARN and id are always set; the optional role and the
// three input forms are passed through, and each block converts itself, leaving
// its member unset when absent.
func (r *TargetResource) target(id string) eventbridgetypes.Target {
	return eventbridgetypes.Target{
		Id:                          aws.String(id),
		Arn:                         aws.String(r.Arn),
		RoleArn:                     r.RoleArn,
		Input:                       r.Input,
		InputPath:                   r.InputPath,
		InputTransformer:            r.InputTransformer.to(),
		RetryPolicy:                 r.RetryPolicy.to(),
		DeadLetterConfig:            r.DeadLetterConfig.to(),
		EcsParameters:               r.EcsParameters.to(),
		BatchParameters:             r.BatchParameters.to(),
		KinesisParameters:           r.KinesisParameters.to(),
		SqsParameters:               r.SqsParameters.to(),
		HttpParameters:              r.HttpParameters.to(),
		RedshiftDataParameters:      r.RedshiftDataParameters.to(),
		RunCommandParameters:        r.RunCommandParameters.to(),
		SageMakerPipelineParameters: r.SageMakerPipelineParameters.to(),
		AppSyncParameters:           r.AppSyncParameters.to(),
	}
}

// resolveTargetID returns the configured target id, or a generated unique id
// when none is given. The generated id is the prefix "unobin-" followed by 32
// hex characters, 39 in all, within the 64-character limit and the
// [0-9A-Za-z_.-] character set EventBridge accepts.
func (r *TargetResource) resolveTargetID() (string, error) {
	if r.TargetId != nil {
		return *r.TargetId, nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate target id: %w", err)
	}
	return "unobin-" + hex.EncodeToString(b), nil
}

// targetChanged reports whether any input PutTargets sends differs from the
// prior inputs. It covers every parameter and the destination ARN, but not the
// identity inputs, which trigger a replace instead of an update, nor
// force-destroy, which acts only at delete.
func (r *TargetResource) targetChanged(prior TargetResource) bool {
	return runtime.Changed(prior.Arn, r.Arn) ||
		runtime.Changed(prior.RoleArn, r.RoleArn) ||
		runtime.Changed(prior.Input, r.Input) ||
		runtime.Changed(prior.InputPath, r.InputPath) ||
		runtime.Changed(prior.InputTransformer, r.InputTransformer) ||
		runtime.Changed(prior.RetryPolicy, r.RetryPolicy) ||
		runtime.Changed(prior.DeadLetterConfig, r.DeadLetterConfig) ||
		runtime.Changed(prior.EcsParameters, r.EcsParameters) ||
		runtime.Changed(prior.BatchParameters, r.BatchParameters) ||
		runtime.Changed(prior.KinesisParameters, r.KinesisParameters) ||
		runtime.Changed(prior.SqsParameters, r.SqsParameters) ||
		runtime.Changed(prior.HttpParameters, r.HttpParameters) ||
		runtime.Changed(prior.RedshiftDataParameters, r.RedshiftDataParameters) ||
		runtime.Changed(prior.RunCommandParameters, r.RunCommandParameters) ||
		runtime.Changed(prior.SageMakerPipelineParameters, r.SageMakerPipelineParameters) ||
		runtime.Changed(prior.AppSyncParameters, r.AppSyncParameters)
}

// isRuleGone reports whether a ListTargetsByRule error means the rule no longer
// exists. EventBridge answers a missing rule with its typed not-found, but it
// can also answer with a ValidationException or another error whose message
// ends in " not found"; the SDK has no typed ValidationException, so that form
// is matched by code and message.
func isRuleGone(err error) bool {
	if isNotFound(err) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() == "ValidationException" {
			return true
		}
		if strings.HasSuffix(apiErr.ErrorMessage(), " not found") {
			return true
		}
	}
	return false
}

// putFailures joins the per-target failures from a PutTargets result into one
// error, naming each failed target id with its error code and message. It
// returns nil when there are none.
func putFailures(entries []eventbridgetypes.PutTargetsResultEntry) error {
	if len(entries) == 0 {
		return nil
	}
	errs := make([]error, 0, len(entries))
	for i := range entries {
		errs = append(errs, fmt.Errorf("target %s: %s: %s",
			aws.ToString(entries[i].TargetId),
			aws.ToString(entries[i].ErrorCode),
			aws.ToString(entries[i].ErrorMessage)))
	}
	return errors.Join(errs...)
}

// removeFailures joins the per-target failures from a RemoveTargets result into
// one error, naming each failed target id with its error code and message. It
// returns nil when there are none.
func removeFailures(entries []eventbridgetypes.RemoveTargetsResultEntry) error {
	if len(entries) == 0 {
		return nil
	}
	errs := make([]error, 0, len(entries))
	for i := range entries {
		errs = append(errs, fmt.Errorf("target %s: %s: %s",
			aws.ToString(entries[i].TargetId),
			aws.ToString(entries[i].ErrorCode),
			aws.ToString(entries[i].ErrorMessage)))
	}
	return errors.Join(errs...)
}
