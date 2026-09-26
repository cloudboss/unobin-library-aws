package cognitoidp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPoolClientCreate(t *testing.T) {
	name := "application"
	resource := UserPoolClientResource{
		UserPoolID:     "us-east-1_example",
		Name:           &name,
		GenerateSecret: aws.Bool(true),
	}
	client := &fakeUserPoolClientAPI{
		createOutputs: []*cognitoidentityprovider.CreateUserPoolClientOutput{{
			UserPoolClient: &cognitotypes.UserPoolClientType{
				UserPoolId:   aws.String(resource.UserPoolID),
				ClientId:     aws.String("client-id"),
				ClientName:   aws.String(name),
				ClientSecret: aws.String("client-secret"),
			},
		}},
	}

	output, err := resource.create(context.Background(), client, bytes.NewReader(nil))

	require.NoError(t, err)
	assert.Equal(t, &UserPoolClientResourceOutput{
		ID: "client-id", Name: name, ClientSecret: aws.String("client-secret"),
	}, output)
	require.Len(t, client.createInputs, 1)
	assert.Equal(t, resource.UserPoolID, aws.ToString(client.createInputs[0].UserPoolId))
	assert.Equal(t, name, aws.ToString(client.createInputs[0].ClientName))
	assert.True(t, client.createInputs[0].GenerateSecret)
}

type fakeUserPoolClientAPI struct {
	createInputs   []*cognitoidentityprovider.CreateUserPoolClientInput
	createOutputs  []*cognitoidentityprovider.CreateUserPoolClientOutput
	createErrors   []error
	describeInputs []*cognitoidentityprovider.DescribeUserPoolClientInput
	describeOutput []*cognitoidentityprovider.DescribeUserPoolClientOutput
	describeErrors []error
	updateInputs   []*cognitoidentityprovider.UpdateUserPoolClientInput
	updateOutputs  []*cognitoidentityprovider.UpdateUserPoolClientOutput
	updateErrors   []error
	deleteInputs   []*cognitoidentityprovider.DeleteUserPoolClientInput
	deleteOutputs  []*cognitoidentityprovider.DeleteUserPoolClientOutput
	deleteErrors   []error
}

func (c *fakeUserPoolClientAPI) CreateUserPoolClient(
	_ context.Context,
	input *cognitoidentityprovider.CreateUserPoolClientInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.CreateUserPoolClientOutput, error) {
	c.createInputs = append(c.createInputs, input)
	index := len(c.createInputs) - 1
	return valueAt(c.createOutputs, index), valueAt(c.createErrors, index)
}

func (c *fakeUserPoolClientAPI) DeleteUserPoolClient(
	_ context.Context,
	input *cognitoidentityprovider.DeleteUserPoolClientInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.DeleteUserPoolClientOutput, error) {
	c.deleteInputs = append(c.deleteInputs, input)
	index := len(c.deleteInputs) - 1
	return valueAt(c.deleteOutputs, index), valueAt(c.deleteErrors, index)
}

func (c *fakeUserPoolClientAPI) DescribeUserPoolClient(
	_ context.Context,
	input *cognitoidentityprovider.DescribeUserPoolClientInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.DescribeUserPoolClientOutput, error) {
	c.describeInputs = append(c.describeInputs, input)
	index := len(c.describeInputs) - 1
	return valueAt(c.describeOutput, index), valueAt(c.describeErrors, index)
}

func (c *fakeUserPoolClientAPI) UpdateUserPoolClient(
	_ context.Context,
	input *cognitoidentityprovider.UpdateUserPoolClientInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.UpdateUserPoolClientOutput, error) {
	c.updateInputs = append(c.updateInputs, input)
	index := len(c.updateInputs) - 1
	return valueAt(c.updateOutputs, index), valueAt(c.updateErrors, index)
}

