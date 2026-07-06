package cloudwatch

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePeriod(t *testing.T) {
	cases := []struct {
		name           string
		period         *int64
		highResolution bool
		wantErr        bool
	}{
		{name: "nil passes", period: nil, wantErr: false},
		{name: "60 multiple", period: aws.Int64(60), wantErr: false},
		{name: "300 multiple", period: aws.Int64(300), wantErr: false},
		{name: "10 standard", period: aws.Int64(10), wantErr: false},
		{name: "20 standard", period: aws.Int64(20), wantErr: false},
		{name: "30 standard", period: aws.Int64(30), wantErr: false},
		{name: "45 invalid", period: aws.Int64(45), wantErr: true},
		{name: "0 invalid", period: aws.Int64(0), wantErr: true},
		{name: "1 invalid without high resolution", period: aws.Int64(1), wantErr: true},
		{name: "5 invalid without high resolution", period: aws.Int64(5), wantErr: true},
		{name: "1 valid with high resolution", period: aws.Int64(1),
			highResolution: true, wantErr: false},
		{name: "5 valid with high resolution", period: aws.Int64(5),
			highResolution: true, wantErr: false},
		{name: "7 invalid with high resolution", period: aws.Int64(7),
			highResolution: true, wantErr: true},
		{name: "120 multiple with high resolution", period: aws.Int64(120),
			highResolution: true, wantErr: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePeriod("period", tt.period, tt.highResolution)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateNamespace(t *testing.T) {
	cases := []struct {
		name      string
		namespace *string
		wantErr   bool
	}{
		{name: "nil passes", namespace: nil, wantErr: false},
		{name: "ordinary namespace", namespace: aws.String("AWS/EC2"), wantErr: false},
		{name: "custom namespace", namespace: aws.String("My/App"), wantErr: false},
		{name: "empty rejected", namespace: aws.String(""), wantErr: true},
		{name: "colon rejected", namespace: aws.String("bad:namespace"), wantErr: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNamespace(tt.namespace)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidStat(t *testing.T) {
	cases := []struct {
		name string
		stat string
		want bool
	}{
		{name: "standard Average", stat: "Average", want: true},
		{name: "standard SampleCount", stat: "SampleCount", want: true},
		{name: "standard Sum", stat: "Sum", want: true},
		{name: "standard Minimum", stat: "Minimum", want: true},
		{name: "standard Maximum", stat: "Maximum", want: true},
		{name: "percentile p99", stat: "p99", want: true},
		{name: "percentile p99.9", stat: "p99.9", want: true},
		{name: "percentile p0.0", stat: "p0.0", want: true},
		{name: "trimmed mean tm90", stat: "tm90", want: true},
		{name: "interquartile mean", stat: "IQM", want: true},
		{name: "trimmed range TM percent", stat: "TM(10%:90%)", want: true},
		{name: "percentile rank PR open upper", stat: "PR(100:)", want: true},
		{name: "lowercase average not a statistic", stat: "average", want: false},
		{name: "garbage", stat: "not-a-stat", want: false},
		{name: "empty", stat: "", want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validStat(tt.stat))
		})
	}
}

func TestValidateActions(t *testing.T) {
	cases := []struct {
		name    string
		actions []string
		wantErr bool
	}{
		{name: "empty passes", actions: nil, wantErr: false},
		{name: "sns arn", actions: []string{"arn:aws:sns:us-east-1:123456789012:topic"},
			wantErr: false},
		{name: "ec2 automate stop",
			actions: []string{"arn:aws:automate:us-east-1:ec2:stop"}, wantErr: false},
		{name: "ec2 automate recover",
			actions: []string{"arn:aws:automate:us-east-1:ec2:recover"}, wantErr: false},
		{name: "swf action",
			actions: []string{
				"arn:aws:swf:us-east-1:123456789012:action/actions/AWS_EC2.InstanceId.Stop/1.0"},
			wantErr: false},
		{name: "iso partition arn",
			actions: []string{"arn:aws-iso:sns:us-iso-east-1:123456789012:topic"},
			wantErr: false},
		{name: "not an arn", actions: []string{"just-a-string"}, wantErr: true},
		{name: "missing arn prefix",
			actions: []string{"aws:sns:us-east-1:123456789012:topic"}, wantErr: true},
		{name: "one bad among good",
			actions: []string{"arn:aws:sns:us-east-1:123456789012:topic", "bad"},
			wantErr: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := validateActions("alarm-actions", tt.actions)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestExpandDimensions(t *testing.T) {
	t.Run("empty yields nil", func(t *testing.T) {
		assert.Nil(t, expandDimensions(nil))
		assert.Nil(t, expandDimensions(map[string]string{}))
	})
	t.Run("sorted by name", func(t *testing.T) {
		got := expandDimensions(map[string]string{"Zeta": "1", "Alpha": "2", "Mu": "3"})
		require.Len(t, got, 3)
		assert.Equal(t, "Alpha", aws.ToString(got[0].Name))
		assert.Equal(t, "Mu", aws.ToString(got[1].Name))
		assert.Equal(t, "Zeta", aws.ToString(got[2].Name))
		assert.Equal(t, "2", aws.ToString(got[0].Value))
	})
}

func TestExpandMetricQuery(t *testing.T) {
	t.Run("empty yields nil", func(t *testing.T) {
		assert.Nil(t, expandMetricQuery(nil))
	})
	t.Run("expression element has no metric stat", func(t *testing.T) {
		got := expandMetricQuery([]MetricAlarmMetricQuery{{
			Id:         "e1",
			Expression: aws.String("m1 + m2"),
			ReturnData: aws.Bool(true),
		}})
		require.Len(t, got, 1)
		assert.Equal(t, "e1", aws.ToString(got[0].Id))
		assert.Equal(t, "m1 + m2", aws.ToString(got[0].Expression))
		assert.True(t, aws.ToBool(got[0].ReturnData))
		assert.Nil(t, got[0].MetricStat)
	})
	t.Run("metric element expands stat with narrowed period and unit", func(t *testing.T) {
		got := expandMetricQuery([]MetricAlarmMetricQuery{{
			Id: "m1",
			Metric: &MetricAlarmMetricStat{
				MetricName: aws.String("CPUUtilization"),
				Namespace:  aws.String("AWS/EC2"),
				Dimensions: &map[string]string{"InstanceId": "i-abc"},
				Stat:       aws.String("Average"),
				Period:     aws.Int64(60),
				Unit:       aws.String("Percent"),
			},
			ReturnData: aws.Bool(false),
		}})
		require.Len(t, got, 1)
		stat := got[0].MetricStat
		require.NotNil(t, stat)
		assert.Equal(t, int32(60), aws.ToInt32(stat.Period))
		assert.Equal(t, "Average", aws.ToString(stat.Stat))
		assert.Equal(t, cloudwatchtypes.StandardUnitPercent, stat.Unit)
		require.NotNil(t, stat.Metric)
		assert.Equal(t, "CPUUtilization", aws.ToString(stat.Metric.MetricName))
		require.Len(t, stat.Metric.Dimensions, 1)
		assert.Equal(t, "InstanceId", aws.ToString(stat.Metric.Dimensions[0].Name))
	})
	t.Run("metric element without dimensions", func(t *testing.T) {
		got := expandMetricQuery([]MetricAlarmMetricQuery{{
			Id: "m1",
			Metric: &MetricAlarmMetricStat{
				MetricName: aws.String("CPUUtilization"),
				Namespace:  aws.String("AWS/EC2"),
				Stat:       aws.String("Average"),
				Period:     aws.Int64(60),
			},
		}})
		require.Len(t, got, 1)
		require.NotNil(t, got[0].MetricStat)
		assert.Nil(t, got[0].MetricStat.Metric.Dimensions)
	})
}

func TestExpandPutMetricAlarmInputThreshold(t *testing.T) {
	t.Run("static threshold sent when no metric id", func(t *testing.T) {
		r := &MetricAlarmResource{AlarmName: "a", Threshold: aws.Float64(80)}
		in := r.expandPutMetricAlarmInput()
		assert.Equal(t, float64(80), aws.ToFloat64(in.Threshold))
		assert.Nil(t, in.ThresholdMetricId)
	})
	t.Run("metric id sent instead of static threshold", func(t *testing.T) {
		r := &MetricAlarmResource{
			AlarmName:         "a",
			Threshold:         aws.Float64(80),
			ThresholdMetricId: aws.String("ad1"),
		}
		in := r.expandPutMetricAlarmInput()
		assert.Equal(t, "ad1", aws.ToString(in.ThresholdMetricId))
		assert.Nil(t, in.Threshold)
	})
}

func TestExpandPutMetricAlarmInputDefaults(t *testing.T) {
	t.Run("omitted defaults are always sent", func(t *testing.T) {
		r := &MetricAlarmResource{AlarmName: "a"}
		in := r.expandPutMetricAlarmInput()
		assert.True(t, aws.ToBool(in.ActionsEnabled))
		assert.Equal(t, "missing", aws.ToString(in.TreatMissingData))
	})
	t.Run("explicit values override defaults", func(t *testing.T) {
		r := &MetricAlarmResource{
			AlarmName:        "a",
			ActionsEnabled:   aws.Bool(false),
			TreatMissingData: aws.String("breaching"),
		}
		in := r.expandPutMetricAlarmInput()
		assert.False(t, aws.ToBool(in.ActionsEnabled))
		assert.Equal(t, "breaching", aws.ToString(in.TreatMissingData))
	})
}

func TestChangedExceptTags(t *testing.T) {
	base := MetricAlarmResource{
		AlarmName:          "a",
		ComparisonOperator: aws.String("GreaterThanThreshold"),
		EvaluationPeriods:  aws.Int64(1),
		MetricName:         aws.String("CPUUtilization"),
		Tags:               new(map[string]string{"env": "prod"}),
	}
	t.Run("tag-only change does not count", func(t *testing.T) {
		current := base
		current.Tags = new(map[string]string{"env": "dev", "team": "core"})
		assert.False(t, current.changedExceptTags(base))
	})
	t.Run("non-tag change counts", func(t *testing.T) {
		current := base
		current.Threshold = aws.Float64(90)
		assert.True(t, current.changedExceptTags(base))
	})
	t.Run("no change at all", func(t *testing.T) {
		current := base
		assert.False(t, current.changedExceptTags(base))
	})
}

func TestMetricAlarmTags(t *testing.T) {
	t.Run("empty yields nil", func(t *testing.T) {
		assert.Nil(t, metricAlarmTags(nil))
		assert.Nil(t, metricAlarmTags(map[string]string{}))
	})
	t.Run("sorted by key", func(t *testing.T) {
		got := metricAlarmTags(map[string]string{"z": "1", "a": "2"})
		require.Len(t, got, 2)
		assert.Equal(t, "a", aws.ToString(got[0].Key))
		assert.Equal(t, "z", aws.ToString(got[1].Key))
	})
}

func TestReplaceFields(t *testing.T) {
	r := &MetricAlarmResource{}
	assert.Equal(t, []string{"alarm-name"}, r.ReplaceFields())
}
