package lambda

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// FunctionUrl gives a Lambda function, or one of its aliases, a dedicated
// HTTPS endpoint, the way CloudFormation models AWS::Lambda::Url. The function
// and the alias qualifier identify the endpoint and are fixed at creation; the
// authentication type, the CORS settings, and the invoke mode reconcile in
// place through UpdateFunctionUrlConfig. Setting auth-type to NONE does not by
// itself open the endpoint to anonymous callers: invocation also requires a
// lambda:InvokeFunctionUrl statement with function-url-auth-type NONE and
// principal "*" on the function's resource policy, which belongs to the
// lambda-permission resource, so pair the two.
type FunctionUrl struct {
	// FunctionName names the function the endpoint fronts, as a function
	// name, a partial ARN, or a full ARN.
	FunctionName string `ub:"function-name"`
	// AuthType is how callers authenticate: AWS_IAM to require IAM
	// authorization, or NONE for a public endpoint.
	AuthType string `ub:"auth-type"`
	// Qualifier is the alias the endpoint is attached to. When unset the
	// endpoint addresses the function's unpublished version.
	Qualifier *string `ub:"qualifier"`
	// InvokeMode selects BUFFERED, where Lambda returns the response once the
	// payload is complete, or RESPONSE_STREAM, where the function streams it.
	// Unset leaves the AWS default of BUFFERED; removing the field later
	// leaves the live mode unchanged, so set BUFFERED explicitly to return a
	// streaming endpoint to the default.
	InvokeMode *string `ub:"invoke-mode"`
	// Cors holds the cross-origin resource sharing settings the endpoint
	// answers browser preflight requests with. Removing the block clears the
	// settings on the next update.
	Cors *FunctionUrlCors `ub:"cors"`
}

// FunctionUrlOutput holds what Lambda computes for a function URL.
// FunctionUrl is the endpoint itself. FunctionArn is the canonical function
// ARN, alias-qualified when the endpoint is on an alias; together with the
// qualifier echo it keys every later read, update, and delete, because both
// identity inputs are replace fields and a replacement's delete must find the
// old endpoint through the prior outputs. UrlId is the endpoint's subdomain
// label, parsed from the URL because the API has no field for it.
type FunctionUrlOutput struct {
	FunctionUrl string `ub:"function-url"`
	FunctionArn string `ub:"function-arn"`
	UrlId       string `ub:"url-id"`
	Qualifier   string `ub:"qualifier"`
}

func (r *FunctionUrl) SchemaVersion() int { return 1 }

// ReplaceFields lists the two identity inputs. A function URL belongs to one
// function and one alias; Lambda has no call to move it, so a change to either
// replaces the endpoint, and the replacement is assigned a new URL.
func (r *FunctionUrl) ReplaceFields() []string {
	return []string{
		"function-name",
		"qualifier",
	}
}

// Constraints declares the two enum rules and the CORS max-age bound. The
// remaining CORS rules, such as allow-methods taking HTTP method names or "*",
// are enforced by the API.
func (r FunctionUrl) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.AuthType, "AWS_IAM", "NONE")).
			Message("auth-type must be AWS_IAM or NONE"),
		constraint.When(constraint.Present(r.InvokeMode)).
			Require(constraint.OneOf(r.InvokeMode, "BUFFERED", "RESPONSE_STREAM")).
			Message("invoke-mode must be BUFFERED or RESPONSE_STREAM"),
		constraint.When(constraint.Present(r.Cors.MaxAge)).
			Require(constraint.AtLeast(r.Cors.MaxAge, 0),
				constraint.AtMost(r.Cors.MaxAge, 86400)).
			Message("cors max-age must be between 0 and 86400 seconds"),
	}
}

func (r *FunctionUrl) Create(ctx context.Context, cfg *awsCfg) (*FunctionUrlOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &lambda.CreateFunctionUrlConfigInput{
		FunctionName: aws.String(r.FunctionName),
		AuthType:     lambdatypes.FunctionUrlAuthType(r.AuthType),
		Cors:         r.Cors.to(),
	}
	if r.InvokeMode != nil {
		in.InvokeMode = lambdatypes.InvokeMode(*r.InvokeMode)
	}
	qualifier := aws.ToString(r.Qualifier)
	if qualifier != "" {
		in.Qualifier = aws.String(qualifier)
	}
	resp, err := client.CreateFunctionUrlConfig(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("create function url config: %w", err)
	}
	// The create response already reports the final endpoint and ARN and a
	// new config needs no settling, so there is no follow-up read.
	return functionUrlOutput(aws.ToString(resp.FunctionUrl), aws.ToString(resp.FunctionArn),
		qualifier)
}

