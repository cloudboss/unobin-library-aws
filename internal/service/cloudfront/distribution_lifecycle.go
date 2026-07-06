package cloudfront

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// statusDeployed is the distribution status in which CloudFront has propagated
// the configuration to every edge location. A create or update is not settled
// until the distribution reaches it.
const statusDeployed = "Deployed"

// deployTimeout bounds the Deployed waiter. CloudFront propagation usually
// settles in fifteen to thirty minutes but can take longer, so the budget is a
// generous ninety minutes.
const deployTimeout = 90 * time.Minute

// deployInterval is how long the Deployed waiter sleeps between polls. A
// thirty-second interval keeps it from hammering the API across the long
// propagation window.
const deployInterval = 30 * time.Second

// deleteWaitTimeout bounds the wait for a deleted distribution to disappear.
const deleteWaitTimeout = 90 * time.Minute

// deleteWaitInterval is how long the delete waiter sleeps between polls. The
// distribution is already gone or nearly so, so a fifteen-second interval
// confirms it quickly.
const deleteWaitInterval = 15 * time.Second

// disableRetryTimeout bounds the loop that retries a delete after disabling a
// distribution that CloudFront still reported as enabled.
const disableRetryTimeout = 3 * time.Minute

// etagRetryTimeout bounds the loop that retries a delete whose IfMatch token
// went stale, re-fetching the ETag each try.
const etagRetryTimeout = time.Minute

// cloudFrontHostedZoneID is the fixed Route 53 hosted zone for CloudFront
// distributions in the standard partition. An alias record pointing at a
// distribution uses it as the alias target's zone id.
const cloudFrontHostedZoneID = "Z2FDTNDATAQYW2"

// cloudFrontHostedZoneIDChina is the fixed CloudFront hosted zone in the China
// partition, which differs from the standard one.
const cloudFrontHostedZoneIDChina = "Z3RFFRIM2A3IF5"

// loggingBucketGoneMessage is the InvalidArgument message CloudFront returns
// when a distribution's configured logging bucket no longer exists. During a
// disable, that should not block removal, so the disable nulls the logging
// config and retries when it sees this message.
const loggingBucketGoneMessage = "The S3 bucket that you specified for CloudFront logs " +
	"doesn't exist"

func (r *DistributionResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *DistributionResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.Id
	// CloudFront refuses to delete an enabled or still-deploying distribution,
	// so disable it first and wait for that to propagate.
	if err := disableDistribution(ctx, client, id); err != nil {
		if isDistributionNotFound(err) {
			return nil
		}
		return err
	}
	return deleteDistribution(ctx, client, id)
}

// disableDistribution sets a distribution's enabled flag to false and waits for
// the change to deploy, working from the distribution's current cloud
// configuration rather than the local one so it disables exactly what is live.
// A distribution already disabled needs no change. A distribution mid-deploy is
// waited out first, since an update is rejected while one is in progress. If the
// disabling update fails because the configured logging bucket is gone, the
// logging config is nulled and the update retried, since a missing log bucket
// must not block removal.
func disableDistribution(ctx context.Context, client *cloudfront.Client, id string) error {
	resp, err := client.GetDistributionConfig(ctx, &cloudfront.GetDistributionConfigInput{
		Id: aws.String(id),
	})
	if err != nil {
		if isDistributionNotFound(err) {
			return err
		}
		return fmt.Errorf("get distribution config %s: %w", id, err)
	}
	config := resp.DistributionConfig
	if config == nil {
		return nil
	}
	if !aws.ToBool(config.Enabled) {
		return nil
	}
	// A distribution mid-deploy rejects an update, so wait for it to settle
	// first. Waiting can rotate the ETag, so re-fetch the version token right
	// before the disabling update.
	if err := waitDistributionDeployed(ctx, client, id); err != nil {
		return err
	}
	etag, err := distributionETag(ctx, client, id)
	if err != nil {
		if isDistributionNotFound(err) {
			return err
		}
		return fmt.Errorf("get distribution etag %s: %w", id, err)
	}
	config.Enabled = aws.Bool(false)
	if err := applyDisable(ctx, client, id, etag, config); err != nil {
		return err
	}
	return waitDistributionDeployed(ctx, client, id)
}

