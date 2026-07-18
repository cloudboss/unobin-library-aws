package wafv2_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/wafv2"
)

func TestWebACLSchemaTopLevelContract(t *testing.T) {
	schema := readLibrarySchema(t)
	require.Equal(t, []string{"web-acl", "web-acl-association"},
		typeSchemaKeys(schema.Resources))
	require.Empty(t, schema.DataSources)
	require.Empty(t, schema.Actions)
	got := schema.Resources["web-acl"]
	require.NotNil(t, got)

	assert.Equal(t, []string{
		"application-config",
		"association-config",
		"captcha-config",
		"challenge-config",
		"custom-response-bodies",
		"data-protection-config",
		"default-action",
		"description",
		"name",
		"on-source-ddos-protection-config",
		"rules",
		"scope",
		"tags",
		"token-domains",
		"visibility-config",
	}, typeKeys(got.Inputs))
	assert.Equal(t, []string{
		"application-integration-url",
		"arn",
		"capacity",
		"id",
		"label-namespace",
		"lock-token",
	}, typeKeys(got.Outputs))
	assert.Empty(t, got.SensitiveInputs)
	assert.Empty(t, got.SensitiveOutputs)
	assert.Equal(t, []string{"name", "scope", "application-config"},
		(&svc.WebACLResource{}).ReplaceFields())
}

func TestWebACLAssociationSchemaContract(t *testing.T) {
	resource := readLibrarySchema(t).Resources["web-acl-association"]
	require.NotNil(t, resource)

	assert.Equal(t, []string{"resource-arn", "web-acl-arn"}, typeKeys(resource.Inputs))
	assert.Equal(t, []string{"resource-arn", "web-acl-arn"}, typeKeys(resource.Outputs))
	for name, value := range resource.Inputs {
		assert.Equal(t, typecheck.String, value.Kind, "input.%s", name)
	}
	for name, value := range resource.Outputs {
		assert.Equal(t, typecheck.String, value.Kind, "output.%s", name)
	}
	assert.Empty(t, resource.SensitiveInputs)
	assert.Empty(t, resource.SensitiveOutputs)
	assert.Equal(t, []string{"resource-arn", "web-acl-arn"},
		(&svc.WebACLAssociationResource{}).ReplaceFields())
	assert.Equal(t, 1, (&svc.WebACLAssociationResource{}).SchemaVersion())
}

func TestWebACLSchemaContainsNoUnknownOrOpaqueTypes(t *testing.T) {
	resource := readLibrarySchema(t).Resources["web-acl"]
	require.NotNil(t, resource)
	for name, value := range resource.Inputs {
		assertConcreteSchemaType(t, value, "input."+name)
	}
	for name, value := range resource.Outputs {
		assertConcreteSchemaType(t, value, "output."+name)
	}
}

func TestWebACLStatementSchemaHasFiniteLogicalLevels(t *testing.T) {
	resource := readLibrarySchema(t).Resources["web-acl"]
	require.NotNil(t, resource)
	rules := requireOptionalSchemaType(t, resource.Inputs["rules"], "input.rules")
	rule := requireListSchemaType(t, rules, "input.rules")
	level3 := requireObjectField(t, rule, "statement", "input.rules[].statement")
	level2 := requireNextLogicalLevel(t, level3, "level3")
	level1 := requireNextLogicalLevel(t, level2, "level2")
	level0 := requireNextLogicalLevel(t, level1, "level1")

	leafFields := []string{
		"asn-match-statement",
		"byte-match-statement",
		"geo-match-statement",
		"ip-set-reference-statement",
		"label-match-statement",
		"regex-match-statement",
		"regex-pattern-set-reference-statement",
		"size-constraint-statement",
		"sqli-match-statement",
		"xss-match-statement",
	}
	assert.Equal(t, leafFields, objectFieldNames(t, level0, "level0"))
	for _, field := range []string{"and-statement", "not-statement", "or-statement"} {
		assert.False(t, hasObjectField(level0, field), "level0 contains %s", field)
	}

	rootOnly := []string{
		"managed-rule-group-statement",
		"rate-based-statement",
		"rule-group-reference-statement",
	}
	for _, field := range rootOnly {
		assert.True(t, hasObjectField(level3, field), "level3 is missing %s", field)
		for name, lower := range map[string]typecheck.Type{
			"level2": level2,
			"level1": level1,
			"level0": level0,
		} {
			assert.False(t, hasObjectField(lower, field), "%s contains %s", name, field)
		}
	}
}

