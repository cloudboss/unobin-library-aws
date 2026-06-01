package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/library/internal/iamhelpers"
	"github.com/cloudboss/unobin-library-aws/library/internal/partition"
	"github.com/cloudboss/unobin-library-aws/library/internal/retry"
	"github.com/cloudboss/unobin-library-aws/library/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/library/internal/wait"
)

// IamInstanceProfile is a container that an EC2 instance assumes to gain
// the permissions of a single IAM role. The name and path fix the
// profile's identity, so changing either replaces the profile. The role
// is the one role the profile holds; it is attached after the profile
// exists and can be swapped in place. Tags are reconciled to match the
// configuration on every apply.
type IamInstanceProfile struct {
	InstanceProfileName string            `ub:"instance-profile-name"`
	Path                *string           `ub:"path"`
	Role                *string           `ub:"role"`
	Tags                map[string]string `ub:"tags"`
}

// IamInstanceProfileOutput holds the values the IAM API computes for an
// instance profile. The name, path, and role are configuration inputs and
// are referenced from the input, so they are not echoed here.
type IamInstanceProfileOutput struct {
	Arn               string `ub:"arn"`
	InstanceProfileId string `ub:"instance-profile-id"`
	CreateDate        string `ub:"create-date"`
}

func (r *IamInstanceProfile) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that fix the profile's identity. The name
// and path cannot change on an existing profile, so a change to either
// forces a replace. The role is left out because it is attached and
// detached in place during Update.
func (r *IamInstanceProfile) ReplaceFields() []string {
	return []string{
		"instance-profile-name",
		"path",
	}
}

func (r *IamInstanceProfile) Create(
	ctx context.Context, cfg any,
) (*IamInstanceProfileOutput, error) {
	client, err := iamhelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &iam.CreateInstanceProfileInput{
		InstanceProfileName: aws.String(r.InstanceProfileName),
		Path:                r.Path,
		Tags:                instanceProfileTags(r.Tags),
	}
	_, err = client.CreateInstanceProfile(ctx, in)
	// Some partitions, such as the ISO partitions, cannot tag a profile as
	// it is created. When the tagged create fails for that reason, create
	// the profile without tags and apply them with a separate call below.
	taggedSeparately := false
	if err != nil && in.Tags != nil &&
		partition.UnsupportedOperation(iamhelpers.Region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		_, err = client.CreateInstanceProfile(ctx, in)
	}
	if err != nil {
		return nil, err
	}
	if r.Role != nil && *r.Role != "" {
		if err := r.addRole(ctx, client, *r.Role); err != nil {
			return nil, err
		}
	}
	if taggedSeparately && len(r.Tags) > 0 {
		if err := r.syncTags(ctx, client); err != nil {
			return nil, err
		}
	}
	// Read settles the eventual consistency that follows a create: it waits for
	// the profile to become visible and for its ARN to take its final form,
	// which is what belongs in the output.
	return r.read(ctx, client, true)
}

func (r *IamInstanceProfile) Read(
	ctx context.Context, cfg any, prior *IamInstanceProfileOutput,
) (*IamInstanceProfileOutput, error) {
	client, err := iamhelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, false)
}

// read fetches the profile and returns its computed outputs. Just after a
// create IAM is eventually consistent in two ways this absorbs: the profile
// can read as absent, and it can come back with an ARN that is not yet
// well-formed. When created is true the profile was just made, so a missing
// profile means it has not propagated yet and read waits; otherwise a missing
// profile is drift and maps to runtime.ErrNotFound. In both cases read waits
// for a well-formed ARN, so an unsettled ARN never reaches the output.
//
// On a create the ARN can also flap between replicas, so read requires it
// well-formed on a few consecutive reads before trusting it; a steady-state
// read takes the first well-formed ARN, since by then it has settled.
func (r *IamInstanceProfile) read(
	ctx context.Context, client *iam.Client, created bool,
) (*IamInstanceProfileOutput, error) {
	var profile *iamtypes.InstanceProfile
	probe := func(ctx context.Context) (bool, error) {
		resp, err := client.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
			InstanceProfileName: aws.String(r.InstanceProfileName),
		})
		if err != nil {
			if iamhelpers.IsNotFound(err) {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			return false, fmt.Errorf("get instance profile: %w", err)
		}
		if resp.InstanceProfile == nil {
			if created {
				return false, nil
			}
			return false, runtime.ErrNotFound
		}
		profile = resp.InstanceProfile
		return arn.IsARN(aws.ToString(profile.Arn)), nil
	}
	what := fmt.Sprintf("instance profile %s", r.InstanceProfileName)
	var err error
	if created {
		err = wait.UntilStable(ctx, what, 3, probe)
	} else {
		err = wait.Until(ctx, what, probe)
	}
	if err != nil {
		return nil, err
	}
	return instanceProfileOutput(profile), nil
}