func valueAt[T any](values []T, index int) T {
	var zero T
	if index >= len(values) {
		return zero
	}
	return values[index]
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func userPoolClientResult(poolID, clientID, name string) *cognitotypes.UserPoolClientType {
	return &cognitotypes.UserPoolClientType{
		UserPoolId: aws.String(poolID),
		ClientId:   aws.String(clientID),
		ClientName: aws.String(name),
	}
}

func userPoolClientPrior(
	inputs UserPoolClientResource,
	outputName string,
) runtime.Prior[UserPoolClientResource, *UserPoolClientResourceOutput, *awsCfg] {
	output := &UserPoolClientResourceOutput{
		ID: "client-id", Name: outputName, ClientSecret: aws.String("old-secret"),
	}
	return runtime.Prior[UserPoolClientResource, *UserPoolClientResourceOutput, *awsCfg]{
		Inputs: inputs, Outputs: output, Observed: output,
	}
}

func ptrSlice(values ...string) *[]string { return &values }

func completeUserPoolClientResource() UserPoolClientResource {
	name := "new-name"
	accessUnit := "minutes"
	idUnit := "minutes"
	refreshUnit := "hours"
	prevent := "ENABLED"
	return UserPoolClientResource{
		UserPoolID:                               "us-east-1_example",
		Name:                                     &name,
		GenerateSecret:                           aws.Bool(true),
		AccessTokenValidity:                      aws.Int32(30),
		IDTokenValidity:                          aws.Int32(45),
		RefreshTokenValidity:                     aws.Int32(24),
		AuthSessionValidity:                      aws.Int32(10),
		AllowedOAuthFlows:                        ptrSlice("code"),
		AllowedOAuthScopes:                       ptrSlice("openid", "email"),
		CallbackURLs:                             ptrSlice("https://example.com/callback"),
		LogoutURLs:                               ptrSlice("https://example.com/logout"),
		ExplicitAuthFlows:                        ptrSlice("ALLOW_USER_SRP_AUTH"),
		ReadAttributes:                           ptrSlice("email"),
		WriteAttributes:                          ptrSlice("name"),
		SupportedIdentityProviders:               ptrSlice("COGNITO"),
		AllowedOAuthFlowsUserPoolClient:          aws.Bool(true),
		EnablePropagateAdditionalUserContextData: aws.Bool(true),
		EnableTokenRevocation:                    aws.Bool(true),
		DefaultRedirectURI:                       aws.String("https://example.com/callback"),
		PreventUserExistenceErrors:               &prevent,
		TokenValidityUnits: &UserPoolClientValidityUnits{
			AccessToken: &accessUnit, IDToken: &idUnit, RefreshToken: &refreshUnit,
		},
		AnalyticsConfiguration: &UserPoolClientAnalytics{
			ApplicationARN: aws.String(
				"arn:aws:mobiletargeting:us-east-1:123456789012:apps/example",
			),
			UserDataShared: aws.Bool(true),
		},
		RefreshTokenRotation: &UserPoolClientRotation{
			Feature: "ENABLED", RetryGracePeriodSeconds: aws.Int32(30),
		},
	}
}

var _ io.Reader = errorReader{}

func TestUserPoolClientCreateGeneratedName(t *testing.T) {
	resource := UserPoolClientResource{UserPoolID: "us-east-1_example"}
	client := &fakeUserPoolClientAPI{createOutputs: []*cognitoidentityprovider.
		CreateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(
			resource.UserPoolID,
			"client-id",
			"unobin-000102030405060708090a0b",
		),
	}}}

	output, err := resource.create(
		context.Background(),
		client,
		bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}),
	)

	require.NoError(t, err)
	assert.Equal(t, "unobin-000102030405060708090a0b", output.Name)
	assert.Equal(t, output.Name, aws.ToString(client.createInputs[0].ClientName))
}

func TestUserPoolClientCreateWithoutSecret(t *testing.T) {
	name := "public-client"
	resource := UserPoolClientResource{
		UserPoolID: "us-east-1_example",
		Name:       &name,
	}
	client := &fakeUserPoolClientAPI{createOutputs: []*cognitoidentityprovider.
		CreateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(resource.UserPoolID, "client-id", name),
	}}}

	output, err := resource.create(context.Background(), client, bytes.NewReader(nil))

	require.NoError(t, err)
	assert.Nil(t, output.ClientSecret)
	require.Len(t, client.createInputs, 1)
	input := client.createInputs[0]
	assert.False(t, input.GenerateSecret)
	assert.Nil(t, input.TokenValidityUnits)
	assert.Nil(t, input.AllowedOAuthFlows)
	assert.Nil(t, input.AllowedOAuthScopes)
	assert.Nil(t, input.CallbackURLs)
	assert.Nil(t, input.LogoutURLs)
	assert.Nil(t, input.ExplicitAuthFlows)
	assert.Nil(t, input.ReadAttributes)
	assert.Nil(t, input.WriteAttributes)
	assert.Nil(t, input.SupportedIdentityProviders)
}

func TestUserPoolClientCreateSendsCompleteConfiguration(t *testing.T) {
	resource := completeUserPoolClientResource()
	client := &fakeUserPoolClientAPI{createOutputs: []*cognitoidentityprovider.
		CreateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(resource.UserPoolID, "client-id", "new-name"),
	}}}

	_, err := resource.create(context.Background(), client, bytes.NewReader(nil))

	require.NoError(t, err)
	require.Len(t, client.createInputs, 1)
	assert.Equal(t, completeUserPoolClientCreateInput(), client.createInputs[0])
}

