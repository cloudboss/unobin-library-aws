package rds

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// Create, update, and delete timeouts for the availability and deletion waits.
// An instance can take many minutes to provision, modify, or delete, well past
// the wait package defaults, so each wait is given an explicit bound.
const (
	instanceCreateTimeout = 50 * time.Minute
	instanceUpdateTimeout = 80 * time.Minute
	instanceDeleteTimeout = 60 * time.Minute
)

// propagationTimeout bounds the create and modify retries that clear once a
// dependency RDS names propagates: an enhanced-monitoring role, an instance
// profile, or an IAM role just granted permissions.
const propagationTimeout = 2 * time.Minute

// instancePendingStatuses are the DB instance statuses that mean the instance
// is still working toward available. A read seeing any of them keeps waiting.
var instancePendingStatuses = []string{
	"backing-up",
	"configuring-enhanced-monitoring",
	"configuring-iam-database-auth",
	"configuring-log-exports",
	"creating",
	"maintenance",
	"modifying",
	"moving-to-vpc",
	"rebooting",
	"renaming",
	"resetting-master-credentials",
	"starting",
	"stopping",
	"storage-config-upgrade",
	"storage-full",
	"storage-initialization",
	"storage-optimization",
	"upgrading",
}

// instanceAvailableStatuses are the terminal-success statuses the availability
// wait targets. An instance reaching either is settled. storage-optimization is
// both a pending status here and a success target: an instance can sit in it
// indefinitely while still usable, so the wait accepts it as done.
var instanceAvailableStatuses = []string{
	"available",
	"storage-optimization",
}

// Instance is a standalone Amazon RDS database instance. It has five
// mutually-exclusive create modes, chosen by which input is set: a read replica
// of another instance (replicate-source-db), a restore from an S3 backup
// (s3-import), a restore from a snapshot (snapshot-identifier), a point-in-time
// restore (restore-to-point-in-time), or a plain new instance when none is set.
// Each create call under-fills the instance, so the fields it does not accept
// are reconciled by a follow-on ModifyDBInstance, and the read-replica path may
// also reboot to pick up a parameter-group change. Every create response is
// partial: the endpoint, ARN, status, and managed secret settle only after the
// instance becomes available, so Create returns values from a post-wait read.
//
// The password is reconciled either directly (password) or by letting RDS
// manage it in Secrets Manager (manage-master-user-password); the two conflict.
// On delete, a final snapshot is taken unless skip-final-snapshot is set, and a
// deletion-protection retry clears protection before retrying the delete. The
// blue/green deployment update strategy is not modeled: every ModifyDBInstance
// applies immediately in place, so there is no apply-immediately input.
type Instance struct {
	Identifier    string  `ub:"identifier"`
	Engine        *string `ub:"engine"`
	EngineVersion *string `ub:"engine-version"`
	Username      *string `ub:"username"`
	Password      *string `ub:"password,sensitive"`
	InstanceClass *string `ub:"instance-class"`

	AllocatedStorage    *int64  `ub:"allocated-storage"`
	MaxAllocatedStorage *int64  `ub:"max-allocated-storage"`
	Iops                *int64  `ub:"iops"`
	StorageType         *string `ub:"storage-type"`
	StorageThroughput   *int64  `ub:"storage-throughput"`
	StorageEncrypted    *bool   `ub:"storage-encrypted"`
	KmsKeyId            *string `ub:"kms-key-id"`

	DbName              *string  `ub:"db-name"`
	DbSubnetGroupName   *string  `ub:"db-subnet-group-name"`
	ParameterGroupName  *string  `ub:"parameter-group-name"`
	OptionGroupName     *string  `ub:"option-group-name"`
	Port                *int64   `ub:"port"`
	AvailabilityZone    *string  `ub:"availability-zone"`
	MultiAz             *bool    `ub:"multi-az"`
	PubliclyAccessible  *bool    `ub:"publicly-accessible"`
	NetworkType         *string  `ub:"network-type"`
	VpcSecurityGroupIds []string `ub:"vpc-security-group-ids"`

	LicenseModel          *string `ub:"license-model"`
	CharacterSetName      *string `ub:"character-set-name"`
	NcharCharacterSetName *string `ub:"nchar-character-set-name"`
	Timezone              *string `ub:"timezone"`

	BackupRetentionPeriod    *int64  `ub:"backup-retention-period"`
	BackupWindow             *string `ub:"backup-window"`
	BackupTarget             *string `ub:"backup-target"`
	CopyTagsToSnapshot       *bool   `ub:"copy-tags-to-snapshot"`
	MaintenanceWindow        *string `ub:"maintenance-window"`
	AutoMinorVersionUpgrade  *bool   `ub:"auto-minor-version-upgrade"`
	AllowMajorVersionUpgrade *bool   `ub:"allow-major-version-upgrade"`
	DeletionProtection       *bool   `ub:"deletion-protection"`

	CaCertIdentifier         *string `ub:"ca-cert-identifier"`
	CustomerOwnedIpEnabled   *bool   `ub:"customer-owned-ip-enabled"`
	CustomIamInstanceProfile *string `ub:"custom-iam-instance-profile"`
	DedicatedLogVolume       *bool   `ub:"dedicated-log-volume"`

	IamDatabaseAuthenticationEnabled *bool    `ub:"iam-database-authentication-enabled"`
	DatabaseInsightsMode             *string  `ub:"database-insights-mode"`
	EnabledCloudwatchLogsExports     []string `ub:"enabled-cloudwatch-logs-exports"`
	EngineLifecycleSupport           *string  `ub:"engine-lifecycle-support"`

	MonitoringInterval                 *int64  `ub:"monitoring-interval"`
	MonitoringRoleArn                  *string `ub:"monitoring-role-arn"`
	EnablePerformanceInsights          *bool   `ub:"enable-performance-insights"`
	PerformanceInsightsKmsKeyId        *string `ub:"performance-insights-kms-key-id"`
	PerformanceInsightsRetentionPeriod *int64  `ub:"performance-insights-retention-period"`

	ManageMasterUserPassword *bool   `ub:"manage-master-user-password"`
	MasterUserSecretKmsKeyId *string `ub:"master-user-secret-kms-key-id"`

	Domain              *string  `ub:"domain"`
	DomainIamRoleName   *string  `ub:"domain-iam-role-name"`
	DomainFqdn          *string  `ub:"domain-fqdn"`
	DomainOu            *string  `ub:"domain-ou"`
	DomainAuthSecretArn *string  `ub:"domain-auth-secret-arn"`
	DomainDnsIps        []string `ub:"domain-dns-ips"`

	ReplicateSourceDb    *string                       `ub:"replicate-source-db"`
	ReplicaMode          *string                       `ub:"replica-mode"`
	SnapshotIdentifier   *string                       `ub:"snapshot-identifier"`
	S3Import             *InstanceS3Import             `ub:"s3-import"`
	RestoreToPointInTime *InstanceRestoreToPointInTime `ub:"restore-to-point-in-time"`

	SkipFinalSnapshot       *bool   `ub:"skip-final-snapshot"`
	FinalSnapshotIdentifier *string `ub:"final-snapshot-identifier"`
	DeleteAutomatedBackups  *bool   `ub:"delete-automated-backups"`

	Tags map[string]string `ub:"tags"`
}

// createResult is what a create mode produces beyond the new instance's resource
// id: whether a reboot is needed to load a parameter group, and whether the
// multi-AZ setting was deferred from a snapshot restore to the follow-on modify.
type createResult struct {
	resourceID   string
	needReboot   bool
	deferMultiAz bool
}

// InstanceOutput holds the values RDS computes or fills for an instance. The ARN
// is the instance's handle in tagging and policies; the resource id is the
// stable identity Read keys on, since the user identifier can be renamed in
// place. The endpoint, address, port, and hosted-zone settle only after the
// instance is available, as does the managed secret and the engine version RDS
// resolved from the requested one. The replicas are the identifiers of the read
// replicas pointing at this instance.
type InstanceOutput struct {
	Arn                  string                    `ub:"arn"`
	ResourceId           string                    `ub:"resource-id"`
	Endpoint             string                    `ub:"endpoint"`
	Address              string                    `ub:"address"`
	Port                 int64                     `ub:"port"`
	HostedZoneId         string                    `ub:"hosted-zone-id"`
	Status               string                    `ub:"status"`
	EngineVersionActual  string                    `ub:"engine-version-actual"`
	CaCertIdentifier     string                    `ub:"ca-cert-identifier"`
	LatestRestorableTime string                    `ub:"latest-restorable-time"`
	MasterUserSecret     *InstanceMasterUserSecret `ub:"master-user-secret"`
	ListenerEndpoint     *InstanceEndpoint         `ub:"listener-endpoint"`
	Replicas             []string                  `ub:"replicas"`
}

func (r *Instance) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs RDS fixes when an instance is created. A change
// to any of them cannot be applied to the running instance and requires a new
// one. The identifier is not here: a rename rides ModifyDBInstance through its
// NewDBInstanceIdentifier field. replicate-source-db is not here either: its
// removal promotes the replica to a standalone instance rather than replacing
// it. The s3-import and restore-to-point-in-time blocks are immutable as wholes,
// so a change to any of their inner fields replaces the instance.
func (r *Instance) ReplaceFields() []string {
	return []string{
		"availability-zone",
		"backup-target",
		"character-set-name",
		"db-name",
		"engine",
		"kms-key-id",
		"nchar-character-set-name",
		"storage-encrypted",
		"timezone",
		"username",
		"custom-iam-instance-profile",
		"snapshot-identifier",
		"restore-to-point-in-time",
		"s3-import",
	}
}

// Defaults marks the optional collection inputs an instance may omit. A bare
// list or map input is otherwise compile-required; these are all optional.
func (r Instance) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.VpcSecurityGroupIds),
		defaults.Optional(r.EnabledCloudwatchLogsExports),
		defaults.Optional(r.DomainDnsIps),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules RDS enforces on an instance's inputs. At most
