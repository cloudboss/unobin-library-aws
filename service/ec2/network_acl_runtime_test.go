package ec2

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type networkACLSeed struct {
	VpcId   *string           `ub:"vpc-id"`
	Ingress *[]map[string]any `ub:"ingress"`
	Egress  *[]map[string]any `ub:"egress"`
}

type networkACLSeedOutput struct {
	ResolvedVpc     *string           `ub:"resolved-vpc"`
	ResolvedIngress *[]map[string]any `ub:"resolved-ingress"`
	ResolvedEgress  *[]map[string]any `ub:"resolved-egress"`
}

func (s *networkACLSeed) Run(context.Context, runtime.NoConfig) (*networkACLSeedOutput, error) {
	return &networkACLSeedOutput{s.VpcId, s.Ingress, s.Egress}, nil
}

type networkACLCloud struct {
	mu           sync.Mutex
	requests     []url.Values
	exists       bool
	vpc          bool
	subnet       bool
	fail         string
	cleanupFails bool
	rules        []map[string]string
	associations map[string]string
}

func (c *networkACLCloud) endpoint(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		c.mu.Lock()
		defer c.mu.Unlock()
		c.requests = append(c.requests, r.PostForm)
		action := r.PostForm.Get("Action")
		w.Header().Set("Content-Type", "text/xml")
		if action == c.fail || action == "DeleteNetworkAcl" && c.cleanupFails {
			w.WriteHeader(400)
			fmt.Fprintf(w, "<Response><Errors><Error><Code>%sFailure</Code>"+
				"<Message>injected failure</Message></Error></Errors></Response>", action)
			return
		}
		switch action {
		case "CreateVpc":
			c.vpc = true
			fmt.Fprint(w, "<CreateVpcResponse><vpc><vpcId>vpc-one</vpcId>"+
				"<cidrBlock>10.70.0.0/16</cidrBlock></vpc></CreateVpcResponse>")
		case "DescribeVpcs":
			fmt.Fprint(w, "<DescribeVpcsResponse><vpcSet>")
			if c.vpc {
				fmt.Fprint(w, "<item><vpcId>vpc-one</vpcId><state>available</state>"+
					"<cidrBlock>10.70.0.0/16</cidrBlock></item>")
			}
			fmt.Fprint(w, "</vpcSet></DescribeVpcsResponse>")
		case "DeleteVpc":
			assert.False(t, c.exists)
			assert.False(t, c.subnet)
			c.vpc = false
			fmt.Fprint(w, "<DeleteVpcResponse/>")
		case "CreateSubnet":
			c.subnet = true
			fmt.Fprint(w, "<CreateSubnetResponse><subnet><subnetId>subnet-one</subnetId>"+
				"<vpcId>vpc-one</vpcId><state>available</state></subnet></CreateSubnetResponse>")
		case "DescribeSubnets":
			fmt.Fprint(w, "<DescribeSubnetsResponse><subnetSet>")
			if c.subnet {
				fmt.Fprint(w, "<item><subnetId>subnet-one</subnetId>"+
					"<vpcId>vpc-one</vpcId><state>available</state></item>")
			}
			fmt.Fprint(w, "</subnetSet></DescribeSubnetsResponse>")
		case "DeleteSubnet":
			assert.False(t, c.exists)
			c.subnet = false
			fmt.Fprint(w, "<DeleteSubnetResponse/>")
		case "CreateNetworkAcl":
			c.exists = true
			assert.NotEmpty(t, r.PostForm.Get("ClientToken"))
			fmt.Fprint(w, "<CreateNetworkAclResponse><networkAcl><networkAclId>acl-one"+
				"</networkAclId></networkAcl></CreateNetworkAclResponse>")
		case "DescribeNetworkAcls":
			fmt.Fprint(w, "<DescribeNetworkAclsResponse><networkAclSet>")
			id := "acl-one"
			if r.PostForm.Get("Filter.2.Name") == "default" {
				id = "acl-default"
			}
			if c.exists || id == "acl-default" {
				fmt.Fprintf(w, "<item><networkAclId>%s</networkAclId><vpcId>vpc-one</vpcId>"+
					"<ownerId>123456789012</ownerId><entrySet>", id)
				for _, rule := range c.rules {
					fmt.Fprintf(w, "<item><ruleNumber>%s</ruleNumber><ruleAction>%s</ruleAction>"+
						"<protocol>%s</protocol><egress>%s</egress><portRange>"+
						"<from>%s</from><to>%s</to></portRange></item>", rule["RuleNumber"],
						rule["RuleAction"], rule["Protocol"], rule["Egress"],
						rule["PortRange.From"], rule["PortRange.To"])
				}
				fmt.Fprint(w, "</entrySet><associationSet>")
				for subnet, association := range c.associations {
					fmt.Fprintf(w, "<item><subnetId>%s</subnetId><networkAclAssociationId>%s"+
						"</networkAclAssociationId></item>", subnet, association)
				}
				fmt.Fprint(w, "</associationSet></item>")
			}
			fmt.Fprint(w, "</networkAclSet></DescribeNetworkAclsResponse>")
		case "CreateNetworkAclEntry":
			rule := map[string]string{}
			for key := range r.PostForm {
				rule[key] = r.PostForm.Get(key)
			}
			c.rules = append(c.rules, rule)
			fmt.Fprint(w, "<CreateNetworkAclEntryResponse/>")
		case "DeleteNetworkAclEntry":
			kept := c.rules[:0]
			for _, rule := range c.rules {
				if rule["RuleNumber"] != r.PostForm.Get("RuleNumber") ||
					rule["Egress"] != r.PostForm.Get("Egress") {
					kept = append(kept, rule)
				}
			}
			c.rules = kept
			fmt.Fprint(w, "<DeleteNetworkAclEntryResponse/>")
		case "ReplaceNetworkAclAssociation":
			if r.PostForm.Get("NetworkAclId") == "acl-default" {
				for subnet, association := range c.associations {
					if association == r.PostForm.Get("AssociationId") {
						delete(c.associations, subnet)
					}
				}
			}
			fmt.Fprint(w, "<ReplaceNetworkAclAssociationResponse/>")
		case "DeleteNetworkAcl":
			assert.Empty(t, c.associations)
			c.exists = false
			fmt.Fprint(w, "<DeleteNetworkAclResponse/>")
		default:
			t.Errorf("unexpected network ACL API action %s", action)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestNetworkACLNativeFailedCreateLeavesOwnedDependenciesDestroyable(t *testing.T) {
	libraries := networkACLRuntimeLibraries(t)
	for _, cleanupFails := range []bool{false, true} {
		t.Run(fmt.Sprint(cleanupFails), func(t *testing.T) {
			cloud := &networkACLCloud{fail: "CreateNetworkAclEntry", cleanupFails: cleanupFails}
			endpoint := cloud.endpoint(t)
			exec := peeringRuntimeExecutor(t, "../network-acl/owned", libraries,
				map[string]any{"aws-config": networkACLInputs(t, endpoint)["aws-config"]})
			var captured string
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
				defer cancel()
				cloud.fail, cloud.cleanupFails = "", false
				if captured != "" && cloud.exists {
					cfg, err := awscfg.Load(ctx, &awscfg.Configuration{
						Region: aws.String("us-east-1"), EndpointURL: &endpoint})
					require.NoError(t, err)
					_, err = ec2.NewFromConfig(cfg).DeleteNetworkAcl(ctx,
						&ec2.DeleteNetworkAclInput{NetworkAclId: &captured})
					require.NoError(t, err)
				}
				exec.Destroy = true
				plan, err := exec.Plan(ctx)
				require.NoError(t, err)
				encoded, err := runtime.EncodePlan(plan)
				require.NoError(t, err)
				saved, err := runtime.DecodePlan(encoded)
				require.NoError(t, err)
				_, err = exec.ApplyPlan(ctx, saved)
				require.NoError(t, err)
				assert.False(t, cloud.exists)
				assert.False(t, cloud.subnet)
				assert.False(t, cloud.vpc)
			})
			_, err := exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
			require.ErrorContains(t, err, "CreateNetworkAclEntryFailure")
			captured = "acl-one"
			if cleanupFails {
				require.ErrorContains(t, err, "compensating delete network ACL acl-one")
			}
			assert.Equal(t, cleanupFails, cloud.exists)
			assert.True(t, cloud.subnet)
			assert.True(t, cloud.vpc)
			snapshot, err := exec.Store.Current()
			require.NoError(t, err)
			var addresses []string
			for _, entry := range snapshot.Entries {
				addresses = append(addresses, entry.Address)
			}
			assert.ElementsMatch(t, []string{"resource.vpc", "resource.subnet"}, addresses)
			assert.Nil(t, snapshot.Find(peeringAddress))
		})
	}
}

