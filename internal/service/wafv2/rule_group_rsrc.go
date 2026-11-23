package wafv2

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	awsretry "github.com/cloudboss/unobin-library-aws/internal/retry"
)

type RuleGroupResource struct {
	Capacity             int64                                `ub:"capacity"`
	Scope                string                               `ub:"scope"`
	VisibilityConfig     WebACLVisibilityConfig               `ub:"visibility-config"`
	Name                 *string                              `ub:"name"`
	NamePrefix           *string                              `ub:"name-prefix"`
	Description          *string                              `ub:"description"`
	Rules                *[]RuleGroupRule                     `ub:"rules"`
	RulesJSON            *string                              `ub:"rules-json"`
	CustomResponseBodies *map[string]WebACLCustomResponseBody `ub:"custom-response-bodies"`
	Tags                 *map[string]string                   `ub:"tags"`
}

type RuleGroupResourceOutput struct {
	ID                   string                              `ub:"id"`
	ARN                  string                              `ub:"arn"`
	LockToken            string                              `ub:"lock-token"`
	Region               string                              `ub:"region"`
	Scope                string                              `ub:"scope"`
	Name                 string                              `ub:"name"`
	NamePrefix           *string                             `ub:"name-prefix"`
	Capacity             int64                               `ub:"capacity"`
	Description          *string                             `ub:"description"`
	VisibilityConfig     WebACLVisibilityConfig              `ub:"visibility-config"`
	Rules                *[]RuleGroupRule                    `ub:"rules"`
	RulesJSON            *string                             `ub:"rules-json"`
	CustomResponseBodies map[string]WebACLCustomResponseBody `ub:"custom-response-bodies"`
	Tags                 map[string]string                   `ub:"tags"`
}

type ruleGroupPrior = runtime.Prior[RuleGroupResource, *RuleGroupResourceOutput, *awsCfg]
type ruleGroupDefinition = runtime.ResourceDefinition[
	RuleGroupResource, *RuleGroupResourceOutput, *awsCfg,
]

