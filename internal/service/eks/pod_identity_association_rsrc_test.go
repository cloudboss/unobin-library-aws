package eks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const propagationMessage = "Role provided in the request does not exist"

func TestPodIdentityAssociationPropagationRetry(t *testing.T) {
	apiErr := &ekstypes.InvalidParameterException{Message: aws.String(propagationMessage)}
	t.Run("success", func(t *testing.T) {
		clock := newFakeClusterClock()
		attempts := 0
		result, err := retryPodIdentityOperation(t.Context(), clock, func(context.Context) (int, error) {
			attempts++
			if attempts == 1 {
				return 0, fmt.Errorf("wrapped: %w", apiErr)
			}
			return 7, nil
		})
		require.NoError(t, err)
		require.Equal(t, 7, result)
		require.Equal(t, 2, attempts)
	})
	t.Run("does not retry other errors", func(t *testing.T) {
		otherErrors := []error{
			&ekstypes.ResourceInUseException{Message: apiErr.Message},
			&ekstypes.InvalidParameterException{Message: aws.String("role" + propagationMessage[4:])},
			&ekstypes.InvalidParameterException{Message: aws.String("unrelated error")},
			errors.New(propagationMessage),
		}
		for _, apiErr := range otherErrors {
			clock := newFakeClusterClock()
			_, err := retryPodIdentityOperation(t.Context(), clock, func(context.Context) (int, error) {
				return 0, apiErr
			})
			require.ErrorIs(t, err, apiErr)
			require.Empty(t, clock.sleeps)
		}
	})
	t.Run("exhaustion preserves the last API error", func(t *testing.T) {
		clock := newFakeClusterClock()
		attempts := 0
		last := fmt.Errorf("last attempt: %w", apiErr)
		_, err := retryPodIdentityOperation(t.Context(), clock, func(context.Context) (int, error) {
			attempts++
			if attempts == 16 {
				return 0, last
			}
			return 0, apiErr
		})
		require.ErrorIs(t, err, last)
		require.Equal(t, 2*time.Minute, clock.elapsed())
		require.Equal(t, []time.Duration{
			500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
			10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
			10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
			4500 * time.Millisecond,
		}, clock.sleeps)
	})
	t.Run("caller cancellation stops retries", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		cause := errors.New("cancelled by caller")
		clock := newFakeClusterClock()
		_, err := retryPodIdentityOperation(ctx, clock, func(context.Context) (int, error) {
			cancel(cause)
			return 0, apiErr
		})
		require.ErrorIs(t, err, cause)
		require.Empty(t, clock.sleeps)
	})
	t.Run("successful reply completes the operation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		clock := newFakeClusterClock()
		result, err := retryPodIdentityOperation(ctx, clock, func(context.Context) (int, error) {
			cancel()
			return 7, nil
		})
		require.NoError(t, err)
		require.Equal(t, 7, result)
		require.Empty(t, clock.sleeps)
	})

	t.Run("unrelated deadline error retains its cause", func(t *testing.T) {
		clock := newFakeClusterClock()
		other := fmt.Errorf("service transport stopped: %w", context.DeadlineExceeded)
		_, err := retryPodIdentityOperation(t.Context(), clock, func(context.Context) (int, error) {
			if clock.elapsed() == 0 {
				return 0, apiErr
			}
			return 0, other
		})
		require.ErrorIs(t, err, other)
	})

}

const (
	podRole       = "arn:aws:iam::111122223333:role/workload"
	podTarget     = "arn:aws:iam::333322221111:role/target"
	podID         = "a-prior"
	podARN        = "arn:aws:eks:us-east-1:111122223333:podidentityassociation/prior/" + podID
	podCollection = "/clusters/prior/pod-identity-associations"
	podPath       = podCollection + "/" + podID
	podTagsPath   = "/tags/" + podARN
)

type podHTTPCall struct {
	method string
	path   string
	query  url.Values
	body   map[string]any
	result string
	status int
}

type podHTTPTransport struct {
	t     *testing.T
	mu    sync.Mutex
	calls []podHTTPCall
}

