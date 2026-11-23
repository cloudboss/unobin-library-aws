package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

const (
	clusterName = "unobin-it-eks-entry"
	roleName    = "unobin-it-eks-entry-principal"
)

type entryIdentity struct {
	PrincipalARN string
	ARN          string
	CreatedAt    time.Time
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("verify EKS access entry: %v", err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	client := eks.NewFromConfig(cfg)
	path := filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "access-entry-identity.json")
	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		cluster, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{
			Name: aws.String(clusterName),
		})
		if err != nil {
			return fmt.Errorf("describe EKS cluster: %w", err)
		}
		if cluster.Cluster == nil {
			return errors.New("describe EKS cluster returned no cluster")
		}
		arn, err := awsarn.Parse(aws.ToString(cluster.Cluster.Arn))
		if err != nil {
			return fmt.Errorf("parse cluster ARN: %w", err)
		}
		principal := fmt.Sprintf("arn:%s:iam::%s:role/%s", arn.Partition, arn.AccountID, roleName)
		identity, err := verifyPresent(ctx, client, principal, "unobin-reader",
			[]string{"unobin-readers"},
			map[string]string{"change": "old", "keep": "1", "remove": "yes"},
		)
		if err != nil {
			return err
		}
		data, err := json.Marshal(identity)
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o600)
	case "updated":
		prior, err := readIdentity(path)
		if err != nil {
			return err
		}
		current, err := verifyPresent(ctx, client, prior.PrincipalARN, "unobin-editor",
			[]string{"unobin-editors", "unobin-readers"},
			map[string]string{"add": "", "change": "new", "keep": "1"},
		)
		if err != nil {
			return err
		}
		if current.ARN != prior.ARN || !current.CreatedAt.Equal(prior.CreatedAt) {
			return fmt.Errorf("access entry was replaced: got %+v, want %+v", current, prior)
		}
		return nil
	case "destroyed":
		prior, err := readIdentity(path)
		if err != nil {
			return err
		}
		entry, err := describeEntry(ctx, client, prior.PrincipalARN)
		if err == nil && entry != nil {
			return fmt.Errorf("access entry %s still exists", prior.ARN)
		}
		return err
	default:
		return fmt.Errorf("unexpected VERIFY_PHASE %q", phase)
	}
}

func verifyPresent(
	ctx context.Context, client *eks.Client, principal, username string,
	groups []string, tags map[string]string,
) (entryIdentity, error) {
	entry, err := describeEntry(ctx, client, principal)
	if err != nil {
		return entryIdentity{}, err
	}
	if entry == nil {
		return entryIdentity{}, fmt.Errorf("access entry for %s is missing", principal)
	}
	arn := aws.ToString(entry.AccessEntryArn)
	if arn == "" || entry.CreatedAt == nil || entry.CreatedAt.IsZero() {
		return entryIdentity{}, errors.New("access entry has no ARN or creation time")
	}
	if got := aws.ToString(entry.ClusterName); got != clusterName {
		return entryIdentity{}, fmt.Errorf("cluster name is %q, want %q", got, clusterName)
	}
	if got := aws.ToString(entry.PrincipalArn); got != principal {
		return entryIdentity{}, fmt.Errorf("principal is %q, want %q", got, principal)
	}
	if got := aws.ToString(entry.Type); got != "STANDARD" {
		return entryIdentity{}, fmt.Errorf("entry type is %q, want STANDARD", got)
	}
	if got := aws.ToString(entry.Username); got != username {
		return entryIdentity{}, fmt.Errorf("username is %q, want %q", got, username)
	}
	if got := slices.Sorted(slices.Values(entry.KubernetesGroups)); !slices.Equal(got, groups) {
		return entryIdentity{}, fmt.Errorf("groups are %v, want %v", got, groups)
	}
	list, err := client.ListTagsForResource(ctx, &eks.ListTagsForResourceInput{
		ResourceArn: aws.String(arn),
	})
	if err != nil {
		return entryIdentity{}, fmt.Errorf("list access entry tags: %w", err)
	}
	actual := list.Tags
	maps.DeleteFunc(actual, func(key, _ string) bool { return strings.HasPrefix(key, "aws:") })
	if !maps.Equal(actual, tags) {
		return entryIdentity{}, fmt.Errorf("tags are %v, want %v", actual, tags)
	}
	return entryIdentity{PrincipalARN: principal, ARN: arn, CreatedAt: *entry.CreatedAt}, nil
}

func describeEntry(
	ctx context.Context, client *eks.Client, principal string,
) (*ekstypes.AccessEntry, error) {
	output, err := client.DescribeAccessEntry(ctx, &eks.DescribeAccessEntryInput{
		ClusterName: aws.String(clusterName), PrincipalArn: aws.String(principal),
	})
	var missing *ekstypes.ResourceNotFoundException
	if errors.As(err, &missing) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("describe EKS access entry: %w", err)
	}
	return output.AccessEntry, nil
}

func readIdentity(path string) (entryIdentity, error) {
	var identity entryIdentity
	data, err := os.ReadFile(path)
	if err != nil {
		return identity, err
	}
	err = json.Unmarshal(data, &identity)
	return identity, err
}
