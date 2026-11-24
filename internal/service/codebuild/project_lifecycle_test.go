package codebuild

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClient struct {
	calls                           []string
	creates                         []*sdk.CreateProjectInput
	updates                         []*sdk.UpdateProjectInput
	reads                           []*sdk.BatchGetProjectsInput
	deletes                         []*sdk.DeleteProjectInput
	visibilities                    []*sdk.UpdateProjectVisibilityInput
	project                         types.Project
	failure                         string
	failureErr, cleanupErr          error
	lookup                          *sdk.BatchGetProjectsOutput
	overrideLookup, missingCreate   bool
	cancel                          context.CancelFunc
	cleanupDetached, cleanupBounded bool
}

func (c *fakeClient) fail(action string) error {
	c.calls = append(c.calls, action)
	if c.failure == action {
		if c.cancel != nil {
			c.cancel()
		}
		if c.failureErr != nil {
			return c.failureErr
		}
		return errors.New("injected failure")
	}
	return nil
}
func (c *fakeClient) CreateProject(
	_ context.Context, in *sdk.CreateProjectInput, _ ...func(*sdk.Options),
) (*sdk.CreateProjectOutput, error) {
	c.creates = append(c.creates, in)
	if err := c.fail("create"); err != nil {
		return nil, err
	}
	if c.missingCreate {
		return &sdk.CreateProjectOutput{}, nil
	}
	return &sdk.CreateProjectOutput{Project: &c.project}, nil
}
func (c *fakeClient) BatchGetProjects(
	_ context.Context, in *sdk.BatchGetProjectsInput, _ ...func(*sdk.Options),
) (*sdk.BatchGetProjectsOutput, error) {
	c.reads = append(c.reads, in)
	if err := c.fail("read"); err != nil {
		return nil, err
	}
	if c.overrideLookup {
		return c.lookup, nil
	}
	return &sdk.BatchGetProjectsOutput{Projects: []types.Project{c.project}}, nil
}
func (c *fakeClient) UpdateProject(
	_ context.Context, in *sdk.UpdateProjectInput, _ ...func(*sdk.Options),
) (*sdk.UpdateProjectOutput, error) {
	c.updates = append(c.updates, in)
	if err := c.fail("update"); err != nil {
		return nil, err
	}
	return &sdk.UpdateProjectOutput{Project: &c.project}, nil
}
func (c *fakeClient) UpdateProjectVisibility(
	_ context.Context, in *sdk.UpdateProjectVisibilityInput, _ ...func(*sdk.Options),
) (*sdk.UpdateProjectVisibilityOutput, error) {
	c.visibilities = append(c.visibilities, in)
	if err := c.fail("visibility"); err != nil {
		return nil, err
	}
	return &sdk.UpdateProjectVisibilityOutput{}, nil
}
func (c *fakeClient) DeleteProject(
	ctx context.Context, in *sdk.DeleteProjectInput, _ ...func(*sdk.Options),
) (*sdk.DeleteProjectOutput, error) {
	c.deletes = append(c.deletes, in)
	c.cleanupDetached = ctx.Err() == nil
	deadline, ok := ctx.Deadline()
	c.cleanupBounded = ok && time.Until(deadline) <= 2*time.Minute &&
		time.Until(deadline) > time.Minute
	if err := c.fail("delete"); err != nil {
		return nil, err
	}
	return &sdk.DeleteProjectOutput{}, c.cleanupErr
}

func TestProjectLifecycle(t *testing.T) {
	for _, visibility := range []string{"PRIVATE", "PUBLIC_READ"} {
		r := minimalProject()
		r.ProjectVisibility, r.ResourceAccessRole = &visibility, aws.String(r.ServiceRole)
		c := &fakeClient{project: minimalResponse()}
		in, err := r.prepare(true)
		require.NoError(t, err)
		out, err := r.create(t.Context(), c, in, realRetryClock())
		require.NoError(t, err)
		assert.Equal(t, "test", out.Name)
		want := []string{"create", "read"}
		if visibility == "PUBLIC_READ" {
			want = []string{"create", "visibility", "read"}
			assert.Equal(t, []*sdk.UpdateProjectVisibilityInput{{
				ProjectArn: c.project.Arn, ProjectVisibility: "PUBLIC_READ",
				ResourceAccessRole: aws.String(r.ServiceRole),
			}}, c.visibilities)
		}
		assert.Equal(t, want, c.calls)
		assert.Equal(t, []*sdk.BatchGetProjectsInput{{Names: []string{out.Arn}}}, c.reads)
	}
}

