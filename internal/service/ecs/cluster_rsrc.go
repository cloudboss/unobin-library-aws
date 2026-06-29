package ecs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// clusterNotFoundCode is the error code ECS uses for a cluster that does not
// exist, on DescribeClusters and on the cluster mutation calls alike.
const clusterNotFoundCode = "ClusterNotFoundException"

// The cluster status strings ECS reports. A cluster moves through
// PROVISIONING and DEPROVISIONING while the resources behind its capacity
// providers are created or deleted, and a deleted cluster remains
// describable as INACTIVE for a while afterward.
const (
	clusterStatusActive         = "ACTIVE"
	clusterStatusProvisioning   = "PROVISIONING"
	clusterStatusDeprovisioning = "DEPROVISIONING"
	clusterStatusInactive       = "INACTIVE"
)

// clusterTimeout bounds the cluster waits and the retried mutation calls.
// Attaching or detaching capacity providers can keep a cluster in
// PROVISIONING, and a delete blocked by draining container instances or
// stopping tasks can stay busy, for several minutes.
const clusterTimeout = 10 * time.Minute

// clusterNameRegexp matches a valid cluster name: 1 to 255 letters, numbers,
// underscores, and hyphens. The character class is ASCII, so the byte length
// the regexp enforces equals the character length ECS limits.
var clusterNameRegexp = regexp.MustCompile(`^[0-9A-Za-z_-]{1,255}$`)

// Cluster manages an ECS cluster. The name is the only input fixed at create
// time, so changing it replaces the cluster; every other input is reconciled
// in place. The configuration, service connect defaults, and settings are
// reconciled by UpdateCluster when they change, and tags by the tag calls.
// The capacity-providers and default-capacity-provider-strategy fields have
// no member in the update call: they are reconciled by the whole-state
// PutClusterCapacityProviders call after create and on any later change,
// each call replacing both sets together, so removing either field from the
// configuration clears its set on the cluster.
//
// Name is required, since an omitted name would silently address the cluster
// literally named "default". It must match ^[0-9A-Za-z_-]{1,255}$, a
// regular-expression and byte-length check enforced in Create rather than a
// declarative constraint.
type Cluster struct {
	Name                            string                                 `ub:"name"`
	Configuration                   *ClusterConfiguration                  `ub:"configuration"`
	ServiceConnectDefaults          *ClusterServiceConnectDefaults         `ub:"service-connect-defaults"`
	Settings                        *[]ClusterSetting                      `ub:"settings"`
	CapacityProviders               *[]string                              `ub:"capacity-providers"`
	DefaultCapacityProviderStrategy *[]ClusterCapacityProviderStrategyItem `ub:"default-capacity-provider-strategy"`
	Tags                            *map[string]string                     `ub:"tags"`
}

// ClusterOutput holds the one value ECS computes for a cluster: the ARN that
// identifies it. The ARN is the identity handle, so Read and Delete key off
// it from the prior outputs; on a replace the receiver already holds the new
// name while the old cluster still needs to be found and removed.
type ClusterOutput struct {
	Arn string `ub:"arn"`
}

func (r *Cluster) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs ECS fixes when a cluster is created. Only
// the name is immutable; the configuration, service connect defaults,
// settings, capacity provider fields, and tags all change in place.
func (r *Cluster) ReplaceFields() []string {
	return []string{"name"}
}

// Constraints declares the rules ECS places on a cluster's inputs. The
// execute command logging mode accepts a fixed set of values, and the
// OVERRIDE mode needs the log configuration it redirects to. The only
// setting name ECS defines is containerInsights. A strategy item's base and
// weight each have a fixed range. The cluster name rule is a
// regular-expression check in Create rather than a constraint, since it
// needs a character-class match.
func (r Cluster) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.Configuration.ExecuteCommandConfiguration.Logging)).
			Require(constraint.OneOf(r.Configuration.ExecuteCommandConfiguration.Logging,
				"NONE", "DEFAULT", "OVERRIDE")).
			Message("logging must be one of NONE, DEFAULT, or OVERRIDE"),
		constraint.When(constraint.Equals(
			r.Configuration.ExecuteCommandConfiguration.Logging, "OVERRIDE")).
			Require(constraint.Present(
				r.Configuration.ExecuteCommandConfiguration.LogConfiguration)).
			Message("log-configuration is required when logging is OVERRIDE"),
		constraint.ForEach(r.Settings, func(s ClusterSetting) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(s.Name, "containerInsights")).
					Message("name must be containerInsights"),
			}
		}),
		constraint.ForEach(r.DefaultCapacityProviderStrategy,
			func(item ClusterCapacityProviderStrategyItem) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(item.Base)).
						Require(constraint.AtLeast(item.Base, 0),
							constraint.AtMost(item.Base, 100000)).
						Message("base must be between 0 and 100000"),
					constraint.When(constraint.Present(item.Weight)).
						Require(constraint.AtLeast(item.Weight, 0),
							constraint.AtMost(item.Weight, 1000)).
						Message("weight must be between 0 and 1000"),
				}
			}),
	}
}

