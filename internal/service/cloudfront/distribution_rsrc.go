package cloudfront

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

// certRetryTimeout bounds the retry that absorbs a viewer certificate's
// eventual consistency. A certificate referenced moments after it is issued or
// imported can briefly fail validation; the window clears in well under a
// minute, so a create or update wrapping its call retries over one minute.
const certRetryTimeout = time.Minute

// DistributionResource manages a CloudFront distribution: the edge configuration that
// tells CloudFront where to fetch content (its origins) and how to serve it
// (its cache behaviors), plus the TLS, geo, logging, and error-handling
// settings. CloudFront takes the whole configuration in one call and replaces
// it whole on every update, guarded by an optimistic-concurrency token (an
// ETag) and a long propagation wait, so no field forces a new resource.
//
// Caching for each behavior is configured through its cache-policy-id, a
// managed or custom cache policy; the deprecated inline forwarded-values path
// (forwarded values and the min, default, and max TTLs) is intentionally not
// supported, since AWS recommends a cache policy for new distributions.
//
// A delete is a two-step dance: CloudFront refuses to remove an enabled or
// still-deploying distribution, so Delete first disables it and waits for that
// to propagate, then deletes it and waits for it to disappear.
//
// This is the minimal usable core. The following genuine properties of
// AWS::CloudFront::Distribution are deferred to a follow-up: origin groups
// (failover); an origin's VPC origin, origin shield, connection attempts and
// timeouts, and connection-function association; the continuous-deployment
// policy and the staging flag (the only field that would force a replace);
// trusted signers and trusted key groups; per-behavior field-level encryption,
// real-time log config, smooth streaming, and gRPC; the anycast IP list; the
// cache-tag config; the viewer mTLS config; and the multi-tenant distribution
// (a separate resource).
type DistributionResource struct {
	Enabled              *bool                              `ub:"enabled"`
	Aliases              *[]string                          `ub:"aliases"`
	Comment              *string                            `ub:"comment"`
	DefaultRootObject    *string                            `ub:"default-root-object"`
	PriceClass           *string                            `ub:"price-class"`
	HttpVersion          *string                            `ub:"http-version"`
	IsIPV6Enabled        *bool                              `ub:"is-ipv6-enabled"`
	WebACLId             *string                            `ub:"web-acl-id"`
	Origins              []DistributionOrigin               `ub:"origins"`
	DefaultCacheBehavior DistributionDefaultCacheBehavior   `ub:"default-cache-behavior"`
	CacheBehaviors       *[]DistributionCacheBehavior       `ub:"cache-behaviors"`
	CustomErrorResponses *[]DistributionCustomErrorResponse `ub:"custom-error-responses"`
	ViewerCertificate    *DistributionViewerCertificate     `ub:"viewer-certificate"`
	Restrictions         *DistributionRestrictions          `ub:"restrictions"`
	Logging              *DistributionLogging               `ub:"logging"`
	Tags                 *map[string]string                 `ub:"tags"`
}

// DistributionResourceOutput holds the values CloudFront computes for a distribution.
// Id is the stable handle used to read, update, and delete it and the value a
// Route 53 alias target references. DomainName is the dxxx.cloudfront.net host
// that downstream DNS points at. ETag is the distribution's current version,
// the concurrency token CloudFront requires as IfMatch on an update or delete;
// it is returned only by a read, so the create learns it through one.
// CallerReference is the create idempotency token, generated once at create and
// resent unchanged on every update. HostedZoneId is the fixed CloudFront zone
// for the partition, used to build an alias record, and is a constant rather
// than an API value.
type DistributionResourceOutput struct {
	Id              string `ub:"id"`
	Arn             string `ub:"arn"`
	DomainName      string `ub:"domain-name"`
	HostedZoneId    string `ub:"hosted-zone-id"`
	Status          string `ub:"status"`
	ETag            string `ub:"etag"`
	CallerReference string `ub:"caller-reference"`
}

func (r *DistributionResource) SchemaVersion() int { return 1 }

// ReplaceFields is empty: CloudFront reconciles every core setting in place
// through UpdateDistribution, which replaces the whole configuration, so none
// forces a new resource. The one field that would force a replace, the staging
// flag, is deferred.
func (r *DistributionResource) ReplaceFields() []string {
	return nil
}

