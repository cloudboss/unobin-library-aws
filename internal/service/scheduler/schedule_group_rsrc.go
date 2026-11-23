package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

const scheduleGroupSuffixBytes = 13

type ScheduleGroupResource struct {
	Name       *string            `ub:"name"`
	NamePrefix *string            `ub:"name-prefix"`
	Tags       *map[string]string `ub:"tags"`
}

type ScheduleGroupResourceOutput struct {
	ARN                  string            `ub:"arn"`
	Name                 string            `ub:"name"`
	NamePrefix           *string           `ub:"name-prefix"`
	CreationDate         string            `ub:"creation-date"`
	LastModificationDate string            `ub:"last-modification-date"`
	State                string            `ub:"state"`
	Region               string            `ub:"region"`
	Tags                 map[string]string `ub:"tags"`
}

type scheduleGroupPrior = runtime.Prior[
	ScheduleGroupResource, *ScheduleGroupResourceOutput, *awsCfg,
]
type scheduleGroupDefinition = runtime.ResourceDefinition[
	ScheduleGroupResource, *ScheduleGroupResourceOutput, *awsCfg,
]

type scheduleGroupClient interface {
	CreateScheduleGroup(context.Context, *schedulerapi.CreateScheduleGroupInput,
		...func(*schedulerapi.Options)) (*schedulerapi.CreateScheduleGroupOutput, error)
	GetScheduleGroup(context.Context, *schedulerapi.GetScheduleGroupInput,
		...func(*schedulerapi.Options)) (*schedulerapi.GetScheduleGroupOutput, error)
	DeleteScheduleGroup(context.Context, *schedulerapi.DeleteScheduleGroupInput,
		...func(*schedulerapi.Options)) (*schedulerapi.DeleteScheduleGroupOutput, error)
	ListTagsForResource(context.Context, *schedulerapi.ListTagsForResourceInput,
		...func(*schedulerapi.Options)) (*schedulerapi.ListTagsForResourceOutput, error)
	TagResource(context.Context, *schedulerapi.TagResourceInput,
		...func(*schedulerapi.Options)) (*schedulerapi.TagResourceOutput, error)
	UntagResource(context.Context, *schedulerapi.UntagResourceInput,
		...func(*schedulerapi.Options)) (*schedulerapi.UntagResourceOutput, error)
}

type scheduleGroupClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

func (r ScheduleGroupResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.Name, r.NamePrefix),
		constraint.Must(constraint.IsMatch(r.Name, "^[0-9A-Za-z_.-]{1,64}$")),
		constraint.Must(constraint.IsMatch(r.NamePrefix, "^[0-9A-Za-z_.-]{1,38}$")),
	}
}

