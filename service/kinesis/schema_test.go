package kinesis_test

import (
	"slices"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/kinesis"
)

func TestStreamSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Len(t, schema.Resources, 1)
	stream := schema.Resources["stream"]
	assert.Equal(t, []string{
		"encryption-type",
		"enforce-consumer-deletion",
		"kms-key-id",
		"max-record-size-in-kib",
		"name",
		"retention-period",
		"shard-count",
		"shard-level-metrics",
		"stream-mode-details",
		"tags",
		"warm-throughput-mib-ps",
	}, sortedKeys(stream.Inputs))
	assert.Equal(t, typecheck.TString(), stream.Inputs["name"])
	assert.Equal(
		t,
		typecheck.TOptional(typecheck.TList(typecheck.TString())),
		stream.Inputs["shard-level-metrics"],
	)
	assert.Equal(
		t,
		typecheck.TOptional(typecheck.TMap(typecheck.TString())),
		stream.Inputs["tags"],
	)
	assert.Empty(t, stream.SensitiveInputs)
	assert.Empty(t, stream.SensitiveOutputs)
	assert.Contains(t, stream.Defaults, lang.DefaultSpec{
		Field: "input.encryption-type", Value: "'NONE'",
	})
	assert.Contains(t, stream.Defaults, lang.DefaultSpec{
		Field: "input.enforce-consumer-deletion", Value: "false",
	})
	assert.Contains(t, stream.Defaults, lang.DefaultSpec{
		Field: "input.retention-period", Value: "24",
	})
}

func TestStreamOutputsAndReplacement(t *testing.T) {
	stream := readLibrarySchema(t).Resources["stream"]
	assert.Equal(t, []string{
		"arn",
		"consumer-count",
		"max-record-size-in-kib",
		"name",
		"open-shard-count",
		"stream-creation-timestamp",
		"stream-mode-details",
		"stream-status",
		"warm-throughput",
	}, sortedKeys(stream.Outputs))
	assert.Equal(t, []string{"name"}, (&svc.StreamResource{}).ReplaceFields())
}

func TestStreamConstraints(t *testing.T) {
	constraints := readLibrarySchema(t).Resources["stream"].Constraints
	require.NotEmpty(t, constraints)
	messages := make([]string, 0, len(constraints))
	for _, value := range constraints {
		messages = append(messages, value.Message)
	}
	for _, message := range []string{
		"encryption-type must be NONE or KMS",
		"KMS encryption requires a non-empty kms-key-id",
		"retention-period must be between 24 and 8760",
		"max-record-size-in-kib must be between 1024 and 10240",
		"PROVISIONED mode requires shard-count",
		"ON_DEMAND mode forbids shard-count",
		"warm-throughput-mib-ps conflicts with shard-count",
		"shard-level-metrics contains an unsupported metric",
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
