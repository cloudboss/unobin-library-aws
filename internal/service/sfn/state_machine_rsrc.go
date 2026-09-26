package sfn

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	stateMachineCreateWindow = 5 * time.Minute
	stateMachineUpdateWindow = time.Minute
	stateMachineDeleteWindow = 5 * time.Minute
)

var (
	stateMachineNamePattern   = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)
	stateMachineTagKeyPattern = regexp.MustCompile(
		`^[\p{L}\p{Z}\p{N}_.:/=+@-]+$`)
	stateMachineTagValuePattern = regexp.MustCompile(
		`^[\p{L}\p{Z}\p{N}_.:/=+@-]*$`)
)

type StateMachineEncryptionConfiguration struct {
	Type                         string  `ub:"type"`
	KMSKeyID                     *string `ub:"kms-key-id"`
	KMSDataKeyReusePeriodSeconds *int64  `ub:"kms-data-key-reuse-period-seconds"`
}

type StateMachineLoggingConfiguration struct {
	IncludeExecutionData *bool   `ub:"include-execution-data"`
	Level                *string `ub:"level"`
	LogDestination       *string `ub:"log-destination"`
}

type StateMachineTracingConfiguration struct {
	Enabled *bool `ub:"enabled"`
}

type StateMachineResource struct {
	Name                    *string                              `ub:"name"`
	Definition              string                               `ub:"definition,sensitive"`
	RoleARN                 string                               `ub:"role-arn"`
	Type                    string                               `ub:"type"`
	EncryptionConfiguration *StateMachineEncryptionConfiguration `ub:"encryption-configuration"`
	LoggingConfiguration    *StateMachineLoggingConfiguration    `ub:"logging-configuration"`
	TracingConfiguration    *StateMachineTracingConfiguration    `ub:"tracing-configuration"`
	Tags                    *map[string]string                   `ub:"tags"`
}

type StateMachineResourceOutput struct {
	ARN        string `ub:"arn"`
	RevisionID string `ub:"revision-id"`
}

func (r *StateMachineResource) SchemaVersion() int { return 1 }

func (r *StateMachineResource) ReplaceFields() []string {
	return []string{"name", "type"}
}

func (r StateMachineResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.Type, "STANDARD"),
	}
}

func (r StateMachineResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.Definition)).
			Message("definition must not be empty"),
		constraint.Must(constraint.OneOf(r.Type, "STANDARD", "EXPRESS")).
			Message("type must be STANDARD or EXPRESS"),
		constraint.Must(constraint.MaxItems(r.Tags, 50)).
			Message("tags holds at most 50 entries"),
		constraint.When(constraint.Present(r.LoggingConfiguration.Level)).
			Require(constraint.OneOf(r.LoggingConfiguration.Level,
				"ALL", "ERROR", "FATAL", "OFF")).
			Message("logging-configuration level must be ALL, ERROR, FATAL, or OFF"),
		constraint.When(constraint.All(
			constraint.Present(r.LoggingConfiguration.Level),
			constraint.NotEquals(r.LoggingConfiguration.Level, "OFF"),
		)).Require(constraint.Present(r.LoggingConfiguration.LogDestination)).
			Message("non-OFF logging requires log-destination"),
		constraint.When(constraint.Present(r.EncryptionConfiguration)).
			Require(constraint.OneOf(r.EncryptionConfiguration.Type,
				"AWS_OWNED_KEY", "CUSTOMER_MANAGED_KMS_KEY")).
			Message("encryption-configuration type must be AWS_OWNED_KEY or " +
				"CUSTOMER_MANAGED_KMS_KEY"),
		constraint.When(constraint.Equals(r.EncryptionConfiguration.Type,
			"CUSTOMER_MANAGED_KMS_KEY")).
			Require(constraint.Present(r.EncryptionConfiguration.KMSKeyID)).
			Message("customer-managed encryption requires kms-key-id"),
		constraint.When(constraint.Present(
			r.EncryptionConfiguration.KMSDataKeyReusePeriodSeconds)).
			Require(
				constraint.AtLeast(
					r.EncryptionConfiguration.KMSDataKeyReusePeriodSeconds, 60),
				constraint.AtMost(
					r.EncryptionConfiguration.KMSDataKeyReusePeriodSeconds, 900),
			).
			Message("kms-data-key-reuse-period-seconds must be between 60 and 900"),
	}
}

