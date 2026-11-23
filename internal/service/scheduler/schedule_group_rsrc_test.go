package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

const groupTestRegion = "us-west-2"

type groupTestReply struct {
	output *schedulerapi.GetScheduleGroupOutput
	err    error
}

type groupTestClient struct {
	t         *testing.T
	calls     []any
	gets      []groupTestReply
	get       func() groupTestReply
	listTags  []schedulertypes.Tag
	createErr error
	deleteErr error
	listErr   error
	tagErr    error
	untagErr  error
}

func (c *groupTestClient) CreateScheduleGroup(
	_ context.Context, input *schedulerapi.CreateScheduleGroupInput,
	_ ...func(*schedulerapi.Options),
) (*schedulerapi.CreateScheduleGroupOutput, error) {
	c.calls = append(c.calls, *input)
	return &schedulerapi.CreateScheduleGroupOutput{
		ScheduleGroupArn: aws.String("ignored create result"),
	}, c.createErr
}

func (c *groupTestClient) GetScheduleGroup(
	_ context.Context, input *schedulerapi.GetScheduleGroupInput, _ ...func(*schedulerapi.Options),
) (*schedulerapi.GetScheduleGroupOutput, error) {
	c.calls = append(c.calls, *input)
	if c.get != nil {
		reply := c.get()
		return reply.output, reply.err
	}
	require.NotEmpty(c.t, c.gets, "unexpected GetScheduleGroup")
	reply := c.gets[0]
	c.gets = c.gets[1:]
	return reply.output, reply.err
}

func (c *groupTestClient) DeleteScheduleGroup(
	_ context.Context, input *schedulerapi.DeleteScheduleGroupInput,
	_ ...func(*schedulerapi.Options),
) (*schedulerapi.DeleteScheduleGroupOutput, error) {
	c.calls = append(c.calls, *input)
	return nil, c.deleteErr
}

func (c *groupTestClient) ListTagsForResource(
	_ context.Context, input *schedulerapi.ListTagsForResourceInput,
	_ ...func(*schedulerapi.Options),
) (*schedulerapi.ListTagsForResourceOutput, error) {
	c.calls = append(c.calls, *input)
	return &schedulerapi.ListTagsForResourceOutput{Tags: c.listTags}, c.listErr
}

func (c *groupTestClient) TagResource(
	_ context.Context, input *schedulerapi.TagResourceInput, _ ...func(*schedulerapi.Options),
) (*schedulerapi.TagResourceOutput, error) {
	c.calls = append(c.calls, *input)
	return nil, c.tagErr
}

func (c *groupTestClient) UntagResource(
	_ context.Context, input *schedulerapi.UntagResourceInput, _ ...func(*schedulerapi.Options),
) (*schedulerapi.UntagResourceOutput, error) {
	c.calls = append(c.calls, *input)
	return nil, c.untagErr
}

type groupTestClock struct {
	elapsed time.Duration
	sleeps  []time.Duration
	cancel  context.CancelCauseFunc
}

func (c *groupTestClock) Now() time.Time {
	return time.Unix(0, 0).Add(c.elapsed)
}

func (c *groupTestClock) Sleep(ctx context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	if c.cancel != nil {
		c.cancel(errGroupTestCanceled)
		return context.Cause(ctx)
	}
	c.elapsed += d
	return nil
}

var errGroupTestCanceled = errors.New("wait canceled by test")

func groupTestIdentity(name, region string) ScheduleGroupResourceOutput {
	partition := "aws"
	if strings.HasPrefix(region, "us-iso-") {
		partition = "aws-iso"
	}
	return ScheduleGroupResourceOutput{
		ARN: fmt.Sprintf("arn:%s:scheduler:%s:123456789012:schedule-group/%s",
			partition, region, name),
		Name: name, Region: region,
	}
}

func groupTestGet(name string) schedulerapi.GetScheduleGroupInput {
	return schedulerapi.GetScheduleGroupInput{Name: aws.String(name)}
}

func groupTestTag(key, value string) schedulertypes.Tag {
	return schedulertypes.Tag{Key: aws.String(key), Value: aws.String(value)}
}

