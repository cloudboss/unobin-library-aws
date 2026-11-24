package codebuild

type ProjectArtifacts struct {
	Type                 string  `ub:"type"`
	ArtifactIdentifier   *string `ub:"artifact-identifier"`
	BucketOwnerAccess    *string `ub:"bucket-owner-access"`
	EncryptionDisabled   *bool   `ub:"encryption-disabled"`
	Location             *string `ub:"location"`
	Name                 *string `ub:"name"`
	NamespaceType        *string `ub:"namespace-type"`
	OverrideArtifactName *bool   `ub:"override-artifact-name"`
	Packaging            *string `ub:"packaging"`
	Path                 *string `ub:"path"`
}

type SecondaryProjectArtifacts struct {
	Type                 string  `ub:"type"`
	ArtifactIdentifier   string  `ub:"artifact-identifier"`
	BucketOwnerAccess    *string `ub:"bucket-owner-access"`
	EncryptionDisabled   *bool   `ub:"encryption-disabled"`
	Location             *string `ub:"location"`
	Name                 *string `ub:"name"`
	NamespaceType        *string `ub:"namespace-type"`
	OverrideArtifactName *bool   `ub:"override-artifact-name"`
	Packaging            *string `ub:"packaging"`
	Path                 *string `ub:"path"`
}

