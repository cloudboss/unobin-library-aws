package cloudwatchlogs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatchlogs "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	metricFilterNameMaxBytes          = 512
	metricFilterPatternMaxRunes       = 1024
	metricTransformationNameMaxBytes  = 255
	metricTransformationValueMaxBytes = 100
	metricFilterDefaultUnit           = "None"
	metricFilterLockKeyPrefix         = "log-group-"
)

var (
	metricFilterNameRe          = regexp.MustCompile(`^[^:*]+$`)
	metricTransformationNameRe  = regexp.MustCompile(`^[^:*$]*$`)
	metricFilterLogGroupMutexes sync.Map
)

// MetricFilterResource extracts metric observations from matching CloudWatch Logs
// events. PutMetricFilter is an upsert, so Create and Update use the same call
// and Read then returns the described server state. The filter name and log
// group name are the identity and force replacement; the pattern, transformed
// log flag, and single metric-transformation block update in place.
type MetricFilterResource struct {
	FilterName             string                           `ub:"filter-name"`
	LogGroupName           string                           `ub:"log-group-name"`
	FilterPattern          string                           `ub:"filter-pattern"`
	ApplyOnTransformedLogs *bool                            `ub:"apply-on-transformed-logs"`
	MetricTransformation   MetricFilterMetricTransformation `ub:"metric-transformation"`
}

// MetricFilterMetricTransformation is the one metric transformation a metric
// filter emits. Unit uses None before the request is built. Empty metric-name,
// metric-namespace, and metric-value are omitted from the SDK input so AWS
// reports the missing required member.
type MetricFilterMetricTransformation struct {
	DefaultValue    *float64           `ub:"default-value"`
	Dimensions      *map[string]string `ub:"dimensions"`
	MetricName      string             `ub:"metric-name"`
	MetricNamespace string             `ub:"metric-namespace"`
	MetricValue     string             `ub:"metric-value"`
	Unit            *string            `ub:"unit"`
}

// MetricFilterMetricTransformationOutput is the transformation CloudWatch Logs
// reports after read. Unit is always present so references see the service value
// when the input omitted it.
type MetricFilterMetricTransformationOutput struct {
	DefaultValue    *float64           `ub:"default-value"`
	Dimensions      *map[string]string `ub:"dimensions"`
	MetricName      string             `ub:"metric-name"`
	MetricNamespace string             `ub:"metric-namespace"`
	MetricValue     string             `ub:"metric-value"`
	Unit            string             `ub:"unit"`
}

// MetricFilterResourceOutput records the two-part handle plus the CloudWatch Logs values
// that may differ from omitted inputs. The handle is stored so a replacement
// deletes the prior filter even when the desired name or log group changed.
type MetricFilterResourceOutput struct {
	FilterName             string                                 `ub:"filter-name"`
	LogGroupName           string                                 `ub:"log-group-name"`
	FilterPattern          string                                 `ub:"filter-pattern"`
	ApplyOnTransformedLogs bool                                   `ub:"apply-on-transformed-logs"`
	MetricTransformation   MetricFilterMetricTransformationOutput `ub:"metric-transformation"`
}

func (r *MetricFilterResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that identify a metric filter at the API.
// Changing either names a different filter, so the old one is deleted and a new
// one is created.
func (r *MetricFilterResource) ReplaceFields() []string {
	return []string{"filter-name", "log-group-name"}
}

// Constraints declares the enum rule the schema can express. Length and
// pattern checks run in validate because they need byte counts, UTF-8 character
// counts, or regular expressions.
func (r MetricFilterResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.MetricTransformation.Unit)).
			Require(constraint.OneOf(r.MetricTransformation.Unit,
				"Seconds", "Microseconds", "Milliseconds", "Bytes", "Kilobytes",
				"Megabytes", "Gigabytes", "Terabytes", "Bits", "Kilobits",
				"Megabits", "Gigabits", "Terabits", "Percent", "Count",
				"Bytes/Second", "Kilobytes/Second", "Megabytes/Second",
				"Gigabytes/Second", "Terabytes/Second", "Bits/Second",
				"Kilobits/Second", "Megabits/Second", "Gigabits/Second",
				"Terabits/Second", "Count/Second", "None")).
			Message("metric-transformation unit must be a valid CloudWatch unit"),
	}
}

func (r *MetricFilterResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*MetricFilterResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client); err != nil {
		return nil, err
	}
	return r.read(ctx, client, r.key(nil))
}

