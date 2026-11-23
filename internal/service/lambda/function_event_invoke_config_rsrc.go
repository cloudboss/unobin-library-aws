package lambda

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"

	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

type FunctionEventInvokeConfigResource struct {
	FunctionName             string                   `ub:"function-name"`
	Qualifier                *string                  `ub:"qualifier"`
	MaximumEventAgeInSeconds *int32                   `ub:"maximum-event-age-in-seconds"`
	MaximumRetryAttempts     int32                    `ub:"maximum-retry-attempts"`
	DestinationConfig        *EventInvokeDestinations `ub:"destination-config"`

	clientFor func(context.Context, *awsCfg) (eventInvokeClient, error) `ub:"-"`
}

type EventInvokeDestinations struct {
	OnFailure *EventInvokeDestination `ub:"on-failure"`
	OnSuccess *EventInvokeDestination `ub:"on-success"`
}

type EventInvokeDestination struct {
	Destination *string `ub:"destination"`
}

type FunctionEventInvokeConfigResourceOutput struct {
	FunctionName             string                   `ub:"function-name"`
	Qualifier                string                   `ub:"qualifier"`
	Region                   string                   `ub:"region"`
	MaximumEventAgeInSeconds int32                    `ub:"maximum-event-age-in-seconds"`
	MaximumRetryAttempts     int32                    `ub:"maximum-retry-attempts"`
	DestinationConfig        *EventInvokeDestinations `ub:"destination-config"`
}

type eventInvokeKey struct {
	functionName string
	qualifier    *string
}

type eventInvokeClient interface {
	PutFunctionEventInvokeConfig(
		context.Context, *lambda.PutFunctionEventInvokeConfigInput, ...func(*lambda.Options),
	) (*lambda.PutFunctionEventInvokeConfigOutput, error)
	GetFunctionEventInvokeConfig(
		context.Context, *lambda.GetFunctionEventInvokeConfigInput, ...func(*lambda.Options),
	) (*lambda.GetFunctionEventInvokeConfigOutput, error)
	DeleteFunctionEventInvokeConfig(
		context.Context, *lambda.DeleteFunctionEventInvokeConfigInput, ...func(*lambda.Options),
	) (*lambda.DeleteFunctionEventInvokeConfigOutput, error)
	Options() lambda.Options
}

type eventInvokePrior = runtime.Prior[
	FunctionEventInvokeConfigResource, *FunctionEventInvokeConfigResourceOutput, *awsCfg,
]

func (r *FunctionEventInvokeConfigResource) ResourceDefinition() runtime.ResourceDefinition[
	FunctionEventInvokeConfigResource, *FunctionEventInvokeConfigResourceOutput, *awsCfg,
] {
	type input = FunctionEventInvokeConfigResource
	type output = *FunctionEventInvokeConfigResourceOutput
	return runtime.ResourceDefinition[input, output, *awsCfg]{
		SchemaVersion: 1,
		Validate: func(_ context.Context, r input, _ *awsCfg) error {
			_, _, err := r.preparePut()
			return err
		},
		Replace: runtime.Replacement[input, output, *awsCfg]{
			Fields: []runtime.AnyInputField[input]{
				runtime.InputField(func(r *input) *string { return &r.FunctionName }),
				runtime.InputField(func(r *input) **string { return &r.Qualifier }),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
	}
}

func (r FunctionEventInvokeConfigResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.FunctionName)).
			Message("function-name must not be empty"),
		constraint.When(constraint.Present(r.Qualifier)).
			Require(constraint.NotEmpty(r.Qualifier)).
			Message("qualifier must not be empty"),
		constraint.When(constraint.Present(r.MaximumEventAgeInSeconds)).
			Require(
				constraint.AtLeast(r.MaximumEventAgeInSeconds, 60),
				constraint.AtMost(r.MaximumEventAgeInSeconds, 21600),
			).
			Message("maximum-event-age-in-seconds must be between 60 and 21600"),
		constraint.Must(
			constraint.AtLeast(r.MaximumRetryAttempts, 0),
			constraint.AtMost(r.MaximumRetryAttempts, 2),
		).
			Message("maximum-retry-attempts must be between 0 and 2"),
		constraint.When(constraint.Present(r.DestinationConfig.OnFailure)).
			Require(constraint.Present(r.DestinationConfig.OnFailure.Destination)).
			Message("destination-config.on-failure.destination is required"),
		constraint.When(constraint.Present(r.DestinationConfig.OnSuccess)).
			Require(constraint.Present(r.DestinationConfig.OnSuccess.Destination)).
			Message("destination-config.on-success.destination is required"),
	}
}

func (r FunctionEventInvokeConfigResource) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Value(r.MaximumRetryAttempts, 2)}
}

func (r *FunctionEventInvokeConfigResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*FunctionEventInvokeConfigResourceOutput, error) {
	in, key, err := r.preparePut()
	if err != nil {
		return nil, err
	}
	client, err := r.eventInvokeClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return putEventInvokeConfig(ctx, client, in, key, client.Options().Region)
}

