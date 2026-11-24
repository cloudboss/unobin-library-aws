package ec2

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stopTestID = "i-0123456789abcdef0"

type stopTestClient struct {
	history  []any
	describe func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	stop     func(*ec2.StopInstancesInput) (*ec2.StopInstancesOutput, error)
}

func TestStopInstancePagination(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		t.Run(fmt.Sprint(multiple), func(t *testing.T) {
			client := &stopTestClient{describe: func(in *ec2.DescribeInstancesInput) (
				*ec2.DescribeInstancesOutput, error,
			) {
				if in.NextToken == nil {
					page := stopTestPage()
					if multiple {
						page = stopTestPage("stopped")
					}
					page.NextToken = aws.String("later")
					return page, nil
				}
				return stopTestPage("stopped"), nil
			}}
			action := StopInstanceAction{InstanceID: stopTestID}
			out, err := action.runStopInstance(t.Context(), client, stopTestClock(client))
			if multiple {
				require.ErrorContains(t, err, "instance not found")
				assert.Nil(t, out)
			} else {
				require.NoError(t, err)
				assert.Equal(t, &StopInstanceActionOutput{}, out)
			}
			assert.Equal(t, []any{
				ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}},
				ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}, NextToken: aws.String("later")},
			}, client.history)
		})
	}
	client := &stopTestClient{describe: func(*ec2.DescribeInstancesInput) (
		*ec2.DescribeInstancesOutput, error,
	) {
		return &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{
			Instances: append(stopTestPage("running").Reservations[0].Instances,
				stopTestPage("stopped").Reservations[0].Instances...),
		}}}, nil
	}}
	_, err := findStopInstance(t.Context(), client, stopTestID)
	require.ErrorIs(t, err, errStopInstanceMissing)
}

func TestStopInstanceWaitFailuresAndStopOutputs(t *testing.T) {
	nilState := stopTestPage("stopping")
	nilState.Reservations[0].Instances[0].State = nil
	wrongID := stopTestPage("stopping")
	wrongID.Reservations[0].Instances[0].InstanceId = aws.String("other")
	apiErr := &smithy.GenericAPIError{Code: "InvalidInstanceID.NotFound", Message: "gone"}
	for _, test := range []struct {
		name string
		page *ec2.DescribeInstancesOutput
		err  error
		want string
	}{
		{"missing", stopTestPage(), nil, "wait for instance"},
		{"nil state", nilState, nil, "wait for instance"},
		{"wrong id", wrongID, nil, "wait for instance"},
		{"multiple", stopTestPage("stopping", "stopped"), nil, "wait for instance"},
		{"nil", nil, nil, "nil response"},
		{"API missing", nil, apiErr, "wait for instance"},
		{"cancelled", nil, context.Canceled, "wait for instance"},
		{"terminated", stopTestPage("terminated"), nil, "wait for instance"},
		{"pending", stopTestPage("pending"), nil, `unexpected state "pending"`},
		{"unknown", stopTestPage("unknown"), nil, `unexpected state "unknown"`},
		{"empty", stopTestPage(""), nil, `unexpected state ""`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &stopTestClient{}
			calls := 0
			client.describe = func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
				calls++
				if calls == 1 {
					return stopTestPage("running"), nil
				}
				return test.page, test.err
			}
			action := StopInstanceAction{InstanceID: stopTestID}
			out, err := action.runStopInstance(t.Context(), client, stopTestClock(client))
			require.ErrorContains(t, err, test.want)
			assert.ErrorContains(t, err, stopTestID)
			assert.Nil(t, out)
			if test.err != nil {
				assert.ErrorIs(t, err, test.err)
			}
			assert.Equal(t, []any{
				ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}},
				ec2.StopInstancesInput{InstanceIds: []string{stopTestID}, Force: aws.Bool(false)},
				ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}},
			}, client.history)
		})
	}
	for _, output := range []*ec2.StopInstancesOutput{nil, {}} {
		client := &stopTestClient{}
		client.describe = func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			if len(client.history) == 1 {
				return stopTestPage("running"), nil
			}
			return stopTestPage("stopped"), nil
		}
		stopErr := errors.New("stop denied")
		for _, fail := range []bool{false, true} {
			client.history = nil
			client.stop = func(*ec2.StopInstancesInput) (*ec2.StopInstancesOutput, error) {
				if fail {
					return nil, stopErr
				}
				return output, nil
			}
			action := StopInstanceAction{InstanceID: stopTestID}
			_, err := action.runStopInstance(t.Context(), client, stopTestClock(client))
			want := []any{
				ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}},
				ec2.StopInstancesInput{InstanceIds: []string{stopTestID}, Force: aws.Bool(false)},
			}
			if fail {
				require.ErrorContains(t, err, "stop instance "+stopTestID)
				assert.ErrorIs(t, err, stopErr)
			} else {
				require.NoError(t, err)
				want = append(want, ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}})
			}
			assert.Equal(t, want, client.history)
		}
	}
}

