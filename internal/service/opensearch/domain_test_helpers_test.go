package opensearch

import (
	"context"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
)

type recordingDomainClock struct {
	now    time.Time
	sleeps []time.Duration
	err    error
}

func newRecordingDomainClock() *recordingDomainClock {
	return &recordingDomainClock{now: time.Unix(0, 0).UTC()}
}

func (clock *recordingDomainClock) Now() time.Time { return clock.now }

func (clock *recordingDomainClock) Sleep(
	_ context.Context,
	delay time.Duration,
) error {
	clock.sleeps = append(clock.sleeps, delay)
	if clock.err != nil {
		return clock.err
	}
	clock.now = clock.now.Add(delay)
	return nil
}

type fakeDomainClient struct {
	addTags               func(context.Context, *awssdk.AddTagsInput) (*awssdk.AddTagsOutput, error)
	createDomain          func(context.Context, *awssdk.CreateDomainInput) (*awssdk.CreateDomainOutput, error)
	deleteDomain          func(context.Context, *awssdk.DeleteDomainInput) (*awssdk.DeleteDomainOutput, error)
	describeDomain        func(context.Context, *awssdk.DescribeDomainInput) (*awssdk.DescribeDomainOutput, error)
	describeDomainConfig  func(context.Context, *awssdk.DescribeDomainConfigInput) (*awssdk.DescribeDomainConfigOutput, error)
	getCompatibleVersions func(context.Context, *awssdk.GetCompatibleVersionsInput) (*awssdk.GetCompatibleVersionsOutput, error)
	getUpgradeStatus      func(context.Context, *awssdk.GetUpgradeStatusInput) (*awssdk.GetUpgradeStatusOutput, error)
	listTags              func(context.Context, *awssdk.ListTagsInput) (*awssdk.ListTagsOutput, error)
	removeTags            func(context.Context, *awssdk.RemoveTagsInput) (*awssdk.RemoveTagsOutput, error)
	updateDomainConfig    func(context.Context, *awssdk.UpdateDomainConfigInput) (*awssdk.UpdateDomainConfigOutput, error)
	upgradeDomain         func(context.Context, *awssdk.UpgradeDomainInput) (*awssdk.UpgradeDomainOutput, error)
}

func (c *fakeDomainClient) AddTags(
	ctx context.Context,
	input *awssdk.AddTagsInput,
	_ ...func(*awssdk.Options),
) (*awssdk.AddTagsOutput, error) {
	if c.addTags == nil {
		return &awssdk.AddTagsOutput{}, nil
	}
	return c.addTags(ctx, input)
}

func (c *fakeDomainClient) CreateDomain(
	ctx context.Context,
	input *awssdk.CreateDomainInput,
	_ ...func(*awssdk.Options),
) (*awssdk.CreateDomainOutput, error) {
	if c.createDomain == nil {
		return &awssdk.CreateDomainOutput{}, nil
	}
	return c.createDomain(ctx, input)
}

func (c *fakeDomainClient) DeleteDomain(
	ctx context.Context,
	input *awssdk.DeleteDomainInput,
	_ ...func(*awssdk.Options),
) (*awssdk.DeleteDomainOutput, error) {
	if c.deleteDomain == nil {
		return &awssdk.DeleteDomainOutput{}, nil
	}
	return c.deleteDomain(ctx, input)
}

func (c *fakeDomainClient) DescribeDomain(
	ctx context.Context,
	input *awssdk.DescribeDomainInput,
	_ ...func(*awssdk.Options),
) (*awssdk.DescribeDomainOutput, error) {
	if c.describeDomain == nil {
		return &awssdk.DescribeDomainOutput{}, nil
	}
	return c.describeDomain(ctx, input)
}

func (c *fakeDomainClient) DescribeDomainConfig(
	ctx context.Context,
	input *awssdk.DescribeDomainConfigInput,
	_ ...func(*awssdk.Options),
) (*awssdk.DescribeDomainConfigOutput, error) {
	if c.describeDomainConfig == nil {
		return &awssdk.DescribeDomainConfigOutput{}, nil
	}
	return c.describeDomainConfig(ctx, input)
}

func (c *fakeDomainClient) GetCompatibleVersions(
	ctx context.Context,
	input *awssdk.GetCompatibleVersionsInput,
	_ ...func(*awssdk.Options),
) (*awssdk.GetCompatibleVersionsOutput, error) {
	if c.getCompatibleVersions == nil {
		return &awssdk.GetCompatibleVersionsOutput{}, nil
	}
	return c.getCompatibleVersions(ctx, input)
}

func (c *fakeDomainClient) GetUpgradeStatus(
	ctx context.Context,
	input *awssdk.GetUpgradeStatusInput,
	_ ...func(*awssdk.Options),
) (*awssdk.GetUpgradeStatusOutput, error) {
	if c.getUpgradeStatus == nil {
		return &awssdk.GetUpgradeStatusOutput{}, nil
	}
	return c.getUpgradeStatus(ctx, input)
}

func (c *fakeDomainClient) ListTags(
	ctx context.Context,
	input *awssdk.ListTagsInput,
	_ ...func(*awssdk.Options),
) (*awssdk.ListTagsOutput, error) {
	if c.listTags == nil {
		return &awssdk.ListTagsOutput{}, nil
	}
	return c.listTags(ctx, input)
}

func (c *fakeDomainClient) RemoveTags(
	ctx context.Context,
	input *awssdk.RemoveTagsInput,
	_ ...func(*awssdk.Options),
) (*awssdk.RemoveTagsOutput, error) {
	if c.removeTags == nil {
		return &awssdk.RemoveTagsOutput{}, nil
	}
	return c.removeTags(ctx, input)
}

func (c *fakeDomainClient) UpdateDomainConfig(
	ctx context.Context,
	input *awssdk.UpdateDomainConfigInput,
	_ ...func(*awssdk.Options),
) (*awssdk.UpdateDomainConfigOutput, error) {
	if c.updateDomainConfig == nil {
		return &awssdk.UpdateDomainConfigOutput{}, nil
	}
	return c.updateDomainConfig(ctx, input)
}

func (c *fakeDomainClient) UpgradeDomain(
	ctx context.Context,
	input *awssdk.UpgradeDomainInput,
	_ ...func(*awssdk.Options),
) (*awssdk.UpgradeDomainOutput, error) {
	if c.upgradeDomain == nil {
		return &awssdk.UpgradeDomainOutput{}, nil
	}
	return c.upgradeDomain(ctx, input)
}
