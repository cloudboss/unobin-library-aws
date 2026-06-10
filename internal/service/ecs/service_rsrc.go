package ecs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// serviceNotFoundCode is the error code ECS uses for a service that does not
// exist in the cluster it was looked up in.
const serviceNotFoundCode = "ServiceNotFoundException"

// serviceFailureReasonMissing is the failure reason DescribeServices reports
// for a requested service it cannot find. A missing service is usually not an
// error: the call succeeds with the miss recorded in the Failures array.
const serviceFailureReasonMissing = "MISSING"

// The service status strings ECS reports. A service being deleted moves
// through DRAINING while its tasks stop, and a deleted service remains
// describable as INACTIVE long afterward.
const (
	serviceStatusActive   = "ACTIVE"
	serviceStatusDraining = "DRAINING"
	serviceStatusInactive = "INACTIVE"
)

// serviceTimeout bounds the status waits and the retried DeleteService call.
// A service draining many tasks, or a deployment still settling, can hold a
// transition open for many minutes.
const serviceTimeout = 20 * time.Minute

// servicePropagationTimeout bounds the CreateService and UpdateService
// retries that absorb propagation of the resources a service depends on: a
// just-created cluster not yet visible, an IAM role not yet assumable, or a
// target group not yet associated with its load balancer.
const servicePropagationTimeout = 4 * time.Minute

// Service manages an ECS service. The cluster, name, scheduling strategy,
// and launch type are fixed at create time, so changing any of them replaces
// the service; every other input is reconciled in place by a single
// UpdateService call sent only when something it covers changed, and tags by
// the tag calls. Deleting drains a REPLICA service to zero desired tasks,
// then deletes it and waits for it to leave the ACTIVE status.
//
// Name is required and the cluster is optional: when cluster is unset the
// account's default cluster is used. The task definition is required and may
// be a family, family:revision, or full ARN; a bare family resolves to the
// latest ACTIVE revision only when the service is created or that input
// changes, so referencing the ecs-task-definition resource's arn output is
// what redeploys the service as revisions are registered.
//
// Desired-count applies only to REPLICA services, riding as 0 when unset at
// create; on update it is sent only when this input changed, so a value an
// autoscaling policy moved stays untouched by unrelated applies, and a
// removed desired-count leaves the running count as is. Setting
// health-check-grace-period-seconds requires at least one load-balancers
// entry, a rule checked in Create and Update. Force-delete applies only at
// delete time: it skips the drain and forces deletion of a service still
// running tasks.
//
// Service Connect, managed volume configurations, deployment controllers
// other than the ECS rolling controller, deployment alarms, service
// registries, VPC Lattice configurations, and classic load balancer names
// are not modeled.
type Service struct {
	Name                          string                                `ub:"name"`
	Cluster                       *string                               `ub:"cluster"`
	TaskDefinition                string                                `ub:"task-definition"`
	DesiredCount                  *int64                                `ub:"desired-count"`
	LaunchType                    *string                               `ub:"launch-type"`
	SchedulingStrategy            *string                               `ub:"scheduling-strategy"`
	CapacityProviderStrategy      []ServiceCapacityProviderStrategyItem `ub:"capacity-provider-strategy"`
	DeploymentConfiguration       *ServiceDeploymentConfiguration       `ub:"deployment-configuration"`
	NetworkConfiguration          *ServiceNetworkConfiguration          `ub:"network-configuration"`
	LoadBalancers                 []ServiceLoadBalancer                 `ub:"load-balancers"`
	PlacementConstraints          []ServicePlacementConstraint          `ub:"placement-constraints"`
	PlacementStrategy             []ServicePlacementStrategy            `ub:"placement-strategy"`
	PlatformVersion               *string                               `ub:"platform-version"`
	PropagateTags                 *string                               `ub:"propagate-tags"`
	AvailabilityZoneRebalancing   *string                               `ub:"availability-zone-rebalancing"`
	EnableECSManagedTags          *bool                                 `ub:"enable-ecs-managed-tags"`
	EnableExecuteCommand          *bool                                 `ub:"enable-execute-command"`
	HealthCheckGracePeriodSeconds *int64                                `ub:"health-check-grace-period-seconds"`
	Tags                          map[string]string                     `ub:"tags"`
	ForceDelete                   *bool                                 `ub:"force-delete"`
}

