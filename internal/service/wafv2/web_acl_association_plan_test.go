package wafv2

import (
	"context"
	"fmt"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type webACLAssociationPlanRecorder struct {
	createIdentity WebACLAssociationResourceOutput
	deleteIdentity WebACLAssociationResourceOutput
	creates        int
	deletes        int
	updates        int
	drifted        bool
}

type webACLAssociationPlanProbe struct {
	ResourceARN string `ub:"resource-arn"`
	WebACLARN   string `ub:"web-acl-arn"`

	recorder *webACLAssociationPlanRecorder
}

func (*webACLAssociationPlanProbe) ResourceDefinition() runtime.ResourceDefinition[
	webACLAssociationPlanProbe, *WebACLAssociationResourceOutput, any,
] {
	return runtime.ResourceDefinition[
		webACLAssociationPlanProbe, *WebACLAssociationResourceOutput, any,
	]{
		SchemaVersion: 1,
		Replace: runtime.Replacement[
			webACLAssociationPlanProbe, *WebACLAssociationResourceOutput, any,
		]{
			Fields: []runtime.AnyInputField[webACLAssociationPlanProbe]{
				runtime.InputField(func(input *webACLAssociationPlanProbe) *string {
					return &input.ResourceARN
				}),
				runtime.InputField(func(input *webACLAssociationPlanProbe) *string {
					return &input.WebACLARN
				}),
			},
		},
	}
}

func (r *webACLAssociationPlanProbe) Create(
	context.Context,
	any,
) (*WebACLAssociationResourceOutput, error) {
	r.recorder.creates++
	r.recorder.createIdentity = r.identity(nil)
	return &r.recorder.createIdentity, nil
}

func (r *webACLAssociationPlanProbe) Read(
	_ context.Context,
	_ any,
	prior runtime.Prior[webACLAssociationPlanProbe, *WebACLAssociationResourceOutput, any],
) (*WebACLAssociationResourceOutput, error) {
	if r.recorder.drifted {
		return nil, runtime.ErrNotFound
	}
	return prior.Outputs, nil
}

func (r *webACLAssociationPlanProbe) Update(
	_ context.Context,
	_ any,
	prior runtime.Prior[webACLAssociationPlanProbe, *WebACLAssociationResourceOutput, any],
) (*WebACLAssociationResourceOutput, error) {
	r.recorder.updates++
	return prior.Outputs, nil
}

func (r *webACLAssociationPlanProbe) Delete(
	_ context.Context,
	_ any,
	prior runtime.Prior[webACLAssociationPlanProbe, *WebACLAssociationResourceOutput, any],
) error {
	r.recorder.deletes++
	r.recorder.deleteIdentity = r.identity(prior.Outputs)
	return nil
}

func (r *webACLAssociationPlanProbe) identity(
	prior *WebACLAssociationResourceOutput,
) WebACLAssociationResourceOutput {
	resource := &WebACLAssociationResource{
		ResourceARN: r.ResourceARN,
		WebACLARN:   r.WebACLARN,
	}
	return resource.identity(prior)
}

func TestWebACLAssociationPlanReplacesEitherFieldAndDeletesPriorPair(t *testing.T) {
	newResourceARN := "arn:aws:example:us-east-1:123456789012:thing/new"
	newWebACLARN :=
		"arn:aws:wafv2:us-east-1:123456789012:regional/webacl/new/new-id"
	tests := map[string]struct {
		resourceARN string
		webACLARN   string
		trigger     string
	}{
		"resource ARN": {
			resourceARN: newResourceARN,
			webACLARN:   associationWebACLARN,
			trigger:     "resource-arn",
		},
		"web ACL ARN": {
			resourceARN: associationResourceARN,
			webACLARN:   newWebACLARN,
			trigger:     "web-acl-arn",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			recorder := &webACLAssociationPlanRecorder{}
			executor := webACLAssociationPlanExecutor(
				t, tt.resourceARN, tt.webACLARN, recorder,
			)

			plan, err := executor.Plan(context.Background())
			require.NoError(t, err)
			require.Len(t, plan.Steps, 1)
			assert.Equal(t, runtime.DecisionReplace, plan.Steps[0].Decision)
			assert.Equal(t, []string{tt.trigger}, plan.Steps[0].ReplacementReasons)

			encoded, err := runtime.EncodePlan(plan)
			require.NoError(t, err)
			planFile, err := runtime.DecodePlan(encoded)
			require.NoError(t, err)
			_, err = executor.ApplyPlan(context.Background(), planFile)
			require.NoError(t, err)
			assert.Equal(t, 1, recorder.deletes)
			assert.Equal(t, 1, recorder.creates)
			assert.Zero(t, recorder.updates)
			assert.Equal(t, WebACLAssociationResourceOutput{
				ResourceARN: associationResourceARN,
				WebACLARN:   associationWebACLARN,
			}, recorder.deleteIdentity)
			assert.Equal(t, WebACLAssociationResourceOutput{
				ResourceARN: tt.resourceARN,
				WebACLARN:   tt.webACLARN,
			}, recorder.createIdentity)
		})
	}
}

