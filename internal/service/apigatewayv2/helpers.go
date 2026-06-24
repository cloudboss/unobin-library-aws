package apigatewayv2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin/pkg/awscfg"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for API Gateway v2, configured
// from cfg. It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*apigatewayv2.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return apigatewayv2.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is the API Gateway v2 NotFoundException,
// the typed error the service returns for a missing resource.
func isNotFound(err error) bool {
	var nf *apigatewayv2types.NotFoundException
	return errors.As(err, &nf)
}

// withConflictRetry runs one API Gateway v2 call, retrying the service's
// transient concurrent-modification conflict. Writes to one API from
// different steps of an apply, or from an outside actor, can race; the
// conflict clears in well under the default retry interval, so attempts run
// a second apart. Every resource of the package wraps its mutating calls in
// this, the equivalent of the retry the service's clients install globally.
func withConflictRetry(ctx context.Context, fn func(context.Context) error) error {
	return retry.OnError(ctx, conflictRetryable, fn, retry.WithInterval(time.Second))
}

// conflictRetryable reports whether err is the transient conflict API
// Gateway v2 raises when two mutations race: a ConflictException asking the
// caller to try again later. The service sometimes reports it as a generic
// operation error rather than the typed exception, so the canonical message
// ("Unable to complete operation due to concurrent modification. Please try
// again later.") is matched as well. A name-collision ConflictException
// contains neither phrase and is not retried.
func conflictRetryable(err error) bool {
	if err == nil {
		return false
	}
	var conflict *apigatewayv2types.ConflictException
	if errors.As(err, &conflict) {
		return strings.Contains(conflict.ErrorMessage(), "try again later")
	}
	return strings.Contains(err.Error(), "concurrent modification") &&
		strings.Contains(err.Error(), "try again later")
}

// syncResourceTags reconciles the tags on the API Gateway v2 resource
// identified by arn with the desired set, reading the live tags with GetTags
// and applying the difference with TagResource and UntagResource. The
// taggable resources of the service (APIs and stages) share these three
// calls, keyed by ARN.
func syncResourceTags(
	ctx context.Context, client *apigatewayv2.Client, arn string, desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.GetTags(ctx, &apigatewayv2.GetTagsInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("get tags: %w", err)
			}
			return resp.Tags, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &apigatewayv2.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        upsert,
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &apigatewayv2.UntagResourceInput{
				ResourceArn: aws.String(arn),
				TagKeys:     remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}
