package eventbridge

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	api "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	types "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const busPolicy = `{"Statement":[{"Action":["events:PutEvents","events:DescribeEventBus"],` +
	`"Effect":"Allow","Resource":"*"}]}`

type policyReply struct {
	out *api.DescribeEventBusOutput
	err error
}

type policyClient struct {
	calls     []any
	replies   []policyReply
	putErr    error
	deleteErr error
	afterPut  func()
	onDelete  func(context.Context)
}

func (c *policyClient) PutPermission(_ context.Context, in *api.PutPermissionInput,
	_ ...func(*api.Options)) (*api.PutPermissionOutput, error) {
	c.calls = append(c.calls, in)
	if c.afterPut != nil {
		c.afterPut()
	}
	return &api.PutPermissionOutput{}, c.putErr
}
func (c *policyClient) DescribeEventBus(_ context.Context, in *api.DescribeEventBusInput,
	_ ...func(*api.Options)) (*api.DescribeEventBusOutput, error) {
	c.calls = append(c.calls, in)
	reply := c.replies[0]
	if len(c.replies) > 1 {
		c.replies = c.replies[1:]
	}
	return reply.out, reply.err
}
func (c *policyClient) RemovePermission(ctx context.Context, in *api.RemovePermissionInput,
	_ ...func(*api.Options)) (*api.RemovePermissionOutput, error) {
	c.calls = append(c.calls, in)
	if c.onDelete != nil {
		c.onDelete(ctx)
	}
	return &api.RemovePermissionOutput{}, c.deleteErr
}

func fastPolicyRetry() policyRetry {
	return policyRetry{now: time.Now, timeout: 2 * time.Minute,
		sleep: func(context.Context, time.Duration) error { return nil }}
}

func TestEventBusPolicyAcceptedCreateCleanup(t *testing.T) {
	failure, cleanupFailure := errors.New("lookup denied"), errors.New("cleanup denied")
	present := policyReply{out: &api.DescribeEventBusOutput{Policy: aws.String(busPolicy)}}
	for _, tt := range []struct {
		name              string
		replies           []policyReply
		putErr, deleteErr error
		cancel            bool
		want              error
		cleanup           bool
	}{
		{"visibility", []policyReply{{err: failure}}, nil, nil, false, failure, true},
		{"exhausted", []policyReply{{}, {}}, nil, nil, false, runtime.ErrNotFound, true},
		{"final read", []policyReply{present, {err: failure}}, nil, nil, false, failure, true},
		{"normalization", []policyReply{present,
			{out: &api.DescribeEventBusOutput{Policy: aws.String("broken")}}},
			nil, nil, false, nil, true},
		{"cleanup denied", []policyReply{{err: failure}}, nil, cleanupFailure,
			false, failure, true},
		{"cleanup absent", []policyReply{{err: failure}}, nil,
			&types.ResourceNotFoundException{}, false, failure, true},
		{"cancellation", nil, nil, nil, true, context.Canceled, true},
		{"rejected", nil, failure, nil, false, failure, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c := &policyClient{replies: tt.replies, putErr: tt.putErr, deleteErr: tt.deleteErr}
			if tt.cancel {
				c.afterPut = cancel
			}
			c.onDelete = func(cleanup context.Context) {
				require.NoError(t, cleanup.Err())
				deadline, ok := cleanup.Deadline()
				require.True(t, ok)
				assert.InDelta(t, 120, time.Until(deadline).Seconds(), 1)
			}
			r := EventBusPolicyResource{Policy: busPolicy}
			retry := fastPolicyRetry()
			if tt.name == "exhausted" {
				retry.timeout = 0
			}
			out, err := r.createPolicy(ctx, c, "accepted-bus", retry)
			require.Error(t, err)
			assert.Nil(t, out)
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			}
			if tt.deleteErr == cleanupFailure {
				require.ErrorIs(t, err, cleanupFailure)
				require.ErrorContains(t, err, "cleanup event bus policy")
			}
			want := []any{&api.PutPermissionInput{
				EventBusName: aws.String("accepted-bus"), Policy: aws.String(busPolicy)}}
			for range tt.replies {
				want = append(want, &api.DescribeEventBusInput{Name: aws.String("accepted-bus")})
			}
			if tt.cleanup {
				want = append(want, &api.RemovePermissionInput{
					EventBusName: aws.String("accepted-bus"), RemoveAllPermissions: true})
			}
			assert.Equal(t, want, c.calls)
		})
	}
}

