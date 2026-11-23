package kms

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type aliasListResult struct {
	page *kms.ListAliasesOutput
	err  error
}

type aliasLookupStub struct {
	t             *testing.T
	listResults   []aliasListResult
	key           *kms.DescribeKeyOutput
	keyErr        error
	listCalls     []kms.ListAliasesInput
	describeCalls []kms.DescribeKeyInput
	operations    []string
}

func (s *aliasLookupStub) ListAliases(
	ctx context.Context, in *kms.ListAliasesInput, _ ...func(*kms.Options),
) (*kms.ListAliasesOutput, error) {
	assert.Same(s.t, s.t.Context(), ctx)
	index := len(s.listCalls)
	require.Less(s.t, index, len(s.listResults), "unexpected ListAliases request")
	s.operations = append(s.operations, "ListAliases")
	s.listCalls = append(s.listCalls, *in)
	result := s.listResults[index]
	return result.page, result.err
}

func (s *aliasLookupStub) DescribeKey(
	ctx context.Context, in *kms.DescribeKeyInput, _ ...func(*kms.Options),
) (*kms.DescribeKeyOutput, error) {
	assert.Same(s.t, s.t.Context(), ctx)
	s.operations = append(s.operations, "DescribeKey")
	s.describeCalls = append(s.describeCalls, *in)
	return s.key, s.keyErr
}

func TestAliasLookupPages(t *testing.T) {
	const name = "alias/MiXeD-1"
	const aliasARN = "arn:aws-us-gov:kms:us-gov-west-1:123456789012:alias/MiXeD-1"
	const keyID = "mrk-123456789abcdef"
	const keyARN = "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/" + keyID
	match := kmstypes.AliasListEntry{
		AliasName: aws.String(name), AliasArn: aws.String(aliasARN),
		TargetKeyId: aws.String("stale-id"),
	}
	decoy := kmstypes.AliasListEntry{AliasName: aws.String("alias/mixed-1")}
	listErr := errors.New("list service failed")
	tests := []struct {
		name          string
		pages         []aliasListResult
		listCalls     []kms.ListAliasesInput
		describeCalls []kms.DescribeKeyInput
		operations    []string
		want          *AliasDataSourceOutput
		wantError     string
		wantCause     error
	}{
		{
			name: "later exact match and exhausted pages",
			pages: []aliasListResult{
				{page: &kms.ListAliasesOutput{
					Aliases: []kmstypes.AliasListEntry{decoy}, NextMarker: aws.String("next"),
				}},
				{page: &kms.ListAliasesOutput{
					Aliases: []kmstypes.AliasListEntry{match}, NextMarker: aws.String("last"),
				}},
				{page: &kms.ListAliasesOutput{Aliases: []kmstypes.AliasListEntry{decoy}}},
			},
			listCalls: []kms.ListAliasesInput{
				{}, {Marker: aws.String("next")}, {Marker: aws.String("last")},
			},
			describeCalls: []kms.DescribeKeyInput{{KeyId: aws.String(name)}},
			operations:    []string{"ListAliases", "ListAliases", "ListAliases", "DescribeKey"},
			want: &AliasDataSourceOutput{
				AliasName: name, Arn: aliasARN, TargetKeyArn: keyARN, TargetKeyID: keyID,
			},
		},
		{
			name: "missing exact match",
			pages: []aliasListResult{
				{page: &kms.ListAliasesOutput{Aliases: []kmstypes.AliasListEntry{decoy}}},
			},
			listCalls:  []kms.ListAliasesInput{{}},
			operations: []string{"ListAliases"},
			wantError:  `list aliases for "alias/MiXeD-1": no exact match`,
		},
		{
			name: "duplicate matches across pages",
			pages: []aliasListResult{
				{page: &kms.ListAliasesOutput{
					Aliases: []kmstypes.AliasListEntry{match}, NextMarker: aws.String("next"),
				}},
				{page: &kms.ListAliasesOutput{Aliases: []kmstypes.AliasListEntry{match}}},
			},
			listCalls:  []kms.ListAliasesInput{{}, {Marker: aws.String("next")}},
			operations: []string{"ListAliases", "ListAliases"},
			wantError:  `list aliases for "alias/MiXeD-1": multiple exact matches`,
		},
		{
			name: "later list error takes precedence over duplicates",
			pages: []aliasListResult{
				{page: &kms.ListAliasesOutput{
					Aliases:    []kmstypes.AliasListEntry{match, match},
					NextMarker: aws.String("next"),
				}},
				{err: listErr},
			},
			listCalls:  []kms.ListAliasesInput{{}, {Marker: aws.String("next")}},
			operations: []string{"ListAliases", "ListAliases"},
			wantError:  `list aliases for "alias/MiXeD-1": list service failed`,
			wantCause:  listErr,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &aliasLookupStub{
				t: t, listResults: tc.pages,
				key: &kms.DescribeKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{
					KeyId: aws.String(keyID), Arn: aws.String(keyARN),
					KeyState: kmstypes.KeyStateEnabled,
				}},
			}
			query := AliasDataSource{AliasName: name}
			got, err := query.read(t.Context(), client)
			if tc.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantError)
			}
			if tc.wantCause != nil {
				require.ErrorIs(t, err, tc.wantCause)
			}
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.listCalls, client.listCalls)
			assert.Equal(t, tc.describeCalls, client.describeCalls)
			assert.Equal(t, tc.operations, client.operations)
		})
	}
}

