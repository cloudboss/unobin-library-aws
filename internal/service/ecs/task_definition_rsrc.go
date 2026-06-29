package ecs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	ecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// taskDefinitionFamilyRegexp matches a valid task definition family: 1 to
// 255 letters, numbers, underscores, and hyphens. The character class is
// ASCII, so the byte length the regexp enforces equals the character length
// ECS limits.
var taskDefinitionFamilyRegexp = regexp.MustCompile(`^[0-9A-Za-z_-]{1,255}$`)

// TaskDefinition manages one revision of an ECS task definition. A revision
// is registered whole by a single RegisterTaskDefinition call and is
// immutable afterward: every input except tags is fixed at registration, so
// any other change replaces the resource by registering a new revision of
// the family. Registering a family again never conflicts; the revision
// number auto-increments and is never reused. Deleting deregisters the
// revision, which makes it INACTIVE; tasks and services already running it
// keep working.
//
// Family is required and must match ^[0-9A-Za-z_-]{1,255}$, a
// regular-expression and byte-length check enforced in Create rather than a
// declarative constraint. Cpu and memory are strings in the API, expressed
// as CPU units or vCPUs (for example "256", "0.5 vCPU") and MiB or GB (for
// example "1024", "1GB"); both are required for Fargate. The execution-role
// and task-role inputs take role ARNs.
type TaskDefinition struct {
	Family                  string                               `ub:"family"`
	ContainerDefinitions    []TaskDefinitionContainerDefinition  `ub:"container-definitions"`
	Cpu                     *string                              `ub:"cpu"`
	EnableFaultInjection    *bool                                `ub:"enable-fault-injection"`
	EphemeralStorage        *TaskDefinitionEphemeralStorage      `ub:"ephemeral-storage"`
	ExecutionRoleArn        *string                              `ub:"execution-role-arn"`
	IpcMode                 *string                              `ub:"ipc-mode"`
	Memory                  *string                              `ub:"memory"`
	NetworkMode             *string                              `ub:"network-mode"`
	PidMode                 *string                              `ub:"pid-mode"`
	PlacementConstraints    *[]TaskDefinitionPlacementConstraint `ub:"placement-constraints"`
	ProxyConfiguration      *TaskDefinitionProxyConfiguration    `ub:"proxy-configuration"`
	RequiresCompatibilities *[]string                            `ub:"requires-compatibilities"`
	RuntimePlatform         *TaskDefinitionRuntimePlatform       `ub:"runtime-platform"`
	TaskRoleArn             *string                              `ub:"task-role-arn"`
	Volumes                 *[]TaskDefinitionVolume              `ub:"volumes"`
	Tags                    *map[string]string                   `ub:"tags"`
}

// TaskDefinitionOutput holds the values ECS computes at registration. The
// revision-pinned ARN is the identity handle: Read describes it and Delete
// deregisters it, both from the prior outputs, because resolving by family
// finds the latest ACTIVE revision, which on a replace is the new one.
// ArnWithoutRevision is the family-level form of the ARN, derived
// client-side for downstream references such as IAM policies.
type TaskDefinitionOutput struct {
	Arn                string `ub:"arn"`
	Revision           int64  `ub:"revision"`
	ArnWithoutRevision string `ub:"arn-without-revision"`
}

func (r *TaskDefinition) SchemaVersion() int { return 1 }

// ReplaceFields lists every input except tags: a registered revision is
// immutable, so any change other than tags registers a replacement revision.
func (r *TaskDefinition) ReplaceFields() []string {
	return []string{
		"family",
		"container-definitions",
		"cpu",
		"enable-fault-injection",
		"ephemeral-storage",
		"execution-role-arn",
		"ipc-mode",
		"memory",
		"network-mode",
		"pid-mode",
		"placement-constraints",
		"proxy-configuration",
		"requires-compatibilities",
		"runtime-platform",
		"task-role-arn",
		"volumes",
	}
}

