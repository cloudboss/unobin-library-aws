package iam

import (
	"context"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const updateRoleResponseXML = `
<UpdateRoleResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <UpdateRoleResult/>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</UpdateRoleResponse>`

// TestRoleUpdateLeavesRemovedOptionsToAWS removes the description and the
// session limit between applies and checks the update sends no UpdateRole at
// all. A nil option means the value is AWS's to decide: a removed description
// must not be turned into an explicit empty string, which clears it, and a
// removed session limit must not produce an UpdateRole that names no field.
func TestRoleUpdateLeavesRemovedOptionsToAWS(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("UpdateRole", func(int, url.Values) (int, string) {
		return 200, updateRoleResponseXML
	})
	cfg := fake.configuration()

	base := Role{
		RoleName:                 "test-role",
		AssumeRolePolicyDocument: `{"Version":"2012-10-17","Statement":[]}`,
	}
	priorInputs := base
	priorInputs.Description = aws.String("a role description")
	priorInputs.MaxSessionDuration = aws.Int64(7200)

	current := base
	prior := runtime.Prior[Role, *RoleOutput]{
		Inputs: priorInputs,
		Outputs: &RoleOutput{
			Arn:    "arn:aws:iam::123456789012:role/test-role",
			RoleId: "AROA0123456789EXAMPLE",
		},
	}
	out, err := current.Update(context.Background(), cfg, prior)
	require.NoError(t, err)
	assert.Equal(t, prior.Outputs, out)
	assert.Empty(t, fake.sent("UpdateRole"),
		"removing the description or session limit must not send UpdateRole")
}
