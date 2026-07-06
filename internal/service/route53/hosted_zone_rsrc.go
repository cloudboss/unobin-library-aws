package route53

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	route53 "github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

// zoneRecordDeleteBatch is the most record changes Route 53 accepts in one
// ChangeResourceRecordSets call. A force-destroy purge splits the zone's
// records into batches no larger than this.
const zoneRecordDeleteBatch = 100

// HostedZoneResource manages a Route 53 hosted zone: a DNS namespace, either public or
// private. The name and the reusable delegation set are fixed at creation, so a
// change to either replaces the zone; the comment, the tags, and the VPC
// associations reconcile in place. Associating any VPC makes the zone private,
// and there is no flag to flip a zone between public and private, so the set of
// VPCs is what decides the kind. ForceDestroy is a delete-time switch: when it
// is set, the zone's own records are purged before the zone is deleted, since
// Route 53 refuses to delete a zone that still holds records.
type HostedZoneResource struct {
	Name            string             `ub:"name"`
	Comment         *string            `ub:"comment"`
	DelegationSetId *string            `ub:"delegation-set-id"`
	Vpcs            *[]HostedZoneVpc   `ub:"vpcs"`
	ForceDestroy    *bool              `ub:"force-destroy"`
	Tags            *map[string]string `ub:"tags"`
}

// HostedZoneVpc is one VPC associated with a private hosted zone. The id names
// the VPC. The region is the region the VPC lives in; when it is omitted, the
// region the call is made from is used, the same region Route 53 would assume.
type HostedZoneVpc struct {
	VpcId     string  `ub:"vpc-id"`
	VpcRegion *string `ub:"vpc-region"`
}

// HostedZoneResourceOutput holds the values Route 53 computes for a hosted zone. ZoneId
// is the stable handle used to read, update, and delete the zone and to attach
// records to it. Arn names the zone in policies; a hosted zone ARN is global,
// naming neither region nor account. Name is the zone's name with the trailing
// dot stripped, the form downstream records and delegations match against.
// NameServers is the zone's authoritative name server set, sorted.
// PrimaryNameServer is the lead server: the first of a public zone's delegation
// set as Route 53 returns it, or the first of a private zone's sorted set.
type HostedZoneResourceOutput struct {
	ZoneId            string   `ub:"zone-id"`
	Arn               string   `ub:"arn"`
	Name              string   `ub:"name"`
	NameServers       []string `ub:"name-servers"`
	PrimaryNameServer string   `ub:"primary-name-server"`
}

func (r *HostedZoneResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs Route 53 fixes when a zone is created. The name
// is the zone's identity and Route 53 offers no rename, and a reusable
// delegation set can only be chosen at creation, so a change to either requires
// a new zone. The comment, the tags, and the VPC associations reconcile in
// place. ForceDestroy never reaches create, so it is not a replace trigger.
func (r *HostedZoneResource) ReplaceFields() []string {
	return []string{"name", "delegation-set-id"}
}

// Constraints declares the rules Route 53 places on a zone's inputs. A reusable
// delegation set belongs to a public zone, while any VPC makes the zone
// private, so the two are mutually exclusive, and every VPC association must
// name a VPC. The name, comment, and delegation-set-id length bounds are
// counted in characters, which the constraint layer measures in bytes, so they
// are checked in validate rather than declared here.
func (r HostedZoneResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Any(
			constraint.Absent(r.DelegationSetId),
			constraint.Not(constraint.NotEmpty(r.Vpcs)))).
			Message("delegation-set-id and vpcs are mutually exclusive"),
		constraint.ForEach(r.Vpcs, func(v HostedZoneVpc) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.NotEmpty(v.VpcId)).
					Message("a vpc association requires a vpc-id"),
			}
		}),
	}
}

// validate checks the length bounds Route 53 enforces but the constraint layer
// cannot express, since it counts string length in bytes rather than in the
// characters Route 53 limits: the name is 1 to 1024 characters, the comment at
// most 256, and the delegation-set-id at most 32.
func (r *HostedZoneResource) validate() error {
	n := len(r.Name)
	if n < 1 || n > 1024 {
		return errors.New("name must be between 1 and 1024 characters")
	}
	if r.Comment != nil && len(*r.Comment) > 256 {
		return errors.New("comment must be at most 256 characters")
	}
	if r.DelegationSetId != nil && len(*r.DelegationSetId) > 32 {
		return errors.New("delegation-set-id must be at most 32 characters")
	}
	return nil
}

