package opensearch

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainCreateInput(t *testing.T) {
	policy := `{"Statement":[]}`
	engineVersion := "OpenSearch_2.17"
	tags := map[string]string{"z": "last", "a": "first"}
	logs := []DomainLogPublishingOption{
		{
			CloudWatchLogGroupARN: "arn:aws:logs:us-east-1:123456789012:log-group:test",
			LogType:               "INDEX_SLOW_LOGS",
		},
	}
	resource := DomainResource{
		DomainName:           "example-domain",
		AccessPolicies:       &policy,
		EngineVersion:        &engineVersion,
		LogPublishingOptions: &logs,
		Tags:                 &tags,
	}

	input, err := resource.createInput()
	require.NoError(t, err)
	assert.Equal(t, "example-domain", aws.ToString(input.DomainName))
	assert.Equal(t, policy, aws.ToString(input.AccessPolicies))
	assert.Equal(t, engineVersion, aws.ToString(input.EngineVersion))
	assert.Equal(t, []awstypes.Tag{
		{Key: aws.String("a"), Value: aws.String("first")},
		{Key: aws.String("z"), Value: aws.String("last")},
	}, input.TagList)
	require.Contains(t, input.LogPublishingOptions, "INDEX_SLOW_LOGS")
	assert.True(t, aws.ToBool(
		input.LogPublishingOptions["INDEX_SLOW_LOGS"].Enabled,
	))
}

func TestDomainAutoTuneCreateAndFollowUpProjections(t *testing.T) {
	schedules := []DomainAutoTuneMaintenanceSchedule{
		domainMaintenanceSchedule("2026-07-18T12:00:00Z"),
	}
	resource := DomainResource{
		DomainName: "example",
		AutoTuneOptions: &DomainAutoTuneOptions{
			DesiredState:        "DISABLED",
			MaintenanceSchedule: &schedules,
			RollbackOnDisable:   stringPointer("DEFAULT_ROLLBACK"),
			UseOffPeakWindow:    false,
		},
	}

	createInput, err := resource.createInput()
	require.NoError(t, err)
	require.NotNil(t, createInput.AutoTuneOptions)
	assert.Equal(t, awstypes.AutoTuneDesiredState("DISABLED"),
		createInput.AutoTuneOptions.DesiredState)
	require.Len(t, createInput.AutoTuneOptions.MaintenanceSchedules, 1)
	assert.Equal(t, schedules[0].StartAt,
		createInput.AutoTuneOptions.MaintenanceSchedules[0].StartAt.Format(time.RFC3339))
	assert.False(t, aws.ToBool(createInput.AutoTuneOptions.UseOffPeakWindow))

	followUps, err := resource.createFollowUpInputs()
	require.NoError(t, err)
	require.Len(t, followUps, 1)
	require.NotNil(t, followUps[0].AutoTuneOptions)
	assert.Equal(t, awstypes.RollbackOnDisable("DEFAULT_ROLLBACK"),
		followUps[0].AutoTuneOptions.RollbackOnDisable)
	require.Len(t, followUps[0].AutoTuneOptions.MaintenanceSchedules, 1)
}

func TestDomainAutoTuneOmissionSendsNeitherProjection(t *testing.T) {
	resource := DomainResource{DomainName: "example"}

	createInput, err := resource.createInput()
	require.NoError(t, err)
	assert.Nil(t, createInput.AutoTuneOptions)
	followUps, err := resource.createFollowUpInputs()
	require.NoError(t, err)
	assert.Empty(t, followUps)
}

func TestDomainOutputRejectsMissingStatus(t *testing.T) {
	output, err := domainOutput(nil)
	require.Error(t, err)
	assert.Nil(t, output)
}

