package iam

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// Role is an IAM role: a named identity, governed by a trust policy, that
// principals assume to receive temporary credentials. The fields mirror the
// IAM CreateRole API. The role name and path fix the role's identity and ARN,
// so a change to either replaces the role; the trust policy, description,
// session limit, permissions boundary, and tags all change in place.
type Role struct {
	RoleName                 string            `ub:"role-name"`
	AssumeRolePolicyDocument string            `ub:"assume-role-policy-document"`
	Path                     *string           `ub:"path"`
	Description              *string           `ub:"description"`
	MaxSessionDuration       *int64            `ub:"max-session-duration"`
	PermissionsBoundary      *string           `ub:"permissions-boundary"`
	Tags                     map[string]string `ub:"tags"`
}

// RoleOutput holds the values IAM computes for a role. The ARN and role id
// identify the role; the role id is the stable, unique handle that survives a
// rename. The create date is the moment IAM recorded the role, in RFC 3339.
type RoleOutput struct {
	Arn        string `ub:"arn"`
	RoleId     string `ub:"role-id"`
	CreateDate string `ub:"create-date"`
}

func (r *Role) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs IAM cannot change on an existing role. The
// name and path are baked into the role's ARN at creation, so changing either
// requires a new role. Every other input is reconciled in place by Update.
func (r *Role) ReplaceFields() []string {
	return []string{
		"role-name",
		"path",
	}
}

// Defaults marks the collection inputs a role may omit.
func (r Role) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the bounds IAM places on a role's inputs. The maximum
// session duration, when set, runs from one hour to twelve hours, expressed in
// seconds; IAM rejects anything outside that window.
func (r Role) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.MaxSessionDuration)).
			Require(constraint.AtLeast(r.MaxSessionDuration, 3600),
				constraint.AtMost(r.MaxSessionDuration, 43200)).
			Message("max-session-duration must be between 3600 and 43200 seconds"),
	}
}

func (r *Role) Create(ctx context.Context, cfg *awsCfg) (*RoleOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &iam.CreateRoleInput{
		RoleName:                 aws.String(r.RoleName),
		AssumeRolePolicyDocument: aws.String(r.AssumeRolePolicyDocument),
		Path:                     r.Path,
		Description:              r.Description,
		MaxSessionDuration:       ptr.Int32(r.MaxSessionDuration),
		PermissionsBoundary:      r.PermissionsBoundary,
		Tags:                     iamRoleTags(r.Tags),
	}
	// A trust policy that names a just-created principal, or a concurrent IAM
	// change, makes CreateRole fail transiently until the change propagates, so
	// retry through those.
	createRole := func() error {
		return retry.OnError(ctx, iamRoleCreateRetryable,
			func(ctx context.Context) error {
				_, err := client.CreateRole(ctx, in)
				return err
			})
	}
	err = createRole()
	// Some partitions, such as the ISO partitions, cannot tag a role as it is
	// created. When the tagged create fails for that reason, create the role
	// without tags and apply them with a separate call below.
	taggedSeparately := false
	if err != nil && in.Tags != nil &&
		partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		err = createRole()
	}
	if err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}
	if taggedSeparately && len(r.Tags) > 0 {
		if err := r.syncTags(ctx, client); err != nil {
			return nil, err
		}
	}
	// Read settles the eventual consistency that follows a create: it waits for
	// the role to become visible and for its ARN to take its final form, which
	// is what belongs in the output.
	return r.read(ctx, client, true)
}

func (r *Role) Read(ctx context.Context, cfg *awsCfg, prior *RoleOutput) (*RoleOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, false)
}

// read fetches the role and returns its computed outputs. Just after a create
// IAM is eventually consistent in two ways this absorbs: the role can read as
// absent, and it can come back with an ARN still in its unique-id form
// (AROA...) rather than the real arn:aws:iam::...:role/name. When created is
// true the role was just made, so a missing role means it has not propagated
// yet and read waits; otherwise a missing role is drift and maps to
// runtime.ErrNotFound. In both cases read waits for a well-formed ARN before
// returning, so the unique-id placeholder never reaches the output.
func (r *Role) read(
	ctx context.Context, client *iam.Client, created bool,
) (*RoleOutput, error) {
	var role *iamtypes.Role
	probe := func(ctx context.Context) (bool, error) {
		resp, err := client.GetRole(ctx, &iam.GetRoleInput{
			RoleName: aws.String(r.RoleName),
		})
		if err != nil {
			if isNotFound(err) {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			return false, fmt.Errorf("get role: %w", err)
		}
		role = resp.Role
		return arn.IsARN(aws.ToString(role.Arn)), nil
	}
	// On a create the ARN can flap between replicas, so require it well-formed
	// on a few consecutive reads before trusting it; a steady-state read takes
	// the first well-formed ARN, since by then it has settled.
	what := fmt.Sprintf("role %s", r.RoleName)
	var err error
	if created {
		err = wait.UntilStable(ctx, what, 5, probe)
	} else {
		err = wait.Until(ctx, what, probe)
	}
	if err != nil {
		return nil, err
	}
	return iamRoleOutput(role), nil
}

func (r *Role) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Role, *RoleOutput],
) (*RoleOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.AssumeRolePolicyDocument, r.AssumeRolePolicyDocument) {
		// As with create, a trust policy naming a just-created principal can be
		// rejected until that principal propagates, so retry through it.
		err := retry.OnError(ctx, isUnpropagatedPrincipal,
			func(ctx context.Context) error {
				_, err := client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
					RoleName:       aws.String(r.RoleName),
					PolicyDocument: aws.String(r.AssumeRolePolicyDocument),
				})
				return err
			})
		if err != nil {
			return nil, fmt.Errorf("update assume role policy: %w", err)
		}
	}
	// The description and session limit are reconciled only when present: a nil
	// value is never sent, so a removed one keeps its last applied value, and an
	// explicit empty description is the way to clear one. The permissions
	// boundary differs because IAM gives it its own delete call, so its removal
	// below is a real detach.
	if runtime.Changed(prior.Inputs.Description, r.Description) && r.Description != nil {
		_, err := client.UpdateRole(ctx, &iam.UpdateRoleInput{
			RoleName:    aws.String(r.RoleName),
			Description: r.Description,
		})
		if err != nil {
			return nil, fmt.Errorf("update role description: %w", err)
		}
	}
	if runtime.Changed(prior.Inputs.MaxSessionDuration, r.MaxSessionDuration) &&
		r.MaxSessionDuration != nil {
		_, err := client.UpdateRole(ctx, &iam.UpdateRoleInput{
			RoleName:           aws.String(r.RoleName),
			MaxSessionDuration: ptr.Int32(r.MaxSessionDuration),
		})
		if err != nil {
			return nil, fmt.Errorf("update role max session duration: %w", err)
		}
	}
	if runtime.Changed(prior.Inputs.PermissionsBoundary, r.PermissionsBoundary) {
		if r.PermissionsBoundary != nil {
			_, err := client.PutRolePermissionsBoundary(ctx,
				&iam.PutRolePermissionsBoundaryInput{
					RoleName:            aws.String(r.RoleName),
					PermissionsBoundary: r.PermissionsBoundary,
				})
			if err != nil {
				return nil, fmt.Errorf("put role permissions boundary: %w", err)
			}
		} else {
			_, err := client.DeleteRolePermissionsBoundary(ctx,
				&iam.DeleteRolePermissionsBoundaryInput{
					RoleName: aws.String(r.RoleName),
				})
			if err != nil {
				return nil, fmt.Errorf("delete role permissions boundary: %w", err)
			}
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client); err != nil {
			return nil, err
		}
	}
	// The outputs -- ARN, role id, create date -- are fixed when the role is
	// created and an update never changes them, so the prior outputs still
	// describe the role. There is nothing fresh to read.
	return prior.Outputs, nil
}

