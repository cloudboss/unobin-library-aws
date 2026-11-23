package ec2

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/stretchr/testify/require"
)

const peeringFixtures = "testdata/ub/peering"

func TestPeeringCompileRejectsInvalidInputs(t *testing.T) {
	replacements := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(replacements, "library"), 0o755))
	target := filepath.Join(replacements, "library", "target")
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, target))
	e2etest.RunSourceCases(t, "testdata/ub/invalid/source",
		e2etest.WithSourceDirectory("replacements", replacements),
	)
}
