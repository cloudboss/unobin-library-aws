package ecr

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// deleteTimeout bounds the wait for a deleted repository to stop describing.
// ECR usually removes a repository in seconds, but a force delete of a
// populated repository removes every image first, so the bound is generous.
const deleteTimeout = 20 * time.Minute

// Repository manages an ECR private repository: the registry entry container
// images are pushed to, plus the settings ECR writes through their own calls,
// folded in as fields. The name and the encryption configuration are fixed at
// create, so a change to either replaces the repository; the tag mutability
// setting and its exclusion filters, the scan-on-push toggle, the two policy
// texts, and the tags all reconcile in place.
type Repository struct {
	// Name is the repository name, on its own or prefixed with a namespace,
	// such as project-a/nginx-web-app. ECR requires 2 to 256 characters of
	// lowercase letters, digits, and ._-/ separators, which the API enforces.
	Name string `ub:"name"`
	// EncryptionConfiguration fixes the at-rest encryption of the
	// repository's contents. Omitted, ECR encrypts with AES256.
	EncryptionConfiguration *RepositoryEncryptionConfiguration `ub:"encryption-configuration"`
	// ScanOnPush, when true, scans each image for known vulnerabilities as it
	// is pushed. It backs the image scanning configuration, reconciled by its
	// own PutImageScanningConfiguration call; removing the field sets it back
	// to false, the ECR default.
	ScanOnPush *bool `ub:"scan-on-push"`
	// ImageTagMutability sets whether image tags may be overwritten: MUTABLE,
	// IMMUTABLE, or the MUTABLE_WITH_EXCLUSION and IMMUTABLE_WITH_EXCLUSION
	// variants that exempt the tags the exclusion filters match. Omitted, ECR
	// defaults to MUTABLE, and a later update writes MUTABLE back.
	ImageTagMutability *string `ub:"image-tag-mutability"`
	// ImageTagMutabilityExclusionFilters lists up to five filters naming the
	// image tags exempt from the mutability setting. The list is only valid
	// with one of the WITH_EXCLUSION mutability modes, and it changes through
	// the same PutImageTagMutability call as the setting itself.
	ImageTagMutabilityExclusionFilters []RepositoryExclusionFilter `ub:"image-tag-mutability-exclusion-filters"`
	// LifecyclePolicy is the JSON lifecycle policy text, which expires images
	// by age or count. It is reconciled by its own PutLifecyclePolicy call,
	// an upsert, so a changed policy updates in place; removing the field
	// deletes the policy. The text is sent as given, so reformatting it reads
	// as a change.
	LifecyclePolicy *string `ub:"lifecycle-policy"`
	// RepositoryPolicy is the JSON repository policy text granting other
	// principals access to the repository. It is reconciled by its own
	// SetRepositoryPolicy call; removing the field deletes the policy. The
	// text is sent as given, so reformatting it reads as a change.
	RepositoryPolicy *string `ub:"repository-policy"`
	// Tags are the metadata tags on the repository, reconciled as a set.
	Tags map[string]string `ub:"tags"`
	// ForceDelete, when true, deletes the repository even when it still holds
	// images. It is a delete-time switch with no presence in the cloud, so it
	// is never sent to create or read.
	ForceDelete *bool `ub:"force-delete"`
}

// RepositoryOutput holds the values ECR computes for a repository. The ARN is
// the repository's identity in IAM policies and tag calls; the registry id is
// the account that owns it; the repository URI is the registry hostname plus
// the name, the value image push and pull operations address. Name repeats
// the input because it is the handle every describe and delete keys off: on a
// replace, Delete receives the new inputs and must remove the repository
// recorded in the prior outputs.
type RepositoryOutput struct {
	Arn           string `ub:"arn"`
	Name          string `ub:"name"`
	RegistryId    string `ub:"registry-id"`
	RepositoryUri string `ub:"repository-uri"`
}

func (r *Repository) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs ECR fixes when a repository is created. The
// name is the repository's identity, and the encryption configuration is
// baked into how its contents are stored, so a change to either requires a
// new repository. Every other input is reconciled in place by Update; in
// particular the two policy texts update in place, since their put calls are
// upserts.
func (r *Repository) ReplaceFields() []string {
	return []string{"name", "encryption-configuration"}
}

