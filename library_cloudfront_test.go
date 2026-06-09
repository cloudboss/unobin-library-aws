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
	"github.com/cloudboss/unobin-library-aws/internal/service/cloudfront"
)

// TestLibraryRegistersCloudfront checks the runtime registration: each CloudFront
// resource is present under Resources and dispatches to its output type.
func TestLibraryRegistersCloudfront(t *testing.T) {
	lib := library.Library()
	resources := map[string]reflect.Type{
		"cloudfront-origin-access-control":   reflect.TypeFor[*cloudfront.OriginAccessControlOutput](),
		"cloudfront-function":                reflect.TypeFor[*cloudfront.FunctionOutput](),
		"cloudfront-response-headers-policy": reflect.TypeFor[*cloudfront.ResponseHeadersPolicyOutput](),
		"cloudfront-distribution":            reflect.TypeFor[*cloudfront.DistributionOutput](),
	}
	for key, outputType := range resources {
		t.Run(key, func(t *testing.T) {
			require.Contains(t, lib.Resources, key)
			assert.Equal(t, outputType, lib.Resources[key].OutputType())
		})
	}
}

// TestCloudfrontSchemas asserts the whole derived TypeSchema for each CloudFront
// resource: input and output field types (including the nested origin, cache
// behavior, viewer certificate, and header-config blocks), the cross-field and
// enum constraints, and the optional defaults.
func TestCloudfrontSchemas(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	resources := map[string]*runtime.TypeSchema{
		"cloudfront-origin-access-control": {
			Inputs: map[string]typecheck.Type{
				"description":                       typecheck.TOptional(typecheck.TString()),
				"name":                              typecheck.TString(),
				"origin-access-control-origin-type": typecheck.TString(),
				"signing-behavior":                  typecheck.TString(),
				"signing-protocol":                  typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"etag": typecheck.TString(),
				"id":   typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.origin-access-control-origin-type == 's3' || " +
						"var.origin-access-control-origin-type == 'mediastore' || " +
						"var.origin-access-control-origin-type == 'mediapackagev2' || " +
						"var.origin-access-control-origin-type == 'lambda')",
					Message: "origin-access-control-origin-type must be one of " +
						"s3, mediastore, mediapackagev2, lambda",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.signing-behavior == 'never' || " +
						"var.signing-behavior == 'always' || " +
						"var.signing-behavior == 'no-override')",
					Message: "signing-behavior must be one of never, always, no-override",
				},
				{
					Kind:    "predicate",
					When:    "true",
					Require: "(var.signing-protocol == 'sigv4')",
					Message: "signing-protocol must be sigv4",
				},
			},
		},

		"cloudfront-function": {
			Inputs: map[string]typecheck.Type{
				"code-content":                 typecheck.TOptional(typecheck.TString()),
				"code-path":                    typecheck.TOptional(typecheck.TString()),
				"comment":                      typecheck.TOptional(typecheck.TString()),
				"key-value-store-associations": typecheck.TList(typecheck.TString()),
				"name":                         typecheck.TString(),
				"publish":                      typecheck.TOptional(typecheck.TBoolean()),
				"runtime":                      typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"arn":             typecheck.TString(),
				"etag":            typecheck.TString(),
				"live-stage-etag": typecheck.TString(),
				"status":          typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.runtime == 'cloudfront-js-1.0' || " +
						"var.runtime == 'cloudfront-js-2.0')",
					Message: "runtime must be one of cloudfront-js-1.0, cloudfront-js-2.0",
				},
				{
					Kind:   "exactly-one-of",
					Fields: []string{"var.code-content", "var.code-path"},
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.key-value-store-associations", Optional: true},
			},
		},

		"cloudfront-response-headers-policy": {
			Inputs: map[string]typecheck.Type{
				"comment": typecheck.TOptional(typecheck.TString()),
				"cors-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "access-control-allow-credentials", Type: typecheck.TBoolean(), Optional: true},
					{Name: "access-control-allow-headers", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "items", Type: typecheck.TList(typecheck.TString())},
					})},
					{Name: "access-control-allow-methods", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "items", Type: typecheck.TList(typecheck.TString())},
					})},
					{Name: "access-control-allow-origins", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "items", Type: typecheck.TList(typecheck.TString())},
					})},
					{Name: "access-control-expose-headers", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "items", Type: typecheck.TList(typecheck.TString())},
					}), Optional: true},
					{Name: "access-control-max-age-sec", Type: typecheck.TInteger(), Optional: true},
					{Name: "origin-override", Type: typecheck.TBoolean(), Optional: true},
				})),
				"custom-headers-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "items", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "header", Type: typecheck.TString()},
						{Name: "value", Type: typecheck.TString()},
						{Name: "override", Type: typecheck.TBoolean(), Optional: true},
					}))},
				})),
				"name": typecheck.TString(),
				"remove-headers-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "items", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "header", Type: typecheck.TString()},
					}))},
				})),
				"security-headers-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "content-security-policy", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "content-security-policy", Type: typecheck.TString()},
						{Name: "override", Type: typecheck.TBoolean(), Optional: true},
					}), Optional: true},
					{Name: "content-type-options", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "override", Type: typecheck.TBoolean(), Optional: true},
					}), Optional: true},
					{Name: "frame-options", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "frame-option", Type: typecheck.TString()},
						{Name: "override", Type: typecheck.TBoolean(), Optional: true},
					}), Optional: true},
					{Name: "referrer-policy", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "referrer-policy", Type: typecheck.TString()},
						{Name: "override", Type: typecheck.TBoolean(), Optional: true},
					}), Optional: true},
					{Name: "strict-transport-security", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "access-control-max-age-sec", Type: typecheck.TInteger(), Optional: true},
						{Name: "override", Type: typecheck.TBoolean(), Optional: true},
						{Name: "include-subdomains", Type: typecheck.TBoolean(), Optional: true},
						{Name: "preload", Type: typecheck.TBoolean(), Optional: true},
					}), Optional: true},
					{Name: "xss-protection", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "protection", Type: typecheck.TBoolean(), Optional: true},
						{Name: "override", Type: typecheck.TBoolean(), Optional: true},
						{Name: "mode-block", Type: typecheck.TBoolean(), Optional: true},
						{Name: "report-uri", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
				})),
				"server-timing-headers-config": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "enabled", Type: typecheck.TBoolean(), Optional: true},
					{Name: "sampling-rate", Type: typecheck.TNumber(), Optional: true},
				})),
			},
			Outputs: map[string]typecheck.Type{
				"etag": typecheck.TString(),
				"id":   typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "at-least-one-of",
					Fields: []string{
						"var.cors-config", "var.custom-headers-config",
						"var.remove-headers-config", "var.security-headers-config",
						"var.server-timing-headers-config",
					},
				},
				{
					Kind: "predicate",
					When: "(var.security-headers-config.frame-options != null)",
					Require: "(var.security-headers-config.frame-options.frame-option == 'DENY' || " +
						"var.security-headers-config.frame-options.frame-option == 'SAMEORIGIN')",
					Message: "security-headers-config frame-options frame-option " +
						"must be DENY or SAMEORIGIN",
				},
				{
					Kind: "predicate",
					When: "(var.security-headers-config.referrer-policy != null)",
					Require: "(var.security-headers-config.referrer-policy.referrer-policy == " +
						"'no-referrer' || " +
						"var.security-headers-config.referrer-policy.referrer-policy == " +
						"'no-referrer-when-downgrade' || " +
						"var.security-headers-config.referrer-policy.referrer-policy == " +
						"'origin' || " +
						"var.security-headers-config.referrer-policy.referrer-policy == " +
						"'origin-when-cross-origin' || " +
						"var.security-headers-config.referrer-policy.referrer-policy == " +
						"'same-origin' || " +
						"var.security-headers-config.referrer-policy.referrer-policy == " +
						"'strict-origin' || " +
						"var.security-headers-config.referrer-policy.referrer-policy == " +
						"'strict-origin-when-cross-origin' || " +
						"var.security-headers-config.referrer-policy.referrer-policy == " +
						"'unsafe-url')",
					Message: "security-headers-config referrer-policy referrer-policy must be " +
						"one of no-referrer, no-referrer-when-downgrade, origin, " +
						"origin-when-cross-origin, same-origin, strict-origin, " +
						"strict-origin-when-cross-origin, unsafe-url",
				},
				{
					Kind: "predicate",
					When: "(var.server-timing-headers-config.sampling-rate != null)",
					Require: "(var.server-timing-headers-config.sampling-rate == null || " +
						"var.server-timing-headers-config.sampling-rate >= 0.0) && " +
						"(var.server-timing-headers-config.sampling-rate == null || " +
						"var.server-timing-headers-config.sampling-rate <= 100.0)",
					Message: "server-timing-headers-config sampling-rate must be between 0 and 100",
				},
			},
		},

		"cloudfront-distribution": {
			Inputs: map[string]typecheck.Type{
				"aliases": typecheck.TList(typecheck.TString()),
				"cache-behaviors": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "path-pattern", Type: typecheck.TString()},
					{Name: "target-origin-id", Type: typecheck.TString()},
					{Name: "viewer-protocol-policy", Type: typecheck.TString()},
					{Name: "cache-policy-id", Type: typecheck.TString()},
					{Name: "allowed-methods", Type: typecheck.TList(typecheck.TString())},
					{Name: "cached-methods", Type: typecheck.TList(typecheck.TString())},
					{Name: "compress", Type: typecheck.TBoolean(), Optional: true},
					{Name: "origin-request-policy-id", Type: typecheck.TString(), Optional: true},
					{Name: "response-headers-policy-id", Type: typecheck.TString(), Optional: true},
					{
						Name: "function-associations",
						Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "event-type", Type: typecheck.TString()},
							{Name: "function-arn", Type: typecheck.TString()},
						})),
					},
					{
						Name: "lambda-function-associations",
						Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "event-type", Type: typecheck.TString()},
							{Name: "lambda-arn", Type: typecheck.TString()},
							{Name: "include-body", Type: typecheck.TBoolean(), Optional: true},
						})),
					},
				})),
				"comment": typecheck.TOptional(typecheck.TString()),
				"custom-error-responses": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "error-code", Type: typecheck.TInteger(), Optional: true},
					{Name: "response-code", Type: typecheck.TString(), Optional: true},
					{Name: "response-page-path", Type: typecheck.TString(), Optional: true},
					{Name: "error-caching-min-ttl", Type: typecheck.TInteger(), Optional: true},
				})),
				"default-cache-behavior": typecheck.TObject([]typecheck.ObjectField{
					{Name: "target-origin-id", Type: typecheck.TString()},
					{Name: "viewer-protocol-policy", Type: typecheck.TString()},
					{Name: "cache-policy-id", Type: typecheck.TString()},
					{Name: "allowed-methods", Type: typecheck.TList(typecheck.TString())},
					{Name: "cached-methods", Type: typecheck.TList(typecheck.TString())},
					{Name: "compress", Type: typecheck.TBoolean(), Optional: true},
					{Name: "origin-request-policy-id", Type: typecheck.TString(), Optional: true},
					{Name: "response-headers-policy-id", Type: typecheck.TString(), Optional: true},
					{
						Name: "function-associations",
						Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "event-type", Type: typecheck.TString()},
							{Name: "function-arn", Type: typecheck.TString()},
						})),
					},
					{
						Name: "lambda-function-associations",
						Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
							{Name: "event-type", Type: typecheck.TString()},
							{Name: "lambda-arn", Type: typecheck.TString()},
							{Name: "include-body", Type: typecheck.TBoolean(), Optional: true},
						})),
					},
				}),
				"default-root-object": typecheck.TOptional(typecheck.TString()),
				"enabled":             typecheck.TOptional(typecheck.TBoolean()),
				"http-version":        typecheck.TOptional(typecheck.TString()),
				"is-ipv6-enabled":     typecheck.TOptional(typecheck.TBoolean()),
				"logging": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "bucket", Type: typecheck.TString()},
					{Name: "prefix", Type: typecheck.TString(), Optional: true},
					{Name: "include-cookies", Type: typecheck.TBoolean(), Optional: true},
				})),
				"origins": typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
					{Name: "domain-name", Type: typecheck.TString()},
					{Name: "origin-id", Type: typecheck.TString()},
					{Name: "origin-path", Type: typecheck.TString(), Optional: true},
					{Name: "origin-access-control-id", Type: typecheck.TString(), Optional: true},
					{Name: "custom-headers", Type: typecheck.TList(typecheck.TObject([]typecheck.ObjectField{
						{Name: "header-name", Type: typecheck.TString()},
						{Name: "header-value", Type: typecheck.TString()},
					}))},
					{Name: "s3-origin-config", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "origin-access-identity", Type: typecheck.TString(), Optional: true},
					}), Optional: true},
					{Name: "custom-origin-config", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "http-port", Type: typecheck.TInteger(), Optional: true},
						{Name: "https-port", Type: typecheck.TInteger(), Optional: true},
						{Name: "origin-protocol-policy", Type: typecheck.TString(), Optional: true},
						{Name: "origin-ssl-protocols", Type: typecheck.TList(typecheck.TString())},
						{Name: "origin-read-timeout", Type: typecheck.TInteger(), Optional: true},
						{Name: "origin-keepalive-timeout", Type: typecheck.TInteger(), Optional: true},
					}), Optional: true},
				})),
				"price-class": typecheck.TOptional(typecheck.TString()),
				"restrictions": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "geo-restriction", Type: typecheck.TObject([]typecheck.ObjectField{
						{Name: "restriction-type", Type: typecheck.TString(), Optional: true},
						{Name: "locations", Type: typecheck.TList(typecheck.TString())},
					}), Optional: true},
				})),
				"tags": typecheck.TMap(typecheck.TString()),
				"viewer-certificate": typecheck.TOptional(typecheck.TObject([]typecheck.ObjectField{
					{Name: "cloudfront-default-certificate", Type: typecheck.TBoolean(), Optional: true},
					{Name: "acm-certificate-arn", Type: typecheck.TString(), Optional: true},
					{Name: "iam-certificate-id", Type: typecheck.TString(), Optional: true},
					{Name: "minimum-protocol-version", Type: typecheck.TString(), Optional: true},
					{Name: "ssl-support-method", Type: typecheck.TString(), Optional: true},
				})),
				"web-acl-id": typecheck.TOptional(typecheck.TString()),
			},
			Outputs: map[string]typecheck.Type{
				"arn":              typecheck.TString(),
				"caller-reference": typecheck.TString(),
				"domain-name":      typecheck.TString(),
				"etag":             typecheck.TString(),
				"hosted-zone-id":   typecheck.TString(),
				"id":               typecheck.TString(),
				"status":           typecheck.TString(),
			},
			Constraints: []lang.ConstraintSpec{
				{
					Kind: "predicate",
					When: "(var.price-class != null)",
					Require: "(var.price-class == 'PriceClass_100' || " +
						"var.price-class == 'PriceClass_200' || " +
						"var.price-class == 'PriceClass_All')",
					Message: "price-class must be PriceClass_100, PriceClass_200, or PriceClass_All",
				},
				{
					Kind: "predicate",
					When: "(var.http-version != null)",
					Require: "(var.http-version == 'http1.1' || " +
						"var.http-version == 'http2' || " +
						"var.http-version == 'http2and3' || " +
						"var.http-version == 'http3')",
					Message: "http-version must be http1.1, http2, http2and3, or http3",
				},
				{
					Kind: "predicate",
					When: "(var.viewer-certificate != null)",
					Require: "(((var.viewer-certificate.cloudfront-default-certificate == true) && " +
						"(var.viewer-certificate.acm-certificate-arn == null) && " +
						"(var.viewer-certificate.iam-certificate-id == null)) || " +
						"((var.viewer-certificate.acm-certificate-arn != null) && " +
						"(var.viewer-certificate.iam-certificate-id == null) && " +
						"!(var.viewer-certificate.cloudfront-default-certificate == true)) || " +
						"((var.viewer-certificate.iam-certificate-id != null) && " +
						"(var.viewer-certificate.acm-certificate-arn == null) && " +
						"!(var.viewer-certificate.cloudfront-default-certificate == true)))",
					Message: "viewer-certificate must set exactly one of " +
						"cloudfront-default-certificate true, acm-certificate-arn, " +
						"or iam-certificate-id",
				},
				{
					Kind: "predicate",
					When: "(var.viewer-certificate.minimum-protocol-version != null)",
					Require: "(var.viewer-certificate.minimum-protocol-version == 'SSLv3' || " +
						"var.viewer-certificate.minimum-protocol-version == 'TLSv1' || " +
						"var.viewer-certificate.minimum-protocol-version == 'TLSv1_2016' || " +
						"var.viewer-certificate.minimum-protocol-version == 'TLSv1.1_2016' || " +
						"var.viewer-certificate.minimum-protocol-version == 'TLSv1.2_2018' || " +
						"var.viewer-certificate.minimum-protocol-version == 'TLSv1.2_2019' || " +
						"var.viewer-certificate.minimum-protocol-version == 'TLSv1.2_2021' || " +
						"var.viewer-certificate.minimum-protocol-version == 'TLSv1.3_2025' || " +
						"var.viewer-certificate.minimum-protocol-version == 'TLSv1.2_2025')",
					Message: "viewer-certificate minimum-protocol-version must be one of " +
						"SSLv3, TLSv1, TLSv1_2016, TLSv1.1_2016, TLSv1.2_2018, " +
						"TLSv1.2_2019, TLSv1.2_2021, TLSv1.3_2025, TLSv1.2_2025",
				},
				{
					Kind: "predicate",
					When: "(var.viewer-certificate.ssl-support-method != null)",
					Require: "(var.viewer-certificate.ssl-support-method == 'sni-only' || " +
						"var.viewer-certificate.ssl-support-method == 'vip' || " +
						"var.viewer-certificate.ssl-support-method == 'static-ip')",
					Message: "viewer-certificate ssl-support-method must be sni-only, vip, or static-ip",
				},
				{
					Kind: "predicate",
					When: "(var.restrictions.geo-restriction.restriction-type != null)",
					Require: "(var.restrictions.geo-restriction.restriction-type == 'none' || " +
						"var.restrictions.geo-restriction.restriction-type == 'whitelist' || " +
						"var.restrictions.geo-restriction.restriction-type == 'blacklist')",
					Message: "restrictions geo-restriction restriction-type must be " +
						"none, whitelist, or blacklist",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(var.default-cache-behavior.viewer-protocol-policy == 'allow-all' || " +
						"var.default-cache-behavior.viewer-protocol-policy == 'https-only' || " +
						"var.default-cache-behavior.viewer-protocol-policy == 'redirect-to-https')",
					Message: "default-cache-behavior viewer-protocol-policy must be " +
						"allow-all, https-only, or redirect-to-https",
				},
				{
					Kind: "predicate",
					When: "true",
					Require: "(@each.value.viewer-protocol-policy == 'allow-all' || " +
						"@each.value.viewer-protocol-policy == 'https-only' || " +
						"@each.value.viewer-protocol-policy == 'redirect-to-https')",
					Message: "cache-behaviors viewer-protocol-policy must be " +
						"allow-all, https-only, or redirect-to-https",
					ForEach: "var.cache-behaviors",
				},
				{
					Kind: "at-most-one-of",
					Fields: []string{
						"var.origins[*].s3-origin-config",
						"var.origins[*].custom-origin-config",
					},
				},
				{
					Kind: "predicate",
					When: "(@each.value.custom-origin-config.origin-protocol-policy != null)",
					Require: "(@each.value.custom-origin-config.origin-protocol-policy == 'http-only' || " +
						"@each.value.custom-origin-config.origin-protocol-policy == 'https-only' || " +
						"@each.value.custom-origin-config.origin-protocol-policy == 'match-viewer')",
					Message: "custom-origin-config origin-protocol-policy must be " +
						"http-only, https-only, or match-viewer",
					ForEach: "var.origins",
				},
			},
			Defaults: []lang.DefaultSpec{
				{Field: "var.aliases", Optional: true},
				{Field: "var.cache-behaviors", Optional: true},
				{Field: "var.custom-error-responses", Optional: true},
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
