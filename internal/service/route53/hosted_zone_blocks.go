package route53

import (
	"fmt"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	route53 "github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

// toSDK converts a desired VPC association into the SDK type. When the region is
// omitted, the region the call is made from is used, the region Route 53 would
// otherwise assume.
func (v HostedZoneVpc) toSDK(defaultRegion string) *route53types.VPC {
	reg := defaultRegion
	if v.VpcRegion != nil {
		reg = *v.VpcRegion
	}
	return &route53types.VPC{
		VPCId:     aws.String(v.VpcId),
		VPCRegion: route53types.VPCRegion(reg),
	}
}

// zoneVPCsByID indexes a VPC list by its VPC id, so an update can tell which
// associations are new and which are gone.
func zoneVPCsByID(vpcs []HostedZoneVpc) map[string]HostedZoneVpc {
	byID := make(map[string]HostedZoneVpc, len(vpcs))
	for _, v := range vpcs {
		byID[v.VpcId] = v
	}
	return byID
}

// zoneTags converts a desired tag map into the Route 53 SDK tag list, ordered
// by key so the request is deterministic.
func zoneTags(tags map[string]string) []route53types.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]route53types.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, route53types.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}

// zoneIsApexNSOrSOA reports whether a record set is the zone's own apex NS or
// SOA record. Route 53 manages these two and clears them when the zone is
// deleted, so a force-destroy purge must leave them in place. The comparison is
// on the normalized name, since Route 53 may echo the name with different casing
// or a trailing dot.
func zoneIsApexNSOrSOA(rs route53types.ResourceRecordSet, apex string) bool {
	if normalizeName(aws.ToString(rs.Name)) != normalizeName(apex) {
		return false
	}
	return rs.Type == route53types.RRTypeNs || rs.Type == route53types.RRTypeSoa
}

// zoneCallerReference returns a unique idempotency token for a CreateHostedZone
// request. Route 53 requires a distinct CallerReference per create so a retried
// request is not taken for a duplicate; a timestamp with nanosecond precision is
// unique enough for one process.
func zoneCallerReference() string {
	return fmt.Sprintf("unobin-%d", time.Now().UnixNano())
}

// zoneARN builds the ARN of a hosted zone. A hosted zone ARN names neither
// region nor account, only the partition and the zone id, since a zone id is
// globally unique.
func zoneARN(reg, zoneID string) string {
	return fmt.Sprintf("arn:%s:route53:::hostedzone/%s", partition.Of(reg), zoneID)
}

// zoneRegion returns the region the client is configured for, used to derive the
// partition for the zone ARN.
func zoneRegion(client *route53.Client) string {
	return client.Options().Region
}
