package codebuild

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func minimalProject() ProjectResource {
	return ProjectResource{
		Name: "test", ServiceRole: "arn:aws:iam::123456789012:role/build",
		Artifacts: ProjectArtifacts{Type: "NO_ARTIFACTS"},
		Environment: ProjectEnvironment{
			ComputeType: "BUILD_GENERAL1_SMALL", Image: "image", Type: "LINUX_CONTAINER",
		},
		Source:           ProjectSource{Type: "NO_SOURCE", Buildspec: aws.String("version: 0.2")},
		TimeoutInMinutes: 60, QueuedTimeoutInMinutes: 480,
	}
}

func minimalResponse() types.Project {
	r := minimalProject()
	in, _ := r.prepare(true)
	return types.Project{
		Arn:  aws.String("arn:aws:codebuild:us-east-1:123456789012:project/test"),
		Name: in.Name, Artifacts: in.Artifacts, Environment: in.Environment,
		ServiceRole: in.ServiceRole, Source: in.Source, TimeoutInMinutes: in.TimeoutInMinutes,
		QueuedTimeoutInMinutes: in.QueuedTimeoutInMinutes, LogsConfig: in.LogsConfig,
	}
}

func TestProjectMinimalRequest(t *testing.T) {
	r := minimalProject()
	got, err := r.prepare(true)
	require.NoError(t, err)
	assert.Equal(t, &sdk.CreateProjectInput{
		Name: aws.String("test"), ServiceRole: aws.String(r.ServiceRole),
		Artifacts: &types.ProjectArtifacts{
			Type: "NO_ARTIFACTS", OverrideArtifactName: aws.Bool(false),
		},
		Environment: &types.ProjectEnvironment{
			ComputeType: "BUILD_GENERAL1_SMALL", Image: aws.String("image"),
			Type: "LINUX_CONTAINER", ImagePullCredentialsType: "CODEBUILD",
			PrivilegedMode: aws.Bool(false),
		},
		Source: &types.ProjectSource{
			Type: "NO_SOURCE", Buildspec: aws.String("version: 0.2"),
			GitCloneDepth: aws.Int32(0), InsecureSsl: aws.Bool(false),
		},
		LogsConfig: &types.LogsConfig{
			CloudWatchLogs: &types.CloudWatchLogsConfig{Status: "ENABLED"},
			S3Logs:         &types.S3LogsConfig{Status: "DISABLED"},
		},
		TimeoutInMinutes: aws.Int32(60), QueuedTimeoutInMinutes: aws.Int32(480),
		Tags: []types.Tag{},
	}, got)
	r.AutoRetryLimit, r.BadgeEnabled = aws.Int64(0), aws.Bool(false)
	r.Description, r.EncryptionKey, r.SourceVersion = aws.String(""), aws.String(""), aws.String("")
	gotZero, err := r.prepare(true)
	require.NoError(t, err)
	assert.Equal(t, got, gotZero)
}

func TestProjectCreateOnlySource(t *testing.T) {
	for _, tc := range []struct {
		buildspec, location string
		valid               bool
	}{
		{"", "", false}, {" ", "", true}, {"x", " ", false}, {"x", "", true},
	} {
		t.Run(fmt.Sprintf("%q/%q", tc.buildspec, tc.location), func(t *testing.T) {
			r := minimalProject()
			r.Source.Buildspec, r.Source.Location = &tc.buildspec, &tc.location
			_, err := r.prepare(true)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			_, err = r.prepare(false)
			require.NoError(t, err)
			r.Source.Type = "S3"
			r.SecondarySources = &[]SecondaryProjectSource{{Type: "NO_SOURCE", SourceIdentifier: "x"}}
			_, err = r.prepare(true)
			require.NoError(t, err)
		})
	}
}

