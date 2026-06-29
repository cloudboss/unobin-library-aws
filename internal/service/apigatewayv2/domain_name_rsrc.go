package apigatewayv2

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

const (
	domainNameCreateTimeout = 10 * time.Minute
	domainNameUpdateTimeout = 60 * time.Minute
	domainNamePollInterval  = 5 * time.Second
	domainNameNotFoundLimit = 20
)

var (
	domainNameARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	domainNameARNRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	domainNameARNAccountPattern   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`)
)

// DomainName manages an API Gateway v2 custom domain and its regional endpoint
// configuration. API Gateway accepts exactly one domain-name-configuration, so
// the list length and the ARN syntax are checked in validate. Endpoint-type,
// security-policy, and routing-mode are also checked there because the service
// accepts the documented values case-insensitively, which the constraint layer
// cannot express compactly.
type DomainName struct {
	// DomainName is the custom host name and the resource identity. It must be 1
	// to 512 characters, counted in validate because AWS counts characters and
	// the constraint layer counts bytes.
	DomainName string `ub:"domain-name"`
	// DomainNameConfigurations holds exactly one regional configuration. The
	// list form follows the SDK and CloudFormation member name.
	DomainNameConfigurations []DomainNameConfiguration `ub:"domain-name-configurations"`
	// MutualTlsAuthentication enables mutual TLS with an S3 truststore. Removing
	// the block sends the service's empty truststore-uri sentinel, which disables
	// mutual TLS instead of leaving the existing setting alone.
	MutualTlsAuthentication *DomainNameMutualTlsAuthentication `ub:"mutual-tls-authentication"`
	// RoutingMode selects how the domain routes requests. Omitted on create lets
	// the service use its default, API_MAPPING_ONLY.
	RoutingMode *string `ub:"routing-mode"`
	// Tags label the domain. Keys with the aws: prefix are ignored when sending
	// creates and updates, matching AWS system-tag behavior.
	Tags *map[string]string `ub:"tags"`
}

// DomainNameConfiguration is the one regional endpoint configuration of a
// custom domain. CertificateArn and OwnershipVerificationCertificateArn must be
// valid ARNs; validate checks them because ARN syntax is not a derived
// constraint.
type DomainNameConfiguration struct {
	CertificateArn                      string  `ub:"certificate-arn"`
	EndpointType                        string  `ub:"endpoint-type"`
	IpAddressType                       *string `ub:"ip-address-type"`
	OwnershipVerificationCertificateArn *string `ub:"ownership-verification-certificate-arn"`
	SecurityPolicy                      string  `ub:"security-policy"`
}

// DomainNameMutualTlsAuthentication names the truststore API Gateway uses to
// authenticate clients.
type DomainNameMutualTlsAuthentication struct {
	TruststoreUri     string  `ub:"truststore-uri"`
	TruststoreVersion *string `ub:"truststore-version"`
}

// DomainNameOutput holds the domain identity and the values API Gateway fills
// after creation. ApiGatewayDomainName and HostedZoneId are the Route 53 alias
// target values. TargetDomainName repeats ApiGatewayDomainName under the Route
// 53 alias name. Arn is composed client-side because GetDomainName does not
// return the no-account apigateway ARN that tag calls require. Tags are the
// user-managed tags API Gateway reports, with AWS system tags removed.
type DomainNameOutput struct {
	DomainName                          string            `ub:"domain-name"`
	Arn                                 string            `ub:"arn"`
	ApiGatewayDomainName                string            `ub:"api-gateway-domain-name"`
	TargetDomainName                    string            `ub:"target-domain-name"`
	HostedZoneId                        string            `ub:"hosted-zone-id"`
	ApiMappingSelectionExpression       string            `ub:"api-mapping-selection-expression"`
	DomainNameStatus                    string            `ub:"domain-name-status"`
	DomainNameStatusMessage             string            `ub:"domain-name-status-message"`
	IpAddressType                       string            `ub:"ip-address-type"`
	OwnershipVerificationCertificateArn string            `ub:"ownership-verification-certificate-arn"`
	RoutingMode                         string            `ub:"routing-mode"`
	Tags                                map[string]string `ub:"tags"`
}

func (r *DomainName) SchemaVersion() int { return 1 }

// ReplaceFields lists the domain identity, the one field API Gateway cannot
// change in place.
func (r *DomainName) ReplaceFields() []string {
	return []string{"domain-name"}
}

// Constraints declares list-size and enum rules the schema can express exactly.
// The other enum fields accept case-insensitive values and are checked in validate.
func (r DomainName) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(
			constraint.MinItems(r.DomainNameConfigurations, 1),
			constraint.MaxItems(r.DomainNameConfigurations, 1)).
			Message("domain-name-configurations must have exactly one item"),
		constraint.ForEach(r.DomainNameConfigurations,
			func(e DomainNameConfiguration) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(e.IpAddressType)).
						Require(constraint.OneOf(e.IpAddressType, "ipv4", "dualstack")).
						Message("domain-name-configurations ip-address-type must be " +
							"ipv4 or dualstack"),
				}
			}),
	}
}

func (r *DomainName) validate() error {
	if n := utf8.RuneCountInString(r.DomainName); n < 1 || n > 512 {
		return errors.New("domain-name must be between 1 and 512 characters")
	}
	if len(r.DomainNameConfigurations) != 1 {
		return errors.New("domain-name-configurations must have exactly one item")
	}
	for i, config := range r.DomainNameConfigurations {
		if !validDomainNameARN(config.CertificateArn) {
			return fmt.Errorf(
				"domain-name-configurations[%d].certificate-arn must be a valid ARN", i)
		}
		if _, ok := canonicalDomainNameEndpointType(config.EndpointType); !ok {
			return fmt.Errorf(
				"domain-name-configurations[%d].endpoint-type must be REGIONAL", i)
		}
		if _, ok := canonicalDomainNameSecurityPolicy(config.SecurityPolicy); !ok {
			return fmt.Errorf(
				"domain-name-configurations[%d].security-policy must be TLS_1_2", i)
		}
		if config.OwnershipVerificationCertificateArn != nil &&
			!validDomainNameARN(*config.OwnershipVerificationCertificateArn) {
			return fmt.Errorf("domain-name-configurations[%d]."+
				"ownership-verification-certificate-arn must be a valid ARN", i)
		}
	}
	if r.RoutingMode != nil {
		if _, ok := canonicalDomainNameRoutingMode(*r.RoutingMode); !ok {
			return errors.New("routing-mode must be API_MAPPING_ONLY, ROUTING_RULE_ONLY, " +
				"or ROUTING_RULE_THEN_API_MAPPING")
		}
	}
	return nil
}

func (r *DomainName) Create(ctx context.Context, cfg *awsCfg) (*DomainNameOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &apigatewayv2.CreateDomainNameInput{
		DomainName:               aws.String(r.DomainName),
		DomainNameConfigurations: domainNameConfigurations(r.DomainNameConfigurations),
		MutualTlsAuthentication:  r.MutualTlsAuthentication.sdk(),
	}
	if r.RoutingMode != nil {
		in.RoutingMode = domainNameRoutingMode(*r.RoutingMode)
	}
	if tags := domainNameUserTags(ptr.Value(r.Tags)); len(tags) > 0 {
		in.Tags = tags
	}
	var resp *apigatewayv2.CreateDomainNameOutput
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateDomainName(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create domain name: %w", err)
	}
	domainName := r.DomainName
	if resp != nil && aws.ToString(resp.DomainName) != "" {
		domainName = aws.ToString(resp.DomainName)
	}
	if err := waitDomainNameAvailable(ctx, client, domainName, domainNameCreateTimeout); err != nil {
		r.deleteAfterCreateFailure(ctx, client, domainName)
		return nil, err
	}
	out, err := r.read(ctx, client, domainName)
	if err != nil {
		r.deleteAfterCreateFailure(ctx, client, domainName)
		return nil, err
	}
	return out, nil
}

func (r *DomainName) Read(
	ctx context.Context, cfg *awsCfg, prior *DomainNameOutput,
) (*DomainNameOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.DomainName)
}

func (r *DomainName) read(
	ctx context.Context, client *apigatewayv2.Client, domainName string,
) (*DomainNameOutput, error) {
	resp, err := getDomainName(ctx, client, domainName)
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get domain name %s: %w", domainName, err)
	}
	if resp == nil || len(resp.DomainNameConfigurations) == 0 {
		return nil, runtime.ErrNotFound
	}
	return domainNameOutput(client.Options().Region, domainName, resp), nil
}

func (r *DomainName) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[DomainName, *DomainNameOutput],
) (*DomainNameOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	domainName := prior.Outputs.DomainName
	if r.tagsNeedSync(prior) {
		arn := domainNameARN(client.Options().Region, domainName)
		desiredTags := domainNameUserTags(ptr.Value(r.Tags))
		err := withConflictRetry(ctx, func(ctx context.Context) error {
			return syncResourceTags(ctx, client, arn, desiredTags)
		})
		if err != nil {
			return nil, fmt.Errorf("sync domain name tags: %w", err)
		}
	}
	if in, changed := r.updateDomainNameInput(prior, domainName); changed {
		err := withConflictRetry(ctx, func(ctx context.Context) error {
			_, err := client.UpdateDomainName(ctx, in)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("update domain name: %w", err)
		}
		if err := waitDomainNameAvailable(ctx, client, domainName, domainNameUpdateTimeout); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, domainName)
}

func (r *DomainName) Delete(ctx context.Context, cfg *awsCfg, prior *DomainNameOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.DeleteDomainName(ctx, &apigatewayv2.DeleteDomainNameInput{
			DomainName: aws.String(prior.DomainName),
		})
		return err
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete domain name: %w", err)
	}
	return nil
}

func (r *DomainName) tagsNeedSync(
	prior runtime.Prior[DomainName, *DomainNameOutput],
) bool {
	desired := domainNameUserTags(ptr.Value(r.Tags))
	if !maps.Equal(domainNameUserTags(ptr.Value(prior.Inputs.Tags)), desired) {
		return true
	}
	return prior.Observed != nil && !maps.Equal(domainNameUserTags(prior.Observed.Tags), desired)
}

func (r *DomainName) updateDomainNameInput(
	prior runtime.Prior[DomainName, *DomainNameOutput], domainName string,
) (*apigatewayv2.UpdateDomainNameInput, bool) {
	configChanged := runtime.Changed(
		prior.Inputs.DomainNameConfigurations, r.DomainNameConfigurations)
	mutualTLSChanged := runtime.Changed(
		prior.Inputs.MutualTlsAuthentication, r.MutualTlsAuthentication)
	routingChanged := runtime.Changed(prior.Inputs.RoutingMode, r.RoutingMode)
	configDrifted := r.configuredConfigurationDrifted(prior.Observed)
	routingDrifted := r.configuredRoutingModeDrifted(prior.Observed)
	if !(configChanged || mutualTLSChanged || routingChanged ||
		configDrifted || routingDrifted) {
		return nil, false
	}
	in := &apigatewayv2.UpdateDomainNameInput{
		DomainName:               aws.String(domainName),
		DomainNameConfigurations: domainNameConfigurations(r.DomainNameConfigurations),
	}
	if mutualTLSChanged {
		in.MutualTlsAuthentication = r.mutualTLSUpdate(
			prior.Inputs.MutualTlsAuthentication)
	}
	if routingChanged || routingDrifted {
		in.RoutingMode = r.updateRoutingMode()
	}
	return in, true
}

func (r *DomainName) configuredConfigurationDrifted(observed *DomainNameOutput) bool {
	if observed == nil || len(r.DomainNameConfigurations) == 0 {
		return false
	}
	config := r.DomainNameConfigurations[0]
	if config.IpAddressType != nil &&
		observed.IpAddressType != aws.ToString(config.IpAddressType) {
		return true
	}
	if config.OwnershipVerificationCertificateArn != nil &&
		observed.OwnershipVerificationCertificateArn !=
			aws.ToString(config.OwnershipVerificationCertificateArn) {
		return true
	}
	return false
}

func (r *DomainName) configuredRoutingModeDrifted(observed *DomainNameOutput) bool {
	if observed == nil || r.RoutingMode == nil {
		return false
	}
	desired, ok := canonicalDomainNameRoutingMode(*r.RoutingMode)
	return ok && observed.RoutingMode != desired
}

func (r *DomainName) mutualTLSUpdate(
	prior *DomainNameMutualTlsAuthentication,
) *apigatewayv2types.MutualTlsAuthenticationInput {
	if r.MutualTlsAuthentication == nil {
		return &apigatewayv2types.MutualTlsAuthenticationInput{
			TruststoreUri: aws.String(""),
		}
	}
	return r.MutualTlsAuthentication.sdkChanged(prior)
}

func (r *DomainName) updateRoutingMode() apigatewayv2types.RoutingMode {
	if r.RoutingMode == nil {
		return apigatewayv2types.RoutingMode("API_MAPPING_ONLY")
	}
	return domainNameRoutingMode(*r.RoutingMode)
}

func (r *DomainName) deleteAfterCreateFailure(
	ctx context.Context, client *apigatewayv2.Client, domainName string,
) {
	_ = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.DeleteDomainName(ctx, &apigatewayv2.DeleteDomainNameInput{
			DomainName: aws.String(domainName),
		})
		return err
	})
}

func (c DomainNameConfiguration) sdk() apigatewayv2types.DomainNameConfiguration {
	endpointType, _ := canonicalDomainNameEndpointType(c.EndpointType)
	securityPolicy, _ := canonicalDomainNameSecurityPolicy(c.SecurityPolicy)
	out := apigatewayv2types.DomainNameConfiguration{
		CertificateArn:                      aws.String(c.CertificateArn),
		EndpointType:                        apigatewayv2types.EndpointType(endpointType),
		IpAddressType:                       domainNameIPAddressType(c.IpAddressType),
		OwnershipVerificationCertificateArn: c.OwnershipVerificationCertificateArn,
		SecurityPolicy:                      apigatewayv2types.SecurityPolicy(securityPolicy),
	}
	return out
}

func domainNameConfigurations(
	configs []DomainNameConfiguration,
) []apigatewayv2types.DomainNameConfiguration {
	if len(configs) == 0 {
		return nil
	}
	out := make([]apigatewayv2types.DomainNameConfiguration, 0, len(configs))
	for _, config := range configs {
		out = append(out, config.sdk())
	}
	return out
}

func (m *DomainNameMutualTlsAuthentication) sdk() *apigatewayv2types.MutualTlsAuthenticationInput {
	if m == nil {
		return nil
	}
	return &apigatewayv2types.MutualTlsAuthenticationInput{
		TruststoreUri:     aws.String(m.TruststoreUri),
		TruststoreVersion: m.TruststoreVersion,
	}
}

func (m *DomainNameMutualTlsAuthentication) sdkChanged(
	prior *DomainNameMutualTlsAuthentication,
) *apigatewayv2types.MutualTlsAuthenticationInput {
	in := &apigatewayv2types.MutualTlsAuthenticationInput{}
	if prior == nil || prior.TruststoreUri != m.TruststoreUri {
		in.TruststoreUri = aws.String(m.TruststoreUri)
	}
	var priorVersion *string
	if prior != nil {
		priorVersion = prior.TruststoreVersion
	}
	if runtime.Changed(priorVersion, m.TruststoreVersion) {
		in.TruststoreVersion = aws.String(aws.ToString(m.TruststoreVersion))
	}
	return in
}

type domainNameGetter func(context.Context) (*apigatewayv2.GetDomainNameOutput, error)

func waitDomainNameAvailable(
	ctx context.Context, client *apigatewayv2.Client, domainName string, timeout time.Duration,
) error {
	return waitDomainNameAvailableWithGetter(
		ctx, domainName, timeout, domainNamePollInterval,
		func(ctx context.Context) (*apigatewayv2.GetDomainNameOutput, error) {
			return getDomainName(ctx, client, domainName)
		})
}

func waitDomainNameAvailableWithGetter(
	ctx context.Context,
	domainName string,
	timeout time.Duration,
	interval time.Duration,
	get domainNameGetter,
) error {
	deadline := time.Now().Add(timeout)
	notFoundCount := 0
	lastStatus := ""
	lastMessage := ""
	for {
		resp, err := get(ctx)
		if err != nil {
			if !isNotFound(err) {
				return fmt.Errorf("get domain name %s while waiting: %w", domainName, err)
			}
			notFoundCount++
			if notFoundCount > domainNameNotFoundLimit {
				return fmt.Errorf("domain name %s was not visible after %d checks",
					domainName, domainNameNotFoundLimit)
			}
		} else if resp == nil || len(resp.DomainNameConfigurations) == 0 {
			notFoundCount++
			if notFoundCount > domainNameNotFoundLimit {
				return fmt.Errorf("domain name %s returned no configurations after %d checks",
					domainName, domainNameNotFoundLimit)
			}
		} else {
			notFoundCount = 0
			config := resp.DomainNameConfigurations[0]
			lastStatus = string(config.DomainNameStatus)
			lastMessage = aws.ToString(config.DomainNameStatusMessage)
			switch config.DomainNameStatus {
			case apigatewayv2types.DomainNameStatus("AVAILABLE"):
				return nil
			case apigatewayv2types.DomainNameStatus("UPDATING"):
			default:
				return domainNameStatusError(domainName, lastStatus, lastMessage)
			}
		}
		if time.Now().After(deadline) {
			return domainNameWaitTimeoutError(domainName, lastStatus, lastMessage)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func getDomainName(
	ctx context.Context, client *apigatewayv2.Client, domainName string,
) (*apigatewayv2.GetDomainNameOutput, error) {
	var resp *apigatewayv2.GetDomainNameOutput
	err := withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.GetDomainName(ctx, &apigatewayv2.GetDomainNameInput{
			DomainName: aws.String(domainName),
		})
		return err
	})
	return resp, err
}

func domainNameOutput(
	region, requestedName string, resp *apigatewayv2.GetDomainNameOutput,
) *DomainNameOutput {
	config := resp.DomainNameConfigurations[0]
	domainName := aws.ToString(resp.DomainName)
	if domainName == "" {
		domainName = requestedName
	}
	apiGatewayDomainName := aws.ToString(config.ApiGatewayDomainName)
	return &DomainNameOutput{
		DomainName:                          domainName,
		Arn:                                 domainNameARN(region, domainName),
		ApiGatewayDomainName:                apiGatewayDomainName,
		TargetDomainName:                    apiGatewayDomainName,
		HostedZoneId:                        aws.ToString(config.HostedZoneId),
		ApiMappingSelectionExpression:       aws.ToString(resp.ApiMappingSelectionExpression),
		DomainNameStatus:                    string(config.DomainNameStatus),
		DomainNameStatusMessage:             aws.ToString(config.DomainNameStatusMessage),
		IpAddressType:                       string(config.IpAddressType),
		OwnershipVerificationCertificateArn: aws.ToString(config.OwnershipVerificationCertificateArn),
		RoutingMode:                         string(resp.RoutingMode),
		Tags:                                domainNameOutputTags(resp.Tags),
	}
}

func domainNameOutputTags(tags map[string]string) map[string]string {
	userTags := domainNameUserTags(tags)
	if userTags == nil {
		return map[string]string{}
	}
	return userTags
}

func domainNameStatusError(domainName, status, message string) error {
	if message != "" {
		return fmt.Errorf("domain name %s status is %s: %s", domainName, status, message)
	}
	return fmt.Errorf("domain name %s status is %s", domainName, status)
}

func domainNameWaitTimeoutError(domainName, status, message string) error {
	if status == "" {
		return fmt.Errorf("timed out waiting for domain name %s to become AVAILABLE", domainName)
	}
	if message != "" {
		return fmt.Errorf("timed out waiting for domain name %s to become AVAILABLE "+
			"(last status %s: %s)", domainName, status, message)
	}
	return fmt.Errorf("timed out waiting for domain name %s to become AVAILABLE "+
		"(last status %s)", domainName, status)
}

func domainNameARN(region, domainName string) string {
	return fmt.Sprintf("arn:%s:apigateway:%s::/domainnames/%s",
		partition.Of(region), region, domainName)
}

func domainNameUserTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func domainNameIPAddressType(value *string) apigatewayv2types.IpAddressType {
	if value == nil {
		return ""
	}
	return apigatewayv2types.IpAddressType(*value)
}

func domainNameRoutingMode(value string) apigatewayv2types.RoutingMode {
	mode, _ := canonicalDomainNameRoutingMode(value)
	return apigatewayv2types.RoutingMode(mode)
}

func canonicalDomainNameEndpointType(value string) (string, bool) {
	return canonicalDomainNameEnum(value, "REGIONAL")
}

func canonicalDomainNameSecurityPolicy(value string) (string, bool) {
	return canonicalDomainNameEnum(value, "TLS_1_2")
}

func canonicalDomainNameRoutingMode(value string) (string, bool) {
	return canonicalDomainNameEnum(value,
		"API_MAPPING_ONLY", "ROUTING_RULE_ONLY", "ROUTING_RULE_THEN_API_MAPPING")
}

func canonicalDomainNameEnum(value string, allowed ...string) (string, bool) {
	for _, item := range allowed {
		if strings.EqualFold(value, item) {
			return item, true
		}
	}
	return "", false
}

func validDomainNameARN(value string) bool {
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return false
	}
	if !domainNameARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !domainNameARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" && !domainNameARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}
