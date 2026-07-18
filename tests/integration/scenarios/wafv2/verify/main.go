package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

const (
	webACLName       = "unobin-it-wafv2"
	ruleName         = "uri-path-count"
	webACLMetricName = "unobin-it-wafv2"
	ruleMetricName   = "unobin-it-wafv2-rule"
	recordFileName   = "web-acl-identity.json"
)

type webACLIdentity struct {
	ID  string `json:"id"`
	ARN string `json:"arn"`
}

type verifierClient interface {
	GetWebACL(
		context.Context,
		*wafv2.GetWebACLInput,
		...func(*wafv2.Options),
	) (*wafv2.GetWebACLOutput, error)
	ListTagsForResource(
		context.Context,
		*wafv2.ListTagsForResourceInput,
		...func(*wafv2.Options),
	) (*wafv2.ListTagsForResourceOutput, error)
	ListWebACLs(
		context.Context,
		*wafv2.ListWebACLsInput,
		...func(*wafv2.Options),
	) (*wafv2.ListWebACLsOutput, error)
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	if buildDir == "" {
		return errors.New("VERIFY_BUILD_DIR must be set")
	}
	mode := os.Getenv("VERIFY_PHASE")
	if mode != "applied" && mode != "updated" && mode != "destroyed" {
		return fmt.Errorf(
			"VERIFY_PHASE must be applied, updated, or destroyed, got %q", mode,
		)
	}

	ctx := context.Background()
	configuration, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := wafv2.NewFromConfig(configuration)

	switch mode {
	case "applied":
		return verifyApplied(ctx, client, buildDir)
	case "updated":
		return verifyUpdated(ctx, client, buildDir)
	default:
		return verifyDestroyed(ctx, client, buildDir)
	}
}

func verifyApplied(ctx context.Context, client verifierClient, buildDir string) error {
	identity, err := findWebACL(ctx, client)
	if err != nil {
		return err
	}
	wantTags := map[string]string{
		"change": "old",
		"keep":   "1",
		"remove": "yes",
		"unobin": "wafv2-it",
	}
	if err := verifyPresent(ctx, client, identity, "initial", "/initial", wantTags); err != nil {
		return err
	}
	if err := writeIdentity(buildDir, identity); err != nil {
		return err
	}
	fmt.Printf("ok: web ACL %s matches the applied configuration\n", webACLName)
	return nil
}

func verifyUpdated(ctx context.Context, client verifierClient, buildDir string) error {
	wantIdentity, err := readIdentity(buildDir)
	if err != nil {
		return err
	}
	identity, err := findWebACL(ctx, client)
	if err != nil {
		return err
	}
	if identity != wantIdentity {
		return fmt.Errorf(
			"web ACL identity is id %q arn %q, want id %q arn %q",
			identity.ID, identity.ARN, wantIdentity.ID, wantIdentity.ARN,
		)
	}
	wantTags := map[string]string{
		"add":    "yes",
		"change": "new",
		"keep":   "1",
		"unobin": "wafv2-it",
	}
	if err := verifyPresent(ctx, client, identity, "updated", "/updated", wantTags); err != nil {
		return err
	}
	fmt.Printf("ok: web ACL %s matches the updated configuration\n", webACLName)
	return nil
}

func verifyDestroyed(ctx context.Context, client verifierClient, buildDir string) error {
	identity, err := readIdentity(buildDir)
	if err != nil {
		return err
	}
	_, err = client.GetWebACL(ctx, &wafv2.GetWebACLInput{
		Id:    aws.String(identity.ID),
		Name:  aws.String(webACLName),
		Scope: types.ScopeRegional,
	})
	if err == nil {
		return fmt.Errorf("web ACL %s still exists", webACLName)
	}
	var notFound *types.WAFNonexistentItemException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("get destroyed web ACL %s: %w", webACLName, err)
	}
	fmt.Printf("ok: web ACL %s is gone\n", webACLName)
	return nil
}

