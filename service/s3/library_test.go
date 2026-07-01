package s3_test

import (
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/require"

	internal "github.com/cloudboss/unobin-library-aws/internal/service/s3"
	awss3 "github.com/cloudboss/unobin-library-aws/service/s3"
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

func TestLibraryRegistersS3LocalResources(t *testing.T) {
	lib := awss3.Library()
	require.NotNil(t, lib)
	require.Equal(t, "aws-s3", lib.Name)
	require.NotNil(t, lib.Configuration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), lib.Configuration.ValueType())

	require.Equal(t, []string{
		"bucket",
		"bucket-notification",
		"bucket-policy",
		"object",
	}, sortedKeys(lib.Resources))
	require.Empty(t, lib.DataSources)
	require.Empty(t, lib.Actions)

	outputs := map[string]reflect.Type{
		"bucket":              reflect.TypeFor[*internal.BucketOutput](),
		"bucket-notification": reflect.TypeFor[*internal.BucketNotificationOutput](),
		"bucket-policy":       reflect.TypeFor[*internal.BucketPolicyOutput](),
		"object":              reflect.TypeFor[*internal.ObjectOutput](),
	}
	for name, outputType := range outputs {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, outputType, lib.Resources[name].OutputType())
		})
	}
}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(awss3.Library().Configuration)
	require.NoError(t, err)
	require.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	require.NotEmpty(t, view.SchemaDigest)
}

