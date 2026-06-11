package lambda

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// fakeLambda is an in-process stand-in for the Lambda REST API. Each test
// registers a response function per route, the method and path of one
// operation; the function receives the 1-based call number for that route and
// returns the HTTP status and JSON body to send. Every request body is
// recorded by route so a test can assert on what was sent.
type fakeLambda struct {
	t        *testing.T
	mu       sync.Mutex
	calls    map[string]int
	handlers map[string]func(n int) (int, string)
	bodies   map[string][][]byte
	server   *httptest.Server
}

func newFakeLambda(t *testing.T) *fakeLambda {
	f := &fakeLambda{
		t:        t,
		calls:    map[string]int{},
		handlers: map[string]func(int) (int, string){},
		bodies:   map[string][][]byte{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// on registers the response function for one route, the method and path of an
// operation, such as "PUT /2015-03-31/functions/fn/configuration".
func (f *fakeLambda) on(route string, h func(n int) (int, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[route] = h
}

// sent returns a copy of every request body recorded for the route.
func (f *fakeLambda) sent(route string) [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.bodies[route]...)
}

func (f *fakeLambda) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("fake lambda: read body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	route := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.calls[route]++
	f.bodies[route] = append(f.bodies[route], body)
	h, ok := f.handlers[route]
	n := f.calls[route]
	f.mu.Unlock()
	if !ok {
		f.t.Errorf("fake lambda: no handler for route %s", route)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	status, resp := h(n)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, resp)
}

// configuration returns a module configuration that points the SDK at the
// fake server with credentials from the environment, and isolates the test
// from the developer's shared AWS config files and the instance metadata
// service.
func (f *fakeLambda) configuration() *awscfg.Configuration {
	dir := f.t.TempDir()
	f.t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))
	f.t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	f.t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	f.t.Setenv("AWS_PROFILE", "")
	f.t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFAKEFAKEFAKEFAKE")
	f.t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	return &awscfg.Configuration{
		Region:      &cfg.String{Value: "us-east-1"},
		EndpointURL: &cfg.String{Value: f.server.URL},
	}
}
