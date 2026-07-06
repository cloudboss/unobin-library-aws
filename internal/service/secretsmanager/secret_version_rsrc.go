package secretsmanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	secretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const previousStage = "AWSPREVIOUS"

// SecretVersionResource manages one immutable Secrets Manager value version. The secret
// id and payload create the version through PutSecretValue and cannot change in
// place; only the version-stages label set reconciles after creation with
// UpdateSecretVersionStage. A binary payload can be supplied inline as bytes in
// a string. With no explicit version stages, Secrets Manager applies its default
// labels and unobin leaves them service-managed. An explicit empty list manages
// labels and removes every removable label on update.
type SecretVersionResource struct {
	SecretId            string    `ub:"secret-id"`
	SecretBinaryContent *string   `ub:"secret-binary-content,sensitive"`
	SecretString        *string   `ub:"secret-string,sensitive"`
	VersionStages       *[]string `ub:"version-stages"`
}

// SecretVersionResourceOutput holds the server-computed identity and staging labels for
// a secret version. SecretId is stored with the returned version id so Read and
// Delete keep addressing the original version when a replacement changes the
// parent secret input.
type SecretVersionResourceOutput struct {
	SecretId      string   `ub:"secret-id"`
	Arn           string   `ub:"arn"`
	VersionId     string   `ub:"version-id"`
	VersionStages []string `ub:"version-stages"`
}

func (r *SecretVersionResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the values Secrets Manager fixes for a version. Staging
// labels are mutable; the parent secret and value bytes are not.
func (r *SecretVersionResource) ReplaceFields() []string {
	return []string{
		"secret-id",
		"secret-binary-content",
		"secret-string",
	}
}

// Constraints declares that only one payload can be used. A value is not
// required here; if none is supplied, Secrets Manager rejects the create.
func (r SecretVersionResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(
			r.SecretBinaryContent,
			r.SecretString,
		),
	}
}

func (r *SecretVersionResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*SecretVersionResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	token, err := secretVersionClientRequestToken()
	if err != nil {
		return nil, err
	}
	in := &secretsmanager.PutSecretValueInput{
		SecretId:           aws.String(r.SecretId),
		ClientRequestToken: aws.String(token),
	}
	if err := r.setPayload(in); err != nil {
		return nil, err
	}
	if stages := r.desiredVersionStages(); len(stages) > 0 {
		in.VersionStages = stages
	}
	resp, err := client.PutSecretValue(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("put secret value: %w", err)
	}
	if resp == nil || aws.ToString(resp.VersionId) == "" {
		return nil, fmt.Errorf("put secret value: missing version id")
	}
	return r.waitReadable(ctx, client, r.SecretId, aws.ToString(resp.VersionId))
}

func (r *SecretVersionResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *SecretVersionResourceOutput,
) (*SecretVersionResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	secretID, versionID := r.identity(prior)
	return r.readByID(ctx, client, secretID, versionID)
}

func (r *SecretVersionResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[SecretVersionResource, *SecretVersionResourceOutput],
) (*SecretVersionResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	secretID, versionID := r.identity(prior.Outputs)
	if r.stagesNeedUpdate(prior) {
		if err := r.reconcileStages(ctx, client, secretID, versionID); err != nil {
			return nil, err
		}
	}
	return r.readByID(ctx, client, secretID, versionID)
}

func (r *SecretVersionResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *SecretVersionResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	secretID, versionID := r.identity(prior)
	entry, err := r.findVersionEntry(ctx, client, secretID, versionID)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			return nil
		}
		return err
	}
	for _, stage := range secretVersionStages(entry.VersionStages) {
		if stage == currentStage {
			continue
		}
		if err := r.removeStage(ctx, client, secretID, versionID, stage); err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				return nil
			}
			return err
		}
	}
	return r.waitDetached(ctx, client, secretID, versionID)
}

func (r *SecretVersionResource) identity(prior *SecretVersionResourceOutput) (string, string) {
	secretID := r.SecretId
	versionID := ""
	if prior != nil {
		if prior.SecretId != "" {
			secretID = prior.SecretId
		}
		versionID = prior.VersionId
	}
	return secretID, versionID
}

func (r *SecretVersionResource) setPayload(in *secretsmanager.PutSecretValueInput) error {
	switch {
	case r.SecretString != nil:
		in.SecretString = r.SecretString
	case r.SecretBinaryContent != nil:
		in.SecretBinary = []byte(*r.SecretBinaryContent)
	}
	return nil
}

