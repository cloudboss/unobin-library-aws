package main

import (
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
	"testing"
)

func queueSnapshot() *state.Snapshot {
	return &state.Snapshot{Entries: []*state.Entry{
		{Address: "resource.sqs-queue-it", Inputs: map[string]any{
			"tags": map[string]any{"unobin": "sqs-it", "phase": "updated"}},
			Outputs: map[string]any{"url": "URL", "arn": "ARN"}},
		{Address: "data-source.sqs-queue-lookup", Inputs: map[string]any{"queue-name": queueName},
			Outputs: map[string]any{"queue-url": "URL", "queue-arn": "ARN",
				"tags": map[string]any{"unobin": "sqs-it", "phase": "updated"}}},
	}, Outputs: map[string]any{"lookup-queue-url": "URL", "lookup-queue-arn": "ARN",
		"lookup-tags": map[string]any{"unobin": "sqs-it", "phase": "updated"}}}
}

func TestQueueLookupCloudVerification(t *testing.T) {
	tags := map[string]string{"unobin": "sqs-it", "phase": "updated"}
	require.NoError(t, verifyLookupSnapshot(queueSnapshot(), "URL", "ARN", tags))
	for _, field := range []string{"queue-url", "queue-arn", "tags"} {
		t.Run(field, func(t *testing.T) {
			snapshot := queueSnapshot()
			snapshot.Find("data-source.sqs-queue-lookup").Outputs[field] = "old"
			require.ErrorContains(t, verifyLookupSnapshot(snapshot, "URL", "ARN", tags), field)
		})
	}
	snapshot := queueSnapshot()
	snapshot.Outputs["lookup-tags"] = map[string]any{"unobin": "sqs-it", "phase": "initial"}
	require.ErrorContains(t, verifyLookupSnapshot(snapshot, "URL", "ARN", tags), "tags")
	snapshot = queueSnapshot()
	snapshot.Find("resource.sqs-queue-it").Inputs["tags"] = map[string]any{"phase": "initial"}
	require.ErrorContains(t, verifyLookupSnapshot(snapshot, "URL", "ARN", tags), "tags")
	snapshot = queueSnapshot()
	snapshot.Entries = nil
	require.ErrorContains(t, verifyLookupSnapshot(snapshot, "URL", "ARN", tags), "required")
	snapshot = queueSnapshot()
	snapshot.Find("data-source.sqs-queue-lookup").Inputs["queue-name"] = "changed"
	require.ErrorContains(t, verifyLookupSnapshot(snapshot, "URL", "ARN", tags), "selector changed")
}
