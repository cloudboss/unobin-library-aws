package cognitoidp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	userPoolDomainPrefixCreateTimeout = time.Minute
	userPoolDomainDefaultTimeout      = 5 * time.Minute
	userPoolDomainDeleteTimeout       = time.Minute
	userPoolDomainCustomTimeout       = 60 * time.Minute
)

type userPoolDomainAPI interface {
	CreateUserPoolDomain(context.Context, *cognitoidentityprovider.CreateUserPoolDomainInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.CreateUserPoolDomainOutput, error)
	DeleteUserPoolDomain(context.Context, *cognitoidentityprovider.DeleteUserPoolDomainInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.DeleteUserPoolDomainOutput, error)
	DescribeUserPoolDomain(context.Context, *cognitoidentityprovider.DescribeUserPoolDomainInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.DescribeUserPoolDomainOutput, error)
	UpdateUserPoolDomain(context.Context, *cognitoidentityprovider.UpdateUserPoolDomainInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.UpdateUserPoolDomainOutput, error)
}

func (r *UserPoolDomainResource) SchemaVersion() int { return 1 }

func (r *UserPoolDomainResource) ReplaceFields() []string {
	return []string{"domain", "user-pool-id"}
}

func (r *UserPoolDomainResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*UserPoolDomainResourceOutput, error) {
	client, region, err := newDomainClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, region, systemUserPoolClock{})
}

func (r *UserPoolDomainResource) create(
	ctx context.Context,
	client userPoolDomainAPI,
	region string,
	clock userPoolClock,
) (*UserPoolDomainResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	if _, err := client.CreateUserPoolDomain(ctx, r.createDomainInput()); err != nil {
		return nil, fmt.Errorf("create user pool domain %s: %w", r.Domain, err)
	}
	waitTimeout := userPoolDomainPrefixCreateTimeout
	if r.hasCustomCertificate() {
		waitTimeout = userPoolDomainCustomTimeout
	}
	if err := waitUserPoolDomainActive(ctx, client, clock, r.Domain, waitTimeout); err != nil {
		return nil, r.cleanupCreatedDomain(ctx, client, err)
	}
	out, err := r.read(ctx, client, region, r.Domain)
	if err != nil {
		return nil, r.cleanupCreatedDomain(ctx, client, err)
	}
	return out, nil
}

func (r *UserPoolDomainResource) cleanupCreatedDomain(
	ctx context.Context,
	client userPoolDomainAPI,
	original error,
) error {
	cleanupErr := r.delete(ctx, client, &UserPoolDomainResourceOutput{
		Domain: r.Domain, UserPoolID: r.UserPoolID,
	}, systemUserPoolClock{})
	if cleanupErr != nil {
		return errors.Join(original, fmt.Errorf("cleanup user pool domain %s: %w",
			r.Domain, cleanupErr))
	}
	return original
}

func (r *UserPoolDomainResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg],
) (*UserPoolDomainResourceOutput, error) {
	prior := recordedPrior.Outputs
	domain, err := priorUserPoolDomain(prior)
	if err != nil {
		return nil, err
	}
	client, region, err := newDomainClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, region, domain)
}

func (r *UserPoolDomainResource) read(
	ctx context.Context,
	client userPoolDomainAPI,
	region string,
	domain string,
) (*UserPoolDomainResourceOutput, error) {
	described, err := client.DescribeUserPoolDomain(
		ctx,
		&cognitoidentityprovider.DescribeUserPoolDomainInput{Domain: aws.String(domain)},
	)
	if isUserPoolDomainNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe user pool domain %s: %w", domain, err)
	}
	description, err := userPoolDomainDescription("describe user pool domain "+domain, described)
	if err != nil {
		return nil, err
	}
	if description.Status == "" {
		return nil, runtime.ErrNotFound
	}
	poolID := aws.ToString(description.UserPoolId)
	if poolID == "" {
		return nil, fmt.Errorf("describe user pool domain %s: response has no user pool ID", domain)
	}
	return &UserPoolDomainResourceOutput{
		Domain:                       aws.ToString(description.Domain),
		UserPoolID:                   poolID,
		CustomDomainConfig:           userPoolDomainCustomOutput(description.CustomDomainConfig),
		ManagedLoginVersion:          description.ManagedLoginVersion,
		Routing:                      userPoolDomainRoutingOutput(description.Routing),
		AWSAccountID:                 description.AWSAccountId,
		CloudFrontDistribution:       description.CloudFrontDistribution,
		CloudFrontDistributionZoneID: userPoolDomainHostedZoneID(region),
		S3Bucket:                     description.S3Bucket,
		Version:                      description.Version,
	}, nil
}

