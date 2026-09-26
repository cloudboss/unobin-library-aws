package cognitoidp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCognitoClient struct {
	calls []string

	addInput          *cognitoidentityprovider.AddCustomAttributesInput
	createInput       *cognitoidentityprovider.CreateUserPoolInput
	deleteContext     context.Context
	deleteContextErr  error
	deleteDeadline    time.Time
	deleteHasDeadline bool
	deleteInput       *cognitoidentityprovider.DeleteUserPoolInput
	describeInput     *cognitoidentityprovider.DescribeUserPoolInput
	getMFAInput       *cognitoidentityprovider.GetUserPoolMfaConfigInput
	listTagsInput     *cognitoidentityprovider.ListTagsForResourceInput
	setMFAInput       *cognitoidentityprovider.SetUserPoolMfaConfigInput
	tagInput          *cognitoidentityprovider.TagResourceInput
	untagInput        *cognitoidentityprovider.UntagResourceInput
	updateInput       *cognitoidentityprovider.UpdateUserPoolInput

	addFn func(
		*cognitoidentityprovider.AddCustomAttributesInput,
	) (*cognitoidentityprovider.AddCustomAttributesOutput, error)
	createFn func(
		*cognitoidentityprovider.CreateUserPoolInput,
	) (*cognitoidentityprovider.CreateUserPoolOutput, error)
	deleteFn func(
		*cognitoidentityprovider.DeleteUserPoolInput,
	) (*cognitoidentityprovider.DeleteUserPoolOutput, error)
	describeFn func(
		*cognitoidentityprovider.DescribeUserPoolInput,
	) (*cognitoidentityprovider.DescribeUserPoolOutput, error)
	getMFAFn func(
		*cognitoidentityprovider.GetUserPoolMfaConfigInput,
	) (*cognitoidentityprovider.GetUserPoolMfaConfigOutput, error)
	listTagsFn func(
		*cognitoidentityprovider.ListTagsForResourceInput,
	) (*cognitoidentityprovider.ListTagsForResourceOutput, error)
	setMFAFn func(
		*cognitoidentityprovider.SetUserPoolMfaConfigInput,
	) (*cognitoidentityprovider.SetUserPoolMfaConfigOutput, error)
	tagFn func(
		*cognitoidentityprovider.TagResourceInput,
	) (*cognitoidentityprovider.TagResourceOutput, error)
	untagFn func(
		*cognitoidentityprovider.UntagResourceInput,
	) (*cognitoidentityprovider.UntagResourceOutput, error)
	updateFn func(
		*cognitoidentityprovider.UpdateUserPoolInput,
	) (*cognitoidentityprovider.UpdateUserPoolOutput, error)
}

func (f *fakeCognitoClient) AddCustomAttributes(
	_ context.Context,
	in *cognitoidentityprovider.AddCustomAttributesInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AddCustomAttributesOutput, error) {
	f.calls = append(f.calls, "add-custom-attributes")
	f.addInput = in
	if f.addFn != nil {
		return f.addFn(in)
	}
	return &cognitoidentityprovider.AddCustomAttributesOutput{}, nil
}

func (f *fakeCognitoClient) CreateUserPool(
	_ context.Context,
	in *cognitoidentityprovider.CreateUserPoolInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.CreateUserPoolOutput, error) {
	f.calls = append(f.calls, "create")
	f.createInput = in
	if f.createFn != nil {
		return f.createFn(in)
	}
	return &cognitoidentityprovider.CreateUserPoolOutput{}, nil
}

func (f *fakeCognitoClient) DeleteUserPool(
	ctx context.Context,
	in *cognitoidentityprovider.DeleteUserPoolInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.DeleteUserPoolOutput, error) {
	f.calls = append(f.calls, "delete")
	f.deleteContext = ctx
	f.deleteContextErr = ctx.Err()
	f.deleteDeadline, f.deleteHasDeadline = ctx.Deadline()
	f.deleteInput = in
	if f.deleteFn != nil {
		return f.deleteFn(in)
	}
	return &cognitoidentityprovider.DeleteUserPoolOutput{}, nil
}

func (f *fakeCognitoClient) DescribeUserPool(
	_ context.Context,
	in *cognitoidentityprovider.DescribeUserPoolInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
	f.calls = append(f.calls, "describe")
	f.describeInput = in
	if f.describeFn != nil {
		return f.describeFn(in)
	}
	return &cognitoidentityprovider.DescribeUserPoolOutput{}, nil
}

func (f *fakeCognitoClient) GetUserPoolMfaConfig(
	_ context.Context,
	in *cognitoidentityprovider.GetUserPoolMfaConfigInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.GetUserPoolMfaConfigOutput, error) {
	f.calls = append(f.calls, "get-mfa")
	f.getMFAInput = in
	if f.getMFAFn != nil {
		return f.getMFAFn(in)
	}
	return &cognitoidentityprovider.GetUserPoolMfaConfigOutput{}, nil
}

func (f *fakeCognitoClient) ListTagsForResource(
	_ context.Context,
	in *cognitoidentityprovider.ListTagsForResourceInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.ListTagsForResourceOutput, error) {
	f.calls = append(f.calls, "list-tags")
	f.listTagsInput = in
	if f.listTagsFn != nil {
		return f.listTagsFn(in)
	}
	return &cognitoidentityprovider.ListTagsForResourceOutput{}, nil
}

func (f *fakeCognitoClient) SetUserPoolMfaConfig(
	_ context.Context,
	in *cognitoidentityprovider.SetUserPoolMfaConfigInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.SetUserPoolMfaConfigOutput, error) {
	f.calls = append(f.calls, "set-mfa")
	f.setMFAInput = in
	if f.setMFAFn != nil {
		return f.setMFAFn(in)
	}
	return &cognitoidentityprovider.SetUserPoolMfaConfigOutput{}, nil
}

