package dynamodb

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	dynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

// billingModeProvisioned is the billing mode DynamoDB applies when none is
// given and the one under which read and write capacity are required. It is
// matched here to decide whether the create and the per-index updates send
// provisioned capacity.
const billingModeProvisioned = "PROVISIONED"

// TableResource manages a DynamoDB table and the settings DynamoDB reconciles through
// follow-on calls, the way CloudFormation models AWS::DynamoDB::Table. The
// name, the table key (hash and range), and the local secondary indexes are
// fixed at creation, so a change to any of them replaces the table; everything
// else reconciles in place. The primary key and the index keys are decomposed
// into hash-key, range-key, and the per-index blocks rather than a raw key
// schema, and read-capacity and write-capacity into a provisioned-throughput
// pair, to match how a table is described. Time-to-live and point-in-time
// recovery are separate DynamoDB operations with no create-time field, so they
// are blocks reconciled after the table exists. An unset billing-mode is
// treated by DynamoDB as PROVISIONED, which then requires capacity; it is sent
// only when set so an omitted value does not read as drift.
type TableResource struct {
	Name                      string                       `ub:"name"`
	BillingMode               *string                      `ub:"billing-mode"`
	HashKey                   string                       `ub:"hash-key"`
	RangeKey                  *string                      `ub:"range-key"`
	Attribute                 *[]TableAttribute            `ub:"attribute"`
	ReadCapacity              *int64                       `ub:"read-capacity"`
	WriteCapacity             *int64                       `ub:"write-capacity"`
	LocalSecondaryIndex       *[]TableLocalSecondaryIndex  `ub:"local-secondary-index"`
	GlobalSecondaryIndex      *[]TableGlobalSecondaryIndex `ub:"global-secondary-index"`
	OnDemandThroughput        *TableOnDemandThroughput     `ub:"on-demand-throughput"`
	StreamEnabled             *bool                        `ub:"stream-enabled"`
	StreamViewType            *string                      `ub:"stream-view-type"`
	ServerSideEncryption      *TableServerSideEncryption   `ub:"server-side-encryption"`
	TableClass                *string                      `ub:"table-class"`
	WarmThroughput            *TableWarmThroughput         `ub:"warm-throughput"`
	Ttl                       *TableTtl                    `ub:"ttl"`
	PointInTimeRecovery       *TablePointInTimeRecovery    `ub:"point-in-time-recovery"`
	DeletionProtectionEnabled *bool                        `ub:"deletion-protection-enabled"`
	Tags                      *map[string]string           `ub:"tags"`
}

// TableResourceOutput holds the values DynamoDB computes for a table. Arn identifies the
// table in policies and tag calls. StreamArn and StreamLabel identify the
// table's stream and are empty when no stream is enabled.
type TableResourceOutput struct {
	Arn         string `ub:"arn"`
	StreamArn   string `ub:"stream-arn"`
	StreamLabel string `ub:"stream-label"`
}

