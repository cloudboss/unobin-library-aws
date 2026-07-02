package meta

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

type awsCfg = awscfg.Configuration

type regionInfo struct {
	Name        string
	Description string
	Partition   endpoints.Partition
}

func loadConfig(ctx context.Context, cfg *awsCfg) (aws.Config, error) {
	sdkCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return aws.Config{}, err
	}
	return sdkCfg, nil
}

func configuredRegion(ctx context.Context, cfg *awsCfg) (string, error) {
	sdkCfg, err := loadConfig(ctx, cfg)
	if err != nil {
		return "", err
	}
	if sdkCfg.Region == "" {
		return "", errors.New("aws region is not set")
	}
	return sdkCfg.Region, nil
}

func newEC2Client(ctx context.Context, cfg *awsCfg) (*ec2.Client, aws.Config, error) {
	sdkCfg, err := loadConfig(ctx, cfg)
	if err != nil {
		return nil, aws.Config{}, err
	}
	return ec2.NewFromConfig(sdkCfg), sdkCfg, nil
}

func findRegionByName(name string) (regionInfo, error) {
	for _, partition := range endpoints.DefaultPartitions() {
		if region, ok := partition.Regions()[name]; ok {
			return regionInfo{
				Name:        region.ID(),
				Description: region.Description(),
				Partition:   partition,
			}, nil
		}
		if partition.RegionRegex().MatchString(name) {
			return regionInfo{Name: name, Partition: partition}, nil
		}
	}
	return regionInfo{}, fmt.Errorf("unknown AWS region %q", name)
}

func findRegionByEndpoint(ctx context.Context, endpoint string) (regionInfo, error) {
	wantHost, err := endpointHost(endpoint)
	if err != nil {
		return regionInfo{}, err
	}
	for _, partition := range endpoints.DefaultPartitions() {
		regions := partition.Regions()
		names := mapsKeys(regions)
		slices.Sort(names)
		for _, name := range names {
			gotHost, err := ec2Endpoint(ctx, name)
			if err != nil {
				return regionInfo{}, err
			}
			if gotHost == wantHost {
				region := regions[name]
				return regionInfo{
					Name:        region.ID(),
					Description: region.Description(),
					Partition:   partition,
				}, nil
			}
		}
	}
	return regionInfo{}, fmt.Errorf("no AWS region has EC2 endpoint %q", endpoint)
}

func endpointHost(raw string) (string, error) {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse endpoint: %w", err)
		}
		if u.Host == "" {
			return "", fmt.Errorf("endpoint %q has no host", raw)
		}
		return u.Host, nil
	}
	if raw == "" {
		return "", errors.New("endpoint must not be empty")
	}
	return raw, nil
}

func ec2Endpoint(ctx context.Context, region string) (string, error) {
	endpoint, err := ec2.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx,
		ec2.EndpointParameters{Region: aws.String(region)})
	if err != nil {
		return "", err
	}
	return endpoint.URI.Host, nil
}

func reverseDNSPrefix(dnsSuffix string) string {
	parts := strings.Split(dnsSuffix, ".")
	slices.Reverse(parts)
	return strings.Join(parts, ".")
}

func reverseDNSName(prefix, region, serviceID string) string {
	return fmt.Sprintf("%s.%s.%s", prefix, region, serviceID)
}

func dnsName(reverseName string) string {
	parts := strings.Split(reverseName, ".")
	slices.Reverse(parts)
	return strings.ToLower(strings.Join(parts, "."))
}

func mapsKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
