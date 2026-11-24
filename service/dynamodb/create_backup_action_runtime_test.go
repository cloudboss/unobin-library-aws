package dynamodb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const backupAddress = "action.backup"

const backupARN = "arn:aws:dynamodb:us-east-1:123456789012:table/table/backup/completed"

type backupSeed struct {
	Echo any `ub:"echo"`
}

func (r *backupSeed) Run(context.Context, *awscfg.Configuration) (map[string]any, error) {
	return map[string]any{"value": r.Echo}, nil
}

type backupRequest struct {
	Operation string
	Body      map[string]any
	Region    string
}

func backupEndpoint(t *testing.T) (string, func() []backupRequest) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	history := make(chan backupRequest, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			w.WriteHeader(400)
			return
		}
		region := "us-east-1"
		if strings.Contains(r.Header.Get("Authorization"), "/us-west-2/") {
			region = "us-west-2"
		}
		operation := r.Header.Get("X-Amz-Target")
		history <- backupRequest{operation, body, region}
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		if operation == "DynamoDB_20120810.CreateBackup" {
			_, _ = w.Write([]byte(`{"BackupDetails":{"BackupArn":"created"}}`))
		} else {
			_, _ = w.Write([]byte(`{"BackupDescription":{"BackupDetails":{` +
				`"BackupArn":"` + backupARN + `","BackupCreationDateTime":1795392000,` +
				`"BackupSizeBytes":42,"BackupStatus":"AVAILABLE"}}}`))
		}
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []backupRequest {
		var out []backupRequest
		for len(history) > 0 {
			out = append(out, <-history)
		}
		return out
	}
}

func backupExecutor(t *testing.T, fixture string, schema *runtime.LibrarySchema) (
	*runtime.Executor, *local.Store,
) {
	t.Helper()
	lib := runtime.LibraryWithPath(Library(),
		"github.com/cloudboss/unobin-library-aws//service/dynamodb")
	lib.Schema = schema
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"action.create-backup": schema.Actions["create-backup"].Constraints,
	}
	libs := map[string]*runtime.Library{
		"dynamodb": lib,
		"test": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[backupSeed, map[string]any, *awscfg.Configuration](),
		}},
	}
	path := backupFixture(fixture)
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	store, err := local.NewStore(t.TempDir(), "test-backup", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "test-backup", Version: "v0", ContentRevision: "test"},
	}, store
}

func savedBackupPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func backupStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == backupAddress {
			return step
		}
	}
	t.Fatal("backup action absent from plan")
	return runtime.PlanStep{}
}

func backupExpectedRequests(table, name, region string) []backupRequest {
	return []backupRequest{
		{"DynamoDB_20120810.CreateBackup", map[string]any{
			"TableName": table, "BackupName": name,
		}, region},
		{"DynamoDB_20120810.DescribeBackup", map[string]any{"BackupArn": "created"}, region},
	}
}

func backupInputCases() []struct {
	name, field string
	value       any
	rule        string
} {
	return []struct {
		name, field string
		value       any
		rule        string
	}{
		{"null table", "table", nil, "constraints[0] (predicate): table-name is required"},
		{"empty table", "table", "", "constraints[1] (predicate): table-name must contain"},
		{"long table", "table", strings.Repeat("t", 1025), "constraints[1] (predicate): table-name"},
		{"empty name", "name", "", "constraints[2] (predicate): backup-name must contain"},
		{"short name", "name", "ab", "constraints[2] (predicate): backup-name must contain"},
		{"long name", "name", strings.Repeat("b", 256), "constraints[2] (predicate): backup-name"},
		{"space name", "name", "a b", "constraints[2] (predicate): backup-name must contain"},
		{"slash name", "name", "a/b", "constraints[2] (predicate): backup-name must contain"},
		{"unicode name", "name", "ééé", "constraints[2] (predicate): backup-name must contain"},
	}
}

func TestBackupEvaluatedInputsAndSavedSkip(t *testing.T) {
	endpoint, requests := backupEndpoint(t)
	executor, store := backupExecutor(t, "settled", readLibrarySchema(t))
	valid := map[string]any{
		"endpoint": endpoint, "table": "table", "name": "chosen", "trigger": "fixed", "region": nil,
	}
	executor.Inputs = valid
	events := make(chan runtime.ApplyEvent, 30)
	executor.Events = events
	first := savedBackupPlan(t, executor)
	require.Equal(t, runtime.DecisionRerun, backupStep(t, first).Decision)
	executor.Inputs = nil
	result, err := executor.ApplyPlan(t.Context(), first)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"backup-arn": backupARN, "backup-creation-date-time": "2026-11-23T00:00:00Z",
		"backup-size-bytes": int64(42),
	}, result.Outputs)
	assert.Equal(t, backupExpectedRequests("table", "chosen", "us-east-1"), requests())
	var stages []runtime.ApplyStage
	for len(events) > 0 {
		if event := <-events; event.Address == backupAddress {
			stages = append(stages, event.Stage)
		}
	}
	assert.Equal(t, []runtime.ApplyStage{runtime.StageStart, runtime.StageDone}, stages)
	executor.Events = nil
	executor.Inputs = valid
	skip := savedBackupPlan(t, executor)
	require.Equal(t, runtime.DecisionSkip, backupStep(t, skip).Decision)
	executor.Inputs = nil
	unchanged, err := executor.ApplyPlan(t.Context(), skip)
	require.NoError(t, err)
	assert.Equal(t, result.Outputs, unchanged.Outputs)
	assert.Empty(t, requests())
	for _, test := range backupInputCases() {
		t.Run(test.name, func(t *testing.T) {
			executor.Inputs = map[string]any{
				"endpoint": endpoint, "table": "table", "name": "chosen", "trigger": "fixed", "region": nil,
			}
			saved := savedBackupPlan(t, executor)
			require.Equal(t, runtime.DecisionSkip, backupStep(t, saved).Decision)
			executor.Inputs[test.field] = test.value
			_, err := executor.Plan(t.Context())
			require.ErrorContains(t, err, "action.backup: schema: "+test.rule)
			_, err = executor.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, "action.backup: schema: "+test.rule)
			assert.Empty(t, requests())
		})
	}
	snapshot, err := store.Current()
	require.NoError(t, err)
	snapshot.Find(backupAddress).Inputs["table-name"] = ""
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	executor.Destroy = true
	executor.Inputs = map[string]any{
		"endpoint": endpoint, "table": "", "name": "!", "trigger": "fixed", "region": nil,
	}
	destroy := savedBackupPlan(t, executor)
	require.Equal(t, runtime.DecisionDestroy, backupStep(t, destroy).Decision)
	_, err = executor.ApplyPlan(t.Context(), destroy)
	require.NoError(t, err)
	assert.Empty(t, requests())
	snapshot, err = store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(backupAddress))
}

