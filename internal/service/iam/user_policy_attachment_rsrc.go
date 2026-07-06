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
	userPolicyAttachmentARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	userPolicyAttachmentARNRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	userPolicyAttachmentARNAccountPattern   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`)
)

// UserPolicyAttachmentResource attaches a managed policy to an IAM user. The user name
// and policy ARN are the identity, and IAM has no in-place update, so both
// inputs are replace-only.
type UserPolicyAttachmentResource struct {
	User      string `ub:"user"`
	PolicyArn string `ub:"policy-arn"`
}

// UserPolicyAttachmentResourceOutput records the identity observed after create and
// read so replacement deletes the prior attachment.
type UserPolicyAttachmentResourceOutput struct {
	User      string `ub:"user"`
	PolicyArn string `ub:"policy-arn"`
}

func (r *UserPolicyAttachmentResource) SchemaVersion() int { return 1 }

func (r *UserPolicyAttachmentResource) ReplaceFields() []string {
	return []string{
		"user",
		"policy-arn",
	}
}

func (r *UserPolicyAttachmentResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*UserPolicyAttachmentResourceOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &iam.AttachUserPolicyInput{
		UserName:  aws.String(r.User),
		PolicyArn: aws.String(r.PolicyArn),
	}
	err = retry.OnError(ctx, isConcurrentModification,
		func(ctx context.Context) error {
			_, err := client.AttachUserPolicy(ctx, in)
			return err
		})
	if err != nil {
		return nil, fmt.Errorf("attach user policy: %w", err)
	}
	return r.read(ctx, client, true)
}

func (r *UserPolicyAttachmentResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *UserPolicyAttachmentResourceOutput,
) (*UserPolicyAttachmentResourceOutput, error) {
	key := r.key(prior)
	if err := validateUserPolicyAttachmentARN(key.PolicyArn); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return key.read(ctx, client, false)
}

func (r *UserPolicyAttachmentResource) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[UserPolicyAttachmentResource, *UserPolicyAttachmentResourceOutput],
) (*UserPolicyAttachmentResourceOutput, error) {
	return prior.Outputs, nil
}

func (r *UserPolicyAttachmentResource) Delete(
	ctx context.Context, cfg *awsCfg, prior *UserPolicyAttachmentResourceOutput) error {
	key := r.key(prior)
	if err := validateUserPolicyAttachmentARN(key.PolicyArn); err != nil {
		return err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &iam.DetachUserPolicyInput{
		UserName:  aws.String(key.User),
		PolicyArn: aws.String(key.PolicyArn),
	}
	err = retry.OnError(ctx, isConcurrentModification,
		func(ctx context.Context) error {
			_, err := client.DetachUserPolicy(ctx, in)
			return err
		})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("detach user policy: %w", err)
	}
	return nil
}

func (r *UserPolicyAttachmentResource) read(
	ctx context.Context, client *iam.Client, created bool,
) (*UserPolicyAttachmentResourceOutput, error) {
	var out *UserPolicyAttachmentResourceOutput
	what := fmt.Sprintf("policy %s attached to user %s", r.PolicyArn, r.User)
	err := wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		policyArn, err := r.find(ctx, client)
		if err != nil {
			if created && errors.Is(err, runtime.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		out = &UserPolicyAttachmentResourceOutput{User: r.User, PolicyArn: policyArn}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *UserPolicyAttachmentResource) find(
	ctx context.Context,
	client *iam.Client,
) (string, error) {
	paginator := iam.NewListAttachedUserPoliciesPaginator(client,
		&iam.ListAttachedUserPoliciesInput{
			UserName: aws.String(r.User),
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return "", runtime.ErrNotFound
			}
			return "", fmt.Errorf("list attached user policies: %w", err)
		}
		for _, policy := range page.AttachedPolicies {
			policyArn := aws.ToString(policy.PolicyArn)
			if policyArn == r.PolicyArn {
				return policyArn, nil
			}
		}
	}
	return "", runtime.ErrNotFound
}

func (r *UserPolicyAttachmentResource) validate() error {
	return validateUserPolicyAttachmentARN(r.PolicyArn)
}

func (r *UserPolicyAttachmentResource) key(
	prior *UserPolicyAttachmentResourceOutput) *UserPolicyAttachmentResource {
	if prior != nil {
		return &UserPolicyAttachmentResource{User: prior.User, PolicyArn: prior.PolicyArn}
	}
	return r
}

func validateUserPolicyAttachmentARN(value string) error {
	if !validUserPolicyAttachmentARN(value) {
		return fmt.Errorf("policy-arn must be a valid ARN")
	}
	return nil
}

func validUserPolicyAttachmentARN(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return false
	}
	if !userPolicyAttachmentARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !userPolicyAttachmentARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" && !userPolicyAttachmentARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}
