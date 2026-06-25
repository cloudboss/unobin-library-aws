// verify checks the IAM resources the scenario applied against the phase named
// in the VERIFY_PHASE environment variable, looking each resource up by its
// stable name because the test driver does not pass plan outputs into verify.
// It only reads cloud state: applied requires the role, group, policies,
// attachment, instance profile, and OIDC provider to be present and joined
// together; destroyed requires them all to be gone. Tearing resources down is
// the destroy plan's job, not the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

const (
	roleName              = "unobin-it-role"
	groupName             = "unobin-it-group"
	updatedGroupName      = "unobin-it-group-updated"
	userName              = "unobin-it-user"
	updatedUserName       = "unobin-it-user-updated"
	accessKeyUserName     = "unobin-it-access-key-user"
	identityPath          = "/unobin/"
	policyName            = "unobin-it-policy"
	inlinePolicyName      = "unobin-it-inline"
	groupInlinePolicyName = "unobin-it-group-inline"
	userInlinePolicyName  = "unobin-it-user-inline"
	profileName           = "unobin-it-profile"
	oidcURL               = "https://oidc.unobin-it.example.com"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := iam.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *iam.Client) error {
	if _, err := client.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	}); err != nil {
		return fmt.Errorf("get role %s: %w", roleName, err)
	}

	group, err := client.GetGroup(ctx, &iam.GetGroupInput{
		GroupName: aws.String(groupName),
	})
	if err != nil {
		return fmt.Errorf("get group %s: %w", groupName, err)
	}
	if got := groupPathOf(group.Group); got != identityPath {
		return fmt.Errorf("group %s path is %q, want %q", groupName, got, identityPath)
	}

	user, err := client.GetUser(ctx, &iam.GetUserInput{UserName: aws.String(userName)})
	if err != nil {
		return fmt.Errorf("get user %s: %w", userName, err)
	}
	if got := userPathOf(user.User); got != identityPath {
		return fmt.Errorf("user %s path is %q, want %q", userName, got, identityPath)
	}
	if got := userTagValue(user.User, "Scenario"); got == "" {
		fmt.Printf("skip: user %s tags are not available from this IAM endpoint\n", userName)
	} else if got != "iam" {
		return fmt.Errorf("user %s Scenario tag is %q, want iam", userName, got)
	}

	accessKeyUser, err := client.GetUser(ctx,
		&iam.GetUserInput{UserName: aws.String(accessKeyUserName)})
	if err != nil {
		return fmt.Errorf("get user %s: %w", accessKeyUserName, err)
	}
	if got := userPathOf(accessKeyUser.User); got != identityPath {
		return fmt.Errorf("user %s path is %q, want %q", accessKeyUserName, got, identityPath)
	}
	if err := requireAccessKeyStatus(ctx, client,
		accessKeyUserName, iamtypes.StatusTypeInactive); err != nil {
		return err
	}

	if _, err := client.GetRolePolicy(ctx, &iam.GetRolePolicyInput{
		RoleName:   aws.String(roleName),
		PolicyName: aws.String(inlinePolicyName),
	}); err != nil {
		return fmt.Errorf("get inline role policy %s: %w", inlinePolicyName, err)
	}
	if _, err := client.GetGroupPolicy(ctx, &iam.GetGroupPolicyInput{
		GroupName:  aws.String(groupName),
		PolicyName: aws.String(groupInlinePolicyName),
	}); err != nil {
		return fmt.Errorf("get inline group policy %s: %w", groupInlinePolicyName, err)
	}
	if _, err := client.GetUserPolicy(ctx, &iam.GetUserPolicyInput{
		UserName:   aws.String(userName),
		PolicyName: aws.String(userInlinePolicyName),
	}); err != nil {
		return fmt.Errorf("get inline user policy %s: %w", userInlinePolicyName, err)
	}

	policyArn, err := findPolicyArn(ctx, client)
	if err != nil {
		return err
	}
	if policyArn == "" {
		return fmt.Errorf("no managed policy named %s", policyName)
	}

	attached, err := roleHasPolicy(ctx, client, policyArn)
	if err != nil {
		return err
	}
	if !attached {
		return fmt.Errorf("policy %s is not attached to role %s", policyName, roleName)
	}
	attached, err = groupHasPolicy(ctx, client, policyArn, groupName)
	if err != nil {
		return err
	}
	if !attached {
		return fmt.Errorf("policy %s is not attached to group %s", policyName, groupName)
	}
	attached, err = userHasPolicy(ctx, client, policyArn, userName)
	if err != nil {
		return err
	}
	if !attached {
		return fmt.Errorf("policy %s is not attached to user %s", policyName, userName)
	}

	profile, err := client.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
	})
	if err != nil {
		return fmt.Errorf("get instance profile %s: %w", profileName, err)
	}
	if !profileHasRole(profile.InstanceProfile, roleName) {
		return fmt.Errorf("instance profile %s does not hold role %s", profileName, roleName)
	}

	oidcArn, err := findOIDCProviderArn(ctx, client)
	if err != nil {
		return err
	}
	if oidcArn == "" {
		return fmt.Errorf("no OIDC provider for url %s", oidcURL)
	}

	fmt.Printf(
		"ok: role %s, group %s, user %s, policy %s attached, profile %s, OIDC provider %s present\n",
		roleName, groupName, userName, policyName, profileName, oidcArn)
	return nil
}