func (r *ScheduleGroupResource) ResourceDefinition() scheduleGroupDefinition {
	tags := runtime.InputField(func(input *ScheduleGroupResource) **map[string]string {
		return &input.Tags
	})
	return scheduleGroupDefinition{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[ScheduleGroupResource]{
			runtime.EqualBy(tags, func(prior, desired *map[string]string) bool {
				return maps.Equal(scheduleGroupUserTags(ptr.Value(prior)),
					scheduleGroupUserTags(ptr.Value(desired)))
			}),
		},
		Replace: runtime.Replacement[ScheduleGroupResource, *ScheduleGroupResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ScheduleGroupResource]{
				runtime.InputField(func(input *ScheduleGroupResource) **string {
					return &input.Name
				}),
				runtime.InputField(func(input *ScheduleGroupResource) **string {
					return &input.NamePrefix
				}),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(_ ScheduleGroupResource, output *ScheduleGroupResourceOutput) (string, error) {
			if err := checkScheduleGroupIdentity(output); err != nil {
				return "", err
			}
			return output.ARN, nil
		},
	}
}

func (r *ScheduleGroupResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*ScheduleGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createScheduleGroup(ctx, client, client.Options().Region, rand.Reader,
		systemScheduleGroupClock{})
}

func (r *ScheduleGroupResource) Read(
	ctx context.Context, cfg *awsCfg, prior scheduleGroupPrior,
) (*ScheduleGroupResourceOutput, error) {
	client, err := existingScheduleGroupClient(ctx, scheduleGroupPriorConfig(cfg, prior),
		prior.Outputs)
	if err != nil {
		return nil, err
	}
	return readScheduleGroup(ctx, client, *prior.Outputs)
}

func (r *ScheduleGroupResource) Update(
	ctx context.Context, cfg *awsCfg, prior scheduleGroupPrior,
) (*ScheduleGroupResourceOutput, error) {
	client, err := existingScheduleGroupClient(ctx, cfg, prior.Outputs)
	if err != nil {
		return nil, err
	}
	return r.updateScheduleGroup(ctx, client, prior)
}

func (r *ScheduleGroupResource) Delete(
	ctx context.Context, cfg *awsCfg, prior scheduleGroupPrior,
) error {
	client, err := existingScheduleGroupClient(ctx, scheduleGroupPriorConfig(cfg, prior),
		prior.Outputs)
	if err != nil {
		return err
	}
	return deleteScheduleGroup(ctx, client, *prior.Outputs, systemScheduleGroupClock{})
}

func (r *ScheduleGroupResource) createScheduleGroup(
	ctx context.Context, client scheduleGroupClient, region string, random io.Reader,
	clock scheduleGroupClock,
) (*ScheduleGroupResourceOutput, error) {
	input, err := r.prepareScheduleGroupCreate(random)
	if err != nil {
		return nil, err
	}
	name := aws.ToString(input.Name)
	if _, err := client.CreateScheduleGroup(ctx, input); err != nil {
		return nil, fmt.Errorf("create schedule group %s: %w", name, err)
	}
	identity := ScheduleGroupResourceOutput{Name: name, Region: region}
	if err := waitScheduleGroup(ctx, client, identity, clock, true); err != nil {
		return nil, fmt.Errorf("wait for schedule group %s to become active: %w", name, err)
	}
	return readScheduleGroupAfterMutation(ctx, client, identity)
}

func readScheduleGroup(
	ctx context.Context, client scheduleGroupClient, identity ScheduleGroupResourceOutput,
) (*ScheduleGroupResourceOutput, error) {
	result, err := getScheduleGroup(ctx, client, identity)
	if err != nil {
		return nil, err
	}
	identity.ARN = aws.ToString(result.Arn)
	identity.NamePrefix = scheduleGroupNamePrefix(identity.Name)
	identity.CreationDate = aws.ToTime(result.CreationDate).Format(time.RFC3339)
	identity.LastModificationDate = aws.ToTime(result.LastModificationDate).Format(time.RFC3339)
	identity.State = string(result.State)
	identity.Tags, err = listScheduleGroupTags(ctx, client, identity)
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func (r *ScheduleGroupResource) updateScheduleGroup(
	ctx context.Context, client scheduleGroupClient, prior scheduleGroupPrior,
) (*ScheduleGroupResourceOutput, error) {
	if err := checkScheduleGroupIdentity(prior.Outputs); err != nil {
		return nil, err
	}
	current := prior.Outputs
	if prior.Observed != nil {
		if prior.Observed.ARN != current.ARN || prior.Observed.Name != current.Name ||
			prior.Observed.Region != current.Region {
			return nil, fmt.Errorf("schedule group %s observation identity does not match saved state",
				current.Name)
		}
		current = prior.Observed
	}
	if err := syncScheduleGroupTags(ctx, client, current, ptr.Value(r.Tags)); err != nil {
		return nil, err
	}
	return readScheduleGroupAfterMutation(ctx, client, *current)
}

func deleteScheduleGroup(
	ctx context.Context, client scheduleGroupClient, identity ScheduleGroupResourceOutput,
	clock scheduleGroupClock,
) error {
	_, err := client.DeleteScheduleGroup(ctx, &schedulerapi.DeleteScheduleGroupInput{
		Name: aws.String(identity.Name),
	})
	if isScheduleNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete schedule group %s: %w", identity.Name, err)
	}
	if err := waitScheduleGroup(ctx, client, identity, clock, false); err != nil {
		return fmt.Errorf("wait for schedule group %s to be deleted: %w", identity.Name, err)
	}
	return nil
}

func scheduleGroupPriorConfig(cfg *awsCfg, prior scheduleGroupPrior) *awsCfg {
	if prior.Configuration != nil {
		return prior.Configuration
	}
	return cfg
}

func existingScheduleGroupClient(
	ctx context.Context, cfg *awsCfg, identity *ScheduleGroupResourceOutput,
) (*schedulerapi.Client, error) {
	if err := checkScheduleGroupIdentity(identity); err != nil {
		return nil, err
	}
	saved := ptr.Value(cfg)
	saved.Region = aws.String(identity.Region)
	return newClient(ctx, &saved)
}

func (r ScheduleGroupResource) prepareScheduleGroupCreate(
	random io.Reader,
) (*schedulerapi.CreateScheduleGroupInput, error) {
	name := r.Name
	if name == nil {
		prefix := "unobin-"
		if r.NamePrefix != nil {
			prefix = *r.NamePrefix
		}
		var suffix [scheduleGroupSuffixBytes]byte
		if _, err := io.ReadFull(random, suffix[:]); err != nil {
			return nil, fmt.Errorf("generate schedule group name: %w", err)
		}
		name = aws.String(prefix + hex.EncodeToString(suffix[:]))
	}
	return &schedulerapi.CreateScheduleGroupInput{
		Name: name,
		Tags: expandScheduleGroupTags(scheduleGroupUserTags(ptr.Value(r.Tags))),
	}, nil
}

func scheduleGroupNamePrefix(name string) *string {
	start := len(name) - hex.EncodedLen(scheduleGroupSuffixBytes)
	if start <= 0 {
		return nil
	}
	if _, err := hex.DecodeString(name[start:]); err != nil {
		return nil
	}
	return aws.String(name[:start])
}

func checkScheduleGroupIdentity(identity *ScheduleGroupResourceOutput) error {
	if identity == nil || identity.Name == "" || identity.Region == "" {
		return errors.New("schedule group identity requires name and region")
	}
	return checkScheduleGroupARN(identity, identity.ARN)
}

func checkScheduleGroupARN(identity *ScheduleGroupResourceOutput, value string) error {
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return fmt.Errorf("schedule group %s has invalid ARN %q: %w", identity.Name, value, err)
	}
	if parsed.Partition == "" || parsed.AccountID == "" || parsed.Service != "scheduler" ||
		parsed.Region != identity.Region || strings.Contains(identity.Name, "/") ||
		parsed.Resource != "schedule-group/"+identity.Name ||
		(identity.ARN != "" && value != identity.ARN) {
		return fmt.Errorf("schedule group %s identity does not match ARN %q", identity.Name, value)
	}
	return nil
}

