package cloudwatchlogs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
)

func TestSubscriptionFilterReplaceFields(t *testing.T) {
	r := &SubscriptionFilter{}
	assert.Equal(t, []string{"destination-arn", "log-group-name", "name"},
		r.ReplaceFields())
}

func TestSubscriptionFilterPutInput(t *testing.T) {
	roleArn := "arn:aws:iam::123456789012:role/logs"
	criteria := "@aws.region = \"us-east-1\""
	applyOnTransformedLogs := false
	r := &SubscriptionFilter{
		DestinationArn:         "arn:aws:lambda:us-east-1:123456789012:function:fn",
		LogGroupName:           "group",
		Name:                   "filter",
		FilterPattern:          "",
		Distribution:           distributionRandom,
		EmitSystemFields:       &[]string{"@aws.region", "@aws.account", "@aws.region"},
		FieldSelectionCriteria: &criteria,
		RoleArn:                &roleArn,
		ApplyOnTransformedLogs: &applyOnTransformedLogs,
	}

	got := r.putInput()

	assert.Equal(t, r.DestinationArn, aws.ToString(got.DestinationArn))
	assert.Equal(t, r.LogGroupName, aws.ToString(got.LogGroupName))
	assert.Equal(t, r.Name, aws.ToString(got.FilterName))
	assert.Equal(t, r.FilterPattern, aws.ToString(got.FilterPattern))
	assert.Equal(t, distributionRandom, string(got.Distribution))
	assert.Equal(t, []string{"@aws.account", "@aws.region"}, got.EmitSystemFields)
	assert.Equal(t, criteria, aws.ToString(got.FieldSelectionCriteria))
	assert.Equal(t, roleArn, aws.ToString(got.RoleArn))
	assert.False(t, got.ApplyOnTransformedLogs)
}

func TestSubscriptionFilterPutInputDefaults(t *testing.T) {
	r := &SubscriptionFilter{
		DestinationArn: "arn:aws:lambda:us-east-1:123456789012:function:fn",
		LogGroupName:   "group",
		Name:           "filter",
		FilterPattern:  "",
	}

	got := r.putInput()

	assert.Equal(t, distributionByLogStream, string(got.Distribution))
	assert.Nil(t, got.EmitSystemFields)
	assert.Nil(t, got.RoleArn)
	assert.False(t, got.ApplyOnTransformedLogs)
}

func TestSubscriptionFilterMutableInputChanged(t *testing.T) {
	empty := ""
	tests := []struct {
		name    string
		prior   SubscriptionFilter
		current SubscriptionFilter
		want    bool
	}{
		{
			name: "effective defaults and set order match",
			prior: SubscriptionFilter{
				FilterPattern:    "",
				EmitSystemFields: &[]string{"@aws.region", "@aws.account"},
			},
			current: SubscriptionFilter{
				FilterPattern:    "",
				Distribution:     distributionByLogStream,
				EmitSystemFields: &[]string{"@aws.account", "@aws.region", "@aws.account"},
				RoleArn:          &empty,
			},
			want: false,
		},
		{
			name: "filter pattern changes",
			prior: SubscriptionFilter{
				FilterPattern: "",
			},
			current: SubscriptionFilter{
				FilterPattern: "{ $.level = \"info\" }",
			},
			want: true,
		},
		{
			name: "system field set changes",
			prior: SubscriptionFilter{
				EmitSystemFields: &[]string{"@aws.account"},
			},
			current: SubscriptionFilter{
				EmitSystemFields: &[]string{"@aws.region"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.current.mutableInputChanged(tt.prior))
		})
	}
}

func TestSubscriptionFilterManagedOutputDrifted(t *testing.T) {
	roleArn := "arn:aws:iam::123456789012:role/logs"
	otherRoleArn := "arn:aws:iam::123456789012:role/other"
	applyOnTransformedLogs := false
	tests := []struct {
		name     string
		current  SubscriptionFilter
		observed *SubscriptionFilterOutput
		want     bool
	}{
		{
			name:    "unmanaged role value only refreshes state",
			current: SubscriptionFilter{},
			observed: &SubscriptionFilterOutput{
				RoleArn: &roleArn,
			},
			want: false,
		},
		{
			name: "managed role differs",
			current: SubscriptionFilter{
				RoleArn: &roleArn,
			},
			observed: &SubscriptionFilterOutput{
				RoleArn: &otherRoleArn,
			},
			want: true,
		},
		{
			name: "managed transformed-log flag differs",
			current: SubscriptionFilter{
				ApplyOnTransformedLogs: &applyOnTransformedLogs,
			},
			observed: &SubscriptionFilterOutput{ApplyOnTransformedLogs: true},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.current.managedOutputDrifted(tt.observed))
		})
	}
}

