package ecs

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// ServiceDeploymentConfiguration controls how many tasks run during a rolling
// deployment. For a REPLICA service an omitted maximum-percent rides as 200
// and an omitted minimum-healthy-percent as 100, the documented defaults, so
// removing either restores its default on the next apply. A DAEMON service
// has no task count to batch over: maximum-percent is never sent for one, and
// minimum-healthy-percent is sent only when set to a value other than 100.
type ServiceDeploymentConfiguration struct {
	MaximumPercent           *int64                           `ub:"maximum-percent"`
	MinimumHealthyPercent    *int64                           `ub:"minimum-healthy-percent"`
	DeploymentCircuitBreaker *ServiceDeploymentCircuitBreaker `ub:"deployment-circuit-breaker"`
}

// ServiceDeploymentCircuitBreaker fails a deployment that cannot reach a
// steady state instead of letting it retry forever, and with rollback on
// returns the service to the last deployment that completed. Removing the
// block turns the breaker off on the next apply, through the documented
// empty-object clear.
type ServiceDeploymentCircuitBreaker struct {
	Enable   bool `ub:"enable"`
	Rollback bool `ub:"rollback"`
}

// ServiceNetworkConfiguration is the awsvpc networking for the service's
// tasks: the subnets the task network interfaces are placed in (required,
// non-empty, at most 16), the security groups attached to them (the VPC
// default group when omitted, at most 5), and whether each interface gets a
// public IP, ENABLED or DISABLED (the server default). The block is required
// when the task definition uses the awsvpc network mode, which includes every
// Fargate service, and rejected for other network modes; ENABLED is accepted
// only on Fargate. Both rules are enforced by the API since they depend on
// the task definition. Removing the block leaves the networking unchanged,
// since the API has no clear for it and a service cannot leave awsvpc mode.
type ServiceNetworkConfiguration struct {
	Subnets        []string  `ub:"subnets"`
	SecurityGroups *[]string `ub:"security-groups"`
	AssignPublicIp *string   `ub:"assign-public-ip"`
}

// ServiceLoadBalancer registers the service's tasks with one Application or
// Network Load Balancer target group: tasks are added to the target group as
// they start and deregistered as they stop. The container name and port pick
// which container in the task definition receives the traffic. Classic load
// balancer names are not modeled, so the target group ARN is required.
type ServiceLoadBalancer struct {
	ContainerName  string `ub:"container-name"`
	ContainerPort  int64  `ub:"container-port"`
	TargetGroupArn string `ub:"target-group-arn"`
}

// ServicePlacementConstraint is one rule a candidate container instance must
// satisfy: distinctInstance places each task on a different instance, and
// memberOf restricts placement to instances matching the required cluster
// query language expression. distinctInstance takes no expression. Placement
// constraints are rejected for Fargate tasks, a task-definition-dependent
// rule the API enforces.
type ServicePlacementConstraint struct {
	Type       string  `ub:"type"`
	Expression *string `ub:"expression"`
}

// ServicePlacementStrategy is one step of the ordered strategy that picks a
// container instance for each task: random ignores field and must omit it,
// spread balances over the named field, such as instanceId or an attribute
// expression like attribute:ecs.availability-zone, and binpack packs tasks
// onto the instances with the least remaining cpu or memory, named in
// lowercase as the API requires even though describes echo the field in
// uppercase. The spread field values are enforced by the API.
type ServicePlacementStrategy struct {
	Type  string  `ub:"type"`
	Field *string `ub:"field"`
}

// ServiceCapacityProviderStrategyItem is one entry of the service's capacity
// provider strategy. The capacity provider is FARGATE, FARGATE_SPOT, or the
// name of a capacity provider attached to the cluster; that membership, and
// the API rule that only one item may define a nonzero base, are enforced by
// ECS rather than validated here. An omitted base or weight rides as 0, the
// API default.
type ServiceCapacityProviderStrategyItem struct {
	CapacityProvider string `ub:"capacity-provider"`
	Base             *int64 `ub:"base"`
	Weight           *int64 `ub:"weight"`
}

// sdk converts the circuit breaker block to its SDK type, returning nil for
// a nil block so the caller decides between omitting the member and sending
// the empty-object clear.
func (b *ServiceDeploymentCircuitBreaker) sdk() *ecstypes.DeploymentCircuitBreaker {
	if b == nil {
		return nil
	}
	return &ecstypes.DeploymentCircuitBreaker{
		Enable:   b.Enable,
		Rollback: b.Rollback,
	}
}

// sdk converts the network configuration block to the SDK member, returning
// nil for a nil block so an absent configuration stays out of the request.
// The block's fields live on the awsvpc configuration the member holds.
func (n *ServiceNetworkConfiguration) sdk() *ecstypes.NetworkConfiguration {
	if n == nil {
		return nil
	}
	awsvpc := &ecstypes.AwsVpcConfiguration{
		Subnets:        n.Subnets,
		SecurityGroups: ptr.Value(n.SecurityGroups),
	}
	if n.AssignPublicIp != nil {
		awsvpc.AssignPublicIp = ecstypes.AssignPublicIp(*n.AssignPublicIp)
	}
	return &ecstypes.NetworkConfiguration{AwsvpcConfiguration: awsvpc}
}

