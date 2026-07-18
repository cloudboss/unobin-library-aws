package eks

import (
	"context"

	eks "github.com/aws/aws-sdk-go-v2/service/eks"
)

type nodeGroupClient interface {
	CreateNodegroup(context.Context, *eks.CreateNodegroupInput,
		...func(*eks.Options)) (*eks.CreateNodegroupOutput, error)
	DescribeNodegroup(context.Context, *eks.DescribeNodegroupInput,
		...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error)
	UpdateNodegroupVersion(context.Context, *eks.UpdateNodegroupVersionInput,
		...func(*eks.Options)) (*eks.UpdateNodegroupVersionOutput, error)
	UpdateNodegroupConfig(context.Context, *eks.UpdateNodegroupConfigInput,
		...func(*eks.Options)) (*eks.UpdateNodegroupConfigOutput, error)
	DescribeUpdate(context.Context, *eks.DescribeUpdateInput,
		...func(*eks.Options)) (*eks.DescribeUpdateOutput, error)
	DeleteNodegroup(context.Context, *eks.DeleteNodegroupInput,
		...func(*eks.Options)) (*eks.DeleteNodegroupOutput, error)
	ListTagsForResource(context.Context, *eks.ListTagsForResourceInput,
		...func(*eks.Options)) (*eks.ListTagsForResourceOutput, error)
	TagResource(context.Context, *eks.TagResourceInput,
		...func(*eks.Options)) (*eks.TagResourceOutput, error)
	UntagResource(context.Context, *eks.UntagResourceInput,
		...func(*eks.Options)) (*eks.UntagResourceOutput, error)
}
