package applicationautoscaling

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	sdk "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type targetCall struct {
	Operation string
	Input     map[string]any
}

type targetAPI struct {
	t      *testing.T
	target map[string]any
	tags   map[string]string
	calls  []targetCall
	hook   func(*http.Request, string, map[string]any) (map[string]any, string)
}

func targetClient(t *testing.T) (*targetAPI, *sdk.Client) {
	t.Helper()
	api := &targetAPI{t: t, tags: map[string]string{}}
	client := sdk.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("x", "y", ""),
		HTTPClient: api, RetryMaxAttempts: 1,
	}, func(o *sdk.Options) { o.BaseEndpoint = aws.String("http://target.test") })
	return api, client
}

func (a *targetAPI) Do(req *http.Request) (*http.Response, error) {
	var input map[string]any
	require.NoError(a.t, json.NewDecoder(req.Body).Decode(&input))
	op := strings.TrimPrefix(req.Header.Get("X-Amz-Target"), "AnyScaleFrontendService.")
	a.calls = append(a.calls, targetCall{op, input})
	var result map[string]any
	code := ""
	if a.hook != nil {
		result, code = a.hook(req, op, input)
	}
	if result == nil && code == "" {
		result = map[string]any{}
		switch op {
		case "RegisterScalableTarget":
			if a.target == nil {
				a.target = map[string]any{"RoleARN": "remote-role", "ScalableTargetARN": "target-arn"}
			}
			for key, value := range input {
				if key != "Tags" {
					a.target[key] = value
				}
			}
			if tags, ok := input["Tags"].(map[string]any); ok {
				for k, v := range tags {
					a.tags[k] = v.(string)
				}
			}
		case "DescribeScalableTargets":
			if a.target != nil {
				result["ScalableTargets"] = []any{a.target}
			}
		case "ListTagsForResource":
			result["Tags"] = a.tags
		case "DeregisterScalableTarget":
			a.target = nil
		case "TagResource":
			for key, value := range input["Tags"].(map[string]any) {
				a.tags[key] = value.(string)
			}
		case "UntagResource":
			for _, key := range input["TagKeys"].([]any) {
				delete(a.tags, key.(string))
			}
		default:
			a.t.Fatalf("unexpected operation %s", op)
		}
	}
	status := http.StatusOK
	if code != "" {
		status = http.StatusBadRequest
		result = map[string]any{"__type": code, "message": result["message"]}
	}
	data, err := json.Marshal(result)
	require.NoError(a.t, err)
	return &http.Response{StatusCode: status, Header: http.Header{
		"Content-Type": {"application/x-amz-json-1.1"},
	}, Body: io.NopCloser(strings.NewReader(string(data))), Request: req}, nil
}

func testTarget() ScalableTargetResource {
	return ScalableTargetResource{ResourceID: "table/a/b", ServiceNamespace: "dynamodb",
		ScalableDimension: "dynamodb:table:ReadCapacityUnits", MinCapacity: 0, MaxCapacity: 9}
}

func (a *targetAPI) operations() []string {
	ops := make([]string, 0, len(a.calls))
	for _, call := range a.calls {
		ops = append(ops, call.Operation)
	}
	return ops
}

func TestTargetCreateAndReadOutputs(t *testing.T) {
	api, client := targetClient(t)
	r := testTarget()
	r.Tags = &map[string]string{"empty": "", "aws:system": "ignore"}
	r.SuspendedState = &SuspendedState{DynamicScalingInSuspended: aws.Bool(true),
		DynamicScalingOutSuspended: aws.Bool(true), ScheduledScalingSuspended: aws.Bool(true)}
	out, err := r.create(t.Context(), client, &fakeClock{})
	require.NoError(t, err)
	assert.Equal(t, &ScalableTargetResourceOutput{ResourceID: r.ResourceID,
		ServiceNamespace: r.ServiceNamespace, ScalableDimension: r.ScalableDimension,
		MinCapacity: 0, MaxCapacity: 9, RoleARN: "remote-role", ScalableTargetARN: "target-arn",
		SuspendedState: r.SuspendedState, Tags: map[string]string{"empty": ""}}, out)
	assert.Equal(t, []string{"RegisterScalableTarget", "DescribeScalableTargets",
		"ListTagsForResource"}, api.operations())
	assert.Equal(t, map[string]any{"ResourceId": r.ResourceID,
		"ServiceNamespace": r.ServiceNamespace, "ScalableDimension": r.ScalableDimension,
		"MinCapacity": float64(0), "MaxCapacity": float64(9),
		"Tags": map[string]any{"empty": ""}, "SuspendedState": map[string]any{
			"DynamicScalingInSuspended": true, "DynamicScalingOutSuspended": true,
			"ScheduledScalingSuspended": true}}, api.calls[0].Input)
	assert.Equal(t, map[string]any{"ResourceIds": []any{r.ResourceID},
		"ServiceNamespace": r.ServiceNamespace, "ScalableDimension": r.ScalableDimension},
		api.calls[1].Input)
	assert.Equal(t, map[string]any{"ResourceARN": "target-arn"}, api.calls[2].Input)
	delete(api.target, "SuspendedState")
	api.tags["aws:system"] = "hidden"
	out, err = readTarget(t.Context(), client, r.key())
	require.NoError(t, err)
	assert.Nil(t, out.SuspendedState)
	assert.Equal(t, map[string]string{"empty": ""}, out.Tags)
}

