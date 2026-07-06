package lambda

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const getFunctionResponseJSON = `{
  "Configuration": {
    "FunctionName": "fn",
    "FunctionArn": "arn:aws:lambda:us-east-1:123456789012:function:fn",
    "State": "Active",
    "LastUpdateStatus": "Successful",
    "CodeSha256": "abc123",
    "CodeSize": 1,
    "LastModified": "2026-06-05T00:00:00.000+0000"
  },
  "Code": {"RepositoryType": "S3", "Location": "https://example"}
}`

// TestFunctionUpdateLeavesRemovedScalarsToAWS removes the description, the
// handler, and the KMS key between applies and checks the configuration
// update does not turn any of them into an explicit empty string, which
// Lambda reads as a clear. A nil scalar is never sent, so the function keeps
// each value until an apply sets it again.
func TestFunctionUpdateLeavesRemovedScalarsToAWS(t *testing.T) {
	fake := newFakeLambda(t)
	const configRoute = "PUT /2015-03-31/functions/fn/configuration"
	fake.on(configRoute, func(int) (int, string) { return 200, `{}` })
	fake.on("GET /2015-03-31/functions/fn", func(int) (int, string) {
		return 200, getFunctionResponseJSON
	})
	fake.on("GET /2015-03-31/functions/fn/versions", func(int) (int, string) {
		return 200, `{"Versions": []}`
	})
	cfg := fake.configuration()

	base := FunctionResource{
		FunctionName: "fn",
		Role:         "arn:aws:iam::123456789012:role/fn-role",
		Code:         FunctionCode{S3Bucket: aws.String("b"), S3Key: aws.String("k")},
		Runtime:      aws.String("python3.13"),
	}
	priorInputs := base
	priorInputs.Description = aws.String("a function description")
	priorInputs.Handler = aws.String("index.handler")
	priorInputs.KMSKeyArn = aws.String("arn:aws:kms:us-east-1:123456789012:key/k-1")

	current := base
	prior := runtime.Prior[FunctionResource, *FunctionResourceOutput]{
		Inputs: priorInputs,
		Outputs: &FunctionResourceOutput{
			Arn: "arn:aws:lambda:us-east-1:123456789012:function:fn",
		},
	}
	_, err := current.Update(context.Background(), cfg, prior)
	require.NoError(t, err)
	sent := fake.sent(configRoute)
	require.Len(t, sent, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal(sent[0], &body))
	for _, key := range []string{"Description", "Handler", "KMSKeyArn"} {
		assert.NotContains(t, body, key,
			"a removed %s must not be sent to UpdateFunctionConfiguration", key)
	}
}

func TestFunctionModifyResourcePlanUnknownVersionOutputs(t *testing.T) {
	priorInputs := FunctionResource{
		FunctionName: "fn",
		Role:         "arn:aws:iam::123456789012:role/fn-role",
		Code:         FunctionCode{S3Bucket: aws.String("b"), S3Key: aws.String("k")},
		Runtime:      aws.String("python3.13"),
		MemorySize:   aws.Int64(128),
		Publish:      aws.Bool(true),
	}
	current := priorInputs
	current.MemorySize = aws.Int64(256)

	var resp runtime.ResourcePlanResponse
	err := current.ModifyResourcePlan(runtime.ResourcePlanRequest[
		FunctionResource, *FunctionResourceOutput, *awsCfg,
	]{
		PriorInputs:   priorInputs,
		CurrentInputs: current,
		PriorOutputs: &FunctionResourceOutput{
			Arn:     "arn:aws:lambda:us-east-1:123456789012:function:fn",
			Version: "1",
		},
		HasPriorState: true,
	}, &resp)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{
		"qualified-arn":        true,
		"qualified-invoke-arn": true,
		"version":              true,
	}, resp.UnknownOutputs)
}

func TestFunctionModifyResourcePlanLeavesStableVersionOutputs(t *testing.T) {
	priorInputs := FunctionResource{
		FunctionName: "fn",
		Role:         "arn:aws:iam::123456789012:role/fn-role",
		Code:         FunctionCode{S3Bucket: aws.String("b"), S3Key: aws.String("k")},
		Runtime:      aws.String("python3.13"),
		MemorySize:   aws.Int64(128),
		Publish:      aws.Bool(true),
	}
	current := priorInputs

	var resp runtime.ResourcePlanResponse
	err := current.ModifyResourcePlan(runtime.ResourcePlanRequest[
		FunctionResource, *FunctionResourceOutput, *awsCfg,
	]{
		PriorInputs:   priorInputs,
		CurrentInputs: current,
		PriorOutputs: &FunctionResourceOutput{
			Arn:     "arn:aws:lambda:us-east-1:123456789012:function:fn",
			Version: "1",
		},
		HasPriorState: true,
	}, &resp)
	require.NoError(t, err)
	assert.Empty(t, resp.UnknownOutputs)
}
