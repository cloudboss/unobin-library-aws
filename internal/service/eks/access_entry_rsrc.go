package eks

import (
	"context"
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type AccessEntryResource struct {
	ClusterName      string             `ub:"cluster-name"`
	PrincipalARN     string             `ub:"principal-arn"`
	Type             *string            `ub:"type"`
	KubernetesGroups *[]string          `ub:"kubernetes-groups"`
	UserName         *string            `ub:"user-name"`
	Tags             *map[string]string `ub:"tags"`
}

type AccessEntryResourceOutput struct {
	ClusterName      string            `ub:"cluster-name"`
	PrincipalARN     string            `ub:"principal-arn"`
	ARN              string            `ub:"arn"`
	Type             string            `ub:"type"`
	KubernetesGroups []string          `ub:"kubernetes-groups"`
	UserName         string            `ub:"user-name"`
	Tags             map[string]string `ub:"tags"`
	CreatedAt        string            `ub:"created-at"`
	ModifiedAt       string            `ub:"modified-at"`
}

type accessEntryPrior = runtime.Prior[AccessEntryResource, *AccessEntryResourceOutput, *awsCfg]
type accessEntryDefinition = runtime.ResourceDefinition[
	AccessEntryResource, *AccessEntryResourceOutput, *awsCfg,
]

func (r AccessEntryResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.IsMatch(r.ClusterName, "^[0-9A-Za-z][0-9A-Za-z_-]{0,99}$")),
		constraint.When(constraint.Present(r.Type)).Require(
			constraint.OneOf(r.Type,
				"EC2", "EC2_LINUX", "EC2_WINDOWS", "FARGATE_LINUX", "HYBRID_LINUX", "STANDARD",
			),
		),
		constraint.Must(constraint.NotEquals(r.ClusterName, nil)).
			Message("cluster-name must be a non-null string"),
		constraint.Must(
			constraint.NotEquals(r.PrincipalARN, nil),
			constraint.NotEquals(r.PrincipalARN, ""),
		).Message("principal-arn must be a nonempty string"),
	}
}

func (r AccessEntryResource) Defaults() []defaults.Default {
	return []defaults.Default{defaults.NullableValue(r.Type, "STANDARD")}
}

func (r *AccessEntryResource) ResourceDefinition() accessEntryDefinition {
	clusterName := runtime.InputField(func(input *AccessEntryResource) *string {
		return &input.ClusterName
	})
	principal := runtime.InputField(func(input *AccessEntryResource) *string {
		return &input.PrincipalARN
	})
	entryType := runtime.InputField(func(input *AccessEntryResource) **string {
		return &input.Type
	})
	groups := runtime.InputField(func(input *AccessEntryResource) **[]string {
		return &input.KubernetesGroups
	})
	tags := runtime.InputField(func(input *AccessEntryResource) **map[string]string {
		return &input.Tags
	})
	return accessEntryDefinition{
		SchemaVersion: 1,
		Validate: func(_ context.Context, input AccessEntryResource, _ *awsCfg) error {
			_, err := input.prepareAccessEntryCreate()
			return err
		},
		Equality: []runtime.InputEqualityRule[AccessEntryResource]{
			runtime.EqualBy(entryType, func(prior, desired *string) bool {
				return effectiveAccessEntryType(prior) == effectiveAccessEntryType(desired)
			}),
			runtime.EqualBy(groups, func(prior, desired *[]string) bool {
				if prior == nil || desired == nil {
					return prior == desired
				}
				return slices.Equal(accessEntryRequestGroups(*prior), accessEntryRequestGroups(*desired))
			}),
			runtime.EqualBy(tags, func(prior, desired *map[string]string) bool {
				return maps.Equal(clusterTags(valueMapOrNil(prior)), clusterTags(valueMapOrNil(desired)))
			}),
		},
		Replace: runtime.Replacement[AccessEntryResource, *AccessEntryResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[AccessEntryResource]{clusterName, principal, entryType},
		},
	}
}

func (r *AccessEntryResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*AccessEntryResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createAccessEntry(ctx, client, systemClusterClock{}, eksClientRequestToken)
}

func (r *AccessEntryResource) Read(
	ctx context.Context, cfg *awsCfg, prior accessEntryPrior,
) (*AccessEntryResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.readAccessEntry(ctx, client, prior.Outputs)
}

func (r *AccessEntryResource) Update(
	ctx context.Context, cfg *awsCfg, prior accessEntryPrior,
) (*AccessEntryResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.updateAccessEntry(ctx, client, prior)
}

func (r *AccessEntryResource) Delete(
	ctx context.Context, cfg *awsCfg, prior accessEntryPrior,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return deleteAccessEntry(ctx, client, prior.Outputs)
}
