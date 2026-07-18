package cognitoidp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

const userPoolCleanupTimeout = 2 * time.Minute

func (r *UserPoolResource) SchemaVersion() int { return 1 }

func (r *UserPoolResource) ReplaceFields() []string {
	return []string{"alias-attributes", "username-attributes", "username-configuration"}
}

func (r *UserPoolResource) EquivalentInput(
	field string,
	prior UserPoolResource,
	current UserPoolResource,
) bool {
	switch field {
	case "alias-attributes":
		return optionalUnorderedSliceEqual(prior.AliasAttributes, current.AliasAttributes)
	case "username-attributes":
		return optionalUnorderedSliceEqual(
			prior.UsernameAttributes,
			current.UsernameAttributes,
		)
	case "auto-verified-attributes":
		return optionalUnorderedSliceEqual(
			prior.AutoVerifiedAttributes,
			current.AutoVerifiedAttributes,
		)
	case "enabled-mfas":
		return optionalUnorderedSliceEqual(prior.EnabledMFAs, current.EnabledMFAs)
	case "schema":
		return optionalUnorderedSliceEqual(prior.Schema, current.Schema)
	case "account-recovery-setting":
		return equivalentRecoverySetting(
			prior.AccountRecoverySetting,
			current.AccountRecoverySetting,
		)
	case "sign-in-policy":
		return equivalentSignInPolicy(prior.SignInPolicy, current.SignInPolicy)
	case "user-attribute-update-settings":
		return equivalentAttributeUpdateSettings(
			prior.UserAttributeUpdateSettings,
			current.UserAttributeUpdateSettings,
		)
	default:
		return false
	}
}

func optionalUnorderedSliceEqual[T any](left, right *[]T) bool {
	if (left == nil) != (right == nil) {
		return false
	}
	if left == nil {
		return true
	}
	return unorderedSliceEqual(*left, *right)
}

func unorderedSliceEqual[T any](left, right []T) bool {
	if (left == nil) != (right == nil) || len(left) != len(right) {
		return false
	}
	matched := make([]bool, len(right))
	for _, leftValue := range left {
		found := false
		for index, rightValue := range right {
			if matched[index] || !reflect.DeepEqual(leftValue, rightValue) {
				continue
			}
			matched[index] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func equivalentRecoverySetting(
	left *UserPoolAccountRecoverySetting,
	right *UserPoolAccountRecoverySetting,
) bool {
	if (left == nil) != (right == nil) {
		return false
	}
	return left == nil || unorderedSliceEqual(left.RecoveryMechanisms, right.RecoveryMechanisms)
}

func equivalentSignInPolicy(left, right *UserPoolSignInPolicy) bool {
	if (left == nil) != (right == nil) {
		return false
	}
	return left == nil || optionalUnorderedSliceEqual(
		left.AllowedFirstAuthFactors,
		right.AllowedFirstAuthFactors,
	)
}

func equivalentAttributeUpdateSettings(
	left *UserPoolAttributeUpdateSettings,
	right *UserPoolAttributeUpdateSettings,
) bool {
	if (left == nil) != (right == nil) {
		return false
	}
	return left == nil || unorderedSliceEqual(
		left.AttributesRequireVerificationBeforeUpdate,
		right.AttributesRequireVerificationBeforeUpdate,
	)
}

func (r *UserPoolResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*UserPoolResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, client.Options().Region, systemUserPoolClock{})
}

func (r *UserPoolResource) create(
	ctx context.Context,
	client cognitoClient,
	region string,
	clock userPoolClock,
) (*UserPoolResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	in, err := r.createInput()
	if err != nil {
		return nil, err
	}
	var created *cognitoidentityprovider.CreateUserPoolOutput
	err = retryUserPool(ctx, clock, func(ctx context.Context) error {
		var err error
		created, err = client.CreateUserPool(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create user pool %s: %w", r.Name, err)
	}
	if created == nil || created.UserPool == nil ||
		aws.ToString(created.UserPool.Id) == "" {
		return nil, fmt.Errorf("create user pool %s: response has no user pool ID", r.Name)
	}
	id := aws.ToString(created.UserPool.Id)
	if r.needsMFAWrite() {
		mfaInput := r.mfaInput(id)
		err := retryUserPool(ctx, clock, func(ctx context.Context) error {
			_, err := client.SetUserPoolMfaConfig(ctx, mfaInput)
			return err
		})
		if err != nil {
			failure := fmt.Errorf("set user pool %s MFA configuration: %w", id, err)
			return nil, cleanupFailedUserPoolCreate(ctx, client, id, failure)
		}
	}
	out, err := r.read(ctx, client, region, id)
	if err != nil {
		return nil, cleanupFailedUserPoolCreate(ctx, client, id, err)
	}
	return out, nil
}

func cleanupFailedUserPoolCreate(
	ctx context.Context,
	client cognitoClient,
	id string,
	failure error,
) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		userPoolCleanupTimeout,
	)
	defer cancel()
	_, err := client.DeleteUserPool(
		cleanupCtx,
		&cognitoidentityprovider.DeleteUserPoolInput{UserPoolId: aws.String(id)},
	)
	if err == nil || isUserPoolNotFound(err) {
		return failure
	}
	cleanupErr := fmt.Errorf("compensating delete user pool %s: %w", id, err)
	return errors.Join(failure, cleanupErr)
}

func (r *UserPoolResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *UserPoolResourceOutput,
) (*UserPoolResourceOutput, error) {
	id, err := priorUserPoolID(prior)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, client.Options().Region, id)
}

func (r *UserPoolResource) read(
	ctx context.Context,
	client cognitoClient,
	region string,
	id string,
) (*UserPoolResourceOutput, error) {
	described, err := client.DescribeUserPool(ctx, &cognitoidentityprovider.DescribeUserPoolInput{
		UserPoolId: aws.String(id),
	})
	if isUserPoolNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe user pool %s: %w", id, err)
	}
	if described == nil || described.UserPool == nil {
		return nil, fmt.Errorf("describe user pool %s: empty response", id)
	}
	pool := described.UserPool
	if aws.ToString(pool.Id) == "" {
		return nil, fmt.Errorf("describe user pool %s: response has no ID", id)
	}
	if aws.ToString(pool.Arn) == "" {
		return nil, fmt.Errorf("describe user pool %s: response has no ARN", id)
	}
	mfa, err := client.GetUserPoolMfaConfig(
		ctx,
		&cognitoidentityprovider.GetUserPoolMfaConfigInput{UserPoolId: aws.String(id)},
	)
	if isUserPoolNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user pool %s MFA configuration: %w", id, err)
	}
	if mfa == nil {
		return nil, fmt.Errorf("get user pool %s MFA configuration: empty response", id)
	}
	endpoint := cognitoProviderName(region, aws.ToString(pool.Id))
	out := &UserPoolResourceOutput{
		UserPoolID:             aws.ToString(pool.Id),
		ARN:                    aws.ToString(pool.Arn),
		ProviderName:           endpoint,
		ProviderURL:            "https://" + endpoint,
		Endpoint:               endpoint,
		Domain:                 aws.ToString(pool.Domain),
		CustomDomain:           aws.ToString(pool.CustomDomain),
		EstimatedNumberOfUsers: int64(pool.EstimatedNumberOfUsers),
		UserPoolTier:           string(pool.UserPoolTier),
	}
	if pool.CreationDate != nil {
		out.CreationDate = pool.CreationDate.UTC().Format(time.RFC3339)
	}
	if pool.LastModifiedDate != nil {
		out.LastModifiedDate = pool.LastModifiedDate.UTC().Format(time.RFC3339)
	}
	return out, nil
}

