package rds

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
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

// clusterInstanceTimeout bounds the create, update, and delete availability
// waits. A cluster member can take many minutes to join, modify, or leave a
// cluster, so the wait runs up to ninety minutes before giving up.
const clusterInstanceTimeout = 90 * time.Minute

// clusterInstanceNotFoundChecks is how many consecutive not-found reads the
// availability wait tolerates before it fails. A describe right after a create
// can briefly miss the new member; twenty reads matches the tolerance the
// Terraform provider gives the same refresh.
const clusterInstanceNotFoundChecks = 20

// clusterInstancePropagationTimeout bounds the create and modify retries that
// wait for a just-created enhanced-monitoring role to become assumable by RDS.
const clusterInstancePropagationTimeout = 2 * time.Minute

// clusterInstanceEngineCustomPrefix marks the RDS Custom engines. A member on a
// Custom engine cannot take a final snapshot, so its delete skips one.
const clusterInstanceEngineCustomPrefix = "custom-"

// clusterInstanceIAMPropagationMessage is the message RDS returns from
// CreateDBInstance and ModifyDBInstance while the enhanced-monitoring role it
// was handed has not propagated yet. RDS gives no distinct code for this, so
// the match is on an InvalidParameterValue whose message contains this text.
const clusterInstanceIAMPropagationMessage = "IAM role ARN value is invalid or " +
	"does not include the required permissions"

// clusterInstanceReplicaClusterMessage is the message RDS returns when a member
// cannot be deleted because the replica cluster it belongs to must go first.
// This clears once the cluster is promoted or removed, so the delete retries
// through it.
const clusterInstanceReplicaClusterMessage = "Delete the replica cluster before deleting"

// clusterInstanceLastMemberMessage is the message RDS returns when a member is
// the last instance of a read-replica cluster. With force-destroy set, the
// member is freed by promoting the cluster out of read-replica mode.
const clusterInstanceLastMemberMessage = "Cannot delete the last instance of the " +
	"read replica DB cluster"

// clusterInstanceAlreadyDeletingMessage is the message RDS returns when a member
// is already on its way out, which counts as a successful delete.
const clusterInstanceAlreadyDeletingMessage = "is already being deleted"

// clusterInstanceEngineValues are the non-Custom engines a cluster member
// accepts. A Custom engine matches the custom- prefix instead, checked
// separately, since a prefix cannot be a constraint enum.
var clusterInstanceEngineValues = []string{
	"aurora-mysql",
	"aurora-postgresql",
	"mysql",
	"postgres",
}

