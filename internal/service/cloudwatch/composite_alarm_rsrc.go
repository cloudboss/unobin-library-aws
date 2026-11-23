package cloudwatch

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type CompositeAlarmResource struct {
	AlarmName               string                    `ub:"alarm-name"`
	AlarmRule               string                    `ub:"alarm-rule"`
	AlarmDescription        *string                   `ub:"alarm-description"`
	ActionsEnabled          *bool                     `ub:"actions-enabled"`
	ActionsSuppressor       *CompositeAlarmSuppressor `ub:"actions-suppressor"`
	AlarmActions            *[]string                 `ub:"alarm-actions"`
	OKActions               *[]string                 `ub:"ok-actions"`
	InsufficientDataActions *[]string                 `ub:"insufficient-data-actions"`
	Tags                    *map[string]string        `ub:"tags"`
}

type CompositeAlarmSuppressor struct {
	Alarm           string `ub:"alarm"`
	ExtensionPeriod int64  `ub:"extension-period"`
	WaitPeriod      int64  `ub:"wait-period"`
}

type CompositeAlarmResourceOutput struct {
	Arn                     string                    `ub:"arn"`
	AlarmName               string                    `ub:"alarm-name"`
	AlarmRule               string                    `ub:"alarm-rule"`
	AlarmDescription        string                    `ub:"alarm-description"`
	ActionsEnabled          bool                      `ub:"actions-enabled"`
	ActionsSuppressor       *CompositeAlarmSuppressor `ub:"actions-suppressor"`
	AlarmActions            []string                  `ub:"alarm-actions"`
	OKActions               []string                  `ub:"ok-actions"`
	InsufficientDataActions []string                  `ub:"insufficient-data-actions"`
	Tags                    map[string]string         `ub:"tags"`
}

type compositeAlarmPrior = runtime.Prior[
	CompositeAlarmResource, *CompositeAlarmResourceOutput, *awsCfg,
]

type compositeAlarmDefinition = runtime.ResourceDefinition[
	CompositeAlarmResource, *CompositeAlarmResourceOutput, *awsCfg,
]

const compositeActionARNPattern = `^(?:|arn:aws(?:-[a-z]+)*:[^:]*:` +
	`(?:[a-z]{2,4}(?:-[a-z]+)+-[0-9]{1,2})?:` +
	`(?:aws|aws-managed|third-party|aws-marketplace|partner-managed|[0-9]{12}|cw[^:\n]{10})?:` +
	`[\s\S]+)$`

const compositeActionCountRule = "@core.length({ for value in (input.actions ?? []): " +
	"value => true... when value != null }) <= 5"

