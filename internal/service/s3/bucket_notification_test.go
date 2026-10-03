package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBucketNotificationConfiguration(t *testing.T) {
	empty := ""
	prefix := "images/"
	suffix := ".jpg"
	id := "topic-id"
	r := &BucketNotificationResource{
		Eventbridge: aws.Bool(true),
		LambdaFunction: new([]BucketNotificationLambdaFunction{
			{
				LambdaFunctionArn: aws.String("arn:aws:lambda:us-east-1:123:function:f"),
				Events:            []string{"", "s3:ObjectCreated:*"},
				FilterPrefix:      &empty,
				FilterSuffix:      &suffix,
			},
		}),
		Queue: new([]BucketNotificationQueue{
			{
				Id:           &empty,
				QueueArn:     "arn:aws:sqs:us-east-1:123:q",
				Events:       []string{"s3:ObjectRemoved:*", ""},
				FilterPrefix: &prefix,
			},
		}),
		Topic: new([]BucketNotificationTopic{
			{
				Id:       &id,
				TopicArn: "arn:aws:sns:us-east-1:123:t",
				Events:   []string{"", "s3:ReducedRedundancyLostObject", ""},
			},
		}),
	}
	prior := bucketNotificationEffectiveIDSource{
		prior: &BucketNotificationResourceOutput{
			LambdaFunctionSummaries: []BucketNotificationLambdaSummary{
				{Id: "lambda-prior"},
			},
		},
	}

	desired, err := r.notificationConfiguration(prior)
	require.NoError(t, err)
	got := desired.notification
	require.NotNil(t, got.EventBridgeConfiguration)

	assert.Equal(t, []BucketNotificationLambdaSummary{
		{
			Id:                "lambda-prior",
			LambdaFunctionArn: "arn:aws:lambda:us-east-1:123:function:f",
			Events:            []string{"s3:ObjectCreated:*"},
			FilterSuffix:      suffix,
		},
	}, desired.lambdaFunctionSummaries)
	assert.Equal(t, []BucketNotificationTopicSummary{
		{
			Id:       id,
			TopicArn: "arn:aws:sns:us-east-1:123:t",
			Events:   []string{"s3:ReducedRedundancyLostObject"},
		},
	}, desired.topicSummaries)

	require.Len(t, got.LambdaFunctionConfigurations, 1)
	lambda := got.LambdaFunctionConfigurations[0]
	assert.Equal(t, "lambda-prior", aws.ToString(lambda.Id))
	assert.Equal(t, "arn:aws:lambda:us-east-1:123:function:f",
		aws.ToString(lambda.LambdaFunctionArn))
	assert.Equal(t, []s3types.Event{s3types.EventS3ObjectCreated}, lambda.Events)
	prefixValue, suffixValue := bucketNotificationFilterValues(lambda.Filter)
	assert.Empty(t, prefixValue)
	assert.Equal(t, suffix, suffixValue)

	require.Len(t, got.QueueConfigurations, 1)
	queue := got.QueueConfigurations[0]
	queueID := aws.ToString(queue.Id)
	assert.True(t, strings.HasPrefix(queueID, "tf-s3-queue-"))
	assert.Equal(t, []BucketNotificationQueueSummary{
		{
			Id:           queueID,
			QueueArn:     "arn:aws:sqs:us-east-1:123:q",
			Events:       []string{"s3:ObjectRemoved:*"},
			FilterPrefix: prefix,
		},
	}, desired.queueSummaries)
	assert.Equal(t, "arn:aws:sqs:us-east-1:123:q", aws.ToString(queue.QueueArn))
	assert.Equal(t, []s3types.Event{s3types.EventS3ObjectRemoved}, queue.Events)
	prefixValue, suffixValue = bucketNotificationFilterValues(queue.Filter)
	assert.Equal(t, prefix, prefixValue)
	assert.Empty(t, suffixValue)

	require.Len(t, got.TopicConfigurations, 1)
	topic := got.TopicConfigurations[0]
	assert.Equal(t, id, aws.ToString(topic.Id))
	assert.Equal(t, "arn:aws:sns:us-east-1:123:t", aws.ToString(topic.TopicArn))
	assert.Equal(t,
		[]s3types.Event{s3types.EventS3ReducedRedundancyLostObject}, topic.Events)
	assert.Nil(t, topic.Filter)
}

func TestBucketNotificationConfigurationUsesObservedEffectiveIDs(t *testing.T) {
	r := &BucketNotificationResource{
		Queue: new([]BucketNotificationQueue{
			{
				QueueArn: "arn:aws:sqs:us-east-1:123:q",
				Events:   []string{"s3:ObjectCreated:*"},
			},
		}),
		Topic: new([]BucketNotificationTopic{
			{
				TopicArn: "arn:aws:sns:us-east-1:123:t",
				Events:   []string{"s3:ObjectRemoved:*"},
			},
		}),
	}
	source := bucketNotificationEffectiveIDSource{
		observed: &BucketNotificationResourceOutput{
			QueueSummaries: []BucketNotificationQueueSummary{{Id: "queue-observed"}},
		},
		prior: &BucketNotificationResourceOutput{
			QueueSummaries: []BucketNotificationQueueSummary{{Id: "queue-prior"}},
			TopicSummaries: []BucketNotificationTopicSummary{{Id: "topic-prior"}},
		},
	}

	desired, err := r.notificationConfiguration(source)

	require.NoError(t, err)
	got := desired.notification
	require.Len(t, got.QueueConfigurations, 1)
	assert.Equal(t, "queue-observed", aws.ToString(got.QueueConfigurations[0].Id))
	require.Len(t, got.TopicConfigurations, 1)
	assert.Equal(t, "topic-prior", aws.ToString(got.TopicConfigurations[0].Id))
}