func (r *HostedZoneResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*HostedZoneResourceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	region := client.Options().Region
	in := &route53.CreateHostedZoneInput{
		CallerReference: aws.String(zoneCallerReference()),
		Name:            aws.String(r.Name),
	}
	if r.Comment != nil {
		in.HostedZoneConfig = &route53types.HostedZoneConfig{Comment: r.Comment}
	}
	if r.DelegationSetId != nil {
		in.DelegationSetId = r.DelegationSetId
	}
	// A private zone can be created with only its first VPC; the rest are
	// associated after the zone exists. Setting any VPC on the create is what
	// makes the zone private.
	if len(ptr.Value(r.Vpcs)) > 0 {
		in.VPC = ptr.Value(r.Vpcs)[0].toSDK(region)
	}
	resp, err := client.CreateHostedZone(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("create hosted zone: %w", err)
	}
	zoneID := cleanZoneID(aws.ToString(resp.HostedZone.Id))
	// The create returns a change that is still propagating to the Route 53 DNS
	// servers. Waiting for it to reach INSYNC keeps a record added right after
	// from racing a zone that is not yet live.
	if resp.ChangeInfo != nil {
		if err := waitChangeInsync(ctx, client, aws.ToString(resp.ChangeInfo.Id)); err != nil {
			return nil, err
		}
	}
	// The remaining VPCs are associated one at a time, each waited to INSYNC, so
	// every association is in place before the create returns. A public zone has
	// no VPCs and a private zone's first one rode the create, so there is nothing
	// to do unless more than one was given.
	if len(ptr.Value(r.Vpcs)) > 1 {
		for _, v := range ptr.Value(r.Vpcs)[1:] {
			if err := r.associateVPC(ctx, client, zoneID, v.toSDK(region)); err != nil {
				return nil, err
			}
		}
	}
	if len(ptr.Value(r.Tags)) > 0 {
		if err := r.syncTags(ctx, client, zoneID); err != nil {
			return nil, err
		}
	}
	// The create response does not include the settled name servers for a private
	// zone, and downstream records need the live values, so the outputs come from
	// a read after the zone is in sync.
	return r.read(ctx, client, zoneID)
}

func (r *HostedZoneResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *HostedZoneResourceOutput,
) (*HostedZoneResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.ZoneId)
}

// read fetches the zone by id and computes its outputs. A gone zone maps to
// runtime.ErrNotFound so a plan recreates it. The name servers come from the
// delegation set for a public zone; a private zone has an empty delegation-set
// list, so its servers are read from the zone's own apex NS record. The primary
// name server is the first server as Route 53 returns it, taken before the list
// is sorted, since a registrar expects that order.
func (r *HostedZoneResource) read(
	ctx context.Context, client *route53.Client, zoneID string,
) (*HostedZoneResourceOutput, error) {
	resp, err := client.GetHostedZone(ctx, &route53.GetHostedZoneInput{
		Id: aws.String(zoneID),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get hosted zone %s: %w", zoneID, err)
	}
	name := aws.ToString(resp.HostedZone.Name)
	nameServers := []string{}
	if resp.DelegationSet != nil {
		nameServers = resp.DelegationSet.NameServers
	}
	private := resp.HostedZone.Config != nil && resp.HostedZone.Config.PrivateZone
	if private || len(nameServers) == 0 {
		nameServers, err = r.findNameServers(ctx, client, zoneID, name)
		if err != nil {
			return nil, err
		}
		// A public zone's delegation set lists its servers in a fixed order, but a
		// private zone's are read from its apex NS record in no set order, so sort
		// them to give a deterministic primary, the order Terraform reports.
		slices.Sort(nameServers)
	}
	primary := ""
	if len(nameServers) > 0 {
		primary = nameServers[0]
	}
	sorted := slices.Clone(nameServers)
	slices.Sort(sorted)
	return &HostedZoneResourceOutput{
		ZoneId:            zoneID,
		Arn:               zoneARN(zoneRegion(client), zoneID),
		Name:              normalizeName(name),
		NameServers:       sorted,
		PrimaryNameServer: primary,
	}, nil
}

func (r *HostedZoneResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[HostedZoneResource, *HostedZoneResourceOutput],
) (*HostedZoneResourceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	zoneID := prior.Outputs.ZoneId
	region := client.Options().Region
	// The comment is reconciled only when it changed. Route 53 clears the comment
	// when none is sent, so an omitted comment after one was set removes it.
	if runtime.Changed(prior.Inputs.Comment, r.Comment) {
		_, err := client.UpdateHostedZoneComment(ctx, &route53.UpdateHostedZoneCommentInput{
			Id:      aws.String(zoneID),
			Comment: r.Comment,
		})
		if err != nil {
			return nil, fmt.Errorf("update hosted zone comment %s: %w", zoneID, err)
		}
	}
	// A private zone must always keep at least one VPC, so a changed VPC set adds
	// the new associations before removing the old ones.
	if runtime.Changed(prior.Inputs.Vpcs, r.Vpcs) {
		if err := r.reconcileVPCs(ctx, client, zoneID, region, ptr.Value(prior.Inputs.Vpcs)); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, zoneID); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, zoneID)
}

