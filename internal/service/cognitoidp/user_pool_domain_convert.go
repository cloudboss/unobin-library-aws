package cognitoidp

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

func (r *UserPoolDomainResource) createDomainInput() *cognitoidentityprovider.CreateUserPoolDomainInput {
	return &cognitoidentityprovider.CreateUserPoolDomainInput{
		Domain:              aws.String(r.Domain),
		UserPoolId:          aws.String(r.UserPoolID),
		CustomDomainConfig:  userPoolDomainCustomInput(r.CustomDomainConfig),
		ManagedLoginVersion: r.ManagedLoginVersion,
		Routing:             userPoolDomainRoutingInput(r.Routing),
	}
}

func (r *UserPoolDomainResource) updateDomainInput(
	prior UserPoolDomainResource,
) *cognitoidentityprovider.UpdateUserPoolDomainInput {
	input := &cognitoidentityprovider.UpdateUserPoolDomainInput{
		Domain:     aws.String(prior.Domain),
		UserPoolId: aws.String(prior.UserPoolID),
	}
	customChanged := !equalDomainCustomConfig(prior.CustomDomainConfig, r.CustomDomainConfig)
	managedChanged := r.ManagedLoginVersion != nil &&
		!equalInt32Ptr(prior.ManagedLoginVersion, r.ManagedLoginVersion)
	routingChanged := !equalDomainRouting(prior.Routing, r.Routing)
	if customChanged || managedChanged && r.hasCustomCertificate() {
		input.CustomDomainConfig = userPoolDomainCustomInput(r.CustomDomainConfig)
	}
	if managedChanged {
		input.ManagedLoginVersion = r.ManagedLoginVersion
	}
	if routingChanged {
		input.Routing = userPoolDomainRoutingInput(r.Routing)
	}
	return input
}

func userPoolDomainCustomInput(
	config *UserPoolDomainCustomDomainConfig,
) *cognitotypes.CustomDomainConfigType {
	certificate := userPoolDomainCustomCertificate(config)
	if certificate == "" {
		return nil
	}
	out := &cognitotypes.CustomDomainConfigType{CertificateArn: aws.String(certificate)}
	if config.SecurityPolicy != nil {
		out.SecurityPolicy = cognitotypes.SecurityPolicyType(*config.SecurityPolicy)
	}
	return out
}

func userPoolDomainRoutingInput(routing *UserPoolDomainRouting) *cognitotypes.RoutingType {
	if routing == nil || routing.Failover == nil {
		return nil
	}
	return &cognitotypes.RoutingType{
		Failover: &cognitotypes.FailoverType{
			PrimaryRoute53HealthCheckId: aws.String(
				routing.Failover.PrimaryRoute53HealthCheckID,
			),
			SecondaryRegion: aws.String(routing.Failover.SecondaryRegion),
		},
	}
}

func userPoolDomainCustomOutput(
	config *cognitotypes.CustomDomainConfigType,
) *UserPoolDomainCustomDomainConfig {
	if config == nil || aws.ToString(config.CertificateArn) == "" {
		return nil
	}
	out := &UserPoolDomainCustomDomainConfig{CertificateARN: config.CertificateArn}
	if config.SecurityPolicy != "" {
		value := string(config.SecurityPolicy)
		out.SecurityPolicy = &value
	}
	return out
}

func userPoolDomainRoutingOutput(routing *cognitotypes.RoutingType) *UserPoolDomainRouting {
	if routing == nil || routing.Failover == nil {
		return nil
	}
	return &UserPoolDomainRouting{Failover: &UserPoolDomainFailover{
		PrimaryRoute53HealthCheckID: aws.ToString(
			routing.Failover.PrimaryRoute53HealthCheckId,
		),
		SecondaryRegion: aws.ToString(routing.Failover.SecondaryRegion),
	}}
}

func equalDomainCustomConfig(a, b *UserPoolDomainCustomDomainConfig) bool {
	return userPoolDomainCustomCertificate(a) == userPoolDomainCustomCertificate(b) &&
		stringPtrValue(securityPolicyPtr(a)) == stringPtrValue(securityPolicyPtr(b))
}

func securityPolicyPtr(config *UserPoolDomainCustomDomainConfig) *string {
	if config == nil {
		return nil
	}
	return config.SecurityPolicy
}

func equalDomainRouting(a, b *UserPoolDomainRouting) bool {
	af, bf := userPoolDomainFailover(a), userPoolDomainFailover(b)
	if af == nil || bf == nil {
		return af == nil && bf == nil
	}
	return af.PrimaryRoute53HealthCheckID == bf.PrimaryRoute53HealthCheckID &&
		af.SecondaryRegion == bf.SecondaryRegion
}

func userPoolDomainFailover(routing *UserPoolDomainRouting) *UserPoolDomainFailover {
	if routing == nil {
		return nil
	}
	return routing.Failover
}

func equalInt32Ptr(a, b *int32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
