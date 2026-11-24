package kms

import (
	"context"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type GrantConstraints struct {
	EncryptionContextEquals *map[string]string `ub:"encryption-context-equals"`
	EncryptionContextSubset *map[string]string `ub:"encryption-context-subset"`
}

type GrantResource struct {
	KeyID             string            `ub:"key-id"`
	GranteePrincipal  string            `ub:"grantee-principal"`
	Operations        []string          `ub:"operations"`
	GrantConstraints  *GrantConstraints `ub:"constraints"`
	GrantTokens       *[]string         `ub:"grant-tokens"`
	Name              *string           `ub:"name"`
	RetiringPrincipal *string           `ub:"retiring-principal"`
	RetireOnDelete    bool              `ub:"retire-on-delete"`
}

type GrantResourceOutput struct {
	KeyID             string            `ub:"key-id"`
	GrantID           string            `ub:"grant-id"`
	GrantToken        *string           `ub:"grant-token,sensitive"`
	GranteePrincipal  string            `ub:"grantee-principal"`
	RetiringPrincipal *string           `ub:"retiring-principal"`
	Name              *string           `ub:"name"`
	Operations        []string          `ub:"operations"`
	GrantConstraints  *GrantConstraints `ub:"constraints"`
}

func (r GrantResource) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Value(r.RetireOnDelete, false)}
}

func (r GrantResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Expr("input.key-id != null", constraint.Bind("key-id", r.KeyID))).
			Message("key-id is required"),
		constraint.Must(constraint.Expr("input.grantee-principal != null",
			constraint.Bind("grantee-principal", r.GranteePrincipal))).
			Message("grantee-principal is required"),
		constraint.Must(constraint.Expr("input.operations != null",
			constraint.Bind("operations", r.Operations))).Message("operations is required"),
		constraint.Must(constraint.IsMatch(r.Name, `^[0-9A-Za-z_:/-]+$`)).
			Message("name must contain only ASCII letters, digits, _, :, /, or -"),
		constraint.Must(constraint.Expr("@core.byte-length(input.name ?? '') <= 256",
			constraint.Bind("name", r.Name))).Message("name must be at most 256 bytes"),
		constraint.ForEach(r.Operations, func(operation string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(operation,
					"Decrypt", "Encrypt", "GenerateDataKey", "GenerateDataKeyWithoutPlaintext",
					"ReEncryptFrom", "ReEncryptTo", "Sign", "Verify", "GetPublicKey", "CreateGrant",
					"RetireGrant", "DescribeKey", "GenerateDataKeyPair", "GenerateDataKeyPairWithoutPlaintext",
					"GenerateMac", "VerifyMac", "DeriveSharedSecret")).Message("invalid grant operation"),
			}
		}),
		constraint.Must(constraint.Not(constraint.All(
			constraint.NotEmpty(r.GrantConstraints.EncryptionContextEquals),
			constraint.NotEmpty(r.GrantConstraints.EncryptionContextSubset),
		))).Message("encryption context maps cannot both be nonempty"),
	}
}

func (r *GrantResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*GrantResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createGrant(ctx, client, realGrantClock())
}

func (r *GrantResource) Read(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[GrantResource, *GrantResourceOutput, *awsCfg],
) (*GrantResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return readGrant(ctx, client, prior.Outputs, false, realGrantClock())
}

func (r *GrantResource) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[GrantResource, *GrantResourceOutput, *awsCfg],
) (*GrantResourceOutput, error) {
	return r.Read(ctx, cfg, prior)
}

func (r *GrantResource) Delete(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[GrantResource, *GrantResourceOutput, *awsCfg],
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return deleteGrant(ctx, client, prior.Outputs, prior.Inputs.RetireOnDelete, realGrantClock())
}
