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
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/state/local"
)

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
	client := ec2.NewFromConfig(cfg)
	build, phase := os.Getenv("VERIFY_BUILD_DIR"), os.Getenv("VERIFY_PHASE")
	path := filepath.Join(build, "prefix-ids.json")
	ids := map[string]string{}
	if phase == "destroyed" {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, &ids); err != nil {
			return err
		}
		for _, id := range ids {
			response, err := client.DescribeManagedPrefixLists(ctx,
				&ec2.DescribeManagedPrefixListsInput{PrefixListIds: []string{id}})
			var api smithy.APIError
			if errors.As(err, &api) && api.ErrorCode() == "InvalidPrefixListID.NotFound" {
				continue
			}
			if err != nil {
				return err
			}
			if len(response.PrefixLists) != 0 {
				return fmt.Errorf("prefix list %s remains", id)
			}
		}
		return nil
	}
	if phase != "applied" && phase != "updated" {
		return fmt.Errorf("unknown phase %s", phase)
	}
	store, err := local.NewStore(filepath.Join(build, ".unobin", "state"),
		"ec2-managed-prefix-list", "config", encrypters.Noop{})
	if err != nil {
		return err
	}
	snapshot, err := store.Current()
	if err != nil {
		return err
	}
	for _, role := range []string{"managed", "cleared", "unmanaged"} {
		saved := snapshot.Find("resource." + role)
		if saved == nil {
			return fmt.Errorf("%s has no managed ownership", role)
		}
		id, ok := saved.Outputs["prefix-list-id"].(string)
		if !ok || id == "" {
			return fmt.Errorf("%s has no saved identity", role)
		}
		ids[role] = id
	}
	body, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return err
	}
	verify := func() error {
		for _, role := range []string{"managed", "cleared", "unmanaged"} {
			saved := snapshot.Find("resource." + role)
			response, err := client.DescribeManagedPrefixLists(ctx,
				&ec2.DescribeManagedPrefixListsInput{PrefixListIds: []string{ids[role]}})
			if err != nil {
				return err
			}
			if len(response.PrefixLists) != 1 {
				return fmt.Errorf("missing %s list", role)
			}
			list := response.PrefixLists[0]
			wanted, name, capacity := desired(role, phase)
			tags := map[string]string{"unobin": "ec2-managed-prefix-list-it",
				"role": role, "stage": "initial"}
			if role == "managed" {
				tags["blank"] = ""
			}
			if phase == "updated" {
				tags["stage"] = "updated"
			}
			actualTags := map[string]string{}
			for _, tag := range list.Tags {
				actualTags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
			}
			if aws.ToString(list.PrefixListName) != name || aws.ToInt32(list.MaxEntries) != capacity ||
				aws.ToString(list.AddressFamily) != "IPv4" || aws.ToString(list.OwnerId) == "" ||
				aws.ToString(list.PrefixListArn) == "" || aws.ToInt64(list.Version) < 1 ||
				!maps.Equal(tags, actualTags) {
				return fmt.Errorf("unexpected %s metadata: %+v", role, list)
			}
			entries := map[string]string{}
			pages := ec2.NewGetManagedPrefixListEntriesPaginator(client,
				&ec2.GetManagedPrefixListEntriesInput{PrefixListId: aws.String(ids[role])})
			for pages.HasMorePages() {
				page, err := pages.NextPage(ctx)
				if err != nil {
					return err
				}
				for _, entry := range page.Entries {
					entries[aws.ToString(entry.Cidr)] = aws.ToString(entry.Description)
				}
			}
			if !maps.Equal(wanted, entries) {
				return fmt.Errorf("%s entries: %v; want %v", role, entries, wanted)
			}
			for key, value := range map[string]any{"prefix-list-id": ids[role], "name": name,
				"address-family": "IPv4", "max-entries": int64(capacity), "version": aws.ToInt64(list.Version),
				"arn": aws.ToString(list.PrefixListArn), "owner-id": aws.ToString(list.OwnerId),
				"region": cfg.Region,
			} {
				if !reflect.DeepEqual(saved.Outputs[key], value) {
					return fmt.Errorf("%s output %s mismatch", role, key)
				}
			}
			outputEntries := map[string]string{}
			for _, entry := range saved.Outputs["entries"].([]any) {
				fields := entry.(map[string]any)
				description, _ := fields["description"].(string)
				outputEntries[fields["cidr"].(string)] = description
			}
			if !maps.Equal(outputEntries, wanted) {
				return fmt.Errorf("%s saved entries differ", role)
			}
			outputTags := map[string]string{}
			for key, value := range saved.Outputs["tags"].(map[string]any) {
				outputTags[key] = value.(string)
			}
			if !maps.Equal(outputTags, tags) {
				return fmt.Errorf("%s saved tags differ", role)
			}
		}
		return nil
	}
	if err := verify(); err != nil {
		return err
	}
	if phase == "updated" {
		if err := unchanged(ctx, build); err != nil {
			return err
		}
		_, err = client.ModifyManagedPrefixList(ctx, &ec2.ModifyManagedPrefixListInput{
			PrefixListId: aws.String(ids["managed"]), PrefixListName: aws.String("remote-drift"),
		})
		if err != nil {
			return err
		}
		if err := reapply(func(args ...string) error { return runCLI(ctx, build, args...) }); err != nil {
			return err
		}
		snapshot, err = store.Current()
		if err != nil {
			return err
		}
		if err := verify(); err != nil {
			return err
		}
		return unchanged(ctx, build)
	}
	return nil
}

