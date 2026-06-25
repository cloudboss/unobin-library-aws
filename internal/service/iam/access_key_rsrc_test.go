package iam

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessKeyCreateReturnsPlaintextSecretAndSetsInactive(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("CreateAccessKey", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Empty(t, form.Get("Status"))
		return 200, createAccessKeyResponseXML(
			"test-user", "AKIATEST", "test-secret", "Active")
	})
	fake.on("UpdateAccessKey", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, "AKIATEST", form.Get("AccessKeyId"))
		assert.Equal(t, "Inactive", form.Get("Status"))
		return 200, emptyIAMResultXML("UpdateAccessKey")
	})

	out, err := (&AccessKey{UserName: "test-user", Status: "Inactive"}).Create(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, "AKIATEST", out.AccessKeyId)
	assert.Equal(t, "test-user", out.UserName)
	assert.Equal(t, "2024-01-02T03:04:05Z", out.CreateDate)
	assert.Equal(t, "Inactive", out.Status)
	require.NotNil(t, out.Secret)
	assert.Equal(t, "test-secret", *out.Secret)
	require.NotNil(t, out.SesSmtpPasswordV4)
	assert.Equal(t, "BAx+YEGKP2aq+tRuKLJq9jUwAsKrwOiHHot1+tip//wq",
		*out.SesSmtpPasswordV4)
	assert.Nil(t, out.EncryptedSecret)
	assert.Nil(t, out.EncryptedSesSmtpPasswordV4)
	assert.Nil(t, out.KeyFingerprint)
}

func TestAccessKeyCreateUsesDefaultActiveStatus(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("CreateAccessKey", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		return 200, createAccessKeyResponseXML(
			"test-user", "AKIAACTIVE", "test-secret", "Active")
	})

	out, err := (&AccessKey{UserName: "test-user"}).Create(
		context.Background(), fake.configuration())
	require.NoError(t, err)
	assert.Equal(t, "AKIAACTIVE", out.AccessKeyId)
	assert.Equal(t, "test-user", out.UserName)
	assert.Equal(t, "Active", out.Status)
	assert.Empty(t, fake.sent("UpdateAccessKey"))
}

func TestAccessKeyCreateRequiresSecret(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("CreateAccessKey", func(_ int, _ url.Values) (int, string) {
		return 200, createAccessKeyResponseXML("test-user", "AKIATEST", "", "Active")
	})

	_, err := (&AccessKey{UserName: "test-user"}).Create(context.Background(), fake.configuration())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret access key")
}

func TestAccessKeyReadPaginatesAndPreservesSecretOutputs(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("ListAccessKeys", func(n int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		if n == 1 {
			assert.Empty(t, form.Get("Marker"))
			return 200, listAccessKeysPageXML(true, "next",
				accessKeyMetadataXML("", "", ""),
				accessKeyMetadataXML("test-user", "AKIAOTHER", "Active"))
		}
		assert.Equal(t, "next", form.Get("Marker"))
		return 200, listAccessKeysPageXML(false, "",
			accessKeyMetadataXML("test-user", "AKIAMATCH", "Inactive"))
	})
	prior := &AccessKeyOutput{
		AccessKeyId:                "AKIAMATCH",
		UserName:                   "test-user",
		Secret:                     aws.String("secret"),
		SesSmtpPasswordV4:          aws.String("smtp"),
		EncryptedSecret:            aws.String("encrypted-secret"),
		EncryptedSesSmtpPasswordV4: aws.String("encrypted-smtp"),
		KeyFingerprint:             aws.String("fingerprint"),
	}

	out, err := (&AccessKey{UserName: "desired-user"}).Read(
		context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, "AKIAMATCH", out.AccessKeyId)
	assert.Equal(t, "test-user", out.UserName)
	assert.Equal(t, "2024-01-02T03:04:05Z", out.CreateDate)
	assert.Equal(t, "Inactive", out.Status)
	require.NotNil(t, out.Secret)
	assert.Equal(t, "secret", *out.Secret)
	require.NotNil(t, out.SesSmtpPasswordV4)
	assert.Equal(t, "smtp", *out.SesSmtpPasswordV4)
	require.NotNil(t, out.EncryptedSecret)
	assert.Equal(t, "encrypted-secret", *out.EncryptedSecret)
	require.NotNil(t, out.EncryptedSesSmtpPasswordV4)
	assert.Equal(t, "encrypted-smtp", *out.EncryptedSesSmtpPasswordV4)
	require.NotNil(t, out.KeyFingerprint)
	assert.Equal(t, "fingerprint", *out.KeyFingerprint)
}

