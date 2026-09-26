package efs

import (
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type accessPointPlanProbe struct {
	FileSystemId string             `ub:"file-system-id"`
	Tags         *map[string]string `ub:"tags"`
}

func (*accessPointPlanProbe) Create(context.Context, any) (*AccessPointResourceOutput, error) {
	return &AccessPointResourceOutput{AccessPointId: testAccessPointID}, nil
}

func (*accessPointPlanProbe) Read(
	_ context.Context, _ any,
	prior runtime.Prior[accessPointPlanProbe, *AccessPointResourceOutput, any],
) (*AccessPointResourceOutput, error) {
	return prior.Outputs, nil
}

func (*accessPointPlanProbe) Update(
	_ context.Context,
	_ any,
	prior runtime.Prior[accessPointPlanProbe, *AccessPointResourceOutput, any],
) (*AccessPointResourceOutput, error) {
	return prior.Outputs, nil
}

func (*accessPointPlanProbe) Delete(
	context.Context, any, runtime.Prior[accessPointPlanProbe, *AccessPointResourceOutput, any],
) error {
	return nil
}

func (*accessPointPlanProbe) ResourceDefinition() runtime.ResourceDefinition[
	accessPointPlanProbe, *AccessPointResourceOutput, any,
] {
	return runtime.ResourceDefinition[accessPointPlanProbe, *AccessPointResourceOutput, any]{
		SchemaVersion: 1,
		Replace: runtime.Replacement[accessPointPlanProbe, *AccessPointResourceOutput, any]{
			Fields: []runtime.AnyInputField[accessPointPlanProbe]{
				runtime.InputField(func(input *accessPointPlanProbe) *string {
					return &input.FileSystemId
				}),
			},
		},
	}
}

func TestAccessPointPlanDistinguishesUpdateAndReplacement(t *testing.T) {
	tests := []struct {
		name            string
		source          string
		wantDecision    runtime.Decision
		wantReplacement []string
	}{
		{
			name: "tags update in place",
			source: `factory: {
  resources: {
    access: aws-efs.access-point {
      file-system-id: 'fs-0123456789abcdef0'
      tags: { env: 'new' }
    }
  }
}`,
			wantDecision: runtime.DecisionUpdate,
		},
		{
			name: "file system change replaces",
			source: `factory: {
  resources: {
    access: aws-efs.access-point {
      file-system-id: 'fs-0123456789abcdef1'
      tags: { env: 'old' }
    }
  }
}`,
			wantDecision:    runtime.DecisionReplace,
			wantReplacement: []string{"file-system-id"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := syntax.ParseSource("factory.ub", []byte(tt.source))
			require.NoError(t, err)
			require.NotNil(t, parsed.Factory)
			body := parsed.Factory.Body
			libraries := map[string]*runtime.Library{
				"aws-efs": {
					Name: "aws-efs",
					Resources: map[string]runtime.ResourceRegistration{
						"access-point": runtime.MakeResource[
							accessPointPlanProbe,
							*AccessPointResourceOutput,
							any,
						]((&accessPointPlanProbe{}).ResourceDefinition()),
					},
				},
			}
			store, err := local.NewStore(
				t.TempDir(), "factory", "stack", encrypters.Noop{})
			require.NoError(t, err)
			factory := state.FactoryInfo{
				Name: "factory", Version: "v0", ContentRevision: "test",
			}
			snapshot := state.NewSnapshot(factory, store.Stack())
			snapshot.Entries = []*state.Entry{{
				Address:       "resource.access",
				Type:          state.EntryLeaf,
				Category:      "resource",
				Binding:       &state.Binding{Alias: "aws-efs", Export: "access-point"},
				SchemaVersion: 1,
				Inputs: map[string]any{
					"file-system-id": testAccessPointFileSystemID,
					"tags":           map[string]any{"env": "old"},
				},
				Outputs: map[string]any{"access-point-id": testAccessPointID},
			}}
			revision, err := store.Write(snapshot)
			require.NoError(t, err)
			require.NoError(t, store.SetCurrent(revision))
			executor := &runtime.Executor{
				DAG:          runtime.BuildSyntaxDAG(body, libraries),
				SyntaxSource: &body,
				Libraries:    libraries,
				Store:        store,
				Factory:      factory,
			}

			plan, err := executor.Plan(context.Background())

			require.NoError(t, err)
			require.Len(t, plan.Steps, 1)
			assert.Equal(t, tt.wantDecision, plan.Steps[0].Decision)
			assert.Equal(t, tt.wantReplacement, plan.Steps[0].ReplacementReasons)
		})
	}
}
