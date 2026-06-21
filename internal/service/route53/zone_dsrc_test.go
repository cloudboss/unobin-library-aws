package route53

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeDomainName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "root", in: ".", want: "."},
		{name: "lowercase and trim one trailing dot", in: "Example.COM.", want: "example.com"},
		{name: "trim only one trailing dot", in: "Example.COM..", want: "example.com."},
		{name: "keep existing octal escape", in: `\052.Example.COM.`, want: `\052.example.com`},
		{name: "escape wildcard", in: "*.Example.COM.", want: `\052.example.com`},
		{name: "preserve underscore", in: "_Sip._TCP.Example.COM.", want: `_sip._tcp.example.com`},
		{name: "escape space", in: "hello world.example.com", want: `hello\040world.example.com`},
		{name: "escape non-ascii rune", in: "é.example.com", want: `\351.example.com`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeDomainName(tt.in))
		})
	}
}

func TestZoneARN(t *testing.T) {
	assert.Equal(t, "arn:aws:route53:::hostedzone/Z123", zoneARN("us-east-1", "Z123"))
}

func TestUserTags(t *testing.T) {
	tags := userTags(map[string]string{
		"Name":        "example",
		"aws:service": "route53",
	})

	assert.Equal(t, map[string]string{"Name": "example"}, tags)
}

func TestTagsContainAll(t *testing.T) {
	tests := []struct {
		name   string
		actual map[string]string
		want   map[string]string
		ok     bool
	}{
		{
			name:   "subset",
			actual: map[string]string{"env": "test", "team": "dns"},
			want:   map[string]string{"env": "test"},
			ok:     true,
		},
		{
			name:   "missing key",
			actual: map[string]string{"env": "test"},
			want:   map[string]string{"team": "dns"},
			ok:     false,
		},
		{
			name:   "empty value needs the key",
			actual: nil,
			want:   map[string]string{"env": ""},
			ok:     false,
		},
		{
			name:   "different value",
			actual: map[string]string{"env": "test"},
			want:   map[string]string{"env": "prod"},
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.ok, tagsContainAll(tt.actual, tt.want))
		})
	}
}
