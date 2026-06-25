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

const updateGroupResponseXML = `
<UpdateGroupResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <UpdateGroupResult/>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</UpdateGroupResponse>`

const noSuchEntityXML = `
<ErrorResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <Error>
    <Type>Sender</Type>
    <Code>NoSuchEntity</Code>
    <Message>group not found</Message>
  </Error>
  <RequestId>req-1</RequestId>
</ErrorResponse>`

const emptyGetGroupResponseXML = `
<GetGroupResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <GetGroupResult>
    <Users/>
    <IsTruncated>false</IsTruncated>
  </GetGroupResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetGroupResponse>`

func TestGroupCreateDefaultsPathAndReadsSettledGroup(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("CreateGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		assert.Equal(t, "/", form.Get("Path"))
		return 200, createGroupResponseXML("test-group", "/", "AGPACREATE", "create-arn")
	})
	fake.on("GetGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-group", form.Get("GroupName"))
		return 200, getGroupResponseXML(
			"test-group", "/", "AGPASETTLED", "arn:aws:iam::123456789012:group/test-group")
	})

	out, err := (&Group{Name: "test-group", Path: "/"}).Create(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, &GroupOutput{
		Arn:      "arn:aws:iam::123456789012:group/test-group",
		UniqueId: "AGPASETTLED",
		Name:     "test-group",
	}, out)
}

func TestGroupCreatePreservesExplicitEmptyPath(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("CreateGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "", form.Get("Path"))
		return 200, createGroupResponseXML("empty-path-group", "", "AGPAEMPTY", "create-arn")
	})
	fake.on("GetGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "empty-path-group", form.Get("GroupName"))
		return 200, getGroupResponseXML(
			"empty-path-group", "", "AGPAEMPTY", "arn:aws:iam::123456789012:group/empty-path-group")
	})

	_, err := (&Group{Name: "empty-path-group"}).Create(context.Background(), fake.configuration())
	require.NoError(t, err)
}

func TestGroupReadUsesPriorOutputHandle(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "old-group", form.Get("GroupName"))
		return 200, getGroupResponseXML(
			"old-group", "/", "AGPAOLD", "arn:aws:iam::123456789012:group/old-group")
	})

	out, err := (&Group{Name: "new-group"}).Read(
		context.Background(), fake.configuration(), &GroupOutput{Name: "old-group"})
	require.NoError(t, err)
	assert.Equal(t, "old-group", out.Name)
}

func TestGroupUpdateUsesPriorHandleAndDefaultsPath(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("UpdateGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "old-group", form.Get("GroupName"))
		assert.Equal(t, "new-group", form.Get("NewGroupName"))
		assert.Equal(t, "/", form.Get("NewPath"))
		return 200, updateGroupResponseXML
	})
	fake.on("GetGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "new-group", form.Get("GroupName"))
		return 200, getGroupResponseXML(
			"new-group", "/", "AGPANEW", "arn:aws:iam::123456789012:group/new-group")
	})

	prior := runtime.Prior[Group, *GroupOutput]{
		Inputs:  Group{Name: "old-group", Path: "/"},
		Outputs: &GroupOutput{Name: "old-group"},
	}
	out, err := (&Group{Name: "new-group", Path: "/"}).Update(
		context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, &GroupOutput{
		Arn:      "arn:aws:iam::123456789012:group/new-group",
		UniqueId: "AGPANEW",
		Name:     "new-group",
	}, out)
}

func TestGroupUpdateReconcilesObservedDriftWhenInputsAreUnchanged(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("UpdateGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "same-group", form.Get("GroupName"))
		assert.Equal(t, "same-group", form.Get("NewGroupName"))
		assert.Equal(t, "/wanted/", form.Get("NewPath"))
		return 200, updateGroupResponseXML
	})
	fake.on("GetGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "same-group", form.Get("GroupName"))
		return 200, getGroupResponseXML(
			"same-group", "/wanted/", "AGPASAME", "arn:aws:iam::123456789012:group/wanted/same-group")
	})

	prior := runtime.Prior[Group, *GroupOutput]{
		Inputs:   Group{Name: "same-group", Path: "/wanted/"},
		Outputs:  &GroupOutput{Name: "same-group"},
		Observed: &GroupOutput{Name: "same-group", Arn: "arn:aws:iam::123456789012:group/drift/same-group"},
	}
	out, err := (&Group{Name: "same-group", Path: "/wanted/"}).Update(
		context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, "arn:aws:iam::123456789012:group/wanted/same-group", out.Arn)
}

func TestGroupUpdateReturnsObservedWhenOnlyUniqueIdDrifted(t *testing.T) {
	fake := newFakeIAM(t)
	observed := &GroupOutput{
		Arn:      "arn:aws:iam::123456789012:group/same-group",
		Name:     "same-group",
		UniqueId: "AGPANEW",
	}
	prior := runtime.Prior[Group, *GroupOutput]{
		Inputs: Group{Name: "same-group", Path: "/"},
		Outputs: &GroupOutput{
			Arn:      "arn:aws:iam::123456789012:group/same-group",
			Name:     "same-group",
			UniqueId: "AGPAOLD",
		},
		Observed: observed,
	}

	out, err := (&Group{Name: "same-group", Path: "/"}).Update(
		context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Same(t, observed, out)
	assert.Empty(t, fake.sent("UpdateGroup"))
}

func TestGroupDeleteTreatsNotFoundAsSuccess(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("DeleteGroup", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "old-group", form.Get("GroupName"))
		return 404, noSuchEntityXML
	})

	err := (&Group{Name: "new-group"}).Delete(
		context.Background(), fake.configuration(), &GroupOutput{Name: "old-group"})
	require.NoError(t, err)
}

func TestGroupReadMapsEmptyResultToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetGroup", func(_ int, _ url.Values) (int, string) {
		return 200, emptyGetGroupResponseXML
	})

	_, err := (&Group{Name: "missing-group"}).Read(
		context.Background(), fake.configuration(), &GroupOutput{Name: "missing-group"})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestValidateGroupName(t *testing.T) {
	assert.NoError(t, validateGroupName("abcXYZ012=,.@_-+"))
	for _, name := range []string{"", "has space", "has/slash"} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, validateGroupName(name))
		})
	}
}

func createGroupResponseXML(name, path, id, arn string) string {
	return fmt.Sprintf(`
<CreateGroupResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <CreateGroupResult>
    <Group>
      <Path>%s</Path>
      <GroupName>%s</GroupName>
      <GroupId>%s</GroupId>
      <Arn>%s</Arn>
      <CreateDate>2024-01-02T03:04:05Z</CreateDate>
    </Group>
  </CreateGroupResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</CreateGroupResponse>`, path, name, id, arn)
}

func getGroupResponseXML(name, path, id, arn string) string {
	return fmt.Sprintf(`
<GetGroupResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <GetGroupResult>
    <Group>
      <Path>%s</Path>
      <GroupName>%s</GroupName>
      <GroupId>%s</GroupId>
      <Arn>%s</Arn>
      <CreateDate>2024-01-02T03:04:05Z</CreateDate>
    </Group>
    <Users/>
    <IsTruncated>false</IsTruncated>
  </GetGroupResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetGroupResponse>`, path, name, id, arn)
}
