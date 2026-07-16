package sfn

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateMachineReplacementFields(t *testing.T) {
	assert.Equal(t, []string{"name", "type"}, (&StateMachineResource{}).ReplaceFields())
}

func TestStateMachineValidateInputsAcceptsBoundaries(t *testing.T) {
	tags := map[string]string{
		"Owner Name": "José / platform+ops@example.com",
	}
	destination := "arn:aws:logs:us-east-1:123456789012:log-group:workflow:*"
	resource := baseStateMachine()
	resource.Name = aws.String(strings.Repeat("a", 80))
	resource.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
		Type:                         "CUSTOMER_MANAGED_KMS_KEY",
		KMSKeyID:                     aws.String(strings.Repeat("k", 2048)),
		KMSDataKeyReusePeriodSeconds: aws.Int64(900),
	}
	resource.LoggingConfiguration = &StateMachineLoggingConfiguration{
		IncludeExecutionData: aws.Bool(true),
		Level:                aws.String("ALL"),
		LogDestination:       &destination,
	}
	resource.TracingConfiguration = &StateMachineTracingConfiguration{Enabled: aws.Bool(false)}
	resource.Tags = &tags

	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestStateMachineValidateInputsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*StateMachineResource)
		match  string
	}{
		{
			name: "empty definition",
			mutate: func(r *StateMachineResource) {
				r.Definition = ""
			},
			match: "definition",
		},
		{
			name: "long definition",
			mutate: func(r *StateMachineResource) {
				r.Definition = strings.Repeat("x", 1_048_577)
			},
			match: "definition",
		},
		{
			name: "empty explicit name",
			mutate: func(r *StateMachineResource) {
				r.Name = aws.String("")
			},
			match: "name",
		},
		{
			name: "invalid name characters",
			mutate: func(r *StateMachineResource) {
				r.Name = aws.String("workflow/name")
			},
			match: "name",
		},
		{
			name: "invalid type",
			mutate: func(r *StateMachineResource) {
				r.Type = "OTHER"
			},
			match: "type",
		},
		{
			name: "invalid role arn",
			mutate: func(r *StateMachineResource) {
				r.RoleARN = "not-an-arn"
			},
			match: "role-arn",
		},
		{
			name: "long role arn",
			mutate: func(r *StateMachineResource) {
				r.RoleARN = "arn:aws:iam::123456789012:role/" + strings.Repeat("r", 300)
			},
			match: "role-arn",
		},
		{
			name: "logging enum",
			mutate: func(r *StateMachineResource) {
				r.LoggingConfiguration = &StateMachineLoggingConfiguration{
					Level: aws.String("DEBUG"),
				}
			},
			match: "level",
		},
		{
			name: "logging destination required",
			mutate: func(r *StateMachineResource) {
				r.LoggingConfiguration = &StateMachineLoggingConfiguration{
					Level: aws.String("ERROR"),
				}
			},
			match: "log-destination",
		},
		{
			name: "long logging destination",
			mutate: func(r *StateMachineResource) {
				r.LoggingConfiguration = &StateMachineLoggingConfiguration{
					Level: aws.String("ERROR"),
					LogDestination: aws.String(
						"arn:aws:logs:us-east-1:123456789012:log-group:" +
							strings.Repeat("x", 210) + ":*"),
				}
			},
			match: "256",
		},
		{
			name: "logging destination suffix",
			mutate: func(r *StateMachineResource) {
				r.LoggingConfiguration = &StateMachineLoggingConfiguration{
					Level: aws.String("FATAL"),
					LogDestination: aws.String(
						"arn:aws:logs:us-east-1:123456789012:log-group:workflow"),
				}
			},
			match: "log-destination",
		},
		{
			name: "encryption enum",
			mutate: func(r *StateMachineResource) {
				r.EncryptionConfiguration = &StateMachineEncryptionConfiguration{Type: "OTHER"}
			},
			match: "encryption-configuration type",
		},
		{
			name: "customer key required",
			mutate: func(r *StateMachineResource) {
				r.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
					Type: "CUSTOMER_MANAGED_KMS_KEY",
				}
			},
			match: "kms-key-id",
		},
		{
			name: "owned key forbids kms members",
			mutate: func(r *StateMachineResource) {
				r.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
					Type:     "AWS_OWNED_KEY",
					KMSKeyID: aws.String("alias/key"),
				}
			},
			match: "AWS_OWNED_KEY",
		},
		{
			name: "reuse period minimum",
			mutate: func(r *StateMachineResource) {
				r.EncryptionConfiguration = &StateMachineEncryptionConfiguration{
					Type:                         "CUSTOMER_MANAGED_KMS_KEY",
					KMSKeyID:                     aws.String("alias/key"),
					KMSDataKeyReusePeriodSeconds: aws.Int64(59),
				}
			},
			match: "reuse-period",
		},
		{
			name: "too many tags",
			mutate: func(r *StateMachineResource) {
				tags := make(map[string]string, 51)
				for i := range 51 {
					tags[string(rune('A'+i))] = "value"
				}
				r.Tags = &tags
			},
			match: "50",
		},
		{
			name: "empty tag key",
			mutate: func(r *StateMachineResource) {
				tags := map[string]string{"": "value"}
				r.Tags = &tags
			},
			match: "1 to 128",
		},
		{
			name: "long tag key",
			mutate: func(r *StateMachineResource) {
				tags := map[string]string{strings.Repeat("k", 129): "value"}
				r.Tags = &tags
			},
			match: "1 to 128",
		},
		{
			name: "long tag value",
			mutate: func(r *StateMachineResource) {
				tags := map[string]string{"key": strings.Repeat("v", 257)}
				r.Tags = &tags
			},
			match: "256",
		},
		{
			name: "reserved tag key",
			mutate: func(r *StateMachineResource) {
				tags := map[string]string{"aws:owner": "user"}
				r.Tags = &tags
			},
			match: "aws:",
		},
		{
			name: "reserved tag value",
			mutate: func(r *StateMachineResource) {
				tags := map[string]string{"owner": "aws:managed"}
				r.Tags = &tags
			},
			match: "aws:",
		},
		{
			name: "invalid tag characters",
			mutate: func(r *StateMachineResource) {
				tags := map[string]string{"owner?": "user"}
				r.Tags = &tags
			},
			match: "characters",
		},
		{
			name: "invalid tag value characters",
			mutate: func(r *StateMachineResource) {
				tags := map[string]string{"owner": "user?"}
				r.Tags = &tags
			},
			match: "characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := baseStateMachine()
			tt.mutate(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.match)
		})
	}
}

