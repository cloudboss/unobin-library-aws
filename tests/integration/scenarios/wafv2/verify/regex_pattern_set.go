package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

const (
	regexClearName = "unobin-it-wafv2-regex-clear"
	regexEditName  = "unobin-it-wafv2-regex-edit"
)

func regexExpected(name, mode string) (string, []string, map[string]string) {
	if mode == "updated" && name == regexClearName {
		return "", nil, map[string]string{}
	}
	if mode == "updated" {
		return "updated", []string{"/updated"}, map[string]string{
			"unobin": "wafv2-it", "add": "yes", "change": "new", "keep": "1",
		}
	}
	patterns := []string{"/initial"}
	if name == regexClearName {
		patterns = []string{"^initial", "example$"}
	}
	return "initial", patterns, map[string]string{
		"unobin": "wafv2-it", "change": "old", "keep": "1", "remove": "yes",
	}
}

func verifyRegexPatternSets(
	ctx context.Context, client *wafv2.Client, buildDir, mode string,
) error {
	path := filepath.Join(buildDir, "regex-pattern-set-identities.json")
	identities := map[string]ipSetIdentity{}
	if mode == "applied" {
		output, err := client.ListRegexPatternSets(ctx, &wafv2.ListRegexPatternSetsInput{
			Scope: types.ScopeRegional,
		})
		if err != nil {
			return fmt.Errorf("list regex pattern sets: %w", err)
		}
		for _, set := range output.RegexPatternSets {
			name := aws.ToString(set.Name)
			if name == regexClearName || name == regexEditName {
				identities[name] = ipSetIdentity{aws.ToString(set.Id), aws.ToString(set.ARN)}
			}
		}
	} else {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read regex pattern set identities: %w", err)
		}
		if err := json.Unmarshal(data, &identities); err != nil {
			return fmt.Errorf("decode regex pattern set identities: %w", err)
		}
	}
	for _, name := range []string{regexClearName, regexEditName} {
		identity := identities[name]
		if identity.ID == "" || identity.ARN == "" {
			return fmt.Errorf("missing identity for regex pattern set %s", name)
		}
		output, err := client.GetRegexPatternSet(ctx, &wafv2.GetRegexPatternSetInput{
			Id: aws.String(identity.ID), Name: aws.String(name), Scope: types.ScopeRegional,
		})
		if mode == "destroyed" {
			var absent *types.WAFNonexistentItemException
			if errors.As(err, &absent) {
				continue
			}
			return fmt.Errorf("expected regex pattern set %s to be destroyed, got %v", name, err)
		}
		if err != nil {
			return fmt.Errorf("get regex pattern set %s: %w", name, err)
		}
		set := output.RegexPatternSet
		if set == nil || aws.ToString(set.Id) != identity.ID ||
			aws.ToString(set.ARN) != identity.ARN || aws.ToString(set.Name) != name ||
			aws.ToString(output.LockToken) == "" {
			return fmt.Errorf("unexpected regex pattern set %s identity or token: %+v", name, output)
		}
		description, patterns, tags := regexExpected(name, mode)
		actual := make([]string, 0, len(set.RegularExpressionList))
		for _, pattern := range set.RegularExpressionList {
			actual = append(actual, aws.ToString(pattern.RegexString))
		}
		slices.Sort(actual)
		slices.Sort(patterns)
		if aws.ToString(set.Description) != description || !slices.Equal(actual, patterns) {
			return fmt.Errorf("regex pattern set %s description %q and patterns %v, want %q and %v",
				name, aws.ToString(set.Description), actual, description, patterns)
		}
		observed, err := listTags(ctx, client, identity.ARN)
		if err != nil {
			return err
		}
		if !maps.Equal(observed, tags) {
			return fmt.Errorf("regex pattern set %s tags %v, want %v", name, observed, tags)
		}
	}
	if mode == "applied" {
		data, err := json.Marshal(identities)
		if err != nil {
			return fmt.Errorf("encode regex pattern set identities: %w", err)
		}
		return os.WriteFile(path, data, 0o600)
	}
	return nil
}
