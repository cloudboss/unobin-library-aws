package codebuild

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

func TestBuildSDKRetries(t *testing.T) {
	for _, eventualSuccess := range []bool{false, true} {
		t.Run(map[bool]string{false: "exhausted", true: "success"}[eventualSuccess],
			func(t *testing.T) {
				buildCredentials(t)
				var operations []string
				attempts := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					op := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "CodeBuild_20161006.")
					operations = append(operations, op)
					var body map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					w.Header().Set("Content-Type", "application/x-amz-json-1.1")
					if op == "StartBuild" {
						attempts++
						assert.Equal(t, map[string]any{"projectName": "project"}, body)
						if attempts == 1 || !eventualSuccess {
							w.WriteHeader(500)
							_, _ = w.Write([]byte(`{"__type":"ServiceUnavailableException","message":"retry"}`))
						} else {
							_, _ = w.Write([]byte(`{"build":{"id":"started"}}`))
						}
					} else {
						assert.Equal(t, map[string]any{"ids": []any{"started"}}, body)
						_, _ = w.Write([]byte(`{"builds":[{"buildStatus":"SUCCEEDED"}]}`))
					}
				}))
				defer server.Close()
				r := &StartBuildAction{ProjectName: "project"}
				out, err := r.Run(t.Context(), &awscfg.Configuration{
					EndpointURL: &server.URL, Region: aws.String("us-east-1"), MaxAttempts: aws.Int64(2),
				})
				if eventualSuccess {
					require.NoError(t, err)
					assert.Equal(t, &StartBuildActionOutput{BuildID: "started"}, out)
					assert.Equal(t, []string{"StartBuild", "StartBuild", "BatchGetBuilds"}, operations)
				} else {
					require.ErrorContains(t, err, "starting build")
					assert.Nil(t, out)
					assert.Equal(t, []string{"StartBuild", "StartBuild"}, operations)
				}
			})
	}
}

func TestBuildAPICancellation(t *testing.T) {
	for _, operation := range []string{"StartBuild", "BatchGetBuilds"} {
		t.Run(operation, func(t *testing.T) {
			buildCredentials(t)
			entered := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, err := io.Copy(io.Discard, r.Body)
				require.NoError(t, err)
				if strings.HasSuffix(r.Header.Get("X-Amz-Target"), "."+operation) {
					close(entered)
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				_, _ = w.Write([]byte(`{"build":{"id":"started"}}`))
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go func() {
				select {
				case <-entered:
					cancel()
				case <-ctx.Done():
				}
			}()
			r := &StartBuildAction{ProjectName: "project"}
			out, err := r.Run(ctx, &awscfg.Configuration{
				EndpointURL: &server.URL, Region: aws.String("us-east-1"), MaxAttempts: aws.Int64(1),
			})
			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, out)
		})
	}
}

func TestBuildSleepCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, sleepForBuild(ctx, 0))
	require.ErrorIs(t, sleepForBuild(ctx, time.Hour), context.Canceled)
	clock := &fakeBuildClock{onSleep: func(ctx context.Context, delay time.Duration) error {
		if delay > 0 {
			cancel()
		}
		return sleepForBuild(ctx, delay)
	}}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	client := &fakeBuildClient{fetch: func(context.Context, *sdk.BatchGetBuildsInput) (
		*sdk.BatchGetBuildsOutput, error,
	) {
		return buildStatus("IN_PROGRESS"), nil
	}}
	require.ErrorIs(t, waitForBuild(ctx, client, "id", time.Hour, clock.clock()), context.Canceled)
	assert.Equal(t, []time.Duration{0, 500 * time.Millisecond}, clock.sleeps)
	assert.Equal(t, []*sdk.BatchGetBuildsInput{{Ids: []string{"id"}}, {Ids: []string{"id"}}},
		client.gets)
}