func readServiceSchema(t *testing.T) *runtime.LibrarySchema {
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

func TestReadS3ServiceSchema(t *testing.T) {
	schema := readServiceSchema(t)
	require.Equal(t, []string{
		"bucket",
		"bucket-notification",
		"bucket-policy",
		"object",
	}, sortedKeys(schema.Resources))
	require.Empty(t, schema.DataSources)
	require.Empty(t, schema.Actions)
}

func TestS3Schemas(t *testing.T) {
	schema := readServiceSchema(t)

	cases := map[string]*runtime.TypeSchema{
		"bucket": {
			Inputs: map[string]typecheck.Type{
				"accelerate": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "status", Type: typecheck.TString()},
				})),
				"acl": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "acl", Type: typecheck.TString()},
				})),
				"bucket": typecheck.TString(),
				"cors": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "rules", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "id", Type: typecheck.TString(), Optional: true},
						{Name: "allowed-headers", Type: typecheck.TList(typecheck.TString())},
						{Name: "allowed-methods", Type: typecheck.TList(typecheck.TString())},
						{Name: "allowed-origins", Type: typecheck.TList(typecheck.TString())},
						{Name: "expose-headers", Type: typecheck.TList(typecheck.TString())},
						{Name: "max-age-seconds", Type: typecheck.TInteger(), Optional: true},
					}))},
				})),
				"empty-on-destroy": typecheck.TOptional(typecheck.TBoolean()),
				"encryption": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "sse-algorithm", Type: typecheck.TString()},
					{Name: "kms-master-key-id", Type: typecheck.TString(), Optional: true},
					{Name: "bucket-key-enabled", Type: typecheck.TBoolean(), Optional: true},
				})),
				"lifecycle": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "rules", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "id", Type: typecheck.TString()},
						{Name: "status", Type: typecheck.TString()},
						{Name: "filter", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "prefix", Type: typecheck.TString(), Optional: true},
							{Name: "tag", Type: typecheck.TObject([]typecheck.ObjectField{
								{Name: "key", Type: typecheck.TString()},
								{Name: "value", Type: typecheck.TString()},
							}), Optional: true},
							{Name: "object-size-greater-than", Type: typecheck.TInteger(), Optional: true},
							{Name: "object-size-less-than", Type: typecheck.TInteger(), Optional: true},
							{Name: "and", Type: typecheck.TObject([]typecheck.ObjectField{
								{Name: "prefix", Type: typecheck.TString(), Optional: true},
								{Name: "tags", Type: typecheck.TMap(typecheck.TString())},
								{Name: "object-size-greater-than", Type: typecheck.TInteger(), Optional: true},
								{Name: "object-size-less-than", Type: typecheck.TInteger(), Optional: true},
							}), Optional: true},
						}), Optional: true},
						{Name: "expiration", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "date", Type: typecheck.TString(), Optional: true},
							{Name: "days", Type: typecheck.TInteger(), Optional: true},
							{Name: "expired-object-delete-marker", Type: typecheck.TBoolean(), Optional: true},
						}), Optional: true},
						{Name: "transitions", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "date", Type: typecheck.TString(), Optional: true},
							{Name: "days", Type: typecheck.TInteger(), Optional: true},
							{Name: "storage-class", Type: typecheck.TString()},
						}))},
						{Name: "noncurrent-version-expiration", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "noncurrent-days", Type: typecheck.TInteger(), Optional: true},
							{Name: "newer-noncurrent-versions", Type: typecheck.TInteger(), Optional: true},
						}), Optional: true},
						{
							Name: "noncurrent-version-transitions",
							Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
								{Name: "noncurrent-days", Type: typecheck.TInteger(), Optional: true},
								{Name: "newer-noncurrent-versions", Type: typecheck.TInteger(), Optional: true},
								{Name: "storage-class", Type: typecheck.TString()},
							})),
						},
						{Name: "abort-incomplete-multipart-upload", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "days-after-initiation", Type: typecheck.TInteger(), Optional: true},
						}), Optional: true},
					}))},
				})),
				"logging": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "target-bucket", Type: typecheck.TString()},
					{Name: "target-prefix", Type: typecheck.TString()},
					{Name: "target-grants", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "grantee", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "type", Type: typecheck.TString()},
							{Name: "email-address", Type: typecheck.TString(), Optional: true},
							{Name: "id", Type: typecheck.TString(), Optional: true},
							{Name: "uri", Type: typecheck.TString(), Optional: true},
						}), Optional: true},
						{Name: "permission", Type: typecheck.TString()},
					}))},
					{Name: "target-object-key-format", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "partitioned-prefix", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "partition-date-source", Type: typecheck.TString()},
						}), Optional: true},
						{Name: "simple-prefix", Type: typecheck.TBoolean(), Optional: true},
					}), Optional: true},
				})),
				"object-lock": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "rule", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "default-retention", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "mode", Type: typecheck.TString()},
							{Name: "days", Type: typecheck.TInteger(), Optional: true},
							{Name: "years", Type: typecheck.TInteger(), Optional: true},
						})},
					})},
				})),
				"object-lock-enabled": typecheck.TOptional(typecheck.TBoolean()),
				"ownership-controls": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "object-ownership", Type: typecheck.TString()},
				})),
				"public-access-block": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "block-public-acls", Type: typecheck.TBoolean(), Optional: true},
					{Name: "block-public-policy", Type: typecheck.TBoolean(), Optional: true},
					{Name: "ignore-public-acls", Type: typecheck.TBoolean(), Optional: true},
					{Name: "restrict-public-buckets", Type: typecheck.TBoolean(), Optional: true},
				})),
				"tags": typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"versioning": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "status", Type: typecheck.TString()},
					{Name: "mfa-delete", Type: typecheck.TString(), Optional: true},
				})),
				"website": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "index-document", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "suffix", Type: typecheck.TString()},
					}), Optional: true},
					{Name: "error-document", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "key", Type: typecheck.TString()},
					}), Optional: true},
					{Name: "redirect-all-requests-to", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "host-name", Type: typecheck.TString()},
						{Name: "protocol", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
					{Name: "routing-rules", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "condition", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "http-error-code-returned-equals", Type: typecheck.TString(), Optional: true},
							{Name: "key-prefix-equals", Type: typecheck.TString(), Optional: true},
						}), Optional: true},
						{Name: "redirect", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "host-name", Type: typecheck.TString(), Optional: true},
							{Name: "http-redirect-code", Type: typecheck.TString(), Optional: true},
							{Name: "protocol", Type: typecheck.TString(), Optional: true},
							{Name: "replace-key-prefix-with", Type: typecheck.TString(), Optional: true},
							{Name: "replace-key-with", Type: typecheck.TString(), Optional: true},
						}), Optional: true},
					}))},
				})),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                         typecheck.TString(),
				"bucket-region":               typecheck.TString(),
				"bucket-domain-name":          typecheck.TString(),
				"bucket-regional-domain-name": typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind:    "predicate",
					When:    "(input.accelerate.status != null)",
					Require: "(input.accelerate.status == 'Enabled' || input.accelerate.status == 'Suspended')",
					Message: "accelerate status must be Enabled or Suspended",
				},
				{
					Kind:    "predicate",
					When:    "(input.versioning.status != null)",
					Require: "(input.versioning.status == 'Enabled' || input.versioning.status == 'Suspended')",
					Message: "versioning status must be Enabled or Suspended",
				},
				{
					Kind: "predicate",
					When: "(input.versioning.mfa-delete != null)",
					Require: "(input.versioning.mfa-delete == 'Enabled' || " +
						"input.versioning.mfa-delete == 'Disabled')",
					Message: "versioning mfa-delete must be Enabled or Disabled",
				},
				{
					Kind: "predicate",
					When: "(input.acl.acl != null)",
					Require: "(input.acl.acl == 'private' || input.acl.acl == 'public-read' || " +
						"input.acl.acl == 'public-read-write' || input.acl.acl == 'authenticated-read' || " +
						"input.acl.acl == 'aws-exec-read' || input.acl.acl == 'bucket-owner-read' || " +
						"input.acl.acl == 'bucket-owner-full-control' || input.acl.acl == 'log-delivery-write')",
					Message: "acl must be one of the S3 canned bucket ACLs",
				},
				{
					Kind: "predicate",
					When: "(input.ownership-controls.object-ownership != null)",
					Require: "(input.ownership-controls.object-ownership == 'BucketOwnerPreferred' || " +
						"input.ownership-controls.object-ownership == 'ObjectWriter' || " +
						"input.ownership-controls.object-ownership == 'BucketOwnerEnforced')",
					Message: "object-ownership must be BucketOwnerPreferred, ObjectWriter, or BucketOwnerEnforced",
				},
				{
					Kind: "predicate",
					When: "(input.encryption.sse-algorithm != null)",
					Require: "(input.encryption.sse-algorithm == 'AES256' || " +
						"input.encryption.sse-algorithm == 'aws:kms' || " +
						"input.encryption.sse-algorithm == 'aws:kms:dsse')",
					Message: "sse-algorithm must be AES256, aws:kms, or aws:kms:dsse",
				},
				{
					Kind: "predicate",
					When: "(input.encryption.kms-master-key-id != null)",
					Require: "(input.encryption.sse-algorithm == 'aws:kms' || " +
						"input.encryption.sse-algorithm == 'aws:kms:dsse')",
					Message: "kms-master-key-id requires a KMS sse-algorithm",
				},
				{
					Kind:    "predicate",
					When:    "(input.object-lock != null)",
					Require: "(input.object-lock-enabled == true)",
					Message: "object-lock requires object-lock-enabled to be true",
				},
				{
					Kind: "predicate",
					When: "(input.object-lock.rule.default-retention.mode != null)",
					Require: "(input.object-lock.rule.default-retention.mode == 'GOVERNANCE' || " +
						"input.object-lock.rule.default-retention.mode == 'COMPLIANCE')",
					Message: "object-lock mode must be GOVERNANCE or COMPLIANCE",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.object-lock.rule.default-retention.days",
						"input.object-lock.rule.default-retention.years",
					},
				},
				{
					Kind: "predicate",
					When: "(input.object-lock != null)",
					Require: "((input.object-lock.rule.default-retention.days != null) || " +
						"(input.object-lock.rule.default-retention.years != null))",
					Message: "object-lock retention requires days or years",
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"input.website.redirect-all-requests-to", "input.website.index-document",
						"input.website.error-document", "input.website.routing-rules",
					},
				},
				{
					Kind: "predicate",
					When: "(input.website != null)",
					Require: "((input.website.index-document != null) || " +
						"(input.website.redirect-all-requests-to != null))",
					Message: "website requires index-document or redirect-all-requests-to",
				},
				{
					Kind: "predicate",
					When: "(input.website.redirect-all-requests-to.protocol != null)",
					Require: "(input.website.redirect-all-requests-to.protocol == 'http' || " +
						"input.website.redirect-all-requests-to.protocol == 'https')",
					Message: "redirect-all-requests-to protocol must be http or https",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value.redirect != null)",
					Message: "a routing rule requires a redirect",
					ForEach: "input.website.routing-rules",
				},
				{
					Kind: "predicate",
					When: "(@each.value.redirect.protocol != null)",
					Require: "(@each.value.redirect.protocol == 'http' || " +
						"@each.value.redirect.protocol == 'https')",
					Message: "a routing rule redirect protocol must be http or https",
					ForEach: "input.website.routing-rules",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.website.routing-rules[*].redirect.replace-key-prefix-with",
						"input.website.routing-rules[*].redirect.replace-key-with",
					},
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(input.cors.rules == null || @core.length(input.cors.rules) <= 100)",
					Message: "cors holds at most 100 rules",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@core.length(@each.value.allowed-methods) >= 1) && " +
						"(@core.length(@each.value.allowed-origins) >= 1)",
					Message: "a cors rule requires allowed-methods and allowed-origins",
					ForEach: "input.cors.rules",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@m.value == 'GET' || @m.value == 'PUT' || @m.value == 'POST' || " +
						"@m.value == 'DELETE' || @m.value == 'HEAD')",
					Message: "an allowed method must be GET, PUT, POST, DELETE, or HEAD",
					ForEachLevels: []lang.ForEachSpecLevel{
						{Name: "@rule", In: "input.cors.rules"},
						{Name: "@m", In: "@rule.value.allowed-methods"},
					},
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value.status == 'Enabled' || @each.value.status == 'Disabled')",
					Message: "a lifecycle rule status must be Enabled or Disabled",
					ForEach: "input.lifecycle.rules",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "((@each.value.expiration != null) || " +
						"(@core.length(@each.value.transitions) >= 1) || " +
						"(@each.value.noncurrent-version-expiration != null) || " +
						"(@core.length(@each.value.noncurrent-version-transitions) >= 1) || " +
						"(@each.value.abort-incomplete-multipart-upload != null))",
					Message: "a lifecycle rule needs at least one action",
					ForEach: "input.lifecycle.rules",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.lifecycle.rules[*].filter.prefix",
						"input.lifecycle.rules[*].filter.tag",
						"input.lifecycle.rules[*].filter.object-size-greater-than",
						"input.lifecycle.rules[*].filter.object-size-less-than",
						"input.lifecycle.rules[*].filter.and",
					},
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.lifecycle.rules[*].expiration.date", "input.lifecycle.rules[*].expiration.days",
						"input.lifecycle.rules[*].expiration.expired-object-delete-marker",
					},
				},
				{
					Kind: "predicate",
					When: "(@each.value.expiration != null)",
					Require: "((@each.value.expiration.date != null) || " +
						"(@each.value.expiration.days != null) || " +
						"(@each.value.expiration.expired-object-delete-marker != null))",
					Message: "an expiration needs date, days, or expired-object-delete-marker",
					ForEach: "input.lifecycle.rules",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@tr.value.storage-class == 'GLACIER' || " +
						"@tr.value.storage-class == 'STANDARD_IA' || " +
						"@tr.value.storage-class == 'ONEZONE_IA' || " +
						"@tr.value.storage-class == 'INTELLIGENT_TIERING' || " +
						"@tr.value.storage-class == 'DEEP_ARCHIVE' || " +
						"@tr.value.storage-class == 'GLACIER_IR')",
					Message: "a transition storage-class must be GLACIER, STANDARD_IA, " +
						"ONEZONE_IA, INTELLIGENT_TIERING, DEEP_ARCHIVE, or GLACIER_IR",
					ForEachLevels: []lang.ForEachSpecLevel{
						{Name: "@rule", In: "input.lifecycle.rules"},
						{Name: "@tr", In: "@rule.value.transitions"},
					},
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.lifecycle.rules[*].transitions[*].date",
						"input.lifecycle.rules[*].transitions[*].days",
					},
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "((@tr.value.date != null) || (@tr.value.days != null))",
					Message: "a transition needs date or days",
					ForEachLevels: []lang.ForEachSpecLevel{
						{Name: "@rule", In: "input.lifecycle.rules"},
						{Name: "@tr", In: "@rule.value.transitions"},
					},
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@tr.value.storage-class == 'GLACIER' || " +
						"@tr.value.storage-class == 'STANDARD_IA' || " +
						"@tr.value.storage-class == 'ONEZONE_IA' || " +
						"@tr.value.storage-class == 'INTELLIGENT_TIERING' || " +
						"@tr.value.storage-class == 'DEEP_ARCHIVE' || " +
						"@tr.value.storage-class == 'GLACIER_IR')",
					Message: "a transition storage-class must be GLACIER, STANDARD_IA, " +
						"ONEZONE_IA, INTELLIGENT_TIERING, DEEP_ARCHIVE, or GLACIER_IR",
					ForEachLevels: []lang.ForEachSpecLevel{
						{Name: "@rule", In: "input.lifecycle.rules"},
						{Name: "@tr", In: "@rule.value.noncurrent-version-transitions"},
					},
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.permission == 'FULL_CONTROL' || @each.value.permission == 'READ' || " +
						"@each.value.permission == 'WRITE')",
					Message: "a target grant permission must be FULL_CONTROL, READ, or WRITE",
					ForEach: "input.logging.target-grants",
				},
				{
					Kind: "predicate",
					When: "(@each.value.grantee.type != null)",
					Require: "(@each.value.grantee.type == 'CanonicalUser' || " +
						"@each.value.grantee.type == 'AmazonCustomerByEmail' || " +
						"@each.value.grantee.type == 'Group')",
					Message: "a grantee type must be CanonicalUser, AmazonCustomerByEmail, or Group",
					ForEach: "input.logging.target-grants",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.logging.target-object-key-format.partitioned-prefix",
						"input.logging.target-object-key-format.simple-prefix",
					},
				},
				{
					Kind: "predicate",
					When: "(input.logging.target-object-key-format != null)",
					Require: "((input.logging.target-object-key-format.partitioned-prefix != null) || " +
						"(input.logging.target-object-key-format.simple-prefix != null))",
					Message: "target-object-key-format requires partitioned-prefix or simple-prefix",
				},
				{
					Kind: "predicate",
					When: "(input.logging.target-object-key-format.partitioned-prefix.partition-date-source != " +
						"null)",
					Require: "(input.logging.target-object-key-format.partitioned-prefix.partition-date-source " +
						"== 'EventTime' || " +
						"input.logging.target-object-key-format.partitioned-prefix.partition-date-source == " +
						"'DeliveryTime')",
					Message: "partition-date-source must be EventTime or DeliveryTime",
				},
			},
		},
		"bucket-notification": {
			Inputs: map[string]typecheck.Type{
				"bucket":      typecheck.TString(),
				"eventbridge": typecheck.TOptional(typecheck.TBoolean()),
				"lambda-function": typecheck.TOptional(typecheck.TList(typecheck.TObject(
					[]typecheck.ObjectField{
						{Name: "id", Type: typecheck.TString(), Optional: true},
						{Name: "lambda-function-arn", Type: typecheck.TString(), Optional: true},
						{Name: "events", Type: typecheck.TList(typecheck.TString())},
						{Name: "filter-prefix", Type: typecheck.TString(), Optional: true},
						{Name: "filter-suffix", Type: typecheck.TString(), Optional: true},
					}))),
				"queue": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "id", Type: typecheck.TString(), Optional: true},
					{Name: "queue-arn", Type: typecheck.TString()},
					{Name: "events", Type: typecheck.TList(typecheck.TString())},
					{Name: "filter-prefix", Type: typecheck.TString(), Optional: true},
					{Name: "filter-suffix", Type: typecheck.TString(), Optional: true},
				}))),
				"topic": typecheck.TOptional(typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "id", Type: typecheck.TString(), Optional: true},
					{Name: "topic-arn", Type: typecheck.TString()},
					{Name: "events", Type: typecheck.TList(typecheck.TString())},
					{Name: "filter-prefix", Type: typecheck.TString(), Optional: true},
					{Name: "filter-suffix", Type: typecheck.TString(), Optional: true},
				}))),
			},
			Outputs: map[string]typecheck.Type{
				"bucket":      typecheck.TString(),
				"eventbridge": typecheck.TBoolean(),
				"lambda-function-observed-summaries": typecheck.TList(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "id", Type: typecheck.TString()},
						{Name: "lambda-function-arn", Type: typecheck.TString()},
						{Name: "events", Type: typecheck.TList(typecheck.TString())},
						{Name: "filter-prefix", Type: typecheck.TString()},
						{Name: "filter-suffix", Type: typecheck.TString()},
					})),
				"queue-observed-summaries": typecheck.TList(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "id", Type: typecheck.TString()},
						{Name: "queue-arn", Type: typecheck.TString()},
						{Name: "events", Type: typecheck.TList(typecheck.TString())},
						{Name: "filter-prefix", Type: typecheck.TString()},
						{Name: "filter-suffix", Type: typecheck.TString()},
					})),
				"topic-observed-summaries": typecheck.TList(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "id", Type: typecheck.TString()},
						{Name: "topic-arn", Type: typecheck.TString()},
						{Name: "events", Type: typecheck.TList(typecheck.TString())},
						{Name: "filter-prefix", Type: typecheck.TString()},
						{Name: "filter-suffix", Type: typecheck.TString()},
					})),
			},
		},
		"bucket-policy": {
			Inputs: map[string]typecheck.Type{
				"bucket": typecheck.TString(),
				"policy": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{},
		},
		"object": {
			Inputs: map[string]typecheck.Type{
				"bucket":                        typecheck.TString(),
				"key":                           typecheck.TString(),
				"body-content":                  typecheck.TOptional(typecheck.TString()),
				"body-path":                     typecheck.TOptional(typecheck.TString()),
				"body-base64":                   typecheck.TOptional(typecheck.TString()),
				"acl":                           typecheck.TOptional(typecheck.TString()),
				"bucket-key-enabled":            typecheck.TOptional(typecheck.TBoolean()),
				"cache-control":                 typecheck.TOptional(typecheck.TString()),
				"checksum-algorithm":            typecheck.TOptional(typecheck.TString()),
				"content-disposition":           typecheck.TOptional(typecheck.TString()),
				"content-encoding":              typecheck.TOptional(typecheck.TString()),
				"content-language":              typecheck.TOptional(typecheck.TString()),
				"content-type":                  typecheck.TOptional(typecheck.TString()),
				"kms-key-id":                    typecheck.TOptional(typecheck.TString()),
				"metadata":                      typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"object-lock-legal-hold-status": typecheck.TOptional(typecheck.TString()),
				"object-lock-mode":              typecheck.TOptional(typecheck.TString()),
				"object-lock-retain-until-date": typecheck.TOptional(typecheck.TString()),
				"server-side-encryption":        typecheck.TOptional(typecheck.TString()),
				"storage-class":                 typecheck.TOptional(typecheck.TString()),
				"tags":                          typecheck.TOptional(typecheck.TMap(typecheck.TString())),
				"website-redirect":              typecheck.TOptional(typecheck.TString()),
				"purge-on-destroy":              typecheck.TOptional(typecheck.TBoolean()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":                    typecheck.TString(),
				"bucket-key-enabled":     typecheck.TBoolean(),
				"checksum-crc32":         typecheck.TString(),
				"checksum-crc32c":        typecheck.TString(),
				"checksum-crc64nvme":     typecheck.TString(),
				"checksum-sha1":          typecheck.TString(),
				"checksum-sha256":        typecheck.TString(),
				"content-type":           typecheck.TString(),
				"etag":                   typecheck.TString(),
				"key":                    typecheck.TString(),
				"kms-key-id":             typecheck.TString(),
				"server-side-encryption": typecheck.TString(),
				"storage-class":          typecheck.TString(),
				"version-id":             typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"input.body-content", "input.body-path",
						"input.body-base64",
					},
				},
				{
					Kind: "predicate",
					When: "(input.acl != null)",
					Require: "(input.acl == 'private' || input.acl == 'public-read' || " +
						"input.acl == 'public-read-write' || input.acl == 'authenticated-read' || " +
						"input.acl == 'aws-exec-read' || input.acl == 'bucket-owner-read' || " +
						"input.acl == 'bucket-owner-full-control')",
					Message: "acl must be a valid S3 canned ACL",
				},
				{
					Kind: "predicate",
					When: "(input.checksum-algorithm != null)",
					Require: "(input.checksum-algorithm == 'CRC32' || " +
						"input.checksum-algorithm == 'CRC32C' || " +
						"input.checksum-algorithm == 'SHA1' || input.checksum-algorithm == 'SHA256' || " +
						"input.checksum-algorithm == 'CRC64NVME' || " +
						"input.checksum-algorithm == 'SHA512' || input.checksum-algorithm == 'MD5' || " +
						"input.checksum-algorithm == 'XXHASH64' || " +
						"input.checksum-algorithm == 'XXHASH3' || input.checksum-algorithm == 'XXHASH128')",
					Message: "checksum-algorithm must be a valid S3 checksum algorithm",
				},
				{
					Kind: "predicate",
					When: "(input.server-side-encryption != null)",
					Require: "(input.server-side-encryption == 'AES256' || " +
						"input.server-side-encryption == 'aws:fsx' || " +
						"input.server-side-encryption == 'aws:kms' || " +
						"input.server-side-encryption == 'aws:kms:dsse')",
					Message: "server-side-encryption must be a valid S3 encryption value",
				},
				{
					Kind: "predicate",
					When: "(input.storage-class != null)",
					Require: "(input.storage-class == 'STANDARD' || " +
						"input.storage-class == 'REDUCED_REDUNDANCY' || " +
						"input.storage-class == 'GLACIER' || input.storage-class == 'STANDARD_IA' || " +
						"input.storage-class == 'ONEZONE_IA' || " +
						"input.storage-class == 'INTELLIGENT_TIERING' || " +
						"input.storage-class == 'DEEP_ARCHIVE' || input.storage-class == 'OUTPOSTS' || " +
						"input.storage-class == 'GLACIER_IR' || input.storage-class == 'SNOW' || " +
						"input.storage-class == 'EXPRESS_ONEZONE' || " +
						"input.storage-class == 'FSX_OPENZFS' || input.storage-class == 'FSX_ONTAP')",
					Message: "storage-class must be a valid S3 storage class",
				},
				{
					Kind: "predicate",
					When: "(input.object-lock-mode != null)",
					Require: "(input.object-lock-mode == 'GOVERNANCE' || " +
						"input.object-lock-mode == 'COMPLIANCE')",
					Message: "object-lock-mode must be GOVERNANCE or COMPLIANCE",
				},
				{
					Kind: "predicate",
					When: "(input.object-lock-legal-hold-status != null)",
					Require: "(input.object-lock-legal-hold-status == 'ON' || " +
						"input.object-lock-legal-hold-status == 'OFF')",
					Message: "object-lock-legal-hold-status must be ON or OFF",
				},
			},
		},
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assertTypeSchemaEqual(t, want, schema.Resources[key])
		})
	}
}
