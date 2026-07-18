package opensearch

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainNameValidation(t *testing.T) {
	tests := []struct {
		name       string
		domainName string
		wantError  string
	}{
		{name: "valid", domainName: "example-domain"},
		{name: "minimum", domainName: "a12"},
		{name: "starts with digit", domainName: "1example", wantError: "domain-name"},
		{name: "uppercase", domainName: "Example", wantError: "domain-name"},
		{name: "too short", domainName: "ab", wantError: "domain-name"},
		{name: "too long", domainName: "abcdefghijklmnopqrstuvwxyz123", wantError: "domain-name"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := DomainResource{DomainName: test.domainName}
			err := resource.ValidateInputs(context.Background(), nil)
			if test.wantError == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestDomainInputValidation(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*DomainResource)
		wantError string
	}{
		{
			name: "invalid policy",
			configure: func(resource *DomainResource) {
				resource.AccessPolicies = stringPointer("{invalid")
			},
			wantError: "access-policies",
		},
		{
			name: "JWT requires OpenSearch 2.11",
			configure: func(resource *DomainResource) {
				resource.EngineVersion = stringPointer("OpenSearch_2.10")
				resource.AdvancedSecurityOptions = enabledJWT(stringPointer("key"), nil)
			},
			wantError: "OpenSearch 2.11",
		},
		{
			name: "JWT rejects Elasticsearch",
			configure: func(resource *DomainResource) {
				resource.EngineVersion = stringPointer("Elasticsearch_7.10")
				resource.AdvancedSecurityOptions = enabledJWT(stringPointer("key"), nil)
			},
			wantError: "OpenSearch 2.11",
		},
		{
			name: "JWT requires a key source",
			configure: func(resource *DomainResource) {
				resource.EngineVersion = stringPointer("OpenSearch_2.11")
				resource.AdvancedSecurityOptions = enabledJWT(nil, nil)
			},
			wantError: "jwks-url or public-key",
		},
		{
			name: "JWT JWKS URL length",
			configure: func(resource *DomainResource) {
				resource.EngineVersion = stringPointer("OpenSearch_2.11")
				resource.AdvancedSecurityOptions = enabledJWT(
					nil, stringPointer(strings.Repeat("x", 2049)),
				)
			},
			wantError: "jwks-url",
		},
		{
			name: "JWT roles key length",
			configure: func(resource *DomainResource) {
				resource.EngineVersion = stringPointer("OpenSearch_2.11")
				resource.AdvancedSecurityOptions = enabledJWT(stringPointer("key"), nil)
				resource.AdvancedSecurityOptions.JWTOptions.RolesKey = stringPointer("")
			},
			wantError: "roles-key",
		},
		{
			name: "JWT subject key length",
			configure: func(resource *DomainResource) {
				resource.EngineVersion = stringPointer("OpenSearch_2.11")
				resource.AdvancedSecurityOptions = enabledJWT(stringPointer("key"), nil)
				resource.AdvancedSecurityOptions.JWTOptions.SubjectKey = stringPointer(
					strings.Repeat("x", 65),
				)
			},
			wantError: "subject-key",
		},
		{
			name: "SAML requires IDP",
			configure: func(resource *DomainResource) {
				resource.AdvancedSecurityOptions = &DomainAdvancedSecurityOptions{
					Enabled: true,
					SAMLOptions: &DomainSAMLOptions{
						Enabled: true, SessionTimeoutMinutes: 60,
					},
				}
			},
			wantError: "saml-options enabled requires idp",
		},
		{
			name: "SAML session lower bound",
			configure: func(resource *DomainResource) {
				resource.AdvancedSecurityOptions = enabledSAML(0)
			},
			wantError: "session-timeout-minutes",
		},
		{
			name: "SAML session upper bound",
			configure: func(resource *DomainResource) {
				resource.AdvancedSecurityOptions = enabledSAML(1441)
			},
			wantError: "session-timeout-minutes",
		},
		{
			name: "Auto-Tune desired state",
			configure: func(resource *DomainResource) {
				resource.AutoTuneOptions = &DomainAutoTuneOptions{DesiredState: "INVALID"}
			},
			wantError: "auto-tune-options desired-state",
		},
		{
			name: "Auto-Tune rollback",
			configure: func(resource *DomainResource) {
				resource.AutoTuneOptions = &DomainAutoTuneOptions{
					DesiredState: "DISABLED", RollbackOnDisable: stringPointer("INVALID"),
				}
			},
			wantError: "rollback-on-disable",
		},
		{
			name: "Auto-Tune schedule start",
			configure: func(resource *DomainResource) {
				resource.AutoTuneOptions = autoTuneWithSchedule("not-a-time", "HOURS", 2)
			},
			wantError: "start-at",
		},
		{
			name: "Auto-Tune duration unit",
			configure: func(resource *DomainResource) {
				resource.AutoTuneOptions = autoTuneWithSchedule(
					"2026-07-18T12:00:00Z", "MINUTES", 2,
				)
			},
			wantError: "duration unit",
		},
		{
			name: "Auto-Tune duration value",
			configure: func(resource *DomainResource) {
				resource.AutoTuneOptions = autoTuneWithSchedule(
					"2026-07-18T12:00:00Z", "HOURS", 0,
				)
			},
			wantError: "duration value",
		},
		{
			name: "Auto-Tune schedule conflicts with off-peak window",
			configure: func(resource *DomainResource) {
				resource.AutoTuneOptions = autoTuneWithSchedule(
					"2026-07-18T12:00:00Z", "HOURS", 2,
				)
				resource.AutoTuneOptions.UseOffPeakWindow = true
			},
			wantError: "maintenance-schedule conflicts",
		},
		{
			name: "warm count",
			configure: func(resource *DomainResource) {
				resource.ClusterConfig = &DomainClusterConfig{
					InstanceCount: 1, InstanceType: "t3.small.search",
					WarmEnabled: aws.Bool(true), WarmCount: 1,
				}
			},
			wantError: "warm-count",
		},
		{
			name: "warm type",
			configure: func(resource *DomainResource) {
				resource.ClusterConfig = &DomainClusterConfig{
					InstanceCount: 1, InstanceType: "t3.small.search",
					WarmEnabled: aws.Bool(true), WarmCount: 2,
					WarmType: stringPointer("invalid"),
				}
			},
			wantError: "warm-type",
		},
		{
			name: "zone count",
			configure: func(resource *DomainResource) {
				resource.ClusterConfig = &DomainClusterConfig{
					InstanceCount: 1, InstanceType: "t3.small.search",
					ZoneAwarenessEnabled: aws.Bool(true),
					ZoneAwarenessConfig: &DomainZoneAwarenessConfig{
						AvailabilityZoneCount: 4,
					},
				}
			},
			wantError: "availability-zone-count",
		},
		{
			name: "node count",
			configure: func(resource *DomainResource) {
				nodes := []DomainNodeOption{{
					NodeType:   "coordinator",
					NodeConfig: &DomainNodeConfig{Enabled: aws.Bool(true), Count: int64Pointer(0)},
				}}
				resource.ClusterConfig = &DomainClusterConfig{
					InstanceCount: 1, InstanceType: "t3.small.search", NodeOptions: &nodes,
				}
			},
			wantError: "node-options count",
		},
		{
			name: "TLS policy",
			configure: func(resource *DomainResource) {
				resource.DomainEndpointOptions = &DomainEndpointOptions{
					TLSSecurityPolicy: stringPointer("invalid"),
				}
			},
			wantError: "tls-security-policy",
		},
		{
			name: "IP address type",
			configure: func(resource *DomainResource) {
				resource.IPAddressType = stringPointer("invalid")
			},
			wantError: "ip-address-type",
		},
		{
			name: "log type",
			configure: func(resource *DomainResource) {
				logs := []DomainLogPublishingOption{{LogType: "invalid"}}
				resource.LogPublishingOptions = &logs
			},
			wantError: "log-type",
		},
		{
			name: "deployment strategy",
			configure: func(resource *DomainResource) {
				resource.DeploymentStrategyOptions = &DomainDeploymentStrategyOptions{
					DeploymentStrategy: "invalid",
				}
			},
			wantError: "deployment-strategy",
		},
		{
			name: "EBS volume type",
			configure: func(resource *DomainResource) {
				resource.EBSOptions = &DomainEBSOptions{
					EBSEnabled: true, VolumeType: stringPointer("invalid"),
				}
			},
			wantError: "volume-type",
		},
		{
			name: "EBS throughput lower bound",
			configure: func(resource *DomainResource) {
				resource.EBSOptions = &DomainEBSOptions{
					EBSEnabled: true, VolumeType: stringPointer("gp3"),
					Throughput: int64Pointer(124),
				}
			},
			wantError: "throughput",
		},
		{
			name: "EBS IOPS conflict",
			configure: func(resource *DomainResource) {
				resource.EBSOptions = &DomainEBSOptions{
					EBSEnabled: true, VolumeType: stringPointer("gp2"),
					IOPS: int64Pointer(3000),
				}
			},
			wantError: "iops requires",
		},
		{
			name: "snapshot hour",
			configure: func(resource *DomainResource) {
				resource.SnapshotOptions = &DomainSnapshotOptions{
					AutomatedSnapshotStartHour: 24,
				}
			},
			wantError: "automated-snapshot-start-hour",
		},
		{
			name: "off-peak hour",
			configure: func(resource *DomainResource) {
				resource.OffPeakWindowOptions = &DomainOffPeakWindowOptions{
					OffPeakWindow: &DomainOffPeakWindow{
						WindowStartTime: &DomainWindowStartTime{Hours: 24},
					},
				}
			},
			wantError: "hours",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := DomainResource{DomainName: "example"}
			test.configure(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestDomainInputValidationAcceptsBoundaries(t *testing.T) {
	resource := DomainResource{
		DomainName:    "example",
		EngineVersion: stringPointer("OpenSearch_2.11"),
		AdvancedSecurityOptions: &DomainAdvancedSecurityOptions{
			Enabled: true,
			JWTOptions: &DomainJWTOptions{
				Enabled: aws.Bool(true), JWKSURL: stringPointer("x"),
				RolesKey:   stringPointer(strings.Repeat("r", 64)),
				SubjectKey: stringPointer(strings.Repeat("s", 64)),
			},
			SAMLOptions: enabledSAML(1440).SAMLOptions,
		},
		SnapshotOptions: &DomainSnapshotOptions{AutomatedSnapshotStartHour: 23},
	}
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func enabledJWT(publicKey, jwksURL *string) *DomainAdvancedSecurityOptions {
	return &DomainAdvancedSecurityOptions{
		Enabled: true,
		JWTOptions: &DomainJWTOptions{
			Enabled: aws.Bool(true), PublicKey: publicKey, JWKSURL: jwksURL,
		},
	}
}

func enabledSAML(session int64) *DomainAdvancedSecurityOptions {
	return &DomainAdvancedSecurityOptions{
		Enabled: true,
		SAMLOptions: &DomainSAMLOptions{
			Enabled: true, SessionTimeoutMinutes: session,
			IDP: &DomainSAMLIDP{EntityID: "entity", MetadataContent: "metadata"},
		},
	}
}

func autoTuneWithSchedule(start, unit string, value int64) *DomainAutoTuneOptions {
	schedules := []DomainAutoTuneMaintenanceSchedule{{
		CronExpressionForRecurrence: "cron(0 0 ? * 1 *)",
		StartAt:                     start,
		Duration:                    DomainAutoTuneDuration{Unit: unit, Value: value},
	}}
	return &DomainAutoTuneOptions{
		DesiredState: "ENABLED", MaintenanceSchedule: &schedules,
	}
}