// Constraints declares the rules ECS places on a task definition's inputs:
// the mode and platform enums, the ephemeral storage size range, the
// placement constraint rules (at most 10, type memberOf with an expression),
// the per-container version consistency enum, and the volume rules (docker
// scope and EFS enums, transit encryption port ranges, and the FSx
// authorization-config block the API requires). The family character rule is
// a regular-expression check in Create, and the enums nested inside a
// container's optional lists, such as a port mapping protocol or a
// dependency condition, are enforced by the API.
func (r TaskDefinition) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.NetworkMode)).
			Require(constraint.OneOf(r.NetworkMode, "bridge", "host", "awsvpc", "none")).
			Message("network-mode must be bridge, host, awsvpc, or none"),
		constraint.When(constraint.Present(r.IpcMode)).
			Require(constraint.OneOf(r.IpcMode, "host", "task", "none")).
			Message("ipc-mode must be host, task, or none"),
		constraint.When(constraint.Present(r.PidMode)).
			Require(constraint.OneOf(r.PidMode, "host", "task")).
			Message("pid-mode must be host or task"),
		constraint.ForEach(r.RequiresCompatibilities, func(c string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(c,
					"EC2", "FARGATE", "EXTERNAL", "MANAGED_INSTANCES")).
					Message("a compatibility must be EC2, FARGATE, EXTERNAL," +
						" or MANAGED_INSTANCES"),
			}
		}),
		constraint.When(constraint.Present(r.EphemeralStorage.SizeInGiB)).
			Require(constraint.AtLeast(r.EphemeralStorage.SizeInGiB, 21),
				constraint.AtMost(r.EphemeralStorage.SizeInGiB, 200)).
			Message("ephemeral-storage size-in-gib must be between 21 and 200"),
		constraint.When(constraint.Present(r.ProxyConfiguration.Type)).
			Require(constraint.OneOf(r.ProxyConfiguration.Type, "APPMESH")).
			Message("proxy-configuration type must be APPMESH"),
		constraint.When(constraint.Present(r.RuntimePlatform.CpuArchitecture)).
			Require(constraint.OneOf(r.RuntimePlatform.CpuArchitecture, "X86_64", "ARM64")).
			Message("runtime-platform cpu-architecture must be X86_64 or ARM64"),
		constraint.When(constraint.Present(r.RuntimePlatform.OperatingSystemFamily)).
			Require(constraint.OneOf(r.RuntimePlatform.OperatingSystemFamily,
				"LINUX", "WINDOWS_SERVER_2016_FULL", "WINDOWS_SERVER_2019_CORE",
				"WINDOWS_SERVER_2019_FULL", "WINDOWS_SERVER_2004_CORE",
				"WINDOWS_SERVER_2022_CORE", "WINDOWS_SERVER_2022_FULL",
				"WINDOWS_SERVER_2025_CORE", "WINDOWS_SERVER_2025_FULL",
				"WINDOWS_SERVER_20H2_CORE")).
			Message("runtime-platform operating-system-family must be LINUX or a" +
				" WINDOWS_SERVER family"),
		constraint.Must(constraint.MaxItems(r.PlacementConstraints, 10)).
			Message("placement-constraints allows at most 10 entries"),
		constraint.ForEach(r.PlacementConstraints,
			func(c TaskDefinitionPlacementConstraint) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(c.Type, "memberOf")).
						Message("a task definition placement constraint type" +
							" must be memberOf"),
					constraint.When(constraint.Equals(c.Type, "memberOf")).
						Require(constraint.Present(c.Expression)).
						Message("a memberOf placement constraint requires an expression"),
				}
			}),
		constraint.ForEach(r.ContainerDefinitions,
			func(c TaskDefinitionContainerDefinition) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.When(constraint.Present(c.VersionConsistency)).
						Require(constraint.OneOf(c.VersionConsistency,
							"enabled", "disabled")).
						Message("version-consistency must be enabled or disabled"),
				}
			}),
		constraint.ForEach(r.Volumes, func(v TaskDefinitionVolume) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.When(constraint.Present(v.DockerVolumeConfiguration.Scope)).
					Require(constraint.OneOf(v.DockerVolumeConfiguration.Scope,
						"task", "shared")).
					Message("a docker volume scope must be task or shared"),
				constraint.When(constraint.Present(
					v.EfsVolumeConfiguration.TransitEncryption)).
					Require(constraint.OneOf(v.EfsVolumeConfiguration.TransitEncryption,
						"ENABLED", "DISABLED")).
					Message("efs transit-encryption must be ENABLED or DISABLED"),
				constraint.When(constraint.Present(
					v.EfsVolumeConfiguration.TransitEncryptionPort)).
					Require(
						constraint.AtLeast(
							v.EfsVolumeConfiguration.TransitEncryptionPort, 1),
						constraint.AtMost(
							v.EfsVolumeConfiguration.TransitEncryptionPort, 65535)).
					Message("efs transit-encryption-port must be between 1 and 65535"),
				constraint.When(constraint.Present(
					v.EfsVolumeConfiguration.AuthorizationConfig.Iam)).
					Require(constraint.OneOf(
						v.EfsVolumeConfiguration.AuthorizationConfig.Iam,
						"ENABLED", "DISABLED")).
					Message("efs authorization-config iam must be ENABLED or DISABLED"),
				constraint.When(constraint.Present(
					v.FsxWindowsFileServerVolumeConfiguration)).
					Require(constraint.Present(
						v.FsxWindowsFileServerVolumeConfiguration.AuthorizationConfig)).
					Message("an fsx-windows-file-server volume requires" +
						" authorization-config"),
				constraint.When(constraint.Present(
					v.S3filesVolumeConfiguration.TransitEncryptionPort)).
					Require(
						constraint.AtLeast(
							v.S3filesVolumeConfiguration.TransitEncryptionPort, 1),
						constraint.AtMost(
							v.S3filesVolumeConfiguration.TransitEncryptionPort, 65535)).
					Message("s3files transit-encryption-port must be between 1 and 65535"),
			}
		}),
	}
}

