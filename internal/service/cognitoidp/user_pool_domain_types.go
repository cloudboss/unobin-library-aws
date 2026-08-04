package cognitoidp

type UserPoolDomainCustomDomainConfig struct {
	CertificateARN *string `ub:"certificate-arn"`
	SecurityPolicy *string `ub:"security-policy"`
}

type UserPoolDomainRouting struct {
	Failover *UserPoolDomainFailover `ub:"failover"`
}

type UserPoolDomainFailover struct {
	PrimaryRoute53HealthCheckID string `ub:"primary-route53-health-check-id"`
	SecondaryRegion             string `ub:"secondary-region"`
}

type UserPoolDomainResource struct {
	Domain              string                            `ub:"domain"`
	UserPoolID          string                            `ub:"user-pool-id"`
	CustomDomainConfig  *UserPoolDomainCustomDomainConfig `ub:"custom-domain-config"`
	ManagedLoginVersion *int32                            `ub:"managed-login-version"`
	Routing             *UserPoolDomainRouting            `ub:"routing"`
}

type UserPoolDomainResourceOutput struct {
	Domain                       string                            `ub:"domain"`
	UserPoolID                   string                            `ub:"user-pool-id"`
	CustomDomainConfig           *UserPoolDomainCustomDomainConfig `ub:"custom-domain-config"`
	ManagedLoginVersion          *int32                            `ub:"managed-login-version"`
	Routing                      *UserPoolDomainRouting            `ub:"routing"`
	AWSAccountID                 *string                           `ub:"aws-account-id"`
	CloudFrontDistribution       *string                           `ub:"cloudfront-distribution"`
	CloudFrontDistributionZoneID string                            `ub:"cloudfront-distribution-zone-id"`
	S3Bucket                     *string                           `ub:"s3-bucket"`
	Version                      *string                           `ub:"version"`
}
