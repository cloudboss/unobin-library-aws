package rds

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// clusterCreateRetryTimeout bounds the retry around a create call while a
// just-passed IAM role or an S3 backup is not yet usable. RDS rejects the create
// transiently in those windows, which clear within a couple of minutes.
const clusterCreateRetryTimeout = 2 * time.Minute

// clusterModifyRetryTimeout bounds the retry around the update ModifyDBCluster
// call. A modify can be rejected while the cluster is mid-transition or a passed
// role has not propagated; five minutes covers those windows.
const clusterModifyRetryTimeout = 5 * time.Minute

// clusterDeleteRetryTimeout bounds the retry around the delete call. A delete is
// rejected while the cluster is still transitioning, while a global unjoin has
// not settled, or until deletion protection is cleared; two minutes covers it.
const clusterDeleteRetryTimeout = 2 * time.Minute

// clusterSettleTimeout bounds every cluster availability and deletion wait. A
// cluster create, modify, or delete can run for a long time, so the waits are
// given the full create timeout RDS itself allows.
const clusterSettleTimeout = 120 * time.Minute

// clusterWaitInterval paces the cluster availability and deletion polls. A
// cluster changes state over seconds to minutes, so a ten-second interval keeps
// the polling from hammering the API.
const clusterWaitInterval = 10 * time.Second

// clusterIamRoleInvalidMsg is the InvalidParameterValue message RDS returns when
// a passed IAM role is not yet usable. It clears once the role propagates, so a
// create or modify that passes the role is retried while this message holds.
const clusterIamRoleInvalidMsg = "IAM role ARN value is invalid or does not " +
	"include the required permissions"

// finalSnapshotIdentifierRe matches the characters RDS permits in a final
// snapshot identifier: it must start with a letter and hold only letters, digits,
// and single hyphens, with no trailing hyphen and no two consecutive hyphens.
// This is a pattern the constraint layer cannot express, so it is checked in
// Delete and documented on the field.
var finalSnapshotIdentifierRe = regexp.MustCompile(
	`^[a-zA-Z](?:-?[a-zA-Z0-9])*$`)

// ClusterResource manages an Amazon Aurora or Multi-AZ DB cluster, the way CloudFormation
// models AWS::RDS::DBCluster. A cluster is created through one of four calls
// chosen by which input is set: a snapshot restore, an S3 restore, a
// point-in-time restore, or a plain create. Each restore call accepts only a
// subset of the cluster's settings, so the rest are reconciled by a follow-on
// ModifyDBCluster after the cluster becomes available. The IAM roles, global
// cluster membership, and HTTP endpoint are each reconciled by their own calls;
// everything else mutable is reconciled by ModifyDBCluster in Update, gated on a
// change to its own fields. Deletion optionally takes a final snapshot and first
// removes the cluster from any global cluster it belongs to.
//
// Several create-time inputs are fixed by RDS and cannot change on an existing
// cluster, so a change to any of them replaces the cluster; see ReplaceFields.
// The four mode-selecting inputs are mutually exclusive, and several inputs
// conflict by mode; see Constraints. Not-found is the typed DBClusterNotFoundFault
// rather than an HTTP status.
//
// Out of scope and not modeled: auto-minor-version-upgrade, character-set-name,
// publicly-accessible, the activity stream, RDS-custom cluster configuration,
// limitless-database, the option group (clusters do not take one), the
// performance-insights toggle as distinct from its key and retention, and the
// pre-signed-url cross-region restore plumbing. The apply-immediately flag is not
// an input: every ModifyDBCluster is sent with it set.
type ClusterResource struct {
	ClusterIdentifier string `ub:"cluster-identifier"`
	Engine            string `ub:"engine"`

	EngineMode             *string `ub:"engine-mode"`
	EngineVersion          *string `ub:"engine-version"`
	EngineLifecycleSupport *string `ub:"engine-lifecycle-support"`
	ClusterScalabilityType *string `ub:"cluster-scalability-type"`
	DatabaseInsightsMode   *string `ub:"database-insights-mode"`
	DatabaseName           *string `ub:"database-name"`
	DbSystemId             *string `ub:"db-system-id"`

	AllocatedStorage *int64  `ub:"allocated-storage"`
	Iops             *int64  `ub:"iops"`
	StorageEncrypted *bool   `ub:"storage-encrypted"`
	StorageType      *string `ub:"storage-type"`
	KmsKeyId         *string `ub:"kms-key-id"`
	NetworkType      *string `ub:"network-type"`
	Port             *int64  `ub:"port"`

	AvailabilityZones   *[]string `ub:"availability-zones"`
	VpcSecurityGroupIds *[]string `ub:"vpc-security-group-ids"`

	DbClusterInstanceClass       *string `ub:"db-cluster-instance-class"`
	DbClusterParameterGroupName  *string `ub:"db-cluster-parameter-group-name"`
	DbInstanceParameterGroupName *string `ub:"db-instance-parameter-group-name"`
	DbSubnetGroupName            *string `ub:"db-subnet-group-name"`
	CaCertificateIdentifier      *string `ub:"ca-certificate-identifier"`
	CopyTagsToSnapshot           *bool   `ub:"copy-tags-to-snapshot"`
	DeletionProtection           *bool   `ub:"deletion-protection"`

	MasterUsername           *string `ub:"master-username"`
	MasterPassword           *string `ub:"master-password,sensitive"`
	ManageMasterUserPassword *bool   `ub:"manage-master-user-password"`
	MasterUserSecretKmsKeyId *string `ub:"master-user-secret-kms-key-id"`

	EnableIamDatabaseAuthentication *bool   `ub:"enable-iam-database-authentication"`
	EnableGlobalWriteForwarding     *bool   `ub:"enable-global-write-forwarding"`
	EnableLocalWriteForwarding      *bool   `ub:"enable-local-write-forwarding"`
	EnableHttpEndpoint              *bool   `ub:"enable-http-endpoint"`
	Domain                          *string `ub:"domain"`
	DomainIamRoleName               *string `ub:"domain-iam-role-name"`

	MonitoringInterval                 *int64  `ub:"monitoring-interval"`
	MonitoringRoleArn                  *string `ub:"monitoring-role-arn"`
	PerformanceInsightsEnabled         *bool   `ub:"performance-insights-enabled"`
	PerformanceInsightsKmsKeyId        *string `ub:"performance-insights-kms-key-id"`
	PerformanceInsightsRetentionPeriod *int64  `ub:"performance-insights-retention-period"`

	BacktrackWindow              *int64    `ub:"backtrack-window"`
	BackupRetentionPeriod        *int64    `ub:"backup-retention-period"`
	PreferredBackupWindow        *string   `ub:"preferred-backup-window"`
	PreferredMaintenanceWindow   *string   `ub:"preferred-maintenance-window"`
	EnabledCloudwatchLogsExports *[]string `ub:"enabled-cloudwatch-logs-exports"`

	GlobalClusterIdentifier     *string   `ub:"global-cluster-identifier"`
	IamRoles                    *[]string `ub:"iam-roles"`
	ReplicationSourceIdentifier *string   `ub:"replication-source-identifier"`

	Scaling             *ClusterScaling             `ub:"scaling"`
	ServerlessV2Scaling *ClusterServerlessV2Scaling `ub:"serverlessv2-scaling"`

	SnapshotIdentifier   *string                      `ub:"snapshot-identifier"`
	S3Import             *ClusterS3Import             `ub:"s3-import"`
	RestoreToPointInTime *ClusterRestoreToPointInTime `ub:"restore-to-point-in-time"`
	SourceRegion         *string                      `ub:"source-region"`

	SkipFinalSnapshot       *bool   `ub:"skip-final-snapshot"`
	FinalSnapshotIdentifier *string `ub:"final-snapshot-identifier"`
	DeleteAutomatedBackups  *bool   `ub:"delete-automated-backups"`

	Tags *map[string]string `ub:"tags"`
}

// ClusterResourceOutput holds the values RDS computes for a DB cluster once it is
// available. The ARN is the cluster's handle, against which its tags and IAM
// roles are managed. The endpoint and reader endpoint are the writer and
// load-balanced reader addresses; the hosted zone id is the zone those endpoints
// live in. The port is the listening port RDS settled on. The resource id is the
// stable id used to find the cluster's global membership. The members are the
// instance identifiers that have joined the cluster. The master user secret is
// present only when the password is managed by RDS. The actual engine version is
// the version RDS resolved from a partial one. The global cluster identifier is
// the global database the cluster belongs to, empty when it belongs to none.
type ClusterResourceOutput struct {
	Arn                     string                   `ub:"arn"`
	Endpoint                string                   `ub:"endpoint"`
	ReaderEndpoint          string                   `ub:"reader-endpoint"`
	HostedZoneId            string                   `ub:"hosted-zone-id"`
	Port                    int64                    `ub:"port"`
	ClusterResourceId       string                   `ub:"cluster-resource-id"`
	ClusterMembers          []string                 `ub:"cluster-members"`
	MasterUserSecret        *ClusterMasterUserSecret `ub:"master-user-secret"`
	EngineVersionActual     string                   `ub:"engine-version-actual"`
	GlobalClusterIdentifier string                   `ub:"global-cluster-identifier"`
}

