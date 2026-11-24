package ecr

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type ReplicationConfigurationResource struct {
	ReplicationConfiguration *ReplicationConfiguration `ub:"replication-configuration"`
}

type ReplicationConfiguration struct {
	Rules []ReplicationRule `ub:"rules"`
}

type ReplicationRule struct {
	Destinations      []ReplicationDestination       `ub:"destinations"`
	RepositoryFilters *[]ReplicationRepositoryFilter `ub:"repository-filters"`
}

type ReplicationDestination struct {
	Region     string `ub:"region"`
	RegistryID string `ub:"registry-id"`
}

type ReplicationRepositoryFilter struct {
	Filter     string `ub:"filter"`
	FilterType string `ub:"filter-type"`
}

type ReplicationConfigurationResourceOutput struct {
	RegistryID               string                    `ub:"registry-id"`
	Region                   string                    `ub:"region"`
	ReplicationConfiguration *ReplicationConfiguration `ub:"replication-configuration"`
}

type replicationPrior = runtime.Prior[
	ReplicationConfigurationResource, *ReplicationConfigurationResourceOutput, *awsCfg,
]
type replicationDefinition = runtime.ResourceDefinition[
	ReplicationConfigurationResource, *ReplicationConfigurationResourceOutput, *awsCfg,
]

type replicationClient interface {
	PutReplicationConfiguration(context.Context, *ecr.PutReplicationConfigurationInput,
		...func(*ecr.Options)) (*ecr.PutReplicationConfigurationOutput, error)
	DescribeRegistry(context.Context, *ecr.DescribeRegistryInput,
		...func(*ecr.Options)) (*ecr.DescribeRegistryOutput, error)
}

func (r ReplicationConfigurationResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.NotEquals(r.ReplicationConfiguration, nil)).
			Require(constraint.NotEquals(r.ReplicationConfiguration.Rules, nil)).
			Message("rules is required"),
		constraint.When(constraint.NotEquals(r.ReplicationConfiguration, nil)).
			Require(constraint.MinItems(r.ReplicationConfiguration.Rules, 1)).
			Message("rules must contain 1-10 entries"),
		constraint.Must(constraint.MaxItems(r.ReplicationConfiguration.Rules, 10)).
			Message("rules must contain 1-10 entries"),
		constraint.ForEach(r.ReplicationConfiguration.Rules,
			func(rule ReplicationRule) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.NotEquals(rule.Destinations, nil)).
						Message("destinations is required"),
					constraint.Must(constraint.MinItems(rule.Destinations, 1)).
						Message("destinations must contain 1-25 entries"),
					constraint.Must(constraint.MaxItems(rule.Destinations, 25)).
						Message("destinations must contain 1-25 entries"),
					constraint.Must(constraint.MaxItems(rule.RepositoryFilters, 100)).
						Message("repository-filters must contain at most 100 entries"),
					constraint.ForEach(rule.Destinations,
						func(destination ReplicationDestination) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.NotEquals(destination.Region, nil)).
									Message("destination region is required"),
								constraint.Must(constraint.IsMatch(destination.Region,
									"^[a-z]{2,4}(?:-[a-z]+)+-\\d{1,2}$")).
									Message("destination region must match AWS region syntax"),
								constraint.Must(constraint.NotEquals(destination.RegistryID, nil)).
									Message("destination registry-id is required"),
								constraint.Must(constraint.IsMatch(destination.RegistryID, "^\\d{12}$")).
									Message("destination registry-id must contain 12 digits"),
							}
						}),
					constraint.ForEach(rule.RepositoryFilters,
						func(filter ReplicationRepositoryFilter) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.NotEquals(filter.Filter, nil)).
									Message("filter is required"),
								constraint.Must(constraint.OneOf(filter.FilterType, "PREFIX_MATCH")).
									Message("filter-type must be PREFIX_MATCH"),
							}
						}),
				}
			}),
	}
}