func (r *MetricFilterResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *MetricFilterResourceOutput,
) (*MetricFilterResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, r.key(prior))
}

func (r *MetricFilterResource) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[MetricFilterResource, *MetricFilterResourceOutput],
) (*MetricFilterResourceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if r.shouldPut(prior) {
		if err := r.put(ctx, client); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, r.key(prior.Outputs))
}

func (r *MetricFilterResource) Delete(
	ctx context.Context, cfg *awsCfg, prior *MetricFilterResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	key := r.key(prior)
	err = withMetricFilterLogGroupLock(key.LogGroupName, func() error {
		_, err := client.DeleteMetricFilter(ctx, &cloudwatchlogs.DeleteMetricFilterInput{
			FilterName:   aws.String(key.FilterName),
			LogGroupName: aws.String(key.LogGroupName),
		})
		return err
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete metric filter: %w", err)
	}
	return nil
}

func (r *MetricFilterResource) put(
	ctx context.Context, client *cloudwatchlogs.Client,
) error {
	if err := r.validate(); err != nil {
		return err
	}
	in := r.putInput()
	err := withMetricFilterLogGroupLock(r.LogGroupName, func() error {
		_, err := client.PutMetricFilter(ctx, in)
		return err
	})
	if err != nil {
		return fmt.Errorf("put metric filter: %w", err)
	}
	return nil
}

func (r *MetricFilterResource) putInput() *cloudwatchlogs.PutMetricFilterInput {
	return &cloudwatchlogs.PutMetricFilterInput{
		FilterName:             aws.String(r.FilterName),
		LogGroupName:           aws.String(r.LogGroupName),
		FilterPattern:          aws.String(strings.TrimSpace(r.FilterPattern)),
		ApplyOnTransformedLogs: aws.ToBool(r.ApplyOnTransformedLogs),
		MetricTransformations: []cloudwatchlogstypes.MetricTransformation{
			r.MetricTransformation.toSDK(),
		},
	}
}

func (m MetricFilterMetricTransformation) toSDK() cloudwatchlogstypes.MetricTransformation {
	out := cloudwatchlogstypes.MetricTransformation{
		DefaultValue: m.DefaultValue,
		Unit:         cloudwatchlogstypes.StandardUnit(effectiveMetricFilterUnit(m.Unit)),
	}
	if m.Dimensions != nil && len(*m.Dimensions) > 0 {
		out.Dimensions = *m.Dimensions
	}
	if m.MetricName != "" {
		out.MetricName = aws.String(m.MetricName)
	}
	if m.MetricNamespace != "" {
		out.MetricNamespace = aws.String(m.MetricNamespace)
	}
	if m.MetricValue != "" {
		out.MetricValue = aws.String(m.MetricValue)
	}
	return out
}

func (r *MetricFilterResource) read(
	ctx context.Context, client *cloudwatchlogs.Client, key metricFilterKey,
) (*MetricFilterResourceOutput, error) {
	var match *cloudwatchlogstypes.MetricFilter
	pager := cloudwatchlogs.NewDescribeMetricFiltersPaginator(client,
		&cloudwatchlogs.DescribeMetricFiltersInput{
			LogGroupName:     aws.String(key.LogGroupName),
			FilterNamePrefix: aws.String(key.FilterName),
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe metric filters: %w", err)
		}
		for i := range page.MetricFilters {
			if aws.ToString(page.MetricFilters[i].FilterName) == key.FilterName {
				match = &page.MetricFilters[i]
				break
			}
		}
		if match != nil {
			break
		}
	}
	if match == nil {
		return nil, runtime.ErrNotFound
	}
	return &MetricFilterResourceOutput{
		FilterName:             key.FilterName,
		LogGroupName:           key.LogGroupName,
		FilterPattern:          aws.ToString(match.FilterPattern),
		ApplyOnTransformedLogs: match.ApplyOnTransformedLogs,
		MetricTransformation:   metricFilterTransformationOutput(match.MetricTransformations),
	}, nil
}

func (r *MetricFilterResource) shouldPut(
	prior runtime.Prior[MetricFilterResource, *MetricFilterResourceOutput],
) bool {
	return r.mutableInputChanged(prior.Inputs) || r.managedOutputDrifted(prior.Observed)
}

func (r *MetricFilterResource) mutableInputChanged(prior MetricFilterResource) bool {
	return metricFilterPatternChanged(prior.FilterPattern, r.FilterPattern) ||
		metricFilterBoolChanged(prior.ApplyOnTransformedLogs, r.ApplyOnTransformedLogs) ||
		r.metricTransformationChanged(prior.MetricTransformation)
}

func (r *MetricFilterResource) metricTransformationChanged(
	prior MetricFilterMetricTransformation,
) bool {
	current := r.MetricTransformation
	return runtime.Changed(prior.DefaultValue, current.DefaultValue) ||
		!metricFilterDimensionsEqual(prior.Dimensions, current.Dimensions) ||
		runtime.Changed(prior.MetricName, current.MetricName) ||
		runtime.Changed(prior.MetricNamespace, current.MetricNamespace) ||
		runtime.Changed(prior.MetricValue, current.MetricValue) ||
		effectiveMetricFilterUnit(prior.Unit) != effectiveMetricFilterUnit(current.Unit)
}

func (r *MetricFilterResource) managedOutputDrifted(observed *MetricFilterResourceOutput) bool {
	if observed == nil {
		return false
	}
	if observed.FilterPattern != strings.TrimSpace(r.FilterPattern) {
		return true
	}
	if r.ApplyOnTransformedLogs != nil &&
		observed.ApplyOnTransformedLogs != *r.ApplyOnTransformedLogs {
		return true
	}
	return !metricFilterTransformationMatchesDesired(
		observed.MetricTransformation, r.MetricTransformation)
}

func (r *MetricFilterResource) key(prior *MetricFilterResourceOutput) metricFilterKey {
	if prior != nil && prior.FilterName != "" && prior.LogGroupName != "" {
		return metricFilterKey{
			FilterName:   prior.FilterName,
			LogGroupName: prior.LogGroupName,
		}
	}
	return metricFilterKey{
		FilterName:   r.FilterName,
		LogGroupName: r.LogGroupName,
	}
}

func (r *MetricFilterResource) validate() error {
	if err := validateMetricFilterLogGroupName(r.LogGroupName); err != nil {
		return err
	}
	if err := validateMetricFilterName(r.FilterName); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(r.FilterPattern); n > metricFilterPatternMaxRunes {
		return fmt.Errorf("filter-pattern must be at most %d characters, got %d",
			metricFilterPatternMaxRunes, n)
	}
	return r.MetricTransformation.validate()
}

func (m MetricFilterMetricTransformation) validate() error {
	if err := validateMetricTransformationName("metric-name", m.MetricName); err != nil {
		return err
	}
	if err := validateMetricTransformationName(
		"metric-namespace", m.MetricNamespace,
	); err != nil {
		return err
	}
	if n := len(m.MetricValue); n > metricTransformationValueMaxBytes {
		return fmt.Errorf("metric-value must be at most %d bytes, got %d",
			metricTransformationValueMaxBytes, n)
	}
	if !validMetricFilterUnit(effectiveMetricFilterUnit(m.Unit)) {
		return errors.New("unit must be a valid CloudWatch unit")
	}
	return nil
}

func validateMetricFilterLogGroupName(name string) error {
	if len(name) > logGroupNameMaxLen {
		return fmt.Errorf("log-group-name must be at most %d characters", logGroupNameMaxLen)
	}
	if !logGroupNameRe.MatchString(name) {
		return errors.New(
			"log-group-name must contain only letters, digits, underscore, hyphen, " +
				"forward slash, period, or the hash sign")
	}
	return nil
}

func validateMetricFilterName(name string) error {
	if n := len(name); n < 1 || n > metricFilterNameMaxBytes {
		return fmt.Errorf("filter-name must be 1 to %d bytes, got %d",
			metricFilterNameMaxBytes, n)
	}
	if !metricFilterNameRe.MatchString(name) {
		return errors.New("filter-name must not contain colon or asterisk")
	}
	return nil
}

func validateMetricTransformationName(field, name string) error {
	if n := len(name); n > metricTransformationNameMaxBytes {
		return fmt.Errorf("%s must be at most %d bytes, got %d",
			field, metricTransformationNameMaxBytes, n)
	}
	if !metricTransformationNameRe.MatchString(name) {
		return fmt.Errorf("%s must not contain colon, asterisk, or dollar sign", field)
	}
	return nil
}

func metricFilterTransformationOutput(
	transformations []cloudwatchlogstypes.MetricTransformation,
) MetricFilterMetricTransformationOutput {
	if len(transformations) == 0 {
		return MetricFilterMetricTransformationOutput{Unit: metricFilterDefaultUnit}
	}
	mt := transformations[0]
	out := MetricFilterMetricTransformationOutput{
		DefaultValue:    mt.DefaultValue,
		MetricName:      aws.ToString(mt.MetricName),
		MetricNamespace: aws.ToString(mt.MetricNamespace),
		MetricValue:     aws.ToString(mt.MetricValue),
		Unit:            metricFilterOutputUnit(mt.Unit),
	}
	if len(mt.Dimensions) > 0 {
		dimensions := maps.Clone(mt.Dimensions)
		out.Dimensions = &dimensions
	}
	return out
}

func effectiveMetricFilterUnit(unit *string) string {
	if unit == nil {
		return metricFilterDefaultUnit
	}
	return *unit
}

func metricFilterOutputUnit(unit cloudwatchlogstypes.StandardUnit) string {
	if unit == "" {
		return metricFilterDefaultUnit
	}
	return string(unit)
}

func metricFilterDimensionsEqual(a, b *map[string]string) bool {
	if a == nil || len(*a) == 0 {
		return b == nil || len(*b) == 0
	}
	if b == nil {
		return len(*a) == 0
	}
	return maps.Equal(*a, *b)
}

func metricFilterPatternChanged(prior, current string) bool {
	return strings.TrimSpace(prior) != strings.TrimSpace(current)
}

func metricFilterTransformationMatchesDesired(
	observed MetricFilterMetricTransformationOutput,
	desired MetricFilterMetricTransformation,
) bool {
	return !runtime.Changed(observed.DefaultValue, desired.DefaultValue) &&
		metricFilterDimensionsEqual(observed.Dimensions, desired.Dimensions) &&
		observed.MetricName == desired.MetricName &&
		observed.MetricNamespace == desired.MetricNamespace &&
		observed.MetricValue == desired.MetricValue &&
		observed.Unit == effectiveMetricFilterUnit(desired.Unit)
}

func metricFilterBoolChanged(prior, current *bool) bool {
	return aws.ToBool(prior) != aws.ToBool(current)
}

func validMetricFilterUnit(unit string) bool {
	switch cloudwatchlogstypes.StandardUnit(unit) {
	case cloudwatchlogstypes.StandardUnitSeconds,
		cloudwatchlogstypes.StandardUnitMicroseconds,
		cloudwatchlogstypes.StandardUnitMilliseconds,
		cloudwatchlogstypes.StandardUnitBytes,
		cloudwatchlogstypes.StandardUnitKilobytes,
		cloudwatchlogstypes.StandardUnitMegabytes,
		cloudwatchlogstypes.StandardUnitGigabytes,
		cloudwatchlogstypes.StandardUnitTerabytes,
		cloudwatchlogstypes.StandardUnitBits,
		cloudwatchlogstypes.StandardUnitKilobits,
		cloudwatchlogstypes.StandardUnitMegabits,
		cloudwatchlogstypes.StandardUnitGigabits,
		cloudwatchlogstypes.StandardUnitTerabits,
		cloudwatchlogstypes.StandardUnitPercent,
		cloudwatchlogstypes.StandardUnitCount,
		cloudwatchlogstypes.StandardUnitBytesSecond,
		cloudwatchlogstypes.StandardUnitKilobytesSecond,
		cloudwatchlogstypes.StandardUnitMegabytesSecond,
		cloudwatchlogstypes.StandardUnitGigabytesSecond,
		cloudwatchlogstypes.StandardUnitTerabytesSecond,
		cloudwatchlogstypes.StandardUnitBitsSecond,
		cloudwatchlogstypes.StandardUnitKilobitsSecond,
		cloudwatchlogstypes.StandardUnitMegabitsSecond,
		cloudwatchlogstypes.StandardUnitGigabitsSecond,
		cloudwatchlogstypes.StandardUnitTerabitsSecond,
		cloudwatchlogstypes.StandardUnitCountSecond,
		cloudwatchlogstypes.StandardUnitNone:
		return true
	}
	return false
}

type metricFilterKey struct {
	FilterName   string
	LogGroupName string
}

func withMetricFilterLogGroupLock(logGroupName string, fn func() error) error {
	lockKey := metricFilterLockKeyPrefix + logGroupName
	actual, _ := metricFilterLogGroupMutexes.LoadOrStore(lockKey, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	return fn()
}
