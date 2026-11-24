package codebuild

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBuildClient struct {
	start  func(context.Context, *sdk.StartBuildInput) (*sdk.StartBuildOutput, error)
	fetch  func(context.Context, *sdk.BatchGetBuildsInput) (*sdk.BatchGetBuildsOutput, error)
	starts []*sdk.StartBuildInput
	gets   []*sdk.BatchGetBuildsInput
}

func (c *fakeBuildClient) StartBuild(
	ctx context.Context, in *sdk.StartBuildInput, _ ...func(*sdk.Options),
) (*sdk.StartBuildOutput, error) {
	c.starts = append(c.starts, in)
	if c.start != nil {
		return c.start(ctx, in)
	}
	return &sdk.StartBuildOutput{Build: &types.Build{Id: aws.String("started")}}, nil
}

func (c *fakeBuildClient) BatchGetBuilds(
	ctx context.Context, in *sdk.BatchGetBuildsInput, _ ...func(*sdk.Options),
) (*sdk.BatchGetBuildsOutput, error) {
	c.gets = append(c.gets, in)
	if c.fetch != nil {
		return c.fetch(ctx, in)
	}
	return buildStatus(types.StatusTypeSucceeded), nil
}

func buildStatus(status types.StatusType) *sdk.BatchGetBuildsOutput {
	return &sdk.BatchGetBuildsOutput{Builds: []types.Build{{
		Id: aws.String("different"), BuildStatus: status,
	}, {BuildStatus: types.StatusTypeFailed}}, BuildsNotFound: []string{"started"}}
}

type fakeBuildClock struct {
	time    time.Time
	sleeps  []time.Duration
	onSleep func(context.Context, time.Duration) error
}

func (c *fakeBuildClock) clock() buildClock {
	return buildClock{now: func() time.Time { return c.time },
		sleep: func(ctx context.Context, duration time.Duration) error {
			c.sleeps = append(c.sleeps, duration)
			c.time = c.time.Add(duration)
			if c.onSleep != nil {
				return c.onSleep(ctx, duration)
			}
			return nil
		}}
}

func TestBuildRequest(t *testing.T) {
	for _, test := range []struct {
		name     string
		r        StartBuildAction
		want     *sdk.StartBuildInput
		duration time.Duration
	}{
		{"omitted", StartBuildAction{ProjectName: ""},
			&sdk.StartBuildInput{ProjectName: aws.String("")}, 30 * time.Minute},
		{"empty", StartBuildAction{ProjectName: "p", SourceVersion: aws.String(""),
			EnvironmentVariablesOverride: &[]BuildEnvironmentVariable{}, Timeout: aws.Int64(0)},
			&sdk.StartBuildInput{ProjectName: aws.String("p"), SourceVersion: aws.String(""),
				EnvironmentVariablesOverride: []types.EnvironmentVariable{}}, 0},
		{"populated", StartBuildAction{ProjectName: "p", SourceVersion: aws.String("v"),
			Timeout: aws.Int64(-1), EnvironmentVariablesOverride: &[]BuildEnvironmentVariable{
				{Name: "", Value: ""}, {Name: "x", Value: "a", Type: aws.String("custom")},
				{Name: "x", Value: "b", Type: aws.String("")},
			}}, &sdk.StartBuildInput{ProjectName: aws.String("p"), SourceVersion: aws.String("v"),
			EnvironmentVariablesOverride: []types.EnvironmentVariable{
				{Name: aws.String(""), Value: aws.String("")},
				{Name: aws.String("x"), Value: aws.String("a"), Type: "custom"},
				{Name: aws.String("x"), Value: aws.String("b")},
			}}, -time.Second},
		{"overflow", StartBuildAction{Timeout: aws.Int64(math.MaxInt64)},
			&sdk.StartBuildInput{ProjectName: aws.String("")}, -time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, timeout := test.r.prepareBuildRequest()
			assert.Equal(t, test.want, in)
			assert.Equal(t, test.duration, timeout)
		})
	}
}

