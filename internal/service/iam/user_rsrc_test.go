package iam

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unsupportedOperationXML = `
<ErrorResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <Error>
    <Type>Sender</Type>
    <Code>UnsupportedOperation</Code>
    <Message>tagging is not supported</Message>
  </Error>
  <RequestId>req-1</RequestId>
</ErrorResponse>`

const emptyGetUserResponseXML = `
<GetUserResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <GetUserResult/>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetUserResponse>`

func TestUserCreateRetriesWithoutTagsOutsideStandardPartition(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("CreateUser", func(n int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, "/", form.Get("Path"))
		if n == 1 {
			assert.Equal(t, "Project", form.Get("Tags.member.1.Key"))
			assert.Equal(t, "unobin", form.Get("Tags.member.1.Value"))
			assert.Empty(t, form.Get("Tags.member.2.Key"))
			return 400, unsupportedOperationXML
		}
		assert.Empty(t, form.Get("Tags.member.1.Key"))
		return 200, createUserResponseXML("test-user", "/", "AIDACREATE", "create-arn", nil)
	})
	fake.on("TagUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, "Project", form.Get("Tags.member.1.Key"))
		assert.Equal(t, "unobin", form.Get("Tags.member.1.Value"))
		assert.Empty(t, form.Get("Tags.member.2.Key"))
		return 200, emptyIAMResultXML("TagUser")
	})
	fake.on("GetUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		return 200, getUserResponseXML("test-user", "/", "AIDASETTLED",
			"arn:aws:iam::123456789012:user/test-user", nil,
			map[string]string{"Project": "unobin", "aws:system": "ignored"})
	})
	cfg := fake.configuration()
	cfg.Region = aws.String("us-iso-east-1")

	out, err := (&User{
		Name: "test-user",
		Path: "/",
		Tags: new(map[string]string{"Project": "unobin", "aws:skip": "ignored"}),
	}).Create(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, &UserOutput{
		Arn:      "arn:aws:iam::123456789012:user/test-user",
		UniqueId: "AIDASETTLED",
		Name:     "test-user",
		Path:     "/",
		Tags:     map[string]string{"Project": "unobin"},
	}, out)
}

func TestUserReadMapsEmptyResultToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetUser", func(_ int, _ url.Values) (int, string) {
		return 200, emptyGetUserResponseXML
	})

	_, err := (&User{Name: "missing-user"}).Read(
		context.Background(), fake.configuration(), &UserOutput{Name: "missing-user"})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestUserUpdateReconcilesChangedFields(t *testing.T) {
	oldBoundary := "arn:aws:iam::123456789012:policy/old"
	fake := newFakeIAM(t)
	fake.on("UpdateUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "old-user", form.Get("UserName"))
		assert.Equal(t, "new-user", form.Get("NewUserName"))
		assert.Equal(t, "/new/", form.Get("NewPath"))
		return 200, emptyIAMResultXML("UpdateUser")
	})
	fake.on("DeleteUserPermissionsBoundary", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "new-user", form.Get("UserName"))
		return 200, emptyIAMResultXML("DeleteUserPermissionsBoundary")
	})
	fake.on("UntagUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "new-user", form.Get("UserName"))
		assert.Equal(t, "Remove", form.Get("TagKeys.member.1"))
		assert.Empty(t, form.Get("TagKeys.member.2"))
		return 200, emptyIAMResultXML("UntagUser")
	})
	fake.on("TagUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "new-user", form.Get("UserName"))
		assert.Equal(t, "Keep", form.Get("Tags.member.1.Key"))
		assert.Equal(t, "new", form.Get("Tags.member.1.Value"))
		assert.Empty(t, form.Get("Tags.member.2.Key"))
		return 200, emptyIAMResultXML("TagUser")
	})
	fake.on("GetUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "new-user", form.Get("UserName"))
		return 200, getUserResponseXML("new-user", "/new/", "AIDANEW",
			"arn:aws:iam::123456789012:user/new/new-user", nil,
			map[string]string{"Keep": "new"})
	})
	prior := runtime.Prior[User, *UserOutput]{
		Inputs: User{
			Name:                "old-user",
			Path:                "/old/",
			PermissionsBoundary: aws.String(oldBoundary),
			Tags: new(map[string]string{
				"Keep":      "old",
				"Remove":    "old",
				"aws:owned": "old",
			}),
		},
		Outputs: &UserOutput{Name: "old-user"},
	}

	out, err := (&User{
		Name:                "new-user",
		Path:                "/new/",
		PermissionsBoundary: aws.String(""),
		Tags: new(map[string]string{
			"Keep":       "new",
			"aws:wanted": "new",
		}),
	}).Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, "new-user", out.Name)
	assert.Equal(t, map[string]string{"Keep": "new"}, out.Tags)
}