func TestAccessKeyReadMapsNoSuchEntityToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("ListAccessKeys", func(_ int, _ url.Values) (int, string) {
		return 400, noSuchEntityResponseXML()
	})

	_, err := (&AccessKey{UserName: "missing-user"}).Read(
		context.Background(), fake.configuration(), &AccessKeyOutput{AccessKeyId: "AKIAMISS"})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestAccessKeyReadMapsMissingKeyToNotFound(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("ListAccessKeys", func(_ int, _ url.Values) (int, string) {
		return 200, listAccessKeysPageXML(false, "",
			accessKeyMetadataXML("test-user", "AKIAOTHER", "Active"))
	})

	_, err := (&AccessKey{UserName: "test-user"}).Read(
		context.Background(), fake.configuration(), &AccessKeyOutput{AccessKeyId: "AKIAMISS"})
	assert.True(t, errors.Is(err, runtime.ErrNotFound))
}

func TestAccessKeyUpdateReconcilesObservedStatusDrift(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("UpdateAccessKey", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		assert.Equal(t, "AKIATEST", form.Get("AccessKeyId"))
		assert.Equal(t, "Active", form.Get("Status"))
		return 200, emptyIAMResultXML("UpdateAccessKey")
	})
	fake.on("ListAccessKeys", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "test-user", form.Get("UserName"))
		return 200, listAccessKeysPageXML(false, "",
			accessKeyMetadataXML("test-user", "AKIATEST", "Active"))
	})
	prior := runtime.Prior[AccessKey, *AccessKeyOutput]{
		Inputs: AccessKey{UserName: "test-user", Status: "Active"},
		Outputs: &AccessKeyOutput{
			AccessKeyId:       "AKIATEST",
			UserName:          "test-user",
			Status:            "Active",
			Secret:            aws.String("secret"),
			SesSmtpPasswordV4: aws.String("smtp"),
		},
		Observed: &AccessKeyOutput{AccessKeyId: "AKIATEST", UserName: "test-user", Status: "Inactive"},
	}

	out, err := (&AccessKey{UserName: "test-user", Status: "Active"}).Update(
		context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Equal(t, "Active", out.Status)
	require.NotNil(t, out.Secret)
	assert.Equal(t, "secret", *out.Secret)
	require.NotNil(t, out.SesSmtpPasswordV4)
	assert.Equal(t, "smtp", *out.SesSmtpPasswordV4)
}

func TestAccessKeyUpdateReturnsObservedWhenStatusMatches(t *testing.T) {
	fake := newFakeIAM(t)
	observed := &AccessKeyOutput{AccessKeyId: "AKIATEST", UserName: "test-user", Status: "Active"}
	prior := runtime.Prior[AccessKey, *AccessKeyOutput]{
		Inputs:   AccessKey{UserName: "test-user", Status: "Active"},
		Outputs:  &AccessKeyOutput{AccessKeyId: "AKIATEST", UserName: "test-user", Status: "Active"},
		Observed: observed,
	}

	out, err := (&AccessKey{UserName: "test-user", Status: "Active"}).Update(
		context.Background(), fake.configuration(), prior)
	require.NoError(t, err)
	assert.Same(t, observed, out)
	assert.Empty(t, fake.sent("UpdateAccessKey"))
}

func TestAccessKeyDeleteUsesPriorUserNameAndTreatsNoSuchEntityAsSuccess(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("DeleteAccessKey", func(_ int, form url.Values) (int, string) {
		assert.Equal(t, "old-user", form.Get("UserName"))
		assert.Equal(t, "AKIATEST", form.Get("AccessKeyId"))
		return 400, noSuchEntityResponseXML()
	})

	err := (&AccessKey{UserName: "new-user"}).Delete(
		context.Background(), fake.configuration(),
		&AccessKeyOutput{AccessKeyId: "AKIATEST", UserName: "old-user"})
	require.NoError(t, err)
}

