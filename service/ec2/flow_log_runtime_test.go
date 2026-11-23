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

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/ec2"
)

type flowLogSeed struct {
	Value           *string
	Traffic         *string
	Aggregate       *int64
	Format          *string
	Destination     *string
	DestinationType *string `ub:"destination-type"`
	Options         *svc.FlowLogDestinationOptions
}

type flowLogSeedOutput struct {
	ResolvedVpc         *string
	ResolvedTraffic     *string
	ResolvedAggregate   *int64
	ResolvedFormat      *string
	ResolvedDestination *string
	ResolvedType        *string
	ResolvedOptions     *svc.FlowLogDestinationOptions
}

func (s *flowLogSeed) Run(context.Context, runtime.NoConfig) (*flowLogSeedOutput, error) {
	return &flowLogSeedOutput{
		ResolvedVpc: s.Value, ResolvedTraffic: s.Traffic, ResolvedAggregate: s.Aggregate,
		ResolvedFormat: s.Format, ResolvedDestination: s.Destination,
		ResolvedType: s.DestinationType, ResolvedOptions: s.Options,
	}, nil
}

type flowLogCloud struct {
	mu              sync.Mutex
	requests        []peeringRuntimeRequest
	exists          bool
	vpc             bool
	bucket          bool
	tags            map[string]string
	createdResource string
	fail            string
	failAfterCreate string
	failCleanup     bool
}

