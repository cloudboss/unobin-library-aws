package ec2

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func flowLogTestClient(t *testing.T, f *fakeEC2) *flowLogClient {
	t.Helper()
	client, err := newFlowLogClient(t.Context(), f.configuration())
	require.NoError(t, err)
	client.config.Credentials = aws.CredentialsProviderFunc(
		func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccountID: "123456789012"}, nil
		},
	)
	client.clock = &flowLogTestClock{}
	return client
}

func flowLogPage(items, token string) string {
	return "<DescribeFlowLogsResponse><flowLogSet>" + items + "</flowLogSet>" +
		"<nextToken>" + token + "</nextToken></DescribeFlowLogsResponse>"
}

func flowLogItem(id, extra string) string {
	return "<item><flowLogId>" + id + "</flowLogId>" + extra + "</item>"
}

func TestFlowLogLookupCardinalityAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, first, second, code string
		missing                   bool
	}{
		{name: "one", first: flowLogPage(flowLogItem("fl-1", ""), "")},
		{name: "next page", first: flowLogPage("", "next"),
			second: flowLogPage(flowLogItem("fl-1", ""), "")},
		{name: "zero", first: flowLogPage("", ""), missing: true},
		{name: "mismatch", first: flowLogPage(flowLogItem("fl-other", ""), ""), missing: true},
		{name: "multiple pages", first: flowLogPage(flowLogItem("fl-1", ""), "next"),
			second: flowLogPage(flowLogItem("fl-1", ""), ""), missing: true},
		{name: "multiple", first: flowLogPage(flowLogItem("fl-1", "")+
			flowLogItem("fl-2", ""), ""), missing: true},
		{name: "not found API", code: "InvalidFlowLogId.NotFound"},
		{name: "denied", code: "UnauthorizedOperation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DescribeFlowLogs", func(n int, form url.Values) (int, string) {
				if tc.code != "" {
					return 400, ec2ErrorXML(tc.code, "denied")
				}
				if n == 1 {
					return 200, tc.first
				}
				return 200, tc.second
			})
			client := flowLogTestClient(t, f)
			got, err := client.lookup(t.Context(), "fl-1")
			switch {
			case tc.code != "":
				require.ErrorContains(t, err, tc.code)
				assert.NotErrorIs(t, err, runtime.ErrNotFound)
			case tc.missing:
				assert.ErrorIs(t, err, runtime.ErrNotFound)
			default:
				require.NoError(t, err)
				assert.Equal(t, &ec2types.FlowLog{FlowLogId: aws.String("fl-1")}, got)
			}
			expected := []url.Values{{"Action": {"DescribeFlowLogs"}, "Version": {"2016-11-15"},
				"FlowLogId.1": {"fl-1"}}}
			if tc.second != "" {
				expected = append(expected, url.Values{"Action": {"DescribeFlowLogs"},
					"Version": {"2016-11-15"}, "FlowLogId.1": {"fl-1"}, "NextToken": {"next"}})
			}
			assert.Equal(t, expected, f.sent("DescribeFlowLogs"))
		})
	}
}