func (r *ClusterResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs RDS fixes when a cluster is created. A change to
// any of them cannot be applied to an existing cluster, so it requires a new one.
// These match the CloudFormation create-only properties plus the whole restore
// blocks, whose contents are all create-time.
//
// snapshot-identifier is listed here even though Terraform suppresses the replace
// when the value is removed after a restore. unobin cannot express "replace only
// when changed to a different non-empty value", and CloudFormation itself marks
// the property create-only, so this port takes the stricter rule: clearing or
// changing snapshot-identifier replaces the cluster. The alternative -- omitting
// it from ReplaceFields -- would let a changed snapshot id silently do nothing,
// which is the worse surprise.
func (r *ClusterResource) ReplaceFields() []string {
	return []string{
		"availability-zones",
		"cluster-identifier",
		"cluster-scalability-type",
		"database-name",
		"db-subnet-group-name",
		"db-system-id",
		"engine",
		"engine-mode",
		"kms-key-id",
		"master-username",
		"restore-to-point-in-time",
		"s3-import",
		"snapshot-identifier",
		"source-region",
		"storage-encrypted",
	}
}

// Constraints declares the rules RDS places on a cluster's inputs. At most one
// create mode is selected, and a snapshot restore cannot also join a global
// cluster. The password is managed by RDS or given directly, never both. The
// optional enums and numeric bounds are checked only when present. The inner
// rules of the restore-to-point-in-time and s3-import blocks are checked in
// Create, since the constraint layer does not reach into a nested block.
func (r ClusterResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.SnapshotIdentifier, r.S3Import, r.RestoreToPointInTime),
		constraint.ForbiddenWith(r.SnapshotIdentifier, r.GlobalClusterIdentifier),
		constraint.AtMostOneOf(r.ManageMasterUserPassword, r.MasterPassword),
		constraint.When(constraint.Present(r.EngineMode)).
			Require(constraint.OneOf(r.EngineMode,
				"global", "multimaster", "parallelquery", "provisioned", "serverless")).
			Message("engine-mode must be one of global, multimaster, " +
				"parallelquery, provisioned, or serverless"),
		constraint.When(constraint.Present(r.ClusterScalabilityType)).
			Require(constraint.OneOf(r.ClusterScalabilityType, "standard", "limitless")).
			Message("cluster-scalability-type must be standard or limitless"),
		constraint.When(constraint.Present(r.DatabaseInsightsMode)).
			Require(constraint.OneOf(r.DatabaseInsightsMode, "standard", "advanced")).
			Message("database-insights-mode must be standard or advanced"),
		constraint.When(constraint.Present(r.EngineLifecycleSupport)).
			Require(constraint.OneOf(r.EngineLifecycleSupport,
				"open-source-rds-extended-support",
				"open-source-rds-extended-support-disabled")).
			Message("engine-lifecycle-support must be " +
				"open-source-rds-extended-support or " +
				"open-source-rds-extended-support-disabled"),
		constraint.When(constraint.Present(r.NetworkType)).
			Require(constraint.OneOf(r.NetworkType, "DUAL", "IPV4")).
			Message("network-type must be DUAL or IPV4"),
		constraint.When(constraint.Present(r.AvailabilityZones)).
			Require(constraint.MinItems(r.AvailabilityZones, 1)).
			Message("availability-zones must list at least one zone when given"),
		constraint.When(constraint.Present(r.BackupRetentionPeriod)).
			Require(constraint.AtMost(r.BackupRetentionPeriod, 35)).
			Message("backup-retention-period must be at most 35"),
		constraint.When(constraint.Present(r.BacktrackWindow)).
			Require(constraint.AtLeast(r.BacktrackWindow, 0),
				constraint.AtMost(r.BacktrackWindow, 259200)).
			Message("backtrack-window must be between 0 and 259200"),
		constraint.ForEach(r.EnabledCloudwatchLogsExports,
			func(v string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(v,
						"audit", "error", "general", "iam-db-auth-error", "instance",
						"postgresql", "slowquery", "upgrade")).
						Message("enabled-cloudwatch-logs-exports entries must be " +
							"valid cluster log types"),
				}
			}),
	}
}

