package wafv2

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandCustomResponseBodies(t *testing.T) {
	input := map[string]WebACLCustomResponseBody{
		"plain": {Content: "plain text", ContentType: "TEXT_PLAIN"},
		"html":  {Content: "<strong>blocked</strong>", ContentType: "TEXT_HTML"},
		"json":  {Content: "not parsed as JSON", ContentType: "APPLICATION_JSON"},
	}

	require.NoError(t, validateCustomResponseBodies(&input))
	assert.Equal(t, map[string]awstypes.CustomResponseBody{
		"plain": {
			Content:     aws.String("plain text"),
			ContentType: awstypes.ResponseContentTypeTextPlain,
		},
		"html": {
			Content:     aws.String("<strong>blocked</strong>"),
			ContentType: awstypes.ResponseContentTypeTextHtml,
		},
		"json": {
			Content:     aws.String("not parsed as JSON"),
			ContentType: awstypes.ResponseContentTypeApplicationJson,
		},
	}, expandCustomResponseBodies(&input))
}

func TestCustomResponseBodiesNilAndEmpty(t *testing.T) {
	require.NoError(t, validateCustomResponseBodies(nil))
	assert.Nil(t, expandCustomResponseBodies(nil))

	empty := map[string]WebACLCustomResponseBody{}
	require.NoError(t, validateCustomResponseBodies(&empty))
	assert.Equal(t, map[string]awstypes.CustomResponseBody{}, expandCustomResponseBodies(&empty))
}

func TestValidateCustomResponseBodies(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		body    WebACLCustomResponseBody
		wantErr string
	}{
		{
			name:    "empty key",
			body:    validCustomResponseBody(),
			wantErr: "custom response body key must be 1..128 characters",
		},
		{
			name:    "long key",
			key:     strings.Repeat("k", 129),
			body:    validCustomResponseBody(),
			wantErr: "custom response body key must be 1..128 characters",
		},
		{
			name:    "invalid key",
			key:     "bad key",
			body:    validCustomResponseBody(),
			wantErr: "custom response body key must match ^[A-Za-z0-9_-]+$",
		},
		{
			name:    "empty content",
			key:     "body",
			body:    WebACLCustomResponseBody{ContentType: "TEXT_PLAIN"},
			wantErr: "custom response body content must be 1..10240 characters",
		},
		{
			name: "long content",
			key:  "body",
			body: WebACLCustomResponseBody{
				Content:     strings.Repeat("c", 10241),
				ContentType: "TEXT_PLAIN",
			},
			wantErr: "custom response body content must be 1..10240 characters",
		},
		{
			name: "invalid content type",
			key:  "body",
			body: WebACLCustomResponseBody{
				Content:     "content",
				ContentType: "JSON",
			},
			wantErr: "content-type must be TEXT_PLAIN, TEXT_HTML, or APPLICATION_JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := map[string]WebACLCustomResponseBody{tt.key: tt.body}
			assert.ErrorContains(t, validateCustomResponseBodies(&input), tt.wantErr)
		})
	}
}

func TestValidateCustomResponseBodyBoundaries(t *testing.T) {
	input := map[string]WebACLCustomResponseBody{
		"x": {
			Content:     "x",
			ContentType: "TEXT_PLAIN",
		},
		strings.Repeat("k", 128): {
			Content:     strings.Repeat("c", 10240),
			ContentType: "TEXT_HTML",
		},
	}
	require.NoError(t, validateCustomResponseBodies(&input))
}

func TestCustomResponseBodiesHaveNoLocalMaximum(t *testing.T) {
	input := make(map[string]WebACLCustomResponseBody, 101)
	for index := 0; index < 101; index++ {
		input[fmt.Sprintf("body-%d", index)] = validCustomResponseBody()
	}

	require.NoError(t, validateCustomResponseBodies(&input))
	assert.Len(t, expandCustomResponseBodies(&input), 101)
}

func validCustomResponseBody() WebACLCustomResponseBody {
	return WebACLCustomResponseBody{Content: "content", ContentType: "TEXT_PLAIN"}
}