func (r *UserPoolDomainResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg],
) (*UserPoolDomainResourceOutput, error) {
	client, region, err := newDomainClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, region, prior, systemUserPoolClock{})
}

func (r *UserPoolDomainResource) update(
	ctx context.Context,
	client userPoolDomainAPI,
	region string,
	prior runtime.Prior[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg],
	clock userPoolClock,
) (*UserPoolDomainResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	if err := r.validateUpdate(prior.Inputs); err != nil {
		return nil, err
	}
	domain, err := priorUserPoolDomain(prior.Outputs)
	if err != nil {
		return nil, err
	}
	if !r.domainUpdateNeeded(prior.Inputs, prior.Observed) {
		if prior.Observed != nil {
			return prior.Observed, nil
		}
		return prior.Outputs, nil
	}
	input := r.updateDomainInput(prior.Inputs)
	if _, err := client.UpdateUserPoolDomain(ctx, input); err != nil {
		return nil, fmt.Errorf("update user pool domain %s: %w", domain, err)
	}
	timeout := userPoolDomainDefaultTimeout
	if input.CustomDomainConfig != nil {
		timeout = userPoolDomainCustomTimeout
	}
	if err := waitUserPoolDomainUpdateActive(ctx, client, clock, domain, timeout); err != nil {
		return nil, err
	}
	return r.read(ctx, client, region, domain)
}

func (r *UserPoolDomainResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[UserPoolDomainResource, *UserPoolDomainResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, _, err := newDomainClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior, systemUserPoolClock{})
}

func (r *UserPoolDomainResource) delete(
	ctx context.Context,
	client userPoolDomainAPI,
	prior *UserPoolDomainResourceOutput,
	clock userPoolClock,
) error {
	domain, err := priorUserPoolDomain(prior)
	if err != nil {
		return err
	}
	poolID, err := priorUserPoolDomainPoolID(prior)
	if err != nil {
		return err
	}
	_, err = client.DeleteUserPoolDomain(
		ctx,
		&cognitoidentityprovider.DeleteUserPoolDomainInput{
			Domain:     aws.String(domain),
			UserPoolId: aws.String(poolID),
		},
	)
	if isNoSuchUserPoolDomain(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete user pool domain %s: %w", domain, err)
	}
	if err := waitUserPoolDomainAbsent(ctx, client, clock, domain, userPoolDomainDeleteTimeout); err != nil {
		return fmt.Errorf("wait for user pool domain %s delete: %w", domain, err)
	}
	return nil
}

func newDomainClient(ctx context.Context, cfg *awsCfg) (*cognitoidentityprovider.Client, string, error) {
	loaded, err := awscfgLoad(ctx, cfg)
	if err != nil {
		return nil, "", err
	}
	return cognitoidentityprovider.NewFromConfig(loaded), loaded.Region, nil
}

func awscfgLoad(ctx context.Context, cfg *awsCfg) (aws.Config, error) {
	return awscfg.Load(ctx, cfg)
}

func (r *UserPoolDomainResource) domainUpdateNeeded(
	prior UserPoolDomainResource,
	observed *UserPoolDomainResourceOutput,
) bool {
	if !equalDomainCustomConfig(prior.CustomDomainConfig, r.CustomDomainConfig) ||
		!equalDomainRouting(prior.Routing, r.Routing) {
		return true
	}
	if r.ManagedLoginVersion != nil &&
		!equalInt32Ptr(prior.ManagedLoginVersion, r.ManagedLoginVersion) {
		return true
	}
	if observed == nil {
		return false
	}
	if !equalDomainCustomConfig(outputCustomConfig(observed), r.CustomDomainConfig) ||
		!equalDomainRouting(observed.Routing, r.Routing) {
		return true
	}
	return r.ManagedLoginVersion != nil &&
		!equalInt32Ptr(observed.ManagedLoginVersion, r.ManagedLoginVersion)
}

func outputCustomConfig(
	out *UserPoolDomainResourceOutput,
) *UserPoolDomainCustomDomainConfig {
	if out == nil {
		return nil
	}
	return out.CustomDomainConfig
}

