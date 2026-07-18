package opensearch

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainUpdateConfigInputSkipsEngineOnlyChange(t *testing.T) {
	prior := DomainResource{DomainName: "example", EngineVersion: stringPointer("OpenSearch_2.11")}
	current := DomainResource{
		DomainName:    "example",
		EngineVersion: stringPointer("OpenSearch_2.13"),
	}
	input, needed, err := current.updateConfigInput(prior, "example")
	require.NoError(t, err)
	assert.False(t, needed)
	assert.Nil(t, input)
}

func TestDomainUpdateConfigInputUsesSemanticPolicyEquality(t *testing.T) {
	priorPolicy := `{"Statement":[{"Effect":"Allow","Action":["es:Get","es:List"]}]}`
	currentPolicy := `{
		"Statement": [{"Action": ["es:List", "es:Get"], "Effect": "Allow"}]
	}`
	prior := DomainResource{DomainName: "example", AccessPolicies: &priorPolicy}
	current := DomainResource{DomainName: "example", AccessPolicies: &currentPolicy}
	input, needed, err := current.updateConfigInput(prior, "example")
	require.NoError(t, err)
	assert.False(t, needed)
	assert.Nil(t, input)
}

func TestDomainPolicyEquivalenceMatchesPinnedIAMSemantics(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
	}{
		{
			name: "scalar and singleton action",
			left: `{"Statement":[{"Effect":"Allow","Action":"es:Get","Resource":"*"}]}`,
			right: `{"Statement":[{"Effect":"Allow","Action":["es:Get"],` +
				`"Resource":"*"}]}`,
		},
		{
			name: "scalar and singleton principal",
			left: `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::` +
				`123456789012:role/example"},"Action":"es:Get","Resource":"*"}]}`,
			right: `{"Statement":[{"Effect":"Allow","Principal":{"AWS":["arn:aws:iam::` +
				`123456789012:role/example"]},"Action":"es:Get","Resource":"*"}]}`,
		},
		{
			name: "scalar and singleton condition",
			left: `{"Statement":[{"Effect":"Allow","Action":"es:Get","Resource":"*",` +
				`"Condition":{"StringEquals":{"aws:PrincipalTag/team":"ops"}}}]}`,
			right: `{"Statement":[{"Effect":"Allow","Action":"es:Get","Resource":"*",` +
				`"Condition":{"StringEquals":{"aws:PrincipalTag/team":["ops"]}}}]}`,
		},
		{
			name:  "effect casing",
			left:  `{"Statement":[{"Effect":"Allow","Action":"es:Get","Resource":"*"}]}`,
			right: `{"Statement":[{"Effect":"allow","Action":"es:Get","Resource":"*"}]}`,
		},
		{
			name: "account ID and root ARN",
			left: `{"Statement":[{"Effect":"Allow","Principal":"123456789012",` +
				`"Action":"es:Get","Resource":"*"}]}`,
			right: `{"Statement":[{"Effect":"Allow","Principal":` +
				`"arn:aws:iam::123456789012:root","Action":"es:Get","Resource":"*"}]}`,
		},
		{
			name:  "statement object and list",
			left:  `{"Statement":{"Effect":"Allow","Action":"es:Get","Resource":"*"}}`,
			right: `{"Statement":[{"Effect":"Allow","Action":"es:Get","Resource":"*"}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			equivalent, _, err := equivalentPolicyJSON(test.left, test.right)
			require.NoError(t, err)
			assert.True(t, equivalent)
		})
	}
}

func TestDomainPolicyNormalizationPreservesListOrder(t *testing.T) {
	document := `{"Statement":[{"Effect":"Allow","Action":["es:List","es:Get"],` +
		`"Resource":"*"}]}`

	normalized, err := normalizePolicyJSON(document)

	require.NoError(t, err)
	assert.JSONEq(t, document, normalized)
	assert.Contains(t, normalized, `"Action":["es:List","es:Get"]`)
}

func TestDomainUpdateConfigInputRejectsInvalidPolicy(t *testing.T) {
	priorPolicy := `{"Statement":[]}`
	currentPolicy := `{invalid`
	prior := DomainResource{DomainName: "example", AccessPolicies: &priorPolicy}
	current := DomainResource{DomainName: "example", AccessPolicies: &currentPolicy}
	_, _, err := current.updateConfigInput(prior, "example")
	require.Error(t, err)
	assert.ErrorContains(t, err, "access-policies")
}

func TestDomainUpdateConfigInputSendsExplicitClears(t *testing.T) {
	priorLogs := []DomainLogPublishingOption{
		{LogType: "INDEX_SLOW_LOGS", CloudWatchLogGroupARN: "old"},
		{LogType: "SEARCH_SLOW_LOGS", CloudWatchLogGroupARN: "removed"},
	}
	currentLogs := []DomainLogPublishingOption{
		{LogType: "INDEX_SLOW_LOGS", CloudWatchLogGroupARN: "new"},
	}
	priorAdvanced := map[string]string{"rest.action.multi.allow_explicit_index": "true"}
	prior := DomainResource{
		DomainName:      "example",
		AdvancedOptions: &priorAdvanced,
		AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
			IAMFederationOptions: &DomainIAMFederationOptions{},
			JWTOptions:           &DomainJWTOptions{},
			SAMLOptions:          &DomainSAMLOptions{},
		},
		AutomatedSnapshotPauseOptions: &DomainAutomatedSnapshotPauseRequestOptions{
			Enabled: true,
		},
		CognitoOptions: &DomainCognitoOptions{},
		DomainEndpointOptions: &DomainEndpointOptions{
			CustomEndpointEnabled: true,
		},
		IdentityCenterOptions: &DomainIdentityCenterOptions{},
		LogPublishingOptions:  &priorLogs,
	}
	current := DomainResource{
		DomainName: "example",
		AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
			Enabled: false,
		},
		LogPublishingOptions: &currentLogs,
	}
	input, needed, err := current.updateConfigInput(prior, "example")
	require.NoError(t, err)
	require.True(t, needed)
	assert.Empty(t, input.AdvancedOptions)
	require.NotNil(t, input.AdvancedSecurityOptions)
	assert.False(t, aws.ToBool(input.AdvancedSecurityOptions.JWTOptions.Enabled))
	assert.False(t, aws.ToBool(input.AdvancedSecurityOptions.SAMLOptions.Enabled))
	assert.False(t, aws.ToBool(input.AdvancedSecurityOptions.IAMFederationOptions.Enabled))
	assert.False(t, aws.ToBool(input.AutomatedSnapshotPauseOptions.Enabled))
	assert.False(t, aws.ToBool(input.CognitoOptions.Enabled))
	assert.False(t, aws.ToBool(input.IdentityCenterOptions.EnabledAPIAccess))
	assert.False(t, aws.ToBool(input.LogPublishingOptions["SEARCH_SLOW_LOGS"].Enabled))
	assert.Nil(t, input.DomainEndpointOptions)
}

func TestDomainUpdateConfigInputOmitsUnsupportedAdvancedSecurityRemoval(t *testing.T) {
	prior := DomainResource{
		DomainName: "example",
		AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
			Enabled: true,
		},
	}
	current := DomainResource{DomainName: "example"}

	input, needed, err := current.updateConfigInput(prior, "example")
	require.NoError(t, err)
	assert.False(t, needed)
	assert.Nil(t, input)
}

func TestDomainUpdateConfigInputResendsExistingEBSOnClusterChange(t *testing.T) {
	ebs := &DomainEBSOptions{
		EBSEnabled: true,
		VolumeSize: int64Pointer(20),
		VolumeType: stringPointer("gp3"),
	}
	prior := DomainResource{
		DomainName:    "example",
		ClusterConfig: &DomainClusterConfig{InstanceCount: 1, InstanceType: "t3.small.search"},
		EBSOptions:    ebs,
	}
	current := DomainResource{
		DomainName:    "example",
		ClusterConfig: &DomainClusterConfig{InstanceCount: 2, InstanceType: "t3.small.search"},
		EBSOptions:    ebs,
	}
	input, needed, err := current.updateConfigInput(prior, "example")
	require.NoError(t, err)
	require.True(t, needed)
	require.NotNil(t, input.ClusterConfig)
	require.NotNil(t, input.EBSOptions)
	assert.Equal(t, int32(20), aws.ToInt32(input.EBSOptions.VolumeSize))

	current.EBSOptions = nil
	input, needed, err = current.updateConfigInput(prior, "example")
	require.NoError(t, err)
	require.True(t, needed)
	assert.Nil(t, input.EBSOptions)
}

func stringPointer(value string) *string { return &value }

func int64Pointer(value int64) *int64 { return &value }
