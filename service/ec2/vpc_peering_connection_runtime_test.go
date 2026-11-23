package ec2

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	peeringAddress = "resource.connection"
	priorPeeringID = "pcx-prior"
)

type peeringSeed struct {
	Requester *string
	Accepter  *string
	Region    *string
}

type peeringSeedOutput struct {
	VpcID      *string `ub:"vpc-id"`
	PeerVpcID  *string `ub:"peer-vpc-id"`
	PeerRegion *string `ub:"peer-region"`
}

func (s *peeringSeed) Run(context.Context, runtime.NoConfig) (*peeringSeedOutput, error) {
	return &peeringSeedOutput{VpcID: s.Requester, PeerVpcID: s.Accepter, PeerRegion: s.Region}, nil
}

type peeringRuntimeRequest struct {
	Endpoint string
	Form     url.Values
	Signing  string
}

type peeringRuntimeCloud struct {
	mu          sync.Mutex
	requests    []peeringRuntimeRequest
	deleted     bool
	created     bool
	createdVpc  string
	createdPeer string
	drift       string
}

func (c *peeringRuntimeCloud) endpoint(t *testing.T, name string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.requests = append(c.requests, peeringRuntimeRequest{
			Endpoint: name, Form: r.PostForm, Signing: r.Header.Get("Authorization"),
		})
		id := r.PostForm.Get("VpcPeeringConnectionId")
		action := r.PostForm.Get("Action")
		w.Header().Set("Content-Type", "text/xml")
		switch action {
		case "CreateVpcPeeringConnection":
			c.created = true
			c.createdVpc = r.PostForm.Get("VpcId")
			c.createdPeer = r.PostForm.Get("PeerVpcId")
			fmt.Fprint(w, `<CreateVpcPeeringConnectionResponse><vpcPeeringConnection>`+
				`<vpcPeeringConnectionId>pcx-created</vpcPeeringConnectionId>`+
				`</vpcPeeringConnection></CreateVpcPeeringConnectionResponse>`)
		case "DeleteVpcPeeringConnection":
			if id != priorPeeringID {
				t.Errorf("delete addressed %q", id)
			}
			c.deleted = true
			fmt.Fprint(w, `<DeleteVpcPeeringConnectionResponse><return>true</return>`+
				`</DeleteVpcPeeringConnectionResponse>`)
		case "ModifyVpcPeeringConnectionOptions":
			c.drift = "true"
			fmt.Fprint(w, `<ModifyVpcPeeringConnectionOptionsResponse/>`)
		case "DescribeVpcPeeringConnections":
			id := r.PostForm.Get("VpcPeeringConnectionId.1")
			switch {
			case id == priorPeeringID && !c.deleted:
				fmt.Fprint(w, peeringRuntimeResponse(id, "vpc-one", "vpc-two", c.drift))
			case id == "pcx-created" && c.created:
				fmt.Fprint(w, peeringRuntimeResponse(id, c.createdVpc, c.createdPeer, ""))
			default:
				fmt.Fprint(w, `<DescribeVpcPeeringConnectionsResponse/>`)
			}
		default:
			t.Errorf("unexpected EC2 action %s", action)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func peeringRuntimeResponse(id, requester, accepter, dns string) string {
	options := ""
	if dns != "" {
		options = "<peeringOptions><allowDnsResolutionFromRemoteVpc>" + dns +
			"</allowDnsResolutionFromRemoteVpc></peeringOptions>"
	}
	return fmt.Sprintf(
		"<DescribeVpcPeeringConnectionsResponse><vpcPeeringConnectionSet><item>"+
			"<vpcPeeringConnectionId>%s</vpcPeeringConnectionId>"+
			"<requesterVpcInfo><vpcId>%s</vpcId><ownerId>123456789012</ownerId>"+
			"<region>us-east-1</region></requesterVpcInfo>"+
			"<accepterVpcInfo><vpcId>%s</vpcId><ownerId>123456789012</ownerId>"+
			"<region>us-west-2</region>%s</accepterVpcInfo>"+
			"<status><code>active</code></status></item></vpcPeeringConnectionSet>"+
			"</DescribeVpcPeeringConnectionsResponse>", id, requester, accepter, options,
	)
}

func (c *peeringRuntimeCloud) calls() []peeringRuntimeRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]peeringRuntimeRequest(nil), c.requests...)
}