func TestAliasLookupDescribesAWSManagedKey(t *testing.T) {
	const name = "alias/aws/SomeService"
	const aliasARN = "arn:aws:kms:us-east-1:123456789012:alias/aws/SomeService"
	const keyID = "a-key-id"
	const keyARN = "arn:aws:kms:us-east-1:123456789012:key/" + keyID
	t.Run("unlisted alias", func(t *testing.T) {
		client := &aliasLookupStub{
			t: t, listResults: []aliasListResult{{page: &kms.ListAliasesOutput{}}},
		}
		query := AliasDataSource{AliasName: name}
		got, err := query.read(t.Context(), client)
		require.ErrorContains(t, err, `list aliases for "alias/aws/SomeService": no exact match`)
		require.Nil(t, got)
		assert.Equal(t, []kms.ListAliasesInput{{}}, client.listCalls)
		assert.Equal(t, []string{"ListAliases"}, client.operations)
	})

	notFound := &kmstypes.NotFoundException{Message: aws.String("gone")}
	denied := errors.New("access denied")
	tests := []struct {
		name      string
		key       *kms.DescribeKeyOutput
		keyErr    error
		wantError string
	}{
		{
			name: "disabled key succeeds",
			key:  aliasKeyResponse(keyID, keyARN, kmstypes.KeyStateDisabled),
		},
		{name: "nil output", wantError: "no key metadata returned"},
		{
			name:      "nil metadata",
			key:       &kms.DescribeKeyOutput{},
			wantError: "no key metadata returned",
		},
		{name: "not found", keyErr: notFound, wantError: "NotFoundException: gone"},
		{name: "other API error", keyErr: denied, wantError: "access denied"},
		{
			name:      "pending deletion",
			key:       aliasKeyResponse(keyID, keyARN, kmstypes.KeyStatePendingDeletion),
			wantError: "key is in state PendingDeletion",
		},
		{
			name:      "pending replica deletion",
			key:       aliasKeyResponse(keyID, keyARN, kmstypes.KeyStatePendingReplicaDeletion),
			wantError: "key is in state PendingReplicaDeletion",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &aliasLookupStub{
				t: t, key: tc.key, keyErr: tc.keyErr,
				listResults: []aliasListResult{{page: &kms.ListAliasesOutput{
					Aliases: []kmstypes.AliasListEntry{
						{AliasName: aws.String(name), AliasArn: aws.String(aliasARN)},
					},
				}}},
			}
			query := AliasDataSource{AliasName: name}
			got, err := query.read(t.Context(), client)
			if tc.wantError == "" {
				require.NoError(t, err)
				assert.Equal(t, &AliasDataSourceOutput{
					AliasName: name, Arn: aliasARN, TargetKeyArn: keyARN, TargetKeyID: keyID,
				}, got)
			} else {
				require.ErrorContains(t, err, `describe key for alias "alias/aws/SomeService": `)
				require.ErrorContains(t, err, tc.wantError)
				require.Nil(t, got)
			}
			if tc.keyErr != nil {
				require.ErrorIs(t, err, tc.keyErr)
			}
			assert.Equal(t, []kms.ListAliasesInput{{}}, client.listCalls)
			assert.Equal(t, []kms.DescribeKeyInput{{KeyId: aws.String(name)}}, client.describeCalls)
			assert.Equal(t, []string{"ListAliases", "DescribeKey"}, client.operations)
		})
	}
}

func aliasKeyResponse(id, arn string, state kmstypes.KeyState) *kms.DescribeKeyOutput {
	return &kms.DescribeKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{
		KeyId: aws.String(id), Arn: aws.String(arn), KeyState: state,
	}}
}
