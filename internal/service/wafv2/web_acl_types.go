package wafv2

type WebACLEmpty struct{}

type WebACLCustomHTTPHeader struct {
	Name  string `ub:"name"`
	Value string `ub:"value"`
}

type WebACLCustomRequestHandling struct {
	InsertHeaders []WebACLCustomHTTPHeader `ub:"insert-headers"`
}

type WebACLCustomResponse struct {
	ResponseCode          int64                     `ub:"response-code"`
	CustomResponseBodyKey *string                   `ub:"custom-response-body-key"`
	ResponseHeaders       *[]WebACLCustomHTTPHeader `ub:"response-headers"`
}

type WebACLAllowAction struct {
	CustomRequestHandling *WebACLCustomRequestHandling `ub:"custom-request-handling"`
}

type WebACLBlockAction struct {
	CustomResponse *WebACLCustomResponse `ub:"custom-response"`
}

type WebACLCaptchaAction struct {
	CustomRequestHandling *WebACLCustomRequestHandling `ub:"custom-request-handling"`
}

type WebACLChallengeAction struct {
	CustomRequestHandling *WebACLCustomRequestHandling `ub:"custom-request-handling"`
}

type WebACLCountAction struct {
	CustomRequestHandling *WebACLCustomRequestHandling `ub:"custom-request-handling"`
}

type WebACLNoneAction struct{}

type WebACLDefaultAction struct {
	Allow *WebACLAllowAction `ub:"allow"`
	Block *WebACLBlockAction `ub:"block"`
}

type WebACLRuleAction struct {
	Allow     *WebACLAllowAction     `ub:"allow"`
	Block     *WebACLBlockAction     `ub:"block"`
	Captcha   *WebACLCaptchaAction   `ub:"captcha"`
	Challenge *WebACLChallengeAction `ub:"challenge"`
	Count     *WebACLCountAction     `ub:"count"`
}

type WebACLOverrideAction struct {
	Count *WebACLEmpty      `ub:"count"`
	None  *WebACLNoneAction `ub:"none"`
}

type WebACLRuleActionOverride struct {
	ActionToUse WebACLRuleAction `ub:"action-to-use"`
	Name        string           `ub:"name"`
}

type WebACLVisibilityConfig struct {
	CloudWatchMetricsEnabled bool   `ub:"cloudwatch-metrics-enabled"`
	MetricName               string `ub:"metric-name"`
	SampledRequestsEnabled   bool   `ub:"sampled-requests-enabled"`
}

type WebACLImmunityTimeProperty struct {
	ImmunityTime int64 `ub:"immunity-time"`
}

type WebACLCaptchaConfig struct {
	ImmunityTimeProperty *WebACLImmunityTimeProperty `ub:"immunity-time-property"`
}

type WebACLChallengeConfig struct {
	ImmunityTimeProperty *WebACLImmunityTimeProperty `ub:"immunity-time-property"`
}

type WebACLTextTransformation struct {
	Priority int64  `ub:"priority"`
	Type     string `ub:"type"`
}

type WebACLForwardedIPConfig struct {
	FallbackBehavior string `ub:"fallback-behavior"`
	HeaderName       string `ub:"header-name"`
}

type WebACLIPSetForwardedIPConfig struct {
	FallbackBehavior string `ub:"fallback-behavior"`
	HeaderName       string `ub:"header-name"`
	Position         string `ub:"position"`
}

type WebACLBody struct {
	OversizeHandling *string `ub:"oversize-handling"`
}

type WebACLCookieMatchPattern struct {
	All             *WebACLEmpty `ub:"all"`
	ExcludedCookies *[]string    `ub:"excluded-cookies"`
	IncludedCookies *[]string    `ub:"included-cookies"`
}

type WebACLCookies struct {
	MatchPattern     WebACLCookieMatchPattern `ub:"match-pattern"`
	MatchScope       string                   `ub:"match-scope"`
	OversizeHandling string                   `ub:"oversize-handling"`
}