func (r CompositeAlarmResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(
			constraint.NotEquals(r.AlarmName, nil),
			constraint.NotEquals(r.AlarmName, ""),
			constraint.AtMost(constraint.ByteLength(r.AlarmName), 255),
		).Message("alarm-name must be a nonempty string of at most 255 bytes"),
		constraint.Must(
			constraint.NotEquals(r.AlarmRule, nil),
			constraint.NotEquals(r.AlarmRule, ""),
			constraint.AtMost(constraint.ByteLength(r.AlarmRule), 10240),
		).Message("alarm-rule must be a nonempty string of at most 10240 bytes"),
		constraint.Must(constraint.Expr(
			"@core.byte-length(input.description ?? '') <= 1024",
			constraint.Bind("description", r.AlarmDescription),
		)).Message("alarm-description must be at most 1024 bytes"),
		constraint.When(constraint.NotEquals(r.ActionsSuppressor, nil)).Require(
			constraint.NotEquals(r.ActionsSuppressor.Alarm, nil),
			constraint.NotEquals(r.ActionsSuppressor.ExtensionPeriod, nil),
			constraint.NotEquals(r.ActionsSuppressor.WaitPeriod, nil),
			constraint.AtLeast(r.ActionsSuppressor.ExtensionPeriod, math.MinInt32),
			constraint.AtMost(r.ActionsSuppressor.ExtensionPeriod, math.MaxInt32),
			constraint.AtLeast(r.ActionsSuppressor.WaitPeriod, math.MinInt32),
			constraint.AtMost(r.ActionsSuppressor.WaitPeriod, math.MaxInt32),
		).Message("actions-suppressor requires alarm and signed 32-bit extension and wait periods"),
		constraint.Must(constraint.Expr(compositeActionCountRule,
			constraint.Bind("actions", r.AlarmActions),
		)).Message("alarm-actions allows at most five distinct values"),
		constraint.Must(constraint.Expr(compositeActionCountRule,
			constraint.Bind("actions", r.OKActions),
		)).Message("ok-actions allows at most five distinct values"),
		constraint.Must(constraint.Expr(compositeActionCountRule,
			constraint.Bind("actions", r.InsufficientDataActions),
		)).Message("insufficient-data-actions allows at most five distinct values"),
		constraint.ForEach(r.AlarmActions, func(action string) []constraint.Constraint {
			return []constraint.Constraint{constraint.Must(
				constraint.NotEquals(action, nil),
				constraint.IsMatch(action, compositeActionARNPattern),
			).Message("alarm-actions entries must be AWS ARNs or empty strings")}
		}),
		constraint.ForEach(r.OKActions, func(action string) []constraint.Constraint {
			return []constraint.Constraint{constraint.Must(
				constraint.NotEquals(action, nil),
				constraint.IsMatch(action, compositeActionARNPattern),
			).Message("ok-actions entries must be AWS ARNs or empty strings")}
		}),
		constraint.ForEach(r.InsufficientDataActions, func(action string) []constraint.Constraint {
			return []constraint.Constraint{constraint.Must(
				constraint.NotEquals(action, nil),
				constraint.IsMatch(action, compositeActionARNPattern),
			).Message("insufficient-data-actions entries must be AWS ARNs or empty strings")}
		}),
	}
}

