package route53

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	route53 "github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/cloudboss/unobin/pkg/awscfg"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for route53, configured from
// cfg. It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*route53.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return route53.NewFromConfig(awsCfg), nil
}

// isNotFound reports whether err is Route 53's no-such-hosted-zone error.
// Route 53 models a missing hosted zone as its own error type, so a Read
// matches the type to turn a read of a gone zone into runtime.ErrNotFound.
// This is the same condition the Terraform provider tests with its typed
// error check.
func isNotFound(err error) bool {
	var notFound *route53types.NoSuchHostedZone
	return errors.As(err, &notFound)
}

// hostedZonePrefix is the path Route 53 prefixes onto a hosted zone id in some
// responses. The bare id without it is what the API takes elsewhere.
const hostedZonePrefix = "/hostedzone/"

// changeInsyncInterval is how long the INSYNC waiter sleeps between GetChange
// polls. Route 53 throttles GetChange hard, so the wait uses a long fifteen-
// second interval to avoid the throttle while a change propagates from PENDING
// to INSYNC.
const changeInsyncInterval = 15 * time.Second

// changeInsyncTimeout bounds how long the INSYNC waiter runs before it gives up.
// A change usually settles in well under a minute, but Route 53 can take much
// longer under load, so the budget is a generous thirty minutes.
const changeInsyncTimeout = 30 * time.Minute

// cleanZoneID strips the /hostedzone/ prefix Route 53 puts on a zone id in some
// responses, leaving the bare id the API takes elsewhere.
func cleanZoneID(id string) string {
	return strings.TrimPrefix(id, hostedZonePrefix)
}

// normalizeName trims a single trailing dot and restores an octal-escaped
// leading wildcard label, so two spellings of one FQDN compare equal. Route 53
// returns "*" as "\\052"; restore it to "*".
func normalizeName(name string) string {
	name = strings.TrimSuffix(name, ".")
	name = strings.Replace(name, "\\052", "*", 1)
	return name
}

// changeError unpacks the per-record messages Route 53 attaches to an invalid
// change batch into the returned error, so a validation rejection names what was
// wrong rather than just reporting the batch as invalid. Every other error is
// returned as is, so the typed checks at the call site still match it.
func changeError(err error) error {
	var invalid *route53types.InvalidChangeBatch
	if errors.As(err, &invalid) && len(invalid.Messages) > 0 {
		return fmt.Errorf("change resource record sets: %s: %w",
			strings.Join(invalid.Messages, "; "), err)
	}
	return err
}

// waitChangeInsync polls GetChange until the change reaches INSYNC, the state in
// which Route 53 has applied it to all of its DNS servers. A change that is not
// yet visible reports NoSuchChange, treated as still pending rather than an
// error so the wait keeps polling. The poll interval is deliberately long, since
// Route 53 throttles GetChange.
func waitChangeInsync(ctx context.Context, client *route53.Client, changeID string) error {
	return wait.Until(ctx, fmt.Sprintf("change %s to be in sync", changeID),
		func(ctx context.Context) (bool, error) {
			resp, err := client.GetChange(ctx, &route53.GetChangeInput{
				Id: aws.String(changeID),
			})
			if err != nil {
				var notFound *route53types.NoSuchChange
				if errors.As(err, &notFound) {
					return false, nil
				}
				return false, fmt.Errorf("get change %s: %w", changeID, err)
			}
			if resp.ChangeInfo == nil {
				return false, nil
			}
			return resp.ChangeInfo.Status == route53types.ChangeStatusInsync, nil
		},
		wait.WithInterval(changeInsyncInterval),
		wait.WithTimeout(changeInsyncTimeout),
	)
}
