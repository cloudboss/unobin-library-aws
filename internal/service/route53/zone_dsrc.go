package route53

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	route53 "github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin/pkg/constraint"
)

// ZoneData resolves one existing Route 53 hosted zone. A zone-id reads that
// zone directly. Otherwise the lookup scans hosted zones and filters by the
// normalized name, public/private kind, optional VPC id, and optional tag subset
// until exactly one zone remains.
type ZoneData struct {
	ZoneId      *string            `ub:"zone-id"`
	Name        *string            `ub:"name"`
	PrivateZone *bool              `ub:"private-zone"`
	VpcId       *string            `ub:"vpc-id"`
	Tags        *map[string]string `ub:"tags"`
}

// ZoneDataOutput holds the hosted zone attributes returned by the lookup.
type ZoneDataOutput struct {
	ZoneId                    string            `ub:"zone-id"`
	Arn                       string            `ub:"arn"`
	Name                      string            `ub:"name"`
	NameServers               []string          `ub:"name-servers"`
	PrimaryNameServer         string            `ub:"primary-name-server"`
	CallerReference           string            `ub:"caller-reference"`
	Comment                   string            `ub:"comment"`
	PrivateZone               bool              `ub:"private-zone"`
	ResourceRecordSetCount    int64             `ub:"resource-record-set-count"`
	EnableAcceleratedRecovery bool              `ub:"enable-accelerated-recovery"`
	LinkedServiceDescription  string            `ub:"linked-service-description"`
	LinkedServicePrincipal    string            `ub:"linked-service-principal"`
	Tags                      map[string]string `ub:"tags"`
}

// Constraints declares the mutually exclusive hosted zone selectors. Name and
// zone-id are both optional because tag-only and VPC-only scans can select one
// zone.
func (r ZoneData) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.ZoneId, r.Name),
	}
}

// Read resolves the hosted zone and returns its current attributes. A data
// source lookup that finds no zone, or more than one zone, returns an ordinary
// descriptive error rather than runtime.ErrNotFound.
func (r *ZoneData) Read(ctx context.Context, cfg *awsCfg) (*ZoneDataOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	zone, err := r.findZone(ctx, client)
	if err != nil {
		return nil, err
	}
	return r.output(ctx, client, zone)
}

func (r *ZoneData) findZone(
	ctx context.Context, client *route53.Client,
) (route53types.HostedZone, error) {
	if r.ZoneId != nil {
		return r.findZoneByID(ctx, client, *r.ZoneId)
	}
	return r.findZoneByFilters(ctx, client)
}

func (r *ZoneData) findZoneByID(
	ctx context.Context, client *route53.Client, zoneID string,
) (route53types.HostedZone, error) {
	resp, err := client.GetHostedZone(ctx, &route53.GetHostedZoneInput{Id: aws.String(zoneID)})
	if err != nil {
		if isNotFound(err) {
			return route53types.HostedZone{}, fmt.Errorf(
				"route53 hosted zone %q not found", zoneID)
		}
		return route53types.HostedZone{}, fmt.Errorf("get hosted zone %s: %w", zoneID, err)
	}
	if resp.HostedZone == nil {
		return route53types.HostedZone{}, fmt.Errorf("route53 hosted zone %q not found", zoneID)
	}
	return *resp.HostedZone, nil
}

func (r *ZoneData) findZoneByFilters(
	ctx context.Context, client *route53.Client,
) (route53types.HostedZone, error) {
	wantPrivate := aws.ToBool(r.PrivateZone) || r.VpcId != nil
	wantTags := userTags(ptr.Value(r.Tags))
	var matches []route53types.HostedZone
	pager := route53.NewListHostedZonesPaginator(client, &route53.ListHostedZonesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return route53types.HostedZone{}, fmt.Errorf("list hosted zones: %w", err)
		}
		for i := range page.HostedZones {
			zone := page.HostedZones[i]
			matched, err := r.zoneMatches(ctx, client, zone, wantPrivate, wantTags)
			if err != nil {
				return route53types.HostedZone{}, err
			}
			if matched {
				matches = append(matches, zone)
			}
		}
	}
	switch len(matches) {
	case 0:
		return route53types.HostedZone{}, errors.New("no matching Route 53 Hosted Zone found")
	case 1:
		return matches[0], nil
	default:
		return route53types.HostedZone{}, errors.New(
			"multiple Route 53 Hosted Zones matched; use additional constraints to " +
				"reduce matches to a single Route 53 Hosted Zone")
	}
}