func (r *StateMachineResource) ValidateInputs(context.Context, *awsCfg) error {
	if length := utf8.RuneCountInString(r.Definition); length < 1 || length > 1_048_576 {
		return errors.New("definition must contain 1 to 1,048,576 characters")
	}
	if r.Name != nil {
		length := utf8.RuneCountInString(*r.Name)
		if length < 1 || length > 80 || !stateMachineNamePattern.MatchString(*r.Name) {
			return errors.New(
				"name must contain 1 to 80 letters, digits, underscores, or hyphens")
		}
	}
	if !slices.Contains([]string{"", "STANDARD", "EXPRESS"}, r.Type) {
		return errors.New("type must be STANDARD or EXPRESS")
	}
	if length := utf8.RuneCountInString(r.RoleARN); length < 1 || length > 256 ||
		!arn.IsARN(r.RoleARN) {
		return errors.New("role-arn must be a valid ARN containing 1 to 256 characters")
	}
	if err := r.validateEncryption(); err != nil {
		return err
	}
	if err := r.validateLogging(); err != nil {
		return err
	}
	return r.validateTags()
}

func (r *StateMachineResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*StateMachineResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, defaultStateMachineOperationOptions())
}

func (r *StateMachineResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[StateMachineResource, *StateMachineResourceOutput, *awsCfg],
) (*StateMachineResourceOutput, error) {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior)
}

func (r *StateMachineResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[StateMachineResource, *StateMachineResourceOutput, *awsCfg],
) (*StateMachineResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior, defaultStateMachineOperationOptions())
}

func (r *StateMachineResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[StateMachineResource, *StateMachineResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior, defaultStateMachineOperationOptions())
}

type stateMachineClient interface {
	CreateStateMachine(context.Context, *sfn.CreateStateMachineInput,
		...func(*sfn.Options)) (*sfn.CreateStateMachineOutput, error)
	DescribeStateMachine(context.Context, *sfn.DescribeStateMachineInput,
		...func(*sfn.Options)) (*sfn.DescribeStateMachineOutput, error)
	UpdateStateMachine(context.Context, *sfn.UpdateStateMachineInput,
		...func(*sfn.Options)) (*sfn.UpdateStateMachineOutput, error)
	DeleteStateMachine(context.Context, *sfn.DeleteStateMachineInput,
		...func(*sfn.Options)) (*sfn.DeleteStateMachineOutput, error)
	ValidateStateMachineDefinition(context.Context, *sfn.ValidateStateMachineDefinitionInput,
		...func(*sfn.Options)) (*sfn.ValidateStateMachineDefinitionOutput, error)
	ListTagsForResource(context.Context, *sfn.ListTagsForResourceInput,
		...func(*sfn.Options)) (*sfn.ListTagsForResourceOutput, error)
	TagResource(context.Context, *sfn.TagResourceInput,
		...func(*sfn.Options)) (*sfn.TagResourceOutput, error)
	UntagResource(context.Context, *sfn.UntagResourceInput,
		...func(*sfn.Options)) (*sfn.UntagResourceOutput, error)
}

type stateMachineClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type stateMachineOperationOptions struct {
	clock        stateMachineClock
	random       io.Reader
	createWindow time.Duration
	updateWindow time.Duration
	deleteWindow time.Duration
}

func (r *StateMachineResource) create(
	ctx context.Context,
	client stateMachineClient,
	options stateMachineOperationOptions,
) (*StateMachineResourceOutput, error) {
	return r.createStateMachine(ctx, client, options.withDefaults())
}

func (r *StateMachineResource) read(
	ctx context.Context,
	client stateMachineClient,
	prior *StateMachineResourceOutput,
) (*StateMachineResourceOutput, error) {
	if prior == nil {
		return nil, runtime.ErrNotFound
	}
	return readStateMachine(ctx, client, prior.ARN, sfntypes.IncludedDataMetadataOnly)
}

func (r *StateMachineResource) update(
	ctx context.Context,
	client stateMachineClient,
	prior runtime.Prior[StateMachineResource, *StateMachineResourceOutput, *awsCfg],
	options stateMachineOperationOptions,
) (*StateMachineResourceOutput, error) {
	return r.updateStateMachine(ctx, client, prior, options.withDefaults())
}

func (r *StateMachineResource) delete(
	ctx context.Context,
	client stateMachineClient,
	prior *StateMachineResourceOutput,
	options stateMachineOperationOptions,
) error {
	return r.deleteStateMachine(ctx, client, prior, options.withDefaults())
}

func stateMachineTags(values map[string]string) []sfntypes.Tag {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	tags := make([]sfntypes.Tag, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		tags = append(tags, sfntypes.Tag{Key: &key, Value: &value})
	}
	return tags
}