func groupTestActive(id ScheduleGroupResourceOutput) groupTestReply {
	return groupTestReply{output: &schedulerapi.GetScheduleGroupOutput{
		Arn: aws.String(id.ARN), Name: aws.String(id.Name),
		State: schedulertypes.ScheduleGroupStateActive,
	}}
}

func groupTestMissing() error {
	return fmt.Errorf("wrapped Scheduler response: %w", &schedulertypes.ResourceNotFoundException{})
}

func TestScheduleGroupCreateRequests(t *testing.T) {
	prefix := strings.Repeat("p", 38)
	suffix := strings.Repeat("ab", 13)
	defaultName := "unobin-" + suffix
	for _, tc := range []struct {
		label  string
		input  ScheduleGroupResource
		name   string
		tags   []schedulertypes.Tag
		prefix *string
	}{
		{"exact", ScheduleGroupResource{Name: aws.String("Group_1.2-")}, "Group_1.2-", nil, nil},
		{"prefix", ScheduleGroupResource{NamePrefix: &prefix}, prefix + suffix, nil, &prefix},
		{"generated", ScheduleGroupResource{}, defaultName, nil, aws.String("unobin-")},
		{
			label: "empty tags",
			input: ScheduleGroupResource{Tags: &map[string]string{}},
			name:  defaultName, prefix: aws.String("unobin-"),
		},
		{
			label: "reserved tags",
			input: ScheduleGroupResource{Tags: &map[string]string{"aws:owner": "aws"}},
			name:  defaultName, prefix: aws.String("unobin-"),
		},
		{
			label: "user tags",
			input: ScheduleGroupResource{
				Tags: &map[string]string{"z": "2", "a": "", "aws:owner": "aws"},
			},
			name: defaultName, prefix: aws.String("unobin-"),
			tags: []schedulertypes.Tag{groupTestTag("a", ""), groupTestTag("z", "2")},
		},
	} {
		t.Run(tc.label, func(t *testing.T) {
			id := groupTestIdentity(tc.name, groupTestRegion)
			active := groupTestActive(id)
			client := &groupTestClient{t: t, gets: []groupTestReply{active, active, active}}
			random := bytes.NewReader(bytes.Repeat([]byte{0xab}, 13))
			clock := &groupTestClock{}
			out, err := tc.input.createScheduleGroup(t.Context(), client, id.Region, random, clock)
			require.NoError(t, err)
			require.Equal(t, id.ARN, out.ARN)
			require.Equal(t, tc.name, out.Name)
			require.Equal(t, tc.prefix, out.NamePrefix)
			get := groupTestGet(id.Name)
			require.Equal(t, []any{
				schedulerapi.CreateScheduleGroupInput{Name: aws.String(id.Name), Tags: tc.tags},
				get, get, get, schedulerapi.ListTagsForResourceInput{ResourceArn: &id.ARN},
			}, client.calls)
			require.Equal(t, []time.Duration{100 * time.Millisecond}, clock.sleeps)
		})
	}
}

func TestScheduleGroupCreateWaitReadsFinalMetadata(t *testing.T) {
	name := "Exact-" + strings.Repeat("Aa", 13)
	id := groupTestIdentity(name, groupTestRegion)
	active := groupTestActive(id)
	deleting := groupTestActive(id)
	deleting.output.State = schedulertypes.ScheduleGroupStateDeleting
	final := groupTestActive(id)
	final.output.State = schedulertypes.ScheduleGroupStateDeleting
	created := time.Date(2024, 5, 6, 7, 8, 9, 200_000_000, time.UTC)
	final.output.CreationDate = &created
	client := &groupTestClient{
		t:    t,
		gets: []groupTestReply{active, {err: groupTestMissing()}, deleting, active, final},
		listTags: []schedulertypes.Tag{
			groupTestTag("keep", ""), groupTestTag("aws:managed", "1"),
		},
	}
	clock := &groupTestClock{}
	r := ScheduleGroupResource{Name: &name}
	out, err := r.createScheduleGroup(t.Context(), client, id.Region, nil, clock)
	require.NoError(t, err)
	require.Equal(t, &ScheduleGroupResourceOutput{
		ARN: id.ARN, Name: name, NamePrefix: aws.String("Exact-"),
		CreationDate: "2024-05-06T07:08:09Z", LastModificationDate: "0001-01-01T00:00:00Z",
		State: "DELETING", Region: id.Region, Tags: map[string]string{"keep": ""},
	}, out)
	get := groupTestGet(name)
	require.Equal(t, []any{
		schedulerapi.CreateScheduleGroupInput{Name: &name}, get, get, get, get, get,
		schedulerapi.ListTagsForResourceInput{ResourceArn: &id.ARN},
	}, client.calls)
	require.Equal(t, []time.Duration{100_000_000, 100_000_000, 100_000_000}, clock.sleeps)
}