func TestProjectSources(t *testing.T) {
	for _, kind := range []string{"CODECOMMIT", "CODEPIPELINE", "GITHUB", "GITLAB",
		"GITLAB_SELF_MANAGED", "S3", "BITBUCKET", "GITHUB_ENTERPRISE", "NO_SOURCE"} {
		t.Run(kind, func(t *testing.T) {
			s := ProjectSource{
				Type: kind, Auth: &ProjectSourceAuth{Type: "BASIC_AUTH", Resource: ""},
				BuildStatusConfig:   &ProjectBuildStatusConfig{Context: aws.String("")},
				GitSubmodulesConfig: &ProjectGitSubmodulesConfig{FetchSubmodules: false},
			}
			expected := &types.ProjectSource{
				Type: types.SourceType(kind), Buildspec: aws.String(""),
				GitCloneDepth: aws.Int32(0), InsecureSsl: aws.Bool(false),
				Auth: &types.SourceAuth{Type: "BASIC_AUTH"},
			}
			switch kind {
			case "BITBUCKET", "GITHUB", "GITHUB_ENTERPRISE", "GITLAB", "GITLAB_SELF_MANAGED":
				expected.ReportBuildStatus = aws.Bool(false)
				expected.BuildStatusConfig = &types.BuildStatusConfig{}
			}
			switch kind {
			case "BITBUCKET", "CODECOMMIT", "GITHUB", "GITHUB_ENTERPRISE":
				expected.GitSubmodulesConfig = &types.GitSubmodulesConfig{
					FetchSubmodules: aws.Bool(false),
				}
			}
			assert.Equal(t, expected, source(s))
			secondary := secondarySource(SecondaryProjectSource{
				Type: kind, SourceIdentifier: "secondary", Auth: s.Auth,
				BuildStatusConfig: s.BuildStatusConfig, GitSubmodulesConfig: s.GitSubmodulesConfig,
			})
			expected.SourceIdentifier = aws.String("secondary")
			assert.Equal(t, expected, secondary)
		})
	}
}

func TestProjectNarrowedConcurrentLimit(t *testing.T) {
	for _, limit := range []int64{1, 4294967296, 4294967297} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			r := minimalProject()
			r.ConcurrentBuildLimit, r.AutoRetryLimit = &limit, &limit
			create, err := r.prepare(true)
			require.NoError(t, err)
			assert.Equal(t, aws.Int32(int32(limit)), create.ConcurrentBuildLimit)
			assert.Equal(t, aws.Int32(int32(limit)), create.AutoRetryLimit)
			out, err := projectOutput(minimalResponse())
			require.NoError(t, err)
			in, err := r.prepare(false)
			require.NoError(t, err)
			patch, _, err := r.patch(in, minimalProject(), out, out)
			require.NoError(t, err)
			want := int32(limit)
			if want == 0 {
				want = -1
			}
			expected := &sdk.UpdateProjectInput{
				Name: aws.String("test"), ConcurrentBuildLimit: aws.Int32(want), Tags: []types.Tag{},
			}
			if int32(limit) != 0 {
				expected.AutoRetryLimit = aws.Int32(int32(limit))
			}
			assert.Equal(t, expected, patch)
		})
	}
}