func (r *ZoneData) zoneMatches(
	ctx context.Context,
	client *route53.Client,
	zone route53types.HostedZone,
	wantPrivate bool,
	wantTags map[string]string,
) (bool, error) {
	if r.Name != nil && normalizeDomainName(aws.ToString(zone.Name)) != normalizeDomainName(*r.Name) {
		return false, nil
	}
	if zonePrivate(zone) != wantPrivate {
		return false, nil
	}
	zoneID := cleanZoneID(aws.ToString(zone.Id))
	if r.VpcId != nil {
		matched, err := zoneHasVPC(ctx, client, zoneID, *r.VpcId)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	if len(wantTags) > 0 {
		tags, err := listZoneTags(ctx, client, zoneID)
		if err != nil {
			return false, err
		}
		if !tagsContainAll(tags, wantTags) {
			return false, nil
		}
	}
	return true, nil
}

func zoneHasVPC(ctx context.Context, client *route53.Client, zoneID, vpcID string) (bool, error) {
	resp, err := client.GetHostedZone(ctx, &route53.GetHostedZoneInput{Id: aws.String(zoneID)})
	if err != nil {
		if isNotFound(err) {
			return false, fmt.Errorf("route53 hosted zone %q not found", zoneID)
		}
		return false, fmt.Errorf("get hosted zone %s for vpc filter: %w", zoneID, err)
	}
	for _, vpc := range resp.VPCs {
		if aws.ToString(vpc.VPCId) == vpcID {
			return true, nil
		}
	}
	return false, nil
}

func (r *ZoneData) output(
	ctx context.Context, client *route53.Client, zone route53types.HostedZone,
) (*ZoneDataOutput, error) {
	zoneID := cleanZoneID(aws.ToString(zone.Id))
	if zoneID == "" {
		return nil, errors.New("route53 hosted zone has an empty id")
	}
	private := zonePrivate(zone)
	nameServers, err := r.findNameServers(ctx, client, zoneID, private)
	if err != nil {
		return nil, err
	}
	tags, err := listZoneTags(ctx, client, zoneID)
	if err != nil {
		return nil, err
	}
	out := &ZoneDataOutput{
		ZoneId:                    zoneID,
		Arn:                       zoneARN(zoneRegion(client), zoneID),
		Name:                      normalizeDomainName(aws.ToString(zone.Name)),
		NameServers:               nameServers,
		PrimaryNameServer:         primaryNameServer(nameServers),
		CallerReference:           aws.ToString(zone.CallerReference),
		Comment:                   zoneComment(zone),
		PrivateZone:               private,
		ResourceRecordSetCount:    aws.ToInt64(zone.ResourceRecordSetCount),
		EnableAcceleratedRecovery: acceleratedRecoveryEnabled(zone),
		Tags:                      tags,
	}
	if zone.LinkedService != nil {
		out.LinkedServiceDescription = aws.ToString(zone.LinkedService.Description)
		out.LinkedServicePrincipal = aws.ToString(zone.LinkedService.ServicePrincipal)
	}
	return out, nil
}

func (r *ZoneData) findNameServers(
	ctx context.Context, client *route53.Client, zoneID string, private bool,
) ([]string, error) {
	resp, err := client.GetHostedZone(ctx, &route53.GetHostedZoneInput{Id: aws.String(zoneID)})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("route53 hosted zone %q not found", zoneID)
		}
		return nil, fmt.Errorf("get hosted zone %s for name servers: %w", zoneID, err)
	}
	if resp.HostedZone == nil {
		return nil, fmt.Errorf("route53 hosted zone %q not found", zoneID)
	}
	if !private {
		if resp.DelegationSet == nil {
			return nil, nil
		}
		return resp.DelegationSet.NameServers, nil
	}
	return findPrivateNameServers(ctx, client, zoneID, aws.ToString(resp.HostedZone.Name))
}