func TestSubscriptionFilterShouldPut(t *testing.T) {
	roleArn := "arn:aws:iam::123456789012:role/logs"
	r := &SubscriptionFilter{RoleArn: &roleArn}
	prior := runtime.Prior[SubscriptionFilter, *SubscriptionFilterOutput]{
		Inputs: SubscriptionFilter{RoleArn: &roleArn},
		Observed: &SubscriptionFilterOutput{
			RoleArn: aws.String("arn:aws:iam::123456789012:role/other"),
		},
	}

	assert.True(t, r.shouldPut(prior))
}

func TestSubscriptionFilterDeleteValidatesDesiredInputBeforeClient(t *testing.T) {
	poisonCfg := &awsCfg{
		AssumeRole:                &awscfg.AssumeRole{},
		AssumeRoleWithWebIdentity: &awscfg.AssumeRoleWithWebIdentity{},
	}
	valid := SubscriptionFilter{
		DestinationArn: "arn:aws:lambda:us-east-1:123456789012:function:fn",
		LogGroupName:   "new-group",
		Name:           "new-filter",
		FilterPattern:  "",
	}
	prior := &SubscriptionFilterOutput{
		LogGroupName: "old-group",
		Name:         "old-filter",
	}
	tests := []struct {
		name    string
		mutate  func(*SubscriptionFilter)
		wantErr string
	}{
		{
			name: "invalid name",
			mutate: func(r *SubscriptionFilter) {
				r.Name = ""
			},
			wantErr: "name must be 1 to 512 bytes",
		},
		{
			name: "invalid destination ARN",
			mutate: func(r *SubscriptionFilter) {
				r.DestinationArn = "not-an-arn"
			},
			wantErr: "destination-arn must be a valid ARN",
		},
		{
			name: "long filter pattern",
			mutate: func(r *SubscriptionFilter) {
				r.FilterPattern = strings.Repeat("x", 1025)
			},
			wantErr: "filter-pattern must be at most 1024 bytes",
		},
		{
			name: "long field selection criteria",
			mutate: func(r *SubscriptionFilter) {
				criteria := strings.Repeat("x", 2001)
				r.FieldSelectionCriteria = &criteria
			},
			wantErr: "field-selection-criteria must be at most 2000 bytes",
		},
		{
			name: "invalid role ARN",
			mutate: func(r *SubscriptionFilter) {
				roleArn := "arn:aws:iam::123:role/logs"
				r.RoleArn = &roleArn
			},
			wantErr: "role-arn must be a valid ARN",
		},
		{
			name: "invalid distribution",
			mutate: func(r *SubscriptionFilter) {
				r.Distribution = "Nearest"
			},
			wantErr: "distribution must be ByLogStream or Random",
		},
		{
			name: "invalid system field",
			mutate: func(r *SubscriptionFilter) {
				r.EmitSystemFields = &[]string{"@aws.partition"}
			},
			wantErr: "emit-system-fields entry \"@aws.partition\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid
			tt.mutate(&r)

			err := r.Delete(context.Background(), poisonCfg, prior)

			if assert.ErrorContains(t, err, tt.wantErr) {
				assert.NotContains(t, err.Error(), "assume-role")
			}
		})
	}
}

