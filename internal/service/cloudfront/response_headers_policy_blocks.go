package cloudfront

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// ResponseHeadersPolicyCors is the cross-origin resource sharing configuration.
// AccessControlAllowCredentials and OriginOverride are required, as are the
// three header lists allow-headers, allow-methods, and allow-origins. Those
// three are present-but-may-be-empty: a block with no items sends Quantity 0,
// which is distinct from omitting the block. expose-headers is optional.
type ResponseHeadersPolicyCors struct {
	AccessControlAllowCredentials *bool                             `ub:"access-control-allow-credentials"`
	AccessControlAllowHeaders     ResponseHeadersPolicyCorsHeaders  `ub:"access-control-allow-headers"`
	AccessControlAllowMethods     ResponseHeadersPolicyCorsMethods  `ub:"access-control-allow-methods"`
	AccessControlAllowOrigins     ResponseHeadersPolicyCorsHeaders  `ub:"access-control-allow-origins"`
	AccessControlExposeHeaders    *ResponseHeadersPolicyCorsHeaders `ub:"access-control-expose-headers"`
	AccessControlMaxAgeSec        *int64                            `ub:"access-control-max-age-sec"`
	OriginOverride                *bool                             `ub:"origin-override"`
}

// ResponseHeadersPolicyCorsHeaders is one of the CORS string lists: allowed
// headers, allowed origins, or exposed headers. The items may be empty, in
// which case the SDK call sends Quantity 0.
type ResponseHeadersPolicyCorsHeaders struct {
	Items []string `ub:"items"`
}

// ResponseHeadersPolicyCorsMethods is the allowed-methods list. CloudFront
// limits the values to GET, POST, OPTIONS, PUT, DELETE, PATCH, HEAD, and ALL,
// which the API enforces through its typed enum.
type ResponseHeadersPolicyCorsMethods struct {
	Items []string `ub:"items"`
}

// ResponseHeadersPolicyCustomHeaders is the set of custom response headers
// CloudFront adds. The items may be empty; the SDK call sends Quantity 0.
type ResponseHeadersPolicyCustomHeaders struct {
	Items []ResponseHeadersPolicyCustomHeader `ub:"items"`
}

// ResponseHeadersPolicyCustomHeader is one custom response header. All three
// fields are required.
type ResponseHeadersPolicyCustomHeader struct {
	Header   string `ub:"header"`
	Value    string `ub:"value"`
	Override *bool  `ub:"override"`
}

// ResponseHeadersPolicyRemoveHeaders is the set of headers CloudFront strips
// from the response. The items may be empty; the SDK call sends Quantity 0.
type ResponseHeadersPolicyRemoveHeaders struct {
	Items []ResponseHeadersPolicyRemoveHeader `ub:"items"`
}

// ResponseHeadersPolicyRemoveHeader is one header name to remove. Header is
// required.
type ResponseHeadersPolicyRemoveHeader struct {
	Header string `ub:"header"`
}

// ResponseHeadersPolicySecurity holds the security-related response headers.
// Every sub-block is optional, and each one has a required override bool.
type ResponseHeadersPolicySecurity struct {
	ContentSecurityPolicy   *ResponseHeadersPolicyContentSecurityPolicy   `ub:"content-security-policy"`
	ContentTypeOptions      *ResponseHeadersPolicyContentTypeOptions      `ub:"content-type-options"`
	FrameOptions            *ResponseHeadersPolicyFrameOptions            `ub:"frame-options"`
	ReferrerPolicy          *ResponseHeadersPolicyReferrerPolicy          `ub:"referrer-policy"`
	StrictTransportSecurity *ResponseHeadersPolicyStrictTransportSecurity `ub:"strict-transport-security"`
	XSSProtection           *ResponseHeadersPolicyXSSProtection           `ub:"xss-protection"`
}

// ResponseHeadersPolicyContentSecurityPolicy sets the Content-Security-Policy
// header. The directive value and override are required.
type ResponseHeadersPolicyContentSecurityPolicy struct {
	ContentSecurityPolicy string `ub:"content-security-policy"`
	Override              *bool  `ub:"override"`
}

// ResponseHeadersPolicyContentTypeOptions sets X-Content-Type-Options to
// nosniff. Override is required.
type ResponseHeadersPolicyContentTypeOptions struct {
	Override *bool `ub:"override"`
}

