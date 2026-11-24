package codebuild

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectRemovalAndDrift(t *testing.T) {
	old := minimalProject()
	old.AutoRetryLimit, old.BadgeEnabled, old.ConcurrentBuildLimit = aws.Int64(3),
		aws.Bool(true), aws.Int64(4)
	old.Description, old.EncryptionKey, old.SourceVersion = aws.String("d"),
		aws.String("key"), aws.String("v")
	old.Cache = &ProjectCache{Type: aws.String("LOCAL"), Modes: &[]string{"LOCAL_SOURCE_CACHE"}}
	old.BuildBatchConfig = &ProjectBuildBatchConfig{ServiceRole: old.ServiceRole}
	old.VpcConfig = &ProjectVpcConfig{
		VpcId: "vpc", Subnets: []string{"s"}, SecurityGroupIds: []string{"sg"},
	}
	old.FileSystemLocations = &[]ProjectFileSystemLocation{{Identifier: aws.String("old")}}
	old.SecondaryArtifacts = &[]SecondaryProjectArtifacts{{Type: "S3", ArtifactIdentifier: "a"}}
	old.SecondarySources = &[]SecondaryProjectSource{{Type: "S3", SourceIdentifier: "s"}}
	old.SecondarySourceVersions = &[]ProjectSourceVersion{{SourceIdentifier: "s", SourceVersion: "v"}}
	old.LogsConfig = &ProjectLogsConfig{CloudWatchLogs: &ProjectCloudWatchLogs{
		GroupName: aws.String("old"), Status: aws.String("DISABLED"),
	}}
	old.Tags = &map[string]string{"old": "tag"}
	remote := minimalResponse()
	full, err := old.prepare(false)
	require.NoError(t, err)
	remote.AutoRetryLimit, remote.Badge = full.AutoRetryLimit, &types.ProjectBadge{BadgeEnabled: true}
	remote.ConcurrentBuildLimit, remote.Description = full.ConcurrentBuildLimit, full.Description
	remote.EncryptionKey, remote.SourceVersion = full.EncryptionKey, full.SourceVersion
	remote.Cache, remote.BuildBatchConfig = full.Cache, full.BuildBatchConfig
	remote.VpcConfig = full.VpcConfig
	remote.FileSystemLocations = full.FileSystemLocations
	remote.SecondaryArtifacts = full.SecondaryArtifacts
	remote.SecondarySources = full.SecondarySources
	remote.SecondarySourceVersions = full.SecondarySourceVersions
	remote.LogsConfig, remote.Tags = full.LogsConfig, full.Tags
	out, err := projectOutput(remote)
	require.NoError(t, err)
	desired := minimalProject()
	in, err := desired.prepare(false)
	require.NoError(t, err)
	patch, visible, err := desired.patch(in, old, out, out)
	require.NoError(t, err)
	assert.False(t, visible)
	assert.Equal(t, &sdk.UpdateProjectInput{
		Name: remote.Name, AutoRetryLimit: aws.Int32(0), BadgeEnabled: aws.Bool(false),
		ConcurrentBuildLimit: aws.Int32(-1), Description: aws.String(""), EncryptionKey: aws.String(""),
		SourceVersion: aws.String(""), Cache: &types.ProjectCache{Type: "NO_CACHE"},
		BuildBatchConfig: &types.ProjectBuildBatchConfig{}, VpcConfig: &types.VpcConfig{},
		FileSystemLocations: []types.ProjectFileSystemLocation{},
		SecondaryArtifacts:  []types.ProjectArtifacts{}, SecondarySources: []types.ProjectSource{},
		SecondarySourceVersions: []types.ProjectSourceVersion{}, Tags: []types.Tag{},
		LogsConfig: &types.LogsConfig{
			CloudWatchLogs: &types.CloudWatchLogsConfig{Status: "ENABLED"},
			S3Logs:         &types.S3LogsConfig{Status: "DISABLED"},
		},
	}, patch)
	in, err = desired.prepare(false)
	require.NoError(t, err)
	patch, _, err = desired.patch(in, desired, out, out)
	require.NoError(t, err)
	require.NotNil(t, patch)
	assert.Nil(t, patch.Description)
	assert.Nil(t, patch.Cache)
	assert.Nil(t, patch.EncryptionKey)
	assert.Nil(t, patch.AutoRetryLimit)
	assert.Equal(t, aws.Bool(false), patch.BadgeEnabled)
}

