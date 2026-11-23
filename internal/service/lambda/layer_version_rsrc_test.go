package lambda

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const layerVersionARN = "arn:aws:lambda:us-east-1:123456789012:layer:observed:4294967298"

type fakeLayerVersionClient struct {
	ctx      context.Context
	requests []any

	published   *lambda.PublishLayerVersionOutput
	publishErr  error
	observation *lambda.GetLayerVersionOutput
	getErr      error
	deleteErr   error
}

func (c *fakeLayerVersionClient) PublishLayerVersion(
	ctx context.Context, in *lambda.PublishLayerVersionInput, _ ...func(*lambda.Options),
) (*lambda.PublishLayerVersionOutput, error) {
	c.ctx = ctx
	c.requests = append(c.requests, in)
	return c.published, c.publishErr
}

func (c *fakeLayerVersionClient) GetLayerVersion(
	ctx context.Context, in *lambda.GetLayerVersionInput, _ ...func(*lambda.Options),
) (*lambda.GetLayerVersionOutput, error) {
	c.ctx = ctx
	c.requests = append(c.requests, in)
	return c.observation, c.getErr
}

func (c *fakeLayerVersionClient) DeleteLayerVersion(
	ctx context.Context, in *lambda.DeleteLayerVersionInput, _ ...func(*lambda.Options),
) (*lambda.DeleteLayerVersionOutput, error) {
	c.ctx = ctx
	c.requests = append(c.requests, in)
	return nil, c.deleteErr
}

func (c *fakeLayerVersionClient) Options() lambda.Options {
	return lambda.Options{Region: "us-east-1"}
}

func TestLayerVersionPublishSources(t *testing.T) {
	home := t.TempDir()
	setLayerHome(t, home)
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	entry, err := archive.Create("python/canary.txt")
	require.NoError(t, err)
	_, err = io.WriteString(entry, "local layer")
	require.NoError(t, err)
	require.NoError(t, archive.Close())
	path := filepath.Join(home, "layer.zip")
	require.NoError(t, os.WriteFile(path, data.Bytes(), 0o600))

	unpinned := LayerVersionContent{S3Bucket: aws.String("bucket"), S3Key: aws.String("layer.zip")}
	unpinnedRequest := &lambdatypes.LayerVersionContentInput{
		S3Bucket: aws.String("bucket"), S3Key: aws.String("layer.zip"),
	}
	for _, tc := range []struct {
		name     string
		content  LayerVersionContent
		expected *lambdatypes.LayerVersionContentInput
		explicit bool
	}{
		{
			"local", LayerVersionContent{ZipFilePath: &path},
			&lambdatypes.LayerVersionContentInput{ZipFile: data.Bytes()}, false,
		},
		{
			"home-relative",
			LayerVersionContent{ZipFilePath: aws.String(filepath.Join("~", "layer.zip"))},
			&lambdatypes.LayerVersionContentInput{ZipFile: data.Bytes()}, false,
		},
		{"s3", unpinned, unpinnedRequest, false},
		{
			"empty-s3-version",
			LayerVersionContent{
				S3Bucket: unpinned.S3Bucket, S3Key: unpinned.S3Key, S3ObjectVersion: aws.String(""),
			},
			unpinnedRequest, true,
		},
		{
			"pinned-s3",
			LayerVersionContent{
				S3Bucket: unpinned.S3Bucket, S3Key: unpinned.S3Key, S3ObjectVersion: aws.String("v1"),
			},
			&lambdatypes.LayerVersionContentInput{
				S3Bucket: unpinned.S3Bucket, S3Key: unpinned.S3Key, S3ObjectVersion: aws.String("v1"),
			},
			true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeLayerVersionClient{
				published: &lambda.PublishLayerVersionOutput{
					LayerVersionArn: aws.String(layerVersionARN),
					Version:         1,
				},
				observation: &lambda.GetLayerVersionOutput{
					LayerVersionArn: aws.String(layerVersionARN),
					LayerArn: aws.String(
						"arn:aws:lambda:us-east-1:123456789012:layer:observed",
					),
					Content: &lambdatypes.LayerVersionContentOutput{
						CodeSha256: aws.String("observed checksum"),
					},
					Description: aws.String("observed description"),
					LicenseInfo: aws.String("observed license"),
				},
			}
			r := LayerVersionResource{
				LayerName: "desired", Content: tc.content,
				clientFor: func(context.Context, *awsCfg) (layerVersionClient, error) {
					return client, nil
				},
			}
			if tc.explicit {
				emptyValues := []string{}
				r.Description, r.LicenseInfo = aws.String(""), aws.String("")
				r.CompatibleArchitectures, r.CompatibleRuntimes = &emptyValues, &emptyValues
			}
			out, err := r.Create(t.Context(), nil)
			require.NoError(t, err)
			assert.Same(t, t.Context(), client.ctx)
			assert.Equal(t, []any{
				&lambda.PublishLayerVersionInput{
					LayerName: aws.String("desired"), Content: tc.expected,
					Description: aws.String(""), LicenseInfo: aws.String(""),
				},
				&lambda.GetLayerVersionInput{
					LayerName: aws.String("observed"), VersionNumber: aws.Int64(4294967298),
				},
			}, client.requests)
			assert.Equal(t, int64(4294967298), out.Version)
			assert.Equal(t, "observed checksum", out.CodeSHA256)
			assert.Equal(t, "observed description", out.Description)
			assert.Equal(t, "observed license", out.LicenseInfo)
		})
	}
}

