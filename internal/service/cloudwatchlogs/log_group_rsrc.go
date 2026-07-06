package cloudwatchlogs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatchlogs "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

// logGroupNameRe matches the characters CloudWatch Logs permits in a log group
// name: letters, digits, underscore, hyphen, forward slash, period, and the
// hash sign. The name is also bounded at 512 bytes. Both the pattern and the
// length bound are rules the constraint layer cannot express, so they are
// checked in code and documented on the name field.
var logGroupNameRe = regexp.MustCompile(`^[0-9A-Za-z_./#-]+$`)

// logGroupNameMaxLen is the longest a log group name may be. CloudWatch Logs
// measures the limit in characters; this check counts bytes, which agrees for
// the ASCII set the name pattern already restricts the value to.
const logGroupNameMaxLen = 512

// retentionRetryTimeout bounds the retry on PutRetentionPolicy. A retention
// policy set right after the log group is created can fail while the caller's
// own IAM permission to set it is still propagating; two minutes is ample for
// that grant to take effect.
const retentionRetryTimeout = 2 * time.Minute

// deleteRetryTimeout bounds the retry on DeleteLogGroup. A delete can briefly
// abort while a concurrent operation against the same group finishes; one
// minute is ample for that to clear.
const deleteRetryTimeout = time.Minute

// LogGroupResource is a CloudWatch Logs log group: the container that holds log streams
// and sets their retention and encryption. The name and the log group class are
// fixed when the group is created, so a change to either replaces the group.
// The retention period, the KMS key, deletion protection, and the tags are
// reconciled in place. Retention and the KMS key have no setting on the create
// call: retention is applied afterward by PutRetentionPolicy (or cleared by
// DeleteRetentionPolicy), and the KMS key, while it does ride the create call,
// is changed afterward by AssociateKmsKey or DisassociateKmsKey.
type LogGroupResource struct {
	// Name is the log group name. It must hold only letters, digits, underscore,
	// hyphen, forward slash, period, or the hash sign, and be no longer than 512
	// characters. The name is the group's identity, fixed at create time.
	Name                      string             `ub:"name"`
	LogGroupClass             *string            `ub:"log-group-class"`
	RetentionInDays           *int64             `ub:"retention-in-days"`
	KmsKeyId                  *string            `ub:"kms-key-id"`
	DeletionProtectionEnabled *bool              `ub:"deletion-protection-enabled"`
	Tags                      *map[string]string `ub:"tags"`
}

// LogGroupResourceOutput holds the value CloudWatch Logs computes for a log group. The
// ARN is the group's handle, against which its tags and deletion protection are
// managed. The describe call may return the ARN with a trailing ":*" wildcard;
// it is trimmed here, because the trimmed form is the ARN downstream resources
// reference.
type LogGroupResourceOutput struct {
	Arn string `ub:"arn"`
}

func (r *LogGroupResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs CloudWatch Logs fixes when a log group is
// created. The name is the group's identity, and the log group class cannot be
// changed on an existing group, so a change to either requires a new group.
// Every other input is reconciled in place by Update.
func (r *LogGroupResource) ReplaceFields() []string {
	return []string{
		"name",
		"log-group-class",
	}
}

// Constraints declares the rules CloudWatch Logs places on a log group's
// inputs. The retention period accepts only a fixed set of day counts, where 0
// means the log events never expire. The log group class accepts only its three
// known values.
func (r LogGroupResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.RetentionInDays)).
			Require(constraint.OneOf(r.RetentionInDays,
				0, 1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545,
				731, 1096, 1827, 2192, 2557, 2922, 3288, 3653)).
			Message("retention-in-days must be a valid CloudWatch Logs retention value"),
		constraint.When(constraint.Present(r.LogGroupClass)).
			Require(constraint.OneOf(r.LogGroupClass,
				"STANDARD", "INFREQUENT_ACCESS", "DELIVERY")).
			Message("log-group-class must be STANDARD, INFREQUENT_ACCESS, or DELIVERY"),
	}
}

