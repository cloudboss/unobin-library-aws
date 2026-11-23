package ec2

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type accepterFixtureCloud struct {
	mu                        sync.Mutex
	vpcs                      map[string]string
	tags                      map[string]string
	connection, accepted, dns bool
	fail                      string
	failAt                    int
	counts                    map[string]int
}

func (c *accepterFixtureCloud) endpoint(t *testing.T) string {
	t.Helper()
	c.vpcs, c.tags, c.counts = map[string]string{}, map[string]string{}, map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		c.mu.Lock()
		defer c.mu.Unlock()
		action := r.Form.Get("Action")
		c.counts[action]++
		w.Header().Set("Content-Type", "text/xml")
		if action == c.fail && c.counts[action] == c.failAt {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<Response><Errors><Error><Code>FixtureFailure</Code>`+
				`<Message>injected failure</Message></Error></Errors></Response>`)
			return
		}
		switch action {
		case "CreateVpc":
			id := "vpc-" + strings.ReplaceAll(r.Form.Get("CidrBlock"), ".", "-")
			c.vpcs[id] = r.Form.Get("CidrBlock")
			fmt.Fprintf(w, `<CreateVpcResponse><vpc><vpcId>%s</vpcId></vpc></CreateVpcResponse>`, id)
		case "DescribeVpcs":
			fmt.Fprint(w, `<DescribeVpcsResponse><vpcSet>`)
			for id, cidr := range c.vpcs {
				if want := r.Form.Get("VpcId.1"); want != "" && want != id {
					continue
				}
				fmt.Fprintf(w, `<item><vpcId>%s</vpcId><cidrBlock>%s</cidrBlock>`+
					`<state>available</state></item>`, id, cidr)
			}
			fmt.Fprint(w, `</vpcSet></DescribeVpcsResponse>`)
		case "DeleteVpc":
			if c.connection {
				t.Error("VPC deleted before peering connection")
			}
			delete(c.vpcs, r.Form.Get("VpcId"))
			fmt.Fprint(w, `<DeleteVpcResponse/>`)
		case "CreateVpcPeeringConnection":
			c.connection = true
			for n := 1; ; n++ {
				prefix := fmt.Sprintf("TagSpecification.1.Tag.%d.", n)
				key := r.Form.Get(prefix + "Key")
				if key == "" {
					break
				}
				c.tags[key] = r.Form.Get(prefix + "Value")
			}
			fmt.Fprint(w, `<CreateVpcPeeringConnectionResponse><vpcPeeringConnection>`+
				`<vpcPeeringConnectionId>pcx-fixture</vpcPeeringConnectionId>`+
				`</vpcPeeringConnection></CreateVpcPeeringConnectionResponse>`)
		case "DescribeVpcPeeringConnections":
			fmt.Fprint(w, `<DescribeVpcPeeringConnectionsResponse><vpcPeeringConnectionSet>`)
			if c.connection {
				status := "pending-acceptance"
				if c.accepted {
					status = "active"
				}
				fmt.Fprint(w, `<item><vpcPeeringConnectionId>pcx-fixture</vpcPeeringConnectionId>`)
				for _, side := range []struct{ name, cidr string }{
					{"requester", "10.65.0.0/16"}, {"accepter", "10.66.0.0/16"},
				} {
					fmt.Fprintf(w, `<%sVpcInfo><vpcId>vpc-%s</vpcId>`+
						`<ownerId>123456789012</ownerId><region>us-east-1</region>`+
						`<peeringOptions><allowDnsResolutionFromRemoteVpc>%t</allowDnsResolutionFromRemoteVpc>`+
						`</peeringOptions></%sVpcInfo>`, side.name,
						strings.ReplaceAll(side.cidr, ".", "-"), c.dns, side.name)
				}
				fmt.Fprintf(w, `<status><code>%s</code></status><tagSet>`, status)
				keys := make([]string, 0, len(c.tags))
				for key := range c.tags {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					fmt.Fprintf(w, `<item><key>%s</key><value>%s</value></item>`, key, c.tags[key])
				}
				fmt.Fprint(w, `</tagSet></item>`)
			}
			fmt.Fprint(w, `</vpcPeeringConnectionSet></DescribeVpcPeeringConnectionsResponse>`)
		case "AcceptVpcPeeringConnection":
			c.accepted = true
			fmt.Fprint(w, `<AcceptVpcPeeringConnectionResponse/>`)
		case "ModifyVpcPeeringConnectionOptions":
			c.dns = true
			fmt.Fprint(w, `<ModifyVpcPeeringConnectionOptionsResponse/>`)
		case "CreateTags", "DeleteTags":
			for n := 1; ; n++ {
				prefix := fmt.Sprintf("Tag.%d.", n)
				key := r.Form.Get(prefix + "Key")
				if key == "" {
					break
				}
				if action == "CreateTags" {
					c.tags[key] = r.Form.Get(prefix + "Value")
				} else {
					delete(c.tags, key)
				}
			}
			fmt.Fprintf(w, `<%sResponse/>`, action)
		case "DeleteVpcPeeringConnection":
			c.connection = false
			fmt.Fprint(w, `<DeleteVpcPeeringConnectionResponse/>`)
		default:
			t.Errorf("unexpected fixture action %s", action)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestPeeringAccepterManagedFixtureLifecycle(t *testing.T) {
	lib := peeringAccepterRuntimeLibrary(t)
	for _, tc := range []struct {
		name, fail string
		failAt     int
	}{
		{name: "survival"},
		{name: "partial VPC creation", fail: "CreateVpc", failAt: 2},
		{name: "requester creation", fail: "CreateVpcPeeringConnection", failAt: 1},
		{name: "acceptance", fail: "AcceptVpcPeeringConnection", failAt: 1},
		{name: "DNS update", fail: "ModifyVpcPeeringConnectionOptions", failAt: 1},
		{name: "adoption tags", fail: "CreateTags", failAt: 1},
		{name: "tag update", fail: "CreateTags", failAt: 2},
		{name: "plan failure"},
		{name: "cleanup failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &accepterFixtureCloud{fail: tc.fail, failAt: tc.failAt}
			inputs := peeringAccepterRuntimeInputs(t, cloud.endpoint(t))
			inputs = map[string]any{
				"aws-config": inputs["aws-config"], "accept": false, "stage": "initial",
				"adoptions": map[string]any{"adopted": true},
			}
			exec := peeringRuntimeExecutor(t, "../peering-accepter/evaluated", lib, inputs)
			path := "../../tests/integration/scenarios/ec2-vpc-peering-connection-accepter/factory.ub"
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			parsed, err := syntax.ParseSource(path, src)
			require.NoError(t, err)
			exec.SyntaxSource = &parsed.Factory.Body
			exec.DAG = runtime.BuildSyntaxDAG(parsed.Factory.Body, lib)
			if tc.name == "plan failure" {
				exec.Inputs["adoptions"] = nil
				_, err := exec.Plan(t.Context())
				require.Error(t, err)
				assert.False(t, cloud.connection)
				assert.Equal(t, map[string]string{}, cloud.vpcs)
				return
			}
			plan := peeringRuntimeSavedPlan(t, exec)
			_, applyErr := exec.ApplyPlan(t.Context(), plan)
			if tc.fail != "CreateVpc" && tc.fail != "CreateVpcPeeringConnection" &&
				tc.name != "adoption tags" {
				require.NoError(t, applyErr)
				assert.True(t, cloud.connection)
				assert.False(t, cloud.accepted)
				exec.Inputs["accept"], exec.Inputs["stage"] = true, "updated"
				_, applyErr = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
			}
			if tc.fail != "" {
				require.ErrorContains(t, applyErr, "FixtureFailure")
			} else {
				require.NoError(t, applyErr)
			}
			if tc.name == "survival" {
				snapshot, err := exec.Store.Current()
				require.NoError(t, err)
				require.NotNil(t, snapshot.Find("resource.connection['adopted']"))
				before := maps.Clone(cloud.tags)
				assert.Equal(t, map[string]string{
					"unobin": "vpc-peering-connection-accepter-it", "stage": "updated", "seed": "preserve",
				}, before)
				mutations := maps.Clone(cloud.counts)
				exec.Inputs["adoptions"] = map[string]any{}
				_, err = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
				require.NoError(t, err)
				assert.True(t, cloud.connection)
				assert.True(t, cloud.accepted)
				assert.True(t, cloud.dns)
				assert.Equal(t, before, cloud.tags)
				for action, count := range cloud.counts {
					if !strings.HasPrefix(action, "Describe") {
						assert.Equal(t, mutations[action], count, action)
					}
				}
				snapshot, err = exec.Store.Current()
				require.NoError(t, err)
				owned := map[string]any{}
				for _, entry := range snapshot.Entries {
					if entry.Address == "resource.peering" {
						owned[entry.Address] = entry.Outputs["vpc-peering-connection-id"]
					} else {
						owned[entry.Address] = entry.Outputs["vpc-id"]
					}
				}
				assert.Equal(t, map[string]any{
					"resource.requester": "vpc-10-65-0-0/16",
					"resource.accepter":  "vpc-10-66-0-0/16",
					"resource.peering":   "pcx-fixture",
				}, owned)
			}
			exec.Destroy = true
			if tc.name == "cleanup failure" {
				cloud.fail, cloud.failAt = "DeleteVpcPeeringConnection", 1
				_, err = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
				require.ErrorContains(t, err, "FixtureFailure")
				assert.True(t, cloud.connection)
				snapshot, err := exec.Store.Current()
				require.NoError(t, err)
				require.NotNil(t, snapshot.Find("resource.peering"))
			}
			_, err = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
			require.NoError(t, err)
			assert.False(t, cloud.connection)
			assert.Equal(t, map[string]string{}, cloud.vpcs)
			snapshot, err := exec.Store.Current()
			require.NoError(t, err)
			assert.Empty(t, snapshot.Entries)
		})
	}
}
