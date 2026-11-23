package wafv2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	awsretry "github.com/cloudboss/unobin-library-aws/internal/retry"
)

func TestRuleGroupJSONSearchUsesLiteralText(t *testing.T) {
	resource := RuleGroupResource{RulesJSON: aws.String(`[
		{"Statement":{"ByteMatchStatement":{"SearchString":"Zm9v"}}},
		{"Statement":{"NotStatement":{"Statement":{"ByteMatchStatement":{"SearchString":"abc"}}}},
		 "ChallengeConfig":{"ImmunityTimeProperty":{"ImmunityTime":9}}}
	]`)}
	prepared, err := resource.prepareRuleGroup()
	require.NoError(t, err)
	require.Equal(t, []byte("Zm9v"), prepared.rules[0].Statement.ByteMatchStatement.SearchString)
	second := prepared.rules[1]
	require.Equal(t, []byte("abc"),
		second.Statement.NotStatement.Statement.ByteMatchStatement.SearchString)
	require.EqualValues(t, 9, *second.ChallengeConfig.ImmunityTimeProperty.ImmunityTime)
}

type ruleGroupFake struct {
	calls        []string
	creates      []*awssvc.CreateRuleGroupInput
	updates      []*awssvc.UpdateRuleGroupInput
	deletes      []*awssvc.DeleteRuleGroupInput
	gets         []*awssvc.GetRuleGroupInput
	lists        []*awssvc.ListTagsForResourceInput
	untags       []*awssvc.UntagResourceInput
	tags         []*awssvc.TagResourceInput
	createErrors []error
	updateErrors []error
	deleteErrors []error
	getOutput    *awssvc.GetRuleGroupOutput
	getError     error
	listError    error
	tagError     error
	untagError   error
	omitSummary  bool
}

func takeRuleGroupError(queue *[]error) error {
	if len(*queue) == 0 {
		return nil
	}
	err := (*queue)[0]
	*queue = (*queue)[1:]
	return err
}

func (f *ruleGroupFake) CreateRuleGroup(
	_ context.Context, input *awssvc.CreateRuleGroupInput, _ ...func(*awssvc.Options),
) (*awssvc.CreateRuleGroupOutput, error) {
	f.calls = append(f.calls, "create")
	f.creates = append(f.creates, input)
	if err := takeRuleGroupError(&f.createErrors); err != nil {
		return nil, err
	}
	if f.omitSummary {
		return &awssvc.CreateRuleGroupOutput{}, nil
	}
	return &awssvc.CreateRuleGroupOutput{
		Summary: &awstypes.RuleGroupSummary{Id: aws.String("group-id"), LockToken: aws.String("old")},
	}, nil
}

func (f *ruleGroupFake) GetRuleGroup(
	_ context.Context, input *awssvc.GetRuleGroupInput, _ ...func(*awssvc.Options),
) (*awssvc.GetRuleGroupOutput, error) {
	f.calls = append(f.calls, "get")
	f.gets = append(f.gets, input)
	return f.getOutput, f.getError
}

func (f *ruleGroupFake) UpdateRuleGroup(
	_ context.Context, input *awssvc.UpdateRuleGroupInput, _ ...func(*awssvc.Options),
) (*awssvc.UpdateRuleGroupOutput, error) {
	f.calls = append(f.calls, "update")
	f.updates = append(f.updates, input)
	return &awssvc.UpdateRuleGroupOutput{NextLockToken: aws.String("discard")},
		takeRuleGroupError(&f.updateErrors)
}

func (f *ruleGroupFake) DeleteRuleGroup(
	_ context.Context, input *awssvc.DeleteRuleGroupInput, _ ...func(*awssvc.Options),
) (*awssvc.DeleteRuleGroupOutput, error) {
	f.calls = append(f.calls, "delete")
	f.deletes = append(f.deletes, input)
	return nil, takeRuleGroupError(&f.deleteErrors)
}

