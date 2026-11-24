package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/state/local"
)

type identity struct {
	ID  string `json:"id"`
	ARN string `json:"arn"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := cloudwatchlogs.NewFromConfig(cfg)
	build, phase := os.Getenv("VERIFY_BUILD_DIR"), os.Getenv("VERIFY_PHASE")
	path := filepath.Join(build, "query-identity.json")
	var saved identity
	if phase != "applied" {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, &saved); err != nil {
			return err
		}
	}
	if phase == "destroyed" {
		entry, err := lookup(ctx, client, saved.ID)
		if err != nil {
			return err
		}
		if entry != nil {
			return fmt.Errorf("query definition %s remains", saved.ID)
		}
		return nil
	}
	if phase != "applied" && phase != "updated" {
		return fmt.Errorf("invalid phase %q", phase)
	}
	store, err := local.NewStore(filepath.Join(build, ".unobin", "state"),
		"cloudwatchlogs-query-definition", "config", encrypters.Noop{})
	if err != nil {
		return err
	}
	snapshot, err := store.Current()
	if err != nil {
		return err
	}
	managed := snapshot.Find("resource.query")
	if managed == nil {
		return fmt.Errorf("query definition has no managed state")
	}
	current := identity{ID: managed.Outputs["query-definition-id"].(string),
		ARN: managed.Outputs["arn"].(string)}
	if current.ID == "" || !strings.HasSuffix(current.ARN, ":query-definition:"+current.ID) {
		return fmt.Errorf("invalid query identity: %+v", current)
	}
	if phase == "updated" && current != saved {
		return fmt.Errorf("query identity changed")
	}
	entry, err := lookup(ctx, client, current.ID)
	if err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("query definition %s is absent", current.ID)
	}
	name, query := "unobin-query-definition-it", "fields @timestamp, @message | limit 5"
	groups := []string{"unobin-query-definition-group-it"}
	if phase == "updated" {
		name, query, groups = "unobin-query-definition-renamed-it", "fields @message | limit 10", nil
	}
	if aws.ToString(entry.Name) != name || aws.ToString(entry.QueryString) != query ||
		!slices.Equal(entry.LogGroupNames, groups) {
		return fmt.Errorf("unexpected remote query definition: %+v", entry)
	}
	if managed.Outputs["name"] != name || managed.Outputs["query-string"] != query {
		return fmt.Errorf("managed query fields differ from remote fields")
	}
	actual := []string{}
	for _, group := range managed.Outputs["log-group-names"].([]any) {
		actual = append(actual, group.(string))
	}
	if !slices.Equal(actual, groups) {
		return fmt.Errorf("managed groups are %v", actual)
	}
	if phase == "updated" {
		if err := verifyUnchanged(ctx, build); err != nil {
			return err
		}
	}
	body, err := json.Marshal(current)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

func verifyUnchanged(ctx context.Context, build string) error {
	cmd := exec.CommandContext(ctx, filepath.Join(build, "cloudwatchlogs-query-definition"),
		"plan", "-c", "update/config.ub", "-o", "query-unchanged.ubp")
	cmd.Dir = build
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("plan unchanged query: %s: %w", output, err)
	}
	body, err := os.ReadFile(filepath.Join(build, "query-unchanged.ubp"))
	if err != nil {
		return err
	}
	return verifyUnchangedPlan(body)
}

func verifyUnchangedPlan(body []byte) error {
	plan, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	if err != nil {
		return err
	}
	resources := map[string]bool{"resource.group": false, "resource.query": false}
	for _, step := range plan.Steps {
		if step.Kind == runtime.NodeResource {
			seen, expected := resources[step.Address]
			if !expected {
				return fmt.Errorf("unexpected resource %s in unchanged plan", step.Address)
			}
			if seen {
				return fmt.Errorf("duplicate %s in unchanged plan", step.Address)
			}
			resources[step.Address] = true
		} else if (step.Kind == runtime.NodeLibraryConfig || step.Kind == runtime.NodeOutput) &&
			step.Decision == runtime.DecisionEval {
			continue
		}
		if step.Decision != runtime.DecisionNoOp {
			return fmt.Errorf("unchanged %s planned %s", step.Address, step.Decision)
		}
	}
	for _, address := range []string{"resource.group", "resource.query"} {
		if !resources[address] {
			return fmt.Errorf("unchanged plan has no %s", address)
		}
	}
	return nil
}

func lookup(ctx context.Context, client *cloudwatchlogs.Client, id string) (*types.QueryDefinition,
	error,
) {
	input := &cloudwatchlogs.DescribeQueryDefinitionsInput{}
	for {
		page, err := client.DescribeQueryDefinitions(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, entry := range page.QueryDefinitions {
			if aws.ToString(entry.QueryDefinitionId) == id {
				return &entry, nil
			}
		}
		if aws.ToString(page.NextToken) == "" {
			return nil, nil
		}
		input.NextToken = page.NextToken
	}
}
