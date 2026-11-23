package sfn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const startExecutionMachineARN = "arn:aws:states:us-east-1:123456789012:stateMachine:example"

type startExecutionCall struct {
	Target string
	Region string
	Body   map[string]any
}

type startExecutionResponse struct {
	Status int
	Body   string
}

func startExecutionEndpoint(
	t *testing.T, responses ...startExecutionResponse,
) (*awsCfg, func() []startExecutionCall) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	calls := make(chan startExecutionCall, len(responses)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credential := strings.Split(r.Header.Get("Authorization"), "/")
		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		if !assert.NoError(t, err) || !assert.GreaterOrEqual(t, len(credential), 5) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		index := len(calls)
		select {
		case calls <- startExecutionCall{r.Header.Get("X-Amz-Target"), credential[2], body}:
		default:
			t.Error("unexpected extra SFN call")
		}
		if index >= len(responses) {
			t.Error("unexpected SFN call")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		w.WriteHeader(responses[index].Status)
		_, _ = w.Write([]byte(responses[index].Body))
	}))
	t.Cleanup(server.Close)
	attempts := int64(1)
	config := &awsCfg{
		EndpointURL: aws.String(server.URL),
		MaxAttempts: &attempts,
	}
	return config, func() []startExecutionCall {
		var history []startExecutionCall
		for len(calls) > 0 {
			history = append(history, <-calls)
		}
		return history
	}
}

func TestStartExecutionRegion(t *testing.T) {
	for _, test := range []struct {
		name     string
		base     string
		override *string
		signing  string
		failure  string
	}{
		{name: "base", base: "us-west-2", signing: "us-west-2"},
		{name: "explicit empty", base: "us-west-2", override: aws.String(""), signing: "us-west-2"},
		{
			name: "within partition", base: "cn-north-1",
			override: aws.String("cn-northwest-1"), signing: "cn-northwest-1",
		},
		{
			name: "commercial fallback", base: "us-west-2",
			override: aws.String("custom-1"), signing: "custom-1",
		},
		{
			name: "no base", override: aws.String("us-gov-west-1"), signing: "us-gov-west-1",
		},
		{
			name: "across partitions", base: "us-east-1", override: aws.String("cn-north-1"),
			failure: `region "cn-north-1" belongs to partition "aws-cn", expected "aws"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, calls := startExecutionEndpoint(t, startExecutionResponse{http.StatusOK, "{}"})
			cfg.Region = aws.String(test.base)
			action := StartExecutionAction{StateMachineARN: startExecutionMachineARN}
			action.Region = test.override
			result, err := action.Run(t.Context(), cfg)
			if test.failure != "" {
				require.ErrorContains(t, err, test.failure)
				assert.Nil(t, result)
				assert.Empty(t, calls())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, &StartExecutionActionOutput{}, result)
			assert.Equal(t, []startExecutionCall{{
				Target: "AWSStepFunctions.StartExecution",
				Region: test.signing,
				Body:   map[string]any{"stateMachineArn": startExecutionMachineARN, "input": "{}"},
			}}, calls())
		})
	}
}

func TestStartExecutionServiceErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		code string
		arn  string
		want any
	}{
		{
			name: "rejected ARN", code: "InvalidArn", arn: "service-owned ARN syntax",
			want: new(*sfntypes.InvalidArn),
		},
		{
			name: "missing state machine", code: "StateMachineDoesNotExist",
			arn: startExecutionMachineARN, want: new(*sfntypes.StateMachineDoesNotExist),
		},
		{
			name: "duplicate name", code: "ExecutionAlreadyExists",
			arn: startExecutionMachineARN, want: new(*sfntypes.ExecutionAlreadyExists),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, calls := startExecutionEndpoint(t, startExecutionResponse{
				http.StatusBadRequest, `{"__type":"` + test.code + `","message":"test failure"}`,
			})
			cfg.Region = aws.String("us-east-1")
			action := StartExecutionAction{StateMachineARN: test.arn, Name: aws.String("example")}
			result, err := action.Run(t.Context(), cfg)
			require.ErrorContains(t, err, "start Step Functions execution for "+test.arn)
			assert.ErrorAs(t, err, test.want)
			assert.Nil(t, result)
			assert.Equal(t, []startExecutionCall{{
				Target: "AWSStepFunctions.StartExecution", Region: "us-east-1",
				Body: map[string]any{
					"stateMachineArn": test.arn, "name": "example", "input": "{}",
				},
			}}, calls())
		})
	}
}

func TestStartExecutionSharedRetry(t *testing.T) {
	cfg, calls := startExecutionEndpoint(t,
		startExecutionResponse{http.StatusInternalServerError, `{"__type":"InternalServerError"}`},
		startExecutionResponse{http.StatusOK, `{"executionArn":"retry-arn"}`},
	)
	cfg.Region = aws.String("us-east-1")
	attempts := int64(2)
	cfg.MaxAttempts = &attempts
	action := StartExecutionAction{
		StateMachineARN: startExecutionMachineARN,
		Input:           aws.String(" [1, true] "),
	}
	result, err := action.Run(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, &StartExecutionActionOutput{ExecutionARN: "retry-arn"}, result)
	want := startExecutionCall{
		Target: "AWSStepFunctions.StartExecution", Region: "us-east-1",
		Body: map[string]any{"stateMachineArn": startExecutionMachineARN, "input": " [1, true] "},
	}
	assert.Equal(t, []startExecutionCall{want, want}, calls())
}

func TestStartExecutionCancellation(t *testing.T) {
	cfg, calls := startExecutionEndpoint(t)
	cfg.Region = aws.String("us-east-1")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	action := StartExecutionAction{StateMachineARN: startExecutionMachineARN}
	result, err := action.Run(ctx, cfg)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
	assert.Empty(t, calls())
}