func completeUserPoolClientCreateInput() *cognitoidentityprovider.CreateUserPoolClientInput {
	return &cognitoidentityprovider.CreateUserPoolClientInput{
		UserPoolId:           aws.String("us-east-1_example"),
		ClientName:           aws.String("new-name"),
		GenerateSecret:       true,
		AccessTokenValidity:  aws.Int32(30),
		IdTokenValidity:      aws.Int32(45),
		RefreshTokenValidity: 24,
		AuthSessionValidity:  aws.Int32(10),
		AllowedOAuthFlows:    []cognitotypes.OAuthFlowType{"code"},
		AllowedOAuthScopes:   []string{"openid", "email"},
		CallbackURLs:         []string{"https://example.com/callback"},
		LogoutURLs:           []string{"https://example.com/logout"},
		ExplicitAuthFlows: []cognitotypes.ExplicitAuthFlowsType{
			"ALLOW_USER_SRP_AUTH",
		},
		ReadAttributes:                           []string{"email"},
		WriteAttributes:                          []string{"name"},
		SupportedIdentityProviders:               []string{"COGNITO"},
		AllowedOAuthFlowsUserPoolClient:          true,
		EnablePropagateAdditionalUserContextData: aws.Bool(true),
		EnableTokenRevocation:                    aws.Bool(true),
		DefaultRedirectURI:                       aws.String("https://example.com/callback"),
		PreventUserExistenceErrors:               cognitotypes.PreventUserExistenceErrorTypesEnabled,
		TokenValidityUnits: &cognitotypes.TokenValidityUnitsType{
			AccessToken:  cognitotypes.TimeUnitsTypeMinutes,
			IdToken:      cognitotypes.TimeUnitsTypeMinutes,
			RefreshToken: cognitotypes.TimeUnitsTypeHours,
		},
		AnalyticsConfiguration: &cognitotypes.AnalyticsConfigurationType{
			ApplicationArn: aws.String(
				"arn:aws:mobiletargeting:us-east-1:123456789012:apps/example",
			),
			UserDataShared: true,
		},
		RefreshTokenRotation: &cognitotypes.RefreshTokenRotationType{
			Feature:                 cognitotypes.FeatureTypeEnabled,
			RetryGracePeriodSeconds: aws.Int32(30),
		},
	}
}

func TestUserPoolClientCreateDistinguishesNilAndEmptyCollections(t *testing.T) {
	name := "public-client"
	resource := UserPoolClientResource{
		UserPoolID:                      "us-east-1_example",
		Name:                            &name,
		AllowedOAuthFlows:               ptrSlice(),
		AllowedOAuthScopes:              ptrSlice(),
		CallbackURLs:                    ptrSlice(),
		LogoutURLs:                      ptrSlice(),
		ExplicitAuthFlows:               ptrSlice(),
		ReadAttributes:                  ptrSlice(),
		WriteAttributes:                 ptrSlice(),
		SupportedIdentityProviders:      ptrSlice(),
		AllowedOAuthFlowsUserPoolClient: aws.Bool(false),
	}
	client := &fakeUserPoolClientAPI{createOutputs: []*cognitoidentityprovider.
		CreateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(resource.UserPoolID, "client-id", name),
	}}}

	_, err := resource.create(context.Background(), client, bytes.NewReader(nil))

	require.NoError(t, err)
	require.Len(t, client.createInputs, 1)
	input := client.createInputs[0]
	assert.NotNil(t, input.AllowedOAuthFlows)
	assert.Empty(t, input.AllowedOAuthFlows)
	assert.NotNil(t, input.AllowedOAuthScopes)
	assert.Empty(t, input.AllowedOAuthScopes)
	assert.NotNil(t, input.CallbackURLs)
	assert.Empty(t, input.CallbackURLs)
	assert.NotNil(t, input.LogoutURLs)
	assert.Empty(t, input.LogoutURLs)
	assert.NotNil(t, input.ExplicitAuthFlows)
	assert.Empty(t, input.ExplicitAuthFlows)
	assert.NotNil(t, input.ReadAttributes)
	assert.Empty(t, input.ReadAttributes)
	assert.NotNil(t, input.WriteAttributes)
	assert.Empty(t, input.WriteAttributes)
	assert.NotNil(t, input.SupportedIdentityProviders)
	assert.Empty(t, input.SupportedIdentityProviders)
}

func TestUserPoolClientCreateErrors(t *testing.T) {
	sentinel := errors.New("sentinel")
	name := "application"
	tests := map[string]struct {
		resource UserPoolClientResource
		client   *fakeUserPoolClientAPI
		random   io.Reader
		want     string
	}{
		"random": {
			resource: UserPoolClientResource{UserPoolID: "us-east-1_example"},
			client:   &fakeUserPoolClientAPI{},
			random:   errorReader{err: sentinel},
			want:     "generate user pool client name: sentinel",
		},
		"api": {
			resource: UserPoolClientResource{UserPoolID: "us-east-1_example", Name: &name},
			client:   &fakeUserPoolClientAPI{createErrors: []error{sentinel}},
			random:   errorReader{err: errors.New("must not read")},
			want:     "create user pool client application: sentinel",
		},
		"empty response": {
			resource: UserPoolClientResource{UserPoolID: "us-east-1_example", Name: &name},
			client:   &fakeUserPoolClientAPI{},
			random:   bytes.NewReader(nil),
			want:     "create user pool client application: empty response",
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			_, err := tt.resource.create(context.Background(), tt.client, tt.random)
			require.EqualError(t, err, tt.want)
		})
	}
}

