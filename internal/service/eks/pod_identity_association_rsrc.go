package eks

import (
	"context"
	"maps"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type PodIdentityAssociationResource struct {
	ClusterName        string             `ub:"cluster-name"`
	Namespace          string             `ub:"namespace"`
	ServiceAccount     string             `ub:"service-account"`
	RoleARN            string             `ub:"role-arn"`
	DisableSessionTags *bool              `ub:"disable-session-tags"`
	TargetRoleARN      *string            `ub:"target-role-arn"`
	Tags               *map[string]string `ub:"tags"`
}

type PodIdentityAssociationResourceOutput struct {
	AssociationID      string            `ub:"association-id"`
	AssociationARN     string            `ub:"association-arn"`
	ClusterName        string            `ub:"cluster-name"`
	Namespace          string            `ub:"namespace"`
	ServiceAccount     string            `ub:"service-account"`
	RoleARN            string            `ub:"role-arn"`
	DisableSessionTags bool              `ub:"disable-session-tags"`
	TargetRoleARN      *string           `ub:"target-role-arn"`
	ExternalID         *string           `ub:"external-id"`
	Tags               map[string]string `ub:"tags"`
}

type podIdentityPrior = runtime.Prior[
	PodIdentityAssociationResource, *PodIdentityAssociationResourceOutput, *awsCfg,
]
type podIdentityDefinition = runtime.ResourceDefinition[
	PodIdentityAssociationResource, *PodIdentityAssociationResourceOutput, *awsCfg,
]

const podIdentityARNPattern = `^arn:[^:]*:[^:]*:[^:]*:[^:]*:`

func (r PodIdentityAssociationResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.All(
			constraint.NotEquals(r.ClusterName, nil),
			constraint.NotEquals(r.ClusterName, ""),
		)).Message("cluster-name must be a nonempty string"),
		constraint.Must(constraint.All(
			constraint.NotEquals(r.Namespace, nil),
			constraint.NotEquals(r.Namespace, ""),
		)).Message("namespace must be a nonempty string"),
		constraint.Must(constraint.All(
			constraint.NotEquals(r.ServiceAccount, nil),
			constraint.NotEquals(r.ServiceAccount, ""),
		)).Message("service-account must be a nonempty string"),
		constraint.Must(constraint.NotEquals(r.RoleARN, nil)).
			Message("role-arn must be a non-null string"),
		constraint.Must(constraint.IsMatch(r.RoleARN, podIdentityARNPattern)).
			Message("role-arn must be an ARN"),
		constraint.Must(constraint.IsMatch(r.TargetRoleARN, podIdentityARNPattern)).
			Message("target-role-arn must be an ARN"),
	}
}

func (r PodIdentityAssociationResource) Defaults() []defaults.Default {
	return []defaults.Default{defaults.NullableValue(r.DisableSessionTags, false)}
}

func (r *PodIdentityAssociationResource) ResourceDefinition() podIdentityDefinition {
	return podIdentityDefinition{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[PodIdentityAssociationResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *PodIdentityAssociationResource) **bool {
					return &input.DisableSessionTags
				}),
				func(prior, desired *bool) bool { return aws.ToBool(prior) == aws.ToBool(desired) },
			),
			runtime.EqualBy(
				runtime.InputField(func(input *PodIdentityAssociationResource) **map[string]string {
					return &input.Tags
				}),
				func(prior, desired *map[string]string) bool {
					return maps.Equal(
						clusterTags(valueMapOrNil(prior)), clusterTags(valueMapOrNil(desired)),
					)
				},
			),
		},
		Replace: runtime.Replacement[
			PodIdentityAssociationResource, *PodIdentityAssociationResourceOutput, *awsCfg,
		]{
			Fields: []runtime.AnyInputField[PodIdentityAssociationResource]{
				runtime.InputField(func(input *PodIdentityAssociationResource) *string {
					return &input.ClusterName
				}),
				runtime.InputField(func(input *PodIdentityAssociationResource) *string {
					return &input.Namespace
				}),
				runtime.InputField(func(input *PodIdentityAssociationResource) *string {
					return &input.ServiceAccount
				}),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(
			_ PodIdentityAssociationResource, output *PodIdentityAssociationResourceOutput,
		) (string, error) {
			key, err := storedPodIdentityKey(output)
			if err != nil {
				return "", err
			}
			return key.String(), nil
		},
	}
}

func (r *PodIdentityAssociationResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*PodIdentityAssociationResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createPodIdentity(ctx, client, systemClusterClock{}, eksClientRequestToken)
}

func (r *PodIdentityAssociationResource) Read(
	ctx context.Context, cfg *awsCfg, prior podIdentityPrior,
) (*PodIdentityAssociationResourceOutput, error) {
	key, err := storedPodIdentityKey(prior.Outputs)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, associationPriorConfig(cfg, prior.Configuration))
	if err != nil {
		return nil, err
	}
	return readPodIdentity(ctx, client, key)
}

func (r *PodIdentityAssociationResource) Update(
	ctx context.Context, cfg *awsCfg, prior podIdentityPrior,
) (*PodIdentityAssociationResourceOutput, error) {
	client, err := newClient(ctx, associationPriorConfig(cfg, prior.Configuration))
	if err != nil {
		return nil, err
	}
	return r.updatePodIdentity(ctx, client, prior, systemClusterClock{}, eksClientRequestToken)
}

func (r *PodIdentityAssociationResource) Delete(
	ctx context.Context, cfg *awsCfg, prior podIdentityPrior,
) error {
	key, err := storedPodIdentityKey(prior.Outputs)
	if err != nil {
		return err
	}
	client, err := newClient(ctx, associationPriorConfig(cfg, prior.Configuration))
	if err != nil {
		return err
	}
	return deletePodIdentity(ctx, client, key)
}