func desired(role, phase string) (map[string]string, string, int32) {
	if role == "managed" {
		if phase == "updated" {
			return map[string]string{"10.0.0.0/8": "updated",
				"198.51.100.0/24": "new"}, "unobin-prefix-renamed-it", 4
		}
		return map[string]string{"10.0.0.0/8": "initial", "192.0.2.0/24": ""}, "unobin-prefix-it", 2
	}
	if role == "cleared" {
		if phase == "updated" {
			return map[string]string{}, "unobin-prefix-cleared-it", 1
		}
		return map[string]string{"192.0.2.0/24": "remove"}, "unobin-prefix-cleared-it", 4
	}
	return map[string]string{"203.0.113.0/24": "retained"}, "unobin-prefix-unmanaged-it", 2
}

func runCLI(ctx context.Context, build string, args ...string) error {
	cmd := exec.CommandContext(ctx, filepath.Join(build, "ec2-managed-prefix-list"), args...)
	cmd.Dir = build
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("prefix CLI: %s: %w", output, err)
	}
	return nil
}

func unchanged(ctx context.Context, build string) error {
	if err := runCLI(ctx, build, "plan", "-c", "update/config.ub", "-o",
		"prefix-unchanged.ubp"); err != nil {
		return err
	}
	body, err := os.ReadFile(filepath.Join(build, "prefix-unchanged.ubp"))
	if err != nil {
		return err
	}
	return unchangedPlan(body)
}

func unchangedPlan(body []byte) error {
	plan, err := runtime.OpenPlan(body, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	if err != nil {
		return err
	}
	expected := map[string]bool{"resource.managed": false, "resource.cleared": false,
		"resource.unmanaged": false}
	for _, step := range plan.Steps {
		if step.Kind == runtime.NodeResource {
			seen, ok := expected[step.Address]
			if !ok || seen {
				return fmt.Errorf("unexpected resource %s", step.Address)
			}
			expected[step.Address] = true
		} else if (step.Kind == runtime.NodeLibraryConfig || step.Kind == runtime.NodeOutput) &&
			step.Decision == runtime.DecisionEval {
			continue
		}
		if step.Decision != runtime.DecisionNoOp {
			return fmt.Errorf("%s planned %s", step.Address, step.Decision)
		}
	}
	for address, seen := range expected {
		if !seen {
			return fmt.Errorf("missing resource %s", address)
		}
	}
	return nil
}

func reapply(run func(...string) error) error {
	if err := run("plan", "-c", "update/config.ub", "-o", "prefix-repair.ubp"); err != nil {
		return err
	}
	return run("apply", "prefix-repair.ubp")
}
