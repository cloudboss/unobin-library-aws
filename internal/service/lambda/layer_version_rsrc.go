package lambda

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type LayerVersionResource struct {
	LayerName               string              `ub:"layer-name"`
	Content                 LayerVersionContent `ub:"content"`
	Description             *string             `ub:"description"`
	LicenseInfo             *string             `ub:"license-info"`
	SourceCodeHash          *string             `ub:"source-code-hash"`
	CompatibleArchitectures *[]string           `ub:"compatible-architectures"`
	CompatibleRuntimes      *[]string           `ub:"compatible-runtimes"`
	SkipDestroy             bool                `ub:"skip-destroy"`

	clientFor func(context.Context, *awsCfg) (layerVersionClient, error) `ub:"-"`
}

type LayerVersionContent struct {
	ZipFilePath     *string `ub:"zip-file-path"`
	S3Bucket        *string `ub:"s3-bucket"`
	S3Key           *string `ub:"s3-key"`
	S3ObjectVersion *string `ub:"s3-object-version"`
}

type LayerVersionResourceOutput struct {
	LayerVersionArn          string   `ub:"layer-version-arn"`
	LayerArn                 string   `ub:"layer-arn"`
	LayerName                string   `ub:"layer-name"`
	Version                  int64    `ub:"version"`
	Region                   string   `ub:"region"`
	CreatedDate              string   `ub:"created-date"`
	Description              string   `ub:"description"`
	LicenseInfo              string   `ub:"license-info"`
	CompatibleArchitectures  []string `ub:"compatible-architectures"`
	CompatibleRuntimes       []string `ub:"compatible-runtimes"`
	CodeSHA256               string   `ub:"code-sha256"`
	CodeSize                 int64    `ub:"code-size"`
	SigningJobArn            string   `ub:"signing-job-arn"`
	SigningProfileVersionArn string   `ub:"signing-profile-version-arn"`
}

type layerVersionPrior = runtime.Prior[LayerVersionResource, *LayerVersionResourceOutput, *awsCfg]
type layerVersionDefinition = runtime.ResourceDefinition[
	LayerVersionResource, *LayerVersionResourceOutput, *awsCfg,
]

func (r LayerVersionResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForbiddenWith(
			r.Content.ZipFilePath, r.Content.S3Bucket, r.Content.S3Key, r.Content.S3ObjectVersion,
		),
		constraint.Must(constraint.Any(
			constraint.NotEmpty(r.Content.ZipFilePath),
			constraint.All(
				constraint.NotEmpty(r.Content.S3Bucket),
				constraint.NotEmpty(r.Content.S3Key),
			),
		)).Message("content needs a nonempty zip-file-path or both s3-bucket and s3-key"),
		constraint.ForEach(r.CompatibleArchitectures, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(value, "x86_64", "arm64")).
					Message("compatible-architectures entries must be valid Lambda architectures"),
			}
		}),
		constraint.ForEach(r.CompatibleRuntimes, func(value string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(value,
					"nodejs", "nodejs4.3", "nodejs6.10", "nodejs8.10",
					"nodejs10.x", "nodejs12.x", "nodejs14.x", "nodejs16.x",
					"nodejs18.x", "nodejs20.x", "nodejs22.x", "nodejs24.x",
					"java8", "java8.al2", "java11", "java17", "java21", "java25",
					"python2.7", "python3.6", "python3.7", "python3.8", "python3.9",
					"python3.10", "python3.11", "python3.12", "python3.13", "python3.14",
					"dotnetcore1.0", "dotnetcore2.0", "dotnetcore2.1", "dotnetcore3.1",
					"dotnet6", "dotnet8", "dotnet10", "nodejs4.3-edge", "go1.x",
					"ruby2.5", "ruby2.7", "ruby3.2", "ruby3.3", "ruby3.4", "ruby4.0",
					"provided", "provided.al2", "provided.al2023",
					"java8.al2023", "java11.al2023", "java17.al2023",
				)).Message("compatible-runtimes entries must be valid Lambda runtimes"),
			}
		}),
		constraint.Must(constraint.Expr(
			"@core.length({ for value in (input.values ?? []): value => true... when value != null })"+
				" <= 2",
			constraint.Bind("values", r.CompatibleArchitectures),
		)).Message("compatible-architectures allows at most 2 distinct values"),
		constraint.Must(constraint.Expr(
			"@core.length({ for value in (input.values ?? []): value => true... when value != null })"+
				" <= 15",
			constraint.Bind("values", r.CompatibleRuntimes),
		)).Message("compatible-runtimes allows at most 15 distinct values"),
		constraint.Must(constraint.Expr(
			"@core.byte-length(input.license ?? '') <= 512",
			constraint.Bind("license", r.LicenseInfo),
		)).Message("license-info must be at most 512 bytes"),
		constraint.Must(
			constraint.NotEquals(r.LayerName, nil),
			constraint.NotEquals(r.LayerName, ""),
		).Message("layer-name must not be empty"),
	}
}

