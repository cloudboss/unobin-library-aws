package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var scenarioCreatedAt = time.Date(2026, time.July, 17, 12, 30, 0, 0, time.UTC)

func TestScenarioUsesVPCCNIAddon(t *testing.T) {
	assert.Equal(t, "vpc-cni", addonName)
}

func TestVerifyAppliedAndUpdatedKeepIdentityAndIgnoreReservedTags(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	initialTags := map[string]string{
		"change": "old", "keep": "1", "remove": "yes",
	}
	applied := scenarioClient(initialTags, initialTags, initialTags, initialTags)
	require.NoError(t, verifyApplied(context.Background(), applied))

	updated := scenarioClient(map[string]string{
		"add": "yes", "change": "new", "keep": "1",
		"aws:cluster": "service",
	}, map[string]string{
		"add": "yes", "change": "new", "keep": "1",
		"aws:node-group": "service",
	}, map[string]string{
		"add": "yes", "change": "new", "keep": "1",
		"aws:fargate-profile": "service",
	}, map[string]string{
		"add": "yes", "change": "new", "keep": "1",
		"aws:addon": "service",
	})
	require.NoError(t, verifyUpdated(context.Background(), updated))
}

func TestVerifyAppliedAllowsEmptyAddonVersion(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	initialTags := map[string]string{
		"change": "old", "keep": "1", "remove": "yes",
	}
	client := scenarioClient(initialTags, initialTags, initialTags, initialTags)
	client.addon.AddonVersion = nil

	require.NoError(t, verifyApplied(context.Background(), client))
}

func TestVerifyUpdatedRejectsReplacement(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	require.NoError(t, writeIdentity(clusterIdentity{
		ARN:                "arn:aws:eks:us-east-1:123456789012:cluster/original",
		CreatedAt:          scenarioCreatedAt.Format(time.RFC3339Nano),
		NodeGroupARN:       "arn:node-group",
		NodeGroupCreatedAt: scenarioCreatedAt.Format(time.RFC3339Nano),
		FargateProfileARN:  "arn:fargate-profile",
		AddonARN:           "arn:addon",
		AddonCreatedAt:     scenarioCreatedAt.Format(time.RFC3339Nano),
	}))
	client := scenarioClient(
		map[string]string{"add": "yes", "change": "new", "keep": "1"},
		map[string]string{"add": "yes", "change": "new", "keep": "1"},
		map[string]string{"add": "yes", "change": "new", "keep": "1"},
		map[string]string{"add": "yes", "change": "new", "keep": "1"},
	)

	err := verifyUpdated(context.Background(), client)

	require.Error(t, err)
	assert.ErrorContains(t, err, "identity changed")
}

func TestVerifyDestroyedAcceptsTypedNotFound(t *testing.T) {
	client := &fakeVerifierClient{describeError: &ekstypes.ResourceNotFoundException{
		Message: aws.String("missing"),
	}}
	require.NoError(t, verifyDestroyed(context.Background(), client))
}

func TestVerifyDestroyedAcceptsClientNotFound(t *testing.T) {
	client := &fakeVerifierClient{describeError: &ekstypes.ClientException{
		Message: aws.String("No cluster found for name: " + clusterName),
	}}
	require.NoError(t, verifyDestroyed(context.Background(), client))
}

func TestVerifyDestroyedPropagatesUnrelatedError(t *testing.T) {
	sentinel := errors.New("access denied")
	err := verifyDestroyed(
		context.Background(), &fakeVerifierClient{describeError: sentinel},
	)
	require.ErrorIs(t, err, sentinel)
}

type fakeVerifierClient struct {
	cluster        *ekstypes.Cluster
	nodeGroup      *ekstypes.Nodegroup
	fargateProfile *ekstypes.FargateProfile
	addon          *ekstypes.Addon
	describeError  error
	nodeGroupError error
	tagsByARN      map[string]map[string]string
}

func (c *fakeVerifierClient) DescribeAddon(
	context.Context,
	*eks.DescribeAddonInput,
	...func(*eks.Options),
) (*eks.DescribeAddonOutput, error) {
	return &eks.DescribeAddonOutput{Addon: c.addon}, nil
}