// Create registers the revision with one RegisterTaskDefinition call. The
// response is fully settled, so the outputs come straight from it with no
// follow-up describe, wait, or retry. Some partitions, such as the ISO
// partitions, cannot tag a task definition as it is registered; when the
// tagged register fails for that reason, the revision is registered without
// tags and tagged right after.
func (r *TaskDefinition) Create(ctx context.Context, cfg *awsCfg) (*TaskDefinitionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if !taskDefinitionFamilyRegexp.MatchString(r.Family) {
		return nil, fmt.Errorf("family %q must match %s",
			r.Family, taskDefinitionFamilyRegexp.String())
	}
	in := r.registerInput()
	resp, err := client.RegisterTaskDefinition(ctx, in)
	taggedSeparately := false
	if err != nil && in.Tags != nil && partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = client.RegisterTaskDefinition(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("register task definition: %w", err)
	}
	if resp.TaskDefinition == nil {
		return nil, errors.New("register task definition: response holds no task definition")
	}
	pinned := aws.ToString(resp.TaskDefinition.TaskDefinitionArn)
	if taggedSeparately && len(ptr.Value(r.Tags)) > 0 {
		if _, err := client.TagResource(ctx, &ecs.TagResourceInput{
			ResourceArn: aws.String(pinned),
			Tags:        tagsSDK(ptr.Value(r.Tags)),
		}); err != nil {
			return nil, fmt.Errorf("tag task definition: %w", err)
		}
	}
	return &TaskDefinitionOutput{
		Arn:                pinned,
		Revision:           int64(resp.TaskDefinition.Revision),
		ArnWithoutRevision: taskDefinitionARNWithoutRevision(pinned),
	}, nil
}

// Read describes the revision by the prior pinned ARN. A revision that was
// deregistered out of band still describes successfully, just with an
// INACTIVE or DELETE_IN_PROGRESS status, so the status check is the second
// half of not-found detection.
func (r *TaskDefinition) Read(
	ctx context.Context, cfg *awsCfg, prior *TaskDefinitionOutput,
) (*TaskDefinitionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(prior.Arn),
	})
	if err != nil {
		if taskDefinitionGone(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe task definition: %w", err)
	}
	td := resp.TaskDefinition
	if td == nil {
		return nil, runtime.ErrNotFound
	}
	switch td.Status {
	case ecstypes.TaskDefinitionStatusInactive, ecstypes.TaskDefinitionStatusDeleteInProgress:
		return nil, runtime.ErrNotFound
	}
	pinned := aws.ToString(td.TaskDefinitionArn)
	return &TaskDefinitionOutput{
		Arn:                pinned,
		Revision:           int64(td.Revision),
		ArnWithoutRevision: taskDefinitionARNWithoutRevision(pinned),
	}, nil
}

