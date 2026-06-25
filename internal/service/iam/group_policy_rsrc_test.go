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

const putGroupPolicyResponseXML = `
<PutGroupPolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</PutGroupPolicyResponse>`

const emptyGetGroupPolicyResponseXML = `
<GetGroupPolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <GetGroupPolicyResult>
    <GroupName>test-group</GroupName>
    <PolicyName>test-inline</PolicyName>
  </GetGroupPolicyResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetGroupPolicyResponse>`

func TestGroupPolicyCreateNormalizesAndReturnsSettledPolicy(t *testing.T) {
	fake := newFakeIAM(t)
	inputDocument := `{ "Statement" : [ { "Resource" : "*", "Action" : ` +
		`"s3:ListBucket", "Effect" : "Allow" } ], "Version" : "2012-10-17" }`
	storedDocument := `{"Statement":[{"Resource":"*","Effect":"Allow",` +
		`"Action":"s3:ListBucket"}],"Version":"2012-10-17"}`
	wantDocument := `{"Version":"2012-10-17","Statement":[{"Action":"s3:ListBucket",` +
		`"Effect":"Allow","Resource":"*"}]}`

	fake.on("PutGroupPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		assert.Equal(t, wantDocument, form.Get("PolicyDocument"))
		return 200, putGroupPolicyResponseXML
	})
	fake.on("GetGroupPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		return 200, getGroupPolicyResponseXML("test-group", "test-inline", storedDocument)
	})

	out, err := (&GroupPolicy{
		GroupName:      "test-group",
		PolicyName:     "test-inline",
		PolicyDocument: inputDocument,
	}).Create(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &GroupPolicyOutput{
		GroupName:      "test-group",
		PolicyName:     "test-inline",
		PolicyDocument: wantDocument,
	}, out)
}

func TestGroupPolicyUpdateOnlyPutsWhenDocumentChanged(t *testing.T) {
	fake := newFakeIAM(t)
	document := `{"Version":"2012-10-17","Statement":[]}`
	fake.on("GetGroupPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		return 200, getGroupPolicyResponseXML("test-group", "test-inline", document)
	})

	prior := runtime.Prior[GroupPolicy, *GroupPolicyOutput]{
		Inputs: GroupPolicy{
			GroupName:      "test-group",
			PolicyName:     "test-inline",
			PolicyDocument: document,
		},
		Outputs: &GroupPolicyOutput{
			GroupName:      "test-group",
			PolicyName:     "test-inline",
			PolicyDocument: document,
		},
		Observed: &GroupPolicyOutput{
			GroupName:      "test-group",
			PolicyName:     "test-inline",
			PolicyDocument: document,
		},
	}
	out, err := (&GroupPolicy{
		GroupName:      "test-group",
		PolicyName:     "test-inline",
		PolicyDocument: document,
	}).Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, document, out.PolicyDocument)
	assert.Empty(t, fake.sent("PutGroupPolicy"))
}

func TestGroupPolicyUpdateNormalizesChangedDocument(t *testing.T) {
	fake := newFakeIAM(t)
	newDocument := `{"Statement":[],"Version":"2012-10-17"}`
	wantDocument := `{"Version":"2012-10-17","Statement":[]}`
	fake.on("PutGroupPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, wantDocument, form.Get("PolicyDocument"))
		return 200, putGroupPolicyResponseXML
	})
	fake.on("GetGroupPolicy", func(_ int, _ url.Values) (int, string) {
		return 200, getGroupPolicyResponseXML("test-group", "test-inline", newDocument)
	})

	prior := runtime.Prior[GroupPolicy, *GroupPolicyOutput]{
		Inputs: GroupPolicy{
			GroupName:      "test-group",
			PolicyName:     "test-inline",
			PolicyDocument: `{"Version":"2012-10-17","Statement":[]}`,
		},
		Outputs: &GroupPolicyOutput{GroupName: "test-group", PolicyName: "test-inline"},
	}
	out, err := (&GroupPolicy{
		GroupName:      "test-group",
		PolicyName:     "test-inline",
		PolicyDocument: newDocument,
	}).Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, wantDocument, out.PolicyDocument)
}

