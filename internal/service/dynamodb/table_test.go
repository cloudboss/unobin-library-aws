package dynamodb

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiffGSIs checks that a prior and desired global-secondary-index list are
// classified into the delete, update, recreate, and create groups DynamoDB
// serializes: an index gone is deleted, a new one is created, one whose
// throughput changed is updated in place, one whose projection or key schema
// changed is recreated, and one unchanged is left alone.
func TestDiffGSIs(t *testing.T) {
	gsi := func(name string, read int64) TableGlobalSecondaryIndex {
		return TableGlobalSecondaryIndex{
			Name:          name,
			HashKey:       name + "_hk",
			ReadCapacity:  aws.Int64(read),
			WriteCapacity: aws.Int64(1),
		}
	}
	withProjection := func(g TableGlobalSecondaryIndex, kind string) TableGlobalSecondaryIndex {
		g.ProjectionType = kind
		return g
	}
	withHashKey := func(g TableGlobalSecondaryIndex, key string) TableGlobalSecondaryIndex {
		g.HashKey = key
		return g
	}
	cases := []struct {
		name      string
		prior     []TableGlobalSecondaryIndex
		desired   []TableGlobalSecondaryIndex
		deletes   []string
		updates   []string
		recreates []string
		creates   []string
	}{
		{
			name:    "no change",
			prior:   []TableGlobalSecondaryIndex{gsi("a", 1)},
			desired: []TableGlobalSecondaryIndex{gsi("a", 1)},
		},
		{
			name:    "create only",
			prior:   nil,
			desired: []TableGlobalSecondaryIndex{gsi("a", 1)},
			creates: []string{"a"},
		},
		{
			name:    "delete only",
			prior:   []TableGlobalSecondaryIndex{gsi("a", 1)},
			desired: nil,
			deletes: []string{"a"},
		},
		{
			name:    "throughput change is an update",
			prior:   []TableGlobalSecondaryIndex{gsi("a", 1)},
			desired: []TableGlobalSecondaryIndex{gsi("a", 5)},
			updates: []string{"a"},
		},
		{
			name:      "projection change is a recreate",
			prior:     []TableGlobalSecondaryIndex{withProjection(gsi("a", 1), "ALL")},
			desired:   []TableGlobalSecondaryIndex{withProjection(gsi("a", 1), "KEYS_ONLY")},
			recreates: []string{"a"},
		},
		{
			name:      "hash key change is a recreate",
			prior:     []TableGlobalSecondaryIndex{withHashKey(gsi("a", 1), "old_hk")},
			desired:   []TableGlobalSecondaryIndex{withHashKey(gsi("a", 1), "new_hk")},
			recreates: []string{"a"},
		},
		{
			name:    "create and delete and update together",
			prior:   []TableGlobalSecondaryIndex{gsi("keep", 1), gsi("change", 1), gsi("gone", 1)},
			desired: []TableGlobalSecondaryIndex{gsi("keep", 1), gsi("change", 9), gsi("new", 1)},
			deletes: []string{"gone"},
			updates: []string{"change"},
			creates: []string{"new"},
		},
		{
			name: "recreate joins delete and create together",
			prior: []TableGlobalSecondaryIndex{
				withProjection(gsi("redo", 1), "ALL"), gsi("gone", 1),
			},
			desired: []TableGlobalSecondaryIndex{
				withProjection(gsi("redo", 1), "KEYS_ONLY"), gsi("new", 1),
			},
			deletes:   []string{"gone"},
			recreates: []string{"redo"},
			creates:   []string{"new"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diff := diffGSIs(c.prior, c.desired)
			assert.Equal(t, c.deletes, diff.deletes)
			assert.Equal(t, c.updates, updateNames(diff.updates))
			assert.Equal(t, c.recreates, names(diff.recreates))
			assert.Equal(t, c.creates, names(diff.creates))
		})
	}
}

// names returns the index names of a global-secondary-index list, for comparing
// against the expected sets in a diff test.
func names(in []TableGlobalSecondaryIndex) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, gsi := range in {
		out = append(out, gsi.Name)
	}
	return out
}

// updateNames returns the index names of an in-place update group, for comparing
// against the expected set in a diff test.
func updateNames(in []gsiUpdate) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, u := range in {
		out = append(out, u.current.Name)
	}
	return out
}

