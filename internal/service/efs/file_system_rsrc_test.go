package efs

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	efssdk "github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testFileSystemID  = "fs-0123456789abcdef0"
	testFileSystemARN = "arn:aws:elasticfilesystem:us-east-1:123456789012:" +
		"file-system/fs-0123456789abcdef0"
)

func TestFileSystemMetadata(t *testing.T) {
	r := &FileSystemResource{}
	assert.Equal(t, 1, r.SchemaVersion())
	assert.Equal(t, []string{
		"availability-zone-name",
		"encrypted",
		"kms-key-id",
		"performance-mode",
	}, r.ReplaceFields())
}

func TestFileSystemValidateInputs(t *testing.T) {
	validPolicy := `{"Version":"2012-10-17","Statement":[]}`
	valid := FileSystemResource{
		Encrypted:        aws.Bool(true),
		KmsKeyId:         aws.String("alias/efs"),
		PerformanceMode:  aws.String("generalPurpose"),
		ThroughputMode:   aws.String("elastic"),
		FileSystemPolicy: &validPolicy,
		LifecyclePolicies: &[]FileSystemLifecyclePolicy{
			{TransitionToIA: aws.String("AFTER_7_DAYS")},
			{TransitionToArchive: aws.String("AFTER_14_DAYS")},
			{TransitionToPrimaryStorageClass: aws.String("AFTER_1_ACCESS")},
		},
		BackupPolicy: &FileSystemBackupPolicy{Status: "ENABLED"},
		FileSystemProtection: &FileSystemProtection{
			ReplicationOverwriteProtection: "DISABLED",
		},
		ReplicationConfiguration: &FileSystemReplicationConfiguration{
			Destinations: []FileSystemReplicationDestination{{
				Region: aws.String("us-west-2"),
			}},
		},
		Tags: &map[string]string{"Name": "unobin-efs"},
	}
	tests := []struct {
		name    string
		mutate  func(*FileSystemResource)
		wantErr string
	}{
		{name: "valid"},
		{
			name: "KMS key needs encryption",
			mutate: func(r *FileSystemResource) {
				r.Encrypted = aws.Bool(false)
			},
			wantErr: "encrypted",
		},
		{
			name: "provisioned mode needs throughput",
			mutate: func(r *FileSystemResource) {
				r.ThroughputMode = aws.String("provisioned")
				r.ProvisionedThroughputInMibps = nil
				r.LifecyclePolicies = nil
			},
			wantErr: "provisioned-throughput",
		},
		{
			name: "throughput value needs provisioned mode",
			mutate: func(r *FileSystemResource) {
				r.ProvisionedThroughputInMibps = aws.Float64(1)
			},
			wantErr: "throughput-mode",
		},
		{
			name: "throughput lower bound",
			mutate: func(r *FileSystemResource) {
				r.ThroughputMode = aws.String("provisioned")
				r.ProvisionedThroughputInMibps = aws.Float64(0.5)
				r.LifecyclePolicies = nil
			},
			wantErr: "at least 1",
		},
		{
			name: "max IO conflicts with one zone",
			mutate: func(r *FileSystemResource) {
				r.AvailabilityZoneName = aws.String("us-east-1a")
				r.PerformanceMode = aws.String("maxIO")
				r.ThroughputMode = aws.String("bursting")
				r.LifecyclePolicies = nil
			},
			wantErr: "maxIO",
		},
		{
			name: "max IO conflicts with elastic",
			mutate: func(r *FileSystemResource) {
				r.PerformanceMode = aws.String("maxIO")
				r.LifecyclePolicies = nil
			},
			wantErr: "maxIO",
		},
		{
			name: "too many lifecycle policies",
			mutate: func(r *FileSystemResource) {
				policies := append([]FileSystemLifecyclePolicy{}, (*r.LifecyclePolicies)...)
				policies = append(policies,
					FileSystemLifecyclePolicy{TransitionToIA: aws.String("AFTER_30_DAYS")})
				r.LifecyclePolicies = &policies
			},
			wantErr: "at most 3",
		},
		{
			name: "lifecycle object has one transition",
			mutate: func(r *FileSystemResource) {
				(*r.LifecyclePolicies)[0].TransitionToArchive = aws.String("AFTER_30_DAYS")
			},
			wantErr: "exactly one transition",
		},
		{
			name: "duplicate lifecycle transition",
			mutate: func(r *FileSystemResource) {
				*r.LifecyclePolicies = []FileSystemLifecyclePolicy{
					{TransitionToIA: aws.String("AFTER_7_DAYS")},
					{TransitionToIA: aws.String("AFTER_14_DAYS")},
				}
			},
			wantErr: "one policy per transition",
		},
		{
			name: "archive follows IA",
			mutate: func(r *FileSystemResource) {
				*r.LifecyclePolicies = []FileSystemLifecyclePolicy{
					{TransitionToIA: aws.String("AFTER_30_DAYS")},
					{TransitionToArchive: aws.String("AFTER_14_DAYS")},
				}
			},
			wantErr: "later than",
		},
		{
			name: "archive uses default general purpose performance",
			mutate: func(r *FileSystemResource) {
				r.PerformanceMode = nil
			},
		},
		{
			name: "backup enum",
			mutate: func(r *FileSystemResource) {
				r.BackupPolicy.Status = "ENABLING"
			},
			wantErr: "ENABLED or DISABLED",
		},
		{
			name: "protection enum",
			mutate: func(r *FileSystemResource) {
				r.FileSystemProtection.ReplicationOverwriteProtection = "REPLICATING"
			},
			wantErr: "ENABLED or DISABLED",
		},
		{
			name: "policy length",
			mutate: func(r *FileSystemResource) {
				policy := strings.Repeat("x", 20001)
				r.FileSystemPolicy = &policy
			},
			wantErr: "20,000",
		},
		{
			name: "policy length counts characters",
			mutate: func(r *FileSystemResource) {
				policy := `"` + strings.Repeat("é", 10000) + `"`
				r.FileSystemPolicy = &policy
			},
		},
		{
			name: "one replication destination",
			mutate: func(r *FileSystemResource) {
				r.ReplicationConfiguration.Destinations = nil
			},
			wantErr: "exactly one destination",
		},
		{
			name: "replication destination location",
			mutate: func(r *FileSystemResource) {
				r.ReplicationConfiguration.Destinations[0].Region = nil
			},
			wantErr: "region or availability-zone-name",
		},
		{
			name: "same account role allows destination ID",
			mutate: func(r *FileSystemResource) {
				d := &r.ReplicationConfiguration.Destinations[0]
				d.FileSystemId = aws.String("fs-12345678")
				d.RoleArn = aws.String("arn:aws:iam::123456789012:role/efs")
			},
		},
		{
			name: "reserved tag",
			mutate: func(r *FileSystemResource) {
				r.Tags = &map[string]string{"aws:owner": "system"}
			},
			wantErr: "aws:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := cloneFileSystemResource(valid)
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			err := r.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestFileSystemKMSKeyIDServicePattern(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{
			name:  "key ID",
			value: "01234567-89ab-cdef-0123-456789abcdef",
			valid: true,
		},
		{
			name:  "multi region key ID",
			value: "mrk-0123456789abcdef0123456789abcdef",
			valid: true,
		},
		{name: "alias", value: "alias/Team_1", valid: true},
		{
			name: "key ARN",
			value: "arn:aws-us-gov:kms:us-gov-west-1:123456789012:" +
				"key/01234567-89ab-cdef-0123-456789abcdef",
			valid: true,
		},
		{
			name:  "alias ARN",
			value: "arn:aws:kms:us-east-1:123456789012:alias/Team_1",
			valid: true,
		},
		{
			name:  "uppercase key ID",
			value: "01234567-89AB-CDEF-0123-456789ABCDEF",
		},
		{
			name:  "malformed key ARN resource",
			value: "arn:aws:kms:us-east-1:123456789012:key/not-a-key",
		},
		{
			name:  "invalid alias ARN character",
			value: "arn:aws:kms:us-east-1:123456789012:alias/team!",
		},
		{name: "overlong alias", value: "alias/" + strings.Repeat("a", 2043)},
	}
	for _, tt := range tests {
		for _, target := range []string{"source", "destination"} {
			t.Run(tt.name+" "+target, func(t *testing.T) {
				r := FileSystemResource{}
				if target == "source" {
					r.Encrypted = aws.Bool(true)
					r.KmsKeyId = aws.String(tt.value)
				} else {
					r.ReplicationConfiguration = &FileSystemReplicationConfiguration{
						Destinations: []FileSystemReplicationDestination{{
							Region:   aws.String("us-west-2"),
							KmsKeyId: aws.String(tt.value),
						}},
					}
				}
				err := r.ValidateInputs(context.Background(), nil)
				if tt.valid {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
				assert.Contains(t, err.Error(), "kms-key-id")
			})
		}
	}
}

func TestFileSystemServiceModelBoundaries(t *testing.T) {
	replication := func(destination FileSystemReplicationDestination) FileSystemResource {
		return FileSystemResource{
			ReplicationConfiguration: &FileSystemReplicationConfiguration{
				Destinations: []FileSystemReplicationDestination{destination},
			},
		}
	}
	tests := []struct {
		name    string
		input   FileSystemResource
		wantErr string
	}{
		{
			name:  "source availability zone maximum",
			input: FileSystemResource{AvailabilityZoneName: aws.String(strings.Repeat("a", 64))},
		},
		{
			name:    "source availability zone empty",
			input:   FileSystemResource{AvailabilityZoneName: aws.String("")},
			wantErr: "availability-zone-name",
		},
		{
			name: "source availability zone too long",
			input: FileSystemResource{
				AvailabilityZoneName: aws.String(strings.Repeat("a", 65)),
			},
			wantErr: "availability-zone-name",
		},
		{
			name: "destination availability zone maximum",
			input: replication(FileSystemReplicationDestination{
				AvailabilityZoneName: aws.String(strings.Repeat("a", 64)),
			}),
		},
		{
			name: "destination availability zone empty",
			input: replication(FileSystemReplicationDestination{
				AvailabilityZoneName: aws.String(""),
			}),
			wantErr: "availability-zone-name",
		},
		{
			name: "destination availability zone too long",
			input: replication(FileSystemReplicationDestination{
				AvailabilityZoneName: aws.String(strings.Repeat("a", 65)),
			}),
			wantErr: "availability-zone-name",
		},
		{
			name: "regional partition name",
			input: replication(FileSystemReplicationDestination{
				Region: aws.String("us-isob-east-1"),
			}),
		},
		{
			name: "region maximum",
			input: replication(FileSystemReplicationDestination{
				Region: aws.String("us-" + strings.Repeat("a", 60) + "1"),
			}),
		},
		{
			name: "region too long",
			input: replication(FileSystemReplicationDestination{
				Region: aws.String("us-" + strings.Repeat("a", 61) + "1"),
			}),
			wantErr: "region",
		},
		{
			name: "invalid region",
			input: replication(FileSystemReplicationDestination{
				Region: aws.String("US-EAST-1"),
			}),
			wantErr: "region",
		},
		{
			name: "file system ID",
			input: replication(FileSystemReplicationDestination{
				Region:       aws.String("us-west-2"),
				FileSystemId: aws.String("fs-01234567"),
			}),
		},
		{
			name: "file system ID maximum",
			input: replication(FileSystemReplicationDestination{
				Region:       aws.String("us-west-2"),
				FileSystemId: aws.String("fs-" + strings.Repeat("a", 40)),
			}),
		},
		{
			name: "file system ID too long",
			input: replication(FileSystemReplicationDestination{
				Region:       aws.String("us-west-2"),
				FileSystemId: aws.String("fs-" + strings.Repeat("a", 41)),
			}),
			wantErr: "file-system-id",
		},
		{
			name: "file system ARN partition",
			input: replication(FileSystemReplicationDestination{
				Region: aws.String("cn-north-1"),
				FileSystemId: aws.String(
					"arn:aws-cn:elasticfilesystem:cn-north-1:123456789012:" +
						"file-system/fs-01234567"),
			}),
		},
		{
			name: "invalid file system ID",
			input: replication(FileSystemReplicationDestination{
				Region:       aws.String("us-west-2"),
				FileSystemId: aws.String("fs-0123456G"),
			}),
			wantErr: "file-system-id",
		},
		{
			name: "valid Unicode tags",
			input: FileSystemResource{Tags: &map[string]string{
				"部署\u2003１": "東京 + 1",
				"empty":     "",
			}},
		},
		{
			name: "tag lengths at maximum",
			input: FileSystemResource{Tags: &map[string]string{
				strings.Repeat("é", 128): strings.Repeat("界", 256),
			}},
		},
		{
			name: "tag key too long",
			input: FileSystemResource{Tags: &map[string]string{
				strings.Repeat("é", 129): "value",
			}},
			wantErr: "1 to 128",
		},
		{
			name: "tag value too long",
			input: FileSystemResource{Tags: &map[string]string{
				"key": strings.Repeat("界", 257),
			}},
			wantErr: "at most 256",
		},
		{
			name: "case insensitive reserved tag",
			input: FileSystemResource{Tags: &map[string]string{
				"AWS:owner": "system",
			}},
			wantErr: "aws:",
		},
		{
			name: "invalid tag key character",
			input: FileSystemResource{Tags: &map[string]string{
				"team!": "core",
			}},
			wantErr: "allowed characters",
		},
		{
			name: "invalid tag value character",
			input: FileSystemResource{Tags: &map[string]string{
				"team": "core\tplatform",
			}},
			wantErr: "allowed characters",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), tt.wantErr)
		})
	}
}