func TestStateMachineModifyResourcePlan(t *testing.T) {
	base := baseStateMachine()
	tags := map[string]string{"owner": "one"}
	base.Tags = &tags
	priorOutput := &StateMachineResourceOutput{ARN: testStateMachineARN, RevisionID: "revision-1"}

	tests := []struct {
		name    string
		mutate  func(*StateMachineResource)
		unknown bool
	}{
		{
			name: "definition update",
			mutate: func(r *StateMachineResource) {
				r.Definition = `{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`
			},
			unknown: true,
		},
		{
			name: "role update",
			mutate: func(r *StateMachineResource) {
				r.RoleARN = "arn:aws:iam::123456789012:role/other"
			},
			unknown: true,
		},
		{
			name: "configuration removal",
			mutate: func(r *StateMachineResource) {
				r.TracingConfiguration = nil
			},
			unknown: true,
		},
		{
			name: "tag update",
			mutate: func(r *StateMachineResource) {
				updated := map[string]string{"owner": "two"}
				r.Tags = &updated
			},
		},
		{
			name: "replacement",
			mutate: func(r *StateMachineResource) {
				r.Name = aws.String("replacement")
			},
		},
		{name: "no change", mutate: func(*StateMachineResource) {}},
	}

	base.TracingConfiguration = &StateMachineTracingConfiguration{Enabled: aws.Bool(true)}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := base
			tt.mutate(&current)
			var response runtime.ResourcePlanResponse
			err := current.ModifyResourcePlan(runtime.ResourcePlanRequest[
				StateMachineResource, *StateMachineResourceOutput, *awsCfg,
			]{
				PriorInputs:   base,
				CurrentInputs: current,
				PriorOutputs:  priorOutput,
				HasPriorState: true,
			}, &response)
			require.NoError(t, err)
			assert.Equal(t, tt.unknown, response.UnknownOutputs["revision-id"])
		})
	}
}

func TestStateMachineModifyResourcePlanWithoutPriorState(t *testing.T) {
	resource := baseStateMachine()
	var response runtime.ResourcePlanResponse
	err := resource.ModifyResourcePlan(runtime.ResourcePlanRequest[
		StateMachineResource, *StateMachineResourceOutput, *awsCfg,
	]{CurrentInputs: resource}, &response)
	require.NoError(t, err)
	assert.Empty(t, response.UnknownOutputs)
}