func TestBucketNotificationCreateReturnsMatchingObservedConfiguration(t *testing.T) {
	queueA := "arn:aws:sqs:us-east-1:123456789012:images"
	queueB := "arn:aws:sqs:us-east-1:123456789012:logs"
	idA := "queue-images"
	idB := "queue-logs"
	server := newFakeBucketNotificationServer(t,
		bucketNotificationHTTPResponse{method: http.MethodPut, status: http.StatusOK},
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			body:   bucketNotificationXML(false),
		},
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			body:   bucketNotificationXML(true),
		},
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			body: bucketNotificationXML(true, bucketNotificationXMLQueue{
				id:       idB,
				arn:      queueB,
				events:   []string{"s3:ObjectRemoved:*"},
				prefix:   "logs/",
				reversed: true,
			}, bucketNotificationXMLQueue{
				id:       idA,
				arn:      queueA,
				events:   []string{"s3:ObjectCreated:*", "s3:ObjectRemoved:*"},
				prefix:   "images/",
				suffix:   ".jpg",
				reversed: true,
			}),
		},
	)
	resource := &BucketNotificationResource{
		Bucket:      "bucket",
		Eventbridge: aws.Bool(true),
		Queue: new([]BucketNotificationQueue{{
			Id:           &idA,
			QueueArn:     queueA,
			Events:       []string{"s3:ObjectRemoved:*", "s3:ObjectCreated:*"},
			FilterPrefix: aws.String("images/"),
			FilterSuffix: aws.String(".jpg"),
		}, {
			Id:           &idB,
			QueueArn:     queueB,
			Events:       []string{"s3:ObjectRemoved:*"},
			FilterPrefix: aws.String("logs/"),
		}}),
	}

	got, err := resource.Create(context.Background(), server.config())

	require.NoError(t, err)
	assert.True(t, got.Eventbridge)
	assert.Equal(t, []BucketNotificationQueueSummary{{
		Id:           idB,
		QueueArn:     queueB,
		Events:       []string{"s3:ObjectRemoved:*"},
		FilterPrefix: "logs/",
	}, {
		Id:           idA,
		QueueArn:     queueA,
		Events:       []string{"s3:ObjectCreated:*", "s3:ObjectRemoved:*"},
		FilterPrefix: "images/",
		FilterSuffix: ".jpg",
	}}, got.QueueSummaries)
	assert.Equal(t, []string{http.MethodPut, http.MethodGet, http.MethodGet, http.MethodGet},
		server.methods())
}

func TestBucketNotificationCreateReturnsFirstMatchingObservation(t *testing.T) {
	queue := "arn:aws:sqs:us-east-1:123456789012:images"
	id := "queue-observed"
	server := newFakeBucketNotificationServer(t,
		bucketNotificationHTTPResponse{method: http.MethodPut, status: http.StatusOK},
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			body: bucketNotificationXML(true, bucketNotificationXMLQueue{
				id:     id,
				arn:    queue,
				events: []string{"s3:ObjectCreated:*"},
			}),
		},
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			body:   bucketNotificationXML(false),
		},
	)
	resource := &BucketNotificationResource{
		Bucket:      "bucket",
		Eventbridge: aws.Bool(true),
		Queue: new([]BucketNotificationQueue{{
			Id:       &id,
			QueueArn: queue,
			Events:   []string{"s3:ObjectCreated:*"},
		}}),
	}

	got, err := resource.Create(context.Background(), server.config())

	require.NoError(t, err)
	assert.True(t, got.Eventbridge)
	assert.Equal(t, []BucketNotificationQueueSummary{{
		Id:       id,
		QueueArn: queue,
		Events:   []string{"s3:ObjectCreated:*"},
	}}, got.QueueSummaries)
	assert.Equal(t, []string{http.MethodPut, http.MethodGet}, server.methods())
}