func TestFileSystemRejectsReservedTagPrefixCaseVariants(t *testing.T) {
	for _, prefix := range []string{
		"aws:", "awS:", "aWs:", "aWS:",
		"Aws:", "AwS:", "AWs:", "AWS:",
	} {
		t.Run(prefix, func(t *testing.T) {
			tags := map[string]string{prefix + "owner": "system"}
			err := (&FileSystemResource{Tags: &tags}).ValidateInputs(
				context.Background(), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "aws:")
		})
	}
}

func TestFileSystemCreateOmitsCloudDefaults(t *testing.T) {
	r := &FileSystemResource{}
	client := newFakeFileSystemClient("us-east-1")
	clients := fakeFileSystemClients{
		region: "us-east-1",
		source: client,
	}

	out, err := r.create(context.Background(), &clients)

	require.NoError(t, err)
	require.NotNil(t, out)
	require.Len(t, client.createInputs, 1)
	in := client.createInputs[0]
	require.NotNil(t, in.CreationToken)
	assert.Len(t, *in.CreationToken, 64)
	assert.Nil(t, in.AvailabilityZoneName)
	assert.Nil(t, in.Encrypted)
	assert.Nil(t, in.KmsKeyId)
	assert.Empty(t, in.PerformanceMode)
	assert.Empty(t, in.ThroughputMode)
	assert.Nil(t, in.ProvisionedThroughputInMibps)
	assert.Nil(t, in.Tags)
	assert.Equal(t, testFileSystemID, out.FileSystemId)
}

