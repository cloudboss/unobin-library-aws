package sfn_test

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/stretchr/testify/require"
)

const compileStartJSONRule = "schema: action.start: constraints[0] (predicate): " +
	"input must be valid JSON"

func startExecutionFixture(name string) string {
	return filepath.Join("testdata", "ub", "start-execution", name, "factory.ub")
}

func TestStartExecutionCompile(t *testing.T) {
	module := moduleRoot(t, ".")
	unobin := unobinRoot(t)
	for _, test := range []struct {
		fixture string
		want    []string
	}{
		{fixture: "literal"},
		{fixture: "settled"},
		{
			fixture: "invalid/missing-arn",
			want:    []string{"required input", "state-machine-arn"},
		},
		{
			fixture: "invalid/null-arn",
			want:    []string{"type mismatch: expected string, got null"},
		},
		{
			fixture: "invalid/nullable-arn",
			want:    []string{"type mismatch: expected string, got optional(string)"},
		},
		{
			fixture: "invalid/wrong-arn-type",
			want:    []string{"type mismatch: expected string, got integer"},
		},
		{
			fixture: "invalid/unexpected-input",
			want:    []string{"payload", "input"},
		},
		{
			fixture: "invalid/wrong-input-type",
			want:    []string{"type mismatch: expected optional(string), got boolean"},
		},
		{fixture: "invalid/empty-json", want: []string{compileStartJSONRule}},
		{fixture: "invalid/whitespace-json", want: []string{compileStartJSONRule}},
		{fixture: "invalid/malformed-json", want: []string{compileStartJSONRule}},
		{fixture: "invalid/trailing-json", want: []string{compileStartJSONRule}},
	} {
		t.Run(test.fixture, func(t *testing.T) {
			err := compile.Run(compile.Options{
				FactoryPath:      startExecutionFixture(test.fixture),
				OutDir:           t.TempDir(),
				GoVersion:        compile.GoMajorMinor(),
				CLIVersion:       "dev",
				ReplaceUnobin:    unobin.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				Stdout:           io.Discard,
				Stderr:           io.Discard,
			})
			if len(test.want) == 0 {
				require.NoError(t, err)
				return
			}
			for _, diagnostic := range test.want {
				require.ErrorContains(t, err, diagnostic)
			}
		})
	}
}