type ProjectBuildBatchConfig struct {
	ServiceRole      string                    `ub:"service-role"`
	CombineArtifacts *bool                     `ub:"combine-artifacts"`
	Restrictions     *ProjectBatchRestrictions `ub:"restrictions"`
	TimeoutInMins    *int64                    `ub:"timeout-in-mins"`
}
type ProjectBatchRestrictions struct {
	ComputeTypesAllowed  *[]string `ub:"compute-types-allowed"`
	MaximumBuildsAllowed *int64    `ub:"maximum-builds-allowed"`
}
type ProjectCache struct {
	Type           *string   `ub:"type"`
	Location       *string   `ub:"location"`
	Modes          *[]string `ub:"modes"`
	CacheNamespace *string   `ub:"cache-namespace"`
}
type ProjectEnvironment struct {
	ComputeType              string                        `ub:"compute-type"`
	Image                    string                        `ub:"image"`
	Type                     string                        `ub:"type"`
	Certificate              *string                       `ub:"certificate"`
	DockerServer             *ProjectDockerServer          `ub:"docker-server"`
	Fleet                    *ProjectFleet                 `ub:"fleet"`
	EnvironmentVariables     *[]ProjectEnvironmentVariable `ub:"environment-variables"`
	ImagePullCredentialsType *string                       `ub:"image-pull-credentials-type"`
	PrivilegedMode           *bool                         `ub:"privileged-mode"`
	RegistryCredential       *ProjectRegistryCredential    `ub:"registry-credential"`
}
type ProjectDockerServer struct {
	ComputeType      string    `ub:"compute-type"`
	SecurityGroupIds *[]string `ub:"security-group-ids"`
}
type ProjectFleet struct {
	FleetArn *string `ub:"fleet-arn"`
}
type ProjectEnvironmentVariable struct {
	Name  string  `ub:"name"`
	Value string  `ub:"value"`
	Type  *string `ub:"type"`
}
type ProjectRegistryCredential struct {
	Credential         string `ub:"credential"`
	CredentialProvider string `ub:"credential-provider"`
}
type ProjectFileSystemLocation struct {
	Identifier   *string `ub:"identifier"`
	Location     *string `ub:"location"`
	MountOptions *string `ub:"mount-options"`
	MountPoint   *string `ub:"mount-point"`
	Type         *string `ub:"type"`
}
type ProjectLogsConfig struct {
	CloudWatchLogs *ProjectCloudWatchLogs `ub:"cloud-watch-logs"`
	S3Logs         *ProjectS3Logs         `ub:"s3-logs"`
}
type ProjectCloudWatchLogs struct {
	GroupName  *string `ub:"group-name"`
	StreamName *string `ub:"stream-name"`
	Status     *string `ub:"status"`
}
type ProjectS3Logs struct {
	BucketOwnerAccess  *string `ub:"bucket-owner-access"`
	EncryptionDisabled *bool   `ub:"encryption-disabled"`
	Location           *string `ub:"location"`
	Status             *string `ub:"status"`
}
type ProjectSource struct {
	Type                string                      `ub:"type"`
	Auth                *ProjectSourceAuth          `ub:"auth"`
	BuildStatusConfig   *ProjectBuildStatusConfig   `ub:"build-status-config"`
	Buildspec           *string                     `ub:"buildspec"`
	GitCloneDepth       *int64                      `ub:"git-clone-depth"`
	GitSubmodulesConfig *ProjectGitSubmodulesConfig `ub:"git-submodules-config"`
	InsecureSsl         *bool                       `ub:"insecure-ssl"`
	Location            *string                     `ub:"location"`
	ReportBuildStatus   *bool                       `ub:"report-build-status"`
}
type SecondaryProjectSource struct {
	Type                string                      `ub:"type"`
	SourceIdentifier    string                      `ub:"source-identifier"`
	Auth                *ProjectSourceAuth          `ub:"auth"`
	BuildStatusConfig   *ProjectBuildStatusConfig   `ub:"build-status-config"`
	Buildspec           *string                     `ub:"buildspec"`
	GitCloneDepth       *int64                      `ub:"git-clone-depth"`
	GitSubmodulesConfig *ProjectGitSubmodulesConfig `ub:"git-submodules-config"`
	InsecureSsl         *bool                       `ub:"insecure-ssl"`
	Location            *string                     `ub:"location"`
	ReportBuildStatus   *bool                       `ub:"report-build-status"`
}
type ProjectSourceAuth struct {
	Type     string `ub:"type"`
	Resource string `ub:"resource"`
}
type ProjectBuildStatusConfig struct {
	Context   *string `ub:"context"`
	TargetUrl *string `ub:"target-url"`
}
type ProjectGitSubmodulesConfig struct {
	FetchSubmodules bool `ub:"fetch-submodules"`
}
type ProjectSourceVersion struct {
	SourceIdentifier string `ub:"source-identifier"`
	SourceVersion    string `ub:"source-version"`
}
type ProjectVpcConfig struct {
	VpcId            string   `ub:"vpc-id"`
	Subnets          []string `ub:"subnets"`
	SecurityGroupIds []string `ub:"security-group-ids"`
}
type ProjectBadge struct {
	BadgeEnabled    bool   `ub:"badge-enabled"`
	BadgeRequestUrl string `ub:"badge-request-url"`
}
type ProjectResourceOutput struct {
	Arn                     string                       `ub:"arn"`
	Name                    string                       `ub:"name"`
	Artifacts               *ProjectArtifacts            `ub:"artifacts"`
	Environment             ProjectEnvironment           `ub:"environment"`
	ServiceRole             string                       `ub:"service-role"`
	Source                  *ProjectSource               `ub:"source"`
	AutoRetryLimit          *int64                       `ub:"auto-retry-limit"`
	Badge                   ProjectBadge                 `ub:"badge"`
	BuildBatchConfig        *ProjectBuildBatchConfig     `ub:"build-batch-config"`
	Cache                   *ProjectCache                `ub:"cache"`
	ConcurrentBuildLimit    *int64                       `ub:"concurrent-build-limit"`
	Description             *string                      `ub:"description"`
	EncryptionKey           *string                      `ub:"encryption-key"`
	FileSystemLocations     *[]ProjectFileSystemLocation `ub:"file-system-locations"`
	LogsConfig              *ProjectLogsConfig           `ub:"logs-config"`
	ProjectVisibility       string                       `ub:"project-visibility"`
	PublicProjectAlias      string                       `ub:"public-project-alias"`
	QueuedTimeoutInMinutes  int64                        `ub:"queued-timeout-in-minutes"`
	ResourceAccessRole      *string                      `ub:"resource-access-role"`
	SecondaryArtifacts      *[]SecondaryProjectArtifacts `ub:"secondary-artifacts"`
	SecondarySources        *[]SecondaryProjectSource    `ub:"secondary-sources"`
	SecondarySourceVersions *[]ProjectSourceVersion      `ub:"secondary-source-versions"`
	SourceVersion           *string                      `ub:"source-version"`
	Tags                    map[string]string            `ub:"tags"`
	TimeoutInMinutes        int64                        `ub:"timeout-in-minutes"`
	VpcConfig               *ProjectVpcConfig            `ub:"vpc-config"`
}
