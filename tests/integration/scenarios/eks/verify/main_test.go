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

func TestVerifyAppliedAndUpdatedKeepClusterIdentity(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	applied := scenarioClient(map[string]string{
		"change": "old", "keep": "1", "remove": "yes",
	})
	require.NoError(t, verifyApplied(context.Background(), applied))

	updated := scenarioClient(map[string]string{
		"add": "yes", "change": "new", "keep": "1", "aws:retained": "service",
	})
	require.NoError(t, verifyUpdated(context.Background(), updated))
}

func TestVerifyUpdatedRejectsReplacement(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	require.NoError(t, writeIdentity(clusterIdentity{
		ARN:       "arn:aws:eks:us-east-1:123456789012:cluster/original",
		CreatedAt: scenarioCreatedAt.Format(time.RFC3339Nano),
	}))
	client := scenarioClient(map[string]string{
		"add": "yes", "change": "new", "keep": "1",
	})

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
	cluster       *ekstypes.Cluster
	describeError error
	tags          map[string]string
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
	context.Context,
	*eks.ListTagsForResourceInput,
	...func(*eks.Options),
) (*eks.ListTagsForResourceOutput, error) {
	return &eks.ListTagsForResourceOutput{Tags: c.tags}, nil
}

func scenarioClient(tags map[string]string) *fakeVerifierClient {
	return &fakeVerifierClient{
		tags: tags,
		cluster: &ekstypes.Cluster{
			Name:            aws.String(clusterName),
			Arn:             aws.String("arn:aws:eks:us-east-1:123456789012:cluster/example"),
			CreatedAt:       &scenarioCreatedAt,
			Endpoint:        aws.String("https://cluster.example"),
			PlatformVersion: aws.String("eks.1"),
			Status:          ekstypes.ClusterStatusActive,
			Version:         aws.String("1.33"),
		},
	}
}