func TestBuildInvocationAndMalformedResponses(t *testing.T) {
	apiErr := errors.New("API rejected")
	for _, test := range []struct {
		name    string
		out     *sdk.StartBuildOutput
		err     error
		timeout *int64
		message string
	}{
		{"success", &sdk.StartBuildOutput{Build: &types.Build{Id: aws.String("started")}},
			nil, nil, ""},
		{"start error", nil, apiErr, nil, "starting build"},
		{"nil output", nil, nil, nil, "response has no build ID"},
		{"nil build", &sdk.StartBuildOutput{}, nil, nil, "response has no build ID"},
		{"nil ID", &sdk.StartBuildOutput{Build: &types.Build{}}, nil, nil,
			"response has no build ID"},
		{"empty ID", &sdk.StartBuildOutput{Build: &types.Build{Id: aws.String("")}}, nil,
			nil, "response has no build ID"},
		{"zero timeout", &sdk.StartBuildOutput{Build: &types.Build{Id: aws.String("started")}},
			nil, aws.Int64(0), "timeout must be positive"},
		{"negative timeout", &sdk.StartBuildOutput{Build: &types.Build{Id: aws.String("started")}},
			nil, aws.Int64(-1), "timeout must be positive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := &fakeBuildClock{}
			client := &fakeBuildClient{start: func(context.Context, *sdk.StartBuildInput) (
				*sdk.StartBuildOutput, error,
			) {
				clock.time = clock.time.Add(time.Hour)
				return test.out, test.err
			}}
			r := StartBuildAction{ProjectName: "project", Timeout: test.timeout}
			out, err := r.runBuild(t.Context(), client, clock.clock())
			assert.Equal(t, []*sdk.StartBuildInput{{ProjectName: aws.String("project")}}, client.starts)
			if test.message != "" {
				require.ErrorContains(t, err, test.message)
				assert.Nil(t, out)
				assert.Empty(t, client.gets)
				if test.err != nil {
					assert.ErrorIs(t, err, test.err)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, &StartBuildActionOutput{BuildID: "started"}, out)
			assert.Equal(t, []*sdk.BatchGetBuildsInput{{Ids: []string{"started"}}}, client.gets)
		})
	}
}

func TestBuildStatusesAndFetchErrors(t *testing.T) {
	apiErr := errors.New("fetch failed")
	for _, test := range []struct {
		name    string
		out     *sdk.BatchGetBuildsOutput
		err     error
		message string
	}{
		{"success", buildStatus("SUCCEEDED"), nil, ""},
		{"failed", buildStatus("FAILED"), nil, "build failed with status FAILED"},
		{"fault", buildStatus("FAULT"), nil, "build failed with status FAULT"},
		{"stopped", buildStatus("STOPPED"), nil, "build failed with status STOPPED"},
		{"timed out", buildStatus("TIMED_OUT"), nil, "build failed with status TIMED_OUT"},
		{"unknown", buildStatus("OTHER"), nil, "unexpected build status"},
		{"empty", buildStatus(""), nil, "unexpected build status"},
		{"case", buildStatus("succeeded"), nil, "unexpected build status"},
		{"empty builds", &sdk.BatchGetBuildsOutput{}, nil, "build not found"},
		{"not found", &sdk.BatchGetBuildsOutput{BuildsNotFound: []string{"started"}},
			nil, "build not found"},
		{"nil", nil, nil, "response has no builds"},
		{"API", nil, apiErr, "waiting for build"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeBuildClient{fetch: func(context.Context, *sdk.BatchGetBuildsInput) (
				*sdk.BatchGetBuildsOutput, error,
			) {
				return test.out, test.err
			}}
			clock := &fakeBuildClock{}
			err := waitForBuild(t.Context(), client, "started", time.Minute, clock.clock())
			if test.message == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.message)
				if test.err != nil {
					assert.ErrorIs(t, err, test.err)
				}
				if test.message == "unexpected build status" {
					assert.ErrorContains(t, err, "SUCCEEDED, IN_PROGRESS")
				}
			}
			assert.Equal(t, []*sdk.BatchGetBuildsInput{{Ids: []string{"started"}}}, client.gets)
			assert.Empty(t, clock.sleeps)
		})
	}
}

