package elbv2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type awsCfg = awscfg.Configuration

// newClient returns the AWS SDK Go v2 client for Elastic Load Balancing v2,
// configured from cfg. It builds an aws.Config via awscfg.Load.
func newClient(ctx context.Context, cfg *awsCfg) (*elbv2.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return elbv2.NewFromConfig(awsCfg), nil
}

// region returns the region the client is configured for, used to decide
// whether a create that sends tags must retry without them on a partition that
// cannot tag a resource at create time.
func region(client *elbv2.Client) string {
	return client.Options().Region
}

// isNotFound reports whether err is one of the ELBv2 not-found exceptions for
// the resources this package manages: a load balancer, target group, listener,
// or rule. ELBv2 models each missing resource as its own typed exception, so a
// resource Read matches the type to turn a describe of a gone resource into
// runtime.ErrNotFound.
func isNotFound(err error) bool {
	var (
		lb       *elbv2types.LoadBalancerNotFoundException
		tg       *elbv2types.TargetGroupNotFoundException
		listener *elbv2types.ListenerNotFoundException
		rule     *elbv2types.RuleNotFoundException
	)
	return errors.As(err, &lb) || errors.As(err, &tg) ||
		errors.As(err, &listener) || errors.As(err, &rule)
}

// isCertificateNotFound reports whether err is ELBv2's CertificateNotFound
// exception. ELBv2 raises it transiently when a listener or listener
// certificate references an ACM certificate the certificate control plane has
// not yet made visible, a race that clears on its own, so a caller retries.
func isCertificateNotFound(err error) bool {
	var notFound *elbv2types.CertificateNotFoundException
	return errors.As(err, &notFound)
}

// isPriorityInUse reports whether err is ELBv2's PriorityInUse exception. A
// listener rule created with an auto-assigned priority races other rules for
// the next free slot, and ELBv2 rejects the loser with this exception, so a
// caller recomputes the priority and retries.
func isPriorityInUse(err error) bool {
	var inUse *elbv2types.PriorityInUseException
	return errors.As(err, &inUse)
}

// isResourceInUse reports whether err is ELBv2's ResourceInUse exception. A
// target group still attached to a listener or rule cannot be deleted until the
// dependency clears, which ELBv2 reports with this exception, so a caller
// retries the delete through that window.
func isResourceInUse(err error) bool {
	var inUse *elbv2types.ResourceInUseException
	return errors.As(err, &inUse)
}

// isTagOnCreateUnsupported reports whether err is the validation error ELBv2
// returns when a resource cannot be tagged on the create call itself, as some
// load balancer types (Gateway) reject. It complements
// partition.UnsupportedOperation, which catches the ISO-partition form of the
// same limitation: either signal means the resource must be created untagged
// and tagged by a separate call.
func isTagOnCreateUnsupported(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) &&
		apiErr.ErrorCode() == "ValidationError" &&
		strings.Contains(apiErr.ErrorMessage(), "cannot specify tags on creation")
}

// tagList converts a desired tag map into the ELBv2 SDK tag list. ELBv2 shares
// one Tag type across its taggable resources, so the load balancer, target
// group, listener, and rule all build their create-time tags through it.
func tagList(tags map[string]string) []elbv2types.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]elbv2types.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, elbv2types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}

// syncTags reconciles a resource's tags with the desired set, reading the live
// tags through DescribeTags and writing changes with AddTags and RemoveTags.
// ELBv2 addresses every taggable resource by its ARN through one shared tag
// API, so the load balancer, target group, listener, and rule all reconcile
// their tags through this helper.
func syncTags(
	ctx context.Context, client *elbv2.Client, arn string, desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.DescribeTags(ctx,
				&elbv2.DescribeTagsInput{ResourceArns: []string{arn}})
			if err != nil {
				return nil, fmt.Errorf("describe tags: %w", err)
			}
			current := map[string]string{}
			for _, desc := range resp.TagDescriptions {
				for _, t := range desc.Tags {
					current[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.AddTags(ctx, &elbv2.AddTagsInput{
				ResourceArns: []string{arn},
				Tags:         tagList(upsert),
			}); err != nil {
				return fmt.Errorf("add tags: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.RemoveTags(ctx, &elbv2.RemoveTagsInput{
				ResourceArns: []string{arn},
				TagKeys:      remove,
			}); err != nil {
				return fmt.Errorf("remove tags: %w", err)
			}
			return nil
		},
	)
}