func TestUserUpdateReconcilesObservedMutableDrift(t *testing.T) {
	desiredBoundary := "arn:aws:iam::123456789012:policy/desired"
	oldBoundary := "arn:aws:iam::123456789012:policy/old"
	fake := newFakeIAM(t)
	fake.on("UpdateUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "same-user", form.Get("UserName"))
		assert.Equal(t, "same-user", form.Get("NewUserName"))
		assert.Equal(t, "/wanted/", form.Get("NewPath"))
		return 200, emptyIAMResultXML("UpdateUser")
	})
	fake.on("PutUserPermissionsBoundary", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "same-user", form.Get("UserName"))
		assert.Equal(t, desiredBoundary, form.Get("PermissionsBoundary"))
		return 200, emptyIAMResultXML("PutUserPermissionsBoundary")
	})
	fake.on("UntagUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "same-user", form.Get("UserName"))
		assert.Equal(t, "Remove", form.Get("TagKeys.member.1"))
		assert.Empty(t, form.Get("TagKeys.member.2"))
		return 200, emptyIAMResultXML("UntagUser")
	})
	fake.on("TagUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "same-user", form.Get("UserName"))
		assert.Equal(t, "Keep", form.Get("Tags.member.1.Key"))
		assert.Equal(t, "wanted", form.Get("Tags.member.1.Value"))
		assert.Empty(t, form.Get("Tags.member.2.Key"))
		return 200, emptyIAMResultXML("TagUser")
	})
	fake.on("GetUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "same-user", form.Get("UserName"))
		return 200, getUserResponseXML("same-user", "/wanted/", "AIDASAME",
			"arn:aws:iam::123456789012:user/wanted/same-user", aws.String(desiredBoundary),
			map[string]string{"Keep": "wanted", "aws:system": "ignored"})
	})
	prior := runtime.Prior[User, *UserOutput]{
		Inputs: User{
			Name:                "same-user",
			Path:                "/wanted/",
			PermissionsBoundary: aws.String(desiredBoundary),
			Tags:                new(map[string]string{"Keep": "wanted"}),
		},
		Outputs: &UserOutput{Name: "same-user"},
		Observed: &UserOutput{
			Name:                "same-user",
			Path:                "/drift/",
			PermissionsBoundary: aws.String(oldBoundary),
			Tags: map[string]string{
				"Keep":   "drift",
				"Remove": "old",
			},
		},
	}

	out, err := (&User{
		Name:                "same-user",
		Path:                "/wanted/",
		PermissionsBoundary: aws.String(desiredBoundary),
		Tags:                new(map[string]string{"Keep": "wanted", "aws:desired": "ignored"}),
	}).Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, "/wanted/", out.Path)
	require.NotNil(t, out.PermissionsBoundary)
	assert.Equal(t, desiredBoundary, *out.PermissionsBoundary)
	assert.Equal(t, map[string]string{"Keep": "wanted"}, out.Tags)
}

func TestUserUpdateReturnsObservedWhenOnlyComputedFieldsChanged(t *testing.T) {
	fake := newFakeIAM(t)
	observed := &UserOutput{
		Arn:      "arn:aws:iam::123456789012:user/same",
		UniqueId: "AIDANEW",
		Name:     "same",
		Path:     "/",
		Tags:     map[string]string{"Keep": "same"},
	}
	prior := runtime.Prior[User, *UserOutput]{
		Inputs: User{Name: "same", Path: "/", Tags: new(map[string]string{"Keep": "same"})},
		Outputs: &UserOutput{
			Arn:      "old",
			UniqueId: "AIDAOLD",
			Name:     "same",
			Path:     "/",
			Tags:     map[string]string{"Keep": "same"},
		},
		Observed: observed,
	}

	out, err := (&User{Name: "same", Path: "/", Tags: new(map[string]string{"Keep": "same"})}).
		Update(context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Same(t, observed, out)
	assert.Empty(t, fake.sent("UpdateUser"))
	assert.Empty(t, fake.sent("TagUser"))
}

func TestUserDeleteRemovesGroupsBeforeDeletingUser(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("ListGroupsForUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "delete-user", form.Get("UserName"))
		return 200, listGroupsForUserResponseXML("delete-group")
	})
	fake.on("RemoveUserFromGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "delete-user", form.Get("UserName"))
		assert.Equal(t, "delete-group", form.Get("GroupName"))
		return 200, emptyIAMResultXML("RemoveUserFromGroup")
	})
	fake.on("DeleteUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "delete-user", form.Get("UserName"))
		assert.Len(t, fake.sent("RemoveUserFromGroup"), 1)
		return 200, emptyIAMResultXML("DeleteUser")
	})

	err := (&User{Name: "delete-user"}).Delete(
		context.Background(), fake.configuration(), &UserOutput{Name: "delete-user"})
	require.NoError(t, err)
}

