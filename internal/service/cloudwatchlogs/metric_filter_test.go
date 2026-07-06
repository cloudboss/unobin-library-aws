package cloudwatchlogs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricFilterPutInput(t *testing.T) {
	defaultValue := 0.0
	dimensions := map[string]string{"service": "$.service"}
	r := MetricFilterResource{
		FilterName:             "errors",
		LogGroupName:           "app/logs",
		FilterPattern:          "  { $.level = \"error\" }\n",
		ApplyOnTransformedLogs: aws.Bool(true),
		MetricTransformation: MetricFilterMetricTransformation{
			DefaultValue:    &defaultValue,
			Dimensions:      &dimensions,
			MetricName:      "ErrorCount",
			MetricNamespace: "Unobin/Integration",
			MetricValue:     "1",
			Unit:            aws.String("Count"),
		},
	}

	in := r.putInput()
	require.Len(t, in.MetricTransformations, 1)
	mt := in.MetricTransformations[0]
	assert.Equal(t, "errors", aws.ToString(in.FilterName))
	assert.Equal(t, "app/logs", aws.ToString(in.LogGroupName))
	assert.Equal(t, `{ $.level = "error" }`, aws.ToString(in.FilterPattern))
	assert.True(t, in.ApplyOnTransformedLogs)
	assert.Equal(t, &defaultValue, mt.DefaultValue)
	assert.Equal(t, map[string]string{"service": "$.service"}, mt.Dimensions)
	assert.Equal(t, "ErrorCount", aws.ToString(mt.MetricName))
	assert.Equal(t, "Unobin/Integration", aws.ToString(mt.MetricNamespace))
	assert.Equal(t, "1", aws.ToString(mt.MetricValue))
	assert.Equal(t, cloudwatchlogstypes.StandardUnitCount, mt.Unit)
}

func TestMetricFilterPutInputDefaultsAndOmissions(t *testing.T) {
	emptyDimensions := map[string]string{}
	r := MetricFilterResource{
		FilterName:    "all",
		LogGroupName:  "app/logs",
		FilterPattern: "",
		MetricTransformation: MetricFilterMetricTransformation{
			Dimensions: &emptyDimensions,
		},
	}

	in := r.putInput()
	require.Len(t, in.MetricTransformations, 1)
	mt := in.MetricTransformations[0]
	assert.False(t, in.ApplyOnTransformedLogs)
	assert.Nil(t, mt.Dimensions)
	assert.Nil(t, mt.MetricName)
	assert.Nil(t, mt.MetricNamespace)
	assert.Nil(t, mt.MetricValue)
	assert.Equal(t, cloudwatchlogstypes.StandardUnitNone, mt.Unit)
}

func TestMetricFilterTransformationOutput(t *testing.T) {
	defaultValue := 0.0
	dimensions := map[string]string{"service": "api"}
	tests := []struct {
		name string
		in   []cloudwatchlogstypes.MetricTransformation
		want MetricFilterMetricTransformationOutput
	}{
		{
			name: "missing transformation still reports unit default",
			want: MetricFilterMetricTransformationOutput{Unit: metricFilterDefaultUnit},
		},
		{
			name: "empty unit reports unit default",
			in: []cloudwatchlogstypes.MetricTransformation{
				{
					DefaultValue:    &defaultValue,
					Dimensions:      dimensions,
					MetricName:      aws.String("Count"),
					MetricNamespace: aws.String("Unobin/Integration"),
					MetricValue:     aws.String("1"),
				},
			},
			want: MetricFilterMetricTransformationOutput{
				DefaultValue:    &defaultValue,
				Dimensions:      &dimensions,
				MetricName:      "Count",
				MetricNamespace: "Unobin/Integration",
				MetricValue:     "1",
				Unit:            metricFilterDefaultUnit,
			},
		},
		{
			name: "explicit unit is preserved",
			in: []cloudwatchlogstypes.MetricTransformation{
				{Unit: cloudwatchlogstypes.StandardUnitCount},
			},
			want: MetricFilterMetricTransformationOutput{Unit: "Count"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := metricFilterTransformationOutput(tt.in)
			assert.Equal(t, tt.want, got)
			if got.Dimensions != nil {
				(*got.Dimensions)["service"] = "changed"
				assert.Equal(t, "api", dimensions["service"])
			}
		})
	}
}

func TestMetricFilterUpdateValidatesBeforeNoop(t *testing.T) {
	r := MetricFilterResource{
		FilterName:    "metric-filter",
		LogGroupName:  "app/logs",
		FilterPattern: strings.Repeat(" ", metricFilterPatternMaxRunes+1),
		MetricTransformation: MetricFilterMetricTransformation{
			MetricName:      "Count",
			MetricNamespace: "Unobin/Integration",
			MetricValue:     "1",
		},
	}
	priorInputs := r
	priorInputs.FilterPattern = ""
	prior := runtime.Prior[MetricFilterResource, *MetricFilterResourceOutput]{
		Inputs: priorInputs,
		Observed: &MetricFilterResourceOutput{
			FilterPattern:          "",
			MetricTransformation:   MetricFilterMetricTransformationOutput{Unit: metricFilterDefaultUnit},
			ApplyOnTransformedLogs: false,
		},
	}

	_, err := r.Update(context.Background(), nil, prior)
	require.ErrorContains(t, err, "filter-pattern must be at most 1024 characters")
}

