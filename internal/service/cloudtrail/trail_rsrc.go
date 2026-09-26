package cloudtrail

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	cloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

const trailPropagationTimeout = 2 * time.Minute

var trailRetryInterval = 5 * time.Second

var (
	trailNameCharacters  = regexp.MustCompile(`^[0-9A-Za-z._-]+$`)
	trailNamePunctuation = regexp.MustCompile(`[._-]{2}`)
	arnPartition         = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	arnRegion            = regexp.MustCompile(`^[a-z]{2,4}(-[a-z]+)+-[0-9]{1,2}$`)
	arnAccount           = regexp.MustCompile(
		`^([0-9]{12}|aws|aws-managed|third-party|aws-marketplace|partner-managed|cw.{10})$`,
	)
)

type TrailEventSelector struct {
	DataResources                 *[]TrailDataResource `ub:"data-resources"`
	ExcludeManagementEventSources *[]string            `ub:"exclude-management-event-sources"`
	IncludeManagementEvents       *bool                `ub:"include-management-events"`
	ReadWriteType                 *string              `ub:"read-write-type"`
}

type TrailDataResource struct {
	Type   string    `ub:"type"`
	Values *[]string `ub:"values"`
}

type TrailAdvancedEventSelector struct {
	Name           *string                      `ub:"name"`
	FieldSelectors []TrailAdvancedFieldSelector `ub:"field-selectors"`
}

type TrailAdvancedFieldSelector struct {
	Field         string    `ub:"field"`
	Equals        *[]string `ub:"equals"`
	NotEquals     *[]string `ub:"not-equals"`
	StartsWith    *[]string `ub:"starts-with"`
	NotStartsWith *[]string `ub:"not-starts-with"`
	EndsWith      *[]string `ub:"ends-with"`
	NotEndsWith   *[]string `ub:"not-ends-with"`
}

type TrailInsightSelector struct {
	InsightType     string    `ub:"insight-type"`
	EventCategories *[]string `ub:"event-categories"`
}

type TrailAggregationConfiguration struct {
	EventCategory string   `ub:"event-category"`
	Templates     []string `ub:"templates"`
}

type TrailResource struct {
	Name                       string                           `ub:"name"`
	S3BucketName               string                           `ub:"s3-bucket-name"`
	S3KeyPrefix                *string                          `ub:"s3-key-prefix"`
	SnsTopicName               *string                          `ub:"sns-topic-name"`
	CloudWatchLogsLogGroupArn  *string                          `ub:"cloud-watch-logs-log-group-arn"`
	CloudWatchLogsRoleArn      *string                          `ub:"cloud-watch-logs-role-arn"`
	KmsKeyId                   *string                          `ub:"kms-key-id"`
	EnableLogFileValidation    *bool                            `ub:"enable-log-file-validation"`
	IncludeGlobalServiceEvents *bool                            `ub:"include-global-service-events"`
	IsMultiRegionTrail         *bool                            `ub:"is-multi-region-trail"`
	IsOrganizationTrail        *bool                            `ub:"is-organization-trail"`
	EnableLogging              *bool                            `ub:"enable-logging"`
	EventSelectors             *[]TrailEventSelector            `ub:"event-selectors"`
	AdvancedEventSelectors     *[]TrailAdvancedEventSelector    `ub:"advanced-event-selectors"`
	InsightSelectors           *[]TrailInsightSelector          `ub:"insight-selectors"`
	AggregationConfigurations  *[]TrailAggregationConfiguration `ub:"aggregation-configurations"`
	Tags                       *map[string]string               `ub:"tags"`
}

type TrailResourceOutput struct {
	Arn         string `ub:"arn"`
	HomeRegion  string `ub:"home-region"`
	SnsTopicArn string `ub:"sns-topic-arn"`
}

type trailClient interface {
	CreateTrail(context.Context, *cloudtrail.CreateTrailInput,
		...func(*cloudtrail.Options)) (*cloudtrail.CreateTrailOutput, error)
	DescribeTrails(context.Context, *cloudtrail.DescribeTrailsInput,
		...func(*cloudtrail.Options)) (*cloudtrail.DescribeTrailsOutput, error)
	UpdateTrail(context.Context, *cloudtrail.UpdateTrailInput,
		...func(*cloudtrail.Options)) (*cloudtrail.UpdateTrailOutput, error)
	DeleteTrail(context.Context, *cloudtrail.DeleteTrailInput,
		...func(*cloudtrail.Options)) (*cloudtrail.DeleteTrailOutput, error)
	StartLogging(context.Context, *cloudtrail.StartLoggingInput,
		...func(*cloudtrail.Options)) (*cloudtrail.StartLoggingOutput, error)
	StopLogging(context.Context, *cloudtrail.StopLoggingInput,
		...func(*cloudtrail.Options)) (*cloudtrail.StopLoggingOutput, error)
	PutEventSelectors(context.Context, *cloudtrail.PutEventSelectorsInput,
		...func(*cloudtrail.Options)) (*cloudtrail.PutEventSelectorsOutput, error)
	PutInsightSelectors(context.Context, *cloudtrail.PutInsightSelectorsInput,
		...func(*cloudtrail.Options)) (*cloudtrail.PutInsightSelectorsOutput, error)
	PutEventConfiguration(context.Context, *cloudtrail.PutEventConfigurationInput,
		...func(*cloudtrail.Options)) (*cloudtrail.PutEventConfigurationOutput, error)
	ListTags(context.Context, *cloudtrail.ListTagsInput,
		...func(*cloudtrail.Options)) (*cloudtrail.ListTagsOutput, error)
	AddTags(context.Context, *cloudtrail.AddTagsInput,
		...func(*cloudtrail.Options)) (*cloudtrail.AddTagsOutput, error)
	RemoveTags(context.Context, *cloudtrail.RemoveTagsInput,
		...func(*cloudtrail.Options)) (*cloudtrail.RemoveTagsOutput, error)
}