func TestBucketNotificationUpdateReturnsMatchingObservedConfiguration(t *testing.T) {
	queue := "arn:aws:sqs:us-east-1:123456789012:images"
	id := "queue-configured"
	server := newFakeBucketNotificationServer(t,
		bucketNotificationHTTPResponse{method: http.MethodPut, status: http.StatusOK},
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			body: bucketNotificationXML(false, bucketNotificationXMLTopic{
				id:     "topic-prior",
				arn:    "arn:aws:sns:us-east-1:123456789012:images",
				events: []string{"s3:ObjectCreated:*"},
			}),
		},
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			body: bucketNotificationXML(false, bucketNotificationXMLQueue{
				id:     id,
				arn:    queue,
				events: []string{"s3:ObjectCreated:*"},
			}),
		},
	)
	resource := &BucketNotificationResource{
		Bucket: "bucket",
		Queue: new([]BucketNotificationQueue{{
			Id:       &id,
			QueueArn: queue,
			Events:   []string{"s3:ObjectCreated:*"},
		}}),
	}
	prior := runtime.Prior[
		BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg,
	]{
		Inputs: BucketNotificationResource{
			Bucket: "bucket",
			Topic: new([]BucketNotificationTopic{{
				TopicArn: "arn:aws:sns:us-east-1:123456789012:images",
				Events:   []string{"s3:ObjectCreated:*"},
			}}),
		},
		Outputs: &BucketNotificationResourceOutput{
			Bucket: "bucket",
			TopicSummaries: []BucketNotificationTopicSummary{{
				Id:       "topic-prior",
				TopicArn: "arn:aws:sns:us-east-1:123456789012:images",
				Events:   []string{"s3:ObjectCreated:*"},
			}},
		},
	}

	got, err := resource.Update(context.Background(), server.config(), prior)

	require.NoError(t, err)
	assert.Equal(t, []BucketNotificationQueueSummary{{
		Id:       id,
		QueueArn: queue,
		Events:   []string{"s3:ObjectCreated:*"},
	}}, got.QueueSummaries)
	assert.Empty(t, got.TopicSummaries)
	assert.Equal(t, []string{http.MethodPut, http.MethodGet, http.MethodGet},
		server.methods())
}

func TestBucketNotificationCreateAcceptsEmptyDesiredConfiguration(t *testing.T) {
	server := newFakeBucketNotificationServer(t,
		bucketNotificationHTTPResponse{method: http.MethodPut, status: http.StatusOK},
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			body:   bucketNotificationXML(false),
		},
	)
	resource := &BucketNotificationResource{Bucket: "bucket", Eventbridge: aws.Bool(false)}

	got, err := resource.Create(context.Background(), server.config())

	require.NoError(t, err)
	assert.False(t, got.Eventbridge)
	assert.Empty(t, got.QueueSummaries)
	assert.Empty(t, got.TopicSummaries)
	assert.Empty(t, got.LambdaFunctionSummaries)
}

func TestBucketNotificationUpdateNoOpReadsWithoutPut(t *testing.T) {
	queue := "arn:aws:sqs:us-east-1:123456789012:images"
	server := newFakeBucketNotificationServer(t,
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			body: bucketNotificationXML(false, bucketNotificationXMLQueue{
				id:     "queue-observed",
				arn:    queue,
				events: []string{"s3:ObjectCreated:*"},
			}),
		},
	)
	resource := &BucketNotificationResource{
		Bucket: "bucket",
		Queue: new([]BucketNotificationQueue{{
			QueueArn: queue,
			Events:   []string{"s3:ObjectCreated:*"},
		}}),
	}
	prior := runtime.Prior[
		BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg,
	]{
		Inputs: *resource,
		Outputs: &BucketNotificationResourceOutput{
			Bucket: "bucket",
			QueueSummaries: []BucketNotificationQueueSummary{{
				Id:       "queue-observed",
				QueueArn: queue,
				Events:   []string{"s3:ObjectCreated:*"},
			}},
		},
		Observed: &BucketNotificationResourceOutput{
			Bucket: "bucket",
			QueueSummaries: []BucketNotificationQueueSummary{{
				Id:       "queue-observed",
				QueueArn: queue,
				Events:   []string{"s3:ObjectCreated:*"},
			}},
		},
	}

	got, err := resource.Update(context.Background(), server.config(), prior)

	require.NoError(t, err)
	assert.Equal(t, []BucketNotificationQueueSummary{{
		Id:       "queue-observed",
		QueueArn: queue,
		Events:   []string{"s3:ObjectCreated:*"},
	}}, got.QueueSummaries)
	assert.Empty(t, got.LambdaFunctionSummaries)
	assert.Empty(t, got.TopicSummaries)
	assert.Equal(t, []string{http.MethodGet}, server.methods())
}

