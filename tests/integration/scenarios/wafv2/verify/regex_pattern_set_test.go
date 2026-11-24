package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/stretchr/testify/require"
)

func TestVerifyRegexPatternSets(t *testing.T) {
	for _, mode := range []string{"applied", "updated", "destroyed"} {
		t.Run(mode, func(t *testing.T) {
			phase := "applied"
			initialTags := map[string]string{
				"unobin": "wafv2-it", "change": "old", "keep": "1", "remove": "yes",
			}
			updatedTags := map[string]string{
				"unobin": "wafv2-it", "add": "yes", "change": "new", "keep": "1",
			}
			type remote struct {
				description string
				patterns    []string
				tags        map[string]string
			}
			fixtures := map[string]map[string]remote{
				"applied": {
					"clear": {"initial", []string{"example$", "^initial"}, initialTags},
					"edit":  {"initial", []string{"/initial"}, initialTags},
				},
				"updated": {
					"clear": {"", nil, nil},
					"edit":  {"updated", []string{"/updated"}, updatedTags},
				},
			}
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, operation, _ := strings.Cut(r.Header.Get("X-Amz-Target"), ".")
				calls = append(calls, operation)
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				if phase == "destroyed" {
					w.WriteHeader(http.StatusBadRequest)
					_, err := w.Write([]byte(`{"__type":"WAFNonexistentItemException"}`))
					require.NoError(t, err)
					return
				}
				response := map[string]any{}
				switch operation {
				case "ListRegexPatternSets":
					response["RegexPatternSets"] = []any{
						map[string]any{"Id": "clear", "ARN": "arn-clear", "Name": regexClearName},
						map[string]any{"Id": "edit", "ARN": "arn-edit", "Name": regexEditName},
					}
				case "GetRegexPatternSet":
					id := body["Id"].(string)
					set := fixtures[phase][id]
					values := []any{}
					for _, pattern := range set.patterns {
						values = append(values, map[string]any{"RegexString": pattern})
					}
					response["RegexPatternSet"] = map[string]any{
						"Id": id, "ARN": "arn-" + id, "Name": body["Name"],
						"Description": set.description, "RegularExpressionList": values,
					}
					response["LockToken"] = "current"
				case "ListTagsForResource":
					id := strings.TrimPrefix(body["ResourceARN"].(string), "arn-")
					values := []any{}
					for key, value := range fixtures[phase][id].tags {
						values = append(values, map[string]any{"Key": key, "Value": value})
					}
					response["TagInfoForResource"] = map[string]any{"TagList": values}
				}
				require.NoError(t, json.NewEncoder(w).Encode(response))
			}))
			defer server.Close()
			client := wafv2.New(wafv2.Options{
				Region: "us-east-1", BaseEndpoint: aws.String(server.URL),
				Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
			})
			dir := t.TempDir()
			require.NoError(t, verifyRegexPatternSets(t.Context(), client, dir, "applied"))
			phase = mode
			calls = nil
			require.NoError(t, verifyRegexPatternSets(t.Context(), client, dir, mode))
			want := []string{"GetRegexPatternSet", "ListTagsForResource",
				"GetRegexPatternSet", "ListTagsForResource"}
			if mode == "applied" {
				want = append([]string{"ListRegexPatternSets"}, want...)
			}
			if mode == "destroyed" {
				want = []string{"GetRegexPatternSet", "GetRegexPatternSet"}
			}
			require.Equal(t, want, calls)
		})
	}
}
