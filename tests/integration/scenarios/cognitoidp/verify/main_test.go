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
	testPrimaryID = "us-east-1_primary"
	testClearID   = "us-east-1_clear"
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
	}

	err := verifyDestroyed(context.Background(), client)

	require.NoError(t, err)
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
	listPages       map[string]*cognitoidentityprovider.ListUserPoolsOutput
	describeOutputs map[string]*cognitoidentityprovider.DescribeUserPoolOutput
	describeErrors  map[string]error
	mfaOutputs      map[string]*cognitoidentityprovider.GetUserPoolMfaConfigOutput
	tagOutputs      map[string]*cognitoidentityprovider.ListTagsForResourceOutput
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

func updatedVerifierClient(
	primaryName string,
	primaryID string,
	primaryTier cognitotypes.UserPoolTierType,
	allowAdminCreateOnly bool,
	primaryTags map[string]string,
	clearTags map[string]string,
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
	}
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
	}
}

func userPoolARN(id string) string {
	return "arn:aws:cognito-idp:us-east-1:123456789012:userpool/" + id
}
