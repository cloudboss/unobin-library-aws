package opensearch

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainIdentityCenterCreateFollowUpGatesDisabledSubordinates(t *testing.T) {
	resource := DomainResource{
		DomainName: "example",
		IdentityCenterOptions: &DomainIdentityCenterOptions{
			EnabledAPIAccess:          aws.Bool(false),
			IdentityCenterInstanceARN: stringPointer("not-an-arn"),
			RolesKey:                  stringPointer("invalid-role-key"),
			SubjectKey:                stringPointer("invalid-subject-key"),
		},
	}

	err := resource.ValidateInputs(context.Background(), nil)
	require.NoError(t, err)
	followUps, err := resource.createFollowUpInputs()
	require.NoError(t, err)
	require.Len(t, followUps, 1)
	assertDisabledIdentityCenterRequest(t, followUps[0].IdentityCenterOptions)
}

func TestDomainIdentityCenterUpdateDisableGatesStaleSubordinates(t *testing.T) {
	prior := DomainResource{
		DomainName: "example",
		IdentityCenterOptions: &DomainIdentityCenterOptions{
			EnabledAPIAccess: aws.Bool(true),
			IdentityCenterInstanceARN: stringPointer(
				"arn:aws:sso:::instance/ssoins-1234567890123456",
			),
			RolesKey:   stringPointer("GroupName"),
			SubjectKey: stringPointer("UserName"),
		},
	}
	current := DomainResource{
		DomainName: "example",
		IdentityCenterOptions: &DomainIdentityCenterOptions{
			EnabledAPIAccess:          aws.Bool(false),
			IdentityCenterInstanceARN: stringPointer("not-an-arn"),
			RolesKey:                  stringPointer("invalid-role-key"),
			SubjectKey:                stringPointer("invalid-subject-key"),
		},
	}

	input, needed, err := current.updateConfigInput(prior, "example")

	require.NoError(t, err)
	require.True(t, needed)
	assertDisabledIdentityCenterRequest(t, input.IdentityCenterOptions)
}

func assertDisabledIdentityCenterRequest(
	t *testing.T,
	options *awstypes.IdentityCenterOptionsInput,
) {
	t.Helper()
	require.NotNil(t, options)
	require.NotNil(t, options.EnabledAPIAccess)
	assert.False(t, aws.ToBool(options.EnabledAPIAccess))
	assert.Nil(t, options.IdentityCenterInstanceARN)
	assert.Nil(t, options.IdentityCenterInstanceRegion)
	assert.Empty(t, options.RolesKey)
	assert.Empty(t, options.SubjectKey)
}
