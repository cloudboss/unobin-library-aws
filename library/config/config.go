// Package config contains the AWS module configuration plus a
// helper that converts it to an aws.Config for the SDK.
package config

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdkconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	Region          *cfg.String
	Profile         *cfg.String
	AccessKeyId     *cfg.String
	SecretAccessKey *cfg.String
	SessionToken    *cfg.String
	EndpointURL     *cfg.String
	MaxAttempts     *cfg.Integer
	AssumeRole      *AssumeRole
}

type AssumeRole struct {
	RoleArn         cfg.String
	RoleSessionName *cfg.String
	ExternalId      *cfg.String
	DurationSeconds *cfg.Integer
	Policy          *cfg.String
	SourceIdentity  *cfg.String
}

// LoadAWSConfig builds an aws.Config from c. Region, profile, static
// credentials, endpoint URL, and max-attempts feed into
// awssdk config.LoadDefaultConfig. AssumeRole is recognized in the
// Configuration today but not applied -- assume-role support is a V1
// follow-up.
func LoadAWSConfig(ctx context.Context, c *Configuration) (aws.Config, error) {
	if c == nil {
		return sdkconfig.LoadDefaultConfig(ctx)
	}
	opts := []func(*sdkconfig.LoadOptions) error{}
	if v := stringValue(c.Region); v != "" {
		opts = append(opts, sdkconfig.WithRegion(v))
	}
	if v := stringValue(c.Profile); v != "" {
		opts = append(opts, sdkconfig.WithSharedConfigProfile(v))
	}
	if hasStaticCredentials(c) {
		opts = append(opts, sdkconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				stringValue(c.AccessKeyId),
				stringValue(c.SecretAccessKey),
				stringValue(c.SessionToken),
			),
		))
	}
	if v := stringValue(c.EndpointURL); v != "" {
		opts = append(opts, sdkconfig.WithBaseEndpoint(v))
	}
	if c.MaxAttempts != nil && c.MaxAttempts.Value > 0 {
		opts = append(opts, sdkconfig.WithRetryMaxAttempts(int(c.MaxAttempts.Value)))
	}
	return sdkconfig.LoadDefaultConfig(ctx, opts...)
}

func hasStaticCredentials(c *Configuration) bool {
	return stringValue(c.AccessKeyId) != "" && stringValue(c.SecretAccessKey) != ""
}

func stringValue(p *cfg.String) string {
	if p == nil {
		return ""
	}
	return p.Value
}
