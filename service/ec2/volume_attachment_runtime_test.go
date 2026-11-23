package ec2

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	attachmentAddress          = "resource.attachment"
	attachmentLibrary          = "github.com/cloudboss/unobin-library-aws//service/ec2"
	attachmentEvaluatedFactory = "../testdata/ub/valid/compiled/" +
		"ec2-volume-attachment-evaluated/src/factory.ub"
	attachmentDeferredFactory = "testdata/ub/volume-attachment/deferred/factory.ub"
)

type attachmentRuntimeTuple struct {
	device, instance, volume string
}

var initialAttachment = attachmentRuntimeTuple{"/dev/sdf", "i-one", "vol-one"}

func (a attachmentRuntimeTuple) inputs() map[string]any {
	return map[string]any{
		"device-name": a.device, "instance-id": a.instance, "volume-id": a.volume,
	}
}

type attachmentRuntimeCall struct {
	endpoint, region, action string
	tuple                    attachmentRuntimeTuple
	force                    string
}

type attachmentRuntimeCloud struct {
	mu    sync.Mutex
	calls []attachmentRuntimeCall
}

var attachmentSigningRegion = regexp.MustCompile(`Credential=[^/]+/[^/]+/([^/]+)/ec2/`)

func (c *attachmentRuntimeCloud) endpoint(
	t *testing.T, name string, existing ...attachmentRuntimeTuple,
) string {
	t.Helper()
	attached := map[attachmentRuntimeTuple]bool{}
	for _, a := range existing {
		attached[a] = true
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		action := r.PostForm.Get("Action")
		tuple := attachmentRuntimeTuple{
			r.PostForm.Get("Device"), r.PostForm.Get("InstanceId"), r.PostForm.Get("VolumeId"),
		}
		switch action {
		case "DescribeVolumes":
			tuple.volume = r.PostForm.Get("VolumeId.1")
			for i := 1; i <= 2; i++ {
				prefix := fmt.Sprintf("Filter.%d.", i)
				switch r.PostForm.Get(prefix + "Name") {
				case "attachment.device":
					tuple.device = r.PostForm.Get(prefix + "Value.1")
				case "attachment.instance-id":
					tuple.instance = r.PostForm.Get(prefix + "Value.1")
				}
			}
		case "DescribeInstances", "StopInstances":
			tuple.instance = r.PostForm.Get("InstanceId.1")
		}
		region := ""
		if m := attachmentSigningRegion.FindStringSubmatch(r.Header.Get("Authorization")); m != nil {
			region = m[1]
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.calls = append(c.calls,
			attachmentRuntimeCall{name, region, action, tuple, r.PostForm.Get("Force")})
		w.Header().Set("Content-Type", "text/xml")
		switch action {
		case "DescribeVolumes":
			if !attached[tuple] {
				fmt.Fprint(w, "<DescribeVolumesResponse/>")
				return
			}
			fmt.Fprintf(w, "<DescribeVolumesResponse><volumeSet><item>"+
				"<volumeId>%s</volumeId><status>in-use</status><attachmentSet><item>"+
				"<device>%s</device><instanceId>%s</instanceId><status>attached</status>"+
				"</item></attachmentSet></item></volumeSet></DescribeVolumesResponse>",
				tuple.volume, tuple.device, tuple.instance)
		case "DescribeInstances":
			fmt.Fprint(w, "<DescribeInstancesResponse><reservationSet><item><instancesSet><item>"+
				"<instanceState><name>stopped</name></instanceState></item></instancesSet></item>"+
				"</reservationSet></DescribeInstancesResponse>")
		case "AttachVolume":
			attached[tuple] = true
			fmt.Fprint(w, "<AttachVolumeResponse/>")
		case "DetachVolume":
			delete(attached, tuple)
			fmt.Fprint(w, "<DetachVolumeResponse/>")
		case "StopInstances":
			fmt.Fprint(w, "<StopInstancesResponse/>")
		default:
			t.Errorf("unexpected EC2 action %s", action)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(s.Close)
	return s.URL
}

func (c *attachmentRuntimeCloud) requests() []attachmentRuntimeCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

func (c *attachmentRuntimeCloud) mutations() []attachmentRuntimeCall {
	var calls []attachmentRuntimeCall
	for _, call := range c.requests() {
		if !strings.HasPrefix(call.action, "Describe") {
			calls = append(calls, call)
		}
	}
	return calls
}

type attachmentSeed struct {
	Source map[string]string `ub:"source"`
}

type attachmentSeedOutput struct {
	Tuple map[string]string `ub:"tuple"`
}

func (s *attachmentSeed) Run(context.Context, runtime.NoConfig) (*attachmentSeedOutput, error) {
	return &attachmentSeedOutput{Tuple: s.Source}, nil
}

func attachmentRuntimeLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	schema := readLibrarySchema(t)
	lib := Library()
	lib.LibraryPath = attachmentLibrary
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.volume-attachment": schema.Resources["volume-attachment"].Constraints,
	}
	return map[string]*runtime.Library{
		"aws-ec2": lib,
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[
				attachmentSeed, *attachmentSeedOutput, runtime.NoConfig, *attachmentSeed,
			](),
		}},
	}
}

