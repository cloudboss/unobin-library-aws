package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/constraint"
)

const defaultIPRangesURL = "https://ip-ranges.amazonaws.com/ip-ranges.json"

// IPRanges reads AWS public IP range metadata and filters it by service and
// optionally by region.
type IPRanges struct {
	Regions  *[]string `ub:"regions"`
	Services []string  `ub:"services"`
	URL      *string   `ub:"url"`
}

// IPRangesOutput contains the matched IPv4 and IPv6 CIDR blocks.
type IPRangesOutput struct {
	CIDRBlocks     []string `ub:"cidr-blocks"`
	CreateDate     string   `ub:"create-date"`
	IPv6CIDRBlocks []string `ub:"ipv6-cidr-blocks"`
	SyncToken      int64    `ub:"sync-token"`
}

func (d IPRanges) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(d.Services)).
			Message("services must list at least one service"),
	}
}

func (d *IPRanges) Read(ctx context.Context, _ *awsCfg) (*IPRangesOutput, error) {
	if len(d.Services) == 0 {
		return nil, errors.New("services must list at least one service")
	}
	body, err := readAll(ctx, ipRangesURL(d.URL))
	if err != nil {
		return nil, err
	}
	var ranges ipRangesDocument
	if err := json.Unmarshal(body, &ranges); err != nil {
		return nil, fmt.Errorf("parse IP ranges JSON: %w", err)
	}
	syncToken, err := strconv.ParseInt(ranges.SyncToken, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse sync token: %w", err)
	}
	ipv4, ipv6 := d.matchingCIDRBlocks(ranges)
	return &IPRangesOutput{
		CIDRBlocks:     ipv4,
		CreateDate:     ranges.CreateDate,
		IPv6CIDRBlocks: ipv6,
		SyncToken:      syncToken,
	}, nil
}

func (d *IPRanges) matchingCIDRBlocks(ranges ipRangesDocument) ([]string, []string) {
	regions := lowerSet(sliceValue(d.Regions))
	services := lowerSet(d.Services)
	matches := func(region, service string) bool {
		region = strings.ToLower(region)
		service = strings.ToLower(service)
		return (len(regions) == 0 || regions[region]) && services[service]
	}

	ipv4 := make([]string, 0, len(ranges.IPv4Prefixes))
	for _, prefix := range ranges.IPv4Prefixes {
		if matches(prefix.Region, prefix.Service) {
			ipv4 = append(ipv4, prefix.Prefix)
		}
	}
	slices.Sort(ipv4)

	ipv6 := make([]string, 0, len(ranges.IPv6Prefixes))
	for _, prefix := range ranges.IPv6Prefixes {
		if matches(prefix.Region, prefix.Service) {
			ipv6 = append(ipv6, prefix.Prefix)
		}
	}
	slices.Sort(ipv6)
	return ipv4, ipv6
}

func ipRangesURL(v *string) string {
	if v == nil || *v == "" {
		return defaultIPRangesURL
	}
	return *v
}

func readAll(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP GET %s: %s", rawURL, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return body, nil
}

func lowerSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[strings.ToLower(value)] = true
	}
	return out
}

func sliceValue[T any](v *[]T) []T {
	if v == nil {
		return nil
	}
	return *v
}

type ipRangesDocument struct {
	CreateDate   string       `json:"createDate"`
	IPv4Prefixes []ipv4Prefix `json:"prefixes"`
	IPv6Prefixes []ipv6Prefix `json:"ipv6_prefixes"`
	SyncToken    string       `json:"syncToken"`
}

type ipv4Prefix struct {
	Prefix  string `json:"ip_prefix"`
	Region  string `json:"region"`
	Service string `json:"service"`
}

type ipv6Prefix struct {
	Prefix  string `json:"ipv6_prefix"`
	Region  string `json:"region"`
	Service string `json:"service"`
}
