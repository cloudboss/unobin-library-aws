package lambdamicrovms

import (
	"reflect"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/lambdamicrovms"
)

func TestLibraryRegistersLambdaMicrovms(t *testing.T) {
	lib := Library()

	resources := []constructRegistration{
		{
			key:        "microvm-image",
			outputType: reflect.TypeFor[*svc.MicrovmImageOutput](),
		},
	}
	for _, resource := range resources {
		t.Run(resource.key, func(t *testing.T) {
			require.Contains(t, lib.Resources, resource.key)
			assert.Equal(t, resource.outputType, lib.Resources[resource.key].OutputType())
		})
	}

	dataSources := []constructRegistration{
		{
			key:        "microvm-image",
			outputType: reflect.TypeFor[*svc.MicrovmImageDataOutput](),
		},
		{
			key:        "microvm-images",
			outputType: reflect.TypeFor[*svc.MicrovmImagesOutput](),
		},
		{
			key:        "microvm-image-version",
			outputType: reflect.TypeFor[*svc.MicrovmImageVersionDataOutput](),
		},
		{
			key:        "microvm-image-versions",
			outputType: reflect.TypeFor[*svc.MicrovmImageVersionsOutput](),
		},
		{
			key:        "microvm-image-build",
			outputType: reflect.TypeFor[*svc.MicrovmImageBuildDataOutput](),
		},
		{
			key:        "microvm-image-builds",
			outputType: reflect.TypeFor[*svc.MicrovmImageBuildsOutput](),
		},
		{
			key:        "managed-microvm-images",
			outputType: reflect.TypeFor[*svc.ManagedMicrovmImagesOutput](),
		},
		{
			key:        "managed-microvm-image-versions",
			outputType: reflect.TypeFor[*svc.ManagedMicrovmImageVersionsOutput](),
		},
		{
			key:        "microvm",
			outputType: reflect.TypeFor[*svc.MicrovmDataOutput](),
		},
		{
			key:        "microvms",
			outputType: reflect.TypeFor[*svc.MicrovmsOutput](),
		},
	}
	for _, dataSource := range dataSources {
		t.Run(dataSource.key, func(t *testing.T) {
			require.Contains(t, lib.DataSources, dataSource.key)
			assert.Equal(t, dataSource.outputType, lib.DataSources[dataSource.key].OutputType())
		})
	}

	actions := []constructRegistration{
		{
			key:        "run-microvm",
			outputType: reflect.TypeFor[*svc.MicrovmDataOutput](),
		},
		{
			key:        "create-microvm-auth-token",
			outputType: reflect.TypeFor[*svc.MicrovmAuthTokenOutput](),
		},
		{
			key:        "create-microvm-shell-auth-token",
			outputType: reflect.TypeFor[*svc.MicrovmShellAuthTokenOutput](),
		},
		{
			key:        "suspend-microvm",
			outputType: reflect.TypeFor[*svc.SuspendMicrovmOutput](),
		},
		{
			key:        "resume-microvm",
			outputType: reflect.TypeFor[*svc.ResumeMicrovmOutput](),
		},
		{
			key:        "terminate-microvm",
			outputType: reflect.TypeFor[*svc.TerminateMicrovmOutput](),
		},
		{
			key:        "update-microvm-image-version-status",
			outputType: reflect.TypeFor[*svc.MicrovmImageVersionDataOutput](),
		},
	}
	for _, action := range actions {
		t.Run(action.key, func(t *testing.T) {
			require.Contains(t, lib.Actions, action.key)
			assert.Equal(t, action.outputType, lib.Actions[action.key].OutputType())
		})
	}
}

type constructRegistration struct {
	key        string
	outputType reflect.Type
}