// Constraints declares the rules CloudFront places on a distribution. The
// enums (price class, HTTP version, the default behavior's viewer protocol
// policy and per-behavior settings, the geo restriction type, and the viewer
// certificate's protocol version and SSL support method) are each one of a
// fixed set when present. The viewer certificate must name exactly one source:
// the default CloudFront certificate, an ACM certificate, or an IAM
// certificate; the rule chains through the optional block, reading null and
// passing when it is absent.
func (r DistributionResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.PriceClass)).
			Require(constraint.OneOf(r.PriceClass,
				"PriceClass_100", "PriceClass_200", "PriceClass_All")).
			Message("price-class must be PriceClass_100, PriceClass_200, or PriceClass_All"),
		constraint.When(constraint.Present(r.HttpVersion)).
			Require(constraint.OneOf(r.HttpVersion,
				"http1.1", "http2", "http2and3", "http3")).
			Message("http-version must be http1.1, http2, http2and3, or http3"),
		constraint.When(constraint.Present(r.ViewerCertificate)).
			Require(constraint.Any(
				constraint.All(
					constraint.IsTrue(r.ViewerCertificate.CloudFrontDefaultCertificate),
					constraint.Absent(r.ViewerCertificate.ACMCertificateArn),
					constraint.Absent(r.ViewerCertificate.IAMCertificateId)),
				constraint.All(
					constraint.Present(r.ViewerCertificate.ACMCertificateArn),
					constraint.Absent(r.ViewerCertificate.IAMCertificateId),
					constraint.Not(
						constraint.IsTrue(r.ViewerCertificate.CloudFrontDefaultCertificate))),
				constraint.All(
					constraint.Present(r.ViewerCertificate.IAMCertificateId),
					constraint.Absent(r.ViewerCertificate.ACMCertificateArn),
					constraint.Not(
						constraint.IsTrue(r.ViewerCertificate.CloudFrontDefaultCertificate))))).
			Message("viewer-certificate must set exactly one of " +
				"cloudfront-default-certificate true, acm-certificate-arn, or iam-certificate-id"),
		constraint.When(constraint.Present(r.ViewerCertificate.MinimumProtocolVersion)).
			Require(constraint.OneOf(r.ViewerCertificate.MinimumProtocolVersion,
				"SSLv3", "TLSv1", "TLSv1_2016", "TLSv1.1_2016", "TLSv1.2_2018",
				"TLSv1.2_2019", "TLSv1.2_2021", "TLSv1.3_2025", "TLSv1.2_2025")).
			Message("viewer-certificate minimum-protocol-version must be one of SSLv3, " +
				"TLSv1, TLSv1_2016, TLSv1.1_2016, TLSv1.2_2018, TLSv1.2_2019, TLSv1.2_2021, " +
				"TLSv1.3_2025, TLSv1.2_2025"),
		constraint.When(constraint.Present(r.ViewerCertificate.SSLSupportMethod)).
			Require(constraint.OneOf(r.ViewerCertificate.SSLSupportMethod,
				"sni-only", "vip", "static-ip")).
			Message("viewer-certificate ssl-support-method must be sni-only, vip, or static-ip"),
		constraint.When(constraint.Present(r.Restrictions.GeoRestriction.RestrictionType)).
			Require(constraint.OneOf(r.Restrictions.GeoRestriction.RestrictionType,
				"none", "whitelist", "blacklist")).
			Message("restrictions geo-restriction restriction-type must be " +
				"none, whitelist, or blacklist"),
		constraint.Must(constraint.OneOf(r.DefaultCacheBehavior.ViewerProtocolPolicy,
			"allow-all", "https-only", "redirect-to-https")).
			Message("default-cache-behavior viewer-protocol-policy must be " +
				"allow-all, https-only, or redirect-to-https"),
		constraint.ForEach(r.CacheBehaviors,
			func(b DistributionCacheBehavior) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(b.ViewerProtocolPolicy,
						"allow-all", "https-only", "redirect-to-https")).
						Message("cache-behaviors viewer-protocol-policy must be " +
							"allow-all, https-only, or redirect-to-https"),
				}
			}),
		constraint.ForEach(r.Origins, func(o DistributionOrigin) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.AtMostOneOf(o.S3OriginConfig, o.CustomOriginConfig),
				constraint.When(constraint.Present(o.CustomOriginConfig.OriginProtocolPolicy)).
					Require(constraint.OneOf(o.CustomOriginConfig.OriginProtocolPolicy,
						"http-only", "https-only", "match-viewer")).
					Message("custom-origin-config origin-protocol-policy must be " +
						"http-only, https-only, or match-viewer"),
			}
		}),
	}
}

