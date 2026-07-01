package iam_test

import (
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/iam"
	awsiam "github.com/cloudboss/unobin-library-aws/service/iam"
)

const unobinModulePath = "github.com/cloudboss/unobin"

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

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestLibraryRegistersIAMLocalKinds(t *testing.T) {
	lib := awsiam.Library()
	require.NotNil(t, lib)
	require.Equal(t, "aws-iam", lib.Name)
	require.NotNil(t, lib.Configuration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), lib.Configuration.ValueType())

	require.Equal(t, []string{
		"access-key",
		"group",
		"group-policy",
		"group-policy-attachment",
		"instance-profile",
		"openid-connect-provider",
		"policy",
		"role",
		"role-policy",
		"role-policy-attachment",
		"user",
		"user-policy",
		"user-policy-attachment",
	}, sortedKeys(lib.Resources))
	require.Equal(t, []string{
		"openid-connect-provider",
	}, sortedKeys(lib.DataSources))
	require.Empty(t, sortedKeys(lib.Actions))

	resourcesOutputs := map[string]reflect.Type{
		"role":                    reflect.TypeFor[*svc.RoleOutput](),
		"group":                   reflect.TypeFor[*svc.GroupOutput](),
		"user":                    reflect.TypeFor[*svc.UserOutput](),
		"access-key":              reflect.TypeFor[*svc.AccessKeyOutput](),
		"policy":                  reflect.TypeFor[*svc.PolicyOutput](),
		"instance-profile":        reflect.TypeFor[*svc.InstanceProfileOutput](),
		"openid-connect-provider": reflect.TypeFor[*svc.OpenIDConnectProviderOutput](),
		"role-policy-attachment":  reflect.TypeFor[*svc.RolePolicyAttachmentOutput](),
		"group-policy-attachment": reflect.TypeFor[*svc.GroupPolicyAttachmentOutput](),
		"user-policy-attachment":  reflect.TypeFor[*svc.UserPolicyAttachmentOutput](),
		"role-policy":             reflect.TypeFor[*svc.RolePolicyOutput](),
		"group-policy":            reflect.TypeFor[*svc.GroupPolicyOutput](),
		"user-policy":             reflect.TypeFor[*svc.UserPolicyOutput](),
	}
	for name, outputType := range resourcesOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.Resources[name].OutputType())
		})
	}
	dataSourcesOutputs := map[string]reflect.Type{
		"openid-connect-provider": reflect.TypeFor[*svc.OpenIDConnectProviderDataOutput](),
	}
	for name, outputType := range dataSourcesOutputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.DataSources[name].OutputType())
		})
	}

}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(awsiam.Library().Configuration)
	require.NoError(t, err)
	require.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	require.NotEmpty(t, view.SchemaDigest)
}

func TestReadIAMServiceSchema(t *testing.T) {
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

	require.Equal(t, []string{
		"access-key",
		"group",
		"group-policy",
		"group-policy-attachment",
		"instance-profile",
		"openid-connect-provider",
		"policy",
		"role",
		"role-policy",
		"role-policy-attachment",
		"user",
		"user-policy",
		"user-policy-attachment",
	}, sortedKeys(schema.Resources))
	require.Equal(t, []string{
		"openid-connect-provider",
	}, sortedKeys(schema.DataSources))
	require.Empty(t, sortedKeys(schema.Actions))
}
