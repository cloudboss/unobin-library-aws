package secretsmanager

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	secretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretVersionDataGetInput(t *testing.T) {
	versionID := "version-1"
	customStage := "AWSPREVIOUS"
	empty := ""
	tests := []struct {
		name             string
		data             SecretVersionData
		wantVersionID    *string
		wantVersionStage *string
	}{
		{
			name:             "default stage",
			data:             SecretVersionData{SecretId: "secret"},
			wantVersionStage: aws.String(currentStage),
		},
		{
			name: "custom stage",
			data: SecretVersionData{
				SecretId:     "secret",
				VersionStage: &customStage,
			},
			wantVersionStage: &customStage,
		},
		{
			name: "version id wins",
			data: SecretVersionData{
				SecretId:     "secret",
				VersionId:    &versionID,
				VersionStage: &customStage,
			},
			wantVersionID: &versionID,
		},
		{
			name: "empty version id uses stage",
			data: SecretVersionData{
				SecretId:     "secret",
				VersionId:    &empty,
				VersionStage: &customStage,
			},
			wantVersionStage: &customStage,
		},
		{
			name: "empty stage defaults current",
			data: SecretVersionData{
				SecretId:     "secret",
				VersionStage: &empty,
			},
			wantVersionStage: aws.String(currentStage),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.data.getInput()
			require.NotNil(t, got)
			assert.Equal(t, "secret", aws.ToString(got.SecretId))
			assert.Equal(t, aws.ToString(tt.wantVersionID), aws.ToString(got.VersionId))
			assert.Equal(t, aws.ToString(tt.wantVersionStage), aws.ToString(got.VersionStage))
			assert.Equal(t, tt.wantVersionID == nil, got.VersionId == nil)
			assert.Equal(t, tt.wantVersionStage == nil, got.VersionStage == nil)
		})
	}
}

func TestSecretVersionDataOutput(t *testing.T) {
	created := time.Date(2026, 7, 5, 12, 13, 14, 0, time.UTC)
	out, err := secretVersionDataOutput(&secretsmanager.GetSecretValueOutput{
		ARN:           aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:s"),
		CreatedDate:   &created,
		Name:          aws.String("s"),
		SecretBinary:  []byte("binary-value"),
		SecretString:  aws.String("string-value"),
		VersionId:     aws.String("version-1"),
		VersionStages: []string{"beta", "", "alpha", "beta"},
	})
	require.NoError(t, err)
	assert.Equal(t, &SecretVersionDataOutput{
		Arn:           "arn:aws:secretsmanager:us-east-1:123456789012:secret:s",
		CreatedDate:   "2026-07-05T12:13:14Z",
		Name:          "s",
		SecretBinary:  "binary-value",
		SecretString:  "string-value",
		VersionId:     "version-1",
		VersionStages: []string{"alpha", "beta"},
	}, out)
}

func TestSecretVersionDataOutputEmpty(t *testing.T) {
	out, err := secretVersionDataOutput(&secretsmanager.GetSecretValueOutput{})
	assert.Nil(t, out)
	assert.ErrorIs(t, err, errSecretVersionDataNotFound)
}

func TestIsSecretVersionDataNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "empty output sentinel",
			err:  errSecretVersionDataNotFound,
			want: true,
		},
		{
			name: "resource not found",
			err:  &secretsmanagertypes.ResourceNotFoundException{},
			want: true,
		},
		{
			name: "deleted invalid request",
			err: &secretsmanagertypes.InvalidRequestException{
				Message: aws.String("secret because it was deleted"),
			},
			want: true,
		},
		{
			name: "marked for deletion invalid request",
			err: &secretsmanagertypes.InvalidRequestException{
				Message: aws.String("secret because it was marked for deletion"),
			},
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("boom"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSecretVersionDataNotFound(tt.err))
		})
	}
}
