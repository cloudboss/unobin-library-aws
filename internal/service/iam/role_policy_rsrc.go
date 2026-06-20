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

// RolePolicy manages an inline policy embedded in an IAM role. The role
// name and policy name form the identity, so a change to either makes a
// different policy and recreates this one; the policy document is the
// permission set and is updated in place. A single PutRolePolicy both
// creates and overwrites the named policy, so Create and Update share it.
//
// IAM validates the role name (1-128 characters, the set [\w+=,.@-], and
// the role's name rather than its ARN) and the policy name (1-128
// characters, the set [\w+=,.@-]). These are left to IAM rather than
// declared as constraints: the character-set rule needs a regex match the
// constraint vocabulary cannot express, the length bound counts bytes
// where IAM counts characters, and the "name not ARN" rule has no field to
// branch on.
type RolePolicy struct {
	RoleName       string `ub:"role-name"`
	PolicyName     string `ub:"policy-name"`
	PolicyDocument string `ub:"policy-document"`
}

// RolePolicyOutput holds the resource's identity and the policy document
// IAM stores. The role name and policy name are echoed so Delete can key
// off the prior outputs when a replace recreates the policy. The document
// is the value IAM returns, URL-decoded, so a downstream reader sees real
// JSON rather than the percent-encoded form IAM hands back.
type RolePolicyOutput struct {
	RoleName       string `ub:"role-name"`
	PolicyName     string `ub:"policy-name"`
	PolicyDocument string `ub:"policy-document"`
}

func (r *RolePolicy) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that form the policy's identity. The role
// name and policy name name the inline policy, so a change to either is a
// different policy and recreates this one. The document is updated in place.
func (r *RolePolicy) ReplaceFields() []string {
	return []string{
		"role-name",
		"policy-name",
	}
}

func (r *RolePolicy) Create(ctx context.Context, cfg *awsCfg) (*RolePolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &iam.PutRolePolicyInput{
		RoleName:       aws.String(r.RoleName),
		PolicyName:     aws.String(r.PolicyName),
		PolicyDocument: aws.String(r.PolicyDocument),
	}
	if _, err := client.PutRolePolicy(ctx, in); err != nil {
		return nil, fmt.Errorf("put role policy: %w", err)
	}
	// A role created moments earlier, in the same apply, can briefly be
	// invisible to a read-after-write of its inline policy. Wait for the
	// policy to become readable so the next read does not take it for absent
	// and recreate it, and return the read-back value.
	return r.read(ctx, client, true)
}

func (r *RolePolicy) Read(
	ctx context.Context, cfg *awsCfg, prior *RolePolicyOutput,
) (*RolePolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return (&RolePolicy{
		RoleName:   prior.RoleName,
		PolicyName: prior.PolicyName,
	}).read(ctx, client, false)
}

// read fetches the inline policy and returns its outputs. When created is
// true the policy was just put, so a not-found means the role or policy has
// not propagated yet and read waits for it; otherwise a not-found is drift
// and maps to runtime.ErrNotFound at once. IAM returns NoSuchEntity for a
// missing role as well as a missing policy, so the one code covers both. The
// returned document is URL-percent-encoded and is decoded before output.
func (r *RolePolicy) read(
	ctx context.Context, client *iam.Client, created bool,
) (*RolePolicyOutput, error) {
	var document string
	what := fmt.Sprintf("role policy %s on role %s", r.PolicyName, r.RoleName)
	err := wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		resp, err := client.GetRolePolicy(ctx, &iam.GetRolePolicyInput{
			RoleName:   aws.String(r.RoleName),
			PolicyName: aws.String(r.PolicyName),
		})
		if err != nil {
			if isNotFound(err) {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			return false, fmt.Errorf("get role policy: %w", err)
		}
		if resp == nil || resp.PolicyDocument == nil {
			if created {
				return false, nil
			}
			return false, runtime.ErrNotFound
		}
		decoded, err := url.QueryUnescape(aws.ToString(resp.PolicyDocument))
		if err != nil {
			return false, fmt.Errorf("decode role policy document: %w", err)
		}
		document = decoded
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return &RolePolicyOutput{
		RoleName:       r.RoleName,
		PolicyName:     r.PolicyName,
		PolicyDocument: document,
	}, nil
}

func (r *RolePolicy) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[RolePolicy, *RolePolicyOutput],
) (*RolePolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Only the document is mutable; the role and policy names force a replace.
	// PutRolePolicy overwrites the policy, so reissue it only when the document
	// changed, keeping the update idempotent.
	if runtime.Changed(prior.Inputs.PolicyDocument, r.PolicyDocument) {
		in := &iam.PutRolePolicyInput{
			RoleName:       aws.String(r.RoleName),
			PolicyName:     aws.String(r.PolicyName),
			PolicyDocument: aws.String(r.PolicyDocument),
		}
		if _, err := client.PutRolePolicy(ctx, in); err != nil {
			return nil, fmt.Errorf("put role policy: %w", err)
		}
	}
	return r.read(ctx, client, false)
}

// Delete removes the inline policy from the role. A delete of a policy that
// is already gone returns NoSuchEntity, which is treated as success so the
// delete is idempotent. It keys off the prior outputs so a replace deletes
// the original policy rather than the new one.
func (r *RolePolicy) Delete(ctx context.Context, cfg *awsCfg, prior *RolePolicyOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &iam.DeleteRolePolicyInput{
		RoleName:   aws.String(prior.RoleName),
		PolicyName: aws.String(prior.PolicyName),
	}
	if _, err := client.DeleteRolePolicy(ctx, in); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete role policy: %w", err)
	}
	return nil
}
