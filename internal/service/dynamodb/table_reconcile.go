package dynamodb

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	dynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

// pitrEnableTimeout bounds the retry while DynamoDB is still enabling backups
// for a table and rejects the point-in-time-recovery call as unavailable.
const pitrEnableTimeout = 20 * time.Minute

// updateTimeToLive enables or disables time-to-live on the table to match the
// block, then waits for the change to settle. AttributeName is required by the
// API when enabling and ignored when disabling; the configured name is sent
// either way, since DynamoDB keeps the prior attribute on a disable.
func updateTimeToLive(
	ctx context.Context, client *dynamodb.Client, name string, ttl *TableTtl,
) error {
	enabled := ttl != nil && aws.ToBool(ttl.Enabled)
	spec := &dynamodbtypes.TimeToLiveSpecification{
		Enabled:       aws.Bool(enabled),
		AttributeName: ttlAttributeName(ttl),
	}
	_, err := client.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName:               aws.String(name),
		TimeToLiveSpecification: spec,
	})
	if err != nil {
		return fmt.Errorf("update time to live %s: %w", name, err)
	}
	return waitTTLUpdated(ctx, client, name, enabled)
}

// ttlAttributeName returns the time-to-live attribute name to send. DynamoDB
// requires a non-empty name even on a disable, so an empty configured name
// falls back to a placeholder the disable ignores.
func ttlAttributeName(ttl *TableTtl) *string {
	if ttl != nil && ttl.AttributeName != nil && *ttl.AttributeName != "" {
		return ttl.AttributeName
	}
	return aws.String("ttl")
}

// updatePITR enables or disables point-in-time recovery on the table to match
// the block, then waits for the change to settle. Enabling can race the backups
// still coming online, which the API rejects as unavailable, so it retries
// through that window.
func updatePITR(
	ctx context.Context, client *dynamodb.Client, name string, pitr *TablePointInTimeRecovery,
) error {
	enabled := pitr != nil && aws.ToBool(pitr.Enabled)
	spec := &dynamodbtypes.PointInTimeRecoverySpecification{
		PointInTimeRecoveryEnabled: aws.Bool(enabled),
	}
	if enabled && pitr.RecoveryPeriodInDays != nil {
		spec.RecoveryPeriodInDays = recoveryPeriodDays(pitr.RecoveryPeriodInDays)
	}
	err := retry.OnError(ctx, isContinuousBackupsUnavailable, func(ctx context.Context) error {
		_, err := client.UpdateContinuousBackups(ctx, &dynamodb.UpdateContinuousBackupsInput{
			TableName:                        aws.String(name),
			PointInTimeRecoverySpecification: spec,
		})
		return err
	}, retry.WithTimeout(pitrEnableTimeout))
	if err != nil {
		return fmt.Errorf("update continuous backups %s: %w", name, err)
	}
	return waitPITRUpdated(ctx, client, name, enabled)
}

// updateSSE applies the server-side-encryption block to the table, then waits
// for encryption to settle. A nil or disabled block sends Enabled false, which
// returns the table to the AWS owned key.
func updateSSE(
	ctx context.Context, client *dynamodb.Client, name string, sse *TableServerSideEncryption,
) error {
	spec := sseSpecification(sse)
	_, err := client.UpdateTable(ctx, &dynamodb.UpdateTableInput{
		TableName:        aws.String(name),
		SSESpecification: spec,
	})
	if err != nil {
		return fmt.Errorf("update table encryption %s: %w", name, err)
	}
	return waitSSEUpdated(ctx, client, name, aws.ToBool(spec.Enabled))
}

// sseSpecification builds the SDK encryption spec from the block. An absent
// block disables encryption. A KMS key is sent only when one is configured;
// without it an enabled block selects the AWS managed key.
func sseSpecification(sse *TableServerSideEncryption) *dynamodbtypes.SSESpecification {
	if sse == nil {
		return &dynamodbtypes.SSESpecification{Enabled: aws.Bool(false)}
	}
	spec := &dynamodbtypes.SSESpecification{Enabled: aws.Bool(aws.ToBool(sse.Enabled))}
	if sse.KmsKeyId != nil {
		spec.KMSMasterKeyId = sse.KmsKeyId
		spec.SSEType = dynamodbtypes.SSETypeKms
	}
	return spec
}

// updateWarmThroughput applies the warm-throughput block to the table, then
// waits for the table and its warm throughput to settle. It is called only when
// the block is set, so the AWS-default warm throughput is never overwritten.
func updateWarmThroughput(
	ctx context.Context, client *dynamodb.Client, name string, warm *TableWarmThroughput,
) error {
	_, err := client.UpdateTable(ctx, &dynamodb.UpdateTableInput{
		TableName:      aws.String(name),
		WarmThroughput: warmThroughput(warm),
	})
	if err != nil {
		return fmt.Errorf("update table warm throughput %s: %w", name, err)
	}
	if _, err := waitTableActive(ctx, client, name); err != nil {
		return err
	}
	return waitTableWarmThroughputActive(ctx, client, name)
}

// validateAttributesIndexed checks that every attribute definition is used by a
// key and every key references a defined attribute. DynamoDB rejects a create
// or update that defines an attribute no key uses, and one whose key names an
// attribute that is not defined. The set-membership rule spans the table key,
// every local secondary index, and every global secondary index, so it cannot
// be expressed as a field constraint and is checked here. The returned error
// lists the offending names so the operator can see which to add or remove.
func validateAttributesIndexed(
	attributes []TableAttribute,
	hashKey string,
	rangeKey *string,
	lsis []TableLocalSecondaryIndex,
	gsis []TableGlobalSecondaryIndex,
) error {
	defined := make(map[string]bool, len(attributes))
	for _, a := range attributes {
		defined[a.Name] = true
	}
	used := keyAttributesUsed(hashKey, rangeKey, lsis, gsis)
	var missing []string
	for name := range used {
		if !defined[name] {
			missing = append(missing, name)
		}
	}
	var unused []string
	for name := range defined {
		if !used[name] {
			unused = append(unused, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(unused)
	if len(missing) > 0 {
		return fmt.Errorf("key attributes %v are not defined in attribute", missing)
	}
	if len(unused) > 0 {
		return fmt.Errorf("attribute %v are not used by any key", unused)
	}
	return nil
}

// keyAttributesUsed collects every attribute name referenced by a key: the
// table hash and range keys, each local secondary index range key, and each
// global secondary index hash and range key.
func keyAttributesUsed(
	hashKey string,
	rangeKey *string,
	lsis []TableLocalSecondaryIndex,
	gsis []TableGlobalSecondaryIndex,
) map[string]bool {
	used := map[string]bool{hashKey: true}
	if rangeKey != nil {
		used[*rangeKey] = true
	}
	for _, lsi := range lsis {
		used[lsi.RangeKey] = true
	}
	for _, gsi := range gsis {
		used[gsi.HashKey] = true
		if gsi.RangeKey != nil {
			used[*gsi.RangeKey] = true
		}
	}
	return used
}
