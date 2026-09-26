package cognitoidp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type userAPI interface {
	AdminCreateUser(context.Context, *cognitoidentityprovider.AdminCreateUserInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.AdminCreateUserOutput, error)
	AdminDeleteUser(context.Context, *cognitoidentityprovider.AdminDeleteUserInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.AdminDeleteUserOutput, error)
	AdminDeleteUserAttributes(context.Context,
		*cognitoidentityprovider.AdminDeleteUserAttributesInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.AdminDeleteUserAttributesOutput, error)
	AdminDisableUser(context.Context, *cognitoidentityprovider.AdminDisableUserInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.AdminDisableUserOutput, error)
	AdminEnableUser(context.Context, *cognitoidentityprovider.AdminEnableUserInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.AdminEnableUserOutput, error)
	AdminGetUser(context.Context, *cognitoidentityprovider.AdminGetUserInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.AdminGetUserOutput, error)
	AdminSetUserPassword(context.Context, *cognitoidentityprovider.AdminSetUserPasswordInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.AdminSetUserPasswordOutput, error)
	AdminUpdateUserAttributes(context.Context,
		*cognitoidentityprovider.AdminUpdateUserAttributesInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.AdminUpdateUserAttributesOutput, error)
}

func (r *UserResource) SchemaVersion() int { return 1 }

func (r *UserResource) ReplaceFields() []string {
	return []string{"user-pool-id", "username"}
}

func (r *UserResource) ValidateInputs(context.Context, *awsCfg) error {
	if r.UserPoolID == "" {
		return fmt.Errorf("user-pool-id must contain at least 1 character")
	}
	if utf8.RuneCountInString(r.Username) < 1 || utf8.RuneCountInString(r.Username) > 128 {
		return fmt.Errorf("username must contain 1 to 128 characters")
	}
	if r.Password != nil && r.TemporaryPassword != nil {
		return fmt.Errorf("password conflicts with temporary-password")
	}
	if err := validateUserPassword("password", r.Password); err != nil {
		return err
	}
	if err := validateUserPassword("temporary-password", r.TemporaryPassword); err != nil {
		return err
	}
	if r.DesiredDeliveryMediums != nil {
		for _, value := range *r.DesiredDeliveryMediums {
			if !slices.Contains([]string{"EMAIL", "SMS"}, value) {
				return fmt.Errorf("desired-delivery-mediums contains invalid value %q", value)
			}
		}
	}
	if r.MessageAction != nil && !slices.Contains([]string{"RESEND", "SUPPRESS"},
		*r.MessageAction) {
		return fmt.Errorf("message-action must be RESEND or SUPPRESS")
	}
	return nil
}

func (r *UserResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*UserResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client)
}

func (r *UserResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[UserResource, *UserResourceOutput, *awsCfg],
) (*UserResourceOutput, error) {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.readPrior(ctx, client, prior)
}

func (r *UserResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[UserResource, *UserResourceOutput, *awsCfg],
) (*UserResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior)
}

