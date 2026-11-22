package dynamodb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/testutil"
)

func TestTableKeyAttributeConstraints(t *testing.T) {
	schema := readLibrarySchema(t).Resources["table"]
	attribute := func(name string) any { return map[string]any{"name": name, "type": "S"} }
	inputs := map[string]any{
		"name":         "orders",
		"billing-mode": "PAY_PER_REQUEST",
		"hash-key":     "pk",
		"attribute":    []any{attribute("pk")},
	}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	delete(inputs, "attribute")
	errs := testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "hash-key must name a declared attribute")

	inputs["range-key"] = "sk"
	inputs["local-secondary-index"] = []any{map[string]any{
		"name": "by-local", "range-key": "lsi", "projection-type": "ALL",
	}}
	inputs["global-secondary-index"] = []any{map[string]any{
		"name": "by-global", "hash-key": "gpk", "range-key": "gsk", "projection-type": "ALL",
	}}
	inputs["attribute"] = []any{
		attribute("pk"), attribute("sk"), attribute("lsi"), attribute("gpk"), attribute("gsk"),
	}
	require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
	inputs["attribute"] = []any{attribute("pk"), attribute("extra")}
	errs = testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errs, 5)
	require.Contains(t, errs[0], "range-key must name a declared attribute")
	require.Contains(t, errs[1], "attribute must be used by a table or index key")
	require.Contains(t, errs[2], "local-secondary-index range-key must name a declared attribute")
	require.Contains(t, errs[3], "global-secondary-index hash-key must name a declared attribute")
	require.Contains(t, errs[4], "global-secondary-index range-key must name a declared attribute")
}