// Defaults marks the collection inputs a repository may omit.
func (r Repository) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.ImageTagMutabilityExclusionFilters),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules ECR places on a repository's inputs: the
// enums, the exclusion-filter count, and the pairing of a non-empty
// exclusion-filter list with an exclusion mutability mode. The filter content
// rules, at most two wildcards in 1 to 128 characters of a fixed charset,
// need string predicates the constraint vocabulary does not have, so the API
// enforces them.
func (r Repository) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.ImageTagMutability)).
			Require(constraint.OneOf(r.ImageTagMutability,
				"MUTABLE", "IMMUTABLE", "MUTABLE_WITH_EXCLUSION", "IMMUTABLE_WITH_EXCLUSION")).
			Message("image-tag-mutability must be a valid tag mutability setting"),
		constraint.When(constraint.NotEmpty(r.ImageTagMutabilityExclusionFilters)).
			Require(constraint.OneOf(r.ImageTagMutability,
				"MUTABLE_WITH_EXCLUSION", "IMMUTABLE_WITH_EXCLUSION")).
			Message("exclusion filters require a WITH_EXCLUSION image-tag-mutability"),
		constraint.Must(constraint.MaxItems(r.ImageTagMutabilityExclusionFilters, 5)).
			Message("image-tag-mutability-exclusion-filters holds at most 5 filters"),
		constraint.ForEach(r.ImageTagMutabilityExclusionFilters,
			func(f RepositoryExclusionFilter) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(f.FilterType, "WILDCARD")).
						Message("a filter type must be WILDCARD"),
				}
			}),
		constraint.When(constraint.Present(r.EncryptionConfiguration.EncryptionType)).
			Require(constraint.OneOf(r.EncryptionConfiguration.EncryptionType,
				"AES256", "KMS", "KMS_DSSE")).
			Message("encryption-type must be AES256, KMS, or KMS_DSSE"),
	}
}

func (r *Repository) Create(ctx context.Context, cfg any) (*RepositoryOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &ecr.CreateRepositoryInput{
		RepositoryName:                     aws.String(r.Name),
		EncryptionConfiguration:            encryptionConfiguration(r.EncryptionConfiguration),
		ImageTagMutabilityExclusionFilters: exclusionFilters(r.ImageTagMutabilityExclusionFilters),
		Tags:                               repositoryTags(r.Tags),
	}
	if r.ScanOnPush != nil {
		in.ImageScanningConfiguration = &ecrtypes.ImageScanningConfiguration{
			ScanOnPush: *r.ScanOnPush,
		}
	}
	if r.ImageTagMutability != nil {
		in.ImageTagMutability = ecrtypes.ImageTagMutability(*r.ImageTagMutability)
	}
	_, err = client.CreateRepository(ctx, in)
	// Some partitions, such as the ISO partitions, cannot tag a repository as
	// it is created. When the tagged create fails for that reason, create the
	// repository without tags and apply them with a separate call below.
	taggedSeparately := false
	if err != nil && in.Tags != nil && partition.UnsupportedOperation(region(client), err) {
		in.Tags = nil
		taggedSeparately = true
		_, err = client.CreateRepository(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create repository: %w", err)
	}
	// A just-created repository can briefly describe as absent. Read with the
	// created flag waits out that window before anything else addresses the
	// repository: the policy puts below have no repository-not-found retry of
	// their own; it also settles the ARN the separate tagging below needs.
	out, err := r.read(ctx, client, r.Name, true)
	if err != nil {
		return nil, err
	}
	if taggedSeparately && len(r.Tags) > 0 {
		if _, err := client.TagResource(ctx, &ecr.TagResourceInput{
			ResourceArn: aws.String(out.Arn),
			Tags:        repositoryTags(r.Tags),
		}); err != nil {
			return nil, fmt.Errorf("tag resource: %w", err)
		}
	}
	if r.LifecyclePolicy != nil {
		if err := r.putLifecyclePolicy(ctx, client, r.Name); err != nil {
			return nil, err
		}
	}
	if r.RepositoryPolicy != nil {
		if err := r.setRepositoryPolicy(ctx, client, r.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *Repository) Read(
	ctx context.Context, cfg any, prior *RepositoryOutput,
) (*RepositoryOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Name, false)
}

// read fetches the repository by name and returns its computed outputs. When
// created is true the repository was just made, so an absent result is the
// create still propagating and read waits for it; otherwise an absent result
// is drift and maps to runtime.ErrNotFound at once. Absent means the typed
// not-found, an empty result, or a single result whose name does not match
// the request, the last a defensive check against an eventually-consistent
// describe. More than one result for one name is an error.
func (r *Repository) read(
	ctx context.Context, client *ecr.Client, name string, created bool,
) (*RepositoryOutput, error) {
	var repo ecrtypes.Repository
	err := wait.Until(ctx, fmt.Sprintf("repository %s", name),
		func(ctx context.Context) (bool, error) {
			resp, err := client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
				RepositoryNames: []string{name},
			})
			if err != nil {
				if isNotFound(err) {
					if created {
						return false, nil
					}
					return false, runtime.ErrNotFound
				}
				return false, fmt.Errorf("describe repositories: %w", err)
			}
			if len(resp.Repositories) > 1 {
				return false, fmt.Errorf("describe repositories: %d results for name %s",
					len(resp.Repositories), name)
			}
			if len(resp.Repositories) == 0 ||
				aws.ToString(resp.Repositories[0].RepositoryName) != name {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			repo = resp.Repositories[0]
			return true, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return &RepositoryOutput{
		Arn:           aws.ToString(repo.RepositoryArn),
		Name:          aws.ToString(repo.RepositoryName),
		RegistryId:    aws.ToString(repo.RegistryId),
		RepositoryUri: aws.ToString(repo.RepositoryUri),
	}, nil
}

func (r *Repository) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Repository, *RepositoryOutput],
) (*RepositoryOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	name := prior.Outputs.Name
	if r.mutabilityChanged(prior.Inputs) {
		if err := r.putImageTagMutability(ctx, client, name); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.ScanOnPush, r.ScanOnPush) {
		if err := r.putImageScanningConfiguration(ctx, client, name); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.LifecyclePolicy, r.LifecyclePolicy) {
		if r.LifecyclePolicy != nil {
			err = r.putLifecyclePolicy(ctx, client, name)
		} else {
			err = r.deleteLifecyclePolicy(ctx, client, name)
		}
		if err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.RepositoryPolicy, r.RepositoryPolicy) {
		if r.RepositoryPolicy != nil {
			err = r.setRepositoryPolicy(ctx, client, name)
		} else {
			err = r.deleteRepositoryPolicy(ctx, client, name)
		}
		if err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client, prior.Outputs.Arn); err != nil {
			return nil, err
		}
	}
	// Every output is fixed at create (name itself forces a replace), so there
	// is nothing fresh for a trailing describe to pick up.
	return prior.Outputs, nil
}

