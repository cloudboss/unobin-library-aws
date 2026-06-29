package lambdamicrovms

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedMicrovmImagesPagesAllResults(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/managed-microvm-images"
	fake.on(route, func(n int) (int, string) {
		if n == 1 {
			return http.StatusOK, `{
				"items":[{
					"imageArn":"managed-1",
					"createdAt":1782691200,
					"updatedAt":1782691260
				}],
				"nextToken":"token-2"
			}`
		}
		return http.StatusOK, `{
			"items":[{
				"imageArn":"managed-2",
				"createdAt":1782691320,
				"updatedAt":1782691380
			}]
		}`
	})

	out, err := (&ManagedMicrovmImages{}).Read(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, []ManagedMicrovmImageSummary{
		{ImageArn: "managed-1", CreatedAt: "2026-06-29T00:00:00Z", UpdatedAt: "2026-06-29T00:01:00Z"},
		{ImageArn: "managed-2", CreatedAt: "2026-06-29T00:02:00Z", UpdatedAt: "2026-06-29T00:03:00Z"},
	}, out.Items)
	queries := fake.queries(route)
	require.Len(t, queries, 2)
	assert.Equal(t, "token-2", queries[1].Get("nextToken"))
}

func TestManagedMicrovmImageVersionsPagesAllResults(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/managed-microvm-images/managed-1/versions"
	fake.on(route, func(n int) (int, string) {
		if n == 1 {
			return http.StatusOK, `{
				"items":[{
					"imageArn":"managed-1",
					"imageVersion":"1",
					"createdAt":1782691200,
					"updatedAt":1782691260
				}],
				"nextToken":"token-2"
			}`
		}
		return http.StatusOK, `{
			"items":[{
				"imageArn":"managed-1",
				"imageVersion":"2",
				"createdAt":1782691320,
				"updatedAt":1782691380
			}]
		}`
	})

	out, err := (&ManagedMicrovmImageVersions{ImageIdentifier: "managed-1"}).Read(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, []ManagedMicrovmImageVersion{
		{
			ImageArn:     "managed-1",
			ImageVersion: "1",
			CreatedAt:    "2026-06-29T00:00:00Z",
			UpdatedAt:    "2026-06-29T00:01:00Z",
		},
		{
			ImageArn:     "managed-1",
			ImageVersion: "2",
			CreatedAt:    "2026-06-29T00:02:00Z",
			UpdatedAt:    "2026-06-29T00:03:00Z",
		},
	}, out.Items)
	queries := fake.queries(route)
	require.Len(t, queries, 2)
	assert.Equal(t, "token-2", queries[1].Get("nextToken"))
}

func TestMicrovmImagesPagesWithNameFilter(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvm-images"
	fake.on(route, func(n int) (int, string) {
		if n == 1 {
			return http.StatusOK, `{
				"items":[{
					"imageArn":"image-1",
					"name":"demo-a",
					"state":"CREATED",
					"createdAt":1782691200,
					"latestActiveImageVersion":"1"
				}],
				"nextToken":"token-2"
			}`
		}
		return http.StatusOK, `{
			"items":[{
				"imageArn":"image-2",
				"name":"demo-b",
				"state":"UPDATED",
				"createdAt":1782691260,
				"latestFailedImageVersion":"2"
			}]
		}`
	})

	out, err := (&MicrovmImages{NameFilter: aws.String("demo")}).Read(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, []MicrovmImageSummary{
		{
			ImageArn:                 "image-1",
			Name:                     "demo-a",
			State:                    "CREATED",
			CreatedAt:                "2026-06-29T00:00:00Z",
			LatestActiveImageVersion: "1",
		},
		{
			ImageArn:                 "image-2",
			Name:                     "demo-b",
			State:                    "UPDATED",
			CreatedAt:                "2026-06-29T00:01:00Z",
			LatestFailedImageVersion: "2",
		},
	}, out.Items)
	queries := fake.queries(route)
	require.Len(t, queries, 2)
	assert.Equal(t, "demo", queries[0].Get("nameFilter"))
	assert.Equal(t, "token-2", queries[1].Get("nextToken"))
}

