package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
)

func grantVerificationFixture(updated bool) (
	*state.Snapshot, identities, identities, []types.GrantListEntry,
) {
	ids := identities{KeyID: "key", Principal: "arn:aws:iam::123456789012:user/principal",
		Revoked: "revoke", Retired: "retire"}
	old := identities{Revoked: "old-revoke", Retired: "old-retire"}
	stage := "created"
	operations := []types.GrantOperation{"Encrypt"}
	if updated {
		stage = "updated"
		operations = []types.GrantOperation{"Decrypt", "Encrypt"}
	}
	snapshot := &state.Snapshot{}
	var grants []types.GrantListEntry
	for _, item := range []struct{ name, id string }{
		{"revoked", ids.Revoked}, {"retired", ids.Retired},
	} {
		snapshot.Entries = append(snapshot.Entries, &state.Entry{Address: "resource." + item.name,
			Inputs: map[string]any{"key-id": "key-arn", "retiring-principal": "caller"},
			Outputs: map[string]any{"key-id": "key-arn", "grant-id": item.id,
				"grantee-principal": ids.Principal}})
		grant := types.GrantListEntry{GrantId: aws.String(item.id),
			Name: aws.String("unobin-grant-" + item.name), GranteePrincipal: aws.String(ids.Principal),
			Operations: operations, Constraints: &types.GrantConstraints{}}
		if item.name == "retired" {
			grant.RetiringPrincipal = aws.String("caller")
			grant.Constraints.EncryptionContextSubset = map[string]string{"stage": stage}
		} else {
			grant.Constraints.EncryptionContextEquals = map[string]string{"stage": stage}
		}
		grants = append(grants, grant)
	}
	return snapshot, ids, old, grants
}

func TestGrantCloudVerification(t *testing.T) {
	for _, updated := range []bool{false, true} {
		snapshot, ids, old, grants := grantVerificationFixture(updated)
		require.NoError(t, verifyGrants(snapshot, ids, old, grants, updated))
	}
	for _, mutation := range []struct {
		name   string
		change func(*state.Snapshot, *[]types.GrantListEntry)
	}{
		{"missing", func(s *state.Snapshot, _ *[]types.GrantListEntry) { s.Entries = nil }},
		{"wrong state", func(s *state.Snapshot, _ *[]types.GrantListEntry) {
			s.Entries[0].Outputs["key-id"] = "normalized-key"
		}},
		{"missing remote", func(_ *state.Snapshot, g *[]types.GrantListEntry) { *g = (*g)[1:] }},
		{"wrong contents", func(_ *state.Snapshot, g *[]types.GrantListEntry) {
			(*g)[0].Operations = []types.GrantOperation{"Encrypt", "Sign"}
		}},
		{"wrong principal", func(_ *state.Snapshot, g *[]types.GrantListEntry) {
			(*g)[0].GranteePrincipal = aws.String("wrong")
		}},
		{"wrong context", func(_ *state.Snapshot, g *[]types.GrantListEntry) {
			(*g)[1].Constraints.EncryptionContextSubset = map[string]string{"stage": "created"}
		}},
		{"wrong retiring principal", func(_ *state.Snapshot, g *[]types.GrantListEntry) {
			(*g)[1].RetiringPrincipal = aws.String("wrong")
		}},
		{"old grant remains", func(_ *state.Snapshot, g *[]types.GrantListEntry) {
			*g = append(*g, types.GrantListEntry{GrantId: aws.String("old-revoke")})
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			snapshot, ids, old, grants := grantVerificationFixture(true)
			mutation.change(snapshot, &grants)
			require.Error(t, verifyGrants(snapshot, ids, old, grants, true))
		})
	}
}
