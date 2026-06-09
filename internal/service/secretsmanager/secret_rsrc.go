package secretsmanager

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	secretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// secretNameRegexp matches a valid secret name: one or more of the ASCII
// letters, digits, and the punctuation Secrets Manager allows. The byte length
// the validation enforces equals the character length because the class is
// ASCII.
var secretNameRegexp = regexp.MustCompile(`^[0-9A-Za-z/_+=.@-]+$`)

// secretNameMaxLength is the longest a secret name may be.
const secretNameMaxLength = 512

// currentStage is the staging label of the live secret version. PutSecretValue
// advances it to the new version automatically, and a value read targets it, so
// it is fixed here rather than exposed.
const currentStage = "AWSCURRENT"

// Secret manages a Secrets Manager secret: the encrypted value plus the
// metadata that governs it. The name is fixed at creation, so a change to it
// replaces the secret; the description, the KMS key, the value, the replica
// regions, and the tags reconcile in place. The value is a field reconciled by
// PutSecretValue rather than a separate version resource, since CloudFormation
// models the secret string as a property of the secret. An unset optional input
// rides as absent and Secrets Manager applies its own default, including the
// AWS-managed aws/secretsmanager key when no KMS key is given.
//
// Name is required, since Secrets Manager does not generate a name and
// CreateSecret needs one. It must match ^[0-9A-Za-z/_+=.@-]+$ and be at most 512
// characters; that rule is a regular-expression and byte-length check in Create
// rather than a declarative constraint.
type Secret struct {
	Name                        string            `ub:"name"`
	Description                 *string           `ub:"description"`
	KmsKeyId                    *string           `ub:"kms-key-id"`
	ForceOverwriteReplicaSecret *bool             `ub:"force-overwrite-replica-secret"`
	Replica                     []SecretReplica   `ub:"replica"`
	SecretString                *string           `ub:"secret-string,sensitive"`
	SecretBinary                *string           `ub:"secret-binary,sensitive"`
	RecoveryWindowInDays        *int64            `ub:"recovery-window-in-days"`
	Tags                        map[string]string `ub:"tags"`
}

// SecretReplica is one Region the secret is replicated to. The region names the
// destination. The KMS key id encrypts the replica there; when it is omitted,
// Secrets Manager uses the AWS-managed aws/secretsmanager key in that Region.
type SecretReplica struct {
	Region   string  `ub:"region"`
	KmsKeyId *string `ub:"kms-key-id"`
}

// SecretOutput holds the values Secrets Manager computes for a secret. The ARN
// is the secret's identity in policies and the stable handle every later call
// addresses it by, so it is the identity Delete keys off the prior outputs on a
// replace. VersionId is the identifier of the live value version, advanced by
// each PutSecretValue. ReplicaStatus is the per-region replication state read
// back from the secret, since add and remove are fire-and-forget and the status
// settles on its own.
type SecretOutput struct {
	Arn           string                `ub:"arn"`
	VersionId     string                `ub:"version-id"`
	ReplicaStatus []SecretReplicaStatus `ub:"replica-status"`
}

// SecretReplicaStatus is the computed replication state for one Region. Status
// is InProgress, Failed, or InSync; StatusMessage explains a failure when there
// is one. LastAccessedDate is when the replica was last retrieved in that
// Region, in RFC3339, and is empty when it has never been retrieved.
type SecretReplicaStatus struct {
	Region           string `ub:"region"`
	Status           string `ub:"status"`
	StatusMessage    string `ub:"status-message"`
	LastAccessedDate string `ub:"last-accessed-date"`
}

func (r *Secret) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs Secrets Manager fixes when a secret is created.
// The name is the secret's identity and there is no rename, so a change to it
// requires a new secret. Every other input reconciles in place by Update. The
// value is a field reconciled by PutSecretValue, not a replace trigger: a
// changed value advances to a new version in place.
func (r *Secret) ReplaceFields() []string {
	return []string{"name"}
}

