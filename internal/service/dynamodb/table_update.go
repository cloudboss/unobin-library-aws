package dynamodb

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	dynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/cloudboss/unobin/pkg/runtime"
)

// createTableTimeout and deleteTableTimeout bound the create and delete retries
// through the throttling and simultaneity-limit errors DynamoDB returns while
// many table operations run at once. They match the windows the Terraform
// provider uses.
const (
	createTableTimeout = 30 * time.Minute
	deleteTableTimeout = 10 * time.Minute
)

// reconcile applies an update to the table in the order DynamoDB requires, each
// step gated on a change to the inputs it reconciles. Indexes are deleted first
// (including the delete half of any index that must be recreated), then the
// table class alone, then a stream view-type change that needs the stream
// cycled, then the one main UpdateTable that sends the capacity, billing mode,
// deletion protection, on-demand throughput, stream toggle, and the capacity and
// on-demand facets of in-place index updates, then each warm-throughput index
// update on its own, then new indexes one at a time (including the create half
// of a recreated index), then encryption, time-to-live, point-in-time recovery,
// and table warm throughput. The order keeps operations DynamoDB will not run
// together apart: a table-class change cannot share a call, an index create and
// delete cannot share a call, a view-type change is rejected on an
// already-enabled stream, and a warm-throughput index update cannot share a call
// with a capacity or on-demand change.
func (r *Table) reconcile(
	ctx context.Context, client *dynamodb.Client, prior runtime.Prior[Table, *TableOutput],
) error {
	diff := diffGSIs(prior.Inputs.GlobalSecondaryIndex, r.GlobalSecondaryIndex)
	deletes := append(diff.deletes, recreateNames(diff.recreates)...)
	if err := r.deleteIndexes(ctx, client, deletes); err != nil {
		return err
	}
	if err := r.reconcileTableClass(ctx, client, prior); err != nil {
		return err
	}
	if err := r.reconcileStreamViewType(ctx, client, prior); err != nil {
		return err
	}
	if err := r.mainUpdate(ctx, client, prior, diff.updates); err != nil {
		return err
	}
	if err := r.updateWarmIndexes(ctx, client, gsiWarmUpdates(diff.updates)); err != nil {
		return err
	}
	creates := append(diff.creates, diff.recreates...)
	if err := r.createIndexes(ctx, client, creates); err != nil {
		return err
	}
	if err := r.reconcileSSE(ctx, client, prior); err != nil {
		return err
	}
	if err := r.reconcileTTL(ctx, client, prior); err != nil {
		return err
	}
	if err := r.reconcilePITR(ctx, client, prior); err != nil {
		return err
	}
	return r.reconcileWarmThroughput(ctx, client, prior)
}

// deleteIndexes removes each global secondary index that is no longer desired,
// one UpdateTable per index, waiting for each to disappear before the next.
// DynamoDB allows only one online index delete at a time.
func (r *Table) deleteIndexes(
	ctx context.Context, client *dynamodb.Client, names []string,
) error {
	for _, name := range names {
		_, err := client.UpdateTable(ctx, &dynamodb.UpdateTableInput{
			TableName: aws.String(r.Name),
			GlobalSecondaryIndexUpdates: []dynamodbtypes.GlobalSecondaryIndexUpdate{
				gsiDeleteUpdate(name),
			},
		})
		if err != nil {
			return fmt.Errorf("delete index %s on table %s: %w", name, r.Name, err)
		}
		if err := waitGSIDeleted(ctx, client, r.Name, name); err != nil {
			return err
		}
	}
	return nil
}

// reconcileTableClass changes the table class when it changed, in its own
// UpdateTable, since DynamoDB rejects a table-class change combined with any
// other change.
func (r *Table) reconcileTableClass(
	ctx context.Context, client *dynamodb.Client, prior runtime.Prior[Table, *TableOutput],
) error {
	if !runtime.Changed(prior.Inputs.TableClass, r.TableClass) || r.TableClass == nil {
		return nil
	}
	_, err := client.UpdateTable(ctx, &dynamodb.UpdateTableInput{
		TableName:  aws.String(r.Name),
		TableClass: dynamodbtypes.TableClass(*r.TableClass),
	})
	if err != nil {
		return fmt.Errorf("update table class %s: %w", r.Name, err)
	}
	_, err = waitTableActive(ctx, client, r.Name)
	return err
}

