package iam

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	userARNStableCount    = 5
	userARNNotFoundChecks = 5
)

// userNameRe matches the IAM user name character set. The constraint layer
// cannot express regular expressions, so Create and Update check it in code.
var userNameRe = regexp.MustCompile(`^[0-9A-Za-z=,.@_+-]+$`)

// User is an IAM user. Name and path change in place through UpdateUser, the
// permissions boundary changes through its own put and delete calls, tags are
// reconciled through the IAM tag calls, and force destroy controls delete-time
// cleanup of dependent IAM credentials and policies.
type User struct {
	Name                string            `ub:"name"`
	Path                string            `ub:"path"`
	PermissionsBoundary *string           `ub:"permissions-boundary"`
	ForceDestroy        bool              `ub:"force-destroy"`
	Tags                map[string]string `ub:"tags"`
}

// UserOutput holds the values IAM reports for a user. Name is the current cloud
// handle, so reads and deletes address a renamed user by its current name.
type UserOutput struct {
	Arn                 string            `ub:"arn"`
	UniqueId            string            `ub:"unique-id"`
	Name                string            `ub:"name"`
	Path                string            `ub:"path"`
	PermissionsBoundary *string           `ub:"permissions-boundary"`
	Tags                map[string]string `ub:"tags"`
}

func (r *User) SchemaVersion() int { return 1 }

// ReplaceFields is empty because IAM can change the user name, path,
// permissions boundary, and tags in place.
func (r *User) ReplaceFields() []string { return nil }

// Defaults gives path and force destroy their resource defaults and marks tags
// optional.
func (r User) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.Path, "/"),
		defaults.Value(r.ForceDestroy, false),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares value rules that can be derived. The name pattern is a
// regular expression, so Create and Update check it in code.
func (r User) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.PermissionsBoundary)).
			Require(constraint.MaxItems(r.PermissionsBoundary, 2048)).
			Message("permissions-boundary must be at most 2048 characters"),
	}
}

func (r *User) Create(ctx context.Context, cfg *awsCfg) (*UserOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &iam.CreateUserInput{
		UserName: aws.String(r.Name),
		Path:     aws.String(r.Path),
		Tags:     userTags(r.Tags),
	}
	if permissionsBoundaryPresent(r.PermissionsBoundary) {
		in.PermissionsBoundary = r.PermissionsBoundary
	}
	var resp *iam.CreateUserOutput
	create := func() error {
		return retry.OnError(ctx, isConcurrentModification,
			func(ctx context.Context) error {
				var err error
				resp, err = client.CreateUser(ctx, in)
				return err
			})
	}
	err = create()
	taggedSeparately := false
	if err != nil && len(in.Tags) > 0 && partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		err = create()
	}
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	if resp == nil || resp.User == nil || aws.ToString(resp.User.UserName) == "" {
		return nil, errors.New("create user: response holds no user name")
	}
	name := aws.ToString(resp.User.UserName)
	if taggedSeparately && len(r.Tags) > 0 {
		if err := tagUser(ctx, client, name, r.Tags); err != nil {
			return nil, err
		}
	}
	return readUser(ctx, client, name, true)
}

func (r *User) Read(ctx context.Context, cfg *awsCfg, prior *UserOutput) (*UserOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return readUser(ctx, client, r.handle(prior), false)
}

