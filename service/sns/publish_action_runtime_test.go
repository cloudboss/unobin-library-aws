package sns

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sns/types"
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

const (
	publishAddress = "action.send"
	publishTopic   = "arn:aws:sns:us-east-1:012345678901:topic"
	attributeRule  = "constraints[4] (predicate): data-type must be String, Number, or Binary"
)

type publishSeed struct {
	Echo any `ub:"echo"`
}

func (r *publishSeed) Run(context.Context, *awscfg.Configuration) (map[string]any, error) {
	return map[string]any{"value": r.Echo}, nil
}

type publishRequest struct {
	form          url.Values
	authorization string
}

func publishEndpoint(t *testing.T) (string, <-chan publishRequest) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "sns-test-credentials")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	requests := make(chan publishRequest, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse SNS query request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case requests <- publishRequest{r.PostForm, r.Header.Get("Authorization")}:
		default:
			t.Error("unexpected SNS request")
		}
		w.Header().Set("Content-Type", "text/xml")
		code, message := "", ""
		switch {
		case r.PostForm.Get("TopicArn") == "invalid-arn":
			code, message = "InvalidParameter", "Invalid parameter: TopicArn"
		case r.PostForm.Get("Message") == "{malformed":
			code, message = "InvalidParameter", "Invalid parameter: Message JSON"
		case r.PostForm.Get("Message") == "reject-binary":
			code, message = "InvalidParameter", "Binary attribute requires BinaryValue"
		}
		if code != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "<ErrorResponse><Error><Code>%s</Code>"+
				"<Message>%s</Message></Error></ErrorResponse>", code, message)
			return
		}
		_, _ = fmt.Fprint(w, `<PublishResponse><PublishResult><MessageId>test-id</MessageId>
<SequenceNumber>43</SequenceNumber></PublishResult></PublishResponse>`)
	}))
	t.Cleanup(server.Close)
	return server.URL, requests
}

func publishExecutor(t *testing.T, fixture string) *runtime.Executor {
	t.Helper()
	lib := runtime.LibraryWithPath(Library(),
		"github.com/cloudboss/unobin-library-aws//service/sns")
	lib.Schema = readLibrarySchema(t)
	lib.Constraints = map[string][]lang.ConstraintSpec{
		"action.publish": lib.Schema.Actions["publish"].Constraints,
	}
	libs := map[string]*runtime.Library{
		"sns": lib,
		"test": {
			Actions: map[string]runtime.ActionRegistration{
				"seed": runtime.MakeAction[publishSeed, map[string]any, *awscfg.Configuration](),
			},
		},
	}
	path := filepath.Join("testdata", "ub", "publish", fixture, "factory.ub")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	file, _, err := compile.ParseFactorySyntaxSource(path, source)
	require.NoError(t, err)
	body := file.Factory.Body
	checker := check.NewSyntax(body, libs, nil, "")
	require.NoError(t, checker.References(nil).Err())
	require.NoError(t, checker.LiteralConstraints().Err())
	store, err := local.NewStore(t.TempDir(), "test-publish", "test", encrypters.Noop{})
	require.NoError(t, err)
	return &runtime.Executor{
		DAG: checker.DAG(), SyntaxSource: &body, Libraries: libs, Store: store,
		Factory: state.FactoryInfo{Name: "test-publish", Version: "v0", ContentRevision: "test"},
	}
}

func savedPublishPlan(t *testing.T, executor *runtime.Executor) *runtime.PlanFile {
	t.Helper()
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	saved, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	return saved
}

func publishStep(t *testing.T, plan *runtime.PlanFile) runtime.PlanStep {
	t.Helper()
	for _, step := range plan.Steps {
		if step.Address == publishAddress {
			return step
		}
	}
	t.Fatalf("saved plan has no %s step", publishAddress)
	return runtime.PlanStep{}
}

func publishStages(events <-chan runtime.ApplyEvent) []runtime.ApplyStage {
	var stages []runtime.ApplyStage
	for len(events) > 0 {
		if event := <-events; event.Address == publishAddress {
			stages = append(stages, event.Stage)
		}
	}
	return stages
}

