package lambdamicrovms

type CodeArtifact struct {
	Uri string `ub:"uri"`
}

type Logging struct {
	CloudWatch *CloudWatchLogging `ub:"cloud-watch"`
	Disabled   *bool              `ub:"disabled"`
}

type CloudWatchLogging struct {
	LogGroup  *string `ub:"log-group"`
	LogStream *string `ub:"log-stream"`
}

type CpuConfiguration struct {
	Architecture string `ub:"architecture"`
}

type Resources struct {
	MinimumMemoryInMiB int64 `ub:"minimum-memory-in-mib"`
}

type Hooks struct {
	Port              *int64             `ub:"port"`
	MicrovmHooks      *MicrovmHooks      `ub:"microvm-hooks"`
	MicrovmImageHooks *MicrovmImageHooks `ub:"microvm-image-hooks"`
}

type MicrovmHooks struct {
	Run                       *string `ub:"run"`
	RunTimeoutInSeconds       *int64  `ub:"run-timeout-in-seconds"`
	Resume                    *string `ub:"resume"`
	ResumeTimeoutInSeconds    *int64  `ub:"resume-timeout-in-seconds"`
	Suspend                   *string `ub:"suspend"`
	SuspendTimeoutInSeconds   *int64  `ub:"suspend-timeout-in-seconds"`
	Terminate                 *string `ub:"terminate"`
	TerminateTimeoutInSeconds *int64  `ub:"terminate-timeout-in-seconds"`
}

type MicrovmImageHooks struct {
	Ready                    *string `ub:"ready"`
	ReadyTimeoutInSeconds    *int64  `ub:"ready-timeout-in-seconds"`
	Validate                 *string `ub:"validate"`
	ValidateTimeoutInSeconds *int64  `ub:"validate-timeout-in-seconds"`
}

type IdlePolicy struct {
	AutoResumeEnabled        bool  `ub:"auto-resume-enabled"`
	MaxIdleDurationSeconds   int64 `ub:"max-idle-duration-seconds"`
	SuspendedDurationSeconds int64 `ub:"suspended-duration-seconds"`
}

type PortSpecification struct {
	AllPorts *bool      `ub:"all-ports"`
	Port     *int64     `ub:"port"`
	Range    *PortRange `ub:"range"`
}

type PortRange struct {
	StartPort int64 `ub:"start-port"`
	EndPort   int64 `ub:"end-port"`
}

type SnapshotBuild struct {
	CodeInstallSizeInBytes    int64 `ub:"code-install-size-in-bytes"`
	DiskSnapshotSizeInBytes   int64 `ub:"disk-snapshot-size-in-bytes"`
	MemorySnapshotSizeInBytes int64 `ub:"memory-snapshot-size-in-bytes"`
}

type MicrovmImageResourceOutput struct {
	ImageArn                 string `ub:"image-arn"`
	Name                     string `ub:"name"`
	State                    string `ub:"state"`
	CreatedAt                string `ub:"created-at"`
	UpdatedAt                string `ub:"updated-at"`
	LatestActiveImageVersion string `ub:"latest-active-image-version"`
	LatestFailedImageVersion string `ub:"latest-failed-image-version"`
}

type MicrovmImageDataSourceOutput struct {
	ImageArn                 string            `ub:"image-arn"`
	Name                     string            `ub:"name"`
	State                    string            `ub:"state"`
	CreatedAt                string            `ub:"created-at"`
	UpdatedAt                string            `ub:"updated-at"`
	LatestActiveImageVersion string            `ub:"latest-active-image-version"`
	LatestFailedImageVersion string            `ub:"latest-failed-image-version"`
	Tags                     map[string]string `ub:"tags"`
}

type MicrovmImageSummary struct {
	ImageArn                 string `ub:"image-arn"`
	Name                     string `ub:"name"`
	State                    string `ub:"state"`
	CreatedAt                string `ub:"created-at"`
	LatestActiveImageVersion string `ub:"latest-active-image-version"`
	LatestFailedImageVersion string `ub:"latest-failed-image-version"`
}

type MicrovmImagesDataSourceOutput struct {
	Items []MicrovmImageSummary `ub:"items"`
}

