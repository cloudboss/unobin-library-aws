package wafv2

import (
	"context"
	"errors"

	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type wafClient interface {
	CreateWebACL(context.Context, *awssvc.CreateWebACLInput,
		...func(*awssvc.Options)) (*awssvc.CreateWebACLOutput, error)
	DeleteWebACL(context.Context, *awssvc.DeleteWebACLInput,
		...func(*awssvc.Options)) (*awssvc.DeleteWebACLOutput, error)
	GetWebACL(context.Context, *awssvc.GetWebACLInput,
		...func(*awssvc.Options)) (*awssvc.GetWebACLOutput, error)
	ListTagsForResource(context.Context, *awssvc.ListTagsForResourceInput,
		...func(*awssvc.Options)) (*awssvc.ListTagsForResourceOutput, error)
	TagResource(context.Context, *awssvc.TagResourceInput,
		...func(*awssvc.Options)) (*awssvc.TagResourceOutput, error)
	UntagResource(context.Context, *awssvc.UntagResourceInput,
		...func(*awssvc.Options)) (*awssvc.UntagResourceOutput, error)
	UpdateWebACL(context.Context, *awssvc.UpdateWebACLInput,
		...func(*awssvc.Options)) (*awssvc.UpdateWebACLOutput, error)
}

func newClient(ctx context.Context, cfg *awsCfg) (*awssvc.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return awssvc.NewFromConfig(loaded), nil
}

func isWebACLNotFound(err error) bool {
	var target *awstypes.WAFNonexistentItemException
	return errors.As(err, &target)
}

func isWebACLUnavailable(err error) bool {
	var target *awstypes.WAFUnavailableEntityException
	return errors.As(err, &target)
}

func isWebACLAssociated(err error) bool {
	var target *awstypes.WAFAssociatedItemException
	return errors.As(err, &target)
}

func isWebACLLockConflict(err error) bool {
	var target *awstypes.WAFOptimisticLockException
	return errors.As(err, &target)
}
