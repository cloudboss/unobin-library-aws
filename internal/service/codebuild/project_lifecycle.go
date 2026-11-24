package codebuild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type projectClient interface {
	CreateProject(context.Context, *sdk.CreateProjectInput,
		...func(*sdk.Options)) (*sdk.CreateProjectOutput, error)
	BatchGetProjects(context.Context, *sdk.BatchGetProjectsInput,
		...func(*sdk.Options)) (*sdk.BatchGetProjectsOutput, error)
	UpdateProject(context.Context, *sdk.UpdateProjectInput,
		...func(*sdk.Options)) (*sdk.UpdateProjectOutput, error)
	UpdateProjectVisibility(context.Context, *sdk.UpdateProjectVisibilityInput,
		...func(*sdk.Options)) (*sdk.UpdateProjectVisibilityOutput, error)
	DeleteProject(context.Context, *sdk.DeleteProjectInput,
		...func(*sdk.Options)) (*sdk.DeleteProjectOutput, error)
}

func newClient(ctx context.Context, cfg *awscfg.Configuration) (*sdk.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return sdk.NewFromConfig(loaded), nil
}

func (r *ProjectResource) Create(
	ctx context.Context, cfg *awscfg.Configuration,
) (*ProjectResourceOutput, error) {
	in, err := r.prepare(true)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, in, realRetryClock())
}

func (r ProjectResource) create(
	ctx context.Context, client projectClient, in *sdk.CreateProjectInput, clock retryClock,
) (_ *ProjectResourceOutput, err error) {
	var response *sdk.CreateProjectOutput
	err = propagate(ctx, clock, func(ctx context.Context) error {
		var callErr error
		response, callErr = client.CreateProject(ctx, in)
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	identity := &ProjectResourceOutput{Name: r.Name}
	if response != nil && response.Project != nil {
		identity.Arn = aws.ToString(response.Project.Arn)
	}
	defer func() {
		if err == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if cleanupErr := deleteProject(cleanup, client, identity); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("compensating delete project: %w", cleanupErr))
		}
	}()
	if response == nil || response.Project == nil || identity.Arn == "" {
		return nil, errors.New("create project returned no project ARN")
	}
	if fallback(r.ProjectVisibility, "PRIVATE") == "PUBLIC_READ" {
		if err := r.visibility(ctx, client, identity); err != nil {
			return nil, err
		}
	}
	return readProject(ctx, client, identity, true)
}

func (r *ProjectResource) Read(
	ctx context.Context, cfg *awscfg.Configuration,
	prior runtime.Prior[ProjectResource, *ProjectResourceOutput, *awscfg.Configuration],
) (*ProjectResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return readProject(ctx, client, prior.Outputs, false)
}

func projectIdentity(prior *ProjectResourceOutput) (string, error) {
	if prior != nil {
		if prior.Arn != "" {
			return prior.Arn, nil
		}
		if prior.Name != "" {
			return prior.Name, nil
		}
	}
	return "", errors.New("project has no persisted ARN or name")
}

func readProject(
	ctx context.Context, client projectClient, prior *ProjectResourceOutput, created bool,
) (*ProjectResourceOutput, error) {
	identity, err := projectIdentity(prior)
	if err != nil {
		return nil, err
	}
	response, err := client.BatchGetProjects(ctx,
		&sdk.BatchGetProjectsInput{Names: []string{identity}})
	if err != nil {
		var absent *types.ResourceNotFoundException
		if !created && errors.As(err, &absent) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get project: %w", err)
	}
	if response == nil || len(response.Projects) != 1 {
		if !created {
			return nil, runtime.ErrNotFound
		}
		return nil, errors.New("get created project: expected exactly one project")
	}
	return projectOutput(response.Projects[0])
}

func (r *ProjectResource) Update(
	ctx context.Context, cfg *awscfg.Configuration,
	prior runtime.Prior[ProjectResource, *ProjectResourceOutput, *awscfg.Configuration],
) (*ProjectResourceOutput, error) {
	in, err := r.prepare(false)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, in, prior, realRetryClock())
}

