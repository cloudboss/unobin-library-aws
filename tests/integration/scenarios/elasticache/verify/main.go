package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
)

const (
	primaryName = "unobin-it-cache-primary"
	clearName   = "unobin-it-cache-clear"
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
	elasticacheClient := elasticache.NewFromConfig(cfg)
	ec2Client := ec2.NewFromConfig(cfg)

	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		return verifyPresent(ctx, elasticacheClient, ec2Client,
			"initial elasticache subnet group",
			[]string{"10.72.1.0/24", "10.72.2.0/24"},
			map[string]string{"change": "old", "keep": "1", "remove": "yes"},
			map[string]string{"clear": "yes"})
	case "updated":
		return verifyPresent(ctx, elasticacheClient, ec2Client,
			"updated elasticache subnet group",
			[]string{"10.72.1.0/24", "10.72.3.0/24"},
			map[string]string{"add": "yes", "change": "new", "keep": "1"},
			map[string]string{})
	case "destroyed":
		return verifyDestroyed(ctx, elasticacheClient)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q",
			phase)
	}
}

func verifyPresent(
	ctx context.Context,
	elasticacheClient *elasticache.Client,
	ec2Client *ec2.Client,
	description string,
	subnetCIDRs []string,
	primaryTags map[string]string,
	clearTags map[string]string,
) error {
	primary, err := findSubnetGroup(ctx, elasticacheClient, primaryName)
	if err != nil {
		return err
	}
	if primary == nil {
		return fmt.Errorf("cache subnet group %s not found", primaryName)
	}
	if aws.ToString(primary.CacheSubnetGroupName) != primaryName {
		return fmt.Errorf("cache subnet group name is %q, want %q",
			aws.ToString(primary.CacheSubnetGroupName), primaryName)
	}
	if aws.ToString(primary.CacheSubnetGroupDescription) != description {
		return fmt.Errorf("cache subnet group description is %q, want %q",
			aws.ToString(primary.CacheSubnetGroupDescription), description)
	}
	wantSubnetIDs, err := findSubnetIDs(ctx, ec2Client, subnetCIDRs)
	if err != nil {
		return err
	}
	gotSubnetIDs := make([]string, 0, len(primary.Subnets))
	for _, subnet := range primary.Subnets {
		gotSubnetIDs = append(gotSubnetIDs, aws.ToString(subnet.SubnetIdentifier))
	}
	slices.Sort(gotSubnetIDs)
	slices.Sort(wantSubnetIDs)
	if !reflect.DeepEqual(gotSubnetIDs, wantSubnetIDs) {
		return fmt.Errorf("cache subnet group subnets are %v, want %v",
			gotSubnetIDs, wantSubnetIDs)
	}
	if err := verifyTags(ctx, elasticacheClient, primary, primaryTags); err != nil {
		return err
	}

	clearGroup, err := findSubnetGroup(ctx, elasticacheClient, clearName)
	if err != nil {
		return err
	}
	if clearGroup == nil {
		return fmt.Errorf("cache subnet group %s not found", clearName)
	}
	if err := verifyTags(ctx, elasticacheClient, clearGroup, clearTags); err != nil {
		return err
	}
	fmt.Printf("ok: cache subnet groups %s and %s match cloud state\n",
		primaryName, clearName)
	return nil
}

func verifyDestroyed(ctx context.Context, client *elasticache.Client) error {
	for _, name := range []string{primaryName, clearName} {
		group, err := findSubnetGroup(ctx, client, name)
		if err != nil {
			return err
		}
		if group != nil {
			return fmt.Errorf("cache subnet group %s still exists", name)
		}
	}
	fmt.Printf("ok: cache subnet groups %s and %s are gone\n", primaryName, clearName)
	return nil
}

func findSubnetGroup(
	ctx context.Context,
	client *elasticache.Client,
	name string,
) (*elasticachetypes.CacheSubnetGroup, error) {
	paginator := elasticache.NewDescribeCacheSubnetGroupsPaginator(client,
		&elasticache.DescribeCacheSubnetGroupsInput{
			CacheSubnetGroupName: aws.String(name),
		})
	var groups []elasticachetypes.CacheSubnetGroup
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			var notFound *elasticachetypes.CacheSubnetGroupNotFoundFault
			if errors.As(err, &notFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("describe cache subnet groups: %w", err)
		}
		groups = append(groups, page.CacheSubnetGroups...)
	}
	if len(groups) == 0 {
		return nil, nil
	}
	if len(groups) != 1 {
		return nil, fmt.Errorf("describe cache subnet groups returned %d results for %s",
			len(groups), name)
	}
	return &groups[0], nil
}

func verifyTags(
	ctx context.Context,
	client *elasticache.Client,
	group *elasticachetypes.CacheSubnetGroup,
	want map[string]string,
) error {
	out, err := client.ListTagsForResource(ctx, &elasticache.ListTagsForResourceInput{
		ResourceName: group.ARN,
	})
	if err != nil {
		return fmt.Errorf("list cache subnet group tags: %w", err)
	}
	got := make(map[string]string, len(out.TagList))
	for _, tag := range out.TagList {
		got[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("cache subnet group %s tags are %v, want %v",
			aws.ToString(group.CacheSubnetGroupName), got, want)
	}
	return nil
}

func findSubnetIDs(
	ctx context.Context,
	client *ec2.Client,
	cidrs []string,
) ([]string, error) {
	paginator := ec2.NewDescribeSubnetsPaginator(client, &ec2.DescribeSubnetsInput{})
	want := make(map[string]bool, len(cidrs))
	for _, cidr := range cidrs {
		want[cidr] = true
	}
	ids := make([]string, 0, len(cidrs))
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe subnets: %w", err)
		}
		for _, subnet := range page.Subnets {
			if want[aws.ToString(subnet.CidrBlock)] {
				ids = append(ids, aws.ToString(subnet.SubnetId))
			}
		}
	}
	if len(ids) != len(cidrs) {
		return nil, fmt.Errorf("found %d subnets for CIDRs %v, want %d",
			len(ids), cidrs, len(cidrs))
	}
	return ids, nil
}
