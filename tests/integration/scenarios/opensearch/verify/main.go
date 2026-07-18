package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	opensearch "github.com/aws/aws-sdk-go-v2/service/opensearch"
	opensearchtypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
)

const (
	primaryDomainName = "unobin-it-opensearch-primary"
	clearDomainName   = "unobin-it-opensearch-clear"
	engineVersion     = "OpenSearch_2.13"
	volumeSize        = int32(10)
	volumeIOPS        = int32(3000)
	volumeThroughput  = int32(125)
)

type verifierClient interface {
	DescribeDomain(
		context.Context,
		*opensearch.DescribeDomainInput,
		...func(*opensearch.Options),
	) (*opensearch.DescribeDomainOutput, error)
	DescribeDomainConfig(
		context.Context,
		*opensearch.DescribeDomainConfigInput,
		...func(*opensearch.Options),
	) (*opensearch.DescribeDomainConfigOutput, error)
	ListTags(
		context.Context,
		*opensearch.ListTagsInput,
		...func(*opensearch.Options),
	) (*opensearch.ListTagsOutput, error)
}

type expectedDomain struct {
	name          string
	instanceCount int32
	tags          map[string]string
}

type domainIdentity struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	ARN  string `json:"arn"`
}

type recordedDomains struct {
	Primary domainIdentity `json:"primary"`
	Clear   domainIdentity `json:"clear"`
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := opensearch.NewFromConfig(cfg)
	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "updated":
		return verifyUpdated(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf(
			"VERIFY_PHASE must be applied, updated, or destroyed, got %q",
			phase,
		)
	}
}

func verifyApplied(ctx context.Context, client verifierClient) error {
	primary, err := verifyPresent(ctx, client, expectedDomain{
		name: primaryDomainName, instanceCount: 1,
		tags: map[string]string{"change": "old", "keep": "1", "remove": "yes"},
	})
	if err != nil {
		return err
	}
	clear, err := verifyPresent(ctx, client, expectedDomain{
		name: clearDomainName, instanceCount: 1,
		tags: map[string]string{"clear": "yes"},
	})
	if err != nil {
		return err
	}
	return writeRecordedDomains(recordedDomains{Primary: primary, Clear: clear})
}

func verifyUpdated(ctx context.Context, client verifierClient) error {
	recorded, err := readRecordedDomains()
	if err != nil {
		return err
	}
	primary, err := verifyPresent(ctx, client, expectedDomain{
		name: primaryDomainName, instanceCount: 2,
		tags: map[string]string{"add": "yes", "change": "new", "keep": "1"},
	})
	if err != nil {
		return err
	}
	if primary != recorded.Primary {
		return fmt.Errorf("primary domain identity changed: got %+v, want %+v",
			primary, recorded.Primary)
	}
	clear, err := verifyPresent(ctx, client, expectedDomain{
		name: clearDomainName, instanceCount: 1, tags: map[string]string{},
	})
	if err != nil {
		return err
	}
	if clear != recorded.Clear {
		return fmt.Errorf("clear domain identity changed: got %+v, want %+v",
			clear, recorded.Clear)
	}
	return nil
}

func verifyPresent(
	ctx context.Context,
	client verifierClient,
	expected expectedDomain,
) (domainIdentity, error) {
	described, err := client.DescribeDomain(ctx, &opensearch.DescribeDomainInput{
		DomainName: aws.String(expected.name),
	})
	if err != nil {
		return domainIdentity{}, fmt.Errorf("describe domain %s: %w", expected.name, err)
	}
	if described == nil || described.DomainStatus == nil {
		return domainIdentity{}, fmt.Errorf("domain %s returned no status", expected.name)
	}
	status := described.DomainStatus
	identity := domainIdentity{
		Name: aws.ToString(status.DomainName),
		ID:   aws.ToString(status.DomainId),
		ARN:  aws.ToString(status.ARN),
	}
	if identity.Name != expected.name || identity.ID == "" || identity.ARN == "" {
		return domainIdentity{}, fmt.Errorf("domain %s returned incomplete identity %+v",
			expected.name, identity)
	}
	if err := verifyStatusConfiguration(status, expected); err != nil {
		return domainIdentity{}, err
	}
	configured, err := client.DescribeDomainConfig(
		ctx,
		&opensearch.DescribeDomainConfigInput{DomainName: aws.String(expected.name)},
	)
	if err != nil {
		return domainIdentity{}, fmt.Errorf(
			"describe domain %s configuration: %w", expected.name, err,
		)
	}
	if err := verifyDomainConfig(configured, expected); err != nil {
		return domainIdentity{}, err
	}
	tags, err := client.ListTags(ctx, &opensearch.ListTagsInput{ARN: status.ARN})
	if err != nil {
		return domainIdentity{}, fmt.Errorf("list domain %s tags: %w", expected.name, err)
	}
	if actual := managedTags(tags); !maps.Equal(actual, expected.tags) {
		return domainIdentity{}, fmt.Errorf("domain %s tags are %v, want %v",
			expected.name, actual, expected.tags)
	}
	fmt.Printf("ok: domain %s has %d instances, unchanged gp3 EBS, and expected tags\n",
		expected.name, expected.instanceCount)
	return identity, nil
}

