package lambda

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

// AliasResource gives a Lambda function version a stable name, the way CloudFormation
// models AWS::Lambda::Alias. The function and alias name identify the alias and
// are fixed at creation; the target version, description, and routing weights
// reconcile in place through UpdateAlias. Removing description means the
// desired description is the empty string, and removing routing-config means the
// desired traffic-shift configuration is empty, so update sends both members
// explicitly instead of omitting them.
type AliasResource struct {
	Name            string              `ub:"name"`
	FunctionName    string              `ub:"function-name"`
	FunctionVersion string              `ub:"function-version"`
	Description     *string             `ub:"description"`
	RoutingConfig   *AliasRoutingConfig `ub:"routing-config"`
}

// AliasRoutingConfig holds the traffic-shifting weights for a Lambda alias.
// AdditionalVersionWeights maps a second function version to the fraction of
// traffic the alias sends to it; Lambda validates the version names and weight
// range.
type AliasRoutingConfig struct {
	AdditionalVersionWeights *map[string]float64 `ub:"additional-version-weights"`
}

// AliasResourceOutput holds the values Lambda computes for an alias and the identity
// handles needed to address the same alias later. Arn is the alias ARN returned
// by GetAlias, and InvokeArn is the API Gateway integration target composed
// from it. FunctionName and Name are stored as prior handles because both are
// replace fields, so a replacement delete or read must use the old identity.
type AliasResourceOutput struct {
	Arn          string `ub:"arn"`
	InvokeArn    string `ub:"invoke-arn"`
	FunctionName string `ub:"function-name"`
	Name         string `ub:"name"`
}

func (r *AliasResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the two inputs that identify the alias. Lambda has no
// operation to move an alias to a different function or rename it, so changing
// either field replaces the alias.
func (r *AliasResource) ReplaceFields() []string {
	return []string{
		"function-name",
		"name",
	}
}

func (r *AliasResource) EquivalentInput(field string, prior, current AliasResource) bool {
	if field != "function-name" {
		return false
	}
	return equivalentFunctionNameOrARN(prior.FunctionName, current.FunctionName)
}

func (r *AliasResource) Create(ctx context.Context, cfg *awsCfg) (*AliasResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_, err = client.CreateAlias(ctx, &lambda.CreateAliasInput{
		Name:            aws.String(r.Name),
		FunctionName:    aws.String(r.FunctionName),
		FunctionVersion: aws.String(r.FunctionVersion),
		Description:     aliasDescription(r.Description),
		RoutingConfig:   aliasRoutingConfigForRequest(r.RoutingConfig),
	})
	if err != nil {
		return nil, fmt.Errorf("create alias %s on %s: %w", r.Name, r.FunctionName, err)
	}
	return r.read(ctx, client, r.FunctionName, r.Name)
}

// Read refreshes the alias by the identity stored in the prior outputs. A
// replacement read receives the new inputs on the receiver, so using the prior
// handles is what still finds the old alias. A typed Lambda not-found or an
// empty GetAlias result maps to runtime.ErrNotFound.
func (r *AliasResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *AliasResourceOutput,
) (*AliasResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.FunctionName, prior.Name)
}

// Update reconciles the mutable alias fields with one full UpdateAlias request
// when any of them changed. Description is always sent, using an explicit empty
// string when absent, and routing-config is always sent, using an empty config
// when absent, so removed values clear in AWS instead of being left in place.
func (r *AliasResource) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[AliasResource, *AliasResourceOutput],
) (*AliasResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if !r.mutableChanged(prior.Inputs) {
		if prior.Observed != nil {
			return prior.Observed, nil
		}
		return prior.Outputs, nil
	}
	functionName := prior.Outputs.FunctionName
	name := prior.Outputs.Name
	_, err = client.UpdateAlias(ctx, &lambda.UpdateAliasInput{
		FunctionName:    aws.String(functionName),
		Name:            aws.String(name),
		FunctionVersion: aws.String(r.FunctionVersion),
		Description:     aliasDescription(r.Description),
		RoutingConfig:   aliasRoutingConfigForRequest(r.RoutingConfig),
	})
	if err != nil {
		return nil, fmt.Errorf("update alias %s on %s: %w", name, functionName, err)
	}
	return r.read(ctx, client, functionName, name)
}

// Delete removes the alias by the prior identity, because a replacement delete
// receives the replacement's new inputs on the receiver. A missing alias is
// already deleted and is therefore a successful outcome.
func (r *AliasResource) Delete(ctx context.Context, cfg *awsCfg, prior *AliasResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteAlias(ctx, &lambda.DeleteAliasInput{
		FunctionName: aws.String(prior.FunctionName),
		Name:         aws.String(prior.Name),
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete alias %s on %s: %w", prior.Name, prior.FunctionName, err)
	}
	return nil
}

func (r *AliasResource) read(
	ctx context.Context, client *lambda.Client, functionName, name string,
) (*AliasResourceOutput, error) {
	resp, err := client.GetAlias(ctx, &lambda.GetAliasInput{
		FunctionName: aws.String(functionName),
		Name:         aws.String(name),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get alias %s on %s: %w", name, functionName, err)
	}
	if resp == nil || aws.ToString(resp.AliasArn) == "" {
		return nil, runtime.ErrNotFound
	}
	arn := aws.ToString(resp.AliasArn)
	region := region(client)
	part := partition.Of(region)
	return &AliasResourceOutput{
		Arn:          arn,
		InvokeArn:    functionInvokeARN(part, region, arn),
		FunctionName: functionName,
		Name:         name,
	}, nil
}

func (r *AliasResource) mutableChanged(prior AliasResource) bool {
	return runtime.Changed(prior.FunctionVersion, r.FunctionVersion) ||
		runtime.Changed(prior.Description, r.Description) ||
		runtime.Changed(prior.RoutingConfig, r.RoutingConfig)
}

func equivalentFunctionNameOrARN(prior, current string) bool {
	if prior == current {
		return true
	}
	if name, ok := lambdaFunctionNameFromIdentifier(prior); ok && name == current {
		return true
	}
	if name, ok := lambdaFunctionNameFromIdentifier(current); ok && name == prior {
		return true
	}
	return false
}

func lambdaFunctionNameFromIdentifier(s string) (string, bool) {
	if strings.HasPrefix(s, "arn:") {
		parts := strings.SplitN(s, ":", 7)
		if len(parts) != 7 || parts[2] != "lambda" || parts[5] != "function" {
			return "", false
		}
		name, _, _ := strings.Cut(parts[6], ":")
		return name, name != ""
	}
	parts := strings.SplitN(s, ":", 3)
	if len(parts) == 3 && isLambdaAccountID(parts[0]) && parts[1] == "function" {
		name, _, _ := strings.Cut(parts[2], ":")
		return name, name != ""
	}
	return "", false
}

func isLambdaAccountID(s string) bool {
	if len(s) != 12 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func aliasDescription(description *string) *string {
	return aws.String(aws.ToString(description))
}

func aliasRoutingConfigForRequest(
	config *AliasRoutingConfig,
) *lambdatypes.AliasRoutingConfiguration {
	if config == nil {
		return &lambdatypes.AliasRoutingConfiguration{}
	}
	return config.to()
}

func (b *AliasRoutingConfig) to() *lambdatypes.AliasRoutingConfiguration {
	if b == nil {
		return nil
	}
	out := &lambdatypes.AliasRoutingConfiguration{}
	if b.AdditionalVersionWeights != nil {
		out.AdditionalVersionWeights = *b.AdditionalVersionWeights
	}
	return out
}
