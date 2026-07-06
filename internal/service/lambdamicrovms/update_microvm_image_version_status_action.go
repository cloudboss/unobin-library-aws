package lambdamicrovms

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	lambdamicrovmstypes "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms/types"
	"github.com/cloudboss/unobin/pkg/constraint"
)

type UpdateMicrovmImageVersionStatusAction struct {
	ImageIdentifier string `ub:"image-identifier"`
	ImageVersion    string `ub:"image-version"`
	Status          string `ub:"status"`
}

func (r UpdateMicrovmImageVersionStatusAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.Status, "ACTIVE", "INACTIVE")).
			Message("status must be ACTIVE or INACTIVE"),
	}
}

func (r *UpdateMicrovmImageVersionStatusAction) Run(
	ctx context.Context,
	cfg *awsCfg,
) (*UpdateMicrovmImageVersionStatusActionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := client.UpdateMicrovmImageVersion(ctx,
		&awslambdamicrovms.UpdateMicrovmImageVersionInput{
			ImageIdentifier: aws.String(r.ImageIdentifier),
			ImageVersion:    aws.String(r.ImageVersion),
			Status:          lambdamicrovmstypes.MicrovmImageVersionStatus(r.Status),
		})
	if err != nil {
		return nil, fmt.Errorf("update Microvm image version %s: %w", r.ImageVersion, err)
	}
	version, err := microvmImageVersionOutputFromUpdate(out)
	if err != nil {
		return nil, err
	}
	return (*UpdateMicrovmImageVersionStatusActionOutput)(version), nil
}