// applyDisable sends the disabling update and absorbs the missing-log-bucket
// case: if CloudFront rejects the update because the logging bucket is gone, the
// logging config is nulled and the update retried with a fresh ETag.
func applyDisable(
	ctx context.Context, client *cloudfront.Client, id, etag string,
	config *cloudfronttypes.DistributionConfig,
) error {
	_, err := client.UpdateDistribution(ctx, &cloudfront.UpdateDistributionInput{
		Id:                 aws.String(id),
		DistributionConfig: config,
		IfMatch:            aws.String(etag),
	})
	if err == nil {
		return nil
	}
	if isLoggingBucketGone(err) {
		config.Logging = &cloudfronttypes.LoggingConfig{
			Enabled:        aws.Bool(false),
			IncludeCookies: aws.Bool(false),
			Bucket:         aws.String(""),
			Prefix:         aws.String(""),
		}
		fresh, freshErr := distributionETag(ctx, client, id)
		if freshErr != nil {
			return freshErr
		}
		_, err = client.UpdateDistribution(ctx, &cloudfront.UpdateDistributionInput{
			Id:                 aws.String(id),
			DistributionConfig: config,
			IfMatch:            aws.String(fresh),
		})
	}
	if err != nil {
		return fmt.Errorf("disable distribution %s: %w", id, err)
	}
	return nil
}

// deleteDistribution removes a disabled distribution and waits for it to
// disappear. It handles the windows CloudFront opens during a delete: a
// distribution it still considers enabled is disabled again and the delete
// retried over a few minutes; a stale IfMatch token is refreshed and the delete
// retried over a minute. A distribution already gone counts as deleted.
func deleteDistribution(ctx context.Context, client *cloudfront.Client, id string) error {
	err := retry.OnError(ctx, isDistributionNotDisabled, func(ctx context.Context) error {
		if err := disableDistribution(ctx, client, id); err != nil {
			if isDistributionNotFound(err) {
				return nil
			}
			return err
		}
		return deleteOnce(ctx, client, id)
	}, retry.WithTimeout(disableRetryTimeout))
	if err != nil {
		if isDistributionNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete distribution %s: %w", id, err)
	}
	return waitDistributionDeleted(ctx, client, id)
}

// deleteOnce fetches the current ETag and deletes the distribution, retrying a
// stale IfMatch token over a minute by re-fetching the ETag each try. A
// distribution already gone is treated as deleted.
func deleteOnce(ctx context.Context, client *cloudfront.Client, id string) error {
	return retry.OnError(ctx, isStaleIfMatch, func(ctx context.Context) error {
		etag, err := distributionETag(ctx, client, id)
		if err != nil {
			if isDistributionNotFound(err) {
				return nil
			}
			return err
		}
		_, err = client.DeleteDistribution(ctx, &cloudfront.DeleteDistributionInput{
			Id:      aws.String(id),
			IfMatch: aws.String(etag),
		})
		if err != nil && isDistributionNotFound(err) {
			return nil
		}
		return err
	}, retry.WithTimeout(etagRetryTimeout))
}

// waitDistributionDeployed polls GetDistribution until the distribution's
// status is Deployed, the state in which a configuration change has reached
// every edge location. A distribution that has gone missing ends the wait with
// a not-found error.
func waitDistributionDeployed(ctx context.Context, client *cloudfront.Client, id string) error {
	return wait.Until(ctx, fmt.Sprintf("distribution %s to deploy", id),
		func(ctx context.Context) (bool, error) {
			resp, err := client.GetDistribution(ctx, &cloudfront.GetDistributionInput{
				Id: aws.String(id),
			})
			if err != nil {
				if isDistributionNotFound(err) {
					return false, fmt.Errorf("distribution %s: %w", id, err)
				}
				return false, fmt.Errorf("get distribution %s: %w", id, err)
			}
			if resp.Distribution == nil {
				return false, nil
			}
			return aws.ToString(resp.Distribution.Status) == statusDeployed, nil
		},
		wait.WithInterval(deployInterval),
		wait.WithTimeout(deployTimeout),
	)
}

