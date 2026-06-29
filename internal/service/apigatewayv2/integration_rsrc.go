package apigatewayv2

import (
	"context"
	"fmt"
	"maps"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// Integration manages an API Gateway v2 integration: the connection between
// an API's routes and the backend that handles their requests, identified by
// the pair of API id and integration id. The API id, the integration type,
// and the AWS service action subtype are fixed at create time, so a change to
// any of them replaces the integration; every other input is reconciled in
// place by a single UpdateIntegration call sent only when something it covers
// changed. Integrations take no tags.
//
// Several inputs apply to one API protocol only, a rule the service enforces
// because the protocol belongs to the referenced API rather than to this
// resource: passthrough-behavior, content-handling-strategy,
// request-templates, and template-selection-expression are WebSocket-only, as
// are the AWS, HTTP, and MOCK integration types; response-parameters,
// tls-config, and integration-subtype work only on HTTP APIs.
// integration-method is required for every type but MOCK, except that AWS and
// AWS_PROXY Lambda integrations default it to POST. timeout-in-millis must be
// between 50 and 29000 on a WebSocket API or 50 and 30000 on an HTTP API,
// defaulting to the maximum, and the valid integration-subtype values are the
// AWS service action catalog. When omitted, the server also fills
// connection-type as INTERNET, payload-format-version as 1.0, and, on
// WebSocket APIs only, passthrough-behavior as WHEN_NO_MATCH; none of these
// is fabricated client-side.
//
// Removal behaves differently per field. The plain strings description,
// credentials-arn, integration-method, integration-uri, connection-id, and
// template-selection-expression clear on the next update when removed. The
// enum-valued connection-type, content-handling-strategy, and
// passthrough-behavior, along with payload-format-version and
// timeout-in-millis, cannot be cleared once set: removing one leaves the
// stored value, and moving off a VPC link is an explicit change of
// connection-type to INTERNET. The update API merges request-parameters, so
// removed keys are deleted with an explicit empty value while the rest are
// re-sent; request-templates has no removal sentinel, an empty string being a
// valid template, so a removed template key may remain on the integration. A
// response-parameters status code that is removed has its mappings deleted
// with an empty set, and a removed tls-config block is cleared with an empty
// object. Length limits and syntax are left to the API: connection-id
// (1-1024), integration-subtype (1-128), parameter values (1-512), templates
// (up to 32768), the response-parameters status code range, and the
// credentials-arn ARN form.
type Integration struct {
	ApiId                       string                          `ub:"api-id"`
	IntegrationType             string                          `ub:"integration-type"`
	ConnectionId                *string                         `ub:"connection-id"`
	ConnectionType              *string                         `ub:"connection-type"`
	ContentHandlingStrategy     *string                         `ub:"content-handling-strategy"`
	CredentialsArn              *string                         `ub:"credentials-arn"`
	Description                 *string                         `ub:"description"`
	IntegrationMethod           *string                         `ub:"integration-method"`
	IntegrationSubtype          *string                         `ub:"integration-subtype"`
	IntegrationUri              *string                         `ub:"integration-uri"`
	PassthroughBehavior         *string                         `ub:"passthrough-behavior"`
	PayloadFormatVersion        *string                         `ub:"payload-format-version"`
	RequestParameters           *map[string]string              `ub:"request-parameters"`
	RequestTemplates            *map[string]string              `ub:"request-templates"`
	ResponseParameters          *[]IntegrationResponseParameter `ub:"response-parameters"`
	TemplateSelectionExpression *string                         `ub:"template-selection-expression"`
	TimeoutInMillis             *int64                          `ub:"timeout-in-millis"`
	TlsConfig                   *IntegrationTlsConfig           `ub:"tls-config"`
}

// IntegrationOutput holds the integration's identity and its one computed
// attribute. The integration id and the API id together are the identity
// handle: Read, Update, and Delete key off them from the prior outputs, so a
// replace across APIs still deletes the old integration. The API id echoes an
// input, which is safe here because an integration cannot move between APIs,
// so the echo cannot drift. The integration response selection expression is
// filled by the service for WebSocket non-proxy integrations and is empty
// otherwise.
type IntegrationOutput struct {
	ApiId                                  string `ub:"api-id"`
	IntegrationId                          string `ub:"integration-id"`
	IntegrationResponseSelectionExpression string `ub:"integration-response-selection-expression"`
}

func (r *Integration) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs fixed when an integration is created. The
// API id names the parent API and CloudFormation marks it create-only; the
// integration type and the AWS service action subtype decide what kind of
// integration this is, and a different kind is a different integration, so
// adding, removing, or changing either one replaces it. Every other input is
// reconciled in place by UpdateIntegration.
func (r *Integration) ReplaceFields() []string {
	return []string{
		"api-id",
		"integration-type",
		"integration-subtype",
	}
}

// Constraints declares the rules the service places on an integration's
// inputs that depend on this resource's fields alone: the integration type,
// connection type, content handling, passthrough behavior, payload format
// version, and HTTP method enums; the rule that a subtype is the AWS-service
// form of an AWS_PROXY integration; and the rule that a VPC link connection
// names its link. Rules that branch on the referenced API's protocol are left
// to the API.
func (r Integration) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.IntegrationType,
			"AWS", "AWS_PROXY", "HTTP", "HTTP_PROXY", "MOCK")).
			Message("integration-type must be AWS, AWS_PROXY, HTTP, HTTP_PROXY, or MOCK"),
		constraint.When(constraint.Present(r.IntegrationSubtype)).
			Require(constraint.Equals(r.IntegrationType, "AWS_PROXY")).
			Message("integration-subtype requires integration-type AWS_PROXY"),
		constraint.When(constraint.Equals(r.ConnectionType, "VPC_LINK")).
			Require(constraint.Present(r.ConnectionId)).
			Message("connection-type VPC_LINK requires connection-id"),
		constraint.When(constraint.Present(r.ConnectionType)).
			Require(constraint.OneOf(r.ConnectionType, "INTERNET", "VPC_LINK")).
			Message("connection-type must be INTERNET or VPC_LINK"),
		constraint.When(constraint.Present(r.ContentHandlingStrategy)).
			Require(constraint.OneOf(r.ContentHandlingStrategy,
				"CONVERT_TO_BINARY", "CONVERT_TO_TEXT")).
			Message("content-handling-strategy must be CONVERT_TO_BINARY or CONVERT_TO_TEXT"),
		constraint.When(constraint.Present(r.PassthroughBehavior)).
			Require(constraint.OneOf(r.PassthroughBehavior,
				"WHEN_NO_MATCH", "NEVER", "WHEN_NO_TEMPLATES")).
			Message("passthrough-behavior must be WHEN_NO_MATCH, NEVER, or WHEN_NO_TEMPLATES"),
		constraint.When(constraint.Present(r.PayloadFormatVersion)).
			Require(constraint.OneOf(r.PayloadFormatVersion, "1.0", "2.0")).
			Message("payload-format-version must be 1.0 or 2.0"),
		constraint.When(constraint.Present(r.IntegrationMethod)).
			Require(constraint.OneOf(r.IntegrationMethod,
				"ANY", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT")).
			Message("integration-method must be a valid HTTP method"),
	}
}

