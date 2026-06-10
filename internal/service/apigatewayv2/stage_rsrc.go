package apigatewayv2

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

// Stage manages a stage of an API Gateway v2 API: a named deployment of the
// API, reachable at its own URL. A stage is addressed by the pair of its API
// id and its name, so a change to either replaces it; every other input is
// reconciled in place. The API's protocol type, HTTP or WebSocket, decides
// which route-setting members may be sent, so creates and updates first read
// the API itself.
type Stage struct {
	// ApiId is the identifier of the API the stage belongs to.
	ApiId string `ub:"api-id"`
	// Name is the stage name: up to 128 characters of alphanumerics,
	// hyphens, and underscores, or the literal $default (AWS enforces the
	// form). The name becomes the path of the invoke URL.
	Name string `ub:"name"`
	// AccessLogSettings turns on access logging for the stage. Removing
	// the block turns it off again.
	AccessLogSettings *StageAccessLogSettings `ub:"access-log-settings"`
	// AutoDeploy makes every change to the API deploy to this stage
	// automatically. It defaults to false and cannot combine with
	// deployment-id, since AWS then chooses the deployment itself.
	AutoDeploy *bool `ub:"auto-deploy"`
	// ClientCertificateId names the client certificate the stage presents
	// to backend integrations. Supported only for WebSocket APIs. Removing
	// the field detaches the certificate.
	ClientCertificateId *string `ub:"client-certificate-id"`
	// DefaultRouteSettings applies to every route that has no settings of
	// its own. Removing the block resets its members to their defaults,
	// except logging-level, which AWS keeps until it is set again; send
	// OFF explicitly to silence WebSocket execution logging.
	DefaultRouteSettings *StageDefaultRouteSettings `ub:"default-route-settings"`
	// DeploymentId pins the stage to one deployment of the API. It must
	// be absent when auto-deploy is enabled. Once set, removing the field
	// leaves the stage on its current deployment.
	DeploymentId *string `ub:"deployment-id"`
	// Description describes the stage, in up to 1024 characters. Removing
	// it clears the stored description.
	Description *string `ub:"description"`
	// RouteSettings overrides the default route settings for individual
	// routes, one entry per route key. Route keys must be unique. An
	// entry may name a route key with no matching route, such as
	// $disconnect on a WebSocket API.
	RouteSettings []StageRouteSettings `ub:"route-settings"`
	// StageVariables are name-value pairs available to integrations.
	// Values may use the characters [A-Za-z0-9-._~:/?#&=,] (AWS enforces
	// the form). An empty value is not representable, because the API
	// reads an empty string as an instruction to remove the variable.
	StageVariables map[string]string `ub:"stage-variables"`
	// Tags are the stage's tags.
	Tags map[string]string `ub:"tags"`
}

// StageOutput holds the stage's identity and the values composed for it.
// The API id and name are the composite identity every later call takes, so
// they live here for the delete after a replace. The ARN, which has no
// account id, is the identifier the tagging calls use. The invoke URL is
// where clients reach the stage. The deployment id is the deployment the
// stage serves; with auto-deploy enabled AWS replaces it on its own, so each
// auto-deployment refreshes this output on the next plan without any write.
type StageOutput struct {
	ApiId        string `ub:"api-id"`
	Name         string `ub:"name"`
	Arn          string `ub:"arn"`
	InvokeUrl    string `ub:"invoke-url"`
	DeploymentId string `ub:"deployment-id"`
}

func (r *Stage) SchemaVersion() int { return 1 }

// ReplaceFields lists the create-only inputs. A stage is addressed by its
// API and its name, so moving it to another API or renaming it means a new
// stage.
func (r *Stage) ReplaceFields() []string {
	return []string{"api-id", "name"}
}

// Defaults marks the collection inputs a stage may omit.
func (r Stage) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.RouteSettings),
		defaults.Optional(r.StageVariables),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules on a stage's inputs. With auto-deploy