// waitDistributionDeleted polls GetDistribution until the distribution is gone.
// While it still exists, in either the InProgress or Deployed state, the wait
// keeps polling; a not-found result ends it successfully.
func waitDistributionDeleted(ctx context.Context, client *cloudfront.Client, id string) error {
	return wait.Until(ctx, fmt.Sprintf("distribution %s to be deleted", id),
		func(ctx context.Context) (bool, error) {
			_, err := client.GetDistribution(ctx, &cloudfront.GetDistributionInput{
				Id: aws.String(id),
			})
			if err != nil {
				if isDistributionNotFound(err) {
					return true, nil
				}
				return false, fmt.Errorf("get distribution %s: %w", id, err)
			}
			return false, nil
		},
		wait.WithInterval(deleteWaitInterval),
		wait.WithTimeout(deleteWaitTimeout),
	)
}

// distributionETag fetches the distribution's current version token, the value
// an update or delete must pass as IfMatch.
func distributionETag(
	ctx context.Context, client *cloudfront.Client, id string,
) (string, error) {
	resp, err := client.GetDistribution(ctx, &cloudfront.GetDistributionInput{
		Id: aws.String(id),
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(resp.ETag), nil
}

// distributionHostedZoneID returns the fixed CloudFront hosted zone for the
// region's partition. It is a constant per partition, not an API value: the
// China partition uses a different zone than every other partition.
func distributionHostedZoneID(region string) string {
	if partition.Of(region) == "aws-cn" {
		return cloudFrontHostedZoneIDChina
	}
	return cloudFrontHostedZoneID
}

// distributionCallerReference returns a unique idempotency token for a create.
// CloudFront treats a repeated CallerReference as a duplicate create, so the
// value must be distinct per distribution; a timestamp with nanosecond
// precision is unique enough for one process.
func distributionCallerReference() string {
	return fmt.Sprintf("unobin-%d", time.Now().UnixNano())
}

// distributionTags converts a desired tag map into the CloudFront tag
// collection, ordered by key so the request is deterministic. The WithTags
// create requires the member, so an empty map yields an empty item list rather
// than nil.
func distributionTags(tags map[string]string) *cloudfronttypes.Tags {
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]cloudfronttypes.Tag, 0, len(tags))
	for _, k := range keys {
		items = append(items, cloudfronttypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(tags[k]),
		})
	}
	return &cloudfronttypes.Tags{Items: items}
}

// isDistributionNotFound reports whether err is CloudFront's
// no-such-distribution error. CloudFront models a missing distribution as its
// own error type, so a Read turns it into runtime.ErrNotFound and a Delete
// treats it as already done.
func isDistributionNotFound(err error) bool {
	var notFound *cloudfronttypes.NoSuchDistribution
	return errors.As(err, &notFound)
}

// isInvalidViewerCertificate reports whether err is the eventual-consistency
// error CloudFront returns when a viewer certificate referenced moments after
// it was issued or imported is not yet usable. The window clears on its own, so
// a create or update retries on it.
func isInvalidViewerCertificate(err error) bool {
	var invalid *cloudfronttypes.InvalidViewerCertificate
	return errors.As(err, &invalid)
}

// isDistributionNotDisabled reports whether err is CloudFront's refusal to
// delete a distribution it still considers enabled. The caller disables the
// distribution again and retries the delete.
func isDistributionNotDisabled(err error) bool {
	var notDisabled *cloudfronttypes.DistributionNotDisabled
	return errors.As(err, &notDisabled)
}

// isPreconditionFailed reports whether err is a stale-ETag rejection of an
// update. The caller re-reads a fresh ETag and retries once.
func isPreconditionFailed(err error) bool {
	var failed *cloudfronttypes.PreconditionFailed
	return errors.As(err, &failed)
}

// isStaleIfMatch reports whether err is either form of stale-token rejection a
// delete can hit: a precondition failure or an invalid IfMatch version. The
// caller re-fetches the ETag and retries.
func isStaleIfMatch(err error) bool {
	if isPreconditionFailed(err) {
		return true
	}
	var invalid *cloudfronttypes.InvalidIfMatchVersion
	return errors.As(err, &invalid)
}

// isLoggingBucketGone reports whether err is the InvalidArgument CloudFront
// returns when a distribution's configured logging bucket no longer exists.
// During a disable, the caller nulls the logging config and retries rather than
// letting a missing log bucket block removal.
func isLoggingBucketGone(err error) bool {
	var invalid *cloudfronttypes.InvalidArgument
	if !errors.As(err, &invalid) {
		return false
	}
	return strings.Contains(invalid.ErrorMessage(), loggingBucketGoneMessage)
}