func TestScheduleGroupCreateAbsenceLimit(t *testing.T) {
	id := groupTestIdentity("group", groupTestRegion)
	active := groupTestActive(id)
	deleting := groupTestActive(id)
	deleting.output.State = schedulertypes.ScheduleGroupStateDeleting
	missing := groupTestReply{err: groupTestMissing()}
	for _, tc := range []struct {
		name   string
		gets   []groupTestReply
		errMsg string
	}{
		{"twenty", append(slices.Repeat([]groupTestReply{missing}, 20), active, active, active), ""},
		{
			"reset after present",
			slices.Concat([]groupTestReply{active},
				slices.Repeat([]groupTestReply{missing}, 20), []groupTestReply{deleting},
				slices.Repeat([]groupTestReply{missing}, 20), []groupTestReply{active, active}),
			"",
		},
		{"twenty-one", slices.Repeat([]groupTestReply{missing}, 21), "21 consecutive"},
		{"final absence", []groupTestReply{active, active, missing}, "final read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ScheduleGroupResource{Name: &id.Name}
			client := &groupTestClient{t: t, gets: tc.gets}
			clock := &groupTestClock{}
			out, err := r.createScheduleGroup(t.Context(), client, id.Region, nil, clock)
			expected := []any{schedulerapi.CreateScheduleGroupInput{Name: &id.Name}}
			expected = append(expected, slices.Repeat([]any{groupTestGet(id.Name)}, len(tc.gets))...)
			if tc.errMsg != "" {
				require.ErrorContains(t, err, tc.errMsg)
				require.NotErrorIs(t, err, runtime.ErrNotFound)
				require.Nil(t, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, id.ARN, out.ARN)
				expected = append(expected, schedulerapi.ListTagsForResourceInput{
					ResourceArn: &id.ARN,
				})
			}
			require.Equal(t, expected, client.calls)
			if tc.name == "twenty" {
				require.Equal(t, []time.Duration{
					200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond,
					1600 * time.Millisecond, 3200 * time.Millisecond, 6400 * time.Millisecond,
				}, clock.sleeps[:6])
				require.Equal(t, slices.Repeat([]time.Duration{10 * time.Second}, 15),
					clock.sleeps[6:])
			}
		})
	}
}

