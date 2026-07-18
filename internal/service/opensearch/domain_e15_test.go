package opensearch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainSnapshotPauseValidation(t *testing.T) {
	validStart := "2026-07-18T12:00:00Z"
	validEnd := "2026-07-19T12:00:00Z"
	tests := []struct {
		name      string
		startTime *string
		endTime   *string
		wantError string
	}{
		{name: "missing end", startTime: &validStart, wantError: "requires end-time"},
		{
			name: "invalid start", startTime: stringPointer("invalid"), endTime: &validEnd,
			wantError: "start-time must be RFC3339",
		},
		{
			name: "invalid end", startTime: &validStart, endTime: stringPointer("invalid"),
			wantError: "end-time must be RFC3339",
		},
		{
			name: "nonpositive interval", startTime: &validStart, endTime: &validStart,
			wantError: "interval must be greater than zero",
		},
		{
			name:      "interval longer than 72 hours",
			startTime: &validStart,
			endTime:   stringPointer("2026-07-21T12:00:01Z"),
			wantError: "at most 72 hours",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := DomainResource{
				DomainName: "example",
				AutomatedSnapshotPauseOptions: &DomainAutomatedSnapshotPauseRequestOptions{
					Enabled: true, StartTime: test.startTime, EndTime: test.endTime,
				},
			}
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestDomainSnapshotPauseValidationAcceptsServiceValidatedTiming(t *testing.T) {
	resource := DomainResource{
		DomainName: "example",
		AutomatedSnapshotPauseOptions: &DomainAutomatedSnapshotPauseRequestOptions{
			Enabled: true,
			EndTime: stringPointer("2026-07-18T12:00:00Z"),
		},
	}
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))

	resource.AutomatedSnapshotPauseOptions.StartTime = stringPointer(
		"2026-07-18T12:00:00Z",
	)
	resource.AutomatedSnapshotPauseOptions.EndTime = stringPointer(
		"2026-07-21T12:00:00Z",
	)
	require.NoError(t, resource.ValidateInputs(context.Background(), nil))
}

func TestDomainSnapshotPauseRequests(t *testing.T) {
	start := "2026-07-18T12:00:00-04:00"
	end := "2026-07-19T12:00:00-04:00"
	options := &DomainAutomatedSnapshotPauseRequestOptions{
		Enabled: true, StartTime: &start, EndTime: &end,
	}
	wantStart, err := time.Parse(time.RFC3339, start)
	require.NoError(t, err)
	wantEnd, err := time.Parse(time.RFC3339, end)
	require.NoError(t, err)
	assertEnabled := func(
		t *testing.T,
		actual *awstypes.AutomatedSnapshotPauseRequestOptions,
	) {
		t.Helper()
		require.NotNil(t, actual)
		assert.True(t, aws.ToBool(actual.Enabled))
		assert.True(t, wantStart.Equal(aws.ToTime(actual.StartTime)))
		assert.True(t, wantEnd.Equal(aws.ToTime(actual.EndTime)))
	}

	t.Run("create", func(t *testing.T) {
		input, err := (DomainResource{
			DomainName: "example", AutomatedSnapshotPauseOptions: options,
		}).createInput()
		require.NoError(t, err)
		assertEnabled(t, input.AutomatedSnapshotPauseOptions)
	})

	t.Run("update", func(t *testing.T) {
		current := DomainResource{
			DomainName: "example", AutomatedSnapshotPauseOptions: options,
		}
		input, needed, err := current.updateConfigInput(
			DomainResource{DomainName: "example"}, "example",
		)
		require.NoError(t, err)
		require.True(t, needed)
		assertEnabled(t, input.AutomatedSnapshotPauseOptions)
	})

	t.Run("disable", func(t *testing.T) {
		prior := DomainResource{
			DomainName: "example", AutomatedSnapshotPauseOptions: options,
		}
		current := DomainResource{DomainName: "example"}
		input, needed, err := current.updateConfigInput(prior, "example")
		require.NoError(t, err)
		require.True(t, needed)
		require.NotNil(t, input.AutomatedSnapshotPauseOptions)
		assert.False(t, aws.ToBool(input.AutomatedSnapshotPauseOptions.Enabled))
		assert.Nil(t, input.AutomatedSnapshotPauseOptions.StartTime)
		assert.Nil(t, input.AutomatedSnapshotPauseOptions.EndTime)
	})
}

func TestReadDomainMapsSnapshotPauseOutput(t *testing.T) {
	start := time.Date(2026, time.July, 18, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
	end := start.Add(24 * time.Hour)
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			status := completeDomainStatus("example")
			status.AutomatedSnapshotPauseOptions = &awstypes.AutomatedSnapshotPauseOptions{
				Enabled:   aws.Bool(true),
				State:     awstypes.PauseStateActive,
				StartTime: &start,
				EndTime:   &end,
			}
			return &awssdk.DescribeDomainOutput{DomainStatus: status}, nil
		},
	}
	output, err := (DomainResource{DomainName: "example"}).read(
		context.Background(), client, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, output.AutomatedSnapshotPauseOptions)
	assert.True(t, output.AutomatedSnapshotPauseOptions.Enabled)
	assert.Equal(t, "Active", output.AutomatedSnapshotPauseOptions.State)
	assert.Equal(t, "2026-07-18T16:00:00Z",
		aws.ToString(output.AutomatedSnapshotPauseOptions.StartTime))
	assert.Equal(t, "2026-07-19T16:00:00Z",
		aws.ToString(output.AutomatedSnapshotPauseOptions.EndTime))
}

func TestUpdateDomainPropagatesSnapshotPauseServiceError(t *testing.T) {
	wantErr := errors.New("pause window rejected by service")
	calls := 0
	client := &fakeDomainClient{
		updateDomainConfig: func(
			_ context.Context,
			input *awssdk.UpdateDomainConfigInput,
		) (*awssdk.UpdateDomainConfigOutput, error) {
			calls++
			require.NotNil(t, input.AutomatedSnapshotPauseOptions)
			return nil, wantErr
		},
	}
	prior := runtime.Prior[DomainResource, *DomainResourceOutput]{
		Inputs:  DomainResource{DomainName: "example"},
		Outputs: &DomainResourceOutput{DomainName: "example"},
	}
	resource := DomainResource{
		DomainName: "example",
		AutomatedSnapshotPauseOptions: &DomainAutomatedSnapshotPauseRequestOptions{
			Enabled: true, EndTime: stringPointer("2026-07-19T12:00:00Z"),
		},
	}
	output, err := resource.update(
		context.Background(), client, prior,
		shortDomainOptions(newRecordingDomainClock()),
	)
	require.ErrorIs(t, err, wantErr)
	assert.Nil(t, output)
	assert.Equal(t, 1, calls)
}
