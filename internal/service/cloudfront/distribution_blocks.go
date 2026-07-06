package cloudfront

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// DistributionOrigin is one origin: a place CloudFront fetches content from.
// DomainName and OriginId are required. An origin is exactly one of an S3
// origin (an S3 bucket reached as a REST endpoint) or a custom origin (any
// other HTTP origin, including an S3 website endpoint); when neither block is
// set, an empty S3 origin is sent, since the API requires every origin to name
// one type. OriginAccessControlId links an S3 origin to an origin access
// control. CustomHeaders are extra headers CloudFront adds to every request it
// sends to this origin.
type DistributionOrigin struct {
	DomainName            string                           `ub:"domain-name"`
	OriginId              string                           `ub:"origin-id"`
	OriginPath            *string                          `ub:"origin-path"`
	OriginAccessControlId *string                          `ub:"origin-access-control-id"`
	CustomHeaders         []DistributionOriginCustomHeader `ub:"custom-headers"`
	S3OriginConfig        *DistributionS3OriginConfig      `ub:"s3-origin-config"`
	CustomOriginConfig    *DistributionCustomOriginConfig  `ub:"custom-origin-config"`
}

// DistributionOriginCustomHeader is one header CloudFront adds to requests to
// the origin. Both fields are required.
type DistributionOriginCustomHeader struct {
	HeaderName  string `ub:"header-name"`
	HeaderValue string `ub:"header-value"`
}

// DistributionS3OriginConfig configures an S3 REST origin. OriginAccessIdentity
// is the legacy origin access identity path (empty for a bucket reached through
// an origin access control or a public bucket).
type DistributionS3OriginConfig struct {
	OriginAccessIdentity *string `ub:"origin-access-identity"`
}

// DistributionCustomOriginConfig configures a custom HTTP origin. HTTPPort,
// HTTPSPort, and OriginProtocolPolicy are required; the SSL protocols list and
// the two timeouts are optional and default to CloudFront's own values when
// omitted.
type DistributionCustomOriginConfig struct {
	HTTPPort               *int64   `ub:"http-port"`
	HTTPSPort              *int64   `ub:"https-port"`
	OriginProtocolPolicy   *string  `ub:"origin-protocol-policy"`
	OriginSslProtocols     []string `ub:"origin-ssl-protocols"`
	OriginReadTimeout      *int64   `ub:"origin-read-timeout"`
	OriginKeepaliveTimeout *int64   `ub:"origin-keepalive-timeout"`
}

// DistributionDefaultCacheBehavior is the cache behavior CloudFront applies to
// requests that match no ordered behavior. TargetOriginId,
// ViewerProtocolPolicy, and CachePolicyId are required. CachePolicyId names a
// managed or custom cache policy that controls the cache key and the cache TTLs;
// the deprecated inline forwarded-values path is intentionally not supported.
type DistributionDefaultCacheBehavior struct {
	TargetOriginId             string                                  `ub:"target-origin-id"`
	ViewerProtocolPolicy       string                                  `ub:"viewer-protocol-policy"`
	CachePolicyId              string                                  `ub:"cache-policy-id"`
	AllowedMethods             []string                                `ub:"allowed-methods"`
	CachedMethods              []string                                `ub:"cached-methods"`
	Compress                   *bool                                   `ub:"compress"`
	OriginRequestPolicyId      *string                                 `ub:"origin-request-policy-id"`
	ResponseHeadersPolicyId    *string                                 `ub:"response-headers-policy-id"`
	FunctionAssociations       []DistributionFunctionAssociation       `ub:"function-associations"`
	LambdaFunctionAssociations []DistributionLambdaFunctionAssociation `ub:"lambda-function-associations"`
}

// DistributionCacheBehavior is an ordered cache behavior: the same as the
// default behavior plus the path pattern that selects which requests it
// applies to. PathPattern, TargetOriginId, ViewerProtocolPolicy, and
// CachePolicyId are required.
type DistributionCacheBehavior struct {
	PathPattern                string                                  `ub:"path-pattern"`
	TargetOriginId             string                                  `ub:"target-origin-id"`
	ViewerProtocolPolicy       string                                  `ub:"viewer-protocol-policy"`
	CachePolicyId              string                                  `ub:"cache-policy-id"`
	AllowedMethods             []string                                `ub:"allowed-methods"`
	CachedMethods              []string                                `ub:"cached-methods"`
	Compress                   *bool                                   `ub:"compress"`
	OriginRequestPolicyId      *string                                 `ub:"origin-request-policy-id"`
	ResponseHeadersPolicyId    *string                                 `ub:"response-headers-policy-id"`
	FunctionAssociations       []DistributionFunctionAssociation       `ub:"function-associations"`
	LambdaFunctionAssociations []DistributionLambdaFunctionAssociation `ub:"lambda-function-associations"`
}

