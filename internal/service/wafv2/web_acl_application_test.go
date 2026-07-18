package wafv2

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandApplicationConfig(t *testing.T) {
	input := &WebACLApplicationConfig{
		Attributes: []WebACLApplicationAttribute{
			{Name: "environment", Values: []string{"production", "staging"}},
			{Name: "framework", Values: []string{"go"}},
		},
	}

	require.NoError(t, validateApplicationConfig(input))
	assert.Equal(t, &awstypes.ApplicationConfig{
		Attributes: []awstypes.ApplicationAttribute{
			{Name: aws.String("environment"), Values: []string{"production", "staging"}},
			{Name: aws.String("framework"), Values: []string{"go"}},
		},
	}, expandApplicationConfig(input))
}

func TestApplicationConfigOmitsNil(t *testing.T) {
	require.NoError(t, validateApplicationConfig(nil))
	assert.Nil(t, expandApplicationConfig(nil))
}

func TestValidateApplicationConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   *WebACLApplicationConfig
		wantErr string
	}{
		{
			name:    "empty attributes",
			input:   &WebACLApplicationConfig{},
			wantErr: "attributes must contain 1..10 items",
		},
		{
			name: "too many attributes",
			input: &WebACLApplicationConfig{
				Attributes: make([]WebACLApplicationAttribute, 11),
			},
			wantErr: "attributes must contain 1..10 items",
		},
		{
			name: "empty name",
			input: &WebACLApplicationConfig{Attributes: []WebACLApplicationAttribute{
				{Name: "", Values: []string{"value"}},
			}},
			wantErr: "attribute name must be 1..64 characters",
		},
		{
			name: "long name",
			input: &WebACLApplicationConfig{Attributes: []WebACLApplicationAttribute{
				{Name: strings.Repeat("n", 65), Values: []string{"value"}},
			}},
			wantErr: "attribute name must be 1..64 characters",
		},
		{
			name: "invalid name",
			input: &WebACLApplicationConfig{Attributes: []WebACLApplicationAttribute{
				{Name: "bad name", Values: []string{"value"}},
			}},
			wantErr: "attribute name must match ^[A-Za-z0-9_-]+$",
		},
		{
			name: "empty values",
			input: &WebACLApplicationConfig{Attributes: []WebACLApplicationAttribute{
				{Name: "name"},
			}},
			wantErr: "attribute values must contain 1..10 items",
		},
		{
			name: "too many values",
			input: &WebACLApplicationConfig{Attributes: []WebACLApplicationAttribute{
				{Name: "name", Values: make([]string, 11)},
			}},
			wantErr: "attribute values must contain 1..10 items",
		},
		{
			name: "empty value",
			input: &WebACLApplicationConfig{Attributes: []WebACLApplicationAttribute{
				{Name: "name", Values: []string{""}},
			}},
			wantErr: "attribute value must be 1..64 characters",
		},
		{
			name: "long value",
			input: &WebACLApplicationConfig{Attributes: []WebACLApplicationAttribute{
				{Name: "name", Values: []string{strings.Repeat("v", 65)}},
			}},
			wantErr: "attribute value must be 1..64 characters",
		},
		{
			name: "invalid value",
			input: &WebACLApplicationConfig{Attributes: []WebACLApplicationAttribute{
				{Name: "name", Values: []string{"bad value"}},
			}},
			wantErr: "attribute value must match ^[A-Za-z0-9_-]+$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, validateApplicationConfig(tt.input), tt.wantErr)
		})
	}
}

func TestValidateApplicationConfigBoundariesAndDuplicateNames(t *testing.T) {
	component := strings.Repeat("a", 64)
	values := make([]string, 10)
	for index := range values {
		values[index] = component
	}
	attributes := make([]WebACLApplicationAttribute, 10)
	for index := range attributes {
		attributes[index] = WebACLApplicationAttribute{Name: component, Values: values}
	}

	require.NoError(t, validateApplicationConfig(&WebACLApplicationConfig{
		Attributes: attributes,
	}))
}
