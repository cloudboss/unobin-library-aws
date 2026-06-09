package dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// TableAttribute is one attribute definition: an attribute used in a key schema
// of the table or one of its indexes. Type is the scalar type S, N, or B.
type TableAttribute struct {
	Name string `ub:"name"`
	Type string `ub:"type"`
}

// TableGlobalSecondaryIndex is one global secondary index. HashKey and RangeKey
// name the index key attributes, which must also appear in the table's
// attribute list. ProjectionType is ALL, INCLUDE, or KEYS_ONLY; NonKeyAttributes
// lists the extra attributes projected when the type is INCLUDE. ReadCapacity
// and WriteCapacity are required when the table billing mode is PROVISIONED;
// OnDemandThroughput and WarmThroughput apply otherwise. A global secondary
// index can be created, updated, and deleted on a live table.
type TableGlobalSecondaryIndex struct {
	Name               string                   `ub:"name"`
	HashKey            string                   `ub:"hash-key"`
	RangeKey           *string                  `ub:"range-key"`
	ProjectionType     string                   `ub:"projection-type"`
	NonKeyAttributes   []string                 `ub:"non-key-attributes"`
	ReadCapacity       *int64                   `ub:"read-capacity"`
	WriteCapacity      *int64                   `ub:"write-capacity"`
	OnDemandThroughput *TableOnDemandThroughput `ub:"on-demand-throughput"`
	WarmThroughput     *TableWarmThroughput     `ub:"warm-throughput"`
}

// TableLocalSecondaryIndex is one local secondary index. A local secondary
// index shares the table's hash key and adds its own range key, and is fixed at
// table creation, so a change to any local secondary index replaces the table.
type TableLocalSecondaryIndex struct {
	Name             string   `ub:"name"`
	RangeKey         string   `ub:"range-key"`
	ProjectionType   string   `ub:"projection-type"`
	NonKeyAttributes []string `ub:"non-key-attributes"`
}

// TableOnDemandThroughput caps the read and write request units a table or
// index serves in on-demand billing mode. Each unit is sent only when set, so
// an unset maximum leaves the limit uncapped.
type TableOnDemandThroughput struct {
	MaxReadRequestUnits  *int64 `ub:"max-read-request-units"`
	MaxWriteRequestUnits *int64 `ub:"max-write-request-units"`
}

// TableWarmThroughput pre-warms a table or index to a read and write rate it can
// serve immediately. Each rate is sent only when set. Warm throughput can be
// raised in place but not lowered; the API rejects a decrease.
type TableWarmThroughput struct {
	ReadUnitsPerSecond  *int64 `ub:"read-units-per-second"`
	WriteUnitsPerSecond *int64 `ub:"write-units-per-second"`
}

// TableServerSideEncryption is the table's server-side encryption setting.
// Enabled true selects an AWS managed KMS key, or the customer key named by
// KmsKeyId; false or an absent block leaves the AWS owned key.
type TableServerSideEncryption struct {
	Enabled  *bool   `ub:"enabled"`
	KmsKeyId *string `ub:"kms-key-id"`
}

// TableTtl is the table's time-to-live setting. Enabled true turns on automatic
// expiry of items by the timestamp in AttributeName, which is required when
// enabled.
type TableTtl struct {
	Enabled       *bool   `ub:"enabled"`
	AttributeName *string `ub:"attribute-name"`
}

// TablePointInTimeRecovery is the table's continuous-backup setting. Enabled
// true keeps continuous backups for restore to any point in
// RecoveryPeriodInDays, which DynamoDB defaults to 35.
type TablePointInTimeRecovery struct {
	Enabled              *bool  `ub:"enabled"`
	RecoveryPeriodInDays *int64 `ub:"recovery-period-in-days"`
}

// attributeDefinitions expands the attribute list into the SDK type.
func attributeDefinitions(in []TableAttribute) []dynamodbtypes.AttributeDefinition {
	if len(in) == 0 {
		return nil
	}
	out := make([]dynamodbtypes.AttributeDefinition, 0, len(in))
	for _, a := range in {
		out = append(out, dynamodbtypes.AttributeDefinition{
			AttributeName: aws.String(a.Name),
			AttributeType: dynamodbtypes.ScalarAttributeType(a.Type),
		})
	}
	return out
}

// keySchema builds a table or index key schema from a hash key and an optional
// range key, in the HASH-then-RANGE order DynamoDB requires.
func keySchema(hashKey string, rangeKey *string) []dynamodbtypes.KeySchemaElement {
	schema := []dynamodbtypes.KeySchemaElement{{
		AttributeName: aws.String(hashKey),
		KeyType:       dynamodbtypes.KeyTypeHash,
	}}
	if rangeKey != nil {
		schema = append(schema, dynamodbtypes.KeySchemaElement{
			AttributeName: aws.String(*rangeKey),
			KeyType:       dynamodbtypes.KeyTypeRange,
		})
	}
	return schema
}

