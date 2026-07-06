package lambdamicrovms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMicrovmImageReplaceFields(t *testing.T) {
	assert.Equal(t, []string{"name"}, (&MicrovmImageResource{}).ReplaceFields())
}

func TestMicrovmImageCreateWaitsAndReturnsReadOutput(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	createRoute := "POST /2025-09-09/microvm-images"
	getRoute := "GET /2025-09-09/microvm-images/image-1"
	fake.on(createRoute, func(n int) (int, string) {
		return http.StatusCreated, `{"imageArn":"image-1","state":"CREATING"}`
	})
	fake.on(getRoute, func(n int) (int, string) {
		return http.StatusOK, microvmImageGetResponse("image-1", "demo", "CREATED")
	})

	out, err := testMicrovmImage(nil).Create(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, microvmImageOutput("image-1", "demo", "CREATED"), out)
	body := sentJSON(t, fake, createRoute, 0)
	assert.NotEmpty(t, body["clientToken"])
}

func TestMicrovmImageCreateSendsOptionalFields(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	createRoute := "POST /2025-09-09/microvm-images"
	getRoute := "GET /2025-09-09/microvm-images/image-1"
	fake.on(createRoute, func(n int) (int, string) {
		return http.StatusCreated, `{"imageArn":"image-1","state":"CREATING"}`
	})
	fake.on(getRoute, func(n int) (int, string) {
		return http.StatusOK, microvmImageGetResponse("image-1", "demo", "CREATED")
	})

	image := testMicrovmImage(map[string]string{"env": "test"})
	image.BaseImageVersion = aws.String("base-version-1")
	image.AdditionalOsCapabilities = &[]string{"ALL"}
	image.CpuConfigurations = &[]CpuConfiguration{{Architecture: "ARM_64"}}
	image.Description = aws.String("description")
	image.EgressNetworkConnectors = &[]string{"egress-1"}
	image.EnvironmentVariables = &map[string]string{"ENV": "test"}
	image.Hooks = &Hooks{Port: aws.Int64(8080)}
	image.Logging = &Logging{CloudWatch: &CloudWatchLogging{LogGroup: aws.String("group")}}
	image.Resources = &[]Resources{{MinimumMemoryInMiB: 512}}

	_, err := image.Create(context.Background(), fake.configuration())
	require.NoError(t, err)
	body := sentJSON(t, fake, createRoute, 0)
	assert.Equal(t, "base-1", body["baseImageArn"])
	assert.Equal(t, "role-1", body["buildRoleArn"])
	assert.Equal(t, "s3://bucket/app.tar", body["codeArtifact"].(map[string]any)["uri"])
	assert.Equal(t, "base-version-1", body["baseImageVersion"])
	assert.Equal(t, []any{"ALL"}, body["additionalOsCapabilities"])
	assert.Equal(t, "ARM_64", body["cpuConfigurations"].([]any)[0].(map[string]any)["architecture"])
	assert.Equal(t, "description", body["description"])
	assert.Equal(t, []any{"egress-1"}, body["egressNetworkConnectors"])
	assert.Equal(t, "test", body["environmentVariables"].(map[string]any)["ENV"])
	assert.Equal(t, float64(8080), body["hooks"].(map[string]any)["port"])
	logging := body["logging"].(map[string]any)
	cloudWatch := logging["cloudWatch"].(map[string]any)
	assert.Equal(t, "group", cloudWatch["logGroup"])
	assert.Equal(t, float64(512), body["resources"].([]any)[0].(map[string]any)["minimumMemoryInMiB"])
	assert.Equal(t, "test", body["tags"].(map[string]any)["env"])
}

func TestMicrovmImageCreateFailureStateReturnsError(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	fake.on("POST /2025-09-09/microvm-images", func(n int) (int, string) {
		return http.StatusCreated, `{"imageArn":"image-1","state":"CREATING"}`
	})
	fake.on("GET /2025-09-09/microvm-images/image-1", func(n int) (int, string) {
		return http.StatusOK, microvmImageGetResponse("image-1", "demo", "CREATE_FAILED")
	})

	_, err := testMicrovmImage(nil).Create(context.Background(), fake.configuration())
	require.ErrorContains(t, err, "demo")
	assert.ErrorContains(t, err, "CREATE_FAILED")
}

