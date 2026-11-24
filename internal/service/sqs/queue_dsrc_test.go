package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/require"
)

type lookupStub struct {
	t         *testing.T
	name, url string
	calls     []string
	urlOut    *sdk.GetQueueUrlOutput
	attrOut   *sdk.GetQueueAttributesOutput
	tagOut    *sdk.ListQueueTagsOutput
	failAt    string
	failure   error
}

func (s *lookupStub) GetQueueUrl(ctx context.Context, in *sdk.GetQueueUrlInput,
	_ ...func(*sdk.Options)) (*sdk.GetQueueUrlOutput, error) {
	s.calls = append(s.calls, "url")
	require.Equal(s.t, &sdk.GetQueueUrlInput{QueueName: aws.String(s.name)}, in)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.failAt == "url" {
		return nil, s.failure
	}
	return s.urlOut, nil
}

func (s *lookupStub) GetQueueAttributes(ctx context.Context, in *sdk.GetQueueAttributesInput,
	_ ...func(*sdk.Options)) (*sdk.GetQueueAttributesOutput, error) {
	s.calls = append(s.calls, "attributes")
	require.Equal(s.t, &sdk.GetQueueAttributesInput{QueueUrl: aws.String(s.url),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn}}, in)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.failAt == "attributes" {
		return nil, s.failure
	}
	return s.attrOut, nil
}

func (s *lookupStub) ListQueueTags(ctx context.Context, in *sdk.ListQueueTagsInput,
	_ ...func(*sdk.Options)) (*sdk.ListQueueTagsOutput, error) {
	s.calls = append(s.calls, "tags")
	require.Equal(s.t, &sdk.ListQueueTagsInput{QueueUrl: aws.String(s.url)}, in)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.failAt == "tags" {
		return nil, s.failure
	}
	return s.tagOut, nil
}

func newLookupStub(t *testing.T, name string) *lookupStub {
	return &lookupStub{t: t, name: name, url: "unusual://URL /kept",
		urlOut: &sdk.GetQueueUrlOutput{QueueUrl: aws.String("unusual://URL /kept")},
		attrOut: &sdk.GetQueueAttributesOutput{Attributes: map[string]string{
			"QueueArn": "arbitrary ARN unchanged", "VisibilityTimeout": "30"}},
		tagOut: &sdk.ListQueueTagsOutput{Tags: map[string]string{
			"ordinary": "value", "empty": "", "AWS:kept": "upper", "aws:removed": "system"}},
	}
}

func TestQueueLookupSelectorAndOutputs(t *testing.T) {
	names := []string{"standard", "standard.fifo", "", " ", " ?/ü\n", string(make([]byte, 100))}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			api := newLookupStub(t, name)
			got, err := (&QueueDataSource{QueueName: name}).read(t.Context(), api, "us-east-1")
			require.NoError(t, err)
			require.Equal(t, &QueueDataSourceOutput{QueueURL: api.url,
				QueueARN: "arbitrary ARN unchanged", Tags: map[string]string{
					"ordinary": "value", "empty": "", "AWS:kept": "upper"}}, got)
			require.Equal(t, []string{"url", "attributes", "tags"}, api.calls)
		})
	}
}

func TestQueueLookupEmptyResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*lookupStub)
		calls   []string
		message string
	}{
		{"nil URL response", func(s *lookupStub) { s.urlOut = nil }, []string{"url"}, "get queue URL"},
		{"nil URL pointer", func(s *lookupStub) { s.urlOut.QueueUrl = nil },
			[]string{"url"}, "get queue URL"},
		{"nil attributes response", func(s *lookupStub) { s.attrOut = nil },
			[]string{"url", "attributes"}, "get queue ARN"},
		{"nil attributes", func(s *lookupStub) { s.attrOut.Attributes = nil },
			[]string{"url", "attributes"}, "get queue ARN"},
		{"empty attributes", func(s *lookupStub) { s.attrOut.Attributes = map[string]string{} },
			[]string{"url", "attributes"}, "get queue ARN"},
		{"missing ARN", func(s *lookupStub) { delete(s.attrOut.Attributes, "QueueArn") },
			[]string{"url", "attributes"}, "get queue ARN"},
		{"empty ARN", func(s *lookupStub) { s.attrOut.Attributes["QueueArn"] = "" },
			[]string{"url", "attributes"}, "get queue ARN"},
		{"nil tags response", func(s *lookupStub) { s.tagOut = nil },
			[]string{"url", "attributes", "tags"}, "list queue tags"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newLookupStub(t, "selected")
			tc.change(api)
			got, err := (&QueueDataSource{QueueName: api.name}).read(t.Context(), api, "us-east-1")
			require.Nil(t, got)
			require.ErrorContains(t, err, tc.message)
			require.ErrorContains(t, err, "empty response")
			require.Equal(t, tc.calls, api.calls)
		})
	}
}

