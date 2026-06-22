package elbv2

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTargetGroupAttachmentReplaceFields(t *testing.T) {
	r := &TargetGroupAttachment{}
	assert.Equal(t, []string{
		"target-group-arn",
		"target-id",
		"availability-zone",
		"port",
		"quic-server-id",
	}, r.ReplaceFields())
}

func TestTargetGroupAttachmentEffectiveTuple(t *testing.T) {
	port := int64(8080)
	zero := int64(0)
	az := "us-east-1a"
	empty := ""
	quicServerID := "0x0123456789abcdef"

	tests := []struct {
		name string
		in   TargetGroupAttachment
		want TargetGroupAttachmentOutput
	}{
		{
			name: "required fields only",
			in: TargetGroupAttachment{
				TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc",
				TargetId:       "10.20.1.50",
			},
			want: TargetGroupAttachmentOutput{
				TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc",
				TargetId:       "10.20.1.50",
			},
		},
		{
			name: "optional tuple fields",
			in: TargetGroupAttachment{
				TargetGroupArn:   "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc",
				TargetId:         "10.20.1.50",
				AvailabilityZone: aws.String(az),
				Port:             aws.Int64(port),
				QuicServerId:     aws.String(quicServerID),
			},
			want: TargetGroupAttachmentOutput{
				TargetGroupArn:   "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc",
				TargetId:         "10.20.1.50",
				AvailabilityZone: aws.String(az),
				Port:             aws.Int64(port),
				QuicServerId:     aws.String(quicServerID),
			},
		},
		{
			name: "zero optional tuple fields are absent",
			in: TargetGroupAttachment{
				TargetGroupArn:   "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc",
				TargetId:         "10.20.1.50",
				AvailabilityZone: aws.String(empty),
				Port:             aws.Int64(zero),
				QuicServerId:     aws.String(empty),
			},
			want: TargetGroupAttachmentOutput{
				TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc",
				TargetId:       "10.20.1.50",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.in.effectiveTuple())
		})
	}
}

func TestTargetGroupAttachmentTupleWithFallback(t *testing.T) {
	oldPort := int64(8080)
	newPort := int64(9090)
	zero := int64(0)
	empty := ""
	current := &TargetGroupAttachment{
		TargetGroupArn:   "arn:aws:elasticloadbalancing:us-east-1:123:new",
		TargetId:         "10.20.2.50",
		AvailabilityZone: aws.String("us-east-1b"),
		Port:             aws.Int64(newPort),
		QuicServerId:     aws.String("0x1111111111111111"),
	}

	tests := []struct {
		name  string
		prior *TargetGroupAttachmentOutput
		want  TargetGroupAttachmentOutput
	}{
		{
			name: "usable prior output wins",
			prior: &TargetGroupAttachmentOutput{
				TargetGroupArn:   "arn:aws:elasticloadbalancing:us-east-1:123:old",
				TargetId:         "10.20.1.50",
				AvailabilityZone: aws.String("us-east-1a"),
				Port:             aws.Int64(oldPort),
				QuicServerId:     aws.String("0x0123456789abcdef"),
			},
			want: TargetGroupAttachmentOutput{
				TargetGroupArn:   "arn:aws:elasticloadbalancing:us-east-1:123:old",
				TargetId:         "10.20.1.50",
				AvailabilityZone: aws.String("us-east-1a"),
				Port:             aws.Int64(oldPort),
				QuicServerId:     aws.String("0x0123456789abcdef"),
			},
		},
		{
			name: "usable prior output does not take current optional fields",
			prior: &TargetGroupAttachmentOutput{
				TargetGroupArn:   "arn:aws:elasticloadbalancing:us-east-1:123:old",
				TargetId:         "10.20.1.50",
				AvailabilityZone: aws.String(empty),
				Port:             aws.Int64(zero),
				QuicServerId:     aws.String(empty),
			},
			want: TargetGroupAttachmentOutput{
				TargetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123:old",
				TargetId:       "10.20.1.50",
			},
		},
		{
			name:  "nil prior output falls back to current input",
			prior: nil,
			want:  current.effectiveTuple(),
		},
		{
			name: "prior output without required fields falls back to current input",
			prior: &TargetGroupAttachmentOutput{
				TargetGroupArn: "",
				TargetId:       "10.20.1.50",
				Port:           aws.Int64(oldPort),
			},
			want: current.effectiveTuple(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, current.tupleWithFallback(tt.prior))
		})
	}
}

