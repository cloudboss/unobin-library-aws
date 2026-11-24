package dynamodbbackup

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	awsdynamodb "github.com/cloudboss/unobin-library-aws/service/dynamodb"
)

func TestCleanupExistsBeforeBackupCreation(t *testing.T) {
	r := CleanupResource{TableArn: "table-arn", TableName: "table"}
	out, err := r.Create(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, &CleanupOutput{TableName: "table"}, out)
}

func TestFailedActionStillCleansUnreturnedBackup(t *testing.T) {
	for _, failCreation := range []bool{false, true} {
		t.Run(fmt.Sprintf("creation-error=%t", failCreation), func(t *testing.T) {
			t.Setenv("AWS_ACCESS_KEY_ID", "test")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
			t.Setenv("AWS_SESSION_TOKEN", "")
			t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
			var calls []string
			deleted := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				op := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "DynamoDB_20120810.")
				calls = append(calls, op)
				w.Header().Set("Content-Type", "application/x-amz-json-1.0")
				switch op {
				case "CreateBackup":
					assert.Equal(t, map[string]any{
						"TableName": "owned-table", "BackupName": "failure-backup",
					}, body)
					if failCreation {
						w.WriteHeader(400)
						_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException","message":"late error"}`))
					} else {
						_, _ = w.Write([]byte(`{"BackupDetails":{"BackupArn":"unreturned"}}`))
					}
				case "ListBackups":
					assert.Equal(t, map[string]any{"TableName": "owned-table", "BackupType": "USER"}, body)
					_, _ = w.Write([]byte(`{"BackupSummaries":[{"BackupArn":"unreturned"}]}`))
				case "DescribeBackup":
					assert.Equal(t, map[string]any{"BackupArn": "unreturned"}, body)
					if deleted {
						w.WriteHeader(400)
						_, _ = w.Write([]byte(`{"__type":"BackupNotFoundException","message":"gone"}`))
					} else {
						_, _ = w.Write([]byte(`{"BackupDescription":{"BackupDetails":` +
							`{"BackupArn":"unreturned","BackupStatus":"AVAILABLE"}}}`))
					}
				case "DeleteBackup":
					assert.Equal(t, map[string]any{"BackupArn": "unreturned"}, body)
					deleted = true
					_, _ = w.Write([]byte(`{}`))
				default:
					t.Errorf("unexpected operation %s", op)
				}
			}))
			defer server.Close()
			module, dep := fixtureModule(t, "github.com/cloudboss/unobin-library-aws"),
				fixtureModule(t, "github.com/cloudboss/unobin")
			schema, warnings, err := goschema.Read(".", module, dep)
			require.NoError(t, err)
			require.Empty(t, warnings)
			lib := runtime.LibraryWithPath(Library(),
				"github.com/cloudboss/unobin-library-aws//tests/integration/fixtures/dynamodbbackup")
			lib.Schema = schema
			actionSchema, warnings, err := goschema.Read("../../../../service/dynamodb", module, dep)
			require.NoError(t, err)
			require.Empty(t, warnings)
			action := runtime.LibraryWithPath(awsdynamodb.Library(),
				"github.com/cloudboss/unobin-library-aws//service/dynamodb")
			action.Schema = actionSchema
			action.Constraints = map[string][]lang.ConstraintSpec{
				"action.create-backup": actionSchema.Actions["create-backup"].Constraints,
			}
			libs := map[string]*runtime.Library{"fixture": lib, "dynamodb": action}
			path := filepath.Join("testdata", "ub", "failure", "factory.ub")
			source, err := os.ReadFile(path)
			require.NoError(t, err)
			file, _, err := compile.ParseFactorySyntaxSource(path, source)
			require.NoError(t, err)
			body := file.Factory.Body
			checker := check.NewSyntax(body, libs, nil, "")
			require.NoError(t, checker.References(nil).Err())
			store, err := local.NewStore(t.TempDir(), "failed-backup", "test", encrypters.Noop{})
			require.NoError(t, err)
			executor := &runtime.Executor{
				DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
				Inputs:  map[string]any{"endpoint": server.URL},
				Factory: state.FactoryInfo{Name: "failed-backup", Version: "v0", ContentRevision: "test"},
			}
			plan, err := executor.Plan(t.Context())
			require.NoError(t, err)
			_, err = executor.ApplyPlan(t.Context(), fixtureSavedPlan(t, plan))
			if failCreation {
				require.ErrorContains(t, err, "create backup for table owned-table")
			} else {
				require.ErrorContains(t, err, "timeout must be positive")
			}
			snapshot, err := store.Current()
			require.NoError(t, err)
			require.NotNil(t, snapshot.Find("resource.cleanup"))
			assert.Nil(t, snapshot.Find("action.backup"))
			executor.Destroy = true
			plan, err = executor.Plan(t.Context())
			require.NoError(t, err)
			_, err = executor.ApplyPlan(t.Context(), fixtureSavedPlan(t, plan))
			require.NoError(t, err)
			assert.True(t, deleted)
			assert.Equal(t, []string{
				"CreateBackup", "ListBackups", "DescribeBackup", "DeleteBackup", "DescribeBackup",
			}, calls)
		})
	}
}

func fixtureModule(t *testing.T, name string) goschema.ModuleRoot {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", name).Output()
	require.NoError(t, err)
	return goschema.ModuleRoot{Path: name, Dir: strings.TrimSpace(string(out))}
}

func fixtureSavedPlan(t *testing.T, plan *runtime.Plan) *runtime.PlanFile {
	t.Helper()
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func TestIntegrationFactoryCompiles(t *testing.T) {
	module := fixtureModule(t, "github.com/cloudboss/unobin-library-aws")
	dep := fixtureModule(t, "github.com/cloudboss/unobin")
	for _, path := range []string{
		"../../scenarios/dynamodb-create-backup/factory.ub", "testdata/ub/failure/factory.ub",
	} {
		err := compile.Run(compile.Options{
			FactoryPath: path, OutDir: t.TempDir(), GoVersion: compile.GoMajorMinor(),
			CLIVersion: "dev", ReplaceUnobin: dep.Dir,
			ReplaceGoModules: map[string]string{module.Path: module.Dir},
			Stdout:           io.Discard, Stderr: io.Discard,
		})
		require.NoError(t, err)
	}
}

func TestBackupCheckComparesCompletedOutputs(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = w.Write([]byte(`{"BackupDescription":{"BackupDetails":{` +
			`"BackupArn":"backup","BackupCreationDateTime":1795392000,` +
			`"BackupStatus":"AVAILABLE","BackupSizeBytes":42}}}`))
	}))
	defer server.Close()
	for _, field := range []string{"valid", "arn", "date", "size"} {
		r := BackupCheckDataSource{
			BackupArn: "backup", BackupCreationDateTime: "2026-11-23T00:00:00Z", BackupSizeBytes: 42,
		}
		switch field {
		case "arn":
			r.BackupArn = "other"
		case "date":
			r.BackupCreationDateTime = "different"
		case "size":
			r.BackupSizeBytes++
		}
		out, err := r.Read(t.Context(), &awscfg.Configuration{
			EndpointURL: &server.URL, Region: new("us-east-1"),
		})
		if field == "valid" {
			require.NoError(t, err)
			assert.Equal(t, &BackupCheckOutput{Verified: true}, out)
		} else {
			require.ErrorContains(t, err, "action outputs differ")
		}
	}
}