func (s *podHTTPTransport) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !assert.NotEmpty(s.t, s.calls, "unexpected %s %s", req.Method, req.URL) {
		http.Error(w, "unexpected call", http.StatusInternalServerError)
		return
	}
	call := s.calls[0]
	s.calls = s.calls[1:]
	assert.Equal(s.t, call.method, req.Method)
	assert.Equal(s.t, call.path, req.URL.Path)
	query := call.query
	if query == nil {
		query = url.Values{}
	}
	assert.Equal(s.t, query, req.URL.Query())
	var body map[string]any
	if req.Body != nil && req.Body != http.NoBody {
		assert.NoError(s.t, json.NewDecoder(req.Body).Decode(&body))
	}
	assert.Equal(s.t, call.body, body)
	w.Header().Set("Content-Type", "application/json")
	status := call.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, call.result)
}

func podHTTPClient(t *testing.T, calls ...podHTTPCall) *eks.Client {
	t.Helper()
	script := &podHTTPTransport{t: t, calls: calls}
	server := httptest.NewServer(script)
	t.Cleanup(func() {
		server.Close()
		assert.Empty(t, script.calls)
	})
	return eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: aws.AnonymousCredentials{},
		HTTPClient:  server.Client(),
		Retryer:     func() aws.Retryer { return aws.NopRetryer{} },
	}, func(options *eks.Options) {
		options.BaseEndpoint = aws.String(server.URL)
	})
}

func podReply(t *testing.T, fields map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"association": fields})
	require.NoError(t, err)
	return string(payload)
}

func TestPodIdentityAssociationCreateRequests(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target *string
		tags   map[string]string
	}{
		{name: "omitted target and tags"},
		{name: "cross-account target and filtered tags", target: aws.String(podTarget),
			tags: map[string]string{"empty": "", "aws:ignore": "omitted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{
				"namespace": "ns", "serviceAccount": "account", "roleArn": podRole,
				"disableSessionTags": false, "clientRequestToken": "create-token",
			}
			response := map[string]any{
				"associationId": podID, "associationArn": podARN, "clusterName": "prior",
				"namespace": "ns", "serviceAccount": "account", "roleArn": podRole,
				"disableSessionTags": false,
			}
			want := &PodIdentityAssociationResourceOutput{
				AssociationID: podID, AssociationARN: podARN, ClusterName: "prior",
				Namespace: "ns", ServiceAccount: "account", RoleARN: podRole,
				Tags: map[string]string{},
			}
			if tc.target != nil {
				body["targetRoleArn"] = podTarget
				body["tags"] = map[string]any{"empty": ""}
				response["targetRoleArn"] = podTarget
				response["externalId"] = "external-create"
				response["tags"] = map[string]any{"empty": "", "aws:owner": "eks"}
				want.TargetRoleARN = tc.target
				want.ExternalID = aws.String("external-create")
				want.Tags = map[string]string{"empty": ""}
			}
			client := podHTTPClient(t,
				podHTTPCall{
					method: "POST", path: podCollection, body: body, status: 400,
					result: `{"__type":"InvalidParameterException","message":"` +
						propagationMessage + `"}`,
				},
				podHTTPCall{
					method: "POST", path: podCollection, body: body, result: podReply(t, response),
				},
			)
			res := PodIdentityAssociationResource{
				ClusterName: "prior", Namespace: "ns", ServiceAccount: "account", RoleARN: podRole,
				TargetRoleARN: tc.target, Tags: &tc.tags,
			}
			count := 0
			got, err := res.createPodIdentity(t.Context(), client, newFakeClusterClock(),
				func() (string, error) {
					count++
					return "create-token", nil
				},
			)
			require.NoError(t, err)
			assert.Equal(t, want, got)
			assert.Equal(t, 1, count)
		})
	}
}

func TestPodIdentityAssociationUpdateTargetOmission(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(fmt.Sprint(retained), func(t *testing.T) {
			prior := podPrior()
			prior.Outputs.TargetRoleARN = aws.String(podTarget)
			prior.Outputs.ExternalID = aws.String("external-before")
			response := map[string]any{
				"disableSessionTags": false, "roleArn": podRole,
			}
			want := *prior.Outputs
			want.DisableSessionTags = false
			want.TargetRoleARN, want.ExternalID = nil, nil
			if retained {
				response["targetRoleArn"], response["externalId"] = podTarget, "external-before"
				want.TargetRoleARN = aws.String(podTarget)
				want.ExternalID = aws.String("external-before")
			}
			body := map[string]any{
				"disableSessionTags": false, "roleArn": podRole,
				"clientRequestToken": "update-token",
			}
			client := podHTTPClient(t,
				podHTTPCall{
					method: "POST", path: podPath, body: body, status: 400,
					result: `{"__type":"InvalidParameterException","message":"` +
						propagationMessage + `"}`,
				},
				podHTTPCall{
					method: "POST", path: podPath, body: body, result: podReply(t, response),
				},
			)
			res := PodIdentityAssociationResource{ClusterName: "desired", RoleARN: podRole}
			count := 0
			got, err := res.updatePodIdentity(t.Context(), client, prior, newFakeClusterClock(),
				func() (string, error) {
					count++
					return "update-token", nil
				},
			)
			require.NoError(t, err)
			assert.Equal(t, &want, got)
			assert.Equal(t, 1, count)
		})
	}
}

