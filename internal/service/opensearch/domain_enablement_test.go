package opensearch

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainRequestEnablementGates(t *testing.T) {
	tests := []struct {
		name     string
		disabled DomainResource
		enabled  DomainResource
		check    func(*testing.T, *awssdk.CreateDomainInput, bool)
	}{
		{
			name: "dedicated master",
			disabled: resourceWithCluster(DomainClusterConfig{
				DedicatedMasterEnabled: false,
				DedicatedMasterCount:   int64Pointer(3),
				DedicatedMasterType:    stringPointer("m6g.large.search"),
			}),
			enabled: resourceWithCluster(DomainClusterConfig{
				DedicatedMasterEnabled: true,
				DedicatedMasterCount:   int64Pointer(3),
				DedicatedMasterType:    stringPointer("m6g.large.search"),
			}),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				require.NotNil(t, input.ClusterConfig)
				assert.Equal(t, enabled, aws.ToBool(input.ClusterConfig.DedicatedMasterEnabled))
				if enabled {
					assert.Equal(t, int32(3), aws.ToInt32(input.ClusterConfig.DedicatedMasterCount))
					assert.Equal(t, "m6g.large.search", string(input.ClusterConfig.DedicatedMasterType))
					return
				}
				assert.Nil(t, input.ClusterConfig.DedicatedMasterCount)
				assert.Empty(t, input.ClusterConfig.DedicatedMasterType)
			},
		},
		{
			name: "warm nodes",
			disabled: resourceWithCluster(DomainClusterConfig{
				WarmEnabled: aws.Bool(false), WarmCount: 3,
				WarmType: stringPointer("ultrawarm1.medium.search"),
			}),
			enabled: resourceWithCluster(DomainClusterConfig{
				WarmEnabled: aws.Bool(true), WarmCount: 3,
				WarmType: stringPointer("ultrawarm1.medium.search"),
			}),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				require.NotNil(t, input.ClusterConfig)
				assert.Equal(t, enabled, aws.ToBool(input.ClusterConfig.WarmEnabled))
				if enabled {
					assert.Equal(t, int32(3), aws.ToInt32(input.ClusterConfig.WarmCount))
					assert.Equal(t, "ultrawarm1.medium.search", string(input.ClusterConfig.WarmType))
					return
				}
				assert.Nil(t, input.ClusterConfig.WarmCount)
				assert.Empty(t, input.ClusterConfig.WarmType)
			},
		},
		{
			name: "zone awareness",
			disabled: resourceWithCluster(DomainClusterConfig{
				ZoneAwarenessEnabled: aws.Bool(false),
				ZoneAwarenessConfig: &DomainZoneAwarenessConfig{
					AvailabilityZoneCount: 3,
				},
			}),
			enabled: resourceWithCluster(DomainClusterConfig{
				ZoneAwarenessEnabled: aws.Bool(true),
				ZoneAwarenessConfig: &DomainZoneAwarenessConfig{
					AvailabilityZoneCount: 3,
				},
			}),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				require.NotNil(t, input.ClusterConfig)
				assert.Equal(t, enabled, aws.ToBool(input.ClusterConfig.ZoneAwarenessEnabled))
				if enabled {
					require.NotNil(t, input.ClusterConfig.ZoneAwarenessConfig)
					assert.Equal(t, int32(3), aws.ToInt32(
						input.ClusterConfig.ZoneAwarenessConfig.AvailabilityZoneCount,
					))
					return
				}
				assert.Nil(t, input.ClusterConfig.ZoneAwarenessConfig)
			},
		},
		{
			name:     "coordinator node",
			disabled: resourceWithNode(false),
			enabled:  resourceWithNode(true),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				require.Len(t, input.ClusterConfig.NodeOptions, 1)
				config := input.ClusterConfig.NodeOptions[0].NodeConfig
				require.NotNil(t, config)
				assert.Equal(t, enabled, aws.ToBool(config.Enabled))
				if enabled {
					assert.Equal(t, int32(2), aws.ToInt32(config.Count))
					assert.Equal(t, "m6g.large.search", string(config.Type))
					return
				}
				assert.Nil(t, config.Count)
				assert.Empty(t, config.Type)
			},
		},
		{
			name:     "Cognito",
			disabled: resourceWithCognito(false),
			enabled:  resourceWithCognito(true),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				options := input.CognitoOptions
				require.NotNil(t, options)
				assert.Equal(t, enabled, aws.ToBool(options.Enabled))
				if enabled {
					assert.Equal(t, "identity", aws.ToString(options.IdentityPoolId))
					assert.Equal(t, "role", aws.ToString(options.RoleArn))
					assert.Equal(t, "user", aws.ToString(options.UserPoolId))
					return
				}
				assert.Nil(t, options.IdentityPoolId)
				assert.Nil(t, options.RoleArn)
				assert.Nil(t, options.UserPoolId)
			},
		},
		{
			name:     "custom endpoint",
			disabled: resourceWithEndpoint(false),
			enabled:  resourceWithEndpoint(true),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				options := input.DomainEndpointOptions
				require.NotNil(t, options)
				assert.Equal(t, enabled, aws.ToBool(options.CustomEndpointEnabled))
				assert.True(t, aws.ToBool(options.EnforceHTTPS))
				assert.Equal(t, "Policy-Min-TLS-1-2-2019-07", string(options.TLSSecurityPolicy))
				if enabled {
					assert.Equal(t, "search.example.com", aws.ToString(options.CustomEndpoint))
					assert.Equal(t, "certificate", aws.ToString(
						options.CustomEndpointCertificateArn,
					))
					return
				}
				assert.Nil(t, options.CustomEndpoint)
				assert.Nil(t, options.CustomEndpointCertificateArn)
			},
		},
		{
			name:     "EBS",
			disabled: resourceWithEBS(false),
			enabled:  resourceWithEBS(true),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				options := input.EBSOptions
				require.NotNil(t, options)
				assert.Equal(t, enabled, aws.ToBool(options.EBSEnabled))
				if enabled {
					assert.Equal(t, int32(3000), aws.ToInt32(options.Iops))
					assert.Equal(t, int32(125), aws.ToInt32(options.Throughput))
					assert.Equal(t, int32(20), aws.ToInt32(options.VolumeSize))
					assert.Equal(t, "gp3", string(options.VolumeType))
					return
				}
				assert.Nil(t, options.Iops)
				assert.Nil(t, options.Throughput)
				assert.Nil(t, options.VolumeSize)
				assert.Empty(t, options.VolumeType)
			},
		},
		{
			name:     "advanced security",
			disabled: resourceWithAdvancedSecurity(false),
			enabled:  resourceWithAdvancedSecurity(true),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				options := input.AdvancedSecurityOptions
				require.NotNil(t, options)
				assert.Equal(t, enabled, aws.ToBool(options.Enabled))
				if enabled {
					assert.True(t, aws.ToBool(options.AnonymousAuthEnabled))
					assert.True(t, aws.ToBool(options.InternalUserDatabaseEnabled))
					require.NotNil(t, options.MasterUserOptions)
					require.NotNil(t, options.JWTOptions)
					require.NotNil(t, options.SAMLOptions)
					return
				}
				assert.Nil(t, options.AnonymousAuthEnabled)
				assert.Nil(t, options.InternalUserDatabaseEnabled)
				assert.Nil(t, options.MasterUserOptions)
				assert.Nil(t, options.JWTOptions)
				assert.Nil(t, options.SAMLOptions)
			},
		},
		{
			name:     "JWT",
			disabled: resourceWithJWT(false),
			enabled:  resourceWithJWT(true),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				options := input.AdvancedSecurityOptions.JWTOptions
				require.NotNil(t, options)
				assert.Equal(t, enabled, aws.ToBool(options.Enabled))
				if enabled {
					assert.Equal(t, "jwks", aws.ToString(options.JwksUrl))
					assert.Equal(t, "publickey", aws.ToString(options.PublicKey))
					assert.Equal(t, "roles", aws.ToString(options.RolesKey))
					assert.Equal(t, "subject", aws.ToString(options.SubjectKey))
					return
				}
				assert.Nil(t, options.JwksUrl)
				assert.Nil(t, options.PublicKey)
				assert.Nil(t, options.RolesKey)
				assert.Nil(t, options.SubjectKey)
			},
		},
		{
			name:     "SAML",
			disabled: resourceWithSAML(false),
			enabled:  resourceWithSAML(true),
			check: func(t *testing.T, input *awssdk.CreateDomainInput, enabled bool) {
				options := input.AdvancedSecurityOptions.SAMLOptions
				require.NotNil(t, options)
				assert.Equal(t, enabled, aws.ToBool(options.Enabled))
				if enabled {
					require.NotNil(t, options.Idp)
					assert.Equal(t, "entity", aws.ToString(options.Idp.EntityId))
					assert.Equal(t, "metadata", aws.ToString(options.Idp.MetadataContent))
					assert.Equal(t, "backend", aws.ToString(options.MasterBackendRole))
					assert.Equal(t, "master", aws.ToString(options.MasterUserName))
					assert.Equal(t, "roles", aws.ToString(options.RolesKey))
					assert.Equal(t, int32(60), aws.ToInt32(options.SessionTimeoutMinutes))
					assert.Equal(t, "subject", aws.ToString(options.SubjectKey))
					return
				}
				assert.Nil(t, options.Idp)
				assert.Nil(t, options.MasterBackendRole)
				assert.Nil(t, options.MasterUserName)
				assert.Nil(t, options.RolesKey)
				assert.Nil(t, options.SessionTimeoutMinutes)
				assert.Nil(t, options.SubjectKey)
			},
		},
	}
	for _, test := range tests {
		for _, state := range []struct {
			name     string
			resource DomainResource
			enabled  bool
		}{
			{name: "disabled", resource: test.disabled},
			{name: "enabled", resource: test.enabled, enabled: true},
		} {
			t.Run(test.name+"/"+state.name, func(t *testing.T) {
				input, err := state.resource.createInput()
				require.NoError(t, err)
				test.check(t, input, state.enabled)
			})
		}
	}
}

