package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyBackupPhases(t *testing.T) {
	for _, test := range []struct{ phase, failure string }{
		{phase: "applied"}, {phase: "updated"}, {phase: "destroyed"},
		{phase: "applied", failure: "wrong name"},
		{phase: "applied", failure: "incomplete"},
		{phase: "destroyed", failure: "remaining backup"},
		{phase: "destroyed", failure: "table exists"},
	} {
		t.Run(test.phase+"/"+test.failure, func(t *testing.T) {
			var operations []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				op := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "DynamoDB_20120810.")
				operations = append(operations, op)
				w.Header().Set("Content-Type", "application/x-amz-json-1.0")
				switch op {
				case "DescribeTable":
					assert.Equal(t, map[string]any{"TableName": tableName}, request)
					if test.phase == "destroyed" && test.failure != "table exists" {
						w.WriteHeader(400)
						_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException"}`))
					} else {
						_, _ = w.Write([]byte(`{"Table":{}}`))
					}
				case "ListBackups":
					assert.Equal(t, map[string]any{
						"TableName": tableName, "BackupType": "USER",
					}, request)
					if test.phase == "destroyed" && test.failure != "remaining backup" {
						_, _ = w.Write([]byte(`{"BackupSummaries":[]}`))
					} else if test.phase == "updated" {
						_, _ = w.Write([]byte(`{"BackupSummaries":[` +
							`{"BackupArn":"applied"},{"BackupArn":"updated"}]}`))
					} else {
						_, _ = w.Write([]byte(`{"BackupSummaries":[{"BackupArn":"applied"}]}`))
					}
				case "DescribeBackup":
					arn := request["BackupArn"].(string)
					name, status := "unobin-it-backup-"+arn, "AVAILABLE"
					if test.failure == "wrong name" {
						name = "wrong"
					}
					if test.failure == "incomplete" {
						status = "CREATING"
					}
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
						"BackupDescription": map[string]any{"BackupDetails": map[string]any{
							"BackupArn": arn, "BackupName": name, "BackupStatus": status,
							"BackupCreationDateTime": 1795392000,
						}},
					}))
				default:
					t.Errorf("unexpected API operation %s", op)
				}
			}))
			defer server.Close()
			client := dynamodb.NewFromConfig(aws.Config{
				Region: "us-east-1", BaseEndpoint: &server.URL,
				Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
			})
			err := verify(t.Context(), client, test.phase)
			if test.failure != "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			want := []string{"DescribeTable", "ListBackups"}
			if test.phase != "destroyed" {
				want = append(want, "DescribeBackup")
			}
			if test.phase == "updated" {
				want = append(want, "DescribeBackup")
			}
			assert.Equal(t, want, operations)
		})
	}
}
