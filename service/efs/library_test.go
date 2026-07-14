package efs

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/efs"
)

func TestLibraryRegistersResources(t *testing.T) {
	lib := Library()
	require.Equal(t, "aws-efs", lib.Name)
	require.NotNil(t, lib.Configuration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), lib.Configuration.ValueType())
	require.Len(t, lib.Resources, 3)
	require.Contains(t, lib.Resources, "access-point")
	assert.Equal(t, reflect.TypeFor[*svc.AccessPointResourceOutput](),
		lib.Resources["access-point"].OutputType())
	require.Contains(t, lib.Resources, "file-system")
	assert.Equal(t, reflect.TypeFor[*svc.FileSystemResourceOutput](),
		lib.Resources["file-system"].OutputType())
	require.Contains(t, lib.Resources, "mount-target")
	assert.Equal(t, reflect.TypeFor[*svc.MountTargetResourceOutput](),
		lib.Resources["mount-target"].OutputType())
	assert.Empty(t, lib.DataSources)
	assert.Empty(t, lib.Actions)
}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(Library().Configuration)
	require.NoError(t, err)
	assert.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	assert.NotEmpty(t, view.SchemaDigest)
}
