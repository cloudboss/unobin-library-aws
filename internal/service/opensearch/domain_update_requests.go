package opensearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	awspolicy "github.com/hashicorp/awspolicyequivalence"
)

func (r DomainResource) updateConfigInput(
	prior DomainResource,
	name string,
) (*awssdk.UpdateDomainConfigInput, bool, error) {
	return r.updateConfigInputForEngine(prior, name, r.EngineVersion)
}

func (r DomainResource) updateConfigInputForEngine(
	prior DomainResource,
	name string,
	engineVersion *string,
) (*awssdk.UpdateDomainConfigInput, bool, error) {
	input := &awssdk.UpdateDomainConfigInput{DomainName: aws.String(name)}
	needed := false
	if runtime.Changed(prior.AccessPolicies, r.AccessPolicies) && r.AccessPolicies != nil {
		equivalent, normalized, err := equivalentPolicyJSON(
			stringValue(prior.AccessPolicies), *r.AccessPolicies,
		)
		if err != nil {
			return nil, false, fmt.Errorf("access-policies: %w", err)
		}
		if !equivalent {
			input.AccessPolicies = aws.String(normalized)
			needed = true
		}
	}
	if runtime.Changed(prior.AdvancedOptions, r.AdvancedOptions) {
		if r.AdvancedOptions == nil {
			input.AdvancedOptions = map[string]string{}
		} else {
			input.AdvancedOptions = copyStringMap(r.AdvancedOptions)
		}
		needed = true
	}
	if runtime.Changed(prior.AdvancedSecurityOptions, r.AdvancedSecurityOptions) {
		input.AdvancedSecurityOptions = domainAdvancedSecurityUpdate(
			prior.AdvancedSecurityOptions,
			r.AdvancedSecurityOptions,
		)
		if input.AdvancedSecurityOptions != nil {
			needed = true
		}
	}
	if runtime.Changed(prior.AIMLOptions, r.AIMLOptions) && r.AIMLOptions != nil {
		input.AIMLOptions = domainAIMLOptions(r.AIMLOptions)
		needed = true
	}
	if !equivalentDomainAutoTuneOptions(prior.AutoTuneOptions, r.AutoTuneOptions) &&
		r.AutoTuneOptions != nil {
		autoTune, err := domainAutoTuneOptions(r.AutoTuneOptions)
		if err != nil {
			return nil, false, err
		}
		input.AutoTuneOptions = autoTune
		needed = true
	}
	if runtime.Changed(
		prior.AutomatedSnapshotPauseOptions,
		r.AutomatedSnapshotPauseOptions,
	) {
		if r.AutomatedSnapshotPauseOptions == nil {
			input.AutomatedSnapshotPauseOptions =
				&awstypes.AutomatedSnapshotPauseRequestOptions{Enabled: aws.Bool(false)}
		} else {
			pause, err := domainAutomatedSnapshotPauseOptions(
				r.AutomatedSnapshotPauseOptions,
			)
			if err != nil {
				return nil, false, err
			}
			input.AutomatedSnapshotPauseOptions = pause
		}
		needed = true
	}
	clusterChanged := runtime.Changed(prior.ClusterConfig, r.ClusterConfig)
	ebsChanged := runtime.Changed(prior.EBSOptions, r.EBSOptions)
	if clusterChanged && r.ClusterConfig != nil {
		input.ClusterConfig = domainClusterConfig(r.ClusterConfig, engineVersion)
		needed = true
	}
	if (clusterChanged || ebsChanged) && r.EBSOptions != nil {
		input.EBSOptions = domainEBSOptions(r.EBSOptions)
		needed = true
	}
	if runtime.Changed(prior.CognitoOptions, r.CognitoOptions) {
		if r.CognitoOptions == nil {
			input.CognitoOptions = &awstypes.CognitoOptions{Enabled: aws.Bool(false)}
		} else {
			input.CognitoOptions = domainCognitoOptions(r.CognitoOptions)
		}
		needed = true
	}
	if runtime.Changed(
		prior.DeploymentStrategyOptions,
		r.DeploymentStrategyOptions,
	) && r.DeploymentStrategyOptions != nil {
		input.DeploymentStrategyOptions = domainDeploymentStrategyOptions(
			r.DeploymentStrategyOptions,
		)
		needed = true
	}
	if runtime.Changed(prior.DomainEndpointOptions, r.DomainEndpointOptions) &&
		r.DomainEndpointOptions != nil {
		input.DomainEndpointOptions = domainEndpointOptions(r.DomainEndpointOptions)
		needed = true
	}
	if runtime.Changed(prior.EncryptAtRest, r.EncryptAtRest) && r.EncryptAtRest != nil {
		input.EncryptionAtRestOptions = domainEncryptionAtRestOptions(r.EncryptAtRest)
		needed = true
	}
	if runtime.Changed(prior.IdentityCenterOptions, r.IdentityCenterOptions) {
		if r.IdentityCenterOptions == nil {
			input.IdentityCenterOptions = &awstypes.IdentityCenterOptionsInput{
				EnabledAPIAccess: aws.Bool(false),
			}
		} else {
			input.IdentityCenterOptions = domainIdentityCenterOptions(r.IdentityCenterOptions)
		}
		needed = true
	}
	if runtime.Changed(prior.IPAddressType, r.IPAddressType) && r.IPAddressType != nil {
		input.IPAddressType = awstypes.IPAddressType(*r.IPAddressType)
		needed = true
	}
	if !equivalentDomainLogPublishingOptions(
		prior.LogPublishingOptions,
		r.LogPublishingOptions,
	) {
		input.LogPublishingOptions = domainLogPublishingUpdate(
			prior.LogPublishingOptions,
			r.LogPublishingOptions,
		)
		needed = true
	}
	if runtime.Changed(prior.NodeToNodeEncryption, r.NodeToNodeEncryption) &&
		r.NodeToNodeEncryption != nil {
		input.NodeToNodeEncryptionOptions = domainNodeToNodeEncryption(
			r.NodeToNodeEncryption,
		)
		needed = true
	}
	if runtime.Changed(prior.OffPeakWindowOptions, r.OffPeakWindowOptions) &&
		r.OffPeakWindowOptions != nil {
		input.OffPeakWindowOptions = domainOffPeakWindowOptions(r.OffPeakWindowOptions, false)
		needed = true
	}
	if runtime.Changed(prior.SnapshotOptions, r.SnapshotOptions) && r.SnapshotOptions != nil {
		input.SnapshotOptions = domainSnapshotOptions(r.SnapshotOptions)
		needed = true
	}
	if runtime.Changed(prior.SoftwareUpdateOptions, r.SoftwareUpdateOptions) &&
		r.SoftwareUpdateOptions != nil {
		input.SoftwareUpdateOptions = domainSoftwareUpdateOptions(r.SoftwareUpdateOptions)
		needed = true
	}
	if !needed {
		return nil, false, nil
	}
	return input, true, nil
}

