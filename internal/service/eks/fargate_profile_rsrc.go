package eks

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *FargateProfileResource) SchemaVersion() int { return 1 }

func (r *FargateProfileResource) ReplaceFields() []string {
	return []string{
		"cluster-name",
		"fargate-profile-name",
		"pod-execution-role-arn",
		"selector",
		"subnet-ids",
	}
}

func (r FargateProfileResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.Selector)).
			Message("selector must not be empty"),
		constraint.When(constraint.Present(r.SubnetIDs)).
			Require(constraint.NotEmpty(r.SubnetIDs)).
			Message("subnet-ids must not be empty when present"),
	}
}

func (r *FargateProfileResource) ValidateInputs(ctx context.Context, cfg *awsCfg) error {
	return r.validateFargateProfileInputs(ctx, cfg)
}

func (r *FargateProfileResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*FargateProfileResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createFargateProfile(ctx, client, systemClusterClock{})
}

func (r *FargateProfileResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *FargateProfileResourceOutput,
) (*FargateProfileResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.readFargateProfile(ctx, client, prior)
}

func (r *FargateProfileResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[FargateProfileResource, *FargateProfileResourceOutput],
) (*FargateProfileResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.updateFargateProfile(ctx, client, prior)
}

func (r *FargateProfileResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *FargateProfileResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.deleteFargateProfile(ctx, client, prior, systemClusterClock{})
}

func (r *FargateProfileResource) validateFargateProfileInputs(context.Context, *awsCfg) error {
	if len(r.ClusterName) < 1 || len(r.ClusterName) > 100 ||
		!clusterNamePattern.MatchString(r.ClusterName) {
		return fmt.Errorf("cluster-name must match %s and contain 1 to 100 characters",
			clusterNamePattern.String())
	}
	if r.FargateProfileName == "" {
		return errors.New("fargate-profile-name must not be empty")
	}
	if r.PodExecutionRoleARN == "" {
		return errors.New("pod-execution-role-arn must not be empty")
	}
	if len(r.Selector) == 0 {
		return errors.New("selector must not be empty")
	}
	for _, selector := range r.Selector {
		if selector.Namespace == "" {
			return errors.New("selector.namespace must not be empty")
		}
	}
	if r.SubnetIDs != nil && len(*r.SubnetIDs) == 0 {
		return errors.New("subnet-ids must not be empty when present")
	}
	return nil
}
