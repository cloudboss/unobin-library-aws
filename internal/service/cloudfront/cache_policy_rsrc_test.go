package cloudfront

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type policyXMLItems struct {
	Quantity int      `xml:"Quantity"`
	Items    []string `xml:"Items>Name"`
}

type policyXMLCookies struct {
	Behavior string          `xml:"CookieBehavior"`
	Cookies  *policyXMLItems `xml:"Cookies"`
}

type policyXMLHeaders struct {
	Behavior string          `xml:"HeaderBehavior"`
	Headers  *policyXMLItems `xml:"Headers"`
}

type policyXMLQueries struct {
	Behavior     string          `xml:"QueryStringBehavior"`
	QueryStrings *policyXMLItems `xml:"QueryStrings"`
}

type policyXMLParameters struct {
	Gzip    *bool             `xml:"EnableAcceptEncodingGzip"`
	Brotli  *bool             `xml:"EnableAcceptEncodingBrotli"`
	Cookies *policyXMLCookies `xml:"CookiesConfig"`
	Headers *policyXMLHeaders `xml:"HeadersConfig"`
	Queries *policyXMLQueries `xml:"QueryStringsConfig"`
}

type policyXMLConfig struct {
	XMLName    xml.Name             `xml:"CachePolicyConfig"`
	Name       string               `xml:"Name"`
	Comment    *string              `xml:"Comment"`
	DefaultTTL *int64               `xml:"DefaultTTL"`
	MaxTTL     *int64               `xml:"MaxTTL"`
	MinTTL     *int64               `xml:"MinTTL"`
	Parameters *policyXMLParameters `xml:"ParametersInCacheKeyAndForwardedToOrigin"`
}

type policyCall struct {
	method  string
	path    string
	ifMatch string
	config  *policyXMLConfig
}

type policyResponse struct {
	status int
	etag   string
	body   string
}

const policyPath = "/2020-05-31/cache-policy"
const policyIDPath = policyPath + "/policy-id"

func policyReply(
	t *testing.T, id, etag string, config *policyXMLConfig,
) policyResponse {
	t.Helper()
	policy := struct {
		XMLName xml.Name         `xml:"CachePolicy"`
		ID      string           `xml:"Id,omitempty"`
		Config  *policyXMLConfig `xml:"CachePolicyConfig"`
	}{ID: id, Config: config}
	body, err := xml.Marshal(policy)
	require.NoError(t, err)
	return policyResponse{status: http.StatusOK, etag: etag, body: string(body)}
}

func policyError(status int, code string) policyResponse {
	return policyResponse{
		status: status,
		body: fmt.Sprintf("<ErrorResponse><Error><Code>%s</Code>"+
			"<Message>test error</Message></Error></ErrorResponse>", code),
	}
}

func policyTestEndpoint(
	t *testing.T, replies ...policyResponse,
) (*awsCfg, func() []policyCall) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_ACCOUNT_ID", "123456789012")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent/credentials")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	var mu sync.Mutex
	var calls []policyCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := policyCall{method: r.Method, path: r.URL.Path, ifMatch: r.Header.Get("If-Match")}
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			call.config = new(policyXMLConfig)
			if !assert.NoError(t, xml.NewDecoder(r.Body).Decode(call.config)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		mu.Lock()
		defer mu.Unlock()
		i := len(calls)
		calls = append(calls, call)
		if i >= len(replies) {
			t.Errorf("unexpected CloudFront call %+v", call)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reply := replies[i]
		w.Header().Set("Content-Type", "application/xml")
		if reply.etag != "" {
			w.Header().Set("ETag", reply.etag)
		}
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	}))
	t.Cleanup(server.Close)
	return &awsCfg{
			Region: aws.String("us-gov-west-1"), EndpointURL: aws.String(server.URL),
			MaxAttempts: aws.Int64(1),
		}, func() []policyCall {
			mu.Lock()
			defer mu.Unlock()
			return append([]policyCall(nil), calls...)
		}
}

func policyInput() CachePolicyResource {
	return CachePolicyResource{
		Name: "example", DefaultTTL: 86400, MaxTTL: 31536000,
		Parameters: CachePolicyParameters{
			CookiesConfig:      CachePolicyCookiesConfig{CookieBehavior: "none"},
			HeadersConfig:      CachePolicyHeadersConfig{HeaderBehavior: "none"},
			QueryStringsConfig: CachePolicyQueryStringsConfig{QueryStringBehavior: "none"},
		},
	}
}

