package lambdamicrovms

import (
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	lambdamicrovmstypes "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms/types"
)

const (
	minInt32 = -1 << 31
	maxInt32 = 1<<31 - 1
)

func codeArtifactToSDK(in CodeArtifact) lambdamicrovmstypes.CodeArtifact {
	return &lambdamicrovmstypes.CodeArtifactMemberUri{Value: in.Uri}
}

func loggingToSDK(in *Logging) lambdamicrovmstypes.Logging {
	if in == nil {
		return nil
	}
	if in.CloudWatch != nil {
		return &lambdamicrovmstypes.LoggingMemberCloudWatch{
			Value: lambdamicrovmstypes.CloudWatchLogging{
				LogGroup:  in.CloudWatch.LogGroup,
				LogStream: in.CloudWatch.LogStream,
			},
		}
	}
	if in.Disabled != nil && *in.Disabled {
		return &lambdamicrovmstypes.LoggingMemberDisabled{
			Value: lambdamicrovmstypes.LoggingDisabled{},
		}
	}
	return nil
}

func loggingFromSDK(in lambdamicrovmstypes.Logging) (*Logging, error) {
	switch v := in.(type) {
	case nil:
		return nil, nil
	case *lambdamicrovmstypes.LoggingMemberCloudWatch:
		return &Logging{
			CloudWatch: &CloudWatchLogging{
				LogGroup:  v.Value.LogGroup,
				LogStream: v.Value.LogStream,
			},
		}, nil
	case *lambdamicrovmstypes.LoggingMemberDisabled:
		disabled := true
		return &Logging{Disabled: &disabled}, nil
	case *lambdamicrovmstypes.UnknownUnionMember:
		return nil, fmt.Errorf("unknown logging union member %q", v.Tag)
	default:
		return nil, fmt.Errorf("unknown logging union type %T", in)
	}
}

func cpuConfigurationsToSDK(
	in *[]CpuConfiguration,
) []lambdamicrovmstypes.CpuConfiguration {
	if in == nil {
		return nil
	}
	out := make([]lambdamicrovmstypes.CpuConfiguration, 0, len(*in))
	for _, cfg := range *in {
		out = append(out, lambdamicrovmstypes.CpuConfiguration{
			Architecture: lambdamicrovmstypes.Architecture(cfg.Architecture),
		})
	}
	return out
}

func resourcesToSDK(in *[]Resources) ([]lambdamicrovmstypes.Resources, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]lambdamicrovmstypes.Resources, 0, len(*in))
	for _, resource := range *in {
		memory, err := int32PtrFromInt64(
			"minimum-memory-in-mib", resource.MinimumMemoryInMiB)
		if err != nil {
			return nil, err
		}
		out = append(out, lambdamicrovmstypes.Resources{
			MinimumMemoryInMiB: memory,
		})
	}
	return out, nil
}