func (c *networkACLCloud) mutations() []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	var mutations []url.Values
	for _, req := range c.requests {
		if !strings.HasPrefix(req.Get("Action"), "Describe") {
			copy := maps.Clone(req)
			copy.Del("ClientToken")
			mutations = append(mutations, copy)
		}
	}
	return mutations
}

func networkACLRuntimeLibraries(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.LibraryPath = "github.com/cloudboss/unobin-library-aws//service/ec2"
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.network-acl": readLibrarySchema(t).Resources["network-acl"].Constraints,
	}
	return map[string]*runtime.Library{"aws-ec2": lib,
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[networkACLSeed, *networkACLSeedOutput,
				runtime.NoConfig, *networkACLSeed](),
		}},
	}
}

func networkACLInputs(t *testing.T, endpoint string) map[string]any {
	t.Helper()
	inputs := peeringRuntimeInputs(t, endpoint, "us-east-1")
	return map[string]any{"aws-config": inputs["aws-config"], "vpc-id": "vpc-one",
		"ingress": nil, "egress": nil, "subnet-ids": nil, "tags": nil}
}

func seedNetworkACL(t *testing.T, exec *runtime.Executor) {
	t.Helper()
	inputs := maps.Clone(exec.Inputs)
	config := inputs["aws-config"].(map[string]any)
	delete(inputs, "aws-config")
	snapshot := state.NewSnapshot(exec.Factory, exec.Store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address: peeringAddress, Category: "resource", SchemaVersion: 1,
		Binding: &state.Binding{Alias: "aws-ec2", Export: "network-acl",
			LibraryPath: "github.com/cloudboss/unobin-library-aws//service/ec2"},
		Inputs: inputs, Configuration: config, Outputs: map[string]any{
			"network-acl-id": "acl-one", "vpc-id": "vpc-one", "owner-id": "123456789012",
			"arn":     "arn:aws:ec2:us-east-1:123456789012:network-acl/acl-one",
			"ingress": []any{}, "egress": []any{}, "subnet-ids": []any{}, "tags": map[string]any{},
		},
	}}
	revision, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(revision))
}

