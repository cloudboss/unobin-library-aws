package tagsync

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiff(t *testing.T) {
	tests := []struct {
		name       string
		current    map[string]string
		desired    map[string]string
		wantUpsert map[string]string
		wantRemove []string
	}{
		{
			name:       "both empty",
			current:    map[string]string{},
			desired:    map[string]string{},
			wantUpsert: map[string]string{},
			wantRemove: nil,
		},
		{
			name:       "add new",
			current:    map[string]string{},
			desired:    map[string]string{"env": "prod"},
			wantUpsert: map[string]string{"env": "prod"},
			wantRemove: nil,
		},
		{
			name:       "unchanged",
			current:    map[string]string{"env": "prod"},
			desired:    map[string]string{"env": "prod"},
			wantUpsert: map[string]string{},
			wantRemove: nil,
		},
		{
			name:       "changed value",
			current:    map[string]string{"env": "dev"},
			desired:    map[string]string{"env": "prod"},
			wantUpsert: map[string]string{"env": "prod"},
			wantRemove: nil,
		},
		{
			name:       "removed",
			current:    map[string]string{"env": "prod", "team": "core"},
			desired:    map[string]string{"env": "prod"},
			wantUpsert: map[string]string{},
			wantRemove: []string{"team"},
		},
		{
			name:       "reserved kept",
			current:    map[string]string{"aws:cloudformation:stack-name": "s"},
			desired:    map[string]string{},
			wantUpsert: map[string]string{},
			wantRemove: nil,
		},
		{
			name:       "mixed",
			current:    map[string]string{"keep": "1", "drop": "2", "change": "old", "aws:x": "y"},
			desired:    map[string]string{"keep": "1", "change": "new", "add": "3"},
			wantUpsert: map[string]string{"change": "new", "add": "3"},
			wantRemove: []string{"drop"},
		},
		{
			name:       "removals sorted",
			current:    map[string]string{"c": "1", "a": "2", "b": "3"},
			desired:    map[string]string{},
			wantUpsert: map[string]string{},
			wantRemove: []string{"a", "b", "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upsert, remove := Diff(tt.current, tt.desired)
			assert.Equal(t, tt.wantUpsert, upsert)
			assert.Equal(t, tt.wantRemove, remove)
		})
	}
}

func TestSyncAppliesRemovalsThenWrites(t *testing.T) {
	var order []string
	var gotPut map[string]string
	var gotRemove []string
	err := Sync(context.Background(),
		map[string]string{"env": "prod", "add": "x"},
		func(context.Context) (map[string]string, error) {
			return map[string]string{"env": "prod", "drop": "y"}, nil
		},
		func(_ context.Context, up map[string]string) error {
			order = append(order, "put")
			gotPut = up
			return nil
		},
		func(_ context.Context, rm []string) error {
			order = append(order, "remove")
			gotRemove = rm
			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"remove", "put"}, order)
	assert.Equal(t, map[string]string{"add": "x"}, gotPut)
	assert.Equal(t, []string{"drop"}, gotRemove)
}

func TestSyncSkipsEmptySets(t *testing.T) {
	called := false
	err := Sync(context.Background(),
		map[string]string{"env": "prod"},
		func(context.Context) (map[string]string, error) {
			return map[string]string{"env": "prod"}, nil
		},
		func(context.Context, map[string]string) error { called = true; return nil },
		func(context.Context, []string) error { called = true; return nil },
	)
	require.NoError(t, err)
	assert.False(t, called)
}

func TestSyncReadError(t *testing.T) {
	sentinel := errors.New("boom")
	err := Sync(context.Background(),
		map[string]string{"env": "prod"},
		func(context.Context) (map[string]string, error) { return nil, sentinel },
		func(context.Context, map[string]string) error { return nil },
		func(context.Context, []string) error { return nil },
	)
	assert.ErrorIs(t, err, sentinel)
}

func TestSyncRemoveErrorStopsBeforePut(t *testing.T) {
	sentinel := errors.New("remove failed")
	putCalled := false
	err := Sync(context.Background(),
		map[string]string{"add": "x"},
		func(context.Context) (map[string]string, error) {
			return map[string]string{"drop": "y"}, nil
		},
		func(context.Context, map[string]string) error { putCalled = true; return nil },
		func(context.Context, []string) error { return sentinel },
	)
	assert.ErrorIs(t, err, sentinel)
	assert.False(t, putCalled)
}
