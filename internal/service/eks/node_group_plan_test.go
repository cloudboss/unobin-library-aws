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

type nodeGroupPlanProbe NodeGroupResource

type nodeGroupPlanOutput struct {
	ARN string `ub:"arn"`
}

func (*nodeGroupPlanProbe) Create(context.Context, any) (*nodeGroupPlanOutput, error) {
	return &nodeGroupPlanOutput{ARN: "arn:new"}, nil
}

func (*nodeGroupPlanProbe) Read(
	_ context.Context,
	_ any,
	prior runtime.Prior[nodeGroupPlanProbe, *nodeGroupPlanOutput, any],
) (*nodeGroupPlanOutput, error) {
	return prior.Outputs, nil
}

func (*nodeGroupPlanProbe) Update(
	_ context.Context,
	_ any,
	prior runtime.Prior[nodeGroupPlanProbe, *nodeGroupPlanOutput, any],
) (*nodeGroupPlanOutput, error) {
	return prior.Outputs, nil
}

func (*nodeGroupPlanProbe) Delete(
	context.Context, any, runtime.Prior[nodeGroupPlanProbe, *nodeGroupPlanOutput, any],
) error {
	return nil
}

func (*nodeGroupPlanProbe) ResourceDefinition() runtime.ResourceDefinition[
	nodeGroupPlanProbe, *nodeGroupPlanOutput, any,
] {
	equalField := func(field string, prior, desired NodeGroupResource) bool {
		return (&NodeGroupResource{}).EquivalentInput(field, prior, desired)
	}
	return runtime.ResourceDefinition[nodeGroupPlanProbe, *nodeGroupPlanOutput, any]{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[nodeGroupPlanProbe]{
			runtime.EqualBy(
				runtime.InputField(func(input *nodeGroupPlanProbe) *[]string {
					return &input.SubnetIDs
				}),
				func(prior, desired []string) bool {
					return equalField("subnet-ids",
						NodeGroupResource{SubnetIDs: prior}, NodeGroupResource{SubnetIDs: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *nodeGroupPlanProbe) **NodeGroupRemoteAccess {
					return &input.RemoteAccess
				}),
				func(prior, desired *NodeGroupRemoteAccess) bool {
					return equalField("remote-access",
						NodeGroupResource{RemoteAccess: prior}, NodeGroupResource{RemoteAccess: desired})
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *nodeGroupPlanProbe) **[]NodeGroupTaint {
					return &input.Taints
				}),
				func(prior, desired *[]NodeGroupTaint) bool {
					return equalField("taints",
						NodeGroupResource{Taints: prior}, NodeGroupResource{Taints: desired})
				},
			),
		},
		Replace: runtime.Replacement[nodeGroupPlanProbe, *nodeGroupPlanOutput, any]{
			Fields: []runtime.AnyInputField[nodeGroupPlanProbe]{
				runtime.InputField(func(input *nodeGroupPlanProbe) *string { return &input.ClusterName }),
				runtime.InputField(func(input *nodeGroupPlanProbe) *string { return &input.NodeGroupName }),
				runtime.InputField(func(input *nodeGroupPlanProbe) *string { return &input.NodeRoleARN }),
				runtime.InputField(func(input *nodeGroupPlanProbe) *[]string { return &input.SubnetIDs }),
				runtime.InputField(func(input *nodeGroupPlanProbe) **string { return &input.AMIType }),
				runtime.InputField(func(input *nodeGroupPlanProbe) **string { return &input.CapacityType }),
				runtime.InputField(func(input *nodeGroupPlanProbe) **int64 { return &input.DiskSize }),
				runtime.InputField(func(input *nodeGroupPlanProbe) **[]string { return &input.InstanceTypes }),
				runtime.InputField(func(input *nodeGroupPlanProbe) **NodeGroupRemoteAccess {
					return &input.RemoteAccess
				}),
				runtime.InputField(func(input *nodeGroupPlanProbe) **string { return &input.LaunchTemplateID }),
				runtime.InputField(func(input *nodeGroupPlanProbe) **string { return &input.LaunchTemplateName }),
			},
		},
	}
}