func TestBucketNotificationConvergenceErrors(t *testing.T) {
	t.Run("get access denied aborts", func(t *testing.T) {
		server := newFakeBucketNotificationServer(t,
			bucketNotificationHTTPResponse{method: http.MethodPut, status: http.StatusOK},
			bucketNotificationHTTPResponse{
				method: http.MethodGet,
				status: http.StatusForbidden,
				body:   bucketNotificationErrorXML("AccessDenied", "denied"),
			},
		)
		resource := &BucketNotificationResource{
			Bucket:      "bucket",
			Eventbridge: aws.Bool(true),
		}

		_, err := resource.Create(context.Background(), server.config())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "get bucket notification configuration")
		assert.Contains(t, err.Error(), "AccessDenied")
	})

	t.Run("context cancellation aborts", func(t *testing.T) {
		var cancel context.CancelFunc
		server := newFakeBucketNotificationServer(t,
			bucketNotificationHTTPResponse{method: http.MethodPut, status: http.StatusOK},
			bucketNotificationHTTPResponse{
				method: http.MethodGet,
				status: http.StatusOK,
				body:   bucketNotificationXML(false),
				after:  func() { cancel() },
			},
		)
		resource := &BucketNotificationResource{
			Bucket:      "bucket",
			Eventbridge: aws.Bool(true),
		}
		ctx, cancelFunc := context.WithCancel(context.Background())
		cancel = cancelFunc

		_, err := resource.Create(ctx, server.config())

		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled), "got %v", err)
	})

	t.Run("directory bucket unsupported put keeps message", func(t *testing.T) {
		server := newFakeBucketNotificationServer(t,
			bucketNotificationHTTPResponse{
				method: http.MethodPut,
				status: http.StatusBadRequest,
				body: bucketNotificationErrorXML(
					"InvalidArgument", bucketNotificationDirectoryBucketMessage),
			},
		)
		resource := &BucketNotificationResource{Bucket: "bucket", Eventbridge: aws.Bool(true)}
		awsConfig := aws.Config{
			Region:           "us-east-1",
			BaseEndpoint:     aws.String(server.server.URL),
			Credentials:      aws.AnonymousCredentials{},
			RetryMaxAttempts: 1,
		}

		err := resource.put(
			context.Background(),
			s3ClientFromConfig(awsConfig),
			resource.Bucket,
			&s3types.NotificationConfiguration{},
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "bucket notifications are not supported for bucket bucket")
		assert.Contains(t, err.Error(), bucketNotificationDirectoryBucketMessage)
	})
}

func TestBucketNotificationCreateReturnsGeneratedQueueID(t *testing.T) {
	queue := "arn:aws:sqs:us-east-1:123456789012:images"
	server := newFakeBucketNotificationServer(t,
		bucketNotificationHTTPResponse{method: http.MethodPut, status: http.StatusOK},
		bucketNotificationHTTPResponse{
			method: http.MethodGet,
			status: http.StatusOK,
			bodyFunc: func(fake *fakeBucketNotificationServer) string {
				return bucketNotificationXML(false, bucketNotificationXMLQueue{
					id:     fake.lastPutQueueID(),
					arn:    queue,
					events: []string{"s3:ObjectCreated:*"},
				})
			},
		},
	)
	resource := &BucketNotificationResource{
		Bucket: "bucket",
		Queue: new([]BucketNotificationQueue{{
			QueueArn: queue,
			Events:   []string{"s3:ObjectCreated:*"},
		}}),
	}

	got, err := resource.Create(context.Background(), server.config())

	require.NoError(t, err)
	require.Len(t, got.QueueSummaries, 1)
	assert.True(t, strings.HasPrefix(got.QueueSummaries[0].Id, "tf-s3-queue-"))
	assert.Equal(t, queue, got.QueueSummaries[0].QueueArn)
}

type bucketNotificationHTTPResponse struct {
	method   string
	status   int
	body     string
	bodyFunc func(*fakeBucketNotificationServer) string
	after    func()
}

type fakeBucketNotificationServer struct {
	t         *testing.T
	server    *httptest.Server
	mu        sync.Mutex
	responses []bucketNotificationHTTPResponse
	requests  []string
	lastPut   string
}

func newFakeBucketNotificationServer(
	t *testing.T,
	responses ...bucketNotificationHTTPResponse,
) *fakeBucketNotificationServer {
	t.Helper()
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "testdata/nonexistent-aws-config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "testdata/nonexistent-aws-credentials")

	fake := &fakeBucketNotificationServer{t: t, responses: responses}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeBucketNotificationServer) config() *awsCfg {
	maxAttempts := int64(1)
	return &awscfg.Configuration{
		Region:      aws.String("us-east-1"),
		EndpointURL: aws.String(f.server.URL),
		MaxAttempts: &maxAttempts,
	}
}

func (f *fakeBucketNotificationServer) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *fakeBucketNotificationServer) serve(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, r.Method)
	if r.Method == http.MethodPut {
		f.lastPut = string(bodyBytes)
	}
	idx := len(f.requests) - 1
	if idx >= len(f.responses) {
		f.mu.Unlock()
		http.Error(w, "unexpected request", http.StatusInternalServerError)
		return
	}
	response := f.responses[idx]
	f.mu.Unlock()

	if response.method != "" && r.Method != response.method {
		f.t.Errorf("request %d method = %s, want %s", idx, r.Method, response.method)
		http.Error(w, "unexpected method", http.StatusInternalServerError)
		return
	}
	if response.status == 0 {
		response.status = http.StatusOK
	}
	body := response.body
	if response.bodyFunc != nil {
		body = response.bodyFunc(f)
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(response.status)
	if body != "" {
		_, _ = w.Write([]byte(body))
	}
	if response.after != nil {
		response.after()
	}
}

func (f *fakeBucketNotificationServer) lastPutQueueID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	start := strings.Index(f.lastPut, "<QueueConfiguration>")
	if start == -1 {
		return ""
	}
	rest := f.lastPut[start:]
	idStart := strings.Index(rest, "<Id>")
	idEnd := strings.Index(rest, "</Id>")
	if idStart == -1 || idEnd == -1 || idEnd < idStart {
		return ""
	}
	return rest[idStart+len("<Id>") : idEnd]
}

type bucketNotificationXMLQueue struct {
	id       string
	arn      string
	events   []string
	prefix   string
	suffix   string
	reversed bool
}

