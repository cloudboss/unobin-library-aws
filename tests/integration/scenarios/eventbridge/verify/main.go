// verify checks the EventBridge group the scenario applied against the phase
// named in the VERIFY_PHASE environment variable. It looks the rule up by its
// stable name because the driver passes no plan outputs into verify, and it
// reads only cloud state: applied requires the rule to be present and the
// target to be attached to it; destroyed requires the rule to be gone, which
// takes its target with it. Tearing the group down is the destroy plan's job,
// not the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

const (
	ruleName = "unobin-it-rule"
	targetID = "unobin-it-target"
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
	client := eventbridge.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *eventbridge.Client) error {
	resp, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{
		Name: aws.String(ruleName),
	})
	if err != nil {
		return fmt.Errorf("describe rule %s: %w", ruleName, err)
	}
	if aws.ToString(resp.Arn) == "" {
		return fmt.Errorf("rule %s has no arn", ruleName)
	}

	hasTarget, err := ruleHasTarget(ctx, client)
	if err != nil {
		return err
	}
	if !hasTarget {
		return fmt.Errorf("rule %s has no target %s", ruleName, targetID)
	}
	fmt.Printf("ok: rule %s exists and has target %s\n", ruleName, targetID)
	return nil
}

func verifyDestroyed(ctx context.Context, client *eventbridge.Client) error {
	_, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{
		Name: aws.String(ruleName),
	})
	if err == nil {
		return fmt.Errorf("rule %s still exists", ruleName)
	}
	if !isNotFound(err) {
		return fmt.Errorf("describe rule %s: %w", ruleName, err)
	}
	fmt.Printf("ok: rule %s is gone\n", ruleName)
	return nil
}

// ruleHasTarget reports whether the scenario's target is attached to the rule.
func ruleHasTarget(ctx context.Context, client *eventbridge.Client) (bool, error) {
	resp, err := client.ListTargetsByRule(ctx, &eventbridge.ListTargetsByRuleInput{
		Rule: aws.String(ruleName),
	})
	if err != nil {
		return false, fmt.Errorf("list targets for rule %s: %w", ruleName, err)
	}
	for i := range resp.Targets {
		if aws.ToString(resp.Targets[i].Id) == targetID {
			return true, nil
		}
	}
	return false, nil
}

func isNotFound(err error) bool {
	var notFound *eventbridgetypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
