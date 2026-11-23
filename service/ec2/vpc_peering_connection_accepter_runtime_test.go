package ec2

import (
	"maps"
	"net/url"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func peeringAccepterRuntimeLibrary(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := peeringRuntimeLibrary(t)
	schema := readLibrarySchema(t).Resources["vpc-peering-connection-accepter"]
	lib["aws-ec2"].Constraints = map[string][]lang.ConstraintSpec{
		"resource.vpc-peering-connection-accepter": schema.Constraints,
	}
	return lib
}

func peeringAccepterRuntimeInputs(t *testing.T, endpoint string) map[string]any {
	t.Helper()
	inputs := peeringRuntimeInputs(t, endpoint, "us-east-1")
	for _, key := range []string{"vpc-id", "peer-vpc-id", "peer-owner-id", "peer-region"} {
		delete(inputs, key)
	}
	inputs["vpc-peering-connection-id"] = priorPeeringID
	return inputs
}

func peeringAccepterRuntimeSeed(t *testing.T, exec *runtime.Executor, region string) {
	t.Helper()
	inputs := maps.Clone(exec.Inputs)
	cfg := inputs["aws-config"].(map[string]any)
	delete(inputs, "aws-config")
	snapshot := state.NewSnapshot(exec.Factory, exec.Store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address: peeringAddress, Category: "resource", SchemaVersion: 1,
		Binding: &state.Binding{
			Alias: "aws-ec2", Export: "vpc-peering-connection-accepter",
			LibraryPath: "github.com/cloudboss/unobin-library-aws//service/ec2",
		},
		Inputs: inputs, Configuration: maps.Clone(cfg),
		Outputs: map[string]any{
			"vpc-peering-connection-id": priorPeeringID, "vpc-id": "vpc-one",
			"peer-vpc-id": "vpc-two", "peer-owner-id": "123456789012",
			"peer-region": "us-west-2", "region": region, "accept-status": "active",
			"accepter": inputs["accepter"], "requester": inputs["requester"],
			"tags": map[string]any{},
		},
	}}
	rev, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(rev))
}

const peeringAccepterIDDiagnostic = "constraints[0] (predicate): " +
	"vpc-peering-connection-id must be a non-null string"

func TestPeeringAccepterRuntimeEvaluatedAndSavedNoOp(t *testing.T) {
	lib := peeringAccepterRuntimeLibrary(t)
	cloud := &peeringRuntimeCloud{}
	inputs := peeringAccepterRuntimeInputs(t, cloud.endpoint(t, "prior"))
	exec := peeringRuntimeExecutor(t, "../peering-accepter/evaluated", lib, inputs)
	peeringAccepterRuntimeSeed(t, exec, "us-east-1")
	valid := peeringRuntimeSavedPlan(t, exec)
	require.Equal(t, runtime.DecisionNoOp, peeringRuntimeStep(t, valid).Decision)
	_, err := exec.ApplyPlan(t.Context(), valid)
	require.NoError(t, err)
	assert.Equal(t, priorPeeringID, peeringRuntimeIdentity(t, exec))
	assert.Empty(t, cloud.mutations())

	saved := peeringRuntimeSavedPlan(t, exec)
	exec.Inputs["vpc-peering-connection-id"] = nil
	_, err = exec.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, peeringAddress)
	require.ErrorContains(t, err, peeringAccepterIDDiagnostic)
	assert.Equal(t, priorPeeringID, peeringRuntimeIdentity(t, exec))
	assert.Empty(t, cloud.mutations())

	peeringAccepterRuntimeSeed(t, exec, "us-east-1")
	_, err = exec.Plan(t.Context())
	require.ErrorContains(t, err, peeringAddress)
	require.ErrorContains(t, err, peeringAccepterIDDiagnostic)
	assert.Equal(t, priorPeeringID, peeringRuntimeIdentity(t, exec))
	assert.Empty(t, cloud.mutations())

	exec.Destroy = true
	destroy := peeringRuntimeSavedPlan(t, exec)
	require.Equal(t, runtime.DecisionDestroy, peeringRuntimeStep(t, destroy).Decision)
	_, err = exec.ApplyPlan(t.Context(), destroy)
	require.NoError(t, err)
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(peeringAddress))
	assert.False(t, cloud.deleted)
	assert.Empty(t, cloud.mutations())
}