func TestUserPoolClientRead(t *testing.T) {
	resource := UserPoolClientResource{UserPoolID: "us-east-1_example"}
	client := &fakeUserPoolClientAPI{describeOutput: []*cognitoidentityprovider.
		DescribeUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(resource.UserPoolID, "client-id", "application"),
	}}}
	client.describeOutput[0].UserPoolClient.ClientSecret = aws.String("secret")

	output, err := resource.read(context.Background(), client, "client-id")

	require.NoError(t, err)
	assert.Equal(t, &UserPoolClientResourceOutput{
		ID: "client-id", Name: "application", ClientSecret: aws.String("secret"),
	}, output)
	require.Len(t, client.describeInputs, 1)
	assert.Equal(t, resource.UserPoolID, aws.ToString(client.describeInputs[0].UserPoolId))
	assert.Equal(t, "client-id", aws.ToString(client.describeInputs[0].ClientId))
}

func TestUserPoolClientReadErrors(t *testing.T) {
	resource := UserPoolClientResource{UserPoolID: "us-east-1_example"}
	notFound := &cognitotypes.ResourceNotFoundException{Message: aws.String("gone")}
	tests := map[string]struct {
		client *fakeUserPoolClientAPI
		want   error
		text   string
	}{
		"not found": {
			client: &fakeUserPoolClientAPI{describeErrors: []error{fmt.Errorf("wrapped: %w", notFound)}},
			want:   runtime.ErrNotFound,
		},
		"api": {
			client: &fakeUserPoolClientAPI{describeErrors: []error{errors.New("denied")}},
			text:   "describe user pool client client-id: denied",
		},
		"empty response": {
			client: &fakeUserPoolClientAPI{},
			text:   "describe user pool client client-id: empty response",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := resource.read(context.Background(), tt.client, "client-id")
			if tt.want != nil {
				assert.ErrorIs(t, err, tt.want)
			} else {
				require.EqualError(t, err, tt.text)
			}
		})
	}
}

func TestUserPoolClientUpdateNoOpReturnsObserved(t *testing.T) {
	resource := completeUserPoolClientResource()
	prior := userPoolClientPrior(resource, "new-name")
	prior.Observed = &UserPoolClientResourceOutput{
		ID: "client-id", Name: "new-name", ClientSecret: aws.String("observed-secret"),
	}
	client := &fakeUserPoolClientAPI{}

	output, err := resource.update(
		context.Background(), client, prior, &instantUserPoolClock{},
	)

	require.NoError(t, err)
	assert.Same(t, prior.Observed, output)
	assert.Empty(t, client.updateInputs)
}

func TestUserPoolClientUpdateOmittedNamePreservesObserved(t *testing.T) {
	priorResource := completeUserPoolClientResource()
	resource := priorResource
	resource.Name = nil
	resource.EnableTokenRevocation = aws.Bool(false)
	prior := userPoolClientPrior(priorResource, "recorded-name")
	prior.Observed = &UserPoolClientResourceOutput{
		ID: "client-id", Name: "actual-name", ClientSecret: aws.String("actual-secret"),
	}
	client := &fakeUserPoolClientAPI{updateOutputs: []*cognitoidentityprovider.
		UpdateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(
			resource.UserPoolID, "client-id", "actual-name",
		),
	}}}

	output, err := resource.update(
		context.Background(), client, prior, &instantUserPoolClock{},
	)

	require.NoError(t, err)
	assert.Equal(t, "actual-name", output.Name)
	assert.Equal(t, "actual-secret", aws.ToString(output.ClientSecret))
	require.Len(t, client.updateInputs, 1)
	assert.Equal(t, "actual-name", aws.ToString(client.updateInputs[0].ClientName))
}

func TestUserPoolClientUpdateRepairsNameDrift(t *testing.T) {
	resource := completeUserPoolClientResource()
	prior := userPoolClientPrior(resource, "new-name")
	prior.Observed = &UserPoolClientResourceOutput{
		ID: "client-id", Name: "drifted-name", ClientSecret: aws.String("secret"),
	}
	client := &fakeUserPoolClientAPI{updateOutputs: []*cognitoidentityprovider.
		UpdateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(
			resource.UserPoolID, "client-id", "new-name",
		),
	}}}

	_, err := resource.update(context.Background(), client, prior, &instantUserPoolClock{})

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	assert.Equal(t, "new-name", aws.ToString(client.updateInputs[0].ClientName))
}

