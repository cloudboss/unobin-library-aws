package opensearch

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r DomainResource) preflightUpdate(
	ctx context.Context,
	client domainClient,
	name string,
	prior DomainResource,
) error {
	return r.preflightUpdateForEngine(ctx, client, name, prior, currentEngineVersion(prior, r))
}

func (r DomainResource) preflightUpdateForEngine(
	ctx context.Context,
	client domainClient,
	name string,
	prior DomainResource,
	engineVersion string,
) error {
	if err := validateConditionalReplacementsForEngine(prior, r, engineVersion); err != nil {
		return err
	}
	if r.EngineVersion == nil || !runtime.Changed(prior.EngineVersion, r.EngineVersion) {
		return nil
	}
	output, err := client.GetCompatibleVersions(ctx, &awssdk.GetCompatibleVersionsInput{
		DomainName: aws.String(name),
	})
	if err != nil {
		return nil
	}
	if output == nil || len(output.CompatibleVersions) != 1 ||
		!slices.Contains(output.CompatibleVersions[0].TargetVersions, *r.EngineVersion) {
		return fmt.Errorf("engine-version change to %s requires replacement", *r.EngineVersion)
	}
	return nil
}

func validateConditionalReplacements(prior, current DomainResource) error {
	return validateConditionalReplacementsForEngine(
		prior,
		current,
		currentEngineVersion(prior, current),
	)
}

func validateConditionalReplacementsForEngine(
	prior,
	current DomainResource,
	engineVersion string,
) error {
	if current.EncryptAtRest != nil {
		priorEnabled := prior.EncryptAtRest != nil && prior.EncryptAtRest.Enabled
		if priorEnabled && !current.EncryptAtRest.Enabled {
			return fmt.Errorf("disabling encrypt-at-rest requires replacement")
		}
		if !priorEnabled && current.EncryptAtRest.Enabled &&
			!supportsInPlaceEncryption(engineVersion) {
			return fmt.Errorf("enabling encrypt-at-rest for %s requires replacement",
				engineVersion)
		}
		if prior.EncryptAtRest != nil && !equivalentKMSKeyIDs(
			prior.EncryptAtRest.KMSKeyID,
			current.EncryptAtRest.KMSKeyID,
		) {
			return fmt.Errorf("changing encrypt-at-rest kms-key-id requires replacement")
		}
	}
	if current.NodeToNodeEncryption != nil {
		priorEnabled := prior.NodeToNodeEncryption != nil &&
			prior.NodeToNodeEncryption.Enabled
		if priorEnabled && !current.NodeToNodeEncryption.Enabled {
			return fmt.Errorf("disabling node-to-node-encryption requires replacement")
		}
		if !priorEnabled && current.NodeToNodeEncryption.Enabled &&
			!supportsInPlaceEncryption(engineVersion) {
			return fmt.Errorf("enabling node-to-node-encryption for %s requires replacement",
				engineVersion)
		}
	}
	if current.AdvancedSecurityOptions != nil && prior.AdvancedSecurityOptions != nil &&
		prior.AdvancedSecurityOptions.Enabled && !current.AdvancedSecurityOptions.Enabled {
		return fmt.Errorf("disabling advanced-security-options requires replacement")
	}
	if prior.IPAddressType != nil && current.IPAddressType != nil &&
		*prior.IPAddressType == "dualstack" && *current.IPAddressType != "dualstack" {
		return fmt.Errorf("changing ip-address-type from dualstack requires replacement")
	}
	return nil
}

func currentEngineVersion(prior, current DomainResource) string {
	if current.EngineVersion != nil {
		return *current.EngineVersion
	}
	if prior.EngineVersion != nil {
		return *prior.EngineVersion
	}
	return ""
}

func supportsInPlaceEncryption(engineVersion string) bool {
	switch {
	case strings.HasPrefix(engineVersion, "OpenSearch_"):
		return true
	case strings.HasPrefix(engineVersion, "Elasticsearch_"):
		return compareVersion(strings.TrimPrefix(engineVersion, "Elasticsearch_"), "6.7") >= 0
	default:
		return false
	}
}

func equivalentKMSKeyIDs(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if *left == *right {
		return true
	}
	if keyID, ok := kmsARNKeyID(*left); ok && keyID == *right {
		return true
	}
	keyID, ok := kmsARNKeyID(*right)
	return ok && keyID == *left
}

func kmsARNKeyID(value string) (string, bool) {
	parsed, err := awsarn.Parse(value)
	if err != nil || parsed.Service != "kms" || !strings.HasPrefix(parsed.Resource, "key/") {
		return "", false
	}
	keyID := strings.TrimPrefix(parsed.Resource, "key/")
	return keyID, keyID != "" && !strings.Contains(keyID, "/")
}
