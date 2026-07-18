package wafv2

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandTokenDomainsPreservesCaseAndOrder(t *testing.T) {
	input := []string{"Shop.Example.COM", "api.example.co.uk"}
	require.NoError(t, validateTokenDomains(&input))
	assert.Equal(t, input, expandTokenDomains(&input))
}

func TestTokenDomainsNilAndEmpty(t *testing.T) {
	require.NoError(t, validateTokenDomains(nil))
	assert.Nil(t, expandTokenDomains(nil))

	empty := []string{}
	require.NoError(t, validateTokenDomains(&empty))
	assert.Equal(t, []string{}, expandTokenDomains(&empty))
}

func TestValidateTokenDomains(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr string
	}{
		{
			name:    "empty",
			wantErr: "token-domain must be 1..253 characters",
		},
		{
			name:    "long",
			domain:  strings.Repeat("d", 254),
			wantErr: "token-domain must be 1..253 characters",
		},
		{
			name:    "invalid ASCII characters",
			domain:  "bad domain.example.com",
			wantErr: "token-domain has invalid characters",
		},
		{
			name:    "non-ASCII",
			domain:  "café.example.com",
			wantErr: "token-domain has invalid characters",
		},
		{
			name:    "slash",
			domain:  "foo/bar.com",
			wantErr: "token-domain has invalid characters",
		},
		{
			name:    "underscore",
			domain:  "foo_bar.com",
			wantErr: "token-domain has invalid characters",
		},
		{
			name:    "empty label",
			domain:  "foo..com",
			wantErr: "token-domain must be a valid DNS name",
		},
		{
			name:    "leading hyphen",
			domain:  "-foo.com",
			wantErr: "token-domain must be a valid DNS name",
		},
		{
			name:    "trailing hyphen",
			domain:  "foo-.com",
			wantErr: "token-domain must be a valid DNS name",
		},
		{
			name:    "trailing dot",
			domain:  "foo.com.",
			wantErr: "token-domain must be a valid DNS name",
		},
		{
			name:    "long label",
			domain:  strings.Repeat("a", 64) + ".com",
			wantErr: "token-domain must be a valid DNS name",
		},
		{
			name:    "IPv4 address",
			domain:  "192.0.2.1",
			wantErr: "token-domain must be a valid DNS name",
		},
		{
			name:    "IPv6 address",
			domain:  "2001:db8::1",
			wantErr: "token-domain must be a valid DNS name",
		},
		{
			name:    "public suffix",
			domain:  "com",
			wantErr: "token-domain must include a registrable domain above its suffix",
		},
		{
			name:    "multi-label public suffix",
			domain:  "co.uk",
			wantErr: "token-domain must include a registrable domain above its suffix",
		},
		{
			name:    "private suffix",
			domain:  "github.io",
			wantErr: "token-domain must include a registrable domain above its suffix",
		},
		{
			name:    "uppercase suffix",
			domain:  "COM",
			wantErr: "token-domain must include a registrable domain above its suffix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domains := []string{tt.domain}
			assert.ErrorContains(t, validateTokenDomains(&domains), tt.wantErr)
		})
	}
}

func TestValidateTokenDomainBoundariesAndPrivateRegistrableDomain(t *testing.T) {
	domains := []string{
		strings.Join([]string{
			strings.Repeat("a", 63),
			strings.Repeat("b", 63),
			strings.Repeat("c", 63),
			strings.Repeat("d", 57),
			"com",
		}, "."),
		"User.GitHub.io",
	}
	require.NoError(t, validateTokenDomains(&domains))
}
