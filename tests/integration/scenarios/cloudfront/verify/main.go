// verify checks the CloudFront distribution the scenario applied against the
// phase named in the VERIFY_PHASE environment variable. A distribution has no
// stable name, so it is found by its comment, which the first apply sets: applied
// requires it present and enabled, destroyed requires it gone. It only reads
// cloud state; tearing the distribution down is the destroy plan's job.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// wantComment is the comment the first apply set; the distribution is found by it.
const wantComment = "unobin cloudfront integration test"

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
	client := cloudfront.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *cloudfront.Client) error {
	d, err := findDistribution(ctx, client)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("no distribution with comment %q", wantComment)
	}
	if !aws.ToBool(d.Enabled) {
		return fmt.Errorf("distribution %s is not enabled", aws.ToString(d.Id))
	}
	fmt.Printf("ok: distribution %s present and enabled with comment %q\n",
		aws.ToString(d.Id), wantComment)
	return nil
}

func verifyDestroyed(ctx context.Context, client *cloudfront.Client) error {
	d, err := findDistribution(ctx, client)
	if err != nil {
		return err
	}
	if d != nil {
		return fmt.Errorf("distribution %s still exists", aws.ToString(d.Id))
	}
	fmt.Printf("ok: no distribution with comment %q\n", wantComment)
	return nil
}

// findDistribution returns the distribution summary whose comment matches
// wantComment, or nil when none does, paging through every distribution.
func findDistribution(
	ctx context.Context, client *cloudfront.Client,
) (*cloudfronttypes.DistributionSummary, error) {
	pager := cloudfront.NewListDistributionsPaginator(client,
		&cloudfront.ListDistributionsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list distributions: %w", err)
		}
		if page.DistributionList == nil {
			continue
		}
		for i, d := range page.DistributionList.Items {
			if aws.ToString(d.Comment) == wantComment {
				return &page.DistributionList.Items[i], nil
			}
		}
	}
	return nil, nil
}