func (f *fakeCognitoClient) TagResource(
	_ context.Context,
	in *cognitoidentityprovider.TagResourceInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.TagResourceOutput, error) {
	f.calls = append(f.calls, "tag")
	f.tagInput = in
	if f.tagFn != nil {
		return f.tagFn(in)
	}
	return &cognitoidentityprovider.TagResourceOutput{}, nil
}

func (f *fakeCognitoClient) UntagResource(
	_ context.Context,
	in *cognitoidentityprovider.UntagResourceInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.UntagResourceOutput, error) {
	f.calls = append(f.calls, "untag")
	f.untagInput = in
	if f.untagFn != nil {
		return f.untagFn(in)
	}
	return &cognitoidentityprovider.UntagResourceOutput{}, nil
}

func (f *fakeCognitoClient) UpdateUserPool(
	_ context.Context,
	in *cognitoidentityprovider.UpdateUserPoolInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.UpdateUserPoolOutput, error) {
	f.calls = append(f.calls, "update")
	f.updateInput = in
	if f.updateFn != nil {
		return f.updateFn(in)
	}
	return &cognitoidentityprovider.UpdateUserPoolOutput{}, nil
}

func TestUserPoolCreateAppliesMFAAndReadsFinalValue(t *testing.T) {
	createdAt := time.Date(2026, 7, 16, 12, 30, 0, 0, time.FixedZone("test", 3600))
	modifiedAt := createdAt.Add(time.Hour)
	client := &fakeCognitoClient{
		createFn: func(
			*cognitoidentityprovider.CreateUserPoolInput,
		) (*cognitoidentityprovider.CreateUserPoolOutput, error) {
			return &cognitoidentityprovider.CreateUserPoolOutput{
				UserPool: &cognitotypes.UserPoolType{Id: aws.String("created-id")},
			}, nil
		},
		describeFn: func(
			*cognitoidentityprovider.DescribeUserPoolInput,
		) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
			return userPoolDescribeOutput("created-id", createdAt, modifiedAt), nil
		},
	}
	resource := UserPoolResource{
		Name:             "pool-name",
		MFAConfiguration: aws.String("OPTIONAL"),
		EnabledMFAs:      &[]string{"SOFTWARE_TOKEN_MFA"},
	}

	out, err := resource.create(
		context.Background(), client, "us-east-1", &instantUserPoolClock{},
	)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, []string{"create", "set-mfa", "describe", "get-mfa"}, client.calls)
	assert.Empty(t, client.createInput.MfaConfiguration)
	assert.Equal(t, "created-id", aws.ToString(client.setMFAInput.UserPoolId))
	require.NotNil(t, client.setMFAInput.SoftwareTokenMfaConfiguration)
	assert.Equal(t, "created-id", aws.ToString(client.describeInput.UserPoolId))
	assert.Equal(t, "created-id", aws.ToString(client.getMFAInput.UserPoolId))
	assert.Equal(t, "created-id", out.UserPoolID)
	assert.Equal(t, "arn:aws:cognito-idp:us-east-1:123456789012:userpool/created-id", out.ARN)
	assert.Equal(t, "cognito-idp.us-east-1.amazonaws.com/created-id", out.ProviderName)
	assert.Equal(t, "https://cognito-idp.us-east-1.amazonaws.com/created-id", out.ProviderURL)
	assert.Equal(t, "2026-07-16T11:30:00Z", out.CreationDate)
	assert.Equal(t, "2026-07-16T12:30:00Z", out.LastModifiedDate)
}

func TestUserPoolCreateRetriesCreateAndMFA(t *testing.T) {
	createAttempts := 0
	mfaAttempts := 0
	client := &fakeCognitoClient{
		createFn: func(
			*cognitoidentityprovider.CreateUserPoolInput,
		) (*cognitoidentityprovider.CreateUserPoolOutput, error) {
			createAttempts++
			if createAttempts == 1 {
				return nil, retryableUserPoolError()
			}
			return &cognitoidentityprovider.CreateUserPoolOutput{
				UserPool: &cognitotypes.UserPoolType{Id: aws.String("created-id")},
			}, nil
		},
		setMFAFn: func(
			*cognitoidentityprovider.SetUserPoolMfaConfigInput,
		) (*cognitoidentityprovider.SetUserPoolMfaConfigOutput, error) {
			mfaAttempts++
			if mfaAttempts == 1 {
				return nil, retryableUserPoolError()
			}
			return &cognitoidentityprovider.SetUserPoolMfaConfigOutput{}, nil
		},
		describeFn: func(
			*cognitoidentityprovider.DescribeUserPoolInput,
		) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
			return userPoolDescribeOutput("created-id", time.Time{}, time.Time{}), nil
		},
	}
	clock := &instantUserPoolClock{}
	resource := UserPoolResource{
		Name:             "pool-name",
		MFAConfiguration: aws.String("OPTIONAL"),
		EnabledMFAs:      &[]string{"SOFTWARE_TOKEN_MFA"},
	}

	out, err := resource.create(context.Background(), client, "us-east-1", clock)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, 2, createAttempts)
	assert.Equal(t, 2, mfaAttempts)
	assert.Equal(t, []time.Duration{500 * time.Millisecond, 500 * time.Millisecond},
		clock.sleeps)
	assert.Equal(t, []string{
		"create", "create", "set-mfa", "set-mfa", "describe", "get-mfa",
	}, client.calls)
}

func TestUserPoolCreateRejectsMissingID(t *testing.T) {
	tests := map[string]*cognitoidentityprovider.CreateUserPoolOutput{
		"nil response": nil,
		"nil pool":     {},
		"empty id":     {UserPool: &cognitotypes.UserPoolType{}},
	}
	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeCognitoClient{
				createFn: func(
					*cognitoidentityprovider.CreateUserPoolInput,
				) (*cognitoidentityprovider.CreateUserPoolOutput, error) {
					return response, nil
				},
			}
			_, err := (&UserPoolResource{Name: "pool-name"}).create(
				context.Background(), client, "us-east-1", &instantUserPoolClock{},
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, "response has no user pool ID")
			assert.Equal(t, []string{"create"}, client.calls)
		})
	}
}