func TestUserDeleteForceDestroyCleansDependencies(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("ListGroupsForUser", func(_ int, _ url.Values) (int, string) {
		return 200, listGroupsForUserResponseXML()
	})
	fake.on("ListUserPolicies", func(_ int, _ url.Values) (int, string) {
		return 200, listUserPoliciesResponseXML("inline")
	})
	fake.on("DeleteUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "inline", form.Get("PolicyName"))
		return 200, emptyIAMResultXML("DeleteUserPolicy")
	})
	fake.on("ListAttachedUserPolicies", func(_ int, _ url.Values) (int, string) {
		return 200, listAttachedUserPolicyCleanupResponseXML(
			"arn:aws:iam::123456789012:policy/p")
	})
	fake.on("DetachUserPolicy", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "arn:aws:iam::123456789012:policy/p", form.Get("PolicyArn"))
		return 200, emptyIAMResultXML("DetachUserPolicy")
	})
	fake.on("ListAccessKeys", func(_ int, _ url.Values) (int, string) {
		return 200, listAccessKeysResponseXML("AKIATEST")
	})
	fake.on("DeleteAccessKey", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "AKIATEST", form.Get("AccessKeyId"))
		return 200, emptyIAMResultXML("DeleteAccessKey")
	})
	fake.on("ListSSHPublicKeys", func(_ int, _ url.Values) (int, string) {
		return 200, listSSHPublicKeysResponseXML("APKATEST")
	})
	fake.on("DeleteSSHPublicKey", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "APKATEST", form.Get("SSHPublicKeyId"))
		return 200, emptyIAMResultXML("DeleteSSHPublicKey")
	})
	fake.on("ListVirtualMFADevices", func(_ int, _ url.Values) (int, string) {
		return 200, listVirtualMFADevicesResponseXML("force-user", "arn:aws:iam::123:mfa/v")
	})
	fake.on("DeleteVirtualMFADevice", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "arn:aws:iam::123:mfa/v", form.Get("SerialNumber"))
		return 200, emptyIAMResultXML("DeleteVirtualMFADevice")
	})
	fake.on("ListMFADevices", func(_ int, _ url.Values) (int, string) {
		return 200, listMFADevicesResponseXML("arn:aws:iam::123:mfa/h")
	})
	fake.on("DeactivateMFADevice", func(n int, form url.Values) (int, string) {
		want := map[int]string{1: "arn:aws:iam::123:mfa/v", 2: "arn:aws:iam::123:mfa/h"}
		assert.Equal(t, want[n], form.Get("SerialNumber"))
		return 200, emptyIAMResultXML("DeactivateMFADevice")
	})
	fake.on("DeleteLoginProfile", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "force-user", form.Get("UserName"))
		return 200, emptyIAMResultXML("DeleteLoginProfile")
	})
	fake.on("ListSigningCertificates", func(_ int, _ url.Values) (int, string) {
		return 200, listSigningCertificatesResponseXML("CERTTEST")
	})
	fake.on("DeleteSigningCertificate", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "CERTTEST", form.Get("CertificateId"))
		return 200, emptyIAMResultXML("DeleteSigningCertificate")
	})
	fake.on("ListServiceSpecificCredentials", func(_ int, _ url.Values) (int, string) {
		return 200, listServiceSpecificCredentialsResponseXML("SSCTEST")
	})
	fake.on("DeleteServiceSpecificCredential", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "SSCTEST", form.Get("ServiceSpecificCredentialId"))
		return 200, emptyIAMResultXML("DeleteServiceSpecificCredential")
	})
	fake.on("DeleteUser", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "force-user", form.Get("UserName"))
		return 200, emptyIAMResultXML("DeleteUser")
	})

	err := (&User{Name: "force-user", ForceDestroy: true}).Delete(
		context.Background(), fake.configuration(), &UserOutput{Name: "force-user"})
	require.NoError(t, err)
}

