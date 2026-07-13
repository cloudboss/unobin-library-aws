package efs

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

func expandLifecyclePolicies(
	policies []FileSystemLifecyclePolicy,
) []efstypes.LifecyclePolicy {
	if len(policies) == 0 {
		return []efstypes.LifecyclePolicy{}
	}
	out := make([]efstypes.LifecyclePolicy, 0, len(policies))
	for _, policy := range policies {
		expanded := efstypes.LifecyclePolicy{}
		if policy.TransitionToIA != nil {
			expanded.TransitionToIA = efstypes.TransitionToIARules(*policy.TransitionToIA)
		}
		if policy.TransitionToArchive != nil {
			expanded.TransitionToArchive = efstypes.TransitionToArchiveRules(
				*policy.TransitionToArchive)
		}
		if policy.TransitionToPrimaryStorageClass != nil {
			expanded.TransitionToPrimaryStorageClass =
				efstypes.TransitionToPrimaryStorageClassRules(
					*policy.TransitionToPrimaryStorageClass)
		}
		out = append(out, expanded)
	}
	return out
}

func flattenLifecyclePolicies(
	policies []efstypes.LifecyclePolicy,
) []FileSystemLifecyclePolicy {
	if len(policies) == 0 {
		return []FileSystemLifecyclePolicy{}
	}
	out := make([]FileSystemLifecyclePolicy, 0, len(policies))
	for _, policy := range policies {
		flattened := FileSystemLifecyclePolicy{}
		if policy.TransitionToIA != "" {
			flattened.TransitionToIA = aws.String(string(policy.TransitionToIA))
		}
		if policy.TransitionToArchive != "" {
			flattened.TransitionToArchive = aws.String(string(policy.TransitionToArchive))
		}
		if policy.TransitionToPrimaryStorageClass != "" {
			flattened.TransitionToPrimaryStorageClass = aws.String(
				string(policy.TransitionToPrimaryStorageClass))
		}
		out = append(out, flattened)
	}
	return out
}

func expandReplicationDestination(
	destination FileSystemReplicationDestination,
) efstypes.DestinationToCreate {
	return efstypes.DestinationToCreate{
		Region:               destination.Region,
		AvailabilityZoneName: destination.AvailabilityZoneName,
		KmsKeyId:             destination.KmsKeyId,
		FileSystemId:         destination.FileSystemId,
		RoleArn:              destination.RoleArn,
	}
}

func flattenReplication(
	replications []efstypes.ReplicationConfigurationDescription,
) *FileSystemReplicationOutput {
	if len(replications) == 0 || len(replications[0].Destinations) == 0 {
		return nil
	}
	destinations := make([]FileSystemReplicationDestinationOutput, 0,
		len(replications[0].Destinations))
	for _, destination := range replications[0].Destinations {
		destinations = append(destinations, FileSystemReplicationDestinationOutput{
			FileSystemId:  aws.ToString(destination.FileSystemId),
			Region:        aws.ToString(destination.Region),
			OwnerId:       aws.ToString(destination.OwnerId),
			RoleArn:       aws.ToString(destination.RoleArn),
			Status:        string(destination.Status),
			StatusMessage: aws.ToString(destination.StatusMessage),
		})
	}
	return &FileSystemReplicationOutput{Destinations: destinations}
}

func fileSystemTags(tags map[string]string) []efstypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]efstypes.Tag, 0, len(tags))
	for _, key := range slices.Sorted(maps.Keys(tags)) {
		out = append(out, efstypes.Tag{
			Key:   aws.String(key),
			Value: aws.String(tags[key]),
		})
	}
	return out
}

func fileSystemTagMap(tags []efstypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return out
}

func userFileSystemTags(tags map[string]string) map[string]string {
	out := make(map[string]string)
	for key, value := range tags {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = value
	}
	return out
}

func fileSystemSizeOutput(size *efstypes.FileSystemSize) FileSystemSizeOutput {
	if size == nil {
		return FileSystemSizeOutput{}
	}
	timestamp := ""
	if size.Timestamp != nil {
		timestamp = size.Timestamp.UTC().Format(time.RFC3339)
	}
	return FileSystemSizeOutput{
		Value:           size.Value,
		Timestamp:       timestamp,
		ValueInArchive:  ptr.Value(size.ValueInArchive),
		ValueInIA:       ptr.Value(size.ValueInIA),
		ValueInStandard: ptr.Value(size.ValueInStandard),
	}
}

func fileSystemProtectionOutput(
	description *efstypes.FileSystemProtectionDescription,
) *FileSystemProtection {
	if description == nil || description.ReplicationOverwriteProtection == "" {
		return nil
	}
	return &FileSystemProtection{
		ReplicationOverwriteProtection: string(
			description.ReplicationOverwriteProtection),
	}
}

func backupPolicyOutput(policy *efstypes.BackupPolicy) *FileSystemBackupPolicy {
	if policy == nil || policy.Status == "" {
		return nil
	}
	return &FileSystemBackupPolicy{Status: string(policy.Status)}
}
