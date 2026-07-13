package efs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	efssdk "github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	fileSystemWaitTimeout        = 10 * time.Minute
	replicationWaitTimeout       = 20 * time.Minute
	fileSystemPolicyRetryTimeout = 2 * time.Minute
)

var (
	fileSystemPollInterval        = 5 * time.Second
	fileSystemPolicyRetryInterval = 5 * time.Second
	kmsKeyIDPattern               = regexp.MustCompile(
		`^(?:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}` +
			`|mrk-[0-9a-f]{32}|alias/[a-zA-Z0-9/_-]+` +
			`|arn:aws[-a-z]*:kms:[a-z0-9-]+:[0-9]{12}:` +
			`(?:key/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-` +
			`[0-9a-f]{12}|key/mrk-[0-9a-f]{32}|alias/[a-zA-Z0-9/_-]+))$`)
	availabilityZoneNamePattern = regexp.MustCompile(`^.+$`)
	regionNamePattern           = regexp.MustCompile(
		`^[a-z]{2}-(?:(?:iso[a-z]?-|gov-))?[a-z]+-?[0-9]?$`)
	fileSystemIDPattern = regexp.MustCompile(
		`^(?:arn:aws[-a-z]*:elasticfilesystem:[0-9a-z-:]+:` +
			`file-system/fs-[0-9a-f]{8,40}|fs-[0-9a-f]{8,40})$`)
	tagKeyPattern = regexp.MustCompile(
		`^[-\p{L}\p{Z}\p{N}_.:/=+@]+$`)
	tagValuePattern = regexp.MustCompile(
		`^[-\p{L}\p{Z}\p{N}_.:/=+@]*$`)
)

type FileSystemLifecyclePolicy struct {
	TransitionToIA                  *string `ub:"transition-to-ia"`
	TransitionToArchive             *string `ub:"transition-to-archive"`
	TransitionToPrimaryStorageClass *string `ub:"transition-to-primary-storage-class"`
}

type FileSystemBackupPolicy struct {
	Status string `ub:"status"`
}

type FileSystemProtection struct {
	ReplicationOverwriteProtection string `ub:"replication-overwrite-protection"`
}

type FileSystemReplicationDestination struct {
	Region               *string `ub:"region"`
	AvailabilityZoneName *string `ub:"availability-zone-name"`
	KmsKeyId             *string `ub:"kms-key-id"`
	FileSystemId         *string `ub:"file-system-id"`
	RoleArn              *string `ub:"role-arn"`
}

type FileSystemReplicationConfiguration struct {
	Destinations []FileSystemReplicationDestination `ub:"destinations"`
}

type FileSystemSizeOutput struct {
	Value           int64  `ub:"value"`
	Timestamp       string `ub:"timestamp"`
	ValueInArchive  int64  `ub:"value-in-archive"`
	ValueInIA       int64  `ub:"value-in-ia"`
	ValueInStandard int64  `ub:"value-in-standard"`
}

type FileSystemReplicationDestinationOutput struct {
	FileSystemId  string `ub:"file-system-id"`
	Region        string `ub:"region"`
	OwnerId       string `ub:"owner-id"`
	RoleArn       string `ub:"role-arn"`
	Status        string `ub:"status"`
	StatusMessage string `ub:"status-message"`
}

type FileSystemReplicationOutput struct {
	Destinations []FileSystemReplicationDestinationOutput `ub:"destinations"`
}

type FileSystemResource struct {
	AvailabilityZoneName         *string  `ub:"availability-zone-name"`
	Encrypted                    *bool    `ub:"encrypted"`
	KmsKeyId                     *string  `ub:"kms-key-id"`
	PerformanceMode              *string  `ub:"performance-mode"`
	ThroughputMode               *string  `ub:"throughput-mode"`
	ProvisionedThroughputInMibps *float64 `ub:"provisioned-throughput-in-mibps"`

	LifecyclePolicies *[]FileSystemLifecyclePolicy `ub:"lifecycle-policies"`

	BackupPolicy         *FileSystemBackupPolicy `ub:"backup-policy"`
	FileSystemPolicy     *string                 `ub:"file-system-policy"`
	FileSystemProtection *FileSystemProtection   `ub:"file-system-protection"`

	BypassPolicyLockoutSafetyCheck *bool `ub:"bypass-policy-lockout-safety-check"`

	ReplicationConfiguration *FileSystemReplicationConfiguration `ub:"replication-configuration"`

	Tags *map[string]string `ub:"tags"`
}

type FileSystemResourceOutput struct {
	FileSystemId         string `ub:"file-system-id"`
	Arn                  string `ub:"arn"`
	AvailabilityZoneId   string `ub:"availability-zone-id"`
	AvailabilityZoneName string `ub:"availability-zone-name"`
	DNSName              string `ub:"dns-name"`
	OwnerId              string `ub:"owner-id"`
	Name                 string `ub:"name"`

	NumberOfMountTargets int64                `ub:"number-of-mount-targets"`
	SizeInBytes          FileSystemSizeOutput `ub:"size-in-bytes"`

	Encrypted                    bool    `ub:"encrypted"`
	KmsKeyId                     string  `ub:"kms-key-id"`
	PerformanceMode              string  `ub:"performance-mode"`
	ProvisionedThroughputInMibps float64 `ub:"provisioned-throughput-in-mibps"`
	ThroughputMode               string  `ub:"throughput-mode"`

	LifecyclePolicies []FileSystemLifecyclePolicy `ub:"lifecycle-policies"`

	BackupPolicy         *FileSystemBackupPolicy `ub:"backup-policy"`
	FileSystemPolicy     *string                 `ub:"file-system-policy"`
	FileSystemProtection *FileSystemProtection   `ub:"file-system-protection"`

	ReplicationConfiguration *FileSystemReplicationOutput `ub:"replication-configuration"`

	Tags map[string]string `ub:"tags"`
}

