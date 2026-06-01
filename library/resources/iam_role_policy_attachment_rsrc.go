package resources

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/library/internal/iamhelpers"
	"github.com/cloudboss/unobin-library-aws/library/internal/retry"
)

// IamRolePolicyAttachment attaches a managed policy to an IAM role. It is
// a pure association between a role and a policy ARN, so it has no tags,
// no description, and no mutable fields. Changing either the role or the
// policy ARN makes a different attachment, which is why both are replace
// fields.
type IamRolePolicyAttachment struct {
	RoleName  string `ub:"role-name"`
	PolicyArn string `ub:"policy-arn"`
}

// IamRolePolicyAttachmentOutput is empty because the attachment has no
// identifier or computed value of its own. The role name and policy ARN
// are inputs and are already referenceable, so nothing is echoed here.
type IamRolePolicyAttachmentOutput struct{}

func (r *IamRolePolicyAttachment) SchemaVersion() int { return 1 }

func (r *IamRolePolicyAttachment) ReplaceFields() []string {
	return []string{
		"role-name",
		"policy-arn",
	}
}

func (r *IamRolePolicyAttachment) Create(
	ctx context.Context, cfg any,
) (*IamRolePolicyAttachmentOutput, error) {
	client, err := iamhelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &iam.AttachRolePolicyInput{
		RoleName:  aws.String(r.RoleName),
		PolicyArn: aws.String(r.PolicyArn),
	}
	// IAM serializes changes to one role, so attaching several policies at
	// once can collide. The conflict clears on its own, so retry through it.
	err = retry.OnError(ctx, iamhelpers.IsConcurrentModification,
		func(ctx context.Context) error {
			_, err := client.AttachRolePolicy(ctx, in)
			return err
		})
	if err != nil {
		return nil, err
	}
	return &IamRolePolicyAttachmentOutput{}, nil
}

// Read lists the policies attached to the role and confirms the policy
// ARN is among them. IAM has no API to read a single attachment, so the
// presence of the ARN in the list is the attachment. A missing role
// returns NoSuchEntity, and a role whose list no longer contains the ARN
// means the attachment drifted away. Both map to runtime.ErrNotFound.
func (r *IamRolePolicyAttachment) Read(
	ctx context.Context, cfg any, prior *IamRolePolicyAttachmentOutput,
) (*IamRolePolicyAttachmentOutput, error) {
	client, err := iamhelpers.NewClient(ctx, cfg)
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
			if iamhelpers.IsNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, err
		}
		for _, policy := range page.AttachedPolicies {
			if aws.ToString(policy.PolicyArn) == r.PolicyArn {
				return &IamRolePolicyAttachmentOutput{}, nil
			}
		}
	}
	return nil, runtime.ErrNotFound
}

func (r *IamRolePolicyAttachment) Update(
	ctx context.Context, cfg any,
	prior runtime.Prior[IamRolePolicyAttachment, *IamRolePolicyAttachmentOutput],
) (*IamRolePolicyAttachmentOutput, error) {
	return prior.Outputs, nil
}

// Delete detaches the policy from the role. A detach of an attachment
// that is already gone returns NoSuchEntity, which is treated as success
// so delete is idempotent.
func (r *IamRolePolicyAttachment) Delete(
	ctx context.Context, cfg any, prior *IamRolePolicyAttachmentOutput,
) error {
	client, err := iamhelpers.NewClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &iam.DetachRolePolicyInput{
		RoleName:  aws.String(r.RoleName),
		PolicyArn: aws.String(r.PolicyArn),
	}
	// As with attach, a concurrent change to the role can make detach collide;
	// retry through the conflict.
	err = retry.OnError(ctx, iamhelpers.IsConcurrentModification,
		func(ctx context.Context) error {
			_, err := client.DetachRolePolicy(ctx, in)
			return err
		})
	if err != nil {
		if iamhelpers.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