func TestUserPoolCreateCompensatesPostIDFailures(t *testing.T) {
	primary := errors.New("primary failure")
	tests := []struct {
		name      string
		resource  UserPoolResource
		configure func(*fakeCognitoClient)
		wantCalls []string
		wantError string
	}{
		{
			name: "set mfa",
			resource: UserPoolResource{
				Name:             "pool-name",
				MFAConfiguration: aws.String("OPTIONAL"),
				EnabledMFAs:      &[]string{"SOFTWARE_TOKEN_MFA"},
			},
			configure: func(client *fakeCognitoClient) {
				client.setMFAFn = func(
					*cognitoidentityprovider.SetUserPoolMfaConfigInput,
				) (*cognitoidentityprovider.SetUserPoolMfaConfigOutput, error) {
					return nil, primary
				}
			},
			wantCalls: []string{"create", "set-mfa", "delete"},
			wantError: "set user pool created-id MFA configuration",
		},
		{
			name:     "describe",
			resource: UserPoolResource{Name: "pool-name"},
			configure: func(client *fakeCognitoClient) {
				client.describeFn = func(
					*cognitoidentityprovider.DescribeUserPoolInput,
				) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
					return nil, primary
				}
			},
			wantCalls: []string{"create", "describe", "delete"},
			wantError: "describe user pool created-id",
		},
		{
			name:     "get mfa",
			resource: UserPoolResource{Name: "pool-name"},
			configure: func(client *fakeCognitoClient) {
				client.describeFn = func(
					*cognitoidentityprovider.DescribeUserPoolInput,
				) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
					return userPoolDescribeOutput(
						"created-id", time.Time{}, time.Time{},
					), nil
				}
				client.getMFAFn = func(
					*cognitoidentityprovider.GetUserPoolMfaConfigInput,
				) (*cognitoidentityprovider.GetUserPoolMfaConfigOutput, error) {
					return nil, primary
				}
			},
			wantCalls: []string{"create", "describe", "get-mfa", "delete"},
			wantError: "get user pool created-id MFA configuration",
		},
		{
			name:     "malformed read",
			resource: UserPoolResource{Name: "pool-name"},
			configure: func(client *fakeCognitoClient) {
				client.describeFn = func(
					*cognitoidentityprovider.DescribeUserPoolInput,
				) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
					return &cognitoidentityprovider.DescribeUserPoolOutput{}, nil
				}
			},
			wantCalls: []string{"create", "describe", "delete"},
			wantError: "empty response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeCognitoClient{
				createFn: func(
					*cognitoidentityprovider.CreateUserPoolInput,
				) (*cognitoidentityprovider.CreateUserPoolOutput, error) {
					return &cognitoidentityprovider.CreateUserPoolOutput{
						UserPool: &cognitotypes.UserPoolType{Id: aws.String("created-id")},
					}, nil
				},
			}
			tt.configure(client)
			out, err := tt.resource.create(
				context.Background(), client, "us-east-1", &instantUserPoolClock{},
			)
			assert.Nil(t, out)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantError)
			if tt.name != "malformed read" {
				assert.ErrorIs(t, err, primary)
			}
			assert.Equal(t, tt.wantCalls, client.calls)
			require.NotNil(t, client.deleteInput)
			assert.Equal(t, "created-id", aws.ToString(client.deleteInput.UserPoolId))
		})
	}
}

