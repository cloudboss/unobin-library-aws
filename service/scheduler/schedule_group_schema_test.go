package scheduler_test

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/require"
)

func scheduleGroupFixture(name string) string {
	return filepath.Join("testdata", "ub", "schedule-group", name)
}

func TestScheduleGroupCompile(t *testing.T) {
	module := moduleRoot(t, ".")
	core := unobinRoot(t)
	for _, test := range []struct {
		fixture string
		want    []string
	}{
		{fixture: "valid"},
		{fixture: "rules", want: []string{
			"resource.both: constraints[0] (at-most-one-of [name, name-prefix])",
			"resource.empty-name: constraints[1] (predicate)",
			"resource.invalid-name: constraints[1] (predicate)",
			"resource.overflow-name: constraints[1] (predicate)",
			"resource.empty-prefix: constraints[2] (predicate)",
			"resource.invalid-prefix: constraints[2] (predicate)",
			"resource.overflow-prefix: constraints[2] (predicate)",
		}},
		{fixture: "types", want: []string{
			"type mismatch: expected optional(string), got integer",
			"type mismatch: expected optional(string), got boolean",
			"type mismatch: expected optional(map(string)), got list(string)",
			"type mismatch: expected optional(map(string)), got object({ tag: integer })",
		}},
	} {
		t.Run(test.fixture, func(t *testing.T) {
			err := compile.Run(compile.Options{
				FactoryPath:      scheduleGroupFixture(test.fixture),
				OutDir:           "-",
				CLIVersion:       "v0.13.0-a.4",
				ReplaceUnobin:    core.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(project string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(project), nil
				},
				Stdout: io.Discard,
				Stderr: io.Discard,
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