func (r *LogGroupResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*LogGroupResourceOutput, error) {
	if err := r.validateName(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The name, class, KMS key, deletion protection, and tags ride the create
	// call. Retention has no setting here; it is applied below by its own call.
	in := &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName:              aws.String(r.Name),
		KmsKeyId:                  r.KmsKeyId,
		DeletionProtectionEnabled: r.DeletionProtectionEnabled,
		Tags:                      ptr.Value(r.Tags),
	}
	if r.LogGroupClass != nil {
		in.LogGroupClass = cloudwatchlogstypes.LogGroupClass(*r.LogGroupClass)
	}
	if _, err := client.CreateLogGroup(ctx, in); err != nil {
		return nil, fmt.Errorf("create log group: %w", err)
	}
	// A log group in the Delivery class cannot take a retention policy, so the
	// retention call is skipped for that class even when a value is given.
	if r.retentionSettable() && aws.ToInt64(r.RetentionInDays) > 0 {
		if err := r.putRetention(ctx, client); err != nil {
			return nil, err
		}
	}
	// Read after create to obtain the trimmed ARN, which the create response does
	// not return. CreateLogGroup is synchronous, so no settling wait is needed.
	return r.read(ctx, client)
}

func (r *LogGroupResource) Read(
	ctx context.Context, cfg *awsCfg, prior *LogGroupResourceOutput) (*LogGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read finds the log group by an exact name match and returns its computed
// output. CloudWatch Logs has no describe-by-name call, only a prefix filter,
// so it lists groups whose name begins with this name and keeps the one whose
// name matches exactly. Zero matches means the group is gone, which maps to
// runtime.ErrNotFound so a plan recreates it; a deleted group simply yields no
// matching rows rather than a typed not-found error.
func (r *LogGroupResource) read(
	ctx context.Context, client *cloudwatchlogs.Client,
) (*LogGroupResourceOutput, error) {
	var match *cloudwatchlogstypes.LogGroup
	pager := cloudwatchlogs.NewDescribeLogGroupsPaginator(client,
		&cloudwatchlogs.DescribeLogGroupsInput{
			LogGroupNamePrefix: aws.String(r.Name),
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe log groups: %w", err)
		}
		for i := range page.LogGroups {
			if aws.ToString(page.LogGroups[i].LogGroupName) == r.Name {
				match = &page.LogGroups[i]
				break
			}
		}
		if match != nil {
			break
		}
	}
	if match == nil {
		return nil, runtime.ErrNotFound
	}
	return &LogGroupResourceOutput{Arn: trimARNWildcardSuffix(aws.ToString(match.Arn))}, nil
}

func (r *LogGroupResource) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[LogGroupResource, *LogGroupResourceOutput],
) (*LogGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Retention reconciles when it changes: a value above zero is written, and a
	// cleared or zero value deletes the policy. The Delivery class takes no
	// retention policy, so the whole reconcile is skipped for it, the same as on
	// create; without the class guard a Delivery group would issue a needless
	// DeleteRetentionPolicy when its retention input changed.
	if r.retentionSettable() &&
		runtime.Changed(prior.Inputs.RetentionInDays, r.RetentionInDays) {
		if aws.ToInt64(r.RetentionInDays) > 0 {
			if err := r.putRetention(ctx, client); err != nil {
				return nil, err
			}
		} else {
			_, err := client.DeleteRetentionPolicy(ctx,
				&cloudwatchlogs.DeleteRetentionPolicyInput{
					LogGroupName: aws.String(r.Name),
				})
			if err != nil {
				return nil, fmt.Errorf("delete retention policy: %w", err)
			}
		}
	}
	// Deletion protection is set by its own call, which addresses the group by
	// ARN, so the prior output's ARN is reused.
	if runtime.Changed(prior.Inputs.DeletionProtectionEnabled, r.DeletionProtectionEnabled) {
		_, err := client.PutLogGroupDeletionProtection(ctx,
			&cloudwatchlogs.PutLogGroupDeletionProtectionInput{
				LogGroupIdentifier:        aws.String(prior.Outputs.Arn),
				DeletionProtectionEnabled: aws.Bool(aws.ToBool(r.DeletionProtectionEnabled)),
			})
		if err != nil {
			return nil, fmt.Errorf("put log group deletion protection: %w", err)
		}
	}
	// The KMS key is associated when set and disassociated when cleared.
	if runtime.Changed(prior.Inputs.KmsKeyId, r.KmsKeyId) {
		if r.KmsKeyId != nil {
			_, err := client.AssociateKmsKey(ctx, &cloudwatchlogs.AssociateKmsKeyInput{
				LogGroupName: aws.String(r.Name),
				KmsKeyId:     r.KmsKeyId,
			})
			if err != nil {
				return nil, fmt.Errorf("associate kms key: %w", err)
			}
		} else {
			_, err := client.DisassociateKmsKey(ctx,
				&cloudwatchlogs.DisassociateKmsKeyInput{
					LogGroupName: aws.String(r.Name),
				})
			if err != nil {
				return nil, fmt.Errorf("disassociate kms key: %w", err)
			}
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, prior.Outputs.Arn); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *LogGroupResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *LogGroupResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// A delete can briefly abort while a concurrent operation against the same
	// group finishes, signaled by an OperationAborted error asking to try again.
	// Retry through that window.
	err = retry.OnError(ctx, isAbortedTryAgain, func(ctx context.Context) error {
		_, err := client.DeleteLogGroup(ctx, &cloudwatchlogs.DeleteLogGroupInput{
			LogGroupName: aws.String(r.Name),
		})
		return err
	}, retry.WithTimeout(deleteRetryTimeout), retry.WithInterval(time.Second))
	if err != nil {
		// A group already gone counts as deleted.
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete log group: %w", err)
	}
	return nil
}

// putRetention writes the retention policy. A policy set right after the group
// is created can fail while the caller's logs:PutRetentionPolicy permission is
// still propagating; that grant takes effect within a couple of minutes, so the
// call retries through that window.
func (r *LogGroupResource) putRetention(ctx context.Context, client *cloudwatchlogs.Client) error {
	in := &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String(r.Name),
		RetentionInDays: ptr.Int32(r.RetentionInDays),
	}
	err := retry.OnError(ctx, isRetentionAccessDenied, func(ctx context.Context) error {
		_, err := client.PutRetentionPolicy(ctx, in)
		return err
	}, retry.WithTimeout(retentionRetryTimeout))
	if err != nil {
		return fmt.Errorf("put retention policy: %w", err)
	}
	return nil
}