// reconcileStreamViewType handles a change to the stream view type when the
// stream stays enabled. DynamoDB rejects changing the view type on an enabled
// stream, so the stream is disabled and re-enabled with the new view type. When
// the stream toggle itself changed, the main UpdateTable handles the stream
// instead and this step is skipped.
func (r *Table) reconcileStreamViewType(
	ctx context.Context, client *dynamodb.Client, prior runtime.Prior[Table, *TableOutput],
) error {
	viewChanged := runtime.Changed(prior.Inputs.StreamViewType, r.StreamViewType)
	enabledChanged := runtime.Changed(prior.Inputs.StreamEnabled, r.StreamEnabled)
	if !viewChanged || enabledChanged || !aws.ToBool(r.StreamEnabled) {
		return nil
	}
	disable := &dynamodbtypes.StreamSpecification{StreamEnabled: aws.Bool(false)}
	if _, err := client.UpdateTable(ctx, &dynamodb.UpdateTableInput{
		TableName:           aws.String(r.Name),
		StreamSpecification: disable,
	}); err != nil {
		return fmt.Errorf("disable stream on table %s: %w", r.Name, err)
	}
	if _, err := waitTableActive(ctx, client, r.Name); err != nil {
		return err
	}
	enable := streamSpecification(r.StreamEnabled, r.StreamViewType)
	if _, err := client.UpdateTable(ctx, &dynamodb.UpdateTableInput{
		TableName:           aws.String(r.Name),
		StreamSpecification: enable,
	}); err != nil {
		return fmt.Errorf("re-enable stream on table %s: %w", r.Name, err)
	}
	_, err := waitTableActive(ctx, client, r.Name)
	return err
}

// mainUpdate issues the one UpdateTable that sends the in-place table changes:
// billing mode and capacity, deletion protection, on-demand throughput, the
// stream toggle, and the capacity and on-demand facets of existing index
// updates. It runs only when one of those changed. The index updates are
// billing-mode-branched, since provisioned capacity is not a valid update in
// on-demand mode, and they never include warm throughput, which is applied on
// its own afterward. After the call the table and each index updated here are
// waited active.
func (r *Table) mainUpdate(
	ctx context.Context,
	client *dynamodb.Client,
	prior runtime.Prior[Table, *TableOutput],
	updates []gsiUpdate,
) error {
	in := &dynamodb.UpdateTableInput{TableName: aws.String(r.Name)}
	changed := false
	billingChanged := runtime.Changed(prior.Inputs.BillingMode, r.BillingMode)
	capacityChanged := runtime.Changed(prior.Inputs.ReadCapacity, r.ReadCapacity) ||
		runtime.Changed(prior.Inputs.WriteCapacity, r.WriteCapacity)
	if billingChanged && r.BillingMode != nil {
		in.BillingMode = dynamodbtypes.BillingMode(*r.BillingMode)
		changed = true
	}
	if (billingChanged || capacityChanged) && r.provisioned() {
		in.ProvisionedThroughput = provisionedThroughput(r.ReadCapacity, r.WriteCapacity)
		changed = true
	}
	if runtime.Changed(prior.Inputs.DeletionProtectionEnabled, r.DeletionProtectionEnabled) &&
		r.DeletionProtectionEnabled != nil {
		in.DeletionProtectionEnabled = r.DeletionProtectionEnabled
		changed = true
	}
	if runtime.Changed(prior.Inputs.OnDemandThroughput, r.OnDemandThroughput) &&
		r.OnDemandThroughput != nil {
		in.OnDemandThroughput = onDemandThroughput(r.OnDemandThroughput)
		changed = true
	}
	if runtime.Changed(prior.Inputs.StreamEnabled, r.StreamEnabled) && r.StreamEnabled != nil {
		in.StreamSpecification = streamSpecification(r.StreamEnabled, r.StreamViewType)
		changed = true
	}
	if actions := gsiMainUpdateActions(updates, r.provisioned()); len(actions) > 0 {
		in.GlobalSecondaryIndexUpdates = actions
		changed = true
	}
	if !changed {
		return nil
	}
	if _, err := client.UpdateTable(ctx, in); err != nil {
		return fmt.Errorf("update table %s: %w", r.Name, err)
	}
	if _, err := waitTableActive(ctx, client, r.Name); err != nil {
		return err
	}
	for _, name := range gsiMainUpdateNames(updates, r.provisioned()) {
		if err := waitGSIActive(ctx, client, r.Name, name); err != nil {
			return err
		}
	}
	return nil
}

