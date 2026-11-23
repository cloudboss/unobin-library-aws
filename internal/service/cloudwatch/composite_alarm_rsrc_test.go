package cloudwatch

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	compositeTestName  = "all alarms"
	compositeTestARN   = "arn:aws:cloudwatch:us-east-1:123456789012:alarm:all alarms"
	compositeTestRule  = "ALARM(\"one\")\nOR OK('two')"
	compositeActionOne = "arn:aws:sns:us-east-1:123456789012:a"
	compositeActionTwo = "arn:aws:sns:us-east-1:123456789012:b"
)

type compositeReply struct {
	values url.Values
	result string
	code   string
}

func compositeTestAPI(t *testing.T, region string, replies ...compositeReply) *awsCfg {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	requests := make(chan url.Values, len(replies)+8)
	queue := make(chan compositeReply, len(replies))
	for _, reply := range replies {
		queue <- reply
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		select {
		case requests <- r.PostForm:
		default:
			t.Error("unexpected additional AWS requests")
		}
		select {
		case reply := <-queue:
			w.Header().Set("Content-Type", "text/xml")
			if reply.code != "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w,
					"<ErrorResponse><Error><Code>%s</Code><Message>test error</Message>"+
						"</Error></ErrorResponse>", reply.code)
				return
			}
			action := r.PostForm.Get("Action")
			_, _ = fmt.Fprintf(w, "<%[1]sResponse><%[1]sResult>%[2]s</%[1]sResult></%[1]sResponse>",
				action, reply.result)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(func() {
		server.Close()
		close(requests)
		var actual []url.Values
		for request := range requests {
			request.Del("Version")
			actual = append(actual, request)
		}
		var expected []url.Values
		for _, reply := range replies {
			expected = append(expected, reply.values)
		}
		assert.Equal(t, expected, actual)
	})
	return &awsCfg{
		EndpointURL: aws.String(server.URL), Region: aws.String(region), MaxAttempts: aws.Int64(1),
	}
}

func compositePut(values url.Values) url.Values {
	values.Set("Action", "PutCompositeAlarm")
	values.Set("AlarmName", compositeTestName)
	if !values.Has("AlarmRule") {
		values.Set("AlarmRule", compositeTestRule)
	}
	if !values.Has("ActionsEnabled") {
		values.Set("ActionsEnabled", "true")
	}
	return values
}

func compositeDescribe(token string) url.Values {
	values := url.Values{
		"Action": {"DescribeAlarms"}, "AlarmNames.member.1": {compositeTestName},
		"AlarmTypes.member.1": {"CompositeAlarm"},
	}
	if token != "" {
		values.Set("NextToken", token)
	}
	return values
}

func compositeTagAction(action string) url.Values {
	return url.Values{"Action": {action}, "ResourceARN": {compositeTestARN}}
}

func compositeXML(fields string) string {
	var rule strings.Builder
	_ = xml.EscapeText(&rule, []byte(compositeTestRule))
	return "<CompositeAlarms><member><AlarmArn>" + compositeTestARN +
		"</AlarmArn><AlarmName>" + compositeTestName +
		"</AlarmName><AlarmRule>" + rule.String() + "</AlarmRule>" +
		fields + "</member></CompositeAlarms>"
}

func compositeInput() CompositeAlarmResource {
	return CompositeAlarmResource{AlarmName: compositeTestName, AlarmRule: compositeTestRule}
}

func TestCompositeAlarmActionQuery(t *testing.T) {
	cases := []struct {
		name  string
		input *[]string
		want  []string
	}{
		{"omitted", nil, nil},
		{"empty", &[]string{}, nil},
		{"blank", &[]string{""}, []string{}},
		{"blank and duplicates", &[]string{"", compositeActionTwo, compositeActionOne,
			compositeActionTwo}, []string{compositeActionOne, compositeActionTwo}},
		{"reordered", &[]string{compositeActionTwo, compositeActionOne},
			[]string{compositeActionOne, compositeActionTwo}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := compositeInput()
			input.AlarmActions = tc.input
			input.OKActions = tc.input
			input.InsufficientDataActions = tc.input
			input.AlarmDescription = aws.String("")
			want := compositePut(url.Values{})
			for _, field := range []string{"AlarmActions", "OKActions", "InsufficientDataActions"} {
				if tc.want != nil && len(tc.want) == 0 {
					want.Set(field, "")
				}
				for i, action := range tc.want {
					want.Set(fmt.Sprintf("%s.member.%d", field, i+1), action)
				}
			}
			cfg := compositeTestAPI(t, "us-east-1",
				compositeReply{values: want}, compositeReply{values: compositeDescribe("")})
			_, err := input.Create(t.Context(), cfg)
			require.ErrorContains(t, err, "read created composite alarm")
		})
	}
}

