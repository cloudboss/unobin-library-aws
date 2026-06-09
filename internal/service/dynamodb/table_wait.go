package dynamodb

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	dynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// tableSettleRuns is how many consecutive ACTIVE reads end a wait for the table
// or its encryption to settle. DynamoDB reports status across replicas that lag
// each other, so a single ACTIVE read can come from a caught-up replica while
// the change is still in flight elsewhere; two in a row confirm it.
const tableSettleRuns = 2

// describeTable fetches the table description by name. A gone table returns nil
// with no error, which the waiters read as the absent state.
func describeTable(
	ctx context.Context, client *dynamodb.Client, name string,
) (*dynamodbtypes.TableDescription, error) {
	resp, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe table %s: %w", name, err)
	}
	return resp.Table, nil
}

// waitTableActive polls the table until it reports ACTIVE on consecutive reads,
// then returns the settled description. The description holds the ARN, stream
// ARN, and stream label DynamoDB computes, which the create response does not
// return well-formed, so the caller takes its outputs from here.
func waitTableActive(
	ctx context.Context, client *dynamodb.Client, name string,
) (*dynamodbtypes.TableDescription, error) {
	var settled *dynamodbtypes.TableDescription
	err := wait.UntilStable(ctx, fmt.Sprintf("table %s to be active", name), tableSettleRuns,
		func(ctx context.Context) (bool, error) {
			desc, err := describeTable(ctx, client, name)
			if err != nil {
				return false, err
			}
			if desc == nil {
				return false, nil
			}
			if desc.TableStatus == dynamodbtypes.TableStatusActive {
				settled = desc
				return true, nil
			}
			return false, nil
		},
		wait.WithTimeout(30*time.Minute),
	)
	if err != nil {
		return nil, err
	}
	return settled, nil
}

// waitTableDeleted polls the table until DynamoDB no longer reports it, after a
// delete. It polls quickly, since a deleted table disappears in seconds.
func waitTableDeleted(ctx context.Context, client *dynamodb.Client, name string) error {
	return wait.Until(ctx, fmt.Sprintf("table %s to be deleted", name),
		func(ctx context.Context) (bool, error) {
			desc, err := describeTable(ctx, client, name)
			if err != nil {
				return false, err
			}
			return desc == nil, nil
		},
		wait.WithTimeout(10*time.Minute),
		wait.WithInterval(time.Second),
	)
}

// waitTableWarmThroughputActive polls the table until its warm-throughput
// sub-status reports ACTIVE, after a warm-throughput change.
func waitTableWarmThroughputActive(
	ctx context.Context, client *dynamodb.Client, name string,
) error {
	return wait.Until(ctx, fmt.Sprintf("table %s warm throughput to be active", name),
		func(ctx context.Context) (bool, error) {
			desc, err := describeTable(ctx, client, name)
			if err != nil {
				return false, err
			}
			if desc == nil {
				return false, nil
			}
			warm := desc.WarmThroughput
			if warm == nil {
				return true, nil
			}
			return warm.Status == dynamodbtypes.TableStatusActive, nil
		},
		wait.WithTimeout(30*time.Minute),
	)
}

// waitGSIActive polls the table until the named global secondary index reports
// ACTIVE, after the index is created or its throughput is updated. A create
// backfills the index, which can take a long time, so the wait runs a wide
// window.
func waitGSIActive(
	ctx context.Context, client *dynamodb.Client, table, index string,
) error {
	return wait.Until(ctx,
		fmt.Sprintf("index %s on table %s to be active", index, table),
		func(ctx context.Context) (bool, error) {
			status, found, err := gsiStatus(ctx, client, table, index)
			if err != nil {
				return false, err
			}
			if !found {
				return false, nil
			}
			return status == dynamodbtypes.IndexStatusActive, nil
		},
		wait.WithTimeout(30*time.Minute),
	)
}

// waitGSIDeleted polls the table until the named global secondary index is no
// longer present, after a delete.
func waitGSIDeleted(
	ctx context.Context, client *dynamodb.Client, table, index string,
) error {
	return wait.Until(ctx,
		fmt.Sprintf("index %s on table %s to be deleted", index, table),
		func(ctx context.Context) (bool, error) {
			_, found, err := gsiStatus(ctx, client, table, index)
			if err != nil {
				return false, err
			}
			return !found, nil
		},
		wait.WithTimeout(30*time.Minute),
	)
}

// gsiStatus reads the status of one global secondary index from the table
// description, reporting whether the index was found. A gone table reports the
// index as absent.
func gsiStatus(
	ctx context.Context, client *dynamodb.Client, table, index string,
) (dynamodbtypes.IndexStatus, bool, error) {
	desc, err := describeTable(ctx, client, table)
	if err != nil {
		return "", false, err
	}
	if desc == nil {
		return "", false, nil
	}
	for _, gsi := range desc.GlobalSecondaryIndexes {
		if aws.ToString(gsi.IndexName) == index {
			return gsi.IndexStatus, true, nil
		}
	}
	return "", false, nil
}

