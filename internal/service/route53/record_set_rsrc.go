package route53

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	route53 "github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

// zonePropagationTimeout bounds the create-time retry that waits out a hosted
// zone that was made moments earlier and is not yet visible to the change call.
// AWS clears this race in well under a minute.
const zonePropagationTimeout = time.Minute

// RecordSet manages one Route 53 record set: a name, a type, and either a set
// of plain values or an alias to an AWS resource, optionally governed by a single
// routing policy. Every write -- create and update alike -- is one
// ChangeResourceRecordSets UPSERT against the hosted zone, gated by a poll until
// the change reaches INSYNC. The hosted zone, the name, the type, and the
// set-identifier together identify the record set at the API, so a change to any
// of them is a different record set and replaces this one; every other field is
// reconciled in place by the next UPSERT.
type RecordSet struct {
	ZoneId                        string                             `ub:"zone-id"`
	Name                          string                             `ub:"name"`
	Type                          string                             `ub:"type"`
	SetIdentifier                 *string                            `ub:"set-identifier"`
	Records                       []string                           `ub:"records"`
	Ttl                           *int64                             `ub:"ttl"`
	HealthCheckId                 *string                            `ub:"health-check-id"`
	Alias                         *RecordSetAlias                    `ub:"alias"`
	WeightedRoutingPolicy         *RecordSetWeightedRoutingPolicy    `ub:"weighted-routing-policy"`
	LatencyRoutingPolicy          *RecordSetLatencyRoutingPolicy     `ub:"latency-routing-policy"`
	FailoverRoutingPolicy         *RecordSetFailoverRoutingPolicy    `ub:"failover-routing-policy"`
	GeolocationRoutingPolicy      *RecordSetGeolocationRoutingPolicy `ub:"geolocation-routing-policy"`
	MultivalueAnswerRoutingPolicy *bool                              `ub:"multivalue-answer-routing-policy"`
}

// RecordSetOutput holds the record set's computed fqdn and the four-part key
// that names it at the API. Fqdn is the record name joined to the zone name, with
// an octal-escaped wildcard label restored to a leading "*". A record set has no
// server-assigned id, so its identity -- zone id, name, type, and set-identifier
// -- is the handle Delete addresses; holding it here lets a replace remove the
// old record set, since the runtime hands Delete the prior output, not the prior
// inputs.
type RecordSetOutput struct {
	Fqdn          string `ub:"fqdn"`
	ZoneId        string `ub:"zone-id"`
	Name          string `ub:"name"`
	Type          string `ub:"type"`
	SetIdentifier string `ub:"set-identifier"`
}

func (r *RecordSet) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that identify the record set at the API. The
// hosted zone, the name, the type, and the set-identifier are the four-part key
// Route 53 addresses a record set by, so changing any of them names a different
// record set; unobin's replace deletes the old one and creates the new. Every
// other field updates in place.
func (r *RecordSet) ReplaceFields() []string {
	return []string{
		"zone-id",
		"name",
		"type",
		"set-identifier",
	}
}

// Defaults marks the one collection input a record set may omit. The routing
// policy and alias blocks are pointers and are omittable through the pointer
// itself, so they are not marked here.
func (r RecordSet) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Records),
	}
}

// Constraints declares the rules Route 53 places on a record set's inputs. The
// hosted zone id must be a non-empty string. Exactly one of alias and records
// supplies the answer. A TTL belongs to plain records and is meaningless on an
// alias, which follows its target's TTL. At most one routing policy applies, and
// any routing policy needs a set-identifier to tell the records in its group
// apart. The failover type, the latency region, and the top-level record type
// each accept a fixed set of values.
func (r RecordSet) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.ZoneId)),
		constraint.ExactlyOneOf(r.Alias, r.Records),
		constraint.ForbiddenWith(r.Ttl, r.Alias),
		constraint.RequiredWith(r.Ttl, r.Records),
		constraint.AtMostOneOf(
			r.WeightedRoutingPolicy,
			r.LatencyRoutingPolicy,
			r.FailoverRoutingPolicy,
			r.GeolocationRoutingPolicy,
			r.MultivalueAnswerRoutingPolicy,
		),
		constraint.RequiredWith(r.WeightedRoutingPolicy, r.SetIdentifier),
		constraint.RequiredWith(r.LatencyRoutingPolicy, r.SetIdentifier),
		constraint.RequiredWith(r.FailoverRoutingPolicy, r.SetIdentifier),
		constraint.RequiredWith(r.GeolocationRoutingPolicy, r.SetIdentifier),
		constraint.RequiredWith(r.MultivalueAnswerRoutingPolicy, r.SetIdentifier),
		constraint.When(constraint.Present(r.FailoverRoutingPolicy)).
			Require(constraint.OneOf(r.FailoverRoutingPolicy.Type, "PRIMARY", "SECONDARY")).
			Message("failover-routing-policy type must be PRIMARY or SECONDARY"),
		constraint.When(constraint.Present(r.LatencyRoutingPolicy)).
			Require(constraint.OneOf(r.LatencyRoutingPolicy.Region,
				"us-east-1", "us-east-2", "us-west-1", "us-west-2",
				"ca-central-1", "ca-west-1",
				"eu-west-1", "eu-west-2", "eu-west-3",
				"eu-central-1", "eu-central-2", "eu-north-1",
				"eu-south-1", "eu-south-2",
				"ap-east-1", "ap-east-2", "ap-south-1", "ap-south-2",
				"ap-southeast-1", "ap-southeast-2", "ap-southeast-3",
				"ap-southeast-4", "ap-southeast-5", "ap-southeast-6", "ap-southeast-7",
				"ap-northeast-1", "ap-northeast-2", "ap-northeast-3",
				"sa-east-1", "me-south-1", "me-central-1", "af-south-1",
				"il-central-1", "mx-central-1",
				"cn-north-1", "cn-northwest-1",
				"us-gov-east-1", "us-gov-west-1", "eusc-de-east-1")).
			Message("latency-routing-policy region must be a valid AWS region"),
		constraint.Must(constraint.OneOf(r.Type,
			"A", "AAAA", "CAA", "CNAME", "DS", "MX", "NAPTR", "NS",
			"PTR", "SOA", "SPF", "SRV", "TXT", "TLSA", "SSHFP", "SVCB", "HTTPS")),
	}
}

