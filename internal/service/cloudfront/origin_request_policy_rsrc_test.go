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

const originRequestPolicyPath = "/2020-05-31/origin-request-policy"
const originRequestPolicyIDPath = originRequestPolicyPath + "/policy-id"

type originRequestXMLConfig struct {
	XMLName xml.Name          `xml:"OriginRequestPolicyConfig"`
	Name    string            `xml:"Name"`
	Comment *string           `xml:"Comment"`
	Cookies *policyXMLCookies `xml:"CookiesConfig"`
	Headers *policyXMLHeaders `xml:"HeadersConfig"`
	Queries *policyXMLQueries `xml:"QueryStringsConfig"`
}

type originRequestCall struct {
	Method  string
	Path    string
	IfMatch string
	Config  *originRequestXMLConfig
}

func originRequestReply(
	t *testing.T, id, etag string, config *originRequestXMLConfig,
) policyResponse {
	t.Helper()
	policy := struct {
		XMLName xml.Name                `xml:"OriginRequestPolicy"`
		ID      string                  `xml:"Id,omitempty"`
		Config  *originRequestXMLConfig `xml:"OriginRequestPolicyConfig"`
	}{ID: id, Config: config}
	body, err := xml.Marshal(policy)
	require.NoError(t, err)
	return policyResponse{status: http.StatusOK, etag: etag, body: string(body)}
}

func originRequestEndpoint(t *testing.T, replies ...policyResponse) (
	*awsCfg, func() []originRequestCall,
) {
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
	var calls []originRequestCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := originRequestCall{
			Method: r.Method, Path: r.URL.Path, IfMatch: r.Header.Get("If-Match"),
		}
		assert.Contains(t, r.Header.Get("Authorization"), "Credential=test/")
		assert.Contains(t, r.Header.Get("Authorization"), "/cloudfront/aws4_request")
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			call.Config = new(originRequestXMLConfig)
			if !assert.NoError(t, xml.NewDecoder(r.Body).Decode(call.Config)) {
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
		w.Header().Set("ETag", reply.etag)
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	}))
	t.Cleanup(server.Close)
	cfg := &awsCfg{
		Region: aws.String("us-east-1"), EndpointURL: aws.String(server.URL),
		MaxAttempts: aws.Int64(1),
	}
	return cfg, func() []originRequestCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]originRequestCall(nil), calls...)
	}
}

func originRequestInput() OriginRequestPolicyResource {
	return OriginRequestPolicyResource{
		Name:               "desired",
		CookiesConfig:      OriginRequestPolicyCookiesConfig{CookieBehavior: "none"},
		HeadersConfig:      OriginRequestPolicyHeadersConfig{HeaderBehavior: "none"},
		QueryStringsConfig: OriginRequestPolicyQueryStringsConfig{QueryStringBehavior: "none"},
	}
}

func originRequestInputXML() *originRequestXMLConfig {
	return &originRequestXMLConfig{
		XMLName: xml.Name{
			Space: "http://cloudfront.amazonaws.com/doc/2020-05-31/",
			Local: "OriginRequestPolicyConfig",
		},
		Name:    "desired",
		Cookies: &policyXMLCookies{Behavior: "none"},
		Headers: &policyXMLHeaders{Behavior: "none"},
		Queries: &policyXMLQueries{Behavior: "none"},
	}
}