// Defaults marks the collection inputs a secret may omit.
func (r Secret) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Replica),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules Secrets Manager places on a secret's inputs. A
// secret holds at most one value form, so the secret string and secret binary
// are mutually exclusive, and both may be absent since a secret can exist with
// no value. A recovery window is either zero, meaning force-delete with no
// window, or between 7 and 30 days. Every replica must name a Region. The name
// charset and length are a regular-expression and byte-length check in Create
// rather than declared here, since the constraint layer has no pattern match and
// counts length in bytes.
func (r Secret) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.SecretString, r.SecretBinary),
		constraint.When(constraint.Present(r.RecoveryWindowInDays)).
			Require(constraint.Any(constraint.Equals(r.RecoveryWindowInDays, 0),
				constraint.All(constraint.AtLeast(r.RecoveryWindowInDays, 7),
					constraint.AtMost(r.RecoveryWindowInDays, 30)))).
			Message("recovery-window-in-days must be 0 or between 7 and 30"),
		constraint.ForEach(r.Replica, func(rep SecretReplica) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.NotEmpty(rep.Region)).
					Message("a replica requires a region"),
			}
		}),
	}
}

// validate checks the rules Secrets Manager enforces that the constraint layer
// cannot express: the name must match the allowed charset and be no longer than
// the byte limit, and a secret binary value must be valid base64, since it is
// decoded before the call.
func (r *Secret) validate() error {
	if !secretNameRegexp.MatchString(r.Name) {
		return fmt.Errorf("name %q must match %s", r.Name, secretNameRegexp.String())
	}
	if len(r.Name) > secretNameMaxLength {
		return fmt.Errorf("name must be at most %d characters", secretNameMaxLength)
	}
	if r.SecretBinary != nil {
		if _, err := base64.StdEncoding.DecodeString(*r.SecretBinary); err != nil {
			return fmt.Errorf("secret-binary must be valid base64: %w", err)
		}
	}
	return nil
}

func (r *Secret) Create(ctx context.Context, cfg any) (*SecretOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &secretsmanager.CreateSecretInput{
		Name:                        aws.String(r.Name),
		Description:                 r.Description,
		ForceOverwriteReplicaSecret: aws.ToBool(r.ForceOverwriteReplicaSecret),
		AddReplicaRegions:           expandReplicas(r.Replica),
		Tags:                        secretTags(r.Tags),
	}
	// The KMS key is sent only when set; with none given Secrets Manager uses the
	// AWS-managed aws/secretsmanager key, so a nil key must not be fabricated.
	if r.KmsKeyId != nil {
		in.KmsKeyId = r.KmsKeyId
	}
	// Creating a secret whose name belongs to a secret that is mid-deletion fails
	// until that deletion finishes and the name frees up. Retry through that
	// window. The SDK fills a per-attempt ClientRequestToken via its idempotency
	// middleware, so none is set here.
	var resp *secretsmanager.CreateSecretOutput
	err = retry.OnError(ctx, isScheduledForDeletion, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateSecret(ctx, in)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create secret: %w", err)
	}
	arn := aws.ToString(resp.ARN)
	// CreateSecret returns before the secret is consistently describable, so wait
	// until a describe stops reporting it as not-found before going on.
	if err := r.waitDescribable(ctx, client, arn); err != nil {
		return nil, err
	}
	// The value is folded in by PutSecretValue when one is configured, and the
	// new version is waited until it is gettable so a later read finds it.
	if r.SecretString != nil || r.SecretBinary != nil {
		if err := r.putValue(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	// The replication status and the value version are not on the create
	// response, so the outputs come from a read once the secret has settled.
	return r.read(ctx, client, arn)
}

func (r *Secret) Read(ctx context.Context, cfg any, prior *SecretOutput) (*SecretOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Arn)
}

// read fetches the secret by ARN and computes its outputs. A gone secret, or
// one whose described DeletedDate is set because it is scheduled for deletion,
// maps to runtime.ErrNotFound so a plan recreates it. The live value version is
// read separately through GetSecretValue against the current stage; a value
// that is gone is treated as no current version rather than an error.
func (r *Secret) read(
	ctx context.Context, client *secretsmanager.Client, arn string,
) (*SecretOutput, error) {
	desc, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(arn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe secret: %w", err)
	}
	// A secret scheduled for deletion still describes, but it is logically gone:
	// its value is inaccessible and a plan should recreate it.
	if desc.DeletedDate != nil {
		return nil, runtime.ErrNotFound
	}
	versionID, err := r.readVersionID(ctx, client, arn)
	if err != nil {
		return nil, err
	}
	return &SecretOutput{
		Arn:           aws.ToString(desc.ARN),
		VersionId:     versionID,
		ReplicaStatus: flattenReplicaStatus(desc.ReplicationStatus),
	}, nil
}

// readVersionID returns the identifier of the secret's current value version,
// or an empty string when the secret holds no value. GetSecretValue reports a
// secret that is gone or scheduled for deletion through a not-found or an
// invalid-request error naming the deletion; both mean there is no current
// version to report, so they yield an empty id rather than an error.
func (r *Secret) readVersionID(
	ctx context.Context, client *secretsmanager.Client, arn string,
) (string, error) {
	resp, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String(arn),
		VersionStage: aws.String(currentStage),
	})
	if err != nil {
		if isValueGone(err) {
			return "", nil
		}
		return "", fmt.Errorf("get secret value: %w", err)
	}
	return aws.ToString(resp.VersionId), nil
}

