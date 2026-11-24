package kms

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type keyLookupStub struct {
	t     *testing.T
	ctx   context.Context
	out   *kms.DescribeKeyOutput
	err   error
	calls []kms.DescribeKeyInput
}

func (s *keyLookupStub) DescribeKey(
	ctx context.Context, in *kms.DescribeKeyInput, _ ...func(*kms.Options),
) (*kms.DescribeKeyOutput, error) {
	assert.Same(s.t, s.ctx, ctx)
	s.calls = append(s.calls, *in)
	return s.out, s.err
}

func TestKeyLookupGrantTokens(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  *[]string
		tokens []string
	}{
		{name: "omitted"},
		{name: "null"},
		{name: "empty", input: new([]string{})},
		{name: "mixed", input: new([]string{"", "b", "a", "b", ""}),
			tokens: []string{"b", "a", "b"}},
		{name: "all empty", input: new([]string{"", ""}), tokens: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := KeyDataSource{KeyID: "alias/test", GrantTokens: tc.input}
			got, err := r.prepareDescribeKey()
			require.NoError(t, err)
			assert.Equal(t, &kms.DescribeKeyInput{
				KeyId: aws.String("alias/test"), GrantTokens: tc.tokens,
			}, got)
		})
	}
}

func TestKeyLookupSelectors(t *testing.T) {
	const uuid = "12345678-1234-1234-a123-123456789abc"
	const mrk = "mrk-1234567890abcdef1234567890abcdef"
	for _, selector := range []string{
		"", uuid, mrk, uuid + " arbitrary suffix!", "arbitrary prefix!" + mrk,
		"alias/aws/service", "alias/MiXeD_12/path-", "alias//",
		"arn:aws:kms:us-east-1:123456789012:key/" + uuid,
		"arn:aws-cn:kms:cn-north-1:123456789012:key/" + mrk,
		"arn:aws-us-gov:kms:us-gov-west-1:123456789012:alias/test",
		"arn:::::key/" + uuid, "arn:other:other:other:other:alias/",
		"arn:::::alias/! spaces:unicode-λ",
		"arn:::::key/prefix" + mrk,
	} {
		t.Run(selector, func(t *testing.T) {
			r := KeyDataSource{KeyID: selector}
			got, err := r.prepareDescribeKey()
			require.NoError(t, err)
			assert.Equal(t, &kms.DescribeKeyInput{KeyId: aws.String(selector)}, got)
		})
	}
	for _, selector := range []string{
		" ", "bad", "alias/", "Alias/test", "alias/test!", " " + uuid,
		"12345678-1234-6234-a123-123456789abc", "12345678-1234-1234-c123-123456789abc",
		"12345678-1234-1234-a123-123456789abC", mrk + "suffix", "mrk-abc",
		"arn:kms:key/" + uuid, "ARN:aws:kms:x:x:alias/test", "arn:::::Key/" + uuid,
		"arn:::::key/" + uuid + "suffix",
		"arn:::::alias", "arn:::::Alias/test",
	} {
		t.Run(selector, func(t *testing.T) {
			r := KeyDataSource{KeyID: selector}
			client := &keyLookupStub{t: t, ctx: t.Context()}
			got, err := r.read(t.Context(), client)
			require.ErrorContains(t, err, "invalid key selector")
			require.Nil(t, got)
			assert.Empty(t, client.calls)
		})
	}
}