// ServiceOutput holds the values ECS computes for a service: its ARN and the
// ARN of the cluster it runs in. Both are immutable and together they are the
// identity handle, so Read and Delete key off them from the prior outputs; on
// a replace the receiver already holds the new cluster and name while the old
// service still needs to be found and removed. The cluster ARN is also the
// normalized form of the cluster input, which may be a short name or absent.
type ServiceOutput struct {
	Arn        string `ub:"arn"`
	ClusterArn string `ub:"cluster-arn"`
}

func (r *Service) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs ECS fixes when a service is created: the
// cluster, name, and scheduling strategy, which CloudFormation also marks
// create-only, and the launch type, which UpdateService has no member for.
// Everything else changes in place through UpdateService or the tag calls.
func (r *Service) ReplaceFields() []string {
	return []string{
		"cluster",
		"name",
		"scheduling-strategy",
		"launch-type",
	}
}

// Defaults marks the collection inputs a service may omit.
func (r Service) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.CapacityProviderStrategy),
		defaults.Optional(r.LoadBalancers),
		defaults.Optional(r.PlacementConstraints),
		defaults.Optional(r.PlacementStrategy),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules ECS places on a service's inputs: the
// launch type, scheduling strategy, tag propagation, zone rebalancing, and
// public IP enums; the API rule that a launch type and a capacity provider
// strategy cannot be combined, with neither given the cluster's default
// strategy applies; the strategy item base and weight ranges; the health
// check grace period range; non-empty awsvpc subnets; the container port
// range; the placement list sizes; and the per-element placement rules. The
// rule that a health check grace period needs at least one load balancer
// keys on the list's contents, so it is checked in Create and Update rather
// than declared here.
func (r Service) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.LaunchType, r.CapacityProviderStrategy),
		constraint.When(constraint.Present(r.LaunchType)).
			Require(constraint.OneOf(r.LaunchType, "EC2", "FARGATE", "EXTERNAL")).
			Message("launch-type must be EC2, FARGATE, or EXTERNAL"),
		constraint.When(constraint.Present(r.SchedulingStrategy)).
			Require(constraint.OneOf(r.SchedulingStrategy, "REPLICA", "DAEMON")).
			Message("scheduling-strategy must be REPLICA or DAEMON"),
		constraint.When(constraint.Present(r.PropagateTags)).
			Require(constraint.OneOf(r.PropagateTags, "SERVICE", "TASK_DEFINITION", "NONE")).
			Message("propagate-tags must be SERVICE, TASK_DEFINITION, or NONE"),
		constraint.When(constraint.Present(r.AvailabilityZoneRebalancing)).
			Require(constraint.OneOf(r.AvailabilityZoneRebalancing, "ENABLED", "DISABLED")).
			Message("availability-zone-rebalancing must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.HealthCheckGracePeriodSeconds)).
			Require(constraint.AtLeast(r.HealthCheckGracePeriodSeconds, 0),
				constraint.AtMost(r.HealthCheckGracePeriodSeconds, 2147483647)).
			Message("health-check-grace-period-seconds must be between 0 and 2147483647"),
		constraint.When(constraint.Present(r.NetworkConfiguration)).
			Require(constraint.NotEmpty(r.NetworkConfiguration.Subnets)).
			Message("network-configuration subnets must not be empty"),
		constraint.When(constraint.Present(r.NetworkConfiguration.AssignPublicIp)).
			Require(constraint.OneOf(r.NetworkConfiguration.AssignPublicIp,
				"ENABLED", "DISABLED")).
			Message("assign-public-ip must be ENABLED or DISABLED"),
		constraint.ForEach(r.CapacityProviderStrategy,
			func(item ServiceCapacityProviderStrategyItem) []constraint.Constraint {
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
		constraint.ForEach(r.LoadBalancers,
			func(lb ServiceLoadBalancer) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.AtLeast(lb.ContainerPort, 0),
						constraint.AtMost(lb.ContainerPort, 65536)).
						Message("container-port must be between 0 and 65536"),
				}
			}),
		constraint.Must(constraint.MaxItems(r.PlacementConstraints, 10)).
			Message("placement-constraints allows at most 10 entries"),
		constraint.ForEach(r.PlacementConstraints,
			func(c ServicePlacementConstraint) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(c.Type, "distinctInstance", "memberOf")).
						Message("type must be distinctInstance or memberOf"),
					constraint.When(constraint.Equals(c.Type, "memberOf")).
						Require(constraint.Present(c.Expression)).
						Message("a memberOf placement constraint requires an expression"),
					constraint.When(constraint.Equals(c.Type, "distinctInstance")).
						Require(constraint.Absent(c.Expression)).
						Message("a distinctInstance placement constraint takes no expression"),
				}
			}),
		constraint.Must(constraint.MaxItems(r.PlacementStrategy, 5)).
			Message("placement-strategy allows at most 5 entries"),
		constraint.ForEach(r.PlacementStrategy,
			func(s ServicePlacementStrategy) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(s.Type, "random", "spread", "binpack")).
						Message("type must be random, spread, or binpack"),
					constraint.When(constraint.Equals(s.Type, "random")).
						Require(constraint.Absent(s.Field)).
						Message("a random placement strategy must omit field"),
					constraint.When(constraint.Equals(s.Type, "binpack")).
						Require(constraint.OneOf(s.Field, "cpu", "memory")).
						Message("a binpack placement strategy field must be cpu or memory"),
				}
			}),
	}
}

