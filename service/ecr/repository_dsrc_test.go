package ecr

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/stretchr/testify/require"
)

func TestRepositoryLookupCompile(t *testing.T) {
	module := libraryModuleRoot(t)
	core := unobinModuleRoot(t)
	for _, tc := range []struct {
		fixture string
		want    []string
	}{
		{fixture: "valid"},
		{fixture: "missing", want: []string{"missing required input", "name"}},
		{fixture: "null", want: []string{"expected string, got null"}},
		{fixture: "wrong-type", want: []string{"expected string, got integer"}},
		{fixture: "aliases", want: []string{
			"region", "id", "tags-all", "repository-url", "image-tag-mutability-exclusion-filter",
		}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			err := compile.Run(compile.Options{
				FactoryPath: filepath.Join("testdata", "ub", "repository-lookup", tc.fixture),
				OutDir:      t.TempDir(), GoVersion: compile.GoMajorMinor(), CLIVersion: "dev",
				ReplaceUnobin:    core.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				Build:            tc.fixture == "valid",
				Stdout:           io.Discard, Stderr: io.Discard,
			})
			if len(tc.want) == 0 {
				require.NoError(t, err)
				return
			}
			for _, want := range tc.want {
				require.ErrorContains(t, err, want)
			}
		})
	}
}
