package codebuild

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
)

func projectMap(v any) map[string]any {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		panic(err)
	}
	return result
}

func canonical(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		panic(err)
	}
	data, err = json.Marshal(normalizeCollections(decoded))
	if err != nil {
		panic(err)
	}
	return data
}

func normalizeCollections(v any) any {
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			return nil
		}
		for i := range x {
			x[i] = normalizeCollections(x[i])
		}
	case map[string]any:
		for key, item := range x {
			x[key] = normalizeCollections(item)
		}
	}
	return v
}

func equal(a, b any) bool { return bytes.Equal(canonical(a), canonical(b)) }

func setOrder[T any](items []T) []T {
	items = slices.Clone(items)
	slices.SortFunc(items, func(a, b T) int { return bytes.Compare(canonical(a), canonical(b)) })
	return slices.CompactFunc(items, func(a, b T) bool { return equal(a, b) })
}

func uniqueSet[T any](items []T, key func(T) any) []T {
	first := map[string]T{}
	for _, item := range items {
		k := string(canonical(key(item)))
		if _, exists := first[k]; !exists {
			first[k] = item
		}
	}
	keys := make([]string, 0, len(first))
	for key := range first {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]T, 0, len(keys))
	for _, key := range keys {
		out = append(out, first[key])
	}
	return out
}

func artifactIdentity(a SecondaryProjectArtifacts) any {
	a.Name = nil
	a.BucketOwnerAccess = aws.String(value(a.BucketOwnerAccess))
	a.EncryptionDisabled = aws.Bool(value(a.EncryptionDisabled))
	a.Location, a.Path = aws.String(value(a.Location)), aws.String(value(a.Path))
	a.NamespaceType = aws.String(fallback(a.NamespaceType, "NONE"))
	a.Packaging = aws.String(fallback(a.Packaging, "NONE"))
	a.OverrideArtifactName = aws.Bool(value(a.OverrideArtifactName))
	return a
}

func sourceIdentity(s SecondaryProjectSource) any {
	s.Buildspec, s.Location = aws.String(value(s.Buildspec)), aws.String(value(s.Location))
	s.GitCloneDepth = aws.Int64(value(s.GitCloneDepth))
	s.InsecureSsl = aws.Bool(value(s.InsecureSsl))
	s.ReportBuildStatus = aws.Bool(value(s.ReportBuildStatus))
	if s.BuildStatusConfig != nil {
		s.BuildStatusConfig = &ProjectBuildStatusConfig{
			Context:   aws.String(value(s.BuildStatusConfig.Context)),
			TargetUrl: aws.String(value(s.BuildStatusConfig.TargetUrl)),
		}
	}
	return s
}

func filesystemIdentity(f ProjectFileSystemLocation) any {
	f.Identifier, f.Location = aws.String(value(f.Identifier)), aws.String(value(f.Location))
	f.MountOptions, f.MountPoint = aws.String(value(f.MountOptions)), aws.String(value(f.MountPoint))
	f.Type = aws.String(fallback(f.Type, "EFS"))
	return f
}

func artifactSet(items *[]SecondaryProjectArtifacts) any {
	keys := []any{}
	for _, item := range uniqueSet(value(items), artifactIdentity) {
		keys = append(keys, artifactIdentity(item))
	}
	return keys
}

func sourceSet(items *[]SecondaryProjectSource) any {
	keys := []any{}
	for _, item := range uniqueSet(value(items), sourceIdentity) {
		keys = append(keys, sourceIdentity(item))
	}
	return keys
}

func filesystemSet(items *[]ProjectFileSystemLocation) any {
	keys := []any{}
	for _, item := range uniqueSet(value(items), filesystemIdentity) {
		keys = append(keys, filesystemIdentity(item))
	}
	return keys
}