func (r *FunctionEventInvokeConfigResource) Read(
	ctx context.Context, cfg *awsCfg, prior eventInvokePrior,
) (*FunctionEventInvokeConfigResourceOutput, error) {
	client, key, err := r.eventInvokePriorTarget(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	return readEventInvokeConfig(ctx, client, key, client.Options().Region)
}

func (r *FunctionEventInvokeConfigResource) Update(
	ctx context.Context, cfg *awsCfg, prior eventInvokePrior,
) (*FunctionEventInvokeConfigResourceOutput, error) {
	in, _, err := r.preparePut()
	if err != nil {
		return nil, err
	}
	if !runtime.Changed(r.MaximumEventAgeInSeconds, prior.Inputs.MaximumEventAgeInSeconds) &&
		!runtime.Changed(r.MaximumRetryAttempts, prior.Inputs.MaximumRetryAttempts) &&
		!runtime.Changed(r.DestinationConfig, prior.Inputs.DestinationConfig) &&
		(prior.Observed == nil || !runtime.Changed(prior.Outputs, prior.Observed)) {
		return prior.Outputs, nil
	}
	client, key, err := r.eventInvokePriorTarget(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	in.FunctionName = aws.String(key.functionName)
	in.Qualifier = key.qualifier
	return putEventInvokeConfig(ctx, client, in, key, client.Options().Region)
}

func (r *FunctionEventInvokeConfigResource) Delete(
	ctx context.Context, cfg *awsCfg, prior eventInvokePrior,
) error {
	client, key, err := r.eventInvokePriorTarget(ctx, cfg, prior)
	if err != nil {
		return err
	}
	_, err = client.DeleteFunctionEventInvokeConfig(ctx, &lambda.DeleteFunctionEventInvokeConfigInput{
		FunctionName: aws.String(key.functionName),
		Qualifier:    key.qualifier,
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete function event invoke config %s: %w", key, err)
	}
	return nil
}

func (r *FunctionEventInvokeConfigResource) eventInvokePriorTarget(
	ctx context.Context, cfg *awsCfg, prior eventInvokePrior,
) (eventInvokeClient, eventInvokeKey, error) {
	if prior.Outputs == nil || prior.Outputs.FunctionName == "" {
		return nil, eventInvokeKey{}, errors.New("missing prior function event invoke config key")
	}
	key := eventInvokeKey{functionName: prior.Outputs.FunctionName}
	if prior.Outputs.Qualifier != "" {
		key.qualifier = aws.String(prior.Outputs.Qualifier)
	}
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	var regional awsCfg
	if cfg != nil {
		regional = *cfg
	}
	if prior.Outputs.Region != "" {
		regional.Region = aws.String(prior.Outputs.Region)
	}
	client, err := r.eventInvokeClient(ctx, &regional)
	return client, key, err
}

func (r *FunctionEventInvokeConfigResource) eventInvokeClient(
	ctx context.Context, cfg *awsCfg,
) (eventInvokeClient, error) {
	if r.clientFor != nil {
		return r.clientFor(ctx, cfg)
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (r *FunctionEventInvokeConfigResource) preparePut() (
	*lambda.PutFunctionEventInvokeConfigInput, eventInvokeKey, error,
) {
	key, err := eventInvokeFunctionKey(r.FunctionName, r.Qualifier)
	if err != nil {
		return nil, eventInvokeKey{}, err
	}
	in := &lambda.PutFunctionEventInvokeConfigInput{
		FunctionName:             aws.String(r.FunctionName),
		Qualifier:                r.Qualifier,
		MaximumEventAgeInSeconds: r.MaximumEventAgeInSeconds,
		MaximumRetryAttempts:     aws.Int32(r.MaximumRetryAttempts),
	}
	if dest := r.DestinationConfig; dest != nil {
		in.DestinationConfig = &lambdatypes.DestinationConfig{}
		if dest.OnFailure != nil {
			if !validEventInvokeDestination(aws.ToString(dest.OnFailure.Destination)) {
				return nil, eventInvokeKey{}, errors.New(
					"destination-config.on-failure.destination must be a valid ARN",
				)
			}
			in.DestinationConfig.OnFailure = &lambdatypes.OnFailure{
				Destination: dest.OnFailure.Destination,
			}
		}
		if dest.OnSuccess != nil {
			if !validEventInvokeDestination(aws.ToString(dest.OnSuccess.Destination)) {
				return nil, eventInvokeKey{}, errors.New(
					"destination-config.on-success.destination must be a valid ARN",
				)
			}
			in.DestinationConfig.OnSuccess = &lambdatypes.OnSuccess{
				Destination: dest.OnSuccess.Destination,
			}
		}
	}
	return in, key, nil
}

func eventInvokeFunctionKey(name string, qualifier *string) (eventInvokeKey, error) {
	parts := strings.Split(name, ":")
	baseParts := 1
	switch {
	case arn.IsARN(name):
		baseParts = 7
		if len(parts) < baseParts || parts[2] != "lambda" || parts[5] != "function" {
			return eventInvokeKey{}, fmt.Errorf("cannot parse function-name %q as a Lambda ARN", name)
		}
	case len(parts) >= 3 && parts[1] == "function" && eventInvokeAccountID.MatchString(parts[0]):
		baseParts = 3
	}
	if len(parts) > baseParts+1 || (len(parts) > 1 && parts[baseParts-1] == "") {
		return eventInvokeKey{}, fmt.Errorf("cannot separate function-name and qualifier in %q", name)
	}
	if len(parts) == baseParts+1 {
		if parts[baseParts] == "" || qualifier != nil {
			return eventInvokeKey{}, fmt.Errorf("invalid inline qualifier in function-name %q", name)
		}
		qualifier = aws.String(parts[baseParts])
	}
	return eventInvokeKey{strings.Join(parts[:baseParts], ":"), qualifier}, nil
}

var (
	eventInvokeARNPartition = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	eventInvokeARNRegion    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	eventInvokeARNAccount   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|\d{12}|cw.{10})$`)
	eventInvokeAccountID = regexp.MustCompile(`^\d{12}$`)
)

func validEventInvokeDestination(destination string) bool {
	if destination == "" {
		return true
	}
	parsed, err := arn.Parse(destination)
	return err == nil &&
		eventInvokeARNPartition.MatchString(parsed.Partition) &&
		(parsed.Region == "" || eventInvokeARNRegion.MatchString(parsed.Region)) &&
		(parsed.AccountID == "" || eventInvokeARNAccount.MatchString(parsed.AccountID)) &&
		parsed.Resource != ""
}

func (key eventInvokeKey) String() string {
	if key.qualifier != nil {
		return fmt.Sprintf("%q (qualifier %q)", key.functionName, *key.qualifier)
	}
	return fmt.Sprintf("%q", key.functionName)
}

func readEventInvokeConfig(
	ctx context.Context, client eventInvokeClient, key eventInvokeKey, awsRegion string,
) (*FunctionEventInvokeConfigResourceOutput, error) {
	out, err := client.GetFunctionEventInvokeConfig(ctx, &lambda.GetFunctionEventInvokeConfigInput{
		FunctionName: aws.String(key.functionName),
		Qualifier:    key.qualifier,
	})
	if isNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get function event invoke config %s: %w", key, err)
	}
	if out == nil {
		return nil, runtime.ErrNotFound
	}
	return &FunctionEventInvokeConfigResourceOutput{
		FunctionName:             key.functionName,
		Qualifier:                aws.ToString(key.qualifier),
		Region:                   awsRegion,
		MaximumEventAgeInSeconds: aws.ToInt32(out.MaximumEventAgeInSeconds),
		MaximumRetryAttempts:     aws.ToInt32(out.MaximumRetryAttempts),
		DestinationConfig:        eventInvokeDestinationsFromSDK(out.DestinationConfig),
	}, nil
}

func putEventInvokeConfig(
	ctx context.Context, client eventInvokeClient, input *lambda.PutFunctionEventInvokeConfigInput,
	key eventInvokeKey, awsRegion string, opts ...retry.Option,
) (*FunctionEventInvokeConfigResourceOutput, error) {
	err := retry.OnError(ctx, eventInvokePutRetryable, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := client.PutFunctionEventInvokeConfig(ctx, input)
		return err
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("put function event invoke config %s: %w", key, err)
	}
	out, err := readEventInvokeConfig(ctx, client, key, awsRegion)
	if errors.Is(err, runtime.ErrNotFound) {
		return nil, fmt.Errorf("get function event invoke config %s after put: %w", key, err)
	}
	return out, err
}

func eventInvokePutRetryable(err error) bool {
	var invalid *lambdatypes.InvalidParameterValueException
	if !errors.As(err, &invalid) {
		return false
	}
	msg := invalid.ErrorMessage()
	return strings.Contains(msg, "destination ARN") ||
		strings.Contains(msg, "does not have permissions")
}

func eventInvokeDestinationsFromSDK(in *lambdatypes.DestinationConfig) *EventInvokeDestinations {
	if in == nil {
		return nil
	}
	out := &EventInvokeDestinations{}
	if in.OnFailure != nil && in.OnFailure.Destination != nil {
		out.OnFailure = &EventInvokeDestination{Destination: in.OnFailure.Destination}
	}
	if in.OnSuccess != nil && in.OnSuccess.Destination != nil {
		out.OnSuccess = &EventInvokeDestination{Destination: in.OnSuccess.Destination}
	}
	if out.OnFailure == nil && out.OnSuccess == nil &&
		(in.OnFailure == nil) == (in.OnSuccess == nil) {
		return nil
	}
	return out
}
