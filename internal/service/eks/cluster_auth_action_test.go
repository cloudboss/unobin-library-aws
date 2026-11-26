package eks

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/middleware"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type authSigner struct {
	request *http.Request
	err     error
	calls   int
}

func (s *authSigner) PresignHTTP(
	ctx context.Context, creds aws.Credentials, request *http.Request,
	hash, service, region string, _ time.Time, options ...func(*v4.SignerOptions),
) (string, http.Header, error) {
	s.calls++
	s.request = request.Clone(ctx)
	if s.err != nil {
		return "", nil, s.err
	}
	return v4.NewSigner().PresignHTTP(ctx, creds, request, hash, service, region,
		time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), options...)
}

type authTransport struct{ calls int }

func (s *authTransport) Do(*http.Request) (*http.Response, error) {
	s.calls++
	return nil, errors.New("operation HTTP send is forbidden")
}

func authSDK() aws.Config {
	return aws.Config{
		Region: "us-west-2",
		Credentials: credentials.NewStaticCredentialsProvider(
			"example-key", "example-secret", "session-token",
		),
	}
}

func authEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "example-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "example-secret")
	t.Setenv("AWS_SESSION_TOKEN", "session-token")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ENDPOINT_URL", "")
	t.Setenv("AWS_ENDPOINT_URL_STS", "")
}

func TestClusterAuthSigning(t *testing.T) {
	var signatures []string
	for _, name := range []string{"nonexistent-cluster", "", "  name / ? 雪  "} {
		t.Run(name, func(t *testing.T) {
			transport, signer := &authTransport{}, &authSigner{}
			cfg := authSDK()
			cfg.HTTPClient = transport
			action := ClusterAuthAction{Name: name}
			signed, err := action.prepareRequest(t.Context(), cfg, nil,
				func(o *sts.PresignOptions) { o.Presigner = signer })
			require.NoError(t, err)
			parsed, err := url.Parse(signed.URL)
			require.NoError(t, err)
			assert.Equal(t, "GET", signed.Method)
			assert.Equal(t, name, signer.request.Header.Get("x-k8s-aws-id"))
			assert.Equal(t, name, signed.SignedHeader.Get("x-k8s-aws-id"))
			query := parsed.Query()
			assert.Equal(t, "0", query.Get("X-Amz-Expires"))
			assert.Equal(t, "host;x-k8s-aws-id", query.Get("X-Amz-SignedHeaders"))
			assert.Equal(t, "GetCallerIdentity", query.Get("Action"))
			assert.Equal(t, "2011-06-15", query.Get("Version"))
			assert.Equal(t, "session-token", query.Get("X-Amz-Security-Token"))
			assert.Equal(t, "example-key/20261126/us-west-2/sts/aws4_request",
				query.Get("X-Amz-Credential"))
			assert.NotContains(t, query, "x-k8s-aws-id")
			assert.Equal(t, 1, signer.calls)
			assert.Zero(t, transport.calls)
			signatures = append(signatures, query.Get("X-Amz-Signature"))
		})
	}
	assert.NotEqual(t, signatures[0], signatures[1])
	assert.NotEqual(t, signatures[1], signatures[2])
}

