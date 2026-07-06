package iam

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const attachGroupPolicyResponseXML = `
<AttachGroupPolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</AttachGroupPolicyResponse>`

func TestGroupPolicyAttachmentCreateAttachesAndReadsPolicy(t *testing.T) {
	policyArn := "arn:aws:iam::123456789012:policy/test-policy"
	fake := newFakeIAM(t)
	fake.on("AttachGroupPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		assert.Equal(t, policyArn, form.Get("PolicyArn"))
		return 200, attachGroupPolicyResponseXML
	})
	fake.on("ListAttachedGroupPolicies", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		return 200, listAttachedGroupPoliciesResponseXML(false, "",
			attachedPolicyXML("test-policy", policyArn))
	})

	out, err := (&GroupPolicyAttachmentResource{
		GroupName: "test-group",
		PolicyArn: policyArn,
	}).Create(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &GroupPolicyAttachmentResourceOutput{
		GroupName: "test-group",
		PolicyArn: policyArn,
	}, out)
}

func TestGroupPolicyAttachmentReadUsesPriorIdentityAndPaginates(t *testing.T) {
	policyArn := "arn:aws:iam::123456789012:policy/test-policy"
	fake := newFakeIAM(t)
	fake.on("ListAttachedGroupPolicies", func(n int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		switch n {
		case 1:
			assert.Empty(t, form.Get("Marker"))
			return 200, listAttachedGroupPoliciesResponseXML(true, "next",
				attachedPolicyXML("", ""),
				attachedPolicyXML("other", "arn:aws:iam::123456789012:policy/other"))
		case 2:
			assert.Equal(t, "next", form.Get("Marker"))
			return 200, listAttachedGroupPoliciesResponseXML(false, "",
				attachedPolicyXML("test-policy", policyArn))
		default:
			t.Fatalf("unexpected ListAttachedGroupPolicies call %d", n)
			return 500, ""
		}
	})

	out, err := (&GroupPolicyAttachmentResource{
		GroupName: "desired-group",
		PolicyArn: "arn:aws:iam::123456789012:policy/desired-policy",
	}).Read(context.Background(), fake.configuration(), &GroupPolicyAttachmentResourceOutput{
		GroupName: "test-group",
		PolicyArn: policyArn,
	})
	require.NoError(t, err)
	assert.Equal(t, &GroupPolicyAttachmentResourceOutput{
		GroupName: "test-group",
		PolicyArn: policyArn,
	}, out)
}

func TestGroupPolicyAttachmentReadMapsMissingGroupToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("ListAttachedGroupPolicies", func(_ int, _ url.Values) (int, string) {
		return 404, noSuchEntityXML
	})

	_, err := (&GroupPolicyAttachmentResource{
		GroupName: "missing-group",
		PolicyArn: "arn:aws:iam::123456789012:policy/test-policy",
	}).Read(context.Background(), fake.configuration(), &GroupPolicyAttachmentResourceOutput{
		GroupName: "missing-group",
		PolicyArn: "arn:aws:iam::123456789012:policy/test-policy",
	})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestGroupPolicyAttachmentReadMapsAbsentPolicyToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("ListAttachedGroupPolicies", func(_ int, _ url.Values) (int, string) {
		return 200, listAttachedGroupPoliciesResponseXML(false, "",
			attachedPolicyXML("other", "arn:aws:iam::123456789012:policy/other"))
	})

	_, err := (&GroupPolicyAttachmentResource{
		GroupName: "test-group",
		PolicyArn: "arn:aws:iam::123456789012:policy/test-policy",
	}).Read(context.Background(), fake.configuration(), &GroupPolicyAttachmentResourceOutput{
		GroupName: "test-group",
		PolicyArn: "arn:aws:iam::123456789012:policy/test-policy",
	})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestGroupPolicyAttachmentDeleteUsesPriorIdentityAndTreatsNotFoundAsSuccess(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("DetachGroupPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "old-group", form.Get("GroupName"))
		assert.Equal(t,
			"arn:aws:iam::123456789012:policy/old-policy", form.Get("PolicyArn"))
		return 404, noSuchEntityXML
	})

	err := (&GroupPolicyAttachmentResource{
		GroupName: "new-group",
		PolicyArn: "arn:aws:iam::123456789012:policy/new-policy",
	}).Delete(context.Background(), fake.configuration(), &GroupPolicyAttachmentResourceOutput{
		GroupName: "old-group",
		PolicyArn: "arn:aws:iam::123456789012:policy/old-policy",
	})
	require.NoError(t, err)
}

func TestValidGroupPolicyAttachmentARN(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want bool
	}{
		{
			name: "managed policy ARN",
			arn:  "arn:aws:iam::123456789012:policy/test-policy",
			want: true,
		},
		{
			name: "gov partition",
			arn:  "arn:aws-us-gov:iam::aws:policy/AdministratorAccess",
			want: true,
		},
		{
			name: "missing ARN prefix",
			arn:  "aws:iam::123456789012:policy/test-policy",
			want: false,
		},
		{
			name: "missing partition",
			arn:  "arn::iam::123456789012:policy/test-policy",
			want: false,
		},
		{
			name: "invalid region",
			arn:  "arn:aws:iam:useast1:123456789012:policy/test-policy",
			want: false,
		},
		{
			name: "invalid account",
			arn:  "arn:aws:iam::123:policy/test-policy",
			want: false,
		},
		{
			name: "missing resource",
			arn:  "arn:aws:iam::123456789012:",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validGroupPolicyAttachmentARN(tt.arn))
		})
	}
}

func listAttachedGroupPoliciesResponseXML(
	truncated bool, marker string, policies ...string,
) string {
	markerXML := ""
	if marker != "" {
		markerXML = fmt.Sprintf("<Marker>%s</Marker>", marker)
	}
	return fmt.Sprintf(`
<ListAttachedGroupPoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListAttachedGroupPoliciesResult>
    <AttachedPolicies>%s</AttachedPolicies>
    <IsTruncated>%t</IsTruncated>
    %s
  </ListAttachedGroupPoliciesResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListAttachedGroupPoliciesResponse>`, joinXML(policies), truncated, markerXML)
}

func attachedPolicyXML(name, arn string) string {
	if name == "" && arn == "" {
		return "<member/>"
	}
	return fmt.Sprintf(`
<member>
  <PolicyName>%s</PolicyName>
  <PolicyArn>%s</PolicyArn>
</member>`, name, arn)
}

func joinXML(parts []string) string {
	return strings.Join(parts, "")
}