func (r *TrailResource) SchemaVersion() int { return 1 }

func (r *TrailResource) ReplaceFields() []string { return []string{"name"} }

func (r TrailResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.NullableValue(r.EnableLogging, true),
		defaults.NullableValue(r.IncludeGlobalServiceEvents, true),
	}
}

func (r TrailResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.EventSelectors, r.AdvancedEventSelectors),
		constraint.Must(constraint.MaxItems(r.EventSelectors, 5)).
			Message("event-selectors holds at most 5 selectors"),
		constraint.ForEach(r.EventSelectors, func(s TrailEventSelector) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.When(constraint.Present(s.ReadWriteType)).
					Require(constraint.OneOf(s.ReadWriteType, "All", "ReadOnly", "WriteOnly")).
					Message("read-write-type must be All, ReadOnly, or WriteOnly"),
				constraint.ForEach(s.DataResources,
					func(d TrailDataResource) []constraint.Constraint {
						return []constraint.Constraint{
							constraint.Must(constraint.OneOf(d.Type,
								"AWS::S3::Object", "AWS::Lambda::Function",
								"AWS::DynamoDB::Table")).
								Message("data resource type must be supported by CloudTrail"),
							constraint.Must(constraint.MaxItems(d.Values, 250)).
								Message("data resource values holds at most 250 entries"),
						}
					}),
			}
		}),
		constraint.ForEach(r.AdvancedEventSelectors,
			func(s TrailAdvancedEventSelector) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.NotEmpty(s.FieldSelectors)).
						Message("an advanced event selector requires field-selectors"),
					constraint.ForEach(s.FieldSelectors,
						func(f TrailAdvancedFieldSelector) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.OneOf(f.Field,
									"errorCode", "eventCategory", "eventName", "eventSource",
									"eventType", "readOnly", "resources.ARN", "resources.type",
									"sessionCredentialFromConsole", "userIdentity.arn",
									"vpcEndpointId")).
									Message("advanced selector field is invalid"),
								constraint.Must(constraint.MinItems(f.Equals, 1)).
									Message("equals must not be empty"),
								constraint.Must(constraint.MinItems(f.NotEquals, 1)).
									Message("not-equals must not be empty"),
								constraint.Must(constraint.MinItems(f.StartsWith, 1)).
									Message("starts-with must not be empty"),
								constraint.Must(constraint.MinItems(f.NotStartsWith, 1)).
									Message("not-starts-with must not be empty"),
								constraint.Must(constraint.MinItems(f.EndsWith, 1)).
									Message("ends-with must not be empty"),
								constraint.Must(constraint.MinItems(f.NotEndsWith, 1)).
									Message("not-ends-with must not be empty"),
							}
						}),
				}
			}),
		constraint.ForEach(r.InsightSelectors,
			func(s TrailInsightSelector) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(s.InsightType,
						"ApiCallRateInsight", "ApiErrorRateInsight")).
						Message("insight-type must be a CloudTrail Insights type"),
					constraint.Must(constraint.MinItems(s.EventCategories, 1),
						constraint.MaxItems(s.EventCategories, 2)).
						Message("event-categories holds one or two entries"),
					constraint.ForEach(s.EventCategories,
						func(category string) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.OneOf(category,
									"Management", "Data")).
									Message("event category must be Management or Data"),
							}
						}),
				}
			}),
		constraint.Must(constraint.MaxItems(r.AggregationConfigurations, 1)).
			Message("aggregation-configurations holds at most 1 entry"),
		constraint.ForEach(r.AggregationConfigurations,
			func(a TrailAggregationConfiguration) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.Equals(a.EventCategory, "Data")).
						Message("aggregation event-category must be Data"),
					constraint.Must(constraint.MinItems(a.Templates, 1),
						constraint.MaxItems(a.Templates, 50)).
						Message("aggregation templates holds 1 to 50 entries"),
					constraint.ForEach(a.Templates,
						func(template string) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.Must(constraint.OneOf(template,
									"API_ACTIVITY", "RESOURCE_ACCESS", "USER_ACTIONS")).
									Message("aggregation template is invalid"),
							}
						}),
				}
			}),
	}
}

func (r *TrailResource) ValidateInputs(context.Context, *awsCfg) error {
	if len(r.Name) < 3 || len(r.Name) > 128 {
		return errors.New("name must be 3 to 128 characters")
	}
	if !trailNameCharacters.MatchString(r.Name) {
		return errors.New("name must contain only ASCII letters, digits, period, underscore, or hyphen")
	}
	if !isASCIIAlphanumeric(r.Name[0]) || !isASCIIAlphanumeric(r.Name[len(r.Name)-1]) {
		return errors.New("name must start and end with an ASCII letter or digit")
	}
	if trailNamePunctuation.MatchString(r.Name) {
		return errors.New("name must not contain adjacent period, underscore, or hyphen characters")
	}
	if net.ParseIP(r.Name) != nil {
		return errors.New("name must not be in IP address form")
	}
	if r.S3KeyPrefix != nil && len(*r.S3KeyPrefix) > 200 {
		return errors.New("s3-key-prefix must be at most 200 characters")
	}
	if r.SnsTopicName != nil && len(*r.SnsTopicName) > 256 {
		return errors.New("sns-topic-name must be at most 256 characters")
	}
	for name, value := range map[string]*string{
		"cloud-watch-logs-log-group-arn": r.CloudWatchLogsLogGroupArn,
		"cloud-watch-logs-role-arn":      r.CloudWatchLogsRoleArn,
		"kms-key-id":                     r.KmsKeyId,
	} {
		if value != nil && *value != "" {
			if err := validateGenericARN(*value); err != nil {
				return fmt.Errorf("%s must be a valid ARN when non-empty: %w", name, err)
			}
		}
	}
	if r.EventSelectors != nil && r.AdvancedEventSelectors != nil {
		return errors.New("event-selectors conflict with advanced-event-selectors")
	}
	if err := r.validateEventSelectors(); err != nil {
		return err
	}
	if err := r.validateAdvancedEventSelectors(); err != nil {
		return err
	}
	if err := r.validateInsightSelectors(); err != nil {
		return err
	}
	return r.validateAggregationConfigurations()
}

