package eks

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

// ClusterAuthAction generates an EKS authentication token by locally signing an STS
// GetCallerIdentity request with the library's AWS credentials and STS endpoint configuration.
// It does not look up the cluster or send the signed request. Credential providers may make
// network requests while resolving credentials.
//
// Set @trigger: 'always' to generate a token on each apply. Otherwise, unchanged inputs or an
// unchanged explicit trigger can reuse the previous output. Planning and destruction do not
// generate tokens. There is no automatic token refresh.
//
// The token output is sensitive and is masked in command output. It persists in ordinary
// action state. Use encrypted state, such as env-key or kms, to protect the stored token;
// sensitivity alone does not encrypt it.
type ClusterAuthAction struct {
	// Name is the required cluster name included in the signed x-k8s-aws-id header.
	// Token generation does not check whether the cluster exists.
	Name string `ub:"name"`

	// Region optionally overrides the library's AWS region within the configured partition.
	// Omitted, null, and empty values use the library's region.
	Region *string `ub:"region"`
}

// ClusterAuthActionOutput contains the generated authentication token.
type ClusterAuthActionOutput struct {
	// Token is sensitive and is masked in command output. It persists in ordinary action state.
	// Use encrypted state, such as env-key or kms, to protect the stored token; sensitivity alone
	// does not encrypt it.
	Token string `ub:"token,sensitive"`
}

func (r ClusterAuthAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Expr(
			"input.name != null", constraint.Bind("name", r.Name),
		)).Message("name is required"),
	}
}

func (r *ClusterAuthAction) Run(
	ctx context.Context, cfg *awsCfg,
) (*ClusterAuthActionOutput, error) {
	sdkConfig, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration for EKS cluster authentication: %w", err)
	}
	signed, err := r.prepareRequest(ctx, sdkConfig, cfg)
	if err != nil {
		return nil, err
	}
	return &ClusterAuthActionOutput{
		Token: "k8s-aws-v1." + base64.RawURLEncoding.EncodeToString([]byte(signed.URL)),
	}, nil
}

func (r *ClusterAuthAction) prepareRequest(
	ctx context.Context, sdkConfig aws.Config, cfg *awsCfg, options ...func(*sts.PresignOptions),
) (*v4.PresignedHTTPRequest, error) {
	if region := aws.ToString(r.Region); region != "" {
		if sdkConfig.Region != "" {
			partition := func(value string) string {
				found, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), value)
				if ok {
					return found.ID()
				}
				return "aws"
			}
			configured := partition(sdkConfig.Region)
			if selected := partition(region); selected != configured {
				return nil, fmt.Errorf("region %q belongs to partition %q, expected %q",
					region, selected, configured)
			}
		}
		sdkConfig.Region = region
	}
	client := sts.NewFromConfig(sdkConfig, func(o *sts.Options) {
		if endpoint := cfg.STSEndpoint(); endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.APIOptions = append(o.APIOptions, func(stack *middleware.Stack) error {
			return stack.Build.Add(middleware.BuildMiddlewareFunc("EKSClusterAuth",
				func(ctx context.Context, in middleware.BuildInput, next middleware.BuildHandler) (
					middleware.BuildOutput, middleware.Metadata, error,
				) {
					request := in.Request.(*smithyhttp.Request)
					request.Header.Set("x-k8s-aws-id", r.Name)
					query := request.URL.Query()
					query.Set("X-Amz-Expires", "0")
					request.URL.RawQuery = query.Encode()
					return next.HandleBuild(ctx, in)
				}), middleware.After)
		})
	})
	signed, err := sts.NewPresignClient(client, options...).PresignGetCallerIdentity(
		ctx, &sts.GetCallerIdentityInput{},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"presign STS GetCallerIdentity for EKS cluster authentication: %w", err,
		)
	}
	return signed, nil
}
