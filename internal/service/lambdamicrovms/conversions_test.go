package lambdamicrovms

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambdamicrovmstypes "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeArtifactToSDK(t *testing.T) {
	got := codeArtifactToSDK(CodeArtifact{Uri: "s3://bucket/app.tar"})
	member, ok := got.(*lambdamicrovmstypes.CodeArtifactMemberUri)
	require.True(t, ok)
	assert.Equal(t, "s3://bucket/app.tar", member.Value)
}

func TestLoggingToSDK(t *testing.T) {
	t.Run("cloud-watch", func(t *testing.T) {
		got := loggingToSDK(&Logging{
			CloudWatch: &CloudWatchLogging{
				LogGroup:  aws.String("/aws/lambda/microvm"),
				LogStream: aws.String("build"),
			},
		})
		member, ok := got.(*lambdamicrovmstypes.LoggingMemberCloudWatch)
		require.True(t, ok)
		assert.Equal(t, "/aws/lambda/microvm", aws.ToString(member.Value.LogGroup))
		assert.Equal(t, "build", aws.ToString(member.Value.LogStream))
	})

	t.Run("disabled", func(t *testing.T) {
		got := loggingToSDK(&Logging{Disabled: aws.Bool(true)})
		_, ok := got.(*lambdamicrovmstypes.LoggingMemberDisabled)
		assert.True(t, ok)
	})

	t.Run("nil", func(t *testing.T) {
		assert.Nil(t, loggingToSDK(nil))
	})
}

