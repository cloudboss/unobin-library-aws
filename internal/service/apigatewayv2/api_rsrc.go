package apigatewayv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
	"gopkg.in/yaml.v3"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// Api manages an API Gateway v2 API, the root of an HTTP or WebSocket API:
// its protocol, name, selection expressions, CORS configuration, and tags,
// plus the OpenAPI import that can populate it. The protocol is fixed at
// create time. The quick-create trio (target, route-key, credentials-arn)
// rides the create call only and materializes an integration, a catch-all
// route, and an auto-deployed default stage that this resource does not
// manage afterward, so a change to any of the three replaces the API. An
// OpenAPI body is applied with ReimportApi, which replaces the API's routes
// and integrations and overwrites API-level properties from the document;
// the resource then re-asserts the configured name, description, version,
// CORS, and tags, so a value set in configuration wins over the document.
type Api struct {
	// Name identifies the API, 1 to 128 characters. The bound is checked in
	// validate, since AWS counts characters and the constraint layer counts
	// bytes.
	Name string `ub:"name"`
	// ProtocolType is HTTP or WEBSOCKET and cannot change on an existing
	// API. It decides what else may be set: the quick-create trio, the CORS
	// block, and the OpenAPI import are HTTP-only features.
	ProtocolType string `ub:"protocol-type"`
	// ApiKeySelectionExpression is meaningful for WebSocket APIs, though AWS
	// stores and echoes it for HTTP APIs too. Unset leaves the server
	// default, $request.header.x-api-key.
	ApiKeySelectionExpression *string `ub:"api-key-selection-expression"`
	// CorsConfiguration is the CORS configuration of an HTTP API. Removing
	// the block deletes the configuration with its own call; changing it
	// replaces the whole Cors object.
	CorsConfiguration *ApiCors `ub:"cors-configuration"`
	// CredentialsArn is part of quick create: the credentials the
	// quick-created integration uses, for HTTP APIs only. It rides the
	// create call alone and the API never echoes it back, so out-of-band
	// drift on it is undetectable and a change replaces the API.
	CredentialsArn *string `ub:"credentials-arn"`
	// Description is at most 1024 characters, checked in validate. Removing
	// it leaves the stored description alone.
	Description *string `ub:"description"`
	// DisableExecuteApiEndpoint turns off the default execute-api endpoint
	// so clients must use a custom domain name. Unset leaves the cloud
	// value alone; an explicit false re-enables the default endpoint.
	DisableExecuteApiEndpoint *bool `ub:"disable-execute-api-endpoint"`
	// DisableSchemaValidation skips request-body schema validation, for
	// WebSocket APIs only; AWS rejects it elsewhere. Unset leaves the cloud
	// value alone; an explicit false re-enables validation.
	DisableSchemaValidation *bool `ub:"disable-schema-validation"`
	// IpAddressType is ipv4 or dualstack. Unset lets the server choose
	// (ipv4) and, on update, leaves the cloud value alone.
	IpAddressType *string `ub:"ip-address-type"`
	// RouteKey is part of quick create: the key of the quick-created route,
	// for HTTP APIs only, defaulting to the $default catch-all. Like the
	// rest of the trio it is write-only and a change replaces the API.
	RouteKey *string `ub:"route-key"`
	// RouteSelectionExpression routes requests to WebSocket routes, such as
	// $request.body.action. HTTP APIs accept only
	// "$request.method $request.path", which is also the server default.
	RouteSelectionExpression *string `ub:"route-selection-expression"`
	// Target is part of quick create: a URL for an HTTP_PROXY integration
	// or a Lambda function ARN for AWS_PROXY, HTTP APIs only. Write-only;
	// a change replaces the API.
	Target *string `ub:"target"`
	// Version is a version identifier, 1 to 64 characters, checked in
	// validate.
	Version *string `ub:"version"`
	// Body is an OpenAPI definition, JSON or YAML, for an HTTP API. It is
	// applied by import, which replaces the API's routes and integrations
	// and overwrites API-level properties from the document; the configured
	// name, description, version, CORS, and tags are re-asserted after each
	// import. Removing the body makes no call, leaving the imported routes
	// in place.
	Body *string `ub:"body"`
	// BasePath tells an import how to interpret the base path of the
	// document: ignore, prepend, or split. It parameterizes the import call
	// (the SDK member is spelled Basepath), so changing it by itself does
	// nothing.
	BasePath *string `ub:"base-path"`
	// FailOnWarnings makes the import fail when the document produces
	// warnings; by default warnings are tolerated. It rides the import call
	// only, so changing it by itself does nothing.
	FailOnWarnings *bool `ub:"fail-on-warnings"`
	// Tags label the API. A document with its own tags section replaces the
	// API's tag set on import, so the resource re-syncs this map after every
	// import: tags belong here rather than in the body.
	Tags *map[string]string `ub:"tags"`
}

