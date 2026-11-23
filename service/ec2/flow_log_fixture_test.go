package ec2

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	s3library "github.com/cloudboss/unobin-library-aws/service/s3"
)

func TestFlowLogManagedFixtureLifecycleAndFailureCleanup(t *testing.T) {
	libraries := flowLogRuntimeLibrary(t)
	libraries["aws-s3"] = s3library.Library()
	path := "../../tests/integration/scenarios/ec2-flow-log/factory.ub"
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	for _, mode := range []string{"success", "CreateVpc", "CreateBucket", "CreateFlowLogs",
		"CreateTags", "plan failure", "cleanup failure"} {
		t.Run(mode, func(t *testing.T) {
			cloud := &flowLogCloud{}
			if mode != "success" && mode != "plan failure" && mode != "cleanup failure" {
				cloud.fail = mode
			}
			exec := peeringRuntimeExecutor(t, "../flow-log/evaluated", libraries,
				flowLogInputs(t, cloud.endpoint(t, "fixture")))
			exec.SyntaxSource = &parsed.Factory.Body
			exec.DAG = runtime.BuildSyntaxDAG(parsed.Factory.Body, libraries)
			exec.Inputs = map[string]any{
				"aws-config": exec.Inputs["aws-config"], "stage": "initial", "traffic-type": "ALL",
			}
			if mode == "plan failure" {
				exec.Inputs["traffic-type"] = "invalid"
				_, err := exec.Plan(t.Context())
				require.Error(t, err)
				assert.False(t, cloud.exists)
				assert.False(t, cloud.vpc)
				assert.False(t, cloud.bucket)
				return
			}
			_, err := exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
			if mode == "CreateVpc" || mode == "CreateBucket" || mode == "CreateFlowLogs" {
				require.ErrorContains(t, err, "FixtureFailure")
			} else {
				require.NoError(t, err)
				assert.True(t, cloud.exists)
				assert.True(t, cloud.vpc)
				assert.True(t, cloud.bucket)
				for _, request := range cloud.requests {
					if request.Form.Get("Action") == "CreateFlowLogs" {
						assert.NotContains(t, request.Form, "DestinationOptions.FileFormat")
						assert.NotContains(t, request.Form, "DestinationOptions.HiveCompatiblePartitions")
						assert.NotContains(t, request.Form, "DestinationOptions.PerHourPartition")
					}
				}
				assert.Equal(t, map[string]string{"unobin": "ec2-flow-log-it", "stage": "initial"}, cloud.tags)
				exec.Inputs["stage"] = "updated"
				_, err = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
				if mode == "CreateTags" {
					require.ErrorContains(t, err, "FixtureFailure")
				} else {
					require.NoError(t, err)
					assert.Equal(t, map[string]string{"unobin": "ec2-flow-log-it", "stage": "updated"}, cloud.tags)
					unchanged := peeringRuntimeSavedPlan(t, exec)
					for _, step := range unchanged.Steps {
						if step.Kind == runtime.NodeResource {
							assert.Equal(t, runtime.DecisionNoOp, step.Decision)
						}
					}
				}
			}
			cloud.fail = ""
			exec.Destroy = true
			if mode == "cleanup failure" {
				cloud.fail = "DeleteFlowLogs"
				_, err = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
				require.ErrorContains(t, err, "FixtureFailure")
				assert.True(t, cloud.exists)
				assert.True(t, cloud.vpc)
				assert.True(t, cloud.bucket)
				snapshot, err := exec.Store.Current()
				require.NoError(t, err)
				require.NotNil(t, snapshot.Find("resource.flow"))
				cloud.fail = ""
			}
			_, err = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
			require.NoError(t, err)
			assert.False(t, cloud.exists)
			assert.False(t, cloud.vpc)
			assert.False(t, cloud.bucket)
			snapshot, err := exec.Store.Current()
			require.NoError(t, err)
			assert.Empty(t, snapshot.Entries)
		})
	}
}

func TestFlowLogManagedFixtureAcceptedCreateFailureCleanup(t *testing.T) {
	libraries := flowLogRuntimeLibrary(t)
	libraries["aws-s3"] = s3library.Library()
	path := "../../tests/integration/scenarios/ec2-flow-log/factory.ub"
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	for _, action := range []string{"DescribeFlowLogs", "GetCallerIdentity"} {
		for _, cleanupFailure := range []bool{false, true} {
			t.Run(action+"/cleanup-failure="+fmt.Sprint(cleanupFailure), func(t *testing.T) {
				cloud := &flowLogCloud{failAfterCreate: action, failCleanup: cleanupFailure}
				endpoint := cloud.endpoint(t, "accepted-create")
				exec := peeringRuntimeExecutor(t, "../flow-log/evaluated", libraries,
					flowLogInputs(t, endpoint))
				exec.SyntaxSource = &parsed.Factory.Body
				exec.DAG = runtime.BuildSyntaxDAG(parsed.Factory.Body, libraries)
				exec.Inputs = map[string]any{
					"aws-config": exec.Inputs["aws-config"], "stage": "initial", "traffic-type": "ALL",
				}
				_, createErr := exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
				require.ErrorContains(t, createErr, "FixtureFailure")
				assert.Equal(t, cleanupFailure, cloud.exists)
				assert.True(t, cloud.vpc)
				assert.True(t, cloud.bucket)
				snapshot, err := exec.Store.Current()
				require.NoError(t, err)
				assert.Nil(t, snapshot.Find("resource.flow"))
				var addresses []string
				for _, entry := range snapshot.Entries {
					addresses = append(addresses, entry.Address)
				}
				slices.Sort(addresses)
				assert.Equal(t, []string{"resource.logs", "resource.network"}, addresses)
				if cleanupFailure {
					assert.ErrorContains(t, createErr, "clean up flow log fl-created in region us-east-1")
					cloud.failCleanup = false
					cfg, err := awscfg.Load(t.Context(), &awscfg.Configuration{
						Region: aws.String("us-east-1"), EndpointURL: &endpoint})
					require.NoError(t, err)
					_, err = ec2.NewFromConfig(cfg).DeleteFlowLogs(t.Context(), &ec2.DeleteFlowLogsInput{
						FlowLogIds: []string{"fl-created"},
					})
					require.NoError(t, err)
					assert.False(t, cloud.exists)
				}
				cloud.failAfterCreate = ""
				exec.Destroy = true
				_, err = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
				require.NoError(t, err)
				assert.False(t, cloud.exists)
				assert.False(t, cloud.vpc)
				assert.False(t, cloud.bucket)
				snapshot, err = exec.Store.Current()
				require.NoError(t, err)
				assert.Empty(t, snapshot.Entries)
			})
		}
	}
}
