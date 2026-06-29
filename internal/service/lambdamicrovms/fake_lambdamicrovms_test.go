package lambdamicrovms

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"

	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLambdaMicrovms struct {
	t           *testing.T
	mu          sync.Mutex
	calls       map[string]int
	handlers    map[string]func(n int) (int, string)
	bodies      map[string][][]byte
	queryValues map[string][]url.Values
	server      *httptest.Server
}

func newFakeLambdaMicrovms(t *testing.T) *fakeLambdaMicrovms {
	fake := &fakeLambdaMicrovms{
		t:           t,
		calls:       map[string]int{},
		handlers:    map[string]func(int) (int, string){},
		bodies:      map[string][][]byte{},
		queryValues: map[string][]url.Values{},
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeLambdaMicrovms) on(route string, h func(n int) (int, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[route] = h
}

func (f *fakeLambdaMicrovms) sent(route string) [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.bodies[route]...)
}

func (f *fakeLambdaMicrovms) queries(route string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.queryValues[route]...)
}

func (f *fakeLambdaMicrovms) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("fake lambdamicrovms: read body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	route := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.calls[route]++
	f.bodies[route] = append(f.bodies[route], body)
	f.queryValues[route] = append(f.queryValues[route], r.URL.Query())
	h, ok := f.handlers[route]
	n := f.calls[route]
	f.mu.Unlock()
	if !ok {
		f.t.Errorf("fake lambdamicrovms: no handler for route %s", route)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	status, resp := h(n)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, resp)
}

func (f *fakeLambdaMicrovms) configuration() *awscfg.Configuration {
	dir := f.t.TempDir()
	f.t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))
	f.t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	f.t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	f.t.Setenv("AWS_PROFILE", "")
	f.t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFAKEFAKEFAKEFAKE")
	f.t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	region := "us-east-1"
	endpointURL := f.server.URL
	return &awscfg.Configuration{
		Region:      &region,
		EndpointURL: &endpointURL,
	}
}

func TestFakeLambdaMicrovmsRecordsBodies(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "POST /2025-09-09/microvm-images"
	fake.on(route, func(n int) (int, string) {
		assert.Equal(t, 1, n)
		return http.StatusCreated,
			`{"imageArn":"arn:aws:lambda:us-east-1:123456789012:microvm-image/demo"}`
	})

	resp, err := http.Post(fake.server.URL+"/2025-09-09/microvm-images",
		"application/json", bytes.NewBufferString(`{"name":"demo"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	require.Len(t, fake.sent(route), 1)
	assert.JSONEq(t, `{"name":"demo"}`, string(fake.sent(route)[0]))
}

func TestNewClientUsesConfiguredEndpoint(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/managed-microvm-images"
	fake.on(route, func(n int) (int, string) {
		assert.Equal(t, 1, n)
		return http.StatusOK, `{"items":[]}`
	})

	client, err := newClient(context.Background(), fake.configuration())
	require.NoError(t, err)
	_, err = client.ListManagedMicrovmImages(context.Background(),
		&awslambdamicrovms.ListManagedMicrovmImagesInput{})
	require.NoError(t, err)

	require.Len(t, fake.sent(route), 1)
	assert.Empty(t, fake.sent(route)[0])
}
