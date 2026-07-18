package opensearch

import (
	"context"

	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

type domainClient interface {
	AddTags(context.Context, *awssdk.AddTagsInput,
		...func(*awssdk.Options)) (*awssdk.AddTagsOutput, error)
	CreateDomain(context.Context, *awssdk.CreateDomainInput,
		...func(*awssdk.Options)) (*awssdk.CreateDomainOutput, error)
	DeleteDomain(context.Context, *awssdk.DeleteDomainInput,
		...func(*awssdk.Options)) (*awssdk.DeleteDomainOutput, error)
	DescribeDomain(context.Context, *awssdk.DescribeDomainInput,
		...func(*awssdk.Options)) (*awssdk.DescribeDomainOutput, error)
	DescribeDomainConfig(context.Context, *awssdk.DescribeDomainConfigInput,
		...func(*awssdk.Options)) (*awssdk.DescribeDomainConfigOutput, error)
	GetCompatibleVersions(context.Context, *awssdk.GetCompatibleVersionsInput,
		...func(*awssdk.Options)) (*awssdk.GetCompatibleVersionsOutput, error)
	GetUpgradeStatus(context.Context, *awssdk.GetUpgradeStatusInput,
		...func(*awssdk.Options)) (*awssdk.GetUpgradeStatusOutput, error)
	ListTags(context.Context, *awssdk.ListTagsInput,
		...func(*awssdk.Options)) (*awssdk.ListTagsOutput, error)
	RemoveTags(context.Context, *awssdk.RemoveTagsInput,
		...func(*awssdk.Options)) (*awssdk.RemoveTagsOutput, error)
	UpdateDomainConfig(context.Context, *awssdk.UpdateDomainConfigInput,
		...func(*awssdk.Options)) (*awssdk.UpdateDomainConfigOutput, error)
	UpgradeDomain(context.Context, *awssdk.UpgradeDomainInput,
		...func(*awssdk.Options)) (*awssdk.UpgradeDomainOutput, error)
}

func newClient(ctx context.Context, cfg *awsCfg) (*awssdk.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return awssdk.NewFromConfig(loaded), nil
}