func (f *ruleGroupFake) ListTagsForResource(
	_ context.Context, input *awssvc.ListTagsForResourceInput, _ ...func(*awssvc.Options),
) (*awssvc.ListTagsForResourceOutput, error) {
	f.calls = append(f.calls, "list")
	f.lists = append(f.lists, input)
	return &awssvc.ListTagsForResourceOutput{
		NextMarker: aws.String("ignored"),
		TagInfoForResource: &awstypes.TagInfoForResource{
			TagList: []awstypes.Tag{
				{Key: aws.String("aws:system"), Value: aws.String("ignored")},
				{Key: aws.String("AWS:case"), Value: aws.String("")},
				{Key: aws.String("keep"), Value: aws.String("yes")},
			},
		},
	}, f.listError
}

func (f *ruleGroupFake) UntagResource(
	_ context.Context, input *awssvc.UntagResourceInput, _ ...func(*awssvc.Options),
) (*awssvc.UntagResourceOutput, error) {
	f.calls = append(f.calls, "untag")
	f.untags = append(f.untags, input)
	return nil, f.untagError
}

func (f *ruleGroupFake) TagResource(
	_ context.Context, input *awssvc.TagResourceInput, _ ...func(*awssvc.Options),
) (*awssvc.TagResourceOutput, error) {
	f.calls = append(f.calls, "tag")
	f.tags = append(f.tags, input)
	return nil, f.tagError
}

func ruleGroupIdentityFixture(name, region string) RuleGroupResourceOutput {
	return RuleGroupResourceOutput{
		ID: "group-id", Name: name, Region: region, Scope: "REGIONAL", LockToken: "saved",
		ARN: "arn:aws:wafv2:" + region + ":123456789012:regional/rulegroup/" + name + "/group-id",
	}
}

func ruleGroupReadFixture(identity RuleGroupResourceOutput) *awssvc.GetRuleGroupOutput {
	return &awssvc.GetRuleGroupOutput{
		LockToken: aws.String("fresh"),
		RuleGroup: &awstypes.RuleGroup{
			Id: aws.String(identity.ID), Name: aws.String(identity.Name), ARN: aws.String(identity.ARN),
			Capacity:         aws.Int64(10),
			VisibilityConfig: &awstypes.VisibilityConfig{MetricName: aws.String("remote-visibility")},
		},
	}
}

func TestRuleGroupNamingAndReadback(t *testing.T) {
	for _, tc := range []struct {
		name    *string
		prefix  *string
		want    string
		derived *string
	}{
		{name: aws.String("exact"), want: "exact"},
		{name: aws.String("exact-" + strings.Repeat("ab", 13)),
			want: "exact-" + strings.Repeat("ab", 13), derived: aws.String("exact-")},
		{prefix: aws.String(strings.Repeat("a", 102)),
			want:    strings.Repeat("a", 102) + strings.Repeat("ab", 13),
			derived: aws.String(strings.Repeat("a", 102))},
		{want: "unobin-" + strings.Repeat("ab", 13), derived: aws.String("unobin-")},
	} {
		t.Run(tc.want, func(t *testing.T) {
			resource := RuleGroupResource{
				Name: tc.name, NamePrefix: tc.prefix, Capacity: 10, Scope: "REGIONAL",
				VisibilityConfig: WebACLVisibilityConfig{MetricName: "visibility"},
			}
			saved := ruleGroupIdentityFixture(tc.want, "us-west-2")
			cloud := ruleGroupFake{getOutput: ruleGroupReadFixture(saved)}
			random := bytes.NewReader(bytes.Repeat([]byte{0xab}, 13))
			out, err := resource.createRuleGroup(t.Context(), &cloud, "us-west-2", random)
			require.NoError(t, err)
			require.Equal(t, []string{"create", "get", "list"}, cloud.calls)
			require.Equal(t, []*awssvc.CreateRuleGroupInput{{
				Capacity: aws.Int64(10), Name: aws.String(tc.want), Scope: awstypes.ScopeRegional,
				VisibilityConfig: &awstypes.VisibilityConfig{MetricName: aws.String("visibility")},
			}}, cloud.creates)
			require.Equal(t, []*awssvc.GetRuleGroupInput{{
				Id: aws.String(saved.ID), Name: aws.String(tc.want), Scope: awstypes.ScopeRegional,
			}}, cloud.gets)
			require.Equal(t, []*awssvc.ListTagsForResourceInput{{ResourceARN: aws.String(saved.ARN)}},
				cloud.lists)
			saved.LockToken = "fresh"
			saved.NamePrefix = tc.derived
			saved.Capacity = 10
			saved.VisibilityConfig.MetricName = "remote-visibility"
			saved.Rules = ptr.To([]RuleGroupRule{})
			saved.CustomResponseBodies = map[string]WebACLCustomResponseBody{}
			saved.Tags = map[string]string{"AWS:case": "", "keep": "yes"}
			require.Equal(t, &saved, out)
		})
	}
	resource := RuleGroupResource{Scope: "REGIONAL"}
	cloud := ruleGroupFake{}
	_, err := resource.createRuleGroup(t.Context(), &cloud, "us-west-2", strings.NewReader(""))
	require.ErrorIs(t, err, io.EOF)
	require.Empty(t, cloud.calls)
	cloud.omitSummary = true
	resource.Name = aws.String("missing")
	_, err = resource.createRuleGroup(t.Context(), &cloud, "us-west-2", nil)
	require.ErrorContains(t, err, "response has no rule group ID")
	require.Equal(t, []string{"create"}, cloud.calls)
}

