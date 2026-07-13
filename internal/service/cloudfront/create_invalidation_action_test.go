package cloudfront

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInvalidationClient struct {
	getDistributionFn func(
		*cloudfront.GetDistributionInput,
	) (*cloudfront.GetDistributionOutput, error)
	createInvalidationFn func(
		*cloudfront.CreateInvalidationInput,
	) (*cloudfront.CreateInvalidationOutput, error)
	getInvalidationFn func(
		*cloudfront.GetInvalidationInput,
	) (*cloudfront.GetInvalidationOutput, error)
	calls []string
}

func (f *fakeInvalidationClient) GetDistribution(
	_ context.Context,
	in *cloudfront.GetDistributionInput,
	_ ...func(*cloudfront.Options),
) (*cloudfront.GetDistributionOutput, error) {
	f.calls = append(f.calls, "get-distribution")
	if f.getDistributionFn != nil {
		return f.getDistributionFn(in)
	}
	return &cloudfront.GetDistributionOutput{Distribution: &cloudfronttypes.Distribution{
		DistributionConfig: &cloudfronttypes.DistributionConfig{},
	}}, nil
}

func (f *fakeInvalidationClient) CreateInvalidation(
	_ context.Context,
	in *cloudfront.CreateInvalidationInput,
	_ ...func(*cloudfront.Options),
) (*cloudfront.CreateInvalidationOutput, error) {
	f.calls = append(f.calls, "create-invalidation")
	if f.createInvalidationFn != nil {
		return f.createInvalidationFn(in)
	}
	return &cloudfront.CreateInvalidationOutput{
		Invalidation: &cloudfronttypes.Invalidation{Id: aws.String("INV123")},
	}, nil
}

func (f *fakeInvalidationClient) GetInvalidation(
	_ context.Context,
	in *cloudfront.GetInvalidationInput,
	_ ...func(*cloudfront.Options),
) (*cloudfront.GetInvalidationOutput, error) {
	f.calls = append(f.calls, "get-invalidation")
	if f.getInvalidationFn != nil {
		return f.getInvalidationFn(in)
	}
	return completedInvalidation("INV123"), nil
}

func completedInvalidation(id string) *cloudfront.GetInvalidationOutput {
	return &cloudfront.GetInvalidationOutput{Invalidation: &cloudfronttypes.Invalidation{
		Id:     aws.String(id),
		Status: aws.String("Completed"),
	}}
}

func noInvalidationSleep(context.Context, time.Duration) error { return nil }

