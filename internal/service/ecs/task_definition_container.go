package ecs

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// TaskDefinitionContainerDefinition is one container of the task. It mirrors
// the SDK ContainerDefinition type. Name and image are required; everything
// else is optional and defaulted by the server, notably essential, which
// defaults to true. Cpu is in CPU units (1024 per vCPU), and memory and
// memory-reservation are in MiB. The enums inside the optional lists, such
// as a depends-on condition (START, COMPLETE, SUCCESS, HEALTHY), an
// environment-file type (s3), a port mapping protocol (tcp, udp) and
// app-protocol (http, http2, grpc), a resource requirement type (GPU,
// InferenceAccelerator), and a ulimit name, are enforced by the API.
type TaskDefinitionContainerDefinition struct {
	Name  string `ub:"name"`
	Image string `ub:"image"`
	// Environment is the container's environment variables, sent to the API
	// as its key-value pair list ordered by name.
	Environment            *map[string]string                            `ub:"environment"`
	Command                *[]string                                     `ub:"command"`
	Cpu                    *int64                                        `ub:"cpu"`
	CredentialSpecs        *[]string                                     `ub:"credential-specs"`
	DependsOn              *[]TaskDefinitionContainerDependency          `ub:"depends-on"`
	DisableNetworking      *bool                                         `ub:"disable-networking"`
	DnsSearchDomains       *[]string                                     `ub:"dns-search-domains"`
	DnsServers             *[]string                                     `ub:"dns-servers"`
	DockerLabels           *map[string]string                            `ub:"docker-labels"`
	DockerSecurityOptions  *[]string                                     `ub:"docker-security-options"`
	EntryPoint             *[]string                                     `ub:"entry-point"`
	EnvironmentFiles       *[]TaskDefinitionContainerEnvironmentFile     `ub:"environment-files"`
	Essential              *bool                                         `ub:"essential"`
	ExtraHosts             *[]TaskDefinitionContainerHostEntry           `ub:"extra-hosts"`
	FirelensConfiguration  *TaskDefinitionContainerFirelens              `ub:"firelens-configuration"`
	HealthCheck            *TaskDefinitionContainerHealthCheck           `ub:"health-check"`
	Hostname               *string                                       `ub:"hostname"`
	Interactive            *bool                                         `ub:"interactive"`
	Links                  *[]string                                     `ub:"links"`
	LinuxParameters        *TaskDefinitionContainerLinuxParameters       `ub:"linux-parameters"`
	LogConfiguration       *TaskDefinitionContainerLogConfiguration      `ub:"log-configuration"`
	Memory                 *int64                                        `ub:"memory"`
	MemoryReservation      *int64                                        `ub:"memory-reservation"`
	MountPoints            *[]TaskDefinitionContainerMountPoint          `ub:"mount-points"`
	PortMappings           *[]TaskDefinitionContainerPortMapping         `ub:"port-mappings"`
	Privileged             *bool                                         `ub:"privileged"`
	PseudoTerminal         *bool                                         `ub:"pseudo-terminal"`
	ReadonlyRootFilesystem *bool                                         `ub:"readonly-root-filesystem"`
	RepositoryCredentials  *TaskDefinitionContainerRepositoryCredentials `ub:"repository-credentials"`
	ResourceRequirements   *[]TaskDefinitionContainerResourceRequirement `ub:"resource-requirements"`
	RestartPolicy          *TaskDefinitionContainerRestartPolicy         `ub:"restart-policy"`
	Secrets                *[]TaskDefinitionContainerSecret              `ub:"secrets"`
	StartTimeout           *int64                                        `ub:"start-timeout"`
	StopTimeout            *int64                                        `ub:"stop-timeout"`
	SystemControls         *[]TaskDefinitionContainerSystemControl       `ub:"system-controls"`
	Ulimits                *[]TaskDefinitionContainerUlimit              `ub:"ulimits"`
	User                   *string                                       `ub:"user"`
	VersionConsistency     *string                                       `ub:"version-consistency"`
	VolumesFrom            *[]TaskDefinitionContainerVolumeFrom          `ub:"volumes-from"`
	WorkingDirectory       *string                                       `ub:"working-directory"`
}