func TestFileSystemCreateForwardsProvisionedThroughput(t *testing.T) {
	r := &FileSystemResource{
		ThroughputMode:               aws.String("provisioned"),
		ProvisionedThroughputInMibps: aws.Float64(4.5),
	}
	client := newFakeFileSystemClient("us-east-1")
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	_, err := r.create(context.Background(), &clients)

	require.NoError(t, err)
	require.Len(t, client.createInputs, 1)
	assert.Equal(t, efstypes.ThroughputModeProvisioned,
		client.createInputs[0].ThroughputMode)
	assert.Equal(t, 4.5,
		aws.ToFloat64(client.createInputs[0].ProvisionedThroughputInMibps))
}

func TestFileSystemCreateAppliesFollowOnConfiguration(t *testing.T) {
	setFastFileSystemTimings(t)
	policy := `{"Version":"2012-10-17","Statement":[]}`
	r := &FileSystemResource{
		Encrypted:       aws.Bool(true),
		KmsKeyId:        aws.String("alias/efs"),
		PerformanceMode: aws.String("generalPurpose"),
		ThroughputMode:  aws.String("elastic"),
		LifecyclePolicies: &[]FileSystemLifecyclePolicy{
			{TransitionToIA: aws.String("AFTER_7_DAYS")},
			{TransitionToArchive: aws.String("AFTER_14_DAYS")},
			{TransitionToPrimaryStorageClass: aws.String("AFTER_1_ACCESS")},
		},
		BackupPolicy:                   &FileSystemBackupPolicy{Status: "ENABLED"},
		FileSystemPolicy:               &policy,
		BypassPolicyLockoutSafetyCheck: aws.Bool(true),
		FileSystemProtection: &FileSystemProtection{
			ReplicationOverwriteProtection: "DISABLED",
		},
		ReplicationConfiguration: &FileSystemReplicationConfiguration{
			Destinations: []FileSystemReplicationDestination{{
				Region:  aws.String("us-west-2"),
				RoleArn: aws.String("arn:aws:iam::123456789012:role/efs"),
				FileSystemId: aws.String(
					"arn:aws:elasticfilesystem:us-west-2:123456789012:" +
						"file-system/fs-11111111"),
			}},
		},
		Tags: &map[string]string{"Name": "unobin-efs", "team": "core"},
	}
	client := newFakeFileSystemClient("us-east-1")
	order := []string{}
	client.order = &order
	client.backupStatuses = []efstypes.Status{
		efstypes.StatusEnabling,
		efstypes.StatusEnabled,
	}
	client.replicationStatuses = []efstypes.ReplicationStatus{
		efstypes.ReplicationStatusEnabling,
		efstypes.ReplicationStatusEnabled,
	}
	client.policyErrors = []error{
		&efstypes.InvalidPolicyException{
			Message: aws.String("Policy contains invalid Principal block"),
		},
		nil,
	}
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	out, err := r.create(context.Background(), &clients)

	require.NoError(t, err)
	assert.Equal(t, testFileSystemID, out.FileSystemId)
	require.Len(t, client.lifecyclePutInputs, 1)
	require.Len(t, client.lifecyclePutInputs[0].LifecyclePolicies, 3)
	assert.Equal(t, efstypes.TransitionToIARulesAfter7Days,
		client.lifecyclePutInputs[0].LifecyclePolicies[0].TransitionToIA)
	assert.Equal(t, efstypes.TransitionToArchiveRulesAfter14Days,
		client.lifecyclePutInputs[0].LifecyclePolicies[1].TransitionToArchive)
	assert.Equal(t, efstypes.TransitionToPrimaryStorageClassRulesAfter1Access,
		client.lifecyclePutInputs[0].LifecyclePolicies[2].TransitionToPrimaryStorageClass)
	assert.Less(t, indexOf(order, "us-east-1:describe"),
		indexOf(order, "us-east-1:put-lifecycle"))
	require.Len(t, client.backupPutInputs, 1)
	assert.Equal(t, efstypes.StatusEnabled,
		client.backupPutInputs[0].BackupPolicy.Status)
	require.Len(t, client.policyPutInputs, 2)
	assert.True(t, client.policyPutInputs[0].BypassPolicyLockoutSafetyCheck)
	require.Len(t, client.protectionInputs, 1)
	assert.Equal(t, efstypes.ReplicationOverwriteProtectionDisabled,
		client.protectionInputs[0].ReplicationOverwriteProtection)
	require.Len(t, client.replicationCreateInputs, 1)
	destination := client.replicationCreateInputs[0].Destinations[0]
	assert.Equal(t, "arn:aws:iam::123456789012:role/efs", aws.ToString(destination.RoleArn))
	assert.Equal(t, []efstypes.Tag{
		{Key: aws.String("Name"), Value: aws.String("unobin-efs")},
		{Key: aws.String("team"), Value: aws.String("core")},
	}, client.createInputs[0].Tags)
}

func TestFileSystemPolicyDoesNotRetryOtherErrors(t *testing.T) {
	policy := `{"Version":"2012-10-17","Statement":[]}`
	r := &FileSystemResource{FileSystemPolicy: &policy}
	client := newFakeFileSystemClient("us-east-1")
	client.policyErrors = []error{
		&efstypes.InvalidPolicyException{Message: aws.String("another policy error")},
		nil,
	}

	err := r.putFileSystemPolicy(context.Background(), client, testFileSystemID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "another policy error")
	assert.Len(t, client.policyPutInputs, 1)
}

