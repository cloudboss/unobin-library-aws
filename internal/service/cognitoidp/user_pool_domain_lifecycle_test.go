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

type fakeUserPoolDomainAPI struct {
	createInputs    []*cognitoidentityprovider.CreateUserPoolDomainInput
	createOutputs   []*cognitoidentityprovider.CreateUserPoolDomainOutput
	createErrors    []error
	describeInputs  []*cognitoidentityprovider.DescribeUserPoolDomainInput
	describeOutputs []*cognitoidentityprovider.DescribeUserPoolDomainOutput
	describeErrors  []error
	updateInputs    []*cognitoidentityprovider.UpdateUserPoolDomainInput
	updateOutputs   []*cognitoidentityprovider.UpdateUserPoolDomainOutput
	updateErrors    []error
	deleteInputs    []*cognitoidentityprovider.DeleteUserPoolDomainInput
	deleteOutputs   []*cognitoidentityprovider.DeleteUserPoolDomainOutput
	deleteErrors    []error
}

func (c *fakeUserPoolDomainAPI) CreateUserPoolDomain(
	_ context.Context,
	input *cognitoidentityprovider.CreateUserPoolDomainInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.CreateUserPoolDomainOutput, error) {
	c.createInputs = append(c.createInputs, input)
	index := len(c.createInputs) - 1
	return valueAt(c.createOutputs, index), valueAt(c.createErrors, index)
}

func (c *fakeUserPoolDomainAPI) DeleteUserPoolDomain(
	_ context.Context,
	input *cognitoidentityprovider.DeleteUserPoolDomainInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.DeleteUserPoolDomainOutput, error) {
	c.deleteInputs = append(c.deleteInputs, input)
	index := len(c.deleteInputs) - 1
	return valueAt(c.deleteOutputs, index), valueAt(c.deleteErrors, index)
}

func (c *fakeUserPoolDomainAPI) DescribeUserPoolDomain(
	_ context.Context,
	input *cognitoidentityprovider.DescribeUserPoolDomainInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.DescribeUserPoolDomainOutput, error) {
	c.describeInputs = append(c.describeInputs, input)
	index := len(c.describeInputs) - 1
	return valueAt(c.describeOutputs, index), valueAt(c.describeErrors, index)
}

func (c *fakeUserPoolDomainAPI) UpdateUserPoolDomain(
	_ context.Context,
	input *cognitoidentityprovider.UpdateUserPoolDomainInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.UpdateUserPoolDomainOutput, error) {
	c.updateInputs = append(c.updateInputs, input)
	index := len(c.updateInputs) - 1
	return valueAt(c.updateOutputs, index), valueAt(c.updateErrors, index)
}

func TestUserPoolDomainCreateWaitsAndReads(t *testing.T) {
	resource := UserPoolDomainResource{
		Domain:     "auth",
		UserPoolID: "us-east-1_pool",
	}
	client := &fakeUserPoolDomainAPI{
		createOutputs: []*cognitoidentityprovider.CreateUserPoolDomainOutput{{}},
		describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
			domainDescribe("auth", "us-east-1_pool", cognitotypes.DomainStatusTypeCreating),
			domainDescribe("auth", "us-east-1_pool", cognitotypes.DomainStatusTypeActive),
			domainDescribe("auth", "us-east-1_pool", cognitotypes.DomainStatusTypeActive),
		},
	}
	clock := &instantUserPoolClock{}

	out, err := resource.create(context.Background(), client, "us-east-1", clock)

	require.NoError(t, err)
	assert.Equal(t, "auth", out.Domain)
	assert.Equal(t, "us-east-1_pool", out.UserPoolID)
	assert.Nil(t, client.createInputs[0].CustomDomainConfig)
	assert.Nil(t, client.createInputs[0].ManagedLoginVersion)
	assert.Nil(t, client.createInputs[0].Routing)
	assert.Equal(t, []time.Duration{200 * time.Millisecond}, clock.sleeps)
	require.Len(t, client.describeInputs, 3)
	assert.Equal(t, "auth", aws.ToString(client.describeInputs[0].Domain))
}

