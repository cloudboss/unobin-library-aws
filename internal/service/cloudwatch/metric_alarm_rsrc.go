package cloudwatch

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatch "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

// treatMissingDataDefault is the behavior CloudWatch applies for missing data
// points when none is given. The upsert always sends a value, so an omitted
// treat-missing-data is sent as this default rather than left out.
const treatMissingDataDefault = "missing"

// actionsEnabledDefault is the default for whether alarm actions run on a state
// change. CloudWatch defaults it to true, and the upsert always sends a value,
// so an omitted actions-enabled is sent as this default.
const actionsEnabledDefault = true

// percentilePattern matches the extended statistics CloudWatch accepts for an
// extended-statistic or a metric-query stat: a pXX.X percentile or one of the
// trimmed/winsorized/interquartile families. It mirrors the values the API
// validates and is checked in code because the constraint vocabulary cannot
// match a string against a pattern.
var percentilePattern = regexp.MustCompile(
	`^p(\d{1,3}(\.\d{1,2})?)$|` +
		`^(tm|wm|tc|ts)(\d{1,3}(\.\d{1,2})?)$|` +
		`^IQM$|` +
		`^(TM|WM|PR|TC|TS)\(\d+(\.\d+)?%?:\d+(\.\d+)?%?\)$|` +
		`^(TM|WM|PR|TC|TS)\(\d+(\.\d+)?%?:\)$|` +
		`^(TM|WM|PR|TC|TS)\(:\d+(\.\d+)?%?\)$`)

// standardStatistics are the non-percentile statistics CloudWatch accepts for a
// metric-query stat. A metric-query stat may be one of these or a percentile,
// so the value set is checked in code alongside the percentile pattern.
var standardStatistics = map[string]bool{
	"SampleCount": true,
	"Average":     true,
	"Sum":         true,
	"Minimum":     true,
	"Maximum":     true,
}

// ec2AutomatePattern matches the EC2-automate action ARNs CloudWatch accepts in
// an alarm action list in addition to ordinary service ARNs. It is checked in
// code because an action value is validated against a pattern, not a value set.
var ec2AutomatePattern = regexp.MustCompile(
	`^arn:[\w-]+:(automate|swf):[\w-]+:.*:.+$`)

// MetricAlarmResource is a CloudWatch metric alarm, modeling AWS::CloudWatch::Alarm. A
// single PutMetricAlarm upsert reconciles every writable property for both
// create and update; the alarm name is fixed at creation and is the identity,
// so a change to it replaces the alarm, while every other property is rewritten
// in place. The alarm watches either a single metric (metric-name with a
// namespace, statistic or extended-statistic, dimensions, period, and unit) or
// the result of a metric-math expression (the metric-query array); the two
// forms are mutually exclusive. Tags are reconciled separately by ARN.
//
// Several rules cannot be expressed as constraints and are checked in code:
// alarm-name is 1-255 and alarm-description 0-1024 characters (the length
// function counts bytes, not characters); a namespace is 1-255 characters and
// cannot contain a colon; an extended-statistic and a metric-query stat must
// match a percentile or statistic pattern; a period is 10, 20, or 30 seconds
// or a multiple of 60 (a metric-query period also allows 1 and 5); and an
// action ARN must be an ordinary ARN or an EC2-automate ARN.
type MetricAlarmResource struct {
	AlarmName                        string                    `ub:"alarm-name"`
	ActionsEnabled                   *bool                     `ub:"actions-enabled"`
	AlarmActions                     *[]string                 `ub:"alarm-actions"`
	OKActions                        *[]string                 `ub:"ok-actions"`
	InsufficientDataActions          *[]string                 `ub:"insufficient-data-actions"`
	AlarmDescription                 *string                   `ub:"alarm-description"`
	ComparisonOperator               *string                   `ub:"comparison-operator"`
	DatapointsToAlarm                *int64                    `ub:"datapoints-to-alarm"`
	Dimensions                       *map[string]string        `ub:"dimensions"`
	EvaluateLowSampleCountPercentile *string                   `ub:"evaluate-low-sample-count-percentile"`
	EvaluationPeriods                *int64                    `ub:"evaluation-periods"`
	ExtendedStatistic                *string                   `ub:"extended-statistic"`
	MetricName                       *string                   `ub:"metric-name"`
	MetricQuery                      *[]MetricAlarmMetricQuery `ub:"metric-query"`
	Namespace                        *string                   `ub:"namespace"`
	Period                           *int64                    `ub:"period"`
	Statistic                        *string                   `ub:"statistic"`
	Threshold                        *float64                  `ub:"threshold"`
	ThresholdMetricId                *string                   `ub:"threshold-metric-id"`
	TreatMissingData                 *string                   `ub:"treat-missing-data"`
	Unit                             *string                   `ub:"unit"`
	Tags                             *map[string]string        `ub:"tags"`
}

