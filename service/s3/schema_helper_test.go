package s3_test

import (
	"regexp"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
)

func assertTypeSchemaEqual(t *testing.T, want, got *runtime.TypeSchema, msgAndArgs ...any) {
	t.Helper()
	assert.Equal(t, normalizeTypeSchema(want), normalizeTypeSchema(got), msgAndArgs...)
}

func normalizeTypeSchema(in *runtime.TypeSchema) *runtime.TypeSchema {
	if in == nil {
		return nil
	}
	out := *in
	out.Constraints = normalizeConstraintSpecs(in.Constraints)
	return &out
}

func normalizeConstraintSpecs(in []lang.ConstraintSpec) []lang.ConstraintSpec {
	if in == nil {
		return nil
	}
	out := make([]lang.ConstraintSpec, len(in))
	for i, c := range in {
		out[i] = c
		out[i].When = normalizeConstraintExpr(c.When)
		out[i].Require = normalizeConstraintExpr(c.Require)
		out[i].ForEach = normalizeConstraintExpr(c.ForEach)
		if len(c.ForEachLevels) > 0 {
			out[i].ForEachLevels = append([]lang.ForEachSpecLevel(nil), c.ForEachLevels...)
			for j := range out[i].ForEachLevels {
				out[i].ForEachLevels[j].In = normalizeConstraintExpr(out[i].ForEachLevels[j].In)
			}
		}
	}
	return out
}

var (
	constraintFallbackPattern = regexp.MustCompile(`\s+\?\?\s+(\[\]|\{\}|'')`)
	constraintPresentPattern  = regexp.MustCompile(
		`\(\(([^()]+) != null\) && \(@core\.length\(([^()]+)\) >= 1\)\)`)
	constraintAbsentPattern = regexp.MustCompile(
		`!\(\(([^()]+) != null\) && \(@core\.length\(([^()]+)\) >= 1\)\)`)
	constraintLowerPattern = regexp.MustCompile(
		`([@a-zA-Z0-9_.-]+) == null \|\| ([@a-zA-Z0-9_.-]+) >= ([^)]+)`)
	constraintUpperPattern = regexp.MustCompile(
		`([@a-zA-Z0-9_.-]+) == null \|\| ([@a-zA-Z0-9_.-]+) <= ([^)]+)`)
	constraintAbovePattern = regexp.MustCompile(
		`([@a-zA-Z0-9_.-]+) == null \|\| ([@a-zA-Z0-9_.-]+) > ([^)]+)`)
	constraintBelowPattern = regexp.MustCompile(
		`([@a-zA-Z0-9_.-]+) == null \|\| ([@a-zA-Z0-9_.-]+) < ([^)]+)`)
	constraintLengthLowerPattern = regexp.MustCompile(
		`([@a-zA-Z0-9_.-]+) == null \|\| @core\.length\(([@a-zA-Z0-9_.-]+)\) >= ([^)]+)`)
	constraintLengthUpperPattern = regexp.MustCompile(
		`([@a-zA-Z0-9_.-]+) == null \|\| @core\.length\(([@a-zA-Z0-9_.-]+)\) <= ([^)]+)`)
)

func normalizeConstraintExpr(in string) string {
	out := constraintFallbackPattern.ReplaceAllString(in, "")
	out = replaceMatchingFields(out, constraintAbsentPattern,
		func(field string) string { return "!(@core.length(" + field + ") >= 1)" })
	out = replaceMatchingFields(out, constraintPresentPattern,
		func(field string) string { return "(@core.length(" + field + ") >= 1)" })
	out = replaceMatchingBounds(out, constraintLengthLowerPattern, "@core.length", ">=")
	out = replaceMatchingBounds(out, constraintLengthUpperPattern, "@core.length", "<=")
	out = replaceMatchingBounds(out, constraintLowerPattern, "", ">=")
	out = replaceMatchingBounds(out, constraintUpperPattern, "", "<=")
	out = replaceMatchingBounds(out, constraintAbovePattern, "", ">")
	out = replaceMatchingBounds(out, constraintBelowPattern, "", "<")
	return out
}

func replaceMatchingFields(
	in string, pattern *regexp.Regexp, replacement func(string) string,
) string {
	return pattern.ReplaceAllStringFunc(in, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) != 3 || parts[1] != parts[2] {
			return match
		}
		return replacement(parts[1])
	})
}

func replaceMatchingBounds(in string, pattern *regexp.Regexp, fn, op string) string {
	return pattern.ReplaceAllStringFunc(in, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) != 4 || parts[1] != parts[2] {
			return match
		}
		field := parts[1]
		if fn != "" {
			field = fn + "(" + field + ")"
		}
		return field + " " + op + " " + parts[3]
	})
}
