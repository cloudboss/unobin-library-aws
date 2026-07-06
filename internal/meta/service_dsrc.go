package meta

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/constraint"
)

// ServiceDataSource describes the DNS names and partition support for an AWS service
// endpoint identifier in a region.
type ServiceDataSource struct {
	DNSName          *string `ub:"dns-name"`
	Region           *string `ub:"region"`
	ReverseDNSName   *string `ub:"reverse-dns-name"`
	ReverseDNSPrefix *string `ub:"reverse-dns-prefix"`
	ServiceID        *string `ub:"service-id"`
}

// ServiceDataSourceOutput contains forward and reverse DNS names for the service.
type ServiceDataSourceOutput struct {
	DNSName          string `ub:"dns-name"`
	Partition        string `ub:"partition"`
	Region           string `ub:"region"`
	ReverseDNSName   string `ub:"reverse-dns-name"`
	ReverseDNSPrefix string `ub:"reverse-dns-prefix"`
	ServiceID        string `ub:"service-id"`
	Supported        bool   `ub:"supported"`
}

func (d ServiceDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(d.DNSName, d.ReverseDNSName, d.ServiceID),
	}
}

func (d *ServiceDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*ServiceDataSourceOutput, error) {
	resolved, err := d.resolve(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return resolved.output(), nil
}

type resolvedService struct {
	partition        string
	region           string
	reverseDNSPrefix string
	serviceID        string
	supported        bool
}

func (d *ServiceDataSource) resolve(ctx context.Context, cfg *awsCfg) (*resolvedService, error) {
	resolved := &resolvedService{
		region:           stringValue(d.Region),
		reverseDNSPrefix: stringValue(d.ReverseDNSPrefix),
		serviceID:        stringValue(d.ServiceID),
	}
	if err := d.applyDNSInputs(resolved); err != nil {
		return nil, err
	}
	if resolved.serviceID == "" {
		return nil, errors.New("service-id not provided directly or through a DNS name")
	}
	if resolved.region == "" {
		var err error
		resolved.region, err = configuredRegion(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	info, err := findRegionByName(resolved.region)
	if err != nil {
		return nil, err
	}
	resolved.partition = info.Partition.ID()
	if resolved.reverseDNSPrefix == "" {
		resolved.reverseDNSPrefix = reverseDNSPrefix(info.Partition.DNSSuffix())
	}
	_, resolved.supported = info.Partition.Services()[resolved.serviceID]
	return resolved, nil
}

func (d *ServiceDataSource) applyDNSInputs(resolved *resolvedService) error {
	if v := stringValue(d.ReverseDNSName); v != "" {
		parsed, err := parseReverseServiceDNSName(v)
		if err != nil {
			return err
		}
		if err := resolved.merge(parsed); err != nil {
			return err
		}
	}
	if v := stringValue(d.DNSName); v != "" {
		parsed, err := parseServiceDNSName(v)
		if err != nil {
			return err
		}
		if err := resolved.merge(parsed); err != nil {
			return err
		}
	}
	return nil
}

func (s *resolvedService) merge(parsed *resolvedService) error {
	if s.region != "" && s.region != parsed.region {
		return errors.New("region and DNS name matched different AWS regions")
	}
	if s.reverseDNSPrefix != "" && s.reverseDNSPrefix != parsed.reverseDNSPrefix {
		return errors.New("reverse-dns-prefix and DNS name matched different prefixes")
	}
	if s.serviceID != "" && s.serviceID != parsed.serviceID {
		return errors.New("service-id and DNS name matched different services")
	}
	s.region = parsed.region
	s.reverseDNSPrefix = parsed.reverseDNSPrefix
	s.serviceID = parsed.serviceID
	return nil
}

func (s *resolvedService) output() *ServiceDataSourceOutput {
	reverseName := reverseDNSName(s.reverseDNSPrefix, s.region, s.serviceID)
	return &ServiceDataSourceOutput{
		DNSName:          dnsName(reverseName),
		Partition:        s.partition,
		Region:           s.region,
		ReverseDNSName:   reverseName,
		ReverseDNSPrefix: s.reverseDNSPrefix,
		ServiceID:        s.serviceID,
		Supported:        s.supported,
	}
}

func parseServiceDNSName(v string) (*resolvedService, error) {
	parts := strings.Split(strings.ToLower(v), ".")
	reverse(parts)
	return parseServiceParts(parts, "service DNS names")
}

func parseReverseServiceDNSName(v string) (*resolvedService, error) {
	return parseServiceParts(strings.Split(strings.ToLower(v), "."), "reverse service DNS names")
}

func parseServiceParts(parts []string, label string) (*resolvedService, error) {
	n := len(parts)
	if n < 4 {
		return nil, fmt.Errorf("%s must have at least 4 parts, got %d", label, n)
	}
	return &resolvedService{
		region:           parts[n-2],
		reverseDNSPrefix: strings.Join(parts[0:n-2], "."),
		serviceID:        parts[n-1],
	}, nil
}

func reverse(values []string) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}
