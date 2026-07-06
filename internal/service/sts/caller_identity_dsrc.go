package sts

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// CallerIdentityDataSource resolves the account, ARN, and user id of the credentials the
// runtime is configured with, via a single GetCallerIdentity call. The call
// takes no parameters, so the data source has no inputs. It describes the
// caller's own identity and cannot be absent, so there is no not-found path,
// waiter, or retry beyond the SDK's defaults.
type CallerIdentityDataSource struct{}

// CallerIdentityDataSourceOutput holds the three values GetCallerIdentity returns. The
// account key follows the library's kebab-of-SDK-field rule on the SDK Account
// field, so it is account rather than account-id. None of the three is treated
// as a secret.
type CallerIdentityDataSourceOutput struct {
	Account string `ub:"account"`
	Arn     string `ub:"arn"`
	UserId  string `ub:"user-id"`
}

// Read calls GetCallerIdentity and maps the response to the output struct. Any
// error propagates wrapped; there is no value to find and so no not-found case.
func (d *CallerIdentityDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*CallerIdentityDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("get caller identity: %w", err)
	}
	return &CallerIdentityDataSourceOutput{
		Account: aws.ToString(out.Account),
		Arn:     aws.ToString(out.Arn),
		UserId:  aws.ToString(out.UserId),
	}, nil
}