func (r ProjectResource) update(
	ctx context.Context, client projectClient, in *sdk.CreateProjectInput,
	prior runtime.Prior[ProjectResource, *ProjectResourceOutput, *awscfg.Configuration],
	clock retryClock,
) (*ProjectResourceOutput, error) {
	current := prior.Observed
	if current == nil {
		current = prior.Outputs
	}
	patch, visible, err := r.patch(in, prior.Inputs, current, prior.Outputs)
	if err != nil {
		return prior.Outputs, err
	}
	if visible {
		if err := r.visibility(ctx, client, prior.Outputs); err != nil {
			return prior.Outputs, err
		}
	}
	if patch != nil {
		err := propagate(ctx, clock, func(ctx context.Context) error {
			_, err := client.UpdateProject(ctx, patch)
			return err
		})
		if err != nil {
			return prior.Outputs, fmt.Errorf("update project: %w", err)
		}
	}
	out, err := readProject(ctx, client, prior.Outputs, false)
	if err != nil {
		return prior.Outputs, err
	}
	return out, nil
}

func (r ProjectResource) visibility(
	ctx context.Context, client projectClient, prior *ProjectResourceOutput,
) error {
	identity, err := projectIdentity(prior)
	if err != nil {
		return err
	}
	_, err = client.UpdateProjectVisibility(ctx, &sdk.UpdateProjectVisibilityInput{
		ProjectArn:         aws.String(identity),
		ProjectVisibility:  types.ProjectVisibilityType(fallback(r.ProjectVisibility, "PRIVATE")),
		ResourceAccessRole: optional(r.ResourceAccessRole),
	})
	if err != nil {
		return fmt.Errorf("update project visibility: %w", err)
	}
	return nil
}

func (r *ProjectResource) Delete(
	ctx context.Context, cfg *awscfg.Configuration,
	prior runtime.Prior[ProjectResource, *ProjectResourceOutput, *awscfg.Configuration],
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return deleteProject(ctx, client, prior.Outputs)
}

func deleteProject(
	ctx context.Context, client projectClient, prior *ProjectResourceOutput,
) error {
	identity, err := projectIdentity(prior)
	if err != nil {
		return err
	}
	_, err = client.DeleteProject(ctx, &sdk.DeleteProjectInput{Name: aws.String(identity)})
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func projectOutput(p types.Project) (*ProjectResourceOutput, error) {
	if p.Environment == nil || p.Name == nil || *p.Name == "" ||
		p.Arn == nil || *p.Arn == "" {
		return nil, errors.New("project response is missing ARN, name, or environment")
	}
	tags := p.Tags
	p.Tags = nil
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode project response: %w", err)
	}
	var out ProjectResourceOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode project response: %w", err)
	}
	out.Tags = map[string]string{}
	for _, tag := range tags {
		if tag.Key != nil {
			for _, user := range projectTags(map[string]string{*tag.Key: aws.ToString(tag.Value)}) {
				out.Tags[*user.Key] = *user.Value
			}
		}
	}
	if out.ProjectVisibility == "" {
		out.ProjectVisibility = "PRIVATE"
	}
	out.Environment.Certificate = aws.String(value(out.Environment.Certificate))
	out.Environment.PrivilegedMode = aws.Bool(value(out.Environment.PrivilegedMode))
	normalizeSource(out.Source)
	for i := range value(out.SecondarySources) {
		s := &(*out.SecondarySources)[i]
		s.Buildspec = aws.String(value(s.Buildspec))
		s.GitCloneDepth = aws.Int64(value(s.GitCloneDepth))
		s.InsecureSsl = aws.Bool(value(s.InsecureSsl))
		s.Location = aws.String(value(s.Location))
		s.ReportBuildStatus = aws.Bool(value(s.ReportBuildStatus))
	}
	if out.LogsConfig != nil {
		if out.LogsConfig.CloudWatchLogs == nil {
			out.LogsConfig.CloudWatchLogs = &ProjectCloudWatchLogs{Status: aws.String("DISABLED")}
		}
		if out.LogsConfig.S3Logs == nil {
			out.LogsConfig.S3Logs = &ProjectS3Logs{Status: aws.String("DISABLED")}
		}
	}
	return &out, nil
}

func normalizeSource(s *ProjectSource) {
	if s == nil {
		return
	}
	s.Buildspec = aws.String(value(s.Buildspec))
	s.GitCloneDepth = aws.Int64(value(s.GitCloneDepth))
	s.InsecureSsl = aws.Bool(value(s.InsecureSsl))
	s.Location = aws.String(value(s.Location))
	s.ReportBuildStatus = aws.Bool(value(s.ReportBuildStatus))
}
