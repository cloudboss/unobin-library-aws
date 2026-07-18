package opensearch

type DomainResource struct {
	DomainName                    string                                      `ub:"domain-name"`
	AccessPolicies                *string                                     `ub:"access-policies"`
	AdvancedOptions               *map[string]string                          `ub:"advanced-options"`
	AdvancedSecurityOptions       *DomainAdvancedSecurityOptions              `ub:"advanced-security-options,sensitive"`
	AIMLOptions                   *DomainAIMLOptions                          `ub:"aiml-options"`
	AutoTuneOptions               *DomainAutoTuneOptions                      `ub:"auto-tune-options"`
	AutomatedSnapshotPauseOptions *DomainAutomatedSnapshotPauseRequestOptions `ub:"automated-snapshot-pause-options"`
	ClusterConfig                 *DomainClusterConfig                        `ub:"cluster-config"`
	CognitoOptions                *DomainCognitoOptions                       `ub:"cognito-options"`
	DeploymentStrategyOptions     *DomainDeploymentStrategyOptions            `ub:"deployment-strategy-options"`
	DomainEndpointOptions         *DomainEndpointOptions                      `ub:"domain-endpoint-options"`
	EBSOptions                    *DomainEBSOptions                           `ub:"ebs-options"`
	EncryptAtRest                 *DomainEncryptionAtRestOptions              `ub:"encrypt-at-rest"`
	EngineVersion                 *string                                     `ub:"engine-version"`
	IdentityCenterOptions         *DomainIdentityCenterOptions                `ub:"identity-center-options"`
	IPAddressType                 *string                                     `ub:"ip-address-type"`
	LogPublishingOptions          *[]DomainLogPublishingOption                `ub:"log-publishing-options"`
	NodeToNodeEncryption          *DomainNodeToNodeEncryptionOptions          `ub:"node-to-node-encryption"`
	OffPeakWindowOptions          *DomainOffPeakWindowOptions                 `ub:"off-peak-window-options"`
	SnapshotOptions               *DomainSnapshotOptions                      `ub:"snapshot-options"`
	SoftwareUpdateOptions         *DomainSoftwareUpdateOptions                `ub:"software-update-options"`
	Tags                          *map[string]string                          `ub:"tags"`
	VPCOptions                    *DomainVPCOptions                           `ub:"vpc-options"`
}

type DomainAdvancedSecurityOptions struct {
	AnonymousAuthEnabled        *bool                       `ub:"anonymous-auth-enabled"`
	Enabled                     bool                        `ub:"enabled"`
	IAMFederationOptions        *DomainIAMFederationOptions `ub:"iam-federation-options"`
	InternalUserDatabaseEnabled bool                        `ub:"internal-user-database-enabled"`
	JWTOptions                  *DomainJWTOptions           `ub:"jwt-options"`
	MasterUserOptions           *DomainMasterUserOptions    `ub:"master-user-options"`
	SAMLOptions                 *DomainSAMLOptions          `ub:"saml-options"`
}

type DomainIAMFederationOptions struct {
	Enabled    *bool   `ub:"enabled"`
	RolesKey   *string `ub:"roles-key"`
	SubjectKey *string `ub:"subject-key"`
}

type DomainJWTOptions struct {
	Enabled    *bool   `ub:"enabled"`
	JWKSURL    *string `ub:"jwks-url"`
	PublicKey  *string `ub:"public-key"`
	RolesKey   *string `ub:"roles-key"`
	SubjectKey *string `ub:"subject-key"`
}

type DomainMasterUserOptions struct {
	MasterUserARN      *string `ub:"master-user-arn"`
	MasterUserName     *string `ub:"master-user-name"`
	MasterUserPassword *string `ub:"master-user-password,sensitive"`
}

type DomainSAMLOptions struct {
	Enabled               bool           `ub:"enabled"`
	IDP                   *DomainSAMLIDP `ub:"idp"`
	MasterBackendRole     *string        `ub:"master-backend-role"`
	MasterUserName        *string        `ub:"master-user-name,sensitive"`
	RolesKey              *string        `ub:"roles-key"`
	SessionTimeoutMinutes int64          `ub:"session-timeout-minutes"`
	SubjectKey            *string        `ub:"subject-key"`
}

type DomainSAMLIDP struct {
	EntityID        string `ub:"entity-id"`
	MetadataContent string `ub:"metadata-content"`
}

