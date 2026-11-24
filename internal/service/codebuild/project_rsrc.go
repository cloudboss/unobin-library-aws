package codebuild

import (
	"context"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type ProjectResource struct {
	Name                    string                       `ub:"name"`
	Artifacts               ProjectArtifacts             `ub:"artifacts"`
	Environment             ProjectEnvironment           `ub:"environment"`
	ServiceRole             string                       `ub:"service-role"`
	Source                  ProjectSource                `ub:"source"`
	AutoRetryLimit          *int64                       `ub:"auto-retry-limit"`
	BadgeEnabled            *bool                        `ub:"badge-enabled"`
	BuildBatchConfig        *ProjectBuildBatchConfig     `ub:"build-batch-config"`
	Cache                   *ProjectCache                `ub:"cache"`
	ConcurrentBuildLimit    *int64                       `ub:"concurrent-build-limit"`
	Description             *string                      `ub:"description"`
	EncryptionKey           *string                      `ub:"encryption-key"`
	FileSystemLocations     *[]ProjectFileSystemLocation `ub:"file-system-locations"`
	LogsConfig              *ProjectLogsConfig           `ub:"logs-config"`
	ProjectVisibility       *string                      `ub:"project-visibility"`
	QueuedTimeoutInMinutes  int64                        `ub:"queued-timeout-in-minutes"`
	ResourceAccessRole      *string                      `ub:"resource-access-role"`
	SecondaryArtifacts      *[]SecondaryProjectArtifacts `ub:"secondary-artifacts"`
	SecondarySources        *[]SecondaryProjectSource    `ub:"secondary-sources"`
	SecondarySourceVersions *[]ProjectSourceVersion      `ub:"secondary-source-versions"`
	SourceVersion           *string                      `ub:"source-version"`
	Tags                    *map[string]string           `ub:"tags"`
	TimeoutInMinutes        int64                        `ub:"timeout-in-minutes"`
	VpcConfig               *ProjectVpcConfig            `ub:"vpc-config"`
}

func (r *ProjectResource) ResourceDefinition() runtime.ResourceDefinition[
	ProjectResource, *ProjectResourceOutput, *awscfg.Configuration,
] {
	return runtime.ResourceDefinition[
		ProjectResource, *ProjectResourceOutput, *awscfg.Configuration,
	]{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[ProjectResource]{
			runtime.EqualBy(
				runtime.InputField(func(input *ProjectResource) **[]SecondaryProjectArtifacts {
					return &input.SecondaryArtifacts
				}),
				func(prior, desired *[]SecondaryProjectArtifacts) bool {
					return equal(artifactSet(prior), artifactSet(desired))
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *ProjectResource) **[]SecondaryProjectSource {
					return &input.SecondarySources
				}),
				func(prior, desired *[]SecondaryProjectSource) bool {
					return equal(sourceSet(prior), sourceSet(desired))
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *ProjectResource) **[]ProjectFileSystemLocation {
					return &input.FileSystemLocations
				}),
				func(prior, desired *[]ProjectFileSystemLocation) bool {
					return equal(filesystemSet(prior), filesystemSet(desired))
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *ProjectResource) **[]ProjectSourceVersion {
					return &input.SecondarySourceVersions
				}),
				func(prior, desired *[]ProjectSourceVersion) bool {
					return equal(setOrder(value(prior)), setOrder(value(desired)))
				},
			),
			runtime.EqualBy(
				runtime.InputField(func(input *ProjectResource) **ProjectVpcConfig {
					return &input.VpcConfig
				}),
				func(prior, desired *ProjectVpcConfig) bool {
					return equal(vpc(prior), vpc(desired))
				},
			),
		},
		Validate: func(_ context.Context, input ProjectResource, _ *awscfg.Configuration) error {
			_, err := input.prepare(false)
			return err
		},
		Replace: runtime.Replacement[
			ProjectResource, *ProjectResourceOutput, *awscfg.Configuration,
		]{
			Fields: []runtime.AnyInputField[ProjectResource]{
				runtime.InputField(func(input *ProjectResource) *string { return &input.Name }),
			},
		},
	}
}

// PrepareCreate checks the operation-specific source precondition before replacement deletion.
func (r ProjectResource) PrepareCreate() error {
	_, err := r.prepare(true)
	return err
}

func (r ProjectResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.TimeoutInMinutes, int64(60)),
		defaults.Value(r.QueuedTimeoutInMinutes, int64(480)),
		defaults.NullableValue(r.BadgeEnabled, false),
		defaults.NullableValue(r.ProjectVisibility, "PRIVATE"),
	}
}

