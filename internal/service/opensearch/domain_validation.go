package opensearch

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
)

var (
	domainNamePattern         = regexp.MustCompile(`^[a-z][0-9a-z-]{2,27}$`)
	domainARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	domainARNRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(-[a-z]+)+-[0-9]{1,2}$`)
	domainARNAccountPattern   = regexp.MustCompile(
		`^([0-9]{12}|aws|aws-managed|third-party|aws-marketplace|partner-managed|cw.{10})$`,
	)
)

func (r DomainResource) validateInputs(ctx context.Context, cfg *awsCfg) error {
	return r.validateInputsForEngine(ctx, cfg, r.EngineVersion)
}

func (r DomainResource) validateInputsForEngine(
	_ context.Context,
	_ *awsCfg,
	engineVersion *string,
) error {
	if !domainNamePattern.MatchString(r.DomainName) {
		return errors.New(
			"domain-name must contain 3 to 28 lowercase letters, digits, or hyphens " +
				"and start with a lowercase letter",
		)
	}
	if err := validateDomainUnorderedCollections(r); err != nil {
		return err
	}
	if r.AccessPolicies != nil {
		if _, err := normalizePolicyJSON(*r.AccessPolicies); err != nil {
			return fmt.Errorf("access-policies: %w", err)
		}
	}
	if err := validateAdvancedSecurity(r.AdvancedSecurityOptions, engineVersion); err != nil {
		return err
	}
	if err := validateAIMLOptions(r.AIMLOptions); err != nil {
		return err
	}
	if err := validateAutoTune(r.AutoTuneOptions); err != nil {
		return err
	}
	if r.AutomatedSnapshotPauseOptions != nil {
		if err := validateAutomatedSnapshotPause(*r.AutomatedSnapshotPauseOptions); err != nil {
			return err
		}
	}
	if err := validateClusterConfig(r.ClusterConfig); err != nil {
		return err
	}
	if err := validateCognitoOptions(r.CognitoOptions); err != nil {
		return err
	}
	if r.DeploymentStrategyOptions != nil && !oneOf(
		r.DeploymentStrategyOptions.DeploymentStrategy,
		"Default", "CapacityOptimized",
	) {
		return errors.New("deployment-strategy must be Default or CapacityOptimized")
	}
	if err := validateEndpointOptions(r.DomainEndpointOptions); err != nil {
		return err
	}
	if err := validateEBSOptions(r.EBSOptions); err != nil {
		return err
	}
	if r.IPAddressType != nil && !oneOf(*r.IPAddressType, "ipv4", "dualstack") {
		return errors.New("ip-address-type must be ipv4 or dualstack")
	}
	if err := validateLogPublishingOptions(r.LogPublishingOptions); err != nil {
		return err
	}
	if err := validateOffPeakWindow(r.OffPeakWindowOptions); err != nil {
		return err
	}
	if r.SnapshotOptions != nil && (r.SnapshotOptions.AutomatedSnapshotStartHour < 0 ||
		r.SnapshotOptions.AutomatedSnapshotStartHour > 23) {
		return errors.New("automated-snapshot-start-hour must be between 0 and 23")
	}
	if err := validateIdentityCenterOptions(r.IdentityCenterOptions); err != nil {
		return err
	}
	if err := validateDomainTags(r.Tags); err != nil {
		return err
	}
	return nil
}

func validateDomainUnorderedCollections(r DomainResource) error {
	if r.VPCOptions != nil {
		if r.VPCOptions.SecurityGroupIDs != nil &&
			domainSliceHasDuplicate(*r.VPCOptions.SecurityGroupIDs) {
			return errors.New("vpc-options contains duplicate security-group-ids")
		}
		if r.VPCOptions.SubnetIDs != nil &&
			domainSliceHasDuplicate(*r.VPCOptions.SubnetIDs) {
			return errors.New("vpc-options contains duplicate subnet-ids")
		}
	}
	if r.AutoTuneOptions != nil && r.AutoTuneOptions.MaintenanceSchedule != nil &&
		domainSliceHasDuplicate(*r.AutoTuneOptions.MaintenanceSchedule) {
		return errors.New("auto-tune-options contains duplicate maintenance-schedule")
	}
	if r.LogPublishingOptions != nil &&
		domainSliceHasDuplicate(*r.LogPublishingOptions) {
		return errors.New("duplicate log-publishing-options entry")
	}
	return nil
}

func validateAdvancedSecurity(
	options *DomainAdvancedSecurityOptions,
	engineVersion *string,
) error {
	if options == nil {
		return nil
	}
	if options.MasterUserOptions != nil && options.MasterUserOptions.MasterUserARN != nil {
		if err := validateDomainARN(
			"advanced-security-options.master-user-options.master-user-arn",
			*options.MasterUserOptions.MasterUserARN,
		); err != nil {
			return err
		}
	}
	if options.SAMLOptions != nil && options.SAMLOptions.IDP != nil &&
		utf8.RuneCountInString(options.SAMLOptions.IDP.MetadataContent) > 1_048_576 {
		return errors.New(
			"saml-options idp metadata-content must contain at most 1048576 characters",
		)
	}
	if !options.Enabled {
		return nil
	}
	if options.JWTOptions != nil && boolValue(options.JWTOptions.Enabled) {
		jwt := options.JWTOptions
		if engineVersion != nil && (!strings.HasPrefix(*engineVersion, "OpenSearch_") ||
			compareVersion(strings.TrimPrefix(*engineVersion, "OpenSearch_"), "2.11") < 0) {
			return errors.New("jwt-options requires OpenSearch 2.11 or later")
		}
		publicKey := stringValue(jwt.PublicKey)
		jwksURL := stringValue(jwt.JWKSURL)
		if publicKey == "" && jwksURL == "" {
			return errors.New("jwt-options enabled requires jwks-url or public-key")
		}
		if jwt.JWKSURL != nil && (len(jwksURL) < 1 || len(jwksURL) > 2048) {
			return errors.New("jwt-options jwks-url must contain 1 to 2048 characters")
		}
		if err := validateOptionalStringLength("jwt-options roles-key", jwt.RolesKey, 1, 64); err != nil {
			return err
		}
		if err := validateOptionalStringLength(
			"jwt-options subject-key", jwt.SubjectKey, 1, 64,
		); err != nil {
			return err
		}
	}
	if options.SAMLOptions != nil && options.SAMLOptions.Enabled {
		saml := options.SAMLOptions
		if saml.SessionTimeoutMinutes < 1 || saml.SessionTimeoutMinutes > 1440 {
			return errors.New("saml-options session-timeout-minutes must be between 1 and 1440")
		}
		if saml.IDP == nil {
			return errors.New("saml-options enabled requires idp")
		}
		if saml.IDP.EntityID == "" {
			return errors.New("saml-options idp entity-id must not be empty")
		}
		if saml.IDP.MetadataContent == "" {
			return errors.New("saml-options idp metadata-content must not be empty")
		}
		if saml.MasterBackendRole != nil && *saml.MasterBackendRole == "" {
			return errors.New("saml-options master-backend-role must not be empty")
		}
		if saml.MasterUserName != nil && *saml.MasterUserName == "" {
			return errors.New("saml-options master-user-name must not be empty")
		}
	}
	return nil
}

func validateOptionalStringLength(field string, value *string, minimum, maximum int) error {
	if value == nil {
		return nil
	}
	if len(*value) < minimum || len(*value) > maximum {
		return fmt.Errorf("%s must contain %d to %d characters", field, minimum, maximum)
	}
	return nil
}

func validateAIMLOptions(options *DomainAIMLOptions) error {
	if options == nil || options.NaturalLanguageQueryGenerationOptions == nil {
		return nil
	}
	desired := options.NaturalLanguageQueryGenerationOptions.DesiredState
	if !oneOf(desired, "ENABLED", "DISABLED") {
		return errors.New("aiml-options desired-state must be ENABLED or DISABLED")
	}
	return nil
}

func validateAutoTune(options *DomainAutoTuneOptions) error {
	if options == nil {
		return nil
	}
	if !oneOf(options.DesiredState, "ENABLED", "DISABLED") {
		return errors.New("auto-tune-options desired-state must be ENABLED or DISABLED")
	}
	if options.RollbackOnDisable != nil && !oneOf(
		*options.RollbackOnDisable, "NO_ROLLBACK", "DEFAULT_ROLLBACK",
	) {
		return errors.New(
			"auto-tune-options rollback-on-disable must be NO_ROLLBACK or DEFAULT_ROLLBACK",
		)
	}
	if options.UseOffPeakWindow && options.MaintenanceSchedule != nil &&
		len(*options.MaintenanceSchedule) > 0 {
		return errors.New(
			"auto-tune-options maintenance-schedule conflicts with use-off-peak-window",
		)
	}
	if options.MaintenanceSchedule == nil {
		return nil
	}
	for index, schedule := range *options.MaintenanceSchedule {
		if _, err := time.Parse(time.RFC3339, schedule.StartAt); err != nil {
			return fmt.Errorf(
				"auto-tune-options maintenance-schedule %d start-at must be RFC3339",
				index,
			)
		}
		if schedule.CronExpressionForRecurrence == "" {
			return fmt.Errorf(
				"auto-tune-options maintenance-schedule %d cron expression must not be empty",
				index,
			)
		}
		if schedule.Duration.Unit != "HOURS" {
			return fmt.Errorf(
				"auto-tune-options maintenance-schedule %d duration unit must be HOURS",
				index,
			)
		}
		if schedule.Duration.Value <= 0 {
			return fmt.Errorf(
				"auto-tune-options maintenance-schedule %d duration value must be greater than zero",
				index,
			)
		}
	}
	return nil
}

func validateClusterConfig(config *DomainClusterConfig) error {
	if config == nil {
		return nil
	}
	if config.InstanceCount < 1 {
		return errors.New("cluster-config instance-count must be at least 1")
	}
	if config.InstanceType == "" {
		return errors.New("cluster-config instance-type must not be empty")
	}
	if config.DedicatedMasterEnabled && config.DedicatedMasterCount != nil &&
		*config.DedicatedMasterCount < 1 {
		return errors.New("cluster-config dedicated-master-count must be at least 1")
	}
	if config.DedicatedMasterEnabled && config.DedicatedMasterType != nil &&
		*config.DedicatedMasterType == "" {
		return errors.New("cluster-config dedicated-master-type must not be empty")
	}
	if boolValue(config.WarmEnabled) {
		if config.WarmCount < 2 || config.WarmCount > 150 {
			return errors.New("cluster-config warm-count must be between 2 and 150")
		}
		if config.WarmType != nil && !oneOf(
			*config.WarmType,
			"ultrawarm1.medium.search",
			"ultrawarm1.large.search",
			"ultrawarm1.xlarge.search",
		) {
			return errors.New("cluster-config warm-type is not supported")
		}
	}
	if boolValue(config.ZoneAwarenessEnabled) && config.ZoneAwarenessConfig != nil &&
		!oneOf(config.ZoneAwarenessConfig.AvailabilityZoneCount, int64(2), int64(3)) {
		return errors.New("cluster-config availability-zone-count must be 2 or 3")
	}
	if config.NodeOptions == nil {
		return nil
	}
	for index, option := range *config.NodeOptions {
		if option.NodeType != "coordinator" {
			return fmt.Errorf("cluster-config node-options %d node-type must be coordinator", index)
		}
		if option.NodeConfig == nil || !boolValue(option.NodeConfig.Enabled) {
			continue
		}
		if option.NodeConfig.Count != nil && *option.NodeConfig.Count < 1 {
			return fmt.Errorf("cluster-config node-options count at index %d must be at least 1", index)
		}
		if option.NodeConfig.Type != nil && *option.NodeConfig.Type == "" {
			return fmt.Errorf("cluster-config node-options %d type must not be empty", index)
		}
	}
	return nil
}

func validateCognitoOptions(options *DomainCognitoOptions) error {
	if options == nil || !options.Enabled {
		return nil
	}
	if options.IdentityPoolID == "" || options.RoleARN == "" || options.UserPoolID == "" {
		return errors.New(
			"cognito-options enabled requires identity-pool-id, role-arn, and user-pool-id",
		)
	}
	return validateDomainARN("cognito-options.role-arn", options.RoleARN)
}

func validateEndpointOptions(options *DomainEndpointOptions) error {
	if options == nil {
		return nil
	}
	if options.CustomEndpointCertificateARN != nil {
		if err := validateDomainARN(
			"domain-endpoint-options.custom-endpoint-certificate-arn",
			*options.CustomEndpointCertificateARN,
		); err != nil {
			return err
		}
	}
	if options.TLSSecurityPolicy == nil {
		return nil
	}
	if !oneOf(
		*options.TLSSecurityPolicy,
		"Policy-Min-TLS-1-0-2019-07",
		"Policy-Min-TLS-1-2-2019-07",
		"Policy-Min-TLS-1-2-PFS-2023-10",
		"Policy-Min-TLS-1-2-RFC9151-FIPS-2024-08",
	) {
		return errors.New("domain-endpoint-options tls-security-policy is not supported")
	}
	return nil
}

func validateEBSOptions(options *DomainEBSOptions) error {
	if options == nil || !options.EBSEnabled {
		return nil
	}
	volumeType := stringValue(options.VolumeType)
	if options.VolumeType != nil && !oneOf(volumeType, "standard", "gp2", "io1", "gp3") {
		return errors.New("ebs-options volume-type must be standard, gp2, io1, or gp3")
	}
	if options.VolumeSize != nil && *options.VolumeSize < 1 {
		return errors.New("ebs-options volume-size must be at least 1")
	}
	if options.IOPS != nil {
		if *options.IOPS < 1 {
			return errors.New("ebs-options iops must be at least 1")
		}
		if !oneOf(volumeType, "io1", "gp3") {
			return errors.New("ebs-options iops requires volume-type io1 or gp3")
		}
	}
	if options.Throughput != nil {
		if *options.Throughput < 125 {
			return errors.New("ebs-options throughput must be at least 125")
		}
		if volumeType != "gp3" {
			return errors.New("ebs-options throughput requires volume-type gp3")
		}
	}
	return nil
}

func validateLogPublishingOptions(options *[]DomainLogPublishingOption) error {
	if options == nil {
		return nil
	}
	for index, option := range *options {
		if !oneOf(
			option.LogType,
			"INDEX_SLOW_LOGS",
			"SEARCH_SLOW_LOGS",
			"ES_APPLICATION_LOGS",
			"AUDIT_LOGS",
		) {
			return fmt.Errorf("log-publishing-options %d log-type is not supported", index)
		}
		if err := validateDomainARN(
			fmt.Sprintf("log-publishing-options %d cloudwatch-log-group-arn", index),
			option.CloudWatchLogGroupARN,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateOffPeakWindow(options *DomainOffPeakWindowOptions) error {
	if options == nil || options.OffPeakWindow == nil ||
		options.OffPeakWindow.WindowStartTime == nil {
		return nil
	}
	start := options.OffPeakWindow.WindowStartTime
	if start.Hours < 0 || start.Hours > 23 {
		return errors.New("off-peak-window-options hours must be between 0 and 23")
	}
	if start.Minutes < 0 || start.Minutes > 59 {
		return errors.New("off-peak-window-options minutes must be between 0 and 59")
	}
	return nil
}

func validateIdentityCenterOptions(options *DomainIdentityCenterOptions) error {
	if options == nil || !boolValue(options.EnabledAPIAccess) {
		return nil
	}
	if options.IdentityCenterInstanceARN != nil {
		if err := validateDomainARN(
			"identity-center-options.identity-center-instance-arn",
			*options.IdentityCenterInstanceARN,
		); err != nil {
			return err
		}
	}
	if options.RolesKey != nil && !oneOf(*options.RolesKey, "GroupName", "GroupId") {
		return errors.New("identity-center-options roles-key must be GroupName or GroupId")
	}
	if options.SubjectKey != nil && !oneOf(
		*options.SubjectKey, "UserName", "UserId", "Email",
	) {
		return errors.New(
			"identity-center-options subject-key must be UserName, UserId, or Email",
		)
	}
	return nil
}

func validateDomainTags(tags *map[string]string) error {
	if tags == nil {
		return nil
	}
	if len(*tags) > 10 {
		return errors.New("tags must contain at most 10 entries")
	}
	for key, value := range *tags {
		if utf8.RuneCountInString(key) > 128 {
			return errors.New("tag key must contain at most 128 characters")
		}
		if utf8.RuneCountInString(value) > 256 {
			return fmt.Errorf(
				"tag value for key %q must contain at most 256 characters",
				key,
			)
		}
	}
	return nil
}

func validateDomainARN(field, value string) error {
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid ARN: %w", field, err)
	}
	switch {
	case !domainARNPartitionPattern.MatchString(parsed.Partition):
		err = errors.New("partition is empty or malformed")
	case parsed.Service == "":
		err = errors.New("service is empty")
	case parsed.Region != "" && !domainARNRegionPattern.MatchString(parsed.Region):
		err = errors.New("region is malformed")
	case parsed.AccountID != "" && !domainARNAccountPattern.MatchString(parsed.AccountID):
		err = errors.New("account is malformed")
	case parsed.Resource == "":
		err = errors.New("resource is empty")
	}
	if err != nil {
		return fmt.Errorf("%s must be a valid ARN: %w", field, err)
	}
	return nil
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func oneOf[T comparable](value T, allowed ...T) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateAutomatedSnapshotPause(options DomainAutomatedSnapshotPauseRequestOptions) error {
	if !options.Enabled {
		return nil
	}
	if options.EndTime == nil {
		return errors.New("automated-snapshot-pause-options enabled requires end-time")
	}
	start, err := parseOptionalTime(options.StartTime)
	if err != nil {
		return errors.New("automated-snapshot-pause-options start-time must be RFC3339")
	}
	end, err := parseOptionalTime(options.EndTime)
	if err != nil {
		return errors.New("automated-snapshot-pause-options end-time must be RFC3339")
	}
	if start != nil {
		duration := end.Sub(*start)
		if duration <= 0 || duration > 72*time.Hour {
			return errors.New(
				"automated-snapshot-pause-options interval must be greater than zero " +
					"and at most 72 hours",
			)
		}
	}
	return nil
}

func compareVersion(left, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := 0; index < len(leftParts) || index < len(rightParts); index++ {
		leftPart := versionPart(leftParts, index)
		rightPart := versionPart(rightParts, index)
		if leftPart < rightPart {
			return -1
		}
		if leftPart > rightPart {
			return 1
		}
	}
	return 0
}

func versionPart(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	value := 0
	for _, character := range parts[index] {
		if character < '0' || character > '9' {
			break
		}
		value = value*10 + int(character-'0')
	}
	return value
}
