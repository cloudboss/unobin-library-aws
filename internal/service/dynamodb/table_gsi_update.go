package dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/cloudboss/unobin/pkg/runtime"
)

// gsiUpdate pairs an index's prior and desired config for an in-place update, so
// the per-facet update actions can be built from what actually changed.
type gsiUpdate struct {
	old     TableGlobalSecondaryIndex
	current TableGlobalSecondaryIndex
}

// gsiDiff is the set of global-secondary-index changes between a prior config
// and a desired one, split into the groups DynamoDB serializes: indexes to
// delete, indexes whose throughput changed and can be updated in place, indexes
// that changed in a way the API cannot apply in place and so must be recreated,
// and indexes to create. A create and a delete cannot be requested in the same
// UpdateTable, and only one index may be created or deleted per call, so the
// groups run separately. A recreated index runs through the delete group first
// and then the create group.
type gsiDiff struct {
	deletes   []string
	updates   []gsiUpdate
	recreates []TableGlobalSecondaryIndex
	creates   []TableGlobalSecondaryIndex
}

// diffGSIs compares the prior and desired global-secondary-index lists and
// classifies each. An index present before but not now is deleted; one present
// now but not before is created. An index present in both is recreated when it
// changed in a way DynamoDB cannot apply to a live index (its projection, its
// projected non-key attributes, its key schema, or a warm-throughput decrease),
// since those are fixed once the index exists; otherwise it is an in-place
// update when only its throughput inputs changed. The desired order is preserved
// within each group.
func diffGSIs(prior, desired []TableGlobalSecondaryIndex) gsiDiff {
	priorByName := indexNames(prior)
	desiredByName := indexNames(desired)
	var diff gsiDiff
	for _, gsi := range prior {
		if _, ok := desiredByName[gsi.Name]; !ok {
			diff.deletes = append(diff.deletes, gsi.Name)
		}
	}
	for _, gsi := range desired {
		old, ok := priorByName[gsi.Name]
		if !ok {
			diff.creates = append(diff.creates, gsi)
			continue
		}
		if gsiNeedsRecreate(old, gsi) {
			diff.recreates = append(diff.recreates, gsi)
			continue
		}
		if gsiThroughputChanged(old, gsi) {
			diff.updates = append(diff.updates, gsiUpdate{old: old, current: gsi})
		}
	}
	return diff
}

// recreateNames returns the names of the indexes to recreate, for routing them
// through the delete group before the create group.
func recreateNames(recreates []TableGlobalSecondaryIndex) []string {
	if len(recreates) == 0 {
		return nil
	}
	out := make([]string, 0, len(recreates))
	for _, gsi := range recreates {
		out = append(out, gsi.Name)
	}
	return out
}

// gsiNeedsRecreate reports whether an index that exists in both configs changed
// in a way DynamoDB cannot apply to a live index, so it must be deleted and
// created again. The projection type, the projected non-key attributes, and the
// key schema are fixed once an index exists, and warm throughput can be raised
// in place but not lowered, so a decrease also forces a recreate.
func gsiNeedsRecreate(old, current TableGlobalSecondaryIndex) bool {
	return runtime.Changed(old.ProjectionType, current.ProjectionType) ||
		nonKeyAttributesChanged(old.NonKeyAttributes, current.NonKeyAttributes) ||
		runtime.Changed(old.HashKey, current.HashKey) ||
		runtime.Changed(old.RangeKey, current.RangeKey) ||
		warmThroughputDecreased(old.WarmThroughput, current.WarmThroughput)
}

// nonKeyAttributesChanged reports whether the projected non-key attributes of an
// index changed. Order is not significant to DynamoDB, so the two lists are
// compared as sets.
func nonKeyAttributesChanged(old, current []string) bool {
	if len(old) != len(current) {
		return true
	}
	seen := make(map[string]struct{}, len(old))
	for _, a := range old {
		seen[a] = struct{}{}
	}
	for _, a := range current {
		if _, ok := seen[a]; !ok {
			return true
		}
	}
	return false
}

// warmThroughputDecreased reports whether either warm-throughput rate of an index
// fell below its prior value. A rate counts as decreased only when the new
// value is set and lower than the old, matching the API rule that warm
// throughput can be raised in place but not lowered. A removed block (new nil)
// is not a decrease, since an unset block is never sent.
func warmThroughputDecreased(old, current *TableWarmThroughput) bool {
	if old == nil || current == nil {
		return false
	}
	return unitDecreased(old.ReadUnitsPerSecond, current.ReadUnitsPerSecond) ||
		unitDecreased(old.WriteUnitsPerSecond, current.WriteUnitsPerSecond)
}

// unitDecreased reports whether a throughput unit decreased: both values set and
// the new one below the old.
func unitDecreased(old, current *int64) bool {
	if old == nil || current == nil {
		return false
	}
	return *current < *old
}

// gsiThroughputChanged reports whether the throughput inputs of an existing
// index changed between two configs. Only the capacity, on-demand, and warm
// throughput of a live index can be updated, so only those are compared.
func gsiThroughputChanged(old, current TableGlobalSecondaryIndex) bool {
	return runtime.Changed(old.ReadCapacity, current.ReadCapacity) ||
		runtime.Changed(old.WriteCapacity, current.WriteCapacity) ||
		runtime.Changed(old.OnDemandThroughput, current.OnDemandThroughput) ||
		runtime.Changed(old.WarmThroughput, current.WarmThroughput)
}