type WebACLHeaderOrder struct {
	OversizeHandling string `ub:"oversize-handling"`
}

type WebACLHeaderMatchPattern struct {
	All             *WebACLEmpty `ub:"all"`
	ExcludedHeaders *[]string    `ub:"excluded-headers"`
	IncludedHeaders *[]string    `ub:"included-headers"`
}

type WebACLHeaders struct {
	MatchPattern     WebACLHeaderMatchPattern `ub:"match-pattern"`
	MatchScope       string                   `ub:"match-scope"`
	OversizeHandling string                   `ub:"oversize-handling"`
}

type WebACLJAFingerprint struct {
	FallbackBehavior string `ub:"fallback-behavior"`
}

type WebACLJSONMatchPattern struct {
	All           *WebACLEmpty `ub:"all"`
	IncludedPaths *[]string    `ub:"included-paths"`
}

type WebACLJSONBody struct {
	InvalidFallbackBehavior *string                `ub:"invalid-fallback-behavior"`
	MatchPattern            WebACLJSONMatchPattern `ub:"match-pattern"`
	MatchScope              string                 `ub:"match-scope"`
	OversizeHandling        string                 `ub:"oversize-handling"`
}

type WebACLSingleName struct {
	Name string `ub:"name"`
}

type WebACLURIFragment struct {
	FallbackBehavior *string `ub:"fallback-behavior"`
}

type WebACLFieldToMatch struct {
	AllQueryArguments   *WebACLEmpty         `ub:"all-query-arguments"`
	Body                *WebACLBody          `ub:"body"`
	Cookies             *WebACLCookies       `ub:"cookies"`
	HeaderOrder         *WebACLHeaderOrder   `ub:"header-order"`
	Headers             *WebACLHeaders       `ub:"headers"`
	JA3Fingerprint      *WebACLJAFingerprint `ub:"ja3-fingerprint"`
	JA4Fingerprint      *WebACLJAFingerprint `ub:"ja4-fingerprint"`
	JSONBody            *WebACLJSONBody      `ub:"json-body"`
	Method              *WebACLEmpty         `ub:"method"`
	QueryString         *WebACLEmpty         `ub:"query-string"`
	SingleHeader        *WebACLSingleName    `ub:"single-header"`
	SingleQueryArgument *WebACLSingleName    `ub:"single-query-argument"`
	URIFragment         *WebACLURIFragment   `ub:"uri-fragment"`
	URIPath             *WebACLEmpty         `ub:"uri-path"`
}

type WebACLASNMatchStatement struct {
	ASNList           []int64                  `ub:"asn-list"`
	ForwardedIPConfig *WebACLForwardedIPConfig `ub:"forwarded-ip-config"`
}

type WebACLByteMatchStatement struct {
	FieldToMatch         WebACLFieldToMatch         `ub:"field-to-match"`
	PositionalConstraint string                     `ub:"positional-constraint"`
	SearchString         *string                    `ub:"search-string"`
	SearchStringBase64   *string                    `ub:"search-string-base64"`
	TextTransformations  []WebACLTextTransformation `ub:"text-transformations"`
}

type WebACLGeoMatchStatement struct {
	CountryCodes      []string                 `ub:"country-codes"`
	ForwardedIPConfig *WebACLForwardedIPConfig `ub:"forwarded-ip-config"`
}

type WebACLIPSetReferenceStatement struct {
	ARN                    string                        `ub:"arn"`
	IPSetForwardedIPConfig *WebACLIPSetForwardedIPConfig `ub:"ip-set-forwarded-ip-config"`
}

type WebACLLabelMatchStatement struct {
	Key   string `ub:"key"`
	Scope string `ub:"scope"`
}