func findWebACL(ctx context.Context, client verifierClient) (webACLIdentity, error) {
	var marker *string
	for {
		output, err := client.ListWebACLs(ctx, &wafv2.ListWebACLsInput{
			NextMarker: marker,
			Scope:      types.ScopeRegional,
		})
		if err != nil {
			return webACLIdentity{}, fmt.Errorf("list REGIONAL web ACLs: %w", err)
		}
		for _, summary := range output.WebACLs {
			if aws.ToString(summary.Name) != webACLName {
				continue
			}
			identity := webACLIdentity{
				ID:  aws.ToString(summary.Id),
				ARN: aws.ToString(summary.ARN),
			}
			if identity.ID == "" || identity.ARN == "" {
				return webACLIdentity{}, fmt.Errorf(
					"web ACL %s list identity is incomplete", webACLName,
				)
			}
			return identity, nil
		}
		if output.NextMarker == nil || aws.ToString(output.NextMarker) == "" {
			break
		}
		marker = output.NextMarker
	}
	return webACLIdentity{}, fmt.Errorf(
		"web ACL %s was not found in REGIONAL scope", webACLName,
	)
}

func verifyPresent(
	ctx context.Context,
	client verifierClient,
	identity webACLIdentity,
	description string,
	searchString string,
	wantTags map[string]string,
) error {
	output, err := client.GetWebACL(ctx, &wafv2.GetWebACLInput{
		Id:    aws.String(identity.ID),
		Name:  aws.String(webACLName),
		Scope: types.ScopeRegional,
	})
	if err != nil {
		return fmt.Errorf("get web ACL %s: %w", webACLName, err)
	}
	if output.WebACL == nil {
		return fmt.Errorf("get web ACL %s returned no web ACL", webACLName)
	}
	webACL := output.WebACL
	if aws.ToString(webACL.Id) != identity.ID || aws.ToString(webACL.ARN) != identity.ARN {
		return fmt.Errorf("get web ACL %s returned a different identity", webACLName)
	}
	if aws.ToString(webACL.Name) != webACLName {
		return fmt.Errorf("web ACL name is %q, want %q", aws.ToString(webACL.Name), webACLName)
	}
	if aws.ToString(webACL.Description) != description {
		return fmt.Errorf(
			"web ACL description is %q, want %q",
			aws.ToString(webACL.Description), description,
		)
	}
	if output.LockToken == nil || aws.ToString(output.LockToken) == "" {
		return fmt.Errorf("web ACL %s has no lock token", webACLName)
	}
	labelNamespace := aws.ToString(webACL.LabelNamespace)
	if !strings.HasPrefix(labelNamespace, "awswaf:") ||
		!strings.HasSuffix(labelNamespace, ":webacl:"+webACLName+":") {
		return fmt.Errorf("web ACL label namespace is %q", labelNamespace)
	}
	if webACL.Capacity <= 0 {
		return fmt.Errorf("web ACL capacity is %d, want a positive value", webACL.Capacity)
	}
	if err := verifyDefaultAction(webACL.DefaultAction); err != nil {
		return err
	}
	if err := verifyVisibility(webACL.VisibilityConfig, webACLMetricName, "web ACL"); err != nil {
		return err
	}
	if len(webACL.Rules) != 1 {
		return fmt.Errorf("web ACL has %d rules, want 1", len(webACL.Rules))
	}
	if err := verifyRule(&webACL.Rules[0], searchString); err != nil {
		return err
	}
	gotTags, err := listTags(ctx, client, identity.ARN)
	if err != nil {
		return err
	}
	if !equalTags(gotTags, wantTags) {
		return fmt.Errorf("web ACL tags are %v, want %v", gotTags, wantTags)
	}
	return nil
}

func verifyDefaultAction(action *types.DefaultAction) error {
	if action == nil || action.Allow == nil {
		return errors.New("web ACL default action is not Allow")
	}
	if populatedMemberCount(action) != 1 {
		return errors.New("web ACL default action has unexpected members")
	}
	return nil
}

func verifyRule(rule *types.Rule, searchString string) error {
	if aws.ToString(rule.Name) != ruleName {
		return fmt.Errorf("rule name is %q, want %q", aws.ToString(rule.Name), ruleName)
	}
	if rule.Priority != 0 {
		return fmt.Errorf("rule priority is %d, want 0", rule.Priority)
	}
	if rule.OverrideAction != nil {
		return errors.New("rule has an unexpected override action")
	}
	if rule.Action == nil || rule.Action.Count == nil {
		return errors.New("rule action is not Count")
	}
	if populatedMemberCount(rule.Action) != 1 {
		return errors.New("rule action has unexpected members")
	}
	if rule.Statement == nil || rule.Statement.ByteMatchStatement == nil {
		return errors.New("rule statement is not ByteMatch")
	}
	if populatedMemberCount(rule.Statement) != 1 {
		return errors.New("rule statement has unexpected members")
	}
	if err := verifyByteMatch(rule.Statement.ByteMatchStatement, searchString); err != nil {
		return err
	}
	return verifyVisibility(rule.VisibilityConfig, ruleMetricName, "rule")
}