func waitUserPoolDomainActive(
	ctx context.Context,
	client userPoolDomainAPI,
	clock userPoolClock,
	domain string,
	timeout time.Duration,
) error {
	return waitUserPoolDomain(ctx, client, clock, domain, timeout, true, map[cognitotypes.DomainStatusType]bool{
		cognitotypes.DomainStatusTypeCreating: true,
		cognitotypes.DomainStatusTypeUpdating: true,
	})
}

func waitUserPoolDomainUpdateActive(
	ctx context.Context,
	client userPoolDomainAPI,
	clock userPoolClock,
	domain string,
	timeout time.Duration,
) error {
	return waitUserPoolDomain(ctx, client, clock, domain, timeout, true, map[cognitotypes.DomainStatusType]bool{
		cognitotypes.DomainStatusTypeUpdating: true,
	})
}

func waitUserPoolDomainAbsent(
	ctx context.Context,
	client userPoolDomainAPI,
	clock userPoolClock,
	domain string,
	timeout time.Duration,
) error {
	return waitUserPoolDomain(ctx, client, clock, domain, timeout, false, map[cognitotypes.DomainStatusType]bool{
		cognitotypes.DomainStatusTypeUpdating: true,
		cognitotypes.DomainStatusTypeDeleting: true,
	})
}

func waitUserPoolDomain(
	ctx context.Context,
	client userPoolDomainAPI,
	clock userPoolClock,
	domain string,
	timeout time.Duration,
	targetActive bool,
	pending map[cognitotypes.DomainStatusType]bool,
) error {
	deadline := clock.Now().Add(timeout)
	delay := 200 * time.Millisecond
	absent := 0
	for {
		status, isAbsent, err := readUserPoolDomainStatus(ctx, client, domain)
		if err != nil {
			return err
		}
		if isAbsent {
			if !targetActive {
				return nil
			}
			absent++
			if absent > 20 {
				return fmt.Errorf("user pool domain %s absent for 21 observations", domain)
			}
		} else {
			absent = 0
			if targetActive && status == cognitotypes.DomainStatusTypeActive {
				return nil
			}
			if !pending[status] {
				return fmt.Errorf("user pool domain %s status is %q", domain, status)
			}
		}
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			return fmt.Errorf("timed out waiting for user pool domain %s", domain)
		}
		sleep := min(delay, remaining)
		if err := clock.Sleep(ctx, sleep); err != nil {
			return err
		}
		delay = min(delay*2, 10*time.Second)
	}
}

func readUserPoolDomainStatus(
	ctx context.Context,
	client userPoolDomainAPI,
	domain string,
) (cognitotypes.DomainStatusType, bool, error) {
	out, err := client.DescribeUserPoolDomain(
		ctx,
		&cognitoidentityprovider.DescribeUserPoolDomainInput{Domain: aws.String(domain)},
	)
	if isUserPoolDomainNotFound(err) {
		return "", true, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("describe user pool domain %s: %w", domain, err)
	}
	description, err := userPoolDomainDescription("describe user pool domain "+domain, out)
	if errors.Is(err, runtime.ErrNotFound) {
		return "", true, nil
	}
	if err != nil {
		return "", false, err
	}
	if description.Status == "" {
		return "", true, nil
	}
	return description.Status, false, nil
}

func userPoolDomainDescription(
	operation string,
	out *cognitoidentityprovider.DescribeUserPoolDomainOutput,
) (*cognitotypes.DomainDescriptionType, error) {
	if out == nil || out.DomainDescription == nil {
		return nil, runtime.ErrNotFound
	}
	return out.DomainDescription, nil
}

func priorUserPoolDomain(prior *UserPoolDomainResourceOutput) (string, error) {
	if prior == nil || prior.Domain == "" {
		return "", errors.New("prior user pool domain output has no domain")
	}
	return prior.Domain, nil
}

func priorUserPoolDomainPoolID(prior *UserPoolDomainResourceOutput) (string, error) {
	if prior == nil || prior.UserPoolID == "" {
		return "", errors.New("prior user pool domain output has no user pool ID")
	}
	return prior.UserPoolID, nil
}

func isUserPoolDomainNotFound(err error) bool {
	var notFound *cognitotypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func isNoSuchUserPoolDomain(err error) bool {
	var invalid *cognitotypes.InvalidParameterException
	return errors.As(err, &invalid) && strings.Contains(invalid.ErrorMessage(), "No such domain")
}

func userPoolDomainHostedZoneID(region string) string {
	if strings.HasPrefix(region, "cn-") {
		return "Z3RFFRIM2A3IF5"
	}
	return "Z2FDTNDATAQYW2"
}