func (c *peeringRuntimeCloud) mutations() []url.Values {
	var forms []url.Values
	for _, req := range c.calls() {
		if req.Form.Get("Action") != "DescribeVpcPeeringConnections" {
			forms = append(forms, req.Form)
		}
	}
	return forms
}

func peeringRuntimeInputs(t *testing.T, endpoint, region string) map[string]any {
	t.Helper()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFAKEFAKEFAKEFAKE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	return map[string]any{
		"aws-config":    map[string]any{"region": region, "endpoint-url": endpoint},
		"vpc-id":        "vpc-one",
		"peer-vpc-id":   "vpc-two",
		"peer-owner-id": nil,
		"peer-region":   nil,
		"auto-accept":   false,
		"accepter":      nil,
		"requester":     nil,
		"tags":          nil,
	}
}

func peeringRuntimeLibrary(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.LibraryPath = "github.com/cloudboss/unobin-library-aws//service/ec2"
	schema := readLibrarySchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.vpc-peering-connection": schema.Resources["vpc-peering-connection"].Constraints,
	}
	return map[string]*runtime.Library{
		"aws-ec2": lib,
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[peeringSeed, *peeringSeedOutput, runtime.NoConfig, *peeringSeed](),
		}},
	}
}

func peeringRuntimeExecutor(
	t *testing.T,
	fixture string,
	lib map[string]*runtime.Library,
	inputs map[string]any,
) *runtime.Executor {
	t.Helper()
	path := filepath.Join(peeringFixtures, fixture, "factory.ub")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := syntax.ParseSource(path, src)
	require.NoError(t, err)
	body := &parsed.Factory.Body
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: runtime.BuildSyntaxDAG(*body, lib), Libraries: lib, Inputs: inputs,
		SyntaxSource: body, Store: store,
		Factory: state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"},
	}
}

func peeringRuntimeSeedState(t *testing.T, exec *runtime.Executor, effective string) {
	t.Helper()
	inputs := maps.Clone(exec.Inputs)
	cfg := inputs["aws-config"].(map[string]any)
	delete(inputs, "aws-config")
	snapshot := state.NewSnapshot(exec.Factory, exec.Store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address: peeringAddress, Category: "resource", SchemaVersion: 1,
		Binding: &state.Binding{
			Alias: "aws-ec2", Export: "vpc-peering-connection",
			LibraryPath: "github.com/cloudboss/unobin-library-aws//service/ec2",
		},
		Inputs: inputs, Configuration: maps.Clone(cfg),
		Outputs: map[string]any{
			"vpc-peering-connection-id": priorPeeringID, "vpc-id": "vpc-one",
			"peer-vpc-id": "vpc-two", "peer-owner-id": "123456789012",
			"peer-region": "us-west-2", "region": effective, "accept-status": "active",
			"accepter": inputs["accepter"], "requester": nil, "tags": map[string]any{},
		},
	}}
	rev, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(rev))
}

func peeringRuntimeSavedPlan(t *testing.T, exec *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := exec.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func peeringRuntimeStep(t *testing.T, plan *runtime.PlanFile) *runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == peeringAddress {
			return &step
		}
	}
	t.Fatalf("missing %s in %+v", peeringAddress, plan.Steps)
	return nil
}

func peeringRuntimeIdentity(t *testing.T, exec *runtime.Executor) any {
	t.Helper()
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(peeringAddress)
	require.NotNil(t, entry)
	return entry.Outputs["vpc-peering-connection-id"]
}

