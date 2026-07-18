package eks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddonValidation(t *testing.T) {
	valid := validAddonResource()
	tests := []struct {
		name   string
		modify func(*AddonResource)
		match  string
	}{
		{name: "cluster", modify: func(r *AddonResource) { r.ClusterName = "bad name" },
			match: "cluster-name"},
		{name: "add-on", modify: func(r *AddonResource) { r.AddonName = "" },
			match: "addon-name"},
		{name: "version", modify: func(r *AddonResource) {
			r.AddonVersion = stringPointer("1.2.3")
		}, match: "addon-version"},
		{name: "configuration", modify: func(r *AddonResource) {
			r.ConfigurationValues = stringPointer("")
		}, match: "configuration-values"},
		{name: "namespace", modify: func(r *AddonResource) {
			r.NamespaceConfig = &AddonNamespaceConfig{Namespace: "Bad_Name"}
		}, match: "namespace-config.namespace"},
		{name: "create conflict", modify: func(r *AddonResource) {
			r.ResolveConflictsOnCreate = stringPointer("PRESERVE")
		}, match: "resolve-conflicts-on-create"},
		{name: "update conflict", modify: func(r *AddonResource) {
			r.ResolveConflictsOnUpdate = stringPointer("unknown")
		}, match: "resolve-conflicts-on-update"},
		{name: "service role", modify: func(r *AddonResource) {
			r.ServiceAccountRoleARN = stringPointer("not-an-arn")
		}, match: "service-account-role-arn"},
		{name: "pod role", modify: func(r *AddonResource) {
			r.PodIdentityAssociation = &[]AddonPodIdentityAssociation{{
				RoleARN: "not-an-arn", ServiceAccount: "coredns",
			}}
		}, match: "pod-identity-association[0].role-arn"},
		{name: "pod service account", modify: func(r *AddonResource) {
			r.PodIdentityAssociation = &[]AddonPodIdentityAssociation{{
				RoleARN: "arn:aws:iam::123456789012:role/addon",
			}}
		}, match: "pod-identity-association[0].service-account"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := valid
			test.modify(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, test.match)
		})
	}
}

func TestAddonValidationAllowsSupportedValues(t *testing.T) {
	resource := validAddonResource()
	resource.AddonVersion = stringPointer("v1.2.3-eksbuild.1")
	resource.ConfigurationValues = stringPointer("{}")
	resource.NamespaceConfig = &AddonNamespaceConfig{Namespace: "kube-system"}
	resource.ResolveConflictsOnCreate = stringPointer("NONE")
	resource.ResolveConflictsOnUpdate = stringPointer("PRESERVE")
	resource.ServiceAccountRoleARN = stringPointer(
		"arn:aws:iam::123456789012:role/addon",
	)
	resource.PodIdentityAssociation = &[]AddonPodIdentityAssociation{{
		RoleARN:        "arn:aws:iam::123456789012:role/pod",
		ServiceAccount: "coredns",
	}}
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestAddonValidationAllowsEveryAWSPartition(t *testing.T) {
	for _, partition := range []string{"aws", "aws-cn", "aws-us-gov"} {
		resource := validAddonResource()
		resource.ServiceAccountRoleARN = stringPointer(
			"arn:" + partition + ":iam::123456789012:role/addon",
		)
		resource.PodIdentityAssociation = &[]AddonPodIdentityAssociation{{
			RoleARN:        "arn:" + partition + ":iam::123456789012:role/pod",
			ServiceAccount: "coredns",
		}}
		require.NoError(t, resource.ValidateInputs(context.Background(), nil))
	}
}

func validAddonResource() AddonResource {
	return AddonResource{ClusterName: "example", AddonName: "coredns"}
}