func TestSubscriptionFilterValidate(t *testing.T) {
	valid := SubscriptionFilter{
		DestinationArn: "arn:aws:lambda:us-east-1:123456789012:function:fn",
		LogGroupName:   "group",
		Name:           "filter",
		FilterPattern:  "",
	}
	tests := []struct {
		name    string
		mutate  func(*SubscriptionFilter)
		wantErr string
	}{
		{
			name: "valid",
		},
		{
			name: "empty name",
			mutate: func(r *SubscriptionFilter) {
				r.Name = ""
			},
			wantErr: "name must be 1 to 512 bytes",
		},
		{
			name: "multibyte name boundary",
			mutate: func(r *SubscriptionFilter) {
				r.Name = strings.Repeat("\u00e9", 256)
			},
		},
		{
			name: "multibyte name too long",
			mutate: func(r *SubscriptionFilter) {
				r.Name = strings.Repeat("\u00e9", 257)
			},
			wantErr: "name must be 1 to 512 bytes",
		},
		{
			name: "long filter pattern",
			mutate: func(r *SubscriptionFilter) {
				r.FilterPattern = strings.Repeat("x", 1025)
			},
			wantErr: "filter-pattern must be at most 1024 bytes",
		},
		{
			name: "multibyte filter pattern boundary",
			mutate: func(r *SubscriptionFilter) {
				r.FilterPattern = strings.Repeat("\u00e9", 512)
			},
		},
		{
			name: "multibyte filter pattern too long",
			mutate: func(r *SubscriptionFilter) {
				r.FilterPattern = strings.Repeat("\u00e9", 513)
			},
			wantErr: "filter-pattern must be at most 1024 bytes",
		},
		{
			name: "invalid destination ARN",
			mutate: func(r *SubscriptionFilter) {
				r.DestinationArn = "not-an-arn"
			},
			wantErr: "destination-arn must be a valid ARN",
		},
		{
			name: "long field selection criteria",
			mutate: func(r *SubscriptionFilter) {
				criteria := strings.Repeat("x", 2001)
				r.FieldSelectionCriteria = &criteria
			},
			wantErr: "field-selection-criteria must be at most 2000 bytes",
		},
		{
			name: "invalid role ARN",
			mutate: func(r *SubscriptionFilter) {
				roleArn := "arn:aws:iam::123:role/logs"
				r.RoleArn = &roleArn
			},
			wantErr: "role-arn must be a valid ARN",
		},
		{
			name: "invalid distribution",
			mutate: func(r *SubscriptionFilter) {
				r.Distribution = "Nearest"
			},
			wantErr: "distribution must be ByLogStream or Random",
		},
		{
			name: "invalid system field",
			mutate: func(r *SubscriptionFilter) {
				r.EmitSystemFields = &[]string{"@aws.partition"}
			},
			wantErr: "emit-system-fields entry \"@aws.partition\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			err := r.validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidSubscriptionFilterARN(t *testing.T) {
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
			name: "lambda ARN",
			arn:  "arn:aws:lambda:us-east-1:123456789012:function:fn",
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
			name: "missing prefix",
			arn:  "lambda:us-east-1:123456789012:function:fn",
			want: false,
		},
		{
			name: "missing partition",
			arn:  "arn::lambda:us-east-1:123456789012:function:fn",
			want: false,
		},
		{
			name: "invalid partition",
			arn:  "arn:aws123:lambda:us-east-1:123456789012:function:fn",
			want: false,
		},
		{
			name: "invalid region",
			arn:  "arn:aws:lambda:useast1:123456789012:function:fn",
			want: false,
		},
		{
			name: "invalid account",
			arn:  "arn:aws:lambda:us-east-1:123:function:fn",
			want: false,
		},
		{
			name: "missing resource",
			arn:  "arn:aws:lambda:us-east-1:123456789012:",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validSubscriptionFilterARN(tt.arn))
		})
	}
}

func TestPutSubscriptionFilterRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "destination propagation",
			err: &cloudwatchlogstypes.InvalidParameterException{
				Message: aws.String("Could not deliver test message to specified destination"),
			},
			want: true,
		},
		{
			name: "lambda invoke propagation",
			err: &cloudwatchlogstypes.InvalidParameterException{
				Message: aws.String("Could not execute the lambda function"),
			},
			want: true,
		},
		{
			name: "concurrent update",
			err: &cloudwatchlogstypes.OperationAbortedException{
				Message: aws.String("Please try again"),
			},
			want: true,
		},
		{
			name: "role propagation",
			err: &cloudwatchlogstypes.ValidationException{
				Message: aws.String("Make sure you have given CloudWatch Logs permission " +
					"to assume the provided role"),
			},
			want: true,
		},
		{
			name: "wrapped role propagation",
			err: fmt.Errorf("put: %w", &cloudwatchlogstypes.ValidationException{
				Message: aws.String("Make sure you have given CloudWatch Logs permission " +
					"to assume the provided role"),
			}),
			want: true,
		},
		{
			name: "other invalid parameter",
			err: &cloudwatchlogstypes.InvalidParameterException{
				Message: aws.String("bad pattern"),
			},
			want: false,
		},
		{
			name: "ordinary error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPutSubscriptionFilterRetryable(tt.err))
		})
	}
}