type bucketNotificationXMLTopic struct {
	id     string
	arn    string
	events []string
}

func bucketNotificationXML(eventbridge bool, destinations ...any) string {
	var b strings.Builder
	b.WriteString(`<NotificationConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	if eventbridge {
		b.WriteString(`<EventBridgeConfiguration/>`)
	}
	for _, destination := range destinations {
		switch item := destination.(type) {
		case bucketNotificationXMLQueue:
			b.WriteString(`<QueueConfiguration>`)
			b.WriteString(fmt.Sprintf(`<Id>%s</Id><Queue>%s</Queue>`, item.id, item.arn))
			for _, event := range item.events {
				b.WriteString(fmt.Sprintf(`<Event>%s</Event>`, event))
			}
			b.WriteString(bucketNotificationXMLFilter(item.prefix, item.suffix, item.reversed))
			b.WriteString(`</QueueConfiguration>`)
		case bucketNotificationXMLTopic:
			b.WriteString(`<TopicConfiguration>`)
			b.WriteString(fmt.Sprintf(`<Id>%s</Id><Topic>%s</Topic>`, item.id, item.arn))
			for _, event := range item.events {
				b.WriteString(fmt.Sprintf(`<Event>%s</Event>`, event))
			}
			b.WriteString(`</TopicConfiguration>`)
		}
	}
	b.WriteString(`</NotificationConfiguration>`)
	return b.String()
}

func bucketNotificationXMLFilter(prefix, suffix string, reversed bool) string {
	if prefix == "" && suffix == "" {
		return ""
	}
	var rules []string
	if prefix != "" {
		rules = append(rules, fmt.Sprintf(
			`<FilterRule><Name>prefix</Name><Value>%s</Value></FilterRule>`, prefix))
	}
	if suffix != "" {
		rules = append(rules, fmt.Sprintf(
			`<FilterRule><Name>suffix</Name><Value>%s</Value></FilterRule>`, suffix))
	}
	if reversed {
		for i, j := 0, len(rules)-1; i < j; i, j = i+1, j-1 {
			rules[i], rules[j] = rules[j], rules[i]
		}
	}
	return `<Filter><S3Key>` + strings.Join(rules, "") + `</S3Key></Filter>`
}

func bucketNotificationErrorXML(code, message string) string {
	return fmt.Sprintf(`<Error><Code>%s</Code><Message>%s</Message></Error>`, code, message)
}

func TestBucketNotificationNeedsPut(t *testing.T) {
	configured := "configured"
	prefix := "images/"
	tests := []struct {
		name  string
		item  *BucketNotificationResource
		prior runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]
		want  bool
	}{
		{
			name: "input change writes full configuration",
			item: &BucketNotificationResource{Eventbridge: aws.Bool(true)},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs:   BucketNotificationResource{Eventbridge: aws.Bool(false)},
				Observed: &BucketNotificationResourceOutput{Eventbridge: true},
			},
			want: true,
		},
		{
			name: "eventbridge drift writes full configuration",
			item: &BucketNotificationResource{Eventbridge: aws.Bool(true)},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs:   BucketNotificationResource{Eventbridge: aws.Bool(true)},
				Observed: &BucketNotificationResourceOutput{Eventbridge: false},
			},
			want: true,
		},
		{
			name: "explicit destination id drift writes full configuration",
			item: &BucketNotificationResource{
				LambdaFunction: new([]BucketNotificationLambdaFunction{{Id: &configured}}),
			},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs: BucketNotificationResource{
					LambdaFunction: new([]BucketNotificationLambdaFunction{{Id: &configured}}),
				},
				Observed: &BucketNotificationResourceOutput{
					LambdaFunctionSummaries: []BucketNotificationLambdaSummary{
						{Id: "drifted"},
					},
				},
			},
			want: true,
		},
		{
			name: "omitted destination id drift accepts observed effective id",
			item: &BucketNotificationResource{
				Queue: new([]BucketNotificationQueue{
					{QueueArn: "arn", Events: []string{"s3:ObjectCreated:*"}},
				}),
			},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs: BucketNotificationResource{
					Queue: new([]BucketNotificationQueue{
						{QueueArn: "arn", Events: []string{"s3:ObjectCreated:*"}},
					}),
				},
				Outputs: &BucketNotificationResourceOutput{
					QueueSummaries: []BucketNotificationQueueSummary{{Id: "old"}},
				},
				Observed: &BucketNotificationResourceOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:       "new",
							QueueArn: "arn",
							Events:   []string{"s3:ObjectCreated:*"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "destination count drift writes full configuration",
			item: &BucketNotificationResource{
				Queue: new([]BucketNotificationQueue{
					{QueueArn: "arn", Events: []string{"s3:ObjectCreated:*"}},
				}),
			},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs: BucketNotificationResource{
					Queue: new([]BucketNotificationQueue{
						{QueueArn: "arn", Events: []string{"s3:ObjectCreated:*"}},
					}),
				},
				Outputs: &BucketNotificationResourceOutput{
					QueueSummaries: []BucketNotificationQueueSummary{{Id: "old"}},
				},
				Observed: &BucketNotificationResourceOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{Id: "new"},
						{Id: "extra"},
					},
				},
			},
			want: true,
		},
		{
			name: "destination arn drift writes full configuration",
			item: &BucketNotificationResource{
				Queue: new([]BucketNotificationQueue{
					{
						Id:       &configured,
						QueueArn: "arn:desired",
						Events:   []string{"s3:ObjectCreated:*"},
					},
				}),
			},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs: BucketNotificationResource{
					Queue: new([]BucketNotificationQueue{
						{
							Id:       &configured,
							QueueArn: "arn:desired",
							Events:   []string{"s3:ObjectCreated:*"},
						},
					}),
				},
				Observed: &BucketNotificationResourceOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:       configured,
							QueueArn: "arn:drifted",
							Events:   []string{"s3:ObjectCreated:*"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "destination event drift writes full configuration",
			item: &BucketNotificationResource{
				Queue: new([]BucketNotificationQueue{
					{
						Id:       &configured,
						QueueArn: "arn",
						Events:   []string{"s3:ObjectCreated:*"},
					},
				}),
			},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs: BucketNotificationResource{
					Queue: new([]BucketNotificationQueue{
						{
							Id:       &configured,
							QueueArn: "arn",
							Events:   []string{"s3:ObjectCreated:*"},
						},
					}),
				},
				Observed: &BucketNotificationResourceOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:       configured,
							QueueArn: "arn",
							Events:   []string{"s3:ObjectRemoved:*"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "destination filter drift writes full configuration",
			item: &BucketNotificationResource{
				Queue: new([]BucketNotificationQueue{
					{
						Id:           &configured,
						QueueArn:     "arn",
						Events:       []string{"s3:ObjectCreated:*"},
						FilterPrefix: &prefix,
					},
				}),
			},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs: BucketNotificationResource{
					Queue: new([]BucketNotificationQueue{
						{
							Id:           &configured,
							QueueArn:     "arn",
							Events:       []string{"s3:ObjectCreated:*"},
							FilterPrefix: &prefix,
						},
					}),
				},
				Observed: &BucketNotificationResourceOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:           configured,
							QueueArn:     "arn",
							Events:       []string{"s3:ObjectCreated:*"},
							FilterPrefix: "logs/",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "normalized destination events skip put",
			item: &BucketNotificationResource{
				Queue: new([]BucketNotificationQueue{
					{
						Id:       &configured,
						QueueArn: "arn",
						Events: []string{
							"s3:ObjectRemoved:*", "", "s3:ObjectCreated:*",
							"s3:ObjectCreated:*",
						},
					},
				}),
			},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs: BucketNotificationResource{
					Queue: new([]BucketNotificationQueue{
						{
							Id:       &configured,
							QueueArn: "arn",
							Events: []string{
								"s3:ObjectRemoved:*", "", "s3:ObjectCreated:*",
								"s3:ObjectCreated:*",
							},
						},
					}),
				},
				Observed: &BucketNotificationResourceOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:       configured,
							QueueArn: "arn",
							Events: []string{
								"s3:ObjectCreated:*", "s3:ObjectRemoved:*",
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "reordered destinations skip put",
			item: &BucketNotificationResource{
				Queue: new([]BucketNotificationQueue{
					{
						Id:       aws.String("a"),
						QueueArn: "arn:a",
						Events:   []string{"s3:ObjectCreated:*"},
					},
					{
						Id:       aws.String("b"),
						QueueArn: "arn:b",
						Events:   []string{"s3:ObjectRemoved:*"},
					},
				}),
			},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs: BucketNotificationResource{
					Queue: new([]BucketNotificationQueue{
						{
							Id:       aws.String("a"),
							QueueArn: "arn:a",
							Events:   []string{"s3:ObjectCreated:*"},
						},
						{
							Id:       aws.String("b"),
							QueueArn: "arn:b",
							Events:   []string{"s3:ObjectRemoved:*"},
						},
					}),
				},
				Observed: &BucketNotificationResourceOutput{
					QueueSummaries: []BucketNotificationQueueSummary{
						{
							Id:       "b",
							QueueArn: "arn:b",
							Events:   []string{"s3:ObjectRemoved:*"},
						},
						{
							Id:       "a",
							QueueArn: "arn:a",
							Events:   []string{"s3:ObjectCreated:*"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "matching observed state skips put",
			item: &BucketNotificationResource{
				Eventbridge: aws.Bool(true),
				Topic:       new([]BucketNotificationTopic{{Id: &configured}}),
			},
			prior: runtime.Prior[BucketNotificationResource, *BucketNotificationResourceOutput, *awsCfg]{
				Inputs: BucketNotificationResource{
					Eventbridge: aws.Bool(true),
					Topic:       new([]BucketNotificationTopic{{Id: &configured}}),
				},
				Observed: &BucketNotificationResourceOutput{
					Eventbridge: true,
					TopicSummaries: []BucketNotificationTopicSummary{
						{Id: configured, Events: []string{}},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := bucketNotificationEffectiveIDSource{
				observed: tt.prior.Observed,
				prior:    tt.prior.Outputs,
			}
			desired, err := tt.item.notificationConfiguration(ids)
			require.NoError(t, err)
			assert.Equal(t, tt.want, tt.item.needsPut(tt.prior, desired))
		})
	}
}

func TestBucketNotificationEventsOmitsEmptyStrings(t *testing.T) {
	tests := []struct {
		name   string
		events []string
		want   []s3types.Event
	}{
		{
			name:   "no configured events returns nil",
			events: nil,
			want:   nil,
		},
		{
			name:   "nonempty events keep order",
			events: []string{"s3:ObjectCreated:*", "s3:ObjectRemoved:*"},
			want: []s3types.Event{
				s3types.EventS3ObjectCreated,
				s3types.EventS3ObjectRemoved,
			},
		},
		{
			name:   "empty strings are omitted",
			events: []string{"", "s3:ObjectCreated:*", ""},
			want:   []s3types.Event{s3types.EventS3ObjectCreated},
		},
		{
			name:   "all empty strings returns an empty list",
			events: []string{"", ""},
			want:   []s3types.Event{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bucketNotificationEvents(tt.events))
		})
	}
}

func TestBucketNotificationOutputSummaries(t *testing.T) {
	prefix := "images/"
	suffix := ".jpg"
	resp := &s3.GetBucketNotificationConfigurationOutput{
		EventBridgeConfiguration: &s3types.EventBridgeConfiguration{},
		LambdaFunctionConfigurations: []s3types.LambdaFunctionConfiguration{
			{
				Id:                aws.String("lambda-id"),
				LambdaFunctionArn: aws.String("arn:lambda"),
				Events: []s3types.Event{
					s3types.EventS3ObjectRemoved,
					"",
					s3types.EventS3ObjectCreated,
					s3types.EventS3ObjectCreated,
				},
				Filter: bucketNotificationFilter(&prefix, &suffix),
			},
		},
		QueueConfigurations: []s3types.QueueConfiguration{
			{
				Id:       aws.String("queue-id"),
				QueueArn: aws.String("arn:queue"),
				Events:   []s3types.Event{s3types.EventS3ObjectCreated},
				Filter:   bucketNotificationFilter(&prefix, nil),
			},
		},
		TopicConfigurations: []s3types.TopicConfiguration{
			{
				Id:       aws.String("topic-id"),
				TopicArn: aws.String("arn:topic"),
				Events:   []s3types.Event{s3types.EventS3ObjectRemoved},
				Filter:   bucketNotificationFilter(nil, &suffix),
			},
		},
	}

	got := bucketNotificationOutput("bucket", resp)

	assert.Equal(t, &BucketNotificationResourceOutput{
		Bucket:      "bucket",
		Eventbridge: true,
		LambdaFunctionSummaries: []BucketNotificationLambdaSummary{
			{
				Id:                "lambda-id",
				LambdaFunctionArn: "arn:lambda",
				Events:            []string{"s3:ObjectCreated:*", "s3:ObjectRemoved:*"},
				FilterPrefix:      prefix,
				FilterSuffix:      suffix,
			},
		},
		QueueSummaries: []BucketNotificationQueueSummary{
			{
				Id:           "queue-id",
				QueueArn:     "arn:queue",
				Events:       []string{"s3:ObjectCreated:*"},
				FilterPrefix: prefix,
			},
		},
		TopicSummaries: []BucketNotificationTopicSummary{
			{
				Id:           "topic-id",
				TopicArn:     "arn:topic",
				Events:       []string{"s3:ObjectRemoved:*"},
				FilterSuffix: suffix,
			},
		},
	}, got)
}

func TestBucketNotificationOutputEmptyConfiguration(t *testing.T) {
	resp := &s3.GetBucketNotificationConfigurationOutput{}
	resp.ResultMetadata.Set("request-id", "id")

	got := bucketNotificationOutput("bucket", resp)

	assert.Equal(t, &BucketNotificationResourceOutput{
		Bucket:                  "bucket",
		Eventbridge:             false,
		LambdaFunctionSummaries: []BucketNotificationLambdaSummary{},
		QueueSummaries:          []BucketNotificationQueueSummary{},
		TopicSummaries:          []BucketNotificationTopicSummary{},
	}, got)
}

func TestBucketNotificationMigrateV1Output(t *testing.T) {
	got, err := (&BucketNotificationResource{}).Migrate(1, runtime.MigrationState{
		Inputs: map[string]any{"bucket": "bucket"},
		Outputs: map[string]any{
			"bucket":          "bucket",
			"lambda-function": []any{map[string]any{"id": "lambda-id"}},
			"queue":           []any{map[string]any{"id": "queue-id"}},
			"topic":           []any{map[string]any{"id": "topic-id"}},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, runtime.MigrationState{
		Inputs: map[string]any{"bucket": "bucket"},
		Outputs: map[string]any{
			"bucket":      "bucket",
			"eventbridge": false,
			"lambda-function-observed-summaries": []BucketNotificationLambdaSummary{
				{Id: "lambda-id"},
			},
			"queue-observed-summaries": []BucketNotificationQueueSummary{
				{Id: "queue-id"},
			},
			"topic-observed-summaries": []BucketNotificationTopicSummary{
				{Id: "topic-id"},
			},
		},
	}, got)
}

func TestBucketNotificationMigrateV2Output(t *testing.T) {
	got, err := (&BucketNotificationResource{}).Migrate(2, runtime.MigrationState{
		Inputs: map[string]any{"bucket": "bucket"},
		Outputs: map[string]any{
			"bucket":                        "bucket",
			"eventbridge":                   true,
			"lambda-function-effective-ids": []any{"lambda-id"},
			"queue-effective-ids":           []any{"queue-id"},
			"topic-effective-ids":           []any{"topic-id"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, runtime.MigrationState{
		Inputs: map[string]any{"bucket": "bucket"},
		Outputs: map[string]any{
			"bucket":      "bucket",
			"eventbridge": true,
			"lambda-function-observed-summaries": []BucketNotificationLambdaSummary{
				{Id: "lambda-id"},
			},
			"queue-observed-summaries": []BucketNotificationQueueSummary{
				{Id: "queue-id"},
			},
			"topic-observed-summaries": []BucketNotificationTopicSummary{
				{Id: "topic-id"},
			},
		},
	}, got)
}

func TestBucketNotificationFilterValues(t *testing.T) {
	prefix := "logs/"
	suffix := ".gz"
	ignored := "ignored"
	filter := &s3types.NotificationConfigurationFilter{
		Key: &s3types.S3KeyFilter{
			FilterRules: []s3types.FilterRule{
				{Name: s3types.FilterRuleName("PREFIX"), Value: &prefix},
				{Name: s3types.FilterRuleName("unknown"), Value: &ignored},
				{Name: s3types.FilterRuleName("Suffix"), Value: &suffix},
			},
		},
	}

	gotPrefix, gotSuffix := bucketNotificationFilterValues(filter)

	assert.Equal(t, prefix, gotPrefix)
	assert.Equal(t, suffix, gotSuffix)
}

func TestBucketNotificationZero(t *testing.T) {
	assert.True(t, bucketNotificationZero(nil))
	assert.True(t, bucketNotificationZero(&s3.GetBucketNotificationConfigurationOutput{}))

	resp := &s3.GetBucketNotificationConfigurationOutput{}
	resp.ResultMetadata.Set("request-id", "id")
	assert.False(t, bucketNotificationZero(resp))

	assert.False(t, bucketNotificationZero(&s3.GetBucketNotificationConfigurationOutput{
		EventBridgeConfiguration: &s3types.EventBridgeConfiguration{},
	}))
	assert.False(t, bucketNotificationZero(&s3.GetBucketNotificationConfigurationOutput{
		QueueConfigurations: []s3types.QueueConfiguration{{Id: aws.String("id")}},
	}))
}

func TestBucketNotificationDeleteRetryable(t *testing.T) {
	assert.True(t, bucketNotificationDeleteRetryable(&smithy.GenericAPIError{
		Code:    "OperationAborted",
		Message: "A conflicting conditional operation is currently in progress.",
	}))
	assert.False(t, bucketNotificationDeleteRetryable(&smithy.GenericAPIError{
		Code:    "NoSuchBucket",
		Message: "missing",
	}))
}

func TestBucketNotificationDirectoryBucketUnsupported(t *testing.T) {
	err := &smithy.GenericAPIError{
		Code:    "InvalidArgument",
		Message: "NotificationConfiguration is not valid, expected CreateBucketConfiguration",
	}

	assert.True(t, isBucketNotificationDirectoryBucketUnsupported(err))
	assert.False(t, isBucketNotificationDirectoryBucketUnsupported(&smithy.GenericAPIError{
		Code:    "InvalidArgument",
		Message: "different",
	}))
}

func TestDirectoryBucketAWSConfigUsesRegionalEndpoint(t *testing.T) {
	got := directoryBucketAWSConfig(aws.Config{Region: awsGlobalRegion})
	assert.Equal(t, usEast1Region, got.Region)

	got = directoryBucketAWSConfig(aws.Config{Region: "us-west-2"})
	assert.Equal(t, "us-west-2", got.Region)
}

func TestBucketNotificationClientKind(t *testing.T) {
	assert.Equal(t, bucketNotificationClientS3Express,
		bucketNotificationClientKindFor("bucket--usw2-az1--x-s3"))
	assert.Equal(t, bucketNotificationClientS3Express,
		bucketNotificationClientKindFor("my--s3--bucket--abcd-ab1--x-s3"))
	assert.Equal(t, bucketNotificationClientS3,
		bucketNotificationClientKindFor("bucket--x-s3"))
	assert.Equal(t, bucketNotificationClientS3,
		bucketNotificationClientKindFor("bucket"))
}

func TestDirectoryBucketName(t *testing.T) {
	assert.True(t, isDirectoryBucketName("bucket--usw2-az1--x-s3"))
	assert.True(t, isDirectoryBucketName("my--s3--bucket--abcd-ab1--x-s3"))
	assert.False(t, isDirectoryBucketName("--usw2-az1--x-s3"))
	assert.False(t, isDirectoryBucketName("bucket--USW2-AZ1--x-s3"))
	assert.False(t, isDirectoryBucketName("bucket--x-s3"))
	assert.False(t, isDirectoryBucketName("bucket"))
}