func TestProjectFailedCreation(t *testing.T) {
	for _, mode := range []string{"visibility", "read", "output", "response"} {
		for _, cleanupFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cleanup=%t", mode, cleanupFails), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				r := minimalProject()
				r.ProjectVisibility = aws.String("PUBLIC_READ")
				c := &fakeClient{project: minimalResponse(), failure: mode, cancel: cancel}
				if mode == "output" {
					c.project.Environment = nil
				}
				if mode == "response" {
					c.missingCreate = true
				}
				if cleanupFails {
					c.cleanupErr = errors.New("cleanup failed")
				}
				in, err := r.prepare(true)
				require.NoError(t, err)
				out, err := r.create(ctx, c, in, realRetryClock())
				require.Error(t, err)
				assert.Nil(t, out)
				if cleanupFails {
					assert.ErrorContains(t, err, "cleanup failed")
				}
				assert.Equal(t, "delete", c.calls[len(c.calls)-1])
				want := c.project.Arn
				if mode == "response" {
					want = aws.String("test")
				}
				assert.Equal(t, []*sdk.DeleteProjectInput{{Name: want}}, c.deletes)
				assert.True(t, c.cleanupDetached)
				assert.True(t, c.cleanupBounded)
			})
		}
	}
}

func TestProjectReadAndDelete(t *testing.T) {
	old := &ProjectResourceOutput{Arn: "old-arn", Name: "old-name"}
	for _, response := range []*sdk.BatchGetProjectsOutput{nil, {},
		{Projects: []types.Project{minimalResponse(), minimalResponse()}}} {
		for _, created := range []bool{false, true} {
			c := &fakeClient{overrideLookup: true, lookup: response}
			_, err := readProject(t.Context(), c, old, created)
			if created {
				require.Error(t, err)
				assert.NotErrorIs(t, err, runtime.ErrNotFound)
			} else {
				assert.ErrorIs(t, err, runtime.ErrNotFound)
			}
			assert.Equal(t, []*sdk.BatchGetProjectsInput{{Names: []string{"old-arn"}}}, c.reads)
		}
	}
	for _, failure := range []error{errors.New("denied"), &types.ResourceNotFoundException{}} {
		c := &fakeClient{failure: "read", failureErr: failure}
		_, err := readProject(t.Context(), c, old, false)
		if _, ok := failure.(*types.ResourceNotFoundException); ok {
			assert.ErrorIs(t, err, runtime.ErrNotFound)
		} else {
			assert.ErrorIs(t, err, failure)
		}
		c.failure = "delete"
		assert.ErrorIs(t, deleteProject(t.Context(), c, old), failure)
		assert.Equal(t, []*sdk.DeleteProjectInput{{Name: aws.String("old-arn")}}, c.deletes)
	}
	c := &fakeClient{project: minimalResponse()}
	_, err := readProject(t.Context(), c, &ProjectResourceOutput{Name: "old"}, false)
	require.NoError(t, err)
	assert.Equal(t, []*sdk.BatchGetProjectsInput{{Names: []string{"old"}}}, c.reads)
}

func TestProjectOutput(t *testing.T) {
	p := minimalResponse()
	p.LogsConfig, p.Source = &types.LogsConfig{}, &types.ProjectSource{Type: "S3"}
	p.Environment = &types.ProjectEnvironment{Type: "LINUX_CONTAINER"}
	p.Tags = []types.Tag{
		{Key: aws.String("user"), Value: aws.String("")},
		{Key: aws.String("aws:system"), Value: aws.String("x")},
	}
	out, err := projectOutput(p)
	require.NoError(t, err)
	assert.Equal(t, ProjectBadge{}, out.Badge)
	assert.Equal(t, "PRIVATE", out.ProjectVisibility)
	assert.Equal(t, &ProjectSource{Type: "S3", Buildspec: aws.String(""),
		GitCloneDepth: aws.Int64(0), InsecureSsl: aws.Bool(false), Location: aws.String(""),
		ReportBuildStatus: aws.Bool(false)}, out.Source)
	assert.Equal(t, ProjectEnvironment{Type: "LINUX_CONTAINER",
		Certificate: aws.String(""), PrivilegedMode: aws.Bool(false),
		ImagePullCredentialsType: aws.String("")}, out.Environment)
	assert.Equal(t, &ProjectLogsConfig{
		CloudWatchLogs: &ProjectCloudWatchLogs{Status: aws.String("DISABLED")},
		S3Logs:         &ProjectS3Logs{Status: aws.String("DISABLED")},
	}, out.LogsConfig)
	assert.Equal(t, map[string]string{"user": ""}, out.Tags)
	assert.Nil(t, out.Cache)
	assert.Nil(t, out.BuildBatchConfig)
	assert.Nil(t, out.VpcConfig)
	p.Artifacts, p.Source = nil, nil
	out, err = projectOutput(p)
	require.NoError(t, err)
	assert.Nil(t, out.Artifacts)
	assert.Nil(t, out.Source)
}