// ApiOutput holds the values computed for an API. ApiId is the stable handle
// reads, updates, and deletes key off, and the join key the integration,
// route, and stage resources reference. ApiEndpoint is the server-assigned
// URI, https for an HTTP API and wss for a WebSocket API. Arn is the
// apigateway ARN, composed from the partition and region since GetApi does
// not return one; it is the identifier the tagging calls take.
type ApiOutput struct {
	ApiId       string `ub:"api-id"`
	ApiEndpoint string `ub:"api-endpoint"`
	Arn         string `ub:"arn"`
}

func (r *Api) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs fixed at create time. The protocol cannot
// change on an existing API. The quick-create trio (credentials-arn,
// route-key, target) rides the create call only: the integration, route, and
// stage it produces are not managed by this resource afterward, and the API
// never echoes the three back, so a change takes a new API rather than an
// update the resource could not verify. CloudFormation marks only the
// protocol create-only; the trio follows the write-only quick-create
// behavior instead.
func (r *Api) ReplaceFields() []string {
	return []string{
		"protocol-type",
		"credentials-arn",
		"route-key",
		"target",
	}
}

// Constraints declares the rules API Gateway places on an API's inputs. The
// protocol decides what else may be set: the CORS block, the quick-create
// trio, and the OpenAPI import belong to HTTP APIs, and an HTTP API accepts
// only the one fixed route selection expression. The name, description, and
// version length bounds are counted in characters, which the constraint
// layer measures in bytes, so they are checked in validate along with the
// body's JSON-or-YAML syntax.
func (r Api) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.ProtocolType, "HTTP", "WEBSOCKET")).
			Message("protocol-type must be HTTP or WEBSOCKET"),
		constraint.When(constraint.Present(r.IpAddressType)).
			Require(constraint.OneOf(r.IpAddressType, "ipv4", "dualstack")).
			Message("ip-address-type must be ipv4 or dualstack"),
		constraint.When(constraint.Present(r.ApiKeySelectionExpression)).
			Require(constraint.OneOf(r.ApiKeySelectionExpression,
				"$context.authorizer.usageIdentifierKey", "$request.header.x-api-key")).
			Message("api-key-selection-expression must be " +
				"$context.authorizer.usageIdentifierKey or $request.header.x-api-key"),
		constraint.When(constraint.Equals(r.ProtocolType, "WEBSOCKET")).
			Require(constraint.Absent(r.CorsConfiguration),
				constraint.Absent(r.CredentialsArn),
				constraint.Absent(r.RouteKey),
				constraint.Absent(r.Target),
				constraint.Absent(r.Body),
				constraint.Absent(r.FailOnWarnings)).
			Message("cors-configuration, credentials-arn, route-key, target, body, " +
				"and fail-on-warnings are supported only for HTTP APIs"),
		constraint.When(constraint.Equals(r.ProtocolType, "WEBSOCKET")).
			Require(constraint.Present(r.RouteSelectionExpression)).
			Message("route-selection-expression is required for a WebSocket API"),
		constraint.When(constraint.All(constraint.Equals(r.ProtocolType, "HTTP"),
			constraint.Present(r.RouteSelectionExpression))).
			Require(constraint.OneOf(r.RouteSelectionExpression,
				"$request.method $request.path")).
			Message("route-selection-expression for an HTTP API must be " +
				"$request.method $request.path"),
		constraint.When(constraint.Present(r.BasePath)).
			Require(constraint.OneOf(r.BasePath, "ignore", "prepend", "split")).
			Message("base-path must be ignore, prepend, or split"),
		constraint.RequiredWith(r.FailOnWarnings, r.Body).
			Message("fail-on-warnings applies only to a body import"),
		constraint.RequiredWith(r.BasePath, r.Body).
			Message("base-path applies only to a body import"),
	}
}

