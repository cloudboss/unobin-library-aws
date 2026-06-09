package ssm

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// dataTypeImage is the data type that triggers asynchronous AMI-id validation.
// When a parameter is created with this data type, PutParameter returns before
// SSM finishes validating the value, so the just-created parameter can read as
// absent for a short while; the value read retries through that window.
const dataTypeImage = "aws:ec2:image"

// typeSecureString is the parameter type whose value is encrypted. A KMS key id
// is only honored for it, and an insecure (plaintext) value is forbidden with
// it.
const typeSecureString = "SecureString"

// validationWait bounds how long the post-create value read retries a not-found
// while SSM validates an aws:ec2:image value. It mirrors the two-minute window
// the Terraform provider allows for that asynchronous validation.
const validationWait = 2 * time.Minute

// Parameter manages an SSM parameter: a single PutParameter upsert keyed by
// name. The name and the data type are fixed at create time -- a parameter
// cannot be renamed in place, and SSM rejects a data-type change on an existing
// parameter -- so a change to either replaces the parameter; every other input
// is reconciled in place by Update through another PutParameter.
//
// The value is given either as value (masked, the SecureString secret) or as
// insecure-value (plaintext, readable back). Both feed the single SDK Value
// argument; exactly one must be set, and insecure-value is forbidden when the
// type is SecureString. Tags do not ride a PutParameter that overwrites an
// existing parameter -- SSM rejects setting both Tags and Overwrite -- so on an
// update they are reconciled by separate AddTagsToResource and
// RemoveTagsFromResource calls.
type Parameter struct {
	// Name is the fully qualified parameter name and the resource identity. It
	// must be 1 to 2048 characters, a length checked in Create and Update.
	Name string `ub:"name"`
	// Type is the parameter type: String, StringList, or SecureString.
	Type string `ub:"type"`
	// Value is the parameter value when it should be masked; it is the
	// SecureString secret and is held sensitive. Exactly one of value or
	// insecure-value is set.
	Value *string `ub:"value,sensitive"`
	// InsecureValue is the parameter value when it should read back in plaintext.
	// It is not sensitive and is forbidden with a SecureString type. Exactly one
	// of value or insecure-value is set.
	InsecureValue *string `ub:"insecure-value"`
	// AllowedPattern is a regular expression SSM uses to validate the value. It
	// is always sent so clearing it back to empty clears it server-side, and must
	// be at most 1024 characters, a length checked in Create and Update.
	AllowedPattern *string `ub:"allowed-pattern"`
	// Description is optional metadata for the parameter, at most 1024
	// characters, a length checked in Create and Update.
	Description *string `ub:"description"`
	// KeyId is the KMS key that encrypts a SecureString value. It is sent only
	// when the type is SecureString, since SSM rejects it for other types.
	KeyId *string `ub:"key-id"`
	// DataType is the value's data type: text, aws:ec2:image, or
	// aws:ssm:integration. An aws:ec2:image value is validated asynchronously.
	DataType *string `ub:"data-type"`
	// Tier is the parameter tier: Standard, Advanced, or Intelligent-Tiering. An
	// unset tier lets SSM apply the account default.
	Tier *string `ub:"tier"`
	// Tags is the parameter's tag set. On create the tags ride the PutParameter
	// call; on update they are reconciled by separate tag calls, since SSM
	// forbids setting both Tags and Overwrite.
	Tags map[string]string `ub:"tags"`
}

// ParameterOutput holds the values SSM computes for a parameter. The ARN is the
// parameter's reference handle in policies, and version settles after each
// write. Name is the identity handle: a parameter has no server-assigned id, so
// a name change is a replace, and Delete keys off the prior name to remove the
// old parameter rather than the new one.
type ParameterOutput struct {
	Arn     string `ub:"arn"`
	Version int64  `ub:"version"`
	Name    string `ub:"name"`
}

func (r *Parameter) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs SSM fixes once a parameter exists. The name is
// the identity and cannot change; the data type cannot be changed on an
// existing parameter, so a change to either requires a new parameter. The tier
// is deliberately omitted: a tier change is in place except for an
// Advanced-to-Standard downgrade, which SSM rejects (an advanced parameter
// cannot revert to standard without data loss). That value-comparison-branched
// replace cannot be a clean ReplaceFields entry, so it stays API-enforced.
func (r *Parameter) ReplaceFields() []string {
	return []string{
		"name",
		"data-type",
	}
}

