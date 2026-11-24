package cloudwatchlogs

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/cloudwatchlogs"
)

func TestQueryDefinitionUpdateObservationFallback(t *testing.T) {
	api := newQueryAPI(t)
	api.entries["id-1"] = map[string]any{"queryDefinitionId": "id-1", "name": "query",
		"queryString": "desired", "logGroupNames": []any{"a", "b", "a"}}
	r := svc.QueryDefinitionResource{Name: "query", QueryString: "desired",
		LogGroupNames: &[]string{"a", "b", "a"}}
	correct := &svc.QueryDefinitionResourceOutput{QueryDefinitionID: "id-1", Name: "query",
		QueryString: "desired", LogGroupNames: []string{"a", "b", "a"}, Region: "us-east-1"}
	drift := *correct
	drift.QueryString, drift.LogGroupNames = "external", []string{"b", "a"}
	for _, tc := range []struct {
		name            string
		saved, observed *svc.QueryDefinitionResourceOutput
		put             bool
	}{
		{"observed correct", &drift, correct, false},
		{"observed drift", correct, &drift, true},
		{"saved correct", correct, nil, false},
		{"saved drift", &drift, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Update(t.Context(), &awscfg.Configuration{Region: aws.String("us-east-1"),
				EndpointURL: aws.String(api.endpoint), MaxAttempts: aws.Int64(1)},
				runtime.Prior[svc.QueryDefinitionResource,
					*svc.QueryDefinitionResourceOutput, *awscfg.Configuration]{
					Inputs: r, Outputs: tc.saved, Observed: tc.observed})
			require.NoError(t, err)
			want := []queryCall{{"DescribeQueryDefinitions", map[string]any{
				"queryDefinitionNamePrefix": "query"}}}
			if tc.put {
				want = append([]queryCall{{"PutQueryDefinition", map[string]any{
					"name": "query", "queryString": "desired", "queryDefinitionId": "id-1",
					"logGroupNames": []any{"a", "b", "a"}}}}, want...)
			}
			require.Equal(t, want, api.takeCalls())
		})
	}
}