// MetricAlarmResourceOutput holds the values CloudWatch computes for an alarm. Arn is
// the alarm's stable handle: it is the tag identifier and the value Delete keys
// off so a replace removes the old alarm rather than orphaning it, and it is
// settled only after the alarm is stored, so Create reads it back after the
// put. AlarmName is the identity used to find and delete the alarm.
// EvaluateLowSampleCountPercentile is server-filled and read back so consumers
// see the value CloudWatch applied.
type MetricAlarmResourceOutput struct {
	Arn                              string `ub:"arn"`
	AlarmName                        string `ub:"alarm-name"`
	EvaluateLowSampleCountPercentile string `ub:"evaluate-low-sample-count-percentile"`
}

func (r *MetricAlarmResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs CloudWatch fixes when an alarm is created. The
// alarm name is the alarm's identity and cannot be changed in place, so a
// change to it requires a new alarm; every other input is rewritten by Update.
func (r *MetricAlarmResource) ReplaceFields() []string {
	return []string{"alarm-name"}
}

// Constraints declares the rules CloudWatch places on an alarm's inputs. The
// central rule is the metric form: an alarm watches exactly one of a single
// metric (metric-name) or a metric-math expression (metric-query), and the
// metric-query form forbids the single-metric fields. A single metric uses
// either a statistic or an extended-statistic but not both, and a threshold is
// either a static threshold or an anomaly-detection threshold-metric-id but not
// both. The comparison-operator and evaluation-periods are required for either
// form, and a single-metric alarm must set exactly one of statistic or
// extended-statistic. The enums fix their value sets, and the counts and bounds
// are enforced. The pattern-based and divisible-by rules are checked in code
// (see the type doc) because the constraint vocabulary cannot express them.
func (r MetricAlarmResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Any(
			constraint.All(
				constraint.Present(r.MetricName),
				constraint.Not(constraint.NotEmpty(r.MetricQuery))),
			constraint.All(
				constraint.Absent(r.MetricName),
				constraint.NotEmpty(r.MetricQuery)))).
			Message("exactly one of metric-name or metric-query is required"),
		constraint.When(constraint.NotEmpty(r.MetricQuery)).
			Require(constraint.Absent(r.Namespace),
				constraint.Not(constraint.NotEmpty(r.Dimensions)),
				constraint.Absent(r.Period),
				constraint.Absent(r.Unit),
				constraint.Absent(r.Statistic),
				constraint.Absent(r.ExtendedStatistic)).
			Message("metric-query cannot combine with single-metric fields"),
		constraint.AtMostOneOf(r.Statistic, r.ExtendedStatistic),
		constraint.ExactlyOneOf(r.Threshold, r.ThresholdMetricId),
		constraint.When(constraint.Present(r.MetricName)).
			Require(constraint.Present(r.ComparisonOperator),
				constraint.Present(r.EvaluationPeriods)).
			Message("comparison-operator and evaluation-periods are required " +
				"when metric-name is set"),
		constraint.When(constraint.NotEmpty(r.MetricQuery)).
			Require(constraint.Present(r.ComparisonOperator),
				constraint.Present(r.EvaluationPeriods)).
			Message("comparison-operator and evaluation-periods are required " +
				"when metric-query is set"),
		constraint.When(constraint.Present(r.MetricName)).
			Require(constraint.Any(
				constraint.All(constraint.Present(r.Statistic),
					constraint.Absent(r.ExtendedStatistic)),
				constraint.All(constraint.Absent(r.Statistic),
					constraint.Present(r.ExtendedStatistic)))).
			Message("exactly one of statistic or extended-statistic is required " +
				"when metric-name is set"),
		constraint.When(constraint.Present(r.ComparisonOperator)).
			Require(constraint.OneOf(r.ComparisonOperator,
				"GreaterThanOrEqualToThreshold", "GreaterThanThreshold",
				"LessThanThreshold", "LessThanOrEqualToThreshold",
				"LessThanLowerOrGreaterThanUpperThreshold", "LessThanLowerThreshold",
				"GreaterThanUpperThreshold")).
			Message("comparison-operator must be a valid CloudWatch comparison operator"),
		constraint.When(constraint.Present(r.Statistic)).
			Require(constraint.OneOf(r.Statistic,
				"SampleCount", "Average", "Sum", "Minimum", "Maximum")).
			Message("statistic must be SampleCount, Average, Sum, Minimum, or Maximum"),
		constraint.When(constraint.Present(r.Unit)).
			Require(constraint.OneOf(r.Unit,
				"Seconds", "Microseconds", "Milliseconds", "Bytes", "Kilobytes",
				"Megabytes", "Gigabytes", "Terabytes", "Bits", "Kilobits", "Megabits",
				"Gigabits", "Terabits", "Percent", "Count", "Bytes/Second",
				"Kilobytes/Second", "Megabytes/Second", "Gigabytes/Second",
				"Terabytes/Second", "Bits/Second", "Kilobits/Second", "Megabits/Second",
				"Gigabits/Second", "Terabits/Second", "Count/Second", "None")).
			Message("unit must be a valid CloudWatch standard unit"),
		constraint.When(constraint.Present(r.TreatMissingData)).
			Require(constraint.OneOf(r.TreatMissingData,
				"breaching", "notBreaching", "ignore", "missing")).
			Message("treat-missing-data must be breaching, notBreaching, ignore, or missing"),
		constraint.When(constraint.Present(r.EvaluateLowSampleCountPercentile)).
			Require(constraint.OneOf(r.EvaluateLowSampleCountPercentile, "evaluate", "ignore")).
			Message("evaluate-low-sample-count-percentile must be evaluate or ignore"),
		constraint.When(constraint.Present(r.DatapointsToAlarm)).
			Require(constraint.AtLeast(r.DatapointsToAlarm, 1)).
			Message("datapoints-to-alarm must be at least 1"),
		constraint.When(constraint.Present(r.EvaluationPeriods)).
			Require(constraint.AtLeast(r.EvaluationPeriods, 1)).
			Message("evaluation-periods must be at least 1"),
		constraint.When(constraint.NotEmpty(r.OKActions)).
			Require(constraint.MaxItems(r.OKActions, 5)).
			Message("ok-actions allows at most 5 actions"),
		constraint.When(constraint.NotEmpty(r.InsufficientDataActions)).
			Require(constraint.MaxItems(r.InsufficientDataActions, 5)).
			Message("insufficient-data-actions allows at most 5 actions"),
		constraint.ForEach(r.MetricQuery, func(q MetricAlarmMetricQuery) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.AtMostOneOf(q.Expression, q.Metric),
			}
		}),
	}
}