// Create creates the service and waits for it to settle in the ACTIVE
// status. The create call retries for a few minutes over the propagation
// windows of the resources a new service typically depends on, reusing one
// client token across attempts so a retry resumes the same creation instead
// of starting another. The wait matters even on success: a describe right
// after the create can briefly miss the service, or find the INACTIVE or
// DRAINING predecessor when a name is reused after a delete, so the outputs
// come from the settled describe rather than the create response. Some
// partitions, such as the ISO partitions, cannot tag a service as it is
// created; when the tagged create fails for that reason, the service is
// created without tags and tagged once it is active.
func (r *Service) Create(ctx context.Context, cfg any) (*ServiceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	token, err := newClientToken()
	if err != nil {
		return nil, err
	}
	in := r.createInput(token)
	createService := func() (*ecs.CreateServiceOutput, error) {
		var resp *ecs.CreateServiceOutput
		err := retry.OnError(ctx, serviceCreateRetryable, func(ctx context.Context) error {
			var err error
			resp, err = client.CreateService(ctx, in)
			return err
		}, retry.WithTimeout(servicePropagationTimeout))
		return resp, err
	}
	resp, err := createService()
	taggedSeparately := false
	if err != nil && in.Tags != nil && partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = createService()
	}
	if err != nil {
		return nil, fmt.Errorf("create service: %w", err)
	}
	if resp.Service == nil {
		return nil, errors.New("create service: response holds no service")
	}
	arn := aws.ToString(resp.Service.ServiceArn)
	svc, err := waitServiceActive(ctx, client, r.Cluster, arn)
	if err != nil {
		return nil, err
	}
	if taggedSeparately && len(r.Tags) > 0 {
		// The separate tagging only happens for explicitly configured tags, so
		// a partition that cannot tag the service at all is a real failure.
		if _, err := client.TagResource(ctx, &ecs.TagResourceInput{
			ResourceArn: aws.String(arn),
			Tags:        tagsSDK(r.Tags),
		}); err != nil {
			return nil, fmt.Errorf("tag service: %w", err)
		}
	}
	return &ServiceOutput{
		Arn:        aws.ToString(svc.ServiceArn),
		ClusterArn: aws.ToString(svc.ClusterArn),
	}, nil
}

// Read describes the service by the prior ARNs. A deleted service describes
// as INACTIVE long after deletion and as DRAINING while its tasks stop, so
// any status other than ACTIVE reads as not found; Create and Delete run
// their own waits, leaving no transitional status for Read to wait out.
func (r *Service) Read(
	ctx context.Context, cfg any, prior *ServiceOutput,
) (*ServiceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	svc, err := findService(ctx, client, aws.String(prior.ClusterArn), prior.Arn)
	if err != nil {
		return nil, err
	}
	if aws.ToString(svc.Status) != serviceStatusActive {
		return nil, runtime.ErrNotFound
	}
	return &ServiceOutput{
		Arn:        aws.ToString(svc.ServiceArn),
		ClusterArn: aws.ToString(svc.ClusterArn),
	}, nil
}

