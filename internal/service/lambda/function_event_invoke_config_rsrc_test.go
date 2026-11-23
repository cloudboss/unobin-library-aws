package lambda

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

func TestEventInvokePreparePut(t *testing.T) {
	const full = "arn:aws:lambda:us-east-1:123456789012:function:example"
	const partial = "123456789012:function:example"
	tests := []struct {
		name                string
		function, base      string
		separate, effective *string
		retries             int32
		age                 *int32
		dest                *EventInvokeDestinations
		wantDest            *lambdatypes.DestinationConfig
	}{
		{name: "bare", function: "example", base: "example", retries: 2},
		{
			name: "separate alias and empty config", function: "example", base: "example",
			separate: aws.String("blue"), effective: aws.String("blue"), retries: 0,
			dest: &EventInvokeDestinations{}, wantDest: &lambdatypes.DestinationConfig{},
		},
		{
			name: "inline version and failure", function: "example:1", base: "example",
			effective: aws.String("1"), retries: 1, age: aws.Int32(60),
			dest: &EventInvokeDestinations{OnFailure: eventInvokeDestination("arn:aws::::q")},
			wantDest: &lambdatypes.DestinationConfig{
				OnFailure: &lambdatypes.OnFailure{Destination: aws.String("arn:aws::::q")},
			},
		},
		{
			name: "full ARN and success", function: full, base: full,
			retries: 2, age: aws.Int32(21600),
			dest: &EventInvokeDestinations{OnSuccess: eventInvokeDestination("")},
			wantDest: &lambdatypes.DestinationConfig{
				OnSuccess: &lambdatypes.OnSuccess{Destination: aws.String("")},
			},
		},
		{
			name: "qualified full ARN and both arms", function: full + ":$LATEST", base: full,
			effective: aws.String("$LATEST"), retries: 2,
			dest: &EventInvokeDestinations{
				OnFailure: eventInvokeDestination("arn:aws:s3:::bucket"),
				OnSuccess: eventInvokeDestination("arn:aws:unknown:::r"),
			},
			wantDest: &lambdatypes.DestinationConfig{
				OnFailure: &lambdatypes.OnFailure{Destination: aws.String("arn:aws:s3:::bucket")},
				OnSuccess: &lambdatypes.OnSuccess{Destination: aws.String("arn:aws:unknown:::r")},
			},
		},
		{name: "partial ARN", function: partial, base: partial, retries: 2},
		{
			name: "qualified partial ARN", function: partial + ":blue", base: partial,
			effective: aws.String("blue"), retries: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := FunctionEventInvokeConfigResource{
				FunctionName: tt.function, Qualifier: tt.separate,
				MaximumRetryAttempts: tt.retries, MaximumEventAgeInSeconds: tt.age,
				DestinationConfig: tt.dest,
			}
			put, key, err := r.preparePut()
			require.NoError(t, err)
			assert.Equal(t, eventInvokeKey{tt.base, tt.effective}, key)
			assert.Equal(t, &lambda.PutFunctionEventInvokeConfigInput{
				FunctionName: aws.String(tt.function), Qualifier: tt.separate,
				MaximumRetryAttempts: aws.Int32(tt.retries), MaximumEventAgeInSeconds: tt.age,
				DestinationConfig: tt.wantDest,
			}, put)
		})
	}
}

