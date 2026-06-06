package library_test

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	library "github.com/cloudboss/unobin-library-aws"
	"github.com/cloudboss/unobin-library-aws/internal/service/rds"
)

// TestLibraryRegistersRds checks the runtime registration: the six RDS
// resources are present under Resources and dispatch to their output types.
func TestLibraryRegistersRds(t *testing.T) {
	lib := library.Library()
	require.Contains(t, lib.Resources, "rds-subnet-group")
	assert.Equal(t, reflect.TypeFor[*rds.SubnetGroupOutput](),
		lib.Resources["rds-subnet-group"].OutputType())
	require.Contains(t, lib.Resources, "rds-parameter-group")
	assert.Equal(t, reflect.TypeFor[*rds.ParameterGroupOutput](),
		lib.Resources["rds-parameter-group"].OutputType())
	require.Contains(t, lib.Resources, "rds-cluster-parameter-group")
	assert.Equal(t, reflect.TypeFor[*rds.ClusterParameterGroupOutput](),
		lib.Resources["rds-cluster-parameter-group"].OutputType())
	require.Contains(t, lib.Resources, "rds-instance")
	assert.Equal(t, reflect.TypeFor[*rds.InstanceOutput](),
		lib.Resources["rds-instance"].OutputType())
	require.Contains(t, lib.Resources, "rds-cluster")
	assert.Equal(t, reflect.TypeFor[*rds.ClusterOutput](),
		lib.Resources["rds-cluster"].OutputType())
	require.Contains(t, lib.Resources, "rds-cluster-instance")
	assert.Equal(t, reflect.TypeFor[*rds.ClusterInstanceOutput](),
		lib.Resources["rds-cluster-instance"].OutputType())
}

