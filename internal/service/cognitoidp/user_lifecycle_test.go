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

type fakeUserAPI struct {
	createInputs     []*cognitoidentityprovider.AdminCreateUserInput
	createErrors     []error
	deleteInputs     []*cognitoidentityprovider.AdminDeleteUserInput
	deleteErrors     []error
	deleteAttrInputs []*cognitoidentityprovider.AdminDeleteUserAttributesInput
	deleteAttrErrors []error
	disableInputs    []*cognitoidentityprovider.AdminDisableUserInput
	disableErrors    []error
	enableInputs     []*cognitoidentityprovider.AdminEnableUserInput
	enableErrors     []error
	getInputs        []*cognitoidentityprovider.AdminGetUserInput
	getOutputs       []*cognitoidentityprovider.AdminGetUserOutput
	getErrors        []error
	passwordInputs   []*cognitoidentityprovider.AdminSetUserPasswordInput
	passwordErrors   []error
	updateAttrInputs []*cognitoidentityprovider.AdminUpdateUserAttributesInput
	updateAttrErrors []error
}

func (c *fakeUserAPI) AdminCreateUser(
	_ context.Context,
	input *cognitoidentityprovider.AdminCreateUserInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AdminCreateUserOutput, error) {
	c.createInputs = append(c.createInputs, input)
	index := len(c.createInputs) - 1
	return &cognitoidentityprovider.AdminCreateUserOutput{}, valueAt(c.createErrors, index)
}

func (c *fakeUserAPI) AdminDeleteUser(
	_ context.Context,
	input *cognitoidentityprovider.AdminDeleteUserInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AdminDeleteUserOutput, error) {
	c.deleteInputs = append(c.deleteInputs, input)
	index := len(c.deleteInputs) - 1
	return &cognitoidentityprovider.AdminDeleteUserOutput{}, valueAt(c.deleteErrors, index)
}

func (c *fakeUserAPI) AdminDeleteUserAttributes(
	_ context.Context,
	input *cognitoidentityprovider.AdminDeleteUserAttributesInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AdminDeleteUserAttributesOutput, error) {
	c.deleteAttrInputs = append(c.deleteAttrInputs, input)
	index := len(c.deleteAttrInputs) - 1
	return &cognitoidentityprovider.AdminDeleteUserAttributesOutput{},
		valueAt(c.deleteAttrErrors, index)
}

func (c *fakeUserAPI) AdminDisableUser(
	_ context.Context,
	input *cognitoidentityprovider.AdminDisableUserInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AdminDisableUserOutput, error) {
	c.disableInputs = append(c.disableInputs, input)
	index := len(c.disableInputs) - 1
	return &cognitoidentityprovider.AdminDisableUserOutput{}, valueAt(c.disableErrors, index)
}

func (c *fakeUserAPI) AdminEnableUser(
	_ context.Context,
	input *cognitoidentityprovider.AdminEnableUserInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AdminEnableUserOutput, error) {
	c.enableInputs = append(c.enableInputs, input)
	index := len(c.enableInputs) - 1
	return &cognitoidentityprovider.AdminEnableUserOutput{}, valueAt(c.enableErrors, index)
}

func (c *fakeUserAPI) AdminGetUser(
	_ context.Context,
	input *cognitoidentityprovider.AdminGetUserInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AdminGetUserOutput, error) {
	c.getInputs = append(c.getInputs, input)
	index := len(c.getInputs) - 1
	return valueAt(c.getOutputs, index), valueAt(c.getErrors, index)
}

func (c *fakeUserAPI) AdminSetUserPassword(
	_ context.Context,
	input *cognitoidentityprovider.AdminSetUserPasswordInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AdminSetUserPasswordOutput, error) {
	c.passwordInputs = append(c.passwordInputs, input)
	index := len(c.passwordInputs) - 1
	return &cognitoidentityprovider.AdminSetUserPasswordOutput{},
		valueAt(c.passwordErrors, index)
}

func (c *fakeUserAPI) AdminUpdateUserAttributes(
	_ context.Context,
	input *cognitoidentityprovider.AdminUpdateUserAttributesInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AdminUpdateUserAttributesOutput, error) {
	c.updateAttrInputs = append(c.updateAttrInputs, input)
	index := len(c.updateAttrInputs) - 1
	return &cognitoidentityprovider.AdminUpdateUserAttributesOutput{},
		valueAt(c.updateAttrErrors, index)
}