func (r *IamInstanceProfile) Update(
	ctx context.Context, cfg any, prior runtime.Prior[IamInstanceProfile, *IamInstanceProfileOutput],
) (*IamInstanceProfileOutput, error) {
	client, err := iamhelpers.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.Role, r.Role) {
		if old := aws.ToString(prior.Inputs.Role); old != "" {
			if err := r.removeRole(ctx, client, old); err != nil {
				return nil, err
			}
		}
		if r.Role != nil && *r.Role != "" {
			if err := r.addRole(ctx, client, *r.Role); err != nil {
				return nil, err
			}
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client); err != nil {
			return nil, err
		}
	}
	return prior.Outputs, nil
}

func (r *IamInstanceProfile) Delete(
	ctx context.Context, cfg any, prior *IamInstanceProfileOutput,
) error {
	client, err := iamhelpers.NewClient(ctx, cfg)
	if err != nil {
		return err
	}
	// A profile keeps its delete from succeeding while a role is still
	// attached, so detach the configured role first.
	if r.Role != nil && *r.Role != "" {
		if err := r.removeRole(ctx, client, *r.Role); err != nil {
			return err
		}
	}
	_, err = client.DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(r.InstanceProfileName),
	})
	if err != nil {
		if iamhelpers.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (r *IamInstanceProfile) addRole(
	ctx context.Context, client *iam.Client, role string,
) error {
	// A profile or role created moments earlier may not have propagated to the
	// add call yet, which IAM reports as a transient parameter or not-found
	// error; retry through it.
	return retry.OnError(ctx, iamhelpers.IsRoleNotYetPropagated,
		func(ctx context.Context) error {
			_, err := client.AddRoleToInstanceProfile(ctx, &iam.AddRoleToInstanceProfileInput{
				InstanceProfileName: aws.String(r.InstanceProfileName),
				RoleName:            aws.String(role),
			})
			return err
		})
}

func (r *IamInstanceProfile) removeRole(
	ctx context.Context, client *iam.Client, role string,
) error {
	_, err := client.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String(r.InstanceProfileName),
		RoleName:            aws.String(role),
	})
	if err != nil {
		if iamhelpers.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// syncTags reconciles the profile's tags so they match the configuration.
// It is the IAM instance profile binding of tagsync.Sync, reading the
// current tags with the list-tags paginator and applying changes through
// the IAM tag and untag calls, which upsert and remove by key.
func (r *IamInstanceProfile) syncTags(ctx context.Context, client *iam.Client) error {
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			out := make(map[string]string)
			pages := iam.NewListInstanceProfileTagsPaginator(
				client, &iam.ListInstanceProfileTagsInput{
					InstanceProfileName: aws.String(r.InstanceProfileName),
				})
			for pages.HasMorePages() {
				page, err := pages.NextPage(ctx)
				if err != nil {
					return nil, err
				}
				for _, t := range page.Tags {
					out[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
			}
			return out, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagInstanceProfile(ctx, &iam.TagInstanceProfileInput{
				InstanceProfileName: aws.String(r.InstanceProfileName),
				Tags:                tagsFromMap(upsert),
			})
			return err
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagInstanceProfile(ctx, &iam.UntagInstanceProfileInput{
				InstanceProfileName: aws.String(r.InstanceProfileName),
				TagKeys:             remove,
			})
			return err
		},
	)
}

func instanceProfileOutput(p *iamtypes.InstanceProfile) *IamInstanceProfileOutput {
	out := &IamInstanceProfileOutput{
		Arn:               aws.ToString(p.Arn),
		InstanceProfileId: aws.ToString(p.InstanceProfileId),
	}
	if p.CreateDate != nil {
		out.CreateDate = p.CreateDate.UTC().Format(time.RFC3339)
	}
	return out
}

// instanceProfileTags converts the configuration's tag map to the IAM tag
// slice the create call takes. It returns nil for an empty map so no tags
// are sent.
func instanceProfileTags(tags map[string]string) []iamtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	return tagsFromMap(tags)
}

func tagsFromMap(m map[string]string) []iamtypes.Tag {
	tags := make([]iamtypes.Tag, 0, len(m))
	for k, v := range m {
		tags = append(tags, iamtypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return tags
}