type DomainAIMLOptions struct {
	NaturalLanguageQueryGenerationOptions *DomainNaturalLanguageQueryGenerationOptions `ub:"natural-language-query-generation-options"`
	S3VectorsEngine                       *DomainFeatureOptions                        `ub:"s3-vectors-engine"`
	ServerlessVectorAcceleration          *DomainFeatureOptions                        `ub:"serverless-vector-acceleration"`
}

type DomainNaturalLanguageQueryGenerationOptions struct {
	DesiredState string `ub:"desired-state"`
}

type DomainFeatureOptions struct {
	Enabled *bool `ub:"enabled"`
}

type DomainAutoTuneOptions struct {
	DesiredState        string                               `ub:"desired-state"`
	MaintenanceSchedule *[]DomainAutoTuneMaintenanceSchedule `ub:"maintenance-schedule"`
	RollbackOnDisable   *string                              `ub:"rollback-on-disable"`
	UseOffPeakWindow    bool                                 `ub:"use-off-peak-window"`
}

type DomainAutoTuneMaintenanceSchedule struct {
	CronExpressionForRecurrence string                 `ub:"cron-expression-for-recurrence"`
	Duration                    DomainAutoTuneDuration `ub:"duration"`
	StartAt                     string                 `ub:"start-at"`
}

type DomainAutoTuneDuration struct {
	Unit  string `ub:"unit"`
	Value int64  `ub:"value"`
}

type DomainAutomatedSnapshotPauseRequestOptions struct {
	Enabled   bool    `ub:"enabled"`
	StartTime *string `ub:"start-time"`
	EndTime   *string `ub:"end-time"`
}

type DomainClusterConfig struct {
	ColdStorageOptions        *DomainColdStorageOptions  `ub:"cold-storage-options"`
	DedicatedMasterCount      *int64                     `ub:"dedicated-master-count"`
	DedicatedMasterEnabled    bool                       `ub:"dedicated-master-enabled"`
	DedicatedMasterType       *string                    `ub:"dedicated-master-type"`
	InstanceCount             int64                      `ub:"instance-count"`
	InstanceType              string                     `ub:"instance-type"`
	MultiAZWithStandbyEnabled *bool                      `ub:"multi-az-with-standby-enabled"`
	NodeOptions               *[]DomainNodeOption        `ub:"node-options"`
	WarmCount                 int64                      `ub:"warm-count"`
	WarmEnabled               *bool                      `ub:"warm-enabled"`
	WarmType                  *string                    `ub:"warm-type"`
	ZoneAwarenessConfig       *DomainZoneAwarenessConfig `ub:"zone-awareness-config"`
	ZoneAwarenessEnabled      *bool                      `ub:"zone-awareness-enabled"`
}

type DomainColdStorageOptions struct {
	Enabled *bool `ub:"enabled"`
}

type DomainNodeOption struct {
	NodeConfig *DomainNodeConfig `ub:"node-config"`
	NodeType   string            `ub:"node-type"`
}

type DomainNodeConfig struct {
	Count   *int64  `ub:"count"`
	Enabled *bool   `ub:"enabled"`
	Type    *string `ub:"type"`
}

type DomainZoneAwarenessConfig struct {
	AvailabilityZoneCount int64 `ub:"availability-zone-count"`
}

type DomainCognitoOptions struct {
	Enabled        bool   `ub:"enabled"`
	IdentityPoolID string `ub:"identity-pool-id"`
	RoleARN        string `ub:"role-arn"`
	UserPoolID     string `ub:"user-pool-id"`
}

type DomainDeploymentStrategyOptions struct {
	DeploymentStrategy string `ub:"deployment-strategy"`
}

type DomainEndpointOptions struct {
	CustomEndpoint               *string `ub:"custom-endpoint"`
	CustomEndpointCertificateARN *string `ub:"custom-endpoint-certificate-arn"`
	CustomEndpointEnabled        bool    `ub:"custom-endpoint-enabled"`
	EnforceHTTPS                 bool    `ub:"enforce-https"`
	TLSSecurityPolicy            *string `ub:"tls-security-policy"`
}

type DomainEBSOptions struct {
	EBSEnabled bool    `ub:"ebs-enabled"`
	IOPS       *int64  `ub:"iops"`
	Throughput *int64  `ub:"throughput"`
	VolumeSize *int64  `ub:"volume-size"`
	VolumeType *string `ub:"volume-type"`
}

type DomainEncryptionAtRestOptions struct {
	Enabled  bool    `ub:"enabled"`
	KMSKeyID *string `ub:"kms-key-id"`
}