func (r *TrailResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*TrailResourceOutput, error) {
	if err := r.ValidateInputs(ctx, cfg); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client)
}

func (r *TrailResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[TrailResource, *TrailResourceOutput, *awsCfg],
) (*TrailResourceOutput, error) {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, trailHandle(r.Name, prior), false)
}

func (r *TrailResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[TrailResource, *TrailResourceOutput, *awsCfg],
) (*TrailResourceOutput, error) {
	if err := r.ValidateInputs(ctx, cfg); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior)
}

func (r *TrailResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[TrailResource, *TrailResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior)
}

func (r *TrailResource) create(
	ctx context.Context,
	client trailClient,
) (*TrailResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	in := r.createInput()
	var created *cloudtrail.CreateTrailOutput
	err := retry.OnError(ctx, isCloudWatchAccessDenied, func(ctx context.Context) error {
		var err error
		created, err = client.CreateTrail(ctx, in)
		return err
	}, retry.WithTimeout(trailPropagationTimeout), retry.WithInterval(trailRetryInterval))
	if err != nil {
		return nil, fmt.Errorf("create trail: %w", err)
	}
	handle := r.Name
	if created != nil && aws.ToString(created.TrailARN) != "" {
		handle = aws.ToString(created.TrailARN)
	}
	if r.loggingEnabled() {
		if _, err := client.StartLogging(ctx, &cloudtrail.StartLoggingInput{
			Name: aws.String(handle),
		}); err != nil {
			return nil, fmt.Errorf("start trail logging: %w", err)
		}
	}
	if r.EventSelectors != nil {
		if err := r.putEventSelectors(ctx, client, handle); err != nil {
			return nil, err
		}
	}
	if r.AdvancedEventSelectors != nil {
		if err := r.putAdvancedEventSelectors(ctx, client, handle); err != nil {
			return nil, err
		}
	}
	if r.InsightSelectors != nil {
		if err := r.putInsightSelectors(ctx, client, handle); err != nil {
			return nil, err
		}
	}
	if r.AggregationConfigurations != nil {
		if err := r.putAggregationConfigurations(ctx, client, handle); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, handle, true)
}

func (r *TrailResource) read(
	ctx context.Context,
	client trailClient,
	handle string,
	retryNotFound bool,
) (*TrailResourceOutput, error) {
	var trail *cloudtrailtypes.Trail
	read := func(ctx context.Context) error {
		resp, err := client.DescribeTrails(ctx, &cloudtrail.DescribeTrailsInput{
			TrailNameList: []string{handle},
		})
		if err != nil {
			return err
		}
		if resp == nil || len(resp.TrailList) == 0 {
			return &cloudtrailtypes.TrailNotFoundException{}
		}
		trail = &resp.TrailList[0]
		return nil
	}
	var err error
	if retryNotFound {
		err = retry.OnError(ctx, isTrailNotFound, read,
			retry.WithTimeout(trailPropagationTimeout),
			retry.WithInterval(trailRetryInterval))
	} else {
		err = read(ctx)
	}
	if isTrailNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe trail: %w", err)
	}
	return &TrailResourceOutput{
		Arn:         aws.ToString(trail.TrailARN),
		HomeRegion:  aws.ToString(trail.HomeRegion),
		SnsTopicArn: aws.ToString(trail.SnsTopicARN),
	}, nil
}

func (r *TrailResource) update(
	ctx context.Context,
	client trailClient,
	prior runtime.Prior[TrailResource, *TrailResourceOutput, *awsCfg],
) (*TrailResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	handle := trailPriorHandle(r.Name, prior)
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, handle); err != nil {
			return nil, err
		}
	}
	if r.directInputsChanged(prior.Inputs) {
		in := r.updateInput(handle, prior.Inputs)
		err := retry.OnError(ctx, isCloudWatchAccessDenied, func(ctx context.Context) error {
			_, err := client.UpdateTrail(ctx, in)
			return err
		}, retry.WithTimeout(trailPropagationTimeout),
			retry.WithInterval(trailRetryInterval))
		if err != nil {
			return nil, fmt.Errorf("update trail: %w", err)
		}
	}
	if runtime.Changed(prior.Inputs.EnableLogging, r.EnableLogging) {
		if err := r.setLogging(ctx, client, handle); err != nil {
			return nil, err
		}
	}
	if err := r.updateSelectorsAndInsights(ctx, client, handle, prior.Inputs); err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.AggregationConfigurations,
		r.AggregationConfigurations) {
		if err := r.putAggregationConfigurations(ctx, client, handle); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, handle, false)
}

func (r *TrailResource) delete(
	ctx context.Context,
	client trailClient,
	prior *TrailResourceOutput,
) error {
	_, err := client.DeleteTrail(ctx, &cloudtrail.DeleteTrailInput{
		Name: aws.String(trailHandle(r.Name, prior)),
	})
	if isTrailNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete trail: %w", err)
	}
	return nil
}

func (r *TrailResource) createInput() *cloudtrail.CreateTrailInput {
	return &cloudtrail.CreateTrailInput{
		Name:                       aws.String(r.Name),
		S3BucketName:               aws.String(r.S3BucketName),
		S3KeyPrefix:                r.S3KeyPrefix,
		SnsTopicName:               r.SnsTopicName,
		CloudWatchLogsLogGroupArn:  r.CloudWatchLogsLogGroupArn,
		CloudWatchLogsRoleArn:      r.CloudWatchLogsRoleArn,
		KmsKeyId:                   r.KmsKeyId,
		EnableLogFileValidation:    r.EnableLogFileValidation,
		IncludeGlobalServiceEvents: aws.Bool(r.includeGlobalServiceEvents()),
		IsMultiRegionTrail:         r.IsMultiRegionTrail,
		IsOrganizationTrail:        r.IsOrganizationTrail,
		TagsList:                   trailTags(ptr.Value(r.Tags), false),
	}
}

