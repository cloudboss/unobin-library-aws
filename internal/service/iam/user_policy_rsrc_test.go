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

const putUserPolicyResponseXML = `
<PutUserPolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</PutUserPolicyResponse>`

const emptyGetUserPolicyResponseXML = `
<GetUserPolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <GetUserPolicyResult>
    <UserName>test-user</UserName>
    <PolicyName>test-inline</PolicyName>
  </GetUserPolicyResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetUserPolicyResponse>`

func TestUserPolicyCreateNormalizesAndReturnsSettledPolicy(t *testing.T) {
	fake := newFakeIAM(t)
	inputDocument := `{ "Statement" : [ { "Resource" : "*", "Action" : ` +
		`"s3:ListBucket", "Effect" : "Allow" } ], "Version" : "2012-10-17" }`
	storedDocument := `{"Statement":[{"Resource":"*","Effect":"Allow",` +
		`"Action":"s3:ListBucket"}],"Version":"2012-10-17"}`
	wantDocument := `{"Version":"2012-10-17","Statement":[{"Action":"s3:ListBucket",` +
		`"Effect":"Allow","Resource":"*"}]}`

	fake.on("PutUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		assert.Equal(t, wantDocument, form.Get("PolicyDocument"))
		return 200, putUserPolicyResponseXML
	})
	fake.on("GetUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		return 200, getUserPolicyResponseXML("test-user", "test-inline", storedDocument)
	})

	out, err := (&UserPolicy{
		UserName:       "test-user",
		PolicyName:     "test-inline",
		PolicyDocument: inputDocument,
	}).Create(context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &UserPolicyOutput{
		UserName:       "test-user",
		PolicyName:     "test-inline",
		PolicyDocument: wantDocument,
	}, out)
}

func TestUserPolicyUpdateOnlyPutsWhenDocumentUnchanged(t *testing.T) {
	fake := newFakeIAM(t)
	document := `{"Version":"2012-10-17","Statement":[]}`
	fake.on("GetUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		return 200, getUserPolicyResponseXML("test-user", "test-inline", document)
	})

	prior := runtime.Prior[UserPolicy, *UserPolicyOutput]{
		Inputs: UserPolicy{
			UserName:       "test-user",
			PolicyName:     "test-inline",
			PolicyDocument: document,
		},
		Outputs: &UserPolicyOutput{
			UserName:       "test-user",
			PolicyName:     "test-inline",
			PolicyDocument: document,
		},
		Observed: &UserPolicyOutput{
			UserName:       "test-user",
			PolicyName:     "test-inline",
			PolicyDocument: document,
		},
	}
	out, err := (&UserPolicy{
		UserName:       "test-user",
		PolicyName:     "test-inline",
		PolicyDocument: document,
	}).Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, document, out.PolicyDocument)
	assert.Empty(t, fake.sent("PutUserPolicy"))
}

func TestUserPolicyUpdateNormalizesChangedDocument(t *testing.T) {
	fake := newFakeIAM(t)
	newDocument := `{"Statement":[],"Version":"2012-10-17"}`
	wantDocument := `{"Version":"2012-10-17","Statement":[]}`
	fake.on("PutUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, wantDocument, form.Get("PolicyDocument"))
		return 200, putUserPolicyResponseXML
	})
	fake.on("GetUserPolicy", func(_ int, _ url.Values) (int, string) {
		return 200, getUserPolicyResponseXML("test-user", "test-inline", newDocument)
	})

	prior := runtime.Prior[UserPolicy, *UserPolicyOutput]{
		Inputs: UserPolicy{
			UserName:       "test-user",
			PolicyName:     "test-inline",
			PolicyDocument: `{"Version":"2012-10-17","Statement":[]}`,
		},
		Outputs: &UserPolicyOutput{UserName: "test-user", PolicyName: "test-inline"},
	}
	out, err := (&UserPolicy{
		UserName:       "test-user",
		PolicyName:     "test-inline",
		PolicyDocument: newDocument,
	}).Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, wantDocument, out.PolicyDocument)
}

func TestUserPolicyUpdateReconcilesDocumentDrift(t *testing.T) {
	fake := newFakeIAM(t)
	desiredDocument := `{"Version":"2012-10-17","Statement":[]}`
	driftedDocument := `{"Version":"2012-10-17","Statement":` +
		`[{"Effect":"Deny","Action":"*","Resource":"*"}]}`
	fake.on("PutUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		assert.Equal(t, desiredDocument, form.Get("PolicyDocument"))
		return 200, putUserPolicyResponseXML
	})
	fake.on("GetUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, "test-inline", form.Get("PolicyName"))
		return 200, getUserPolicyResponseXML("test-user", "test-inline", desiredDocument)
	})

	prior := runtime.Prior[UserPolicy, *UserPolicyOutput]{
		Inputs: UserPolicy{
			UserName:       "test-user",
			PolicyName:     "test-inline",
			PolicyDocument: desiredDocument,
		},
		Outputs: &UserPolicyOutput{
			UserName:       "test-user",
			PolicyName:     "test-inline",
			PolicyDocument: desiredDocument,
		},
		Observed: &UserPolicyOutput{
			UserName:       "test-user",
			PolicyName:     "test-inline",
			PolicyDocument: driftedDocument,
		},
	}
	out, err := (&UserPolicy{
		UserName:       "test-user",
		PolicyName:     "test-inline",
		PolicyDocument: desiredDocument,
	}).Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, desiredDocument, out.PolicyDocument)
}

func TestUserPolicyReadMapsMissingPolicyToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetUserPolicy", func(_ int, _ url.Values) (int, string) {
		return 404, noSuchEntityXML
	})

	_, err := (&UserPolicy{}).Read(context.Background(), fake.configuration(), &UserPolicyOutput{
		UserName:   "test-user",
		PolicyName: "test-inline",
	})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestUserPolicyReadMapsNilPolicyDocumentToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetUserPolicy", func(_ int, _ url.Values) (int, string) {
		return 200, emptyGetUserPolicyResponseXML
	})

	_, err := (&UserPolicy{}).Read(context.Background(), fake.configuration(), &UserPolicyOutput{
		UserName:   "test-user",
		PolicyName: "test-inline",
	})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestUserPolicyDeleteUsesPriorIdentityAndIgnoresNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("DeleteUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "old-user", form.Get("UserName"))
		assert.Equal(t, "old-inline", form.Get("PolicyName"))
		return 404, noSuchEntityXML
	})

	err := (&UserPolicy{UserName: "new-user", PolicyName: "new-inline"}).Delete(
		context.Background(), fake.configuration(),
		&UserPolicyOutput{UserName: "old-user", PolicyName: "old-inline"})
	require.NoError(t, err)
}

func getUserPolicyResponseXML(user, policyName, document string) string {
	return fmt.Sprintf(`
<GetUserPolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <GetUserPolicyResult>
    <UserName>%s</UserName>
    <PolicyName>%s</PolicyName>
    <PolicyDocument>%s</PolicyDocument>
  </GetUserPolicyResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetUserPolicyResponse>`, user, policyName, url.QueryEscape(document))
}