func TestCompositeAlarmSuppressorRequest(t *testing.T) {
	for _, suppressor := range []string{
		"other alarm", "arn:aws:cloudwatch:us-east-1:123456789012:alarm:other alarm", "",
	} {
		t.Run(suppressor, func(t *testing.T) {
			input := compositeInput()
			input.ActionsSuppressor = &CompositeAlarmSuppressor{
				Alarm: suppressor, ExtensionPeriod: 0, WaitPeriod: 30,
			}
			input.AlarmDescription = aws.String("verbatim alarm description")
			assert.Equal(t, &cloudwatch.PutCompositeAlarmInput{
				AlarmName: aws.String(compositeTestName), AlarmRule: aws.String(compositeTestRule),
				ActionsEnabled:                   aws.Bool(true),
				AlarmDescription:                 input.AlarmDescription,
				ActionsSuppressor:                nonemptyTestString(suppressor),
				ActionsSuppressorExtensionPeriod: aws.Int32(0),
				ActionsSuppressorWaitPeriod:      aws.Int32(30),
			}, input.putInput())
		})
	}
}

func nonemptyTestString(value string) *string {
	if value == "" {
		return nil
	}
	return aws.String(value)
}

func TestCompositeAlarmReadPaginationAndOutput(t *testing.T) {
	cfg := compositeTestAPI(t, "us-east-1",
		compositeReply{values: compositeDescribe(""), result: "<NextToken>next</NextToken>" +
			"<MetricAlarms><member><AlarmName>ignored</AlarmName></member></MetricAlarms>"},
		compositeReply{values: compositeDescribe("next"), result: compositeXML(
			"<ActionsEnabled>true</ActionsEnabled><ActionsSuppressor>alarm</ActionsSuppressor>" +
				"<ActionsSuppressorWaitPeriod>0</ActionsSuppressorWaitPeriod>" +
				"<AlarmActions><member>" + compositeActionTwo + "</member><member>" +
				compositeActionOne + "</member><member>" + compositeActionTwo + "</member></AlarmActions>" +
				"<OKActions><member>" + compositeActionTwo + "</member></OKActions>")},
		compositeReply{values: compositeTagAction("ListTagsForResource"),
			result: "<Tags><member><Key>env</Key><Value></Value></member>" +
				"<member><Key>aws:system</Key><Value>filtered</Value></member></Tags>"},
	)
	input := CompositeAlarmResource{AlarmName: "wrong name"}
	out, err := input.Read(t.Context(), nil, compositeAlarmPrior{
		Inputs: compositeInput(), Configuration: cfg,
	})
	require.NoError(t, err)
	assert.Equal(t, &CompositeAlarmResourceOutput{
		Arn: compositeTestARN, AlarmName: compositeTestName, AlarmRule: compositeTestRule,
		ActionsEnabled: true, ActionsSuppressor: &CompositeAlarmSuppressor{Alarm: "alarm"},
		AlarmActions: []string{compositeActionOne, compositeActionTwo},
		OKActions:    []string{compositeActionTwo}, InsufficientDataActions: []string{},
		Tags: map[string]string{"env": ""},
	}, out)
}

func TestCompositeAlarmReadFailures(t *testing.T) {
	cases := []struct {
		name, first, next, code, message string
	}{
		{"metric only", "<MetricAlarms><member/></MetricAlarms>", "", "", ""},
		{"ambiguous", compositeXML("") + "<NextToken>next</NextToken>",
			compositeXML(""), "", "multiple composite alarms"},
		{"later error", compositeXML("") + "<NextToken>next</NextToken>",
			"", "AccessDenied", "AccessDenied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			replies := []compositeReply{{values: compositeDescribe(""), result: tc.first}}
			if tc.next != "" || tc.code != "" {
				replies = append(replies, compositeReply{
					values: compositeDescribe("next"), result: tc.next, code: tc.code,
				})
			}
			cfg := compositeTestAPI(t, "us-east-1", replies...)
			input := compositeInput()
			_, err := input.Read(t.Context(), cfg, compositeAlarmPrior{Inputs: input})
			if tc.message == "" {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.ErrorContains(t, err, tc.message)
				require.NotErrorIs(t, err, runtime.ErrNotFound)
			}
		})
	}
}