func TestEventBusPolicyRetrySchedule(t *testing.T) {
	start := time.Unix(0, 0)
	var first []time.Duration
	for range 2 {
		now := start
		var polls, delays []time.Duration
		cfg := policyRetry{timeout: 2 * time.Minute, now: func() time.Time { return now },
			sleep: func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				now = now.Add(delay)
				return nil
			}}
		type callerKey struct{}
		ctx := context.WithValue(t.Context(), callerKey{}, "caller")
		var last error
		err := retryPolicyPresence(ctx, func(got context.Context) error {
			assert.Same(t, ctx, got)
			polls = append(polls, now.Sub(start))
			last = fmt.Errorf("poll %d: %w", len(polls), runtime.ErrNotFound)
			return last
		}, cfg)
		require.ErrorIs(t, err, runtime.ErrNotFound)
		assert.Same(t, last, err)
		want := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond,
			3500 * time.Millisecond, 7500 * time.Millisecond, 15500 * time.Millisecond}
		for at := 25500 * time.Millisecond; at <= 135500*time.Millisecond; at += 10 * time.Second {
			want = append(want, at)
		}
		assert.Equal(t, want, polls)
		wantDelays := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second,
			4 * time.Second, 8 * time.Second}
		for range 12 {
			wantDelays = append(wantDelays, 10*time.Second)
		}
		assert.Equal(t, wantDelays, delays)
		if first != nil {
			assert.Equal(t, first, polls)
		}
		first = polls
	}
}

func TestEventBusPolicyRetryDeadlineAndGrace(t *testing.T) {
	failure := errors.New("denied")
	for _, tt := range []struct {
		name                  string
		timeout, callbackTime time.Duration
		replies               []error
		want                  []time.Duration
		wantErr               error
	}{
		{"zero", 0, 0, []error{nil}, []time.Duration{0}, nil},
		{"exact deadline", time.Second / 2, 0,
			[]error{runtime.ErrNotFound, runtime.ErrNotFound, nil},
			[]time.Duration{0, time.Second / 2, 1500 * time.Millisecond}, nil},
		{"sleep crosses", time.Second / 4, 0,
			[]error{runtime.ErrNotFound, runtime.ErrNotFound, nil},
			[]time.Duration{0, time.Second / 2, 1500 * time.Millisecond}, nil},
		{"callback crosses", time.Second, 2 * time.Second,
			[]error{runtime.ErrNotFound, runtime.ErrNotFound},
			[]time.Duration{0, 2500 * time.Millisecond}, runtime.ErrNotFound},
		{"grace denied", 0, 0, []error{runtime.ErrNotFound, failure},
			[]time.Duration{0, time.Second / 2}, failure},
		{"grace absent", 0, 0, []error{runtime.ErrNotFound, runtime.ErrNotFound},
			[]time.Duration{0, time.Second / 2}, runtime.ErrNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Unix(0, 0)
			now := start
			var polls []time.Duration
			cfg := policyRetry{timeout: tt.timeout, now: func() time.Time { return now },
				sleep: func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }}
			err := retryPolicyPresence(t.Context(), func(context.Context) error {
				polls = append(polls, now.Sub(start))
				now = now.Add(tt.callbackTime)
				return tt.replies[len(polls)-1]
			}, cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, polls)
		})
	}
}

func TestEventBusPolicyRetryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls []string
	err := retryPolicyPresence(ctx, func(context.Context) error {
		calls = append(calls, "poll")
		return runtime.ErrNotFound
	}, policyRetry{now: time.Now, timeout: time.Minute,
		sleep: func(ctx context.Context, delay time.Duration) error {
			calls = append(calls, "sleep")
			assert.Equal(t, 500*time.Millisecond, delay)
			cancel()
			return sleepPolicyRetry(ctx, delay)
		}})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, runtime.ErrNotFound)
	assert.Equal(t, []string{"poll", "sleep"}, calls)
	calls = nil
	err = retryPolicyPresence(ctx, func(context.Context) error {
		calls = append(calls, "unexpected")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, calls)
}

func TestEventBusPolicyPrepare(t *testing.T) {
	for _, policy := range []string{` { "a" : 2 } `, `null`, `true`, `3`, `"text"`, `[]`,
		`{"a":1,"a":2}`} {
		t.Run(policy, func(t *testing.T) {
			r := EventBusPolicyResource{EventBusName: "default", Policy: policy}
			got, err := r.preparePolicy("persisted")
			require.NoError(t, err)
			assert.JSONEq(t, policy, aws.ToString(got.Policy))
			expected := &api.PutPermissionInput{EventBusName: aws.String("persisted"), Policy: got.Policy}
			assert.Equal(t, expected, got)
		})
	}
	r := EventBusPolicyResource{Policy: `broken`}
	_, err := r.preparePolicy("bus")
	require.ErrorContains(t, err, "prepare event bus policy")
}

func TestEventBusPolicySourceNumberNormalization(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`{"z":1.00,"a":1e2,"z":2.0}`, `{"a":100,"z":2}`},
		{`9007199254740993`, `9007199254740992`},
	} {
		r := EventBusPolicyResource{Policy: tt.input}
		in, err := r.preparePolicy("bus")
		require.NoError(t, err)
		assert.Equal(t, &api.PutPermissionInput{
			EventBusName: aws.String("bus"), Policy: aws.String(tt.want)}, in)
	}
	r := EventBusPolicyResource{Policy: `1e1000`}
	c := &policyClient{}
	_, err := r.createPolicy(t.Context(), c, "bus")
	require.ErrorContains(t, err, "prepare event bus policy")
	assert.Empty(t, c.calls)
}

func TestEventBusPolicyPutPresence(t *testing.T) {
	for _, absent := range []policyReply{
		{err: &types.ResourceNotFoundException{}}, {}, {out: &api.DescribeEventBusOutput{}},
		{out: &api.DescribeEventBusOutput{Policy: aws.String("")}},
	} {
		r := EventBusPolicyResource{Policy: busPolicy}
		present := policyReply{out: &api.DescribeEventBusOutput{Policy: aws.String(`{"old":true}`)}}
		c := &policyClient{replies: []policyReply{absent, present, present}}
		out, err := r.putPolicy(t.Context(), c, "old-bus", fastPolicyRetry())
		require.NoError(t, err)
		assert.Equal(t, &EventBusPolicyResourceOutput{Name: "old-bus", Policy: `{"old":true}`}, out)
		assert.Equal(t, []any{
			&api.PutPermissionInput{EventBusName: aws.String("old-bus"), Policy: aws.String(busPolicy)},
			&api.DescribeEventBusInput{Name: aws.String("old-bus")},
			&api.DescribeEventBusInput{Name: aws.String("old-bus")},
			&api.DescribeEventBusInput{Name: aws.String("old-bus")},
		}, c.calls)
		c.calls = nil
		c.replies = []policyReply{absent}
		_, err = r.readPolicy(t.Context(), c, "old-bus")
		require.ErrorIs(t, err, runtime.ErrNotFound)
		assert.Equal(t, []any{
			&api.DescribeEventBusInput{Name: aws.String("old-bus")},
		}, c.calls)
	}
}

