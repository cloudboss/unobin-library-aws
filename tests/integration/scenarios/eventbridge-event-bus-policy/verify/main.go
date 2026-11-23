package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/cloudboss/unobin/pkg/encrypters/local"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

const busName = "unobin-it-event-bus-policy"

func main() {
	if err := verify(context.Background(), os.Getenv("VERIFY_PHASE")); err != nil {
		log.Fatal(err)
	}
}

func verify(ctx context.Context, phase string) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := eventbridge.NewFromConfig(cfg)
	out, err := client.DescribeEventBus(ctx, &eventbridge.DescribeEventBusInput{
		Name: aws.String(busName),
	})
	if phase == "destroyed" {
		if _, missing := errors.AsType[*eventtypes.ResourceNotFoundException](err); missing {
			return nil
		}
		if err != nil {
			return fmt.Errorf("describe destroyed event bus: %w", err)
		}
		return fmt.Errorf("event bus still exists after managed destruction")
	}
	if err != nil {
		return fmt.Errorf("describe managed event bus: %w", err)
	}

	expected := "InitialPermission"
	if phase == "updated" {
		expected = "UpdatedPermission"
	} else if phase != "applied" {
		return fmt.Errorf("unknown verification phase %q", phase)
	}
	var policy struct {
		Version   string
		Statement []struct {
			Sid       string
			Effect    string
			Action    string
			Resource  string
			Principal map[string]string
		}
	}
	if err := json.Unmarshal([]byte(aws.ToString(out.Policy)), &policy); err != nil {
		return err
	}
	if policy.Version != "2012-10-17" || len(policy.Statement) != 1 {
		return fmt.Errorf("unexpected policy document: %s", aws.ToString(out.Policy))
	}
	identity, err := arn.Parse(aws.ToString(out.Arn))
	if err != nil {
		return err
	}
	root := "arn:" + identity.Partition + ":iam::" + identity.AccountID + ":root"
	statement := policy.Statement[0]
	principalMatches := maps.Equal(
		statement.Principal, map[string]string{"AWS": identity.AccountID},
	) ||
		maps.Equal(statement.Principal, map[string]string{"AWS": root})
	if statement.Sid != expected || statement.Effect != "Allow" ||
		statement.Action != "events:PutEvents" ||
		statement.Resource != aws.ToString(out.Arn) || !principalMatches {
		return fmt.Errorf("unexpected policy statement: %+v", statement)
	}
	if phase == "updated" {
		if err := verifyUnchanged(ctx); err != nil {
			return err
		}
		_, err = client.PutPermission(ctx, &eventbridge.PutPermissionInput{
			EventBusName: aws.String(busName), StatementId: aws.String("ExternalPermission"),
			Action: aws.String("events:PutEvents"), Principal: aws.String("123456789012"),
		})
		if err != nil {
			return fmt.Errorf("add external permission before full policy deletion: %w", err)
		}
		return removePolicy(ctx, client)
	}
	return nil
}

func verifyUnchanged(ctx context.Context) error {
	dir := os.Getenv("VERIFY_BUILD_DIR")
	planPath := filepath.Join(dir, "verify-unchanged.json")
	command := exec.CommandContext(ctx, filepath.Join(dir, "eventbridge-event-bus-policy"),
		"plan", "-c", "update/config.ub", "-o", planPath)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("plan unchanged policy: %w: %s", err, output)
	}
	contents, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	return verifyUnchangedPlan(contents)
}

func verifyUnchangedPlan(contents []byte) error {
	plan, err := runtime.OpenPlan(contents, func(ref *runtime.StateRef) (encrypt.Encrypter, error) {
		if ref != nil && ref.Name != local.NoopName {
			return nil, fmt.Errorf("unsupported plan encrypter %s", ref.Name)
		}
		return local.Noop{}, nil
	})
	if err != nil {
		return err
	}
	for _, step := range plan.Steps {
		if step.Address == "resource.policy['managed']" {
			if step.Decision != runtime.DecisionNoOp {
				return fmt.Errorf("unchanged policy decision: %s", step.Decision)
			}
			return nil
		}
	}
	return fmt.Errorf("unchanged plan has no policy resource")
}

func removePolicy(ctx context.Context, client *eventbridge.Client) error {
	dir := os.Getenv("VERIFY_BUILD_DIR")
	configPath := filepath.Join(dir, "policy-removed", "config.ub")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	source := filepath.Join("tests", "integration", "scenarios", "eventbridge-event-bus-policy",
		"config-policy-removed.ub")
	contents, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		return err
	}
	if err := scenarioCommand(ctx, "pin", "-c", configPath); err != nil {
		return err
	}
	planPath := filepath.Join(dir, "policy-removed.json")
	if err := scenarioCommand(ctx, "plan", "-c", configPath, "-o", planPath); err != nil {
		return err
	}
	if err := scenarioCommand(ctx, "apply", planPath); err != nil {
		return err
	}
	out, err := client.DescribeEventBus(ctx, &eventbridge.DescribeEventBusInput{
		Name: aws.String(busName),
	})
	if err != nil {
		return fmt.Errorf("describe bus after policy destruction: %w", err)
	}
	if aws.ToString(out.Name) != busName || aws.ToString(out.Policy) != "" {
		return fmt.Errorf("bus must remain present with no policy after policy deletion: %+v", out)
	}
	return nil
}

func scenarioCommand(ctx context.Context, args ...string) error {
	dir := os.Getenv("VERIFY_BUILD_DIR")
	command := exec.CommandContext(ctx, filepath.Join(dir, "eventbridge-event-bus-policy"), args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("scenario command %v: %w: %s", args, err, output)
	}
	return nil
}