func (r *ClusterResource) Create(ctx context.Context, cfg *awsCfg) (*ClusterResourceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The create mode is chosen by which input is set, in priority order: a
	// snapshot restore, then an S3 restore, then a point-in-time restore, then a
	// plain create. Each restore call accepts only a subset of the cluster's
	// settings; the create helper for each mode returns whether a follow-on
	// ModifyDBCluster is needed to apply the rest.
	var deferModify bool
	switch {
	case r.SnapshotIdentifier != nil:
		deferModify, err = r.createFromSnapshot(ctx, client)
	case r.S3Import != nil:
		deferModify, err = r.createFromS3(ctx, client)
	case r.RestoreToPointInTime != nil:
		deferModify, err = r.createToPointInTime(ctx, client)
	default:
		deferModify, err = r.createPlain(ctx, client)
	}
	if err != nil {
		return nil, err
	}
	if err := r.waitCreated(ctx, client); err != nil {
		return nil, err
	}
	// IAM roles are associated one at a time after the cluster is available; a
	// restore or create call does not take them.
	for _, roleArn := range ptr.Value(r.IamRoles) {
		if err := r.addRole(ctx, client, roleArn); err != nil {
			return nil, err
		}
	}
	// A restore that could not accept every setting on its restore call applies
	// the rest with one ModifyDBCluster, then waits for the cluster to settle with
	// no pending modified values.
	if deferModify {
		in := r.deferredModifyInput()
		if err := r.modify(ctx, client, in); err != nil {
			return nil, err
		}
		if err := r.waitUpdated(ctx, client); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

// createPlain creates a cluster with CreateDBCluster, the mode used when no
// restore input is set. It accepts every create-time setting directly, so no
// follow-on modify is needed -- except that when the cluster joins a global
// cluster, the insights and performance-insights settings are deferred, since
// sending them on the create of a global member would reset them.
func (r *ClusterResource) createPlain(
	ctx context.Context, client *rds.Client,
) (bool, error) {
	joinsGlobal := r.GlobalClusterIdentifier != nil && *r.GlobalClusterIdentifier != ""
	in := &rds.CreateDBClusterInput{
		DBClusterIdentifier:                aws.String(r.ClusterIdentifier),
		Engine:                             aws.String(r.Engine),
		EngineMode:                         r.EngineMode,
		EngineVersion:                      r.EngineVersion,
		EngineLifecycleSupport:             r.EngineLifecycleSupport,
		AllocatedStorage:                   ptr.Int32(r.AllocatedStorage),
		AvailabilityZones:                  ptr.Value(r.AvailabilityZones),
		BacktrackWindow:                    r.BacktrackWindow,
		BackupRetentionPeriod:              ptr.Int32(r.BackupRetentionPeriod),
		CACertificateIdentifier:            r.CaCertificateIdentifier,
		CopyTagsToSnapshot:                 r.CopyTagsToSnapshot,
		DBClusterInstanceClass:             r.DbClusterInstanceClass,
		DBClusterParameterGroupName:        r.DbClusterParameterGroupName,
		DBSubnetGroupName:                  r.DbSubnetGroupName,
		DBSystemId:                         r.DbSystemId,
		DatabaseName:                       r.DatabaseName,
		DeletionProtection:                 r.DeletionProtection,
		Domain:                             r.Domain,
		DomainIAMRoleName:                  r.DomainIamRoleName,
		EnableCloudwatchLogsExports:        ptr.Value(r.EnabledCloudwatchLogsExports),
		EnableGlobalWriteForwarding:        r.EnableGlobalWriteForwarding,
		EnableHttpEndpoint:                 r.EnableHttpEndpoint,
		EnableIAMDatabaseAuthentication:    r.EnableIamDatabaseAuthentication,
		EnableLocalWriteForwarding:         r.EnableLocalWriteForwarding,
		GlobalClusterIdentifier:            r.GlobalClusterIdentifier,
		Iops:                               ptr.Int32(r.Iops),
		KmsKeyId:                           r.KmsKeyId,
		ManageMasterUserPassword:           r.ManageMasterUserPassword,
		MasterUserPassword:                 r.MasterPassword,
		MasterUserSecretKmsKeyId:           r.MasterUserSecretKmsKeyId,
		MasterUsername:                     r.MasterUsername,
		MonitoringInterval:                 ptr.Int32(r.MonitoringInterval),
		MonitoringRoleArn:                  r.MonitoringRoleArn,
		NetworkType:                        r.NetworkType,
		PerformanceInsightsKMSKeyId:        r.PerformanceInsightsKmsKeyId,
		PerformanceInsightsRetentionPeriod: ptr.Int32(r.PerformanceInsightsRetentionPeriod),
		Port:                               ptr.Int32(r.Port),
		PreferredBackupWindow:              r.PreferredBackupWindow,
		PreferredMaintenanceWindow:         r.PreferredMaintenanceWindow,
		ReplicationSourceIdentifier:        r.ReplicationSourceIdentifier,
		ScalingConfiguration:               r.Scaling.toSDK(),
		ServerlessV2ScalingConfiguration:   r.ServerlessV2Scaling.toSDK(),
		SourceRegion:                       r.SourceRegion,
		StorageEncrypted:                   r.StorageEncrypted,
		StorageType:                        r.StorageType,
		Tags:                               tagList(ptr.Value(r.Tags)),
		VpcSecurityGroupIds:                ptr.Value(r.VpcSecurityGroupIds),
	}
	if r.ClusterScalabilityType != nil {
		in.ClusterScalabilityType = rdstypes.ClusterScalabilityType(*r.ClusterScalabilityType)
	}
	if r.DatabaseInsightsMode != nil {
		in.DatabaseInsightsMode = rdstypes.DatabaseInsightsMode(*r.DatabaseInsightsMode)
	}
	// A global member defers the insights settings to the post-create modify;
	// otherwise they ride the create call.
	if !joinsGlobal {
		in.EnablePerformanceInsights = r.PerformanceInsightsEnabled
	}
	if joinsGlobal {
		in.DatabaseInsightsMode = ""
		in.PerformanceInsightsKMSKeyId = nil
		in.PerformanceInsightsRetentionPeriod = nil
		in.ReplicationSourceIdentifier = nil
	}
	err := retry.OnError(ctx, isIamRoleInvalid, func(ctx context.Context) error {
		_, err := client.CreateDBCluster(ctx, in)
		return err
	}, retry.WithTimeout(clusterCreateRetryTimeout))
	if err != nil {
		return false, fmt.Errorf("create db cluster: %w", err)
	}
	// The deferred modify has work only when an insights setting was actually
	// configured; a global member with none declared skips it.
	deferInsights := joinsGlobal && (r.DatabaseInsightsMode != nil ||
		r.PerformanceInsightsEnabled != nil ||
		r.PerformanceInsightsKmsKeyId != nil ||
		r.PerformanceInsightsRetentionPeriod != nil)
	return deferInsights, nil
}

// createFromSnapshot restores a cluster from a snapshot with
// RestoreDBClusterFromSnapshot. The call accepts the AZs, networking, encryption,
// monitoring, logging, and serverless v1 scaling settings; it does not accept the
// backup window, retention, maintenance window, password management, or
// serverless v2 scaling, which are deferred to the post-create modify. It always
// returns true so the deferred modify runs.
func (r *ClusterResource) createFromSnapshot(
	ctx context.Context, client *rds.Client,
) (bool, error) {
	in := &rds.RestoreDBClusterFromSnapshotInput{
		DBClusterIdentifier:             aws.String(r.ClusterIdentifier),
		Engine:                          aws.String(r.Engine),
		SnapshotIdentifier:              r.SnapshotIdentifier,
		EngineMode:                      r.EngineMode,
		EngineVersion:                   r.EngineVersion,
		EngineLifecycleSupport:          r.EngineLifecycleSupport,
		AvailabilityZones:               ptr.Value(r.AvailabilityZones),
		BacktrackWindow:                 r.BacktrackWindow,
		CopyTagsToSnapshot:              r.CopyTagsToSnapshot,
		DBClusterInstanceClass:          r.DbClusterInstanceClass,
		DBClusterParameterGroupName:     r.DbClusterParameterGroupName,
		DBSubnetGroupName:               r.DbSubnetGroupName,
		DatabaseName:                    r.DatabaseName,
		DeletionProtection:              r.DeletionProtection,
		Domain:                          r.Domain,
		DomainIAMRoleName:               r.DomainIamRoleName,
		EnableCloudwatchLogsExports:     ptr.Value(r.EnabledCloudwatchLogsExports),
		EnableIAMDatabaseAuthentication: r.EnableIamDatabaseAuthentication,
		Iops:                            ptr.Int32(r.Iops),
		KmsKeyId:                        r.KmsKeyId,
		MonitoringInterval:              ptr.Int32(r.MonitoringInterval),
		MonitoringRoleArn:               r.MonitoringRoleArn,
		NetworkType:                     r.NetworkType,
		Port:                            ptr.Int32(r.Port),
		ScalingConfiguration:            r.Scaling.toSDK(),
		StorageType:                     r.StorageType,
		Tags:                            tagList(ptr.Value(r.Tags)),
		VpcSecurityGroupIds:             ptr.Value(r.VpcSecurityGroupIds),
	}
	err := retry.OnError(ctx, isIamRoleInvalid, func(ctx context.Context) error {
		_, err := client.RestoreDBClusterFromSnapshot(ctx, in)
		return err
	}, retry.WithTimeout(clusterCreateRetryTimeout))
	if err != nil {
		return false, fmt.Errorf("restore db cluster from snapshot: %w", err)
	}
	return true, nil
}

// createFromS3 restores a cluster from a backup in Amazon S3 with
// RestoreDBClusterFromS3. The call accepts the password, backup, and management
// fields directly, so no follow-on modify is needed -- it returns false. RDS
// requires a master username for an S3 restore, which Constraints cannot express
// across the block boundary, so it is checked here.
func (r *ClusterResource) createFromS3(
	ctx context.Context, client *rds.Client,
) (bool, error) {
	if r.MasterUsername == nil {
		return false, errors.New("master-username is required for an s3-import restore")
	}
	in := &rds.RestoreDBClusterFromS3Input{
		DBClusterIdentifier:              aws.String(r.ClusterIdentifier),
		Engine:                           aws.String(r.Engine),
		MasterUsername:                   r.MasterUsername,
		S3BucketName:                     aws.String(r.S3Import.BucketName),
		S3IngestionRoleArn:               aws.String(r.S3Import.IngestionRole),
		SourceEngine:                     aws.String(r.S3Import.SourceEngine),
		SourceEngineVersion:              aws.String(r.S3Import.SourceEngineVersion),
		S3Prefix:                         r.S3Import.BucketPrefix,
		AvailabilityZones:                ptr.Value(r.AvailabilityZones),
		BacktrackWindow:                  r.BacktrackWindow,
		BackupRetentionPeriod:            ptr.Int32(r.BackupRetentionPeriod),
		CopyTagsToSnapshot:               r.CopyTagsToSnapshot,
		DBClusterParameterGroupName:      r.DbClusterParameterGroupName,
		DBSubnetGroupName:                r.DbSubnetGroupName,
		DatabaseName:                     r.DatabaseName,
		DeletionProtection:               r.DeletionProtection,
		Domain:                           r.Domain,
		DomainIAMRoleName:                r.DomainIamRoleName,
		EnableCloudwatchLogsExports:      ptr.Value(r.EnabledCloudwatchLogsExports),
		EnableIAMDatabaseAuthentication:  r.EnableIamDatabaseAuthentication,
		EngineLifecycleSupport:           r.EngineLifecycleSupport,
		EngineVersion:                    r.EngineVersion,
		KmsKeyId:                         r.KmsKeyId,
		ManageMasterUserPassword:         r.ManageMasterUserPassword,
		MasterUserPassword:               r.MasterPassword,
		MasterUserSecretKmsKeyId:         r.MasterUserSecretKmsKeyId,
		NetworkType:                      r.NetworkType,
		Port:                             ptr.Int32(r.Port),
		PreferredBackupWindow:            r.PreferredBackupWindow,
		PreferredMaintenanceWindow:       r.PreferredMaintenanceWindow,
		ServerlessV2ScalingConfiguration: r.ServerlessV2Scaling.toSDK(),
		StorageEncrypted:                 r.StorageEncrypted,
		StorageType:                      r.StorageType,
		Tags:                             tagList(ptr.Value(r.Tags)),
		VpcSecurityGroupIds:              ptr.Value(r.VpcSecurityGroupIds),
	}
	err := retry.OnError(ctx, isS3RestoreRetryable, func(ctx context.Context) error {
		_, err := client.RestoreDBClusterFromS3(ctx, in)
		return err
	}, retry.WithTimeout(clusterCreateRetryTimeout))
	if err != nil {
		return false, fmt.Errorf("restore db cluster from s3: %w", err)
	}
	return false, nil
}

// createToPointInTime restores a cluster to an earlier point in time with
// RestoreDBClusterToPointInTime. The call accepts the networking, encryption,
// monitoring, and logging settings plus the inner source and time fields; it does
// not accept the backup window, retention, maintenance window, password
// management, or either scaling block, which are deferred to the post-create
// modify. It always returns true so the deferred modify runs.
func (r *ClusterResource) createToPointInTime(
	ctx context.Context, client *rds.Client,
) (bool, error) {
	b := r.RestoreToPointInTime
	in := &rds.RestoreDBClusterToPointInTimeInput{
		DBClusterIdentifier:             aws.String(r.ClusterIdentifier),
		SourceDBClusterIdentifier:       b.SourceClusterIdentifier,
		SourceDbClusterResourceId:       b.SourceClusterResourceId,
		RestoreType:                     b.RestoreType,
		UseLatestRestorableTime:         b.UseLatestRestorableTime,
		BacktrackWindow:                 r.BacktrackWindow,
		CopyTagsToSnapshot:              r.CopyTagsToSnapshot,
		DBClusterInstanceClass:          r.DbClusterInstanceClass,
		DBClusterParameterGroupName:     r.DbClusterParameterGroupName,
		DBSubnetGroupName:               r.DbSubnetGroupName,
		DeletionProtection:              r.DeletionProtection,
		Domain:                          r.Domain,
		DomainIAMRoleName:               r.DomainIamRoleName,
		EnableCloudwatchLogsExports:     ptr.Value(r.EnabledCloudwatchLogsExports),
		EnableIAMDatabaseAuthentication: r.EnableIamDatabaseAuthentication,
		EngineLifecycleSupport:          r.EngineLifecycleSupport,
		Iops:                            ptr.Int32(r.Iops),
		KmsKeyId:                        r.KmsKeyId,
		MonitoringInterval:              ptr.Int32(r.MonitoringInterval),
		MonitoringRoleArn:               r.MonitoringRoleArn,
		NetworkType:                     r.NetworkType,
		Port:                            ptr.Int32(r.Port),
		StorageType:                     r.StorageType,
		Tags:                            tagList(ptr.Value(r.Tags)),
		VpcSecurityGroupIds:             ptr.Value(r.VpcSecurityGroupIds),
	}
	if b.RestoreToTime != nil {
		t, err := time.Parse(time.RFC3339, *b.RestoreToTime)
		if err != nil {
			return false, fmt.Errorf("parse restore-to-time %q: %w", *b.RestoreToTime, err)
		}
		in.RestoreToTime = aws.Time(t)
	}
	_, err := client.RestoreDBClusterToPointInTime(ctx, in)
	if err != nil {
		return false, fmt.Errorf("restore db cluster to point in time: %w", err)
	}
	return true, nil
}

// deferredModifyInput builds the post-create ModifyDBCluster for the settings
// the active restore mode could not accept on its restore call. The snapshot and
// point-in-time modes defer the backup window, retention, maintenance window,
// password management, and serverless v2 scaling; point-in-time additionally
// defers serverless v1 scaling, which the snapshot restore takes directly. A
// global-member plain create defers the insights and performance-insights
// settings instead. The call always sets ApplyImmediately so the change takes
// effect at once.
func (r *ClusterResource) deferredModifyInput() *rds.ModifyDBClusterInput {
	in := &rds.ModifyDBClusterInput{
		DBClusterIdentifier: aws.String(r.ClusterIdentifier),
		ApplyImmediately:    aws.Bool(true),
	}
	// The plain create defers only the insights settings, and only when the
	// cluster joins a global cluster.
	if r.SnapshotIdentifier == nil && r.S3Import == nil && r.RestoreToPointInTime == nil {
		if r.DatabaseInsightsMode != nil {
			in.DatabaseInsightsMode = rdstypes.DatabaseInsightsMode(*r.DatabaseInsightsMode)
		}
		in.EnablePerformanceInsights = r.PerformanceInsightsEnabled
		in.PerformanceInsightsKMSKeyId = r.PerformanceInsightsKmsKeyId
		in.PerformanceInsightsRetentionPeriod = ptr.Int32(r.PerformanceInsightsRetentionPeriod)
		return in
	}
	in.BackupRetentionPeriod = ptr.Int32(r.BackupRetentionPeriod)
	in.ManageMasterUserPassword = r.ManageMasterUserPassword
	in.MasterUserPassword = r.MasterPassword
	in.MasterUserSecretKmsKeyId = r.MasterUserSecretKmsKeyId
	in.PreferredBackupWindow = r.PreferredBackupWindow
	in.PreferredMaintenanceWindow = r.PreferredMaintenanceWindow
	in.ServerlessV2ScalingConfiguration = r.ServerlessV2Scaling.toSDK()
	// A point-in-time restore also defers the serverless v1 scaling block, which
	// the snapshot restore accepts on its own call.
	if r.RestoreToPointInTime != nil {
		in.ScalingConfiguration = r.Scaling.toSDK()
	}
	return in
}

// validate checks the inner rules of the nested blocks, which the constraint
// layer cannot reach across the block boundary. The point-in-time block names its
// source by exactly one of the identifier or resource id, fixes the moment by
// exactly one of a restore time or the latest-restorable-time flag, and takes a
// restore type from a fixed set. The s3-import block requires the bucket,
// ingestion role, source engine, and source engine version. Each scaling block
// checks its own capacity and timeout bounds.
func (r *ClusterResource) validate() error {
	if b := r.RestoreToPointInTime; b != nil {
		hasId := b.SourceClusterIdentifier != nil
		hasResourceId := b.SourceClusterResourceId != nil
		if hasId == hasResourceId {
			return errors.New("restore-to-point-in-time requires exactly one of " +
				"source-cluster-identifier or source-cluster-resource-id")
		}
		hasTime := b.RestoreToTime != nil
		latest := b.UseLatestRestorableTime != nil && *b.UseLatestRestorableTime
		if hasTime == latest {
			return errors.New("restore-to-point-in-time requires exactly one of " +
				"restore-to-time or use-latest-restorable-time")
		}
		if b.RestoreType != nil &&
			*b.RestoreType != "copy-on-write" && *b.RestoreType != "full-copy" {
			return errors.New("restore-to-point-in-time restore-type must be " +
				"copy-on-write or full-copy")
		}
	}
	if b := r.S3Import; b != nil {
		if b.BucketName == "" || b.IngestionRole == "" ||
			b.SourceEngine == "" || b.SourceEngineVersion == "" {
			return errors.New("s3-import requires bucket-name, ingestion-role, " +
				"source-engine, and source-engine-version")
		}
	}
	if err := r.Scaling.validate(); err != nil {
		return err
	}
	return r.ServerlessV2Scaling.validate()
}

func (r *ClusterResource) Read(
	ctx context.Context, cfg *awsCfg, prior *ClusterResourceOutput) (*ClusterResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the cluster by identifier, maps it to outputs, and recovers its
// global cluster membership. A missing cluster maps to runtime.ErrNotFound so a
// plan recreates it.
func (r *ClusterResource) read(
	ctx context.Context,
	client *rds.Client,
) (*ClusterResourceOutput, error) {
	cluster, err := findCluster(ctx, client, r.ClusterIdentifier)
	if err != nil {
		return nil, err
	}
	out := &ClusterResourceOutput{
		Arn:                 aws.ToString(cluster.DBClusterArn),
		Endpoint:            aws.ToString(cluster.Endpoint),
		ReaderEndpoint:      aws.ToString(cluster.ReaderEndpoint),
		HostedZoneId:        aws.ToString(cluster.HostedZoneId),
		Port:                int64(aws.ToInt32(cluster.Port)),
		ClusterResourceId:   aws.ToString(cluster.DbClusterResourceId),
		ClusterMembers:      clusterMembers(cluster),
		MasterUserSecret:    clusterMasterUserSecret(cluster.MasterUserSecret),
		EngineVersionActual: aws.ToString(cluster.EngineVersion),
	}
	id, err := r.readGlobalMembership(ctx, client, out.Arn, cluster)
	if err != nil {
		return nil, err
	}
	out.GlobalClusterIdentifier = id
	return out, nil
}

// readGlobalMembership recovers the identifier of the global cluster the cluster
// belongs to. The describe record reports it directly when present; otherwise,
// for a cluster whose engine mode is global or provisioned, it is recovered by
// scanning the global clusters for one that lists this cluster's ARN. That scan
// is best-effort: a not-found, or an access-denied on a partition without Global
// Databases, leaves the membership empty rather than failing the read.
func (r *ClusterResource) readGlobalMembership(
	ctx context.Context, client *rds.Client, arn string, cluster *rdstypes.DBCluster,
) (string, error) {
	if id := aws.ToString(cluster.GlobalClusterIdentifier); id != "" {
		return id, nil
	}
	mode := aws.ToString(cluster.EngineMode)
	if mode != "global" && mode != "provisioned" {
		return "", nil
	}
	id, err := findGlobalClusterByClusterArn(ctx, client, arn)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) ||
			isGlobalClusterNotFound(err) || isGlobalDatabasesUnsupported(err) {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

func (r *ClusterResource) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[ClusterResource, *ClusterResourceOutput],
) (*ClusterResourceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	// The HTTP endpoint is toggled by its own call only for a provisioned cluster;
	// for any other engine mode it rides the ModifyDBCluster below. The toggle runs
	// only when the field changed.
	if runtime.Changed(prior.Inputs.EnableHttpEndpoint, r.EnableHttpEndpoint) &&
		r.engineModeIs("provisioned") {
		if err := r.modifyHttpEndpoint(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	// Clearing the replication source promotes a read replica to a standalone
	// cluster. Only clearing it is supported; setting it on an existing cluster is
	// rejected by RDS.
	if runtime.Changed(prior.Inputs.ReplicationSourceIdentifier, r.ReplicationSourceIdentifier) &&
		aws.ToString(r.ReplicationSourceIdentifier) == "" {
		if err := r.promoteReadReplica(ctx, client); err != nil {
			return nil, err
		}
		if err := r.waitAvailable(ctx, client); err != nil {
			return nil, err
		}
	}
	// The single ModifyDBCluster sends every changed mutable field. It runs only
	// when at least one of those fields changed, then waits for the cluster to
	// settle with no pending modified values.
	in, changed := r.modifyInput(prior)
	if changed {
		if err := r.modifyWithRetry(ctx, client, in); err != nil {
			return nil, err
		}
		if err := r.waitUpdated(ctx, client); err != nil {
			return nil, err
		}
	}
	// Global cluster membership is only removable in place; adding or switching is
	// rejected. Removal unjoins the cluster and waits for it to settle.
	if runtime.Changed(prior.Inputs.GlobalClusterIdentifier, r.GlobalClusterIdentifier) {
		if err := r.updateGlobalMembership(ctx, client, prior, arn); err != nil {
			return nil, err
		}
	}
	// IAM roles are reconciled as a set: the added ARNs are associated and the
	// removed ARNs are disassociated.
	if r.IamRoles != nil && runtime.Changed(prior.Inputs.IamRoles, r.IamRoles) {
		if err := r.reconcileRoles(ctx, client, prior.Inputs.IamRoles); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := syncTags(ctx, client, arn, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

// modifyInput builds the ModifyDBCluster for Update, setting only the fields that
// changed and reporting whether any did. The fields reconciled by their own calls
// -- the IAM roles, global membership, replication source -- and the delete-time
// parameters are never sent here. The cloudwatch logs exports are sent as an
// enable/disable diff rather than a whole set. The HTTP endpoint rides this call
// only when the engine mode is not provisioned.
func (r *ClusterResource) modifyInput(
	prior runtime.Prior[ClusterResource, *ClusterResourceOutput],
) (*rds.ModifyDBClusterInput, bool) {
	p := prior.Inputs
	in := &rds.ModifyDBClusterInput{
		DBClusterIdentifier: aws.String(r.ClusterIdentifier),
		ApplyImmediately:    aws.Bool(true),
	}
	changed := false
	set := func(cond bool, apply func()) {
		if cond {
			apply()
			changed = true
		}
	}
	set(runtime.Changed(p.AllocatedStorage, r.AllocatedStorage),
		func() { in.AllocatedStorage = ptr.Int32(r.AllocatedStorage) })
	set(runtime.Changed(p.BacktrackWindow, r.BacktrackWindow),
		func() { in.BacktrackWindow = r.BacktrackWindow })
	set(runtime.Changed(p.BackupRetentionPeriod, r.BackupRetentionPeriod),
		func() { in.BackupRetentionPeriod = ptr.Int32(r.BackupRetentionPeriod) })
	set(runtime.Changed(p.CaCertificateIdentifier, r.CaCertificateIdentifier),
		func() { in.CACertificateIdentifier = r.CaCertificateIdentifier })
	set(runtime.Changed(p.CopyTagsToSnapshot, r.CopyTagsToSnapshot),
		func() { in.CopyTagsToSnapshot = r.CopyTagsToSnapshot })
	// A database-insights-mode change travels with the performance-insights
	// settings, since RDS rejects the mode alone.
	insightsModeChanged := runtime.Changed(p.DatabaseInsightsMode, r.DatabaseInsightsMode)
	set(insightsModeChanged, func() {
		in.DatabaseInsightsMode = rdstypes.DatabaseInsightsMode(aws.ToString(r.DatabaseInsightsMode))
	})
	set(runtime.Changed(p.DbClusterInstanceClass, r.DbClusterInstanceClass),
		func() { in.DBClusterInstanceClass = r.DbClusterInstanceClass })
	set(runtime.Changed(p.DbClusterParameterGroupName, r.DbClusterParameterGroupName),
		func() { in.DBClusterParameterGroupName = r.DbClusterParameterGroupName })
	set(runtime.Changed(p.DbInstanceParameterGroupName, r.DbInstanceParameterGroupName),
		func() { in.DBInstanceParameterGroupName = r.DbInstanceParameterGroupName })
	set(runtime.Changed(p.DeletionProtection, r.DeletionProtection),
		func() { in.DeletionProtection = r.DeletionProtection })
	// The directory fields travel together: a change to either sends both,
	// since RDS validates the domain join as a pair.
	set(runtime.Changed(p.Domain, r.Domain) ||
		runtime.Changed(p.DomainIamRoleName, r.DomainIamRoleName), func() {
		in.Domain = r.Domain
		in.DomainIAMRoleName = r.DomainIamRoleName
	})
	set(runtime.Changed(p.EnableGlobalWriteForwarding, r.EnableGlobalWriteForwarding),
		func() { in.EnableGlobalWriteForwarding = r.EnableGlobalWriteForwarding })
	set(runtime.Changed(p.EnableLocalWriteForwarding, r.EnableLocalWriteForwarding),
		func() { in.EnableLocalWriteForwarding = r.EnableLocalWriteForwarding })
	set(runtime.Changed(p.EnableIamDatabaseAuthentication, r.EnableIamDatabaseAuthentication),
		func() { in.EnableIAMDatabaseAuthentication = r.EnableIamDatabaseAuthentication })
	set(runtime.Changed(p.EngineVersion, r.EngineVersion),
		func() { in.EngineVersion = r.EngineVersion })
	set(runtime.Changed(p.Iops, r.Iops), func() { in.Iops = ptr.Int32(r.Iops) })
	// Provisioned-IOPS storage takes the storage size and iops as a pair: when
	// either changes on an io1 or io2 cluster, RDS requires both on the call.
	if (in.AllocatedStorage != nil) != (in.Iops != nil) && r.StorageType != nil &&
		(*r.StorageType == "io1" || *r.StorageType == "io2") {
		in.AllocatedStorage = ptr.Int32(r.AllocatedStorage)
		in.Iops = ptr.Int32(r.Iops)
	}
	set(runtime.Changed(p.ManageMasterUserPassword, r.ManageMasterUserPassword),
		func() { in.ManageMasterUserPassword = r.ManageMasterUserPassword })
	set(runtime.Changed(p.MasterPassword, r.MasterPassword),
		func() { in.MasterUserPassword = r.MasterPassword })
	set(runtime.Changed(p.MasterUserSecretKmsKeyId, r.MasterUserSecretKmsKeyId),
		func() { in.MasterUserSecretKmsKeyId = r.MasterUserSecretKmsKeyId })
	set(runtime.Changed(p.MonitoringInterval, r.MonitoringInterval),
		func() { in.MonitoringInterval = ptr.Int32(r.MonitoringInterval) })
	set(runtime.Changed(p.MonitoringRoleArn, r.MonitoringRoleArn),
		func() { in.MonitoringRoleArn = r.MonitoringRoleArn })
	set(runtime.Changed(p.NetworkType, r.NetworkType),
		func() { in.NetworkType = r.NetworkType })
	set(runtime.Changed(p.PerformanceInsightsEnabled, r.PerformanceInsightsEnabled) ||
		insightsModeChanged,
		func() { in.EnablePerformanceInsights = r.PerformanceInsightsEnabled })
	set(runtime.Changed(p.PerformanceInsightsKmsKeyId, r.PerformanceInsightsKmsKeyId) ||
		insightsModeChanged,
		func() { in.PerformanceInsightsKMSKeyId = r.PerformanceInsightsKmsKeyId })
	piRetentionChanged := runtime.Changed(
		p.PerformanceInsightsRetentionPeriod, r.PerformanceInsightsRetentionPeriod)
	set(piRetentionChanged || insightsModeChanged, func() {
		in.PerformanceInsightsRetentionPeriod = ptr.Int32(r.PerformanceInsightsRetentionPeriod)
	})
	set(runtime.Changed(p.Port, r.Port), func() { in.Port = ptr.Int32(r.Port) })
	set(runtime.Changed(p.PreferredBackupWindow, r.PreferredBackupWindow),
		func() { in.PreferredBackupWindow = r.PreferredBackupWindow })
	set(runtime.Changed(p.PreferredMaintenanceWindow, r.PreferredMaintenanceWindow),
		func() { in.PreferredMaintenanceWindow = r.PreferredMaintenanceWindow })
	set(runtime.Changed(p.Scaling, r.Scaling),
		func() { in.ScalingConfiguration = r.Scaling.toSDK() })
	set(serverlessV2Changed(p.ServerlessV2Scaling, r.ServerlessV2Scaling),
		func() { in.ServerlessV2ScalingConfiguration = r.ServerlessV2Scaling.toSDK() })
	set(runtime.Changed(p.StorageType, r.StorageType),
		func() { in.StorageType = r.StorageType })
	set(ptr.Value(r.VpcSecurityGroupIds) != nil &&
		runtime.Changed(ptr.Value(p.VpcSecurityGroupIds), ptr.Value(r.VpcSecurityGroupIds)),
		func() { in.VpcSecurityGroupIds = ptr.Value(r.VpcSecurityGroupIds) })
	// The HTTP endpoint rides this call only for a non-provisioned engine mode;
	// the provisioned case is handled by its own call before this one.
	set(runtime.Changed(p.EnableHttpEndpoint, r.EnableHttpEndpoint) &&
		!r.engineModeIs("provisioned"),
		func() { in.EnableHttpEndpoint = r.EnableHttpEndpoint })
	// The cloudwatch logs exports are reconciled as an enable/disable diff of the
	// log-type set rather than a whole-set replacement.
	if enable, disable := logExportsDiff(ptr.Value(p.EnabledCloudwatchLogsExports),
		ptr.Value(r.EnabledCloudwatchLogsExports)); len(enable) > 0 || len(disable) > 0 {
		in.CloudwatchLogsExportConfiguration = &rdstypes.CloudwatchLogsExportConfiguration{
			EnableLogTypes:  enable,
			DisableLogTypes: disable,
		}
		changed = true
	}
	return in, changed
}

func (r *ClusterResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *ClusterResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	skip := r.SkipFinalSnapshot != nil && *r.SkipFinalSnapshot
	if !skip {
		if r.FinalSnapshotIdentifier == nil {
			return errors.New(
				"final-snapshot-identifier is required when skip-final-snapshot is false")
		}
		if !finalSnapshotIdentifierRe.MatchString(*r.FinalSnapshotIdentifier) {
			return errors.New("final-snapshot-identifier must start with a letter and " +
				"contain only letters, digits, and single hyphens")
		}
	}
	// A cluster that belongs to a global cluster cannot be deleted until it is
	// removed from it, so the membership is recovered and removed first.
	if prior.GlobalClusterIdentifier != "" {
		if err := r.removeFromGlobalCluster(ctx, client,
			prior.GlobalClusterIdentifier, prior.Arn); err != nil {
			return err
		}
		if err := r.waitAvailable(ctx, client); err != nil {
			if err == runtime.ErrNotFound {
				return nil
			}
			return err
		}
	}
	in := &rds.DeleteDBClusterInput{
		DBClusterIdentifier:    aws.String(r.ClusterIdentifier),
		DeleteAutomatedBackups: r.DeleteAutomatedBackups,
		SkipFinalSnapshot:      aws.Bool(skip),
	}
	if !skip {
		in.FinalDBSnapshotIdentifier = r.FinalSnapshotIdentifier
	}
	if err := r.deleteWithRetry(ctx, client, in); err != nil {
		return err
	}
	return r.waitDeleted(ctx, client)
}

// deleteWithRetry issues DeleteDBCluster, retrying while the cluster is still
// transitioning or a global unjoin has not settled, and self-healing a delete
// blocked by deletion protection. Deletion protection is cleared at most once,
// only when the desired state does not keep it on, after which the retry issues
// the delete again. A not-found fault means the cluster is already gone, which is
// the outcome the delete wants.
func (r *ClusterResource) deleteWithRetry(
	ctx context.Context, client *rds.Client, in *rds.DeleteDBClusterInput,
) error {
	healed := false
	err := retry.OnError(ctx, isDeleteRetryable, func(ctx context.Context) error {
		_, err := client.DeleteDBCluster(ctx, in)
		if err != nil && isDeletionProtectionBlock(err) && !healed &&
			(r.DeletionProtection == nil || !*r.DeletionProtection) {
			healed = true
			if healErr := r.clearDeletionProtection(ctx, client); healErr != nil {
				return healErr
			}
		}
		return err
	}, retry.WithTimeout(clusterDeleteRetryTimeout))
	if err != nil {
		if isClusterNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete db cluster: %w", err)
	}
	return nil
}

// clearDeletionProtection turns off deletion protection so a blocked delete can
// proceed, then waits for the cluster to settle. The modify is retried on the
// same transient conditions as the update modify.
func (r *ClusterResource) clearDeletionProtection(ctx context.Context, client *rds.Client) error {
	in := &rds.ModifyDBClusterInput{
		DBClusterIdentifier: aws.String(r.ClusterIdentifier),
		ApplyImmediately:    aws.Bool(true),
		DeletionProtection:  aws.Bool(false),
	}
	if err := r.modifyWithRetry(ctx, client, in); err != nil {
		return err
	}
	return r.waitUpdated(ctx, client)
}

// modify issues a ModifyDBCluster without a retry, used for the create-time
// deferred modify where the cluster is freshly available.
func (r *ClusterResource) modify(
	ctx context.Context, client *rds.Client, in *rds.ModifyDBClusterInput,
) error {
	if _, err := client.ModifyDBCluster(ctx, in); err != nil {
		return fmt.Errorf("modify db cluster: %w", err)
	}
	return nil
}

// modifyWithRetry issues a ModifyDBCluster, retrying while a passed role has not
// propagated or the cluster is mid-transition. A combination error reporting that
// the instance parameter group applies only to a major version upgrade is healed
// by removing that field and retrying, since a minor upgrade rejects it.
func (r *ClusterResource) modifyWithRetry(
	ctx context.Context, client *rds.Client, in *rds.ModifyDBClusterInput,
) error {
	err := retry.OnError(ctx, isModifyRetryable, func(ctx context.Context) error {
		_, err := client.ModifyDBCluster(ctx, in)
		if err != nil && isInstanceParamGroupMajorOnly(err) {
			in.DBInstanceParameterGroupName = nil
		}
		return err
	}, retry.WithTimeout(clusterModifyRetryTimeout))
	if err != nil {
		return fmt.Errorf("modify db cluster: %w", err)
	}
	return nil
}

// modifyHttpEndpoint enables or disables the RDS Data API HTTP endpoint for a
// provisioned cluster, keyed by the cluster ARN.
func (r *ClusterResource) modifyHttpEndpoint(
	ctx context.Context,
	client *rds.Client,
	arn string,
) error {
	if r.EnableHttpEndpoint != nil && *r.EnableHttpEndpoint {
		if _, err := client.EnableHttpEndpoint(ctx,
			&rds.EnableHttpEndpointInput{ResourceArn: aws.String(arn)}); err != nil {
			return fmt.Errorf("enable http endpoint: %w", err)
		}
		return nil
	}
	if _, err := client.DisableHttpEndpoint(ctx,
		&rds.DisableHttpEndpointInput{ResourceArn: aws.String(arn)}); err != nil {
		return fmt.Errorf("disable http endpoint: %w", err)
	}
	return nil
}

// promoteReadReplica promotes a read replica cluster to a standalone cluster by
// clearing its replication source.
func (r *ClusterResource) promoteReadReplica(ctx context.Context, client *rds.Client) error {
	_, err := client.PromoteReadReplicaDBCluster(ctx,
		&rds.PromoteReadReplicaDBClusterInput{
			DBClusterIdentifier: aws.String(r.ClusterIdentifier),
		})
	if err != nil {
		return fmt.Errorf("promote read replica db cluster: %w", err)
	}
	return nil
}

// updateGlobalMembership reconciles a change to the cluster's global cluster
// membership. Only removal is supported: a cluster already in a global cluster
// can be unjoined, but adding or switching membership in place is rejected. On
// removal the cluster is unjoined and waited on; any other change is an error.
func (r *ClusterResource) updateGlobalMembership(
	ctx context.Context, client *rds.Client,
	prior runtime.Prior[ClusterResource, *ClusterResourceOutput], arn string,
) error {
	desired := aws.ToString(r.GlobalClusterIdentifier)
	priorID := prior.Outputs.GlobalClusterIdentifier
	if desired != "" {
		return fmt.Errorf("global-cluster-identifier can only be removed in place, "+
			"not changed from %q to %q", priorID, desired)
	}
	if priorID == "" {
		return nil
	}
	if err := r.removeFromGlobalCluster(ctx, client, priorID, arn); err != nil {
		return err
	}
	return r.waitAvailable(ctx, client)
}

// removeFromGlobalCluster unjoins the cluster from a global cluster, keyed by the
// cluster ARN. A global-cluster-not-found fault, or a message that the cluster is
// not found in the global cluster, means the membership is already gone, which is
// the outcome the removal wants.
func (r *ClusterResource) removeFromGlobalCluster(
	ctx context.Context, client *rds.Client, globalID, arn string,
) error {
	_, err := client.RemoveFromGlobalCluster(ctx, &rds.RemoveFromGlobalClusterInput{
		GlobalClusterIdentifier: aws.String(globalID),
		DbClusterIdentifier:     aws.String(arn),
	})
	if err != nil {
		if isGlobalClusterNotFound(err) || isNotInGlobalCluster(err) {
			return nil
		}
		return fmt.Errorf("remove from global cluster: %w", err)
	}
	return nil
}

// reconcileRoles associates the IAM role ARNs added since the prior apply and
// disassociates the ones removed.
func (r *ClusterResource) reconcileRoles(
	ctx context.Context, client *rds.Client, priorRoles *[]string,
) error {
	priorList := ptr.Value(priorRoles)
	desiredList := ptr.Value(r.IamRoles)
	prior := map[string]bool{}
	for _, a := range priorList {
		prior[a] = true
	}
	desired := map[string]bool{}
	for _, a := range desiredList {
		desired[a] = true
	}
	for _, a := range desiredList {
		if !prior[a] {
			if err := r.addRole(ctx, client, a); err != nil {
				return err
			}
		}
	}
	for _, a := range priorList {
		if !desired[a] {
			if err := r.removeRole(ctx, client, a); err != nil {
				return err
			}
		}
	}
	return nil
}

// addRole associates one IAM role ARN with the cluster.
func (r *ClusterResource) addRole(ctx context.Context, client *rds.Client, roleArn string) error {
	_, err := client.AddRoleToDBCluster(ctx, &rds.AddRoleToDBClusterInput{
		DBClusterIdentifier: aws.String(r.ClusterIdentifier),
		RoleArn:             aws.String(roleArn),
	})
	if err != nil {
		return fmt.Errorf("add role to db cluster: %w", err)
	}
	return nil
}

// removeRole disassociates one IAM role ARN from the cluster.
func (r *ClusterResource) removeRole(
	ctx context.Context,
	client *rds.Client,
	roleArn string,
) error {
	_, err := client.RemoveRoleFromDBCluster(ctx, &rds.RemoveRoleFromDBClusterInput{
		DBClusterIdentifier: aws.String(r.ClusterIdentifier),
		RoleArn:             aws.String(roleArn),
	})
	if err != nil {
		return fmt.Errorf("remove role from db cluster: %w", err)
	}
	return nil
}

// engineModeIs reports whether the cluster's engine mode equals mode. RDS treats
// an unset engine mode as provisioned, so an absent value matches "provisioned".
func (r *ClusterResource) engineModeIs(mode string) bool {
	if r.EngineMode == nil {
		return mode == "provisioned"
	}
	return *r.EngineMode == mode
}

// clusterCreatePending are the transient statuses a cluster passes through on its
// way to available right after a create or restore.
var clusterCreatePending = map[string]bool{
	"backing-up":                   true,
	"creating":                     true,
	"migrating":                    true,
	"modifying":                    true,
	"preparing-data-migration":     true,
	"rebooting":                    true,
	"resetting-master-credentials": true,
}

// clusterUpdatePending are the transient statuses a cluster passes through while
// a modify is taking effect.
var clusterUpdatePending = map[string]bool{
	"backing-up":                      true,
	"configuring-iam-database-auth":   true,
	"configuring-enhanced-monitoring": true,
	"modifying":                       true,
	"renaming":                        true,
	"resetting-master-credentials":    true,
	"scaling-compute":                 true,
	"scaling-storage":                 true,
	"upgrading":                       true,
}

// clusterAvailablePending are the transient statuses a cluster passes through
// before it settles back to available after a promotion or a global unjoin, which
// puts the cluster into the promoting state.
var clusterAvailablePending = map[string]bool{
	"backing-up":                      true,
	"configuring-enhanced-monitoring": true,
	"configuring-iam-database-auth":   true,
	"creating":                        true,
	"migrating":                       true,
	"modifying":                       true,
	"preparing-data-migration":        true,
	"promoting":                       true,
	"rebooting":                       true,
	"renaming":                        true,
	"resetting-master-credentials":    true,
	"scaling-compute":                 true,
	"scaling-storage":                 true,
	"upgrading":                       true,
}

// waitCreated waits for the cluster to reach available after a create or restore.
// A transient not-found while the cluster is still settling keeps the wait going;
// the returned describe holds the computed ARN, endpoints, and resolved engine
// version that the create response lacks.
func (r *ClusterResource) waitCreated(ctx context.Context, client *rds.Client) error {
	return r.waitAvailableStatus(ctx, client, "created", clusterCreatePending, false, true)
}

// waitUpdated waits for the cluster to reach available with no pending modified
// values after a modify.
func (r *ClusterResource) waitUpdated(ctx context.Context, client *rds.Client) error {
	return r.waitAvailableStatus(ctx, client, "updated", clusterUpdatePending, true, false)
}

// waitAvailable waits for the cluster to reach available with no pending modified
// values after a promotion or a global unjoin.
func (r *ClusterResource) waitAvailable(ctx context.Context, client *rds.Client) error {
	return r.waitAvailableStatus(ctx, client, "available", clusterAvailablePending, true, false)
}

// waitAvailableStatus polls until the cluster reports available. While its status
// is in pending, the wait continues; a status outside pending and not available is
// reported as an error. When noPending is set, an available cluster that still has
// pending modified values is treated as not yet settled. When tolerateMissing is
// set, a not-found cluster is treated as still settling rather than as drift,
// which suits the window right after a create.
func (r *ClusterResource) waitAvailableStatus(
	ctx context.Context, client *rds.Client, verb string,
	pending map[string]bool, noPending, tolerateMissing bool,
) error {
	what := fmt.Sprintf("db cluster %s to be %s", r.ClusterIdentifier, verb)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		cluster, err := findCluster(ctx, client, r.ClusterIdentifier)
		if err != nil {
			if err == runtime.ErrNotFound && tolerateMissing {
				return false, nil
			}
			return false, err
		}
		status := aws.ToString(cluster.Status)
		if status == "available" {
			if noPending && hasPendingModifiedValues(cluster) {
				return false, nil
			}
			return true, nil
		}
		if pending[status] {
			return false, nil
		}
		return false, fmt.Errorf("db cluster %s entered unexpected status %q",
			r.ClusterIdentifier, status)
	}, wait.WithTimeout(clusterSettleTimeout), wait.WithInterval(clusterWaitInterval))
}

// waitDeleted waits for the cluster to disappear after a delete. The cluster
// keeps describing for a while after the delete call accepts, so the delete is
// not complete until the describe returns not-found.
func (r *ClusterResource) waitDeleted(ctx context.Context, client *rds.Client) error {
	what := fmt.Sprintf("db cluster %s to be deleted", r.ClusterIdentifier)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := findCluster(ctx, client, r.ClusterIdentifier)
		if err == runtime.ErrNotFound {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	}, wait.WithTimeout(clusterSettleTimeout), wait.WithInterval(clusterWaitInterval))
}

// findCluster describes the cluster by identifier and returns it. RDS signals a
// missing cluster with the typed DBClusterNotFoundFault, which maps to
// runtime.ErrNotFound; an empty result, or a returned cluster whose identifier
// does not match the request, likewise maps to not-found, guarding against a
// stale read just after a create.
func findCluster(
	ctx context.Context, client *rds.Client, id string,
) (*rdstypes.DBCluster, error) {
	paginator := rds.NewDescribeDBClustersPaginator(client,
		&rds.DescribeDBClustersInput{DBClusterIdentifier: aws.String(id)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isClusterNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe db clusters: %w", err)
		}
		for i := range page.DBClusters {
			c := page.DBClusters[i]
			if strings.EqualFold(aws.ToString(c.DBClusterIdentifier), id) {
				return &c, nil
			}
		}
	}
	return nil, runtime.ErrNotFound
}

// findGlobalClusterByClusterArn finds the identifier of the global cluster that
// lists clusterArn among its members, by filtering the global clusters on the
// cluster id. It returns runtime.ErrNotFound when no global cluster lists the ARN.
func findGlobalClusterByClusterArn(
	ctx context.Context, client *rds.Client, clusterArn string,
) (string, error) {
	paginator := rds.NewDescribeGlobalClustersPaginator(client,
		&rds.DescribeGlobalClustersInput{
			Filters: []rdstypes.Filter{
				{Name: aws.String("db-cluster-id"), Values: []string{clusterArn}},
			},
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("describe global clusters: %w", err)
		}
		for _, g := range page.GlobalClusters {
			for _, m := range g.GlobalClusterMembers {
				if aws.ToString(m.DBClusterArn) == clusterArn {
					return aws.ToString(g.GlobalClusterIdentifier), nil
				}
			}
		}
	}
	return "", runtime.ErrNotFound
}

// clusterMembers returns the instance identifiers that have joined the cluster.
func clusterMembers(cluster *rdstypes.DBCluster) []string {
	if len(cluster.DBClusterMembers) == 0 {
		return nil
	}
	members := make([]string, 0, len(cluster.DBClusterMembers))
	for _, m := range cluster.DBClusterMembers {
		members = append(members, aws.ToString(m.DBInstanceIdentifier))
	}
	return members
}

// hasPendingModifiedValues reports whether the cluster has any pending modified
// value. An available cluster with pending values is mid-change, so a wait that
// requires a fully settled cluster treats it as not yet ready.
func hasPendingModifiedValues(cluster *rdstypes.DBCluster) bool {
	p := cluster.PendingModifiedValues
	if p == nil {
		return false
	}
	return p.AllocatedStorage != nil ||
		p.BackupRetentionPeriod != nil ||
		p.CertificateDetails != nil ||
		p.DBClusterIdentifier != nil ||
		p.EngineVersion != nil ||
		p.IAMDatabaseAuthenticationEnabled != nil ||
		p.Iops != nil ||
		p.MasterUserPassword != nil ||
		p.PendingCloudwatchLogsExports != nil ||
		p.RdsCustomClusterConfiguration != nil ||
		p.StorageType != nil
}

// logExportsDiff computes the cloudwatch log-type enable and disable sets for a
// change from prior to desired: the types newly present are enabled, the types no
// longer present are disabled. RDS reconciles the exports by this diff rather than
// by a whole-set replacement.
func logExportsDiff(prior, desired []string) (enable, disable []string) {
	priorSet := map[string]bool{}
	for _, t := range prior {
		priorSet[t] = true
	}
	desiredSet := map[string]bool{}
	for _, t := range desired {
		desiredSet[t] = true
	}
	for _, t := range desired {
		if !priorSet[t] {
			enable = append(enable, t)
		}
	}
	for _, t := range prior {
		if !desiredSet[t] {
			disable = append(disable, t)
		}
	}
	return enable, disable
}

// serverlessV2Changed reports whether the serverless v2 scaling block changed
// between the prior and desired inputs. RDS cannot remove the block, so a removal
// in config is not a change to reconcile; only a present, differing block is.
func serverlessV2Changed(prior, desired *ClusterServerlessV2Scaling) bool {
	if desired == nil {
		return false
	}
	return runtime.Changed(prior, desired)
}

// isClusterNotFound reports whether err is the RDS typed fault for a missing DB
// cluster. RDS signals not-found with the typed exception DBClusterNotFoundFault
// rather than an HTTP status or a string code.
func isClusterNotFound(err error) bool {
	var fault *rdstypes.DBClusterNotFoundFault
	return errors.As(err, &fault)
}

// isGlobalClusterNotFound reports whether err is the RDS typed fault for a
// missing global cluster.
func isGlobalClusterNotFound(err error) bool {
	var fault *rdstypes.GlobalClusterNotFoundFault
	return errors.As(err, &fault)
}

// isInvalidClusterState reports whether err is the RDS typed fault raised when an
// operation is attempted in a cluster state that does not allow it.
func isInvalidClusterState(err error) bool {
	var fault *rdstypes.InvalidDBClusterStateFault
	return errors.As(err, &fault)
}

// isIamRoleInvalid reports whether err is the InvalidParameterValue RDS returns
// while a passed IAM role has not yet propagated. The condition clears on its own.
func isIamRoleInvalid(err error) bool {
	return isInvalidParameterValue(err, clusterIamRoleInvalidMsg)
}

// isS3RestoreRetryable reports whether err is one of the self-clearing
// InvalidParameterValue conditions an S3 restore can hit while the backup is not
// yet readable.
func isS3RestoreRetryable(err error) bool {
	const cannotDownload = "Files from the specified Amazon S3 bucket cannot be downloaded"
	return isInvalidParameterValue(err, cannotDownload) ||
		isInvalidParameterValue(err, "S3_SNAPSHOT_INGESTION") ||
		isInvalidParameterValue(err, "S3 bucket cannot be found")
}

// isModifyRetryable reports whether a modify error is one that clears on its own:
// a passed role not yet propagated, the cluster mid-transition, or the instance
// parameter group rejected on a non-major upgrade, which the caller heals before
// the retry.
func isModifyRetryable(err error) bool {
	return isIamRoleInvalid(err) ||
		isInvalidClusterState(err) ||
		isInstanceParamGroupMajorOnly(err)
}

// isInstanceParamGroupMajorOnly reports whether err is the InvalidParameterCombination
// RDS returns when the instance parameter group is sent on a minor version
// upgrade, which it accepts only for a major version upgrade.
func isInstanceParamGroupMajorOnly(err error) bool {
	return isInvalidParameterCombination(err,
		"db-instance-parameter-group-name can only be specified for a major")
}

// isDeleteRetryable reports whether a delete error is one that clears on its own:
// the cluster not yet in the available state, the global unjoin not yet settled,
// or deletion protection blocking the delete, which the caller heals before the
// retry.
func isDeleteRetryable(err error) bool {
	if isDeletionProtectionBlock(err) {
		return true
	}
	if isInvalidClusterState(err) {
		msg := errorMessage(err)
		return strings.Contains(msg, "is not currently in the available state") ||
			strings.Contains(msg, "cluster is a part of a global cluster") ||
			strings.Contains(msg, "part of a global cluster")
	}
	return false
}

// isDeletionProtectionBlock reports whether err is the InvalidParameterCombination
// RDS returns when a delete is blocked by deletion protection.
func isDeletionProtectionBlock(err error) bool {
	return isInvalidParameterCombination(err, "disable deletion pro")
}

// isNotInGlobalCluster reports whether err is the InvalidParameterValue RDS
// returns when a cluster is already absent from the global cluster it is being
// removed from.
func isNotInGlobalCluster(err error) bool {
	return isInvalidParameterValue(err, "is not found in global cluster")
}

// isGlobalDatabasesUnsupported reports whether err is the access-denied
// InvalidParameterValue RDS returns in a partition without Aurora Global
// Databases, where the membership read is simply skipped.
func isGlobalDatabasesUnsupported(err error) bool {
	return isInvalidParameterValue(err, "Access Denied to API Version: APIGlobalDatabases")
}

// isInvalidParameterValue reports whether err is an InvalidParameterValue whose
// message contains substr.
func isInvalidParameterValue(err error, substr string) bool {
	return isAPIErrorWithMessage(err, "InvalidParameterValue", substr)
}

// isInvalidParameterCombination reports whether err is an
// InvalidParameterCombination whose message contains substr.
func isInvalidParameterCombination(err error, substr string) bool {
	return isAPIErrorWithMessage(err, "InvalidParameterCombination", substr)
}

// isAPIErrorWithMessage reports whether err is an API error with the given code
// whose message contains substr. RDS raises several distinct, self-clearing
// conditions under one code, told apart only by message text.
func isAPIErrorWithMessage(err error, code, substr string) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) &&
		apiErr.ErrorCode() == code &&
		strings.Contains(apiErr.ErrorMessage(), substr)
}

// errorMessage returns the message of err when it is an API error, or its plain
// error text otherwise.
func errorMessage(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorMessage()
	}
	return err.Error()
}