func TestOriginRequestPolicyCreateAndProject(t *testing.T) {
	resource := originRequestInput()
	resource.Comment = aws.String("new comment")
	items := []string{"Z", " b ", "a", "Z", "", "A"}
	wrappers := &OriginRequestPolicyItems{Items: &items}
	resource.CookiesConfig = OriginRequestPolicyCookiesConfig{"allExcept", wrappers}
	resource.HeadersConfig = OriginRequestPolicyHeadersConfig{"whitelist", wrappers}
	resource.QueryStringsConfig = OriginRequestPolicyQueryStringsConfig{"whitelist", wrappers}
	wantRequest := originRequestInputXML()
	wantRequest.Comment = resource.Comment
	expectedItems := &policyXMLItems{Quantity: 4, Items: []string{" b ", "A", "Z", "a"}}
	wantRequest.Cookies = &policyXMLCookies{"allExcept", expectedItems}
	wantRequest.Headers = &policyXMLHeaders{"whitelist", expectedItems}
	wantRequest.Queries = &policyXMLQueries{"whitelist", expectedItems}

	observed := &originRequestXMLConfig{
		Name: "observed",
		Cookies: &policyXMLCookies{
			Behavior: "whitelist", Cookies: &policyXMLItems{99, []string{"z", " A ", "z", "", "a"}},
		},
		Headers: &policyXMLHeaders{Behavior: "none", Headers: &policyXMLItems{}},
	}
	reply := originRequestReply(t, "policy-id", "created", observed)
	cfg, calls := originRequestEndpoint(t, reply, reply)
	cfg.Endpoints = &awscfg.Endpoints{STS: cfg.EndpointURL}
	out, err := resource.Create(t.Context(), cfg)
	require.NoError(t, err)
	wantOutput := &OriginRequestPolicyResourceOutput{
		Id: "policy-id", Name: "observed", ETag: "created",
		Arn: "arn:aws:cloudfront::123456789012:origin-request-policy/policy-id",
		CookiesConfig: &OriginRequestPolicyDataSourceCookiesConfig{
			CookieBehavior: "whitelist",
			Cookies: &OriginRequestPolicyDataSourceCookieNames{
				Items: []string{"", " A ", "a", "z"},
			},
		},
		HeadersConfig: &OriginRequestPolicyDataSourceHeadersConfig{HeaderBehavior: "none"},
	}
	assert.Equal(t, wantOutput, out)
	post := originRequestCall{
		Method: http.MethodPost, Path: originRequestPolicyPath, Config: wantRequest,
	}
	require.Equal(t, []originRequestCall{post}, calls())
	assert.Equal(t, []string{"Z", " b ", "a", "Z", "", "A"}, items)

	out, err = resource.Read(t.Context(), cfg, originRequestPolicyPrior{Outputs: out})
	require.NoError(t, err)
	assert.Equal(t, wantOutput, out)
	assert.Equal(t, []originRequestCall{
		post, {Method: http.MethodGet, Path: originRequestPolicyIDPath},
	}, calls())
}

func TestOriginRequestPolicyOmitsEmptyRequestMembers(t *testing.T) {
	for _, tt := range []struct {
		name    string
		comment *string
		items   *OriginRequestPolicyItems
	}{
		{name: "null"},
		{name: "empty comment and wrapper", comment: aws.String(""), items: &OriginRequestPolicyItems{}},
		{name: "empty list", items: &OriginRequestPolicyItems{Items: &[]string{}}},
		{name: "empty strings", items: &OriginRequestPolicyItems{Items: &[]string{"", ""}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resource := originRequestInput()
			resource.Comment = tt.comment
			resource.CookiesConfig.Cookies = tt.items
			resource.HeadersConfig.Headers = tt.items
			resource.QueryStringsConfig.QueryStrings = tt.items
			cfg, calls := originRequestEndpoint(t,
				originRequestReply(t, "policy-id", "created", originRequestInputXML()))
			_, err := resource.Create(t.Context(), cfg)
			require.NoError(t, err)
			assert.Equal(t, []originRequestCall{{
				Method: http.MethodPost, Path: originRequestPolicyPath,
				Config: originRequestInputXML(),
			}}, calls())
		})
	}
}

func TestOriginRequestPolicyCreateObservation(t *testing.T) {
	complete := originRequestReply(t, "policy-id", "latest", originRequestInputXML())
	post := originRequestCall{
		Method: http.MethodPost, Path: originRequestPolicyPath, Config: originRequestInputXML(),
	}
	get := originRequestCall{Method: http.MethodGet, Path: originRequestPolicyIDPath}
	for _, tt := range []struct {
		name    string
		replies []policyResponse
		calls   []originRequestCall
		wantErr string
	}{
		{
			name: "missing config",
			replies: []policyResponse{
				originRequestReply(t, "policy-id", "created", nil), complete,
			},
			calls: []originRequestCall{post, get},
		},
		{
			name: "missing ETag",
			replies: []policyResponse{
				originRequestReply(t, "policy-id", "", originRequestInputXML()), complete,
			},
			calls: []originRequestCall{post, get},
		},
		{
			name:    "missing create ID",
			replies: []policyResponse{originRequestReply(t, "", "created", originRequestInputXML())},
			calls:   []originRequestCall{post},
			wantErr: "create origin request policy: missing policy ID",
		},
		{
			name: "missing fallback",
			replies: []policyResponse{
				originRequestReply(t, "policy-id", "created", nil),
				policyError(http.StatusNotFound, "NoSuchOriginRequestPolicy"),
			},
			calls:   []originRequestCall{post, get},
			wantErr: "create origin request policy policy-id observation:",
		},
		{
			name: "duplicate name",
			replies: []policyResponse{
				policyError(http.StatusConflict, "OriginRequestPolicyAlreadyExists"),
			},
			calls:   []originRequestCall{post},
			wantErr: "create origin request policy:",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, calls := originRequestEndpoint(t, tt.replies...)
			resource := originRequestInput()
			out, err := resource.Create(t.Context(), cfg)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, out)
				require.NotErrorIs(t, err, runtime.ErrNotFound)
				if tt.name == "duplicate name" {
					var duplicate *cloudfronttypes.OriginRequestPolicyAlreadyExists
					require.ErrorAs(t, err, &duplicate)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, "policy-id", out.Id)
				assert.Equal(t, "latest", out.ETag)
			}
			assert.Equal(t, tt.calls, calls())
		})
	}
}

