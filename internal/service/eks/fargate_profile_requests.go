package eks

import (
	"maps"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

func (r FargateProfileResource) fargateProfileCreateInput(
	token string,
) *eks.CreateFargateProfileInput {
	subnets := []string{}
	if r.SubnetIDs != nil {
		subnets = slices.Clone(*r.SubnetIDs)
	}
	return &eks.CreateFargateProfileInput{
		ClientRequestToken:  aws.String(token),
		ClusterName:         aws.String(r.ClusterName),
		FargateProfileName:  aws.String(r.FargateProfileName),
		PodExecutionRoleArn: aws.String(r.PodExecutionRoleARN),
		Selectors:           fargateProfileSelectorInputs(r.Selector),
		Subnets:             subnets,
		Tags:                clusterTags(valueMapOrNil(r.Tags)),
	}
}

func fargateProfileSelectorInputs(
	selectors []FargateProfileSelector,
) []ekstypes.FargateProfileSelector {
	inputs := make([]ekstypes.FargateProfileSelector, 0, len(selectors))
	for _, selector := range selectors {
		input := ekstypes.FargateProfileSelector{
			Namespace: aws.String(selector.Namespace),
		}
		if selector.Labels != nil && len(*selector.Labels) > 0 {
			input.Labels = maps.Clone(*selector.Labels)
		}
		inputs = append(inputs, input)
	}
	return inputs
}

func fargateProfileSelectorOutput(
	selectors []ekstypes.FargateProfileSelector,
) []FargateProfileSelector {
	outputs := make([]FargateProfileSelector, 0, len(selectors))
	for _, selector := range selectors {
		output := FargateProfileSelector{
			Namespace: aws.ToString(selector.Namespace),
		}
		if len(selector.Labels) > 0 {
			labels := maps.Clone(selector.Labels)
			output.Labels = &labels
		}
		outputs = append(outputs, output)
	}
	return outputs
}
