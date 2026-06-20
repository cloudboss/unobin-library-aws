package lambda

import (
	"context"
	"errors"
	"strings"

	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for lambda, configured from
// cfg. It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*lambda.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return lambda.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is Lambda's ResourceNotFound error. Lambda
// models each failure as its own error type, so a resource Read matches the
// type to turn a read of a gone function or policy statement into
// runtime.ErrNotFound. This is the same condition the Terraform provider
// tests with its typed error check.
func isNotFound(err error) bool {
	var notFound *lambdatypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

// isResourceConflict reports whether err is Lambda's ResourceConflict error.
// Lambda raises it while a function is still settling after a create or an
// update, and while two writes to one function race. It clears on its own,
// so a caller retries the operation.
func isResourceConflict(err error) bool {
	var conflict *lambdatypes.ResourceConflictException
	return errors.As(err, &conflict)
}

// region returns the region the client is configured for. A resource reads
// it to decide partition-specific behavior, such as whether the Signer
// service that backs code signing is available where the function lives.
func region(client *lambda.Client) string {
	return client.Options().Region
}

// functionRetryMessages are the substrings Lambda puts in an
// InvalidParameterValueException that mark a self-clearing failure rather than
// a real one. A function's create or configuration update is rejected with one
// of these while a dependency it names is still settling: the execution role
// just created and not yet assumable or not yet granted its permissions, the
// VPC plumbing still being set up so EC2 throttles the call, or the KMS key for
// the environment variables not yet usable for a grant. Each clears on its own,
// so the call is retried.
var functionRetryMessages = []string{
	"The role defined for the function cannot be assumed by Lambda",
	"The provided execution role does not have permissions",
	"throttled by EC2",
	"Lambda was unable to configure access to your environment variables " +
		"because the KMS key is invalid for CreateGrant",
}

// isFunctionRetryable reports whether err is one a function create or
// configuration update should be retried through: a resource conflict while the
// function is still settling, or an invalid-parameter error whose message marks
// a dependency that has not yet propagated. A plain invalid-parameter error
// with no recognized message is a real validation failure and is not retried,
// so a genuine mistake fails at once rather than after the full window.
func isFunctionRetryable(err error) bool {
	if isResourceConflict(err) {
		return true
	}
	var invalid *lambdatypes.InvalidParameterValueException
	if !errors.As(err, &invalid) {
		return false
	}
	msg := invalid.ErrorMessage()
	for _, m := range functionRetryMessages {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// isPublishInProgress reports whether err is the conflict PublishVersion
// returns while an update to the same function is still in progress. Lambda
// reports it as a resource conflict whose message says an update is in
// progress; a publish that races a just-finished code or configuration update
// hits it and is retried once the update settles.
func isPublishInProgress(err error) bool {
	return isResourceConflict(err) && strings.Contains(err.Error(), "in progress")
}