type MicrovmImageVersionDataSourceOutput struct {
	ImageArn                 string             `ub:"image-arn"`
	ImageVersion             string             `ub:"image-version"`
	State                    string             `ub:"state"`
	Status                   string             `ub:"status"`
	BaseImageArn             string             `ub:"base-image-arn"`
	BaseImageVersion         string             `ub:"base-image-version"`
	BuildRoleArn             string             `ub:"build-role-arn"`
	CodeArtifact             CodeArtifact       `ub:"code-artifact"`
	AdditionalOsCapabilities []string           `ub:"additional-os-capabilities"`
	CpuConfigurations        []CpuConfiguration `ub:"cpu-configurations"`
	Description              string             `ub:"description"`
	EgressNetworkConnectors  []string           `ub:"egress-network-connectors"`
	EnvironmentVariables     map[string]string  `ub:"environment-variables"`
	Hooks                    *Hooks             `ub:"hooks"`
	Logging                  *Logging           `ub:"logging"`
	Resources                []Resources        `ub:"resources"`
	StateReason              string             `ub:"state-reason"`
	Tags                     map[string]string  `ub:"tags"`
	CreatedAt                string             `ub:"created-at"`
	UpdatedAt                string             `ub:"updated-at"`
}

type UpdateMicrovmImageVersionStatusActionOutput struct {
	ImageArn                 string             `ub:"image-arn"`
	ImageVersion             string             `ub:"image-version"`
	State                    string             `ub:"state"`
	Status                   string             `ub:"status"`
	BaseImageArn             string             `ub:"base-image-arn"`
	BaseImageVersion         string             `ub:"base-image-version"`
	BuildRoleArn             string             `ub:"build-role-arn"`
	CodeArtifact             CodeArtifact       `ub:"code-artifact"`
	AdditionalOsCapabilities []string           `ub:"additional-os-capabilities"`
	CpuConfigurations        []CpuConfiguration `ub:"cpu-configurations"`
	Description              string             `ub:"description"`
	EgressNetworkConnectors  []string           `ub:"egress-network-connectors"`
	EnvironmentVariables     map[string]string  `ub:"environment-variables"`
	Hooks                    *Hooks             `ub:"hooks"`
	Logging                  *Logging           `ub:"logging"`
	Resources                []Resources        `ub:"resources"`
	StateReason              string             `ub:"state-reason"`
	Tags                     map[string]string  `ub:"tags"`
	CreatedAt                string             `ub:"created-at"`
	UpdatedAt                string             `ub:"updated-at"`
}

type MicrovmImageVersionSummary struct {
	ImageArn                 string             `ub:"image-arn"`
	ImageVersion             string             `ub:"image-version"`
	State                    string             `ub:"state"`
	Status                   string             `ub:"status"`
	BaseImageArn             string             `ub:"base-image-arn"`
	BaseImageVersion         string             `ub:"base-image-version"`
	BuildRoleArn             string             `ub:"build-role-arn"`
	CodeArtifact             CodeArtifact       `ub:"code-artifact"`
	AdditionalOsCapabilities []string           `ub:"additional-os-capabilities"`
	CpuConfigurations        []CpuConfiguration `ub:"cpu-configurations"`
	Description              string             `ub:"description"`
	EgressNetworkConnectors  []string           `ub:"egress-network-connectors"`
	EnvironmentVariables     map[string]string  `ub:"environment-variables"`
	Hooks                    *Hooks             `ub:"hooks"`
	Logging                  *Logging           `ub:"logging"`
	Resources                []Resources        `ub:"resources"`
	StateReason              string             `ub:"state-reason"`
	Tags                     map[string]string  `ub:"tags"`
	CreatedAt                string             `ub:"created-at"`
	UpdatedAt                string             `ub:"updated-at"`
}

type MicrovmImageVersionsDataSourceOutput struct {
	Items []MicrovmImageVersionSummary `ub:"items"`
}

type MicrovmImageBuildDataSourceOutput struct {
	ImageArn          string         `ub:"image-arn"`
	ImageVersion      string         `ub:"image-version"`
	BuildId           string         `ub:"build-id"`
	BuildState        string         `ub:"build-state"`
	Architecture      string         `ub:"architecture"`
	Chipset           string         `ub:"chipset"`
	ChipsetGeneration string         `ub:"chipset-generation"`
	SnapshotBuild     *SnapshotBuild `ub:"snapshot-build"`
	StateReason       string         `ub:"state-reason"`
	CreatedAt         string         `ub:"created-at"`
}

