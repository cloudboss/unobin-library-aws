package iam

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// maxPolicyVersions is the number of versions IAM keeps for a managed
// policy. Before adding a new version once the policy is full, the oldest
// non-default version is removed to make room.
const maxPolicyVersions = 5

// Policy manages an IAM customer managed policy. The policy document is
// the permission set the policy grants; a change to it is applied in place
// by adding a new default version. The name, path, and description are
// fixed at create time: IAM cannot rename or re-path a policy, and it
// treats the description as immutable, so a change to any of them recreates
// the policy.
type Policy struct {
	PolicyName     string            `ub:"policy-name"`
	PolicyDocument string            `ub:"policy-document"`
	Path           *string           `ub:"path"`
	Description    *string           `ub:"description"`
	Tags           map[string]string `ub:"tags"`
}

// PolicyOutput holds the values IAM computes for a managed policy. The
// ARN is the policy's identity, used to read, update, and delete it.
type PolicyOutput struct {
	Arn              string `ub:"arn"`
	PolicyId         string `ub:"policy-id"`
	DefaultVersionId string `ub:"default-version-id"`
	AttachmentCount  int64  `ub:"attachment-count"`
	CreateDate       string `ub:"create-date"`
}

func (r *Policy) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that IAM cannot change on an existing
// policy. The name and path are part of the policy's ARN, and IAM treats
// the description as immutable once set, so a change to any of them
// requires replacing the policy.
func (r *Policy) ReplaceFields() []string {
	return []string{
		"policy-name",
		"path",
		"description",
	}
}

// Defaults marks the collection inputs a policy may omit.
func (r Policy) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

func (r *Policy) Create(ctx context.Context, cfg any) (*PolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &iam.CreatePolicyInput{
		PolicyName:     aws.String(r.PolicyName),
		PolicyDocument: aws.String(r.PolicyDocument),
		Path:           r.Path,
		Description:    r.Description,
		Tags:           toIamTags(r.Tags),
	}
	resp, err := client.CreatePolicy(ctx, in)
	// Some partitions, such as the ISO partitions, cannot tag a policy as it
	// is created. When the tagged create fails for that reason, create the
	// policy without tags and apply them with a separate call below.
	taggedSeparately := false
	if err != nil && in.Tags != nil &&
		partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = client.CreatePolicy(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create policy: %w", err)
	}
	if taggedSeparately && len(r.Tags) > 0 {
		err := syncPolicyTags(ctx, client, aws.ToString(resp.Policy.Arn), r.Tags)
		if err != nil {
			return nil, err
		}
	}
	// Read settles the eventual consistency that follows a create: IAM can
	// briefly report the just-created policy as absent, and a later plan that
	// read it absent would take it for deleted and recreate it. Waiting here
	// for it to become visible keeps the next read truthful.
	return r.read(ctx, client, aws.ToString(resp.Policy.Arn), true)
}

func (r *Policy) Read(
	ctx context.Context, cfg any, prior *PolicyOutput,
) (*PolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Arn, false)
}