func TestQueueLookupEmptyURLAndTags(t *testing.T) {
	for _, tc := range []string{"empty URL", "nil tags", "empty tags"} {
		t.Run(tc, func(t *testing.T) {
			api := newLookupStub(t, "selected")
			wantCalls := []string{"url", "attributes", "tags"}
			switch tc {
			case "empty URL":
				api.url = ""
				api.urlOut.QueueUrl = aws.String("")
				wantCalls = []string{"url", "attributes"}
			case "nil tags":
				api.tagOut.Tags = nil
			case "empty tags":
				api.tagOut.Tags = map[string]string{}
			}
			got, err := (&QueueDataSource{QueueName: api.name}).read(t.Context(), api, "us-east-1")
			require.NoError(t, err)
			require.Equal(t, &QueueDataSourceOutput{QueueURL: api.url,
				QueueARN: "arbitrary ARN unchanged", Tags: map[string]string{}}, got)
			require.Equal(t, wantCalls, api.calls)
		})
	}
}

func TestQueueLookupErrors(t *testing.T) {
	for _, failure := range []error{errors.New("transport"), context.Canceled,
		&smithy.GenericAPIError{Code: "QueueDoesNotExist", Message: "missing"},
		&smithy.GenericAPIError{Code: "AWS.SimpleQueueService.NonExistentQueue", Message: "missing"},
		&smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}} {
		for i, stage := range []string{"url", "attributes", "tags"} {
			t.Run(stage+failure.Error(), func(t *testing.T) {
				api := newLookupStub(t, "selected")
				api.failAt = stage
				api.failure = failure
				got, err := (&QueueDataSource{QueueName: api.name}).read(t.Context(), api, "us-east-1")
				require.Nil(t, got)
				require.ErrorIs(t, err, failure)
				require.Contains(t, err.Error(), map[string]string{
					"url": "get queue URL \"selected\"", "attributes": "get queue ARN",
					"tags": "list queue tags"}[stage])
				require.Equal(t, []string{"url", "attributes", "tags"}[:i+1], api.calls)
			})
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	api := newLookupStub(t, "selected")
	got, err := (&QueueDataSource{QueueName: api.name}).read(ctx, api, "us-east-1")
	require.Nil(t, got)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"url"}, api.calls)
}

func TestQueueLookupTagSuppression(t *testing.T) {
	codes := []string{
		"AccessDenied", "AuthorizationError", "InternalException", "InternalServiceError",
		"InvalidAction", "InvalidParameterException", "InvalidParameterValue", "InvalidRequest",
		"OperationDisabledException", "OperationNotPermitted", "UnknownOperationException",
		"UnsupportedFeatureException", "UnsupportedOperation", "ValidationException"}
	failures := []error{errors.New("transport"), context.Canceled,
		&smithy.GenericAPIError{Code: "Unrelated"},
		&smithy.GenericAPIError{Code: "ValidationError", Message: "different"},
		&smithy.GenericAPIError{Code: "ValidationError", Message: "does not support tagging here"}}
	for _, code := range codes {
		failures = append(failures, &smithy.GenericAPIError{Code: "prefix" + code + "suffix"})
	}
	for _, region := range []string{"us-east-1", "us-gov-west-1", "cn-north-1", "us-iso-east-1"} {
		for i, failure := range failures {
			t.Run(region+failure.Error(), func(t *testing.T) {
				api := newLookupStub(t, "selected")
				api.failAt = "tags"
				api.failure = failure
				got, err := (&QueueDataSource{QueueName: api.name}).read(t.Context(), api, region)
				if region != "us-east-1" && i >= 4 {
					require.NoError(t, err)
					require.Equal(t, &QueueDataSourceOutput{QueueURL: api.url,
						QueueARN: "arbitrary ARN unchanged", Tags: map[string]string{}}, got)
				} else {
					require.Nil(t, got)
					require.ErrorIs(t, err, failure)
				}
				require.Equal(t, []string{"url", "attributes", "tags"}, api.calls)
			})
		}
		for _, stage := range []string{"url", "attributes"} {
			api := newLookupStub(t, "selected")
			api.failAt = stage
			api.failure = &smithy.GenericAPIError{Code: "UnsupportedOperation"}
			got, err := (&QueueDataSource{QueueName: api.name}).read(t.Context(), api, region)
			require.Nil(t, got)
			require.ErrorIs(t, err, api.failure)
			want := []string{"url"}
			if stage == "attributes" {
				want = append(want, "attributes")
			}
			require.Equal(t, want, api.calls)
		}
	}
}

