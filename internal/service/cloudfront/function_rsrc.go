package cloudfront

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// Function manages a CloudFront function: the JavaScript that runs at the edge
// on a viewer request or response. Its lifecycle is two-staged. Every create
// and update writes the DEVELOPMENT stage, and when publish is set a
// PublishFunction promotes that code to LIVE, the stage a distribution attaches.
// CloudFront guards every mutating call with the function's current version, an
// ETag the API returns only from a read, so the create routes through a read to
// learn it and the ETag is an output the update and delete pass back as the
// IfMatch concurrency token. The name fixes the function at create time, so a
// change to it replaces the function; the code, comment, runtime, and key value
// store associations reconcile in place.
type Function struct {
	// Name identifies the function and fixes it at create time.
	Name string `ub:"name"`
	// Runtime is the function's runtime environment, one of cloudfront-js-1.0
	// or cloudfront-js-2.0.
	Runtime string `ub:"runtime"`
	// CodeContent is the function's JavaScript given inline. Exactly one of
	// code-content or code-path is set.
	CodeContent *string `ub:"code-content"`
	// CodePath is a path to a file holding the function's JavaScript. Exactly
	// one of code-content or code-path is set.
	CodePath *string `ub:"code-path"`
	// Comment describes the function. It is optional but always sent,
	// defaulting to the empty string, because CloudFront wants the field
	// present in the config.
	Comment *string `ub:"comment"`
	// KeyValueStoreAssociations lists the ARNs of the key value stores the
	// function reads at runtime. CloudFront limits this to one store; the API
	// rejects more, so no constraint caps it here.
	KeyValueStoreAssociations []string `ub:"key-value-store-associations"`
	// Publish is an intent flag, defaulting to true, that promotes the
	// DEVELOPMENT code to the LIVE stage through PublishFunction. It is
	// reconciled by that call on every create and update, not echoed to output.
	Publish *bool `ub:"publish"`
}

// FunctionOutput holds the values CloudFront computes for a function. Arn is the
// function's identity, the value a distribution's cache behavior attaches it by.
// Status reflects the publish lifecycle and changes across applies without input
// changes. ETag is the function's current version, the concurrency token
// CloudFront requires as IfMatch on an update, publish, or delete; it settles
// after every write and is returned only by a read. LiveStageETag is the LIVE
// stage's own version, which exists only after a publish and is empty before
// one.
type FunctionOutput struct {
	Arn           string `ub:"arn"`
	Status        string `ub:"status"`
	ETag          string `ub:"etag"`
	LiveStageETag string `ub:"live-stage-etag"`
}

func (r *Function) SchemaVersion() int { return 1 }

// ReplaceFields lists the input CloudFront fixes when a function is created. The
// name cannot be changed on an existing function, so a change to it requires a
// new function. Every other input reconciles in place through UpdateFunction.
func (r *Function) ReplaceFields() []string {
	return []string{"name"}
}

// Defaults marks the collection input a function may omit.
func (r Function) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.KeyValueStoreAssociations),
	}
}

// Constraints declares the rules CloudFront places on a function's inputs. The
// runtime is one of a fixed set and is required, so an unconditional Must holds.
// The function code comes from exactly one source, given inline or as a file.
func (r Function) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.Runtime,
			"cloudfront-js-1.0", "cloudfront-js-2.0")).
			Message("runtime must be one of cloudfront-js-1.0, cloudfront-js-2.0"),
		constraint.ExactlyOneOf(r.CodeContent, r.CodePath),
	}
}

// publishWanted reports whether the function should be promoted to the LIVE
// stage. Publish is an intent flag defaulting to true, so an unset value means
// publish.
func (r *Function) publishWanted() bool {
	return r.Publish == nil || *r.Publish
}

