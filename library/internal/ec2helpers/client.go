package ec2helpers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	smithy "github.com/aws/smithy-go"

	"github.com/cloudboss/unobin-library-aws/library/config"
)

// NewClient returns the AWS SDK Go v2 client for ec2,
// configured from cfg. cfg is the *config.Configuration the runtime
// hands every lifecycle method; the helper unwraps it and builds an
// aws.Config via config.LoadAWSConfig.
func NewClient(ctx context.Context, cfg any) (*ec2.Client, error) {
	c, ok := cfg.(*config.Configuration)
	if !ok {
		return nil, fmt.Errorf("ec2client: unexpected configuration type %T", cfg)
	}
	awsCfg, err := config.LoadAWSConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	return ec2.NewFromConfig(awsCfg), nil
}

// IsNotFound reports whether err is an AWS API error whose service code
// is one of codes. EC2 reports a missing resource with a service code
// such as InvalidVpcID.NotFound carried on an HTTP 400, not a 404, so a
// resource Read matches the code to turn a describe of a gone resource
// into runtime.ErrNotFound.
func IsNotFound(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return slices.Contains(codes, apiErr.ErrorCode())
	}
	return false
}

// Region returns the region the client is configured for. A resource that
// composes an ARN needs it alongside the partition and account id.
func Region(client *ec2.Client) string {
	return client.Options().Region
}