func TestTargetPreparationOptionalAndConversion(t *testing.T) {
	r := testTarget()
	r.RoleARN = aws.String("")
	r.Tags = &map[string]string{"aws:hidden": "x"}
	r.SuspendedState = &SuspendedState{}
	got := r.prepare(nil)
	assert.Nil(t, got.RoleARN)
	assert.Nil(t, got.Tags)
	assert.Equal(t, &types.SuspendedState{DynamicScalingInSuspended: aws.Bool(false),
		DynamicScalingOutSuspended: aws.Bool(false), ScheduledScalingSuspended: aws.Bool(false)},
		got.SuspendedState)
	for _, field := range []string{"in", "out", "scheduled"} {
		s := &SuspendedState{}
		switch field {
		case "in":
			s.DynamicScalingInSuspended = aws.Bool(true)
		case "out":
			s.DynamicScalingOutSuspended = aws.Bool(true)
		case "scheduled":
			s.ScheduledScalingSuspended = aws.Bool(true)
		}
		r.SuspendedState = s
		request := r.prepare(nil).SuspendedState
		assert.Equal(t, field == "in", aws.ToBool(request.DynamicScalingInSuspended))
		assert.Equal(t, field == "out", aws.ToBool(request.DynamicScalingOutSuspended))
		assert.Equal(t, field == "scheduled", aws.ToBool(request.ScheduledScalingSuspended))
	}
}

func TestTargetLookupPagesAndErrors(t *testing.T) {
	for _, mode := range []string{"empty", "multiple", "mismatch", "paged", "error"} {
		t.Run(mode, func(t *testing.T) {
			api, client := targetClient(t)
			r := testTarget()
			_, err := r.create(t.Context(), client, &fakeClock{})
			require.NoError(t, err)
			api.calls = nil
			api.hook = func(_ *http.Request, op string, input map[string]any) (map[string]any, string) {
				if op != "DescribeScalableTargets" {
					return nil, ""
				}
				switch mode {
				case "empty":
					return map[string]any{}, ""
				case "error":
					return map[string]any{"message": "remote missing"}, "ObjectNotFoundException"
				case "mismatch":
					bad := maps.Clone(api.target)
					bad["ServiceNamespace"] = "ecs"
					return map[string]any{"ScalableTargets": []any{bad}}, ""
				default:
					if input["NextToken"] == nil {
						page := map[string]any{"NextToken": "second"}
						if mode == "multiple" {
							page["ScalableTargets"] = []any{api.target}
						}
						return page, ""
					}
					assert.Equal(t, "second", input["NextToken"])
					return map[string]any{"ScalableTargets": []any{api.target}}, ""
				}
			}
			out, err := readTarget(t.Context(), client, r.key())
			switch mode {
			case "paged":
				require.NoError(t, err)
				assert.Equal(t, r.ResourceID, out.ResourceID)
				assert.Equal(t, []string{"DescribeScalableTargets", "DescribeScalableTargets",
					"ListTagsForResource"}, api.operations())
			case "error":
				var apiError smithy.APIError
				require.ErrorAs(t, err, &apiError)
				assert.Equal(t, "ObjectNotFoundException", apiError.ErrorCode())
				assert.NotErrorIs(t, err, runtime.ErrNotFound)
			default:
				require.ErrorIs(t, err, runtime.ErrNotFound)
			}
		})
	}
}

