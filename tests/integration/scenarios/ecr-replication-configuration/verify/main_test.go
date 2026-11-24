package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if os.Getenv("ECR_VERIFY_TEST_COMMAND") == "1" {
		os.Exit(runTestCommand())
	}
	os.Exit(m.Run())
}

func runTestCommand() int {
	want := []string{"plan", "-c", os.Getenv("ECR_VERIFY_TEST_CONFIG"), "-o", "noop.json"}
	if fmt.Sprint(os.Args[1:]) != fmt.Sprint(want) {
		fmt.Fprintf(os.Stderr, "arguments = %v, want %v", os.Args[1:], want)
		return 1
	}
	dir, err := os.Getwd()
	if err != nil || dir != os.Getenv("VERIFY_BUILD_DIR") {
		fmt.Fprintf(os.Stderr, "working directory = %q: %v", dir, err)
		return 1
	}
	body, err := os.ReadFile("expected.plan")
	if err == nil {
		err = os.WriteFile("noop.json", body, 0o600)
	}
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		return 1
	}
	return 0
}

func noOpSteps() []*runtime.PlanStep {
	return []*runtime.PlanStep{
		{Address: "library-config.ecr", Kind: runtime.NodeLibraryConfig,
			Decision: runtime.DecisionEval},
		{Address: "resource.replication", Kind: runtime.NodeResource,
			Decision: runtime.DecisionNoOp},
		{Address: "output.configuration", Kind: runtime.NodeOutput,
			Decision: runtime.DecisionEval},
	}
}

func writeTestPlan(t *testing.T, path string, steps []*runtime.PlanStep) {
	t.Helper()
	body, err := runtime.SealPlan(&runtime.Plan{Steps: steps}, encrypters.Noop{})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o600))
}

func TestCheckNoOpPlan(t *testing.T) {
	tests := []struct {
		name    string
		steps   []*runtime.PlanStep
		wantErr string
	}{
		{name: "resource NoOp beside evaluations", steps: noOpSteps()},
		{name: "empty plan", wantErr: "missing resource.replication"},
		{name: "evaluations only", steps: noOpSteps()[:1],
			wantErr: "missing resource.replication"},
		{name: "duplicate resource", steps: append(noOpSteps(), noOpSteps()[1]),
			wantErr: "duplicate resource.replication"},
		{name: "unexpected resource", steps: append(noOpSteps(), &runtime.PlanStep{
			Address: "resource.other", Kind: runtime.NodeResource, Decision: runtime.DecisionNoOp,
		}), wantErr: "unexpected resource"},
		{name: "wrong resource kind", steps: []*runtime.PlanStep{{
			Address: "resource.replication", Kind: runtime.NodeOutput, Decision: runtime.DecisionEval,
		}}, wantErr: "missing resource.replication"},
	}
	for _, decision := range []runtime.Decision{
		runtime.DecisionCreate, runtime.DecisionUpdate, runtime.DecisionReplace,
		runtime.DecisionDestroy, runtime.DecisionRead, "",
	} {
		steps := noOpSteps()
		steps[1].Decision = decision
		tests = append(tests, struct {
			name    string
			steps   []*runtime.PlanStep
			wantErr string
		}{name: "resource decision " + string(decision), steps: steps, wantErr: "want no-op"})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "noop.json")
			writeTestPlan(t, path, tt.steps)
			err := checkNoOpPlan(path)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVerifyManagedNoOp(t *testing.T) {
	for _, phase := range []string{"applied", "updated"} {
		for _, mutate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mutate=%t", phase, mutate), func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("VERIFY_BUILD_DIR", dir)
				t.Setenv("ECR_VERIFY_TEST_COMMAND", "1")
				config := "config.ub"
				if phase == "updated" {
					config = "update/config.ub"
				}
				remote := `{"registryId":"123456789012","replicationConfiguration":{"rules":[{` +
					`"destinations":[{"region":"us-west-2","registryId":"000000000001"}],` +
					`"repositoryFilters":[{"filter":"unobin-initial","filterType":"PREFIX_MATCH"}]` +
					`}]}}`
				if phase == "updated" {
					remote = `{"registryId":"123456789012","replicationConfiguration":{"rules":[{` +
						`"destinations":[{"region":"eu-west-1","registryId":"000000000002"}]` +
						`}]}}`
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,
					r *http.Request,
				) {
					require.Equal(t, "AmazonEC2ContainerRegistry_V20150921.DescribeRegistry",
						r.Header.Get("X-Amz-Target"))
					w.Header().Set("Content-Type", "application/x-amz-json-1.1")
					_, _ = fmt.Fprint(w, remote)
				}))
				t.Cleanup(server.Close)
				t.Setenv("AWS_ENDPOINT_URL_ECR", server.URL)
				t.Setenv("AWS_ACCESS_KEY_ID", "test-access")
				t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
				t.Setenv("AWS_REGION", "us-east-1")
				t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
				t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "aws-config"))
				t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
				t.Setenv("ECR_VERIFY_TEST_CONFIG", config)
				binary, err := os.Executable()
				require.NoError(t, err)
				require.NoError(t, os.Symlink(binary, filepath.Join(dir,
					"ecr-replication-configuration")))
				steps := noOpSteps()
				if mutate {
					steps[1].Decision = runtime.DecisionUpdate
				}
				writeTestPlan(t, filepath.Join(dir, "expected.plan"), steps)
				err = verify(t.Context(), phase)
				if mutate {
					require.ErrorContains(t, err, "want no-op")
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestNoOpPlanErrors(t *testing.T) {
	t.Setenv("VERIFY_BUILD_DIR", "")
	require.ErrorContains(t, verifyNoOp(t.Context(), "applied"), "VERIFY_BUILD_DIR")
	t.Setenv("VERIFY_BUILD_DIR", t.TempDir())
	require.ErrorContains(t, verifyNoOp(t.Context(), "applied"), "plan unchanged configuration")
	require.ErrorContains(t, checkNoOpPlan("missing.plan"), "read unchanged plan")
	path := filepath.Join(t.TempDir(), "invalid.plan")
	require.NoError(t, os.WriteFile(path, []byte("invalid envelope"), 0o600))
	require.ErrorContains(t, checkNoOpPlan(path), "open unchanged plan")
}