func (o stateMachineOperationOptions) withDefaults() stateMachineOperationOptions {
	if o.clock == nil {
		o.clock = realStateMachineClock{}
	}
	if o.random == nil {
		o.random = rand.Reader
	}
	if o.createWindow == 0 {
		o.createWindow = stateMachineCreateWindow
	}
	if o.updateWindow == 0 {
		o.updateWindow = stateMachineUpdateWindow
	}
	if o.deleteWindow == 0 {
		o.deleteWindow = stateMachineDeleteWindow
	}
	return o
}

func defaultStateMachineOperationOptions() stateMachineOperationOptions {
	return stateMachineOperationOptions{}.withDefaults()
}

func resolvedStateMachineType(value string) sfntypes.StateMachineType {
	if value == "" {
		return sfntypes.StateMachineTypeStandard
	}
	return sfntypes.StateMachineType(value)
}

func stateMachineConfigurationChanged(prior, current StateMachineResource) bool {
	return runtime.Changed(prior.Definition, current.Definition) ||
		runtime.Changed(prior.RoleARN, current.RoleARN) ||
		runtime.Changed(prior.EncryptionConfiguration, current.EncryptionConfiguration) ||
		runtime.Changed(prior.LoggingConfiguration, current.LoggingConfiguration) ||
		runtime.Changed(prior.TracingConfiguration, current.TracingConfiguration)
}

func (r *StateMachineResource) validateEncryption() error {
	configuration := r.EncryptionConfiguration
	if configuration == nil {
		return nil
	}
	switch configuration.Type {
	case "AWS_OWNED_KEY":
		if configuration.KMSKeyID != nil || configuration.KMSDataKeyReusePeriodSeconds != nil {
			return errors.New("AWS_OWNED_KEY encryption forbids KMS key members")
		}
	case "CUSTOMER_MANAGED_KMS_KEY":
		if configuration.KMSKeyID == nil {
			return errors.New("customer-managed encryption requires kms-key-id")
		}
		length := utf8.RuneCountInString(*configuration.KMSKeyID)
		if length < 1 || length > 2048 {
			return errors.New("kms-key-id must contain 1 to 2,048 characters")
		}
	default:
		return errors.New("encryption-configuration type must be AWS_OWNED_KEY or " +
			"CUSTOMER_MANAGED_KMS_KEY")
	}
	if period := configuration.KMSDataKeyReusePeriodSeconds; period != nil &&
		(*period < 60 || *period > 900) {
		return errors.New("kms-data-key-reuse-period-seconds must be between 60 and 900")
	}
	return nil
}

func (r *StateMachineResource) validateLogging() error {
	configuration := r.LoggingConfiguration
	if configuration == nil {
		return nil
	}
	level := "OFF"
	if configuration.Level != nil {
		level = *configuration.Level
	}
	if !slices.Contains([]string{"ALL", "ERROR", "FATAL", "OFF"}, level) {
		return errors.New("logging-configuration level must be ALL, ERROR, FATAL, or OFF")
	}
	if level != "OFF" && configuration.LogDestination == nil {
		return errors.New("non-OFF logging requires log-destination")
	}
	if configuration.LogDestination == nil {
		return nil
	}
	destination := *configuration.LogDestination
	if utf8.RuneCountInString(destination) > 256 || !strings.HasSuffix(destination, ":*") ||
		!arn.IsARN(destination) {
		return errors.New("log-destination must be a valid ARN of at most 256 characters " +
			"ending in :*")
	}
	return nil
}

func (r *StateMachineResource) validateTags() error {
	values := stateMachineTagMap(r.Tags)
	if len(values) > 50 {
		return errors.New("tags must contain at most 50 entries")
	}
	for key, value := range values {
		if strings.HasPrefix(key, "aws:") || strings.HasPrefix(value, "aws:") {
			return fmt.Errorf("tag key and value must not begin with aws: for %q", key)
		}
		if length := utf8.RuneCountInString(key); length < 1 || length > 128 {
			return fmt.Errorf("tag key %q must contain 1 to 128 characters", key)
		}
		if !stateMachineTagKeyPattern.MatchString(key) {
			return fmt.Errorf("tag key %q contains characters outside the allowed characters", key)
		}
		if utf8.RuneCountInString(value) > 256 {
			return fmt.Errorf("tag value for %q must contain at most 256 characters", key)
		}
		if !stateMachineTagValuePattern.MatchString(value) {
			return fmt.Errorf(
				"tag value for %q contains characters outside the allowed characters", key)
		}
	}
	return nil
}

func stateMachineTagMap(tags *map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	return *tags
}
