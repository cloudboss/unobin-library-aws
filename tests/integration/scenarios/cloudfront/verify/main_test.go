package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSamePathMultiset(t *testing.T) {
	tests := []struct {
		name  string
		got   []string
		want  []string
		equal bool
	}{
		{
			name:  "different response order",
			got:   []string{"/index.html", "/*", "/"},
			want:  []string{"/", "/index.html", "/*"},
			equal: true,
		},
		{
			name:  "different path",
			got:   []string{"/", "/index.html", "/missing"},
			want:  []string{"/", "/index.html", "/*"},
			equal: false,
		},
		{
			name:  "different duplicate count",
			got:   []string{"/", "/", "/*"},
			want:  []string{"/", "/index.html", "/*"},
			equal: false,
		},
		{
			name:  "different length",
			got:   []string{"/", "/*"},
			want:  []string{"/", "/index.html", "/*"},
			equal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.equal, samePathMultiset(tt.got, tt.want))
		})
	}
}