func (r *Role) Delete(ctx context.Context, cfg *awsCfg, prior *RoleOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// IAM refuses to delete a role still attached to an instance profile, so
	// detach it from any profile first. A profile that has since vanished is
	// already detached and is not an error.
	if err := iamRoleDetachInstanceProfiles(ctx, client, r.RoleName); err != nil {
		return err
	}
	// Just after the detach, IAM can still report the role as in use, so retry
	// the delete through that conflict.
	err = retry.OnError(ctx, isDeleteConflict,
		func(ctx context.Context) error {
			_, err := client.DeleteRole(ctx, &iam.DeleteRoleInput{
				RoleName: aws.String(r.RoleName),
			})
			return err
		})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete role: %w", err)
	}
	return nil
}

// syncTags reconciles the role's tags with the desired set, reading the live
// tags through the paginated ListRoleTags and writing changes with TagRole and
// UntagRole. IAM addresses role tags by role name.
func (r *Role) syncTags(ctx context.Context, client *iam.Client) error {
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			current := map[string]string{}
			pager := iam.NewListRoleTagsPaginator(client, &iam.ListRoleTagsInput{
				RoleName: aws.String(r.RoleName),
			})
			for pager.HasMorePages() {
				page, err := pager.NextPage(ctx)
				if err != nil {
					return nil, fmt.Errorf("list role tags: %w", err)
				}
				for _, t := range page.Tags {
					current[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagRole(ctx, &iam.TagRoleInput{
				RoleName: aws.String(r.RoleName),
				Tags:     iamRoleTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag role: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagRole(ctx, &iam.UntagRoleInput{
				RoleName: aws.String(r.RoleName),
				TagKeys:  remove,
			}); err != nil {
				return fmt.Errorf("untag role: %w", err)
			}
			return nil
		},
	)
}

// iamRoleDetachInstanceProfiles removes the role from every instance profile it
// belongs to so the role can be deleted. A profile already gone since the page
// was read counts as detached.
func iamRoleDetachInstanceProfiles(ctx context.Context, client *iam.Client, roleName string) error {
	pager := iam.NewListInstanceProfilesForRolePaginator(client,
		&iam.ListInstanceProfilesForRoleInput{RoleName: aws.String(roleName)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list instance profiles for role: %w", err)
		}
		for _, profile := range page.InstanceProfiles {
			_, err := client.RemoveRoleFromInstanceProfile(ctx,
				&iam.RemoveRoleFromInstanceProfileInput{
					InstanceProfileName: profile.InstanceProfileName,
					RoleName:            aws.String(roleName),
				})
			if err != nil && !isNotFound(err) {
				return fmt.Errorf("remove role from instance profile: %w", err)
			}
		}
	}
	return nil
}

// iamRoleCreateRetryable reports whether a CreateRole error is one that
// clears on its own: a trust policy naming a principal that has not propagated
// yet, or a concurrent change to IAM.
func iamRoleCreateRetryable(err error) bool {
	return isUnpropagatedPrincipal(err) || isConcurrentModification(err)
}

// iamRoleTags converts a desired tag map into the IAM SDK tag list.
func iamRoleTags(tags map[string]string) []iamtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]iamtypes.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, iamtypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}

// iamRoleOutput maps an IAM role record to the computed output struct.
func iamRoleOutput(role *iamtypes.Role) *RoleOutput {
	return &RoleOutput{
		Arn:        aws.ToString(role.Arn),
		RoleId:     aws.ToString(role.RoleId),
		CreateDate: aws.ToTime(role.CreateDate).Format(time.RFC3339),
	}
}
