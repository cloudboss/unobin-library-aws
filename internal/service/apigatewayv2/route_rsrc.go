package apigatewayv2

import (
	"context"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// Route manages a route on an API Gateway v2 API: the route key that matches
// incoming requests plus the authorization, request validation, and target
// settings bound to that key. A route belongs to one API for life, so a
// change to api-id replaces it; every other field, the route key included,
// changes in place.
type Route struct {
	// ApiId is the API the route belongs to. Changing it replaces the route.
	ApiId string `ub:"api-id"`
	// RouteKey selects the requests the route handles. For HTTP APIs it is
	// $default or an HTTP method and path such as GET /pets; for WebSocket
	// APIs it is a route selection key such as $connect or $default.
	RouteKey string `ub:"route-key"`
	// ApiKeyRequired requires callers to present an API key. WebSocket APIs
	// only. Unset means false.
	ApiKeyRequired *bool `ub:"api-key-required"`
	// AuthorizationScopes lists the scopes a JWT authorizer matches against
	// the caller's access token. HTTP APIs with a JWT authorizer only.
	AuthorizationScopes *[]string `ub:"authorization-scopes"`
	// AuthorizationType is how callers are authorized: NONE, AWS_IAM, CUSTOM,
	// or JWT. JWT applies to HTTP APIs only; WebSocket APIs accept the other
	// three. Unset means NONE.
	AuthorizationType *string `ub:"authorization-type"`
	// AuthorizerId names the authorizer consulted when authorization-type is
	// CUSTOM or JWT.
	AuthorizerId *string `ub:"authorizer-id"`
	// ModelSelectionExpression picks which request model validates a request.
	// WebSocket APIs only.
	ModelSelectionExpression *string `ub:"model-selection-expression"`
	// OperationName labels the route for documentation. The API accepts at
	// most 64 characters.
	OperationName *string `ub:"operation-name"`
	// RequestModels maps a content type to the name of the model that
	// validates request bodies. WebSocket APIs only.
	RequestModels *map[string]string `ub:"request-models"`
	// RequestParameters maps a request parameter key, such as
	// route.request.header.x-api-key, to whether the parameter is required.
	// WebSocket APIs only.
	RequestParameters *map[string]bool `ub:"request-parameters"`
	// RouteResponseSelectionExpression picks the route response for a
	// request. WebSocket APIs only.
	RouteResponseSelectionExpression *string `ub:"route-response-selection-expression"`
	// Target is the integration the route invokes, in the form
	// integrations/<integration-id>. The API accepts at most 128 characters.
	Target *string `ub:"target"`
}

// RouteOutput holds the identity of a route. API Gateway generates the route
// id; the api id is recorded beside it because the pair addresses every call
// on the route and GetRoute does not echo it back. The api id is fixed for
// the life of the route, so recording it cannot hide drift.
type RouteOutput struct {
	ApiId   string `ub:"api-id"`
	RouteId string `ub:"route-id"`
}

func (r *Route) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs fixed at create time. A route cannot move
// between APIs, so a new api-id means a new route. The route key, despite
// naming the route, updates in place.
func (r *Route) ReplaceFields() []string {
	return []string{"api-id"}
}

// Constraints declares the rules on a route's inputs. The authorization type
// has a fixed set of values, and the CUSTOM and JWT types consult an
// authorizer, so they require one. The operation name and target also have
// API-enforced maximum lengths (64 and 128 characters); only their non-empty
// minimum is checked here.
func (r Route) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.AuthorizationType)).
			Require(constraint.OneOf(r.AuthorizationType, "NONE", "AWS_IAM", "CUSTOM", "JWT")).
			Message("authorization-type must be NONE, AWS_IAM, CUSTOM, or JWT"),
		constraint.When(constraint.OneOf(r.AuthorizationType, "CUSTOM", "JWT")).
			Require(constraint.Present(r.AuthorizerId)).
			Message("authorizer-id is required when authorization-type is CUSTOM or JWT"),
		constraint.When(constraint.Present(r.OperationName)).
			Require(constraint.NotEmpty(r.OperationName)).
			Message("operation-name must not be empty"),
		constraint.When(constraint.Present(r.Target)).
			Require(constraint.NotEmpty(r.Target)).
			Message("target must not be empty"),
	}
}