func TestUserPoolCreateCleanupUsesUncanceledTimeoutAndAcceptsNotFound(t *testing.T) {
	primary := errors.New("set mfa failed")
	client := &fakeCognitoClient{
		createFn: func(
			*cognitoidentityprovider.CreateUserPoolInput,
		) (*cognitoidentityprovider.CreateUserPoolOutput, error) {
			return &cognitoidentityprovider.CreateUserPoolOutput{
				UserPool: &cognitotypes.UserPoolType{Id: aws.String("created-id")},
			}, nil
		},
		setMFAFn: func(
			*cognitoidentityprovider.SetUserPoolMfaConfigInput,
		) (*cognitoidentityprovider.SetUserPoolMfaConfigOutput, error) {
			return nil, primary
		},
		deleteFn: func(
			*cognitoidentityprovider.DeleteUserPoolInput,
		) (*cognitoidentityprovider.DeleteUserPoolOutput, error) {
			return nil, &cognitotypes.ResourceNotFoundException{Message: aws.String("gone")}
		},
	}
	resource := UserPoolResource{
		Name:             "pool-name",
		MFAConfiguration: aws.String("OPTIONAL"),
		EnabledMFAs:      &[]string{"SOFTWARE_TOKEN_MFA"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()

	out, err := resource.create(ctx, client, "us-east-1", &instantUserPoolClock{})

	assert.Nil(t, out)
	assert.ErrorIs(t, err, primary)
	assert.NotContains(t, err.Error(), "compensating delete")
	require.NotNil(t, client.deleteContext)
	assert.NoError(t, client.deleteContextErr)
	require.True(t, client.deleteHasDeadline)
	assert.WithinDuration(
		t,
		started.Add(userPoolCleanupTimeout),
		client.deleteDeadline,
		time.Second,
	)
	assert.Equal(t, []string{"create", "set-mfa", "delete"}, client.calls)
}

func TestUserPoolCreateCleanupJoinsErrors(t *testing.T) {
	primary := errors.New("read failed")
	tests := []struct {
		name    string
		cleanup error
	}{
		{name: "unrelated", cleanup: errors.New("delete denied")},
		{
			name: "deletion protection",
			cleanup: &cognitotypes.InvalidParameterException{
				Message: aws.String("deletion protection is active"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeCognitoClient{
				createFn: func(
					*cognitoidentityprovider.CreateUserPoolInput,
				) (*cognitoidentityprovider.CreateUserPoolOutput, error) {
					return &cognitoidentityprovider.CreateUserPoolOutput{
						UserPool: &cognitotypes.UserPoolType{Id: aws.String("created-id")},
					}, nil
				},
				describeFn: func(
					*cognitoidentityprovider.DescribeUserPoolInput,
				) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
					return nil, primary
				},
				deleteFn: func(
					*cognitoidentityprovider.DeleteUserPoolInput,
				) (*cognitoidentityprovider.DeleteUserPoolOutput, error) {
					return nil, tt.cleanup
				},
			}
			out, err := (&UserPoolResource{Name: "pool-name"}).create(
				context.Background(), client, "us-east-1", &instantUserPoolClock{},
			)
			assert.Nil(t, out)
			assert.ErrorIs(t, err, primary)
			assert.ErrorIs(t, err, tt.cleanup)
			assert.ErrorContains(t, err, "compensating delete user pool created-id")
			assert.Equal(t, []string{"create", "describe", "delete"}, client.calls)
		})
	}
}

func TestUserPoolCreateDoesNotCleanupBeforeID(t *testing.T) {
	primary := errors.New("create failed")
	client := &fakeCognitoClient{
		createFn: func(
			*cognitoidentityprovider.CreateUserPoolInput,
		) (*cognitoidentityprovider.CreateUserPoolOutput, error) {
			return nil, primary
		},
	}
	out, err := (&UserPoolResource{Name: "pool-name"}).create(
		context.Background(), client, "us-east-1", &instantUserPoolClock{},
	)
	assert.Nil(t, out)
	assert.ErrorIs(t, err, primary)
	assert.Equal(t, []string{"create"}, client.calls)
}

func TestUserPoolReadMapsNotFound(t *testing.T) {
	notFound := &cognitotypes.ResourceNotFoundException{Message: aws.String("gone")}
	tests := map[string]*fakeCognitoClient{
		"describe": {
			describeFn: func(
				*cognitoidentityprovider.DescribeUserPoolInput,
			) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
				return nil, notFound
			},
		},
		"mfa": {
			describeFn: func(
				*cognitoidentityprovider.DescribeUserPoolInput,
			) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
				return userPoolDescribeOutput("prior-id", time.Time{}, time.Time{}), nil
			},
			getMFAFn: func(
				*cognitoidentityprovider.GetUserPoolMfaConfigInput,
			) (*cognitoidentityprovider.GetUserPoolMfaConfigOutput, error) {
				return nil, notFound
			},
		},
	}
	for name, client := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := (&UserPoolResource{}).read(
				context.Background(), client, "us-east-1", "prior-id",
			)
			assert.ErrorIs(t, err, runtime.ErrNotFound)
			assert.Equal(t, "prior-id", aws.ToString(client.describeInput.UserPoolId))
			if name == "mfa" {
				assert.Equal(t, "prior-id", aws.ToString(client.getMFAInput.UserPoolId))
			}
		})
	}
}