func TestLambdaMicrovmSchemas(t *testing.T) {
	schema := readLibrarySchema(t)

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"name":                       typecheck.TString(),
			"base-image-arn":             typecheck.TString(),
			"build-role-arn":             typecheck.TString(),
			"code-artifact":              codeArtifactType(),
			"base-image-version":         typecheck.TOptional(typecheck.TString()),
			"additional-os-capabilities": typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"cpu-configurations":         typecheck.TOptional(typecheck.TList(cpuConfigurationType())),
			"description":                typecheck.TOptional(typecheck.TString()),
			"egress-network-connectors":  typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"environment-variables":      typecheck.TOptional(typecheck.TMap(typecheck.TString())),
			"hooks":                      typecheck.TOptional(hooksType()),
			"logging":                    typecheck.TOptional(loggingType()),
			"resources":                  typecheck.TOptional(typecheck.TList(resourcesType())),
			"tags":                       typecheck.TOptional(typecheck.TMap(typecheck.TString())),
			"terminate-on-destroy":       typecheck.TOptional(typecheck.TBoolean()),
		},
		Outputs: microvmImageOutputTypes(),
	}, schema.Resources["microvm-image"])

	assert.Equal(t, []string{"name"}, (&svc.MicrovmImage{}).ReplaceFields())

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"image-identifier": typecheck.TOptional(typecheck.TString()),
			"name":             typecheck.TOptional(typecheck.TString()),
		},
		Outputs: microvmImageDataOutputTypes(),
	}, schema.DataSources["microvm-image"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"name-filter": typecheck.TOptional(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"items": typecheck.TList(microvmImageSummaryType()),
		},
	}, schema.DataSources["microvm-images"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"image-identifier": typecheck.TString(),
			"image-version":    typecheck.TString(),
		},
		Outputs: microvmImageVersionOutputTypes(),
	}, schema.DataSources["microvm-image-version"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"image-identifier": typecheck.TString(),
		},
		Outputs: map[string]typecheck.Type{
			"items": typecheck.TList(microvmImageVersionSummaryType()),
		},
	}, schema.DataSources["microvm-image-versions"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"image-identifier": typecheck.TString(),
			"image-version":    typecheck.TString(),
			"build-id":         typecheck.TString(),
		},
		Outputs: microvmImageBuildOutputTypes(),
	}, schema.DataSources["microvm-image-build"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"image-identifier":   typecheck.TString(),
			"image-version":      typecheck.TString(),
			"architecture":       typecheck.TOptional(typecheck.TString()),
			"chipset":            typecheck.TOptional(typecheck.TString()),
			"chipset-generation": typecheck.TOptional(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"items": typecheck.TList(microvmImageBuildSummaryType()),
		},
	}, schema.DataSources["microvm-image-builds"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{},
		Outputs: map[string]typecheck.Type{
			"items": typecheck.TList(managedMicrovmImageSummaryType()),
		},
	}, schema.DataSources["managed-microvm-images"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"image-identifier": typecheck.TString(),
		},
		Outputs: map[string]typecheck.Type{
			"items": typecheck.TList(managedMicrovmImageVersionType()),
		},
	}, schema.DataSources["managed-microvm-image-versions"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"microvm-identifier": typecheck.TString(),
		},
		Outputs: microvmOutputTypes(),
	}, schema.DataSources["microvm"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"image-identifier": typecheck.TOptional(typecheck.TString()),
			"image-version":    typecheck.TOptional(typecheck.TString()),
		},
		Outputs: map[string]typecheck.Type{
			"items": typecheck.TList(microvmSummaryType()),
		},
	}, schema.DataSources["microvms"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"image-identifier":            typecheck.TString(),
			"image-version":               typecheck.TOptional(typecheck.TString()),
			"execution-role-arn":          typecheck.TOptional(typecheck.TString()),
			"ingress-network-connectors":  typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"egress-network-connectors":   typecheck.TOptional(typecheck.TList(typecheck.TString())),
			"idle-policy":                 typecheck.TOptional(idlePolicyType()),
			"logging":                     typecheck.TOptional(loggingType()),
			"maximum-duration-in-seconds": typecheck.TOptional(typecheck.TInteger()),
			"run-hook-payload-content":    typecheck.TOptional(typecheck.TString()),
			"run-hook-payload-path":       typecheck.TOptional(typecheck.TString()),
		},
		Outputs: microvmOutputTypes(),
	}, schema.Actions["run-microvm"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"microvm-identifier":    typecheck.TString(),
			"expiration-in-minutes": typecheck.TInteger(),
			"allowed-ports":         typecheck.TList(portSpecificationType()),
		},
		Outputs: map[string]typecheck.Type{
			"auth-token": typecheck.TMap(typecheck.TString()),
		},
		SensitiveOutputs: []string{"auth-token"},
	}, schema.Actions["create-microvm-auth-token"])

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"microvm-identifier":    typecheck.TString(),
			"expiration-in-minutes": typecheck.TInteger(),
		},
		Outputs: map[string]typecheck.Type{
			"auth-token": typecheck.TMap(typecheck.TString()),
		},
		SensitiveOutputs: []string{"auth-token"},
	}, schema.Actions["create-microvm-shell-auth-token"])

	for _, key := range []string{
		"suspend-microvm",
		"resume-microvm",
		"terminate-microvm",
	} {
		assertSchemaFieldsEqual(t, &runtime.TypeSchema{
			Inputs: map[string]typecheck.Type{
				"microvm-identifier": typecheck.TString(),
			},
			Outputs: map[string]typecheck.Type{
				"microvm-identifier": typecheck.TString(),
			},
		}, schema.Actions[key], key)
	}

	assertSchemaFieldsEqual(t, &runtime.TypeSchema{
		Inputs: map[string]typecheck.Type{
			"image-identifier": typecheck.TString(),
			"image-version":    typecheck.TString(),
			"status":           typecheck.TString(),
		},
		Outputs: microvmImageVersionOutputTypes(),
	}, schema.Actions["update-microvm-image-version-status"])

	assert.Empty(t, schema.Resources["microvm-image"].SensitiveInputs)
	assert.Empty(t, schema.Resources["microvm-image"].SensitiveOutputs)
	for key, sch := range schema.DataSources {
		if keyHasLambdaMicrovmPrefix(key) {
			assert.Empty(t, sch.SensitiveInputs, key)
			assert.Empty(t, sch.SensitiveOutputs, key)
		}
	}
	for key, sch := range schema.Actions {
		if !keyHasLambdaMicrovmPrefix(key) {
			continue
		}
		assert.Empty(t, sch.SensitiveInputs, key)
		if key == "create-microvm-auth-token" ||
			key == "create-microvm-shell-auth-token" {
			assert.Equal(t, []string{"auth-token"}, sch.SensitiveOutputs, key)
		} else {
			assert.Empty(t, sch.SensitiveOutputs, key)
		}
	}

	assertSchemaConstraints(t, schema)
}

