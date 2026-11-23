package wafv2_test

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/testutil"
)

func TestRuleGroupNativeStatementConstraint(t *testing.T) {
	schema := readLibrarySchema(t).Resources["rule-group"]
	require.NotNil(t, schema)
	inputs := map[string]any{
		"capacity":          int64(1),
		"scope":             "REGIONAL",
		"visibility-config": ruleGroupVisibility("group"),
		"rules": []any{map[string]any{
			"name": "a", "priority": int64(0), "visibility-config": ruleGroupVisibility("a"),
			"action": map[string]any{"allow": map[string]any{}},
			"statement": map[string]any{"not-statement": map[string]any{
				"statement": map[string]any{"label-match-statement": map[string]any{
					"scope": "INVALID", "key": "app:label",
				}},
			}},
		}},
	}
	errors := testutil.CheckConstraints(t, schema, inputs)
	require.Len(t, errors, 1)
	require.Contains(t, errors[0], "statement scalar or collection input")
}

func TestRuleGroupCompileLiterals(t *testing.T) {
	module := moduleRoot(t, ".")
	unobin := unobinRoot(t)
	for _, name := range []string{"wider", "invalid", "invalid-native"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", "ub", "rule-group", "compile", name, "factory.ub")
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			parsed, err := syntax.ParseSource(path, src)
			require.NoError(t, err)
			err = compile.Run(compile.Options{
				FactoryPath: path, OutDir: "-", CLIVersion: "v0.13.0-a.4",
				ReplaceUnobin: unobin.Dir, ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(directory string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(directory), nil
				},
				Stdout: io.Discard, Stderr: io.Discard,
			})
			if name == "wider" {
				require.NoError(t, err)
				return
			}
			var diagnostics *lang.ErrorList
			if !assert.ErrorAs(t, err, &diagnostics) {
				t.Fatalf("expected compiler diagnostics, got %T", err)
			}
			byResource := map[string][]string{}
			for _, entry := range diagnostics.Errors() {
				for _, node := range parsed.Factory.Body.Resources {
					if node.S.Start.Line <= entry.Pos.Line && entry.Pos.Line <= node.S.End.Line {
						message := entry.Msg
						if inferred := strings.LastIndex(message, "), got "); inferred >= 0 {
							message = "type mismatch" + message[inferred+1:]
						}
						byResource[node.Name.Name] = append(byResource[node.Name.Name], message)
					}
				}
			}
			for resource, want := range map[string]string{
				"too-deep":                 "statement must select exactly one member",
				"nested-rate":              "statement must select exactly one member",
				"managed-reference":        "statement must select exactly one member",
				"group-reference":          "statement must select exactly one member",
				"unsupported-rule-members": "unknown field \"tags-all\"",
				"encoded-search":           "search-string-base64",
				"missing-priority":         "type mismatch",
				"null-priority":            "priority: null",
				"missing-action":           "type mismatch",
				"missing-visibility-flag":  "type mismatch",
				"missing-byte-field":       "type mismatch",
				"missing-regex-field":      "type mismatch",
				"missing-regex-set-field":  "type mismatch",
				"missing-size-field":       "type mismatch",
				"missing-sqli-field":       "type mismatch",
				"missing-xss-field":        "type mismatch",
				"zero-capacity":            "constraints[2]",
				"bad-visibility":           "constraints[6]",
				"bad-json-path":            "invalid cookies, headers or JSON body match pattern",
				"bad-nested-leaf":          "statement scalar or collection input",
				"bad-rule-action":          "exactly-one-of",
				"two-names":                "at-most-one-of [name, name-prefix]",
				"two-modes":                "at-most-one-of [rules, rules-json]",
			} {
				for _, node := range parsed.Factory.Body.Resources {
					if node.Name.Name == resource {
						assert.Contains(t, strings.Join(byResource[resource], "\n"), want, resource)
					}
				}
			}
		})
	}
}