func TestPeeringRuntimeChecksEvaluatedAndSavedNoOp(t *testing.T) {
	lib := peeringRuntimeLibrary(t)
	for _, tc := range []struct {
		name, field, diagnostic string
		value                   any
	}{
		{"guard", "peer-region", "constraints[2] (predicate): auto-accept requires peer-region",
			"us-west-2"},
		{"requester ID", "vpc-id", "constraints[0] (predicate): vpc-id must be a non-null string",
			nil},
		{"accepter ID", "peer-vpc-id",
			"constraints[1] (predicate): peer-vpc-id must be a non-null string", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &peeringRuntimeCloud{}
			inputs := peeringRuntimeInputs(t, cloud.endpoint(t, "prior"), "us-east-1")
			inputs["auto-accept"] = true
			exec := peeringRuntimeExecutor(t, "evaluated", lib, inputs)
			peeringRuntimeSeedState(t, exec, "us-east-1")
			valid := peeringRuntimeSavedPlan(t, exec)
			require.Equal(t, runtime.DecisionNoOp, peeringRuntimeStep(t, valid).Decision)
			_, err := exec.ApplyPlan(t.Context(), valid)
			require.NoError(t, err)

			saved := peeringRuntimeSavedPlan(t, exec)
			require.Equal(t, runtime.DecisionNoOp, peeringRuntimeStep(t, saved).Decision)
			exec.Inputs[tc.field] = tc.value
			_, err = exec.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, peeringAddress)
			require.ErrorContains(t, err, tc.diagnostic)
			assert.Equal(t, priorPeeringID, peeringRuntimeIdentity(t, exec))
			assert.Empty(t, cloud.mutations())

			peeringRuntimeSeedState(t, exec, "us-east-1")
			_, err = exec.Plan(t.Context())
			require.ErrorContains(t, err, peeringAddress)
			require.ErrorContains(t, err, tc.diagnostic)
			assert.Equal(t, priorPeeringID, peeringRuntimeIdentity(t, exec))
			assert.Empty(t, cloud.mutations())

			exec.Destroy = true
			destroy := peeringRuntimeSavedPlan(t, exec)
			require.Equal(t, runtime.DecisionDestroy, peeringRuntimeStep(t, destroy).Decision)
			_, err = exec.ApplyPlan(t.Context(), destroy)
			require.NoError(t, err)
			assert.Equal(t, []url.Values{{
				"Action": {"DeleteVpcPeeringConnection"}, "Version": {"2016-11-15"},
				"VpcPeeringConnectionId": {priorPeeringID},
			}}, cloud.mutations())
		})
	}
}

func TestPeeringRuntimeChecksDeferredReplacement(t *testing.T) {
	lib := peeringRuntimeLibrary(t)
	for _, tc := range []struct {
		name, field, diagnostic string
		value                   any
	}{
		{"guard", "peer-region", "constraints[2] (predicate): auto-accept requires peer-region",
			"us-west-2"},
		{"requester ID", "vpc-id", "constraints[0] (predicate): vpc-id must be a non-null string",
			nil},
		{"accepter ID", "peer-vpc-id",
			"constraints[1] (predicate): peer-vpc-id must be a non-null string", nil},
		{"valid deferred ID", "vpc-id", "", "vpc-three"},
		{"explicit empty ID", "vpc-id", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &peeringRuntimeCloud{}
			inputs := peeringRuntimeInputs(t, cloud.endpoint(t, "prior"), "us-east-1")
			inputs["auto-accept"] = true
			exec := peeringRuntimeExecutor(t, "deferred", lib, inputs)
			peeringRuntimeSeedState(t, exec, "us-east-1")
			exec.Inputs[tc.field] = tc.value
			saved := peeringRuntimeSavedPlan(t, exec)
			step := peeringRuntimeStep(t, saved)
			require.Equal(t, map[string][]string{
				"vpc-id": {"action.seed.vpc-id"}, "peer-vpc-id": {"action.seed.peer-vpc-id"},
				"peer-region": {"action.seed.peer-region"},
			}, step.UnresolvedInputs)
			assert.Equal(t, []string{"peer-region", "peer-vpc-id", "vpc-id"},
				step.PendingReplacementReasons)
			require.Contains(t, step.AllowedDecisions, runtime.DecisionReplace)
			_, err := exec.ApplyPlan(t.Context(), saved)
			if tc.diagnostic != "" {
				require.ErrorContains(t, err, peeringAddress)
				require.ErrorContains(t, err, tc.diagnostic)
				assert.Equal(t, priorPeeringID, peeringRuntimeIdentity(t, exec))
				assert.Empty(t, cloud.mutations())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "pcx-created", peeringRuntimeIdentity(t, exec))
			assert.Equal(t, []url.Values{
				{
					"Action": {"DeleteVpcPeeringConnection"}, "Version": {"2016-11-15"},
					"VpcPeeringConnectionId": {priorPeeringID},
				},
				{
					"Action": {"CreateVpcPeeringConnection"}, "Version": {"2016-11-15"},
					"VpcId": {tc.value.(string)}, "PeerVpcId": {"vpc-two"},
				},
			}, cloud.mutations())
		})
	}
}