// TestGSIThroughputChanged checks that a change to any throughput input of an
// existing index is detected, while an unchanged index is not, and that a
// change to a field that cannot be updated in place is ignored here.
func TestGSIThroughputChanged(t *testing.T) {
	base := TableGlobalSecondaryIndex{
		Name:          "a",
		HashKey:       "hk",
		ReadCapacity:  aws.Int64(1),
		WriteCapacity: aws.Int64(1),
	}
	cases := []struct {
		name    string
		mutate  func(g *TableGlobalSecondaryIndex)
		changed bool
	}{
		{name: "identical", mutate: func(*TableGlobalSecondaryIndex) {}, changed: false},
		{
			name:    "read capacity",
			mutate:  func(g *TableGlobalSecondaryIndex) { g.ReadCapacity = aws.Int64(2) },
			changed: true,
		},
		{
			name:    "write capacity",
			mutate:  func(g *TableGlobalSecondaryIndex) { g.WriteCapacity = aws.Int64(2) },
			changed: true,
		},
		{
			name: "on-demand throughput",
			mutate: func(g *TableGlobalSecondaryIndex) {
				g.OnDemandThroughput = &TableOnDemandThroughput{MaxReadRequestUnits: aws.Int64(5)}
			},
			changed: true,
		},
		{
			name: "warm throughput",
			mutate: func(g *TableGlobalSecondaryIndex) {
				g.WarmThroughput = &TableWarmThroughput{ReadUnitsPerSecond: aws.Int64(5)}
			},
			changed: true,
		},
		{
			name:    "hash key is not an in-place update",
			mutate:  func(g *TableGlobalSecondaryIndex) { g.HashKey = "other" },
			changed: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			current := base
			c.mutate(&current)
			assert.Equal(t, c.changed, gsiThroughputChanged(base, current))
		})
	}
}

// TestGSINeedsRecreate checks the classification of a kept index as a recreate:
// a projection, non-key-attribute, or key-schema change, or a warm-throughput
// decrease, forces a delete-then-create, while a pure throughput change or a
// warm-throughput increase does not.
func TestGSINeedsRecreate(t *testing.T) {
	base := TableGlobalSecondaryIndex{
		Name:             "a",
		HashKey:          "hk",
		RangeKey:         aws.String("rk"),
		ProjectionType:   "INCLUDE",
		NonKeyAttributes: []string{"x", "y"},
		WarmThroughput: &TableWarmThroughput{
			ReadUnitsPerSecond:  aws.Int64(100),
			WriteUnitsPerSecond: aws.Int64(100),
		},
	}
	cases := []struct {
		name     string
		mutate   func(g *TableGlobalSecondaryIndex)
		recreate bool
	}{
		{name: "identical", mutate: func(*TableGlobalSecondaryIndex) {}, recreate: false},
		{
			name:     "projection type",
			mutate:   func(g *TableGlobalSecondaryIndex) { g.ProjectionType = "ALL" },
			recreate: true,
		},
		{
			name:     "non-key attributes",
			mutate:   func(g *TableGlobalSecondaryIndex) { g.NonKeyAttributes = []string{"x", "z"} },
			recreate: true,
		},
		{
			name:     "non-key attribute reorder is not a change",
			mutate:   func(g *TableGlobalSecondaryIndex) { g.NonKeyAttributes = []string{"y", "x"} },
			recreate: false,
		},
		{
			name:     "hash key",
			mutate:   func(g *TableGlobalSecondaryIndex) { g.HashKey = "other" },
			recreate: true,
		},
		{
			name:     "range key",
			mutate:   func(g *TableGlobalSecondaryIndex) { g.RangeKey = aws.String("other") },
			recreate: true,
		},
		{
			name: "warm throughput decrease",
			mutate: func(g *TableGlobalSecondaryIndex) {
				g.WarmThroughput = &TableWarmThroughput{
					ReadUnitsPerSecond:  aws.Int64(50),
					WriteUnitsPerSecond: aws.Int64(100),
				}
			},
			recreate: true,
		},
		{
			name: "warm throughput increase is not a recreate",
			mutate: func(g *TableGlobalSecondaryIndex) {
				g.WarmThroughput = &TableWarmThroughput{
					ReadUnitsPerSecond:  aws.Int64(200),
					WriteUnitsPerSecond: aws.Int64(200),
				}
			},
			recreate: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			current := base
			c.mutate(&current)
			assert.Equal(t, c.recreate, gsiNeedsRecreate(base, current))
		})
	}
}