// enabled AWS owns the stage's deployment, so deployment-id must be absent.
// The logging levels accept the fixed set the service defines. The
// WebSocket-only rule on data-trace-enabled and logging-level cannot be
// declared here, because the protocol type lives on the referenced API and
// is unknown at plan time; it is enforced in code by omitting those members
// when the API is an HTTP API.
func (r Stage) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.IsTrue(r.AutoDeploy)).
			Require(constraint.Absent(r.DeploymentId)).
			Message("deployment-id cannot be set when auto-deploy is enabled"),
		constraint.When(constraint.Present(r.DefaultRouteSettings.LoggingLevel)).
			Require(constraint.OneOf(r.DefaultRouteSettings.LoggingLevel,
				"ERROR", "INFO", "OFF")).
			Message("default-route-settings logging-level must be ERROR, INFO, or OFF"),
		constraint.ForEach(r.RouteSettings,
			func(e StageRouteSettings) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(e.LoggingLevel)).
						Require(constraint.OneOf(e.LoggingLevel, "ERROR", "INFO", "OFF")).
						Message("route-settings logging-level must be ERROR, INFO, or OFF"),
				}
			}),
	}
}

// validate checks the length bounds the constraint layer cannot express,
// since AWS counts characters and the length predicate counts bytes.
func (r *Stage) validate() error {
	if n := utf8.RuneCountInString(r.Name); n < 1 || n > 128 {
		return errors.New("name must be between 1 and 128 characters")
	}
	if r.Description != nil && utf8.RuneCountInString(*r.Description) > 1024 {
		return errors.New("description must be at most 1024 characters")
	}
	return nil
}

func (r *Stage) Create(ctx context.Context, cfg any) (*StageOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The API's protocol type gates the WebSocket-only route-setting
	// members and decides the invoke URL, so the API is read before the
	// stage is written. A missing API fails here, before any write.
	api, err := client.GetApi(ctx, &apigatewayv2.GetApiInput{ApiId: aws.String(r.ApiId)})
	if err != nil {
		return nil, fmt.Errorf("get api: %w", err)
	}
	websocket := api.ProtocolType == apigatewayv2types.ProtocolTypeWebsocket
	in := &apigatewayv2.CreateStageInput{
		ApiId:     aws.String(r.ApiId),
		StageName: aws.String(r.Name),
		// AutoDeploy is always sent, so an omitted input states the
		// documented default of false explicitly.
		AutoDeploy:          aws.Bool(aws.ToBool(r.AutoDeploy)),
		ClientCertificateId: r.ClientCertificateId,
		DeploymentId:        r.DeploymentId,
		Description:         r.Description,
	}
	if r.AccessLogSettings != nil {
		in.AccessLogSettings = r.AccessLogSettings.expand()
	}
	if r.DefaultRouteSettings != nil {
		in.DefaultRouteSettings = r.DefaultRouteSettings.expand(websocket)
	}
	if len(r.RouteSettings) > 0 {
		routeSettings, err := stageRouteSettingsMap(r.RouteSettings, websocket)
		if err != nil {
			return nil, err
		}
		in.RouteSettings = routeSettings
	}
	if len(r.StageVariables) > 0 {
		in.StageVariables = r.StageVariables
	}
	if len(r.Tags) > 0 {
		in.Tags = r.Tags
	}
	var resp *apigatewayv2.CreateStageOutput
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateStage(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create stage: %w", err)
	}
	// The outputs need nothing the create response lacks: the ARN and the
	// invoke URL are composed from the inputs and the API already read.
	// With auto-deploy enabled AWS fills the deployment id asynchronously,
	// and deliberately nothing waits for it; the output catches up on the
	// next read.
	return stageOutput(client.Options().Region, r.ApiId, r.Name, api, resp.DeploymentId), nil
}

func (r *Stage) Read(ctx context.Context, cfg any, prior *StageOutput) (*StageOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.ApiId, prior.Name)
}

// read fetches the stage and the API it belongs to and composes the
// outputs. A stage cannot outlive its API, so a not-found on either read
// means the stage is gone.
func (r *Stage) read(
	ctx context.Context, client *apigatewayv2.Client, apiID, name string,
) (*StageOutput, error) {
	stage, err := client.GetStage(ctx, &apigatewayv2.GetStageInput{
		ApiId:     aws.String(apiID),
		StageName: aws.String(name),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get stage: %w", err)
	}
	api, err := client.GetApi(ctx, &apigatewayv2.GetApiInput{ApiId: aws.String(apiID)})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get api: %w", err)
	}
	return stageOutput(client.Options().Region, apiID, name, api, stage.DeploymentId), nil
}