func TestRuleGroupRefreshClassifiesOnlyEstablishedAbsence(t *testing.T) {
	id := ruleGroupIdentityFixture("sample", "us-west-2")
	absent := &awstypes.WAFNonexistentItemException{Message: aws.String("gone")}
	similar := errors.New("WAFNonexistentItemException: gone")
	for _, tc := range []struct {
		name       string
		err        error
		output     func(*awssvc.GetRuleGroupOutput) *awssvc.GetRuleGroupOutput
		missing    bool
		diagnostic string
	}{
		{name: "typed absence", err: absent, missing: true, diagnostic: "gone"},
		{name: "similar error", err: similar, diagnostic: "gone"},
		{name: "nil output", output: func(*awssvc.GetRuleGroupOutput) *awssvc.GetRuleGroupOutput {
			return nil
		}, missing: true, diagnostic: "response has no rule group"},
		{name: "nil group", output: func(out *awssvc.GetRuleGroupOutput) *awssvc.GetRuleGroupOutput {
			out.RuleGroup = nil
			return out
		}, missing: true, diagnostic: "response has no rule group"},
		{name: "wrong ARN", output: func(out *awssvc.GetRuleGroupOutput) *awssvc.GetRuleGroupOutput {
			out.RuleGroup.ARN = aws.String(id.ARN + "-mismatch")
			return out
		}, diagnostic: "response identity does not match"},
		{name: "wrong ID", output: func(out *awssvc.GetRuleGroupOutput) *awssvc.GetRuleGroupOutput {
			out.RuleGroup.Id = aws.String("wrong")
			return out
		}, diagnostic: "response identity does not match"},
		{name: "wrong name", output: func(out *awssvc.GetRuleGroupOutput) *awssvc.GetRuleGroupOutput {
			out.RuleGroup.Name = aws.String("wrong")
			return out
		}, diagnostic: "response identity does not match"},
		{name: "missing token", output: func(out *awssvc.GetRuleGroupOutput) *awssvc.GetRuleGroupOutput {
			out.LockToken = nil
			return out
		}, diagnostic: "response has no lock token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := ruleGroupReadFixture(id)
			if tc.output != nil {
				output = tc.output(output)
			}
			cloud := ruleGroupFake{getOutput: output, getError: tc.err}
			for _, established := range []bool{true, false} {
				_, err := readRuleGroup(t.Context(), &cloud, id, nil, established)
				if established && tc.missing {
					require.ErrorIs(t, err, runtime.ErrNotFound)
				} else {
					require.NotErrorIs(t, err, runtime.ErrNotFound)
					require.ErrorContains(t, err, tc.diagnostic)
				}
			}
			require.Equal(t, []string{"get", "get"}, cloud.calls)
		})
	}
}

