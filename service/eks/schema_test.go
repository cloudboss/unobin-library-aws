package eks_test

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/eks"
)

func TestClusterSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 3)
	cluster := schema.Resources["cluster"]
	require.NotNil(t, cluster)

	assert.Equal(t, typecheck.TString(), cluster.Inputs["name"])
	assert.Equal(t, typecheck.TString(), cluster.Inputs["role-arn"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()), cluster.Inputs["version"])
	assert.Equal(t, typecheck.TOptional(typecheck.TBoolean()),
		cluster.Inputs["force-update-version"])
	assert.Equal(t, typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		cluster.Inputs["tags"])
	assert.Equal(t, typecheck.TString(), cluster.Outputs["name"])
	assert.Equal(t, typecheck.TString(), cluster.Outputs["arn"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()), cluster.Outputs["cluster-id"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()), cluster.Outputs["oidc-issuer"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()),
		cluster.Outputs["service-ipv6-cidr"])
	assert.Empty(t, cluster.SensitiveInputs)
	assert.Empty(t, cluster.SensitiveOutputs)
	assert.Len(t, cluster.Defaults, 3)
}

func TestNodeGroupSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 3)
	nodeGroup := schema.Resources["node-group"]
	require.NotNil(t, nodeGroup)

	assert.Equal(t, typecheck.TString(), nodeGroup.Inputs["cluster-name"])
	assert.Equal(t, typecheck.TString(), nodeGroup.Inputs["node-group-name"])
	assert.Equal(t, typecheck.TString(), nodeGroup.Inputs["node-role-arn"])
	assert.Equal(t, typecheck.TList(typecheck.TString()), nodeGroup.Inputs["subnet-ids"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()),
		nodeGroup.Inputs["launch-template-version"])
	assert.Equal(t, typecheck.TBoolean(), nodeGroup.Inputs["force-update-version"])
	assert.Equal(t, typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		nodeGroup.Inputs["tags"])
	assert.Equal(t, typecheck.TString(), nodeGroup.Outputs["cluster-name"])
	assert.Equal(t, typecheck.TString(), nodeGroup.Outputs["node-group-name"])
	assert.Equal(t, typecheck.TString(), nodeGroup.Outputs["arn"])
	assert.Equal(t, typecheck.TList(typecheck.TString()),
		nodeGroup.Outputs["auto-scaling-group-names"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()),
		nodeGroup.Outputs["remote-access-security-group-id"])
	assert.Empty(t, nodeGroup.SensitiveInputs)
	assert.Empty(t, nodeGroup.SensitiveOutputs)
	assert.Len(t, nodeGroup.Defaults, 2)
}

func TestAddonSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 3)
	addon := schema.Resources["addon"]
	require.NotNil(t, addon)

	assert.Equal(t, typecheck.TString(), addon.Inputs["cluster-name"])
	assert.Equal(t, typecheck.TString(), addon.Inputs["addon-name"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()),
		addon.Inputs["addon-version"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()),
		addon.Inputs["configuration-values"])
	assert.Equal(t, typecheck.TBoolean(), addon.Inputs["preserve"])
	assert.Equal(t, typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		addon.Inputs["tags"])
	assert.Equal(t, typecheck.TString(), addon.Outputs["cluster-name"])
	assert.Equal(t, typecheck.TString(), addon.Outputs["addon-name"])
	assert.Equal(t, typecheck.TString(), addon.Outputs["arn"])
	assert.Equal(t, typecheck.TString(), addon.Outputs["addon-version"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()),
		addon.Outputs["namespace"])
	assert.NotContains(t, addon.Outputs, "addon-version-actual")
	assert.NotContains(t, addon.Outputs, "namespace-actual")
	assert.Empty(t, addon.SensitiveInputs)
	assert.Empty(t, addon.SensitiveOutputs)
	assert.Len(t, addon.Defaults, 1)
}

func TestAddonReplacementFields(t *testing.T) {
	resource := &svc.AddonResource{}
	assert.Equal(t, []string{
		"cluster-name", "addon-name", "namespace-config",
	}, resource.ReplaceFields())
}

func TestClusterReplacementFields(t *testing.T) {
	resource := &svc.ClusterResource{}
	assert.Equal(t, []string{
		"name", "role-arn", "bootstrap-self-managed-addons", "outpost-config",
	}, resource.ReplaceFields())
}

func TestNodeGroupReplacementFields(t *testing.T) {
	resource := &svc.NodeGroupResource{}
	assert.Equal(t, []string{
		"cluster-name",
		"node-group-name",
		"node-role-arn",
		"subnet-ids",
		"ami-type",
		"capacity-type",
		"disk-size",
		"instance-types",
		"remote-access",
		"launch-template-id",
		"launch-template-name",
	}, resource.ReplaceFields())
}
