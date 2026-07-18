package opensearch

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainJWTRequestsCompactPublicKeyNewlines(t *testing.T) {
	publicKey := "first\r\nsecond\nthird\rfourth"
	advanced := &DomainAdvancedSecurityOptions{
		Enabled: true,
		JWTOptions: &DomainJWTOptions{
			Enabled:   aws.Bool(true),
			PublicKey: &publicKey,
		},
	}

	t.Run("create", func(t *testing.T) {
		input, err := (DomainResource{
			DomainName:              "example",
			AdvancedSecurityOptions: advanced,
		}).createInput()
		require.NoError(t, err)
		require.NotNil(t, input.AdvancedSecurityOptions)
		require.NotNil(t, input.AdvancedSecurityOptions.JWTOptions)
		assert.Equal(t, "firstsecondthirdfourth",
			aws.ToString(input.AdvancedSecurityOptions.JWTOptions.PublicKey))
	})

	t.Run("update", func(t *testing.T) {
		priorKey := "prior"
		prior := DomainResource{
			DomainName: "example",
			AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
				Enabled: true,
				JWTOptions: &DomainJWTOptions{
					Enabled: aws.Bool(true), PublicKey: &priorKey,
				},
			},
		}
		current := DomainResource{
			DomainName:              "example",
			AdvancedSecurityOptions: advanced,
		}
		input, needed, err := current.updateConfigInput(prior, "example")
		require.NoError(t, err)
		require.True(t, needed)
		require.NotNil(t, input.AdvancedSecurityOptions)
		require.NotNil(t, input.AdvancedSecurityOptions.JWTOptions)
		assert.Equal(t, "firstsecondthirdfourth",
			aws.ToString(input.AdvancedSecurityOptions.JWTOptions.PublicKey))
	})
}

func TestDomainSAMLRequests(t *testing.T) {
	enabled := func() *DomainAdvancedSecurityOptions {
		return &DomainAdvancedSecurityOptions{
			Enabled: true,
			SAMLOptions: &DomainSAMLOptions{
				Enabled:               true,
				IDP:                   &DomainSAMLIDP{EntityID: "entity", MetadataContent: "metadata"},
				MasterBackendRole:     stringPointer("backend"),
				MasterUserName:        stringPointer("master"),
				RolesKey:              stringPointer("roles"),
				SessionTimeoutMinutes: 120,
				SubjectKey:            stringPointer("subject"),
			},
		}
	}
	assertEnabled := func(t *testing.T, options *awstypes.SAMLOptionsInput) {
		t.Helper()
		require.NotNil(t, options)
		assert.True(t, aws.ToBool(options.Enabled))
		require.NotNil(t, options.Idp)
		assert.Equal(t, "entity", aws.ToString(options.Idp.EntityId))
		assert.Equal(t, "metadata", aws.ToString(options.Idp.MetadataContent))
		assert.Equal(t, "backend", aws.ToString(options.MasterBackendRole))
		assert.Equal(t, "master", aws.ToString(options.MasterUserName))
		assert.Equal(t, "roles", aws.ToString(options.RolesKey))
		assert.Equal(t, int32(120), aws.ToInt32(options.SessionTimeoutMinutes))
		assert.Equal(t, "subject", aws.ToString(options.SubjectKey))
	}

	t.Run("create", func(t *testing.T) {
		input, err := (DomainResource{
			DomainName: "example", AdvancedSecurityOptions: enabled(),
		}).createInput()
		require.NoError(t, err)
		require.NotNil(t, input.AdvancedSecurityOptions)
		assertEnabled(t, input.AdvancedSecurityOptions.SAMLOptions)
	})

	t.Run("update", func(t *testing.T) {
		current := DomainResource{
			DomainName: "example", AdvancedSecurityOptions: enabled(),
		}
		input, needed, err := current.updateConfigInput(
			DomainResource{DomainName: "example"}, "example",
		)
		require.NoError(t, err)
		require.True(t, needed)
		require.NotNil(t, input.AdvancedSecurityOptions)
		assertEnabled(t, input.AdvancedSecurityOptions.SAMLOptions)
	})

	t.Run("disable", func(t *testing.T) {
		prior := DomainResource{
			DomainName: "example", AdvancedSecurityOptions: enabled(),
		}
		current := DomainResource{
			DomainName: "example",
			AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
				Enabled: true,
			},
		}
		input, needed, err := current.updateConfigInput(prior, "example")
		require.NoError(t, err)
		require.True(t, needed)
		require.NotNil(t, input.AdvancedSecurityOptions)
		require.NotNil(t, input.AdvancedSecurityOptions.SAMLOptions)
		assert.False(t, aws.ToBool(input.AdvancedSecurityOptions.SAMLOptions.Enabled))
		assert.Nil(t, input.AdvancedSecurityOptions.SAMLOptions.Idp)
	})
}
