package library_test

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-aws"
	"github.com/cloudboss/unobin-library-aws/internal/service/ecs"
)

// TestLibraryRegistersEcs checks the runtime registration: the cluster, task
// definition, and service resources are present under Resources and dispatch
// to their output types.
func TestLibraryRegistersEcs(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"ecs-cluster":         reflect.TypeFor[*ecs.ClusterOutput](),
		"ecs-task-definition": reflect.TypeFor[*ecs.TaskDefinitionOutput](),
		"ecs-service":         reflect.TypeFor[*ecs.ServiceOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestEcsSchemas asserts the whole derived TypeSchema for the three ECS
// resources: input and output field types (including the task definition's
// container and volume models), the enum and placement rules, and the
// optional defaults.
func TestEcsSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	resources := map[string]*runtime.TypeSchema{
		"ecs-cluster": {
			Inputs: map[string]typecheck.Type{
				"capacity-providers": typecheck.TList(typecheck.TString()),
				"configuration": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "execute-command-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
						{Name: "logging", Type: typecheck.TString(), Optional: true},
						{Name: "log-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "cloud-watch-encryption-enabled", Type: typecheck.TBoolean(), Optional: true},
							{Name: "cloud-watch-log-group-name", Type: typecheck.TString(), Optional: true},
							{Name: "s3-bucket-name", Type: typecheck.TString(), Optional: true},
							{Name: "s3-encryption-enabled", Type: typecheck.TBoolean(), Optional: true},
							{Name: "s3-key-prefix", Type: typecheck.TString(), Optional: true},
						}), Optional: true},
					}), Optional: true},
					{Name: "managed-storage-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "kms-key-id", Type: typecheck.TString(), Optional: true},
						{Name: "fargate-ephemeral-storage-kms-key-id", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
				})),
				"default-capacity-provider-strategy": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "capacity-provider", Type: typecheck.TString()},
					{Name: "base", Type: typecheck.TInteger(), Optional: true},
					{Name: "weight", Type: typecheck.TInteger(), Optional: true},
				})),
				"name": typecheck.TString(),
				"service-connect-defaults": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "namespace", Type: typecheck.TString()},
				})),
				"settings": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "name", Type: typecheck.TString()},
					{Name: "value", Type: typecheck.TString()},
				})),
				"tags": typecheck.TMap(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(var.configuration.execute-command-configuration.logging != null)",
					Require: "(var.configuration.execute-command-configuration.logging == 'NONE' || " +
						"var.configuration.execute-command-configuration.logging == 'DEFAULT' || " +
						"var.configuration.execute-command-configuration.logging == 'OVERRIDE')",
					Message: "logging must be one of NONE, DEFAULT, or OVERRIDE",
				},
				{
					Kind:    "predicate",
					When:    "(var.configuration.execute-command-configuration.logging == 'OVERRIDE')",
					Require: "(var.configuration.execute-command-configuration.log-configuration != null)",
					Message: "log-configuration is required when logging is OVERRIDE",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value.name == 'containerInsights')",
					Message: "name must be containerInsights",
					ForEach: "var.settings",
				},
				{
					Kind: "predicate",
					When: "(@each.value.base != null)",
					Require: "(@each.value.base == null || " +
						"@each.value.base >= 0) && " +
						"(@each.value.base == null || " +
						"@each.value.base <= 100000)",
					Message: "base must be between 0 and 100000",
					ForEach: "var.default-capacity-provider-strategy",
				},
				{
					Kind: "predicate",
					When: "(@each.value.weight != null)",
					Require: "(@each.value.weight == null || " +
						"@each.value.weight >= 0) && " +
						"(@each.value.weight == null || " +
						"@each.value.weight <= 1000)",
					Message: "weight must be between 0 and 1000",
					ForEach: "var.default-capacity-provider-strategy",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.settings", Optional: true},
				{Field: "var.capacity-providers", Optional: true},
				{Field: "var.default-capacity-provider-strategy", Optional: true},
				{Field: "var.tags", Optional: true},
			},
		},
		"ecs-task-definition": {
			Inputs: map[string]typecheck.Type{
				"container-definitions": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "name", Type: typecheck.TString()},
					{Name: "image", Type: typecheck.TString()},
					{Name: "environment", Type: typecheck.TMap(typecheck.TString()), Optional: true},
					{Name: "command", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "cpu", Type: typecheck.TInteger(), Optional: true},
					{Name: "credential-specs", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "depends-on", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "condition", Type: typecheck.TString()},
						{Name: "container-name", Type: typecheck.TString()},
					})), Optional: true},
					{Name: "disable-networking", Type: typecheck.TBoolean(), Optional: true},
					{Name: "dns-search-domains", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "dns-servers", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "docker-labels", Type: typecheck.TMap(typecheck.TString()), Optional: true},
					{Name: "docker-security-options", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "entry-point", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "environment-files", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "type", Type: typecheck.TString()},
						{Name: "value", Type: typecheck.TString()},
					})), Optional: true},
					{Name: "essential", Type: typecheck.TBoolean(), Optional: true},
					{Name: "extra-hosts", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "hostname", Type: typecheck.TString()},
						{Name: "ip-address", Type: typecheck.TString()},
					})), Optional: true},
					{Name: "firelens-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "type", Type: typecheck.TString()},
						{Name: "options", Type: typecheck.TMap(typecheck.TString()), Optional: true},
					}), Optional: true},
					{Name: "health-check", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "command", Type: typecheck.TList(typecheck.TString())},
						{Name: "interval", Type: typecheck.TInteger(), Optional: true},
						{Name: "retries", Type: typecheck.TInteger(), Optional: true},
						{Name: "start-period", Type: typecheck.TInteger(), Optional: true},
						{Name: "timeout", Type: typecheck.TInteger(), Optional: true},
					}), Optional: true},
					{Name: "hostname", Type: typecheck.TString(), Optional: true},
					{Name: "interactive", Type: typecheck.TBoolean(), Optional: true},
					{Name: "links", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "linux-parameters", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "capabilities", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "add", Type: typecheck.TList(typecheck.TString()), Optional: true},
							{Name: "drop", Type: typecheck.TList(typecheck.TString()), Optional: true},
						}), Optional: true},
						{Name: "devices", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "host-path", Type: typecheck.TString()},
							{Name: "container-path", Type: typecheck.TString(), Optional: true},
							{Name: "permissions", Type: typecheck.TList(typecheck.TString()), Optional: true},
						})), Optional: true},
						{Name: "init-process-enabled", Type: typecheck.TBoolean(), Optional: true},
						{Name: "max-swap", Type: typecheck.TInteger(), Optional: true},
						{Name: "shared-memory-size", Type: typecheck.TInteger(), Optional: true},
						{Name: "swappiness", Type: typecheck.TInteger(), Optional: true},
						{Name: "tmpfs", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "container-path", Type: typecheck.TString()},
							{Name: "size", Type: typecheck.TInteger()},
							{Name: "mount-options", Type: typecheck.TList(typecheck.TString()), Optional: true},
						})), Optional: true},
					}), Optional: true},
					{Name: "log-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "log-driver", Type: typecheck.TString()},
						{Name: "options", Type: typecheck.TMap(typecheck.TString()), Optional: true},
						{Name: "secret-options", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "name", Type: typecheck.TString()},
							{Name: "value-from", Type: typecheck.TString()},
						})), Optional: true},
					}), Optional: true},
					{Name: "memory", Type: typecheck.TInteger(), Optional: true},
					{Name: "memory-reservation", Type: typecheck.TInteger(), Optional: true},
					{Name: "mount-points", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "container-path", Type: typecheck.TString(), Optional: true},
						{Name: "read-only", Type: typecheck.TBoolean(), Optional: true},
						{Name: "source-volume", Type: typecheck.TString(), Optional: true},
					})), Optional: true},
					{Name: "port-mappings", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "app-protocol", Type: typecheck.TString(), Optional: true},
						{Name: "container-port", Type: typecheck.TInteger(), Optional: true},
						{Name: "container-port-range", Type: typecheck.TString(), Optional: true},
						{Name: "host-port", Type: typecheck.TInteger(), Optional: true},
						{Name: "name", Type: typecheck.TString(), Optional: true},
						{Name: "protocol", Type: typecheck.TString(), Optional: true},
					})), Optional: true},
					{Name: "privileged", Type: typecheck.TBoolean(), Optional: true},
					{Name: "pseudo-terminal", Type: typecheck.TBoolean(), Optional: true},
					{Name: "readonly-root-filesystem", Type: typecheck.TBoolean(), Optional: true},
					{Name: "repository-credentials", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "credentials-parameter", Type: typecheck.TString()},
					}), Optional: true},
					{Name: "resource-requirements", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "type", Type: typecheck.TString()},
						{Name: "value", Type: typecheck.TString()},
					})), Optional: true},
					{Name: "restart-policy", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "enabled", Type: typecheck.TBoolean()},
						{Name: "ignored-exit-codes", Type: typecheck.TList(typecheck.TInteger()), Optional: true},
						{Name: "restart-attempt-period", Type: typecheck.TInteger(), Optional: true},
					}), Optional: true},
					{Name: "secrets", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "name", Type: typecheck.TString()},
						{Name: "value-from", Type: typecheck.TString()},
					})), Optional: true},
					{Name: "start-timeout", Type: typecheck.TInteger(), Optional: true},
					{Name: "stop-timeout", Type: typecheck.TInteger(), Optional: true},
					{Name: "system-controls", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "namespace", Type: typecheck.TString(), Optional: true},
						{Name: "value", Type: typecheck.TString(), Optional: true},
					})), Optional: true},
					{Name: "ulimits", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "hard-limit", Type: typecheck.TInteger()},
						{Name: "name", Type: typecheck.TString()},
						{Name: "soft-limit", Type: typecheck.TInteger()},
					})), Optional: true},
					{Name: "user", Type: typecheck.TString(), Optional: true},
					{Name: "version-consistency", Type: typecheck.TString(), Optional: true},
					{Name: "volumes-from", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "read-only", Type: typecheck.TBoolean(), Optional: true},
						{Name: "source-container", Type: typecheck.TString(), Optional: true},
					})), Optional: true},
					{Name: "working-directory", Type: typecheck.TString(), Optional: true},
				})),
				"cpu":                    typecheck.TOptional(typecheck.TString()),
				"enable-fault-injection": typecheck.TOptional(typecheck.TBoolean()),
				"ephemeral-storage": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "size-in-gib", Type: typecheck.TInteger()},
				})),
				"execution-role-arn": typecheck.TOptional(typecheck.TString()),
				"family":             typecheck.TString(),
				"ipc-mode":           typecheck.TOptional(typecheck.TString()),
				"memory":             typecheck.TOptional(typecheck.TString()),
				"network-mode":       typecheck.TOptional(typecheck.TString()),
				"pid-mode":           typecheck.TOptional(typecheck.TString()),
				"placement-constraints": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "type", Type: typecheck.TString()},
					{Name: "expression", Type: typecheck.TString(), Optional: true},
				})),
				"proxy-configuration": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "container-name", Type: typecheck.TString()},
					{Name: "properties", Type: typecheck.TMap(typecheck.TString()), Optional: true},
					{Name: "type", Type: typecheck.TString(), Optional: true},
				})),
				"requires-compatibilities": typecheck.TList(typecheck.TString()),
				"runtime-platform": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "cpu-architecture", Type: typecheck.TString(), Optional: true},
					{Name: "operating-system-family", Type: typecheck.TString(), Optional: true},
				})),
				"tags":          typecheck.TMap(typecheck.TString()),
				"task-role-arn": typecheck.TOptional(typecheck.TString()),
				"volumes": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "name", Type: typecheck.TString()},
					{Name: "configured-at-launch", Type: typecheck.TBoolean(), Optional: true},
					{Name: "host", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "source-path", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
					{Name: "docker-volume-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "autoprovision", Type: typecheck.TBoolean(), Optional: true},
						{Name: "driver", Type: typecheck.TString(), Optional: true},
						{Name: "driver-opts", Type: typecheck.TMap(typecheck.TString()), Optional: true},
						{Name: "labels", Type: typecheck.TMap(typecheck.TString()), Optional: true},
						{Name: "scope", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
					{Name: "efs-volume-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "file-system-id", Type: typecheck.TString()},
						{Name: "authorization-config", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "access-point-id", Type: typecheck.TString(), Optional: true},
							{Name: "iam", Type: typecheck.TString(), Optional: true},
						}), Optional: true},
						{Name: "root-directory", Type: typecheck.TString(), Optional: true},
						{Name: "transit-encryption", Type: typecheck.TString(), Optional: true},
						{Name: "transit-encryption-port", Type: typecheck.TInteger(), Optional: true},
					}), Optional: true},
					{Name: "fsx-windows-file-server-volume-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "file-system-id", Type: typecheck.TString()},
						{Name: "root-directory", Type: typecheck.TString()},
						{Name: "authorization-config", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "credentials-parameter", Type: typecheck.TString()},
							{Name: "domain", Type: typecheck.TString()},
						}), Optional: true},
					}), Optional: true},
					{Name: "s3files-volume-configuration", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "file-system-arn", Type: typecheck.TString()},
						{Name: "access-point-arn", Type: typecheck.TString(), Optional: true},
						{Name: "root-directory", Type: typecheck.TString(), Optional: true},
						{Name: "transit-encryption-port", Type: typecheck.TInteger(), Optional: true},
					}), Optional: true},
				})),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                  typecheck.TString(),
				"arn-without-revision": typecheck.TString(),
				"revision":             typecheck.TInteger(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(var.network-mode != null)",
					Require: "(var.network-mode == 'bridge' || " +
						"var.network-mode == 'host' || " +
						"var.network-mode == 'awsvpc' || " +
						"var.network-mode == 'none')",
					Message: "network-mode must be bridge, host, awsvpc, or none",
				},
				{
					Kind: "predicate",
					When: "(var.ipc-mode != null)",
					Require: "(var.ipc-mode == 'host' || " +
						"var.ipc-mode == 'task' || " +
						"var.ipc-mode == 'none')",
					Message: "ipc-mode must be host, task, or none",
				},
				{
					Kind:    "predicate",
					When:    "(var.pid-mode != null)",
					Require: "(var.pid-mode == 'host' || var.pid-mode == 'task')",
					Message: "pid-mode must be host or task",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value == 'EC2' || " +
						"@each.value == 'FARGATE' || " +
						"@each.value == 'EXTERNAL' || " +
						"@each.value == 'MANAGED_INSTANCES')",
					Message: "a compatibility must be EC2, FARGATE, EXTERNAL, or MANAGED_INSTANCES",
					ForEach: "var.requires-compatibilities",
				},
				{
					Kind: "predicate",
					When: "(var.ephemeral-storage.size-in-gib != null)",
					Require: "(var.ephemeral-storage.size-in-gib == null || " +
						"var.ephemeral-storage.size-in-gib >= 21) && " +
						"(var.ephemeral-storage.size-in-gib == null || " +
						"var.ephemeral-storage.size-in-gib <= 200)",
					Message: "ephemeral-storage size-in-gib must be between 21 and 200",
				},
				{
					Kind:    "predicate",
					When:    "(var.proxy-configuration.type != null)",
					Require: "(var.proxy-configuration.type == 'APPMESH')",
					Message: "proxy-configuration type must be APPMESH",
				},
				{
					Kind: "predicate",
					When: "(var.runtime-platform.cpu-architecture != null)",
					Require: "(var.runtime-platform.cpu-architecture == 'X86_64' || " +
						"var.runtime-platform.cpu-architecture == 'ARM64')",
					Message: "runtime-platform cpu-architecture must be X86_64 or ARM64",
				},
				{
					Kind: "predicate",
					When: "(var.runtime-platform.operating-system-family != null)",
					Require: "(var.runtime-platform.operating-system-family == 'LINUX' || " +
						"var.runtime-platform.operating-system-family == 'WINDOWS_SERVER_2016_FULL' || " +
						"var.runtime-platform.operating-system-family == 'WINDOWS_SERVER_2019_CORE' || " +
						"var.runtime-platform.operating-system-family == 'WINDOWS_SERVER_2019_FULL' || " +
						"var.runtime-platform.operating-system-family == 'WINDOWS_SERVER_2004_CORE' || " +
						"var.runtime-platform.operating-system-family == 'WINDOWS_SERVER_2022_CORE' || " +
						"var.runtime-platform.operating-system-family == 'WINDOWS_SERVER_2022_FULL' || " +
						"var.runtime-platform.operating-system-family == 'WINDOWS_SERVER_2025_CORE' || " +
						"var.runtime-platform.operating-system-family == 'WINDOWS_SERVER_2025_FULL' || " +
						"var.runtime-platform.operating-system-family == 'WINDOWS_SERVER_20H2_CORE')",
					Message: "runtime-platform operating-system-family must be LINUX or a WINDOWS_SERVER family",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.placement-constraints == null || " +
						"@core.length(var.placement-constraints) <= 10)",
					Message: "placement-constraints allows at most 10 entries",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value.type == 'memberOf')",
					Message: "a task definition placement constraint type must be memberOf",
					ForEach: "var.placement-constraints",
				},
				{
					Kind:    "predicate",
					When:    "(@each.value.type == 'memberOf')",
					Require: "(@each.value.expression != null)",
					Message: "a memberOf placement constraint requires an expression",
					ForEach: "var.placement-constraints",
				},
				{
					Kind: "predicate",
					When: "(@each.value.version-consistency != null)",
					Require: "(@each.value.version-consistency == 'enabled' || " +
						"@each.value.version-consistency == 'disabled')",
					Message: "version-consistency must be enabled or disabled",
					ForEach: "var.container-definitions",
				},
				{
					Kind: "predicate",
					When: "(@each.value.docker-volume-configuration.scope != null)",
					Require: "(@each.value.docker-volume-configuration.scope == 'task' || " +
						"@each.value.docker-volume-configuration.scope == 'shared')",
					Message: "a docker volume scope must be task or shared",
					ForEach: "var.volumes",
				},
				{
					Kind: "predicate",
					When: "(@each.value.efs-volume-configuration.transit-encryption != null)",
					Require: "(@each.value.efs-volume-configuration.transit-encryption == 'ENABLED' || " +
						"@each.value.efs-volume-configuration.transit-encryption == 'DISABLED')",
					Message: "efs transit-encryption must be ENABLED or DISABLED",
					ForEach: "var.volumes",
				},
				{
					Kind: "predicate",
					When: "(@each.value.efs-volume-configuration.transit-encryption-port != null)",
					Require: "(@each.value.efs-volume-configuration.transit-encryption-port == null || " +
						"@each.value.efs-volume-configuration.transit-encryption-port >= 1) && " +
						"(@each.value.efs-volume-configuration.transit-encryption-port == null || " +
						"@each.value.efs-volume-configuration.transit-encryption-port <= 65535)",
					Message: "efs transit-encryption-port must be between 1 and 65535",
					ForEach: "var.volumes",
				},
				{
					Kind: "predicate",
					When: "(@each.value.efs-volume-configuration.authorization-config.iam != null)",
					Require: "(@each.value.efs-volume-configuration.authorization-config.iam == 'ENABLED' || " +
						"@each.value.efs-volume-configuration.authorization-config.iam == 'DISABLED')",
					Message: "efs authorization-config iam must be ENABLED or DISABLED",
					ForEach: "var.volumes",
				},
				{
					Kind:    "predicate",
					When:    "(@each.value.fsx-windows-file-server-volume-configuration != null)",
					Require: "(@each.value.fsx-windows-file-server-volume-configuration.authorization-config != null)",
					Message: "an fsx-windows-file-server volume requires authorization-config",
					ForEach: "var.volumes",
				},
				{
					Kind: "predicate",
					When: "(@each.value.s3files-volume-configuration.transit-encryption-port != null)",
					Require: "(@each.value.s3files-volume-configuration.transit-encryption-port == null || " +
						"@each.value.s3files-volume-configuration.transit-encryption-port >= 1) && " +
						"(@each.value.s3files-volume-configuration.transit-encryption-port == null || " +
						"@each.value.s3files-volume-configuration.transit-encryption-port <= 65535)",
					Message: "s3files transit-encryption-port must be between 1 and 65535",
					ForEach: "var.volumes",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.placement-constraints", Optional: true},
				{Field: "var.requires-compatibilities", Optional: true},
				{Field: "var.volumes", Optional: true},
				{Field: "var.tags", Optional: true},
			},
		},
		"ecs-service": {
			Inputs: map[string]typecheck.Type{
				"availability-zone-rebalancing": typecheck.TOptional(typecheck.TString()),
				"capacity-provider-strategy": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "capacity-provider", Type: typecheck.TString()},
					{Name: "base", Type: typecheck.TInteger(), Optional: true},
					{Name: "weight", Type: typecheck.TInteger(), Optional: true},
				})),
				"cluster": typecheck.TOptional(typecheck.TString()),
				"deployment-configuration": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "maximum-percent", Type: typecheck.TInteger(), Optional: true},
					{Name: "minimum-healthy-percent", Type: typecheck.TInteger(), Optional: true},
					{Name: "deployment-circuit-breaker", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "enable", Type: typecheck.TBoolean()},
						{Name: "rollback", Type: typecheck.TBoolean()},
					}), Optional: true},
				})),
				"desired-count":                     typecheck.TOptional(typecheck.TInteger()),
				"enable-ecs-managed-tags":           typecheck.TOptional(typecheck.TBoolean()),
				"enable-execute-command":            typecheck.TOptional(typecheck.TBoolean()),
				"force-delete":                      typecheck.TOptional(typecheck.TBoolean()),
				"health-check-grace-period-seconds": typecheck.TOptional(typecheck.TInteger()),
				"launch-type":                       typecheck.TOptional(typecheck.TString()),
				"load-balancers": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "container-name", Type: typecheck.TString()},
					{Name: "container-port", Type: typecheck.TInteger()},
					{Name: "target-group-arn", Type: typecheck.TString()},
				})),
				"name": typecheck.TString(),
				"network-configuration": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "subnets", Type: typecheck.TList(typecheck.TString())},
					{Name: "security-groups", Type: typecheck.TList(typecheck.TString()), Optional: true},
					{Name: "assign-public-ip", Type: typecheck.TString(), Optional: true},
				})),
				"placement-constraints": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "type", Type: typecheck.TString()},
					{Name: "expression", Type: typecheck.TString(), Optional: true},
				})),
				"placement-strategy": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "type", Type: typecheck.TString()},
					{Name: "field", Type: typecheck.TString(), Optional: true},
				})),
				"platform-version":    typecheck.TOptional(typecheck.TString()),
				"propagate-tags":      typecheck.TOptional(typecheck.TString()),
				"scheduling-strategy": typecheck.TOptional(typecheck.TString()),
				"tags":                typecheck.TMap(typecheck.TString()),
				"task-definition":     typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"arn":         typecheck.TString(),
				"cluster-arn": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:   "at-most-one-of",
					Fields: []string{"var.launch-type", "var.capacity-provider-strategy"},
				},
				{
					Kind: "predicate",
					When: "(var.launch-type != null)",
					Require: "(var.launch-type == 'EC2' || " +
						"var.launch-type == 'FARGATE' || " +
						"var.launch-type == 'EXTERNAL')",
					Message: "launch-type must be EC2, FARGATE, or EXTERNAL",
				},
				{
					Kind: "predicate",
					When: "(var.scheduling-strategy != null)",
					Require: "(var.scheduling-strategy == 'REPLICA' || " +
						"var.scheduling-strategy == 'DAEMON')",
					Message: "scheduling-strategy must be REPLICA or DAEMON",
				},
				{
					Kind: "predicate",
					When: "(var.propagate-tags != null)",
					Require: "(var.propagate-tags == 'SERVICE' || " +
						"var.propagate-tags == 'TASK_DEFINITION' || " +
						"var.propagate-tags == 'NONE')",
					Message: "propagate-tags must be SERVICE, TASK_DEFINITION, or NONE",
				},
				{
					Kind: "predicate",
					When: "(var.availability-zone-rebalancing != null)",
					Require: "(var.availability-zone-rebalancing == 'ENABLED' || " +
						"var.availability-zone-rebalancing == 'DISABLED')",
					Message: "availability-zone-rebalancing must be ENABLED or DISABLED",
				},
				{
					Kind: "predicate",
					When: "(var.health-check-grace-period-seconds != null)",
					Require: "(var.health-check-grace-period-seconds == null || " +
						"var.health-check-grace-period-seconds >= 0) && " +
						"(var.health-check-grace-period-seconds == null || " +
						"var.health-check-grace-period-seconds <= 2147483647)",
					Message: "health-check-grace-period-seconds must be between 0 and 2147483647",
				},
				{
					Kind: "predicate",
					When: "(var.network-configuration != null)",
					Require: "((var.network-configuration.subnets != null) && " +
						"(@core.length(var.network-configuration.subnets) >= 1))",
					Message: "network-configuration subnets must not be empty",
				},
				{
					Kind: "predicate",
					When: "(var.network-configuration.assign-public-ip != null)",
					Require: "(var.network-configuration.assign-public-ip == 'ENABLED' || " +
						"var.network-configuration.assign-public-ip == 'DISABLED')",
					Message: "assign-public-ip must be ENABLED or DISABLED",
				},
				{
					Kind: "predicate",
					When: "(@each.value.base != null)",
					Require: "(@each.value.base == null || " +
						"@each.value.base >= 0) && " +
						"(@each.value.base == null || " +
						"@each.value.base <= 100000)",
					Message: "base must be between 0 and 100000",
					ForEach: "var.capacity-provider-strategy",
				},
				{
					Kind: "predicate",
					When: "(@each.value.weight != null)",
					Require: "(@each.value.weight == null || " +
						"@each.value.weight >= 0) && " +
						"(@each.value.weight == null || " +
						"@each.value.weight <= 1000)",
					Message: "weight must be between 0 and 1000",
					ForEach: "var.capacity-provider-strategy",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.container-port == null || " +
						"@each.value.container-port >= 0) && " +
						"(@each.value.container-port == null || " +
						"@each.value.container-port <= 65536)",
					Message: "container-port must be between 0 and 65536",
					ForEach: "var.load-balancers",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.placement-constraints == null || " +
						"@core.length(var.placement-constraints) <= 10)",
					Message: "placement-constraints allows at most 10 entries",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.type == 'distinctInstance' || " +
						"@each.value.type == 'memberOf')",
					Message: "type must be distinctInstance or memberOf",
					ForEach: "var.placement-constraints",
				},
				{
					Kind:    "predicate",
					When:    "(@each.value.type == 'memberOf')",
					Require: "(@each.value.expression != null)",
					Message: "a memberOf placement constraint requires an expression",
					ForEach: "var.placement-constraints",
				},
				{
					Kind:    "predicate",
					When:    "(@each.value.type == 'distinctInstance')",
					Require: "(@each.value.expression == null)",
					Message: "a distinctInstance placement constraint takes no expression",
					ForEach: "var.placement-constraints",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.placement-strategy == null || " +
						"@core.length(var.placement-strategy) <= 5)",
					Message: "placement-strategy allows at most 5 entries",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.type == 'random' || " +
						"@each.value.type == 'spread' || " +
						"@each.value.type == 'binpack')",
					Message: "type must be random, spread, or binpack",
					ForEach: "var.placement-strategy",
				},
				{
					Kind:    "predicate",
					When:    "(@each.value.type == 'random')",
					Require: "(@each.value.field == null)",
					Message: "a random placement strategy must omit field",
					ForEach: "var.placement-strategy",
				},
				{
					Kind: "predicate",
					When: "(@each.value.type == 'binpack')",
					Require: "(@each.value.field == 'cpu' || " +
						"@each.value.field == 'memory')",
					Message: "a binpack placement strategy field must be cpu or memory",
					ForEach: "var.placement-strategy",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.capacity-provider-strategy", Optional: true},
				{Field: "var.load-balancers", Optional: true},
				{Field: "var.placement-constraints", Optional: true},
				{Field: "var.placement-strategy", Optional: true},
				{Field: "var.tags", Optional: true},
			},
		},
	}
	for key, want := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, want, schema.Resources[key])
		})
	}
}