func (r *Route) Create(ctx context.Context, cfg *awsCfg) (*RouteOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The API key flag and the authorization type are always sent, pinning
	// the server defaults false and NONE client-side; every other member is
	// sent only when set, leaving the omitted ones to the service.
	in := &apigatewayv2.CreateRouteInput{
		ApiId:             aws.String(r.ApiId),
		RouteKey:          aws.String(r.RouteKey),
		ApiKeyRequired:    aws.Bool(aws.ToBool(r.ApiKeyRequired)),
		AuthorizationType: routeAuthorizationType(r.AuthorizationType),
	}
	if len(ptr.Value(r.AuthorizationScopes)) > 0 {
		in.AuthorizationScopes = ptr.Value(r.AuthorizationScopes)
	}
	if v := aws.ToString(r.AuthorizerId); v != "" {
		in.AuthorizerId = aws.String(v)
	}
	if v := aws.ToString(r.ModelSelectionExpression); v != "" {
		in.ModelSelectionExpression = aws.String(v)
	}
	if v := aws.ToString(r.OperationName); v != "" {
		in.OperationName = aws.String(v)
	}
	if len(ptr.Value(r.RequestModels)) > 0 {
		in.RequestModels = ptr.Value(r.RequestModels)
	}
	if len(ptr.Value(r.RequestParameters)) > 0 {
		in.RequestParameters = routeParameterConstraints(ptr.Value(r.RequestParameters))
	}
	if v := aws.ToString(r.RouteResponseSelectionExpression); v != "" {
		in.RouteResponseSelectionExpression = aws.String(v)
	}
	if v := aws.ToString(r.Target); v != "" {
		in.Target = aws.String(v)
	}
	var resp *apigatewayv2.CreateRouteOutput
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateRoute(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create route: %w", err)
	}
	// The create response holds the route id and nothing settles afterward,
	// so the outputs come straight from it.
	return &RouteOutput{
		ApiId:   r.ApiId,
		RouteId: aws.ToString(resp.RouteId),
	}, nil
}

// Read fetches the route by the (api-id, route-id) pair recorded at create.
// The pair comes from prior outputs rather than the receiver so that a
// planned api-id replacement still reads the old route as existing. A
// NotFoundException covers both a deleted route and a deleted parent API,
// and either one correctly plans a recreate.
func (r *Route) Read(ctx context.Context, cfg *awsCfg, prior *RouteOutput) (*RouteOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetRoute(ctx, &apigatewayv2.GetRouteInput{
		ApiId:   aws.String(prior.ApiId),
		RouteId: aws.String(prior.RouteId),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get route: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("get route %s: empty response", prior.RouteId)
	}
	return &RouteOutput{
		ApiId:   prior.ApiId,
		RouteId: aws.ToString(resp.RouteId),
	}, nil
}

// Update reconciles the changed fields. UpdateRoute is a patch: a member
// left out keeps its value on the route, so each member is included only
// when its input changed, and a field cleared from the configuration is
// included with its empty value to clear it on the route. Request parameters
// are the exception: the patch map only adds or overwrites keys, so departed
// keys are removed first with DeleteRouteRequestParameter, and when those
// removals are the whole change the patch call is skipped.
func (r *Route) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Route, *RouteOutput],
) (*RouteOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	parametersChanged := runtime.Changed(ptr.Value(prior.Inputs.RequestParameters), ptr.Value(r.RequestParameters))
	if parametersChanged {
		if err := r.removeDepartedParameters(ctx, client, prior); err != nil {
			return nil, err
		}
	}
	apiKeyRequiredChanged := runtime.Changed(prior.Inputs.ApiKeyRequired, r.ApiKeyRequired)
	scopesChanged := runtime.Changed(ptr.Value(prior.Inputs.AuthorizationScopes), ptr.Value(r.AuthorizationScopes))
	authTypeChanged := runtime.Changed(prior.Inputs.AuthorizationType, r.AuthorizationType)
	authorizerChanged := runtime.Changed(prior.Inputs.AuthorizerId, r.AuthorizerId)
	modelSelectionChanged := runtime.Changed(
		prior.Inputs.ModelSelectionExpression, r.ModelSelectionExpression)
	operationNameChanged := runtime.Changed(prior.Inputs.OperationName, r.OperationName)
	modelsChanged := runtime.Changed(ptr.Value(prior.Inputs.RequestModels), ptr.Value(r.RequestModels))
	routeKeyChanged := runtime.Changed(prior.Inputs.RouteKey, r.RouteKey)
	responseSelectionChanged := runtime.Changed(
		prior.Inputs.RouteResponseSelectionExpression, r.RouteResponseSelectionExpression)
	targetChanged := runtime.Changed(prior.Inputs.Target, r.Target)
	otherChanged := apiKeyRequiredChanged || scopesChanged || authTypeChanged ||
		authorizerChanged || modelSelectionChanged || operationNameChanged ||
		modelsChanged || routeKeyChanged || responseSelectionChanged || targetChanged
	// When the only change is the removal of the last request parameters,
	// the deletes above are the whole update.
	if !otherChanged && (!parametersChanged || len(ptr.Value(r.RequestParameters)) == 0) {
		return prior.Outputs, nil
	}
	in := &apigatewayv2.UpdateRouteInput{
		ApiId:   aws.String(prior.Outputs.ApiId),
		RouteId: aws.String(prior.Outputs.RouteId),
	}
	if apiKeyRequiredChanged {
		in.ApiKeyRequired = aws.Bool(aws.ToBool(r.ApiKeyRequired))
	}
	if scopesChanged {
		// A scope list cleared from the configuration is sent as the empty
		// list, which removes every scope from the route.
		in.AuthorizationScopes = ptr.Value(r.AuthorizationScopes)
		if in.AuthorizationScopes == nil {
			in.AuthorizationScopes = []string{}
		}
	}
	if authTypeChanged || authorizerChanged {
		// A changed authorizer must be sent together with the authorization
		// type even when the type itself did not change; an authorizer swap
		// under the same type depends on it.
		in.AuthorizationType = routeAuthorizationType(r.AuthorizationType)
	}
	if authorizerChanged {
		in.AuthorizerId = aws.String(aws.ToString(r.AuthorizerId))
	}
	if modelSelectionChanged {
		in.ModelSelectionExpression = aws.String(aws.ToString(r.ModelSelectionExpression))
	}
	if operationNameChanged {
		in.OperationName = aws.String(aws.ToString(r.OperationName))
	}
	if modelsChanged {
		// A model map cleared from the configuration is sent as the empty
		// map, which removes every request model from the route.
		in.RequestModels = ptr.Value(r.RequestModels)
		if in.RequestModels == nil {
			in.RequestModels = map[string]string{}
		}
	}
	if parametersChanged && len(ptr.Value(r.RequestParameters)) > 0 {
		in.RequestParameters = routeParameterConstraints(ptr.Value(r.RequestParameters))
	}
	if routeKeyChanged {
		in.RouteKey = aws.String(r.RouteKey)
	}
	if responseSelectionChanged {
		expression := aws.ToString(r.RouteResponseSelectionExpression)
		in.RouteResponseSelectionExpression = aws.String(expression)
	}
	if targetChanged {
		in.Target = aws.String(aws.ToString(r.Target))
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.UpdateRoute(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("update route: %w", err)
	}
	// The route id and api id are fixed, so the outputs cannot change here.
	return prior.Outputs, nil
}

// Delete removes the route, keyed by the (api-id, route-id) pair from prior
// outputs so that a replacement deletes the old route rather than the new
// one's coordinates. A NotFoundException means the route is already gone,
// which is the desired end state.
func (r *Route) Delete(ctx context.Context, cfg *awsCfg, prior *RouteOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.DeleteRoute(ctx, &apigatewayv2.DeleteRouteInput{
			ApiId:   aws.String(prior.ApiId),
			RouteId: aws.String(prior.RouteId),
		})
		return err
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete route: %w", err)
	}
	return nil
}