func getScheduleGroup(
	ctx context.Context, client scheduleGroupClient, identity ScheduleGroupResourceOutput,
) (*schedulerapi.GetScheduleGroupOutput, error) {
	out, err := client.GetScheduleGroup(ctx, &schedulerapi.GetScheduleGroupInput{
		Name: aws.String(identity.Name),
	})
	if isScheduleNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get schedule group %s: %w", identity.Name, err)
	}
	if out == nil || out.Arn == nil {
		return nil, runtime.ErrNotFound
	}
	if err := checkScheduleGroupARN(&identity, *out.Arn); err != nil {
		return nil, err
	}
	if out.Name != nil && *out.Name != identity.Name {
		return nil, fmt.Errorf("get schedule group %s returned name %q", identity.Name, *out.Name)
	}
	return out, nil
}

func readScheduleGroupAfterMutation(
	ctx context.Context, client scheduleGroupClient, identity ScheduleGroupResourceOutput,
) (*ScheduleGroupResourceOutput, error) {
	out, err := readScheduleGroup(ctx, client, identity)
	if errors.Is(err, runtime.ErrNotFound) {
		return nil, fmt.Errorf("schedule group %s is absent on final read", identity.Name)
	}
	return out, err
}

func scheduleGroupUserTags(tags map[string]string) map[string]string {
	filtered := make(map[string]string, len(tags))
	for key, value := range tags {
		if !strings.HasPrefix(key, "aws:") {
			filtered[key] = value
		}
	}
	return filtered
}

func expandScheduleGroupTags(tags map[string]string) []schedulertypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]schedulertypes.Tag, 0, len(tags))
	for _, key := range slices.Sorted(maps.Keys(tags)) {
		out = append(out, schedulertypes.Tag{Key: aws.String(key), Value: aws.String(tags[key])})
	}
	return out
}

func listScheduleGroupTags(
	ctx context.Context, client scheduleGroupClient, identity ScheduleGroupResourceOutput,
) (map[string]string, error) {
	out, err := client.ListTagsForResource(ctx, &schedulerapi.ListTagsForResourceInput{
		ResourceArn: aws.String(identity.ARN),
	})
	if err != nil && !partition.UnsupportedOperation(identity.Region, err) {
		return nil, fmt.Errorf("list schedule group tags %s: %w", identity.ARN, err)
	}
	tags := map[string]string{}
	if err == nil && out != nil {
		for _, tag := range out.Tags {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}
	return scheduleGroupUserTags(tags), nil
}

func syncScheduleGroupTags(
	ctx context.Context, client scheduleGroupClient, current *ScheduleGroupResourceOutput,
	desired map[string]string,
) error {
	upserts, removals := tagsync.Diff(current.Tags, desired)
	if len(removals) != 0 {
		_, err := client.UntagResource(ctx, &schedulerapi.UntagResourceInput{
			ResourceArn: aws.String(current.ARN), TagKeys: removals,
		})
		if err != nil {
			if partition.UnsupportedOperation(current.Region, err) {
				return nil
			}
			return fmt.Errorf("untag schedule group %s: %w", current.ARN, err)
		}
	}
	if len(upserts) != 0 {
		_, err := client.TagResource(ctx, &schedulerapi.TagResourceInput{
			ResourceArn: aws.String(current.ARN), Tags: expandScheduleGroupTags(upserts),
		})
		if err != nil && !partition.UnsupportedOperation(current.Region, err) {
			return fmt.Errorf("tag schedule group %s: %w", current.ARN, err)
		}
	}
	return nil
}
