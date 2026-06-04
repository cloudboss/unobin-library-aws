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
	"github.com/cloudboss/unobin-library-aws/internal/service/s3"
)

// TestLibraryRegistersS3Resources checks the runtime registration: every S3
// resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersS3Resources(t *testing.T) {
	lib := library.Library()
	cases := map[string]reflect.Type{
		"s3-bucket":        reflect.TypeFor[*s3.BucketOutput](),
		"s3-bucket-policy": reflect.TypeFor[*s3.BucketPolicyOutput](),
		"s3-object":        reflect.TypeFor[*s3.ObjectOutput](),
	}
	for key, outputType := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestS3Schemas checks what the dev CLI reads from this library's source for
// each S3 resource: the input and output field types, that nothing is marked
// sensitive, and the cross-field constraints derived from each Constraints
// method. The whole TypeSchema is asserted so a stray field or tag is caught.
func TestS3Schemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	cases := map[string]*runtime.TypeSchema{
		"s3-bucket": {
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
						{Name: "allowed-headers", Type: typecheck.TList(typecheck.TString())},
						{Name: "allowed-methods", Type: typecheck.TList(typecheck.TString())},
						{Name: "allowed-origins", Type: typecheck.TList(typecheck.TString())},
						{Name: "expose-headers", Type: typecheck.TList(typecheck.TString())},
						{Name: "id", Type: typecheck.TString(), Optional: true},
						{Name: "max-age-seconds", Type: typecheck.TInteger(), Optional: true},
					}))},
				})),
				"empty-on-destroy": typecheck.TOptional(typecheck.TBoolean()),
				"encryption": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "bucket-key-enabled", Type: typecheck.TBoolean(), Optional: true},
					{Name: "kms-master-key-id", Type: typecheck.TString(), Optional: true},
					{Name: "sse-algorithm", Type: typecheck.TString()},
				})),
				"lifecycle": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "rules", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "abort-incomplete-multipart-upload", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "days-after-initiation", Type: typecheck.TInteger(), Optional: true},
						}), Optional: true},
						{Name: "expiration", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "date", Type: typecheck.TString(), Optional: true},
							{Name: "days", Type: typecheck.TInteger(), Optional: true},
							{Name: "expired-object-delete-marker", Type: typecheck.TBoolean(), Optional: true},
						}), Optional: true},
						{Name: "filter", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "and", Type: typecheck.TObject([]typecheck.ObjectField{
								{Name: "object-size-greater-than", Type: typecheck.TInteger(), Optional: true},
								{Name: "object-size-less-than", Type: typecheck.TInteger(), Optional: true},
								{Name: "prefix", Type: typecheck.TString(), Optional: true},
								{Name: "tags", Type: typecheck.TMap(typecheck.TString())},
							}), Optional: true},
							{Name: "object-size-greater-than", Type: typecheck.TInteger(), Optional: true},
							{Name: "object-size-less-than", Type: typecheck.TInteger(), Optional: true},
							{Name: "prefix", Type: typecheck.TString(), Optional: true},
							{Name: "tag", Type: typecheck.TObject([]typecheck.ObjectField{
								{Name: "key", Type: typecheck.TString()},
								{Name: "value", Type: typecheck.TString()},
							}), Optional: true},
						}), Optional: true},
						{Name: "id", Type: typecheck.TString()},
						{Name: "noncurrent-version-expiration", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "newer-noncurrent-versions", Type: typecheck.TInteger(), Optional: true},
							{Name: "noncurrent-days", Type: typecheck.TInteger(), Optional: true},
						}), Optional: true},
						{
							Name: "noncurrent-version-transitions",
							Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
								{Name: "newer-noncurrent-versions", Type: typecheck.TInteger(), Optional: true},
								{Name: "noncurrent-days", Type: typecheck.TInteger(), Optional: true},
								{Name: "storage-class", Type: typecheck.TString()},
							})),
						},
						{Name: "status", Type: typecheck.TString()},
						{Name: "transitions", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "date", Type: typecheck.TString(), Optional: true},
							{Name: "days", Type: typecheck.TInteger(), Optional: true},
							{Name: "storage-class", Type: typecheck.TString()},
						}))},
					}))},
				})),
				"logging": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "target-bucket", Type: typecheck.TString()},
					{Name: "target-grants", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "grantee", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "email-address", Type: typecheck.TString(), Optional: true},
							{Name: "id", Type: typecheck.TString(), Optional: true},
							{Name: "type", Type: typecheck.TString()},
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
					{Name: "target-prefix", Type: typecheck.TString()},
				})),
				"object-lock": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "rule", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "default-retention", Type: typecheck.TObject([]typecheck.ObjectField{
							{Name: "days", Type: typecheck.TInteger(), Optional: true},
							{Name: "mode", Type: typecheck.TString()},
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
				"tags": typecheck.TMap(typecheck.TString()),
				"versioning": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "mfa-delete", Type: typecheck.TString(), Optional: true},
					{Name: "status", Type: typecheck.TString()},
				})),
				"website": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "error-document", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "key", Type: typecheck.TString()},
					}), Optional: true},
					{Name: "index-document", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "suffix", Type: typecheck.TString()},
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
					When:    "(var.accelerate.status != null)",
					Require: "(var.accelerate.status == 'Enabled' || var.accelerate.status == 'Suspended')",
					Message: "accelerate status must be Enabled or Suspended",
				},
				{
					Kind:    "predicate",
					When:    "(var.versioning.status != null)",
					Require: "(var.versioning.status == 'Enabled' || var.versioning.status == 'Suspended')",
					Message: "versioning status must be Enabled or Suspended",
				},
				{
					Kind:    "predicate",
					When:    "(var.versioning.mfa-delete != null)",
					Require: "(var.versioning.mfa-delete == 'Enabled' || var.versioning.mfa-delete == 'Disabled')",
					Message: "versioning mfa-delete must be Enabled or Disabled",
				},
				{
					Kind: "predicate",
					When: "(var.acl.acl != null)",
					Require: "(var.acl.acl == 'private' || var.acl.acl == 'public-read' || " +
						"var.acl.acl == 'public-read-write' || var.acl.acl == 'authenticated-read' || " +
						"var.acl.acl == 'aws-exec-read' || var.acl.acl == 'bucket-owner-read' || " +
						"var.acl.acl == 'bucket-owner-full-control' || var.acl.acl == 'log-delivery-write')",
					Message: "acl must be one of the S3 canned bucket ACLs",
				},
				{
					Kind: "predicate",
					When: "(var.ownership-controls.object-ownership != null)",
					Require: "(var.ownership-controls.object-ownership == 'BucketOwnerPreferred' || " +
						"var.ownership-controls.object-ownership == 'ObjectWriter' || " +
						"var.ownership-controls.object-ownership == 'BucketOwnerEnforced')",
					Message: "object-ownership must be BucketOwnerPreferred, ObjectWriter, or BucketOwnerEnforced",
				},
				{
					Kind: "predicate",
					When: "(var.encryption.sse-algorithm != null)",
					Require: "(var.encryption.sse-algorithm == 'AES256' || " +
						"var.encryption.sse-algorithm == 'aws:kms' || " +
						"var.encryption.sse-algorithm == 'aws:kms:dsse')",
					Message: "sse-algorithm must be AES256, aws:kms, or aws:kms:dsse",
				},
				{
					Kind: "predicate",
					When: "(var.encryption.kms-master-key-id != null)",
					Require: "(var.encryption.sse-algorithm == 'aws:kms' || " +
						"var.encryption.sse-algorithm == 'aws:kms:dsse')",
					Message: "kms-master-key-id requires a KMS sse-algorithm",
				},
				{
					Kind:    "predicate",
					When:    "(var.object-lock != null)",
					Require: "(var.object-lock-enabled == true)",
					Message: "object-lock requires object-lock-enabled to be true",
				},
				{
					Kind: "predicate",
					When: "(var.object-lock.rule.default-retention.mode != null)",
					Require: "(var.object-lock.rule.default-retention.mode == 'GOVERNANCE' || " +
						"var.object-lock.rule.default-retention.mode == 'COMPLIANCE')",
					Message: "object-lock mode must be GOVERNANCE or COMPLIANCE",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.object-lock.rule.default-retention.days", "var.object-lock.rule.default-retention.years",
					},
				},
				{
					Kind: "predicate",
					When: "(var.object-lock != null)",
					Require: "((var.object-lock.rule.default-retention.days != null) || " +
						"(var.object-lock.rule.default-retention.years != null))",
					Message: "object-lock retention requires days or years",
				},
				{
					Kind: "forbidden-with",
					Fields: []string{
						"var.website.redirect-all-requests-to", "var.website.index-document",
						"var.website.error-document", "var.website.routing-rules",
					},
				},
				{
					Kind: "predicate",
					When: "(var.website != null)",
					Require: "((var.website.index-document != null) || " +
						"(var.website.redirect-all-requests-to != null))",
					Message: "website requires index-document or redirect-all-requests-to",
				},
				{
					Kind: "predicate",
					When: "(var.website.redirect-all-requests-to.protocol != null)",
					Require: "(var.website.redirect-all-requests-to.protocol == 'http' || " +
						"var.website.redirect-all-requests-to.protocol == 'https')",
					Message: "redirect-all-requests-to protocol must be http or https",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value.redirect != null)",
					Message: "a routing rule requires a redirect",
					ForEach: "var.website.routing-rules",
				},
				{
					Kind: "predicate",
					When: "(@each.value.redirect.protocol != null)",
					Require: "(@each.value.redirect.protocol == 'http' || " +
						"@each.value.redirect.protocol == 'https')",
					Message: "a routing rule redirect protocol must be http or https",
					ForEach: "var.website.routing-rules",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.website.routing-rules[*].redirect.replace-key-prefix-with",
						"var.website.routing-rules[*].redirect.replace-key-with",
					},
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value.allowed-methods != null) && (@each.value.allowed-origins != null)",
					Message: "a cors rule requires allowed-methods and allowed-origins",
					ForEach: "var.cors.rules",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(@each.value.status == 'Enabled' || @each.value.status == 'Disabled')",
					Message: "a lifecycle rule status must be Enabled or Disabled",
					ForEach: "var.lifecycle.rules",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "((@each.value.expiration != null) || (@each.value.transitions != null) || " +
						"(@each.value.noncurrent-version-expiration != null) || " +
						"(@each.value.noncurrent-version-transitions != null) || " +
						"(@each.value.abort-incomplete-multipart-upload != null))",
					Message: "a lifecycle rule needs at least one action",
					ForEach: "var.lifecycle.rules",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.lifecycle.rules[*].filter.prefix", "var.lifecycle.rules[*].filter.tag",
						"var.lifecycle.rules[*].filter.object-size-greater-than",
						"var.lifecycle.rules[*].filter.object-size-less-than", "var.lifecycle.rules[*].filter.and",
					},
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.lifecycle.rules[*].expiration.date", "var.lifecycle.rules[*].expiration.days",
						"var.lifecycle.rules[*].expiration.expired-object-delete-marker",
					},
				},
				{
					Kind: "predicate",
					When: "(@each.value.expiration != null)",
					Require: "((@each.value.expiration.date != null) || " +
						"(@each.value.expiration.days != null) || " +
						"(@each.value.expiration.expired-object-delete-marker != null))",
					Message: "an expiration needs date, days, or expired-object-delete-marker",
					ForEach: "var.lifecycle.rules",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.permission == 'FULL_CONTROL' || @each.value.permission == 'READ' || " +
						"@each.value.permission == 'WRITE')",
					Message: "a target grant permission must be FULL_CONTROL, READ, or WRITE",
					ForEach: "var.logging.target-grants",
				},
				{
					Kind: "predicate",
					When: "(@each.value.grantee.type != null)",
					Require: "(@each.value.grantee.type == 'CanonicalUser' || " +
						"@each.value.grantee.type == 'AmazonCustomerByEmail' || " +
						"@each.value.grantee.type == 'Group')",
					Message: "a grantee type must be CanonicalUser, AmazonCustomerByEmail, or Group",
					ForEach: "var.logging.target-grants",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.logging.target-object-key-format.partitioned-prefix",
						"var.logging.target-object-key-format.simple-prefix",
					},
				},
				{
					Kind: "predicate",
					When: "(var.logging.target-object-key-format != null)",
					Require: "((var.logging.target-object-key-format.partitioned-prefix != null) || " +
						"(var.logging.target-object-key-format.simple-prefix != null))",
					Message: "target-object-key-format requires partitioned-prefix or simple-prefix",
				},
				{
					Kind: "predicate",
					When: "(var.logging.target-object-key-format.partitioned-prefix.partition-date-source != " +
						"null)",
					Require: "(var.logging.target-object-key-format.partitioned-prefix.partition-date-source " +
						"== 'EventTime' || " +
						"var.logging.target-object-key-format.partitioned-prefix.partition-date-source == " +
						"'DeliveryTime')",
					Message: "partition-date-source must be EventTime or DeliveryTime",
				},
			},
		},
		"s3-bucket-policy": {
			Inputs: map[string]typecheck.Type{
				"bucket": typecheck.TString(),
				"policy": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{},
		},
		"s3-object": {
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
				"metadata":                      typecheck.TMap(typecheck.TString()),
				"object-lock-legal-hold-status": typecheck.TOptional(typecheck.TString()),
				"object-lock-mode":              typecheck.TOptional(typecheck.TString()),
				"object-lock-retain-until-date": typecheck.TOptional(typecheck.TString()),
				"server-side-encryption":        typecheck.TOptional(typecheck.TString()),
				"storage-class":                 typecheck.TOptional(typecheck.TString()),
				"tags":                          typecheck.TMap(typecheck.TString()),
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
						"var.body-content", "var.body-path",
						"var.body-base64",
					},
				},
				{
					Kind: "predicate",
					When: "(var.acl != null)",
					Require: "(var.acl == 'private' || var.acl == 'public-read' || " +
						"var.acl == 'public-read-write' || var.acl == 'authenticated-read' || " +
						"var.acl == 'aws-exec-read' || var.acl == 'bucket-owner-read' || " +
						"var.acl == 'bucket-owner-full-control')",
					Message: "acl must be a valid S3 canned ACL",
				},
				{
					Kind: "predicate",
					When: "(var.checksum-algorithm != null)",
					Require: "(var.checksum-algorithm == 'CRC32' || " +
						"var.checksum-algorithm == 'CRC32C' || " +
						"var.checksum-algorithm == 'SHA1' || var.checksum-algorithm == 'SHA256' || " +
						"var.checksum-algorithm == 'CRC64NVME' || " +
						"var.checksum-algorithm == 'SHA512' || var.checksum-algorithm == 'MD5' || " +
						"var.checksum-algorithm == 'XXHASH64' || " +
						"var.checksum-algorithm == 'XXHASH3' || var.checksum-algorithm == 'XXHASH128')",
					Message: "checksum-algorithm must be a valid S3 checksum algorithm",
				},
				{
					Kind: "predicate",
					When: "(var.server-side-encryption != null)",
					Require: "(var.server-side-encryption == 'AES256' || " +
						"var.server-side-encryption == 'aws:fsx' || " +
						"var.server-side-encryption == 'aws:kms' || " +
						"var.server-side-encryption == 'aws:kms:dsse')",
					Message: "server-side-encryption must be a valid S3 encryption value",
				},
				{
					Kind: "predicate",
					When: "(var.storage-class != null)",
					Require: "(var.storage-class == 'STANDARD' || " +
						"var.storage-class == 'REDUCED_REDUNDANCY' || " +
						"var.storage-class == 'GLACIER' || var.storage-class == 'STANDARD_IA' || " +
						"var.storage-class == 'ONEZONE_IA' || " +
						"var.storage-class == 'INTELLIGENT_TIERING' || " +
						"var.storage-class == 'DEEP_ARCHIVE' || var.storage-class == 'OUTPOSTS' || " +
						"var.storage-class == 'GLACIER_IR' || var.storage-class == 'SNOW' || " +
						"var.storage-class == 'EXPRESS_ONEZONE' || " +
						"var.storage-class == 'FSX_OPENZFS' || var.storage-class == 'FSX_ONTAP')",
					Message: "storage-class must be a valid S3 storage class",
				},
				{
					Kind: "predicate",
					When: "(var.object-lock-mode != null)",
					Require: "(var.object-lock-mode == 'GOVERNANCE' || " +
						"var.object-lock-mode == 'COMPLIANCE')",
					Message: "object-lock-mode must be GOVERNANCE or COMPLIANCE",
				},
				{
					Kind: "predicate",
					When: "(var.object-lock-legal-hold-status != null)",
					Require: "(var.object-lock-legal-hold-status == 'ON' || " +
						"var.object-lock-legal-hold-status == 'OFF')",
					Message: "object-lock-legal-hold-status must be ON or OFF",
				},
			},
		},
	}

	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, schema.Resources, key)
			assert.Equal(t, normalizeSchema(want), normalizeSchema(schema.Resources[key]))
		})
	}
}