func TestUserCreateOmitsEmptyInputsAndReads(t *testing.T) {
	resource := UserResource{UserPoolID: "us-east-1_pool", Username: "alice"}
	client := &fakeUserAPI{getOutputs: []*cognitoidentityprovider.AdminGetUserOutput{
		userGetOutput("alice"),
	}}

	output, err := resource.create(context.Background(), client)

	require.NoError(t, err)
	require.Len(t, client.createInputs, 1)
	input := client.createInputs[0]
	assert.Equal(t, "us-east-1_pool", aws.ToString(input.UserPoolId))
	assert.Equal(t, "alice", aws.ToString(input.Username))
	assert.Empty(t, input.ClientMetadata)
	assert.Empty(t, input.DesiredDeliveryMediums)
	assert.False(t, input.ForceAliasCreation)
	assert.Empty(t, input.MessageAction)
	assert.Nil(t, input.TemporaryPassword)
	assert.Empty(t, input.UserAttributes)
	assert.Empty(t, input.ValidationData)
	assert.Empty(t, client.disableInputs)
	assert.Empty(t, client.passwordInputs)
	require.Len(t, client.getInputs, 1)
	assert.Equal(t, "us-east-1_pool", aws.ToString(client.getInputs[0].UserPoolId))
	assert.Equal(t, "alice", aws.ToString(client.getInputs[0].Username))
	assert.Equal(t, "sub-value", output.Sub)
}

func TestUserCreateSendsOptionsInOrder(t *testing.T) {
	temporary := "TempPass123!"
	messageAction := "SUPPRESS"
	resource := UserResource{
		UserPoolID: "us-east-1_pool",
		Username:   "alice",
		Attributes: stringMap(
			"email", "alice@example.com",
			"department", "platform",
			"dev:legacy", "value",
		),
		ClientMetadata:         stringMap("trace", "create"),
		ValidationData:         stringMap("risk", "low"),
		DesiredDeliveryMediums: stringSlice("EMAIL", "SMS"),
		Enabled:                aws.Bool(false),
		ForceAliasCreation:     aws.Bool(true),
		MessageAction:          &messageAction,
		TemporaryPassword:      &temporary,
	}
	client := &fakeUserAPI{getOutputs: []*cognitoidentityprovider.AdminGetUserOutput{
		userGetOutput("alice"),
	}}

	_, err := resource.create(context.Background(), client)

	require.NoError(t, err)
	require.Len(t, client.createInputs, 1)
	assert.Equal(t, map[string]string{"trace": "create"}, client.createInputs[0].ClientMetadata)
	assert.Equal(t, []cognitotypes.DeliveryMediumType{"EMAIL", "SMS"},
		client.createInputs[0].DesiredDeliveryMediums)
	assert.True(t, client.createInputs[0].ForceAliasCreation)
	assert.Equal(t, cognitotypes.MessageActionTypeSuppress,
		client.createInputs[0].MessageAction)
	assert.Equal(t, temporary, aws.ToString(client.createInputs[0].TemporaryPassword))
	assert.Equal(t, []cognitotypes.AttributeType{
		{Name: aws.String("custom:department"), Value: aws.String("platform")},
		{Name: aws.String("custom:dev:legacy"), Value: aws.String("value")},
		{Name: aws.String("email"), Value: aws.String("alice@example.com")},
	}, client.createInputs[0].UserAttributes)
	assert.Equal(t, []cognitotypes.AttributeType{
		{Name: aws.String("custom:risk"), Value: aws.String("low")},
	}, client.createInputs[0].ValidationData)
	require.Len(t, client.disableInputs, 1)
	assert.Equal(t, "alice", aws.ToString(client.disableInputs[0].Username))
	assert.Empty(t, client.passwordInputs)
}