func (r *HostedZoneResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *HostedZoneResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	zoneID := prior.ZoneId
	// A pre-delete read confirms the zone is still there; a gone zone is already
	// deleted and needs no further work.
	_, err = client.GetHostedZone(ctx, &route53.GetHostedZoneInput{Id: aws.String(zoneID)})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("get hosted zone %s: %w", zoneID, err)
	}
	// Route 53 refuses to delete a zone that still holds records. With force
	// destroy the zone's own records, all but the apex NS and SOA that Route 53
	// manages, are removed first so the delete can proceed.
	if aws.ToBool(r.ForceDestroy) {
		if err := r.purgeRecords(ctx, client, zoneID); err != nil {
			return err
		}
	}
	resp, err := client.DeleteHostedZone(ctx, &route53.DeleteHostedZoneInput{
		Id: aws.String(zoneID),
	})
	if err != nil {
		// A zone already gone counts as deleted.
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete hosted zone %s: %w", zoneID, err)
	}
	// The delete returns a change that is still propagating. Waiting for INSYNC
	// confirms the zone is gone everywhere before the delete is reported done.
	if resp.ChangeInfo != nil {
		if err := waitChangeInsync(ctx, client, aws.ToString(resp.ChangeInfo.Id)); err != nil {
			return err
		}
	}
	return nil
}

// reconcileVPCs brings the zone's VPC associations to the desired set. The new
// associations are added before any old one is removed, because Route 53
// rejects a private zone left with no VPC. Each association and disassociation
// returns a change that is waited to INSYNC.
func (r *HostedZoneResource) reconcileVPCs(
	ctx context.Context, client *route53.Client, zoneID, region string, prior []HostedZoneVpc,
) error {
	priorByID := zoneVPCsByID(prior)
	desiredByID := zoneVPCsByID(ptr.Value(r.Vpcs))
	for _, v := range ptr.Value(r.Vpcs) {
		if _, ok := priorByID[v.VpcId]; ok {
			continue
		}
		if err := r.associateVPC(ctx, client, zoneID, v.toSDK(region)); err != nil {
			return err
		}
	}
	for _, v := range prior {
		if _, ok := desiredByID[v.VpcId]; ok {
			continue
		}
		if err := r.disassociateVPC(ctx, client, zoneID, v.toSDK(region)); err != nil {
			return err
		}
	}
	return nil
}

// associateVPC adds one VPC to the zone and waits for the association to reach
// INSYNC.
func (r *HostedZoneResource) associateVPC(
	ctx context.Context, client *route53.Client, zoneID string, vpc *route53types.VPC,
) error {
	resp, err := client.AssociateVPCWithHostedZone(ctx, &route53.AssociateVPCWithHostedZoneInput{
		HostedZoneId: aws.String(zoneID),
		VPC:          vpc,
	})
	if err != nil {
		return fmt.Errorf("associate vpc with hosted zone %s: %w", zoneID, err)
	}
	if resp.ChangeInfo == nil {
		return nil
	}
	return waitChangeInsync(ctx, client, aws.ToString(resp.ChangeInfo.Id))
}

// disassociateVPC removes one VPC from the zone and waits for the change to
// reach INSYNC.
func (r *HostedZoneResource) disassociateVPC(
	ctx context.Context, client *route53.Client, zoneID string, vpc *route53types.VPC,
) error {
	resp, err := client.DisassociateVPCFromHostedZone(
		ctx, &route53.DisassociateVPCFromHostedZoneInput{
			HostedZoneId: aws.String(zoneID),
			VPC:          vpc,
		})
	if err != nil {
		return fmt.Errorf("disassociate vpc from hosted zone %s: %w", zoneID, err)
	}
	if resp.ChangeInfo == nil {
		return nil
	}
	return waitChangeInsync(ctx, client, aws.ToString(resp.ChangeInfo.Id))
}

// findNameServers reads a private zone's authoritative name servers from its own
// apex NS record, the way a private zone reports them, since its delegation set
// has no servers. The listing starts at the zone's own name and NS type and
// returns the values of the matching record.
func (r *HostedZoneResource) findNameServers(
	ctx context.Context, client *route53.Client, zoneID, name string,
) ([]string, error) {
	resp, err := client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(zoneID),
		StartRecordName: aws.String(name),
		StartRecordType: route53types.RRTypeNs,
		MaxItems:        aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("list resource record sets %s: %w", zoneID, err)
	}
	want := normalizeName(name)
	for _, rs := range resp.ResourceRecordSets {
		if rs.Type != route53types.RRTypeNs {
			continue
		}
		if normalizeName(aws.ToString(rs.Name)) != want {
			continue
		}
		servers := make([]string, 0, len(rs.ResourceRecords))
		for _, rec := range rs.ResourceRecords {
			servers = append(servers, aws.ToString(rec.Value))
		}
		return servers, nil
	}
	return []string{}, nil
}

