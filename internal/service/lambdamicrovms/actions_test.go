package lambdamicrovms

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunMicrovmSendsInputAndReturnsOutput(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "POST /2025-09-09/microvms"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, microvmRunResponse()
	})
	loggingDisabled := true
	duration := int64(300)
	payload := "payload"
	action := &RunMicrovmAction{
		ImageIdentifier:          "image-1",
		ImageVersion:             aws.String("1"),
		ExecutionRoleArn:         aws.String("role-1"),
		IngressNetworkConnectors: &[]string{"ingress-1"},
		EgressNetworkConnectors:  &[]string{"egress-1"},
		IdlePolicy: &IdlePolicy{
			AutoResumeEnabled:        true,
			MaxIdleDurationSeconds:   30,
			SuspendedDurationSeconds: 60,
		},
		Logging:                  &Logging{Disabled: &loggingDisabled},
		MaximumDurationInSeconds: &duration,
		RunHookPayloadContent:    &payload,
	}

	out, err := action.Run(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, microvmRunOutput(), out)
	body := sentJSON(t, fake, route, 0)
	assert.Equal(t, "image-1", body["imageIdentifier"])
	assert.Equal(t, "1", body["imageVersion"])
	assert.Equal(t, "role-1", body["executionRoleArn"])
	assert.Equal(t, []any{"ingress-1"}, body["ingressNetworkConnectors"])
	assert.Equal(t, []any{"egress-1"}, body["egressNetworkConnectors"])
	assert.Equal(t, map[string]any{}, body["logging"].(map[string]any)["disabled"])
	assert.Equal(t, float64(300), body["maximumDurationInSeconds"])
	assert.Equal(t, "payload", body["runHookPayload"])
	assert.NotEmpty(t, body["clientToken"])
	idlePolicy := body["idlePolicy"].(map[string]any)
	assert.Equal(t, true, idlePolicy["autoResumeEnabled"])
	assert.Equal(t, float64(30), idlePolicy["maxIdleDurationSeconds"])
	assert.Equal(t, float64(60), idlePolicy["suspendedDurationSeconds"])
}

func TestRunMicrovmPayloadPath(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "POST /2025-09-09/microvms"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, microvmRunResponse()
	})
	path := t.TempDir() + "/payload.txt"
	require.NoError(t, os.WriteFile(path, []byte("file payload"), 0o600))

	_, err := (&RunMicrovmAction{
		ImageIdentifier:    "image-1",
		RunHookPayloadPath: &path,
	}).Run(context.Background(), fake.configuration())
	require.NoError(t, err)
	body := sentJSON(t, fake, route, 0)
	assert.Equal(t, "file payload", body["runHookPayload"])
}

func TestRunMicrovmPayloadTooLargeFailsBeforeAWS(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "POST /2025-09-09/microvms"
	payload := strings.Repeat("x", 16385)

	_, err := (&RunMicrovmAction{
		ImageIdentifier:       "image-1",
		RunHookPayloadContent: &payload,
	}).Run(context.Background(), fake.configuration())
	require.ErrorContains(t, err, "run-hook-payload")
	assert.Empty(t, fake.sent(route))
}

func TestRunMicrovmRejectsTwoPayloadSources(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "POST /2025-09-09/microvms"
	path := t.TempDir() + "/payload.txt"
	require.NoError(t, os.WriteFile(path, []byte("file payload"), 0o600))
	payload := "inline payload"

	_, err := (&RunMicrovmAction{
		ImageIdentifier:       "image-1",
		RunHookPayloadContent: &payload,
		RunHookPayloadPath:    &path,
	}).Run(context.Background(), fake.configuration())
	require.ErrorContains(t, err, "at most one")
	assert.Empty(t, fake.sent(route))
}

func TestCreateMicrovmAuthTokenSendsAllowedPorts(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "POST /2025-09-09/microvms/microvm-1/auth-token"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, `{"authToken":{"X-aws-proxy-auth":"token"}}`
	})
	allPorts := true
	port := int64(8443)

	out, err := (&MicrovmAuthTokenAction{
		MicrovmIdentifier:   "microvm-1",
		ExpirationInMinutes: 15,
		AllowedPorts: []PortSpecification{
			{AllPorts: &allPorts},
			{Port: &port},
			{Range: &PortRange{StartPort: 8000, EndPort: 9000}},
		},
	}).Run(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &MicrovmAuthTokenActionOutput{
		AuthToken: map[string]string{"X-aws-proxy-auth": "token"},
	}, out)
	body := sentJSON(t, fake, route, 0)
	assert.Equal(t, float64(15), body["expirationInMinutes"])
	allowed := body["allowedPorts"].([]any)
	assert.Equal(t, map[string]any{}, allowed[0].(map[string]any)["allPorts"])
	assert.Equal(t, float64(8443), allowed[1].(map[string]any)["port"])
	portRange := allowed[2].(map[string]any)["range"].(map[string]any)
	assert.Equal(t, float64(8000), portRange["startPort"])
	assert.Equal(t, float64(9000), portRange["endPort"])
}