func (r *TrailResource) updateInput(
	handle string,
	prior TrailResource,
) *cloudtrail.UpdateTrailInput {
	in := &cloudtrail.UpdateTrailInput{Name: aws.String(handle)}
	if runtime.Changed(prior.S3BucketName, r.S3BucketName) {
		in.S3BucketName = aws.String(r.S3BucketName)
	}
	if runtime.Changed(prior.S3KeyPrefix, r.S3KeyPrefix) {
		in.S3KeyPrefix = aws.String(aws.ToString(r.S3KeyPrefix))
	}
	if runtime.Changed(prior.SnsTopicName, r.SnsTopicName) {
		in.SnsTopicName = aws.String(aws.ToString(r.SnsTopicName))
	}
	if runtime.Changed(prior.CloudWatchLogsLogGroupArn, r.CloudWatchLogsLogGroupArn) ||
		runtime.Changed(prior.CloudWatchLogsRoleArn, r.CloudWatchLogsRoleArn) {
		in.CloudWatchLogsLogGroupArn = aws.String(aws.ToString(r.CloudWatchLogsLogGroupArn))
		in.CloudWatchLogsRoleArn = aws.String(aws.ToString(r.CloudWatchLogsRoleArn))
	}
	if runtime.Changed(prior.KmsKeyId, r.KmsKeyId) {
		in.KmsKeyId = aws.String(aws.ToString(r.KmsKeyId))
	}
	if runtime.Changed(prior.EnableLogFileValidation, r.EnableLogFileValidation) {
		in.EnableLogFileValidation = aws.Bool(aws.ToBool(r.EnableLogFileValidation))
	}
	if runtime.Changed(prior.IncludeGlobalServiceEvents, r.IncludeGlobalServiceEvents) {
		in.IncludeGlobalServiceEvents = aws.Bool(r.includeGlobalServiceEvents())
	}
	if runtime.Changed(prior.IsMultiRegionTrail, r.IsMultiRegionTrail) {
		in.IsMultiRegionTrail = aws.Bool(aws.ToBool(r.IsMultiRegionTrail))
	}
	if runtime.Changed(prior.IsOrganizationTrail, r.IsOrganizationTrail) {
		in.IsOrganizationTrail = aws.Bool(aws.ToBool(r.IsOrganizationTrail))
	}
	return in
}

func (r *TrailResource) directInputsChanged(prior TrailResource) bool {
	return runtime.Changed(prior.S3BucketName, r.S3BucketName) ||
		runtime.Changed(prior.S3KeyPrefix, r.S3KeyPrefix) ||
		runtime.Changed(prior.SnsTopicName, r.SnsTopicName) ||
		runtime.Changed(prior.CloudWatchLogsLogGroupArn, r.CloudWatchLogsLogGroupArn) ||
		runtime.Changed(prior.CloudWatchLogsRoleArn, r.CloudWatchLogsRoleArn) ||
		runtime.Changed(prior.KmsKeyId, r.KmsKeyId) ||
		runtime.Changed(prior.EnableLogFileValidation, r.EnableLogFileValidation) ||
		runtime.Changed(prior.IncludeGlobalServiceEvents, r.IncludeGlobalServiceEvents) ||
		runtime.Changed(prior.IsMultiRegionTrail, r.IsMultiRegionTrail) ||
		runtime.Changed(prior.IsOrganizationTrail, r.IsOrganizationTrail)
}

func (r *TrailResource) setLogging(
	ctx context.Context,
	client trailClient,
	handle string,
) error {
	if r.loggingEnabled() {
		_, err := client.StartLogging(ctx, &cloudtrail.StartLoggingInput{Name: aws.String(handle)})
		if err != nil {
			return fmt.Errorf("start trail logging: %w", err)
		}
		return nil
	}
	_, err := client.StopLogging(ctx, &cloudtrail.StopLoggingInput{Name: aws.String(handle)})
	if err != nil {
		return fmt.Errorf("stop trail logging: %w", err)
	}
	return nil
}

func (r *TrailResource) putEventSelectors(
	ctx context.Context,
	client trailClient,
	handle string,
) error {
	selectors := expandEventSelectors(ptr.Value(r.EventSelectors))
	if len(selectors) == 0 {
		selectors = []cloudtrailtypes.EventSelector{{
			IncludeManagementEvents: aws.Bool(true),
			ReadWriteType:           cloudtrailtypes.ReadWriteTypeAll,
		}}
	}
	_, err := client.PutEventSelectors(ctx, &cloudtrail.PutEventSelectorsInput{
		TrailName:      aws.String(handle),
		EventSelectors: selectors,
	})
	if err != nil {
		return fmt.Errorf("put trail event selectors: %w", err)
	}
	return nil
}

func (r *TrailResource) putAdvancedEventSelectors(
	ctx context.Context,
	client trailClient,
	handle string,
) error {
	_, err := client.PutEventSelectors(ctx, &cloudtrail.PutEventSelectorsInput{
		TrailName: aws.String(handle),
		AdvancedEventSelectors: expandAdvancedEventSelectors(
			ptr.Value(r.AdvancedEventSelectors)),
	})
	if err != nil {
		return fmt.Errorf("put trail advanced event selectors: %w", err)
	}
	return nil
}

func (r *TrailResource) putDesiredEventSelectors(
	ctx context.Context,
	client trailClient,
	handle string,
	prior TrailResource,
) error {
	if r.EventSelectors != nil {
		return r.putEventSelectors(ctx, client, handle)
	}
	if r.AdvancedEventSelectors != nil || prior.AdvancedEventSelectors != nil {
		return r.putAdvancedEventSelectors(ctx, client, handle)
	}
	return r.putEventSelectors(ctx, client, handle)
}