func (r *CompositeAlarmResource) ResourceDefinition() compositeAlarmDefinition {
	type input = CompositeAlarmResource
	type output = *CompositeAlarmResourceOutput
	return compositeAlarmDefinition{
		SchemaVersion: 1,
		StableID: func(_ input, out output) (string, error) {
			if out == nil || out.Arn == "" {
				return "", errors.New("missing composite alarm ARN")
			}
			return out.Arn, nil
		},
		Equality: []runtime.InputEqualityRule[input]{
			runtime.EqualBy(
				runtime.InputField(func(r *input) **bool { return &r.ActionsEnabled }),
				func(a, b *bool) bool { return compositeActionsEnabled(a) == compositeActionsEnabled(b) },
			),
			runtime.EqualBy(
				runtime.InputField(func(r *input) **string { return &r.AlarmDescription }),
				func(a, b *string) bool { return aws.ToString(a) == aws.ToString(b) },
			),
			runtime.EqualBy(
				runtime.InputField(func(r *input) **[]string { return &r.AlarmActions }),
				equalCompositeActions,
			),
			runtime.EqualBy(
				runtime.InputField(func(r *input) **[]string { return &r.OKActions }),
				equalCompositeActions,
			),
			runtime.EqualBy(
				runtime.InputField(func(r *input) **[]string { return &r.InsufficientDataActions }),
				equalCompositeActions,
			),
			runtime.EqualBy(
				runtime.InputField(func(r *input) **map[string]string { return &r.Tags }),
				equalCompositeTags,
			),
		},
		Replace: runtime.Replacement[input, output, *awsCfg]{
			Fields: []runtime.AnyInputField[input]{
				runtime.InputField(func(r *input) *string { return &r.AlarmName }),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
	}
}

func (r *CompositeAlarmResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*CompositeAlarmResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	input := r.putInput()
	tags := metricAlarmTags(compositeUserTags(ptr.Value(r.Tags)))
	input.Tags = tags
	_, err = client.PutCompositeAlarm(ctx, input)
	tagSeparately := len(tags) > 0 && partition.UnsupportedOperation(client.Options().Region, err)
	if tagSeparately {
		input.Tags = nil
		_, err = client.PutCompositeAlarm(ctx, input)
	}
	if err != nil {
		return nil, fmt.Errorf("put composite alarm: %w", err)
	}
	alarm, err := requireCompositeAlarm(ctx, client, r.AlarmName)
	if err != nil {
		return nil, fmt.Errorf("read created composite alarm: %w", err)
	}
	if tagSeparately {
		if _, err := client.TagResource(ctx, &cloudwatch.TagResourceInput{
			ResourceARN: alarm.AlarmArn, Tags: tags,
		}); err != nil {
			return nil, fmt.Errorf("tag created composite alarm: %w", err)
		}
		alarm, err = requireCompositeAlarm(ctx, client, r.AlarmName)
		if err != nil {
			return nil, fmt.Errorf("read created composite alarm: %w", err)
		}
	}
	return compositeAlarmOutput(ctx, client, alarm)
}

func (r *CompositeAlarmResource) Read(
	ctx context.Context, cfg *awsCfg, prior compositeAlarmPrior,
) (*CompositeAlarmResourceOutput, error) {
	client, err := newCompositePriorClient(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	alarm, err := findCompositeAlarm(ctx, client, compositePriorName(prior))
	if err != nil {
		return nil, err
	}
	if alarm == nil {
		return nil, runtime.ErrNotFound
	}
	return compositeAlarmOutput(ctx, client, alarm)
}

func (r *CompositeAlarmResource) Update(
	ctx context.Context, cfg *awsCfg, prior compositeAlarmPrior,
) (*CompositeAlarmResourceOutput, error) {
	client, err := newCompositePriorClient(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	tagsChanged := !equalCompositeTags(prior.Inputs.Tags, r.Tags)
	tagsDrifted := prior.Outputs != nil && prior.Observed != nil &&
		!maps.Equal(prior.Outputs.Tags, prior.Observed.Tags)
	if tagsChanged || tagsDrifted {
		if err := r.syncTags(ctx, client, compositePriorARN(prior)); err != nil {
			return nil, err
		}
	}
	if r.alarmChanged(prior.Inputs) || compositeAlarmDrifted(prior.Outputs, prior.Observed) {
		if _, err := client.PutCompositeAlarm(ctx, r.putInput()); err != nil {
			return nil, fmt.Errorf("put composite alarm: %w", err)
		}
	}

	alarm, err := requireCompositeAlarm(ctx, client, r.AlarmName)
	if err != nil {
		return nil, fmt.Errorf("read updated composite alarm: %w", err)
	}
	return compositeAlarmOutput(ctx, client, alarm)
}

func (r *CompositeAlarmResource) Delete(
	ctx context.Context, cfg *awsCfg, prior compositeAlarmPrior,
) error {
	client, err := newCompositePriorClient(ctx, cfg, prior)
	if err != nil {
		return err
	}
	_, err = client.DeleteAlarms(ctx, &cloudwatch.DeleteAlarmsInput{
		AlarmNames: []string{compositePriorName(prior)},
	})
	var absent *cloudwatchtypes.ResourceNotFound
	if err != nil && !isNotFound(err) && !errors.As(err, &absent) {
		return fmt.Errorf("delete composite alarm: %w", err)
	}
	return nil
}

func compositePriorName(prior compositeAlarmPrior) string {
	if prior.Outputs != nil && prior.Outputs.AlarmName != "" {
		return prior.Outputs.AlarmName
	}
	return prior.Inputs.AlarmName
}

func newCompositePriorClient(
	ctx context.Context, cfg *awsCfg, prior compositeAlarmPrior,
) (*cloudwatch.Client, error) {
	if prior.Configuration != nil {
		cfg = prior.Configuration
		if aws.ToString(cfg.Region) != "" {
			return newClient(ctx, cfg)
		}
	}
	if prior.Outputs == nil || prior.Outputs.Arn == "" {
		return newClient(ctx, cfg)
	}
	target, err := arn.Parse(prior.Outputs.Arn)
	if err != nil {
		return nil, fmt.Errorf("parse prior composite alarm ARN: %w", err)
	}
	if target.Service != "cloudwatch" || target.Region == "" {
		return nil, fmt.Errorf("prior composite alarm ARN has no CloudWatch region: %q",
			prior.Outputs.Arn)
	}
	var regional awsCfg
	if cfg != nil {
		regional = *cfg
	}
	regional.Region = aws.String(target.Region)
	return newClient(ctx, &regional)
}

func compositePriorARN(prior compositeAlarmPrior) string {
	if prior.Observed != nil {
		return prior.Observed.Arn
	}
	return prior.Outputs.Arn
}

func (r *CompositeAlarmResource) putInput() *cloudwatch.PutCompositeAlarmInput {
	input := &cloudwatch.PutCompositeAlarmInput{
		AlarmName:               aws.String(r.AlarmName),
		AlarmRule:               aws.String(r.AlarmRule),
		ActionsEnabled:          aws.Bool(compositeActionsEnabled(r.ActionsEnabled)),
		AlarmActions:            compositeActionRequest(ptr.Value(r.AlarmActions)),
		OKActions:               compositeActionRequest(ptr.Value(r.OKActions)),
		InsufficientDataActions: compositeActionRequest(ptr.Value(r.InsufficientDataActions)),
	}
	if aws.ToString(r.AlarmDescription) != "" {
		input.AlarmDescription = r.AlarmDescription
	}
	if suppressor := r.ActionsSuppressor; suppressor != nil {
		if suppressor.Alarm != "" {
			input.ActionsSuppressor = aws.String(suppressor.Alarm)
		}
		input.ActionsSuppressorExtensionPeriod = aws.Int32(int32(suppressor.ExtensionPeriod))
		input.ActionsSuppressorWaitPeriod = aws.Int32(int32(suppressor.WaitPeriod))
	}
	return input
}

func compositeActionsEnabled(value *bool) bool {
	return value == nil || *value
}

func compositeActionRequest(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return compositeActionSet(values)
}

func compositeActionSet(values []string) []string {
	set := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			set = append(set, value)
		}
	}
	slices.Sort(set)
	return slices.Compact(set)
}

func equalCompositeActions(a, b *[]string) bool {
	left, right := compositeActionRequest(ptr.Value(a)), compositeActionRequest(ptr.Value(b))
	return (left == nil) == (right == nil) && slices.Equal(left, right)
}

func equalCompositeTags(a, b *map[string]string) bool {
	return maps.Equal(compositeUserTags(ptr.Value(a)), compositeUserTags(ptr.Value(b)))
}

func (r *CompositeAlarmResource) alarmChanged(prior CompositeAlarmResource) bool {
	return r.AlarmName != prior.AlarmName ||
		r.AlarmRule != prior.AlarmRule ||
		aws.ToString(r.AlarmDescription) != aws.ToString(prior.AlarmDescription) ||
		compositeActionsEnabled(r.ActionsEnabled) != compositeActionsEnabled(prior.ActionsEnabled) ||
		runtime.Changed(r.ActionsSuppressor, prior.ActionsSuppressor) ||
		!equalCompositeActions(r.AlarmActions, prior.AlarmActions) ||
		!equalCompositeActions(r.OKActions, prior.OKActions) ||
		!equalCompositeActions(r.InsufficientDataActions, prior.InsufficientDataActions)
}

func compositeAlarmDrifted(recorded, observed *CompositeAlarmResourceOutput) bool {
	if recorded == nil || observed == nil {
		return false
	}
	before, after := *recorded, *observed
	before.Tags, after.Tags = nil, nil
	return runtime.Changed(before, after)
}

func findCompositeAlarm(
	ctx context.Context, client *cloudwatch.Client, name string,
) (*cloudwatchtypes.CompositeAlarm, error) {
	pages := cloudwatch.NewDescribeAlarmsPaginator(client, &cloudwatch.DescribeAlarmsInput{
		AlarmNames: []string{name},
		AlarmTypes: []cloudwatchtypes.AlarmType{cloudwatchtypes.AlarmTypeCompositeAlarm},
	})
	var alarm *cloudwatchtypes.CompositeAlarm
	count := 0
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe composite alarm %q: %w", name, err)
		}
		if len(page.CompositeAlarms) > 0 {
			alarm = &page.CompositeAlarms[0]
			count += len(page.CompositeAlarms)
		}
	}
	if count > 1 {
		return nil, fmt.Errorf("describe composite alarm %q: multiple composite alarms", name)
	}
	return alarm, nil
}

