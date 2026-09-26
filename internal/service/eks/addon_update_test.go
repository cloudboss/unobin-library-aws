package eks

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddonTagOnlyUpdateRunsRemoveBeforeUpsertAndSkipsUpdateAPI(t *testing.T) {
	priorTags := map[string]string{"change": "old", "drop": "yes"}
	currentTags := map[string]string{"change": "new", "add": "yes"}
	prior := validAddonResource()
	prior.Tags = &priorTags
	current := prior
	current.Tags = &currentTags
	client := &fakeAddonClient{
		listTags: map[string]string{
			"change": "old", "drop": "yes", "aws:managed": "keep",
		},
		describeResults: []addonDescribeResult{{
			output: addonDescribe(ekstypes.AddonStatusActive),
		}},
	}
	tokenCalls := 0

	_, err := current.updateAddon(
		context.Background(),
		client,
		addonPrior(prior),
		newFakeClusterClock(),
		func() (string, error) { tokenCalls++; return "token", nil },
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"list-tags", "untag", "tag", "describe"}, client.calls)
	assert.Zero(t, tokenCalls)
	assert.Empty(t, client.updateInputs)
	require.Len(t, client.untagInputs, 1)
	assert.Equal(t, []string{"drop"}, client.untagInputs[0].TagKeys)
	require.Len(t, client.tagInputs, 1)
	assert.Equal(t, map[string]string{"add": "yes", "change": "new"},
		client.tagInputs[0].Tags)
}

func TestAddonTagUpdateClearsEveryNonSystemTag(t *testing.T) {
	priorTags := map[string]string{"one": "1", "two": "2"}
	prior := validAddonResource()
	prior.Tags = &priorTags
	current := prior
	current.Tags = nil
	client := &fakeAddonClient{
		listTags: map[string]string{"two": "2", "one": "1", "aws:managed": "keep"},
		describeResults: []addonDescribeResult{{
			output: addonDescribe(ekstypes.AddonStatusActive),
		}},
	}

	_, err := current.updateAddon(
		context.Background(), client, addonPrior(prior), newFakeClusterClock(),
		fixedAddonToken("token"),
	)

	require.NoError(t, err)
	require.Len(t, client.untagInputs, 1)
	assert.Equal(t, []string{"one", "two"}, client.untagInputs[0].TagKeys)
	assert.Empty(t, client.tagInputs)
	assert.Empty(t, client.updateInputs)
}

func TestAddonUpdateTagsFirstThenSendsOneMutableRequest(t *testing.T) {
	priorTags := map[string]string{"old": "tag"}
	currentTags := map[string]string{"new": "tag"}
	prior := validAddonResource()
	prior.Tags = &priorTags
	prior.ServiceAccountRoleARN = stringPointer(
		"arn:aws:iam::123456789012:role/old",
	)
	current := prior
	current.Tags = &currentTags
	current.AddonVersion = stringPointer("v1.12.0-eksbuild.1")
	current.ConfigurationValues = stringPointer("{}")
	current.ServiceAccountRoleARN = nil
	client := &fakeAddonClient{
		listTags: map[string]string{"old": "tag"},
		updateResults: []addonUpdateResult{{output: &eks.UpdateAddonOutput{
			Update: &ekstypes.Update{Id: aws.String("update-1")},
		}}},
		describeUpdateResults: []addonDescribeUpdateResult{{
			output: addonUpdateDescription("update-1", ekstypes.UpdateStatusSuccessful),
		}},
		describeResults: []addonDescribeResult{{
			output: addonDescribe(ekstypes.AddonStatusActive),
		}},
	}

	_, err := current.updateAddon(
		context.Background(), client, addonPrior(prior), newFakeClusterClock(),
		fixedAddonToken("22222222-2222-4222-8222-222222222222"),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"list-tags", "untag", "tag", "update", "describe-update", "describe",
	}, client.calls)
	require.Len(t, client.updateInputs, 1)
	input := client.updateInputs[0]
	assert.Equal(t, "v1.12.0-eksbuild.1", aws.ToString(input.AddonVersion))
	assert.Equal(t, "{}", aws.ToString(input.ConfigurationValues))
	assert.NotNil(t, input.ServiceAccountRoleArn)
	assert.Empty(t, aws.ToString(input.ServiceAccountRoleArn))
	assert.Equal(t, "22222222-2222-4222-8222-222222222222",
		aws.ToString(input.ClientRequestToken))
}