func (c *flowLogCloud) endpoint(t *testing.T, name string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/unobin-it-ec2-flow-log") {
			c.serveBucket(t, w, r)
			return
		}
		require.NoError(t, r.ParseForm())
		c.mu.Lock()
		defer c.mu.Unlock()
		c.requests = append(c.requests, peeringRuntimeRequest{
			Endpoint: name, Form: r.PostForm, Signing: r.Header.Get("Authorization")})
		w.Header().Set("Content-Type", "text/xml")
		action := r.PostForm.Get("Action")
		if c.fail == action || (c.exists && c.failAfterCreate == action) ||
			(action == "DeleteFlowLogs" && c.failCleanup) {
			w.WriteHeader(http.StatusBadRequest)
			if action == "GetCallerIdentity" {
				fmt.Fprint(w, `<ErrorResponse><Error><Code>FixtureFailure</Code>`+
					`<Message>injected failure</Message></Error></ErrorResponse>`)
				return
			}
			fmt.Fprint(w, `<Response><Errors><Error><Code>FixtureFailure</Code>`+
				`<Message>injected failure</Message></Error></Errors></Response>`)
			return
		}
		switch action {
		case "CreateVpc":
			c.vpc = true
			fmt.Fprint(w, `<CreateVpcResponse><vpc><vpcId>vpc-fixture</vpcId>`+
				`<cidrBlock>10.67.0.0/16</cidrBlock></vpc></CreateVpcResponse>`)
		case "DescribeVpcs":
			fmt.Fprint(w, `<DescribeVpcsResponse><vpcSet>`)
			if c.vpc {
				fmt.Fprint(w, `<item><vpcId>vpc-fixture</vpcId><cidrBlock>10.67.0.0/16</cidrBlock>`+
					`<state>available</state></item>`)
			}
			fmt.Fprint(w, `</vpcSet></DescribeVpcsResponse>`)
		case "DeleteVpc":
			if c.exists {
				t.Error("VPC deleted before flow log")
			}
			c.vpc = false
			fmt.Fprint(w, `<DeleteVpcResponse/>`)
		case "GetCallerIdentity":
			fmt.Fprint(w, `<GetCallerIdentityResponse><GetCallerIdentityResult>`+
				`<Account>123456789012</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`)
		case "DescribeFlowLogs":
			fmt.Fprint(w, `<DescribeFlowLogsResponse><flowLogSet>`)
			if c.exists {
				id := r.PostForm.Get("FlowLogId.1")
				resource := c.createdResource
				if resource == "" {
					resource = "vpc-one"
				}
				fmt.Fprintf(w, `<item><flowLogId>%s</flowLogId><resourceId>%s</resourceId>`+
					`<logDestinationType>s3</logDestinationType>`+
					`<logDestination>arn:aws:s3:::computed</logDestination>`+
					`<logFormat>computed</logFormat><maxAggregationInterval>600</maxAggregationInterval>`+
					`<tagSet>`, id, resource)
				for key, value := range c.tags {
					fmt.Fprintf(w, `<item><key>%s</key><value>%s</value></item>`, key, value)
				}
				fmt.Fprint(w, `</tagSet></item>`)
			}
			fmt.Fprint(w, `</flowLogSet></DescribeFlowLogsResponse>`)
		case "CreateFlowLogs":
			assert.NotEmpty(t, r.PostForm.Get("ClientToken"))
			c.exists = true
			c.createdResource = r.PostForm.Get("ResourceId.1")
			c.tags = flowLogFormTags(r.PostForm, "TagSpecification.1.Tag.")
			fmt.Fprint(w, `<CreateFlowLogsResponse><flowLogIdSet><item>fl-created</item>`+
				`</flowLogIdSet></CreateFlowLogsResponse>`)
		case "DeleteFlowLogs":
			c.exists = false
			fmt.Fprint(w, `<DeleteFlowLogsResponse/>`)
		case "CreateTags", "DeleteTags":
			if c.tags == nil {
				c.tags = map[string]string{}
			}
			for key, value := range flowLogFormTags(r.PostForm, "Tag.") {
				if action == "CreateTags" {
					c.tags[key] = value
				} else {
					delete(c.tags, key)
				}
			}
			fmt.Fprintf(w, `<%sResponse/>`, action)
		default:
			t.Errorf("unexpected flow log action %s", action)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func flowLogFormTags(form url.Values, prefix string) map[string]string {
	tags := map[string]string{}
	for i := 1; ; i++ {
		key := form.Get(fmt.Sprintf("%s%d.Key", prefix, i))
		if key == "" {
			return tags
		}
		tags[key] = form.Get(fmt.Sprintf("%s%d.Value", prefix, i))
	}
}

func (c *flowLogCloud) mutations() []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	var mutations []url.Values
	for _, req := range c.requests {
		action := req.Form.Get("Action")
		if !strings.HasPrefix(action, "Describe") && action != "GetCallerIdentity" {
			form := maps.Clone(req.Form)
			form.Del("ClientToken")
			mutations = append(mutations, form)
		}
	}
	return mutations
}

func flowLogRuntimeLibrary(t *testing.T) map[string]*runtime.Library {
	t.Helper()
	lib := Library()
	lib.LibraryPath = "github.com/cloudboss/unobin-library-aws//service/ec2"
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"resource.flow-log": readLibrarySchema(t).Resources["flow-log"].Constraints,
	}
	return map[string]*runtime.Library{
		"aws-ec2": lib,
		"fixture": {Actions: map[string]runtime.ActionRegistration{
			"seed": runtime.MakeAction[flowLogSeed, *flowLogSeedOutput, runtime.NoConfig, *flowLogSeed](),
		}},
	}
}

func flowLogInputs(t *testing.T, endpoint string) map[string]any {
	t.Helper()
	inputs := peeringRuntimeInputs(t, endpoint, "us-east-1")
	for key := range inputs {
		if key != "aws-config" {
			delete(inputs, key)
		}
	}
	for _, key := range []string{"eni-id", "regional-nat-gateway-id", "subnet-id", "vpc-id",
		"transit-gateway-id", "transit-gateway-attachment-id", "deliver-cross-account-role",
		"iam-role-arn", "log-destination", "log-destination-type", "log-format", "traffic-type",
		"max-aggregation-interval", "destination-options", "tags"} {
		inputs[key] = nil
	}
	inputs["vpc-id"] = "vpc-one"
	return inputs
}

