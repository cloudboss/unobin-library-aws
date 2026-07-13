package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
)

const (
	trailName = "unobin-it-cloudtrail"
	tagState  = "state"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := cloudtrail.NewFromConfig(cfg)
	switch step := os.Getenv("VERIFY_" + "PH" + "ASE"); step {
	case "applied":
		return verifyTrail(ctx, client, true, false, "initial", true)
	case "updated":
		return verifyTrail(ctx, client, false, true, "updated", false)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("verification step is %q, want applied, updated, or destroyed", step)
	}
}

func verifyTrail(
	ctx context.Context,
	client *cloudtrail.Client,
	wantLogging bool,
	wantValidation bool,
	wantPrefix string,
	initial bool,
) error {
	described, err := client.DescribeTrails(ctx, &cloudtrail.DescribeTrailsInput{
		TrailNameList: []string{trailName},
	})
	if err != nil {
		return fmt.Errorf("describe trail: %w", err)
	}
	if len(described.TrailList) != 1 {
		return fmt.Errorf("describe trail returned %d trails, want 1", len(described.TrailList))
	}
	trail := described.TrailList[0]
	if aws.ToString(trail.S3KeyPrefix) != wantPrefix {
		return fmt.Errorf("trail prefix is %q, want %q", aws.ToString(trail.S3KeyPrefix), wantPrefix)
	}
	if aws.ToBool(trail.LogFileValidationEnabled) != wantValidation {
		return fmt.Errorf("trail validation is %t, want %t",
			aws.ToBool(trail.LogFileValidationEnabled), wantValidation)
	}
	status, err := client.GetTrailStatus(ctx, &cloudtrail.GetTrailStatusInput{Name: trail.TrailARN})
	if err != nil {
		return fmt.Errorf("get trail status: %w", err)
	}
	if aws.ToBool(status.IsLogging) != wantLogging {
		return fmt.Errorf("trail logging is %t, want %t",
			aws.ToBool(status.IsLogging), wantLogging)
	}
	if err := verifySelectors(ctx, client, initial); err != nil {
		return err
	}
	if err := verifyInsights(ctx, client, initial); err != nil {
		return err
	}
	if err := verifyAggregation(ctx, client, initial); err != nil {
		return err
	}
	tags, err := client.ListTags(ctx, &cloudtrail.ListTagsInput{
		ResourceIdList: []string{aws.ToString(trail.TrailARN)},
	})
	if err != nil {
		return fmt.Errorf("list trail tags: %w", err)
	}
	return verifyTags(tags.ResourceTagList, initial)
}

func verifySelectors(
	ctx context.Context,
	client *cloudtrail.Client,
	wantStandard bool,
) error {
	out, err := client.GetEventSelectors(ctx, &cloudtrail.GetEventSelectorsInput{
		TrailName: aws.String(trailName),
	})
	if err != nil {
		return fmt.Errorf("get event selectors: %w", err)
	}
	if wantStandard {
		if len(out.EventSelectors) != 1 || len(out.AdvancedEventSelectors) != 0 {
			return fmt.Errorf("standard selectors are %d/%d, want 1/0",
				len(out.EventSelectors), len(out.AdvancedEventSelectors))
		}
		selector := out.EventSelectors[0]
		if !aws.ToBool(selector.IncludeManagementEvents) ||
			selector.ReadWriteType != cloudtrailtypes.ReadWriteTypeAll ||
			len(selector.ExcludeManagementEventSources) != 0 ||
			len(selector.DataResources) != 1 {
			return fmt.Errorf("standard selector is unexpected: %+v", selector)
		}
		resource := selector.DataResources[0]
		if aws.ToString(resource.Type) != "AWS::S3::Object" ||
			!slices.Equal(resource.Values,
				[]string{"arn:aws:s3:::unobin-it-cloudtrail-logs/"}) {
			return fmt.Errorf("standard selector data resource is unexpected: %+v", resource)
		}
		return nil
	}
	if len(out.EventSelectors) != 0 || len(out.AdvancedEventSelectors) != 1 {
		return fmt.Errorf("advanced selectors are %d/%d, want 0/1",
			len(out.EventSelectors), len(out.AdvancedEventSelectors))
	}
	selector := out.AdvancedEventSelectors[0]
	if aws.ToString(selector.Name) != "data-events" || len(selector.FieldSelectors) != 2 {
		return fmt.Errorf("advanced selector is unexpected: %+v", selector)
	}
	expected := map[string][]string{
		"eventCategory":  {"Data"},
		"resources.type": {"AWS::S3::Object"},
	}
	for _, field := range selector.FieldSelectors {
		name := aws.ToString(field.Field)
		values, ok := expected[name]
		if !ok || !slices.Equal(field.Equals, values) ||
			len(field.NotEquals) != 0 || len(field.StartsWith) != 0 ||
			len(field.NotStartsWith) != 0 || len(field.EndsWith) != 0 ||
			len(field.NotEndsWith) != 0 {
			return fmt.Errorf("advanced field selector is unexpected: %+v", field)
		}
		delete(expected, name)
	}
	if len(expected) != 0 {
		return fmt.Errorf("advanced selector is missing fields: %v", expected)
	}
	return nil
}