func assertSchemaFieldsEqual(
	t *testing.T,
	want *runtime.TypeSchema,
	got *runtime.TypeSchema,
	msgAndArgs ...any,
) {
	t.Helper()
	require.NotNil(t, got, msgAndArgs...)
	assert.Equal(t, want.Inputs, got.Inputs, msgAndArgs...)
	assert.Equal(t, want.Outputs, got.Outputs, msgAndArgs...)
	assert.Equal(t, want.SensitiveInputs, got.SensitiveInputs, msgAndArgs...)
	assert.Equal(t, want.SensitiveOutputs, got.SensitiveOutputs, msgAndArgs...)
	assert.Empty(t, got.Defaults, msgAndArgs...)
}

func assertSchemaConstraints(t *testing.T, schema *runtime.LibrarySchema) {
	t.Helper()
	image := schema.Resources["microvm-image"]
	assertConstraintMessage(t, image.Constraints,
		"logging must set exactly one of cloud-watch or disabled")
	assertConstraintsContain(t, image.Constraints, lang.ConstraintSpec{
		Kind:    "predicate",
		When:    "(input.logging.disabled != null)",
		Require: "(input.logging.disabled == true)",
		Message: "logging disabled must be true",
	})
	assertConstraintMessage(t, image.Constraints,
		"additional-os-capabilities values must be ALL")
	assertConstraintMessage(t, image.Constraints,
		"cpu-configurations architecture must be ARM_64")
	assertConstraintMessage(t, image.Constraints,
		"resources must have at most one item")
	assertConstraintMessage(t, image.Constraints,
		"hooks port must be between 1 and 65535")
	assertConstraintMessage(t, image.Constraints,
		"microvm hook timeouts must be between 1 and 60")
	assertConstraintMessage(t, image.Constraints,
		"microvm image hook timeouts must be between 1 and 3600")

	assertConstraintsContain(t,
		schema.DataSources["microvm-image"].Constraints,
		lang.ConstraintSpec{
			Kind: "exactly-one-of",
			Fields: []string{
				"input.image-identifier",
				"input.name",
			},
		})
	assertConstraintMessage(t,
		schema.DataSources["microvm-image-builds"].Constraints,
		"architecture must be ARM_64")
	assertConstraintMessage(t,
		schema.DataSources["microvm-image-builds"].Constraints,
		"chipset must be GRAVITON")

	run := schema.Actions["run-microvm"]
	assertConstraintsContain(t, run.Constraints, lang.ConstraintSpec{
		Kind: "at-most-one-of",
		Fields: []string{
			"input.run-hook-payload-content",
			"input.run-hook-payload-path",
		},
	})
	assertConstraintMessage(t, run.Constraints,
		"maximum-duration-in-seconds must be between 1 and 28800")

	auth := schema.Actions["create-microvm-auth-token"]
	assertConstraintMessage(t, auth.Constraints,
		"expiration-in-minutes must be between 1 and 60")
	assertConstraintMessage(t, auth.Constraints, "allowed-ports must not be empty")
	assertConstraintMessage(t, auth.Constraints, "port range start must be no greater than end")

	shellAuth := schema.Actions["create-microvm-shell-auth-token"]
	assertConstraintMessage(t, shellAuth.Constraints,
		"expiration-in-minutes must be between 1 and 60")

	status := schema.Actions["update-microvm-image-version-status"]
	assertConstraintMessage(t, status.Constraints, "status must be ACTIVE or INACTIVE")
}