func (*nodeGroupPlanProbe) EquivalentInput(
	field string,
	prior nodeGroupPlanProbe,
	current nodeGroupPlanProbe,
) bool {
	return (&NodeGroupResource{}).EquivalentInput(
		field,
		NodeGroupResource(prior),
		NodeGroupResource(current),
	)
}

func TestNodeGroupRuntimePlanReplacesEveryStaticField(t *testing.T) {
	tests := []struct {
		name    string
		trigger string
		modify  func(*NodeGroupResource, *NodeGroupResource)
	}{
		{name: "cluster name", trigger: "cluster-name",
			modify: func(_, current *NodeGroupResource) { current.ClusterName = "new-cluster" }},
		{name: "node group name", trigger: "node-group-name",
			modify: func(_, current *NodeGroupResource) { current.NodeGroupName = "new-workers" }},
		{name: "node role", trigger: "node-role-arn",
			modify: func(_, current *NodeGroupResource) { current.NodeRoleARN += "-new" }},
		{name: "subnets", trigger: "subnet-ids",
			modify: func(_, current *NodeGroupResource) {
				current.SubnetIDs = []string{"subnet-a", "subnet-c"}
			}},
		{name: "AMI type", trigger: "ami-type",
			modify: func(prior, current *NodeGroupResource) {
				prior.AMIType = stringPointer("AL2_x86_64")
				current.AMIType = stringPointer("AL2023_x86_64_STANDARD")
			}},
		{name: "capacity type", trigger: "capacity-type",
			modify: func(prior, current *NodeGroupResource) {
				prior.CapacityType = stringPointer("ON_DEMAND")
				current.CapacityType = stringPointer("SPOT")
			}},
		{name: "disk size", trigger: "disk-size",
			modify: func(prior, current *NodeGroupResource) {
				prior.DiskSize = int64Pointer(20)
				current.DiskSize = int64Pointer(40)
			}},
		{name: "instance types", trigger: "instance-types",
			modify: func(prior, current *NodeGroupResource) {
				prior.InstanceTypes = &[]string{"m7i.large"}
				current.InstanceTypes = &[]string{"m7i.xlarge"}
			}},
		{name: "remote access", trigger: "remote-access",
			modify: func(prior, current *NodeGroupResource) {
				prior.RemoteAccess = &NodeGroupRemoteAccess{EC2SSHKey: "old-key"}
				current.RemoteAccess = &NodeGroupRemoteAccess{EC2SSHKey: "new-key"}
			}},
		{name: "launch template ID", trigger: "launch-template-id",
			modify: func(prior, current *NodeGroupResource) {
				prior.LaunchTemplateID = stringPointer("lt-old")
				current.LaunchTemplateID = stringPointer("lt-new")
			}},
		{name: "launch template name", trigger: "launch-template-name",
			modify: func(prior, current *NodeGroupResource) {
				prior.LaunchTemplateName = stringPointer("old-template")
				current.LaunchTemplateName = stringPointer("new-template")
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prior := validNodeGroupResource()
			current := cloneNodeGroupResource(t, prior)
			tt.modify(&prior, &current)

			step := planNodeGroupChange(t, prior, current)

			assert.Equal(t, runtime.DecisionReplace, step.Decision)
			assert.Equal(t, []string{tt.trigger}, step.ReplacementReasons)
		})
	}
}

func TestNodeGroupRuntimePlanUpdatesLaunchTemplateVersion(t *testing.T) {
	prior := validNodeGroupResource()
	prior.LaunchTemplateID = stringPointer("lt-123")
	prior.LaunchTemplateVersion = stringPointer("1")
	current := cloneNodeGroupResource(t, prior)
	current.LaunchTemplateVersion = stringPointer("2")

	step := planNodeGroupChange(t, prior, current)

	assert.Equal(t, runtime.DecisionUpdate, step.Decision)
	assert.Empty(t, step.ReplacementReasons)
}