func (r *User) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[User, *UserOutput],
) (*UserOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	handle := priorUserName(prior, r.Name)
	updated := false
	if userNameOrPathNeedsUpdate(prior, *r) {
		_, err := client.UpdateUser(ctx, &iam.UpdateUserInput{
			UserName:    aws.String(handle),
			NewUserName: aws.String(r.Name),
			NewPath:     aws.String(r.Path),
		})
		if err != nil {
			return nil, fmt.Errorf("update user: %w", err)
		}
		handle = r.Name
		updated = true
	}
	if userPermissionsBoundaryNeedsUpdate(prior, r.PermissionsBoundary) {
		if permissionsBoundaryPresent(r.PermissionsBoundary) {
			_, err := client.PutUserPermissionsBoundary(ctx,
				&iam.PutUserPermissionsBoundaryInput{
					UserName:            aws.String(handle),
					PermissionsBoundary: r.PermissionsBoundary,
				})
			if err != nil {
				return nil, fmt.Errorf("put user permissions boundary: %w", err)
			}
		} else {
			_, err := client.DeleteUserPermissionsBoundary(ctx,
				&iam.DeleteUserPermissionsBoundaryInput{
					UserName: aws.String(handle),
				})
			if err != nil {
				return nil, fmt.Errorf("delete user permissions boundary: %w", err)
			}
		}
		updated = true
	}
	oldTags := prior.Inputs.Tags
	if prior.Observed != nil {
		oldTags = prior.Observed.Tags
	}
	if userTagsNeedSync(oldTags, r.Tags) {
		if err := syncUserTags(ctx, client, handle, oldTags, r.Tags, true); err != nil {
			return nil, err
		}
		updated = true
	}
	if !updated {
		if prior.Observed != nil {
			return prior.Observed, nil
		}
		return prior.Outputs, nil
	}
	return readUser(ctx, client, handle, false)
}

func (r *User) Delete(ctx context.Context, cfg *awsCfg, prior *UserOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	name := r.handle(prior)
	if err := deleteUserGroupMemberships(ctx, client, name); err != nil {
		return err
	}
	if r.ForceDestroy {
		for _, cleanup := range userForceDestroyCleanups {
			if err := cleanup(ctx, client, name); err != nil {
				if isNotFound(err) || partition.UnsupportedOperation(region(client), err) {
					continue
				}
				return err
			}
		}
	}
	_, err = client.DeleteUser(ctx, &iam.DeleteUserInput{UserName: aws.String(name)})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

func readUser(
	ctx context.Context, client *iam.Client, name string, created bool,
) (*UserOutput, error) {
	user, err := readUserRecord(ctx, client, name, created)
	if err != nil {
		return nil, err
	}
	if arn.IsARN(aws.ToString(user.Arn)) {
		return userOutput(user), nil
	}
	user, err = waitUserARN(ctx, client, name)
	if err != nil {
		return nil, err
	}
	return userOutput(user), nil
}

func readUserRecord(
	ctx context.Context, client *iam.Client, name string, created bool,
) (*iamtypes.User, error) {
	var user *iamtypes.User
	read := func(ctx context.Context) (bool, error) {
		var err error
		user, err = findUserByName(ctx, client, name)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			return false, err
		}
		return true, nil
	}
	if created {
		err := wait.Until(ctx, fmt.Sprintf("user %s", name), read)
		if err != nil {
			return nil, err
		}
		return user, nil
	}
	ready, err := read(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, runtime.ErrNotFound
	}
	return user, nil
}

func waitUserARN(
	ctx context.Context, client *iam.Client, name string,
) (*iamtypes.User, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
	}
	var user *iamtypes.User
	notFound := 0
	err := wait.UntilStable(ctx, fmt.Sprintf("user %s arn", name), userARNStableCount,
		func(ctx context.Context) (bool, error) {
			candidate, err := findUserByName(ctx, client, name)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					notFound++
					if notFound <= userARNNotFoundChecks {
						return false, nil
					}
					return false, fmt.Errorf("wait for user %s ARN: user was not found", name)
				}
				return false, err
			}
			notFound = 0
			if !arn.IsARN(aws.ToString(candidate.Arn)) {
				return false, nil
			}
			user = candidate
			return true, nil
		},
		wait.WithInterval(time.Second),
		wait.WithTimeout(2*time.Minute),
	)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func findUserByName(ctx context.Context, client *iam.Client, name string) (*iamtypes.User, error) {
	resp, err := client.GetUser(ctx, &iam.GetUserInput{UserName: aws.String(name)})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	if resp == nil || resp.User == nil || aws.ToString(resp.User.UserName) == "" {
		return nil, runtime.ErrNotFound
	}
	return resp.User, nil
}

func (r *User) handle(prior *UserOutput) string {
	if prior != nil && prior.Name != "" {
		return prior.Name
	}
	return r.Name
}