func (r *Cluster) Create(ctx context.Context, cfg *awsCfg) (*ClusterOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if !clusterNameRegexp.MatchString(r.Name) {
		return nil, fmt.Errorf("name %q must match %s", r.Name, clusterNameRegexp.String())
	}
	in := &ecs.CreateClusterInput{
		ClusterName:            aws.String(r.Name),
		Configuration:          r.Configuration.sdk(),
		ServiceConnectDefaults: r.ServiceConnectDefaults.sdk(),
		Settings:               clusterSettingsSDK(ptr.Value(r.Settings)),
		Tags:                   clusterTags(ptr.Value(r.Tags)),
	}
	// The first ECS provision in an account creates the AWSServiceRoleForECS
	// service-linked role asynchronously, and CreateCluster fails until IAM
	// propagates it, so the create retries through that window.
	createCluster := func() (*ecs.CreateClusterOutput, error) {
		var resp *ecs.CreateClusterOutput
		err := retry.OnError(ctx, clusterCreateRetryable, func(ctx context.Context) error {
			var err error
			resp, err = client.CreateCluster(ctx, in)
			return err
		})
		return resp, err
	}
	resp, err := createCluster()
	// Some partitions, such as the ISO partitions, cannot tag a cluster as it
	// is created. When the tagged create fails for that reason, create the
	// cluster without tags and apply them once the cluster is available.
	taggedSeparately := false
	if err != nil && in.Tags != nil && partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = createCluster()
	}
	if err != nil {
		return nil, fmt.Errorf("create cluster: %w", err)
	}
	if resp.Cluster == nil {
		return nil, errors.New("create cluster: response holds no cluster")
	}
	// The ARN is already final in the create response; the wait is for the
	// status to settle from PROVISIONING to ACTIVE before any follow-on call.
	arn := aws.ToString(resp.Cluster.ClusterArn)
	if err := waitClusterAvailable(ctx, client, arn); err != nil {
		return nil, err
	}
	if taggedSeparately && len(ptr.Value(r.Tags)) > 0 {
		if err := r.syncTags(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	if len(ptr.Value(r.CapacityProviders)) > 0 || len(ptr.Value(r.DefaultCapacityProviderStrategy)) > 0 {
		if err := r.putCapacityProviders(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	return &ClusterOutput{Arn: arn}, nil
}

func (r *Cluster) Read(
	ctx context.Context, cfg *awsCfg, prior *ClusterOutput,
) (*ClusterOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	c, err := findCluster(ctx, client, prior.Arn)
	if err != nil {
		return nil, err
	}
	return &ClusterOutput{Arn: aws.ToString(c.ClusterArn)}, nil
}

func (r *Cluster) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Cluster, *ClusterOutput],
) (*ClusterOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	if in, needed := r.updateClusterInput(prior, arn); needed {
		if _, err := client.UpdateCluster(ctx, in); err != nil {
			return nil, fmt.Errorf("update cluster: %w", err)
		}
		if err := waitClusterAvailable(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	// The put runs after UpdateCluster; its retry on an update still in
	// flight absorbs the race with the call just made.
	if r.capacityProvidersChanged(prior) {
		if err := r.putCapacityProviders(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	return &ClusterOutput{Arn: arn}, nil
}

func (r *Cluster) Delete(ctx context.Context, cfg *awsCfg, prior *ClusterOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// Container instances still registered or draining, services or tasks
	// still stopping, and an update still in flight all make DeleteCluster
	// fail until they finish, so it retries through those conditions.
	err = retry.OnError(ctx, clusterDeleteRetryable, func(ctx context.Context) error {
		_, err := client.DeleteCluster(ctx, &ecs.DeleteClusterInput{
			Cluster: aws.String(prior.Arn),
		})
		return err
	}, retry.WithTimeout(clusterTimeout))
	if err != nil {
		// A cluster already gone counts as deleted.
		if isNotFound(err, clusterNotFoundCode) {
			return nil
		}
		return fmt.Errorf("delete cluster: %w", err)
	}
	return waitClusterDeleted(ctx, client, prior.Arn)
}

// updateClusterInput builds the UpdateCluster request for the aspects that
// changed, reporting whether any did. A nil member leaves that aspect of the
// cluster unchanged. A removed configuration block is cleared with the empty
// configuration sentinel, and a removed service-connect-defaults block with
// the documented empty-string namespace; settings have no clear operation,
// so removing them sends nothing and leaves the values already on the
// cluster in place.
func (r *Cluster) updateClusterInput(
	prior runtime.Prior[Cluster, *ClusterOutput], arn string,
) (*ecs.UpdateClusterInput, bool) {
	in := &ecs.UpdateClusterInput{Cluster: aws.String(arn)}
	needed := false
	if runtime.Changed(prior.Inputs.Configuration, r.Configuration) {
		needed = true
		in.Configuration = r.Configuration.sdk()
		if in.Configuration == nil {
			in.Configuration = &ecstypes.ClusterConfiguration{}
		}
	}
	if runtime.Changed(prior.Inputs.ServiceConnectDefaults, r.ServiceConnectDefaults) {
		needed = true
		in.ServiceConnectDefaults = r.ServiceConnectDefaults.sdk()
		if in.ServiceConnectDefaults == nil {
			in.ServiceConnectDefaults = &ecstypes.ClusterServiceConnectDefaultsRequest{
				Namespace: aws.String(""),
			}
		}
	}
	if runtime.Changed(prior.Inputs.Settings, r.Settings) && len(ptr.Value(r.Settings)) > 0 {
		needed = true
		in.Settings = clusterSettingsSDK(ptr.Value(r.Settings))
	}
	return in, needed
}

// capacityProvidersChanged reports whether either of the fields
// PutClusterCapacityProviders reconciles differs from the prior inputs. The
// two ride one whole-state call, so a change to either resends both.
func (r *Cluster) capacityProvidersChanged(prior runtime.Prior[Cluster, *ClusterOutput]) bool {
	return runtime.Changed(ptr.Value(prior.Inputs.CapacityProviders), ptr.Value(r.CapacityProviders)) ||
		runtime.Changed(prior.Inputs.DefaultCapacityProviderStrategy,
			r.DefaultCapacityProviderStrategy)
}

// putCapacityProviders reconciles the capacity-providers and
// default-capacity-provider-strategy fields with one whole-state
// PutClusterCapacityProviders call. The call requires both collections and
// replaces both sets on the cluster, so an absent field is sent as an
// explicit empty slice, which is how a removed field clears its set. The
// call retries while the cluster is still settling from a previous change,
// while another update is in flight, and while an active service still uses
// a provider being removed; each put is followed by the available wait,
// since changing providers moves the cluster through PROVISIONING.
func (r *Cluster) putCapacityProviders(
	ctx context.Context, client *ecs.Client, arn string,
) error {
	providers := []string{}
	if ptr.Value(r.CapacityProviders) != nil {
		providers = ptr.Value(r.CapacityProviders)
	}
	in := &ecs.PutClusterCapacityProvidersInput{
		Cluster:                         aws.String(arn),
		CapacityProviders:               providers,
		DefaultCapacityProviderStrategy: clusterStrategySDK(ptr.Value(r.DefaultCapacityProviderStrategy)),
	}
	err := retry.OnError(ctx, clusterPutRetryable, func(ctx context.Context) error {
		_, err := client.PutClusterCapacityProviders(ctx, in)
		return err
	}, retry.WithTimeout(clusterTimeout))
	if err != nil {
		return fmt.Errorf("put cluster capacity providers: %w", err)
	}
	return waitClusterAvailable(ctx, client, arn)
}

// syncTags reconciles the cluster's tags with the desired set, reading the
// live tags with ListTagsForResource and writing changes with TagResource
// and UntagResource against the cluster ARN.
func (r *Cluster) syncTags(ctx context.Context, client *ecs.Client, arn string) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			current := map[string]string{}
			for _, t := range resp.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &ecs.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        clusterTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &ecs.UntagResourceInput{
				ResourceArn: aws.String(arn),
				TagKeys:     remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}

// findCluster describes one cluster by name or ARN and maps the three forms
// a missing cluster takes to runtime.ErrNotFound: the typed
// ClusterNotFoundException; a response that does not hold exactly the one
// cluster asked for, since a missing cluster is usually a success whose
// Clusters list is empty with the miss reported only in the Failures array;
// and a cluster lingering in the INACTIVE status, which is a deleted cluster
// that has not yet aged out of the account.
func findCluster(
	ctx context.Context, client *ecs.Client, nameOrARN string,
) (*ecstypes.Cluster, error) {
	resp, err := client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: []string{nameOrARN},
	})
	if err != nil {
		if isNotFound(err, clusterNotFoundCode) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe clusters: %w", err)
	}
	if len(resp.Clusters) != 1 {
		return nil, runtime.ErrNotFound
	}
	c := resp.Clusters[0]
	if aws.ToString(c.Status) == clusterStatusInactive {
		return nil, runtime.ErrNotFound
	}
	return &c, nil
}

// waitClusterAvailable polls the cluster after a create or a mutation until
// its status settles on ACTIVE. A cluster attaching capacity providers moves
// through PROVISIONING first, and right after a create the describe can
// briefly miss the cluster entirely, so a not-found observation keeps
// waiting rather than failing. Any status other than those two, such as
// FAILED, fails the wait at once.
func waitClusterAvailable(ctx context.Context, client *ecs.Client, arn string) error {
	return wait.Until(ctx, fmt.Sprintf("cluster %s", arn),
		func(ctx context.Context) (bool, error) {
			c, err := findCluster(ctx, client, arn)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					return false, nil
				}
				return false, err
			}
			switch status := aws.ToString(c.Status); status {
			case clusterStatusActive:
				return true, nil
			case clusterStatusProvisioning:
				return false, nil
			default:
				return false, fmt.Errorf("cluster %s entered unexpected status %s", arn, status)
			}
		},
		wait.WithTimeout(clusterTimeout),
		wait.WithInterval(10*time.Second),
	)
}

// waitClusterDeleted polls the cluster after a delete until the find reports
// it gone, which includes the deleted cluster lingering in the INACTIVE
// status. ACTIVE and DEPROVISIONING mean the deletion is still in progress;
// any other status fails the wait at once.
func waitClusterDeleted(ctx context.Context, client *ecs.Client, arn string) error {
	return wait.Until(ctx, fmt.Sprintf("cluster %s to be deleted", arn),
		func(ctx context.Context) (bool, error) {
			c, err := findCluster(ctx, client, arn)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					return true, nil
				}
				return false, err
			}
			switch status := aws.ToString(c.Status); status {
			case clusterStatusActive, clusterStatusDeprovisioning:
				return false, nil
			default:
				return false, fmt.Errorf(
					"cluster %s entered unexpected status %s while deleting", arn, status)
			}
		},
		wait.WithTimeout(clusterTimeout),
		wait.WithInterval(10*time.Second),
	)
}

