package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPrimaryID    = "us-east-1_primary"
	testClearID      = "us-east-1_clear"
	testClientID     = "client-id"
	testClientSecret = "client-secret"
	testUserSub      = "user-sub"
)

var testCreationDate = time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)

func TestVerifyAppliedRecordsUserPoolIdentities(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	client := updatedVerifierClient(
		primaryInitialName,
		testPrimaryID,
		cognitotypes.UserPoolTierTypeEssentials,
		false,
		map[string]string{"change": "old", "keep": "1", "remove": "yes"},
		map[string]string{"clear": "yes"},
		clientInitialName,
		5,
		false,
		false,
		map[string]string{"middle_name": "Remove Me", "name": "Initial User"},
	)

	err := verifyApplied(context.Background(), client)

	require.NoError(t, err)
	recorded, err := readRecordedPools()
	require.NoError(t, err)
	assert.Equal(t, expectedRecordedPools(testPrimaryID), recorded)
}

func TestVerifyUpdatedAcceptsInPlaceChangesAndTagClear(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	require.NoError(t, writeRecordedPools(expectedRecordedPools(testPrimaryID)))
	client := updatedVerifierClient(
		primaryUpdatedName,
		testPrimaryID,
		cognitotypes.UserPoolTierTypePlus,
		true,
		map[string]string{"add": "yes", "change": "new", "keep": "1"},
		map[string]string{"aws:retained": "service-owned"},
		clientUpdatedName,
		10,
		true,
		true,
		map[string]string{"name": "Updated User"},
	)

	err := verifyUpdated(context.Background(), client)

	require.NoError(t, err)
}

func TestVerifyUpdatedRejectsReplacement(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	require.NoError(t, writeRecordedPools(expectedRecordedPools(testPrimaryID)))
	client := updatedVerifierClient(
		primaryUpdatedName,
		"us-east-1_replacement",
		cognitotypes.UserPoolTierTypePlus,
		true,
		map[string]string{"add": "yes", "change": "new", "keep": "1"},
		map[string]string{},
		clientUpdatedName,
		10,
		true,
		true,
		map[string]string{"name": "Updated User"},
	)

	err := verifyUpdated(context.Background(), client)

	require.Error(t, err)
	assert.ErrorContains(t, err, "primary user pool identity")
}

func TestVerifyDestroyedAcceptsTypedNotFound(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	require.NoError(t, writeRecordedPools(expectedRecordedPools(testPrimaryID)))
	client := &fakeVerifierClient{
		describeErrors: map[string]error{
			testPrimaryID: &cognitotypes.ResourceNotFoundException{
				Message: aws.String("primary is gone"),
			},
			testClearID: &cognitotypes.ResourceNotFoundException{
				Message: aws.String("clear is gone"),
			},
		},
		describeClientErrors: map[string]error{
			testClientID: &cognitotypes.ResourceNotFoundException{
				Message: aws.String("client is gone"),
			},
		},
		describeDomainErrors: map[string]error{
			domainName: &cognitotypes.ResourceNotFoundException{
				Message: aws.String("domain is gone"),
			},
		},
		getUserErrors: map[string]error{
			userKey(testPrimaryID, userName): &cognitotypes.UserNotFoundException{
				Message: aws.String("user is gone"),
			},
		},
		listClientErrors: map[string]error{
			testPrimaryID: &cognitotypes.ResourceNotFoundException{
				Message: aws.String("pool is gone"),
			},
		},
	}

	err := verifyDestroyed(context.Background(), client)

	require.NoError(t, err)
}