func TestNodeGroupRuntimePlanCollectionOrder(t *testing.T) {
	tests := []struct {
		name     string
		modify   func(*NodeGroupResource, *NodeGroupResource)
		decision runtime.Decision
		triggers []string
	}{
		{
			name: "subnet reorder is no-op",
			modify: func(_, current *NodeGroupResource) {
				current.SubnetIDs = []string{"subnet-b", "subnet-a"}
			},
			decision: runtime.DecisionNoOp,
		},
		{
			name: "remote security group reorder is no-op",
			modify: func(prior, current *NodeGroupResource) {
				prior.RemoteAccess = &NodeGroupRemoteAccess{
					EC2SSHKey:              "key",
					SourceSecurityGroupIDs: &[]string{"sg-a", "sg-b"},
				}
				current.RemoteAccess = &NodeGroupRemoteAccess{
					EC2SSHKey:              "key",
					SourceSecurityGroupIDs: &[]string{"sg-b", "sg-a"},
				}
			},
			decision: runtime.DecisionNoOp,
		},
		{
			name: "taint reorder is no-op",
			modify: func(prior, current *NodeGroupResource) {
				prior.Taints = &[]NodeGroupTaint{
					{Key: "one", Effect: "NO_SCHEDULE"},
					{Key: "two", Effect: "NO_EXECUTE"},
				}
				current.Taints = &[]NodeGroupTaint{
					{Key: "two", Effect: "NO_EXECUTE"},
					{Key: "one", Effect: "NO_SCHEDULE"},
				}
			},
			decision: runtime.DecisionNoOp,
		},
		{
			name: "instance type reorder replaces",
			modify: func(prior, current *NodeGroupResource) {
				prior.InstanceTypes = &[]string{"m7i.large", "m7i.xlarge"}
				current.InstanceTypes = &[]string{"m7i.xlarge", "m7i.large"}
			},
			decision: runtime.DecisionReplace,
			triggers: []string{"instance-types"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prior := validNodeGroupResource()
			current := cloneNodeGroupResource(t, prior)
			tt.modify(&prior, &current)

			step := planNodeGroupChange(t, prior, current)

			assert.Equal(t, tt.decision, step.Decision)
			assert.Equal(t, tt.triggers, step.ReplacementReasons)
		})
	}
}

func planNodeGroupChange(
	t *testing.T,
	prior NodeGroupResource,
	current NodeGroupResource,
) *runtime.PlanStep {
	t.Helper()
	parsed, err := syntax.ParseSource(
		"factory.ub", []byte(nodeGroupPlanSource(current)),
	)
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)
	body := parsed.Factory.Body
	libraries := map[string]*runtime.Library{
		"aws-eks": {
			Name: "aws-eks",
			Resources: map[string]runtime.ResourceRegistration{
				"node-group": runtime.MakeResource[
					nodeGroupPlanProbe,
					*nodeGroupPlanOutput,
					any,
				]((&nodeGroupPlanProbe{}).ResourceDefinition()),
			},
		},
	}
	store, err := local.NewStore(t.TempDir(), "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	factory := state.FactoryInfo{Name: "factory", Version: "v0", ContentRevision: "test"}
	snapshot := state.NewSnapshot(factory, store.Stack())
	snapshot.Entries = []*state.Entry{{
		Address:       "resource.workers",
		Type:          state.EntryLeaf,
		Category:      "resource",
		Binding:       &state.Binding{Alias: "aws-eks", Export: "node-group"},
		SchemaVersion: 1,
		Inputs:        nodeGroupPlanInputs(prior),
		Outputs:       map[string]any{"arn": "arn:prior"},
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

func nodeGroupPlanSource(resource NodeGroupResource) string {
	var optional strings.Builder
	writeOptionalNodeGroupPlanFields(&optional, resource)
	return fmt.Sprintf(`factory: {
  resources: {
    workers: aws-eks.node-group {
      cluster-name:   '%s'
      node-group-name: '%s'
      node-role-arn:   '%s'
      subnet-ids:      [%s]
      scaling-config: { desired-size: %d, min-size: %d, max-size: %d }
      force-update-version: %t
%s    }
  }
}`,
		resource.ClusterName,
		resource.NodeGroupName,
		resource.NodeRoleARN,
		nodeGroupPlanStringList(resource.SubnetIDs),
		resource.ScalingConfig.DesiredSize,
		resource.ScalingConfig.MinSize,
		resource.ScalingConfig.MaxSize,
		resource.ForceUpdateVersion,
		optional.String(),
	)
}

func writeOptionalNodeGroupPlanFields(builder *strings.Builder, resource NodeGroupResource) {
	writeNodeGroupPlanString(builder, "ami-type", resource.AMIType)
	writeNodeGroupPlanString(builder, "capacity-type", resource.CapacityType)
	if resource.DiskSize != nil {
		fmt.Fprintf(builder, "      disk-size: %d\n", *resource.DiskSize)
	}
	if resource.InstanceTypes != nil {
		fmt.Fprintf(builder, "      instance-types: [%s]\n",
			nodeGroupPlanStringList(*resource.InstanceTypes))
	}
	writeNodeGroupPlanString(builder, "launch-template-id", resource.LaunchTemplateID)
	writeNodeGroupPlanString(builder, "launch-template-name", resource.LaunchTemplateName)
	writeNodeGroupPlanString(builder, "launch-template-version", resource.LaunchTemplateVersion)
	if resource.RemoteAccess != nil {
		fmt.Fprintf(builder, "      remote-access: { ec2-ssh-key: '%s'",
			resource.RemoteAccess.EC2SSHKey)
		if resource.RemoteAccess.SourceSecurityGroupIDs != nil {
			fmt.Fprintf(builder, ", source-security-group-ids: [%s]",
				nodeGroupPlanStringList(*resource.RemoteAccess.SourceSecurityGroupIDs))
		}
		builder.WriteString(" }\n")
	}
	if resource.Taints != nil {
		builder.WriteString("      taints: [")
		for index, taint := range *resource.Taints {
			if index > 0 {
				builder.WriteString(", ")
			}
			fmt.Fprintf(builder, "{ key: '%s', effect: '%s' }", taint.Key, taint.Effect)
		}
		builder.WriteString("]\n")
	}
}

func writeNodeGroupPlanString(builder *strings.Builder, field string, value *string) {
	if value != nil {
		fmt.Fprintf(builder, "      %s: '%s'\n", field, *value)
	}
}

func nodeGroupPlanStringList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+value+"'")
	}
	return strings.Join(quoted, ", ")
}

