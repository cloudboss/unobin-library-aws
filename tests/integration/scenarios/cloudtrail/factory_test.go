package cloudtrail

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trailBucketPolicy struct {
	Version   string                 `json:"Version"`
	Statement []trailPolicyStatement `json:"Statement"`
}

type trailPolicyStatement struct {
	Sid       string                       `json:"Sid"`
	Effect    string                       `json:"Effect"`
	Principal map[string]string            `json:"Principal"`
	Action    string                       `json:"Action"`
	Resource  string                       `json:"Resource"`
	Condition map[string]map[string]string `json:"Condition,omitempty"`
}

func TestTrailWaitsForCompatibleBucketPolicy(t *testing.T) {
	source, err := os.ReadFile("factory.ub")
	require.NoError(t, err)
	parsed, err := syntax.ParseSource("factory.ub", source)
	require.NoError(t, err)
	require.NotNil(t, parsed.Factory)

	body := parsed.Factory.Body
	dag := runtime.BuildSyntaxDAG(body, nil)
	assert.Contains(t, dag.Edges["resource.trail"], "resource.logs-policy")

	ctx := &runtime.EvalContext{
		Inputs: map[string]any{"s3-key-prefix": "initial"},
		Resources: map[string]any{
			"logs": map[string]any{"arn": "arn:aws:s3:::logs"},
		},
		Data: map[string]any{
			"caller": map[string]any{"account": "123456789012"},
		},
	}
	policyValue, err := runtime.Eval(
		factoryResourceField(t, body, "logs-policy", "policy"), ctx,
	)
	require.NoError(t, err)
	policyJSON, ok := policyValue.(string)
	require.True(t, ok)

	var policy trailBucketPolicy
	require.NoError(t, json.Unmarshal([]byte(policyJSON), &policy))
	assert.Equal(t, trailBucketPolicy{
		Version: "2012-10-17",
		Statement: []trailPolicyStatement{
			{
				Sid:       "CloudTrailAcl",
				Effect:    "Allow",
				Principal: map[string]string{"Service": "cloudtrail.amazonaws.com"},
				Action:    "s3:GetBucketAcl",
				Resource:  "arn:aws:s3:::logs",
			},
			{
				Sid:       "CloudTrailWrite",
				Effect:    "Allow",
				Principal: map[string]string{"Service": "cloudtrail.amazonaws.com"},
				Action:    "s3:PutObject",
				Resource: "arn:aws:s3:::logs/initial/AWSLogs/" +
					"123456789012/*",
				Condition: map[string]map[string]string{
					"StringEquals": {"s3:x-amz-acl": "bucket-owner-full-control"},
				},
			},
		},
	}, policy)
}

func factoryResourceField(
	t *testing.T,
	body syntax.FactoryBody,
	resourceName string,
	fieldName string,
) lang.Expr {
	t.Helper()
	for _, resource := range body.Resources {
		if resource.Name.Name != resourceName {
			continue
		}
		for _, field := range resource.Body.Fields {
			if field.Key.Name == fieldName {
				return field.Value
			}
		}
		require.FailNow(t, "resource field not found", "%s.%s", resourceName, fieldName)
	}
	require.FailNow(t, "resource not found", resourceName)
	return nil
}