func TestCreateInvalidationActionValidation(t *testing.T) {
	tests := []struct {
		name   string
		action CreateInvalidationAction
		want   string
	}{
		{
			name:   "empty distribution id",
			action: CreateInvalidationAction{Paths: []string{"/"}},
			want:   "distribution-id",
		},
		{
			name: "lowercase distribution id",
			action: CreateInvalidationAction{
				DistributionId: "Edist",
				Paths:          []string{"/"},
			},
			want: "distribution-id",
		},
		{
			name: "punctuated distribution id",
			action: CreateInvalidationAction{
				DistributionId: "E-DIST",
				Paths:          []string{"/"},
			},
			want: "distribution-id",
		},
		{
			name: "empty path",
			action: CreateInvalidationAction{
				DistributionId: "EDIST",
				Paths:          []string{""},
			},
			want: "paths[0]",
		},
		{
			name: "path without slash",
			action: CreateInvalidationAction{
				DistributionId: "EDIST",
				Paths:          []string{"images/*"},
			},
			want: "paths[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.action.validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestCreateInvalidationActionValidationAcceptsPaths(t *testing.T) {
	paths := make([]string, 3000)
	for i := range paths {
		paths[i] = "/item"
	}
	paths[0] = "/"
	paths[1] = "/*"
	paths[2] = "*"

	err := (&CreateInvalidationAction{
		DistributionId: "E123ABC",
		Paths:          paths,
	}).validate()
	require.NoError(t, err)
}

func TestCreateInvalidationActionRunValidatesBeforeLoadingConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		action CreateInvalidationAction
		want   string
	}{
		{
			name: "distribution id",
			action: CreateInvalidationAction{
				DistributionId: "invalid",
				Paths:          []string{"/"},
			},
			want: "distribution-id",
		},
		{
			name: "path",
			action: CreateInvalidationAction{
				DistributionId: "EDIST",
				Paths:          []string{"invalid"},
			},
			want: "paths[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.action.Run(context.Background(), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestCreateInvalidationActionSendsRequestAndReturnsCompletedOutput(t *testing.T) {
	var got *cloudfront.CreateInvalidationInput
	client := &fakeInvalidationClient{
		createInvalidationFn: func(
			in *cloudfront.CreateInvalidationInput,
		) (*cloudfront.CreateInvalidationOutput, error) {
			got = in
			return &cloudfront.CreateInvalidationOutput{
				Invalidation: &cloudfronttypes.Invalidation{Id: aws.String("INV456")},
			}, nil
		},
		getInvalidationFn: func(
			in *cloudfront.GetInvalidationInput,
		) (*cloudfront.GetInvalidationOutput, error) {
			assert.Equal(t, "EDIST", aws.ToString(in.DistributionId))
			assert.Equal(t, "INV456", aws.ToString(in.Id))
			return completedInvalidation("INV456"), nil
		},
	}
	callerReference := "request-1"
	action := &CreateInvalidationAction{
		DistributionId:  "EDIST",
		Paths:           []string{"/one", "/*", "*"},
		CallerReference: &callerReference,
	}

	out, err := action.run(context.Background(), client, noInvalidationSleep)
	require.NoError(t, err)
	assert.Equal(t, &CreateInvalidationActionOutput{Id: "INV456", Status: "Completed"}, out)
	assert.Equal(t, []string{
		"get-distribution",
		"create-invalidation",
		"get-invalidation",
	}, client.calls)
	require.NotNil(t, got)
	assert.Equal(t, "EDIST", aws.ToString(got.DistributionId))
	require.NotNil(t, got.InvalidationBatch)
	assert.Equal(t, "request-1", aws.ToString(got.InvalidationBatch.CallerReference))
	require.NotNil(t, got.InvalidationBatch.Paths)
	assert.Equal(t, int32(3), aws.ToInt32(got.InvalidationBatch.Paths.Quantity))
	assert.Equal(t, []string{"/one", "/*", "*"}, got.InvalidationBatch.Paths.Items)
}

func TestCreateInvalidationActionGeneratesFreshCallerReferences(t *testing.T) {
	tests := []struct {
		name            string
		callerReference *string
	}{
		{name: "omitted"},
		{name: "empty", callerReference: aws.String("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var references []string
			client := &fakeInvalidationClient{
				createInvalidationFn: func(
					in *cloudfront.CreateInvalidationInput,
				) (*cloudfront.CreateInvalidationOutput, error) {
					references = append(
						references,
						aws.ToString(in.InvalidationBatch.CallerReference),
					)
					id := "INV" + string(rune('0'+len(references)))
					return &cloudfront.CreateInvalidationOutput{
						Invalidation: &cloudfronttypes.Invalidation{Id: aws.String(id)},
					}, nil
				},
				getInvalidationFn: func(
					in *cloudfront.GetInvalidationInput,
				) (*cloudfront.GetInvalidationOutput, error) {
					return completedInvalidation(aws.ToString(in.Id)), nil
				},
			}
			action := &CreateInvalidationAction{
				DistributionId:  "EDIST",
				Paths:           []string{"/"},
				CallerReference: tt.callerReference,
			}

			for range 2 {
				_, err := action.run(context.Background(), client, noInvalidationSleep)
				require.NoError(t, err)
			}
			require.Len(t, references, 2)
			assert.NotEmpty(t, references[0])
			assert.NotEmpty(t, references[1])
			assert.NotEqual(t, references[0], references[1])
		})
	}
}

func TestCreateInvalidationActionPreflightFailures(t *testing.T) {
	tests := []struct {
		name string
		out  *cloudfront.GetDistributionOutput
		err  error
		want string
	}{
		{
			name: "distribution not found",
			err:  &cloudfronttypes.NoSuchDistribution{},
			want: "distribution EDIST not found",
		},
		{name: "empty response", out: &cloudfront.GetDistributionOutput{}, want: "no distribution"},
		{
			name: "missing distribution config",
			out: &cloudfront.GetDistributionOutput{
				Distribution: &cloudfronttypes.Distribution{},
			},
			want: "no distribution",
		},
		{name: "generic error", err: errors.New("preflight failed"), want: "preflight failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeInvalidationClient{
				getDistributionFn: func(
					*cloudfront.GetDistributionInput,
				) (*cloudfront.GetDistributionOutput, error) {
					return tt.out, tt.err
				},
			}
			action := &CreateInvalidationAction{
				DistributionId: "EDIST",
				Paths:          []string{"/"},
			}

			out, err := action.run(context.Background(), client, noInvalidationSleep)
			require.Error(t, err)
			assert.Nil(t, out)
			assert.Contains(t, err.Error(), tt.want)
			assert.Equal(t, []string{"get-distribution"}, client.calls)
		})
	}
}