// Update reconciles every changed input with one UpdateService call, skipped
// entirely when nothing it covers changed, and reconciles tags separately
// when they did. The update retries over the same IAM and load balancer
// propagation windows as create, then waits for the ACTIVE status, which an
// ordinary deployment never leaves. The outputs cannot change, so the prior
// outputs are returned.
func (r *Service) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Service, *ServiceOutput],
) (*ServiceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if in, needed := r.updateServiceInput(prior); needed {
		err := retry.OnError(ctx, serviceUpdateRetryable, func(ctx context.Context) error {
			_, err := client.UpdateService(ctx, in)
			return err
		}, retry.WithTimeout(servicePropagationTimeout))
		if err != nil {
			return nil, fmt.Errorf("update service: %w", err)
		}
		cluster := aws.String(prior.Outputs.ClusterArn)
		if _, err := waitServiceActive(ctx, client, cluster, prior.Outputs.Arn); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncResourceTags(ctx, client, prior.Outputs.Arn, r.Tags); err != nil {
			// Listing tags in a cluster that was deleted out of band fails
			// with the inactive-cluster error, which means the service is
			// gone rather than the tagging having failed.
			if serviceClusterInactive(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, err
		}
	}
	return prior.Outputs, nil
}

// Delete removes the service named by the prior outputs; the receiver's
// cluster and name are not used as identifiers, since on a replace they
// already name the new service. A service that is gone, or lingering as
// INACTIVE, counts as deleted. A REPLICA service still ACTIVE is first
// drained to zero desired tasks unless force-delete is set; a DAEMON service
// has no desired count to zero, decided from the describe rather than the
// receiver because on a replace the receiver's strategy describes the new
// service. The delete call retries while the drain's deployment settles and
// while dependent objects detach, and then the wait holds until the service
// leaves the ACTIVE and DRAINING statuses; a service that stops describing
// at all counts as deleted.
func (r *Service) Delete(ctx context.Context, cfg any, prior *ServiceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	cluster := aws.String(prior.ClusterArn)
	svc, err := findService(ctx, client, cluster, prior.Arn)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		return err
	}
	status := aws.ToString(svc.Status)
	if status == serviceStatusInactive {
		return nil
	}
	force := aws.ToBool(r.ForceDelete)
	if status != serviceStatusDraining && !force &&
		svc.SchedulingStrategy != ecstypes.SchedulingStrategyDaemon {
		_, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
			Cluster:      cluster,
			Service:      aws.String(prior.Arn),
			DesiredCount: aws.Int32(0),
		})
		if err != nil && !drainSkippable(err) {
			return fmt.Errorf("drain service: %w", err)
		}
	}
	err = retry.OnError(ctx, serviceDeleteRetryable, func(ctx context.Context) error {
		_, err := client.DeleteService(ctx, &ecs.DeleteServiceInput{
			Cluster: cluster,
			Service: aws.String(prior.Arn),
			Force:   r.ForceDelete,
		})
		return err
	}, retry.WithTimeout(serviceTimeout))
	if err != nil {
		// A service or cluster already gone counts as deleted.
		if isNotFound(err, clusterNotFoundCode, serviceNotFoundCode) {
			return nil
		}
		return fmt.Errorf("delete service: %w", err)
	}
	return waitServiceInactive(ctx, client, cluster, prior.Arn)
}

// validate checks the rule that cannot be a declarative constraint because
// it keys on a list's contents: a health check grace period only means
// something when health checks exist, so the API rejects it on a service
// with no load balancer.
func (r *Service) validate() error {
	if r.HealthCheckGracePeriodSeconds != nil && len(r.LoadBalancers) == 0 {
		return errors.New(
			"health-check-grace-period-seconds requires at least one load-balancers entry")
	}
	return nil
}

// daemon reports whether the service uses the DAEMON scheduling strategy.
// The strategy is fixed at create time, so the input always describes the
// service the receiver manages.
func (r *Service) daemon() bool {
	return aws.ToString(r.SchedulingStrategy) == string(ecstypes.SchedulingStrategyDaemon)
}