func TestProjectEmptyMembersSDK(t *testing.T) {
	for _, member := range []string{"auth", "credential", "service-role"} {
		for _, empty := range []bool{true, false} {
			for _, create := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/empty=%t/create=%t", member, empty, create), func(t *testing.T) {
					var requests []map[string]any
					var operations []string
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,
						r *http.Request) {
						var body map[string]any
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						requests = append(requests, body)
						operations = append(operations, r.Header.Get("X-Amz-Target"))
						w.Header().Set("Content-Type", "application/x-amz-json-1.1")
						fmt.Fprint(w, "{}")
					}))
					defer server.Close()
					client := sdk.New(sdk.Options{
						Region: "us-east-1", BaseEndpoint: aws.String(server.URL),
						Credentials: aws.AnonymousCredentials{}, HTTPClient: server.Client(),
					})
					r, before := minimalProject(), minimalProject()
					text := "arn:aws:other:::accepted"
					if empty {
						text = ""
					}
					var expected map[string]any
					missing := ""
					switch member {
					case "auth":
						r.Source.Auth = &ProjectSourceAuth{Type: "OAUTH", Resource: text}
						r.SecondarySources = &[]SecondaryProjectSource{{
							Type: "S3", SourceIdentifier: "secondary", Auth: r.Source.Auth,
						}}
					case "credential":
						r.Environment.RegistryCredential = &ProjectRegistryCredential{
							Credential: text, CredentialProvider: "SECRETS_MANAGER",
						}
						if empty {
							missing = "Environment.RegistryCredential.Credential"
						}
					case "service-role":
						r.ServiceRole = text
						if empty && create {
							missing = "ServiceRole"
						}
					}
					in, err := r.prepare(create)
					require.NoError(t, err, "empty parser inputs remain valid")
					switch member {
					case "auth":
						auth := &types.SourceAuth{Type: "OAUTH"}
						if !empty {
							auth.Resource = aws.String(text)
						}
						assert.Equal(t, auth, in.Source.Auth)
						assert.Equal(t, auth, in.SecondarySources[0].Auth)
					case "credential":
						credential := &types.RegistryCredential{CredentialProvider: "SECRETS_MANAGER"}
						if !empty {
							credential.Credential = aws.String(text)
						}
						assert.Equal(t, credential, in.Environment.RegistryCredential)
					case "service-role":
						if empty && create {
							assert.Nil(t, in.ServiceRole)
						} else {
							assert.Equal(t, aws.String(text), in.ServiceRole)
						}
					}
					if create {
						_, err = client.CreateProject(t.Context(), in)
					} else {
						out, outputErr := projectOutput(minimalResponse())
						require.NoError(t, outputErr)
						patch, _, patchErr := r.patch(in, before, out, out)
						require.NoError(t, patchErr)
						require.NotNil(t, patch)
						assert.Equal(t, aws.String("test"), patch.Name)
						_, err = client.UpdateProject(t.Context(), patch)
					}
					if missing != "" {
						var invalid smithy.InvalidParamsError
						require.ErrorAs(t, err, &invalid)
						operation := "UpdateProjectInput"
						if create {
							operation = "CreateProjectInput"
						}
						messages := []string{}
						for _, parameter := range invalid.Errs() {
							assert.IsType(t, &smithy.ParamRequiredError{}, parameter)
							messages = append(messages, parameter.Error())
						}
						assert.Equal(t, []string{
							"missing required field, " + operation + "." + missing + ".",
						}, messages)
						assert.Empty(t, requests)
						assert.Empty(t, operations)
						return
					}
					require.NoError(t, err)
					sourceJSON := map[string]any{"type": "NO_SOURCE", "buildspec": "version: 0.2",
						"gitCloneDepth": float64(0), "insecureSsl": false}
					environmentJSON := map[string]any{
						"computeType": "BUILD_GENERAL1_SMALL", "image": "image",
						"type": "LINUX_CONTAINER", "imagePullCredentialsType": "CODEBUILD",
						"privilegedMode": false,
					}
					if create {
						expected = map[string]any{
							"name": "test", "serviceRole": r.ServiceRole, "tags": []any{},
							"artifacts": map[string]any{"type": "NO_ARTIFACTS",
								"overrideArtifactName": false},
							"environment": environmentJSON, "source": sourceJSON,
							"timeoutInMinutes": float64(60), "queuedTimeoutInMinutes": float64(480),
							"logsConfig": map[string]any{
								"cloudWatchLogs": map[string]any{"status": "ENABLED"},
								"s3Logs":         map[string]any{"status": "DISABLED"}},
						}
					} else {
						expected = map[string]any{"name": "test", "tags": []any{}}
					}
					switch member {
					case "auth":
						auth := map[string]any{"type": "OAUTH"}
						if !empty {
							auth["resource"] = text
						}
						sourceJSON["auth"] = auth
						expected["source"] = sourceJSON
						expected["secondarySources"] = []any{map[string]any{
							"type": "S3", "sourceIdentifier": "secondary", "auth": auth,
							"buildspec": "", "gitCloneDepth": float64(0), "insecureSsl": false,
						}}
					case "credential":
						environmentJSON["registryCredential"] = map[string]any{
							"credential": text, "credentialProvider": "SECRETS_MANAGER"}
						expected["environment"] = environmentJSON
					case "service-role":
						expected["serviceRole"] = text
					}
					assert.Equal(t, []map[string]any{expected}, requests)
					action := "UpdateProject"
					if create {
						action = "CreateProject"
					}
					assert.Equal(t, []string{"CodeBuild_20161006." + action}, operations)
				})
			}
		}
	}
}

