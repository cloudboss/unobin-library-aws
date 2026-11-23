package eventbridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type putEventsStub struct {
	requests []*eventbridge.PutEventsInput
	put      func(context.Context) (*eventbridge.PutEventsOutput, error)
}

func (s *putEventsStub) PutEvents(
	ctx context.Context, input *eventbridge.PutEventsInput, _ ...func(*eventbridge.Options),
) (*eventbridge.PutEventsOutput, error) {
	s.requests = append(s.requests, input)
	if s.put != nil {
		return s.put(ctx)
	}
	return &eventbridge.PutEventsOutput{}, nil
}

func TestPutEventsRequest(t *testing.T) {
	empty := aws.String("")
	emptyResources := []string{}
	resources := []string{"resource-2", "", "resource-1"}
	offset := time.FixedZone("", 2*60*60+30*60)
	utcTime := time.Date(2026, 11, 22, 23, 45, 0, 0, time.UTC)
	offsetTime := time.Date(2026, 11, 22, 23, 45, 0, 123000000, offset)
	busName := "sample-bus"
	busARN := "arn:aws:events:us-east-1:123456789012:event-bus/sample-bus"
	batch := []PutEventEntry{
		{Source: ""},
		{
			Source: "second", Detail: aws.String("{malformed"), DetailType: empty,
			EventBusName: &busARN, Resources: &resources, Time: aws.String("2026-11-22T23:45:00Z"),
		},
		{
			Source: "third", Detail: empty, DetailType: aws.String("type"),
			EventBusName: &busName, Resources: &emptyResources,
			Time: aws.String("2026-11-22T23:45:00.123+02:30"),
		},
		{Source: "fourth", EventBusName: empty},
	}
	largeBatch := make([]PutEventEntry, 11)
	largeRequest := make([]types.PutEventsRequestEntry, 11)
	for i := range largeBatch {
		source := fmt.Sprintf("entry-%d", i)
		largeBatch[i].Source = source
		largeRequest[i].Source = &source
	}
	twoEntries := batch[:2]
	for _, tt := range []struct {
		name    string
		entries *[]PutEventEntry
		want    []types.PutEventsRequestEntry
	}{
		{name: "omitted", want: []types.PutEventsRequestEntry{}},
		{name: "empty", entries: &[]PutEventEntry{}, want: []types.PutEventsRequestEntry{}},
		{
			name: "two ordered entries", entries: &twoEntries,
			want: []types.PutEventsRequestEntry{
				{Source: empty},
				{
					Source: aws.String("second"), Detail: aws.String("{malformed"), DetailType: empty,
					EventBusName: &busARN, Resources: resources, Time: &utcTime,
				},
			},
		},
		{name: "more than ten", entries: &largeBatch, want: largeRequest},
		{
			name: "ordered entries and optional values", entries: &batch,
			want: []types.PutEventsRequestEntry{
				{Source: empty},
				{
					Source: aws.String("second"), Detail: aws.String("{malformed"), DetailType: empty,
					EventBusName: &busARN, Resources: resources, Time: &utcTime,
				},
				{
					Source: aws.String("third"), Detail: empty, DetailType: aws.String("type"),
					EventBusName: &busName, Resources: emptyResources, Time: &offsetTime,
				},
				{Source: aws.String("fourth"), EventBusName: empty},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub := &putEventsStub{}
			action := &PutEventsAction{Entries: tt.entries}
			result, err := action.run(t.Context(), stub)
			require.NoError(t, err)
			assert.Equal(t, &PutEventsActionOutput{}, result)
			assert.Equal(t, []*eventbridge.PutEventsInput{{Entries: tt.want}}, stub.requests)
		})
	}
}

func TestPutEventsRejectsUnconvertibleTimeBeforeCalling(t *testing.T) {
	batch := []PutEventEntry{{Source: "event", Time: aws.String("bad")}}
	stub := &putEventsStub{}
	result, err := (&PutEventsAction{Entries: &batch}).run(t.Context(), stub)
	require.ErrorAs(t, err, new(*time.ParseError))
	assert.ErrorContains(t, err, "entries[0].time")
	assert.Nil(t, result)
	assert.Empty(t, stub.requests)
}