func (r RuleGroupResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.Name, r.NamePrefix),
		constraint.AtMostOneOf(r.Rules, r.RulesJSON),
		constraint.Must(constraint.AtLeast(r.Capacity, 1)),
		constraint.Must(constraint.OneOf(r.Scope, "REGIONAL", "CLOUDFRONT")),
		constraint.Must(constraint.IsMatch(r.Name, "^[0-9A-Za-z_-]{1,128}$")),
		constraint.Must(constraint.IsMatch(r.NamePrefix, "^[0-9A-Za-z_-]{1,102}$")),
		constraint.Must(constraint.IsMatch(
			r.VisibilityConfig.MetricName, "^[0-9A-Za-z_-]{1,128}$",
		)),
		constraint.When(constraint.Present(r.Description)).Require(
			constraint.AtLeast(constraint.ByteLength(r.Description), 1),
			constraint.AtMost(constraint.ByteLength(r.Description), 256),
		),
		constraint.Must(constraint.IsFormat(r.RulesJSON, constraint.FormatJSON)),
		constraint.ForEach(r.CustomResponseBodies,
			func(body constraint.MapEntry[WebACLCustomResponseBody]) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(
						constraint.IsMatch(body.Key, "^[0-9A-Za-z_-]{1,128}$"),
						constraint.AtLeast(constraint.ByteLength(body.Value.Content), 1),
						constraint.AtMost(constraint.ByteLength(body.Value.Content), 10240),
						constraint.OneOf(body.Value.ContentType,
							"TEXT_PLAIN", "TEXT_HTML", "APPLICATION_JSON"),
					).Message("custom-response-bodies contains an invalid key, content or type"),
				}
			},
		),
		constraint.ForEach(r.Rules, func(rule RuleGroupRule) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.ExactlyOneOf(rule.Action.Allow, rule.Action.Block,
					rule.Action.Captcha, rule.Action.Challenge, rule.Action.Count,
				),
				constraint.Must(
					constraint.AtLeast(constraint.ByteLength(rule.Name), 1),
					constraint.AtMost(constraint.ByteLength(rule.Name), 128),
				).Message("rule name must be 1 to 128 bytes"),
				constraint.Must(constraint.IsMatch(
					rule.VisibilityConfig.MetricName, "^[0-9A-Za-z_-]{1,128}$",
				)).Message("rule visibility metric-name must use 1 to 128 name characters"),
				constraint.Must(constraint.NotEquals(rule.Priority, nil)).
					Message("rule priority must be set"),
				constraint.Must(constraint.NotEquals(
					rule.VisibilityConfig.CloudWatchMetricsEnabled, nil,
				)).Message("rule visibility cloudwatch-metrics-enabled must be set"),
				constraint.Must(constraint.NotEquals(
					rule.VisibilityConfig.SampledRequestsEnabled, nil,
				)).Message("rule visibility sampled-requests-enabled must be set"),
				constraint.ForEach(rule.RuleLabels, func(label WebACLRuleLabel) []constraint.Constraint {
					return []constraint.Constraint{
						constraint.Must(
							constraint.IsMatch(label.Name, "^[0-9A-Za-z_:-]+$"),
							constraint.AtMost(constraint.ByteLength(label.Name), 1024)).
							Message("rule-label name must use 1 to 1024 label characters"),
					}
				}),
				constraint.Must(constraint.Expr(
					ruleGroupActionPayloads, constraint.Bind("action", rule.Action),
				)).Message("rule action contains an invalid custom HTTP header or response"),
				constraint.Must(constraint.Expr(
					ruleGroupStatementTree, constraint.Bind("statement", rule.Statement),
				)).Message("statement must select exactly one member"),
				constraint.Must(constraint.Expr(
					ruleGroupLeafChecks, constraint.Bind("statement", rule.Statement),
				)).Message("statement scalar or collection input is outside its allowed values"),
				constraint.Must(constraint.Expr(
					ruleGroupForwardingChecks, constraint.Bind("statement", rule.Statement),
				)).Message("forwarded IP configuration contains an invalid header, fallback or position"),
				constraint.Must(constraint.Expr(
					ruleGroupTransformChecks, constraint.Bind("statement", rule.Statement),
				)).Message("text-transformations requires entries with a valid type"),
				constraint.Must(constraint.Expr(
					ruleGroupFieldSelectionChecks, constraint.Bind("statement", rule.Statement),
				)).Message("field-to-match must select exactly one member"),
				constraint.Must(constraint.Expr(
					ruleGroupFieldScalarChecks, constraint.Bind("statement", rule.Statement),
				)).Message("field-to-match contains an invalid name, scope or handling option"),
				constraint.Must(constraint.Expr(
					ruleGroupPatternChecks, constraint.Bind("statement", rule.Statement),
				)).Message("field-to-match contains an invalid cookies, headers or JSON body match pattern"),
				constraint.When(constraint.Present(rule.Statement.RateBasedStatement)).Require(
					constraint.AtLeast(rule.Statement.RateBasedStatement.Limit, 10),
					constraint.AtMost(rule.Statement.RateBasedStatement.Limit, 2000000000),
					constraint.MinItems(rule.Statement.RateBasedStatement.CustomKeys, 1),
					constraint.MaxItems(rule.Statement.RateBasedStatement.CustomKeys, 5),
				).Message("rate-based statement contains an invalid limit or custom-keys length"),
				constraint.When(constraint.Present(
					rule.Statement.RateBasedStatement.AggregateKeyType,
				)).Require(constraint.OneOf(rule.Statement.RateBasedStatement.AggregateKeyType,
					"IP", "FORWARDED_IP", "CUSTOM_KEYS", "CONSTANT",
				)).Message("rate-based aggregate-key-type is invalid"),
				constraint.When(constraint.Present(
					rule.Statement.RateBasedStatement.EvaluationWindowSec,
				)).Require(constraint.OneOf(rule.Statement.RateBasedStatement.EvaluationWindowSec,
					60, 120, 300, 600,
				)).Message("rate-based evaluation-window-sec must be 60, 120, 300 or 600"),
				constraint.When(constraint.Equals(
					rule.Statement.RateBasedStatement.AggregateKeyType, "CONSTANT",
				)).Require(constraint.Present(rule.Statement.RateBasedStatement.ScopeDownStatement)).
					Message("CONSTANT requires scope-down-statement"),
				constraint.When(constraint.Equals(
					rule.Statement.RateBasedStatement.AggregateKeyType, "CUSTOM_KEYS",
				)).Require(constraint.Present(rule.Statement.RateBasedStatement.CustomKeys)).
					Message("CUSTOM_KEYS requires custom-keys"),
				constraint.When(constraint.Any(
					constraint.Equals(rule.Statement.RateBasedStatement.AggregateKeyType, "FORWARDED_IP"),
					constraint.Above(constraint.CountWhere(
						rule.Statement.RateBasedStatement.CustomKeys,
						func(key WebACLRateCustomKey) constraint.Condition {
							return constraint.Present(key.ForwardedIP)
						},
					), 0),
				)).Require(constraint.Present(rule.Statement.RateBasedStatement.ForwardedIPConfig)).
					Message("forwarded IP aggregation or custom key requires forwarded-ip-config"),
				constraint.When(constraint.Present(
					rule.Statement.RateBasedStatement.ForwardedIPConfig,
				)).Require(constraint.OneOf(
					rule.Statement.RateBasedStatement.ForwardedIPConfig.FallbackBehavior,
					"MATCH", "NO_MATCH",
				)).Message("rate-based forwarded IP fallback-behavior is invalid"),
				constraint.ForEach(rule.Statement.RateBasedStatement.CustomKeys,
					func(key WebACLRateCustomKey) []constraint.Constraint {
						return []constraint.Constraint{
							constraint.ExactlyOneOf(
								key.ASN, key.Cookie, key.ForwardedIP, key.HTTPMethod, key.Header,
								key.IP, key.JA3Fingerprint, key.JA4Fingerprint, key.LabelNamespace,
								key.QueryArgument, key.QueryString, key.URIPath,
							),
							constraint.Must(constraint.Expr(
								ruleGroupRateNamedKeys,
								constraint.Bind("cookie", key.Cookie),
								constraint.Bind("header", key.Header),
								constraint.Bind("query-argument", key.QueryArgument),
							)).Message("rate custom key name must be 1 to 64 bytes"),
							constraint.Must(constraint.Expr(
								ruleGroupRateKeyTransforms,
								constraint.Bind("cookie", key.Cookie),
								constraint.Bind("header", key.Header),
								constraint.Bind("query-argument", key.QueryArgument),
								constraint.Bind("query-string", key.QueryString),
								constraint.Bind("uri-path", key.URIPath),
							)).Message("rate custom key requires text-transformations with a valid type"),
							constraint.When(constraint.Present(key.LabelNamespace)).Require(
								constraint.IsMatch(key.LabelNamespace.Namespace, ruleGroupLabelPattern),
							).Message("rate custom key namespace must use 1 to 1024 label characters"),
							constraint.When(constraint.Present(key.JA3Fingerprint)).Require(
								constraint.OneOf(key.JA3Fingerprint.FallbackBehavior, "MATCH", "NO_MATCH"),
							).Message("rate JA3 fallback-behavior is invalid"),
							constraint.When(constraint.Present(key.JA4Fingerprint)).Require(
								constraint.OneOf(key.JA4Fingerprint.FallbackBehavior, "MATCH", "NO_MATCH"),
							).Message("rate JA4 fallback-behavior is invalid"),
						}
					},
				),
			}
		}),
		constraint.Must(constraint.NotEquals(r.VisibilityConfig.CloudWatchMetricsEnabled, nil)).
			Message("visibility-config cloudwatch-metrics-enabled must be set"),
		constraint.Must(constraint.NotEquals(r.VisibilityConfig.SampledRequestsEnabled, nil)).
			Message("visibility-config sampled-requests-enabled must be set"),
	}
}

