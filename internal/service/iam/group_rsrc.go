package iam

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// groupNameRe matches the IAM group name character set. The constraint layer
// cannot express regular expressions, so Create and Update check it in code.
var groupNameRe = regexp.MustCompile(`^[0-9A-Za-z=,.@_+-]+$`)

// GroupResource is an IAM group. The group name and path are reconciled with
// CreateGroup and UpdateGroup; both can change in place. Path defaults to "/",
// matching IAM's API default, and is always sent on create and update.
type GroupResource struct {
	// Name is the IAM group name. It must contain only letters, digits,
	// equals, comma, period, at sign, underscore, plus, or hyphen.
	Name string `ub:"name"`
	Path string `ub:"path"`
}

// GroupResourceOutput holds the values IAM reports for a group. Name is the current
// cloud handle, so reads and deletes keep addressing a renamed group by its
// current name instead of the desired name from a later plan.
type GroupResourceOutput struct {
	Arn      string `ub:"arn"`
	UniqueId string `ub:"unique-id"`
	Name     string `ub:"name"`
}

func (r *GroupResource) SchemaVersion() int { return 1 }

// ReplaceFields is empty because IAM can change both name and path in place
// through UpdateGroup.
func (r *GroupResource) ReplaceFields() []string { return nil }

// Defaults gives path the IAM default value.
func (r GroupResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.Path, "/"),
	}
}

func (r *GroupResource) Create(ctx context.Context, cfg *awsCfg) (*GroupResourceOutput, error) {
	if err := validateGroupName(r.Name); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateGroup(ctx, &iam.CreateGroupInput{
		GroupName: aws.String(r.Name),
		Path:      aws.String(r.Path),
	})
	if err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	if resp == nil || resp.Group == nil || aws.ToString(resp.Group.GroupName) == "" {
		return nil, errors.New("create group: response holds no group name")
	}
	return readGroup(ctx, client, aws.ToString(resp.Group.GroupName), true)
}

func (r *GroupResource) Read(
	ctx context.Context, cfg *awsCfg, prior *GroupResourceOutput) (*GroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return readGroup(ctx, client, r.handle(prior), false)
}

func (r *GroupResource) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[GroupResource, *GroupResourceOutput],
) (*GroupResourceOutput, error) {
	if err := validateGroupName(r.Name); err != nil {
		return nil, err
	}
	if !groupNeedsUpdate(prior, *r) {
		if prior.Observed != nil {
			return prior.Observed, nil
		}
		return prior.Outputs, nil
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_, err = client.UpdateGroup(ctx, &iam.UpdateGroupInput{
		GroupName:    aws.String(priorGroupName(prior, r.Name)),
		NewGroupName: aws.String(r.Name),
		NewPath:      aws.String(r.Path),
	})
	if err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}
	return readGroup(ctx, client, r.Name, false)
}

func (r *GroupResource) Delete(ctx context.Context, cfg *awsCfg, prior *GroupResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteGroup(ctx, &iam.DeleteGroupInput{
		GroupName: aws.String(r.handle(prior)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete group: %w", err)
	}
	return nil
}

func readGroup(
	ctx context.Context, client *iam.Client, name string, created bool,
) (*GroupResourceOutput, error) {
	var group *iamtypes.Group
	if created {
		err := wait.Until(ctx, fmt.Sprintf("group %s", name),
			func(ctx context.Context) (bool, error) {
				var err error
				group, err = findGroupByName(ctx, client, name)
				if err != nil {
					if errors.Is(err, runtime.ErrNotFound) {
						return false, nil
					}
					return false, err
				}
				return true, nil
			})
		if err != nil {
			return nil, err
		}
		return groupOutput(group), nil
	}
	var err error
	group, err = findGroupByName(ctx, client, name)
	if err != nil {
		return nil, err
	}
	return groupOutput(group), nil
}

func findGroupByName(
	ctx context.Context, client *iam.Client, name string,
) (*iamtypes.Group, error) {
	resp, err := client.GetGroup(ctx, &iam.GetGroupInput{GroupName: aws.String(name)})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get group: %w", err)
	}
	if resp == nil || resp.Group == nil {
		return nil, runtime.ErrNotFound
	}
	return resp.Group, nil
}

func groupNeedsUpdate(
	prior runtime.Prior[GroupResource, *GroupResourceOutput],
	current GroupResource,
) bool {
	if runtime.Changed(prior.Inputs.Name, current.Name) ||
		runtime.Changed(prior.Inputs.Path, current.Path) {
		return true
	}
	if prior.Outputs == nil || prior.Observed == nil {
		return false
	}
	return runtime.Changed(prior.Outputs.Name, prior.Observed.Name) ||
		runtime.Changed(prior.Outputs.Arn, prior.Observed.Arn)
}

func priorGroupName(
	prior runtime.Prior[GroupResource, *GroupResourceOutput],
	fallback string,
) string {
	if prior.Outputs != nil && prior.Outputs.Name != "" {
		return prior.Outputs.Name
	}
	if prior.Observed != nil && prior.Observed.Name != "" {
		return prior.Observed.Name
	}
	if prior.Inputs.Name != "" {
		return prior.Inputs.Name
	}
	return fallback
}

func (r *GroupResource) handle(prior *GroupResourceOutput) string {
	if prior != nil && prior.Name != "" {
		return prior.Name
	}
	return r.Name
}

func validateGroupName(name string) error {
	if !groupNameRe.MatchString(name) {
		return errors.New(
			"name must contain only letters, digits, equals, comma, period, " +
				"at sign, underscore, plus, or hyphen")
	}
	return nil
}

func groupOutput(group *iamtypes.Group) *GroupResourceOutput {
	return &GroupResourceOutput{
		Arn:      aws.ToString(group.Arn),
		UniqueId: aws.ToString(group.GroupId),
		Name:     aws.ToString(group.GroupName),
	}
}
