package codebuild

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/hashicorp/aws-sdk-go-base/v2/endpoints"
)

type StartBuildAction struct {
	ProjectName                  string                      `ub:"project-name"`
	SourceVersion                *string                     `ub:"source-version"`
	Timeout                      *int64                      `ub:"timeout"`
	EnvironmentVariablesOverride *[]BuildEnvironmentVariable `ub:"environment-variables-override"`
	Region                       *string                     `ub:"region"`
}

type BuildEnvironmentVariable struct {
	Name  string  `ub:"name"`
	Value string  `ub:"value"`
	Type  *string `ub:"type"`
}

type StartBuildActionOutput struct {
	BuildID string `ub:"build-id"`
}

type buildClient interface {
	StartBuild(context.Context, *sdk.StartBuildInput,
		...func(*sdk.Options)) (*sdk.StartBuildOutput, error)
	BatchGetBuilds(context.Context, *sdk.BatchGetBuildsInput,
		...func(*sdk.Options)) (*sdk.BatchGetBuildsOutput, error)
}

type buildClock struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func (r StartBuildAction) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Expr(
			"input.project-name != null", constraint.Bind("project-name", r.ProjectName),
		)).Message("project-name is required"),
		constraint.ForEach(r.EnvironmentVariablesOverride,
			func(v BuildEnvironmentVariable) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.Expr(
						"input.name != null", constraint.Bind("name", v.Name),
					)).Message("environment variable name is required"),
					constraint.Must(constraint.Expr(
						"input.value != null", constraint.Bind("value", v.Value),
					)).Message("environment variable value is required"),
				}
			}),
	}
}

func (r *StartBuildAction) Run(
	ctx context.Context, cfg *awscfg.Configuration,
) (*StartBuildActionOutput, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if region := aws.ToString(r.Region); region != "" {
		if loaded.Region != "" && buildRegionPartition(region) !=
			buildRegionPartition(loaded.Region) {
			return nil, fmt.Errorf("build region %q is outside configured partition %q",
				region, buildRegionPartition(loaded.Region))
		}
		loaded.Region = region
	}
	return r.runBuild(ctx, sdk.NewFromConfig(loaded), buildClock{
		now: time.Now, sleep: sleepForBuild,
	})
}

func (r StartBuildAction) prepareBuildRequest() (*sdk.StartBuildInput, time.Duration) {
	in := &sdk.StartBuildInput{
		ProjectName: aws.String(r.ProjectName), SourceVersion: r.SourceVersion,
	}
	if r.EnvironmentVariablesOverride != nil {
		in.EnvironmentVariablesOverride = make([]types.EnvironmentVariable,
			len(*r.EnvironmentVariablesOverride))
		for i, variable := range *r.EnvironmentVariablesOverride {
			in.EnvironmentVariablesOverride[i] = types.EnvironmentVariable{
				Name: aws.String(variable.Name), Value: aws.String(variable.Value),
				Type: types.EnvironmentVariableType(aws.ToString(variable.Type)),
			}
		}
	}
	timeout := 30 * time.Minute
	if r.Timeout != nil {
		timeout = time.Duration(*r.Timeout) * time.Second
	}
	return in, timeout
}

func (r StartBuildAction) runBuild(
	ctx context.Context, client buildClient, clock buildClock,
) (*StartBuildActionOutput, error) {
	in, timeout := r.prepareBuildRequest()
	out, err := client.StartBuild(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("starting build: %w", err)
	}
	if out == nil || out.Build == nil || aws.ToString(out.Build.Id) == "" {
		return nil, fmt.Errorf("starting build: response has no build ID")
	}
	id := *out.Build.Id
	if timeout <= 0 {
		return nil, fmt.Errorf("waiting for build %s: timeout must be positive", id)
	}
	if err := waitForBuild(ctx, client, id, timeout, clock); err != nil {
		return nil, err
	}
	return &StartBuildActionOutput{BuildID: id}, nil
}

func waitForBuild(
	ctx context.Context, client buildClient, id string, timeout time.Duration, clock buildClock,
) error {
	deadline := clock.now().Add(timeout)
	var delay time.Duration
	var lastProgress time.Time
	nextInterval := func(attempt int) time.Duration {
		if attempt == 0 {
			return 0
		}
		if delay == 0 {
			delay = 100 * time.Millisecond
		}
		delay = min(max(2*delay, 500*time.Millisecond), 10*time.Second)
		return delay
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for build %s: %w", id, err)
		}
		if clock.now().After(deadline) {
			return fmt.Errorf("build timeout waiting for %s", id)
		}
		out, err := client.BatchGetBuilds(ctx, &sdk.BatchGetBuildsInput{Ids: []string{id}})
		if err != nil {
			return fmt.Errorf("waiting for build %s: %w", id, err)
		}
		if out == nil {
			return fmt.Errorf("waiting for build %s: response has no builds", id)
		}
		if len(out.Builds) == 0 {
			return fmt.Errorf("waiting for build %s: build not found in BatchGetBuilds response", id)
		}
		status := out.Builds[0].BuildStatus
		switch status {
		case types.StatusTypeSucceeded:
			return nil
		case types.StatusTypeInProgress:
		case types.StatusTypeFailed, types.StatusTypeFault, types.StatusTypeStopped,
			types.StatusTypeTimedOut:
			return fmt.Errorf("build failed with status %s: %s", status, id)
		default:
			return fmt.Errorf("unexpected build status %q for %s; allowed SUCCEEDED, IN_PROGRESS",
				status, id)
		}
		if now := clock.now(); attempt == 0 || now.Sub(lastProgress) >= 2*time.Minute {
			// Progress metadata advances the same delay used for polling.
			nextInterval(attempt)
			lastProgress = now
		}
		if err := clock.sleep(ctx, nextInterval(attempt)); err != nil {
			return fmt.Errorf("waiting for build %s: %w", id, err)
		}
	}
}

func sleepForBuild(ctx context.Context, duration time.Duration) error {
	if duration == 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func buildRegionPartition(region string) string {
	partition, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), region)
	if ok {
		return partition.ID()
	}
	return "aws"
}
