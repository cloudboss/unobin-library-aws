package eks

import (
	"context"

	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *AddonResource) SchemaVersion() int { return 1 }

func (r *AddonResource) ReplaceFields() []string {
	return []string{"cluster-name", "addon-name", "namespace-config"}
}

func (r AddonResource) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Value(r.Preserve, false)}
}

func (r *AddonResource) EquivalentInput(
	field string,
	prior AddonResource,
	current AddonResource,
) bool {
	if field != "pod-identity-association" {
		return false
	}
	return optionalUnorderedNodeGroupSliceEqual(
		prior.PodIdentityAssociation,
		current.PodIdentityAssociation,
	)
}

func (r *AddonResource) ValidateInputs(ctx context.Context, cfg *awsCfg) error {
	return r.validateAddonInputs(ctx, cfg)
}

func (r *AddonResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*AddonResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createAddon(ctx, client, systemClusterClock{}, eksClientRequestToken)
}

func (r *AddonResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[AddonResource, *AddonResourceOutput, *awsCfg],
) (*AddonResourceOutput, error) {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.readAddon(ctx, client, prior)
}

func (r *AddonResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[AddonResource, *AddonResourceOutput, *awsCfg],
) (*AddonResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.updateAddon(ctx, client, prior, systemClusterClock{}, eksClientRequestToken)
}

func (r *AddonResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[AddonResource, *AddonResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.deleteAddon(ctx, client, prior, systemClusterClock{})
}
