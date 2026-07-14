package efs

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unobinModulePath = "github.com/cloudboss/unobin"

func TestFileSystemSchema(t *testing.T) {
	schema := readEFSSchema(t)
	require.Contains(t, schema.Resources, "file-system")
	fileSystem := schema.Resources["file-system"]

	assert.Equal(t, map[string]typecheck.Type{
		"availability-zone-name":          typecheck.TOptional(typecheck.TString()),
		"encrypted":                       typecheck.TOptional(typecheck.TBoolean()),
		"kms-key-id":                      typecheck.TOptional(typecheck.TString()),
		"performance-mode":                typecheck.TOptional(typecheck.TString()),
		"throughput-mode":                 typecheck.TOptional(typecheck.TString()),
		"provisioned-throughput-in-mibps": typecheck.TOptional(typecheck.TNumber()),
		"lifecycle-policies": typecheck.TOptional(typecheck.TList(typecheck.TObject(
			[]typecheck.ObjectField{
				{Name: "transition-to-ia", Type: typecheck.TString(), Optional: true},
				{Name: "transition-to-archive", Type: typecheck.TString(), Optional: true},
				{
					Name:     "transition-to-primary-storage-class",
					Type:     typecheck.TString(),
					Optional: true,
				},
			}))),
		"backup-policy": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
			{Name: "status", Type: typecheck.TString()},
		})),
		"file-system-policy":                 typecheck.TOptional(typecheck.TString()),
		"bypass-policy-lockout-safety-check": typecheck.TOptional(typecheck.TBoolean()),
		"file-system-protection": typecheck.TOptional(typecheck.TObject(
			[]typecheck.ObjectField{{
				Name: "replication-overwrite-protection", Type: typecheck.TString(),
			}})),
		"replication-configuration": typecheck.TOptional(typecheck.TObject(
			[]typecheck.ObjectField{{
				Name: "destinations",
				Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "region", Type: typecheck.TString(), Optional: true},
					{Name: "availability-zone-name", Type: typecheck.TString(), Optional: true},
					{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
					{Name: "file-system-id", Type: typecheck.TString(), Optional: true},
					{Name: "role-arn", Type: typecheck.TString(), Optional: true},
				})),
			}})),
		"tags": typecheck.TOptional(typecheck.TMap(typecheck.TString())),
	}, fileSystem.Inputs)
	assert.Equal(t, map[string]typecheck.Type{
		"file-system-id":                  typecheck.TString(),
		"arn":                             typecheck.TString(),
		"availability-zone-id":            typecheck.TString(),
		"availability-zone-name":          typecheck.TString(),
		"dns-name":                        typecheck.TString(),
		"owner-id":                        typecheck.TString(),
		"name":                            typecheck.TString(),
		"number-of-mount-targets":         typecheck.TInteger(),
		"encrypted":                       typecheck.TBoolean(),
		"kms-key-id":                      typecheck.TString(),
		"performance-mode":                typecheck.TString(),
		"provisioned-throughput-in-mibps": typecheck.TNumber(),
		"throughput-mode":                 typecheck.TString(),
		"size-in-bytes": typecheck.TObject([]typecheck.ObjectField{
			{Name: "value", Type: typecheck.TInteger()},
			{Name: "timestamp", Type: typecheck.TString()},
			{Name: "value-in-archive", Type: typecheck.TInteger()},
			{Name: "value-in-ia", Type: typecheck.TInteger()},
			{Name: "value-in-standard", Type: typecheck.TInteger()},
		}),
		"lifecycle-policies": typecheck.TList(typecheck.TObject(
			[]typecheck.ObjectField{
				{Name: "transition-to-ia", Type: typecheck.TString(), Optional: true},
				{Name: "transition-to-archive", Type: typecheck.TString(), Optional: true},
				{
					Name:     "transition-to-primary-storage-class",
					Type:     typecheck.TString(),
					Optional: true,
				},
			})),
		"backup-policy": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
			{Name: "status", Type: typecheck.TString()},
		})),
		"file-system-policy": typecheck.TOptional(typecheck.TString()),
		"file-system-protection": typecheck.TOptional(typecheck.TObject(
			[]typecheck.ObjectField{{
				Name: "replication-overwrite-protection", Type: typecheck.TString(),
			}})),
		"replication-configuration": typecheck.TOptional(typecheck.TObject(
			[]typecheck.ObjectField{{
				Name: "destinations",
				Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "file-system-id", Type: typecheck.TString()},
					{Name: "region", Type: typecheck.TString()},
					{Name: "owner-id", Type: typecheck.TString()},
					{Name: "role-arn", Type: typecheck.TString()},
					{Name: "status", Type: typecheck.TString()},
					{Name: "status-message", Type: typecheck.TString()},
				})),
			}})),
		"tags": typecheck.TMap(typecheck.TString()),
	}, fileSystem.Outputs)
	assert.Empty(t, fileSystem.SensitiveInputs)
	assert.Empty(t, fileSystem.SensitiveOutputs)
	assert.Empty(t, fileSystem.Defaults)

	messages := make(map[string]bool, len(fileSystem.Constraints))
	for _, item := range fileSystem.Constraints {
		messages[item.Message] = true
	}
	for _, message := range []string{
		"kms-key-id requires encrypted to be true",
		"performance-mode must be generalPurpose or maxIO",
		"throughput-mode must be bursting, provisioned, or elastic",
		"provisioned throughput requires provisioned-throughput-in-mibps",
		"lifecycle-policies holds at most 3 entries",
		"backup-policy status must be ENABLED or DISABLED",
		"file-system-protection must be ENABLED or DISABLED",
		"replication-configuration must have exactly one destination",
	} {
		assert.True(t, messages[message], "missing constraint %q", message)
	}
}