func TestAddonUpdateConflictModifierAloneIsNoOp(t *testing.T) {
	prior := validAddonResource()
	current := prior
	current.ResolveConflictsOnUpdate = stringPointer("PRESERVE")
	client := &fakeAddonClient{describeResults: []addonDescribeResult{{
		output: addonDescribe(ekstypes.AddonStatusActive),
	}}}

	_, err := current.updateAddon(
		context.Background(), client, addonPrior(prior), newFakeClusterClock(),
		fixedAddonToken("token"),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"describe"}, client.calls)
	assert.Empty(t, client.updateInputs)
}

func TestAddonUpdateRequiresUpdateAndID(t *testing.T) {
	for _, response := range []*eks.UpdateAddonOutput{
		nil,
		{},
		{Update: &ekstypes.Update{}},
	} {
		prior := validAddonResource()
		current := prior
		current.AddonVersion = stringPointer("v1.12.0-eksbuild.1")
		client := &fakeAddonClient{updateResults: []addonUpdateResult{{output: response}}}

		_, err := current.updateAddon(
			context.Background(), client, addonPrior(prior), newFakeClusterClock(),
			fixedAddonToken("token"),
		)

		require.Error(t, err)
		assert.ErrorContains(t, err, "response has no update ID")
	}
}

func TestAddonUpdateFailureIncludesConflictHintUnlessOverwrite(t *testing.T) {
	for _, test := range []struct {
		mode     *string
		wantHint bool
	}{
		{mode: nil, wantHint: true},
		{mode: stringPointer("NONE"), wantHint: true},
		{mode: stringPointer("PRESERVE"), wantHint: true},
		{mode: stringPointer("OVERWRITE"), wantHint: false},
	} {
		prior := validAddonResource()
		current := prior
		current.AddonVersion = stringPointer("v1.12.0-eksbuild.1")
		current.ResolveConflictsOnUpdate = test.mode
		client := &fakeAddonClient{
			updateResults: []addonUpdateResult{{output: &eks.UpdateAddonOutput{
				Update: &ekstypes.Update{Id: aws.String("update-1")},
			}}},
			describeUpdateResults: []addonDescribeUpdateResult{{
				output: addonUpdateDescription("update-1", ekstypes.UpdateStatusFailed),
			}},
		}

		_, err := current.updateAddon(
			context.Background(), client, addonPrior(prior), newFakeClusterClock(),
			fixedAddonToken("token"),
		)

		require.Error(t, err)
		if test.wantHint {
			assert.ErrorContains(t, err, "resolve-conflicts-on-update to OVERWRITE")
		} else {
			assert.NotContains(t, err.Error(), "consider setting")
		}
	}
}

func TestAddonUpdatePropagatesAPIError(t *testing.T) {
	sentinel := errors.New("denied")
	prior := validAddonResource()
	current := prior
	current.AddonVersion = stringPointer("v1.12.0-eksbuild.1")
	client := &fakeAddonClient{updateResults: []addonUpdateResult{{err: sentinel}}}
	_, err := current.updateAddon(
		context.Background(), client, addonPrior(prior), newFakeClusterClock(),
		fixedAddonToken("token"),
	)
	assert.ErrorIs(t, err, sentinel)
}

func addonPrior(
	inputs AddonResource,
) runtime.Prior[AddonResource, *AddonResourceOutput, *awsCfg] {
	return runtime.Prior[AddonResource, *AddonResourceOutput, *awsCfg]{
		Inputs: inputs,
		Outputs: &AddonResourceOutput{
			ClusterName: "prior-cluster", AddonName: "prior-addon", ARN: "arn:recorded",
		},
		Observed: &AddonResourceOutput{ARN: "arn:observed"},
	}
}
