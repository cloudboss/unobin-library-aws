package ec2

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/require"
)

func TestPeeringAccepterCompileInputsAndOutputs(t *testing.T) {
	module, unobin := libraryModuleRoot(t), unobinModuleRoot(t)
	for _, tc := range []struct{ name, diagnostic string }{
		{"valid", ""}, {"scenario", ""},
		{"missing", "vpc-peering-connection-id"},
		{"null", "expected string, got null"},
		{"wrong-id", "type mismatch"}, {"wrong-auto", "type mismatch"},
		{"wrong-accepter", "type mismatch"}, {"wrong-requester", "type mismatch"},
		{"wrong-tags", "type mismatch"}, {"output-only", "peer-region"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata/ub/peering-accepter/compile", tc.name, "factory.ub")
			if tc.name == "scenario" {
				path = "../../tests/integration/scenarios/ec2-vpc-peering-connection-accepter/factory.ub"
			}
			out := "-"
			if tc.name == "scenario" {
				out = t.TempDir()
			}
			err := compile.Run(compile.Options{
				FactoryPath: path, Build: tc.name == "scenario", GoVersion: "1.26.2",
				OutDir: out, CLIVersion: "v0.13.0-a.4", ReplaceUnobin: unobin.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(directory string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(directory), nil
				},
				Stdout: io.Discard, Stderr: io.Discard,
			})
			if tc.diagnostic == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.diagnostic)
			}
		})
	}
}