func TestRuleGroupNativeEnums(t *testing.T) {
	schema := readLibrarySchema(t).Resources["rule-group"]
	for _, tc := range []struct {
		name       string
		sample     string
		configure  func(map[string]any, map[string]any, string)
		diagnostic string
	}{
		{"scope", "REGIONAL", func(input, _ map[string]any, value string) {
			input["scope"] = value
		}, "constraints[3]"},
		{"label scope", "LABEL", func(_, rule map[string]any, value string) {
			rule["statement"].(map[string]any)["label-match-statement"].(map[string]any)["scope"] = value
		}, "statement scalar or collection input"},
		{"match scope", "KEY", func(_, rule map[string]any, value string) {
			setByteField(rule, "json-body", map[string]any{
				"match-scope": value, "match-pattern": map[string]any{"all": map[string]any{}},
			})
		}, "invalid name, scope or handling option"},
		{"oversize", "NO_MATCH", func(_, rule map[string]any, value string) {
			setByteField(rule, "header-order", map[string]any{"oversize-handling": value})
		}, "invalid name, scope or handling option"},
		{"fallback", "MATCH", func(_, rule map[string]any, value string) {
			setByteField(rule, "ja4-fingerprint", map[string]any{"fallback-behavior": value})
		}, "invalid name, scope or handling option"},
		{"JSON invalid fallback", "EVALUATE_AS_STRING", func(_, rule map[string]any, value string) {
			setByteField(rule, "json-body", map[string]any{
				"match-scope": "KEY", "invalid-fallback-behavior": value,
				"match-pattern": map[string]any{"all": map[string]any{}},
			})
		}, "invalid name, scope or handling option"},
		{"IP position", "FIRST", func(_, rule map[string]any, value string) {
			rule["statement"] = map[string]any{"ip-set-reference-statement": map[string]any{
				"arn": "",
				"ip-set-forwarded-ip-config": map[string]any{
					"position": value, "fallback-behavior": "MATCH", "header-name": "X-IP",
				},
			}}
		}, "forwarded IP configuration"},
		{"byte position", "CONTAINS_WORD", func(_, rule map[string]any, value string) {
			match := setByteField(rule, "method", map[string]any{})
			match["positional-constraint"] = value
		}, "statement scalar or collection input"},
		{"comparison", "EQ", func(_, rule map[string]any, value string) {
			match := setByteField(rule, "method", map[string]any{})
			delete(match, "search-string")
			delete(match, "positional-constraint")
			match["comparison-operator"], match["size"] = value, int64(0)
			rule["statement"] = map[string]any{"size-constraint-statement": match}
		}, "statement scalar or collection input"},
		{"SQLi sensitivity", "LOW", func(_, rule map[string]any, value string) {
			match := setByteField(rule, "method", map[string]any{})
			delete(match, "search-string")
			delete(match, "positional-constraint")
			match["sensitivity-level"] = value
			rule["statement"] = map[string]any{"sqli-match-statement": match}
		}, "statement scalar or collection input"},
		{"rate aggregation", "IP", func(_, rule map[string]any, value string) {
			rule["statement"] = map[string]any{"rate-based-statement": map[string]any{
				"limit": int64(10), "aggregate-key-type": value,
			}}
		}, "rate-based aggregate-key-type"},
		{"content type", "TEXT_PLAIN", func(input, _ map[string]any, value string) {
			input["custom-response-bodies"] = map[string]any{
				"plain": map[string]any{"content": "body", "content-type": value},
			}
		}, "invalid key, content or type"},
		{"transform type", "HTML_ENTITY_DECODE", func(_, rule map[string]any, value string) {
			match := setByteField(rule, "method", map[string]any{})
			match["text-transformations"] = []any{map[string]any{"priority": int64(0), "type": value}}
		}, "text-transformations requires entries with a valid type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := labelRule("example")
			inputs := ruleGroupConstraintInputs(rule)
			tc.configure(inputs, rule, tc.sample)
			require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
			for _, value := range []string{strings.ToLower(tc.sample), "UNRECOGNIZED"} {
				tc.configure(inputs, rule, value)
				errs := testutil.CheckConstraints(t, schema, inputs)
				require.Len(t, errs, 1)
				require.Contains(t, errs[0], tc.diagnostic)
			}
		})
	}
}

func ruleGroupConstraintInputs(rule map[string]any) map[string]any {
	return map[string]any{
		"capacity": int64(1), "scope": "REGIONAL", "visibility-config": ruleGroupVisibility("valid"),
		"rules": []any{rule},
	}
}

func setByteField(rule map[string]any, field string, data map[string]any) map[string]any {
	match := map[string]any{
		"field-to-match": map[string]any{field: data},
		"search-string":  "literal", "positional-constraint": "EXACTLY",
		"text-transformations": []any{map[string]any{"priority": int64(0), "type": "NONE"}},
	}
	rule["statement"] = map[string]any{"byte-match-statement": match}
	return match
}

