package s3

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

type BucketDataSource struct {
	Bucket string `ub:"bucket"`
}

type BucketDataSourceOutput struct {
	Arn                      string  `ub:"arn"`
	BucketRegion             string  `ub:"bucket-region"`
	BucketDomainName         string  `ub:"bucket-domain-name"`
	BucketRegionalDomainName string  `ub:"bucket-regional-domain-name"`
	HostedZoneID             *string `ub:"hosted-zone-id"`
	WebsiteDomain            *string `ub:"website-domain"`
	WebsiteEndpoint          *string `ub:"website-endpoint"`
}

func (r BucketDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.Bucket, nil)).
			Message("bucket is required"),
	}
}

type bucketLookupClient interface {
	HeadBucket(
		context.Context, *s3.HeadBucketInput, ...func(*s3.Options),
	) (*s3.HeadBucketOutput, error)
	GetBucketWebsite(
		context.Context, *s3.GetBucketWebsiteInput, ...func(*s3.Options),
	) (*s3.GetBucketWebsiteOutput, error)
}

func (r *BucketDataSource) Read(
	ctx context.Context, cfg *awsCfg,
) (*BucketDataSourceOutput, error) {
	sdkConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client := s3ClientFromConfig(sdkConfig, func(o *s3.Options) {
		if endpoint := cfg.S3Endpoint(); endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = o.BaseEndpoint != nil
	})
	return r.read(ctx, client, client.Options().Region)
}

func (r *BucketDataSource) read(
	ctx context.Context, client bucketLookupClient, region string,
) (*BucketDataSourceOutput, error) {
	headRequest, websiteRequest, isARN := r.lookupRequests()
	head, err := client.HeadBucket(ctx, headRequest, func(options *s3.Options) {
		if region == awsGlobalRegion && isARN {
			options.UseARNRegion = true
		}
	})
	if err != nil {
		if isMissingBucketLookup(err) {
			return nil, fmt.Errorf("head bucket %q: bucket not found: %w", r.Bucket, err)
		}
		return nil, fmt.Errorf("head bucket %q: %w", r.Bucket, err)
	}
	if head == nil {
		return nil, fmt.Errorf("head bucket %q: empty response", r.Bucket)
	}
	bucketRegion := aws.ToString(head.BucketRegion)
	if bucketRegion == "" {
		bucketRegion = region
		if region == awsGlobalRegion {
			bucketRegion = usEast1Region
		}
	}
	if bucketRegion == "" {
		return nil, fmt.Errorf("head bucket %q: bucket region is unavailable", r.Bucket)
	}

	partition, dns := bucketLookupPartition(region)
	_, regionalDNS := bucketLookupPartition(bucketRegion)
	bucketARN := r.Bucket
	if !isARN {
		bucketARN = "arn:" + partition + ":s3:::" + r.Bucket
	}
	out := &BucketDataSourceOutput{
		Arn:                      bucketARN,
		BucketRegion:             bucketRegion,
		BucketDomainName:         r.Bucket + ".s3." + dns,
		BucketRegionalDomainName: r.Bucket + ".s3." + bucketRegion + "." + regionalDNS,
	}
	if zoneID, ok := bucketWebsiteHostedZones[bucketRegion]; ok {
		out.HostedZoneID = aws.String(zoneID)
	} else {
		slog.WarnContext(ctx, "no S3 website hosted zone", "bucket", r.Bucket, "region", bucketRegion)
	}

	website, err := client.GetBucketWebsite(ctx, websiteRequest)
	if err != nil {
		if !isNotFound(err, "NoSuchWebsiteConfiguration", "NoSuchBucket") {
			slog.WarnContext(ctx, "get bucket website", "bucket", r.Bucket, "error", err)
		}
	} else if website != nil {
		domain := bucketWebsiteDomain(bucketRegion, regionalDNS)
		out.WebsiteDomain = aws.String(domain)
		out.WebsiteEndpoint = aws.String(r.Bucket + "." + domain)
	}
	return out, nil
}

func (r *BucketDataSource) lookupRequests() (*s3.HeadBucketInput, *s3.GetBucketWebsiteInput, bool) {
	bucket := aws.String(r.Bucket)
	return &s3.HeadBucketInput{Bucket: bucket},
		&s3.GetBucketWebsiteInput{Bucket: bucket}, arn.IsARN(r.Bucket)
}

// The S3 Express identity resolver formats the CreateSession error without wrapping it.
var missingBucketSessionError = regexp.MustCompile(
	`^get identity: get credentials: operation error S3: CreateSession, ` +
		`https response error StatusCode: [0-9]+, RequestID: [^,]*, HostID: [^,]*, NoSuchBucket: `,
)

func isMissingBucketLookup(err error) bool {
	if isNotFound(err, "NoSuchBucket", "NotFound") || isHTTPNotFound(err) {
		return true
	}
	for err != nil {
		var op *smithy.OperationError
		if !errors.As(err, &op) {
			return false
		}
		err = op.Unwrap()
		if err == nil || op.Service() != s3.ServiceID {
			continue
		}
		switch op.Operation() {
		case "CreateSession":
			return strings.Contains(err.Error(), "NoSuchBucket")
		case "HeadBucket":
			if missingBucketSessionError.MatchString(err.Error()) {
				return true
			}
		}
	}
	return false
}

func bucketLookupPartition(region string) (id, dns string) {
	p, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), region)
	if !ok {
		return endpoints.AwsPartitionID, "amazonaws.com"
	}
	return p.ID(), p.DNSSuffix()
}

func bucketWebsiteDomain(region, dns string) string {
	switch region {
	case "ap-northeast-1", "ap-southeast-1", "ap-southeast-2", "eu-west-1", "sa-east-1",
		"us-east-1", "us-gov-west-1", "us-west-1", "us-west-2":
		return "s3-website-" + region + ".amazonaws.com"
	default:
		return "s3-website." + region + "." + dns
	}
}