func verifyDestroyed(ctx context.Context, client *iam.Client) error {
	if err := requireRoleGone(ctx, client); err != nil {
		return err
	}
	if err := requireInlinePolicyGone(ctx, client); err != nil {
		return err
	}
	if err := requireGroupInlinePolicyGone(ctx, client, groupName); err != nil {
		return err
	}
	if err := requireGroupInlinePolicyGone(ctx, client, updatedGroupName); err != nil {
		return err
	}
	if err := requireUserInlinePolicyGone(ctx, client, userName); err != nil {
		return err
	}
	if err := requireUserInlinePolicyGone(ctx, client, updatedUserName); err != nil {
		return err
	}
	if err := requireAccessKeysGone(ctx, client, accessKeyUserName); err != nil {
		return err
	}
	if err := requireGroupGone(ctx, client, groupName); err != nil {
		return err
	}
	if err := requireGroupGone(ctx, client, updatedGroupName); err != nil {
		return err
	}
	if err := requireUserGone(ctx, client, userName); err != nil {
		return err
	}
	if err := requireUserGone(ctx, client, updatedUserName); err != nil {
		return err
	}
	if err := requireUserGone(ctx, client, accessKeyUserName); err != nil {
		return err
	}
	if err := requireProfileGone(ctx, client); err != nil {
		return err
	}
	policyArn, err := findPolicyArn(ctx, client)
	if err != nil {
		return err
	}
	if policyArn != "" {
		return fmt.Errorf("policy %s still exists at %s", policyName, policyArn)
	}
	oidcArn, err := findOIDCProviderArn(ctx, client)
	if err != nil {
		return err
	}
	if oidcArn != "" {
		return fmt.Errorf("OIDC provider for url %s still exists at %s", oidcURL, oidcArn)
	}
	fmt.Printf(
		"ok: role %s, groups, users, policy %s, instance profile %s, and the OIDC provider are gone\n",
		roleName, policyName, profileName)
	return nil
}

func requireRoleGone(ctx context.Context, client *iam.Client) error {
	_, err := client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(roleName)})
	if err == nil {
		return fmt.Errorf("role %s still exists", roleName)
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("get role %s: %w", roleName, err)
}