// TaskDefinitionContainerDependency makes the container's startup or
// shutdown wait on another container of the task reaching a condition:
// START, COMPLETE, SUCCESS, or HEALTHY. It mirrors the SDK
// ContainerDependency type.
type TaskDefinitionContainerDependency struct {
	Condition     string `ub:"condition"`
	ContainerName string `ub:"container-name"`
}

// TaskDefinitionContainerDevice exposes a host device to the container. It
// mirrors the SDK Device type; permissions entries are read, write, or
// mknod, defaulting to all three.
type TaskDefinitionContainerDevice struct {
	HostPath      string    `ub:"host-path"`
	ContainerPath *string   `ub:"container-path"`
	Permissions   *[]string `ub:"permissions"`
}

// TaskDefinitionContainerEnvironmentFile names a file of environment
// variables to supply to the container. It mirrors the SDK EnvironmentFile
// type: the only type is s3, and the value is the ARN of an S3 object.
type TaskDefinitionContainerEnvironmentFile struct {
	Type  string `ub:"type"`
	Value string `ub:"value"`
}

// TaskDefinitionContainerFirelens routes the container's logs through a
// FireLens log router. It mirrors the SDK FirelensConfiguration type; the
// type is fluentd or fluentbit.
type TaskDefinitionContainerFirelens struct {
	Type    string             `ub:"type"`
	Options *map[string]string `ub:"options"`
}

// TaskDefinitionContainerHealthCheck is the command the container agent runs
// to decide container health, with its schedule. It mirrors the SDK
// HealthCheck type: command is required and starts with CMD or CMD-SHELL;
// interval, timeout, and start-period are in seconds.
type TaskDefinitionContainerHealthCheck struct {
	Command     []string `ub:"command"`
	Interval    *int64   `ub:"interval"`
	Retries     *int64   `ub:"retries"`
	StartPeriod *int64   `ub:"start-period"`
	Timeout     *int64   `ub:"timeout"`
}

// TaskDefinitionContainerHostEntry is one /etc/hosts entry to add to the
// container. It mirrors the SDK HostEntry type.
type TaskDefinitionContainerHostEntry struct {
	Hostname  string `ub:"hostname"`
	IpAddress string `ub:"ip-address"`
}

// TaskDefinitionContainerKernelCapabilities adjusts the Linux capabilities
// the container starts with: one list grants capabilities beyond the default
// configuration and the other removes them from it. It mirrors the SDK
// KernelCapabilities type; entries are capability names such as SYS_PTRACE.
type TaskDefinitionContainerKernelCapabilities struct {
	Add  *[]string `ub:"add"`
	Drop *[]string `ub:"drop"`
}

// TaskDefinitionContainerLinuxParameters is the container's Linux-specific
// options: capabilities, devices, an init process, swap limits, shared
// memory, and tmpfs mounts. It mirrors the SDK LinuxParameters type.
type TaskDefinitionContainerLinuxParameters struct {
	Capabilities       *TaskDefinitionContainerKernelCapabilities `ub:"capabilities"`
	Devices            *[]TaskDefinitionContainerDevice           `ub:"devices"`
	InitProcessEnabled *bool                                      `ub:"init-process-enabled"`
	MaxSwap            *int64                                     `ub:"max-swap"`
	SharedMemorySize   *int64                                     `ub:"shared-memory-size"`
	Swappiness         *int64                                     `ub:"swappiness"`
	Tmpfs              *[]TaskDefinitionContainerTmpfs            `ub:"tmpfs"`
}

// TaskDefinitionContainerLogConfiguration is the container's log driver and
// its options. It mirrors the SDK LogConfiguration type: log-driver is
// required (awslogs, awsfirelens, splunk, json-file, syslog, journald, gelf,
// or fluentd), and secret-options pulls option values from Secrets Manager
// or SSM by ARN.
type TaskDefinitionContainerLogConfiguration struct {
	LogDriver     string                           `ub:"log-driver"`
	Options       *map[string]string               `ub:"options"`
	SecretOptions *[]TaskDefinitionContainerSecret `ub:"secret-options"`
}

// TaskDefinitionContainerMountPoint mounts one of the task's volumes into
// the container. It mirrors the SDK MountPoint type.
type TaskDefinitionContainerMountPoint struct {
	ContainerPath *string `ub:"container-path"`
	ReadOnly      *bool   `ub:"read-only"`
	SourceVolume  *string `ub:"source-volume"`
}