func TestFlowLogCreateRetriesAndReadback(t *testing.T) {
	for _, tc := range []struct {
		name, code, message, successful string
		attempts                        int
		timeout                         time.Duration
		cancel                          bool
	}{
		{name: "IAM propagation", code: "InvalidParameter", message: "Unable to assume given IAM role",
			attempts: 3, successful: "fl-1"},
		{name: "other code", code: "InvalidParameterValue", message: "Unable to assume given IAM role",
			attempts: 1},
		{name: "other message", code: "InvalidParameter", message: "bad value", attempts: 1},
		{name: "timeout", code: "InvalidParameter", message: "Unable to assume given IAM role",
			attempts: 2, timeout: -time.Second},
		{name: "cancellation", code: "InvalidParameter", message: "Unable to assume given IAM role",
			attempts: 1, cancel: true},
		{name: "missing ID", attempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.on("CreateFlowLogs", func(n int, form url.Values) (int, string) {
				if tc.code != "" && (tc.successful == "" || n < tc.attempts) {
					if tc.cancel {
						cancel()
					}
					return 400, ec2ErrorXML(tc.code, tc.message)
				}
				return 200, "<CreateFlowLogsResponse><flowLogIdSet><item>" + tc.successful +
					"</item><item>fl-ignored</item></flowLogIdSet></CreateFlowLogsResponse>"
			})
			f.on("DescribeFlowLogs", func(_ int, form url.Values) (int, string) {
				return 200, flowLogPage(flowLogItem("fl-1", ""), "")
			})
			client := flowLogTestClient(t, f)
			if tc.timeout != 0 {
				client.retryTimeout = tc.timeout
			}
			request, err := (&FlowLogResource{VpcId: aws.String("vpc-1")}).prepareRequest("retained")
			require.NoError(t, err)
			out, err := client.create(ctx, request)
			if tc.successful == "" {
				require.Error(t, err)
				if tc.cancel {
					assert.ErrorIs(t, err, context.Canceled)
				}
				assert.Empty(t, f.sent("DescribeFlowLogs"))
			} else {
				require.NoError(t, err)
				assert.Equal(t, "fl-1", out.FlowLogId)
			}
			assert.Empty(t, f.sent("DeleteFlowLogs"))
			expected := make([]url.Values, tc.attempts)
			for i := range expected {
				expected[i] = url.Values{"Action": {"CreateFlowLogs"}, "Version": {"2016-11-15"},
					"ClientToken": {"retained"}, "ResourceId.1": {"vpc-1"}, "ResourceType": {"VPC"},
					"LogDestinationType": {"cloud-watch-logs"}, "MaxAggregationInterval": {"600"}}
			}
			assert.Equal(t, expected, f.sent("CreateFlowLogs"))
		})
	}
}

func TestFlowLogCreateDisappearanceIsAnError(t *testing.T) {
	f := newFakeEC2(t)
	f.on("CreateFlowLogs", func(int, url.Values) (int, string) {
		return 200, "<CreateFlowLogsResponse><flowLogIdSet><item>fl-1</item>" +
			"</flowLogIdSet></CreateFlowLogsResponse>"
	})
	f.on("DescribeFlowLogs", func(int, url.Values) (int, string) {
		return 200, flowLogPage("", "")
	})
	f.on("DeleteFlowLogs", func(int, url.Values) (int, string) {
		return 200, "<DeleteFlowLogsResponse/>"
	})
	client := flowLogTestClient(t, f)
	request, err := (&FlowLogResource{VpcId: aws.String("vpc-1")}).prepareRequest("token")
	require.NoError(t, err)
	_, err = client.create(t.Context(), request)
	require.ErrorContains(t, err, "after create")
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
}

func TestFlowLogCreateReadbackCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, describeCode, items, account, deleteCode string
		sts, cancel, deleteItem, cleanupFailure        bool
	}{
		{name: "describe failure", describeCode: "UnauthorizedOperation"},
		{name: "describe not found", describeCode: "InvalidFlowLogId.NotFound"},
		{name: "empty result"},
		{name: "mismatched result", items: flowLogItem("fl-other", "")},
		{name: "multiple results", items: flowLogItem("fl-1", "") + flowLogItem("fl-2", "")},
		{name: "STS failure", items: flowLogItem("fl-1", ""), sts: true},
		{name: "empty account", items: flowLogItem("fl-1", ""), account: "empty"},
		{name: "canceled caller", cancel: true},
		{name: "top-level not found", describeCode: "UnauthorizedOperation",
			deleteCode: "InvalidFlowLogId.NotFound"},
		{name: "item not found", describeCode: "UnauthorizedOperation",
			deleteCode: "InvalidFlowLogId.NotFound", deleteItem: true},
		{name: "cleanup API failure", describeCode: "UnauthorizedOperation",
			deleteCode: "CleanupDenied", cleanupFailure: true},
		{name: "cleanup item failure", describeCode: "UnauthorizedOperation",
			deleteCode: "CleanupDenied", deleteItem: true, cleanupFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEC2(t)
			exists := false
			f.on("CreateFlowLogs", func(int, url.Values) (int, string) {
				exists = true
				return 200, "<CreateFlowLogsResponse><flowLogIdSet><item>fl-1</item>" +
					"<item>fl-ignored</item></flowLogIdSet></CreateFlowLogsResponse>"
			})
			f.on("DescribeFlowLogs", func(int, url.Values) (int, string) {
				if tc.describeCode != "" {
					return 400, ec2ErrorXML(tc.describeCode, "readback denied")
				}
				return 200, flowLogPage(tc.items, "")
			})
			f.on("GetCallerIdentity", func(int, url.Values) (int, string) {
				if tc.sts {
					return 400, "<ErrorResponse><Error><Code>IdentityDenied</Code>" +
						"<Message>identity denied</Message></Error></ErrorResponse>"
				}
				return 200, "<GetCallerIdentityResponse><GetCallerIdentityResult>" +
					"</GetCallerIdentityResult></GetCallerIdentityResponse>"
			})
			f.on("DeleteFlowLogs", func(int, url.Values) (int, string) {
				if !tc.cleanupFailure {
					exists = false
				}
				if tc.deleteItem {
					return 200, "<DeleteFlowLogsResponse><unsuccessful><item><resourceId>fl-1" +
						"</resourceId><error><code>" + tc.deleteCode + "</code></error></item>" +
						"</unsuccessful></DeleteFlowLogsResponse>"
				}
				if tc.deleteCode != "" {
					return 400, ec2ErrorXML(tc.deleteCode, "cleanup denied")
				}
				return 200, "<DeleteFlowLogsResponse/>"
			})
			client := flowLogTestClient(t, f)
			client.config.Region = "us-west-2"
			client.stsEndpoint = f.server.URL
			if tc.sts || tc.account != "" {
				client.config.Credentials = client.ec2.Options().Credentials
			}
			type contextKey struct{}
			ctx, cancel := context.WithTimeout(
				context.WithValue(t.Context(), contextKey{}, "retained"), time.Minute)
			defer cancel()
			var cleanupCtx context.Context
			options := client.ec2.Options()
			options.Region = client.config.Region
			client.ec2 = ec2.New(options, func(o *ec2.Options) {
				o.APIOptions = append(o.APIOptions, func(stack *middleware.Stack) error {
					return stack.Initialize.Add(middleware.InitializeMiddlewareFunc("inspect-cleanup",
						func(ctx context.Context, in middleware.InitializeInput,
							next middleware.InitializeHandler,
						) (middleware.InitializeOutput, middleware.Metadata, error) {
							if stack.ID() == "DescribeFlowLogs" && tc.cancel {
								cancel()
								return middleware.InitializeOutput{}, middleware.Metadata{}, ctx.Err()
							}
							if stack.ID() == "DeleteFlowLogs" {
								cleanupCtx = ctx
								assert.NoError(t, ctx.Err())
								assert.Equal(t, "retained", ctx.Value(contextKey{}))
								deadline, ok := ctx.Deadline()
								assert.True(t, ok)
								assert.InDelta(t, 120, time.Until(deadline).Seconds(), 2)
							}
							return next.HandleInitialize(ctx, in)
						}), middleware.Before)
				})
			})
			out, err := client.create(ctx, &ec2.CreateFlowLogsInput{
				ResourceIds: []string{"vpc-1"}, ResourceType: "VPC"})
			require.ErrorContains(t, err, "after create")
			assert.Nil(t, out)
			assert.Equal(t, tc.cleanupFailure, exists)
			assert.Equal(t, []url.Values{{"Action": {"DeleteFlowLogs"}, "Version": {"2016-11-15"},
				"FlowLogId.1": {"fl-1"}}}, f.sent("DeleteFlowLogs"))
			if assert.NotNil(t, cleanupCtx) {
				assert.ErrorIs(t, cleanupCtx.Err(), context.Canceled)
			}
			if tc.cancel {
				assert.ErrorIs(t, err, context.Canceled)
			}
			if tc.describeCode != "" || tc.sts {
				var api smithy.APIError
				require.ErrorAs(t, err, &api)
				code := tc.describeCode
				if tc.sts {
					code = "IdentityDenied"
				}
				assert.Equal(t, code, api.ErrorCode())
			}
			if tc.cleanupFailure {
				assert.ErrorContains(t, err, "clean up flow log fl-1 in region us-west-2")
				assert.ErrorContains(t, err, "CleanupDenied")
			}
		})
	}
}

