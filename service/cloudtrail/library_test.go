package cloudtrail

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/cloudtrail"
)

const unobinModulePath = "github.com/cloudboss/unobin"

func TestLibraryRegistersTrail(t *testing.T) {
	lib := Library()
	require.Equal(t, "aws-cloudtrail", lib.Name)
	require.NotNil(t, lib.Configuration)
	require.Equal(t, reflect.TypeFor[*awscfg.Configuration](), lib.Configuration.ValueType())
	require.Len(t, lib.Resources, 1)
	require.Contains(t, lib.Resources, "trail")
	assert.Equal(t, reflect.TypeFor[*svc.TrailResourceOutput](),
		lib.Resources["trail"].OutputType())
	assert.Empty(t, lib.DataSources)
	assert.Empty(t, lib.Actions)
}

func TestLibraryConfigurationView(t *testing.T) {
	view, err := cfg.View(Library().Configuration)
	require.NoError(t, err)
	assert.Equal(t, "github.com/cloudboss/unobin/pkg/awscfg.Configuration", view.Identity)
	assert.NotEmpty(t, view.SchemaDigest)
}

func TestTrailSchema(t *testing.T) {
	schema := readCloudTrailSchema(t)
	require.Contains(t, schema.Resources, "trail")
	trail := schema.Resources["trail"]

	assert.Equal(t, map[string]typecheck.Type{
		"advanced-event-selectors": typecheck.TOptional(typecheck.TList(typecheck.TObject(
			[]typecheck.ObjectField{
				{Name: "name", Type: typecheck.TString(), Optional: true},
				{Name: "field-selectors", Type: typecheck.TList(typecheck.TObject(
					[]typecheck.ObjectField{
						{Name: "field", Type: typecheck.TString()},
						{Name: "equals", Type: typecheck.TList(typecheck.TString()), Optional: true},
						{Name: "not-equals", Type: typecheck.TList(typecheck.TString()), Optional: true},
						{Name: "starts-with", Type: typecheck.TList(typecheck.TString()), Optional: true},
						{Name: "not-starts-with", Type: typecheck.TList(typecheck.TString()), Optional: true},
						{Name: "ends-with", Type: typecheck.TList(typecheck.TString()), Optional: true},
						{Name: "not-ends-with", Type: typecheck.TList(typecheck.TString()), Optional: true},
					}))},
			}))),
		"aggregation-configurations": typecheck.TOptional(typecheck.TList(typecheck.TObject(
			[]typecheck.ObjectField{
				{Name: "event-category", Type: typecheck.TString()},
				{Name: "templates", Type: typecheck.TList(typecheck.TString())},
			}))),
		"cloud-watch-logs-log-group-arn": typecheck.TOptional(typecheck.TString()),
		"cloud-watch-logs-role-arn":      typecheck.TOptional(typecheck.TString()),
		"enable-log-file-validation":     typecheck.TOptional(typecheck.TBoolean()),
		"enable-logging":                 typecheck.TOptional(typecheck.TBoolean()),
		"event-selectors": typecheck.TOptional(typecheck.TList(typecheck.TObject(
			[]typecheck.ObjectField{
				{Name: "data-resources", Type: typecheck.TList(
					typecheck.TObject([]typecheck.ObjectField{
						{Name: "type", Type: typecheck.TString()},
						{Name: "values", Type: typecheck.TList(typecheck.TString()), Optional: true},
					})), Optional: true},
				{Name: "exclude-management-event-sources",
					Type: typecheck.TList(typecheck.TString()), Optional: true},
				{Name: "include-management-events", Type: typecheck.TBoolean(), Optional: true},
				{Name: "read-write-type", Type: typecheck.TString(), Optional: true},
			}))),
		"include-global-service-events": typecheck.TOptional(typecheck.TBoolean()),
		"insight-selectors": typecheck.TOptional(typecheck.TList(typecheck.TObject(
			[]typecheck.ObjectField{
				{Name: "insight-type", Type: typecheck.TString()},
				{Name: "event-categories", Type: typecheck.TList(typecheck.TString()), Optional: true},
			}))),
		"is-multi-region-trail": typecheck.TOptional(typecheck.TBoolean()),
		"is-organization-trail": typecheck.TOptional(typecheck.TBoolean()),
		"kms-key-id":            typecheck.TOptional(typecheck.TString()),
		"name":                  typecheck.TString(),
		"s3-bucket-name":        typecheck.TString(),
		"s3-key-prefix":         typecheck.TOptional(typecheck.TString()),
		"sns-topic-name":        typecheck.TOptional(typecheck.TString()),
		"tags":                  typecheck.TOptional(typecheck.TMap(typecheck.TString())),
	}, trail.Inputs)
	assert.Equal(t, map[string]typecheck.Type{
		"arn":           typecheck.TString(),
		"home-region":   typecheck.TString(),
		"sns-topic-arn": typecheck.TString(),
	}, trail.Outputs)
	assert.Empty(t, trail.SensitiveInputs)
	assert.Empty(t, trail.SensitiveOutputs)
	assert.Equal(t, []lang.DefaultSpec{
		{Field: "input.enable-logging", Value: "true"},
		{Field: "input.include-global-service-events", Value: "true"},
	}, trail.Defaults)

	messages := make(map[string]lang.ConstraintSpec, len(trail.Constraints))
	for _, c := range trail.Constraints {
		messages[c.Message] = c
	}
	for _, message := range []string{
		"event-selectors holds at most 5 selectors",
		"data resource type must be supported by CloudTrail",
		"data resource values holds at most 250 entries",
		"an advanced event selector requires field-selectors",
		"advanced selector field is invalid",
		"insight-type must be a CloudTrail Insights type",
		"event-categories holds one or two entries",
		"aggregation-configurations holds at most 1 entry",
		"aggregation event-category must be Data",
		"aggregation templates holds 1 to 50 entries",
		"aggregation template is invalid",
	} {
		assert.Contains(t, messages, message)
	}
	for _, field := range []string{
		"errorCode", "eventCategory", "eventName", "eventSource", "eventType",
		"readOnly", "resources.ARN", "resources.type", "sessionCredentialFromConsole",
		"userIdentity.arn", "vpcEndpointId",
	} {
		assert.Contains(t, messages["advanced selector field is invalid"].Require,
			"'"+field+"'")
	}
	for _, insightType := range []string{"ApiCallRateInsight", "ApiErrorRateInsight"} {
		assert.Contains(t, messages["insight-type must be a CloudTrail Insights type"].Require,
			"'"+insightType+"'")
	}
	for _, category := range []string{"Management", "Data"} {
		assert.Contains(t, messages["event category must be Management or Data"].Require,
			"'"+category+"'")
	}
	for _, template := range []string{"API_ACTIVITY", "RESOURCE_ACCESS", "USER_ACTIONS"} {
		assert.Contains(t, messages["aggregation template is invalid"].Require,
			"'"+template+"'")
	}

	conflicts := false
	for _, c := range trail.Constraints {
		if c.Kind == "at-most-one-of" {
			assert.Equal(t, []string{
				"input.event-selectors",
				"input.advanced-event-selectors",
			}, c.Fields)
			conflicts = true
		}
	}
	assert.True(t, conflicts)
}

func TestTrailSchemaUniqueCollectionsAreValidated(t *testing.T) {
	r := &svc.TrailResource{
		Name:         "unobin-trail",
		S3BucketName: "logs-bucket",
		InsightSelectors: &[]svc.TrailInsightSelector{{
			InsightType:     "ApiCallRateInsight",
			EventCategories: &[]string{"Data", "Data"},
		}},
	}
	err := r.ValidateInputs(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unique")
}

func readCloudTrailSchema(t *testing.T) *runtime.LibrarySchema {
	t.Helper()
	moduleRoot := goModuleRoot(t, "")
	unobinRoot := goModuleRoot(t, unobinModulePath)
	schema, warnings, err := goschema.Read(".", moduleRoot, unobinRoot)
	require.NoError(t, err)
	require.Empty(t, warnings)
	return schema
}

func goModuleRoot(t *testing.T, module string) goschema.ModuleRoot {
	t.Helper()
	args := []string{"list", "-m", "-f", "{{.Path}}\n{{.Dir}}"}
	if module != "" {
		args = append(args, module)
	}
	out, err := exec.Command("go", args...).Output()
	require.NoError(t, err)
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, parts, 2)
	return goschema.ModuleRoot{Path: parts[0], Dir: parts[1]}
}