func hooksToSDK(in *Hooks) (*lambdamicrovmstypes.Hooks, error) {
	if in == nil {
		return nil, nil
	}
	port, err := int32PtrFromOptionalInt64("hooks.port", in.Port)
	if err != nil {
		return nil, err
	}
	out := &lambdamicrovmstypes.Hooks{Port: port}
	if in.MicrovmHooks != nil {
		out.MicrovmHooks, err = microvmHooksToSDK(in.MicrovmHooks)
		if err != nil {
			return nil, err
		}
	}
	if in.MicrovmImageHooks != nil {
		out.MicrovmImageHooks, err = microvmImageHooksToSDK(in.MicrovmImageHooks)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func microvmHooksToSDK(in *MicrovmHooks) (*lambdamicrovmstypes.MicrovmHooks, error) {
	out := &lambdamicrovmstypes.MicrovmHooks{
		Run:       hookState(in.Run),
		Resume:    hookState(in.Resume),
		Suspend:   hookState(in.Suspend),
		Terminate: hookState(in.Terminate),
	}
	var err error
	out.RunTimeoutInSeconds, err = int32PtrFromOptionalInt64(
		"run-timeout-in-seconds", in.RunTimeoutInSeconds)
	if err != nil {
		return nil, err
	}
	out.ResumeTimeoutInSeconds, err = int32PtrFromOptionalInt64(
		"resume-timeout-in-seconds", in.ResumeTimeoutInSeconds)
	if err != nil {
		return nil, err
	}
	out.SuspendTimeoutInSeconds, err = int32PtrFromOptionalInt64(
		"suspend-timeout-in-seconds", in.SuspendTimeoutInSeconds)
	if err != nil {
		return nil, err
	}
	out.TerminateTimeoutInSeconds, err = int32PtrFromOptionalInt64(
		"terminate-timeout-in-seconds", in.TerminateTimeoutInSeconds)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func microvmImageHooksToSDK(
	in *MicrovmImageHooks,
) (*lambdamicrovmstypes.MicrovmImageHooks, error) {
	out := &lambdamicrovmstypes.MicrovmImageHooks{
		Ready:    hookState(in.Ready),
		Validate: hookState(in.Validate),
	}
	var err error
	out.ReadyTimeoutInSeconds, err = int32PtrFromOptionalInt64(
		"ready-timeout-in-seconds", in.ReadyTimeoutInSeconds)
	if err != nil {
		return nil, err
	}
	out.ValidateTimeoutInSeconds, err = int32PtrFromOptionalInt64(
		"validate-timeout-in-seconds", in.ValidateTimeoutInSeconds)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func idlePolicyToSDK(in *IdlePolicy) (*lambdamicrovmstypes.IdlePolicy, error) {
	if in == nil {
		return nil, nil
	}
	maxIdle, err := int32PtrFromInt64(
		"max-idle-duration-seconds", in.MaxIdleDurationSeconds)
	if err != nil {
		return nil, err
	}
	suspended, err := int32PtrFromInt64(
		"suspended-duration-seconds", in.SuspendedDurationSeconds)
	if err != nil {
		return nil, err
	}
	autoResume := in.AutoResumeEnabled
	return &lambdamicrovmstypes.IdlePolicy{
		AutoResumeEnabled:        &autoResume,
		MaxIdleDurationSeconds:   maxIdle,
		SuspendedDurationSeconds: suspended,
	}, nil
}

func portSpecificationToSDK(
	in PortSpecification,
) (lambdamicrovmstypes.PortSpecification, error) {
	if err := validatePortSpecification(in); err != nil {
		return nil, err
	}
	if in.AllPorts != nil {
		return &lambdamicrovmstypes.PortSpecificationMemberAllPorts{
			Value: lambdamicrovmstypes.Unit{},
		}, nil
	}
	if in.Port != nil {
		return &lambdamicrovmstypes.PortSpecificationMemberPort{
			Value: int32(*in.Port),
		}, nil
	}
	return &lambdamicrovmstypes.PortSpecificationMemberRange{
		Value: lambdamicrovmstypes.PortRange{
			StartPort: int32Ptr(int32(in.Range.StartPort)),
			EndPort:   int32Ptr(int32(in.Range.EndPort)),
		},
	}, nil
}

func portSpecificationsToSDK(
	in []PortSpecification,
) ([]lambdamicrovmstypes.PortSpecification, error) {
	out := make([]lambdamicrovmstypes.PortSpecification, 0, len(in))
	for _, spec := range in {
		converted, err := portSpecificationToSDK(spec)
		if err != nil {
			return nil, err
		}
		out = append(out, converted)
	}
	return out, nil
}

func validatePortSpecification(in PortSpecification) error {
	count := 0
	if in.AllPorts != nil {
		count++
	}
	if in.Port != nil {
		count++
	}
	if in.Range != nil {
		count++
	}
	if count != 1 {
		return errors.New("port specification must set exactly one member")
	}
	if in.AllPorts != nil {
		if !*in.AllPorts {
			return errors.New("all-ports must be true")
		}
		return nil
	}
	if in.Port != nil {
		return validatePort("port", *in.Port)
	}
	if err := validatePort("range start-port", in.Range.StartPort); err != nil {
		return fmt.Errorf("range ports must be between 1 and 65535: %w", err)
	}
	if err := validatePort("range end-port", in.Range.EndPort); err != nil {
		return fmt.Errorf("range ports must be between 1 and 65535: %w", err)
	}
	if in.Range.StartPort > in.Range.EndPort {
		return errors.New("port range start must be no greater than end")
	}
	return nil
}

func validatePort(name string, value int64) error {
	if value < 1 || value > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", name)
	}
	return nil
}

func hookState(in *string) lambdamicrovmstypes.HookState {
	if in == nil {
		return ""
	}
	return lambdamicrovmstypes.HookState(*in)
}

func int32PtrFromOptionalInt64(name string, in *int64) (*int32, error) {
	if in == nil {
		return nil, nil
	}
	return int32PtrFromInt64(name, *in)
}

func int32PtrFromInt64(name string, in int64) (*int32, error) {
	value, err := int32FromInt64(name, in)
	if err != nil {
		return nil, err
	}
	return int32Ptr(value), nil
}

func int32FromInt64(name string, in int64) (int32, error) {
	if in < minInt32 || in > maxInt32 {
		return 0, fmt.Errorf("%s must fit in int32", name)
	}
	return int32(in), nil
}

func int32Ptr(in int32) *int32 {
	return &in
}

func codeArtifactFromSDK(in lambdamicrovmstypes.CodeArtifact) (CodeArtifact, error) {
	switch v := in.(type) {
	case nil:
		return CodeArtifact{}, nil
	case *lambdamicrovmstypes.CodeArtifactMemberUri:
		return CodeArtifact{Uri: v.Value}, nil
	case *lambdamicrovmstypes.UnknownUnionMember:
		return CodeArtifact{}, fmt.Errorf("unknown code-artifact union member %q", v.Tag)
	default:
		return CodeArtifact{}, fmt.Errorf("unknown code-artifact union type %T", in)
	}
}

func cpuConfigurationsFromSDK(
	in []lambdamicrovmstypes.CpuConfiguration,
) []CpuConfiguration {
	if in == nil {
		return nil
	}
	out := make([]CpuConfiguration, 0, len(in))
	for _, cfg := range in {
		out = append(out, CpuConfiguration{Architecture: string(cfg.Architecture)})
	}
	return out
}

func resourcesFromSDK(in []lambdamicrovmstypes.Resources) []Resources {
	if in == nil {
		return nil
	}
	out := make([]Resources, 0, len(in))
	for _, resource := range in {
		out = append(out, Resources{
			MinimumMemoryInMiB: int64(aws.ToInt32(resource.MinimumMemoryInMiB)),
		})
	}
	return out
}

func hooksFromSDK(in *lambdamicrovmstypes.Hooks) *Hooks {
	if in == nil {
		return nil
	}
	return &Hooks{
		Port:              int64PtrFromInt32(in.Port),
		MicrovmHooks:      microvmHooksFromSDK(in.MicrovmHooks),
		MicrovmImageHooks: microvmImageHooksFromSDK(in.MicrovmImageHooks),
	}
}

func microvmHooksFromSDK(in *lambdamicrovmstypes.MicrovmHooks) *MicrovmHooks {
	if in == nil {
		return nil
	}
	return &MicrovmHooks{
		Run:                       hookStateString(in.Run),
		RunTimeoutInSeconds:       int64PtrFromInt32(in.RunTimeoutInSeconds),
		Resume:                    hookStateString(in.Resume),
		ResumeTimeoutInSeconds:    int64PtrFromInt32(in.ResumeTimeoutInSeconds),
		Suspend:                   hookStateString(in.Suspend),
		SuspendTimeoutInSeconds:   int64PtrFromInt32(in.SuspendTimeoutInSeconds),
		Terminate:                 hookStateString(in.Terminate),
		TerminateTimeoutInSeconds: int64PtrFromInt32(in.TerminateTimeoutInSeconds),
	}
}

func microvmImageHooksFromSDK(
	in *lambdamicrovmstypes.MicrovmImageHooks,
) *MicrovmImageHooks {
	if in == nil {
		return nil
	}
	return &MicrovmImageHooks{
		Ready:                    hookStateString(in.Ready),
		ReadyTimeoutInSeconds:    int64PtrFromInt32(in.ReadyTimeoutInSeconds),
		Validate:                 hookStateString(in.Validate),
		ValidateTimeoutInSeconds: int64PtrFromInt32(in.ValidateTimeoutInSeconds),
	}
}

func idlePolicyFromSDK(in *lambdamicrovmstypes.IdlePolicy) *IdlePolicy {
	if in == nil {
		return nil
	}
	return &IdlePolicy{
		AutoResumeEnabled:        aws.ToBool(in.AutoResumeEnabled),
		MaxIdleDurationSeconds:   int64(aws.ToInt32(in.MaxIdleDurationSeconds)),
		SuspendedDurationSeconds: int64(aws.ToInt32(in.SuspendedDurationSeconds)),
	}
}

func managedMicrovmImageSummaryFromSDK(
	in lambdamicrovmstypes.ManagedMicrovmImageSummary,
) ManagedMicrovmImageSummary {
	return ManagedMicrovmImageSummary{
		ImageArn:  aws.ToString(in.ImageArn),
		CreatedAt: formatTime(in.CreatedAt),
		UpdatedAt: formatTime(in.UpdatedAt),
	}
}

func managedMicrovmImageVersionFromSDK(
	in lambdamicrovmstypes.ManagedMicrovmImageVersion,
) ManagedMicrovmImageVersion {
	return ManagedMicrovmImageVersion{
		ImageArn:     aws.ToString(in.ImageArn),
		ImageVersion: aws.ToString(in.ImageVersion),
		CreatedAt:    formatTime(in.CreatedAt),
		UpdatedAt:    formatTime(in.UpdatedAt),
	}
}

func microvmImageSummaryFromSDK(
	in lambdamicrovmstypes.MicrovmImageSummary,
) MicrovmImageSummary {
	return MicrovmImageSummary{
		ImageArn:                 aws.ToString(in.ImageArn),
		Name:                     aws.ToString(in.Name),
		State:                    string(in.State),
		CreatedAt:                formatTime(in.CreatedAt),
		LatestActiveImageVersion: aws.ToString(in.LatestActiveImageVersion),
		LatestFailedImageVersion: aws.ToString(in.LatestFailedImageVersion),
	}
}

func microvmImageOutputFromGet(
	in *awslambdamicrovms.GetMicrovmImageOutput,
) *MicrovmImageResourceOutput {
	return &MicrovmImageResourceOutput{
		ImageArn:                 aws.ToString(in.ImageArn),
		Name:                     aws.ToString(in.Name),
		State:                    string(in.State),
		CreatedAt:                formatTime(in.CreatedAt),
		UpdatedAt:                formatTime(in.UpdatedAt),
		LatestActiveImageVersion: aws.ToString(in.LatestActiveImageVersion),
		LatestFailedImageVersion: aws.ToString(in.LatestFailedImageVersion),
	}
}

func microvmImageDataOutputFromGet(
	in *awslambdamicrovms.GetMicrovmImageOutput,
) *MicrovmImageDataSourceOutput {
	return &MicrovmImageDataSourceOutput{
		ImageArn:                 aws.ToString(in.ImageArn),
		Name:                     aws.ToString(in.Name),
		State:                    string(in.State),
		CreatedAt:                formatTime(in.CreatedAt),
		UpdatedAt:                formatTime(in.UpdatedAt),
		LatestActiveImageVersion: aws.ToString(in.LatestActiveImageVersion),
		LatestFailedImageVersion: aws.ToString(in.LatestFailedImageVersion),
		Tags:                     in.Tags,
	}
}

func microvmImageVersionOutputFromGet(
	in *awslambdamicrovms.GetMicrovmImageVersionOutput,
) (*MicrovmImageVersionDataSourceOutput, error) {
	return microvmImageVersionDataOutput(
		aws.ToString(in.ImageArn),
		aws.ToString(in.ImageVersion),
		string(in.State),
		string(in.Status),
		aws.ToString(in.BaseImageArn),
		aws.ToString(in.BaseImageVersion),
		aws.ToString(in.BuildRoleArn),
		in.CodeArtifact,
		in.AdditionalOsCapabilities,
		in.CpuConfigurations,
		aws.ToString(in.Description),
		in.EgressNetworkConnectors,
		in.EnvironmentVariables,
		in.Hooks,
		in.Logging,
		in.Resources,
		aws.ToString(in.StateReason),
		in.Tags,
		in.CreatedAt,
		in.UpdatedAt,
	)
}

func microvmImageVersionOutputFromUpdate(
	in *awslambdamicrovms.UpdateMicrovmImageVersionOutput,
) (*MicrovmImageVersionDataSourceOutput, error) {
	return microvmImageVersionDataOutput(
		aws.ToString(in.ImageArn),
		aws.ToString(in.ImageVersion),
		string(in.State),
		string(in.Status),
		aws.ToString(in.BaseImageArn),
		aws.ToString(in.BaseImageVersion),
		aws.ToString(in.BuildRoleArn),
		in.CodeArtifact,
		in.AdditionalOsCapabilities,
		in.CpuConfigurations,
		aws.ToString(in.Description),
		in.EgressNetworkConnectors,
		in.EnvironmentVariables,
		in.Hooks,
		in.Logging,
		in.Resources,
		aws.ToString(in.StateReason),
		in.Tags,
		in.CreatedAt,
		in.UpdatedAt,
	)
}

func microvmImageVersionDataOutput(
	imageArn string,
	imageVersion string,
	state string,
	status string,
	baseImageArn string,
	baseImageVersion string,
	buildRoleArn string,
	codeArtifactIn lambdamicrovmstypes.CodeArtifact,
	additionalOsCapabilities []lambdamicrovmstypes.Capability,
	cpuConfigurations []lambdamicrovmstypes.CpuConfiguration,
	description string,
	egressNetworkConnectors []string,
	environmentVariables map[string]string,
	hooksIn *lambdamicrovmstypes.Hooks,
	loggingIn lambdamicrovmstypes.Logging,
	resourcesIn []lambdamicrovmstypes.Resources,
	stateReason string,
	tags map[string]string,
	createdAt *time.Time,
	updatedAt *time.Time,
) (*MicrovmImageVersionDataSourceOutput, error) {
	codeArtifact, err := codeArtifactFromSDK(codeArtifactIn)
	if err != nil {
		return nil, err
	}
	logging, err := loggingFromSDK(loggingIn)
	if err != nil {
		return nil, err
	}
	return &MicrovmImageVersionDataSourceOutput{
		ImageArn:                 imageArn,
		ImageVersion:             imageVersion,
		State:                    state,
		Status:                   status,
		BaseImageArn:             baseImageArn,
		BaseImageVersion:         baseImageVersion,
		BuildRoleArn:             buildRoleArn,
		CodeArtifact:             codeArtifact,
		AdditionalOsCapabilities: capabilitiesFromSDK(additionalOsCapabilities),
		CpuConfigurations:        cpuConfigurationsFromSDK(cpuConfigurations),
		Description:              description,
		EgressNetworkConnectors:  egressNetworkConnectors,
		EnvironmentVariables:     environmentVariables,
		Hooks:                    hooksFromSDK(hooksIn),
		Logging:                  logging,
		Resources:                resourcesFromSDK(resourcesIn),
		StateReason:              stateReason,
		Tags:                     tags,
		CreatedAt:                formatTime(createdAt),
		UpdatedAt:                formatTime(updatedAt),
	}, nil
}

func microvmImageVersionSummaryFromSDK(
	in lambdamicrovmstypes.MicrovmImageVersionSummary,
) (MicrovmImageVersionSummary, error) {
	codeArtifact, err := codeArtifactFromSDK(in.CodeArtifact)
	if err != nil {
		return MicrovmImageVersionSummary{}, err
	}
	logging, err := loggingFromSDK(in.Logging)
	if err != nil {
		return MicrovmImageVersionSummary{}, err
	}
	return MicrovmImageVersionSummary{
		ImageArn:                 aws.ToString(in.ImageArn),
		ImageVersion:             aws.ToString(in.ImageVersion),
		State:                    string(in.State),
		Status:                   string(in.Status),
		BaseImageArn:             aws.ToString(in.BaseImageArn),
		BaseImageVersion:         aws.ToString(in.BaseImageVersion),
		BuildRoleArn:             aws.ToString(in.BuildRoleArn),
		CodeArtifact:             codeArtifact,
		AdditionalOsCapabilities: capabilitiesFromSDK(in.AdditionalOsCapabilities),
		CpuConfigurations:        cpuConfigurationsFromSDK(in.CpuConfigurations),
		Description:              aws.ToString(in.Description),
		EgressNetworkConnectors:  in.EgressNetworkConnectors,
		EnvironmentVariables:     in.EnvironmentVariables,
		Hooks:                    hooksFromSDK(in.Hooks),
		Logging:                  logging,
		Resources:                resourcesFromSDK(in.Resources),
		StateReason:              aws.ToString(in.StateReason),
		Tags:                     in.Tags,
		CreatedAt:                formatTime(in.CreatedAt),
		UpdatedAt:                formatTime(in.UpdatedAt),
	}, nil
}

func microvmImageBuildOutputFromGet(
	in *awslambdamicrovms.GetMicrovmImageBuildOutput,
) *MicrovmImageBuildDataSourceOutput {
	return &MicrovmImageBuildDataSourceOutput{
		ImageArn:          aws.ToString(in.ImageArn),
		ImageVersion:      aws.ToString(in.ImageVersion),
		BuildId:           aws.ToString(in.BuildId),
		BuildState:        string(in.BuildState),
		Architecture:      string(in.Architecture),
		Chipset:           string(in.Chipset),
		ChipsetGeneration: aws.ToString(in.ChipsetGeneration),
		SnapshotBuild:     snapshotBuildFromSDK(in.SnapshotBuild),
		StateReason:       aws.ToString(in.StateReason),
		CreatedAt:         formatTime(in.CreatedAt),
	}
}

func microvmImageBuildSummaryFromSDK(
	in lambdamicrovmstypes.MicrovmImageBuildSummary,
) MicrovmImageBuildSummary {
	return MicrovmImageBuildSummary{
		ImageArn:          aws.ToString(in.ImageArn),
		ImageVersion:      aws.ToString(in.ImageVersion),
		BuildId:           aws.ToString(in.BuildId),
		BuildState:        string(in.BuildState),
		Architecture:      string(in.Architecture),
		Chipset:           string(in.Chipset),
		ChipsetGeneration: aws.ToString(in.ChipsetGeneration),
		StateReason:       aws.ToString(in.StateReason),
		CreatedAt:         formatTime(in.CreatedAt),
	}
}

func snapshotBuildFromSDK(in *lambdamicrovmstypes.SnapshotBuild) *SnapshotBuild {
	if in == nil {
		return nil
	}
	return &SnapshotBuild{
		CodeInstallSizeInBytes:    aws.ToInt64(in.CodeInstallSizeInBytes),
		DiskSnapshotSizeInBytes:   aws.ToInt64(in.DiskSnapshotSizeInBytes),
		MemorySnapshotSizeInBytes: aws.ToInt64(in.MemorySnapshotSizeInBytes),
	}
}

func microvmOutputFromGet(in *awslambdamicrovms.GetMicrovmOutput) *MicrovmDataSourceOutput {
	return microvmDataOutput(
		aws.ToString(in.MicrovmId),
		aws.ToString(in.Endpoint),
		aws.ToString(in.ImageArn),
		aws.ToString(in.ImageVersion),
		string(in.State),
		in.StartedAt,
		in.TerminatedAt,
		in.MaximumDurationInSeconds,
		aws.ToString(in.ExecutionRoleArn),
		in.IngressNetworkConnectors,
		in.EgressNetworkConnectors,
		in.IdlePolicy,
		aws.ToString(in.StateReason),
	)
}

func microvmOutputFromRun(in *awslambdamicrovms.RunMicrovmOutput) *MicrovmDataSourceOutput {
	return microvmDataOutput(
		aws.ToString(in.MicrovmId),
		aws.ToString(in.Endpoint),
		aws.ToString(in.ImageArn),
		aws.ToString(in.ImageVersion),
		string(in.State),
		in.StartedAt,
		in.TerminatedAt,
		in.MaximumDurationInSeconds,
		aws.ToString(in.ExecutionRoleArn),
		in.IngressNetworkConnectors,
		in.EgressNetworkConnectors,
		in.IdlePolicy,
		aws.ToString(in.StateReason),
	)
}

func microvmDataOutput(
	microvmID string,
	endpoint string,
	imageArn string,
	imageVersion string,
	state string,
	startedAt *time.Time,
	terminatedAt *time.Time,
	maximumDurationInSeconds *int32,
	executionRoleArn string,
	ingressNetworkConnectors []string,
	egressNetworkConnectors []string,
	idlePolicy *lambdamicrovmstypes.IdlePolicy,
	stateReason string,
) *MicrovmDataSourceOutput {
	return &MicrovmDataSourceOutput{
		MicrovmId:                microvmID,
		Endpoint:                 endpoint,
		ImageArn:                 imageArn,
		ImageVersion:             imageVersion,
		State:                    state,
		StartedAt:                formatTime(startedAt),
		TerminatedAt:             formatTime(terminatedAt),
		MaximumDurationInSeconds: int64(aws.ToInt32(maximumDurationInSeconds)),
		ExecutionRoleArn:         executionRoleArn,
		IngressNetworkConnectors: ingressNetworkConnectors,
		EgressNetworkConnectors:  egressNetworkConnectors,
		IdlePolicy:               idlePolicyFromSDK(idlePolicy),
		StateReason:              stateReason,
	}
}

func microvmSummaryFromSDK(in lambdamicrovmstypes.MicrovmItem) MicrovmSummary {
	return MicrovmSummary{
		MicrovmId:    aws.ToString(in.MicrovmId),
		ImageArn:     aws.ToString(in.ImageArn),
		ImageVersion: aws.ToString(in.ImageVersion),
		State:        string(in.State),
		StartedAt:    formatTime(in.StartedAt),
	}
}

func capabilitiesFromSDK(in []lambdamicrovmstypes.Capability) []string {
	if in == nil {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, value := range in {
		out = append(out, string(value))
	}
	return out
}

func formatTime(in *time.Time) string {
	if in == nil {
		return ""
	}
	return in.UTC().Format(time.RFC3339)
}

func hookStateString(in lambdamicrovmstypes.HookState) *string {
	if in == "" {
		return nil
	}
	out := string(in)
	return &out
}

func int64PtrFromInt32(in *int32) *int64 {
	if in == nil {
		return nil
	}
	out := int64(*in)
	return &out
}
