package scheduler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
)

var (
	scheduleNamePattern   = regexp.MustCompile(`^[0-9A-Za-z_.-]+$`)
	scheduleKMSARNPattern = regexp.MustCompile(
		`^arn:aws(-[a-z]+)?:kms:[a-z0-9-]+:\d{12}:(key|alias)/[0-9a-zA-Z_-]*$`)
	scheduleRoleARNPattern = regexp.MustCompile(
		`^arn:aws(-[a-z]+)?:iam::\d{12}:role/[\w+=,.@/-]+$`)
	scheduleQueueARNPattern = regexp.MustCompile(
		`^arn:aws(-[a-z]+)?:sqs:[a-z0-9-]+:\d{12}:[a-zA-Z0-9_-]+$`)
	scheduleSageMakerNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]*$`)
	scheduleEventSourcePattern   = regexp.MustCompile(`^[/.\-_A-Za-z0-9]+$`)
	scheduleEventPathPattern     = regexp.MustCompile(
		`^\$(\.[A-Za-z0-9_-]+(\[(\d+|\*)\])*)*$`)
)

func (r *ScheduleResource) ValidateInputs(context.Context, *awsCfg) error {
	if r.Name != nil {
		if err := validateScheduleName("name", *r.Name); err != nil {
			return err
		}
	}
	if r.GroupName != nil {
		if err := validateScheduleName("group-name", *r.GroupName); err != nil {
			return err
		}
	}
	if r.ActionAfterCompletion != nil &&
		!slices.Contains([]string{"DELETE", "NONE"}, *r.ActionAfterCompletion) {
		return errors.New("action-after-completion must be DELETE or NONE")
	}
	if r.Description != nil && utf8.RuneCountInString(*r.Description) > 512 {
		return errors.New("description must contain at most 512 characters")
	}
	if !slices.Contains([]string{"OFF", "FLEXIBLE"}, r.FlexibleTimeWindow.Mode) {
		return errors.New("flexible-time-window mode must be OFF or FLEXIBLE")
	}
	window := r.FlexibleTimeWindow.MaximumWindowInMinutes
	if window != nil && (*window < 1 || *window > 1440) {
		return errors.New("maximum-window-in-minutes must be between 1 and 1440")
	}
	if r.FlexibleTimeWindow.Mode == "FLEXIBLE" && window == nil {
		return errors.New("FLEXIBLE mode requires maximum-window-in-minutes")
	}
	if err := validateLength(
		"schedule-expression", r.ScheduleExpression, 1, 256); err != nil {
		return err
	}
	if r.ScheduleExpressionTimezone != nil {
		if err := validateLength(
			"schedule-expression-timezone",
			*r.ScheduleExpressionTimezone,
			1,
			50,
		); err != nil {
			return err
		}
	}
	if r.State != nil && !slices.Contains([]string{"ENABLED", "DISABLED"}, *r.State) {
		return errors.New("state must be ENABLED or DISABLED")
	}
	if r.KMSKeyARN != nil {
		if err := validatePatternLength(
			"kms-key-arn", *r.KMSKeyARN, 1, 2048, scheduleKMSARNPattern); err != nil {
			return err
		}
	}
	if err := validateRFC3339("start-date", r.StartDate); err != nil {
		return err
	}
	if err := validateRFC3339("end-date", r.EndDate); err != nil {
		return err
	}
	return r.Target.validate()
}

func (t ScheduleTarget) validate() error {
	if err := validateARNLength("target arn", t.ARN, 1, 1600); err != nil {
		return err
	}
	if err := validatePatternLength(
		"target role-arn", t.RoleARN, 1, 1600, scheduleRoleARNPattern); err != nil {
		return err
	}
	if t.Input != nil && *t.Input == "" {
		return errors.New("target input must not be empty")
	}
	if countPresent(
		t.ECSParameters,
		t.EventBridgeParameters,
		t.KinesisParameters,
		t.SageMakerPipelineParameters,
		t.SQSParameters,
	) > 1 {
		return errors.New("target permits at most one templated parameter block")
	}
	if t.DeadLetterConfig != nil {
		if err := validatePatternLength(
			"dead-letter-config arn",
			t.DeadLetterConfig.ARN,
			1,
			1600,
			scheduleQueueARNPattern,
		); err != nil {
			return err
		}
	}
	if t.RetryPolicy != nil {
		if age := t.RetryPolicy.MaximumEventAgeInSeconds; age != nil &&
			(*age < 60 || *age > 86400) {
			return errors.New(
				"maximum-event-age-in-seconds must be between 60 and 86400")
		}
		if attempts := t.RetryPolicy.MaximumRetryAttempts; attempts != nil &&
			(*attempts < 0 || *attempts > 185) {
			return errors.New("maximum-retry-attempts must be between 0 and 185")
		}
	}
	if err := t.ECSParameters.validate(); err != nil {
		return err
	}
	if err := t.EventBridgeParameters.validate(); err != nil {
		return err
	}
	if err := t.KinesisParameters.validate(); err != nil {
		return err
	}
	if err := t.SageMakerPipelineParameters.validate(); err != nil {
		return err
	}
	return t.SQSParameters.validate()
}

func (p *ScheduleTargetECSParameters) validate() error {
	if p == nil {
		return nil
	}
	if err := validateARNLength(
		"task-definition-arn", p.TaskDefinitionARN, 1, 1600); err != nil {
		return err
	}
	if p.TaskCount != nil && (*p.TaskCount < 1 || *p.TaskCount > 10) {
		return errors.New("task-count must be between 1 and 10")
	}
	if p.LaunchType != nil &&
		!slices.Contains([]string{"EC2", "FARGATE", "EXTERNAL"}, *p.LaunchType) {
		return errors.New("launch-type must be EC2, FARGATE, or EXTERNAL")
	}
	if p.CapacityProviderStrategy != nil && p.LaunchType != nil {
		return errors.New("capacity-provider-strategy conflicts with launch-type")
	}
	if err := p.validateCapacityProviderStrategy(); err != nil {
		return err
	}
	if err := p.validateNetworkConfiguration(); err != nil {
		return err
	}
	if p.PlatformVersion != nil {
		if err := validateLength("platform-version", *p.PlatformVersion, 1, 64); err != nil {
			return err
		}
		if p.LaunchType == nil || *p.LaunchType != "FARGATE" {
			return errors.New("platform-version requires FARGATE")
		}
	}
	if p.Group != nil {
		if err := validateLength("ECS group", *p.Group, 1, 255); err != nil {
			return err
		}
	}
	if p.LaunchType != nil && *p.LaunchType == "FARGATE" {
		if p.NetworkConfiguration == nil {
			return errors.New("FARGATE requires network-configuration")
		}
		if p.PlacementConstraints != nil {
			return errors.New("FARGATE forbids placement-constraints")
		}
	}
	if err := p.validatePlacementConstraints(); err != nil {
		return err
	}
	if err := p.validatePlacementStrategy(); err != nil {
		return err
	}
	if p.PropagateTags != nil && *p.PropagateTags != "TASK_DEFINITION" {
		return errors.New("propagate-tags must be TASK_DEFINITION")
	}
	if p.ReferenceID != nil && utf8.RuneCountInString(*p.ReferenceID) > 1024 {
		return errors.New("reference-id must contain at most 1024 characters")
	}
	return p.validateTags()
}

func (p *ScheduleTargetECSParameters) validateCapacityProviderStrategy() error {
	if p.CapacityProviderStrategy == nil {
		return nil
	}
	items := *p.CapacityProviderStrategy
	if len(items) > 6 {
		return errors.New("capacity-provider-strategy must contain at most 6 entries")
	}
	positiveBases := 0
	positiveWeight := false
	for i := range items {
		item := items[i]
		if err := validateLength(
			"capacity-provider", item.CapacityProvider, 1, 255); err != nil {
			return fmt.Errorf("capacity-provider-strategy entry %d: %w", i, err)
		}
		if item.Base != nil {
			if *item.Base < 0 || *item.Base > 100000 {
				return fmt.Errorf(
					"capacity-provider-strategy entry %d base must be between 0 and 100000",
					i)
			}
			if *item.Base > 0 {
				positiveBases++
			}
		}
		if item.Weight != nil {
			if *item.Weight < 0 || *item.Weight > 1000 {
				return fmt.Errorf(
					"capacity-provider-strategy entry %d weight must be between 0 and 1000",
					i)
			}
			positiveWeight = positiveWeight || *item.Weight > 0
		}
	}
	if positiveBases > 1 {
		return errors.New("capacity-provider-strategy permits at most one positive base")
	}
	if len(items) > 0 && !positiveWeight {
		return errors.New("capacity-provider-strategy requires at least one positive weight")
	}
	return nil
}

func (p *ScheduleTargetECSParameters) validateNetworkConfiguration() error {
	network := p.NetworkConfiguration
	if network == nil {
		return nil
	}
	if len(network.Subnets) < 1 || len(network.Subnets) > 16 {
		return errors.New("network-configuration requires 1 to 16 subnets")
	}
	for i, subnet := range network.Subnets {
		if err := validateLength("subnet", subnet, 1, 1000); err != nil {
			return fmt.Errorf("network-configuration subnet %d: %w", i, err)
		}
	}
	if network.SecurityGroups != nil {
		groups := *network.SecurityGroups
		if len(groups) < 1 || len(groups) > 5 {
			return errors.New("network-configuration permits 1 to 5 security groups")
		}
		for i, group := range groups {
			if err := validateLength("security group", group, 1, 1000); err != nil {
				return fmt.Errorf("network-configuration security group %d: %w", i, err)
			}
		}
	}
	if network.AssignPublicIP != nil && *network.AssignPublicIP &&
		(p.LaunchType == nil || *p.LaunchType != "FARGATE") {
		return errors.New("assign-public-ip true requires FARGATE")
	}
	return nil
}

func (p *ScheduleTargetECSParameters) validatePlacementConstraints() error {
	if p.PlacementConstraints == nil {
		return nil
	}
	items := *p.PlacementConstraints
	if len(items) > 10 {
		return errors.New("placement-constraints must contain at most 10 entries")
	}
	for i := range items {
		item := items[i]
		switch item.Type {
		case "memberOf":
			if item.Expression == nil {
				return fmt.Errorf(
					"placement-constraints entry %d memberOf requires an expression", i)
			}
			if err := validateLength(
				"expression", *item.Expression, 1, 2000); err != nil {
				return fmt.Errorf("placement-constraints entry %d: %w", i, err)
			}
		case "distinctInstance":
			if item.Expression != nil {
				return fmt.Errorf(
					"placement-constraints entry %d distinctInstance forbids an expression",
					i)
			}
		default:
			return fmt.Errorf(
				"placement-constraints entry %d type must be distinctInstance or memberOf",
				i)
		}
	}
	return nil
}

func (p *ScheduleTargetECSParameters) validatePlacementStrategy() error {
	if p.PlacementStrategy == nil {
		return nil
	}
	items := *p.PlacementStrategy
	if len(items) > 5 {
		return errors.New("placement-strategy must contain at most 5 entries")
	}
	for i := range items {
		item := items[i]
		if !slices.Contains([]string{"random", "spread", "binpack"}, item.Type) {
			return fmt.Errorf(
				"placement-strategy entry %d type must be random, spread, or binpack", i)
		}
		if item.Field != nil && utf8.RuneCountInString(*item.Field) > 255 {
			return fmt.Errorf(
				"placement-strategy entry %d field must contain at most 255 characters", i)
		}
	}
	return nil
}

func (p *ScheduleTargetECSParameters) validateTags() error {
	if p.Tags == nil {
		return nil
	}
	tags := *p.Tags
	if len(tags) > 50 {
		return errors.New("ECS tags must contain at most 50 entries")
	}
	for key, value := range tags {
		if err := validateLength("ECS tag key", key, 1, 128); err != nil {
			return fmt.Errorf("%q: %w", key, err)
		}
		if strings.HasPrefix(strings.ToLower(key), "aws:") {
			return fmt.Errorf("ECS tag key %q must not begin with aws:", key)
		}
		if utf8.RuneCountInString(value) > 256 {
			return fmt.Errorf("ECS tag value for %q must contain at most 256 characters", key)
		}
	}
	return nil
}

func (p *ScheduleEventBridgeParameters) validate() error {
	if p == nil {
		return nil
	}
	if err := validateLength("eventbridge detail-type", p.DetailType, 1, 128); err != nil {
		return err
	}
	if err := validateLength("eventbridge source", p.Source, 1, 256); err != nil {
		return err
	}
	if scheduleEventPathPattern.MatchString(p.Source) {
		return nil
	}
	if strings.HasPrefix(p.Source, "aws.") || !scheduleEventSourcePattern.MatchString(p.Source) {
		return errors.New("eventbridge source does not match the Scheduler source pattern")
	}
	return nil
}

func (p *ScheduleTargetKinesisParameters) validate() error {
	if p == nil {
		return nil
	}
	return validateLength("kinesis partition-key", p.PartitionKey, 1, 256)
}

func (p *ScheduleSageMakerPipelineParams) validate() error {
	if p == nil || p.PipelineParameterList == nil {
		return nil
	}
	items := *p.PipelineParameterList
	if len(items) > 200 {
		return errors.New("pipeline-parameter-list must contain at most 200 entries")
	}
	for i := range items {
		item := items[i]
		if err := validatePatternLength(
			"pipeline parameter name",
			item.Name,
			1,
			256,
			scheduleSageMakerNamePattern,
		); err != nil {
			return fmt.Errorf("pipeline-parameter-list entry %d: %w", i, err)
		}
		if err := validateLength(
			"pipeline parameter value", item.Value, 1, 1024); err != nil {
			return fmt.Errorf("pipeline-parameter-list entry %d: %w", i, err)
		}
	}
	return nil
}

func (p *ScheduleTargetSQSParameters) validate() error {
	if p == nil || p.MessageGroupID == nil {
		return nil
	}
	return validateLength("sqs message-group-id", *p.MessageGroupID, 1, 128)
}

func validateScheduleName(field, value string) error {
	if err := validateLength(field, value, 1, 64); err != nil {
		return err
	}
	if !scheduleNamePattern.MatchString(value) {
		return fmt.Errorf("%s must contain only letters, digits, period, underscore, or hyphen",
			field)
	}
	return nil
}

func validateRFC3339(field string, value *string) error {
	if value == nil {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, *value); err != nil {
		return fmt.Errorf("%s must be RFC3339: %w", field, err)
	}
	return nil
}

func validateARNLength(field, value string, minimum, maximum int) error {
	if err := validateLength(field, value, minimum, maximum); err != nil {
		return err
	}
	parsed, err := awsarn.Parse(value)
	if err != nil || parsed.Partition == "" || parsed.Service == "" || parsed.Resource == "" {
		return fmt.Errorf("%s must be a valid ARN", field)
	}
	return nil
}

func validatePatternLength(
	field string,
	value string,
	minimum int,
	maximum int,
	pattern *regexp.Regexp,
) error {
	if err := validateLength(field, value, minimum, maximum); err != nil {
		return err
	}
	if !pattern.MatchString(value) {
		return fmt.Errorf("%s does not match the Scheduler service pattern", field)
	}
	return nil
}

func validateLength(field, value string, minimum, maximum int) error {
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return fmt.Errorf(
			"%s must contain %d to %d characters", field, minimum, maximum)
	}
	return nil
}

func countPresent(values ...any) int {
	count := 0
	for _, value := range values {
		switch typed := value.(type) {
		case *ScheduleTargetECSParameters:
			if typed != nil {
				count++
			}
		case *ScheduleEventBridgeParameters:
			if typed != nil {
				count++
			}
		case *ScheduleTargetKinesisParameters:
			if typed != nil {
				count++
			}
		case *ScheduleSageMakerPipelineParams:
			if typed != nil {
				count++
			}
		case *ScheduleTargetSQSParameters:
			if typed != nil {
				count++
			}
		}
	}
	return count
}