func assertConstraintMessage(
	t *testing.T,
	constraints []lang.ConstraintSpec,
	message string,
) {
	t.Helper()
	for _, c := range constraints {
		if c.Message == message {
			return
		}
	}
	assert.Failf(t, "missing constraint", "missing constraint message %q in %#v", message, constraints)
}

func keyHasLambdaMicrovmPrefix(key string) bool {
	return len(key) >= len("lambdamicrovms-") && key[:len("lambdamicrovms-")] == "lambdamicrovms-"
}

func codeArtifactType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "uri", Type: typecheck.TString()},
	})
}

func loggingType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "cloud-watch", Type: cloudWatchLoggingType(), Optional: true},
		{Name: "disabled", Type: typecheck.TBoolean(), Optional: true},
	})
}

func cloudWatchLoggingType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "log-group", Type: typecheck.TString(), Optional: true},
		{Name: "log-stream", Type: typecheck.TString(), Optional: true},
	})
}

func cpuConfigurationType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "architecture", Type: typecheck.TString()},
	})
}

func resourcesType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "minimum-memory-in-mib", Type: typecheck.TInteger()},
	})
}

func hooksType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "port", Type: typecheck.TInteger(), Optional: true},
		{Name: "microvm-hooks", Type: microvmHooksType(), Optional: true},
		{Name: "microvm-image-hooks", Type: microvmImageHooksType(), Optional: true},
	})
}

func microvmHooksType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "run", Type: typecheck.TString(), Optional: true},
		{Name: "run-timeout-in-seconds", Type: typecheck.TInteger(), Optional: true},
		{Name: "resume", Type: typecheck.TString(), Optional: true},
		{Name: "resume-timeout-in-seconds", Type: typecheck.TInteger(), Optional: true},
		{Name: "suspend", Type: typecheck.TString(), Optional: true},
		{Name: "suspend-timeout-in-seconds", Type: typecheck.TInteger(), Optional: true},
		{Name: "terminate", Type: typecheck.TString(), Optional: true},
		{Name: "terminate-timeout-in-seconds", Type: typecheck.TInteger(), Optional: true},
	})
}

