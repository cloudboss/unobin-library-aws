package rds

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/rds"
)

// TestLibraryRegistersRds checks the runtime registration: the six RDS
// resources are present under Resources and dispatch to their output types.
func TestLibraryRegistersRds(t *testing.T) {
	lib := Library()
	require.Contains(t, lib.Resources, "subnet-group")
	assert.Equal(t, reflect.TypeFor[*svc.SubnetGroupResourceOutput](),
		lib.Resources["subnet-group"].OutputType())
	require.Contains(t, lib.Resources, "parameter-group")
	assert.Equal(t, reflect.TypeFor[*svc.ParameterGroupResourceOutput](),
		lib.Resources["parameter-group"].OutputType())
	require.Contains(t, lib.Resources, "cluster-parameter-group")
	assert.Equal(t, reflect.TypeFor[*svc.ClusterParameterGroupResourceOutput](),
		lib.Resources["cluster-parameter-group"].OutputType())
	require.Contains(t, lib.Resources, "instance")
	assert.Equal(t, reflect.TypeFor[*svc.InstanceResourceOutput](),
		lib.Resources["instance"].OutputType())
	require.Contains(t, lib.Resources, "cluster")
	assert.Equal(t, reflect.TypeFor[*svc.ClusterResourceOutput](),
		lib.Resources["cluster"].OutputType())
	require.Contains(t, lib.Resources, "cluster-instance")
	assert.Equal(t, reflect.TypeFor[*svc.ClusterInstanceResourceOutput](),
		lib.Resources["cluster-instance"].OutputType())
}

