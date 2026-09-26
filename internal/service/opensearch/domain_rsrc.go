package opensearch

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *DomainResource) SchemaVersion() int { return 1 }

func (r *DomainResource) ReplaceFields() []string {
	return []string{"domain-name", "vpc-options"}
}

func (r DomainResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.IPAddressType)).
			Require(constraint.OneOf(r.IPAddressType, "ipv4", "dualstack")).
			Message("ip-address-type must be ipv4 or dualstack"),
		constraint.When(constraint.Present(r.DeploymentStrategyOptions)).
			Require(constraint.OneOf(
				r.DeploymentStrategyOptions.DeploymentStrategy,
				"Default", "CapacityOptimized",
			)).
			Message("deployment-strategy must be Default or CapacityOptimized"),
		constraint.When(constraint.Present(r.DomainEndpointOptions.TLSSecurityPolicy)).
			Require(constraint.OneOf(
				r.DomainEndpointOptions.TLSSecurityPolicy,
				"Policy-Min-TLS-1-0-2019-07",
				"Policy-Min-TLS-1-2-2019-07",
				"Policy-Min-TLS-1-2-PFS-2023-10",
				"Policy-Min-TLS-1-2-RFC9151-FIPS-2024-08",
			)).
			Message("tls-security-policy must be a supported OpenSearch policy"),
		constraint.When(constraint.Present(r.SnapshotOptions)).
			Require(
				constraint.AtLeast(r.SnapshotOptions.AutomatedSnapshotStartHour, 0),
				constraint.AtMost(r.SnapshotOptions.AutomatedSnapshotStartHour, 23),
			).
			Message("automated-snapshot-start-hour must be between 0 and 23"),
		constraint.When(constraint.Present(
			r.AdvancedSecurityOptions.SAMLOptions,
		)).
			Require(
				constraint.AtLeast(
					r.AdvancedSecurityOptions.SAMLOptions.SessionTimeoutMinutes, 1,
				),
				constraint.AtMost(
					r.AdvancedSecurityOptions.SAMLOptions.SessionTimeoutMinutes, 1440,
				),
			).
			Message("session-timeout-minutes must be between 1 and 1440"),
		constraint.When(constraint.IsTrue(
			r.AdvancedSecurityOptions.JWTOptions.Enabled,
		)).
			Require(constraint.Any(
				constraint.Present(r.AdvancedSecurityOptions.JWTOptions.JWKSURL),
				constraint.Present(r.AdvancedSecurityOptions.JWTOptions.PublicKey),
			)).
			Message("enabled JWT requires jwks-url or public-key"),
		constraint.When(constraint.IsTrue(
			r.AdvancedSecurityOptions.SAMLOptions.Enabled,
		)).
			Require(constraint.Present(r.AdvancedSecurityOptions.SAMLOptions.IDP)).
			Message("enabled SAML requires idp"),
		constraint.When(constraint.Present(r.AutoTuneOptions)).
			Require(constraint.OneOf(
				r.AutoTuneOptions.DesiredState, "ENABLED", "DISABLED",
			)).
			Message("auto-tune desired-state must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.AutoTuneOptions.RollbackOnDisable)).
			Require(constraint.OneOf(
				r.AutoTuneOptions.RollbackOnDisable, "NO_ROLLBACK", "DEFAULT_ROLLBACK",
			)).
			Message("rollback-on-disable must be NO_ROLLBACK or DEFAULT_ROLLBACK"),
		constraint.When(constraint.IsTrue(r.AutoTuneOptions.UseOffPeakWindow)).
			Require(constraint.Absent(r.AutoTuneOptions.MaintenanceSchedule)).
			Message("use-off-peak-window conflicts with maintenance-schedule"),
		constraint.ForEach(
			r.AutoTuneOptions.MaintenanceSchedule,
			func(schedule DomainAutoTuneMaintenanceSchedule) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(schedule.Duration.Unit, "HOURS")).
						Message("auto-tune duration unit must be HOURS"),
					constraint.Must(constraint.Above(schedule.Duration.Value, 0)).
						Message("auto-tune duration value must be greater than zero"),
				}
			},
		),
		constraint.When(constraint.Present(r.ClusterConfig.DedicatedMasterCount)).
			Require(constraint.AtLeast(r.ClusterConfig.DedicatedMasterCount, 1)).
			Message("dedicated-master-count must be at least 1"),
		constraint.When(constraint.Present(r.ClusterConfig)).
			Require(constraint.AtLeast(r.ClusterConfig.InstanceCount, 1)).
			Message("instance-count must be at least 1"),
		constraint.When(constraint.Present(r.ClusterConfig.WarmEnabled)).
			Require(
				constraint.AtLeast(r.ClusterConfig.WarmCount, 2),
				constraint.AtMost(r.ClusterConfig.WarmCount, 150),
			).
			Message("warm-count must be between 2 and 150"),
		constraint.When(constraint.Present(r.ClusterConfig.WarmType)).
			Require(constraint.OneOf(
				r.ClusterConfig.WarmType,
				"ultrawarm1.medium.search",
				"ultrawarm1.large.search",
				"ultrawarm1.xlarge.search",
			)).
			Message("warm-type must be a supported UltraWarm instance type"),
		constraint.When(constraint.Present(r.ClusterConfig.ZoneAwarenessConfig)).
			Require(constraint.OneOf(
				r.ClusterConfig.ZoneAwarenessConfig.AvailabilityZoneCount, 2, 3,
			)).
			Message("availability-zone-count must be 2 or 3"),
		constraint.ForEach(
			r.ClusterConfig.NodeOptions,
			func(option DomainNodeOption) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(option.NodeType, "coordinator")).
						Message("node-type must be coordinator"),
					constraint.When(constraint.Present(option.NodeConfig.Count)).
						Require(constraint.AtLeast(option.NodeConfig.Count, 1)).
						Message("node count must be at least 1"),
				}
			},
		),
		constraint.When(constraint.Present(r.EBSOptions.VolumeType)).
			Require(constraint.OneOf(
				r.EBSOptions.VolumeType, "standard", "gp2", "io1", "gp3",
			)).
			Message("volume-type must be standard, gp2, io1, or gp3"),
		constraint.When(constraint.Present(r.EBSOptions.IOPS)).
			Require(constraint.OneOf(r.EBSOptions.VolumeType, "io1", "gp3")).
			Message("iops requires volume-type io1 or gp3"),
		constraint.When(constraint.Present(r.EBSOptions.Throughput)).
			Require(
				constraint.OneOf(r.EBSOptions.VolumeType, "gp3"),
				constraint.AtLeast(r.EBSOptions.Throughput, 125),
			).
			Message("throughput requires gp3 and must be at least 125"),
		constraint.When(constraint.Present(r.EBSOptions.VolumeSize)).
			Require(constraint.AtLeast(r.EBSOptions.VolumeSize, 1)).
			Message("volume-size must be at least 1"),
		constraint.ForEach(
			r.LogPublishingOptions,
			func(option DomainLogPublishingOption) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(
						option.LogType,
						"INDEX_SLOW_LOGS",
						"SEARCH_SLOW_LOGS",
						"ES_APPLICATION_LOGS",
						"AUDIT_LOGS",
					)).
						Message("log-type must be a supported OpenSearch log type"),
				}
			},
		),
	}
}