func (r *TableResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs DynamoDB fixes when a table is created. The
// name is the table's identity, the hash and range keys make up the primary key,
// and a local secondary index can only be defined at creation; a change to any
// of them requires a new table. The warm-throughput decrease rule is not here:
// a decrease is rejected by the API rather than replacing the table, and an
// increase is applied in place, so it stays API-enforced.
func (r *TableResource) ReplaceFields() []string {
	return []string{"name", "hash-key", "range-key", "local-secondary-index"}
}

// Constraints declares the rules DynamoDB places on a table's inputs. The
// billing-mode rules are the central pair: under PROVISIONED, the table and
// every global secondary index need read and write capacity, and the on-demand
// throughput block conflicts with capacity. The stream view type requires a
// stream and the time-to-live attribute name requires time-to-live. The enums
// fix the value sets, and the recovery period is bounded. The all-attributes-
// indexed rule spans the table key and every index and cannot be expressed
// here, so it is checked in Create and Update.
func (r TableResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.ReadCapacity, r.OnDemandThroughput),
		constraint.AtMostOneOf(r.WriteCapacity, r.OnDemandThroughput),
		constraint.When(constraint.Equals(r.BillingMode, "PROVISIONED")).
			Require(constraint.Present(r.ReadCapacity), constraint.AtLeast(r.ReadCapacity, 1),
				constraint.Present(r.WriteCapacity), constraint.AtLeast(r.WriteCapacity, 1)).
			Message("read-capacity and write-capacity are required and at least 1 " +
				"when billing-mode is PROVISIONED"),
		constraint.When(constraint.Equals(r.BillingMode, "PAY_PER_REQUEST")).
			Require(constraint.Absent(r.ReadCapacity), constraint.Absent(r.WriteCapacity)).
			Message("read-capacity and write-capacity must be unset " +
				"when billing-mode is PAY_PER_REQUEST"),
		constraint.When(constraint.Present(r.BillingMode)).
			Require(constraint.OneOf(r.BillingMode, "PROVISIONED", "PAY_PER_REQUEST")).
			Message("billing-mode must be PROVISIONED or PAY_PER_REQUEST"),
		constraint.When(constraint.Present(r.TableClass)).
			Require(constraint.OneOf(r.TableClass, "STANDARD", "STANDARD_INFREQUENT_ACCESS")).
			Message("table-class must be STANDARD or STANDARD_INFREQUENT_ACCESS"),
		constraint.RequiredWith(r.StreamViewType, r.StreamEnabled),
		constraint.When(constraint.IsTrue(r.StreamEnabled)).
			Require(constraint.Present(r.StreamViewType)).
			Message("stream-view-type is required when stream-enabled is true"),
		constraint.When(constraint.IsFalse(r.StreamEnabled)).
			Require(constraint.Absent(r.StreamViewType)).
			Message("stream-view-type must be unset when stream-enabled is false"),
		constraint.When(constraint.Present(r.StreamViewType)).
			Require(constraint.OneOf(r.StreamViewType,
				"NEW_IMAGE", "OLD_IMAGE", "NEW_AND_OLD_IMAGES", "KEYS_ONLY")).
			Message("stream-view-type must be a valid DynamoDB stream view type"),
		constraint.When(constraint.IsTrue(r.Ttl.Enabled)).
			Require(constraint.NotEmpty(r.Ttl.AttributeName)).
			Message("ttl attribute-name is required when ttl is enabled"),
		constraint.When(constraint.Present(r.PointInTimeRecovery.RecoveryPeriodInDays)).
			Require(constraint.AtLeast(r.PointInTimeRecovery.RecoveryPeriodInDays, 1),
				constraint.AtMost(r.PointInTimeRecovery.RecoveryPeriodInDays, 35)).
			Message("point-in-time-recovery recovery-period-in-days must be between 1 and 35"),
		constraint.ForEach(r.Attribute, func(a TableAttribute) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(a.Type, "S", "N", "B")).
					Message("attribute type must be S, N, or B"),
			}
		}),
		constraint.ForEach(r.LocalSecondaryIndex,
			func(lsi TableLocalSecondaryIndex) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(lsi.ProjectionType,
						"ALL", "INCLUDE", "KEYS_ONLY")).
						Message("local-secondary-index projection-type must be " +
							"ALL, INCLUDE, or KEYS_ONLY"),
				}
			}),
		constraint.ForEach(r.GlobalSecondaryIndex,
			func(gsi TableGlobalSecondaryIndex) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(gsi.ProjectionType,
						"ALL", "INCLUDE", "KEYS_ONLY")).
						Message("global-secondary-index projection-type must be " +
							"ALL, INCLUDE, or KEYS_ONLY"),
					constraint.AtMostOneOf(gsi.ReadCapacity, gsi.OnDemandThroughput),
					constraint.AtMostOneOf(gsi.WriteCapacity, gsi.OnDemandThroughput),
					constraint.When(constraint.Equals(r.BillingMode, "PROVISIONED")).
						Require(constraint.Present(gsi.ReadCapacity),
							constraint.AtLeast(gsi.ReadCapacity, 1),
							constraint.Present(gsi.WriteCapacity),
							constraint.AtLeast(gsi.WriteCapacity, 1)).
						Message("global-secondary-index read-capacity and write-capacity " +
							"are required and at least 1 when billing-mode is PROVISIONED"),
					constraint.When(constraint.Equals(r.BillingMode, "PAY_PER_REQUEST")).
						Require(constraint.Absent(gsi.ReadCapacity),
							constraint.Absent(gsi.WriteCapacity)).
						Message("global-secondary-index read-capacity and write-capacity " +
							"must be unset when billing-mode is PAY_PER_REQUEST"),
				}
			}),
	}
}