func (r *Repository) Delete(ctx context.Context, cfg any, prior *RepositoryOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// On a replace the receiver holds the new inputs, so the repository to
	// remove is the one recorded in the prior outputs.
	name := prior.Name
	_, err = client.DeleteRepository(ctx, &ecr.DeleteRepositoryInput{
		RepositoryName: aws.String(name),
		Force:          aws.ToBool(r.ForceDelete),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		if isNotEmpty(err) {
			return fmt.Errorf("delete repository %s: it still holds images; "+
				"set force-delete to delete it anyway: %w", name, err)
		}
		return fmt.Errorf("delete repository: %w", err)
	}
	// The delete can return while the repository still describes, and a plan
	// that read it then would take it for alive. Wait until it is gone, with
	// a generous bound, since a force delete of a populated repository
	// removes every image first.
	return wait.Until(ctx, fmt.Sprintf("repository %s to be deleted", name),
		func(ctx context.Context) (bool, error) {
			resp, err := client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
				RepositoryNames: []string{name},
			})
			if err != nil {
				if isNotFound(err) {
					return true, nil
				}
				return false, fmt.Errorf("describe repositories: %w", err)
			}
			for _, repo := range resp.Repositories {
				if aws.ToString(repo.RepositoryName) == name {
					return false, nil
				}
			}
			return true, nil
		},
		wait.WithTimeout(deleteTimeout),
	)
}

// mutabilityChanged reports whether the tag mutability setting or its
// exclusion-filter list changed since the last apply. The two ride one
// PutImageTagMutability call that always sends both, so either change
// triggers the same put.
func (r *Repository) mutabilityChanged(prior Repository) bool {
	return runtime.Changed(prior.ImageTagMutability, r.ImageTagMutability) ||
		runtime.Changed(prior.ImageTagMutabilityExclusionFilters,
			r.ImageTagMutabilityExclusionFilters)
}

// putImageTagMutability applies the mutability setting and the exclusion
// filters in one call, always sending both. ECR requires a mutability value
// on every put, so an unset input sends MUTABLE, the service default; an
// empty filter list is sent as no filters, which clears any set previously.
func (r *Repository) putImageTagMutability(
	ctx context.Context, client *ecr.Client, name string,
) error {
	mutability := ecrtypes.ImageTagMutabilityMutable
	if r.ImageTagMutability != nil {
		mutability = ecrtypes.ImageTagMutability(*r.ImageTagMutability)
	}
	_, err := client.PutImageTagMutability(ctx, &ecr.PutImageTagMutabilityInput{
		RepositoryName:                     aws.String(name),
		ImageTagMutability:                 mutability,
		ImageTagMutabilityExclusionFilters: exclusionFilters(r.ImageTagMutabilityExclusionFilters),
	})
	if err != nil {
		return fmt.Errorf("put image tag mutability: %w", err)
	}
	return nil
}