func (r *MetricAlarmResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*MetricAlarmResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	in := r.expandPutMetricAlarmInput()
	in.Tags = metricAlarmTags(ptr.Value(r.Tags))
	// Some partitions, such as the ISO partitions, cannot tag an alarm as it is
	// created. When the tagged put fails for that reason, repeat the put without
	// tags and apply them with a separate call below.
	taggedSeparately := false
	_, err = client.PutMetricAlarm(ctx, in)
	if err != nil && in.Tags != nil &&
		partition.UnsupportedOperation(metricAlarmRegion(client), err) {
		in.Tags = nil
		taggedSeparately = true
		_, err = client.PutMetricAlarm(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("put metric alarm: %w", err)
	}
	// PutMetricAlarm returns an empty body and the ARN settles only once the
	// alarm is stored, so the freshly-read alarm provides the ARN and the
	// tag handle.
	out, err := r.read(ctx, client)
	if err != nil {
		return nil, err
	}
	if taggedSeparately && len(ptr.Value(r.Tags)) > 0 {
		if err := r.syncTags(ctx, client, out.Arn); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *MetricAlarmResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *MetricAlarmResourceOutput,
) (*MetricAlarmResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read finds the alarm by name and returns its computed outputs. CloudWatch
// does not error on a missing alarm; DescribeAlarms returns an empty result, so
// zero alarms maps to runtime.ErrNotFound and a plan recreates it.
func (r *MetricAlarmResource) read(
	ctx context.Context, client *cloudwatch.Client,
) (*MetricAlarmResourceOutput, error) {
	resp, err := client.DescribeAlarms(ctx, &cloudwatch.DescribeAlarmsInput{
		AlarmNames: []string{r.AlarmName},
		AlarmTypes: []cloudwatchtypes.AlarmType{cloudwatchtypes.AlarmTypeMetricAlarm},
	})
	if err != nil {
		return nil, fmt.Errorf("describe alarms: %w", err)
	}
	if len(resp.MetricAlarms) == 0 {
		return nil, runtime.ErrNotFound
	}
	alarm := resp.MetricAlarms[0]
	return &MetricAlarmResourceOutput{
		Arn:                              aws.ToString(alarm.AlarmArn),
		AlarmName:                        aws.ToString(alarm.AlarmName),
		EvaluateLowSampleCountPercentile: aws.ToString(alarm.EvaluateLowSampleCountPercentile),
	}, nil
}

func (r *MetricAlarmResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[MetricAlarmResource, *MetricAlarmResourceOutput],
) (*MetricAlarmResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	// PutMetricAlarm is a full-replace upsert, not a partial patch, so it runs
	// only when something other than tags changed; otherwise it would rewrite
	// the whole alarm on a tag-only apply. Tags reconcile separately by ARN.
	if r.changedExceptTags(prior.Inputs) {
		in := r.expandPutMetricAlarmInput()
		// On an update PutMetricAlarm ignores any tags in the request, so they
		// are left off and reconciled through TagResource/UntagResource below.
		if _, err := client.PutMetricAlarm(ctx, in); err != nil {
			return nil, fmt.Errorf("put metric alarm: %w", err)
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, prior.Outputs.Arn); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *MetricAlarmResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *MetricAlarmResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// On a replace, Delete receives the prior alarm's outputs while the receiver
	// holds the new inputs, so keying off the prior name removes the exact alarm
	// that was created rather than orphaning it under a changed name.
	name := prior.AlarmName
	if name == "" {
		name = r.AlarmName
	}
	_, err = client.DeleteAlarms(ctx, &cloudwatch.DeleteAlarmsInput{
		AlarmNames: []string{name},
	})
	if err != nil {
		// An alarm already gone reports as a ResourceNotFoundException, which
		// counts as deleted.
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete alarms: %w", err)
	}
	return nil
}

// expandPutMetricAlarmInput builds the upsert request from the inputs. The
// alarm name, actions, description, dimensions, and the metric form are sent as
// given; treat-missing-data and actions-enabled are always sent, defaulting to
// missing and true when omitted. The threshold is sent as the
// anomaly-detection threshold-metric-id when that is set, otherwise as the
// static threshold, mirroring how the two are mutually exclusive. Tags are not
// set here; the caller fills them for a create and leaves them off an update.
func (r *MetricAlarmResource) expandPutMetricAlarmInput() *cloudwatch.PutMetricAlarmInput {
	in := &cloudwatch.PutMetricAlarmInput{
		AlarmName:                        aws.String(r.AlarmName),
		ActionsEnabled:                   aws.Bool(r.actionsEnabled()),
		AlarmActions:                     ptr.Value(r.AlarmActions),
		OKActions:                        ptr.Value(r.OKActions),
		InsufficientDataActions:          ptr.Value(r.InsufficientDataActions),
		AlarmDescription:                 r.AlarmDescription,
		DatapointsToAlarm:                ptr.Int32(r.DatapointsToAlarm),
		Dimensions:                       expandDimensions(ptr.Value(r.Dimensions)),
		EvaluateLowSampleCountPercentile: r.EvaluateLowSampleCountPercentile,
		EvaluationPeriods:                ptr.Int32(r.EvaluationPeriods),
		ExtendedStatistic:                r.ExtendedStatistic,
		MetricName:                       r.MetricName,
		Metrics:                          expandMetricQuery(ptr.Value(r.MetricQuery)),
		Namespace:                        r.Namespace,
		Period:                           ptr.Int32(r.Period),
		TreatMissingData:                 aws.String(r.treatMissingData()),
	}
	if r.ComparisonOperator != nil {
		in.ComparisonOperator = cloudwatchtypes.ComparisonOperator(*r.ComparisonOperator)
	}
	if r.Statistic != nil {
		in.Statistic = cloudwatchtypes.Statistic(*r.Statistic)
	}
	if r.Unit != nil {
		in.Unit = cloudwatchtypes.StandardUnit(*r.Unit)
	}
	// The static threshold and the anomaly-detection threshold are mutually
	// exclusive, so the metric-id is sent when set, otherwise the static value.
	if r.ThresholdMetricId != nil {
		in.ThresholdMetricId = r.ThresholdMetricId
	} else {
		in.Threshold = r.Threshold
	}
	return in
}

// actionsEnabled returns whether alarm actions run, defaulting to true when
// omitted so the always-sent value matches CloudWatch's own default.
func (r *MetricAlarmResource) actionsEnabled() bool {
	if r.ActionsEnabled == nil {
		return actionsEnabledDefault
	}
	return *r.ActionsEnabled
}

// treatMissingData returns how the alarm handles missing data points,
// defaulting to missing when omitted so the always-sent value matches
// CloudWatch's own default.
func (r *MetricAlarmResource) treatMissingData() string {
	if r.TreatMissingData == nil {
		return treatMissingDataDefault
	}
	return *r.TreatMissingData
}

// syncTags reconciles the alarm's tags with the desired set, reading the live
// tags through ListTagsForResource and writing changes with TagResource and
// UntagResource. CloudWatch addresses an alarm's tags by its ARN.
func (r *MetricAlarmResource) syncTags(
	ctx context.Context, client *cloudwatch.Client, arn string,
) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx,
				&cloudwatch.ListTagsForResourceInput{ResourceARN: aws.String(arn)})
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			current := map[string]string{}
			for _, t := range resp.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &cloudwatch.TagResourceInput{
				ResourceARN: aws.String(arn),
				Tags:        metricAlarmTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &cloudwatch.UntagResourceInput{
				ResourceARN: aws.String(arn),
				TagKeys:     remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}

// changedExceptTags reports whether any input other than tags differs from the
// prior apply. PutMetricAlarm is a full-replace upsert, so it runs only on a
// real change; a tag-only change is reconciled by the separate tag sync. Both
// sides are compared by value with tags cleared, so the tag map alone never
// triggers the upsert.
func (r *MetricAlarmResource) changedExceptTags(prior MetricAlarmResource) bool {
	before := prior
	before.Tags = nil
	after := *r
	after.Tags = nil
	return runtime.Changed(before, after)
}

// metricAlarmRegion returns the region the client is configured for, used to
// decide whether a create that sends tags must repeat without them on a
// partition that cannot tag an alarm at create time.
func metricAlarmRegion(client *cloudwatch.Client) string {
	return client.Options().Region
}

// metricAlarmTags converts a desired tag map into the CloudWatch SDK tag list,
// ordered by key so the request is deterministic, or nil when empty.
func metricAlarmTags(tags map[string]string) []cloudwatchtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]cloudwatchtypes.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, cloudwatchtypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}

// validate checks the rules the constraint vocabulary cannot express: the
// character-count limits (the length function counts bytes, not characters),
// the colon-free namespace, the percentile and statistic patterns, the
// divisible-by period values, and the action ARN forms. CloudWatch enforces
// these as well; checking them here turns a server rejection into a clear error.
func (r *MetricAlarmResource) validate() error {
	if n := len(r.AlarmName); n < 1 || n > 255 {
		return fmt.Errorf("alarm-name must be 1 to 255 characters, got %d", n)
	}
	if r.AlarmDescription != nil && len(*r.AlarmDescription) > 1024 {
		return fmt.Errorf("alarm-description must be at most 1024 characters")
	}
	if err := validateNamespace(r.Namespace); err != nil {
		return err
	}
	if r.ExtendedStatistic != nil && !percentilePattern.MatchString(*r.ExtendedStatistic) {
		return fmt.Errorf(
			"extended-statistic %q is not a valid percentile or extended statistic",
			*r.ExtendedStatistic)
	}
	if err := validatePeriod("period", r.Period, false); err != nil {
		return err
	}
	if err := validateActions("alarm-actions", ptr.Value(r.AlarmActions)); err != nil {
		return err
	}
	if err := validateActions("ok-actions", ptr.Value(r.OKActions)); err != nil {
		return err
	}
	if err := validateActions("insufficient-data-actions", ptr.Value(r.InsufficientDataActions)); err != nil {
		return err
	}
	return r.validateMetricQuery()
}

// validateMetricQuery checks the per-element rules of the metric-math array that
// cannot be expressed as constraints: each element period and each metric block
// period, the metric block namespace, and the metric block statistic, which may
// be a standard statistic or a percentile.
func (r *MetricAlarmResource) validateMetricQuery() error {
	for i, q := range ptr.Value(r.MetricQuery) {
		if err := validatePeriod(
			fmt.Sprintf("metric-query[%d].period", i), q.Period, true); err != nil {
			return err
		}
		if q.Metric == nil {
			continue
		}
		field := fmt.Sprintf("metric-query[%d].metric", i)
		if err := validatePeriod(field+".period", q.Metric.Period, true); err != nil {
			return err
		}
		if err := validateNamespace(q.Metric.Namespace); err != nil {
			return err
		}
		if q.Metric.Stat != nil && !validStat(*q.Metric.Stat) {
			return fmt.Errorf("%s.stat %q is not a valid statistic or percentile",
				field, *q.Metric.Stat)
		}
	}
	return nil
}

// validateNamespace checks a metric namespace is 1 to 255 characters and free
// of a colon. An unset namespace passes; it is required only for the
// single-metric form, which the constraints already cover.
func validateNamespace(namespace *string) error {
	if namespace == nil {
		return nil
	}
	n := len(*namespace)
	if n < 1 || n > 255 {
		return fmt.Errorf("namespace must be 1 to 255 characters, got %d", n)
	}
	if strings.Contains(*namespace, ":") {
		return fmt.Errorf("namespace %q must not contain a colon", *namespace)
	}
	return nil
}

// validatePeriod checks a period is one of CloudWatch's accepted values: 10,
// 20, or 30 seconds, or any multiple of 60. A metric-query period also accepts
// 1 and 5 for high-resolution metrics, selected by highResolution. An unset
// period passes; it is required only where the constraints already cover it.
func validatePeriod(field string, period *int64, highResolution bool) error {
	if period == nil {
		return nil
	}
	p := *period
	if p%60 == 0 && p > 0 {
		return nil
	}
	if p == 10 || p == 20 || p == 30 {
		return nil
	}
	if highResolution && (p == 1 || p == 5) {
		return nil
	}
	if highResolution {
		return fmt.Errorf(
			"%s must be 1, 5, 10, 20, 30, or a multiple of 60, got %d", field, p)
	}
	return fmt.Errorf("%s must be 10, 20, 30, or a multiple of 60, got %d", field, p)
}

// validateActions checks each action in a list is an ordinary ARN or an
// EC2-automate ARN, the two forms CloudWatch accepts for an alarm action.
func validateActions(field string, actions []string) error {
	for _, action := range actions {
		if strings.HasPrefix(action, "arn:") && (validArn(action) ||
			ec2AutomatePattern.MatchString(action)) {
			continue
		}
		return fmt.Errorf("%s entry %q must be a valid ARN", field, action)
	}
	return nil
}

// validArn reports whether s has the six-part form of an ARN. It is a coarse
// check; CloudWatch validates the service and resource form itself.
func validArn(s string) bool {
	return len(strings.SplitN(s, ":", 6)) == 6
}

// validStat reports whether a metric-query statistic is one CloudWatch accepts:
// a standard statistic or a percentile or extended statistic.
func validStat(stat string) bool {
	return standardStatistics[stat] || percentilePattern.MatchString(stat)
}