func priorUserName(prior runtime.Prior[User, *UserOutput], fallback string) string {
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

func (r *User) validate() error {
	if !userNameRe.MatchString(r.Name) {
		return errors.New(
			"name must contain only letters, digits, equals, comma, period, " +
				"at sign, underscore, plus, or hyphen")
	}
	if r.PermissionsBoundary != nil && len(*r.PermissionsBoundary) > 2048 {
		return errors.New("permissions-boundary must be at most 2048 characters")
	}
	return nil
}

func permissionsBoundaryPresent(v *string) bool {
	return v != nil && *v != ""
}

func userNameOrPathNeedsUpdate(prior runtime.Prior[User, *UserOutput], current User) bool {
	if prior.Observed != nil {
		return runtime.Changed(prior.Observed.Name, current.Name) ||
			runtime.Changed(prior.Observed.Path, current.Path)
	}
	return runtime.Changed(prior.Inputs.Name, current.Name) ||
		runtime.Changed(prior.Inputs.Path, current.Path)
}

func userPermissionsBoundaryNeedsUpdate(
	prior runtime.Prior[User, *UserOutput], current *string,
) bool {
	desired := desiredUserPermissionsBoundary(current)
	if prior.Observed != nil {
		return !sameOptionalString(prior.Observed.PermissionsBoundary, desired)
	}
	return !sameOptionalString(
		desiredUserPermissionsBoundary(prior.Inputs.PermissionsBoundary), desired)
}

func desiredUserPermissionsBoundary(v *string) *string {
	if !permissionsBoundaryPresent(v) {
		return nil
	}
	out := *v
	return &out
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func userOutput(user *iamtypes.User) *UserOutput {
	return &UserOutput{
		Arn:                 aws.ToString(user.Arn),
		UniqueId:            aws.ToString(user.UserId),
		Name:                aws.ToString(user.UserName),
		Path:                aws.ToString(user.Path),
		PermissionsBoundary: userPermissionsBoundary(user),
		Tags:                userTagsMap(user.Tags),
	}
}

func userPermissionsBoundary(user *iamtypes.User) *string {
	if user.PermissionsBoundary == nil || user.PermissionsBoundary.PermissionsBoundaryArn == nil {
		return nil
	}
	boundary := aws.ToString(user.PermissionsBoundary.PermissionsBoundaryArn)
	return &boundary
}

func userTagsMap(tags []iamtypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, t := range tags {
		key := aws.ToString(t.Key)
		if userSystemTag(key) {
			continue
		}
		out[key] = aws.ToString(t.Value)
	}
	return out
}

func userTags(tags map[string]string) []iamtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		if !userSystemTag(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]iamtypes.Tag, 0, len(keys))
	for _, k := range keys {
		out = append(out, iamtypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}

func syncUserTags(
	ctx context.Context,
	client *iam.Client,
	name string,
	old map[string]string,
	desired map[string]string,
	ignoreUnsupported bool,
) error {
	upsert, remove := userTagDiff(old, desired)
	if len(remove) > 0 {
		_, err := client.UntagUser(ctx, &iam.UntagUserInput{
			UserName: aws.String(name),
			TagKeys:  remove,
		})
		if err != nil && !(ignoreUnsupported && partition.UnsupportedOperation(region(client), err)) {
			return fmt.Errorf("untag user: %w", err)
		}
	}
	if len(upsert) > 0 {
		if err := tagUser(ctx, client, name, upsert); err != nil {
			if ignoreUnsupported && partition.UnsupportedOperation(region(client), err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func tagUser(ctx context.Context, client *iam.Client, name string, tags map[string]string) error {
	apiTags := userTags(tags)
	if len(apiTags) == 0 {
		return nil
	}
	_, err := client.TagUser(ctx, &iam.TagUserInput{
		UserName: aws.String(name),
		Tags:     apiTags,
	})
	if err != nil {
		return fmt.Errorf("tag user: %w", err)
	}
	return nil
}

func userTagsNeedSync(old map[string]string, desired map[string]string) bool {
	upsert, remove := userTagDiff(old, desired)
	return len(upsert) > 0 || len(remove) > 0
}

func userTagDiff(old map[string]string, desired map[string]string) (map[string]string, []string) {
	upsert := make(map[string]string)
	for k, v := range desired {
		if userSystemTag(k) {
			continue
		}
		if oldValue, ok := old[k]; !ok || oldValue != v {
			upsert[k] = v
		}
	}
	var remove []string
	for k := range old {
		if userSystemTag(k) {
			continue
		}
		if _, ok := desired[k]; !ok {
			remove = append(remove, k)
		}
	}
	sort.Strings(remove)
	return upsert, remove
}

func userSystemTag(key string) bool {
	return strings.HasPrefix(key, "aws:")
}

type userCleanup func(context.Context, *iam.Client, string) error

var userForceDestroyCleanups = []userCleanup{
	deleteUserPolicies,
	detachUserPolicies,
	deleteUserAccessKeys,
	deleteUserSSHKeys,
	deleteUserVirtualMFADevices,
	deactivateUserMFADevices,
	deleteUserLoginProfile,
	deleteUserSigningCertificates,
	deleteUserServiceSpecificCredentials,
}

func deleteUserGroupMemberships(ctx context.Context, client *iam.Client, name string) error {
	pager := iam.NewListGroupsForUserPaginator(client,
		&iam.ListGroupsForUserInput{UserName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list groups for user: %w", err)
		}
		for _, group := range page.Groups {
			_, err := client.RemoveUserFromGroup(ctx, &iam.RemoveUserFromGroupInput{
				GroupName: group.GroupName,
				UserName:  aws.String(name),
			})
			if err != nil && !isNotFound(err) {
				return fmt.Errorf("remove user from group: %w", err)
			}
		}
	}
	return nil
}

func deleteUserPolicies(ctx context.Context, client *iam.Client, name string) error {
	pager := iam.NewListUserPoliciesPaginator(client,
		&iam.ListUserPoliciesInput{UserName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list user policies: %w", err)
		}
		for _, policyName := range page.PolicyNames {
			_, err := client.DeleteUserPolicy(ctx, &iam.DeleteUserPolicyInput{
				UserName:   aws.String(name),
				PolicyName: aws.String(policyName),
			})
			if err != nil && !isNotFound(err) {
				return fmt.Errorf("delete user policy: %w", err)
			}
		}
	}
	return nil
}

func detachUserPolicies(ctx context.Context, client *iam.Client, name string) error {
	pager := iam.NewListAttachedUserPoliciesPaginator(client,
		&iam.ListAttachedUserPoliciesInput{UserName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list attached user policies: %w", err)
		}
		for _, policy := range page.AttachedPolicies {
			err := detachUserPolicy(ctx, client, name, aws.ToString(policy.PolicyArn))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func detachUserPolicy(
	ctx context.Context, client *iam.Client, name string, policyArn string,
) error {
	err := retry.OnError(ctx, isConcurrentModification,
		func(ctx context.Context) error {
			_, err := client.DetachUserPolicy(ctx, &iam.DetachUserPolicyInput{
				UserName:  aws.String(name),
				PolicyArn: aws.String(policyArn),
			})
			if isNotFound(err) {
				return nil
			}
			return err
		})
	if err != nil {
		return fmt.Errorf("detach user policy: %w", err)
	}
	return nil
}

func deleteUserAccessKeys(ctx context.Context, client *iam.Client, name string) error {
	pager := iam.NewListAccessKeysPaginator(client,
		&iam.ListAccessKeysInput{UserName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list access keys: %w", err)
		}
		for _, key := range page.AccessKeyMetadata {
			_, err := client.DeleteAccessKey(ctx, &iam.DeleteAccessKeyInput{
				UserName:    aws.String(name),
				AccessKeyId: key.AccessKeyId,
			})
			if err != nil && !isNotFound(err) {
				return fmt.Errorf("delete access key: %w", err)
			}
		}
	}
	return nil
}

func deleteUserSSHKeys(ctx context.Context, client *iam.Client, name string) error {
	pager := iam.NewListSSHPublicKeysPaginator(client,
		&iam.ListSSHPublicKeysInput{UserName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list ssh public keys: %w", err)
		}
		for _, key := range page.SSHPublicKeys {
			_, err := client.DeleteSSHPublicKey(ctx, &iam.DeleteSSHPublicKeyInput{
				UserName:       aws.String(name),
				SSHPublicKeyId: key.SSHPublicKeyId,
			})
			if err != nil && !isNotFound(err) {
				return fmt.Errorf("delete ssh public key: %w", err)
			}
		}
	}
	return nil
}

func deleteUserVirtualMFADevices(ctx context.Context, client *iam.Client, name string) error {
	pager := iam.NewListVirtualMFADevicesPaginator(client,
		&iam.ListVirtualMFADevicesInput{
			AssignmentStatus: iamtypes.AssignmentStatusTypeAssigned,
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list virtual mfa devices: %w", err)
		}
		for _, device := range page.VirtualMFADevices {
			if device.User == nil || aws.ToString(device.User.UserName) != name {
				continue
			}
			serial := aws.ToString(device.SerialNumber)
			if err := deactivateMFADevice(ctx, client, name, serial); err != nil {
				return err
			}
			_, err := client.DeleteVirtualMFADevice(ctx, &iam.DeleteVirtualMFADeviceInput{
				SerialNumber: aws.String(serial),
			})
			if err != nil && !isNotFound(err) {
				return fmt.Errorf("delete virtual mfa device: %w", err)
			}
		}
	}
	return nil
}

func deactivateUserMFADevices(ctx context.Context, client *iam.Client, name string) error {
	pager := iam.NewListMFADevicesPaginator(client,
		&iam.ListMFADevicesInput{UserName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list mfa devices: %w", err)
		}
		for _, device := range page.MFADevices {
			if err := deactivateMFADevice(ctx, client, name,
				aws.ToString(device.SerialNumber)); err != nil {
				return err
			}
		}
	}
	return nil
}

func deactivateMFADevice(
	ctx context.Context, client *iam.Client, name string, serial string,
) error {
	_, err := client.DeactivateMFADevice(ctx, &iam.DeactivateMFADeviceInput{
		UserName:     aws.String(name),
		SerialNumber: aws.String(serial),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("deactivate mfa device: %w", err)
	}
	return nil
}

func deleteUserLoginProfile(ctx context.Context, client *iam.Client, name string) error {
	err := retry.OnError(ctx, isEntityTemporarilyUnmodifiable,
		func(ctx context.Context) error {
			_, err := client.DeleteLoginProfile(ctx, &iam.DeleteLoginProfileInput{
				UserName: aws.String(name),
			})
			if isNotFound(err) {
				return nil
			}
			return err
		})
	if err != nil {
		return fmt.Errorf("delete login profile: %w", err)
	}
	return nil
}

func deleteUserSigningCertificates(ctx context.Context, client *iam.Client, name string) error {
	pager := iam.NewListSigningCertificatesPaginator(client,
		&iam.ListSigningCertificatesInput{UserName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list signing certificates: %w", err)
		}
		for _, certificate := range page.Certificates {
			_, err := client.DeleteSigningCertificate(ctx,
				&iam.DeleteSigningCertificateInput{
					UserName:      aws.String(name),
					CertificateId: certificate.CertificateId,
				})
			if err != nil && !isNotFound(err) {
				return fmt.Errorf("delete signing certificate: %w", err)
			}
		}
	}
	return nil
}

func deleteUserServiceSpecificCredentials(
	ctx context.Context, client *iam.Client, name string,
) error {
	var marker *string
	for {
		page, err := client.ListServiceSpecificCredentials(ctx,
			&iam.ListServiceSpecificCredentialsInput{
				UserName: aws.String(name),
				Marker:   marker,
			})
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list service-specific credentials: %w", err)
		}
		for _, credential := range page.ServiceSpecificCredentials {
			_, err := client.DeleteServiceSpecificCredential(ctx,
				&iam.DeleteServiceSpecificCredentialInput{
					UserName:                    aws.String(name),
					ServiceSpecificCredentialId: credential.ServiceSpecificCredentialId,
				})
			if err != nil && !isNotFound(err) {
				return fmt.Errorf("delete service-specific credential: %w", err)
			}
		}
		if !page.IsTruncated {
			return nil
		}
		marker = page.Marker
	}
}