func TestMicrovmImageReadNotFound(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	fake.on("GET /2025-09-09/microvm-images/missing", func(n int) (int, string) {
		return http.StatusNotFound, `{"__type":"ResourceNotFoundException","message":"not found"}`
	})

	_, err := (&MicrovmImageResource{}).Read(context.Background(), fake.configuration(),
		&MicrovmImageResourceOutput{ImageArn: "missing"})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestMicrovmImageUpdatePutSendsFullDesiredConfig(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	putRoute := "PUT /2025-09-09/microvm-images/image-1"
	getRoute := "GET /2025-09-09/microvm-images/image-1"
	fake.on(putRoute, func(n int) (int, string) {
		return http.StatusOK, `{"imageArn":"image-1","state":"UPDATING"}`
	})
	fake.on(getRoute, func(n int) (int, string) {
		return http.StatusOK, microvmImageGetResponse("image-1", "demo", "UPDATED")
	})

	prior := testMicrovmImage(nil)
	current := testMicrovmImage(nil)
	current.Description = aws.String("new description")
	_, err := current.Update(context.Background(), fake.configuration(),
		runtime.Prior[MicrovmImageResource, *MicrovmImageResourceOutput]{
			Inputs:  *prior,
			Outputs: &MicrovmImageResourceOutput{ImageArn: "image-1"},
		})
	require.NoError(t, err)
	body := sentJSON(t, fake, putRoute, 0)
	assert.Equal(t, "base-1", body["baseImageArn"])
	assert.Equal(t, "role-1", body["buildRoleArn"])
	assert.Equal(t, "s3://bucket/app.tar", body["codeArtifact"].(map[string]any)["uri"])
	assert.NotEmpty(t, body["clientToken"])
}

func TestMicrovmImageUpdateSyncsTags(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	putRoute := "PUT /2025-09-09/microvm-images/image-1"
	getRoute := "GET /2025-09-09/microvm-images/image-1"
	listTagsRoute := "GET /2017-03-31/tags/image-1"
	untagRoute := "DELETE /2017-03-31/tags/image-1"
	tagRoute := "POST /2017-03-31/tags/image-1"
	fake.on(putRoute, func(n int) (int, string) {
		return http.StatusOK, `{"imageArn":"image-1","state":"UPDATING"}`
	})
	fake.on(getRoute, func(n int) (int, string) {
		return http.StatusOK, microvmImageGetResponse("image-1", "demo", "UPDATED")
	})
	fake.on(listTagsRoute, func(n int) (int, string) {
		return http.StatusOK, `{"Tags":{"old":"x","changed":"old","same":"value","aws:system":"keep"}}`
	})
	fake.on(untagRoute, func(n int) (int, string) { return http.StatusOK, `{}` })
	fake.on(tagRoute, func(n int) (int, string) { return http.StatusOK, `{}` })

	prior := testMicrovmImage(map[string]string{"old": "x"})
	current := testMicrovmImage(map[string]string{
		"changed": "new",
		"same":    "value",
		"new":     "value",
	})
	current.Description = aws.String("new description")
	_, err := current.Update(context.Background(), fake.configuration(),
		runtime.Prior[MicrovmImageResource, *MicrovmImageResourceOutput]{
			Inputs:  *prior,
			Outputs: &MicrovmImageResourceOutput{ImageArn: "image-1"},
		})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"old"}, fake.queries(untagRoute)[0]["tagKeys"])
	tagBody := sentJSON(t, fake, tagRoute, 0)
	assert.Equal(t, map[string]any{"changed": "new", "new": "value"}, tagBody["Tags"])
}