func TestLoggingFromSDK(t *testing.T) {
	t.Run("cloud-watch", func(t *testing.T) {
		got, err := loggingFromSDK(&lambdamicrovmstypes.LoggingMemberCloudWatch{
			Value: lambdamicrovmstypes.CloudWatchLogging{
				LogGroup:  aws.String("/aws/lambda/microvm"),
				LogStream: aws.String("build"),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, got)
		require.NotNil(t, got.CloudWatch)
		assert.Equal(t, "/aws/lambda/microvm", aws.ToString(got.CloudWatch.LogGroup))
		assert.Equal(t, "build", aws.ToString(got.CloudWatch.LogStream))
		assert.Nil(t, got.Disabled)
	})

	t.Run("disabled", func(t *testing.T) {
		got, err := loggingFromSDK(&lambdamicrovmstypes.LoggingMemberDisabled{})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Nil(t, got.CloudWatch)
		assert.Equal(t, true, aws.ToBool(got.Disabled))
	})

	t.Run("unknown", func(t *testing.T) {
		got, err := loggingFromSDK(&lambdamicrovmstypes.UnknownUnionMember{Tag: "future"})
		require.ErrorContains(t, err, "future")
		assert.Nil(t, got)
	})
}

func TestCpuConfigurationsToSDK(t *testing.T) {
	in := []CpuConfiguration{{Architecture: "ARM_64"}}
	got := cpuConfigurationsToSDK(&in)
	require.Len(t, got, 1)
	assert.Equal(t, lambdamicrovmstypes.ArchitectureArm64, got[0].Architecture)
	assert.Nil(t, cpuConfigurationsToSDK(nil))
}

func TestResourcesToSDK(t *testing.T) {
	in := []Resources{{MinimumMemoryInMiB: 512}}
	got, err := resourcesToSDK(&in)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int32(512), aws.ToInt32(got[0].MinimumMemoryInMiB))

	nilGot, err := resourcesToSDK(nil)
	require.NoError(t, err)
	assert.Nil(t, nilGot)
}

func TestHooksToSDK(t *testing.T) {
	got, err := hooksToSDK(&Hooks{
		Port: aws.Int64(8080),
		MicrovmHooks: &MicrovmHooks{
			Run:                       aws.String("ENABLED"),
			RunTimeoutInSeconds:       aws.Int64(10),
			Resume:                    aws.String("DISABLED"),
			ResumeTimeoutInSeconds:    aws.Int64(11),
			Suspend:                   aws.String("ENABLED"),
			SuspendTimeoutInSeconds:   aws.Int64(12),
			Terminate:                 aws.String("DISABLED"),
			TerminateTimeoutInSeconds: aws.Int64(13),
		},
		MicrovmImageHooks: &MicrovmImageHooks{
			Ready:                    aws.String("ENABLED"),
			ReadyTimeoutInSeconds:    aws.Int64(120),
			Validate:                 aws.String("DISABLED"),
			ValidateTimeoutInSeconds: aws.Int64(121),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int32(8080), aws.ToInt32(got.Port))
	require.NotNil(t, got.MicrovmHooks)
	assert.Equal(t, lambdamicrovmstypes.HookStateEnabled, got.MicrovmHooks.Run)
	assert.Equal(t, int32(10), aws.ToInt32(got.MicrovmHooks.RunTimeoutInSeconds))
	assert.Equal(t, lambdamicrovmstypes.HookStateDisabled, got.MicrovmHooks.Resume)
	assert.Equal(t, int32(11), aws.ToInt32(got.MicrovmHooks.ResumeTimeoutInSeconds))
	assert.Equal(t, lambdamicrovmstypes.HookStateEnabled, got.MicrovmHooks.Suspend)
	assert.Equal(t, int32(12), aws.ToInt32(got.MicrovmHooks.SuspendTimeoutInSeconds))
	assert.Equal(t, lambdamicrovmstypes.HookStateDisabled, got.MicrovmHooks.Terminate)
	assert.Equal(t, int32(13), aws.ToInt32(got.MicrovmHooks.TerminateTimeoutInSeconds))
	require.NotNil(t, got.MicrovmImageHooks)
	assert.Equal(t, lambdamicrovmstypes.HookStateEnabled, got.MicrovmImageHooks.Ready)
	assert.Equal(t, int32(120), aws.ToInt32(got.MicrovmImageHooks.ReadyTimeoutInSeconds))
	assert.Equal(t, lambdamicrovmstypes.HookStateDisabled, got.MicrovmImageHooks.Validate)
	assert.Equal(t, int32(121), aws.ToInt32(got.MicrovmImageHooks.ValidateTimeoutInSeconds))

	nilGot, err := hooksToSDK(nil)
	require.NoError(t, err)
	assert.Nil(t, nilGot)
}

func TestIdlePolicyToSDK(t *testing.T) {
	got, err := idlePolicyToSDK(&IdlePolicy{
		AutoResumeEnabled:        true,
		MaxIdleDurationSeconds:   30,
		SuspendedDurationSeconds: 60,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, true, aws.ToBool(got.AutoResumeEnabled))
	assert.Equal(t, int32(30), aws.ToInt32(got.MaxIdleDurationSeconds))
	assert.Equal(t, int32(60), aws.ToInt32(got.SuspendedDurationSeconds))

	nilGot, err := idlePolicyToSDK(nil)
	require.NoError(t, err)
	assert.Nil(t, nilGot)
}

func TestPortSpecificationToSDK(t *testing.T) {
	t.Run("all ports", func(t *testing.T) {
		got, err := portSpecificationToSDK(PortSpecification{AllPorts: aws.Bool(true)})
		require.NoError(t, err)
		_, ok := got.(*lambdamicrovmstypes.PortSpecificationMemberAllPorts)
		assert.True(t, ok)
	})

	t.Run("single port", func(t *testing.T) {
		got, err := portSpecificationToSDK(PortSpecification{Port: aws.Int64(8443)})
		require.NoError(t, err)
		member, ok := got.(*lambdamicrovmstypes.PortSpecificationMemberPort)
		require.True(t, ok)
		assert.Equal(t, int32(8443), member.Value)
	})

	t.Run("range", func(t *testing.T) {
		got, err := portSpecificationToSDK(PortSpecification{
			Range: &PortRange{StartPort: 8000, EndPort: 9000},
		})
		require.NoError(t, err)
		member, ok := got.(*lambdamicrovmstypes.PortSpecificationMemberRange)
		require.True(t, ok)
		assert.Equal(t, int32(8000), aws.ToInt32(member.Value.StartPort))
		assert.Equal(t, int32(9000), aws.ToInt32(member.Value.EndPort))
	})
}

func TestPortSpecificationConstraints(t *testing.T) {
	tests := []struct {
		name string
		spec PortSpecification
		want string
	}{
		{
			name: "multiple members",
			spec: PortSpecification{AllPorts: aws.Bool(true), Port: aws.Int64(443)},
			want: "exactly one",
		},
		{
			name: "false all ports",
			spec: PortSpecification{AllPorts: aws.Bool(false)},
			want: "all-ports must be true",
		},
		{
			name: "port below range",
			spec: PortSpecification{Port: aws.Int64(0)},
			want: "port must be between 1 and 65535",
		},
		{
			name: "port above range",
			spec: PortSpecification{Port: aws.Int64(65536)},
			want: "port must be between 1 and 65535",
		},
		{
			name: "range below range",
			spec: PortSpecification{Range: &PortRange{StartPort: 0, EndPort: 80}},
			want: "range ports must be between 1 and 65535",
		},
		{
			name: "range above range",
			spec: PortSpecification{Range: &PortRange{StartPort: 80, EndPort: 65536}},
			want: "range ports must be between 1 and 65535",
		},
		{
			name: "range inverted",
			spec: PortSpecification{Range: &PortRange{StartPort: 9000, EndPort: 8000}},
			want: "port range start must be no greater than end",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePortSpecification(tt.spec)
			require.ErrorContains(t, err, tt.want)
		})
	}
}