// putImageScanningConfiguration applies the scan-on-push setting. A removed
// input sends false, the service default, which is how the setting is turned
// back off.
func (r *Repository) putImageScanningConfiguration(
	ctx context.Context, client *ecr.Client, name string,
) error {
	_, err := client.PutImageScanningConfiguration(ctx, &ecr.PutImageScanningConfigurationInput{
		RepositoryName: aws.String(name),
		ImageScanningConfiguration: &ecrtypes.ImageScanningConfiguration{
			ScanOnPush: aws.ToBool(r.ScanOnPush),
		},
	})
	if err != nil {
		return fmt.Errorf("put image scanning configuration: %w", err)
	}
	return nil
}

// putLifecyclePolicy writes the lifecycle policy text. The call is an upsert,
// creating the policy or overwriting the previous one, so create and update
// take the same path.
func (r *Repository) putLifecyclePolicy(
	ctx context.Context, client *ecr.Client, name string,
) error {
	_, err := client.PutLifecyclePolicy(ctx, &ecr.PutLifecyclePolicyInput{
		RepositoryName:      aws.String(name),
		LifecyclePolicyText: r.LifecyclePolicy,
	})
	if err != nil {
		return fmt.Errorf("put lifecycle policy: %w", err)
	}
	return nil
}

// deleteLifecyclePolicy removes the lifecycle policy after the field is
// removed from the source. A repository with no policy raises the lifecycle
// flavor of not-found, which means the field is already absent, and a
// repository already gone has nothing to keep a policy on; both count as
// success.
func (r *Repository) deleteLifecyclePolicy(
	ctx context.Context, client *ecr.Client, name string,
) error {
	_, err := client.DeleteLifecyclePolicy(ctx, &ecr.DeleteLifecyclePolicyInput{
		RepositoryName: aws.String(name),
	})
	if err != nil && !isNotFound(err) && !isLifecyclePolicyNotFound(err) {
		return fmt.Errorf("delete lifecycle policy: %w", err)
	}
	return nil
}

// setRepositoryPolicy writes the repository policy text. A policy can name an
// IAM principal created moments earlier that ECR cannot resolve yet, which it
// rejects as an invalid parameter; that window clears on its own, so the put
// retries through it.
func (r *Repository) setRepositoryPolicy(
	ctx context.Context, client *ecr.Client, name string,
) error {
	in := &ecr.SetRepositoryPolicyInput{
		RepositoryName: aws.String(name),
		PolicyText:     r.RepositoryPolicy,
	}
	err := retry.OnError(ctx, isPrincipalNotFound, func(ctx context.Context) error {
		_, err := client.SetRepositoryPolicy(ctx, in)
		return err
	})
	if err != nil {
		return fmt.Errorf("set repository policy: %w", err)
	}
	return nil
}

// deleteRepositoryPolicy removes the repository policy after the field is
// removed from the source, tolerating the same pair of not-founds as its
// lifecycle counterpart: no policy set, or no repository at all.
func (r *Repository) deleteRepositoryPolicy(
	ctx context.Context, client *ecr.Client, name string,
) error {
	_, err := client.DeleteRepositoryPolicy(ctx, &ecr.DeleteRepositoryPolicyInput{
		RepositoryName: aws.String(name),
	})
	if err != nil && !isNotFound(err) && !isRepositoryPolicyNotFound(err) {
		return fmt.Errorf("delete repository policy: %w", err)
	}
	return nil
}

// syncTags reconciles the repository's tags with the desired set, reading the
// live tags with ListTagsForResource and writing changes with TagResource and
// UntagResource. ECR addresses repository tags by ARN, and the describe a
// Read makes does not return them, so the tag list is its own call.
func (r *Repository) syncTags(ctx context.Context, client *ecr.Client, arn string) error {
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx, &ecr.ListTagsForResourceInput{
				ResourceArn: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			current := map[string]string{}
			for _, t := range resp.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &ecr.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        repositoryTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &ecr.UntagResourceInput{
				ResourceArn: aws.String(arn),
				TagKeys:     remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}