func TestMicrovmImageUpdateSkipsPutWhenOnlyTagsChanged(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	putRoute := "PUT /2025-09-09/microvm-images/image-1"
	getRoute := "GET /2025-09-09/microvm-images/image-1"
	listTagsRoute := "GET /2017-03-31/tags/image-1"
	tagRoute := "POST /2017-03-31/tags/image-1"
	fake.on(getRoute, func(n int) (int, string) {
		return http.StatusOK, microvmImageGetResponse("image-1", "demo", "UPDATED")
	})
	fake.on(listTagsRoute, func(n int) (int, string) {
		return http.StatusOK, `{"Tags":{}}`
	})
	fake.on(tagRoute, func(n int) (int, string) { return http.StatusOK, `{}` })

	prior := testMicrovmImage(map[string]string{"old": "value"})
	current := testMicrovmImage(map[string]string{"new": "value"})
	_, err := current.Update(context.Background(), fake.configuration(),
		runtime.Prior[MicrovmImageResource, *MicrovmImageResourceOutput]{
			Inputs:  *prior,
			Outputs: &MicrovmImageResourceOutput{ImageArn: "image-1"},
		})
	require.NoError(t, err)
	assert.Empty(t, fake.sent(putRoute))
}

func TestMicrovmImageUpdateSkipsPutWhenOnlyTerminateOnDestroyChanged(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	putRoute := "PUT /2025-09-09/microvm-images/image-1"
	fake.on("GET /2025-09-09/microvm-images/image-1", func(n int) (int, string) {
		return http.StatusOK, microvmImageGetResponse("image-1", "demo", "UPDATED")
	})

	prior := testMicrovmImage(nil)
	current := testMicrovmImage(nil)
	current.TerminateOnDestroy = aws.Bool(true)
	_, err := current.Update(context.Background(), fake.configuration(),
		runtime.Prior[MicrovmImageResource, *MicrovmImageResourceOutput]{
			Inputs:  *prior,
			Outputs: &MicrovmImageResourceOutput{ImageArn: "image-1"},
		})
	require.NoError(t, err)
	assert.Empty(t, fake.sent(putRoute))
}

func TestMicrovmImageDeleteWaitsUntilGone(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	fake.on("DELETE /2025-09-09/microvm-images/image-1", func(n int) (int, string) {
		return http.StatusOK, `{"imageIdentifier":"image-1","state":"DELETING"}`
	})
	fake.on("GET /2025-09-09/microvm-images/image-1", func(n int) (int, string) {
		return http.StatusNotFound, `{"__type":"ResourceNotFoundException","message":"not found"}`
	})

	err := (&MicrovmImageResource{}).Delete(context.Background(), fake.configuration(),
		&MicrovmImageResourceOutput{ImageArn: "image-1"})
	assert.NoError(t, err)
}

func TestMicrovmImageDeleteDoesNotTerminateMicrovmsByDefault(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	fake.on("DELETE /2025-09-09/microvm-images/image-1", func(n int) (int, string) {
		return http.StatusBadRequest,
			`{"__type":"ValidationException","message":"Cannot delete MicroVM image with running MicroVMs"}`
	})

	err := (&MicrovmImageResource{}).Delete(context.Background(), fake.configuration(),
		&MicrovmImageResourceOutput{ImageArn: "image-1"})
	require.ErrorContains(t, err, "Cannot delete MicroVM image with running MicroVMs")
}