func TestTargetRegisterRetry(t *testing.T) {
	for _, tc := range []struct {
		name, code, message string
		transient           bool
	}{
		{"role", "ValidationException", "Unable to assume IAM role x", true},
		{"ecs", "ValidationException", "ECS service doesn't exist x", true},
		{"wrong type", "ObjectNotFoundException", "Unable to assume IAM role", false},
		{"unrelated", "ValidationException", "capacity invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, client := targetClient(t)
			clock := &fakeClock{}
			api.hook = func(_ *http.Request, op string, _ map[string]any) (map[string]any, string) {
				if op == "RegisterScalableTarget" && len(api.calls) == 1 {
					return map[string]any{"message": tc.message}, tc.code
				}
				return nil, ""
			}
			err := registerTarget(t.Context(), client, testTarget().prepare(nil), clock)
			if tc.transient {
				require.NoError(t, err)
				assert.Equal(t, []string{"RegisterScalableTarget", "RegisterScalableTarget"},
					api.operations())
				assert.Equal(t, []time.Duration{500 * time.Millisecond}, clock.delays)
			} else {
				require.ErrorContains(t, err, tc.message)
				assert.Equal(t, []string{"RegisterScalableTarget"}, api.operations())
				assert.Empty(t, clock.delays)
			}
		})
	}
	api, client := targetClient(t)
	clock := &fakeClock{}
	api.hook = func(_ *http.Request, _ string, _ map[string]any) (map[string]any, string) {
		return map[string]any{"message": "Unable to assume IAM role"}, "ValidationException"
	}
	err := registerTarget(t.Context(), client, testTarget().prepare(nil), clock)
	var invalid *types.ValidationException
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, retryDelays(2*time.Minute), clock.delays)
}

func retryDelays(timeout time.Duration) []time.Duration {
	var result []time.Duration
	elapsed, delay := time.Duration(0), 500*time.Millisecond
	for elapsed < timeout {
		result = append(result, delay)
		elapsed += delay
		delay = min(2*delay, 10*time.Second)
	}
	return append(result, delay)
}

func TestTargetRegisterFinalGraceSleep(t *testing.T) {
	for _, mode := range []string{"recovery", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			api, client := targetClient(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := &fakeClock{}
			var attempts []time.Duration
			api.hook = func(_ *http.Request, op string, _ map[string]any) (map[string]any, string) {
				require.Equal(t, "RegisterScalableTarget", op)
				elapsed := clock.Now().Sub(time.Time{})
				attempts = append(attempts, elapsed)
				if elapsed >= 130*time.Second {
					return nil, ""
				}
				return map[string]any{"message": "Unable to assume IAM role"}, "ValidationException"
			}
			if mode == "cancellation" {
				clock.cancel = func() {
					if clock.Now().Sub(time.Time{}) >= 130*time.Second {
						cancel()
					}
				}
			}
			err := registerTarget(ctx, client, testTarget().prepare(nil), clock)
			wantAttempts := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond,
				3500 * time.Millisecond, 7500 * time.Millisecond, 15500 * time.Millisecond,
				25500 * time.Millisecond, 35500 * time.Millisecond, 45500 * time.Millisecond,
				55500 * time.Millisecond, 65500 * time.Millisecond, 75500 * time.Millisecond,
				85500 * time.Millisecond, 95500 * time.Millisecond, 105500 * time.Millisecond,
				115500 * time.Millisecond, 125500 * time.Millisecond}
			if mode == "recovery" {
				require.NoError(t, err)
				wantAttempts = append(wantAttempts, 135500*time.Millisecond)
			} else {
				require.ErrorIs(t, err, context.Canceled)
			}
			assert.Equal(t, wantAttempts, attempts)
			assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second,
				4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second,
				10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
				10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
				10 * time.Second, 10 * time.Second}, clock.delays)
		})
	}
}