func resourceWithCluster(config DomainClusterConfig) DomainResource {
	config.InstanceCount = 1
	config.InstanceType = "t3.small.search"
	return DomainResource{DomainName: "example", ClusterConfig: &config}
}

func resourceWithNode(enabled bool) DomainResource {
	nodes := []DomainNodeOption{{
		NodeType: "coordinator",
		NodeConfig: &DomainNodeConfig{
			Enabled: aws.Bool(enabled), Count: int64Pointer(2),
			Type: stringPointer("m6g.large.search"),
		},
	}}
	return resourceWithCluster(DomainClusterConfig{NodeOptions: &nodes})
}

func resourceWithCognito(enabled bool) DomainResource {
	return DomainResource{
		DomainName: "example",
		CognitoOptions: &DomainCognitoOptions{
			Enabled: enabled, IdentityPoolID: "identity", RoleARN: "role", UserPoolID: "user",
		},
	}
}

func resourceWithEndpoint(enabled bool) DomainResource {
	return DomainResource{
		DomainName: "example",
		DomainEndpointOptions: &DomainEndpointOptions{
			CustomEndpoint:               stringPointer("search.example.com"),
			CustomEndpointCertificateARN: stringPointer("certificate"),
			CustomEndpointEnabled:        enabled,
			EnforceHTTPS:                 true,
			TLSSecurityPolicy:            stringPointer("Policy-Min-TLS-1-2-2019-07"),
		},
	}
}