// Create makes the integration with one CreateIntegration call; optional
// inputs are sent only when set, so the server applies its own defaults to
// the rest. The service reads back a just-created integration immediately,
// and the create response is the full integration representation, so the
// outputs come straight from it with no settling wait.
func (r *Integration) Create(ctx context.Context, cfg *awsCfg) (*IntegrationOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &apigatewayv2.CreateIntegrationInput{
		ApiId:                       aws.String(r.ApiId),
		IntegrationType:             apigatewayv2types.IntegrationType(r.IntegrationType),
		ConnectionId:                r.ConnectionId,
		CredentialsArn:              r.CredentialsArn,
		Description:                 r.Description,
		IntegrationMethod:           r.IntegrationMethod,
		IntegrationSubtype:          r.IntegrationSubtype,
		IntegrationUri:              r.IntegrationUri,
		PayloadFormatVersion:        r.PayloadFormatVersion,
		RequestParameters:           ptr.Value(r.RequestParameters),
		RequestTemplates:            ptr.Value(r.RequestTemplates),
		ResponseParameters:          integrationResponseParameterMap(ptr.Value(r.ResponseParameters)),
		TemplateSelectionExpression: r.TemplateSelectionExpression,
		TimeoutInMillis:             ptr.Int32(r.TimeoutInMillis),
		TlsConfig:                   r.TlsConfig.sdk(),
	}
	if r.ConnectionType != nil {
		in.ConnectionType = apigatewayv2types.ConnectionType(*r.ConnectionType)
	}
	if r.ContentHandlingStrategy != nil {
		in.ContentHandlingStrategy =
			apigatewayv2types.ContentHandlingStrategy(*r.ContentHandlingStrategy)
	}
	if r.PassthroughBehavior != nil {
		in.PassthroughBehavior = apigatewayv2types.PassthroughBehavior(*r.PassthroughBehavior)
	}
	var resp *apigatewayv2.CreateIntegrationOutput
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateIntegration(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create integration: %w", err)
	}
	return &IntegrationOutput{
		ApiId:         r.ApiId,
		IntegrationId: aws.ToString(resp.IntegrationId),
		IntegrationResponseSelectionExpression: aws.ToString(
			resp.IntegrationResponseSelectionExpression),
	}, nil
}