func TestFlowLogUnsuccessfulItems(t *testing.T) {
	items := []ec2types.UnsuccessfulItem{
		{ResourceId: aws.String("no-error")},
		{ResourceId: aws.String("vpc-1"), Error: &ec2types.UnsuccessfulItemError{
			Code: aws.String("BadOne"), Message: aws.String("first")}},
		{ResourceId: aws.String("vpc-2"), Error: &ec2types.UnsuccessfulItemError{
			Code: aws.String("BadTwo"), Message: aws.String("second")}},
	}
	err := flowLogUnsuccessful(items)
	require.ErrorContains(t, err, "resource vpc-1: api error BadOne: first")
	require.ErrorContains(t, err, "resource vpc-2: api error BadTwo: second")
	var api smithy.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, "BadOne", api.ErrorCode())
	assert.NoError(t, flowLogUnsuccessful(nil))
	f := newFakeEC2(t)
	f.on("CreateFlowLogs", func(int, url.Values) (int, string) {
		return 200, `<CreateFlowLogsResponse><flowLogIdSet><item>fl-unaccepted</item></flowLogIdSet>` +
			`<unsuccessful><item><resourceId>vpc-1</resourceId>` +
			`<error><code>InvalidParameter</code><message>Unable to assume given IAM role</message>` +
			`</error></item></unsuccessful></CreateFlowLogsResponse>`
	})
	client := flowLogTestClient(t, f)
	_, err = client.create(t.Context(), &ec2.CreateFlowLogsInput{
		ResourceIds: []string{"vpc-1"}, ResourceType: "VPC"})
	require.ErrorContains(t, err, "vpc-1")
	assert.Empty(t, f.sent("DeleteFlowLogs"))
	assert.Equal(t, []url.Values{{"Action": {"CreateFlowLogs"}, "Version": {"2016-11-15"},
		"ResourceId.1": {"vpc-1"}, "ResourceType": {"VPC"}}}, f.sent("CreateFlowLogs"))
}

func TestFlowLogReadFullOutputs(t *testing.T) {
	f := newFakeEC2(t)
	f.on("DescribeFlowLogs", func(int, url.Values) (int, string) {
		return 200, flowLogPage(flowLogItem("fl-1",
			`<deliverCrossAccountRole>cross</deliverCrossAccountRole>`+
				`<deliverLogsPermissionArn>role</deliverLogsPermissionArn>`+
				`<logDestination>remote</logDestination>`+
				`<logDestinationType>s3</logDestinationType><logFormat>remote-format</logFormat>`+
				`<maxAggregationInterval>60</maxAggregationInterval><resourceId>vpc-1</resourceId>`+
				`<trafficType>ALL</trafficType><destinationOptions><fileFormat>parquet</fileFormat>`+
				`<hiveCompatiblePartitions>false</hiveCompatiblePartitions>`+
				`<perHourPartition>false</perHourPartition>`+
				`</destinationOptions><tagSet><item><key>stage</key><value>observed</value></item>`+
				`<item><key>blank</key><value></value></item><item><key>aws:reserved</key>`+
				`<value>keep</value></item></tagSet>`), "")
	})
	client := flowLogTestClient(t, f)
	client.config.Region = "cn-north-1"
	output, err := client.read(t.Context(), "fl-1")
	require.NoError(t, err)
	assert.Equal(t, &FlowLogResourceOutput{
		FlowLogId: "fl-1", Arn: "arn:aws-cn:ec2:cn-north-1:123456789012:vpc-flow-log/fl-1",
		Region: "cn-north-1", DeliverCrossAccountRole: aws.String("cross"),
		DeliverLogsPermissionArn: aws.String("role"), LogDestination: aws.String("remote"),
		LogDestinationType: "s3", LogFormat: aws.String("remote-format"),
		MaxAggregationInterval: aws.Int64(60), ResourceId: aws.String("vpc-1"),
		TrafficType: aws.String("ALL"), Tags: map[string]string{"stage": "observed", "blank": ""},
		DestinationOptions: &FlowLogDestinationOptions{FileFormat: aws.String("parquet"),
			HiveCompatiblePartitions: aws.Bool(false), PerHourPartition: aws.Bool(false)},
	}, output)
}