func TestOriginRequestPolicyReadIdentityAndErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		reply   policyResponse
		missing bool
		wantErr string
	}{
		{"omitted ID", originRequestReply(t, "", "observed", originRequestInputXML()), false, ""},
		{"conflicting ID", originRequestReply(t, "other", "observed", originRequestInputXML()),
			false, `returned policy ID "other"`},
		{"missing ETag", originRequestReply(t, "policy-id", "", originRequestInputXML()),
			false, "missing ETag"},
		{"typed missing", policyError(404, "NoSuchOriginRequestPolicy"), true, ""},
		{"empty response", policyResponse{status: http.StatusOK}, true, ""},
		{"missing config", originRequestReply(t, "policy-id", "observed", nil), true, ""},
		{"unrelated 404", policyError(404, "NoSuchDistribution"), false, "NoSuchDistribution"},
		{"access error", policyError(403, "AccessDenied"), false, "AccessDenied"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, calls := originRequestEndpoint(t, tt.reply)
			resource := originRequestInput()
			prior := originRequestPolicyPrior{
				Outputs: &OriginRequestPolicyResourceOutput{Id: "policy-id", ETag: "old"},
			}
			out, err := resource.Read(t.Context(), cfg, prior)
			switch {
			case tt.missing:
				require.ErrorIs(t, err, runtime.ErrNotFound)
			case tt.wantErr != "":
				require.ErrorContains(t, err, tt.wantErr)
				require.NotErrorIs(t, err, runtime.ErrNotFound)
			default:
				require.NoError(t, err)
				assert.Equal(t, "policy-id", out.Id)
				assert.Equal(t, "observed", out.ETag)
			}
			assert.Equal(t, []originRequestCall{
				{Method: http.MethodGet, Path: originRequestPolicyIDPath},
			}, calls())
		})
	}
}

func TestOriginRequestPolicyUpdateAndDelete(t *testing.T) {
	for _, tt := range []struct {
		name     string
		observed *OriginRequestPolicyResourceOutput
		token    string
	}{
		{name: "persisted", token: "persisted"},
		{name: "observed", observed: &OriginRequestPolicyResourceOutput{ETag: "latest"},
			token: "latest"},
		{name: "empty observation", observed: &OriginRequestPolicyResourceOutput{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resource := originRequestInput()
			resource.Name = "renamed"
			resource.Comment = aws.String("")
			resource.HeadersConfig.HeaderBehavior = "allViewer"
			want := originRequestInputXML()
			want.Name = "renamed"
			want.Headers.Behavior = "allViewer"

			prior := originRequestPolicyPrior{
				Outputs: &OriginRequestPolicyResourceOutput{
					Id: "policy-id", ETag: "persisted", Comment: "old comment",
				},
				Observed: tt.observed,
			}
			cfg, calls := originRequestEndpoint(t,
				originRequestReply(t, "policy-id", "unused", nil),
				originRequestReply(t, "policy-id", "updated", want),
				policyError(http.StatusNotFound, "NoSuchOriginRequestPolicy"),
			)
			out, err := resource.Update(t.Context(), cfg, prior)
			if tt.token == "" {
				require.ErrorContains(t, err, "update origin request policy policy-id: missing ETag")
				err = resource.Delete(t.Context(), cfg, prior)
				require.ErrorContains(t, err, "delete origin request policy policy-id: missing ETag")
				require.Empty(t, calls())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "updated", out.ETag)
			assert.Equal(t, "policy-id", out.Id)
			assert.Equal(t, "renamed", out.Name)
			assert.Empty(t, out.Comment)
			assert.Equal(t, "allViewer", out.HeadersConfig.HeaderBehavior)
			require.NoError(t, resource.Delete(t.Context(), cfg, prior))
			assert.Equal(t, []originRequestCall{
				{Method: http.MethodPut, Path: originRequestPolicyIDPath, IfMatch: tt.token,
					Config: want},
				{Method: http.MethodGet, Path: originRequestPolicyIDPath},
				{Method: http.MethodDelete, Path: originRequestPolicyIDPath, IfMatch: tt.token},
			}, calls())
		})
	}
}

