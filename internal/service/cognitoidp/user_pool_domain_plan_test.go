package cognitoidp

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPoolDomainModifyResourcePlan(t *testing.T) {
	prior := completeUserPoolDomainResource()
	current := completeUserPoolDomainResource()
	current.ManagedLoginVersion = aws.Int32(1)
	resource := current
	resp := &runtime.ResourcePlanResponse{}

	err := resource.ModifyResourcePlan(
		runtime.ResourcePlanRequest[
			UserPoolDomainResource,
			*UserPoolDomainResourceOutput,
			*awsCfg,
		]{
			HasPriorState: true,
			PriorInputs:   prior,
			CurrentInputs: current,
			PriorOutputs: &UserPoolDomainResourceOutput{
				Domain: "auth.example.com", UserPoolID: "us-east-1_pool",
			},
		},
		resp,
	)

	require.NoError(t, err)
	assert.Equal(t, map[string]bool{
		"custom-domain-config":            true,
		"managed-login-version":           true,
		"routing":                         true,
		"aws-account-id":                  true,
		"cloudfront-distribution":         true,
		"cloudfront-distribution-zone-id": true,
		"s3-bucket":                       true,
		"version":                         true,
	}, resp.UnknownOutputs)

	resp = &runtime.ResourcePlanResponse{}
	current.CustomDomainConfig = nil
	err = current.ModifyResourcePlan(
		runtime.ResourcePlanRequest[
			UserPoolDomainResource,
			*UserPoolDomainResourceOutput,
			*awsCfg,
		]{
			HasPriorState: true,
			PriorInputs:   prior,
			CurrentInputs: current,
		},
		resp,
	)
	require.NoError(t, err)
	assert.Empty(t, resp.UnknownOutputs, "prefix/custom transitions are handled as update errors")
}
