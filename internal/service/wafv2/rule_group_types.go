package wafv2

type RuleGroupRule struct {
	Action           WebACLRuleAction         `ub:"action"`
	CaptchaConfig    *RuleGroupCaptchaConfig  `ub:"captcha-config"`
	Name             string                   `ub:"name"`
	Priority         int64                    `ub:"priority"`
	RuleLabels       *[]WebACLRuleLabel       `ub:"rule-labels"`
	Statement        RuleGroupStatementLevel3 `ub:"statement"`
	VisibilityConfig WebACLVisibilityConfig   `ub:"visibility-config"`
}

type RuleGroupCaptchaConfig struct {
	ImmunityTimeProperty *RuleGroupImmunityTimeProperty `ub:"immunity-time-property"`
}

type RuleGroupImmunityTimeProperty struct {
	ImmunityTime *int64 `ub:"immunity-time"`
}

type RuleGroupRateBasedStatement struct {
	AggregateKeyType    *string                   `ub:"aggregate-key-type"`
	CustomKeys          *[]WebACLRateCustomKey    `ub:"custom-keys"`
	EvaluationWindowSec *int64                    `ub:"evaluation-window-sec"`
	ForwardedIPConfig   *WebACLForwardedIPConfig  `ub:"forwarded-ip-config"`
	Limit               int64                     `ub:"limit"`
	ScopeDownStatement  *RuleGroupStatementLevel2 `ub:"scope-down-statement"`
}

type RuleGroupJSONBody struct {
	InvalidFallbackBehavior *string                `ub:"invalid-fallback-behavior"`
	MatchPattern            WebACLJSONMatchPattern `ub:"match-pattern"`
	MatchScope              string                 `ub:"match-scope"`
	OversizeHandling        *string                `ub:"oversize-handling"`
}

type RuleGroupFieldToMatch struct {
	AllQueryArguments   *WebACLEmpty         `ub:"all-query-arguments"`
	Body                *WebACLBody          `ub:"body"`
	Cookies             *WebACLCookies       `ub:"cookies"`
	HeaderOrder         *WebACLHeaderOrder   `ub:"header-order"`
	Headers             *WebACLHeaders       `ub:"headers"`
	JA3Fingerprint      *WebACLJAFingerprint `ub:"ja3-fingerprint"`
	JA4Fingerprint      *WebACLJAFingerprint `ub:"ja4-fingerprint"`
	JSONBody            *RuleGroupJSONBody   `ub:"json-body"`
	Method              *WebACLEmpty         `ub:"method"`
	QueryString         *WebACLEmpty         `ub:"query-string"`
	SingleHeader        *WebACLSingleName    `ub:"single-header"`
	SingleQueryArgument *WebACLSingleName    `ub:"single-query-argument"`
	URIFragment         *WebACLURIFragment   `ub:"uri-fragment"`
	URIPath             *WebACLEmpty         `ub:"uri-path"`
}

type RuleGroupByteMatchStatement struct {
	FieldToMatch         RuleGroupFieldToMatch      `ub:"field-to-match"`
	PositionalConstraint string                     `ub:"positional-constraint"`
	SearchString         string                     `ub:"search-string"`
	TextTransformations  []WebACLTextTransformation `ub:"text-transformations"`
}

