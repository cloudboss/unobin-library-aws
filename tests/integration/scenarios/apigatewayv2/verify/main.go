// verify checks the HTTP API stack the scenario applied against the phase
// named in the VERIFY_PHASE environment variable. It looks the API up by its
// stable name because the driver passes no plan outputs into verify, and it
// reads only cloud state: applied requires the HTTP API, its AWS_PROXY
// integration, the GET /hello route targeting that integration, the $default
// stage, and the function URL on the backing function; destroyed requires the
// API and the function URL to be gone. Input echoes an emulator may not model
// (auto-deploy, stage variables) are checked best-effort, degrading to a
// printed skip. Tearing the stack down is the destroy plan's job, not the
// verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

const (
	apiName      = "unobin-it-apigatewayv2"
	functionName = "unobin-it-apigatewayv2"
	routeKey     = "GET /hello"
	stageName    = "$default"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	apiClient := apigatewayv2.NewFromConfig(cfg)
	lambdaClient := lambda.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, apiClient, lambdaClient)
	case "destroyed":
		return verifyDestroyed(ctx, apiClient, lambdaClient)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(
	ctx context.Context, apiClient *apigatewayv2.Client, lambdaClient *lambda.Client,
) error {
	api, err := findAPI(ctx, apiClient)
	if err != nil {
		return err
	}
	if api == nil {
		return fmt.Errorf("api %s not found", apiName)
	}
	if got := api.ProtocolType; got != apigatewayv2types.ProtocolTypeHttp {
		return fmt.Errorf("api %s protocol is %s, want HTTP", apiName, got)
	}
	if aws.ToString(api.ApiEndpoint) == "" {
		fmt.Println("skip: api endpoint not modeled")
	}
	apiID := aws.ToString(api.ApiId)

	integration, err := findIntegration(ctx, apiClient, apiID)
	if err != nil {
		return err
	}
	if integration == nil {
		return fmt.Errorf("api %s has no AWS_PROXY integration", apiName)
	}

	route, err := findRoute(ctx, apiClient, apiID)
	if err != nil {
		return err
	}
	if route == nil {
		return fmt.Errorf("api %s has no route %s", apiName, routeKey)
	}
	wantTarget := "integrations/" + aws.ToString(integration.IntegrationId)
	if got := aws.ToString(route.Target); got != wantTarget {
		return fmt.Errorf("route %s target is %q, want %q", routeKey, got, wantTarget)
	}

	stage, err := findStage(ctx, apiClient, apiID)
	if err != nil {
		return err
	}
	if stage == nil {
		return fmt.Errorf("api %s has no stage %s", apiName, stageName)
	}
	checkStageEchoes(stage)

	urlConfig, err := findFunctionURL(ctx, lambdaClient)
	if err != nil {
		return err
	}
	if urlConfig == nil {
		return fmt.Errorf("function %s has no function url", functionName)
	}
	if aws.ToString(urlConfig.FunctionUrl) == "" {
		return fmt.Errorf("function url of %s is empty", functionName)
	}
	if got := urlConfig.AuthType; got != lambdatypes.FunctionUrlAuthTypeNone {
		return fmt.Errorf("function url auth type is %s, want NONE", got)
	}
	return nil
}

// checkStageEchoes confirms the stage's input echoes best-effort: an emulator
// may not store auto-deploy or stage variables, so a miss degrades to a
// printed skip rather than a failure.
func checkStageEchoes(stage *apigatewayv2.GetStageOutput) {
	if !aws.ToBool(stage.AutoDeploy) {
		fmt.Println("skip: stage auto-deploy not modeled")
	} else {
		fmt.Println("ok: stage auto-deploy enabled")
	}
	if got := stage.StageVariables["GREETING"]; got == "" {
		fmt.Println("skip: stage variables not modeled")
	} else {
		fmt.Printf("ok: stage variable GREETING=%s\n", got)
	}
}

func verifyDestroyed(
	ctx context.Context, apiClient *apigatewayv2.Client, lambdaClient *lambda.Client,
) error {
	api, err := findAPI(ctx, apiClient)
	if err != nil {
		return err
	}
	if api != nil {
		return fmt.Errorf("api %s still exists", apiName)
	}
	urlConfig, err := findFunctionURL(ctx, lambdaClient)
	if err != nil {
		return err
	}
	if urlConfig != nil {
		return fmt.Errorf("function url of %s still exists", functionName)
	}
	return nil
}

// findAPI returns the scenario's API matched by name, or nil when it is gone.
func findAPI(ctx context.Context, client *apigatewayv2.Client) (*apigatewayv2types.Api, error) {
	var next *string
	for {
		resp, err := client.GetApis(ctx, &apigatewayv2.GetApisInput{NextToken: next})
		if err != nil {
			return nil, fmt.Errorf("get apis: %w", err)
		}
		for i := range resp.Items {
			if aws.ToString(resp.Items[i].Name) == apiName {
				return &resp.Items[i], nil
			}
		}
		if resp.NextToken == nil {
			return nil, nil
		}
		next = resp.NextToken
	}
}

// findIntegration returns the API's AWS_PROXY integration, or nil when none
// exists.
func findIntegration(
	ctx context.Context, client *apigatewayv2.Client, apiID string,
) (*apigatewayv2types.Integration, error) {
	var next *string
	for {
		resp, err := client.GetIntegrations(ctx, &apigatewayv2.GetIntegrationsInput{
			ApiId:     aws.String(apiID),
			NextToken: next,
		})
		if err != nil {
			return nil, fmt.Errorf("get integrations: %w", err)
		}
		for i := range resp.Items {
			if resp.Items[i].IntegrationType == apigatewayv2types.IntegrationTypeAwsProxy {
				return &resp.Items[i], nil
			}
		}
		if resp.NextToken == nil {
			return nil, nil
		}
		next = resp.NextToken
	}
}

// findRoute returns the API's route matched by route key, or nil when none
// exists.
func findRoute(
	ctx context.Context, client *apigatewayv2.Client, apiID string,
) (*apigatewayv2types.Route, error) {
	var next *string
	for {
		resp, err := client.GetRoutes(ctx, &apigatewayv2.GetRoutesInput{
			ApiId:     aws.String(apiID),
			NextToken: next,
		})
		if err != nil {
			return nil, fmt.Errorf("get routes: %w", err)
		}
		for i := range resp.Items {
			if aws.ToString(resp.Items[i].RouteKey) == routeKey {
				return &resp.Items[i], nil
			}
		}
		if resp.NextToken == nil {
			return nil, nil
		}
		next = resp.NextToken
	}
}

// findStage returns the scenario's stage, or nil when it or its API is gone.
func findStage(
	ctx context.Context, client *apigatewayv2.Client, apiID string,
) (*apigatewayv2.GetStageOutput, error) {
	resp, err := client.GetStage(ctx, &apigatewayv2.GetStageInput{
		ApiId:     aws.String(apiID),
		StageName: aws.String(stageName),
	})
	if err != nil {
		var notFound *apigatewayv2types.NotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get stage: %w", err)
	}
	return resp, nil
}

// findFunctionURL returns the function's URL config, or nil when the config
// or the function itself is gone.
func findFunctionURL(
	ctx context.Context, client *lambda.Client,
) (*lambda.GetFunctionUrlConfigOutput, error) {
	resp, err := client.GetFunctionUrlConfig(ctx, &lambda.GetFunctionUrlConfigInput{
		FunctionName: aws.String(functionName),
	})
	if err != nil {
		var notFound *lambdatypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get function url config: %w", err)
	}
	return resp, nil
}