func (r *DistributionResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*DistributionResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	callerReference := distributionCallerReference()
	in := &cloudfront.CreateDistributionWithTagsInput{
		DistributionConfigWithTags: &cloudfronttypes.DistributionConfigWithTags{
			DistributionConfig: r.expandConfig(callerReference),
			Tags:               distributionTags(ptr.Value(r.Tags)),
		},
	}
	var resp *cloudfront.CreateDistributionWithTagsOutput
	err = retry.OnError(ctx, isInvalidViewerCertificate, func(ctx context.Context) error {
		var createErr error
		resp, createErr = client.CreateDistributionWithTags(ctx, in)
		return createErr
	}, retry.WithTimeout(certRetryTimeout))
	if err != nil {
		return nil, fmt.Errorf("create distribution: %w", err)
	}
	if resp.Distribution == nil {
		return nil, errors.New("create distribution: empty response")
	}
	id := aws.ToString(resp.Distribution.Id)
	// CloudFront returns the new distribution in the InProgress state; it is not
	// usable until it has propagated to the edge, so wait for Deployed before
	// reading the settled outputs.
	if err := waitDistributionDeployed(ctx, client, id); err != nil {
		return nil, err
	}
	return r.read(ctx, client, id)
}

func (r *DistributionResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *DistributionResourceOutput,
) (*DistributionResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Id)
}

// read fetches the distribution by id and computes its outputs. A gone
// distribution maps to runtime.ErrNotFound so a plan recreates it. The ETag
// comes from the top level of the response, a sibling of the distribution and
// not part of its config, and is the version token a later update or delete
// passes as IfMatch. The hosted zone id is the fixed CloudFront zone for the
// client's partition, not an API value.
func (r *DistributionResource) read(
	ctx context.Context, client *cloudfront.Client, id string,
) (*DistributionResourceOutput, error) {
	resp, err := client.GetDistribution(ctx, &cloudfront.GetDistributionInput{
		Id: aws.String(id),
	})
	if err != nil {
		if isDistributionNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get distribution %s: %w", id, err)
	}
	dist := resp.Distribution
	if dist == nil {
		return nil, runtime.ErrNotFound
	}
	callerReference := ""
	if dist.DistributionConfig != nil {
		callerReference = aws.ToString(dist.DistributionConfig.CallerReference)
	}
	return &DistributionResourceOutput{
		Id:              aws.ToString(dist.Id),
		Arn:             aws.ToString(dist.ARN),
		DomainName:      aws.ToString(dist.DomainName),
		HostedZoneId:    distributionHostedZoneID(client.Options().Region),
		Status:          aws.ToString(dist.Status),
		ETag:            aws.ToString(resp.ETag),
		CallerReference: callerReference,
	}, nil
}

func (r *DistributionResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[DistributionResource, *DistributionResourceOutput],
) (*DistributionResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.Id
	// A full UpdateDistribution replaces the whole configuration and pushes the
	// distribution back to InProgress for a fifteen-to-ninety-minute redeploy, so
	// it runs only when a non-tag input actually changed. A tag-only edit skips it
	// and reconciles tags alone, avoiding a redeploy the cloud does not need.
	if r.configChanged(prior) {
		// UpdateDistribution must send a complete configuration -- every member
		// present at every level, even the optional ones create lets CloudFront
		// default. The update therefore starts from the live config and overlays
		// only the fields that changed, guarded by the ETag read alongside it.
		config, etag, err := r.updatedConfig(ctx, client, id, prior)
		if err != nil {
			return nil, err
		}
		if err := r.updateConfig(ctx, client, id, etag, config); err != nil {
			return nil, err
		}
		// An update that changes the configuration moves the distribution back to
		// InProgress, so wait for it to redeploy before reading.
		if err := waitDistributionDeployed(ctx, client, id); err != nil {
			return nil, err
		}
	}
	// Tags are not part of the configuration diff; they reconcile on their own
	// change through TagResource and UntagResource against the distribution ARN.
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, prior.Outputs.Arn); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, id)
}