func (r *FileSystemResource) SchemaVersion() int { return 1 }

func (r *FileSystemResource) ReplaceFields() []string {
	return []string{
		"availability-zone-name",
		"encrypted",
		"kms-key-id",
		"performance-mode",
	}
}

func (r FileSystemResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.KmsKeyId)).
			Require(constraint.IsTrue(r.Encrypted)).
			Message("kms-key-id requires encrypted to be true"),
		constraint.When(constraint.Present(r.PerformanceMode)).
			Require(constraint.OneOf(r.PerformanceMode, "generalPurpose", "maxIO")).
			Message("performance-mode must be generalPurpose or maxIO"),
		constraint.When(constraint.Present(r.ThroughputMode)).
			Require(constraint.OneOf(r.ThroughputMode, "bursting", "provisioned", "elastic")).
			Message("throughput-mode must be bursting, provisioned, or elastic"),
		constraint.When(constraint.Equals(r.ThroughputMode, "provisioned")).
			Require(constraint.Present(r.ProvisionedThroughputInMibps)).
			Message("provisioned throughput requires provisioned-throughput-in-mibps"),
		constraint.When(constraint.Present(r.ProvisionedThroughputInMibps)).
			Require(constraint.Equals(r.ThroughputMode, "provisioned")).
			Message("provisioned-throughput-in-mibps requires provisioned throughput-mode"),
		constraint.Must(constraint.MaxItems(r.LifecyclePolicies, 3)).
			Message("lifecycle-policies holds at most 3 entries"),
		constraint.ForEach(r.LifecyclePolicies,
			func(policy FileSystemLifecyclePolicy) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.ExactlyOneOf(
						policy.TransitionToIA,
						policy.TransitionToArchive,
						policy.TransitionToPrimaryStorageClass,
					),
				}
			}),
		constraint.When(constraint.Present(r.BackupPolicy)).
			Require(constraint.OneOf(r.BackupPolicy.Status, "ENABLED", "DISABLED")).
			Message("backup-policy status must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.FileSystemProtection)).
			Require(constraint.OneOf(
				r.FileSystemProtection.ReplicationOverwriteProtection,
				"ENABLED",
				"DISABLED",
			)).
			Message("file-system-protection must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.ReplicationConfiguration)).
			Require(
				constraint.MinItems(r.ReplicationConfiguration.Destinations, 1),
				constraint.MaxItems(r.ReplicationConfiguration.Destinations, 1),
			).
			Message("replication-configuration must have exactly one destination"),
		constraint.ForEach(r.ReplicationConfiguration.Destinations,
			func(destination FileSystemReplicationDestination) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.AtLeastOneOf(
						destination.Region,
						destination.AvailabilityZoneName,
					),
				}
			}),
	}
}

func (r *FileSystemResource) ValidateInputs(context.Context, *awsCfg) error {
	if err := r.validateCoreInputs(); err != nil {
		return err
	}
	if err := r.validateLifecyclePolicies(); err != nil {
		return err
	}
	if err := r.validateOptionalConfiguration(); err != nil {
		return err
	}
	return r.validateTags()
}

func (r *FileSystemResource) validateCoreInputs() error {
	if r.AvailabilityZoneName != nil &&
		!validServiceString(*r.AvailabilityZoneName, 1, 64,
			availabilityZoneNamePattern) {
		return errors.New("availability-zone-name must contain 1 to 64 nonempty characters")
	}
	if r.KmsKeyId != nil {
		if r.Encrypted == nil || !*r.Encrypted {
			return errors.New("kms-key-id requires encrypted to be true")
		}
		if !validKMSKeyID(*r.KmsKeyId) {
			return errors.New("kms-key-id must be a key ID, key ARN, alias, or alias ARN")
		}
	}
	if r.PerformanceMode != nil &&
		!slices.Contains([]string{"generalPurpose", "maxIO"}, *r.PerformanceMode) {
		return errors.New("performance-mode must be generalPurpose or maxIO")
	}
	if r.ThroughputMode != nil &&
		!slices.Contains([]string{"bursting", "provisioned", "elastic"}, *r.ThroughputMode) {
		return errors.New("throughput-mode must be bursting, provisioned, or elastic")
	}
	mode := aws.ToString(r.ThroughputMode)
	if mode == "provisioned" && r.ProvisionedThroughputInMibps == nil {
		return errors.New(
			"provisioned throughput-mode requires provisioned-throughput-in-mibps")
	}
	if r.ProvisionedThroughputInMibps != nil {
		if mode != "provisioned" {
			return errors.New(
				"provisioned-throughput-in-mibps requires provisioned throughput-mode")
		}
		if *r.ProvisionedThroughputInMibps < 1 {
			return errors.New("provisioned-throughput-in-mibps must be at least 1")
		}
	}
	if aws.ToString(r.PerformanceMode) == "maxIO" {
		if r.AvailabilityZoneName != nil {
			return errors.New("maxIO performance-mode is invalid for One Zone file systems")
		}
		if mode == "elastic" {
			return errors.New("maxIO performance-mode is invalid with elastic throughput")
		}
	}
	return nil
}

