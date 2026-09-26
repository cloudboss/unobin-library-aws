package efs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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
	testAccessPointID           = "fsap-0123456789abcdef0"
	testAccessPointFileSystemID = "fs-0123456789abcdef0"
)

func TestAccessPointMetadata(t *testing.T) {
	resource := &AccessPointResource{}
	assert.Equal(t, 1, resource.SchemaVersion())
	assert.Equal(t, []string{
		"file-system-id", "client-token", "posix-user", "root-directory",
	}, resource.ReplaceFields())
	assert.NotPanics(t, func() {
		assert.NotEmpty(t, resource.Constraints())
	})
}

func TestAccessPointValidateInputsAcceptsBoundaries(t *testing.T) {
	secondaryGIDs := make([]int64, 16)
	for index := range secondaryGIDs {
		secondaryGIDs[index] = int64(index)
	}
	token := strings.Repeat("a", 64)
	path := "/" + strings.Repeat("p", 99)
	tags := map[string]string{strings.Repeat("k", 128): strings.Repeat("v", 256), "empty": ""}
	resources := []AccessPointResource{
		{
			FileSystemId: testAccessPointFileSystemID,
			ClientToken:  &token,
			PosixUser: &AccessPointPosixUser{
				Uid: 0, Gid: accessPointMaxPOSIXID, SecondaryGids: &secondaryGIDs,
			},
			RootDirectory: &AccessPointRootDirectory{
				Path: &path,
				CreationInfo: &AccessPointCreationInfo{
					OwnerUid: 0, OwnerGid: accessPointMaxPOSIXID, Permissions: "0755",
				},
			},
			Tags: &tags,
		},
		{
			FileSystemId: "arn:aws:elasticfilesystem:us-east-1:123456789012:" +
				"file-system/fs-01234567",
			RootDirectory: &AccessPointRootDirectory{Path: aws.String("/existing")},
		},
		{FileSystemId: testAccessPointFileSystemID, RootDirectory: &AccessPointRootDirectory{
			Path: aws.String("/one/two/three/four"),
		}},
		{FileSystemId: testAccessPointFileSystemID, RootDirectory: &AccessPointRootDirectory{
			Path: aws.String("/"),
		}},
	}
	for index := range resources {
		require.NoError(t, resources[index].ValidateInputs(context.Background(), nil), index)
	}
}