func TestGroupPolicyUpdateReconcilesDocumentDrift(t *testing.T) {
	fake := newFakeIAM(t)
	desiredDocument := `{"Version":"2012-10-17","Statement":[]}`
	driftedDocument := `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":"*","Resource":"*"}]}`
	fake.on("PutGroupPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		assert.Equal(t, desiredDocument, form.Get("PolicyDocument"))
		return 200, putGroupPolicyResponseXML
	})
	fake.on("GetGroupPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		return 200, getGroupPolicyResponseXML("test-group", "test-inline", desiredDocument)
	})

	prior := runtime.Prior[GroupPolicy, *GroupPolicyOutput]{
		Inputs: GroupPolicy{
			GroupName:      "test-group",
			PolicyName:     "test-inline",
			PolicyDocument: desiredDocument,
		},
		Outputs: &GroupPolicyOutput{
			GroupName:      "test-group",
			PolicyName:     "test-inline",
			PolicyDocument: desiredDocument,
		},
		Observed: &GroupPolicyOutput{
			GroupName:      "test-group",
			PolicyName:     "test-inline",
			PolicyDocument: driftedDocument,
		},
	}
	out, err := (&GroupPolicy{
		GroupName:      "test-group",
		PolicyName:     "test-inline",
		PolicyDocument: desiredDocument,
	}).Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, desiredDocument, out.PolicyDocument)
}

func TestGroupPolicyReadMapsMissingPolicyToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetGroupPolicy", func(_ int, _ url.Values) (int, string) {
		return 404, noSuchEntityXML
	})

	_, err := (&GroupPolicy{}).Read(context.Background(), fake.configuration(), &GroupPolicyOutput{
		GroupName:  "test-group",
		PolicyName: "test-inline",
	})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestGroupPolicyReadMapsNilPolicyDocumentToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetGroupPolicy", func(_ int, _ url.Values) (int, string) {
		return 200, emptyGetGroupPolicyResponseXML
	})

	_, err := (&GroupPolicy{}).Read(context.Background(), fake.configuration(), &GroupPolicyOutput{
		GroupName:  "test-group",
		PolicyName: "test-inline",
	})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestGroupPolicyDeleteUsesPriorIdentityAndIgnoresNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("DeleteGroupPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "old-group", form.Get("GroupName"))
		assert.Equal(t, "old-inline", form.Get("PolicyName"))
		return 404, noSuchEntityXML
	})

	err := (&GroupPolicy{GroupName: "new-group", PolicyName: "new-inline"}).Delete(
		context.Background(), fake.configuration(),
		&GroupPolicyOutput{GroupName: "old-group", PolicyName: "old-inline"})
	require.NoError(t, err)
}

func TestNormalizeIAMPolicyJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "version first",
			input: `{"Statement":[],"Version":"2012-10-17"}`,
			want:  `{"Version":"2012-10-17","Statement":[]}`,
		},
		{
			name:  "no matching version",
			input: `{"Statement":[],"Version":"2008-10-17"}`,
			want:  `{"Statement":[],"Version":"2008-10-17"}`,
		},
		{name: "empty", input: "  ", wantErr: true},
		{name: "malformed", input: `{"Statement":`, wantErr: true},
		{name: "array", input: `[]`, wantErr: true},
		{name: "string", input: `"policy"`, wantErr: true},
		{name: "duplicate top-level key", input: `{"Statement":[],"Statement":[]}`, wantErr: true},
		{
			name:    "duplicate nested key",
			input:   `{"Statement":[{"Effect":"Allow","Effect":"Deny"}]}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeIAMPolicyJSON(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func getGroupPolicyResponseXML(group, policyName, document string) string {
	return fmt.Sprintf(`
<GetGroupPolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <GetGroupPolicyResult>
    <GroupName>%s</GroupName>
    <PolicyName>%s</PolicyName>
    <PolicyDocument>%s</PolicyDocument>
  </GetGroupPolicyResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetGroupPolicyResponse>`, group, policyName, url.QueryEscape(document))
}
