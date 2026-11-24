package iam

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	sdk "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type ServiceLinkedRoleResource struct {
	AWSServiceName string             `ub:"aws-service-name"`
	CustomSuffix   *string            `ub:"custom-suffix"`
	Description    *string            `ub:"description"`
	Tags           *map[string]string `ub:"tags"`
}

type ServiceLinkedRoleResourceOutput struct {
	Arn            string            `ub:"arn"`
	RoleName       string            `ub:"role-name"`
	RoleId         string            `ub:"role-id"`
	Path           string            `ub:"path"`
	CreateDate     string            `ub:"create-date"`
	Description    string            `ub:"description"`
	Tags           map[string]string `ub:"tags"`
	AWSServiceName string            `ub:"aws-service-name"`
	CustomSuffix   string            `ub:"custom-suffix"`
}

func (r ServiceLinkedRoleResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.IsTrue(true)).
			Require(constraint.NotEquals(r.AWSServiceName, nil)).
			Message("aws-service-name is required"),
		constraint.When(constraint.IsTrue(true)).
			Require(constraint.IsMatch(r.AWSServiceName, `\.`)).
			Message("aws-service-name must contain a dot"),
	}
}

func (r *ServiceLinkedRoleResource) ResourceDefinition() runtime.ResourceDefinition[
	ServiceLinkedRoleResource, *ServiceLinkedRoleResourceOutput, *awsCfg,
] {
	service := runtime.InputField(func(r *ServiceLinkedRoleResource) *string {
		return &r.AWSServiceName
	})
	suffix := runtime.InputField(func(r *ServiceLinkedRoleResource) **string {
		return &r.CustomSuffix
	})
	description := runtime.InputField(func(r *ServiceLinkedRoleResource) **string {
		return &r.Description
	})
	tags := runtime.InputField(func(r *ServiceLinkedRoleResource) **map[string]string {
		return &r.Tags
	})
	return runtime.ResourceDefinition[
		ServiceLinkedRoleResource, *ServiceLinkedRoleResourceOutput, *awsCfg,
	]{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[ServiceLinkedRoleResource]{
			runtime.EqualBy(suffix, func(a, b *string) bool { return ptr.Value(a) == ptr.Value(b) }),
			runtime.EqualBy(description, func(a, b *string) bool { return ptr.Value(a) == ptr.Value(b) }),
			runtime.EqualBy(tags, func(a, b *map[string]string) bool {
				upsert, remove := tagsync.Diff(ptr.Value(a), ptr.Value(b))
				return len(upsert) == 0 && len(remove) == 0
			}),
		},
		Replace: runtime.Replacement[
			ServiceLinkedRoleResource, *ServiceLinkedRoleResourceOutput, *awsCfg,
		]{Fields: []runtime.AnyInputField[ServiceLinkedRoleResource]{service, suffix}},
		StableID: func(
			_ ServiceLinkedRoleResource, out *ServiceLinkedRoleResourceOutput,
		) (string, error) {
			if out == nil {
				return "", nil
			}
			return out.Arn, nil
		},
	}
}

func (r *ServiceLinkedRoleResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*ServiceLinkedRoleResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createServiceLinkedRole(ctx, client, realServiceLinkedRoleClock())
}

func (r *ServiceLinkedRoleResource) Read(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ServiceLinkedRoleResource, *ServiceLinkedRoleResourceOutput, *awsCfg],
) (*ServiceLinkedRoleResourceOutput, error) {
	identity := serviceLinkedRoleIdentity(prior.Outputs)
	if _, err := r.prepare(identity); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return readServiceLinkedRole(ctx, client, identity, false, realServiceLinkedRoleClock())
}

func (r *ServiceLinkedRoleResource) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ServiceLinkedRoleResource, *ServiceLinkedRoleResourceOutput, *awsCfg],
) (*ServiceLinkedRoleResourceOutput, error) {
	identity := serviceLinkedRoleIdentity(prior.Outputs)
	request, err := r.prepare(identity)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	observed := prior.Observed
	if observed == nil {
		observed, err = readServiceLinkedRole(ctx, client, identity, false, realServiceLinkedRoleClock())
		if err != nil {
			return nil, err
		}
	}
	if err := syncServiceLinkedRoleTags(ctx, client, request.name, observed.Tags,
		ptr.Value(r.Tags)); err != nil {
		return nil, err
	}
	if observed.Description != ptr.Value(r.Description) {
		_, err := client.UpdateRole(ctx, &sdk.UpdateRoleInput{
			RoleName: aws.String(request.name), Description: aws.String(ptr.Value(r.Description)),
		})
		if err != nil {
			return nil, fmt.Errorf("update service-linked role %s: %w", identity, err)
		}
	}
	return readServiceLinkedRole(ctx, client, identity, false, realServiceLinkedRoleClock())
}

func (r *ServiceLinkedRoleResource) Delete(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ServiceLinkedRoleResource, *ServiceLinkedRoleResourceOutput, *awsCfg],
) error {
	identity := serviceLinkedRoleIdentity(prior.Outputs)
	if _, err := r.prepare(identity); err != nil {
		return err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return deleteServiceLinkedRole(ctx, client, identity, realServiceLinkedRoleClock())
}

func serviceLinkedRoleIdentity(out *ServiceLinkedRoleResourceOutput) string {
	if out == nil {
		return ""
	}
	return out.Arn
}

func (r ServiceLinkedRoleResource) prepare(identity string) (*serviceLinkedRoleRequest, error) {
	request := &serviceLinkedRoleRequest{
		create: &sdk.CreateServiceLinkedRoleInput{AWSServiceName: aws.String(r.AWSServiceName)},
	}
	if ptr.Value(r.CustomSuffix) != "" {
		request.create.CustomSuffix = r.CustomSuffix
	}
	if ptr.Value(r.Description) != "" {
		request.create.Description = r.Description
	}
	if identity == "" {
		return request, nil
	}
	parsed, err := arn.Parse(identity)
	if err != nil {
		return nil, fmt.Errorf("parse service-linked role ARN %q: %w", identity, err)
	}
	parts := strings.Split(parsed.Resource, "/")
	if len(parts) != 4 {
		return nil, fmt.Errorf("service-linked role ARN %q must have four resource parts", identity)
	}
	request.name, request.service = parts[3], parts[2]
	if suffix := strings.Split(request.name, "_"); len(suffix) == 2 {
		request.suffix = suffix[1]
	}
	return request, nil
}

type serviceLinkedRoleRequest struct {
	create  *sdk.CreateServiceLinkedRoleInput
	name    string
	service string
	suffix  string
}