func lookupAWSEnvironment(t *testing.T, region string) {
	t.Helper()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "fake-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	t.Setenv("AWS_REGION", region)
}

func TestQueueLookupResolvedRegion(t *testing.T) {
	for _, region := range []string{"us-east-1", "us-gov-west-1", "cn-north-1", "us-iso-east-1"} {
		t.Run(region, func(t *testing.T) {
			lookupAWSEnvironment(t, region)
			var mu sync.Mutex
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				operation := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "AmazonSQS.")
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Contains(t, r.Header.Get("Authorization"), "/"+region+"/sqs/aws4_request")
				mu.Lock()
				calls = append(calls, operation)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-amz-json-1.0")
				var response any
				switch operation {
				case "GetQueueUrl":
					require.Equal(t, map[string]any{"QueueName": "selected"}, body)
					response = map[string]any{"QueueUrl": "exact URL"}
				case "GetQueueAttributes":
					require.Equal(t, map[string]any{
						"QueueUrl": "exact URL", "AttributeNames": []any{"QueueArn"}}, body)
					// The returned ARN is intentionally unrelated to the configured partition.
					response = map[string]any{"Attributes": map[string]string{"QueueArn": "arn:aws:sqs:x:y:z"}}
				case "ListQueueTags":
					require.Equal(t, map[string]any{"QueueUrl": "exact URL"}, body)
					w.WriteHeader(http.StatusBadRequest)
					response = map[string]any{"__type": "UnsupportedOperation", "message": "unsupported"}
				default:
					t.Errorf("unexpected operation %s", operation)
				}
				require.NoError(t, json.NewEncoder(w).Encode(response))
			}))
			defer server.Close()
			got, err := (&QueueDataSource{QueueName: "selected"}).Read(t.Context(), &awsCfg{
				EndpointURL: aws.String(server.URL), MaxAttempts: aws.Int64(1),
			})
			if region == "us-east-1" {
				require.Nil(t, got)
				var apiErr smithy.APIError
				require.ErrorAs(t, err, &apiErr)
				require.Equal(t, "UnsupportedOperation", apiErr.ErrorCode())
			} else {
				require.NoError(t, err)
				require.Equal(t, &QueueDataSourceOutput{QueueURL: "exact URL",
					QueueARN: "arn:aws:sqs:x:y:z", Tags: map[string]string{}}, got)
			}
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, []string{"GetQueueUrl", "GetQueueAttributes", "ListQueueTags"}, calls)
		})
	}
}

func TestQueueLookupHTTPCancellation(t *testing.T) {
	lookupAWSEnvironment(t, "us-east-1")
	started := make(chan struct{})
	stopped := make(chan struct{})
	var calls []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, map[string]any{"QueueName": "selected"}, body)
		mu.Lock()
		calls = append(calls, r.Header.Get("X-Amz-Target"))
		mu.Unlock()
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		got, err := (&QueueDataSource{QueueName: "selected"}).Read(ctx, &awsCfg{
			EndpointURL: aws.String(server.URL), MaxAttempts: aws.Int64(1),
		})
		if got != nil {
			t.Error("canceled read returned outputs")
		}
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("read did not stop")
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("request context stayed active")
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"AmazonSQS.GetQueueUrl"}, calls)
}