func nodeGroupPlanInputs(resource NodeGroupResource) map[string]any {
	inputs := map[string]any{
		"cluster-name":         resource.ClusterName,
		"node-group-name":      resource.NodeGroupName,
		"node-role-arn":        resource.NodeRoleARN,
		"subnet-ids":           nodeGroupPlanAnyList(resource.SubnetIDs),
		"force-update-version": resource.ForceUpdateVersion,
		"scaling-config": map[string]any{
			"desired-size": resource.ScalingConfig.DesiredSize,
			"min-size":     resource.ScalingConfig.MinSize,
			"max-size":     resource.ScalingConfig.MaxSize,
		},
	}
	setNodeGroupPlanInput(inputs, "ami-type", resource.AMIType)
	setNodeGroupPlanInput(inputs, "capacity-type", resource.CapacityType)
	setNodeGroupPlanInput(inputs, "disk-size", resource.DiskSize)
	if resource.InstanceTypes != nil {
		inputs["instance-types"] = nodeGroupPlanAnyList(*resource.InstanceTypes)
	}
	setNodeGroupPlanInput(inputs, "launch-template-id", resource.LaunchTemplateID)
	setNodeGroupPlanInput(inputs, "launch-template-name", resource.LaunchTemplateName)
	setNodeGroupPlanInput(inputs, "launch-template-version", resource.LaunchTemplateVersion)
	if resource.RemoteAccess != nil {
		remoteAccess := map[string]any{"ec2-ssh-key": resource.RemoteAccess.EC2SSHKey}
		if resource.RemoteAccess.SourceSecurityGroupIDs != nil {
			remoteAccess["source-security-group-ids"] = nodeGroupPlanAnyList(
				*resource.RemoteAccess.SourceSecurityGroupIDs,
			)
		}
		inputs["remote-access"] = remoteAccess
	}
	if resource.Taints != nil {
		taints := make([]any, 0, len(*resource.Taints))
		for _, taint := range *resource.Taints {
			taints = append(taints, map[string]any{
				"key": taint.Key, "effect": taint.Effect,
			})
		}
		inputs["taints"] = taints
	}
	return inputs
}

func setNodeGroupPlanInput[T any](inputs map[string]any, field string, value *T) {
	if value != nil {
		inputs[field] = *value
	}
}

func nodeGroupPlanAnyList(values []string) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return items
}