func policyInputXML() *policyXMLConfig {
	return &policyXMLConfig{
		XMLName: xml.Name{
			Space: "http://cloudfront.amazonaws.com/doc/2020-05-31/", Local: "CachePolicyConfig",
		},
		Name: "example", DefaultTTL: aws.Int64(86400), MaxTTL: aws.Int64(31536000),
		MinTTL: aws.Int64(0),
		Parameters: &policyXMLParameters{
			Gzip: aws.Bool(false), Brotli: aws.Bool(false),
			Cookies: &policyXMLCookies{Behavior: "none"},
			Headers: &policyXMLHeaders{Behavior: "none"},
			Queries: &policyXMLQueries{Behavior: "none"},
		},
	}
}

func TestCachePolicyResourceSendsCompleteConfig(t *testing.T) {
	initial, wantCreate := policyInput(), policyInputXML()
	initial.Comment, wantCreate.Comment = aws.String("original"), aws.String("original")
	initial.DefaultTTL, wantCreate.DefaultTTL = 0, aws.Int64(0)
	initial.Parameters.EnableAcceptEncodingGzip = aws.Bool(true)
	initial.Parameters.EnableAcceptEncodingBrotli = aws.Bool(true)
	wantCreate.Parameters.Gzip = aws.Bool(true)
	wantCreate.Parameters.Brotli = aws.Bool(true)
	initial.Parameters.CookiesConfig = CachePolicyCookiesConfig{
		CookieBehavior: "whitelist",
		Cookies:        &CachePolicyItems{Items: &[]string{"z", "", "a", "a", " A "}},
	}
	initial.Parameters.HeadersConfig = CachePolicyHeadersConfig{
		HeaderBehavior: "whitelist",
		Headers:        &CachePolicyItems{Items: &[]string{"Z", "z", "", "z"}},
	}
	initial.Parameters.QueryStringsConfig = CachePolicyQueryStringsConfig{
		QueryStringBehavior: "allExcept",
		QueryStrings:        &CachePolicyItems{Items: &[]string{"z", "z", "a", ""}},
	}
	wantCreate.Parameters.Cookies = &policyXMLCookies{
		Behavior: "whitelist", Cookies: &policyXMLItems{3, []string{" A ", "a", "z"}},
	}
	wantCreate.Parameters.Headers = &policyXMLHeaders{
		Behavior: "whitelist", Headers: &policyXMLItems{2, []string{"Z", "z"}},
	}
	wantCreate.Parameters.Queries = &policyXMLQueries{
		Behavior: "allExcept", QueryStrings: &policyXMLItems{2, []string{"a", "z"}},
	}

	desired, wantUpdate := policyInput(), policyInputXML()
	desired.Name, wantUpdate.Name = "changed", "changed"
	desired.Comment = aws.String("")
	desired.DefaultTTL, wantUpdate.DefaultTTL = 0, aws.Int64(0)
	desired.MaxTTL, wantUpdate.MaxTTL = 0, aws.Int64(0)
	desired.Parameters.EnableAcceptEncodingGzip = aws.Bool(false)
	desired.Parameters.CookiesConfig.Cookies = &CachePolicyItems{}
	desired.Parameters.HeadersConfig.Headers = &CachePolicyItems{Items: &[]string{}}
	desired.Parameters.QueryStringsConfig.QueryStrings = &CachePolicyItems{Items: &[]string{""}}

	observedCreate := *wantCreate
	observedParameters := *wantCreate.Parameters
	observedCreate.Parameters = &observedParameters
	observedParameters.Cookies = &policyXMLCookies{
		Behavior: "whitelist", Cookies: &policyXMLItems{6, []string{"z", "a", "", "a", " A ", "z"}},
	}
	observedParameters.Headers = &policyXMLHeaders{
		Behavior: "whitelist", Headers: &policyXMLItems{4, []string{"z", "", "Z", "z"}},
	}
	observedParameters.Queries = &policyXMLQueries{
		Behavior: "allExcept", QueryStrings: &policyXMLItems{4, []string{"z", "z", "", "a"}},
	}
	observedUpdate := *wantUpdate
	observedUpdatedParameters := *wantUpdate.Parameters
	observedUpdate.Parameters = &observedUpdatedParameters
	observedUpdatedParameters.Headers = nil
	observedUpdatedParameters.Cookies = &policyXMLCookies{
		Behavior: "none", Cookies: &policyXMLItems{0, nil},
	}

	cfg, calls := policyTestEndpoint(t,
		policyReply(t, "policy-id", "initial", &observedCreate),
		policyResponse{status: http.StatusOK},
		policyReply(t, "policy-id", "changed", &observedUpdate),
		policyResponse{status: http.StatusNoContent},
	)
	created, err := initial.Create(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, &CachePolicyResourceOutput{
		Id: "policy-id", Arn: "arn:aws-us-gov:cloudfront::123456789012:cache-policy/policy-id",
		ETag: "initial", Name: "example", Comment: "original", MaxTTL: 31536000,
		Parameters: &CachePolicyDataSourceParameters{
			EnableAcceptEncodingGzip: true, EnableAcceptEncodingBrotli: true,
			CookiesConfig: &CachePolicyDataSourceCookiesConfig{
				CookieBehavior: "whitelist",
				Cookies: &CachePolicyDataSourceCookieNames{
					Items: []string{" A ", "a", "z"},
				},
			},
			HeadersConfig: &CachePolicyDataSourceHeadersConfig{
				HeaderBehavior: "whitelist",
				Headers:        &CachePolicyDataSourceHeaders{Items: []string{"Z", "z"}},
			},
			QueryStringsConfig: &CachePolicyDataSourceQueryStringsConfig{
				QueryStringBehavior: "allExcept",
				QueryStrings:        &CachePolicyDataSourceQueryStringNames{Items: []string{"a", "z"}},
			},
		},
	}, created)
	updated, err := desired.Update(t.Context(), cfg, cachePolicyPrior{
		Outputs: created, Observed: &CachePolicyResourceOutput{ETag: "observed"},
	})
	require.NoError(t, err)
	assert.Equal(t, &CachePolicyResourceOutput{
		Id: created.Id, Arn: created.Arn, ETag: "changed", Name: "changed",
		Parameters: &CachePolicyDataSourceParameters{
			CookiesConfig:      &CachePolicyDataSourceCookiesConfig{CookieBehavior: "none"},
			QueryStringsConfig: &CachePolicyDataSourceQueryStringsConfig{QueryStringBehavior: "none"},
		},
	}, updated)
	require.NoError(t, desired.Delete(t.Context(), cfg, cachePolicyPrior{
		Outputs: updated, Observed: &CachePolicyResourceOutput{ETag: "latest"},
	}))
	assert.Equal(t, []policyCall{
		{method: http.MethodPost, path: policyPath, config: wantCreate},
		{method: http.MethodPut, path: policyIDPath, ifMatch: "observed", config: wantUpdate},
		{method: http.MethodGet, path: policyIDPath},
		{method: http.MethodDelete, path: policyIDPath, ifMatch: "latest"},
	}, calls())
}