func seedFlowLog(t *testing.T, exec *runtime.Executor) {
	t.Helper()
	inputs := maps.Clone(exec.Inputs)
	config := inputs["aws-config"].(map[string]any)
	delete(inputs, "aws-config")
	snapshot := state.NewSnapshot(exec.Factory, exec.Store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address: peeringAddress, Category: "resource", SchemaVersion: 1,
		Binding: &state.Binding{Alias: "aws-ec2", Export: "flow-log",
			LibraryPath: "github.com/cloudboss/unobin-library-aws//service/ec2"},
		Inputs: inputs, Configuration: maps.Clone(config), Outputs: map[string]any{
			"flow-log-id": "fl-prior", "region": "us-east-1", "resource-id": "vpc-one",
			"tags": map[string]any{}, "log-destination": "arn:aws:s3:::computed", "log-format": "computed",
			"arn":                  "arn:aws:ec2:us-east-1:123456789012:vpc-flow-log/fl-prior",
			"log-destination-type": "s3", "max-aggregation-interval": int64(600),
			"deliver-cross-account-role": nil, "deliver-logs-permission-arn": nil,
			"destination-options": nil, "traffic-type": "",
		},
	}}
	revision, err := exec.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, exec.Store.SetCurrent(revision))
}

func TestFlowLogNativeEvaluatedAndSavedNoOp(t *testing.T) {
	libraries := flowLogRuntimeLibrary(t)
	for _, tc := range []struct {
		name, field, diagnostic string
		value                   any
	}{
		{"zero", "vpc-id", "constraints[0]", nil},
		{"multiple", "subnet-id", "constraints[0]", "subnet-one"},
		{"destination", "log-destination-type", "constraints[1]", "invalid"},
		{"aggregation", "max-aggregation-interval", "constraints[2]", int64(61)},
		{"traffic", "traffic-type", "constraints[3]", "invalid"},
		{"options", "destination-options", "constraints[4]", map[string]any{"file-format": "bad"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &flowLogCloud{exists: true}
			exec := peeringRuntimeExecutor(t, "../flow-log/evaluated", libraries,
				flowLogInputs(t, cloud.endpoint(t, "prior")))
			seedFlowLog(t, exec)
			valid := peeringRuntimeSavedPlan(t, exec)
			require.Equal(t, runtime.DecisionNoOp, peeringRuntimeStep(t, valid).Decision)
			_, err := exec.ApplyPlan(t.Context(), valid)
			require.NoError(t, err)
			saved := peeringRuntimeSavedPlan(t, exec)
			exec.Inputs[tc.field] = tc.value
			_, err = exec.ApplyPlan(t.Context(), saved)
			require.ErrorContains(t, err, peeringAddress)
			require.ErrorContains(t, err, tc.diagnostic)
			assert.Empty(t, cloud.mutations())
			seedFlowLog(t, exec)
			_, err = exec.Plan(t.Context())
			require.ErrorContains(t, err, tc.diagnostic)
			exec.Destroy = true
			_, err = exec.ApplyPlan(t.Context(), peeringRuntimeSavedPlan(t, exec))
			require.NoError(t, err)
			assert.Equal(t, []url.Values{{"Action": {"DeleteFlowLogs"}, "Version": {"2016-11-15"},
				"FlowLogId.1": {"fl-prior"}}}, cloud.mutations())
		})
	}
}

