package eks

import (
	"context"

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