func (r *TableResource) Create(ctx context.Context, cfg *awsCfg) (*TableResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := r.createTable(ctx, client); err != nil {
		return nil, err
	}
	desc, err := waitTableActive(ctx, client, r.Name)
	if err != nil {
		return nil, err
	}
	if r.WarmThroughput != nil {
		if err := waitTableWarmThroughputActive(ctx, client, r.Name); err != nil {
			return nil, err
		}
	}
	for _, gsi := range ptr.Value(r.GlobalSecondaryIndex) {
		if err := waitGSIActive(ctx, client, r.Name, gsi.Name); err != nil {
			return nil, err
		}
	}
	if r.Ttl != nil && aws.ToBool(r.Ttl.Enabled) {
		if err := updateTimeToLive(ctx, client, r.Name, r.Ttl); err != nil {
			return nil, err
		}
	}
	if r.PointInTimeRecovery != nil && aws.ToBool(r.PointInTimeRecovery.Enabled) {
		if err := updatePITR(ctx, client, r.Name, r.PointInTimeRecovery); err != nil {
			return nil, err
		}
	}
	return tableOutput(desc), nil
}

// createTable issues CreateTable with the create-time settings, retrying the
// throttling and simultaneity-limit errors DynamoDB returns while too many
// table or index operations run at once.
func (r *TableResource) createTable(ctx context.Context, client *dynamodb.Client) error {
	in := &dynamodb.CreateTableInput{
		TableName:                 aws.String(r.Name),
		AttributeDefinitions:      attributeDefinitions(ptr.Value(r.Attribute)),
		KeySchema:                 keySchema(r.HashKey, r.RangeKey),
		LocalSecondaryIndexes:     localSecondaryIndexes(r.HashKey, ptr.Value(r.LocalSecondaryIndex)),
		GlobalSecondaryIndexes:    globalSecondaryIndexes(ptr.Value(r.GlobalSecondaryIndex)),
		OnDemandThroughput:        onDemandThroughput(r.OnDemandThroughput),
		StreamSpecification:       streamSpecification(r.StreamEnabled, r.StreamViewType),
		WarmThroughput:            warmThroughput(r.WarmThroughput),
		DeletionProtectionEnabled: r.DeletionProtectionEnabled,
		Tags:                      tableTags(ptr.Value(r.Tags)),
	}
	// Provisioned capacity is sent only in provisioned mode; DynamoDB rejects it
	// in on-demand mode, where capacity is fixed at zero.
	if r.provisioned() {
		in.ProvisionedThroughput = provisionedThroughput(r.ReadCapacity, r.WriteCapacity)
	}
	if r.BillingMode != nil {
		in.BillingMode = dynamodbtypes.BillingMode(*r.BillingMode)
	}
	if r.TableClass != nil {
		in.TableClass = dynamodbtypes.TableClass(*r.TableClass)
	}
	if r.ServerSideEncryption != nil {
		in.SSESpecification = sseSpecification(r.ServerSideEncryption)
	}
	err := retry.OnError(ctx, createRetryable, func(ctx context.Context) error {
		_, err := client.CreateTable(ctx, in)
		return err
	}, retry.WithTimeout(createTableTimeout))
	if err != nil {
		return fmt.Errorf("create table %s: %w", r.Name, err)
	}
	return nil
}

func (r *TableResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *TableResourceOutput,
) (*TableResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	desc, err := describeTable(ctx, client, r.Name)
	if err != nil {
		return nil, err
	}
	if desc == nil {
		return nil, runtime.ErrNotFound
	}
	return tableOutput(desc), nil
}

