package iam

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/aws/aws-sdk-go-v2/aws"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	accessKeyDefaultStatus = string(iamtypes.StatusTypeActive)
	accessKeyInactive      = string(iamtypes.StatusTypeInactive)
	accessKeyKeybasePrefix = "keybase:"
	accessKeyMaxPGPKeySize = 1 << 20
)

// AccessKey manages an IAM user's access key. IAM only returns the secret when
// creating the key, so Read preserves the create-only secret outputs from the
// prior state while refreshing the key metadata IAM can still report.
type AccessKey struct {
	UserName string `ub:"user-name"`
	PgpKey   string `ub:"pgp-key"`
	Status   string `ub:"status"`
}

// AccessKeyOutput holds the access-key metadata IAM reports and the secret
// material captured at create time. Plaintext secret fields are sensitive; when
// pgp-key is set, only encrypted secret fields are returned.
type AccessKeyOutput struct {
	AccessKeyId                string  `ub:"access-key-id"`
	UserName                   string  `ub:"user-name"`
	CreateDate                 string  `ub:"create-date"`
	Status                     string  `ub:"status"`
	Secret                     *string `ub:"secret,sensitive"`
	SesSmtpPasswordV4          *string `ub:"ses-smtp-password-v4,sensitive"`
	EncryptedSecret            *string `ub:"encrypted-secret"`
	EncryptedSesSmtpPasswordV4 *string `ub:"encrypted-ses-smtp-password-v4"`
	KeyFingerprint             *string `ub:"key-fingerprint"`
}

func (r *AccessKey) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that require a new access key. IAM cannot move
// an access key to another user, and pgp-key controls create-only outputs that
// cannot be recomputed after the secret is gone.
func (r *AccessKey) ReplaceFields() []string {
	return []string{"user-name", "pgp-key"}
}

// Defaults gives status the IAM create default.
func (r AccessKey) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.PgpKey, ""),
		defaults.Value(r.Status, "Active"),
	}
}

// Constraints declares the access-key status enum supported by IAM for active
// keys.
func (r AccessKey) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.Status, "Active", "Inactive")).
			Message("status must be Active or Inactive"),
	}
}

func (r *AccessKey) Create(ctx context.Context, cfg *awsCfg) (*AccessKeyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateAccessKey(ctx, &iam.CreateAccessKeyInput{
		UserName: aws.String(r.UserName),
	})
	if err != nil {
		return nil, fmt.Errorf("create access key: %w", err)
	}
	if resp == nil || resp.AccessKey == nil {
		return nil, errors.New("create access key: response holds no access key")
	}
	key := resp.AccessKey
	if aws.ToString(key.AccessKeyId) == "" {
		return nil, errors.New("create access key: response holds no access key id")
	}
	secret := aws.ToString(key.SecretAccessKey)
	if secret == "" {
		return nil, errors.New("create access key: response holds no secret access key")
	}
	out, err := r.outputFromCreate(ctx, client, key, secret)
	if err != nil {
		return nil, err
	}
	if r.desiredStatus() == accessKeyInactive {
		if err := r.updateStatus(ctx, client, out.UserName, out.AccessKeyId, accessKeyInactive); err != nil {
			return nil, err
		}
		out.Status = accessKeyInactive
	}
	return out, nil
}

func (r *AccessKey) Read(
	ctx context.Context, cfg *awsCfg, prior *AccessKeyOutput,
) (*AccessKeyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	metadata, err := findAccessKey(ctx, client,
		accessKeyOutputUserName(prior, r.UserName), accessKeyOutputID(prior))
	if err != nil {
		return nil, err
	}
	out := outputFromAccessKeyMetadata(metadata)
	preserveAccessKeySecretOutputs(out, prior)
	return out, nil
}

func (r *AccessKey) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[AccessKey, *AccessKeyOutput],
) (*AccessKeyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	desired := r.desiredStatus()
	if accessKeyPriorStatus(prior) != desired {
		if err := r.updateStatus(ctx, client,
			accessKeyPriorUserName(prior, r.UserName), accessKeyPriorID(prior), desired); err != nil {
			return nil, err
		}
		return r.readAfterUpdate(ctx, client, prior)
	}
	if prior.Observed != nil {
		return prior.Observed, nil
	}
	if prior.Outputs != nil {
		return prior.Outputs, nil
	}
	return nil, errors.New("update access key: missing prior output")
}