func (r *TrailResource) putInsightSelectors(
	ctx context.Context,
	client trailClient,
	handle string,
) error {
	return putInsightSelectorValues(ctx, client, handle, ptr.Value(r.InsightSelectors))
}

func putInsightSelectorValues(
	ctx context.Context,
	client trailClient,
	handle string,
	selectors []TrailInsightSelector,
) error {
	_, err := client.PutInsightSelectors(ctx, &cloudtrail.PutInsightSelectorsInput{
		TrailName:        aws.String(handle),
		InsightSelectors: expandInsightSelectors(selectors),
	})
	if err != nil {
		return fmt.Errorf("put trail insight selectors: %w", err)
	}
	return nil
}

func (r *TrailResource) updateSelectorsAndInsights(
	ctx context.Context,
	client trailClient,
	handle string,
	prior TrailResource,
) error {
	selectorsChanged := runtime.Changed(prior.EventSelectors, r.EventSelectors) ||
		runtime.Changed(prior.AdvancedEventSelectors, r.AdvancedEventSelectors)
	insightsChanged := runtime.Changed(prior.InsightSelectors, r.InsightSelectors)
	if !selectorsChanged {
		if insightsChanged {
			return r.putInsightSelectors(ctx, client, handle)
		}
		return nil
	}
	if !insightsChanged {
		return r.putDesiredEventSelectors(ctx, client, handle, prior)
	}

	priorInsights := ptr.Value(prior.InsightSelectors)
	desiredInsights := ptr.Value(r.InsightSelectors)
	if desiredSelectorCoverage(*r, prior).supportsAll(priorInsights) {
		if err := r.putDesiredEventSelectors(ctx, client, handle, prior); err != nil {
			return err
		}
		return r.putInsightSelectors(ctx, client, handle)
	}
	if configuredSelectorCoverage(prior).supportsAll(desiredInsights) {
		if err := r.putInsightSelectors(ctx, client, handle); err != nil {
			return err
		}
		return r.putDesiredEventSelectors(ctx, client, handle, prior)
	}
	if err := putInsightSelectorValues(ctx, client, handle, []TrailInsightSelector{}); err != nil {
		return err
	}
	if err := r.putDesiredEventSelectors(ctx, client, handle, prior); err != nil {
		return err
	}
	return r.putInsightSelectors(ctx, client, handle)
}

func (r *TrailResource) putAggregationConfigurations(
	ctx context.Context,
	client trailClient,
	handle string,
) error {
	_, err := client.PutEventConfiguration(ctx, &cloudtrail.PutEventConfigurationInput{
		TrailName: aws.String(handle),
		AggregationConfigurations: expandAggregationConfigurations(
			ptr.Value(r.AggregationConfigurations)),
	})
	if err != nil {
		return fmt.Errorf("put trail event configuration: %w", err)
	}
	return nil
}

func (r *TrailResource) syncTags(
	ctx context.Context,
	client trailClient,
	handle string,
) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTags(ctx, &cloudtrail.ListTagsInput{
				ResourceIdList: []string{handle},
			})
			if err != nil {
				return nil, fmt.Errorf("list trail tags: %w", err)
			}
			return trailTagMap(resp), nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.AddTags(ctx, &cloudtrail.AddTagsInput{
				ResourceId: aws.String(handle),
				TagsList:   trailTags(upsert, false),
			})
			if err != nil {
				return fmt.Errorf("add trail tags: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.RemoveTags(ctx, &cloudtrail.RemoveTagsInput{
				ResourceId: aws.String(handle),
				TagsList:   trailTagKeys(remove),
			})
			if err != nil {
				return fmt.Errorf("remove trail tags: %w", err)
			}
			return nil
		},
	)
}

func (r *TrailResource) includeGlobalServiceEvents() bool {
	return r.IncludeGlobalServiceEvents == nil || *r.IncludeGlobalServiceEvents
}

func (r *TrailResource) loggingEnabled() bool {
	return r.EnableLogging == nil || *r.EnableLogging
}

func expandEventSelectors(in []TrailEventSelector) []cloudtrailtypes.EventSelector {
	out := make([]cloudtrailtypes.EventSelector, 0, len(in))
	for _, selector := range in {
		includeManagement := true
		if selector.IncludeManagementEvents != nil {
			includeManagement = *selector.IncludeManagementEvents
		}
		readWriteType := cloudtrailtypes.ReadWriteTypeAll
		if selector.ReadWriteType != nil {
			readWriteType = cloudtrailtypes.ReadWriteType(*selector.ReadWriteType)
		}
		out = append(out, cloudtrailtypes.EventSelector{
			DataResources:                 expandDataResources(ptr.Value(selector.DataResources)),
			ExcludeManagementEventSources: ptr.Value(selector.ExcludeManagementEventSources),
			IncludeManagementEvents:       aws.Bool(includeManagement),
			ReadWriteType:                 readWriteType,
		})
	}
	return out
}

func expandDataResources(in []TrailDataResource) []cloudtrailtypes.DataResource {
	out := make([]cloudtrailtypes.DataResource, 0, len(in))
	for _, resource := range in {
		out = append(out, cloudtrailtypes.DataResource{
			Type:   aws.String(resource.Type),
			Values: ptr.Value(resource.Values),
		})
	}
	return out
}

