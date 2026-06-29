package lambdamicrovms

import (
	"errors"
	"fmt"

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