// TestRdsSchemas asserts the whole derived TypeSchema -- input and output field
// types, sensitivity, the cross-field constraints, and the declared optional
// defaults -- for each RDS resource. The comparison is direct: goschema emits
// object fields in declaration order, and the fixtures match that order.
func TestRdsSchemas(t *testing.T) {
	schema := readLibrarySchema(t)
	tests := []struct {
		key  string
		want *runtime.TypeSchema
	}{
		{
			key: "subnet-group",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"description": typecheck.TString(),
					"name":        typecheck.TString(),
					"subnet-ids":  typecheck.TList(typecheck.TString()),
					"tags":        typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				},
				Outputs: map[string]typecheck.Type{
					"arn":                     typecheck.TString(),
					"supported-network-types": typecheck.TList(typecheck.TString()),
					"vpc-id":                  typecheck.TString(),
				},
				Constraints: []lang.ConstraintSpec{
					{
						Kind:    "predicate",
						When:    "true",
						Require: "(@core.length(input.subnet-ids) >= 1)",
						Message: "subnet-ids must not be empty",
					},
				},
			},
		},
		{
			key: "parameter-group",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"description": typecheck.TString(),
					"family":      typecheck.TString(),
					"name":        typecheck.TString(),
					"parameters": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "name", Type: typecheck.TString()},
						{Name: "value", Type: typecheck.TString()},
						{Name: "apply-method", Type: typecheck.TString(), Optional: true},
					}))),
					"tags": typecheck.TOptional(typecheck.TMap(typecheck.TString())),
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
						ForEach: "input.parameters ?? []",
					},
				},
			},
		},
		{
			key: "cluster-parameter-group",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"description": typecheck.TString(),
					"family":      typecheck.TString(),
					"name":        typecheck.TString(),
					"parameters": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "name", Type: typecheck.TString()},
						{Name: "value", Type: typecheck.TString()},
						{Name: "apply-method", Type: typecheck.TString(), Optional: true},
					}))),
					"tags": typecheck.TOptional(typecheck.TMap(typecheck.TString())),
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
						ForEach: "input.parameters ?? []",
					},
				},
			},
		},
		{
			key: "instance",
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
					"domain-dns-ips":              typecheck.TOptional(typecheck.TList(typecheck.TString())),
					"domain-fqdn":                 typecheck.TOptional(typecheck.TString()),
					"domain-iam-role-name":        typecheck.TOptional(typecheck.TString()),
					"domain-ou":                   typecheck.TOptional(typecheck.TString()),
					"enable-performance-insights": typecheck.TOptional(typecheck.TBoolean()),
					"enabled-cloudwatch-logs-exports": typecheck.TOptional(typecheck.TList(
						typecheck.TString())),
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
					"tags":                   typecheck.TOptional(typecheck.TMap(typecheck.TString())),
					"timezone":               typecheck.TOptional(typecheck.TString()),
					"username":               typecheck.TOptional(typecheck.TString()),
					"vpc-security-group-ids": typecheck.TOptional(typecheck.TList(typecheck.TString())),
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
						Fields: []string{"input.replicate-source-db", "input.s3-import",
							"input.snapshot-identifier", "input.restore-to-point-in-time"},
					},
					{
						Kind:   "at-most-one-of",
						Fields: []string{"input.manage-master-user-password", "input.password"},
					},
					{
						Kind:   "at-most-one-of",
						Fields: []string{"input.domain", "input.domain-fqdn"},
					},
					{
						Kind:   "at-most-one-of",
						Fields: []string{"input.domain-iam-role-name", "input.domain-fqdn"},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{"input.character-set-name", "input.replicate-source-db",
							"input.s3-import", "input.snapshot-identifier",
							"input.restore-to-point-in-time"},
					},
					{
						Kind:   "forbidden-with",
						Fields: []string{"input.db-name", "input.replicate-source-db"},
					},
					{
						Kind:   "forbidden-with",
						Fields: []string{"input.username", "input.replicate-source-db"},
					},
					{
						Kind:   "forbidden-with",
						Fields: []string{"input.timezone", "input.s3-import"},
					},
					{
						Kind:   "forbidden-with",
						Fields: []string{"input.backup-target", "input.s3-import"},
					},
					{
						Kind: "predicate",
						When: "(input.database-insights-mode != null)",
						Require: "(input.database-insights-mode == 'standard' || " +
							"input.database-insights-mode == 'advanced')",
						Message: "database-insights-mode must be standard or advanced",
					},
					{
						Kind: "predicate",
						When: "(input.replica-mode != null)",
						Require: "(input.replica-mode == 'open-read-only' || " +
							"input.replica-mode == 'mounted')",
						Message: "replica-mode must be open-read-only or mounted",
					},
					{
						Kind: "predicate",
						When: "(input.engine-lifecycle-support != null)",
						Require: "(input.engine-lifecycle-support == " +
							"'open-source-rds-extended-support' || " +
							"input.engine-lifecycle-support == " +
							"'open-source-rds-extended-support-disabled')",
						Message: "engine-lifecycle-support must be a valid " +
							"extended-support value",
					},
					{
						Kind: "predicate",
						When: "(input.network-type != null)",
						Require: "(input.network-type == 'IPV4' || " +
							"input.network-type == 'DUAL')",
						Message: "network-type must be IPV4 or DUAL",
					},
					{
						Kind: "predicate",
						When: "(input.vpc-security-group-ids != null)",
						Require: "(input.vpc-security-group-ids == null || " +
							"@core.length(input.vpc-security-group-ids) >= 1)",
						Message: "vpc-security-group-ids must list at least one group when given",
					},
					{
						Kind: "predicate",
						When: "(input.domain-dns-ips != null)",
						Require: "(input.domain-dns-ips == null || " +
							"@core.length(input.domain-dns-ips) >= 2) && " +
							"(input.domain-dns-ips == null || " +
							"@core.length(input.domain-dns-ips) <= 2)",
						Message: "domain-dns-ips must contain exactly two IP addresses when given",
					},
					{
						Kind: "predicate",
						When: "(input.backup-target != null)",
						Require: "(input.backup-target == 'outposts' || " +
							"input.backup-target == 'region')",
						Message: "backup-target must be outposts or region",
					},
					{
						Kind: "predicate",
						When: "(input.storage-type != null)",
						Require: "(input.storage-type == 'gp2' || input.storage-type == 'gp3' " +
							"|| input.storage-type == 'io1' || input.storage-type == 'io2' || " +
							"input.storage-type == 'standard')",
						Message: "storage-type must be gp2, gp3, io1, io2, or standard",
					},
					{
						Kind: "predicate",
						When: "(input.backup-retention-period != null)",
						Require: "(input.backup-retention-period == null || " +
							"input.backup-retention-period >= 0) && " +
							"(input.backup-retention-period == null || " +
							"input.backup-retention-period <= 35)",
						Message: "backup-retention-period must be between 0 and 35",
					},
					{
						Kind: "predicate",
						When: "(input.monitoring-interval != null)",
						Require: "(input.monitoring-interval == 0 || " +
							"input.monitoring-interval == 1 || " +
							"input.monitoring-interval == 5 || " +
							"input.monitoring-interval == 10 || " +
							"input.monitoring-interval == 15 || " +
							"input.monitoring-interval == 30 || " +
							"input.monitoring-interval == 60)",
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
						ForEach: "input.enabled-cloudwatch-logs-exports ?? []",
					},
				},
			},
		},
		{
			key: "cluster",
			want: &runtime.TypeSchema{
				Inputs: map[string]typecheck.Type{
					"allocated-storage":         typecheck.TOptional(typecheck.TInteger()),
					"availability-zones":        typecheck.TOptional(typecheck.TList(typecheck.TString())),
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
					"enabled-cloudwatch-logs-exports": typecheck.TOptional(typecheck.TList(
						typecheck.TString())),
					"engine":                    typecheck.TString(),
					"engine-lifecycle-support":  typecheck.TOptional(typecheck.TString()),
					"engine-mode":               typecheck.TOptional(typecheck.TString()),
					"engine-version":            typecheck.TOptional(typecheck.TString()),
					"final-snapshot-identifier": typecheck.TOptional(typecheck.TString()),
					"global-cluster-identifier": typecheck.TOptional(typecheck.TString()),
					"iam-roles":                 typecheck.TOptional(typecheck.TList(typecheck.TString())),
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
					"skip-final-snapshot": typecheck.TOptional(typecheck.TBoolean()),
					"snapshot-identifier": typecheck.TOptional(typecheck.TString()),
					"source-region":       typecheck.TOptional(typecheck.TString()),
					"storage-encrypted":   typecheck.TOptional(typecheck.TBoolean()),
					"storage-type":        typecheck.TOptional(typecheck.TString()),
					"tags":                typecheck.TOptional(typecheck.TMap(typecheck.TString())),
					"vpc-security-group-ids": typecheck.TOptional(typecheck.TList(
						typecheck.TString())),
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
						Fields: []string{"input.snapshot-identifier", "input.s3-import",
							"input.restore-to-point-in-time"},
					},
					{
						Kind: "forbidden-with",
						Fields: []string{"input.snapshot-identifier",
							"input.global-cluster-identifier"},
					},
					{
						Kind: "at-most-one-of",
						Fields: []string{"input.manage-master-user-password",
							"input.master-password"},
					},
					{
						Kind: "predicate",
						When: "(input.engine-mode != null)",
						Require: "(input.engine-mode == 'global' || " +
							"input.engine-mode == 'multimaster' || " +
							"input.engine-mode == 'parallelquery' || " +
							"input.engine-mode == 'provisioned' || " +
							"input.engine-mode == 'serverless')",
						Message: "engine-mode must be one of global, multimaster, " +
							"parallelquery, provisioned, or serverless",
					},
					{
						Kind: "predicate",
						When: "(input.cluster-scalability-type != null)",
						Require: "(input.cluster-scalability-type == 'standard' || " +
							"input.cluster-scalability-type == 'limitless')",
						Message: "cluster-scalability-type must be standard or limitless",
					},
					{
						Kind: "predicate",
						When: "(input.database-insights-mode != null)",
						Require: "(input.database-insights-mode == 'standard' || " +
							"input.database-insights-mode == 'advanced')",
						Message: "database-insights-mode must be standard or advanced",
					},
					{
						Kind: "predicate",
						When: "(input.engine-lifecycle-support != null)",
						Require: "(input.engine-lifecycle-support == " +
							"'open-source-rds-extended-support' || " +
							"input.engine-lifecycle-support == " +
							"'open-source-rds-extended-support-disabled')",
						Message: "engine-lifecycle-support must be " +
							"open-source-rds-extended-support or " +
							"open-source-rds-extended-support-disabled",
					},
					{
						Kind: "predicate",
						When: "(input.network-type != null)",
						Require: "(input.network-type == 'DUAL' || " +
							"input.network-type == 'IPV4')",
						Message: "network-type must be DUAL or IPV4",
					},
					{
						Kind: "predicate",
						When: "(input.availability-zones != null)",
						Require: "(input.availability-zones == null || " +
							"@core.length(input.availability-zones) >= 1)",
						Message: "availability-zones must list at least one zone when given",
					},
					{
						Kind: "predicate",
						When: "(input.backup-retention-period != null)",
						Require: "(input.backup-retention-period == null || " +
							"input.backup-retention-period <= 35)",
						Message: "backup-retention-period must be at most 35",
					},
					{
						Kind: "predicate",
						When: "(input.backtrack-window != null)",
						Require: "(input.backtrack-window == null || " +
							"input.backtrack-window >= 0) && " +
							"(input.backtrack-window == null || " +
							"input.backtrack-window <= 259200)",
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
						ForEach: "input.enabled-cloudwatch-logs-exports ?? []",
					},
				},
			},
		},
		{
			key: "cluster-instance",
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
					"tags":                         typecheck.TOptional(typecheck.TMap(typecheck.TString())),
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
						When: "(input.performance-insights-retention-period != null)",
						Require: "(input.performance-insights-retention-period == null || " +
							"input.performance-insights-retention-period >= 7) && " +
							"(input.performance-insights-retention-period == null || " +
							"input.performance-insights-retention-period <= 731)",
						Message: "performance-insights-retention-period must be " +
							"between 7 and 731",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		require.Contains(t, schema.Resources, tt.key)
		assertTypeSchemaEqual(t, tt.want, schema.Resources[tt.key],
			"schema mismatch for %s", tt.key)
	}
}