func (r *UserResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[UserResource, *UserResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.deletePrior(ctx, client, prior)
}

func (r *UserResource) create(ctx context.Context, client userAPI) (*UserResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	if _, err := client.AdminCreateUser(ctx, r.createInput()); err != nil {
		return nil, fmt.Errorf("create user %s: %w", r.Username, err)
	}
	if r.Enabled != nil && !*r.Enabled {
		if err := r.disable(ctx, client); err != nil {
			return nil, cleanupFailedUserCreate(ctx, client, r.UserPoolID, r.Username, err)
		}
	}
	if r.Password != nil {
		if err := r.setPassword(ctx, client, true, *r.Password); err != nil {
			return nil, cleanupFailedUserCreate(ctx, client, r.UserPoolID, r.Username, err)
		}
	}
	out, err := r.read(ctx, client)
	if err != nil {
		return nil, cleanupFailedUserCreate(ctx, client, r.UserPoolID, r.Username, err)
	}
	return out, nil
}

func (r *UserResource) read(ctx context.Context, client userAPI) (*UserResourceOutput, error) {
	return r.readIdentity(ctx, client, r.UserPoolID, r.Username)
}

func (r *UserResource) readPrior(
	ctx context.Context,
	client userAPI,
	prior *UserResourceOutput,
) (*UserResourceOutput, error) {
	poolID, username, err := readUserIdentity(r, prior)
	if err != nil {
		return nil, err
	}
	return r.readIdentity(ctx, client, poolID, username)
}

func (r *UserResource) readIdentity(
	ctx context.Context,
	client userAPI,
	poolID string,
	username string,
) (*UserResourceOutput, error) {
	out, err := client.AdminGetUser(ctx, &cognitoidentityprovider.AdminGetUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String(username),
	})
	if isUserNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", username, err)
	}
	if out == nil {
		return nil, fmt.Errorf("get user %s: empty response", username)
	}
	attributes := flattenUserAttributes(out.UserAttributes)
	return &UserResourceOutput{
		Attributes:          attributes,
		CreationDate:        userTime(out.UserCreateDate),
		LastModifiedDate:    userTime(out.UserLastModifiedDate),
		Enabled:             out.Enabled,
		MFASettingList:      append([]string{}, out.UserMFASettingList...),
		PreferredMFASetting: aws.ToString(out.PreferredMfaSetting),
		Status:              string(out.UserStatus),
		Sub:                 attributes["sub"],
		UserPoolID:          poolID,
		Username:            aws.ToString(out.Username),
	}, nil
}

func (r *UserResource) update(
	ctx context.Context,
	client userAPI,
	prior runtime.Prior[UserResource, *UserResourceOutput, *awsCfg],
) (*UserResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	updateAttributes, deleteAttributes := userAttributeChanges(
		prior.Inputs.Attributes,
		r.Attributes,
	)
	if len(updateAttributes) > 0 {
		input := &cognitoidentityprovider.AdminUpdateUserAttributesInput{
			UserPoolId:     aws.String(r.UserPoolID),
			Username:       aws.String(r.Username),
			UserAttributes: updateAttributes,
			ClientMetadata: nonEmptyStringMap(r.ClientMetadata),
		}
		if _, err := client.AdminUpdateUserAttributes(ctx, input); err != nil {
			return nil, fmt.Errorf("update user %s attributes: %w", r.Username, err)
		}
	}
	if len(deleteAttributes) > 0 {
		input := &cognitoidentityprovider.AdminDeleteUserAttributesInput{
			UserPoolId:         aws.String(r.UserPoolID),
			Username:           aws.String(r.Username),
			UserAttributeNames: deleteAttributes,
		}
		if _, err := client.AdminDeleteUserAttributes(ctx, input); err != nil {
			return nil, fmt.Errorf("delete user %s attributes: %w", r.Username, err)
		}
	}
	if effectiveUserEnabled(prior.Inputs.Enabled) != effectiveUserEnabled(r.Enabled) {
		if effectiveUserEnabled(r.Enabled) {
			if err := r.enable(ctx, client); err != nil {
				return nil, err
			}
		} else if err := r.disable(ctx, client); err != nil {
			return nil, err
		}
	}
	if changedUserString(prior.Inputs.TemporaryPassword, r.TemporaryPassword) &&
		r.TemporaryPassword != nil {
		if err := r.setPassword(ctx, client, false, *r.TemporaryPassword); err != nil {
			return nil, err
		}
	}
	if changedUserString(prior.Inputs.Password, r.Password) && r.Password != nil {
		if err := r.setPassword(ctx, client, true, *r.Password); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *UserResource) delete(ctx context.Context, client userAPI) error {
	return r.deleteIdentity(ctx, client, r.UserPoolID, r.Username)
}

func (r *UserResource) deletePrior(
	ctx context.Context,
	client userAPI,
	prior *UserResourceOutput,
) error {
	poolID, username, err := priorUserIdentity(prior)
	if err != nil {
		return err
	}
	return r.deleteIdentity(ctx, client, poolID, username)
}

func (r *UserResource) deleteIdentity(
	ctx context.Context,
	client userAPI,
	poolID string,
	username string,
) error {
	_, err := client.AdminDeleteUser(ctx, &cognitoidentityprovider.AdminDeleteUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String(username),
	})
	if isUserNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete user %s: %w", username, err)
	}
	return nil
}