func TestUserPoolClientUpdateSendsCompleteConfiguration(t *testing.T) {
	resource := completeUserPoolClientResource()
	priorResource := UserPoolClientResource{
		UserPoolID: resource.UserPoolID,
		Name:       aws.String("old-name"),
		TokenValidityUnits: &UserPoolClientValidityUnits{
			AccessToken:  aws.String("seconds"),
			IDToken:      aws.String("seconds"),
			RefreshToken: aws.String("seconds"),
		},
	}
	prior := userPoolClientPrior(priorResource, "old-name")
	client := &fakeUserPoolClientAPI{updateOutputs: []*cognitoidentityprovider.
		UpdateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(resource.UserPoolID, "client-id", "new-name"),
	}}}

	_, err := resource.update(context.Background(), client, prior, &instantUserPoolClock{})

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	assert.Equal(t, completeUserPoolClientUpdateInput(), client.updateInputs[0])
}

func TestUserPoolClientOneFieldUpdateSendsCompleteConfiguration(t *testing.T) {
	resource := completeUserPoolClientResource()
	priorResource := resource
	priorResource.EnableTokenRevocation = aws.Bool(false)
	prior := userPoolClientPrior(priorResource, "new-name")
	client := &fakeUserPoolClientAPI{updateOutputs: []*cognitoidentityprovider.
		UpdateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(resource.UserPoolID, "client-id", "new-name"),
	}}}

	_, err := resource.update(context.Background(), client, prior, &instantUserPoolClock{})

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	assert.Equal(t, completeUserPoolClientUpdateInput(), client.updateInputs[0])
}

func completeUserPoolClientUpdateInput() *cognitoidentityprovider.UpdateUserPoolClientInput {
	return &cognitoidentityprovider.UpdateUserPoolClientInput{
		UserPoolId:           aws.String("us-east-1_example"),
		ClientId:             aws.String("client-id"),
		ClientName:           aws.String("new-name"),
		AccessTokenValidity:  aws.Int32(30),
		IdTokenValidity:      aws.Int32(45),
		RefreshTokenValidity: 24,
		AuthSessionValidity:  aws.Int32(10),
		AllowedOAuthFlows:    []cognitotypes.OAuthFlowType{"code"},
		AllowedOAuthScopes:   []string{"openid", "email"},
		CallbackURLs:         []string{"https://example.com/callback"},
		LogoutURLs:           []string{"https://example.com/logout"},
		ExplicitAuthFlows: []cognitotypes.ExplicitAuthFlowsType{
			"ALLOW_USER_SRP_AUTH",
		},
		ReadAttributes:                           []string{"email"},
		WriteAttributes:                          []string{"name"},
		SupportedIdentityProviders:               []string{"COGNITO"},
		AllowedOAuthFlowsUserPoolClient:          true,
		EnablePropagateAdditionalUserContextData: aws.Bool(true),
		EnableTokenRevocation:                    aws.Bool(true),
		DefaultRedirectURI:                       aws.String("https://example.com/callback"),
		PreventUserExistenceErrors:               cognitotypes.PreventUserExistenceErrorTypesEnabled,
		TokenValidityUnits: &cognitotypes.TokenValidityUnitsType{
			AccessToken:  cognitotypes.TimeUnitsTypeMinutes,
			IdToken:      cognitotypes.TimeUnitsTypeMinutes,
			RefreshToken: cognitotypes.TimeUnitsTypeHours,
		},
		AnalyticsConfiguration: &cognitotypes.AnalyticsConfigurationType{
			ApplicationArn: aws.String(
				"arn:aws:mobiletargeting:us-east-1:123456789012:apps/example",
			),
			UserDataShared: true,
		},
		RefreshTokenRotation: &cognitotypes.RefreshTokenRotationType{
			Feature:                 cognitotypes.FeatureTypeEnabled,
			RetryGracePeriodSeconds: aws.Int32(30),
		},
	}
}

func TestUserPoolClientUpdateResetsOptionalScalars(t *testing.T) {
	priorResource := completeUserPoolClientResource()
	resource := priorResource
	resource.AccessTokenValidity = nil
	resource.IDTokenValidity = nil
	resource.RefreshTokenValidity = nil
	resource.AuthSessionValidity = nil
	resource.EnableTokenRevocation = nil
	resource.PreventUserExistenceErrors = nil
	prior := userPoolClientPrior(priorResource, "new-name")
	client := &fakeUserPoolClientAPI{updateOutputs: []*cognitoidentityprovider.
		UpdateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(resource.UserPoolID, "client-id", "new-name"),
	}}}

	_, err := resource.update(context.Background(), client, prior, &instantUserPoolClock{})

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	input := client.updateInputs[0]
	assert.Nil(t, input.AccessTokenValidity)
	assert.Nil(t, input.IdTokenValidity)
	assert.Zero(t, input.RefreshTokenValidity)
	assert.Nil(t, input.AuthSessionValidity)
	assert.Nil(t, input.EnableTokenRevocation)
	assert.Empty(t, input.PreventUserExistenceErrors)
}