const ruleGroupRoot = `@core.all([ for statement in [input.statement] : `
const ruleGroupRootEnd = ` ])`

// Scope-down and direct logical children share their remaining schema depth.
const ruleGroupFirstChildren = `[
	input.statement.and-statement?.statements ?? [],
	input.statement.or-statement?.statements ?? [],
	[input.statement.not-statement?.statement],
	[input.statement.rate-based-statement?.scope-down-statement]
]`
const ruleGroupSecondChildren = `[
	level2.and-statement?.statements ?? [],
	level2.or-statement?.statements ?? [],
	[level2.not-statement?.statement]
]`
const ruleGroupThirdChildren = `[
	level1.and-statement?.statements ?? [],
	level1.or-statement?.statements ?? [],
	[level1.not-statement?.statement]
]`

const ruleGroupFirstLevel = `@core.all([ for first in ` + ruleGroupFirstChildren +
	` : @core.all([ for statement in first : `
const ruleGroupFirstLevelEnd = ` when statement != null ]) ])`

const ruleGroupSecondLevel = `@core.all([ for first in ` + ruleGroupFirstChildren +
	` : @core.all([ for level2 in first :
		@core.all([ for second in ` + ruleGroupSecondChildren + ` :
			@core.all([ for statement in second : `
const ruleGroupSecondLevelEnd = ` when statement != null ]) ])
	when level2 != null ]) ])`

const ruleGroupThirdLevel = `@core.all([ for first in ` + ruleGroupFirstChildren +
	` : @core.all([ for level2 in first :
		@core.all([ for second in ` + ruleGroupSecondChildren + ` :
			@core.all([ for level1 in second :
				@core.all([ for third in ` + ruleGroupThirdChildren + ` :
					@core.all([ for statement in third : `
const ruleGroupThirdLevelEnd = ` when statement != null ]) ])
	when level1 != null ]) ]) when level2 != null ]) ])`

const ruleGroupStatementMembers = `@core.length([ for selected in [
	statement.asn-match-statement != null,
	statement.byte-match-statement != null,
	statement.geo-match-statement != null,
	statement.ip-set-reference-statement != null,
	statement.label-match-statement != null,
	statement.regex-match-statement != null,
	statement.regex-pattern-set-reference-statement != null,
	statement.size-constraint-statement != null,
	statement.sqli-match-statement != null,
	statement.xss-match-statement != null,
`

const ruleGroupStatementLogicalMembers = `
	statement.and-statement != null,
	statement.or-statement != null,
	statement.not-statement != null,
`

const ruleGroupStatementSelection = `] : selected when selected ]) == 1`

const ruleGroupSelectionRoot = ruleGroupStatementMembers + ruleGroupStatementLogicalMembers +
	`statement.rate-based-statement != null,` + ruleGroupStatementSelection
const ruleGroupSelectionLogical = ruleGroupStatementMembers + ruleGroupStatementLogicalMembers +
	ruleGroupStatementSelection
const ruleGroupSelectionLeaf = ruleGroupStatementMembers + ruleGroupStatementSelection

const ruleGroupStatementTree = ruleGroupRoot + ruleGroupSelectionRoot + ruleGroupRootEnd + " && " +
	ruleGroupFirstLevel + ruleGroupSelectionLogical + ruleGroupFirstLevelEnd + " && " +
	ruleGroupSecondLevel + ruleGroupSelectionLogical + ruleGroupSecondLevelEnd + " && " +
	ruleGroupThirdLevel + ruleGroupSelectionLeaf + ruleGroupThirdLevelEnd

const ruleGroupMatchFields = `@core.all([ for field in [
	statement.byte-match-statement?.field-to-match,
	statement.regex-match-statement?.field-to-match,
	statement.regex-pattern-set-reference-statement?.field-to-match,
	statement.size-constraint-statement?.field-to-match,
	statement.sqli-match-statement?.field-to-match,
	statement.xss-match-statement?.field-to-match
] : (`
const ruleGroupMatchFieldsEnd = `) when field != null ])`

const ruleGroupFieldSelection = `@core.length([ for selected in [
	field.all-query-arguments != null,
	field.body != null,
	field.cookies != null,
	field.header-order != null,
	field.headers != null,
	field.ja3-fingerprint != null,
	field.ja4-fingerprint != null,
	field.json-body != null,
	field.method != null,
	field.query-string != null,
	field.single-header != null,
	field.single-query-argument != null,
	field.uri-fragment != null,
	field.uri-path != null
] : selected when selected ]) == 1`

const ruleGroupLabelPattern = "^[0-9A-Za-z_:-]{1,1000}[0-9A-Za-z_:-]{0,24}$"