func TestUserPoolReadRejectsIncompleteResponses(t *testing.T) {
	tests := map[string]struct {
		describe *cognitoidentityprovider.DescribeUserPoolOutput
		mfa      *cognitoidentityprovider.GetUserPoolMfaConfigOutput
		want     string
	}{
		"nil describe": {want: "empty response"},
		"nil pool": {
			describe: &cognitoidentityprovider.DescribeUserPoolOutput{},
			want:     "empty response",
		},
		"missing id": {
			describe: &cognitoidentityprovider.DescribeUserPoolOutput{
				UserPool: &cognitotypes.UserPoolType{Arn: aws.String("arn")},
			},
			want: "response has no ID",
		},
		"missing arn": {
			describe: &cognitoidentityprovider.DescribeUserPoolOutput{
				UserPool: &cognitotypes.UserPoolType{Id: aws.String("id")},
			},
			want: "response has no ARN",
		},
		"nil mfa": {
			describe: userPoolDescribeOutput("id", time.Time{}, time.Time{}),
			want:     "MFA configuration: empty response",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeCognitoClient{
				describeFn: func(
					*cognitoidentityprovider.DescribeUserPoolInput,
				) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
					return tt.describe, nil
				},
				getMFAFn: func(
					*cognitoidentityprovider.GetUserPoolMfaConfigInput,
				) (*cognitoidentityprovider.GetUserPoolMfaConfigOutput, error) {
					return tt.mfa, nil
				},
			}
			_, err := (&UserPoolResource{}).read(
				context.Background(), client, "us-east-1", "id",
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestUserPoolProviderOutputsUsePartitionDNS(t *testing.T) {
	tests := map[string]string{
		"us-east-1":       "amazonaws.com",
		"us-gov-west-1":   "amazonaws.com",
		"cn-north-1":      "amazonaws.com.cn",
		"eusc-de-east-1":  "amazonaws.eu",
		"us-iso-east-1":   "c2s.ic.gov",
		"us-isob-east-1":  "sc2s.sgov.gov",
		"eu-isoe-west-1":  "cloud.adc-e.uk",
		"us-isof-south-1": "csp.hci.ic.gov",
	}
	for region, suffix := range tests {
		t.Run(region, func(t *testing.T) {
			client := &fakeCognitoClient{
				describeFn: func(
					*cognitoidentityprovider.DescribeUserPoolInput,
				) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
					return userPoolDescribeOutput("pool-id", time.Time{}, time.Time{}), nil
				},
			}
			out, err := (&UserPoolResource{}).read(
				context.Background(), client, region, "pool-id",
			)
			require.NoError(t, err)
			want := "cognito-idp." + region + "." + suffix + "/pool-id"
			assert.Equal(t, want, out.ProviderName)
			assert.Equal(t, want, out.Endpoint)
			assert.Equal(t, "https://"+want, out.ProviderURL)
		})
	}
}

func TestUserPoolUpdateOrdersOperations(t *testing.T) {
	priorSchema := []UserPoolSchemaAttribute{{
		Name: "old", AttributeDataType: "String",
	}}
	currentSchema := append([]UserPoolSchemaAttribute{}, priorSchema...)
	currentSchema = append(currentSchema,
		UserPoolSchemaAttribute{Name: "new", AttributeDataType: "String"},
		UserPoolSchemaAttribute{
			Name:                   "owner",
			AttributeDataType:      "String",
			DeveloperOnlyAttribute: aws.Bool(true),
		},
	)
	priorTags := map[string]string{"drop": "old", "keep": "same"}
	currentTags := map[string]string{
		"add": "new", "keep": "same", "aws:reserved": "ignored",
	}
	resource := UserPoolResource{
		Name:             "new-name",
		MFAConfiguration: aws.String("OPTIONAL"),
		EnabledMFAs:      &[]string{"SOFTWARE_TOKEN_MFA"},
		Schema:           &currentSchema,
		Tags:             &currentTags,
	}
	prior := runtime.Prior[UserPoolResource, *UserPoolResourceOutput, *awsCfg]{
		Inputs: UserPoolResource{
			Name:             "old-name",
			MFAConfiguration: aws.String("OFF"),
			Schema:           &priorSchema,
			Tags:             &priorTags,
		},
		Outputs: &UserPoolResourceOutput{
			UserPoolID: "prior-id",
			ARN:        "arn:aws:cognito-idp:us-east-1:123456789012:userpool/prior-id",
		},
	}
	client := &fakeCognitoClient{
		listTagsFn: func(
			*cognitoidentityprovider.ListTagsForResourceInput,
		) (*cognitoidentityprovider.ListTagsForResourceOutput, error) {
			return &cognitoidentityprovider.ListTagsForResourceOutput{Tags: map[string]string{
				"drop": "old", "keep": "same", "aws:owned": "preserved",
			}}, nil
		},
		describeFn: func(
			*cognitoidentityprovider.DescribeUserPoolInput,
		) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
			return userPoolDescribeOutput("prior-id", time.Time{}, time.Time{}), nil
		},
	}

	out, err := resource.update(
		context.Background(), client, "us-east-1", prior, &instantUserPoolClock{},
	)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, []string{
		"list-tags",
		"untag",
		"tag",
		"set-mfa",
		"update",
		"add-custom-attributes",
		"describe",
		"get-mfa",
	}, client.calls)
	assert.Equal(t, prior.Outputs.ARN, aws.ToString(client.listTagsInput.ResourceArn))
	assert.Equal(t, []string{"drop"}, client.untagInput.TagKeys)
	assert.Equal(t, map[string]string{"add": "new"}, client.tagInput.Tags)
	assert.Equal(t, "prior-id", aws.ToString(client.setMFAInput.UserPoolId))
	assert.Equal(t, "new-name", aws.ToString(client.updateInput.PoolName))
	require.Len(t, client.addInput.CustomAttributes, 2)
	assert.Equal(t, "new", aws.ToString(client.addInput.CustomAttributes[0].Name))
	assert.Equal(t, "owner", aws.ToString(client.addInput.CustomAttributes[1].Name))
	assert.True(t, aws.ToBool(
		client.addInput.CustomAttributes[1].DeveloperOnlyAttribute,
	))
}

func TestUserPoolUpdateCallGates(t *testing.T) {
	priorSchema := []UserPoolSchemaAttribute{{
		Name: "old", AttributeDataType: "String",
	}}
	currentSchema := append([]UserPoolSchemaAttribute{}, priorSchema...)
	currentSchema = append(currentSchema, UserPoolSchemaAttribute{
		Name:                   "new",
		AttributeDataType:      "String",
		DeveloperOnlyAttribute: aws.Bool(true),
	})
	reorderedPriorSchema := []UserPoolSchemaAttribute{
		{Name: "first", AttributeDataType: "String"},
		{Name: "second", AttributeDataType: "Number"},
	}
	reorderedCurrentSchema := []UserPoolSchemaAttribute{
		reorderedPriorSchema[1], reorderedPriorSchema[0],
	}
	smsConfiguration := &UserPoolSMSConfiguration{
		SNSCallerARN: "arn:aws:iam::123456789012:role/cognito",
	}
	priorTags := map[string]string{}
	currentTags := map[string]string{"new": "value"}
	tests := map[string]struct {
		prior   UserPoolResource
		current UserPoolResource
		want    []string
	}{
		"tags only": {
			prior: UserPoolResource{Name: "pool-name", Tags: &priorTags},
			current: UserPoolResource{
				Name: "pool-name", Tags: &currentTags,
			},
			want: []string{"list-tags", "tag", "describe", "get-mfa"},
		},
		"mfa only": {
			prior: UserPoolResource{
				Name: "pool-name", MFAConfiguration: aws.String("OFF"),
			},
			current: UserPoolResource{
				Name:             "pool-name",
				MFAConfiguration: aws.String("OPTIONAL"),
				EnabledMFAs:      &[]string{"SOFTWARE_TOKEN_MFA"},
			},
			want: []string{"set-mfa", "describe", "get-mfa"},
		},
		"mfa factor reorder only": {
			prior: UserPoolResource{
				Name:             "pool-name",
				MFAConfiguration: aws.String("OPTIONAL"),
				EnabledMFAs:      &[]string{"SMS_MFA", "SOFTWARE_TOKEN_MFA"},
				SMSConfiguration: smsConfiguration,
			},
			current: UserPoolResource{
				Name:             "pool-name",
				MFAConfiguration: aws.String("OPTIONAL"),
				EnabledMFAs:      &[]string{"SOFTWARE_TOKEN_MFA", "SMS_MFA"},
				SMSConfiguration: smsConfiguration,
			},
			want: []string{"describe", "get-mfa"},
		},
		"standard only": {
			prior:   UserPoolResource{Name: "old-name"},
			current: UserPoolResource{Name: "new-name"},
			want:    []string{"update", "describe", "get-mfa"},
		},
		"schema only": {
			prior: UserPoolResource{
				Name: "pool-name", Schema: &priorSchema,
			},
			current: UserPoolResource{
				Name: "pool-name", Schema: &currentSchema,
			},
			want: []string{"add-custom-attributes", "describe", "get-mfa"},
		},
		"schema reorder only": {
			prior: UserPoolResource{
				Name: "pool-name", Schema: &reorderedPriorSchema,
			},
			current: UserPoolResource{
				Name: "pool-name", Schema: &reorderedCurrentSchema,
			},
			want: []string{"describe", "get-mfa"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := updateReadClient("prior-id")
			out, err := tt.current.update(
				context.Background(),
				client,
				"us-east-1",
				runtime.Prior[UserPoolResource, *UserPoolResourceOutput, *awsCfg]{
					Inputs: tt.prior,
					Outputs: &UserPoolResourceOutput{
						UserPoolID: "prior-id",
						ARN: "arn:aws:cognito-idp:us-east-1:123456789012:" +
							"userpool/prior-id",
					},
				},
				&instantUserPoolClock{},
			)
			require.NoError(t, err)
			require.NotNil(t, out)
			assert.Equal(t, tt.want, client.calls)
		})
	}
}

