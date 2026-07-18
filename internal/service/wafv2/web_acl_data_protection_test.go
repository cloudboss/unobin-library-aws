package wafv2

import (
	"strings"
	"testing"

	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandDataProtectionConfig(t *testing.T) {
	headerKeys := []string{"authorization", "x-api-key"}
	emptyKeys := []string{}
	input := &WebACLDataProtectionConfig{
		DataProtections: []WebACLDataProtection{
			{
				Action:                  "HASH",
				ExcludeRateBasedDetails: true,
				Field: WebACLDataProtectionField{
					FieldKeys: &headerKeys,
					FieldType: "SINGLE_HEADER",
				},
			},
			{
				Action:                  "SUBSTITUTION",
				ExcludeRuleMatchDetails: true,
				Field: WebACLDataProtectionField{
					FieldType: "BODY",
				},
			},
			{
				Action: "HASH",
				Field: WebACLDataProtectionField{
					FieldKeys: &emptyKeys,
					FieldType: "QUERY_STRING",
				},
			},
		},
	}

	require.NoError(t, validateDataProtectionConfig(input))
	assert.Equal(t, &awstypes.DataProtectionConfig{
		DataProtections: []awstypes.DataProtection{
			{
				Action:                  awstypes.DataProtectionActionHash,
				ExcludeRateBasedDetails: true,
				Field: &awstypes.FieldToProtect{
					FieldKeys: []string{"authorization", "x-api-key"},
					FieldType: awstypes.FieldToProtectTypeSingleHeader,
				},
			},
			{
				Action:                  awstypes.DataProtectionActionSubstitution,
				ExcludeRuleMatchDetails: true,
				Field: &awstypes.FieldToProtect{
					FieldType: awstypes.FieldToProtectTypeBody,
				},
			},
			{
				Action: awstypes.DataProtectionActionHash,
				Field: &awstypes.FieldToProtect{
					FieldKeys: []string{},
					FieldType: awstypes.FieldToProtectTypeQueryString,
				},
			},
		},
	}, expandDataProtectionConfig(input))
}

func TestDataProtectionConfigNilAndEmpty(t *testing.T) {
	require.NoError(t, validateDataProtectionConfig(nil))
	assert.Nil(t, expandDataProtectionConfig(nil))

	assert.ErrorContains(
		t,
		validateDataProtectionConfig(&WebACLDataProtectionConfig{}),
		"data-protections must contain 1..26 items",
	)
}

func TestValidateDataProtectionConfig(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WebACLDataProtectionConfig)
		wantErr string
	}{
		{
			name: "too many protections",
			mutate: func(config *WebACLDataProtectionConfig) {
				config.DataProtections = make([]WebACLDataProtection, 27)
			},
			wantErr: "data-protections must contain 1..26 items",
		},
		{
			name: "invalid action",
			mutate: func(config *WebACLDataProtectionConfig) {
				config.DataProtections[0].Action = "REDACT"
			},
			wantErr: "action must be SUBSTITUTION or HASH",
		},
		{
			name: "missing field type",
			mutate: func(config *WebACLDataProtectionConfig) {
				config.DataProtections[0].Field.FieldType = ""
			},
			wantErr: "field-type must be one of SINGLE_HEADER, SINGLE_COOKIE, " +
				"SINGLE_QUERY_ARGUMENT, QUERY_STRING, BODY",
		},
		{
			name: "invalid field type",
			mutate: func(config *WebACLDataProtectionConfig) {
				config.DataProtections[0].Field.FieldType = "HEADER"
			},
			wantErr: "field-type must be one of SINGLE_HEADER, SINGLE_COOKIE, " +
				"SINGLE_QUERY_ARGUMENT, QUERY_STRING, BODY",
		},
		{
			name: "too many field keys",
			mutate: func(config *WebACLDataProtectionConfig) {
				keys := make([]string, 101)
				config.DataProtections[0].Field.FieldKeys = &keys
			},
			wantErr: "field-keys must contain at most 100 items",
		},
		{
			name: "empty field key",
			mutate: func(config *WebACLDataProtectionConfig) {
				keys := []string{""}
				config.DataProtections[0].Field.FieldKeys = &keys
			},
			wantErr: "field-key must be 1..64 nonblank characters",
		},
		{
			name: "long field key",
			mutate: func(config *WebACLDataProtectionConfig) {
				keys := []string{strings.Repeat("k", 65)}
				config.DataProtections[0].Field.FieldKeys = &keys
			},
			wantErr: "field-key must be 1..64 nonblank characters",
		},
		{
			name: "blank field key",
			mutate: func(config *WebACLDataProtectionConfig) {
				keys := []string{" \t"}
				config.DataProtections[0].Field.FieldKeys = &keys
			},
			wantErr: "field-key must be 1..64 nonblank characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validDataProtectionConfig()
			tt.mutate(config)
			assert.ErrorContains(t, validateDataProtectionConfig(config), tt.wantErr)
		})
	}
}

func TestValidateDataProtectionBoundariesWithoutExtraRestrictions(t *testing.T) {
	keys := make([]string, 100)
	for index := range keys {
		keys[index] = strings.Repeat("k", 64)
	}
	protections := make([]WebACLDataProtection, 26)
	for index := range protections {
		protections[index] = WebACLDataProtection{
			Action: "HASH",
			Field: WebACLDataProtectionField{
				FieldKeys: &keys,
				FieldType: "BODY",
			},
		}
	}
	require.NoError(t, validateDataProtectionConfig(&WebACLDataProtectionConfig{
		DataProtections: protections,
	}))
}

func TestValidateDataProtectionFieldTypes(t *testing.T) {
	for _, fieldType := range []string{
		"SINGLE_HEADER",
		"SINGLE_COOKIE",
		"SINGLE_QUERY_ARGUMENT",
		"QUERY_STRING",
		"BODY",
	} {
		t.Run(fieldType, func(t *testing.T) {
			config := validDataProtectionConfig()
			config.DataProtections[0].Field.FieldType = fieldType
			require.NoError(t, validateDataProtectionConfig(config))
		})
	}
}

func validDataProtectionConfig() *WebACLDataProtectionConfig {
	return &WebACLDataProtectionConfig{
		DataProtections: []WebACLDataProtection{
			{
				Action: "SUBSTITUTION",
				Field:  WebACLDataProtectionField{FieldType: "SINGLE_HEADER"},
			},
		},
	}
}