// removeDepartedParameters deletes each request parameter that the prior
// configuration declared and the current one no longer does. The pair of key
// and required flag is the unit of comparison, matching the whole-element
// diff the API needs: a flipped required flag deletes the key here and
// re-adds it through the following patch. Each delete tolerates
// NotFoundException, since a parameter already gone needs no removal.
func (r *Route) removeDepartedParameters(
	ctx context.Context, client *apigatewayv2.Client, prior runtime.Prior[Route, *RouteOutput],
) error {
	var keys []string
	for key, required := range ptr.Value(prior.Inputs.RequestParameters) {
		if current, ok := ptr.Value(r.RequestParameters)[key]; !ok || current != required {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		in := &apigatewayv2.DeleteRouteRequestParameterInput{
			ApiId:               aws.String(prior.Outputs.ApiId),
			RequestParameterKey: aws.String(key),
			RouteId:             aws.String(prior.Outputs.RouteId),
		}
		err := withConflictRetry(ctx, func(ctx context.Context) error {
			_, err := client.DeleteRouteRequestParameter(ctx, in)
			return err
		})
		if err != nil && !isNotFound(err) {
			return fmt.Errorf("delete route request parameter %s: %w", key, err)
		}
	}
	return nil
}

// routeAuthorizationType returns the authorization type to send, pinning the
// server default NONE client-side when the input is unset.
func routeAuthorizationType(authorizationType *string) apigatewayv2types.AuthorizationType {
	if authorizationType == nil {
		return apigatewayv2types.AuthorizationTypeNone
	}
	return apigatewayv2types.AuthorizationType(*authorizationType)
}

// routeParameterConstraints converts the request-parameters map into the SDK
// member type, one single-field ParameterConstraints per parameter key.
func routeParameterConstraints(
	parameters map[string]bool,
) map[string]apigatewayv2types.ParameterConstraints {
	out := make(map[string]apigatewayv2types.ParameterConstraints, len(parameters))
	for key, required := range parameters {
		out[key] = apigatewayv2types.ParameterConstraints{Required: aws.Bool(required)}
	}
	return out
}