// TestWarmThroughputDecreased checks that a decrease in either warm-throughput
// rate is a decrease, an increase or no change is not, and a removed or absent
// block is not a decrease since an unset block is never sent.
func TestWarmThroughputDecreased(t *testing.T) {
	warm := func(read, write int64) *TableWarmThroughput {
		return &TableWarmThroughput{
			ReadUnitsPerSecond:  aws.Int64(read),
			WriteUnitsPerSecond: aws.Int64(write),
		}
	}
	cases := []struct {
		name      string
		old       *TableWarmThroughput
		current   *TableWarmThroughput
		decreased bool
	}{
		{name: "both nil", old: nil, current: nil, decreased: false},
		{name: "removed block", old: warm(100, 100), current: nil, decreased: false},
		{name: "added block", old: nil, current: warm(100, 100), decreased: false},
		{name: "equal", old: warm(100, 100), current: warm(100, 100), decreased: false},
		{name: "increase", old: warm(100, 100), current: warm(200, 200), decreased: false},
		{name: "read decrease", old: warm(100, 100), current: warm(50, 100), decreased: true},
		{name: "write decrease", old: warm(100, 100), current: warm(100, 50), decreased: true},
		{
			name:      "read unset on new is not a decrease",
			old:       warm(100, 100),
			current:   &TableWarmThroughput{WriteUnitsPerSecond: aws.Int64(100)},
			decreased: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.decreased, warmThroughputDecreased(c.old, c.current))
		})
	}
}

// TestGSIMainUpdateActionsExcludeWarm checks that the main UpdateTable actions
// for in-place index updates never include warm throughput, send provisioned
// capacity only in provisioned mode, and emit a separate Update action per facet
// so a capacity or on-demand change is never combined with warm throughput.
func TestGSIMainUpdateActionsExcludeWarm(t *testing.T) {
	old := TableGlobalSecondaryIndex{
		Name:          "a",
		HashKey:       "hk",
		ReadCapacity:  aws.Int64(1),
		WriteCapacity: aws.Int64(1),
	}
	current := old
	current.ReadCapacity = aws.Int64(5)
	current.OnDemandThroughput = &TableOnDemandThroughput{MaxReadRequestUnits: aws.Int64(10)}
	current.WarmThroughput = &TableWarmThroughput{ReadUnitsPerSecond: aws.Int64(100)}
	updates := []gsiUpdate{{old: old, current: current}}

	provisioned := gsiMainUpdateActions(updates, true)
	for _, a := range provisioned {
		require.NotNil(t, a.Update)
		assert.Nil(t, a.Update.WarmThroughput, "main update must never include warm throughput")
	}
	assert.Equal(t, []string{"a"}, gsiMainUpdateNames(updates, true))

	onDemandOnly := gsiMainUpdateActions(updates, false)
	for _, a := range onDemandOnly {
		require.NotNil(t, a.Update)
		assert.Nil(t, a.Update.ProvisionedThroughput,
			"on-demand mode must not send provisioned capacity")
		assert.Nil(t, a.Update.WarmThroughput)
	}
}

// TestGSIWarmUpdatesIsolated checks that an index whose warm throughput changed
// is routed to the isolated path, that an index whose only change is warm
// throughput contributes nothing to the main update, and that the isolated
// action sets warm throughput alone.
func TestGSIWarmUpdatesIsolated(t *testing.T) {
	old := TableGlobalSecondaryIndex{Name: "a", HashKey: "hk"}
	current := old
	current.WarmThroughput = &TableWarmThroughput{ReadUnitsPerSecond: aws.Int64(200)}
	updates := []gsiUpdate{{old: old, current: current}}

	assert.Empty(t, gsiMainUpdateActions(updates, true),
		"a warm-only change must not ride the main update")
	assert.Empty(t, gsiMainUpdateNames(updates, true))

	warm := gsiWarmUpdates(updates)
	assert.Equal(t, []string{"a"}, names(warm))

	action := gsiWarmUpdateAction(warm[0])
	require.NotNil(t, action.Update)
	assert.NotNil(t, action.Update.WarmThroughput)
	assert.Nil(t, action.Update.ProvisionedThroughput)
	assert.Nil(t, action.Update.OnDemandThroughput)
}

