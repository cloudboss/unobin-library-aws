package autoscaling

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// GroupLaunchTemplate names the launch template an Auto Scaling group launches
// instances from. The template is referenced by id or by name, never both, and
// an optional version pins which version to use. A free-form version string
// passes through unchanged, including the special tokens $Latest and $Default;
// omitting it lets the API use the template's own default version.
type GroupLaunchTemplate struct {
	Id      *string `ub:"id"`
	Name    *string `ub:"name"`
	Version *string `ub:"version"`
}

// GroupTag is one entry in an Auto Scaling group's structured tag set. Unlike
// the flat string map most resources tag with, an ASG tag pairs a key and value
// with a propagate-at-launch flag that copies the tag onto each instance the
// group launches. All three are required.
type GroupTag struct {
	Key               string `ub:"key"`
	Value             string `ub:"value"`
	PropagateAtLaunch bool   `ub:"propagate-at-launch"`
}

// GroupInstanceMaintenancePolicy bounds how much of an Auto Scaling group may be
// replaced at once during instance maintenance, as a min and max percentage of
// desired capacity. Both percentages are part of the policy; the API clears a
// previously set policy when each is sent as -1, the removal sentinel this
// resource uses when the block is omitted on update.
type GroupInstanceMaintenancePolicy struct {
	MinHealthyPercentage int64 `ub:"min-healthy-percentage"`
	MaxHealthyPercentage int64 `ub:"max-healthy-percentage"`
}

// expandLaunchTemplate converts the launch-template block into the SDK
// specification. It sends the id when one is given, otherwise the name, since
// LaunchTemplateSpecification accepts only one of the two. The version is passed
// through verbatim, or left unset to take the template's default.
func expandLaunchTemplate(lt GroupLaunchTemplate) *autoscalingtypes.LaunchTemplateSpecification {
	spec := &autoscalingtypes.LaunchTemplateSpecification{Version: lt.Version}
	if lt.Id != nil {
		spec.LaunchTemplateId = lt.Id
	} else {
		spec.LaunchTemplateName = lt.Name
	}
	return spec
}

// expandTags converts the structured tag list into the SDK tag list, stamping
// every tag with the group's name as ResourceId and the fixed resource type the
// Auto Scaling tag operations require.
func expandTags(name string, tags []GroupTag) []autoscalingtypes.Tag {
	out := make([]autoscalingtypes.Tag, 0, len(tags))
	for _, t := range tags {
		out = append(out, tagToSDK(name, t))
	}
	return out
}

// tagToSDK builds the SDK tag for one structured tag entry, including the
// ResourceId and ResourceType every Auto Scaling tag call requires.
func tagToSDK(name string, t GroupTag) autoscalingtypes.Tag {
	return autoscalingtypes.Tag{
		Key:               aws.String(t.Key),
		Value:             aws.String(t.Value),
		PropagateAtLaunch: aws.Bool(t.PropagateAtLaunch),
		ResourceId:        aws.String(name),
		ResourceType:      aws.String(tagResourceType),
	}
}

// expandMaintenancePolicy converts the instance-maintenance-policy block into
// the SDK policy.
func expandMaintenancePolicy(
	p GroupInstanceMaintenancePolicy,
) *autoscalingtypes.InstanceMaintenancePolicy {
	return &autoscalingtypes.InstanceMaintenancePolicy{
		MinHealthyPercentage: ptr.Int32(aws.Int64(p.MinHealthyPercentage)),
		MaxHealthyPercentage: ptr.Int32(aws.Int64(p.MaxHealthyPercentage)),
	}
}

// removedMaintenancePolicy is the policy that clears a previously set
// instance-maintenance-policy at the API, sent when the block is omitted on
// update. The API treats min and max healthy percentages of -1 as a removal.
func removedMaintenancePolicy() *autoscalingtypes.InstanceMaintenancePolicy {
	return &autoscalingtypes.InstanceMaintenancePolicy{
		MinHealthyPercentage: aws.Int32(-1),
		MaxHealthyPercentage: aws.Int32(-1),
	}
}

// tagKey identifies a structured tag for set reconciliation. propagate-at-launch
// is part of the identity so a change to it re-creates the tag, the same as a
// value change.
type tagKey struct {
	key               string
	value             string
	propagateAtLaunch bool
}

// keyOf returns the comparison key for a structured tag.
func keyOf(t GroupTag) tagKey {
	return tagKey{key: t.Key, value: t.Value, propagateAtLaunch: t.PropagateAtLaunch}
}

// diffTags compares the prior and desired tag sets and returns the tags to
// remove and the tags to create or update. A tag whose value or propagate flag
// changed appears in both: its old form is removed by key and its new form is
// written. Removal keys only on the tag key, since DeleteTags matches by key.
func diffTags(prior, desired []GroupTag) (remove, upsert []GroupTag) {
	desiredByKey := make(map[string]GroupTag, len(desired))
	for _, t := range desired {
		desiredByKey[t.Key] = t
	}
	priorByKey := make(map[string]GroupTag, len(prior))
	for _, t := range prior {
		priorByKey[t.Key] = t
	}
	for _, t := range prior {
		if d, ok := desiredByKey[t.Key]; !ok || keyOf(d) != keyOf(t) {
			remove = append(remove, t)
		}
	}
	for _, t := range desired {
		if p, ok := priorByKey[t.Key]; !ok || keyOf(p) != keyOf(t) {
			upsert = append(upsert, t)
		}
	}
	return remove, upsert
}

// stringSetDiff compares two string sets and returns the members added in
// desired and the members removed from prior, ignoring order and duplicates. It
// reconciles the suspended-process, metric, and target-group sets.
func stringSetDiff(prior, desired []string) (added, removed []string) {
	priorSet := make(map[string]struct{}, len(prior))
	for _, s := range prior {
		priorSet[s] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desired))
	for _, s := range desired {
		desiredSet[s] = struct{}{}
	}
	for s := range desiredSet {
		if _, ok := priorSet[s]; !ok {
			added = append(added, s)
		}
	}
	for s := range priorSet {
		if _, ok := desiredSet[s]; !ok {
			removed = append(removed, s)
		}
	}
	return added, removed
}

// batches splits items into consecutive slices of at most size, so a call that
// caps its list length is made over several requests.
func batches[T any](items []T, size int) [][]T {
	if len(items) == 0 {
		return nil
	}
	var out [][]T
	for start := 0; start < len(items); start += size {
		out = append(out, items[start:min(start+size, len(items))])
	}
	return out
}
