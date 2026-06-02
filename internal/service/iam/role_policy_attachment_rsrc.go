package iam

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

// RolePolicyAttachment attaches a managed policy to an IAM role. It is
// a pure association between a role and a policy ARN, so it has no tags,
// no description, and no mutable fields. Changing either the role or the
// policy ARN makes a different attachment, which is why both are replace
// fields.
type RolePolicyAttachment struct {
	RoleName  string `ub:"role-name"`
	PolicyArn string `ub:"policy-arn"`
}

// RolePolicyAttachmentOutput is empty because the attachment has no
// identifier or computed value of its own. The role name and policy ARN
// are inputs and are already referenceable, so nothing is echoed here.
type RolePolicyAttachmentOutput struct{}

func (r *RolePolicyAttachment) SchemaVersion() int { return 1 }

func (r *RolePolicyAttachment) ReplaceFields() []string {
	return []string{
		"role-name",
		"policy-arn",
	}
}

func (r *RolePolicyAttachment) Create(
	ctx context.Context, cfg any,
) (*RolePolicyAttachmentOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &iam.AttachRolePolicyInput{
		RoleName:  aws.String(r.RoleName),
		PolicyArn: aws.String(r.PolicyArn),
	}
	// IAM serializes changes to one role, so attaching several policies at
	// once can collide. The conflict clears on its own, so retry through it.
	err = retry.OnError(ctx, isConcurrentModification,
		func(ctx context.Context) error {
			_, err := client.AttachRolePolicy(ctx, in)
			return err
		})
	if err != nil {
		return nil, err
	}
	return &RolePolicyAttachmentOutput{}, nil
}

// Read lists the policies attached to the role and confirms the policy
// ARN is among them. IAM has no API to read a single attachment, so the
// presence of the ARN in the list is the attachment. A missing role
// returns NoSuchEntity, and a role whose list no longer contains the ARN
// means the attachment drifted away. Both map to runtime.ErrNotFound.
func (r *RolePolicyAttachment) Read(
	ctx context.Context, cfg any, prior *RolePolicyAttachmentOutput,
) (*RolePolicyAttachmentOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	paginator := iam.NewListAttachedRolePoliciesPaginator(client,
		&iam.ListAttachedRolePoliciesInput{
			RoleName: aws.String(r.RoleName),
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, err
		}
		for _, policy := range page.AttachedPolicies {
			if aws.ToString(policy.PolicyArn) == r.PolicyArn {
				return &RolePolicyAttachmentOutput{}, nil
			}
		}
	}
	return nil, runtime.ErrNotFound
}

func (r *RolePolicyAttachment) Update(
	ctx context.Context, cfg any,
	prior runtime.Prior[RolePolicyAttachment, *RolePolicyAttachmentOutput],
) (*RolePolicyAttachmentOutput, error) {
	return prior.Outputs, nil
}

// Delete detaches the policy from the role. A detach of an attachment
// that is already gone returns NoSuchEntity, which is treated as success
// so delete is idempotent.
func (r *RolePolicyAttachment) Delete(
	ctx context.Context, cfg any, prior *RolePolicyAttachmentOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &iam.DetachRolePolicyInput{
		RoleName:  aws.String(r.RoleName),
		PolicyArn: aws.String(r.PolicyArn),
	}
	// As with attach, a concurrent change to the role can make detach collide;
	// retry through the conflict.
	err = retry.OnError(ctx, isConcurrentModification,
		func(ctx context.Context) error {
			_, err := client.DetachRolePolicy(ctx, in)
			return err
		})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
