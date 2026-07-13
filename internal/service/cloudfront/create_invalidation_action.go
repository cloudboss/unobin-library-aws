package cloudfront

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/cloudboss/unobin/pkg/constraint"
)

const invalidationPollInterval = 30 * time.Second

var distributionIDPattern = regexp.MustCompile(`^[A-Z0-9]+$`)

type invalidationClient interface {
	GetDistribution(
		context.Context,
		*cloudfront.GetDistributionInput,
		...func(*cloudfront.Options),
	) (*cloudfront.GetDistributionOutput, error)
	CreateInvalidation(
		context.Context,
		*cloudfront.CreateInvalidationInput,
		...func(*cloudfront.Options),
	) (*cloudfront.CreateInvalidationOutput, error)
	GetInvalidation(
		context.Context,
		*cloudfront.GetInvalidationInput,
		...func(*cloudfront.Options),
	) (*cloudfront.GetInvalidationOutput, error)
}

type invalidationSleep func(context.Context, time.Duration) error

// CreateInvalidationAction requests and waits for a CloudFront invalidation.
type CreateInvalidationAction struct {
	DistributionId  string   `ub:"distribution-id"`
	Paths           []string `ub:"paths"`
	CallerReference *string  `ub:"caller-reference"`
}

// CreateInvalidationActionOutput identifies a completed invalidation.
type CreateInvalidationActionOutput struct {
	Id     string `ub:"id"`
	Status string `ub:"status"`
}

func (r CreateInvalidationAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(
			constraint.MinItems(r.Paths, 1),
			constraint.MaxItems(r.Paths, 3000),
		).Message("paths must contain between 1 and 3000 values"),
		constraint.ForEach(r.Paths, func(path string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.NotEmpty(path)).
					Message("paths values must not be empty"),
			}
		}),
		constraint.When(constraint.Present(r.CallerReference)).
			Require(constraint.MaxItems(r.CallerReference, 128)).
			Message("caller-reference must contain at most 128 characters"),
	}
}

func (r *CreateInvalidationAction) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*CreateInvalidationActionOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.run(ctx, client, sleepForInvalidation)
}

func (r *CreateInvalidationAction) validate() error {
	if !distributionIDPattern.MatchString(r.DistributionId) {
		return fmt.Errorf(
			"distribution-id must contain only uppercase ASCII letters and digits",
		)
	}
	for i, path := range r.Paths {
		if path == "" {
			return fmt.Errorf("paths[%d] must not be empty", i)
		}
		if path != "*" && !strings.HasPrefix(path, "/") {
			return fmt.Errorf("paths[%d] must be * or begin with /", i)
		}
	}
	return nil
}

func (r *CreateInvalidationAction) run(
	ctx context.Context,
	client invalidationClient,
	sleep invalidationSleep,
) (*CreateInvalidationActionOutput, error) {
	distribution, err := client.GetDistribution(ctx, &cloudfront.GetDistributionInput{
		Id: aws.String(r.DistributionId),
	})
	if err != nil {
		var notFound *cloudfronttypes.NoSuchDistribution
		if errors.As(err, &notFound) {
			return nil, fmt.Errorf("distribution %s not found: %w", r.DistributionId, err)
		}
		return nil, fmt.Errorf("get distribution %s: %w", r.DistributionId, err)
	}
	if distribution == nil ||
		distribution.Distribution == nil ||
		distribution.Distribution.DistributionConfig == nil {
		return nil, fmt.Errorf("get distribution %s: response has no distribution", r.DistributionId)
	}

	callerReference := aws.ToString(r.CallerReference)
	if callerReference == "" {
		callerReference = rand.Text()
	}
	paths := append([]string(nil), r.Paths...)
	created, err := client.CreateInvalidation(ctx, &cloudfront.CreateInvalidationInput{
		DistributionId: aws.String(r.DistributionId),
		InvalidationBatch: &cloudfronttypes.InvalidationBatch{
			CallerReference: aws.String(callerReference),
			Paths: &cloudfronttypes.Paths{
				Items:    paths,
				Quantity: aws.Int32(int32(len(paths))),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create invalidation for distribution %s: %w", r.DistributionId, err)
	}
	if created == nil || created.Invalidation == nil {
		return nil, fmt.Errorf(
			"create invalidation for distribution %s: response has no invalidation",
			r.DistributionId,
		)
	}
	invalidationID := aws.ToString(created.Invalidation.Id)
	if invalidationID == "" {
		return nil, fmt.Errorf(
			"create invalidation for distribution %s: response has no invalidation id",
			r.DistributionId,
		)
	}
	return waitForInvalidation(ctx, client, r.DistributionId, invalidationID, sleep)
}

func waitForInvalidation(
	ctx context.Context,
	client invalidationClient,
	distributionID string,
	invalidationID string,
	sleep invalidationSleep,
) (*CreateInvalidationActionOutput, error) {
	for {
		out, err := client.GetInvalidation(ctx, &cloudfront.GetInvalidationInput{
			DistributionId: aws.String(distributionID),
			Id:             aws.String(invalidationID),
		})
		if err != nil {
			return nil, fmt.Errorf("get invalidation %s: %w", invalidationID, err)
		}
		if out == nil || out.Invalidation == nil {
			return nil, fmt.Errorf("get invalidation %s: response has no invalidation", invalidationID)
		}
		status := aws.ToString(out.Invalidation.Status)
		switch status {
		case "Completed":
			return &CreateInvalidationActionOutput{Id: invalidationID, Status: status}, nil
		case "InProgress":
			if err := sleep(ctx, invalidationPollInterval); err != nil {
				return nil, fmt.Errorf("wait for invalidation %s: %w", invalidationID, err)
			}
		default:
			return nil, fmt.Errorf("invalidation %s has unexpected status %q", invalidationID, status)
		}
	}
}

func sleepForInvalidation(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