func findPrivateNameServers(
	ctx context.Context, client *route53.Client, zoneID, name string,
) ([]string, error) {
	resp, err := client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(zoneID),
		StartRecordName: aws.String(name),
		StartRecordType: route53types.RRTypeNs,
		MaxItems:        aws.Int32(1),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("route53 hosted zone %q not found", zoneID)
		}
		return nil, fmt.Errorf("list resource record sets %s for name servers: %w", zoneID, err)
	}
	want := normalizeDomainName(name)
	for _, rs := range resp.ResourceRecordSets {
		if rs.Type != route53types.RRTypeNs {
			continue
		}
		if normalizeDomainName(aws.ToString(rs.Name)) != want {
			continue
		}
		servers := make([]string, 0, len(rs.ResourceRecords))
		for _, record := range rs.ResourceRecords {
			servers = append(servers, aws.ToString(record.Value))
		}
		slices.Sort(servers)
		return servers, nil
	}
	return nil, nil
}

func listZoneTags(
	ctx context.Context, client *route53.Client, zoneID string,
) (map[string]string, error) {
	resp, err := client.ListTagsForResource(ctx, &route53.ListTagsForResourceInput{
		ResourceId:   aws.String(zoneID),
		ResourceType: route53types.TagResourceTypeHostedzone,
	})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("route53 hosted zone %q not found", zoneID)
		}
		return nil, fmt.Errorf("list tags for hosted zone %s: %w", zoneID, err)
	}
	if resp.ResourceTagSet == nil {
		return nil, nil
	}
	tags := make(map[string]string, len(resp.ResourceTagSet.Tags))
	for _, tag := range resp.ResourceTagSet.Tags {
		key := aws.ToString(tag.Key)
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	return tags, nil
}

func tagsContainAll(actual, wanted map[string]string) bool {
	for key, value := range wanted {
		got, ok := actual[key]
		if !ok || got != value {
			return false
		}
	}
	return true
}

func userTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = value
	}
	return out
}

func zonePrivate(zone route53types.HostedZone) bool {
	return zone.Config != nil && zone.Config.PrivateZone
}

func zoneComment(zone route53types.HostedZone) string {
	if zone.Config == nil {
		return ""
	}
	return aws.ToString(zone.Config.Comment)
}

func acceleratedRecoveryEnabled(zone route53types.HostedZone) bool {
	return zone.Features != nil &&
		zone.Features.AcceleratedRecoveryStatus == route53types.AcceleratedRecoveryStatusEnabled
}

func primaryNameServer(nameServers []string) string {
	if len(nameServers) == 0 {
		return ""
	}
	return nameServers[0]
}

func normalizeDomainName(name string) string {
	if name == "." {
		return "."
	}
	name = strings.TrimSuffix(name, ".")
	var b strings.Builder
	skipUntil := 0
	for i, r := range name {
		if i < skipUntil {
			continue
		}
		if isOctalEscape(name, i) {
			b.WriteString(name[i : i+4])
			skipUntil = i + 4
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if isRoute53DomainRune(r) {
			b.WriteRune(r)
			continue
		}
		writeOctalEscape(&b, r)
	}
	return b.String()
}

func writeOctalEscape(b *strings.Builder, r rune) {
	b.WriteByte('\\')
	octal := strconv.FormatInt(int64(r), 8)
	if len(octal) < 3 {
		for range 3 - len(octal) {
			b.WriteByte('0')
		}
	}
	b.WriteString(octal)
}

func isOctalEscape(s string, start int) bool {
	return s[start] == '\\' && start+3 < len(s) &&
		isOctalDigit(s[start+1]) && isOctalDigit(s[start+2]) && isOctalDigit(s[start+3])
}

func isOctalDigit(c byte) bool {
	return c >= '0' && c <= '7'
}

func isRoute53DomainRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
		r == '-' || r == '.' || r == '_'
}
