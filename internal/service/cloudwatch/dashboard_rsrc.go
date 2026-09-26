package cloudwatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatch "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

var (
	dashboardNamePattern     = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)
	dashboardTagKeyPattern   = regexp.MustCompile(`^[\p{L}\p{Z}\p{N}_.:/=+@-]+$`)
	dashboardTagValuePattern = regexp.MustCompile(`^[\p{L}\p{Z}\p{N}_.:/=+@-]*$`)
)

type DashboardResource struct {
	Name string             `ub:"name"`
	Body string             `ub:"body"`
	Tags *map[string]string `ub:"tags"`
}

type DashboardResourceOutput struct {
	Arn  string `ub:"arn"`
	Name string `ub:"name"`
}

func (r *DashboardResource) SchemaVersion() int { return 1 }

func (r *DashboardResource) ReplaceFields() []string {
	return []string{"name"}
}

func (r DashboardResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.MaxItems(r.Tags, 50)).
			Message("tags holds at most 50 entries"),
	}
}

func (r *DashboardResource) EquivalentInput(
	field string,
	prior DashboardResource,
	current DashboardResource,
) bool {
	if field != "body" {
		return false
	}
	if prior.Body == current.Body {
		return true
	}
	return equivalentDashboardJSON(prior.Body, current.Body)
}

func (r *DashboardResource) ValidateInputs(context.Context, *awsCfg) error {
	if len(r.Name) < 1 || len(r.Name) > 255 {
		return errors.New("name must be 1 to 255 ASCII characters")
	}
	if !dashboardNamePattern.MatchString(r.Name) {
		return errors.New("name must contain only ASCII letters, digits, underscore, or hyphen")
	}
	if r.Body != "" && !json.Valid([]byte(r.Body)) {
		return errors.New("body must be valid JSON")
	}
	tags := ptr.Value(r.Tags)
	if len(tags) > 50 {
		return errors.New("tags must have at most 50 entries")
	}
	for key, value := range tags {
		if n := utf8.RuneCountInString(key); n < 1 || n > 128 {
			return errors.New("tag key must be 1 to 128 characters")
		}
		if strings.HasPrefix(key, "aws:") {
			return errors.New("tag key must not begin with aws:")
		}
		if !dashboardTagKeyPattern.MatchString(key) {
			return errors.New("tag key contains invalid characters")
		}
		if utf8.RuneCountInString(value) > 256 {
			return errors.New("tag value must be at most 256 characters")
		}
		if !dashboardTagValuePattern.MatchString(value) {
			return errors.New("tag value contains invalid characters")
		}
	}
	return nil
}

func (r *DashboardResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*DashboardResourceOutput, error) {
	if err := r.ValidateInputs(ctx, cfg); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client)
}

func (r *DashboardResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[DashboardResource, *DashboardResourceOutput, *awsCfg],
) (*DashboardResourceOutput, error) {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	name := r.Name
	if prior != nil && prior.Name != "" {
		name = prior.Name
	}
	return r.read(ctx, client, name)
}

func (r *DashboardResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[DashboardResource, *DashboardResourceOutput, *awsCfg],
) (*DashboardResourceOutput, error) {
	if err := r.ValidateInputs(ctx, cfg); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior)
}

func (r *DashboardResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[DashboardResource, *DashboardResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior)
}

type dashboardClient interface {
	PutDashboard(
		context.Context,
		*cloudwatch.PutDashboardInput,
		...func(*cloudwatch.Options),
	) (*cloudwatch.PutDashboardOutput, error)
	GetDashboard(
		context.Context,
		*cloudwatch.GetDashboardInput,
		...func(*cloudwatch.Options),
	) (*cloudwatch.GetDashboardOutput, error)
	DeleteDashboards(
		context.Context,
		*cloudwatch.DeleteDashboardsInput,
		...func(*cloudwatch.Options),
	) (*cloudwatch.DeleteDashboardsOutput, error)
	ListTagsForResource(
		context.Context,
		*cloudwatch.ListTagsForResourceInput,
		...func(*cloudwatch.Options),
	) (*cloudwatch.ListTagsForResourceOutput, error)
	TagResource(
		context.Context,
		*cloudwatch.TagResourceInput,
		...func(*cloudwatch.Options),
	) (*cloudwatch.TagResourceOutput, error)
	UntagResource(
		context.Context,
		*cloudwatch.UntagResourceInput,
		...func(*cloudwatch.Options),
	) (*cloudwatch.UntagResourceOutput, error)
}