func TestUserCreateCleansUpPostCreateFailures(t *testing.T) {
	sentinel := errors.New("sentinel")
	password := "PermPass123!"
	tests := []struct {
		name   string
		mutate func(*UserResource, *fakeUserAPI)
		want   string
	}{
		{
			name: "disable failure",
			mutate: func(r *UserResource, c *fakeUserAPI) {
				r.Enabled = aws.Bool(false)
				c.disableErrors = []error{sentinel}
			},
			want: "disable user alice: sentinel",
		},
		{
			name: "permanent password failure",
			mutate: func(r *UserResource, c *fakeUserAPI) {
				r.Password = &password
				c.passwordErrors = []error{sentinel}
			},
			want: "set user alice password: sentinel",
		},
		{
			name: "read failure",
			mutate: func(_ *UserResource, c *fakeUserAPI) {
				c.getErrors = []error{sentinel}
			},
			want: "get user alice: sentinel",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := UserResource{UserPoolID: "us-east-1_pool", Username: "alice"}
			client := &fakeUserAPI{getOutputs: []*cognitoidentityprovider.AdminGetUserOutput{
				userGetOutput("alice"),
			}}
			tt.mutate(&resource, client)

			_, err := resource.create(context.Background(), client)

			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
			require.Len(t, client.deleteInputs, 1)
			assert.Equal(t, "us-east-1_pool", aws.ToString(client.deleteInputs[0].UserPoolId))
			assert.Equal(t, "alice", aws.ToString(client.deleteInputs[0].Username))
		})
	}
}

func TestUserCreateCleanupSuppressesNotFound(t *testing.T) {
	for _, cleanupErr := range []error{
		&cognitotypes.UserNotFoundException{Message: aws.String("gone")},
		&cognitotypes.ResourceNotFoundException{Message: aws.String("pool gone")},
	} {
		client := &fakeUserAPI{
			getErrors:    []error{errors.New("read denied")},
			deleteErrors: []error{cleanupErr},
		}
		resource := UserResource{UserPoolID: "us-east-1_pool", Username: "alice"}

		_, err := resource.create(context.Background(), client)

		require.Error(t, err)
		assert.ErrorContains(t, err, "get user alice: read denied")
		assert.NotContains(t, err.Error(), "compensating delete user")
		require.Len(t, client.deleteInputs, 1)
	}
}

func TestUserCreateCleanupJoinsDeleteFailure(t *testing.T) {
	client := &fakeUserAPI{
		getErrors:    []error{errors.New("read denied")},
		deleteErrors: []error{errors.New("delete denied")},
	}
	resource := UserResource{UserPoolID: "us-east-1_pool", Username: "alice"}

	_, err := resource.create(context.Background(), client)

	require.Error(t, err)
	assert.ErrorContains(t, err, "get user alice: read denied")
	assert.ErrorContains(t, err, "compensating delete user alice: delete denied")
	require.Len(t, client.deleteInputs, 1)
}

func TestUserCreateDoesNotCleanupCreateFailure(t *testing.T) {
	client := &fakeUserAPI{createErrors: []error{errors.New("create denied")}}
	resource := UserResource{UserPoolID: "us-east-1_pool", Username: "alice"}

	_, err := resource.create(context.Background(), client)

	require.EqualError(t, err, "create user alice: create denied")
	assert.Empty(t, client.deleteInputs)
}

func TestUserCreateSetsPermanentPassword(t *testing.T) {
	password := "PermPass123!"
	resource := UserResource{
		UserPoolID: "us-east-1_pool",
		Username:   "alice",
		Password:   &password,
	}
	client := &fakeUserAPI{getOutputs: []*cognitoidentityprovider.AdminGetUserOutput{
		userGetOutput("alice"),
	}}

	_, err := resource.create(context.Background(), client)

	require.NoError(t, err)
	require.Len(t, client.passwordInputs, 1)
	assert.Equal(t, password, aws.ToString(client.passwordInputs[0].Password))
	assert.True(t, client.passwordInputs[0].Permanent)
}

