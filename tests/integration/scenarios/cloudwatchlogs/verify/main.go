// verify checks the CloudWatch Logs resources the scenario applied. It only
// reads cloud state; removing resources is the destroy plan's job.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

const (
	logGroupName     = "unobin-it-log-group"
	filterName       = "unobin-it-subscription-filter"
	functionName     = "unobin-it-cwl-sink"
	markerKey        = "unobin"
	markerValue      = "cloudwatchlogs-it"
	appliedRetention = 30
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	mode := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	logsClient := cloudwatchlogs.NewFromConfig(cfg)
	lambdaClient := lambda.NewFromConfig(cfg)

	switch mode {
	case "applied":
		return verifyApplied(ctx, logsClient, lambdaClient)
	case "destroyed":
		return verifyDestroyed(ctx, logsClient, lambdaClient)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", mode)
	}
}

func verifyApplied(
	ctx context.Context, logsClient *cloudwatchlogs.Client, lambdaClient *lambda.Client,
) error {
	group, err := findGroup(ctx, logsClient)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("log group %s not found", logGroupName)
	}
	retention := aws.ToInt32(group.RetentionInDays)
	if retention != appliedRetention {
		return fmt.Errorf("log group %s retention is %d days, want %d",
			logGroupName, retention, appliedRetention)
	}

	filter, err := findSubscriptionFilter(ctx, logsClient)
	if err != nil {
		return err
	}
	if filter == nil {
		return fmt.Errorf("subscription filter %s not found", filterName)
	}
	destination := aws.ToString(filter.DestinationArn)
	if !strings.Contains(destination, ":function:"+functionName) {
		return fmt.Errorf("subscription filter destination is %s, want function %s",
			destination, functionName)
	}

	_, err = lambdaClient.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(functionName),
	})
	if err != nil {
		return fmt.Errorf("get function %s: %w", functionName, err)
	}

	arn := strings.TrimSuffix(aws.ToString(group.Arn), ":*")
	tags, err := logsClient.ListTagsForResource(ctx,
		&cloudwatchlogs.ListTagsForResourceInput{ResourceArn: aws.String(arn)})
	switch {
	case err != nil:
		fmt.Printf("skip: list tags for %s: %v\n", arn, err)
	case tags.Tags[markerKey] != markerValue:
		fmt.Printf("skip: marker tag %s=%s not modeled on %s\n",
			markerKey, markerValue, logGroupName)
	default:
		fmt.Printf("ok: log group %s has the marker tag\n", logGroupName)
	}

	fmt.Printf("ok: log group %s present with retention %d days\n",
		logGroupName, retention)
	fmt.Printf("ok: subscription filter %s sends to %s\n", filterName, functionName)
	return nil
}

func verifyDestroyed(
	ctx context.Context, logsClient *cloudwatchlogs.Client, lambdaClient *lambda.Client,
) error {
	group, err := findGroup(ctx, logsClient)
	if err != nil {
		return err
	}
	if group != nil {
		return fmt.Errorf("log group %s still exists", logGroupName)
	}
	filter, err := findSubscriptionFilter(ctx, logsClient)
	if err != nil {
		return err
	}
	if filter != nil {
		return fmt.Errorf("subscription filter %s still exists", filterName)
	}
	_, err = lambdaClient.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(functionName),
	})
	if err == nil {
		return fmt.Errorf("function %s still exists", functionName)
	}
	if !isLambdaNotFound(err) {
		return fmt.Errorf("get function %s: %w", functionName, err)
	}
	fmt.Printf("ok: log group %s and subscription filter %s gone\n", logGroupName, filterName)
	return nil
}

func findGroup(
	ctx context.Context, client *cloudwatchlogs.Client,
) (*cloudwatchlogstypes.LogGroup, error) {
	pager := cloudwatchlogs.NewDescribeLogGroupsPaginator(client,
		&cloudwatchlogs.DescribeLogGroupsInput{
			LogGroupNamePrefix: aws.String(logGroupName),
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe log groups: %w", err)
		}
		for i := range page.LogGroups {
			if aws.ToString(page.LogGroups[i].LogGroupName) == logGroupName {
				return &page.LogGroups[i], nil
			}
		}
	}
	return nil, nil
}

func findSubscriptionFilter(
	ctx context.Context, client *cloudwatchlogs.Client,
) (*cloudwatchlogstypes.SubscriptionFilter, error) {
	pager := cloudwatchlogs.NewDescribeSubscriptionFiltersPaginator(client,
		&cloudwatchlogs.DescribeSubscriptionFiltersInput{
			LogGroupName:     aws.String(logGroupName),
			FilterNamePrefix: aws.String(filterName),
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isLogsNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("describe subscription filters: %w", err)
		}
		for i := range page.SubscriptionFilters {
			if aws.ToString(page.SubscriptionFilters[i].FilterName) == filterName {
				return &page.SubscriptionFilters[i], nil
			}
		}
	}
	return nil, nil
}

func isLogsNotFound(err error) bool {
	var notFound *cloudwatchlogstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func isLambdaNotFound(err error) bool {
	var notFound *lambdatypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
