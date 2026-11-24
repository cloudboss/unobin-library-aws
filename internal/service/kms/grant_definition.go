package kms

import (
	"context"
	"reflect"
	"slices"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *GrantResource) ResourceDefinition() runtime.ResourceDefinition[
	GrantResource, *GrantResourceOutput, *awsCfg,
] {
	operations := runtime.InputField(func(r *GrantResource) *[]string { return &r.Operations })
	tokens := runtime.InputField(func(r *GrantResource) **[]string { return &r.GrantTokens })
	return runtime.ResourceDefinition[GrantResource, *GrantResourceOutput, *awsCfg]{
		SchemaVersion: 1,
		Validate: func(_ context.Context, r GrantResource, _ *awsCfg) error {
			_, err := r.prepareGrant()
			return err
		},
		Equality: []runtime.InputEqualityRule[GrantResource]{
			runtime.EqualBy(operations, func(a, b []string) bool {
				return slices.Equal(grantSet(a), grantSet(b))
			}),
			runtime.EqualBy(tokens, func(a, b *[]string) bool {
				var left, right []string
				if a != nil {
					left = *a
				}
				if b != nil {
					right = *b
				}
				return slices.Equal(grantSet(left), grantSet(right))
			}),
		},
		Replace: runtime.Replacement[GrantResource, *GrantResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[GrantResource]{
				runtime.InputField(func(r *GrantResource) *string { return &r.KeyID }),
				runtime.InputField(func(r *GrantResource) *string { return &r.GranteePrincipal }),
				operations, tokens,
				runtime.InputField(func(r *GrantResource) **GrantConstraints { return &r.GrantConstraints }),
				runtime.InputField(func(r *GrantResource) **string { return &r.Name }),
				runtime.InputField(func(r *GrantResource) **string { return &r.RetiringPrincipal }),
				runtime.InputField(func(r *GrantResource) *bool { return &r.RetireOnDelete }),
			},
			Drift: []runtime.DriftRule[*GrantResourceOutput]{
				runtime.ReplaceOnDrift(runtime.OutputField(func(o *GrantResourceOutput) *[]string {
					return &o.Operations
				}), func(a, b []string) bool { return !slices.Equal(grantSet(a), grantSet(b)) }),
				runtime.ReplaceOnDrift(runtime.OutputField(func(o *GrantResourceOutput) *string {
					return &o.GranteePrincipal
				}), func(a, b string) bool { return a != b }),
				runtime.ReplaceOnDrift(runtime.OutputField(func(o *GrantResourceOutput) **string {
					return &o.RetiringPrincipal
				}), func(a, b *string) bool { return !reflect.DeepEqual(a, b) }),
				runtime.ReplaceOnDrift(runtime.OutputField(func(o *GrantResourceOutput) **string {
					return &o.Name
				}), func(a, b *string) bool { return !reflect.DeepEqual(a, b) }),
				runtime.ReplaceOnDrift(runtime.OutputField(func(o *GrantResourceOutput) **GrantConstraints {
					return &o.GrantConstraints
				}), func(a, b *GrantConstraints) bool { return !reflect.DeepEqual(a, b) }),
			},
		},
		StableID: func(_ GrantResource, out *GrantResourceOutput) (string, error) {
			if out == nil {
				return "", nil
			}
			return out.KeyID + ":" + out.GrantID, nil
		},
	}
}
