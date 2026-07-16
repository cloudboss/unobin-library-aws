package elasticache

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/elasticache"
)

const unobinModulePath = "github.com/cloudboss/unobin"

func TestLibraryRegistersResources(t *testing.T) {
	lib := Library()

	assert.Equal(t, "aws-elasticache", lib.Name)
	require.Len(t, lib.Resources, 2)
	require.Contains(t, lib.Resources, "replication-group")
	assert.Equal(t, reflect.TypeFor[*svc.ReplicationGroupResourceOutput](),
		lib.Resources["replication-group"].OutputType())
	require.Contains(t, lib.Resources, "subnet-group")
	assert.Equal(t, reflect.TypeFor[*svc.SubnetGroupResourceOutput](),
		lib.Resources["subnet-group"].OutputType())
	assert.Empty(t, lib.DataSources)
	assert.Empty(t, lib.Actions)
}

func TestSubnetGroupSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 2)

	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"name":        typecheck.TString(),
			"description": typecheck.TString(),
			"subnet-ids":  typecheck.TList(typecheck.TString()),
			"tags":        typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		},
		Outputs: map[string]typecheck.Type{
			"name":   typecheck.TString(),
			"arn":    typecheck.TString(),
			"vpc-id": typecheck.TString(),
		},
		Constraints: []lang.ConstraintSpec{
			{
				Kind:    "predicate",
				When:    "true",
				Require: "(@core.length(input.subnet-ids) >= 1)",
				Message: "subnet-ids must not be empty",
			},
			{
				Kind:    "predicate",
				When:    "true",
				Require: "(@core.length(input.tags ?? {}) <= 50)",
				Message: "tags holds at most 50 entries",
			},
		},
		Defaults: []lang.DefaultSpec{
			{Field: "input.description", Value: "''"},
		},
	}
	require.Contains(t, schema.Resources, "subnet-group")
	assert.Equal(t, want, schema.Resources["subnet-group"])
}

func TestReplicationGroupSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Contains(t, schema.Resources, "replication-group")
	got := schema.Resources["replication-group"]

	logConfiguration := typecheck.TObject([]typecheck.ObjectField{
		{Name: "destination", Type: typecheck.TString()},
		{Name: "destination-type", Type: typecheck.TString()},
		{Name: "log-format", Type: typecheck.TString()},
		{Name: "log-type", Type: typecheck.TString()},
	})
	nodeGroupConfiguration := typecheck.TObject([]typecheck.ObjectField{
		{Name: "node-group-id", Type: typecheck.TString(), Optional: true},
		{Name: "primary-availability-zone", Type: typecheck.TString(), Optional: true},
		{Name: "primary-outpost-arn", Type: typecheck.TString(), Optional: true},
		{Name: "replica-availability-zones",
			Type: typecheck.TList(typecheck.TString()), Optional: true},
		{Name: "replica-count", Type: typecheck.TInteger(), Optional: true},
		{Name: "replica-outpost-arns",
			Type: typecheck.TList(typecheck.TString()), Optional: true},
		{Name: "slots", Type: typecheck.TString(), Optional: true},
	})
	wantInputs := map[string]typecheck.Type{
		"at-rest-encryption-enabled":  typecheck.TOptional(typecheck.TBoolean()),
		"auth-token":                  typecheck.TOptional(typecheck.TString()),
		"auth-token-update-strategy":  typecheck.TString(),
		"auto-minor-version-upgrade":  typecheck.TOptional(typecheck.TBoolean()),
		"automatic-failover-enabled":  typecheck.TBoolean(),
		"cluster-mode":                typecheck.TOptional(typecheck.TString()),
		"data-tiering-enabled":        typecheck.TOptional(typecheck.TBoolean()),
		"description":                 typecheck.TString(),
		"durability":                  typecheck.TOptional(typecheck.TString()),
		"engine":                      typecheck.TString(),
		"engine-version":              typecheck.TOptional(typecheck.TString()),
		"final-snapshot-identifier":   typecheck.TOptional(typecheck.TString()),
		"global-replication-group-id": typecheck.TOptional(typecheck.TString()),
		"ip-discovery":                typecheck.TOptional(typecheck.TString()),
		"kms-key-id":                  typecheck.TOptional(typecheck.TString()),
		"log-delivery-configuration": typecheck.TOptional(
			typecheck.TList(logConfiguration)),
		"maintenance-window": typecheck.TOptional(typecheck.TString()),
		"multi-az-enabled":   typecheck.TOptional(typecheck.TBoolean()),
		"network-type":       typecheck.TOptional(typecheck.TString()),
		"node-group-configuration": typecheck.TOptional(
			typecheck.TList(nodeGroupConfiguration)),
		"node-type":              typecheck.TOptional(typecheck.TString()),
		"notification-topic-arn": typecheck.TOptional(typecheck.TString()),
		"num-cache-clusters":     typecheck.TOptional(typecheck.TInteger()),
		"num-node-groups":        typecheck.TOptional(typecheck.TInteger()),
		"parameter-group-name":   typecheck.TOptional(typecheck.TString()),
		"port":                   typecheck.TOptional(typecheck.TInteger()),
		"preferred-cache-cluster-azs": typecheck.TOptional(
			typecheck.TList(typecheck.TString())),
		"primary-cluster-id":      typecheck.TOptional(typecheck.TString()),
		"replicas-per-node-group": typecheck.TOptional(typecheck.TInteger()),
		"replication-group-id":    typecheck.TString(),
		"security-group-ids": typecheck.TOptional(
			typecheck.TList(typecheck.TString())),
		"security-group-names": typecheck.TOptional(
			typecheck.TList(typecheck.TString())),
		"snapshot-arns":              typecheck.TOptional(typecheck.TList(typecheck.TString())),
		"snapshot-name":              typecheck.TOptional(typecheck.TString()),
		"snapshot-retention-limit":   typecheck.TOptional(typecheck.TInteger()),
		"snapshot-window":            typecheck.TOptional(typecheck.TString()),
		"snapshotting-cluster-id":    typecheck.TOptional(typecheck.TString()),
		"subnet-group-name":          typecheck.TOptional(typecheck.TString()),
		"tags":                       typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		"transit-encryption-enabled": typecheck.TBoolean(),
		"transit-encryption-mode":    typecheck.TOptional(typecheck.TString()),
		"user-group-ids":             typecheck.TOptional(typecheck.TList(typecheck.TString())),
	}
	wantOutputs := map[string]typecheck.Type{
		"arn":                            typecheck.TString(),
		"cluster-enabled":                typecheck.TBoolean(),
		"configuration-endpoint-address": typecheck.TString(),
		"engine-version-actual":          typecheck.TString(),
		"global-replication-group-id":    typecheck.TString(),
		"member-clusters":                typecheck.TList(typecheck.TString()),
		"num-cache-clusters":             typecheck.TInteger(),
		"num-node-groups":                typecheck.TInteger(),
		"parameter-group-name":           typecheck.TString(),
		"port":                           typecheck.TInteger(),
		"primary-endpoint-address":       typecheck.TString(),
		"reader-endpoint-address":        typecheck.TString(),
		"replicas-per-node-group":        typecheck.TInteger(),
		"replication-group-id":           typecheck.TString(),
		"snapshotting-cluster-id":        typecheck.TString(),
	}
	assert.Equal(t, wantInputs, got.Inputs)
	assert.Equal(t, wantOutputs, got.Outputs)
	assert.Equal(t, []string{"auth-token"}, got.SensitiveInputs)
	assert.Equal(t, []lang.DefaultSpec{
		{Field: "input.auth-token-update-strategy", Value: "'ROTATE'"},
		{Field: "input.automatic-failover-enabled", Value: "false"},
		{Field: "input.engine", Value: "'redis'"},
		{Field: "input.transit-encryption-enabled", Value: "false"},
	}, got.Defaults)
	require.Len(t, got.Constraints, 27)
	assert.Contains(t, got.Constraints, lang.ConstraintSpec{
		Kind:    "predicate",
		When:    "true",
		Require: "(@core.length(input.tags ?? {}) <= 50)",
		Message: "tags holds at most 50 entries",
	})
	assert.Contains(t, got.Constraints, lang.ConstraintSpec{
		Kind: "forbidden-with",
		Fields: []string{
			"input.global-replication-group-id",
			"input.primary-cluster-id",
			"input.at-rest-encryption-enabled",
			"input.auth-token",
			"input.data-tiering-enabled",
			"input.engine-version",
			"input.kms-key-id",
			"input.node-type",
			"input.num-node-groups",
			"input.parameter-group-name",
			"input.security-group-names",
			"input.snapshot-arns",
			"input.snapshot-name",
			"input.transit-encryption-mode",
		},
	})
}

