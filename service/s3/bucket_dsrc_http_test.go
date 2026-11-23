package s3_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/s3"
)

type bucketLookupResponse struct {
	region        string
	headCodes     []int
	website       string
	sessionStatus int
	sessionBody   string
}

type bucketLookupAPI struct {
	endpoint         string
	loopbackEndpoint string
	mu               sync.Mutex
	calls            []string
	response         bucketLookupResponse
}

func newBucketLookupAPI(t *testing.T, response bucketLookupResponse) *bucketLookupAPI {
	t.Helper()
	temp := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(temp, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(temp, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "bucket-test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "bucket-test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "bucket-test-token")
	t.Setenv("AWS_ENDPOINT_URL", "")
	t.Setenv("AWS_ENDPOINT_URL_S3", "")
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")
	api := &bucketLookupAPI{response: response}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=bucket-test-key/")
		assert.Equal(t, "bucket-test-token", r.Header.Get("X-Amz-Security-Token"))
		assert.Empty(t, r.Header.Values("X-Amz-Expected-Bucket-Owner"))

		api.mu.Lock()
		defer api.mu.Unlock()
		api.calls = append(api.calls, r.Method+" "+r.URL.RequestURI())
		if r.Method == http.MethodHead {
			if api.response.region != "" {
				w.Header().Set("x-amz-bucket-region", api.response.region)
			}
			if len(api.response.headCodes) > 0 {
				code := api.response.headCodes[0]
				api.response.headCodes = api.response.headCodes[1:]
				w.WriteHeader(code)
			}
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Has("session") && api.response.sessionStatus != 0 {
			w.WriteHeader(api.response.sessionStatus)
			_, _ = w.Write([]byte(api.response.sessionBody))
			return
		}
		if api.response.website == "" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<Error><Code>NoSuchWebsiteConfiguration</Code></Error>"))
			return
		}
		_, _ = w.Write([]byte(api.response.website))
	}))
	t.Cleanup(server.Close)
	api.loopbackEndpoint = server.URL
	api.endpoint = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	return api
}

func (a *bucketLookupAPI) setResponse(response bucketLookupResponse) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.response = response
}

func (a *bucketLookupAPI) requireCalls(t *testing.T, want ...string) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	require.Equal(t, want, a.calls)
	a.calls = nil
}

func bucketLookupCalls(bucket string, reads int) []string {
	calls := make([]string, 0, reads*2)
	for range reads {
		calls = append(calls, "HEAD /"+bucket, "GET /"+bucket+"?website=")
	}
	return calls
}

func TestBucketLookupCustomEndpoints(t *testing.T) {
	for _, tt := range []struct {
		name      string
		header    string
		configure func(*testing.T, *awscfg.Configuration, string)
		want      svc.BucketDataSourceOutput
	}{
		{
			name: "explicit S3 wins over global and environment", header: "cn-north-1",
			configure: func(t *testing.T, cfg *awscfg.Configuration, endpoint string) {
				cfg.EndpointURL = aws.String(endpoint + "/global")
				cfg.Endpoints = &awscfg.Endpoints{S3: aws.String(endpoint)}
				t.Setenv("AWS_ENDPOINT_URL_S3", endpoint+"/environment")
			},
			want: svc.BucketDataSourceOutput{
				Arn:                      "arn:aws:s3:::sample",
				BucketRegion:             "cn-north-1",
				BucketDomainName:         "sample.s3.amazonaws.com",
				BucketRegionalDomainName: "sample.s3.cn-north-1.amazonaws.com.cn",
				HostedZoneID:             aws.String("Z5CN8UMXT92WN"),
				WebsiteDomain:            aws.String("s3-website.cn-north-1.amazonaws.com.cn"),
				WebsiteEndpoint:          aws.String("sample.s3-website.cn-north-1.amazonaws.com.cn"),
			},
		},
		{
			name: "SDK resolved S3 endpoint and headerless response",
			configure: func(t *testing.T, _ *awscfg.Configuration, endpoint string) {
				t.Setenv("AWS_ENDPOINT_URL_S3", endpoint)
			},
			want: svc.BucketDataSourceOutput{
				Arn:                      "arn:aws:s3:::sample",
				BucketRegion:             "us-east-1",
				BucketDomainName:         "sample.s3.amazonaws.com",
				BucketRegionalDomainName: "sample.s3.us-east-1.amazonaws.com",
				HostedZoneID:             aws.String("Z3AQBSTGFYJSTF"),
				WebsiteDomain:            aws.String("s3-website-us-east-1.amazonaws.com"),
				WebsiteEndpoint:          aws.String("sample.s3-website-us-east-1.amazonaws.com"),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := newBucketLookupAPI(t, bucketLookupResponse{
				region: tt.header,
				website: "<WebsiteConfiguration><RedirectAllRequestsTo><HostName>site.example" +
					"</HostName></RedirectAllRequestsTo></WebsiteConfiguration>",
			})
			cfg := &awscfg.Configuration{Region: aws.String("us-east-1"), MaxAttempts: aws.Int64(1)}
			tt.configure(t, cfg, api.endpoint)
			got, err := (&svc.BucketDataSource{Bucket: "sample"}).Read(t.Context(), cfg)
			require.NoError(t, err)
			require.Equal(t, &tt.want, got)
			api.requireCalls(t, bucketLookupCalls("sample", 1)...)
		})
	}
}