func TestClusterAuthTokenAndConfiguration(t *testing.T) {
	authEnvironment(t)
	for _, test := range []struct {
		name          string
		region        *string
		endpoints     *awscfg.Endpoints
		host, signing string
		failure       string
	}{
		{name: "omitted", host: "global.invalid", signing: "us-west-2"},
		{name: "empty", region: aws.String(""), host: "global.invalid", signing: "us-west-2"},
		{name: "override", region: aws.String("us-east-1"), host: "global.invalid",
			signing: "us-east-1"},
		{name: "STS endpoint", endpoints: &awscfg.Endpoints{STS: aws.String("https://sts.invalid")},
			host: "sts.invalid", signing: "us-west-2"},
		{name: "partition", region: aws.String("cn-north-1"), failure: "partition"},
	} {
		t.Run(test.name, func(t *testing.T) {
			action := ClusterAuthAction{Name: "missing-cluster", Region: test.region}
			result, err := action.Run(t.Context(), &awsCfg{Region: aws.String("us-west-2"),
				EndpointURL: aws.String("https://global.invalid"), Endpoints: test.endpoints})
			if test.failure != "" {
				require.ErrorContains(t, err, test.failure)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(result.Token, "k8s-aws-v1."))
			encoded := strings.TrimPrefix(result.Token, "k8s-aws-v1.")
			assert.NotContains(t, encoded, "=")
			decoded, err := base64.RawURLEncoding.DecodeString(encoded)
			require.NoError(t, err)
			assert.Equal(t, result.Token,
				"k8s-aws-v1."+base64.RawURLEncoding.EncodeToString(decoded))
			parsed, err := url.Parse(string(decoded))
			require.NoError(t, err)
			assert.Equal(t, test.host, parsed.Host)
			assert.Contains(t, parsed.Query().Get("X-Amz-Credential"), "/"+test.signing+"/sts/")
			assert.Equal(t, "0", parsed.Query().Get("X-Amz-Expires"))
			assert.Equal(t, "session-token", parsed.Query().Get("X-Amz-Security-Token"))
			signingTime, err := time.Parse("20060102T150405Z", parsed.Query().Get("X-Amz-Date"))
			require.NoError(t, err)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
				"https://"+test.host+"/?Action=GetCallerIdentity&Version=2011-06-15"+
					"&X-Amz-Expires=0",
				nil)
			require.NoError(t, err)
			request.Header.Set("x-k8s-aws-id", "missing-cluster")
			expected, _, err := v4.NewSigner().PresignHTTP(t.Context(), aws.Credentials{
				AccessKeyID: "example-key", SecretAccessKey: "example-secret",
				SessionToken: "session-token",
			}, request, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				"sts", test.signing, signingTime)
			require.NoError(t, err)
			assert.Equal(t, expected, string(decoded))
		})
	}
}

func TestClusterAuthErrorsAndContext(t *testing.T) {
	cause := errors.New("test signing failure")
	action := ClusterAuthAction{Name: "example"}
	signer := &authSigner{err: cause}
	signed, err := action.prepareRequest(t.Context(), authSDK(), nil,
		func(o *sts.PresignOptions) { o.Presigner = signer })
	require.ErrorIs(t, err, cause)
	assert.Nil(t, signed)
	assert.Equal(t, 1, signer.calls)
	broken := authSDK()
	broken.APIOptions = append(broken.APIOptions, func(*middleware.Stack) error { return cause })
	signed, err = action.prepareRequest(t.Context(), broken, nil)
	require.ErrorIs(t, err, cause)
	assert.Nil(t, signed)
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(t.Context())
		cfg := authSDK()
		calls := 0
		cfg.Credentials = aws.CredentialsProviderFunc(func(got context.Context) (
			aws.Credentials, error,
		) {
			calls++
			assert.Equal(t, ctx.Done(), got.Done())
			if cancel {
				stop()
				<-got.Done()
				return aws.Credentials{}, got.Err()
			}
			return aws.Credentials{}, cause
		})
		signed, err := action.prepareRequest(ctx, cfg, nil)
		if cancel {
			require.ErrorIs(t, err, context.Canceled)
		} else {
			require.ErrorIs(t, err, cause)
		}
		assert.Nil(t, signed)
		assert.Equal(t, 1, calls)
		stop()
	}
	authEnvironment(t)
	result, err := action.Run(t.Context(), &awsCfg{CustomCABundle: aws.String("/missing/auth-ca")})
	require.ErrorContains(t, err, "load AWS configuration")
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Nil(t, result)
	result, err = action.Run(t.Context(), &awsCfg{Region: aws.String("us-east-1"),
		EndpointURL: aws.String("://invalid")})
	require.Error(t, err)
	assert.Nil(t, result)
}