// validate checks the rules the constraint layer cannot express: the name,
// description, and version length bounds, which AWS counts in characters
// rather than bytes, and the requirement that the body parse as JSON or
// YAML, which API Gateway rejects otherwise.
func (r *Api) validate() error {
	if n := utf8.RuneCountInString(r.Name); n < 1 || n > 128 {
		return errors.New("name must be between 1 and 128 characters")
	}
	if r.Description != nil && utf8.RuneCountInString(*r.Description) > 1024 {
		return errors.New("description must be at most 1024 characters")
	}
	if r.Version != nil {
		if n := utf8.RuneCountInString(*r.Version); n < 1 || n > 64 {
			return errors.New("version must be between 1 and 64 characters")
		}
	}
	if r.Body != nil && !json.Valid([]byte(*r.Body)) {
		var doc any
		if err := yaml.Unmarshal([]byte(*r.Body), &doc); err != nil {
			return fmt.Errorf("body must be valid JSON or YAML: %w", err)
		}
	}
	return nil
}

func (r *Api) Create(ctx context.Context, cfg *awsCfg) (*ApiOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &apigatewayv2.CreateApiInput{
		Name:                      aws.String(r.Name),
		ProtocolType:              apigatewayv2types.ProtocolType(r.ProtocolType),
		ApiKeySelectionExpression: r.ApiKeySelectionExpression,
		CorsConfiguration:         apiCorsToSDK(r.CorsConfiguration),
		CredentialsArn:            r.CredentialsArn,
		Description:               r.Description,
		DisableExecuteApiEndpoint: r.DisableExecuteApiEndpoint,
		DisableSchemaValidation:   r.DisableSchemaValidation,
		RouteKey:                  r.RouteKey,
		RouteSelectionExpression:  r.RouteSelectionExpression,
		Target:                    r.Target,
		Version:                   r.Version,
	}
	if r.IpAddressType != nil {
		in.IpAddressType = apigatewayv2types.IpAddressType(*r.IpAddressType)
	}
	if len(ptr.Value(r.Tags)) > 0 {
		in.Tags = ptr.Value(r.Tags)
	}
	var resp *apigatewayv2.CreateApiOutput
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateApi(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create api: %w", err)
	}
	if resp == nil || resp.ApiId == nil {
		return nil, errors.New("create api: empty response")
	}
	apiID := aws.ToString(resp.ApiId)
	arn := apiARN(client.Options().Region, apiID)
	endpoint := aws.ToString(resp.ApiEndpoint)
	if r.Body != nil {
		// A failed import here would leave an API the state never recorded,
		// so the just-created API is removed, best-effort, before the error
		// returns; it is seconds old and nothing references it yet. The
		// import's own post-import read supplies the outputs, so no call
		// runs after the cleanup envelope closes.
		imported, err := r.reimport(ctx, client, apiID, arn)
		if err != nil {
			_, _ = client.DeleteApi(ctx, &apigatewayv2.DeleteApiInput{
				ApiId: aws.String(apiID),
			})
			return nil, err
		}
		endpoint = aws.ToString(imported.ApiEndpoint)
	}
	return &ApiOutput{
		ApiId:       apiID,
		ApiEndpoint: endpoint,
		Arn:         arn,
	}, nil
}

func (r *Api) Read(ctx context.Context, cfg *awsCfg, prior *ApiOutput) (*ApiOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.ApiId)
}