func TestUserPoolDomainCreateMapsCustomExtensions(t *testing.T) {
	resource := completeUserPoolDomainResource()
	client := &fakeUserPoolDomainAPI{
		createOutputs: []*cognitoidentityprovider.CreateUserPoolDomainOutput{{}},
		describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
			domainDescribe("auth.example.com", "us-east-1_pool",
				cognitotypes.DomainStatusTypeActive),
			completeDomainDescribe("auth.example.com", "us-east-1_pool",
				cognitotypes.DomainStatusTypeActive),
		},
	}

	out, err := resource.create(context.Background(), client, "cn-north-1", &instantUserPoolClock{})

	require.NoError(t, err)
	require.Len(t, client.createInputs, 1)
	input := client.createInputs[0]
	assert.Equal(t, "auth.example.com", aws.ToString(input.Domain))
	assert.Equal(t, "us-east-1_pool", aws.ToString(input.UserPoolId))
	require.NotNil(t, input.CustomDomainConfig)
	assert.Equal(t, "arn:aws:acm:us-east-1:123456789012:certificate/abc",
		aws.ToString(input.CustomDomainConfig.CertificateArn))
	assert.Equal(t, "TLS_V1_3_2025", string(input.CustomDomainConfig.SecurityPolicy))
	assert.Equal(t, int32(2), aws.ToInt32(input.ManagedLoginVersion))
	require.NotNil(t, input.Routing)
	require.NotNil(t, input.Routing.Failover)
	assert.Equal(t, "hc-primary",
		aws.ToString(input.Routing.Failover.PrimaryRoute53HealthCheckId))
	assert.Equal(t, "us-west-2", aws.ToString(input.Routing.Failover.SecondaryRegion))
	require.NotNil(t, out.CustomDomainConfig)
	assert.Equal(t, "TLS_V1_3_2025", stringPtrValue(out.CustomDomainConfig.SecurityPolicy))
	assert.Equal(t, "Z3RFFRIM2A3IF5", out.CloudFrontDistributionZoneID)
	require.NotNil(t, out.Routing)
	assert.Equal(t, "hc-primary", out.Routing.Failover.PrimaryRoute53HealthCheckID)
}

func TestUserPoolDomainCreateCleanup(t *testing.T) {
	tests := []struct {
		name          string
		client        *fakeUserPoolDomainAPI
		wantDeletes   int
		wantErrSubstr string
	}{
		{
			name: "no cleanup before accepted create",
			client: &fakeUserPoolDomainAPI{
				createErrors: []error{errors.New("create failed")},
			},
			wantErrSubstr: "create failed",
		},
		{
			name: "cleanup after waiter failure",
			client: &fakeUserPoolDomainAPI{
				createOutputs: []*cognitoidentityprovider.CreateUserPoolDomainOutput{{}},
				describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
					domainDescribe("auth", "us-east-1_pool", cognitotypes.DomainStatusTypeFailed),
				},
				deleteErrors: []error{noSuchDomainError()},
			},
			wantDeletes:   1,
			wantErrSubstr: "status is \"FAILED\"",
		},
		{
			name: "joins cleanup failure",
			client: &fakeUserPoolDomainAPI{
				createOutputs: []*cognitoidentityprovider.CreateUserPoolDomainOutput{{}},
				describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
					domainDescribe("auth", "us-east-1_pool", cognitotypes.DomainStatusTypeFailed),
				},
				deleteErrors: []error{errors.New("delete failed")},
			},
			wantDeletes:   1,
			wantErrSubstr: "delete failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := UserPoolDomainResource{Domain: "auth", UserPoolID: "us-east-1_pool"}

			_, err := resource.create(
				context.Background(),
				tt.client,
				"us-east-1",
				&instantUserPoolClock{},
			)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrSubstr)
			assert.Len(t, tt.client.deleteInputs, tt.wantDeletes)
		})
	}
}