// functionCode reads the function's JavaScript from whichever source is set,
// inline content or a file on disk, and returns it as the bytes CloudFront
// takes. Constraints guarantees exactly one source.
func (r *Function) functionCode() ([]byte, error) {
	switch {
	case r.CodeContent != nil:
		return []byte(*r.CodeContent), nil
	case r.CodePath != nil:
		data, err := os.ReadFile(*r.CodePath)
		if err != nil {
			return nil, fmt.Errorf("read code file %s: %w", *r.CodePath, err)
		}
		return data, nil
	default:
		return nil, errors.New("one of code-content or code-path is required")
	}
}

// config builds the FunctionConfig sent on create and update. The comment is
// always present, defaulting to the empty string when unset, the value
// CloudFront expects in the field.
func (r *Function) config() *cloudfronttypes.FunctionConfig {
	return &cloudfronttypes.FunctionConfig{
		Comment:                   aws.String(aws.ToString(r.Comment)),
		Runtime:                   cloudfronttypes.FunctionRuntime(r.Runtime),
		KeyValueStoreAssociations: r.keyValueStoreAssociations(),
	}
}

// keyValueStoreAssociations converts the ARN list into the SDK type, which wraps
// the items in a quantity. An empty list leaves the member nil so the field is
// omitted rather than sent as an empty set.
func (r *Function) keyValueStoreAssociations() *cloudfronttypes.KeyValueStoreAssociations {
	if len(r.KeyValueStoreAssociations) == 0 {
		return nil
	}
	items := make([]cloudfronttypes.KeyValueStoreAssociation, 0,
		len(r.KeyValueStoreAssociations))
	for _, arn := range r.KeyValueStoreAssociations {
		items = append(items, cloudfronttypes.KeyValueStoreAssociation{
			KeyValueStoreARN: aws.String(arn),
		})
	}
	return &cloudfronttypes.KeyValueStoreAssociations{
		Quantity: aws.Int32(int32(len(items))),
		Items:    items,
	}
}

func (r *Function) Create(ctx context.Context, cfg any) (*FunctionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	code, err := r.functionCode()
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateFunction(ctx, &cloudfront.CreateFunctionInput{
		Name:           aws.String(r.Name),
		FunctionConfig: r.config(),
		FunctionCode:   code,
	})
	if err != nil {
		return nil, fmt.Errorf("create function: %w", err)
	}
	// A function is created in the DEVELOPMENT stage. Publishing promotes that
	// code to LIVE, the stage a distribution uses; the create response's ETag
	// is the version PublishFunction guards against.
	if r.publishWanted() {
		_, err = client.PublishFunction(ctx, &cloudfront.PublishFunctionInput{
			Name:    aws.String(r.Name),
			IfMatch: resp.ETag,
		})
		if err != nil {
			return nil, fmt.Errorf("publish function %s: %w", r.Name, err)
		}
	}
	// The create response holds the DEVELOPMENT etag but not the settled status
	// or the LIVE stage etag, and publishing has moved both, so the outputs
	// come from a read.
	return r.read(ctx, client)
}

