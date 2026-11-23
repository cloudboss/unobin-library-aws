package iam

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/cloudboss/unobin/pkg/constraint"
)

type RoleDataSource struct {
	RoleName string `ub:"role-name"`
}

type RoleDataSourceOutput struct {
	RoleName                 string            `ub:"role-name"`
	Arn                      string            `ub:"arn"`
	Path                     string            `ub:"path"`
	RoleID                   string            `ub:"role-id"`
	Description              string            `ub:"description"`
	CreateDate               string            `ub:"create-date"`
	AssumeRolePolicyDocument string            `ub:"assume-role-policy-document"`
	MaxSessionDuration       int64             `ub:"max-session-duration"`
	PermissionsBoundary      *string           `ub:"permissions-boundary"`
	RoleLastUsed             *RoleLastUsed     `ub:"role-last-used"`
	Tags                     map[string]string `ub:"tags"`
}

type RoleLastUsed struct {
	Region       string  `ub:"region"`
	LastUsedDate *string `ub:"last-used-date"`
}

func (r RoleDataSource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.RoleName, nil)).
			Message("role-name is required"),
	}
}

type roleGetter interface {
	GetRole(context.Context, *iam.GetRoleInput, ...func(*iam.Options)) (*iam.GetRoleOutput, error)
}

func (r *RoleDataSource) Read(
	ctx context.Context, cfg *awsCfg,
) (*RoleDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *RoleDataSource) read(
	ctx context.Context, client roleGetter,
) (*RoleDataSourceOutput, error) {
	out, err := client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(r.RoleName)})
	if err != nil {
		return nil, fmt.Errorf("get role %q: %w", r.RoleName, err)
	}
	if out == nil || out.Role == nil {
		return nil, fmt.Errorf("get role %q: no role returned", r.RoleName)
	}
	role := out.Role
	if role.CreateDate == nil {
		return nil, fmt.Errorf("get role %q: missing creation date", r.RoleName)
	}
	policy, err := url.QueryUnescape(aws.ToString(role.AssumeRolePolicyDocument))
	if err != nil {
		return nil, fmt.Errorf("get role %q: decode assume-role-policy-document: %w", r.RoleName, err)
	}

	result := &RoleDataSourceOutput{
		RoleName:                 aws.ToString(role.RoleName),
		Arn:                      aws.ToString(role.Arn),
		Path:                     aws.ToString(role.Path),
		RoleID:                   aws.ToString(role.RoleId),
		Description:              aws.ToString(role.Description),
		CreateDate:               role.CreateDate.Format(time.RFC3339),
		AssumeRolePolicyDocument: policy,
		MaxSessionDuration:       int64(aws.ToInt32(role.MaxSessionDuration)),
		Tags:                     tagsToMap(role.Tags),
	}
	if role.PermissionsBoundary != nil {
		result.PermissionsBoundary = role.PermissionsBoundary.PermissionsBoundaryArn
	}
	if used := role.RoleLastUsed; used != nil {
		result.RoleLastUsed = &RoleLastUsed{Region: aws.ToString(used.Region)}
		if used.LastUsedDate != nil {
			result.RoleLastUsed.LastUsedDate = aws.String(used.LastUsedDate.Format(time.RFC3339))
		}
	}
	if result.Tags == nil {
		result.Tags = map[string]string{}
	}
	maps.DeleteFunc(result.Tags, func(key, _ string) bool {
		return strings.HasPrefix(key, "aws:")
	})
	return result, nil
}