// TaskDefinitionContainerPortMapping exposes a container port, or a range of
// them, on the host or the task's elastic network interface. It mirrors the
// SDK PortMapping type: protocol is tcp or udp, and app-protocol is http,
// http2, or grpc.
type TaskDefinitionContainerPortMapping struct {
	AppProtocol        *string `ub:"app-protocol"`
	ContainerPort      *int64  `ub:"container-port"`
	ContainerPortRange *string `ub:"container-port-range"`
	HostPort           *int64  `ub:"host-port"`
	Name               *string `ub:"name"`
	Protocol           *string `ub:"protocol"`
}

// TaskDefinitionContainerRepositoryCredentials names the Secrets Manager
// secret holding the private registry credentials for the container image.
// It mirrors the SDK RepositoryCredentials type.
type TaskDefinitionContainerRepositoryCredentials struct {
	CredentialsParameter string `ub:"credentials-parameter"`
}

// TaskDefinitionContainerResourceRequirement reserves a GPU or Elastic
// Inference accelerator for the container. It mirrors the SDK
// ResourceRequirement type: type is GPU or InferenceAccelerator.
type TaskDefinitionContainerResourceRequirement struct {
	Type  string `ub:"type"`
	Value string `ub:"value"`
}

// TaskDefinitionContainerRestartPolicy restarts the container individually
// when it exits, without replacing the task. It mirrors the SDK
// ContainerRestartPolicy type; the restart attempt period is in seconds,
// between 60 and 1800.
type TaskDefinitionContainerRestartPolicy struct {
	Enabled              bool     `ub:"enabled"`
	IgnoredExitCodes     *[]int64 `ub:"ignored-exit-codes"`
	RestartAttemptPeriod *int64   `ub:"restart-attempt-period"`
}

// TaskDefinitionContainerSecret injects a secret into the container, as an
// environment variable or a log option. It mirrors the SDK Secret type:
// value-from is the ARN of a Secrets Manager secret or an SSM parameter, not
// the secret value itself.
type TaskDefinitionContainerSecret struct {
	Name      string `ub:"name"`
	ValueFrom string `ub:"value-from"`
}

// TaskDefinitionContainerSystemControl sets one kernel parameter in the
// container, such as net.ipv4.tcp_keepalive_time. It mirrors the SDK
// SystemControl type.
type TaskDefinitionContainerSystemControl struct {
	Namespace *string `ub:"namespace"`
	Value     *string `ub:"value"`
}

// TaskDefinitionContainerTmpfs mounts a tmpfs filesystem in the container.
// It mirrors the SDK Tmpfs type; size is in MiB.
type TaskDefinitionContainerTmpfs struct {
	ContainerPath string    `ub:"container-path"`
	Size          int64     `ub:"size"`
	MountOptions  *[]string `ub:"mount-options"`
}

// TaskDefinitionContainerUlimit sets one resource limit, such as nofile, in
// the container. It mirrors the SDK Ulimit type.
type TaskDefinitionContainerUlimit struct {
	HardLimit int64  `ub:"hard-limit"`
	Name      string `ub:"name"`
	SoftLimit int64  `ub:"soft-limit"`
}

// TaskDefinitionContainerVolumeFrom mounts all volumes of another container
// of the task. It mirrors the SDK VolumeFrom type.
type TaskDefinitionContainerVolumeFrom struct {
	ReadOnly        *bool   `ub:"read-only"`
	SourceContainer *string `ub:"source-container"`
}

// taskDefinitionContainersSDK converts the container definition list to its
// SDK type.
func taskDefinitionContainersSDK(
	containers []TaskDefinitionContainerDefinition,
) []ecstypes.ContainerDefinition {
	out := make([]ecstypes.ContainerDefinition, 0, len(containers))
	for _, c := range containers {
		out = append(out, c.sdk())
	}
	return out
}