// configChanged reports whether any non-tag input changed against the prior
// inputs: every field that rides the DistributionConfig sent by
// UpdateDistribution. Tags reconcile separately through their own change check,
// so they are excluded here. When this is false and only tags changed, Update
// skips the full UpdateDistribution and its long redeploy wait entirely.
func (r *DistributionResource) configChanged(
	prior runtime.Prior[DistributionResource, *DistributionResourceOutput],
) bool {
	return runtime.Changed(prior.Inputs.Enabled, r.Enabled) ||
		runtime.Changed(ptr.Value(prior.Inputs.Aliases), ptr.Value(r.Aliases)) ||
		runtime.Changed(prior.Inputs.Comment, r.Comment) ||
		runtime.Changed(prior.Inputs.DefaultRootObject, r.DefaultRootObject) ||
		runtime.Changed(prior.Inputs.PriceClass, r.PriceClass) ||
		runtime.Changed(prior.Inputs.HttpVersion, r.HttpVersion) ||
		runtime.Changed(prior.Inputs.IsIPV6Enabled, r.IsIPV6Enabled) ||
		runtime.Changed(prior.Inputs.WebACLId, r.WebACLId) ||
		runtime.Changed(prior.Inputs.Origins, r.Origins) ||
		runtime.Changed(prior.Inputs.DefaultCacheBehavior, r.DefaultCacheBehavior) ||
		runtime.Changed(prior.Inputs.CacheBehaviors, r.CacheBehaviors) ||
		runtime.Changed(ptr.Value(prior.Inputs.CustomErrorResponses), ptr.Value(r.CustomErrorResponses)) ||
		runtime.Changed(prior.Inputs.ViewerCertificate, r.ViewerCertificate) ||
		runtime.Changed(prior.Inputs.Restrictions, r.Restrictions) ||
		runtime.Changed(prior.Inputs.Logging, r.Logging)
}

// updateConfig replaces the whole distribution configuration, guarded by the
// prior ETag as IfMatch. It absorbs a viewer certificate's eventual
// consistency with a one-minute retry, and a stale ETag once: if the recorded
// version no longer matches because the distribution changed out of band,
// CloudFront returns PreconditionFailed, so the call re-reads a fresh ETag and
// retries.
func (r *DistributionResource) updateConfig(
	ctx context.Context, client *cloudfront.Client, id, etag string,
	config *cloudfronttypes.DistributionConfig,
) error {
	ifMatch := etag
	update := func(ctx context.Context) error {
		_, err := client.UpdateDistribution(ctx, &cloudfront.UpdateDistributionInput{
			Id:                 aws.String(id),
			DistributionConfig: config,
			IfMatch:            aws.String(ifMatch),
		})
		return err
	}
	err := retry.OnError(ctx, isInvalidViewerCertificate, update,
		retry.WithTimeout(certRetryTimeout))
	if err != nil && isPreconditionFailed(err) {
		fresh, freshErr := distributionETag(ctx, client, id)
		if freshErr != nil {
			return freshErr
		}
		ifMatch = fresh
		err = retry.OnError(ctx, isInvalidViewerCertificate, update,
			retry.WithTimeout(certRetryTimeout))
	}
	if err != nil {
		return fmt.Errorf("update distribution %s: %w", id, err)
	}
	return nil
}

// updatedConfig builds the configuration for an UpdateDistribution. CloudFront
// requires the update to send a complete configuration -- every member present
// at every level, even the optional ones create lets it default and the ones the
// library does not model -- so it starts from the live config read with
// GetDistributionConfig and overlays only the fields whose input changed.
// Unchanged and unmodeled fields keep their live values, which keeps the config
// complete without reconstructing CloudFront's defaults, some of which the
// schema does not document (the is-ipv6-enabled default is unstated, and
// http-version defaults to http1.1, not http2). It returns the config and the
// ETag read with it, the concurrency token the write that follows guards on.
func (r *DistributionResource) updatedConfig(
	ctx context.Context, client *cloudfront.Client, id string,
	prior runtime.Prior[DistributionResource, *DistributionResourceOutput],
) (*cloudfronttypes.DistributionConfig, string, error) {
	cur, err := client.GetDistributionConfig(ctx, &cloudfront.GetDistributionConfigInput{
		Id: aws.String(id),
	})
	if err != nil {
		return nil, "", fmt.Errorf("get distribution config %s: %w", id, err)
	}
	config := cur.DistributionConfig
	overlayChangedConfig(config, r, prior)
	return config, aws.ToString(cur.ETag), nil
}