func TestRuleGroupNativeNestedConditions(t *testing.T) {
	schema := readLibrarySchema(t).Resources["rule-group"]
	for _, tc := range []struct {
		name string
		rate map[string]any
		want string
	}{
		{"provider defaults", map[string]any{"limit": int64(10)}, ""},
		{"largest limit", map[string]any{"limit": int64(2_000_000_000)}, ""},
		{"limit below minimum", map[string]any{"limit": int64(9)}, "invalid limit"},
		{"limit above maximum", map[string]any{"limit": int64(2_000_000_001)}, "invalid limit"},
		{"invalid window", map[string]any{"limit": int64(10), "evaluation-window-sec": int64(61)},
			"evaluation-window-sec must be 60, 120, 300 or 600"},
		{"constant without scope-down", map[string]any{
			"limit": int64(10), "aggregate-key-type": "CONSTANT",
		}, "CONSTANT requires scope-down-statement"},
		{"custom without keys", map[string]any{
			"limit": int64(10), "aggregate-key-type": "CUSTOM_KEYS",
		}, "CUSTOM_KEYS requires custom-keys"},
		{"explicit empty rate keys", map[string]any{
			"limit": int64(10), "custom-keys": []any{},
		}, "custom-keys length"},
		{"forwarded aggregation without config", map[string]any{
			"limit": int64(10), "aggregate-key-type": "FORWARDED_IP",
		}, "requires forwarded-ip-config"},
		{"forwarded custom key without config", map[string]any{
			"limit": int64(10), "aggregate-key-type": "CUSTOM_KEYS",
			"custom-keys": []any{map[string]any{"forwarded-ip": map[string]any{}}},
		}, "requires forwarded-ip-config"},
		{"forwarded custom key with config", map[string]any{
			"limit": int64(10), "aggregate-key-type": "CUSTOM_KEYS",
			"custom-keys": []any{map[string]any{"forwarded-ip": map[string]any{}}},
			"forwarded-ip-config": map[string]any{
				"header-name": "accepted spaces!", "fallback-behavior": "MATCH",
			},
		}, ""},
		{"multiple selections", map[string]any{
			"limit":       int64(10),
			"custom-keys": []any{map[string]any{"ip": map[string]any{}, "asn": map[string]any{}}},
		}, "expected exactly one to be set, got 2"},
		{"rate named key with transforms", map[string]any{
			"limit": int64(10),
			"custom-keys": []any{map[string]any{"header": map[string]any{
				"name":                 strings.Repeat("!", 64),
				"text-transformations": []any{map[string]any{"priority": int64(-1), "type": "NONE"}},
			}}},
		}, ""},
		{"named key exceeds bound", map[string]any{
			"limit": int64(10),
			"custom-keys": []any{map[string]any{"cookie": map[string]any{
				"name":                 strings.Repeat("!", 65),
				"text-transformations": []any{map[string]any{"priority": int64(-1), "type": "NONE"}},
			}}},
		}, "rate custom key name must be 1 to 64 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := labelRule("example")
			rule["statement"] = map[string]any{"rate-based-statement": tc.rate}
			errs := testutil.CheckConstraints(t, schema, ruleGroupConstraintInputs(rule))
			if tc.want == "" {
				require.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1)
			require.Contains(t, errs[0], tc.want)
		})
	}
}