func verifyInsights(
	ctx context.Context,
	client *cloudtrail.Client,
	wantManagement bool,
) error {
	out, err := client.GetInsightSelectors(ctx, &cloudtrail.GetInsightSelectorsInput{
		TrailName: aws.String(trailName),
	})
	if err != nil {
		return fmt.Errorf("get insight selectors: %w", err)
	}
	if len(out.InsightSelectors) != 1 {
		return fmt.Errorf("trail has %d insight selectors, want 1", len(out.InsightSelectors))
	}
	selector := out.InsightSelectors[0]
	if selector.InsightType != cloudtrailtypes.InsightTypeApiErrorRateInsight {
		return fmt.Errorf("trail insight type is %q, want ApiErrorRateInsight",
			selector.InsightType)
	}
	categories := selector.EventCategories
	if wantManagement {
		if !slices.Equal(categories, []cloudtrailtypes.SourceEventCategory{
			cloudtrailtypes.SourceEventCategoryManagement,
			cloudtrailtypes.SourceEventCategoryData,
		}) {
			return fmt.Errorf("trail insight categories are %v, want Management and Data",
				categories)
		}
		return nil
	}
	if len(categories) != 1 || categories[0] != cloudtrailtypes.SourceEventCategoryData {
		return fmt.Errorf("trail insight categories are %v, want Data", categories)
	}
	return nil
}

func verifyAggregation(
	ctx context.Context,
	client *cloudtrail.Client,
	want bool,
) error {
	out, err := client.GetEventConfiguration(ctx, &cloudtrail.GetEventConfigurationInput{
		TrailName: aws.String(trailName),
	})
	if err != nil {
		return fmt.Errorf("get event configuration: %w", err)
	}
	if want && len(out.AggregationConfigurations) != 1 {
		return fmt.Errorf("trail has %d aggregations, want 1", len(out.AggregationConfigurations))
	}
	if !want && len(out.AggregationConfigurations) != 0 {
		return fmt.Errorf("trail has %d aggregations, want 0", len(out.AggregationConfigurations))
	}
	if want {
		config := out.AggregationConfigurations[0]
		if config.EventCategory != cloudtrailtypes.EventCategoryAggregationData ||
			!slices.Equal(config.Templates, []cloudtrailtypes.Template{
				cloudtrailtypes.TemplateApiActivity,
				cloudtrailtypes.TemplateResourceAccess,
			}) {
			return fmt.Errorf("trail aggregation is unexpected: %+v", config)
		}
	}
	return nil
}

func verifyTags(resources []cloudtrailtypes.ResourceTag, initial bool) error {
	tags := trailTags(resources)
	if initial {
		if tags[tagState] != "initial" || tags["remove"] != "yes" {
			return fmt.Errorf("initial trail tags are unexpected: %v", tags)
		}
		if _, ok := tags["added"]; ok {
			return fmt.Errorf("initial trail tags still contain added: %v", tags)
		}
		return nil
	}
	if tags[tagState] != "updated" || tags["added"] != "yes" {
		return fmt.Errorf("updated trail tags are unexpected: %v", tags)
	}
	if _, ok := tags["remove"]; ok {
		return fmt.Errorf("updated trail tags still contain remove: %v", tags)
	}
	return nil
}

func verifyDestroyed(ctx context.Context, client *cloudtrail.Client) error {
	out, err := client.DescribeTrails(ctx, &cloudtrail.DescribeTrailsInput{
		TrailNameList: []string{trailName},
	})
	if err != nil {
		var notFound *cloudtrailtypes.TrailNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("describe deleted trail: %w", err)
	}
	if len(out.TrailList) != 0 {
		return errors.New("trail still exists")
	}
	return nil
}

func trailTags(resources []cloudtrailtypes.ResourceTag) map[string]string {
	tags := map[string]string{}
	for _, resource := range resources {
		for _, tag := range resource.TagsList {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}
	return tags
}
