package cloudwatchlogs

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResourcePolicyDocument(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "compacts insignificant whitespace",
			input: " { \"Statement\" : [ { \"Effect\" : \"Allow\" } ] } ",
			want:  `{"Statement":[{"Effect":"Allow"}]}`,
		},
		{
			name:    "rejects empty document",
			input:   " \t\n ",
			wantErr: true,
		},
		{
			name:    "rejects invalid JSON",
			input:   `{"Statement":`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeResourcePolicyDocument(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourcePolicyValidateInputs(t *testing.T) {
	tests := []struct {
		name           string
		policyDocument string
		policyName     *string
		resourceArn    *string
		wantErrMsg     string
	}{
		{
			name:           "account scoped policy",
			policyDocument: `{}`,
			policyName:     aws.String("new-policy"),
		},
		{
			name:           "resource scoped policy",
			policyDocument: `{}`,
			resourceArn:    aws.String("arn:aws:logs:us-east-1:123456789012:log-group:x"),
		},
		{
			name:           "invalid policy document",
			policyDocument: `{"Statement":`,
			policyName:     aws.String("new-policy"),
			wantErrMsg:     "policy-document must be valid JSON",
		},
		{
			name:           "missing identity",
			policyDocument: `{}`,
			wantErrMsg:     "exactly one of policy-name or resource-arn is required",
		},
		{
			name:           "conflicting identities",
			policyDocument: `{}`,
			policyName:     aws.String("new-policy"),
			resourceArn:    aws.String("arn:aws:logs:us-east-1:123456789012:log-group:x"),
			wantErrMsg:     "exactly one of policy-name or resource-arn is required",
		},
		{
			name:           "invalid resource arn",
			policyDocument: `{}`,
			resourceArn:    aws.String("arn:aws:logs:us-east-1:not-valid:log-group:x"),
			wantErrMsg:     "resource-arn must be a valid ARN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ResourcePolicy{
				PolicyDocument: tt.policyDocument,
				PolicyName:     tt.policyName,
				ResourceArn:    tt.resourceArn,
			}

			err := r.ValidateInputs(context.Background(), nil)

			if tt.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDeleteResourcePolicyInput(t *testing.T) {
	tests := []struct {
		name       string
		prior      *ResourcePolicyOutput
		wantName   string
		wantArn    string
		wantRev    string
		wantErrMsg string
	}{
		{
			name:     "account scoped policy",
			prior:    &ResourcePolicyOutput{PolicyName: aws.String("account-policy")},
			wantName: "account-policy",
		},
		{
			name: "resource scoped policy",
			prior: &ResourcePolicyOutput{
				ResourceArn: aws.String("arn:aws:logs:us-east-1:123456789012:log-group:x"),
				RevisionId:  aws.String("rev-1"),
			},
			wantArn: "arn:aws:logs:us-east-1:123456789012:log-group:x",
			wantRev: "rev-1",
		},
		{
			name: "resource scoped policy requires revision",
			prior: &ResourcePolicyOutput{
				ResourceArn: aws.String("arn:aws:logs:us-east-1:123456789012:log-group:x"),
			},
			wantErrMsg: "revision-id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deleteResourcePolicyInput(tt.prior)
			if tt.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, aws.ToString(got.PolicyName))
			assert.Equal(t, tt.wantArn, aws.ToString(got.ResourceArn))
			assert.Equal(t, tt.wantRev, aws.ToString(got.ExpectedRevisionId))
		})
	}
}

func TestValidResourcePolicyARN(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want bool
	}{
		{
			name: "logs arn",
			arn:  "arn:aws:logs:us-east-1:123456789012:log-group:example",
			want: true,
		},
		{
			name: "empty region and account are allowed",
			arn:  "arn:aws:s3:::bucket-name",
			want: true,
		},
		{
			name: "partition suffix is allowed",
			arn:  "arn:aws-us-gov:logs:us-gov-west-1:123456789012:log-group:example",
			want: true,
		},
		{
			name: "rejects malformed arn",
			arn:  "not-an-arn",
		},
		{
			name: "rejects invalid partition",
			arn:  "arn:aws1:logs:us-east-1:123456789012:log-group:example",
		},
		{
			name: "rejects invalid region",
			arn:  "arn:aws:logs:us_east_1:123456789012:log-group:example",
		},
		{
			name: "rejects invalid account",
			arn:  "arn:aws:logs:us-east-1:abc:log-group:example",
		},
		{
			name: "rejects empty resource",
			arn:  "arn:aws:logs:us-east-1:123456789012:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validResourcePolicyARN(tt.arn))
		})
	}

	genericAccounts := []string{
		"aws",
		"aws-managed",
		"third-party",
		"aws-marketplace",
		"partner-managed",
		"cw1234567890",
	}
	for _, account := range genericAccounts {
		t.Run("generic account "+account, func(t *testing.T) {
			arn := fmt.Sprintf("arn:aws:logs:us-east-1:%s:log-group:example", account)
			assert.True(t, validResourcePolicyARN(arn))
		})
	}
}