func TestAccessKeySESSMTPPasswordV4(t *testing.T) {
	got := accessKeySESSMTPPasswordV4(
		"wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "us-east-1")
	assert.Equal(t, "BOntiZFm/r+5s3psZ/RpsjB+aSGsj2J0rXdiLuO0cQL7", got)
}

func TestEncryptAccessKeyValuesWithPgpKey(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is not installed")
	}
	home := t.TempDir()
	require.NoError(t, os.Chmod(home, 0o700))
	runTestGPG(t, home, nil,
		"--pinentry-mode", "loopback",
		"--passphrase", "",
		"--quick-generate-key", "Unobin Test <unobin@example.com>", "rsa2048", "encrypt", "1d")
	publicKey := runTestGPG(t, home, nil, "--export")
	encodedKey := base64.StdEncoding.EncodeToString(publicKey)
	entity, err := parseAccessKeyPGPKey(publicKey)
	require.NoError(t, err)
	fingerprint := fmt.Sprintf("%x", entity.PrimaryKey.Fingerprint)

	encrypted, err := encryptAccessKeyValues(
		context.Background(), encodedKey, "created-secret", "smtp-password")
	require.NoError(t, err)
	assert.Equal(t, fingerprint, encrypted.KeyFingerprint)
	assert.Equal(t, "created-secret", decryptTestGPG(t, home, encrypted.Secret))
	assert.Equal(t, "smtp-password", decryptTestGPG(t, home, encrypted.SesSmtpPasswordV4))
}

func createAccessKeyResponseXML(
	userName string, keyID string, secret string, status string,
) string {
	secretXML := ""
	if secret != "" {
		secretXML = fmt.Sprintf("<SecretAccessKey>%s</SecretAccessKey>", secret)
	}
	return fmt.Sprintf(`
<CreateAccessKeyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <CreateAccessKeyResult>
    <AccessKey>
      <UserName>%s</UserName>
      <AccessKeyId>%s</AccessKeyId>
      <Status>%s</Status>
      %s
      <CreateDate>2024-01-02T03:04:05Z</CreateDate>
    </AccessKey>
  </CreateAccessKeyResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</CreateAccessKeyResponse>`, userName, keyID, status, secretXML)
}

func listAccessKeysPageXML(truncated bool, marker string, members ...string) string {
	markerXML := ""
	if marker != "" {
		markerXML = fmt.Sprintf("<Marker>%s</Marker>", marker)
	}
	return fmt.Sprintf(`
<ListAccessKeysResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <ListAccessKeysResult>
    <AccessKeyMetadata>%s</AccessKeyMetadata>
    <IsTruncated>%t</IsTruncated>
    %s
  </ListAccessKeysResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</ListAccessKeysResponse>`, strings.Join(members, ""), truncated, markerXML)
}

func accessKeyMetadataXML(userName string, keyID string, status string) string {
	if userName == "" && keyID == "" && status == "" {
		return "<member/>"
	}
	return fmt.Sprintf(`<member>
<UserName>%s</UserName>
<AccessKeyId>%s</AccessKeyId>
<Status>%s</Status>
<CreateDate>2024-01-02T03:04:05Z</CreateDate>
</member>`, userName, keyID, status)
}

func noSuchEntityResponseXML() string {
	return `
<ErrorResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">
  <Error>
    <Type>Sender</Type>
    <Code>NoSuchEntity</Code>
    <Message>not found</Message>
  </Error>
  <RequestId>req-1</RequestId>
</ErrorResponse>`
}

func decryptTestGPG(t *testing.T, home string, encoded string) string {
	t.Helper()
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	out := runTestGPG(t, home, ciphertext, "--decrypt")
	return string(out)
}

func runTestGPG(t *testing.T, home string, stdin []byte, args ...string) []byte {
	t.Helper()
	baseArgs := []string{"--homedir", home, "--batch", "--yes", "--no-tty"}
	cmd := exec.Command("gpg", append(baseArgs, args...)...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gpg %v: %v: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return out
}
