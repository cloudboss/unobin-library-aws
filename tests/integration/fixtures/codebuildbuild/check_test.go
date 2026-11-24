package codebuildbuild

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCheckComparesCompletedIdentityAndOverrides(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	for _, field := range []string{"valid", "ID", "project", "status", "environment", "variables",
		"name", "value", "type"} {
		t.Run(field, func(t *testing.T) {
			variable := map[string]any{"name": "RUN", "value": "requested", "type": "PLAINTEXT"}
			environment := map[string]any{"environmentVariables": []any{variable}}
			build := map[string]any{"id": "started", "projectName": "project",
				"buildStatus": "SUCCEEDED", "environment": environment}
			switch field {
			case "ID":
				build["id"] = "different"
			case "project":
				build["projectName"] = "foreign"
			case "status":
				build["buildStatus"] = "FAILED"
			case "environment":
				delete(build, "environment")
			case "variables":
				environment["environmentVariables"] = []any{}
			case "name", "value", "type":
				variable[field] = "different"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, map[string]any{"ids": []any{"started"}}, body)
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"builds": []any{build}}))
			}))
			defer server.Close()
			r := &BuildCheckDataSource{BuildID: "started", ProjectName: "project", Variable: "requested"}
			out, err := r.Read(t.Context(), &awscfg.Configuration{
				EndpointURL: &server.URL, Region: new("us-east-1"),
			})
			if field == "valid" {
				require.NoError(t, err)
				assert.Equal(t, &BuildCheckOutput{Verified: true}, out)
			} else {
				require.ErrorContains(t, err, "build check:")
			}
		})
	}
}