// DistributionFunctionAssociation links a CloudFront function to an event in a
// cache behavior. Both fields are required.
type DistributionFunctionAssociation struct {
	EventType   string `ub:"event-type"`
	FunctionArn string `ub:"function-arn"`
}

// DistributionLambdaFunctionAssociation links a Lambda@Edge function to an
// event in a cache behavior. EventType and LambdaArn are required;
// IncludeBody controls whether the request body is exposed to the function.
type DistributionLambdaFunctionAssociation struct {
	EventType   string `ub:"event-type"`
	LambdaArn   string `ub:"lambda-arn"`
	IncludeBody *bool  `ub:"include-body"`
}

// DistributionViewerCertificate is the distribution's TLS configuration toward
// viewers. Exactly one source is named: the default CloudFront certificate, an
// ACM certificate, or an IAM certificate. The SSL support method applies only
// with an ACM or IAM certificate, not the default one.
type DistributionViewerCertificate struct {
	CloudFrontDefaultCertificate *bool   `ub:"cloudfront-default-certificate"`
	ACMCertificateArn            *string `ub:"acm-certificate-arn"`
	IAMCertificateId             *string `ub:"iam-certificate-id"`
	MinimumProtocolVersion       *string `ub:"minimum-protocol-version"`
	SSLSupportMethod             *string `ub:"ssl-support-method"`
}

// DistributionRestrictions holds the content distribution restrictions. The
// only restriction is geographic.
type DistributionRestrictions struct {
	GeoRestriction *DistributionGeoRestriction `ub:"geo-restriction"`
}

// DistributionGeoRestriction restricts which countries content is served to.
// RestrictionType is one of none, whitelist, or blacklist; Locations lists the
// two-letter country codes the restriction applies to.
type DistributionGeoRestriction struct {
	RestrictionType *string  `ub:"restriction-type"`
	Locations       []string `ub:"locations"`
}

// DistributionLogging configures access logging. When the block is set,
// logging is enabled and a bucket is required; Prefix and IncludeCookies are
// optional. When the block is absent, logging is sent disabled.
type DistributionLogging struct {
	Bucket         string  `ub:"bucket"`
	Prefix         *string `ub:"prefix"`
	IncludeCookies *bool   `ub:"include-cookies"`
}

// DistributionCustomErrorResponse customizes how CloudFront handles an origin
// error. ErrorCode is required; the others tailor the response code, the page
// served, and how long the error is cached.
type DistributionCustomErrorResponse struct {
	ErrorCode          *int64  `ub:"error-code"`
	ResponseCode       *string `ub:"response-code"`
	ResponsePagePath   *string `ub:"response-page-path"`
	ErrorCachingMinTTL *int64  `ub:"error-caching-min-ttl"`
}

// expandConfig builds the DistributionConfig sent on create and update. The
// required parts (caller reference, enabled flag, comment, origins, default
// cache behavior, restrictions, viewer certificate, aliases, logging) are
// always present; the optional ones expand only when set. The comment defaults
// to the empty string and aliases and logging to their disabled forms, since
// the API requires those members.
func (r *DistributionResource) expandConfig(
	callerReference string,
) *cloudfronttypes.DistributionConfig {
	return &cloudfronttypes.DistributionConfig{
		CallerReference:      aws.String(callerReference),
		Enabled:              boolOrFalse(r.Enabled),
		Comment:              aws.String(aws.ToString(r.Comment)),
		DefaultRootObject:    r.DefaultRootObject,
		PriceClass:           cloudfronttypes.PriceClass(aws.ToString(r.PriceClass)),
		HttpVersion:          cloudfronttypes.HttpVersion(aws.ToString(r.HttpVersion)),
		IsIPV6Enabled:        r.IsIPV6Enabled,
		WebACLId:             r.WebACLId,
		Origins:              expandOrigins(r.Origins),
		DefaultCacheBehavior: expandDefaultCacheBehavior(r.DefaultCacheBehavior),
		CacheBehaviors:       expandCacheBehaviors(ptr.Value(r.CacheBehaviors)),
		CustomErrorResponses: expandCustomErrorResponses(ptr.Value(r.CustomErrorResponses)),
		Aliases:              expandAliases(ptr.Value(r.Aliases)),
		ViewerCertificate:    expandViewerCertificate(r.ViewerCertificate),
		Restrictions:         expandRestrictions(r.Restrictions),
		Logging:              expandLogging(r.Logging),
	}
}