func outputRequest(o *ProjectResourceOutput) *sdk.CreateProjectInput {
	if o == nil {
		return &sdk.CreateProjectInput{}
	}
	in := &sdk.CreateProjectInput{
		Name: aws.String(o.Name), Environment: environment(o.Environment),
		ServiceRole: aws.String(o.ServiceRole), AutoRetryLimit: integer(o.AutoRetryLimit),
		BadgeEnabled: aws.Bool(o.Badge.BadgeEnabled), BuildBatchConfig: batch(o.BuildBatchConfig),
		Cache: cache(o.Cache), ConcurrentBuildLimit: integer(o.ConcurrentBuildLimit),
		Description: aws.String(value(o.Description)), EncryptionKey: aws.String(value(o.EncryptionKey)),
		LogsConfig:             logs(o.LogsConfig),
		QueuedTimeoutInMinutes: aws.Int32(int32(o.QueuedTimeoutInMinutes)),
		SourceVersion:          aws.String(value(o.SourceVersion)), Tags: projectTags(o.Tags),
		TimeoutInMinutes: aws.Int32(int32(o.TimeoutInMinutes)), VpcConfig: vpc(o.VpcConfig),
	}
	if o.Artifacts != nil {
		in.Artifacts = artifact(*o.Artifacts)
	}
	if o.Source != nil {
		in.Source = source(*o.Source)
	}
	for _, a := range value(o.SecondaryArtifacts) {
		in.SecondaryArtifacts = append(in.SecondaryArtifacts, *secondaryArtifact(a))
	}
	for _, s := range value(o.SecondarySources) {
		in.SecondarySources = append(in.SecondarySources, *secondarySource(s))
	}
	for _, s := range value(o.SecondarySourceVersions) {
		in.SecondarySourceVersions = append(in.SecondarySourceVersions, types.ProjectSourceVersion{
			SourceIdentifier: aws.String(s.SourceIdentifier), SourceVersion: aws.String(s.SourceVersion),
		})
	}
	for _, f := range value(o.FileSystemLocations) {
		in.FileSystemLocations = append(in.FileSystemLocations, types.ProjectFileSystemLocation{
			Type: types.FileSystemType(fallback(f.Type, "EFS")), Identifier: optional(f.Identifier),
			Location: optional(f.Location), MountPoint: optional(f.MountPoint),
			MountOptions: optional(f.MountOptions),
		})
	}
	return in
}

func normalizeRequest(in *sdk.CreateProjectInput) {
	in.SecondaryArtifacts = setOrder(in.SecondaryArtifacts)
	slices.SortFunc(in.SecondarySources, func(a, b types.ProjectSource) int {
		return bytes.Compare(canonical(a), canonical(b))
	})
	in.SecondarySourceVersions = setOrder(in.SecondarySourceVersions)
	in.FileSystemLocations = setOrder(in.FileSystemLocations)
	if in.VpcConfig != nil {
		in.VpcConfig.Subnets = setOrder(in.VpcConfig.Subnets)
		in.VpcConfig.SecurityGroupIds = setOrder(in.VpcConfig.SecurityGroupIds)
	}
}

