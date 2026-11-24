package codebuildbuild

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
)

type CleanupResource struct {
	ProjectName string `ub:"project-name"`
	ProjectArn  string `ub:"project-arn"`
}

type CleanupOutput struct {
	ProjectName string `ub:"project-name"`
}

type BuildCheckDataSource struct {
	ProjectName string `ub:"project-name"`
	BuildID     string `ub:"build-id"`
	Variable    string `ub:"variable"`
}

type BuildCheckOutput struct {
	Verified bool `ub:"verified"`
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "build-fixture", Configuration: config.LibraryConfiguration(),
		Compatibility: runtime.LibraryCompatibility{
			RequiredAPI: "1.1", SuggestedUnobinVersion: "v0.13.0",
		},
		Resources: map[string]runtime.ResourceRegistration{
			"cleanup": runtime.MakeResource[
				CleanupResource, *CleanupOutput, *awscfg.Configuration,
			](runtime.ResourceDefinition[CleanupResource, *CleanupOutput, *awscfg.Configuration]{
				SchemaVersion: 1,
			}),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"build-check": runtime.MakeDataSource[
				BuildCheckDataSource, *BuildCheckOutput, *awscfg.Configuration,
			](),
		},
	}
}

func (r *CleanupResource) Create(context.Context, *awscfg.Configuration) (*CleanupOutput, error) {
	return &CleanupOutput{ProjectName: r.ProjectName}, nil
}

func (r *CleanupResource) Read(
	_ context.Context, _ *awscfg.Configuration,
	prior runtime.Prior[CleanupResource, *CleanupOutput, *awscfg.Configuration],
) (*CleanupOutput, error) {
	return prior.Outputs, nil
}

func (r *CleanupResource) Update(
	ctx context.Context, cfg *awscfg.Configuration,
	_ runtime.Prior[CleanupResource, *CleanupOutput, *awscfg.Configuration],
) (*CleanupOutput, error) {
	return r.Create(ctx, cfg)
}

func (r *CleanupResource) Delete(
	ctx context.Context, cfg *awscfg.Configuration,
	prior runtime.Prior[CleanupResource, *CleanupOutput, *awscfg.Configuration],
) error {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return stopOwnedBuilds(ctx, sdk.NewFromConfig(loaded), prior.Inputs.ProjectName)
}

func (r *BuildCheckDataSource) Read(
	ctx context.Context, cfg *awscfg.Configuration,
) (*BuildCheckOutput, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := sdk.NewFromConfig(loaded).BatchGetBuilds(ctx,
		&sdk.BatchGetBuildsInput{Ids: []string{r.BuildID}})
	if err != nil {
		return nil, err
	}
	if out == nil || len(out.Builds) != 1 {
		return nil, fmt.Errorf("build check: expected one completed build")
	}
	build := out.Builds[0]
	if aws.ToString(build.Id) != r.BuildID || aws.ToString(build.ProjectName) != r.ProjectName ||
		build.BuildStatus != types.StatusTypeSucceeded || build.Environment == nil {
		return nil, fmt.Errorf("build check: identity or status differs from action")
	}
	vars := build.Environment.EnvironmentVariables
	if len(vars) != 1 || aws.ToString(vars[0].Name) != "RUN" ||
		aws.ToString(vars[0].Value) != r.Variable || vars[0].Type != "PLAINTEXT" {
		return nil, fmt.Errorf("build check: environment overrides differ from request")
	}
	return &BuildCheckOutput{Verified: true}, nil
}

func stopOwnedBuilds(ctx context.Context, client *sdk.Client, project string) error {
	request := &sdk.ListBuildsForProjectInput{ProjectName: aws.String(project)}
	var ids []string
	for {
		page, err := client.ListBuildsForProject(ctx, request)
		if err != nil {
			return fmt.Errorf("list owned builds: %w", err)
		}
		if page == nil {
			return fmt.Errorf("list owned builds: missing response")
		}
		ids = append(ids, page.Ids...)
		if aws.ToString(page.NextToken) == "" {
			break
		}
		request.NextToken = page.NextToken
	}
	var failures []error
	for _, id := range ids {
		out, err := client.BatchGetBuilds(ctx, &sdk.BatchGetBuildsInput{Ids: []string{id}})
		if err != nil {
			failures = append(failures, fmt.Errorf("get owned build: %w", err))
			continue
		}
		if out == nil || len(out.Builds) == 0 {
			continue
		}
		build := out.Builds[0]
		if aws.ToString(build.ProjectName) != project || aws.ToString(build.Id) != id ||
			build.BuildStatus != types.StatusTypeInProgress {
			continue
		}
		if _, err := client.StopBuild(ctx, &sdk.StopBuildInput{Id: aws.String(id)}); err != nil {
			failures = append(failures, fmt.Errorf("stop owned build: %w", err))
			continue
		}
		if err := awaitStopped(ctx, client, id); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func awaitStopped(ctx context.Context, client *sdk.Client, id string) error {
	for {
		out, err := client.BatchGetBuilds(ctx, &sdk.BatchGetBuildsInput{Ids: []string{id}})
		if err != nil {
			return fmt.Errorf("get owned build: %w", err)
		}
		if out == nil || len(out.Builds) == 0 {
			return nil
		}
		if out.Builds[0].BuildStatus != types.StatusTypeInProgress {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
