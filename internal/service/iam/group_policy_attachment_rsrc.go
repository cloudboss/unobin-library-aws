package iam

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

var (
	groupPolicyAttachmentARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	groupPolicyAttachmentARNRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	groupPolicyAttachmentARNAccountPattern   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`)
)

// GroupPolicyAttachment attaches a managed policy to an IAM group. The group
// name and policy ARN are the full identity, and IAM has no in-place update for
// an attachment, so both inputs are replace-only.
type GroupPolicyAttachment struct {
	GroupName string `ub:"group-name"`
	PolicyArn string `ub:"policy-arn"`
}

// GroupPolicyAttachmentOutput preserves the attachment identity so replacement
// deletes the prior group/policy pair instead of the current desired pair.
type GroupPolicyAttachmentOutput struct {
	GroupName string `ub:"group-name"`
	PolicyArn string `ub:"policy-arn"`
}

func (r *GroupPolicyAttachment) SchemaVersion() int { return 1 }

func (r *GroupPolicyAttachment) ReplaceFields() []string {
	return []string{
		"group-name",
		"policy-arn",
	}
}

func (r *GroupPolicyAttachment) Create(
	ctx context.Context, cfg *awsCfg,
) (*GroupPolicyAttachmentOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &iam.AttachGroupPolicyInput{
		GroupName: aws.String(r.GroupName),
		PolicyArn: aws.String(r.PolicyArn),
	}
	err = retry.OnError(ctx, isConcurrentModification,
		func(ctx context.Context) error {
			_, err := client.AttachGroupPolicy(ctx, in)
			return err
		})
	if err != nil {
		return nil, fmt.Errorf("attach group policy: %w", err)
	}
	return r.read(ctx, client, true)
}

func (r *GroupPolicyAttachment) Read(
	ctx context.Context, cfg *awsCfg, prior *GroupPolicyAttachmentOutput,
) (*GroupPolicyAttachmentOutput, error) {
	key := r.key(prior)
	if err := validateGroupPolicyAttachmentARN(key.PolicyArn); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return key.read(ctx, client, false)
}

func (r *GroupPolicyAttachment) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[GroupPolicyAttachment, *GroupPolicyAttachmentOutput],
) (*GroupPolicyAttachmentOutput, error) {
	return prior.Outputs, nil
}

func (r *GroupPolicyAttachment) Delete(
	ctx context.Context, cfg *awsCfg, prior *GroupPolicyAttachmentOutput,
) error {
	key := r.key(prior)
	if err := validateGroupPolicyAttachmentARN(key.PolicyArn); err != nil {
		return err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &iam.DetachGroupPolicyInput{
		GroupName: aws.String(key.GroupName),
		PolicyArn: aws.String(key.PolicyArn),
	}
	err = retry.OnError(ctx, isConcurrentModification,
		func(ctx context.Context) error {
			_, err := client.DetachGroupPolicy(ctx, in)
			return err
		})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("detach group policy: %w", err)
	}
	return nil
}

func (r *GroupPolicyAttachment) read(
	ctx context.Context, client *iam.Client, created bool,
) (*GroupPolicyAttachmentOutput, error) {
	what := fmt.Sprintf("policy %s attached to group %s", r.PolicyArn, r.GroupName)
	err := wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		found, err := r.find(ctx, client)
		if err != nil {
			if created && errors.Is(err, runtime.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		return found, nil
	})
	if err != nil {
		return nil, err
	}
	return &GroupPolicyAttachmentOutput{GroupName: r.GroupName, PolicyArn: r.PolicyArn}, nil
}

func (r *GroupPolicyAttachment) find(ctx context.Context, client *iam.Client) (bool, error) {
	paginator := iam.NewListAttachedGroupPoliciesPaginator(client,
		&iam.ListAttachedGroupPoliciesInput{
			GroupName: aws.String(r.GroupName),
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return false, runtime.ErrNotFound
			}
			return false, fmt.Errorf("list attached group policies: %w", err)
		}
		for _, policy := range page.AttachedPolicies {
			policyArn := aws.ToString(policy.PolicyArn)
			if policyArn == "" {
				continue
			}
			if policyArn == r.PolicyArn {
				return true, nil
			}
		}
	}
	return false, runtime.ErrNotFound
}

func (r *GroupPolicyAttachment) validate() error {
	return validateGroupPolicyAttachmentARN(r.PolicyArn)
}

func (r *GroupPolicyAttachment) key(
	prior *GroupPolicyAttachmentOutput,
) *GroupPolicyAttachment {
	if prior != nil && prior.GroupName != "" && prior.PolicyArn != "" {
		return &GroupPolicyAttachment{GroupName: prior.GroupName, PolicyArn: prior.PolicyArn}
	}
	return r
}

func validateGroupPolicyAttachmentARN(value string) error {
	if !validGroupPolicyAttachmentARN(value) {
		return fmt.Errorf("policy-arn must be a valid ARN")
	}
	return nil
}

func validGroupPolicyAttachmentARN(value string) bool {
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return false
	}
	if !groupPolicyAttachmentARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !groupPolicyAttachmentARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" &&
		!groupPolicyAttachmentARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}
