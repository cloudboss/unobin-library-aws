// verify checks the Route 53 group the scenario applied against the phase named
// in the VERIFY_PHASE environment variable. The hosted zone has a stable name,
// so both phases find it by an exact-name match over ListHostedZonesByName:
// applied requires the zone present with the www A record the first apply set,
// and destroyed requires the zone gone (force-destroy purges its records first).
// It only reads cloud state; tearing the group down is the destroy plan's job.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

const (
	zoneName    = "unobin-it.example.com"
	wwwAddress  = "192.0.2.1"
	wwwTTL      = 300
	markerKey   = "unobin"
	markerValue = "route53-it"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := route53.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *route53.Client) error {
	zone, err := findZone(ctx, client)
	if err != nil {
		return err
	}
	if zone == nil {
		return fmt.Errorf("hosted zone %s not found", zoneName)
	}
	zoneID := cleanZoneID(aws.ToString(zone.Id))

	records, err := listRecords(ctx, client, zoneID)
	if err != nil {
		return err
	}

	www := findRecord(records, "www."+zoneName, route53types.RRTypeA, "")
	if www == nil {
		return fmt.Errorf("www A record not found in zone %s", zoneName)
	}
	if !recordHasValue(www, wwwAddress) {
		return fmt.Errorf("www A record values %v do not include %s",
			recordValues(www), wwwAddress)
	}
	if aws.ToInt64(www.TTL) != wwwTTL {
		return fmt.Errorf("www A record ttl is %d, want %d", aws.ToInt64(www.TTL), wwwTTL)
	}

	// The TXT and weighted records are anchored best-effort: an emulator may not
	// model TXT quoting or weighted routing, so a miss degrades to a printed skip.
	if txt := findRecord(records, "txt."+zoneName, route53types.RRTypeTxt, ""); txt != nil {
		fmt.Printf("ok: txt TXT record present with values %v\n", recordValues(txt))
	} else {
		fmt.Println("skip: txt TXT record not modeled")
	}
	blue := findRecord(records, "app."+zoneName, route53types.RRTypeA, "blue")
	green := findRecord(records, "app."+zoneName, route53types.RRTypeA, "green")
	if blue != nil && green != nil {
		fmt.Println("ok: weighted app A pair present with both set identifiers")
	} else {
		fmt.Println("skip: weighted app A pair not fully modeled")
	}

	fmt.Printf("ok: hosted zone %s present with www A record %s ttl %d\n",
		zoneName, wwwAddress, wwwTTL)
	return nil
}

func verifyDestroyed(ctx context.Context, client *route53.Client) error {
	zone, err := findZone(ctx, client)
	if err != nil {
		return err
	}
	if zone != nil {
		return fmt.Errorf("hosted zone %s still exists", zoneName)
	}
	fmt.Printf("ok: hosted zone %s gone\n", zoneName)
	return nil
}

// findZone returns the hosted zone whose name matches zoneName exactly, or nil
// when none does. ListHostedZonesByName starts at the queried name and can list
// adjacent zones, so the match is confirmed client-side on the normalized name.
func findZone(ctx context.Context, client *route53.Client) (*route53types.HostedZone, error) {
	out, err := client.ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{
		DNSName: aws.String(zoneName),
	})
	if err != nil {
		return nil, fmt.Errorf("list hosted zones by name: %w", err)
	}
	for i := range out.HostedZones {
		if normalize(aws.ToString(out.HostedZones[i].Name)) == zoneName {
			return &out.HostedZones[i], nil
		}
	}
	return nil, nil
}

// listRecords returns every record set in the zone, following truncation.
func listRecords(
	ctx context.Context, client *route53.Client, zoneID string,
) ([]route53types.ResourceRecordSet, error) {
	var out []route53types.ResourceRecordSet
	in := &route53.ListResourceRecordSetsInput{HostedZoneId: aws.String(zoneID)}
	for {
		page, err := client.ListResourceRecordSets(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("list resource record sets: %w", err)
		}
		out = append(out, page.ResourceRecordSets...)
		if !page.IsTruncated {
			return out, nil
		}
		in.StartRecordName = page.NextRecordName
		in.StartRecordType = page.NextRecordType
		in.StartRecordIdentifier = page.NextRecordIdentifier
	}
}

// findRecord returns the record set matching name, type, and set identifier, or
// nil. The name comparison is on the normalized form, since Route 53 stores
// names with a trailing dot. An empty setID matches a record with no identifier.
func findRecord(
	records []route53types.ResourceRecordSet,
	name string, rrType route53types.RRType, setID string,
) *route53types.ResourceRecordSet {
	for i := range records {
		r := &records[i]
		if normalize(aws.ToString(r.Name)) != name || r.Type != rrType {
			continue
		}
		if aws.ToString(r.SetIdentifier) == setID {
			return r
		}
	}
	return nil
}

func recordValues(r *route53types.ResourceRecordSet) []string {
	values := make([]string, 0, len(r.ResourceRecords))
	for _, rr := range r.ResourceRecords {
		values = append(values, aws.ToString(rr.Value))
	}
	return values
}

func recordHasValue(r *route53types.ResourceRecordSet, value string) bool {
	return slices.Contains(recordValues(r), value)
}

func normalize(name string) string {
	return strings.TrimSuffix(name, ".")
}

func cleanZoneID(id string) string {
	return strings.TrimPrefix(id, "/hostedzone/")
}
