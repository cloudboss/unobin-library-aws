package main

import (
	"context"
	"fmt"
	"log"
	"maps"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
)

const fileSystemName = "unobin-it-efs"

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	client := efs.NewFromConfig(cfg)
	switch mode := os.Getenv("VERIFY_PHASE"); mode {
	case "applied":
		return verifyPresent(ctx, client, efstypes.ThroughputModeBursting,
			map[string]string{
				"Name": fileSystemName, "state": "initial", "remove": "yes",
			})
	case "updated":
		return verifyPresent(ctx, client, efstypes.ThroughputModeElastic,
			map[string]string{
				"Name": fileSystemName, "state": "updated", "added": "yes",
			})
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q", mode)
	}
}

func verifyPresent(
	ctx context.Context,
	client *efs.Client,
	wantMode efstypes.ThroughputMode,
	wantTags map[string]string,
) error {
	fileSystem, tags, err := findFileSystem(ctx, client)
	if err != nil {
		return err
	}
	if fileSystem == nil {
		return fmt.Errorf("file system tagged Name=%s not found", fileSystemName)
	}
	if fileSystem.LifeCycleState != efstypes.LifeCycleStateAvailable {
		return fmt.Errorf("file system state is %s, want available", fileSystem.LifeCycleState)
	}
	if fileSystem.ThroughputMode != wantMode {
		return fmt.Errorf("throughput mode is %s, want %s",
			fileSystem.ThroughputMode, wantMode)
	}
	if !maps.Equal(tags, wantTags) {
		return fmt.Errorf("file system tags are %#v, want %#v", tags, wantTags)
	}
	fmt.Printf("ok: EFS file system %s is available with throughput mode %s\n",
		aws.ToString(fileSystem.FileSystemId), wantMode)
	return nil
}

func verifyDestroyed(ctx context.Context, client *efs.Client) error {
	fileSystem, _, err := findFileSystem(ctx, client)
	if err != nil {
		return err
	}
	if fileSystem != nil {
		return fmt.Errorf("file system %s still exists", aws.ToString(fileSystem.FileSystemId))
	}
	fmt.Printf("ok: EFS file system tagged Name=%s is gone\n", fileSystemName)
	return nil
}

func findFileSystem(
	ctx context.Context,
	client *efs.Client,
) (*efstypes.FileSystemDescription, map[string]string, error) {
	pager := efs.NewDescribeFileSystemsPaginator(client, &efs.DescribeFileSystemsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("describe file systems: %w", err)
		}
		for i := range page.FileSystems {
			if page.FileSystems[i].LifeCycleState == efstypes.LifeCycleStateDeleted {
				continue
			}
			tags, err := readTags(ctx, client, aws.ToString(page.FileSystems[i].FileSystemId))
			if err != nil {
				return nil, nil, err
			}
			if tags["Name"] == fileSystemName {
				return &page.FileSystems[i], tags, nil
			}
		}
	}
	return nil, nil, nil
}

func readTags(
	ctx context.Context,
	client *efs.Client,
	fileSystemID string,
) (map[string]string, error) {
	resp, err := client.ListTagsForResource(ctx, &efs.ListTagsForResourceInput{
		ResourceId: aws.String(fileSystemID),
	})
	if err != nil {
		return nil, fmt.Errorf("list tags for file system %s: %w", fileSystemID, err)
	}
	tags := map[string]string{}
	for _, tag := range resp.Tags {
		key := aws.ToString(tag.Key)
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	return tags, nil
}