func TestScheduleGroupWaitFailures(t *testing.T) {
	id := groupTestIdentity("group", groupTestRegion)
	active := groupTestActive(id)
	pending := groupTestActive(id)
	pending.output.State = schedulertypes.ScheduleGroupStateDeleting
	r := ScheduleGroupResource{Name: &id.Name}
	denied := errors.New("get denied")
	for _, deleting := range []bool{false, true} {
		for _, kind := range []string{"timeout", "canceled", "poll error"} {
			t.Run(fmt.Sprintf("delete=%t/%s", deleting, kind), func(t *testing.T) {
				clock := &groupTestClock{}
				client := &groupTestClient{t: t}
				ctx, cancel := context.WithCancelCause(t.Context())
				defer cancel(nil)
				reply := pending
				expectedErr := context.DeadlineExceeded
				switch kind {
				case "canceled":
					clock.cancel = cancel
					expectedErr = errGroupTestCanceled
				case "poll error":
					reply = groupTestReply{err: denied}
					expectedErr = denied
				}
				client.get = func() groupTestReply { return reply }
				var err error
				var mutation any = schedulerapi.CreateScheduleGroupInput{Name: &id.Name}
				if deleting {
					mutation = schedulerapi.DeleteScheduleGroupInput{Name: &id.Name}
					err = deleteScheduleGroup(ctx, client, id, clock)
				} else {
					_, err = r.createScheduleGroup(ctx, client, id.Region, nil, clock)
				}
				require.ErrorIs(t, err, expectedErr)
				polls := 1
				if kind == "timeout" {
					expected := []time.Duration{
						200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond,
						1600 * time.Millisecond, 3200 * time.Millisecond, 6400 * time.Millisecond,
					}
					expected = append(expected, slices.Repeat([]time.Duration{10 * time.Second}, 28)...)
					expected = append(expected, 7400*time.Millisecond)
					require.Equal(t, expected, clock.sleeps)
					require.Equal(t, 5*time.Minute, clock.elapsed)
					polls = len(expected)
				}
				require.Equal(t, append([]any{mutation},
					slices.Repeat([]any{groupTestGet(id.Name)}, polls)...), client.calls)
			})
		}
	}
	t.Run("hold after first active at second poll", func(t *testing.T) {
		final := groupTestActive(id)
		client := &groupTestClient{t: t, gets: []groupTestReply{pending, active, pending, active, final}}
		clock := &groupTestClock{}
		_, err := r.createScheduleGroup(t.Context(), client, id.Region, nil, clock)
		require.NoError(t, err)
		require.Equal(t, slices.Repeat([]time.Duration{200 * time.Millisecond}, 3), clock.sleeps)
	})
	t.Run("conflict stops before polling", func(t *testing.T) {
		conflict := &schedulertypes.ConflictException{}
		client := &groupTestClient{t: t, createErr: conflict}
		_, err := r.createScheduleGroup(t.Context(), client, id.Region, nil, &groupTestClock{})
		require.ErrorIs(t, err, conflict)
		require.Equal(t, []any{schedulerapi.CreateScheduleGroupInput{Name: &id.Name}}, client.calls)
	})
	t.Run("name generation failure stops before create", func(t *testing.T) {
		client := &groupTestClient{t: t}
		unnamed := ScheduleGroupResource{}
		_, err := unnamed.createScheduleGroup(t.Context(), client, id.Region,
			strings.NewReader("short"), &groupTestClock{})
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.Empty(t, client.calls)
	})
}

func TestScheduleGroupReadIdentity(t *testing.T) {
	id := groupTestIdentity("group", groupTestRegion)
	denied := errors.New("get denied")
	for _, tc := range []struct {
		name   string
		reply  groupTestReply
		err    error
		errMsg string
	}{
		{"typed absence", groupTestReply{err: groupTestMissing()}, runtime.ErrNotFound, ""},
		{"nil result", groupTestReply{}, runtime.ErrNotFound, ""},
		{"nil ARN", groupTestReply{output: &schedulerapi.GetScheduleGroupOutput{}},
			runtime.ErrNotFound, ""},
		{"denied", groupTestReply{err: denied}, denied, ""},
		{"blank ARN", groupTestReply{output: &schedulerapi.GetScheduleGroupOutput{
			Arn: aws.String(""),
		}}, nil, "invalid ARN"},
		{"malformed ARN", groupTestReply{output: &schedulerapi.GetScheduleGroupOutput{
			Arn: aws.String("arn:aws:scheduler::123:schedule/group"),
		}}, nil, "does not match"},
		{"wrong group ARN", groupTestActive(groupTestIdentity("other", id.Region)), nil,
			"does not match"},
		{"wrong region ARN", groupTestActive(groupTestIdentity(id.Name, "us-east-1")), nil,
			"does not match"},
		{"wrong account ARN", groupTestReply{output: &schedulerapi.GetScheduleGroupOutput{
			Arn: aws.String(strings.Replace(id.ARN, "123456789012", "121212121212", 1)),
		}}, nil, "does not match"},
		{"contradictory name", groupTestReply{output: &schedulerapi.GetScheduleGroupOutput{
			Arn: &id.ARN, Name: aws.String("other"),
		}}, nil, "returned name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &groupTestClient{t: t, gets: []groupTestReply{tc.reply}}
			output, err := readScheduleGroup(t.Context(), client, id)
			require.Nil(t, output)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.ErrorContains(t, err, tc.errMsg)
				require.NotErrorIs(t, err, runtime.ErrNotFound)
			}
			require.Equal(t, []any{groupTestGet(id.Name)}, client.calls)
		})
	}
}