func TestUserPoolTagReconciliation(t *testing.T) {
	tests := map[string]struct {
		desired     map[string]string
		current     map[string]string
		wantCalls   []string
		wantTags    map[string]string
		wantRemoved []string
	}{
		"add": {
			desired:   map[string]string{"new": "value"},
			current:   map[string]string{},
			wantCalls: []string{"list-tags", "tag"},
			wantTags:  map[string]string{"new": "value"},
		},
		"change": {
			desired:   map[string]string{"change": "new"},
			current:   map[string]string{"change": "old"},
			wantCalls: []string{"list-tags", "tag"},
			wantTags:  map[string]string{"change": "new"},
		},
		"remove": {
			desired:     map[string]string{"keep": "value"},
			current:     map[string]string{"drop": "value", "keep": "value"},
			wantCalls:   []string{"list-tags", "untag"},
			wantRemoved: []string{"drop"},
		},
		"clear": {
			desired: map[string]string{},
			current: map[string]string{
				"z": "last", "a": "first", "aws:owned": "preserved",
			},
			wantCalls:   []string{"list-tags", "untag"},
			wantRemoved: []string{"a", "z"},
		},
		"reserved desired key": {
			desired: map[string]string{
				"aws:ignored": "value", "managed": "value",
			},
			current:   map[string]string{},
			wantCalls: []string{"list-tags", "tag"},
			wantTags:  map[string]string{"managed": "value"},
		},
		"unchanged and reserved current key": {
			desired: map[string]string{"keep": "value"},
			current: map[string]string{
				"keep": "value", "aws:owned": "preserved",
			},
			wantCalls: []string{"list-tags"},
		},
	}
	const arn = "arn:aws:cognito-idp:us-east-1:123456789012:userpool/prior-id"
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeCognitoClient{
				listTagsFn: func(
					*cognitoidentityprovider.ListTagsForResourceInput,
				) (*cognitoidentityprovider.ListTagsForResourceOutput, error) {
					return &cognitoidentityprovider.ListTagsForResourceOutput{
						Tags: tt.current,
					}, nil
				},
			}
			resource := UserPoolResource{Tags: &tt.desired}
			err := resource.syncTags(context.Background(), client, arn)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCalls, client.calls)
			assert.Equal(t, arn, aws.ToString(client.listTagsInput.ResourceArn))
			if client.tagInput != nil {
				assert.Equal(t, arn, aws.ToString(client.tagInput.ResourceArn))
				assert.Equal(t, tt.wantTags, client.tagInput.Tags)
			} else {
				assert.Nil(t, tt.wantTags)
			}
			if client.untagInput != nil {
				assert.Equal(t, arn, aws.ToString(client.untagInput.ResourceArn))
				assert.Equal(t, tt.wantRemoved, client.untagInput.TagKeys)
			} else {
				assert.Nil(t, tt.wantRemoved)
			}
		})
	}
}

func TestUserPoolUpdateWithoutChangesOnlyReads(t *testing.T) {
	resource := UserPoolResource{Name: "pool-name"}
	client := &fakeCognitoClient{
		describeFn: func(
			*cognitoidentityprovider.DescribeUserPoolInput,
		) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
			return userPoolDescribeOutput("prior-id", time.Time{}, time.Time{}), nil
		},
	}
	out, err := resource.update(
		context.Background(),
		client,
		"us-east-1",
		runtime.Prior[UserPoolResource, *UserPoolResourceOutput, *awsCfg]{
			Inputs: resource,
			Outputs: &UserPoolResourceOutput{
				UserPoolID: "prior-id",
				ARN:        "pool-arn",
			},
		},
		&instantUserPoolClock{},
	)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, []string{"describe", "get-mfa"}, client.calls)
}