func TestMetricFilterShouldPut(t *testing.T) {
	pattern := "{ $.level = \"error\" }"
	unit := "Count"
	dimensions := map[string]string{"service": "api"}
	defaultValue := 0.0
	r := MetricFilterResource{
		FilterPattern:          "  " + pattern + "\n",
		ApplyOnTransformedLogs: aws.Bool(true),
		MetricTransformation: MetricFilterMetricTransformation{
			DefaultValue:    &defaultValue,
			Dimensions:      &dimensions,
			MetricName:      "Count",
			MetricNamespace: "Unobin/Integration",
			MetricValue:     "1",
			Unit:            &unit,
		},
	}

	prior := runtimePriorMetricFilter(r)
	assert.False(t, r.shouldPut(prior))

	prior.Observed.FilterPattern = "changed"
	assert.True(t, r.shouldPut(prior))

	prior = runtimePriorMetricFilter(r)
	prior.Observed.MetricTransformation.Unit = "None"
	assert.True(t, r.shouldPut(prior))

	prior = runtimePriorMetricFilter(r)
	prior.Inputs.FilterPattern = pattern
	assert.False(t, r.shouldPut(prior))
}

func runtimePriorMetricFilter(
	r MetricFilterResource) runtime.Prior[MetricFilterResource, *MetricFilterResourceOutput] {
	return runtime.Prior[MetricFilterResource, *MetricFilterResourceOutput]{
		Inputs: r,
		Observed: &MetricFilterResourceOutput{
			FilterPattern:          strings.TrimSpace(r.FilterPattern),
			ApplyOnTransformedLogs: aws.ToBool(r.ApplyOnTransformedLogs),
			MetricTransformation: MetricFilterMetricTransformationOutput{
				DefaultValue:    r.MetricTransformation.DefaultValue,
				Dimensions:      r.MetricTransformation.Dimensions,
				MetricName:      r.MetricTransformation.MetricName,
				MetricNamespace: r.MetricTransformation.MetricNamespace,
				MetricValue:     r.MetricTransformation.MetricValue,
				Unit:            effectiveMetricFilterUnit(r.MetricTransformation.Unit),
			},
		},
	}
}

func TestMetricFilterValidate(t *testing.T) {
	valid := MetricFilterResource{
		FilterName:    "metric-filter",
		LogGroupName:  "app/logs",
		FilterPattern: "",
		MetricTransformation: MetricFilterMetricTransformation{
			MetricName:      "Count",
			MetricNamespace: "Unobin/Integration",
			MetricValue:     "1",
			Unit:            aws.String("None"),
		},
	}

	tests := []struct {
		name    string
		modify  func(*MetricFilterResource)
		wantErr string
	}{
		{
			name: "valid",
		},
		{
			name: "empty filter name",
			modify: func(r *MetricFilterResource) {
				r.FilterName = ""
			},
			wantErr: "filter-name must be 1 to 512 bytes",
		},
		{
			name: "invalid filter name character",
			modify: func(r *MetricFilterResource) {
				r.FilterName = "bad:name"
			},
			wantErr: "filter-name must not contain colon or asterisk",
		},
		{
			name: "invalid log group name",
			modify: func(r *MetricFilterResource) {
				r.LogGroupName = "bad name"
			},
			wantErr: "log-group-name must contain only",
		},
		{
			name: "pattern too long",
			modify: func(r *MetricFilterResource) {
				r.FilterPattern = strings.Repeat("é", metricFilterPatternMaxRunes+1)
			},
			wantErr: "filter-pattern must be at most 1024 characters",
		},
		{
			name: "metric name invalid character",
			modify: func(r *MetricFilterResource) {
				r.MetricTransformation.MetricName = "bad$name"
			},
			wantErr: "metric-name must not contain colon, asterisk, or dollar sign",
		},
		{
			name: "metric namespace too long",
			modify: func(r *MetricFilterResource) {
				r.MetricTransformation.MetricNamespace = strings.Repeat(
					"n", metricTransformationNameMaxBytes+1)
			},
			wantErr: "metric-namespace must be at most 255 bytes",
		},
		{
			name: "metric namespace too many bytes",
			modify: func(r *MetricFilterResource) {
				r.MetricTransformation.MetricNamespace = strings.Repeat(
					"é", metricTransformationNameMaxBytes/2+1)
			},
			wantErr: "metric-namespace must be at most 255 bytes",
		},
		{
			name: "metric value too long",
			modify: func(r *MetricFilterResource) {
				r.MetricTransformation.MetricValue = strings.Repeat(
					"v", metricTransformationValueMaxBytes+1)
			},
			wantErr: "metric-value must be at most 100 bytes",
		},
		{
			name: "unit invalid",
			modify: func(r *MetricFilterResource) {
				r.MetricTransformation.Unit = aws.String("BadUnit")
			},
			wantErr: "unit must be a valid CloudWatch unit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid
			if tt.modify != nil {
				tt.modify(&r)
			}
			err := r.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLogResourceLimitRetryer(t *testing.T) {
	retryer := logResourceLimitRetryer{Retryer: stubRetryer{retryable: true}}

	limitErr := &cloudwatchlogstypes.LimitExceededException{
		Message: aws.String("Resource limit exceeded: metric filters per log group"),
	}
	assert.False(t, retryer.IsErrorRetryable(limitErr))

	otherLimit := &cloudwatchlogstypes.LimitExceededException{
		Message: aws.String("Rate exceeded"),
	}
	assert.True(t, retryer.IsErrorRetryable(otherLimit))

	assert.True(t, retryer.IsErrorRetryable(errors.New("delegate")))
}

type stubRetryer struct {
	retryable bool
}

func (s stubRetryer) IsErrorRetryable(error) bool { return s.retryable }

func (s stubRetryer) MaxAttempts() int { return 3 }

func (s stubRetryer) RetryDelay(int, error) (time.Duration, error) { return 0, nil }

func (s stubRetryer) GetRetryToken(
	context.Context, error,
) (func(error) error, error) {
	return func(error) error { return nil }, nil
}

func (s stubRetryer) GetInitialToken() func(error) error {
	return func(error) error { return nil }
}