func TestCachePolicyResourceCreateResponse(t *testing.T) {
	invalid := policyInputXML()
	invalid.DefaultTTL = aws.Int64(31536001)
	for _, tt := range []struct {
		name    string
		calls   []policyCall
		replies []policyResponse
		fail    string
	}{
		{
			name: "fallback for missing config",
			calls: []policyCall{
				{method: http.MethodPost, path: policyPath, config: policyInputXML()},
				{method: http.MethodGet, path: policyIDPath},
			},
			replies: []policyResponse{
				policyReply(t, "policy-id", "first", nil),
				policyReply(t, "policy-id", "recovered", &policyXMLConfig{Name: "observed"}),
			},
		},
		{
			name: "fallback for missing ETag",
			calls: []policyCall{
				{method: http.MethodPost, path: policyPath, config: policyInputXML()},
				{method: http.MethodGet, path: policyIDPath},
			},
			replies: []policyResponse{
				policyReply(t, "policy-id", "", policyInputXML()),
				policyReply(t, "policy-id", "", policyInputXML()),
			},
			fail: "create cache policy policy-id observation: get cache policy policy-id: missing ETag",
		},
		{
			name: "missing ID",
			calls: []policyCall{
				{method: http.MethodPost, path: policyPath, config: policyInputXML()},
			},
			replies: []policyResponse{policyReply(t, "", "first", policyInputXML())},
			fail:    "create cache policy: missing policy ID",
		},
		{
			name: "service rejection",
			calls: []policyCall{
				{method: http.MethodPost, path: policyPath, config: invalid},
			},
			replies: []policyResponse{policyError(http.StatusBadRequest, "InvalidArgument")},
			fail:    "create cache policy:",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, calls := policyTestEndpoint(t, tt.replies...)
			input := policyInput()
			if tt.name == "service rejection" {
				input.DefaultTTL = 31536001
			}
			out, err := input.Create(t.Context(), cfg)
			if tt.fail != "" {
				require.ErrorContains(t, err, tt.fail)
				if tt.name == "service rejection" {
					var invalid *cloudfronttypes.InvalidArgument
					require.ErrorAs(t, err, &invalid)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, &CachePolicyResourceOutput{
					Id: "policy-id", Name: "observed", ETag: "recovered",
					Arn: "arn:aws-us-gov:cloudfront::123456789012:cache-policy/policy-id",
				}, out)
			}
			assert.Equal(t, tt.calls, calls())
		})
	}
}