func publishArguments(topic, message string) map[string]any {
	return map[string]any{
		"topic-arn": topic, "message": message, "subject": nil,
		"message-structure": nil, "message-attributes": nil, "region": nil,
	}
}

func publishForm(topic, message string) url.Values {
	return url.Values{
		"Action": {"Publish"}, "Version": {"2010-03-31"},
		"TopicArn": {topic}, "Message": {message},
	}
}

func assertPublishRequest(
	t *testing.T, requests <-chan publishRequest, want url.Values, region string,
) {
	t.Helper()
	select {
	case request := <-requests:
		assert.Equal(t, want, request.form)
		assert.Regexp(t, "Credential=sns-test-credentials/[0-9]{8}/"+region+
			"/sns/aws4_request,", request.authorization)
	default:
		t.Fatal("missing SNS Publish request")
	}
	assert.Empty(t, requests)
}

func TestPublishActionSavedPlans(t *testing.T) {
	endpoint, requests := publishEndpoint(t)
	executor := publishExecutor(t, "settled")
	args := publishArguments("", "")
	executor.Inputs = map[string]any{"endpoint": endpoint, "arguments": args, "trigger": "fixed"}
	first := savedPublishPlan(t, executor)
	require.Equal(t, runtime.DecisionRerun, publishStep(t, first).Decision)
	assert.Empty(t, requests)
	events := make(chan runtime.ApplyEvent, 30)
	executor.Events = events
	executor.Inputs = nil
	_, err := executor.ApplyPlan(t.Context(), first)
	require.NoError(t, err)
	assertPublishRequest(t, requests, publishForm("", ""), "us-east-1")
	assert.Equal(t, []runtime.ApplyStage{runtime.StageStart, runtime.StageDone}, publishStages(events))

	snapshot, err := executor.Store.Current()
	require.NoError(t, err)
	entry := snapshot.Find(publishAddress)
	require.NotNil(t, entry)
	assert.Empty(t, entry.Outputs)

	executor.Inputs = first.Inputs
	unchanged := savedPublishPlan(t, executor)
	require.Equal(t, runtime.DecisionSkip, publishStep(t, unchanged).Decision)
	executor.Inputs = nil
	_, err = executor.ApplyPlan(t.Context(), unchanged)
	require.NoError(t, err)
	assert.Empty(t, requests)

	executor.Events = nil
	for _, tt := range []struct {
		name  string
		field string
		value any
		want  string
	}{
		{
			name: "null topic", field: "topic-arn",
			want: "constraints[0] (predicate): topic-arn is required",
		},
		{
			name: "null message", field: "message",
			want: "constraints[1] (predicate): message is required",
		},
		{
			name: "uppercase message structure", field: "message-structure", value: "JSON",
			want: "constraints[2] (predicate): message-structure must be json",
		},
		{
			name: "empty message structure", field: "message-structure", value: "",
			want: "constraints[2] (predicate): message-structure must be json",
		},
		{
			name: "missing attribute name", field: "message-attributes",
			value: []any{map[string]any{"data-type": "String", "string-value": "x"}},
			want:  "constraints[3] (predicate): name is required (message-attributes[0])",
		},
		{
			name: "null attribute name", field: "message-attributes",
			value: []any{map[string]any{"name": nil, "data-type": "String", "string-value": "x"}},
			want:  "constraints[3] (predicate): name is required (message-attributes[0])",
		},
		{
			name: "missing attribute type", field: "message-attributes",
			value: []any{map[string]any{"name": "label", "string-value": "x"}},
			want:  attributeRule + " (message-attributes[0])",
		},
		{
			name: "null attribute type", field: "message-attributes",
			value: []any{map[string]any{"name": "label", "data-type": nil, "string-value": "x"}},
			want:  attributeRule + " (message-attributes[0])",
		},
		{
			name: "bad attribute enum", field: "message-attributes",
			value: []any{map[string]any{"name": "label", "data-type": "binary", "string-value": "x"}},
			want:  attributeRule + " (message-attributes[0])",
		},
		{
			name: "missing attribute value", field: "message-attributes",
			value: []any{map[string]any{"name": "label", "data-type": "String"}},
			want:  "constraints[5] (predicate): string-value is required (message-attributes[0])",
		},
		{
			name: "null attribute value", field: "message-attributes",
			value: []any{map[string]any{"name": "label", "data-type": "String", "string-value": nil}},
			want:  "constraints[5] (predicate): string-value is required (message-attributes[0])",
		},
		{
			name: "null attribute element", field: "message-attributes",
			value: []any{nil},
			want:  "constraints[3] (predicate): name is required (message-attributes[0])",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			executor.Inputs = first.Inputs
			skip := savedPublishPlan(t, executor)
			require.Equal(t, runtime.DecisionSkip, publishStep(t, skip).Decision)
			invalid := publishArguments("", "")
			invalid[tt.field] = tt.value
			executor.Inputs = map[string]any{
				"endpoint": endpoint, "arguments": invalid, "trigger": "fixed",
			}
			_, err := executor.Plan(t.Context())
			require.ErrorContains(t, err, publishAddress+": schema: "+tt.want)
			assert.Empty(t, requests)
			_, err = executor.ApplyPlan(t.Context(), skip)
			require.ErrorContains(t, err, publishAddress+": schema: "+tt.want)
			assert.Empty(t, requests)
		})
	}

	args = publishArguments(publishTopic, " unchanged \n")
	args["region"] = ""
	args["message-attributes"] = []any{}
	executor.Inputs = map[string]any{"endpoint": endpoint, "arguments": args, "trigger": "changed"}
	rerun := savedPublishPlan(t, executor)
	require.Equal(t, runtime.DecisionRerun, publishStep(t, rerun).Decision)
	_, err = executor.ApplyPlan(t.Context(), rerun)
	require.NoError(t, err)
	assertPublishRequest(t, requests, publishForm(publishTopic, " unchanged \n"), "us-east-1")

	snapshot, err = executor.Store.Current()
	require.NoError(t, err)
	entry = snapshot.Find(publishAddress)
	require.NotNil(t, entry)
	entry.Inputs["message-structure"] = "historical invalid value"
	revision, err := executor.Store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(revision))
	args["message-structure"] = "historical invalid value"
	executor.Destroy = true
	destroy := savedPublishPlan(t, executor)
	require.Equal(t, runtime.DecisionDestroy, publishStep(t, destroy).Decision)
	_, err = executor.ApplyPlan(t.Context(), destroy)
	require.NoError(t, err)
	assert.Empty(t, requests)
	snapshot, err = executor.Store.Current()
	require.NoError(t, err)
	assert.Nil(t, snapshot.Find(publishAddress))
}

