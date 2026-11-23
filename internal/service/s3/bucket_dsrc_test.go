package s3

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBucketLookup struct {
	head         *s3.HeadBucketOutput
	headErr      error
	website      *s3.GetBucketWebsiteOutput
	websiteErr   error
	heads        []*s3.HeadBucketInput
	websites     []*s3.GetBucketWebsiteInput
	useARNRegion bool
	headARN      []bool
	websiteARN   []bool
}

func (f *fakeBucketLookup) HeadBucket(
	_ context.Context, input *s3.HeadBucketInput, opts ...func(*s3.Options),
) (*s3.HeadBucketOutput, error) {
	f.heads = append(f.heads, input)
	options := s3.Options{UseARNRegion: f.useARNRegion}
	for _, opt := range opts {
		opt(&options)
	}
	f.headARN = append(f.headARN, options.UseARNRegion)
	return f.head, f.headErr
}

func (f *fakeBucketLookup) GetBucketWebsite(
	_ context.Context, input *s3.GetBucketWebsiteInput, opts ...func(*s3.Options),
) (*s3.GetBucketWebsiteOutput, error) {
	f.websites = append(f.websites, input)
	options := s3.Options{UseARNRegion: f.useARNRegion}
	for _, opt := range opts {
		opt(&options)
	}
	f.websiteARN = append(f.websiteARN, options.UseARNRegion)
	return f.website, f.websiteErr
}