func TestRuleGroupNativeBoundaries(t *testing.T) {
	schema := readLibrarySchema(t).Resources["rule-group"]
	type testCase struct {
		name       string
		max        int
		unit       string
		apply      func(input, rule map[string]any, text string)
		diagnostic string
	}
	cases := []testCase{
		{"name", 128, "a", func(input, _ map[string]any, text string) {
			input["name"] = text
		}, "constraints[4]"},
		{"name prefix", 102, "a", func(input, _ map[string]any, text string) {
			input["name-prefix"] = text
		}, "constraints[5]"},
		{"metric name", 128, "a", func(input, _ map[string]any, text string) {
			input["visibility-config"] = ruleGroupVisibility(text)
		}, "constraints[6]"},
		{"description bytes", 256, "é", func(input, _ map[string]any, text string) {
			input["description"] = text
		}, "constraints[7]"},
		{"rule name bytes", 128, "é", func(_, rule map[string]any, text string) {
			rule["name"] = text
		}, "rule name must be 1 to 128 bytes"},
		{"label name", 1024, ":", func(_, rule map[string]any, text string) {
			rule["rule-labels"] = []any{map[string]any{"name": text}}
		}, "rule-label name"},
		{"label key", 1024, ":", func(_, rule map[string]any, text string) {
			rule["statement"].(map[string]any)["label-match-statement"].(map[string]any)["key"] = text
		}, "statement scalar or collection input"},
		{"byte search", 200, "é", func(_, rule map[string]any, text string) {
			setByteField(rule, "method", map[string]any{})["search-string"] = text
		}, "statement scalar or collection input"},
		{"regex length", 512, "a", func(_, rule map[string]any, text string) {
			match := setByteField(rule, "method", map[string]any{})
			match["regex-string"] = text
			rule["statement"] = map[string]any{"regex-match-statement": match}
		}, "statement scalar or collection input"},
		{"header name", 40, "_", func(_, rule map[string]any, text string) {
			setByteField(rule, "single-header", map[string]any{"name": text})
		}, "invalid name, scope or handling option"},
		{"query name", 30, "_", func(_, rule map[string]any, text string) {
			setByteField(rule, "single-query-argument", map[string]any{"name": text})
		}, "invalid name, scope or handling option"},
		{"body content bytes", 10240, "é", func(input, _ map[string]any, text string) {
			input["custom-response-bodies"] = map[string]any{
				"body": map[string]any{"content": text, "content-type": "TEXT_PLAIN"},
			}
		}, "invalid key, content or type"},
		{"body key", 128, "-", func(input, _ map[string]any, text string) {
			input["custom-response-bodies"] = map[string]any{
				text: map[string]any{"content": "body", "content-type": "TEXT_PLAIN"},
			}
		}, "invalid key, content or type"},
		{"custom header name", 64, "$", func(_, rule map[string]any, text string) {
			rule["action"] = map[string]any{"count": map[string]any{
				"custom-request-handling": map[string]any{"insert-headers": []any{
					map[string]any{"name": text, "value": "ok"},
				}},
			}}
		}, "invalid custom HTTP header or response"},
		{"custom header value bytes", 255, "a", func(_, rule map[string]any, text string) {
			rule["action"] = map[string]any{"block": map[string]any{
				"custom-response": map[string]any{"response-code": int64(200),
					"response-headers": []any{map[string]any{"name": "X-Name", "value": text}}},
			}}
		}, "invalid custom HTTP header or response"},
		{"IP forwarding header", 255, "-", func(_, rule map[string]any, text string) {
			rule["statement"] = map[string]any{"ip-set-reference-statement": map[string]any{
				"arn": "", "ip-set-forwarded-ip-config": map[string]any{
					"header-name": text, "position": "ANY", "fallback-behavior": "MATCH",
				},
			}}
		}, "forwarded IP configuration"},
		{"included header bytes", 64, "é", func(_, rule map[string]any, text string) {
			setByteField(rule, "headers", map[string]any{
				"match-scope": "ALL", "oversize-handling": "NO_MATCH",
				"match-pattern": map[string]any{"included-headers": []any{text}},
			})
		}, "invalid cookies, headers or JSON body match pattern"},
		{"JSON path bytes", 512, "/", func(_, rule map[string]any, text string) {
			setByteField(rule, "json-body", map[string]any{
				"match-scope": "ALL", "match-pattern": map[string]any{"included-paths": []any{text}},
			})
		}, "invalid cookies, headers or JSON body match pattern"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := labelRule("rule")
			inputs := ruleGroupConstraintInputs(rule)
			maximum := strings.Repeat(tc.unit, tc.max/len(tc.unit))
			tc.apply(inputs, rule, maximum)
			require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
			for _, bad := range []string{"", maximum + tc.unit} {
				tc.apply(inputs, rule, bad)
				errs := testutil.CheckConstraints(t, schema, inputs)
				require.Len(t, errs, 1)
				require.Contains(t, errs[0], tc.diagnostic)
			}
		})
	}
}

