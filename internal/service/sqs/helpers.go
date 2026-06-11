package sqs

import (
	"context"
	"errors"
	"fmt"
	"slices"

	sqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

// newClient returns the AWS SDK Go v2 client for sqs, configured from cfg.
// cfg is the *awscfg.Configuration the runtime hands every lifecycle method;
// the helper unwraps it and builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg any) (*sqs.Client, error) {
	c, ok := cfg.(*awscfg.Configuration)
	if !ok {
		return nil, fmt.Errorf("sqsclient: unexpected configuration type %T", cfg)
	}
	awsCfg, err := awscfg.Load(ctx, c)
	if err != nil {
		return nil, err
	}
	return sqs.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is an AWS API error whose service code is one
// of codes. SQS reports a missing queue with a service code reaching the caller
// as a smithy.APIError (QueueDoesNotExist over JSON, the older
// AWS.SimpleQueueService.NonExistentQueue over query), so a resource Read
// matches the code to turn a read of a gone queue into runtime.ErrNotFound.
func isNotFound(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return slices.Contains(codes, apiErr.ErrorCode())
	}
	return false
}