func setLayerHome(t *testing.T, path string) {
	t.Helper()
	switch goruntime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", path)
	case "plan9":
		t.Setenv("home", path)
	default:
		t.Setenv("HOME", path)
	}
}

func TestLayerVersionAccountHomeFallback(t *testing.T) {
	account, err := user.Current()
	require.NoError(t, err)
	require.NotEmpty(t, account.HomeDir)
	setLayerHome(t, "")

	missing := filepath.Join(filepath.Base(filepath.Dir(t.TempDir())), "missing.zip")
	r := LayerVersionResource{
		Content: LayerVersionContent{ZipFilePath: aws.String(filepath.Join("~", missing))},
	}
	_, err = r.prepareLayerPublish()
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	assert.Equal(t, filepath.Join(account.HomeDir, missing), pathErr.Path)
}

func TestLayerVersionLiteralBackslash(t *testing.T) {
	if os.IsPathSeparator('\\') {
		t.Skip("backslash is a path separator on this platform")
	}
	t.Chdir(t.TempDir())
	path := `~\layer.zip`
	data := []byte("local package")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	r := LayerVersionResource{Content: LayerVersionContent{ZipFilePath: &path}}
	request, err := r.prepareLayerPublish()
	require.NoError(t, err)
	assert.Equal(t, data, request.Content.ZipFile)
}

