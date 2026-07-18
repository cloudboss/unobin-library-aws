package opensearch

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
)

func (r DomainResource) createInput() (*awssdk.CreateDomainInput, error) {
	input := &awssdk.CreateDomainInput{DomainName: aws.String(r.DomainName)}
	if r.AccessPolicies != nil {
		normalized, err := normalizePolicyJSON(*r.AccessPolicies)
		if err != nil {
			return nil, fmt.Errorf("access-policies: %w", err)
		}
		input.AccessPolicies = aws.String(normalized)
	}
	input.AdvancedOptions = copyStringMap(r.AdvancedOptions)
	input.AdvancedSecurityOptions = domainAdvancedSecurityOptions(r.AdvancedSecurityOptions)
	input.AIMLOptions = domainAIMLOptions(r.AIMLOptions)
	autoTune, err := domainAutoTuneCreateOptions(r.AutoTuneOptions)
	if err != nil {
		return nil, err
	}
	input.AutoTuneOptions = autoTune
	pause, err := domainAutomatedSnapshotPauseOptions(r.AutomatedSnapshotPauseOptions)
	if err != nil {
		return nil, err
	}
	input.AutomatedSnapshotPauseOptions = pause
	input.ClusterConfig = domainClusterConfig(r.ClusterConfig, r.EngineVersion)
	input.CognitoOptions = domainCognitoOptions(r.CognitoOptions)
	input.DeploymentStrategyOptions = domainDeploymentStrategyOptions(
		r.DeploymentStrategyOptions,
	)
	input.DomainEndpointOptions = domainEndpointOptions(r.DomainEndpointOptions)
	input.EBSOptions = domainEBSOptions(r.EBSOptions)
	input.EncryptionAtRestOptions = domainEncryptionAtRestOptions(r.EncryptAtRest)
	input.EngineVersion = copyString(r.EngineVersion)
	if r.IPAddressType != nil {
		input.IPAddressType = awstypes.IPAddressType(*r.IPAddressType)
	}
	input.LogPublishingOptions = domainLogPublishingOptions(r.LogPublishingOptions)
	input.NodeToNodeEncryptionOptions = domainNodeToNodeEncryption(r.NodeToNodeEncryption)
	input.OffPeakWindowOptions = domainOffPeakWindowOptions(r.OffPeakWindowOptions, true)
	input.SnapshotOptions = domainSnapshotOptions(r.SnapshotOptions)
	input.SoftwareUpdateOptions = domainSoftwareUpdateOptions(r.SoftwareUpdateOptions)
	input.TagList = domainTags(r.Tags)
	input.VPCOptions = domainVPCOptions(r.VPCOptions)
	return input, nil
}

func domainOutput(status *awstypes.DomainStatus) (*DomainResourceOutput, error) {
	if status == nil {
		return nil, fmt.Errorf("read domain: response has no domain status")
	}
	if aws.ToString(status.DomainName) == "" || aws.ToString(status.DomainId) == "" ||
		aws.ToString(status.ARN) == "" || status.ClusterConfig == nil {
		return nil, fmt.Errorf("read domain: response has incomplete domain identity")
	}
	endpoint, endpointV2, endpoints, err := domainEndpoints(status)
	if err != nil {
		return nil, err
	}
	output := &DomainResourceOutput{
		DomainName:                   aws.ToString(status.DomainName),
		DomainID:                     aws.ToString(status.DomainId),
		ARN:                          aws.ToString(status.ARN),
		EngineVersion:                aws.ToString(status.EngineVersion),
		IPAddressType:                string(status.IPAddressType),
		Endpoint:                     endpoint,
		EndpointV2:                   endpointV2,
		Endpoints:                    endpoints,
		DashboardEndpoint:            dashboardEndpoint(endpoint),
		DashboardEndpointV2:          dashboardEndpoint(endpointV2),
		DomainEndpointV2HostedZoneID: copyString(status.DomainEndpointV2HostedZoneId),
		Created:                      aws.ToBool(status.Created),
		Deleted:                      aws.ToBool(status.Deleted),
		Processing:                   aws.ToBool(status.Processing),
		UpgradeProcessing:            aws.ToBool(status.UpgradeProcessing),
		VPCOptions:                   domainVPCOptionsOutput(status.VPCOptions),
		ServiceSoftwareOptions:       domainServiceSoftwareOutput(status.ServiceSoftwareOptions),
		AutomatedSnapshotPauseOptions: domainSnapshotPauseOutput(
			status.AutomatedSnapshotPauseOptions,
		),
	}
	if status.AdvancedSecurityOptions != nil {
		output.AnonymousAuthDisableDate = formatTime(
			status.AdvancedSecurityOptions.AnonymousAuthDisableDate,
		)
	}
	if status.IdentityCenterOptions != nil {
		output.IdentityCenterApplicationARN = copyString(
			status.IdentityCenterOptions.IdentityCenterApplicationARN,
		)
		output.IdentityStoreID = copyString(status.IdentityCenterOptions.IdentityStoreId)
	}
	return output, nil
}