func TestPeeringRuntimeReplacementAndRelinquishment(t *testing.T) {
	lib := peeringRuntimeLibrary(t)
	for _, tc := range []struct {
		name, field    string
		prior, desired any
		decision       runtime.Decision
		reasons        []string
	}{
		{"requester changes", "vpc-id", "vpc-one", "vpc-three", runtime.DecisionReplace,
			[]string{"vpc-id"}},
		{"accepter changes", "peer-vpc-id", "vpc-two", "vpc-three", runtime.DecisionReplace,
			[]string{"peer-vpc-id"}},
		{"change owner", "peer-owner-id", "123456789012", "210987654321", runtime.DecisionReplace,
			[]string{"peer-owner-id"}},
		{"change peer region", "peer-region", "us-west-2", "us-east-2", runtime.DecisionReplace,
			[]string{"peer-region"}},
		{"set observed owner", "peer-owner-id", nil, "123456789012", runtime.DecisionReplace,
			[]string{"peer-owner-id"}},
		{"set observed peer region", "peer-region", nil, "us-west-2", runtime.DecisionReplace,
			[]string{"peer-region"}},
		{"drop owner", "peer-owner-id", "123456789012", nil, runtime.DecisionUpdate, nil},
		{"empty peer region", "peer-region", "us-west-2", "", runtime.DecisionUpdate, nil},
		{"normalize empty peer region", "peer-region", "", nil, runtime.DecisionNoOp, nil},
		{"normalize empty owner", "peer-owner-id", nil, "", runtime.DecisionNoOp, nil},
		{"update accept", "auto-accept", false, true, runtime.DecisionUpdate, nil},
		{"update options", "accepter", nil, map[string]any{}, runtime.DecisionUpdate, nil},
		{"update tags", "tags", nil, map[string]any{"stage": "new"}, runtime.DecisionUpdate, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &peeringRuntimeCloud{}
			inputs := peeringRuntimeInputs(t, cloud.endpoint(t, "prior"), "us-east-1")
			inputs[tc.field] = tc.prior
			exec := peeringRuntimeExecutor(t, "evaluated", lib, inputs)
			peeringRuntimeSeedState(t, exec, "us-east-1")
			exec.Inputs[tc.field] = tc.desired
			plan := peeringRuntimeSavedPlan(t, exec)
			step := peeringRuntimeStep(t, plan)
			assert.Equal(t, tc.decision, step.Decision)
			assert.Equal(t, tc.reasons, step.ReplacementReasons)
			assert.Equal(t, priorPeeringID, step.ObservedOutputs["vpc-peering-connection-id"])
			assert.Equal(t, "us-west-2", step.ObservedOutputs["peer-region"])
			assert.Equal(t, "123456789012", step.ObservedOutputs["peer-owner-id"])
			if tc.decision == runtime.DecisionUpdate &&
				(tc.field == "peer-region" || tc.field == "peer-owner-id") {
				_, err := exec.ApplyPlan(t.Context(), plan)
				require.NoError(t, err)
				snapshot, err := exec.Store.Current()
				require.NoError(t, err)
				entry := snapshot.Find(peeringAddress)
				assert.Equal(t, tc.desired, entry.Inputs[tc.field])
				assert.Equal(t, priorPeeringID, entry.Outputs["vpc-peering-connection-id"])
				assert.Equal(t, "us-west-2", entry.Outputs["peer-region"])
				assert.Equal(t, "123456789012", entry.Outputs["peer-owner-id"])
			}
			assert.Empty(t, cloud.mutations())
		})
	}
}

