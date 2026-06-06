package rds

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// ClusterScaling is the Aurora Serverless v1 scaling configuration, the
// ScalingConfiguration property of AWS::RDS::DBCluster. It applies only to a
// cluster in the serverless engine mode. The capacity bounds set the range the
// cluster scales between; the auto-pause settings govern when an idle cluster
// pauses and what happens when a scaling point cannot be found in time.
//
// The numeric bounds and the timeout-action enum below are rules on a nested
// block, which the constraint layer does not reach, so they are checked in code
// and documented here.
type ClusterScaling struct {
	AutoPause   *bool  `ub:"auto-pause"`
	MaxCapacity *int64 `ub:"max-capacity"`
	MinCapacity *int64 `ub:"min-capacity"`
	// SecondsBeforeTimeout is the time Aurora tries to find a scaling point before
	// enforcing the timeout action; it must be between 60 and 600.
	SecondsBeforeTimeout *int64 `ub:"seconds-before-timeout"`
	// SecondsUntilAutoPause is the idle time before the cluster pauses; it must be
	// between 300 and 86400.
	SecondsUntilAutoPause *int64 `ub:"seconds-until-auto-pause"`
	// TimeoutAction is ForceApplyCapacityChange or RollbackCapacityChange.
	TimeoutAction *string `ub:"timeout-action"`
}

// toSDK converts the serverless v1 scaling block to the RDS SDK type.
func (c *ClusterScaling) toSDK() *rdstypes.ScalingConfiguration {
	if c == nil {
		return nil
	}
	return &rdstypes.ScalingConfiguration{
		AutoPause:             c.AutoPause,
		MaxCapacity:           ptr.Int32(c.MaxCapacity),
		MinCapacity:           ptr.Int32(c.MinCapacity),
		SecondsBeforeTimeout:  ptr.Int32(c.SecondsBeforeTimeout),
		SecondsUntilAutoPause: ptr.Int32(c.SecondsUntilAutoPause),
		TimeoutAction:         c.TimeoutAction,
	}
}

// validate checks the serverless v1 scaling bounds and the timeout-action enum,
// which the constraint layer cannot express on a nested block.
func (c *ClusterScaling) validate() error {
	if c == nil {
		return nil
	}
	if err := inRange("scaling.seconds-before-timeout",
		c.SecondsBeforeTimeout, 60, 600); err != nil {
		return err
	}
	if err := inRange("scaling.seconds-until-auto-pause",
		c.SecondsUntilAutoPause, 300, 86400); err != nil {
		return err
	}
	if c.TimeoutAction != nil &&
		*c.TimeoutAction != "ForceApplyCapacityChange" &&
		*c.TimeoutAction != "RollbackCapacityChange" {
		return errors.New("scaling.timeout-action must be " +
			"ForceApplyCapacityChange or RollbackCapacityChange")
	}
	return nil
}

// ClusterServerlessV2Scaling is the Aurora Serverless v2 scaling configuration,
// the ServerlessV2ScalingConfiguration property of AWS::RDS::DBCluster. The
// capacity is given in Aurora capacity units (ACUs) and may take half-step
// values. RDS supports adding and updating this block but not removing it, so a
// config that removes it leaves the live capacity in place.
//
// The capacity bounds below are rules on a nested block, which the constraint
// layer does not reach, so they are checked in code and documented here.
type ClusterServerlessV2Scaling struct {
	// MaxCapacity is the maximum ACUs; it must be between 1 and 256.
	MaxCapacity *float64 `ub:"max-capacity"`
	// MinCapacity is the minimum ACUs; it must be between 0 and 256.
	MinCapacity *float64 `ub:"min-capacity"`
	// SecondsUntilAutoPause is the idle time before the cluster pauses; it must be
	// between 300 and 86400, and applies only when MinCapacity is 0.
	SecondsUntilAutoPause *int64 `ub:"seconds-until-auto-pause"`
}

// toSDK converts the serverless v2 scaling block to the RDS SDK type. RDS rejects
// a max capacity of zero, so a zero value is left unset; seconds-until-auto-pause
// is only meaningful when the minimum capacity is zero, so it is sent only then.
func (c *ClusterServerlessV2Scaling) toSDK() *rdstypes.ServerlessV2ScalingConfiguration {
	if c == nil {
		return nil
	}
	out := &rdstypes.ServerlessV2ScalingConfiguration{
		MinCapacity: c.MinCapacity,
	}
	if c.MaxCapacity != nil && *c.MaxCapacity != 0 {
		out.MaxCapacity = c.MaxCapacity
	}
	if c.MinCapacity != nil && *c.MinCapacity == 0 {
		out.SecondsUntilAutoPause = ptr.Int32(c.SecondsUntilAutoPause)
	}
	return out
}