func TestUserPoolDomainRead(t *testing.T) {
	resource := UserPoolDomainResource{}
	tests := []struct {
		name       string
		client     *fakeUserPoolDomainAPI
		want       *UserPoolDomainResourceOutput
		wantErr    error
		wantErrStr string
	}{
		{
			name: "full custom response",
			client: &fakeUserPoolDomainAPI{
				describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
					completeDomainDescribe("auth.example.com", "us-east-1_pool",
						cognitotypes.DomainStatusTypeActive),
				},
			},
			want: &UserPoolDomainResourceOutput{
				Domain:                       "auth.example.com",
				UserPoolID:                   "us-east-1_pool",
				CustomDomainConfig:           completeUserPoolDomainCustomConfig(),
				ManagedLoginVersion:          aws.Int32(2),
				Routing:                      completeUserPoolDomainRouting(),
				AWSAccountID:                 aws.String("123456789012"),
				CloudFrontDistribution:       aws.String("d111.cloudfront.net"),
				CloudFrontDistributionZoneID: "Z2FDTNDATAQYW2",
				S3Bucket:                     aws.String("bucket"),
				Version:                      aws.String("v1"),
			},
		},
		{
			name: "not found",
			client: &fakeUserPoolDomainAPI{
				describeErrors: []error{&cognitotypes.ResourceNotFoundException{}},
			},
			wantErr: runtime.ErrNotFound,
		},
		{
			name:    "nil output",
			client:  &fakeUserPoolDomainAPI{},
			wantErr: runtime.ErrNotFound,
		},
		{
			name: "empty status",
			client: &fakeUserPoolDomainAPI{
				describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
					domainDescribe("auth", "us-east-1_pool", ""),
				},
			},
			wantErr: runtime.ErrNotFound,
		},
		{
			name: "empty user pool id",
			client: &fakeUserPoolDomainAPI{
				describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
					domainDescribe("auth", "", cognitotypes.DomainStatusTypeActive),
				},
			},
			wantErrStr: "response has no user pool ID",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := resource.read(context.Background(), tt.client, "us-east-1", "auth")

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			if tt.wantErrStr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrStr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
			require.Len(t, tt.client.describeInputs, 1)
			assert.Equal(t, "auth", aws.ToString(tt.client.describeInputs[0].Domain))
		})
	}
}