type WebACLRegexMatchStatement struct {
	FieldToMatch        WebACLFieldToMatch         `ub:"field-to-match"`
	RegexString         string                     `ub:"regex-string"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type WebACLRegexPatternSetReferenceStatement struct {
	ARN                 string                     `ub:"arn"`
	FieldToMatch        WebACLFieldToMatch         `ub:"field-to-match"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type WebACLSizeConstraintStatement struct {
	ComparisonOperator  string                     `ub:"comparison-operator"`
	FieldToMatch        WebACLFieldToMatch         `ub:"field-to-match"`
	Size                int64                      `ub:"size"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type WebACLSQLiMatchStatement struct {
	FieldToMatch        WebACLFieldToMatch         `ub:"field-to-match"`
	SensitivityLevel    *string                    `ub:"sensitivity-level"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type WebACLXSSMatchStatement struct {
	FieldToMatch        WebACLFieldToMatch         `ub:"field-to-match"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type WebACLExcludedRule struct {
	Name string `ub:"name"`
}

type WebACLManagedRuleGroupIdentifierList struct {
	Identifiers []string `ub:"identifiers"`
}

type WebACLManagedRequestInspectionACFP struct {
	AddressFields     *WebACLManagedRuleGroupIdentifierList  `ub:"address-fields"`
	EmailField        *WebACLManagedRuleGroupIdentifierField `ub:"email-field"`
	PasswordField     *WebACLManagedRuleGroupIdentifierField `ub:"password-field"`
	PayloadType       string                                 `ub:"payload-type"`
	PhoneNumberFields *WebACLManagedRuleGroupIdentifierList  `ub:"phone-number-fields"`
	UsernameField     *WebACLManagedRuleGroupIdentifierField `ub:"username-field"`
}

type WebACLManagedRequestInspection struct {
	PasswordField *WebACLManagedRuleGroupIdentifierField `ub:"password-field"`
	PayloadType   string                                 `ub:"payload-type"`
	UsernameField *WebACLManagedRuleGroupIdentifierField `ub:"username-field"`
}

type WebACLManagedResponseInspectionBodyContains struct {
	FailureStrings *[]string `ub:"failure-strings"`
	SuccessStrings *[]string `ub:"success-strings"`
}

type WebACLManagedResponseInspectionHeader struct {
	FailureValues *[]string `ub:"failure-values"`
	Name          string    `ub:"name"`
	SuccessValues *[]string `ub:"success-values"`
}

type WebACLManagedResponseInspectionJSON struct {
	FailureValues *[]string `ub:"failure-values"`
	Identifier    string    `ub:"identifier"`
	SuccessValues *[]string `ub:"success-values"`
}

type WebACLManagedResponseInspectionStatusCode struct {
	FailureCodes *[]int64 `ub:"failure-codes"`
	SuccessCodes *[]int64 `ub:"success-codes"`
}

type WebACLManagedResponseInspection struct {
	BodyContains *WebACLManagedResponseInspectionBodyContains `ub:"body-contains"`
	Header       *WebACLManagedResponseInspectionHeader       `ub:"header"`
	JSON         *WebACLManagedResponseInspectionJSON         `ub:"json"`
	StatusCode   *WebACLManagedResponseInspectionStatusCode   `ub:"status-code"`
}

type WebACLAWSManagedRulesACFPRuleSet struct {
	CreationPath         string                              `ub:"creation-path"`
	EnableRegexInPath    *bool                               `ub:"enable-regex-in-path"`
	RegistrationPagePath string                              `ub:"registration-page-path"`
	RequestInspection    *WebACLManagedRequestInspectionACFP `ub:"request-inspection"`
	ResponseInspection   *WebACLManagedResponseInspection    `ub:"response-inspection"`
}

type WebACLAWSManagedRulesATPRuleSet struct {
	EnableRegexInPath  *bool                            `ub:"enable-regex-in-path"`
	LoginPath          string                           `ub:"login-path"`
	RequestInspection  *WebACLManagedRequestInspection  `ub:"request-inspection"`
	ResponseInspection *WebACLManagedResponseInspection `ub:"response-inspection"`
}

type WebACLManagedRuleGroupRegex struct {
	RegexString string `ub:"regex-string"`
}

type WebACLClientSideAction struct {
	ExemptURIRegularExpressions *[]WebACLManagedRuleGroupRegex `ub:"exempt-uri-regular-expressions"`
	Sensitivity                 *string                        `ub:"sensitivity"`
	UsageOfAction               string                         `ub:"usage-of-action"`
}

type WebACLClientSideActionConfig struct {
	Challenge *WebACLClientSideAction `ub:"challenge"`
}

type WebACLAWSManagedRulesAntiDDoSRuleSet struct {
	ClientSideActionConfig *WebACLClientSideActionConfig `ub:"client-side-action-config"`
	SensitivityToBlock     *string                       `ub:"sensitivity-to-block"`
}

type WebACLAWSManagedRulesBotControlRuleSet struct {
	EnableMachineLearning *bool  `ub:"enable-machine-learning"`
	InspectionLevel       string `ub:"inspection-level"`
}

type WebACLManagedRuleGroupIdentifierField struct {
	Identifier string `ub:"identifier"`
}

type mgACFP = WebACLAWSManagedRulesACFPRuleSet
type mgATP = WebACLAWSManagedRulesATPRuleSet
type mgDDoS = WebACLAWSManagedRulesAntiDDoSRuleSet
type mgBot = WebACLAWSManagedRulesBotControlRuleSet

type WebACLManagedRuleGroupConfig struct {
	AWSManagedRulesACFPRuleSet *mgACFP `ub:"aws-managed-rules-acfp-rule-set"`

	AWSManagedRulesATPRuleSet *mgATP `ub:"aws-managed-rules-atp-rule-set"`

	AWSManagedRulesAntiDDoSRuleSet *mgDDoS `ub:"aws-managed-rules-anti-ddos-rule-set"`

	AWSManagedRulesBotControlRuleSet *mgBot `ub:"aws-managed-rules-bot-control-rule-set"`

	LoginPath     *string                                `ub:"login-path"`
	PasswordField *WebACLManagedRuleGroupIdentifierField `ub:"password-field"`
	PayloadType   *string                                `ub:"payload-type"`
	UsernameField *WebACLManagedRuleGroupIdentifierField `ub:"username-field"`
}

type WebACLManagedRuleGroupStatement struct {
	ExcludedRules           *[]WebACLExcludedRule           `ub:"excluded-rules"`
	ManagedRuleGroupConfigs *[]WebACLManagedRuleGroupConfig `ub:"managed-rule-group-configs"`
	Name                    string                          `ub:"name"`
	RuleActionOverrides     *[]WebACLRuleActionOverride     `ub:"rule-action-overrides"`
	ScopeDownStatement      *WebACLStatementLevel2          `ub:"scope-down-statement"`
	VendorName              string                          `ub:"vendor-name"`
	Version                 *string                         `ub:"version"`
}

type WebACLRateCustomKey struct {
	ASN            *WebACLEmpty              `ub:"asn"`
	Cookie         *WebACLRateNamedKey       `ub:"cookie"`
	ForwardedIP    *WebACLEmpty              `ub:"forwarded-ip"`
	HTTPMethod     *WebACLEmpty              `ub:"http-method"`
	Header         *WebACLRateNamedKey       `ub:"header"`
	IP             *WebACLEmpty              `ub:"ip"`
	JA3Fingerprint *WebACLJAFingerprint      `ub:"ja3-fingerprint"`
	JA4Fingerprint *WebACLJAFingerprint      `ub:"ja4-fingerprint"`
	LabelNamespace *WebACLRateLabelNamespace `ub:"label-namespace"`
	QueryArgument  *WebACLRateNamedKey       `ub:"query-argument"`
	QueryString    *WebACLRateTransformedKey `ub:"query-string"`
	URIPath        *WebACLRateTransformedKey `ub:"uri-path"`
}

type WebACLRateNamedKey struct {
	Name                string                     `ub:"name"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type WebACLRateLabelNamespace struct {
	Namespace string `ub:"namespace"`
}

type WebACLRateTransformedKey struct {
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type WebACLRateBasedStatement struct {
	AggregateKeyType    string                   `ub:"aggregate-key-type"`
	CustomKeys          *[]WebACLRateCustomKey   `ub:"custom-keys"`
	EvaluationWindowSec *int64                   `ub:"evaluation-window-sec"`
	ForwardedIPConfig   *WebACLForwardedIPConfig `ub:"forwarded-ip-config"`
	Limit               int64                    `ub:"limit"`
	ScopeDownStatement  *WebACLStatementLevel2   `ub:"scope-down-statement"`
}

type WebACLRuleGroupReferenceStatement struct {
	ARN                 string                      `ub:"arn"`
	ExcludedRules       *[]WebACLExcludedRule       `ub:"excluded-rules"`
	RuleActionOverrides *[]WebACLRuleActionOverride `ub:"rule-action-overrides"`
}

type levelASN = WebACLASNMatchStatement
type levelByte = WebACLByteMatchStatement
type levelGeo = WebACLGeoMatchStatement
type levelIPSet = WebACLIPSetReferenceStatement
type levelLabel = WebACLLabelMatchStatement
type levelManaged = WebACLManagedRuleGroupStatement
type levelRate = WebACLRateBasedStatement
type levelRegex = WebACLRegexMatchStatement
type levelRPS = WebACLRegexPatternSetReferenceStatement
type levelRuleGroup = WebACLRuleGroupReferenceStatement
type levelSize = WebACLSizeConstraintStatement
type levelSQLi = WebACLSQLiMatchStatement
type levelXSS = WebACLXSSMatchStatement

type WebACLStatementLevel0 struct {
	ASNMatchStatement        *levelASN   `ub:"asn-match-statement"`
	ByteMatchStatement       *levelByte  `ub:"byte-match-statement"`
	GeoMatchStatement        *levelGeo   `ub:"geo-match-statement"`
	IPSetReferenceStatement  *levelIPSet `ub:"ip-set-reference-statement"`
	LabelMatchStatement      *levelLabel `ub:"label-match-statement"`
	RegexMatchStatement      *levelRegex `ub:"regex-match-statement"`
	RegexPatternSetReference *levelRPS   `ub:"regex-pattern-set-reference-statement"`
	SizeConstraintStatement  *levelSize  `ub:"size-constraint-statement"`
	SQLiMatchStatement       *levelSQLi  `ub:"sqli-match-statement"`
	XSSMatchStatement        *levelXSS   `ub:"xss-match-statement"`
}

type WebACLAndStatementLevel0 struct {
	Statements []WebACLStatementLevel0 `ub:"statements"`
}

type WebACLNotStatementLevel0 struct {
	Statement WebACLStatementLevel0 `ub:"statement"`
}

type WebACLOrStatementLevel0 struct {
	Statements []WebACLStatementLevel0 `ub:"statements"`
}

type WebACLStatementLevel1 struct {
	AndStatement             *WebACLAndStatementLevel0 `ub:"and-statement"`
	NotStatement             *WebACLNotStatementLevel0 `ub:"not-statement"`
	OrStatement              *WebACLOrStatementLevel0  `ub:"or-statement"`
	ASNMatchStatement        *levelASN                 `ub:"asn-match-statement"`
	ByteMatchStatement       *levelByte                `ub:"byte-match-statement"`
	GeoMatchStatement        *levelGeo                 `ub:"geo-match-statement"`
	IPSetReferenceStatement  *levelIPSet               `ub:"ip-set-reference-statement"`
	LabelMatchStatement      *levelLabel               `ub:"label-match-statement"`
	RegexMatchStatement      *levelRegex               `ub:"regex-match-statement"`
	RegexPatternSetReference *levelRPS                 `ub:"regex-pattern-set-reference-statement"`
	SizeConstraintStatement  *levelSize                `ub:"size-constraint-statement"`
	SQLiMatchStatement       *levelSQLi                `ub:"sqli-match-statement"`
	XSSMatchStatement        *levelXSS                 `ub:"xss-match-statement"`
}

type WebACLAndStatementLevel1 struct {
	Statements []WebACLStatementLevel1 `ub:"statements"`
}

type WebACLNotStatementLevel1 struct {
	Statement WebACLStatementLevel1 `ub:"statement"`
}

type WebACLOrStatementLevel1 struct {
	Statements []WebACLStatementLevel1 `ub:"statements"`
}

type WebACLStatementLevel2 struct {
	AndStatement             *WebACLAndStatementLevel1 `ub:"and-statement"`
	NotStatement             *WebACLNotStatementLevel1 `ub:"not-statement"`
	OrStatement              *WebACLOrStatementLevel1  `ub:"or-statement"`
	ASNMatchStatement        *levelASN                 `ub:"asn-match-statement"`
	ByteMatchStatement       *levelByte                `ub:"byte-match-statement"`
	GeoMatchStatement        *levelGeo                 `ub:"geo-match-statement"`
	IPSetReferenceStatement  *levelIPSet               `ub:"ip-set-reference-statement"`
	LabelMatchStatement      *levelLabel               `ub:"label-match-statement"`
	RegexMatchStatement      *levelRegex               `ub:"regex-match-statement"`
	RegexPatternSetReference *levelRPS                 `ub:"regex-pattern-set-reference-statement"`
	SizeConstraintStatement  *levelSize                `ub:"size-constraint-statement"`
	SQLiMatchStatement       *levelSQLi                `ub:"sqli-match-statement"`
	XSSMatchStatement        *levelXSS                 `ub:"xss-match-statement"`
}

type WebACLAndStatementLevel2 struct {
	Statements []WebACLStatementLevel2 `ub:"statements"`
}

type WebACLNotStatementLevel2 struct {
	Statement WebACLStatementLevel2 `ub:"statement"`
}

type WebACLOrStatementLevel2 struct {
	Statements []WebACLStatementLevel2 `ub:"statements"`
}

type WebACLStatementLevel3 struct {
	AndStatement                *WebACLAndStatementLevel2 `ub:"and-statement"`
	NotStatement                *WebACLNotStatementLevel2 `ub:"not-statement"`
	OrStatement                 *WebACLOrStatementLevel2  `ub:"or-statement"`
	ASNMatchStatement           *levelASN                 `ub:"asn-match-statement"`
	ByteMatchStatement          *levelByte                `ub:"byte-match-statement"`
	GeoMatchStatement           *levelGeo                 `ub:"geo-match-statement"`
	IPSetReferenceStatement     *levelIPSet               `ub:"ip-set-reference-statement"`
	LabelMatchStatement         *levelLabel               `ub:"label-match-statement"`
	ManagedRuleGroupStatement   *levelManaged             `ub:"managed-rule-group-statement"`
	RateBasedStatement          *levelRate                `ub:"rate-based-statement"`
	RegexMatchStatement         *levelRegex               `ub:"regex-match-statement"`
	RegexPatternSetReference    *levelRPS                 `ub:"regex-pattern-set-reference-statement"`
	RuleGroupReferenceStatement *levelRuleGroup           `ub:"rule-group-reference-statement"`
	SizeConstraintStatement     *levelSize                `ub:"size-constraint-statement"`
	SQLiMatchStatement          *levelSQLi                `ub:"sqli-match-statement"`
	XSSMatchStatement           *levelXSS                 `ub:"xss-match-statement"`
}

type WebACLRuleLabel struct {
	Name string `ub:"name"`
}

type WebACLRule struct {
	Action           *WebACLRuleAction      `ub:"action"`
	CaptchaConfig    *WebACLCaptchaConfig   `ub:"captcha-config"`
	ChallengeConfig  *WebACLChallengeConfig `ub:"challenge-config"`
	Name             string                 `ub:"name"`
	OverrideAction   *WebACLOverrideAction  `ub:"override-action"`
	Priority         int64                  `ub:"priority"`
	RuleLabels       *[]WebACLRuleLabel     `ub:"rule-labels"`
	Statement        WebACLStatementLevel3  `ub:"statement"`
	VisibilityConfig WebACLVisibilityConfig `ub:"visibility-config"`
}

type WebACLApplicationAttribute struct {
	Name   string   `ub:"name"`
	Values []string `ub:"values"`
}

type WebACLApplicationConfig struct {
	Attributes []WebACLApplicationAttribute `ub:"attributes"`
}

type WebACLAssociationRequestBodyConfig struct {
	DefaultSizeInspectionLimit string `ub:"default-size-inspection-limit"`
}

type WebACLAssociationRequestBody struct {
	APIGateway             *WebACLAssociationRequestBodyConfig `ub:"api-gateway"`
	AppRunnerService       *WebACLAssociationRequestBodyConfig `ub:"app-runner-service"`
	CloudFront             *WebACLAssociationRequestBodyConfig `ub:"cloudfront"`
	CognitoUserPool        *WebACLAssociationRequestBodyConfig `ub:"cognito-user-pool"`
	VerifiedAccessInstance *WebACLAssociationRequestBodyConfig `ub:"verified-access-instance"`
}

type WebACLAssociationConfig struct {
	RequestBody WebACLAssociationRequestBody `ub:"request-body"`
}

type WebACLCustomResponseBody struct {
	Content     string `ub:"content"`
	ContentType string `ub:"content-type"`
}

type WebACLDataProtectionField struct {
	FieldKeys *[]string `ub:"field-keys"`
	FieldType string    `ub:"field-type"`
}

type WebACLDataProtection struct {
	Action                  string                    `ub:"action"`
	ExcludeRateBasedDetails bool                      `ub:"exclude-rate-based-details"`
	ExcludeRuleMatchDetails bool                      `ub:"exclude-rule-match-details"`
	Field                   WebACLDataProtectionField `ub:"field"`
}

type WebACLDataProtectionConfig struct {
	DataProtections []WebACLDataProtection `ub:"data-protections"`
}

type WebACLOnSourceDDoSConfig struct {
	ALBLowReputationMode string `ub:"alb-low-reputation-mode"`
}

type WebACLResource struct {
	ApplicationConfig    *WebACLApplicationConfig             `ub:"application-config"`
	AssociationConfig    *WebACLAssociationConfig             `ub:"association-config"`
	CaptchaConfig        *WebACLCaptchaConfig                 `ub:"captcha-config"`
	ChallengeConfig      *WebACLChallengeConfig               `ub:"challenge-config"`
	CustomResponseBodies *map[string]WebACLCustomResponseBody `ub:"custom-response-bodies"`
	DataProtectionConfig *WebACLDataProtectionConfig          `ub:"data-protection-config"`
	OnSourceDDoSConfig   *WebACLOnSourceDDoSConfig            `ub:"on-source-ddos-protection-config"`
	Name                 *string                              `ub:"name"`
	Scope                string                               `ub:"scope"`
	DefaultAction        WebACLDefaultAction                  `ub:"default-action"`
	VisibilityConfig     WebACLVisibilityConfig               `ub:"visibility-config"`
	Description          *string                              `ub:"description"`
	Rules                *[]WebACLRule                        `ub:"rules"`
	TokenDomains         *[]string                            `ub:"token-domains"`
	Tags                 *map[string]string                   `ub:"tags"`
}

type WebACLResourceOutput struct {
	ARN                       string  `ub:"arn"`
	ID                        string  `ub:"id"`
	Capacity                  int64   `ub:"capacity"`
	LabelNamespace            string  `ub:"label-namespace"`
	ApplicationIntegrationURL *string `ub:"application-integration-url"`
	LockToken                 string  `ub:"lock-token"`
}
