package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindFileSystemIgnoresDeletedEntries(t *testing.T) {
	tagRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2015-02-01/file-systems":
			_, _ = w.Write([]byte(`{"FileSystems":[{` +
				`"CreationTime":0,"CreationToken":"token",` +
				`"FileSystemId":"fs-01234567","LifeCycleState":"deleted",` +
				`"NumberOfMountTargets":0,"OwnerId":"123456789012",` +
				`"PerformanceMode":"generalPurpose",` +
				`"SizeInBytes":{"Value":0}}]}`))
		case "/2015-02-01/resource-tags/fs-01234567":
			tagRequests++
			_, _ = w.Write([]byte(
				`{"Tags":[{"Key":"Name","Value":"unobin-it-efs"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := efs.NewFromConfig(aws.Config{
		Region:       "us-east-1",
		Credentials:  aws.AnonymousCredentials{},
		BaseEndpoint: aws.String(server.URL),
		HTTPClient:   server.Client(),
	})

	fileSystem, tags, err := findFileSystem(context.Background(), client)

	require.NoError(t, err)
	assert.Nil(t, fileSystem)
	assert.Nil(t, tags)
	assert.Zero(t, tagRequests)
}