// clusterInstanceIdentifierPattern is the charset RDS requires of a DB instance
// identifier: lowercase letters, digits, and hyphens, starting with a letter,
// with no doubled or trailing hyphen. RDS enforces this server-side as well; it
// is checked here so a malformed name fails before the call.
var clusterInstanceIdentifierPattern = regexp.MustCompile(
	`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// clusterInstanceCustomProfilePattern is the AWSRDSCustom prefix RDS requires of
// a custom IAM instance profile name.
var clusterInstanceCustomProfilePattern = regexp.MustCompile(`^AWSRDSCustom.*$`)

// clusterInstanceBackupWindowPattern is the once-a-day UTC window format
// hh24:mi-hh24:mi RDS requires of a preferred backup window.
var clusterInstanceBackupWindowPattern = regexp.MustCompile(
	`^([01]\d|2[0-3]):[0-5]\d-([01]\d|2[0-3]):[0-5]\d$`)

// clusterInstanceMaintenanceWindowPattern is the once-a-week window format
// ddd:hh24:mi-ddd:hh24:mi RDS requires of a preferred maintenance window.
var clusterInstanceMaintenanceWindowPattern = regexp.MustCompile(
	`^(mon|tue|wed|thu|fri|sat|sun):([01]\d|2[0-3]):[0-5]\d` +
		`-(mon|tue|wed|thu|fri|sat|sun):([01]\d|2[0-3]):[0-5]\d$`)

// clusterInstanceAvailablePending are the member states the create and update
// availability waits treat as still settling.
var clusterInstanceAvailablePending = map[string]bool{
	"backing-up":                      true,
	"configuring-enhanced-monitoring": true,
	"configuring-iam-database-auth":   true,
	"configuring-log-exports":         true,
	"creating":                        true,
	"maintenance":                     true,
	"modifying":                       true,
	"rebooting":                       true,
	"renaming":                        true,
	"resetting-master-credentials":    true,
	"starting":                        true,
	"upgrading":                       true,
}

// clusterInstanceAvailableTarget are the member states the availability waits
// treat as settled.
var clusterInstanceAvailableTarget = map[string]bool{
	"available":            true,
	"storage-optimization": true,
}

// clusterInstanceDeletedPending are the member states the deletion wait treats
// as still on the way out. Its target set is empty: the member is gone.
var clusterInstanceDeletedPending = map[string]bool{
	"configuring-log-exports": true,
	"delete-precheck":         true,
	"deleting":                true,
	"modifying":               true,
}

// ClusterInstanceResource is one member of an Aurora or Multi-AZ DB cluster. The cluster
// owns the storage, master credentials, encryption, and backup retention, so a
// member declares only its compute class, placement, monitoring, and
// Performance Insights settings, plus the cluster it joins. The cluster
// identifier, identifier, engine, Availability Zone, subnet group, and custom
// instance profile are fixed at create time, so a change to any of them replaces
// the member; every other field is reconciled in place by a single
// ModifyDBInstance.
//
// The CA certificate identifier is not a CreateDBInstance setting: on create it
// is reconciled, only when it differs from the value the create returned, by a
// ModifyDBInstance followed by a reboot, since the certificate takes effect only
// after a reboot. Force-destroy is a delete-time behavior, not a setting sent to
// any API: it permits freeing the last member of a read-replica cluster by
// promoting the cluster.
//
// A member of a cluster never owns a snapshot, so there are no final-snapshot,
// storage, credential, or KMS inputs here; the cluster owns all of those.
type ClusterInstanceResource struct {
	ClusterIdentifier                  string             `ub:"cluster-identifier"`
	InstanceClass                      string             `ub:"instance-class"`
	Engine                             string             `ub:"engine"`
	Identifier                         string             `ub:"identifier"`
	AvailabilityZone                   *string            `ub:"availability-zone"`
	DBParameterGroupName               *string            `ub:"db-parameter-group-name"`
	DBSubnetGroupName                  *string            `ub:"db-subnet-group-name"`
	CustomIamInstanceProfile           *string            `ub:"custom-iam-instance-profile"`
	EngineVersion                      *string            `ub:"engine-version"`
	AutoMinorVersionUpgrade            *bool              `ub:"auto-minor-version-upgrade"`
	CopyTagsToSnapshot                 *bool              `ub:"copy-tags-to-snapshot"`
	PromotionTier                      *int64             `ub:"promotion-tier"`
	PubliclyAccessible                 *bool              `ub:"publicly-accessible"`
	MonitoringInterval                 *int64             `ub:"monitoring-interval"`
	MonitoringRoleArn                  *string            `ub:"monitoring-role-arn"`
	PerformanceInsightsEnabled         *bool              `ub:"performance-insights-enabled"`
	PerformanceInsightsKMSKeyId        *string            `ub:"performance-insights-kms-key-id"`
	PerformanceInsightsRetentionPeriod *int64             `ub:"performance-insights-retention-period"`
	PreferredBackupWindow              *string            `ub:"preferred-backup-window"`
	PreferredMaintenanceWindow         *string            `ub:"preferred-maintenance-window"`
	CACertificateIdentifier            *string            `ub:"ca-cert-identifier"`
	ForceDestroy                       *bool              `ub:"force-destroy"`
	Tags                               *map[string]string `ub:"tags"`
}

// ClusterInstanceResourceOutput holds the values RDS computes or fills for a cluster
// member. The ARN is the member's handle. The endpoint and port are assigned
// only once the member is provisioned, so they settle after the create wait.
// The writer flag, taken from the cluster's member list, says whether this
// member is the cluster's current primary. The resource id, KMS key, network
// type, and storage-encrypted flag are cloud truth the cluster propagates, and
// the resolved engine version is the full patch level RDS chose. The
// Availability Zone, CA certificate identifier, parameter group, and the backup
// and maintenance windows are filled by RDS when omitted, so a consumer reads
// the value the cloud settled on.
type ClusterInstanceResourceOutput struct {
	Arn                        string `ub:"arn"`
	Endpoint                   string `ub:"endpoint"`
	Port                       int64  `ub:"port"`
	Writer                     bool   `ub:"writer"`
	DbiResourceId              string `ub:"dbi-resource-id"`
	KmsKeyId                   string `ub:"kms-key-id"`
	StorageEncrypted           bool   `ub:"storage-encrypted"`
	NetworkType                string `ub:"network-type"`
	EngineVersionActual        string `ub:"engine-version-actual"`
	AvailabilityZone           string `ub:"availability-zone"`
	CACertificateIdentifier    string `ub:"ca-cert-identifier"`
	DBParameterGroupName       string `ub:"db-parameter-group-name"`
	PreferredBackupWindow      string `ub:"preferred-backup-window"`
	PreferredMaintenanceWindow string `ub:"preferred-maintenance-window"`
}

func (r *ClusterInstanceResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs RDS fixes when a member is created. The
// cluster it joins, its identifier, its engine, its Availability Zone, its
// subnet group, and its custom instance profile cannot be changed on an existing
// member, so a change to any of them requires a new member. Every other input is
// reconciled in place by Update.
func (r *ClusterInstanceResource) ReplaceFields() []string {
	return []string{
		"availability-zone",
		"cluster-identifier",
		"custom-iam-instance-profile",
		"db-subnet-group-name",
		"engine",
		"identifier",
	}
}

// Constraints declares the rules RDS places on a member's inputs that the
// constraint layer can express. The Performance Insights retention period, when
// set, is between seven and 731 days; the precise valid set is checked in code,
// since divisibility is not expressible here. The format rules on the
// identifier, the custom instance profile, the engine, the backup window, the
// maintenance window, and the Performance Insights KMS ARN are likewise checked
// in code, since a pattern is not a constraint.
func (r ClusterInstanceResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.PerformanceInsightsRetentionPeriod)).
			Require(constraint.AtLeast(r.PerformanceInsightsRetentionPeriod, 7),
				constraint.AtMost(r.PerformanceInsightsRetentionPeriod, 731)).
			Message("performance-insights-retention-period must be between 7 and 731"),
	}
}

func (r *ClusterInstanceResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*ClusterInstanceResourceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := r.createInstance(ctx, client)
	if err != nil {
		return nil, err
	}
	// The member exists once the create call returns, but it is not usable until
	// it has joined the cluster, so wait for it to settle before reading the
	// endpoint, port, and other values the create response does not yet hold.
	if err := r.waitAvailable(ctx, client); err != nil {
		return nil, err
	}
	// A CA certificate change takes effect only after a reboot, and is not a
	// create-time setting, so reconcile it now if the configured value differs
	// from what the create assigned.
	created := resp.DBInstance
	if err := r.reconcileCACertificate(ctx, client, created); err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// createInstance issues CreateDBInstance, retrying for up to two minutes while
// RDS rejects a just-created enhanced-monitoring role it cannot yet assume.
func (r *ClusterInstanceResource) createInstance(
	ctx context.Context, client *rds.Client,
) (*rds.CreateDBInstanceOutput, error) {
	in := r.createInput()
	var resp *rds.CreateDBInstanceOutput
	err := retry.OnError(ctx, clusterInstanceIAMPropagating, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateDBInstance(ctx, in)
		return err
	}, retry.WithTimeout(clusterInstancePropagationTimeout))
	if err != nil {
		return nil, fmt.Errorf("create db instance: %w", err)
	}
	return resp, nil
}

// createInput builds the CreateDBInstance request from the member's create-time
// inputs. The CA certificate identifier is not sent here; it is reconciled after
// create by a modify and reboot. Copy-tags-to-snapshot, the promotion tier, and
// public accessibility are sent unconditionally so the member's setting is
// explicit; the rest are sent only when configured, letting RDS apply its own
// default for an omitted field.
func (r *ClusterInstanceResource) createInput() *rds.CreateDBInstanceInput {
	in := &rds.CreateDBInstanceInput{
		DBClusterIdentifier:                aws.String(r.ClusterIdentifier),
		DBInstanceClass:                    aws.String(r.InstanceClass),
		Engine:                             aws.String(r.Engine),
		DBInstanceIdentifier:               aws.String(r.Identifier),
		AvailabilityZone:                   r.AvailabilityZone,
		DBParameterGroupName:               r.DBParameterGroupName,
		DBSubnetGroupName:                  r.DBSubnetGroupName,
		CustomIamInstanceProfile:           r.CustomIamInstanceProfile,
		EngineVersion:                      r.EngineVersion,
		AutoMinorVersionUpgrade:            r.AutoMinorVersionUpgrade,
		CopyTagsToSnapshot:                 aws.Bool(aws.ToBool(r.CopyTagsToSnapshot)),
		PromotionTier:                      ptr.Int32(aws.Int64(aws.ToInt64(r.PromotionTier))),
		PubliclyAccessible:                 aws.Bool(aws.ToBool(r.PubliclyAccessible)),
		MonitoringInterval:                 ptr.Int32(r.MonitoringInterval),
		MonitoringRoleArn:                  r.MonitoringRoleArn,
		EnablePerformanceInsights:          r.PerformanceInsightsEnabled,
		PerformanceInsightsKMSKeyId:        r.PerformanceInsightsKMSKeyId,
		PerformanceInsightsRetentionPeriod: ptr.Int32(r.PerformanceInsightsRetentionPeriod),
		PreferredBackupWindow:              r.PreferredBackupWindow,
		PreferredMaintenanceWindow:         r.PreferredMaintenanceWindow,
		Tags:                               tagList(ptr.Value(r.Tags)),
	}
	return in
}

// reconcileCACertificate sets the CA certificate identifier when one is
// configured and differs from the value the create assigned. The certificate
// takes effect only after a reboot, so it modifies the member, waits for it,
// reboots, and waits again. Each wait re-confirms the available state on
// consecutive reads, since the member's status transition can lag the call
// that starts it and a stale available read would end the wait early.
func (r *ClusterInstanceResource) reconcileCACertificate(
	ctx context.Context, client *rds.Client, created *rdstypes.DBInstance,
) error {
	if r.CACertificateIdentifier == nil {
		return nil
	}
	if created != nil &&
		aws.ToString(created.CACertificateIdentifier) == *r.CACertificateIdentifier {
		return nil
	}
	_, err := client.ModifyDBInstance(ctx, &rds.ModifyDBInstanceInput{
		DBInstanceIdentifier:    aws.String(r.Identifier),
		ApplyImmediately:        aws.Bool(true),
		CACertificateIdentifier: r.CACertificateIdentifier,
	})
	if err != nil {
		return fmt.Errorf("modify db instance ca certificate: %w", err)
	}
	if err := r.waitAvailableSettled(ctx, client); err != nil {
		return err
	}
	if _, err := client.RebootDBInstance(ctx, &rds.RebootDBInstanceInput{
		DBInstanceIdentifier: aws.String(r.Identifier),
	}); err != nil {
		return fmt.Errorf("reboot db instance: %w", err)
	}
	return r.waitAvailableSettled(ctx, client)
}

func (r *ClusterInstanceResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *ClusterInstanceResourceOutput,
) (*ClusterInstanceResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the member by identifier, then reads its cluster to derive the
// writer flag from the cluster's member list. A missing member, by typed fault
// or empty result, maps to runtime.ErrNotFound so a plan recreates it.
func (r *ClusterInstanceResource) read(
	ctx context.Context, client *rds.Client,
) (*ClusterInstanceResourceOutput, error) {
	inst, err := r.findInstance(ctx, client)
	if err != nil {
		return nil, err
	}
	writer, err := r.readWriter(ctx, client, inst)
	if err != nil {
		return nil, err
	}
	out := &ClusterInstanceResourceOutput{
		Arn:                        aws.ToString(inst.DBInstanceArn),
		Writer:                     writer,
		DbiResourceId:              aws.ToString(inst.DbiResourceId),
		KmsKeyId:                   aws.ToString(inst.KmsKeyId),
		StorageEncrypted:           aws.ToBool(inst.StorageEncrypted),
		NetworkType:                aws.ToString(inst.NetworkType),
		EngineVersionActual:        aws.ToString(inst.EngineVersion),
		AvailabilityZone:           aws.ToString(inst.AvailabilityZone),
		CACertificateIdentifier:    aws.ToString(inst.CACertificateIdentifier),
		PreferredBackupWindow:      aws.ToString(inst.PreferredBackupWindow),
		PreferredMaintenanceWindow: aws.ToString(inst.PreferredMaintenanceWindow),
	}
	if inst.Endpoint != nil {
		out.Endpoint = aws.ToString(inst.Endpoint.Address)
		out.Port = int64(aws.ToInt32(inst.Endpoint.Port))
	}
	if len(inst.DBParameterGroups) > 0 {
		out.DBParameterGroupName = aws.ToString(inst.DBParameterGroups[0].DBParameterGroupName)
	}
	return out, nil
}

// readWriter reads the member's cluster and reports whether this member is the
// cluster's writer. A member whose cluster identifier is empty is not a cluster
// member at all, which is a hard error rather than a normal path. A cluster that
// no longer exists, or that no longer lists this member, leaves the writer flag
// false.
func (r *ClusterInstanceResource) readWriter(
	ctx context.Context, client *rds.Client, inst *rdstypes.DBInstance,
) (bool, error) {
	clusterID := aws.ToString(inst.DBClusterIdentifier)
	if clusterID == "" {
		return false, fmt.Errorf(
			"db instance %s is not a member of a cluster", r.Identifier)
	}
	resp, err := client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
		DBClusterIdentifier: aws.String(clusterID),
	})
	if err != nil {
		var notFound *rdstypes.DBClusterNotFoundFault
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, fmt.Errorf("describe db clusters: %w", err)
	}
	if len(resp.DBClusters) == 0 {
		return false, nil
	}
	for _, m := range resp.DBClusters[0].DBClusterMembers {
		if aws.ToString(m.DBInstanceIdentifier) == r.Identifier {
			return aws.ToBool(m.IsClusterWriter), nil
		}
	}
	return false, nil
}

// findInstance describes the member by identifier and returns it. A missing
// member, whether RDS reports it as a typed fault or as an empty result, maps to
// runtime.ErrNotFound.
func (r *ClusterInstanceResource) findInstance(
	ctx context.Context, client *rds.Client,
) (*rdstypes.DBInstance, error) {
	resp, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(r.Identifier),
	})
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

func (r *ClusterInstanceResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[ClusterInstanceResource, *ClusterInstanceResourceOutput],
) (*ClusterInstanceResourceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := syncTags(ctx, client, prior.Outputs.Arn, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	in, changed := r.modifyInput(prior)
	if changed {
		if err := r.modify(ctx, client, in); err != nil {
			return nil, err
		}
		if err := r.waitAvailable(ctx, client); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

// modify issues one ModifyDBInstance, retrying for up to two minutes while RDS
// rejects a just-created enhanced-monitoring role it cannot yet assume.
func (r *ClusterInstanceResource) modify(
	ctx context.Context, client *rds.Client, in *rds.ModifyDBInstanceInput,
) error {
	err := retry.OnError(ctx, clusterInstanceIAMPropagating, func(ctx context.Context) error {
		_, err := client.ModifyDBInstance(ctx, in)
		return err
	}, retry.WithTimeout(clusterInstancePropagationTimeout))
	if err != nil {
		return fmt.Errorf("modify db instance: %w", err)
	}
	return nil
}

// modifyInput builds the ModifyDBInstance request, setting only the fields whose
// inputs changed, and reports whether any did. Every change is applied
// immediately. The three Performance Insights fields are reconciled together:
// any one of them changing sends all three, with the enabled flag always set,
// since RDS treats them as a unit. The CA certificate change rides this one call
// on update; its reboot is handled by the availability wait that follows.
func (r *ClusterInstanceResource) modifyInput(
	prior runtime.Prior[ClusterInstanceResource, *ClusterInstanceResourceOutput],
) (*rds.ModifyDBInstanceInput, bool) {
	p := prior.Inputs
	in := &rds.ModifyDBInstanceInput{
		DBInstanceIdentifier: aws.String(r.Identifier),
		ApplyImmediately:     aws.Bool(true),
	}
	changed := false
	if runtime.Changed(p.InstanceClass, r.InstanceClass) {
		in.DBInstanceClass = aws.String(r.InstanceClass)
		changed = true
	}
	if runtime.Changed(p.DBParameterGroupName, r.DBParameterGroupName) {
		in.DBParameterGroupName = r.DBParameterGroupName
		changed = true
	}
	if runtime.Changed(p.EngineVersion, r.EngineVersion) {
		in.EngineVersion = r.EngineVersion
		changed = true
	}
	if runtime.Changed(p.AutoMinorVersionUpgrade, r.AutoMinorVersionUpgrade) {
		in.AutoMinorVersionUpgrade = r.AutoMinorVersionUpgrade
		changed = true
	}
	if runtime.Changed(p.CopyTagsToSnapshot, r.CopyTagsToSnapshot) {
		in.CopyTagsToSnapshot = r.CopyTagsToSnapshot
		changed = true
	}
	if runtime.Changed(p.PromotionTier, r.PromotionTier) {
		in.PromotionTier = ptr.Int32(r.PromotionTier)
		changed = true
	}
	if runtime.Changed(p.PubliclyAccessible, r.PubliclyAccessible) {
		in.PubliclyAccessible = r.PubliclyAccessible
		changed = true
	}
	if runtime.Changed(p.MonitoringInterval, r.MonitoringInterval) {
		in.MonitoringInterval = ptr.Int32(r.MonitoringInterval)
		changed = true
	}
	if runtime.Changed(p.MonitoringRoleArn, r.MonitoringRoleArn) {
		in.MonitoringRoleArn = r.MonitoringRoleArn
		changed = true
	}
	if r.performanceInsightsChanged(prior) {
		in.EnablePerformanceInsights = aws.Bool(aws.ToBool(r.PerformanceInsightsEnabled))
		in.PerformanceInsightsKMSKeyId = r.PerformanceInsightsKMSKeyId
		in.PerformanceInsightsRetentionPeriod = ptr.Int32(r.PerformanceInsightsRetentionPeriod)
		changed = true
	}
	if runtime.Changed(p.PreferredBackupWindow, r.PreferredBackupWindow) {
		in.PreferredBackupWindow = r.PreferredBackupWindow
		changed = true
	}
	if runtime.Changed(p.PreferredMaintenanceWindow, r.PreferredMaintenanceWindow) {
		in.PreferredMaintenanceWindow = r.PreferredMaintenanceWindow
		changed = true
	}
	if runtime.Changed(p.CACertificateIdentifier, r.CACertificateIdentifier) {
		in.CACertificateIdentifier = r.CACertificateIdentifier
		changed = true
	}
	return in, changed
}

// performanceInsightsChanged reports whether any of the three Performance
// Insights inputs differ from the prior inputs. They are reconciled together,
// since RDS treats the toggle, the KMS key, and the retention period as one
// setting.
func (r *ClusterInstanceResource) performanceInsightsChanged(
	prior runtime.Prior[ClusterInstanceResource, *ClusterInstanceResourceOutput],
) bool {
	p := prior.Inputs
	return runtime.Changed(p.PerformanceInsightsEnabled, r.PerformanceInsightsEnabled) ||
		runtime.Changed(p.PerformanceInsightsKMSKeyId, r.PerformanceInsightsKMSKeyId) ||
		runtime.Changed(p.PerformanceInsightsRetentionPeriod, r.PerformanceInsightsRetentionPeriod)
}

func (r *ClusterInstanceResource) Delete(
	ctx context.Context, cfg *awsCfg, prior *ClusterInstanceResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	if err := r.deleteInstance(ctx, client); err != nil {
		return err
	}
	return r.waitDeleted(ctx, client)
}

// deleteInstance issues DeleteDBInstance and resolves the states a member delete
// can hit. A member on a Custom engine cannot take a final snapshot, so its
// delete skips one. The call retries for the delete window while RDS says the
// replica cluster must go first. When force-destroy is set and the member is the
// last in a read-replica cluster, the cluster is promoted out of read-replica
// mode and the delete is retried once. A member already gone, or already on its
// way out, counts as deleted.
func (r *ClusterInstanceResource) deleteInstance(ctx context.Context, client *rds.Client) error {
	in := &rds.DeleteDBInstanceInput{
		DBInstanceIdentifier: aws.String(r.Identifier),
	}
	if strings.HasPrefix(r.Engine, clusterInstanceEngineCustomPrefix) {
		in.SkipFinalSnapshot = aws.Bool(true)
	}
	err := retry.OnError(ctx, clusterInstanceReplicaClusterBusy, func(ctx context.Context) error {
		_, err := client.DeleteDBInstance(ctx, in)
		return err
	}, retry.WithTimeout(clusterInstanceTimeout))
	if err == nil {
		return r.tolerateDeleted(nil)
	}
	if clusterInstanceLastReplicaMember(err) && aws.ToBool(r.ForceDestroy) {
		if err := r.promoteCluster(ctx, client); err != nil {
			return err
		}
		_, err = client.DeleteDBInstance(ctx, in)
	}
	return r.tolerateDeleted(err)
}

// promoteCluster promotes the member's read-replica cluster out of replica mode
// so its last member can be deleted, then waits for the cluster to return to the
// available state. The cluster identifier is the correct target here, not the
// member identifier.
func (r *ClusterInstanceResource) promoteCluster(ctx context.Context, client *rds.Client) error {
	if _, err := client.PromoteReadReplicaDBCluster(ctx,
		&rds.PromoteReadReplicaDBClusterInput{
			DBClusterIdentifier: aws.String(r.ClusterIdentifier),
		}); err != nil {
		return fmt.Errorf("promote read replica db cluster: %w", err)
	}
	what := fmt.Sprintf("cluster %s to become available", r.ClusterIdentifier)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		resp, err := client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
			DBClusterIdentifier: aws.String(r.ClusterIdentifier),
		})
		if err != nil {
			return false, fmt.Errorf("describe db clusters: %w", err)
		}
		if len(resp.DBClusters) == 0 {
			return false, nil
		}
		return aws.ToString(resp.DBClusters[0].Status) == "available", nil
	}, wait.WithTimeout(clusterInstanceTimeout), wait.WithInterval(30*time.Second))
}

// tolerateDeleted maps the terminal states of a delete to success. A member
// already gone, reported as the not-found fault, is deleted. A member already on
// its way out, reported as an invalid-state fault saying so, proceeds to the
// deletion wait. Any other error fails the delete.
func (r *ClusterInstanceResource) tolerateDeleted(err error) error {
	if err == nil {
		return nil
	}
	var notFound *rdstypes.DBInstanceNotFoundFault
	if errors.As(err, &notFound) {
		return nil
	}
	var invalidState *rdstypes.InvalidDBInstanceStateFault
	if errors.As(err, &invalidState) &&
		strings.Contains(invalidState.ErrorMessage(), clusterInstanceAlreadyDeletingMessage) {
		return nil
	}
	return fmt.Errorf("delete db instance: %w", err)
}

// waitAvailable polls the member until it reaches an available state, treating
// the settling states as not-ready and any other state as a failure. It runs up
// to ninety minutes, since a member can take many minutes to join or modify.
func (r *ClusterInstanceResource) waitAvailable(ctx context.Context, client *rds.Client) error {
	return r.waitAvailableEvery(ctx, client, 1, 30*time.Second)
}

// waitAvailableSettled requires three consecutive available reads before it
// returns, for the CA certificate modify and reboot: the member's status
// transition can lag the call that starts it, so a single available read taken
// before the member leaves the available state would end the wait early. The
// short poll suits re-confirming a value that is already present.
func (r *ClusterInstanceResource) waitAvailableSettled(
	ctx context.Context,
	client *rds.Client,
) error {
	return r.waitAvailableEvery(ctx, client, 3, 10*time.Second)
}

// waitAvailableEvery is the availability poll behind waitAvailable and
// waitAvailableSettled. A describe right after a create can briefly miss the
// new member, so a short run of not-found reads counts as not-ready rather
// than failing the wait; a persistent not-found still fails it.
func (r *ClusterInstanceResource) waitAvailableEvery(
	ctx context.Context, client *rds.Client, consecutive int, interval time.Duration,
) error {
	what := fmt.Sprintf("db instance %s to become available", r.Identifier)
	notFound := 0
	return wait.UntilStable(ctx, what, consecutive, func(ctx context.Context) (bool, error) {
		inst, err := r.findInstance(ctx, client)
		if err != nil {
			if err == runtime.ErrNotFound && notFound < clusterInstanceNotFoundChecks {
				notFound++
				return false, nil
			}
			return false, err
		}
		notFound = 0
		status := aws.ToString(inst.DBInstanceStatus)
		if clusterInstanceAvailableTarget[status] {
			return true, nil
		}
		if clusterInstanceAvailablePending[status] {
			return false, nil
		}
		return false, fmt.Errorf(
			"db instance %s entered unexpected state %q", r.Identifier, status)
	}, wait.WithTimeout(clusterInstanceTimeout), wait.WithInterval(interval))
}

// waitDeleted polls until the member is gone. A not-found, by typed fault or
// empty result, is the target state; a member still in a deleting state is
// not-ready; any other state fails the wait. It runs up to ninety minutes.
func (r *ClusterInstanceResource) waitDeleted(ctx context.Context, client *rds.Client) error {
	what := fmt.Sprintf("db instance %s to be deleted", r.Identifier)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		inst, err := r.findInstance(ctx, client)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		status := aws.ToString(inst.DBInstanceStatus)
		if clusterInstanceDeletedPending[status] {
			return false, nil
		}
		return false, fmt.Errorf(
			"db instance %s entered unexpected state %q while deleting", r.Identifier, status)
	}, wait.WithTimeout(clusterInstanceTimeout), wait.WithInterval(30*time.Second))
}

// validate checks the member's inputs against the RDS format rules the
// constraint layer cannot express. The identifier charset, the custom instance
// profile prefix, the engine value, the backup and maintenance window formats,
// the Performance Insights retention set, and the Performance Insights KMS ARN
// are each verified here so a malformed value fails before any API call.
func (r *ClusterInstanceResource) validate() error {
	if !clusterInstanceIdentifierPattern.MatchString(r.Identifier) {
		return fmt.Errorf("identifier %q must be lowercase alphanumeric and hyphens, "+
			"start with a letter, and have no doubled or trailing hyphen", r.Identifier)
	}
	if !clusterInstanceValidEngine(r.Engine) {
		return fmt.Errorf("engine %q must be one of %s or start with %q",
			r.Engine, strings.Join(clusterInstanceEngineValues, ", "),
			clusterInstanceEngineCustomPrefix)
	}
	if r.CustomIamInstanceProfile != nil &&
		!clusterInstanceCustomProfilePattern.MatchString(*r.CustomIamInstanceProfile) {
		return fmt.Errorf("custom-iam-instance-profile %q must start with AWSRDSCustom",
			*r.CustomIamInstanceProfile)
	}
	if r.PreferredBackupWindow != nil &&
		!clusterInstanceBackupWindowPattern.MatchString(*r.PreferredBackupWindow) {
		return fmt.Errorf("preferred-backup-window %q must be hh24:mi-hh24:mi in UTC",
			*r.PreferredBackupWindow)
	}
	if r.PreferredMaintenanceWindow != nil &&
		!clusterInstanceMaintenanceWindowPattern.MatchString(*r.PreferredMaintenanceWindow) {
		return fmt.Errorf(
			"preferred-maintenance-window %q must be ddd:hh24:mi-ddd:hh24:mi",
			*r.PreferredMaintenanceWindow)
	}
	if err := clusterInstanceValidRetention(r.PerformanceInsightsRetentionPeriod); err != nil {
		return err
	}
	if r.PerformanceInsightsKMSKeyId != nil &&
		!strings.HasPrefix(*r.PerformanceInsightsKMSKeyId, "arn:") {
		return fmt.Errorf("performance-insights-kms-key-id %q must be an ARN",
			*r.PerformanceInsightsKMSKeyId)
	}
	return nil
}

// clusterInstanceValidEngine reports whether engine is an accepted member
// engine: one of the fixed non-Custom values, or any Custom engine by prefix.
func clusterInstanceValidEngine(engine string) bool {
	if strings.HasPrefix(engine, clusterInstanceEngineCustomPrefix) {
		return true
	}
	return slices.Contains(clusterInstanceEngineValues, engine)
}

// clusterInstanceValidRetention reports whether a Performance Insights retention
// period is one RDS accepts: seven days, 731 days, or a multiple of 31 between
// the two. A nil value leaves the field unset.
func clusterInstanceValidRetention(period *int64) error {
	if period == nil {
		return nil
	}
	v := *period
	if v == 7 || v == 731 {
		return nil
	}
	if v >= 7 && v <= 731 && v%31 == 0 {
		return nil
	}
	return fmt.Errorf("performance-insights-retention-period %d must be 7, 731, "+
		"or a multiple of 31 between 7 and 731", v)
}

// clusterInstanceIAMPropagating reports whether err is the transient
// InvalidParameterValue RDS returns from a create or modify while a just-created
// enhanced-monitoring role has not propagated yet. It clears once the role is
// assumable, so a caller retries.
func clusterInstanceIAMPropagating(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) &&
		apiErr.ErrorCode() == "InvalidParameterValue" &&
		strings.Contains(apiErr.ErrorMessage(), clusterInstanceIAMPropagationMessage)
}

// clusterInstanceReplicaClusterBusy reports whether err is the
// InvalidDBClusterStateFault RDS returns while a member cannot be deleted until
// its replica cluster is removed. It clears once the cluster is gone, so the
// delete retries.
func clusterInstanceReplicaClusterBusy(err error) bool {
	var fault *rdstypes.InvalidDBClusterStateFault
	return errors.As(err, &fault) &&
		strings.Contains(fault.ErrorMessage(), clusterInstanceReplicaClusterMessage)
}

// clusterInstanceLastReplicaMember reports whether err is the
// InvalidDBClusterStateFault RDS returns when a member is the last instance of a
// read-replica cluster. With force-destroy set, the cluster is promoted so the
// member can be freed.
func clusterInstanceLastReplicaMember(err error) bool {
	var fault *rdstypes.InvalidDBClusterStateFault
	return errors.As(err, &fault) &&
		strings.Contains(fault.ErrorMessage(), clusterInstanceLastMemberMessage)
}
