package lambdamicrovms

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslambdamicrovms "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	lambdamicrovmstypes "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	microvmImageReadyTimeout  = 2 * time.Hour
	microvmImageDeleteTimeout = 30 * time.Minute
	microvmImagePollInterval  = 15 * time.Second
)

type MicrovmImageResource struct {
	Name                     string              `ub:"name"`
	BaseImageArn             string              `ub:"base-image-arn"`
	BuildRoleArn             string              `ub:"build-role-arn"`
	CodeArtifact             CodeArtifact        `ub:"code-artifact"`
	BaseImageVersion         *string             `ub:"base-image-version"`
	AdditionalOsCapabilities *[]string           `ub:"additional-os-capabilities"`
	CpuConfigurations        *[]CpuConfiguration `ub:"cpu-configurations"`
	Description              *string             `ub:"description"`
	EgressNetworkConnectors  *[]string           `ub:"egress-network-connectors"`
	EnvironmentVariables     *map[string]string  `ub:"environment-variables"`
	Hooks                    *Hooks              `ub:"hooks"`
	Logging                  *Logging            `ub:"logging"`
	Resources                *[]Resources        `ub:"resources"`
	Tags                     *map[string]string  `ub:"tags"`
	TerminateOnDestroy       *bool               `ub:"terminate-on-destroy"`
}

func (r *MicrovmImageResource) SchemaVersion() int { return 1 }

func (r *MicrovmImageResource) ReplaceFields() []string {
	return []string{"name"}
}