func TestPutEventsResponse(t *testing.T) {
	failure := errors.New("API failure")
	request := &eventbridge.PutEventsInput{Entries: []types.PutEventsRequestEntry{}}
	for _, tt := range []struct {
		name   string
		output *eventbridge.PutEventsOutput
		cause  error
		err    string
	}{
		{
			name: "success",
			output: &eventbridge.PutEventsOutput{
				Entries: []types.PutEventsResultEntry{{EventId: aws.String("new-id")}},
			},
		},
		{
			name: "partial failure",
			output: &eventbridge.PutEventsOutput{
				FailedEntryCount: 1,
				Entries: []types.PutEventsResultEntry{
					{EventId: aws.String("new-id")},
					{ErrorCode: aws.String("Rejected"), ErrorMessage: aws.String("bad entry")},
				},
			},
			err: "1 entries failed to be processed",
		},
		{name: "SDK error", cause: failure, err: "put EventBridge events: API failure"},
		{
			name: "cancellation", cause: context.Canceled,
			err: "put EventBridge events: context canceled",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cause == context.Canceled {
				cancel()
			}
			stub := &putEventsStub{put: func(callCtx context.Context) (
				*eventbridge.PutEventsOutput, error,
			) {
				assert.Same(t, ctx, callCtx)
				if tt.cause == context.Canceled {
					return nil, callCtx.Err()
				}
				return tt.output, tt.cause
			}}
			result, err := (&PutEventsAction{}).run(ctx, stub)
			assert.Equal(t, []*eventbridge.PutEventsInput{request}, stub.requests)
			if tt.err == "" {
				require.NoError(t, err)
				assert.Equal(t, &PutEventsActionOutput{}, result)
				return
			}
			require.EqualError(t, err, tt.err)
			assert.Nil(t, result)
			if tt.cause != nil {
				assert.ErrorIs(t, err, tt.cause)
			}
		})
	}
}

func TestPutEventsRegionOverride(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "eventbridge-test-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "eventbridge-test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "eventbridge-test-session")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	for _, tt := range []struct {
		name     string
		base     string
		override *string
		want     string
	}{
		{name: "omitted", base: "us-east-1", want: "us-east-1"},
		{name: "empty", base: "us-east-1", override: aws.String(""), want: "us-east-1"},
		{name: "same partition", base: "us-east-1", override: aws.String("us-west-2"),
			want: "us-west-2"},
		{name: "cross partition", base: "us-east-1", override: aws.String("us-gov-west-1")},
		{name: "region pattern", base: "us-gov-west-1", override: aws.String("us-gov-future-99"),
			want: "us-gov-future-99"},
		{name: "unknown fallback", base: "us-east-1", override: aws.String("us-gov-custom"),
			want: "us-gov-custom"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type request struct {
				authorization string
				target        string
				session       string
			}
			requests := make(chan request, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- request{
					authorization: r.Header.Get("Authorization"),
					target:        r.Header.Get("X-Amz-Target"),
					session:       r.Header.Get("X-Amz-Security-Token"),
				}
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				_, _ = w.Write([]byte(`{"FailedEntryCount":0}`))
			}))
			defer server.Close()
			t.Setenv("AWS_REGION", tt.base)
			cfg := &awsCfg{EndpointURL: aws.String(server.URL)}
			action := PutEventsAction{Region: tt.override}
			_, err := action.Run(t.Context(), cfg)
			if tt.want == "" {
				require.ErrorContains(t, err, "partition")
				assert.Empty(t, requests)
				return
			}
			require.NoError(t, err)
			select {
			case req := <-requests:
				const credential = "Credential=eventbridge-test-id/"
				assert.Contains(t, req.authorization, credential)
				assert.Contains(t, req.authorization, "/"+tt.want+"/events/aws4_request")
				assert.Equal(t, "AWSEvents.PutEvents", req.target)
				assert.Equal(t, "eventbridge-test-session", req.session)
			default:
				t.Fatal("missing PutEvents request at the configured endpoint")
			}
			assert.Empty(t, requests)
		})
	}
}
