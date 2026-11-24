package codebuild

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
)

var (
	arnPartition = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	arnRegion    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	arnAccount   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|\d{12}|cw.{10})$`)
	logLocation = regexp.MustCompile(`^[0-9a-z][^/]*\/(.+)$`)
)

func parseARN(value string) error {
	if value == "" {
		return nil
	}
	a, err := awsarn.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid ARN %q: %w", value, err)
	}
	if !arnPartition.MatchString(a.Partition) ||
		(a.Region != "" && !arnRegion.MatchString(a.Region)) ||
		(a.AccountID != "" && !arnAccount.MatchString(a.AccountID)) || a.Resource == "" {
		return fmt.Errorf("invalid ARN %q", value)
	}
	return nil
}

func optional(value *string) *string {
	if value == nil || *value == "" {
		return nil
	}
	return value
}

func value[T any](p *T) (result T) {
	if p != nil {
		return *p
	}
	return result
}

func fallback(p *string, defaultValue string) string {
	if p == nil {
		return defaultValue
	}
	return *p
}

func integer(p *int64) *int32 {
	if p == nil {
		return nil
	}
	return aws.Int32(int32(*p))
}

func nonzero(p *int64) *int32 {
	if value(p) == 0 {
		return nil
	}
	return integer(p)
}

func (r ProjectResource) prepare(create bool) (*sdk.CreateProjectInput, error) {
	if create && r.Source.Type == "NO_SOURCE" {
		if value(r.Source.Buildspec) == "" {
			return nil, fmt.Errorf("NO_SOURCE requires a nonempty buildspec when creating a project")
		}
		if value(r.Source.Location) != "" {
			return nil, fmt.Errorf("NO_SOURCE requires an empty location when creating a project")
		}
	}
	arns := []string{r.ServiceRole, value(r.ResourceAccessRole)}
	if r.BuildBatchConfig != nil {
		arns = append(arns, r.BuildBatchConfig.ServiceRole)
	}
	if r.Environment.Fleet != nil {
		arns = append(arns, value(r.Environment.Fleet.FleetArn))
	}
	if r.Source.Auth != nil {
		arns = append(arns, r.Source.Auth.Resource)
	}
	for _, source := range value(r.SecondarySources) {
		if source.Auth != nil {
			arns = append(arns, source.Auth.Resource)
		}
	}
	for _, arn := range arns {
		if err := parseARN(arn); err != nil {
			return nil, err
		}
	}
	if r.LogsConfig != nil && r.LogsConfig.S3Logs != nil {
		location := value(r.LogsConfig.S3Logs.Location)
		if err := parseARN(location); err != nil && !logLocation.MatchString(location) {
			return nil, fmt.Errorf("invalid S3 log location %q", location)
		}
	}
	in := &sdk.CreateProjectInput{
		Name: aws.String(r.Name), ServiceRole: aws.String(r.ServiceRole),
		Artifacts: artifact(r.Artifacts), Environment: environment(r.Environment),
		Source: source(r.Source), AutoRetryLimit: nonzero(r.AutoRetryLimit),
		BuildBatchConfig: batch(r.BuildBatchConfig), Cache: cache(r.Cache),
		ConcurrentBuildLimit: integer(r.ConcurrentBuildLimit), Description: optional(r.Description),
		EncryptionKey: optional(r.EncryptionKey), LogsConfig: logs(r.LogsConfig),
		QueuedTimeoutInMinutes: aws.Int32(int32(r.QueuedTimeoutInMinutes)),
		TimeoutInMinutes:       aws.Int32(int32(r.TimeoutInMinutes)),
		SourceVersion:          optional(r.SourceVersion), VpcConfig: vpc(r.VpcConfig),
		Tags: projectTags(value(r.Tags)),
	}
	if create {
		in.ServiceRole = optional(in.ServiceRole)
	}
	if value(r.BadgeEnabled) {
		in.BadgeEnabled = aws.Bool(true)
	}
	for _, item := range uniqueSet(value(r.SecondaryArtifacts), artifactIdentity) {
		in.SecondaryArtifacts = append(in.SecondaryArtifacts, *secondaryArtifact(item))
	}
	for _, item := range uniqueSet(value(r.SecondarySources), sourceIdentity) {
		in.SecondarySources = append(in.SecondarySources, *secondarySource(item))
	}
	for _, item := range setOrder(value(r.SecondarySourceVersions)) {
		in.SecondarySourceVersions = append(in.SecondarySourceVersions, types.ProjectSourceVersion{
			SourceIdentifier: aws.String(item.SourceIdentifier),
			SourceVersion:    aws.String(item.SourceVersion),
		})
	}
	for _, item := range uniqueSet(value(r.FileSystemLocations), filesystemIdentity) {
		in.FileSystemLocations = append(in.FileSystemLocations, types.ProjectFileSystemLocation{
			Type:       types.FileSystemType(fallback(item.Type, "EFS")),
			Identifier: optional(item.Identifier), Location: optional(item.Location),
			MountOptions: optional(item.MountOptions), MountPoint: optional(item.MountPoint),
		})
	}
	return in, nil
}

func artifact(a ProjectArtifacts) *types.ProjectArtifacts {
	out := &types.ProjectArtifacts{
		Type: types.ArtifactsType(a.Type), ArtifactIdentifier: optional(a.ArtifactIdentifier),
		BucketOwnerAccess: types.BucketOwnerAccess(value(a.BucketOwnerAccess)),
		Location:          optional(a.Location), Name: optional(a.Name),
		NamespaceType:        types.ArtifactNamespace(value(a.NamespaceType)),
		OverrideArtifactName: aws.Bool(value(a.OverrideArtifactName)),
		Packaging:            types.ArtifactPackaging(value(a.Packaging)), Path: optional(a.Path),
	}
	if a.Type == "S3" || a.Type == "CODEPIPELINE" {
		out.EncryptionDisabled = aws.Bool(value(a.EncryptionDisabled))
	}
	return out
}

func secondaryArtifact(a SecondaryProjectArtifacts) *types.ProjectArtifacts {
	return artifact(ProjectArtifacts{
		Type: a.Type, ArtifactIdentifier: aws.String(a.ArtifactIdentifier),
		BucketOwnerAccess: a.BucketOwnerAccess, EncryptionDisabled: a.EncryptionDisabled,
		Location: a.Location, Name: a.Name,
		NamespaceType:        aws.String(fallback(a.NamespaceType, "NONE")),
		OverrideArtifactName: a.OverrideArtifactName,
		Packaging:            aws.String(fallback(a.Packaging, "NONE")), Path: a.Path,
	})
}

func cache(c *ProjectCache) *types.ProjectCache {
	if c == nil {
		return nil
	}
	out := &types.ProjectCache{
		Type:     types.CacheType(fallback(c.Type, "NO_CACHE")),
		Location: aws.String(value(c.Location)), CacheNamespace: optional(c.CacheNamespace),
	}
	if out.Type == types.CacheTypeLocal {
		for _, mode := range value(c.Modes) {
			out.Modes = append(out.Modes, types.CacheMode(mode))
		}
	}
	return out
}

func environment(e ProjectEnvironment) *types.ProjectEnvironment {
	out := &types.ProjectEnvironment{
		ComputeType: types.ComputeType(e.ComputeType), Image: optional(aws.String(e.Image)),
		Type: types.EnvironmentType(e.Type), Certificate: optional(e.Certificate),
		ImagePullCredentialsType: types.ImagePullCredentialsType(
			fallback(e.ImagePullCredentialsType, "CODEBUILD")),
		PrivilegedMode: aws.Bool(value(e.PrivilegedMode)),
	}
	if e.DockerServer != nil {
		out.DockerServer = &types.DockerServer{
			ComputeType:      types.ComputeType(e.DockerServer.ComputeType),
			SecurityGroupIds: slices.Clone(value(e.DockerServer.SecurityGroupIds)),
		}
	}
	if e.Fleet != nil {
		out.Fleet = &types.ProjectFleet{FleetArn: optional(e.Fleet.FleetArn)}
	}
	if e.RegistryCredential != nil {
		out.RegistryCredential = &types.RegistryCredential{
			Credential:         optional(aws.String(e.RegistryCredential.Credential)),
			CredentialProvider: types.CredentialProviderType(e.RegistryCredential.CredentialProvider),
		}
	}
	for _, v := range value(e.EnvironmentVariables) {
		out.EnvironmentVariables = append(out.EnvironmentVariables, types.EnvironmentVariable{
			Name: optional(aws.String(v.Name)), Value: aws.String(v.Value),
			Type: types.EnvironmentVariableType(fallback(v.Type, "PLAINTEXT")),
		})
	}
	return out
}

func source(s ProjectSource) *types.ProjectSource {
	out := &types.ProjectSource{
		Type: types.SourceType(s.Type), Buildspec: aws.String(value(s.Buildspec)),
		GitCloneDepth: aws.Int32(int32(value(s.GitCloneDepth))),
		InsecureSsl:   aws.Bool(value(s.InsecureSsl)), Location: optional(s.Location),
	}
	if s.Auth != nil {
		out.Auth = &types.SourceAuth{
			Type: types.SourceAuthType(s.Auth.Type), Resource: optional(aws.String(s.Auth.Resource)),
		}
	}
	switch s.Type {
	case "BITBUCKET", "GITHUB", "GITHUB_ENTERPRISE", "GITLAB", "GITLAB_SELF_MANAGED":
		out.ReportBuildStatus = aws.Bool(value(s.ReportBuildStatus))
		if s.BuildStatusConfig != nil {
			out.BuildStatusConfig = &types.BuildStatusConfig{
				Context:   optional(s.BuildStatusConfig.Context),
				TargetUrl: optional(s.BuildStatusConfig.TargetUrl),
			}
		}
	}
	switch s.Type {
	case "BITBUCKET", "CODECOMMIT", "GITHUB", "GITHUB_ENTERPRISE":
		if s.GitSubmodulesConfig != nil {
			out.GitSubmodulesConfig = &types.GitSubmodulesConfig{
				FetchSubmodules: aws.Bool(s.GitSubmodulesConfig.FetchSubmodules),
			}
		}
	}
	return out
}

func secondarySource(s SecondaryProjectSource) *types.ProjectSource {
	out := source(ProjectSource{
		Type: s.Type, Auth: s.Auth, BuildStatusConfig: s.BuildStatusConfig, Buildspec: s.Buildspec,
		GitCloneDepth: s.GitCloneDepth, GitSubmodulesConfig: s.GitSubmodulesConfig,
		InsecureSsl: s.InsecureSsl, Location: s.Location, ReportBuildStatus: s.ReportBuildStatus,
	})
	out.SourceIdentifier = optional(aws.String(s.SourceIdentifier))
	return out
}

func logs(l *ProjectLogsConfig) *types.LogsConfig {
	out := &types.LogsConfig{
		CloudWatchLogs: &types.CloudWatchLogsConfig{Status: types.LogsConfigStatusTypeEnabled},
		S3Logs:         &types.S3LogsConfig{Status: types.LogsConfigStatusTypeDisabled},
	}
	if l == nil {
		return out
	}
	if l.CloudWatchLogs != nil {
		c := l.CloudWatchLogs
		out.CloudWatchLogs = &types.CloudWatchLogsConfig{
			GroupName: optional(c.GroupName), StreamName: optional(c.StreamName),
			Status: types.LogsConfigStatusType(fallback(c.Status, "ENABLED")),
		}
	}
	if l.S3Logs != nil {
		s := l.S3Logs
		out.S3Logs = &types.S3LogsConfig{
			BucketOwnerAccess:  types.BucketOwnerAccess(value(s.BucketOwnerAccess)),
			EncryptionDisabled: aws.Bool(value(s.EncryptionDisabled)), Location: optional(s.Location),
			Status: types.LogsConfigStatusType(fallback(s.Status, "DISABLED")),
		}
	}
	return out
}

func batch(b *ProjectBuildBatchConfig) *types.ProjectBuildBatchConfig {
	if b == nil {
		return nil
	}
	out := &types.ProjectBuildBatchConfig{
		ServiceRole: aws.String(b.ServiceRole), CombineArtifacts: aws.Bool(value(b.CombineArtifacts)),
		TimeoutInMins: nonzero(b.TimeoutInMins),
	}
	if b.Restrictions != nil {
		out.Restrictions = &types.BatchRestrictions{
			MaximumBuildsAllowed: nonzero(b.Restrictions.MaximumBuildsAllowed),
		}
		if len(value(b.Restrictions.ComputeTypesAllowed)) > 0 {
			out.Restrictions.ComputeTypesAllowed = slices.Clone(value(b.Restrictions.ComputeTypesAllowed))
		}
	}
	return out
}

func vpc(v *ProjectVpcConfig) *types.VpcConfig {
	if v == nil {
		return nil
	}
	return &types.VpcConfig{
		VpcId: aws.String(v.VpcId), Subnets: setOrder(v.Subnets),
		SecurityGroupIds: setOrder(v.SecurityGroupIds),
	}
}

func projectTags(tags map[string]string) []types.Tag {
	out := []types.Tag{}
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		if !strings.HasPrefix(k, "aws:") {
			out = append(out, types.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
		}
	}
	return out
}
