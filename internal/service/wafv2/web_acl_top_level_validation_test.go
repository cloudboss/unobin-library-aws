package wafv2

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebACLTagsNilAndEmpty(t *testing.T) {
	require.NoError(t, validateWebACLTags(nil))

	empty := map[string]string{}
	require.NoError(t, validateWebACLTags(&empty))
	assert.Equal(t, []awstypes.Tag{}, expandTags(empty))
}

func TestValidateWebACLTags(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{
			name:    "empty key",
			wantErr: "tag key must be 1..128 characters",
		},
		{
			name:    "long key",
			key:     strings.Repeat("界", 129),
			wantErr: "tag key must be 1..128 characters",
		},
		{
			name:    "long value",
			key:     "key",
			value:   strings.Repeat("é", 257),
			wantErr: "tag value must be 0..256 characters",
		},
		{
			name:    "invalid key characters",
			key:     "key#",
			wantErr: "tag key must match the WAF character pattern",
		},
		{
			name:    "invalid value characters",
			key:     "key",
			value:   "value#",
			wantErr: "tag value must match the WAF character pattern",
		},
		{
			name:    "reserved prefix",
			key:     "aws:reserved",
			wantErr: "tag key must not start with aws:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := map[string]string{tt.key: tt.value}
			assert.ErrorContains(t, validateWebACLTags(&tags), tt.wantErr)
		})
	}
}

func TestValidateWebACLTagBoundariesAndCaseSensitivePrefix(t *testing.T) {
	tags := map[string]string{
		strings.Repeat("界", 128): strings.Repeat("é", 256),
		"AWS:reserved":           "",
		"Team Name:/=+-@_.":      "Value With Spaces:/=+-@_.",
	}
	require.NoError(t, validateWebACLTags(&tags))
}

func TestValidateWebACLTagMaximum(t *testing.T) {
	tags := make(map[string]string, 50)
	for index := 0; index < 50; index++ {
		tags[fmt.Sprintf("key-%d", index)] = "value"
	}
	require.NoError(t, validateWebACLTags(&tags))

	tags["overflow"] = "value"
	assert.ErrorContains(t, validateWebACLTags(&tags), "tags must contain at most 50 items")
}

func TestValidateWebACLIdentityAndDescription(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WebACLResource)
		wantErr string
	}{
		{
			name: "empty explicit name",
			mutate: func(resource *WebACLResource) {
				value := ""
				resource.Name = &value
			},
			wantErr: "name must be 1..128 characters",
		},
		{
			name: "long name",
			mutate: func(resource *WebACLResource) {
				value := strings.Repeat("n", 129)
				resource.Name = &value
			},
			wantErr: "name must be 1..128 characters",
		},
		{
			name: "invalid name",
			mutate: func(resource *WebACLResource) {
				value := "bad name"
				resource.Name = &value
			},
			wantErr: "name must match ^[0-9A-Za-z_-]+$",
		},
		{
			name: "invalid scope",
			mutate: func(resource *WebACLResource) {
				resource.Scope = "GLOBAL"
			},
			wantErr: "scope must be REGIONAL or CLOUDFRONT",
		},
		{
			name: "empty description",
			mutate: func(resource *WebACLResource) {
				value := ""
				resource.Description = &value
			},
			wantErr: "description must be 1..256 characters",
		},
		{
			name: "long description",
			mutate: func(resource *WebACLResource) {
				value := strings.Repeat("d", 257)
				resource.Description = &value
			},
			wantErr: "description must be 1..256 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validWebACLResource()
			tt.mutate(resource)
			assert.ErrorContains(
				t,
				resource.ValidateInputs(context.Background(), nil),
				tt.wantErr,
			)
		})
	}
}

func TestValidateWebACLIdentityAndDescriptionBoundaries(t *testing.T) {
	for _, name := range []string{"x", strings.Repeat("n", 128)} {
		resource := validWebACLResource()
		resource.Name = aws.String(name)
		resource.Description = aws.String(strings.Repeat("界", 256))
		require.NoError(t, resource.ValidateInputs(context.Background(), nil))
	}

	resource := validWebACLResource()
	resource.Name = nil
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestWebACLRejectsTopLevelVisibilityBeforeAPICall(t *testing.T) {
	tests := []struct {
		name    string
		metric  string
		wantErr string
	}{
		{
			name:    "empty",
			wantErr: "metric-name must be 1..128 characters",
		},
		{
			name:    "long",
			metric:  strings.Repeat("m", 129),
			wantErr: "metric-name must be 1..128 characters",
		},
		{
			name:    "invalid characters",
			metric:  "bad metric",
			wantErr: "metric-name must match ^[0-9A-Za-z_-]+$",
		},
		{
			name:    "reserved All",
			metric:  "All",
			wantErr: "metric-name is reserved",
		},
		{
			name:    "reserved Default_Action",
			metric:  "Default_Action",
			wantErr: "metric-name is reserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priorInputs := *validWebACLResource()
			resource := validWebACLResource()
			resource.VisibilityConfig.MetricName = tt.metric
			client := &fakeWAFClient{
				create: func(
					context.Context,
					*awssvc.CreateWebACLInput,
				) (*awssvc.CreateWebACLOutput, error) {
					t.Fatal("unexpected CreateWebACL call")
					return nil, nil
				},
				update: func(
					context.Context,
					*awssvc.UpdateWebACLInput,
				) (*awssvc.UpdateWebACLOutput, error) {
					t.Fatal("unexpected UpdateWebACL call")
					return nil, nil
				},
			}

			out, err := resource.create(context.Background(), client, strings.NewReader(""))
			assert.Nil(t, out)
			assert.ErrorContains(t, err, tt.wantErr)

			out, err = resource.update(
				context.Background(),
				client,
				validWebACLUpdatePrior(priorInputs),
			)
			assert.Nil(t, out)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestWebACLValidateInputsIncludesProtectionFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WebACLResource)
		wantErr string
	}{
		{
			name: "data protection",
			mutate: func(resource *WebACLResource) {
				resource.DataProtectionConfig = &WebACLDataProtectionConfig{}
			},
			wantErr: "data-protections must contain 1..26 items",
		},
		{
			name: "on-source DDoS",
			mutate: func(resource *WebACLResource) {
				resource.OnSourceDDoSConfig = &WebACLOnSourceDDoSConfig{}
			},
			wantErr: "alb-low-reputation-mode must be ACTIVE_UNDER_DDOS or ALWAYS_ON",
		},
		{
			name: "token domains",
			mutate: func(resource *WebACLResource) {
				domains := []string{"com"}
				resource.TokenDomains = &domains
			},
			wantErr: "token-domain must include a registrable domain above its suffix",
		},
		{
			name: "tags",
			mutate: func(resource *WebACLResource) {
				resource.Tags = &map[string]string{"aws:reserved": "value"}
			},
			wantErr: "tag key must not start with aws:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validWebACLResource()
			tt.mutate(resource)
			assert.ErrorContains(
				t,
				resource.ValidateInputs(context.Background(), nil),
				tt.wantErr,
			)
		})
	}
}
