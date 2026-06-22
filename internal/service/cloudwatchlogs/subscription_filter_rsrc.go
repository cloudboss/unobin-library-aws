package cloudwatchlogs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	cloudwatchlogs "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

var (
	subscriptionFilterARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	subscriptionFilterARNRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	subscriptionFilterARNAccountPattern   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`)
)

const (
	subscriptionFilterPutRetryTimeout = 5 * time.Minute
	distributionByLogStream           = "ByLogStream"
	distributionRandom                = "Random"
)

// SubscriptionFilter sends events from one CloudWatch Logs log group to a
// Lambda function, Kinesis stream, Firehose stream, or Logs destination. The
// log group, filter name, and destination are fixed once the filter is made;
// the pattern, distribution, system fields, role, and transformed-log flag are
// reconciled in place by PutSubscriptionFilter.
type SubscriptionFilter struct {
	DestinationArn         string   `ub:"destination-arn"`
	LogGroupName           string   `ub:"log-group-name"`
	Name                   string   `ub:"name"`
	FilterPattern          string   `ub:"filter-pattern"`
	Distribution           string   `ub:"distribution"`
	EmitSystemFields       []string `ub:"emit-system-fields"`
	FieldSelectionCriteria *string  `ub:"field-selection-criteria"`
	RoleArn                *string  `ub:"role-arn"`
	ApplyOnTransformedLogs *bool    `ub:"apply-on-transformed-logs"`
}

// SubscriptionFilterOutput records the two-part handle plus values the service
// may fill. The handle is stored so a replacement can delete the prior filter
// even when the new configuration names a different log group or filter name.
type SubscriptionFilterOutput struct {
	LogGroupName           string  `ub:"log-group-name"`
	Name                   string  `ub:"name"`
	ApplyOnTransformedLogs bool    `ub:"apply-on-transformed-logs"`
	RoleArn                *string `ub:"role-arn"`
}

func (r *SubscriptionFilter) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that identify a subscription filter at the
// API. Changing any of them names a different filter, so the old filter is
// deleted and a new one is created.
func (r *SubscriptionFilter) ReplaceFields() []string {
	return []string{"destination-arn", "log-group-name", "name"}
}

// Defaults gives distribution the API default and marks the system-field list optional.
func (r SubscriptionFilter) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.Distribution, "ByLogStream"),
		defaults.Optional(r.EmitSystemFields),
	}
}

// Constraints declares the enum rules the schema can express. Length and ARN
// checks run in validate because they need byte counts and ARN parsing.
func (r SubscriptionFilter) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.Distribution)).
			Require(constraint.OneOf(r.Distribution, "ByLogStream", "Random")).
			Message("distribution must be ByLogStream or Random"),
		constraint.ForEach(r.EmitSystemFields,
			func(v string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(v, "@aws.account", "@aws.region")).
						Message("emit-system-fields entries must be @aws.account or @aws.region"),
				}
			}),
	}
}

func (r *SubscriptionFilter) Create(
	ctx context.Context, cfg *awsCfg,
) (*SubscriptionFilterOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client); err != nil {
		return nil, err
	}
	return r.read(ctx, client, r.key(nil))
}

func (r *SubscriptionFilter) Read(
	ctx context.Context, cfg *awsCfg, prior *SubscriptionFilterOutput,
) (*SubscriptionFilterOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, r.key(prior))
}

func (r *SubscriptionFilter) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[SubscriptionFilter, *SubscriptionFilterOutput],
) (*SubscriptionFilterOutput, error) {
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

func (r *SubscriptionFilter) Delete(
	ctx context.Context, cfg *awsCfg, prior *SubscriptionFilterOutput,
) error {
	// Replacement runs Delete on the desired receiver before Create, so validate the
	// desired input before deleting the prior filter.
	if err := r.validate(); err != nil {
		return err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	key := r.key(prior)
	_, err = client.DeleteSubscriptionFilter(ctx,
		&cloudwatchlogs.DeleteSubscriptionFilterInput{
			LogGroupName: aws.String(key.LogGroupName),
			FilterName:   aws.String(key.Name),
		})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete subscription filter: %w", err)
	}
	return nil
}

func (r *SubscriptionFilter) put(
	ctx context.Context, client *cloudwatchlogs.Client,
) error {
	if err := r.validate(); err != nil {
		return err
	}
	in := r.putInput()
	err := retry.OnError(ctx, isPutSubscriptionFilterRetryable,
		func(ctx context.Context) error {
			_, err := client.PutSubscriptionFilter(ctx, in)
			return err
		}, retry.WithTimeout(subscriptionFilterPutRetryTimeout))
	if err != nil {
		return fmt.Errorf("put subscription filter: %w", err)
	}
	return nil
}

func (r *SubscriptionFilter) putInput() *cloudwatchlogs.PutSubscriptionFilterInput {
	in := &cloudwatchlogs.PutSubscriptionFilterInput{
		DestinationArn: aws.String(r.DestinationArn),
		LogGroupName:   aws.String(r.LogGroupName),
		FilterName:     aws.String(r.Name),
		FilterPattern:  aws.String(r.FilterPattern),
		Distribution: cloudwatchlogstypes.Distribution(
			effectiveDistribution(r.Distribution)),
	}
	if fields := normalizedEmitSystemFields(r.EmitSystemFields); len(fields) > 0 {
		in.EmitSystemFields = fields
	}
	if r.FieldSelectionCriteria != nil {
		in.FieldSelectionCriteria = r.FieldSelectionCriteria
	}
	if roleArn := effectiveOptionalString(r.RoleArn); roleArn != "" {
		in.RoleArn = aws.String(roleArn)
	}
	if r.ApplyOnTransformedLogs != nil {
		in.ApplyOnTransformedLogs = *r.ApplyOnTransformedLogs
	}
	return in
}

func (r *SubscriptionFilter) read(
	ctx context.Context, client *cloudwatchlogs.Client, key subscriptionFilterKey,
) (*SubscriptionFilterOutput, error) {
	var match *cloudwatchlogstypes.SubscriptionFilter
	pager := cloudwatchlogs.NewDescribeSubscriptionFiltersPaginator(client,
		&cloudwatchlogs.DescribeSubscriptionFiltersInput{
			LogGroupName:     aws.String(key.LogGroupName),
			FilterNamePrefix: aws.String(key.Name),
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe subscription filters: %w", err)
		}
		for i := range page.SubscriptionFilters {
			if aws.ToString(page.SubscriptionFilters[i].FilterName) == key.Name {
				match = &page.SubscriptionFilters[i]
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
	return &SubscriptionFilterOutput{
		LogGroupName:           key.LogGroupName,
		Name:                   key.Name,
		ApplyOnTransformedLogs: match.ApplyOnTransformedLogs,
		RoleArn:                nonEmptyStringPtr(match.RoleArn),
	}, nil
}

func (r *SubscriptionFilter) shouldPut(
	prior runtime.Prior[SubscriptionFilter, *SubscriptionFilterOutput],
) bool {
	return r.mutableInputChanged(prior.Inputs) || r.managedOutputDrifted(prior.Observed)
}

func (r *SubscriptionFilter) mutableInputChanged(prior SubscriptionFilter) bool {
	return runtime.Changed(prior.FilterPattern, r.FilterPattern) ||
		effectiveDistribution(prior.Distribution) != effectiveDistribution(r.Distribution) ||
		!sameStringSet(prior.EmitSystemFields, r.EmitSystemFields) ||
		runtime.Changed(prior.FieldSelectionCriteria, r.FieldSelectionCriteria) ||
		effectiveOptionalString(prior.RoleArn) != effectiveOptionalString(r.RoleArn) ||
		runtime.Changed(prior.ApplyOnTransformedLogs, r.ApplyOnTransformedLogs)
}

func (r *SubscriptionFilter) managedOutputDrifted(observed *SubscriptionFilterOutput) bool {
	if observed == nil {
		return false
	}
	if roleArn := effectiveOptionalString(r.RoleArn); roleArn != "" &&
		effectiveOptionalString(observed.RoleArn) != roleArn {
		return true
	}
	if r.ApplyOnTransformedLogs != nil {
		return observed.ApplyOnTransformedLogs != *r.ApplyOnTransformedLogs
	}
	return false
}

func (r *SubscriptionFilter) key(prior *SubscriptionFilterOutput) subscriptionFilterKey {
	if prior != nil && prior.LogGroupName != "" && prior.Name != "" {
		return subscriptionFilterKey{
			LogGroupName: prior.LogGroupName,
			Name:         prior.Name,
		}
	}
	return subscriptionFilterKey{
		LogGroupName: r.LogGroupName,
		Name:         r.Name,
	}
}

func (r *SubscriptionFilter) validate() error {
	if n := len(r.Name); n < 1 || n > 512 {
		return fmt.Errorf("name must be 1 to 512 bytes, got %d", n)
	}
	if n := len(r.FilterPattern); n > 1024 {
		return fmt.Errorf("filter-pattern must be at most 1024 bytes, got %d", n)
	}
	if r.FieldSelectionCriteria != nil {
		if n := len(*r.FieldSelectionCriteria); n > 2000 {
			return fmt.Errorf("field-selection-criteria must be at most 2000 bytes, got %d",
				n)
		}
	}
	if !validRequiredSubscriptionFilterARN(r.DestinationArn) {
		return errors.New("destination-arn must be a valid ARN")
	}
	if roleArn := effectiveOptionalString(r.RoleArn); roleArn != "" &&
		!validSubscriptionFilterARN(roleArn) {
		return errors.New("role-arn must be a valid ARN")
	}
	if !validSubscriptionFilterDistribution(effectiveDistribution(r.Distribution)) {
		return errors.New("distribution must be ByLogStream or Random")
	}
	for _, field := range r.EmitSystemFields {
		if !validEmitSystemField(field) {
			return fmt.Errorf("emit-system-fields entry %q must be @aws.account or @aws.region",
				field)
		}
	}
	return nil
}

type subscriptionFilterKey struct {
	LogGroupName string
	Name         string
}

func effectiveDistribution(v string) string {
	if v == "" {
		return distributionByLogStream
	}
	return v
}

func effectiveOptionalString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func nonEmptyStringPtr(v *string) *string {
	if effectiveOptionalString(v) == "" {
		return nil
	}
	return aws.String(*v)
}

func normalizedEmitSystemFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	slices.Sort(out)
	return out
}

func sameStringSet(a, b []string) bool {
	return slices.Equal(normalizedEmitSystemFields(a), normalizedEmitSystemFields(b))
}

func validSubscriptionFilterDistribution(v string) bool {
	return v == distributionByLogStream || v == distributionRandom
}

func validEmitSystemField(v string) bool {
	return v == "@aws.account" || v == "@aws.region"
}

func validRequiredSubscriptionFilterARN(s string) bool {
	return s != "" && validSubscriptionFilterARN(s)
}

func validSubscriptionFilterARN(s string) bool {
	if s == "" {
		return true
	}
	parsed, err := awsarn.Parse(s)
	if err != nil {
		return false
	}
	if !subscriptionFilterARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !subscriptionFilterARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" && !subscriptionFilterARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}

func isPutSubscriptionFilterRetryable(err error) bool {
	var invalid *cloudwatchlogstypes.InvalidParameterException
	if errors.As(err, &invalid) {
		return strings.Contains(invalid.ErrorMessage(),
			"Could not deliver test message to specified") ||
			strings.Contains(invalid.ErrorMessage(),
				"Could not execute the lambda function")
	}
	var aborted *cloudwatchlogstypes.OperationAbortedException
	if errors.As(err, &aborted) {
		return strings.Contains(aborted.ErrorMessage(), "Please try again")
	}
	var validation *cloudwatchlogstypes.ValidationException
	if errors.As(err, &validation) {
		return strings.Contains(validation.ErrorMessage(),
			"Make sure you have given CloudWatch Logs permission to assume the provided role")
	}
	return false
}