func TestPeeringRuntimeKeepsPriorOperationRegionOnReplacement(t *testing.T) {
	lib := peeringRuntimeLibrary(t)
	for _, changeOperationRegion := range []bool{false, true} {
		name := "peer region"
		if changeOperationRegion {
			name = "operation region"
		}
		t.Run(name, func(t *testing.T) {
			cloud := &peeringRuntimeCloud{}
			inputs := peeringRuntimeInputs(t, cloud.endpoint(t, "prior"), "us-east-1")
			exec := peeringRuntimeExecutor(t, "evaluated", lib, inputs)
			peeringRuntimeSeedState(t, exec, "us-east-1")
			expectReason := "peer-region"
			expectRegion := "us-east-1"
			expectEndpoint := "prior"
			expectCreate := url.Values{
				"Action": {"CreateVpcPeeringConnection"}, "Version": {"2016-11-15"},
				"VpcId": {"vpc-one"}, "PeerVpcId": {"vpc-two"},
			}
			if changeOperationRegion {
				expectReason = "region"
				expectRegion = "us-east-2"
				expectEndpoint = "current"
				exec.Inputs["aws-config"] = map[string]any{
					"region": expectRegion, "endpoint-url": cloud.endpoint(t, expectEndpoint),
				}
			} else {
				exec.Inputs["peer-region"] = "us-west-2"
				expectCreate.Set("PeerRegion", "us-west-2")
			}
			plan := peeringRuntimeSavedPlan(t, exec)
			step := peeringRuntimeStep(t, plan)
			require.Equal(t, runtime.DecisionReplace, step.Decision)
			require.Equal(t, []string{expectReason}, step.ReplacementReasons)
			_, err := exec.ApplyPlan(t.Context(), plan)
			require.NoError(t, err)
			assert.Equal(t, "pcx-created", peeringRuntimeIdentity(t, exec))
			assert.Equal(t, []url.Values{
				{
					"Action": {"DeleteVpcPeeringConnection"}, "Version": {"2016-11-15"},
					"VpcPeeringConnectionId": {priorPeeringID},
				},
				expectCreate,
			}, cloud.mutations())
			for _, call := range cloud.calls() {
				isNew := call.Form.Get("Action") == "CreateVpcPeeringConnection" ||
					call.Form.Get("VpcPeeringConnectionId.1") == "pcx-created"
				if isNew {
					assert.Equal(t, expectEndpoint, call.Endpoint)
					assert.True(t, strings.Contains(call.Signing, "/"+expectRegion+"/ec2/"))
				} else {
					assert.Equal(t, "prior", call.Endpoint)
					assert.True(t, strings.Contains(call.Signing, "/us-east-1/ec2/"))
				}
			}
		})
	}
}

func TestPeeringRuntimeReconcilesObservedOptionsDrift(t *testing.T) {
	cloud := &peeringRuntimeCloud{drift: "false"}
	inputs := peeringRuntimeInputs(t, cloud.endpoint(t, "prior"), "us-east-1")
	wantDNS := map[string]any{"allow-remote-vpc-dns-resolution": true}
	inputs["accepter"] = wantDNS
	exec := peeringRuntimeExecutor(t, "evaluated", peeringRuntimeLibrary(t), inputs)
	peeringRuntimeSeedState(t, exec, "us-east-1")

	plan := peeringRuntimeSavedPlan(t, exec)
	step := peeringRuntimeStep(t, plan)
	require.Equal(t, runtime.DecisionUpdate, step.Decision)
	assert.Empty(t, step.ReplacementReasons)
	assert.Equal(t, wantDNS, step.PriorOutputs["accepter"])
	assert.Equal(t, map[string]any{"allow-remote-vpc-dns-resolution": false},
		step.ObservedOutputs["accepter"])
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, []url.Values{{
		"Action": {"ModifyVpcPeeringConnectionOptions"}, "Version": {"2016-11-15"},
		"VpcPeeringConnectionId": {priorPeeringID},
		"AccepterPeeringConnectionOptions.AllowDnsResolutionFromRemoteVpc": {"true"},
	}}, cloud.mutations())
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(peeringAddress)
	assert.Equal(t, wantDNS, entry.Outputs["accepter"])
	assert.Equal(t, priorPeeringID, entry.Outputs["vpc-peering-connection-id"])
}