// ResponseHeadersPolicyFrameOptions sets X-Frame-Options. FrameOption is one of
// DENY or SAMEORIGIN, enforced by the resource's Constraints; override is
// required.
type ResponseHeadersPolicyFrameOptions struct {
	FrameOption string `ub:"frame-option"`
	Override    *bool  `ub:"override"`
}

// ResponseHeadersPolicyReferrerPolicy sets the Referrer-Policy header.
// ReferrerPolicy is one of the eight standard values, enforced by the
// resource's Constraints; override is required.
type ResponseHeadersPolicyReferrerPolicy struct {
	ReferrerPolicy string `ub:"referrer-policy"`
	Override       *bool  `ub:"override"`
}

// ResponseHeadersPolicyStrictTransportSecurity sets Strict-Transport-Security.
// The max-age and override are required; include-subdomains and preload are
// optional directives.
type ResponseHeadersPolicyStrictTransportSecurity struct {
	AccessControlMaxAgeSec *int64 `ub:"access-control-max-age-sec"`
	Override               *bool  `ub:"override"`
	IncludeSubdomains      *bool  `ub:"include-subdomains"`
	Preload                *bool  `ub:"preload"`
}

// ResponseHeadersPolicyXSSProtection sets X-XSS-Protection. Protection and
// override are required; mode-block and report-uri are optional. CloudFront
// rejects a report-uri when mode-block is true.
type ResponseHeadersPolicyXSSProtection struct {
	Protection *bool   `ub:"protection"`
	Override   *bool   `ub:"override"`
	ModeBlock  *bool   `ub:"mode-block"`
	ReportUri  *string `ub:"report-uri"`
}

// ResponseHeadersPolicyServerTiming enables the Server-Timing header. Enabled
// and SamplingRate are required; the sampling rate is a percentage from 0 to
// 100 inclusive.
type ResponseHeadersPolicyServerTiming struct {
	Enabled      *bool    `ub:"enabled"`
	SamplingRate *float64 `ub:"sampling-rate"`
}

// expandCors builds the CORS config sent to CloudFront. The three required
// lists are always present, sending Quantity 0 when empty; expose-headers is
// included only when the block is set.
func expandCors(in *ResponseHeadersPolicyCors) *cloudfronttypes.ResponseHeadersPolicyCorsConfig {
	if in == nil {
		return nil
	}
	out := &cloudfronttypes.ResponseHeadersPolicyCorsConfig{
		AccessControlAllowCredentials: in.AccessControlAllowCredentials,
		OriginOverride:                in.OriginOverride,
		AccessControlMaxAgeSec:        ptr.Int32(in.AccessControlMaxAgeSec),
		AccessControlAllowHeaders: &cloudfronttypes.ResponseHeadersPolicyAccessControlAllowHeaders{
			Items:    in.AccessControlAllowHeaders.Items,
			Quantity: aws.Int32(int32(len(in.AccessControlAllowHeaders.Items))),
		},
		AccessControlAllowMethods: &cloudfronttypes.ResponseHeadersPolicyAccessControlAllowMethods{
			Items:    corsMethods(in.AccessControlAllowMethods.Items),
			Quantity: aws.Int32(int32(len(in.AccessControlAllowMethods.Items))),
		},
		AccessControlAllowOrigins: &cloudfronttypes.ResponseHeadersPolicyAccessControlAllowOrigins{
			Items:    in.AccessControlAllowOrigins.Items,
			Quantity: aws.Int32(int32(len(in.AccessControlAllowOrigins.Items))),
		},
	}
	if in.AccessControlExposeHeaders != nil {
		out.AccessControlExposeHeaders =
			&cloudfronttypes.ResponseHeadersPolicyAccessControlExposeHeaders{
				Items:    in.AccessControlExposeHeaders.Items,
				Quantity: aws.Int32(int32(len(in.AccessControlExposeHeaders.Items))),
			}
	}
	return out
}

// corsMethods converts the allowed-method strings into the SDK enum slice.
func corsMethods(
	in []string,
) []cloudfronttypes.ResponseHeadersPolicyAccessControlAllowMethodsValues {
	out := make([]cloudfronttypes.ResponseHeadersPolicyAccessControlAllowMethodsValues, len(in))
	for i, m := range in {
		out[i] = cloudfronttypes.ResponseHeadersPolicyAccessControlAllowMethodsValues(m)
	}
	return out
}