func (*ReplicationConfigurationResource) ResourceDefinition() replicationDefinition {
	return replicationDefinition{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[ReplicationConfigurationResource]{
			runtime.EqualBy(runtime.InputField(func(r *ReplicationConfigurationResource,
			) **ReplicationConfiguration {
				return &r.ReplicationConfiguration
			}), replicationConfigurationsEqual),
		},
		Replace: runtime.Replacement[
			ReplicationConfigurationResource, *ReplicationConfigurationResourceOutput, *awsCfg,
		]{
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(_ ReplicationConfigurationResource,
			out *ReplicationConfigurationResourceOutput,
		) (string, error) {
			if err := checkReplicationIdentity(out); err != nil {
				return "", err
			}
			return out.RegistryID + "@" + out.Region, nil
		},
	}
}

func (r *ReplicationConfigurationResource) Create(ctx context.Context,
	cfg *awsCfg,
) (*ReplicationConfigurationResourceOutput, error) {
	client, identity, err := newReplicationClient(ctx, cfg, nil)
	if err != nil {
		return nil, err
	}
	return r.createReplicationConfiguration(ctx, client, identity)
}

func (r *ReplicationConfigurationResource) Read(ctx context.Context, cfg *awsCfg,
	prior replicationPrior,
) (*ReplicationConfigurationResourceOutput, error) {
	client, identity, err := priorReplicationClient(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	return readReplicationConfiguration(ctx, client, identity)
}

func (r *ReplicationConfigurationResource) Update(ctx context.Context, cfg *awsCfg,
	prior replicationPrior,
) (*ReplicationConfigurationResourceOutput, error) {
	client, identity, err := priorReplicationClient(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	return r.updateReplicationConfiguration(ctx, client, identity, prior)
}

func (*ReplicationConfigurationResource) Delete(ctx context.Context, cfg *awsCfg,
	prior replicationPrior,
) error {
	client, _, err := priorReplicationClient(ctx, cfg, prior)
	if err != nil {
		return err
	}
	_, err = client.PutReplicationConfiguration(ctx, emptyReplicationConfiguration())
	if err != nil {
		return fmt.Errorf("clear replication configuration: %w", err)
	}
	return nil
}

func (r ReplicationConfigurationResource) preparePut() *ecr.PutReplicationConfigurationInput {
	in := &ecr.PutReplicationConfigurationInput{}
	if r.ReplicationConfiguration == nil {
		return in
	}
	in.ReplicationConfiguration = &types.ReplicationConfiguration{
		Rules: make([]types.ReplicationRule, len(r.ReplicationConfiguration.Rules)),
	}
	for i, rule := range r.ReplicationConfiguration.Rules {
		converted := &in.ReplicationConfiguration.Rules[i]
		converted.Destinations = make([]types.ReplicationDestination, len(rule.Destinations))
		for j, destination := range rule.Destinations {
			converted.Destinations[j] = types.ReplicationDestination{
				Region: aws.String(destination.Region), RegistryId: aws.String(destination.RegistryID),
			}
		}
		if rule.RepositoryFilters != nil && len(*rule.RepositoryFilters) > 0 {
			converted.RepositoryFilters = make([]types.RepositoryFilter, len(*rule.RepositoryFilters))
			for j, filter := range *rule.RepositoryFilters {
				converted.RepositoryFilters[j] = types.RepositoryFilter{
					Filter: aws.String(filter.Filter), FilterType: types.RepositoryFilterType(filter.FilterType),
				}
			}
		}
	}
	return in
}

func replicationConfigurationsEqual(a, b *ReplicationConfiguration) bool {
	leftResource := ReplicationConfigurationResource{ReplicationConfiguration: a}
	left := leftResource.preparePut()
	rightResource := ReplicationConfigurationResource{ReplicationConfiguration: b}
	right := rightResource.preparePut()
	return reflect.DeepEqual(left, right)
}

func emptyReplicationConfiguration() *ecr.PutReplicationConfigurationInput {
	return &ecr.PutReplicationConfigurationInput{
		ReplicationConfiguration: &types.ReplicationConfiguration{Rules: []types.ReplicationRule{}},
	}
}

func (r ReplicationConfigurationResource) createReplicationConfiguration(ctx context.Context,
	client replicationClient, identity *ReplicationConfigurationResourceOutput,
) (*ReplicationConfigurationResourceOutput, error) {
	in := r.preparePut()
	snapshot, err := client.DescribeRegistry(ctx, &ecr.DescribeRegistryInput{})
	if err != nil {
		return nil, fmt.Errorf("snapshot replication configuration: %w", err)
	}
	restore := emptyReplicationConfiguration()
	if snapshot != nil && snapshot.ReplicationConfiguration != nil &&
		len(snapshot.ReplicationConfiguration.Rules) > 0 {
		restore.ReplicationConfiguration = snapshot.ReplicationConfiguration
	}
	if _, err := client.PutReplicationConfiguration(ctx, in); err != nil {
		return nil, fmt.Errorf("put replication configuration: %w", err)
	}
	out, err := readReplicationConfiguration(ctx, client, identity)
	if err == nil {
		return out, nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if _, cleanupErr := client.PutReplicationConfiguration(cleanupCtx, restore); cleanupErr != nil {
		err = errors.Join(err, fmt.Errorf("restore replication configuration: %w", cleanupErr))
	}
	return nil, err
}

func (r ReplicationConfigurationResource) updateReplicationConfiguration(ctx context.Context,
	client replicationClient, identity *ReplicationConfigurationResourceOutput, prior replicationPrior,
) (*ReplicationConfigurationResourceOutput, error) {
	current := prior.Observed
	if current == nil {
		current = prior.Outputs
	}
	if err := checkReplicationIdentity(current); err != nil {
		return nil, err
	}
	if current.RegistryID != identity.RegistryID || current.Region != identity.Region {
		return nil, errors.New("replication configuration observation identity differs from saved state")
	}
	if replicationConfigurationsEqual(r.ReplicationConfiguration, current.ReplicationConfiguration) {
		return current, nil
	}
	in := r.preparePut()
	if _, err := client.PutReplicationConfiguration(ctx, in); err != nil {
		return nil, fmt.Errorf("put replication configuration: %w", err)
	}
	return readReplicationConfiguration(ctx, client, identity)
}

func readReplicationConfiguration(ctx context.Context, client replicationClient,
	identity *ReplicationConfigurationResourceOutput,
) (*ReplicationConfigurationResourceOutput, error) {
	result, err := client.DescribeRegistry(ctx, &ecr.DescribeRegistryInput{})
	if err != nil {
		return nil, fmt.Errorf("describe replication configuration: %w", err)
	}
	if result == nil || result.ReplicationConfiguration == nil ||
		len(result.ReplicationConfiguration.Rules) == 0 {
		return nil, runtime.ErrNotFound
	}
	if aws.ToString(result.RegistryId) != identity.RegistryID {
		return nil, errors.New(
			"described registry account differs from replication configuration identity")
	}
	configuration := &ReplicationConfiguration{
		Rules: make([]ReplicationRule, len(result.ReplicationConfiguration.Rules)),
	}
	for i, rule := range result.ReplicationConfiguration.Rules {
		converted := &configuration.Rules[i]
		converted.Destinations = make([]ReplicationDestination, len(rule.Destinations))
		for j, destination := range rule.Destinations {
			converted.Destinations[j] = ReplicationDestination{
				Region: aws.ToString(destination.Region), RegistryID: aws.ToString(destination.RegistryId),
			}
		}
		if len(rule.RepositoryFilters) > 0 {
			filters := make([]ReplicationRepositoryFilter, len(rule.RepositoryFilters))
			for j, filter := range rule.RepositoryFilters {
				filters[j] = ReplicationRepositoryFilter{
					Filter: aws.ToString(filter.Filter), FilterType: string(filter.FilterType),
				}
			}
			converted.RepositoryFilters = &filters
		}
	}
	return &ReplicationConfigurationResourceOutput{
		RegistryID: identity.RegistryID, Region: identity.Region,
		ReplicationConfiguration: configuration,
	}, nil
}

func checkReplicationIdentity(identity *ReplicationConfigurationResourceOutput) error {
	if identity == nil || identity.RegistryID == "" || identity.Region == "" {
		return errors.New("replication configuration identity requires registry-id and region")
	}
	return nil
}

func priorReplicationClient(ctx context.Context, cfg *awsCfg, prior replicationPrior,
) (*ecr.Client, *ReplicationConfigurationResourceOutput, error) {
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	if err := checkReplicationIdentity(prior.Outputs); err != nil {
		return nil, nil, err
	}
	regional := awsCfg{}
	if cfg != nil {
		regional = *cfg
	}
	regional.Region = aws.String(prior.Outputs.Region)
	return newReplicationClient(ctx, &regional, prior.Outputs)
}

func newReplicationClient(ctx context.Context, cfg *awsCfg,
	saved *ReplicationConfigurationResourceOutput,
) (*ecr.Client, *ReplicationConfigurationResourceOutput, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	account, err := replicationAccountID(ctx, cfg, loaded)
	if err != nil {
		return nil, nil, err
	}
	identity := &ReplicationConfigurationResourceOutput{RegistryID: account, Region: loaded.Region}
	if err := checkReplicationIdentity(identity); err != nil {
		return nil, nil, err
	}
	if saved != nil && (saved.RegistryID != account || saved.Region != loaded.Region) {
		return nil, nil, errors.New(
			"replication configuration caller account differs from saved identity")
	}
	return ecr.NewFromConfig(loaded), identity, nil
}

func replicationAccountID(ctx context.Context, cfg *awsCfg, loaded aws.Config) (string, error) {
	if loaded.Credentials != nil {
		credentials, err := loaded.Credentials.Retrieve(ctx)
		if err == nil && credentials.AccountID != "" {
			return credentials.AccountID, nil
		}
	}
	client := sts.NewFromConfig(loaded, func(options *sts.Options) {
		if cfg != nil && cfg.STSEndpoint() != "" {
			options.BaseEndpoint = aws.String(cfg.STSEndpoint())
		}
	})
	result, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("get replication configuration caller identity: %w", err)
	}
	if result == nil || aws.ToString(result.Account) == "" {
		return "", errors.New("get replication configuration caller identity returned no account")
	}
	return *result.Account, nil
}
