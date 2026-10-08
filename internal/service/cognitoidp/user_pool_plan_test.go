package cognitoidp

import (
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userPoolPlanProbe UserPoolResource

type userPoolClientPlanProbe UserPoolClientResource

type userPoolPlanOutput struct {
	UserPoolID string `ub:"user-pool-id"`
}

type userPoolClientPlanOutput struct {
	ID   string `ub:"id"`
	Name string `ub:"name"`
}

func (*userPoolPlanProbe) Create(context.Context, any) (*userPoolPlanOutput, error) {
	return &userPoolPlanOutput{UserPoolID: "new-id"}, nil
}

func (*userPoolPlanProbe) Read(
	_ context.Context,
	_ any,
	prior runtime.Prior[userPoolPlanProbe, *userPoolPlanOutput, any],
) (*userPoolPlanOutput, error) {
	return prior.Outputs, nil
}

func (*userPoolPlanProbe) Update(
	_ context.Context,
	_ any,
	prior runtime.Prior[userPoolPlanProbe, *userPoolPlanOutput, any],
) (*userPoolPlanOutput, error) {
	return prior.Outputs, nil
}

func (*userPoolPlanProbe) Delete(
	context.Context, any, runtime.Prior[userPoolPlanProbe, *userPoolPlanOutput, any],
) error {
	return nil
}

func poolEqual[Value any](
	field runtime.InputDescriptor[userPoolPlanProbe, Value],
	name string,
	toInput func(Value) UserPoolResource,
) runtime.InputEqualityRule[userPoolPlanProbe] {
	return runtime.EqualBy(field, func(prior, desired Value) bool {
		return (&UserPoolResource{}).EquivalentInput(
			name, toInput(prior), toInput(desired),
		)
	})
}

func (*userPoolPlanProbe) ResourceDefinition() runtime.ResourceDefinition[
	userPoolPlanProbe, *userPoolPlanOutput, any,
] {
	return runtime.ResourceDefinition[userPoolPlanProbe, *userPoolPlanOutput, any]{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[userPoolPlanProbe]{
			poolEqual(runtime.InputField(func(input *userPoolPlanProbe) **[]string {
				return &input.AliasAttributes
			}), "alias-attributes", func(value *[]string) UserPoolResource {
				return UserPoolResource{AliasAttributes: value}
			}),
			poolEqual(runtime.InputField(func(input *userPoolPlanProbe) **[]string {
				return &input.UsernameAttributes
			}), "username-attributes", func(value *[]string) UserPoolResource {
				return UserPoolResource{UsernameAttributes: value}
			}),
			poolEqual(runtime.InputField(func(input *userPoolPlanProbe) **[]string {
				return &input.AutoVerifiedAttributes
			}), "auto-verified-attributes", func(value *[]string) UserPoolResource {
				return UserPoolResource{AutoVerifiedAttributes: value}
			}),
			poolEqual(runtime.InputField(func(input *userPoolPlanProbe) **[]string {
				return &input.EnabledMFAs
			}), "enabled-mfas", func(value *[]string) UserPoolResource {
				return UserPoolResource{EnabledMFAs: value}
			}),
			poolEqual(runtime.InputField(func(input *userPoolPlanProbe) **[]UserPoolSchemaAttribute {
				return &input.Schema
			}), "schema", func(value *[]UserPoolSchemaAttribute) UserPoolResource {
				return UserPoolResource{Schema: value}
			}),
			poolEqual(runtime.InputField(func(input *userPoolPlanProbe) **UserPoolAccountRecoverySetting {
				return &input.AccountRecoverySetting
			}), "account-recovery-setting", func(value *UserPoolAccountRecoverySetting) UserPoolResource {
				return UserPoolResource{AccountRecoverySetting: value}
			}),
			poolEqual(runtime.InputField(func(input *userPoolPlanProbe) **UserPoolSignInPolicy {
				return &input.SignInPolicy
			}), "sign-in-policy", func(value *UserPoolSignInPolicy) UserPoolResource {
				return UserPoolResource{SignInPolicy: value}
			}),
			poolEqual(runtime.InputField(func(input *userPoolPlanProbe) **UserPoolAttributeUpdateSettings {
				return &input.UserAttributeUpdateSettings
			}), "user-attribute-update-settings", func(value *UserPoolAttributeUpdateSettings) UserPoolResource {
				return UserPoolResource{UserAttributeUpdateSettings: value}
			}),
		},
		Replace: runtime.Replacement[userPoolPlanProbe, *userPoolPlanOutput, any]{
			Fields: []runtime.AnyInputField[userPoolPlanProbe]{
				runtime.InputField(func(input *userPoolPlanProbe) **[]string {
					return &input.AliasAttributes
				}),
				runtime.InputField(func(input *userPoolPlanProbe) **[]string {
					return &input.UsernameAttributes
				}),
				runtime.InputField(func(input *userPoolPlanProbe) **UserPoolUsernameConfiguration {
					return &input.UsernameConfiguration
				}),
			},
		},
	}
}

func (*userPoolPlanProbe) EquivalentInput(
	field string,
	prior userPoolPlanProbe,
	current userPoolPlanProbe,
) bool {
	return (&UserPoolResource{}).EquivalentInput(
		field,
		UserPoolResource(prior),
		UserPoolResource(current),
	)
}

func (*userPoolClientPlanProbe) Create(context.Context, any) (*userPoolClientPlanOutput, error) {
	return &userPoolClientPlanOutput{ID: "new-id", Name: "client-name"}, nil
}

func (*userPoolClientPlanProbe) Read(
	_ context.Context,
	_ any,
	prior runtime.Prior[userPoolClientPlanProbe, *userPoolClientPlanOutput, any],
) (*userPoolClientPlanOutput, error) {
	return prior.Outputs, nil
}

func (*userPoolClientPlanProbe) Update(
	_ context.Context,
	_ any,
	prior runtime.Prior[userPoolClientPlanProbe, *userPoolClientPlanOutput, any],
) (*userPoolClientPlanOutput, error) {
	return prior.Outputs, nil
}

func (*userPoolClientPlanProbe) Delete(
	context.Context, any, runtime.Prior[userPoolClientPlanProbe, *userPoolClientPlanOutput, any],
) error {
	return nil
}

func (*userPoolClientPlanProbe) ResourceDefinition() runtime.ResourceDefinition[
	userPoolClientPlanProbe, *userPoolClientPlanOutput, any,
] {
	return runtime.ResourceDefinition[userPoolClientPlanProbe, *userPoolClientPlanOutput, any]{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[userPoolClientPlanProbe]{
			runtime.EqualBy(
				runtime.InputField(func(input *userPoolClientPlanProbe) **bool {
					return &input.GenerateSecret
				}),
				func(prior, desired *bool) bool {
					return (&UserPoolClientResource{}).EquivalentInput("generate-secret",
						UserPoolClientResource{GenerateSecret: prior},
						UserPoolClientResource{GenerateSecret: desired})
				},
			),
		},
		Replace: runtime.Replacement[userPoolClientPlanProbe, *userPoolClientPlanOutput, any]{
			Fields: []runtime.AnyInputField[userPoolClientPlanProbe]{
				runtime.InputField(func(input *userPoolClientPlanProbe) *string {
					return &input.UserPoolID
				}),
				runtime.InputField(func(input *userPoolClientPlanProbe) **bool {
					return &input.GenerateSecret
				}),
			},
		},
	}
}

func (*userPoolClientPlanProbe) EquivalentInput(
	field string,
	prior userPoolClientPlanProbe,
	current userPoolClientPlanProbe,
) bool {
	return (&UserPoolClientResource{}).EquivalentInput(
		field,
		UserPoolClientResource(prior),
		UserPoolClientResource(current),
	)
}

func TestUserPoolClientGenerateSecretPlans(t *testing.T) {
	tests := []struct {
		name     string
		prior    map[string]any
		current  string
		decision runtime.Decision
		triggers []string
	}{
		{
			name:     "omitted to false is no-op",
			prior:    map[string]any{},
			current:  "generate-secret: false",
			decision: runtime.DecisionNoOp,
		},
		{
			name:     "false to omitted is no-op",
			prior:    map[string]any{"generate-secret": false},
			decision: runtime.DecisionNoOp,
		},
		{
			name:     "false to true replaces",
			prior:    map[string]any{"generate-secret": false},
			current:  "generate-secret: true",
			decision: runtime.DecisionReplace,
			triggers: []string{"generate-secret"},
		},
		{
			name:     "true to false replaces",
			prior:    map[string]any{"generate-secret": true},
			current:  "generate-secret: false",
			decision: runtime.DecisionReplace,
			triggers: []string{"generate-secret"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := planUserPoolClientChange(t, tt.prior, tt.current)
			assert.Equal(t, tt.decision, step.Decision)
			assert.Equal(t, tt.triggers, step.ReplacementReasons)
		})
	}
}

func planUserPoolClientChange(
	t *testing.T,
	prior map[string]any,
	current string,
) *runtime.PlanStep {
	t.Helper()
	source := fmt.Sprintf(`factory: {
  resources: {
    client: aws-cognitoidp.user-pool-client {
      user-pool-id: 'us-east-1_example'
      %s
    }
  }
}`, current)
	parsed, err := syntax.ParseSource("factory.ub", []byte(source))
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)
	body := parsed.Factory.Body
	libraries := map[string]*runtime.Library{
		"aws-cognitoidp": {
			Name: "aws-cognitoidp",
			Resources: map[string]runtime.ResourceRegistration{
				"user-pool-client": runtime.MakeResource[
					userPoolClientPlanProbe,
					*userPoolClientPlanOutput,
					any,
				]((&userPoolClientPlanProbe{}).ResourceDefinition()),
			},
		},
	}
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	factory := state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"}
	snapshot := state.NewSnapshot(factory, store.Stack())
	inputs := map[string]any{"user-pool-id": "us-east-1_example"}
	maps.Copy(inputs, prior)
	snapshot.Entries = []*state.Entry{{
		Address:       "resource.client",
		Composite:     false,
		Category:      "resource",
		Binding:       &state.Binding{Alias: "aws-cognitoidp", Export: "user-pool-client"},
		SchemaVersion: 1,
		Inputs:        inputs,
		Outputs:       map[string]any{"id": "client-id", "name": "client-name"},
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
	return plan.Steps[0]
}

func TestUserPoolUnorderedCollectionPlans(t *testing.T) {
	tests := []struct {
		name         string
		priorField   string
		priorValue   any
		currentField string
		decision     runtime.Decision
		triggers     []string
	}{
		{
			name:         "alias reorder is no-op",
			priorField:   "alias-attributes",
			priorValue:   []any{"email", "phone_number"},
			currentField: "alias-attributes: ['phone_number', 'email']",
			decision:     runtime.DecisionNoOp,
		},
		{
			name:         "alias removal replaces",
			priorField:   "alias-attributes",
			priorValue:   []any{"email", "phone_number"},
			currentField: "alias-attributes: ['email']",
			decision:     runtime.DecisionReplace,
			triggers:     []string{"alias-attributes"},
		},
		{
			name:         "username reorder is no-op",
			priorField:   "username-attributes",
			priorValue:   []any{"email", "phone_number"},
			currentField: "username-attributes: ['phone_number', 'email']",
			decision:     runtime.DecisionNoOp,
		},
		{
			name:         "username removal replaces",
			priorField:   "username-attributes",
			priorValue:   []any{"email", "phone_number"},
			currentField: "username-attributes: ['email']",
			decision:     runtime.DecisionReplace,
			triggers:     []string{"username-attributes"},
		},
		{
			name:         "auto verified reorder is no-op",
			priorField:   "auto-verified-attributes",
			priorValue:   []any{"email", "phone_number"},
			currentField: "auto-verified-attributes: ['phone_number', 'email']",
			decision:     runtime.DecisionNoOp,
		},
		{
			name:         "auto verified removal updates",
			priorField:   "auto-verified-attributes",
			priorValue:   []any{"email", "phone_number"},
			currentField: "auto-verified-attributes: ['email']",
			decision:     runtime.DecisionUpdate,
		},
		{
			name:         "enabled mfas reorder is no-op",
			priorField:   "enabled-mfas",
			priorValue:   []any{"SMS_MFA", "SOFTWARE_TOKEN_MFA"},
			currentField: "enabled-mfas: ['SOFTWARE_TOKEN_MFA', 'SMS_MFA']",
			decision:     runtime.DecisionNoOp,
		},
		{
			name:         "enabled mfas removal updates",
			priorField:   "enabled-mfas",
			priorValue:   []any{"SMS_MFA", "SOFTWARE_TOKEN_MFA"},
			currentField: "enabled-mfas: ['SOFTWARE_TOKEN_MFA']",
			decision:     runtime.DecisionUpdate,
		},
		{
			name:       "schema reorder is no-op",
			priorField: "schema",
			priorValue: []any{
				map[string]any{"name": "first", "attribute-data-type": "String"},
				map[string]any{"name": "second", "attribute-data-type": "Number"},
			},
			currentField: `schema: [
		{ name: 'second', attribute-data-type: 'Number' },
		{ name: 'first', attribute-data-type: 'String' }
	]`,
			decision: runtime.DecisionNoOp,
		},
		{
			name:       "schema member modification updates",
			priorField: "schema",
			priorValue: []any{
				map[string]any{"name": "first", "attribute-data-type": "String"},
				map[string]any{"name": "second", "attribute-data-type": "Number"},
			},
			currentField: `schema: [
		{ name: 'second', attribute-data-type: 'Boolean' },
		{ name: 'first', attribute-data-type: 'String' }
	]`,
			decision: runtime.DecisionUpdate,
		},
		{
			name:       "recovery mechanism reorder is no-op",
			priorField: "account-recovery-setting",
			priorValue: map[string]any{"recovery-mechanisms": []any{
				map[string]any{"name": "verified_email", "priority": int64(1)},
				map[string]any{"name": "verified_phone_number", "priority": int64(2)},
			}},
			currentField: `account-recovery-setting: {
		recovery-mechanisms: [
		  { name: 'verified_phone_number', priority: 2 },
		  { name: 'verified_email', priority: 1 }
		]
      }`,
			decision: runtime.DecisionNoOp,
		},
		{
			name:       "recovery mechanism modification updates",
			priorField: "account-recovery-setting",
			priorValue: map[string]any{"recovery-mechanisms": []any{
				map[string]any{"name": "verified_email", "priority": int64(1)},
				map[string]any{"name": "verified_phone_number", "priority": int64(2)},
			}},
			currentField: `account-recovery-setting: {
		recovery-mechanisms: [
		  { name: 'verified_phone_number', priority: 1 },
		  { name: 'verified_email', priority: 2 }
		]
      }`,
			decision: runtime.DecisionUpdate,
		},
		{
			name:       "first auth factor reorder is no-op",
			priorField: "sign-in-policy",
			priorValue: map[string]any{
				"allowed-first-auth-factors": []any{"PASSWORD", "WEB_AUTHN"},
			},
			currentField: `sign-in-policy: {
        allowed-first-auth-factors: ['WEB_AUTHN', 'PASSWORD']
      }`,
			decision: runtime.DecisionNoOp,
		},
		{
			name:       "first auth factor removal updates",
			priorField: "sign-in-policy",
			priorValue: map[string]any{
				"allowed-first-auth-factors": []any{"PASSWORD", "WEB_AUTHN"},
			},
			currentField: `sign-in-policy: {
        allowed-first-auth-factors: ['PASSWORD']
      }`,
			decision: runtime.DecisionUpdate,
		},
		{
			name:       "verification attribute reorder is no-op",
			priorField: "user-attribute-update-settings",
			priorValue: map[string]any{
				"attributes-require-verification-before-update": []any{
					"email", "phone_number",
				},
			},
			currentField: `user-attribute-update-settings: {
        attributes-require-verification-before-update: ['phone_number', 'email']
      }`,
			decision: runtime.DecisionNoOp,
		},
		{
			name:       "verification attribute removal updates",
			priorField: "user-attribute-update-settings",
			priorValue: map[string]any{
				"attributes-require-verification-before-update": []any{
					"email", "phone_number",
				},
			},
			currentField: `user-attribute-update-settings: {
        attributes-require-verification-before-update: ['email']
      }`,
			decision: runtime.DecisionUpdate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := planUserPoolChange(t, tt.priorField, tt.priorValue, tt.currentField)
			assert.Equal(t, tt.decision, step.Decision)
			assert.Equal(t, tt.triggers, step.ReplacementReasons)
		})
	}
}

func planUserPoolChange(
	t *testing.T,
	priorField string,
	priorValue any,
	currentField string,
) *runtime.PlanStep {
	t.Helper()
	source := fmt.Sprintf(`factory: {
  resources: {
    pool: aws-cognitoidp.user-pool {
      name: 'pool-name'
      %s
    }
  }
}`, currentField)
	parsed, err := syntax.ParseSource("factory.ub", []byte(source))
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)
	body := parsed.Factory.Body
	libraries := map[string]*runtime.Library{
		"aws-cognitoidp": {
			Name: "aws-cognitoidp",
			Resources: map[string]runtime.ResourceRegistration{
				"user-pool": runtime.MakeResource[
					userPoolPlanProbe,
					*userPoolPlanOutput,
					any,
				]((&userPoolPlanProbe{}).ResourceDefinition()),
			},
		},
	}
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	factory := state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"}
	snapshot := state.NewSnapshot(factory, store.Stack())
	inputs := map[string]any{"name": "pool-name", priorField: priorValue}
	snapshot.Entries = []*state.Entry{
		{
			Address:       "resource.pool",
			Composite:     false,
			Category:      "resource",
			Binding:       &state.Binding{Alias: "aws-cognitoidp", Export: "user-pool"},
			SchemaVersion: 1,
			Inputs:        inputs,
			Outputs:       map[string]any{"user-pool-id": "prior-id"},
		},
	}
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
	return plan.Steps[0]
}