func TestProjectCollectionSemantics(t *testing.T) {
	r := minimalProject()
	r.SecondaryArtifacts = &[]SecondaryProjectArtifacts{
		{Type: "S3", ArtifactIdentifier: "a", Name: aws.String("first")},
		{Type: "S3", ArtifactIdentifier: "a", Name: aws.String("second")},
	}
	r.VpcConfig = &ProjectVpcConfig{VpcId: "v", Subnets: []string{"b", "a", "a"},
		SecurityGroupIds: []string{"b", "a", "b"}}
	in, err := r.prepare(false)
	require.NoError(t, err)
	assert.Equal(t, []types.ProjectArtifacts{{
		Type: "S3", ArtifactIdentifier: aws.String("a"), Name: aws.String("first"),
		EncryptionDisabled: aws.Bool(false), OverrideArtifactName: aws.Bool(false),
		NamespaceType: "NONE", Packaging: "NONE",
	}}, in.SecondaryArtifacts)
	assert.Equal(t, []string{"a", "b"}, in.VpcConfig.Subnets)
	assert.Equal(t, []string{"a", "b"}, in.VpcConfig.SecurityGroupIds)
	p := minimalResponse()
	p.SecondaryArtifacts, p.VpcConfig = in.SecondaryArtifacts, in.VpcConfig
	out, err := projectOutput(p)
	require.NoError(t, err)
	(*r.SecondaryArtifacts)[0].Name = aws.String("changed")
	in, err = r.prepare(false)
	require.NoError(t, err)
	patch, _, err := r.patch(in, r, out, out)
	require.NoError(t, err)
	assert.Nil(t, patch)
	(*r.SecondaryArtifacts)[0].Path = aws.String("new-path")
	in, err = r.prepare(false)
	require.NoError(t, err)
	patch, _, err = r.patch(in, minimalProject(), out, out)
	require.NoError(t, err)
	require.NotNil(t, patch)
	assert.Equal(t, in.SecondaryArtifacts, patch.SecondaryArtifacts)
}

func TestProjectUpdatePreservesConfiguredSourceIdentities(t *testing.T) {
	before := minimalProject()
	desired := minimalProject()
	desired.SecondarySources = &[]SecondaryProjectSource{
		{Type: "S3", SourceIdentifier: "same"},
		{Type: "S3", SourceIdentifier: "same", BuildStatusConfig: &ProjectBuildStatusConfig{}},
	}
	in, err := desired.prepare(false)
	require.NoError(t, err)
	out, err := projectOutput(minimalResponse())
	require.NoError(t, err)
	patch, _, err := desired.patch(in, before, out, out)
	require.NoError(t, err)
	require.NotNil(t, patch)
	expected := types.ProjectSource{
		Type: "S3", SourceIdentifier: aws.String("same"), Buildspec: aws.String(""),
		GitCloneDepth: aws.Int32(0), InsecureSsl: aws.Bool(false),
	}
	assert.Equal(t, []types.ProjectSource{expected, expected}, patch.SecondarySources)
}

func TestProjectEffectiveCollectionIdentity(t *testing.T) {
	r := minimalProject()
	r.SecondarySources = &[]SecondaryProjectSource{
		{Type: "S3", SourceIdentifier: "same"},
		{Type: "S3", SourceIdentifier: "same", Buildspec: aws.String(""),
			GitCloneDepth: aws.Int64(0), InsecureSsl: aws.Bool(false), ReportBuildStatus: aws.Bool(false)},
		{Type: "S3", SourceIdentifier: "same", Buildspec: aws.String("other")},
	}
	r.SecondarySourceVersions = &[]ProjectSourceVersion{
		{SourceIdentifier: "same", SourceVersion: "b"},
		{SourceIdentifier: "same", SourceVersion: "a"},
		{SourceIdentifier: "same", SourceVersion: "b"},
	}
	r.FileSystemLocations = &[]ProjectFileSystemLocation{
		{Identifier: aws.String("same"), MountPoint: aws.String("/b")},
		{Identifier: aws.String("same"), MountPoint: aws.String("/a")},
		{Identifier: aws.String("same"), MountPoint: aws.String("/a"), Type: aws.String("EFS")},
	}
	in, err := r.prepare(false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []types.ProjectSource{
		{Type: "S3", SourceIdentifier: aws.String("same"), Buildspec: aws.String(""),
			GitCloneDepth: aws.Int32(0), InsecureSsl: aws.Bool(false)},
		{Type: "S3", SourceIdentifier: aws.String("same"), Buildspec: aws.String("other"),
			GitCloneDepth: aws.Int32(0), InsecureSsl: aws.Bool(false)},
	}, in.SecondarySources)
	assert.Equal(t, []types.ProjectSourceVersion{
		{SourceIdentifier: aws.String("same"), SourceVersion: aws.String("a")},
		{SourceIdentifier: aws.String("same"), SourceVersion: aws.String("b")},
	}, in.SecondarySourceVersions)
	assert.Equal(t, []types.ProjectFileSystemLocation{
		{Type: "EFS", Identifier: aws.String("same"), MountPoint: aws.String("/a")},
		{Type: "EFS", Identifier: aws.String("same"), MountPoint: aws.String("/b")},
	}, in.FileSystemLocations)
	reversed := []SecondaryProjectSource{(*r.SecondarySources)[2], (*r.SecondarySources)[0]}
	assert.Equal(t, sourceSet(r.SecondarySources), sourceSet(&reversed))
}