func (r *FileSystemResource) validateLifecyclePolicies() error {
	policies := ptr.Value(r.LifecyclePolicies)
	if len(policies) > 3 {
		return errors.New("lifecycle-policies holds at most 3 entries")
	}
	seen := map[string]bool{}
	transitionDays := map[string]int{}
	for i, policy := range policies {
		transitions := map[string]*string{
			"transition-to-ia":                    policy.TransitionToIA,
			"transition-to-archive":               policy.TransitionToArchive,
			"transition-to-primary-storage-class": policy.TransitionToPrimaryStorageClass,
		}
		kind := ""
		value := ""
		for name, transition := range transitions {
			if transition == nil {
				continue
			}
			if kind != "" {
				return fmt.Errorf("lifecycle-policies[%d] must have exactly one transition", i)
			}
			kind = name
			value = *transition
		}
		if kind == "" {
			return fmt.Errorf("lifecycle-policies[%d] must have exactly one transition", i)
		}
		if seen[kind] {
			return errors.New("lifecycle-policies allows one policy per transition")
		}
		seen[kind] = true
		if err := validateLifecycleTransition(kind, value); err != nil {
			return fmt.Errorf("lifecycle-policies[%d]: %w", i, err)
		}
		if kind != "transition-to-primary-storage-class" {
			transitionDays[kind] = lifecycleTransitionDays[value]
		}
	}
	archive, hasArchive := transitionDays["transition-to-archive"]
	if hasArchive {
		ia, hasIA := transitionDays["transition-to-ia"]
		if !hasIA || archive <= ia {
			return errors.New("transition-to-archive must be later than transition-to-ia")
		}
		performanceMode := aws.ToString(r.PerformanceMode)
		if performanceMode == "" {
			performanceMode = "generalPurpose"
		}
		if aws.ToString(r.ThroughputMode) != "elastic" ||
			performanceMode != "generalPurpose" {
			return errors.New("transition-to-archive requires elastic throughput and " +
				"generalPurpose performance")
		}
	}
	return nil
}

func (r *FileSystemResource) validateOptionalConfiguration() error {
	if r.BackupPolicy != nil &&
		!slices.Contains([]string{"ENABLED", "DISABLED"}, r.BackupPolicy.Status) {
		return errors.New("backup-policy status must be ENABLED or DISABLED")
	}
	if r.FileSystemPolicy != nil {
		if n := utf8.RuneCountInString(*r.FileSystemPolicy); n < 1 || n > 20000 {
			return errors.New("file-system-policy must contain 1 to 20,000 characters")
		}
		if !json.Valid([]byte(*r.FileSystemPolicy)) {
			return errors.New("file-system-policy must be valid JSON")
		}
	} else if r.BypassPolicyLockoutSafetyCheck != nil {
		return errors.New(
			"bypass-policy-lockout-safety-check requires file-system-policy")
	}
	if r.FileSystemProtection != nil && !slices.Contains(
		[]string{"ENABLED", "DISABLED"},
		r.FileSystemProtection.ReplicationOverwriteProtection,
	) {
		return errors.New("file-system-protection must be ENABLED or DISABLED")
	}
	if r.ReplicationConfiguration == nil {
		return nil
	}
	if len(r.ReplicationConfiguration.Destinations) != 1 {
		return errors.New("replication-configuration must have exactly one destination")
	}
	destination := r.ReplicationConfiguration.Destinations[0]
	if destination.Region == nil && destination.AvailabilityZoneName == nil {
		return errors.New("replication destination requires region or availability-zone-name")
	}
	if destination.Region != nil &&
		!validServiceString(*destination.Region, 1, 64, regionNamePattern) {
		return errors.New("replication destination region is invalid")
	}
	if destination.AvailabilityZoneName != nil &&
		!validServiceString(*destination.AvailabilityZoneName, 1, 64,
			availabilityZoneNamePattern) {
		return errors.New(
			"replication destination availability-zone-name must contain 1 to 64 " +
				"nonempty characters")
	}
	if destination.KmsKeyId != nil && !validKMSKeyID(*destination.KmsKeyId) {
		return errors.New("replication destination kms-key-id is invalid")
	}
	if destination.FileSystemId != nil &&
		!validServiceString(*destination.FileSystemId, 0, 128, fileSystemIDPattern) {
		return errors.New("replication destination file-system-id is invalid")
	}
	if destination.RoleArn != nil {
		if !validRoleARN(*destination.RoleArn) {
			return errors.New("replication destination role-arn must be a valid IAM role ARN")
		}
	}
	return nil
}

func (r *FileSystemResource) validateTags() error {
	for key, value := range ptr.Value(r.Tags) {
		if hasReservedTagPrefix(key) {
			return fmt.Errorf("tag key %q must not start with aws:", key)
		}
		if n := utf8.RuneCountInString(key); n < 1 || n > 128 {
			return fmt.Errorf("tag key %q must contain 1 to 128 characters", key)
		}
		if !tagKeyPattern.MatchString(key) {
			return fmt.Errorf("tag key %q contains characters outside the allowed characters", key)
		}
		if n := utf8.RuneCountInString(value); n > 256 {
			return fmt.Errorf("tag value for %q must contain at most 256 characters", key)
		}
		if !tagValuePattern.MatchString(value) {
			return fmt.Errorf(
				"tag value for %q contains characters outside the allowed characters", key)
		}
	}
	return nil
}

func (r *FileSystemResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*FileSystemResourceOutput, error) {
	clients, err := newFileSystemClients(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, clients)
}

func (r *FileSystemResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *FileSystemResourceOutput,
) (*FileSystemResourceOutput, error) {
	clients, err := newFileSystemClients(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, clients, prior)
}

func (r *FileSystemResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) (*FileSystemResourceOutput, error) {
	clients, err := newFileSystemClients(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, clients, prior)
}

func (r *FileSystemResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *FileSystemResourceOutput,
) error {
	clients, err := newFileSystemClients(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, clients, prior)
}