type DomainIdentityCenterOptions struct {
	EnabledAPIAccess          *bool   `ub:"enabled-api-access"`
	IdentityCenterInstanceARN *string `ub:"identity-center-instance-arn"`
	RolesKey                  *string `ub:"roles-key"`
	SubjectKey                *string `ub:"subject-key"`
}

type DomainLogPublishingOption struct {
	CloudWatchLogGroupARN string `ub:"cloudwatch-log-group-arn"`
	Enabled               *bool  `ub:"enabled"`
	LogType               string `ub:"log-type"`
}

type DomainNodeToNodeEncryptionOptions struct {
	Enabled bool `ub:"enabled"`
}

type DomainOffPeakWindowOptions struct {
	Enabled       *bool                `ub:"enabled"`
	OffPeakWindow *DomainOffPeakWindow `ub:"off-peak-window"`
}

type DomainOffPeakWindow struct {
	WindowStartTime *DomainWindowStartTime `ub:"window-start-time"`
}

type DomainWindowStartTime struct {
	Hours   int64 `ub:"hours"`
	Minutes int64 `ub:"minutes"`
}

type DomainSnapshotOptions struct {
	AutomatedSnapshotStartHour int64 `ub:"automated-snapshot-start-hour"`
}

type DomainSoftwareUpdateOptions struct {
	AutoSoftwareUpdateEnabled bool `ub:"auto-software-update-enabled"`
}

type DomainVPCOptions struct {
	EgressEnabled    *bool     `ub:"egress-enabled"`
	SecurityGroupIDs *[]string `ub:"security-group-ids"`
	SubnetIDs        *[]string `ub:"subnet-ids"`
}

type DomainResourceOutput struct {
	DomainName                    string                               `ub:"domain-name"`
	DomainID                      string                               `ub:"domain-id"`
	ARN                           string                               `ub:"arn"`
	EngineVersion                 string                               `ub:"engine-version"`
	IPAddressType                 string                               `ub:"ip-address-type"`
	Endpoint                      *string                              `ub:"endpoint"`
	EndpointV2                    *string                              `ub:"endpoint-v2"`
	Endpoints                     *map[string]string                   `ub:"endpoints"`
	DashboardEndpoint             *string                              `ub:"dashboard-endpoint"`
	DashboardEndpointV2           *string                              `ub:"dashboard-endpoint-v2"`
	DomainEndpointV2HostedZoneID  *string                              `ub:"domain-endpoint-v2-hosted-zone-id"`
	Created                       bool                                 `ub:"created"`
	Deleted                       bool                                 `ub:"deleted"`
	Processing                    bool                                 `ub:"processing"`
	UpgradeProcessing             bool                                 `ub:"upgrade-processing"`
	VPCOptions                    *DomainVPCDerivedInfo                `ub:"vpc-options"`
	ServiceSoftwareOptions        *DomainServiceSoftwareOptions        `ub:"service-software-options"`
	AnonymousAuthDisableDate      *string                              `ub:"anonymous-auth-disable-date"`
	IdentityCenterApplicationARN  *string                              `ub:"identity-center-application-arn"`
	IdentityStoreID               *string                              `ub:"identity-store-id"`
	AutomatedSnapshotPauseOptions *DomainAutomatedSnapshotPauseOptions `ub:"automated-snapshot-pause-options"`
}

type DomainVPCDerivedInfo struct {
	AvailabilityZones []string `ub:"availability-zones"`
	EgressEnabled     *bool    `ub:"egress-enabled"`
	SecurityGroupIDs  []string `ub:"security-group-ids"`
	SubnetIDs         []string `ub:"subnet-ids"`
	VPCID             string   `ub:"vpc-id"`
}

type DomainServiceSoftwareOptions struct {
	AutomatedUpdateDate *string `ub:"automated-update-date"`
	Cancellable         bool    `ub:"cancellable"`
	CurrentVersion      *string `ub:"current-version"`
	Description         *string `ub:"description"`
	NewVersion          *string `ub:"new-version"`
	OptionalDeployment  bool    `ub:"optional-deployment"`
	UpdateAvailable     bool    `ub:"update-available"`
	UpdateStatus        string  `ub:"update-status"`
}

type DomainAutomatedSnapshotPauseOptions struct {
	Enabled   bool    `ub:"enabled"`
	State     string  `ub:"state"`
	StartTime *string `ub:"start-time"`
	EndTime   *string `ub:"end-time"`
}
