package iam

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type serviceLinkedRoleClock struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

type serviceLinkedRoleBackoff struct {
	clock    serviceLinkedRoleClock
	deadline time.Time
	delay    time.Duration
	expired  bool
}

func (b *serviceLinkedRoleBackoff) wait(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !b.clock.now().Before(b.deadline) {
		if b.expired {
			return false, nil
		}
		b.expired = true
	}
	if err := b.clock.sleep(ctx, b.delay); err != nil {
		return false, err
	}
	b.delay = min(2*b.delay, 10*time.Second)
	return true, nil
}

type serviceLinkedRoleDeletionClient interface {
	DeleteServiceLinkedRole(context.Context, *iam.DeleteServiceLinkedRoleInput,
		...func(*iam.Options)) (*iam.DeleteServiceLinkedRoleOutput, error)
	GetServiceLinkedRoleDeletionStatus(context.Context, *iam.GetServiceLinkedRoleDeletionStatusInput,
		...func(*iam.Options)) (*iam.GetServiceLinkedRoleDeletionStatusOutput, error)
}

func realServiceLinkedRoleClock() serviceLinkedRoleClock {
	return serviceLinkedRoleClock{now: time.Now, sleep: func(
		ctx context.Context, d time.Duration,
	) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}}
}

func (r *ServiceLinkedRoleResource) createServiceLinkedRole(
	ctx context.Context, client *iam.Client, clock serviceLinkedRoleClock,
) (*ServiceLinkedRoleResourceOutput, error) {
	request, err := r.prepare("")
	if err != nil {
		return nil, err
	}
	backoff := serviceLinkedRoleBackoff{
		clock: clock, deadline: clock.now().Add(2 * time.Minute), delay: 500 * time.Millisecond,
	}
	var result *iam.CreateServiceLinkedRoleOutput
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err = client.CreateServiceLinkedRole(ctx, request.create)
		if err == nil {
			break
		}
		var api smithy.APIError
		if !errors.As(err, &api) || api.ErrorCode() != "AccessDenied" {
			return nil, fmt.Errorf("create service-linked role: %w", err)
		}
		again, waitErr := backoff.wait(ctx)
		if waitErr != nil {
			return nil, waitErr
		}
		if !again {
			return nil, fmt.Errorf("create service-linked role: %w", err)
		}
	}
	if result == nil || result.Role == nil || aws.ToString(result.Role.Arn) == "" {
		return nil, errors.New("create service-linked role: response holds no role ARN")
	}
	identity := aws.ToString(result.Role.Arn)
	parsed, err := r.prepare(identity)
	if err != nil {
		return nil, err
	}
	if parsed.name == "" {
		return nil, fmt.Errorf("create service-linked role %s: ARN has no role name", identity)
	}
	if err := syncServiceLinkedRoleTags(ctx, client, parsed.name, nil,
		ptr.Value(r.Tags)); err != nil {
		return nil, cleanupFailedServiceLinkedRoleCreate(ctx, client, identity, clock, err)
	}
	out, err := readServiceLinkedRole(ctx, client, identity, true, clock)
	if err != nil {
		return nil, cleanupFailedServiceLinkedRoleCreate(ctx, client, identity, clock, err)
	}
	return out, nil
}

func cleanupFailedServiceLinkedRoleCreate(
	ctx context.Context, client serviceLinkedRoleDeletionClient, identity string,
	clock serviceLinkedRoleClock, failure error,
) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if err := deleteServiceLinkedRole(cleanupCtx, client, identity, clock); err != nil {
		return errors.Join(failure,
			fmt.Errorf("compensating delete service-linked role %s: %w", identity, err))
	}
	return failure
}