// updateWarmIndexes raises the warm throughput of each in-place index update on
// its own, one UpdateTable per index, after the main update. A warm-throughput
// update cannot share a call with a capacity or on-demand change, so each runs
// alone and is waited active before the next.
func (r *Table) updateWarmIndexes(
	ctx context.Context, client *dynamodb.Client, warm []TableGlobalSecondaryIndex,
) error {
	for _, gsi := range warm {
		_, err := client.UpdateTable(ctx, &dynamodb.UpdateTableInput{
			TableName: aws.String(r.Name),
			GlobalSecondaryIndexUpdates: []dynamodbtypes.GlobalSecondaryIndexUpdate{
				gsiWarmUpdateAction(gsi),
			},
		})
		if err != nil {
			return fmt.Errorf(
				"update warm throughput of index %s on table %s: %w", gsi.Name, r.Name, err)
		}
		if _, err := waitTableActive(ctx, client, r.Name); err != nil {
			return err
		}
		if err := waitGSIActive(ctx, client, r.Name, gsi.Name); err != nil {
			return err
		}
	}
	return nil
}

// createIndexes adds each new global secondary index, one UpdateTable per index,
// each sending the full attribute set so a key on a new attribute is defined,
// waiting for each to become active before the next. DynamoDB allows only one
// online index create at a time.
func (r *Table) createIndexes(
	ctx context.Context, client *dynamodb.Client, creates []TableGlobalSecondaryIndex,
) error {
	for _, gsi := range creates {
		_, err := client.UpdateTable(ctx, &dynamodb.UpdateTableInput{
			TableName:            aws.String(r.Name),
			AttributeDefinitions: attributeDefinitions(r.Attribute),
			GlobalSecondaryIndexUpdates: []dynamodbtypes.GlobalSecondaryIndexUpdate{
				gsiCreateUpdate(gsi),
			},
		})
		if err != nil {
			return fmt.Errorf("create index %s on table %s: %w", gsi.Name, r.Name, err)
		}
		if err := waitGSIActive(ctx, client, r.Name, gsi.Name); err != nil {
			return err
		}
	}
	return nil
}

// reconcileSSE applies the encryption block when it changed, then waits for
// encryption to settle.
func (r *Table) reconcileSSE(
	ctx context.Context, client *dynamodb.Client, prior runtime.Prior[Table, *TableOutput],
) error {
	if !runtime.Changed(prior.Inputs.ServerSideEncryption, r.ServerSideEncryption) {
		return nil
	}
	return updateSSE(ctx, client, r.Name, r.ServerSideEncryption)
}

// reconcileTTL applies the time-to-live block when it changed, then waits for
// the change to settle.
func (r *Table) reconcileTTL(
	ctx context.Context, client *dynamodb.Client, prior runtime.Prior[Table, *TableOutput],
) error {
	if !runtime.Changed(prior.Inputs.Ttl, r.Ttl) {
		return nil
	}
	return updateTimeToLive(ctx, client, r.Name, r.Ttl)
}

// reconcilePITR applies the point-in-time-recovery block when it changed, then
// waits for the change to settle.
func (r *Table) reconcilePITR(
	ctx context.Context, client *dynamodb.Client, prior runtime.Prior[Table, *TableOutput],
) error {
	if !runtime.Changed(prior.Inputs.PointInTimeRecovery, r.PointInTimeRecovery) {
		return nil
	}
	return updatePITR(ctx, client, r.Name, r.PointInTimeRecovery)
}

// reconcileWarmThroughput applies the warm-throughput block when it changed and
// is set. An unset block is never sent, so DynamoDB's automatic warm throughput
// is left alone rather than reset, and a decrease the API rejects is left to the
// API to enforce.
func (r *Table) reconcileWarmThroughput(
	ctx context.Context, client *dynamodb.Client, prior runtime.Prior[Table, *TableOutput],
) error {
	if !runtime.Changed(prior.Inputs.WarmThroughput, r.WarmThroughput) || r.WarmThroughput == nil {
		return nil
	}
	return updateWarmThroughput(ctx, client, r.Name, r.WarmThroughput)
}

// provisioned reports whether the table is in provisioned billing mode. An unset
// billing mode is treated as provisioned, the DynamoDB default.
func (r *Table) provisioned() bool {
	return r.BillingMode == nil || *r.BillingMode == billingModeProvisioned
}