func microvmImageHooksType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "ready", Type: typecheck.TString(), Optional: true},
		{Name: "ready-timeout-in-seconds", Type: typecheck.TInteger(), Optional: true},
		{Name: "validate", Type: typecheck.TString(), Optional: true},
		{Name: "validate-timeout-in-seconds", Type: typecheck.TInteger(), Optional: true},
	})
}

func idlePolicyType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "auto-resume-enabled", Type: typecheck.TBoolean()},
		{Name: "max-idle-duration-seconds", Type: typecheck.TInteger()},
		{Name: "suspended-duration-seconds", Type: typecheck.TInteger()},
	})
}

func portSpecificationType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "all-ports", Type: typecheck.TBoolean(), Optional: true},
		{Name: "port", Type: typecheck.TInteger(), Optional: true},
		{Name: "range", Type: portRangeType(), Optional: true},
	})
}

func portRangeType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "start-port", Type: typecheck.TInteger()},
		{Name: "end-port", Type: typecheck.TInteger()},
	})
}

func snapshotBuildType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "code-install-size-in-bytes", Type: typecheck.TInteger()},
		{Name: "disk-snapshot-size-in-bytes", Type: typecheck.TInteger()},
		{Name: "memory-snapshot-size-in-bytes", Type: typecheck.TInteger()},
	})
}

func microvmImageOutputTypes() map[string]typecheck.Type {
	return map[string]typecheck.Type{
		"image-arn":                   typecheck.TString(),
		"name":                        typecheck.TString(),
		"state":                       typecheck.TString(),
		"created-at":                  typecheck.TString(),
		"updated-at":                  typecheck.TString(),
		"latest-active-image-version": typecheck.TString(),
		"latest-failed-image-version": typecheck.TString(),
	}
}

func microvmImageDataOutputTypes() map[string]typecheck.Type {
	out := microvmImageOutputTypes()
	out["tags"] = typecheck.TMap(typecheck.TString())
	return out
}

func microvmImageSummaryType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "image-arn", Type: typecheck.TString()},
		{Name: "name", Type: typecheck.TString()},
		{Name: "state", Type: typecheck.TString()},
		{Name: "created-at", Type: typecheck.TString()},
		{Name: "latest-active-image-version", Type: typecheck.TString()},
		{Name: "latest-failed-image-version", Type: typecheck.TString()},
	})
}

func microvmImageVersionOutputTypes() map[string]typecheck.Type {
	return map[string]typecheck.Type{
		"image-arn":                  typecheck.TString(),
		"image-version":              typecheck.TString(),
		"state":                      typecheck.TString(),
		"status":                     typecheck.TString(),
		"base-image-arn":             typecheck.TString(),
		"base-image-version":         typecheck.TString(),
		"build-role-arn":             typecheck.TString(),
		"code-artifact":              codeArtifactType(),
		"additional-os-capabilities": typecheck.TList(typecheck.TString()),
		"cpu-configurations":         typecheck.TList(cpuConfigurationType()),
		"description":                typecheck.TString(),
		"egress-network-connectors":  typecheck.TList(typecheck.TString()),
		"environment-variables":      typecheck.TMap(typecheck.TString()),
		"hooks":                      typecheck.TOptional(hooksType()),
		"logging":                    typecheck.TOptional(loggingType()),
		"resources":                  typecheck.TList(resourcesType()),
		"state-reason":               typecheck.TString(),
		"tags":                       typecheck.TMap(typecheck.TString()),
		"created-at":                 typecheck.TString(),
		"updated-at":                 typecheck.TString(),
	}
}