func TestUserPoolDomainUpdateRequests(t *testing.T) {
	cert := "arn:aws:acm:us-east-1:123456789012:certificate/next"
	tests := []struct {
		name       string
		current    UserPoolDomainResource
		prior      UserPoolDomainResource
		observed   *UserPoolDomainResourceOutput
		assertions func(*testing.T, *cognitoidentityprovider.UpdateUserPoolDomainInput)
		wantCalls  int
	}{
		{
			name: "certificate only",
			current: UserPoolDomainResource{
				Domain: "auth.example.com", UserPoolID: "pool",
				CustomDomainConfig: &UserPoolDomainCustomDomainConfig{CertificateARN: &cert},
			},
			prior: UserPoolDomainResource{
				Domain: "auth.example.com", UserPoolID: "pool",
				CustomDomainConfig: completeUserPoolDomainCustomConfig(),
			},
			assertions: func(t *testing.T, input *cognitoidentityprovider.UpdateUserPoolDomainInput) {
				require.NotNil(t, input.CustomDomainConfig)
				assert.Equal(t, cert, aws.ToString(input.CustomDomainConfig.CertificateArn))
				assert.Nil(t, input.ManagedLoginVersion)
				assert.Nil(t, input.Routing)
			},
			wantCalls: 1,
		},
		{
			name: "managed login prefix",
			current: UserPoolDomainResource{
				Domain: "auth", UserPoolID: "pool", ManagedLoginVersion: aws.Int32(2),
			},
			prior: UserPoolDomainResource{
				Domain: "auth", UserPoolID: "pool", ManagedLoginVersion: aws.Int32(1),
			},
			assertions: func(t *testing.T, input *cognitoidentityprovider.UpdateUserPoolDomainInput) {
				assert.Nil(t, input.CustomDomainConfig)
				assert.Equal(t, int32(2), aws.ToInt32(input.ManagedLoginVersion))
			},
			wantCalls: 1,
		},
		{
			name: "managed login custom includes current custom config",
			current: UserPoolDomainResource{
				Domain: "auth.example.com", UserPoolID: "pool",
				CustomDomainConfig:  completeUserPoolDomainCustomConfig(),
				ManagedLoginVersion: aws.Int32(2),
			},
			prior: UserPoolDomainResource{
				Domain: "auth.example.com", UserPoolID: "pool",
				CustomDomainConfig:  completeUserPoolDomainCustomConfig(),
				ManagedLoginVersion: aws.Int32(1),
			},
			assertions: func(t *testing.T, input *cognitoidentityprovider.UpdateUserPoolDomainInput) {
				require.NotNil(t, input.CustomDomainConfig)
				assert.Equal(t, int32(2), aws.ToInt32(input.ManagedLoginVersion))
			},
			wantCalls: 1,
		},
		{
			name: "security policy updates in place",
			current: UserPoolDomainResource{
				Domain: "auth.example.com", UserPoolID: "pool",
				CustomDomainConfig: &UserPoolDomainCustomDomainConfig{
					CertificateARN: aws.String(
						"arn:aws:acm:us-east-1:123456789012:certificate/abc",
					),
					SecurityPolicy: aws.String("TLS_V1_2_2021"),
				},
			},
			prior: UserPoolDomainResource{
				Domain:             "auth.example.com",
				UserPoolID:         "pool",
				CustomDomainConfig: completeUserPoolDomainCustomConfig(),
			},
			assertions: func(t *testing.T, input *cognitoidentityprovider.UpdateUserPoolDomainInput) {
				require.NotNil(t, input.CustomDomainConfig)
				assert.Equal(t, "TLS_V1_2_2021", string(input.CustomDomainConfig.SecurityPolicy))
			},
			wantCalls: 1,
		},
		{
			name:    "no op",
			current: completeUserPoolDomainResource(),
			prior:   completeUserPoolDomainResource(),
			observed: &UserPoolDomainResourceOutput{
				Domain:              "auth.example.com",
				UserPoolID:          "pool",
				CustomDomainConfig:  completeUserPoolDomainCustomConfig(),
				ManagedLoginVersion: aws.Int32(2),
				Routing:             completeUserPoolDomainRouting(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeUserPoolDomainAPI{
				updateOutputs: []*cognitoidentityprovider.UpdateUserPoolDomainOutput{{}},
				describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
					domainDescribe(tt.current.Domain, tt.current.UserPoolID,
						cognitotypes.DomainStatusTypeUpdating),
					completeDomainDescribe(tt.current.Domain, tt.current.UserPoolID,
						cognitotypes.DomainStatusTypeActive),
					completeDomainDescribe(tt.current.Domain, tt.current.UserPoolID,
						cognitotypes.DomainStatusTypeActive),
				},
			}
			prior := runtime.Prior[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg]{
				Inputs: tt.prior,
				Outputs: &UserPoolDomainResourceOutput{
					Domain: tt.prior.Domain, UserPoolID: tt.prior.UserPoolID,
				},
				Observed: tt.observed,
			}
			out, err := tt.current.update(context.Background(), client, "us-east-1", prior,
				&instantUserPoolClock{})

			require.NoError(t, err)
			assert.NotNil(t, out)
			assert.Len(t, client.updateInputs, tt.wantCalls)
			if tt.wantCalls > 0 {
				assert.Equal(t, tt.prior.Domain, aws.ToString(client.updateInputs[0].Domain))
				assert.Equal(t, tt.prior.UserPoolID, aws.ToString(client.updateInputs[0].UserPoolId))
				tt.assertions(t, client.updateInputs[0])
			}
		})
	}
}

