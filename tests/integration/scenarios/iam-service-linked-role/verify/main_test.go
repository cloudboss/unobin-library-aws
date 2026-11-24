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

func TestVerifyRole(t *testing.T) {
	for _, mode := range []string{"present", "identity drift", "description drift", "tag drift"} {
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
				switch r.PostForm.Get("Action") {
				case "GetRole":
					id, description := "AROA123", "updated integration role"
					if mode == "identity drift" {
						id = "OTHER"
					}
					if mode == "description drift" {
						description = "external"
					}
					fmt.Fprintf(w, `<GetRoleResponse><GetRoleResult><Role><Arn>assigned-arn</Arn>
      <RoleId>%s</RoleId><Description>%s</Description></Role></GetRoleResult></GetRoleResponse>`,
						id, description)
				case "ListRoleTags":
					value := ""
					if mode == "tag drift" {
						value = "external"
					}
					fmt.Fprintf(w, `<ListRoleTagsResponse><ListRoleTagsResult><Tags>
      <member><Key>empty</Key><Value>%s</Value></member></Tags>
      </ListRoleTagsResult></ListRoleTagsResponse>`, value)
				default:
					t.Errorf("unexpected IAM action %s", r.PostForm.Get("Action"))
				}
			}))
			defer server.Close()
			client := iam.New(iam.Options{
				Region: "us-east-1", BaseEndpoint: aws.String(server.URL),
				Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
					return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
				}),
			})
			role := identity{ARN: "assigned-arn", Name: "assigned", ID: "AROA123"}
			err := verifyRole(t.Context(), client, role,
				"updated integration role", map[string]string{"empty": ""})
			if mode == "present" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			expected := []url.Values{{
				"Action": {"GetRole"}, "Version": {"2010-05-08"}, "RoleName": {"assigned"},
			}}
			if mode == "present" || mode == "tag drift" {
				expected = append(expected, url.Values{
					"Action": {"ListRoleTags"}, "Version": {"2010-05-08"}, "RoleName": {"assigned"},
				})
			}
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, expected, requests)
		})
	}
}