func microvmImageVersionSummaryType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "image-arn", Type: typecheck.TString()},
		{Name: "image-version", Type: typecheck.TString()},
		{Name: "state", Type: typecheck.TString()},
		{Name: "status", Type: typecheck.TString()},
		{Name: "base-image-arn", Type: typecheck.TString()},
		{Name: "base-image-version", Type: typecheck.TString()},
		{Name: "build-role-arn", Type: typecheck.TString()},
		{Name: "code-artifact", Type: codeArtifactType()},
		{Name: "additional-os-capabilities", Type: typecheck.TList(typecheck.TString())},
		{Name: "cpu-configurations", Type: typecheck.TList(cpuConfigurationType())},
		{Name: "description", Type: typecheck.TString()},
		{Name: "egress-network-connectors", Type: typecheck.TList(typecheck.TString())},
		{Name: "environment-variables", Type: typecheck.TMap(typecheck.TString())},
		{Name: "hooks", Type: hooksType(), Optional: true},
		{Name: "logging", Type: loggingType(), Optional: true},
		{Name: "resources", Type: typecheck.TList(resourcesType())},
		{Name: "state-reason", Type: typecheck.TString()},
		{Name: "tags", Type: typecheck.TMap(typecheck.TString())},
		{Name: "created-at", Type: typecheck.TString()},
		{Name: "updated-at", Type: typecheck.TString()},
	})
}

func microvmImageBuildOutputTypes() map[string]typecheck.Type {
	return map[string]typecheck.Type{
		"image-arn":          typecheck.TString(),
		"image-version":      typecheck.TString(),
		"build-id":           typecheck.TString(),
		"build-state":        typecheck.TString(),
		"architecture":       typecheck.TString(),
		"chipset":            typecheck.TString(),
		"chipset-generation": typecheck.TString(),
		"snapshot-build":     typecheck.TOptional(snapshotBuildType()),
		"state-reason":       typecheck.TString(),
		"created-at":         typecheck.TString(),
	}
}

func microvmImageBuildSummaryType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "image-arn", Type: typecheck.TString()},
		{Name: "image-version", Type: typecheck.TString()},
		{Name: "build-id", Type: typecheck.TString()},
		{Name: "build-state", Type: typecheck.TString()},
		{Name: "architecture", Type: typecheck.TString()},
		{Name: "chipset", Type: typecheck.TString()},
		{Name: "chipset-generation", Type: typecheck.TString()},
		{Name: "snapshot-build", Type: snapshotBuildType(), Optional: true},
		{Name: "state-reason", Type: typecheck.TString()},
		{Name: "created-at", Type: typecheck.TString()},
	})
}

func managedMicrovmImageSummaryType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "image-arn", Type: typecheck.TString()},
		{Name: "created-at", Type: typecheck.TString()},
		{Name: "updated-at", Type: typecheck.TString()},
	})
}

func managedMicrovmImageVersionType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "image-arn", Type: typecheck.TString()},
		{Name: "image-version", Type: typecheck.TString()},
		{Name: "created-at", Type: typecheck.TString()},
		{Name: "updated-at", Type: typecheck.TString()},
	})
}

func microvmOutputTypes() map[string]typecheck.Type {
	return map[string]typecheck.Type{
		"microvm-id":                  typecheck.TString(),
		"endpoint":                    typecheck.TString(),
		"image-arn":                   typecheck.TString(),
		"image-version":               typecheck.TString(),
		"state":                       typecheck.TString(),
		"started-at":                  typecheck.TString(),
		"terminated-at":               typecheck.TString(),
		"maximum-duration-in-seconds": typecheck.TInteger(),
		"execution-role-arn":          typecheck.TString(),
		"ingress-network-connectors":  typecheck.TList(typecheck.TString()),
		"egress-network-connectors":   typecheck.TList(typecheck.TString()),
		"idle-policy":                 typecheck.TOptional(idlePolicyType()),
		"state-reason":                typecheck.TString(),
	}
}

func microvmSummaryType() typecheck.Type {
	return typecheck.TObject([]typecheck.ObjectField{
		{Name: "microvm-id", Type: typecheck.TString()},
		{Name: "image-arn", Type: typecheck.TString()},
		{Name: "image-version", Type: typecheck.TString()},
		{Name: "state", Type: typecheck.TString()},
		{Name: "started-at", Type: typecheck.TString()},
	})
}