func (r DomainResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.AdvancedSecurityOptions.Enabled, false),
		defaults.Value(r.AdvancedSecurityOptions.InternalUserDatabaseEnabled, false),
		defaults.Value(r.AdvancedSecurityOptions.SAMLOptions.Enabled, true),
		defaults.Value(r.AdvancedSecurityOptions.SAMLOptions.SessionTimeoutMinutes, int64(60)),
		defaults.Value(r.AutoTuneOptions.UseOffPeakWindow, false),
		defaults.Value(r.ClusterConfig.DedicatedMasterEnabled, false),
		defaults.Value(r.ClusterConfig.InstanceCount, int64(1)),
		defaults.Value(r.ClusterConfig.InstanceType, "m3.medium.search"),
		defaults.Value(r.ClusterConfig.WarmCount, int64(2)),
		defaults.Value(r.ClusterConfig.ZoneAwarenessConfig.AvailabilityZoneCount, int64(2)),
		defaults.Value(r.CognitoOptions.Enabled, false),
		defaults.Value(r.DomainEndpointOptions.CustomEndpointEnabled, false),
		defaults.Value(r.DomainEndpointOptions.EnforceHTTPS, true),
		defaults.Value(r.EncryptAtRest.Enabled, false),
		defaults.Value(r.NodeToNodeEncryption.Enabled, false),
		defaults.Value(r.SoftwareUpdateOptions.AutoSoftwareUpdateEnabled, false),
	}
}

func (r *DomainResource) EquivalentInput(
	field string,
	prior DomainResource,
	current DomainResource,
) bool {
	switch field {
	case "vpc-options":
		return equivalentDomainVPCOptions(prior.VPCOptions, current.VPCOptions)
	case "auto-tune-options":
		return equivalentDomainAutoTuneOptions(
			prior.AutoTuneOptions,
			current.AutoTuneOptions,
		)
	case "log-publishing-options":
		return equivalentDomainLogPublishingOptions(
			prior.LogPublishingOptions,
			current.LogPublishingOptions,
		)
	case "access-policies":
		if prior.AccessPolicies == nil || current.AccessPolicies == nil {
			return prior.AccessPolicies == nil && current.AccessPolicies == nil
		}
	default:
		return false
	}
	equivalent, _, err := equivalentPolicyJSON(*prior.AccessPolicies, *current.AccessPolicies)
	return err == nil && equivalent
}

func (r *DomainResource) ValidateInputs(ctx context.Context, cfg *awsCfg) error {
	return r.validateInputs(ctx, cfg)
}

func (r *DomainResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*DomainResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, defaultDomainOperationOptions())
}

func (r *DomainResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg],
) (*DomainResourceOutput, error) {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior)
}

func (r *DomainResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg],
) (*DomainResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior, defaultDomainOperationOptions())
}

func (r *DomainResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior, defaultDomainOperationOptions())
}