func TestFlowLogNativeDeferredAndParserPreflight(t *testing.T) {
	libraries := flowLogRuntimeLibrary(t)
	for _, tc := range []struct {
		name, field, diagnostic string
		value                   any
	}{
		{"zero", "vpc-id", "constraints[0]", nil},
		{"multiple", "subnet-id", "constraints[0]", "subnet-one"},
		{"destination type", "log-destination-type", "constraints[1]", "bad"},
		{"aggregation", "max-aggregation-interval", "constraints[2]", int64(61)},
		{"traffic", "traffic-type", "constraints[3]", "bad"},
		{"options", "destination-options", "constraints[4]", map[string]any{"file-format": "bad"}},
		{"ARN parser", "log-destination", "invalid ARN", "bad"},
		{"valid replacement", "vpc-id", "", "vpc-two"},
		{"valid unchanged", "vpc-id", "", "vpc-one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &flowLogCloud{exists: true}
			exec := peeringRuntimeExecutor(t, "../flow-log/deferred", libraries,
				flowLogInputs(t, cloud.endpoint(t, "prior")))
			seedFlowLog(t, exec)
			exec.Inputs[tc.field] = tc.value
			saved := peeringRuntimeSavedPlan(t, exec)
			assert.Equal(t, map[string][]string{
				"vpc-id": {"action.seed.resolved-vpc"}, "traffic-type": {"action.seed.resolved-traffic"},
				"max-aggregation-interval": {"action.seed.resolved-aggregate"},
				"log-format":               {"action.seed.resolved-format"},
				"log-destination":          {"action.seed.resolved-destination"},
				"log-destination-type":     {"action.seed.resolved-type"},
				"destination-options":      {"action.seed.resolved-options"},
			}, peeringRuntimeStep(t, saved).UnresolvedInputs)
			_, err := exec.ApplyPlan(t.Context(), saved)
			if tc.diagnostic != "" {
				require.ErrorContains(t, err, tc.diagnostic)
				assert.Empty(t, cloud.mutations())
				return
			}
			require.NoError(t, err)
			if tc.name == "valid unchanged" {
				assert.Empty(t, cloud.mutations())
				return
			}
			assert.Equal(t, []url.Values{
				{"Action": {"DeleteFlowLogs"}, "Version": {"2016-11-15"}, "FlowLogId.1": {"fl-prior"}},
				{"Action": {"CreateFlowLogs"}, "Version": {"2016-11-15"}, "ResourceId.1": {"vpc-two"},
					"ResourceType": {"VPC"}, "MaxAggregationInterval": {"600"},
					"LogDestinationType": {"cloud-watch-logs"}},
			}, cloud.mutations())
		})
	}
}