func expandAdvancedEventSelectors(
	in []TrailAdvancedEventSelector,
) []cloudtrailtypes.AdvancedEventSelector {
	out := make([]cloudtrailtypes.AdvancedEventSelector, 0, len(in))
	for _, selector := range in {
		fields := make([]cloudtrailtypes.AdvancedFieldSelector, 0, len(selector.FieldSelectors))
		for _, field := range selector.FieldSelectors {
			fields = append(fields, cloudtrailtypes.AdvancedFieldSelector{
				Field:         aws.String(field.Field),
				Equals:        ptr.Value(field.Equals),
				NotEquals:     ptr.Value(field.NotEquals),
				StartsWith:    ptr.Value(field.StartsWith),
				NotStartsWith: ptr.Value(field.NotStartsWith),
				EndsWith:      ptr.Value(field.EndsWith),
				NotEndsWith:   ptr.Value(field.NotEndsWith),
			})
		}
		out = append(out, cloudtrailtypes.AdvancedEventSelector{
			Name:           selector.Name,
			FieldSelectors: fields,
		})
	}
	return out
}

func expandInsightSelectors(in []TrailInsightSelector) []cloudtrailtypes.InsightSelector {
	out := make([]cloudtrailtypes.InsightSelector, 0, len(in))
	for _, selector := range in {
		var categories []cloudtrailtypes.SourceEventCategory
		if selector.EventCategories != nil {
			categories = make([]cloudtrailtypes.SourceEventCategory, 0,
				len(*selector.EventCategories))
			for _, category := range *selector.EventCategories {
				categories = append(categories, cloudtrailtypes.SourceEventCategory(category))
			}
		}
		out = append(out, cloudtrailtypes.InsightSelector{
			InsightType:     cloudtrailtypes.InsightType(selector.InsightType),
			EventCategories: categories,
		})
	}
	return out
}

type trailEventCoverage struct {
	managementRead  bool
	managementWrite bool
	data            bool
}

func configuredSelectorCoverage(r TrailResource) trailEventCoverage {
	if r.EventSelectors != nil {
		return standardSelectorCoverage(*r.EventSelectors)
	}
	if r.AdvancedEventSelectors != nil {
		return advancedSelectorCoverage(*r.AdvancedEventSelectors)
	}
	return standardSelectorCoverage(nil)
}

func desiredSelectorCoverage(r TrailResource, prior TrailResource) trailEventCoverage {
	if r.EventSelectors != nil {
		return standardSelectorCoverage(*r.EventSelectors)
	}
	if r.AdvancedEventSelectors != nil {
		return advancedSelectorCoverage(*r.AdvancedEventSelectors)
	}
	if prior.AdvancedEventSelectors != nil {
		return advancedSelectorCoverage(nil)
	}
	return standardSelectorCoverage(nil)
}

func standardSelectorCoverage(selectors []TrailEventSelector) trailEventCoverage {
	if len(selectors) == 0 {
		return trailEventCoverage{managementRead: true, managementWrite: true}
	}
	var coverage trailEventCoverage
	for _, selector := range selectors {
		read, write := selectorReadWriteCoverage(selector.ReadWriteType)
		if selector.IncludeManagementEvents == nil || *selector.IncludeManagementEvents {
			coverage.managementRead = coverage.managementRead || read
			coverage.managementWrite = coverage.managementWrite || write
		}
		for _, resource := range ptr.Value(selector.DataResources) {
			if len(ptr.Value(resource.Values)) > 0 && (read || write) {
				coverage.data = true
			}
		}
	}
	return coverage
}

func selectorReadWriteCoverage(readWriteType *string) (bool, bool) {
	switch aws.ToString(readWriteType) {
	case "ReadOnly":
		return true, false
	case "WriteOnly":
		return false, true
	default:
		return true, true
	}
}

func advancedSelectorCoverage(
	selectors []TrailAdvancedEventSelector,
) trailEventCoverage {
	var coverage trailEventCoverage
	for _, selector := range selectors {
		management := advancedSelectorAllows(selector, "eventCategory", "Management")
		data := advancedSelectorAllows(selector, "eventCategory", "Data")
		read := advancedSelectorAllows(selector, "readOnly", "true")
		write := advancedSelectorAllows(selector, "readOnly", "false")
		coverage.managementRead = coverage.managementRead || management && read
		coverage.managementWrite = coverage.managementWrite || management && write
		coverage.data = coverage.data || data && (read || write)
	}
	return coverage
}

func advancedSelectorAllows(
	selector TrailAdvancedEventSelector,
	fieldName string,
	value string,
) bool {
	for _, field := range selector.FieldSelectors {
		if field.Field == fieldName && !advancedFieldAllows(field, value) {
			return false
		}
	}
	return true
}

func advancedFieldAllows(field TrailAdvancedFieldSelector, value string) bool {
	if field.Equals != nil && !slices.Contains(*field.Equals, value) {
		return false
	}
	if field.NotEquals != nil && slices.Contains(*field.NotEquals, value) {
		return false
	}
	if field.StartsWith != nil && !matchesString(*field.StartsWith,
		func(candidate string) bool { return strings.HasPrefix(value, candidate) }) {
		return false
	}
	if field.NotStartsWith != nil && matchesString(*field.NotStartsWith,
		func(candidate string) bool { return strings.HasPrefix(value, candidate) }) {
		return false
	}
	if field.EndsWith != nil && !matchesString(*field.EndsWith,
		func(candidate string) bool { return strings.HasSuffix(value, candidate) }) {
		return false
	}
	if field.NotEndsWith != nil && matchesString(*field.NotEndsWith,
		func(candidate string) bool { return strings.HasSuffix(value, candidate) }) {
		return false
	}
	return true
}

func matchesString(values []string, match func(string) bool) bool {
	return slices.ContainsFunc(values, match)
}

