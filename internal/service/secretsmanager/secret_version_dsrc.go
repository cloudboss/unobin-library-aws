package secretsmanager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/cloudboss/unobin/pkg/defaults"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

const secretVersionDataPropagationTimeout = 2 * time.Minute

var errSecretVersionDataNotFound = errors.New("secret version not found")

// SecretVersionData reads one Secrets Manager value version with GetSecretValue.
// When version-id is set, Read uses it alone and does not send version-stage.
// Without version-id, version-stage defaults to AWSCURRENT.
type SecretVersionData struct {
	SecretId     string  `ub:"secret-id"`
	VersionId    *string `ub:"version-id"`
	VersionStage *string `ub:"version-stage"`
}

// SecretVersionDataOutput holds the value and metadata returned by GetSecretValue.
// SecretBinary is the raw bytes converted to a string; it is not base64 encoded.
// CreatedDate is formatted as RFC3339 when Secrets Manager returns it.
type SecretVersionDataOutput struct {
	Arn           string   `ub:"arn"`
	CreatedDate   string   `ub:"created-date"`
	Name          string   `ub:"name"`
	SecretBinary  string   `ub:"secret-binary,sensitive"`
	SecretString  string   `ub:"secret-string,sensitive"`
	VersionId     string   `ub:"version-id"`
	VersionStages []string `ub:"version-stages"`
}

// Defaults gives version-stage its AWSCURRENT default. Read also treats a nil
// VersionStage as AWSCURRENT so direct calls match runtime-defaulted calls.
func (d SecretVersionData) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.NullableValue(d.VersionStage, "AWSCURRENT"),
	}
}

func (d *SecretVersionData) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*SecretVersionDataOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := d.getSecretValue(ctx, client)
	if err != nil {
		return nil, err
	}
	out, err := secretVersionDataOutput(resp)
	if err != nil {
		return nil, d.notFoundError()
	}
	return out, nil
}

func (d *SecretVersionData) getSecretValue(
	ctx context.Context,
	client *secretsmanager.Client,
) (*secretsmanager.GetSecretValueOutput, error) {
	var resp *secretsmanager.GetSecretValueOutput
	err := retry.OnError(ctx, isSecretVersionDataNotFound, func(ctx context.Context) error {
		out, err := client.GetSecretValue(ctx, d.getInput())
		if err != nil {
			return err
		}
		if secretVersionDataEmpty(out) {
			return errSecretVersionDataNotFound
		}
		resp = out
		return nil
	}, retry.WithTimeout(secretVersionDataPropagationTimeout))
	if err != nil {
		if isSecretVersionDataNotFound(err) {
			return nil, d.notFoundError()
		}
		return nil, fmt.Errorf("get secret value: %w", err)
	}
	return resp, nil
}

func (d *SecretVersionData) getInput() *secretsmanager.GetSecretValueInput {
	in := &secretsmanager.GetSecretValueInput{SecretId: aws.String(d.SecretId)}
	if versionID, ok := d.versionID(); ok {
		in.VersionId = aws.String(versionID)
		return in
	}
	in.VersionStage = aws.String(d.versionStage())
	return in
}

func (d *SecretVersionData) versionID() (string, bool) {
	if d.VersionId == nil || *d.VersionId == "" {
		return "", false
	}
	return *d.VersionId, true
}

func (d *SecretVersionData) versionStage() string {
	if d.VersionStage == nil || *d.VersionStage == "" {
		return currentStage
	}
	return *d.VersionStage
}

func (d *SecretVersionData) notFoundError() error {
	if versionID, ok := d.versionID(); ok {
		return fmt.Errorf("secretsmanager secret version %q for secret %q not found",
			versionID, d.SecretId)
	}
	return fmt.Errorf("secretsmanager secret version with stage %q for secret %q not found",
		d.versionStage(), d.SecretId)
}

func secretVersionDataOutput(
	resp *secretsmanager.GetSecretValueOutput,
) (*SecretVersionDataOutput, error) {
	if secretVersionDataEmpty(resp) {
		return nil, errSecretVersionDataNotFound
	}
	createdDate := ""
	if resp.CreatedDate != nil {
		createdDate = resp.CreatedDate.Format(time.RFC3339)
	}
	return &SecretVersionDataOutput{
		Arn:           aws.ToString(resp.ARN),
		CreatedDate:   createdDate,
		Name:          aws.ToString(resp.Name),
		SecretBinary:  string(resp.SecretBinary),
		SecretString:  aws.ToString(resp.SecretString),
		VersionId:     aws.ToString(resp.VersionId),
		VersionStages: secretVersionStages(resp.VersionStages),
	}, nil
}

func secretVersionDataEmpty(resp *secretsmanager.GetSecretValueOutput) bool {
	if resp == nil {
		return true
	}
	return resp.ARN == nil &&
		resp.CreatedDate == nil &&
		resp.Name == nil &&
		resp.SecretBinary == nil &&
		resp.SecretString == nil &&
		resp.VersionId == nil &&
		len(resp.VersionStages) == 0
}

func isSecretVersionDataNotFound(err error) bool {
	return errors.Is(err, errSecretVersionDataNotFound) || isValueGone(err)
}