// TestValidateAttributesIndexed checks the cross-collection rule: every
// attribute must be used by a key, and every key must reference a defined
// attribute, across the table key and every index.
func TestValidateAttributesIndexed(t *testing.T) {
	cases := []struct {
		name       string
		attributes []TableAttribute
		hashKey    string
		rangeKey   *string
		lsis       []TableLocalSecondaryIndex
		gsis       []TableGlobalSecondaryIndex
		wantErr    bool
	}{
		{
			name:       "hash only matches",
			attributes: []TableAttribute{{Name: "id", Type: "S"}},
			hashKey:    "id",
		},
		{
			name:       "hash and range matched",
			attributes: []TableAttribute{{Name: "id", Type: "S"}, {Name: "sk", Type: "N"}},
			hashKey:    "id",
			rangeKey:   aws.String("sk"),
		},
		{
			name:       "unused attribute is rejected",
			attributes: []TableAttribute{{Name: "id", Type: "S"}, {Name: "extra", Type: "S"}},
			hashKey:    "id",
			wantErr:    true,
		},
		{
			name:       "undefined key is rejected",
			attributes: []TableAttribute{{Name: "id", Type: "S"}},
			hashKey:    "id",
			rangeKey:   aws.String("sk"),
			wantErr:    true,
		},
		{
			name: "gsi and lsi keys count as used",
			attributes: []TableAttribute{
				{Name: "id", Type: "S"},
				{Name: "lsiRange", Type: "S"},
				{Name: "gsiHash", Type: "S"},
				{Name: "gsiRange", Type: "S"},
			},
			hashKey: "id",
			lsis:    []TableLocalSecondaryIndex{{Name: "l", RangeKey: "lsiRange"}},
			gsis: []TableGlobalSecondaryIndex{
				{Name: "g", HashKey: "gsiHash", RangeKey: aws.String("gsiRange")},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateAttributesIndexed(
				c.attributes, c.hashKey, c.rangeKey, c.lsis, c.gsis)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestKeySchema checks that a key schema is built in the HASH-then-RANGE order
// DynamoDB requires, with the range element present only when a range key is
// given.
func TestKeySchema(t *testing.T) {
	hashOnly := keySchema("id", nil)
	assert.Equal(t, []dynamodbtypes.KeySchemaElement{
		{AttributeName: aws.String("id"), KeyType: dynamodbtypes.KeyTypeHash},
	}, hashOnly)

	composite := keySchema("id", aws.String("sk"))
	assert.Equal(t, []dynamodbtypes.KeySchemaElement{
		{AttributeName: aws.String("id"), KeyType: dynamodbtypes.KeyTypeHash},
		{AttributeName: aws.String("sk"), KeyType: dynamodbtypes.KeyTypeRange},
	}, composite)
}

// TestStreamSpecification checks that the stream spec is nil when the toggle is
// unset, omits the view type when the stream is disabled, and includes the view
// type when the stream is enabled.
func TestStreamSpecification(t *testing.T) {
	assert.Nil(t, streamSpecification(nil, aws.String("NEW_IMAGE")))

	disabled := streamSpecification(aws.Bool(false), aws.String("NEW_IMAGE"))
	assert.Equal(t, aws.Bool(false), disabled.StreamEnabled)
	assert.Equal(t, dynamodbtypes.StreamViewType(""), disabled.StreamViewType)

	enabled := streamSpecification(aws.Bool(true), aws.String("NEW_AND_OLD_IMAGES"))
	assert.Equal(t, aws.Bool(true), enabled.StreamEnabled)
	assert.Equal(t, dynamodbtypes.StreamViewTypeNewAndOldImages, enabled.StreamViewType)
}

// TestSSESpecification checks that an absent block disables encryption, an
// enabled block without a key selects the managed key, and an enabled block
// with a key sets the KMS type and key.
func TestSSESpecification(t *testing.T) {
	absent := sseSpecification(nil)
	assert.Equal(t, aws.Bool(false), absent.Enabled)
	assert.Nil(t, absent.KMSMasterKeyId)

	managed := sseSpecification(&TableServerSideEncryption{Enabled: aws.Bool(true)})
	assert.Equal(t, aws.Bool(true), managed.Enabled)
	assert.Nil(t, managed.KMSMasterKeyId)
	assert.Equal(t, dynamodbtypes.SSEType(""), managed.SSEType)

	customer := sseSpecification(&TableServerSideEncryption{
		Enabled:  aws.Bool(true),
		KmsKeyId: aws.String("alias/my-key"),
	})
	assert.Equal(t, aws.Bool(true), customer.Enabled)
	assert.Equal(t, aws.String("alias/my-key"), customer.KMSMasterKeyId)
	assert.Equal(t, dynamodbtypes.SSETypeKms, customer.SSEType)
}

// TestTableTags checks that a tag map becomes a key-sorted SDK tag list and an
// empty map yields nil so no tags are sent.
func TestTableTags(t *testing.T) {
	assert.Nil(t, tableTags(nil))
	assert.Nil(t, tableTags(map[string]string{}))

	got := tableTags(map[string]string{"b": "2", "a": "1"})
	assert.Equal(t, []dynamodbtypes.Tag{
		{Key: aws.String("a"), Value: aws.String("1")},
		{Key: aws.String("b"), Value: aws.String("2")},
	}, got)
}
