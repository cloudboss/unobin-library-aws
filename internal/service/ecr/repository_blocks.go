package ecr

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// RepositoryEncryptionConfiguration fixes how the repository's contents are
// encrypted at rest. The encryption type is required: AES256 for Amazon
// S3-managed keys, KMS or KMS_DSSE for a Key Management Service key. The KMS
// key applies to the KMS types and names the key by alias, key id, or ARN;
// when it is omitted, ECR uses the AWS-managed aws/ecr key. ECR sets
// encryption once at create, so a change to this block replaces the
// repository.
//
// An omitted block is the same repository server-side as an explicit AES256
// block, but the two spellings diff against each other as source and that
// diff forces a replace, so pick one spelling and keep it.
type RepositoryEncryptionConfiguration struct {
	EncryptionType string  `ub:"encryption-type"`
	KmsKey         *string `ub:"kms-key"`
}

// RepositoryExclusionFilter is one image tag mutability exclusion filter, the
// SDK's ImageTagMutabilityExclusionFilter: image tags matching it keep the
// mutability the repository-wide setting would otherwise override. The filter
// value and type are both required, and WILDCARD is the only filter type. ECR
// holds the value to 1 to 128 characters of letters, digits, and ._*- with at
// most two * wildcards; those content rules are enforced by the API.
type RepositoryExclusionFilter struct {
	Filter     string `ub:"filter"`
	FilterType string `ub:"filter-type"`
}

// encryptionConfiguration expands the encryption block into the SDK type. The
// KMS key is sent only when non-empty, so an omitted key leaves ECR on the
// AWS-managed aws/ecr key.
func encryptionConfiguration(
	block *RepositoryEncryptionConfiguration,
) *ecrtypes.EncryptionConfiguration {
	if block == nil {
		return nil
	}
	out := &ecrtypes.EncryptionConfiguration{
		EncryptionType: ecrtypes.EncryptionType(block.EncryptionType),
	}
	if key := aws.ToString(block.KmsKey); key != "" {
		out.KmsKey = aws.String(key)
	}
	return out
}

// exclusionFilters expands the desired exclusion filters into the SDK type.
// An empty list expands to nil: CreateRepository omits the parameter, and
// PutImageTagMutability reads it as clearing every filter.
func exclusionFilters(
	filters []RepositoryExclusionFilter,
) []ecrtypes.ImageTagMutabilityExclusionFilter {
	if len(filters) == 0 {
		return nil
	}
	out := make([]ecrtypes.ImageTagMutabilityExclusionFilter, 0, len(filters))
	for _, f := range filters {
		out = append(out, ecrtypes.ImageTagMutabilityExclusionFilter{
			Filter:     aws.String(f.Filter),
			FilterType: ecrtypes.ImageTagMutabilityExclusionFilterType(f.FilterType),
		})
	}
	return out
}