func TestProjectSelectiveUpdate(t *testing.T) {
	r := minimalProject()
	out, err := projectOutput(minimalResponse())
	require.NoError(t, err)
	in, err := r.prepare(false)
	require.NoError(t, err)
	patch, visibility, err := r.patch(in, r, out, out)
	require.NoError(t, err)
	assert.False(t, visibility)
	assert.Nil(t, patch)
	r.Description, r.ProjectVisibility = aws.String("changed"), aws.String("PUBLIC_READ")
	r.Tags = &map[string]string{"owner": "test"}
	c := &fakeClient{project: minimalResponse()}
	in, err = r.prepare(false)
	require.NoError(t, err)
	_, err = r.update(t.Context(), c, in,
		runtime.Prior[ProjectResource, *ProjectResourceOutput, *awscfg.Configuration]{
			Inputs: minimalProject(), Outputs: out,
		}, realRetryClock())
	require.NoError(t, err)
	assert.Equal(t, []string{"visibility", "update", "read"}, c.calls)
	assert.Equal(t, []*sdk.UpdateProjectInput{{
		Name: aws.String(out.Name), Description: aws.String("changed"),
		Tags: []types.Tag{{Key: aws.String("owner"), Value: aws.String("test")}},
	}}, c.updates)
	c = &fakeClient{project: minimalResponse()}
	r.Description, r.Tags = nil, nil
	in, err = r.prepare(false)
	require.NoError(t, err)
	_, err = r.update(t.Context(), c, in,
		runtime.Prior[ProjectResource, *ProjectResourceOutput, *awscfg.Configuration]{
			Inputs: minimalProject(), Outputs: out,
		}, realRetryClock())
	require.NoError(t, err)
	assert.Equal(t, []string{"visibility", "read"}, c.calls)
	assert.Empty(t, c.updates)
}

func TestProjectUpdateGatesAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*ProjectResource)
		failure string
		want    []string
	}{
		{"role only", func(r *ProjectResource) { r.ResourceAccessRole = aws.String(r.ServiceRole) },
			"", []string{"visibility", "read"}},
		{"tags only", func(r *ProjectResource) { r.Tags = &map[string]string{"new": "value"} },
			"", []string{"update", "read"}},
		{"visibility failure", func(r *ProjectResource) {
			r.ProjectVisibility, r.Description = aws.String("PUBLIC_READ"), aws.String("new")
		}, "visibility", []string{"visibility"}},
		{"ordinary update failure", func(r *ProjectResource) {
			r.ProjectVisibility, r.Description = aws.String("PUBLIC_READ"), aws.String("new")
		}, "update", []string{"visibility", "update"}},
		{"final read failure", func(r *ProjectResource) { r.Description = aws.String("new") },
			"read", []string{"update", "read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := minimalProject()
			tc.change(&r)
			out, err := projectOutput(minimalResponse())
			require.NoError(t, err)
			in, err := r.prepare(false)
			require.NoError(t, err)
			failure := errors.New("operation failed")
			c := &fakeClient{project: minimalResponse(), failure: tc.failure, failureErr: failure}
			got, err := r.update(t.Context(), c, in,
				runtime.Prior[ProjectResource, *ProjectResourceOutput, *awscfg.Configuration]{
					Inputs: minimalProject(), Outputs: out,
				}, realRetryClock())
			if tc.failure != "" {
				assert.ErrorIs(t, err, failure)
				assert.Same(t, out, got)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, c.calls)
			for _, update := range c.updates {
				assert.Equal(t, aws.String(out.Name), update.Name)
				assert.Equal(t, projectTags(value(r.Tags)), update.Tags)
			}
			for _, visibility := range c.visibilities {
				assert.Equal(t, aws.String(out.Arn), visibility.ProjectArn)
			}
			for _, read := range c.reads {
				assert.Equal(t, []string{out.Arn}, read.Names)
			}
			assert.Empty(t, c.deletes)
		})
	}
}

func TestProjectDeleteCancellationAndCreatedReadErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	identity := &ProjectResourceOutput{Name: "old"}
	c := &fakeClient{failure: "delete", failureErr: ctx.Err()}
	assert.ErrorIs(t, deleteProject(ctx, c, identity), context.Canceled)
	assert.Equal(t, []string{"delete"}, c.calls)
	assert.Equal(t, []*sdk.DeleteProjectInput{{Name: aws.String("old")}}, c.deletes)
	for _, failure := range []error{errors.New("denied"), &types.ResourceNotFoundException{}} {
		c := &fakeClient{failure: "read", failureErr: failure}
		_, err := readProject(t.Context(), c, identity, true)
		assert.ErrorIs(t, err, failure)
		assert.NotErrorIs(t, err, runtime.ErrNotFound)
	}
}