// sdk converts one container definition to its SDK type. The SDK Cpu member
// is a plain int32 whose zero value the server reads as no reservation, so
// an omitted cpu rides as 0, the same value every SDK caller sends when the
// field is unset.
func (c TaskDefinitionContainerDefinition) sdk() ecstypes.ContainerDefinition {
	out := ecstypes.ContainerDefinition{
		Name:                   aws.String(c.Name),
		Image:                  aws.String(c.Image),
		Command:                derefStrings(c.Command),
		Cpu:                    int32(aws.ToInt64(c.Cpu)),
		CredentialSpecs:        derefStrings(c.CredentialSpecs),
		DependsOn:              taskDefinitionDependenciesSDK(c.DependsOn),
		DisableNetworking:      c.DisableNetworking,
		DnsSearchDomains:       derefStrings(c.DnsSearchDomains),
		DnsServers:             derefStrings(c.DnsServers),
		DockerLabels:           derefStringMap(c.DockerLabels),
		DockerSecurityOptions:  derefStrings(c.DockerSecurityOptions),
		EntryPoint:             derefStrings(c.EntryPoint),
		Environment:            taskDefinitionKeyValuePairs(c.Environment),
		EnvironmentFiles:       taskDefinitionEnvironmentFilesSDK(c.EnvironmentFiles),
		Essential:              c.Essential,
		ExtraHosts:             taskDefinitionExtraHostsSDK(c.ExtraHosts),
		FirelensConfiguration:  c.FirelensConfiguration.sdk(),
		HealthCheck:            c.HealthCheck.sdk(),
		Hostname:               c.Hostname,
		Interactive:            c.Interactive,
		Links:                  derefStrings(c.Links),
		LinuxParameters:        c.LinuxParameters.sdk(),
		LogConfiguration:       c.LogConfiguration.sdk(),
		Memory:                 ptr.Int32(c.Memory),
		MemoryReservation:      ptr.Int32(c.MemoryReservation),
		MountPoints:            taskDefinitionMountPointsSDK(c.MountPoints),
		PortMappings:           taskDefinitionPortMappingsSDK(c.PortMappings),
		Privileged:             c.Privileged,
		PseudoTerminal:         c.PseudoTerminal,
		ReadonlyRootFilesystem: c.ReadonlyRootFilesystem,
		RepositoryCredentials:  c.RepositoryCredentials.sdk(),
		ResourceRequirements:   taskDefinitionResourceRequirementsSDK(c.ResourceRequirements),
		RestartPolicy:          c.RestartPolicy.sdk(),
		Secrets:                taskDefinitionSecretsSDK(c.Secrets),
		StartTimeout:           ptr.Int32(c.StartTimeout),
		StopTimeout:            ptr.Int32(c.StopTimeout),
		SystemControls:         taskDefinitionSystemControlsSDK(c.SystemControls),
		Ulimits:                taskDefinitionUlimitsSDK(c.Ulimits),
		User:                   c.User,
		VolumesFrom:            taskDefinitionVolumesFromSDK(c.VolumesFrom),
		WorkingDirectory:       c.WorkingDirectory,
	}
	if c.VersionConsistency != nil {
		out.VersionConsistency = ecstypes.VersionConsistency(*c.VersionConsistency)
	}
	return out
}

func taskDefinitionDependenciesSDK(
	deps *[]TaskDefinitionContainerDependency,
) []ecstypes.ContainerDependency {
	if deps == nil {
		return nil
	}
	out := make([]ecstypes.ContainerDependency, 0, len(*deps))
	for _, d := range *deps {
		out = append(out, ecstypes.ContainerDependency{
			Condition:     ecstypes.ContainerCondition(d.Condition),
			ContainerName: aws.String(d.ContainerName),
		})
	}
	return out
}

func taskDefinitionEnvironmentFilesSDK(
	files *[]TaskDefinitionContainerEnvironmentFile,
) []ecstypes.EnvironmentFile {
	if files == nil {
		return nil
	}
	out := make([]ecstypes.EnvironmentFile, 0, len(*files))
	for _, f := range *files {
		out = append(out, ecstypes.EnvironmentFile{
			Type:  ecstypes.EnvironmentFileType(f.Type),
			Value: aws.String(f.Value),
		})
	}
	return out
}

func taskDefinitionExtraHostsSDK(
	hosts *[]TaskDefinitionContainerHostEntry,
) []ecstypes.HostEntry {
	if hosts == nil {
		return nil
	}
	out := make([]ecstypes.HostEntry, 0, len(*hosts))
	for _, h := range *hosts {
		out = append(out, ecstypes.HostEntry{
			Hostname:  aws.String(h.Hostname),
			IpAddress: aws.String(h.IpAddress),
		})
	}
	return out
}

