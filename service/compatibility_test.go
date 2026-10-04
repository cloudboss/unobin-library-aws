package service_test

import (
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/golibrary"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestLibraryCompatibility(t *testing.T) {
	moduleRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	want := runtime.LibraryCompatibility{
		RequiredAPI:            "1.0",
		SuggestedUnobinVersion: "v0.12.0",
	}
	for name, lib := range libraries() {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, want, lib.Compatibility)

			dir := filepath.Join(moduleRoot, "service", name)
			if name == "meta" {
				dir = filepath.Join(moduleRoot, name)
			}
			declaration, err := golibrary.ReadCompatibility(moduleRoot, dir)
			require.NoError(t, err)
			require.Equal(t, want.RequiredAPI, declaration.RequiredAPI)
			require.Equal(t, want.SuggestedUnobinVersion, declaration.SuggestedUnobinVersion)
			require.NoError(t, libraryapi.Check(declaration.RequiredAPI, libraryapi.Current()))
		})
	}
}

func TestCompiledLibraryCompatibility(t *testing.T) {
	e2etest.RunCompiledCases(t, "testdata/ub/valid/compiled",
		e2etest.WithGoModule("github.com/cloudboss/unobin-library-aws", ".."),
	)
}