func (r ProjectResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(r.SecondaryArtifacts,
			func(a SecondaryProjectArtifacts) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.Expr("input.value != null",
						constraint.Bind("value", a.Type))).
						Message("Type is required"),
					constraint.Must(constraint.Expr("input.value != null",
						constraint.Bind("value", a.ArtifactIdentifier))).
						Message("ArtifactIdentifier is required"),
				}
			}),
		constraint.ForEach(r.SecondarySources, func(s SecondaryProjectSource) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.Expr("input.value != null",
					constraint.Bind("value", s.Type))).
					Message("Type is required"),
				constraint.Must(constraint.Expr("input.value != null",
					constraint.Bind("value", s.SourceIdentifier))).
					Message("SourceIdentifier is required"),
			}
		}),
		constraint.ForEach(r.SecondarySourceVersions,
			func(v ProjectSourceVersion) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.Expr("input.value != null",
						constraint.Bind("value", v.SourceIdentifier))).
						Message("SourceIdentifier is required"),
					constraint.Must(constraint.Expr("input.value != null",
						constraint.Bind("value", v.SourceVersion))).
						Message("SourceVersion is required"),
				}
			}),
		constraint.ForEach(r.Environment.EnvironmentVariables,
			func(v ProjectEnvironmentVariable) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.Expr("input.value != null",
						constraint.Bind("value", v.Name))).
						Message("Name is required"),
					constraint.Must(constraint.Expr("input.value != null",
						constraint.Bind("value", v.Value))).
						Message("Value is required"),
				}
			}),
		constraint.ForEach(r.SecondarySources,
			func(s SecondaryProjectSource) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(s.Auth)).
						Require(constraint.Expr("input.value != null",
							constraint.Bind("value", s.Auth.Type)),
							constraint.Expr("input.value != null",
								constraint.Bind("value", s.Auth.Resource))).
						Message("secondary source auth type and resource are required"),
					constraint.When(constraint.Present(s.GitSubmodulesConfig)).
						Require(constraint.Expr("input.value != null",
							constraint.Bind("value", s.GitSubmodulesConfig.FetchSubmodules))).
						Message("fetch-submodules is required"),
				}
			}),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Name))).
			Message("Name is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Artifacts))).
			Message("Artifacts is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Environment))).
			Message("Environment is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.ServiceRole))).
			Message("ServiceRole is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Source))).
			Message("Source is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Artifacts.Type))).
			Message("Artifacts.Type is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Environment.ComputeType))).
			Message("Environment.ComputeType is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Environment.Image))).
			Message("Environment.Image is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Environment.Type))).
			Message("Environment.Type is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Source.Type))).
			Message("Source.Type is required"),
		constraint.When(constraint.Present(r.BuildBatchConfig)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.BuildBatchConfig.ServiceRole))).
			Message("BuildBatchConfig.ServiceRole is required"),
		constraint.When(constraint.Present(r.Environment.DockerServer)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.Environment.DockerServer.ComputeType))).
			Message("Environment.DockerServer.ComputeType is required"),
		constraint.When(constraint.Present(r.Environment.RegistryCredential)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.Environment.RegistryCredential.Credential))).
			Message("Environment.RegistryCredential.Credential is required"),
		constraint.When(constraint.Present(r.Environment.RegistryCredential)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.Environment.RegistryCredential.CredentialProvider))).
			Message("Environment.RegistryCredential.CredentialProvider is required"),
		constraint.When(constraint.Present(r.Source.Auth)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.Source.Auth.Type))).
			Message("Source.Auth.Type is required"),
		constraint.When(constraint.Present(r.Source.Auth)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.Source.Auth.Resource))).
			Message("Source.Auth.Resource is required"),
		constraint.When(constraint.Present(r.Source.GitSubmodulesConfig)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.Source.GitSubmodulesConfig.FetchSubmodules))).
			Message("Source.GitSubmodulesConfig.FetchSubmodules is required"),
		constraint.When(constraint.Present(r.VpcConfig)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.VpcConfig.VpcId))).
			Message("VpcConfig.VpcId is required"),
		constraint.When(constraint.Present(r.VpcConfig)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.VpcConfig.Subnets))).
			Message("VpcConfig.Subnets is required"),
		constraint.When(constraint.Present(r.VpcConfig)).
			Require(constraint.Expr("input.value != null",
				constraint.Bind("value", r.VpcConfig.SecurityGroupIds))).
			Message("VpcConfig.SecurityGroupIds is required"),
		constraint.Must(constraint.IsMatch(r.Name, `^[0-9A-Za-z][0-9A-Za-z_-]*$`),
			constraint.AtMost(constraint.ByteLength(r.Name), 255)).
			Message("name must start with an alphanumeric and contain at most 255 bytes"),
		constraint.Must(constraint.Expr(
			"@core.byte-length(input.value ?? '') <= 255",
			constraint.Bind("value", r.Description))).
			Message("description must contain at most 255 bytes"),
		constraint.Must(constraint.IsMatch(r.Environment.Certificate, `\.(pem|zip)$`)).
			Message("certificate must end with .pem or .zip"),
		constraint.Must(constraint.AtLeast(r.TimeoutInMinutes, 5),
			constraint.AtMost(r.TimeoutInMinutes, 2160)).
			Message("timeout-in-minutes must be between 5 and 2160"),
		constraint.Must(constraint.AtLeast(r.QueuedTimeoutInMinutes, 5),
			constraint.AtMost(r.QueuedTimeoutInMinutes, 480)).
			Message("queued-timeout-in-minutes must be between 5 and 480"),
		constraint.Must(constraint.AtLeast(r.BuildBatchConfig.TimeoutInMins, 5),
			constraint.AtMost(r.BuildBatchConfig.TimeoutInMins, 2160)).
			Message("batch timeout-in-mins must be between 5 and 2160"),
		constraint.Must(constraint.AtLeast(r.ConcurrentBuildLimit, 1)).
			Message("concurrent-build-limit must be at least 1"),
		constraint.Must(constraint.AtLeast(r.BuildBatchConfig.Restrictions.MaximumBuildsAllowed, 1),
			constraint.AtMost(r.BuildBatchConfig.Restrictions.MaximumBuildsAllowed, 100)).
			Message("maximum-builds-allowed must be between 1 and 100"),
		constraint.Must(constraint.AtLeast(r.Source.GitCloneDepth, 0)).
			Message("git-clone-depth must be at least 0"),
		constraint.Must(constraint.Expr(`
			@core.length({ for item in input.values ?? [] : @core.to-json({
				artifact-identifier: item.artifact-identifier,
				bucket-owner-access: item.bucket-owner-access,
				encryption-disabled: item.encryption-disabled,
				location: item.location, name: item.name, namespace-type: item.namespace-type,
				override-artifact-name: item.override-artifact-name,
				packaging: item.packaging, path: item.path, type: item.type
			}) => true... }) <= 12`, constraint.Bind("values", r.SecondaryArtifacts)),
			constraint.Expr(`
			@core.length({ for item in input.values ?? [] : @core.to-json({
				auth: if item.auth == null then null else {
					resource: item.auth.resource, type: item.auth.type
				},
				build-status-config: if item.build-status-config == null then null else {
					context: item.build-status-config.context,
					target-url: item.build-status-config.target-url
				},
				buildspec: item.buildspec, git-clone-depth: item.git-clone-depth,
				git-submodules-config: if item.git-submodules-config == null then null else {
					fetch-submodules: item.git-submodules-config.fetch-submodules
				},
				insecure-ssl: item.insecure-ssl, location: item.location,
				report-build-status: item.report-build-status,
				source-identifier: item.source-identifier, type: item.type
			}) => true... }) <= 12`, constraint.Bind("values", r.SecondarySources)),
			constraint.Expr(`
			@core.length({ for item in input.values ?? [] : @core.to-json({
				source-identifier: item.source-identifier, source-version: item.source-version
			}) => true... }) <= 12`, constraint.Bind("values", r.SecondarySourceVersions))).
			Message("secondary collections accept at most 12 distinct configurations"),
		constraint.Must(constraint.MaxItems(r.Environment.DockerServer.SecurityGroupIds, 5),
			constraint.Expr(
				"@core.length({ for item in input.values ?? [] : item => true... }) <= 16",
				constraint.Bind("values", r.VpcConfig.Subnets)),
			constraint.Expr(
				"@core.length({ for item in input.values ?? [] : item => true... }) <= 5",
				constraint.Bind("values", r.VpcConfig.SecurityGroupIds))).
			Message("security groups or subnets exceed the collection limit"),
		constraint.When(constraint.Equals(r.Cache.Type, "S3")).
			Require(constraint.NotEmpty(r.Cache.Location)).
			Message("S3 cache requires a nonempty location"),
		constraint.When(constraint.Present(r.ProjectVisibility)).
			Require(constraint.OneOf(r.ProjectVisibility, "PUBLIC_READ", "PRIVATE")).
			Message("project-visibility must be PUBLIC_READ or PRIVATE"),
		constraint.Must(constraint.OneOf(r.Artifacts.Type,
			"CODEPIPELINE",
			"S3",
			"NO_ARTIFACTS")).
			Message("artifact must be a supported value"),
		constraint.When(constraint.Present(r.Artifacts.BucketOwnerAccess)).
			Require(constraint.OneOf(r.Artifacts.BucketOwnerAccess,
				"NONE",
				"READ_ONLY",
				"FULL")).
			Message("bucket must be a supported value"),
		constraint.When(constraint.Present(r.Artifacts.NamespaceType)).
			Require(constraint.OneOf(r.Artifacts.NamespaceType,
				"NONE",
				"BUILD_ID")).
			Message("namespace must be a supported value"),
		constraint.When(constraint.Present(r.Artifacts.Packaging)).
			Require(constraint.OneOf(r.Artifacts.Packaging,
				"NONE",
				"ZIP")).
			Message("packaging must be a supported value"),
		constraint.When(constraint.Present(r.Cache.Type)).
			Require(constraint.OneOf(r.Cache.Type,
				"NO_CACHE",
				"S3",
				"LOCAL")).
			Message("cache must be a supported value"),
		constraint.Must(constraint.OneOf(r.Environment.ComputeType,
			"BUILD_GENERAL1_SMALL",
			"BUILD_GENERAL1_MEDIUM",
			"BUILD_GENERAL1_LARGE",
			"BUILD_GENERAL1_XLARGE",
			"BUILD_GENERAL1_2XLARGE",
			"BUILD_LAMBDA_1GB",
			"BUILD_LAMBDA_2GB",
			"BUILD_LAMBDA_4GB",
			"BUILD_LAMBDA_8GB",
			"BUILD_LAMBDA_10GB",
			"ATTRIBUTE_BASED_COMPUTE",
			"CUSTOM_INSTANCE_TYPE")).
			Message("compute must be a supported value"),
		constraint.Must(constraint.OneOf(r.Environment.Type,
			"WINDOWS_CONTAINER",
			"LINUX_CONTAINER",
			"LINUX_GPU_CONTAINER",
			"ARM_CONTAINER",
			"WINDOWS_SERVER_2019_CONTAINER",
			"WINDOWS_SERVER_2022_CONTAINER",
			"LINUX_LAMBDA_CONTAINER",
			"ARM_LAMBDA_CONTAINER",
			"LINUX_EC2",
			"ARM_EC2",
			"WINDOWS_EC2",
			"MAC_ARM")).
			Message("environment must be a supported value"),
		constraint.When(constraint.Present(r.Environment.DockerServer)).
			Require(constraint.OneOf(r.Environment.DockerServer.ComputeType,
				"BUILD_GENERAL1_SMALL",
				"BUILD_GENERAL1_MEDIUM",
				"BUILD_GENERAL1_LARGE",
				"BUILD_GENERAL1_XLARGE",
				"BUILD_GENERAL1_2XLARGE",
				"BUILD_LAMBDA_1GB",
				"BUILD_LAMBDA_2GB",
				"BUILD_LAMBDA_4GB",
				"BUILD_LAMBDA_8GB",
				"BUILD_LAMBDA_10GB",
				"ATTRIBUTE_BASED_COMPUTE",
				"CUSTOM_INSTANCE_TYPE")).
			Message("compute must be a supported value"),
		constraint.When(constraint.Present(r.Environment.ImagePullCredentialsType)).
			Require(constraint.OneOf(r.Environment.ImagePullCredentialsType,
				"CODEBUILD",
				"SERVICE_ROLE")).
			Message("credentials must be a supported value"),
		constraint.When(constraint.Present(r.Environment.RegistryCredential)).
			Require(constraint.OneOf(r.Environment.RegistryCredential.CredentialProvider,
				"SECRETS_MANAGER")).
			Message("registry must be a supported value"),
		constraint.When(constraint.Present(r.LogsConfig.CloudWatchLogs.Status)).
			Require(constraint.OneOf(r.LogsConfig.CloudWatchLogs.Status,
				"ENABLED",
				"DISABLED")).
			Message("status must be a supported value"),
		constraint.When(constraint.Present(r.LogsConfig.S3Logs.Status)).
			Require(constraint.OneOf(r.LogsConfig.S3Logs.Status,
				"ENABLED",
				"DISABLED")).
			Message("status must be a supported value"),
		constraint.When(constraint.Present(r.LogsConfig.S3Logs.BucketOwnerAccess)).
			Require(constraint.OneOf(r.LogsConfig.S3Logs.BucketOwnerAccess,
				"NONE",
				"READ_ONLY",
				"FULL")).
			Message("bucket must be a supported value"),
		constraint.Must(constraint.OneOf(r.Source.Type,
			"CODECOMMIT",
			"CODEPIPELINE",
			"GITHUB",
			"GITLAB",
			"GITLAB_SELF_MANAGED",
			"S3",
			"BITBUCKET",
			"GITHUB_ENTERPRISE",
			"NO_SOURCE")).
			Message("source must be a supported value"),
		constraint.When(constraint.Present(r.Source.Auth)).
			Require(constraint.OneOf(r.Source.Auth.Type,
				"OAUTH",
				"BASIC_AUTH",
				"PERSONAL_ACCESS_TOKEN",
				"CODECONNECTIONS",
				"SECRETS_MANAGER")).
			Message("auth must be a supported value"),
		constraint.ForEach(r.Cache.Modes,
			func(mode string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(mode,
						"LOCAL_DOCKER_LAYER_CACHE",
						"LOCAL_SOURCE_CACHE",
						"LOCAL_CUSTOM_CACHE")).
						Message("modes must be a supported value"),
				}
			}),
		constraint.ForEach(r.Environment.EnvironmentVariables,
			func(v ProjectEnvironmentVariable) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(v.Type)).
						Require(constraint.OneOf(v.Type,
							"PLAINTEXT",
							"PARAMETER_STORE",
							"SECRETS_MANAGER")).
						Message("variable must be a supported value"),
				}
			}),
		constraint.ForEach(r.FileSystemLocations,
			func(f ProjectFileSystemLocation) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(f.Type)).
						Require(constraint.OneOf(f.Type,
							"EFS")).
						Message("filesystem must be a supported value"),
				}
			}),
		constraint.ForEach(r.SecondaryArtifacts,
			func(a SecondaryProjectArtifacts) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(a.Type,
						"CODEPIPELINE",
						"S3",
						"NO_ARTIFACTS")).
						Message("artifact must be a supported value"),
					constraint.When(constraint.Present(a.BucketOwnerAccess)).
						Require(constraint.OneOf(a.BucketOwnerAccess,
							"NONE",
							"READ_ONLY",
							"FULL")).
						Message("bucket must be a supported value"),
					constraint.When(constraint.Present(a.NamespaceType)).
						Require(constraint.OneOf(a.NamespaceType,
							"NONE",
							"BUILD_ID")).
						Message("namespace must be a supported value"),
					constraint.When(constraint.Present(a.Packaging)).
						Require(constraint.OneOf(a.Packaging,
							"NONE",
							"ZIP")).
						Message("packaging must be a supported value"),
				}
			}),
		constraint.ForEach(r.SecondarySources,
			func(s SecondaryProjectSource) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(s.Type,
						"CODECOMMIT",
						"CODEPIPELINE",
						"GITHUB",
						"GITLAB",
						"GITLAB_SELF_MANAGED",
						"S3",
						"BITBUCKET",
						"GITHUB_ENTERPRISE",
						"NO_SOURCE")).
						Message("source must be a supported value"),
					constraint.When(constraint.Present(s.Auth)).
						Require(constraint.OneOf(s.Auth.Type,
							"OAUTH",
							"BASIC_AUTH",
							"PERSONAL_ACCESS_TOKEN",
							"CODECONNECTIONS",
							"SECRETS_MANAGER")).
						Message("auth must be a supported value"),
					constraint.Must(constraint.AtLeast(s.GitCloneDepth, 0)).
						Message("git-clone-depth must be at least 0"),
				}
			}),
		constraint.ForEach(r.BuildBatchConfig.Restrictions.ComputeTypesAllowed,
			func(compute string) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(compute,
						"BUILD_GENERAL1_SMALL",
						"BUILD_GENERAL1_MEDIUM",
						"BUILD_GENERAL1_LARGE",
						"BUILD_GENERAL1_XLARGE",
						"BUILD_GENERAL1_2XLARGE",
						"BUILD_LAMBDA_1GB",
						"BUILD_LAMBDA_2GB",
						"BUILD_LAMBDA_4GB",
						"BUILD_LAMBDA_8GB",
						"BUILD_LAMBDA_10GB",
						"ATTRIBUTE_BASED_COMPUTE",
						"CUSTOM_INSTANCE_TYPE")).
						Message("batch compute type must be a supported value"),
				}
			}),
	}
}