func (r *FileSystemResource) create(
	ctx context.Context,
	clients fileSystemClientProvider,
) (*FileSystemResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	token, err := newCreationToken()
	if err != nil {
		return nil, err
	}
	in := &efssdk.CreateFileSystemInput{
		CreationToken:                aws.String(token),
		AvailabilityZoneName:         r.AvailabilityZoneName,
		Encrypted:                    r.Encrypted,
		KmsKeyId:                     r.KmsKeyId,
		ProvisionedThroughputInMibps: r.ProvisionedThroughputInMibps,
		Tags:                         fileSystemTags(ptr.Value(r.Tags)),
	}
	if r.PerformanceMode != nil {
		in.PerformanceMode = efstypes.PerformanceMode(*r.PerformanceMode)
	}
	if r.ThroughputMode != nil {
		in.ThroughputMode = efstypes.ThroughputMode(*r.ThroughputMode)
	}
	created, err := clients.Source().CreateFileSystem(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("create file system: %w", err)
	}
	fileSystemID := ""
	if created != nil {
		fileSystemID = aws.ToString(created.FileSystemId)
	}
	if fileSystemID == "" {
		return nil, errors.New("create file system returned no file-system-id")
	}
	description, err := waitFileSystemAvailable(ctx, clients.Source(), fileSystemID)
	if err != nil {
		return nil, err
	}
	if r.LifecyclePolicies != nil {
		if err := r.putLifecyclePolicies(ctx, clients.Source(), fileSystemID); err != nil {
			return nil, err
		}
	}
	if r.BackupPolicy != nil {
		if err := putBackupPolicy(ctx, clients.Source(), fileSystemID,
			r.BackupPolicy.Status); err != nil {
			return nil, err
		}
	}
	if r.FileSystemPolicy != nil {
		if err := r.putFileSystemPolicy(ctx, clients.Source(), fileSystemID); err != nil {
			return nil, err
		}
	}
	if r.FileSystemProtection != nil {
		if err := updateFileSystemProtection(ctx, clients.Source(), fileSystemID,
			r.FileSystemProtection.ReplicationOverwriteProtection); err != nil {
			return nil, err
		}
	}
	if r.ReplicationConfiguration != nil {
		if err := r.createReplication(ctx, clients.Source(), fileSystemID,
			aws.ToString(description.OwnerId)); err != nil {
			return nil, err
		}
	}
	return r.readByID(ctx, clients, fileSystemID)
}

func (r *FileSystemResource) read(
	ctx context.Context,
	clients fileSystemClientProvider,
	prior *FileSystemResourceOutput,
) (*FileSystemResourceOutput, error) {
	if prior == nil || prior.FileSystemId == "" {
		return nil, errors.New("read file system: missing prior file-system-id")
	}
	return r.readByID(ctx, clients, prior.FileSystemId)
}

func (r *FileSystemResource) update(
	ctx context.Context,
	clients fileSystemClientProvider,
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) (*FileSystemResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	if prior.Outputs == nil || prior.Outputs.FileSystemId == "" {
		return nil, errors.New("update file system: missing prior file-system-id")
	}
	fileSystemID := prior.Outputs.FileSystemId
	client := clients.Source()
	if r.throughputChanged(prior) {
		if err := r.updateThroughput(ctx, client, fileSystemID, prior); err != nil {
			return nil, err
		}
	}
	if r.lifecycleChanged(prior) {
		if err := r.putLifecyclePolicies(ctx, client, fileSystemID); err != nil {
			return nil, err
		}
	}
	if r.backupChanged(prior) {
		if err := putBackupPolicy(ctx, client, fileSystemID, r.desiredBackupStatus()); err != nil {
			return nil, err
		}
	}
	if r.policyChanged(prior) {
		if r.FileSystemPolicy == nil {
			if err := deleteFileSystemPolicy(ctx, client, fileSystemID); err != nil {
				return nil, err
			}
		} else if err := r.putFileSystemPolicy(ctx, client, fileSystemID); err != nil {
			return nil, err
		}
	}
	if r.protectionChanged(prior) {
		if err := updateFileSystemProtection(ctx, client, fileSystemID,
			r.desiredProtectionStatus()); err != nil {
			return nil, err
		}
	}
	if r.replicationChanged(prior) {
		if err := deleteReplication(ctx, clients, fileSystemID); err != nil {
			return nil, err
		}
		if r.ReplicationConfiguration != nil {
			description, err := waitFileSystemAvailable(ctx, client, fileSystemID)
			if err != nil {
				return nil, err
			}
			if err := r.createReplication(ctx, client, fileSystemID,
				aws.ToString(description.OwnerId)); err != nil {
				return nil, err
			}
		}
	}
	if r.tagsChanged(prior) {
		if err := r.syncTags(ctx, client, fileSystemID); err != nil {
			return nil, err
		}
	}
	return r.readByID(ctx, clients, fileSystemID)
}

func (r *FileSystemResource) delete(
	ctx context.Context,
	clients fileSystemClientProvider,
	prior *FileSystemResourceOutput,
) error {
	if prior == nil || prior.FileSystemId == "" {
		return errors.New("delete file system: missing prior file-system-id")
	}
	fileSystemID := prior.FileSystemId
	if err := deleteReplication(ctx, clients, fileSystemID); err != nil {
		return err
	}
	_, err := clients.Source().DeleteFileSystem(ctx, &efssdk.DeleteFileSystemInput{
		FileSystemId: aws.String(fileSystemID),
	})
	if isFileSystemNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete file system: %w", err)
	}
	return waitFileSystemDeleted(ctx, clients.Source(), fileSystemID)
}