func TestScheduleGroupTagUpdate(t *testing.T) {
	id := groupTestIdentity("group-0123456789abcdefABCDEF1234", groupTestRegion)
	id.Tags = map[string]string{"saved": "fallback"}
	observed := id
	observed.Tags = map[string]string{"changed": "old", "drop": "x", "keep": "same", "aws:key": "x"}
	desired := map[string]string{"changed": "", "keep": "same", "new": "v", "aws:key": "y"}
	for _, tc := range []struct {
		name       string
		observed   *ScheduleGroupResourceOutput
		desired    *map[string]string
		unmanaged  []string
		upserts    []schedulertypes.Tag
		resultTags []schedulertypes.Tag
		wantTags   map[string]string
	}{
		{
			"drift patch", &observed, &desired, []string{"drop"},
			[]schedulertypes.Tag{groupTestTag("changed", ""), groupTestTag("new", "v")},
			[]schedulertypes.Tag{groupTestTag("changed", ""), groupTestTag("keep", "same"),
				groupTestTag("new", "v"), groupTestTag("aws:key", "x")},
			map[string]string{"changed": "", "keep": "same", "new": "v"},
		},
		{"omitted clears", &observed, nil, []string{"changed", "drop", "keep"}, nil,
			[]schedulertypes.Tag{groupTestTag("aws:key", "x")}, map[string]string{}},
		{"empty clears", nil, &map[string]string{}, []string{"saved"}, nil, nil, map[string]string{}},
		{"unchanged", nil, &id.Tags, nil, nil, []schedulertypes.Tag{groupTestTag("saved", "fallback")},
			id.Tags},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &groupTestClient{
				t: t, gets: []groupTestReply{groupTestActive(id)}, listTags: tc.resultTags,
			}
			r := ScheduleGroupResource{Tags: tc.desired}
			out, err := r.updateScheduleGroup(t.Context(), client,
				scheduleGroupPrior{Outputs: &id, Observed: tc.observed})
			require.NoError(t, err)
			require.Equal(t, id.ARN, out.ARN)
			require.Equal(t, id.Name, out.Name)
			require.Equal(t, tc.wantTags, out.Tags)
			var want []any
			if tc.unmanaged != nil {
				want = append(want, schedulerapi.UntagResourceInput{
					ResourceArn: &id.ARN, TagKeys: tc.unmanaged,
				})
			}
			if tc.upserts != nil {
				want = append(want, schedulerapi.TagResourceInput{
					ResourceArn: &id.ARN, Tags: tc.upserts,
				})
			}
			want = append(want, groupTestGet(id.Name),
				schedulerapi.ListTagsForResourceInput{ResourceArn: &id.ARN})
			require.Equal(t, want, client.calls)
		})
	}
	t.Run("observation for another ARN", func(t *testing.T) {
		client := &groupTestClient{t: t}
		r := ScheduleGroupResource{Tags: &desired}
		other := groupTestIdentity("other", groupTestRegion)
		_, err := r.updateScheduleGroup(t.Context(), client,
			scheduleGroupPrior{Outputs: &id, Observed: &other})
		require.ErrorContains(t, err, "observation identity")
		require.Empty(t, client.calls)
	})
	t.Run("absent after update", func(t *testing.T) {
		client := &groupTestClient{t: t, gets: []groupTestReply{{}}}
		r := ScheduleGroupResource{Tags: &id.Tags}
		_, err := r.updateScheduleGroup(t.Context(), client, scheduleGroupPrior{Outputs: &id})
		require.ErrorContains(t, err, "final read")
		require.NotErrorIs(t, err, runtime.ErrNotFound)
		require.Equal(t, []any{groupTestGet(id.Name)}, client.calls)
	})
}