func TestUserPoolUpdateReplacementErrorsPrecedeCalls(t *testing.T) {
	schema := UserPoolSchemaAttribute{Name: "old", AttributeDataType: "String"}
	tests := map[string]struct {
		prior   UserPoolResource
		current UserPoolResource
		want    string
	}{
		"schema removal": {
			prior: UserPoolResource{
				Name: "pool-name", Schema: &[]UserPoolSchemaAttribute{schema},
			},
			current: UserPoolResource{Name: "pool-name"},
			want:    "removing schema attribute",
		},
		"schema modification": {
			prior: UserPoolResource{
				Name: "pool-name", Schema: &[]UserPoolSchemaAttribute{schema},
			},
			current: UserPoolResource{
				Name: "pool-name",
				Schema: &[]UserPoolSchemaAttribute{{
					Name: "old", AttributeDataType: "Number",
				}},
			},
			want: "modifying schema attribute",
		},
		"standard schema addition": {
			prior: UserPoolResource{Name: "pool-name"},
			current: UserPoolResource{
				Name: "pool-name",
				Schema: &[]UserPoolSchemaAttribute{
					{Name: "email", AttributeDataType: "String"},
				},
			},
			want: "adding standard schema attribute",
		},
		"sms removal": {
			prior: UserPoolResource{
				Name: "pool-name",
				SMSConfiguration: &UserPoolSMSConfiguration{
					SNSCallerARN: "arn:aws:iam::123456789012:role/cognito",
				},
			},
			current: UserPoolResource{Name: "pool-name"},
			want:    "removing sms-configuration",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeCognitoClient{}
			_, err := tt.current.update(
				context.Background(),
				client,
				"us-east-1",
				runtime.Prior[UserPoolResource, *UserPoolResourceOutput, *awsCfg]{
					Inputs: tt.prior,
					Outputs: &UserPoolResourceOutput{
						UserPoolID: "prior-id", ARN: "pool-arn",
					},
				},
				&instantUserPoolClock{},
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
			assert.Empty(t, client.calls)
		})
	}
}

func TestSchemaAdditionsIgnoreOrdering(t *testing.T) {
	first := UserPoolSchemaAttribute{Name: "first", AttributeDataType: "String"}
	second := UserPoolSchemaAttribute{Name: "second", AttributeDataType: "Number"}
	third := UserPoolSchemaAttribute{
		Name:                   "third",
		AttributeDataType:      "Boolean",
		DeveloperOnlyAttribute: aws.Bool(true),
	}
	fourth := UserPoolSchemaAttribute{Name: "fourth", AttributeDataType: "DateTime"}

	additions, err := schemaAdditions(
		[]UserPoolSchemaAttribute{first, second},
		[]UserPoolSchemaAttribute{second, third, first, fourth},
	)
	require.NoError(t, err)
	assert.Equal(t, []UserPoolSchemaAttribute{third, fourth}, additions)
}

func TestUserPoolDeleteUsesPriorID(t *testing.T) {
	tests := map[string]struct {
		err     error
		wantErr bool
	}{
		"success": {},
		"already absent": {
			err: &cognitotypes.ResourceNotFoundException{Message: aws.String("gone")},
		},
		"other error": {err: errors.New("denied"), wantErr: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeCognitoClient{
				deleteFn: func(
					*cognitoidentityprovider.DeleteUserPoolInput,
				) (*cognitoidentityprovider.DeleteUserPoolOutput, error) {
					return nil, tt.err
				},
			}
			err := (&UserPoolResource{Name: "current-name"}).delete(
				context.Background(), client, "prior-id",
			)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, "delete user pool prior-id")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, "prior-id", aws.ToString(client.deleteInput.UserPoolId))
			assert.Equal(t, []string{"delete"}, client.calls)
		})
	}
}

