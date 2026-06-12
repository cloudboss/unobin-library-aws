package cloudfront

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOverlayChangedConfig guards the fix for an UpdateDistribution rejection.
// CreateDistribution lets CloudFront default optional members at every level,
// but UpdateDistribution rejects a config that omits one ("<field> is missing
// for the resource"). The update starts from the complete live config and
// overlays only the fields the user changed, so an unchanged field -- including
// a nested one like an origin's path -- keeps its live value and the config
// stays complete.
func TestOverlayChangedConfig(t *testing.T) {
	config := &cloudfronttypes.DistributionConfig{
		Comment:     aws.String("old"),
		HttpVersion: cloudfronttypes.HttpVersion("http1.1"),
		WebACLId:    aws.String(""),
		Origins: &cloudfronttypes.Origins{
			Quantity: aws.Int32(1),
			Items: []cloudfronttypes.Origin{{
				Id:         aws.String("s3-origin"),
				DomainName: aws.String("example.s3.amazonaws.com"),
				OriginPath: aws.String(""),
			}},
		},
	}
	// Only the comment changed; every other input is unset and unchanged.
	r := &Distribution{Comment: aws.String("new")}
	prior := runtime.Prior[Distribution, *DistributionOutput]{
		Inputs: Distribution{Comment: aws.String("old")},
	}

	overlayChangedConfig(config, r, prior)

	// The changed field is overlaid.
	assert.Equal(t, "new", aws.ToString(config.Comment))
	// Unchanged top-level fields keep their live values.
	assert.Equal(t, cloudfronttypes.HttpVersion("http1.1"), config.HttpVersion)
	assert.Equal(t, "", aws.ToString(config.WebACLId))
	// Unchanged nested fields are preserved: the origin path stays present, which
	// is the member UpdateDistribution complained was missing.
	require.NotNil(t, config.Origins)
	require.Len(t, config.Origins.Items, 1)
	require.NotNil(t, config.Origins.Items[0].OriginPath)
	assert.Equal(t, "", aws.ToString(config.Origins.Items[0].OriginPath))
}