func TestBuildPollDelaysAndProgress(t *testing.T) {
	for _, slow := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "progress"}[slow], func(t *testing.T) {
			clock := &fakeBuildClock{}
			calls := 0
			var order []string
			clock.onSleep = func(_ context.Context, delay time.Duration) error {
				order = append(order, delay.String())
				return nil
			}
			client := &fakeBuildClient{fetch: func(context.Context, *sdk.BatchGetBuildsInput) (
				*sdk.BatchGetBuildsOutput, error,
			) {
				calls++
				order = append(order, "poll")
				if slow && calls == 2 {
					clock.time = clock.time.Add(2 * time.Minute)
				}
				if calls == 10 {
					return buildStatus("SUCCEEDED"), nil
				}
				return buildStatus("IN_PROGRESS"), nil
			}}
			require.NoError(t, waitForBuild(t.Context(), client, "id", time.Hour, clock.clock()))
			want := []time.Duration{0, 500 * time.Millisecond, time.Second, 2 * time.Second,
				4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second}
			if slow {
				want = []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second,
					8 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second}
			}
			assert.Equal(t, want, clock.sleeps)
			var wantOrder []string
			for _, delay := range want {
				wantOrder = append(wantOrder, "poll", delay.String())
			}
			wantOrder = append(wantOrder, "poll")
			assert.Equal(t, wantOrder, order)
			requests := make([]*sdk.BatchGetBuildsInput, 10)
			for i := range requests {
				requests[i] = &sdk.BatchGetBuildsInput{Ids: []string{"id"}}
			}
			assert.Equal(t, requests, client.gets)
		})
	}
}

func TestBuildDeadlineAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name       string
		advance    time.Duration
		status     types.StatusType
		cancel     bool
		fetchError error
		message    string
	}{
		{"late success", 2 * time.Second, "SUCCEEDED", false, nil, ""},
		{"late failure", 2 * time.Second, "FAILED", false, nil, "build failed"},
		{"late error", 2 * time.Second, "", false, context.DeadlineExceeded, "waiting for build"},
		{"late transition", 2 * time.Second, "IN_PROGRESS", false, nil, "build timeout"},
		{"cancel success", 0, "SUCCEEDED", true, nil, ""},
		{"cancel failure", 0, "FAILED", true, nil, "build failed"},
		{"cancel transition", 2 * time.Second, "IN_PROGRESS", true, nil, "context canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := &fakeBuildClock{}
			client := &fakeBuildClient{fetch: func(context.Context, *sdk.BatchGetBuildsInput) (
				*sdk.BatchGetBuildsOutput, error,
			) {
				clock.time = clock.time.Add(test.advance)
				if test.cancel {
					cancel()
				}
				return buildStatus(test.status), test.fetchError
			}}
			err := waitForBuild(ctx, client, "id", time.Second, clock.clock())
			assert.Equal(t, []*sdk.BatchGetBuildsInput{{Ids: []string{"id"}}}, client.gets)
			if test.message == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.message)
				if test.fetchError != nil {
					assert.ErrorIs(t, err, test.fetchError)
				}
				if test.message == "context canceled" {
					assert.ErrorIs(t, err, context.Canceled)
				}
			}
		})
	}
	for _, equality := range []bool{true, false} {
		clock := &fakeBuildClock{}
		calls := 0
		client := &fakeBuildClient{fetch: func(context.Context, *sdk.BatchGetBuildsInput) (
			*sdk.BatchGetBuildsOutput, error,
		) {
			calls++
			if equality && calls == 4 {
				return buildStatus("SUCCEEDED"), nil
			}
			return buildStatus("IN_PROGRESS"), nil
		}}
		err := waitForBuild(t.Context(), client, "id", 1500*time.Millisecond, clock.clock())
		if equality {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "build timeout")
			assert.Equal(t, []time.Duration{0, 500 * time.Millisecond, time.Second, 2 * time.Second},
				clock.sleeps)
		}
		assert.Equal(t, 4, calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	clock := &fakeBuildClock{}
	client := &fakeBuildClient{}
	require.ErrorIs(t, waitForBuild(ctx, client, "id", -time.Second, clock.clock()), context.Canceled)
	assert.Empty(t, client.gets)
}