func TestCompositeAlarmTagFallback(t *testing.T) {
	cases := []struct {
		name, region, tagError, describeError, listError string
		retries, describeAbsent                          bool
	}{
		{name: "standard error", region: "us-east-1"},
		{name: "fallback", region: "us-gov-west-1", retries: true},
		{
			name: "explicit tag error", region: "us-gov-west-1", retries: true,
			tagError: "UnsupportedOperation",
		},
		{
			name: "final describe error", region: "us-gov-west-1", retries: true,
			describeError: "AccessDenied",
		},
		{
			name: "final describe empty", region: "us-gov-west-1", retries: true,
			describeAbsent: true,
		},
		{
			name: "listing unsupported", region: "us-gov-west-1", retries: true,
			listError: "UnsupportedOperation",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tagged := compositePut(url.Values{"Tags.member.1.Key": {"env"},
				"Tags.member.1.Value": {""}})
			replies := []compositeReply{{values: tagged, code: "UnsupportedOperation"}}
			if tc.retries {
				tag := compositeTagAction("TagResource")
				tag.Set("Tags.member.1.Key", "env")
				tag.Set("Tags.member.1.Value", "")
				replies = append(replies,
					compositeReply{values: compositePut(url.Values{})},
					compositeReply{values: compositeDescribe(""), result: compositeXML("")},
					compositeReply{values: tag, code: tc.tagError},
				)
				if tc.tagError == "" {
					result := compositeXML(
						"<AlarmDescription>observed after tagging</AlarmDescription>")
					if tc.describeAbsent {
						result = "<CompositeAlarms/>"
					}
					replies = append(replies, compositeReply{
						values: compositeDescribe(""), code: tc.describeError,
						result: result,
					})
				}
				if tc.tagError == "" && tc.describeError == "" && !tc.describeAbsent {
					replies = append(replies, compositeReply{
						values: compositeTagAction("ListTagsForResource"), code: tc.listError,
						result: "<Tags><member><Key>env</Key><Value/></member></Tags>",
					})
				}
			}
			cfg := compositeTestAPI(t, tc.region, replies...)
			input := compositeInput()
			input.Tags = &map[string]string{"env": "", "aws:system": "ignored"}
			out, err := input.Create(t.Context(), cfg)
			if !tc.retries || tc.tagError != "" {
				require.ErrorContains(t, err, "UnsupportedOperation")
				return
			}
			if tc.describeError != "" {
				require.ErrorContains(t, err, "read created composite alarm")
				require.ErrorContains(t, err, tc.describeError)
				return
			}
			if tc.describeAbsent {
				require.ErrorContains(t, err, "read created composite alarm")
				require.ErrorContains(t, err, "was absent after write")
				assert.NotErrorIs(t, err, runtime.ErrNotFound)
				assert.Nil(t, out)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "observed after tagging", out.AlarmDescription)
			if tc.listError == "" {
				assert.Equal(t, map[string]string{"env": ""}, out.Tags)
			} else {
				assert.Equal(t, map[string]string{}, out.Tags)
			}
		})
	}
}

func TestCompositeAlarmDeleteErrors(t *testing.T) {
	for _, code := range []string{
		"", "ResourceNotFoundException", "ResourceNotFound", "ValidationError", "AccessDenied",
	} {
		t.Run(code, func(t *testing.T) {
			cfg := compositeTestAPI(t, "us-east-1", compositeReply{
				values: url.Values{"Action": {"DeleteAlarms"},
					"AlarmNames.member.1": {compositeTestName}}, code: code,
			})
			input := CompositeAlarmResource{AlarmName: "different"}
			err := input.Delete(t.Context(), nil, compositeAlarmPrior{
				Inputs: compositeInput(), Configuration: cfg,
			})
			if code == "" || code == "ResourceNotFound" {
				require.NoError(t, err)
				return
			}
			var apiErr smithy.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, code, apiErr.ErrorCode())
		})
	}
	assert.True(t, isNotFound(&cloudwatchtypes.ResourceNotFoundException{}))
}

func TestCompositeAlarmRuleRejection(t *testing.T) {
	cfg := compositeTestAPI(t, "us-east-1", compositeReply{
		values: compositePut(url.Values{}), code: "ValidationError",
	})
	input := compositeInput()
	_, err := input.Create(t.Context(), cfg)
	require.ErrorContains(t, err, "put composite alarm")
	var apiErr smithy.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "ValidationError", apiErr.ErrorCode())
}

func TestCompositeAlarmClearsOmittedSettings(t *testing.T) {
	cfg := compositeTestAPI(t, "us-east-1",
		compositeReply{values: compositePut(url.Values{})},
		compositeReply{values: compositeDescribe(""), result: compositeXML("")},
		compositeReply{values: compositeTagAction("ListTagsForResource")},
	)
	prior := compositeInput()
	actions := []string{compositeActionOne}
	prior.AlarmActions, prior.OKActions, prior.InsufficientDataActions = &actions, &actions, &actions
	prior.AlarmDescription = aws.String("previous description")
	prior.ActionsSuppressor = &CompositeAlarmSuppressor{Alarm: "mute"}
	input := compositeInput()
	out, err := input.Update(t.Context(), cfg, compositeAlarmPrior{Inputs: prior})
	require.NoError(t, err)
	assert.Equal(t, "", out.AlarmDescription)
	assert.Nil(t, out.ActionsSuppressor)
	assert.Equal(t, []string{}, out.AlarmActions)
	assert.Equal(t, []string{}, out.OKActions)
	assert.Equal(t, []string{}, out.InsufficientDataActions)
}

func TestCompositeAlarmUpdateMissingReadback(t *testing.T) {
	cfg := compositeTestAPI(t, "us-east-1",
		compositeReply{values: compositePut(url.Values{"AlarmRule": {"FALSE"}})},
		compositeReply{values: compositeDescribe(""), result: "<CompositeAlarms/>"},
	)
	prior := compositeInput()
	input := prior
	input.AlarmRule = "FALSE"
	out, err := input.Update(t.Context(), cfg, compositeAlarmPrior{Inputs: prior})
	require.ErrorContains(t, err, "read updated composite alarm")
	require.ErrorContains(t, err, "was absent after write")
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
	assert.Nil(t, out)
}
