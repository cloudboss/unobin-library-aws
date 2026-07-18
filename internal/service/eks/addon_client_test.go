package eks

import (
	"context"
	"errors"
	"maps"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
)

type addonCreateResult struct {
	output *eks.CreateAddonOutput
	err    error
}

type addonDescribeResult struct {
	output *eks.DescribeAddonOutput
	err    error
}

type addonUpdateResult struct {
	output *eks.UpdateAddonOutput
	err    error
}

type addonDescribeUpdateResult struct {
	output *eks.DescribeUpdateOutput
	err    error
}

type addonDeleteResult struct {
	output *eks.DeleteAddonOutput
	err    error
}

type fakeAddonClient struct {
	createResults []addonCreateResult
	createHook    func()
	createInputs  []*eks.CreateAddonInput

	describeResults  []addonDescribeResult
	describeFallback *eks.DescribeAddonOutput
	describeInputs   []*eks.DescribeAddonInput

	updateResults []addonUpdateResult
	updateInputs  []*eks.UpdateAddonInput

	describeUpdateResults  []addonDescribeUpdateResult
	describeUpdateFallback *eks.DescribeUpdateOutput
	describeUpdateInputs   []*eks.DescribeUpdateInput

	deleteResults []addonDeleteResult
	deleteInputs  []*eks.DeleteAddonInput

	listTags       map[string]string
	listTagsInputs []*eks.ListTagsForResourceInput
	tagInputs      []*eks.TagResourceInput
	untagInputs    []*eks.UntagResourceInput

	calls []string
}

func (c *fakeAddonClient) CreateAddon(
	_ context.Context,
	input *eks.CreateAddonInput,
	_ ...func(*eks.Options),
) (*eks.CreateAddonOutput, error) {
	c.calls = append(c.calls, "create")
	c.createInputs = append(c.createInputs, input)
	if c.createHook != nil {
		c.createHook()
	}
	if len(c.createResults) == 0 {
		return nil, errors.New("unexpected CreateAddon call")
	}
	result := c.createResults[0]
	c.createResults = c.createResults[1:]
	return result.output, result.err
}

func (c *fakeAddonClient) DescribeAddon(
	_ context.Context,
	input *eks.DescribeAddonInput,
	_ ...func(*eks.Options),
) (*eks.DescribeAddonOutput, error) {
	c.calls = append(c.calls, "describe")
	c.describeInputs = append(c.describeInputs, input)
	if len(c.describeResults) == 0 {
		if c.describeFallback != nil {
			return c.describeFallback, nil
		}
		return nil, errors.New("unexpected DescribeAddon call")
	}
	result := c.describeResults[0]
	c.describeResults = c.describeResults[1:]
	return result.output, result.err
}

func (c *fakeAddonClient) UpdateAddon(
	_ context.Context,
	input *eks.UpdateAddonInput,
	_ ...func(*eks.Options),
) (*eks.UpdateAddonOutput, error) {
	c.calls = append(c.calls, "update")
	c.updateInputs = append(c.updateInputs, input)
	if len(c.updateResults) == 0 {
		return nil, errors.New("unexpected UpdateAddon call")
	}
	result := c.updateResults[0]
	c.updateResults = c.updateResults[1:]
	return result.output, result.err
}

func (c *fakeAddonClient) DescribeUpdate(
	_ context.Context,
	input *eks.DescribeUpdateInput,
	_ ...func(*eks.Options),
) (*eks.DescribeUpdateOutput, error) {
	c.calls = append(c.calls, "describe-update")
	c.describeUpdateInputs = append(c.describeUpdateInputs, input)
	if len(c.describeUpdateResults) == 0 {
		if c.describeUpdateFallback != nil {
			return c.describeUpdateFallback, nil
		}
		return nil, errors.New("unexpected DescribeUpdate call")
	}
	result := c.describeUpdateResults[0]
	c.describeUpdateResults = c.describeUpdateResults[1:]
	return result.output, result.err
}

func (c *fakeAddonClient) DeleteAddon(
	_ context.Context,
	input *eks.DeleteAddonInput,
	_ ...func(*eks.Options),
) (*eks.DeleteAddonOutput, error) {
	c.calls = append(c.calls, "delete")
	c.deleteInputs = append(c.deleteInputs, input)
	if len(c.deleteResults) == 0 {
		return &eks.DeleteAddonOutput{}, nil
	}
	result := c.deleteResults[0]
	c.deleteResults = c.deleteResults[1:]
	return result.output, result.err
}

func (c *fakeAddonClient) ListTagsForResource(
	_ context.Context,
	input *eks.ListTagsForResourceInput,
	_ ...func(*eks.Options),
) (*eks.ListTagsForResourceOutput, error) {
	c.calls = append(c.calls, "list-tags")
	c.listTagsInputs = append(c.listTagsInputs, input)
	return &eks.ListTagsForResourceOutput{Tags: maps.Clone(c.listTags)}, nil
}

func (c *fakeAddonClient) TagResource(
	_ context.Context,
	input *eks.TagResourceInput,
	_ ...func(*eks.Options),
) (*eks.TagResourceOutput, error) {
	c.calls = append(c.calls, "tag")
	c.tagInputs = append(c.tagInputs, input)
	return &eks.TagResourceOutput{}, nil
}

func (c *fakeAddonClient) UntagResource(
	_ context.Context,
	input *eks.UntagResourceInput,
	_ ...func(*eks.Options),
) (*eks.UntagResourceOutput, error) {
	c.calls = append(c.calls, "untag")
	c.untagInputs = append(c.untagInputs, input)
	return &eks.UntagResourceOutput{}, nil
}

func fixedAddonToken(token string) func() (string, error) {
	return func() (string, error) { return token, nil }
}

func addonInputNames(input *eks.DescribeAddonInput) (string, string) {
	return aws.ToString(input.ClusterName), aws.ToString(input.AddonName)
}