// Defaults marks the collection input a parameter may omit.
func (r Parameter) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules SSM places on a parameter's inputs. Exactly one
// of value or insecure-value supplies the parameter value, and a plaintext
// insecure-value cannot be combined with a SecureString type. The type is
// required and accepts a fixed set; the tier and data type are optional and each
// accept a fixed set. The length bounds on name, allowed-pattern, and
// description are checked in Create and Update rather than here, because the
// constraint vocabulary has no string-length condition (AtMost compares a
// numeric value, not a character count).
func (r Parameter) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.Value, r.InsecureValue),
		constraint.When(constraint.Equals(r.Type, "SecureString")).
			Require(constraint.Absent(r.InsecureValue)).
			Message("insecure-value cannot be set when type is SecureString"),
		constraint.Must(constraint.OneOf(r.Type, "String", "StringList", "SecureString")),
		constraint.When(constraint.Present(r.Tier)).
			Require(constraint.OneOf(r.Tier, "Standard", "Advanced", "Intelligent-Tiering")).
			Message("tier must be Standard, Advanced, or Intelligent-Tiering"),
		constraint.When(constraint.Present(r.DataType)).
			Require(constraint.OneOf(r.DataType, "text", "aws:ec2:image", "aws:ssm:integration")).
			Message("data-type must be text, aws:ec2:image, or aws:ssm:integration"),
	}
}

// validate checks the length bounds SSM places on the string inputs: name is 1
// to 2048 characters, and allowed-pattern and description are at most 1024.
// These are checked here because the constraint vocabulary has no string-length
// condition. The bounds are byte counts, which match SSM's character limits for
// the ASCII these fields hold.
func (r *Parameter) validate() error {
	if n := len(r.Name); n < 1 || n > 2048 {
		return fmt.Errorf("name must be between 1 and 2048 characters, got %d", n)
	}
	if r.AllowedPattern != nil && len(*r.AllowedPattern) > 1024 {
		return fmt.Errorf("allowed-pattern must be at most 1024 characters, got %d",
			len(*r.AllowedPattern))
	}
	if r.Description != nil && len(*r.Description) > 1024 {
		return fmt.Errorf("description must be at most 1024 characters, got %d",
			len(*r.Description))
	}
	return nil
}

func (r *Parameter) Create(ctx context.Context, cfg any) (*ParameterOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Overwrite is false on create so the tags can ride the PutParameter call;
	// SSM rejects a request that sets both Tags and Overwrite. The tags are
	// inlined here, so no separate AddTagsToResource is needed.
	in := r.putInput(false)
	in.Tags = parameterTags(r.Tags)
	if err := r.put(ctx, client, in); err != nil {
		return nil, fmt.Errorf("create parameter: %w", err)
	}
	// PutParameter does not return the parameter ARN, only its version and tier,
	// so the settled ARN and version come from the follow-up read. When the data
	// type is aws:ec2:image the value is validated asynchronously and the read
	// may briefly find the parameter absent, so the read waits through that
	// window on this first create.
	return r.read(ctx, client, true)
}

func (r *Parameter) Read(
	ctx context.Context, cfg any, prior *ParameterOutput,
) (*ParameterOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, false)
}