func TestPublishActionSDKQuery(t *testing.T) {
	endpoint, requests := publishEndpoint(t)
	executor := publishExecutor(t, "settled")
	args := publishArguments(publishTopic, "{malformed")
	args["region"] = "us-west-2"
	args["subject"] = ""
	args["message-structure"] = "json"
	args["message-attributes"] = []any{
		map[string]any{"name": "label", "data-type": "String", "string-value": "first"},
		map[string]any{"name": "amount", "data-type": "Number", "string-value": "001.20"},
		map[string]any{"name": "blob", "data-type": "Binary", "string-value": "YWJj"},
		map[string]any{"name": "label", "data-type": "String", "string-value": "last"},
	}
	executor.Inputs = map[string]any{"endpoint": endpoint, "arguments": args, "trigger": "first"}
	events := make(chan runtime.ApplyEvent, 20)
	executor.Events = events
	want := publishForm(publishTopic, "{malformed")
	want.Set("Subject", "")
	want.Set("MessageStructure", "json")
	want.Set("MessageAttributes.entry.1.Name", "amount")
	want.Set("MessageAttributes.entry.1.Value.DataType", "Number")
	want.Set("MessageAttributes.entry.1.Value.StringValue", "001.20")
	want.Set("MessageAttributes.entry.2.Name", "blob")
	want.Set("MessageAttributes.entry.2.Value.DataType", "Binary")
	want.Set("MessageAttributes.entry.2.Value.StringValue", "YWJj")
	want.Set("MessageAttributes.entry.3.Name", "label")
	want.Set("MessageAttributes.entry.3.Value.DataType", "String")
	want.Set("MessageAttributes.entry.3.Value.StringValue", "last")

	plan := savedPublishPlan(t, executor)
	_, err := executor.ApplyPlan(t.Context(), plan)
	var apiErr *types.InvalidParameterException
	require.ErrorAs(t, err, &apiErr)
	assert.ErrorContains(t, err, "publish SNS message")
	assert.Equal(t, "Invalid parameter: Message JSON", apiErr.ErrorMessage())
	assertPublishRequest(t, requests, want, "us-west-2")
	assert.Equal(t, []runtime.ApplyStage{runtime.StageStart, runtime.StageFail}, publishStages(events))

	args["message-structure"] = nil
	args["message"] = "reject-binary"
	want.Set("Message", "reject-binary")
	want.Del("MessageStructure")
	_, err = executor.ApplyPlan(t.Context(), savedPublishPlan(t, executor))
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "Binary attribute requires BinaryValue", apiErr.ErrorMessage())
	assertPublishRequest(t, requests, want, "us-west-2")

	args = publishArguments("invalid-arn", "message")
	executor.Inputs["arguments"] = args
	_, err = executor.ApplyPlan(t.Context(), savedPublishPlan(t, executor))
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "Invalid parameter: TopicArn", apiErr.ErrorMessage())
	assertPublishRequest(t, requests, publishForm("invalid-arn", "message"), "us-east-1")

	args["region"] = "cn-north-1"
	_, err = executor.ApplyPlan(t.Context(), savedPublishPlan(t, executor))
	require.ErrorContains(t, err, `region "cn-north-1" belongs to partition "aws-cn", expected "aws"`)
	assert.Empty(t, requests)
}