// createInput assembles the CreateService request. A REPLICA service, which
// is also what an omitted strategy creates, always sends a desired count,
// with an unset input riding as 0 because the API requires the member, and
// always sends the deployment configuration with its documented defaults. A
// DAEMON service sends neither, which the API would reject. Optional members
// stay out of the request when unset so the server defaults apply.
func (r *Service) createInput(token string) *ecs.CreateServiceInput {
	in := &ecs.CreateServiceInput{
		ServiceName:                   aws.String(r.Name),
		ClientToken:                   aws.String(token),
		Cluster:                       r.Cluster,
		TaskDefinition:                aws.String(r.TaskDefinition),
		PlatformVersion:               r.PlatformVersion,
		EnableECSManagedTags:          aws.ToBool(r.EnableECSManagedTags),
		EnableExecuteCommand:          aws.ToBool(r.EnableExecuteCommand),
		HealthCheckGracePeriodSeconds: ptr.Int32(r.HealthCheckGracePeriodSeconds),
		NetworkConfiguration:          r.NetworkConfiguration.sdk(),
		Tags:                          tagsSDK(r.Tags),
	}
	if r.LaunchType != nil {
		in.LaunchType = ecstypes.LaunchType(*r.LaunchType)
	}
	if r.SchedulingStrategy != nil {
		in.SchedulingStrategy = ecstypes.SchedulingStrategy(*r.SchedulingStrategy)
	}
	if r.PropagateTags != nil {
		in.PropagateTags = ecstypes.PropagateTags(*r.PropagateTags)
	}
	if r.AvailabilityZoneRebalancing != nil {
		in.AvailabilityZoneRebalancing =
			ecstypes.AvailabilityZoneRebalancing(*r.AvailabilityZoneRebalancing)
	}
	if len(r.CapacityProviderStrategy) > 0 {
		in.CapacityProviderStrategy = serviceStrategySDK(r.CapacityProviderStrategy)
	}
	if len(r.LoadBalancers) > 0 {
		in.LoadBalancers = serviceLoadBalancersSDK(r.LoadBalancers)
	}
	if len(r.PlacementConstraints) > 0 {
		in.PlacementConstraints = servicePlacementConstraintsSDK(r.PlacementConstraints)
	}
	if len(r.PlacementStrategy) > 0 {
		in.PlacementStrategy = servicePlacementStrategySDK(r.PlacementStrategy)
	}
	if !r.daemon() {
		in.DesiredCount = aws.Int32(int32(aws.ToInt64(r.DesiredCount)))
	}
	in.DeploymentConfiguration = serviceDeploymentConfigurationSDK(
		r.DeploymentConfiguration, r.daemon(), false)
	return in
}

