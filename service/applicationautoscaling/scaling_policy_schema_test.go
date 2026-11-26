package applicationautoscaling_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/stretchr/testify/require"
)

func TestScalingPolicyCompile(t *testing.T) {
	modules := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(modules, "local"), 0o700))
	require.NoError(t, os.Symlink(moduleRoot(t, "").Dir, filepath.Join(modules, "local", "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/policy/compile",
		e2etest.WithSourceDirectory("modules", modules))
}