func (r *FileSystemResource) readByID(
	ctx context.Context,
	clients fileSystemClientProvider,
	fileSystemID string,
) (*FileSystemResourceOutput, error) {
	description, err := describeFileSystem(ctx, clients.Source(), fileSystemID)
	if err != nil {
		return nil, err
	}
	if description.LifeCycleState == efstypes.LifeCycleStateDeleted {
		return nil, runtime.ErrNotFound
	}
	lifecycle, err := clients.Source().DescribeLifecycleConfiguration(ctx,
		&efssdk.DescribeLifecycleConfigurationInput{FileSystemId: aws.String(fileSystemID)})
	if isFileSystemNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe lifecycle configuration: %w", err)
	}
	if lifecycle == nil {
		return nil, errors.New("describe lifecycle configuration returned no output")
	}
	backup, err := clients.Source().DescribeBackupPolicy(ctx,
		&efssdk.DescribeBackupPolicyInput{FileSystemId: aws.String(fileSystemID)})
	backupPolicy := (*efstypes.BackupPolicy)(nil)
	if isPolicyNotFound(err) {
		backupPolicy = &efstypes.BackupPolicy{Status: efstypes.StatusDisabled}
	} else if isFileSystemNotFound(err) {
		return nil, runtime.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("describe backup policy: %w", err)
	} else if backup == nil {
		return nil, errors.New("describe backup policy returned no output")
	} else {
		backupPolicy = backup.BackupPolicy
	}
	policy, err := readFileSystemPolicy(ctx, clients.Source(), fileSystemID)
	if err != nil {
		return nil, err
	}
	replications, err := readReplication(ctx, clients.Source(), fileSystemID)
	if err != nil {
		return nil, err
	}
	return &FileSystemResourceOutput{
		FileSystemId:         aws.ToString(description.FileSystemId),
		Arn:                  aws.ToString(description.FileSystemArn),
		AvailabilityZoneId:   aws.ToString(description.AvailabilityZoneId),
		AvailabilityZoneName: aws.ToString(description.AvailabilityZoneName),
		DNSName:              fileSystemDNSName(fileSystemID, clients.Region()),
		OwnerId:              aws.ToString(description.OwnerId),
		Name:                 aws.ToString(description.Name),
		NumberOfMountTargets: int64(description.NumberOfMountTargets),
		SizeInBytes:          fileSystemSizeOutput(description.SizeInBytes),
		Encrypted:            aws.ToBool(description.Encrypted),
		KmsKeyId:             aws.ToString(description.KmsKeyId),
		PerformanceMode:      string(description.PerformanceMode),
		ProvisionedThroughputInMibps: aws.ToFloat64(
			description.ProvisionedThroughputInMibps),
		ThroughputMode:           string(description.ThroughputMode),
		LifecyclePolicies:        flattenLifecyclePolicies(lifecycle.LifecyclePolicies),
		BackupPolicy:             backupPolicyOutput(backupPolicy),
		FileSystemPolicy:         policy,
		FileSystemProtection:     fileSystemProtectionOutput(description.FileSystemProtection),
		ReplicationConfiguration: flattenReplication(replications),
		Tags: userFileSystemTags(
			fileSystemTagMap(description.Tags)),
	}, nil
}

func describeFileSystem(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
) (efstypes.FileSystemDescription, error) {
	resp, err := client.DescribeFileSystems(ctx, &efssdk.DescribeFileSystemsInput{
		FileSystemId: aws.String(fileSystemID),
	})
	if isFileSystemNotFound(err) {
		return efstypes.FileSystemDescription{}, runtime.ErrNotFound
	}
	if err != nil {
		return efstypes.FileSystemDescription{}, fmt.Errorf("describe file system: %w", err)
	}
	if resp == nil || len(resp.FileSystems) == 0 {
		return efstypes.FileSystemDescription{}, runtime.ErrNotFound
	}
	if len(resp.FileSystems) != 1 {
		return efstypes.FileSystemDescription{}, fmt.Errorf(
			"describe file system returned %d results, want 1", len(resp.FileSystems))
	}
	if aws.ToString(resp.FileSystems[0].FileSystemId) != fileSystemID {
		return efstypes.FileSystemDescription{}, runtime.ErrNotFound
	}
	return resp.FileSystems[0], nil
}