// read fetches the API by id and returns its computed outputs. The typed
// NotFoundException maps to runtime.ErrNotFound so a missing API reads as
// drift; an empty response body is an error, never a not-found.
func (r *Api) read(
	ctx context.Context, client *apigatewayv2.Client, apiID string,
) (*ApiOutput, error) {
	var resp *apigatewayv2.GetApiOutput
	err := withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.GetApi(ctx, &apigatewayv2.GetApiInput{ApiId: aws.String(apiID)})
		return err
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get api %s: %w", apiID, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("get api %s: empty response", apiID)
	}
	return &ApiOutput{
		ApiId:       aws.ToString(resp.ApiId),
		ApiEndpoint: aws.ToString(resp.ApiEndpoint),
		Arn:         apiARN(client.Options().Region, aws.ToString(resp.ApiId)),
	}, nil
}

func (r *Api) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Api, *ApiOutput],
) (*ApiOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	apiID := prior.Outputs.ApiId
	// A removed CORS block takes a dedicated delete call; an UpdateApi with
	// an empty Cors object would not clear it.
	if prior.Inputs.CorsConfiguration != nil && r.CorsConfiguration == nil {
		if err := r.deleteCors(ctx, client, apiID); err != nil {
			return nil, err
		}
	}
	if in, changed := r.updateInput(prior.Inputs, apiID); changed {
		err := withConflictRetry(ctx, func(ctx context.Context) error {
			_, err := client.UpdateApi(ctx, in)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("update api: %w", err)
		}
	}
	// The import is gated on the body alone: a change to fail-on-warnings by
	// itself makes no call, and a removed body leaves the imported routes and
	// integrations in place.
	if r.Body != nil && runtime.Changed(prior.Inputs.Body, r.Body) {
		if _, err := r.reimport(ctx, client, apiID, prior.Outputs.Arn); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		err := withConflictRetry(ctx, func(ctx context.Context) error {
			return syncResourceTags(ctx, client, prior.Outputs.Arn, ptr.Value(r.Tags))
		})
		if err != nil {
			return nil, fmt.Errorf("sync tags: %w", err)
		}
	}
	// The id, the endpoint, and the composed ARN cannot change in place, so
	// the prior outputs stand.
	return prior.Outputs, nil
}

func (r *Api) Delete(ctx context.Context, cfg *awsCfg, prior *ApiOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.DeleteApi(ctx, &apigatewayv2.DeleteApiInput{
			ApiId: aws.String(prior.ApiId),
		})
		return err
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete api: %w", err)
	}
	return nil
}

// updateInput assembles the UpdateApi call from the inputs that changed since
// the prior apply, reporting whether any field was set. A field removed from
// the configuration (now nil) is never sent, so the stored value stays; an
// explicit false on disable-execute-api-endpoint is sent, which is how the
// default endpoint is re-enabled after a true. The CORS block rides whole,
// since the API replaces the entire Cors object on update. The quick-create
// trio is absent by design: those fields are create-only.
func (r *Api) updateInput(prior Api, apiID string) (*apigatewayv2.UpdateApiInput, bool) {
	in := &apigatewayv2.UpdateApiInput{ApiId: aws.String(apiID)}
	changed := false
	if runtime.Changed(prior.Name, r.Name) {
		in.Name = aws.String(r.Name)
		changed = true
	}
	if r.ApiKeySelectionExpression != nil &&
		runtime.Changed(prior.ApiKeySelectionExpression, r.ApiKeySelectionExpression) {
		in.ApiKeySelectionExpression = r.ApiKeySelectionExpression
		changed = true
	}
	if r.Description != nil && runtime.Changed(prior.Description, r.Description) {
		in.Description = r.Description
		changed = true
	}
	if r.DisableExecuteApiEndpoint != nil &&
		runtime.Changed(prior.DisableExecuteApiEndpoint, r.DisableExecuteApiEndpoint) {
		in.DisableExecuteApiEndpoint = r.DisableExecuteApiEndpoint
		changed = true
	}
	if r.DisableSchemaValidation != nil &&
		runtime.Changed(prior.DisableSchemaValidation, r.DisableSchemaValidation) {
		in.DisableSchemaValidation = r.DisableSchemaValidation
		changed = true
	}
	if r.IpAddressType != nil && runtime.Changed(prior.IpAddressType, r.IpAddressType) {
		in.IpAddressType = apigatewayv2types.IpAddressType(*r.IpAddressType)
		changed = true
	}
	if r.RouteSelectionExpression != nil &&
		runtime.Changed(prior.RouteSelectionExpression, r.RouteSelectionExpression) {
		in.RouteSelectionExpression = r.RouteSelectionExpression
		changed = true
	}
	if r.Version != nil && runtime.Changed(prior.Version, r.Version) {
		in.Version = r.Version
		changed = true
	}
	if r.CorsConfiguration != nil &&
		runtime.Changed(prior.CorsConfiguration, r.CorsConfiguration) {
		in.CorsConfiguration = apiCorsToSDK(r.CorsConfiguration)
		changed = true
	}
	return in, changed
}