// one create mode is selected. The managed master-user password conflicts with
// an explicit password. The two Active Directory modes -- AWS-managed and
// self-managed -- are mutually exclusive. The optional enums and numeric bounds
// are checked only when present. Several rules RDS enforces are too conditional
// for the constraint layer and are checked in Create instead: the create-mode
// required fields (engine, username, allocated-storage for the plain and S3
// modes), the exactly-two domain DNS IPs, the restore-point exclusivity inside
// the point-in-time block, the performance-insights retention divisibility, and
// the master-user-secret-kms-key-id requiring manage-master-user-password.
func (r Instance) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(
			r.ReplicateSourceDb, r.S3Import, r.SnapshotIdentifier, r.RestoreToPointInTime),
		constraint.AtMostOneOf(r.ManageMasterUserPassword, r.Password),
		constraint.AtMostOneOf(r.Domain, r.DomainFqdn),
		constraint.AtMostOneOf(r.DomainIamRoleName, r.DomainFqdn),
		constraint.ForbiddenWith(r.CharacterSetName, r.ReplicateSourceDb,
			r.S3Import, r.SnapshotIdentifier, r.RestoreToPointInTime),
		constraint.ForbiddenWith(r.DbName, r.ReplicateSourceDb),
		constraint.ForbiddenWith(r.Username, r.ReplicateSourceDb),
		constraint.ForbiddenWith(r.Timezone, r.S3Import),
		constraint.ForbiddenWith(r.BackupTarget, r.S3Import),
		constraint.When(constraint.Present(r.DatabaseInsightsMode)).
			Require(constraint.OneOf(r.DatabaseInsightsMode, "standard", "advanced")).
			Message("database-insights-mode must be standard or advanced"),
		constraint.When(constraint.Present(r.ReplicaMode)).
			Require(constraint.OneOf(r.ReplicaMode, "open-read-only", "mounted")).
			Message("replica-mode must be open-read-only or mounted"),
		constraint.When(constraint.Present(r.EngineLifecycleSupport)).
			Require(constraint.OneOf(r.EngineLifecycleSupport,
				"open-source-rds-extended-support",
				"open-source-rds-extended-support-disabled")).
			Message("engine-lifecycle-support must be a valid extended-support value"),
		constraint.When(constraint.Present(r.NetworkType)).
			Require(constraint.OneOf(r.NetworkType, "IPV4", "DUAL")).
			Message("network-type must be IPV4 or DUAL"),
		constraint.When(constraint.Present(r.BackupTarget)).
			Require(constraint.OneOf(r.BackupTarget, "outposts", "region")).
			Message("backup-target must be outposts or region"),
		constraint.When(constraint.Present(r.StorageType)).
			Require(constraint.OneOf(r.StorageType, "gp2", "gp3", "io1", "io2", "standard")).
			Message("storage-type must be gp2, gp3, io1, io2, or standard"),
		constraint.When(constraint.Present(r.BackupRetentionPeriod)).
			Require(constraint.AtLeast(r.BackupRetentionPeriod, 0),
				constraint.AtMost(r.BackupRetentionPeriod, 35)).
			Message("backup-retention-period must be between 0 and 35"),
		constraint.When(constraint.Present(r.MonitoringInterval)).
			Require(constraint.OneOf(r.MonitoringInterval, 0, 1, 5, 10, 15, 30, 60)).
			Message("monitoring-interval must be 0, 1, 5, 10, 15, 30, or 60"),
		constraint.ForEach(r.EnabledCloudwatchLogsExports,
			func(v string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(v,
						"agent", "alert", "audit", "diag.log", "error", "general",
						"iam-db-auth-error", "listener", "notify.log", "oemagent",
						"postgresql", "slowquery", "trace", "upgrade")).
						Message("enabled-cloudwatch-logs-exports entries must be " +
							"valid instance log types"),
				}
			}),
	}
}

func (r *Instance) Create(ctx context.Context, cfg any) (*InstanceOutput, error) {
	if err := r.validateCreate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	result, err := r.runCreateMode(ctx, client)
	if err != nil {
		return nil, err
	}
	// Every create call returns before the instance is usable, so wait for it to
	// reach available before doing anything else with it.
	if err := r.waitAvailable(ctx, client, instanceCreateTimeout); err != nil {
		return nil, err
	}
	// A restore or replica call cannot accept every field, so the ones it left
	// out are reconciled now by a single modify that applies immediately, then
	// the instance is waited available again.
	if modify := r.createFollowOnModify(result.deferMultiAz); modify != nil {
		modify.DBInstanceIdentifier = aws.String(r.Identifier)
		if err := r.modifyAndWait(ctx, client, modify, instanceCreateTimeout); err != nil {
			return nil, err
		}
	}
	// A read replica created against a non-default parameter group must reboot to
	// load it; the reboot is the only create-time reboot RDS needs.
	if result.needReboot {
		if err := r.rebootAndWait(ctx, client, instanceCreateTimeout); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, result.resourceID)
}

func (r *Instance) Read(
	ctx context.Context, cfg any, prior *InstanceOutput,
) (*InstanceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.ResourceId)
}