func domainEndpoints(
	status *awstypes.DomainStatus,
) (*string, *string, *map[string]string, error) {
	endpoint := nonemptyString(status.Endpoint)
	endpointV2 := nonemptyString(status.EndpointV2)
	if status.VPCOptions == nil {
		if status.Endpoints != nil {
			return nil, nil, nil, fmt.Errorf("read domain: public domain returned VPC endpoints")
		}
		if endpoint == nil {
			return nil, nil, nil, fmt.Errorf("read domain: response has no endpoint")
		}
		return endpoint, endpointV2, nil, nil
	}
	if status.Endpoint != nil || status.EndpointV2 != nil {
		return nil, nil, nil, fmt.Errorf("read domain: VPC domain returned scalar endpoint")
	}
	if status.Endpoints == nil {
		return nil, nil, nil, fmt.Errorf("read domain: VPC domain returned no endpoint map")
	}
	if len(status.Endpoints) == 0 {
		return nil, nil, nil, fmt.Errorf("read domain: VPC domain returned empty endpoint map")
	}
	mappedEndpoint := strings.TrimSpace(status.Endpoints["vpc"])
	mappedEndpointV2 := strings.TrimSpace(status.Endpoints["vpcv2"])
	if mappedEndpoint == "" && mappedEndpointV2 == "" {
		return nil, nil, nil, fmt.Errorf("read domain: VPC endpoint map has no endpoint")
	}
	var primary, dualstack *string
	if mappedEndpoint != "" {
		primary = aws.String(mappedEndpoint)
	}
	if mappedEndpointV2 != "" {
		dualstack = aws.String(mappedEndpointV2)
	}
	cloned := maps.Clone(status.Endpoints)
	return primary, dualstack, &cloned, nil
}

func dashboardEndpoint(endpoint *string) *string {
	if endpoint == nil {
		return nil
	}
	return aws.String(*endpoint + "/_dashboards")
}

func domainTags(tags *map[string]string) []awstypes.Tag {
	if tags == nil {
		return nil
	}
	keys := make([]string, 0, len(*tags))
	for key := range *tags {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]awstypes.Tag, 0, len(keys))
	for _, key := range keys {
		result = append(result, awstypes.Tag{Key: aws.String(key), Value: aws.String((*tags)[key])})
	}
	return result
}

func domainLogPublishingOptions(
	options *[]DomainLogPublishingOption,
) map[string]awstypes.LogPublishingOption {
	if options == nil {
		return nil
	}
	result := make(map[string]awstypes.LogPublishingOption, len(*options))
	for _, option := range *options {
		enabled := option.Enabled
		if enabled == nil {
			enabled = aws.Bool(true)
		}
		result[option.LogType] = awstypes.LogPublishingOption{
			CloudWatchLogsLogGroupArn: aws.String(option.CloudWatchLogGroupARN),
			Enabled:                   copyBool(enabled),
		}
	}
	return result
}

