package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

func TestKeyLookupCloudVerification(t *testing.T) {
	key := &kmstypes.KeyMetadata{
		KeyId: aws.String("key"), Arn: aws.String("arn"), Description: aws.String("updated"),
		KeySpec: "SYMMETRIC_DEFAULT", KeyState: "Enabled", Enabled: true,
	}
	newSnapshot := func() *state.Snapshot {
		snapshot := &state.Snapshot{Outputs: map[string]any{
			"lookup-key-id": "key", "lookup-key-arn": "arn",
			"lookup-key-spec": "SYMMETRIC_DEFAULT", "lookup-key-description": "updated",
			"lookup-key-alias-id": "key", "lookup-key-alias-description": "updated",
			"lookup-key-arn-id": "key", "lookup-key-arn-description": "updated",
		}}
		for _, name := range []string{"kms-key-lookup", "kms-key-alias-lookup", "kms-key-arn-lookup"} {
			snapshot.Entries = append(snapshot.Entries, &state.Entry{
				Address: "data-source." + name,
				Outputs: map[string]any{
					"key-id": "key", "arn": "arn", "description": "updated",
					"key-spec": "SYMMETRIC_DEFAULT", "key-state": "Enabled", "enabled": true,
				},
			})
		}
		return snapshot
	}
	require.NoError(t, verifyKeyLookups(newSnapshot(), key))
	for _, name := range []string{"kms-key-lookup", "kms-key-alias-lookup", "kms-key-arn-lookup"} {
		t.Run(name, func(t *testing.T) {
			snapshot := newSnapshot()
			snapshot.Find("data-source." + name).Outputs["description"] = "old"
			require.ErrorContains(t, verifyKeyLookups(snapshot, key), name+" description")
		})
	}
	snapshot := newSnapshot()
	snapshot.Outputs["lookup-key-alias-description"] = "old"
	require.ErrorContains(t, verifyKeyLookups(snapshot, key), "lookup-key-alias-description")
	snapshot = newSnapshot()
	snapshot.Entries = nil
	require.ErrorContains(t, verifyKeyLookups(snapshot, key), "kms-key-lookup")
}
