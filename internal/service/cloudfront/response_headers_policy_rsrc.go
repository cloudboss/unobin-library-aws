package cloudfront

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// ResponseHeadersPolicy manages a CloudFront response headers policy: a named
// set of HTTP headers a distribution adds to or removes from its responses,
// grouped into CORS, custom, removed, security, and server-timing
// configurations. CloudFront replaces the whole policy on every update rather
// than patching one field, so no field forces a new resource. An update or
// delete is guarded by the policy's current version, an ETag that the create and
// read both return; the ETag is an output the update and delete pass back as the
// IfMatch concurrency token.
type ResponseHeadersPolicy struct {
	Name                      string                              `ub:"name"`
	Comment                   *string                             `ub:"comment"`
	CorsConfig                *ResponseHeadersPolicyCors          `ub:"cors-config"`
	CustomHeadersConfig       *ResponseHeadersPolicyCustomHeaders `ub:"custom-headers-config"`
	RemoveHeadersConfig       *ResponseHeadersPolicyRemoveHeaders `ub:"remove-headers-config"`
	SecurityHeadersConfig     *ResponseHeadersPolicySecurity      `ub:"security-headers-config"`
	ServerTimingHeadersConfig *ResponseHeadersPolicyServerTiming  `ub:"server-timing-headers-config"`
}

// ResponseHeadersPolicyOutput holds the values CloudFront computes for a
// response headers policy. Id is the stable handle used to read, update, and
// delete it and the value a distribution links the policy by. ETag is the
// policy's current version, the concurrency token CloudFront requires as
// IfMatch on an update or delete. The policy's ARN is omitted: composing it
// needs the account id, and the policy is referenced by id, as the origin
// access control resource is.
type ResponseHeadersPolicyOutput struct {
	Id   string `ub:"id"`
	ETag string `ub:"etag"`
}

func (r *ResponseHeadersPolicy) SchemaVersion() int { return 1 }

// ReplaceFields is empty: every setting of a response headers policy reconciles
// in place through UpdateResponseHeadersPolicy, including the name, so none
// forces a new resource.
func (r *ResponseHeadersPolicy) ReplaceFields() []string {
	return nil
}

// Constraints declares the rules CloudFront places on a response headers
// policy. At least one of the five configuration blocks must be set. The frame
// option and the referrer policy are each one of a fixed set when their block
// is present; both rules chain through the optional security block, reading null
// and passing when the block is absent. The sampling rate is a percentage from
// 0 to 100 inclusive, and AtLeast and AtMost pass on a null operand, so the
// rule holds whether or not the server-timing block is set. The
// access-control-allow-methods values stay API-enforced, since the constraint
// layer cannot express a per-element enum over a list nested inside a block.
func (r ResponseHeadersPolicy) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtLeastOneOf(r.CorsConfig, r.CustomHeadersConfig, r.RemoveHeadersConfig,
			r.SecurityHeadersConfig, r.ServerTimingHeadersConfig),
		constraint.When(constraint.Present(r.SecurityHeadersConfig.FrameOptions)).
			Require(constraint.OneOf(r.SecurityHeadersConfig.FrameOptions.FrameOption,
				"DENY", "SAMEORIGIN")).
			Message("security-headers-config frame-options frame-option must be DENY or SAMEORIGIN"),
		constraint.When(constraint.Present(r.SecurityHeadersConfig.ReferrerPolicy)).
			Require(constraint.OneOf(r.SecurityHeadersConfig.ReferrerPolicy.ReferrerPolicy,
				"no-referrer", "no-referrer-when-downgrade", "origin", "origin-when-cross-origin",
				"same-origin", "strict-origin", "strict-origin-when-cross-origin", "unsafe-url")).
			Message("security-headers-config referrer-policy referrer-policy must be one of " +
				"no-referrer, no-referrer-when-downgrade, origin, origin-when-cross-origin, " +
				"same-origin, strict-origin, strict-origin-when-cross-origin, unsafe-url"),
		constraint.When(constraint.Present(r.ServerTimingHeadersConfig.SamplingRate)).
			Require(constraint.AtLeast(r.ServerTimingHeadersConfig.SamplingRate, 0.0),
				constraint.AtMost(r.ServerTimingHeadersConfig.SamplingRate, 100.0)).
			Message("server-timing-headers-config sampling-rate must be between 0 and 100"),
	}
}

