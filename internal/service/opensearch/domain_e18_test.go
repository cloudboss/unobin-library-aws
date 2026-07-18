package opensearch

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainEquivalentInputUsesUnorderedCollections(t *testing.T) {
	firstSchedule := domainMaintenanceSchedule("2026-07-18T12:00:00Z")
	secondSchedule := domainMaintenanceSchedule("2026-07-19T12:00:00Z")
	firstLog := DomainLogPublishingOption{
		CloudWatchLogGroupARN: "arn:aws:logs:us-east-1:123456789012:log-group:first",
		LogType:               "INDEX_SLOW_LOGS",
	}
	secondLog := DomainLogPublishingOption{
		CloudWatchLogGroupARN: "arn:aws:logs:us-east-1:123456789012:log-group:second",
		LogType:               "SEARCH_SLOW_LOGS",
	}
	prior := DomainResource{
		VPCOptions: &DomainVPCOptions{
			EgressEnabled:    boolPointer(true),
			SecurityGroupIDs: &[]string{"sg-a", "sg-b"},
			SubnetIDs:        &[]string{"subnet-a", "subnet-b"},
		},
		AutoTuneOptions: &DomainAutoTuneOptions{
			DesiredState:        "ENABLED",
			MaintenanceSchedule: &[]DomainAutoTuneMaintenanceSchedule{firstSchedule, secondSchedule},
		},
		LogPublishingOptions: &[]DomainLogPublishingOption{firstLog, secondLog},
	}
	current := DomainResource{
		VPCOptions: &DomainVPCOptions{
			EgressEnabled:    boolPointer(true),
			SecurityGroupIDs: &[]string{"sg-b", "sg-a"},
			SubnetIDs:        &[]string{"subnet-b", "subnet-a"},
		},
		AutoTuneOptions: &DomainAutoTuneOptions{
			DesiredState:        "ENABLED",
			MaintenanceSchedule: &[]DomainAutoTuneMaintenanceSchedule{secondSchedule, firstSchedule},
		},
		LogPublishingOptions: &[]DomainLogPublishingOption{secondLog, firstLog},
	}
	resource := &DomainResource{}

	assert.True(t, resource.EquivalentInput("vpc-options", prior, current))
	assert.True(t, resource.EquivalentInput("auto-tune-options", prior, current))
	assert.True(t, resource.EquivalentInput("log-publishing-options", prior, current))

	current.VPCOptions.EgressEnabled = boolPointer(false)
	assert.False(t, resource.EquivalentInput("vpc-options", prior, current))
	current.VPCOptions.EgressEnabled = boolPointer(true)
	current.AutoTuneOptions.DesiredState = "DISABLED"
	assert.False(t, resource.EquivalentInput("auto-tune-options", prior, current))
	current.AutoTuneOptions.DesiredState = "ENABLED"
	emptyLogs := []DomainLogPublishingOption{}
	current.LogPublishingOptions = &emptyLogs
	assert.False(t, resource.EquivalentInput("log-publishing-options", prior, current))
	assert.False(t, resource.EquivalentInput("log-publishing-options", DomainResource{}, current))
	current.LogPublishingOptions = nil
	assert.True(t, resource.EquivalentInput("log-publishing-options", DomainResource{}, current))
}

