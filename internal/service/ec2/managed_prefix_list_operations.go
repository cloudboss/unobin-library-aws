package ec2

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

const managedPrefixListNotFound = "InvalidPrefixListID.NotFound"

type managedPrefixListClient struct {
	ec2    *ec2.Client
	waiter managedPrefixListWaiter
}

func newManagedPrefixListClient(
	ctx context.Context, cfg *awsCfg,
) (*managedPrefixListClient, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &managedPrefixListClient{ec2: client, waiter: managedPrefixListWaiter{
		now: time.Now, sleep: networkACLSleep, timeout: 15 * time.Minute,
	}}, nil
}

func (r *ManagedPrefixListResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*ManagedPrefixListResourceOutput, error) {
	request := r.prepareRequest(rand.Text())
	c, err := newManagedPrefixListClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, c, request)
}

func (r *ManagedPrefixListResource) Read(
	ctx context.Context, cfg *awsCfg, prior managedPrefixListPrior,
) (*ManagedPrefixListResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.PrefixListId == "" {
		return nil, runtime.ErrNotFound
	}
	c, err := newManagedPrefixListClient(ctx, managedPrefixListPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	return c.read(ctx, prior.Outputs.PrefixListId)
}

func (r *ManagedPrefixListResource) Update(
	ctx context.Context, cfg *awsCfg, prior managedPrefixListPrior,
) (*ManagedPrefixListResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.PrefixListId == "" {
		return nil, errors.New("update managed prefix list requires a saved prefix list ID")
	}
	c, err := newManagedPrefixListClient(ctx, managedPrefixListPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	return r.update(ctx, c, prior)
}

func (r *ManagedPrefixListResource) Delete(
	ctx context.Context, cfg *awsCfg, prior managedPrefixListPrior,
) error {
	if prior.Outputs == nil || prior.Outputs.PrefixListId == "" {
		return nil
	}
	c, err := newManagedPrefixListClient(ctx, managedPrefixListPriorConfig(cfg, prior))
	if err != nil {
		return err
	}
	return c.delete(ctx, prior.Outputs.PrefixListId)
}

func managedPrefixListPriorConfig(cfg *awsCfg, prior managedPrefixListPrior) *awsCfg {
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	if prior.Outputs == nil || prior.Outputs.Region == "" {
		return cfg
	}
	saved := ptr.Value(cfg)
	saved.Region = aws.String(prior.Outputs.Region)
	return &saved
}

func (r *ManagedPrefixListResource) create(
	ctx context.Context, c *managedPrefixListClient, request *ec2.CreateManagedPrefixListInput,
) (*ManagedPrefixListResourceOutput, error) {
	response, err := c.ec2.CreateManagedPrefixList(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("create managed prefix list: %w", err)
	}
	if response == nil || response.PrefixList == nil ||
		aws.ToString(response.PrefixList.PrefixListId) == "" {
		return nil, errors.New("create managed prefix list returned no prefix list ID")
	}
	id := *response.PrefixList.PrefixListId
	fail := func(failure error) (*ManagedPrefixListResourceOutput, error) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if err := c.delete(cleanup, id); err != nil {
			failure = errors.Join(failure,
				fmt.Errorf("compensating delete managed prefix list %s: %w", id, err))
		}
		return nil, failure
	}
	if _, err := c.wait(ctx, id, "create"); err != nil {
		return fail(err)
	}
	output, err := c.read(ctx, id)
	if err != nil {
		return fail(err)
	}
	return output, nil
}

func (r *ManagedPrefixListResource) update(
	ctx context.Context, c *managedPrefixListClient, prior managedPrefixListPrior,
) (*ManagedPrefixListResourceOutput, error) {
	id := prior.Outputs.PrefixListId
	known := prior.Observed
	if known == nil {
		known = prior.Outputs
	}
	prepared := r.prepareRequest("")
	desiredTags, _ := tagsync.Diff(nil, ptr.Value(r.Tags))
	if err := syncEC2Tags(ctx, c.ec2, id, desiredTags,
		func(context.Context) (map[string]string, error) { return known.Tags, nil }); err != nil {
		return nil, err
	}
	if r.MaxEntries > known.MaxEntries {
		if err := c.capacity(ctx, id, prepared.MaxEntries); err != nil {
			return nil, err
		}
	}
	var additions []ec2types.AddPrefixListEntry
	var removals []ec2types.RemovePrefixListEntry
	current := managedPrefixListEntries(known.Entries)
	if r.Entries != nil {
		wanted := managedPrefixListEntries(*r.Entries)
		contains := func(entries []ManagedPrefixListEntry, value ManagedPrefixListEntry) bool {
			return slices.ContainsFunc(entries, func(entry ManagedPrefixListEntry) bool {
				return entry.Cidr == value.Cidr &&
					aws.ToString(entry.Description) == aws.ToString(value.Description)
			})
		}
		for _, entry := range wanted {
			if !contains(current, entry) {
				additions = append(additions, ec2types.AddPrefixListEntry{
					Cidr: aws.String(entry.Cidr), Description: entry.Description,
				})
			}
		}
		for _, entry := range current {
			if !contains(wanted, entry) {
				removals = append(removals, ec2types.RemovePrefixListEntry{Cidr: aws.String(entry.Cidr)})
			}
		}
	}
	version := known.Version
	changedEntries := len(additions) != 0 || len(removals) != 0
	if changedEntries {
		var overlap, remaining []ec2types.RemovePrefixListEntry
		for _, removal := range removals {
			if slices.ContainsFunc(additions, func(add ec2types.AddPrefixListEntry) bool {
				return aws.ToString(add.Cidr) == aws.ToString(removal.Cidr)
			}) {
				overlap = append(overlap, removal)
			} else {
				remaining = append(remaining, removal)
			}
		}
		if len(overlap) != 0 {
			if _, err := c.ec2.ModifyManagedPrefixList(ctx, &ec2.ModifyManagedPrefixListInput{
				PrefixListId: aws.String(id), CurrentVersion: aws.Int64(version), RemoveEntries: overlap,
			}); err != nil {
				return nil, fmt.Errorf("remove overlapping entries from %s: %w", id, err)
			}
			waited, err := c.wait(ctx, id, "modify")
			if err != nil {
				return nil, err
			}
			version, removals = aws.ToInt64(waited.Version), remaining
		}
	}
	if changedEntries || r.Name != known.Name {
		request := &ec2.ModifyManagedPrefixListInput{
			PrefixListId: aws.String(id), PrefixListName: prepared.PrefixListName,
			AddEntries: additions, RemoveEntries: removals,
		}
		if changedEntries {
			request.CurrentVersion = aws.Int64(version)
		}
		if _, err := c.ec2.ModifyManagedPrefixList(ctx, request); err != nil {
			return nil, fmt.Errorf("modify managed prefix list %s: %w", id, err)
		}
		if changedEntries {
			if _, err := c.wait(ctx, id, "modify"); err != nil {
				return nil, err
			}
		}
	}
	if r.MaxEntries < known.MaxEntries {
		if err := c.capacity(ctx, id, prepared.MaxEntries); err != nil {
			return nil, err
		}
	}
	return c.read(ctx, id)
}

func (c *managedPrefixListClient) capacity(ctx context.Context, id string, capacity *int32) error {
	if _, err := c.ec2.ModifyManagedPrefixList(ctx, &ec2.ModifyManagedPrefixListInput{
		PrefixListId: aws.String(id), MaxEntries: capacity,
	}); err != nil {
		return fmt.Errorf("modify managed prefix list %s capacity: %w", id, err)
	}
	_, err := c.wait(ctx, id, "modify")
	return err
}

func (c *managedPrefixListClient) lookup(
	ctx context.Context, id string,
) (*ec2types.ManagedPrefixList, error) {
	pages := ec2.NewDescribeManagedPrefixListsPaginator(c.ec2,
		&ec2.DescribeManagedPrefixListsInput{PrefixListIds: []string{id}})
	var results []ec2types.ManagedPrefixList
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if isNotFound(err, managedPrefixListNotFound) {
			return nil, runtime.ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("describe managed prefix list %s: %w", id, err)
		}
		results = append(results, page.PrefixLists...)
	}
	if len(results) != 1 || aws.ToString(results[0].PrefixListId) != id ||
		results[0].State == ec2types.PrefixListStateDeleteComplete {
		return nil, runtime.ErrNotFound
	}
	return &results[0], nil
}