func TestTargetGroupAttachmentTargetDescription(t *testing.T) {
	port := int64(8080)
	zero := int64(0)
	az := "us-east-1a"
	empty := ""
	quicServerID := "0x0123456789abcdef"

	tests := []struct {
		name string
		in   TargetGroupAttachmentOutput
		want elbv2types.TargetDescription
	}{
		{
			name: "required fields only",
			in: TargetGroupAttachmentOutput{
				TargetId: "10.20.1.50",
			},
			want: elbv2types.TargetDescription{
				Id: aws.String("10.20.1.50"),
			},
		},
		{
			name: "optional tuple fields",
			in: TargetGroupAttachmentOutput{
				TargetId:         "10.20.1.50",
				AvailabilityZone: aws.String(az),
				Port:             aws.Int64(port),
				QuicServerId:     aws.String(quicServerID),
			},
			want: elbv2types.TargetDescription{
				Id:               aws.String("10.20.1.50"),
				AvailabilityZone: aws.String(az),
				Port:             aws.Int32(8080),
				QuicServerId:     aws.String(quicServerID),
			},
		},
		{
			name: "zero optional tuple fields are absent",
			in: TargetGroupAttachmentOutput{
				TargetId:         "10.20.1.50",
				AvailabilityZone: aws.String(empty),
				Port:             aws.Int64(zero),
				QuicServerId:     aws.String(empty),
			},
			want: elbv2types.TargetDescription{
				Id: aws.String("10.20.1.50"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.in.targetDescription())
		})
	}
}

func TestTargetGroupAttachmentInputs(t *testing.T) {
	zero := int64(0)
	empty := ""
	tuple := TargetGroupAttachmentOutput{
		TargetGroupArn:   "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc",
		TargetId:         "10.20.1.50",
		AvailabilityZone: aws.String(empty),
		Port:             aws.Int64(zero),
		QuicServerId:     aws.String(empty),
	}
	wantTargetGroupArn := aws.String(
		"arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc")
	wantTargets := []elbv2types.TargetDescription{{Id: aws.String("10.20.1.50")}}

	register := tuple.registerInput()
	assert.Equal(t, wantTargetGroupArn, register.TargetGroupArn)
	assert.Equal(t, wantTargets, register.Targets)
	describe := tuple.describeInput()
	assert.Equal(t, wantTargetGroupArn, describe.TargetGroupArn)
	assert.Equal(t, wantTargets, describe.Targets)
	deregister := tuple.deregisterInput()
	assert.Equal(t, wantTargetGroupArn, deregister.TargetGroupArn)
	assert.Equal(t, wantTargets, deregister.Targets)
}

func TestTargetHealthDescriptionEligible(t *testing.T) {
	tests := []struct {
		name string
		desc elbv2types.TargetHealthDescription
		want bool
	}{
		{
			name: "nil target health",
			desc: elbv2types.TargetHealthDescription{},
			want: false,
		},
		{
			name: "not registered",
			desc: healthDescription(elbv2types.TargetHealthReasonEnumNotRegistered),
			want: false,
		},
		{
			name: "deregistration in progress",
			desc: healthDescription(
				elbv2types.TargetHealthReasonEnumDeregistrationInProgress),
			want: false,
		},
		{
			name: "not in use counts present",
			desc: healthDescription(elbv2types.TargetHealthReasonEnumNotInUse),
			want: true,
		},
		{
			name: "healthy with no reason counts present",
			desc: healthDescription(""),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, targetHealthDescriptionEligible(tt.desc))
		})
	}
}

func TestEligibleTargetHealthDescriptions(t *testing.T) {
	descriptions := []elbv2types.TargetHealthDescription{
		{},
		healthDescription(elbv2types.TargetHealthReasonEnumNotRegistered),
		healthDescription(elbv2types.TargetHealthReasonEnumNotInUse),
		healthDescription(""),
	}

	got := eligibleTargetHealthDescriptions(descriptions)
	assert.Equal(t, []elbv2types.TargetHealthDescription{
		healthDescription(elbv2types.TargetHealthReasonEnumNotInUse),
		healthDescription(""),
	}, got)
}