func (r MicrovmImageResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.Logging)).Require(constraint.Any(
			constraint.All(
				constraint.Present(r.Logging.CloudWatch),
				constraint.Absent(r.Logging.Disabled),
			),
			constraint.All(
				constraint.Absent(r.Logging.CloudWatch),
				constraint.Present(r.Logging.Disabled),
			),
		)).Message("logging must set exactly one of cloud-watch or disabled"),
		constraint.When(constraint.Present(r.Logging.Disabled)).
			Require(constraint.IsTrue(r.Logging.Disabled)).
			Message("logging disabled must be true"),
		constraint.ForEach(r.AdditionalOsCapabilities, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.Equals(value, "ALL")).
					Message("additional-os-capabilities values must be ALL"),
			}
		}),
		constraint.ForEach(r.CpuConfigurations, func(c CpuConfiguration) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.Equals(c.Architecture, "ARM_64")).
					Message("cpu-configurations architecture must be ARM_64"),
			}
		}),
		constraint.When(constraint.Present(r.EgressNetworkConnectors)).
			Require(constraint.MaxItems(r.EgressNetworkConnectors, 10)).
			Message("egress-network-connectors must have at most 10 items"),
		constraint.When(constraint.Present(r.Resources)).
			Require(constraint.MaxItems(r.Resources, 1)).
			Message("resources must have at most one item"),
		constraint.When(constraint.Present(r.Hooks.Port)).
			Require(constraint.AtLeast(r.Hooks.Port, 1), constraint.AtMost(r.Hooks.Port, 65535)).
			Message("hooks port must be between 1 and 65535"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.Run)).
			Require(constraint.OneOf(r.Hooks.MicrovmHooks.Run, "ENABLED", "DISABLED")).
			Message("microvm hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.Resume)).
			Require(constraint.OneOf(r.Hooks.MicrovmHooks.Resume, "ENABLED", "DISABLED")).
			Message("microvm hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.Suspend)).
			Require(constraint.OneOf(r.Hooks.MicrovmHooks.Suspend, "ENABLED", "DISABLED")).
			Message("microvm hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.Terminate)).
			Require(constraint.OneOf(r.Hooks.MicrovmHooks.Terminate, "ENABLED", "DISABLED")).
			Message("microvm hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.RunTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmHooks.RunTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmHooks.RunTimeoutInSeconds, 60)).
			Message("microvm hook timeouts must be between 1 and 60"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.ResumeTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmHooks.ResumeTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmHooks.ResumeTimeoutInSeconds, 60)).
			Message("microvm hook timeouts must be between 1 and 60"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.SuspendTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmHooks.SuspendTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmHooks.SuspendTimeoutInSeconds, 60)).
			Message("microvm hook timeouts must be between 1 and 60"),
		constraint.When(constraint.Present(r.Hooks.MicrovmHooks.TerminateTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmHooks.TerminateTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmHooks.TerminateTimeoutInSeconds, 60)).
			Message("microvm hook timeouts must be between 1 and 60"),
		constraint.When(constraint.Present(r.Hooks.MicrovmImageHooks.Ready)).
			Require(constraint.OneOf(r.Hooks.MicrovmImageHooks.Ready, "ENABLED", "DISABLED")).
			Message("microvm image hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmImageHooks.Validate)).
			Require(constraint.OneOf(r.Hooks.MicrovmImageHooks.Validate, "ENABLED", "DISABLED")).
			Message("microvm image hook states must be ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Hooks.MicrovmImageHooks.ReadyTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmImageHooks.ReadyTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmImageHooks.ReadyTimeoutInSeconds, 3600)).
			Message("microvm image hook timeouts must be between 1 and 3600"),
		constraint.When(constraint.Present(r.Hooks.MicrovmImageHooks.ValidateTimeoutInSeconds)).
			Require(constraint.AtLeast(r.Hooks.MicrovmImageHooks.ValidateTimeoutInSeconds, 1),
				constraint.AtMost(r.Hooks.MicrovmImageHooks.ValidateTimeoutInSeconds, 3600)).
			Message("microvm image hook timeouts must be between 1 and 3600"),
	}
}

func (r *MicrovmImageResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*MicrovmImageResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in, err := r.createInput()
	if err != nil {
		return nil, err
	}
	created, err := client.CreateMicrovmImage(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("create Microvm image %s: %w", r.Name, err)
	}
	imageArn := aws.ToString(created.ImageArn)
	if imageArn == "" {
		return nil, fmt.Errorf("create Microvm image %s: response holds no image arn", r.Name)
	}
	if err := r.waitReady(ctx, client, imageArn); err != nil {
		return nil, err
	}
	return r.read(ctx, client, imageArn)
}

func (r *MicrovmImageResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *MicrovmImageResourceOutput) (*MicrovmImageResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := r.read(ctx, client, prior.ImageArn)
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, err
	}
	return out, nil
}

func (r *MicrovmImageResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[MicrovmImageResource, *MicrovmImageResourceOutput],
) (*MicrovmImageResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	imageArn := prior.Outputs.ImageArn
	if r.nonTagInputsChanged(prior.Inputs) {
		in, err := r.updateInput(imageArn)
		if err != nil {
			return nil, err
		}
		_, err = client.UpdateMicrovmImage(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("update Microvm image %s: %w", imageArn, err)
		}
		if err := r.waitUpdated(ctx, client, imageArn); err != nil {
			return nil, err
		}
	}
	if r.tagsChanged(prior.Inputs) {
		if err := r.syncTags(ctx, client, imageArn); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, imageArn)
}

func (r *MicrovmImageResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *MicrovmImageResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	imageArn := prior.ImageArn
	if aws.ToBool(r.TerminateOnDestroy) {
		if err := r.terminateMicrovms(ctx, client, imageArn); err != nil {
			return err
		}
	}
	_, err = client.DeleteMicrovmImage(ctx, &awslambdamicrovms.DeleteMicrovmImageInput{
		ImageIdentifier: aws.String(imageArn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete Microvm image %s: %w", imageArn, err)
	}
	return wait.Until(ctx, "Microvm image "+imageArn, func(ctx context.Context) (bool, error) {
		out, err := client.GetMicrovmImage(ctx, &awslambdamicrovms.GetMicrovmImageInput{
			ImageIdentifier: aws.String(imageArn),
		})
		if err != nil {
			if isNotFound(err) {
				return true, nil
			}
			return false, fmt.Errorf("read Microvm image %s: %w", imageArn, err)
		}
		switch out.State {
		case lambdamicrovmstypes.MicrovmImageStateDeleted:
			return true, nil
		case lambdamicrovmstypes.MicrovmImageStateDeleteFailed:
			return false, fmt.Errorf("Microvm image %s entered state %s", imageArn, out.State)
		default:
			return false, nil
		}
	}, wait.WithTimeout(microvmImageDeleteTimeout), wait.WithInterval(microvmImagePollInterval))
}

func (r *MicrovmImageResource) terminateMicrovms(
	ctx context.Context,
	client *awslambdamicrovms.Client,
	imageArn string,
) error {
	items, err := r.activeMicrovms(ctx, client, imageArn)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	for _, item := range items {
		if !microvmCanTerminate(item.State) {
			continue
		}
		microvmID := aws.ToString(item.MicrovmId)
		if microvmID == "" {
			return fmt.Errorf("list Microvms for image %s returned item without microvm id", imageArn)
		}
		_, err := client.TerminateMicrovm(ctx, &awslambdamicrovms.TerminateMicrovmInput{
			MicrovmIdentifier: aws.String(microvmID),
		})
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return fmt.Errorf("terminate Microvm %s for image %s: %w", microvmID, imageArn, err)
		}
	}
	return wait.Until(ctx, "Microvms for image "+imageArn,
		func(ctx context.Context) (bool, error) {
			items, err := r.activeMicrovms(ctx, client, imageArn)
			if err != nil {
				return false, err
			}
			return len(items) == 0, nil
		},
		wait.WithTimeout(microvmImageDeleteTimeout),
		wait.WithInterval(microvmImagePollInterval))
}

func (r *MicrovmImageResource) activeMicrovms(
	ctx context.Context,
	client *awslambdamicrovms.Client,
	imageArn string,
) ([]lambdamicrovmstypes.MicrovmItem, error) {
	items := []lambdamicrovmstypes.MicrovmItem{}
	paginator := awslambdamicrovms.NewListMicrovmsPaginator(client,
		&awslambdamicrovms.ListMicrovmsInput{ImageIdentifier: aws.String(imageArn)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list Microvms for image %s: %w", imageArn, err)
		}
		for _, item := range page.Items {
			if microvmBlocksImageDelete(item.State) {
				items = append(items, item)
			}
		}
	}
	return items, nil
}

func microvmBlocksImageDelete(state lambdamicrovmstypes.MicrovmState) bool {
	return state != lambdamicrovmstypes.MicrovmStateTerminated
}

func microvmCanTerminate(state lambdamicrovmstypes.MicrovmState) bool {
	switch state {
	case lambdamicrovmstypes.MicrovmStateTerminated,
		lambdamicrovmstypes.MicrovmStateTerminating:
		return false
	default:
		return true
	}
}

func (r *MicrovmImageResource) createInput() (*awslambdamicrovms.CreateMicrovmImageInput, error) {
	hooks, resources, err := r.hooksAndResources()
	if err != nil {
		return nil, err
	}
	in := &awslambdamicrovms.CreateMicrovmImageInput{
		Name:                     aws.String(r.Name),
		BaseImageArn:             aws.String(r.BaseImageArn),
		BuildRoleArn:             aws.String(r.BuildRoleArn),
		CodeArtifact:             codeArtifactToSDK(r.CodeArtifact),
		BaseImageVersion:         r.BaseImageVersion,
		AdditionalOsCapabilities: capabilitiesToSDK(r.AdditionalOsCapabilities),
		CpuConfigurations:        cpuConfigurationsToSDK(r.CpuConfigurations),
		Description:              r.Description,
		EgressNetworkConnectors:  stringSliceValue(r.EgressNetworkConnectors),
		EnvironmentVariables:     stringMapValue(r.EnvironmentVariables),
		Hooks:                    hooks,
		Logging:                  loggingToSDK(r.Logging),
		Resources:                resources,
	}
	if r.Tags != nil {
		in.Tags = *r.Tags
	}
	return in, nil
}

func (r *MicrovmImageResource) updateInput(
	imageArn string,
) (*awslambdamicrovms.UpdateMicrovmImageInput, error) {
	hooks, resources, err := r.hooksAndResources()
	if err != nil {
		return nil, err
	}
	return &awslambdamicrovms.UpdateMicrovmImageInput{
		ImageIdentifier:          aws.String(imageArn),
		BaseImageArn:             aws.String(r.BaseImageArn),
		BuildRoleArn:             aws.String(r.BuildRoleArn),
		CodeArtifact:             codeArtifactToSDK(r.CodeArtifact),
		BaseImageVersion:         r.BaseImageVersion,
		AdditionalOsCapabilities: capabilitiesToSDK(r.AdditionalOsCapabilities),
		CpuConfigurations:        cpuConfigurationsToSDK(r.CpuConfigurations),
		Description:              r.Description,
		EgressNetworkConnectors:  stringSliceValue(r.EgressNetworkConnectors),
		EnvironmentVariables:     stringMapValue(r.EnvironmentVariables),
		Hooks:                    hooks,
		Logging:                  loggingToSDK(r.Logging),
		Resources:                resources,
	}, nil
}

func (r *MicrovmImageResource) hooksAndResources() (
	*lambdamicrovmstypes.Hooks,
	[]lambdamicrovmstypes.Resources,
	error,
) {
	hooks, err := hooksToSDK(r.Hooks)
	if err != nil {
		return nil, nil, err
	}
	resources, err := resourcesToSDK(r.Resources)
	if err != nil {
		return nil, nil, err
	}
	return hooks, resources, nil
}

func (r *MicrovmImageResource) waitReady(
	ctx context.Context,
	client *awslambdamicrovms.Client,
	imageArn string,
) error {
	return r.waitForState(ctx, client, imageArn,
		map[lambdamicrovmstypes.MicrovmImageState]bool{
			lambdamicrovmstypes.MicrovmImageStateCreated: true,
		},
		map[lambdamicrovmstypes.MicrovmImageState]bool{
			lambdamicrovmstypes.MicrovmImageStateCreateFailed: true,
		},
		microvmImageReadyTimeout)
}

func (r *MicrovmImageResource) waitUpdated(
	ctx context.Context,
	client *awslambdamicrovms.Client,
	imageArn string,
) error {
	return r.waitForState(ctx, client, imageArn,
		map[lambdamicrovmstypes.MicrovmImageState]bool{
			lambdamicrovmstypes.MicrovmImageStateCreated: true,
			lambdamicrovmstypes.MicrovmImageStateUpdated: true,
		},
		map[lambdamicrovmstypes.MicrovmImageState]bool{
			lambdamicrovmstypes.MicrovmImageStateUpdateFailed: true,
		},
		microvmImageReadyTimeout)
}

func (r *MicrovmImageResource) waitForState(
	ctx context.Context,
	client *awslambdamicrovms.Client,
	imageArn string,
	ready map[lambdamicrovmstypes.MicrovmImageState]bool,
	failed map[lambdamicrovmstypes.MicrovmImageState]bool,
	timeout time.Duration,
) error {
	return wait.Until(ctx, "Microvm image "+imageArn, func(ctx context.Context) (bool, error) {
		out, err := client.GetMicrovmImage(ctx, &awslambdamicrovms.GetMicrovmImageInput{
			ImageIdentifier: aws.String(imageArn),
		})
		if err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("read Microvm image %s: %w", imageArn, err)
		}
		if failed[out.State] {
			name := aws.ToString(out.Name)
			if name == "" {
				name = imageArn
			}
			return false, fmt.Errorf("Microvm image %s entered state %s", name, out.State)
		}
		return ready[out.State], nil
	}, wait.WithTimeout(timeout), wait.WithInterval(microvmImagePollInterval))
}

func (r *MicrovmImageResource) read(
	ctx context.Context,
	client *awslambdamicrovms.Client,
	imageArn string,
) (*MicrovmImageResourceOutput, error) {
	out, err := client.GetMicrovmImage(ctx, &awslambdamicrovms.GetMicrovmImageInput{
		ImageIdentifier: aws.String(imageArn),
	})
	if err != nil {
		return nil, err
	}
	return microvmImageOutputFromGet(out), nil
}

func (r *MicrovmImageResource) syncTags(
	ctx context.Context,
	client *awslambdamicrovms.Client,
	imageArn string,
) error {
	if r.Tags == nil {
		return nil
	}
	return tagsync.Sync(ctx, *r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			out, err := client.ListTags(ctx, &awslambdamicrovms.ListTagsInput{
				Resource: aws.String(imageArn),
			})
			if err != nil {
				return nil, fmt.Errorf("list Microvm image tags %s: %w", imageArn, err)
			}
			return out.Tags, nil
		},
		func(ctx context.Context, tags map[string]string) error {
			_, err := client.TagResource(ctx, &awslambdamicrovms.TagResourceInput{
				Resource: aws.String(imageArn),
				Tags:     tags,
			})
			if err != nil {
				return fmt.Errorf("tag Microvm image %s: %w", imageArn, err)
			}
			return nil
		},
		func(ctx context.Context, keys []string) error {
			_, err := client.UntagResource(ctx, &awslambdamicrovms.UntagResourceInput{
				Resource: aws.String(imageArn),
				TagKeys:  keys,
			})
			if err != nil {
				return fmt.Errorf("untag Microvm image %s: %w", imageArn, err)
			}
			return nil
		})
}

func (r *MicrovmImageResource) nonTagInputsChanged(prior MicrovmImageResource) bool {
	current := *r
	current.Tags = nil
	current.TerminateOnDestroy = nil
	prior.Tags = nil
	prior.TerminateOnDestroy = nil
	return !reflect.DeepEqual(prior, current)
}

func (r *MicrovmImageResource) tagsChanged(prior MicrovmImageResource) bool {
	return r.Tags != nil && !reflect.DeepEqual(prior.Tags, r.Tags)
}

func capabilitiesToSDK(in *[]string) []lambdamicrovmstypes.Capability {
	if in == nil {
		return nil
	}
	out := make([]lambdamicrovmstypes.Capability, 0, len(*in))
	for _, value := range *in {
		out = append(out, lambdamicrovmstypes.Capability(value))
	}
	return out
}

func stringSliceValue(in *[]string) []string {
	if in == nil {
		return nil
	}
	return *in
}

func stringMapValue(in *map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	return *in
}