func (r *SecretVersionResource) readByID(
	ctx context.Context,
	client *secretsmanager.Client,
	secretID string,
	versionID string,
) (*SecretVersionResourceOutput, error) {
	if secretID == "" || versionID == "" {
		return nil, runtime.ErrNotFound
	}
	resp, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId:  aws.String(secretID),
		VersionId: aws.String(versionID),
	})
	if err != nil {
		if isValueGone(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get secret value: %w", err)
	}
	if resp == nil {
		return nil, runtime.ErrNotFound
	}
	gotVersionID := aws.ToString(resp.VersionId)
	if gotVersionID == "" {
		gotVersionID = versionID
	}
	return &SecretVersionResourceOutput{
		SecretId:      secretID,
		Arn:           aws.ToString(resp.ARN),
		VersionId:     gotVersionID,
		VersionStages: secretVersionStages(resp.VersionStages),
	}, nil
}

func (r *SecretVersionResource) waitReadable(
	ctx context.Context,
	client *secretsmanager.Client,
	secretID string,
	versionID string,
) (*SecretVersionResourceOutput, error) {
	var out *SecretVersionResourceOutput
	what := fmt.Sprintf("secret version %s", versionID)
	err := wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		read, err := r.readByID(ctx, client, secretID, versionID)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		out = read
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *SecretVersionResource) stagesNeedUpdate(
	prior runtime.Prior[SecretVersionResource, *SecretVersionResourceOutput],
) bool {
	if r.VersionStages == nil {
		return false
	}
	if secretVersionStagesChanged(prior.Inputs.VersionStages, r.VersionStages) {
		return true
	}
	return prior.Observed != nil &&
		secretVersionStageActionNeeded(prior.Observed.VersionStages, r.desiredVersionStages())
}

func (r *SecretVersionResource) reconcileStages(
	ctx context.Context,
	client *secretsmanager.Client,
	secretID string,
	versionID string,
) error {
	entry, err := r.findVersionEntry(ctx, client, secretID, versionID)
	if err != nil {
		return err
	}
	add, remove := secretVersionStageDiff(entry.VersionStages, r.desiredVersionStages())
	addedCurrent := slices.Contains(add, currentStage)
	for _, stage := range add {
		if err := r.addStage(ctx, client, secretID, versionID, stage); err != nil {
			return err
		}
	}
	for _, stage := range remove {
		if stage == currentStage || (stage == previousStage && addedCurrent) {
			continue
		}
		if err := r.removeStage(ctx, client, secretID, versionID, stage); err != nil {
			return err
		}
	}
	return nil
}

func (r *SecretVersionResource) addStage(
	ctx context.Context,
	client *secretsmanager.Client,
	secretID string,
	versionID string,
	stage string,
) error {
	in := &secretsmanager.UpdateSecretVersionStageInput{
		SecretId:        aws.String(secretID),
		VersionStage:    aws.String(stage),
		MoveToVersionId: aws.String(versionID),
	}
	if stage == currentStage {
		currentVersionID, err := r.findVersionWithStage(ctx, client, secretID, currentStage)
		if err != nil {
			return err
		}
		if currentVersionID != "" && currentVersionID != versionID {
			in.RemoveFromVersionId = aws.String(currentVersionID)
		}
	}
	_, err := client.UpdateSecretVersionStage(ctx, in)
	if err != nil {
		if isValueGone(err) {
			return runtime.ErrNotFound
		}
		return fmt.Errorf("add version stage %s: %w", stage, err)
	}
	return nil
}

func (r *SecretVersionResource) removeStage(
	ctx context.Context,
	client *secretsmanager.Client,
	secretID string,
	versionID string,
	stage string,
) error {
	_, err := client.UpdateSecretVersionStage(ctx,
		&secretsmanager.UpdateSecretVersionStageInput{
			SecretId:            aws.String(secretID),
			VersionStage:        aws.String(stage),
			RemoveFromVersionId: aws.String(versionID),
		})
	if err != nil {
		if isValueGone(err) {
			return runtime.ErrNotFound
		}
		return fmt.Errorf("remove version stage %s: %w", stage, err)
	}
	return nil
}

func (r *SecretVersionResource) waitDetached(
	ctx context.Context,
	client *secretsmanager.Client,
	secretID string,
	versionID string,
) error {
	what := fmt.Sprintf("secret version %s labels", versionID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		entry, err := r.findVersionEntry(ctx, client, secretID, versionID)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			return false, err
		}
		return secretVersionDeleteComplete(entry.VersionStages), nil
	}, wait.WithInterval(time.Second))
}