func (r *Function) Read(
	ctx context.Context, cfg any, prior *FunctionOutput,
) (*FunctionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the function and computes its outputs. CloudFront exposes a
// function in two stages, so the managed fields come from a DEVELOPMENT-stage
// describe, the code from a DEVELOPMENT-stage get, and the LIVE stage etag from
// a second describe against that stage. A gone function maps to
// runtime.ErrNotFound so a plan recreates it. Before a publish the LIVE stage
// does not exist, so a not-found there leaves the live stage etag empty rather
// than failing the read.
func (r *Function) read(
	ctx context.Context, client *cloudfront.Client,
) (*FunctionOutput, error) {
	dev, err := client.DescribeFunction(ctx, &cloudfront.DescribeFunctionInput{
		Name:  aws.String(r.Name),
		Stage: cloudfronttypes.FunctionStageDevelopment,
	})
	if err != nil {
		if isFunctionNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe function %s: %w", r.Name, err)
	}
	out := &FunctionOutput{ETag: aws.ToString(dev.ETag)}
	if summary := dev.FunctionSummary; summary != nil {
		out.Status = aws.ToString(summary.Status)
		if meta := summary.FunctionMetadata; meta != nil {
			out.Arn = aws.ToString(meta.FunctionARN)
		}
	}
	live, err := client.DescribeFunction(ctx, &cloudfront.DescribeFunctionInput{
		Name:  aws.String(r.Name),
		Stage: cloudfronttypes.FunctionStageLive,
	})
	switch {
	case err == nil:
		out.LiveStageETag = aws.ToString(live.ETag)
	case isFunctionNotFound(err):
		// The function has not been published, so the LIVE stage has no version.
	default:
		return nil, fmt.Errorf("describe function %s live stage: %w", r.Name, err)
	}
	return out, nil
}

func (r *Function) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Function, *FunctionOutput],
) (*FunctionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// CloudFront guards every mutating call with the function's current
	// version. The update changes it, so the etag adopted for the publish step
	// is the update response's new etag when an update ran, or the prior read's
	// etag when nothing changed.
	etag := prior.Outputs.ETag
	if r.codeChanged(prior) || r.configChanged(prior) {
		code, err := r.functionCode()
		if err != nil {
			return nil, err
		}
		resp, err := client.UpdateFunction(ctx, &cloudfront.UpdateFunctionInput{
			Name:           aws.String(r.Name),
			IfMatch:        aws.String(etag),
			FunctionConfig: r.config(),
			FunctionCode:   code,
		})
		if err != nil {
			return nil, fmt.Errorf("update function %s: %w", r.Name, err)
		}
		etag = aws.ToString(resp.ETag)
	}
	// Publishing runs on every apply when wanted, even with no field change, so
	// a DEVELOPMENT-only function from an earlier publish=false apply still
	// reaches LIVE once publish becomes true. IfMatch is the current etag.
	if r.publishWanted() {
		_, err = client.PublishFunction(ctx, &cloudfront.PublishFunctionInput{
			Name:    aws.String(r.Name),
			IfMatch: aws.String(etag),
		})
		if err != nil {
			return nil, fmt.Errorf("publish function %s: %w", r.Name, err)
		}
	}
	return r.read(ctx, client)
}

// codeChanged reports whether the function's code source changed against the
// prior inputs.
func (r *Function) codeChanged(prior runtime.Prior[Function, *FunctionOutput]) bool {
	return runtime.Changed(prior.Inputs.CodeContent, r.CodeContent) ||
		runtime.Changed(prior.Inputs.CodePath, r.CodePath)
}

// configChanged reports whether any field that rides FunctionConfig changed
// against the prior inputs.
func (r *Function) configChanged(prior runtime.Prior[Function, *FunctionOutput]) bool {
	return runtime.Changed(prior.Inputs.Comment, r.Comment) ||
		runtime.Changed(prior.Inputs.Runtime, r.Runtime) ||
		runtime.Changed(prior.Inputs.KeyValueStoreAssociations,
			r.KeyValueStoreAssociations)
}

func (r *Function) Delete(ctx context.Context, cfg any, prior *FunctionOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteFunction(ctx, &cloudfront.DeleteFunctionInput{
		Name:    aws.String(r.Name),
		IfMatch: aws.String(prior.ETag),
	})
	if err != nil {
		// A function already gone counts as deleted.
		if isFunctionNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete function %s: %w", r.Name, err)
	}
	return nil
}

// isFunctionNotFound reports whether err is CloudFront's no-such-function error.
// CloudFront models a missing function as its own error type, so a Read matches
// the type to turn a read of a gone function into runtime.ErrNotFound, and a
// Delete treats it as already done. It is named distinctly from the package's
// isNotFound, which matches the origin-access-control error type.
func isFunctionNotFound(err error) bool {
	var notFound *cloudfronttypes.NoSuchFunctionExists
	return errors.As(err, &notFound)
}
