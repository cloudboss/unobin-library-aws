package eks

import (
	"context"
	"errors"
	"maps"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

type createResult struct {
	output *eks.CreateClusterOutput
	err    error
}

type fakeEKSClient struct {
	createResults       []createResult
	describeResults     []clusterDescribeResult
	deleteErrors        []error
	calls               []string
	deletedNames        []string
	versionInputs       []*eks.UpdateClusterVersionInput
	configInputs        []*eks.UpdateClusterConfigInput
	encryptionInputs    []*eks.AssociateEncryptionConfigInput
	listTags            map[string]string
	listTagsARN         string
	untagARN            string
	untagKeys           []string
	tagARN              string
	tagValues           map[string]string
	malformedUpdateKind string
}

func (c *fakeEKSClient) CreateCluster(
	_ context.Context,
	_ *eks.CreateClusterInput,
	_ ...func(*eks.Options),
) (*eks.CreateClusterOutput, error) {
	c.calls = append(c.calls, "create")
	if len(c.createResults) == 0 {
		return nil, errors.New("unexpected CreateCluster call")
	}
	result := c.createResults[0]
	c.createResults = c.createResults[1:]
	return result.output, result.err
}

func (c *fakeEKSClient) DescribeCluster(
	_ context.Context,
	_ *eks.DescribeClusterInput,
	_ ...func(*eks.Options),
) (*eks.DescribeClusterOutput, error) {
	c.calls = append(c.calls, "describe")
	if len(c.describeResults) == 0 {
		return nil, errors.New("unexpected DescribeCluster call")
	}
	result := c.describeResults[0]
	c.describeResults = c.describeResults[1:]
	if result.err != nil {
		return nil, result.err
	}
	return &eks.DescribeClusterOutput{Cluster: result.cluster}, nil
}

func (c *fakeEKSClient) UpdateClusterVersion(
	_ context.Context,
	input *eks.UpdateClusterVersionInput,
	_ ...func(*eks.Options),
) (*eks.UpdateClusterVersionOutput, error) {
	c.calls = append(c.calls, "version")
	c.versionInputs = append(c.versionInputs, input)
	return &eks.UpdateClusterVersionOutput{Update: c.update("version")}, nil
}

func (c *fakeEKSClient) UpdateClusterConfig(
	_ context.Context,
	input *eks.UpdateClusterConfigInput,
	_ ...func(*eks.Options),
) (*eks.UpdateClusterConfigOutput, error) {
	kind := clusterConfigUpdateKind(input)
	c.calls = append(c.calls, kind)
	c.configInputs = append(c.configInputs, input)
	return &eks.UpdateClusterConfigOutput{Update: c.update(kind)}, nil
}

func (c *fakeEKSClient) AssociateEncryptionConfig(
	_ context.Context,
	input *eks.AssociateEncryptionConfigInput,
	_ ...func(*eks.Options),
) (*eks.AssociateEncryptionConfigOutput, error) {
	c.calls = append(c.calls, "encryption")
	c.encryptionInputs = append(c.encryptionInputs, input)
	return &eks.AssociateEncryptionConfigOutput{Update: c.update("encryption")}, nil
}

func (c *fakeEKSClient) DescribeUpdate(
	_ context.Context,
	input *eks.DescribeUpdateInput,
	_ ...func(*eks.Options),
) (*eks.DescribeUpdateOutput, error) {
	id := stringValue(input.UpdateId)
	c.calls = append(c.calls, "wait:"+id)
	return &eks.DescribeUpdateOutput{Update: &ekstypes.Update{
		Id: aws.String(id), Status: ekstypes.UpdateStatusSuccessful,
	}}, nil
}

func (c *fakeEKSClient) DeleteCluster(
	_ context.Context,
	input *eks.DeleteClusterInput,
	_ ...func(*eks.Options),
) (*eks.DeleteClusterOutput, error) {
	c.calls = append(c.calls, "delete")
	c.deletedNames = append(c.deletedNames, stringValue(input.Name))
	if len(c.deleteErrors) == 0 {
		return nil, errors.New("unexpected DeleteCluster call")
	}
	err := c.deleteErrors[0]
	c.deleteErrors = c.deleteErrors[1:]
	return &eks.DeleteClusterOutput{}, err
}

func (c *fakeEKSClient) ListTagsForResource(
	_ context.Context,
	input *eks.ListTagsForResourceInput,
	_ ...func(*eks.Options),
) (*eks.ListTagsForResourceOutput, error) {
	c.calls = append(c.calls, "list-tags")
	c.listTagsARN = stringValue(input.ResourceArn)
	return &eks.ListTagsForResourceOutput{Tags: maps.Clone(c.listTags)}, nil
}

func (c *fakeEKSClient) TagResource(
	_ context.Context,
	input *eks.TagResourceInput,
	_ ...func(*eks.Options),
) (*eks.TagResourceOutput, error) {
	c.calls = append(c.calls, "tag")
	c.tagARN = stringValue(input.ResourceArn)
	c.tagValues = maps.Clone(input.Tags)
	return &eks.TagResourceOutput{}, nil
}

func (c *fakeEKSClient) UntagResource(
	_ context.Context,
	input *eks.UntagResourceInput,
	_ ...func(*eks.Options),
) (*eks.UntagResourceOutput, error) {
	c.calls = append(c.calls, "untag")
	c.untagARN = stringValue(input.ResourceArn)
	c.untagKeys = append([]string(nil), input.TagKeys...)
	return &eks.UntagResourceOutput{}, nil
}

func (c *fakeEKSClient) update(kind string) *ekstypes.Update {
	if c.malformedUpdateKind == kind {
		return &ekstypes.Update{}
	}
	return &ekstypes.Update{Id: aws.String(kind)}
}

func clusterConfigUpdateKind(input *eks.UpdateClusterConfigInput) string {
	switch {
	case input.AccessConfig != nil:
		return "access"
	case input.ComputeConfig != nil:
		return "auto"
	case input.ControlPlaneScalingConfig != nil:
		return "scaling"
	case input.DeletionProtection != nil:
		return "deletion-protection"
	case input.Logging != nil:
		return "logging"
	case input.RemoteNetworkConfig != nil:
		return "remote-networks"
	case input.UpgradePolicy != nil:
		return "upgrade-policy"
	case input.ZonalShiftConfig != nil:
		return "zonal-shift"
	case input.ResourcesVpcConfig != nil:
		vpc := input.ResourcesVpcConfig
		switch {
		case vpc.ControlPlaneEgressMode != "":
			return "vpc-egress"
		case vpc.EndpointPrivateAccess != nil || vpc.EndpointPublicAccess != nil:
			return "vpc-endpoint"
		case vpc.SubnetIds != nil:
			return "vpc-subnets"
		case vpc.SecurityGroupIds != nil:
			return "vpc-security-groups"
		}
	}
	return "unknown-config"
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
