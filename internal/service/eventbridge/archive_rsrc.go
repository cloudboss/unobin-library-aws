package eventbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type ArchiveResource struct {
	Name             string  `ub:"name"`
	EventSourceArn   string  `ub:"event-source-arn"`
	Description      *string `ub:"description"`
	EventPattern     *string `ub:"event-pattern"`
	KmsKeyIdentifier *string `ub:"kms-key-identifier"`
	RetentionDays    *int    `ub:"retention-days"`
}

type ArchiveResourceOutput struct {
	ArchiveArn       string `ub:"archive-arn"`
	ArchiveName      string `ub:"archive-name"`
	EventSourceArn   string `ub:"event-source-arn"`
	Description      string `ub:"description"`
	EventPattern     string `ub:"event-pattern"`
	KmsKeyIdentifier string `ub:"kms-key-identifier"`
	RetentionDays    int    `ub:"retention-days"`
}

type archiveClient interface {
	CreateArchive(context.Context, *eventbridge.CreateArchiveInput,
		...func(*eventbridge.Options)) (*eventbridge.CreateArchiveOutput, error)
	DescribeArchive(context.Context, *eventbridge.DescribeArchiveInput,
		...func(*eventbridge.Options)) (*eventbridge.DescribeArchiveOutput, error)
	UpdateArchive(context.Context, *eventbridge.UpdateArchiveInput,
		...func(*eventbridge.Options)) (*eventbridge.UpdateArchiveOutput, error)
	DeleteArchive(context.Context, *eventbridge.DeleteArchiveInput,
		...func(*eventbridge.Options)) (*eventbridge.DeleteArchiveOutput, error)
}