func TestUserReadMapsOutputAndNotFound(t *testing.T) {
	resource := UserResource{UserPoolID: "us-east-1_pool", Username: "alice"}
	client := &fakeUserAPI{getOutputs: []*cognitoidentityprovider.AdminGetUserOutput{
		userGetOutput("alice"),
	}}

	output, err := resource.read(context.Background(), client)

	require.NoError(t, err)
	assert.Equal(t, "alice", output.Username)
	assert.Equal(t, "us-east-1_pool", output.UserPoolID)
	assert.Equal(t, "2026-07-01T12:00:00Z", output.CreationDate)
	assert.Equal(t, "2026-07-01T12:05:00Z", output.LastModifiedDate)
	assert.True(t, output.Enabled)
	assert.Equal(t, []string{"SMS_MFA"}, output.MFASettingList)
	assert.Equal(t, "SMS_MFA", output.PreferredMFASetting)
	assert.Equal(t, "CONFIRMED", output.Status)
	assert.Equal(t, "sub-value", output.Sub)
	assert.Equal(t, map[string]string{
		"department": "platform",
		"email":      "alice@example.com",
		"legacy":     "legacy-value",
		"sub":        "sub-value",
	}, output.Attributes)

	for _, err := range []error{
		&cognitotypes.UserNotFoundException{Message: aws.String("gone")},
		&cognitotypes.ResourceNotFoundException{Message: aws.String("pool gone")},
	} {
		client := &fakeUserAPI{getErrors: []error{err}}
		_, readErr := resource.read(context.Background(), client)
		assert.ErrorIs(t, readErr, runtime.ErrNotFound)
	}
}

func TestUserReadUsesPriorIdentity(t *testing.T) {
	resource := UserResource{UserPoolID: "us-east-1_desired", Username: "desired"}
	prior := &UserResourceOutput{UserPoolID: "us-east-1_prior", Username: "prior"}
	client := &fakeUserAPI{getOutputs: []*cognitoidentityprovider.AdminGetUserOutput{
		userGetOutput("prior"),
	}}

	output, err := resource.readPrior(context.Background(), client, prior)

	require.NoError(t, err)
	require.Len(t, client.getInputs, 1)
	assert.Equal(t, "us-east-1_prior", aws.ToString(client.getInputs[0].UserPoolId))
	assert.Equal(t, "prior", aws.ToString(client.getInputs[0].Username))
	assert.Equal(t, "us-east-1_prior", output.UserPoolID)
	assert.Equal(t, "prior", output.Username)
}

func TestUserUpdateAttributesEnabledAndTemporaryPassword(t *testing.T) {
	temp := "NewTemp123!"
	priorInputs := UserResource{
		UserPoolID: "us-east-1_pool",
		Username:   "alice",
		Enabled:    aws.Bool(true),
		Attributes: stringMap("department", "old", "legacy", "gone", "sub", "prior-sub"),
	}
	resource := UserResource{
		UserPoolID: "us-east-1_pool",
		Username:   "alice",
		Enabled:    aws.Bool(false),
		Attributes: stringMap(
			"department", "new",
			"email", "alice@example.com",
			"dev:legacy", "value",
		),
		ClientMetadata:    stringMap("trace", "update"),
		TemporaryPassword: &temp,
	}
	client := &fakeUserAPI{getOutputs: []*cognitoidentityprovider.AdminGetUserOutput{
		userGetOutput("alice"),
	}}

	_, err := resource.update(context.Background(), client, userPrior(priorInputs))

	require.NoError(t, err)
	require.Len(t, client.updateAttrInputs, 1)
	assert.Equal(t, map[string]string{"trace": "update"},
		client.updateAttrInputs[0].ClientMetadata)
	assert.Equal(t, []cognitotypes.AttributeType{
		{Name: aws.String("custom:department"), Value: aws.String("new")},
		{Name: aws.String("custom:dev:legacy"), Value: aws.String("value")},
		{Name: aws.String("email"), Value: aws.String("alice@example.com")},
	}, client.updateAttrInputs[0].UserAttributes)
	require.Len(t, client.deleteAttrInputs, 1)
	assert.Equal(t, []string{"custom:legacy"}, client.deleteAttrInputs[0].UserAttributeNames)
	require.Len(t, client.disableInputs, 1)
	require.Len(t, client.passwordInputs, 1)
	assert.Equal(t, temp, aws.ToString(client.passwordInputs[0].Password))
	assert.False(t, client.passwordInputs[0].Permanent)
}