func TestPublishActionDeferredSavedPlan(t *testing.T) {
	for _, tt := range []struct {
		name    string
		fixture string
		value   any
		want    string
	}{
		{name: "valid resolved list", fixture: "deferred", value: "ready"},
		{
			name: "bad sibling beside pending string-value", fixture: "deferred-bad-sibling",
			value: "ready", want: attributeRule + " (message-attributes[1])",
		},
		{
			name: "pending string-value resolves to null", fixture: "deferred",
			want: "constraints[5] (predicate): string-value is required (message-attributes[0])",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, requests := publishEndpoint(t)
			executor := publishExecutor(t, tt.fixture)
			executor.Inputs = map[string]any{"endpoint": endpoint, "value": tt.value}
			plan := savedPublishPlan(t, executor)
			step := publishStep(t, plan)
			require.Equal(t, runtime.DecisionRerun, step.Decision)
			assert.Equal(t, map[string][]string{"message-attributes": {"action.seed.value"}},
				step.UnresolvedInputs)
			assert.Empty(t, requests)
			executor.Inputs = nil
			_, err := executor.ApplyPlan(t.Context(), plan)
			if tt.want != "" {
				require.ErrorContains(t, err, publishAddress+": schema: "+tt.want)
				assert.Empty(t, requests)
				return
			}
			require.NoError(t, err)
			want := publishForm(publishTopic, "deferred message")
			want.Set("MessageAttributes.entry.1.Name", "first")
			want.Set("MessageAttributes.entry.1.Value.DataType", "String")
			want.Set("MessageAttributes.entry.1.Value.StringValue", "ready")
			want.Set("MessageAttributes.entry.2.Name", "second")
			want.Set("MessageAttributes.entry.2.Value.DataType", "Number")
			want.Set("MessageAttributes.entry.2.Value.StringValue", "0001")
			assertPublishRequest(t, requests, want, "us-east-1")
		})
	}
}