const ruleGroupLeafScalars = `
(statement.asn-match-statement == null ||
		(@core.length(statement.asn-match-statement.asn-list) >= 1 &&
		@core.length(statement.asn-match-statement.asn-list) <= 100)) &&
(statement.geo-match-statement == null ||
		(@core.length(statement.geo-match-statement.country-codes) >= 1)) &&
(statement.label-match-statement == null ||
		(@core.is-match(statement.label-match-statement.key, '` + ruleGroupLabelPattern + `') &&
		@core.has-item(['LABEL', 'NAMESPACE'], statement.label-match-statement.scope))) &&
(statement.byte-match-statement == null ||
		(@core.byte-length(statement.byte-match-statement.search-string) >= 1 &&
		@core.byte-length(statement.byte-match-statement.search-string) <= 200 && @core.has-item([
		'EXACTLY', 'STARTS_WITH', 'ENDS_WITH', 'CONTAINS', 'CONTAINS_WORD'],
		statement.byte-match-statement.positional-constraint))) &&
(statement.regex-match-statement == null ||
		(@core.byte-length(statement.regex-match-statement.regex-string) >= 1 &&
		@core.byte-length(statement.regex-match-statement.regex-string) <= 512 &&
		@core.is-format(statement.regex-match-statement.regex-string,
		    '` + constraint.FormatRegex + `'))) &&
(statement.size-constraint-statement == null || (statement.size-constraint-statement.size >=
		0 && statement.size-constraint-statement.size <= 2147483647 && @core.has-item(['EQ', 'NE'
		, 'LE', 'LT', 'GE', 'GT'], statement.size-constraint-statement.comparison-operator))) &&
(statement.sqli-match-statement?.sensitivity-level == null || (@core.has-item(['LOW', 'HIGH'
		], statement.sqli-match-statement?.sensitivity-level)))
`

const ruleGroupForwarding = `
@core.all([ for config in [
	statement.asn-match-statement?.forwarded-ip-config,
	statement.geo-match-statement?.forwarded-ip-config
] : @core.has-item(['MATCH', 'NO_MATCH'], config.fallback-behavior) when config != null ]) &&
@core.all([ for config in [statement.ip-set-reference-statement?.ip-set-forwarded-ip-config]
		: @core.is-match(config.header-name, '^[0-9A-Za-z-]{1,255}$') && @core.has-item(['MATCH',
		'NO_MATCH'], config.fallback-behavior) && @core.has-item(['FIRST', 'LAST', 'ANY'],
		config.position) when config != null ])
`

const ruleGroupFieldScalars = `
(field.body?.oversize-handling == null || (@core.has-item(['CONTINUE', 'MATCH', 'NO_MATCH'
		], field.body?.oversize-handling))) &&
(field.header-order == null || (@core.has-item(['CONTINUE', 'MATCH', 'NO_MATCH'],
		field.header-order.oversize-handling))) &&
(field.uri-fragment?.fallback-behavior == null || (@core.has-item(['MATCH', 'NO_MATCH'],
		field.uri-fragment?.fallback-behavior))) &&
(field.single-header == null || (@core.is-match(field.single-header.name,
		'^[0-9a-z_-]{1,40}$'))) &&
(field.single-query-argument == null || (@core.is-match(field.single-query-argument.name,
		'^[0-9a-z_-]{1,30}$'))) &&
@core.all([ for fingerprint in [field.ja3-fingerprint, field.ja4-fingerprint] :
		@core.has-item(['MATCH', 'NO_MATCH'], fingerprint.fallback-behavior) when fingerprint !=
		null ]) &&
@core.all([ for field-map in [field.cookies, field.headers] : @core.has-item(['ALL', 'KEY',
		'VALUE'], field-map.match-scope) && @core.has-item(['CONTINUE', 'MATCH', 'NO_MATCH'],
		field-map.oversize-handling) when field-map != null ]) &&
(field.json-body == null || (@core.has-item(['ALL', 'KEY', 'VALUE'],
		field.json-body.match-scope) && (field.json-body.oversize-handling == null ||
		(@core.has-item(['CONTINUE', 'MATCH', 'NO_MATCH'], field.json-body.oversize-handling))) &&
		(field.json-body.invalid-fallback-behavior == null || (@core.has-item(['MATCH', 'NO_MATCH'
		, 'EVALUATE_AS_STRING'], field.json-body.invalid-fallback-behavior)))))
`

const ruleGroupPatterns = `
@core.all([ for pattern in [field.cookies?.match-pattern] : (@core.length([ for selected in [
	pattern.all != null,
	@core.length(pattern.excluded-cookies ?? []) > 0,
	@core.length(pattern.included-cookies ?? []) > 0
] : selected when selected ]) == 1) when pattern != null ]) &&
@core.all([ for pattern in [field.headers?.match-pattern] : (@core.length([ for selected in [
	pattern.all != null,
	pattern.excluded-headers != null,
	pattern.included-headers != null
] : selected when selected ]) == 1 &&
	@core.all([ for names in [pattern.excluded-headers, pattern.included-headers] :
		@core.length(names) >= 1 && @core.length(names) <= 199 && @core.all([ for name in names :
		@core.byte-length(name) >= 1 && @core.byte-length(name) <= 64 && @core.is-match(name,
		'[^\\s\\p{Z}\\x{85}\\x{B}]') ]) when names != null ])) when pattern != null ]) &&
@core.all([ for pattern in [field.json-body?.match-pattern] : (@core.length([ for selected
		in [
	pattern.all != null,
	pattern.included-paths != null
] : selected when selected ]) == 1 &&
	(pattern.included-paths == null || (@core.length(pattern.included-paths) >= 1 &&
		@core.all([ for path in pattern.included-paths : @core.byte-length(path) >= 1 &&
		@core.byte-length(path) <= 512 && @core.is-match(path, '(/)|(/(([^~])|(~[01]))+)') ]))))
		when pattern != null ])
`