func TestVerifyDomainDestroyedAcceptsEmptySuccessfulDescriptions(t *testing.T) {
	tests := []struct {
		name   string
		output *cognitoidentityprovider.DescribeUserPoolDomainOutput
	}{
		{name: "nil output"},
		{
			name:   "nil description",
			output: &cognitoidentityprovider.DescribeUserPoolDomainOutput{},
		},
		{
			name: "empty status",
			output: &cognitoidentityprovider.DescribeUserPoolDomainOutput{
				DomainDescription: &cognitotypes.DomainDescriptionType{
					Domain: aws.String(domainName),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeVerifierClient{
				describeDomainOutputs: map[string]*cognitoidentityprovider.DescribeUserPoolDomainOutput{
					domainName: tt.output,
				},
			}

			err := verifyDomainDestroyed(context.Background(), client, domainIdentity{
				Domain: domainName,
			})

			require.NoError(t, err)
		})
	}
}

func TestVerifyDomainDestroyedRejectsUnrelatedError(t *testing.T) {
	client := &fakeVerifierClient{
		describeDomainErrors: map[string]error{
			domainName: errors.New("access denied"),
		},
	}

	err := verifyDomainDestroyed(context.Background(), client, domainIdentity{
		Domain: domainName,
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "describe destroyed user pool domain")
	assert.ErrorContains(t, err, "access denied")
}

func TestVerifyDomainDestroyedRejectsActiveDescription(t *testing.T) {
	client := &fakeVerifierClient{
		describeDomainOutputs: map[string]*cognitoidentityprovider.DescribeUserPoolDomainOutput{
			domainName: userPoolDomainOutput(testPrimaryID, 2),
		},
	}

	err := verifyDomainDestroyed(context.Background(), client, domainIdentity{
		Domain: domainName,
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "user pool domain unobin-it-user-pool-domain still exists")
}

func TestVerifyDestroyedRejectsUnrelatedError(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	require.NoError(t, writeRecordedPools(expectedRecordedPools(testPrimaryID)))
	client := &fakeVerifierClient{
		describeErrors: map[string]error{
			testPrimaryID: errors.New("access denied"),
			testClearID: &cognitotypes.ResourceNotFoundException{
				Message: aws.String("clear is gone"),
			},
		},
		describeClientErrors: map[string]error{
			testClientID: &cognitotypes.ResourceNotFoundException{
				Message: aws.String("client is gone"),
			},
		},
		getUserErrors: map[string]error{
			userKey(testPrimaryID, userName): &cognitotypes.ResourceNotFoundException{
				Message: aws.String("pool is gone"),
			},
		},
		listClientErrors: map[string]error{
			testPrimaryID: &cognitotypes.ResourceNotFoundException{
				Message: aws.String("pool is gone"),
			},
		},
	}

	err := verifyDestroyed(context.Background(), client)

	require.Error(t, err)
	assert.ErrorContains(t, err, "access denied")
}

func TestFindUserPoolIDPaginates(t *testing.T) {
	client := &fakeVerifierClient{
		listPages: map[string]*cognitoidentityprovider.ListUserPoolsOutput{
			"": {
				NextToken: aws.String("next"),
				UserPools: []cognitotypes.UserPoolDescriptionType{{
					Id: aws.String("unrelated"), Name: aws.String("unrelated"),
				}},
			},
			"next": {
				UserPools: []cognitotypes.UserPoolDescriptionType{{
					Id: aws.String(testPrimaryID), Name: aws.String(primaryInitialName),
				}},
			},
		},
	}

	id, err := findUserPoolID(context.Background(), client, primaryInitialName)

	require.NoError(t, err)
	assert.Equal(t, testPrimaryID, id)
}

func TestManagedTagsExcludesAWSKeys(t *testing.T) {
	actual := managedTags(map[string]string{
		"aws:service": "owned",
		"unobin":      "managed",
	})

	assert.Equal(t, map[string]string{"unobin": "managed"}, actual)
}

type fakeVerifierClient struct {
	listPages             map[string]*cognitoidentityprovider.ListUserPoolsOutput
	describeOutputs       map[string]*cognitoidentityprovider.DescribeUserPoolOutput
	describeErrors        map[string]error
	mfaOutputs            map[string]*cognitoidentityprovider.GetUserPoolMfaConfigOutput
	tagOutputs            map[string]*cognitoidentityprovider.ListTagsForResourceOutput
	listClientPages       map[string]*cognitoidentityprovider.ListUserPoolClientsOutput
	listClientErrors      map[string]error
	describeDomainOutputs map[string]*cognitoidentityprovider.DescribeUserPoolDomainOutput
	describeDomainErrors  map[string]error
	describeClientOutputs map[string]*cognitoidentityprovider.DescribeUserPoolClientOutput
	describeClientErrors  map[string]error
	getUserOutputs        map[string]*cognitoidentityprovider.AdminGetUserOutput
	getUserErrors         map[string]error
}

func (c *fakeVerifierClient) DescribeUserPoolDomain(
	_ context.Context,
	input *cognitoidentityprovider.DescribeUserPoolDomainInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.DescribeUserPoolDomainOutput, error) {
	domain := aws.ToString(input.Domain)
	if err := c.describeDomainErrors[domain]; err != nil {
		return nil, err
	}
	return c.describeDomainOutputs[domain], nil
}

func (c *fakeVerifierClient) DescribeUserPool(
	_ context.Context,
	input *cognitoidentityprovider.DescribeUserPoolInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.DescribeUserPoolOutput, error) {
	id := aws.ToString(input.UserPoolId)
	if err := c.describeErrors[id]; err != nil {
		return nil, err
	}
	return c.describeOutputs[id], nil
}

func (c *fakeVerifierClient) GetUserPoolMfaConfig(
	_ context.Context,
	input *cognitoidentityprovider.GetUserPoolMfaConfigInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.GetUserPoolMfaConfigOutput, error) {
	return c.mfaOutputs[aws.ToString(input.UserPoolId)], nil
}

func (c *fakeVerifierClient) ListTagsForResource(
	_ context.Context,
	input *cognitoidentityprovider.ListTagsForResourceInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.ListTagsForResourceOutput, error) {
	return c.tagOutputs[aws.ToString(input.ResourceArn)], nil
}

func (c *fakeVerifierClient) ListUserPools(
	_ context.Context,
	input *cognitoidentityprovider.ListUserPoolsInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.ListUserPoolsOutput, error) {
	return c.listPages[aws.ToString(input.NextToken)], nil
}

func (c *fakeVerifierClient) ListUserPoolClients(
	_ context.Context,
	input *cognitoidentityprovider.ListUserPoolClientsInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.ListUserPoolClientsOutput, error) {
	if err := c.listClientErrors[aws.ToString(input.UserPoolId)]; err != nil {
		return nil, err
	}
	return c.listClientPages[aws.ToString(input.NextToken)], nil
}

func (c *fakeVerifierClient) DescribeUserPoolClient(
	_ context.Context,
	input *cognitoidentityprovider.DescribeUserPoolClientInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.DescribeUserPoolClientOutput, error) {
	id := aws.ToString(input.ClientId)
	if err := c.describeClientErrors[id]; err != nil {
		return nil, err
	}
	return c.describeClientOutputs[id], nil
}

func (c *fakeVerifierClient) AdminGetUser(
	_ context.Context,
	input *cognitoidentityprovider.AdminGetUserInput,
	_ ...func(*cognitoidentityprovider.Options),
) (*cognitoidentityprovider.AdminGetUserOutput, error) {
	key := userKey(aws.ToString(input.UserPoolId), aws.ToString(input.Username))
	if err := c.getUserErrors[key]; err != nil {
		return nil, err
	}
	return c.getUserOutputs[key], nil
}

func updatedVerifierClient(
	primaryName string,
	primaryID string,
	primaryTier cognitotypes.UserPoolTierType,
	allowAdminCreateOnly bool,
	primaryTags map[string]string,
	clearTags map[string]string,
	clientName string,
	authSessionValidity int32,
	enableTokenRevocation bool,
	userEnabled bool,
	userAttrs map[string]string,
) *fakeVerifierClient {
	primaryARN := userPoolARN(primaryID)
	clearARN := userPoolARN(testClearID)
	return &fakeVerifierClient{
		listPages: map[string]*cognitoidentityprovider.ListUserPoolsOutput{
			"": {
				UserPools: []cognitotypes.UserPoolDescriptionType{
					{Id: aws.String(primaryID), Name: aws.String(primaryName)},
					{Id: aws.String(testClearID), Name: aws.String(clearPoolName)},
				},
			},
		},
		describeOutputs: map[string]*cognitoidentityprovider.DescribeUserPoolOutput{
			primaryID: userPoolOutput(
				primaryName,
				primaryID,
				primaryTier,
				allowAdminCreateOnly,
			),
			testClearID: userPoolOutput(
				clearPoolName,
				testClearID,
				cognitotypes.UserPoolTierTypeEssentials,
				false,
			),
		},
		mfaOutputs: map[string]*cognitoidentityprovider.GetUserPoolMfaConfigOutput{
			primaryID:   {MfaConfiguration: cognitotypes.UserPoolMfaTypeOff},
			testClearID: {MfaConfiguration: cognitotypes.UserPoolMfaTypeOff},
		},
		tagOutputs: map[string]*cognitoidentityprovider.ListTagsForResourceOutput{
			primaryARN: {Tags: primaryTags},
			clearARN:   {Tags: clearTags},
		},
		listClientPages: map[string]*cognitoidentityprovider.ListUserPoolClientsOutput{
			"": {UserPoolClients: []cognitotypes.UserPoolClientDescription{{
				ClientId:   aws.String(testClientID),
				ClientName: aws.String(clientName),
				UserPoolId: aws.String(primaryID),
			}}},
		},
		describeClientOutputs: map[string]*cognitoidentityprovider.DescribeUserPoolClientOutput{
			testClientID: {
				UserPoolClient: &cognitotypes.UserPoolClientType{
					AuthSessionValidity:   aws.Int32(authSessionValidity),
					ClientId:              aws.String(testClientID),
					ClientName:            aws.String(clientName),
					ClientSecret:          aws.String(testClientSecret),
					EnableTokenRevocation: aws.Bool(enableTokenRevocation),
					UserPoolId:            aws.String(primaryID),
				},
			},
		},
		describeDomainOutputs: map[string]*cognitoidentityprovider.DescribeUserPoolDomainOutput{
			domainName: userPoolDomainOutput(primaryID, managedLoginForName(primaryName)),
		},
		getUserOutputs: map[string]*cognitoidentityprovider.AdminGetUserOutput{
			userKey(primaryID, userName): userOutput(userName, userEnabled, userAttrs),
		},
	}
}

func userOutput(
	username string,
	enabled bool,
	attributes map[string]string,
) *cognitoidentityprovider.AdminGetUserOutput {
	userAttributes := make([]cognitotypes.AttributeType, 0, len(attributes)+1)
	userAttributes = append(userAttributes, cognitotypes.AttributeType{
		Name: aws.String("sub"), Value: aws.String(testUserSub),
	})
	for name, value := range attributes {
		userAttributes = append(userAttributes, cognitotypes.AttributeType{
			Name: aws.String(name), Value: aws.String(value),
		})
	}
	return &cognitoidentityprovider.AdminGetUserOutput{
		Enabled:        enabled,
		UserAttributes: userAttributes,
		Username:       aws.String(username),
		UserStatus:     cognitotypes.UserStatusTypeForceChangePassword,
	}
}

func managedLoginForName(primaryName string) int32 {
	if primaryName == primaryUpdatedName {
		return 2
	}
	return 1
}

func userPoolOutput(
	name string,
	id string,
	tier cognitotypes.UserPoolTierType,
	allowAdminCreateOnly bool,
) *cognitoidentityprovider.DescribeUserPoolOutput {
	lastModifiedDate := testCreationDate.Add(time.Minute)
	return &cognitoidentityprovider.DescribeUserPoolOutput{
		UserPool: &cognitotypes.UserPoolType{
			AdminCreateUserConfig: &cognitotypes.AdminCreateUserConfigType{
				AllowAdminCreateUserOnly: allowAdminCreateOnly,
			},
			Arn:                aws.String(userPoolARN(id)),
			CreationDate:       aws.Time(testCreationDate),
			DeletionProtection: cognitotypes.DeletionProtectionTypeInactive,
			Id:                 aws.String(id),
			LastModifiedDate:   aws.Time(lastModifiedDate),
			Name:               aws.String(name),
			UserPoolTier:       tier,
		},
	}
}

func expectedRecordedPools(primaryID string) recordedPools {
	creationDate := testCreationDate.Format(time.RFC3339Nano)
	return recordedPools{
		Primary: poolIdentity{
			ID: primaryID, ARN: userPoolARN(primaryID), CreationDate: creationDate,
		},
		Clear: poolIdentity{
			ID: testClearID, ARN: userPoolARN(testClearID), CreationDate: creationDate,
		},
		Client: clientIdentity{
			PoolID: primaryID,
			ID:     testClientID,
			Secret: testClientSecret,
		},
		Domain: domainIdentity{
			Domain: domainName,
			PoolID: primaryID,
		},
		User: userIdentity{
			PoolID:   primaryID,
			Username: userName,
			Sub:      testUserSub,
		},
	}
}

func userPoolDomainOutput(
	poolID string,
	managedLoginVersion int32,
) *cognitoidentityprovider.DescribeUserPoolDomainOutput {
	return &cognitoidentityprovider.DescribeUserPoolDomainOutput{
		DomainDescription: &cognitotypes.DomainDescriptionType{
			Domain:              aws.String(domainName),
			ManagedLoginVersion: aws.Int32(managedLoginVersion),
			Status:              cognitotypes.DomainStatusTypeActive,
			UserPoolId:          aws.String(poolID),
		},
	}
}

func userKey(poolID string, username string) string {
	return poolID + "/" + username
}

func userPoolARN(id string) string {
	return "arn:aws:cognito-idp:us-east-1:123456789012:userpool/" + id
}