func TestStopInstanceWaitClock(t *testing.T) {
	for _, test := range []struct {
		name        string
		timeout     time.Duration
		fetchTime   time.Duration
		states      []types.InstanceStateName
		want        []any
		wantTimeout bool
	}{
		{"transitions", time.Minute, 0,
			[]types.InstanceStateName{"running", "stopping", "shutting-down", "running", "stopped"},
			[]any{"fetch", 10 * time.Second, "fetch", 10 * time.Second, "fetch",
				10 * time.Second, "fetch", 10 * time.Second, "fetch"}, false},
		{"equal deadline", 10 * time.Second, 0, []types.InstanceStateName{"stopping", "stopped"},
			[]any{"fetch", 10 * time.Second, "fetch"}, false},
		{"after deadline", 9 * time.Second, 0, []types.InstanceStateName{"stopping"},
			[]any{"fetch", 10 * time.Second}, true},
		{"success crosses", time.Second, 2 * time.Second, []types.InstanceStateName{"stopped"},
			[]any{"fetch"}, false},
		{"transition crosses", time.Second, 2 * time.Second, []types.InstanceStateName{"running"},
			[]any{"fetch", 10 * time.Second}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(0, 0)
			var history []any
			client := &stopTestClient{describe: func(*ec2.DescribeInstancesInput) (
				*ec2.DescribeInstancesOutput, error,
			) {
				history = append(history, "fetch")
				now = now.Add(test.fetchTime)
				state := test.states[0]
				test.states = test.states[1:]
				return stopTestPage(state), nil
			}}
			clock := stopInstanceClock{
				now: func() time.Time { return now },
				sleep: func(_ context.Context, delay time.Duration) error {
					history = append(history, delay)
					now = now.Add(delay)
					return nil
				},
			}
			_, err := waitForStopInstance(t.Context(), client, stopTestID, test.timeout, clock)
			if test.wantTimeout {
				require.ErrorContains(t, err, "timed out after "+test.timeout.String())
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.want, history)
		})
	}
}

func TestStopInstanceTimeoutStartsAfterLookupAndStop(t *testing.T) {
	client := &stopTestClient{}
	now := time.Unix(0, 0)
	client.describe = func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		now = now.Add(time.Hour)
		if len(client.history) == 1 {
			return stopTestPage("running"), nil
		}
		return stopTestPage("stopped"), nil
	}
	client.stop = func(*ec2.StopInstancesInput) (*ec2.StopInstancesOutput, error) {
		now = now.Add(time.Hour)
		return nil, nil
	}
	action := StopInstanceAction{InstanceID: stopTestID, Timeout: aws.Int64(30)}
	clock := stopTestClock(client)
	clock.now = func() time.Time { return now }
	_, err := action.runStopInstance(t.Context(), client, clock)
	require.NoError(t, err)
	assert.Equal(t, []any{
		ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}},
		ec2.StopInstancesInput{InstanceIds: []string{stopTestID}, Force: aws.Bool(false)},
		ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}},
	}, client.history)
}