// config builds the ResponseHeadersPolicyConfig sent on create and update. Each
// configuration block expands only when set; the comment is included only when
// present.
func (r *ResponseHeadersPolicy) config() *cloudfronttypes.ResponseHeadersPolicyConfig {
	return &cloudfronttypes.ResponseHeadersPolicyConfig{
		Name:                      aws.String(r.Name),
		Comment:                   r.Comment,
		CorsConfig:                expandCors(r.CorsConfig),
		CustomHeadersConfig:       expandCustomHeaders(r.CustomHeadersConfig),
		RemoveHeadersConfig:       expandRemoveHeaders(r.RemoveHeadersConfig),
		SecurityHeadersConfig:     expandSecurity(r.SecurityHeadersConfig),
		ServerTimingHeadersConfig: expandServerTiming(r.ServerTimingHeadersConfig),
	}
}

func (r *ResponseHeadersPolicy) Create(
	ctx context.Context, cfg *awsCfg,
) (*ResponseHeadersPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateResponseHeadersPolicy(ctx,
		&cloudfront.CreateResponseHeadersPolicyInput{
			ResponseHeadersPolicyConfig: r.config(),
		})
	if err != nil {
		return nil, fmt.Errorf("create response headers policy: %w", err)
	}
	if resp.ResponseHeadersPolicy == nil {
		return nil, errors.New("create response headers policy: empty response")
	}
	id := aws.ToString(resp.ResponseHeadersPolicy.Id)
	// The create response includes the id and the ETag, the concurrency token a
	// later update or delete passes as IfMatch, so the outputs come straight from
	// it with no follow-up read.
	return &ResponseHeadersPolicyOutput{Id: id, ETag: aws.ToString(resp.ETag)}, nil
}

func (r *ResponseHeadersPolicy) Read(
	ctx context.Context, cfg *awsCfg, prior *ResponseHeadersPolicyOutput,
) (*ResponseHeadersPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Id)
}

// read fetches the response headers policy by id and computes its outputs. A
// gone policy maps to runtime.ErrNotFound so a plan recreates it. The ETag
// comes from the top level of the response, not from the config, and is the
// version token a later update or delete passes as IfMatch.
func (r *ResponseHeadersPolicy) read(
	ctx context.Context, client *cloudfront.Client, id string,
) (*ResponseHeadersPolicyOutput, error) {
	resp, err := client.GetResponseHeadersPolicy(ctx,
		&cloudfront.GetResponseHeadersPolicyInput{
			Id: aws.String(id),
		})
	if err != nil {
		if isResponseHeadersPolicyNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get response headers policy %s: %w", id, err)
	}
	return &ResponseHeadersPolicyOutput{
		Id:   id,
		ETag: aws.ToString(resp.ETag),
	}, nil
}

func (r *ResponseHeadersPolicy) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ResponseHeadersPolicy, *ResponseHeadersPolicyOutput],
) (*ResponseHeadersPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.Id
	// CloudFront replaces the whole policy on an update and guards it with the
	// current version, the prior read's ETag, passed as IfMatch.
	_, err = client.UpdateResponseHeadersPolicy(ctx,
		&cloudfront.UpdateResponseHeadersPolicyInput{
			Id:                          aws.String(id),
			IfMatch:                     aws.String(prior.Outputs.ETag),
			ResponseHeadersPolicyConfig: r.config(),
		})
	if err != nil {
		return nil, fmt.Errorf("update response headers policy %s: %w", id, err)
	}
	// The update rotates the ETag, so a read keeps the outputs derived in one
	// place and confirms the settled version.
	return r.read(ctx, client, id)
}

func (r *ResponseHeadersPolicy) Delete(
	ctx context.Context, cfg *awsCfg, prior *ResponseHeadersPolicyOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.Id
	_, err = client.DeleteResponseHeadersPolicy(ctx,
		&cloudfront.DeleteResponseHeadersPolicyInput{
			Id:      aws.String(id),
			IfMatch: aws.String(prior.ETag),
		})
	if err != nil {
		// A policy already gone counts as deleted.
		if isResponseHeadersPolicyNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete response headers policy %s: %w", id, err)
	}
	return nil
}

// isResponseHeadersPolicyNotFound reports whether err is CloudFront's
// no-such-response-headers-policy error. CloudFront models a missing policy as
// its own error type, so a Read matches the type to turn a read of a gone
// policy into runtime.ErrNotFound, and a Delete treats it as already done.
func isResponseHeadersPolicyNotFound(err error) bool {
	var notFound *cloudfronttypes.NoSuchResponseHeadersPolicy
	return errors.As(err, &notFound)
}
