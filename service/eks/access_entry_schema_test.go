package eks_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"

	awseks "github.com/cloudboss/unobin-library-aws/service/eks"
)

func accessEntryFixture(t *testing.T, name string) syntax.FactoryBody {
	t.Helper()
	path := filepath.Join("testdata", "ub", "access-entry", name, "factory.ub")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, data)
	require.NoError(t, err)
	return file.Factory.Body
}

func accessEntryLibrary(t *testing.T) *runtime.Library {
	t.Helper()
	library := runtime.LibraryWithPath(awseks.Library(),
		"github.com/cloudboss/unobin-library-aws//service/eks")
	library.Schema = readLibrarySchema(t)
	library.Constraints = make(map[string][]lang.ConstraintSpec)
	library.Defaults = make(map[string][]lang.DefaultSpec)
	for export, schema := range library.Schema.Resources {
		library.Constraints["resource."+export] = schema.Constraints
		library.Defaults["resource."+export] = schema.Defaults
	}
	return library
}

func TestAccessEntryFactoryCompilation(t *testing.T) {
	module := moduleRoot(t, ".")
	core := unobinRoot(t)
	for _, tc := range []struct {
		fixture string
		errors  []string
	}{
		{fixture: "evaluated-identity"},
		{fixture: "deferred"},
		{fixture: "required", errors: []string{
			`missing required input "cluster-name" on aws-eks.access-entry`,
			`missing required input "principal-arn" on aws-eks.access-entry`,
			"14:22: type: type mismatch: expected string, got integer",
			"19:22: type: type mismatch: expected string, got integer",
			"24:27: type: type mismatch: expected string, got integer",
			"29:27: type: type mismatch: expected string, got integer",
		}},
		{fixture: "rules", errors: []string{
			"resource.empty-name: constraints[0]", "resource.leading-underscore: constraints[0]",
			"resource.long-name: constraints[0]", "resource.lower-type: constraints[1]",
			"resource.empty-type: constraints[1]", "resource.new-type: constraints[1]",
			"resource.empty-principal: constraints[3] (predicate): principal-arn must be a nonempty string",
		}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			options := compile.Options{
				FactoryPath:      filepath.Join("testdata", "ub", "access-entry", tc.fixture),
				OutDir:           "-",
				StackName:        "access-entry-schema",
				CLIVersion:       "v0.13.0",
				ReplaceUnobin:    core.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(project string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(project), nil
				},
				Stdout: io.Discard,
				Stderr: io.Discard,
			}
			err := compile.Run(options)
			if len(tc.errors) == 0 {
				require.NoError(t, err)
				return
			}
			for _, want := range tc.errors {
				require.ErrorContains(t, err, want)
			}
		})
	}
}