func waitFileSystemAvailable(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
) (efstypes.FileSystemDescription, error) {
	var description efstypes.FileSystemDescription
	err := wait.Until(ctx, "EFS file system to become available",
		func(ctx context.Context) (bool, error) {
			current, err := describeFileSystem(ctx, client, fileSystemID)
			if errors.Is(err, runtime.ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			switch current.LifeCycleState {
			case efstypes.LifeCycleStateAvailable:
				description = current
				return true, nil
			case efstypes.LifeCycleStateCreating, efstypes.LifeCycleStateUpdating:
				return false, nil
			default:
				return false, fmt.Errorf("file system %s entered lifecycle state %s",
					fileSystemID, current.LifeCycleState)
			}
		},
		wait.WithTimeout(fileSystemWaitTimeout),
		wait.WithInterval(fileSystemPollInterval),
	)
	if err != nil {
		return efstypes.FileSystemDescription{}, err
	}
	return description, nil
}

func waitFileSystemDeleted(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
) error {
	return wait.Until(ctx, "EFS file system to be deleted",
		func(ctx context.Context) (bool, error) {
			description, err := describeFileSystem(ctx, client, fileSystemID)
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			return description.LifeCycleState == efstypes.LifeCycleStateDeleted, nil
		},
		wait.WithTimeout(fileSystemWaitTimeout),
		wait.WithInterval(fileSystemPollInterval),
	)
}

func (r *FileSystemResource) putLifecyclePolicies(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
) error {
	_, err := client.PutLifecycleConfiguration(ctx,
		&efssdk.PutLifecycleConfigurationInput{
			FileSystemId:      aws.String(fileSystemID),
			LifecyclePolicies: expandLifecyclePolicies(ptr.Value(r.LifecyclePolicies)),
		})
	if isFileSystemNotFound(err) {
		return runtime.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("put lifecycle configuration: %w", err)
	}
	return nil
}

func putBackupPolicy(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
	status string,
) error {
	desired := efstypes.Status(status)
	_, err := client.PutBackupPolicy(ctx, &efssdk.PutBackupPolicyInput{
		FileSystemId: aws.String(fileSystemID),
		BackupPolicy: &efstypes.BackupPolicy{Status: desired},
	})
	if isFileSystemNotFound(err) {
		return runtime.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("put backup policy: %w", err)
	}
	return wait.Until(ctx, "EFS backup policy to reach its desired status",
		func(ctx context.Context) (bool, error) {
			resp, err := client.DescribeBackupPolicy(ctx,
				&efssdk.DescribeBackupPolicyInput{FileSystemId: aws.String(fileSystemID)})
			if isFileSystemNotFound(err) {
				return false, runtime.ErrNotFound
			}
			if err != nil {
				return false, fmt.Errorf("describe backup policy: %w", err)
			}
			if resp == nil || resp.BackupPolicy == nil {
				return false, nil
			}
			return resp.BackupPolicy.Status == desired, nil
		},
		wait.WithTimeout(fileSystemWaitTimeout),
		wait.WithInterval(fileSystemPollInterval),
	)
}

func (r *FileSystemResource) putFileSystemPolicy(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
) error {
	in := &efssdk.PutFileSystemPolicyInput{
		FileSystemId:                   aws.String(fileSystemID),
		Policy:                         r.FileSystemPolicy,
		BypassPolicyLockoutSafetyCheck: aws.ToBool(r.BypassPolicyLockoutSafetyCheck),
	}
	err := retry.OnError(ctx, isFileSystemPolicyRetryable,
		func(ctx context.Context) error {
			_, err := client.PutFileSystemPolicy(ctx, in)
			return err
		},
		retry.WithTimeout(fileSystemPolicyRetryTimeout),
		retry.WithInterval(fileSystemPolicyRetryInterval),
	)
	if isFileSystemNotFound(err) {
		return runtime.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("put file system policy: %w", err)
	}
	return nil
}

func readFileSystemPolicy(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
) (*string, error) {
	resp, err := client.DescribeFileSystemPolicy(ctx,
		&efssdk.DescribeFileSystemPolicyInput{FileSystemId: aws.String(fileSystemID)})
	if isPolicyNotFound(err) {
		return nil, nil
	}
	if isFileSystemNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe file system policy: %w", err)
	}
	if resp == nil || resp.Policy == nil {
		return nil, nil
	}
	return resp.Policy, nil
}

func deleteFileSystemPolicy(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
) error {
	_, err := client.DeleteFileSystemPolicy(ctx, &efssdk.DeleteFileSystemPolicyInput{
		FileSystemId: aws.String(fileSystemID),
	})
	if isPolicyNotFound(err) {
		return nil
	}
	if isFileSystemNotFound(err) {
		return runtime.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("delete file system policy: %w", err)
	}
	return nil
}

func updateFileSystemProtection(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
	status string,
) error {
	_, err := client.UpdateFileSystemProtection(ctx,
		&efssdk.UpdateFileSystemProtectionInput{
			FileSystemId: aws.String(fileSystemID),
			ReplicationOverwriteProtection: efstypes.ReplicationOverwriteProtection(
				status),
		})
	if isFileSystemNotFound(err) {
		return runtime.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update file system protection: %w", err)
	}
	return nil
}

func (r *FileSystemResource) createReplication(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
	sourceOwnerID string,
) error {
	destination := r.ReplicationConfiguration.Destinations[0]
	if err := validateReplicationAccounts(destination, sourceOwnerID); err != nil {
		return err
	}
	_, err := client.CreateReplicationConfiguration(ctx,
		&efssdk.CreateReplicationConfigurationInput{
			SourceFileSystemId: aws.String(fileSystemID),
			Destinations: []efstypes.DestinationToCreate{
				expandReplicationDestination(destination),
			},
		})
	if err != nil {
		return fmt.Errorf("create replication configuration: %w", err)
	}
	return wait.Until(ctx, "EFS replication configuration to become enabled",
		func(ctx context.Context) (bool, error) {
			replications, err := readReplication(ctx, client, fileSystemID)
			if errors.Is(err, runtime.ErrNotFound) {
				return false, err
			}
			if err != nil {
				return false, err
			}
			if len(replications) == 0 || len(replications[0].Destinations) == 0 {
				return false, nil
			}
			status := replications[0].Destinations[0].Status
			switch status {
			case efstypes.ReplicationStatusEnabled:
				return true, nil
			case efstypes.ReplicationStatusEnabling:
				return false, nil
			default:
				return false, fmt.Errorf("replication configuration entered status %s", status)
			}
		},
		wait.WithTimeout(replicationWaitTimeout),
		wait.WithInterval(fileSystemPollInterval),
	)
}

func readReplication(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
) ([]efstypes.ReplicationConfigurationDescription, error) {
	resp, err := client.DescribeReplicationConfigurations(ctx,
		&efssdk.DescribeReplicationConfigurationsInput{
			FileSystemId: aws.String(fileSystemID),
		})
	if isReplicationNotFound(err) {
		return nil, nil
	}
	if isFileSystemNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe replication configurations: %w", err)
	}
	if resp == nil {
		return nil, nil
	}
	if len(resp.Replications) > 1 {
		return nil, fmt.Errorf("describe replication configurations returned %d results, want 1",
			len(resp.Replications))
	}
	return resp.Replications, nil
}