func TestBucketLookupRejectedHeadWithRegionHint(t *testing.T) {
	for _, tt := range []struct {
		name string
		code int
	}{
		{"permanent redirect", http.StatusMovedPermanently},
		{"forbidden", http.StatusForbidden},
		{"server error with retries disabled", http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := newBucketLookupAPI(t, bucketLookupResponse{
				region: "us-west-2", headCodes: []int{tt.code},
			})
			cfg := &awscfg.Configuration{
				Region: aws.String("us-east-1"), EndpointURL: aws.String(api.endpoint),
				MaxAttempts: aws.Int64(1),
			}
			out, err := (&svc.BucketDataSource{Bucket: "sample"}).Read(t.Context(), cfg)
			require.ErrorContains(t, err, `head bucket "sample"`)
			var response *smithyhttp.ResponseError
			require.ErrorAs(t, err, &response)
			require.Equal(t, tt.code, response.HTTPStatusCode())
			require.Equal(t, "us-west-2", response.Response.Header.Get("x-amz-bucket-region"))
			require.Nil(t, out)
			api.requireCalls(t, "HEAD /sample")
		})
	}
}

func TestBucketLookupDirectorySessionErrors(t *testing.T) {
	const bucket = "missing--use1-az2--x-s3"
	for _, tt := range []struct {
		name, code, message string
		status              int
		missing             bool
	}{
		{
			name: "missing bucket", code: "NoSuchBucket", message: "The bucket does not exist",
			status: http.StatusNotFound, missing: true,
		},
		{
			name: "access denied", code: "AccessDenied", message: "NoSuchBucket: access denied",
			status: http.StatusForbidden,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AWS_S3_DISABLE_EXPRESS_SESSION_AUTH", "false")
			api := newBucketLookupAPI(t, bucketLookupResponse{
				sessionStatus: tt.status,
				sessionBody: "<Error><Code>" + tt.code + "</Code><Message>" + tt.message +
					"</Message></Error>",
			})
			// S3 Express uses path-style requests for IP endpoints.
			cfg := &awscfg.Configuration{
				Region: aws.String("us-east-1"), EndpointURL: aws.String(api.loopbackEndpoint),
				MaxAttempts: aws.Int64(1),
			}
			out, err := (&svc.BucketDataSource{Bucket: bucket}).Read(t.Context(), cfg)
			require.Nil(t, out)
			require.ErrorContains(t, err, `head bucket "`+bucket+`"`)
			var operation *smithy.OperationError
			require.ErrorAs(t, err, &operation)
			assert.Equal(t, "S3", operation.Service())
			assert.Equal(t, "HeadBucket", operation.Operation())
			assert.ErrorIs(t, err, operation)
			assert.ErrorContains(t, operation, "operation error S3: CreateSession")
			assert.ErrorContains(t, operation, tt.code+": "+tt.message)
			api.requireCalls(t, "GET /"+bucket+"?session=")
			if tt.missing {
				assert.ErrorContains(t, err, "bucket not found")
			} else {
				assert.NotContains(t, err.Error(), "bucket not found")
			}
		})
	}
}

func TestBucketLookupRetainsRetrySettings(t *testing.T) {
	api := newBucketLookupAPI(t, bucketLookupResponse{
		region: "us-east-1", headCodes: []int{http.StatusInternalServerError},
	})
	cfg := &awscfg.Configuration{
		Region: aws.String("us-east-1"), EndpointURL: aws.String(api.endpoint),
		MaxAttempts: aws.Int64(2),
	}
	got, err := (&svc.BucketDataSource{Bucket: "sample"}).Read(t.Context(), cfg)
	require.NoError(t, err)
	require.Equal(t, &svc.BucketDataSourceOutput{
		Arn:                      "arn:aws:s3:::sample",
		BucketRegion:             "us-east-1",
		BucketDomainName:         "sample.s3.amazonaws.com",
		BucketRegionalDomainName: "sample.s3.us-east-1.amazonaws.com",
		HostedZoneID:             aws.String("Z3AQBSTGFYJSTF"),
	}, got)
	api.requireCalls(t, "HEAD /sample", "HEAD /sample", "GET /sample?website=")
}