func (c trailEventCoverage) supportsAll(insights []TrailInsightSelector) bool {
	for _, insight := range insights {
		categories := []string{"Management"}
		if insight.EventCategories != nil {
			categories = *insight.EventCategories
		}
		for _, category := range categories {
			switch category {
			case "Management":
				if insight.InsightType == "ApiCallRateInsight" {
					if !c.managementWrite {
						return false
					}
				} else if !c.managementRead && !c.managementWrite {
					return false
				}
			case "Data":
				if !c.data {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

func expandAggregationConfigurations(
	in []TrailAggregationConfiguration,
) []cloudtrailtypes.AggregationConfiguration {
	out := make([]cloudtrailtypes.AggregationConfiguration, 0, len(in))
	for _, config := range in {
		templates := make([]cloudtrailtypes.Template, 0, len(config.Templates))
		for _, template := range config.Templates {
			templates = append(templates, cloudtrailtypes.Template(template))
		}
		out = append(out, cloudtrailtypes.AggregationConfiguration{
			EventCategory: cloudtrailtypes.EventCategoryAggregation(config.EventCategory),
			Templates:     templates,
		})
	}
	return out
}

func (r *TrailResource) validateEventSelectors() error {
	selectors := ptr.Value(r.EventSelectors)
	if len(selectors) > 5 {
		return errors.New("event-selectors holds at most 5 selectors")
	}
	for _, selector := range selectors {
		if selector.ReadWriteType != nil && !oneOf(*selector.ReadWriteType,
			"All", "ReadOnly", "WriteOnly") {
			return errors.New("read-write-type must be All, ReadOnly, or WriteOnly")
		}
		for _, resource := range ptr.Value(selector.DataResources) {
			if !oneOf(resource.Type, "AWS::S3::Object", "AWS::Lambda::Function",
				"AWS::DynamoDB::Table") {
				return errors.New("data resource type is invalid")
			}
			values := ptr.Value(resource.Values)
			if len(values) > 250 {
				return errors.New("data resource values holds at most 250 entries")
			}
			if !uniqueStrings(values) {
				return errors.New("data resource values must be unique")
			}
		}
	}
	return nil
}

func (r *TrailResource) validateAdvancedEventSelectors() error {
	totalValues := 0
	for _, selector := range ptr.Value(r.AdvancedEventSelectors) {
		if len(selector.FieldSelectors) == 0 {
			return errors.New("an advanced event selector requires field-selectors")
		}
		for _, field := range selector.FieldSelectors {
			if !oneOf(field.Field,
				"errorCode", "eventCategory", "eventName", "eventSource", "eventType",
				"readOnly", "resources.ARN", "resources.type",
				"sessionCredentialFromConsole", "userIdentity.arn", "vpcEndpointId") {
				return errors.New("advanced selector field is invalid")
			}
			count, err := validateAdvancedFieldValues(field)
			if err != nil {
				return err
			}
			totalValues += count
			if totalValues > 500 {
				return errors.New("advanced selector operators hold at most 500 values")
			}
		}
		category, err := advancedSelectorCategory(selector)
		if err != nil {
			return err
		}
		if err := validateAdvancedSelectorFields(selector, category); err != nil {
			return err
		}
	}
	return nil
}

type advancedFieldOperator struct {
	name   string
	values *[]string
}

func advancedFieldOperators(field TrailAdvancedFieldSelector) [6]advancedFieldOperator {
	return [6]advancedFieldOperator{
		{name: "equals", values: field.Equals},
		{name: "not-equals", values: field.NotEquals},
		{name: "starts-with", values: field.StartsWith},
		{name: "not-starts-with", values: field.NotStartsWith},
		{name: "ends-with", values: field.EndsWith},
		{name: "not-ends-with", values: field.NotEndsWith},
	}
}

func validateAdvancedFieldValues(field TrailAdvancedFieldSelector) (int, error) {
	count := 0
	hasOperator := false
	for _, operator := range advancedFieldOperators(field) {
		if operator.values == nil {
			continue
		}
		hasOperator = true
		if len(*operator.values) == 0 {
			return 0, fmt.Errorf("%s must not be empty", operator.name)
		}
		if !uniqueStrings(*operator.values) {
			return 0, fmt.Errorf("%s values must be unique", operator.name)
		}
		for _, value := range *operator.values {
			length := utf8.RuneCountInString(value)
			if length < 1 || length > 2048 {
				return 0, fmt.Errorf("%s values must be 1 to 2048 characters", operator.name)
			}
		}
		count += len(*operator.values)
	}
	if !hasOperator {
		return 0, fmt.Errorf("advanced selector field %s requires an operator", field.Field)
	}
	return count, nil
}

func advancedSelectorCategory(selector TrailAdvancedEventSelector) (string, error) {
	var categoryField *TrailAdvancedFieldSelector
	for i := range selector.FieldSelectors {
		field := &selector.FieldSelectors[i]
		if field.Field != "eventCategory" {
			continue
		}
		if categoryField != nil {
			return "", errors.New("an advanced selector requires exactly one eventCategory")
		}
		categoryField = field
	}
	if categoryField == nil {
		return "", errors.New("an advanced selector requires exactly one eventCategory")
	}
	if !advancedFieldUsesOnly(*categoryField, "equals") {
		return "", errors.New("eventCategory must use only equals")
	}
	values := ptr.Value(categoryField.Equals)
	if len(values) != 1 {
		return "", errors.New("eventCategory equals requires exactly one value")
	}
	if !oneOf(values[0], "Management", "Data", "NetworkActivity") {
		return "", errors.New("eventCategory must be Management, Data, or NetworkActivity")
	}
	return values[0], nil
}

func validateAdvancedSelectorFields(
	selector TrailAdvancedEventSelector,
	category string,
) error {
	resourceTypes := 0
	eventSources := 0
	for _, field := range selector.FieldSelectors {
		if !advancedFieldAllowed(category, field.Field) {
			return fmt.Errorf("advanced selector field %s is not allowed for %s",
				field.Field, category)
		}
		if field.Field == "resources.type" {
			resourceTypes++
		}
		if field.Field == "eventSource" {
			eventSources++
		}
		if err := validateAdvancedFieldOperators(field, category); err != nil {
			return err
		}
	}
	if category == "Data" && resourceTypes != 1 {
		return errors.New("a Data selector requires exactly one resources.type")
	}
	if category == "NetworkActivity" && eventSources == 0 {
		return errors.New("a NetworkActivity selector requires eventSource")
	}
	return nil
}

func advancedFieldAllowed(category string, field string) bool {
	switch category {
	case "Management":
		return oneOf(field, "eventCategory", "eventSource", "readOnly")
	case "Data":
		return oneOf(field,
			"eventCategory", "resources.type", "resources.ARN", "eventName",
			"eventSource", "eventType", "readOnly", "sessionCredentialFromConsole",
			"userIdentity.arn")
	case "NetworkActivity":
		return oneOf(field,
			"eventCategory", "eventSource", "eventName", "errorCode", "vpcEndpointId")
	default:
		return false
	}
}

func validateAdvancedFieldOperators(
	field TrailAdvancedFieldSelector,
	category string,
) error {
	switch field.Field {
	case "eventCategory", "readOnly":
		if !advancedFieldUsesOnly(field, "equals") {
			return fmt.Errorf("%s must use only equals", field.Field)
		}
	case "resources.type":
		if !advancedFieldUsesOnly(field, "equals") {
			return errors.New("resources.type must use only equals")
		}
		if len(ptr.Value(field.Equals)) != 1 {
			return errors.New("resources.type equals requires exactly one value")
		}
	case "sessionCredentialFromConsole":
		if !advancedFieldUsesOnly(field, "equals", "not-equals") {
			return errors.New(
				"sessionCredentialFromConsole supports only equals and not-equals")
		}
	case "eventSource", "errorCode":
		if category == "NetworkActivity" && !advancedFieldUsesOnly(field, "equals") {
			return fmt.Errorf("%s must use only equals for NetworkActivity", field.Field)
		}
	}
	if category == "NetworkActivity" && field.Field == "errorCode" {
		for _, value := range ptr.Value(field.Equals) {
			if value != "VpceAccessDenied" {
				return errors.New("errorCode equals accepts only VpceAccessDenied")
			}
		}
	}
	return nil
}

func advancedFieldUsesOnly(field TrailAdvancedFieldSelector, allowed ...string) bool {
	for _, operator := range advancedFieldOperators(field) {
		if operator.values != nil && !slices.Contains(allowed, operator.name) {
			return false
		}
	}
	return true
}

func (r *TrailResource) validateInsightSelectors() error {
	for _, selector := range ptr.Value(r.InsightSelectors) {
		if !oneOf(selector.InsightType, "ApiCallRateInsight", "ApiErrorRateInsight") {
			return errors.New("insight-type must be a CloudTrail Insights type")
		}
		if selector.EventCategories == nil {
			continue
		}
		categories := *selector.EventCategories
		if len(categories) < 1 || len(categories) > 2 {
			return errors.New("event-categories holds one or two entries")
		}
		if !uniqueStrings(categories) {
			return errors.New("event-categories values must be unique")
		}
		for _, category := range categories {
			if !oneOf(category, "Management", "Data") {
				return errors.New("event category must be Management or Data")
			}
		}
	}
	return nil
}

func (r *TrailResource) validateAggregationConfigurations() error {
	configs := ptr.Value(r.AggregationConfigurations)
	if len(configs) > 1 {
		return errors.New("aggregation-configurations holds at most 1 entry")
	}
	for _, config := range configs {
		if config.EventCategory != "Data" {
			return errors.New("aggregation event-category must be Data")
		}
		if len(config.Templates) < 1 || len(config.Templates) > 50 {
			return errors.New("aggregation templates holds 1 to 50 entries")
		}
		if !uniqueStrings(config.Templates) {
			return errors.New("aggregation templates must be unique")
		}
		for _, template := range config.Templates {
			if !oneOf(template, "API_ACTIVITY", "RESOURCE_ACCESS", "USER_ACTIONS") {
				return errors.New("aggregation template is invalid")
			}
		}
	}
	return nil
}

func trailTags(tags map[string]string, keysOnly bool) []cloudtrailtypes.Tag {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		if !strings.HasPrefix(key, "aws:") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]cloudtrailtypes.Tag, 0, len(keys))
	for _, key := range keys {
		tag := cloudtrailtypes.Tag{Key: aws.String(key)}
		if !keysOnly {
			tag.Value = aws.String(tags[key])
		}
		out = append(out, tag)
	}
	return out
}

func trailTagKeys(keys []string) []cloudtrailtypes.Tag {
	tags := make(map[string]string, len(keys))
	for _, key := range keys {
		tags[key] = ""
	}
	return trailTags(tags, true)
}

func trailTagMap(resp *cloudtrail.ListTagsOutput) map[string]string {
	out := map[string]string{}
	if resp == nil {
		return out
	}
	for _, resource := range resp.ResourceTagList {
		for _, tag := range resource.TagsList {
			out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}
	return out
}

func trailHandle(fallback string, prior *TrailResourceOutput) string {
	if prior != nil && prior.Arn != "" {
		return prior.Arn
	}
	return fallback
}

func trailPriorHandle(
	fallback string,
	prior runtime.Prior[TrailResource, *TrailResourceOutput, *awsCfg],
) string {
	if prior.Observed != nil && prior.Observed.Arn != "" {
		return prior.Observed.Arn
	}
	return trailHandle(fallback, prior.Outputs)
}

func uniqueStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func oneOf(value string, values ...string) bool {
	return slices.Contains(values, value)
}

func isASCIIAlphanumeric(value byte) bool {
	return value >= '0' && value <= '9' ||
		value >= 'A' && value <= 'Z' ||
		value >= 'a' && value <= 'z'
}

func validateGenericARN(value string) error {
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return err
	}
	if !arnPartition.MatchString(parsed.Partition) {
		return errors.New("partition is empty or malformed")
	}
	if parsed.Region != "" && !arnRegion.MatchString(parsed.Region) {
		return errors.New("region is malformed")
	}
	if parsed.AccountID != "" && !arnAccount.MatchString(parsed.AccountID) {
		return errors.New("account is malformed")
	}
	if parsed.Resource == "" {
		return errors.New("resource is empty")
	}
	return nil
}
