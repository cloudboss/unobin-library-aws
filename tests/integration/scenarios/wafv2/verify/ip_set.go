package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type ipSetIdentity struct {
	ID  string
	ARN string
}

func verifyIPSets(ctx context.Context, client *wafv2.Client, buildDir, mode string) error {
	names := []string{"unobin-it-wafv2-ip-set-clear", "unobin-it-wafv2-ip-set-edit"}
	path := filepath.Join(buildDir, "ip-set-identities.json")
	identities := map[string]ipSetIdentity{}
	switch mode {
	case "applied":
		output, err := client.ListIPSets(ctx, &wafv2.ListIPSetsInput{Scope: types.ScopeRegional})
		if err != nil {
			return fmt.Errorf("list IP sets: %w", err)
		}
		for _, set := range output.IPSets {
			if name := aws.ToString(set.Name); slices.Contains(names, name) {
				identities[name] = ipSetIdentity{aws.ToString(set.Id), aws.ToString(set.ARN)}
			}
		}
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read IP set identities: %w", err)
		}
		if err := json.Unmarshal(data, &identities); err != nil {
			return fmt.Errorf("decode IP set identities: %w", err)
		}
	}

	for _, name := range names {
		identity := identities[name]
		if identity.ID == "" || identity.ARN == "" {
			return fmt.Errorf("missing identity for IP set %s", name)
		}
		if err := verifyIPSet(ctx, client, name, identity, mode); err != nil {
			return err
		}
	}
	switch mode {
	case "applied":
		data, err := json.Marshal(identities)
		if err != nil {
			return fmt.Errorf("encode IP set identities: %w", err)
		}
		return os.WriteFile(path, data, 0o600)
	case "updated":
		return verifyWAFNoOp(ctx, buildDir)
	default:
		return nil
	}
}

func verifyIPSet(
	ctx context.Context, client *wafv2.Client, name string, identity ipSetIdentity, mode string,
) error {
	output, err := client.GetIPSet(ctx, &wafv2.GetIPSetInput{
		Id: aws.String(identity.ID), Name: aws.String(name), Scope: types.ScopeRegional,
	})
	if mode == "destroyed" {
		var absent *types.WAFNonexistentItemException
		if errors.As(err, &absent) {
			return nil
		}
		return fmt.Errorf("expected IP set %s to be destroyed, get returned %v", name, err)
	}
	if err != nil {
		return fmt.Errorf("get IP set %s: %w", name, err)
	}
	set := output.IPSet
	if set == nil || aws.ToString(set.ARN) != identity.ARN || aws.ToString(set.Id) != identity.ID ||
		aws.ToString(set.Name) != name || set.IPAddressVersion != types.IPAddressVersionIpv4 ||
		aws.ToString(output.LockToken) == "" {
		return fmt.Errorf("unexpected IP set %s identity or version in %+v", name, output)
	}

	wantDescription := "initial"
	wantAddresses := []string{"192.0.2.0/24"}
	wantTags := map[string]string{"unobin": "wafv2-it", "change": "old", "keep": "1", "remove": "yes"}
	switch {
	case mode == "updated" && name == "unobin-it-wafv2-ip-set-clear":
		wantDescription = ""
		wantAddresses = nil
	case mode == "updated":
		wantDescription = "updated"
		wantAddresses = []string{"203.0.113.0/24"}
	case name == "unobin-it-wafv2-ip-set-clear":
		wantAddresses = append(wantAddresses, "198.51.100.0/24")
	}
	if mode == "updated" {
		wantTags = map[string]string{"unobin": "wafv2-it", "add": "yes", "change": "new", "keep": "1"}
	}
	slices.Sort(set.Addresses)
	if aws.ToString(set.Description) != wantDescription ||
		!slices.Equal(set.Addresses, wantAddresses) {
		return fmt.Errorf("IP set %s description %q and addresses %v, want %q and %v",
			name, aws.ToString(set.Description), set.Addresses, wantDescription, wantAddresses)
	}
	tags, err := listTags(ctx, client, identity.ARN)
	if err != nil {
		return fmt.Errorf("read tags for IP set %s: %w", name, err)
	}
	if !maps.Equal(tags, wantTags) {
		return fmt.Errorf("IP set %s tags %v, want %v", name, tags, wantTags)
	}
	return nil
}

func verifyWAFNoOp(ctx context.Context, buildDir string) error {
	command := exec.CommandContext(
		ctx, "./wafv2", "plan", "-c", "./update/config.ub", "--format", "json",
	)
	command.Dir = buildDir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	data, err := command.Output()
	if err != nil {
		return fmt.Errorf("plan after clearing WAF resources: %w: %s", err, &stderr)
	}
	var plan runtime.PlanFile
	if err := json.Unmarshal(data, &plan); err != nil {
		return fmt.Errorf("decode plan after clearing WAF resources: %w", err)
	}
	want := map[string]runtime.Decision{
		"resource.ip-set-clear": runtime.DecisionNoOp,
		"resource.ip-set-edit":  runtime.DecisionNoOp,
		"resource.rule-group":   runtime.DecisionNoOp,
	}
	got := map[string]runtime.Decision{}
	for _, step := range plan.Steps {
		if _, applies := want[step.Address]; applies {
			got[step.Address] = step.Decision
		}
	}
	if !maps.Equal(got, want) {
		return fmt.Errorf("WAF decisions %v, want %v", got, want)
	}
	return nil
}