func TestProjectArtifacts(t *testing.T) {
	for _, kind := range []string{"S3", "CODEPIPELINE", "NO_ARTIFACTS"} {
		expected := &types.ProjectArtifacts{
			Type: types.ArtifactsType(kind), OverrideArtifactName: aws.Bool(false),
		}
		if kind != "NO_ARTIFACTS" {
			expected.EncryptionDisabled = aws.Bool(false)
		}
		assert.Equal(t, expected, artifact(ProjectArtifacts{Type: kind}))
		expected.NamespaceType, expected.Packaging = "NONE", "NONE"
		expected.ArtifactIdentifier = aws.String("secondary")
		assert.Equal(t, expected, secondaryArtifact(SecondaryProjectArtifacts{
			Type: kind, ArtifactIdentifier: "secondary",
		}))
	}
	assert.Equal(t, &types.ProjectArtifacts{
		Type: "S3", ArtifactIdentifier: aws.String("id"), BucketOwnerAccess: "FULL",
		EncryptionDisabled: aws.Bool(true), Location: aws.String("bucket"), Name: aws.String("out"),
		NamespaceType: "BUILD_ID", OverrideArtifactName: aws.Bool(true),
		Packaging: "ZIP", Path: aws.String("path"),
	}, artifact(ProjectArtifacts{
		Type: "S3", ArtifactIdentifier: aws.String("id"), BucketOwnerAccess: aws.String("FULL"),
		EncryptionDisabled: aws.Bool(true), Location: aws.String("bucket"), Name: aws.String("out"),
		NamespaceType: aws.String("BUILD_ID"), OverrideArtifactName: aws.Bool(true),
		Packaging: aws.String("ZIP"), Path: aws.String("path"),
	}))
}

func TestProjectEnvironment(t *testing.T) {
	e := ProjectEnvironment{
		Type: "LINUX_CONTAINER", ComputeType: "BUILD_GENERAL1_SMALL", Image: "img",
		Certificate: aws.String("cert.pem"), ImagePullCredentialsType: aws.String("SERVICE_ROLE"),
		DockerServer: &ProjectDockerServer{
			ComputeType: "BUILD_GENERAL1_LARGE", SecurityGroupIds: &[]string{"sg-b", "sg-a"},
		},
		Fleet: &ProjectFleet{},
		RegistryCredential: &ProjectRegistryCredential{
			Credential: "secret", CredentialProvider: "SECRETS_MANAGER",
		},
		EnvironmentVariables: &[]ProjectEnvironmentVariable{
			{Name: "", Value: ""},
			{Name: "secret", Value: "secret-key", Type: aws.String("SECRETS_MANAGER")},
		},
	}
	assert.Equal(t, &types.ProjectEnvironment{
		Type: "LINUX_CONTAINER", ComputeType: "BUILD_GENERAL1_SMALL", Image: aws.String("img"),
		Certificate: aws.String("cert.pem"), PrivilegedMode: aws.Bool(false),
		ImagePullCredentialsType: "SERVICE_ROLE",
		DockerServer: &types.DockerServer{
			ComputeType: "BUILD_GENERAL1_LARGE", SecurityGroupIds: []string{"sg-b", "sg-a"},
		},
		Fleet: &types.ProjectFleet{},
		RegistryCredential: &types.RegistryCredential{
			Credential: aws.String("secret"), CredentialProvider: "SECRETS_MANAGER",
		},
		EnvironmentVariables: []types.EnvironmentVariable{
			{Value: aws.String(""), Type: "PLAINTEXT"},
			{Name: aws.String("secret"), Value: aws.String("secret-key"), Type: "SECRETS_MANAGER"},
		},
	}, environment(e))
}

func TestProjectParsers(t *testing.T) {
	for _, account := range []string{"", "aws", "aws-managed", "third-party", "aws-marketplace",
		"partner-managed", "123456789012", "cwabcdefghij"} {
		require.NoError(t, parseARN("arn:aws-custom:other:us-gov-east-1:"+account+":item"))
	}
	for _, arn := range []string{"", "arn:aws:other:::x", "arn:aws:s3:::bucket/key"} {
		require.NoError(t, parseARN(arn))
	}
	for _, arn := range []string{"arn:other:iam:::x", "arn:aws:iam:US-east-1::x",
		"arn:aws:iam:us-east-123::x", "arn:aws:iam::123:x", "arn:aws:iam:::", "no"} {
		require.Error(t, parseARN(arn), arn)
	}
	for _, tc := range []struct {
		location string
		valid    bool
	}{
		{"", true}, {"arn:aws:iam::aws:role/x", true}, {"a/key", true},
		{"1!bucket/key", true}, {"UPPER/key", false}, {"bucket/", false}, {"bucket", false},
	} {
		r := minimalProject()
		r.LogsConfig = &ProjectLogsConfig{S3Logs: &ProjectS3Logs{Location: &tc.location}}
		_, err := r.prepare(false)
		if tc.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}
