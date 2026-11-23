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

	for name, lib := range libraries() {
		t.Run(name, func(t *testing.T) {
			want := runtime.LibraryCompatibility{
				RequiredAPI:            "1.0",
				SuggestedUnobinVersion: "v0.12.0",
			}
			switch name {
			case "cloudwatch", "dynamodb", "ec2", "eks", "eventbridge",
				"iam", "lambda", "scheduler", "sns", "sqs", "wafv2":
				want = runtime.LibraryCompatibility{
					RequiredAPI:            "1.1",
					SuggestedUnobinVersion: "v0.13.0-a.4",
				}
			}
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