func TestTargetCreateAppearanceAndCompensation(t *testing.T) {
	for _, mode := range []string{"delayed", "absent", "describe error", "tag error",
		"cancelled", "cleanup error"} {
		t.Run(mode, func(t *testing.T) {
			api, client := targetClient(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := &fakeClock{}
			describes := 0
			cleaning := false
			api.hook = func(req *http.Request, op string, _ map[string]any) (map[string]any, string) {
				if op == "DeregisterScalableTarget" {
					cleaning = true
					assert.NoError(t, context.Cause(req.Context()))
					if mode == "cleanup error" {
						return map[string]any{"message": "cleanup failed"}, "ValidationException"
					}
				}
				if cleaning {
					return nil, ""
				}
				if op == "ListTagsForResource" && mode == "tag error" {
					return map[string]any{"message": "tag failed"}, "ValidationException"
				}
				if op != "DescribeScalableTargets" {
					return nil, ""
				}
				describes++
				switch mode {
				case "delayed":
					if describes == 1 {
						return map[string]any{}, ""
					}
				case "absent":
					return map[string]any{}, ""
				case "cancelled":
					cancel()
					return map[string]any{}, ""
				case "describe error", "cleanup error":
					return map[string]any{"message": "describe failed"}, "ObjectNotFoundException"
				}
				return nil, ""
			}
			out, err := testTarget().create(ctx, client, clock)
			if mode == "delayed" {
				require.NoError(t, err)
				assert.Equal(t, "target-arn", out.ScalableTargetARN)
				assert.Equal(t, []string{"RegisterScalableTarget", "DescribeScalableTargets",
					"DescribeScalableTargets", "ListTagsForResource"}, api.operations())
				return
			}
			require.Error(t, err)
			assert.Nil(t, out)
			if mode == "cleanup error" {
				assert.ErrorContains(t, err, "describe failed")
				assert.ErrorContains(t, err, "cleanup failed")
			} else {
				assert.Nil(t, api.target)
			}
			if mode == "cancelled" {
				assert.ErrorIs(t, err, context.Canceled)
			}
			if mode == "absent" {
				assert.ErrorIs(t, err, runtime.ErrNotFound)
				assert.Equal(t, retryDelays(2*time.Minute), clock.delays)
			}
			assert.True(t, cleaning)
			for _, call := range api.calls {
				if call.Operation == "DeregisterScalableTarget" {
					assert.Equal(t, map[string]any{"ResourceId": "table/a/b",
						"ServiceNamespace":  "dynamodb",
						"ScalableDimension": "dynamodb:table:ReadCapacityUnits"}, call.Input)
				}
			}
		})
	}
}

func TestTargetDelete(t *testing.T) {
	for _, mode := range []string{"missing", "poll", "persistent", "deregister error", "read error"} {
		t.Run(mode, func(t *testing.T) {
			api, client := targetClient(t)
			r := testTarget()
			out, err := r.create(t.Context(), client, &fakeClock{})
			require.NoError(t, err)
			api.calls = nil
			clock := &fakeClock{}
			saved := maps.Clone(api.target)
			polls := 0
			api.hook = func(_ *http.Request, op string, _ map[string]any) (map[string]any, string) {
				if op == "DeregisterScalableTarget" {
					switch mode {
					case "missing":
						return map[string]any{"message": "gone"}, "ObjectNotFoundException"
					case "deregister error":
						return map[string]any{"message": "bad"}, "ValidationException"
					}
				}
				if op == "DescribeScalableTargets" {
					polls++
					if mode == "read error" {
						return map[string]any{"message": "failed"}, "ObjectNotFoundException"
					}
					if mode == "persistent" || polls == 1 {
						return map[string]any{"ScalableTargets": []any{saved}}, ""
					}
				}
				return nil, ""
			}
			err = deleteTarget(t.Context(), client, out, clock)
			switch mode {
			case "missing":
				require.NoError(t, err)
				assert.Equal(t, []string{"DeregisterScalableTarget"}, api.operations())
			case "poll":
				require.NoError(t, err)
				assert.Equal(t, []string{"DeregisterScalableTarget", "DescribeScalableTargets",
					"DescribeScalableTargets"}, api.operations())
			case "persistent":
				require.ErrorIs(t, err, errTargetPresent)
				assert.Equal(t, retryDelays(5*time.Minute), clock.delays)
			default:
				require.Error(t, err)
			}
		})
	}
}

func TestTargetUpdateManagementAndDrift(t *testing.T) {
	for _, mode := range []string{
		"tags", "capacities", "role", "suspension", "unmanaged", "fallback",
	} {
		t.Run(mode, func(t *testing.T) {
			api, client := targetClient(t)
			r := testTarget()
			r.Tags = &map[string]string{"remove": "x", "change": "old"}
			out, err := r.create(t.Context(), client, &fakeClock{})
			require.NoError(t, err)
			initial := r
			if mode != "fallback" {
				api.target["SuspendedState"] = map[string]any{"DynamicScalingInSuspended": true,
					"DynamicScalingOutSuspended": true, "ScheduledScalingSuspended": true}
			}
			api.target["RoleARN"] = "current-role"
			switch mode {
			case "capacities":
				api.target["MinCapacity"] = float64(7)
			case "role":
				r.RoleARN = aws.String("desired-role")
			case "suspension":
				r.SuspendedState = &SuspendedState{}
			case "unmanaged", "fallback":
				r.MaxCapacity = 11
			}
			r.Tags = &map[string]string{"change": "", "aws:x": "ignore"}
			observed, err := readTarget(t.Context(), client, r.key())
			require.NoError(t, err)
			api.calls = nil
			prior := targetPrior{Inputs: initial, Outputs: out, Observed: observed}
			if mode == "fallback" {
				prior.Observed = nil
			}
			updated, err := r.update(t.Context(), client, prior, &fakeClock{})
			require.NoError(t, err)
			want := []string{"ListTagsForResource", "UntagResource", "TagResource"}
			if mode != "tags" {
				want = append(want, "RegisterScalableTarget")
			}
			want = append(want, "DescribeScalableTargets", "ListTagsForResource")
			assert.Equal(t, want, api.operations())
			assert.Equal(t, []any{"remove"}, api.calls[1].Input["TagKeys"])
			assert.Equal(t, map[string]any{"change": ""}, api.calls[2].Input["Tags"])
			assert.Equal(t, map[string]string{"change": ""}, updated.Tags)
			if mode == "suspension" {
				assert.True(t, sameSuspension(&SuspendedState{},
					updated.SuspendedState))
			}
			if mode == "unmanaged" {
				assert.Equal(t, "current-role", updated.RoleARN)
				assert.True(t, aws.ToBool(updated.SuspendedState.DynamicScalingInSuspended))
			}
			if mode == "fallback" {
				assert.Equal(t, "remote-role", updated.RoleARN)
			}
		})
	}
}

func TestTargetTagFailuresStopUpdate(t *testing.T) {
	for _, operation := range []string{"ListTagsForResource", "UntagResource", "TagResource"} {
		t.Run(operation, func(t *testing.T) {
			api, client := targetClient(t)
			r := testTarget()
			r.Tags = &map[string]string{"drop": "x"}
			out, err := r.create(t.Context(), client, &fakeClock{})
			require.NoError(t, err)
			api.calls = nil
			r.Tags = &map[string]string{"add": ""}
			r.MaxCapacity++
			api.hook = func(_ *http.Request, op string, _ map[string]any) (map[string]any, string) {
				if op == operation {
					return map[string]any{"message": "tag failed"}, "ValidationException"
				}
				return nil, ""
			}
			_, err = r.update(t.Context(), client, targetPrior{Outputs: out}, &fakeClock{})
			require.ErrorContains(t, err, "tag failed")
			assert.NotContains(t, api.operations(), "RegisterScalableTarget")
			assert.Equal(t, operation, api.operations()[len(api.calls)-1])
		})
	}
}

func TestTargetRetryFinalAndCancellation(t *testing.T) {
	pending := errors.New("pending")
	for _, mode := range []string{"final success", "cancel sleep", "cancel operation", "backoff"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := &fakeClock{}
			calls := 0
			var attempts []time.Duration
			if mode == "cancel sleep" {
				clock.cancel = cancel
			}
			err := retryTarget(ctx, clock, 20*time.Second, func(context.Context) error {
				calls++
				attempts = append(attempts, clock.Now().Sub(time.Time{}))
				if mode == "cancel operation" {
					cancel()
				}
				if mode == "final success" && calls == 8 {
					return nil
				}
				return pending
			}, func(error) bool { return true })
			switch mode {
			case "final success":
				require.NoError(t, err)
				assert.Equal(t, 8, calls)
			case "backoff":
				require.ErrorIs(t, err, pending)
				assert.Equal(t, 8, calls)
			default:
				require.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, 1, calls)
			}
			if mode == "backoff" || mode == "final success" {
				assert.Equal(t, []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond,
					3500 * time.Millisecond, 7500 * time.Millisecond, 15500 * time.Millisecond,
					25500 * time.Millisecond, 35500 * time.Millisecond}, attempts)
				assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second,
					4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}, clock.delays)
			}
		})
	}
}
