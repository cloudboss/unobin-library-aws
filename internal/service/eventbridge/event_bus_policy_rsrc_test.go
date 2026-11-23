package eventbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	api "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	types "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
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
}

func (c *policyClient) PutPermission(_ context.Context, in *api.PutPermissionInput,
	_ ...func(*api.Options)) (*api.PutPermissionOutput, error) {
	c.calls = append(c.calls, in)
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
func (c *policyClient) RemovePermission(_ context.Context, in *api.RemovePermissionInput,
	_ ...func(*api.Options)) (*api.RemovePermissionOutput, error) {
	c.calls = append(c.calls, in)
	return &api.RemovePermissionOutput{}, c.deleteErr
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

func TestEventBusPolicyPutPresence(t *testing.T) {
	for _, absent := range []policyReply{
		{err: &types.ResourceNotFoundException{}}, {}, {out: &api.DescribeEventBusOutput{}},
		{out: &api.DescribeEventBusOutput{Policy: aws.String("")}},
	} {
		r := EventBusPolicyResource{Policy: busPolicy}
		present := policyReply{out: &api.DescribeEventBusOutput{Policy: aws.String(`{"old":true}`)}}
		c := &policyClient{replies: []policyReply{absent, present, present}}
		out, err := r.putPolicy(t.Context(), c, "old-bus", wait.WithInterval(0))
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
			_, err := r.putPolicy(t.Context(), tt.client, "bus", wait.WithInterval(0))
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
	_, err := r.putPolicy(ctx, c, "bus", wait.WithInterval(time.Hour))
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
	_, err := selectEventBusPolicy(busPolicy, "broken")
	require.Error(t, err)
}