func (r *Parameter) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Parameter, *ParameterOutput],
) (*ParameterOutput, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Tags are reconciled by their own calls, so a change to only the tag set
	// must not re-issue PutParameter. The value, allowed pattern, and tier are
	// always part of the write because SSM treats an omitted allowed pattern or
	// value as a clear; gate the write on any non-tag input changing.
	if r.nonTagChanged(prior.Inputs) {
		// Overwrite is true on update, which forbids inline tags; they are
		// handled by syncTags below.
		in := r.putInput(true)
		if err := r.put(ctx, client, in); err != nil {
			return nil, fmt.Errorf("update parameter: %w", err)
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, false)
}

func (r *Parameter) Delete(ctx context.Context, cfg any, prior *ParameterOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// A parameter has no server-assigned id, so its name is the identity handle.
	// On a replace the receiver holds the new name while prior holds the old
	// outputs, so Delete keys off the prior name to remove the old parameter.
	name := prior.Name
	if name == "" {
		name = r.Name
	}
	_, err = client.DeleteParameter(ctx, &ssm.DeleteParameterInput{
		Name: aws.String(name),
	})
	if err != nil {
		// A parameter already gone, deleted out of band or by an earlier run,
		// counts as deleted.
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete parameter: %w", err)
	}
	return nil
}

// putInput builds the PutParameter request shared by Create and Update. The
// name, type, and value are always sent, and the allowed pattern is sent
// unconditionally (defaulting to the empty string) so clearing it back to empty
// clears it server-side. The data type, description, and tier are sent when
// present; a KMS key id is sent only for a SecureString, since SSM rejects it
// for other types. Tags are left for the caller to set, because whether they
// ride the call depends on the Overwrite value.
func (r *Parameter) putInput(overwrite bool) *ssm.PutParameterInput {
	in := &ssm.PutParameterInput{
		Name:           aws.String(r.Name),
		Type:           ssmtypes.ParameterType(r.Type),
		Value:          r.value(),
		AllowedPattern: aws.String(aws.ToString(r.AllowedPattern)),
		Overwrite:      aws.Bool(overwrite),
		DataType:       r.DataType,
		Description:    r.Description,
	}
	if r.Tier != nil {
		in.Tier = ssmtypes.ParameterTier(*r.Tier)
	}
	if r.Type == typeSecureString {
		in.KeyId = r.KeyId
	}
	return in
}

// put issues PutParameter, retrying once without the tier when SSM reports the
// requested tier is not supported. Clearing the tier lets SSM apply the account
// default rather than failing the apply over a tier the Region does not offer.
func (r *Parameter) put(ctx context.Context, client *ssm.Client, in *ssm.PutParameterInput) error {
	_, err := client.PutParameter(ctx, in)
	if err != nil && isTierUnsupported(err) {
		in.Tier = ""
		_, err = client.PutParameter(ctx, in)
	}
	return err
}

// value returns the single Value the SDK takes, folding value and
// insecure-value into one argument. A constraint guarantees exactly one is set,
// so whichever is present is the value; insecure-value exists only so it reads
// back in plaintext while value stays masked.
func (r *Parameter) value() *string {
	if r.Value != nil {
		return r.Value
	}
	return r.InsecureValue
}

// nonTagChanged reports whether any input other than tags changed, the gate for
// re-issuing PutParameter on an update. Tags are reconciled separately, so a
// change to only the tag set leaves the parameter value untouched.
func (r *Parameter) nonTagChanged(prior Parameter) bool {
	return runtime.Changed(prior.Name, r.Name) ||
		runtime.Changed(prior.Type, r.Type) ||
		runtime.Changed(prior.Value, r.Value) ||
		runtime.Changed(prior.InsecureValue, r.InsecureValue) ||
		runtime.Changed(prior.AllowedPattern, r.AllowedPattern) ||
		runtime.Changed(prior.Description, r.Description) ||
		runtime.Changed(prior.KeyId, r.KeyId) ||
		runtime.Changed(prior.DataType, r.DataType) ||
		runtime.Changed(prior.Tier, r.Tier)
}

// read fetches the parameter's computed outputs through GetParameter, which
// confirms the parameter exists and yields its ARN, version, and name. When
// created is true the parameter was just made; a not-found while the data type
// is aws:ec2:image means the asynchronous AMI validation has not finished, so
// the read waits through that window. Otherwise a not-found is drift and maps to
// runtime.ErrNotFound at once.
func (r *Parameter) read(
	ctx context.Context, client *ssm.Client, created bool,
) (*ParameterOutput, error) {
	param, err := r.getParameter(ctx, client, created)
	if err != nil {
		return nil, err
	}
	return &ParameterOutput{
		Arn:     aws.ToString(param.ARN),
		Version: param.Version,
		Name:    aws.ToString(param.Name),
	}, nil
}

// getParameter reads the parameter value with decryption. On a fresh create of
// an aws:ec2:image parameter it retries a not-found for up to two minutes while
// SSM validates the AMI id; otherwise a not-found maps to runtime.ErrNotFound.
func (r *Parameter) getParameter(
	ctx context.Context, client *ssm.Client, created bool,
) (*ssmtypes.Parameter, error) {
	awaitValidation := created && aws.ToString(r.DataType) == dataTypeImage
	var param *ssmtypes.Parameter
	err := wait.Until(ctx, fmt.Sprintf("parameter %s", r.Name),
		func(ctx context.Context) (bool, error) {
			resp, err := client.GetParameter(ctx, &ssm.GetParameterInput{
				Name:           aws.String(r.Name),
				WithDecryption: aws.Bool(true),
			})
			if err != nil {
				if isNotFound(err) {
					// While the AMI id is being validated the parameter is not
					// yet readable; keep waiting. In every other case a
					// not-found is drift.
					if awaitValidation {
						return false, nil
					}
					return false, runtime.ErrNotFound
				}
				return false, fmt.Errorf("get parameter: %w", err)
			}
			if resp.Parameter == nil {
				if awaitValidation {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			param = resp.Parameter
			return true, nil
		}, wait.WithTimeout(validationWait))
	if err != nil {
		return nil, err
	}
	return param, nil
}

// syncTags reconciles the parameter's tags with the desired set. SSM addresses
// parameter tags by name with the Parameter resource type, reading them with
// ListTagsForResource and writing changes with AddTagsToResource and
// RemoveTagsFromResource.
func (r *Parameter) syncTags(ctx context.Context, client *ssm.Client) error {
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx, &ssm.ListTagsForResourceInput{
				ResourceId:   aws.String(r.Name),
				ResourceType: ssmtypes.ResourceTypeForTaggingParameter,
			})
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			current := make(map[string]string, len(resp.TagList))
			for _, t := range resp.TagList {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.AddTagsToResource(ctx, &ssm.AddTagsToResourceInput{
				ResourceId:   aws.String(r.Name),
				ResourceType: ssmtypes.ResourceTypeForTaggingParameter,
				Tags:         parameterTags(upsert),
			}); err != nil {
				return fmt.Errorf("add tags to resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.RemoveTagsFromResource(ctx, &ssm.RemoveTagsFromResourceInput{
				ResourceId:   aws.String(r.Name),
				ResourceType: ssmtypes.ResourceTypeForTaggingParameter,
				TagKeys:      remove,
			}); err != nil {
				return fmt.Errorf("remove tags from resource: %w", err)
			}
			return nil
		},
	)
}

// parameterTags converts a desired tag map into the SSM SDK tag list, ordered
// by key so the request is deterministic.
func parameterTags(tags map[string]string) []ssmtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ssmtypes.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, ssmtypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}
