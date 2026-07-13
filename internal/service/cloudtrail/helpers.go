package cloudtrail

import (
	"context"
	"errors"
	"strings"

	cloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

func newClient(ctx context.Context, cfg *awsCfg) (*cloudtrail.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return cloudtrail.NewFromConfig(loaded), nil
}

func isTrailNotFound(err error) bool {
	var notFound *cloudtrailtypes.TrailNotFoundException
	return errors.As(err, &notFound)
}

func isCloudWatchAccessDenied(err error) bool {
	var role *cloudtrailtypes.InvalidCloudWatchLogsRoleArnException
	if errors.As(err, &role) {
		return strings.Contains(role.ErrorMessage(), "Access denied.")
	}
	var group *cloudtrailtypes.InvalidCloudWatchLogsLogGroupArnException
	return errors.As(err, &group) && strings.Contains(group.ErrorMessage(), "Access denied.")
}