// gsiDeleteUpdate builds the UpdateTable global-secondary-index action that
// removes one index.
func gsiDeleteUpdate(name string) dynamodbtypes.GlobalSecondaryIndexUpdate {
	return dynamodbtypes.GlobalSecondaryIndexUpdate{
		Delete: &dynamodbtypes.DeleteGlobalSecondaryIndexAction{
			IndexName: aws.String(name),
		},
	}
}

// gsiCreateUpdate builds the UpdateTable global-secondary-index action that adds
// one index. Provisioned throughput is included when set, and the on-demand and
// warm-throughput blocks when present, so the action matches the table's billing
// mode.
func gsiCreateUpdate(gsi TableGlobalSecondaryIndex) dynamodbtypes.GlobalSecondaryIndexUpdate {
	return dynamodbtypes.GlobalSecondaryIndexUpdate{
		Create: &dynamodbtypes.CreateGlobalSecondaryIndexAction{
			IndexName:             aws.String(gsi.Name),
			KeySchema:             keySchema(gsi.HashKey, gsi.RangeKey),
			Projection:            projection(gsi.ProjectionType, gsi.NonKeyAttributes),
			ProvisionedThroughput: provisionedThroughput(gsi.ReadCapacity, gsi.WriteCapacity),
			OnDemandThroughput:    onDemandThroughput(gsi.OnDemandThroughput),
			WarmThroughput:        warmThroughput(gsi.WarmThroughput),
		},
	}
}

// gsiMainUpdateActions builds the in-place index update actions that ride the
// main UpdateTable: the provisioned-capacity and on-demand facets of each
// changed index, each as its own Update action. Warm throughput is never sent
// here; it can only be raised, and the API rejects it combined with a capacity
// or on-demand change in one call, so it runs on its own afterward. In
// PAY_PER_REQUEST mode provisioned capacity is not a valid update, so only the
// on-demand facet is sent, which means an index that changed only its warm
// throughput contributes nothing to the main call.
func gsiMainUpdateActions(
	updates []gsiUpdate, provisioned bool,
) []dynamodbtypes.GlobalSecondaryIndexUpdate {
	if len(updates) == 0 {
		return nil
	}
	var out []dynamodbtypes.GlobalSecondaryIndexUpdate
	for _, u := range updates {
		capacityChanged := runtime.Changed(u.old.ReadCapacity, u.current.ReadCapacity) ||
			runtime.Changed(u.old.WriteCapacity, u.current.WriteCapacity)
		if provisioned && capacityChanged {
			out = append(out, dynamodbtypes.GlobalSecondaryIndexUpdate{
				Update: &dynamodbtypes.UpdateGlobalSecondaryIndexAction{
					IndexName: aws.String(u.current.Name),
					ProvisionedThroughput: provisionedThroughput(
						u.current.ReadCapacity, u.current.WriteCapacity),
				},
			})
		}
		if runtime.Changed(u.old.OnDemandThroughput, u.current.OnDemandThroughput) {
			out = append(out, dynamodbtypes.GlobalSecondaryIndexUpdate{
				Update: &dynamodbtypes.UpdateGlobalSecondaryIndexAction{
					IndexName:          aws.String(u.current.Name),
					OnDemandThroughput: onDemandThroughput(u.current.OnDemandThroughput),
				},
			})
		}
	}
	return out
}

// gsiMainUpdateNames returns the names of the indexes the main UpdateTable
// updates in place, so each can be waited active after the call. An index whose
// only change is warm throughput is excluded, since it is updated on its own.
func gsiMainUpdateNames(updates []gsiUpdate, provisioned bool) []string {
	var out []string
	for _, u := range updates {
		capacityChanged := runtime.Changed(u.old.ReadCapacity, u.current.ReadCapacity) ||
			runtime.Changed(u.old.WriteCapacity, u.current.WriteCapacity)
		onDemandChanged := runtime.Changed(u.old.OnDemandThroughput, u.current.OnDemandThroughput)
		if (provisioned && capacityChanged) || onDemandChanged {
			out = append(out, u.current.Name)
		}
	}
	return out
}

// gsiWarmUpdates returns the indexes whose warm throughput changed, for the
// isolated per-index update path that runs after the main UpdateTable. Each runs
// in its own UpdateTable carrying only its warm-throughput Update action.
func gsiWarmUpdates(updates []gsiUpdate) []TableGlobalSecondaryIndex {
	var out []TableGlobalSecondaryIndex
	for _, u := range updates {
		if runtime.Changed(u.old.WarmThroughput, u.current.WarmThroughput) {
			out = append(out, u.current)
		}
	}
	return out
}

// gsiWarmUpdateAction builds the UpdateTable action that raises one index's warm
// throughput, with no other facet set so it is never combined with a capacity or
// on-demand change.
func gsiWarmUpdateAction(gsi TableGlobalSecondaryIndex) dynamodbtypes.GlobalSecondaryIndexUpdate {
	return dynamodbtypes.GlobalSecondaryIndexUpdate{
		Update: &dynamodbtypes.UpdateGlobalSecondaryIndexAction{
			IndexName:      aws.String(gsi.Name),
			WarmThroughput: warmThroughput(gsi.WarmThroughput),
		},
	}
}
