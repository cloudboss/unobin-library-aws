package secretsmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	secretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// expandReplicas converts the configured replica blocks into the SDK replica
// list passed to a create or a replicate call. A nil result is returned for an
// empty set so the request omits the field rather than sending an empty list.
func expandReplicas(replicas []SecretReplica) []secretsmanagertypes.ReplicaRegionType {
	if len(replicas) == 0 {
		return nil
	}
	out := make([]secretsmanagertypes.ReplicaRegionType, 0, len(replicas))
	for _, rep := range replicas {
		out = append(out, secretsmanagertypes.ReplicaRegionType{
			Region:   aws.String(rep.Region),
			KmsKeyId: rep.KmsKeyId,
		})
	}
	return out
}

// flattenReplicaStatus converts the replication status the secret reports into
// the output blocks, one per Region. The last-accessed date is rendered as
// RFC3339, or left empty when the replica has never been retrieved in that
// Region.
func flattenReplicaStatus(
	status []secretsmanagertypes.ReplicationStatusType,
) []SecretReplicaStatus {
	if len(status) == 0 {
		return nil
	}
	out := make([]SecretReplicaStatus, 0, len(status))
	for _, s := range status {
		lastAccessed := ""
		if s.LastAccessedDate != nil {
			lastAccessed = s.LastAccessedDate.Format(time.RFC3339)
		}
		out = append(out, SecretReplicaStatus{
			Region:           aws.ToString(s.Region),
			Status:           string(s.Status),
			StatusMessage:    aws.ToString(s.StatusMessage),
			LastAccessedDate: lastAccessed,
		})
	}
	return out
}

// replicaStatusRegions returns the Regions named in a set of replica status
// blocks, used at delete time to remove every replica the secret still has.
func replicaStatusRegions(status []SecretReplicaStatus) []string {
	regions := make([]string, 0, len(status))
	for _, s := range status {
		regions = append(regions, s.Region)
	}
	return regions
}

// reconcileReplicas brings the secret's replica Regions to the desired set by
// Region. The Regions present before but no longer desired are removed first,
// since a Region cannot be removed and re-added in one pass; then the Regions
// newly desired are replicated. A Region kept across the change is left alone.
func (r *SecretResource) reconcileReplicas(
	ctx context.Context, client *secretsmanager.Client, arn string, prior *[]SecretReplica,
) error {
	priorList := ptr.Value(prior)
	desiredList := ptr.Value(r.Replica)
	priorRegions := replicaRegionSet(priorList)
	desiredRegions := replicaRegionSet(desiredList)
	var remove []string
	for _, rep := range priorList {
		if _, ok := desiredRegions[rep.Region]; !ok {
			remove = append(remove, rep.Region)
		}
	}
	if len(remove) > 0 {
		if err := r.removeReplicaRegions(ctx, client, arn, remove); err != nil {
			return err
		}
	}
	var add []SecretReplica
	for _, rep := range desiredList {
		if _, ok := priorRegions[rep.Region]; !ok {
			add = append(add, rep)
		}
	}
	if len(add) > 0 {
		_, err := client.ReplicateSecretToRegions(ctx,
			&secretsmanager.ReplicateSecretToRegionsInput{
				SecretId:                    aws.String(arn),
				AddReplicaRegions:           expandReplicas(add),
				ForceOverwriteReplicaSecret: aws.ToBool(r.ForceOverwriteReplicaSecret),
			})
		if err != nil {
			return fmt.Errorf("replicate secret to regions: %w", err)
		}
	}
	return nil
}

// removeReplicaRegions takes the given Regions out of the secret's replication.
// A secret already gone is tolerated, so a delete that races a concurrent
// removal does not fail.
func (r *SecretResource) removeReplicaRegions(
	ctx context.Context, client *secretsmanager.Client, arn string, regions []string,
) error {
	if len(regions) == 0 {
		return nil
	}
	_, err := client.RemoveRegionsFromReplication(ctx,
		&secretsmanager.RemoveRegionsFromReplicationInput{
			SecretId:             aws.String(arn),
			RemoveReplicaRegions: regions,
		})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("remove regions from replication: %w", err)
	}
	return nil
}

// replicaRegionSet indexes a replica list by Region for membership tests.
func replicaRegionSet(replicas []SecretReplica) map[string]struct{} {
	set := make(map[string]struct{}, len(replicas))
	for _, rep := range replicas {
		set[rep.Region] = struct{}{}
	}
	return set
}
