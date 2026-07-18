package eks

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func validNodeGroupResource() NodeGroupResource {
	return NodeGroupResource{
		ClusterName:   "example",
		NodeGroupName: "workers",
		NodeRoleARN:   "arn:aws:iam::123456789012:role/node",
		SubnetIDs:     []string{"subnet-a", "subnet-b"},
		ScalingConfig: NodeGroupScalingConfig{DesiredSize: 0, MinSize: 0, MaxSize: 1},
	}
}

func cloneNodeGroupResource(t *testing.T, resource NodeGroupResource) NodeGroupResource {
	t.Helper()
	encoded, err := json.Marshal(resource)
	require.NoError(t, err)
	var clone NodeGroupResource
	require.NoError(t, json.Unmarshal(encoded, &clone))
	return clone
}

func int64Pointer(value int64) *int64 { return &value }