func (r *DashboardResource) create(
	ctx context.Context,
	client dashboardClient,
) (*DashboardResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	_, err := client.PutDashboard(ctx, &cloudwatch.PutDashboardInput{
		DashboardName: aws.String(r.Name),
		DashboardBody: aws.String(r.Body),
	})
	if err != nil {
		return nil, fmt.Errorf("put dashboard: %w", err)
	}
	out, err := r.read(ctx, client, r.Name)
	if err != nil {
		return nil, err
	}
	if len(ptr.Value(r.Tags)) > 0 {
		if _, err := client.TagResource(ctx, &cloudwatch.TagResourceInput{
			ResourceARN: aws.String(out.Arn),
			Tags:        dashboardTags(ptr.Value(r.Tags)),
		}); err != nil {
			return nil, fmt.Errorf("tag dashboard: %w", err)
		}
	}
	return out, nil
}

func (r *DashboardResource) read(
	ctx context.Context,
	client dashboardClient,
	name string,
) (*DashboardResourceOutput, error) {
	resp, err := client.GetDashboard(ctx, &cloudwatch.GetDashboardInput{
		DashboardName: aws.String(name),
	})
	if isDashboardNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get dashboard: %w", err)
	}
	if resp == nil {
		return nil, runtime.ErrNotFound
	}
	return &DashboardResourceOutput{
		Arn:  aws.ToString(resp.DashboardArn),
		Name: aws.ToString(resp.DashboardName),
	}, nil
}

func (r *DashboardResource) update(
	ctx context.Context,
	client dashboardClient,
	prior runtime.Prior[DashboardResource, *DashboardResourceOutput, *awsCfg],
) (*DashboardResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.Body, r.Body) &&
		!r.EquivalentInput("body", prior.Inputs, *r) {
		if _, err := client.PutDashboard(ctx, &cloudwatch.PutDashboardInput{
			DashboardName: aws.String(r.Name),
			DashboardBody: aws.String(r.Body),
		}); err != nil {
			return nil, fmt.Errorf("put dashboard: %w", err)
		}
	}
	handle := dashboardPriorOutput(prior)
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if handle.Arn == "" {
			return nil, errors.New("dashboard ARN is unavailable for tag update")
		}
		if err := r.syncTags(ctx, client, handle.Arn); err != nil {
			return nil, err
		}
	}
	name := handle.Name
	if name == "" {
		name = r.Name
	}
	return r.read(ctx, client, name)
}

func (r *DashboardResource) delete(
	ctx context.Context,
	client dashboardClient,
	prior *DashboardResourceOutput,
) error {
	name := r.Name
	if prior != nil && prior.Name != "" {
		name = prior.Name
	}
	_, err := client.DeleteDashboards(ctx, &cloudwatch.DeleteDashboardsInput{
		DashboardNames: []string{name},
	})
	if err != nil {
		return fmt.Errorf("delete dashboards: %w", err)
	}
	return nil
}

func (r *DashboardResource) syncTags(
	ctx context.Context,
	client dashboardClient,
	arn string,
) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForResource(ctx,
				&cloudwatch.ListTagsForResourceInput{ResourceARN: aws.String(arn)})
			if err != nil {
				return nil, fmt.Errorf("list dashboard tags: %w", err)
			}
			if resp == nil {
				return map[string]string{}, nil
			}
			return dashboardTagsToMap(resp.Tags), nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &cloudwatch.TagResourceInput{
				ResourceARN: aws.String(arn),
				Tags:        dashboardTags(upsert),
			})
			if err != nil {
				return fmt.Errorf("tag dashboard: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &cloudwatch.UntagResourceInput{
				ResourceARN: aws.String(arn),
				TagKeys:     remove,
			})
			if err != nil {
				return fmt.Errorf("untag dashboard: %w", err)
			}
			return nil
		},
	)
}

