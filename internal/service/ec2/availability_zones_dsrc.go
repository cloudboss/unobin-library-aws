package ec2

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin/pkg/constraint"
)

// AvailabilityZones looks up the Availability Zones, Local Zones, and Wavelength
// Zones for the configured Region with DescribeAvailabilityZones. Three inputs
// reach AWS: all-availability-zones widens the result to every zone regardless
// of opt-in status, state becomes a state filter, and filters passes generic
// name/values filters straight through. exclude-names and exclude-zone-ids act
// client-side after the call, skipping the named zones from the projection. The
// lookup errors when no zone survives the projection rather than returning empty
// lists.
//
// The three output lists are positionally aligned: names is sorted ascending by
// zone name, and zone-ids holds each zone's id at the same index as its name, so
// the same index in either list refers to the same zone. group-names is a
// separate, deduplicated, sorted list and is not index-aligned with the others.
type AvailabilityZones struct {
	AllAvailabilityZones *bool                      `ub:"all-availability-zones"`
	State                *string                    `ub:"state"`
	Filters              *[]AvailabilityZonesFilter `ub:"filters"`
	ExcludeNames         *[]string                  `ub:"exclude-names"`
	ExcludeZoneIds       *[]string                  `ub:"exclude-zone-ids"`
}

// AvailabilityZonesFilter is one DescribeAvailabilityZones filter: a filter name
// and the values to match, joined with OR. The name is an AWS
// DescribeAvailabilityZones filter key such as group-name, region-name,
// zone-type, parent-zone-id, or opt-in-status. Both fields are required and pass
// straight through to the API with no client-side interpretation.
type AvailabilityZonesFilter struct {
	Name   string   `ub:"name"`
	Values []string `ub:"values"`
}

// AvailabilityZonesOutput holds the matched zones as three lists. names is the
// zone names sorted ascending. zone-ids is the matching zone ids, each at the
// same index as its zone name in names. group-names is the distinct zone-group
// names, deduplicated and sorted ascending for reproducible output; Terraform
// models the same value as an unordered set.
type AvailabilityZonesOutput struct {
	Names      []string `ub:"names"`
	ZoneIds    []string `ub:"zone-ids"`
	GroupNames []string `ub:"group-names"`
}

// Constraints declares the input rules expressible as a derived schema. The
// state input, when given, must be one of the AWS zone-state filter values.
func (r AvailabilityZones) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.State)).
			Require(constraint.OneOf(r.State,
				"available", "information", "impaired", "unavailable", "constrained")).
			Message("state must be one of available, information, impaired, " +
				"unavailable, or constrained"),
	}
}

// Read resolves the zones. It builds the DescribeAvailabilityZones input,
// projects the matched zones into the three output lists, and errors when no
// zone survives the excludes. The call adds no retry or waiter:
// DescribeAvailabilityZones is consistent enough for a one-shot lookup.
func (r *AvailabilityZones) Read(
	ctx context.Context,
	cfg *awsCfg) (*AvailabilityZonesOutput,
	error,
) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.DescribeAvailabilityZones(ctx, r.describeInput())
	if err != nil {
		return nil, fmt.Errorf("describe availability zones: %w", err)
	}
	return r.output(resp.AvailabilityZones)
}

// describeInput builds the DescribeAvailabilityZones input.
// all-availability-zones is sent only when set so an unset input keeps the AWS
// default of opt-in zones. The state input becomes a state filter joined with
// any user filters. The filter list is left nil when there is nothing to filter
// on, since EC2 rejects an empty filter list.
func (r *AvailabilityZones) describeInput() *ec2.DescribeAvailabilityZonesInput {
	in := &ec2.DescribeAvailabilityZonesInput{}
	if r.AllAvailabilityZones != nil {
		in.AllAvailabilityZones = aws.Bool(*r.AllAvailabilityZones)
	}
	filters := availabilityZonesFilters(ptr.Value(r.Filters))
	if r.State != nil {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String("state"),
			Values: []string{*r.State},
		})
	}
	if len(filters) > 0 {
		in.Filters = filters
	}
	return in
}

// output projects the matched zones into the three output lists. The zones are
// sorted ascending by zone name first, so names and the index-aligned zone-ids
// share the same order. The excludes are applied during the projection, after
// the sort, skipping any zone whose name or id is listed. group-names is
// collected, deduplicated, and sorted separately. No zone surviving the excludes
// is an error rather than three empty lists.
func (r *AvailabilityZones) output(
	zones []ec2types.AvailabilityZone,
) (*AvailabilityZonesOutput, error) {
	sorted := slices.Clone(zones)
	slices.SortFunc(sorted, func(a, b ec2types.AvailabilityZone) int {
		return strings.Compare(aws.ToString(a.ZoneName), aws.ToString(b.ZoneName))
	})
	out := &AvailabilityZonesOutput{}
	groups := map[string]struct{}{}
	for _, zone := range sorted {
		name := aws.ToString(zone.ZoneName)
		zoneID := aws.ToString(zone.ZoneId)
		if slices.Contains(ptr.Value(r.ExcludeNames), name) {
			continue
		}
		if slices.Contains(ptr.Value(r.ExcludeZoneIds), zoneID) {
			continue
		}
		out.Names = append(out.Names, name)
		out.ZoneIds = append(out.ZoneIds, zoneID)
		if group := aws.ToString(zone.GroupName); group != "" {
			groups[group] = struct{}{}
		}
	}
	if len(out.Names) == 0 {
		return nil, errors.New(
			"availability zones lookup returned no results; " +
				"adjust state, filters, or excludes")
	}
	out.GroupNames = slices.Sorted(maps.Keys(groups))
	return out, nil
}

// availabilityZonesFilters converts the input filters into the
// DescribeAvailabilityZones filter list, passing each name and its values
// through unchanged. It returns nil for an empty input so the caller can append
// the state filter onto a clean slice.
func availabilityZonesFilters(filters []AvailabilityZonesFilter) []ec2types.Filter {
	if len(filters) == 0 {
		return nil
	}
	out := make([]ec2types.Filter, 0, len(filters))
	for _, f := range filters {
		out = append(out, ec2types.Filter{
			Name:   aws.String(f.Name),
			Values: f.Values,
		})
	}
	return out
}