func TestFlowLogReadLegacyAndAbsentOutputs(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DescribeFlowLogs", func(int, url.Values) (int, string) {
				fields := "<resourceId>tgw-1</resourceId><trafficType>ALL</trafficType>"
				if legacy {
					fields += "<logDestinationType>cloud-watch-logs</logDestinationType>" +
						"<logGroupName>legacy</logGroupName>"
				}
				return 200, flowLogPage(flowLogItem("fl-1", fields), "")
			})
			client := flowLogTestClient(t, f)
			output, err := client.read(t.Context(), "fl-1")
			require.NoError(t, err)
			expected := &FlowLogResourceOutput{FlowLogId: "fl-1", Region: "us-east-1",
				Arn:        "arn:aws:ec2:us-east-1:123456789012:vpc-flow-log/fl-1",
				ResourceId: aws.String("tgw-1"), Tags: map[string]string{}}
			if legacy {
				expected.LogDestinationType = "cloud-watch-logs"
				expected.LogDestination = aws.String("arn:aws:logs:us-east-1:123456789012:log-group:legacy")
			}
			assert.Equal(t, expected, output)
		})
	}
}

func TestFlowLogUpdateObservedTagsAndDelete(t *testing.T) {
	f := newFakeEC2(t)
	f.on("DeleteTags", func(int, url.Values) (int, string) { return 200, "<DeleteTagsResponse/>" })
	f.on("CreateTags", func(int, url.Values) (int, string) { return 200, "<CreateTagsResponse/>" })
	f.on("GetCallerIdentity", func(int, url.Values) (int, string) {
		return 200, `<GetCallerIdentityResponse><GetCallerIdentityResult>` +
			`<Account>123456789012</Account>` +
			`</GetCallerIdentityResult></GetCallerIdentityResponse>`
	})
	f.on("DescribeFlowLogs", func(int, url.Values) (int, string) {
		return 200, flowLogPage(flowLogItem("fl-1", ""), "")
	})
	f.on("DeleteFlowLogs", func(int, url.Values) (int, string) {
		return 200, "<DeleteFlowLogsResponse/>"
	})
	cfg := f.configuration()
	r := &FlowLogResource{VpcId: aws.String("vpc-1"),
		Tags: &map[string]string{"change": "", "aws:reserved": "ignored"}}
	prior := flowLogPrior{Outputs: &FlowLogResourceOutput{FlowLogId: "fl-1", Region: "us-west-2",
		Tags: map[string]string{"stale": "old"}},
		Observed: &FlowLogResourceOutput{Tags: map[string]string{
			"change": "old", "drop": "yes", "aws:reserved": "keep"}}}
	out, err := r.Update(t.Context(), cfg, prior)
	require.NoError(t, err)
	assert.Equal(t, "us-west-2", out.Region)
	var mutations []url.Values
	for _, request := range f.requests {
		if request.Get("Action") == "CreateTags" || request.Get("Action") == "DeleteTags" {
			mutations = append(mutations, request)
		}
	}
	assert.Equal(t, []url.Values{
		{"Action": {"DeleteTags"}, "Version": {"2016-11-15"},
			"ResourceId.1": {"fl-1"}, "Tag.1.Key": {"drop"}},
		{"Action": {"CreateTags"}, "Version": {"2016-11-15"},
			"ResourceId.1": {"fl-1"}, "Tag.1.Key": {"change"}, "Tag.1.Value": {""}},
	}, mutations)
	require.NoError(t, r.Delete(t.Context(), cfg, prior))
	assert.Equal(t, []url.Values{{"Action": {"DeleteFlowLogs"}, "Version": {"2016-11-15"},
		"FlowLogId.1": {"fl-1"}}}, f.sent("DeleteFlowLogs"))
}

func TestFlowLogDeleteFailuresAndMissingIdentity(t *testing.T) {
	for _, tc := range []struct {
		code string
		item bool
		ok   bool
	}{
		{code: "InvalidFlowLogId.NotFound", ok: true},
		{code: "UnauthorizedOperation"},
		{code: "InvalidFlowLogId.NotFound", item: true, ok: true},
		{code: "Denied", item: true},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DeleteFlowLogs", func(int, url.Values) (int, string) {
				if !tc.item {
					return 400, ec2ErrorXML(tc.code, "message")
				}
				return 200, `<DeleteFlowLogsResponse><unsuccessful><item><resourceId>fl-1</resourceId>` +
					`<error><code>` + tc.code + `</code><message>message</message></error></item>` +
					`</unsuccessful></DeleteFlowLogsResponse>`
			})
			cfg := f.configuration()
			r := &FlowLogResource{}
			require.NoError(t, r.Delete(t.Context(), cfg, flowLogPrior{}))
			_, err := r.Read(t.Context(), cfg, flowLogPrior{})
			require.ErrorIs(t, err, runtime.ErrNotFound)
			_, err = r.Update(t.Context(), cfg, flowLogPrior{})
			require.ErrorContains(t, err, "saved flow log ID")
			err = r.Delete(t.Context(), cfg, flowLogPrior{
				Outputs: &FlowLogResourceOutput{FlowLogId: "fl-1"}})
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.code)
				var api smithy.APIError
				assert.True(t, errors.As(err, &api))
			}
		})
	}
}