// waitTTLUpdated polls the table's time-to-live status until it reaches the
// target, after a time-to-live change. The target is ENABLED when enabling and
// DISABLED when disabling.
func waitTTLUpdated(
	ctx context.Context, client *dynamodb.Client, name string, enabled bool,
) error {
	target := dynamodbtypes.TimeToLiveStatusDisabled
	if enabled {
		target = dynamodbtypes.TimeToLiveStatusEnabled
	}
	return wait.Until(ctx, fmt.Sprintf("table %s ttl to settle", name),
		func(ctx context.Context) (bool, error) {
			status, err := ttlStatus(ctx, client, name)
			if err != nil {
				return false, err
			}
			return status == target, nil
		},
		wait.WithTimeout(30*time.Minute),
	)
}

// ttlStatus reads the table's time-to-live status. A gone table reports the
// disabled status, the absent state.
func ttlStatus(
	ctx context.Context, client *dynamodb.Client, name string,
) (dynamodbtypes.TimeToLiveStatus, error) {
	resp, err := client.DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{
		TableName: aws.String(name),
	})
	if err != nil {
		if isNotFound(err) {
			return dynamodbtypes.TimeToLiveStatusDisabled, nil
		}
		return "", fmt.Errorf("describe time to live %s: %w", name, err)
	}
	if resp.TimeToLiveDescription == nil {
		return dynamodbtypes.TimeToLiveStatusDisabled, nil
	}
	return resp.TimeToLiveDescription.TimeToLiveStatus, nil
}

// waitPITRUpdated polls the table's point-in-time-recovery status until it
// reaches the target, after a recovery change. Enabling passes through an
// ENABLING state and a brief DISABLED read before settling, so only the final
// target ends the wait.
func waitPITRUpdated(
	ctx context.Context, client *dynamodb.Client, name string, enabled bool,
) error {
	target := dynamodbtypes.PointInTimeRecoveryStatusDisabled
	if enabled {
		target = dynamodbtypes.PointInTimeRecoveryStatusEnabled
	}
	return wait.Until(ctx, fmt.Sprintf("table %s point-in-time recovery to settle", name),
		func(ctx context.Context) (bool, error) {
			status, err := pitrStatus(ctx, client, name)
			if err != nil {
				return false, err
			}
			return status == target, nil
		},
		wait.WithTimeout(30*time.Minute),
		wait.WithInterval(15*time.Second),
	)
}

// pitrStatus reads the table's point-in-time-recovery status. A table whose
// backups are not enabled, or which is gone or archived, reports the disabled
// status, the absent state.
func pitrStatus(
	ctx context.Context, client *dynamodb.Client, name string,
) (dynamodbtypes.PointInTimeRecoveryStatus, error) {
	resp, err := client.DescribeContinuousBackups(ctx, &dynamodb.DescribeContinuousBackupsInput{
		TableName: aws.String(name),
	})
	if err != nil {
		if isNotFound(err) || isTableNotFound(err) || isUnknownOperation(err) {
			return dynamodbtypes.PointInTimeRecoveryStatusDisabled, nil
		}
		return "", fmt.Errorf("describe continuous backups %s: %w", name, err)
	}
	desc := resp.ContinuousBackupsDescription
	if desc == nil || desc.PointInTimeRecoveryDescription == nil {
		return dynamodbtypes.PointInTimeRecoveryStatusDisabled, nil
	}
	return desc.PointInTimeRecoveryDescription.PointInTimeRecoveryStatus, nil
}

// waitSSEUpdated polls the table's server-side-encryption status until it
// settles to the target on consecutive reads, after an encryption change. The
// target is ENABLED when a key is configured and DISABLED otherwise; an absent
// encryption description reads as DISABLED.
func waitSSEUpdated(
	ctx context.Context, client *dynamodb.Client, name string, enabled bool,
) error {
	target := dynamodbtypes.SSEStatusDisabled
	if enabled {
		target = dynamodbtypes.SSEStatusEnabled
	}
	return wait.UntilStable(ctx, fmt.Sprintf("table %s encryption to settle", name),
		tableSettleRuns,
		func(ctx context.Context) (bool, error) {
			status, err := sseStatus(ctx, client, name)
			if err != nil {
				return false, err
			}
			return status == target, nil
		},
		wait.WithTimeout(30*time.Minute),
	)
}

// sseStatus reads the table's server-side-encryption status. DynamoDB omits the
// encryption description for a table on the AWS owned key, which reads as
// DISABLED, the absent state. A gone table reads the same way.
func sseStatus(
	ctx context.Context, client *dynamodb.Client, name string,
) (dynamodbtypes.SSEStatus, error) {
	desc, err := describeTable(ctx, client, name)
	if err != nil {
		return "", err
	}
	if desc == nil || desc.SSEDescription == nil {
		return dynamodbtypes.SSEStatusDisabled, nil
	}
	return desc.SSEDescription.Status, nil
}
