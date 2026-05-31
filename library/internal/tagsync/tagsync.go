// Package tagsync holds the service-independent half of tag reconciliation.
// Diff computes the upsert and removal sets for a desired tag map against the
// tags currently on a resource; Sync drives the read-diff-delete-write
// sequence given a service's own SDK calls as closures. The SDK calls stay
// per service -- every service names its tag verbs and tag struct differently
// -- but the comparison and ordering live here once.
package tagsync

import (
	"context"
	"slices"
	"strings"
)

// reservedPrefix marks tags AWS attaches itself (e.g. aws:cloudformation:...).
// They are never removed: they are not user-managed and would read as drift.
const reservedPrefix = "aws:"

// Diff compares the tags currently on a resource against the desired set.
// upsert holds the desired entries that are absent or hold a different value
// and must be written; remove holds the keys present on the resource but no
// longer desired, sorted for deterministic calls. Keys with the reserved
// "aws:" prefix are never removed.
func Diff(current, desired map[string]string) (upsert map[string]string, remove []string) {
	upsert = make(map[string]string)
	for k, v := range desired {
		if cur, ok := current[k]; !ok || cur != v {
			upsert[k] = v
		}
	}
	for k := range current {
		if strings.HasPrefix(k, reservedPrefix) {
			continue
		}
		if _, ok := desired[k]; !ok {
			remove = append(remove, k)
		}
	}
	slices.Sort(remove)
	return upsert, remove
}

// Sync reconciles a resource's tags with desired. It reads the current tags,
// diffs them, removes the keys no longer wanted, then writes the new or
// changed ones. read, put, and remove are the service's own SDK calls: read
// returns the live tags as a map, put writes an upsert set, remove deletes a
// list of keys. A step is skipped when its set is empty. Removals run before
// writes so a service with a per-resource tag-count limit frees slots first.
func Sync(
	ctx context.Context,
	desired map[string]string,
	read func(context.Context) (map[string]string, error),
	put func(context.Context, map[string]string) error,
	remove func(context.Context, []string) error,
) error {
	current, err := read(ctx)
	if err != nil {
		return err
	}
	upsert, removals := Diff(current, desired)
	if len(removals) > 0 {
		if err := remove(ctx, removals); err != nil {
			return err
		}
	}
	if len(upsert) > 0 {
		if err := put(ctx, upsert); err != nil {
			return err
		}
	}
	return nil
}