func domainAutomatedSnapshotPauseOptions(
	options *DomainAutomatedSnapshotPauseRequestOptions,
) (*awstypes.AutomatedSnapshotPauseRequestOptions, error) {
	if options == nil {
		return nil, nil
	}
	result := &awstypes.AutomatedSnapshotPauseRequestOptions{Enabled: aws.Bool(options.Enabled)}
	var err error
	result.StartTime, err = parseOptionalTime(options.StartTime)
	if err != nil {
		return nil, fmt.Errorf("automated snapshot pause start-time: %w", err)
	}
	result.EndTime, err = parseOptionalTime(options.EndTime)
	if err != nil {
		return nil, fmt.Errorf("automated snapshot pause end-time: %w", err)
	}
	return result, nil
}

func parseOptionalTime(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func domainAdvancedSecurityOptions(
	options *DomainAdvancedSecurityOptions,
) *awstypes.AdvancedSecurityOptionsInput {
	if options == nil {
		return nil
	}
	result := &awstypes.AdvancedSecurityOptionsInput{
		Enabled: aws.Bool(options.Enabled),
	}
	if !options.Enabled {
		return result
	}
	result.AnonymousAuthEnabled = copyBool(options.AnonymousAuthEnabled)
	result.InternalUserDatabaseEnabled = aws.Bool(options.InternalUserDatabaseEnabled)
	if options.IAMFederationOptions != nil {
		federation := options.IAMFederationOptions
		result.IAMFederationOptions = &awstypes.IAMFederationOptionsInput{
			Enabled: copyBool(federation.Enabled),
		}
		if aws.ToBool(federation.Enabled) {
			result.IAMFederationOptions.RolesKey = copyString(federation.RolesKey)
			result.IAMFederationOptions.SubjectKey = copyString(federation.SubjectKey)
		}
	}
	if options.JWTOptions != nil {
		jwt := options.JWTOptions
		result.JWTOptions = &awstypes.JWTOptionsInput{
			Enabled: copyBool(jwt.Enabled),
		}
		if aws.ToBool(jwt.Enabled) {
			result.JWTOptions.JwksUrl = copyString(jwt.JWKSURL)
			result.JWTOptions.PublicKey = compactPublicKey(jwt.PublicKey)
			result.JWTOptions.RolesKey = copyString(jwt.RolesKey)
			result.JWTOptions.SubjectKey = copyString(jwt.SubjectKey)
		}
	}
	if options.MasterUserOptions != nil {
		master := options.MasterUserOptions
		result.MasterUserOptions = &awstypes.MasterUserOptions{
			MasterUserARN:      copyString(master.MasterUserARN),
			MasterUserName:     copyString(master.MasterUserName),
			MasterUserPassword: copyString(master.MasterUserPassword),
		}
	}
	if options.SAMLOptions != nil {
		saml := options.SAMLOptions
		result.SAMLOptions = &awstypes.SAMLOptionsInput{
			Enabled: aws.Bool(saml.Enabled),
		}
		if saml.Enabled {
			result.SAMLOptions.MasterBackendRole = copyString(saml.MasterBackendRole)
			result.SAMLOptions.MasterUserName = copyString(saml.MasterUserName)
			result.SAMLOptions.RolesKey = copyString(saml.RolesKey)
			result.SAMLOptions.SessionTimeoutMinutes = aws.Int32(
				int32(saml.SessionTimeoutMinutes),
			)
			result.SAMLOptions.SubjectKey = copyString(saml.SubjectKey)
		}
		if saml.Enabled && saml.IDP != nil {
			result.SAMLOptions.Idp = &awstypes.SAMLIdp{
				EntityId:        aws.String(saml.IDP.EntityID),
				MetadataContent: aws.String(saml.IDP.MetadataContent),
			}
		}
	}
	return result
}

func compactPublicKey(value *string) *string {
	if value == nil {
		return nil
	}
	compacted := strings.NewReplacer("\r", "", "\n", "").Replace(*value)
	return &compacted
}

func domainAIMLOptions(options *DomainAIMLOptions) *awstypes.AIMLOptionsInput {
	if options == nil {
		return nil
	}
	result := &awstypes.AIMLOptionsInput{}
	if options.NaturalLanguageQueryGenerationOptions != nil {
		result.NaturalLanguageQueryGenerationOptions =
			&awstypes.NaturalLanguageQueryGenerationOptionsInput{
				DesiredState: awstypes.NaturalLanguageQueryGenerationDesiredState(
					options.NaturalLanguageQueryGenerationOptions.DesiredState,
				),
			}
	}
	if options.S3VectorsEngine != nil {
		result.S3VectorsEngine = &awstypes.S3VectorsEngine{
			Enabled: copyBool(options.S3VectorsEngine.Enabled),
		}
	}
	if options.ServerlessVectorAcceleration != nil {
		result.ServerlessVectorAcceleration = &awstypes.ServerlessVectorAcceleration{
			Enabled: copyBool(options.ServerlessVectorAcceleration.Enabled),
		}
	}
	return result
}

func domainClusterConfig(
	config *DomainClusterConfig,
	engineVersion *string,
) *awstypes.ClusterConfig {
	if config == nil {
		return nil
	}
	result := &awstypes.ClusterConfig{
		DedicatedMasterEnabled:    aws.Bool(config.DedicatedMasterEnabled),
		InstanceCount:             aws.Int32(int32(config.InstanceCount)),
		InstanceType:              awstypes.OpenSearchPartitionInstanceType(config.InstanceType),
		MultiAZWithStandbyEnabled: copyBool(config.MultiAZWithStandbyEnabled),
		WarmEnabled:               copyBool(config.WarmEnabled),
		ZoneAwarenessEnabled:      copyBool(config.ZoneAwarenessEnabled),
	}
	if config.DedicatedMasterEnabled {
		result.DedicatedMasterCount = int32Pointer(config.DedicatedMasterCount)
	}
	if config.DedicatedMasterEnabled && config.DedicatedMasterType != nil {
		result.DedicatedMasterType = awstypes.OpenSearchPartitionInstanceType(
			*config.DedicatedMasterType,
		)
	}
	if aws.ToBool(config.WarmEnabled) {
		result.WarmCount = aws.Int32(int32(config.WarmCount))
	}
	if aws.ToBool(config.WarmEnabled) && config.WarmType != nil {
		result.WarmType = awstypes.OpenSearchWarmPartitionInstanceType(*config.WarmType)
	}
	if config.ColdStorageOptions != nil && supportsColdStorage(engineVersion) {
		result.ColdStorageOptions = &awstypes.ColdStorageOptions{
			Enabled: copyBool(config.ColdStorageOptions.Enabled),
		}
	}
	if aws.ToBool(config.ZoneAwarenessEnabled) && config.ZoneAwarenessConfig != nil {
		result.ZoneAwarenessConfig = &awstypes.ZoneAwarenessConfig{
			AvailabilityZoneCount: aws.Int32(
				int32(config.ZoneAwarenessConfig.AvailabilityZoneCount),
			),
		}
	}
	if config.NodeOptions != nil {
		result.NodeOptions = make([]awstypes.NodeOption, 0, len(*config.NodeOptions))
		for _, option := range *config.NodeOptions {
			converted := awstypes.NodeOption{NodeType: awstypes.NodeOptionsNodeType(option.NodeType)}
			if option.NodeConfig != nil {
				converted.NodeConfig = &awstypes.NodeConfig{
					Enabled: copyBool(option.NodeConfig.Enabled),
				}
				if aws.ToBool(option.NodeConfig.Enabled) {
					converted.NodeConfig.Count = int32Pointer(option.NodeConfig.Count)
				}
				if aws.ToBool(option.NodeConfig.Enabled) && option.NodeConfig.Type != nil {
					converted.NodeConfig.Type = awstypes.OpenSearchPartitionInstanceType(
						*option.NodeConfig.Type,
					)
				}
			}
			result.NodeOptions = append(result.NodeOptions, converted)
		}
	}
	return result
}

func supportsColdStorage(engineVersion *string) bool {
	if engineVersion == nil || !strings.HasPrefix(*engineVersion, "Elasticsearch_") {
		return true
	}
	version := strings.TrimPrefix(*engineVersion, "Elasticsearch_")
	return compareVersion(version, "7.9") >= 0
}

func domainCognitoOptions(options *DomainCognitoOptions) *awstypes.CognitoOptions {
	if options == nil {
		return nil
	}
	result := &awstypes.CognitoOptions{
		Enabled: aws.Bool(options.Enabled),
	}
	if options.Enabled {
		result.IdentityPoolId = aws.String(options.IdentityPoolID)
		result.RoleArn = aws.String(options.RoleARN)
		result.UserPoolId = aws.String(options.UserPoolID)
	}
	return result
}

func domainDeploymentStrategyOptions(
	options *DomainDeploymentStrategyOptions,
) *awstypes.DeploymentStrategyOptions {
	if options == nil {
		return nil
	}
	return &awstypes.DeploymentStrategyOptions{
		DeploymentStrategy: awstypes.DeploymentStrategy(options.DeploymentStrategy),
	}
}

func domainEndpointOptions(options *DomainEndpointOptions) *awstypes.DomainEndpointOptions {
	if options == nil {
		return nil
	}
	result := &awstypes.DomainEndpointOptions{
		CustomEndpointEnabled: aws.Bool(options.CustomEndpointEnabled),
		EnforceHTTPS:          aws.Bool(options.EnforceHTTPS),
	}
	if options.CustomEndpointEnabled {
		result.CustomEndpoint = copyString(options.CustomEndpoint)
		result.CustomEndpointCertificateArn = copyString(
			options.CustomEndpointCertificateARN,
		)
	}
	if options.TLSSecurityPolicy != nil {
		result.TLSSecurityPolicy = awstypes.TLSSecurityPolicy(*options.TLSSecurityPolicy)
	}
	return result
}

func domainEBSOptions(options *DomainEBSOptions) *awstypes.EBSOptions {
	if options == nil {
		return nil
	}
	result := &awstypes.EBSOptions{
		EBSEnabled: aws.Bool(options.EBSEnabled),
	}
	if !options.EBSEnabled {
		return result
	}
	result.VolumeSize = int32Pointer(options.VolumeSize)
	if options.VolumeType != nil {
		result.VolumeType = awstypes.VolumeType(*options.VolumeType)
		if *options.VolumeType == "gp3" || *options.VolumeType == "io1" {
			result.Iops = int32Pointer(options.IOPS)
		}
		if *options.VolumeType == "gp3" {
			result.Throughput = int32Pointer(options.Throughput)
		}
	}
	return result
}

func domainEncryptionAtRestOptions(
	options *DomainEncryptionAtRestOptions,
) *awstypes.EncryptionAtRestOptions {
	if options == nil {
		return nil
	}
	return &awstypes.EncryptionAtRestOptions{
		Enabled:  aws.Bool(options.Enabled),
		KmsKeyId: copyString(options.KMSKeyID),
	}
}

func domainNodeToNodeEncryption(
	options *DomainNodeToNodeEncryptionOptions,
) *awstypes.NodeToNodeEncryptionOptions {
	if options == nil {
		return nil
	}
	return &awstypes.NodeToNodeEncryptionOptions{Enabled: aws.Bool(options.Enabled)}
}

func domainOffPeakWindowOptions(
	options *DomainOffPeakWindowOptions,
	creating bool,
) *awstypes.OffPeakWindowOptions {
	if options == nil {
		return nil
	}
	enabled := copyBool(options.Enabled)
	if creating {
		enabled = aws.Bool(true)
	}
	result := &awstypes.OffPeakWindowOptions{Enabled: enabled}
	if options.OffPeakWindow != nil && options.OffPeakWindow.WindowStartTime != nil {
		start := options.OffPeakWindow.WindowStartTime
		result.OffPeakWindow = &awstypes.OffPeakWindow{
			WindowStartTime: &awstypes.WindowStartTime{
				Hours: start.Hours, Minutes: start.Minutes,
			},
		}
	}
	return result
}

func domainSnapshotOptions(options *DomainSnapshotOptions) *awstypes.SnapshotOptions {
	if options == nil {
		return nil
	}
	return &awstypes.SnapshotOptions{
		AutomatedSnapshotStartHour: aws.Int32(int32(options.AutomatedSnapshotStartHour)),
	}
}

func domainSoftwareUpdateOptions(
	options *DomainSoftwareUpdateOptions,
) *awstypes.SoftwareUpdateOptions {
	if options == nil {
		return nil
	}
	return &awstypes.SoftwareUpdateOptions{
		AutoSoftwareUpdateEnabled: aws.Bool(options.AutoSoftwareUpdateEnabled),
	}
}

func domainVPCOptions(options *DomainVPCOptions) *awstypes.VPCOptions {
	if options == nil {
		return nil
	}
	return &awstypes.VPCOptions{
		EgressEnabled:    copyBool(options.EgressEnabled),
		SecurityGroupIds: copyStringSlice(options.SecurityGroupIDs),
		SubnetIds:        copyStringSlice(options.SubnetIDs),
	}
}

func domainVPCOptionsOutput(options *awstypes.VPCDerivedInfo) *DomainVPCDerivedInfo {
	if options == nil {
		return nil
	}
	return &DomainVPCDerivedInfo{
		AvailabilityZones: slices.Clone(options.AvailabilityZones),
		EgressEnabled:     copyBool(options.EgressEnabled),
		SecurityGroupIDs:  slices.Clone(options.SecurityGroupIds),
		SubnetIDs:         slices.Clone(options.SubnetIds),
		VPCID:             aws.ToString(options.VPCId),
	}
}

func domainServiceSoftwareOutput(
	options *awstypes.ServiceSoftwareOptions,
) *DomainServiceSoftwareOptions {
	if options == nil {
		return nil
	}
	return &DomainServiceSoftwareOptions{
		AutomatedUpdateDate: formatTime(options.AutomatedUpdateDate),
		Cancellable:         aws.ToBool(options.Cancellable),
		CurrentVersion:      copyString(options.CurrentVersion),
		Description:         copyString(options.Description),
		NewVersion:          copyString(options.NewVersion),
		OptionalDeployment:  aws.ToBool(options.OptionalDeployment),
		UpdateAvailable:     aws.ToBool(options.UpdateAvailable),
		UpdateStatus:        string(options.UpdateStatus),
	}
}

func domainSnapshotPauseOutput(
	options *awstypes.AutomatedSnapshotPauseOptions,
) *DomainAutomatedSnapshotPauseOptions {
	if options == nil {
		return nil
	}
	return &DomainAutomatedSnapshotPauseOptions{
		Enabled:   aws.ToBool(options.Enabled),
		State:     string(options.State),
		StartTime: formatTime(options.StartTime),
		EndTime:   formatTime(options.EndTime),
	}
}

func formatTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func nonemptyString(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func copyBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func int32Pointer(value *int64) *int32 {
	if value == nil {
		return nil
	}
	return aws.Int32(int32(*value))
}

func copyStringMap(value *map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	return maps.Clone(*value)
}

func copyStringSlice(value *[]string) []string {
	if value == nil {
		return nil
	}
	return slices.Clone(*value)
}
