package testutil

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func CheckConstraints(
	t *testing.T, schema *runtime.TypeSchema, inputs map[string]any,
) []string {
	t.Helper()
	entries, errs := lang.ParseSpecs(schema.Constraints)
	require.Empty(t, errs.Strings())
	eval := func(expr lang.Expr, bindings []lang.EachBinding) (any, error) {
		ctx := &runtime.EvalContext{Inputs: inputs, MissingAsNull: true}
		runtime.ApplyBindings(ctx, bindings)
		return runtime.Eval(expr, ctx)
	}
	return lang.CheckConstraintEntries(entries, inputs, eval, lang.DisplayNodeRelative).Strings()
}