func TestUserPoolClientUpdateResetsOnlyRemovedTokenUnits(t *testing.T) {
	tests := []struct {
		name            string
		remove          func(*UserPoolClientValidityUnits)
		wantAccessUnit  cognitotypes.TimeUnitsType
		wantIDUnit      cognitotypes.TimeUnitsType
		wantRefreshUnit cognitotypes.TimeUnitsType
	}{
		{
			name: "access token",
			remove: func(units *UserPoolClientValidityUnits) {
				units.AccessToken = nil
			},
			wantAccessUnit:  cognitotypes.TimeUnitsTypeHours,
			wantIDUnit:      cognitotypes.TimeUnitsTypeMinutes,
			wantRefreshUnit: cognitotypes.TimeUnitsTypeHours,
		},
		{
			name: "id token",
			remove: func(units *UserPoolClientValidityUnits) {
				units.IDToken = nil
			},
			wantAccessUnit:  cognitotypes.TimeUnitsTypeMinutes,
			wantIDUnit:      cognitotypes.TimeUnitsTypeHours,
			wantRefreshUnit: cognitotypes.TimeUnitsTypeHours,
		},
		{
			name: "refresh token",
			remove: func(units *UserPoolClientValidityUnits) {
				units.RefreshToken = nil
			},
			wantAccessUnit:  cognitotypes.TimeUnitsTypeMinutes,
			wantIDUnit:      cognitotypes.TimeUnitsTypeMinutes,
			wantRefreshUnit: cognitotypes.TimeUnitsTypeDays,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priorResource := completeUserPoolClientResource()
			priorResource.AccessTokenValidity = aws.Int32(5)
			priorResource.IDTokenValidity = aws.Int32(5)
			priorResource.RefreshTokenValidity = aws.Int32(24)
			resource := priorResource
			units := *priorResource.TokenValidityUnits
			resource.TokenValidityUnits = &units
			tt.remove(resource.TokenValidityUnits)
			prior := userPoolClientPrior(priorResource, "new-name")
			client := &fakeUserPoolClientAPI{updateOutputs: []*cognitoidentityprovider.
				UpdateUserPoolClientOutput{{
				UserPoolClient: userPoolClientResult(
					resource.UserPoolID, "client-id", "new-name",
				),
			}}}

			_, err := resource.update(
				context.Background(), client, prior, &instantUserPoolClock{},
			)

			require.NoError(t, err)
			require.Len(t, client.updateInputs, 1)
			require.NotNil(t, client.updateInputs[0].TokenValidityUnits)
			actual := client.updateInputs[0].TokenValidityUnits
			assert.Equal(t, tt.wantAccessUnit, actual.AccessToken)
			assert.Equal(t, tt.wantIDUnit, actual.IdToken)
			assert.Equal(t, tt.wantRefreshUnit, actual.RefreshToken)
		})
	}
}

func TestUserPoolClientUpdateSendsFalseAnalyticsUserDataShared(t *testing.T) {
	priorResource := completeUserPoolClientResource()
	resource := priorResource
	analytics := *priorResource.AnalyticsConfiguration
	analytics.UserDataShared = aws.Bool(false)
	resource.AnalyticsConfiguration = &analytics
	prior := userPoolClientPrior(priorResource, "new-name")
	client := &fakeUserPoolClientAPI{updateOutputs: []*cognitoidentityprovider.
		UpdateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(resource.UserPoolID, "client-id", "new-name"),
	}}}

	_, err := resource.update(context.Background(), client, prior, &instantUserPoolClock{})

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	require.NotNil(t, client.updateInputs[0].AnalyticsConfiguration)
	assert.False(t, client.updateInputs[0].AnalyticsConfiguration.UserDataShared)
}

func TestUserPoolClientUpdateSecretResolution(t *testing.T) {
	tests := []struct {
		name           string
		observedSecret *string
		responseSecret *string
		wantSecret     string
	}{
		{
			name:       "prior output fallback",
			wantSecret: "recorded-secret",
		},
		{
			name:           "response precedence",
			observedSecret: aws.String("observed-secret"),
			responseSecret: aws.String("response-secret"),
			wantSecret:     "response-secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priorName := "old-name"
			currentName := "new-name"
			priorResource := UserPoolClientResource{
				UserPoolID: "us-east-1_example", Name: &priorName,
			}
			resource := UserPoolClientResource{
				UserPoolID: "us-east-1_example", Name: &currentName,
			}
			prior := runtime.Prior[UserPoolClientResource, *UserPoolClientResourceOutput, *awsCfg]{
				Inputs: priorResource,
				Outputs: &UserPoolClientResourceOutput{
					ID: "client-id", Name: priorName,
					ClientSecret: aws.String("recorded-secret"),
				},
				Observed: &UserPoolClientResourceOutput{
					ID: "client-id", Name: priorName, ClientSecret: tt.observedSecret,
				},
			}
			result := userPoolClientResult(resource.UserPoolID, "client-id", currentName)
			result.ClientSecret = tt.responseSecret
			client := &fakeUserPoolClientAPI{updateOutputs: []*cognitoidentityprovider.
				UpdateUserPoolClientOutput{{UserPoolClient: result}}}

			output, err := resource.update(
				context.Background(), client, prior, &instantUserPoolClock{},
			)

			require.NoError(t, err)
			assert.Equal(t, tt.wantSecret, aws.ToString(output.ClientSecret))
		})
	}
}