func TestUserPoolDomainUpdateReplacementAndRemoval(t *testing.T) {
	prior := runtime.Prior[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg]{
		Inputs: UserPoolDomainResource{
			Domain: "auth", UserPoolID: "pool",
		},
		Outputs: &UserPoolDomainResourceOutput{Domain: "auth", UserPoolID: "pool"},
	}
	cert := "arn:aws:acm:us-east-1:123456789012:certificate/abc"
	current := UserPoolDomainResource{
		Domain: "auth", UserPoolID: "pool",
		CustomDomainConfig: &UserPoolDomainCustomDomainConfig{CertificateARN: &cert},
	}
	client := &fakeUserPoolDomainAPI{}

	_, err := current.update(context.Background(), client, "us-east-1", prior,
		&instantUserPoolClock{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires replacement")
	assert.Empty(t, client.updateInputs)

	removedManaged := UserPoolDomainResource{Domain: "auth", UserPoolID: "pool"}
	prior.Inputs.ManagedLoginVersion = aws.Int32(2)
	out, err := removedManaged.update(context.Background(), client, "us-east-1", prior,
		&instantUserPoolClock{})
	require.NoError(t, err)
	assert.Equal(t, prior.Outputs, out)
	assert.Empty(t, client.updateInputs)
}

func TestUserPoolDomainUpdateRejectsCreatingStatus(t *testing.T) {
	current := completeUserPoolDomainResource()
	current.ManagedLoginVersion = aws.Int32(1)
	priorInput := completeUserPoolDomainResource()
	client := &fakeUserPoolDomainAPI{
		updateOutputs: []*cognitoidentityprovider.UpdateUserPoolDomainOutput{{}},
		describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
			domainDescribe("auth.example.com", "us-east-1_pool",
				cognitotypes.DomainStatusTypeCreating),
		},
	}
	prior := runtime.Prior[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg]{
		Inputs: priorInput,
		Outputs: &UserPoolDomainResourceOutput{
			Domain: "auth.example.com", UserPoolID: "us-east-1_pool",
		},
	}

	_, err := current.update(context.Background(), client, "us-east-1", prior,
		&instantUserPoolClock{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status is \"CREATING\"")
	assert.Len(t, client.updateInputs, 1)
	assert.Len(t, client.describeInputs, 1)
}

func TestUserPoolDomainDelete(t *testing.T) {
	prior := &UserPoolDomainResourceOutput{Domain: "auth", UserPoolID: "pool"}
	tests := []struct {
		name    string
		client  *fakeUserPoolDomainAPI
		wantErr string
	}{
		{
			name: "accepted delete waits for absence",
			client: &fakeUserPoolDomainAPI{
				deleteOutputs:  []*cognitoidentityprovider.DeleteUserPoolDomainOutput{{}},
				describeErrors: []error{&cognitotypes.ResourceNotFoundException{}},
			},
		},
		{
			name: "suppresses exact no such domain",
			client: &fakeUserPoolDomainAPI{
				deleteErrors: []error{noSuchDomainError()},
			},
		},
		{
			name: "case mismatch propagates",
			client: &fakeUserPoolDomainAPI{
				deleteErrors: []error{&cognitotypes.InvalidParameterException{
					Message: aws.String("no such domain"),
				}},
			},
			wantErr: "no such domain",
		},
		{
			name: "resource not found propagates",
			client: &fakeUserPoolDomainAPI{
				deleteErrors: []error{&cognitotypes.ResourceNotFoundException{}},
			},
			wantErr: "delete user pool domain",
		},
		{
			name: "wait failure propagates",
			client: &fakeUserPoolDomainAPI{
				deleteOutputs:  []*cognitoidentityprovider.DeleteUserPoolDomainOutput{{}},
				describeErrors: []error{errors.New("describe failed")},
			},
			wantErr: "describe failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&UserPoolDomainResource{}).delete(context.Background(), tt.client, prior,
				&instantUserPoolClock{})

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, tt.client.deleteInputs, 1)
			assert.Equal(t, "auth", aws.ToString(tt.client.deleteInputs[0].Domain))
			assert.Equal(t, "pool", aws.ToString(tt.client.deleteInputs[0].UserPoolId))
		})
	}
}

