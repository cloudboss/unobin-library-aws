package dsql_test

import (
	"slices"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/dsql"
)

func TestClusterSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 1)
	require.Contains(t, schema.Resources, "cluster")
	cluster := schema.Resources["cluster"]

	assert.Equal(t, []string{
		"deletion-protection-enabled",
		"force-destroy",
		"kms-encryption-key",
		"multi-region-properties",
		"tags",
	}, sortedKeys(cluster.Inputs))
	assert.Equal(t, typecheck.TBoolean(), cluster.Inputs["deletion-protection-enabled"])
	assert.Equal(t, typecheck.TBoolean(), cluster.Inputs["force-destroy"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()),
		cluster.Inputs["kms-encryption-key"])
	assert.Equal(t, typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		cluster.Inputs["tags"])
	assert.NotContains(t, cluster.Outputs, "tags-all")
	assert.Empty(t, cluster.SensitiveInputs)
	assert.Empty(t, cluster.SensitiveOutputs)
	assert.Contains(t, cluster.Defaults, lang.DefaultSpec{
		Field: "input.deletion-protection-enabled", Value: "false",
	})
	assert.Contains(t, cluster.Defaults, lang.DefaultSpec{
		Field: "input.force-destroy", Value: "false",
	})
}

func TestClusterOutputs(t *testing.T) {
	cluster := readLibrarySchema(t).Resources["cluster"]
	assert.Equal(t, []string{
		"arn",
		"deletion-protection-enabled",
		"encryption-details",
		"identifier",
		"kms-encryption-key",
		"multi-region-properties",
		"tags",
		"vpc-endpoint-service-name",
	}, sortedKeys(cluster.Outputs))
	assert.Equal(t, typecheck.TString(), cluster.Outputs["arn"])
	assert.Equal(t, typecheck.TString(), cluster.Outputs["identifier"])
	assert.Equal(t, typecheck.TBoolean(), cluster.Outputs["deletion-protection-enabled"])
	assert.Equal(t, typecheck.TString(), cluster.Outputs["kms-encryption-key"])
	assert.Equal(t, typecheck.TString(), cluster.Outputs["vpc-endpoint-service-name"])
}

func TestClusterNestedFields(t *testing.T) {
	cluster := readLibrarySchema(t).Resources["cluster"]

	multiRegion := cluster.Inputs["multi-region-properties"].Unwrap()
	assert.Equal(t, []string{"clusters", "witness-region"}, sortedObjectFields(multiRegion))

	encryption := cluster.Outputs["encryption-details"]
	assert.Equal(t, []string{"encryption-status", "encryption-type"},
		sortedObjectFields(encryption))
}

func TestClusterReplacementFields(t *testing.T) {
	assert.Empty(t, (&svc.ClusterResource{}).ReplaceFields())
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func sortedObjectFields(value typecheck.Type) []string {
	fields := make([]string, 0, len(value.Fields))
	for _, field := range value.Fields {
		fields = append(fields, field.Name)
	}
	slices.Sort(fields)
	return fields
}
