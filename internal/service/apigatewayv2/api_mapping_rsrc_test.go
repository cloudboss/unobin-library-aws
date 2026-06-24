package apigatewayv2

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApiMappingReplaceFields(t *testing.T) {
	var r ApiMapping

	assert.Equal(t, []string{"api-id", "domain-name"}, r.ReplaceFields())
}

func TestApiMappingCreateInputOmitsEmptyKey(t *testing.T) {
	tests := []struct {
		name string
		key  *string
	}{
		{name: "absent"},
		{name: "empty", key: aws.String("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ApiMapping{
				ApiId:         "api-123",
				DomainName:    "api.example.com",
				Stage:         "$default",
				ApiMappingKey: tt.key,
			}

			in := r.createInput()

			assert.Equal(t, "api-123", aws.ToString(in.ApiId))
			assert.Equal(t, "api.example.com", aws.ToString(in.DomainName))
			assert.Equal(t, "$default", aws.ToString(in.Stage))
			assert.Nil(t, in.ApiMappingKey)
		})
	}
}

func TestApiMappingCreateInputIncludesNonEmptyKey(t *testing.T) {
	r := ApiMapping{
		ApiId:         "api-123",
		DomainName:    "api.example.com",
		Stage:         "$default",
		ApiMappingKey: aws.String("v1"),
	}

	in := r.createInput()

	assert.Equal(t, "v1", aws.ToString(in.ApiMappingKey))
}

func TestApiMappingUpdateInputUnchanged(t *testing.T) {
	prior := apiMappingPrior(ApiMapping{
		ApiId:         "api-123",
		DomainName:    "api.example.com",
		Stage:         "$default",
		ApiMappingKey: aws.String("v1"),
	})
	r := prior.Inputs

	in, changed := r.updateInput(prior)

	assert.False(t, changed)
	assert.Nil(t, in)
}

func TestApiMappingUpdateInputClearsKey(t *testing.T) {
	prior := apiMappingPrior(ApiMapping{
		ApiId:         "api-123",
		DomainName:    "api.example.com",
		Stage:         "$default",
		ApiMappingKey: aws.String("v1"),
	})
	r := ApiMapping{
		ApiId:      "api-123",
		DomainName: "api.example.com",
		Stage:      "$default",
	}

	in, changed := r.updateInput(prior)

	require.True(t, changed)
	assert.Equal(t, "api-123", aws.ToString(in.ApiId))
	assert.Equal(t, "mapping-123", aws.ToString(in.ApiMappingId))
	assert.Equal(t, "api.example.com", aws.ToString(in.DomainName))
	assert.Equal(t, "", aws.ToString(in.ApiMappingKey))
	assert.Nil(t, in.Stage)
}

func TestApiMappingUpdateInputChangedStage(t *testing.T) {
	prior := apiMappingPrior(ApiMapping{
		ApiId:      "api-123",
		DomainName: "api.example.com",
		Stage:      "blue",
	})
	r := ApiMapping{
		ApiId:      "api-123",
		DomainName: "api.example.com",
		Stage:      "green",
	}

	in, changed := r.updateInput(prior)

	require.True(t, changed)
	assert.Nil(t, in.ApiMappingKey)
	assert.Equal(t, "green", aws.ToString(in.Stage))
}

func apiMappingPrior(inputs ApiMapping) runtime.Prior[ApiMapping, *ApiMappingOutput] {
	return runtime.Prior[ApiMapping, *ApiMappingOutput]{
		Inputs: inputs,
		Outputs: &ApiMappingOutput{
			ApiMappingId: "mapping-123",
			DomainName:   "api.example.com",
		},
	}
}