func TestPriorUserPoolID(t *testing.T) {
	tests := map[string]struct {
		prior *UserPoolResourceOutput
		want  string
		err   string
	}{
		"missing output": {err: "prior output is missing"},
		"missing id": {
			prior: &UserPoolResourceOutput{},
			err:   "prior output has no user-pool-id",
		},
		"recorded id": {
			prior: &UserPoolResourceOutput{UserPoolID: "prior-id"},
			want:  "prior-id",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := priorUserPoolID(tt.prior)
			if tt.err != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUserPoolEquivalentInput(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		prior     UserPoolResource
		reordered UserPoolResource
		changed   UserPoolResource
		empty     UserPoolResource
	}{
		{
			name:  "alias attributes",
			field: "alias-attributes",
			prior: UserPoolResource{
				AliasAttributes: &[]string{"email", "phone_number"},
			},
			reordered: UserPoolResource{
				AliasAttributes: &[]string{"phone_number", "email"},
			},
			changed: UserPoolResource{AliasAttributes: &[]string{"email"}},
			empty:   UserPoolResource{AliasAttributes: &[]string{}},
		},
		{
			name:  "username attributes",
			field: "username-attributes",
			prior: UserPoolResource{
				UsernameAttributes: &[]string{"email", "phone_number"},
			},
			reordered: UserPoolResource{
				UsernameAttributes: &[]string{"phone_number", "email"},
			},
			changed: UserPoolResource{UsernameAttributes: &[]string{"email"}},
			empty:   UserPoolResource{UsernameAttributes: &[]string{}},
		},
		{
			name:  "auto verified attributes",
			field: "auto-verified-attributes",
			prior: UserPoolResource{
				AutoVerifiedAttributes: &[]string{"email", "phone_number"},
			},
			reordered: UserPoolResource{
				AutoVerifiedAttributes: &[]string{"phone_number", "email"},
			},
			changed: UserPoolResource{AutoVerifiedAttributes: &[]string{"email"}},
			empty:   UserPoolResource{AutoVerifiedAttributes: &[]string{}},
		},
		{
			name:  "enabled mfas",
			field: "enabled-mfas",
			prior: UserPoolResource{
				EnabledMFAs: &[]string{"SMS_MFA", "EMAIL_OTP"},
			},
			reordered: UserPoolResource{
				EnabledMFAs: &[]string{"EMAIL_OTP", "SMS_MFA"},
			},
			changed: UserPoolResource{EnabledMFAs: &[]string{"EMAIL_OTP"}},
			empty:   UserPoolResource{EnabledMFAs: &[]string{}},
		},
		{
			name:  "schema",
			field: "schema",
			prior: UserPoolResource{Schema: &[]UserPoolSchemaAttribute{
				{Name: "first", AttributeDataType: "String"},
				{Name: "second", AttributeDataType: "Number"},
			}},
			reordered: UserPoolResource{Schema: &[]UserPoolSchemaAttribute{
				{Name: "second", AttributeDataType: "Number"},
				{Name: "first", AttributeDataType: "String"},
			}},
			changed: UserPoolResource{Schema: &[]UserPoolSchemaAttribute{
				{Name: "first", AttributeDataType: "String"},
				{Name: "second", AttributeDataType: "Boolean"},
			}},
			empty: UserPoolResource{Schema: &[]UserPoolSchemaAttribute{}},
		},
		{
			name:  "recovery mechanisms",
			field: "account-recovery-setting",
			prior: UserPoolResource{
				AccountRecoverySetting: &UserPoolAccountRecoverySetting{
					RecoveryMechanisms: []UserPoolRecoveryMechanism{
						{Name: "verified_email", Priority: 1},
						{Name: "verified_phone_number", Priority: 2},
					},
				},
			},
			reordered: UserPoolResource{
				AccountRecoverySetting: &UserPoolAccountRecoverySetting{
					RecoveryMechanisms: []UserPoolRecoveryMechanism{
						{Name: "verified_phone_number", Priority: 2},
						{Name: "verified_email", Priority: 1},
					},
				},
			},
			changed: UserPoolResource{
				AccountRecoverySetting: &UserPoolAccountRecoverySetting{
					RecoveryMechanisms: []UserPoolRecoveryMechanism{
						{Name: "verified_phone_number", Priority: 1},
						{Name: "verified_email", Priority: 2},
					},
				},
			},
			empty: UserPoolResource{
				AccountRecoverySetting: &UserPoolAccountRecoverySetting{
					RecoveryMechanisms: []UserPoolRecoveryMechanism{},
				},
			},
		},
		{
			name:  "allowed first auth factors",
			field: "sign-in-policy",
			prior: UserPoolResource{SignInPolicy: &UserPoolSignInPolicy{
				AllowedFirstAuthFactors: &[]string{"PASSWORD", "WEB_AUTHN"},
			}},
			reordered: UserPoolResource{SignInPolicy: &UserPoolSignInPolicy{
				AllowedFirstAuthFactors: &[]string{"WEB_AUTHN", "PASSWORD"},
			}},
			changed: UserPoolResource{SignInPolicy: &UserPoolSignInPolicy{
				AllowedFirstAuthFactors: &[]string{"PASSWORD"},
			}},
			empty: UserPoolResource{SignInPolicy: &UserPoolSignInPolicy{
				AllowedFirstAuthFactors: &[]string{},
			}},
		},
		{
			name:  "verification before update attributes",
			field: "user-attribute-update-settings",
			prior: UserPoolResource{
				UserAttributeUpdateSettings: &UserPoolAttributeUpdateSettings{
					AttributesRequireVerificationBeforeUpdate: []string{
						"email", "phone_number",
					},
				},
			},
			reordered: UserPoolResource{
				UserAttributeUpdateSettings: &UserPoolAttributeUpdateSettings{
					AttributesRequireVerificationBeforeUpdate: []string{
						"phone_number", "email",
					},
				},
			},
			changed: UserPoolResource{
				UserAttributeUpdateSettings: &UserPoolAttributeUpdateSettings{
					AttributesRequireVerificationBeforeUpdate: []string{"email"},
				},
			},
			empty: UserPoolResource{
				UserAttributeUpdateSettings: &UserPoolAttributeUpdateSettings{
					AttributesRequireVerificationBeforeUpdate: []string{},
				},
			},
		},
	}

	resource := UserPoolResource{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, resource.EquivalentInput(tt.field, tt.prior, tt.reordered))
			assert.False(t, resource.EquivalentInput(tt.field, tt.prior, tt.changed))
			assert.False(t, resource.EquivalentInput(tt.field, UserPoolResource{}, tt.empty))
		})
	}
	assert.False(t, resource.EquivalentInput("name", UserPoolResource{}, UserPoolResource{}))
}

func userPoolDescribeOutput(
	id string,
	createdAt time.Time,
	modifiedAt time.Time,
) *cognitoidentityprovider.DescribeUserPoolOutput {
	pool := &cognitotypes.UserPoolType{
		Id:                     aws.String(id),
		Arn:                    aws.String("arn:aws:cognito-idp:us-east-1:123456789012:userpool/" + id),
		Domain:                 aws.String("domain"),
		CustomDomain:           aws.String("login.example.com"),
		EstimatedNumberOfUsers: 42,
		UserPoolTier:           cognitotypes.UserPoolTierTypeEssentials,
	}
	if !createdAt.IsZero() {
		pool.CreationDate = &createdAt
	}
	if !modifiedAt.IsZero() {
		pool.LastModifiedDate = &modifiedAt
	}
	return &cognitoidentityprovider.DescribeUserPoolOutput{UserPool: pool}
}

func updateReadClient(id string) *fakeCognitoClient {
	return &fakeCognitoClient{
		describeFn: func(
			*cognitoidentityprovider.DescribeUserPoolInput,
		) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
			return userPoolDescribeOutput(id, time.Time{}, time.Time{}), nil
		},
	}
}

type instantUserPoolClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *instantUserPoolClock) Now() time.Time {
	return c.now
}

func (c *instantUserPoolClock) Sleep(
	ctx context.Context,
	duration time.Duration,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	c.sleeps = append(c.sleeps, duration)
	c.now = c.now.Add(duration)
	return nil
}