func podPrior() podIdentityPrior {
	return podIdentityPrior{Outputs: &PodIdentityAssociationResourceOutput{
		AssociationID: podID, AssociationARN: podARN, ClusterName: "prior",
		Namespace: "ns", ServiceAccount: "account", RoleARN: podRole,
		Tags: map[string]string{},
	}}
}

func TestPodIdentityAssociationTagReconciliation(t *testing.T) {
	old := map[string]string{"change": "before", "remove": "old", "same": "retained"}
	desired := map[string]string{"change": "after", "add": "", "same": "retained"}
	list := podHTTPCall{
		method: "GET", path: podTagsPath,
		result: `{"tags":{"change":"before","remove":"old","same":"retained","aws:owner":"eks"}}`,
	}
	for _, tc := range []struct {
		name       string
		desired    map[string]string
		changeCore bool
		wantErr    string
		calls      []podHTTPCall
	}{
		{
			name:    "combined drift, entries removed before upsert and association update",
			desired: desired, changeCore: true,
			calls: []podHTTPCall{
				list,
				{method: "DELETE", path: podTagsPath, query: url.Values{"tagKeys": {"remove"}}, result: "{}"},
				{method: "POST", path: podTagsPath, body: map[string]any{
					"tags": map[string]any{"change": "after", "add": ""},
				}, result: "{}"},
				{method: "POST", path: podPath, body: map[string]any{
					"disableSessionTags": false, "roleArn": podRole, "targetRoleArn": podTarget,
					"clientRequestToken": "tag-core-update",
				}, result: podReply(t, map[string]any{
					"disableSessionTags": false, "roleArn": podRole, "targetRoleArn": podTarget,
					"externalId": "external-after", "tags": map[string]string{"change": "stale"},
				})},
			},
		},
		{
			name: "tag-only removal to empty",
			calls: []podHTTPCall{
				list,
				{method: "DELETE", path: podTagsPath, result: "{}",
					query: url.Values{"tagKeys": {"change", "remove", "same"}}},
			},
		},
		{name: "equal", desired: old},
		{
			name: "untag failure stops tag and core update", desired: desired, changeCore: true,
			wantErr: "tag refused",
			calls: []podHTTPCall{
				list,
				{method: "DELETE", path: podTagsPath, query: url.Values{"tagKeys": {"remove"}},
					status: 400, result: `{"__type":"InvalidParameterException","message":"tag refused"}`},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := podPrior()
			prior.Inputs.Tags = &tc.desired
			prior.Outputs.Tags = tc.desired
			drifted := *prior.Outputs
			drifted.Tags = old
			drifted.ExternalID = aws.String("external-before")
			prior.Observed = &drifted
			want := drifted
			want.Tags = tc.desired
			if want.Tags == nil {
				want.Tags = map[string]string{}
			}
			tags := maps.Clone(tc.desired)
			if tags == nil {
				tags = map[string]string{}
			}
			tags["aws:ignore"] = "filtered"
			res := PodIdentityAssociationResource{RoleARN: podRole, Tags: &tags}
			if tc.changeCore {
				res.TargetRoleARN = aws.String(podTarget)
				want.TargetRoleARN = res.TargetRoleARN
				want.ExternalID = aws.String("external-after")
			}
			got, err := res.updatePodIdentity(t.Context(), podHTTPClient(t, tc.calls...), prior,
				newFakeClusterClock(), func() (string, error) { return "tag-core-update", nil })
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, &want, got)
		})
	}
}