func TestUserPoolDomainDeleteWaitsOnlyForDeleteStates(t *testing.T) {
	prior := &UserPoolDomainResourceOutput{Domain: "auth", UserPoolID: "pool"}
	tests := []struct {
		name        string
		outputs     []*cognitoidentityprovider.DescribeUserPoolDomainOutput
		errors      []error
		wantErr     string
		wantSleeps  []time.Duration
		wantElapsed time.Duration
	}{
		{
			name: "active is not success",
			outputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
				domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeActive),
			},
			wantErr: "status is \"ACTIVE\"",
		},
		{
			name: "updating and deleting are pending until absence",
			outputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
				domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeUpdating),
				domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeDeleting),
			},
			errors: []error{
				nil,
				nil,
				&cognitotypes.ResourceNotFoundException{},
			},
			wantSleeps: []time.Duration{200 * time.Millisecond, 400 * time.Millisecond},
		},
		{
			name:        "uses one minute timeout",
			outputs:     repeatedDomainStatus("auth", "pool", cognitotypes.DomainStatusTypeDeleting, 20),
			wantErr:     "timed out",
			wantElapsed: time.Minute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeUserPoolDomainAPI{
				deleteOutputs:   []*cognitoidentityprovider.DeleteUserPoolDomainOutput{{}},
				describeOutputs: tt.outputs,
				describeErrors:  tt.errors,
			}
			clock := &instantUserPoolClock{}

			err := (&UserPoolDomainResource{}).delete(context.Background(), client, prior, clock)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tt.wantSleeps != nil {
				assert.Equal(t, tt.wantSleeps, clock.sleeps)
			}
			if tt.wantElapsed != 0 {
				assert.Equal(t, tt.wantElapsed, clock.now.Sub(time.Time{}))
			}
		})
	}
}

func TestUserPoolDomainWaiterCadenceAndAbsence(t *testing.T) {
	client := &fakeUserPoolDomainAPI{
		describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeCreating),
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeUpdating),
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeCreating),
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeUpdating),
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeCreating),
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeUpdating),
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeCreating),
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeUpdating),
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeActive),
		},
	}
	clock := &instantUserPoolClock{}

	err := waitUserPoolDomainActive(context.Background(), client, clock, "auth", time.Hour)

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
		3200 * time.Millisecond,
		6400 * time.Millisecond,
		10 * time.Second,
		10 * time.Second,
	}, clock.sleeps)

	absentClient := &fakeUserPoolDomainAPI{
		describeOutputs: make([]*cognitoidentityprovider.DescribeUserPoolDomainOutput, 21),
	}
	clock = &instantUserPoolClock{}
	err = waitUserPoolDomainActive(context.Background(), absentClient, clock, "auth", time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absent for 21 observations")

	deleteClient := &fakeUserPoolDomainAPI{
		describeErrors: []error{&cognitotypes.ResourceNotFoundException{}},
	}
	err = waitUserPoolDomainAbsent(context.Background(), deleteClient, &instantUserPoolClock{},
		"auth", time.Hour)
	require.NoError(t, err)
}

func TestUserPoolDomainWaiterResetsAbsenceAndHonorsCancellation(t *testing.T) {
	resetClient := &fakeUserPoolDomainAPI{}
	for range 20 {
		resetClient.describeOutputs = append(resetClient.describeOutputs, nil)
	}
	resetClient.describeOutputs = append(resetClient.describeOutputs,
		domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeCreating),
		domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeActive),
	)

	err := waitUserPoolDomainActive(
		context.Background(),
		resetClient,
		&instantUserPoolClock{},
		"auth",
		time.Hour,
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelClient := &fakeUserPoolDomainAPI{
		describeOutputs: []*cognitoidentityprovider.DescribeUserPoolDomainOutput{
			domainDescribe("auth", "pool", cognitotypes.DomainStatusTypeCreating),
		},
	}
	err = waitUserPoolDomainActive(ctx, cancelClient, &instantUserPoolClock{}, "auth", time.Hour)
	require.ErrorIs(t, err, context.Canceled)
}