func dashboardPriorOutput(
	prior runtime.Prior[DashboardResource, *DashboardResourceOutput, *awsCfg],
) DashboardResourceOutput {
	var out DashboardResourceOutput
	if prior.Outputs != nil {
		out = *prior.Outputs
	}
	if prior.Observed != nil {
		if prior.Observed.Arn != "" {
			out.Arn = prior.Observed.Arn
		}
		if prior.Observed.Name != "" {
			out.Name = prior.Observed.Name
		}
	}
	return out
}

func dashboardTags(tags map[string]string) []cloudwatchtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]cloudwatchtypes.Tag, 0, len(tags))
	for _, key := range keys {
		out = append(out, cloudwatchtypes.Tag{
			Key:   aws.String(key),
			Value: aws.String(tags[key]),
		})
	}
	return out
}

func dashboardTagsToMap(tags []cloudwatchtypes.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return out
}

func isDashboardNotFound(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceNotFound"
}

func equivalentDashboardJSON(left, right string) bool {
	leftValue, err := decodeDashboardJSON(left)
	if err != nil {
		return false
	}
	rightValue, err := decodeDashboardJSON(right)
	if err != nil {
		return false
	}
	return equivalentDashboardJSONValue(leftValue, rightValue)
}

func decodeDashboardJSON(value string) (any, error) {
	if !json.Valid([]byte(value)) {
		return nil, errors.New("invalid JSON")
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func equivalentDashboardJSONValue(left, right any) bool {
	switch left := left.(type) {
	case nil:
		return right == nil
	case bool:
		right, ok := right.(bool)
		return ok && left == right
	case string:
		right, ok := right.(string)
		return ok && left == right
	case json.Number:
		right, ok := right.(json.Number)
		return ok && equivalentDashboardJSONNumber(left, right)
	case []any:
		right, ok := right.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for i := range left {
			if !equivalentDashboardJSONValue(left[i], right[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		right, ok := right.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, leftValue := range left {
			rightValue, ok := right[key]
			if !ok || !equivalentDashboardJSONValue(leftValue, rightValue) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

type dashboardJSONNumber struct {
	negative bool
	digits   string
	exponent big.Int
}

func equivalentDashboardJSONNumber(left, right json.Number) bool {
	leftNumber, ok := normalizeDashboardJSONNumber(left.String())
	if !ok {
		return false
	}
	rightNumber, ok := normalizeDashboardJSONNumber(right.String())
	if !ok {
		return false
	}
	return leftNumber.negative == rightNumber.negative &&
		leftNumber.digits == rightNumber.digits &&
		leftNumber.exponent.Cmp(&rightNumber.exponent) == 0
}

func normalizeDashboardJSONNumber(value string) (dashboardJSONNumber, bool) {
	var out dashboardJSONNumber
	if strings.HasPrefix(value, "-") {
		out.negative = true
		value = value[1:]
	}
	mantissa := value
	exponentText := "0"
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		mantissa = value[:index]
		exponentText = value[index+1:]
	}
	if strings.HasPrefix(exponentText, "+") {
		exponentText = exponentText[1:]
	}
	if _, ok := out.exponent.SetString(exponentText, 10); !ok {
		return dashboardJSONNumber{}, false
	}
	fractionLength := 0
	if index := strings.IndexByte(mantissa, '.'); index >= 0 {
		fractionLength = len(mantissa) - index - 1
		mantissa = mantissa[:index] + mantissa[index+1:]
	}
	mantissa = strings.TrimLeft(mantissa, "0")
	if mantissa == "" {
		out.negative = false
		out.digits = "0"
		out.exponent.SetInt64(0)
		return out, true
	}
	trimmed := strings.TrimRight(mantissa, "0")
	trailingZeros := len(mantissa) - len(trimmed)
	out.digits = trimmed
	out.exponent.Sub(&out.exponent, big.NewInt(int64(fractionLength)))
	out.exponent.Add(&out.exponent, big.NewInt(int64(trailingZeros)))
	return out, true
}