// overlayChangedConfig overlays onto the live config every modeled field whose
// input changed against the prior, leaving every unchanged field at its live
// value. The set of fields mirrors configChanged, which decides whether an
// update runs at all.
func overlayChangedConfig(
	config *cloudfronttypes.DistributionConfig,
	r *DistributionResource,
	prior runtime.Prior[DistributionResource, *DistributionResourceOutput],
) {
	if runtime.Changed(prior.Inputs.Enabled, r.Enabled) {
		config.Enabled = boolOrFalse(r.Enabled)
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Aliases), ptr.Value(r.Aliases)) {
		config.Aliases = expandAliases(ptr.Value(r.Aliases))
	}
	if runtime.Changed(prior.Inputs.Comment, r.Comment) {
		config.Comment = aws.String(aws.ToString(r.Comment))
	}
	if runtime.Changed(prior.Inputs.DefaultRootObject, r.DefaultRootObject) {
		config.DefaultRootObject = aws.String(aws.ToString(r.DefaultRootObject))
	}
	if runtime.Changed(prior.Inputs.PriceClass, r.PriceClass) {
		config.PriceClass = cloudfronttypes.PriceClass(aws.ToString(r.PriceClass))
	}
	if runtime.Changed(prior.Inputs.HttpVersion, r.HttpVersion) {
		config.HttpVersion = cloudfronttypes.HttpVersion(aws.ToString(r.HttpVersion))
	}
	if runtime.Changed(prior.Inputs.IsIPV6Enabled, r.IsIPV6Enabled) {
		config.IsIPV6Enabled = r.IsIPV6Enabled
	}
	if runtime.Changed(prior.Inputs.WebACLId, r.WebACLId) {
		config.WebACLId = r.WebACLId
	}
	if runtime.Changed(prior.Inputs.Origins, r.Origins) {
		config.Origins = expandOrigins(r.Origins)
	}
	if runtime.Changed(prior.Inputs.DefaultCacheBehavior, r.DefaultCacheBehavior) {
		config.DefaultCacheBehavior = expandDefaultCacheBehavior(r.DefaultCacheBehavior)
	}
	if runtime.Changed(prior.Inputs.CacheBehaviors, r.CacheBehaviors) {
		config.CacheBehaviors = expandCacheBehaviors(ptr.Value(r.CacheBehaviors))
	}
	if runtime.Changed(ptr.Value(prior.Inputs.CustomErrorResponses), ptr.Value(r.CustomErrorResponses)) {
		config.CustomErrorResponses = expandCustomErrorResponses(ptr.Value(r.CustomErrorResponses))
	}
	if runtime.Changed(prior.Inputs.ViewerCertificate, r.ViewerCertificate) {
		config.ViewerCertificate = expandViewerCertificate(r.ViewerCertificate)
	}
	if runtime.Changed(prior.Inputs.Restrictions, r.Restrictions) {
		config.Restrictions = expandRestrictions(r.Restrictions)
	}
	if runtime.Changed(prior.Inputs.Logging, r.Logging) {
		config.Logging = expandLogging(r.Logging)
	}
}

// syncTags reconciles the distribution's tags with the desired set, reading the
// live tags through ListTagsForResource and writing changes with TagResource
// and UntagResource. CloudFront addresses a distribution's tags by its ARN.
func (r *DistributionResource) syncTags(
	ctx context.Context,
	client *cloudfront.Client,
	arn string,
) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx,
				&cloudfront.ListTagsForResourceInput{Resource: aws.String(arn)})
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			current := map[string]string{}
			if resp.Tags != nil {
				for _, t := range resp.Tags.Items {
					current[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &cloudfront.TagResourceInput{
				Resource: aws.String(arn),
				Tags:     distributionTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &cloudfront.UntagResourceInput{
				Resource: aws.String(arn),
				TagKeys:  &cloudfronttypes.TagKeys{Items: remove},
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}