// read fetches the policy by ARN and returns its computed outputs. When
// created is true the policy was just made, so a not-found means it has not
// propagated yet and read waits for it; otherwise a not-found is drift and
// maps to runtime.ErrNotFound at once. A policy ARN is well-formed as soon as
// the policy exists, so visibility is the only thing to wait for.
func (r *Policy) read(
	ctx context.Context, client *iam.Client, arn string, created bool,
) (*PolicyOutput, error) {
	var policy *iamtypes.Policy
	err := wait.Until(ctx, fmt.Sprintf("policy %s", r.PolicyName),
		func(ctx context.Context) (bool, error) {
			resp, err := client.GetPolicy(ctx, &iam.GetPolicyInput{
				PolicyArn: aws.String(arn),
			})
			if err != nil {
				if isNotFound(err) {
					if created {
						return false, nil
					}
					return false, runtime.ErrNotFound
				}
				return false, fmt.Errorf("get policy: %w", err)
			}
			if resp.Policy == nil {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			policy = resp.Policy
			return true, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return policyOutput(policy), nil
}

func (r *Policy) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Policy, *PolicyOutput],
) (*PolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	if runtime.Changed(prior.Inputs.PolicyDocument, r.PolicyDocument) {
		if err := r.applyNewVersion(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	if err := syncPolicyTags(ctx, client, arn, r.Tags); err != nil {
		return nil, err
	}
	resp, err := client.GetPolicy(ctx, &iam.GetPolicyInput{PolicyArn: aws.String(arn)})
	if err != nil {
		return nil, fmt.Errorf("get policy: %w", err)
	}
	return policyOutput(resp.Policy), nil
}

func (r *Policy) Delete(ctx context.Context, cfg any, prior *PolicyOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	arn := prior.Arn
	if err := pruneVersions(ctx, client, arn, 0); err != nil {
		return err
	}
	_, err = client.DeletePolicy(ctx, &iam.DeletePolicyInput{PolicyArn: aws.String(arn)})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete policy: %w", err)
	}
	return nil
}

// applyNewVersion records the current policy document as a new version and
// makes it the default. IAM caps a policy at five versions, so a full
// policy is pruned of its oldest non-default version first.
func (r *Policy) applyNewVersion(ctx context.Context, client *iam.Client, arn string) error {
	if err := pruneVersions(ctx, client, arn, maxPolicyVersions-1); err != nil {
		return err
	}
	_, err := client.CreatePolicyVersion(ctx, &iam.CreatePolicyVersionInput{
		PolicyArn:      aws.String(arn),
		PolicyDocument: aws.String(r.PolicyDocument),
		SetAsDefault:   true,
	})
	if err != nil {
		return fmt.Errorf("create policy version: %w", err)
	}
	return nil
}

// pruneVersions removes non-default policy versions until at most keep
// remain. The default version is never removed: IAM rejects deleting it,
// and a policy is deleted only after its other versions are gone. With keep
// set to zero this clears every non-default version ahead of deleting the
// policy. The oldest versions are removed first.
func pruneVersions(ctx context.Context, client *iam.Client, arn string, keep int) error {
	var nonDefault []iamtypes.PolicyVersion
	var marker *string
	for {
		resp, err := client.ListPolicyVersions(ctx, &iam.ListPolicyVersionsInput{
			PolicyArn: aws.String(arn),
			Marker:    marker,
		})
		if err != nil {
			return fmt.Errorf("list policy versions: %w", err)
		}
		for _, v := range resp.Versions {
			if !v.IsDefaultVersion {
				nonDefault = append(nonDefault, v)
			}
		}
		if !resp.IsTruncated {
			break
		}
		marker = resp.Marker
	}
	sort.Slice(nonDefault, func(i, j int) bool {
		return aws.ToTime(nonDefault[i].CreateDate).
			Before(aws.ToTime(nonDefault[j].CreateDate))
	})
	for len(nonDefault) > keep {
		v := nonDefault[0]
		nonDefault = nonDefault[1:]
		_, err := client.DeletePolicyVersion(ctx, &iam.DeletePolicyVersionInput{
			PolicyArn: aws.String(arn),
			VersionId: v.VersionId,
		})
		if err != nil {
			return fmt.Errorf("delete policy version: %w", err)
		}
	}
	return nil
}

// syncPolicyTags reconciles the tags on the policy with desired. The
// comparison and ordering live in tagsync.Sync; the closures supply IAM's
// own policy tag calls, addressed by ARN.
func syncPolicyTags(
	ctx context.Context, client *iam.Client, arn string, desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			current := make(map[string]string)
			var marker *string
			for {
				resp, err := client.ListPolicyTags(ctx, &iam.ListPolicyTagsInput{
					PolicyArn: aws.String(arn),
					Marker:    marker,
				})
				if err != nil {
					return nil, fmt.Errorf("list policy tags: %w", err)
				}
				for _, t := range resp.Tags {
					current[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
				if !resp.IsTruncated {
					break
				}
				marker = resp.Marker
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagPolicy(ctx, &iam.TagPolicyInput{
				PolicyArn: aws.String(arn),
				Tags:      toIamTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag policy: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagPolicy(ctx, &iam.UntagPolicyInput{
				PolicyArn: aws.String(arn),
				TagKeys:   remove,
			}); err != nil {
				return fmt.Errorf("untag policy: %w", err)
			}
			return nil
		},
	)
}

func policyOutput(p *iamtypes.Policy) *PolicyOutput {
	out := &PolicyOutput{
		Arn:              aws.ToString(p.Arn),
		PolicyId:         aws.ToString(p.PolicyId),
		DefaultVersionId: aws.ToString(p.DefaultVersionId),
		AttachmentCount:  int64(aws.ToInt32(p.AttachmentCount)),
	}
	if p.CreateDate != nil {
		out.CreateDate = p.CreateDate.UTC().Format(time.RFC3339)
	}
	return out
}

func toIamTags(tags map[string]string) []iamtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]iamtypes.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, iamtypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(tags[k]),
		})
	}
	return out
}