// clusterCreateRetryable reports whether a CreateCluster error clears on its
// own: the first ECS provision in an account creates the
// AWSServiceRoleForECS service-linked role asynchronously, and until IAM
// propagates it the create fails with an InvalidParameterException naming
// the role.
func clusterCreateRetryable(err error) bool {
	var invalid *ecstypes.InvalidParameterException
	return errors.As(err, &invalid) &&
		strings.Contains(invalid.ErrorMessage(), "Unable to assume the service linked role")
}

// clusterDeleteRetryable reports whether a DeleteCluster error clears on its
// own: container instances still registered or draining, services or tasks
// still stopping, or another cluster update still in flight.
func clusterDeleteRetryable(err error) bool {
	var (
		containsInstances *ecstypes.ClusterContainsContainerInstancesException
		containsServices  *ecstypes.ClusterContainsServicesException
		containsTasks     *ecstypes.ClusterContainsTasksException
		updateInProgress  *ecstypes.UpdateInProgressException
	)
	return errors.As(err, &containsInstances) || errors.As(err, &containsServices) ||
		errors.As(err, &containsTasks) || errors.As(err, &updateInProgress)
}

// clusterPutRetryable reports whether a PutClusterCapacityProviders error
// clears on its own: the cluster not yet ACTIVE after a previous change, a
// removed provider an active service still uses, or another cluster update
// still in flight.
func clusterPutRetryable(err error) bool {
	var clientErr *ecstypes.ClientException
	if errors.As(err, &clientErr) &&
		strings.Contains(clientErr.ErrorMessage(), "Cluster was not ACTIVE") {
		return true
	}
	var (
		inUse            *ecstypes.ResourceInUseException
		updateInProgress *ecstypes.UpdateInProgressException
	)
	return errors.As(err, &inUse) || errors.As(err, &updateInProgress)
}