func TestBackupDeferredSavedPlan(t *testing.T) {
	schema := readLibrarySchema(t)
	for _, test := range append(backupInputCases(),
		struct {
			name, field string
			value       any
			rule        string
		}{name: "valid"},
		struct {
			name, field string
			value       any
			rule        string
		}{
			name: "null name", field: "name", value: nil,
		},
	) {
		t.Run(test.name, func(t *testing.T) {
			endpoint, requests := backupEndpoint(t)
			executor, _ := backupExecutor(t, "deferred", schema)
			executor.Inputs = map[string]any{
				"endpoint": endpoint, "table": "table", "name": "chosen", "trigger": "fixed",
			}
			if test.field != "" {
				executor.Inputs[test.field] = test.value
			}
			saved := savedBackupPlan(t, executor)
			assert.Equal(t, map[string][]string{
				"table-name": {"action.table.value"}, "backup-name": {"action.name.value"},
			}, backupStep(t, saved).UnresolvedInputs)
			assert.Empty(t, requests())
			executor.Inputs = nil
			_, err := executor.ApplyPlan(t.Context(), saved)
			if test.rule != "" {
				require.ErrorContains(t, err, "action.backup: schema: "+test.rule)
				assert.Empty(t, requests())
				return
			}
			require.NoError(t, err)
			got := requests()
			if test.name == "null name" {
				require.NotEmpty(t, got)
				name := got[0].Body["BackupName"].(string)
				assert.Regexp(t, `^table-backup-terraform-[A-Za-z0-9]{26}$`, name)
				assert.Equal(t, backupExpectedRequests("table", name, "us-east-1"), got)
			} else {
				assert.Equal(t, backupExpectedRequests("table", "chosen", "us-east-1"), got)
			}
		})
	}
}

func TestBackupRegionAndBoundaryRequests(t *testing.T) {
	schema := readLibrarySchema(t)
	for _, test := range []struct {
		table, name string
		region      *string
	}{
		{"x", "A_.", nil},
		{strings.Repeat("t", 1024), strings.Repeat("b", 255), new("")},
		{"arn:aws:dynamodb:us-east-1:123456789012:table/a b", "name", new("us-west-2")},
		{"table", "name", new("us-gov-west-1")},
		{"table", "name", new("cn-north-1")},
	} {
		t.Run(test.table[:1]+"/"+fmt.Sprint(test.region), func(t *testing.T) {
			endpoint, requests := backupEndpoint(t)
			executor, _ := backupExecutor(t, "settled", schema)
			var region any
			if test.region != nil {
				region = *test.region
			}
			executor.Inputs = map[string]any{
				"endpoint": endpoint, "table": test.table, "name": test.name,
				"trigger": "fixed", "region": region,
			}
			_, err := executor.ApplyPlan(t.Context(), savedBackupPlan(t, executor))
			if region == "us-gov-west-1" || region == "cn-north-1" {
				require.ErrorContains(t, err, "outside configured partition")
				assert.Empty(t, requests())
				return
			}
			require.NoError(t, err)
			signing := "us-east-1"
			if region == "us-west-2" {
				signing = "us-west-2"
			}
			assert.Equal(t, backupExpectedRequests(test.table, test.name, signing), requests())
		})
	}
}

func TestBackupLiteralRequests(t *testing.T) {
	endpoint, requests := backupEndpoint(t)
	executor, _ := backupExecutor(t, "literal-run", readLibrarySchema(t))
	executor.Inputs = map[string]any{"endpoint": endpoint}
	saved := savedBackupPlan(t, executor)
	executor.Inputs = nil
	result, err := executor.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"backup-arn": backupARN, "backup-creation-date-time": "2026-11-23T00:00:00Z",
		"backup-size-bytes": int64(42),
	}, result.Outputs)
	got := requests()
	want := backupExpectedRequests("x", "A_.-", "us-east-1")
	generated := map[string]string{}
	for _, request := range got {
		if request.Operation != "DynamoDB_20120810.CreateBackup" {
			continue
		}
		table, ok := request.Body["TableName"].(string)
		require.True(t, ok)
		if table == "x" {
			continue
		}
		require.Contains(t, []string{"generated-table", "null-table"}, table)
		name, ok := request.Body["BackupName"].(string)
		require.True(t, ok)
		assert.Regexp(t, "^"+table+"-backup-terraform-[A-Za-z0-9]{26}$", name)
		generated[table] = "created"
		want = append(want, backupExpectedRequests(table, name, "us-east-1")...)
	}
	assert.Equal(t, map[string]string{
		"generated-table": "created", "null-table": "created",
	}, generated)
	assert.ElementsMatch(t, want, got)
}
