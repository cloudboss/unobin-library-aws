package eks

import (
	"regexp"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEKSClientRequestTokenIsUUID(t *testing.T) {
	first, err := eksClientRequestToken()
	require.NoError(t, err)
	second, err := eksClientRequestToken()
	require.NoError(t, err)

	pattern := regexp.MustCompile(
		`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
	)
	assert.Regexp(t, pattern, first)
	assert.Regexp(t, pattern, second)
	assert.NotEqual(t, first, second)
}

func TestAddonCreateInputIncludesConfiguredValues(t *testing.T) {
	resource := validAddonResource()
	resource.AddonVersion = stringPointer("v1.12.0-eksbuild.1")
	resource.ConfigurationValues = stringPointer("{}")
	resource.NamespaceConfig = &AddonNamespaceConfig{Namespace: "kube-system"}
	resource.PodIdentityAssociation = &[]AddonPodIdentityAssociation{{
		RoleARN:        "arn:aws:iam::123456789012:role/dns",
		ServiceAccount: "coredns",
	}}
	resource.ResolveConflictsOnCreate = stringPointer("NONE")
	resource.ServiceAccountRoleARN = stringPointer(
		"arn:aws:iam::123456789012:role/service",
	)
	resource.Tags = &map[string]string{"team": "platform", "aws:service": "ignored"}

	input := resource.addonCreateInput("11111111-1111-4111-8111-111111111111")

	assert.Equal(t, "example", aws.ToString(input.ClusterName))
	assert.Equal(t, "coredns", aws.ToString(input.AddonName))
	assert.Equal(t, "v1.12.0-eksbuild.1", aws.ToString(input.AddonVersion))
	assert.Equal(t, "{}", aws.ToString(input.ConfigurationValues))
	assert.Equal(t, "kube-system", aws.ToString(input.NamespaceConfig.Namespace))
	require.Len(t, input.PodIdentityAssociations, 1)
	assert.Equal(t, "arn:aws:iam::123456789012:role/dns",
		aws.ToString(input.PodIdentityAssociations[0].RoleArn))
	assert.Equal(t, "coredns",
		aws.ToString(input.PodIdentityAssociations[0].ServiceAccount))
	assert.Equal(t, ekstypes.ResolveConflictsNone, input.ResolveConflicts)
	assert.Equal(t, "arn:aws:iam::123456789012:role/service",
		aws.ToString(input.ServiceAccountRoleArn))
	assert.Equal(t, map[string]string{"team": "platform"}, input.Tags)
}

func TestAddonCreateInputOmitsOptionalValues(t *testing.T) {
	input := validAddonResource().addonCreateInput("token")

	assert.Nil(t, input.AddonVersion)
	assert.Nil(t, input.ConfigurationValues)
	assert.Nil(t, input.NamespaceConfig)
	assert.Nil(t, input.PodIdentityAssociations)
	assert.Empty(t, input.ResolveConflicts)
	assert.Nil(t, input.ServiceAccountRoleArn)
	assert.Nil(t, input.Tags)
}

func TestAddonUpdateInputCombinesChangesAndExplicitClears(t *testing.T) {
	prior := validAddonResource()
	prior.AddonVersion = stringPointer("v1.11.0-eksbuild.1")
	prior.ConfigurationValues = stringPointer("{\"replicas\":2}")
	prior.ServiceAccountRoleARN = stringPointer(
		"arn:aws:iam::123456789012:role/old",
	)
	prior.PodIdentityAssociation = &[]AddonPodIdentityAssociation{{
		RoleARN:        "arn:aws:iam::123456789012:role/old",
		ServiceAccount: "coredns",
	}}
	current := prior
	current.AddonVersion = stringPointer("v1.12.0-eksbuild.1")
	current.ConfigurationValues = stringPointer("{}")
	current.ServiceAccountRoleARN = nil
	empty := []AddonPodIdentityAssociation{}
	current.PodIdentityAssociation = &empty
	current.ResolveConflictsOnUpdate = stringPointer("PRESERVE")

	input, needed := current.addonUpdateInput(
		prior,
		"prior-cluster",
		"prior-addon",
		"22222222-2222-4222-8222-222222222222",
	)

	require.True(t, needed)
	assert.Equal(t, "prior-cluster", aws.ToString(input.ClusterName))
	assert.Equal(t, "prior-addon", aws.ToString(input.AddonName))
	assert.Equal(t, "v1.12.0-eksbuild.1", aws.ToString(input.AddonVersion))
	assert.Equal(t, "{}", aws.ToString(input.ConfigurationValues))
	assert.NotNil(t, input.ServiceAccountRoleArn)
	assert.Empty(t, aws.ToString(input.ServiceAccountRoleArn))
	assert.NotNil(t, input.PodIdentityAssociations)
	assert.Empty(t, input.PodIdentityAssociations)
	assert.Equal(t, ekstypes.ResolveConflictsPreserve, input.ResolveConflicts)
}

func TestAddonUpdateInputOmissionDoesNotClearConfigurationOrCallForModifier(t *testing.T) {
	prior := validAddonResource()
	prior.ConfigurationValues = stringPointer("{\"replicas\":2}")
	current := prior
	current.ConfigurationValues = nil
	current.ResolveConflictsOnUpdate = stringPointer("OVERWRITE")

	input, needed := current.addonUpdateInput(prior, "example", "coredns", "token")

	assert.False(t, needed)
	assert.Nil(t, input)
}

func TestAddonUpdateInputRemovalSemantics(t *testing.T) {
	tests := []struct {
		name   string
		prior  AddonResource
		modify func(*AddonResource)
		check  func(*testing.T, bool, any)
	}{
		{
			name: "version omission is no change",
			prior: func() AddonResource {
				r := validAddonResource()
				r.AddonVersion = stringPointer("v1.12.0-eksbuild.1")
				return r
			}(),
			modify: func(r *AddonResource) { r.AddonVersion = nil },
			check: func(t *testing.T, needed bool, raw any) {
				assert.False(t, needed)
				assert.Nil(t, raw)
			},
		},
		{
			name: "nil pod identities clear",
			prior: func() AddonResource {
				r := validAddonResource()
				r.PodIdentityAssociation = &[]AddonPodIdentityAssociation{{
					RoleARN:        "arn:aws:iam::123456789012:role/dns",
					ServiceAccount: "coredns",
				}}
				return r
			}(),
			modify: func(r *AddonResource) { r.PodIdentityAssociation = nil },
			check: func(t *testing.T, needed bool, raw any) {
				require.True(t, needed)
				input := raw.(*eks.UpdateAddonInput)
				assert.NotNil(t, input.PodIdentityAssociations)
				assert.Empty(t, input.PodIdentityAssociations)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := test.prior
			test.modify(&current)
			input, needed := current.addonUpdateInput(
				test.prior, "example", "coredns", "token",
			)
			test.check(t, needed, input)
		})
	}
}

func TestAddonPodIdentityAssociationInputIsOrderIndependent(t *testing.T) {
	first := AddonPodIdentityAssociation{
		RoleARN: "arn:aws:iam::123456789012:role/first", ServiceAccount: "one",
	}
	second := AddonPodIdentityAssociation{
		RoleARN: "arn:aws:iam::123456789012:role/second", ServiceAccount: "two",
	}
	prior := validAddonResource()
	prior.PodIdentityAssociation = &[]AddonPodIdentityAssociation{first, second}
	current := prior
	current.PodIdentityAssociation = &[]AddonPodIdentityAssociation{second, first}

	assert.True(t, current.EquivalentInput("pod-identity-association", prior, current))
	input, needed := current.addonUpdateInput(prior, "example", "coredns", "token")
	assert.False(t, needed)
	assert.Nil(t, input)
}
