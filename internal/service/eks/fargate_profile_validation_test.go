package eks

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validFargateProfileResource() FargateProfileResource {
	return FargateProfileResource{
		ClusterName:         "example",
		FargateProfileName:  "pods",
		PodExecutionRoleARN: "arn:aws:iam::123456789012:role/pods",
		Selector: []FargateProfileSelector{{
			Namespace: "default",
			Labels:    &map[string]string{"app": "api"},
		}},
	}
}

func TestFargateProfileValidateInputs(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*FargateProfileResource)
		match  string
	}{
		{name: "empty cluster name", modify: func(r *FargateProfileResource) {
			r.ClusterName = ""
		}, match: "cluster-name"},
		{name: "invalid cluster name", modify: func(r *FargateProfileResource) {
			r.ClusterName = "-bad"
		}, match: "cluster-name"},
		{name: "long cluster name", modify: func(r *FargateProfileResource) {
			r.ClusterName = strings.Repeat("a", 101)
		}, match: "cluster-name"},
		{name: "empty profile name", modify: func(r *FargateProfileResource) {
			r.FargateProfileName = ""
		}, match: "fargate-profile-name"},
		{name: "empty role", modify: func(r *FargateProfileResource) {
			r.PodExecutionRoleARN = ""
		}, match: "pod-execution-role-arn"},
		{name: "empty selector list", modify: func(r *FargateProfileResource) {
			r.Selector = nil
		}, match: "selector"},
		{name: "empty namespace", modify: func(r *FargateProfileResource) {
			r.Selector = []FargateProfileSelector{{}}
		}, match: "selector.namespace"},
		{name: "empty subnet IDs", modify: func(r *FargateProfileResource) {
			r.SubnetIDs = &[]string{}
		}, match: "subnet-ids"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := validFargateProfileResource()
			tt.modify(&resource)
			err := resource.ValidateInputs(t.Context(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.match)
		})
	}
}