const ruleGroupTransformTypes = `[
	'NONE',
	'COMPRESS_WHITE_SPACE',
	'HTML_ENTITY_DECODE',
	'LOWERCASE',
	'CMD_LINE',
	'URL_DECODE',
	'BASE64_DECODE',
	'HEX_DECODE',
	'MD5',
	'REPLACE_COMMENTS',
	'ESCAPE_SEQ_DECODE',
	'SQL_HEX_DECODE',
	'CSS_DECODE',
	'JS_DECODE',
	'NORMALIZE_PATH',
	'NORMALIZE_PATH_WIN',
	'REMOVE_NULLS',
	'REPLACE_NULLS',
	'BASE64_DECODE_EXT',
	'URL_DECODE_UNI',
	'UTF8_TO_UNICODE'
]`

const ruleGroupTransformEntry = `@core.has-item(` + ruleGroupTransformTypes +
	`, transform.type)`

const ruleGroupTransformsOpen = `@core.length(transforms) >= 1 &&
	@core.all([ for transform in transforms : `
const ruleGroupTransformsClose = ` ])`

const ruleGroupStatementTransforms = `@core.all([ for transforms in [
	statement.byte-match-statement?.text-transformations,
	statement.regex-match-statement?.text-transformations,
	statement.regex-pattern-set-reference-statement?.text-transformations,
	statement.size-constraint-statement?.text-transformations,
	statement.sqli-match-statement?.text-transformations,
	statement.xss-match-statement?.text-transformations
] : ` +
	ruleGroupTransformsOpen + ruleGroupTransformEntry + ruleGroupTransformsClose +
	` when transforms != null ])`

const ruleGroupRateNamedKeys = `@core.all([ for named in [input.cookie, input.header,
	input.query-argument] :
	@core.byte-length(named.name) >= 1 && @core.byte-length(named.name) <= 64
	when named != null ])`

const ruleGroupRateKeyTransforms = `@core.all([ for transforms in [
	input.cookie?.text-transformations,
	input.header?.text-transformations,
	input.query-argument?.text-transformations,
	input.query-string?.text-transformations,
	input.uri-path?.text-transformations
] : ` +
	ruleGroupTransformsOpen + ruleGroupTransformEntry + ruleGroupTransformsClose +
	` when transforms != null ])`

const ruleGroupLeafChecksAtDepth = ruleGroupLeafScalars
const ruleGroupLeafChecks = ruleGroupRoot +
	ruleGroupLeafChecksAtDepth +
	ruleGroupRootEnd + " && " +
	ruleGroupFirstLevel + ruleGroupLeafChecksAtDepth +
	ruleGroupFirstLevelEnd + " && " +
	ruleGroupSecondLevel + ruleGroupLeafChecksAtDepth +
	ruleGroupSecondLevelEnd + " && " +
	ruleGroupThirdLevel + ruleGroupLeafChecksAtDepth +
	ruleGroupThirdLevelEnd

const ruleGroupForwardingChecksAtDepth = ruleGroupForwarding
const ruleGroupForwardingChecks = ruleGroupRoot +
	ruleGroupForwardingChecksAtDepth +
	ruleGroupRootEnd + " && " +
	ruleGroupFirstLevel + ruleGroupForwardingChecksAtDepth +
	ruleGroupFirstLevelEnd + " && " +
	ruleGroupSecondLevel + ruleGroupForwardingChecksAtDepth +
	ruleGroupSecondLevelEnd + " && " +
	ruleGroupThirdLevel + ruleGroupForwardingChecksAtDepth +
	ruleGroupThirdLevelEnd

const ruleGroupTransformChecksAtDepth = ruleGroupStatementTransforms
const ruleGroupTransformChecks = ruleGroupRoot +
	ruleGroupTransformChecksAtDepth +
	ruleGroupRootEnd + " && " +
	ruleGroupFirstLevel + ruleGroupTransformChecksAtDepth +
	ruleGroupFirstLevelEnd + " && " +
	ruleGroupSecondLevel + ruleGroupTransformChecksAtDepth +
	ruleGroupSecondLevelEnd + " && " +
	ruleGroupThirdLevel + ruleGroupTransformChecksAtDepth +
	ruleGroupThirdLevelEnd

const ruleGroupFieldSelectionChecksAtDepth = ruleGroupMatchFields +
	ruleGroupFieldSelection + ruleGroupMatchFieldsEnd
const ruleGroupFieldSelectionChecks = ruleGroupRoot +
	ruleGroupFieldSelectionChecksAtDepth +
	ruleGroupRootEnd + " && " +
	ruleGroupFirstLevel + ruleGroupFieldSelectionChecksAtDepth +
	ruleGroupFirstLevelEnd + " && " +
	ruleGroupSecondLevel + ruleGroupFieldSelectionChecksAtDepth +
	ruleGroupSecondLevelEnd + " && " +
	ruleGroupThirdLevel + ruleGroupFieldSelectionChecksAtDepth +
	ruleGroupThirdLevelEnd

const ruleGroupFieldScalarChecksAtDepth = ruleGroupMatchFields +
	ruleGroupFieldScalars + ruleGroupMatchFieldsEnd