func TestWebACLRuleActionSchemaExcludesMonetize(t *testing.T) {
	resource := readLibrarySchema(t).Resources["web-acl"]
	rules := requireOptionalSchemaType(t, resource.Inputs["rules"], "input.rules")
	rule := requireListSchemaType(t, rules, "input.rules")
	action := requireObjectField(t, rule, "action", "input.rules[].action")
	assert.Equal(t, []string{
		"allow",
		"block",
		"captcha",
		"challenge",
		"count",
	}, objectFieldNames(t, action, "input.rules[].action"))
	assert.False(t, hasObjectField(action, "monetize"))
}

func requireNextLogicalLevel(t *testing.T, current typecheck.Type, path string) typecheck.Type {
	t.Helper()
	andStatement := requireObjectField(t, current, "and-statement", path)
	andChild := requireListSchemaType(
		t,
		requireObjectField(t, andStatement, "statements", path+".and-statement"),
		path+".and-statement.statements",
	)
	notStatement := requireObjectField(t, current, "not-statement", path)
	notChild := requireObjectField(t, notStatement, "statement", path+".not-statement")
	orStatement := requireObjectField(t, current, "or-statement", path)
	orChild := requireListSchemaType(
		t,
		requireObjectField(t, orStatement, "statements", path+".or-statement"),
		path+".or-statement.statements",
	)
	assert.Equal(t, andChild, notChild)
	assert.Equal(t, andChild, orChild)
	return andChild
}

func requireOptionalSchemaType(
	t *testing.T,
	value typecheck.Type,
	path string,
) typecheck.Type {
	t.Helper()
	require.Equal(t, typecheck.Optional, value.Kind, "%s is not optional", path)
	require.NotNil(t, value.Elem, "%s has no optional element", path)
	return *value.Elem
}

func requireListSchemaType(t *testing.T, value typecheck.Type, path string) typecheck.Type {
	t.Helper()
	require.Equal(t, typecheck.List, value.Kind, "%s is not a list", path)
	require.NotNil(t, value.Elem, "%s has no list element", path)
	return *value.Elem
}

func requireObjectField(
	t *testing.T,
	object typecheck.Type,
	name string,
	path string,
) typecheck.Type {
	t.Helper()
	require.Equal(t, typecheck.Object, object.Kind, "%s is not an object", path)
	for _, field := range object.Fields {
		if field.Name == name {
			return field.Type
		}
	}
	require.FailNow(t, fmt.Sprintf("%s has no %s field", path, name))
	return typecheck.Type{}
}

func hasObjectField(object typecheck.Type, name string) bool {
	for _, field := range object.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func objectFieldNames(t *testing.T, object typecheck.Type, path string) []string {
	t.Helper()
	require.Equal(t, typecheck.Object, object.Kind, "%s is not an object", path)
	fields := make([]string, 0, len(object.Fields))
	for _, field := range object.Fields {
		fields = append(fields, field.Name)
	}
	slices.Sort(fields)
	return fields
}

func assertConcreteSchemaType(t *testing.T, value typecheck.Type, path string) {
	t.Helper()
	assert.NotEqual(t, typecheck.Unknown, value.Kind, "%s is unknown", path)
	assert.NotEqual(t, typecheck.Opaque, value.Kind, "%s is opaque", path)
	if value.Elem != nil {
		assertConcreteSchemaType(t, *value.Elem, path+"[]")
	}
	for index, element := range value.Elems {
		assertConcreteSchemaType(t, element, fmt.Sprintf("%s[%d]", path, index))
	}
	for _, field := range value.Fields {
		assertConcreteSchemaType(t, field.Type, path+"."+field.Name)
	}
}

func typeKeys(values map[string]typecheck.Type) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func typeSchemaKeys(values map[string]*runtime.TypeSchema) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