func (r *ArchiveResource) prepareArchive(name string) (*eventbridge.CreateArchiveInput, error) {
	if r.EventSourceArn != "" {
		parts := strings.SplitN(r.EventSourceArn, ":", 6)
		if len(parts) != 6 || parts[0] != "arn" ||
			!archivePartition.MatchString(parts[1]) || parts[5] == "" ||
			(parts[3] != "" && !archiveRegion.MatchString(parts[3])) ||
			(parts[4] != "" && !archiveAccount.MatchString(parts[4])) {
			return nil, errors.New("event-source-arn must be a valid ARN")
		}
	}
	in := &eventbridge.CreateArchiveInput{
		ArchiveName: aws.String(name), EventSourceArn: aws.String(r.EventSourceArn),
		Description:      archiveNonempty(r.Description),
		KmsKeyIdentifier: archiveNonempty(r.KmsKeyIdentifier),
	}
	if r.RetentionDays != nil && *r.RetentionDays != 0 {
		in.RetentionDays = aws.Int32(int32(*r.RetentionDays))
	}
	if pattern := aws.ToString(r.EventPattern); pattern != "" {
		var value any
		if err := json.Unmarshal([]byte(pattern), &value); err != nil {
			return nil, fmt.Errorf("prepare archive event-pattern: %w", err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("prepare archive event-pattern: %w", err)
		}
		normalized := string(encoded)
		lengthValue := strings.NewReplacer(`\u003c`, "<", `\u003e`, ">", `\u0026`, "&").
			Replace(normalized)
		if len(lengthValue) > 4096 {
			return nil, errors.New("event-pattern must be at most 4096 normalized bytes")
		}
		in.EventPattern = aws.String(normalized)
	}
	return in, nil
}

func (r *ArchiveResource) createArchive(ctx context.Context,
	client archiveClient) (*ArchiveResourceOutput, error) {
	in, err := r.prepareArchive(r.Name)
	if err != nil {
		return nil, err
	}
	if _, err := client.CreateArchive(ctx, in); err != nil {
		return nil, fmt.Errorf("create archive %s: %w", r.Name, err)
	}
	out, err := readArchive(ctx, client, r.Name)
	if err == nil {
		return out, nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	return nil, errors.Join(err, deleteArchive(cleanupCtx, client, r.Name))
}

func readArchive(ctx context.Context, client archiveClient,
	name string) (*ArchiveResourceOutput, error) {
	out, err := client.DescribeArchive(ctx,
		&eventbridge.DescribeArchiveInput{ArchiveName: aws.String(name)})
	if isNotFound(err) || (err == nil && out == nil) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe archive %s: %w", name, err)
	}
	return &ArchiveResourceOutput{
		ArchiveArn: aws.ToString(out.ArchiveArn), ArchiveName: aws.ToString(out.ArchiveName),
		EventSourceArn: aws.ToString(out.EventSourceArn), Description: aws.ToString(out.Description),
		EventPattern:     aws.ToString(out.EventPattern),
		KmsKeyIdentifier: aws.ToString(out.KmsKeyIdentifier),
		RetentionDays:    int(aws.ToInt32(out.RetentionDays)),
	}, nil
}

var (
	archivePartition = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	archiveRegion    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	archiveAccount   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|\d{12}|cw.{10})$`)
)

func (r ArchiveResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.Name, nil)).Message("name is required"),
		constraint.Must(constraint.NotEquals(r.EventSourceArn, nil)).
			Message("event-source-arn is required"),
		constraint.Must(constraint.IsMatch(r.Name, `^[.\-_A-Za-z0-9]{1,48}$`)).
			Message("name must be 1-48 ASCII letters, digits, dot, underscore or hyphen"),
		constraint.Must(constraint.Expr(
			"@core.byte-length(input.description ?? '') <= 512",
			constraint.Bind("description", r.Description),
		)).Message("description must be at most 512 bytes"),
		constraint.Must(constraint.Expr(
			"@core.byte-length(input.key ?? '') <= 2048",
			constraint.Bind("key", r.KmsKeyIdentifier),
		)).Message("kms-key-identifier must be at most 2048 bytes"),
		constraint.When(constraint.NotEquals(r.KmsKeyIdentifier, nil)).
			Require(constraint.IsMatch(r.KmsKeyIdentifier, `^[a-zA-Z0-9_\-/:]*$`)).
			Message("kms-key-identifier contains invalid characters"),
		constraint.When(constraint.All(constraint.NotEquals(r.EventPattern, nil),
			constraint.NotEquals(r.EventPattern, ""))).
			Require(constraint.IsFormat(r.EventPattern, constraint.FormatJSON)).
			Message("event-pattern must be valid JSON"),
	}
}

func (r *ArchiveResource) ResourceDefinition() runtime.ResourceDefinition[
	ArchiveResource, *ArchiveResourceOutput, *awsCfg,
] {
	return runtime.ResourceDefinition[ArchiveResource, *ArchiveResourceOutput, *awsCfg]{
		SchemaVersion: 1,
		Validate: func(_ context.Context, in ArchiveResource, _ *awsCfg) error {
			_, err := in.prepareArchive(in.Name)
			return err
		},
		Equality: []runtime.InputEqualityRule[ArchiveResource]{
			runtime.EqualBy(
				runtime.InputField(func(in *ArchiveResource) **string { return &in.EventPattern }),
				func(a, b *string) bool {
					return archivePatternsEqual(aws.ToString(a), aws.ToString(b))
				},
			),
		},
		Replace: runtime.Replacement[ArchiveResource, *ArchiveResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[ArchiveResource]{
				runtime.InputField(func(in *ArchiveResource) *string { return &in.Name }),
				runtime.InputField(func(in *ArchiveResource) *string { return &in.EventSourceArn }),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
	}
}

func (r *ArchiveResource) Create(ctx context.Context, cfg *awsCfg) (*ArchiveResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createArchive(ctx, client)
}

func (r *ArchiveResource) Read(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ArchiveResource, *ArchiveResourceOutput, *awsCfg],
) (*ArchiveResourceOutput, error) {
	client, err := newArchivePriorClient(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	return readArchive(ctx, client, archiveName(r.Name, prior.Outputs))
}

func (r *ArchiveResource) Update(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ArchiveResource, *ArchiveResourceOutput, *awsCfg],
) (*ArchiveResourceOutput, error) {
	client, err := newArchivePriorClient(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	return r.updateArchive(ctx, client, prior)
}

func (r *ArchiveResource) updateArchive(ctx context.Context, client archiveClient,
	prior runtime.Prior[ArchiveResource, *ArchiveResourceOutput, *awsCfg],
) (*ArchiveResourceOutput, error) {
	name := archiveName(r.Name, prior.Outputs)
	in, err := r.prepareArchive(name)
	if err != nil {
		return nil, err
	}
	observed := prior.Observed
	if observed == nil {
		observed = prior.Outputs
	}
	changed := aws.ToString(r.Description) != aws.ToString(prior.Inputs.Description) ||
		!archivePatternsEqual(aws.ToString(r.EventPattern), aws.ToString(prior.Inputs.EventPattern)) ||
		aws.ToString(r.KmsKeyIdentifier) != aws.ToString(prior.Inputs.KmsKeyIdentifier) ||
		aws.ToInt(r.RetentionDays) != aws.ToInt(prior.Inputs.RetentionDays)
	if changed || archiveNeedsUpdate(in, observed) {
		_, err := client.UpdateArchive(ctx, &eventbridge.UpdateArchiveInput{
			ArchiveName: in.ArchiveName, Description: in.Description, EventPattern: in.EventPattern,
			KmsKeyIdentifier: in.KmsKeyIdentifier, RetentionDays: in.RetentionDays,
		})
		if err != nil {
			return nil, fmt.Errorf("update archive %s: %w", name, err)
		}
		return readArchive(ctx, client, name)
	}
	return observed, nil
}

func (r *ArchiveResource) Delete(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ArchiveResource, *ArchiveResourceOutput, *awsCfg],
) error {
	client, err := newArchivePriorClient(ctx, cfg, prior)
	if err != nil {
		return err
	}
	return deleteArchive(ctx, client, archiveName(r.Name, prior.Outputs))
}

func newArchivePriorClient(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ArchiveResource, *ArchiveResourceOutput, *awsCfg],
) (*eventbridge.Client, error) {
	if prior.Configuration != nil {
		cfg = prior.Configuration
		if aws.ToString(cfg.Region) != "" {
			return newClient(ctx, cfg)
		}
	}
	if prior.Outputs == nil || prior.Outputs.ArchiveArn == "" {
		return newClient(ctx, cfg)
	}
	identity, err := arn.Parse(prior.Outputs.ArchiveArn)
	if err != nil {
		return nil, fmt.Errorf("parse prior archive ARN: %w", err)
	}
	if identity.Service != "events" || identity.Region == "" {
		return nil, fmt.Errorf("prior archive ARN has no EventBridge region: %q",
			prior.Outputs.ArchiveArn)
	}
	var regional awsCfg
	if cfg != nil {
		regional = *cfg
	}
	regional.Region = aws.String(identity.Region)
	return newClient(ctx, &regional)
}

func deleteArchive(ctx context.Context, client archiveClient, name string) error {
	_, err := client.DeleteArchive(ctx,
		&eventbridge.DeleteArchiveInput{ArchiveName: aws.String(name)})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete archive %s: %w", name, err)
	}
	return nil
}

func archiveName(fallback string, prior *ArchiveResourceOutput) string {
	if prior != nil && prior.ArchiveName != "" {
		return prior.ArchiveName
	}
	return fallback
}

func archiveNonempty(value *string) *string {
	if aws.ToString(value) == "" {
		return nil
	}
	return value
}

func archiveNeedsUpdate(in *eventbridge.CreateArchiveInput, out *ArchiveResourceOutput) bool {
	return out == nil ||
		(in.Description != nil && *in.Description != out.Description) ||
		(in.EventPattern != nil && !archivePatternsEqual(*in.EventPattern, out.EventPattern)) ||
		(in.KmsKeyIdentifier != nil && *in.KmsKeyIdentifier != out.KmsKeyIdentifier) ||
		(in.RetentionDays != nil && int(*in.RetentionDays) != out.RetentionDays)
}

func archivePatternsEqual(a, b string) bool {
	var left, right any
	if json.Unmarshal([]byte(a), &left) != nil || json.Unmarshal([]byte(b), &right) != nil {
		return a == b
	}
	x, _ := json.Marshal(left)
	y, _ := json.Marshal(right)
	return string(x) == string(y)
}
