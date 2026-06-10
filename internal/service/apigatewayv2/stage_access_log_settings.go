package apigatewayv2

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

// StageAccessLogSettings is the stage's access logging configuration. Both
// members are required while the block is present.
type StageAccessLogSettings struct {
	// DestinationArn is the ARN of the CloudWatch Logs log group that
	// receives the access logs.
	DestinationArn string `ub:"destination-arn"`
	// Format is the single-line log format, built from $context
	// variables; the service requires at least $context.requestId.
	Format string `ub:"format"`
}

// expand converts the block to the SDK type.
func (s StageAccessLogSettings) expand() *apigatewayv2types.AccessLogSettings {
	return &apigatewayv2types.AccessLogSettings{
		DestinationArn: aws.String(s.DestinationArn),
		Format:         aws.String(s.Format),
	}
}
