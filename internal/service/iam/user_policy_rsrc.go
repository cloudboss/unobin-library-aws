package iam

import (
	"context"
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// UserPolicy manages an inline policy embedded in an IAM user. The user name
// and policy name identify the policy, so changes to either replace it. The
// policy document is normalized before it is sent to IAM and updated in place
// with PutUserPolicy.
type UserPolicy struct {
	UserName       string `ub:"user-name"`
	PolicyName     string `ub:"policy-name"`
	PolicyDocument string `ub:"policy-document"`
}

// UserPolicyOutput holds the identity and policy document IAM stores. The
// document is URL-decoded and normalized so references see the stored JSON
// rather than IAM's percent-encoded transport value.
type UserPolicyOutput struct {
	UserName       string `ub:"user-name"`
	PolicyName     string `ub:"policy-name"`
	PolicyDocument string `ub:"policy-document"`
}

func (r *UserPolicy) SchemaVersion() int { return 1 }

func (r *UserPolicy) ReplaceFields() []string {
	return []string{
		"user-name",
		"policy-name",
	}
}

func (r *UserPolicy) Create(ctx context.Context, cfg *awsCfg) (*UserPolicyOutput, error) {
	document, err := normalizeIAMPolicyJSON(r.PolicyDocument)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.putDocument(ctx, client, document); err != nil {
		return nil, err
	}
	return r.read(ctx, client, true)
}

func (r *UserPolicy) Read(
	ctx context.Context, cfg *awsCfg, prior *UserPolicyOutput,
) (*UserPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return (&UserPolicy{
		UserName:   prior.UserName,
		PolicyName: prior.PolicyName,
	}).read(ctx, client, false)
}

func (r *UserPolicy) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[UserPolicy, *UserPolicyOutput],
) (*UserPolicyOutput, error) {
	desiredDocument, err := normalizeIAMPolicyJSON(r.PolicyDocument)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.PolicyDocument, r.PolicyDocument) ||
		userPolicyDocumentDrifted(prior.Observed, desiredDocument) {
		if err := r.putDocument(ctx, client, desiredDocument); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, false)
}

func (r *UserPolicy) Delete(ctx context.Context, cfg *awsCfg, prior *UserPolicyOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &iam.DeleteUserPolicyInput{
		UserName:   aws.String(prior.UserName),
		PolicyName: aws.String(prior.PolicyName),
	}
	if _, err := client.DeleteUserPolicy(ctx, in); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete user policy: %w", err)
	}
	return nil
}

func (r *UserPolicy) putDocument(ctx context.Context, client *iam.Client, document string) error {
	in := &iam.PutUserPolicyInput{
		UserName:       aws.String(r.UserName),
		PolicyName:     aws.String(r.PolicyName),
		PolicyDocument: aws.String(document),
	}
	if _, err := client.PutUserPolicy(ctx, in); err != nil {
		return fmt.Errorf("put user policy: %w", err)
	}
	return nil
}

func userPolicyDocumentDrifted(observed *UserPolicyOutput, desired string) bool {
	return observed != nil && runtime.Changed(observed.PolicyDocument, desired)
}

func (r *UserPolicy) read(
	ctx context.Context, client *iam.Client, created bool,
) (*UserPolicyOutput, error) {
	var out *UserPolicyOutput
	what := fmt.Sprintf("user policy %s on user %s", r.PolicyName, r.UserName)
	err := wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		resp, err := client.GetUserPolicy(ctx, &iam.GetUserPolicyInput{
			UserName:   aws.String(r.UserName),
			PolicyName: aws.String(r.PolicyName),
		})
		if err != nil {
			if isNotFound(err) {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			return false, fmt.Errorf("get user policy: %w", err)
		}
		if resp == nil || resp.PolicyDocument == nil {
			if created {
				return false, nil
			}
			return false, runtime.ErrNotFound
		}
		document, err := decodeAndNormalizeUserPolicy(aws.ToString(resp.PolicyDocument))
		if err != nil {
			return false, err
		}
		out = &UserPolicyOutput{
			UserName:       r.outputUserName(resp),
			PolicyName:     r.outputPolicyName(resp),
			PolicyDocument: document,
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *UserPolicy) outputUserName(resp *iam.GetUserPolicyOutput) string {
	if resp.UserName != nil {
		return aws.ToString(resp.UserName)
	}
	return r.UserName
}

func (r *UserPolicy) outputPolicyName(resp *iam.GetUserPolicyOutput) string {
	if resp.PolicyName != nil {
		return aws.ToString(resp.PolicyName)
	}
	return r.PolicyName
}

func decodeAndNormalizeUserPolicy(encoded string) (string, error) {
	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		return "", fmt.Errorf("decode user policy document: %w", err)
	}
	document, err := normalizeIAMPolicyJSON(decoded)
	if err != nil {
		return "", fmt.Errorf("normalize user policy document: %w", err)
	}
	return document, nil
}
