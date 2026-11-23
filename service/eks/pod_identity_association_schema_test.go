package eks_test

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/require"
)

func TestPodIdentityAssociationInvalidLiterals(t *testing.T) {
	module := moduleRoot(t, ".")
	core := unobinRoot(t)
	for _, tc := range []struct {
		fixture string
		errors  []string
	}{
		{fixture: "required", errors: []string{
			`missing required input "cluster-name" on aws-eks.pod-identity-association`,
			`missing required input "namespace" on aws-eks.pod-identity-association`,
			`missing required input "service-account" on aws-eks.pod-identity-association`,
			`missing required input "role-arn" on aws-eks.pod-identity-association`,
		}},
		{fixture: "rules", errors: []string{
			`resource.names: constraints[0] (predicate): cluster-name must be a nonempty string`,
			`resource.names: constraints[1] (predicate): namespace must be a nonempty string`,
			`resource.names: constraints[2] (predicate): service-account must be a nonempty string`,
			`resource.roles: constraints[4] (predicate): role-arn must be an ARN`,
			`resource.roles: constraints[5] (predicate): target-role-arn must be an ARN`,
		}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			options := compile.Options{
				FactoryPath:      filepath.Join("testdata", "ub", "pod-identity-association", tc.fixture),
				OutDir:           "-",
				StackName:        "pod-identity-association-schema",
				CLIVersion:       "v0.13.0-a.4",
				ReplaceUnobin:    core.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(project string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(project), nil
				},
				Stdout: io.Discard,
				Stderr: io.Discard,
			}
			err := compile.Run(options)
			for _, want := range tc.errors {
				require.ErrorContains(t, err, want)
			}
		})
	}
}