func attachmentRuntimeExecutor(
	t *testing.T, path string, lib map[string]*runtime.Library,
) *runtime.Executor {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "missing-credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFAKEFAKEFAKEFAKE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	doc, err := syntax.ParseSource(path, source)
	require.NoError(t, err)
	body := doc.Factory.Body
	store, err := local.NewStore(dir, "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(body, lib), Libraries: lib, SyntaxSource: &body, Store: store,
		Inputs: map[string]any{
			"tuple": initialAttachment.inputs(), "force-detach": nil, "skip-destroy": nil,
			"stop-instance-before-detaching": nil,
		},
		Factory: state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"},
	}
}

func seedAttachmentState(
	t *testing.T, exec *runtime.Executor, inputs, cfg map[string]any, region string,
) {
	t.Helper()
	snapshot := state.NewSnapshot(exec.Factory, exec.Store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address: attachmentAddress, Category: "resource", SchemaVersion: 1,
		Binding: &state.Binding{
			Alias: "aws-ec2", Export: "volume-attachment", LibraryPath: attachmentLibrary,
		},
		Inputs: maps.Clone(inputs), Configuration: maps.Clone(cfg),
		Outputs: map[string]any{"region": region},
	}}
	rev, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(rev))
}

func savedAttachmentPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	b, err := runtime.SealPlan(plan, encrypters.Noop{})
	require.NoError(t, err)
	saved, err := runtime.OpenPlan(b, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	require.NoError(t, err)
	return saved
}

func attachmentPlanStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == attachmentAddress {
			return step
		}
	}
	t.Fatalf("missing %s in plan %+v", attachmentAddress, plan.Steps)
	return runtime.PlanStep{}
}

func savedAttachment(t *testing.T, exec *runtime.Executor) *state.Entry {
	t.Helper()
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	return snapshot.Find(attachmentAddress)
}

func requireAttachmentConstraint(t *testing.T, err error, rule int, key string) {
	t.Helper()
	require.ErrorContains(t, err, attachmentAddress)
	require.ErrorContains(t, err,
		fmt.Sprintf("constraints[%d] (predicate): %s must be a non-null string", rule, key))
}

func TestVolumeAttachmentRuntimeRejectsEvaluatedNull(t *testing.T) {
	lib := attachmentRuntimeLibraries(t)
	for i, key := range []string{"device-name", "instance-id", "volume-id"} {
		t.Run(key, func(t *testing.T) {
			cloud := &attachmentRuntimeCloud{}
			prior := initialAttachment
			switch key {
			case "device-name":
				prior.device = ""
			case "instance-id":
				prior.instance = ""
			case "volume-id":
				prior.volume = ""
			}
			cfg := map[string]any{"endpoint-url": cloud.endpoint(t, "prior", prior)}
			exec := attachmentRuntimeExecutor(t, attachmentEvaluatedFactory, lib)
			exec.Inputs["aws-config"] = cfg
			tuple := prior.inputs()
			delete(tuple, key)
			exec.Inputs["tuple"] = tuple
			_, err := exec.Plan(t.Context())
			requireAttachmentConstraint(t, err, i, key)
			assert.Empty(t, cloud.requests())

			exec.Inputs["tuple"] = prior.inputs()
			priorInputs := prior.inputs()
			for _, name := range []string{"force-detach", "skip-destroy", "stop-instance-before-detaching"} {
				priorInputs[name] = nil
			}
			seedAttachmentState(t, exec, priorInputs, cfg, "us-east-1")
			valid := savedAttachmentPlan(t, exec)
			require.Equal(t, runtime.DecisionNoOp, attachmentPlanStep(t, valid).Decision)
			result, err := exec.ApplyPlan(t.Context(), valid)
			require.NoError(t, err)
			assert.Equal(t, []any{prior.device, prior.instance, prior.volume, "us-east-1"},
				result.Outputs["tuple"])

			saved := savedAttachmentPlan(t, exec)
			require.Equal(t, runtime.DecisionNoOp, attachmentPlanStep(t, saved).Decision)
			exec.Inputs["tuple"] = tuple
			_, err = exec.Plan(t.Context())
			requireAttachmentConstraint(t, err, i, key)
			before := cloud.requests()
			_, err = exec.ApplyPlan(t.Context(), saved)
			requireAttachmentConstraint(t, err, i, key)
			assert.NotContains(t, err.Error(), "inputs changed since the plan was computed")
			assert.Equal(t, before, cloud.requests())
			assert.Empty(t, cloud.mutations())
			assert.Equal(t, prior.inputs()[key], savedAttachment(t, exec).Inputs[key])
		})
	}
}