func TestAccessPointValidateInputsRejectsInvalidValues(t *testing.T) {
	longSecondary := make([]int64, 17)
	tests := []struct {
		name    string
		change  func(*AccessPointResource)
		wantErr string
	}{
		{name: "file system ID", change: func(r *AccessPointResource) {
			r.FileSystemId = "bad"
		}, wantErr: "file-system-id"},
		{name: "empty client token", change: func(r *AccessPointResource) {
			r.ClientToken = aws.String("")
		}, wantErr: "client-token"},
		{name: "long client token", change: func(r *AccessPointResource) {
			r.ClientToken = aws.String(strings.Repeat("a", 65))
		}, wantErr: "client-token"},
		{name: "non-ASCII client token", change: func(r *AccessPointResource) {
			r.ClientToken = aws.String("café")
		}, wantErr: "ASCII"},
		{name: "negative UID", change: func(r *AccessPointResource) {
			r.PosixUser = &AccessPointPosixUser{Uid: -1}
		}, wantErr: "uid"},
		{name: "large GID", change: func(r *AccessPointResource) {
			r.PosixUser = &AccessPointPosixUser{Gid: accessPointMaxPOSIXID + 1}
		}, wantErr: "gid"},
		{name: "too many secondary GIDs", change: func(r *AccessPointResource) {
			r.PosixUser = &AccessPointPosixUser{SecondaryGids: &longSecondary}
		}, wantErr: "at most 16"},
		{name: "invalid secondary GID", change: func(r *AccessPointResource) {
			values := []int64{-1}
			r.PosixUser = &AccessPointPosixUser{SecondaryGids: &values}
		}, wantErr: "secondary-gids[0]"},
		{name: "empty path", change: func(r *AccessPointResource) {
			r.RootDirectory = &AccessPointRootDirectory{Path: aws.String("")}
		}, wantErr: "root-directory path"},
		{name: "relative path", change: func(r *AccessPointResource) {
			r.RootDirectory = &AccessPointRootDirectory{Path: aws.String("relative")}
		}, wantErr: "root-directory path"},
		{name: "too many path components", change: func(r *AccessPointResource) {
			r.RootDirectory = &AccessPointRootDirectory{Path: aws.String("/a/b/c/d/e")}
		}, wantErr: "root-directory path"},
		{name: "dot-leading component", change: func(r *AccessPointResource) {
			r.RootDirectory = &AccessPointRootDirectory{Path: aws.String("/a/.b")}
		}, wantErr: "root-directory path"},
		{name: "invalid path character", change: func(r *AccessPointResource) {
			r.RootDirectory = &AccessPointRootDirectory{Path: aws.String("/a?b")}
		}, wantErr: "root-directory path"},
		{name: "long path", change: func(r *AccessPointResource) {
			r.RootDirectory = &AccessPointRootDirectory{
				Path: aws.String("/" + strings.Repeat("a", 100)),
			}
		}, wantErr: "root-directory path"},
		{name: "invalid owner UID", change: func(r *AccessPointResource) {
			r.RootDirectory = &AccessPointRootDirectory{
				CreationInfo: &AccessPointCreationInfo{OwnerUid: -1, Permissions: "755"},
			}
		}, wantErr: "owner-uid"},
		{name: "invalid owner GID", change: func(r *AccessPointResource) {
			r.RootDirectory = &AccessPointRootDirectory{
				CreationInfo: &AccessPointCreationInfo{
					OwnerGid: accessPointMaxPOSIXID + 1, Permissions: "755",
				},
			}
		}, wantErr: "owner-gid"},
		{name: "invalid permissions", change: func(r *AccessPointResource) {
			r.RootDirectory = &AccessPointRootDirectory{
				CreationInfo: &AccessPointCreationInfo{Permissions: "888"},
			}
		}, wantErr: "permissions"},
		{name: "empty tag key", change: func(r *AccessPointResource) {
			tags := map[string]string{"": "value"}
			r.Tags = &tags
		}, wantErr: "tag key"},
		{name: "reserved tag key", change: func(r *AccessPointResource) {
			tags := map[string]string{"AWS:owner": "value"}
			r.Tags = &tags
		}, wantErr: "must not start with aws:"},
		{name: "invalid tag key", change: func(r *AccessPointResource) {
			tags := map[string]string{"bad*key": "value"}
			r.Tags = &tags
		}, wantErr: "outside the allowed"},
		{name: "long tag key", change: func(r *AccessPointResource) {
			tags := map[string]string{strings.Repeat("k", 129): "value"}
			r.Tags = &tags
		}, wantErr: "1 to 128"},
		{name: "long tag value", change: func(r *AccessPointResource) {
			tags := map[string]string{"key": strings.Repeat("v", 257)}
			r.Tags = &tags
		}, wantErr: "at most 256"},
		{name: "invalid tag value", change: func(r *AccessPointResource) {
			tags := map[string]string{"key": "line\nbreak"}
			r.Tags = &tags
		}, wantErr: "outside the allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := AccessPointResource{FileSystemId: testAccessPointFileSystemID}
			tt.change(&resource)
			err := resource.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAccessPointCreateMapsInputsAndReturnsWaitObservation(t *testing.T) {
	token := "explicit-token"
	secondary := []int64{3, 4}
	tags := map[string]string{"Name": "access", "empty": ""}
	resource := &AccessPointResource{
		FileSystemId: testAccessPointFileSystemID,
		ClientToken:  &token,
		PosixUser: &AccessPointPosixUser{
			Uid: 1, Gid: 2, SecondaryGids: &secondary,
		},
		RootDirectory: &AccessPointRootDirectory{
			Path: aws.String("/app"),
			CreationInfo: &AccessPointCreationInfo{
				OwnerUid: 5, OwnerGid: 6, Permissions: "0755",
			},
		},
		Tags: &tags,
	}
	client := &fakeAccessPointClient{
		createOutput: &efssdk.CreateAccessPointOutput{AccessPointId: aws.String(testAccessPointID)},
		describeResults: []accessPointDescribeResult{
			{output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating)},
			{output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable)},
		},
	}
	clock := &fakeAccessPointClock{}

	out, err := resource.create(context.Background(), client, clock)

	require.NoError(t, err)
	require.Len(t, client.createInputs, 1)
	in := client.createInputs[0]
	assert.Equal(t, testAccessPointFileSystemID, aws.ToString(in.FileSystemId))
	assert.Equal(t, token, aws.ToString(in.ClientToken))
	require.NotNil(t, in.PosixUser)
	assert.Equal(t, int64(1), aws.ToInt64(in.PosixUser.Uid))
	assert.Equal(t, int64(2), aws.ToInt64(in.PosixUser.Gid))
	assert.Equal(t, secondary, in.PosixUser.SecondaryGids)
	require.NotNil(t, in.RootDirectory)
	assert.Equal(t, "/app", aws.ToString(in.RootDirectory.Path))
	require.NotNil(t, in.RootDirectory.CreationInfo)
	assert.Equal(t, int64(5), aws.ToInt64(in.RootDirectory.CreationInfo.OwnerUid))
	assert.Equal(t, int64(6), aws.ToInt64(in.RootDirectory.CreationInfo.OwnerGid))
	assert.Equal(t, "0755", aws.ToString(in.RootDirectory.CreationInfo.Permissions))
	assert.Equal(t, []efstypes.Tag{
		{Key: aws.String("Name"), Value: aws.String("access")},
		{Key: aws.String("empty"), Value: aws.String("")},
	}, in.Tags)
	assert.Equal(t, testAccessPointOutput(), out)
	assert.Equal(t, []time.Duration{200 * time.Millisecond}, clock.sleeps)
}

func TestAccessPointCreateOmitsOptionalInputs(t *testing.T) {
	tests := []struct {
		name string
		tags *map[string]string
	}{
		{name: "omitted tags"},
		{name: "empty tags", tags: &map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeAccessPointClient{
				createOutput: &efssdk.CreateAccessPointOutput{
					AccessPointId: aws.String(testAccessPointID),
				},
				describeResults: []accessPointDescribeResult{{
					output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable),
				}},
			}
			resource := &AccessPointResource{FileSystemId: testAccessPointFileSystemID, Tags: tt.tags}

			_, err := resource.create(context.Background(), client, &fakeAccessPointClock{})

			require.NoError(t, err)
			require.Len(t, client.createInputs, 1)
			in := client.createInputs[0]
			assert.Nil(t, in.ClientToken)
			assert.Nil(t, in.PosixUser)
			assert.Nil(t, in.RootDirectory)
			assert.Nil(t, in.Tags)
		})
	}
}

