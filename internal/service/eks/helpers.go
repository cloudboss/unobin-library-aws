package eks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

type eksClient interface {
	CreateCluster(context.Context, *eks.CreateClusterInput,
		...func(*eks.Options)) (*eks.CreateClusterOutput, error)
	DescribeCluster(context.Context, *eks.DescribeClusterInput,
		...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	UpdateClusterVersion(context.Context, *eks.UpdateClusterVersionInput,
		...func(*eks.Options)) (*eks.UpdateClusterVersionOutput, error)
	UpdateClusterConfig(context.Context, *eks.UpdateClusterConfigInput,
		...func(*eks.Options)) (*eks.UpdateClusterConfigOutput, error)
	AssociateEncryptionConfig(context.Context, *eks.AssociateEncryptionConfigInput,
		...func(*eks.Options)) (*eks.AssociateEncryptionConfigOutput, error)
	DescribeUpdate(context.Context, *eks.DescribeUpdateInput,
		...func(*eks.Options)) (*eks.DescribeUpdateOutput, error)
	DeleteCluster(context.Context, *eks.DeleteClusterInput,
		...func(*eks.Options)) (*eks.DeleteClusterOutput, error)
	ListTagsForResource(context.Context, *eks.ListTagsForResourceInput,
		...func(*eks.Options)) (*eks.ListTagsForResourceOutput, error)
	TagResource(context.Context, *eks.TagResourceInput,
		...func(*eks.Options)) (*eks.TagResourceOutput, error)
	UntagResource(context.Context, *eks.UntagResourceInput,
		...func(*eks.Options)) (*eks.UntagResourceOutput, error)
}

func newClient(ctx context.Context, cfg *awsCfg) (*eks.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return eks.NewFromConfig(loaded), nil
}

func eksClientRequestToken() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate client request token: %w", err)
	}
	random[6] = (random[6] & 0x0f) | 0x40
	random[8] = (random[8] & 0x3f) | 0x80
	var token [36]byte
	hex.Encode(token[0:8], random[0:4])
	token[8] = '-'
	hex.Encode(token[9:13], random[4:6])
	token[13] = '-'
	hex.Encode(token[14:18], random[6:8])
	token[18] = '-'
	hex.Encode(token[19:23], random[8:10])
	token[23] = '-'
	hex.Encode(token[24:36], random[10:16])
	return string(token[:]), nil
}