func TestFlowLogEffectiveReplacementAndDefaults(t *testing.T) {
	libraries := flowLogRuntimeLibrary(t)
	for _, tc := range []struct {
		name, field    string
		prior, desired any
		decision       runtime.Decision
	}{
		{"ENI", "eni-id", nil, "eni-one", runtime.DecisionReplace},
		{"NAT", "regional-nat-gateway-id", nil, "nat-one", runtime.DecisionReplace},
		{"subnet", "subnet-id", nil, "subnet-one", runtime.DecisionReplace},
		{"VPC", "vpc-id", "vpc-one", "vpc-two", runtime.DecisionReplace},
		{"TGW", "transit-gateway-id", nil, "tgw-one", runtime.DecisionReplace},
		{"attachment", "transit-gateway-attachment-id", nil, "tgw-attach-one", runtime.DecisionReplace},
		{"role", "iam-role-arn", nil, "arn:aws:iam::aws:role/x", runtime.DecisionReplace},
		{"cross role", "deliver-cross-account-role", nil,
			"arn:aws:iam::aws:role/x", runtime.DecisionReplace},
		{"destination", "log-destination", nil, "arn:aws:s3:::new", runtime.DecisionReplace},
		{"format", "log-format", nil, "new", runtime.DecisionReplace},
		{"type", "log-destination-type", nil, "s3", runtime.DecisionReplace},
		{"aggregation", "max-aggregation-interval", nil, int64(60), runtime.DecisionReplace},
		{"traffic", "traffic-type", nil, "ALL", runtime.DecisionReplace},
		{"options", "destination-options", nil, map[string]any{}, runtime.DecisionReplace},
		{"options format", "destination-options", map[string]any{},
			map[string]any{"file-format": "parquet"}, runtime.DecisionReplace},
		{"options hive", "destination-options", map[string]any{},
			map[string]any{"hive-compatible-partitions": true}, runtime.DecisionReplace},
		{"options hourly", "destination-options", map[string]any{},
			map[string]any{"per-hour-partition": true}, runtime.DecisionReplace},
		{"explicit type default", "log-destination-type", nil, "cloud-watch-logs", runtime.DecisionNoOp},
		{"explicit aggregation default", "max-aggregation-interval", nil,
			int64(600), runtime.DecisionNoOp},
		{"explicit option defaults", "destination-options", map[string]any{},
			map[string]any{"file-format": "plain-text", "hive-compatible-partitions": false,
				"per-hour-partition": false}, runtime.DecisionNoOp},
		{"computed destination", "log-destination", "arn:aws:s3:::old", nil, runtime.DecisionNoOp},
		{"computed format", "log-format", "old", nil, runtime.DecisionNoOp},
		{"empty optional role", "iam-role-arn", nil, "", runtime.DecisionNoOp},
		{"empty tags", "tags", nil, map[string]any{}, runtime.DecisionNoOp},
		{"tag update", "tags", nil, map[string]any{"stage": "updated"}, runtime.DecisionUpdate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &flowLogCloud{exists: true}
			inputs := flowLogInputs(t, cloud.endpoint(t, "prior"))
			inputs[tc.field] = tc.prior
			exec := peeringRuntimeExecutor(t, "../flow-log/evaluated", libraries, inputs)
			seedFlowLog(t, exec)
			if tc.field == "eni-id" || tc.field == "regional-nat-gateway-id" || tc.field == "subnet-id" ||
				tc.field == "transit-gateway-id" || tc.field == "transit-gateway-attachment-id" {
				exec.Inputs["vpc-id"] = nil
			}
			exec.Inputs[tc.field] = tc.desired
			step := peeringRuntimeStep(t, peeringRuntimeSavedPlan(t, exec))
			require.Equal(t, tc.decision, step.Decision)
			if tc.decision == runtime.DecisionReplace {
				assert.Contains(t, step.ReplacementReasons, tc.field)
			}
			assert.Equal(t, "arn:aws:s3:::computed", step.ObservedOutputs["log-destination"])
			assert.Equal(t, "computed", step.ObservedOutputs["log-format"])
			assert.Empty(t, cloud.mutations())
		})
	}
}

func TestFlowLogReplacementUsesSavedIdentityAndRegion(t *testing.T) {
	cloud := &flowLogCloud{exists: true}
	libraries := flowLogRuntimeLibrary(t)
	exec := peeringRuntimeExecutor(t, "../flow-log/evaluated", libraries,
		flowLogInputs(t, cloud.endpoint(t, "prior")))
	seedFlowLog(t, exec)
	exec.Inputs["aws-config"] = map[string]any{
		"region": "us-west-2", "endpoint-url": cloud.endpoint(t, "new"),
	}
	saved := peeringRuntimeSavedPlan(t, exec)
	require.Equal(t, runtime.DecisionReplace, peeringRuntimeStep(t, saved).Decision)
	assert.Equal(t, []string{"region"}, peeringRuntimeStep(t, saved).ReplacementReasons)
	_, err := exec.ApplyPlan(t.Context(), saved)
	require.NoError(t, err)
	for _, request := range cloud.requests {
		newResource := request.Form.Get("Action") == "CreateFlowLogs" ||
			request.Form.Get("FlowLogId.1") == "fl-created" ||
			(request.Form.Get("Action") == "GetCallerIdentity" && request.Endpoint == "new")
		if newResource {
			assert.Equal(t, "new", request.Endpoint)
			assert.Contains(t, request.Signing, "/us-west-2/")
		} else {
			assert.Equal(t, "prior", request.Endpoint)
			assert.Contains(t, request.Signing, "/us-east-1/")
		}
	}
	assert.Equal(t, []url.Values{
		{"Action": {"DeleteFlowLogs"}, "Version": {"2016-11-15"}, "FlowLogId.1": {"fl-prior"}},
		{"Action": {"CreateFlowLogs"}, "Version": {"2016-11-15"}, "ResourceId.1": {"vpc-one"},
			"ResourceType": {"VPC"}, "MaxAggregationInterval": {"600"},
			"LogDestinationType": {"cloud-watch-logs"}},
	}, cloud.mutations())
}

