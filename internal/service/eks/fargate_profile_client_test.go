package eks

import (
	"context"
	"errors"
	"maps"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

type fargateProfileDescribeResult struct {
	output *eks.DescribeFargateProfileOutput
	err    error
}

type fakeFargateProfileClient struct {
	mu sync.Mutex

	createOutputs []*eks.CreateFargateProfileOutput
	createErrors  []error
	createInputs  []*eks.CreateFargateProfileInput
	createFn      func(int)

	describeResults  []fargateProfileDescribeResult
	describeFallback *eks.DescribeFargateProfileOutput
	describeInputs   []*eks.DescribeFargateProfileInput

	deleteOutput *eks.DeleteFargateProfileOutput
	deleteErr    error
	deleteInputs []*eks.DeleteFargateProfileInput

	listTags       map[string]string
	listTagsErr    error
	listTagsInputs []*eks.ListTagsForResourceInput
	tagErr         error
	tagInputs      []*eks.TagResourceInput
	untagErr       error
	untagInputs    []*eks.UntagResourceInput

	calls []string
}

func (c *fakeFargateProfileClient) CreateFargateProfile(
	_ context.Context,
	input *eks.CreateFargateProfileInput,
	_ ...func(*eks.Options),
) (*eks.CreateFargateProfileOutput, error) {
	c.mu.Lock()
	c.calls = append(c.calls, "create")
	c.createInputs = append(c.createInputs, input)
	call := len(c.createInputs)
	var output *eks.CreateFargateProfileOutput
	var err error
	if len(c.createOutputs) > 0 {
		output = c.createOutputs[0]
		c.createOutputs = c.createOutputs[1:]
	}
	if len(c.createErrors) > 0 {
		err = c.createErrors[0]
		c.createErrors = c.createErrors[1:]
	}
	fn := c.createFn
	c.mu.Unlock()
	if fn != nil {
		fn(call)
	}
	return output, err
}

func (c *fakeFargateProfileClient) DescribeFargateProfile(
	_ context.Context,
	input *eks.DescribeFargateProfileInput,
	_ ...func(*eks.Options),
) (*eks.DescribeFargateProfileOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, "describe")
	c.describeInputs = append(c.describeInputs, input)
	if len(c.describeResults) == 0 {
		if c.describeFallback != nil {
			return c.describeFallback, nil
		}
		return nil, errors.New("unexpected DescribeFargateProfile call")
	}
	result := c.describeResults[0]
	c.describeResults = c.describeResults[1:]
	return result.output, result.err
}

func (c *fakeFargateProfileClient) DeleteFargateProfile(
	_ context.Context,
	input *eks.DeleteFargateProfileInput,
	_ ...func(*eks.Options),
) (*eks.DeleteFargateProfileOutput, error) {
	c.calls = append(c.calls, "delete")
	c.deleteInputs = append(c.deleteInputs, input)
	return c.deleteOutput, c.deleteErr
}

func (c *fakeFargateProfileClient) ListTagsForResource(
	_ context.Context,
	input *eks.ListTagsForResourceInput,
	_ ...func(*eks.Options),
) (*eks.ListTagsForResourceOutput, error) {
	c.calls = append(c.calls, "list-tags")
	c.listTagsInputs = append(c.listTagsInputs, input)
	if c.listTagsErr != nil {
		return nil, c.listTagsErr
	}
	return &eks.ListTagsForResourceOutput{Tags: maps.Clone(c.listTags)}, nil
}

func (c *fakeFargateProfileClient) TagResource(
	_ context.Context,
	input *eks.TagResourceInput,
	_ ...func(*eks.Options),
) (*eks.TagResourceOutput, error) {
	c.calls = append(c.calls, "tag")
	c.tagInputs = append(c.tagInputs, input)
	return &eks.TagResourceOutput{}, c.tagErr
}

func (c *fakeFargateProfileClient) UntagResource(
	_ context.Context,
	input *eks.UntagResourceInput,
	_ ...func(*eks.Options),
) (*eks.UntagResourceOutput, error) {
	c.calls = append(c.calls, "untag")
	c.untagInputs = append(c.untagInputs, input)
	return &eks.UntagResourceOutput{}, c.untagErr
}

func sdkFargateProfile(status ekstypes.FargateProfileStatus) *ekstypes.FargateProfile {
	return &ekstypes.FargateProfile{
		ClusterName:         aws.String("prior-cluster"),
		FargateProfileName:  aws.String("prior-pods"),
		FargateProfileArn:   aws.String("arn:fargate-profile"),
		PodExecutionRoleArn: aws.String("arn:role"),
		Selectors: []ekstypes.FargateProfileSelector{{
			Namespace: aws.String("default"),
			Labels:    map[string]string{"app": "api"},
		}},
		Status:  status,
		Subnets: []string{"subnet-a", "subnet-b"},
	}
}

func fargateProfileDescribe(
	profile *ekstypes.FargateProfile,
) fargateProfileDescribeResult {
	return fargateProfileDescribeResult{
		output: &eks.DescribeFargateProfileOutput{FargateProfile: profile},
	}
}

func fargateProfileNotFoundError() error {
	return &ekstypes.ResourceNotFoundException{Message: aws.String("missing")}
}

func fargateProfileRetryableRoleError() error {
	return &ekstypes.InvalidParameterException{
		Message: aws.String("Misconfigured PodExecutionRole Trust Policy"),
	}
}
