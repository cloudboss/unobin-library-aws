package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

const ruleGroupName = "unobin-it-wafv2-rule-group"

type ruleGroupIdentity struct {
	ID  string
	ARN string
}

func verifyRuleGroup(ctx context.Context, client *wafv2.Client, buildDir, mode string) error {
	path := filepath.Join(buildDir, "rule-group-identity.json")
	identity := ruleGroupIdentity{}
	if mode == "applied" {
		list, err := client.ListRuleGroups(ctx, &wafv2.ListRuleGroupsInput{
			Scope: types.ScopeRegional,
		})
		if err != nil {
			return fmt.Errorf("list rule groups: %w", err)
		}
		for _, group := range list.RuleGroups {
			if aws.ToString(group.Name) == ruleGroupName {
				identity = ruleGroupIdentity{aws.ToString(group.Id), aws.ToString(group.ARN)}
				break
			}
		}
	} else {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read rule group identity: %w", err)
		}
		if err := json.Unmarshal(data, &identity); err != nil {
			return fmt.Errorf("decode rule group identity: %w", err)
		}
	}
	if identity.ID == "" || identity.ARN == "" {
		return fmt.Errorf("missing identity for rule group %s", ruleGroupName)
	}
	if err := verifyRuleGroupState(ctx, client, identity, mode); err != nil {
		return err
	}
	if mode == "applied" {
		data, err := json.Marshal(identity)
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o600)
	}
	return nil
}

func verifyRuleGroupState(
	ctx context.Context, client *wafv2.Client, identity ruleGroupIdentity, mode string,
) error {
	output, err := client.GetRuleGroup(ctx, &wafv2.GetRuleGroupInput{
		Id: aws.String(identity.ID), Name: aws.String(ruleGroupName), Scope: types.ScopeRegional,
	})
	if mode == "destroyed" {
		var absent *types.WAFNonexistentItemException
		if errors.As(err, &absent) {
			return nil
		}
		return fmt.Errorf("expected rule group %s to be destroyed, get returned %v",
			ruleGroupName, err)
	}
	if err != nil {
		return fmt.Errorf("get rule group %s: %w", ruleGroupName, err)
	}
	group := output.RuleGroup
	if group == nil || aws.ToString(group.Id) != identity.ID ||
		aws.ToString(group.ARN) != identity.ARN || aws.ToString(group.Name) != ruleGroupName ||
		aws.ToInt64(group.Capacity) != 50 || aws.ToString(output.LockToken) == "" {
		return fmt.Errorf("unexpected rule group identity or capacity: %+v", output)
	}
	wantMetric := "unobin-it-group"
	wantTags := map[string]string{"unobin": "wafv2-it", "change": "old", "keep": "1", "remove": "yes"}
	if mode == "updated" {
		wantMetric = "unobin-it-group-updated"
		wantTags = map[string]string{"unobin": "wafv2-it", "add": "yes", "change": "new", "keep": "1"}
		if aws.ToString(group.Description) != "" ||
			len(group.Rules) != 0 || len(group.CustomResponseBodies) != 0 {
			return fmt.Errorf("rule group fields remain after clearing: %+v", group)
		}
	} else if err := verifyInitialRuleGroup(group); err != nil {
		return err
	}
	wantVisibility := &types.VisibilityConfig{MetricName: aws.String(wantMetric)}
	if !reflect.DeepEqual(group.VisibilityConfig, wantVisibility) {
		return fmt.Errorf("rule group visibility %+v, want %+v", group.VisibilityConfig,
			wantVisibility)
	}
	tags, err := listTags(ctx, client, identity.ARN)
	if err != nil {
		return fmt.Errorf("read rule group tags: %w", err)
	}
	if !maps.Equal(tags, wantTags) {
		return fmt.Errorf("rule group tags %v, want %v", tags, wantTags)
	}
	return nil
}

func verifyInitialRuleGroup(group *types.RuleGroup) error {
	wantBodies := map[string]types.CustomResponseBody{
		"blocked": {
			Content: aws.String("blocked by rule group"), ContentType: types.ResponseContentTypeTextPlain,
		},
	}
	if aws.ToString(group.Description) != "initial" ||
		!reflect.DeepEqual(group.CustomResponseBodies, wantBodies) {
		return fmt.Errorf("unexpected rule group description or response bodies: %+v", group)
	}
	want := []types.Rule{{
		Name: aws.String("rule-group-byte"),
		Action: &types.RuleAction{Block: &types.BlockAction{CustomResponse: &types.CustomResponse{
			ResponseCode: aws.Int32(403), CustomResponseBodyKey: aws.String("blocked"),
		}}},
		Statement: &types.Statement{NotStatement: &types.NotStatement{
			Statement: &types.Statement{ByteMatchStatement: &types.ByteMatchStatement{
				FieldToMatch:         &types.FieldToMatch{UriPath: &types.UriPath{}},
				PositionalConstraint: types.PositionalConstraintExactly,
				SearchString:         []byte("/initial"),
				TextTransformations:  []types.TextTransformation{{Type: types.TextTransformationTypeNone}},
			}},
		}},
		VisibilityConfig: &types.VisibilityConfig{
			MetricName: aws.String("unobin-it-group-rule"), SampledRequestsEnabled: true,
		},
	}}
	for i := range group.Rules {
		captcha := group.Rules[i].CaptchaConfig
		if captcha != nil && captcha.ImmunityTimeProperty == nil {
			group.Rules[i].CaptchaConfig = nil
		}
	}
	if !reflect.DeepEqual(group.Rules, want) {
		return fmt.Errorf("rule group rules %+v, want %+v", group.Rules, want)
	}
	return nil
}
