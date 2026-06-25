package apigatewayv2

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

const (
	authorizerCreateDefaultTTL = 300
	authorizerDeleteTimeout    = 30 * time.Minute
)

var (
	authorizerARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	authorizerARNRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	authorizerARNAccountPattern   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`)
)

// Authorizer manages an API Gateway v2 authorizer. The authorizer belongs to
// one API for life, so changing api-id replaces it; the name, type, identity
// sources, URI, credentials, payload format, TTL, simple-response flag, and JWT
// configuration update in place with one UpdateAuthorizer patch. Identity
// sources and JWT audiences are set-like: empty strings, duplicate entries, and
// order are ignored before calls reach AWS. Create first reads the parent API so
// an HTTP REQUEST authorizer with identity sources gets API Gateway's 300-second
// cache TTL default when the input omits it.
type Authorizer struct {
	ApiId                          string                      `ub:"api-id"`
	AuthorizerType                 string                      `ub:"authorizer-type"`
	IdentitySources                []string                    `ub:"identity-sources"`
	Name                           string                      `ub:"name"`
	AuthorizerCredentialsArn       *string                     `ub:"authorizer-credentials-arn"`
	AuthorizerPayloadFormatVersion *string                     `ub:"authorizer-payload-format-version"`
	AuthorizerResultTtlInSeconds   *int64                      `ub:"authorizer-result-ttl-in-seconds"`
	AuthorizerUri                  *string                     `ub:"authorizer-uri"`
	EnableSimpleResponses          *bool                       `ub:"enable-simple-responses"`
	JwtConfiguration               *AuthorizerJwtConfiguration `ub:"jwt-configuration"`
}

// AuthorizerJwtConfiguration is the JWT authorizer configuration. Audience is
// treated as a set before it is sent to AWS: empty strings and duplicate entries
// are removed, and order changes do not cause an update.
type AuthorizerJwtConfiguration struct {
	Audience *[]string `ub:"audience"`
	Issuer   *string   `ub:"issuer"`
}

// AuthorizerOutput holds the authorizer identity and the cloud-filled TTL. The
// API id is recorded with the authorizer id because every read and delete needs
// both values, including a delete after api-id has changed in configuration.
type AuthorizerOutput struct {
	ApiId                        string `ub:"api-id"`
	AuthorizerId                 string `ub:"authorizer-id"`
	AuthorizerResultTtlInSeconds int64  `ub:"authorizer-result-ttl-in-seconds"`
}

func (r *Authorizer) SchemaVersion() int { return 1 }

// ReplaceFields lists the one field API Gateway cannot change in place.
func (r *Authorizer) ReplaceFields() []string {
	return []string{"api-id"}
}

// Defaults marks the identity source list optional.
func (r Authorizer) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Optional(r.IdentitySources)}
}

// Constraints declares the local authorizer rules. The ARN syntax and
// character-count bounds are checked in validate because they are not derived
// constraints.
func (r Authorizer) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.AuthorizerType, "JWT", "REQUEST")).
			Message("authorizer-type must be JWT or REQUEST"),
		constraint.Must(constraint.NotEmpty(r.Name)).
			Message("name must not be empty"),
		constraint.When(constraint.Present(r.AuthorizerPayloadFormatVersion)).
			Require(constraint.OneOf(r.AuthorizerPayloadFormatVersion, "1.0", "2.0")).
			Message("authorizer-payload-format-version must be 1.0 or 2.0"),
		constraint.When(constraint.Present(r.AuthorizerResultTtlInSeconds)).
			Require(constraint.AtLeast(r.AuthorizerResultTtlInSeconds, 0),
				constraint.AtMost(r.AuthorizerResultTtlInSeconds, 3600)).
			Message("authorizer-result-ttl-in-seconds must be between 0 and 3600"),
		constraint.When(constraint.Present(r.AuthorizerUri)).
			Require(constraint.NotEmpty(r.AuthorizerUri)).
			Message("authorizer-uri must not be empty"),
	}
}

func (r *Authorizer) validate() error {
	if n := len(r.Name); n < 1 || n > 128 {
		return errors.New("name must be between 1 and 128 bytes")
	}
	if r.AuthorizerUri != nil {
		if n := len(*r.AuthorizerUri); n < 1 || n > 2048 {
			return errors.New("authorizer-uri must be between 1 and 2048 bytes")
		}
	}
	if r.AuthorizerCredentialsArn != nil &&
		!validAuthorizerARN(*r.AuthorizerCredentialsArn) {
		return errors.New("authorizer-credentials-arn must be a valid ARN")
	}
	return nil
}

func (r *Authorizer) Create(ctx context.Context, cfg *awsCfg) (*AuthorizerOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	api, err := r.getParentAPI(ctx, client)
	if err != nil {
		return nil, err
	}
	var resp *apigatewayv2.CreateAuthorizerOutput
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateAuthorizer(ctx, r.createInput(api.ProtocolType))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create authorizer: %w", err)
	}
	if resp == nil || aws.ToString(resp.AuthorizerId) == "" {
		return nil, errors.New("create authorizer: empty response")
	}
	authorizerID := aws.ToString(resp.AuthorizerId)
	read, err := getAuthorizer(ctx, client, r.ApiId, authorizerID)
	if err != nil {
		return nil, fmt.Errorf("get authorizer after create: %w", err)
	}
	if read == nil {
		return nil, errors.New("get authorizer after create: empty response")
	}
	return authorizerOutput(r.ApiId, authorizerID, read), nil
}

func (r *Authorizer) Read(
	ctx context.Context, cfg *awsCfg, prior *AuthorizerOutput,
) (*AuthorizerOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.ApiId, prior.AuthorizerId)
}

func (r *Authorizer) read(
	ctx context.Context, client *apigatewayv2.Client, apiID, authorizerID string,
) (*AuthorizerOutput, error) {
	resp, err := getAuthorizer(ctx, client, apiID, authorizerID)
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get authorizer %s: %w", authorizerID, err)
	}
	if resp == nil {
		return nil, runtime.ErrNotFound
	}
	return authorizerOutput(apiID, authorizerID, resp), nil
}

func (r *Authorizer) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Authorizer, *AuthorizerOutput],
) (*AuthorizerOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in, changed := r.updateInput(prior)
	if !changed {
		if prior.Observed != nil {
			return prior.Observed, nil
		}
		return r.read(ctx, client, prior.Outputs.ApiId, prior.Outputs.AuthorizerId)
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.UpdateAuthorizer(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("update authorizer: %w", err)
	}
	return r.read(ctx, client, prior.Outputs.ApiId, prior.Outputs.AuthorizerId)
}

func (r *Authorizer) Delete(ctx context.Context, cfg *awsCfg, prior *AuthorizerOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	err = retry.OnError(ctx, authorizerDeleteRetryable, func(ctx context.Context) error {
		return withConflictRetry(ctx, func(ctx context.Context) error {
			_, err := client.DeleteAuthorizer(ctx, &apigatewayv2.DeleteAuthorizerInput{
				ApiId:        aws.String(prior.ApiId),
				AuthorizerId: aws.String(prior.AuthorizerId),
			})
			return err
		})
	}, retry.WithTimeout(authorizerDeleteTimeout))
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete authorizer: %w", err)
	}
	return nil
}

func (r *Authorizer) getParentAPI(
	ctx context.Context, client *apigatewayv2.Client,
) (*apigatewayv2.GetApiOutput, error) {
	var resp *apigatewayv2.GetApiOutput
	err := withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.GetApi(ctx, &apigatewayv2.GetApiInput{ApiId: aws.String(r.ApiId)})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get api %s: %w", r.ApiId, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("get api %s: empty response", r.ApiId)
	}
	return resp, nil
}

func (r *Authorizer) createInput(
	protocol apigatewayv2types.ProtocolType,
) *apigatewayv2.CreateAuthorizerInput {
	identitySources := authorizerStringSet(r.IdentitySources)
	in := &apigatewayv2.CreateAuthorizerInput{
		ApiId:          aws.String(r.ApiId),
		AuthorizerType: apigatewayv2types.AuthorizerType(r.AuthorizerType),
		IdentitySource: identitySources,
		Name:           aws.String(r.Name),
	}
	if v := aws.ToString(r.AuthorizerCredentialsArn); v != "" {
		in.AuthorizerCredentialsArn = aws.String(v)
	}
	if v := aws.ToString(r.AuthorizerPayloadFormatVersion); v != "" {
		in.AuthorizerPayloadFormatVersion = aws.String(v)
	}
	if r.AuthorizerResultTtlInSeconds != nil {
		in.AuthorizerResultTtlInSeconds = ptr.Int32(r.AuthorizerResultTtlInSeconds)
	} else if protocol == apigatewayv2types.ProtocolTypeHttp &&
		r.AuthorizerType == string(apigatewayv2types.AuthorizerTypeRequest) &&
		len(identitySources) > 0 {
		in.AuthorizerResultTtlInSeconds = aws.Int32(authorizerCreateDefaultTTL)
	}
	if v := aws.ToString(r.AuthorizerUri); v != "" {
		in.AuthorizerUri = aws.String(v)
	}
	if aws.ToBool(r.EnableSimpleResponses) {
		in.EnableSimpleResponses = r.EnableSimpleResponses
	}
	if r.JwtConfiguration != nil {
		in.JwtConfiguration = r.JwtConfiguration.sdk()
	}
	return in
}

func (r *Authorizer) updateInput(
	prior runtime.Prior[Authorizer, *AuthorizerOutput],
) (*apigatewayv2.UpdateAuthorizerInput, bool) {
	in := &apigatewayv2.UpdateAuthorizerInput{
		ApiId:        aws.String(prior.Outputs.ApiId),
		AuthorizerId: aws.String(prior.Outputs.AuthorizerId),
	}
	changed := false
	if runtime.Changed(prior.Inputs.AuthorizerCredentialsArn, r.AuthorizerCredentialsArn) {
		in.AuthorizerCredentialsArn = aws.String(aws.ToString(r.AuthorizerCredentialsArn))
		changed = true
	}
	if runtime.Changed(
		prior.Inputs.AuthorizerPayloadFormatVersion,
		r.AuthorizerPayloadFormatVersion,
	) {
		in.AuthorizerPayloadFormatVersion =
			aws.String(aws.ToString(r.AuthorizerPayloadFormatVersion))
		changed = true
	}
	if r.ttlNeedsUpdate(prior) {
		in.AuthorizerResultTtlInSeconds =
			aws.Int32(int32(aws.ToInt64(r.AuthorizerResultTtlInSeconds)))
		changed = true
	}
	if runtime.Changed(prior.Inputs.AuthorizerType, r.AuthorizerType) {
		in.AuthorizerType = apigatewayv2types.AuthorizerType(r.AuthorizerType)
		changed = true
	}
	if runtime.Changed(prior.Inputs.AuthorizerUri, r.AuthorizerUri) {
		in.AuthorizerUri = aws.String(aws.ToString(r.AuthorizerUri))
		changed = true
	}
	if runtime.Changed(prior.Inputs.EnableSimpleResponses, r.EnableSimpleResponses) {
		in.EnableSimpleResponses = aws.Bool(aws.ToBool(r.EnableSimpleResponses))
		changed = true
	}
	if authorizerStringSetChanged(prior.Inputs.IdentitySources, r.IdentitySources) {
		in.IdentitySource = authorizerStringSet(r.IdentitySources)
		changed = true
	}
	if authorizerJwtConfigurationChanged(
		prior.Inputs.JwtConfiguration,
		r.JwtConfiguration,
	) {
		in.JwtConfiguration = r.jwtConfigurationUpdate()
		changed = true
	}
	if runtime.Changed(prior.Inputs.Name, r.Name) {
		in.Name = aws.String(r.Name)
		changed = true
	}
	return in, changed
}

func (r *Authorizer) ttlNeedsUpdate(
	prior runtime.Prior[Authorizer, *AuthorizerOutput],
) bool {
	if runtime.Changed(
		prior.Inputs.AuthorizerResultTtlInSeconds,
		r.AuthorizerResultTtlInSeconds,
	) {
		return true
	}
	if r.AuthorizerResultTtlInSeconds == nil || prior.Observed == nil {
		return false
	}
	return prior.Observed.AuthorizerResultTtlInSeconds != *r.AuthorizerResultTtlInSeconds
}

func (r *Authorizer) jwtConfigurationUpdate() *apigatewayv2types.JWTConfiguration {
	if r.JwtConfiguration == nil {
		return &apigatewayv2types.JWTConfiguration{}
	}
	return r.JwtConfiguration.sdk()
}

func (c *AuthorizerJwtConfiguration) sdk() *apigatewayv2types.JWTConfiguration {
	if c == nil {
		return nil
	}
	out := &apigatewayv2types.JWTConfiguration{}
	if audience := authorizerStringSetPointer(c.Audience); len(audience) > 0 {
		out.Audience = audience
	}
	if v := aws.ToString(c.Issuer); v != "" {
		out.Issuer = aws.String(v)
	}
	return out
}

func getAuthorizer(
	ctx context.Context, client *apigatewayv2.Client, apiID, authorizerID string,
) (*apigatewayv2.GetAuthorizerOutput, error) {
	var resp *apigatewayv2.GetAuthorizerOutput
	err := withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.GetAuthorizer(ctx, &apigatewayv2.GetAuthorizerInput{
			ApiId:        aws.String(apiID),
			AuthorizerId: aws.String(authorizerID),
		})
		return err
	})
	return resp, err
}

func authorizerOutput(
	apiID, requestedID string, resp *apigatewayv2.GetAuthorizerOutput,
) *AuthorizerOutput {
	authorizerID := aws.ToString(resp.AuthorizerId)
	if authorizerID == "" {
		authorizerID = requestedID
	}
	return &AuthorizerOutput{
		ApiId:                        apiID,
		AuthorizerId:                 authorizerID,
		AuthorizerResultTtlInSeconds: int64(aws.ToInt32(resp.AuthorizerResultTtlInSeconds)),
	}
}

func authorizerStringSetChanged(prior, current []string) bool {
	return !slices.Equal(authorizerStringSet(prior), authorizerStringSet(current))
}

func authorizerJwtConfigurationChanged(
	prior, current *AuthorizerJwtConfiguration,
) bool {
	if prior == nil || current == nil {
		return prior != current
	}
	if !slices.Equal(authorizerStringSetPointer(prior.Audience),
		authorizerStringSetPointer(current.Audience)) {
		return true
	}
	return aws.ToString(prior.Issuer) != aws.ToString(current.Issuer)
}

func authorizerStringSetPointer(values *[]string) []string {
	if values == nil {
		return []string{}
	}
	return authorizerStringSet(*values)
}

func authorizerStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func authorizerDeleteRetryable(err error) bool {
	var conflict *apigatewayv2types.ConflictException
	return errors.As(err, &conflict)
}

func validAuthorizerARN(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return false
	}
	if !authorizerARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !authorizerARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" && !authorizerARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}
