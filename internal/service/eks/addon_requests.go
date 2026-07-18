package eks

import (
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r AddonResource) addonCreateInput(token string) *eks.CreateAddonInput {
	input := &eks.CreateAddonInput{
		AddonName:          aws.String(r.AddonName),
		ClientRequestToken: aws.String(token),
		ClusterName:        aws.String(r.ClusterName),
		Tags:               clusterTags(valueMapOrNil(r.Tags)),
	}
	input.AddonVersion = copyStringPointer(r.AddonVersion)
	input.ConfigurationValues = copyStringPointer(r.ConfigurationValues)
	if r.NamespaceConfig != nil {
		input.NamespaceConfig = &ekstypes.AddonNamespaceConfigRequest{
			Namespace: aws.String(r.NamespaceConfig.Namespace),
		}
	}
	input.PodIdentityAssociations = addonPodIdentityInputs(r.PodIdentityAssociation)
	if r.ResolveConflictsOnCreate != nil {
		input.ResolveConflicts = ekstypes.ResolveConflicts(*r.ResolveConflictsOnCreate)
	}
	input.ServiceAccountRoleArn = copyStringPointer(r.ServiceAccountRoleARN)
	return input
}

func (r AddonResource) addonUpdateInput(
	prior AddonResource,
	clusterName string,
	addonName string,
	token string,
) (*eks.UpdateAddonInput, bool) {
	input := &eks.UpdateAddonInput{
		AddonName:          aws.String(addonName),
		ClientRequestToken: aws.String(token),
		ClusterName:        aws.String(clusterName),
	}
	needed := false
	if r.AddonVersion != nil && runtime.Changed(prior.AddonVersion, r.AddonVersion) {
		input.AddonVersion = copyStringPointer(r.AddonVersion)
		needed = true
	}
	if r.ConfigurationValues != nil &&
		runtime.Changed(prior.ConfigurationValues, r.ConfigurationValues) {
		input.ConfigurationValues = copyStringPointer(r.ConfigurationValues)
		needed = true
	}
	if !optionalUnorderedNodeGroupSliceEqual(
		prior.PodIdentityAssociation,
		r.PodIdentityAssociation,
	) {
		input.PodIdentityAssociations = addonPodIdentityInputs(r.PodIdentityAssociation)
		if input.PodIdentityAssociations == nil {
			input.PodIdentityAssociations = []ekstypes.AddonPodIdentityAssociations{}
		}
		needed = true
	}
	if runtime.Changed(prior.ServiceAccountRoleARN, r.ServiceAccountRoleARN) {
		if r.ServiceAccountRoleARN == nil {
			input.ServiceAccountRoleArn = aws.String("")
		} else {
			input.ServiceAccountRoleArn = copyStringPointer(r.ServiceAccountRoleARN)
		}
		needed = true
	}
	if !needed {
		return nil, false
	}
	if r.ResolveConflictsOnUpdate != nil {
		input.ResolveConflicts = ekstypes.ResolveConflicts(*r.ResolveConflictsOnUpdate)
	}
	return input, true
}

func addonPodIdentityInputs(
	associations *[]AddonPodIdentityAssociation,
) []ekstypes.AddonPodIdentityAssociations {
	if associations == nil || len(*associations) == 0 {
		return nil
	}
	inputs := make([]ekstypes.AddonPodIdentityAssociations, 0, len(*associations))
	for _, association := range *associations {
		inputs = append(inputs, ekstypes.AddonPodIdentityAssociations{
			RoleArn:        aws.String(association.RoleARN),
			ServiceAccount: aws.String(association.ServiceAccount),
		})
	}
	return slices.Clone(inputs)
}
