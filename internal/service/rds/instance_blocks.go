package rds

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// InstanceS3Import restores a DB instance from a database backup held in an
// Amazon S3 bucket. Setting this block selects the RestoreDBInstanceFromS3
// create mode. Every field is fixed at create time, so a change to any of them
// replaces the instance. BucketName, IngestionRole, SourceEngine, and
// SourceEngineVersion are required when the block is present; the Instance
// Constraints declare those rules.
type InstanceS3Import struct {
	BucketName          *string `ub:"bucket-name"`
	BucketPrefix        *string `ub:"bucket-prefix"`
	IngestionRole       *string `ub:"ingestion-role"`
	SourceEngine        *string `ub:"source-engine"`
	SourceEngineVersion *string `ub:"source-engine-version"`
}

// InstanceRestoreToPointInTime restores a DB instance to a moment in the source
// instance's recovery window. Setting this block selects the
// RestoreDBInstanceToPointInTime create mode. The source is named by one of
// SourceDbInstanceIdentifier, SourceDbiResourceId, or
// SourceDbInstanceAutomatedBackupsArn, and the restore point is given by either
// RestoreTime or UseLatestRestorableTime, never both. The whole block is fixed
// at create time, so a change replaces the instance.
type InstanceRestoreToPointInTime struct {
	RestoreTime                         *string `ub:"restore-time"`
	UseLatestRestorableTime             *bool   `ub:"use-latest-restorable-time"`
	SourceDbInstanceIdentifier          *string `ub:"source-db-instance-identifier"`
	SourceDbiResourceId                 *string `ub:"source-dbi-resource-id"`
	SourceDbInstanceAutomatedBackupsArn *string `ub:"source-db-instance-automated-backups-arn"`
}

// InstanceMasterUserSecret is the Secrets Manager secret RDS creates and manages
// for the master user password when manage-master-user-password is set. It is a
// computed output: a downstream resource reads SecretArn to grant access to the
// password. RDS never accepts it as an input.
type InstanceMasterUserSecret struct {
	SecretArn    string `ub:"secret-arn"`
	KmsKeyId     string `ub:"kms-key-id"`
	SecretStatus string `ub:"secret-status"`
}

// flattenMasterUserSecret maps the SDK master-user secret onto the output
// sub-object, returning nil when the instance has no managed secret.
func flattenMasterUserSecret(s *rdstypes.MasterUserSecret) *InstanceMasterUserSecret {
	if s == nil {
		return nil
	}
	return &InstanceMasterUserSecret{
		SecretArn:    aws.ToString(s.SecretArn),
		KmsKeyId:     aws.ToString(s.KmsKeyId),
		SecretStatus: aws.ToString(s.SecretStatus),
	}
}

// flattenEndpoint joins an SDK endpoint into the address, port, and hosted-zone
// values an output exposes. The endpoint string is the address and port joined
// by a colon, the form a client uses to connect; it is empty when the instance
// reports no endpoint yet.
func flattenEndpoint(e *rdstypes.Endpoint) (endpoint, address, hostedZone string, port int64) {
	if e == nil {
		return "", "", "", 0
	}
	address = aws.ToString(e.Address)
	hostedZone = aws.ToString(e.HostedZoneId)
	port = int64(aws.ToInt32(e.Port))
	if address != "" {
		endpoint = address + ":" + itoa(port)
	}
	return endpoint, address, hostedZone, port
}

// flattenListenerEndpoint maps the SDK listener endpoint onto the output
// sub-object, returning nil when the instance has none. RDS Custom for SQL
// Server reports a listener endpoint distinct from the primary endpoint.
func flattenListenerEndpoint(e *rdstypes.Endpoint) *InstanceEndpoint {
	if e == nil {
		return nil
	}
	return &InstanceEndpoint{
		Address:      aws.ToString(e.Address),
		Port:         int64(aws.ToInt32(e.Port)),
		HostedZoneId: aws.ToString(e.HostedZoneId),
	}
}

// InstanceEndpoint is the address, port, and hosted-zone of a DB instance
// endpoint, used for the optional listener endpoint output.
type InstanceEndpoint struct {
	Address      string `ub:"address"`
	Port         int64  `ub:"port"`
	HostedZoneId string `ub:"hosted-zone-id"`
}

// expandRestoreToPointInTime fills the point-in-time restore call's source and
// restore-point fields from the block. The restore time is parsed from RFC3339;
// an invalid value returns the parse error so a bad input fails the apply rather
// than silently restoring to the wrong moment.
func expandRestoreToPointInTime(
	in *rds.RestoreDBInstanceToPointInTimeInput, b InstanceRestoreToPointInTime,
) error {
	in.SourceDBInstanceIdentifier = b.SourceDbInstanceIdentifier
	in.SourceDbiResourceId = b.SourceDbiResourceId
	in.SourceDBInstanceAutomatedBackupsArn = b.SourceDbInstanceAutomatedBackupsArn
	in.UseLatestRestorableTime = b.UseLatestRestorableTime
	if b.RestoreTime != nil {
		t, err := time.Parse(time.RFC3339, *b.RestoreTime)
		if err != nil {
			return err
		}
		in.RestoreTime = aws.Time(t)
	}
	return nil
}