func TestFileSystemReadMapsAbsence(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeFileSystemClient)
	}{
		{
			name: "typed not found",
			configure: func(c *fakeFileSystemClient) {
				c.describeFileSystemErrors = []error{&efstypes.FileSystemNotFound{}}
			},
		},
		{
			name: "empty result",
			configure: func(c *fakeFileSystemClient) {
				c.fileSystems = [][]efstypes.FileSystemDescription{{}}
			},
		},
		{
			name: "deleted state",
			configure: func(c *fakeFileSystemClient) {
				fs := testFileSystemDescription()
				fs.LifeCycleState = efstypes.LifeCycleStateDeleted
				c.fileSystems = [][]efstypes.FileSystemDescription{{fs}}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeFileSystemClient("us-east-1")
			tt.configure(client)
			clients := fakeFileSystemClients{region: "us-east-1", source: client}
			out, err := (&FileSystemResource{}).read(context.Background(), &clients,
				&FileSystemResourceOutput{FileSystemId: testFileSystemID})
			assert.Nil(t, out)
			require.ErrorIs(t, err, runtime.ErrNotFound)
		})
	}
}

func TestFileSystemReadMapsOutputsAndOptionalAbsence(t *testing.T) {
	description := testFileSystemDescription()
	description.FileSystemProtection.ReplicationOverwriteProtection =
		efstypes.ReplicationOverwriteProtectionReplicating
	description.Tags = append(description.Tags,
		efstypes.Tag{Key: aws.String("aws:system"), Value: aws.String("ignored")})
	client := newFakeFileSystemClient("us-east-1")
	client.fileSystems = [][]efstypes.FileSystemDescription{{description}}
	client.lifecyclePolicies = []efstypes.LifecyclePolicy{{
		TransitionToIA: efstypes.TransitionToIARulesAfter7Days,
	}}
	client.backupStatuses = []efstypes.Status{efstypes.StatusEnabled}
	client.policyDescribeErrors = []error{&efstypes.PolicyNotFound{}}
	client.replicationErrors = []error{&efstypes.ReplicationNotFound{}}
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	out, err := (&FileSystemResource{}).read(context.Background(), &clients,
		&FileSystemResourceOutput{FileSystemId: testFileSystemID})

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, testFileSystemARN, out.Arn)
	assert.Equal(t, "fs-0123456789abcdef0.efs.us-east-1.amazonaws.com", out.DNSName)
	assert.Equal(t, int64(2), out.NumberOfMountTargets)
	assert.Equal(t, int64(10), out.SizeInBytes.Value)
	assert.Equal(t, "2026-07-13T12:00:00Z", out.SizeInBytes.Timestamp)
	assert.Equal(t, []FileSystemLifecyclePolicy{{
		TransitionToIA: aws.String("AFTER_7_DAYS"),
	}}, out.LifecyclePolicies)
	assert.Equal(t, &FileSystemBackupPolicy{Status: "ENABLED"}, out.BackupPolicy)
	assert.Nil(t, out.FileSystemPolicy)
	require.NotNil(t, out.FileSystemProtection)
	assert.Equal(t, "REPLICATING",
		out.FileSystemProtection.ReplicationOverwriteProtection)
	assert.Nil(t, out.ReplicationConfiguration)
	assert.Equal(t, map[string]string{"Name": "unobin-efs"}, out.Tags)
}

func TestFileSystemReadMapsMissingBackupPolicyToDisabled(t *testing.T) {
	client := newFakeFileSystemClient("us-east-1")
	client.backupDescribeErrors = []error{&efstypes.PolicyNotFound{}}
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	out, err := (&FileSystemResource{}).read(context.Background(), &clients,
		&FileSystemResourceOutput{FileSystemId: testFileSystemID})

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, &FileSystemBackupPolicy{Status: "DISABLED"}, out.BackupPolicy)
}

func TestFileSystemUpdateClearsOptionalConfiguration(t *testing.T) {
	setFastFileSystemTimings(t)
	priorPolicy := `{"Version":"2012-10-17","Statement":[{}]}`
	prior := FileSystemResource{
		ThroughputMode: aws.String("bursting"),
		LifecyclePolicies: &[]FileSystemLifecyclePolicy{{
			TransitionToIA: aws.String("AFTER_7_DAYS"),
		}},
		BackupPolicy:     &FileSystemBackupPolicy{Status: "ENABLED"},
		FileSystemPolicy: &priorPolicy,
		FileSystemProtection: &FileSystemProtection{
			ReplicationOverwriteProtection: "DISABLED",
		},
		Tags: &map[string]string{"remove": "yes", "state": "initial"},
	}
	r := &FileSystemResource{
		ThroughputMode: aws.String("elastic"),
		Tags:           &map[string]string{"state": "updated", "add": "yes"},
	}
	client := newFakeFileSystemClient("us-east-1")
	client.listTags = []efstypes.Tag{
		{Key: aws.String("remove"), Value: aws.String("yes")},
		{Key: aws.String("state"), Value: aws.String("initial")},
	}
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	_, err := r.update(context.Background(), &clients,
		runtime.Prior[FileSystemResource, *FileSystemResourceOutput]{
			Inputs:  prior,
			Outputs: &FileSystemResourceOutput{FileSystemId: testFileSystemID},
		})

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	assert.Equal(t, efstypes.ThroughputModeElastic, client.updateInputs[0].ThroughputMode)
	require.Len(t, client.lifecyclePutInputs, 1)
	assert.NotNil(t, client.lifecyclePutInputs[0].LifecyclePolicies)
	assert.Empty(t, client.lifecyclePutInputs[0].LifecyclePolicies)
	require.Len(t, client.backupPutInputs, 1)
	assert.Equal(t, efstypes.StatusDisabled,
		client.backupPutInputs[0].BackupPolicy.Status)
	require.Len(t, client.policyDeleteInputs, 1)
	require.Len(t, client.protectionInputs, 1)
	assert.Equal(t, efstypes.ReplicationOverwriteProtectionEnabled,
		client.protectionInputs[0].ReplicationOverwriteProtection)
	require.Len(t, client.untagInputs, 1)
	assert.Equal(t, []string{"remove"}, client.untagInputs[0].TagKeys)
	require.Len(t, client.tagInputs, 1)
	assert.Equal(t, []efstypes.Tag{
		{Key: aws.String("add"), Value: aws.String("yes")},
		{Key: aws.String("state"), Value: aws.String("updated")},
	}, client.tagInputs[0].Tags)
}

func TestFileSystemUpdateChangesProvisionedThroughputValue(t *testing.T) {
	r := &FileSystemResource{
		ThroughputMode:               aws.String("provisioned"),
		ProvisionedThroughputInMibps: aws.Float64(5),
	}
	prior := FileSystemResource{
		ThroughputMode:               aws.String("provisioned"),
		ProvisionedThroughputInMibps: aws.Float64(2),
	}
	client := newFakeFileSystemClient("us-east-1")
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	_, err := r.update(context.Background(), &clients,
		runtime.Prior[FileSystemResource, *FileSystemResourceOutput]{
			Inputs:  prior,
			Outputs: &FileSystemResourceOutput{FileSystemId: testFileSystemID},
		})

	require.NoError(t, err)
	require.Len(t, client.updateInputs, 1)
	assert.Empty(t, client.updateInputs[0].ThroughputMode)
	assert.Equal(t, 5.0,
		aws.ToFloat64(client.updateInputs[0].ProvisionedThroughputInMibps))
}