func (c *fakeVerifierClient) DescribeNodegroup(
	context.Context,
	*eks.DescribeNodegroupInput,
	...func(*eks.Options),
) (*eks.DescribeNodegroupOutput, error) {
	if c.nodeGroupError != nil {
		return nil, c.nodeGroupError
	}
	return &eks.DescribeNodegroupOutput{Nodegroup: c.nodeGroup}, nil
}

func (c *fakeVerifierClient) DescribeFargateProfile(
	context.Context,
	*eks.DescribeFargateProfileInput,
	...func(*eks.Options),
) (*eks.DescribeFargateProfileOutput, error) {
	return &eks.DescribeFargateProfileOutput{FargateProfile: c.fargateProfile}, nil
}

func (c *fakeVerifierClient) DescribeCluster(
	context.Context,
	*eks.DescribeClusterInput,
	...func(*eks.Options),
) (*eks.DescribeClusterOutput, error) {
	if c.describeError != nil {
		return nil, c.describeError
	}
	return &eks.DescribeClusterOutput{Cluster: c.cluster}, nil
}

func (c *fakeVerifierClient) ListTagsForResource(
	_ context.Context,
	input *eks.ListTagsForResourceInput,
	_ ...func(*eks.Options),
) (*eks.ListTagsForResourceOutput, error) {
	return &eks.ListTagsForResourceOutput{
		Tags: c.tagsByARN[aws.ToString(input.ResourceArn)],
	}, nil
}

func scenarioClient(
	clusterTags map[string]string,
	nodeGroupTags map[string]string,
	fargateProfileTags map[string]string,
	addonTags map[string]string,
) *fakeVerifierClient {
	clusterARN := "arn:aws:eks:us-east-1:123456789012:cluster/example"
	nodeGroupARN := "arn:aws:eks:us-east-1:123456789012:nodegroup/example/workers/id"
	fargateProfileARN := "arn:aws:eks:us-east-1:123456789012:fargateprofile/example/pods/id"
	addonARN := "arn:aws:eks:us-east-1:123456789012:addon/example/vpc-cni/id"
	return &fakeVerifierClient{
		tagsByARN: map[string]map[string]string{
			clusterARN:        clusterTags,
			nodeGroupARN:      nodeGroupTags,
			fargateProfileARN: fargateProfileTags,
			addonARN:          addonTags,
		},
		cluster: &ekstypes.Cluster{
			Name:            aws.String(clusterName),
			Arn:             aws.String(clusterARN),
			CreatedAt:       &scenarioCreatedAt,
			Endpoint:        aws.String("https://cluster.example"),
			PlatformVersion: aws.String("eks.1"),
			Status:          ekstypes.ClusterStatusActive,
			Version:         aws.String("1.33"),
		},
		nodeGroup: &ekstypes.Nodegroup{
			ClusterName:   aws.String(clusterName),
			CreatedAt:     &scenarioCreatedAt,
			NodegroupArn:  aws.String(nodeGroupARN),
			NodegroupName: aws.String(nodeGroupName),
			NodeRole:      aws.String("arn:aws:iam::123456789012:role/node"),
			ScalingConfig: &ekstypes.NodegroupScalingConfig{
				DesiredSize: aws.Int32(0), MinSize: aws.Int32(0), MaxSize: aws.Int32(1),
			},
			Status:  ekstypes.NodegroupStatusActive,
			Subnets: []string{"subnet-a", "subnet-b"},
		},
		fargateProfile: &ekstypes.FargateProfile{
			ClusterName:         aws.String(clusterName),
			FargateProfileArn:   aws.String(fargateProfileARN),
			FargateProfileName:  aws.String(fargateProfileName),
			PodExecutionRoleArn: aws.String("arn:aws:iam::123456789012:role/fargate"),
			Selectors: []ekstypes.FargateProfileSelector{{
				Namespace: aws.String("unobin-fargate"),
			}},
			Status:  ekstypes.FargateProfileStatusActive,
			Subnets: []string{"subnet-a", "subnet-b"},
		},
		addon: &ekstypes.Addon{
			AddonArn:     aws.String(addonARN),
			AddonName:    aws.String(addonName),
			AddonVersion: aws.String("v1.12.0-eksbuild.1"),
			ClusterName:  aws.String(clusterName),
			CreatedAt:    &scenarioCreatedAt,
			Status:       ekstypes.AddonStatusActive,
		},
	}
}