func TestCachePolicyResourcePrior(t *testing.T) {
	for _, tt := range []struct {
		name     string
		action   string
		observed *CachePolicyResourceOutput
		replies  []policyResponse
		calls    []policyCall
		fail     string
		code     string
		absent   bool
	}{
		{
			name: "typed missing read", action: http.MethodGet, absent: true,
			replies: []policyResponse{policyError(http.StatusNotFound, "NoSuchCachePolicy")},
			calls:   []policyCall{{method: http.MethodGet, path: policyIDPath}},
		},
		{
			name: "empty read", action: http.MethodGet, absent: true,
			replies: []policyResponse{{status: http.StatusOK}},
			calls:   []policyCall{{method: http.MethodGet, path: policyIDPath}},
		},
		{
			name: "config absent", action: http.MethodGet, absent: true,
			replies: []policyResponse{policyReply(t, "policy-id", "tag", nil)},
			calls:   []policyCall{{method: http.MethodGet, path: policyIDPath}},
		},
		{
			name: "conflicting read ID", action: http.MethodGet,
			replies: []policyResponse{policyReply(t, "another", "tag", policyInputXML())},
			calls:   []policyCall{{method: http.MethodGet, path: policyIDPath}},
			fail:    `get cache policy policy-id: returned policy ID "another"`,
		},
		{
			name: "missing read ETag", action: http.MethodGet,
			replies: []policyResponse{policyReply(t, "policy-id", "", policyInputXML())},
			calls:   []policyCall{{method: http.MethodGet, path: policyIDPath}},
			fail:    "get cache policy policy-id: missing ETag",
		},
		{
			name: "access denied", action: http.MethodGet,
			replies: []policyResponse{policyError(http.StatusForbidden, "AccessDenied")},
			calls:   []policyCall{{method: http.MethodGet, path: policyIDPath}},
			fail:    "get cache policy policy-id:", code: "AccessDenied",
		},
		{
			name: "update uses persisted ETag and preserves conflict", action: http.MethodPut,
			replies: []policyResponse{policyError(http.StatusPreconditionFailed, "PreconditionFailed")},
			calls: []policyCall{
				{
					method: http.MethodPut, path: policyIDPath, ifMatch: "stored",
					config: policyInputXML(),
				},
			},
			fail: "update cache policy policy-id:", code: "PreconditionFailed",
		},
		{
			name: "update observation fails", action: http.MethodPut,
			replies: []policyResponse{
				{status: http.StatusOK}, policyError(http.StatusNotFound, "NoSuchCachePolicy"),
			},
			calls: []policyCall{
				{
					method: http.MethodPut, path: policyIDPath, ifMatch: "stored",
					config: policyInputXML(),
				},
				{method: http.MethodGet, path: policyIDPath},
			},
			fail: "update cache policy policy-id observation:", absent: true,
		},
		{
			name: "update refuses missing observed ETag", action: http.MethodPut,
			observed: &CachePolicyResourceOutput{}, fail: "update cache policy policy-id: missing ETag",
		},
		{
			name: "delete refuses missing observed ETag", action: http.MethodDelete,
			observed: &CachePolicyResourceOutput{}, fail: "delete cache policy policy-id: missing ETag",
		},
		{
			name: "delete missing is successful", action: http.MethodDelete,
			replies: []policyResponse{policyError(http.StatusNotFound, "NoSuchCachePolicy")},
			calls:   []policyCall{{method: http.MethodDelete, path: policyIDPath, ifMatch: "stored"}},
		},
		{
			name: "delete in use", action: http.MethodDelete,
			replies: []policyResponse{policyError(http.StatusConflict, "CachePolicyInUse")},
			calls:   []policyCall{{method: http.MethodDelete, path: policyIDPath, ifMatch: "stored"}},
			fail:    "delete cache policy policy-id:", code: "CachePolicyInUse",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, calls := policyTestEndpoint(t, tt.replies...)
			input := policyInput()
			prior := cachePolicyPrior{
				Outputs:  &CachePolicyResourceOutput{Id: "policy-id", ETag: "stored"},
				Observed: tt.observed,
			}
			var err error
			switch tt.action {
			case http.MethodGet:
				_, err = input.Read(t.Context(), cfg, prior)
			case http.MethodPut:
				_, err = input.Update(t.Context(), cfg, prior)
			case http.MethodDelete:
				err = input.Delete(t.Context(), cfg, prior)
			}
			if tt.fail != "" {
				require.ErrorContains(t, err, tt.fail)
			}
			if tt.absent {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.NotErrorIs(t, err, runtime.ErrNotFound)
			}
			if !tt.absent && tt.fail == "" {
				require.NoError(t, err)
			}
			if tt.code != "" {
				var serviceError smithy.APIError
				require.ErrorAs(t, err, &serviceError)
				assert.Equal(t, tt.code, serviceError.ErrorCode())
			}
			assert.Equal(t, tt.calls, calls())
		})
	}
}