func TestUserTagDiffSkipsSystemTags(t *testing.T) {
	upsert, remove := userTagDiff(
		map[string]string{"keep": "old", "remove": "old", "aws:old": "old"},
		map[string]string{"keep": "new", "aws:new": "new"},
	)
	assert.Equal(t, map[string]string{"keep": "new"}, upsert)
	assert.Equal(t, []string{"remove"}, remove)
}

func TestValidateUser(t *testing.T) {
	assert.NoError(t, (&User{Name: "abcXYZ012=,.@_-+"}).validate())
	for _, name := range []string{"", "has space", "has/slash"} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, (&User{Name: name}).validate())
		})
	}
	boundary := make([]byte, 2049)
	for i := range boundary {
		boundary[i] = 'a'
	}
	assert.Error(t, (&User{
		Name:                "valid",
		PermissionsBoundary: aws.String(string(boundary)),
	}).validate())
}

func createUserResponseXML(
	name string, path string, id string, arn string, boundary *string,
) string {
	return fmt.Sprintf(`
<CreateUserResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <CreateUserResult>
    %s
  </CreateUserResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</CreateUserResponse>`, userXML(name, path, id, arn, boundary, nil))
}

func getUserResponseXML(
	name string, path string, id string, arn string, boundary *string, tags map[string]string,
) string {
	return fmt.Sprintf(`
<GetUserResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <GetUserResult>
    %s
  </GetUserResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetUserResponse>`, userXML(name, path, id, arn, boundary, tags))
}

func userXML(
	name string, path string, id string, arnValue string, boundary *string, tags map[string]string,
) string {
	return fmt.Sprintf(`<User>
  <Path>%s</Path>
  <UserName>%s</UserName>
  <UserId>%s</UserId>
  <Arn>%s</Arn>
  <CreateDate>2024-01-02T03:04:05Z</CreateDate>
  %s
  %s
</User>`, path, name, id, arnValue, permissionsBoundaryXML(boundary), tagsXML(tags))
}

func permissionsBoundaryXML(boundary *string) string {
	if boundary == nil {
		return ""
	}
	return fmt.Sprintf(`<PermissionsBoundary>
    <PermissionsBoundaryType>PermissionsBoundaryPolicy</PermissionsBoundaryType>
    <PermissionsBoundaryArn>%s</PermissionsBoundaryArn>
  </PermissionsBoundary>`, *boundary)
}

func tagsXML(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString("<Tags>")
	for _, k := range keys {
		fmt.Fprintf(&out, "<member><Key>%s</Key><Value>%s</Value></member>", k, tags[k])
	}
	out.WriteString("</Tags>")
	return out.String()
}

func emptyIAMResultXML(action string) string {
	return fmt.Sprintf(`
<%sResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <%sResult/>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</%sResponse>`, action, action, action)
}

func listGroupsForUserResponseXML(groups ...string) string {
	var items strings.Builder
	for _, group := range groups {
		fmt.Fprintf(&items, `<member><Path>/</Path><GroupName>%s</GroupName>
<GroupId>AGPATEST</GroupId>
<Arn>arn:aws:iam::123:group/%s</Arn></member>`, group, group)
	}
	return fmt.Sprintf(`
<ListGroupsForUserResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListGroupsForUserResult>
    <Groups>%s</Groups><IsTruncated>false</IsTruncated>
  </ListGroupsForUserResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListGroupsForUserResponse>`, items.String())
}

func listUserPoliciesResponseXML(names ...string) string {
	var items strings.Builder
	for _, name := range names {
		fmt.Fprintf(&items, "<member>%s</member>", name)
	}
	return fmt.Sprintf(`
<ListUserPoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListUserPoliciesResult>
    <PolicyNames>%s</PolicyNames><IsTruncated>false</IsTruncated>
  </ListUserPoliciesResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListUserPoliciesResponse>`, items.String())
}