func TestKeyLookupMetadata(t *testing.T) {
	stamp := time.Date(2024, 2, 3, 4, 5, 6, 987654321, time.FixedZone("offset", 3600))
	meta := &kmstypes.KeyMetadata{
		KeyId: aws.String("canonical"), Arn: aws.String("key-arn"),
		AWSAccountId: aws.String("account"), CloudHsmClusterId: aws.String("cluster"),
		CreationDate: &stamp, CustomKeyStoreId: aws.String("store"), DeletionDate: &stamp,
		Description: aws.String("description"), ExpirationModel: "KEY_MATERIAL_EXPIRES",
		KeyManager: "CUSTOMER", KeySpec: "SYMMETRIC_DEFAULT", KeyState: "Enabled",
		KeyUsage: "ENCRYPT_DECRYPT", Origin: "EXTERNAL", ValidTo: &stamp, Enabled: true,
		MultiRegion: aws.Bool(true), PendingDeletionWindowInDays: aws.Int32(9),
		MultiRegionConfiguration: &kmstypes.MultiRegionConfiguration{
			MultiRegionKeyType: "PRIMARY",
			PrimaryKey: &kmstypes.MultiRegionKey{
				Arn: aws.String("primary"), Region: aws.String("r1"),
			},
			ReplicaKeys: []kmstypes.MultiRegionKey{
				{Arn: aws.String("replica2"), Region: aws.String("r2")},
				{Arn: aws.String("replica1"), Region: aws.String("r1")},
				{},
			},
		},
		XksKeyConfiguration: &kmstypes.XksKeyConfigurationType{Id: aws.String("external-id")},
	}
	want := &KeyDataSourceOutput{
		KeyID: "canonical", Arn: "key-arn", AWSAccountID: "account", CloudHSMClusterID: "cluster",
		CreationDate: "2024-02-03T04:05:06+01:00", CustomKeyStoreID: "store",
		DeletionDate: "2024-02-03T04:05:06+01:00", Description: "description",
		ExpirationModel: "KEY_MATERIAL_EXPIRES", KeyManager: "CUSTOMER",
		KeySpec: "SYMMETRIC_DEFAULT", KeyState: "Enabled", KeyUsage: "ENCRYPT_DECRYPT",
		Origin: "EXTERNAL", ValidTo: "2024-02-03T04:05:06+01:00", Enabled: true,
		MultiRegion: true, PendingDeletionWindowInDays: 9,
		MultiRegionConfiguration: &keyLookupMultiRegion{
			MultiRegionKeyType: "PRIMARY",
			PrimaryKey:         &keyLookupRegional{Arn: "primary", Region: "r1"},
			ReplicaKeys: []keyLookupRegional{
				{Arn: "replica2", Region: "r2"}, {Arn: "replica1", Region: "r1"}, {},
			},
		},
		XKSKeyConfiguration: &keyLookupXKS{ID: "external-id"},
	}
	r := KeyDataSource{KeyID: "alias/test"}
	client := &keyLookupStub{t: t, ctx: t.Context(), out: &kms.DescribeKeyOutput{KeyMetadata: meta}}
	got, err := r.read(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, []kms.DescribeKeyInput{{KeyId: aws.String("alias/test")}}, client.calls)

	for _, state := range []kmstypes.KeyState{"", "PendingDeletion", "PendingReplicaDeletion"} {
		client.out.KeyMetadata = &kmstypes.KeyMetadata{KeyState: state}
		got, err = r.read(t.Context(), client)
		require.NoError(t, err)
		assert.Equal(t, &KeyDataSourceOutput{
			CreationDate: "0001-01-01T00:00:00Z", KeyState: string(state),
		}, got)
	}
	client.out.KeyMetadata.MultiRegionConfiguration = &kmstypes.MultiRegionConfiguration{}
	client.out.KeyMetadata.XksKeyConfiguration = &kmstypes.XksKeyConfigurationType{}
	got, err = r.read(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, &KeyDataSourceOutput{
		CreationDate: "0001-01-01T00:00:00Z", KeyState: "PendingReplicaDeletion",
		MultiRegionConfiguration: &keyLookupMultiRegion{ReplicaKeys: []keyLookupRegional{}},
		XKSKeyConfiguration:      &keyLookupXKS{},
	}, got)
}

func TestKeyLookupErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  *kms.DescribeKeyOutput
		err  error
	}{
		{name: "nil response"},
		{name: "nil metadata", out: &kms.DescribeKeyOutput{}},
		{name: "not found", err: &kmstypes.NotFoundException{Message: aws.String("gone")}},
		{name: "access denied", err: &smithy.GenericAPIError{Code: "AccessDeniedException"}},
		{name: "cancelled", err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := KeyDataSource{KeyID: "alias/test"}
			client := &keyLookupStub{t: t, ctx: t.Context(), out: tc.out, err: tc.err}
			got, err := r.read(t.Context(), client)
			require.ErrorContains(t, err, `describe key "alias/test":`)
			require.Nil(t, got)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.ErrorContains(t, err, "no key metadata returned")
			}
			assert.Equal(t, []kms.DescribeKeyInput{{KeyId: aws.String("alias/test")}}, client.calls)
		})
	}
}