func TestRuleGroupRetryAndMutationTokens(t *testing.T) {
	unavailable := &awstypes.WAFUnavailableEntityException{}
	associated := &awstypes.WAFAssociatedItemException{}
	conflict := &awstypes.WAFOptimisticLockException{}
	id := ruleGroupIdentityFixture("sample", "us-west-2")
	resource := RuleGroupResource{Scope: id.Scope, Name: aws.String(id.Name), Capacity: 10}
	cloud := ruleGroupFake{
		getOutput:    ruleGroupReadFixture(id),
		createErrors: []error{unavailable},
		updateErrors: []error{unavailable},
		deleteErrors: []error{associated, unavailable},
	}
	options := []awsretry.Option{
		awsretry.WithTimeout(time.Second), awsretry.WithInterval(time.Millisecond),
	}
	_, err := resource.createRuleGroup(t.Context(), &cloud, id.Region, nil, options...)
	require.NoError(t, err)
	require.Equal(t, []string{"create", "create", "get", "list"}, cloud.calls)
	require.Equal(t, []*awssvc.CreateRuleGroupInput{cloud.creates[0], cloud.creates[0]}, cloud.creates)
	cloud.calls = nil

	latest := id
	latest.LockToken = "fresh"
	prior := ruleGroupPrior{Inputs: resource, Outputs: &id, Observed: &latest}
	resource.Description = aws.String("updated")
	out, err := resource.updateRuleGroup(t.Context(), &cloud, prior, options...)
	require.NoError(t, err)
	require.Equal(t, "fresh", out.LockToken)
	require.Equal(t, []string{"update", "update", "get", "list"}, cloud.calls)
	update := &awssvc.UpdateRuleGroupInput{
		Id: aws.String(id.ID), Name: aws.String(id.Name), Scope: awstypes.ScopeRegional,
		LockToken: aws.String("fresh"), Description: aws.String("updated"),
		VisibilityConfig: &awstypes.VisibilityConfig{MetricName: aws.String("")},
	}
	require.Equal(t, []*awssvc.UpdateRuleGroupInput{update, update}, cloud.updates)

	cloud.calls = nil
	require.NoError(t, deleteRuleGroup(t.Context(), &cloud, prior, options...))
	deleteRequest := &awssvc.DeleteRuleGroupInput{
		Id: aws.String(id.ID), Name: aws.String(id.Name), Scope: awstypes.ScopeRegional,
		LockToken: aws.String("fresh"),
	}
	require.Equal(t, []string{"delete", "delete", "delete"}, cloud.calls)
	require.Equal(t, []*awssvc.DeleteRuleGroupInput{deleteRequest, deleteRequest, deleteRequest},
		cloud.deletes)

	cloud.calls = nil
	cloud.deleteErrors = []error{conflict, unavailable}
	require.ErrorIs(t, deleteRuleGroup(t.Context(), &cloud, prior, options...), conflict)
	require.Equal(t, []string{"delete"}, cloud.calls)
	require.Equal(t, []error{unavailable}, cloud.deleteErrors)

	cloud.calls = nil
	cloud.updateErrors = []error{conflict, unavailable}
	_, err = resource.updateRuleGroup(t.Context(), &cloud, prior, options...)
	require.ErrorIs(t, err, conflict)
	require.Equal(t, []string{"update"}, cloud.calls)

	cloud.calls = nil
	cloud.deleteErrors = []error{unavailable, unavailable}
	err = deleteRuleGroup(t.Context(), &cloud, prior,
		awsretry.WithTimeout(-time.Second), awsretry.WithInterval(time.Millisecond))
	require.ErrorIs(t, err, unavailable)
	require.Equal(t, []string{"delete"}, cloud.calls)
	require.Equal(t, []error{unavailable}, cloud.deleteErrors)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cloud.calls = nil
	require.ErrorIs(t, deleteRuleGroup(ctx, &cloud, prior, options...), context.Canceled)
	require.Empty(t, cloud.calls)
}