// expandOrigins builds the origins collection. Quantity is the slice length.
func expandOrigins(in []DistributionOrigin) *cloudfronttypes.Origins {
	items := make([]cloudfronttypes.Origin, len(in))
	for i, o := range in {
		items[i] = expandOrigin(o)
	}
	return &cloudfronttypes.Origins{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

// expandOrigin builds one origin. When the origin names neither an S3 nor a
// custom origin type, an empty S3 origin config is sent, the legal minimal
// form, since the API requires every origin to have exactly one type.
func expandOrigin(o DistributionOrigin) cloudfronttypes.Origin {
	out := cloudfronttypes.Origin{
		DomainName:            aws.String(o.DomainName),
		Id:                    aws.String(o.OriginId),
		OriginPath:            o.OriginPath,
		OriginAccessControlId: o.OriginAccessControlId,
		CustomHeaders:         expandOriginCustomHeaders(o.CustomHeaders),
	}
	switch {
	case o.CustomOriginConfig != nil:
		out.CustomOriginConfig = expandCustomOriginConfig(o.CustomOriginConfig)
	case o.S3OriginConfig != nil:
		out.S3OriginConfig = &cloudfronttypes.S3OriginConfig{
			OriginAccessIdentity: aws.String(aws.ToString(o.S3OriginConfig.OriginAccessIdentity)),
		}
	default:
		out.S3OriginConfig = &cloudfronttypes.S3OriginConfig{
			OriginAccessIdentity: aws.String(""),
		}
	}
	return out
}

// expandOriginCustomHeaders builds the custom-headers collection for an origin.
// Quantity is the slice length, zero when there are none.
func expandOriginCustomHeaders(
	in []DistributionOriginCustomHeader,
) *cloudfronttypes.CustomHeaders {
	items := make([]cloudfronttypes.OriginCustomHeader, len(in))
	for i, h := range in {
		items[i] = cloudfronttypes.OriginCustomHeader{
			HeaderName:  aws.String(h.HeaderName),
			HeaderValue: aws.String(h.HeaderValue),
		}
	}
	return &cloudfronttypes.CustomHeaders{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

// expandCustomOriginConfig builds a custom origin config. The SSL protocols
// list sends Quantity as its length.
func expandCustomOriginConfig(
	in *DistributionCustomOriginConfig,
) *cloudfronttypes.CustomOriginConfig {
	protocolPolicy := cloudfronttypes.OriginProtocolPolicy(aws.ToString(in.OriginProtocolPolicy))
	return &cloudfronttypes.CustomOriginConfig{
		HTTPPort:               ptr.Int32(in.HTTPPort),
		HTTPSPort:              ptr.Int32(in.HTTPSPort),
		OriginProtocolPolicy:   protocolPolicy,
		OriginReadTimeout:      ptr.Int32(in.OriginReadTimeout),
		OriginKeepaliveTimeout: ptr.Int32(in.OriginKeepaliveTimeout),
		OriginSslProtocols:     expandOriginSslProtocols(in.OriginSslProtocols),
	}
}

// expandOriginSslProtocols builds the SSL protocols list, returning nil when
// none are given so the API applies its default.
func expandOriginSslProtocols(in []string) *cloudfronttypes.OriginSslProtocols {
	if len(in) == 0 {
		return nil
	}
	items := make([]cloudfronttypes.SslProtocol, len(in))
	for i, p := range in {
		items[i] = cloudfronttypes.SslProtocol(p)
	}
	return &cloudfronttypes.OriginSslProtocols{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

// expandDefaultCacheBehavior builds the default cache behavior. Caching is
// driven by the required cache policy.
func expandDefaultCacheBehavior(
	b DistributionDefaultCacheBehavior,
) *cloudfronttypes.DefaultCacheBehavior {
	return &cloudfronttypes.DefaultCacheBehavior{
		TargetOriginId:             aws.String(b.TargetOriginId),
		ViewerProtocolPolicy:       cloudfronttypes.ViewerProtocolPolicy(b.ViewerProtocolPolicy),
		AllowedMethods:             expandAllowedMethods(b.AllowedMethods, b.CachedMethods),
		Compress:                   b.Compress,
		CachePolicyId:              aws.String(b.CachePolicyId),
		OriginRequestPolicyId:      emptyToNil(b.OriginRequestPolicyId),
		ResponseHeadersPolicyId:    emptyToNil(b.ResponseHeadersPolicyId),
		FunctionAssociations:       expandFunctionAssociations(b.FunctionAssociations),
		LambdaFunctionAssociations: expandLambdaFunctionAssociations(b.LambdaFunctionAssociations),
	}
}

// expandCacheBehaviors builds the ordered cache behaviors collection. Quantity
// is the slice length.
func expandCacheBehaviors(in []DistributionCacheBehavior) *cloudfronttypes.CacheBehaviors {
	items := make([]cloudfronttypes.CacheBehavior, len(in))
	for i, b := range in {
		items[i] = expandCacheBehavior(b)
	}
	return &cloudfronttypes.CacheBehaviors{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

// expandCacheBehavior builds one ordered cache behavior. Caching is driven by
// the required cache policy.
func expandCacheBehavior(b DistributionCacheBehavior) cloudfronttypes.CacheBehavior {
	return cloudfronttypes.CacheBehavior{
		PathPattern:                aws.String(b.PathPattern),
		TargetOriginId:             aws.String(b.TargetOriginId),
		ViewerProtocolPolicy:       cloudfronttypes.ViewerProtocolPolicy(b.ViewerProtocolPolicy),
		AllowedMethods:             expandAllowedMethods(b.AllowedMethods, b.CachedMethods),
		Compress:                   b.Compress,
		CachePolicyId:              aws.String(b.CachePolicyId),
		OriginRequestPolicyId:      emptyToNil(b.OriginRequestPolicyId),
		ResponseHeadersPolicyId:    emptyToNil(b.ResponseHeadersPolicyId),
		FunctionAssociations:       expandFunctionAssociations(b.FunctionAssociations),
		LambdaFunctionAssociations: expandLambdaFunctionAssociations(b.LambdaFunctionAssociations),
	}
}

// expandAllowedMethods builds the allowed-methods collection, with the cached
// methods nested inside it. It returns nil when no allowed methods are given so
// the API applies its default. The cached methods default to the allowed
// methods when not given.
func expandAllowedMethods(allowed, cached []string) *cloudfronttypes.AllowedMethods {
	if len(allowed) == 0 {
		return nil
	}
	cachedList := cached
	if len(cachedList) == 0 {
		cachedList = allowed
	}
	return &cloudfronttypes.AllowedMethods{
		Items:    methods(allowed),
		Quantity: aws.Int32(int32(len(allowed))),
		CachedMethods: &cloudfronttypes.CachedMethods{
			Items:    methods(cachedList),
			Quantity: aws.Int32(int32(len(cachedList))),
		},
	}
}

// methods converts the method strings into the SDK enum slice.
func methods(in []string) []cloudfronttypes.Method {
	out := make([]cloudfronttypes.Method, len(in))
	for i, m := range in {
		out[i] = cloudfronttypes.Method(m)
	}
	return out
}

// expandFunctionAssociations builds the CloudFront function associations.
// Quantity is the slice length.
func expandFunctionAssociations(
	in []DistributionFunctionAssociation,
) *cloudfronttypes.FunctionAssociations {
	items := make([]cloudfronttypes.FunctionAssociation, len(in))
	for i, f := range in {
		items[i] = cloudfronttypes.FunctionAssociation{
			EventType:   cloudfronttypes.EventType(f.EventType),
			FunctionARN: aws.String(f.FunctionArn),
		}
	}
	return &cloudfronttypes.FunctionAssociations{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

// expandLambdaFunctionAssociations builds the Lambda@Edge function
// associations. Quantity is the slice length.
func expandLambdaFunctionAssociations(
	in []DistributionLambdaFunctionAssociation,
) *cloudfronttypes.LambdaFunctionAssociations {
	items := make([]cloudfronttypes.LambdaFunctionAssociation, len(in))
	for i, f := range in {
		items[i] = cloudfronttypes.LambdaFunctionAssociation{
			EventType:         cloudfronttypes.EventType(f.EventType),
			LambdaFunctionARN: aws.String(f.LambdaArn),
			IncludeBody:       f.IncludeBody,
		}
	}
	return &cloudfronttypes.LambdaFunctionAssociations{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

// expandCustomErrorResponses builds the custom error responses collection.
// Quantity is the slice length.
func expandCustomErrorResponses(
	in []DistributionCustomErrorResponse,
) *cloudfronttypes.CustomErrorResponses {
	items := make([]cloudfronttypes.CustomErrorResponse, len(in))
	for i, e := range in {
		items[i] = cloudfronttypes.CustomErrorResponse{
			ErrorCode:          ptr.Int32(e.ErrorCode),
			ResponseCode:       e.ResponseCode,
			ResponsePagePath:   e.ResponsePagePath,
			ErrorCachingMinTTL: e.ErrorCachingMinTTL,
		}
	}
	return &cloudfronttypes.CustomErrorResponses{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

// expandAliases builds the aliases collection. It is always present, sending
// Quantity 0 with no items when no alternate domain names are configured, since
// the API requires the member.
func expandAliases(in []string) *cloudfronttypes.Aliases {
	return &cloudfronttypes.Aliases{
		Items:    in,
		Quantity: aws.Int32(int32(len(in))),
	}
}

// expandViewerCertificate builds the viewer certificate. When the block is
// absent, the default CloudFront certificate is used. Exactly one source is
// sent, preferring an IAM certificate, then an ACM certificate, then the
// default; the SSL support method and minimum protocol version accompany an ACM
// or IAM certificate.
func expandViewerCertificate(
	in *DistributionViewerCertificate,
) *cloudfronttypes.ViewerCertificate {
	if in == nil {
		return &cloudfronttypes.ViewerCertificate{
			CloudFrontDefaultCertificate: aws.Bool(true),
		}
	}
	out := &cloudfronttypes.ViewerCertificate{}
	switch {
	case in.IAMCertificateId != nil:
		out.IAMCertificateId = in.IAMCertificateId
	case in.ACMCertificateArn != nil:
		out.ACMCertificateArn = in.ACMCertificateArn
	default:
		out.CloudFrontDefaultCertificate = aws.Bool(true)
	}
	if out.CloudFrontDefaultCertificate == nil {
		if in.SSLSupportMethod != nil {
			out.SSLSupportMethod = cloudfronttypes.SSLSupportMethod(*in.SSLSupportMethod)
		}
		if in.MinimumProtocolVersion != nil {
			out.MinimumProtocolVersion =
				cloudfronttypes.MinimumProtocolVersion(*in.MinimumProtocolVersion)
		}
	}
	return out
}

// expandRestrictions builds the restrictions. When the block is absent, an
// empty geo restriction of type none is sent, since the API requires a geo
// restriction whenever restrictions are present and the config always sends
// restrictions.
func expandRestrictions(
	in *DistributionRestrictions,
) *cloudfronttypes.Restrictions {
	geo := &cloudfronttypes.GeoRestriction{
		RestrictionType: cloudfronttypes.GeoRestrictionTypeNone,
		Quantity:        aws.Int32(0),
	}
	if in != nil && in.GeoRestriction != nil {
		g := in.GeoRestriction
		if g.RestrictionType != nil {
			geo.RestrictionType = cloudfronttypes.GeoRestrictionType(*g.RestrictionType)
		}
		geo.Items = g.Locations
		geo.Quantity = aws.Int32(int32(len(g.Locations)))
	}
	return &cloudfronttypes.Restrictions{GeoRestriction: geo}
}

// expandLogging builds the logging config. When the block is absent, logging is
// sent disabled with empty bucket and prefix, since the API requires the
// member.
func expandLogging(in *DistributionLogging) *cloudfronttypes.LoggingConfig {
	if in == nil {
		return &cloudfronttypes.LoggingConfig{
			Enabled:        aws.Bool(false),
			IncludeCookies: aws.Bool(false),
			Bucket:         aws.String(""),
			Prefix:         aws.String(""),
		}
	}
	return &cloudfronttypes.LoggingConfig{
		Enabled:        aws.Bool(true),
		Bucket:         aws.String(in.Bucket),
		Prefix:         aws.String(aws.ToString(in.Prefix)),
		IncludeCookies: aws.Bool(aws.ToBool(in.IncludeCookies)),
	}
}

// boolOrFalse returns the pointed-at bool, or false when the pointer is nil, for
// a required API bool that has no meaningful unset state.
func boolOrFalse(b *bool) *bool {
	if b == nil {
		return aws.Bool(false)
	}
	return b
}

// emptyToNil returns nil for a nil or empty string pointer, so an unset or
// blank policy id is omitted rather than sent as an empty string the API
// rejects.
func emptyToNil(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}