func TestFlowLogTagDriftAndErrors(t *testing.T) {
	libraries := flowLogRuntimeLibrary(t)
	for _, fail := range []string{"", "DeleteTags", "CreateTags"} {
		t.Run(fail, func(t *testing.T) {
			cloud := &flowLogCloud{exists: true,
				tags: map[string]string{"drop": "yes", "stage": "old"}, fail: fail}
			inputs := flowLogInputs(t, cloud.endpoint(t, "prior"))
			inputs["tags"] = map[string]any{"stage": "updated"}
			exec := peeringRuntimeExecutor(t, "../flow-log/evaluated", libraries, inputs)
			seedFlowLog(t, exec)
			saved := peeringRuntimeSavedPlan(t, exec)
			require.Equal(t, runtime.DecisionUpdate, peeringRuntimeStep(t, saved).Decision)
			_, err := exec.ApplyPlan(t.Context(), saved)
			if fail != "" {
				require.ErrorContains(t, err, "FixtureFailure")
			} else {
				require.NoError(t, err)
			}
			expected := []url.Values{{"Action": {"DeleteTags"}, "Version": {"2016-11-15"},
				"ResourceId.1": {"fl-prior"}, "Tag.1.Key": {"drop"}}}
			if fail != "DeleteTags" {
				expected = append(expected, url.Values{
					"Action": {"CreateTags"}, "Version": {"2016-11-15"}, "ResourceId.1": {"fl-prior"},
					"Tag.1.Key": {"stage"}, "Tag.1.Value": {"updated"}})
			}
			assert.Equal(t, expected, cloud.mutations())
		})
	}
}

func (c *flowLogCloud) serveBucket(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Content-Type", "application/xml")
	switch r.Method {
	case http.MethodHead:
		if !c.bucket {
			w.WriteHeader(http.StatusNotFound)
		}
	case http.MethodPut:
		if c.fail == "CreateBucket" {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `<Error><Code>FixtureFailure</Code><Message>injected failure</Message></Error>`)
			return
		}
		c.bucket = true
	case http.MethodGet:
		if !r.URL.Query().Has("versions") {
			t.Errorf("unexpected bucket read %s", r.URL)
		}
		fmt.Fprint(w, `<ListVersionsResult/>`)
	case http.MethodDelete:
		if c.exists {
			t.Error("bucket deleted before flow log")
		}
		c.bucket = false
		w.WriteHeader(http.StatusNoContent)
	default:
		t.Errorf("unexpected bucket request %s %s", r.Method, r.URL)
	}
}

func TestFlowLogEvaluatedParserPreflightBeforeReplacement(t *testing.T) {
	cloud := &flowLogCloud{exists: true}
	exec := peeringRuntimeExecutor(t, "../flow-log/evaluated", flowLogRuntimeLibrary(t),
		flowLogInputs(t, cloud.endpoint(t, "prior")))
	seedFlowLog(t, exec)
	exec.Inputs["iam-role-arn"] = "arn:other:iam::123456789012:role/bad"
	saved := peeringRuntimeSavedPlan(t, exec)
	require.Equal(t, runtime.DecisionReplace, peeringRuntimeStep(t, saved).Decision)
	_, err := exec.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "iam-role-arn: invalid ARN")
	assert.Empty(t, cloud.mutations())
	assert.True(t, cloud.exists)
}