func TestCachePolicyResourceAccountResolution(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) {
			var replies []policyResponse
			if allowed {
				replies = []policyResponse{policyReply(t, "policy-id", "v1", policyInputXML())}
			}
			cfg, calls := policyTestEndpoint(t, replies...)
			t.Setenv("AWS_ACCOUNT_ID", "")
			stsCalls := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !assert.NoError(t, r.ParseForm()) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				stsCalls <- r.Form.Get("Action")
				if !allowed {
					w.WriteHeader(http.StatusForbidden)
					_, _ = fmt.Fprint(w, "<ErrorResponse><Error><Code>AccessDenied</Code>"+
						"</Error></ErrorResponse>")
					return
				}
				_, _ = fmt.Fprint(w, "<GetCallerIdentityResponse><GetCallerIdentityResult>"+
					"<Account>987654321123</Account></GetCallerIdentityResult>"+
					"</GetCallerIdentityResponse>")
			}))
			t.Cleanup(server.Close)
			cfg.Endpoints = &awscfg.Endpoints{STS: aws.String(server.URL)}
			input := policyInput()
			out, err := input.Create(t.Context(), cfg)
			require.Len(t, stsCalls, 1)
			assert.Equal(t, "GetCallerIdentity", <-stsCalls)
			if allowed {
				require.NoError(t, err)
				assert.Equal(t, "arn:aws-us-gov:cloudfront::987654321123:cache-policy/policy-id", out.Arn)
				assert.Equal(t, []policyCall{
					{method: http.MethodPost, path: policyPath, config: policyInputXML()},
				}, calls())
			} else {
				require.ErrorContains(t, err, "create cache policy: get caller identity")
				assert.Empty(t, calls())
			}
		})
	}
}

func TestCachePolicyResourceCommentPresence(t *testing.T) {
	wantCreate := policyInputXML()
	wantUpdate := policyInputXML()
	wantUpdate.Comment = aws.String("new comment")
	cfg, calls := policyTestEndpoint(t,
		policyReply(t, "policy-id", "created", wantCreate),
		policyResponse{status: http.StatusOK},
		policyReply(t, "policy-id", "updated", wantUpdate),
	)
	input := policyInput()
	input.Comment = aws.String("")
	created, err := input.Create(t.Context(), cfg)
	require.NoError(t, err)
	require.Empty(t, created.Comment)
	input.Comment = aws.String("new comment")
	updated, err := input.Update(t.Context(), cfg, cachePolicyPrior{Outputs: created})
	require.NoError(t, err)
	require.Equal(t, "new comment", updated.Comment)
	assert.Equal(t, []policyCall{
		{method: http.MethodPost, path: policyPath, config: wantCreate},
		{method: http.MethodPut, path: policyIDPath, ifMatch: "created", config: wantUpdate},
		{method: http.MethodGet, path: policyIDPath},
	}, calls())
}