func verifyStatusConfiguration(
	status *opensearchtypes.DomainStatus,
	expected expectedDomain,
) error {
	if aws.ToString(status.EngineVersion) != engineVersion {
		return fmt.Errorf("domain %s engine version is %q, want %q",
			expected.name, aws.ToString(status.EngineVersion), engineVersion)
	}
	if status.ClusterConfig == nil ||
		aws.ToInt32(status.ClusterConfig.InstanceCount) != expected.instanceCount {
		return fmt.Errorf("domain %s instance count is not %d",
			expected.name, expected.instanceCount)
	}
	return verifyEBS(expected.name, status.EBSOptions)
}

func verifyDomainConfig(
	output *opensearch.DescribeDomainConfigOutput,
	expected expectedDomain,
) error {
	if output == nil || output.DomainConfig == nil {
		return fmt.Errorf("domain %s returned no configuration", expected.name)
	}
	configuration := output.DomainConfig
	if configuration.EngineVersion == nil ||
		aws.ToString(configuration.EngineVersion.Options) != engineVersion {
		return fmt.Errorf("domain %s config engine version is not %q",
			expected.name, engineVersion)
	}
	if configuration.ClusterConfig == nil || configuration.ClusterConfig.Options == nil ||
		aws.ToInt32(configuration.ClusterConfig.Options.InstanceCount) != expected.instanceCount {
		return fmt.Errorf("domain %s config instance count is not %d",
			expected.name, expected.instanceCount)
	}
	if configuration.EBSOptions == nil {
		return fmt.Errorf("domain %s config returned no EBS options", expected.name)
	}
	return verifyEBS(expected.name, configuration.EBSOptions.Options)
}

func verifyEBS(name string, options *opensearchtypes.EBSOptions) error {
	if options == nil || !aws.ToBool(options.EBSEnabled) ||
		options.VolumeType != opensearchtypes.VolumeTypeGp3 ||
		aws.ToInt32(options.VolumeSize) != volumeSize ||
		aws.ToInt32(options.Iops) != volumeIOPS ||
		aws.ToInt32(options.Throughput) != volumeThroughput {
		return fmt.Errorf("domain %s EBS options are not the expected gp3 settings", name)
	}
	return nil
}

func managedTags(output *opensearch.ListTagsOutput) map[string]string {
	result := map[string]string{}
	if output == nil {
		return result
	}
	for _, tag := range output.TagList {
		key := aws.ToString(tag.Key)
		if !strings.HasPrefix(key, "aws:") {
			result[key] = aws.ToString(tag.Value)
		}
	}
	return result
}

func verifyDestroyed(ctx context.Context, client verifierClient) error {
	recorded, err := readRecordedDomains()
	if err != nil {
		return err
	}
	for _, domain := range []domainIdentity{recorded.Primary, recorded.Clear} {
		_, describeErr := client.DescribeDomain(ctx, &opensearch.DescribeDomainInput{
			DomainName: aws.String(domain.Name),
		})
		if !isNotFound(describeErr) {
			return fmt.Errorf("describe deleted domain %s: got %v, want typed not-found",
				domain.Name, describeErr)
		}
		_, configErr := client.DescribeDomainConfig(
			ctx,
			&opensearch.DescribeDomainConfigInput{DomainName: aws.String(domain.Name)},
		)
		if !isNotFound(configErr) {
			return fmt.Errorf(
				"describe deleted domain %s config: got %v, want typed not-found",
				domain.Name, configErr,
			)
		}
		tags, listErr := client.ListTags(
			ctx,
			&opensearch.ListTagsInput{ARN: aws.String(domain.ARN)},
		)
		if listErr != nil {
			if isDeletedDomainTagAbsence(listErr) {
				continue
			}
			return fmt.Errorf("list deleted domain %s tags: %w", domain.Name, listErr)
		}
		if tags == nil {
			return fmt.Errorf("list deleted domain %s tags returned no output", domain.Name)
		}
		if len(tags.TagList) != 0 {
			return fmt.Errorf("deleted domain %s still has tags", domain.Name)
		}
	}
	fmt.Println("ok: both domains and their configurations are deleted")
	return nil
}

func isNotFound(err error) bool {
	var notFound *opensearchtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func isDeletedDomainTagAbsence(err error) bool {
	if isNotFound(err) {
		return true
	}
	var validation *opensearchtypes.ValidationException
	return errors.As(err, &validation) &&
		aws.ToString(validation.Message) == "Invalid ARN. Domain not found."
}

func recordedDomainsPath() (string, error) {
	directory := os.Getenv("VERIFY_BUILD_DIR")
	if directory == "" {
		return "", fmt.Errorf("VERIFY_BUILD_DIR is required")
	}
	return filepath.Join(directory, "opensearch-domains.json"), nil
}

func writeRecordedDomains(recorded recordedDomains) error {
	path, err := recordedDomainsPath()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(recorded)
	if err != nil {
		return fmt.Errorf("encode domain identities: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write domain identities: %w", err)
	}
	return nil
}

func readRecordedDomains() (recordedDomains, error) {
	path, err := recordedDomainsPath()
	if err != nil {
		return recordedDomains{}, err
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return recordedDomains{}, fmt.Errorf("read domain identities: %w", err)
	}
	var recorded recordedDomains
	if err := json.Unmarshal(encoded, &recorded); err != nil {
		return recordedDomains{}, fmt.Errorf("decode domain identities: %w", err)
	}
	return recorded, nil
}