func TestRuleGroupTagOrderingAndFailures(t *testing.T) {
	id := ruleGroupIdentityFixture("sample", "us-west-2")
	id.Tags = map[string]string{"remove": "value", "keep": "yes", "change": "old"}
	desired := RuleGroupResource{Tags: ptr.To(map[string]string{
		"keep": "yes", "change": "", "add": "new", "aws:ignore": "system",
	})}
	cloud := ruleGroupFake{getOutput: ruleGroupReadFixture(id)}
	prior := ruleGroupPrior{Outputs: &id}
	out, err := desired.updateRuleGroup(t.Context(), &cloud, prior)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"keep": "yes", "AWS:case": ""}, out.Tags)
	require.Equal(t, []string{"untag", "tag", "get", "list"}, cloud.calls)
	require.Equal(t, []*awssvc.UntagResourceInput{{
		ResourceARN: aws.String(id.ARN), TagKeys: []string{"remove"},
	}}, cloud.untags)
	require.Equal(t, []*awssvc.TagResourceInput{{
		ResourceARN: aws.String(id.ARN),
		Tags: []awstypes.Tag{
			{Key: aws.String("add"), Value: aws.String("new")},
			{Key: aws.String("change"), Value: aws.String("")},
		},
	}}, cloud.tags)

	cloud.calls, cloud.untags = nil, nil
	desired.Tags = nil
	_, err = desired.updateRuleGroup(t.Context(), &cloud, prior)
	require.NoError(t, err)
	require.Equal(t, []string{"untag", "get", "list"}, cloud.calls)
	require.Equal(t, []*awssvc.UntagResourceInput{{
		ResourceARN: aws.String(id.ARN), TagKeys: []string{"change", "keep", "remove"},
	}}, cloud.untags)

	cloud.calls = nil
	denied := &smithy.GenericAPIError{Code: "AccessDenied", Message: "test"}
	cloud.untagError = denied
	desired.Description = aws.String("core change")
	_, err = desired.updateRuleGroup(t.Context(), &cloud, prior)
	require.ErrorIs(t, err, denied)
	require.Equal(t, []string{"untag"}, cloud.calls)
	iso := ruleGroupIdentityFixture("sample", "us-iso-east-1")
	iso.Tags = id.Tags
	cloud.calls = nil
	cloud.listError = denied
	cloud.getOutput = ruleGroupReadFixture(iso)
	out, err = desired.updateRuleGroup(t.Context(), &cloud, ruleGroupPrior{Outputs: &iso})
	require.NoError(t, err)
	require.Equal(t, map[string]string{}, out.Tags)
	require.Equal(t, []string{"untag", "update", "get", "list"}, cloud.calls)
}

func TestRuleGroupRequestParsingBeforeMutation(t *testing.T) {
	id := ruleGroupIdentityFixture("sample", "us-west-2")
	for _, malformed := range []string{`{}`, `[{}]`, `[null]`,
		`[{"Statement":{"ByteMatchStatement":{"SearchString":7}}}]`,
		`[{"Priority":"string"}]`,
	} {
		t.Run(malformed, func(t *testing.T) {
			resource := RuleGroupResource{RulesJSON: aws.String(malformed)}
			cloud := ruleGroupFake{}
			_, err := resource.createRuleGroup(t.Context(), &cloud, id.Region, nil)
			require.ErrorContains(t, err, "rules-json")
			_, err = resource.updateRuleGroup(t.Context(), &cloud, ruleGroupPrior{Outputs: &id})
			require.ErrorContains(t, err, "rules-json")
			require.Empty(t, cloud.calls)
		})
	}
	input := RuleGroupResource{RulesJSON: aws.String(" [ ] ")}
	got, err := input.prepareRuleGroup()
	require.NoError(t, err)
	require.Equal(t, []awstypes.Rule{}, got.rules)
	require.Equal(t, aws.String("[]"), got.rulesJSON)
	input.RulesJSON = aws.String(" null ")
	got, err = input.prepareRuleGroup()
	require.NoError(t, err)
	require.Nil(t, got.rules)
	require.Equal(t, aws.String("null"), got.rulesJSON)
}

