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

// OriginAccessControl manages a CloudFront origin access control: the signed
// identity a distribution uses to reach a private origin, such as an S3 bucket
// that blocks public access. All five settings live in one config struct and
// reconcile in place, so no field forces a replace. CloudFront guards an update
// or delete with the config's current version, an ETag, which the API returns
// only from a read, not from the create. So the create routes through a read to
// learn the ETag, and the ETag is an output the update and delete pass back as
// the IfMatch concurrency token.
type OriginAccessControl struct {
	// Name identifies the origin access control. CloudFront limits it to 64
	// characters; the bound is checked in validate, since the constraint layer
	// counts bytes rather than the characters CloudFront limits.
	Name string `ub:"name"`
	// Description is optional but always sent, defaulting to the empty string,
	// because CloudFront wants the field present in the config. It is at most
	// 256 characters, checked in validate for the same byte-versus-character
	// reason as the name.
	Description                   *string `ub:"description"`
	OriginAccessControlOriginType string  `ub:"origin-access-control-origin-type"`
	SigningBehavior               string  `ub:"signing-behavior"`
	SigningProtocol               string  `ub:"signing-protocol"`
}

// OriginAccessControlOutput holds the values CloudFront computes for an origin
// access control. Id is the stable handle used to read, update, and delete it
// and the value a distribution links the origin access control by. ETag is the
// config's current version, the concurrency token CloudFront requires as
// IfMatch on an update or delete.
type OriginAccessControlOutput struct {
	Id   string `ub:"id"`
	ETag string `ub:"etag"`
}

func (r *OriginAccessControl) SchemaVersion() int { return 1 }

// ReplaceFields is empty: every setting of an origin access control reconciles
// in place through UpdateOriginAccessControl, so none forces a new resource.
func (r *OriginAccessControl) ReplaceFields() []string {
	return nil
}

// Constraints declares the enum rules CloudFront places on an origin access
// control. The origin type, the signing behavior, and the signing protocol are
// each one of a fixed set; all three are required, so an unconditional Must
// holds. The name and description length bounds are counted in characters,
// which the constraint layer measures in bytes, so they are checked in validate
// rather than declared here.
func (r OriginAccessControl) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.OriginAccessControlOriginType,
			"s3", "mediastore", "mediapackagev2", "lambda")).
			Message("origin-access-control-origin-type must be one of " +
				"s3, mediastore, mediapackagev2, lambda"),
		constraint.Must(constraint.OneOf(r.SigningBehavior, "never", "always", "no-override")).
			Message("signing-behavior must be one of never, always, no-override"),
		constraint.Must(constraint.OneOf(r.SigningProtocol, "sigv4")).
			Message("signing-protocol must be sigv4"),
	}
}

// validate checks the length bounds CloudFront enforces but the constraint
// layer cannot express, since it counts string length in bytes rather than in
// the characters CloudFront limits: the name is 1 to 64 characters and the
// description at most 256.
func (r *OriginAccessControl) validate() error {
	n := len(r.Name)
	if n < 1 || n > 64 {
		return errors.New("name must be between 1 and 64 characters")
	}
	if r.Description != nil && len(*r.Description) > 256 {
		return errors.New("description must be at most 256 characters")
	}
	return nil
}

// config builds the OriginAccessControlConfig sent on create and update. The
// description is always present, defaulting to the empty string when unset, the
// value CloudFront expects in the field.
func (r *OriginAccessControl) config() *cloudfronttypes.OriginAccessControlConfig {
	return &cloudfronttypes.OriginAccessControlConfig{
		Name:        aws.String(r.Name),
		Description: aws.String(aws.ToString(r.Description)),
		OriginAccessControlOriginType: cloudfronttypes.OriginAccessControlOriginTypes(
			r.OriginAccessControlOriginType),
		SigningBehavior: cloudfronttypes.OriginAccessControlSigningBehaviors(r.SigningBehavior),
		SigningProtocol: cloudfronttypes.OriginAccessControlSigningProtocols(r.SigningProtocol),
	}
}

func (r *OriginAccessControl) Create(
	ctx context.Context, cfg *awsCfg,
) (*OriginAccessControlOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateOriginAccessControl(ctx, &cloudfront.CreateOriginAccessControlInput{
		OriginAccessControlConfig: r.config(),
	})
	if err != nil {
		return nil, fmt.Errorf("create origin access control: %w", err)
	}
	if resp.OriginAccessControl == nil {
		return nil, errors.New("create origin access control: empty response")
	}
	id := aws.ToString(resp.OriginAccessControl.Id)
	// The create response includes the id and the ETag, the concurrency token a
	// later update or delete passes as IfMatch, so the outputs come straight from
	// it with no follow-up read.
	return &OriginAccessControlOutput{Id: id, ETag: aws.ToString(resp.ETag)}, nil
}

func (r *OriginAccessControl) Read(
	ctx context.Context, cfg *awsCfg, prior *OriginAccessControlOutput,
) (*OriginAccessControlOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Id)
}

// read fetches the origin access control by id and computes its outputs. A gone
// origin access control maps to runtime.ErrNotFound so a plan recreates it. The
// ETag comes from the top level of the response, not from the config, and is
// the version token a later update or delete passes as IfMatch.
func (r *OriginAccessControl) read(
	ctx context.Context, client *cloudfront.Client, id string,
) (*OriginAccessControlOutput, error) {
	resp, err := client.GetOriginAccessControl(ctx, &cloudfront.GetOriginAccessControlInput{
		Id: aws.String(id),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get origin access control %s: %w", id, err)
	}
	return &OriginAccessControlOutput{
		Id:   id,
		ETag: aws.ToString(resp.ETag),
	}, nil
}

func (r *OriginAccessControl) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[OriginAccessControl, *OriginAccessControlOutput],
) (*OriginAccessControlOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.Id
	// CloudFront takes the whole config on an update and guards it with the
	// current version, the prior read's ETag, passed as IfMatch.
	_, err = client.UpdateOriginAccessControl(ctx, &cloudfront.UpdateOriginAccessControlInput{
		Id:                        aws.String(id),
		IfMatch:                   aws.String(prior.Outputs.ETag),
		OriginAccessControlConfig: r.config(),
	})
	if err != nil {
		return nil, fmt.Errorf("update origin access control %s: %w", id, err)
	}
	// The update returns a new ETag, but a read keeps the outputs derived in one
	// place and confirms the settled version.
	return r.read(ctx, client, id)
}

func (r *OriginAccessControl) Delete(
	ctx context.Context, cfg *awsCfg, prior *OriginAccessControlOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.Id
	_, err = client.DeleteOriginAccessControl(ctx, &cloudfront.DeleteOriginAccessControlInput{
		Id:      aws.String(id),
		IfMatch: aws.String(prior.ETag),
	})
	if err != nil {
		// An origin access control already gone counts as deleted.
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete origin access control %s: %w", id, err)
	}
	return nil
}

// isNotFound reports whether err is CloudFront's no-such-origin-access-control
// error. CloudFront models a missing origin access control as its own error
// type, so a Read matches the type to turn a read of a gone resource into
// runtime.ErrNotFound, and a Delete treats it as already done.
func isNotFound(err error) bool {
	var notFound *cloudfronttypes.NoSuchOriginAccessControl
	return errors.As(err, &notFound)
}