func networkACLRuleInput() map[string]any {
	return map[string]any{"rule-action": "allow", "protocol": "tcp", "rule-number": int64(10),
		"from-port": int64(0), "to-port": int64(80), "cidr-block": nil,
		"ipv6-cidr-block": nil, "icmp-type": nil, "icmp-code": nil}
}

func TestNetworkACLNativeEvaluatedSavedAndHistoricalDestroy(t *testing.T) {
	libraries := networkACLRuntimeLibraries(t)
	for _, direction := range []string{"ingress", "egress"} {
		for _, tc := range []struct {
			field      string
			value      any
			diagnostic string
		}{
			{"rule-action", nil, "fields must be non-null"},
			{"protocol", nil, "fields must be non-null"},
			{"rule-number", nil, "fields must be non-null"},
			{"from-port", nil, "fields must be non-null"},
			{"to-port", nil, "fields must be non-null"},
			{"rule-action", "accept", "rule-action must be allow or deny"},
			{"rule-number", int64(0), "outside its bounds"},
			{"rule-number", int64(32767), "outside its bounds"},
			{"from-port", int64(-1), "outside its bounds"},
			{"from-port", int64(65536), "outside its bounds"},
			{"to-port", int64(-1), "outside its bounds"},
			{"to-port", int64(65536), "outside its bounds"},
			{"protocol", "all", "all protocols require zero ports"},
			{"protocol", "-1", "all protocols require zero ports"},
			{"protocol", "-01", "all protocols require zero ports"},
		} {
			t.Run(fmt.Sprintf("%s/%s/%v", direction, tc.field, tc.value), func(t *testing.T) {
				cloud := &networkACLCloud{exists: true}
				inputs := networkACLInputs(t, cloud.endpoint(t))
				exec := peeringRuntimeExecutor(t, "../network-acl/evaluated", libraries, inputs)
				seedNetworkACL(t, exec)
				saved := peeringRuntimeSavedPlan(t, exec)
				assert.Equal(t, runtime.DecisionNoOp, peeringRuntimeStep(t, saved).Decision)
				_, err := exec.ApplyPlan(t.Context(), saved)
				require.NoError(t, err)
				saved = peeringRuntimeSavedPlan(t, exec)
				rule := networkACLRuleInput()
				rule[tc.field] = tc.value
				exec.Inputs[direction] = []any{rule}
				_, err = exec.ApplyPlan(t.Context(), saved)
				require.ErrorContains(t, err, tc.diagnostic)
				require.ErrorContains(t, err, "constraints[")
				require.ErrorContains(t, err, peeringAddress)
				seedNetworkACL(t, exec)
				_, err = exec.Plan(t.Context())
				require.ErrorContains(t, err, tc.diagnostic)
				assert.Empty(t, cloud.mutations())
				exec.Destroy = true
				_, err = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
				require.NoError(t, err)
				assert.Equal(t, []url.Values{{"Action": {"DeleteNetworkAcl"},
					"Version": {"2016-11-15"}, "NetworkAclId": {"acl-one"}}}, cloud.mutations())
			})
		}
	}
}