func domainAdvancedSecurityUpdate(
	prior *DomainAdvancedSecurityOptions,
	current *DomainAdvancedSecurityOptions,
) *awstypes.AdvancedSecurityOptionsInput {
	var result *awstypes.AdvancedSecurityOptionsInput
	if current != nil {
		result = domainAdvancedSecurityOptions(current)
	}
	if prior == nil {
		return result
	}
	if prior.JWTOptions != nil && (current == nil || current.JWTOptions == nil) {
		if result == nil {
			result = &awstypes.AdvancedSecurityOptionsInput{}
		}
		result.JWTOptions = &awstypes.JWTOptionsInput{Enabled: aws.Bool(false)}
	}
	if prior.SAMLOptions != nil && (current == nil || current.SAMLOptions == nil) {
		if result == nil {
			result = &awstypes.AdvancedSecurityOptionsInput{}
		}
		result.SAMLOptions = &awstypes.SAMLOptionsInput{Enabled: aws.Bool(false)}
	}
	if prior.IAMFederationOptions != nil &&
		(current == nil || current.IAMFederationOptions == nil) {
		if result == nil {
			result = &awstypes.AdvancedSecurityOptionsInput{}
		}
		result.IAMFederationOptions = &awstypes.IAMFederationOptionsInput{
			Enabled: aws.Bool(false),
		}
	}
	return result
}

func domainLogPublishingUpdate(
	prior *[]DomainLogPublishingOption,
	current *[]DomainLogPublishingOption,
) map[string]awstypes.LogPublishingOption {
	result := domainLogPublishingOptions(current)
	if result == nil {
		result = map[string]awstypes.LogPublishingOption{}
	}
	currentTypes := map[string]bool{}
	if current != nil {
		for _, option := range *current {
			currentTypes[option.LogType] = true
		}
	}
	if prior != nil {
		for _, option := range *prior {
			if !currentTypes[option.LogType] {
				result[option.LogType] = awstypes.LogPublishingOption{
					Enabled: aws.Bool(false),
				}
			}
		}
	}
	return result
}

func equivalentPolicyJSON(left, right string) (bool, string, error) {
	rightNormalized, err := normalizePolicyJSON(right)
	if err != nil {
		return false, "", fmt.Errorf("policy is invalid JSON: %w", err)
	}
	if left != "" {
		if _, err := normalizePolicyJSON(left); err != nil {
			return false, "", fmt.Errorf("prior policy is invalid JSON: %w", err)
		}
	}
	equivalent, err := awspolicy.PoliciesAreEquivalent(left, right)
	if err != nil {
		return false, "", fmt.Errorf("compare policies: %w", err)
	}
	return equivalent, rightNormalized, nil
}

func normalizePolicyJSON(document string) (string, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(document))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("multiple JSON values")
		}
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