func readServiceLinkedRole(
	ctx context.Context, client *iam.Client, identity string, created bool,
	clock serviceLinkedRoleClock,
) (*ServiceLinkedRoleResourceOutput, error) {
	if identity == "" {
		return nil, errors.New("service-linked role has no persisted ARN")
	}
	parsed, err := (ServiceLinkedRoleResource{}).prepare(identity)
	if err != nil {
		return nil, err
	}
	backoff := serviceLinkedRoleBackoff{
		clock: clock, deadline: clock.now().Add(2 * time.Minute), delay: 500 * time.Millisecond,
	}
	var result *iam.GetRoleOutput
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err = client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(parsed.name)})
		if err == nil && result != nil && result.Role != nil {
			break
		}
		if err != nil && !isNotFound(err) {
			return nil, fmt.Errorf("get service-linked role %s: %w", identity, err)
		}
		if !created {
			return nil, fmt.Errorf("get service-linked role %s: %w", identity,
				errors.Join(runtime.ErrNotFound, err))
		}
		again, waitErr := backoff.wait(ctx)
		if waitErr != nil {
			return nil, waitErr
		}
		if !again {
			return nil, fmt.Errorf("get service-linked role %s: %w", identity,
				errors.Join(runtime.ErrNotFound, err))
		}
	}
	tags := map[string]string{}
	pager := iam.NewListRoleTagsPaginator(client, &iam.ListRoleTagsInput{
		RoleName: aws.String(parsed.name),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list service-linked role tags %s: %w", identity, err)
		}
		for _, tag := range page.Tags {
			key := aws.ToString(tag.Key)
			if !strings.HasPrefix(key, "aws:") {
				tags[key] = aws.ToString(tag.Value)
			}
		}
	}
	role := result.Role
	return &ServiceLinkedRoleResourceOutput{
		Arn: identity, RoleName: aws.ToString(role.RoleName), RoleId: aws.ToString(role.RoleId),
		Path: aws.ToString(role.Path), CreateDate: aws.ToTime(role.CreateDate).Format(time.RFC3339),
		Description: aws.ToString(role.Description), Tags: tags,
		AWSServiceName: parsed.service, CustomSuffix: parsed.suffix,
	}, nil
}

func syncServiceLinkedRoleTags(
	ctx context.Context, client *iam.Client, name string, current, desired map[string]string,
) error {
	upsert, remove := tagsync.Diff(current, desired)
	if len(remove) > 0 {
		_, err := client.UntagRole(ctx, &iam.UntagRoleInput{
			RoleName: aws.String(name), TagKeys: remove,
		})
		if err != nil {
			return fmt.Errorf("untag service-linked role %s: %w", name, err)
		}
	}
	if len(upsert) > 0 {
		_, err := client.TagRole(ctx, &iam.TagRoleInput{
			RoleName: aws.String(name), Tags: iamRoleTags(upsert),
		})
		if err != nil {
			return fmt.Errorf("tag service-linked role %s: %w", name, err)
		}
	}
	return nil
}

func deleteServiceLinkedRole(
	ctx context.Context, client serviceLinkedRoleDeletionClient, identity string,
	clock serviceLinkedRoleClock,
) error {
	if identity == "" {
		return errors.New("service-linked role has no persisted ARN")
	}
	parsed, err := (ServiceLinkedRoleResource{}).prepare(identity)
	if err != nil {
		return err
	}
	result, err := client.DeleteServiceLinkedRole(ctx, &iam.DeleteServiceLinkedRoleInput{
		RoleName: aws.String(parsed.name),
	})
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete service-linked role %s: %w", identity, err)
	}
	if result == nil || aws.ToString(result.DeletionTaskId) == "" {
		return nil
	}
	backoff := serviceLinkedRoleBackoff{
		clock: clock, deadline: clock.now().Add(5 * time.Minute), delay: 200 * time.Millisecond,
	}
	if err := clock.sleep(ctx, 10*time.Second); err != nil {
		return err
	}
	misses := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		refreshCtx, cancel := context.WithTimeout(ctx, backoff.deadline.Sub(clock.now()))
		status, err := client.GetServiceLinkedRoleDeletionStatus(refreshCtx,
			&iam.GetServiceLinkedRoleDeletionStatusInput{DeletionTaskId: result.DeletionTaskId})
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			if !clock.now().Before(backoff.deadline) {
				return fmt.Errorf("timed out waiting for service-linked role %s deletion", identity)
			}
			return ctx.Err()
		}
		if err != nil && !isNotFound(err) {
			return fmt.Errorf("get deletion status for service-linked role %s: %w", identity, err)
		}
		if isNotFound(err) || status == nil || reflect.ValueOf(*status).IsZero() {
			misses++
			if misses > 20 {
				return fmt.Errorf("service-linked role %s deletion task missing after 20 retries", identity)
			}
		} else {
			misses = 0
			switch status.Status {
			case "SUCCEEDED":
				return nil
			case "NOT_STARTED", "IN_PROGRESS":
			default:
				detail := ""
				if status.Reason != nil {
					detail = aws.ToString(status.Reason.Reason)
					for _, usage := range status.Reason.RoleUsageList {
						detail += fmt.Sprintf("; region %s resources %v",
							aws.ToString(usage.Region), usage.Resources)
					}
				}
				return fmt.Errorf("service-linked role %s deletion %s: %s", identity, status.Status, detail)
			}
		}
		again, err := backoff.wait(ctx)
		if err != nil {
			return err
		}
		if !again {
			return fmt.Errorf("timed out waiting for service-linked role %s deletion", identity)
		}
	}
}
