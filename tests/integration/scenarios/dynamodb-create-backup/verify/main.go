package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const tableName = "unobin-it-create-backup"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err == nil {
		err = verify(ctx, dynamodb.NewFromConfig(cfg), os.Getenv("VERIFY_PHASE"))
	}
	if err != nil {
		log.Fatal(err)
	}
}

func verify(ctx context.Context, client *dynamodb.Client, phase string) error {
	if phase != "applied" && phase != "updated" && phase != "destroyed" {
		return fmt.Errorf("unexpected phase %q", phase)
	}
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(tableName)})
	var missing *types.ResourceNotFoundException
	if phase == "destroyed" {
		if !errors.As(err, &missing) {
			return fmt.Errorf("table still exists or read failed: %v", err)
		}
	} else if err != nil {
		return err
	}
	request := &dynamodb.ListBackupsInput{
		TableName: aws.String(tableName), BackupType: types.BackupTypeFilterUser,
	}
	var names []string
	for {
		page, err := client.ListBackups(ctx, request)
		if err != nil {
			return err
		}
		for _, summary := range page.BackupSummaries {
			if summary.BackupStatus == types.BackupStatusDeleted {
				continue
			}
			if aws.ToString(summary.BackupArn) == "" {
				return errors.New("backup has no ARN")
			}
			out, err := client.DescribeBackup(ctx, &dynamodb.DescribeBackupInput{
				BackupArn: summary.BackupArn,
			})
			if err != nil {
				return err
			}
			if out.BackupDescription == nil || out.BackupDescription.BackupDetails == nil {
				return errors.New("backup has no details")
			}
			details := out.BackupDescription.BackupDetails
			if details.BackupStatus != types.BackupStatusAvailable ||
				aws.ToString(details.BackupArn) != aws.ToString(summary.BackupArn) ||
				details.BackupCreationDateTime == nil {
				return fmt.Errorf("backup is incomplete: %+v", details)
			}
			names = append(names, aws.ToString(details.BackupName))
		}
		if aws.ToString(page.LastEvaluatedBackupArn) == "" {
			break
		}
		request.ExclusiveStartBackupArn = page.LastEvaluatedBackupArn
	}
	var want []string
	if phase == "applied" || phase == "updated" {
		want = append(want, "unobin-it-backup-applied")
	}
	if phase == "updated" {
		want = append(want, "unobin-it-backup-updated")
	}
	slices.Sort(names)
	if !slices.Equal(want, names) {
		return fmt.Errorf("%s backup names %v, expected %v", phase, names, want)
	}
	fmt.Printf("ok: %s backups %v\n", phase, names)
	return nil
}