func TestProjectArtifactNameAndDefaultComparison(t *testing.T) {
	before := minimalProject()
	before.Artifacts = ProjectArtifacts{Type: "S3"}
	before.SecondaryArtifacts = &[]SecondaryProjectArtifacts{
		{Type: "S3", ArtifactIdentifier: "same", Name: aws.String("old")},
	}
	response := minimalResponse()
	response.Artifacts = &types.ProjectArtifacts{
		Type: "S3", Name: aws.String("test"), OverrideArtifactName: aws.Bool(false),
		EncryptionDisabled: aws.Bool(false), NamespaceType: "NONE", Packaging: "NONE",
	}
	response.SecondaryArtifacts = []types.ProjectArtifacts{
		*secondaryArtifact((*before.SecondaryArtifacts)[0]),
	}
	out, err := projectOutput(response)
	require.NoError(t, err)
	desired := before
	desired.SecondaryArtifacts = &[]SecondaryProjectArtifacts{
		{Type: "S3", ArtifactIdentifier: "same", Name: aws.String("new")},
	}
	in, err := desired.prepare(false)
	require.NoError(t, err)
	patch, _, err := desired.patch(in, before, out, out)
	require.NoError(t, err)
	assert.Nil(t, patch)
	desired.Description = aws.String("new")
	in, err = desired.prepare(false)
	require.NoError(t, err)
	patch, _, err = desired.patch(in, before, out, out)
	require.NoError(t, err)
	assert.Equal(t, &sdk.UpdateProjectInput{
		Name: response.Name, Description: aws.String("new"), Tags: []types.Tag{},
	}, patch)
	(*desired.SecondaryArtifacts)[0].Path = aws.String("path")
	in, err = desired.prepare(false)
	require.NoError(t, err)
	patch, _, err = desired.patch(in, before, out, out)
	require.NoError(t, err)
	assert.Equal(t, in.SecondaryArtifacts, patch.SecondaryArtifacts)
	assert.Equal(t, aws.String("new"), patch.SecondaryArtifacts[0].Name)
}

func TestProjectLambdaTimeoutSuppression(t *testing.T) {
	for _, kind := range []string{"LINUX_LAMBDA_CONTAINER", "ARM_LAMBDA_CONTAINER"} {
		r := minimalProject()
		r.Environment.Type = kind
		response := minimalResponse()
		response.Environment.Type = types.EnvironmentType(kind)
		response.TimeoutInMinutes, response.QueuedTimeoutInMinutes = aws.Int32(5), aws.Int32(5)
		out, err := projectOutput(response)
		require.NoError(t, err)
		in, err := r.prepare(true)
		require.NoError(t, err)
		assert.Equal(t, aws.Int32(60), in.TimeoutInMinutes)
		assert.Equal(t, aws.Int32(480), in.QueuedTimeoutInMinutes)
		patch, _, err := r.patch(in, r, out, out)
		require.NoError(t, err)
		assert.Nil(t, patch)
	}
}

func TestProjectEnvironmentRemoval(t *testing.T) {
	var before ProjectResource
	var response types.Project
	readFixture(t, "input", &before)
	readFixture(t, "response", &response)
	out, err := projectOutput(response)
	require.NoError(t, err)
	desired := before
	desired.Environment = minimalProject().Environment
	in, err := desired.prepare(false)
	require.NoError(t, err)
	patch, _, err := desired.patch(in, before, out, out)
	require.NoError(t, err)
	require.NotNil(t, patch)
	assert.Equal(t, &types.ProjectEnvironment{
		Type: "LINUX_CONTAINER", ComputeType: "BUILD_GENERAL1_SMALL", Image: aws.String("image"),
		ImagePullCredentialsType: "CODEBUILD", PrivilegedMode: aws.Bool(false),
	}, patch.Environment)
}
