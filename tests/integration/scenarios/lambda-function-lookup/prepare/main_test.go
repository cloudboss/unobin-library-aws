package main

import (
	"archive/zip"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepare(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "assets")
	require.NoError(t, prepare(directory))
	archive, err := zip.OpenReader(filepath.Join(directory, "function.zip"))
	require.NoError(t, err)
	defer func() { require.NoError(t, archive.Close()) }()
	require.Len(t, archive.File, 1)
	require.Equal(t, "index.py", archive.File[0].Name)
	file, err := archive.File[0].Open()
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, "def handler(event, context):\n    return event\n", string(data))
}