// Read fetches the integration by the identity pair in the prior outputs.
// The service's typed not-found maps to runtime.ErrNotFound whether the
// integration is gone or the whole API is, since either way this integration
// no longer exists; a response with no body is an error rather than drift.
func (r *Integration) Read(
	ctx context.Context, cfg *awsCfg, prior *IntegrationOutput,
) (*IntegrationOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetIntegration(ctx, &apigatewayv2.GetIntegrationInput{
		ApiId:         aws.String(prior.ApiId),
		IntegrationId: aws.String(prior.IntegrationId),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get integration: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("get integration %s: empty response", prior.IntegrationId)
	}
	return &IntegrationOutput{
		ApiId:         prior.ApiId,
		IntegrationId: aws.ToString(resp.IntegrationId),
		IntegrationResponseSelectionExpression: aws.ToString(
			resp.IntegrationResponseSelectionExpression),
	}, nil
}

// Update reconciles every changed input with one UpdateIntegration call,
// skipped entirely when nothing it covers changed. The call always restates
// the integration type, which the service expects in every update even
// though a type change replaces the integration instead, and the subtype
// whenever one is configured, because AWS service integrations reject an
// update that omits it. The update response is the full integration
// representation, so the outputs come from it directly.
func (r *Integration) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Integration, *IntegrationOutput],
) (*IntegrationOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in, needed := r.updateIntegrationInput(prior)
	if !needed {
		return prior.Outputs, nil
	}
	var resp *apigatewayv2.UpdateIntegrationOutput
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.UpdateIntegration(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("update integration: %w", err)
	}
	return &IntegrationOutput{
		ApiId:         prior.Outputs.ApiId,
		IntegrationId: aws.ToString(resp.IntegrationId),
		IntegrationResponseSelectionExpression: aws.ToString(
			resp.IntegrationResponseSelectionExpression),
	}, nil
}