// TestRdsSchemas asserts the whole derived TypeSchema -- input and output field
// types, sensitivity, the cross-field constraints, and the declared optional
// defaults -- for each RDS resource. The comparison is direct: goschema emits
// object fields in declaration order, and the fixtures match that order.
func TestRdsSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)
	tests := []struct {
		key  string
		want *runtime.TypeSchema
	}{
		{
			key: "rds-subnet-group",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"description": typecheck.TString(),
					"name":        typecheck.TString(),
					"subnet-ids":  typecheck.TList(typecheck.TString()),
					"tags":        typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn":                     typecheck.TString(),
					"supported-network-types": typecheck.TList(typecheck.TString()),
					"vpc-id":                  typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "true",
						Require: "((var.subnet-ids != null) && " +
							"(@core.length(var.subnet-ids) >= 1))",
						Message: "subnet-ids must not be empty",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "rds-parameter-group",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"description": typecheck.TString(),
					"family":      typecheck.TString(),
					"name":        typecheck.TString(),
					"parameters": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "name", Type: typecheck.TString()},
						{Name: "value", Type: typecheck.TString()},
						{Name: "apply-method", Type: typecheck.TString(), Optional: true},
					})),
					"tags": typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(@each.value.apply-method != null)",
						Require: "(@each.value.apply-method == 'immediate' || " +
							"@each.value.apply-method == 'pending-reboot')",
						Message: "apply-method must be immediate or pending-reboot",
						ForEach: "var.parameters",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.parameters", Optional: true},
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "rds-cluster-parameter-group",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"description": typecheck.TString(),
					"family":      typecheck.TString(),
					"name":        typecheck.TString(),
					"parameters": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "name", Type: typecheck.TString()},
						{Name: "value", Type: typecheck.TString()},
						{Name: "apply-method", Type: typecheck.TString(), Optional: true},
					})),
					"tags": typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn": typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(@each.value.apply-method != null)",
						Require: "(@each.value.apply-method == 'immediate' || " +
							"@each.value.apply-method == 'pending-reboot')",
						Message: "apply-method must be immediate or pending-reboot",
						ForEach: "var.parameters",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.parameters", Optional: true},
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "rds-instance",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"allocated-storage":           typecheck.TOptional(typecheck.TInteger()),
					"allow-major-version-upgrade": typecheck.TOptional(typecheck.TBoolean()),
					"auto-minor-version-upgrade":  typecheck.TOptional(typecheck.TBoolean()),
					"availability-zone":           typecheck.TOptional(typecheck.TString()),
					"backup-retention-period":     typecheck.TOptional(typecheck.TInteger()),
					"backup-target":               typecheck.TOptional(typecheck.TString()),
					"backup-window":               typecheck.TOptional(typecheck.TString()),
					"ca-cert-identifier":          typecheck.TOptional(typecheck.TString()),
					"character-set-name":          typecheck.TOptional(typecheck.TString()),
					"copy-tags-to-snapshot":       typecheck.TOptional(typecheck.TBoolean()),
					"custom-iam-instance-profile": typecheck.TOptional(typecheck.TString()),
					"customer-owned-ip-enabled":   typecheck.TOptional(typecheck.TBoolean()),
					"database-insights-mode":      typecheck.TOptional(typecheck.TString()),
					"db-name":                     typecheck.TOptional(typecheck.TString()),
					"db-subnet-group-name":        typecheck.TOptional(typecheck.TString()),
					"dedicated-log-volume":        typecheck.TOptional(typecheck.TBoolean()),
					"delete-automated-backups":    typecheck.TOptional(typecheck.TBoolean()),
					"deletion-protection":         typecheck.TOptional(typecheck.TBoolean()),
					"domain":                      typecheck.TOptional(typecheck.TString()),
					"domain-auth-secret-arn":      typecheck.TOptional(typecheck.TString()),
					"domain-dns-ips":              typecheck.TList(typecheck.TString()),
					"domain-fqdn":                 typecheck.TOptional(typecheck.TString()),
					"domain-iam-role-name":        typecheck.TOptional(typecheck.TString()),
					"domain-ou":                   typecheck.TOptional(typecheck.TString()),
					"enable-performance-insights": typecheck.TOptional(typecheck.TBoolean()),
					"enabled-cloudwatch-logs-exports": typecheck.TList(
						typecheck.TString()),
					"engine":                   typecheck.TOptional(typecheck.TString()),
					"engine-lifecycle-support": typecheck.TOptional(typecheck.TString()),
					"engine-version":           typecheck.TOptional(typecheck.TString()),
					"final-snapshot-identifier": typecheck.TOptional(
						typecheck.TString()),
					"iam-database-authentication-enabled": typecheck.TOptional(
						typecheck.TBoolean()),
					"identifier":                    typecheck.TString(),
					"instance-class":                typecheck.TOptional(typecheck.TString()),
					"iops":                          typecheck.TOptional(typecheck.TInteger()),
					"kms-key-id":                    typecheck.TOptional(typecheck.TString()),
					"license-model":                 typecheck.TOptional(typecheck.TString()),
					"maintenance-window":            typecheck.TOptional(typecheck.TString()),
					"manage-master-user-password":   typecheck.TOptional(typecheck.TBoolean()),
					"master-user-secret-kms-key-id": typecheck.TOptional(typecheck.TString()),
					"max-allocated-storage":         typecheck.TOptional(typecheck.TInteger()),
					"monitoring-interval":           typecheck.TOptional(typecheck.TInteger()),
					"monitoring-role-arn":           typecheck.TOptional(typecheck.TString()),
					"multi-az":                      typecheck.TOptional(typecheck.TBoolean()),
					"nchar-character-set-name":      typecheck.TOptional(typecheck.TString()),
					"network-type":                  typecheck.TOptional(typecheck.TString()),
					"option-group-name":             typecheck.TOptional(typecheck.TString()),
					"parameter-group-name":          typecheck.TOptional(typecheck.TString()),
					"password":                      typecheck.TOptional(typecheck.TString()),
					"performance-insights-kms-key-id": typecheck.TOptional(
						typecheck.TString()),
					"performance-insights-retention-period": typecheck.TOptional(
						typecheck.TInteger()),
					"port":                typecheck.TOptional(typecheck.TInteger()),
					"publicly-accessible": typecheck.TOptional(typecheck.TBoolean()),
					"replica-mode":        typecheck.TOptional(typecheck.TString()),
					"replicate-source-db": typecheck.TOptional(typecheck.TString()),
					"restore-to-point-in-time": typecheck.TOptional(
						typecheck.TObject([]typecheck.ObjectField{
							{Name: "restore-time", Type: typecheck.TString(), Optional: true},
							{
								Name:     "use-latest-restorable-time",
								Type:     typecheck.TBoolean(),
								Optional: true,
							},
							{
								Name:     "source-db-instance-identifier",
								Type:     typecheck.TString(),
								Optional: true,
							},
							{
								Name:     "source-dbi-resource-id",
								Type:     typecheck.TString(),
								Optional: true,
							},
							{
								Name:     "source-db-instance-automated-backups-arn",
								Type:     typecheck.TString(),
								Optional: true,
							},
						})),
					"s3-import": typecheck.TOptional(
						typecheck.TObject([]typecheck.ObjectField{
							{Name: "bucket-name", Type: typecheck.TString(), Optional: true},
							{Name: "bucket-prefix", Type: typecheck.TString(), Optional: true},
							{Name: "ingestion-role", Type: typecheck.TString(), Optional: true},
							{Name: "source-engine", Type: typecheck.TString(), Optional: true},
							{
								Name:     "source-engine-version",
								Type:     typecheck.TString(),
								Optional: true,
							},
						})),
					"skip-final-snapshot":    typecheck.TOptional(typecheck.TBoolean()),
					"snapshot-identifier":    typecheck.TOptional(typecheck.TString()),
					"storage-encrypted":      typecheck.TOptional(typecheck.TBoolean()),
					"storage-throughput":     typecheck.TOptional(typecheck.TInteger()),
					"storage-type":           typecheck.TOptional(typecheck.TString()),
					"tags":                   typecheck.TMap(typecheck.TString()),
					"timezone":               typecheck.TOptional(typecheck.TString()),
					"username":               typecheck.TOptional(typecheck.TString()),
					"vpc-security-group-ids": typecheck.TList(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"address":                typecheck.TString(),
					"arn":                    typecheck.TString(),
					"ca-cert-identifier":     typecheck.TString(),
					"endpoint":               typecheck.TString(),
					"engine-version-actual":  typecheck.TString(),
					"hosted-zone-id":         typecheck.TString(),
					"latest-restorable-time": typecheck.TString(),
					"listener-endpoint": typecheck.TOptional(
						typecheck.TObject([]typecheck.ObjectField{
							{Name: "address", Type: typecheck.TString()},
							{Name: "port", Type: typecheck.TInteger()},
							{Name: "hosted-zone-id", Type: typecheck.TString()},
						})),
					"master-user-secret": typecheck.TOptional(
						typecheck.TObject([]typecheck.ObjectField{
							{Name: "secret-arn", Type: typecheck.TString()},
							{Name: "kms-key-id", Type: typecheck.TString()},
							{Name: "secret-status", Type: typecheck.TString()},
						})),
					"port":        typecheck.TInteger(),
					"replicas":    typecheck.TList(typecheck.TString()),
					"resource-id": typecheck.TString(),
					"status":      typecheck.TString(),
				},
				SensitiveInputs: []string{"password"},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "at-most-one-of",
						Fields: []string{"var.replicate-source-db", "var.s3-import",
							"var.snapshot-identifier", "var.restore-to-point-in-time"},
					},
					{
						Kind:   "at-most-one-of",
						Fields: []string{"var.manage-master-user-password", "var.password"},
					},
					{
						Kind:   "at-most-one-of",
						Fields: []string{"var.domain", "var.domain-fqdn"},
					},
					{
						Kind:   "at-most-one-of",
						Fields: []string{"var.domain-iam-role-name", "var.domain-fqdn"},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{"var.character-set-name", "var.replicate-source-db",
							"var.s3-import", "var.snapshot-identifier",
							"var.restore-to-point-in-time"},
					},
					{
						Kind:   "forbidden-with",
						Fields: []string{"var.db-name", "var.replicate-source-db"},
					},
					{
						Kind:   "forbidden-with",
						Fields: []string{"var.username", "var.replicate-source-db"},
					},
					{
						Kind:   "forbidden-with",
						Fields: []string{"var.timezone", "var.s3-import"},
					},
					{
						Kind:   "forbidden-with",
						Fields: []string{"var.backup-target", "var.s3-import"},
					},
					{
						Kind: "predicate",
						When: "(var.database-insights-mode != null)",
						Require: "(var.database-insights-mode == 'standard' || " +
							"var.database-insights-mode == 'advanced')",
						Message: "database-insights-mode must be standard or advanced",
					},
					{
						Kind: "predicate",
						When: "(var.replica-mode != null)",
						Require: "(var.replica-mode == 'open-read-only' || " +
							"var.replica-mode == 'mounted')",
						Message: "replica-mode must be open-read-only or mounted",
					},
					{
						Kind: "predicate",
						When: "(var.engine-lifecycle-support != null)",
						Require: "(var.engine-lifecycle-support == " +
							"'open-source-rds-extended-support' || " +
							"var.engine-lifecycle-support == " +
							"'open-source-rds-extended-support-disabled')",
						Message: "engine-lifecycle-support must be a valid " +
							"extended-support value",
					},
					{
						Kind: "predicate",
						When: "(var.network-type != null)",
						Require: "(var.network-type == 'IPV4' || " +
							"var.network-type == 'DUAL')",
						Message: "network-type must be IPV4 or DUAL",
					},
					{
						Kind: "predicate",
						When: "(var.backup-target != null)",
						Require: "(var.backup-target == 'outposts' || " +
							"var.backup-target == 'region')",
						Message: "backup-target must be outposts or region",
					},
					{
						Kind: "predicate",
						When: "(var.storage-type != null)",
						Require: "(var.storage-type == 'gp2' || var.storage-type == 'gp3' " +
							"|| var.storage-type == 'io1' || var.storage-type == 'io2' || " +
							"var.storage-type == 'standard')",
						Message: "storage-type must be gp2, gp3, io1, io2, or standard",
					},
					{
						Kind: "predicate",
						When: "(var.backup-retention-period != null)",
						Require: "(var.backup-retention-period == null || " +
							"var.backup-retention-period >= 0) && " +
							"(var.backup-retention-period == null || " +
							"var.backup-retention-period <= 35)",
						Message: "backup-retention-period must be between 0 and 35",
					},
					{
						Kind: "predicate",
						When: "(var.monitoring-interval != null)",
						Require: "(var.monitoring-interval == 0 || " +
							"var.monitoring-interval == 1 || " +
							"var.monitoring-interval == 5 || " +
							"var.monitoring-interval == 10 || " +
							"var.monitoring-interval == 15 || " +
							"var.monitoring-interval == 30 || " +
							"var.monitoring-interval == 60)",
						Message: "monitoring-interval must be 0, 1, 5, 10, 15, 30, or 60",
					},
					{
						Kind: "predicate",
						When: "true",
						Require: "(@each.value == 'agent' || @each.value == 'alert' || " +
							"@each.value == 'audit' || @each.value == 'diag.log' || " +
							"@each.value == 'error' || @each.value == 'general' || " +
							"@each.value == 'iam-db-auth-error' || " +
							"@each.value == 'listener' || @each.value == 'notify.log' || " +
							"@each.value == 'oemagent' || @each.value == 'postgresql' || " +
							"@each.value == 'slowquery' || @each.value == 'trace' || " +
							"@each.value == 'upgrade')",
						Message: "enabled-cloudwatch-logs-exports entries must be " +
							"valid instance log types",
						ForEach: "var.enabled-cloudwatch-logs-exports",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.vpc-security-group-ids", Optional: true},
					{Field: "var.enabled-cloudwatch-logs-exports", Optional: true},
					{Field: "var.domain-dns-ips", Optional: true},
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "rds-cluster",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"allocated-storage":         typecheck.TOptional(typecheck.TInteger()),
					"availability-zones":        typecheck.TList(typecheck.TString()),
					"backtrack-window":          typecheck.TOptional(typecheck.TInteger()),
					"backup-retention-period":   typecheck.TOptional(typecheck.TInteger()),
					"ca-certificate-identifier": typecheck.TOptional(typecheck.TString()),
					"cluster-identifier":        typecheck.TString(),
					"cluster-scalability-type":  typecheck.TOptional(typecheck.TString()),
					"copy-tags-to-snapshot":     typecheck.TOptional(typecheck.TBoolean()),
					"database-insights-mode":    typecheck.TOptional(typecheck.TString()),
					"database-name":             typecheck.TOptional(typecheck.TString()),
					"db-cluster-instance-class": typecheck.TOptional(typecheck.TString()),
					"db-cluster-parameter-group-name": typecheck.TOptional(
						typecheck.TString()),
					"db-instance-parameter-group-name": typecheck.TOptional(
						typecheck.TString()),
					"db-subnet-group-name":     typecheck.TOptional(typecheck.TString()),
					"db-system-id":             typecheck.TOptional(typecheck.TString()),
					"delete-automated-backups": typecheck.TOptional(typecheck.TBoolean()),
					"deletion-protection":      typecheck.TOptional(typecheck.TBoolean()),
					"domain":                   typecheck.TOptional(typecheck.TString()),
					"domain-iam-role-name":     typecheck.TOptional(typecheck.TString()),
					"enable-global-write-forwarding": typecheck.TOptional(
						typecheck.TBoolean()),
					"enable-http-endpoint": typecheck.TOptional(typecheck.TBoolean()),
					"enable-iam-database-authentication": typecheck.TOptional(
						typecheck.TBoolean()),
					"enable-local-write-forwarding": typecheck.TOptional(
						typecheck.TBoolean()),
					"enabled-cloudwatch-logs-exports": typecheck.TList(
						typecheck.TString()),
					"engine":                    typecheck.TString(),
					"engine-lifecycle-support":  typecheck.TOptional(typecheck.TString()),
					"engine-mode":               typecheck.TOptional(typecheck.TString()),
					"engine-version":            typecheck.TOptional(typecheck.TString()),
					"final-snapshot-identifier": typecheck.TOptional(typecheck.TString()),
					"global-cluster-identifier": typecheck.TOptional(typecheck.TString()),
					"iam-roles":                 typecheck.TList(typecheck.TString()),
					"iops":                      typecheck.TOptional(typecheck.TInteger()),
					"kms-key-id":                typecheck.TOptional(typecheck.TString()),
					"manage-master-user-password": typecheck.TOptional(
						typecheck.TBoolean()),
					"master-password":               typecheck.TOptional(typecheck.TString()),
					"master-user-secret-kms-key-id": typecheck.TOptional(typecheck.TString()),
					"master-username":               typecheck.TOptional(typecheck.TString()),
					"monitoring-interval":           typecheck.TOptional(typecheck.TInteger()),
					"monitoring-role-arn":           typecheck.TOptional(typecheck.TString()),
					"network-type":                  typecheck.TOptional(typecheck.TString()),
					"performance-insights-enabled": typecheck.TOptional(
						typecheck.TBoolean()),
					"performance-insights-kms-key-id": typecheck.TOptional(
						typecheck.TString()),
					"performance-insights-retention-period": typecheck.TOptional(
						typecheck.TInteger()),
					"port":                          typecheck.TOptional(typecheck.TInteger()),
					"preferred-backup-window":       typecheck.TOptional(typecheck.TString()),
					"preferred-maintenance-window":  typecheck.TOptional(typecheck.TString()),
					"replication-source-identifier": typecheck.TOptional(typecheck.TString()),
					"restore-to-point-in-time": typecheck.TOptional(
						typecheck.TObject([]typecheck.ObjectField{
							{
								Name:     "source-cluster-identifier",
								Type:     typecheck.TString(),
								Optional: true,
							},
							{
								Name:     "source-cluster-resource-id",
								Type:     typecheck.TString(),
								Optional: true,
							},
							{Name: "restore-to-time", Type: typecheck.TString(), Optional: true},
							{
								Name:     "use-latest-restorable-time",
								Type:     typecheck.TBoolean(),
								Optional: true,
							},
							{Name: "restore-type", Type: typecheck.TString(), Optional: true},
						})),
					"s3-import": typecheck.TOptional(
						typecheck.TObject([]typecheck.ObjectField{
							{Name: "bucket-name", Type: typecheck.TString()},
							{Name: "bucket-prefix", Type: typecheck.TString(), Optional: true},
							{Name: "ingestion-role", Type: typecheck.TString()},
							{Name: "source-engine", Type: typecheck.TString()},
							{Name: "source-engine-version", Type: typecheck.TString()},
						})),
					"scaling": typecheck.TOptional(
						typecheck.TObject([]typecheck.ObjectField{
							{Name: "auto-pause", Type: typecheck.TBoolean(), Optional: true},
							{Name: "max-capacity", Type: typecheck.TInteger(), Optional: true},
							{Name: "min-capacity", Type: typecheck.TInteger(), Optional: true},
							{
								Name:     "seconds-before-timeout",
								Type:     typecheck.TInteger(),
								Optional: true,
							},
							{
								Name:     "seconds-until-auto-pause",
								Type:     typecheck.TInteger(),
								Optional: true,
							},
							{Name: "timeout-action", Type: typecheck.TString(), Optional: true},
						})),
					"serverlessv2-scaling": typecheck.TOptional(
						typecheck.TObject([]typecheck.ObjectField{
							{Name: "max-capacity", Type: typecheck.TNumber(), Optional: true},
							{Name: "min-capacity", Type: typecheck.TNumber(), Optional: true},
							{
								Name:     "seconds-until-auto-pause",
								Type:     typecheck.TInteger(),
								Optional: true,
							},
						})),
					"skip-final-snapshot":    typecheck.TOptional(typecheck.TBoolean()),
					"snapshot-identifier":    typecheck.TOptional(typecheck.TString()),
					"source-region":          typecheck.TOptional(typecheck.TString()),
					"storage-encrypted":      typecheck.TOptional(typecheck.TBoolean()),
					"storage-type":           typecheck.TOptional(typecheck.TString()),
					"tags":                   typecheck.TMap(typecheck.TString()),
					"vpc-security-group-ids": typecheck.TList(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn":                       typecheck.TString(),
					"cluster-members":           typecheck.TList(typecheck.TString()),
					"cluster-resource-id":       typecheck.TString(),
					"endpoint":                  typecheck.TString(),
					"engine-version-actual":     typecheck.TString(),
					"global-cluster-identifier": typecheck.TString(),
					"hosted-zone-id":            typecheck.TString(),
					"master-user-secret": typecheck.TOptional(
						typecheck.TObject([]typecheck.ObjectField{
							{Name: "secret-arn", Type: typecheck.TString()},
							{Name: "kms-key-id", Type: typecheck.TString()},
							{Name: "secret-status", Type: typecheck.TString()},
						})),
					"port":            typecheck.TInteger(),
					"reader-endpoint": typecheck.TString(),
				},
				SensitiveInputs: []string{"master-password"},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "at-most-one-of",
						Fields: []string{"var.snapshot-identifier", "var.s3-import",
							"var.restore-to-point-in-time"},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{"var.snapshot-identifier",
							"var.global-cluster-identifier"},
					},
					{
						Kind: "at-most-one-of",
						Fields: []string{"var.manage-master-user-password",
							"var.master-password"},
					},
					{
						Kind: "predicate",
						When: "(var.engine-mode != null)",
						Require: "(var.engine-mode == 'global' || " +
							"var.engine-mode == 'multimaster' || " +
							"var.engine-mode == 'parallelquery' || " +
							"var.engine-mode == 'provisioned' || " +
							"var.engine-mode == 'serverless')",
						Message: "engine-mode must be one of global, multimaster, " +
							"parallelquery, provisioned, or serverless",
					},
					{
						Kind: "predicate",
						When: "(var.cluster-scalability-type != null)",
						Require: "(var.cluster-scalability-type == 'standard' || " +
							"var.cluster-scalability-type == 'limitless')",
						Message: "cluster-scalability-type must be standard or limitless",
					},
					{
						Kind: "predicate",
						When: "(var.database-insights-mode != null)",
						Require: "(var.database-insights-mode == 'standard' || " +
							"var.database-insights-mode == 'advanced')",
						Message: "database-insights-mode must be standard or advanced",
					},
					{
						Kind: "predicate",
						When: "(var.engine-lifecycle-support != null)",
						Require: "(var.engine-lifecycle-support == " +
							"'open-source-rds-extended-support' || " +
							"var.engine-lifecycle-support == " +
							"'open-source-rds-extended-support-disabled')",
						Message: "engine-lifecycle-support must be " +
							"open-source-rds-extended-support or " +
							"open-source-rds-extended-support-disabled",
					},
					{
						Kind: "predicate",
						When: "(var.network-type != null)",
						Require: "(var.network-type == 'DUAL' || " +
							"var.network-type == 'IPV4')",
						Message: "network-type must be DUAL or IPV4",
					},
					{
						Kind: "predicate",
						When: "(var.backup-retention-period != null)",
						Require: "(var.backup-retention-period == null || " +
							"var.backup-retention-period <= 35)",
						Message: "backup-retention-period must be at most 35",
					},
					{
						Kind: "predicate",
						When: "(var.backtrack-window != null)",
						Require: "(var.backtrack-window == null || " +
							"var.backtrack-window >= 0) && " +
							"(var.backtrack-window == null || " +
							"var.backtrack-window <= 259200)",
						Message: "backtrack-window must be between 0 and 259200",
					},
					{
						Kind: "predicate",
						When: "true",
						Require: "(@each.value == 'audit' || @each.value == 'error' || " +
							"@each.value == 'general' || " +
							"@each.value == 'iam-db-auth-error' || " +
							"@each.value == 'instance' || @each.value == 'postgresql' || " +
							"@each.value == 'slowquery' || @each.value == 'upgrade')",
						Message: "enabled-cloudwatch-logs-exports entries must be " +
							"valid cluster log types",
						ForEach: "var.enabled-cloudwatch-logs-exports",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.availability-zones", Optional: true},
					{Field: "var.enabled-cloudwatch-logs-exports", Optional: true},
					{Field: "var.iam-roles", Optional: true},
					{Field: "var.vpc-security-group-ids", Optional: true},
					{Field: "var.tags", Optional: true},
				},
			},
		},
		{
			key: "rds-cluster-instance",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"auto-minor-version-upgrade":  typecheck.TOptional(typecheck.TBoolean()),
					"availability-zone":           typecheck.TOptional(typecheck.TString()),
					"ca-cert-identifier":          typecheck.TOptional(typecheck.TString()),
					"cluster-identifier":          typecheck.TString(),
					"copy-tags-to-snapshot":       typecheck.TOptional(typecheck.TBoolean()),
					"custom-iam-instance-profile": typecheck.TOptional(typecheck.TString()),
					"db-parameter-group-name":     typecheck.TOptional(typecheck.TString()),
					"db-subnet-group-name":        typecheck.TOptional(typecheck.TString()),
					"engine":                      typecheck.TString(),
					"engine-version":              typecheck.TOptional(typecheck.TString()),
					"force-destroy":               typecheck.TOptional(typecheck.TBoolean()),
					"identifier":                  typecheck.TString(),
					"instance-class":              typecheck.TString(),
					"monitoring-interval":         typecheck.TOptional(typecheck.TInteger()),
					"monitoring-role-arn":         typecheck.TOptional(typecheck.TString()),
					"performance-insights-enabled": typecheck.TOptional(
						typecheck.TBoolean()),
					"performance-insights-kms-key-id": typecheck.TOptional(
						typecheck.TString()),
					"performance-insights-retention-period": typecheck.TOptional(
						typecheck.TInteger()),
					"preferred-backup-window":      typecheck.TOptional(typecheck.TString()),
					"preferred-maintenance-window": typecheck.TOptional(typecheck.TString()),
					"promotion-tier":               typecheck.TOptional(typecheck.TInteger()),
					"publicly-accessible":          typecheck.TOptional(typecheck.TBoolean()),
					"tags":                         typecheck.TMap(typecheck.TString()),
				},
				Outputs: map[string]typecheck.Type{
					"arn":                          typecheck.TString(),
					"availability-zone":            typecheck.TString(),
					"ca-cert-identifier":           typecheck.TString(),
					"db-parameter-group-name":      typecheck.TString(),
					"dbi-resource-id":              typecheck.TString(),
					"endpoint":                     typecheck.TString(),
					"engine-version-actual":        typecheck.TString(),
					"kms-key-id":                   typecheck.TString(),
					"network-type":                 typecheck.TString(),
					"port":                         typecheck.TInteger(),
					"preferred-backup-window":      typecheck.TString(),
					"preferred-maintenance-window": typecheck.TString(),
					"storage-encrypted":            typecheck.TBoolean(),
					"writer":                       typecheck.TBoolean(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind: "predicate",
						When: "(var.performance-insights-retention-period != null)",
						Require: "(var.performance-insights-retention-period == null || " +
							"var.performance-insights-retention-period >= 7) && " +
							"(var.performance-insights-retention-period == null || " +
							"var.performance-insights-retention-period <= 731)",
						Message: "performance-insights-retention-period must be " +
							"between 7 and 731",
					},
				},
				Defaults: []lang.DefaultSpec{
					{Field: "var.tags", Optional: true},
				},
			},
		},
	}
	for _, tt := range tests {
		require.Contains(t, schema.Resources, tt.key)
		assert.Equal(t, tt.want, schema.Resources[tt.key],
			"schema mismatch for %s", tt.key)
	}
}