func (r *TableResource) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[TableResource, *TableResourceOutput],
) (*TableResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := r.reconcile(ctx, client, prior); err != nil {
		return nil, err
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, prior.Outputs.Arn); err != nil {
			return nil, err
		}
	}
	desc, err := describeTable(ctx, client, r.Name)
	if err != nil {
		return nil, err
	}
	if desc == nil {
		return nil, runtime.ErrNotFound
	}
	return tableOutput(desc), nil
}

func (r *TableResource) Delete(ctx context.Context, cfg *awsCfg, prior *TableResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// On a replace, Delete receives the prior table's outputs while the receiver
	// holds the new inputs, so a name change would orphan the old table if the
	// delete keyed off the receiver name. The prior ARN identifies the exact
	// table that was created, and DynamoDB accepts an ARN wherever it takes a
	// table name, so it is the delete handle; the receiver name is a fallback for
	// a state entry with no recorded ARN.
	handle := prior.Arn
	if handle == "" {
		handle = r.Name
	}
	err = retry.OnError(ctx, deleteRetryable, func(ctx context.Context) error {
		_, err := client.DeleteTable(ctx, &dynamodb.DeleteTableInput{
			TableName: aws.String(handle),
		})
		return err
	}, retry.WithTimeout(deleteTableTimeout))
	if err != nil {
		// A table already gone counts as deleted.
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete table %s: %w", handle, err)
	}
	return waitTableDeleted(ctx, client, handle)
}

// validate checks the cross-collection rule the constraint vocabulary cannot
// express: every attribute definition must be used by a key, and every key must
// reference a defined attribute.
func (r *TableResource) validate() error {
	return validateAttributesIndexed(
		ptr.Value(r.Attribute), r.HashKey, r.RangeKey, ptr.Value(r.LocalSecondaryIndex),
		ptr.Value(r.GlobalSecondaryIndex))
}

// streamSpecification builds the SDK stream spec from the stream-enabled flag
// and view type, or nil when the flag is unset so DynamoDB leaves the stream
// untouched. A disabled stream sends the flag without a view type.
func streamSpecification(enabled *bool, viewType *string) *dynamodbtypes.StreamSpecification {
	if enabled == nil {
		return nil
	}
	spec := &dynamodbtypes.StreamSpecification{StreamEnabled: enabled}
	if *enabled && viewType != nil {
		spec.StreamViewType = dynamodbtypes.StreamViewType(*viewType)
	}
	return spec
}

// tableOutput builds the computed outputs from a table description.
func tableOutput(desc *dynamodbtypes.TableDescription) *TableResourceOutput {
	return &TableResourceOutput{
		Arn:         aws.ToString(desc.TableArn),
		StreamArn:   aws.ToString(desc.LatestStreamArn),
		StreamLabel: aws.ToString(desc.LatestStreamLabel),
	}
}

// syncTags reconciles the table's tags with the desired set. DynamoDB addresses
// table tags by ARN and lists them through a token-paged ListTagsOfResource.
func (r *TableResource) syncTags(ctx context.Context, client *dynamodb.Client, arn string) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			current := map[string]string{}
			var token *string
			for {
				resp, err := client.ListTagsOfResource(ctx,
					&dynamodb.ListTagsOfResourceInput{
						ResourceArn: aws.String(arn),
						NextToken:   token,
					})
				if err != nil {
					return nil, fmt.Errorf("list tags of resource %s: %w", arn, err)
				}
				for _, t := range resp.Tags {
					current[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
				if resp.NextToken == nil {
					return current, nil
				}
				token = resp.NextToken
			}
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagResource(ctx, &dynamodb.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        tableTags(upsert),
			}); err != nil {
				return fmt.Errorf("tag resource %s: %w", arn, err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagResource(ctx, &dynamodb.UntagResourceInput{
				ResourceArn: aws.String(arn),
				TagKeys:     remove,
			}); err != nil {
				return fmt.Errorf("untag resource %s: %w", arn, err)
			}
			return nil
		},
	)
}

// tableTags converts a desired tag map into the DynamoDB SDK tag list, ordered
// by key so the request is deterministic.
func tableTags(tags map[string]string) []dynamodbtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]dynamodbtypes.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, dynamodbtypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}