func requireCompositeAlarm(
	ctx context.Context, client *cloudwatch.Client, name string,
) (*cloudwatchtypes.CompositeAlarm, error) {
	alarm, err := findCompositeAlarm(ctx, client, name)
	if err != nil {
		return nil, err
	}
	if alarm == nil {
		return nil, fmt.Errorf("composite alarm %q was absent after write", name)
	}
	if aws.ToString(alarm.AlarmArn) == "" {
		return nil, fmt.Errorf("composite alarm %q has no ARN", name)
	}
	return alarm, nil
}

func compositeAlarmOutput(
	ctx context.Context, client *cloudwatch.Client, alarm *cloudwatchtypes.CompositeAlarm,
) (*CompositeAlarmResourceOutput, error) {
	arn := aws.ToString(alarm.AlarmArn)
	tags, err := readCompositeAlarmTags(ctx, client, arn)
	if err != nil {
		return nil, err
	}
	output := &CompositeAlarmResourceOutput{
		Arn:                     arn,
		AlarmName:               aws.ToString(alarm.AlarmName),
		AlarmRule:               aws.ToString(alarm.AlarmRule),
		AlarmDescription:        aws.ToString(alarm.AlarmDescription),
		ActionsEnabled:          aws.ToBool(alarm.ActionsEnabled),
		AlarmActions:            compositeActionSet(alarm.AlarmActions),
		OKActions:               compositeActionSet(alarm.OKActions),
		InsufficientDataActions: compositeActionSet(alarm.InsufficientDataActions),
		Tags:                    tags,
	}
	if alarm.ActionsSuppressor != nil {
		output.ActionsSuppressor = &CompositeAlarmSuppressor{
			Alarm:           *alarm.ActionsSuppressor,
			ExtensionPeriod: int64(aws.ToInt32(alarm.ActionsSuppressorExtensionPeriod)),
			WaitPeriod:      int64(aws.ToInt32(alarm.ActionsSuppressorWaitPeriod)),
		}
	}
	return output, nil
}

