package iam

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"

	"github.com/cloudboss/unobin-library-aws/internal/config"
)

// fakeIAM is an in-process stand-in for the IAM Query API, the same design as
// the EC2 fake. Each test registers a response function per action; the
// function receives the 1-based call number for that action and the decoded
// request form, and returns the HTTP status and XML body to send. Every
// request form is recorded so a test can assert on what was sent.
type fakeIAM struct {
	t        *testing.T
	mu       sync.Mutex
	calls    map[string]int
	handlers map[string]func(n int, form url.Values) (int, string)
	requests []url.Values
	server   *httptest.Server
}

func newFakeIAM(t *testing.T) *fakeIAM {
	f := &fakeIAM{
		t:        t,
		calls:    map[string]int{},
		handlers: map[string]func(int, url.Values) (int, string){},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// on registers the response function for one IAM action.
func (f *fakeIAM) on(action string, h func(n int, form url.Values) (int, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[action] = h
}

// sent returns a copy of every request form recorded for the action.
func (f *fakeIAM) sent(action string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for _, form := range f.requests {
		if form.Get("Action") == action {
			out = append(out, form)
		}
	}
	return out
}

func (f *fakeIAM) serve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("fake iam: parse form: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	action := r.PostForm.Get("Action")
	f.mu.Lock()
	f.calls[action]++
	n := f.calls[action]
	f.requests = append(f.requests, r.PostForm)
	h, ok := f.handlers[action]
	f.mu.Unlock()
	if !ok {
		f.t.Errorf("fake iam: no handler for action %s", action)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	status, body := h(n, r.PostForm)
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

// configuration returns a module configuration that points the SDK at the
// fake server with static credentials, and isolates the test from the
// developer's shared AWS config files and the instance metadata service.
func (f *fakeIAM) configuration() *config.Configuration {
	dir := f.t.TempDir()
	f.t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))
	f.t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	f.t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	f.t.Setenv("AWS_PROFILE", "")
	return &config.Configuration{
		Region:          &cfg.String{Value: "us-east-1"},
		AccessKeyId:     &cfg.String{Value: "AKIAFAKEFAKEFAKEFAKE"},
		SecretAccessKey: &cfg.String{Value: "fake-secret-key"},
		EndpointURL:     &cfg.String{Value: f.server.URL},
	}
}