// purgeRecords removes every record in the zone except the apex NS and SOA that
// Route 53 manages and that the delete itself clears. The records are listed,
// the apex NS and SOA filtered out by their normalized name, and the rest
// deleted in batches no larger than the Route 53 per-request limit, each batch
// waited to INSYNC. A zone that vanishes mid-purge, from a concurrent delete,
// is treated as already gone.
func (r *HostedZoneResource) purgeRecords(
	ctx context.Context, client *route53.Client, zoneID string,
) error {
	apex, err := r.zoneApexName(ctx, client, zoneID)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	var changes []route53types.Change
	pager := route53.NewListResourceRecordSetsPaginator(client,
		&route53.ListResourceRecordSetsInput{HostedZoneId: aws.String(zoneID)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list resource record sets %s: %w", zoneID, err)
		}
		for i := range page.ResourceRecordSets {
			rs := page.ResourceRecordSets[i]
			if zoneIsApexNSOrSOA(rs, apex) {
				continue
			}
			changes = append(changes, route53types.Change{
				Action:            route53types.ChangeActionDelete,
				ResourceRecordSet: &rs,
			})
		}
	}
	for start := 0; start < len(changes); start += zoneRecordDeleteBatch {
		end := min(start+zoneRecordDeleteBatch, len(changes))
		if err := r.deleteRecordBatch(ctx, client, zoneID, changes[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// deleteRecordBatch submits one batch of record deletions and waits for it to
// reach INSYNC. A zone gone underneath is treated as already purged. An
// InvalidChangeBatch holds its own per-change messages, which are unpacked
// into the returned error so the operator sees which records Route 53 refused.
func (r *HostedZoneResource) deleteRecordBatch(
	ctx context.Context, client *route53.Client, zoneID string, batch []route53types.Change,
) error {
	resp, err := client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch:  &route53types.ChangeBatch{Changes: batch},
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		var batchErr *route53types.InvalidChangeBatch
		if errors.As(err, &batchErr) && len(batchErr.Messages) > 0 {
			return fmt.Errorf("delete records %s: %s",
				zoneID, strings.Join(batchErr.Messages, "; "))
		}
		return fmt.Errorf("delete records %s: %w", zoneID, err)
	}
	if resp.ChangeInfo == nil {
		return nil
	}
	return waitChangeInsync(ctx, client, aws.ToString(resp.ChangeInfo.Id))
}

// zoneApexName returns the zone's own name, used to recognize the apex NS and
// SOA records a purge must leave for the delete to clear.
func (r *HostedZoneResource) zoneApexName(
	ctx context.Context, client *route53.Client, zoneID string,
) (string, error) {
	resp, err := client.GetHostedZone(ctx, &route53.GetHostedZoneInput{Id: aws.String(zoneID)})
	if err != nil {
		return "", err
	}
	return aws.ToString(resp.HostedZone.Name), nil
}

// syncTags reconciles the zone's tags with the desired set. Route 53 reads tags
// for one resource at a time and writes adds and removes in a single
// ChangeTagsForResource, so the live set is read, diffed, and the call made only
// when something differs.
func (r *HostedZoneResource) syncTags(
	ctx context.Context, client *route53.Client, zoneID string,
) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx, &route53.ListTagsForResourceInput{
				ResourceId:   aws.String(zoneID),
				ResourceType: route53types.TagResourceTypeHostedzone,
			})
			if err != nil {
				return nil, fmt.Errorf("list tags for hosted zone %s: %w", zoneID, err)
			}
			current := map[string]string{}
			if resp.ResourceTagSet != nil {
				for _, t := range resp.ResourceTagSet.Tags {
					current[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.ChangeTagsForResource(ctx, &route53.ChangeTagsForResourceInput{
				ResourceId:   aws.String(zoneID),
				ResourceType: route53types.TagResourceTypeHostedzone,
				AddTags:      zoneTags(upsert),
			})
			if err != nil {
				return fmt.Errorf("add tags to hosted zone %s: %w", zoneID, err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.ChangeTagsForResource(ctx, &route53.ChangeTagsForResourceInput{
				ResourceId:    aws.String(zoneID),
				ResourceType:  route53types.TagResourceTypeHostedzone,
				RemoveTagKeys: remove,
			})
			if err != nil {
				return fmt.Errorf("remove tags from hosted zone %s: %w", zoneID, err)
			}
			return nil
		},
	)
}
