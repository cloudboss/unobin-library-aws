package eks

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFargateProfileCreateInput(t *testing.T) {
	resource := validFargateProfileResource()
	resource.SubnetIDs = &[]string{"subnet-a", "subnet-b"}
	resource.Tags = &map[string]string{"env": "test", "aws:owned": "ignored"}

	input := resource.fargateProfileCreateInput("token")

	assert.Equal(t, "example", aws.ToString(input.ClusterName))
	assert.Equal(t, "pods", aws.ToString(input.FargateProfileName))
	assert.Equal(t, resource.PodExecutionRoleARN, aws.ToString(input.PodExecutionRoleArn))
	assert.Equal(t, "token", aws.ToString(input.ClientRequestToken))
	assert.Equal(t, []string{"subnet-a", "subnet-b"}, input.Subnets)
	assert.Equal(t, map[string]string{"env": "test"}, input.Tags)
	require.Len(t, input.Selectors, 1)
	assert.Equal(t, "default", aws.ToString(input.Selectors[0].Namespace))
	assert.Equal(t, map[string]string{"app": "api"}, input.Selectors[0].Labels)
}

func TestFargateProfileCreateInputPreservesOmittedSubnetsAsEmptySlice(t *testing.T) {
	input := validFargateProfileResource().fargateProfileCreateInput("token")

	assert.NotNil(t, input.Subnets)
	assert.Empty(t, input.Subnets)
	assert.Nil(t, input.Tags)
}

func TestFargateProfileSelectorConversion(t *testing.T) {
	inputs := fargateProfileSelectorInputs([]FargateProfileSelector{
		{Namespace: "default"},
		{Namespace: "api", Labels: &map[string]string{"tier": "backend"}},
	})

	require.Len(t, inputs, 2)
	assert.Equal(t, "default", aws.ToString(inputs[0].Namespace))
	assert.Nil(t, inputs[0].Labels)
	assert.Equal(t, "api", aws.ToString(inputs[1].Namespace))
	assert.Equal(t, map[string]string{"tier": "backend"}, inputs[1].Labels)

	outputs := fargateProfileSelectorOutput([]ekstypes.FargateProfileSelector{
		{Namespace: aws.String("default")},
		{Namespace: aws.String("api"), Labels: map[string]string{"tier": "backend"}},
	})
	assert.Equal(t, []FargateProfileSelector{
		{Namespace: "default"},
		{Namespace: "api", Labels: &map[string]string{"tier": "backend"}},
	}, outputs)
}

func TestFargateProfileOutputConvertsStateFields(t *testing.T) {
	output := fargateProfileOutput(sdkFargateProfile(ekstypes.FargateProfileStatusActive))

	assert.Equal(t, "prior-cluster", output.ClusterName)
	assert.Equal(t, "prior-pods", output.FargateProfileName)
	assert.Equal(t, "arn:fargate-profile", output.ARN)
	assert.Equal(t, "arn:role", output.PodExecutionRoleARN)
	assert.Equal(t, "ACTIVE", output.Status)
	assert.Equal(t, []string{"subnet-a", "subnet-b"}, output.SubnetIDs)
	assert.Equal(t, []FargateProfileSelector{{
		Namespace: "default",
		Labels:    &map[string]string{"app": "api"},
	}}, output.Selector)
}