func TestCreateInvalidationActionCreateFailuresDoNotPoll(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "too many invalidations", err: &cloudfronttypes.TooManyInvalidationsInProgress{}},
		{name: "invalid argument", err: &cloudfronttypes.InvalidArgument{}},
		{name: "generic failure", err: errors.New("create failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeInvalidationClient{
				createInvalidationFn: func(
					*cloudfront.CreateInvalidationInput,
				) (*cloudfront.CreateInvalidationOutput, error) {
					return nil, tt.err
				},
			}
			action := &CreateInvalidationAction{
				DistributionId: "EDIST",
				Paths:          []string{"/"},
			}

			out, err := action.run(context.Background(), client, noInvalidationSleep)
			require.Error(t, err)
			assert.Nil(t, out)
			assert.ErrorIs(t, err, tt.err)
			assert.Equal(t, []string{"get-distribution", "create-invalidation"}, client.calls)
		})
	}
}

func TestWaitForInvalidationPollsUntilCompleted(t *testing.T) {
	statuses := []string{"InProgress", "InProgress", "Completed"}
	client := &fakeInvalidationClient{
		getInvalidationFn: func(
			*cloudfront.GetInvalidationInput,
		) (*cloudfront.GetInvalidationOutput, error) {
			status := statuses[0]
			statuses = statuses[1:]
			return &cloudfront.GetInvalidationOutput{Invalidation: &cloudfronttypes.Invalidation{
				Id:     aws.String("INV123"),
				Status: aws.String(status),
			}}, nil
		},
	}
	var sleeps []time.Duration
	sleep := func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	out, err := waitForInvalidation(context.Background(), client, "EDIST", "INV123", sleep)
	require.NoError(t, err)
	assert.Equal(t, &CreateInvalidationActionOutput{Id: "INV123", Status: "Completed"}, out)
	assert.Equal(t, []time.Duration{invalidationPollInterval, invalidationPollInterval}, sleeps)
	assert.Equal(t, []string{
		"get-invalidation",
		"get-invalidation",
		"get-invalidation",
	}, client.calls)
}

func TestWaitForInvalidationFailures(t *testing.T) {
	pollErr := errors.New("poll failed")
	tests := []struct {
		name string
		out  *cloudfront.GetInvalidationOutput
		err  error
		want string
	}{
		{
			name: "unexpected status",
			out: &cloudfront.GetInvalidationOutput{Invalidation: &cloudfronttypes.Invalidation{
				Id: aws.String("INV123"), Status: aws.String("Failed"),
			}},
			want: "unexpected status",
		},
		{name: "poll error", err: pollErr, want: "poll failed"},
		{name: "empty response", out: &cloudfront.GetInvalidationOutput{}, want: "no invalidation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeInvalidationClient{
				getInvalidationFn: func(
					*cloudfront.GetInvalidationInput,
				) (*cloudfront.GetInvalidationOutput, error) {
					return tt.out, tt.err
				},
			}

			out, err := waitForInvalidation(
				context.Background(), client, "EDIST", "INV123", noInvalidationSleep)
			require.Error(t, err)
			assert.Nil(t, out)
			assert.Contains(t, err.Error(), tt.want)
			assert.Equal(t, []string{"get-invalidation"}, client.calls)
		})
	}
}

func TestWaitForInvalidationHonorsContext(t *testing.T) {
	tests := []struct {
		name    string
		context func() (context.Context, context.CancelFunc)
		want    error
	}{
		{
			name: "canceled",
			context: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			context: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				return ctx, cancel
			},
			want: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.context()
			defer cancel()
			client := &fakeInvalidationClient{
				getInvalidationFn: func(
					*cloudfront.GetInvalidationInput,
				) (*cloudfront.GetInvalidationOutput, error) {
					return &cloudfront.GetInvalidationOutput{
						Invalidation: &cloudfronttypes.Invalidation{
							Id: aws.String("INV123"), Status: aws.String("InProgress"),
						},
					}, nil
				},
			}
			sleep := func(ctx context.Context, _ time.Duration) error {
				return ctx.Err()
			}

			out, err := waitForInvalidation(ctx, client, "EDIST", "INV123", sleep)
			require.Error(t, err)
			assert.Nil(t, out)
			assert.ErrorIs(t, err, tt.want)
			assert.Equal(t, []string{"get-invalidation"}, client.calls)
		})
	}
}