func TestVolumeAttachmentRuntimeDeferredReplacement(t *testing.T) {
	lib := attachmentRuntimeLibraries(t)
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprintf("valid=%v", valid), func(t *testing.T) {
			cloud := &attachmentRuntimeCloud{}
			cfg := map[string]any{"endpoint-url": cloud.endpoint(t, "prior", initialAttachment)}
			exec := attachmentRuntimeExecutor(t, attachmentDeferredFactory, lib)
			exec.Inputs["aws-config"] = cfg
			desired := attachmentRuntimeTuple{"/dev/sdg", "i-one", "vol-next"}
			tuple := desired.inputs()
			if !valid {
				delete(tuple, "device-name")
			}
			exec.Inputs["tuple"] = tuple
			prior := initialAttachment.inputs()
			prior["force-detach"] = true
			prior["stop-instance-before-detaching"] = true
			seedAttachmentState(t, exec, prior, cfg, "us-east-1")

			plan := savedAttachmentPlan(t, exec)
			step := attachmentPlanStep(t, plan)
			assert.Equal(t, map[string][]string{"device-name": {"action.seed.tuple"}},
				step.UnresolvedInputs)
			assert.Equal(t, []string{"device-name"}, step.PendingReplacementReasons)
			assert.Equal(t, []string{"volume-id"}, step.ReplacementReasons)
			require.Equal(t, runtime.DecisionReplace, step.Decision)
			before := cloud.requests()
			_, err := exec.ApplyPlan(t.Context(), plan)
			if !valid {
				requireAttachmentConstraint(t, err, 0, "device-name")
				assert.Equal(t, before, cloud.requests())
				assert.Empty(t, cloud.mutations())
				assert.Equal(t, prior, savedAttachment(t, exec).Inputs)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []attachmentRuntimeCall{
				{"prior", "us-east-1", "StopInstances", attachmentRuntimeTuple{instance: "i-one"}, "false"},
				{"prior", "us-east-1", "DetachVolume", initialAttachment, "true"},
				{"prior", "us-east-1", "AttachVolume", desired, ""},
			}, cloud.mutations())
			assert.Equal(t, desired.inputs(), savedAttachment(t, exec).Inputs)
		})
	}
}

func TestVolumeAttachmentRuntimeUpdatesAndClearsDeleteFlags(t *testing.T) {
	lib := attachmentRuntimeLibraries(t)
	cloud := &attachmentRuntimeCloud{}
	cfg := map[string]any{"endpoint-url": cloud.endpoint(t, "prior", initialAttachment)}
	exec := attachmentRuntimeExecutor(t, attachmentEvaluatedFactory, lib)
	exec.Inputs["aws-config"] = cfg
	seedAttachmentState(t, exec, initialAttachment.inputs(), cfg, "us-east-1")
	flags := []string{"force-detach", "skip-destroy", "stop-instance-before-detaching"}
	for _, value := range []any{true, nil} {
		want := initialAttachment.inputs()
		for _, flag := range flags {
			exec.Inputs[flag] = value
			want[flag] = value
		}
		plan := savedAttachmentPlan(t, exec)
		require.Equal(t, runtime.DecisionUpdate, attachmentPlanStep(t, plan).Decision)
		_, err := exec.ApplyPlan(t.Context(), plan)
		require.NoError(t, err)
		entry := savedAttachment(t, exec)
		assert.Equal(t, want, entry.Inputs)
		assert.Equal(t, map[string]any{"region": "us-east-1"}, entry.Outputs)
		assert.Empty(t, cloud.mutations())
	}
	exec.Destroy = true
	_, err := exec.ApplyPlan(t.Context(), savedAttachmentPlan(t, exec))
	require.NoError(t, err)
	assert.Equal(t, []attachmentRuntimeCall{
		{"prior", "us-east-1", "DetachVolume", initialAttachment, "false"},
	}, cloud.mutations())
	assert.Nil(t, savedAttachment(t, exec))
}