func TestEventInvokeDestinationARNs(t *testing.T) {
	for _, arm := range []string{"on-failure", "on-success"} {
		t.Run(arm, func(t *testing.T) {
			for _, input := range []string{
				"", "arn:aws::::r", "arn:aws:unfamiliar:us-east-1:aws:r",
				"arn:aws-us-gov:sqs:us-iso-east-01:aws-managed:queue:part",
				"arn:aws-cn:sns:cn-north-1:third-party:t",
				"arn:aws:sns::aws-marketplace:t", "arn:aws:sns::partner-managed:t",
				"arn:aws:sqs::123456789012:q", "arn:aws:sqs::cwαβγδεζηθικ:q",
				"arn:aws:sqs::cw !@#%^&*?_:q", "arn:aws:sqs:abcd-a-b-c-99::q",
			} {
				t.Run(input, func(t *testing.T) {
					r := eventInvokeWithDestination(arm, input)
					put, _, err := r.preparePut()
					require.NoError(t, err)
					if arm == "on-failure" {
						assert.Equal(t, &lambdatypes.DestinationConfig{
							OnFailure: &lambdatypes.OnFailure{Destination: aws.String(input)},
						}, put.DestinationConfig)
					} else {
						assert.Equal(t, &lambdatypes.DestinationConfig{
							OnSuccess: &lambdatypes.OnSuccess{Destination: aws.String(input)},
						}, put.DestinationConfig)
					}
				})
			}
			for _, input := range []string{
				"queue", "ARM:aws:sqs:::q", "arn:aws:sqs:q", "arn::sqs:::q",
				"arn:aws-ISO:sqs:::q", "arn:aws2:sqs:::q", "arn:aws:sqs:us-east-111::q",
				"arn:aws:sqs:a-east-1::q", "arn:aws:sqs:us-east-٢::q",
				"arn:aws:sqs::12345678901:q", "arn:aws:sqs::aws-partner:q",
				"arn:aws:sqs::cw123456789:q", "arn:aws:sqs::cw123456789\n:q", "arn:aws:sqs:::",
			} {
				t.Run(input, func(t *testing.T) {
					r := eventInvokeWithDestination(arm, input)
					_, _, err := r.preparePut()
					require.ErrorContains(t, err, "destination-config."+arm+".destination")
				})
			}
		})
	}
}

func eventInvokeWithDestination(arm, destination string) FunctionEventInvokeConfigResource {
	block := &EventInvokeDestinations{}
	if arm == "on-failure" {
		block.OnFailure = eventInvokeDestination(destination)
	} else {
		block.OnSuccess = eventInvokeDestination(destination)
	}
	return FunctionEventInvokeConfigResource{FunctionName: "example", DestinationConfig: block}
}

func eventInvokeDestination(s string) *EventInvokeDestination {
	return &EventInvokeDestination{Destination: aws.String(s)}
}

func TestEventInvokeDestinationOutputs(t *testing.T) {
	failure := &lambdatypes.OnFailure{Destination: aws.String("failure")}
	success := &lambdatypes.OnSuccess{Destination: aws.String("success")}
	tests := []struct {
		name string
		in   *lambdatypes.DestinationConfig
		want *EventInvokeDestinations
	}{
		{name: "nil"},
		{name: "absent arms", in: &lambdatypes.DestinationConfig{}},
		{
			name: "nil leaves",
			in: &lambdatypes.DestinationConfig{
				OnFailure: &lambdatypes.OnFailure{}, OnSuccess: &lambdatypes.OnSuccess{},
			},
		},
		{
			name: "failure without leaf",
			in:   &lambdatypes.DestinationConfig{OnFailure: &lambdatypes.OnFailure{}},
			want: &EventInvokeDestinations{},
		},
		{
			name: "success without leaf",
			in:   &lambdatypes.DestinationConfig{OnSuccess: &lambdatypes.OnSuccess{}},
			want: &EventInvokeDestinations{},
		},
		{
			name: "failure only", in: &lambdatypes.DestinationConfig{OnFailure: failure},
			want: &EventInvokeDestinations{OnFailure: eventInvokeDestination("failure")},
		},
		{
			name: "success only", in: &lambdatypes.DestinationConfig{OnSuccess: success},
			want: &EventInvokeDestinations{OnSuccess: eventInvokeDestination("success")},
		},
		{
			name: "both", in: &lambdatypes.DestinationConfig{OnFailure: failure, OnSuccess: success},
			want: &EventInvokeDestinations{
				OnFailure: eventInvokeDestination("failure"), OnSuccess: eventInvokeDestination("success"),
			},
		},
		{
			name: "empty destination", in: &lambdatypes.DestinationConfig{
				OnSuccess: &lambdatypes.OnSuccess{Destination: aws.String("")},
			},
			want: &EventInvokeDestinations{OnSuccess: eventInvokeDestination("")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, eventInvokeDestinationsFromSDK(tt.in))
		})
	}
}

type eventInvokeScript struct {
	puts      []lambda.PutFunctionEventInvokeConfigInput
	gets      []lambda.GetFunctionEventInvokeConfigInput
	sequence  []string
	putErrors []error
	getOutput *lambda.GetFunctionEventInvokeConfigOutput
	getError  error
	cancel    context.CancelFunc
}