func compositeUserTags(tags map[string]string) map[string]string {
	user, _ := tagsync.Diff(nil, tags)
	return user
}

func readCompositeAlarmTags(
	ctx context.Context, client *cloudwatch.Client, arn string,
) (map[string]string, error) {
	resp, err := client.ListTagsForResource(ctx, &cloudwatch.ListTagsForResourceInput{
		ResourceARN: aws.String(arn),
	})
	if err != nil {
		if partition.UnsupportedOperation(client.Options().Region, err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("list composite alarm tags: %w", err)
	}
	tags := make(map[string]string, len(resp.Tags))
	for _, tag := range resp.Tags {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return compositeUserTags(tags), nil
}

func (r *CompositeAlarmResource) syncTags(
	ctx context.Context, client *cloudwatch.Client, arn string,
) error {
	err := tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			return readCompositeAlarmTags(ctx, client, arn)
		},
		func(ctx context.Context, tags map[string]string) error {
			_, err := client.TagResource(ctx, &cloudwatch.TagResourceInput{
				ResourceARN: aws.String(arn), Tags: metricAlarmTags(tags),
			})
			if err != nil {
				return fmt.Errorf("tag composite alarm: %w", err)
			}
			return nil
		},
		func(ctx context.Context, keys []string) error {
			_, err := client.UntagResource(ctx, &cloudwatch.UntagResourceInput{
				ResourceARN: aws.String(arn), TagKeys: keys,
			})
			if err != nil {
				return fmt.Errorf("untag composite alarm: %w", err)
			}
			return nil
		},
	)
	if partition.UnsupportedOperation(client.Options().Region, err) {
		return nil
	}
	return err
}
