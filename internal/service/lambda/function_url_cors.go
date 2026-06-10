package lambda

import (
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// FunctionUrlCors holds the cross-origin resource sharing settings a function
// URL answers browser preflight requests with. Every field is optional. The
// block rides CreateFunctionUrlConfig and UpdateFunctionUrlConfig as the Cors
// member; the resource clears a removed block on update by sending the empty
// struct, since a nil member means leave-unchanged.
type FunctionUrlCors struct {
	// AllowCredentials permits cookies and other credentials in requests to
	// the endpoint. The AWS default is false.
	AllowCredentials *bool `ub:"allow-credentials"`
	// AllowHeaders lists the HTTP headers an origin may include in a request,
	// such as Date or X-Custom-Header.
	AllowHeaders *[]string `ub:"allow-headers"`
	// AllowMethods lists the HTTP methods an origin may call the endpoint
	// with: method names such as GET and POST, or the wildcard "*". The API
	// enforces the member set.
	AllowMethods *[]string `ub:"allow-methods"`
	// AllowOrigins lists the origins that may access the endpoint, such as
	// https://www.example.com, or the wildcard "*" for all origins.
	AllowOrigins *[]string `ub:"allow-origins"`
	// ExposeHeaders lists the response headers exposed to calling origins.
	ExposeHeaders *[]string `ub:"expose-headers"`
	// MaxAge is how long, in seconds up to 86400, a browser may cache a
	// preflight result. Unset leaves the AWS default of 0, no caching.
	MaxAge *int64 `ub:"max-age"`
}

// to converts the block to the SDK member. An absent list stays out of the
// request, and so does a declared-but-empty one: each list takes at least one
// item when sent, so empty means not configured rather than a literal empty
// set. A nil block converts to nil, omitting the member entirely.
func (b *FunctionUrlCors) to() *lambdatypes.Cors {
	if b == nil {
		return nil
	}
	return &lambdatypes.Cors{
		AllowCredentials: b.AllowCredentials,
		AllowHeaders:     functionUrlCorsList(b.AllowHeaders),
		AllowMethods:     functionUrlCorsList(b.AllowMethods),
		AllowOrigins:     functionUrlCorsList(b.AllowOrigins),
		ExposeHeaders:    functionUrlCorsList(b.ExposeHeaders),
		MaxAge:           ptr.Int32(b.MaxAge),
	}
}

// functionUrlCorsList returns the items a cors list holds, or nil when the
// list is absent or empty so the member stays out of the request.
func functionUrlCorsList(v *[]string) []string {
	if v == nil || len(*v) == 0 {
		return nil
	}
	return *v
}