// syncTags reconciles the log group's tags with the desired set, addressing the
// group by ARN. CloudWatch Logs reads tags with ListTagsForResource and writes
// changes with TagResource and UntagResource.
func (r *LogGroupResource) syncTags(
	ctx context.Context,
	client *cloudwatchlogs.Client,
	arn string,
) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx,
				&cloudwatchlogs.ListTagsForResourceInput{ResourceArn: aws.String(arn)})
			if err != nil {
				return nil, fmt.Errorf("list tags for resource: %w", err)
			}
			return resp.Tags, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &cloudwatchlogs.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        upsert,
			}); err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &cloudwatchlogs.UntagResourceInput{
				ResourceArn: aws.String(arn),
				TagKeys:     remove,
			}); err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}

// retentionSettable reports whether a retention policy may be set on this log
// group. The Delivery class keeps log events for a fixed single day and does
// not accept a retention policy, so retention is settable for every other
// class.
func (r *LogGroupResource) retentionSettable() bool {
	return r.LogGroupClass == nil ||
		*r.LogGroupClass != string(cloudwatchlogstypes.LogGroupClassDelivery)
}

// validateName checks the name against the rules CloudWatch Logs enforces but
// the constraint layer cannot express: the permitted character set and the
// 512-byte length bound.
func (r *LogGroupResource) validateName() error {
	if len(r.Name) > logGroupNameMaxLen {
		return fmt.Errorf("name must be at most %d characters", logGroupNameMaxLen)
	}
	if !logGroupNameRe.MatchString(r.Name) {
		return errors.New(
			"name must contain only letters, digits, underscore, hyphen, " +
				"forward slash, period, or the hash sign")
	}
	return nil
}

// trimARNWildcardSuffix removes a trailing ":*" wildcard from a log group ARN.
// DescribeLogGroups may return the ARN with that suffix, but the trimmed form
// is the ARN downstream resources reference, so it is the value exposed.
func trimARNWildcardSuffix(arn string) string {
	return strings.TrimSuffix(arn, ":*")
}

// isRetentionAccessDenied reports whether err is the access-denied error
// CloudWatch Logs returns when PutRetentionPolicy runs before the caller's
// logs:PutRetentionPolicy permission has propagated. The grant takes effect
// shortly, so a caller retries until it does.
func isRetentionAccessDenied(err error) bool {
	var denied *cloudwatchlogstypes.AccessDeniedException
	return errors.As(err, &denied) &&
		strings.Contains(denied.ErrorMessage(),
			"no identity-based policy allows the logs:PutRetentionPolicy action")
}

// isAbortedTryAgain reports whether err is the operation-aborted error
// CloudWatch Logs returns when a delete races a concurrent operation against
// the same group and asks the caller to try again. The conflict clears on its
// own, so a caller retries.
func isAbortedTryAgain(err error) bool {
	var aborted *cloudwatchlogstypes.OperationAbortedException
	return errors.As(err, &aborted) &&
		strings.Contains(aborted.ErrorMessage(), "try again")
}