func TestUserPoolClientUpdateClearsCollectionsAndResetsRemovedUnits(t *testing.T) {
	priorResource := completeUserPoolClientResource()
	resource := priorResource
	resource.AllowedOAuthFlows = ptrSlice()
	resource.AllowedOAuthScopes = ptrSlice()
	resource.CallbackURLs = ptrSlice()
	resource.LogoutURLs = ptrSlice()
	resource.ExplicitAuthFlows = ptrSlice()
	resource.ReadAttributes = ptrSlice()
	resource.WriteAttributes = ptrSlice()
	resource.SupportedIdentityProviders = ptrSlice()
	resource.AllowedOAuthFlowsUserPoolClient = aws.Bool(false)
	resource.DefaultRedirectURI = nil
	resource.EnablePropagateAdditionalUserContextData = nil
	resource.AnalyticsConfiguration = nil
	resource.RefreshTokenRotation = nil
	resource.TokenValidityUnits = &UserPoolClientValidityUnits{}
	resource.AccessTokenValidity = aws.Int32(1)
	resource.IDTokenValidity = aws.Int32(1)
	prior := userPoolClientPrior(priorResource, "new-name")
	client := &fakeUserPoolClientAPI{updateOutputs: []*cognitoidentityprovider.
		UpdateUserPoolClientOutput{{
		UserPoolClient: userPoolClientResult(resource.UserPoolID, "client-id", "new-name"),
	}}}

	_, err := resource.update(context.Background(), client, prior, &instantUserPoolClock{})

	require.NoError(t, err)
	input := client.updateInputs[0]
	assert.NotNil(t, input.AllowedOAuthFlows)
	assert.Empty(t, input.AllowedOAuthFlows)
	assert.NotNil(t, input.AllowedOAuthScopes)
	assert.Empty(t, input.AllowedOAuthScopes)
	assert.NotNil(t, input.CallbackURLs)
	assert.Empty(t, input.CallbackURLs)
	assert.NotNil(t, input.LogoutURLs)
	assert.Empty(t, input.LogoutURLs)
	assert.NotNil(t, input.ExplicitAuthFlows)
	assert.Empty(t, input.ExplicitAuthFlows)
	assert.NotNil(t, input.ReadAttributes)
	assert.Empty(t, input.ReadAttributes)
	assert.NotNil(t, input.WriteAttributes)
	assert.Empty(t, input.WriteAttributes)
	assert.NotNil(t, input.SupportedIdentityProviders)
	assert.Empty(t, input.SupportedIdentityProviders)
	assert.Nil(t, input.AnalyticsConfiguration)
	assert.Nil(t, input.RefreshTokenRotation)
	require.NotNil(t, input.TokenValidityUnits)
	assert.Equal(t, cognitotypes.TimeUnitsTypeHours, input.TokenValidityUnits.AccessToken)
	assert.Equal(t, cognitotypes.TimeUnitsTypeHours, input.TokenValidityUnits.IdToken)
	assert.Equal(t, cognitotypes.TimeUnitsTypeDays, input.TokenValidityUnits.RefreshToken)
}

func TestUserPoolClientUpdateRetriesConcurrentModification(t *testing.T) {
	resource := completeUserPoolClientResource()
	prior := userPoolClientPrior(UserPoolClientResource{
		UserPoolID: resource.UserPoolID, Name: aws.String("old-name"),
	}, "old-name")
	retryable := &cognitotypes.ConcurrentModificationException{
		Message: aws.String("busy"),
	}
	client := &fakeUserPoolClientAPI{
		updateErrors: []error{fmt.Errorf("wrapped: %w", retryable), nil},
		updateOutputs: []*cognitoidentityprovider.UpdateUserPoolClientOutput{
			nil,
			{UserPoolClient: userPoolClientResult(
				resource.UserPoolID, "client-id", "new-name",
			)},
		},
	}
	clock := &instantUserPoolClock{}

	_, err := resource.update(context.Background(), client, prior, clock)

	require.NoError(t, err)
	assert.Len(t, client.updateInputs, 2)
	assert.Equal(t, []time.Duration{500 * time.Millisecond}, clock.sleeps)
}

func TestUserPoolClientDelete(t *testing.T) {
	resource := UserPoolClientResource{UserPoolID: "not-validated"}
	retryable := &cognitotypes.ConcurrentModificationException{Message: aws.String("busy")}
	client := &fakeUserPoolClientAPI{deleteErrors: []error{retryable, nil}}
	clock := &instantUserPoolClock{}

	err := resource.delete(context.Background(), client, "client-id", clock)

	require.NoError(t, err)
	assert.Len(t, client.deleteInputs, 2)
	for _, input := range client.deleteInputs {
		assert.Equal(t, "not-validated", aws.ToString(input.UserPoolId))
		assert.Equal(t, "client-id", aws.ToString(input.ClientId))
	}
	assert.Equal(t, []time.Duration{500 * time.Millisecond}, clock.sleeps)
}