// Update reconciles tags, the only mutable aspect of a registered revision.
// The outputs cannot change while the revision lives, so the prior outputs
// are returned as is.
func (r *TaskDefinition) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[TaskDefinition, *TaskDefinitionOutput],
) (*TaskDefinitionOutput, error) {
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		client, err := newClient(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if err := syncResourceTags(ctx, client, prior.Outputs.Arn, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return prior.Outputs, nil
}

// Delete deregisters the revision by the prior pinned ARN. Resolving by
// family instead would find the latest ACTIVE revision, which on a replace
// is the replacement just registered. Deregistration leaves the revision
// describable as INACTIVE; tasks and services already running it keep
// working.
func (r *TaskDefinition) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *TaskDefinitionOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeregisterTaskDefinition(ctx, &ecs.DeregisterTaskDefinitionInput{
		TaskDefinition: aws.String(prior.Arn),
	})
	if err != nil {
		// An HTTP 400 means the revision is already gone or already on the
		// way out: deregistered, deleted out of band, or in the middle of an
		// out-of-band DeleteTaskDefinitions, which deregister reports as a
		// ClientException naming a deletion in progress. All count as
		// deleted.
		if taskDefinitionGone(err) {
			return nil
		}
		return fmt.Errorf("deregister task definition: %w", err)
	}
	return nil
}

// registerInput assembles the RegisterTaskDefinition request from every
// input; one call registers the whole revision, tags included.
func (r *TaskDefinition) registerInput() *ecs.RegisterTaskDefinitionInput {
	in := &ecs.RegisterTaskDefinitionInput{
		Family:               aws.String(r.Family),
		ContainerDefinitions: taskDefinitionContainersSDK(r.ContainerDefinitions),
		Cpu:                  r.Cpu,
		Memory:               r.Memory,
		EnableFaultInjection: r.EnableFaultInjection,
		EphemeralStorage:     r.EphemeralStorage.sdk(),
		ExecutionRoleArn:     r.ExecutionRoleArn,
		TaskRoleArn:          r.TaskRoleArn,
		PlacementConstraints: taskDefinitionPlacementConstraintsSDK(ptr.Value(r.PlacementConstraints)),
		ProxyConfiguration:   r.ProxyConfiguration.sdk(),
		RuntimePlatform:      r.RuntimePlatform.sdk(),
		Volumes:              taskDefinitionVolumesSDK(ptr.Value(r.Volumes)),
		Tags:                 tagsSDK(ptr.Value(r.Tags)),
	}
	if r.NetworkMode != nil {
		in.NetworkMode = ecstypes.NetworkMode(*r.NetworkMode)
	}
	if r.IpcMode != nil {
		in.IpcMode = ecstypes.IpcMode(*r.IpcMode)
	}
	if r.PidMode != nil {
		in.PidMode = ecstypes.PidMode(*r.PidMode)
	}
	for _, c := range ptr.Value(r.RequiresCompatibilities) {
		in.RequiresCompatibilities = append(in.RequiresCompatibilities,
			ecstypes.Compatibility(c))
	}
	return in
}

// taskDefinitionGone reports whether err is ECS telling the caller a task
// definition does not exist. ECS has no typed not-found exception for task
// definitions: a describe or deregister of a missing, fully deleted, or
// never-registered revision fails with a ClientException on an HTTP 400
// response, so the status is matched rather than an error code.
func taskDefinitionGone(err error) bool {
	var respErr *smithyhttp.ResponseError
	return errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusBadRequest
}

// taskDefinitionARNWithoutRevision returns the family-level form of a
// revision-pinned task definition ARN, removing the :revision suffix from
// the resource part. The API does not return this form, so it is derived
// client-side; an ARN not in the expected form is returned unmodified.
func taskDefinitionARNWithoutRevision(pinned string) string {
	parsed, err := arn.Parse(pinned)
	if err != nil {
		return pinned
	}
	parts := strings.Split(parsed.Resource, ":")
	if len(parts) != 2 {
		return pinned
	}
	parsed.Resource = parts[0]
	return parsed.String()
}
