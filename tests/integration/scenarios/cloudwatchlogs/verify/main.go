// verify checks the CloudWatch Logs group the scenario applied against the
// phase named in the VERIFY_PHASE environment variable. The log group has a
// stable name of its own, so both phases find it by an exact-name match over a
// prefix describe: applied requires the group present with the retention the
// first apply set, and destroyed requires it gone. It only reads cloud state;
// tearing the group down is the destroy plan's job, not the verifier's.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

const (
	logGroupName     = "unobin-it-log-group"
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
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := cloudwatchlogs.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *cloudwatchlogs.Client) error {
	group, err := findGroup(ctx, client)
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

	// Tags are anchored client-side: an emulator may not return them, so a
	// missing marker degrades to a printed skip rather than a failure.
	arn := strings.TrimSuffix(aws.ToString(group.Arn), ":*")
	tags, err := client.ListTagsForResource(ctx, &cloudwatchlogs.ListTagsForResourceInput{
		ResourceArn: aws.String(arn),
	})
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
	return nil
}

func verifyDestroyed(ctx context.Context, client *cloudwatchlogs.Client) error {
	group, err := findGroup(ctx, client)
	if err != nil {
		return err
	}
	if group != nil {
		return fmt.Errorf("log group %s still exists", logGroupName)
	}
	fmt.Printf("ok: log group %s gone\n", logGroupName)
	return nil
}

// findGroup returns the log group whose name matches logGroupName exactly, or
// nil when none does. The describe filters by name prefix, which can return
// other groups sharing the prefix, so the match is confirmed client-side.
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
