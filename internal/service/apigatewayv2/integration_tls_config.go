package apigatewayv2

import (
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

// IntegrationTlsConfig is the TLS configuration for a private integration,
// supported only for HTTP APIs; declaring it makes private integration
// traffic use HTTPS. When server-name-to-verify is set, API Gateway uses it
// to verify the hostname on the integration's certificate and includes it in
// the TLS handshake for SNI. Removing the block on an update turns the TLS
// configuration off through the documented empty-object clear, since
// omitting the member would leave it unchanged.
type IntegrationTlsConfig struct {
	ServerNameToVerify *string `ub:"server-name-to-verify"`
}

// sdk converts the block to the SDK input member, returning nil for a nil
// block so the caller decides between omitting the member and sending the
// empty-object clear. The server name is included only when non-empty, so a
// block with no name is sent as the empty object.
func (t *IntegrationTlsConfig) sdk() *apigatewayv2types.TlsConfigInput {
	if t == nil {
		return nil
	}
	in := &apigatewayv2types.TlsConfigInput{}
	if t.ServerNameToVerify != nil && *t.ServerNameToVerify != "" {
		in.ServerNameToVerify = t.ServerNameToVerify
	}
	return in
}