func (r *RecordSet) Create(ctx context.Context, cfg *awsCfg) (*RecordSetOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	zone, err := r.hostedZone(ctx, client)
	if err != nil {
		return nil, err
	}
	rrs := r.resourceRecordSet(zone.name)
	// A hosted zone created moments earlier may not be visible to the change
	// call yet. Only Create retries this race; Update and Delete act on a zone
	// the prior state already proves exists.
	err = retry.OnError(ctx, isNotFound, func(ctx context.Context) error {
		return r.applyChange(ctx, client, zone.id, route53types.ChangeActionUpsert, rrs)
	}, retry.WithTimeout(zonePropagationTimeout), retry.WithInterval(time.Second))
	if err != nil {
		return nil, fmt.Errorf("create record set: %w", err)
	}
	// The change response holds only a change id, never the record, so the
	// settled fqdn comes from a read.
	return r.read(ctx, client)
}

func (r *RecordSet) Read(
	ctx context.Context, cfg *awsCfg, prior *RecordSetOutput,
) (*RecordSetOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *RecordSet) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[RecordSet, *RecordSetOutput],
) (*RecordSetOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The four identity fields are ReplaceFields, so only the in-place fields
	// can differ here. Reissue the UPSERT only when one of them actually
	// changed: an UPSERT of the unchanged record set is work Route 53 does not
	// need, and re-running the INSYNC wait on every apply is needless.
	if r.recordChanged(prior.Inputs) {
		zone, err := r.hostedZone(ctx, client)
		if err != nil {
			return nil, err
		}
		rrs := r.resourceRecordSet(zone.name)
		err = r.applyChange(ctx, client, zone.id, route53types.ChangeActionUpsert, rrs)
		if err != nil {
			return nil, fmt.Errorf("update record set: %w", err)
		}
	}
	return r.read(ctx, client)
}

func (r *RecordSet) Delete(ctx context.Context, cfg *awsCfg, prior *RecordSetOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// A replace decodes the new inputs into the receiver before Delete is called,
	// so the record set to remove is the one the prior apply created, named by the
	// four-part key on the prior output rather than by the receiver's fields.
	old := r.priorRecordSet(prior)
	zone, err := old.hostedZone(ctx, client)
	if err != nil {
		// A zone gone takes its records with it, so the record set is already
		// deleted.
		if isNotFound(err) {
			return nil
		}
		return err
	}
	// Route 53's DELETE wants the complete record set, not just its key, so the
	// live record set is read and resubmitted verbatim. A record set already
	// gone is a delete that has nothing left to do.
	live, err := old.findRecordSet(ctx, client, zone)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		return err
	}
	err = old.applyChange(ctx, client, zone.id, route53types.ChangeActionDelete, live)
	if err != nil {
		// A delete batch Route 53 rejects as invalid -- a record set that
		// changed out from under the read, or one already removed -- leaves
		// nothing to delete, so it counts as deleted.
		var invalid *route53types.InvalidChangeBatch
		if errors.As(err, &invalid) {
			return nil
		}
		return fmt.Errorf("delete record set: %w", err)
	}
	return nil
}