func TestBucketLookupOutputs(t *testing.T) {
	const accessPoint = "arn:aws:s3:us-west-2:123456789012:accesspoint/demo"
	tests := []struct {
		name, bucket, configured, returned string
		routeARN                           bool
		want                               BucketDataSourceOutput
	}{
		{
			name: "commercial fallback and dash website", bucket: "bucket.name",
			configured: "us-east-1",
			want: BucketDataSourceOutput{
				Arn:                      "arn:aws:s3:::bucket.name",
				BucketRegion:             "us-east-1",
				BucketDomainName:         "bucket.name.s3.amazonaws.com",
				BucketRegionalDomainName: "bucket.name.s3.us-east-1.amazonaws.com",
				HostedZoneID:             aws.String("Z3AQBSTGFYJSTF"),
				WebsiteDomain:            aws.String("s3-website-us-east-1.amazonaws.com"),
				WebsiteEndpoint:          aws.String("bucket.name.s3-website-us-east-1.amazonaws.com"),
			},
		},
		{
			name: "China configured partition with GovCloud response", bucket: "mixed",
			configured: "cn-north-1", returned: "us-gov-west-1",
			want: BucketDataSourceOutput{
				Arn:                      "arn:aws-cn:s3:::mixed",
				BucketRegion:             "us-gov-west-1",
				BucketDomainName:         "mixed.s3.amazonaws.com.cn",
				BucketRegionalDomainName: "mixed.s3.us-gov-west-1.amazonaws.com",
				HostedZoneID:             aws.String("Z31GFT0UA1I2HV"),
				WebsiteDomain:            aws.String("s3-website-us-gov-west-1.amazonaws.com"),
				WebsiteEndpoint:          aws.String("mixed.s3-website-us-gov-west-1.amazonaws.com"),
			},
		},
		{
			name: "GovCloud configured partition with China response", bucket: "mixed",
			configured: "us-gov-west-1", returned: "cn-north-1",
			want: BucketDataSourceOutput{
				Arn:                      "arn:aws-us-gov:s3:::mixed",
				BucketRegion:             "cn-north-1",
				BucketDomainName:         "mixed.s3.amazonaws.com",
				BucketRegionalDomainName: "mixed.s3.cn-north-1.amazonaws.com.cn",
				HostedZoneID:             aws.String("Z5CN8UMXT92WN"),
				WebsiteDomain:            aws.String("s3-website.cn-north-1.amazonaws.com.cn"),
				WebsiteEndpoint:          aws.String("mixed.s3-website.cn-north-1.amazonaws.com.cn"),
			},
		},
		{
			name:   "European Sovereign Cloud alias with unknown response region",
			bucket: "demo-s3alias", configured: "eusc-de-east-1", returned: "unknown",
			want: BucketDataSourceOutput{
				Arn:                      "arn:aws-eusc:s3:::demo-s3alias",
				BucketRegion:             "unknown",
				BucketDomainName:         "demo-s3alias.s3.amazonaws.eu",
				BucketRegionalDomainName: "demo-s3alias.s3.unknown.amazonaws.com",
				WebsiteDomain:            aws.String("s3-website.unknown.amazonaws.com"),
				WebsiteEndpoint:          aws.String("demo-s3alias.s3-website.unknown.amazonaws.com"),
			},
		},
		{
			name: "European Sovereign Cloud response suffix", bucket: "bucket",
			configured: "us-east-1", returned: "eusc-de-east-1",
			want: BucketDataSourceOutput{
				Arn:                      "arn:aws:s3:::bucket",
				BucketRegion:             "eusc-de-east-1",
				BucketDomainName:         "bucket.s3.amazonaws.com",
				BucketRegionalDomainName: "bucket.s3.eusc-de-east-1.amazonaws.eu",
				WebsiteDomain:            aws.String("s3-website.eusc-de-east-1.amazonaws.eu"),
				WebsiteEndpoint:          aws.String("bucket.s3-website.eusc-de-east-1.amazonaws.eu"),
			},
		},
		{
			name: "global ARN with default region fallback", bucket: accessPoint,
			configured: "aws-global", routeARN: true,
			want: BucketDataSourceOutput{
				Arn:                      accessPoint,
				BucketRegion:             "us-east-1",
				BucketDomainName:         accessPoint + ".s3.amazonaws.com",
				BucketRegionalDomainName: accessPoint + ".s3.us-east-1.amazonaws.com",
				HostedZoneID:             aws.String("Z3AQBSTGFYJSTF"),
				WebsiteDomain:            aws.String("s3-website-us-east-1.amazonaws.com"),
				WebsiteEndpoint:          aws.String(accessPoint + ".s3-website-us-east-1.amazonaws.com"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeBucketLookup{
				head: &s3.HeadBucketOutput{BucketRegion: aws.String(tt.returned)},
				website: &s3.GetBucketWebsiteOutput{
					RedirectAllRequestsTo: &types.RedirectAllRequestsTo{HostName: aws.String("site")},
				},
			}
			lookup := BucketDataSource{Bucket: tt.bucket}
			got, err := lookup.read(t.Context(), client, tt.configured)
			require.NoError(t, err)
			assert.Equal(t, &tt.want, got)
			assert.Equal(t, []*s3.HeadBucketInput{{Bucket: aws.String(tt.bucket)}}, client.heads)
			assert.Equal(t, []bool{tt.routeARN}, client.headARN)
			assert.Equal(t, []bool{false}, client.websiteARN)
			assert.Equal(t, []*s3.GetBucketWebsiteInput{{Bucket: aws.String(tt.bucket)}},
				client.websites)
		})
	}
}

func TestBucketLookupARNRegionOptions(t *testing.T) {
	for _, tt := range []struct {
		name, bucket, region string
		useARNRegion         bool
	}{
		{
			name: "ARN in regional config", region: "us-east-1",
			bucket: "arn:aws:s3:us-west-2:123456789012:accesspoint/demo",
		},
		{name: "global regular bucket", bucket: "bucket", region: "aws-global"},
		{
			name: "configured SDK option", region: "us-east-1", useARNRegion: true,
			bucket: "arn:aws:s3:us-west-2:123456789012:accesspoint/demo",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lookup := BucketDataSource{Bucket: tt.bucket}
			client := &fakeBucketLookup{
				head: &s3.HeadBucketOutput{}, useARNRegion: tt.useARNRegion,
			}
			_, err := lookup.read(t.Context(), client, tt.region)
			require.NoError(t, err)
			assert.Equal(t, []bool{tt.useARNRegion}, client.headARN)
			assert.Equal(t, []bool{tt.useARNRegion}, client.websiteARN)
		})
	}
}

func TestBucketLookupHeadErrors(t *testing.T) {
	for _, tt := range []struct {
		name, region string
		head         *s3.HeadBucketOutput
		cause        error
		message      string
	}{
		{
			name: "bare 404", cause: &smithyhttp.ResponseError{
				Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 404}},
				Err:      errors.New("no response body"),
			}, message: "bucket not found",
		},
		{
			name: "NoSuchBucket", cause: &types.NoSuchBucket{}, message: "bucket not found",
		},
		{
			name: "directory session", cause: &smithy.OperationError{
				ServiceID: s3.ServiceID, OperationName: "HeadBucket",
				Err: &smithy.OperationError{
					ServiceID: s3.ServiceID, OperationName: "CreateSession",
					Err: errors.New("NoSuchBucket"),
				},
			}, message: "bucket not found",
		},
		{
			name: "access denied", cause: &smithy.GenericAPIError{Code: "AccessDenied"},
			message: "AccessDenied",
		},
		{name: "transport", cause: errors.New("dial failed"), message: "dial failed"},
		{name: "nil success", message: "empty response"},
		{name: "no region", head: &s3.HeadBucketOutput{}, message: "bucket region is unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeBucketLookup{head: tt.head, headErr: tt.cause}
			got, err := (&BucketDataSource{Bucket: "sample"}).read(t.Context(), client, tt.region)
			require.ErrorContains(t, err, `head bucket "sample"`)
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, got)
			if tt.cause != nil {
				assert.ErrorIs(t, err, tt.cause)
			}
			assert.Equal(t, []*s3.HeadBucketInput{{Bucket: aws.String("sample")}}, client.heads)
			assert.Empty(t, client.websites)
		})
	}
}