func TestOriginRequestPolicyMutationErrors(t *testing.T) {
	prior := originRequestPolicyPrior{
		Outputs: &OriginRequestPolicyResourceOutput{Id: "policy-id", ETag: "before"},
	}
	put := originRequestCall{
		Method: http.MethodPut, Path: originRequestPolicyIDPath, IfMatch: "before",
		Config: originRequestInputXML(),
	}
	for _, tt := range []struct {
		name    string
		code    string
		replies []policyResponse
		calls   []originRequestCall
		remove  bool
	}{
		{
			name: "stale ETag", code: "PreconditionFailed",
			replies: []policyResponse{policyError(412, "PreconditionFailed")},
			calls:   []originRequestCall{put},
		},
		{
			name: "post-update observation", code: "AccessDenied",
			replies: []policyResponse{
				originRequestReply(t, "policy-id", "ignored", nil), policyError(403, "AccessDenied"),
			},
			calls: []originRequestCall{
				put, {Method: http.MethodGet, Path: originRequestPolicyIDPath},
			},
		},
		{
			name: "stale delete ETag", code: "PreconditionFailed", remove: true,
			replies: []policyResponse{policyError(412, "PreconditionFailed")},
			calls: []originRequestCall{
				{Method: http.MethodDelete, Path: originRequestPolicyIDPath, IfMatch: "before"},
			},
		},
		{
			name: "in-use delete", code: "OriginRequestPolicyInUse", remove: true,
			replies: []policyResponse{policyError(409, "OriginRequestPolicyInUse")},
			calls: []originRequestCall{
				{Method: http.MethodDelete, Path: originRequestPolicyIDPath, IfMatch: "before"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, calls := originRequestEndpoint(t, tt.replies...)
			resource := originRequestInput()
			var err error
			if tt.remove {
				err = resource.Delete(t.Context(), cfg, prior)
			} else {
				_, err = resource.Update(t.Context(), cfg, prior)
			}
			var apiErr smithy.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tt.code, apiErr.ErrorCode())
			assert.Equal(t, tt.calls, calls())
		})
	}
}

func TestOriginRequestPolicyAccountFallback(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(fmt.Sprintf("account found=%v", allowed), func(t *testing.T) {
			cfg, cloudCalls := originRequestEndpoint(t,
				originRequestReply(t, "policy-id", "created", originRequestInputXML()))
			cfg.Region = aws.String("us-gov-west-1")
			t.Setenv("AWS_ACCOUNT_ID", "")
			stsCalls := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				stsCalls <- r.Method + " " + r.URL.Path + " " + r.FormValue("Action")
				assert.Contains(t, r.Header.Get("Authorization"), "/us-gov-west-1/sts/aws4_request")
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
			cfg.Endpoints = &awscfg.Endpoints{STS: aws.String(server.URL + "/custom")}
			resource := originRequestInput()
			out, err := resource.Create(t.Context(), cfg)
			select {
			case call := <-stsCalls:
				assert.Equal(t, "POST /custom GetCallerIdentity", call)
			default:
				t.Fatal("STS endpoint was not called")
			}
			assert.Empty(t, stsCalls)
			if allowed {
				require.NoError(t, err)
				wantARN := "arn:aws-us-gov:cloudfront::987654321123:origin-request-policy/policy-id"
				assert.Equal(t, wantARN, out.Arn)
				assert.Equal(t, []originRequestCall{
					{Method: http.MethodPost, Path: originRequestPolicyPath,
						Config: originRequestInputXML()},
				}, cloudCalls())
			} else {
				require.ErrorContains(t, err, "get caller identity for origin request policy arn")
				assert.Nil(t, out)
				assert.Empty(t, cloudCalls())
			}
		})
	}
}