func TestStopInstanceAcceptedStopTimesOut(t *testing.T) {
	client := &stopTestClient{describe: func(*ec2.DescribeInstancesInput) (
		*ec2.DescribeInstancesOutput, error,
	) {
		return stopTestPage("running"), nil
	}}
	action := StopInstanceAction{InstanceID: stopTestID, Timeout: aws.Int64(30)}
	out, err := action.runStopInstance(t.Context(), client, stopTestClock(client))
	require.ErrorContains(t, err, "timed out after 30s waiting for instance "+stopTestID)
	assert.Nil(t, out)
	describe := ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}}
	assert.Equal(t, []any{
		describe, ec2.StopInstancesInput{InstanceIds: []string{stopTestID}, Force: aws.Bool(false)},
		describe, 10 * time.Second, describe, 10 * time.Second,
		describe, 10 * time.Second, describe, 10 * time.Second,
	}, client.history)
}

func TestStopInstanceCancellation(t *testing.T) {
	for _, when := range []string{"before", "fetch success", "fetch transition", "sleep", "repeated"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client := &stopTestClient{}
			calls := 0
			client.describe = func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
				calls++
				if when == "fetch success" || when == "fetch transition" {
					cancel()
				}
				if when == "fetch success" {
					return stopTestPage("stopped"), nil
				}
				return stopTestPage("running"), nil
			}
			clock := stopTestClock(client)
			clock.sleep = func(ctx context.Context, delay time.Duration) error {
				client.history = append(client.history, delay)
				if when == "sleep" || when == "repeated" {
					cancel()
				}
				if when == "repeated" {
					return nil
				}
				return sleepForStopInstance(ctx, time.Hour)
			}
			if when == "before" {
				cancel()
			}
			out, err := waitForStopInstance(ctx, client, stopTestID, time.Second, clock)
			var want []any
			if when != "before" {
				want = append(want, ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}})
			}
			if when == "fetch success" {
				require.NoError(t, err)
				assert.Equal(t, &StopInstanceActionOutput{}, out)
			} else {
				require.ErrorIs(t, err, context.Canceled)
				assert.Nil(t, out)
				if when != "before" {
					want = append(want, 10*time.Second)
				}
			}
			assert.Equal(t, want, client.history)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := &stopTestClient{}
	_, err := waitForStopInstance(ctx, client, stopTestID, -time.Second, stopTestClock(client))
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, client.history)
	require.NoError(t, sleepForStopInstance(t.Context(), 0))
}

func TestStopInstanceRetryClassification(t *testing.T) {
	base := retry.NewStandard(func(options *retry.StandardOptions) {
		options.MaxAttempts = 2
	})
	r := stopInstanceRetryer{Retryer: base}
	for _, test := range []struct{ code, message string }{
		{"InvalidParameterValue",
			"This call cannot be completed because there are pending VPNs or Virtual Interfaces"},
		{"InsufficientInstanceCapacity", ""},
		{"OperationNotPermitted", "Endpoint cannot be created while another endpoint is being created"},
		{"ConcurrentMutationLimitExceeded",
			"Cannot initiate another change for this endpoint at this time"},
		{"VpnConcentratorLimitExceeded", "maximum number of mutating objects has been reached"},
		{"VpnConnectionLimitExceeded", "maximum number of mutating objects has been reached"},
		{"VpnGatewayLimitExceeded", "maximum number of mutating objects has been reached"},
	} {
		t.Run(test.code, func(t *testing.T) {
			err := &smithy.GenericAPIError{Code: test.code, Message: "prefix " + test.message + " suffix"}
			assert.True(t, r.IsErrorRetryable(fmt.Errorf("wrapped: %w", err)))
			if test.message != "" {
				err.Message = "near miss"
				assert.Equal(t, base.IsErrorRetryable(err), r.IsErrorRetryable(err))
			}
			err.Code += "Extra"
			assert.Equal(t, base.IsErrorRetryable(err), r.IsErrorRetryable(err))
		})
	}
	for _, err := range []error{errors.New("other"), context.Canceled,
		&smithy.GenericAPIError{Code: "RequestLimitExceeded"}} {
		assert.Equal(t, base.IsErrorRetryable(err), r.IsErrorRetryable(err))
	}
	assert.Equal(t, 2, r.MaxAttempts())
}

