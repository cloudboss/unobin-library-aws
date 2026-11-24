package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

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
	out, err := ecr.NewFromConfig(cfg).DescribeRegistry(ctx, &ecr.DescribeRegistryInput{})
	if err != nil {
		return fmt.Errorf("describe registry: %w", err)
	}
	if phase == "destroyed" {
		if out != nil && out.ReplicationConfiguration != nil &&
			len(out.ReplicationConfiguration.Rules) > 0 {
			return fmt.Errorf("replication rules remain: %+v", out.ReplicationConfiguration.Rules)
		}
		return nil
	}
	region, account := "us-west-2", "000000000001"
	filters := []types.RepositoryFilter{
		{Filter: aws.String("unobin-initial"), FilterType: "PREFIX_MATCH"},
	}
	if phase == "updated" {
		region, account, filters = "eu-west-1", "000000000002", nil
	} else if phase != "applied" {
		return fmt.Errorf("unknown phase %q", phase)
	}
	want := &types.ReplicationConfiguration{Rules: []types.ReplicationRule{{
		Destinations: []types.ReplicationDestination{
			{Region: aws.String(region), RegistryId: aws.String(account)},
		},
		RepositoryFilters: filters,
	}}}
	if out != nil && out.ReplicationConfiguration != nil {
		for i := range out.ReplicationConfiguration.Rules {
			if len(out.ReplicationConfiguration.Rules[i].RepositoryFilters) == 0 {
				out.ReplicationConfiguration.Rules[i].RepositoryFilters = nil
			}
		}
	}
	if out == nil || aws.ToString(out.RegistryId) == "" ||
		!reflect.DeepEqual(want, out.ReplicationConfiguration) {
		return fmt.Errorf("unexpected replication configuration: %+v", out)
	}
	return verifyNoOp(ctx, phase)
}

func verifyNoOp(ctx context.Context, phase string) error {
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	if buildDir == "" {
		return fmt.Errorf("VERIFY_BUILD_DIR is required")
	}
	configPath := "config.ub"
	if phase == "updated" {
		configPath = "update/config.ub"
	}
	command := exec.CommandContext(ctx, filepath.Join(buildDir, "ecr-replication-configuration"),
		"plan", "-c", configPath, "-o", "noop.json")
	command.Dir = buildDir
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("plan unchanged configuration: %w\n%s", err, output)
	}
	return checkNoOpPlan(filepath.Join(buildDir, "noop.json"))
}

func checkNoOpPlan(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read unchanged plan: %w", err)
	}
	plan, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	if err != nil {
		return fmt.Errorf("open unchanged plan: %w", err)
	}
	found := false
	for _, step := range plan.Steps {
		if step.Kind != runtime.NodeResource {
			continue
		}
		if step.Address != "resource.replication" {
			return fmt.Errorf("unexpected resource %q in unchanged plan", step.Address)
		}
		if found {
			return fmt.Errorf("duplicate resource.replication in unchanged plan")
		}
		found = true
		if step.Decision != runtime.DecisionNoOp {
			return fmt.Errorf("resource.replication decision = %s, want no-op", step.Decision)
		}
	}
	if !found {
		return fmt.Errorf("missing resource.replication in unchanged plan")
	}
	return nil
}