// updateServiceInput builds the UpdateService request for the inputs that
// changed, reporting whether any did, with the service addressed by the
// prior ARNs. A member is included only when its input changed, so an apply
// that changes nothing sends nothing. A removed scalar input is left as it
// is on the service, since UpdateService has no per-field clear; a removed
// collection is cleared with an explicit empty list; a removed deployment
// configuration is restored to its documented defaults with the circuit
// breaker turned off by the empty-object clear; and a removed network
// configuration is left unchanged, since a service cannot leave the awsvpc
// network mode its task definition demands. The desired count is sent only
// when that input changed to a set value, so a count moved by an autoscaling
// policy is never stomped by an unrelated apply, and it is never sent for a
// DAEMON service. Changing the capacity provider strategy sets
// ForceNewDeployment, which the API requires for that change.
func (r *Service) updateServiceInput(
	prior runtime.Prior[Service, *ServiceOutput],
) (*ecs.UpdateServiceInput, bool) {
	in := &ecs.UpdateServiceInput{
		Cluster: aws.String(prior.Outputs.ClusterArn),
		Service: aws.String(prior.Outputs.Arn),
	}
	needed := false
	if runtime.Changed(prior.Inputs.TaskDefinition, r.TaskDefinition) {
		needed = true
		in.TaskDefinition = aws.String(r.TaskDefinition)
	}
	if runtime.Changed(prior.Inputs.DesiredCount, r.DesiredCount) &&
		r.DesiredCount != nil && !r.daemon() {
		needed = true
		in.DesiredCount = ptr.Int32(r.DesiredCount)
	}
	if runtime.Changed(prior.Inputs.DeploymentConfiguration, r.DeploymentConfiguration) {
		needed = true
		in.DeploymentConfiguration = serviceDeploymentConfigurationSDK(
			r.DeploymentConfiguration, r.daemon(), true)
	}
	if runtime.Changed(prior.Inputs.CapacityProviderStrategy, r.CapacityProviderStrategy) {
		needed = true
		in.CapacityProviderStrategy = serviceStrategySDK(r.CapacityProviderStrategy)
		in.ForceNewDeployment = true
	}
	if runtime.Changed(prior.Inputs.NetworkConfiguration, r.NetworkConfiguration) &&
		r.NetworkConfiguration != nil {
		needed = true
		in.NetworkConfiguration = r.NetworkConfiguration.sdk()
	}
	if runtime.Changed(prior.Inputs.LoadBalancers, r.LoadBalancers) {
		needed = true
		in.LoadBalancers = serviceLoadBalancersSDK(r.LoadBalancers)
	}
	if runtime.Changed(prior.Inputs.PlacementConstraints, r.PlacementConstraints) {
		needed = true
		in.PlacementConstraints = servicePlacementConstraintsSDK(r.PlacementConstraints)
	}
	if runtime.Changed(prior.Inputs.PlacementStrategy, r.PlacementStrategy) {
		needed = true
		in.PlacementStrategy = servicePlacementStrategySDK(r.PlacementStrategy)
	}
	if runtime.Changed(prior.Inputs.PlatformVersion, r.PlatformVersion) &&
		r.PlatformVersion != nil {
		needed = true
		in.PlatformVersion = r.PlatformVersion
	}
	if runtime.Changed(prior.Inputs.PropagateTags, r.PropagateTags) && r.PropagateTags != nil {
		needed = true
		in.PropagateTags = ecstypes.PropagateTags(*r.PropagateTags)
	}
	if runtime.Changed(prior.Inputs.AvailabilityZoneRebalancing,
		r.AvailabilityZoneRebalancing) && r.AvailabilityZoneRebalancing != nil {
		needed = true
		in.AvailabilityZoneRebalancing =
			ecstypes.AvailabilityZoneRebalancing(*r.AvailabilityZoneRebalancing)
	}
	if runtime.Changed(prior.Inputs.EnableECSManagedTags, r.EnableECSManagedTags) &&
		r.EnableECSManagedTags != nil {
		needed = true
		in.EnableECSManagedTags = r.EnableECSManagedTags
	}
	if runtime.Changed(prior.Inputs.EnableExecuteCommand, r.EnableExecuteCommand) &&
		r.EnableExecuteCommand != nil {
		needed = true
		in.EnableExecuteCommand = r.EnableExecuteCommand
	}
	if runtime.Changed(prior.Inputs.HealthCheckGracePeriodSeconds,
		r.HealthCheckGracePeriodSeconds) && r.HealthCheckGracePeriodSeconds != nil {
		needed = true
		in.HealthCheckGracePeriodSeconds = ptr.Int32(r.HealthCheckGracePeriodSeconds)
	}
	return in, needed
}

// newClientToken returns a fresh idempotency token for CreateService, which
// does not auto-fill one the way some SDK operations do. The token is built
// once per create so every attempt in the retry window resumes the same
// creation instead of starting another.
func newClientToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate client token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// findService describes one service by name or ARN, with a nil cluster
// addressing the account's default cluster, and maps the forms a missing
// service takes to runtime.ErrNotFound: the typed ClusterNotFoundException
// and ServiceNotFoundException, and a response that reports the service only
// in the Failures array with the MISSING reason, which is how a recently
// deleted service usually describes. The status is returned as is; callers
// decide what a non-ACTIVE status means.
func findService(
	ctx context.Context, client *ecs.Client, cluster *string, id string,
) (*ecstypes.Service, error) {
	resp, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  cluster,
		Services: []string{id},
	})
	if err != nil {
		if isNotFound(err, clusterNotFoundCode, serviceNotFoundCode) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe services: %w", err)
	}
	for _, failure := range resp.Failures {
		if aws.ToString(failure.Reason) == serviceFailureReasonMissing {
			return nil, runtime.ErrNotFound
		}
	}
	if len(resp.Services) == 0 {
		return nil, runtime.ErrNotFound
	}
	return &resp.Services[0], nil
}

// waitServiceActive polls the service after a create or an update until its
// status settles on ACTIVE, returning the settled describe so the caller can
// build outputs from confirmed values. Right after a create the describe can
// briefly miss the service, so a not-found observation keeps waiting, and
// when a name is reused after a delete it can find the INACTIVE or DRAINING
// predecessor, so those statuses keep waiting too.
func waitServiceActive(
	ctx context.Context, client *ecs.Client, cluster *string, id string,
) (*ecstypes.Service, error) {
	var settled *ecstypes.Service
	err := wait.Until(ctx, fmt.Sprintf("service %s to be active", id),
		func(ctx context.Context) (bool, error) {
			svc, err := findService(ctx, client, cluster, id)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					return false, nil
				}
				return false, err
			}
			switch status := aws.ToString(svc.Status); status {
			case serviceStatusActive:
				settled = svc
				return true, nil
			case serviceStatusInactive, serviceStatusDraining:
				return false, nil
			default:
				return false, fmt.Errorf("service %s entered unexpected status %s", id, status)
			}
		},
		wait.WithTimeout(serviceTimeout),
		wait.WithInterval(10*time.Second),
	)
	if err != nil {
		return nil, err
	}
	return settled, nil
}