// sdk converts the FireLens block to its SDK type, returning nil for a nil
// block so an absent block stays out of the request.
func (f *TaskDefinitionContainerFirelens) sdk() *ecstypes.FirelensConfiguration {
	if f == nil {
		return nil
	}
	return &ecstypes.FirelensConfiguration{
		Type:    ecstypes.FirelensConfigurationType(f.Type),
		Options: derefStringMap(f.Options),
	}
}

// sdk converts the health check block to its SDK type, returning nil for a
// nil block so an absent block stays out of the request.
func (h *TaskDefinitionContainerHealthCheck) sdk() *ecstypes.HealthCheck {
	if h == nil {
		return nil
	}
	return &ecstypes.HealthCheck{
		Command:     h.Command,
		Interval:    ptr.Int32(h.Interval),
		Retries:     ptr.Int32(h.Retries),
		StartPeriod: ptr.Int32(h.StartPeriod),
		Timeout:     ptr.Int32(h.Timeout),
	}
}

// sdk converts the Linux parameters block to its SDK type, returning nil
// for a nil block so an absent block stays out of the request.
func (p *TaskDefinitionContainerLinuxParameters) sdk() *ecstypes.LinuxParameters {
	if p == nil {
		return nil
	}
	out := &ecstypes.LinuxParameters{
		Devices:            taskDefinitionDevicesSDK(p.Devices),
		InitProcessEnabled: p.InitProcessEnabled,
		MaxSwap:            ptr.Int32(p.MaxSwap),
		SharedMemorySize:   ptr.Int32(p.SharedMemorySize),
		Swappiness:         ptr.Int32(p.Swappiness),
		Tmpfs:              taskDefinitionTmpfsSDK(p.Tmpfs),
	}
	if c := p.Capabilities; c != nil {
		out.Capabilities = &ecstypes.KernelCapabilities{
			Add:  derefStrings(c.Add),
			Drop: derefStrings(c.Drop),
		}
	}
	return out
}

func taskDefinitionDevicesSDK(devices *[]TaskDefinitionContainerDevice) []ecstypes.Device {
	if devices == nil {
		return nil
	}
	out := make([]ecstypes.Device, 0, len(*devices))
	for _, d := range *devices {
		device := ecstypes.Device{
			HostPath:      aws.String(d.HostPath),
			ContainerPath: d.ContainerPath,
		}
		for _, p := range derefStrings(d.Permissions) {
			device.Permissions = append(device.Permissions,
				ecstypes.DeviceCgroupPermission(p))
		}
		out = append(out, device)
	}
	return out
}

func taskDefinitionTmpfsSDK(mounts *[]TaskDefinitionContainerTmpfs) []ecstypes.Tmpfs {
	if mounts == nil {
		return nil
	}
	out := make([]ecstypes.Tmpfs, 0, len(*mounts))
	for _, m := range *mounts {
		out = append(out, ecstypes.Tmpfs{
			ContainerPath: aws.String(m.ContainerPath),
			Size:          int32(m.Size),
			MountOptions:  derefStrings(m.MountOptions),
		})
	}
	return out
}

// sdk converts the log configuration block to its SDK type, returning nil
// for a nil block so an absent block stays out of the request.
func (l *TaskDefinitionContainerLogConfiguration) sdk() *ecstypes.LogConfiguration {
	if l == nil {
		return nil
	}
	return &ecstypes.LogConfiguration{
		LogDriver:     ecstypes.LogDriver(l.LogDriver),
		Options:       derefStringMap(l.Options),
		SecretOptions: taskDefinitionSecretsSDK(l.SecretOptions),
	}
}

func taskDefinitionMountPointsSDK(
	points *[]TaskDefinitionContainerMountPoint,
) []ecstypes.MountPoint {
	if points == nil {
		return nil
	}
	out := make([]ecstypes.MountPoint, 0, len(*points))
	for _, p := range *points {
		out = append(out, ecstypes.MountPoint{
			ContainerPath: p.ContainerPath,
			ReadOnly:      p.ReadOnly,
			SourceVolume:  p.SourceVolume,
		})
	}
	return out
}

