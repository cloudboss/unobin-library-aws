package cloudwatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudWatchClientUsesAWSQueryProtocol(t *testing.T) {
	// The pinned emulators cannot serialize DescribeAlarms responses as RPC v2 CBOR.
	type requestDetails struct {
		contentType string
		action      string
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		requests <- requestDetails{
			contentType: r.Header.Get("Content-Type"),
			action:      r.PostForm.Get("Action"),
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := awssvc.New(awssvc.Options{
		BaseEndpoint: aws.String(server.URL),
		Credentials: aws.CredentialsProviderFunc(
			func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     "test",
					SecretAccessKey: "test",
					Source:          "test",
				}, nil
			},
		),
		HTTPClient:       server.Client(),
		Region:           "us-east-1",
		RetryMaxAttempts: 1,
	})
	_, _ = client.DescribeAlarms(context.Background(), &awssvc.DescribeAlarmsInput{})

	select {
	case request := <-requests:
		assert.Equal(t, "application/x-www-form-urlencoded", request.contentType)
		assert.Equal(t, "DescribeAlarms", request.action)
	default:
		require.Fail(t, "CloudWatch client sent no request")
	}
}
