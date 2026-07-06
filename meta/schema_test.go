package meta

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/meta"
)

func TestLibraryRegistersMetaPartition(t *testing.T) {
	lib := Library()
	require.Contains(t, lib.DataSources, "partition")
	assert.Equal(t, reflect.TypeFor[*svc.PartitionDataSourceOutput](),
		lib.DataSources["partition"].OutputType())
}

func TestMetaPartitionSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Contains(t, schema.DataSources, "partition")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{},
		Outputs: map[string]typecheck.Type{
			"dns-suffix":         typecheck.TString(),
			"partition":          typecheck.TString(),
			"reverse-dns-prefix": typecheck.TString(),
		},
	}
	assertTypeSchemaEqual(t, want, schema.DataSources["partition"])
}

func TestLibraryRegistersMetaRegion(t *testing.T) {
	lib := Library()
	require.Contains(t, lib.DataSources, "region")
	assert.Equal(t, reflect.TypeFor[*svc.RegionDataSourceOutput](),
		lib.DataSources["region"].OutputType())
}

func TestMetaRegionSchema(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Contains(t, schema.DataSources, "region")
	want := &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"endpoint": typecheck.TOptional(typecheck.TString()),
			"region":   typecheck.TOptional(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"description": typecheck.TString(),
			"endpoint":    typecheck.TString(),
			"partition":   typecheck.TString(),
			"region":      typecheck.TString(),
		},
	}
	assertTypeSchemaEqual(t, want, schema.DataSources["region"])
}
