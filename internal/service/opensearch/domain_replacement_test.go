package opensearch

import (
	"context"
	"errors"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConditionalDomainReplacements(t *testing.T) {
	tests := []struct {
		name    string
		prior   DomainResource
		current DomainResource
		wantErr string
	}{
		{
			name:  "disable encryption",
			prior: DomainResource{EncryptAtRest: &DomainEncryptionAtRestOptions{Enabled: true}},
			current: DomainResource{
				EncryptAtRest: &DomainEncryptionAtRestOptions{Enabled: false},
			},
			wantErr: "encrypt-at-rest",
		},
		{
			name: "unsupported encryption enable",
			prior: DomainResource{
				EngineVersion: stringPointer("Elasticsearch_6.6"),
				EncryptAtRest: &DomainEncryptionAtRestOptions{},
			},
			current: DomainResource{
				EngineVersion: stringPointer("Elasticsearch_6.6"),
				EncryptAtRest: &DomainEncryptionAtRestOptions{Enabled: true},
			},
			wantErr: "encrypt-at-rest",
		},
		{
			name: "supported encryption enable",
			prior: DomainResource{
				EngineVersion: stringPointer("Elasticsearch_6.7"),
				EncryptAtRest: &DomainEncryptionAtRestOptions{},
			},
			current: DomainResource{
				EngineVersion: stringPointer("Elasticsearch_6.7"),
				EncryptAtRest: &DomainEncryptionAtRestOptions{Enabled: true},
			},
		},
		{
			name: "kms key change",
			prior: DomainResource{EncryptAtRest: &DomainEncryptionAtRestOptions{
				Enabled: true, KMSKeyID: stringPointer("old"),
			}},
			current: DomainResource{EncryptAtRest: &DomainEncryptionAtRestOptions{
				Enabled: true, KMSKeyID: stringPointer("new"),
			}},
			wantErr: "kms-key-id",
		},
		{
			name: "disable node encryption",
			prior: DomainResource{
				NodeToNodeEncryption: &DomainNodeToNodeEncryptionOptions{Enabled: true},
			},
			current: DomainResource{
				NodeToNodeEncryption: &DomainNodeToNodeEncryptionOptions{Enabled: false},
			},
			wantErr: "node-to-node-encryption",
		},
		{
			name: "disable advanced security",
			prior: DomainResource{AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
				Enabled: true,
			}},
			current: DomainResource{AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
				Enabled: false,
			}},
			wantErr: "advanced-security-options",
		},
		{
			name:    "dualstack to ipv4",
			prior:   DomainResource{IPAddressType: stringPointer("dualstack")},
			current: DomainResource{IPAddressType: stringPointer("ipv4")},
			wantErr: "ip-address-type",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateConditionalReplacements(test.prior, test.current)
			if test.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, test.wantErr)
			assert.ErrorContains(t, err, "requires replacement")
		})
	}
}

func TestDomainEngineCompatibilityPreflight(t *testing.T) {
	tests := []struct {
		name      string
		output    *awssdk.GetCompatibleVersionsOutput
		lookupErr error
		wantErr   bool
	}{
		{
			name: "compatible",
			output: &awssdk.GetCompatibleVersionsOutput{CompatibleVersions: []awstypes.CompatibleVersionsMap{
				{TargetVersions: []string{"OpenSearch_2.13"}},
			}},
		},
		{name: "lookup error proceeds", lookupErr: errors.New("lookup failed")},
		{name: "no maps", output: &awssdk.GetCompatibleVersionsOutput{}, wantErr: true},
		{
			name: "multiple maps",
			output: &awssdk.GetCompatibleVersionsOutput{CompatibleVersions: []awstypes.CompatibleVersionsMap{
				{TargetVersions: []string{"OpenSearch_2.13"}},
				{TargetVersions: []string{"OpenSearch_2.13"}},
			}},
			wantErr: true,
		},
		{
			name: "target missing",
			output: &awssdk.GetCompatibleVersionsOutput{CompatibleVersions: []awstypes.CompatibleVersionsMap{
				{TargetVersions: []string{"OpenSearch_2.12"}},
			}},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeDomainClient{
				getCompatibleVersions: func(
					context.Context,
					*awssdk.GetCompatibleVersionsInput,
				) (*awssdk.GetCompatibleVersionsOutput, error) {
					return test.output, test.lookupErr
				},
			}
			prior := DomainResource{EngineVersion: stringPointer("OpenSearch_2.11")}
			current := DomainResource{EngineVersion: stringPointer("OpenSearch_2.13")}
			err := current.preflightUpdate(context.Background(), client, "example", prior)
			if test.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, "requires replacement")
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestEquivalentKMSKeyIDs(t *testing.T) {
	tests := []struct {
		name  string
		left  *string
		right *string
		want  bool
	}{
		{name: "both absent", want: true},
		{name: "one absent", right: stringPointer("key-id")},
		{name: "exact ID", left: stringPointer("key-id"), right: stringPointer("key-id"), want: true},
		{
			name:  "ARN and ID",
			left:  stringPointer("arn:aws:kms:us-east-1:123456789012:key/key-id"),
			right: stringPointer("key-id"),
			want:  true,
		},
		{
			name:  "ID and ARN",
			left:  stringPointer("key-id"),
			right: stringPointer("arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/key-id"),
			want:  true,
		},
		{
			name: "substring IDs",
			left: stringPointer("key-id"), right: stringPointer("prefix-key-id-suffix"),
		},
		{
			name:  "ARN near match",
			left:  stringPointer("arn:aws:kms:us-east-1:123456789012:key/prefix-key-id"),
			right: stringPointer("key-id"),
		},
		{
			name:  "alias ARN is not a key ID",
			left:  stringPointer("arn:aws:kms:us-east-1:123456789012:alias/key-id"),
			right: stringPointer("key-id"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, equivalentKMSKeyIDs(test.left, test.right))
		})
	}
}