type MicrovmImageBuildSummary struct {
	ImageArn          string         `ub:"image-arn"`
	ImageVersion      string         `ub:"image-version"`
	BuildId           string         `ub:"build-id"`
	BuildState        string         `ub:"build-state"`
	Architecture      string         `ub:"architecture"`
	Chipset           string         `ub:"chipset"`
	ChipsetGeneration string         `ub:"chipset-generation"`
	SnapshotBuild     *SnapshotBuild `ub:"snapshot-build"`
	StateReason       string         `ub:"state-reason"`
	CreatedAt         string         `ub:"created-at"`
}

type MicrovmImageBuildsDataSourceOutput struct {
	Items []MicrovmImageBuildSummary `ub:"items"`
}

type ManagedMicrovmImageSummary struct {
	ImageArn  string `ub:"image-arn"`
	CreatedAt string `ub:"created-at"`
	UpdatedAt string `ub:"updated-at"`
}

type ManagedMicrovmImagesDataSourceOutput struct {
	Items []ManagedMicrovmImageSummary `ub:"items"`
}

type ManagedMicrovmImageVersion struct {
	ImageArn     string `ub:"image-arn"`
	ImageVersion string `ub:"image-version"`
	CreatedAt    string `ub:"created-at"`
	UpdatedAt    string `ub:"updated-at"`
}

type ManagedMicrovmImageVersionsDataSourceOutput struct {
	Items []ManagedMicrovmImageVersion `ub:"items"`
}

type MicrovmDataSourceOutput struct {
	MicrovmId                string      `ub:"microvm-id"`
	Endpoint                 string      `ub:"endpoint"`
	ImageArn                 string      `ub:"image-arn"`
	ImageVersion             string      `ub:"image-version"`
	State                    string      `ub:"state"`
	StartedAt                string      `ub:"started-at"`
	TerminatedAt             string      `ub:"terminated-at"`
	MaximumDurationInSeconds int64       `ub:"maximum-duration-in-seconds"`
	ExecutionRoleArn         string      `ub:"execution-role-arn"`
	IngressNetworkConnectors []string    `ub:"ingress-network-connectors"`
	EgressNetworkConnectors  []string    `ub:"egress-network-connectors"`
	IdlePolicy               *IdlePolicy `ub:"idle-policy"`
	StateReason              string      `ub:"state-reason"`
}

type RunMicrovmActionOutput struct {
	MicrovmId                string      `ub:"microvm-id"`
	Endpoint                 string      `ub:"endpoint"`
	ImageArn                 string      `ub:"image-arn"`
	ImageVersion             string      `ub:"image-version"`
	State                    string      `ub:"state"`
	StartedAt                string      `ub:"started-at"`
	TerminatedAt             string      `ub:"terminated-at"`
	MaximumDurationInSeconds int64       `ub:"maximum-duration-in-seconds"`
	ExecutionRoleArn         string      `ub:"execution-role-arn"`
	IngressNetworkConnectors []string    `ub:"ingress-network-connectors"`
	EgressNetworkConnectors  []string    `ub:"egress-network-connectors"`
	IdlePolicy               *IdlePolicy `ub:"idle-policy"`
	StateReason              string      `ub:"state-reason"`
}

type MicrovmSummary struct {
	MicrovmId    string `ub:"microvm-id"`
	ImageArn     string `ub:"image-arn"`
	ImageVersion string `ub:"image-version"`
	State        string `ub:"state"`
	StartedAt    string `ub:"started-at"`
}

type MicrovmsDataSourceOutput struct {
	Items []MicrovmSummary `ub:"items"`
}

type MicrovmAuthTokenActionOutput struct {
	AuthToken map[string]string `ub:"auth-token,sensitive"`
}

type MicrovmShellAuthTokenActionOutput struct {
	AuthToken map[string]string `ub:"auth-token,sensitive"`
}

type SuspendMicrovmActionOutput struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

type ResumeMicrovmActionOutput struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}

type TerminateMicrovmActionOutput struct {
	MicrovmIdentifier string `ub:"microvm-identifier"`
}