// reimport applies the OpenAPI body with ReimportApi and then restores the
// configuration the import overwrote, returning the post-import read so the
// caller has the settled API without another call. An import replaces the
// API's routes and integrations and rewrites API-level properties from the
// document: info.title becomes the name, info.description the description,
// info.version the version, x-amazon-apigateway-cors the CORS configuration,
// and a document tags section replaces the API's whole tag set. The
// follow-up UpdateApi re-asserts the configured name always and the
// description and version when set, leaving a nil input to the document's
// value; the CORS block is restored when configured and deleted when the
// configuration has none but the import produced one; and the tag set is
// re-synced to the configured map.
func (r *Api) reimport(
	ctx context.Context, client *apigatewayv2.Client, apiID, arn string,
) (*apigatewayv2.GetApiOutput, error) {
	in := &apigatewayv2.ReimportApiInput{
		ApiId:    aws.String(apiID),
		Body:     r.Body,
		Basepath: r.BasePath,
	}
	// FailOnWarnings rides the import and is included only when true; the
	// server default already tolerates warnings.
	if r.FailOnWarnings != nil && *r.FailOnWarnings {
		in.FailOnWarnings = r.FailOnWarnings
	}
	err := withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.ReimportApi(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("reimport api: %w", err)
	}
	var resp *apigatewayv2.GetApiOutput
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.GetApi(ctx, &apigatewayv2.GetApiInput{ApiId: aws.String(apiID)})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get api after import: %w", err)
	}
	if resp == nil {
		return nil, errors.New("get api after import: empty response")
	}
	update := &apigatewayv2.UpdateApiInput{
		ApiId: aws.String(apiID),
		Name:  aws.String(r.Name),
	}
	if r.Description != nil {
		update.Description = r.Description
	}
	if r.Version != nil {
		update.Version = r.Version
	}
	if r.CorsConfiguration != nil {
		update.CorsConfiguration = apiCorsToSDK(r.CorsConfiguration)
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.UpdateApi(ctx, update)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("restore api configuration after import: %w", err)
	}
	if r.CorsConfiguration == nil && resp.CorsConfiguration != nil {
		if err := r.deleteCors(ctx, client, apiID); err != nil {
			return nil, fmt.Errorf("after import: %w", err)
		}
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		return syncResourceTags(ctx, client, arn, ptr.Value(r.Tags))
	})
	if err != nil {
		return nil, fmt.Errorf("sync tags after import: %w", err)
	}
	return resp, nil
}

// deleteCors removes the API's CORS configuration. Clearing CORS is its own
// call: UpdateApi cannot remove the configuration, only replace it.
func (r *Api) deleteCors(ctx context.Context, client *apigatewayv2.Client, apiID string) error {
	in := &apigatewayv2.DeleteCorsConfigurationInput{ApiId: aws.String(apiID)}
	err := withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.DeleteCorsConfiguration(ctx, in)
		return err
	})
	if err != nil {
		return fmt.Errorf("delete cors configuration: %w", err)
	}
	return nil
}

// apiARN composes the apigateway ARN of an API, the identifier the tagging
// calls take. GetApi does not return an ARN, so it is assembled from the
// partition and region; the account field of an apigateway ARN is empty.
func apiARN(region, apiID string) string {
	return fmt.Sprintf("arn:%s:apigateway:%s::/apis/%s", partition.Of(region), region, apiID)
}