func (r *Stage) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Stage, *StageOutput],
) (*StageOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	apiID := prior.Outputs.ApiId
	name := prior.Outputs.Name
	if r.settingsChanged(prior.Inputs) {
		if err := r.updateStage(ctx, client, apiID, name, prior.Inputs); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		arn := stageArn(client.Options().Region, apiID, name)
		err := withConflictRetry(ctx, func(ctx context.Context) error {
			return syncResourceTags(ctx, client, arn, r.Tags)
		})
		if err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, apiID, name)
}

// settingsChanged reports whether any input that rides UpdateStage differs
// from the prior apply. Tags reconcile separately and do not count.
func (r *Stage) settingsChanged(prior Stage) bool {
	return runtime.Changed(prior.AccessLogSettings, r.AccessLogSettings) ||
		runtime.Changed(prior.AutoDeploy, r.AutoDeploy) ||
		runtime.Changed(prior.ClientCertificateId, r.ClientCertificateId) ||
		runtime.Changed(prior.DefaultRouteSettings, r.DefaultRouteSettings) ||
		runtime.Changed(prior.DeploymentId, r.DeploymentId) ||
		runtime.Changed(prior.Description, r.Description) ||
		runtime.Changed(prior.RouteSettings, r.RouteSettings) ||
		runtime.Changed(prior.StageVariables, r.StageVariables)
}

// updateStage reconciles the changed UpdateStage members. The API is read
// first for the protocol type that gates the WebSocket-only members, and the
// desired route-settings map is built before anything is written, so a
// configuration error such as a duplicate route key mutates nothing. A
// removed access-log-settings block takes the service's dedicated delete
// call. Route settings removed or changed since the prior apply are deleted
// key by key, so no stale member can linger, and one UpdateStage call
// applies the changed members: a removed default-route-settings block is
// sent as an empty object, which is how the API clears it, and a removed
// description or client-certificate-id is sent as an explicit empty string,
// which clears the description and detaches the certificate. The deployment
// id keeps its last value when the input is removed, so it is sent only
// while present.
func (r *Stage) updateStage(
	ctx context.Context, client *apigatewayv2.Client, apiID, name string, prior Stage,
) error {
	api, err := client.GetApi(ctx, &apigatewayv2.GetApiInput{ApiId: aws.String(apiID)})
	if err != nil {
		return fmt.Errorf("get api: %w", err)
	}
	websocket := api.ProtocolType == apigatewayv2types.ProtocolTypeWebsocket
	routeSettingsChanged := runtime.Changed(prior.RouteSettings, r.RouteSettings)
	var desiredRouteSettings map[string]apigatewayv2types.RouteSettings
	if routeSettingsChanged && len(r.RouteSettings) > 0 {
		desiredRouteSettings, err = stageRouteSettingsMap(r.RouteSettings, websocket)
		if err != nil {
			return err
		}
	}
	if runtime.Changed(prior.AccessLogSettings, r.AccessLogSettings) &&
		r.AccessLogSettings == nil {
		if err := stageDeleteAccessLogSettings(ctx, client, apiID, name); err != nil {
			return err
		}
	}
	if routeSettingsChanged {
		err := stageDeleteRouteSettings(ctx, client, apiID, name,
			prior.RouteSettings, r.RouteSettings)
		if err != nil {
			return err
		}
	}
	in := &apigatewayv2.UpdateStageInput{
		ApiId:     aws.String(apiID),
		StageName: aws.String(name),
	}
	dirty := false
	if r.AccessLogSettings != nil &&
		runtime.Changed(prior.AccessLogSettings, r.AccessLogSettings) {
		in.AccessLogSettings = r.AccessLogSettings.expand()
		dirty = true
	}
	if runtime.Changed(prior.AutoDeploy, r.AutoDeploy) {
		in.AutoDeploy = aws.Bool(aws.ToBool(r.AutoDeploy))
		dirty = true
	}
	if runtime.Changed(prior.ClientCertificateId, r.ClientCertificateId) {
		in.ClientCertificateId = aws.String(aws.ToString(r.ClientCertificateId))
		dirty = true
	}
	if runtime.Changed(prior.DefaultRouteSettings, r.DefaultRouteSettings) {
		if r.DefaultRouteSettings != nil {
			in.DefaultRouteSettings = r.DefaultRouteSettings.expand(websocket)
		} else {
			in.DefaultRouteSettings = &apigatewayv2types.RouteSettings{}
		}
		dirty = true
	}
	if r.DeploymentId != nil && runtime.Changed(prior.DeploymentId, r.DeploymentId) {
		in.DeploymentId = r.DeploymentId
		dirty = true
	}
	if runtime.Changed(prior.Description, r.Description) {
		in.Description = aws.String(aws.ToString(r.Description))
		dirty = true
	}
	if desiredRouteSettings != nil {
		in.RouteSettings = desiredRouteSettings
		dirty = true
	}
	if runtime.Changed(prior.StageVariables, r.StageVariables) {
		variables := stageVariablesPatch(prior.StageVariables, r.StageVariables)
		if len(variables) > 0 {
			in.StageVariables = variables
			dirty = true
		}
	}
	if !dirty {
		return nil
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.UpdateStage(ctx, in)
		return err
	})
	if err != nil {
		return fmt.Errorf("update stage: %w", err)
	}
	return nil
}