const ruleGroupFieldScalarChecks = ruleGroupRoot +
	ruleGroupFieldScalarChecksAtDepth +
	ruleGroupRootEnd + " && " +
	ruleGroupFirstLevel + ruleGroupFieldScalarChecksAtDepth +
	ruleGroupFirstLevelEnd + " && " +
	ruleGroupSecondLevel + ruleGroupFieldScalarChecksAtDepth +
	ruleGroupSecondLevelEnd + " && " +
	ruleGroupThirdLevel + ruleGroupFieldScalarChecksAtDepth +
	ruleGroupThirdLevelEnd

const ruleGroupPatternChecksAtDepth = ruleGroupMatchFields +
	ruleGroupPatterns + ruleGroupMatchFieldsEnd
const ruleGroupPatternChecks = ruleGroupRoot +
	ruleGroupPatternChecksAtDepth +
	ruleGroupRootEnd + " && " +
	ruleGroupFirstLevel + ruleGroupPatternChecksAtDepth +
	ruleGroupFirstLevelEnd + " && " +
	ruleGroupSecondLevel + ruleGroupPatternChecksAtDepth +
	ruleGroupSecondLevelEnd + " && " +
	ruleGroupThirdLevel + ruleGroupPatternChecksAtDepth +
	ruleGroupThirdLevelEnd

const ruleGroupHeaderCheck = `@core.all([ for header in headers :
	@core.is-match(header.name, '^[0-9A-Za-z_$.-]{1,64}$') &&
	@core.byte-length(header.value) >= 1 && @core.byte-length(header.value) <= 255
])`

const ruleGroupActionPayloads = `@core.all([ for request in [
	input.action.allow?.custom-request-handling,
	input.action.captcha?.custom-request-handling,
	input.action.challenge?.custom-request-handling,
	input.action.count?.custom-request-handling
] : @core.length(request.insert-headers) >= 1 &&
	@core.all([ for headers in [request.insert-headers] : ` + ruleGroupHeaderCheck + ` ])
	when request != null ]) &&
@core.all([ for response in [input.action.block?.custom-response] :
	response.response-code >= 200 && response.response-code <= 600 &&
	@core.is-match(response.custom-response-body-key, '^[0-9A-Za-z_-]{1,128}$') &&
	@core.all([ for headers in [response.response-headers ?? []] : ` + ruleGroupHeaderCheck + ` ])
	when response != null ])`

type ruleGroupClient interface {
	wafTagClient
	CreateRuleGroup(context.Context, *awssvc.CreateRuleGroupInput,
		...func(*awssvc.Options)) (*awssvc.CreateRuleGroupOutput, error)
	GetRuleGroup(context.Context, *awssvc.GetRuleGroupInput,
		...func(*awssvc.Options)) (*awssvc.GetRuleGroupOutput, error)
	UpdateRuleGroup(context.Context, *awssvc.UpdateRuleGroupInput,
		...func(*awssvc.Options)) (*awssvc.UpdateRuleGroupOutput, error)
	DeleteRuleGroup(context.Context, *awssvc.DeleteRuleGroupInput,
		...func(*awssvc.Options)) (*awssvc.DeleteRuleGroupOutput, error)
}