func TestFileSystemUpdateSkipsUnchangedTags(t *testing.T) {
	tags := map[string]string{"Name": "unobin-efs"}
	r := &FileSystemResource{Tags: &tags}
	client := newFakeFileSystemClient("us-east-1")
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	_, err := r.update(context.Background(), &clients,
		runtime.Prior[FileSystemResource, *FileSystemResourceOutput]{
			Inputs:  FileSystemResource{Tags: &tags},
			Outputs: &FileSystemResourceOutput{FileSystemId: testFileSystemID},
			Observed: &FileSystemResourceOutput{Tags: map[string]string{
				"Name": "unobin-efs", "aws:system": "ignored",
			}},
		})

	require.NoError(t, err)
	assert.Empty(t, client.listTagInputs)
	assert.Empty(t, client.tagInputs)
	assert.Empty(t, client.untagInputs)
}

func TestFileSystemUpdateComparesPolicyJSONSemantically(t *testing.T) {
	desired := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`
	tests := []struct {
		name           string
		prior          string
		observed       string
		wantPolicyPuts int
	}{
		{
			name: "formatting and object order are equivalent",
			prior: `{
  "Statement": [{"Effect": "Allow"}],
  "Version": "2012-10-17"
}`,
			observed:       `{"Statement":[{"Effect":"Allow"}],"Version":"2012-10-17"}`,
			wantPolicyPuts: 0,
		},
		{
			name:           "real drift writes policy",
			prior:          desired,
			observed:       `{"Version":"2012-10-17","Statement":[]}`,
			wantPolicyPuts: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &FileSystemResource{FileSystemPolicy: &desired}
			client := newFakeFileSystemClient("us-east-1")
			client.policy = &desired
			clients := fakeFileSystemClients{region: "us-east-1", source: client}

			_, err := r.update(context.Background(), &clients,
				runtime.Prior[FileSystemResource, *FileSystemResourceOutput]{
					Inputs: FileSystemResource{
						FileSystemPolicy: &tt.prior,
					},
					Outputs: &FileSystemResourceOutput{FileSystemId: testFileSystemID},
					Observed: &FileSystemResourceOutput{
						FileSystemPolicy: &tt.observed,
					},
				})

			require.NoError(t, err)
			assert.Len(t, client.policyPutInputs, tt.wantPolicyPuts)
		})
	}
}

func TestFileSystemUpdateAcceptsReplicatingAsDisabledProtection(t *testing.T) {
	protection := &FileSystemProtection{
		ReplicationOverwriteProtection: "DISABLED",
	}
	r := &FileSystemResource{FileSystemProtection: protection}
	client := newFakeFileSystemClient("us-east-1")
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	_, err := r.update(context.Background(), &clients,
		runtime.Prior[FileSystemResource, *FileSystemResourceOutput]{
			Inputs:  FileSystemResource{FileSystemProtection: protection},
			Outputs: &FileSystemResourceOutput{FileSystemId: testFileSystemID},
			Observed: &FileSystemResourceOutput{
				FileSystemProtection: &FileSystemProtection{
					ReplicationOverwriteProtection: "REPLICATING",
				},
			},
		})

	require.NoError(t, err)
	assert.Empty(t, client.protectionInputs)
}

func TestFileSystemReplicationReplacementDeletesDestinationFirst(t *testing.T) {
	setFastFileSystemTimings(t)
	oldDestination := FileSystemReplicationDestination{
		Region:       aws.String("us-west-2"),
		FileSystemId: aws.String("fs-11111111"),
	}
	newDestination := FileSystemReplicationDestination{
		Region:       aws.String("eu-west-1"),
		FileSystemId: aws.String("fs-22222222"),
	}
	prior := FileSystemResource{
		ReplicationConfiguration: &FileSystemReplicationConfiguration{
			Destinations: []FileSystemReplicationDestination{oldDestination},
		},
	}
	r := &FileSystemResource{
		ReplicationConfiguration: &FileSystemReplicationConfiguration{
			Destinations: []FileSystemReplicationDestination{newDestination},
		},
	}
	order := []string{}
	source := newFakeFileSystemClient("us-east-1")
	source.order = &order
	source.replications = [][]efstypes.ReplicationConfigurationDescription{
		{testReplication("us-west-2", "fs-11111111", efstypes.ReplicationStatusEnabled)},
		{testReplication("us-west-2", "fs-11111111", efstypes.ReplicationStatusDeleting)},
		{},
		{},
		{testReplication("eu-west-1", "fs-22222222", efstypes.ReplicationStatusEnabled)},
		{testReplication("eu-west-1", "fs-22222222", efstypes.ReplicationStatusEnabled)},
	}
	destination := newFakeFileSystemClient("us-west-2")
	destination.order = &order
	destination.replications = [][]efstypes.ReplicationConfigurationDescription{
		{testReplication("us-west-2", "fs-11111111", efstypes.ReplicationStatusDeleting)},
		{},
		{},
	}
	clients := fakeFileSystemClients{
		region: "us-east-1",
		source: source,
		regional: map[string]*fakeFileSystemClient{
			"us-west-2": destination,
		},
	}

	_, err := r.update(context.Background(), &clients,
		runtime.Prior[FileSystemResource, *FileSystemResourceOutput]{
			Inputs:  prior,
			Outputs: &FileSystemResourceOutput{FileSystemId: testFileSystemID},
		})

	require.NoError(t, err)
	destinationDelete := indexOf(order, "us-west-2:delete-replication")
	sourceDelete := indexOf(order, "us-east-1:delete-replication")
	require.NotEqual(t, -1, destinationDelete)
	require.NotEqual(t, -1, sourceDelete)
	assert.Less(t, destinationDelete, sourceDelete)
	require.Len(t, destination.replicationDeleteInputs, 1)
	assert.Equal(t, testFileSystemID,
		aws.ToString(destination.replicationDeleteInputs[0].SourceFileSystemId))
	assert.Empty(t, destination.replicationDeleteInputs[0].DeletionMode)
	require.Len(t, source.replicationDeleteInputs, 1)
	assert.Equal(t, testFileSystemID,
		aws.ToString(source.replicationDeleteInputs[0].SourceFileSystemId))
	assert.Empty(t, source.replicationDeleteInputs[0].DeletionMode)
	require.Len(t, destination.replicationDescribeInputs, 3)
	for _, in := range destination.replicationDescribeInputs {
		assert.Equal(t, testFileSystemID, aws.ToString(in.FileSystemId))
	}
	destinationWait := indexOf(order, "us-west-2:describe-replication")
	assert.Less(t, destinationWait, sourceDelete)
	require.Len(t, source.replicationCreateInputs, 1)
	createIndex := indexOf(order, "us-east-1:create-replication")
	require.NotEqual(t, -1, createIndex)
	describesBeforeCreate := 0
	for _, call := range order[:createIndex] {
		if call == "us-east-1:describe-replication" {
			describesBeforeCreate++
		}
	}
	assert.Equal(t, 4, describesBeforeCreate)
}

func TestFileSystemReplicationAccountRules(t *testing.T) {
	t.Run("same account omits role", func(t *testing.T) {
		r := &FileSystemResource{
			ReplicationConfiguration: &FileSystemReplicationConfiguration{
				Destinations: []FileSystemReplicationDestination{{
					Region: aws.String("us-west-2"),
					FileSystemId: aws.String(
						"arn:aws:elasticfilesystem:us-west-2:123456789012:" +
							"file-system/fs-11111111"),
				}},
			},
		}
		client := newFakeFileSystemClient("us-east-1")

		err := r.createReplication(context.Background(), client, testFileSystemID,
			"123456789012")

		require.NoError(t, err)
		require.Len(t, client.replicationCreateInputs, 1)
		assert.Nil(t, client.replicationCreateInputs[0].Destinations[0].RoleArn)
	})

	t.Run("same account forwards role with destination ID", func(t *testing.T) {
		r := &FileSystemResource{
			ReplicationConfiguration: &FileSystemReplicationConfiguration{
				Destinations: []FileSystemReplicationDestination{{
					Region:       aws.String("us-west-2"),
					FileSystemId: aws.String("fs-12345678"),
					RoleArn: aws.String(
						"arn:aws:iam::123456789012:role/efs"),
				}},
			},
		}
		client := newFakeFileSystemClient("us-east-1")

		err := r.createReplication(context.Background(), client, testFileSystemID,
			"123456789012")

		require.NoError(t, err)
		require.Len(t, client.replicationCreateInputs, 1)
		assert.Equal(t, "arn:aws:iam::123456789012:role/efs",
			aws.ToString(client.replicationCreateInputs[0].Destinations[0].RoleArn))
	})

	t.Run("cross account requires role", func(t *testing.T) {
		r := &FileSystemResource{
			ReplicationConfiguration: &FileSystemReplicationConfiguration{
				Destinations: []FileSystemReplicationDestination{{
					Region: aws.String("us-west-2"),
					FileSystemId: aws.String(
						"arn:aws:elasticfilesystem:us-west-2:210987654321:" +
							"file-system/fs-22222222"),
				}},
			},
		}
		client := newFakeFileSystemClient("us-east-1")

		err := r.createReplication(context.Background(), client, testFileSystemID,
			"123456789012")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "cross-account replication requires")
		assert.Empty(t, client.replicationCreateInputs)
	})

	t.Run("role belongs to source account", func(t *testing.T) {
		r := &FileSystemResource{
			ReplicationConfiguration: &FileSystemReplicationConfiguration{
				Destinations: []FileSystemReplicationDestination{{
					Region:       aws.String("us-west-2"),
					FileSystemId: aws.String("fs-12345678"),
					RoleArn: aws.String(
						"arn:aws:iam::210987654321:role/efs"),
				}},
			},
		}
		client := newFakeFileSystemClient("us-east-1")

		err := r.createReplication(context.Background(), client, testFileSystemID,
			"123456789012")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "role-arn must belong to the source account")
		assert.Empty(t, client.replicationCreateInputs)
	})
}

func TestDeleteReplicationPropagatesDescribeError(t *testing.T) {
	client := newFakeFileSystemClient("us-east-1")
	client.replicationErrors = []error{errors.New("describe failed")}
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	err := deleteReplication(context.Background(), &clients, testFileSystemID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "describe failed")
	assert.Empty(t, client.replicationDeleteInputs)
}

func TestFileSystemDeleteWaitsUntilAbsent(t *testing.T) {
	setFastFileSystemTimings(t)
	client := newFakeFileSystemClient("us-east-1")
	client.replicationErrors = []error{&efstypes.ReplicationNotFound{}}
	client.fileSystems = [][]efstypes.FileSystemDescription{
		{testFileSystemDescription()},
		{},
	}
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	err := (&FileSystemResource{}).delete(context.Background(), &clients,
		&FileSystemResourceOutput{FileSystemId: testFileSystemID})

	require.NoError(t, err)
	require.Len(t, client.deleteInputs, 1)
	assert.GreaterOrEqual(t, len(client.describeFileSystemInputs), 2)
}

func TestFileSystemDeleteAlreadyGone(t *testing.T) {
	setFastFileSystemTimings(t)
	client := newFakeFileSystemClient("us-east-1")
	client.replicationErrors = []error{&efstypes.ReplicationNotFound{}}
	client.deleteErrors = []error{&efstypes.FileSystemNotFound{}}
	clients := fakeFileSystemClients{region: "us-east-1", source: client}

	err := (&FileSystemResource{}).delete(context.Background(), &clients,
		&FileSystemResourceOutput{FileSystemId: testFileSystemID})

	require.NoError(t, err)
}

func cloneFileSystemResource(in FileSystemResource) FileSystemResource {
	out := in
	if in.LifecyclePolicies != nil {
		policies := append([]FileSystemLifecyclePolicy(nil), (*in.LifecyclePolicies)...)
		out.LifecyclePolicies = &policies
	}
	if in.BackupPolicy != nil {
		value := *in.BackupPolicy
		out.BackupPolicy = &value
	}
	if in.FileSystemProtection != nil {
		value := *in.FileSystemProtection
		out.FileSystemProtection = &value
	}
	if in.ReplicationConfiguration != nil {
		value := *in.ReplicationConfiguration
		value.Destinations = append(
			[]FileSystemReplicationDestination(nil), value.Destinations...)
		out.ReplicationConfiguration = &value
	}
	if in.Tags != nil {
		tags := make(map[string]string, len(*in.Tags))
		maps.Copy(tags, *in.Tags)
		out.Tags = &tags
	}
	return out
}

func setFastFileSystemTimings(t *testing.T) {
	t.Helper()
	oldPoll := fileSystemPollInterval
	oldPolicy := fileSystemPolicyRetryInterval
	fileSystemPollInterval = 0
	fileSystemPolicyRetryInterval = 0
	t.Cleanup(func() {
		fileSystemPollInterval = oldPoll
		fileSystemPolicyRetryInterval = oldPolicy
	})
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}

func testFileSystemDescription() efstypes.FileSystemDescription {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	return efstypes.FileSystemDescription{
		CreationTime:         &now,
		CreationToken:        aws.String("token"),
		FileSystemId:         aws.String(testFileSystemID),
		LifeCycleState:       efstypes.LifeCycleStateAvailable,
		NumberOfMountTargets: 2,
		OwnerId:              aws.String("123456789012"),
		PerformanceMode:      efstypes.PerformanceModeGeneralPurpose,
		SizeInBytes: &efstypes.FileSystemSize{
			Value:           10,
			Timestamp:       &now,
			ValueInArchive:  aws.Int64(1),
			ValueInIA:       aws.Int64(2),
			ValueInStandard: aws.Int64(7),
		},
		AvailabilityZoneId:   aws.String("use1-az1"),
		AvailabilityZoneName: aws.String("us-east-1a"),
		Encrypted:            aws.Bool(true),
		FileSystemArn:        aws.String(testFileSystemARN),
		FileSystemProtection: &efstypes.FileSystemProtectionDescription{
			ReplicationOverwriteProtection: efstypes.ReplicationOverwriteProtectionEnabled,
		},
		KmsKeyId:       aws.String("alias/efs"),
		Name:           aws.String("unobin-efs"),
		ThroughputMode: efstypes.ThroughputModeElastic,
		Tags: []efstypes.Tag{
			{Key: aws.String("Name"), Value: aws.String("unobin-efs")},
		},
	}
}

func testReplication(
	region string,
	id string,
	status efstypes.ReplicationStatus,
) efstypes.ReplicationConfigurationDescription {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	return efstypes.ReplicationConfigurationDescription{
		CreationTime:                &now,
		OriginalSourceFileSystemArn: aws.String(testFileSystemARN),
		SourceFileSystemArn:         aws.String(testFileSystemARN),
		SourceFileSystemId:          aws.String(testFileSystemID),
		SourceFileSystemRegion:      aws.String("us-east-1"),
		SourceFileSystemOwnerId:     aws.String("123456789012"),
		Destinations: []efstypes.Destination{{
			FileSystemId: aws.String(id),
			Region:       aws.String(region),
			Status:       status,
			OwnerId:      aws.String("123456789012"),
		}},
	}
}

type fakeFileSystemClients struct {
	region   string
	source   *fakeFileSystemClient
	regional map[string]*fakeFileSystemClient
}

func (c *fakeFileSystemClients) Source() fileSystemClient { return c.source }

func (c *fakeFileSystemClients) ForRegion(region string) fileSystemClient {
	if client := c.regional[region]; client != nil {
		return client
	}
	return c.source
}

func (c *fakeFileSystemClients) Region() string { return c.region }

type fakeFileSystemClient struct {
	region                    string
	order                     *[]string
	createInputs              []*efssdk.CreateFileSystemInput
	createErrors              []error
	describeFileSystemInputs  []*efssdk.DescribeFileSystemsInput
	describeFileSystemErrors  []error
	fileSystems               [][]efstypes.FileSystemDescription
	updateInputs              []*efssdk.UpdateFileSystemInput
	updateErrors              []error
	deleteInputs              []*efssdk.DeleteFileSystemInput
	deleteErrors              []error
	lifecyclePutInputs        []*efssdk.PutLifecycleConfigurationInput
	lifecyclePutErrors        []error
	lifecycleDescribeErrors   []error
	lifecyclePolicies         []efstypes.LifecyclePolicy
	backupPutInputs           []*efssdk.PutBackupPolicyInput
	backupPutErrors           []error
	backupDescribeErrors      []error
	backupStatuses            []efstypes.Status
	policyPutInputs           []*efssdk.PutFileSystemPolicyInput
	policyErrors              []error
	policyDescribeErrors      []error
	policy                    *string
	policyDeleteInputs        []*efssdk.DeleteFileSystemPolicyInput
	policyDeleteErrors        []error
	protectionInputs          []*efssdk.UpdateFileSystemProtectionInput
	protectionErrors          []error
	replicationCreateInputs   []*efssdk.CreateReplicationConfigurationInput
	replicationCreateErrors   []error
	replicationDescribeInputs []*efssdk.DescribeReplicationConfigurationsInput
	replicationErrors         []error
	replicationStatuses       []efstypes.ReplicationStatus
	replications              [][]efstypes.ReplicationConfigurationDescription
	replicationDeleteInputs   []*efssdk.DeleteReplicationConfigurationInput
	replicationDeleteErrors   []error
	listTagInputs             []*efssdk.ListTagsForResourceInput
	listTagErrors             []error
	listTags                  []efstypes.Tag
	tagInputs                 []*efssdk.TagResourceInput
	tagErrors                 []error
	untagInputs               []*efssdk.UntagResourceInput
	untagErrors               []error
}

func newFakeFileSystemClient(region string) *fakeFileSystemClient {
	return &fakeFileSystemClient{region: region}
}

func (c *fakeFileSystemClient) record(call string) {
	if c.order != nil {
		*c.order = append(*c.order, c.region+":"+call)
	}
}

func popError(errors *[]error) error {
	if len(*errors) == 0 {
		return nil
	}
	err := (*errors)[0]
	*errors = (*errors)[1:]
	return err
}

func (c *fakeFileSystemClient) CreateFileSystem(
	_ context.Context,
	in *efssdk.CreateFileSystemInput,
	_ ...func(*efssdk.Options),
) (*efssdk.CreateFileSystemOutput, error) {
	c.record("create")
	c.createInputs = append(c.createInputs, in)
	if err := popError(&c.createErrors); err != nil {
		return nil, err
	}
	return &efssdk.CreateFileSystemOutput{FileSystemId: aws.String(testFileSystemID)}, nil
}

func (c *fakeFileSystemClient) DescribeFileSystems(
	_ context.Context,
	in *efssdk.DescribeFileSystemsInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DescribeFileSystemsOutput, error) {
	c.record("describe")
	c.describeFileSystemInputs = append(c.describeFileSystemInputs, in)
	if err := popError(&c.describeFileSystemErrors); err != nil {
		return nil, err
	}
	if len(c.fileSystems) > 0 {
		out := c.fileSystems[0]
		c.fileSystems = c.fileSystems[1:]
		return &efssdk.DescribeFileSystemsOutput{FileSystems: out}, nil
	}
	return &efssdk.DescribeFileSystemsOutput{
		FileSystems: []efstypes.FileSystemDescription{testFileSystemDescription()},
	}, nil
}

func (c *fakeFileSystemClient) UpdateFileSystem(
	_ context.Context,
	in *efssdk.UpdateFileSystemInput,
	_ ...func(*efssdk.Options),
) (*efssdk.UpdateFileSystemOutput, error) {
	c.record("update")
	c.updateInputs = append(c.updateInputs, in)
	return &efssdk.UpdateFileSystemOutput{}, popError(&c.updateErrors)
}

func (c *fakeFileSystemClient) DeleteFileSystem(
	_ context.Context,
	in *efssdk.DeleteFileSystemInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DeleteFileSystemOutput, error) {
	c.record("delete")
	c.deleteInputs = append(c.deleteInputs, in)
	return &efssdk.DeleteFileSystemOutput{}, popError(&c.deleteErrors)
}

func (c *fakeFileSystemClient) PutLifecycleConfiguration(
	_ context.Context,
	in *efssdk.PutLifecycleConfigurationInput,
	_ ...func(*efssdk.Options),
) (*efssdk.PutLifecycleConfigurationOutput, error) {
	c.record("put-lifecycle")
	c.lifecyclePutInputs = append(c.lifecyclePutInputs, in)
	return &efssdk.PutLifecycleConfigurationOutput{}, popError(&c.lifecyclePutErrors)
}

func (c *fakeFileSystemClient) DescribeLifecycleConfiguration(
	_ context.Context,
	_ *efssdk.DescribeLifecycleConfigurationInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DescribeLifecycleConfigurationOutput, error) {
	c.record("describe-lifecycle")
	if err := popError(&c.lifecycleDescribeErrors); err != nil {
		return nil, err
	}
	return &efssdk.DescribeLifecycleConfigurationOutput{
		LifecyclePolicies: c.lifecyclePolicies,
	}, nil
}

func (c *fakeFileSystemClient) PutBackupPolicy(
	_ context.Context,
	in *efssdk.PutBackupPolicyInput,
	_ ...func(*efssdk.Options),
) (*efssdk.PutBackupPolicyOutput, error) {
	c.record("put-backup")
	c.backupPutInputs = append(c.backupPutInputs, in)
	return &efssdk.PutBackupPolicyOutput{}, popError(&c.backupPutErrors)
}

func (c *fakeFileSystemClient) DescribeBackupPolicy(
	_ context.Context,
	_ *efssdk.DescribeBackupPolicyInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DescribeBackupPolicyOutput, error) {
	c.record("describe-backup")
	if err := popError(&c.backupDescribeErrors); err != nil {
		return nil, err
	}
	status := efstypes.StatusDisabled
	if len(c.backupStatuses) > 0 {
		status = c.backupStatuses[0]
		c.backupStatuses = c.backupStatuses[1:]
	}
	return &efssdk.DescribeBackupPolicyOutput{
		BackupPolicy: &efstypes.BackupPolicy{Status: status},
	}, nil
}

func (c *fakeFileSystemClient) PutFileSystemPolicy(
	_ context.Context,
	in *efssdk.PutFileSystemPolicyInput,
	_ ...func(*efssdk.Options),
) (*efssdk.PutFileSystemPolicyOutput, error) {
	c.record("put-policy")
	c.policyPutInputs = append(c.policyPutInputs, in)
	return &efssdk.PutFileSystemPolicyOutput{}, popError(&c.policyErrors)
}

func (c *fakeFileSystemClient) DescribeFileSystemPolicy(
	_ context.Context,
	_ *efssdk.DescribeFileSystemPolicyInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DescribeFileSystemPolicyOutput, error) {
	c.record("describe-policy")
	if err := popError(&c.policyDescribeErrors); err != nil {
		return nil, err
	}
	if c.policy == nil {
		return nil, &efstypes.PolicyNotFound{}
	}
	return &efssdk.DescribeFileSystemPolicyOutput{Policy: c.policy}, nil
}

func (c *fakeFileSystemClient) DeleteFileSystemPolicy(
	_ context.Context,
	in *efssdk.DeleteFileSystemPolicyInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DeleteFileSystemPolicyOutput, error) {
	c.record("delete-policy")
	c.policyDeleteInputs = append(c.policyDeleteInputs, in)
	return &efssdk.DeleteFileSystemPolicyOutput{}, popError(&c.policyDeleteErrors)
}

func (c *fakeFileSystemClient) UpdateFileSystemProtection(
	_ context.Context,
	in *efssdk.UpdateFileSystemProtectionInput,
	_ ...func(*efssdk.Options),
) (*efssdk.UpdateFileSystemProtectionOutput, error) {
	c.record("update-protection")
	c.protectionInputs = append(c.protectionInputs, in)
	return &efssdk.UpdateFileSystemProtectionOutput{}, popError(&c.protectionErrors)
}

func (c *fakeFileSystemClient) CreateReplicationConfiguration(
	_ context.Context,
	in *efssdk.CreateReplicationConfigurationInput,
	_ ...func(*efssdk.Options),
) (*efssdk.CreateReplicationConfigurationOutput, error) {
	c.record("create-replication")
	c.replicationCreateInputs = append(c.replicationCreateInputs, in)
	return &efssdk.CreateReplicationConfigurationOutput{},
		popError(&c.replicationCreateErrors)
}

func (c *fakeFileSystemClient) DescribeReplicationConfigurations(
	_ context.Context,
	in *efssdk.DescribeReplicationConfigurationsInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DescribeReplicationConfigurationsOutput, error) {
	c.record("describe-replication")
	c.replicationDescribeInputs = append(c.replicationDescribeInputs, in)
	if err := popError(&c.replicationErrors); err != nil {
		return nil, err
	}
	if len(c.replications) > 0 {
		out := c.replications[0]
		c.replications = c.replications[1:]
		return &efssdk.DescribeReplicationConfigurationsOutput{Replications: out}, nil
	}
	status := efstypes.ReplicationStatusEnabled
	if len(c.replicationStatuses) > 0 {
		status = c.replicationStatuses[0]
		c.replicationStatuses = c.replicationStatuses[1:]
	}
	return &efssdk.DescribeReplicationConfigurationsOutput{
		Replications: []efstypes.ReplicationConfigurationDescription{
			testReplication("us-west-2", "fs-destination", status),
		},
	}, nil
}

func (c *fakeFileSystemClient) DeleteReplicationConfiguration(
	_ context.Context,
	in *efssdk.DeleteReplicationConfigurationInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DeleteReplicationConfigurationOutput, error) {
	c.record("delete-replication")
	c.replicationDeleteInputs = append(c.replicationDeleteInputs, in)
	return &efssdk.DeleteReplicationConfigurationOutput{},
		popError(&c.replicationDeleteErrors)
}

func (c *fakeFileSystemClient) ListTagsForResource(
	_ context.Context,
	in *efssdk.ListTagsForResourceInput,
	_ ...func(*efssdk.Options),
) (*efssdk.ListTagsForResourceOutput, error) {
	c.record("list-tags")
	c.listTagInputs = append(c.listTagInputs, in)
	if err := popError(&c.listTagErrors); err != nil {
		return nil, err
	}
	return &efssdk.ListTagsForResourceOutput{Tags: c.listTags}, nil
}

func (c *fakeFileSystemClient) TagResource(
	_ context.Context,
	in *efssdk.TagResourceInput,
	_ ...func(*efssdk.Options),
) (*efssdk.TagResourceOutput, error) {
	c.record("tag")
	c.tagInputs = append(c.tagInputs, in)
	return &efssdk.TagResourceOutput{}, popError(&c.tagErrors)
}

func (c *fakeFileSystemClient) UntagResource(
	_ context.Context,
	in *efssdk.UntagResourceInput,
	_ ...func(*efssdk.Options),
) (*efssdk.UntagResourceOutput, error) {
	c.record("untag")
	c.untagInputs = append(c.untagInputs, in)
	return &efssdk.UntagResourceOutput{}, popError(&c.untagErrors)
}

var _ fileSystemClient = (*fakeFileSystemClient)(nil)