func TestScheduleGroupTagErrors(t *testing.T) {
	unsupported := &smithy.GenericAPIError{Code: "UnsupportedOperation", Message: "tags"}
	for _, region := range []string{groupTestRegion, "us-iso-east-1"} {
		id := groupTestIdentity("group", region)
		id.Tags = map[string]string{"drop": "x"}
		r := ScheduleGroupResource{Name: &id.Name, Tags: &map[string]string{"new": ""}}
		for _, stage := range []string{"create", "list", "remove", "upsert", "list not found"} {
			t.Run(region+"/"+stage, func(t *testing.T) {
				client := &groupTestClient{t: t, gets: []groupTestReply{groupTestActive(id)}}
				removal := schedulerapi.UntagResourceInput{
					ResourceArn: &id.ARN, TagKeys: []string{"drop"},
				}
				upsert := schedulerapi.TagResourceInput{
					ResourceArn: &id.ARN, Tags: []schedulertypes.Tag{groupTestTag("new", "")},
				}
				list := []any{groupTestGet(id.Name),
					schedulerapi.ListTagsForResourceInput{ResourceArn: &id.ARN}}
				if stage == "create" {
					client.createErr = unsupported
					_, err := r.createScheduleGroup(t.Context(), client, region, nil, &groupTestClock{})
					require.ErrorIs(t, err, unsupported)
					require.Equal(t, []any{
						schedulerapi.CreateScheduleGroupInput{Name: &id.Name, Tags: upsert.Tags},
					}, client.calls)
					return
				}
				if stage == "list" || stage == "list not found" {
					client.listErr = unsupported
					if stage == "list not found" {
						client.listErr = groupTestMissing()
					}
					out, err := readScheduleGroup(t.Context(), client, id)
					if region != groupTestRegion && stage == "list" {
						require.NoError(t, err)
						require.Equal(t, map[string]string{}, out.Tags)
					} else {
						require.ErrorIs(t, err, client.listErr)
						require.NotErrorIs(t, err, runtime.ErrNotFound)
					}
					require.Equal(t, list, client.calls)
					return
				}
				want := []any{removal}
				if stage == "remove" {
					client.untagErr = unsupported
				} else {
					client.tagErr = unsupported
					want = append(want, upsert)
				}
				out, err := r.updateScheduleGroup(t.Context(), client, scheduleGroupPrior{Outputs: &id})
				if region != groupTestRegion {
					require.NoError(t, err)
					require.Equal(t, id.ARN, out.ARN)
					want = append(want, list...)
				} else {
					require.ErrorIs(t, err, unsupported)
				}
				require.Equal(t, want, client.calls)
			})
		}
	}
}

func TestScheduleGroupDelete(t *testing.T) {
	id := groupTestIdentity("group", groupTestRegion)
	active := groupTestActive(id)
	pending := groupTestActive(id)
	pending.output.State = schedulertypes.ScheduleGroupStateDeleting
	unknown := groupTestActive(id)
	unknown.output.State = "UNEXPECTED"
	for _, tc := range []struct {
		name      string
		deleteErr error
		gets      []groupTestReply
		errMsg    string
	}{
		{"already absent", groupTestMissing(), nil, ""},
		{"pending", nil, []groupTestReply{active, pending, {err: groupTestMissing()}}, ""},
		{"unexpected state", nil, []groupTestReply{unknown}, "unexpected state UNEXPECTED"},
		{"delete error", errors.New("delete denied"), nil, "delete denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &groupTestClient{t: t, deleteErr: tc.deleteErr, gets: tc.gets}
			clock := &groupTestClock{}
			err := deleteScheduleGroup(t.Context(), client, id, clock)
			if tc.errMsg == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.errMsg)
			}
			want := []any{schedulerapi.DeleteScheduleGroupInput{Name: &id.Name}}
			want = append(want, slices.Repeat([]any{groupTestGet(id.Name)}, len(tc.gets))...)
			require.Equal(t, want, client.calls)
			if tc.name == "pending" {
				require.Equal(t, []time.Duration{200 * time.Millisecond, 400 * time.Millisecond},
					clock.sleeps)
			}
		})
	}
}
