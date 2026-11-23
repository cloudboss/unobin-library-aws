package eks

import (
	"context"
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type AccessPolicyAssociationResource struct {
	ClusterName  string                       `ub:"cluster-name"`
	PrincipalARN string                       `ub:"principal-arn"`
	PolicyARN    string                       `ub:"policy-arn"`
	AccessScope  AccessPolicyAssociationScope `ub:"access-scope"`
}

type AccessPolicyAssociationScope struct {
	Type       string    `ub:"type"`
	Namespaces *[]string `ub:"namespaces"`
}

type AccessPolicyAssociationResourceOutput struct {
	ClusterName  string                              `ub:"cluster-name"`
	PrincipalARN string                              `ub:"principal-arn"`
	PolicyARN    string                              `ub:"policy-arn"`
	AccessScope  *AccessPolicyAssociationScopeOutput `ub:"access-scope"`
	AssociatedAt string                              `ub:"associated-at"`
	ModifiedAt   string                              `ub:"modified-at"`
}

type AccessPolicyAssociationScopeOutput struct {
	Type       string   `ub:"type"`
	Namespaces []string `ub:"namespaces"`
}

type accessPolicyAssociationPrior = runtime.Prior[
	AccessPolicyAssociationResource, *AccessPolicyAssociationResourceOutput, *awsCfg,
]
type accessPolicyAssociationDefinition = runtime.ResourceDefinition[
	AccessPolicyAssociationResource, *AccessPolicyAssociationResourceOutput, *awsCfg,
]

const accessPolicyAssociationARNPattern = `^arn:aws(?:-[a-z]+)*:[^:]*:` +
	`(?:[a-z]{2,4}(?:-[a-z]+)+-[0-9]{1,2})?:` +
	`(?:aws|aws-managed|third-party|aws-marketplace|partner-managed|[0-9]{12}|cw[^:\n]{10})?:` +
	`[\s\S]+$`

func (r AccessPolicyAssociationResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.IsMatch(r.ClusterName, "^[0-9A-Za-z][0-9A-Za-z_-]{0,99}$")),
		constraint.When(constraint.NotEquals(r.PrincipalARN, "")).Require(
			constraint.IsMatch(r.PrincipalARN, accessPolicyAssociationARNPattern),
		),
		constraint.When(constraint.NotEquals(r.PolicyARN, "")).Require(
			constraint.IsMatch(r.PolicyARN, accessPolicyAssociationARNPattern),
		),
		constraint.Must(constraint.NotEquals(r.ClusterName, nil)),
		constraint.Must(constraint.NotEquals(r.PrincipalARN, nil)),
		constraint.Must(constraint.NotEquals(r.PolicyARN, nil)),
		constraint.Must(constraint.NotEquals(r.AccessScope.Type, nil)),
	}
}

func (r *AccessPolicyAssociationResource) ResourceDefinition() accessPolicyAssociationDefinition {
	cluster := runtime.InputField(func(input *AccessPolicyAssociationResource) *string {
		return &input.ClusterName
	})
	principal := runtime.InputField(func(input *AccessPolicyAssociationResource) *string {
		return &input.PrincipalARN
	})
	policy := runtime.InputField(func(input *AccessPolicyAssociationResource) *string {
		return &input.PolicyARN
	})
	scope := runtime.InputField(
		func(input *AccessPolicyAssociationResource) *AccessPolicyAssociationScope {
			return &input.AccessScope
		},
	)
	observedScope := runtime.OutputField(
		func(output *AccessPolicyAssociationResourceOutput) **AccessPolicyAssociationScopeOutput {
			return &output.AccessScope
		},
	)
	return accessPolicyAssociationDefinition{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[AccessPolicyAssociationResource]{
			runtime.EqualBy(scope, func(prior, desired AccessPolicyAssociationScope) bool {
				return prior.Type == desired.Type &&
					slices.Equal(prior.requestNamespaces(), desired.requestNamespaces())
			}),
		},
		Replace: runtime.Replacement[
			AccessPolicyAssociationResource, *AccessPolicyAssociationResourceOutput, *awsCfg,
		]{
			Fields: []runtime.AnyInputField[AccessPolicyAssociationResource]{
				cluster, principal, policy, scope,
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
			Drift: []runtime.DriftRule[*AccessPolicyAssociationResourceOutput]{
				runtime.ReplaceOnDrift(observedScope, func(
					recorded, observed *AccessPolicyAssociationScopeOutput,
				) bool {
					if recorded == nil || observed == nil {
						return recorded != observed
					}
					return recorded.Type != observed.Type ||
						!slices.Equal(recorded.Namespaces, observed.Namespaces)
				}),
			},
		},
		StableID: func(
			_ AccessPolicyAssociationResource, output *AccessPolicyAssociationResourceOutput,
		) (string, error) {
			key, err := storedAccessPolicyAssociationKey(output)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%q/%q/%q", key.clusterName, key.principalARN, key.policyARN), nil
		},
	}
}

func (r *AccessPolicyAssociationResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*AccessPolicyAssociationResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createAccessPolicyAssociation(ctx, client, systemClusterClock{})
}

func (r *AccessPolicyAssociationResource) Read(
	ctx context.Context, cfg *awsCfg, prior accessPolicyAssociationPrior,
) (*AccessPolicyAssociationResourceOutput, error) {
	key, err := storedAccessPolicyAssociationKey(prior.Outputs)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, associationPriorConfig(cfg, prior.Configuration))
	if err != nil {
		return nil, err
	}
	return readAccessPolicyAssociation(ctx, client, key)
}

func (r *AccessPolicyAssociationResource) Update(
	_ context.Context, _ *awsCfg, prior accessPolicyAssociationPrior,
) (*AccessPolicyAssociationResourceOutput, error) {
	if prior.Observed != nil {
		return prior.Observed, nil
	}
	return prior.Outputs, nil
}

func (r *AccessPolicyAssociationResource) Delete(
	ctx context.Context, cfg *awsCfg, prior accessPolicyAssociationPrior,
) error {
	key, err := storedAccessPolicyAssociationKey(prior.Outputs)
	if err != nil {
		return err
	}
	client, err := newClient(ctx, associationPriorConfig(cfg, prior.Configuration))
	if err != nil {
		return err
	}
	return disassociateAccessPolicy(ctx, client, key)
}