func TestEventBusPolicyErrors(t *testing.T) {
	failure := errors.New("denied")
	for _, tt := range []struct {
		name   string
		client *policyClient
		want   string
	}{
		{"put", &policyClient{putErr: failure}, "put event bus policy"},
		{"lookup", &policyClient{replies: []policyReply{{err: failure}}}, "describe event bus policy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := EventBusPolicyResource{Policy: busPolicy}
			_, err := r.putPolicy(t.Context(), tt.client, "bus", fastPolicyRetry())
			require.ErrorIs(t, err, failure)
			require.ErrorContains(t, err, tt.want)
			expected := []any{&api.PutPermissionInput{
				EventBusName: aws.String("bus"), Policy: aws.String(busPolicy)}}
			if tt.name == "lookup" {
				expected = append(expected,
					&api.DescribeEventBusInput{Name: aws.String("bus")})
			}
			assert.Equal(t, expected, tt.client.calls)
		})
	}
	r := EventBusPolicyResource{Policy: busPolicy}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := &policyClient{replies: []policyReply{{}}}
	_, err := r.putPolicy(ctx, c, "bus", policyRetry{now: time.Now, timeout: time.Minute,
		sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }})
	require.ErrorIs(t, err, context.Canceled)
	c = &policyClient{}
	r.Policy = "broken"
	_, err = r.putPolicy(t.Context(), c, "bus")
	require.Error(t, err)
	assert.Empty(t, c.calls)
}

func TestEventBusPolicyDelete(t *testing.T) {
	failure := errors.New("denied")
	for _, err := range []error{nil, &types.ResourceNotFoundException{}, failure} {
		c := &policyClient{deleteErr: err}
		got := deleteBusPolicy(t.Context(), c, "prior-bus")
		if errors.Is(err, failure) {
			require.ErrorIs(t, got, failure)
			require.ErrorContains(t, got, "remove event bus policy")
		} else {
			require.NoError(t, got)
		}
		assert.Equal(t, []any{&api.RemovePermissionInput{
			EventBusName: aws.String("prior-bus"), RemoveAllPermissions: true}}, c.calls)
	}
}

func TestEventBusPolicySelection(t *testing.T) {
	equivalent := `{"Statement":{"Resource":["*"],` +
		`"Action":["events:DescribeEventBus","events:PutEvents"],"Effect":"Allow"}}`
	for _, tt := range []struct{ name, prior, remote, want string }{
		{"equivalent", " " + busPolicy, equivalent, busPolicy},
		{"different", busPolicy, `{"Statement":[]}`, `{"Statement":[]}`},
		{"remote empty", busPolicy, " ", ""},
		{"old whitespace", " ", busPolicy, busPolicy},
		{"empty", "", ` { "remote": true } `, `{"remote":true}`},
		{"remote empty object", busPolicy, " {} ", "{}"},
		{"empty object", "{}", busPolicy, busPolicy},
		{"historical invalid statement", `{"Statement":"broken"}`, busPolicy, busPolicy},
		{"historical malformed", "bad", busPolicy, busPolicy},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectEventBusPolicy(tt.prior, tt.remote)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
	assert.True(t, eventBusPoliciesEqual(busPolicy, equivalent))
	assert.True(t, eventBusPoliciesEqual(
		`{"Statement":{"Action":"events:PutEvents","Effect":"Allow","Resource":"*"}}`,
		`{"Statement":[{"Action":["events:PutEvents"],"Effect":"Allow","Resource":["*"]}]}`,
	))
	assert.False(t, eventBusPoliciesEqual(busPolicy, `{"Statement":[]}`))
	assert.False(t, eventBusPoliciesEqual(busPolicy, "broken"))
	assert.True(t, eventBusPoliciesEqual("", "{}"))
	assert.True(t, eventBusPoliciesEqual("{}", ""))
	_, err := selectEventBusPolicy(busPolicy, "broken")
	require.Error(t, err)
}