func (r *AccessKey) Delete(ctx context.Context, cfg *awsCfg, prior *AccessKeyOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteAccessKey(ctx, &iam.DeleteAccessKeyInput{
		AccessKeyId: aws.String(accessKeyOutputID(prior)),
		UserName:    aws.String(accessKeyOutputUserName(prior, r.UserName)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete access key: %w", err)
	}
	return nil
}

func (r *AccessKey) outputFromCreate(
	ctx context.Context, client *iam.Client, key *iamtypes.AccessKey, secret string,
) (*AccessKeyOutput, error) {
	out := &AccessKeyOutput{
		AccessKeyId: aws.ToString(key.AccessKeyId),
		UserName:    accessKeyUserName(key, r.UserName),
		CreateDate:  accessKeyDate(key.CreateDate),
		Status:      string(key.Status),
	}
	if out.Status == "" {
		out.Status = accessKeyDefaultStatus
	}
	smtpPassword := accessKeySESSMTPPasswordV4(secret, region(client))
	if r.PgpKey == "" {
		out.Secret = aws.String(secret)
		out.SesSmtpPasswordV4 = aws.String(smtpPassword)
		return out, nil
	}
	encrypted, err := encryptAccessKeyValues(ctx, r.PgpKey, secret, smtpPassword)
	if err != nil {
		return nil, err
	}
	out.EncryptedSecret = aws.String(encrypted.Secret)
	out.EncryptedSesSmtpPasswordV4 = aws.String(encrypted.SesSmtpPasswordV4)
	out.KeyFingerprint = aws.String(encrypted.KeyFingerprint)
	return out, nil
}

func (r *AccessKey) readAfterUpdate(
	ctx context.Context, client *iam.Client, prior runtime.Prior[AccessKey, *AccessKeyOutput],
) (*AccessKeyOutput, error) {
	secretPrior := prior.Outputs
	if secretPrior == nil {
		secretPrior = prior.Observed
	}
	metadata, err := findAccessKey(ctx, client,
		accessKeyPriorUserName(prior, r.UserName), accessKeyPriorID(prior))
	if err != nil {
		return nil, err
	}
	out := outputFromAccessKeyMetadata(metadata)
	preserveAccessKeySecretOutputs(out, secretPrior)
	return out, nil
}

func (r *AccessKey) updateStatus(
	ctx context.Context, client *iam.Client, userName string, accessKeyID string, status string,
) error {
	_, err := client.UpdateAccessKey(ctx, &iam.UpdateAccessKeyInput{
		AccessKeyId: aws.String(accessKeyID),
		UserName:    aws.String(userName),
		Status:      iamtypes.StatusType(status),
	})
	if err != nil {
		return fmt.Errorf("update access key status: %w", err)
	}
	return nil
}

func (r *AccessKey) desiredStatus() string {
	if r.Status == "" {
		return accessKeyDefaultStatus
	}
	return r.Status
}

func findAccessKey(
	ctx context.Context, client *iam.Client, userName string, accessKeyID string,
) (*iamtypes.AccessKeyMetadata, error) {
	pager := iam.NewListAccessKeysPaginator(client, &iam.ListAccessKeysInput{
		UserName: aws.String(userName),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("list access keys: %w", err)
		}
		for i := range page.AccessKeyMetadata {
			metadata := &page.AccessKeyMetadata[i]
			if aws.ToString(metadata.AccessKeyId) == "" {
				continue
			}
			if aws.ToString(metadata.AccessKeyId) == accessKeyID {
				return metadata, nil
			}
		}
	}
	return nil, runtime.ErrNotFound
}

func accessKeyUserName(key *iamtypes.AccessKey, fallback string) string {
	if key != nil && key.UserName != nil {
		return aws.ToString(key.UserName)
	}
	return fallback
}

func outputFromAccessKeyMetadata(metadata *iamtypes.AccessKeyMetadata) *AccessKeyOutput {
	return &AccessKeyOutput{
		AccessKeyId: aws.ToString(metadata.AccessKeyId),
		UserName:    aws.ToString(metadata.UserName),
		CreateDate:  accessKeyDate(metadata.CreateDate),
		Status:      string(metadata.Status),
	}
}

func preserveAccessKeySecretOutputs(out *AccessKeyOutput, prior *AccessKeyOutput) {
	if prior == nil {
		return
	}
	out.Secret = copyAccessKeyString(prior.Secret)
	out.SesSmtpPasswordV4 = copyAccessKeyString(prior.SesSmtpPasswordV4)
	out.EncryptedSecret = copyAccessKeyString(prior.EncryptedSecret)
	out.EncryptedSesSmtpPasswordV4 = copyAccessKeyString(prior.EncryptedSesSmtpPasswordV4)
	out.KeyFingerprint = copyAccessKeyString(prior.KeyFingerprint)
}

func copyAccessKeyString(v *string) *string {
	if v == nil {
		return nil
	}
	return aws.String(*v)
}

func accessKeyOutputID(out *AccessKeyOutput) string {
	if out == nil {
		return ""
	}
	return out.AccessKeyId
}

func accessKeyOutputUserName(out *AccessKeyOutput, fallback string) string {
	if out != nil && out.UserName != "" {
		return out.UserName
	}
	return fallback
}

func accessKeyPriorID(prior runtime.Prior[AccessKey, *AccessKeyOutput]) string {
	if prior.Outputs != nil && prior.Outputs.AccessKeyId != "" {
		return prior.Outputs.AccessKeyId
	}
	if prior.Observed != nil {
		return prior.Observed.AccessKeyId
	}
	return ""
}

func accessKeyPriorUserName(
	prior runtime.Prior[AccessKey, *AccessKeyOutput], fallback string,
) string {
	if prior.Outputs != nil && prior.Outputs.UserName != "" {
		return prior.Outputs.UserName
	}
	if prior.Observed != nil && prior.Observed.UserName != "" {
		return prior.Observed.UserName
	}
	if prior.Inputs.UserName != "" {
		return prior.Inputs.UserName
	}
	return fallback
}

func accessKeyPriorStatus(prior runtime.Prior[AccessKey, *AccessKeyOutput]) string {
	if prior.Observed != nil && prior.Observed.Status != "" {
		return prior.Observed.Status
	}
	if prior.Outputs != nil && prior.Outputs.Status != "" {
		return prior.Outputs.Status
	}
	if prior.Inputs.Status != "" {
		return prior.Inputs.Status
	}
	return accessKeyDefaultStatus
}

func accessKeyDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func accessKeySESSMTPPasswordV4(secret string, region string) string {
	key := []byte("AWS4" + secret)
	for _, message := range []string{"11111111", region, "ses", "aws4_request", "SendRawEmail"} {
		key = accessKeyHMACSHA256(key, message)
	}
	password := make([]byte, 1, 1+len(key))
	password[0] = 0x04
	password = append(password, key...)
	return base64.StdEncoding.EncodeToString(password)
}

func accessKeyHMACSHA256(key []byte, message string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(message))
	return mac.Sum(nil)
}

type encryptedAccessKeyValues struct {
	Secret            string
	SesSmtpPasswordV4 string
	KeyFingerprint    string
}

func encryptAccessKeyValues(
	ctx context.Context, pgpKey string, secret string, smtpPassword string,
) (*encryptedAccessKeyValues, error) {
	keyData, err := resolveAccessKeyPgpKey(ctx, pgpKey)
	if err != nil {
		return nil, err
	}
	entity, err := parseAccessKeyPGPKey(keyData)
	if err != nil {
		return nil, err
	}
	if entity == nil || entity.PrimaryKey == nil {
		return nil, errors.New("pgp-key contains no public key")
	}
	fingerprint := fmt.Sprintf("%x", entity.PrimaryKey.Fingerprint)
	encryptedSecret, err := encryptAccessKeyValue(entity, secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt secret: %w", err)
	}
	encryptedSMTPPassword, err := encryptAccessKeyValue(entity, smtpPassword)
	if err != nil {
		return nil, fmt.Errorf("encrypt ses smtp password v4: %w", err)
	}
	return &encryptedAccessKeyValues{
		Secret:            encryptedSecret,
		SesSmtpPasswordV4: encryptedSMTPPassword,
		KeyFingerprint:    fingerprint,
	}, nil
}

func resolveAccessKeyPgpKey(ctx context.Context, value string) ([]byte, error) {
	if name, ok := strings.CutPrefix(value, accessKeyKeybasePrefix); ok {
		name = strings.TrimSuffix(name, "\n")
		if name == "" {
			return nil, errors.New("pgp-key keybase username is empty")
		}
		return fetchAccessKeyKeybasePgpKey(ctx, name)
	}
	keyData, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode pgp-key: %w", err)
	}
	if len(keyData) == 0 {
		return nil, errors.New("pgp-key is empty")
	}
	return keyData, nil
}

