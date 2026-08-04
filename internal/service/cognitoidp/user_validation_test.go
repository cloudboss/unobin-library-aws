package cognitoidp

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserValidateInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*UserResource)
		want   string
	}{
		{
			name: "missing pool",
			mutate: func(r *UserResource) {
				r.UserPoolID = ""
			},
			want: "user-pool-id",
		},
		{
			name: "missing username",
			mutate: func(r *UserResource) {
				r.Username = ""
			},
			want: "username",
		},
		{
			name: "long username",
			mutate: func(r *UserResource) {
				r.Username = strings.Repeat("a", 129)
			},
			want: "username",
		},
		{
			name: "password conflict",
			mutate: func(r *UserResource) {
				r.Password = aws.String("Secret123!")
				r.TemporaryPassword = aws.String("Temp123!")
			},
			want: "conflict",
		},
		{
			name: "short password",
			mutate: func(r *UserResource) {
				r.Password = aws.String("short")
			},
			want: "password",
		},
		{
			name: "long temporary password",
			mutate: func(r *UserResource) {
				r.TemporaryPassword = aws.String(strings.Repeat("x", 257))
			},
			want: "temporary-password",
		},
		{
			name: "delivery enum",
			mutate: func(r *UserResource) {
				r.DesiredDeliveryMediums = stringSlice("PUSH")
			},
			want: "desired-delivery-mediums",
		},
		{
			name: "message enum",
			mutate: func(r *UserResource) {
				r.MessageAction = aws.String("SEND")
			},
			want: "message-action",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validUserResource()
			tt.mutate(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestUserValidateInputsAcceptsBoundaries(t *testing.T) {
	resource := validUserResource()
	resource.Password = aws.String(strings.Repeat("p", 6))
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))

	resource.Password = aws.String(strings.Repeat("p", 256))
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func validUserResource() UserResource {
	return UserResource{UserPoolID: "us-east-1_pool", Username: "alice"}
}