func TestUserUpdatePermanentPasswordAndIgnoresCreateOnlyChanges(t *testing.T) {
	oldPassword := "OldPass123!"
	newPassword := "NewPass123!"
	messageAction := "RESEND"
	priorInputs := UserResource{
		UserPoolID: "us-east-1_pool",
		Username:   "alice",
		Enabled:    aws.Bool(true),
		Password:   &oldPassword,
	}
	resource := UserResource{
		UserPoolID:             "us-east-1_pool",
		Username:               "alice",
		Enabled:                aws.Bool(true),
		Password:               &newPassword,
		ValidationData:         stringMap("ignored", "yes"),
		DesiredDeliveryMediums: stringSlice("EMAIL"),
		ForceAliasCreation:     aws.Bool(true),
		MessageAction:          &messageAction,
	}
	client := &fakeUserAPI{getOutputs: []*cognitoidentityprovider.AdminGetUserOutput{
		userGetOutput("alice"),
	}}

	_, err := resource.update(context.Background(), client, userPrior(priorInputs))

	require.NoError(t, err)
	assert.Empty(t, client.updateAttrInputs)
	assert.Empty(t, client.deleteAttrInputs)
	assert.Empty(t, client.enableInputs)
	assert.Empty(t, client.disableInputs)
	require.Len(t, client.passwordInputs, 1)
	assert.True(t, client.passwordInputs[0].Permanent)

	removed := resource
	removed.Password = nil
	client = &fakeUserAPI{getOutputs: []*cognitoidentityprovider.AdminGetUserOutput{
		userGetOutput("alice"),
	}}
	_, err = removed.update(context.Background(), client, userPrior(resource))
	require.NoError(t, err)
	assert.Empty(t, client.passwordInputs)
}

func TestUserDeleteNotFound(t *testing.T) {
	resource := UserResource{UserPoolID: "us-east-1_pool", Username: "alice"}
	for _, err := range []error{
		&cognitotypes.UserNotFoundException{Message: aws.String("gone")},
		&cognitotypes.ResourceNotFoundException{Message: aws.String("pool gone")},
		nil,
	} {
		client := &fakeUserAPI{deleteErrors: []error{err}}
		assert.NoError(t, resource.delete(context.Background(), client))
	}

	client := &fakeUserAPI{deleteErrors: []error{errors.New("denied")}}
	err := resource.delete(context.Background(), client)
	require.EqualError(t, err, "delete user alice: denied")
}

func TestUserDeleteUsesPriorIdentity(t *testing.T) {
	resource := UserResource{UserPoolID: "us-east-1_desired", Username: "desired"}
	prior := &UserResourceOutput{UserPoolID: "us-east-1_prior", Username: "prior"}
	client := &fakeUserAPI{}

	err := resource.deletePrior(context.Background(), client, prior)

	require.NoError(t, err)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, "us-east-1_prior", aws.ToString(client.deleteInputs[0].UserPoolId))
	assert.Equal(t, "prior", aws.ToString(client.deleteInputs[0].Username))
}

func userPrior(
	inputs UserResource,
) runtime.Prior[UserResource, *UserResourceOutput, *awsCfg] {
	output := &UserResourceOutput{
		UserPoolID: inputs.UserPoolID,
		Username:   inputs.Username,
	}
	return runtime.Prior[UserResource, *UserResourceOutput, *awsCfg]{
		Inputs: inputs, Outputs: output, Observed: output,
	}
}

func userGetOutput(username string) *cognitoidentityprovider.AdminGetUserOutput {
	created := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	modified := created.Add(5 * time.Minute)
	return &cognitoidentityprovider.AdminGetUserOutput{
		Username:             aws.String(username),
		Enabled:              true,
		UserCreateDate:       aws.Time(created),
		UserLastModifiedDate: aws.Time(modified),
		UserMFASettingList:   []string{"SMS_MFA"},
		PreferredMfaSetting:  aws.String("SMS_MFA"),
		UserStatus:           cognitotypes.UserStatusTypeConfirmed,
		UserAttributes: []cognitotypes.AttributeType{
			{Name: aws.String("sub"), Value: aws.String("sub-value")},
			{Name: aws.String("email"), Value: aws.String("alice@example.com")},
			{Name: aws.String("custom:department"), Value: aws.String("platform")},
			{Name: aws.String("dev:legacy"), Value: aws.String("legacy-value")},
		},
	}
}

func stringMap(values ...string) *map[string]string {
	out := map[string]string{}
	for i := 0; i < len(values); i += 2 {
		out[values[i]] = values[i+1]
	}
	return &out
}

func stringSlice(values ...string) *[]string { return &values }