func (r ProjectResource) patch(
	in *sdk.CreateProjectInput, before ProjectResource, current, identity *ProjectResourceOutput,
) (*sdk.UpdateProjectInput, bool, error) {
	if _, err := projectIdentity(identity); err != nil {
		return nil, false, err
	}
	if current == nil {
		return nil, false, fmt.Errorf("update project: missing current observation")
	}
	observed := outputRequest(current)
	normalizeRequest(in)
	normalizeRequest(observed)
	if in.Artifacts != nil && observed.Artifacts != nil {
		if value(r.Artifacts.Name) == "" && aws.ToString(observed.Artifacts.Name) == current.Name {
			observed.Artifacts.Name = nil
		}
		if r.Artifacts.Type == "S3" {
			if r.Artifacts.NamespaceType == nil {
				in.Artifacts.NamespaceType = "NONE"
			}
			if r.Artifacts.Packaging == nil {
				in.Artifacts.Packaging = "NONE"
			}
		}
		if r.Artifacts.Type == "CODEPIPELINE" && r.Artifacts.Packaging == nil {
			observed.Artifacts.Packaging = ""
		}
	}
	desired, remote := projectMap(in), projectMap(observed)
	remote["AutoRetryLimit"] = int32(value(current.AutoRetryLimit))
	desired["BadgeEnabled"] = value(r.BadgeEnabled)
	desired["AutoRetryLimit"] = int32(value(r.AutoRetryLimit))
	desired["Description"] = value(r.Description)
	desired["EncryptionKey"] = value(r.EncryptionKey)
	desired["SourceVersion"] = value(r.SourceVersion)
	if int32(value(r.ConcurrentBuildLimit)) == 0 {
		desired["ConcurrentBuildLimit"] = -1
	}
	if r.Cache == nil {
		desired["Cache"] = &types.ProjectCache{Type: "NO_CACHE"}
	}
	if r.BuildBatchConfig == nil {
		desired["BuildBatchConfig"] = &types.ProjectBuildBatchConfig{}
	}
	if r.VpcConfig == nil {
		desired["VpcConfig"] = &types.VpcConfig{}
	}
	for _, key := range []string{"FileSystemLocations", "SecondaryArtifacts",
		"SecondarySources", "SecondarySourceVersions"} {
		if desired[key] == nil {
			desired[key] = []any{}
		}
	}
	managed := map[string]bool{
		"Artifacts": true, "Environment": true, "ServiceRole": true, "Source": true,
		"BadgeEnabled": true, "Tags": true, "TimeoutInMinutes": true,
		"QueuedTimeoutInMinutes": true,
		"AutoRetryLimit":         r.AutoRetryLimit != nil || before.AutoRetryLimit != nil,
		"Description":            r.Description != nil || before.Description != nil,
		"EncryptionKey":          r.EncryptionKey != nil || before.EncryptionKey != nil,
		"SourceVersion":          r.SourceVersion != nil || before.SourceVersion != nil,
		"ConcurrentBuildLimit":   r.ConcurrentBuildLimit != nil || before.ConcurrentBuildLimit != nil,
		"Cache":                  r.Cache != nil || before.Cache != nil,
		"BuildBatchConfig":       r.BuildBatchConfig != nil || before.BuildBatchConfig != nil,
		"VpcConfig":              r.VpcConfig != nil || before.VpcConfig != nil,
		"LogsConfig":             r.LogsConfig != nil || before.LogsConfig != nil,
		"FileSystemLocations":    r.FileSystemLocations != nil || before.FileSystemLocations != nil,
		"SecondaryArtifacts":     r.SecondaryArtifacts != nil || before.SecondaryArtifacts != nil,
		"SecondarySources":       r.SecondarySources != nil || before.SecondarySources != nil,
		"SecondarySourceVersions": r.SecondarySourceVersions != nil ||
			before.SecondarySourceVersions != nil,
	}
	if r.Environment.Type == "LINUX_LAMBDA_CONTAINER" || r.Environment.Type == "ARM_LAMBDA_CONTAINER" {
		managed["TimeoutInMinutes"], managed["QueuedTimeoutInMinutes"] = false, false
	}
	changed := map[string]any{}
	for key, yes := range managed {
		if yes && !equal(desired[key], remote[key]) {
			changed[key] = desired[key]
		}
	}
	if managed["SecondaryArtifacts"] {
		delete(changed, "SecondaryArtifacts")
		if !equal(artifactSet(r.SecondaryArtifacts), artifactSet(current.SecondaryArtifacts)) ||
			!equal(artifactSet(before.SecondaryArtifacts), artifactSet(r.SecondaryArtifacts)) {
			changed["SecondaryArtifacts"] = desired["SecondaryArtifacts"]
		}
	}
	if managed["SecondarySources"] &&
		!equal(sourceSet(before.SecondarySources), sourceSet(r.SecondarySources)) {
		changed["SecondarySources"] = desired["SecondarySources"]
	}
	visible := fallback(r.ProjectVisibility, "PRIVATE") != current.ProjectVisibility ||
		((r.ResourceAccessRole != nil || before.ResourceAccessRole != nil) &&
			value(r.ResourceAccessRole) != value(current.ResourceAccessRole))
	if len(changed) == 0 {
		return nil, visible, nil
	}
	changed["Name"] = identity.Name
	changed["Tags"] = projectTags(value(r.Tags))
	data, err := json.Marshal(changed)
	if err != nil {
		return nil, false, err
	}
	var patch sdk.UpdateProjectInput
	if err := json.Unmarshal(data, &patch); err != nil {
		return nil, false, err
	}
	return &patch, visible, nil
}
