package apigatewayv2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// StageDefaultRouteSettings is the stage-wide default for route settings,
// applied to every route without settings of its own.
type StageDefaultRouteSettings struct {
	// DataTraceEnabled pushes data trace logs to CloudWatch Logs.
	// Supported only for WebSocket APIs; for an HTTP API it is omitted
	// from calls.
	DataTraceEnabled *bool `ub:"data-trace-enabled"`
	// DetailedMetricsEnabled turns on detailed CloudWatch metrics.
	DetailedMetricsEnabled *bool `ub:"detailed-metrics-enabled"`
	// LoggingLevel is the execution logging level: ERROR, INFO, or OFF.
	// Supported only for WebSocket APIs, where enabling it also requires
	// the account-level API Gateway CloudWatch role; for an HTTP API it is
	// omitted from calls. AWS keeps the level when the block is removed,
	// so logging is silenced by sending OFF, not by removal.
	LoggingLevel *string `ub:"logging-level"`
	// ThrottlingBurstLimit is the throttling burst limit. Unset keeps the
	// account default; an explicit zero blocks all traffic.
	ThrottlingBurstLimit *int64 `ub:"throttling-burst-limit"`
	// ThrottlingRateLimit is the steady-state request rate limit. Unset
	// keeps the account default; an explicit zero blocks all traffic.
	ThrottlingRateLimit *float64 `ub:"throttling-rate-limit"`
}

// expand converts the block to the SDK type through the same member gating
// the per-route entries use.
func (s StageDefaultRouteSettings) expand(websocket bool) *apigatewayv2types.RouteSettings {
	settings := StageRouteSettings{
		DataTraceEnabled:       s.DataTraceEnabled,
		DetailedMetricsEnabled: s.DetailedMetricsEnabled,
		LoggingLevel:           s.LoggingLevel,
		ThrottlingBurstLimit:   s.ThrottlingBurstLimit,
		ThrottlingRateLimit:    s.ThrottlingRateLimit,
	}.expand(websocket)
	return &settings
}

// StageRouteSettings is one route's settings override, named by its route
// key. The members mirror StageDefaultRouteSettings, including the
// WebSocket-only gating of data-trace-enabled and logging-level and the
// caveat that AWS keeps a logging level until it is set again.
type StageRouteSettings struct {
	// RouteKey names the route the settings apply to. A key with no
	// matching route, such as $disconnect on a WebSocket API, is
	// accepted.
	RouteKey string `ub:"route-key"`
	// DataTraceEnabled pushes data trace logs to CloudWatch Logs.
	// Supported only for WebSocket APIs; for an HTTP API it is omitted
	// from calls.
	DataTraceEnabled *bool `ub:"data-trace-enabled"`
	// DetailedMetricsEnabled turns on detailed CloudWatch metrics.
	DetailedMetricsEnabled *bool `ub:"detailed-metrics-enabled"`
	// LoggingLevel is the execution logging level: ERROR, INFO, or OFF.
	// Supported only for WebSocket APIs; for an HTTP API it is omitted
	// from calls.
	LoggingLevel *string `ub:"logging-level"`
	// ThrottlingBurstLimit is the throttling burst limit. Unset keeps the
	// account default; an explicit zero blocks all traffic.
	ThrottlingBurstLimit *int64 `ub:"throttling-burst-limit"`
	// ThrottlingRateLimit is the steady-state request rate limit. Unset
	// keeps the account default; an explicit zero blocks all traffic.
	ThrottlingRateLimit *float64 `ub:"throttling-rate-limit"`
}

// expand converts the settings to the SDK type. DetailedMetricsEnabled is
// always sent explicitly, so a removed true reads back as false. The
// throttling limits are sent only when set, so an omitted limit keeps the
// account default and an explicit zero stays expressible. The
// WebSocket-only members, data-trace-enabled and logging-level, are omitted
// entirely when the API is not a WebSocket API, since the service rejects
// them for HTTP APIs.
func (s StageRouteSettings) expand(websocket bool) apigatewayv2types.RouteSettings {
	out := apigatewayv2types.RouteSettings{
		DetailedMetricsEnabled: aws.Bool(aws.ToBool(s.DetailedMetricsEnabled)),
		ThrottlingBurstLimit:   ptr.Int32(s.ThrottlingBurstLimit),
		ThrottlingRateLimit:    s.ThrottlingRateLimit,
	}
	if websocket {
		out.DataTraceEnabled = aws.Bool(aws.ToBool(s.DataTraceEnabled))
		if s.LoggingLevel != nil {
			out.LoggingLevel = apigatewayv2types.LoggingLevel(*s.LoggingLevel)
		}
	}
	return out
}

// stageRouteSettingsMap expands the per-route entries into the SDK map,
// keyed by route key. A duplicate route key is a configuration mistake the
// map would otherwise resolve silently by keeping one entry, so it is
// rejected.
func stageRouteSettingsMap(
	entries []StageRouteSettings, websocket bool,
) (map[string]apigatewayv2types.RouteSettings, error) {
	out := make(map[string]apigatewayv2types.RouteSettings, len(entries))
	for _, e := range entries {
		if _, ok := out[e.RouteKey]; ok {
			return nil, fmt.Errorf("route-settings has duplicate route key %q", e.RouteKey)
		}
		out[e.RouteKey] = e.expand(websocket)
	}
	return out, nil
}

// stageDeleteRouteSettings removes the per-route settings of every route
// key whose entry was removed or changed since the prior apply. A changed
// entry is deleted even though UpdateStage rewrites it right after, so a
// member removed from the entry cannot linger if the service merges partial
// objects rather than replacing them. A key the service no longer knows is
// already in the desired state and is skipped.
func stageDeleteRouteSettings(
	ctx context.Context, client *apigatewayv2.Client, apiID, name string,
	prior, desired []StageRouteSettings,
) error {
	desiredByKey := make(map[string]StageRouteSettings, len(desired))
	for _, e := range desired {
		desiredByKey[e.RouteKey] = e
	}
	for _, p := range prior {
		if d, ok := desiredByKey[p.RouteKey]; ok && !runtime.Changed(p, d) {
			continue
		}
		err := withConflictRetry(ctx, func(ctx context.Context) error {
			_, err := client.DeleteRouteSettings(ctx, &apigatewayv2.DeleteRouteSettingsInput{
				ApiId:     aws.String(apiID),
				StageName: aws.String(name),
				RouteKey:  aws.String(p.RouteKey),
			})
			return err
		})
		if err != nil && !isNotFound(err) {
			return fmt.Errorf("delete route settings for %s: %w", p.RouteKey, err)
		}
	}
	return nil
}
