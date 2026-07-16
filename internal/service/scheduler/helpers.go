package scheduler

import (
	"context"
	"errors"
	"strings"

	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

const iamPropagationMessage = "The execution role you provide must allow AWS EventBridge " +
	"Scheduler to assume the role."

type awsCfg = awscfg.Configuration

func newClient(ctx context.Context, cfg *awsCfg) (*schedulerapi.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return schedulerapi.NewFromConfig(loaded), nil
}

func isScheduleNotFound(err error) bool {
	var notFound *schedulertypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func isIAMPropagationError(err error) bool {
	var validation *schedulertypes.ValidationException
	return errors.As(err, &validation) &&
		strings.Contains(validation.ErrorMessage(), iamPropagationMessage)
}