// stageDeleteAccessLogSettings turns off the stage's access logging with the
// service's dedicated call, the documented way to disable it; an UpdateStage
// member cannot remove the settings reliably. A stage with no settings left
// counts as already cleared.
func stageDeleteAccessLogSettings(
	ctx context.Context, client *apigatewayv2.Client, apiID, name string,
) error {
	err := withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.DeleteAccessLogSettings(ctx, &apigatewayv2.DeleteAccessLogSettingsInput{
			ApiId:     aws.String(apiID),
			StageName: aws.String(name),
		})
		return err
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete access log settings: %w", err)
	}
	return nil
}

// Delete removes the stage, keying off the prior outputs so that on a
// replace the old stage is the one deleted. A stage already gone counts as
// deleted.
func (r *Stage) Delete(ctx context.Context, cfg any, prior *StageOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.DeleteStage(ctx, &apigatewayv2.DeleteStageInput{
			ApiId:     aws.String(prior.ApiId),
			StageName: aws.String(prior.Name),
		})
		return err
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete stage: %w", err)
	}
	return nil
}

// stageArn composes the stage's ARN, which has no account id and is the
// identifier the tagging calls take.
func stageArn(region, apiID, name string) string {
	return fmt.Sprintf("arn:%s:apigateway:%s::/apis/%s/stages/%s",
		partition.Of(region), region, apiID, name)
}

// stageInvokeUrl composes the URL clients call. The API's endpoint already
// includes the scheme, https or wss, along with the host and partition
// suffix; the stage name is the path. The one exception is an HTTP API's
// $default stage, which is served at the bare endpoint; a WebSocket API
// keeps the name in the path even then.
func stageInvokeUrl(api *apigatewayv2.GetApiOutput, name string) string {
	endpoint := aws.ToString(api.ApiEndpoint)
	if api.ProtocolType == apigatewayv2types.ProtocolTypeHttp && name == "$default" {
		return endpoint + "/"
	}
	return endpoint + "/" + name
}

// stageOutput builds the outputs from the stage's identity, the API it
// belongs to, and the deployment id the service reported.
func stageOutput(
	region, apiID, name string, api *apigatewayv2.GetApiOutput, deploymentID *string,
) *StageOutput {
	return &StageOutput{
		ApiId:        apiID,
		Name:         name,
		Arn:          stageArn(region, apiID, name),
		InvokeUrl:    stageInvokeUrl(api, name),
		DeploymentId: aws.ToString(deploymentID),
	}
}

// stageVariablesPatch builds the stage-variable member for UpdateStage,
// whose semantics are a merge rather than a replacement: a key absent from
// the request is left as it is, and a key sent with an empty value is
// removed. The patch is therefore the desired map plus an empty-valued
// entry for each key the prior apply set that is no longer wanted.
func stageVariablesPatch(prior, desired map[string]string) map[string]string {
	patch := make(map[string]string, len(desired)+len(prior))
	maps.Copy(patch, desired)
	for k := range prior {
		if _, ok := desired[k]; !ok {
			patch[k] = ""
		}
	}
	return patch
}
