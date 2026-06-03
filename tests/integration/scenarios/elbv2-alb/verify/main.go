// verify checks the load-balancing group the scenario applied against the phase
// named in the VERIFY_PHASE environment variable. It looks resources up by their
// stable names because the driver passes no plan outputs into verify, and it
// reads only cloud state: applied requires the load balancer to be active with
// the idle-timeout attribute the scenario set, the target group to exist, an
// HTTP listener on the load balancer, and a priority-100 rule on that listener;
// destroyed requires the load balancer and target group to be gone. Tearing the
// group down is the destroy plan's job, not the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

const (
	loadBalancerName = "unobin-it-alb"
	targetGroupName  = "unobin-it-alb-tg"
	idleTimeoutKey   = "idle_timeout.timeout_seconds"
	idleTimeoutValue = "120"
	rulePriority     = "100"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := elasticloadbalancingv2.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *elasticloadbalancingv2.Client) error {
	lb, err := findLoadBalancer(ctx, client)
	if err != nil {
		return err
	}
	if lb == nil {
		return fmt.Errorf("load balancer %s not found", loadBalancerName)
	}
	if lb.State == nil || lb.State.Code != elbv2types.LoadBalancerStateEnumActive {
		return fmt.Errorf("load balancer %s is not active", loadBalancerName)
	}
	lbArn := aws.ToString(lb.LoadBalancerArn)
	if err := checkIdleTimeout(ctx, client, lbArn); err != nil {
		return err
	}

	tg, err := findTargetGroup(ctx, client)
	if err != nil {
		return err
	}
	if tg == nil {
		return fmt.Errorf("target group %s not found", targetGroupName)
	}

	listener, err := findListener(ctx, client, lbArn)
	if err != nil {
		return err
	}
	if listener == nil {
		return fmt.Errorf("no listener found on load balancer %s", loadBalancerName)
	}
	if err := checkRule(ctx, client, aws.ToString(listener.ListenerArn)); err != nil {
		return err
	}

	fmt.Printf("ok: load balancer %s active with target group, listener, and rule\n",
		loadBalancerName)
	return nil
}

func verifyDestroyed(ctx context.Context, client *elasticloadbalancingv2.Client) error {
	lb, err := findLoadBalancer(ctx, client)
	if err != nil {
		return err
	}
	if lb != nil {
		return fmt.Errorf("load balancer %s still exists", loadBalancerName)
	}
	tg, err := findTargetGroup(ctx, client)
	if err != nil {
		return err
	}
	if tg != nil {
		return fmt.Errorf("target group %s still exists", targetGroupName)
	}
	fmt.Printf("ok: load balancer %s and target group %s are gone\n",
		loadBalancerName, targetGroupName)
	return nil
}

// checkIdleTimeout confirms the load balancer has the idle-timeout attribute
// the scenario set, proving the follow-on attribute reconcile ran.
func checkIdleTimeout(
	ctx context.Context, client *elasticloadbalancingv2.Client, lbArn string,
) error {
	resp, err := client.DescribeLoadBalancerAttributes(ctx,
		&elasticloadbalancingv2.DescribeLoadBalancerAttributesInput{
			LoadBalancerArn: aws.String(lbArn),
		})
	if err != nil {
		return fmt.Errorf("describe load balancer attributes %s: %w", loadBalancerName, err)
	}
	for _, attr := range resp.Attributes {
		if aws.ToString(attr.Key) == idleTimeoutKey {
			if aws.ToString(attr.Value) != idleTimeoutValue {
				return fmt.Errorf("load balancer %s idle timeout is %q, want %q",
					loadBalancerName, aws.ToString(attr.Value), idleTimeoutValue)
			}
			return nil
		}
	}
	return fmt.Errorf("load balancer %s has no %s attribute", loadBalancerName, idleTimeoutKey)
}

// checkRule confirms the listener has the scenario's priority-100 rule.
func checkRule(
	ctx context.Context, client *elasticloadbalancingv2.Client, listenerArn string,
) error {
	resp, err := client.DescribeRules(ctx, &elasticloadbalancingv2.DescribeRulesInput{
		ListenerArn: aws.String(listenerArn),
	})
	if err != nil {
		return fmt.Errorf("describe rules for listener %s: %w", listenerArn, err)
	}
	for _, rule := range resp.Rules {
		if aws.ToString(rule.Priority) == rulePriority {
			return nil
		}
	}
	return fmt.Errorf("listener %s has no rule with priority %s", listenerArn, rulePriority)
}

// findLoadBalancer returns the scenario's load balancer, or nil when no load
// balancer by that name exists. ELBv2 reports an unknown name as the typed
// not-found exception, which findLoadBalancer turns into a nil result so the
// caller can tell "gone" apart from a real error.
func findLoadBalancer(
	ctx context.Context, client *elasticloadbalancingv2.Client,
) (*elbv2types.LoadBalancer, error) {
	resp, err := client.DescribeLoadBalancers(ctx,
		&elasticloadbalancingv2.DescribeLoadBalancersInput{
			Names: []string{loadBalancerName},
		})
	if err != nil {
		var notFound *elbv2types.LoadBalancerNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe load balancer %s: %w", loadBalancerName, err)
	}
	if len(resp.LoadBalancers) == 0 {
		return nil, nil
	}
	return &resp.LoadBalancers[0], nil
}

// findTargetGroup returns the scenario's target group, or nil when none by that
// name exists.
func findTargetGroup(
	ctx context.Context, client *elasticloadbalancingv2.Client,
) (*elbv2types.TargetGroup, error) {
	resp, err := client.DescribeTargetGroups(ctx,
		&elasticloadbalancingv2.DescribeTargetGroupsInput{
			Names: []string{targetGroupName},
		})
	if err != nil {
		var notFound *elbv2types.TargetGroupNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("describe target group %s: %w", targetGroupName, err)
	}
	if len(resp.TargetGroups) == 0 {
		return nil, nil
	}
	return &resp.TargetGroups[0], nil
}

// findListener returns the first listener on the load balancer, or nil when it
// has none.
func findListener(
	ctx context.Context, client *elasticloadbalancingv2.Client, lbArn string,
) (*elbv2types.Listener, error) {
	resp, err := client.DescribeListeners(ctx,
		&elasticloadbalancingv2.DescribeListenersInput{
			LoadBalancerArn: aws.String(lbArn),
		})
	if err != nil {
		return nil, fmt.Errorf("describe listeners for %s: %w", loadBalancerName, err)
	}
	if len(resp.Listeners) == 0 {
		return nil, nil
	}
	return &resp.Listeners[0], nil
}