func TestCreateMicrovmShellAuthTokenReturnsSensitiveOutput(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "POST /2025-09-09/microvms/microvm-1/shell-auth-token"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, `{"authToken":{"X-aws-proxy-auth":"shell-token"}}`
	})

	out, err := (&MicrovmShellAuthTokenAction{
		MicrovmIdentifier:   "microvm-1",
		ExpirationInMinutes: 10,
	}).Run(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &MicrovmShellAuthTokenActionOutput{
		AuthToken: map[string]string{"X-aws-proxy-auth": "shell-token"},
	}, out)
	body := sentJSON(t, fake, route, 0)
	assert.Equal(t, float64(10), body["expirationInMinutes"])
}

func TestSuspendMicrovmCallsRoute(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "POST /2025-09-09/microvms/microvm-1/suspend"
	fake.on(route, func(n int) (int, string) { return http.StatusOK, `{}` })

	out, err := (&SuspendMicrovmAction{MicrovmIdentifier: "microvm-1"}).Run(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &SuspendMicrovmActionOutput{MicrovmIdentifier: "microvm-1"}, out)
	assert.Len(t, fake.sent(route), 1)
}

func TestResumeMicrovmCallsRoute(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "POST /2025-09-09/microvms/microvm-1/resume"
	fake.on(route, func(n int) (int, string) { return http.StatusOK, `{}` })

	out, err := (&ResumeMicrovmAction{MicrovmIdentifier: "microvm-1"}).Run(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &ResumeMicrovmActionOutput{MicrovmIdentifier: "microvm-1"}, out)
	assert.Len(t, fake.sent(route), 1)
}

func TestTerminateMicrovmCallsRoute(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "DELETE /2025-09-09/microvms/microvm-1"
	fake.on(route, func(n int) (int, string) { return http.StatusOK, `{}` })

	out, err := (&TerminateMicrovmAction{MicrovmIdentifier: "microvm-1"}).Run(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &TerminateMicrovmActionOutput{MicrovmIdentifier: "microvm-1"}, out)
	assert.Len(t, fake.sent(route), 1)
}

func TestUpdateMicrovmImageVersionStatusSendsPatchAndReturnsVersion(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "PATCH /2025-09-09/microvm-images/image-1/versions/1"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, microvmImageVersionResponse()
	})

	out, err := (&UpdateMicrovmImageVersionStatusAction{
		ImageIdentifier: "image-1",
		ImageVersion:    "1",
		Status:          "ACTIVE",
	}).Run(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t,
		(*UpdateMicrovmImageVersionStatusActionOutput)(microvmImageVersionOutput()), out)
	body := sentJSON(t, fake, route, 0)
	assert.Equal(t, "ACTIVE", body["status"])
}

func microvmRunResponse() string {
	return `{
		"microvmId":"microvm-1",
		"endpoint":"https://microvm.example",
		"imageArn":"image-1",
		"imageVersion":"1",
		"state":"RUNNING",
		"startedAt":1782691200,
		"maximumDurationInSeconds":300,
		"executionRoleArn":"role-1",
		"ingressNetworkConnectors":["ingress-1"],
		"egressNetworkConnectors":["egress-1"],
		"idlePolicy":{
			"autoResumeEnabled":true,
			"maxIdleDurationSeconds":30,
			"suspendedDurationSeconds":60
		},
		"stateReason":"ready"
	}`
}

func microvmRunOutput() *RunMicrovmActionOutput {
	return &RunMicrovmActionOutput{
		MicrovmId:                "microvm-1",
		Endpoint:                 "https://microvm.example",
		ImageArn:                 "image-1",
		ImageVersion:             "1",
		State:                    "RUNNING",
		StartedAt:                "2026-06-29T00:00:00Z",
		MaximumDurationInSeconds: 300,
		ExecutionRoleArn:         "role-1",
		IngressNetworkConnectors: []string{"ingress-1"},
		EgressNetworkConnectors:  []string{"egress-1"},
		IdlePolicy: &IdlePolicy{
			AutoResumeEnabled:        true,
			MaxIdleDurationSeconds:   30,
			SuspendedDurationSeconds: 60,
		},
		StateReason: "ready",
	}
}