func TestReplicationGroupReplacementFields(t *testing.T) {
	r := &svc.ReplicationGroupResource{Engine: "redis"}
	assert.ElementsMatch(t, []string{
		"at-rest-encryption-enabled",
		"data-tiering-enabled",
		"durability",
		"engine",
		"global-replication-group-id",
		"kms-key-id",
		"network-type",
		"node-group-configuration",
		"port",
		"preferred-cache-cluster-azs",
		"replication-group-id",
		"security-group-names",
		"snapshot-arns",
		"snapshot-name",
		"subnet-group-name",
	}, r.ReplaceFields())
	r.Engine = "valkey"
	r.NodeGroupConfiguration = &[]svc.ReplicationGroupNodeGroupConfiguration{{}}
	assert.NotContains(t, r.ReplaceFields(), "engine")
	assert.Contains(t, r.ReplaceFields(), "replicas-per-node-group")
}

func readLibrarySchema(t *testing.T) *runtime.LibrarySchema {
	t.Helper()
	moduleRoot := libraryModuleRoot(t)
	unobinRoot := unobinModuleRoot(t)
	schema, warnings, err := goschema.Read(".", moduleRoot, unobinRoot)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.True(t, schema.HasConfiguration)

	configSchema, warnings, err := goschema.ReadLibraryConfiguration("../../config", unobinRoot)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, configSchema.ConfigurationIdentity, schema.ConfigurationIdentity)
	require.Equal(t, configSchema.ConfigurationDigest, schema.ConfigurationDigest)
	return schema
}

func libraryModuleRoot(t *testing.T) goschema.ModuleRoot {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Path}}\n{{.Dir}}").Output()
	require.NoError(t, err)
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, parts, 2)
	return goschema.ModuleRoot{Path: parts[0], Dir: parts[1]}
}

func unobinModuleRoot(t *testing.T) goschema.ModuleRoot {
	t.Helper()
	out, err := exec.Command(
		"go", "list", "-m", "-f", "{{.Dir}}", unobinModulePath,
	).Output()
	require.NoError(t, err)
	dir := strings.TrimSpace(string(out))
	require.NotEmpty(t, dir)
	return goschema.ModuleRoot{Path: unobinModulePath, Dir: dir}
}