func TestPeeringAccepterRuntimeDeferredInputs(t *testing.T) {
	lib := peeringAccepterRuntimeLibrary(t)
	for _, tc := range []struct {
		name string
		id   any
	}{
		{"invalid", nil}, {"unchanged", priorPeeringID}, {"replacement", "pcx-created"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &peeringRuntimeCloud{created: true, createdVpc: "vpc-one", createdPeer: "vpc-two"}
			inputs := peeringAccepterRuntimeInputs(t, cloud.endpoint(t, "prior"))
			exec := peeringRuntimeExecutor(t, "../peering-accepter/deferred", lib, inputs)
			peeringAccepterRuntimeSeed(t, exec, "us-east-1")
			exec.Inputs["vpc-peering-connection-id"] = tc.id
			saved := peeringRuntimeSavedPlan(t, exec)
			step := peeringRuntimeStep(t, saved)
			assert.Equal(t, map[string][]string{
				"vpc-peering-connection-id": {"action.seed.vpc-id"},
			}, step.UnresolvedInputs)
			assert.Equal(t, []string{"vpc-peering-connection-id"}, step.PendingReplacementReasons)
			require.Contains(t, step.AllowedDecisions, runtime.DecisionReplace)
			_, err := exec.ApplyPlan(t.Context(), saved)
			if tc.id == nil {
				require.ErrorContains(t, err, peeringAddress)
				require.ErrorContains(t, err, peeringAccepterIDDiagnostic)
				assert.Equal(t, priorPeeringID, peeringRuntimeIdentity(t, exec))
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.id, peeringRuntimeIdentity(t, exec))
			}
			assert.Empty(t, cloud.mutations())
			assert.False(t, cloud.deleted)
		})
	}
}

func TestPeeringAccepterRuntimeReplacementUsesSavedIdentityAndRegion(t *testing.T) {
	lib := peeringAccepterRuntimeLibrary(t)
	for _, tc := range []struct {
		name       string
		id, region bool
	}{
		{name: "connection", id: true}, {name: "region", region: true},
		{name: "both", id: true, region: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &peeringRuntimeCloud{created: true, createdVpc: "vpc-one", createdPeer: "vpc-two"}
			priorEndpoint := cloud.endpoint(t, "prior")
			newEndpoint := cloud.endpoint(t, "desired")
			inputs := peeringAccepterRuntimeInputs(t, priorEndpoint)
			exec := peeringRuntimeExecutor(t, "../peering-accepter/evaluated", lib, inputs)
			peeringAccepterRuntimeSeed(t, exec, "us-east-2")
			wantID := priorPeeringID
			var reasons []string
			if tc.id {
				wantID = "pcx-created"
				reasons = append(reasons, "vpc-peering-connection-id")
			}
			desiredRegion := "us-east-1"
			if tc.region {
				desiredRegion = "us-west-2"
				reasons = append(reasons, "region")
			}
			exec.Inputs["vpc-peering-connection-id"] = wantID
			exec.Inputs["aws-config"] = map[string]any{
				"region": desiredRegion, "endpoint-url": newEndpoint,
			}
			saved := peeringRuntimeSavedPlan(t, exec)
			step := peeringRuntimeStep(t, saved)
			require.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.ElementsMatch(t, reasons, step.ReplacementReasons)
			assert.Equal(t, priorPeeringID, step.ObservedOutputs["vpc-peering-connection-id"])
			assert.Equal(t, "us-east-2", step.ObservedOutputs["region"])
			for _, req := range cloud.calls() {
				assert.Equal(t, "prior", req.Endpoint)
				assert.Equal(t, priorPeeringID, req.Form.Get("VpcPeeringConnectionId.1"))
				assert.Contains(t, req.Signing, "/us-east-2/ec2/")
			}
			_, err := exec.ApplyPlan(t.Context(), saved)
			require.NoError(t, err)
			assert.Equal(t, wantID, peeringRuntimeIdentity(t, exec))
			calls := cloud.calls()
			last := calls[len(calls)-1]
			assert.Equal(t, "desired", last.Endpoint)
			assert.Equal(t, wantID, last.Form.Get("VpcPeeringConnectionId.1"))
			assert.Contains(t, last.Signing, "/"+desiredRegion+"/ec2/")
			assert.Empty(t, cloud.mutations())
			assert.False(t, cloud.deleted)
		})
	}
}

func TestPeeringAccepterRuntimeReconcilesObservedOptionsDrift(t *testing.T) {
	cloud := &peeringRuntimeCloud{drift: "false"}
	inputs := peeringAccepterRuntimeInputs(t, cloud.endpoint(t, "prior"))
	wantDNS := map[string]any{"allow-remote-vpc-dns-resolution": true}
	inputs["accepter"] = wantDNS
	exec := peeringRuntimeExecutor(t, "../peering-accepter/evaluated",
		peeringAccepterRuntimeLibrary(t), inputs)
	peeringAccepterRuntimeSeed(t, exec, "us-east-1")
	plan := peeringRuntimeSavedPlan(t, exec)
	step := peeringRuntimeStep(t, plan)
	require.Equal(t, runtime.DecisionUpdate, step.Decision)
	assert.Empty(t, step.ReplacementReasons)
	assert.Equal(t, wantDNS, step.PriorInputs["accepter"])
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
