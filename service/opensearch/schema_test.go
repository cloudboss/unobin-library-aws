package opensearch_test

import (
	"slices"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/opensearch"
)

func TestDomainSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 1)
	require.Contains(t, schema.Resources, "domain")
	domain := schema.Resources["domain"]

	assert.Equal(t, []string{
		"access-policies",
		"advanced-options",
		"advanced-security-options",
		"aiml-options",
		"auto-tune-options",
		"automated-snapshot-pause-options",
		"cluster-config",
		"cognito-options",
		"deployment-strategy-options",
		"domain-endpoint-options",
		"domain-name",
		"ebs-options",
		"encrypt-at-rest",
		"engine-version",
		"identity-center-options",
		"ip-address-type",
		"log-publishing-options",
		"node-to-node-encryption",
		"off-peak-window-options",
		"snapshot-options",
		"software-update-options",
		"tags",
		"vpc-options",
	}, sortedKeys(domain.Inputs))
	assert.Equal(t, typecheck.TString(), domain.Inputs["domain-name"])
	assert.Equal(t, typecheck.TOptional(typecheck.TString()),
		domain.Inputs["access-policies"])
	assert.Equal(t, typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		domain.Inputs["advanced-options"])
	assert.Equal(t, typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		domain.Inputs["tags"])
	assert.Equal(t, []string{"advanced-security-options"}, domain.SensitiveInputs)
	assert.Empty(t, domain.SensitiveOutputs)
	assert.NotContains(t, domain.Inputs, "skip-shard-migration-wait")

	assert.Contains(t, domain.Defaults, lang.DefaultSpec{
		Field: "input.cluster-config.instance-count", Value: "1",
	})
	assert.Contains(t, domain.Defaults, lang.DefaultSpec{
		Field: "input.cluster-config.instance-type", Value: "'m3.medium.search'",
	})
	assert.Contains(t, domain.Defaults, lang.DefaultSpec{
		Field: "input.domain-endpoint-options.enforce-https", Value: "true",
	})
	assert.NotContains(t, domain.Defaults, lang.DefaultSpec{
		Field: "input.log-publishing-options.enabled", Value: "true",
	})
}

func TestDomainOutputs(t *testing.T) {
	domain := readLibrarySchema(t).Resources["domain"]
	assert.Equal(t, []string{
		"anonymous-auth-disable-date",
		"arn",
		"automated-snapshot-pause-options",
		"created",
		"dashboard-endpoint",
		"dashboard-endpoint-v2",
		"deleted",
		"domain-endpoint-v2-hosted-zone-id",
		"domain-id",
		"domain-name",
		"endpoint",
		"endpoint-v2",
		"endpoints",
		"engine-version",
		"identity-center-application-arn",
		"identity-store-id",
		"ip-address-type",
		"processing",
		"service-software-options",
		"upgrade-processing",
		"vpc-options",
	}, sortedKeys(domain.Outputs))
}

func TestDomainNestedInputFields(t *testing.T) {
	domain := readLibrarySchema(t).Resources["domain"]

	identityCenter := domain.Inputs["identity-center-options"].Unwrap()
	assert.Equal(t, []string{
		"enabled-api-access",
		"identity-center-instance-arn",
		"roles-key",
		"subject-key",
	}, sortedObjectFields(identityCenter))

	softwareUpdate := domain.Inputs["software-update-options"].Unwrap()
	assert.Equal(t, []string{
		"auto-software-update-enabled",
	}, sortedObjectFields(softwareUpdate))
}

func TestDomainReplacementFields(t *testing.T) {
	resource := &svc.DomainResource{}
	assert.Equal(t, []string{"domain-name", "vpc-options"}, resource.ReplaceFields())
	assert.NotContains(t, resource.ReplaceFields(), "engine-version")
}

func TestDomainConstraints(t *testing.T) {
	domain := readLibrarySchema(t).Resources["domain"]
	require.NotEmpty(t, domain.Constraints)
	messages := make([]string, 0, len(domain.Constraints))
	for _, spec := range domain.Constraints {
		messages = append(messages, spec.Message)
	}
	for _, message := range []string{
		"ip-address-type must be ipv4 or dualstack",
		"tls-security-policy must be a supported OpenSearch policy",
		"log-type must be a supported OpenSearch log type",
		"automated-snapshot-start-hour must be between 0 and 23",
		"session-timeout-minutes must be between 1 and 1440",
		"warm-count must be between 2 and 150",
		"availability-zone-count must be 2 or 3",
		"volume-type must be standard, gp2, io1, or gp3",
		"auto-tune desired-state must be ENABLED or DISABLED",
	} {
		assert.Contains(t, messages, message)
	}
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
