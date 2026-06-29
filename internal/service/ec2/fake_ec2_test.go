package ec2

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
)

// fakeEC2 is an in-process stand-in for the EC2 Query API. Each test
// registers a response function per action; the function receives the
// 1-based call number for that action and the decoded request form, and
// returns the HTTP status and XML body to send. Every request form is
// recorded so a test can assert on what was sent.
type fakeEC2 struct {
	t        *testing.T
	mu       sync.Mutex
	calls    map[string]int
	handlers map[string]func(n int, form url.Values) (int, string)
	requests []url.Values
	server   *httptest.Server
}

func newFakeEC2(t *testing.T) *fakeEC2 {
	f := &fakeEC2{
		t:        t,
		calls:    map[string]int{},
		handlers: map[string]func(int, url.Values) (int, string){},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// on registers the response function for one EC2 action.
func (f *fakeEC2) on(action string, h func(n int, form url.Values) (int, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[action] = h
}

// sent returns a copy of every request form recorded for the action.
func (f *fakeEC2) sent(action string) []url.Values {
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

func (f *fakeEC2) serve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("fake ec2: parse form: %v", err)
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
		f.t.Errorf("fake ec2: no handler for action %s", action)
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, ec2ErrorXML("InvalidAction", "no handler registered for "+action))
		return
	}
	status, body := h(n, r.PostForm)
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

// configuration returns a module configuration that points the SDK at the
// fake server with credentials from the environment, and isolates the test
// from the developer's shared AWS config files and the instance metadata
// service.
func (f *fakeEC2) configuration() *awscfg.Configuration {
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

// ec2ErrorXML builds the EC2 Query error body for one error code, laid out
// the way GetErrorResponseComponents in the SDK's ec2query protocol package
// decodes it.
func ec2ErrorXML(code, message string) string {
	return fmt.Sprintf(`<Response>
  <Errors>
    <Error>
      <Code>%s</Code>
      <Message>%s</Message>
    </Error>
  </Errors>
  <RequestID>req-00000000-0000-0000-0000-000000000000</RequestID>
</Response>`, code, message)
}