func TestWebACLAssociationDriftPlansCreateAndRestoresDesiredPair(t *testing.T) {
	recorder := &webACLAssociationPlanRecorder{drifted: true}
	executor := webACLAssociationPlanExecutor(
		t, associationResourceARN, associationWebACLARN, recorder,
	)

	plan, err := executor.Plan(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Steps, 1)
	assert.Equal(t, runtime.DecisionCreate, plan.Steps[0].Decision)

	encoded, err := runtime.EncodePlan(plan)
	require.NoError(t, err)
	planFile, err := runtime.DecodePlan(encoded)
	require.NoError(t, err)
	_, err = executor.ApplyPlan(context.Background(), planFile)
	require.NoError(t, err)
	assert.Equal(t, 1, recorder.creates)
	assert.Zero(t, recorder.updates)
	assert.Zero(t, recorder.deletes)
	assert.Equal(t, WebACLAssociationResourceOutput{
		ResourceARN: associationResourceARN,
		WebACLARN:   associationWebACLARN,
	}, recorder.createIdentity)
}

func webACLAssociationPlanExecutor(
	t *testing.T,
	resourceARN string,
	webACLARN string,
	recorder *webACLAssociationPlanRecorder,
) *runtime.Executor {
	t.Helper()
	source := fmt.Sprintf(`factory: {
  resources: {
    association: aws-wafv2.web-acl-association {
      resource-arn: '%s'
      web-acl-arn:   '%s'
    }
  }
}`, resourceARN, webACLARN)
	parsed, err := syntax.ParseSource("factory.ub", []byte(source))
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)
	body := parsed.Factory.Body
	libraries := map[string]*runtime.Library{
		"aws-wafv2": {
			Name: "aws-wafv2",
			Resources: map[string]runtime.ResourceRegistration{
				"web-acl-association": runtime.MakeResourceWith[
					webACLAssociationPlanProbe,
					*WebACLAssociationResourceOutput,
					any,
				]((&webACLAssociationPlanProbe{}).ResourceDefinition(), func() *webACLAssociationPlanProbe {
					return &webACLAssociationPlanProbe{recorder: recorder}
				}),
			},
		},
	}
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	factory := state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"}
	snapshot := state.NewSnapshot(factory, store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address:       "resource.association",
		Composite:     false,
		Category:      "resource",
		Binding:       &state.Binding{Alias: "aws-wafv2", Export: "web-acl-association"},
		SchemaVersion: 1,
		Inputs: map[string]any{
			"resource-arn": associationResourceARN,
			"web-acl-arn":  associationWebACLARN,
		},
		Outputs: map[string]any{
			"resource-arn": associationResourceARN,
			"web-acl-arn":  associationWebACLARN,
		},
	}}
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	return &runtime.Executor{
		DAG:          runtime.BuildSyntaxDAG(body, libraries),
		SyntaxSource: &body,
		Libraries:    libraries,
		Store:        store,
		Factory:      factory,
	}
}