func readUserIdentity(r *UserResource, prior *UserResourceOutput) (string, string, error) {
	if prior == nil {
		return r.UserPoolID, r.Username, nil
	}
	return priorUserIdentity(prior)
}

func priorUserIdentity(prior *UserResourceOutput) (string, string, error) {
	if prior == nil {
		return "", "", fmt.Errorf("prior output is missing")
	}
	if prior.UserPoolID == "" {
		return "", "", fmt.Errorf("prior output has no user-pool-id")
	}
	if prior.Username == "" {
		return "", "", fmt.Errorf("prior output has no username")
	}
	return prior.UserPoolID, prior.Username, nil
}

func cleanupFailedUserCreate(
	ctx context.Context,
	client userAPI,
	poolID string,
	username string,
	failure error,
) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		userPoolCleanupTimeout,
	)
	defer cancel()
	_, err := client.AdminDeleteUser(cleanupCtx, &cognitoidentityprovider.AdminDeleteUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String(username),
	})
	if err == nil || isUserNotFound(err) {
		return failure
	}
	cleanupErr := fmt.Errorf("compensating delete user %s: %w", username, err)
	return errors.Join(failure, cleanupErr)
}

func (r *UserResource) createInput() *cognitoidentityprovider.AdminCreateUserInput {
	return &cognitoidentityprovider.AdminCreateUserInput{
		UserPoolId:             aws.String(r.UserPoolID),
		Username:               aws.String(r.Username),
		ClientMetadata:         nonEmptyStringMap(r.ClientMetadata),
		DesiredDeliveryMediums: userDeliveryMediums(r.DesiredDeliveryMediums),
		ForceAliasCreation:     aws.ToBool(r.ForceAliasCreation),
		MessageAction:          userMessageAction(r.MessageAction),
		TemporaryPassword:      nonEmptyString(r.TemporaryPassword),
		UserAttributes:         userAttributes(r.Attributes),
		ValidationData:         userAttributes(r.ValidationData),
	}
}

func (r *UserResource) enable(ctx context.Context, client userAPI) error {
	_, err := client.AdminEnableUser(ctx, &cognitoidentityprovider.AdminEnableUserInput{
		UserPoolId: aws.String(r.UserPoolID),
		Username:   aws.String(r.Username),
	})
	if err != nil {
		return fmt.Errorf("enable user %s: %w", r.Username, err)
	}
	return nil
}

func (r *UserResource) disable(ctx context.Context, client userAPI) error {
	_, err := client.AdminDisableUser(ctx, &cognitoidentityprovider.AdminDisableUserInput{
		UserPoolId: aws.String(r.UserPoolID),
		Username:   aws.String(r.Username),
	})
	if err != nil {
		return fmt.Errorf("disable user %s: %w", r.Username, err)
	}
	return nil
}

func (r *UserResource) setPassword(
	ctx context.Context,
	client userAPI,
	permanent bool,
	password string,
) error {
	_, err := client.AdminSetUserPassword(ctx, &cognitoidentityprovider.AdminSetUserPasswordInput{
		UserPoolId: aws.String(r.UserPoolID),
		Username:   aws.String(r.Username),
		Password:   aws.String(password),
		Permanent:  permanent,
	})
	if err != nil {
		return fmt.Errorf("set user %s password: %w", r.Username, err)
	}
	return nil
}

func validateUserPassword(name string, value *string) error {
	if value != nil &&
		(utf8.RuneCountInString(*value) < 6 || utf8.RuneCountInString(*value) > 256) {
		return fmt.Errorf("%s must contain 6 to 256 characters", name)
	}
	return nil
}

func nonEmptyStringMap(value *map[string]string) map[string]string {
	if value == nil || len(*value) == 0 {
		return nil
	}
	out := make(map[string]string, len(*value))
	for key, item := range *value {
		out[key] = item
	}
	return out
}

func nonEmptyString(value *string) *string {
	if value == nil || *value == "" {
		return nil
	}
	return value
}

