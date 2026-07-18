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
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

const (
	webACLName            = "unobin-it-wafv2"
	replacementWebACLName = "unobin-it-wafv2-replacement"
	resourceOldPoolName   = "unobin-it-wafv2-association-resource-old"
	resourceNewPoolName   = "unobin-it-wafv2-association-resource-new"
	webACLTargetPoolName  = "unobin-it-wafv2-association-web-acl"
	driftTargetPoolName   = "unobin-it-wafv2-association-drift"
	ruleName              = "uri-path-count"
	webACLMetricName      = "unobin-it-wafv2"
	ruleMetricName        = "unobin-it-wafv2-rule"
	recordFileName        = "web-acl-identity.json"
)

type webACLIdentity struct {
	ID  string `json:"id"`
	ARN string `json:"arn"`
}

type scenarioIdentity struct {
	WebACL            webACLIdentity `json:"web-acl"`
	ReplacementWebACL webACLIdentity `json:"replacement-web-acl"`
	ResourceOldARN    string         `json:"resource-old-arn"`
	ResourceNewARN    string         `json:"resource-new-arn"`
	WebACLTargetARN   string         `json:"web-acl-target-arn"`
	DriftTargetARN    string         `json:"drift-target-arn"`
}

type verifierClient interface {
	AssociateWebACL(
		context.Context,
		*wafv2.AssociateWebACLInput,
		...func(*wafv2.Options),
	) (*wafv2.AssociateWebACLOutput, error)
	GetWebACL(
		context.Context,
		*wafv2.GetWebACLInput,
		...func(*wafv2.Options),
	) (*wafv2.GetWebACLOutput, error)
	GetWebACLForResource(
		context.Context,
		*wafv2.GetWebACLForResourceInput,
		...func(*wafv2.Options),
	) (*wafv2.GetWebACLForResourceOutput, error)
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

type cognitoVerifierClient interface {
	DescribeUserPool(
		context.Context,
		*cognitoidentityprovider.DescribeUserPoolInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.DescribeUserPoolOutput, error)
	ListUserPools(
		context.Context,
		*cognitoidentityprovider.ListUserPoolsInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.ListUserPoolsOutput, error)
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
	wafClient := wafv2.NewFromConfig(configuration)
	cognitoClient := cognitoidentityprovider.NewFromConfig(configuration)

	switch mode {
	case "applied":
		return verifyApplied(ctx, wafClient, cognitoClient, buildDir)
	case "updated":
		return verifyUpdated(ctx, wafClient, buildDir)
	default:
		return verifyDestroyed(ctx, wafClient, buildDir)
	}
}

func verifyApplied(
	ctx context.Context,
	client verifierClient,
	cognitoClient cognitoVerifierClient,
	buildDir string,
) error {
	webACL, err := findWebACL(ctx, client, webACLName)
	if err != nil {
		return err
	}
	wantTags := map[string]string{
		"change": "old",
		"keep":   "1",
		"remove": "yes",
		"unobin": "wafv2-it",
	}
	if err := verifyPresent(ctx, client, webACL, "initial", "/initial", wantTags); err != nil {
		return err
	}
	replacement, err := findWebACL(ctx, client, replacementWebACLName)
	if err != nil {
		return err
	}
	identity, err := findScenarioIdentity(ctx, cognitoClient, webACL, replacement)
	if err != nil {
		return err
	}
	if err := verifyAssociation(ctx, client, identity.ResourceOldARN, webACL.ARN); err != nil {
		return err
	}
	if err := verifyNoAssociation(ctx, client, identity.ResourceNewARN); err != nil {
		return err
	}
	if err := verifyAssociation(ctx, client, identity.WebACLTargetARN, webACL.ARN); err != nil {
		return err
	}
	if err := verifyAssociation(ctx, client, identity.DriftTargetARN, webACL.ARN); err != nil {
		return err
	}
	if err := writeIdentity(buildDir, identity); err != nil {
		return err
	}
	if err := createAssociationDrift(
		ctx, client, identity.DriftTargetARN, replacement.ARN,
	); err != nil {
		return err
	}
	fmt.Printf("ok: WAFv2 ACLs and associations match the applied configuration\n")
	return nil
}

func verifyUpdated(ctx context.Context, client verifierClient, buildDir string) error {
	wantIdentity, err := readIdentity(buildDir)
	if err != nil {
		return err
	}
	webACL, err := findWebACL(ctx, client, webACLName)
	if err != nil {
		return err
	}
	if webACL != wantIdentity.WebACL {
		return fmt.Errorf(
			"web ACL identity is id %q arn %q, want id %q arn %q",
			webACL.ID, webACL.ARN, wantIdentity.WebACL.ID, wantIdentity.WebACL.ARN,
		)
	}
	replacement, err := findWebACL(ctx, client, replacementWebACLName)
	if err != nil {
		return err
	}
	if replacement != wantIdentity.ReplacementWebACL {
		return fmt.Errorf("replacement web ACL identity changed")
	}
	wantTags := map[string]string{
		"add":    "yes",
		"change": "new",
		"keep":   "1",
		"unobin": "wafv2-it",
	}
	if err := verifyPresent(ctx, client, webACL, "updated", "/updated", wantTags); err != nil {
		return err
	}
	if err := verifyNoAssociation(ctx, client, wantIdentity.ResourceOldARN); err != nil {
		return err
	}
	if err := verifyAssociation(
		ctx, client, wantIdentity.ResourceNewARN, webACL.ARN,
	); err != nil {
		return err
	}
	if err := verifyAssociation(
		ctx, client, wantIdentity.WebACLTargetARN, replacement.ARN,
	); err != nil {
		return err
	}
	if err := verifyAssociation(
		ctx, client, wantIdentity.DriftTargetARN, webACL.ARN,
	); err != nil {
		return err
	}
	fmt.Printf("ok: WAFv2 ACLs and associations match the updated configuration\n")
	return nil
}

func verifyDestroyed(ctx context.Context, client verifierClient, buildDir string) error {
	identity, err := readIdentity(buildDir)
	if err != nil {
		return err
	}
	if err := verifyWebACLDestroyed(ctx, client, webACLName, identity.WebACL); err != nil {
		return err
	}
	if err := verifyWebACLDestroyed(
		ctx, client, replacementWebACLName, identity.ReplacementWebACL,
	); err != nil {
		return err
	}
	resourceARNs := []string{
		identity.ResourceOldARN,
		identity.ResourceNewARN,
		identity.WebACLTargetARN,
		identity.DriftTargetARN,
	}
	for _, resourceARN := range resourceARNs {
		if err := verifyNoAssociation(ctx, client, resourceARN); err != nil {
			return err
		}
	}
	fmt.Printf("ok: WAFv2 ACLs and associations are gone\n")
	return nil
}

func findWebACL(
	ctx context.Context,
	client verifierClient,
	name string,
) (webACLIdentity, error) {
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
			if aws.ToString(summary.Name) != name {
				continue
			}
			identity := webACLIdentity{
				ID:  aws.ToString(summary.Id),
				ARN: aws.ToString(summary.ARN),
			}
			if identity.ID == "" || identity.ARN == "" {
				return webACLIdentity{}, fmt.Errorf(
					"web ACL %s list identity is incomplete", name,
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
		"web ACL %s was not found in REGIONAL scope", name,
	)
}

func findScenarioIdentity(
	ctx context.Context,
	client cognitoVerifierClient,
	webACL webACLIdentity,
	replacement webACLIdentity,
) (scenarioIdentity, error) {
	identity := scenarioIdentity{
		WebACL:            webACL,
		ReplacementWebACL: replacement,
	}
	pools := []struct {
		name string
		arn  *string
	}{
		{name: resourceOldPoolName, arn: &identity.ResourceOldARN},
		{name: resourceNewPoolName, arn: &identity.ResourceNewARN},
		{name: webACLTargetPoolName, arn: &identity.WebACLTargetARN},
		{name: driftTargetPoolName, arn: &identity.DriftTargetARN},
	}
	for _, pool := range pools {
		arn, err := findUserPoolARN(ctx, client, pool.name)
		if err != nil {
			return scenarioIdentity{}, err
		}
		*pool.arn = arn
	}
	return identity, nil
}

func findUserPoolARN(
	ctx context.Context,
	client cognitoVerifierClient,
	name string,
) (string, error) {
	var token *string
	for {
		output, err := client.ListUserPools(ctx, &cognitoidentityprovider.ListUserPoolsInput{
			MaxResults: aws.Int32(60),
			NextToken:  token,
		})
		if err != nil {
			return "", fmt.Errorf("list Cognito user pools: %w", err)
		}
		for _, pool := range output.UserPools {
			if aws.ToString(pool.Name) != name {
				continue
			}
			id := aws.ToString(pool.Id)
			if id == "" {
				return "", fmt.Errorf("Cognito user pool %s has no ID", name)
			}
			described, err := client.DescribeUserPool(
				ctx,
				&cognitoidentityprovider.DescribeUserPoolInput{UserPoolId: aws.String(id)},
			)
			if err != nil {
				return "", fmt.Errorf("describe Cognito user pool %s: %w", name, err)
			}
			if described.UserPool == nil || aws.ToString(described.UserPool.Arn) == "" {
				return "", fmt.Errorf("Cognito user pool %s has no ARN", name)
			}
			return aws.ToString(described.UserPool.Arn), nil
		}
		if output.NextToken == nil || aws.ToString(output.NextToken) == "" {
			return "", fmt.Errorf("Cognito user pool %s was not found", name)
		}
		token = output.NextToken
	}
}

func verifyAssociation(
	ctx context.Context,
	client verifierClient,
	resourceARN string,
	wantWebACLARN string,
) error {
	output, err := client.GetWebACLForResource(ctx, &wafv2.GetWebACLForResourceInput{
		ResourceArn: aws.String(resourceARN),
	})
	if err != nil {
		return fmt.Errorf("get web ACL for resource %s: %w", resourceARN, err)
	}
	if output == nil || output.WebACL == nil {
		return fmt.Errorf("resource %s has no web ACL association", resourceARN)
	}
	got := aws.ToString(output.WebACL.ARN)
	if got != wantWebACLARN {
		return fmt.Errorf(
			"resource %s is associated with %q, want %q",
			resourceARN, got, wantWebACLARN,
		)
	}
	return nil
}

func verifyNoAssociation(
	ctx context.Context,
	client verifierClient,
	resourceARN string,
) error {
	output, err := client.GetWebACLForResource(ctx, &wafv2.GetWebACLForResourceInput{
		ResourceArn: aws.String(resourceARN),
	})
	if err != nil {
		var notFound *types.WAFNonexistentItemException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("get web ACL for resource %s: %w", resourceARN, err)
	}
	if output == nil || output.WebACL == nil {
		return nil
	}
	return fmt.Errorf(
		"resource %s remains associated with web ACL %q",
		resourceARN, aws.ToString(output.WebACL.ARN),
	)
}

func createAssociationDrift(
	ctx context.Context,
	client verifierClient,
	resourceARN string,
	webACLARN string,
) error {
	_, err := client.AssociateWebACL(ctx, &wafv2.AssociateWebACLInput{
		ResourceArn: aws.String(resourceARN),
		WebACLArn:   aws.String(webACLARN),
	})
	if err != nil {
		return fmt.Errorf("create association drift: %w", err)
	}
	return verifyAssociation(ctx, client, resourceARN, webACLARN)
}

func verifyWebACLDestroyed(
	ctx context.Context,
	client verifierClient,
	name string,
	identity webACLIdentity,
) error {
	_, err := client.GetWebACL(ctx, &wafv2.GetWebACLInput{
		Id:    aws.String(identity.ID),
		Name:  aws.String(name),
		Scope: types.ScopeRegional,
	})
	if err == nil {
		return fmt.Errorf("web ACL %s still exists", name)
	}
	var notFound *types.WAFNonexistentItemException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("get destroyed web ACL %s: %w", name, err)
	}
	return nil
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

func writeIdentity(buildDir string, identity scenarioIdentity) error {
	data, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("encode WAFv2 scenario identity: %w", err)
	}
	if err := os.WriteFile(identityPath(buildDir), data, 0o600); err != nil {
		return fmt.Errorf("record WAFv2 scenario identity: %w", err)
	}
	return nil
}

func readIdentity(buildDir string) (scenarioIdentity, error) {
	data, err := os.ReadFile(identityPath(buildDir))
	if err != nil {
		return scenarioIdentity{}, fmt.Errorf("read WAFv2 scenario identity: %w", err)
	}
	var identity scenarioIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return scenarioIdentity{}, fmt.Errorf("decode WAFv2 scenario identity: %w", err)
	}
	if identity.WebACL.ID == "" || identity.WebACL.ARN == "" ||
		identity.ReplacementWebACL.ID == "" || identity.ReplacementWebACL.ARN == "" ||
		identity.ResourceOldARN == "" || identity.ResourceNewARN == "" ||
		identity.WebACLTargetARN == "" || identity.DriftTargetARN == "" {
		return scenarioIdentity{}, errors.New("recorded WAFv2 scenario identity is incomplete")
	}
	return identity, nil
}

func identityPath(buildDir string) string {
	return filepath.Join(buildDir, recordFileName)
}