type RuleGroupRegexMatchStatement struct {
	FieldToMatch        RuleGroupFieldToMatch      `ub:"field-to-match"`
	RegexString         string                     `ub:"regex-string"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type RuleGroupRegexPatternSetReferenceStatement struct {
	FieldToMatch        RuleGroupFieldToMatch      `ub:"field-to-match"`
	ARN                 string                     `ub:"arn"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type RuleGroupSizeConstraintStatement struct {
	FieldToMatch        RuleGroupFieldToMatch      `ub:"field-to-match"`
	ComparisonOperator  string                     `ub:"comparison-operator"`
	Size                int64                      `ub:"size"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type RuleGroupSQLiMatchStatement struct {
	FieldToMatch        RuleGroupFieldToMatch      `ub:"field-to-match"`
	SensitivityLevel    *string                    `ub:"sensitivity-level"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type RuleGroupXSSMatchStatement struct {
	FieldToMatch        RuleGroupFieldToMatch      `ub:"field-to-match"`
	TextTransformations []WebACLTextTransformation `ub:"text-transformations"`
}

type ruleGroupRegexSet = RuleGroupRegexPatternSetReferenceStatement
type ruleGroupSizeMatch = RuleGroupSizeConstraintStatement

type RuleGroupStatementLevel0 struct {
	ASNMatchStatement       *WebACLASNMatchStatement       `ub:"asn-match-statement"`
	ByteMatchStatement      *RuleGroupByteMatchStatement   `ub:"byte-match-statement"`
	GeoMatchStatement       *WebACLGeoMatchStatement       `ub:"geo-match-statement"`
	IPSetReferenceStatement *WebACLIPSetReferenceStatement `ub:"ip-set-reference-statement"`
	LabelMatchStatement     *WebACLLabelMatchStatement     `ub:"label-match-statement"`
	RegexMatchStatement     *RuleGroupRegexMatchStatement  `ub:"regex-match-statement"`
	RegexPatternSet         *ruleGroupRegexSet             `ub:"regex-pattern-set-reference-statement"`
	SizeConstraintStatement *ruleGroupSizeMatch            `ub:"size-constraint-statement"`
	SQLiMatchStatement      *RuleGroupSQLiMatchStatement   `ub:"sqli-match-statement"`
	XSSMatchStatement       *RuleGroupXSSMatchStatement    `ub:"xss-match-statement"`
}

type RuleGroupAndStatementLevel0 struct {
	Statements []RuleGroupStatementLevel0 `ub:"statements"`
}

type RuleGroupOrStatementLevel0 struct {
	Statements []RuleGroupStatementLevel0 `ub:"statements"`
}

type RuleGroupNotStatementLevel0 struct {
	Statement RuleGroupStatementLevel0 `ub:"statement"`
}

type RuleGroupStatementLevel1 struct {
	AndStatement            *RuleGroupAndStatementLevel0   `ub:"and-statement"`
	OrStatement             *RuleGroupOrStatementLevel0    `ub:"or-statement"`
	NotStatement            *RuleGroupNotStatementLevel0   `ub:"not-statement"`
	ASNMatchStatement       *WebACLASNMatchStatement       `ub:"asn-match-statement"`
	ByteMatchStatement      *RuleGroupByteMatchStatement   `ub:"byte-match-statement"`
	GeoMatchStatement       *WebACLGeoMatchStatement       `ub:"geo-match-statement"`
	IPSetReferenceStatement *WebACLIPSetReferenceStatement `ub:"ip-set-reference-statement"`
	LabelMatchStatement     *WebACLLabelMatchStatement     `ub:"label-match-statement"`
	RegexMatchStatement     *RuleGroupRegexMatchStatement  `ub:"regex-match-statement"`
	RegexPatternSet         *ruleGroupRegexSet             `ub:"regex-pattern-set-reference-statement"`
	SizeConstraintStatement *ruleGroupSizeMatch            `ub:"size-constraint-statement"`
	SQLiMatchStatement      *RuleGroupSQLiMatchStatement   `ub:"sqli-match-statement"`
	XSSMatchStatement       *RuleGroupXSSMatchStatement    `ub:"xss-match-statement"`
}

type RuleGroupAndStatementLevel1 struct {
	Statements []RuleGroupStatementLevel1 `ub:"statements"`
}

type RuleGroupOrStatementLevel1 struct {
	Statements []RuleGroupStatementLevel1 `ub:"statements"`
}

type RuleGroupNotStatementLevel1 struct {
	Statement RuleGroupStatementLevel1 `ub:"statement"`
}

type RuleGroupStatementLevel2 struct {
	AndStatement            *RuleGroupAndStatementLevel1   `ub:"and-statement"`
	OrStatement             *RuleGroupOrStatementLevel1    `ub:"or-statement"`
	NotStatement            *RuleGroupNotStatementLevel1   `ub:"not-statement"`
	ASNMatchStatement       *WebACLASNMatchStatement       `ub:"asn-match-statement"`
	ByteMatchStatement      *RuleGroupByteMatchStatement   `ub:"byte-match-statement"`
	GeoMatchStatement       *WebACLGeoMatchStatement       `ub:"geo-match-statement"`
	IPSetReferenceStatement *WebACLIPSetReferenceStatement `ub:"ip-set-reference-statement"`
	LabelMatchStatement     *WebACLLabelMatchStatement     `ub:"label-match-statement"`
	RegexMatchStatement     *RuleGroupRegexMatchStatement  `ub:"regex-match-statement"`
	RegexPatternSet         *ruleGroupRegexSet             `ub:"regex-pattern-set-reference-statement"`
	SizeConstraintStatement *ruleGroupSizeMatch            `ub:"size-constraint-statement"`
	SQLiMatchStatement      *RuleGroupSQLiMatchStatement   `ub:"sqli-match-statement"`
	XSSMatchStatement       *RuleGroupXSSMatchStatement    `ub:"xss-match-statement"`
}

type RuleGroupAndStatementLevel2 struct {
	Statements []RuleGroupStatementLevel2 `ub:"statements"`
}

type RuleGroupOrStatementLevel2 struct {
	Statements []RuleGroupStatementLevel2 `ub:"statements"`
}

type RuleGroupNotStatementLevel2 struct {
	Statement RuleGroupStatementLevel2 `ub:"statement"`
}

type RuleGroupStatementLevel3 struct {
	AndStatement            *RuleGroupAndStatementLevel2   `ub:"and-statement"`
	OrStatement             *RuleGroupOrStatementLevel2    `ub:"or-statement"`
	NotStatement            *RuleGroupNotStatementLevel2   `ub:"not-statement"`
	RateBasedStatement      *RuleGroupRateBasedStatement   `ub:"rate-based-statement"`
	ASNMatchStatement       *WebACLASNMatchStatement       `ub:"asn-match-statement"`
	ByteMatchStatement      *RuleGroupByteMatchStatement   `ub:"byte-match-statement"`
	GeoMatchStatement       *WebACLGeoMatchStatement       `ub:"geo-match-statement"`
	IPSetReferenceStatement *WebACLIPSetReferenceStatement `ub:"ip-set-reference-statement"`
	LabelMatchStatement     *WebACLLabelMatchStatement     `ub:"label-match-statement"`
	RegexMatchStatement     *RuleGroupRegexMatchStatement  `ub:"regex-match-statement"`
	RegexPatternSet         *ruleGroupRegexSet             `ub:"regex-pattern-set-reference-statement"`
	SizeConstraintStatement *ruleGroupSizeMatch            `ub:"size-constraint-statement"`
	SQLiMatchStatement      *RuleGroupSQLiMatchStatement   `ub:"sqli-match-statement"`
	XSSMatchStatement       *RuleGroupXSSMatchStatement    `ub:"xss-match-statement"`
}