func TestMountTargetSchema(t *testing.T) {
	schema := readEFSSchema(t)
	require.Contains(t, schema.Resources, "mount-target")
	mountTarget := schema.Resources["mount-target"]

	assert.Equal(t, map[string]typecheck.Type{
		"file-system-id":  typecheck.TString(),
		"subnet-id":       typecheck.TString(),
		"ip-address":      typecheck.TOptional(typecheck.TString()),
		"ip-address-type": typecheck.TOptional(typecheck.TString()),
		"ipv6-address":    typecheck.TOptional(typecheck.TString()),
		"security-groups": typecheck.TOptional(typecheck.TList(typecheck.TString())),
	}, mountTarget.Inputs)
	assert.Equal(t, map[string]typecheck.Type{
		"mount-target-id":        typecheck.TString(),
		"availability-zone-id":   typecheck.TString(),
		"availability-zone-name": typecheck.TString(),
		"ip-address":             typecheck.TString(),
		"ip-address-type":        typecheck.TString(),
		"ipv6-address":           typecheck.TString(),
		"mount-target-dns-name":  typecheck.TString(),
		"network-interface-id":   typecheck.TString(),
		"owner-id":               typecheck.TString(),
		"security-groups":        typecheck.TList(typecheck.TString()),
	}, mountTarget.Outputs)
	assert.Empty(t, mountTarget.SensitiveInputs)
	assert.Empty(t, mountTarget.SensitiveOutputs)
	assert.Empty(t, mountTarget.Defaults)
	require.Len(t, mountTarget.Constraints, 1)
	assert.Equal(t, "ip-address-type must be IPV4_ONLY, IPV6_ONLY, or DUAL_STACK",
		mountTarget.Constraints[0].Message)
}

func readEFSSchema(t *testing.T) *runtime.LibrarySchema {
	t.Helper()
	moduleRoot := goModuleRoot(t, "")
	unobinRoot := goModuleRoot(t, unobinModulePath)
	schema, warnings, err := goschema.Read(".", moduleRoot, unobinRoot)
	require.NoError(t, err)
	require.Empty(t, warnings)
	return schema
}

func goModuleRoot(t *testing.T, module string) goschema.ModuleRoot {
	t.Helper()
	args := []string{"list", "-m", "-f", "{{.Path}}\n{{.Dir}}"}
	if module != "" {
		args = append(args, module)
	}
	out, err := exec.Command("go", args...).Output()
	require.NoError(t, err)
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, parts, 2)
	return goschema.ModuleRoot{Path: parts[0], Dir: parts[1]}
}
