package eks

import (
	"context"
	"errors"
	"maps"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

type nodeGroupDescribeResult struct {
	output *eks.DescribeNodegroupOutput
	err    error
}

type nodeGroupUpdateResult struct {
	output *eks.DescribeUpdateOutput
	err    error
}

type fakeNodeGroupClient struct {
	createOutput *eks.CreateNodegroupOutput
	createErr    error
	createInputs []*eks.CreateNodegroupInput

	describeResults  []nodeGroupDescribeResult
	describeFallback *eks.DescribeNodegroupOutput
	describeInputs   []*eks.DescribeNodegroupInput

	versionOutputs []*eks.UpdateNodegroupVersionOutput
	versionErrors  []error
	versionInputs  []*eks.UpdateNodegroupVersionInput

	configOutputs []*eks.UpdateNodegroupConfigOutput
	configErrors  []error
	configInputs  []*eks.UpdateNodegroupConfigInput

	updateResults  []nodeGroupUpdateResult
	updateFallback *eks.DescribeUpdateOutput
	updateInputs   []*eks.DescribeUpdateInput

	deleteOutput *eks.DeleteNodegroupOutput
	deleteErr    error
	deleteInputs []*eks.DeleteNodegroupInput

	listTags       map[string]string
	listTagsErr    error
	listTagsInputs []*eks.ListTagsForResourceInput
	tagErr         error
	tagInputs      []*eks.TagResourceInput
	untagErr       error
	untagInputs    []*eks.UntagResourceInput

	calls []string
}

func (c *fakeNodeGroupClient) CreateNodegroup(
	_ context.Context,
	input *eks.CreateNodegroupInput,
	_ ...func(*eks.Options),
) (*eks.CreateNodegroupOutput, error) {
	c.calls = append(c.calls, "create")
	c.createInputs = append(c.createInputs, input)
	return c.createOutput, c.createErr
}

func (c *fakeNodeGroupClient) DescribeNodegroup(
	_ context.Context,
	input *eks.DescribeNodegroupInput,
	_ ...func(*eks.Options),
) (*eks.DescribeNodegroupOutput, error) {
	c.calls = append(c.calls, "describe")
	c.describeInputs = append(c.describeInputs, input)
	if len(c.describeResults) == 0 {
		if c.describeFallback != nil {
			return c.describeFallback, nil
		}
		return nil, errors.New("unexpected DescribeNodegroup call")
	}
	result := c.describeResults[0]
	c.describeResults = c.describeResults[1:]
	return result.output, result.err
}

func (c *fakeNodeGroupClient) UpdateNodegroupVersion(
	_ context.Context,
	input *eks.UpdateNodegroupVersionInput,
	_ ...func(*eks.Options),
) (*eks.UpdateNodegroupVersionOutput, error) {
	c.calls = append(c.calls, "version")
	c.versionInputs = append(c.versionInputs, input)
	if len(c.versionOutputs) == 0 && len(c.versionErrors) == 0 {
		return nil, errors.New("unexpected UpdateNodegroupVersion call")
	}
	var output *eks.UpdateNodegroupVersionOutput
	var err error
	if len(c.versionOutputs) > 0 {
		output = c.versionOutputs[0]
		c.versionOutputs = c.versionOutputs[1:]
	}
	if len(c.versionErrors) > 0 {
		err = c.versionErrors[0]
		c.versionErrors = c.versionErrors[1:]
	}
	return output, err
}

func (c *fakeNodeGroupClient) UpdateNodegroupConfig(
	_ context.Context,
	input *eks.UpdateNodegroupConfigInput,
	_ ...func(*eks.Options),
) (*eks.UpdateNodegroupConfigOutput, error) {
	c.calls = append(c.calls, "config")
	c.configInputs = append(c.configInputs, input)
	if len(c.configOutputs) == 0 && len(c.configErrors) == 0 {
		return nil, errors.New("unexpected UpdateNodegroupConfig call")
	}
	var output *eks.UpdateNodegroupConfigOutput
	var err error
	if len(c.configOutputs) > 0 {
		output = c.configOutputs[0]
		c.configOutputs = c.configOutputs[1:]
	}
	if len(c.configErrors) > 0 {
		err = c.configErrors[0]
		c.configErrors = c.configErrors[1:]
	}
	return output, err
}

func (c *fakeNodeGroupClient) DescribeUpdate(
	_ context.Context,
	input *eks.DescribeUpdateInput,
	_ ...func(*eks.Options),
) (*eks.DescribeUpdateOutput, error) {
	id := aws.ToString(input.UpdateId)
	c.calls = append(c.calls, "wait:"+id)
	c.updateInputs = append(c.updateInputs, input)
	if len(c.updateResults) == 0 {
		if c.updateFallback != nil {
			return c.updateFallback, nil
		}
		return nil, errors.New("unexpected DescribeUpdate call")
	}
	result := c.updateResults[0]
	c.updateResults = c.updateResults[1:]
	return result.output, result.err
}

func (c *fakeNodeGroupClient) DeleteNodegroup(
	_ context.Context,
	input *eks.DeleteNodegroupInput,
	_ ...func(*eks.Options),
) (*eks.DeleteNodegroupOutput, error) {
	c.calls = append(c.calls, "delete")
	c.deleteInputs = append(c.deleteInputs, input)
	return c.deleteOutput, c.deleteErr
}

func (c *fakeNodeGroupClient) ListTagsForResource(
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

func (c *fakeNodeGroupClient) TagResource(
	_ context.Context,
	input *eks.TagResourceInput,
	_ ...func(*eks.Options),
) (*eks.TagResourceOutput, error) {
	c.calls = append(c.calls, "tag")
	c.tagInputs = append(c.tagInputs, input)
	return &eks.TagResourceOutput{}, c.tagErr
}

func (c *fakeNodeGroupClient) UntagResource(
	_ context.Context,
	input *eks.UntagResourceInput,
	_ ...func(*eks.Options),
) (*eks.UntagResourceOutput, error) {
	c.calls = append(c.calls, "untag")
	c.untagInputs = append(c.untagInputs, input)
	return &eks.UntagResourceOutput{}, c.untagErr
}

func sdkNodeGroup(status ekstypes.NodegroupStatus) *ekstypes.Nodegroup {
	return &ekstypes.Nodegroup{
		ClusterName:   aws.String("prior-cluster"),
		NodegroupName: aws.String("prior-workers"),
		Status:        status,
	}
}

func nodeGroupDescribe(group *ekstypes.Nodegroup) nodeGroupDescribeResult {
	return nodeGroupDescribeResult{output: &eks.DescribeNodegroupOutput{Nodegroup: group}}
}

func nodeGroupNotFoundError() error {
	return &ekstypes.ResourceNotFoundException{Message: aws.String("missing")}
}
