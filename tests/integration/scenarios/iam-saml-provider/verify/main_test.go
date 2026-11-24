package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/stretchr/testify/require"
)

func TestVerifyProvider(t *testing.T) {
	for _, mode := range []string{"present", "metadata", "uuid", "expiration", "tags",
		"absent", "denied", "still-present"} {
		t.Run(mode, func(t *testing.T) {
			var requests []url.Values
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				requests = append(requests, r.PostForm)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/xml")
				if mode == "absent" || mode == "denied" {
					code := "NoSuchEntity"
					if mode == "denied" {
						code = "AccessDenied"
					}
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(w, `<ErrorResponse><Error><Code>%s</Code>`+
						`<Message>test</Message></Error></ErrorResponse>`, code)
					return
				}
				metadata, uuid, until, tag := "raw", "uuid", "2035-01-02T03:04:05Z", ""
				switch mode {
				case "metadata":
					metadata = "external"
				case "uuid":
					uuid = "other"
				case "expiration":
					until = "2036-01-02T03:04:05Z"
				case "tags":
					tag = "external"
				}
				fmt.Fprintf(w, `<GetSAMLProviderResponse><GetSAMLProviderResult>`+
					`<SAMLMetadataDocument>%s</SAMLMetadataDocument>`+
					`<SAMLProviderUUID>%s</SAMLProviderUUID><ValidUntil>%s</ValidUntil>`+
					`<Tags><member><Key>empty</Key><Value>%s</Value></member></Tags>`+
					`</GetSAMLProviderResult></GetSAMLProviderResponse>`, metadata, uuid, until, tag)
			}))
			defer server.Close()
			client := iam.New(iam.Options{
				Region: "us-east-1", BaseEndpoint: aws.String(server.URL),
				Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
					return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
				}),
			})
			provider := identity{ARN: "assigned-arn", UUID: "uuid",
				ValidUntil: aws.String("2035-01-02T03:04:05Z")}
			var err error
			if mode == "absent" || mode == "denied" || mode == "still-present" {
				err = verifyAbsent(t.Context(), client, provider.ARN)
			} else {
				err = verifyProvider(t.Context(), client, provider, "raw", map[string]string{"empty": ""})
			}
			if mode == "present" || mode == "absent" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, []url.Values{{"Action": {"GetSAMLProvider"},
				"Version": {"2010-05-08"}, "SAMLProviderArn": {"assigned-arn"}}}, requests)
		})
	}
}