func TestPodIdentityAssociationReadAndDelete(t *testing.T) {
	key := podIdentityKey{clusterName: "prior", associationID: podID}
	t.Run("observed fields and filtered tags", func(t *testing.T) {
		response := podReply(t, map[string]any{
			"associationId": podID, "associationArn": podARN, "clusterName": "prior",
			"namespace": "remote-namespace", "serviceAccount": "remote-account",
			"roleArn": podRole, "disableSessionTags": true, "targetRoleArn": podTarget,
			"externalId": "external-observed", "tags": map[string]string{"empty": "", "aws:foo": "bar"},
		})
		client := podHTTPClient(t, podHTTPCall{method: "GET", path: podPath, result: response})
		got, err := readPodIdentity(t.Context(), client, key)
		require.NoError(t, err)
		assert.Equal(t, &PodIdentityAssociationResourceOutput{
			AssociationID: podID, AssociationARN: podARN, ClusterName: "prior",
			Namespace: "remote-namespace", ServiceAccount: "remote-account", RoleARN: podRole,
			DisableSessionTags: true, TargetRoleARN: aws.String(podTarget),
			ExternalID: aws.String("external-observed"), Tags: map[string]string{"empty": ""},
		}, got)
	})
	for _, tc := range []struct {
		name   string
		status int
		reply  string
	}{
		{"missing", 404, `{"__type":"ResourceNotFoundException","message":"gone"}`},
		{"empty", 200, "{}"},
		{"other error", 400, `{"__type":"InvalidParameterException","message":"bad key"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := podHTTPClient(t,
				podHTTPCall{method: "GET", path: podPath, result: tc.reply, status: tc.status},
				podHTTPCall{method: "DELETE", path: podPath, result: tc.reply, status: tc.status},
			)
			got, err := readPodIdentity(t.Context(), client, key)
			assert.Nil(t, got)
			if tc.status == 400 {
				var invalid *ekstypes.InvalidParameterException
				require.ErrorAs(t, err, &invalid)
				require.ErrorAs(t, deletePodIdentity(t.Context(), client, key), &invalid)
			} else {
				require.ErrorIs(t, err, runtime.ErrNotFound)
				require.NoError(t, deletePodIdentity(t.Context(), client, key))
			}
		})
	}
}

func TestPodIdentityAssociationCreateErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		reply  string
		want   string
	}{
		{"missing association", 200, "{}", "missing association"},
		{"missing ID", 200, podReply(t, map[string]any{
			"clusterName": "prior", "associationArn": podARN,
		}), "invalid association identity"},
		{"missing ARN", 200, podReply(t, map[string]any{
			"clusterName": "prior", "associationId": podID,
		}), "invalid association identity"},
		{"incorrect cluster", 200, podReply(t, map[string]any{
			"clusterName": "other", "associationId": podID, "associationArn": podARN,
		}), "invalid association identity"},
		{"conflict", 409,
			`{"__type":"ResourceInUseException","message":"association exists"}`,
			"association exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := podHTTPClient(t, podHTTPCall{
				method: "POST", path: podCollection, status: tc.status, result: tc.reply,
				body: map[string]any{
					"namespace": "ns", "serviceAccount": "account", "roleArn": podRole,
					"disableSessionTags": false, "clientRequestToken": "create",
				},
			})
			res := PodIdentityAssociationResource{
				ClusterName: "prior", Namespace: "ns", ServiceAccount: "account", RoleARN: podRole,
			}
			out, err := res.createPodIdentity(t.Context(), client, newFakeClusterClock(),
				func() (string, error) { return "create", nil })
			assert.Nil(t, out)
			require.ErrorContains(t, err, tc.want)
			if tc.status == http.StatusConflict {
				var duplicate *ekstypes.ResourceInUseException
				require.ErrorAs(t, err, &duplicate)
			}
		})
	}
}

func TestPodIdentityAssociationMalformedStoredKey(t *testing.T) {
	r := PodIdentityAssociationResource{ClusterName: "desired", RoleARN: podRole}
	for _, tc := range []struct {
		output *PodIdentityAssociationResourceOutput
		want   string
	}{
		{nil, "output is missing"},
		{&PodIdentityAssociationResourceOutput{AssociationID: podID}, "no cluster name"},
		{&PodIdentityAssociationResourceOutput{ClusterName: "prior"}, "no association ID"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			prior := podIdentityPrior{Outputs: tc.output, Observed: podPrior().Outputs}
			_, err := r.Read(t.Context(), nil, prior)
			require.ErrorContains(t, err, tc.want)
			require.ErrorContains(t, r.Delete(t.Context(), nil, prior), tc.want)
			token := func() (string, error) {
				t.Fatal("unexpected token generation")
				return "", nil
			}
			_, err = r.updatePodIdentity(t.Context(), podHTTPClient(t), prior,
				newFakeClusterClock(), token)
			require.ErrorContains(t, err, tc.want)
		})
	}
}