func TestRuleGroupNativeMatchPatterns(t *testing.T) {
	schema := readLibrarySchema(t).Resources["rule-group"]
	for _, tc := range []struct {
		name, field string
		pattern     map[string]any
		rejected    bool
	}{
		{"empty cookie list has no selection", "cookies",
			map[string]any{"included-cookies": []any{}}, true},
		{"empty cookie list beside all", "cookies",
			map[string]any{"included-cookies": []any{}, "all": map[string]any{}}, false},
		{"cookie list with empty strings", "cookies",
			map[string]any{"included-cookies": []any{""}}, false},
		{"two cookie selectors", "cookies",
			map[string]any{"included-cookies": []any{"a"}, "all": map[string]any{}}, true},
		{"whitespace-only header", "headers",
			map[string]any{"excluded-headers": []any{" \t\u00a0"}}, true},
		{"header with whitespace and visible text", "headers",
			map[string]any{"excluded-headers": []any{" \tcontent "}}, false},
		{"empty header list", "headers", map[string]any{"included-headers": []any{}}, true},
		{"all headers", "headers", map[string]any{"all": map[string]any{}}, false},
		{"exact header maximum", "headers",
			map[string]any{"excluded-headers": slices.Repeat([]any{"X"}, 199)}, false},
		{"over header maximum", "headers",
			map[string]any{"excluded-headers": slices.Repeat([]any{"X"}, 200)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := labelRule("rule")
			setByteField(rule, tc.field, map[string]any{
				"match-scope": "ALL", "oversize-handling": "CONTINUE", "match-pattern": tc.pattern,
			})
			errs := testutil.CheckConstraints(t, schema, ruleGroupConstraintInputs(rule))
			if tc.rejected {
				require.Len(t, errs, 1)
				require.Contains(t, errs[0], "invalid cookies, headers or JSON body match pattern")
			} else {
				require.Empty(t, errs)
			}
		})
	}
}

func TestRuleGroupNativeCollectionAndNumberBounds(t *testing.T) {
	schema := readLibrarySchema(t).Resources["rule-group"]
	for _, tc := range []struct {
		name           string
		value, invalid any
		apply          func(map[string]any, any)
		diagnostic     string
	}{
		{"ASN minimum", []any{int64(-1)}, []any{}, func(rule map[string]any, value any) {
			rule["statement"] = map[string]any{"asn-match-statement": map[string]any{"asn-list": value}}
		}, "statement scalar or collection input"},
		{"ASN maximum", slices.Repeat([]any{int64(0)}, 100), slices.Repeat([]any{int64(0)}, 101),
			func(rule map[string]any, value any) {
				rule["statement"] = map[string]any{"asn-match-statement": map[string]any{
					"asn-list": value,
				}}
			}, "statement scalar or collection input"},
		{"geo minimum", []any{"ZZ"}, []any{}, func(rule map[string]any, value any) {
			rule["statement"] = map[string]any{"geo-match-statement": map[string]any{
				"country-codes": value,
			}}
		}, "statement scalar or collection input"},
		{"transform minimum", []any{map[string]any{"priority": int64(-1), "type": "NONE"}}, []any{},
			func(rule map[string]any, value any) {
				setByteField(rule, "method", map[string]any{})["text-transformations"] = value
			}, "text-transformations requires entries with a valid type"},
		{"empty inserted headers", []any{map[string]any{"name": "X", "value": "x"}}, []any{},
			func(rule map[string]any, value any) {
				rule["action"] = map[string]any{"allow": map[string]any{
					"custom-request-handling": map[string]any{"insert-headers": value},
				}}
			}, "invalid custom HTTP header or response"},
		{"response code below minimum", int64(200), int64(199), configureRuleGroupResponseCode,
			"invalid custom HTTP header or response"},
		{"response code above maximum", int64(600), int64(601), configureRuleGroupResponseCode,
			"invalid custom HTTP header or response"},
		{"size below minimum", int64(0), int64(-1), configureRuleGroupSize,
			"statement scalar or collection input"},
		{"size above maximum", int64(2147483647), int64(2147483648), configureRuleGroupSize,
			"statement scalar or collection input"},
		{"regex syntax", "a+", "[", func(rule map[string]any, value any) {
			match := setByteField(rule, "method", map[string]any{})
			match["regex-string"] = value
			rule["statement"] = map[string]any{"regex-match-statement": match}
		}, "statement scalar or collection input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := labelRule("rule")
			inputs := ruleGroupConstraintInputs(rule)
			tc.apply(rule, tc.value)
			require.Empty(t, testutil.CheckConstraints(t, schema, inputs))
			tc.apply(rule, tc.invalid)
			errs := testutil.CheckConstraints(t, schema, inputs)
			require.Len(t, errs, 1)
			require.Contains(t, errs[0], tc.diagnostic)
		})
	}
}

func configureRuleGroupResponseCode(rule map[string]any, code any) {
	rule["action"] = map[string]any{"block": map[string]any{
		"custom-response": map[string]any{"response-code": code},
	}}
}

func configureRuleGroupSize(rule map[string]any, size any) {
	match := setByteField(rule, "method", map[string]any{})
	match["comparison-operator"], match["size"] = "GE", size
	rule["statement"] = map[string]any{"size-constraint-statement": match}
}
