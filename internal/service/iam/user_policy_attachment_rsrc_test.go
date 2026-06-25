package iam

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const attachUserPolicyResponseXML = `
<AttachUserPolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</AttachUserPolicyResponse>`

func TestUserPolicyAttachmentCreateAttachesAndReadsPolicy(t *testing.T) {
	policyArn := "arn:aws:iam::123456789012:policy/test-policy"
	fake := newFakeIAM(t)
	fake.on("AttachUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, policyArn, form.Get("PolicyArn"))
		return 200, attachUserPolicyResponseXML
	})
	fake.on("ListAttachedUserPolicies", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		return 200, listAttachedUserPoliciesResponseXML(false, "",
			attachedPolicyXML("test-policy", policyArn))
	})

	out, err := (&UserPolicyAttachment{
		User:      "test-user",
		PolicyArn: policyArn,
	}).Create(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &UserPolicyAttachmentOutput{
		User:      "test-user",
		PolicyArn: policyArn,
	}, out)
}

func TestUserPolicyAttachmentReadUsesPriorIdentityAndPaginates(t *testing.T) {
	policyArn := "arn:aws:iam::123456789012:policy/test-policy"
	fake := newFakeIAM(t)
	fake.on("ListAttachedUserPolicies", func(n int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		switch n {
		case 1:
			assert.Empty(t, form.Get("Marker"))
			return 200, listAttachedUserPoliciesResponseXML(true, "next",
				attachedPolicyXML("other", "arn:aws:iam::123456789012:policy/other"))
		case 2:
			assert.Equal(t, "next", form.Get("Marker"))
			return 200, listAttachedUserPoliciesResponseXML(false, "",
				attachedPolicyXML("test-policy", policyArn))
		default:
			t.Fatalf("unexpected ListAttachedUserPolicies call %d", n)
			return 500, ""
		}
	})

	out, err := (&UserPolicyAttachment{
		User:      "desired-user",
		PolicyArn: "arn:aws:iam::123456789012:policy/desired-policy",
	}).Read(context.Background(), fake.configuration(), &UserPolicyAttachmentOutput{
		User:      "test-user",
		PolicyArn: policyArn,
	})
	require.NoError(t, err)
	assert.Equal(t, &UserPolicyAttachmentOutput{
		User:      "test-user",
		PolicyArn: policyArn,
	}, out)
}

func TestUserPolicyAttachmentReadMapsMissingUserToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("ListAttachedUserPolicies", func(_ int, _ url.Values) (int, string) {
		return 404, noSuchEntityXML
	})

	_, err := (&UserPolicyAttachment{
		User:      "missing-user",
		PolicyArn: "arn:aws:iam::123456789012:policy/test-policy",
	}).Read(context.Background(), fake.configuration(), &UserPolicyAttachmentOutput{
		User:      "missing-user",
		PolicyArn: "arn:aws:iam::123456789012:policy/test-policy",
	})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestUserPolicyAttachmentReadMapsAbsentPolicyToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("ListAttachedUserPolicies", func(_ int, _ url.Values) (int, string) {
		return 200, listAttachedUserPoliciesResponseXML(false, "",
			attachedPolicyXML("other", "arn:aws:iam::123456789012:policy/other"))
	})

	_, err := (&UserPolicyAttachment{
		User:      "test-user",
		PolicyArn: "arn:aws:iam::123456789012:policy/test-policy",
	}).Read(context.Background(), fake.configuration(), &UserPolicyAttachmentOutput{
		User:      "test-user",
		PolicyArn: "arn:aws:iam::123456789012:policy/test-policy",
	})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestUserPolicyAttachmentDeleteUsesPriorIdentityAndTreatsNotFoundAsSuccess(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("DetachUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "old-user", form.Get("UserName"))
		assert.Equal(t,
			"arn:aws:iam::123456789012:policy/old-policy", form.Get("PolicyArn"))
		return 404, noSuchEntityXML
	})

	err := (&UserPolicyAttachment{
		User:      "new-user",
		PolicyArn: "arn:aws:iam::123456789012:policy/new-policy",
	}).Delete(context.Background(), fake.configuration(), &UserPolicyAttachmentOutput{
		User:      "old-user",
		PolicyArn: "arn:aws:iam::123456789012:policy/old-policy",
	})
	require.NoError(t, err)
}

func TestValidUserPolicyAttachmentARN(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want bool
	}{
		{
			name: "empty value",
			arn:  "",
			want: true,
		},
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
			name: "other service ARN",
			arn:  "arn:aws:s3:::test-bucket",
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
			assert.Equal(t, tt.want, validUserPolicyAttachmentARN(tt.arn))
		})
	}
}

func listAttachedUserPoliciesResponseXML(
	truncated bool, marker string, policies ...string,
) string {
	markerXML := ""
	if marker != "" {
		markerXML = fmt.Sprintf("<Marker>%s</Marker>", marker)
	}
	return fmt.Sprintf(`
<ListAttachedUserPoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListAttachedUserPoliciesResult>
    <AttachedPolicies>%s</AttachedPolicies>
    <IsTruncated>%t</IsTruncated>
    %s
  </ListAttachedUserPoliciesResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListAttachedUserPoliciesResponse>`, joinXML(policies), truncated, markerXML)
}
