package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixtureDescriptor struct {
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
	Digest    string `json:"digest"`
}

func TestBuildFixtureImageIncludesOneRealLayer(t *testing.T) {
	image, err := buildFixtureImage("first", "2024-01-01T00:00:00Z")
	require.NoError(t, err)

	var manifest struct {
		SchemaVersion int                 `json:"schemaVersion"`
		MediaType     string              `json:"mediaType"`
		Config        fixtureDescriptor   `json:"config"`
		Layers        []fixtureDescriptor `json:"layers"`
	}
	require.NoError(t, json.Unmarshal([]byte(image.manifest), &manifest))
	assert.Equal(t, 2, manifest.SchemaVersion)
	assert.Equal(t,
		"application/vnd.docker.distribution.manifest.v2+json",
		manifest.MediaType)
	assert.Equal(t, fixtureDescriptor{
		MediaType: "application/vnd.docker.container.image.v1+json",
		Size:      len(image.configBody),
		Digest:    fixtureDigest(image.configBody),
	}, manifest.Config)
	assert.Equal(t, []fixtureDescriptor{
		{
			MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			Size:      len(image.layerBody),
			Digest:    fixtureDigest(image.layerBody),
		},
	}, manifest.Layers)

	rawLayer := decompressFixtureLayer(t, image.layerBody)
	reader := tar.NewReader(bytes.NewReader(rawLayer))
	header, err := reader.Next()
	require.NoError(t, err)
	assert.Equal(t, "fixture.txt", header.Name)
	assert.Equal(t, int64(0o644), header.Mode)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("first\n"), content)
	_, err = reader.Next()
	require.ErrorIs(t, err, io.EOF)

	var config struct {
		Architecture string `json:"architecture"`
		Created      string `json:"created"`
		OS           string `json:"os"`
		Config       struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
		RootFS struct {
			Type    string   `json:"type"`
			DiffIDs []string `json:"diff_ids"`
		} `json:"rootfs"`
	}
	require.NoError(t, json.Unmarshal(image.configBody, &config))
	assert.Equal(t, "amd64", config.Architecture)
	assert.Equal(t, "2024-01-01T00:00:00Z", config.Created)
	assert.Equal(t, "linux", config.OS)
	assert.Equal(t, map[string]string{"fixture": "first"}, config.Config.Labels)
	assert.Equal(t, "layers", config.RootFS.Type)
	assert.Equal(t, []string{fixtureDigest(rawLayer)}, config.RootFS.DiffIDs)
}

func decompressFixtureLayer(t *testing.T, body []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(body))
	require.NoError(t, err)
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return raw
}

func fixtureDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