// requireInlinePolicyGone confirms the inline role policy is gone. A missing
// role reports the same NoSuchEntity, so this passes once the role is deleted
// too, but it still catches an inline policy left behind on a surviving role.
func requireInlinePolicyGone(ctx context.Context, client *iam.Client) error {
	_, err := client.GetRolePolicy(ctx, &iam.GetRolePolicyInput{
		RoleName:   aws.String(roleName),
		PolicyName: aws.String(inlinePolicyName),
	})
	if err == nil {
		return fmt.Errorf("inline role policy %s still exists", inlinePolicyName)
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("get inline role policy %s: %w", inlinePolicyName, err)
}

func requireGroupGone(ctx context.Context, client *iam.Client, name string) error {
	_, err := client.GetGroup(ctx, &iam.GetGroupInput{GroupName: aws.String(name)})
	if err == nil {
		return fmt.Errorf("group %s still exists", name)
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("get group %s: %w", name, err)
}

func requireUserGone(ctx context.Context, client *iam.Client, name string) error {
	_, err := client.GetUser(ctx, &iam.GetUserInput{UserName: aws.String(name)})
	if err == nil {
		return fmt.Errorf("user %s still exists", name)
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("get user %s: %w", name, err)
}

func requireAccessKeyStatus(
	ctx context.Context, client *iam.Client, name string, status iamtypes.StatusType,
) error {
	pager := iam.NewListAccessKeysPaginator(client,
		&iam.ListAccessKeysInput{UserName: aws.String(name)})
	found := false
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list access keys for %s: %w", name, err)
		}
		for _, key := range page.AccessKeyMetadata {
			keyID := aws.ToString(key.AccessKeyId)
			if keyID == "" {
				continue
			}
			found = true
			if key.Status != status {
				return fmt.Errorf("access key %s status is %s, want %s", keyID, key.Status, status)
			}
		}
	}
	if !found {
		return fmt.Errorf("no access key exists for user %s", name)
	}
	return nil
}

func requireAccessKeysGone(ctx context.Context, client *iam.Client, name string) error {
	pager := iam.NewListAccessKeysPaginator(client,
		&iam.ListAccessKeysInput{UserName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("list access keys for %s: %w", name, err)
		}
		for _, key := range page.AccessKeyMetadata {
			keyID := aws.ToString(key.AccessKeyId)
			if keyID != "" {
				return fmt.Errorf("access key %s still exists for user %s", keyID, name)
			}
		}
	}
	return nil
}

func requireGroupInlinePolicyGone(ctx context.Context, client *iam.Client, name string) error {
	_, err := client.GetGroupPolicy(ctx, &iam.GetGroupPolicyInput{
		GroupName:  aws.String(name),
		PolicyName: aws.String(groupInlinePolicyName),
	})
	if err == nil {
		return fmt.Errorf("inline group policy %s still exists on %s", groupInlinePolicyName, name)
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("get inline group policy %s on %s: %w", groupInlinePolicyName, name, err)
}

func requireUserInlinePolicyGone(ctx context.Context, client *iam.Client, name string) error {
	_, err := client.GetUserPolicy(ctx, &iam.GetUserPolicyInput{
		UserName:   aws.String(name),
		PolicyName: aws.String(userInlinePolicyName),
	})
	if err == nil {
		return fmt.Errorf("inline user policy %s still exists on %s", userInlinePolicyName, name)
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("get inline user policy %s on %s: %w", userInlinePolicyName, name, err)
}

func requireProfileGone(ctx context.Context, client *iam.Client) error {
	_, err := client.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
	})
	if err == nil {
		return fmt.Errorf("instance profile %s still exists", profileName)
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("get instance profile %s: %w", profileName, err)
}

// findPolicyArn returns the ARN of the customer managed policy named policyName,
// or the empty string when no such policy exists.
func findPolicyArn(ctx context.Context, client *iam.Client) (string, error) {
	pager := iam.NewListPoliciesPaginator(client, &iam.ListPoliciesInput{
		Scope: iamtypes.PolicyScopeTypeLocal,
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list policies: %w", err)
		}
		for _, p := range page.Policies {
			if aws.ToString(p.PolicyName) == policyName {
				return aws.ToString(p.Arn), nil
			}
		}
	}
	return "", nil
}

// roleHasPolicy reports whether policyArn is attached to the role.
func roleHasPolicy(ctx context.Context, client *iam.Client, policyArn string) (bool, error) {
	pager := iam.NewListAttachedRolePoliciesPaginator(client,
		&iam.ListAttachedRolePoliciesInput{RoleName: aws.String(roleName)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return false, fmt.Errorf("list attached role policies: %w", err)
		}
		for _, p := range page.AttachedPolicies {
			if aws.ToString(p.PolicyArn) == policyArn {
				return true, nil
			}
		}
	}
	return false, nil
}

func groupPathOf(group *iamtypes.Group) string {
	if group == nil {
		return ""
	}
	return aws.ToString(group.Path)
}

func userPathOf(user *iamtypes.User) string {
	if user == nil {
		return ""
	}
	return aws.ToString(user.Path)
}

func userTagValue(user *iamtypes.User, key string) string {
	if user == nil {
		return ""
	}
	for _, tag := range user.Tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}

func groupHasPolicy(
	ctx context.Context, client *iam.Client, policyArn string, name string,
) (bool, error) {
	pager := iam.NewListAttachedGroupPoliciesPaginator(client,
		&iam.ListAttachedGroupPoliciesInput{GroupName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return false, fmt.Errorf("list attached group policies: %w", err)
		}
		for _, p := range page.AttachedPolicies {
			if aws.ToString(p.PolicyArn) == policyArn {
				return true, nil
			}
		}
	}
	return false, nil
}

func userHasPolicy(
	ctx context.Context, client *iam.Client, policyArn string, name string,
) (bool, error) {
	pager := iam.NewListAttachedUserPoliciesPaginator(client,
		&iam.ListAttachedUserPoliciesInput{UserName: aws.String(name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return false, fmt.Errorf("list attached user policies: %w", err)
		}
		for _, p := range page.AttachedPolicies {
			if aws.ToString(p.PolicyArn) == policyArn {
				return true, nil
			}
		}
	}
	return false, nil
}

// profileHasRole reports whether the instance profile holds a role named name.
func profileHasRole(profile *iamtypes.InstanceProfile, name string) bool {
	if profile == nil {
		return false
	}
	for _, r := range profile.Roles {
		if aws.ToString(r.RoleName) == name {
			return true
		}
	}
	return false
}

// findOIDCProviderArn returns the ARN of the OIDC provider whose URL matches
// oidcURL, or the empty string when none matches. IAM stores the provider URL
// without its scheme, so the comparison strips the leading https:// first.
func findOIDCProviderArn(ctx context.Context, client *iam.Client) (string, error) {
	list, err := client.ListOpenIDConnectProviders(ctx,
		&iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return "", fmt.Errorf("list oidc providers: %w", err)
	}
	want := strings.TrimPrefix(oidcURL, "https://")
	for _, p := range list.OpenIDConnectProviderList {
		arn := aws.ToString(p.Arn)
		got, err := client.GetOpenIDConnectProvider(ctx,
			&iam.GetOpenIDConnectProviderInput{OpenIDConnectProviderArn: aws.String(arn)})
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return "", fmt.Errorf("get oidc provider %s: %w", arn, err)
		}
		if aws.ToString(got.Url) == want {
			return arn, nil
		}
	}
	return "", nil
}

func isNotFound(err error) bool {
	var notFound *iamtypes.NoSuchEntityException
	return errors.As(err, &notFound)
}