func verifyByteMatch(statement *types.ByteMatchStatement, searchString string) error {
	if statement.FieldToMatch == nil || statement.FieldToMatch.UriPath == nil {
		return errors.New("ByteMatch field is not URIPath")
	}
	if populatedMemberCount(statement.FieldToMatch) != 1 {
		return errors.New("ByteMatch field has unexpected members")
	}
	if statement.PositionalConstraint != types.PositionalConstraintExactly {
		return fmt.Errorf(
			"ByteMatch positional constraint is %q, want EXACTLY",
			statement.PositionalConstraint,
		)
	}
	if string(statement.SearchString) != searchString {
		return fmt.Errorf(
			"ByteMatch search string is %q, want %q",
			string(statement.SearchString), searchString,
		)
	}
	if len(statement.TextTransformations) != 1 {
		return fmt.Errorf(
			"ByteMatch has %d text transformations, want 1",
			len(statement.TextTransformations),
		)
	}
	transformation := statement.TextTransformations[0]
	if transformation.Priority != 0 || transformation.Type != types.TextTransformationTypeNone {
		return fmt.Errorf(
			"ByteMatch transformation is priority %d type %q, want priority 0 type NONE",
			transformation.Priority, transformation.Type,
		)
	}
	return nil
}

func verifyVisibility(
	visibility *types.VisibilityConfig,
	metricName string,
	owner string,
) error {
	if visibility == nil {
		return fmt.Errorf("%s has no visibility configuration", owner)
	}
	if !visibility.CloudWatchMetricsEnabled {
		return fmt.Errorf("%s CloudWatch metrics are disabled", owner)
	}
	if aws.ToString(visibility.MetricName) != metricName {
		return fmt.Errorf(
			"%s metric name is %q, want %q",
			owner, aws.ToString(visibility.MetricName), metricName,
		)
	}
	if !visibility.SampledRequestsEnabled {
		return fmt.Errorf("%s sampled requests are disabled", owner)
	}
	return nil
}

func listTags(
	ctx context.Context,
	client verifierClient,
	resourceARN string,
) (map[string]string, error) {
	tags := make(map[string]string)
	var marker *string
	for {
		output, err := client.ListTagsForResource(ctx, &wafv2.ListTagsForResourceInput{
			NextMarker:  marker,
			ResourceARN: aws.String(resourceARN),
		})
		if err != nil {
			return nil, fmt.Errorf("list web ACL tags: %w", err)
		}
		if output.TagInfoForResource != nil {
			for _, tag := range output.TagInfoForResource.TagList {
				key := aws.ToString(tag.Key)
				if strings.HasPrefix(key, "aws:") {
					continue
				}
				if _, exists := tags[key]; exists {
					return nil, fmt.Errorf("web ACL tag key %q was returned more than once", key)
				}
				tags[key] = aws.ToString(tag.Value)
			}
		}
		if output.NextMarker == nil || aws.ToString(output.NextMarker) == "" {
			return tags, nil
		}
		marker = output.NextMarker
	}
}

func populatedMemberCount(value any) int {
	structValue := reflect.Indirect(reflect.ValueOf(value))
	structType := structValue.Type()
	count := 0
	for index := 0; index < structValue.NumField(); index++ {
		if structType.Field(index).PkgPath == "" && !structValue.Field(index).IsZero() {
			count++
		}
	}
	return count
}

func equalTags(got map[string]string, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}

func writeIdentity(buildDir string, identity webACLIdentity) error {
	data, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("encode web ACL identity: %w", err)
	}
	if err := os.WriteFile(identityPath(buildDir), data, 0o600); err != nil {
		return fmt.Errorf("record web ACL identity: %w", err)
	}
	return nil
}

func readIdentity(buildDir string) (webACLIdentity, error) {
	data, err := os.ReadFile(identityPath(buildDir))
	if err != nil {
		return webACLIdentity{}, fmt.Errorf("read web ACL identity: %w", err)
	}
	var identity webACLIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return webACLIdentity{}, fmt.Errorf("decode web ACL identity: %w", err)
	}
	if identity.ID == "" || identity.ARN == "" {
		return webACLIdentity{}, errors.New("recorded web ACL identity is incomplete")
	}
	return identity, nil
}

func identityPath(buildDir string) string {
	return filepath.Join(buildDir, recordFileName)
}