func TestMicrovmImageVersionsPagesAllResults(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvm-images/image-1/versions"
	fake.on(route, func(n int) (int, string) {
		if n == 1 {
			return http.StatusOK, `{
				"items":[{
					"imageArn":"image-1",
					"imageVersion":"1",
					"state":"SUCCESSFUL",
					"status":"ACTIVE",
					"baseImageArn":"base-1",
					"baseImageVersion":"base-version-1",
					"buildRoleArn":"role-1",
					"codeArtifact":{"uri":"s3://bucket/app-1.tar"},
					"createdAt":1782691200,
					"updatedAt":1782691260
				}],
				"nextToken":"token-2"
			}`
		}
		return http.StatusOK, `{
			"items":[{
				"imageArn":"image-1",
				"imageVersion":"2",
				"state":"FAILED",
				"status":"INACTIVE",
				"baseImageArn":"base-2",
				"buildRoleArn":"role-2",
				"codeArtifact":{"uri":"s3://bucket/app-2.tar"},
				"stateReason":"bad image",
				"createdAt":1782691320,
				"updatedAt":1782691380
			}]
		}`
	})

	out, err := (&MicrovmImageVersions{ImageIdentifier: "image-1"}).Read(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, []MicrovmImageVersionSummary{
		{
			ImageArn:         "image-1",
			ImageVersion:     "1",
			State:            "SUCCESSFUL",
			Status:           "ACTIVE",
			BaseImageArn:     "base-1",
			BaseImageVersion: "base-version-1",
			BuildRoleArn:     "role-1",
			CodeArtifact:     CodeArtifact{Uri: "s3://bucket/app-1.tar"},
			CreatedAt:        "2026-06-29T00:00:00Z",
			UpdatedAt:        "2026-06-29T00:01:00Z",
		},
		{
			ImageArn:     "image-1",
			ImageVersion: "2",
			State:        "FAILED",
			Status:       "INACTIVE",
			BaseImageArn: "base-2",
			BuildRoleArn: "role-2",
			CodeArtifact: CodeArtifact{Uri: "s3://bucket/app-2.tar"},
			StateReason:  "bad image",
			CreatedAt:    "2026-06-29T00:02:00Z",
			UpdatedAt:    "2026-06-29T00:03:00Z",
		},
	}, out.Items)
}