func TestBucketLookupOptionalWebsite(t *testing.T) {
	for _, tt := range []struct {
		name  string
		cause error
		warn  bool
	}{
		{name: "nil response"},
		{name: "missing configuration", cause: &smithy.GenericAPIError{
			Code: "NoSuchWebsiteConfiguration",
		}},
		{name: "missing bucket", cause: &types.NoSuchBucket{}},
		{name: "denied", cause: &smithy.GenericAPIError{Code: "AccessDenied"}, warn: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			oldLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(oldLogger) })
			client := &fakeBucketLookup{
				head: &s3.HeadBucketOutput{}, websiteErr: tt.cause,
			}
			got, err := (&BucketDataSource{Bucket: "sample"}).read(t.Context(), client, "us-east-1")
			require.NoError(t, err)
			assert.Equal(t, &BucketDataSourceOutput{
				Arn:                      "arn:aws:s3:::sample",
				BucketRegion:             "us-east-1",
				BucketDomainName:         "sample.s3.amazonaws.com",
				BucketRegionalDomainName: "sample.s3.us-east-1.amazonaws.com",
				HostedZoneID:             aws.String("Z3AQBSTGFYJSTF"),
			}, got)
			if tt.warn {
				assert.Contains(t, logs.String(), "level=WARN")
				assert.Contains(t, logs.String(), "get bucket website")
				assert.Contains(t, logs.String(), "bucket=sample")
				assert.Contains(t, logs.String(), "AccessDenied")
			} else {
				assert.Empty(t, logs.String())
			}
		})
	}
}

func TestBucketLookupWebsiteHostedZones(t *testing.T) {
	data, err := os.ReadFile("testdata/website-zones.txt")
	require.NoError(t, err)
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	want := make(map[string]*string)
	got := make(map[string]*string)
	lookup := BucketDataSource{Bucket: "sample"}
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		require.Len(t, fields, 2, "expected a region and zone in %q", line)
		region, zone := fields[0], fields[1]
		client := &fakeBucketLookup{
			head: &s3.HeadBucketOutput{BucketRegion: aws.String(region)},
		}
		out, err := lookup.read(t.Context(), client, "us-east-1")
		require.NoError(t, err)
		want[region] = aws.String(zone)
		got[region] = out.HostedZoneID
	}
	require.NotEmpty(t, want)
	assert.Equal(t, want, got)
	assert.Empty(t, logs.String())

	out, err := lookup.read(t.Context(),
		&fakeBucketLookup{head: &s3.HeadBucketOutput{BucketRegion: aws.String("unknown")}},
		"us-east-1")
	require.NoError(t, err)
	assert.Nil(t, out.HostedZoneID)
	assert.Contains(t, logs.String(), "level=WARN")
	assert.Contains(t, logs.String(), "bucket=sample")
	assert.Contains(t, logs.String(), "region="+out.BucketRegion)
}

func TestBucketLookupLegacyWebsiteDomains(t *testing.T) {
	want := map[string]*string{
		"ap-northeast-1": aws.String("s3-website-ap-northeast-1.amazonaws.com"),
		"ap-southeast-1": aws.String("s3-website-ap-southeast-1.amazonaws.com"),
		"ap-southeast-2": aws.String("s3-website-ap-southeast-2.amazonaws.com"),
		"eu-west-1":      aws.String("s3-website-eu-west-1.amazonaws.com"),
		"sa-east-1":      aws.String("s3-website-sa-east-1.amazonaws.com"),
		"us-east-1":      aws.String("s3-website-us-east-1.amazonaws.com"),
		"us-gov-west-1":  aws.String("s3-website-us-gov-west-1.amazonaws.com"),
		"us-west-1":      aws.String("s3-website-us-west-1.amazonaws.com"),
		"us-west-2":      aws.String("s3-website-us-west-2.amazonaws.com"),
	}
	got := make(map[string]*string)
	lookup := BucketDataSource{Bucket: "sample"}
	for region := range want {
		client := &fakeBucketLookup{
			head:    &s3.HeadBucketOutput{BucketRegion: aws.String(region)},
			website: &s3.GetBucketWebsiteOutput{},
		}
		out, err := lookup.read(t.Context(), client, "us-east-1")
		require.NoError(t, err)
		got[region] = out.WebsiteDomain
	}
	assert.Equal(t, want, got)
}