func resourceWithEBS(enabled bool) DomainResource {
	return DomainResource{
		DomainName: "example",
		EBSOptions: &DomainEBSOptions{
			EBSEnabled: enabled, IOPS: int64Pointer(3000), Throughput: int64Pointer(125),
			VolumeSize: int64Pointer(20), VolumeType: stringPointer("gp3"),
		},
	}
}

func resourceWithAdvancedSecurity(enabled bool) DomainResource {
	options := resourceWithSAML(true).AdvancedSecurityOptions
	options.Enabled = enabled
	options.AnonymousAuthEnabled = aws.Bool(true)
	options.InternalUserDatabaseEnabled = true
	options.JWTOptions = resourceWithJWT(true).AdvancedSecurityOptions.JWTOptions
	options.MasterUserOptions = &DomainMasterUserOptions{
		MasterUserName: stringPointer("master"), MasterUserPassword: stringPointer("password"),
	}
	return DomainResource{DomainName: "example", AdvancedSecurityOptions: options}
}

func resourceWithJWT(enabled bool) DomainResource {
	return DomainResource{
		DomainName: "example",
		AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
			Enabled: true,
			JWTOptions: &DomainJWTOptions{
				Enabled: aws.Bool(enabled), JWKSURL: stringPointer("jwks"),
				PublicKey: stringPointer("public\nkey"), RolesKey: stringPointer("roles"),
				SubjectKey: stringPointer("subject"),
			},
		},
	}
}

func resourceWithSAML(enabled bool) DomainResource {
	return DomainResource{
		DomainName: "example",
		AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
			Enabled: true,
			SAMLOptions: &DomainSAMLOptions{
				Enabled:           enabled,
				IDP:               &DomainSAMLIDP{EntityID: "entity", MetadataContent: "metadata"},
				MasterBackendRole: stringPointer("backend"),
				MasterUserName:    stringPointer("master"), RolesKey: stringPointer("roles"),
				SessionTimeoutMinutes: 60, SubjectKey: stringPointer("subject"),
			},
		},
	}
}
