package ec2

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/require"
)

func TestPeeringAccepterCompileInputsAndOutputs(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(local, "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/peering-accepter/compile",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestPeeringAccepterBuildScenario(t *testing.T) {
	module, unobin := libraryModuleRoot(t), unobinModuleRoot(t)
	err := compile.Run(compile.Options{
		FactoryPath: "../../tests/integration/scenarios/ec2-vpc-peering-connection-accepter/factory.ub",
		Build:       true, GoVersion: "1.26.2", OutDir: t.TempDir(),
		CLIVersion: "v0.13.0", ReplaceUnobin: unobin.Dir,
		ReplaceGoModules: map[string]string{module.Path: module.Dir},
		NewResolver: func(directory string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(directory), nil
		},
		Stdout: io.Discard, Stderr: io.Discard,
	})
	require.NoError(t, err)
}
