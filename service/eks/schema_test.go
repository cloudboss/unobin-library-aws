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
	require.Len(t, schema.Resources, 1)
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

func TestClusterReplacementFields(t *testing.T) {
	resource := &svc.ClusterResource{}
	assert.Equal(t, []string{
		"name", "role-arn", "bootstrap-self-managed-addons", "outpost-config",
	}, resource.ReplaceFields())
}