// expandCustomHeaders builds the custom-headers config, sending Quantity 0 when
// the block is present with no items.
func expandCustomHeaders(
	in *ResponseHeadersPolicyCustomHeaders,
) *cloudfronttypes.ResponseHeadersPolicyCustomHeadersConfig {
	if in == nil {
		return nil
	}
	items := make([]cloudfronttypes.ResponseHeadersPolicyCustomHeader, len(in.Items))
	for i, h := range in.Items {
		items[i] = cloudfronttypes.ResponseHeadersPolicyCustomHeader{
			Header:   aws.String(h.Header),
			Value:    aws.String(h.Value),
			Override: h.Override,
		}
	}
	return &cloudfronttypes.ResponseHeadersPolicyCustomHeadersConfig{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

// expandRemoveHeaders builds the remove-headers config, sending Quantity 0 when
// the block is present with no items.
func expandRemoveHeaders(
	in *ResponseHeadersPolicyRemoveHeaders,
) *cloudfronttypes.ResponseHeadersPolicyRemoveHeadersConfig {
	if in == nil {
		return nil
	}
	items := make([]cloudfronttypes.ResponseHeadersPolicyRemoveHeader, len(in.Items))
	for i, h := range in.Items {
		items[i] = cloudfronttypes.ResponseHeadersPolicyRemoveHeader{
			Header: aws.String(h.Header),
		}
	}
	return &cloudfronttypes.ResponseHeadersPolicyRemoveHeadersConfig{
		Items:    items,
		Quantity: aws.Int32(int32(len(items))),
	}
}

// expandSecurity builds the security-headers config, including each sub-block
// only when set.
func expandSecurity(
	in *ResponseHeadersPolicySecurity,
) *cloudfronttypes.ResponseHeadersPolicySecurityHeadersConfig {
	if in == nil {
		return nil
	}
	out := &cloudfronttypes.ResponseHeadersPolicySecurityHeadersConfig{}
	if in.ContentSecurityPolicy != nil {
		out.ContentSecurityPolicy = &cloudfronttypes.ResponseHeadersPolicyContentSecurityPolicy{
			ContentSecurityPolicy: aws.String(in.ContentSecurityPolicy.ContentSecurityPolicy),
			Override:              in.ContentSecurityPolicy.Override,
		}
	}
	if in.ContentTypeOptions != nil {
		out.ContentTypeOptions = &cloudfronttypes.ResponseHeadersPolicyContentTypeOptions{
			Override: in.ContentTypeOptions.Override,
		}
	}
	if in.FrameOptions != nil {
		out.FrameOptions = &cloudfronttypes.ResponseHeadersPolicyFrameOptions{
			FrameOption: cloudfronttypes.FrameOptionsList(in.FrameOptions.FrameOption),
			Override:    in.FrameOptions.Override,
		}
	}
	if in.ReferrerPolicy != nil {
		out.ReferrerPolicy = &cloudfronttypes.ResponseHeadersPolicyReferrerPolicy{
			ReferrerPolicy: cloudfronttypes.ReferrerPolicyList(in.ReferrerPolicy.ReferrerPolicy),
			Override:       in.ReferrerPolicy.Override,
		}
	}
	if in.StrictTransportSecurity != nil {
		out.StrictTransportSecurity = &cloudfronttypes.ResponseHeadersPolicyStrictTransportSecurity{
			AccessControlMaxAgeSec: ptr.Int32(in.StrictTransportSecurity.AccessControlMaxAgeSec),
			Override:               in.StrictTransportSecurity.Override,
			IncludeSubdomains:      in.StrictTransportSecurity.IncludeSubdomains,
			Preload:                in.StrictTransportSecurity.Preload,
		}
	}
	if in.XSSProtection != nil {
		out.XSSProtection = &cloudfronttypes.ResponseHeadersPolicyXSSProtection{
			Protection: in.XSSProtection.Protection,
			Override:   in.XSSProtection.Override,
			ModeBlock:  in.XSSProtection.ModeBlock,
			ReportUri:  in.XSSProtection.ReportUri,
		}
	}
	return out
}

// expandServerTiming builds the server-timing config when the block is set.
func expandServerTiming(
	in *ResponseHeadersPolicyServerTiming,
) *cloudfronttypes.ResponseHeadersPolicyServerTimingHeadersConfig {
	if in == nil {
		return nil
	}
	return &cloudfronttypes.ResponseHeadersPolicyServerTimingHeadersConfig{
		Enabled:      in.Enabled,
		SamplingRate: in.SamplingRate,
	}
}