// waitServiceInactive polls the service after a delete until it reports the
// INACTIVE status or stops describing at all; deleted services linger as
// INACTIVE and eventually disappear, so a not-found observation also counts
// as deleted. ACTIVE and DRAINING mean the deletion is still in progress.
func waitServiceInactive(
	ctx context.Context, client *ecs.Client, cluster *string, id string,
) error {
	return wait.Until(ctx, fmt.Sprintf("service %s to be deleted", id),
		func(ctx context.Context) (bool, error) {
			svc, err := findService(ctx, client, cluster, id)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					return true, nil
				}
				return false, err
			}
			switch status := aws.ToString(svc.Status); status {
			case serviceStatusInactive:
				return true, nil
			case serviceStatusActive, serviceStatusDraining:
				return false, nil
			default:
				return false, fmt.Errorf(
					"service %s entered unexpected status %s while deleting", id, status)
			}
		},
		wait.WithTimeout(serviceTimeout),
		wait.WithInterval(10*time.Second),
	)
}

// serviceCreateRetryable reports whether a CreateService error clears on its
// own: a just-created cluster not yet visible, an IAM service role whose
// permissions have not propagated, a target group whose load balancer
// association has not propagated, or the service-linked role still being
// created. The messages ride on InvalidParameterException and are matched as
// substrings.
func serviceCreateRetryable(err error) bool {
	var clusterNotFound *ecstypes.ClusterNotFoundException
	if errors.As(err, &clusterNotFound) {
		return true
	}
	var invalid *ecstypes.InvalidParameterException
	if !errors.As(err, &invalid) {
		return false
	}
	msg := invalid.ErrorMessage()
	return strings.Contains(msg,
		"verify that the ECS service role being passed has the proper permissions") ||
		strings.Contains(msg, "does not have an associated load balancer") ||
		strings.Contains(msg, "Unable to assume the service linked role")
}

// serviceUpdateRetryable reports whether an UpdateService error clears on
// its own: the IAM service role permission and load balancer association
// propagation windows, the same two an update can hit when it attaches a
// just-created role or target group.
func serviceUpdateRetryable(err error) bool {
	var invalid *ecstypes.InvalidParameterException
	if !errors.As(err, &invalid) {
		return false
	}
	msg := invalid.ErrorMessage()
	return strings.Contains(msg,
		"verify that the ECS service role being passed has the proper permissions") ||
		strings.Contains(msg, "does not have an associated load balancer")
}

// serviceDeleteRetryable reports whether a DeleteService error clears on its
// own: the drain's deployment still settling, or a dependent object, such as
// a task set, still detaching.
func serviceDeleteRetryable(err error) bool {
	var invalid *ecstypes.InvalidParameterException
	if errors.As(err, &invalid) && strings.Contains(invalid.ErrorMessage(),
		"The service cannot be stopped while deployments are active.") {
		return true
	}
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "DependencyViolation" &&
		strings.Contains(apiErr.ErrorMessage(), "has a dependent object")
}

// drainSkippable reports whether the drain before a delete can be skipped
// because its error already means the service is past draining: gone
// entirely, or no longer active, both of which the delete call and its
// not-found tolerance handle.
func drainSkippable(err error) bool {
	var notActive *ecstypes.ServiceNotActiveException
	return errors.As(err, &notActive) ||
		isNotFound(err, clusterNotFoundCode, serviceNotFoundCode)
}

// serviceClusterInactive reports whether err is the inactive-cluster error
// ListTagsForResource returns when the service's cluster was deleted out of
// band, which means the service itself is gone rather than the call having
// failed.
func serviceClusterInactive(err error) bool {
	var invalid *ecstypes.InvalidParameterException
	return errors.As(err, &invalid) && strings.Contains(invalid.ErrorMessage(),
		"The specified cluster is inactive. Specify an active cluster and try again.")
}