// updateIntegrationInput builds the update call for the inputs that changed
// and reports whether any did. A removed string input clears with an explicit
// empty value, since omitting it would leave the stored value; the enum
// fields, the payload format version, and the timeout are omitted when
// removed, leaving the stored value, because the SDK never serializes an
// empty enum and the timeout has no documented clear; the map fields apply
// their removal sentinels; and a removed tls-config block sends the
// empty-object clear, a nil member meaning leave unchanged.
func (r *Integration) updateIntegrationInput(
	prior runtime.Prior[Integration, *IntegrationOutput],
) (*apigatewayv2.UpdateIntegrationInput, bool) {
	in := &apigatewayv2.UpdateIntegrationInput{
		ApiId:              aws.String(prior.Outputs.ApiId),
		IntegrationId:      aws.String(prior.Outputs.IntegrationId),
		IntegrationType:    apigatewayv2types.IntegrationType(r.IntegrationType),
		IntegrationSubtype: r.IntegrationSubtype,
	}
	changed := false
	if runtime.Changed(prior.Inputs.ConnectionId, r.ConnectionId) {
		in.ConnectionId = aws.String(aws.ToString(r.ConnectionId))
		changed = true
	}
	if runtime.Changed(prior.Inputs.CredentialsArn, r.CredentialsArn) {
		in.CredentialsArn = aws.String(aws.ToString(r.CredentialsArn))
		changed = true
	}
	if runtime.Changed(prior.Inputs.Description, r.Description) {
		in.Description = aws.String(aws.ToString(r.Description))
		changed = true
	}
	if runtime.Changed(prior.Inputs.IntegrationMethod, r.IntegrationMethod) {
		in.IntegrationMethod = aws.String(aws.ToString(r.IntegrationMethod))
		changed = true
	}
	if runtime.Changed(prior.Inputs.IntegrationUri, r.IntegrationUri) {
		in.IntegrationUri = aws.String(aws.ToString(r.IntegrationUri))
		changed = true
	}
	if runtime.Changed(prior.Inputs.TemplateSelectionExpression, r.TemplateSelectionExpression) {
		in.TemplateSelectionExpression =
			aws.String(aws.ToString(r.TemplateSelectionExpression))
		changed = true
	}
	if r.ConnectionType != nil && runtime.Changed(prior.Inputs.ConnectionType, r.ConnectionType) {
		in.ConnectionType = apigatewayv2types.ConnectionType(*r.ConnectionType)
		changed = true
	}
	if r.ContentHandlingStrategy != nil &&
		runtime.Changed(prior.Inputs.ContentHandlingStrategy, r.ContentHandlingStrategy) {
		in.ContentHandlingStrategy =
			apigatewayv2types.ContentHandlingStrategy(*r.ContentHandlingStrategy)
		changed = true
	}
	if r.PassthroughBehavior != nil &&
		runtime.Changed(prior.Inputs.PassthroughBehavior, r.PassthroughBehavior) {
		in.PassthroughBehavior = apigatewayv2types.PassthroughBehavior(*r.PassthroughBehavior)
		changed = true
	}
	if r.PayloadFormatVersion != nil &&
		runtime.Changed(prior.Inputs.PayloadFormatVersion, r.PayloadFormatVersion) {
		in.PayloadFormatVersion = r.PayloadFormatVersion
		changed = true
	}
	if r.TimeoutInMillis != nil &&
		runtime.Changed(prior.Inputs.TimeoutInMillis, r.TimeoutInMillis) {
		in.TimeoutInMillis = ptr.Int32(r.TimeoutInMillis)
		changed = true
	}
	if runtime.Changed(ptr.Value(prior.Inputs.RequestParameters), ptr.Value(r.RequestParameters)) {
		in.RequestParameters = integrationRequestParameterUpdates(
			ptr.Value(prior.Inputs.RequestParameters), ptr.Value(r.RequestParameters))
		changed = true
	}
	if runtime.Changed(ptr.Value(prior.Inputs.RequestTemplates), ptr.Value(r.RequestTemplates)) {
		templates := ptr.Value(r.RequestTemplates)
		if templates == nil {
			templates = map[string]string{}
		}
		in.RequestTemplates = templates
		changed = true
	}
	if runtime.Changed(ptr.Value(prior.Inputs.ResponseParameters), ptr.Value(r.ResponseParameters)) {
		in.ResponseParameters = integrationResponseParameterUpdates(
			ptr.Value(prior.Inputs.ResponseParameters), ptr.Value(r.ResponseParameters))
		changed = true
	}
	if runtime.Changed(prior.Inputs.TlsConfig, r.TlsConfig) {
		tls := r.TlsConfig.sdk()
		if tls == nil {
			tls = &apigatewayv2types.TlsConfigInput{}
		}
		in.TlsConfig = tls
		changed = true
	}
	return in, changed
}

// Delete removes the integration named by the prior outputs; on a replace the
// receiver already describes the new integration, so the identity pair comes
// from prior. An integration already gone, or whose whole API is gone, counts
// as deleted.
func (r *Integration) Delete(ctx context.Context, cfg *awsCfg, prior *IntegrationOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.DeleteIntegration(ctx, &apigatewayv2.DeleteIntegrationInput{
			ApiId:         aws.String(prior.ApiId),
			IntegrationId: aws.String(prior.IntegrationId),
		})
		return err
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete integration: %w", err)
	}
	return nil
}

// integrationRequestParameterUpdates builds the request-parameters member of
// an update. The API merges the map it is sent, so the result restates every
// desired entry, including unchanged ones, because AWS service integrations
// reject an update that omits the action's required parameters; each key that
// was configured before and is absent now is sent with an empty value, the
// documented way to delete a mapping. Removing the whole map empties every
// prior key.
func integrationRequestParameterUpdates(prior, desired map[string]string) map[string]string {
	updates := make(map[string]string, len(desired)+len(prior))
	maps.Copy(updates, desired)
	for k := range prior {
		if _, ok := desired[k]; !ok {
			updates[k] = ""
		}
	}
	return updates
}