func TestVolumeAttachmentRuntimeReplacesEachIdentity(t *testing.T) {
	lib := attachmentRuntimeLibraries(t)
	for key, next := range map[string]attachmentRuntimeTuple{
		"device-name": {"/dev/sdg", "i-one", "vol-one"},
		"instance-id": {"/dev/sdf", "i-next", "vol-one"},
		"volume-id":   {"/dev/sdf", "i-one", "vol-next"},
	} {
		t.Run(key, func(t *testing.T) {
			cloud := &attachmentRuntimeCloud{}
			cfg := map[string]any{"endpoint-url": cloud.endpoint(t, "prior", initialAttachment)}
			exec := attachmentRuntimeExecutor(t, attachmentEvaluatedFactory, lib)
			exec.Inputs["aws-config"] = cfg
			exec.Inputs["tuple"] = next.inputs()
			seedAttachmentState(t, exec, initialAttachment.inputs(), cfg, "us-east-1")
			plan := savedAttachmentPlan(t, exec)
			step := attachmentPlanStep(t, plan)
			require.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.Equal(t, []string{key}, step.ReplacementReasons)
			_, err := exec.ApplyPlan(t.Context(), plan)
			require.NoError(t, err)
			assert.Equal(t, []attachmentRuntimeCall{
				{"prior", "us-east-1", "DetachVolume", initialAttachment, "false"},
				{"prior", "us-east-1", "AttachVolume", next, ""},
			}, cloud.mutations())
		})
	}
}

func TestVolumeAttachmentRuntimeUsesPriorRegion(t *testing.T) {
	lib := attachmentRuntimeLibraries(t)
	for _, declaredChange := range []bool{false, true} {
		t.Run(fmt.Sprintf("declared=%v", declaredChange), func(t *testing.T) {
			cloud := &attachmentRuntimeCloud{}
			priorCfg := map[string]any{"endpoint-url": cloud.endpoint(t, "prior", initialAttachment)}
			exec := attachmentRuntimeExecutor(t, attachmentEvaluatedFactory, lib)
			t.Setenv("AWS_REGION", "us-west-2")
			var want []attachmentRuntimeCall
			if declaredChange {
				priorCfg["region"] = "us-east-1"
				exec.Inputs["aws-config"] = map[string]any{
					"endpoint-url": cloud.endpoint(t, "desired"), "region": "us-west-2",
				}
				want = []attachmentRuntimeCall{
					{"prior", "us-east-1", "DetachVolume", initialAttachment, "false"},
					{"desired", "us-west-2", "AttachVolume", initialAttachment, ""},
				}
			} else {
				exec.Inputs["aws-config"] = maps.Clone(priorCfg)
				want = []attachmentRuntimeCall{
					{"prior", "us-east-1", "DetachVolume", initialAttachment, "false"},
				}
			}
			prior := initialAttachment.inputs()
			for _, name := range []string{"force-detach", "skip-destroy", "stop-instance-before-detaching"} {
				prior[name] = nil
			}
			seedAttachmentState(t, exec, prior, priorCfg, "us-east-1")
			plan := savedAttachmentPlan(t, exec)
			step := attachmentPlanStep(t, plan)
			if declaredChange {
				require.Equal(t, runtime.DecisionReplace, step.Decision)
				assert.Equal(t, []string{"region"}, step.ReplacementReasons)
			} else {
				require.Equal(t, runtime.DecisionNoOp, step.Decision)
			}
			assert.Equal(t, map[string]any{"region": "us-east-1"}, step.ObservedOutputs)
			_, err := exec.ApplyPlan(t.Context(), plan)
			require.NoError(t, err)
			if declaredChange {
				assert.Equal(t, map[string]any{"region": "us-west-2"}, savedAttachment(t, exec).Outputs)
			} else {
				exec.Destroy = true
				_, err = exec.ApplyPlan(t.Context(), savedAttachmentPlan(t, exec))
				require.NoError(t, err)
			}
			assert.Equal(t, want, cloud.mutations())
			detached := false
			for _, call := range cloud.requests() {
				if call.endpoint == "desired" {
					assert.True(t, detached)
					assert.Equal(t, "us-west-2", call.region)
				} else {
					assert.Equal(t, "prior", call.endpoint)
					assert.Equal(t, "us-east-1", call.region)
				}
				if call.action == "DetachVolume" {
					detached = true
				}
			}
		})
	}
}

