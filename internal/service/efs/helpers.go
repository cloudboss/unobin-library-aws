package efs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	efssdk "github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

type awsCfg = awscfg.Configuration

type fileSystemClient interface {
	CreateFileSystem(context.Context, *efssdk.CreateFileSystemInput,
		...func(*efssdk.Options)) (*efssdk.CreateFileSystemOutput, error)
	DescribeFileSystems(context.Context, *efssdk.DescribeFileSystemsInput,
		...func(*efssdk.Options)) (*efssdk.DescribeFileSystemsOutput, error)
	UpdateFileSystem(context.Context, *efssdk.UpdateFileSystemInput,
		...func(*efssdk.Options)) (*efssdk.UpdateFileSystemOutput, error)
	DeleteFileSystem(context.Context, *efssdk.DeleteFileSystemInput,
		...func(*efssdk.Options)) (*efssdk.DeleteFileSystemOutput, error)
	PutLifecycleConfiguration(context.Context, *efssdk.PutLifecycleConfigurationInput,
		...func(*efssdk.Options)) (*efssdk.PutLifecycleConfigurationOutput, error)
	DescribeLifecycleConfiguration(context.Context, *efssdk.DescribeLifecycleConfigurationInput,
		...func(*efssdk.Options)) (*efssdk.DescribeLifecycleConfigurationOutput, error)
	PutBackupPolicy(context.Context, *efssdk.PutBackupPolicyInput,
		...func(*efssdk.Options)) (*efssdk.PutBackupPolicyOutput, error)
	DescribeBackupPolicy(context.Context, *efssdk.DescribeBackupPolicyInput,
		...func(*efssdk.Options)) (*efssdk.DescribeBackupPolicyOutput, error)
	PutFileSystemPolicy(context.Context, *efssdk.PutFileSystemPolicyInput,
		...func(*efssdk.Options)) (*efssdk.PutFileSystemPolicyOutput, error)
	DescribeFileSystemPolicy(context.Context, *efssdk.DescribeFileSystemPolicyInput,
		...func(*efssdk.Options)) (*efssdk.DescribeFileSystemPolicyOutput, error)
	DeleteFileSystemPolicy(context.Context, *efssdk.DeleteFileSystemPolicyInput,
		...func(*efssdk.Options)) (*efssdk.DeleteFileSystemPolicyOutput, error)
	UpdateFileSystemProtection(context.Context, *efssdk.UpdateFileSystemProtectionInput,
		...func(*efssdk.Options)) (*efssdk.UpdateFileSystemProtectionOutput, error)
	CreateReplicationConfiguration(context.Context, *efssdk.CreateReplicationConfigurationInput,
		...func(*efssdk.Options)) (*efssdk.CreateReplicationConfigurationOutput, error)
	DescribeReplicationConfigurations(context.Context,
		*efssdk.DescribeReplicationConfigurationsInput,
		...func(*efssdk.Options)) (*efssdk.DescribeReplicationConfigurationsOutput, error)
	DeleteReplicationConfiguration(context.Context, *efssdk.DeleteReplicationConfigurationInput,
		...func(*efssdk.Options)) (*efssdk.DeleteReplicationConfigurationOutput, error)
	ListTagsForResource(context.Context, *efssdk.ListTagsForResourceInput,
		...func(*efssdk.Options)) (*efssdk.ListTagsForResourceOutput, error)
	TagResource(context.Context, *efssdk.TagResourceInput,
		...func(*efssdk.Options)) (*efssdk.TagResourceOutput, error)
	UntagResource(context.Context, *efssdk.UntagResourceInput,
		...func(*efssdk.Options)) (*efssdk.UntagResourceOutput, error)
}

type fileSystemClientProvider interface {
	Source() fileSystemClient
	ForRegion(string) fileSystemClient
	Region() string
}

type sdkFileSystemClients struct {
	config aws.Config
}

func newFileSystemClients(
	ctx context.Context,
	cfg *awsCfg,
) (*sdkFileSystemClients, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &sdkFileSystemClients{config: loaded}, nil
}

func (c *sdkFileSystemClients) Source() fileSystemClient {
	return efssdk.NewFromConfig(c.config)
}

func (c *sdkFileSystemClients) ForRegion(region string) fileSystemClient {
	config := c.config
	config.Region = region
	return efssdk.NewFromConfig(config)
}

func (c *sdkFileSystemClients) Region() string { return c.config.Region }

func newCreationToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate creation token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func isFileSystemNotFound(err error) bool {
	var notFound *efstypes.FileSystemNotFound
	return errors.As(err, &notFound)
}

func isPolicyNotFound(err error) bool {
	var notFound *efstypes.PolicyNotFound
	return errors.As(err, &notFound)
}

func isReplicationNotFound(err error) bool {
	var notFound *efstypes.ReplicationNotFound
	return errors.As(err, &notFound)
}

func isFileSystemPolicyRetryable(err error) bool {
	var invalid *efstypes.InvalidPolicyException
	return errors.As(err, &invalid) && strings.Contains(
		invalid.ErrorMessage(), "Policy contains invalid Principal block")
}

func fileSystemDNSName(fileSystemID string, region string) string {
	dnsSuffix := "amazonaws.com"
	for _, partition := range endpoints.DefaultPartitions() {
		if _, ok := partition.Regions()[region]; ok || partition.RegionRegex().MatchString(region) {
			dnsSuffix = partition.DNSSuffix()
			break
		}
	}
	return fmt.Sprintf("%s.efs.%s.%s", fileSystemID, region, dnsSuffix)
}

func parsedARN(value string) (awsarn.ARN, bool) {
	parsed, err := awsarn.Parse(value)
	return parsed, err == nil
}