// serviceLoadBalancersSDK converts the load balancer list to its SDK type. It
// always returns a non-nil slice, even for an empty input, because on update
// an explicit empty list is how a removed field detaches every load balancer,
// while a nil member would leave them unchanged.
func serviceLoadBalancersSDK(lbs []ServiceLoadBalancer) []ecstypes.LoadBalancer {
	out := make([]ecstypes.LoadBalancer, 0, len(lbs))
	for _, lb := range lbs {
		out = append(out, ecstypes.LoadBalancer{
			ContainerName:  aws.String(lb.ContainerName),
			ContainerPort:  aws.Int32(int32(lb.ContainerPort)),
			TargetGroupArn: aws.String(lb.TargetGroupArn),
		})
	}
	return out
}

// servicePlacementConstraintsSDK converts the placement constraint list to
// its SDK type. It always returns a non-nil slice, even for an empty input,
// because on update an explicit empty list is how a removed field clears the
// constraints, while a nil member would leave them unchanged.
func servicePlacementConstraintsSDK(
	constraints []ServicePlacementConstraint,
) []ecstypes.PlacementConstraint {
	out := make([]ecstypes.PlacementConstraint, 0, len(constraints))
	for _, c := range constraints {
		out = append(out, ecstypes.PlacementConstraint{
			Type:       ecstypes.PlacementConstraintType(c.Type),
			Expression: c.Expression,
		})
	}
	return out
}

// servicePlacementStrategySDK converts the placement strategy list to its SDK
// type, preserving order. It always returns a non-nil slice, even for an
// empty input, because on update an explicit empty list is how a removed
// field clears the strategy, while a nil member would leave it unchanged.
func servicePlacementStrategySDK(
	strategy []ServicePlacementStrategy,
) []ecstypes.PlacementStrategy {
	out := make([]ecstypes.PlacementStrategy, 0, len(strategy))
	for _, s := range strategy {
		out = append(out, ecstypes.PlacementStrategy{
			Type:  ecstypes.PlacementStrategyType(s.Type),
			Field: s.Field,
		})
	}
	return out
}

// serviceStrategySDK converts the capacity provider strategy to its SDK type.
// It always returns a non-nil slice, even for an empty input, because on
// update an explicit empty list is how a removed field reverts the service to
// the launch type or the cluster's default strategy, while a nil member would
// leave the strategy unchanged.
func serviceStrategySDK(
	items []ServiceCapacityProviderStrategyItem,
) []ecstypes.CapacityProviderStrategyItem {
	out := make([]ecstypes.CapacityProviderStrategyItem, 0, len(items))
	for _, item := range items {
		out = append(out, ecstypes.CapacityProviderStrategyItem{
			CapacityProvider: aws.String(item.CapacityProvider),
			Base:             int32(aws.ToInt64(item.Base)),
			Weight:           int32(aws.ToInt64(item.Weight)),
		})
	}
	return out
}

// serviceDeploymentConfigurationSDK builds the DeploymentConfiguration member
// from the block for the given scheduling strategy. For REPLICA the member is
// always built, with the documented defaults of 200 maximum percent and 100
// minimum healthy percent standing in for omitted fields, so removing a field
// restores its default. For DAEMON the maximum percent is never included, the
// API rejects it. At create the minimum healthy percent rides only when set
// to a value other than 100, the server default, and a DAEMON member with
// nothing to say is nil; on reconcile it is always included, the set value or
// 100, so a changed config can move the service back to the default. When
// reconcile is true, an absent circuit breaker is sent as the documented
// empty-object clear, which turns a removed breaker off and is a no-op on a
// service that never had one; at create the breaker is simply omitted instead.
func serviceDeploymentConfigurationSDK(
	block *ServiceDeploymentConfiguration, daemon, reconcile bool,
) *ecstypes.DeploymentConfiguration {
	out := &ecstypes.DeploymentConfiguration{}
	if block != nil {
		out.DeploymentCircuitBreaker = block.DeploymentCircuitBreaker.sdk()
	}
	if reconcile && out.DeploymentCircuitBreaker == nil {
		out.DeploymentCircuitBreaker = &ecstypes.DeploymentCircuitBreaker{}
	}
	if daemon {
		if reconcile {
			out.MinimumHealthyPercent = aws.Int32(100)
			if block != nil && block.MinimumHealthyPercent != nil {
				out.MinimumHealthyPercent = ptr.Int32(block.MinimumHealthyPercent)
			}
			return out
		}
		if block != nil && block.MinimumHealthyPercent != nil &&
			*block.MinimumHealthyPercent != 100 {
			out.MinimumHealthyPercent = ptr.Int32(block.MinimumHealthyPercent)
		}
		if out.DeploymentCircuitBreaker == nil && out.MinimumHealthyPercent == nil {
			return nil
		}
		return out
	}
	out.MaximumPercent = aws.Int32(200)
	out.MinimumHealthyPercent = aws.Int32(100)
	if block != nil {
		if block.MaximumPercent != nil {
			out.MaximumPercent = ptr.Int32(block.MaximumPercent)
		}
		if block.MinimumHealthyPercent != nil {
			out.MinimumHealthyPercent = ptr.Int32(block.MinimumHealthyPercent)
		}
	}
	return out
}
