package route53

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

// RecordSetAlias points a record at an AWS resource -- a CloudFront
// distribution, an ELB load balancer, an S3 website, or another record in the
// zone -- instead of at plain values. An alias record has no records and no
// TTL; Route 53 follows the target's own TTL. All three fields are required
// when the block is present.
type RecordSetAlias struct {
	Name                 string `ub:"name"`
	ZoneId               string `ub:"zone-id"`
	EvaluateTargetHealth bool   `ub:"evaluate-target-health"`
}

// RecordSetWeightedRoutingPolicy splits queries among records that share a name
// and type in proportion to their weights. Each weighted record needs its own
// set-identifier. Weight is 0 to 255; a weight of zero takes traffic only when
// every record in the group is zero.
type RecordSetWeightedRoutingPolicy struct {
	Weight int64 `ub:"weight"`
}

// RecordSetLatencyRoutingPolicy answers a query with the record whose region is
// closest in latency to the resolver. Each latency record needs its own
// set-identifier and names one AWS region.
type RecordSetLatencyRoutingPolicy struct {
	Region string `ub:"region"`
}

// RecordSetFailoverRoutingPolicy designates a record as the PRIMARY or
// SECONDARY in a failover pair. Each failover record needs its own
// set-identifier; pairing a healthy primary with a secondary is done by sharing
// the record name and type across two records with opposite types.
type RecordSetFailoverRoutingPolicy struct {
	Type string `ub:"type"`
}

// RecordSetGeolocationRoutingPolicy answers a query based on the geographic
// origin of the resolver. A continent code, a country code (with an optional US
// subdivision code), or country code "*" for the default catch-all selects the
// record. Each geolocation record needs its own set-identifier.
type RecordSetGeolocationRoutingPolicy struct {
	ContinentCode   *string `ub:"continent-code"`
	CountryCode     *string `ub:"country-code"`
	SubdivisionCode *string `ub:"subdivision-code"`
}

// aliasTarget converts the alias block into the SDK type. The HostedZoneId and
// DNSName are sent as given; Route 53 normalizes the name itself.
func aliasTarget(a *RecordSetAlias) *route53types.AliasTarget {
	if a == nil {
		return nil
	}
	return &route53types.AliasTarget{
		DNSName:              aws.String(a.Name),
		HostedZoneId:         aws.String(a.ZoneId),
		EvaluateTargetHealth: a.EvaluateTargetHealth,
	}
}

// geoLocation converts the geolocation block into the SDK type, setting only
// the codes that are present so an omitted code stays absent from the request.
func geoLocation(g *RecordSetGeolocationRoutingPolicy) *route53types.GeoLocation {
	if g == nil {
		return nil
	}
	return &route53types.GeoLocation{
		ContinentCode:   g.ContinentCode,
		CountryCode:     g.CountryCode,
		SubdivisionCode: g.SubdivisionCode,
	}
}