func (r *Secret) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Secret, *SecretOutput],
) (*SecretOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	// Replica regions are reconciled by set difference on the configured Regions:
	// the removed Regions are taken out first, because a Region cannot be both
	// removed and re-added in one pass, then the added Regions are replicated.
	if runtime.Changed(prior.Inputs.Replica, r.Replica) {
		if err := r.reconcileReplicas(ctx, client, arn, prior.Inputs.Replica); err != nil {
			return nil, err
		}
	}
	// The description and KMS key are reconciled together by UpdateSecret when
	// either changed. The description is sent unconditionally, as the empty string
	// when cleared, so removing it resets the secret's description rather than
	// leaving the prior value in place. The KMS key is sent only when set, so
	// clearing it does not reset it; the AWS-managed key applies when none is given.
	if runtime.Changed(prior.Inputs.Description, r.Description) ||
		runtime.Changed(prior.Inputs.KmsKeyId, r.KmsKeyId) {
		upd := &secretsmanager.UpdateSecretInput{
			SecretId:    aws.String(arn),
			Description: aws.String(aws.ToString(r.Description)),
		}
		if r.KmsKeyId != nil {
			upd.KmsKeyId = r.KmsKeyId
		}
		if _, err := client.UpdateSecret(ctx, upd); err != nil {
			return nil, fmt.Errorf("update secret: %w", err)
		}
	}
	// A changed value advances to a new version in place through PutSecretValue.
	if runtime.Changed(prior.Inputs.SecretString, r.SecretString) ||
		runtime.Changed(prior.Inputs.SecretBinary, r.SecretBinary) {
		if r.SecretString != nil || r.SecretBinary != nil {
			if err := r.putValue(ctx, client, arn); err != nil {
				return nil, err
			}
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, arn)
}

func (r *Secret) Delete(ctx context.Context, cfg any, prior *SecretOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	arn := prior.Arn
	// Replicas are removed before the secret is deleted, since Secrets Manager
	// refuses to delete a secret that still has replicas. The prior outputs name
	// the Regions that exist; a replica already gone is tolerated.
	if len(prior.ReplicaStatus) > 0 {
		if err := r.removeReplicaRegions(ctx, client, arn,
			replicaStatusRegions(prior.ReplicaStatus)); err != nil {
			return err
		}
	}
	in := &secretsmanager.DeleteSecretInput{SecretId: aws.String(arn)}
	// A zero recovery window means force-delete with no window; the two delete
	// parameters are mutually exclusive, so only one is ever set. With no window
	// given, Secrets Manager applies its own default recovery window.
	if r.RecoveryWindowInDays != nil && *r.RecoveryWindowInDays == 0 {
		in.ForceDeleteWithoutRecovery = aws.Bool(true)
	} else if r.RecoveryWindowInDays != nil {
		in.RecoveryWindowInDays = r.RecoveryWindowInDays
	}
	_, err = client.DeleteSecret(ctx, in)
	if err != nil {
		// A secret already gone counts as deleted.
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete secret: %w", err)
	}
	// DeleteSecret returns before the secret stops describing, so wait until a
	// describe reports it gone or scheduled for deletion before reporting done.
	return r.waitDeleted(ctx, client, arn)
}

// putValue writes a new value version with PutSecretValue. Exactly one of the
// string or binary value is sent; a binary value is the base64 input decoded to
// raw bytes. The new version is waited until it is gettable, since the value is
// not consistently readable the instant the put returns. The SDK fills a
// per-attempt ClientRequestToken via its idempotency middleware, so none is set
// here.
func (r *Secret) putValue(
	ctx context.Context, client *secretsmanager.Client, arn string,
) error {
	in := &secretsmanager.PutSecretValueInput{SecretId: aws.String(arn)}
	if r.SecretString != nil {
		in.SecretString = r.SecretString
	} else if r.SecretBinary != nil {
		decoded, err := base64.StdEncoding.DecodeString(*r.SecretBinary)
		if err != nil {
			return fmt.Errorf("decode secret-binary: %w", err)
		}
		in.SecretBinary = decoded
	}
	resp, err := client.PutSecretValue(ctx, in)
	if err != nil {
		return fmt.Errorf("put secret value: %w", err)
	}
	return r.waitValueGettable(ctx, client, arn, aws.ToString(resp.VersionId))
}

// waitDescribable polls DescribeSecret until the just-created secret stops
// reporting as not-found, the eventual-consistency window after a create.
func (r *Secret) waitDescribable(
	ctx context.Context, client *secretsmanager.Client, arn string,
) error {
	what := fmt.Sprintf("secret %s", arn)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
			SecretId: aws.String(arn),
		})
		if err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("describe secret: %w", err)
		}
		return true, nil
	})
}