func deleteReplication(
	ctx context.Context,
	clients fileSystemClientProvider,
	fileSystemID string,
) error {
	replications, err := readReplication(ctx, clients.Source(), fileSystemID)
	if errors.Is(err, runtime.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(replications) == 0 {
		return nil
	}
	if len(replications[0].Destinations) != 1 {
		return fmt.Errorf("replication configuration has %d destinations, want 1",
			len(replications[0].Destinations))
	}
	destination := replications[0].Destinations[0]
	destinationRegion := aws.ToString(destination.Region)
	crossRegion := destinationRegion != "" && destinationRegion != clients.Region()
	if crossRegion {
		if err := deleteReplicationInRegion(ctx, clients.ForRegion(destinationRegion),
			fileSystemID, "destination"); err != nil {
			return err
		}
	}
	return deleteReplicationInRegion(ctx, clients.Source(), fileSystemID, "source")
}

func deleteReplicationInRegion(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
	location string,
) error {
	_, err := client.DeleteReplicationConfiguration(ctx,
		&efssdk.DeleteReplicationConfigurationInput{
			SourceFileSystemId: aws.String(fileSystemID),
		})
	if err != nil && !isFileSystemNotFound(err) && !isReplicationNotFound(err) {
		return fmt.Errorf("delete %s replication configuration: %w", location, err)
	}
	return wait.UntilStable(ctx,
		fmt.Sprintf("EFS %s replication configuration to be deleted", location), 2,
		func(ctx context.Context) (bool, error) {
			replications, err := readReplication(ctx, client, fileSystemID)
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			return len(replications) == 0, nil
		},
		wait.WithTimeout(replicationWaitTimeout),
		wait.WithInterval(fileSystemPollInterval),
	)
}

func (r *FileSystemResource) updateThroughput(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) error {
	in := &efssdk.UpdateFileSystemInput{FileSystemId: aws.String(fileSystemID)}
	modeDrifted := r.ThroughputMode != nil && prior.Observed != nil &&
		prior.Observed.ThroughputMode != *r.ThroughputMode
	if runtime.Changed(prior.Inputs.ThroughputMode, r.ThroughputMode) || modeDrifted {
		mode := aws.ToString(r.ThroughputMode)
		if mode == "" {
			mode = "bursting"
		}
		in.ThroughputMode = efstypes.ThroughputMode(mode)
	}
	valueDrifted := r.ProvisionedThroughputInMibps != nil && prior.Observed != nil &&
		prior.Observed.ProvisionedThroughputInMibps != *r.ProvisionedThroughputInMibps
	if runtime.Changed(prior.Inputs.ProvisionedThroughputInMibps,
		r.ProvisionedThroughputInMibps) || valueDrifted {
		in.ProvisionedThroughputInMibps = r.ProvisionedThroughputInMibps
	}
	_, err := client.UpdateFileSystem(ctx, in)
	if isFileSystemNotFound(err) {
		return runtime.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update file system: %w", err)
	}
	_, err = waitFileSystemAvailable(ctx, client, fileSystemID)
	return err
}

func (r *FileSystemResource) throughputChanged(
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) bool {
	if runtime.Changed(prior.Inputs.ThroughputMode, r.ThroughputMode) ||
		runtime.Changed(prior.Inputs.ProvisionedThroughputInMibps,
			r.ProvisionedThroughputInMibps) {
		return true
	}
	if prior.Observed == nil {
		return false
	}
	if r.ThroughputMode != nil && prior.Observed.ThroughputMode != *r.ThroughputMode {
		return true
	}
	return r.ProvisionedThroughputInMibps != nil &&
		prior.Observed.ProvisionedThroughputInMibps != *r.ProvisionedThroughputInMibps
}

func (r *FileSystemResource) lifecycleChanged(
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) bool {
	if runtime.Changed(prior.Inputs.LifecyclePolicies, r.LifecyclePolicies) {
		return true
	}
	return prior.Observed != nil &&
		runtime.Changed(prior.Observed.LifecyclePolicies, ptr.Value(r.LifecyclePolicies))
}

func (r *FileSystemResource) backupChanged(
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) bool {
	if runtime.Changed(prior.Inputs.BackupPolicy, r.BackupPolicy) {
		return true
	}
	return r.BackupPolicy != nil && prior.Observed != nil &&
		(prior.Observed.BackupPolicy == nil ||
			prior.Observed.BackupPolicy.Status != r.BackupPolicy.Status)
}

func (r *FileSystemResource) desiredBackupStatus() string {
	if r.BackupPolicy == nil {
		return "DISABLED"
	}
	return r.BackupPolicy.Status
}

func (r *FileSystemResource) policyChanged(
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) bool {
	if !fileSystemPoliciesEqual(prior.Inputs.FileSystemPolicy, r.FileSystemPolicy) ||
		runtime.Changed(prior.Inputs.BypassPolicyLockoutSafetyCheck,
			r.BypassPolicyLockoutSafetyCheck) {
		return true
	}
	if prior.Observed == nil {
		return false
	}
	return !fileSystemPoliciesEqual(prior.Observed.FileSystemPolicy, r.FileSystemPolicy)
}

func (r *FileSystemResource) protectionChanged(
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) bool {
	if runtime.Changed(prior.Inputs.FileSystemProtection, r.FileSystemProtection) {
		return true
	}
	return r.FileSystemProtection != nil && prior.Observed != nil &&
		(prior.Observed.FileSystemProtection == nil ||
			!protectionStatusesEqual(
				r.FileSystemProtection.ReplicationOverwriteProtection,
				prior.Observed.FileSystemProtection.ReplicationOverwriteProtection))
}

func (r *FileSystemResource) desiredProtectionStatus() string {
	if r.FileSystemProtection == nil {
		return "ENABLED"
	}
	return r.FileSystemProtection.ReplicationOverwriteProtection
}

func (r *FileSystemResource) replicationChanged(
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) bool {
	if runtime.Changed(prior.Inputs.ReplicationConfiguration,
		r.ReplicationConfiguration) {
		return true
	}
	if prior.Observed == nil {
		return false
	}
	observed := prior.Observed.ReplicationConfiguration
	if r.ReplicationConfiguration == nil {
		return observed != nil
	}
	if observed == nil || len(observed.Destinations) != 1 {
		return true
	}
	desired := r.ReplicationConfiguration.Destinations[0]
	got := observed.Destinations[0]
	return desired.Region != nil && got.Region != *desired.Region ||
		desired.FileSystemId != nil && !sameFileSystemHandle(
			*desired.FileSystemId, got.FileSystemId) ||
		desired.RoleArn != nil && got.RoleArn != *desired.RoleArn
}

func (r *FileSystemResource) tagsChanged(
	prior runtime.Prior[FileSystemResource, *FileSystemResourceOutput],
) bool {
	desired := ptr.Value(r.Tags)
	if !maps.Equal(ptr.Value(prior.Inputs.Tags), desired) {
		return true
	}
	return prior.Observed != nil &&
		!maps.Equal(userFileSystemTags(prior.Observed.Tags), desired)
}

func (r *FileSystemResource) syncTags(
	ctx context.Context,
	client fileSystemClient,
	fileSystemID string,
) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx, &efssdk.ListTagsForResourceInput{
				ResourceId: aws.String(fileSystemID),
			})
			if isFileSystemNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			if err != nil {
				return nil, fmt.Errorf("list file system tags: %w", err)
			}
			if resp == nil {
				return nil, errors.New("list file system tags returned no output")
			}
			return fileSystemTagMap(resp.Tags), nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &efssdk.TagResourceInput{
				ResourceId: aws.String(fileSystemID),
				Tags:       fileSystemTags(upsert),
			})
			if err != nil {
				return fmt.Errorf("tag file system: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &efssdk.UntagResourceInput{
				ResourceId: aws.String(fileSystemID),
				TagKeys:    remove,
			})
			if err != nil {
				return fmt.Errorf("untag file system: %w", err)
			}
			return nil
		},
	)
}

