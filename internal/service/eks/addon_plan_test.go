package eks

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type addonPlanProbe AddonResource

func (*addonPlanProbe) SchemaVersion() int { return 1 }

func (*addonPlanProbe) Create(context.Context, any) (map[string]any, error) {
	return map[string]any{"arn": "arn:new"}, nil
}

func (*addonPlanProbe) Read(
	_ context.Context,
	_ any,
	prior map[string]any,
) (map[string]any, error) {
	return prior, nil
}

func (*addonPlanProbe) Update(
	_ context.Context,
	_ any,
	prior runtime.Prior[addonPlanProbe, map[string]any],
) (map[string]any, error) {
	return prior.Outputs, nil
}

func (*addonPlanProbe) Delete(context.Context, any, map[string]any) error {
	return nil
}

func (*addonPlanProbe) ReplaceFields() []string {
	return (&AddonResource{}).ReplaceFields()
}

func (*addonPlanProbe) EquivalentInput(
	field string,
	prior addonPlanProbe,
	current addonPlanProbe,
) bool {
	return (&AddonResource{}).EquivalentInput(
		field, AddonResource(prior), AddonResource(current),
	)
}

func TestAddonRuntimePlanReplacesCompositeIdentityAndNamespace(t *testing.T) {
	tests := []struct {
		name    string
		trigger string
		prior   AddonResource
		modify  func(*AddonResource)
	}{
		{
			name: "cluster name", trigger: "cluster-name", prior: validAddonResource(),
			modify: func(current *AddonResource) { current.ClusterName = "new-cluster" },
		},
		{
			name: "add-on name", trigger: "addon-name", prior: validAddonResource(),
			modify: func(current *AddonResource) { current.AddonName = "kube-proxy" },
		},
		{
			name: "namespace addition", trigger: "namespace-config",
			prior: validAddonResource(),
			modify: func(current *AddonResource) {
				current.NamespaceConfig = &AddonNamespaceConfig{Namespace: "kube-system"}
			},
		},
		{
			name: "namespace change", trigger: "namespace-config",
			prior: addonWithNamespace("kube-system"),
			modify: func(current *AddonResource) {
				current.NamespaceConfig = &AddonNamespaceConfig{Namespace: "dns"}
			},
		},
		{
			name: "namespace removal", trigger: "namespace-config",
			prior:  addonWithNamespace("kube-system"),
			modify: func(current *AddonResource) { current.NamespaceConfig = nil },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := test.prior
			test.modify(&current)
			step := planAddonChange(t, test.prior, current)
			assert.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.Equal(t, []string{test.trigger}, step.ReplaceTriggers)
		})
	}
}

func TestAddonRuntimePlanTreatsPodIdentityAssociationsAsUnordered(t *testing.T) {
	first := AddonPodIdentityAssociation{
		RoleARN: "arn:aws:iam::123456789012:role/first", ServiceAccount: "first",
	}
	second := AddonPodIdentityAssociation{
		RoleARN: "arn:aws:iam::123456789012:role/second", ServiceAccount: "second",
	}
	prior := validAddonResource()
	prior.PodIdentityAssociation = &[]AddonPodIdentityAssociation{first, second}
	current := prior
	current.PodIdentityAssociation = &[]AddonPodIdentityAssociation{second, first}

	step := planAddonChange(t, prior, current)

	assert.Equal(t, runtime.DecisionNoOp, step.Decision)
	assert.Empty(t, step.ReplaceTriggers)
}

func TestAddonRuntimePlanUpdatesChangedPodIdentityAssociation(t *testing.T) {
	prior := validAddonResource()
	prior.PodIdentityAssociation = &[]AddonPodIdentityAssociation{{
		RoleARN: "arn:aws:iam::123456789012:role/old", ServiceAccount: "coredns",
	}}
	current := prior
	current.PodIdentityAssociation = &[]AddonPodIdentityAssociation{{
		RoleARN: "arn:aws:iam::123456789012:role/new", ServiceAccount: "coredns",
	}}

	step := planAddonChange(t, prior, current)

	assert.Equal(t, runtime.DecisionUpdate, step.Decision)
	assert.Empty(t, step.ReplaceTriggers)
}

func planAddonChange(
	t *testing.T,
	prior AddonResource,
	current AddonResource,
) *runtime.PlanStep {
	t.Helper()
	parsed, err := syntax.ParseSource("factory.ub", []byte(addonPlanSource(current)))
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)
	body := parsed.Factory.Body
	libraries := map[string]*runtime.Library{
		"aws-eks": {
			Name: "aws-eks",
			Resources: map[string]runtime.ResourceRegistration{
				"addon": runtime.MakeResource[addonPlanProbe, map[string]any, any](),
			},
		},
	}
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	factory := state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"}
	snapshot := state.NewSnapshot(factory, store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address:       "resource.addon",
		Type:          state.EntryLeaf,
		Category:      "resource",
		Binding:       &state.Binding{Alias: "aws-eks", Export: "addon"},
		SchemaVersion: 1,
		Inputs:        addonPlanInputs(prior),
		Outputs: map[string]any{
			"cluster-name": prior.ClusterName,
			"addon-name":   prior.AddonName,
			"arn":          "arn:prior",
		},
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

func addonPlanSource(resource AddonResource) string {
	var optional strings.Builder
	if resource.NamespaceConfig != nil {
		fmt.Fprintf(&optional, "      namespace-config: { namespace: '%s' }\n",
			resource.NamespaceConfig.Namespace)
	}
	if resource.PodIdentityAssociation != nil {
		optional.WriteString("      pod-identity-association: [")
		for index, association := range *resource.PodIdentityAssociation {
			if index > 0 {
				optional.WriteString(", ")
			}
			fmt.Fprintf(&optional, "{ role-arn: '%s', service-account: '%s' }",
				association.RoleARN, association.ServiceAccount)
		}
		optional.WriteString("]\n")
	}
	return fmt.Sprintf(`factory: {
  resources: {
    addon: aws-eks.addon {
      cluster-name: '%s'
      addon-name:   '%s'
      preserve:     false
%s    }
  }
}`,
		resource.ClusterName,
		resource.AddonName,
		optional.String(),
	)
}

func addonPlanInputs(resource AddonResource) map[string]any {
	inputs := map[string]any{
		"cluster-name": resource.ClusterName,
		"addon-name":   resource.AddonName,
		"preserve":     resource.Preserve,
	}
	if resource.NamespaceConfig != nil {
		inputs["namespace-config"] = map[string]any{
			"namespace": resource.NamespaceConfig.Namespace,
		}
	}
	if resource.PodIdentityAssociation != nil {
		associations := make([]any, 0, len(*resource.PodIdentityAssociation))
		for _, association := range *resource.PodIdentityAssociation {
			associations = append(associations, map[string]any{
				"role-arn":        association.RoleARN,
				"service-account": association.ServiceAccount,
			})
		}
		inputs["pod-identity-association"] = associations
	}
	return inputs
}

func addonWithNamespace(namespace string) AddonResource {
	resource := validAddonResource()
	resource.NamespaceConfig = &AddonNamespaceConfig{Namespace: namespace}
	return resource
}
