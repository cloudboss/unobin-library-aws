package cloudwatchlogs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	cloudwatchlogs "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

var (
	resourcePolicyARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	resourcePolicyARNRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	resourcePolicyARNAccountPattern   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|\d{12}|cw.{10})$`)
)

// ResourcePolicyResource manages a CloudWatch Logs resource policy. The resource has
// two identity modes: policy-name creates an account-scoped policy, while
// resource-arn creates a resource-scoped policy attached to that ARN. The
// identity fields force replacement; the policy document is updated in place by
// PutResourcePolicy. The document must be valid JSON and is compacted before it
// is sent so insignificant whitespace does not cause a cloud write.
type ResourcePolicyResource struct {
	PolicyDocument string  `ub:"policy-document"`
	PolicyName     *string `ub:"policy-name"`
	ResourceArn    *string `ub:"resource-arn"`
}

// ResourcePolicyResourceOutput holds the policy handle plus values CloudWatch Logs
// returns from DescribeResourcePolicies. The identity fields are stored so a
// replacement deletes the prior policy, and the resource-scoped revision ID is
// stored because updates and deletes use it as the expected revision token.
type ResourcePolicyResourceOutput struct {
	PolicyDocument string  `ub:"policy-document"`
	PolicyName     *string `ub:"policy-name"`
	PolicyScope    string  `ub:"policy-scope"`
	ResourceArn    *string `ub:"resource-arn"`
	RevisionId     *string `ub:"revision-id"`
}

func (r *ResourcePolicyResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the fields that choose the policy's identity mode and
// handle. Changing either names a different policy, so the old policy is
// deleted and a new one is put.
func (r *ResourcePolicyResource) ReplaceFields() []string {
	return []string{
		"policy-name",
		"resource-arn",
	}
}

// Constraints declares the two mutually exclusive CloudWatch Logs resource
// policy identity modes. The ARN parse and regex checks run in validate because
// they need ARN parsing and regular expressions.
func (r ResourcePolicyResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.PolicyName, r.ResourceArn),
	}
}

func (r *ResourcePolicyResource) ValidateInputs(_ context.Context, _ *awsCfg) error {
	_, err := r.validate()
	return err
}

func (r *ResourcePolicyResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*ResourcePolicyResourceOutput, error) {
	document, err := r.validate()
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := client.PutResourcePolicy(ctx, r.putInput(document, nil)); err != nil {
		return nil, fmt.Errorf("put resource policy: %w", err)
	}
	return r.read(ctx, client, r.key(nil))
}

func (r *ResourcePolicyResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *ResourcePolicyResourceOutput,
) (*ResourcePolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, r.key(prior))
}

func (r *ResourcePolicyResource) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ResourcePolicyResource, *ResourcePolicyResourceOutput],
) (*ResourcePolicyResourceOutput, error) {
	document, err := r.validate()
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if r.shouldPut(document, prior) {
		in := r.putInput(document, resourcePolicyExpectedRevisionId(prior))
		if _, err := client.PutResourcePolicy(ctx, in); err != nil {
			return nil, fmt.Errorf("put resource policy: %w", err)
		}
	}
	return r.read(ctx, client, r.key(prior.Outputs))
}

func (r *ResourcePolicyResource) Delete(
	ctx context.Context, cfg *awsCfg, prior *ResourcePolicyResourceOutput) error {
	if prior == nil {
		return errors.New("delete resource policy: missing prior output")
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in, err := deleteResourcePolicyInput(prior)
	if err != nil {
		return err
	}
	if _, err := client.DeleteResourcePolicy(ctx, in); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete resource policy: %w", err)
	}
	return nil
}

func (r *ResourcePolicyResource) putInput(
	document string, expectedRevisionId *string,
) *cloudwatchlogs.PutResourcePolicyInput {
	in := &cloudwatchlogs.PutResourcePolicyInput{
		PolicyDocument: aws.String(document),
	}
	if effectiveOptionalString(r.ResourceArn) != "" {
		in.ResourceArn = r.ResourceArn
	} else {
		in.PolicyName = r.PolicyName
	}
	if effectiveOptionalString(expectedRevisionId) != "" {
		in.ExpectedRevisionId = expectedRevisionId
	}
	return in
}

func (r *ResourcePolicyResource) read(
	ctx context.Context, client *cloudwatchlogs.Client, key resourcePolicyKey,
) (*ResourcePolicyResourceOutput, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	policy, err := findResourcePolicy(ctx, client, key)
	if err != nil {
		return nil, err
	}
	if policy == nil || policy.PolicyDocument == nil {
		return nil, runtime.ErrNotFound
	}
	return resourcePolicyOutput(key, policy), nil
}

func findResourcePolicy(
	ctx context.Context, client *cloudwatchlogs.Client, key resourcePolicyKey,
) (*cloudwatchlogstypes.ResourcePolicy, error) {
	in := &cloudwatchlogs.DescribeResourcePoliciesInput{}
	matches := func(policy cloudwatchlogstypes.ResourcePolicy) bool {
		return aws.ToString(policy.PolicyName) == key.PolicyName
	}
	if key.resourceScoped() {
		in.ResourceArn = aws.String(key.ResourceArn)
		in.PolicyScope = cloudwatchlogstypes.PolicyScopeResource
		matches = func(policy cloudwatchlogstypes.ResourcePolicy) bool {
			return aws.ToString(policy.ResourceArn) == key.ResourceArn
		}
	}
	for {
		resp, err := client.DescribeResourcePolicies(ctx, in)
		if err != nil {
			if isNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe resource policies: %w", err)
		}
		for i := range resp.ResourcePolicies {
			if matches(resp.ResourcePolicies[i]) {
				return &resp.ResourcePolicies[i], nil
			}
		}
		if effectiveOptionalString(resp.NextToken) == "" {
			break
		}
		in.NextToken = resp.NextToken
	}
	return nil, nil
}

func resourcePolicyOutput(
	key resourcePolicyKey, policy *cloudwatchlogstypes.ResourcePolicy,
) *ResourcePolicyResourceOutput {
	out := &ResourcePolicyResourceOutput{
		PolicyDocument: aws.ToString(policy.PolicyDocument),
		PolicyName:     nonEmptyStringPtr(policy.PolicyName),
		PolicyScope:    string(policy.PolicyScope),
		ResourceArn:    nonEmptyStringPtr(policy.ResourceArn),
		RevisionId:     nonEmptyStringPtr(policy.RevisionId),
	}
	if out.PolicyName == nil && key.PolicyName != "" {
		out.PolicyName = aws.String(key.PolicyName)
	}
	if out.ResourceArn == nil && key.ResourceArn != "" {
		out.ResourceArn = aws.String(key.ResourceArn)
	}
	if out.PolicyScope == "" {
		if key.resourceScoped() {
			out.PolicyScope = string(cloudwatchlogstypes.PolicyScopeResource)
		} else {
			out.PolicyScope = string(cloudwatchlogstypes.PolicyScopeAccount)
		}
	}
	return out
}

func (r *ResourcePolicyResource) shouldPut(
	desiredDocument string, prior runtime.Prior[ResourcePolicyResource, *ResourcePolicyResourceOutput],
) bool {
	priorDocument, err := normalizeResourcePolicyDocument(prior.Inputs.PolicyDocument)
	if err != nil || priorDocument != desiredDocument {
		return true
	}
	if prior.Observed == nil {
		return false
	}
	observedDocument, err := normalizeResourcePolicyDocument(prior.Observed.PolicyDocument)
	return err != nil || observedDocument != desiredDocument
}

func (r *ResourcePolicyResource) key(prior *ResourcePolicyResourceOutput) resourcePolicyKey {
	if prior != nil {
		if resourceArn := effectiveOptionalString(prior.ResourceArn); resourceArn != "" {
			return resourcePolicyKey{ResourceArn: resourceArn}
		}
		if policyName := effectiveOptionalString(prior.PolicyName); policyName != "" {
			return resourcePolicyKey{PolicyName: policyName}
		}
	}
	if resourceArn := effectiveOptionalString(r.ResourceArn); resourceArn != "" {
		return resourcePolicyKey{ResourceArn: resourceArn}
	}
	if policyName := effectiveOptionalString(r.PolicyName); policyName != "" {
		return resourcePolicyKey{PolicyName: policyName}
	}
	return resourcePolicyKey{}
}

func (r *ResourcePolicyResource) validate() (string, error) {
	document, err := normalizeResourcePolicyDocument(r.PolicyDocument)
	if err != nil {
		return "", err
	}
	if (r.PolicyName == nil) == (r.ResourceArn == nil) {
		return "", errors.New("exactly one of policy-name or resource-arn is required")
	}
	policyName := effectiveOptionalString(r.PolicyName)
	resourceArn := effectiveOptionalString(r.ResourceArn)
	if r.PolicyName != nil && policyName == "" {
		return "", errors.New("policy-name must not be empty")
	}
	if r.ResourceArn != nil && resourceArn == "" {
		return "", errors.New("resource-arn must not be empty")
	}
	if resourceArn != "" && !validResourcePolicyARN(resourceArn) {
		return "", errors.New("resource-arn must be a valid ARN")
	}
	return document, nil
}

func normalizeResourcePolicyDocument(document string) (string, error) {
	trimmed := strings.TrimSpace(document)
	if trimmed == "" {
		return "", errors.New("policy-document must not be empty")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(trimmed)); err != nil {
		return "", fmt.Errorf("policy-document must be valid JSON: %w", err)
	}
	return buf.String(), nil
}

func validResourcePolicyARN(s string) bool {
	parsed, err := awsarn.Parse(s)
	if err != nil {
		return false
	}
	if !resourcePolicyARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !resourcePolicyARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" && !resourcePolicyARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}

func resourcePolicyExpectedRevisionId(
	prior runtime.Prior[ResourcePolicyResource, *ResourcePolicyResourceOutput],
) *string {
	if prior.Observed != nil {
		if revisionId := effectiveOptionalString(prior.Observed.RevisionId); revisionId != "" {
			return aws.String(revisionId)
		}
	}
	if prior.Outputs != nil {
		if revisionId := effectiveOptionalString(prior.Outputs.RevisionId); revisionId != "" {
			return aws.String(revisionId)
		}
	}
	return nil
}

func deleteResourcePolicyInput(
	prior *ResourcePolicyResourceOutput) (*cloudwatchlogs.DeleteResourcePolicyInput, error) {
	resourceArn := effectiveOptionalString(prior.ResourceArn)
	if resourceArn != "" {
		revisionId := effectiveOptionalString(prior.RevisionId)
		if revisionId == "" {
			return nil, errors.New(
				"delete resource-scoped resource policy: revision-id is required")
		}
		return &cloudwatchlogs.DeleteResourcePolicyInput{
			ExpectedRevisionId: aws.String(revisionId),
			ResourceArn:        aws.String(resourceArn),
		}, nil
	}
	policyName := effectiveOptionalString(prior.PolicyName)
	if policyName == "" {
		return nil, errors.New("delete resource policy: policy-name is required")
	}
	return &cloudwatchlogs.DeleteResourcePolicyInput{
		PolicyName: aws.String(policyName),
	}, nil
}

type resourcePolicyKey struct {
	PolicyName  string
	ResourceArn string
}

func (k resourcePolicyKey) resourceScoped() bool {
	return k.ResourceArn != ""
}

func (k resourcePolicyKey) validate() error {
	if (k.PolicyName == "") == (k.ResourceArn == "") {
		return errors.New("exactly one of policy-name or resource-arn is required")
	}
	return nil
}