func TestNetworkACLNativeDeferredCreateUpdateReplacement(t *testing.T) {
	libraries := networkACLRuntimeLibraries(t)
	for _, mode := range []string{"create", "update", "replace"} {
		for _, tc := range []struct {
			name       string
			rule       map[string]any
			diagnostic string
		}{
			{"native", map[string]any{"rule-action": "bad"}, "rule-action must be allow or deny"},
			{"null", map[string]any{"from-port": nil}, "fields must be non-null"},
			{"bounds", map[string]any{"rule-number": int64(0)}, "outside its bounds"},
			{"all", map[string]any{"protocol": "-01"}, "all protocols require zero ports"},
			{"protocol", map[string]any{"protocol": "57"}, "invalid network ACL protocol"},
			{"CIDR", map[string]any{"cidr-block": "10.0.0.1/8"}, "invalid network CIDR"},
			{"valid", map[string]any{}, ""},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				cloud := &networkACLCloud{exists: mode != "create"}
				inputs := networkACLInputs(t, cloud.endpoint(t))
				exec := peeringRuntimeExecutor(t, "../network-acl/deferred", libraries, inputs)
				if mode != "create" {
					seedNetworkACL(t, exec)
				}
				if mode == "replace" {
					inputs["vpc-id"] = "vpc-two"
				}
				rule := networkACLRuleInput()
				maps.Copy(rule, tc.rule)
				inputs["ingress"] = []any{rule}
				saved := peeringRuntimeSavedPlan(t, exec)
				assert.Equal(t, map[string][]string{"vpc-id": {"action.seed.resolved-vpc"},
					"ingress": {"action.seed.resolved-ingress"}, "egress": {"action.seed.resolved-egress"}},
					peeringRuntimeStep(t, saved).UnresolvedInputs)
				_, err := exec.ApplyPlan(t.Context(), saved)
				if tc.diagnostic != "" {
					require.ErrorContains(t, err, tc.diagnostic)
					assert.Empty(t, cloud.mutations())
					return
				}
				require.NoError(t, err)
				want := []string{"CreateNetworkAcl", "CreateNetworkAclEntry"}
				if mode == "update" {
					want = []string{"CreateNetworkAclEntry"}
				}
				if mode == "replace" {
					want = append([]string{"DeleteNetworkAcl"}, want...)
				}
				var actions []string
				for _, form := range cloud.mutations() {
					actions = append(actions, form.Get("Action"))
				}
				assert.Equal(t, want, actions)
			})
		}
	}
}