func TestMicrovmImageBuildsPagesWithFilters(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvm-images/image-1/versions/1/builds"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, `{
			"items":[{
				"imageArn":"image-1",
				"imageVersion":"1",
				"buildId":"build-1",
				"buildState":"SUCCESSFUL",
				"architecture":"ARM_64",
				"chipset":"GRAVITON",
				"chipsetGeneration":"g1",
				"createdAt":1782691200
			}]
		}`
	})

	out, err := (&MicrovmImageBuilds{
		ImageIdentifier:   "image-1",
		ImageVersion:      "1",
		Architecture:      aws.String("ARM_64"),
		Chipset:           aws.String("GRAVITON"),
		ChipsetGeneration: aws.String("g1"),
	}).Read(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, []MicrovmImageBuildSummary{
		{
			ImageArn:          "image-1",
			ImageVersion:      "1",
			BuildId:           "build-1",
			BuildState:        "SUCCESSFUL",
			Architecture:      "ARM_64",
			Chipset:           "GRAVITON",
			ChipsetGeneration: "g1",
			CreatedAt:         "2026-06-29T00:00:00Z",
		},
	}, out.Items)
	queries := fake.queries(route)
	require.Len(t, queries, 1)
	assert.Equal(t, "ARM_64", queries[0].Get("architecture"))
	assert.Equal(t, "GRAVITON", queries[0].Get("chipset"))
	assert.Equal(t, "g1", queries[0].Get("chipsetGeneration"))
}

func TestMicrovmsPagesWithFilters(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvms"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, `{
			"items":[{
				"microvmId":"microvm-1",
				"imageArn":"image-1",
				"imageVersion":"1",
				"state":"RUNNING",
				"startedAt":1782691200
			}]
		}`
	})

	out, err := (&Microvms{
		ImageIdentifier: aws.String("image-1"),
		ImageVersion:    aws.String("1"),
	}).Read(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, []MicrovmSummary{
		{
			MicrovmId:    "microvm-1",
			ImageArn:     "image-1",
			ImageVersion: "1",
			State:        "RUNNING",
			StartedAt:    "2026-06-29T00:00:00Z",
		},
	}, out.Items)
	queries := fake.queries(route)
	require.Len(t, queries, 1)
	assert.Equal(t, "image-1", queries[0].Get("imageIdentifier"))
	assert.Equal(t, "1", queries[0].Get("imageVersion"))
}

func TestMicrovmImageDataReadsByIdentifier(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvm-images/image-1"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, microvmImageDataResponse("image-1", "demo")
	})

	out, err := (&MicrovmImageData{ImageIdentifier: aws.String("image-1")}).Read(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, microvmImageDataOutput("image-1", "demo"), out)
}

func TestMicrovmImageDataFindsByExactName(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	listRoute := "GET /2025-09-09/microvm-images"
	getRoute := "GET /2025-09-09/microvm-images/image-exact"
	fake.on(listRoute, func(n int) (int, string) {
		return http.StatusOK, `{
			"items":[
				{"imageArn":"image-partial","name":"demo-old","state":"CREATED"},
				{"imageArn":"image-exact","name":"demo","state":"CREATED"}
			]
		}`
	})
	fake.on(getRoute, func(n int) (int, string) {
		return http.StatusOK, microvmImageDataResponse("image-exact", "demo")
	})

	out, err := (&MicrovmImageData{Name: aws.String("demo")}).Read(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, microvmImageDataOutput("image-exact", "demo"), out)
	queries := fake.queries(listRoute)
	require.Len(t, queries, 1)
	assert.Equal(t, "demo", queries[0].Get("nameFilter"))
}

func TestMicrovmImageDataNameNotFoundErrors(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvm-images"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, `{"items":[{"imageArn":"image-1","name":"demo-old"}]}`
	})

	_, err := (&MicrovmImageData{Name: aws.String("demo")}).Read(
		context.Background(), fake.configuration())
	require.ErrorContains(t, err, "demo")
}

func TestMicrovmImageVersionDataReads(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvm-images/image-1/versions/1"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, microvmImageVersionResponse()
	})

	out, err := (&MicrovmImageVersionData{
		ImageIdentifier: "image-1",
		ImageVersion:    "1",
	}).Read(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, microvmImageVersionOutput(), out)
}

func TestMicrovmImageBuildDataReads(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvm-images/image-1/versions/1/builds/build-1"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, `{
			"imageArn":"image-1",
			"imageVersion":"1",
			"buildId":"build-1",
			"buildState":"SUCCESSFUL",
			"architecture":"ARM_64",
			"chipset":"GRAVITON",
			"chipsetGeneration":"g1",
			"snapshotBuild":{
				"codeInstallSizeInBytes":10,
				"diskSnapshotSizeInBytes":20,
				"memorySnapshotSizeInBytes":30
			},
			"stateReason":"ready",
			"createdAt":1782691200
		}`
	})

	out, err := (&MicrovmImageBuildData{
		ImageIdentifier: "image-1",
		ImageVersion:    "1",
		BuildId:         "build-1",
	}).Read(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &MicrovmImageBuildDataOutput{
		ImageArn:          "image-1",
		ImageVersion:      "1",
		BuildId:           "build-1",
		BuildState:        "SUCCESSFUL",
		Architecture:      "ARM_64",
		Chipset:           "GRAVITON",
		ChipsetGeneration: "g1",
		SnapshotBuild: &SnapshotBuild{
			CodeInstallSizeInBytes:    10,
			DiskSnapshotSizeInBytes:   20,
			MemorySnapshotSizeInBytes: 30,
		},
		StateReason: "ready",
		CreatedAt:   "2026-06-29T00:00:00Z",
	}, out)
}

func TestMicrovmDataReads(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvms/microvm-1"
	fake.on(route, func(n int) (int, string) {
		return http.StatusOK, microvmResponse()
	})

	out, err := (&MicrovmData{MicrovmIdentifier: "microvm-1"}).Read(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, microvmOutput(), out)
}

func TestDataSourceNotFoundReturnsDescriptiveError(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	route := "GET /2025-09-09/microvms/missing"
	fake.on(route, func(n int) (int, string) {
		return http.StatusNotFound, `{"__type":"ResourceNotFoundException","message":"not found"}`
	})

	_, err := (&MicrovmData{MicrovmIdentifier: "missing"}).Read(
		context.Background(), fake.configuration())
	require.Error(t, err)
	assert.False(t, errors.Is(err, runtime.ErrNotFound))
	assert.ErrorContains(t, err, "missing")
}

func microvmImageDataResponse(imageArn, name string) string {
	return `{
		"imageArn":"` + imageArn + `",
		"name":"` + name + `",
		"state":"CREATED",
		"createdAt":1782691200,
		"updatedAt":1782691260,
		"latestActiveImageVersion":"1",
		"latestFailedImageVersion":"2",
		"tags":{"env":"test"}
	}`
}

func microvmImageDataOutput(imageArn, name string) *MicrovmImageDataOutput {
	return &MicrovmImageDataOutput{
		ImageArn:                 imageArn,
		Name:                     name,
		State:                    "CREATED",
		CreatedAt:                "2026-06-29T00:00:00Z",
		UpdatedAt:                "2026-06-29T00:01:00Z",
		LatestActiveImageVersion: "1",
		LatestFailedImageVersion: "2",
		Tags:                     map[string]string{"env": "test"},
	}
}

func microvmImageVersionResponse() string {
	return `{
		"imageArn":"image-1",
		"imageVersion":"1",
		"state":"SUCCESSFUL",
		"status":"ACTIVE",
		"baseImageArn":"base-1",
		"baseImageVersion":"base-version-1",
		"buildRoleArn":"role-1",
		"codeArtifact":{"uri":"s3://bucket/app.tar"},
		"additionalOsCapabilities":["ALL"],
		"cpuConfigurations":[{"architecture":"ARM_64"}],
		"description":"demo version",
		"egressNetworkConnectors":["egress-1"],
		"environmentVariables":{"ENV":"test"},
		"hooks":{"port":8080},
		"logging":{"disabled":{}},
		"resources":[{"minimumMemoryInMiB":512}],
		"stateReason":"ready",
		"tags":{"env":"test"},
		"createdAt":1782691200,
		"updatedAt":1782691260
	}`
}

func microvmImageVersionOutput() *MicrovmImageVersionDataOutput {
	return &MicrovmImageVersionDataOutput{
		ImageArn:                 "image-1",
		ImageVersion:             "1",
		State:                    "SUCCESSFUL",
		Status:                   "ACTIVE",
		BaseImageArn:             "base-1",
		BaseImageVersion:         "base-version-1",
		BuildRoleArn:             "role-1",
		CodeArtifact:             CodeArtifact{Uri: "s3://bucket/app.tar"},
		AdditionalOsCapabilities: []string{"ALL"},
		CpuConfigurations:        []CpuConfiguration{{Architecture: "ARM_64"}},
		Description:              "demo version",
		EgressNetworkConnectors:  []string{"egress-1"},
		EnvironmentVariables:     map[string]string{"ENV": "test"},
		Hooks:                    &Hooks{Port: aws.Int64(8080)},
		Logging:                  &Logging{Disabled: aws.Bool(true)},
		Resources:                []Resources{{MinimumMemoryInMiB: 512}},
		StateReason:              "ready",
		Tags:                     map[string]string{"env": "test"},
		CreatedAt:                "2026-06-29T00:00:00Z",
		UpdatedAt:                "2026-06-29T00:01:00Z",
	}
}

func microvmResponse() string {
	return `{
		"microvmId":"microvm-1",
		"endpoint":"https://microvm.example.com",
		"imageArn":"image-1",
		"imageVersion":"1",
		"state":"RUNNING",
		"startedAt":1782691200,
		"terminatedAt":1782691260,
		"maximumDurationInSeconds":3600,
		"executionRoleArn":"role-1",
		"ingressNetworkConnectors":["ingress-1"],
		"egressNetworkConnectors":["egress-1"],
		"idlePolicy":{
			"autoResumeEnabled":true,
			"maxIdleDurationSeconds":30,
			"suspendedDurationSeconds":60
		},
		"stateReason":"ready"
	}`
}

func microvmOutput() *MicrovmDataOutput {
	return &MicrovmDataOutput{
		MicrovmId:                "microvm-1",
		Endpoint:                 "https://microvm.example.com",
		ImageArn:                 "image-1",
		ImageVersion:             "1",
		State:                    "RUNNING",
		StartedAt:                "2026-06-29T00:00:00Z",
		TerminatedAt:             "2026-06-29T00:01:00Z",
		MaximumDurationInSeconds: 3600,
		ExecutionRoleArn:         "role-1",
		IngressNetworkConnectors: []string{"ingress-1"},
		EgressNetworkConnectors:  []string{"egress-1"},
		IdlePolicy: &IdlePolicy{
			AutoResumeEnabled:        true,
			MaxIdleDurationSeconds:   30,
			SuspendedDurationSeconds: 60,
		},
		StateReason: "ready",
	}
}