// validate checks the serverless v2 capacity bounds, which the constraint layer
// cannot express on a nested block.
func (c *ClusterServerlessV2Scaling) validate() error {
	if c == nil {
		return nil
	}
	if err := inRangeFloat("serverlessv2-scaling.max-capacity",
		c.MaxCapacity, 1, 256); err != nil {
		return err
	}
	if err := inRangeFloat("serverlessv2-scaling.min-capacity",
		c.MinCapacity, 0, 256); err != nil {
		return err
	}
	return inRange("serverlessv2-scaling.seconds-until-auto-pause",
		c.SecondsUntilAutoPause, 300, 86400)
}

// inRange returns an error when an optional integer is set outside [lo, hi]. A
// nil value passes, leaving the bound to AWS for an omitted field.
func inRange(name string, v *int64, lo, hi int64) error {
	if v == nil {
		return nil
	}
	if *v < lo || *v > hi {
		return fmt.Errorf("%s must be between %d and %d", name, lo, hi)
	}
	return nil
}

// inRangeFloat returns an error when an optional number is set outside [lo, hi].
// A nil value passes, leaving the bound to AWS for an omitted field.
func inRangeFloat(name string, v *float64, lo, hi float64) error {
	if v == nil {
		return nil
	}
	if *v < lo || *v > hi {
		return fmt.Errorf("%s must be between %g and %g", name, lo, hi)
	}
	return nil
}

// ClusterS3Import restores a cluster from a database backup stored in Amazon S3,
// the inputs of the RestoreDBClusterFromS3 call. Setting this block selects the
// S3-restore create mode. The bucket holds the backup; the ingestion role grants
// RDS read access to it; the source engine and version describe the database the
// backup was taken from. All but the prefix are required within the block.
type ClusterS3Import struct {
	BucketName          string  `ub:"bucket-name"`
	BucketPrefix        *string `ub:"bucket-prefix"`
	IngestionRole       string  `ub:"ingestion-role"`
	SourceEngine        string  `ub:"source-engine"`
	SourceEngineVersion string  `ub:"source-engine-version"`
}

// ClusterRestoreToPointInTime restores a cluster to an earlier point in time from
// a source cluster, the point-in-time inputs of the RestoreDBClusterToPointInTime
// call. Setting this block selects the point-in-time create mode. The source is
// named by either its identifier or its resource id, exactly one of the two.
// Either a restore time or the latest-restorable-time flag fixes the moment to
// restore to, exactly one of the two. The restore type chooses a full copy or a
// copy-on-write clone.
type ClusterRestoreToPointInTime struct {
	SourceClusterIdentifier *string `ub:"source-cluster-identifier"`
	SourceClusterResourceId *string `ub:"source-cluster-resource-id"`
	RestoreToTime           *string `ub:"restore-to-time"`
	UseLatestRestorableTime *bool   `ub:"use-latest-restorable-time"`
	RestoreType             *string `ub:"restore-type"`
}

// ClusterMasterUserSecret is the secret RDS manages in AWS Secrets Manager for
// the master user password, the MasterUserSecret return value of
// AWS::RDS::DBCluster. It is present only when the password is managed by RDS.
// The secret ARN is the secret's handle; the KMS key id is the key the secret is
// encrypted with; the status reports where the secret is in its lifecycle.
type ClusterMasterUserSecret struct {
	SecretArn    string `ub:"secret-arn"`
	KmsKeyId     string `ub:"kms-key-id"`
	SecretStatus string `ub:"secret-status"`
}

// clusterMasterUserSecret maps the RDS master user secret to the output
// sub-object, returning nil when the cluster has no managed secret.
func clusterMasterUserSecret(s *rdstypes.MasterUserSecret) *ClusterMasterUserSecret {
	if s == nil {
		return nil
	}
	return &ClusterMasterUserSecret{
		SecretArn:    aws.ToString(s.SecretArn),
		KmsKeyId:     aws.ToString(s.KmsKeyId),
		SecretStatus: aws.ToString(s.SecretStatus),
	}
}
