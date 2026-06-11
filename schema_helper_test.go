package library_test

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

// unobinModulePath is the module whose source holds awscfg.Configuration,
// the type this library registers as its configuration.
const unobinModulePath = "github.com/cloudboss/unobin"

// unobinModuleRoot locates the on-disk source of the unobin version this
// library is built against, computed once for the whole test binary. The
// dev CLI reads the awscfg.Configuration fields from that source when it
// extracts the library schema; the tests hand goschema the same root so
// the configuration resolves the same way a factory compile does.
var unobinModuleRoot = sync.OnceValues(func() (goschema.ModuleRoot, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", unobinModulePath).Output()
	if err != nil {
		return goschema.ModuleRoot{}, fmt.Errorf("locate unobin source: %w", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return goschema.ModuleRoot{}, fmt.Errorf("unobin module source not found on disk")
	}
	return goschema.ModuleRoot{Path: unobinModulePath, Dir: dir}, nil
})

// readLibrarySchema extracts the library schema the way the dev CLI does
// when it compiles a factory: goschema reads this library's own source for
// resource, data source, and action types, and the unobin module root for
// the awscfg.Configuration fields. It asserts a clean read -- no error and
// no warnings -- and returns the schema.
func readLibrarySchema(t *testing.T) *runtime.LibrarySchema {
	t.Helper()
	root, err := unobinModuleRoot()
	require.NoError(t, err)
	schema, warnings, err := goschema.Read(".", root)
	require.NoError(t, err)
	require.Empty(t, warnings)
	return schema
}