// priorRecordSet rebuilds the four-part key from a prior output so Delete acts on
// the record set the prior apply recorded. On a replace the receiver holds the
// new inputs, but the runtime passes Delete the prior output, which holds the
// old zone id, name, type, and set-identifier.
func (r *RecordSet) priorRecordSet(prior *RecordSetOutput) *RecordSet {
	old := &RecordSet{
		ZoneId: prior.ZoneId,
		Name:   prior.Name,
		Type:   prior.Type,
	}
	if prior.SetIdentifier != "" {
		old.SetIdentifier = aws.String(prior.SetIdentifier)
	}
	return old
}

// hostedZone resolves the record's hosted zone, returning its cleaned id and
// canonical name. The name qualifies the record name to an FQDN and the cleaned
// id addresses the change and list calls.
func (r *RecordSet) hostedZone(
	ctx context.Context, client *route53.Client,
) (zoneInfo, error) {
	resp, err := client.GetHostedZone(ctx, &route53.GetHostedZoneInput{
		Id: aws.String(cleanZoneID(r.ZoneId)),
	})
	if err != nil {
		return zoneInfo{}, fmt.Errorf("get hosted zone: %w", err)
	}
	return zoneInfo{
		id:   cleanZoneID(aws.ToString(resp.HostedZone.Id)),
		name: aws.ToString(resp.HostedZone.Name),
	}, nil
}

// zoneInfo is the part of a hosted zone the record set needs: its cleaned id
// and its canonical name.
type zoneInfo struct {
	id   string
	name string
}

// read resolves the hosted zone and returns the record set's settled output. A
// zone or record set that is gone reads as runtime.ErrNotFound, which drives a
// recreate.
func (r *RecordSet) read(
	ctx context.Context, client *route53.Client,
) (*RecordSetOutput, error) {
	zone, err := r.hostedZone(ctx, client)
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, err
	}
	if _, err := r.findRecordSet(ctx, client, zone); err != nil {
		return nil, err
	}
	return &RecordSetOutput{
		Fqdn:          r.fqdn(zone.name),
		ZoneId:        r.ZoneId,
		Name:          r.Name,
		Type:          r.Type,
		SetIdentifier: aws.ToString(r.SetIdentifier),
	}, nil
}

// findRecordSet lists the zone's record sets starting at the record's
// fully-qualified name and type, then filters to the exact match on name, type,
// and set-identifier. Route 53 returns record sets ordered by name then type,
// so the listing begins at the target and the scan stops once it moves past the
// target name and type; record sets that share a name and type across many
// set-identifiers can span more than one page, so the listing pages through its
// own NextRecord tokens. Zero matches is runtime.ErrNotFound; more than one is a
// real error, since the four-part key is unique.
func (r *RecordSet) findRecordSet(
	ctx context.Context, client *route53.Client, zone zoneInfo,
) (*route53types.ResourceRecordSet, error) {
	name := r.fqdn(zone.name)
	wantName := strings.ToLower(name)
	wantType := strings.ToUpper(r.Type)
	wantSet := aws.ToString(r.SetIdentifier)
	in := &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(zone.id),
		StartRecordName: aws.String(name),
		StartRecordType: route53types.RRType(r.Type),
	}
	var matches []route53types.ResourceRecordSet
	for {
		resp, err := client.ListResourceRecordSets(ctx, in)
		if err != nil {
			if isNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("list resource record sets: %w", err)
		}
		past := false
		for _, rrs := range resp.ResourceRecordSets {
			gotName := strings.ToLower(normalizeName(aws.ToString(rrs.Name)))
			gotType := strings.ToUpper(string(rrs.Type))
			if gotName == wantName && gotType == wantType &&
				aws.ToString(rrs.SetIdentifier) == wantSet {
				matches = append(matches, rrs)
			}
			// Record sets come ordered by name, so once the listing is past the
			// target name no later record set can match. The name ordering is
			// reliable; the type dimension is left to the IsTruncated paging so a
			// match sharing the name is never skipped.
			if gotName > wantName {
				past = true
				break
			}
		}
		if past || !resp.IsTruncated {
			break
		}
		in.StartRecordName = resp.NextRecordName
		in.StartRecordType = resp.NextRecordType
		in.StartRecordIdentifier = resp.NextRecordIdentifier
	}
	switch len(matches) {
	case 0:
		return nil, runtime.ErrNotFound
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf(
			"found %d record sets matching %s %s, want one", len(matches), name, r.Type)
	}
}

