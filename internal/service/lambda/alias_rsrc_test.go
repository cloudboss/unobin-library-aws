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

const aliasArn = "arn:aws:lambda:us-east-1:123456789012:function:fn:live"

func TestAliasCreateSendsEmptyDescriptionAndRoutingConfig(t *testing.T) {
	fake := newFakeLambda(t)
	const createRoute = "POST /2015-03-31/functions/fn/aliases"
	fake.on(createRoute, func(int) (int, string) { return 201, `{}` })
	fake.on("GET /2015-03-31/functions/fn/aliases/live", func(int) (int, string) {
		return 200, `{"AliasArn":"` + aliasArn + `","Name":"live","FunctionVersion":"1"}`
	})

	out, err := (&AliasResource{
		Name:            "live",
		FunctionName:    "fn",
		FunctionVersion: "1",
	}).Create(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, aliasArn, out.Arn)
	assert.Equal(t, "fn", out.FunctionName)
	assert.Equal(t, "live", out.Name)
	assert.Equal(t, functionInvokeARN("aws", "us-east-1", aliasArn), out.InvokeArn)

	sent := fake.sent(createRoute)
	require.Len(t, sent, 1)
	body := requestBody(t, sent[0])
	assert.Equal(t, "", body["Description"])
	assert.Contains(t, body, "RoutingConfig")
	routing, ok := body["RoutingConfig"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, routing)
}

func TestAliasUpdateUsesPriorIdentityAndClearsRemovedMutableState(t *testing.T) {
	fake := newFakeLambda(t)
	const updateRoute = "PUT /2015-03-31/functions/old-fn/aliases/live"
	fake.on(updateRoute, func(int) (int, string) { return 200, `{}` })
	fake.on("GET /2015-03-31/functions/old-fn/aliases/live", func(int) (int, string) {
		return 200, `{"AliasArn":"` + aliasArn + `","Name":"live","FunctionVersion":"2"}`
	})
	weights := map[string]float64{"3": 0.2}
	prior := runtime.Prior[AliasResource, *AliasResourceOutput]{
		Inputs: AliasResource{
			Name:            "live",
			FunctionName:    "old-fn",
			FunctionVersion: "1",
			Description:     aws.String("old description"),
			RoutingConfig: &AliasRoutingConfig{
				AdditionalVersionWeights: &weights,
			},
		},
		Outputs: &AliasResourceOutput{
			Arn:          aliasArn,
			FunctionName: "old-fn",
			Name:         "live",
		},
	}

	out, err := (&AliasResource{
		Name:            "new-live",
		FunctionName:    "new-fn",
		FunctionVersion: "2",
	}).Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, "old-fn", out.FunctionName)
	assert.Equal(t, "live", out.Name)

	sent := fake.sent(updateRoute)
	require.Len(t, sent, 1)
	body := requestBody(t, sent[0])
	assert.Equal(t, "2", body["FunctionVersion"])
	assert.Equal(t, "", body["Description"])
	assert.Contains(t, body, "RoutingConfig")
	routing, ok := body["RoutingConfig"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, routing)
}

func TestAliasEquivalentInput(t *testing.T) {
	fullArn := "arn:aws:lambda:us-east-1:123456789012:function:fn"
	qualifiedArn := fullArn + ":live"
	partialArn := "123456789012:function:fn"
	otherFullArn := "arn:aws:lambda:us-west-2:123456789012:function:fn"

	tests := []struct {
		name    string
		field   string
		prior   string
		current string
		want    bool
	}{
		{
			name:    "full arn to bare name",
			field:   "function-name",
			prior:   fullArn,
			current: "fn",
			want:    true,
		},
		{
			name:    "bare name to full arn",
			field:   "function-name",
			prior:   "fn",
			current: fullArn,
			want:    true,
		},
		{
			name:    "qualified arn to bare name",
			field:   "function-name",
			prior:   qualifiedArn,
			current: "fn",
			want:    true,
		},
		{
			name:    "partial arn to bare name",
			field:   "function-name",
			prior:   partialArn,
			current: "fn",
			want:    true,
		},
		{
			name:    "two arns are not equivalent",
			field:   "function-name",
			prior:   fullArn,
			current: otherFullArn,
		},
		{
			name:    "different names",
			field:   "function-name",
			prior:   fullArn,
			current: "other-fn",
		},
		{
			name:    "different field",
			field:   "name",
			prior:   fullArn,
			current: "fn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&AliasResource{}).EquivalentInput(tt.field,
				AliasResource{FunctionName: tt.prior},
				AliasResource{FunctionName: tt.current})
			assert.Equal(t, tt.want, got)
		})
	}
}

func requestBody(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(data, &body))
	return body
}
