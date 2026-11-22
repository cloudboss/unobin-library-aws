package ec2

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/testutil"
)

func TestAMIInputConstraints(t *testing.T) {
	schema := readLibrarySchema(t).DataSources["ami"]
	for _, tt := range []struct {
		name   string
		inputs map[string]any
		error  string
	}{
		{"omitted options", nil, ""},
		{"invalid regex", map[string]any{"name-regex": "["}, "valid regular expression"},
		{"empty owner", map[string]any{"owners": []any{"self", ""}}, "empty value"},
		{"missing scope", map[string]any{"most-recent": true}, "most-recent requires owners"},
		{"unsafe override", map[string]any{"most-recent": true, "allow-unsafe-filter": true}, ""},
		{"explicit IDs", map[string]any{"most-recent": true, "image-ids": []any{"ami-abc"}}, ""},
		{"explicit owner", map[string]any{"most-recent": true, "owners": []any{"self"}}, ""},
		{"owner filter", map[string]any{
			"most-recent": true,
			"filters":     []any{map[string]any{"name": "owner-id", "values": []any{"123"}}},
		}, ""},
		{"image filter", map[string]any{
			"most-recent": true,
			"filters":     []any{map[string]any{"name": "image-id", "values": []any{"ami-abc"}}},
		}, ""},
		{"alias filter", map[string]any{
			"most-recent": true,
			"filters":     []any{map[string]any{"name": "owner-alias", "values": []any{"amazon"}}},
		}, "most-recent requires owners"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			errs := testutil.CheckConstraints(t, schema, tt.inputs)
			if tt.error == "" {
				require.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1)
			require.Contains(t, errs[0], tt.error)
		})
	}
}