func TestAccessPointSDKAddsOmittedClientToken(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"AccessPointId":"` + testAccessPointID + `"}`))
	}))
	t.Cleanup(server.Close)
	client := efssdk.NewFromConfig(aws.Config{
		Region:       "us-east-1",
		Credentials:  aws.AnonymousCredentials{},
		BaseEndpoint: aws.String(server.URL),
		HTTPClient:   server.Client(),
	})

	_, err := client.CreateAccessPoint(context.Background(), &efssdk.CreateAccessPointInput{
		FileSystemId: aws.String(testAccessPointFileSystemID),
	})

	require.NoError(t, err)
	token, ok := requestBody["ClientToken"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, token)
}

func TestAccessPointCreateErrorsDoNotRetryOrAdopt(t *testing.T) {
	tests := []struct {
		name      string
		output    *efssdk.CreateAccessPointOutput
		createErr error
		wantErr   string
		wantErrIs error
	}{
		{
			name:      "already exists",
			createErr: &efstypes.AccessPointAlreadyExists{},
			wantErr:   "create access point",
		},
		{name: "missing ID", output: &efssdk.CreateAccessPointOutput{}, wantErr: "no access-point-id"},
		{name: "other error", createErr: errors.New("boom"), wantErr: "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeAccessPointClient{createOutput: tt.output, createErr: tt.createErr}
			resource := &AccessPointResource{FileSystemId: testAccessPointFileSystemID}

			_, err := resource.create(context.Background(), client, &fakeAccessPointClock{})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			if tt.wantErrIs != nil {
				assert.ErrorIs(t, err, tt.wantErrIs)
			}
			assert.Len(t, client.createInputs, 1)
			assert.Zero(t, client.describeCalls)
		})
	}
}

func TestDescribeAccessPoint(t *testing.T) {
	tests := []struct {
		name      string
		results   []accessPointDescribeResult
		wantState efstypes.LifeCycleState
		wantErr   string
		wantGone  bool
	}{
		{name: "typed not found", results: []accessPointDescribeResult{{
			err: &efstypes.AccessPointNotFound{},
		}}, wantGone: true},
		{name: "empty", results: []accessPointDescribeResult{{
			output: &efssdk.DescribeAccessPointsOutput{},
		}}, wantGone: true},
		{name: "deleted", results: []accessPointDescribeResult{{
			output: accessPointDescribeOutput(efstypes.LifeCycleStateDeleted),
		}}, wantGone: true},
		{name: "one error-state result remains readable", results: []accessPointDescribeResult{{
			output: accessPointDescribeOutput(efstypes.LifeCycleStateError),
		}}, wantState: efstypes.LifeCycleStateError},
		{name: "multiple", results: []accessPointDescribeResult{{
			output: &efssdk.DescribeAccessPointsOutput{
				AccessPoints: []efstypes.AccessPointDescription{
					accessPointDescription(efstypes.LifeCycleStateAvailable),
					accessPointDescription(efstypes.LifeCycleStateAvailable),
				},
			},
		}}, wantErr: "returned 2 results"},
		{name: "pagination", results: []accessPointDescribeResult{
			{output: &efssdk.DescribeAccessPointsOutput{NextToken: aws.String("next")}},
			{output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable)},
		}, wantState: efstypes.LifeCycleStateAvailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeAccessPointClient{describeResults: tt.results}

			description, err := describeAccessPoint(
				context.Background(), client, testAccessPointID)

			switch {
			case tt.wantGone:
				assert.ErrorIs(t, err, runtime.ErrNotFound)
			case tt.wantErr != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.wantState, description.LifeCycleState)
			}
			for _, input := range client.describeInputs {
				assert.Equal(t, testAccessPointID, aws.ToString(input.AccessPointId))
			}
		})
	}
}

func TestWaitAccessPointAvailableCadenceAndTarget(t *testing.T) {
	client := &fakeAccessPointClient{describeResults: []accessPointDescribeResult{
		{output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating)},
		{output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating)},
		{output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating)},
		{output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating)},
		{output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating)},
		{output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating)},
		{output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating)},
		{output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable)},
	}}
	clock := &fakeAccessPointClock{}

	description, err := waitAccessPointAvailable(
		context.Background(), client, testAccessPointID, time.Minute, clock)

	require.NoError(t, err)
	assert.Equal(t, efstypes.LifeCycleStateAvailable, description.LifeCycleState)
	assert.Equal(t, []time.Duration{
		200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond,
		1600 * time.Millisecond, 3200 * time.Millisecond, 6400 * time.Millisecond,
		10 * time.Second,
	}, clock.sleeps)
}

func TestWaitAccessPointAvailableNotFoundLimitAndReset(t *testing.T) {
	notFound := accessPointDescribeResult{err: &efstypes.AccessPointNotFound{}}
	t.Run("twenty absences are tolerated", func(t *testing.T) {
		results := slices.Repeat([]accessPointDescribeResult{notFound}, 20)
		results = append(results, accessPointDescribeResult{
			output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable),
		})
		client := &fakeAccessPointClient{describeResults: results}
		_, err := waitAccessPointAvailable(
			context.Background(), client, testAccessPointID, 10*time.Minute,
			&fakeAccessPointClock{})
		require.NoError(t, err)
	})
	t.Run("twenty-first absence fails", func(t *testing.T) {
		client := &fakeAccessPointClient{
			describeResults: slices.Repeat([]accessPointDescribeResult{notFound}, 21),
		}
		_, err := waitAccessPointAvailable(
			context.Background(), client, testAccessPointID, 10*time.Minute,
			&fakeAccessPointClock{})
		assert.ErrorIs(t, err, runtime.ErrNotFound)
		assert.Equal(t, 21, client.describeCalls)
	})
	t.Run("found observation resets count", func(t *testing.T) {
		results := slices.Repeat([]accessPointDescribeResult{notFound}, 20)
		results = append(results, accessPointDescribeResult{
			output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating),
		})
		results = append(results, slices.Repeat([]accessPointDescribeResult{notFound}, 20)...)
		results = append(results, accessPointDescribeResult{
			output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable),
		})
		client := &fakeAccessPointClient{describeResults: results}
		_, err := waitAccessPointAvailable(
			context.Background(), client, testAccessPointID, 10*time.Minute,
			&fakeAccessPointClock{})
		require.NoError(t, err)
	})
}

func TestWaitAccessPointAvailableRejectsUnexpectedStates(t *testing.T) {
	states := []efstypes.LifeCycleState{
		efstypes.LifeCycleStateUpdating,
		efstypes.LifeCycleStateDeleting,
		efstypes.LifeCycleStateError,
		"future",
	}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			client := &fakeAccessPointClient{describeResults: []accessPointDescribeResult{{
				output: accessPointDescribeOutput(state),
			}}}
			_, err := waitAccessPointAvailable(
				context.Background(), client, testAccessPointID, time.Minute,
				&fakeAccessPointClock{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), string(state))
		})
	}
}

func TestAccessPointWaitTimeout(t *testing.T) {
	client := &fakeAccessPointClient{describeResults: []accessPointDescribeResult{{
		output: accessPointDescribeOutput(efstypes.LifeCycleStateCreating),
	}}}
	clock := &fakeAccessPointClock{}

	_, err := waitAccessPointAvailable(
		context.Background(), client, testAccessPointID, 1200*time.Millisecond, clock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Equal(t, 1200*time.Millisecond, clock.elapsed())
}

func TestAccessPointDelete(t *testing.T) {
	tests := []struct {
		name            string
		deleteErr       error
		describeResults []accessPointDescribeResult
		wantErr         string
		wantDescribe    int
	}{
		{name: "typed not found", deleteErr: &efstypes.AccessPointNotFound{}},
		{name: "transitions to absent", describeResults: []accessPointDescribeResult{
			{output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable)},
			{output: accessPointDescribeOutput(efstypes.LifeCycleStateDeleting)},
			{output: &efssdk.DescribeAccessPointsOutput{}},
		}, wantDescribe: 3},
		{name: "deleted state", describeResults: []accessPointDescribeResult{{
			output: accessPointDescribeOutput(efstypes.LifeCycleStateDeleted),
		}}, wantDescribe: 1},
		{name: "delete error", deleteErr: errors.New("boom"), wantErr: "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeAccessPointClient{
				deleteErr: tt.deleteErr, describeResults: tt.describeResults,
			}
			err := (&AccessPointResource{}).delete(
				context.Background(), client, testAccessPointID, &fakeAccessPointClock{})
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Len(t, client.deleteInputs, 1)
			assert.Equal(t, tt.wantDescribe, client.describeCalls)
		})
	}
}

func TestWaitAccessPointDeletedRejectsUnexpectedStates(t *testing.T) {
	states := []efstypes.LifeCycleState{
		efstypes.LifeCycleStateCreating,
		efstypes.LifeCycleStateUpdating,
		efstypes.LifeCycleStateError,
		"future",
	}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			client := &fakeAccessPointClient{describeResults: []accessPointDescribeResult{{
				output: accessPointDescribeOutput(state),
			}}}
			err := waitAccessPointDeleted(
				context.Background(), client, testAccessPointID, time.Minute,
				&fakeAccessPointClock{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), string(state))
		})
	}
}

func TestWaitAccessPointDeletedTimesOut(t *testing.T) {
	client := &fakeAccessPointClient{describeResults: []accessPointDescribeResult{{
		output: accessPointDescribeOutput(efstypes.LifeCycleStateDeleting),
	}}}
	clock := &fakeAccessPointClock{}

	err := waitAccessPointDeleted(
		context.Background(), client, testAccessPointID, 1200*time.Millisecond, clock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Equal(t, 1200*time.Millisecond, clock.elapsed())
}

func TestAccessPointUpdateSkipsTagsWhenSourceIsUnchanged(t *testing.T) {
	tags := map[string]string{"env": "prod"}
	resource := &AccessPointResource{FileSystemId: testAccessPointFileSystemID, Tags: &tags}
	client := &fakeAccessPointClient{describeResults: []accessPointDescribeResult{{
		output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable),
	}}}
	prior := runtime.Prior[AccessPointResource, *AccessPointResourceOutput, *awsCfg]{
		Inputs:  AccessPointResource{FileSystemId: testAccessPointFileSystemID, Tags: &tags},
		Outputs: &AccessPointResourceOutput{AccessPointId: testAccessPointID},
	}

	_, err := resource.update(context.Background(), client, prior)

	require.NoError(t, err)
	assert.Equal(t, []string{"describe"}, client.calls)
}

func TestAccessPointUpdateReconcilesPaginatedTags(t *testing.T) {
	priorTags := map[string]string{"keep": "old", "drop": "yes"}
	desiredTags := map[string]string{"keep": "new", "add": "yes"}
	resource := &AccessPointResource{FileSystemId: testAccessPointFileSystemID, Tags: &desiredTags}
	client := &fakeAccessPointClient{
		listResults: []accessPointListTagsResult{
			{output: &efssdk.ListTagsForResourceOutput{
				Tags: []efstypes.Tag{
					{Key: aws.String("keep"), Value: aws.String("old")},
					{Key: aws.String("aws:owner"), Value: aws.String("system")},
				},
				NextToken: aws.String("next"),
			}},
			{output: &efssdk.ListTagsForResourceOutput{Tags: []efstypes.Tag{
				{Key: aws.String("drop"), Value: aws.String("yes")},
			}}},
		},
		describeResults: []accessPointDescribeResult{{
			output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable),
		}},
	}
	prior := runtime.Prior[AccessPointResource, *AccessPointResourceOutput, *awsCfg]{
		Inputs:  AccessPointResource{FileSystemId: testAccessPointFileSystemID, Tags: &priorTags},
		Outputs: &AccessPointResourceOutput{AccessPointId: testAccessPointID},
	}

	_, err := resource.update(context.Background(), client, prior)

	require.NoError(t, err)
	assert.Equal(t, []string{"list-tags", "list-tags", "untag", "tag", "describe"},
		client.calls)
	require.Len(t, client.untagInputs, 1)
	assert.Equal(t, []string{"drop"}, client.untagInputs[0].TagKeys)
	require.Len(t, client.tagInputs, 1)
	assert.Equal(t, []efstypes.Tag{
		{Key: aws.String("add"), Value: aws.String("yes")},
		{Key: aws.String("keep"), Value: aws.String("new")},
	}, client.tagInputs[0].Tags)
}

func TestAccessPointUpdateClearsAllUserTags(t *testing.T) {
	priorTags := map[string]string{"a": "1", "b": "2"}
	desiredTags := map[string]string{}
	resource := &AccessPointResource{FileSystemId: testAccessPointFileSystemID, Tags: &desiredTags}
	client := &fakeAccessPointClient{
		listResults: []accessPointListTagsResult{{
			output: &efssdk.ListTagsForResourceOutput{Tags: []efstypes.Tag{
				{Key: aws.String("b"), Value: aws.String("2")},
				{Key: aws.String("aws:owner"), Value: aws.String("system")},
				{Key: aws.String("a"), Value: aws.String("1")},
			}},
		}},
		describeResults: []accessPointDescribeResult{{
			output: accessPointDescribeOutput(efstypes.LifeCycleStateAvailable),
		}},
	}
	prior := runtime.Prior[AccessPointResource, *AccessPointResourceOutput, *awsCfg]{
		Inputs:  AccessPointResource{FileSystemId: testAccessPointFileSystemID, Tags: &priorTags},
		Outputs: &AccessPointResourceOutput{AccessPointId: testAccessPointID},
	}

	_, err := resource.update(context.Background(), client, prior)

	require.NoError(t, err)
	assert.Equal(t, []string{"list-tags", "untag", "describe"}, client.calls)
	assert.Equal(t, []string{"a", "b"}, client.untagInputs[0].TagKeys)
	assert.Empty(t, client.tagInputs)
}

func TestAccessPointTagErrorsStopReconciliation(t *testing.T) {
	tests := []struct {
		name      string
		client    *fakeAccessPointClient
		wantCalls []string
	}{
		{
			name: "list error",
			client: &fakeAccessPointClient{listResults: []accessPointListTagsResult{{
				err: errors.New("list failed"),
			}}},
			wantCalls: []string{"list-tags"},
		},
		{
			name: "untag error",
			client: &fakeAccessPointClient{
				listResults: []accessPointListTagsResult{{
					output: &efssdk.ListTagsForResourceOutput{Tags: []efstypes.Tag{{
						Key: aws.String("drop"), Value: aws.String("yes"),
					}}},
				}},
				untagErr: errors.New("untag failed"),
			},
			wantCalls: []string{"list-tags", "untag"},
		},
		{
			name: "tag error",
			client: &fakeAccessPointClient{
				listResults: []accessPointListTagsResult{{
					output: &efssdk.ListTagsForResourceOutput{},
				}},
				tagErr: errors.New("tag failed"),
			},
			wantCalls: []string{"list-tags", "tag"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := syncAccessPointTags(context.Background(), tt.client, testAccessPointID,
				map[string]string{"add": "yes"})
			require.Error(t, err)
			assert.Equal(t, tt.wantCalls, tt.client.calls)
		})
	}
}

func testAccessPointOutput() *AccessPointResourceOutput {
	return &AccessPointResourceOutput{
		AccessPointId: testAccessPointID,
		Arn: "arn:aws:elasticfilesystem:us-east-1:123456789012:" +
			"access-point/" + testAccessPointID,
		OwnerId: "123456789012",
	}
}

func accessPointDescription(state efstypes.LifeCycleState) efstypes.AccessPointDescription {
	return efstypes.AccessPointDescription{
		AccessPointId: aws.String(testAccessPointID),
		AccessPointArn: aws.String("arn:aws:elasticfilesystem:us-east-1:123456789012:" +
			"access-point/" + testAccessPointID),
		FileSystemId:   aws.String(testAccessPointFileSystemID),
		LifeCycleState: state,
		OwnerId:        aws.String("123456789012"),
	}
}

func accessPointDescribeOutput(state efstypes.LifeCycleState) *efssdk.DescribeAccessPointsOutput {
	return &efssdk.DescribeAccessPointsOutput{
		AccessPoints: []efstypes.AccessPointDescription{accessPointDescription(state)},
	}
}

type accessPointDescribeResult struct {
	output *efssdk.DescribeAccessPointsOutput
	err    error
}

type accessPointListTagsResult struct {
	output *efssdk.ListTagsForResourceOutput
	err    error
}

type fakeAccessPointClock struct {
	now      time.Time
	sleeps   []time.Duration
	sleepErr error
}

func (c *fakeAccessPointClock) Now() time.Time { return c.now }

func (c *fakeAccessPointClock) Sleep(
	_ context.Context,
	delay time.Duration,
) error {
	if c.sleepErr != nil {
		return c.sleepErr
	}
	c.sleeps = append(c.sleeps, delay)
	c.now = c.now.Add(delay)
	return nil
}

func (c *fakeAccessPointClock) elapsed() time.Duration {
	var total time.Duration
	for _, delay := range c.sleeps {
		total += delay
	}
	return total
}

type fakeAccessPointClient struct {
	createInputs    []*efssdk.CreateAccessPointInput
	createOutput    *efssdk.CreateAccessPointOutput
	createErr       error
	describeInputs  []*efssdk.DescribeAccessPointsInput
	describeResults []accessPointDescribeResult
	describeCalls   int
	listInputs      []*efssdk.ListTagsForResourceInput
	listResults     []accessPointListTagsResult
	listCalls       int
	tagInputs       []*efssdk.TagResourceInput
	tagErr          error
	untagInputs     []*efssdk.UntagResourceInput
	untagErr        error
	deleteInputs    []*efssdk.DeleteAccessPointInput
	deleteErr       error
	calls           []string
}

func (c *fakeAccessPointClient) CreateAccessPoint(
	_ context.Context,
	in *efssdk.CreateAccessPointInput,
	_ ...func(*efssdk.Options),
) (*efssdk.CreateAccessPointOutput, error) {
	c.calls = append(c.calls, "create")
	copyInput := *in
	copyInput.Tags = slices.Clone(in.Tags)
	c.createInputs = append(c.createInputs, &copyInput)
	return c.createOutput, c.createErr
}

func (c *fakeAccessPointClient) DescribeAccessPoints(
	_ context.Context,
	in *efssdk.DescribeAccessPointsInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DescribeAccessPointsOutput, error) {
	c.calls = append(c.calls, "describe")
	copyInput := *in
	c.describeInputs = append(c.describeInputs, &copyInput)
	index := c.describeCalls
	c.describeCalls++
	if len(c.describeResults) == 0 {
		return &efssdk.DescribeAccessPointsOutput{}, nil
	}
	if index >= len(c.describeResults) {
		index = len(c.describeResults) - 1
	}
	result := c.describeResults[index]
	return result.output, result.err
}

func (c *fakeAccessPointClient) ListTagsForResource(
	_ context.Context,
	in *efssdk.ListTagsForResourceInput,
	_ ...func(*efssdk.Options),
) (*efssdk.ListTagsForResourceOutput, error) {
	c.calls = append(c.calls, "list-tags")
	copyInput := *in
	c.listInputs = append(c.listInputs, &copyInput)
	index := c.listCalls
	c.listCalls++
	if len(c.listResults) == 0 {
		return &efssdk.ListTagsForResourceOutput{}, nil
	}
	if index >= len(c.listResults) {
		index = len(c.listResults) - 1
	}
	result := c.listResults[index]
	return result.output, result.err
}

func (c *fakeAccessPointClient) TagResource(
	_ context.Context,
	in *efssdk.TagResourceInput,
	_ ...func(*efssdk.Options),
) (*efssdk.TagResourceOutput, error) {
	c.calls = append(c.calls, "tag")
	copyInput := *in
	copyInput.Tags = slices.Clone(in.Tags)
	c.tagInputs = append(c.tagInputs, &copyInput)
	return &efssdk.TagResourceOutput{}, c.tagErr
}

func (c *fakeAccessPointClient) UntagResource(
	_ context.Context,
	in *efssdk.UntagResourceInput,
	_ ...func(*efssdk.Options),
) (*efssdk.UntagResourceOutput, error) {
	c.calls = append(c.calls, "untag")
	copyInput := *in
	copyInput.TagKeys = slices.Clone(in.TagKeys)
	c.untagInputs = append(c.untagInputs, &copyInput)
	return &efssdk.UntagResourceOutput{}, c.untagErr
}

func (c *fakeAccessPointClient) DeleteAccessPoint(
	_ context.Context,
	in *efssdk.DeleteAccessPointInput,
	_ ...func(*efssdk.Options),
) (*efssdk.DeleteAccessPointOutput, error) {
	c.calls = append(c.calls, "delete")
	copyInput := *in
	c.deleteInputs = append(c.deleteInputs, &copyInput)
	return &efssdk.DeleteAccessPointOutput{}, c.deleteErr
}

var _ accessPointClient = (*fakeAccessPointClient)(nil)