func (r *UserPoolResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[UserPoolResource, *UserPoolResourceOutput],
) (*UserPoolResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, client.Options().Region, prior, systemUserPoolClock{})
}

func (r *UserPoolResource) update(
	ctx context.Context,
	client cognitoClient,
	region string,
	prior runtime.Prior[UserPoolResource, *UserPoolResourceOutput],
	clock userPoolClock,
) (*UserPoolResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	id, err := priorUserPoolID(prior.Outputs)
	if err != nil {
		return nil, err
	}
	additions, err := r.validateUpdate(prior.Inputs)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, prior.Outputs.ARN); err != nil {
			return nil, err
		}
	}
	if r.mfaInputsChanged(prior.Inputs) {
		err := retryUserPool(ctx, clock, func(ctx context.Context) error {
			_, err := client.SetUserPoolMfaConfig(ctx, r.mfaInput(id))
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("set user pool %s MFA configuration: %w", id, err)
		}
	}
	if r.standardInputsChanged(prior.Inputs) {
		in, err := r.updateInput(id, prior.Inputs)
		if err != nil {
			return nil, err
		}
		err = retryUserPool(ctx, clock, func(ctx context.Context) error {
			_, err := client.UpdateUserPool(ctx, in)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("update user pool %s: %w", id, err)
		}
	}
	if len(additions) > 0 {
		_, err := client.AddCustomAttributes(
			ctx,
			&cognitoidentityprovider.AddCustomAttributesInput{
				UserPoolId:       aws.String(id),
				CustomAttributes: schemaAttributes(additions),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("add user pool %s custom attributes: %w", id, err)
		}
	}
	return r.read(ctx, client, region, id)
}

func (r *UserPoolResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *UserPoolResourceOutput,
) error {
	id, err := priorUserPoolID(prior)
	if err != nil {
		return err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, id)
}

func (r *UserPoolResource) delete(
	ctx context.Context,
	client cognitoClient,
	id string,
) error {
	_, err := client.DeleteUserPool(
		ctx,
		&cognitoidentityprovider.DeleteUserPoolInput{UserPoolId: aws.String(id)},
	)
	if isUserPoolNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete user pool %s: %w", id, err)
	}
	return nil
}

func (r *UserPoolResource) createInput() (*cognitoidentityprovider.CreateUserPoolInput, error) {
	template, err := r.verificationTemplate()
	if err != nil {
		return nil, err
	}
	return &cognitoidentityprovider.CreateUserPoolInput{
		PoolName:                    aws.String(r.Name),
		AccountRecoverySetting:      accountRecoverySetting(r.AccountRecoverySetting),
		AdminCreateUserConfig:       adminCreateUserConfig(r.AdminCreateUserConfig),
		AliasAttributes:             aliasAttributes(ptr.Value(r.AliasAttributes)),
		AutoVerifiedAttributes:      verifiedAttributes(ptr.Value(r.AutoVerifiedAttributes)),
		DeletionProtection:          deletionProtection(r.DeletionProtection),
		DeviceConfiguration:         deviceConfiguration(r.DeviceConfiguration),
		EmailConfiguration:          emailConfiguration(r.EmailConfiguration),
		EmailVerificationMessage:    r.EmailVerificationMessage,
		EmailVerificationSubject:    r.EmailVerificationSubject,
		LambdaConfig:                lambdaConfiguration(r.LambdaConfig),
		Policies:                    userPoolPolicies(r.PasswordPolicy, r.SignInPolicy),
		Schema:                      schemaAttributes(ptr.Value(r.Schema)),
		SmsAuthenticationMessage:    r.SMSAuthenticationMessage,
		SmsConfiguration:            smsConfiguration(r.SMSConfiguration),
		SmsVerificationMessage:      r.SMSVerificationMessage,
		UserAttributeUpdateSettings: userAttributeUpdateSettings(r.UserAttributeUpdateSettings),
		UserPoolAddOns:              userPoolAddOns(r.UserPoolAddOns),
		UserPoolTags:                desiredTags(ptr.Value(r.Tags)),
		UserPoolTier:                userPoolTier(r.UserPoolTier),
		UsernameAttributes:          usernameAttributes(ptr.Value(r.UsernameAttributes)),
		UsernameConfiguration:       usernameConfiguration(r.UsernameConfiguration),
		VerificationMessageTemplate: template,
	}, nil
}

func (r *UserPoolResource) updateInput(
	id string,
	prior UserPoolResource,
) (*cognitoidentityprovider.UpdateUserPoolInput, error) {
	template, err := r.verificationTemplate()
	if err != nil {
		return nil, err
	}
	settings := userAttributeUpdateSettings(r.UserAttributeUpdateSettings)
	if prior.UserAttributeUpdateSettings != nil && r.UserAttributeUpdateSettings == nil {
		settings = emptyUserAttributeUpdateSettings()
	}
	return &cognitoidentityprovider.UpdateUserPoolInput{
		UserPoolId:                  aws.String(id),
		AccountRecoverySetting:      accountRecoverySetting(r.AccountRecoverySetting),
		AdminCreateUserConfig:       adminCreateUserConfig(r.AdminCreateUserConfig),
		AutoVerifiedAttributes:      verifiedAttributes(ptr.Value(r.AutoVerifiedAttributes)),
		DeletionProtection:          deletionProtection(r.DeletionProtection),
		DeviceConfiguration:         deviceConfiguration(r.DeviceConfiguration),
		EmailConfiguration:          emailConfiguration(r.EmailConfiguration),
		EmailVerificationMessage:    r.EmailVerificationMessage,
		EmailVerificationSubject:    r.EmailVerificationSubject,
		LambdaConfig:                lambdaConfiguration(r.LambdaConfig),
		MfaConfiguration:            mfaConfiguration(r.MFAConfiguration),
		Policies:                    userPoolPolicies(r.PasswordPolicy, r.SignInPolicy),
		PoolName:                    aws.String(r.Name),
		SmsAuthenticationMessage:    r.SMSAuthenticationMessage,
		SmsConfiguration:            smsConfiguration(r.SMSConfiguration),
		SmsVerificationMessage:      r.SMSVerificationMessage,
		UserAttributeUpdateSettings: settings,
		UserPoolAddOns:              userPoolAddOns(r.UserPoolAddOns),
		UserPoolTier:                userPoolTier(r.UserPoolTier),
		VerificationMessageTemplate: template,
	}, nil
}

func (r *UserPoolResource) needsMFAWrite() bool {
	return r.MFAConfiguration != nil ||
		r.EnabledMFAs != nil ||
		r.EmailMFAConfiguration != nil ||
		r.WebAuthnConfiguration != nil
}

func (r *UserPoolResource) mfaInputsChanged(prior UserPoolResource) bool {
	return runtime.Changed(prior.MFAConfiguration, r.MFAConfiguration) ||
		!optionalUnorderedSliceEqual(prior.EnabledMFAs, r.EnabledMFAs) ||
		runtime.Changed(prior.EmailMFAConfiguration, r.EmailMFAConfiguration) ||
		runtime.Changed(prior.SMSAuthenticationMessage, r.SMSAuthenticationMessage) ||
		runtime.Changed(prior.SMSConfiguration, r.SMSConfiguration) ||
		runtime.Changed(prior.WebAuthnConfiguration, r.WebAuthnConfiguration)
}

func (r *UserPoolResource) standardInputsChanged(prior UserPoolResource) bool {
	return !equivalentRecoverySetting(
		prior.AccountRecoverySetting,
		r.AccountRecoverySetting,
	) ||
		runtime.Changed(prior.AdminCreateUserConfig, r.AdminCreateUserConfig) ||
		!optionalUnorderedSliceEqual(
			prior.AutoVerifiedAttributes,
			r.AutoVerifiedAttributes,
		) ||
		runtime.Changed(prior.DeletionProtection, r.DeletionProtection) ||
		runtime.Changed(prior.DeviceConfiguration, r.DeviceConfiguration) ||
		runtime.Changed(prior.EmailConfiguration, r.EmailConfiguration) ||
		runtime.Changed(prior.EmailVerificationMessage, r.EmailVerificationMessage) ||
		runtime.Changed(prior.EmailVerificationSubject, r.EmailVerificationSubject) ||
		runtime.Changed(prior.LambdaConfig, r.LambdaConfig) ||
		runtime.Changed(prior.Name, r.Name) ||
		runtime.Changed(prior.PasswordPolicy, r.PasswordPolicy) ||
		!equivalentSignInPolicy(prior.SignInPolicy, r.SignInPolicy) ||
		runtime.Changed(prior.SMSAuthenticationMessage, r.SMSAuthenticationMessage) ||
		runtime.Changed(prior.SMSConfiguration, r.SMSConfiguration) ||
		runtime.Changed(prior.SMSVerificationMessage, r.SMSVerificationMessage) ||
		!equivalentAttributeUpdateSettings(
			prior.UserAttributeUpdateSettings,
			r.UserAttributeUpdateSettings,
		) ||
		runtime.Changed(prior.UserPoolAddOns, r.UserPoolAddOns) ||
		runtime.Changed(prior.UserPoolTier, r.UserPoolTier) ||
		runtime.Changed(prior.VerificationMessageTemplate, r.VerificationMessageTemplate)
}

func (r *UserPoolResource) validateUpdate(
	prior UserPoolResource,
) ([]UserPoolSchemaAttribute, error) {
	if prior.SMSConfiguration != nil && r.SMSConfiguration == nil {
		return nil, fmt.Errorf("removing sms-configuration requires user pool replacement")
	}
	return schemaAdditions(ptr.Value(prior.Schema), ptr.Value(r.Schema))
}

func schemaAdditions(
	prior []UserPoolSchemaAttribute,
	current []UserPoolSchemaAttribute,
) ([]UserPoolSchemaAttribute, error) {
	priorByName := make(map[string]UserPoolSchemaAttribute, len(prior))
	for _, attribute := range prior {
		priorByName[attribute.Name] = attribute
	}
	currentByName := make(map[string]UserPoolSchemaAttribute, len(current))
	for _, attribute := range current {
		currentByName[attribute.Name] = attribute
	}
	for name, oldAttribute := range priorByName {
		newAttribute, ok := currentByName[name]
		if !ok {
			return nil, fmt.Errorf("removing schema attribute %q requires user pool replacement",
				name)
		}
		if !reflect.DeepEqual(oldAttribute, newAttribute) {
			return nil, fmt.Errorf("modifying schema attribute %q requires user pool replacement",
				name)
		}
	}
	additions := make([]UserPoolSchemaAttribute, 0, len(current)-len(prior))
	for _, attribute := range current {
		if _, ok := priorByName[attribute.Name]; !ok {
			if !aws.ToBool(attribute.DeveloperOnlyAttribute) &&
				isStandardUserPoolAttribute(attribute.Name) {
				return nil, fmt.Errorf(
					"adding standard schema attribute %q requires user pool replacement",
					attribute.Name,
				)
			}
			additions = append(additions, attribute)
		}
	}
	return additions, nil
}

func priorUserPoolID(prior *UserPoolResourceOutput) (string, error) {
	if prior == nil {
		return "", fmt.Errorf("user pool prior output is missing")
	}
	if prior.UserPoolID == "" {
		return "", fmt.Errorf("user pool prior output has no user-pool-id")
	}
	return prior.UserPoolID, nil
}