func TestNetworkACLNativeReplacementUsesSavedConfigurationAndIdentity(t *testing.T) {
	libraries := networkACLRuntimeLibraries(t)
	oldCloud, newCloud := &networkACLCloud{exists: true}, &networkACLCloud{}
	inputs := networkACLInputs(t, oldCloud.endpoint(t))
	exec := peeringRuntimeExecutor(t, "../network-acl/evaluated", libraries, inputs)
	seedNetworkACL(t, exec)
	inputs["aws-config"] = map[string]any{"region": "us-west-2", "endpoint-url": newCloud.endpoint(t)}
	inputs["vpc-id"] = "vpc-two"
	plan := peeringRuntimeSavedPlan(t, exec)
	assert.Equal(t, runtime.DecisionReplace, peeringRuntimeStep(t, plan).Decision)
	assert.Equal(t, []string{"vpc-id"}, peeringRuntimeStep(t, plan).ReplacementReasons)
	_, err := exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, []url.Values{{"Action": {"DeleteNetworkAcl"},
		"Version": {"2016-11-15"}, "NetworkAclId": {"acl-one"}}}, oldCloud.mutations())
	assert.Equal(t, []url.Values{{"Action": {"CreateNetworkAcl"},
		"Version": {"2016-11-15"}, "VpcId": {"vpc-two"}}}, newCloud.mutations())
}

func TestNetworkACLNativeCIDRPreflight(t *testing.T) {
	libraries := networkACLRuntimeLibraries(t)
	for _, fixture := range []string{"evaluated", "deferred"} {
		for _, mode := range []string{"create", "replace"} {
			for _, direction := range []string{"ingress", "egress"} {
				for _, tc := range []struct {
					name, field, value, apiField string
					valid                        bool
				}{
					{"IPv4 prefix zero", "cidr-block", "10.0.0.0/08", "CidrBlock", true},
					{"IPv6 prefix zero", "ipv6-cidr-block", "2001:db8::/064", "Ipv6CidrBlock", true},
					{"mapped IPv4", "cidr-block", "::ffff:192.0.2.0/120", "CidrBlock", true},
					{"mapped IPv6", "ipv6-cidr-block", "::ffff:192.0.2.0/120", "", false},
					{"IPv6 in IPv4", "cidr-block", "2001:db8::/64", "", false},
					{"IPv4 in IPv6", "ipv6-cidr-block", "10.0.0.0/8", "", false},
					{"IPv4 host bits", "cidr-block", "10.0.0.1/08", "", false},
					{"IPv6 host bits", "ipv6-cidr-block", "2001:db8::1/064", "", false},
					{"mapped host bits", "cidr-block", "::ffff:192.0.2.1/120", "", false},
				} {
					name := fixture + "/" + mode + "/" + direction + "/" + tc.name
					t.Run(name, func(t *testing.T) {
						cloud := &networkACLCloud{exists: mode == "replace"}
						inputs := networkACLInputs(t, cloud.endpoint(t))
						exec := peeringRuntimeExecutor(t, "../network-acl/"+fixture, libraries, inputs)
						if mode == "replace" {
							seedNetworkACL(t, exec)
							inputs["vpc-id"] = "vpc-two"
						}
						rule := networkACLRuleInput()
						rule[tc.field] = tc.value
						inputs[direction] = []any{rule}
						saved := peeringRuntimeSavedPlan(t, exec)
						if fixture == "deferred" {
							assert.Equal(t, map[string][]string{
								"vpc-id":  {"action.seed.resolved-vpc"},
								"ingress": {"action.seed.resolved-ingress"},
								"egress":  {"action.seed.resolved-egress"},
							}, peeringRuntimeStep(t, saved).UnresolvedInputs)
						} else {
							decision := runtime.DecisionCreate
							if mode == "replace" {
								decision = runtime.DecisionReplace
							}
							assert.Equal(t, decision, peeringRuntimeStep(t, saved).Decision)
						}
						_, err := exec.ApplyPlan(t.Context(), saved)
						if !tc.valid {
							require.ErrorContains(t, err, direction+" rule 10: invalid network CIDR")
							require.ErrorContains(t, err, peeringAddress)
							assert.Empty(t, cloud.mutations())
							assert.Equal(t, mode == "replace", cloud.exists)
							return
						}
						require.NoError(t, err)
						entry := url.Values{
							"Action": {"CreateNetworkAclEntry"}, "Version": {"2016-11-15"},
							"NetworkAclId": {"acl-one"}, "Egress": {"false"},
							"RuleAction": {"allow"}, "RuleNumber": {"10"}, "Protocol": {"6"},
							"PortRange.From": {"0"}, "PortRange.To": {"80"},
							tc.apiField: {tc.value},
						}
						if direction == "egress" {
							entry.Set("Egress", "true")
						}
						want := []url.Values{{"Action": {"CreateNetworkAcl"},
							"Version": {"2016-11-15"}, "VpcId": {inputs["vpc-id"].(string)}}, entry}
						if mode == "replace" {
							want = append([]url.Values{{"Action": {"DeleteNetworkAcl"},
								"Version": {"2016-11-15"}, "NetworkAclId": {"acl-one"}}}, want...)
						}
						assert.Equal(t, want, cloud.mutations())
					})
				}
			}
		}
	}
}

