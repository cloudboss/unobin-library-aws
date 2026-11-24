package dynamodbbackup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
)

type CleanupResource struct {
	TableArn  string `ub:"table-arn"`
	TableName string `ub:"table-name"`
}

type CleanupOutput struct {
	TableName string `ub:"table-name"`
}

type BackupCheckDataSource struct {
	BackupArn              string `ub:"backup-arn"`
	BackupCreationDateTime string `ub:"backup-creation-date-time"`
	BackupSizeBytes        int64  `ub:"backup-size-bytes"`
}

type BackupCheckOutput struct {
	Verified bool `ub:"verified"`
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "backup-fixture", Configuration: config.LibraryConfiguration(),
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI: "1.1", SuggestedUnobinVersion: "v0.13.0-a.4",
		},
		Resources: map[string]runtime.ResourceRegistration{
			"cleanup": runtime.MakeResource[
				CleanupResource, *CleanupOutput, *awscfg.Configuration,
			](runtime.ResourceDefinition[CleanupResource, *CleanupOutput, *awscfg.Configuration]{
				SchemaVersion: 1,
			}),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"backup-check": runtime.MakeDataSource[
				BackupCheckDataSource, *BackupCheckOutput, *awscfg.Configuration,
			](),
		},
	}
}

func (r *CleanupResource) Create(context.Context, *awscfg.Configuration) (*CleanupOutput, error) {
	return &CleanupOutput{TableName: r.TableName}, nil
}

func (r *CleanupResource) Read(context.Context, *awscfg.Configuration,
	runtime.Prior[CleanupResource, *CleanupOutput, *awscfg.Configuration],
) (*CleanupOutput, error) {
	return &CleanupOutput{TableName: r.TableName}, nil
}

func (r *CleanupResource) Update(context.Context, *awscfg.Configuration,
	runtime.Prior[CleanupResource, *CleanupOutput, *awscfg.Configuration],
) (*CleanupOutput, error) {
	return &CleanupOutput{TableName: r.TableName}, nil
}

func (r *CleanupResource) Delete(ctx context.Context, cfg *awscfg.Configuration,
	prior runtime.Prior[CleanupResource, *CleanupOutput, *awscfg.Configuration],
) error {
	sdkConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client := dynamodb.NewFromConfig(sdkConfig)
	request := &dynamodb.ListBackupsInput{
		TableName: aws.String(prior.Inputs.TableName), BackupType: types.BackupTypeFilterUser,
	}
	var arns []string
	for {
		page, err := client.ListBackups(ctx, request)
		if err != nil {
			return fmt.Errorf("list owned backups: %w", err)
		}
		for _, summary := range page.BackupSummaries {
			if arn := aws.ToString(summary.BackupArn); arn != "" {
				arns = append(arns, arn)
			}
		}
		if aws.ToString(page.LastEvaluatedBackupArn) == "" {
			break
		}
		request.ExclusiveStartBackupArn = page.LastEvaluatedBackupArn
	}
	var failures []error
	for _, arn := range arns {
		if err := deleteBackup(ctx, client, arn); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func deleteBackup(ctx context.Context, client *dynamodb.Client, arn string) error {
	if err := awaitBackup(ctx, client, arn, false); err != nil {
		return err
	}
	_, err := client.DeleteBackup(ctx, &dynamodb.DeleteBackupInput{BackupArn: aws.String(arn)})
	var missing *types.BackupNotFoundException
	if errors.As(err, &missing) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete owned backup %s: %w", arn, err)
	}
	return awaitBackup(ctx, client, arn, true)
}

func awaitBackup(ctx context.Context, client *dynamodb.Client, arn string, deleted bool) error {
	for {
		out, err := client.DescribeBackup(ctx, &dynamodb.DescribeBackupInput{BackupArn: aws.String(arn)})
		var missing *types.BackupNotFoundException
		if errors.As(err, &missing) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("describe owned backup %s: %w", arn, err)
		}
		if out.BackupDescription == nil || out.BackupDescription.BackupDetails == nil {
			return fmt.Errorf("describe owned backup %s: missing details", arn)
		}
		status := out.BackupDescription.BackupDetails.BackupStatus
		if status == types.BackupStatusDeleted || (!deleted && status == types.BackupStatusAvailable) {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *BackupCheckDataSource) Read(
	ctx context.Context, cfg *awscfg.Configuration,
) (*BackupCheckOutput, error) {
	sdkConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := dynamodb.NewFromConfig(sdkConfig).DescribeBackup(ctx, &dynamodb.DescribeBackupInput{
		BackupArn: aws.String(r.BackupArn),
	})
	if err != nil {
		return nil, err
	}
	if out.BackupDescription == nil || out.BackupDescription.BackupDetails == nil {
		return nil, fmt.Errorf("backup check: missing backup details")
	}
	details := out.BackupDescription.BackupDetails
	if details.BackupStatus != types.BackupStatusAvailable ||
		aws.ToString(details.BackupArn) != r.BackupArn ||
		details.BackupCreationDateTime == nil ||
		details.BackupCreationDateTime.UTC().Format(time.RFC3339) != r.BackupCreationDateTime ||
		aws.ToInt64(details.BackupSizeBytes) != r.BackupSizeBytes {
		return nil, fmt.Errorf("backup check: action outputs differ from DescribeBackup: %+v", details)
	}
	return &BackupCheckOutput{Verified: true}, nil
}