func (c *managedPrefixListClient) read(
	ctx context.Context, id string,
) (*ManagedPrefixListResourceOutput, error) {
	list, err := c.lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	pages := ec2.NewGetManagedPrefixListEntriesPaginator(c.ec2,
		&ec2.GetManagedPrefixListEntriesInput{PrefixListId: aws.String(id)})
	entries := []ManagedPrefixListEntry{}
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("read managed prefix list %s entries: %w", id, err)
		}
		for _, entry := range page.Entries {
			entries = append(entries, ManagedPrefixListEntry{
				Cidr: aws.ToString(entry.Cidr), Description: entry.Description,
			})
		}
	}
	tags := map[string]string{}
	for _, tag := range list.Tags {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	tags, _ = tagsync.Diff(nil, tags)
	return &ManagedPrefixListResourceOutput{
		PrefixListId: id, AddressFamily: aws.ToString(list.AddressFamily),
		Arn: aws.ToString(list.PrefixListArn), MaxEntries: int64(aws.ToInt32(list.MaxEntries)),
		Name: aws.ToString(list.PrefixListName), OwnerId: aws.ToString(list.OwnerId),
		Version: aws.ToInt64(list.Version), Entries: managedPrefixListEntries(entries),
		Tags: tags, Region: region(c.ec2),
	}, nil
}

func (c *managedPrefixListClient) wait(
	ctx context.Context, id, operation string,
) (*ec2types.ManagedPrefixList, error) {
	target := operation + "-complete"
	if operation == "delete" {
		target = ""
	}
	result, err := c.waiter.wait(ctx, operation+"-in-progress", target,
		func(ctx context.Context) (*ec2types.ManagedPrefixList, error) { return c.lookup(ctx, id) })
	if err != nil {
		if result != nil && string(result.State) == operation+"-failed" &&
			aws.ToString(result.StateMessage) != "" {
			err = fmt.Errorf("%w: %s", err, *result.StateMessage)
		}
		return result, fmt.Errorf("wait for managed prefix list %s %s: %w", id, operation, err)
	}
	return result, nil
}

func (c *managedPrefixListClient) delete(ctx context.Context, id string) error {
	_, err := c.ec2.DeleteManagedPrefixList(ctx, &ec2.DeleteManagedPrefixListInput{
		PrefixListId: aws.String(id),
	})
	if isNotFound(err, managedPrefixListNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete managed prefix list %s: %w", id, err)
	}
	_, err = c.wait(ctx, id, "delete")
	return err
}