func TestRuleGroupReferenceARNGrammar(t *testing.T) {
	for _, arn := range []string{
		"", "arn:aws:::123456789012:part:two", "arn:aws:arbitrary:::resource",
		"arn:aws-cn:lambda:cn-north-1:aws:resource",
		"arn:aws-eusc:lambda:eusc-de-east-1:aws-managed:resource",
		"arn:aws-iso:lambda:us-iso-east-12:third-party:resource",
		"arn:aws-us-gov:lambda:us-gov-west-1:aws-marketplace:resource",
		"arn:aws:lambda::partner-managed:resource",
		"arn:aws:lambda::cw0123456789:resource",
	} {
		require.NoError(t, parseRuleGroupARN(arn), "%q", arn)
	}
	for _, arn := range []string{
		"ARN:aws:wafv2:::resource", "arn:Aws:wafv2:::resource", "arn:aws-other3:lambda:::r",
		"arn:aws:wafv2:us-west-123::resource", "arn:aws:wafv2::123:resource", "arn:aws:wafv2:::",
	} {
		require.ErrorContains(t, parseRuleGroupARN(arn), "invalid reference ARN", "%q", arn)
	}
}

func TestRuleGroupJSONIgnoresUnknownMembersWhileDecodingKnownSearch(t *testing.T) {
	resource := RuleGroupResource{RulesJSON: aws.String(`[
		{"Unknown":{"Statement":{"ByteMatchStatement":{"SearchString":123}}},
		 "statement":{"NotStatement":{"Statement":{"ByteMatchStatement":{
		   "SearchString":"YWJj", "Unknown":{"ByteMatchStatement":{"SearchString":456}}
		 }}}}}
	]`)}
	out, err := resource.prepareRuleGroup()
	require.NoError(t, err)
	want := []awstypes.Rule{{Statement: &awstypes.Statement{NotStatement: &awstypes.NotStatement{
		Statement: &awstypes.Statement{ByteMatchStatement: &awstypes.ByteMatchStatement{
			SearchString: []byte("YWJj"),
		}},
	}}}}
	require.Equal(t, want, out.rules)
}

func TestRuleGroupUpdateReadFailureAndSavedDelete(t *testing.T) {
	id := ruleGroupIdentityFixture("sample", "us-west-2")
	absent := &awstypes.WAFNonexistentItemException{Message: aws.String("gone")}
	cloud := ruleGroupFake{getError: absent}
	prior := ruleGroupPrior{Outputs: &id}
	resource := RuleGroupResource{Description: aws.String("update")}
	_, err := resource.updateRuleGroup(t.Context(), &cloud, prior)
	require.ErrorIs(t, err, absent)
	require.NotErrorIs(t, err, runtime.ErrNotFound)
	require.Equal(t, []string{"update", "get"}, cloud.calls)
	require.Equal(t, aws.String("saved"), cloud.updates[0].LockToken)
	_, err = readRuleGroup(t.Context(), &cloud, id, nil, true)
	require.ErrorIs(t, err, runtime.ErrNotFound)

	cloud.calls = nil
	cloud.deleteErrors = []error{absent}
	require.NoError(t, deleteRuleGroup(t.Context(), &cloud, prior))
	require.Equal(t, []string{"delete"}, cloud.calls)
	require.Equal(t, []*awssvc.DeleteRuleGroupInput{{
		Id: aws.String(id.ID), Name: aws.String(id.Name),
		Scope: awstypes.ScopeRegional, LockToken: aws.String("saved"),
	}}, cloud.deletes)

	cloud.calls = nil
	wrong := id
	wrong.Name = "another"
	prior.Observed = &wrong
	require.ErrorContains(t, deleteRuleGroup(t.Context(), &cloud, prior), "saved identity")
	require.Empty(t, cloud.calls)
}