func TestSelectedTargetHealthDescription(t *testing.T) {
	tests := []struct {
		name        string
		resp        *elbv2.DescribeTargetHealthOutput
		wantID      string
		wantMissing bool
		wantError   string
	}{
		{
			name:        "nil response",
			resp:        nil,
			wantMissing: true,
		},
		{
			name:        "empty response",
			resp:        targetHealthOutput(),
			wantMissing: true,
		},
		{
			name: "only ineligible rows",
			resp: targetHealthOutput(
				healthDescription(elbv2types.TargetHealthReasonEnumNotRegistered),
			),
			wantMissing: true,
		},
		{
			name: "more than one eligible row",
			resp: targetHealthOutput(
				healthDescriptionWithTarget("", "10.20.1.50"),
				healthDescriptionWithTarget(elbv2types.TargetHealthReasonEnumNotInUse,
					"10.20.2.50"),
			),
			wantMissing: true,
		},
		{
			name:      "eligible row with nil target is fatal",
			resp:      targetHealthOutput(healthDescription("")),
			wantError: "target health description has nil target",
		},
		{
			name: "single eligible row",
			resp: targetHealthOutput(
				healthDescriptionWithTarget(elbv2types.TargetHealthReasonEnumNotInUse,
					"10.20.1.50"),
			),
			wantID: "10.20.1.50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectedTargetHealthDescription(tt.resp)
			if tt.wantMissing {
				require.ErrorIs(t, err, runtime.ErrNotFound)
				return
			}
			if tt.wantError != "" {
				require.Error(t, err)
				assert.False(t, errors.Is(err, runtime.ErrNotFound))
				assert.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got.Target)
			assert.Equal(t, tt.wantID, aws.ToString(got.Target.Id))
		})
	}
}

func TestValidARN(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want bool
	}{
		{
			name: "empty value is not checked",
			arn:  "",
			want: true,
		},
		{
			name: "target group ARN",
			arn:  "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/tg/abc",
			want: true,
		},
		{
			name: "gov partition",
			arn:  "arn:aws-us-gov:iam::aws:policy/AdministratorAccess",
			want: true,
		},
		{
			name: "empty account",
			arn:  "arn:aws:s3:us-east-1::bucket/example",
			want: true,
		},
		{
			name: "cw account",
			arn:  "arn:aws:logs:us-east-1:cw1234567890:log-group/example",
			want: true,
		},
		{
			name: "empty service is generic valid ARN",
			arn:  "arn:aws::us-east-1:123456789012:thing",
			want: true,
		},
		{
			name: "missing ARN prefix",
			arn:  "elasticloadbalancing:us-east-1:123456789012:targetgroup/tg/abc",
			want: false,
		},
		{
			name: "missing partition",
			arn:  "arn::elasticloadbalancing:us-east-1:123456789012:targetgroup/tg/abc",
			want: false,
		},
		{
			name: "invalid partition",
			arn:  "arn:aws123:elasticloadbalancing:us-east-1:123456789012:targetgroup/tg/abc",
			want: false,
		},
		{
			name: "invalid region",
			arn:  "arn:aws:elasticloadbalancing:useast1:123456789012:targetgroup/tg/abc",
			want: false,
		},
		{
			name: "invalid account",
			arn:  "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/tg/abc",
			want: false,
		},
		{
			name: "missing resource",
			arn:  "arn:aws:elasticloadbalancing:us-east-1:123456789012:",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validARN(tt.arn))
		})
	}
}

func healthDescription(
	reason elbv2types.TargetHealthReasonEnum,
) elbv2types.TargetHealthDescription {
	return elbv2types.TargetHealthDescription{
		TargetHealth: &elbv2types.TargetHealth{Reason: reason},
	}
}

func healthDescriptionWithTarget(
	reason elbv2types.TargetHealthReasonEnum, id string,
) elbv2types.TargetHealthDescription {
	desc := healthDescription(reason)
	desc.Target = &elbv2types.TargetDescription{Id: aws.String(id)}
	return desc
}

func targetHealthOutput(
	descs ...elbv2types.TargetHealthDescription,
) *elbv2.DescribeTargetHealthOutput {
	return &elbv2.DescribeTargetHealthOutput{TargetHealthDescriptions: descs}
}
