package lambdamicrovms

import (
	"context"
	"errors"

	lambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	lambdamicrovmstypes "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
)

type awsCfg = awscfg.Configuration

func newClient(ctx context.Context, cfg *awsCfg) (*lambdamicrovms.Client, error) {
	awsCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return lambdamicrovms.NewFromConfig(awsCfg), nil
}

func isNotFound(err error) bool {
	var notFound *lambdamicrovmstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