func (s *eventInvokeScript) PutFunctionEventInvokeConfig(
	_ context.Context, in *lambda.PutFunctionEventInvokeConfigInput, _ ...func(*lambda.Options),
) (*lambda.PutFunctionEventInvokeConfigOutput, error) {
	s.puts = append(s.puts, *in)
	s.sequence = append(s.sequence, "put")
	if s.cancel != nil {
		s.cancel()
	}
	i := len(s.puts) - 1
	if i < len(s.putErrors) {
		return nil, s.putErrors[i]
	}
	return &lambda.PutFunctionEventInvokeConfigOutput{MaximumRetryAttempts: aws.Int32(1)}, nil
}

func (s *eventInvokeScript) GetFunctionEventInvokeConfig(
	_ context.Context, in *lambda.GetFunctionEventInvokeConfigInput, _ ...func(*lambda.Options),
) (*lambda.GetFunctionEventInvokeConfigOutput, error) {
	s.gets = append(s.gets, *in)
	s.sequence = append(s.sequence, "get")
	return s.getOutput, s.getError
}

func (s *eventInvokeScript) Options() lambda.Options {
	return lambda.Options{Region: "us-east-1"}
}

func (s *eventInvokeScript) DeleteFunctionEventInvokeConfig(
	_ context.Context, _ *lambda.DeleteFunctionEventInvokeConfigInput, _ ...func(*lambda.Options),
) (*lambda.DeleteFunctionEventInvokeConfigOutput, error) {
	s.sequence = append(s.sequence, "delete")
	return &lambda.DeleteFunctionEventInvokeConfigOutput{}, nil
}

func TestEventInvokePutRetry(t *testing.T) {
	propagation := &lambdatypes.InvalidParameterValueException{
		Message: aws.String("could not use destination ARN"),
	}
	permissions := &lambdatypes.InvalidParameterValueException{
		Message: aws.String("role does not have permissions"),
	}
	ordinary := &lambdatypes.InvalidParameterValueException{
		Message: aws.String("unrecognized destination arn"),
	}
	other := errors.New("destination ARN does not have permissions")
	in := lambda.PutFunctionEventInvokeConfigInput{
		FunctionName: aws.String("fn:blue"), MaximumRetryAttempts: aws.Int32(2),
	}
	key := eventInvokeKey{"fn", aws.String("blue")}
	for _, tt := range []struct {
		name     string
		errors   []error
		cancel   bool
		want     error
		sequence []string
	}{
		{
			name: "typed propagation", errors: []error{fmt.Errorf("write: %w", propagation), permissions},
			sequence: []string{"put", "put", "put", "get"},
		},
		{
			name: "ordinary parameter", errors: []error{ordinary}, want: ordinary,
			sequence: []string{"put"},
		},
		{name: "different type", errors: []error{other}, want: other, sequence: []string{"put"}},
		{
			name: "canceled", errors: []error{permissions}, cancel: true, want: context.Canceled,
			sequence: []string{"put"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			script := &eventInvokeScript{
				putErrors: tt.errors,
				getOutput: &lambda.GetFunctionEventInvokeConfigOutput{
					MaximumRetryAttempts: aws.Int32(0),
				},
			}
			if tt.cancel {
				script.cancel = cancel
			}
			out, err := putEventInvokeConfig(
				ctx, script, &in, key, "us-east-1", retry.WithInterval(0),
			)
			require.Equal(t, tt.sequence, script.sequence)
			for _, got := range script.puts {
				assert.Equal(t, in, got)
			}
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
				assert.Nil(t, out)
				assert.Empty(t, script.gets)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []lambda.GetFunctionEventInvokeConfigInput{{
				FunctionName: aws.String("fn"), Qualifier: aws.String("blue"),
			}}, script.gets)
			assert.Equal(t, &FunctionEventInvokeConfigResourceOutput{
				FunctionName: "fn", Qualifier: "blue", Region: "us-east-1",
			}, out)
		})
	}
}