func userDeliveryMediums(values *[]string) []cognitotypes.DeliveryMediumType {
	if values == nil || len(*values) == 0 {
		return nil
	}
	out := make([]cognitotypes.DeliveryMediumType, 0, len(*values))
	for _, value := range *values {
		out = append(out, cognitotypes.DeliveryMediumType(value))
	}
	return out
}

func userMessageAction(value *string) cognitotypes.MessageActionType {
	if value == nil {
		return ""
	}
	return cognitotypes.MessageActionType(*value)
}

func userAttributes(values *map[string]string) []cognitotypes.AttributeType {
	normalized := normalizedUserAttributeMap(values)
	if len(normalized) == 0 {
		return nil
	}
	names := sortedUserAttributeNames(normalized)
	out := make([]cognitotypes.AttributeType, 0, len(names))
	for _, name := range names {
		out = append(out, cognitotypes.AttributeType{
			Name:  aws.String(name),
			Value: aws.String(normalized[name]),
		})
	}
	return out
}

func userAttributeChanges(
	prior *map[string]string,
	current *map[string]string,
) ([]cognitotypes.AttributeType, []string) {
	priorAttributes := normalizedUserAttributeMap(prior)
	currentAttributes := normalizedUserAttributeMap(current)
	updateNames := []string{}
	deleteNames := []string{}
	for name, value := range currentAttributes {
		if priorValue, ok := priorAttributes[name]; !ok || priorValue != value {
			updateNames = append(updateNames, name)
		}
	}
	for name := range priorAttributes {
		if name == "sub" {
			continue
		}
		if _, ok := currentAttributes[name]; !ok {
			deleteNames = append(deleteNames, name)
		}
	}
	slices.Sort(updateNames)
	slices.Sort(deleteNames)
	updates := make([]cognitotypes.AttributeType, 0, len(updateNames))
	for _, name := range updateNames {
		updates = append(updates, cognitotypes.AttributeType{
			Name:  aws.String(name),
			Value: aws.String(currentAttributes[name]),
		})
	}
	return updates, deleteNames
}

func normalizedUserAttributeMap(values *map[string]string) map[string]string {
	out := map[string]string{}
	if values == nil {
		return out
	}
	for name, value := range *values {
		out[apiUserAttributeName(name)] = value
	}
	return out
}

func sortedUserAttributeNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func flattenUserAttributes(values []cognitotypes.AttributeType) map[string]string {
	out := make(map[string]string, len(values))
	for _, attribute := range values {
		name := aws.ToString(attribute.Name)
		if name == "" {
			continue
		}
		out[targetUserAttributeName(name)] = aws.ToString(attribute.Value)
	}
	return out
}

func apiUserAttributeName(name string) string {
	if strings.HasPrefix(name, "custom:") || standardUserAttributeNames[name] {
		return name
	}
	return "custom:" + name
}

func targetUserAttributeName(name string) string {
	if strings.HasPrefix(name, "custom:") {
		return strings.TrimPrefix(name, "custom:")
	}
	if strings.HasPrefix(name, "dev:") {
		return strings.TrimPrefix(name, "dev:")
	}
	return name
}

func effectiveUserEnabled(value *bool) bool {
	return value == nil || *value
}

func changedUserString(prior, current *string) bool {
	if prior == nil || current == nil {
		return prior != current
	}
	return *prior != *current
}

func isUserNotFound(err error) bool {
	var userNotFound *cognitotypes.UserNotFoundException
	return errors.As(err, &userNotFound) || isUserPoolNotFound(err)
}

func userTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

var standardUserAttributeNames = map[string]bool{
	"address":               true,
	"birthdate":             true,
	"email":                 true,
	"email_verified":        true,
	"family_name":           true,
	"gender":                true,
	"given_name":            true,
	"locale":                true,
	"middle_name":           true,
	"name":                  true,
	"nickname":              true,
	"phone_number":          true,
	"phone_number_verified": true,
	"picture":               true,
	"preferred_username":    true,
	"profile":               true,
	"sub":                   true,
	"updated_at":            true,
	"website":               true,
	"zoneinfo":              true,
}
