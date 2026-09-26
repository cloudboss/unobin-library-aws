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
	require.Len(t, schema.Resources, 4)
	cluster := schema.Resources["cluster"]
	require.NotNil(t, cluster)

	assert.Equal(t, typecheck.TString(), cluster.Inputs["name"])
	assert.Equal(t, typecheck.TString(), cluster.Inputs["role-arn"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()), cluster.Inputs["version"])
	assert.Equal(t, typecheck.TOptional(typecheck.TBoolean()),
		cluster.Inputs["force-update-version"])
	assert.Equal(t, typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		cluster.Inputs["tags"])
	assert.Equal(t, map[string]typecheck.Type{
		"name":                       typecheck.TString(),
		"arn":                        typecheck.TString(),
		"certificate-authority-data": typecheck.TString(),
		"cluster-id":                 typecheck.TOptional(typecheck.TString()),
		"created-at":                 typecheck.TString(),
		"endpoint":                   typecheck.TString(),
		"oidc-issuer":                typecheck.TOptional(typecheck.TString()),
		"platform-version":           typecheck.TString(),
		"status":                     typecheck.TString(),
		"version":                    typecheck.TString(),
		"ip-family":                  typecheck.TString(),
		"service-ipv4-cidr":          typecheck.TString(),
		"service-ipv6-cidr":          typecheck.TOptional(typecheck.TString()),
		"cluster-security-group-id":  typecheck.TString(),
		"vpc-id":                     typecheck.TString(),
	}, cluster.Outputs)
	assert.Empty(t, cluster.SensitiveInputs)
	assert.Empty(t, cluster.SensitiveOutputs)
	assert.Len(t, cluster.Defaults, 3)
}

func TestNodeGroupSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 4)
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
	assert.Equal(t, map[string]typecheck.Type{
		"cluster-name":                    typecheck.TString(),
		"node-group-name":                 typecheck.TString(),
		"arn":                             typecheck.TString(),
		"status":                          typecheck.TString(),
		"auto-scaling-group-names":        typecheck.TList(typecheck.TString()),
		"remote-access-security-group-id": typecheck.TOptional(typecheck.TString()),
		"ami-type":                        typecheck.TString(),
		"capacity-type":                   typecheck.TString(),
		"disk-size":                       typecheck.TOptional(typecheck.TInteger()),
		"instance-types":                  typecheck.TList(typecheck.TString()),
		"launch-template-id":              typecheck.TOptional(typecheck.TString()),
		"launch-template-name":            typecheck.TOptional(typecheck.TString()),
		"launch-template-version":         typecheck.TOptional(typecheck.TString()),
		"node-repair-config":              nodeGroup.Inputs["node-repair-config"],
		"release-version":                 typecheck.TString(),
		"update-config":                   nodeGroup.Inputs["update-config"],
		"version":                         typecheck.TString(),
		"warm-pool-config":                nodeGroup.Inputs["warm-pool-config"],
	}, nodeGroup.Outputs)
	assert.Empty(t, nodeGroup.SensitiveInputs)
	assert.Empty(t, nodeGroup.SensitiveOutputs)
	assert.Len(t, nodeGroup.Defaults, 2)
}

func TestFargateProfileSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 4)
	profile := schema.Resources["fargate-profile"]
	require.NotNil(t, profile)

	assert.Equal(t, typecheck.TString(), profile.Inputs["cluster-name"])
	assert.Equal(t, typecheck.TString(), profile.Inputs["fargate-profile-name"])
	assert.Equal(t, typecheck.TString(), profile.Inputs["pod-execution-role-arn"])
	assert.Equal(t, profile.Outputs["selector"], profile.Inputs["selector"])
	assert.Equal(t, typecheck.TOptional(typecheck.TList(typecheck.TString())),
		profile.Inputs["subnet-ids"])
	assert.Equal(t, typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		profile.Inputs["tags"])
	assert.Equal(t, map[string]typecheck.Type{
		"cluster-name":           typecheck.TString(),
		"fargate-profile-name":   typecheck.TString(),
		"arn":                    typecheck.TString(),
		"pod-execution-role-arn": typecheck.TString(),
		"selector":               profile.Inputs["selector"],
		"status":                 typecheck.TString(),
		"subnet-ids":             typecheck.TList(typecheck.TString()),
	}, profile.Outputs)
	assert.NotContains(t, profile.Outputs, "tags-all")
	assert.Empty(t, profile.SensitiveInputs)
	assert.Empty(t, profile.SensitiveOutputs)
	assert.Empty(t, profile.Defaults)
}

func TestAddonSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 4)
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

func TestFargateProfileReplacementFields(t *testing.T) {
	resource := &svc.FargateProfileResource{}
	assert.Equal(t, []string{
		"cluster-name",
		"fargate-profile-name",
		"pod-execution-role-arn",
		"selector",
		"subnet-ids",
	}, resource.ReplaceFields())
}