func TestEventInvokeGetResults(t *testing.T) {
	denied := errors.New("access denied")
	for _, tt := range []struct {
		name    string
		resp    *lambda.GetFunctionEventInvokeConfigOutput
		err     error
		wantErr error
	}{
		{name: "nil result", wantErr: runtime.ErrNotFound},
		{
			name: "typed absence", err: &lambdatypes.ResourceNotFoundException{},
			wantErr: runtime.ErrNotFound,
		},
		{name: "empty result is present", resp: &lambda.GetFunctionEventInvokeConfigOutput{}},
		{name: "access denied", err: denied, wantErr: denied},
	} {
		t.Run(tt.name, func(t *testing.T) {
			script := &eventInvokeScript{getOutput: tt.resp, getError: tt.err}
			r := FunctionEventInvokeConfigResource{
				FunctionName: "fn", MaximumRetryAttempts: 2,
				clientFor: func(context.Context, *awsCfg) (eventInvokeClient, error) {
					return script, nil
				},
			}
			prior := eventInvokePrior{
				Inputs: FunctionEventInvokeConfigResource{MaximumRetryAttempts: 1},
				Outputs: &FunctionEventInvokeConfigResourceOutput{
					FunctionName: "fn", Region: "us-east-1",
				},
			}
			expected := &FunctionEventInvokeConfigResourceOutput{
				FunctionName: "fn", Region: "us-east-1",
			}
			for _, call := range []func() (*FunctionEventInvokeConfigResourceOutput, error){
				func() (*FunctionEventInvokeConfigResourceOutput, error) {
					return r.Read(t.Context(), nil, prior)
				},
				func() (*FunctionEventInvokeConfigResourceOutput, error) {
					return r.Create(t.Context(), nil)
				},
				func() (*FunctionEventInvokeConfigResourceOutput, error) {
					return r.Update(t.Context(), nil, prior)
				},
			} {
				result, err := call()
				if tt.wantErr != nil {
					require.ErrorIs(t, err, tt.wantErr)
					assert.Nil(t, result)
				} else {
					require.NoError(t, err)
					assert.Equal(t, expected, result)
				}
			}
			assert.Equal(t, []string{"get", "put", "get", "put", "get"}, script.sequence)
			get := lambda.GetFunctionEventInvokeConfigInput{FunctionName: aws.String("fn")}
			assert.Equal(t, []lambda.GetFunctionEventInvokeConfigInput{get, get, get}, script.gets)
			put := lambda.PutFunctionEventInvokeConfigInput{
				FunctionName: aws.String("fn"), MaximumRetryAttempts: aws.Int32(2),
			}
			assert.Equal(t, []lambda.PutFunctionEventInvokeConfigInput{put, put}, script.puts)
		})
	}
}

func TestEventInvokeNilGetRecreates(t *testing.T) {
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ENDPOINT_URL", "http://127.0.0.1:0")
	src, err := os.ReadFile("testdata/ub/event-invoke/factory.ub")
	require.NoError(t, err)
	file, err := syntax.ParseSource("factory.ub", src)
	require.NoError(t, err)
	body := file.Factory.Body

	client := &eventInvokeScript{getOutput: &lambda.GetFunctionEventInvokeConfigOutput{}}
	libraries := map[string]*runtime.Library{
		"lambda": {
			Resources: map[string]runtime.ResourceRegistration{
				"function-event-invoke-config": runtime.MakeResourceWith[
					FunctionEventInvokeConfigResource,
					*FunctionEventInvokeConfigResourceOutput,
					*awsCfg,
				]((&FunctionEventInvokeConfigResource{}).ResourceDefinition(),
					func() *FunctionEventInvokeConfigResource {
						return &FunctionEventInvokeConfigResource{
							clientFor: func(context.Context, *awsCfg) (eventInvokeClient, error) {
								return client, nil
							},
						}
					}),
			},
		},
	}
	store, err := local.NewStore(t.TempDir(), "test", "test", encrypters.Noop{})
	require.NoError(t, err)
	exec := &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(body, libraries), SyntaxSource: &body,
		Libraries: libraries, Store: store,
	}
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	src, err = runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(src)
	require.NoError(t, err)
	_, err = exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, []string{"put", "get"}, client.sequence)

	client.getOutput = nil
	next, err := exec.Plan(t.Context())
	require.NoError(t, err)
	decisions := map[string]runtime.Decision{}
	for _, step := range next.Steps {
		decisions[step.Address] = step.Decision
	}
	assert.Equal(t, map[string]runtime.Decision{"resource.settings": runtime.DecisionCreate},
		decisions)
	assert.Equal(t, []string{"put", "get", "get"}, client.sequence)
}