func (r *RuleGroupResource) ResourceDefinition() ruleGroupDefinition {
	return ruleGroupDefinition{
		SchemaVersion: 1,
		Validate: func(_ context.Context, input RuleGroupResource, _ *awsCfg) error {
			_, err := input.prepareRuleGroup()
			return err
		},
		Equality: []runtime.InputEqualityRule[RuleGroupResource]{
			runtime.EqualBy(
				runtime.InputField(func(r *RuleGroupResource) **string { return &r.RulesJSON }),
				ruleGroupJSONEqual,
			),
			runtime.EqualBy(
				runtime.InputField(
					func(r *RuleGroupResource) **map[string]WebACLCustomResponseBody {
						return &r.CustomResponseBodies
					},
				),
				ruleGroupBodiesEqual,
			),
			runtime.EqualBy(
				runtime.InputField(func(r *RuleGroupResource) **map[string]string { return &r.Tags }),
				ruleGroupTagsEqual,
			),
		},
		Replace: runtime.Replacement[RuleGroupResource, *RuleGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RuleGroupResource]{
				runtime.InputField(func(r *RuleGroupResource) *int64 { return &r.Capacity }),
				runtime.InputField(func(r *RuleGroupResource) *string { return &r.Scope }),
				runtime.InputField(func(r *RuleGroupResource) **string { return &r.Name }),
				runtime.InputField(func(r *RuleGroupResource) **string { return &r.NamePrefix }),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(_ RuleGroupResource, output *RuleGroupResourceOutput) (string, error) {
			if err := checkRuleGroupIdentity(output); err != nil {
				return "", err
			}
			return output.ARN, nil
		},
	}
}

func (r *RuleGroupResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*RuleGroupResourceOutput, error) {
	if r.Scope == string(awstypes.ScopeCloudfront) {
		cloudfront := ptr.Value(cfg)
		cloudfront.Region = aws.String("us-east-1")
		cfg = &cloudfront
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createRuleGroup(ctx, client, client.Options().Region, rand.Reader)
}

func (r *RuleGroupResource) Read(
	ctx context.Context, cfg *awsCfg, prior ruleGroupPrior,
) (*RuleGroupResourceOutput, error) {
	client, err := existingRuleGroupClient(ctx, ruleGroupPriorConfig(cfg, prior), prior.Outputs)
	if err != nil {
		return nil, err
	}
	rulesJSON := prior.Inputs.RulesJSON
	// A NoOp saves the new input spelling. Keep the equivalent output stable during refresh.
	if rulesJSON != nil && ruleGroupJSONEqual(rulesJSON, prior.Outputs.RulesJSON) {
		rulesJSON = prior.Outputs.RulesJSON
	}
	return readRuleGroup(ctx, client, *prior.Outputs, rulesJSON, true)
}

func (r *RuleGroupResource) Update(
	ctx context.Context, cfg *awsCfg, prior ruleGroupPrior,
) (*RuleGroupResourceOutput, error) {
	client, err := existingRuleGroupClient(ctx, cfg, prior.Outputs)
	if err != nil {
		return nil, err
	}
	return r.updateRuleGroup(ctx, client, prior)
}

func (r *RuleGroupResource) Delete(
	ctx context.Context, cfg *awsCfg, prior ruleGroupPrior,
) error {
	client, err := existingRuleGroupClient(ctx, ruleGroupPriorConfig(cfg, prior), prior.Outputs)
	if err != nil {
		return err
	}
	return deleteRuleGroup(ctx, client, prior)
}

func ruleGroupPriorConfig(cfg *awsCfg, prior ruleGroupPrior) *awsCfg {
	if prior.Configuration != nil {
		return prior.Configuration
	}
	return cfg
}

func existingRuleGroupClient(
	ctx context.Context, cfg *awsCfg, output *RuleGroupResourceOutput,
) (*awssvc.Client, error) {
	if err := checkRuleGroupIdentity(output); err != nil {
		return nil, err
	}
	saved := ptr.Value(cfg)
	saved.Region = aws.String(output.Region)
	return newClient(ctx, &saved)
}

func (r *RuleGroupResource) createRuleGroup(
	ctx context.Context, client ruleGroupClient, region string, random io.Reader,
	options ...awsretry.Option,
) (*RuleGroupResourceOutput, error) {
	prepared, err := r.prepareRuleGroup()
	if err != nil {
		return nil, err
	}
	name, err := resolveWAFName(r.Name, r.NamePrefix, random)
	if err != nil {
		return nil, fmt.Errorf("generate rule group name: %w", err)
	}
	input := &awssvc.CreateRuleGroupInput{
		Capacity:             aws.Int64(r.Capacity),
		Name:                 aws.String(name),
		Scope:                awstypes.Scope(r.Scope),
		VisibilityConfig:     expandVisibilityConfig(r.VisibilityConfig),
		CustomResponseBodies: prepared.bodies,
		Description:          r.Description,
		Rules:                prepared.rules,
	}
	if len(prepared.tags) > 0 {
		input.Tags = expandTags(prepared.tags)
	}
	var created *awssvc.CreateRuleGroupOutput
	err = retryRuleGroup(ctx, isWebACLUnavailable, func(ctx context.Context) error {
		var err error
		created, err = client.CreateRuleGroup(ctx, input)
		return err
	}, options...)
	if err != nil {
		return nil, fmt.Errorf("create rule group %s: %w", name, err)
	}
	if created == nil || created.Summary == nil || aws.ToString(created.Summary.Id) == "" {
		return nil, fmt.Errorf("create rule group %s: response has no rule group ID", name)
	}
	return readRuleGroup(ctx, client, RuleGroupResourceOutput{
		ID: aws.ToString(created.Summary.Id), Name: name, Scope: r.Scope, Region: region,
	}, prepared.rulesJSON, false)
}

func readRuleGroup(
	ctx context.Context, client ruleGroupClient, identity RuleGroupResourceOutput,
	rulesJSON *string, established bool,
) (*RuleGroupResourceOutput, error) {
	response, err := client.GetRuleGroup(ctx, &awssvc.GetRuleGroupInput{
		Id: aws.String(identity.ID), Name: aws.String(identity.Name),
		Scope: awstypes.Scope(identity.Scope),
	})
	if err != nil {
		if established && isWebACLNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get rule group %s: %w", identity.ID, err)
	}
	if response == nil || response.RuleGroup == nil {
		if established {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get rule group %s: response has no rule group", identity.ID)
	}
	group := response.RuleGroup
	if aws.ToString(group.Id) != identity.ID || aws.ToString(group.Name) != identity.Name ||
		(identity.ARN != "" && aws.ToString(group.ARN) != identity.ARN) {
		return nil, fmt.Errorf("get rule group %s: response identity does not match request",
			identity.ID)
	}
	identity.ARN = aws.ToString(group.ARN)
	identity.LockToken = aws.ToString(response.LockToken)
	if err := checkRuleGroupIdentity(&identity); err != nil {
		return nil, fmt.Errorf("get rule group %s: %w", identity.ID, err)
	}
	if identity.LockToken == "" {
		return nil, fmt.Errorf("get rule group %s: response has no lock token", identity.ID)
	}
	identity.NamePrefix = wafNamePrefix(identity.Name)
	identity.Capacity = aws.ToInt64(group.Capacity)
	identity.Description = group.Description
	identity.VisibilityConfig = flattenRuleGroupVisibility(group.VisibilityConfig)
	identity.CustomResponseBodies = flattenRuleGroupBodies(group.CustomResponseBodies)
	identity.RulesJSON, identity.Rules = nil, nil
	if rulesJSON != nil {
		normalized, err := normalizedRuleGroupJSON(*rulesJSON)
		if err != nil {
			return nil, fmt.Errorf("read configured rules-json: %w", err)
		}
		identity.RulesJSON = aws.String(string(normalized))
	} else {
		identity.Rules = ptr.To(flattenRuleGroupRules(group.Rules))
	}
	identity.Tags, err = listWAFTags(ctx, client, identity.ARN, identity.Region, "rule group")
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func checkRuleGroupIdentity(output *RuleGroupResourceOutput) error {
	if output == nil || output.ID == "" || output.Name == "" || output.Region == "" {
		return fmt.Errorf("rule group identity requires ID, name, scope and region")
	}
	scope := "regional"
	switch awstypes.Scope(output.Scope) {
	case awstypes.ScopeRegional:
	case awstypes.ScopeCloudfront:
		scope = "global"
	default:
		return fmt.Errorf("rule group identity has invalid scope %q", output.Scope)
	}
	parsed, err := awsarn.Parse(output.ARN)
	if err != nil {
		return fmt.Errorf("rule group identity has invalid ARN %q: %w", output.ARN, err)
	}
	resource := scope + "/rulegroup/" + output.Name + "/" + output.ID
	if parsed.Service != "wafv2" || parsed.Region != output.Region || parsed.Resource != resource {
		return fmt.Errorf("rule group identity does not match ARN %q", output.ARN)
	}
	return nil
}

func ruleGroupMutationState(prior ruleGroupPrior) (*RuleGroupResourceOutput, error) {
	current := prior.Outputs
	if err := checkRuleGroupIdentity(current); err != nil {
		return nil, err
	}
	if observed := prior.Observed; observed != nil {
		if observed.ID != current.ID || observed.ARN != current.ARN ||
			observed.Name != current.Name || observed.Scope != current.Scope ||
			observed.Region != current.Region {
			return nil, fmt.Errorf("rule group %s observation does not match saved identity",
				current.ID)
		}
		current = observed
	}
	if current.LockToken == "" {
		return nil, fmt.Errorf("rule group %s has no lock token", current.ID)
	}
	return current, nil
}

func (r *RuleGroupResource) updateRuleGroup(
	ctx context.Context, client ruleGroupClient, prior ruleGroupPrior, options ...awsretry.Option,
) (*RuleGroupResourceOutput, error) {
	prepared, err := r.prepareRuleGroup()
	if err != nil {
		return nil, err
	}
	current, err := ruleGroupMutationState(prior)
	if err != nil {
		return nil, err
	}
	if err := syncWAFTags(ctx, client, current.ARN, current.Region, "rule group",
		current.Tags, prepared.tags); err != nil {
		return nil, err
	}
	if r.coreChanged(prior.Inputs) || ruleGroupCoreDrifted(prior.Outputs, prior.Observed) {
		input := &awssvc.UpdateRuleGroupInput{
			Id:                   aws.String(current.ID),
			Name:                 aws.String(current.Name),
			Scope:                awstypes.Scope(current.Scope),
			LockToken:            aws.String(current.LockToken),
			VisibilityConfig:     expandVisibilityConfig(r.VisibilityConfig),
			CustomResponseBodies: prepared.bodies,
			Description:          r.Description,
			Rules:                prepared.rules,
		}
		err := retryRuleGroup(ctx, isWebACLUnavailable, func(ctx context.Context) error {
			_, err := client.UpdateRuleGroup(ctx, input)
			return err
		}, options...)
		if err != nil {
			return nil, fmt.Errorf("update rule group %s: %w", current.ID, err)
		}
	}
	return readRuleGroup(ctx, client, *current, prepared.rulesJSON, false)
}

func (r *RuleGroupResource) coreChanged(prior RuleGroupResource) bool {
	return runtime.Changed(r.Description, prior.Description) ||
		r.VisibilityConfig != prior.VisibilityConfig ||
		!ruleGroupBodiesEqual(r.CustomResponseBodies, prior.CustomResponseBodies) ||
		!ruleGroupRulesEqual(r.Rules, prior.Rules) ||
		!ruleGroupJSONEqual(r.RulesJSON, prior.RulesJSON)
}

func ruleGroupCoreDrifted(saved, observed *RuleGroupResourceOutput) bool {
	return observed != nil && (runtime.Changed(saved.Description, observed.Description) ||
		saved.VisibilityConfig != observed.VisibilityConfig ||
		!ruleGroupBodiesEqual(&saved.CustomResponseBodies, &observed.CustomResponseBodies) ||
		!ruleGroupRulesEqual(saved.Rules, observed.Rules))
}

func deleteRuleGroup(
	ctx context.Context, client ruleGroupClient, prior ruleGroupPrior, options ...awsretry.Option,
) error {
	current, err := ruleGroupMutationState(prior)
	if err != nil {
		return err
	}
	input := &awssvc.DeleteRuleGroupInput{
		Id: aws.String(current.ID), Name: aws.String(current.Name),
		Scope: awstypes.Scope(current.Scope), LockToken: aws.String(current.LockToken),
	}
	retryable := func(err error) bool {
		return isWebACLUnavailable(err) || isWebACLAssociated(err)
	}
	err = retryRuleGroup(ctx, retryable, func(ctx context.Context) error {
		_, err := client.DeleteRuleGroup(ctx, input)
		return err
	}, options...)
	if err != nil && !isWebACLNotFound(err) {
		return fmt.Errorf("delete rule group %s: %w", current.ID, err)
	}
	return nil
}

func retryRuleGroup(
	ctx context.Context, retryable func(error) bool, call func(context.Context) error,
	options ...awsretry.Option,
) error {
	options = append([]awsretry.Option{awsretry.WithTimeout(5 * time.Minute)}, options...)
	return awsretry.OnError(ctx, retryable, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return call(ctx)
	}, options...)
}