type secretVersionEntry struct {
	VersionStages []string
}

func (r *SecretVersionResource) findVersionEntry(
	ctx context.Context,
	client *secretsmanager.Client,
	secretID string,
	versionID string,
) (*secretVersionEntry, error) {
	if secretID == "" || versionID == "" {
		return nil, runtime.ErrNotFound
	}
	paginator := secretsmanager.NewListSecretVersionIdsPaginator(client,
		&secretsmanager.ListSecretVersionIdsInput{
			SecretId:          aws.String(secretID),
			IncludeDeprecated: aws.Bool(true),
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isValueGone(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("list secret version ids: %w", err)
		}
		if page == nil {
			continue
		}
		for _, version := range page.Versions {
			if aws.ToString(version.VersionId) == versionID {
				return &secretVersionEntry{
					VersionStages: secretVersionStages(version.VersionStages),
				}, nil
			}
		}
	}
	return nil, runtime.ErrNotFound
}

func (r *SecretVersionResource) findVersionWithStage(
	ctx context.Context,
	client *secretsmanager.Client,
	secretID string,
	stage string,
) (string, error) {
	paginator := secretsmanager.NewListSecretVersionIdsPaginator(client,
		&secretsmanager.ListSecretVersionIdsInput{
			SecretId:          aws.String(secretID),
			IncludeDeprecated: aws.Bool(true),
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isValueGone(err) {
				return "", runtime.ErrNotFound
			}
			return "", fmt.Errorf("list secret version ids: %w", err)
		}
		if page == nil {
			continue
		}
		for _, version := range page.Versions {
			if slices.Contains(version.VersionStages, stage) {
				return aws.ToString(version.VersionId), nil
			}
		}
	}
	return "", nil
}

func secretVersionStages(stages []string) []string {
	out := make([]string, 0, len(stages))
	for _, stage := range stages {
		if stage != "" {
			out = append(out, stage)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (r *SecretVersionResource) desiredVersionStages() []string {
	if r.VersionStages == nil {
		return nil
	}
	return secretVersionStages(*r.VersionStages)
}

func secretVersionStagesChanged(oldStages, newStages *[]string) bool {
	switch {
	case oldStages == nil && newStages == nil:
		return false
	case oldStages == nil || newStages == nil:
		return true
	default:
		oldFiltered := secretVersionStages(*oldStages)
		newFiltered := secretVersionStages(*newStages)
		return !slices.Equal(oldFiltered, newFiltered)
	}
}

func secretVersionStageDiff(current, desired []string) ([]string, []string) {
	current = secretVersionStages(current)
	desired = secretVersionStages(desired)
	currentSet := secretVersionStageSet(current)
	desiredSet := secretVersionStageSet(desired)
	add := make([]string, 0)
	for _, stage := range desired {
		if _, ok := currentSet[stage]; !ok {
			add = append(add, stage)
		}
	}
	remove := make([]string, 0)
	for _, stage := range current {
		if _, ok := desiredSet[stage]; !ok {
			remove = append(remove, stage)
		}
	}
	return add, remove
}

func secretVersionStageActionNeeded(current, desired []string) bool {
	add, remove := secretVersionStageDiff(current, desired)
	if len(add) > 0 {
		return true
	}
	for _, stage := range remove {
		if stage != currentStage {
			return true
		}
	}
	return false
}

func secretVersionStageSet(stages []string) map[string]struct{} {
	set := make(map[string]struct{}, len(stages))
	for _, stage := range stages {
		set[stage] = struct{}{}
	}
	return set
}

func secretVersionDeleteComplete(stages []string) bool {
	stages = secretVersionStages(stages)
	switch len(stages) {
	case 0:
		return true
	case 1:
		return stages[0] == currentStage || stages[0] == previousStage
	default:
		return false
	}
}

func secretVersionClientRequestToken() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate client request token: %w", err)
	}
	random[6] = (random[6] & 0x0f) | 0x40
	random[8] = (random[8] & 0x3f) | 0x80
	var token [36]byte
	hex.Encode(token[0:8], random[0:4])
	token[8] = '-'
	hex.Encode(token[9:13], random[4:6])
	token[13] = '-'
	hex.Encode(token[14:18], random[6:8])
	token[18] = '-'
	hex.Encode(token[19:23], random[8:10])
	token[23] = '-'
	hex.Encode(token[24:36], random[10:16])
	return string(token[:]), nil
}
