package s3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBucketDomainNames checks that a bucket's global and regional domain names
// carry the DNS suffix the S3 endpoint resolver reports for the region's
// partition, so they follow the bucket into the China and GovCloud partitions
// rather than assuming amazonaws.com. The suffix is the resolver's, not a table
// the library keeps, so a new partition arrives with an SDK update.
func TestBucketDomainNames(t *testing.T) {
	cases := []struct {
		region   string
		domain   string
		regional string
	}{
		{"us-east-1", "b.s3.amazonaws.com", "b.s3.us-east-1.amazonaws.com"},
		{"us-east-2", "b.s3.amazonaws.com", "b.s3.us-east-2.amazonaws.com"},
		{"us-west-2", "b.s3.amazonaws.com", "b.s3.us-west-2.amazonaws.com"},
		{"eu-central-1", "b.s3.amazonaws.com", "b.s3.eu-central-1.amazonaws.com"},
		{"ap-southeast-2", "b.s3.amazonaws.com", "b.s3.ap-southeast-2.amazonaws.com"},
		{"cn-north-1", "b.s3.amazonaws.com.cn", "b.s3.cn-north-1.amazonaws.com.cn"},
		{"cn-northwest-1", "b.s3.amazonaws.com.cn", "b.s3.cn-northwest-1.amazonaws.com.cn"},
		{"us-gov-west-1", "b.s3.amazonaws.com", "b.s3.us-gov-west-1.amazonaws.com"},
		{"us-gov-east-1", "b.s3.amazonaws.com", "b.s3.us-gov-east-1.amazonaws.com"},
	}
	for _, c := range cases {
		t.Run(c.region, func(t *testing.T) {
			domain, regional, err := bucketDomainNames(context.Background(), "b", c.region)
			require.NoError(t, err)
			assert.Equal(t, c.domain, domain)
			assert.Equal(t, c.regional, regional)
		})
	}
}