func (r LayerVersionResource) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Value(r.SkipDestroy, false)}
}

func (r *LayerVersionResource) ResourceDefinition() layerVersionDefinition {
	type input = LayerVersionResource
	type output = *LayerVersionResourceOutput
	description := runtime.InputField(func(r *input) **string { return &r.Description })
	license := runtime.InputField(func(r *input) **string { return &r.LicenseInfo })
	architectures := runtime.InputField(func(r *input) **[]string {
		return &r.CompatibleArchitectures
	})
	runtimes := runtime.InputField(func(r *input) **[]string { return &r.CompatibleRuntimes })
	objectVersion := runtime.InputField(func(r *input) **string {
		return &r.Content.S3ObjectVersion
	})
	hash := runtime.InputField(func(r *input) **string { return &r.SourceCodeHash })
	return layerVersionDefinition{
		SchemaVersion: 1,
		Validate: func(_ context.Context, r input, _ *awsCfg) error {
			_, err := r.prepareLayerPublish()
			return err
		},
		StableID: func(_ input, out output) (string, error) {
			if out == nil || out.LayerVersionArn == "" {
				return "", errors.New("missing layer version ARN")
			}
			return out.LayerVersionArn, nil
		},
		Equality: []runtime.InputEqualityRule[input]{
			runtime.EqualBy(description, equalLayerOptionalString),
			runtime.EqualBy(license, equalLayerOptionalString),
			runtime.EqualBy(objectVersion, equalLayerOptionalString),
			runtime.EqualBy(architectures, equalLayerStringSet),
			runtime.EqualBy(runtimes, equalLayerStringSet),
			runtime.EqualBy(hash, func(prior, desired *string) bool {
				return desired == nil || (prior != nil && *prior == *desired)
			}),
		},
		Replace: runtime.Replacement[input, output, *awsCfg]{
			Fields: []runtime.AnyInputField[input]{
				runtime.InputField(func(r *input) *string { return &r.LayerName }),
				runtime.InputField(func(r *input) **string { return &r.Content.ZipFilePath }),
				runtime.InputField(func(r *input) **string { return &r.Content.S3Bucket }),
				runtime.InputField(func(r *input) **string { return &r.Content.S3Key }),
				runtime.InputField(func(r *input) *bool { return &r.SkipDestroy }),
				description, license, objectVersion, architectures, runtimes,
			},
			Rules: []runtime.ReplacementRule[input]{
				runtime.ReplaceWhen(hash, func(_, desired *string) bool {
					return aws.ToString(desired) != ""
				}),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
	}
}

func equalLayerOptionalString(prior, desired *string) bool {
	return aws.ToString(prior) == aws.ToString(desired)
}

func equalLayerStringSet(prior, desired *[]string) bool {
	return slices.Equal(
		normalizedLayerValues[string](layerList(prior)),
		normalizedLayerValues[string](layerList(desired)),
	)
}

func layerList(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}

func normalizedLayerValues[To ~string, From ~string](values []From) []To {
	if len(values) == 0 {
		return nil
	}
	out := make([]To, len(values))
	for i, value := range values {
		out[i] = To(value)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (r *LayerVersionResource) prepareLayerPublish() (*lambda.PublishLayerVersionInput, error) {
	content := &lambdatypes.LayerVersionContentInput{
		S3Bucket: r.Content.S3Bucket,
		S3Key:    r.Content.S3Key,
	}
	if aws.ToString(r.Content.S3ObjectVersion) != "" {
		content.S3ObjectVersion = r.Content.S3ObjectVersion
	}
	if path := aws.ToString(r.Content.ZipFilePath); path != "" {
		expanded := path
		if path[0] == '~' && (len(path) == 1 || os.IsPathSeparator(path[1])) {
			home, err := os.UserHomeDir()
			if err != nil {
				account, accountErr := user.Current()
				if accountErr != nil {
					return nil, fmt.Errorf(
						"expand ZIP path %q: %w", path, errors.Join(err, accountErr),
					)
				}
				home = account.HomeDir
				if home == "" {
					return nil, fmt.Errorf("expand ZIP path %q: OS account home is empty", path)
				}
			}
			expanded = filepath.Join(home, path[1:])
		}
		data, err := os.ReadFile(expanded)
		if err != nil {
			return nil, fmt.Errorf("read ZIP file %q: %w", path, err)
		}
		content.ZipFile = data
	}
	return &lambda.PublishLayerVersionInput{
		LayerName:   aws.String(r.LayerName),
		Content:     content,
		Description: aws.String(aws.ToString(r.Description)),
		LicenseInfo: aws.String(aws.ToString(r.LicenseInfo)),
		CompatibleArchitectures: normalizedLayerValues[lambdatypes.Architecture](
			layerList(r.CompatibleArchitectures),
		),
		CompatibleRuntimes: normalizedLayerValues[lambdatypes.Runtime](
			layerList(r.CompatibleRuntimes),
		),
	}, nil
}

type layerVersionClient interface {
	PublishLayerVersion(
		context.Context, *lambda.PublishLayerVersionInput, ...func(*lambda.Options),
	) (*lambda.PublishLayerVersionOutput, error)
	GetLayerVersion(
		context.Context, *lambda.GetLayerVersionInput, ...func(*lambda.Options),
	) (*lambda.GetLayerVersionOutput, error)
	DeleteLayerVersion(
		context.Context, *lambda.DeleteLayerVersionInput, ...func(*lambda.Options),
	) (*lambda.DeleteLayerVersionOutput, error)
	Options() lambda.Options
}

func (r *LayerVersionResource) layerVersionClient(
	ctx context.Context, cfg *awsCfg,
) (layerVersionClient, error) {
	if r.clientFor != nil {
		return r.clientFor(ctx, cfg)
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (r *LayerVersionResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*LayerVersionResourceOutput, error) {
	input, err := r.prepareLayerPublish()
	if err != nil {
		return nil, err
	}
	client, err := r.layerVersionClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	published, err := client.PublishLayerVersion(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("publish layer version for %q: %w", r.LayerName, err)
	}
	if published == nil {
		return nil, fmt.Errorf("publish layer version for %q: missing response", r.LayerName)
	}
	target, err := parseLayerVersionARN(aws.ToString(published.LayerVersionArn))
	if err != nil {
		return nil, fmt.Errorf("publish layer version for %q: %w", r.LayerName, err)
	}
	out, err := readLayerVersion(ctx, client, target)
	if errors.Is(err, runtime.ErrNotFound) {
		return nil, fmt.Errorf("get layer version %q after publish: %w", target.arn, err)
	}
	return out, err
}

func (r *LayerVersionResource) Read(
	ctx context.Context, cfg *awsCfg, prior layerVersionPrior,
) (*LayerVersionResourceOutput, error) {
	client, target, err := r.priorLayerVersion(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	return readLayerVersion(ctx, client, target)
}

func (r *LayerVersionResource) Update(
	ctx context.Context, cfg *awsCfg, prior layerVersionPrior,
) (*LayerVersionResourceOutput, error) {
	if prior.Observed != nil {
		return prior.Observed, nil
	}
	return r.Read(ctx, cfg, prior)
}

func (r *LayerVersionResource) Delete(
	ctx context.Context, cfg *awsCfg, prior layerVersionPrior,
) error {
	if prior.Inputs.SkipDestroy {
		return nil
	}
	client, target, err := r.priorLayerVersion(ctx, cfg, prior)
	if err != nil {
		return err
	}
	_, err = client.DeleteLayerVersion(ctx, &lambda.DeleteLayerVersionInput{
		LayerName:     aws.String(target.name),
		VersionNumber: aws.Int64(target.version),
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete layer version %q: %w", target.arn, err)
	}
	return nil
}

type layerVersionTarget struct {
	arn     string
	name    string
	version int64
}

func parseLayerVersionARN(value string) (layerVersionTarget, error) {
	parsed, err := arn.Parse(value)
	if err == nil && parsed.Service == "lambda" {
		parts := strings.Split(parsed.Resource, ":")
		if len(parts) == 3 && parts[0] == "layer" && parts[1] != "" {
			version, err := strconv.ParseInt(parts[2], 10, 64)
			if err == nil && version > 0 {
				return layerVersionTarget{arn: value, name: parts[1], version: version}, nil
			}
		}
	}
	return layerVersionTarget{}, fmt.Errorf("invalid layer version ARN %q", value)
}

func (r *LayerVersionResource) priorLayerVersion(
	ctx context.Context, cfg *awsCfg, prior layerVersionPrior,
) (layerVersionClient, layerVersionTarget, error) {
	if prior.Outputs == nil || prior.Outputs.LayerVersionArn == "" {
		return nil, layerVersionTarget{}, errors.New("missing prior layer version ARN")
	}
	target, err := parseLayerVersionARN(prior.Outputs.LayerVersionArn)
	if err != nil {
		return nil, layerVersionTarget{}, fmt.Errorf("prior layer version: %w", err)
	}
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	var regional awsCfg
	if cfg != nil {
		regional = *cfg
	}
	if prior.Outputs.Region != "" {
		regional.Region = aws.String(prior.Outputs.Region)
	}
	client, err := r.layerVersionClient(ctx, &regional)
	return client, target, err
}

func readLayerVersion(
	ctx context.Context, client layerVersionClient, target layerVersionTarget,
) (*LayerVersionResourceOutput, error) {
	out, err := client.GetLayerVersion(ctx, &lambda.GetLayerVersionInput{
		LayerName:     aws.String(target.name),
		VersionNumber: aws.Int64(target.version),
	})
	if isNotFound(err) || (err == nil && out == nil) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get layer version %q: %w", target.arn, err)
	}
	observed, err := parseLayerVersionARN(aws.ToString(out.LayerVersionArn))
	if err != nil {
		return nil, fmt.Errorf("get layer version %q: %w", target.arn, err)
	}
	if aws.ToString(out.LayerArn) == "" || out.Content == nil {
		return nil, fmt.Errorf("get layer version %q: missing layer ARN or content", target.arn)
	}
	return &LayerVersionResourceOutput{
		LayerVersionArn:          observed.arn,
		LayerArn:                 aws.ToString(out.LayerArn),
		LayerName:                observed.name,
		Version:                  observed.version,
		Region:                   client.Options().Region,
		CreatedDate:              aws.ToString(out.CreatedDate),
		Description:              aws.ToString(out.Description),
		LicenseInfo:              aws.ToString(out.LicenseInfo),
		CompatibleArchitectures:  normalizedLayerValues[string](out.CompatibleArchitectures),
		CompatibleRuntimes:       normalizedLayerValues[string](out.CompatibleRuntimes),
		CodeSHA256:               aws.ToString(out.Content.CodeSha256),
		CodeSize:                 out.Content.CodeSize,
		SigningJobArn:            aws.ToString(out.Content.SigningJobArn),
		SigningProfileVersionArn: aws.ToString(out.Content.SigningProfileVersionArn),
	}, nil
}