func TestStopInstancePartitions(t *testing.T) {
	for region, want := range map[string]string{
		"": "aws", "us-east-1": "aws", "us-west-2": "aws", "unrecognized": "aws",
		"us-gov-west-1": "aws-us-gov", "cn-north-1": "aws-cn",
	} {
		assert.Equal(t, want, stopInstancePartition(region), region)
	}
}

func TestStopInstanceConfiguredSDKRetries(t *testing.T) {
	for _, test := range []struct {
		code, message string
		attempts      int
	}{
		{"InsufficientInstanceCapacity", "capacity", 2},
		{"RequestLimitExceeded", "throttled", 2},
		{"OperationNotPermitted", "near miss", 1},
	} {
		t.Run(test.code, func(t *testing.T) {
			server := newFakeEC2(t)
			server.on("DescribeInstances", func(int, url.Values) (int, string) {
				return 400, ec2ErrorXML(test.code, test.message)
			})
			cfg := server.configuration()
			cfg.MaxAttempts = aws.Int64(2)
			action := StopInstanceAction{InstanceID: stopTestID}
			out, err := action.Run(t.Context(), cfg)
			require.ErrorContains(t, err, "describe instance "+stopTestID)
			assert.Nil(t, out)
			var want []url.Values
			for range test.attempts {
				want = append(want, url.Values{
					"Action": {"DescribeInstances"}, "InstanceId.1": {stopTestID},
					"Version": {"2016-11-15"},
				})
			}
			assert.Equal(t, want, server.sent("DescribeInstances"))
			assert.Empty(t, server.sent("StopInstances"))
		})
	}
	server := newFakeEC2(t)
	server.on("DescribeInstances", func(int, url.Values) (int, string) {
		return 200, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item>` +
			`<instanceId>` + stopTestID + `</instanceId><instanceState><name>running</name>` +
			`</instanceState></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`
	})
	server.on("StopInstances", func(int, url.Values) (int, string) {
		return 400, ec2ErrorXML("InsufficientInstanceCapacity", "capacity")
	})
	cfg := server.configuration()
	cfg.MaxAttempts = aws.Int64(2)
	action := StopInstanceAction{InstanceID: stopTestID}
	_, err := action.Run(t.Context(), cfg)
	require.ErrorContains(t, err, "stop instance "+stopTestID)
	assert.Equal(t, []url.Values{{
		"Action": {"DescribeInstances"}, "InstanceId.1": {stopTestID}, "Version": {"2016-11-15"},
	}}, server.sent("DescribeInstances"))
	stop := url.Values{
		"Action": {"StopInstances"}, "InstanceId.1": {stopTestID}, "Force": {"false"},
		"Version": {"2016-11-15"},
	}
	assert.Equal(t, []url.Values{stop, stop}, server.sent("StopInstances"))
}

func TestStopInstanceSDKCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if !assert.NoError(t, r.ParseForm()) {
			return
		}
		calls.Add(1)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	fake := newFakeEC2(t)
	cfg := fake.configuration()
	cfg.EndpointURL = &server.URL
	cfg.MaxAttempts = aws.Int64(3)
	go func() {
		<-started
		cancel()
	}()
	action := StopInstanceAction{InstanceID: stopTestID}
	_, err := action.Run(ctx, cfg)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int64(1), calls.Load())
}

func (c *stopTestClient) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput,
	_ ...func(*ec2.Options),
) (*ec2.DescribeInstancesOutput, error) {
	copy := *in
	c.history = append(c.history, copy)
	return c.describe(in)
}

func (c *stopTestClient) StopInstances(_ context.Context, in *ec2.StopInstancesInput,
	_ ...func(*ec2.Options),
) (*ec2.StopInstancesOutput, error) {
	c.history = append(c.history, *in)
	if c.stop != nil {
		return c.stop(in)
	}
	return nil, nil
}

func stopTestPage(states ...types.InstanceStateName) *ec2.DescribeInstancesOutput {
	page := &ec2.DescribeInstancesOutput{}
	for _, state := range states {
		page.Reservations = append(page.Reservations, types.Reservation{Instances: []types.Instance{{
			InstanceId: aws.String(stopTestID), State: &types.InstanceState{Name: state},
		}}})
	}
	return page
}

func stopTestClock(client *stopTestClient) stopInstanceClock {
	now := time.Unix(0, 0)
	return stopInstanceClock{
		now: func() time.Time { return now },
		sleep: func(_ context.Context, delay time.Duration) error {
			client.history = append(client.history, delay)
			now = now.Add(delay)
			return nil
		},
	}
}

func TestStopInstancePrepareAndBranches(t *testing.T) {
	for _, force := range []*bool{nil, aws.Bool(false), aws.Bool(true)} {
		for _, initial := range []types.InstanceStateName{"stopped", "stopping", "running"} {
			t.Run(fmt.Sprint(initial, force), func(t *testing.T) {
				action := StopInstanceAction{InstanceID: stopTestID, Force: force}
				request, timeout := action.prepareStopRequest()
				assert.Equal(t, &ec2.StopInstancesInput{
					InstanceIds: []string{stopTestID}, Force: aws.Bool(aws.ToBool(force)),
				}, request)
				assert.Equal(t, 600*time.Second, timeout)
				client := &stopTestClient{}
				calls := 0
				client.describe = func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
					calls++
					if calls == 1 {
						return stopTestPage(initial), nil
					}
					return stopTestPage("stopped"), nil
				}
				out, err := action.runStopInstance(t.Context(), client, stopTestClock(client))
				require.NoError(t, err)
				assert.Equal(t, &StopInstanceActionOutput{}, out)
				want := []any{ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}}}
				if initial == "running" {
					want = append(want, *request)
				}
				if initial != "stopped" {
					want = append(want, ec2.DescribeInstancesInput{InstanceIds: []string{stopTestID}})
				}
				assert.Equal(t, want, client.history)
			})
		}
	}
	for _, seconds := range []int64{30, 3600} {
		action := StopInstanceAction{Timeout: &seconds}
		_, timeout := action.prepareStopRequest()
		assert.Equal(t, time.Duration(seconds)*time.Second, timeout)
	}
}

func TestStopInstanceInitialFailures(t *testing.T) {
	apiErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}
	missing := &smithy.GenericAPIError{Code: "InvalidInstanceID.NotFound", Message: "gone"}
	nilState := stopTestPage("running")
	nilState.Reservations[0].Instances[0].State = nil
	wrongID := stopTestPage("running")
	wrongID.Reservations[0].Instances[0].InstanceId = aws.String("i-other")
	for _, test := range []struct {
		name string
		page *ec2.DescribeInstancesOutput
		err  error
		want string
	}{
		{"not found", nil, fmt.Errorf("wrapped: %w", missing), "instance not found"},
		{"empty", stopTestPage(), nil, "instance not found"},
		{"nil state", nilState, nil, "instance not found"},
		{"wrong id", wrongID, nil, "instance not found"},
		{"terminated", stopTestPage("terminated"), nil, "instance not found"},
		{"multiple", stopTestPage("running", "stopped"), nil, "instance not found"},
		{"nil response", nil, nil, "describe instance"},
		{"denied", nil, fmt.Errorf("wrapped: %w", apiErr), "describe instance"},
		{"cancelled", nil, context.Canceled, "describe instance"},
		{"pending", stopTestPage("pending"), nil, "cannot stop"},
		{"shutting down", stopTestPage("shutting-down"), nil, "cannot stop"},
		{"unknown", stopTestPage("unknown"), nil, "cannot stop"},
		{"empty state", stopTestPage(""), nil, "cannot stop"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &stopTestClient{describe: func(*ec2.DescribeInstancesInput) (
				*ec2.DescribeInstancesOutput, error,
			) {
				return test.page, test.err
			}}
			action := StopInstanceAction{InstanceID: stopTestID}
			out, err := action.runStopInstance(t.Context(), client, stopTestClock(client))
			require.ErrorContains(t, err, test.want)
			assert.ErrorContains(t, err, stopTestID)
			assert.Nil(t, out)
			if test.err != nil {
				assert.True(t, errors.Is(err, test.err))
			}
			assert.Equal(t, []any{ec2.DescribeInstancesInput{
				InstanceIds: []string{stopTestID},
			}}, client.history)
		})
	}
}
