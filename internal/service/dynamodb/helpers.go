package dynamodb

import (
	"context"
	"errors"
	"strings"

	dynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for DynamoDB, configured from cfg.
// It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*dynamodb.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return dynamodb.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is DynamoDB's ResourceNotFoundException, the
// error a describe or update of a gone table or index returns. A resource Read
// matches the type to turn it into runtime.ErrNotFound. This is a different
// type than TableNotFoundException, which only the continuous-backups read can
// return for an archived table.
func isNotFound(err error) bool {
	var notFound *dynamodbtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

// isTableNotFound reports whether err is DynamoDB's TableNotFoundException, a
// distinct type from ResourceNotFoundException that DescribeContinuousBackups
// returns for an archived table. The point-in-time-recovery side read swallows
// it and leaves recovery reported as disabled.
func isTableNotFound(err error) bool {
	var notFound *dynamodbtypes.TableNotFoundException
	return errors.As(err, &notFound)
}

// isUnknownOperation reports whether err is the UnknownOperationException some
// endpoints return for DescribeContinuousBackups, which they do not implement.
// It has no typed Go exception, so it is matched by its service error code. The
// point-in-time-recovery side read swallows it and leaves recovery disabled.
func isUnknownOperation(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "UnknownOperationException"
}

// isThrottling reports whether err is DynamoDB's ThrottlingException, a
// transient rejection a busy account returns for a table operation. The create
// and delete retry through it.
func isThrottling(err error) bool {
	var throttling *dynamodbtypes.ThrottlingException
	return errors.As(err, &throttling)
}

// isInUse reports whether err is DynamoDB's ResourceInUseException, returned
// when a delete races a table that is still creating or updating. The delete
// retries through it until the table settles enough to be removed.
func isInUse(err error) bool {
	var inUse *dynamodbtypes.ResourceInUseException
	return errors.As(err, &inUse)
}

// isSimultaneityLimit reports whether err is the LimitExceededException
// DynamoDB returns when too many tables, or too many tables with indexes, are
// being changed at once. The two messages it uses for that case are matched
// here so the unrelated daily-backup and per-account limits, which share the
// type, are not retried forever. The create retries through it.
func isSimultaneityLimit(err error) bool {
	var limit *dynamodbtypes.LimitExceededException
	if !errors.As(err, &limit) {
		return false
	}
	msg := limit.ErrorMessage()
	return strings.Contains(msg, "can be created, updated, or deleted simultaneously") ||
		strings.Contains(msg, "indexed tables that can be created simultaneously")
}

// isContinuousBackupsUnavailable reports whether err is the
// ContinuousBackupsUnavailableException DynamoDB returns while it is still
// enabling backups for a table. Enabling point-in-time recovery retries
// through it.
func isContinuousBackupsUnavailable(err error) bool {
	var unavailable *dynamodbtypes.ContinuousBackupsUnavailableException
	return errors.As(err, &unavailable)
}

// createRetryable reports whether a CreateTable error is one that clears on its
// own: throttling, or the simultaneity limit on concurrent table or index
// operations.
func createRetryable(err error) bool {
	return isThrottling(err) || isSimultaneityLimit(err)
}

// deleteRetryable reports whether a DeleteTable error is one that clears on its
// own: throttling, the simultaneity limit, or the table still being in use by a
// create or update that has not finished.
func deleteRetryable(err error) bool {
	return isThrottling(err) || isSimultaneityLimit(err) || isInUse(err)
}