// projection builds an index projection from a projection type and the extra
// attributes projected when that type is INCLUDE.
func projection(projectionType string, nonKey []string) *dynamodbtypes.Projection {
	p := &dynamodbtypes.Projection{
		ProjectionType: dynamodbtypes.ProjectionType(projectionType),
	}
	if len(nonKey) > 0 {
		p.NonKeyAttributes = nonKey
	}
	return p
}

// provisionedThroughput builds the SDK provisioned-throughput type from a read
// and write capacity, or nil when neither is set. It is sent for a table or
// index in PROVISIONED billing mode.
func provisionedThroughput(read, write *int64) *dynamodbtypes.ProvisionedThroughput {
	if read == nil && write == nil {
		return nil
	}
	return &dynamodbtypes.ProvisionedThroughput{
		ReadCapacityUnits:  read,
		WriteCapacityUnits: write,
	}
}

// onDemandThroughput expands the on-demand block into the SDK type, sending each
// maximum only when set. A nil block yields nil so no limit is requested.
func onDemandThroughput(in *TableOnDemandThroughput) *dynamodbtypes.OnDemandThroughput {
	if in == nil {
		return nil
	}
	if in.MaxReadRequestUnits == nil && in.MaxWriteRequestUnits == nil {
		return nil
	}
	return &dynamodbtypes.OnDemandThroughput{
		MaxReadRequestUnits:  in.MaxReadRequestUnits,
		MaxWriteRequestUnits: in.MaxWriteRequestUnits,
	}
}

// warmThroughput expands the warm-throughput block into the SDK type, sending
// each rate only when set. A nil block yields nil, so DynamoDB's automatic warm
// throughput is left untouched rather than overwritten.
func warmThroughput(in *TableWarmThroughput) *dynamodbtypes.WarmThroughput {
	if in == nil {
		return nil
	}
	if in.ReadUnitsPerSecond == nil && in.WriteUnitsPerSecond == nil {
		return nil
	}
	return &dynamodbtypes.WarmThroughput{
		ReadUnitsPerSecond:  in.ReadUnitsPerSecond,
		WriteUnitsPerSecond: in.WriteUnitsPerSecond,
	}
}

// localSecondaryIndexes expands the local-secondary-index list into the SDK
// type. Each index shares the table's hash key and adds its own range key.
func localSecondaryIndexes(
	tableHashKey string, in []TableLocalSecondaryIndex,
) []dynamodbtypes.LocalSecondaryIndex {
	if len(in) == 0 {
		return nil
	}
	out := make([]dynamodbtypes.LocalSecondaryIndex, 0, len(in))
	for _, lsi := range in {
		rangeKey := lsi.RangeKey
		out = append(out, dynamodbtypes.LocalSecondaryIndex{
			IndexName:  aws.String(lsi.Name),
			KeySchema:  keySchema(tableHashKey, &rangeKey),
			Projection: projection(lsi.ProjectionType, lsi.NonKeyAttributes),
		})
	}
	return out
}

// globalSecondaryIndexes expands the global-secondary-index list into the SDK
// create type. Provisioned throughput is included when set, and the on-demand
// and warm-throughput blocks when present, so a PROVISIONED table sends capacity
// and a PAY_PER_REQUEST table does not.
func globalSecondaryIndexes(
	in []TableGlobalSecondaryIndex,
) []dynamodbtypes.GlobalSecondaryIndex {
	if len(in) == 0 {
		return nil
	}
	out := make([]dynamodbtypes.GlobalSecondaryIndex, 0, len(in))
	for _, gsi := range in {
		out = append(out, dynamodbtypes.GlobalSecondaryIndex{
			IndexName:             aws.String(gsi.Name),
			KeySchema:             keySchema(gsi.HashKey, gsi.RangeKey),
			Projection:            projection(gsi.ProjectionType, gsi.NonKeyAttributes),
			ProvisionedThroughput: provisionedThroughput(gsi.ReadCapacity, gsi.WriteCapacity),
			OnDemandThroughput:    onDemandThroughput(gsi.OnDemandThroughput),
			WarmThroughput:        warmThroughput(gsi.WarmThroughput),
		})
	}
	return out
}

// indexNames returns the set of index names in a global-secondary-index list,
// used to diff a desired set against a prior one.
func indexNames(in []TableGlobalSecondaryIndex) map[string]TableGlobalSecondaryIndex {
	out := make(map[string]TableGlobalSecondaryIndex, len(in))
	for _, gsi := range in {
		out[gsi.Name] = gsi
	}
	return out
}

// recoveryPeriodDays narrows the optional recovery-period days to the int32 the
// SDK wants, preserving nil so an unset value lets DynamoDB apply its default.
func recoveryPeriodDays(in *int64) *int32 {
	return ptr.Int32(in)
}