func listAttachedUserPolicyCleanupResponseXML(arns ...string) string {
	var items strings.Builder
	for _, arnValue := range arns {
		fmt.Fprintf(&items, `<member><PolicyName>p</PolicyName>
<PolicyArn>%s</PolicyArn></member>`, arnValue)
	}
	return fmt.Sprintf(`
<ListAttachedUserPoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListAttachedUserPoliciesResult>
    <AttachedPolicies>%s</AttachedPolicies><IsTruncated>false</IsTruncated>
  </ListAttachedUserPoliciesResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListAttachedUserPoliciesResponse>`, items.String())
}

func listAccessKeysResponseXML(ids ...string) string {
	var items strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&items, `<member><UserName>force-user</UserName>
<AccessKeyId>%s</AccessKeyId>
<Status>Active</Status><CreateDate>2024-01-02T03:04:05Z</CreateDate></member>`, id)
	}
	return fmt.Sprintf(`
<ListAccessKeysResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListAccessKeysResult>
    <AccessKeyMetadata>%s</AccessKeyMetadata><IsTruncated>false</IsTruncated>
  </ListAccessKeysResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListAccessKeysResponse>`, items.String())
}

func listSSHPublicKeysResponseXML(ids ...string) string {
	var items strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&items, `<member><UserName>force-user</UserName>
<SSHPublicKeyId>%s</SSHPublicKeyId>
<Status>Active</Status><UploadDate>2024-01-02T03:04:05Z</UploadDate></member>`, id)
	}
	return fmt.Sprintf(`
<ListSSHPublicKeysResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListSSHPublicKeysResult>
    <SSHPublicKeys>%s</SSHPublicKeys><IsTruncated>false</IsTruncated>
  </ListSSHPublicKeysResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListSSHPublicKeysResponse>`, items.String())
}

func listVirtualMFADevicesResponseXML(name string, serial string) string {
	return fmt.Sprintf(`
<ListVirtualMFADevicesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListVirtualMFADevicesResult>
    <VirtualMFADevices><member><SerialNumber>%s</SerialNumber>
    <User><Path>/</Path><UserName>%s</UserName><UserId>AIDATEST</UserId>
    <Arn>arn:aws:iam::123:user/%s</Arn>
    <CreateDate>2024-01-02T03:04:05Z</CreateDate>
    </User></member></VirtualMFADevices><IsTruncated>false</IsTruncated>
  </ListVirtualMFADevicesResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListVirtualMFADevicesResponse>`, serial, name, name)
}

func listMFADevicesResponseXML(serials ...string) string {
	var items strings.Builder
	for _, serial := range serials {
		fmt.Fprintf(&items, `<member><UserName>force-user</UserName>
<SerialNumber>%s</SerialNumber>
<EnableDate>2024-01-02T03:04:05Z</EnableDate></member>`, serial)
	}
	return fmt.Sprintf(`
<ListMFADevicesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListMFADevicesResult>
    <MFADevices>%s</MFADevices><IsTruncated>false</IsTruncated>
  </ListMFADevicesResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListMFADevicesResponse>`, items.String())
}

func listSigningCertificatesResponseXML(ids ...string) string {
	var items strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&items, `<member><UserName>force-user</UserName>
<CertificateId>%s</CertificateId>
<CertificateBody>body</CertificateBody><Status>Active</Status></member>`, id)
	}
	return fmt.Sprintf(`
<ListSigningCertificatesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListSigningCertificatesResult>
    <Certificates>%s</Certificates><IsTruncated>false</IsTruncated>
  </ListSigningCertificatesResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListSigningCertificatesResponse>`, items.String())
}

func listServiceSpecificCredentialsResponseXML(ids ...string) string {
	var items strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&items, `<member><UserName>force-user</UserName>
<ServiceName>codecommit.amazonaws.com</ServiceName>
<ServiceSpecificCredentialId>%s</ServiceSpecificCredentialId>
<Status>Active</Status><CreateDate>2024-01-02T03:04:05Z</CreateDate></member>`, id)
	}
	return fmt.Sprintf(`
<ListServiceSpecificCredentialsResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListServiceSpecificCredentialsResult>
    <ServiceSpecificCredentials>%s</ServiceSpecificCredentials>
    <IsTruncated>false</IsTruncated>
  </ListServiceSpecificCredentialsResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListServiceSpecificCredentialsResponse>`, items.String())
}
