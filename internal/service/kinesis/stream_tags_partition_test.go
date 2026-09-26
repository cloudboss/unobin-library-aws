package kinesis

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awstypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type streamTagAPIError struct {
	code    string
	message string
}

func (e *streamTagAPIError) Error() string                 { return e.code + ": " + e.message }
func (e *streamTagAPIError) ErrorCode() string             { return e.code }
func (e *streamTagAPIError) ErrorMessage() string          { return e.message }
func (e *streamTagAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestReconcileStreamTagsClassifiesEveryUnsupportedCode(t *testing.T) {
	codes := []string{
		"AccessDenied",
		"AuthorizationError",
		"InternalException",
		"InternalServiceError",
		"InvalidAction",
		"InvalidParameterException",
		"InvalidParameterValue",
		"InvalidRequest",
		"OperationDisabledException",
		"OperationNotPermitted",
		"UnknownOperationException",
		"UnsupportedFeatureException",
		"UnsupportedOperation",
		"ValidationException",
	}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			apiErr := &streamTagAPIError{code: "Prefix" + code + "Suffix"}
			client := &fakeStreamClient{
				region: "us-iso-east-1",
				fail: map[string]error{
					"ListTagsForStream": fmt.Errorf("wrapped: %w", apiErr),
				},
			}

			err := reconcileStreamTags(context.Background(), client, testStreamARN, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{"ListTagsForStream"}, client.calls)
		})
	}
}

func TestReconcileStreamTagsValidationErrorClassificationIsCaseSensitive(t *testing.T) {
	tests := []struct {
		name    string
		message string
		wantErr bool
	}{
		{name: "supported text", message: "region does not support tagging"},
		{name: "different case", message: "region does not Support tagging", wantErr: true},
		{name: "unrelated text", message: "invalid tags", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiErr := &streamTagAPIError{code: "ValidationError", message: test.message}
			client := &fakeStreamClient{
				region: "us-isob-east-1",
				fail:   map[string]error{"ListTagsForStream": apiErr},
			}

			err := reconcileStreamTags(context.Background(), client, testStreamARN, nil)
			if test.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, apiErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestReconcileStreamTagsPropagatesRequiredErrors(t *testing.T) {
	apiErr := &streamTagAPIError{code: "UnsupportedOperation"}
	errorTests := []struct {
		name   string
		region string
		err    error
	}{
		{name: "standard partition", region: "us-east-1", err: apiErr},
		{
			name:   "unrelated API error",
			region: "us-iso-east-1",
			err:    &streamTagAPIError{code: "NoSuchEntity"},
		},
		{name: "non-API error", region: "us-iso-east-1", err: errors.New("connection closed")},
	}
	operationTests := []struct {
		name      string
		operation string
		listTags  []*awssdk.ListTagsForStreamOutput
	}{
		{name: "list", operation: "ListTagsForStream"},
		{
			name:      "remove",
			operation: "RemoveTagsFromStream",
			listTags: []*awssdk.ListTagsForStreamOutput{{
				Tags: []awstypes.Tag{{Key: aws.String("old"), Value: aws.String("value")}},
			}},
		},
		{
			name:      "add",
			operation: "AddTagsToStream",
			listTags:  []*awssdk.ListTagsForStreamOutput{{}},
		},
	}
	desired := map[string]string{"new": "value"}
	for _, operationTest := range operationTests {
		t.Run(operationTest.name, func(t *testing.T) {
			for _, errorTest := range errorTests {
				t.Run(errorTest.name, func(t *testing.T) {
					client := &fakeStreamClient{
						region:   errorTest.region,
						listTags: operationTest.listTags,
						fail: map[string]error{
							operationTest.operation: errorTest.err,
						},
					}

					err := reconcileStreamTags(
						context.Background(), client, testStreamARN, &desired,
					)
					require.Error(t, err)
					assert.ErrorIs(t, err, errorTest.err)
				})
			}
		})
	}
}

func TestStreamUpdateStopsUnsupportedTagCallsAndContinuesMutation(t *testing.T) {
	unsupported := &streamTagAPIError{code: "UnsupportedOperation"}
	tests := []struct {
		name      string
		operation string
		priorTags map[string]string
		tags      map[string]string
		listTags  []*awssdk.ListTagsForStreamOutput
		wantCalls []string
	}{
		{
			name:      "list",
			operation: "ListTagsForStream",
			priorTags: map[string]string{"old": "value"},
			tags:      map[string]string{"new": "value"},
			wantCalls: []string{
				"DescribeLimits",
				"ListTagsForStream",
				"IncreaseStreamRetentionPeriod",
				"DescribeStreamSummary",
				"DescribeStreamSummary",
			},
		},
		{
			name:      "remove",
			operation: "RemoveTagsFromStream",
			priorTags: map[string]string{"old": "value"},
			tags:      map[string]string{"new": "value"},
			listTags: []*awssdk.ListTagsForStreamOutput{{
				Tags: []awstypes.Tag{{Key: aws.String("old"), Value: aws.String("value")}},
			}},
			wantCalls: []string{
				"DescribeLimits",
				"ListTagsForStream",
				"RemoveTagsFromStream",
				"IncreaseStreamRetentionPeriod",
				"DescribeStreamSummary",
				"DescribeStreamSummary",
			},
		},
		{
			name:      "add",
			operation: "AddTagsToStream",
			priorTags: map[string]string{},
			tags:      map[string]string{"new": "value"},
			listTags:  []*awssdk.ListTagsForStreamOutput{{}},
			wantCalls: []string{
				"DescribeLimits",
				"ListTagsForStream",
				"AddTagsToStream",
				"IncreaseStreamRetentionPeriod",
				"DescribeStreamSummary",
				"DescribeStreamSummary",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			priorInput := validProvisionedStream(1)
			priorInput.Tags = &test.priorTags
			resource := validProvisionedStream(1)
			resource.RetentionPeriod = 48
			resource.Tags = &test.tags
			priorOutput := &StreamResourceOutput{
				ARN:               testStreamARN,
				Name:              "events",
				OpenShardCount:    1,
				StreamModeDetails: &StreamModeDetails{StreamMode: "PROVISIONED"},
			}
			client := &fakeStreamClient{
				region:   "us-iso-east-1",
				listTags: test.listTags,
				fail:     map[string]error{test.operation: unsupported},
			}

			_, err := resource.update(
				context.Background(),
				client,
				runtime.Prior[StreamResource, *StreamResourceOutput, *awsCfg]{
					Inputs: priorInput, Outputs: priorOutput, Observed: priorOutput,
				},
				testStreamOptions(&fakeStreamClock{}),
			)
			require.NoError(t, err)
			assert.Equal(t, test.wantCalls, client.calls)
		})
	}
}

func TestStreamCreatePropagatesUnsupportedTagErrorInNonstandardPartition(t *testing.T) {
	apiErr := &streamTagAPIError{code: "UnsupportedOperation"}
	client := &fakeStreamClient{
		region: "us-iso-east-1",
		fail:   map[string]error{"CreateStream": apiErr},
	}

	_, err := validProvisionedStream(1).create(
		context.Background(),
		client,
		testStreamOptions(&fakeStreamClock{}),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, apiErr)
	assert.NotContains(t, client.calls, "DeleteStream")
}