// resourceRecordSet builds the SDK record set sent in a change batch. The name
// is qualified to the zone. An alias has no records or TTL; plain records take
// their values and TTL, with TXT and SPF values double-quoted as Route 53
// stores them. At most one routing policy is set, mapping each block onto its
// flat record-set field.
func (r *RecordSet) resourceRecordSet(zoneName string) *route53types.ResourceRecordSet {
	rrs := &route53types.ResourceRecordSet{
		Name: aws.String(r.fqdn(zoneName)),
		Type: route53types.RRType(r.Type),
	}
	if r.SetIdentifier != nil {
		rrs.SetIdentifier = r.SetIdentifier
	}
	if r.HealthCheckId != nil {
		rrs.HealthCheckId = r.HealthCheckId
	}
	if r.Alias != nil {
		rrs.AliasTarget = aliasTarget(r.Alias)
	} else {
		rrs.TTL = r.Ttl
		rrs.ResourceRecords = r.resourceRecords()
	}
	switch {
	case r.WeightedRoutingPolicy != nil:
		rrs.Weight = aws.Int64(r.WeightedRoutingPolicy.Weight)
	case r.LatencyRoutingPolicy != nil:
		rrs.Region = route53types.ResourceRecordSetRegion(r.LatencyRoutingPolicy.Region)
	case r.FailoverRoutingPolicy != nil:
		rrs.Failover = route53types.ResourceRecordSetFailover(r.FailoverRoutingPolicy.Type)
	case r.GeolocationRoutingPolicy != nil:
		rrs.GeoLocation = geoLocation(r.GeolocationRoutingPolicy)
	case r.MultivalueAnswerRoutingPolicy != nil:
		rrs.MultiValueAnswer = r.MultivalueAnswerRoutingPolicy
	}
	return rrs
}

// resourceRecords converts the record values into the SDK type. TXT and SPF
// values are stored quoted by Route 53, so each is wrapped in double quotes on
// the way out.
func (r *RecordSet) resourceRecords() []route53types.ResourceRecord {
	if len(r.Records) == 0 {
		return nil
	}
	quote := isQuotedType(r.Type)
	records := make([]route53types.ResourceRecord, 0, len(r.Records))
	for _, value := range r.Records {
		v := value
		if quote {
			v = strconvQuote(v)
		}
		records = append(records, route53types.ResourceRecord{Value: aws.String(v)})
	}
	return records
}

// applyChange submits one change to the hosted zone and waits for it to reach
// INSYNC. A change that returns no change info is already applied.
func (r *RecordSet) applyChange(
	ctx context.Context,
	client *route53.Client,
	zoneID string,
	action route53types.ChangeAction,
	rrs *route53types.ResourceRecordSet,
) error {
	resp, err := client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch: &route53types.ChangeBatch{
			Changes: []route53types.Change{{Action: action, ResourceRecordSet: rrs}},
		},
	})
	if err != nil {
		return changeError(err)
	}
	if resp.ChangeInfo == nil {
		return nil
	}
	return waitChangeInsync(ctx, client, aws.ToString(resp.ChangeInfo.Id))
}

// fqdn returns the record name qualified with the zone name. An empty name is
// the zone apex. A name that already ends with the zone name is left alone;
// otherwise the zone name is appended. A leading wildcard label, which Route 53
// stores octal-escaped, is restored to "*".
func (r *RecordSet) fqdn(zoneName string) string {
	zone := normalizeName(zoneName)
	name := normalizeName(r.Name)
	if name == "" {
		return zone
	}
	if name == zone || strings.HasSuffix(name, "."+zone) {
		return name
	}
	if zone == "" {
		return name
	}
	return name + "." + zone
}

// recordChanged reports whether any in-place field of the record set differs
// from the prior apply. The identity fields are ReplaceFields and never reach
// Update, so only these can change.
func (r *RecordSet) recordChanged(old RecordSet) bool {
	return runtime.Changed(old.Records, r.Records) ||
		runtime.Changed(old.Ttl, r.Ttl) ||
		runtime.Changed(old.HealthCheckId, r.HealthCheckId) ||
		runtime.Changed(old.Alias, r.Alias) ||
		runtime.Changed(old.WeightedRoutingPolicy, r.WeightedRoutingPolicy) ||
		runtime.Changed(old.LatencyRoutingPolicy, r.LatencyRoutingPolicy) ||
		runtime.Changed(old.FailoverRoutingPolicy, r.FailoverRoutingPolicy) ||
		runtime.Changed(old.GeolocationRoutingPolicy, r.GeolocationRoutingPolicy) ||
		runtime.Changed(old.MultivalueAnswerRoutingPolicy, r.MultivalueAnswerRoutingPolicy)
}

// isQuotedType reports whether a record type stores its values double-quoted.
// TXT and SPF do; every other type stores its values plain.
func isQuotedType(rrType string) bool {
	switch strings.ToUpper(rrType) {
	case "TXT", "SPF":
		return true
	default:
		return false
	}
}

// strconvQuote wraps a value in double quotes for a TXT or SPF record. An inner
// double quote is backslash-escaped, as Route 53 requires.
func strconvQuote(value string) string {
	escaped := strings.ReplaceAll(value, `"`, `\"`)
	return `"` + escaped + `"`
}