func TestVolumeAttachmentRuntimeHistoricalDestroy(t *testing.T) {
	lib := attachmentRuntimeLibraries(t)
	cloud := &attachmentRuntimeCloud{}
	historical := attachmentRuntimeTuple{"", "i-one", "vol-one"}
	cfg := map[string]any{"endpoint-url": cloud.endpoint(t, "prior", historical)}
	exec := attachmentRuntimeExecutor(t, attachmentEvaluatedFactory, lib)
	exec.Inputs["aws-config"] = cfg
	prior := historical.inputs()
	prior["device-name"] = nil
	prior["force-detach"] = true
	seedAttachmentState(t, exec, prior, cfg, "us-east-1")
	exec.Inputs["tuple"] = map[string]any{"instance-id": "i-one", "volume-id": "vol-one"}
	exec.Destroy = true
	plan := savedAttachmentPlan(t, exec)
	require.Equal(t, runtime.DecisionDestroy, attachmentPlanStep(t, plan).Decision)
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, []attachmentRuntimeCall{
		{"prior", "us-east-1", "DetachVolume", historical, "true"},
	}, cloud.mutations())
	assert.Nil(t, savedAttachment(t, exec))
}

func TestVolumeAttachmentRuntimeAdoptsAndRetains(t *testing.T) {
	lib := attachmentRuntimeLibraries(t)
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprintf("replace=%v", replace), func(t *testing.T) {
			cloud := &attachmentRuntimeCloud{}
			cfg := map[string]any{"endpoint-url": cloud.endpoint(t, "prior", initialAttachment)}
			exec := attachmentRuntimeExecutor(t, attachmentEvaluatedFactory, lib)
			exec.Inputs["aws-config"] = cfg
			exec.Inputs["skip-destroy"] = true
			exec.Inputs["stop-instance-before-detaching"] = true

			plan := savedAttachmentPlan(t, exec)
			require.Equal(t, runtime.DecisionCreate, attachmentPlanStep(t, plan).Decision)
			result, err := exec.ApplyPlan(t.Context(), plan)
			require.NoError(t, err)
			assert.Equal(t, []any{"/dev/sdf", "i-one", "vol-one", "us-east-1"},
				result.Outputs["tuple"])
			lookup := attachmentRuntimeCall{
				"prior", "us-east-1", "DescribeVolumes", initialAttachment, "",
			}
			assert.Equal(t, []attachmentRuntimeCall{lookup, lookup, lookup}, cloud.requests())

			wantDecision := runtime.DecisionDestroy
			var wantMutations []attachmentRuntimeCall
			if replace {
				next := attachmentRuntimeTuple{"/dev/sdg", "i-one", "vol-next"}
				exec.Inputs["tuple"] = next.inputs()
				exec.Inputs["skip-destroy"] = false
				exec.Inputs["stop-instance-before-detaching"] = false
				wantDecision = runtime.DecisionReplace
				wantMutations = []attachmentRuntimeCall{
					{"prior", "us-east-1", "AttachVolume", next, ""},
				}
			} else {
				exec.Destroy = true
			}
			plan = savedAttachmentPlan(t, exec)
			require.Equal(t, wantDecision, attachmentPlanStep(t, plan).Decision)
			_, err = exec.ApplyPlan(t.Context(), plan)
			require.NoError(t, err)
			assert.Equal(t, wantMutations, cloud.mutations())
			if replace {
				assert.Equal(t, "vol-next", savedAttachment(t, exec).Inputs["volume-id"])
			} else {
				assert.Nil(t, savedAttachment(t, exec))
			}
		})
	}
}

func TestVolumeAttachmentRuntimeRecreatesAfterDrift(t *testing.T) {
	lib := attachmentRuntimeLibraries(t)
	cloud := &attachmentRuntimeCloud{}
	cfg := map[string]any{"endpoint-url": cloud.endpoint(t, "prior")}
	exec := attachmentRuntimeExecutor(t, attachmentEvaluatedFactory, lib)
	exec.Inputs["aws-config"] = cfg
	seedAttachmentState(t, exec, initialAttachment.inputs(), cfg, "us-east-1")
	plan := savedAttachmentPlan(t, exec)
	require.Equal(t, runtime.DecisionCreate, attachmentPlanStep(t, plan).Decision)
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, []attachmentRuntimeCall{
		{"prior", "us-east-1", "AttachVolume", initialAttachment, ""},
	}, cloud.mutations())
	assert.Equal(t, "vol-one", savedAttachment(t, exec).Inputs["volume-id"])
}