func TestDomainOutputDerivesPublicDashboardEndpoints(t *testing.T) {
	status := &awstypes.DomainStatus{
		ARN:           aws.String("arn:aws:es:us-east-1:123456789012:domain/example"),
		ClusterConfig: &awstypes.ClusterConfig{},
		DomainId:      aws.String("123456789012/example"),
		DomainName:    aws.String("example"),
		Endpoint:      aws.String("search.example.us-east-1.es.amazonaws.com"),
		EndpointV2:    aws.String("search.example.aos.us-east-1.on.aws"),
		EngineVersion: aws.String("OpenSearch_2.17"),
		IPAddressType: awstypes.IPAddressTypeDualstack,
	}

	output, err := domainOutput(status)
	require.NoError(t, err)
	assert.Equal(t, "example", output.DomainName)
	assert.Equal(t, "123456789012/example", output.DomainID)
	assert.Equal(t, "search.example.us-east-1.es.amazonaws.com/_dashboards",
		aws.ToString(output.DashboardEndpoint))
	assert.Equal(t, "search.example.aos.us-east-1.on.aws/_dashboards",
		aws.ToString(output.DashboardEndpointV2))
}

func TestDomainEndpointsRequireStatusConsistency(t *testing.T) {
	tests := []struct {
		name           string
		status         *awstypes.DomainStatus
		wantEndpoint   string
		wantEndpointV2 string
		wantEndpoints  map[string]string
		wantError      string
	}{
		{
			name: "public scalar",
			status: &awstypes.DomainStatus{
				Endpoint: aws.String("public.example"),
			},
			wantEndpoint: "public.example",
		},
		{
			name: "public dualstack scalars",
			status: &awstypes.DomainStatus{
				Endpoint: aws.String("public.example"), EndpointV2: aws.String("v2.example"),
			},
			wantEndpoint: "public.example", wantEndpointV2: "v2.example",
		},
		{
			name: "public rejects empty non-nil map",
			status: &awstypes.DomainStatus{
				Endpoint: aws.String("public.example"), Endpoints: map[string]string{},
			},
			wantError: "public domain returned VPC endpoints",
		},
		{
			name: "public rejects mapped endpoint",
			status: &awstypes.DomainStatus{
				Endpoint:  aws.String("public.example"),
				Endpoints: map[string]string{"vpc": "vpc.example"},
			},
			wantError: "public domain returned VPC endpoints",
		},
		{
			name:      "VPC requires map",
			status:    &awstypes.DomainStatus{VPCOptions: &awstypes.VPCDerivedInfo{}},
			wantError: "VPC domain returned no endpoint map",
		},
		{
			name: "VPC rejects empty map",
			status: &awstypes.DomainStatus{
				VPCOptions: &awstypes.VPCDerivedInfo{}, Endpoints: map[string]string{},
			},
			wantError: "VPC domain returned empty endpoint map",
		},
		{
			name: "VPC rejects scalar",
			status: &awstypes.DomainStatus{
				VPCOptions: &awstypes.VPCDerivedInfo{}, Endpoint: aws.String("public.example"),
				Endpoints: map[string]string{"vpc": "vpc.example"},
			},
			wantError: "VPC domain returned scalar endpoint",
		},
		{
			name: "VPC endpoint",
			status: &awstypes.DomainStatus{
				VPCOptions: &awstypes.VPCDerivedInfo{},
				Endpoints:  map[string]string{"vpc": "vpc.example"},
			},
			wantEndpoint:  "vpc.example",
			wantEndpoints: map[string]string{"vpc": "vpc.example"},
		},
		{
			name: "VPC endpoint v2",
			status: &awstypes.DomainStatus{
				VPCOptions: &awstypes.VPCDerivedInfo{},
				Endpoints:  map[string]string{"vpcv2": "vpcv2.example"},
			},
			wantEndpointV2: "vpcv2.example",
			wantEndpoints:  map[string]string{"vpcv2": "vpcv2.example"},
		},
		{
			name: "VPC rejects unknown map",
			status: &awstypes.DomainStatus{
				VPCOptions: &awstypes.VPCDerivedInfo{},
				Endpoints:  map[string]string{"unknown": "example"},
			},
			wantError: "VPC endpoint map has no endpoint",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoint, endpointV2, endpoints, err := domainEndpoints(test.status)
			if test.wantError != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantEndpoint, aws.ToString(endpoint))
			assert.Equal(t, test.wantEndpointV2, aws.ToString(endpointV2))
			if test.wantEndpoints == nil {
				assert.Nil(t, endpoints)
			} else {
				require.NotNil(t, endpoints)
				assert.Equal(t, test.wantEndpoints, *endpoints)
			}
		})
	}
}