// Read refreshes the endpoint by the prior outputs: the function ARN, which
// GetFunctionUrlConfig accepts as the function name and which matches the
// prior qualifier when both are set, so the lookup still finds the old
// endpoint while a replacement is pending. A not-found, whether the config or
// the whole function is gone, maps to runtime.ErrNotFound.
func (r *FunctionUrl) Read(
	ctx context.Context, cfg *awsCfg, prior *FunctionUrlOutput,
) (*FunctionUrlOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &lambda.GetFunctionUrlConfigInput{
		FunctionName: aws.String(prior.FunctionArn),
	}
	if prior.Qualifier != "" {
		in.Qualifier = aws.String(prior.Qualifier)
	}
	resp, err := client.GetFunctionUrlConfig(ctx, in)
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get function url config: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("get function url config %s: empty response", prior.FunctionArn)
	}
	return functionUrlOutput(aws.ToString(resp.FunctionUrl), aws.ToString(resp.FunctionArn),
		prior.Qualifier)
}

// Update reconciles the three mutable fields through one partial
// UpdateFunctionUrlConfig: each member is set only when its input changed, and
// an omitted member keeps its live value, so the same config applied twice
// sends nothing. A removed cors block is sent as the empty struct, the
// documented clear, where a nil member would silently leave the old settings
// in place. A removed invoke-mode is not sent at all, per the scalar-removal
// rule. The identity comes from the prior outputs, like Read and Delete.
func (r *FunctionUrl) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[FunctionUrl, *FunctionUrlOutput],
) (*FunctionUrlOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &lambda.UpdateFunctionUrlConfigInput{
		FunctionName: aws.String(prior.Outputs.FunctionArn),
	}
	if prior.Outputs.Qualifier != "" {
		in.Qualifier = aws.String(prior.Outputs.Qualifier)
	}
	changed := false
	if runtime.Changed(prior.Inputs.AuthType, r.AuthType) {
		in.AuthType = lambdatypes.FunctionUrlAuthType(r.AuthType)
		changed = true
	}
	if runtime.Changed(prior.Inputs.InvokeMode, r.InvokeMode) && r.InvokeMode != nil {
		in.InvokeMode = lambdatypes.InvokeMode(*r.InvokeMode)
		changed = true
	}
	if runtime.Changed(prior.Inputs.Cors, r.Cors) {
		cors := r.Cors.to()
		if cors == nil {
			cors = &lambdatypes.Cors{}
		}
		in.Cors = cors
		changed = true
	}
	if !changed {
		// An Update with no input change runs only when the outputs drifted,
		// such as the URL having been deleted and recreated out of band under
		// the same function and qualifier, which mints a new endpoint. The
		// observed outputs are the live ones, so they are adopted; returning
		// the recorded outputs would keep the stale URL forever.
		if prior.Observed != nil {
			return prior.Observed, nil
		}
		return prior.Outputs, nil
	}
	resp, err := client.UpdateFunctionUrlConfig(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("update function url config: %w", err)
	}
	return functionUrlOutput(aws.ToString(resp.FunctionUrl), aws.ToString(resp.FunctionArn),
		prior.Outputs.Qualifier)
}

// Delete removes the endpoint, keyed by the prior outputs because both
// identity inputs are replace fields and on a replacement the receiver already
// holds the new ones. A not-found is success: the config is already gone,
// which deleting the function itself also brings about.
func (r *FunctionUrl) Delete(ctx context.Context, cfg *awsCfg, prior *FunctionUrlOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &lambda.DeleteFunctionUrlConfigInput{
		FunctionName: aws.String(prior.FunctionArn),
	}
	if prior.Qualifier != "" {
		in.Qualifier = aws.String(prior.Qualifier)
	}
	if _, err := client.DeleteFunctionUrlConfig(ctx, in); err != nil && !isNotFound(err) {
		return fmt.Errorf("delete function url config: %w", err)
	}
	return nil
}

// functionUrlOutput assembles the resource outputs from the endpoint and
// function ARN a response reports, plus the qualifier the config is addressed
// by. The url-id has no API field; it is the first label of the endpoint host
// (https://<url-id>.lambda-url.<region>.on.aws/), so it is parsed out here.
func functionUrlOutput(endpoint, arn, qualifier string) (*FunctionUrlOutput, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse function url %q: %w", endpoint, err)
	}
	return &FunctionUrlOutput{
		FunctionUrl: endpoint,
		FunctionArn: arn,
		UrlId:       strings.Split(u.Host, ".")[0],
		Qualifier:   qualifier,
	}, nil
}