func TestUserPoolClientDeleteErrors(t *testing.T) {
	resource := UserPoolClientResource{UserPoolID: "us-east-1_example"}
	notFound := &cognitotypes.ResourceNotFoundException{Message: aws.String("gone")}
	tests := map[string]struct {
		err  error
		want string
	}{
		"already absent": {err: fmt.Errorf("wrapped: %w", notFound)},
		"api":            {err: errors.New("denied"), want: "delete user pool client client-id: denied"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeUserPoolClientAPI{deleteErrors: []error{tt.err}}
			err := resource.delete(
				context.Background(), client, "client-id", &instantUserPoolClock{},
			)
			if tt.want == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.want)
			}
		})
	}
}

func TestUserPoolClientDeleteStopsAtRetryTimeout(t *testing.T) {
	resource := UserPoolClientResource{UserPoolID: "us-east-1_example"}
	sentinel := &cognitotypes.ConcurrentModificationException{
		Message: aws.String("busy"),
	}
	errorsByAttempt := make([]error, 16)
	for index := range errorsByAttempt {
		errorsByAttempt[index] = sentinel
	}
	client := &fakeUserPoolClientAPI{deleteErrors: errorsByAttempt}
	clock := &instantUserPoolClock{}

	err := resource.delete(context.Background(), client, "client-id", clock)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.ErrorContains(t, err, "delete user pool client client-id")
	assert.Len(t, client.deleteInputs, 16)
	assert.Equal(t, userPoolRetryTimeout, clock.now.Sub(time.Time{}))
}

func TestUserPoolClientOutputRejectsMalformedResponses(t *testing.T) {
	const poolID = "us-east-1_example"
	const clientID = "client-id"
	const name = "application"
	tests := map[string]struct {
		client *cognitotypes.UserPoolClientType
		want   string
	}{
		"missing details": {want: "response has no user pool client"},
		"wrong pool": {
			client: userPoolClientResult("us-east-1_other", clientID, name),
			want:   "response user pool ID",
		},
		"empty client ID": {
			client: userPoolClientResult(poolID, "", name),
			want:   "response client ID",
		},
		"wrong client ID": {
			client: userPoolClientResult(poolID, "other-id", name),
			want:   "response client ID",
		},
		"empty name": {
			client: userPoolClientResult(poolID, clientID, ""),
			want:   "response client name",
		},
		"wrong name": {
			client: userPoolClientResult(poolID, clientID, "other-name"),
			want:   "response client name",
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			_, err := userPoolClientOutput(
				"test output", tt.client, poolID, clientID, name, nil,
			)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestUserPoolClientRetryPredicate(t *testing.T) {
	concurrent := &cognitotypes.ConcurrentModificationException{
		Message: aws.String("busy"),
	}
	assert.True(t, isUserPoolClientRetryable(concurrent))
	assert.True(t, isUserPoolClientRetryable(fmt.Errorf("wrapped: %w", concurrent)))
	assert.False(t, isUserPoolClientRetryable(
		&cognitotypes.ResourceNotFoundException{Message: aws.String("gone")},
	))
	assert.False(t, isUserPoolClientRetryable(errors.New("busy")))
}

func TestUserPoolClientUpdateStopsAtRetryTimeout(t *testing.T) {
	resource := UserPoolClientResource{
		UserPoolID: "us-east-1_example",
		Name:       aws.String("new-name"),
	}
	priorResource := UserPoolClientResource{
		UserPoolID: resource.UserPoolID,
		Name:       aws.String("old-name"),
	}
	prior := userPoolClientPrior(priorResource, "old-name")
	sentinel := &cognitotypes.ConcurrentModificationException{
		Message: aws.String("busy"),
	}
	errorsByAttempt := make([]error, 16)
	for index := range errorsByAttempt {
		errorsByAttempt[index] = sentinel
	}
	client := &fakeUserPoolClientAPI{updateErrors: errorsByAttempt}
	clock := &instantUserPoolClock{}

	_, err := resource.update(context.Background(), client, prior, clock)

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.ErrorContains(t, err, "update user pool client client-id")
	assert.Len(t, client.updateInputs, 16)
	assert.Equal(t, userPoolRetryTimeout, clock.now.Sub(time.Time{}))
}

func TestPriorUserPoolClientID(t *testing.T) {
	tests := map[string]struct {
		prior *UserPoolClientResourceOutput
		want  string
	}{
		"missing": {want: "user pool client prior output is missing"},
		"empty": {
			prior: &UserPoolClientResourceOutput{},
			want:  "user pool client prior output has no id",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := priorUserPoolClientID(tt.prior)
			require.EqualError(t, err, tt.want)
		})
	}
}