// waitValueGettable polls GetSecretValue until the new version is readable, the
// eventual-consistency window after a put. The wait targets the version the put
// returned so it confirms that exact version, not an older one.
func (r *Secret) waitValueGettable(
	ctx context.Context, client *secretsmanager.Client, arn, versionID string,
) error {
	what := fmt.Sprintf("secret %s value %s", arn, versionID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
			SecretId:  aws.String(arn),
			VersionId: aws.String(versionID),
		})
		if err != nil {
			if isValueGone(err) {
				return false, nil
			}
			return false, fmt.Errorf("get secret value: %w", err)
		}
		return true, nil
	})
}

// waitDeleted polls DescribeSecret until the secret reports gone, since a
// deleted secret keeps describing for a while after DeleteSecret returns. A
// described DeletedDate satisfies the wait too, so a window-scheduled deletion
// counts as gone the same as a force-delete that has propagated.
func (r *Secret) waitDeleted(
	ctx context.Context, client *secretsmanager.Client, arn string,
) error {
	what := fmt.Sprintf("secret %s deletion", arn)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		desc, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
			SecretId: aws.String(arn),
		})
		if err != nil {
			if isNotFound(err) {
				return true, nil
			}
			return false, fmt.Errorf("describe secret: %w", err)
		}
		return desc.DeletedDate != nil, nil
	})
}

// syncTags reconciles the secret's tags with the desired set, reading the live
// tags from DescribeSecret and writing changes with TagResource and
// UntagResource. Secrets Manager addresses a secret's tags by its ARN.
func (r *Secret) syncTags(
	ctx context.Context, client *secretsmanager.Client, arn string,
) error {
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			desc, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
				SecretId: aws.String(arn),
			})
			if err != nil {
				return nil, fmt.Errorf("describe secret: %w", err)
			}
			current := map[string]string{}
			for _, t := range desc.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &secretsmanager.TagResourceInput{
				SecretId: aws.String(arn),
				Tags:     secretTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &secretsmanager.UntagResourceInput{
				SecretId: aws.String(arn),
				TagKeys:  remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}

// secretTags converts a desired tag map into the Secrets Manager SDK tag list.
func secretTags(tags map[string]string) []secretsmanagertypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]secretsmanagertypes.Tag, 0, len(tags))
	for k, v := range tags {
		out = append(out, secretsmanagertypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}