func TestUserPoolDomainValidateInputs(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*UserPoolDomainResource)
		wantErr string
	}{
		{name: "valid complete"},
		{
			name: "empty certificate omitted",
			change: func(r *UserPoolDomainResource) {
				empty := ""
				r.CustomDomainConfig = &UserPoolDomainCustomDomainConfig{
					CertificateARN: &empty,
				}
				r.Routing = nil
			},
		},
		{
			name: "domain too long",
			change: func(r *UserPoolDomainResource) {
				r.Domain = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijkl"
			},
			wantErr: "domain must contain 1 to 63 characters",
		},
		{
			name: "bad managed login",
			change: func(r *UserPoolDomainResource) {
				r.ManagedLoginVersion = aws.Int32(3)
			},
			wantErr: "managed-login-version must be 1 or 2",
		},
		{
			name: "malformed certificate arn",
			change: func(r *UserPoolDomainResource) {
				r.CustomDomainConfig.CertificateARN = aws.String("not-an-arn")
			},
			wantErr: "certificate-arn must be a valid ARN",
		},
		{
			name: "invalid security policy",
			change: func(r *UserPoolDomainResource) {
				r.CustomDomainConfig.SecurityPolicy = aws.String("TLS_V2")
			},
			wantErr: "security-policy is invalid",
		},
		{
			name: "security policy requires certificate",
			change: func(r *UserPoolDomainResource) {
				r.CustomDomainConfig.CertificateARN = nil
				r.Routing = nil
			},
			wantErr: "security-policy requires certificate-arn",
		},
		{
			name: "routing requires custom domain",
			change: func(r *UserPoolDomainResource) {
				r.CustomDomainConfig = nil
			},
			wantErr: "routing.failover requires a custom-domain certificate",
		},
		{
			name: "routing requires health check",
			change: func(r *UserPoolDomainResource) {
				r.Routing.Failover.PrimaryRoute53HealthCheckID = ""
			},
			wantErr: "primary-route53-health-check-id is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := completeUserPoolDomainResource()
			if tt.change != nil {
				tt.change(&resource)
			}
			err := resource.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestUserPoolDomainCertificateARNUsesGenericARNValidation(t *testing.T) {
	tests := []struct {
		name    string
		arn     string
		wantErr string
	}{
		{
			name: "empty account",
			arn:  "arn:aws:iam:::role/example",
		},
		{
			name: "twelve digit account",
			arn:  "arn:aws:iam::123456789012:role/example",
		},
		{
			name: "aws account",
			arn:  "arn:aws:iam::aws:role/example",
		},
		{
			name: "aws managed account",
			arn:  "arn:aws:iam::aws-managed:role/example",
		},
		{
			name: "third party account",
			arn:  "arn:aws:iam::third-party:role/example",
		},
		{
			name: "aws marketplace account",
			arn:  "arn:aws:iam::aws-marketplace:role/example",
		},
		{
			name: "partner managed account",
			arn:  "arn:aws:iam::partner-managed:role/example",
		},
		{
			name: "cloudwatch service-linked account",
			arn:  "arn:aws:iam::cw1234567890:role/example",
		},
		{
			name: "provider-valid empty service",
			arn:  "arn:aws::us-east-1:123456789012:resource",
		},
		{
			name: "valid regional arn",
			arn:  "arn:aws:iam:us-east-1:123456789012:role/example",
		},
		{
			name:    "invalid account",
			arn:     "arn:aws:iam::not-valid:role/example",
			wantErr: "certificate-arn must be a valid ARN",
		},
		{
			name:    "short numeric account",
			arn:     "arn:aws:iam::12345678901:role/example",
			wantErr: "certificate-arn must be a valid ARN",
		},
		{
			name:    "invalid partition",
			arn:     "arn:aws1:iam::123456789012:role/example",
			wantErr: "certificate-arn must be a valid ARN",
		},
		{
			name:    "invalid region",
			arn:     "arn:aws:iam:us_east_1:123456789012:role/example",
			wantErr: "certificate-arn must be a valid ARN",
		},
		{
			name:    "empty resource",
			arn:     "arn:aws:iam::123456789012:",
			wantErr: "certificate-arn must be a valid ARN",
		},
		{
			name:    "malformed arn",
			arn:     "not-an-arn",
			wantErr: "certificate-arn must be a valid ARN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certificate := tt.arn
			resource := UserPoolDomainResource{
				Domain:     "auth.example.com",
				UserPoolID: "us-east-1_pool",
				CustomDomainConfig: &UserPoolDomainCustomDomainConfig{
					CertificateARN: &certificate,
				},
			}

			err := resource.ValidateInputs(context.Background(), nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			input := resource.createDomainInput()
			require.NotNil(t, input.CustomDomainConfig)
			assert.Equal(t, tt.arn, aws.ToString(input.CustomDomainConfig.CertificateArn))
		})
	}
}

func domainDescribe(
	domain string,
	poolID string,
	status cognitotypes.DomainStatusType,
) *cognitoidentityprovider.DescribeUserPoolDomainOutput {
	return &cognitoidentityprovider.DescribeUserPoolDomainOutput{
		DomainDescription: &cognitotypes.DomainDescriptionType{
			Domain:     aws.String(domain),
			UserPoolId: aws.String(poolID),
			Status:     status,
		},
	}
}

func repeatedDomainStatus(
	domain string,
	poolID string,
	status cognitotypes.DomainStatusType,
	count int,
) []*cognitoidentityprovider.DescribeUserPoolDomainOutput {
	out := make([]*cognitoidentityprovider.DescribeUserPoolDomainOutput, count)
	for i := range out {
		out[i] = domainDescribe(domain, poolID, status)
	}
	return out
}

func completeDomainDescribe(
	domain string,
	poolID string,
	status cognitotypes.DomainStatusType,
) *cognitoidentityprovider.DescribeUserPoolDomainOutput {
	out := domainDescribe(domain, poolID, status)
	out.DomainDescription.CustomDomainConfig = &cognitotypes.CustomDomainConfigType{
		CertificateArn: aws.String("arn:aws:acm:us-east-1:123456789012:certificate/abc"),
		SecurityPolicy: cognitotypes.SecurityPolicyTypeTlsV132025,
	}
	out.DomainDescription.ManagedLoginVersion = aws.Int32(2)
	out.DomainDescription.Routing = &cognitotypes.RoutingType{
		Failover: &cognitotypes.FailoverType{
			PrimaryRoute53HealthCheckId: aws.String("hc-primary"),
			SecondaryRegion:             aws.String("us-west-2"),
		},
	}
	out.DomainDescription.AWSAccountId = aws.String("123456789012")
	out.DomainDescription.CloudFrontDistribution = aws.String("d111.cloudfront.net")
	out.DomainDescription.S3Bucket = aws.String("bucket")
	out.DomainDescription.Version = aws.String("v1")
	return out
}

func completeUserPoolDomainResource() UserPoolDomainResource {
	return UserPoolDomainResource{
		Domain:              "auth.example.com",
		UserPoolID:          "us-east-1_pool",
		CustomDomainConfig:  completeUserPoolDomainCustomConfig(),
		ManagedLoginVersion: aws.Int32(2),
		Routing:             completeUserPoolDomainRouting(),
	}
}

func completeUserPoolDomainCustomConfig() *UserPoolDomainCustomDomainConfig {
	return &UserPoolDomainCustomDomainConfig{
		CertificateARN: aws.String("arn:aws:acm:us-east-1:123456789012:certificate/abc"),
		SecurityPolicy: aws.String("TLS_V1_3_2025"),
	}
}

func completeUserPoolDomainRouting() *UserPoolDomainRouting {
	return &UserPoolDomainRouting{
		Failover: &UserPoolDomainFailover{
			PrimaryRoute53HealthCheckID: "hc-primary",
			SecondaryRegion:             "us-west-2",
		},
	}
}

func noSuchDomainError() error {
	return &cognitotypes.InvalidParameterException{Message: aws.String("No such domain")}
}
