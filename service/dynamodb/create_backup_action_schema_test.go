package dynamodb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/stretchr/testify/require"
)

func backupFixture(name string) string {
	return filepath.Join("testdata", "ub", "create-backup", name, "factory.ub")
}

func TestCreateBackupCompile(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(local, "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/create-backup",
		e2etest.WithSourceDirectory("modules", modules))
}
