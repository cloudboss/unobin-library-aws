package apigatewayv2

import (
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// ApiCors is the cross-origin resource sharing configuration of an HTTP API.
// All fields are optional; AWS evaluates whatever subset is set. AWS
// normalizes the case of allow-headers and allow-methods on its side, which
// is one reason the block is never echoed to outputs: the normalized form
// would diff forever against the configured one. Removing the whole block
// deletes the CORS configuration from the API; changing any field sends the
// whole block, since the API replaces the entire Cors object on update.
type ApiCors struct {
	AllowCredentials *bool     `ub:"allow-credentials"`
	AllowHeaders     *[]string `ub:"allow-headers"`
	AllowMethods     *[]string `ub:"allow-methods"`
	AllowOrigins     *[]string `ub:"allow-origins"`
	ExposeHeaders    *[]string `ub:"expose-headers"`
	MaxAge           *int64    `ub:"max-age"`
}

// apiCorsToSDK expands the desired CORS block into the SDK type, keeping a
// nil block nil so an unset configuration stays absent from the request.
func apiCorsToSDK(in *ApiCors) *apigatewayv2types.Cors {
	if in == nil {
		return nil
	}
	out := &apigatewayv2types.Cors{
		AllowCredentials: in.AllowCredentials,
		MaxAge:           ptr.Int32(in.MaxAge),
	}
	if in.AllowHeaders != nil {
		out.AllowHeaders = *in.AllowHeaders
	}
	if in.AllowMethods != nil {
		out.AllowMethods = *in.AllowMethods
	}
	if in.AllowOrigins != nil {
		out.AllowOrigins = *in.AllowOrigins
	}
	if in.ExposeHeaders != nil {
		out.ExposeHeaders = *in.ExposeHeaders
	}
	return out
}