func (r *Instance) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Instance, *InstanceOutput],
) (*InstanceOutput, error) {
	if err := r.validateCommon(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resourceID := prior.Outputs.ResourceId
	// Removing the replication source promotes the replica to a standalone
	// instance; pointing it at a different source is not something RDS can do.
	if runtime.Changed(prior.Inputs.ReplicateSourceDb, r.ReplicateSourceDb) {
		if r.ReplicateSourceDb != nil {
			return nil, errors.New("cannot elect a new replication source for an existing instance")
		}
		if err := r.promoteAndWait(ctx, client); err != nil {
			return nil, err
		}
	}
	// Every other field RDS can change in place rides one modify that sends only
	// what changed, applies immediately, and waits the instance available.
	if modify := r.updateModify(prior); modify != nil {
		modify.DBInstanceIdentifier = aws.String(r.currentIdentifier(prior))
		if err := r.modifyAndWait(ctx, client, modify, instanceUpdateTimeout); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, prior.Outputs.Arn, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, resourceID)
}

func (r *Instance) Delete(ctx context.Context, cfg any, prior *InstanceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in, err := r.deleteInput()
	if err != nil {
		return err
	}
	_, err = client.DeleteDBInstance(ctx, in)
	if err != nil {
		if instanceGoneOnDelete(err) {
			return r.waitDeleted(ctx, client)
		}
		// RDS refuses to delete an instance with deletion protection on. When the
		// config does not want protection, clear it with a modify and retry the
		// delete; otherwise the refusal is the operator's own setting and stands.
		if instanceBlockedByDeletionProtection(err) && !aws.ToBool(r.DeletionProtection) {
			if err := r.clearDeletionProtection(ctx, client); err != nil {
				return err
			}
			if _, err := client.DeleteDBInstance(ctx, in); err != nil {
				if instanceGoneOnDelete(err) {
					return r.waitDeleted(ctx, client)
				}
				return fmt.Errorf("delete db instance: %w", err)
			}
			return r.waitDeleted(ctx, client)
		}
		return fmt.Errorf("delete db instance: %w", err)
	}
	return r.waitDeleted(ctx, client)
}

// validateCreate checks the create-mode rules the constraint layer cannot
// express: the fields a mode requires, the restore-point exclusivity in the
// point-in-time block, and the rules common to create and update. Each returns
// a clear error so a bad config fails before any call.
func (r *Instance) validateCreate() error {
	mode := r.createMode()
	if mode == createModePlain || mode == createModeS3 {
		if r.Engine == nil || r.Username == nil || r.AllocatedStorage == nil {
			return fmt.Errorf(
				"engine, username, and allocated-storage are required for a %s instance", mode)
		}
	}
	if mode == createModeS3 {
		s := r.S3Import
		if s.BucketName == nil || s.IngestionRole == nil ||
			s.SourceEngine == nil || s.SourceEngineVersion == nil {
			return errors.New(
				"s3-import requires bucket-name, ingestion-role, source-engine, " +
					"and source-engine-version")
		}
	}
	if mode == createModePointInTime {
		b := r.RestoreToPointInTime
		if b.RestoreTime != nil && aws.ToBool(b.UseLatestRestorableTime) {
			return errors.New(
				"restore-to-point-in-time cannot set both restore-time and " +
					"use-latest-restorable-time")
		}
	}
	return r.validateCommon()
}

// validateCommon checks the rules the constraint layer cannot express that apply
// to both create and update: the exactly-two domain DNS IPs, the managed-secret
// KMS key needing the managed password, and the performance-insights retention
// divisibility.
func (r *Instance) validateCommon() error {
	if err := validateInstanceIdentifier(r.Identifier); err != nil {
		return err
	}
	if len(r.DomainDnsIps) != 0 && len(r.DomainDnsIps) != 2 {
		return errors.New("domain-dns-ips must contain exactly two IP addresses")
	}
	if r.MasterUserSecretKmsKeyId != nil && !aws.ToBool(r.ManageMasterUserPassword) {
		return errors.New(
			"master-user-secret-kms-key-id requires manage-master-user-password")
	}
	return validatePerformanceInsightsRetention(r.PerformanceInsightsRetentionPeriod)
}

// validateInstanceIdentifier checks the instance identifier against the rules
// RDS enforces, which the constraint layer cannot express as a pattern: a
// leading lowercase letter, only lowercase letters, digits, and hyphens, no
// doubled hyphen, and no trailing hyphen.
func validateInstanceIdentifier(id string) error {
	if id == "" || id[0] < 'a' || id[0] > 'z' {
		return errors.New("identifier must start with a lowercase letter")
	}
	for i := range len(id) {
		c := id[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if !lower && !digit && c != '-' {
			return errors.New(
				"identifier must contain only lowercase letters, digits, and hyphens")
		}
	}
	if strings.Contains(id, "--") {
		return errors.New("identifier must not contain two consecutive hyphens")
	}
	if strings.HasSuffix(id, "-") {
		return errors.New("identifier must not end with a hyphen")
	}
	return nil
}

// createMode reports which of the five create modes the inputs select, in the
// order RDS checks them.
func (r *Instance) createMode() createMode {
	switch {
	case r.ReplicateSourceDb != nil:
		return createModeReplica
	case r.S3Import != nil:
		return createModeS3
	case r.SnapshotIdentifier != nil:
		return createModeSnapshot
	case r.RestoreToPointInTime != nil:
		return createModePointInTime
	default:
		return createModePlain
	}
}

// createMode names one of the five ways an instance is created.
type createMode string

const (
	createModePlain       createMode = "create"
	createModeReplica     createMode = "read-replica"
	createModeS3          createMode = "restore-from-s3"
	createModeSnapshot    createMode = "restore-from-snapshot"
	createModePointInTime createMode = "restore-to-point-in-time"
)

// runCreateMode issues the create call for the selected mode and returns its
// result: the new instance's resource id plus the follow-on flags. Only the
// read-replica path needs a reboot, and only the snapshot path can defer
// multi-AZ to the follow-on modify.
func (r *Instance) runCreateMode(
	ctx context.Context, client *rds.Client,
) (createResult, error) {
	switch r.createMode() {
	case createModeReplica:
		id, needReboot, err := r.createReadReplica(ctx, client)
		return createResult{resourceID: id, needReboot: needReboot}, err
	case createModeS3:
		id, err := r.restoreFromS3(ctx, client)
		return createResult{resourceID: id}, err
	case createModeSnapshot:
		id, deferMultiAz, err := r.restoreFromSnapshot(ctx, client)
		return createResult{resourceID: id, deferMultiAz: deferMultiAz}, err
	case createModePointInTime:
		id, err := r.restoreToPointInTime(ctx, client)
		return createResult{resourceID: id}, err
	default:
		id, err := r.createPlain(ctx, client)
		return createResult{resourceID: id}, err
	}
}

// createPlain issues CreateDBInstance for a brand-new instance. The call accepts
// nearly every field; only the CA certificate identifier is left to the
// follow-on modify. It retries through the enhanced-monitoring and instance-role
// propagation races RDS reports right after a role is created.
func (r *Instance) createPlain(ctx context.Context, client *rds.Client) (string, error) {
	in := &rds.CreateDBInstanceInput{
		DBInstanceIdentifier:               aws.String(r.Identifier),
		DBInstanceClass:                    r.InstanceClass,
		Engine:                             lowerString(r.Engine),
		EngineVersion:                      r.EngineVersion,
		MasterUsername:                     r.Username,
		MasterUserPassword:                 r.Password,
		AllocatedStorage:                   ptr.Int32(r.AllocatedStorage),
		MaxAllocatedStorage:                ptr.Int32(r.MaxAllocatedStorage),
		Iops:                               ptr.Int32(r.Iops),
		StorageType:                        r.StorageType,
		StorageThroughput:                  ptr.Int32(r.StorageThroughput),
		StorageEncrypted:                   r.StorageEncrypted,
		KmsKeyId:                           r.KmsKeyId,
		DBName:                             r.DbName,
		DBSubnetGroupName:                  r.DbSubnetGroupName,
		DBParameterGroupName:               r.ParameterGroupName,
		OptionGroupName:                    r.OptionGroupName,
		Port:                               ptr.Int32(r.Port),
		AvailabilityZone:                   r.AvailabilityZone,
		MultiAZ:                            r.MultiAz,
		PubliclyAccessible:                 r.PubliclyAccessible,
		NetworkType:                        r.NetworkType,
		VpcSecurityGroupIds:                r.VpcSecurityGroupIds,
		LicenseModel:                       r.LicenseModel,
		CharacterSetName:                   r.CharacterSetName,
		NcharCharacterSetName:              r.NcharCharacterSetName,
		Timezone:                           r.Timezone,
		BackupRetentionPeriod:              ptr.Int32(r.BackupRetentionPeriod),
		PreferredBackupWindow:              r.BackupWindow,
		BackupTarget:                       r.BackupTarget,
		CopyTagsToSnapshot:                 r.CopyTagsToSnapshot,
		PreferredMaintenanceWindow:         lowerString(r.MaintenanceWindow),
		AutoMinorVersionUpgrade:            r.AutoMinorVersionUpgrade,
		DeletionProtection:                 r.DeletionProtection,
		EnableCustomerOwnedIp:              r.CustomerOwnedIpEnabled,
		CustomIamInstanceProfile:           r.CustomIamInstanceProfile,
		DedicatedLogVolume:                 r.DedicatedLogVolume,
		EnableIAMDatabaseAuthentication:    r.IamDatabaseAuthenticationEnabled,
		EnableCloudwatchLogsExports:        r.EnabledCloudwatchLogsExports,
		EngineLifecycleSupport:             r.EngineLifecycleSupport,
		MonitoringInterval:                 ptr.Int32(r.MonitoringInterval),
		MonitoringRoleArn:                  r.MonitoringRoleArn,
		EnablePerformanceInsights:          r.EnablePerformanceInsights,
		PerformanceInsightsKMSKeyId:        r.PerformanceInsightsKmsKeyId,
		PerformanceInsightsRetentionPeriod: ptr.Int32(r.PerformanceInsightsRetentionPeriod),
		ManageMasterUserPassword:           r.ManageMasterUserPassword,
		MasterUserSecretKmsKeyId:           r.MasterUserSecretKmsKeyId,
		Tags:                               tagList(r.Tags),
	}
	if r.DatabaseInsightsMode != nil {
		in.DatabaseInsightsMode = rdstypes.DatabaseInsightsMode(*r.DatabaseInsightsMode)
	}
	r.setDomainInput(&in.Domain, &in.DomainIAMRoleName, &in.DomainFqdn,
		&in.DomainOu, &in.DomainAuthSecretArn, &in.DomainDnsIps)
	var out *rds.CreateDBInstanceOutput
	err := retry.OnError(ctx, instanceCreateRetryable, func(ctx context.Context) error {
		var err error
		out, err = client.CreateDBInstance(ctx, in)
		return err
	}, retry.WithTimeout(propagationTimeout), retry.WithInterval(10*time.Second))
	if err != nil {
		return "", fmt.Errorf("create db instance: %w", err)
	}
	return aws.ToString(out.DBInstance.DbiResourceId), nil
}

// createReadReplica issues CreateDBInstanceReadReplica. The call forbids the
// username and database name and ignores allocated storage; the parameter group
// is sent only when the replica is cross-region, and if RDS rejects it for the
// engine, the call retries without it. The replica reboots afterward when its
// parameter group differs from the source's, so a different parameter group
// signals the reboot.
func (r *Instance) createReadReplica(
	ctx context.Context, client *rds.Client,
) (string, bool, error) {
	in := &rds.CreateDBInstanceReadReplicaInput{
		DBInstanceIdentifier:               aws.String(r.Identifier),
		SourceDBInstanceIdentifier:         r.ReplicateSourceDb,
		DBInstanceClass:                    r.InstanceClass,
		Iops:                               ptr.Int32(r.Iops),
		StorageType:                        r.StorageType,
		StorageThroughput:                  ptr.Int32(r.StorageThroughput),
		KmsKeyId:                           r.KmsKeyId,
		DBSubnetGroupName:                  r.DbSubnetGroupName,
		OptionGroupName:                    r.OptionGroupName,
		Port:                               ptr.Int32(r.Port),
		AvailabilityZone:                   r.AvailabilityZone,
		MultiAZ:                            r.MultiAz,
		PubliclyAccessible:                 r.PubliclyAccessible,
		NetworkType:                        r.NetworkType,
		VpcSecurityGroupIds:                r.VpcSecurityGroupIds,
		BackupTarget:                       r.BackupTarget,
		CopyTagsToSnapshot:                 r.CopyTagsToSnapshot,
		AutoMinorVersionUpgrade:            r.AutoMinorVersionUpgrade,
		CustomIamInstanceProfile:           r.CustomIamInstanceProfile,
		DedicatedLogVolume:                 r.DedicatedLogVolume,
		EnableCustomerOwnedIp:              r.CustomerOwnedIpEnabled,
		EnableIAMDatabaseAuthentication:    r.IamDatabaseAuthenticationEnabled,
		EnableCloudwatchLogsExports:        r.EnabledCloudwatchLogsExports,
		MonitoringInterval:                 ptr.Int32(r.MonitoringInterval),
		MonitoringRoleArn:                  r.MonitoringRoleArn,
		EnablePerformanceInsights:          r.EnablePerformanceInsights,
		PerformanceInsightsKMSKeyId:        r.PerformanceInsightsKmsKeyId,
		PerformanceInsightsRetentionPeriod: ptr.Int32(r.PerformanceInsightsRetentionPeriod),
		Tags:                               tagList(r.Tags),
	}
	if r.DatabaseInsightsMode != nil {
		in.DatabaseInsightsMode = rdstypes.DatabaseInsightsMode(*r.DatabaseInsightsMode)
	}
	if r.ReplicaMode != nil {
		in.ReplicaMode = rdstypes.ReplicaMode(*r.ReplicaMode)
	}
	// A cross-region encrypted replica must name the source region; RDS reads
	// it from the source ARN, whose fourth segment is the region.
	if r.KmsKeyId != nil && strings.HasPrefix(aws.ToString(r.ReplicateSourceDb), "arn:") {
		if parts := strings.Split(aws.ToString(r.ReplicateSourceDb), ":"); len(parts) > 3 {
			in.SourceRegion = aws.String(parts[3])
		}
	}
	r.setDomainInput(&in.Domain, &in.DomainIAMRoleName, &in.DomainFqdn,
		&in.DomainOu, &in.DomainAuthSecretArn, &in.DomainDnsIps)
	out, err := r.createReadReplicaCall(ctx, client, in)
	if err != nil {
		return "", false, err
	}
	needReboot := r.ParameterGroupName != nil
	return aws.ToString(out.DBInstance.DbiResourceId), needReboot, nil
}

// createReadReplicaCall runs the replica create with its retries. It retries
// through the enhanced-monitoring propagation race; if RDS rejects a parameter
// group because the engine forbids one at replica creation, it clears the group
// and retries once, deferring the group to the follow-on modify.
func (r *Instance) createReadReplicaCall(
	ctx context.Context, client *rds.Client, in *rds.CreateDBInstanceReadReplicaInput,
) (*rds.CreateDBInstanceReadReplicaOutput, error) {
	var out *rds.CreateDBInstanceReadReplicaOutput
	err := retry.OnError(ctx, instanceEnhancedMonitoringRetryable, func(ctx context.Context) error {
		var err error
		out, err = client.CreateDBInstanceReadReplica(ctx, in)
		return err
	}, retry.WithTimeout(propagationTimeout), retry.WithInterval(10*time.Second))
	if err == nil {
		return out, nil
	}
	if in.DBParameterGroupName != nil && instanceReplicaParamGroupRejected(err) {
		in.DBParameterGroupName = nil
		err = retry.OnError(ctx, instanceEnhancedMonitoringRetryable, func(ctx context.Context) error {
			var err error
			out, err = client.CreateDBInstanceReadReplica(ctx, in)
			return err
		}, retry.WithTimeout(propagationTimeout), retry.WithInterval(10*time.Second))
	}
	if err != nil {
		return nil, fmt.Errorf("create db instance read replica: %w", err)
	}
	return out, nil
}

// restoreFromS3 issues RestoreDBInstanceFromS3. The call is the closest of the
// restores to a plain create and requires engine, username, and allocated
// storage. It retries through the enhanced-monitoring race and the S3 ingestion
// races RDS reports while the bucket access settles.
func (r *Instance) restoreFromS3(ctx context.Context, client *rds.Client) (string, error) {
	s := r.S3Import
	in := &rds.RestoreDBInstanceFromS3Input{
		DBInstanceIdentifier:               aws.String(r.Identifier),
		DBInstanceClass:                    r.InstanceClass,
		Engine:                             lowerString(r.Engine),
		EngineVersion:                      r.EngineVersion,
		MasterUsername:                     r.Username,
		MasterUserPassword:                 r.Password,
		AllocatedStorage:                   ptr.Int32(r.AllocatedStorage),
		MaxAllocatedStorage:                ptr.Int32(r.MaxAllocatedStorage),
		Iops:                               ptr.Int32(r.Iops),
		StorageType:                        r.StorageType,
		StorageThroughput:                  ptr.Int32(r.StorageThroughput),
		StorageEncrypted:                   r.StorageEncrypted,
		KmsKeyId:                           r.KmsKeyId,
		DBName:                             r.DbName,
		DBSubnetGroupName:                  r.DbSubnetGroupName,
		DBParameterGroupName:               r.ParameterGroupName,
		OptionGroupName:                    r.OptionGroupName,
		Port:                               ptr.Int32(r.Port),
		AvailabilityZone:                   r.AvailabilityZone,
		MultiAZ:                            r.MultiAz,
		PubliclyAccessible:                 r.PubliclyAccessible,
		NetworkType:                        r.NetworkType,
		VpcSecurityGroupIds:                r.VpcSecurityGroupIds,
		LicenseModel:                       r.LicenseModel,
		BackupRetentionPeriod:              ptr.Int32(r.BackupRetentionPeriod),
		PreferredBackupWindow:              r.BackupWindow,
		CopyTagsToSnapshot:                 r.CopyTagsToSnapshot,
		PreferredMaintenanceWindow:         lowerString(r.MaintenanceWindow),
		AutoMinorVersionUpgrade:            r.AutoMinorVersionUpgrade,
		DeletionProtection:                 r.DeletionProtection,
		DedicatedLogVolume:                 r.DedicatedLogVolume,
		EnableIAMDatabaseAuthentication:    r.IamDatabaseAuthenticationEnabled,
		EnableCloudwatchLogsExports:        r.EnabledCloudwatchLogsExports,
		EngineLifecycleSupport:             r.EngineLifecycleSupport,
		MonitoringInterval:                 ptr.Int32(r.MonitoringInterval),
		MonitoringRoleArn:                  r.MonitoringRoleArn,
		EnablePerformanceInsights:          r.EnablePerformanceInsights,
		PerformanceInsightsKMSKeyId:        r.PerformanceInsightsKmsKeyId,
		PerformanceInsightsRetentionPeriod: ptr.Int32(r.PerformanceInsightsRetentionPeriod),
		ManageMasterUserPassword:           r.ManageMasterUserPassword,
		MasterUserSecretKmsKeyId:           r.MasterUserSecretKmsKeyId,
		S3BucketName:                       s.BucketName,
		S3Prefix:                           s.BucketPrefix,
		S3IngestionRoleArn:                 s.IngestionRole,
		SourceEngine:                       s.SourceEngine,
		SourceEngineVersion:                s.SourceEngineVersion,
		Tags:                               tagList(r.Tags),
	}
	if r.DatabaseInsightsMode != nil {
		in.DatabaseInsightsMode = rdstypes.DatabaseInsightsMode(*r.DatabaseInsightsMode)
	}
	var out *rds.RestoreDBInstanceFromS3Output
	err := retry.OnError(ctx, instanceS3RestoreRetryable, func(ctx context.Context) error {
		var err error
		out, err = client.RestoreDBInstanceFromS3(ctx, in)
		return err
	}, retry.WithTimeout(propagationTimeout), retry.WithInterval(10*time.Second))
	if err != nil {
		return "", fmt.Errorf("restore db instance from s3: %w", err)
	}
	return aws.ToString(out.DBInstance.DbiResourceId), nil
}

// restoreFromSnapshot issues RestoreDBInstanceFromDBSnapshot. The call ignores
// the storage, password, and monitoring fields, which the follow-on modify
// reconciles. The database name is skipped for the engines where a snapshot
// restore rejects it. It retries through the instance-profile role race. The
// returned bool reports whether multi-AZ was deferred to the follow-on modify.
func (r *Instance) restoreFromSnapshot(
	ctx context.Context, client *rds.Client,
) (string, bool, error) {
	in := &rds.RestoreDBInstanceFromDBSnapshotInput{
		DBInstanceIdentifier:            aws.String(r.Identifier),
		DBSnapshotIdentifier:            r.SnapshotIdentifier,
		DBInstanceClass:                 r.InstanceClass,
		Engine:                          lowerString(r.Engine),
		DBSubnetGroupName:               r.DbSubnetGroupName,
		DBParameterGroupName:            r.ParameterGroupName,
		OptionGroupName:                 r.OptionGroupName,
		Port:                            ptr.Int32(r.Port),
		AvailabilityZone:                r.AvailabilityZone,
		PubliclyAccessible:              r.PubliclyAccessible,
		NetworkType:                     r.NetworkType,
		VpcSecurityGroupIds:             r.VpcSecurityGroupIds,
		LicenseModel:                    r.LicenseModel,
		BackupTarget:                    r.BackupTarget,
		CopyTagsToSnapshot:              r.CopyTagsToSnapshot,
		AutoMinorVersionUpgrade:         r.AutoMinorVersionUpgrade,
		DeletionProtection:              r.DeletionProtection,
		CustomIamInstanceProfile:        r.CustomIamInstanceProfile,
		DedicatedLogVolume:              r.DedicatedLogVolume,
		EnableCustomerOwnedIp:           r.CustomerOwnedIpEnabled,
		EnableIAMDatabaseAuthentication: r.IamDatabaseAuthenticationEnabled,
		EngineLifecycleSupport:          r.EngineLifecycleSupport,
		Tags:                            tagList(r.Tags),
	}
	if r.DbName != nil && snapshotKeepsDbName(r.Engine) {
		in.DBName = r.DbName
	}
	r.setDomainInput(&in.Domain, &in.DomainIAMRoleName, &in.DomainFqdn,
		&in.DomainOu, &in.DomainAuthSecretArn, &in.DomainDnsIps)
	// Multi-AZ is a restore parameter for every engine but SQL Server, whose
	// mirroring the restore rejects while backups are at zero; for SQL Server it
	// is deferred to the follow-on modify. When the engine is not declared, the
	// snapshot may still be SQL Server, in which case the restore returns the
	// mirroring error and the call retries without multi-AZ, deferring it too.
	deferMultiAz := engineIsSqlServer(r.Engine)
	if !deferMultiAz {
		in.MultiAZ = r.MultiAz
	}
	out, deferred, err := r.restoreFromSnapshotCall(ctx, client, in, deferMultiAz)
	if err != nil {
		return "", false, err
	}
	return aws.ToString(out.DBInstance.DbiResourceId), deferred, nil
}

// restoreFromSnapshotCall runs the snapshot restore with its retries. It retries
// through the instance-profile role race; if the restore rejects multi-AZ
// mirroring because the snapshot is SQL Server and backups are at zero, it
// clears multi-AZ and retries once, reporting that multi-AZ is now deferred to
// the follow-on modify.
func (r *Instance) restoreFromSnapshotCall(
	ctx context.Context, client *rds.Client,
	in *rds.RestoreDBInstanceFromDBSnapshotInput, deferMultiAz bool,
) (*rds.RestoreDBInstanceFromDBSnapshotOutput, bool, error) {
	var out *rds.RestoreDBInstanceFromDBSnapshotOutput
	err := retry.OnError(ctx, instanceInstanceProfileRetryable, func(ctx context.Context) error {
		var err error
		out, err = client.RestoreDBInstanceFromDBSnapshot(ctx, in)
		return err
	}, retry.WithTimeout(propagationTimeout), retry.WithInterval(10*time.Second))
	if err == nil {
		return out, deferMultiAz, nil
	}
	if in.MultiAZ != nil && instanceMirroringRejected(err) {
		in.MultiAZ = nil
		deferMultiAz = true
		err = retry.OnError(ctx, instanceInstanceProfileRetryable, func(ctx context.Context) error {
			var err error
			out, err = client.RestoreDBInstanceFromDBSnapshot(ctx, in)
			return err
		}, retry.WithTimeout(propagationTimeout), retry.WithInterval(10*time.Second))
	}
	if err != nil {
		return nil, false, fmt.Errorf("restore db instance from snapshot: %w", err)
	}
	return out, deferMultiAz, nil
}

// restoreToPointInTime issues RestoreDBInstanceToPointInTime. The call takes the
// source and restore point from the block; the password and monitoring fields
// are reconciled by the follow-on modify. It retries through the instance-profile
// role race.
func (r *Instance) restoreToPointInTime(ctx context.Context, client *rds.Client) (string, error) {
	in := &rds.RestoreDBInstanceToPointInTimeInput{
		TargetDBInstanceIdentifier:      aws.String(r.Identifier),
		DBInstanceClass:                 r.InstanceClass,
		Engine:                          lowerString(r.Engine),
		AllocatedStorage:                ptr.Int32(r.AllocatedStorage),
		MaxAllocatedStorage:             ptr.Int32(r.MaxAllocatedStorage),
		Iops:                            ptr.Int32(r.Iops),
		StorageType:                     r.StorageType,
		StorageThroughput:               ptr.Int32(r.StorageThroughput),
		DBName:                          r.DbName,
		DBSubnetGroupName:               r.DbSubnetGroupName,
		DBParameterGroupName:            r.ParameterGroupName,
		OptionGroupName:                 r.OptionGroupName,
		Port:                            ptr.Int32(r.Port),
		AvailabilityZone:                r.AvailabilityZone,
		MultiAZ:                         r.MultiAz,
		PubliclyAccessible:              r.PubliclyAccessible,
		NetworkType:                     r.NetworkType,
		VpcSecurityGroupIds:             r.VpcSecurityGroupIds,
		LicenseModel:                    r.LicenseModel,
		BackupTarget:                    r.BackupTarget,
		CopyTagsToSnapshot:              r.CopyTagsToSnapshot,
		AutoMinorVersionUpgrade:         r.AutoMinorVersionUpgrade,
		DeletionProtection:              r.DeletionProtection,
		CustomIamInstanceProfile:        r.CustomIamInstanceProfile,
		DedicatedLogVolume:              r.DedicatedLogVolume,
		EnableCustomerOwnedIp:           r.CustomerOwnedIpEnabled,
		EnableIAMDatabaseAuthentication: r.IamDatabaseAuthenticationEnabled,
		EngineLifecycleSupport:          r.EngineLifecycleSupport,
		Tags:                            tagList(r.Tags),
	}
	r.setDomainInput(&in.Domain, &in.DomainIAMRoleName, &in.DomainFqdn,
		&in.DomainOu, &in.DomainAuthSecretArn, &in.DomainDnsIps)
	if err := expandRestoreToPointInTime(in, *r.RestoreToPointInTime); err != nil {
		return "", fmt.Errorf("restore-to-point-in-time restore-time: %w", err)
	}
	var out *rds.RestoreDBInstanceToPointInTimeOutput
	err := retry.OnError(ctx, instanceInstanceProfileRetryable, func(ctx context.Context) error {
		var err error
		out, err = client.RestoreDBInstanceToPointInTime(ctx, in)
		return err
	}, retry.WithTimeout(propagationTimeout), retry.WithInterval(10*time.Second))
	if err != nil {
		return "", fmt.Errorf("restore db instance to point in time: %w", err)
	}
	return aws.ToString(out.DBInstance.DbiResourceId), nil
}

// createFollowOnModify builds the modify that reconciles the fields the selected
// restore or replica create could not accept. It returns nil for the plain
// create, whose call accepts everything, except that the plain create still
// reconciles the CA certificate identifier when one is given.
func (r *Instance) createFollowOnModify(deferMultiAz bool) *rds.ModifyDBInstanceInput {
	switch r.createMode() {
	case createModeReplica:
		return nilIfNoFollowOnWork(r.replicaFollowOnModify())
	case createModeSnapshot:
		return nilIfNoFollowOnWork(r.snapshotFollowOnModify(deferMultiAz))
	case createModePointInTime:
		return nilIfNoFollowOnWork(r.pointInTimeFollowOnModify())
	case createModeS3:
		return r.s3FollowOnModify()
	default:
		if r.CaCertIdentifier != nil {
			return &rds.ModifyDBInstanceInput{
				ApplyImmediately:        aws.Bool(true),
				CACertificateIdentifier: r.CaCertIdentifier,
			}
		}
		return nil
	}
}

// nilIfNoFollowOnWork returns nil when the follow-on modify has nothing to
// reconcile, so a restore or replica that omitted every deferred field skips a
// pointless modify and its wait. A modify holding only the apply-immediately
// flag, the form the per-mode builders start from, is the no-work case.
func nilIfNoFollowOnWork(in *rds.ModifyDBInstanceInput) *rds.ModifyDBInstanceInput {
	empty := &rds.ModifyDBInstanceInput{ApplyImmediately: aws.Bool(true)}
	if reflect.DeepEqual(in, empty) {
		return nil
	}
	return in
}

// replicaFollowOnModify reconciles the fields CreateDBInstanceReadReplica does
// not accept: the backup, maintenance, password, parameter-group, and CA fields,
// plus the managed master-user password.
func (r *Instance) replicaFollowOnModify() *rds.ModifyDBInstanceInput {
	in := &rds.ModifyDBInstanceInput{
		ApplyImmediately:           aws.Bool(true),
		BackupRetentionPeriod:      ptr.Int32(r.BackupRetentionPeriod),
		PreferredBackupWindow:      r.BackupWindow,
		PreferredMaintenanceWindow: lowerString(r.MaintenanceWindow),
		MaxAllocatedStorage:        ptr.Int32(r.MaxAllocatedStorage),
		MasterUserPassword:         r.Password,
		DBParameterGroupName:       r.ParameterGroupName,
		CACertificateIdentifier:    r.CaCertIdentifier,
		AllowMajorVersionUpgrade:   r.AllowMajorVersionUpgrade,
		ManageMasterUserPassword:   r.ManageMasterUserPassword,
		MasterUserSecretKmsKeyId:   r.MasterUserSecretKmsKeyId,
	}
	if r.ReplicaMode != nil {
		in.ReplicaMode = rdstypes.ReplicaMode(*r.ReplicaMode)
	}
	return in
}

// snapshotFollowOnModify reconciles the fields a snapshot restore does not
// accept: the storage, backup, maintenance, monitoring, password, and engine
// version fields, plus the managed master-user password. When deferMultiAz is
// set, multi-AZ is applied here rather than on the restore, since the restore
// could not take it for a SQL Server snapshot whose backups are at zero.
func (r *Instance) snapshotFollowOnModify(deferMultiAz bool) *rds.ModifyDBInstanceInput {
	in := &rds.ModifyDBInstanceInput{
		ApplyImmediately:                   aws.Bool(true),
		AllocatedStorage:                   ptr.Int32(r.AllocatedStorage),
		MaxAllocatedStorage:                ptr.Int32(r.MaxAllocatedStorage),
		Iops:                               ptr.Int32(r.Iops),
		StorageType:                        r.StorageType,
		StorageThroughput:                  ptr.Int32(r.StorageThroughput),
		BackupRetentionPeriod:              ptr.Int32(r.BackupRetentionPeriod),
		PreferredBackupWindow:              r.BackupWindow,
		PreferredMaintenanceWindow:         lowerString(r.MaintenanceWindow),
		EngineVersion:                      r.EngineVersion,
		AllowMajorVersionUpgrade:           r.AllowMajorVersionUpgrade,
		MonitoringInterval:                 ptr.Int32(r.MonitoringInterval),
		MonitoringRoleArn:                  r.MonitoringRoleArn,
		MasterUserPassword:                 r.Password,
		EnablePerformanceInsights:          r.EnablePerformanceInsights,
		PerformanceInsightsKMSKeyId:        r.PerformanceInsightsKmsKeyId,
		PerformanceInsightsRetentionPeriod: ptr.Int32(r.PerformanceInsightsRetentionPeriod),
		ManageMasterUserPassword:           r.ManageMasterUserPassword,
		MasterUserSecretKmsKeyId:           r.MasterUserSecretKmsKeyId,
	}
	if r.DatabaseInsightsMode != nil {
		in.DatabaseInsightsMode = rdstypes.DatabaseInsightsMode(*r.DatabaseInsightsMode)
	}
	// Apply multi-AZ here when the restore could not take it: SQL Server, or a
	// restore that hit the mirroring error and retried without it.
	if deferMultiAz {
		in.MultiAZ = r.MultiAz
	}
	return in
}

// pointInTimeFollowOnModify reconciles the fields a point-in-time restore does
// not accept: the monitoring and password fields and the managed master-user
// password.
func (r *Instance) pointInTimeFollowOnModify() *rds.ModifyDBInstanceInput {
	in := &rds.ModifyDBInstanceInput{
		ApplyImmediately:         aws.Bool(true),
		MonitoringInterval:       ptr.Int32(r.MonitoringInterval),
		MonitoringRoleArn:        r.MonitoringRoleArn,
		MasterUserPassword:       r.Password,
		ManageMasterUserPassword: r.ManageMasterUserPassword,
		MasterUserSecretKmsKeyId: r.MasterUserSecretKmsKeyId,
	}
	if r.DatabaseInsightsMode != nil {
		in.DatabaseInsightsMode = rdstypes.DatabaseInsightsMode(*r.DatabaseInsightsMode)
	}
	return in
}

// s3FollowOnModify reconciles the CA certificate identifier after a restore from
// S3, the one field that restore leaves to a follow-on modify.
func (r *Instance) s3FollowOnModify() *rds.ModifyDBInstanceInput {
	if r.CaCertIdentifier == nil {
		return nil
	}
	return &rds.ModifyDBInstanceInput{
		ApplyImmediately:        aws.Bool(true),
		CACertificateIdentifier: r.CaCertIdentifier,
	}
}

// updateModify builds the in-place modify for an update, sending only the fields
// whose inputs changed. It returns nil when nothing RDS reconciles through
// ModifyDBInstance changed. A removed scalar is simply not sent, so RDS keeps
// its value; the CloudWatch log exports are reconciled as an enable/disable
// diff, and a rename rides NewDBInstanceIdentifier. The storage co-send rules
// the API requires are applied after the per-field diff.
func (r *Instance) updateModify(
	prior runtime.Prior[Instance, *InstanceOutput],
) *rds.ModifyDBInstanceInput {
	p := prior.Inputs
	in := &rds.ModifyDBInstanceInput{ApplyImmediately: aws.Bool(true)}
	changed := false
	set := func(c bool, apply func()) {
		if c {
			apply()
			changed = true
		}
	}
	set(runtime.Changed(p.Identifier, r.Identifier), func() {
		in.NewDBInstanceIdentifier = aws.String(r.Identifier)
	})
	set(runtime.Changed(p.InstanceClass, r.InstanceClass), func() {
		in.DBInstanceClass = r.InstanceClass
	})
	set(runtime.Changed(p.AllocatedStorage, r.AllocatedStorage), func() {
		in.AllocatedStorage = ptr.Int32(r.AllocatedStorage)
	})
	set(runtime.Changed(p.MaxAllocatedStorage, r.MaxAllocatedStorage), func() {
		in.MaxAllocatedStorage = r.maxAllocatedStorageValue()
	})
	set(runtime.Changed(p.Iops, r.Iops), func() { in.Iops = ptr.Int32(r.Iops) })
	set(runtime.Changed(p.StorageType, r.StorageType), func() { in.StorageType = r.StorageType })
	set(runtime.Changed(p.StorageThroughput, r.StorageThroughput), func() {
		in.StorageThroughput = ptr.Int32(r.StorageThroughput)
	})
	set(runtime.Changed(p.DbSubnetGroupName, r.DbSubnetGroupName), func() {
		in.DBSubnetGroupName = r.DbSubnetGroupName
	})
	set(runtime.Changed(p.ParameterGroupName, r.ParameterGroupName), func() {
		in.DBParameterGroupName = r.ParameterGroupName
	})
	set(runtime.Changed(p.OptionGroupName, r.OptionGroupName), func() {
		in.OptionGroupName = r.OptionGroupName
	})
	set(runtime.Changed(p.Port, r.Port), func() { in.DBPortNumber = ptr.Int32(r.Port) })
	set(runtime.Changed(p.MultiAz, r.MultiAz), func() { in.MultiAZ = r.MultiAz })
	set(runtime.Changed(p.PubliclyAccessible, r.PubliclyAccessible), func() {
		in.PubliclyAccessible = r.PubliclyAccessible
	})
	set(runtime.Changed(p.NetworkType, r.NetworkType), func() { in.NetworkType = r.NetworkType })
	set(runtime.Changed(p.VpcSecurityGroupIds, r.VpcSecurityGroupIds), func() {
		in.VpcSecurityGroupIds = r.VpcSecurityGroupIds
	})
	set(runtime.Changed(p.LicenseModel, r.LicenseModel), func() { in.LicenseModel = r.LicenseModel })
	set(runtime.Changed(p.BackupRetentionPeriod, r.BackupRetentionPeriod), func() {
		in.BackupRetentionPeriod = ptr.Int32(r.BackupRetentionPeriod)
	})
	set(runtime.Changed(p.BackupWindow, r.BackupWindow), func() {
		in.PreferredBackupWindow = r.BackupWindow
	})
	set(runtime.Changed(p.CopyTagsToSnapshot, r.CopyTagsToSnapshot), func() {
		in.CopyTagsToSnapshot = r.CopyTagsToSnapshot
	})
	set(runtime.Changed(p.MaintenanceWindow, r.MaintenanceWindow), func() {
		in.PreferredMaintenanceWindow = lowerString(r.MaintenanceWindow)
	})
	set(runtime.Changed(p.AutoMinorVersionUpgrade, r.AutoMinorVersionUpgrade), func() {
		in.AutoMinorVersionUpgrade = r.AutoMinorVersionUpgrade
	})
	set(runtime.Changed(p.DeletionProtection, r.DeletionProtection), func() {
		in.DeletionProtection = r.DeletionProtection
	})
	set(runtime.Changed(p.CaCertIdentifier, r.CaCertIdentifier), func() {
		in.CACertificateIdentifier = r.CaCertIdentifier
	})
	set(runtime.Changed(p.CustomerOwnedIpEnabled, r.CustomerOwnedIpEnabled), func() {
		in.EnableCustomerOwnedIp = r.CustomerOwnedIpEnabled
	})
	set(runtime.Changed(p.DedicatedLogVolume, r.DedicatedLogVolume), func() {
		in.DedicatedLogVolume = r.DedicatedLogVolume
	})
	set(runtime.Changed(p.IamDatabaseAuthenticationEnabled, r.IamDatabaseAuthenticationEnabled),
		func() { in.EnableIAMDatabaseAuthentication = r.IamDatabaseAuthenticationEnabled })
	set(runtime.Changed(p.MonitoringInterval, r.MonitoringInterval), func() {
		in.MonitoringInterval = ptr.Int32(r.MonitoringInterval)
	})
	set(runtime.Changed(p.MonitoringRoleArn, r.MonitoringRoleArn), func() {
		in.MonitoringRoleArn = r.MonitoringRoleArn
	})
	set(runtime.Changed(p.EnablePerformanceInsights, r.EnablePerformanceInsights), func() {
		in.EnablePerformanceInsights = r.EnablePerformanceInsights
	})
	set(runtime.Changed(p.PerformanceInsightsKmsKeyId, r.PerformanceInsightsKmsKeyId), func() {
		in.PerformanceInsightsKMSKeyId = r.PerformanceInsightsKmsKeyId
	})
	set(runtime.Changed(p.PerformanceInsightsRetentionPeriod, r.PerformanceInsightsRetentionPeriod),
		func() {
			in.PerformanceInsightsRetentionPeriod = ptr.Int32(r.PerformanceInsightsRetentionPeriod)
		})
	set(runtime.Changed(p.ManageMasterUserPassword, r.ManageMasterUserPassword), func() {
		in.ManageMasterUserPassword = r.ManageMasterUserPassword
	})
	set(runtime.Changed(p.MasterUserSecretKmsKeyId, r.MasterUserSecretKmsKeyId), func() {
		in.MasterUserSecretKmsKeyId = r.MasterUserSecretKmsKeyId
	})
	set(runtime.Changed(p.Password, r.Password), func() { in.MasterUserPassword = r.Password })
	if r.databaseInsightsModeChanged(p) {
		in.DatabaseInsightsMode = rdstypes.DatabaseInsightsMode(aws.ToString(r.DatabaseInsightsMode))
		// The mode travels with the performance-insights settings, since RDS
		// rejects an insights-mode change that arrives alone.
		in.EnablePerformanceInsights = r.EnablePerformanceInsights
		in.PerformanceInsightsKMSKeyId = r.PerformanceInsightsKmsKeyId
		in.PerformanceInsightsRetentionPeriod = ptr.Int32(r.PerformanceInsightsRetentionPeriod)
		changed = true
	}
	if r.replicaModeChanged(p) {
		in.ReplicaMode = rdstypes.ReplicaMode(aws.ToString(r.ReplicaMode))
		changed = true
	}
	if r.engineVersionChanged(p) {
		in.EngineVersion = r.EngineVersion
		in.AllowMajorVersionUpgrade = r.AllowMajorVersionUpgrade
		changed = true
	}
	if runtime.Changed(p.EnabledCloudwatchLogsExports, r.EnabledCloudwatchLogsExports) {
		in.CloudwatchLogsExportConfiguration = r.logsExportConfig(p.EnabledCloudwatchLogsExports)
		changed = true
	}
	if r.domainChanged(p) {
		r.setDomainModify(in)
		changed = true
	}
	if !changed {
		return nil
	}
	r.applyStorageCoSends(in, prior)
	return in
}

// databaseInsightsModeChanged reports whether the database-insights mode input
// changed for an update.
func (r *Instance) databaseInsightsModeChanged(p Instance) bool {
	return runtime.Changed(p.DatabaseInsightsMode, r.DatabaseInsightsMode) &&
		r.DatabaseInsightsMode != nil
}

// replicaModeChanged reports whether the replica-mode input changed for an
// update.
func (r *Instance) replicaModeChanged(p Instance) bool {
	return runtime.Changed(p.ReplicaMode, r.ReplicaMode) && r.ReplicaMode != nil
}

// engineVersionChanged reports whether the engine version input changed for an
// update; a change sends the allow-major-version-upgrade flag alongside it.
func (r *Instance) engineVersionChanged(p Instance) bool {
	return runtime.Changed(p.EngineVersion, r.EngineVersion) && r.EngineVersion != nil
}

// domainChanged reports whether any Active Directory field changed for an
// update.
func (r *Instance) domainChanged(p Instance) bool {
	return runtime.Changed(p.Domain, r.Domain) ||
		runtime.Changed(p.DomainIamRoleName, r.DomainIamRoleName) ||
		runtime.Changed(p.DomainFqdn, r.DomainFqdn) ||
		runtime.Changed(p.DomainOu, r.DomainOu) ||
		runtime.Changed(p.DomainAuthSecretArn, r.DomainAuthSecretArn) ||
		runtime.Changed(p.DomainDnsIps, r.DomainDnsIps)
}

// logsExportConfig builds the CloudWatch logs export configuration for an
// update: the log types newly listed are enabled, the ones no longer listed are
// disabled.
func (r *Instance) logsExportConfig(prior []string) *rdstypes.CloudwatchLogsExportConfiguration {
	enable, disable := stringSetDiff(prior, r.EnabledCloudwatchLogsExports)
	return &rdstypes.CloudwatchLogsExportConfiguration{
		EnableLogTypes:  enable,
		DisableLogTypes: disable,
	}
}

// maxAllocatedStorageValue gives the value to send for the storage-autoscaling
// upper limit. Disabling autoscaling means setting the limit equal to the
// allocated storage, since RDS reads a limit equal to the current storage as
// off; a zero or unset limit with a known allocated storage uses it.
func (r *Instance) maxAllocatedStorageValue() *int32 {
	if r.MaxAllocatedStorage != nil && *r.MaxAllocatedStorage > 0 {
		return ptr.Int32(r.MaxAllocatedStorage)
	}
	if r.AllocatedStorage != nil {
		return ptr.Int32(r.AllocatedStorage)
	}
	return ptr.Int32(r.MaxAllocatedStorage)
}

// applyStorageCoSends adds the storage fields RDS requires to accompany a
// storage change. On io1 or io2 storage, and on gp3 at or above the
// engine-specific threshold where gp3 takes an IOPS value, a change to the
// storage type, the allocated storage, or the IOPS must send the allocated
// storage and IOPS as a pair. A throughput change co-sends them too. The
// co-sent values come from the inputs.
func (r *Instance) applyStorageCoSends(
	in *rds.ModifyDBInstanceInput, prior runtime.Prior[Instance, *InstanceOutput],
) {
	storageFieldChange := runtime.Changed(prior.Inputs.StorageType, r.StorageType) ||
		runtime.Changed(prior.Inputs.AllocatedStorage, r.AllocatedStorage) ||
		runtime.Changed(prior.Inputs.Iops, r.Iops)
	throughputChange := runtime.Changed(prior.Inputs.StorageThroughput, r.StorageThroughput)
	st := strings.ToLower(aws.ToString(r.StorageType))
	coSendIops := false
	switch {
	case storageFieldChange && (st == "io1" || st == "io2"):
		coSendIops = true
	case storageFieldChange && st == "gp3" && !r.gp3BelowThreshold():
		coSendIops = true
	case throughputChange:
		coSendIops = true
	}
	if !coSendIops {
		return
	}
	if in.Iops == nil {
		in.Iops = ptr.Int32(r.Iops)
	}
	if in.AllocatedStorage == nil {
		in.AllocatedStorage = ptr.Int32(r.AllocatedStorage)
	}
}

// gp3BelowThreshold reports whether the allocated storage is below the
// engine-specific threshold under which gp3 storage does not take an IOPS value.
// Below the threshold RDS rejects a co-sent IOPS, so the co-send is skipped.
func (r *Instance) gp3BelowThreshold() bool {
	if r.AllocatedStorage == nil {
		return false
	}
	storage := *r.AllocatedStorage
	engine := strings.ToLower(aws.ToString(r.Engine))
	switch {
	case strings.HasPrefix(engine, "db2"):
		return storage < 100
	case strings.HasPrefix(engine, "mariadb"),
		strings.HasPrefix(engine, "mysql"),
		strings.HasPrefix(engine, "postgres"):
		return storage < 400
	case strings.HasPrefix(engine, "oracle"):
		return storage < 200
	default:
		return false
	}
}

// currentIdentifier gives the instance identifier to address in an update modify.
// A rename changes the identifier through NewDBInstanceIdentifier, but the call
// must still address the instance by its prior identifier, so the prior input's
// identifier is used when it differs.
func (r *Instance) currentIdentifier(prior runtime.Prior[Instance, *InstanceOutput]) string {
	if prior.Inputs.Identifier != "" {
		return prior.Inputs.Identifier
	}
	return r.Identifier
}

// modifyAndWait issues a ModifyDBInstance and waits the instance back to
// available. It retries through the IAM, storage-optimization, and cluster-state
// races RDS reports while a related change settles.
func (r *Instance) modifyAndWait(
	ctx context.Context, client *rds.Client, in *rds.ModifyDBInstanceInput, timeout time.Duration,
) error {
	err := retry.OnError(ctx, instanceModifyRetryable, func(ctx context.Context) error {
		_, err := client.ModifyDBInstance(ctx, in)
		return err
	}, retry.WithTimeout(propagationTimeout), retry.WithInterval(10*time.Second))
	if err != nil {
		return fmt.Errorf("modify db instance: %w", err)
	}
	return r.waitAvailable(ctx, client, timeout)
}

// rebootAndWait reboots the instance and waits it back to available. A read
// replica reboots after create to load a parameter group that differs from the
// source's.
func (r *Instance) rebootAndWait(
	ctx context.Context, client *rds.Client, timeout time.Duration,
) error {
	_, err := client.RebootDBInstance(ctx, &rds.RebootDBInstanceInput{
		DBInstanceIdentifier: aws.String(r.Identifier),
	})
	if err != nil {
		return fmt.Errorf("reboot db instance: %w", err)
	}
	return r.waitAvailable(ctx, client, timeout)
}

// promoteAndWait promotes a read replica to a standalone instance and waits it
// back to available. This is how removing the replication source is applied.
func (r *Instance) promoteAndWait(ctx context.Context, client *rds.Client) error {
	_, err := client.PromoteReadReplica(ctx, &rds.PromoteReadReplicaInput{
		DBInstanceIdentifier: aws.String(r.Identifier),
	})
	if err != nil {
		return fmt.Errorf("promote read replica: %w", err)
	}
	return r.waitAvailable(ctx, client, instanceUpdateTimeout)
}

// clearDeletionProtection turns off deletion protection so a blocked delete can
// proceed. It retries through the IAM-role and instance-state races RDS reports
// while a modify settles, then waits the instance available.
func (r *Instance) clearDeletionProtection(ctx context.Context, client *rds.Client) error {
	in := &rds.ModifyDBInstanceInput{
		DBInstanceIdentifier: aws.String(r.Identifier),
		DeletionProtection:   aws.Bool(false),
		ApplyImmediately:     aws.Bool(true),
	}
	err := retry.OnError(ctx, instanceDeletionProtectionRetryable, func(ctx context.Context) error {
		_, err := client.ModifyDBInstance(ctx, in)
		return err
	}, retry.WithTimeout(propagationTimeout), retry.WithInterval(10*time.Second))
	if err != nil {
		return fmt.Errorf("disable deletion protection: %w", err)
	}
	return r.waitAvailable(ctx, client, instanceUpdateTimeout)
}

// deleteInput builds the DeleteDBInstance request. A final snapshot is taken
// unless skip-final-snapshot is set, in which case the final snapshot identifier
// is required. The automated backups are removed by default.
func (r *Instance) deleteInput() (*rds.DeleteDBInstanceInput, error) {
	in := &rds.DeleteDBInstanceInput{
		DBInstanceIdentifier:   aws.String(r.Identifier),
		DeleteAutomatedBackups: aws.Bool(aws.ToBool(orTrue(r.DeleteAutomatedBackups))),
	}
	if aws.ToBool(r.SkipFinalSnapshot) {
		in.SkipFinalSnapshot = aws.Bool(true)
		return in, nil
	}
	if r.FinalSnapshotIdentifier == nil {
		return nil, errors.New(
			"final-snapshot-identifier is required when skip-final-snapshot is false")
	}
	in.SkipFinalSnapshot = aws.Bool(false)
	in.FinalDBSnapshotIdentifier = r.FinalSnapshotIdentifier
	return in, nil
}

// setDomainInput fills the Active Directory create fields from the inputs. The
// AWS-managed mode uses the domain id and role name; the self-managed mode uses
// the FQDN, organizational unit, auth secret, and DNS IPs.
func (r *Instance) setDomainInput(
	domain, roleName, fqdn, ou, authSecret **string, dnsIps *[]string,
) {
	*domain = r.Domain
	*roleName = r.DomainIamRoleName
	*fqdn = r.DomainFqdn
	*ou = r.DomainOu
	*authSecret = r.DomainAuthSecretArn
	*dnsIps = r.DomainDnsIps
}

// setDomainModify fills the Active Directory fields on an update modify. When no
// domain is configured, the modify clears any joined domain through the
// disable-domain flag, since a null does not leave a domain on its own.
func (r *Instance) setDomainModify(in *rds.ModifyDBInstanceInput) {
	if r.Domain == nil && r.DomainFqdn == nil {
		in.DisableDomain = aws.Bool(true)
		return
	}
	in.Domain = r.Domain
	in.DomainIAMRoleName = r.DomainIamRoleName
	in.DomainFqdn = r.DomainFqdn
	in.DomainOu = r.DomainOu
	in.DomainAuthSecretArn = r.DomainAuthSecretArn
	in.DomainDnsIps = r.DomainDnsIps
}

// read describes the instance by its resource id and maps it to outputs,
// returning runtime.ErrNotFound when it is gone. The endpoint, secret, and
// resolved engine version come from this post-wait describe, since the create
// and modify responses do not have them in final form.
func (r *Instance) read(
	ctx context.Context, client *rds.Client, resourceID string,
) (*InstanceOutput, error) {
	inst, err := findInstanceByResourceID(ctx, client, resourceID)
	if err != nil {
		return nil, err
	}
	endpoint, address, hostedZone, port := flattenEndpoint(inst.Endpoint)
	out := &InstanceOutput{
		Arn:                 aws.ToString(inst.DBInstanceArn),
		ResourceId:          aws.ToString(inst.DbiResourceId),
		Endpoint:            endpoint,
		Address:             address,
		Port:                port,
		HostedZoneId:        hostedZone,
		Status:              aws.ToString(inst.DBInstanceStatus),
		EngineVersionActual: aws.ToString(inst.EngineVersion),
		CaCertIdentifier:    aws.ToString(inst.CACertificateIdentifier),
		MasterUserSecret:    flattenMasterUserSecret(inst.MasterUserSecret),
		ListenerEndpoint:    flattenListenerEndpoint(inst.ListenerEndpoint),
		Replicas:            inst.ReadReplicaDBInstanceIdentifiers,
	}
	if inst.LatestRestorableTime != nil {
		out.LatestRestorableTime = inst.LatestRestorableTime.Format(time.RFC3339)
	}
	return out, nil
}

// waitAvailable polls until the instance reaches a terminal-success status,
// requiring three consecutive ready reads so a single read against a caught-up
// replica does not end the wait while the status is still flapping. A status
// outside the known pending and success sets fails the wait, since the instance
// has reached a state it will not leave on its own. A not-found right after a
// create is the describe not yet seeing the new instance, so the wait keeps
// polling rather than failing.
func (r *Instance) waitAvailable(
	ctx context.Context, client *rds.Client, timeout time.Duration,
) error {
	what := fmt.Sprintf("db instance %s to be available", r.Identifier)
	return wait.UntilStable(ctx, what, 3, func(ctx context.Context) (bool, error) {
		inst, err := findInstanceByIdentifier(ctx, client, r.Identifier)
		if err == runtime.ErrNotFound {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		status := aws.ToString(inst.DBInstanceStatus)
		if slices.Contains(instanceAvailableStatuses, status) {
			return true, nil
		}
		if !slices.Contains(instancePendingStatuses, status) {
			return false, fmt.Errorf(
				"db instance %s entered unexpected status %q", r.Identifier, status)
		}
		return false, nil
	}, wait.WithTimeout(timeout), wait.WithInterval(10*time.Second))
}

// waitDeleted polls until the instance no longer describes, requiring three
// consecutive gone reads so a lagging replica does not report it gone early. The
// describe keeps returning the deleting instance for a while after the delete
// call accepts, so the delete is not complete until the describe goes empty.
func (r *Instance) waitDeleted(ctx context.Context, client *rds.Client) error {
	what := fmt.Sprintf("db instance %s to be deleted", r.Identifier)
	return wait.UntilStable(ctx, what, 3, func(ctx context.Context) (bool, error) {
		_, err := findInstanceByIdentifier(ctx, client, r.Identifier)
		if err == runtime.ErrNotFound {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	}, wait.WithTimeout(instanceDeleteTimeout), wait.WithInterval(10*time.Second))
}

// findInstanceByResourceID describes the instance by its stable resource id, the
// handle that survives a rename. An empty result or a not-found fault maps to
// runtime.ErrNotFound.
func findInstanceByResourceID(
	ctx context.Context, client *rds.Client, resourceID string,
) (*rdstypes.DBInstance, error) {
	return findInstance(ctx, client, &rds.DescribeDBInstancesInput{
		Filters: []rdstypes.Filter{{
			Name:   aws.String("dbi-resource-id"),
			Values: []string{resourceID},
		}},
	})
}

// findInstanceByIdentifier describes the instance by its user identifier, used
// by the waits, which act on the instance the create or modify named. An empty
// result or a not-found fault maps to runtime.ErrNotFound.
func findInstanceByIdentifier(
	ctx context.Context, client *rds.Client, identifier string,
) (*rdstypes.DBInstance, error) {
	return findInstance(ctx, client, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(identifier),
	})
}

// findInstance runs a describe and returns the single instance it finds. RDS
// signals a missing instance with a DBInstanceNotFoundFault rather than an error
// code or an HTTP status; that fault and an empty result both map to
// runtime.ErrNotFound so a plan recreates a deleted instance.
func findInstance(
	ctx context.Context, client *rds.Client, in *rds.DescribeDBInstancesInput,
) (*rdstypes.DBInstance, error) {
	resp, err := client.DescribeDBInstances(ctx, in)
	if err != nil {
		var notFound *rdstypes.DBInstanceNotFoundFault
		if errors.As(err, &notFound) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe db instances: %w", err)
	}
	if len(resp.DBInstances) == 0 {
		return nil, runtime.ErrNotFound
	}
	return &resp.DBInstances[0], nil
}

// snapshotKeepsDbName reports whether a snapshot restore keeps the database
// name. The MySQL-family engines reject a database name on a snapshot restore,
// so the name is skipped for them and kept for the rest.
func snapshotKeepsDbName(engine *string) bool {
	switch strings.ToLower(aws.ToString(engine)) {
	case "mysql", "mariadb", "postgres":
		return false
	default:
		return true
	}
}

// engineIsSqlServer reports whether the engine is a SQL Server engine, whose
// snapshot restore defers multi-AZ to the follow-on modify.
func engineIsSqlServer(engine *string) bool {
	return strings.HasPrefix(strings.ToLower(aws.ToString(engine)), "sqlserver")
}

// validatePerformanceInsightsRetention checks the performance-insights retention
// period, which RDS accepts only as 7, 731, or a multiple of 31. The constraint
// layer cannot express the divisibility, so it is checked here.
func validatePerformanceInsightsRetention(days *int64) error {
	if days == nil {
		return nil
	}
	d := *days
	if d == 7 || d == 731 || (d > 0 && d%31 == 0) {
		return nil
	}
	return fmt.Errorf(
		"performance-insights-retention-period must be 7, 731, or a multiple of 31, got %d", d)
}

// lowerString lowercases a string input, preserving nil. RDS lowercases the
// engine and maintenance window itself, so sending them lowercased avoids a
// phantom difference between the request and the stored value.
func lowerString(s *string) *string {
	if s == nil {
		return nil
	}
	lowered := strings.ToLower(*s)
	return &lowered
}

// orTrue returns the bool input, defaulting to true when unset. The
// delete-automated-backups flag defaults to true, matching RDS.
func orTrue(b *bool) *bool {
	if b == nil {
		return aws.Bool(true)
	}
	return b
}

// itoa formats an int64 as a base-ten string, used to join an endpoint address
// and port.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// stringSetDiff returns the elements added and removed between a prior set and a
// desired set, treating each as a set of strings.
func stringSetDiff(prior, desired []string) (added, removed []string) {
	priorSet := make(map[string]struct{}, len(prior))
	for _, s := range prior {
		priorSet[s] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desired))
	for _, s := range desired {
		desiredSet[s] = struct{}{}
	}
	for _, s := range desired {
		if _, ok := priorSet[s]; !ok {
			added = append(added, s)
		}
	}
	for _, s := range prior {
		if _, ok := desiredSet[s]; !ok {
			removed = append(removed, s)
		}
	}
	return added, removed
}

// instanceCreateRetryable reports whether a plain create error is a transient
// propagation race: an enhanced-monitoring role or an instance-profile role
// created moments earlier and not yet usable. Both clear on their own.
func instanceCreateRetryable(err error) bool {
	return instanceMessageContains(err, "InvalidParameterValue", "ENHANCED_MONITORING") ||
		instanceMessageContains(err, "ValidationError",
			"RDS couldn't fetch the role from instance profile")
}

// instanceEnhancedMonitoringRetryable reports whether an error is the
// enhanced-monitoring propagation race a read-replica create retries through.
func instanceEnhancedMonitoringRetryable(err error) bool {
	return instanceMessageContains(err, "InvalidParameterValue", "ENHANCED_MONITORING")
}

// instanceS3RestoreRetryable reports whether a restore-from-S3 error is a
// transient propagation race: the enhanced-monitoring or instance-profile role
// races, or an S3 ingestion race while the bucket access settles.
func instanceS3RestoreRetryable(err error) bool {
	if instanceCreateRetryable(err) {
		return true
	}
	for _, m := range []string{
		"S3_SNAPSHOT_INGESTION",
		"S3 bucket cannot be found",
		"Files from the specified Amazon S3 bucket cannot be downloaded",
	} {
		if instanceMessageContains(err, "InvalidParameterValue", m) {
			return true
		}
	}
	return false
}

// instanceInstanceProfileRetryable reports whether a snapshot or point-in-time
// restore error is the instance-profile role propagation race. It clears once
// the role is fetchable.
func instanceInstanceProfileRetryable(err error) bool {
	return instanceMessageContains(err, "ValidationError",
		"RDS couldn't fetch the role from instance profile")
}

// instanceModifyRetryable reports whether a ModifyDBInstance error is a
// transient race: an IAM role not yet granted its permissions, a previous
// storage change still optimizing, or a cluster still in a transient state. Each
// clears on its own.
func instanceModifyRetryable(err error) bool {
	if instanceMessageContains(err, "InvalidParameterValue",
		"IAM role ARN value is invalid or does not include the required permissions") {
		return true
	}
	if instanceMessageContains(err, "InvalidParameterCombination",
		"previous storage change is being optimized") {
		return true
	}
	var clusterState *rdstypes.InvalidDBClusterStateFault
	return errors.As(err, &clusterState)
}

// instanceReplicaParamGroupRejected reports whether a read-replica create error
// is RDS rejecting a parameter group for the engine. The create clears the
// group and retries, deferring it to the follow-on modify.
func instanceReplicaParamGroupRejected(err error) bool {
	return instanceMessageContains(err, "InvalidParameterCombination",
		"A parameter group can't be specified during Read Replica creation for the following DB engine")
}

// instanceMirroringRejected reports whether a snapshot restore error is RDS
// rejecting multi-AZ mirroring because the backup retention is at zero, which
// happens when the snapshot is SQL Server. The restore clears multi-AZ and
// retries, deferring it to the follow-on modify.
func instanceMirroringRejected(err error) bool {
	return instanceMessageContains(err, "InvalidParameterValue",
		"Mirroring cannot be applied to instances with backup retention set to zero")
}

// instanceDeletionProtectionRetryable reports whether the modify that disables
// deletion protection hit a transient race: an IAM role not yet granted its
// permissions, or the instance in a state that asks to retry the request later.
// Both clear on their own, so the modify retries.
func instanceDeletionProtectionRetryable(err error) bool {
	if instanceMessageContains(err, "InvalidParameterValue",
		"IAM role ARN value is invalid or") {
		return true
	}
	var state *rdstypes.InvalidDBInstanceStateFault
	return errors.As(err, &state) && strings.Contains(err.Error(), "your request later")
}

// instanceGoneOnDelete reports whether a delete error means the instance is
// already gone or already on its way out: a not-found fault, or an
// invalid-state fault saying it is already being deleted. Either is treated as a
// successful delete that proceeds to the deleted-wait.
func instanceGoneOnDelete(err error) bool {
	var notFound *rdstypes.DBInstanceNotFoundFault
	if errors.As(err, &notFound) {
		return true
	}
	var state *rdstypes.InvalidDBInstanceStateFault
	return errors.As(err, &state) && strings.Contains(err.Error(), "is already being deleted")
}

// instanceBlockedByDeletionProtection reports whether a delete error is RDS
// refusing because deletion protection is on. The delete clears protection and
// retries.
func instanceBlockedByDeletionProtection(err error) bool {
	return instanceMessageContains(err, "InvalidParameterCombination", "disable deletion pro")
}

// instanceMessageContains reports whether err is an AWS API error whose code
// equals code and whose message contains substr. The spec's retry triggers are
// code-plus-message pairs, so both must match.
func instanceMessageContains(err error, code, substr string) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode() == code && strings.Contains(apiErr.ErrorMessage(), substr)
}
