package opensearch

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
)

func domainAutoTuneOptions(
	options *DomainAutoTuneOptions,
) (*awstypes.AutoTuneOptions, error) {
	if options == nil {
		return nil, nil
	}
	schedules, err := domainAutoTuneSchedules(options.MaintenanceSchedule)
	if err != nil {
		return nil, err
	}
	result := &awstypes.AutoTuneOptions{
		DesiredState:         awstypes.AutoTuneDesiredState(options.DesiredState),
		MaintenanceSchedules: schedules,
		UseOffPeakWindow:     aws.Bool(options.UseOffPeakWindow),
	}
	if options.RollbackOnDisable != nil {
		result.RollbackOnDisable = awstypes.RollbackOnDisable(*options.RollbackOnDisable)
	}
	return result, nil
}

func domainAutoTuneCreateOptions(
	options *DomainAutoTuneOptions,
) (*awstypes.AutoTuneOptionsInput, error) {
	if options == nil {
		return nil, nil
	}
	schedules, err := domainAutoTuneSchedules(options.MaintenanceSchedule)
	if err != nil {
		return nil, err
	}
	return &awstypes.AutoTuneOptionsInput{
		DesiredState:         awstypes.AutoTuneDesiredState(options.DesiredState),
		MaintenanceSchedules: schedules,
		UseOffPeakWindow:     aws.Bool(options.UseOffPeakWindow),
	}, nil
}

func domainAutoTuneSchedules(
	schedules *[]DomainAutoTuneMaintenanceSchedule,
) ([]awstypes.AutoTuneMaintenanceSchedule, error) {
	if schedules == nil {
		return nil, nil
	}
	result := make([]awstypes.AutoTuneMaintenanceSchedule, 0, len(*schedules))
	for index, schedule := range *schedules {
		start, err := parseOptionalTime(&schedule.StartAt)
		if err != nil {
			return nil, fmt.Errorf("auto-tune maintenance-schedule %d start-at: %w",
				index, err)
		}
		result = append(result, awstypes.AutoTuneMaintenanceSchedule{
			CronExpressionForRecurrence: aws.String(
				schedule.CronExpressionForRecurrence,
			),
			Duration: &awstypes.Duration{
				Unit:  awstypes.TimeUnit(schedule.Duration.Unit),
				Value: aws.Int64(schedule.Duration.Value),
			},
			StartAt: start,
		})
	}
	return result, nil
}

func domainIdentityCenterOptions(
	options *DomainIdentityCenterOptions,
) *awstypes.IdentityCenterOptionsInput {
	if options == nil {
		return nil
	}
	result := &awstypes.IdentityCenterOptionsInput{
		EnabledAPIAccess: copyBool(options.EnabledAPIAccess),
	}
	if !aws.ToBool(options.EnabledAPIAccess) {
		return result
	}
	result.IdentityCenterInstanceARN = copyString(options.IdentityCenterInstanceARN)
	if options.RolesKey != nil {
		result.RolesKey = awstypes.RolesKeyIdCOption(*options.RolesKey)
	}
	if options.SubjectKey != nil {
		result.SubjectKey = awstypes.SubjectKeyIdCOption(*options.SubjectKey)
	}
	return result
}