func TestNetworkACLNativeVpcNullBeforeReplacementOrSavedApply(t *testing.T) {
	libraries := networkACLRuntimeLibraries(t)
	for _, fixture := range []string{"evaluated", "deferred"} {
		t.Run(fixture, func(t *testing.T) {
			cloud := &networkACLCloud{exists: true}
			inputs := networkACLInputs(t, cloud.endpoint(t))
			exec := peeringRuntimeExecutor(t, "../network-acl/"+fixture, libraries, inputs)
			seedNetworkACL(t, exec)
			saved := peeringRuntimeSavedPlan(t, exec)
			inputs["vpc-id"] = nil
			if fixture == "deferred" {
				saved = peeringRuntimeSavedPlan(t, exec)
			}
			_, err := exec.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, "vpc-id must be a non-null string")
			require.ErrorContains(t, err, "constraints[0]")
			if fixture == "evaluated" {
				_, err = exec.Plan(t.Context())
				require.ErrorContains(t, err, "vpc-id must be a non-null string")
			}
			assert.Empty(t, cloud.mutations())
		})
	}
}

func TestNetworkACLNativeUnchangedDesiredInputsRepairObservedRuleDrift(t *testing.T) {
	libraries := networkACLRuntimeLibraries(t)
	cloud := &networkACLCloud{exists: true, rules: []map[string]string{{
		"RuleNumber": "10", "RuleAction": "deny", "Protocol": "6", "Egress": "false",
		"PortRange.From": "0", "PortRange.To": "80",
	}}}
	inputs := networkACLInputs(t, cloud.endpoint(t))
	inputs["ingress"] = []any{networkACLRuleInput()}
	exec := peeringRuntimeExecutor(t, "../network-acl/evaluated", libraries, inputs)
	seedNetworkACL(t, exec)
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	savedRule := networkACLRuleInput()
	savedRule["protocol"], savedRule["icmp-type"], savedRule["icmp-code"] = "6", int64(0), int64(0)
	snapshot.Find(peeringAddress).Outputs["ingress"] = []any{savedRule}
	revision, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(revision))
	plan := peeringRuntimeSavedPlan(t, exec)
	assert.Equal(t, runtime.DecisionUpdate, peeringRuntimeStep(t, plan).Decision)
	_, err = exec.ApplyPlan(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, []url.Values{
		{"Action": {"DeleteNetworkAclEntry"}, "Version": {"2016-11-15"},
			"NetworkAclId": {"acl-one"}, "Egress": {"false"}, "RuleNumber": {"10"}},
		{"Action": {"CreateNetworkAclEntry"}, "Version": {"2016-11-15"},
			"NetworkAclId": {"acl-one"}, "Egress": {"false"}, "RuleNumber": {"10"},
			"RuleAction": {"allow"}, "Protocol": {"6"},
			"PortRange.From": {"0"}, "PortRange.To": {"80"}},
	}, cloud.mutations())
}