func TestFlowLogReadEmptyNonTransitTraffic(t *testing.T) {
	f := newFakeEC2(t)
	f.on("DescribeFlowLogs", func(int, url.Values) (int, string) {
		return 200, flowLogPage(flowLogItem("fl-1", "<resourceId>vpc-1</resourceId>"), "")
	})
	client := flowLogTestClient(t, f)
	output, err := client.read(t.Context(), "fl-1")
	require.NoError(t, err)
	assert.Equal(t, aws.String(""), output.TrafficType)
}

func TestFlowLogCreateCleanupPreservesBothCauses(t *testing.T) {
	f := newFakeEC2(t)
	f.on("CreateFlowLogs", func(int, url.Values) (int, string) {
		return 200, "<CreateFlowLogsResponse><flowLogIdSet><item>fl-1</item>" +
			"</flowLogIdSet></CreateFlowLogsResponse>"
	})
	client := flowLogTestClient(t, f)
	readFailure := errors.New("read failed")
	cleanupFailure := &smithy.GenericAPIError{Code: "CleanupDenied", Message: "cleanup failed"}
	var calls []string
	client.ec2 = ec2.New(client.ec2.Options(), func(o *ec2.Options) {
		o.APIOptions = append(o.APIOptions, func(stack *middleware.Stack) error {
			return stack.Initialize.Add(middleware.InitializeMiddlewareFunc("inject-failure",
				func(ctx context.Context, in middleware.InitializeInput,
					next middleware.InitializeHandler,
				) (middleware.InitializeOutput, middleware.Metadata, error) {
					calls = append(calls, stack.ID())
					switch stack.ID() {
					case "DescribeFlowLogs":
						return middleware.InitializeOutput{}, middleware.Metadata{}, readFailure
					case "DeleteFlowLogs":
						return middleware.InitializeOutput{}, middleware.Metadata{}, cleanupFailure
					default:
						return next.HandleInitialize(ctx, in)
					}
				}), middleware.Before)
		})
	})
	out, err := client.create(t.Context(), &ec2.CreateFlowLogsInput{
		ResourceIds: []string{"vpc-1"}, ResourceType: "VPC"})
	assert.Nil(t, out)
	assert.ErrorIs(t, err, readFailure)
	assert.ErrorIs(t, err, cleanupFailure)
	var cleanupAPI *smithy.GenericAPIError
	require.ErrorAs(t, err, &cleanupAPI)
	assert.Same(t, cleanupFailure, cleanupAPI)
	assert.ErrorContains(t, err, "clean up flow log fl-1 in region us-east-1")
	assert.Equal(t, []string{"CreateFlowLogs", "DescribeFlowLogs", "DeleteFlowLogs"}, calls)
}

func TestFlowLogRefreshAndUpdateFailuresDoNotDelete(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(fmt.Sprint(update), func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DescribeFlowLogs", func(int, url.Values) (int, string) {
				return 400, ec2ErrorXML("ReadDenied", "denied")
			})
			cfg := f.configuration()
			r := &FlowLogResource{VpcId: aws.String("vpc-1")}
			prior := flowLogPrior{Outputs: &FlowLogResourceOutput{
				FlowLogId: "fl-1", Region: "us-east-1"}}
			var err error
			if update {
				_, err = r.Update(t.Context(), cfg, prior)
			} else {
				_, err = r.Read(t.Context(), cfg, prior)
			}
			require.ErrorContains(t, err, "ReadDenied")
			assert.Empty(t, f.sent("DeleteFlowLogs"))
		})
	}
}