func TestLayerVersionAPIErrors(t *testing.T) {
	refusal := errors.New("API error")
	missing := &lambdatypes.ResourceNotFoundException{Message: aws.String("gone")}
	get := &lambda.GetLayerVersionInput{
		LayerName: aws.String("observed"), VersionNumber: aws.Int64(4294967298),
	}
	del := &lambda.DeleteLayerVersionInput{
		LayerName: aws.String("observed"), VersionNumber: aws.Int64(4294967298),
	}
	publish := &lambda.PublishLayerVersionInput{
		LayerName: aws.String("desired"),
		Content: &lambdatypes.LayerVersionContentInput{
			S3Bucket: aws.String("bucket"), S3Key: aws.String("layer.zip"),
		},
		Description: aws.String(""), LicenseInfo: aws.String(""),
	}
	prior := layerVersionPrior{Outputs: &LayerVersionResourceOutput{LayerVersionArn: layerVersionARN}}
	for _, tc := range []struct {
		name, operation, diagnostic string
		publishErr, getErr, delErr  error
		observation                 *lambda.GetLayerVersionOutput
		wantErr                     error
		wantRequests                []any
	}{
		{
			name: "publish failed", operation: "create", publishErr: refusal,
			diagnostic: "publish layer version for \"desired\"", wantErr: refusal,
			wantRequests: []any{publish},
		},
		{
			name: "fresh 404", operation: "create", getErr: missing,
			diagnostic: "get layer version \"" + layerVersionARN + "\" after publish",
			wantErr:    runtime.ErrNotFound, wantRequests: []any{publish, get},
		},
		{
			name: "fresh nil", operation: "create",
			diagnostic: "get layer version \"" + layerVersionARN + "\" after publish",
			wantErr:    runtime.ErrNotFound, wantRequests: []any{publish, get},
		},
		{
			name: "tracked 404", operation: "read", getErr: missing,
			wantErr: runtime.ErrNotFound, wantRequests: []any{get},
		},
		{
			name: "tracked nil", operation: "read",
			wantErr: runtime.ErrNotFound, wantRequests: []any{get},
		},
		{
			name: "read failed", operation: "read", getErr: refusal,
			diagnostic: "get layer version \"" + layerVersionARN + "\"",
			wantErr:    refusal, wantRequests: []any{get},
		},
		{
			name: "missing identity", operation: "read",
			observation: &lambda.GetLayerVersionOutput{},
			diagnostic:  "invalid layer version ARN", wantRequests: []any{get},
		},
		{
			name: "missing content", operation: "read",
			observation: &lambda.GetLayerVersionOutput{
				LayerVersionArn: aws.String(layerVersionARN),
				LayerArn: aws.String(
					"arn:aws:lambda:us-east-1:123456789012:layer:observed",
				),
			},
			diagnostic: "missing layer ARN or content", wantRequests: []any{get},
		},
		{
			name: "missing unqualified ARN", operation: "read",
			observation: &lambda.GetLayerVersionOutput{
				LayerVersionArn: aws.String(layerVersionARN),
				Content:         &lambdatypes.LayerVersionContentOutput{},
			},
			diagnostic: "missing layer ARN or content", wantRequests: []any{get},
		},
		{
			name: "delete race", operation: "delete", delErr: missing, wantRequests: []any{del},
		},
		{
			name: "delete failed", operation: "delete", delErr: refusal,
			diagnostic: "delete layer version \"" + layerVersionARN + "\"",
			wantErr:    refusal, wantRequests: []any{del},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeLayerVersionClient{
				published:   &lambda.PublishLayerVersionOutput{LayerVersionArn: aws.String(layerVersionARN)},
				publishErr:  tc.publishErr,
				observation: tc.observation,
				getErr:      tc.getErr,
				deleteErr:   tc.delErr,
			}
			r := LayerVersionResource{
				LayerName: "desired",
				Content: LayerVersionContent{
					S3Bucket: aws.String("bucket"), S3Key: aws.String("layer.zip"),
				},
				clientFor: func(context.Context, *awsCfg) (layerVersionClient, error) {
					return client, nil
				},
			}
			var err error
			switch tc.operation {
			case "create":
				_, err = r.Create(t.Context(), nil)
			case "read":
				_, err = r.Read(t.Context(), nil, prior)
			case "delete":
				err = r.Delete(t.Context(), nil, prior)
			}
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}
			if tc.diagnostic != "" {
				assert.ErrorContains(t, err, tc.diagnostic)
			}
			if tc.wantErr == nil && tc.diagnostic == "" {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.wantRequests, client.requests)
		})
	}
}

func TestLayerVersionSkipDestroy(t *testing.T) {
	resource := &LayerVersionResource{}
	require.NoError(t, resource.Delete(t.Context(), nil, layerVersionPrior{
		Inputs: LayerVersionResource{SkipDestroy: true},
	}))
}

func TestLayerVersionReadMissingIdentity(t *testing.T) {
	resource := &LayerVersionResource{LayerName: "desired"}
	_, err := resource.Read(t.Context(), &awsCfg{Region: aws.String("us-east-1")}, layerVersionPrior{})
	require.ErrorContains(t, err, "missing prior layer version ARN")
}