func TestMicrovmImageDeleteTerminatesMicrovmsBeforeDeletingImage(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	listRoute := "GET /2025-09-09/microvms"
	terminateRoute := "DELETE /2025-09-09/microvms/microvm-1"
	deleteRoute := "DELETE /2025-09-09/microvm-images/image-1"
	fake.on(listRoute, func(n int) (int, string) {
		if n == 1 {
			return http.StatusOK, microvmItemsResponse("microvm-1", "image-1", "RUNNING")
		}
		return http.StatusOK, `{"items":[]}`
	})
	fake.on(terminateRoute, func(n int) (int, string) { return http.StatusOK, `{}` })
	fake.on(deleteRoute, func(n int) (int, string) {
		if len(fake.sent(terminateRoute)) == 0 {
			return http.StatusBadRequest,
				`{"__type":"ValidationException","message":"Cannot delete MicroVM image with running MicroVMs"}`
		}
		return http.StatusOK, `{"imageIdentifier":"image-1","state":"DELETING"}`
	})
	fake.on("GET /2025-09-09/microvm-images/image-1", func(n int) (int, string) {
		return http.StatusNotFound, `{"__type":"ResourceNotFoundException","message":"not found"}`
	})

	err := (&MicrovmImageResource{TerminateOnDestroy: aws.Bool(true)}).Delete(
		context.Background(), fake.configuration(), &MicrovmImageResourceOutput{ImageArn: "image-1"})
	require.NoError(t, err)
	assert.Len(t, fake.sent(terminateRoute), 1)
	queries := fake.queries(listRoute)
	require.Len(t, queries, 2)
	assert.Equal(t, "image-1", queries[0].Get("imageIdentifier"))
	assert.Equal(t, "image-1", queries[1].Get("imageIdentifier"))
}

func TestMicrovmImageDeleteTreatsInitialNotFoundAsSuccess(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	fake.on("DELETE /2025-09-09/microvm-images/image-1", func(n int) (int, string) {
		return http.StatusNotFound, `{"__type":"ResourceNotFoundException","message":"not found"}`
	})

	err := (&MicrovmImageResource{}).Delete(context.Background(), fake.configuration(),
		&MicrovmImageResourceOutput{ImageArn: "image-1"})
	assert.NoError(t, err)
}

func TestMicrovmImageDeleteFailureStateReturnsError(t *testing.T) {
	fake := newFakeLambdaMicrovms(t)
	fake.on("DELETE /2025-09-09/microvm-images/image-1", func(n int) (int, string) {
		return http.StatusOK, `{"imageIdentifier":"image-1","state":"DELETING"}`
	})
	fake.on("GET /2025-09-09/microvm-images/image-1", func(n int) (int, string) {
		return http.StatusOK, microvmImageGetResponse("image-1", "demo", "DELETE_FAILED")
	})

	err := (&MicrovmImageResource{}).Delete(context.Background(), fake.configuration(),
		&MicrovmImageResourceOutput{ImageArn: "image-1"})
	require.ErrorContains(t, err, "DELETE_FAILED")
}

func testMicrovmImage(tags map[string]string) *MicrovmImageResource {
	image := &MicrovmImageResource{
		Name:         "demo",
		BaseImageArn: "base-1",
		BuildRoleArn: "role-1",
		CodeArtifact: CodeArtifact{Uri: "s3://bucket/app.tar"},
	}
	if tags != nil {
		image.Tags = &tags
	}
	return image
}

func microvmImageGetResponse(imageArn, name, state string) string {
	return `{
		"imageArn":"` + imageArn + `",
		"name":"` + name + `",
		"state":"` + state + `",
		"createdAt":1782691200,
		"updatedAt":1782691260,
		"latestActiveImageVersion":"1",
		"latestFailedImageVersion":"2",
		"tags":{"env":"test"}
	}`
}

func microvmItemsResponse(microvmID, imageArn, state string) string {
	return `{
		"items":[{
			"microvmId":"` + microvmID + `",
			"imageArn":"` + imageArn + `",
			"imageVersion":"1",
			"state":"` + state + `",
			"startedAt":1782691200
		}]
	}`
}

func microvmImageOutput(imageArn, name, state string) *MicrovmImageResourceOutput {
	return &MicrovmImageResourceOutput{
		ImageArn:                 imageArn,
		Name:                     name,
		State:                    state,
		CreatedAt:                "2026-06-29T00:00:00Z",
		UpdatedAt:                "2026-06-29T00:01:00Z",
		LatestActiveImageVersion: "1",
		LatestFailedImageVersion: "2",
	}
}

func sentJSON(t *testing.T, fake *fakeLambdaMicrovms, route string, index int) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(fake.sent(route)[index], &body))
	return body
}