type accessKeyKeybaseLookup struct {
	Status struct {
		Code int    `json:"code"`
		Desc string `json:"desc"`
	} `json:"status"`
	Them []struct {
		PublicKeys struct {
			Primary struct {
				Bundle string `json:"bundle"`
			} `json:"primary"`
		} `json:"public_keys"`
	} `json:"them"`
}

func fetchAccessKeyKeybasePgpKey(ctx context.Context, name string) ([]byte, error) {
	endpoint := "https://keybase.io/_/api/1.0/user/lookup.json?fields=public_keys&usernames=" +
		url.QueryEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create keybase request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch keybase pgp key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch keybase pgp key: status %s", resp.Status)
	}
	limited := io.LimitReader(resp.Body, accessKeyMaxPGPKeySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read keybase pgp key: %w", err)
	}
	if len(body) > accessKeyMaxPGPKeySize {
		return nil, errors.New("keybase pgp key response is too large")
	}
	var lookup accessKeyKeybaseLookup
	if err := json.Unmarshal(body, &lookup); err != nil {
		return nil, fmt.Errorf("decode keybase pgp key response: %w", err)
	}
	if lookup.Status.Code != 0 {
		if lookup.Status.Desc == "" {
			lookup.Status.Desc = "unknown error"
		}
		return nil, fmt.Errorf("fetch keybase pgp key: %s", lookup.Status.Desc)
	}
	if len(lookup.Them) != 1 || lookup.Them[0].PublicKeys.Primary.Bundle == "" {
		return nil, fmt.Errorf("keybase user %s has no primary pgp key", name)
	}
	entity, err := parseAccessKeyPGPKey([]byte(lookup.Them[0].PublicKeys.Primary.Bundle))
	if err != nil {
		return nil, fmt.Errorf("parse keybase pgp key: %w", err)
	}
	return serializeAccessKeyPGPEntity(entity)
}

func parseAccessKeyPGPKey(data []byte) (*openpgp.Entity, error) {
	if block, err := armor.Decode(bytes.NewReader(data)); err == nil {
		if block.Type != openpgp.PublicKeyType {
			return nil, fmt.Errorf("pgp-key armor block is %q, not %q",
				block.Type, openpgp.PublicKeyType)
		}
		return openpgp.ReadEntity(packet.NewReader(block.Body))
	}
	entity, err := openpgp.ReadEntity(packet.NewReader(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("parse pgp-key: %w", err)
	}
	return entity, nil
}

func serializeAccessKeyPGPEntity(entity *openpgp.Entity) ([]byte, error) {
	if entity == nil {
		return nil, errors.New("pgp-key contains no public key")
	}
	var buf bytes.Buffer
	if err := entity.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("serialize keybase pgp key: %w", err)
	}
	return buf.Bytes(), nil
}

func encryptAccessKeyValue(entity *openpgp.Entity, value string) (string, error) {
	var buf bytes.Buffer
	writer, err := openpgp.Encrypt(&buf, []*openpgp.Entity{entity}, nil, nil, nil)
	if err != nil {
		return "", err
	}
	if _, err := io.WriteString(writer, value); err != nil {
		_ = writer.Close()
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