var lifecycleTransitionDays = map[string]int{
	"AFTER_1_DAY":    1,
	"AFTER_7_DAYS":   7,
	"AFTER_14_DAYS":  14,
	"AFTER_30_DAYS":  30,
	"AFTER_60_DAYS":  60,
	"AFTER_90_DAYS":  90,
	"AFTER_180_DAYS": 180,
	"AFTER_270_DAYS": 270,
	"AFTER_365_DAYS": 365,
}

func validateLifecycleTransition(kind string, value string) error {
	if kind == "transition-to-primary-storage-class" {
		if value != "AFTER_1_ACCESS" {
			return errors.New(
				"transition-to-primary-storage-class must be AFTER_1_ACCESS")
		}
		return nil
	}
	if _, ok := lifecycleTransitionDays[value]; !ok {
		return fmt.Errorf("%s has invalid value %q", kind, value)
	}
	return nil
}

func validKMSKeyID(value string) bool {
	return validServiceString(value, 0, 2048, kmsKeyIDPattern)
}

func validRoleARN(value string) bool {
	parsed, ok := parsedARN(value)
	return ok && parsed.Service == "iam" && parsed.AccountID != "" &&
		strings.HasPrefix(parsed.Resource, "role/")
}

func sameFileSystemHandle(desired string, observed string) bool {
	if desired == observed {
		return true
	}
	parsed, ok := parsedARN(desired)
	if !ok {
		return false
	}
	return strings.TrimPrefix(parsed.Resource, "file-system/") == observed
}

func validateReplicationAccounts(
	destination FileSystemReplicationDestination,
	sourceOwnerID string,
) error {
	if destination.RoleArn != nil {
		roleARN, _ := parsedARN(*destination.RoleArn)
		if sourceOwnerID != "" && roleARN.AccountID != sourceOwnerID {
			return errors.New("replication role-arn must belong to the source account")
		}
	}
	if destination.FileSystemId != nil {
		fileSystemARN, isARN := parsedARN(*destination.FileSystemId)
		crossAccount := isARN && sourceOwnerID != "" &&
			fileSystemARN.AccountID != sourceOwnerID
		if crossAccount && destination.RoleArn == nil {
			return errors.New("cross-account replication requires destination role-arn")
		}
	}
	return nil
}

func validServiceString(
	value string,
	minLength int,
	maxLength int,
	pattern *regexp.Regexp,
) bool {
	length := utf8.RuneCountInString(value)
	return length >= minLength && length <= maxLength && pattern.MatchString(value)
}

func hasReservedTagPrefix(value string) bool {
	return len(value) >= 4 &&
		(value[0] == 'a' || value[0] == 'A') &&
		(value[1] == 'w' || value[1] == 'W') &&
		(value[2] == 's' || value[2] == 'S') &&
		value[3] == ':'
}

func fileSystemPoliciesEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	var leftValue any
	var rightValue any
	if json.Unmarshal([]byte(*left), &leftValue) != nil ||
		json.Unmarshal([]byte(*right), &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func protectionStatusesEqual(desired string, observed string) bool {
	return desired == observed || desired == "DISABLED" && observed == "REPLICATING"
}
