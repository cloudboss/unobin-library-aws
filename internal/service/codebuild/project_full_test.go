package codebuild

import (
	"encoding/json"
	"os"
	"testing"

	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, name string, target any) {
	t.Helper()
	data, err := os.ReadFile("testdata/full/" + name + ".json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, target))
}

func TestProjectFullRequestAndOutput(t *testing.T) {
	var input ProjectResource
	var expected sdk.CreateProjectInput
	var response types.Project
	var output ProjectResourceOutput
	readFixture(t, "input", &input)
	readFixture(t, "create", &expected)
	readFixture(t, "response", &response)
	readFixture(t, "output", &output)
	in, err := input.prepare(true)
	require.NoError(t, err)
	assert.Equal(t, &expected, in)
	got, err := projectOutput(response)
	require.NoError(t, err)
	assert.Equal(t, &output, got)
}
