package eks

import (
	"context"

	eks "github.com/aws/aws-sdk-go-v2/service/eks"
)

type addonClient interface {
	CreateAddon(context.Context, *eks.CreateAddonInput,
		...func(*eks.Options)) (*eks.CreateAddonOutput, error)
	DescribeAddon(context.Context, *eks.DescribeAddonInput,
		...func(*eks.Options)) (*eks.DescribeAddonOutput, error)
	UpdateAddon(context.Context, *eks.UpdateAddonInput,
		...func(*eks.Options)) (*eks.UpdateAddonOutput, error)
	DescribeUpdate(context.Context, *eks.DescribeUpdateInput,
		...func(*eks.Options)) (*eks.DescribeUpdateOutput, error)
	DeleteAddon(context.Context, *eks.DeleteAddonInput,
		...func(*eks.Options)) (*eks.DeleteAddonOutput, error)
	ListTagsForResource(context.Context, *eks.ListTagsForResourceInput,
		...func(*eks.Options)) (*eks.ListTagsForResourceOutput, error)
	TagResource(context.Context, *eks.TagResourceInput,
		...func(*eks.Options)) (*eks.TagResourceOutput, error)
	UntagResource(context.Context, *eks.UntagResourceInput,
		...func(*eks.Options)) (*eks.UntagResourceOutput, error)
}
