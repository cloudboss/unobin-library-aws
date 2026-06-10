package ecr

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/stretchr/testify/assert"
)

func TestRepositoryTags(t *testing.T) {
	cases := []struct {
		name string
		tags map[string]string
		want []ecrtypes.Tag
	}{
		{name: "nil map", tags: nil, want: nil},
		{name: "empty map", tags: map[string]string{}, want: nil},
		{
			name: "single tag",
			tags: map[string]string{"Name": "it"},
			want: []ecrtypes.Tag{{Key: aws.String("Name"), Value: aws.String("it")}},
		},
		{
			name: "keys ordered",
			tags: map[string]string{"b": "2", "a": "1", "c": "3"},
			want: []ecrtypes.Tag{
				{Key: aws.String("a"), Value: aws.String("1")},
				{Key: aws.String("b"), Value: aws.String("2")},
				{Key: aws.String("c"), Value: aws.String("3")},
			},
		},
		{
			name: "empty value kept",
			tags: map[string]string{"a": ""},
			want: []ecrtypes.Tag{{Key: aws.String("a"), Value: aws.String("")}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Map iteration order varies, so repeat to confirm the ordering
			// by key holds on every run.
			for range 10 {
				assert.Equal(t, c.want, repositoryTags(c.tags))
			}
		})
	}
}

func TestExclusionFilters(t *testing.T) {
	cases := []struct {
		name    string
		filters []RepositoryExclusionFilter
		want    []ecrtypes.ImageTagMutabilityExclusionFilter
	}{
		{name: "nil list", filters: nil, want: nil},
		{name: "empty list", filters: []RepositoryExclusionFilter{}, want: nil},
		{
			name: "single filter",
			filters: []RepositoryExclusionFilter{
				{Filter: "release-*", FilterType: "WILDCARD"},
			},
			want: []ecrtypes.ImageTagMutabilityExclusionFilter{
				{
					Filter:     aws.String("release-*"),
					FilterType: ecrtypes.ImageTagMutabilityExclusionFilterTypeWildcard,
				},
			},
		},
		{
			name: "order preserved",
			filters: []RepositoryExclusionFilter{
				{Filter: "v*", FilterType: "WILDCARD"},
				{Filter: "latest", FilterType: "WILDCARD"},
			},
			want: []ecrtypes.ImageTagMutabilityExclusionFilter{
				{
					Filter:     aws.String("v*"),
					FilterType: ecrtypes.ImageTagMutabilityExclusionFilterTypeWildcard,
				},
				{
					Filter:     aws.String("latest"),
					FilterType: ecrtypes.ImageTagMutabilityExclusionFilterTypeWildcard,
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, exclusionFilters(c.filters))
		})
	}
}

func TestEncryptionConfiguration(t *testing.T) {
	cases := []struct {
		name  string
		block *RepositoryEncryptionConfiguration
		want  *ecrtypes.EncryptionConfiguration
	}{
		{name: "nil block", block: nil, want: nil},
		{
			name:  "aes256 without key",
			block: &RepositoryEncryptionConfiguration{EncryptionType: "AES256"},
			want:  &ecrtypes.EncryptionConfiguration{EncryptionType: ecrtypes.EncryptionTypeAes256},
		},
		{
			name: "kms with key",
			block: &RepositoryEncryptionConfiguration{
				EncryptionType: "KMS",
				KmsKey:         aws.String("alias/containers"),
			},
			want: &ecrtypes.EncryptionConfiguration{
				EncryptionType: ecrtypes.EncryptionTypeKms,
				KmsKey:         aws.String("alias/containers"),
			},
		},
		{
			name:  "kms without key uses the managed default",
			block: &RepositoryEncryptionConfiguration{EncryptionType: "KMS"},
			want:  &ecrtypes.EncryptionConfiguration{EncryptionType: ecrtypes.EncryptionTypeKms},
		},
		{
			name: "empty key omitted",
			block: &RepositoryEncryptionConfiguration{
				EncryptionType: "KMS",
				KmsKey:         aws.String(""),
			},
			want: &ecrtypes.EncryptionConfiguration{EncryptionType: ecrtypes.EncryptionTypeKms},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, encryptionConfiguration(c.block))
		})
	}
}

func TestErrorPredicates(t *testing.T) {
	principalGone := &ecrtypes.InvalidParameterException{
		Message: aws.String("Invalid parameter at 'PolicyText' failed to satisfy constraint: " +
			"'Invalid repository policy provided' Principal not found"),
	}
	cases := []struct {
		name      string
		err       error
		predicate func(error) bool
		want      bool
	}{
		{
			name:      "repository not found",
			err:       &ecrtypes.RepositoryNotFoundException{},
			predicate: isNotFound,
			want:      true,
		},
		{
			name:      "repository not found wrapped",
			err:       fmt.Errorf("describe: %w", &ecrtypes.RepositoryNotFoundException{}),
			predicate: isNotFound,
			want:      true,
		},
		{
			name:      "other error is not repository not found",
			err:       errors.New("dial tcp: timeout"),
			predicate: isNotFound,
			want:      false,
		},
		{
			name:      "policy not found is not repository not found",
			err:       &ecrtypes.LifecyclePolicyNotFoundException{},
			predicate: isNotFound,
			want:      false,
		},
		{
			name:      "repository not empty",
			err:       &ecrtypes.RepositoryNotEmptyException{},
			predicate: isNotEmpty,
			want:      true,
		},
		{
			name:      "lifecycle policy not found",
			err:       &ecrtypes.LifecyclePolicyNotFoundException{},
			predicate: isLifecyclePolicyNotFound,
			want:      true,
		},
		{
			name:      "repository policy not found",
			err:       &ecrtypes.RepositoryPolicyNotFoundException{},
			predicate: isRepositoryPolicyNotFound,
			want:      true,
		},
		{
			name:      "repository not found is not a policy not found",
			err:       &ecrtypes.RepositoryNotFoundException{},
			predicate: isRepositoryPolicyNotFound,
			want:      false,
		},
		{
			name:      "principal not found",
			err:       principalGone,
			predicate: isPrincipalNotFound,
			want:      true,
		},
		{
			name:      "principal not found wrapped",
			err:       fmt.Errorf("set repository policy: %w", principalGone),
			predicate: isPrincipalNotFound,
			want:      true,
		},
		{
			name:      "invalid parameter without the principal message",
			err:       &ecrtypes.InvalidParameterException{Message: aws.String("bad json")},
			predicate: isPrincipalNotFound,
			want:      false,
		},
		{
			name:      "principal message on the wrong type",
			err:       errors.New("Principal not found"),
			predicate: isPrincipalNotFound,
			want:      false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.predicate(c.err))
		})
	}
}