func taskDefinitionPortMappingsSDK(
	mappings *[]TaskDefinitionContainerPortMapping,
) []ecstypes.PortMapping {
	if mappings == nil {
		return nil
	}
	out := make([]ecstypes.PortMapping, 0, len(*mappings))
	for _, m := range *mappings {
		mapping := ecstypes.PortMapping{
			ContainerPort:      ptr.Int32(m.ContainerPort),
			ContainerPortRange: m.ContainerPortRange,
			HostPort:           ptr.Int32(m.HostPort),
			Name:               m.Name,
		}
		if m.AppProtocol != nil {
			mapping.AppProtocol = ecstypes.ApplicationProtocol(*m.AppProtocol)
		}
		if m.Protocol != nil {
			mapping.Protocol = ecstypes.TransportProtocol(*m.Protocol)
		}
		out = append(out, mapping)
	}
	return out
}

// sdk converts the repository credentials block to its SDK type, returning
// nil for a nil block so an absent block stays out of the request.
func (r *TaskDefinitionContainerRepositoryCredentials) sdk() *ecstypes.RepositoryCredentials {
	if r == nil {
		return nil
	}
	return &ecstypes.RepositoryCredentials{
		CredentialsParameter: aws.String(r.CredentialsParameter),
	}
}

func taskDefinitionResourceRequirementsSDK(
	requirements *[]TaskDefinitionContainerResourceRequirement,
) []ecstypes.ResourceRequirement {
	if requirements == nil {
		return nil
	}
	out := make([]ecstypes.ResourceRequirement, 0, len(*requirements))
	for _, r := range *requirements {
		out = append(out, ecstypes.ResourceRequirement{
			Type:  ecstypes.ResourceType(r.Type),
			Value: aws.String(r.Value),
		})
	}
	return out
}

// sdk converts the restart policy block to its SDK type, returning nil for
// a nil block so an absent block stays out of the request.
func (p *TaskDefinitionContainerRestartPolicy) sdk() *ecstypes.ContainerRestartPolicy {
	if p == nil {
		return nil
	}
	out := &ecstypes.ContainerRestartPolicy{
		Enabled:              aws.Bool(p.Enabled),
		RestartAttemptPeriod: ptr.Int32(p.RestartAttemptPeriod),
	}
	if p.IgnoredExitCodes != nil {
		out.IgnoredExitCodes = make([]int32, 0, len(*p.IgnoredExitCodes))
		for _, code := range *p.IgnoredExitCodes {
			out.IgnoredExitCodes = append(out.IgnoredExitCodes, int32(code))
		}
	}
	return out
}

func taskDefinitionSecretsSDK(secrets *[]TaskDefinitionContainerSecret) []ecstypes.Secret {
	if secrets == nil {
		return nil
	}
	out := make([]ecstypes.Secret, 0, len(*secrets))
	for _, s := range *secrets {
		out = append(out, ecstypes.Secret{
			Name:      aws.String(s.Name),
			ValueFrom: aws.String(s.ValueFrom),
		})
	}
	return out
}

func taskDefinitionSystemControlsSDK(
	controls *[]TaskDefinitionContainerSystemControl,
) []ecstypes.SystemControl {
	if controls == nil {
		return nil
	}
	out := make([]ecstypes.SystemControl, 0, len(*controls))
	for _, c := range *controls {
		out = append(out, ecstypes.SystemControl{
			Namespace: c.Namespace,
			Value:     c.Value,
		})
	}
	return out
}

func taskDefinitionUlimitsSDK(ulimits *[]TaskDefinitionContainerUlimit) []ecstypes.Ulimit {
	if ulimits == nil {
		return nil
	}
	out := make([]ecstypes.Ulimit, 0, len(*ulimits))
	for _, u := range *ulimits {
		out = append(out, ecstypes.Ulimit{
			HardLimit: int32(u.HardLimit),
			Name:      ecstypes.UlimitName(u.Name),
			SoftLimit: int32(u.SoftLimit),
		})
	}
	return out
}

func taskDefinitionVolumesFromSDK(
	volumes *[]TaskDefinitionContainerVolumeFrom,
) []ecstypes.VolumeFrom {
	if volumes == nil {
		return nil
	}
	out := make([]ecstypes.VolumeFrom, 0, len(*volumes))
	for _, v := range *volumes {
		out = append(out, ecstypes.VolumeFrom{
			ReadOnly:        v.ReadOnly,
			SourceContainer: v.SourceContainer,
		})
	}
	return out
}
