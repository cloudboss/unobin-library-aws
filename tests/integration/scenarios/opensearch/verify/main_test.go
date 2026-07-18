package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	opensearch "github.com/aws/aws-sdk-go-v2/service/opensearch"
	opensearchtypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyDestroyedChecksBothRecordedDomains(t *testing.T) {
	recorded := prepareDestroyedDomains(t)
	client := &fakeVerifierClient{}

	err := verifyDestroyed(context.Background(), client)

	require.NoError(t, err)
	assert.Equal(t, []string{recorded.Primary.Name, recorded.Clear.Name}, client.describeCalls)
	assert.Equal(t, []string{recorded.Primary.Name, recorded.Clear.Name}, client.configCalls)
	assert.Equal(t, []string{recorded.Primary.ARN, recorded.Clear.ARN}, client.listTagCalls)
}

func TestVerifyDestroyedAcceptsDeletedTagErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "direct typed not-found",
			err: &opensearchtypes.ResourceNotFoundException{
				Message: aws.String("missing"),
			},
		},
		{
			name: "wrapped typed not-found",
			err: fmt.Errorf("wrapped: %w", &opensearchtypes.ResourceNotFoundException{
				Message: aws.String("missing"),
			}),
		},
		{
			name: "direct exact validation response",
			err: &opensearchtypes.ValidationException{
				Message: aws.String("Invalid ARN. Domain not found."),
			},
		},
		{
			name: "wrapped exact validation response",
			err: fmt.Errorf("wrapped: %w", &opensearchtypes.ValidationException{
				Message: aws.String("Invalid ARN. Domain not found."),
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorded := prepareDestroyedDomains(t)
			client := &fakeVerifierClient{tagErrors: map[string]error{
				recorded.Primary.ARN: tt.err,
				recorded.Clear.ARN:   tt.err,
			}}

			err := verifyDestroyed(context.Background(), client)

			require.NoError(t, err)
			assert.Equal(t,
				[]string{recorded.Primary.ARN, recorded.Clear.ARN},
				client.listTagCalls,
			)
		})
	}
}

func TestVerifyDestroyedRejectsNonEmptyTags(t *testing.T) {
	recorded := prepareDestroyedDomains(t)
	client := &fakeVerifierClient{tagOutputs: map[string]*opensearch.ListTagsOutput{
		recorded.Primary.ARN: {
			TagList: []opensearchtypes.Tag{{
				Key: aws.String("still"), Value: aws.String("present"),
			}},
		},
	}}

	err := verifyDestroyed(context.Background(), client)

	require.Error(t, err)
	assert.ErrorContains(t, err, "deleted domain primary still has tags")
}

func TestVerifyDestroyedRejectsValidationNearMatches(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{name: "wrong case", message: "invalid ARN. Domain not found."},
		{name: "trailing space", message: "Invalid ARN. Domain not found. "},
		{name: "different resource", message: "Invalid ARN. Collection not found."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorded := prepareDestroyedDomains(t)
			validationErr := &opensearchtypes.ValidationException{
				Message: aws.String(tt.message),
			}
			client := &fakeVerifierClient{tagErrors: map[string]error{
				recorded.Primary.ARN: validationErr,
			}}

			err := verifyDestroyed(context.Background(), client)

			require.Error(t, err)
			assert.ErrorIs(t, err, validationErr)
		})
	}
}

func TestVerifyDestroyedRejectsUnrelatedTagError(t *testing.T) {
	recorded := prepareDestroyedDomains(t)
	sentinel := errors.New("access denied")
	client := &fakeVerifierClient{tagErrors: map[string]error{
		recorded.Primary.ARN: sentinel,
	}}

	err := verifyDestroyed(context.Background(), client)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func prepareDestroyedDomains(t *testing.T) recordedDomains {
	t.Helper()
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	recorded := recordedDomains{
		Primary: domainIdentity{
			Name: "primary",
			ID:   "primary-id",
			ARN:  "arn:aws:es:us-east-1:123456789012:domain/primary",
		},
		Clear: domainIdentity{
			Name: "clear",
			ID:   "clear-id",
			ARN:  "arn:aws:es:us-east-1:123456789012:domain/clear",
		},
	}
	require.NoError(t, writeRecordedDomains(recorded))
	return recorded
}

type fakeVerifierClient struct {
	describeCalls []string
	configCalls   []string
	listTagCalls  []string
	tagOutputs    map[string]*opensearch.ListTagsOutput
	tagErrors     map[string]error
}

func (c *fakeVerifierClient) DescribeDomain(
	_ context.Context,
	input *opensearch.DescribeDomainInput,
	_ ...func(*opensearch.Options),
) (*opensearch.DescribeDomainOutput, error) {
	c.describeCalls = append(c.describeCalls, aws.ToString(input.DomainName))
	return nil, &opensearchtypes.ResourceNotFoundException{Message: aws.String("missing")}
}

func (c *fakeVerifierClient) DescribeDomainConfig(
	_ context.Context,
	input *opensearch.DescribeDomainConfigInput,
	_ ...func(*opensearch.Options),
) (*opensearch.DescribeDomainConfigOutput, error) {
	c.configCalls = append(c.configCalls, aws.ToString(input.DomainName))
	return nil, &opensearchtypes.ResourceNotFoundException{Message: aws.String("missing")}
}

func (c *fakeVerifierClient) ListTags(
	_ context.Context,
	input *opensearch.ListTagsInput,
	_ ...func(*opensearch.Options),
) (*opensearch.ListTagsOutput, error) {
	arn := aws.ToString(input.ARN)
	c.listTagCalls = append(c.listTagCalls, arn)
	if err := c.tagErrors[arn]; err != nil {
		return nil, err
	}
	if output := c.tagOutputs[arn]; output != nil {
		return output, nil
	}
	return &opensearch.ListTagsOutput{}, nil
}