func TestDomainUpdateConfigInputIgnoresUnorderedCollectionPermutations(t *testing.T) {
	firstSchedule := domainMaintenanceSchedule("2026-07-18T12:00:00Z")
	secondSchedule := domainMaintenanceSchedule("2026-07-19T12:00:00Z")
	firstLog := DomainLogPublishingOption{LogType: "INDEX_SLOW_LOGS"}
	secondLog := DomainLogPublishingOption{LogType: "SEARCH_SLOW_LOGS"}
	priorAdvanced := map[string]string{"indices.query.bool.max_clause_count": "1024"}
	currentAdvanced := map[string]string{"indices.query.bool.max_clause_count": "2048"}
	prior := DomainResource{
		AdvancedOptions: &priorAdvanced,
		AutoTuneOptions: &DomainAutoTuneOptions{
			DesiredState:        "ENABLED",
			MaintenanceSchedule: &[]DomainAutoTuneMaintenanceSchedule{firstSchedule, secondSchedule},
		},
		LogPublishingOptions: &[]DomainLogPublishingOption{firstLog, secondLog},
	}
	current := DomainResource{
		AdvancedOptions: &currentAdvanced,
		AutoTuneOptions: &DomainAutoTuneOptions{
			DesiredState:        "ENABLED",
			MaintenanceSchedule: &[]DomainAutoTuneMaintenanceSchedule{secondSchedule, firstSchedule},
		},
		LogPublishingOptions: &[]DomainLogPublishingOption{secondLog, firstLog},
	}

	input, needed, err := current.updateConfigInput(prior, "example")
	require.NoError(t, err)
	require.True(t, needed)
	assert.Equal(t, currentAdvanced, input.AdvancedOptions)
	assert.Nil(t, input.AutoTuneOptions)
	assert.Nil(t, input.LogPublishingOptions)

	current.AutoTuneOptions.MaintenanceSchedule =
		&[]DomainAutoTuneMaintenanceSchedule{firstSchedule}
	current.LogPublishingOptions = &[]DomainLogPublishingOption{firstLog}
	input, needed, err = current.updateConfigInput(prior, "example")
	require.NoError(t, err)
	require.True(t, needed)
	assert.NotNil(t, input.AutoTuneOptions)
	assert.NotNil(t, input.LogPublishingOptions)
}

func TestDomainValidationRejectsDuplicateUnorderedCollectionMembers(t *testing.T) {
	schedule := domainMaintenanceSchedule("2026-07-18T12:00:00Z")
	logOption := DomainLogPublishingOption{LogType: "INDEX_SLOW_LOGS"}
	tests := []struct {
		name      string
		configure func(*DomainResource)
		wantError string
	}{
		{
			name: "security group IDs",
			configure: func(resource *DomainResource) {
				resource.VPCOptions = &DomainVPCOptions{
					SecurityGroupIDs: &[]string{"sg-a", "sg-a"},
				}
			},
			wantError: "duplicate security-group-ids",
		},
		{
			name: "subnet IDs",
			configure: func(resource *DomainResource) {
				resource.VPCOptions = &DomainVPCOptions{
					SubnetIDs: &[]string{"subnet-a", "subnet-a"},
				}
			},
			wantError: "duplicate subnet-ids",
		},
		{
			name: "maintenance schedules",
			configure: func(resource *DomainResource) {
				resource.AutoTuneOptions = &DomainAutoTuneOptions{
					DesiredState: "ENABLED",
					MaintenanceSchedule: &[]DomainAutoTuneMaintenanceSchedule{
						schedule, schedule,
					},
				}
			},
			wantError: "duplicate maintenance-schedule",
		},
		{
			name: "log entries",
			configure: func(resource *DomainResource) {
				resource.LogPublishingOptions = &[]DomainLogPublishingOption{
					logOption, logOption,
				}
			},
			wantError: "duplicate log-publishing-options",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := DomainResource{DomainName: "example"}
			test.configure(&resource)

			err := resource.ValidateInputs(context.Background(), nil)

			assert.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestDomainCreateRejectsDuplicatesBeforeClientCalls(t *testing.T) {
	called := false
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			called = true
			return &awssdk.DescribeDomainOutput{}, nil
		},
	}
	resource := DomainResource{
		DomainName: "example",
		VPCOptions: &DomainVPCOptions{SubnetIDs: &[]string{"subnet-a", "subnet-a"}},
	}

	output, err := resource.create(
		context.Background(), client, shortDomainOptions(newRecordingDomainClock()),
	)

	require.ErrorContains(t, err, "duplicate subnet-ids")
	assert.Nil(t, output)
	assert.False(t, called)
}

func domainMaintenanceSchedule(start string) DomainAutoTuneMaintenanceSchedule {
	return DomainAutoTuneMaintenanceSchedule{
		CronExpressionForRecurrence: "cron(0 12 ? * SUN *)",
		Duration:                    DomainAutoTuneDuration{Unit: "HOURS", Value: 2},
		StartAt:                     start,
	}
}

func boolPointer(value bool) *bool { return &value }
