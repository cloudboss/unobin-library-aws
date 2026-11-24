package dynamodb

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

type CreateBackupAction struct {
	TableName  string  `ub:"table-name"`
	BackupName *string `ub:"backup-name"`
	Timeout    *int64  `ub:"timeout"`
	Region     *string `ub:"region"`
}

type CreateBackupActionOutput struct {
	BackupArn              string `ub:"backup-arn"`
	BackupCreationDateTime string `ub:"backup-creation-date-time"`
	BackupSizeBytes        int64  `ub:"backup-size-bytes"`
}

type backupClient interface {
	CreateBackup(context.Context, *dynamodb.CreateBackupInput,
		...func(*dynamodb.Options)) (*dynamodb.CreateBackupOutput, error)
	DescribeBackup(context.Context, *dynamodb.DescribeBackupInput,
		...func(*dynamodb.Options)) (*dynamodb.DescribeBackupOutput, error)
}

type backupClock struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func (r CreateBackupAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Expr(
			"input.table-name != null", constraint.Bind("table-name", r.TableName),
		)).Message("table-name is required"),
		constraint.Must(
			constraint.MinItems(r.TableName, 1),
			constraint.MaxItems(r.TableName, 1024),
		).Message("table-name must contain between 1 and 1024 characters"),
		constraint.Must(
			constraint.MinItems(r.BackupName, 3),
			constraint.MaxItems(r.BackupName, 255),
			constraint.IsMatch(r.BackupName, `^[a-zA-Z0-9_.-]+$`),
		).Message("backup-name must contain 3 to 255 ASCII letters, digits, " +
			"underscores, dots or hyphens"),
	}
}

func (r *CreateBackupAction) Run(
	ctx context.Context, cfg *awsCfg,
) (*CreateBackupActionOutput, error) {
	sdkConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if region := aws.ToString(r.Region); region != "" {
		if sdkConfig.Region != "" && backupRegionPartition(region) !=
			backupRegionPartition(sdkConfig.Region) {
			return nil, fmt.Errorf("backup region %q is outside configured partition %q",
				region, backupRegionPartition(sdkConfig.Region))
		}
		sdkConfig.Region = region
	}
	return r.runBackup(ctx, dynamodb.NewFromConfig(sdkConfig), backupClock{
		now: time.Now, sleep: sleepForBackup,
	})
}

func (r *CreateBackupAction) prepareBackupRequest() (*dynamodb.CreateBackupInput, time.Duration) {
	name := r.BackupName
	if name == nil {
		name = aws.String(r.TableName + "-backup-terraform-" + rand.Text())
	}
	timeout := 600 * time.Second
	if r.Timeout != nil {
		timeout = time.Duration(*r.Timeout) * time.Second
	}
	return &dynamodb.CreateBackupInput{
		TableName: aws.String(r.TableName), BackupName: name,
	}, timeout
}

func (r *CreateBackupAction) runBackup(
	ctx context.Context, client backupClient, clock backupClock,
) (*CreateBackupActionOutput, error) {
	request, timeout := r.prepareBackupRequest()
	created, err := createBackupWithRetry(ctx, client, request, timeout, clock)
	if err != nil {
		return nil, fmt.Errorf("create backup for table %s: %w", r.TableName, err)
	}
	if created == nil || created.BackupDetails == nil ||
		aws.ToString(created.BackupDetails.BackupArn) == "" {
		return nil, fmt.Errorf("create backup for table %s: response has no backup ARN", r.TableName)
	}
	arn := aws.ToString(created.BackupDetails.BackupArn)
	if timeout <= 0 {
		return nil, fmt.Errorf("wait for backup %s: timeout must be positive", arn)
	}
	return waitForBackup(ctx, client, arn, timeout, clock)
}

func createBackupWithRetry(
	ctx context.Context, client backupClient, request *dynamodb.CreateBackupInput,
	timeout time.Duration, clock backupClock,
) (*dynamodb.CreateBackupOutput, error) {
	deadline := clock.now().Add(timeout)
	delay := 500 * time.Millisecond
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, err := client.CreateBackup(ctx, request)
	grace := false
	for err != nil && backupCreationRetryable(err) {
		if !clock.now().Before(deadline) {
			if grace {
				break
			}
			grace = true
		}
		if err := clock.sleep(ctx, delay); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out, err = client.CreateBackup(ctx, request)
		delay = min(2*delay, 10*time.Second)
	}
	return out, err
}

func backupCreationRetryable(err error) bool {
	var inUse *types.BackupInUseException
	var limit *types.LimitExceededException
	var unavailable *types.ContinuousBackupsUnavailableException
	return errors.As(err, &inUse) || errors.As(err, &limit) ||
		(errors.As(err, &unavailable) &&
			strings.Contains(unavailable.ErrorMessage(), "Backups are being enabled"))
}

func waitForBackup(
	ctx context.Context, client backupClient, arn string, timeout time.Duration, clock backupClock,
) (*CreateBackupActionOutput, error) {
	deadline := clock.now().Add(timeout)
	var delay time.Duration
	var lastProgress time.Time
	nextInterval := func(attempt int) time.Duration {
		if attempt == 0 {
			return 0
		}
		delay = min(max(2*delay, 500*time.Millisecond), 10*time.Second)
		return delay
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for backup %s: %w", arn, err)
		}
		if clock.now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for backup %s to be AVAILABLE", arn)
		}
		out, err := client.DescribeBackup(ctx, &dynamodb.DescribeBackupInput{
			BackupArn: aws.String(arn),
		})
		if err != nil {
			return nil, fmt.Errorf("describe backup %s: %w", arn, err)
		}
		if out == nil || out.BackupDescription == nil || out.BackupDescription.BackupDetails == nil {
			return nil, fmt.Errorf("describe backup %s: response has no backup details", arn)
		}
		details := out.BackupDescription.BackupDetails
		switch details.BackupStatus {
		case types.BackupStatusAvailable:
			if aws.ToString(details.BackupArn) == "" || details.BackupCreationDateTime == nil {
				return nil, fmt.Errorf("describe backup %s: response has no backup ARN or creation time", arn)
			}
			return &CreateBackupActionOutput{
				BackupArn:              aws.ToString(details.BackupArn),
				BackupCreationDateTime: details.BackupCreationDateTime.UTC().Format(time.RFC3339),
				BackupSizeBytes:        aws.ToInt64(details.BackupSizeBytes),
			}, nil
		case types.BackupStatusCreating:
		case types.BackupStatusDeleted:
			return nil, fmt.Errorf("backup %s failed with status DELETED", arn)
		default:
			return nil, fmt.Errorf("backup %s has unexpected status %q", arn, details.BackupStatus)
		}
		if now := clock.now(); attempt == 0 || now.Sub(lastProgress) >= 30*time.Second {
			// Progress metadata advances the same delay used for polling.
			nextInterval(attempt)
			lastProgress = now
		}
		if err := clock.sleep(ctx, nextInterval(attempt)); err != nil {
			return nil, fmt.Errorf("wait for backup %s: %w", arn, err)
		}
	}
}

func sleepForBackup(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func backupRegionPartition(region string) string {
	partition, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), region)
	if ok {
		return partition.ID()
	}
	return "aws"
}
