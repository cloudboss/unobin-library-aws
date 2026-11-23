package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalDestroyChecksOwnedFixtures(t *testing.T) {
	for _, tc := range []struct{ name, connection, vpc, diagnostic string }{
		{name: "absent"},
		{name: "deleted", connection: "deleted"},
		{name: "missing API codes", connection: "missing", vpc: "missing"},
		{name: "surviving connection", connection: "active", diagnostic: "connection survived"},
		{name: "connection error", connection: "denied", diagnostic: "Denied"},
		{name: "surviving VPC", vpc: "active", diagnostic: "VPC survived"},
		{name: "VPC error", vpc: "denied", diagnostic: "Denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				got = append(got, r.PostForm)
				w.Header().Set("Content-Type", "text/xml")
				action, state := r.Form.Get("Action"), tc.connection
				code := "InvalidVpcPeeringConnectionID.NotFound"
				if action == "DescribeVpcs" {
					state, code = tc.vpc, "InvalidVpcID.NotFound"
				}
				switch state {
				case "missing", "denied":
					if state == "denied" {
						code = "Denied"
					}
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(w, `<Response><Errors><Error><Code>%s</Code>`+
						`<Message>test failure</Message></Error></Errors></Response>`, code)
				default:
					if action == "DescribeVpcs" {
						fmt.Fprint(w, `<DescribeVpcsResponse><vpcSet>`)
						if state == "active" {
							fmt.Fprint(w, `<item><vpcId>vpc-one</vpcId><cidrBlock>10.65.0.0/16</cidrBlock></item>`)
						}
						fmt.Fprint(w, `</vpcSet></DescribeVpcsResponse>`)
					} else {
						fmt.Fprint(w, `<DescribeVpcPeeringConnectionsResponse><vpcPeeringConnectionSet>`)
						if state != "" {
							fmt.Fprintf(w, `<item><vpcPeeringConnectionId>pcx-fixture</vpcPeeringConnectionId>`+
								`<status><code>%s</code></status></item>`, state)
						}
						fmt.Fprint(w, `</vpcPeeringConnectionSet></DescribeVpcPeeringConnectionsResponse>`)
					}
				}
			}))
			defer server.Close()
			buildDir := t.TempDir()
			t.Setenv("VERIFY_BUILD_DIR", buildDir)
			t.Setenv("VERIFY_PHASE", "destroyed")
			t.Setenv("AWS_ACCESS_KEY_ID", "test")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
			t.Setenv("AWS_SESSION_TOKEN", "")
			t.Setenv("AWS_PROFILE", "")
			t.Setenv("AWS_REGION", "us-east-1")
			t.Setenv("AWS_ENDPOINT_URL_EC2", server.URL)
			t.Setenv("AWS_CONFIG_FILE", filepath.Join(buildDir, "missing"))
			t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(buildDir, "missing"))
			require.NoError(t, os.WriteFile(filepath.Join(buildDir, "adopted-id"),
				[]byte("pcx-fixture"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(buildDir, "vpc-ids"),
				[]byte("vpc-one\nvpc-two"), 0o600))
			err := run(t.Context())
			if tc.diagnostic != "" {
				require.ErrorContains(t, err, tc.diagnostic)
			} else {
				require.NoError(t, err)
			}
			want := []url.Values{{
				"Action": {"DescribeVpcPeeringConnections"}, "Version": {"2016-11-15"},
				"VpcPeeringConnectionId.1": {"pcx-fixture"},
			}}
			if tc.connection != "active" && tc.connection != "denied" {
				want = append(want, url.Values{
					"Action": {"DescribeVpcs"}, "Version": {"2016-11-15"}, "VpcId.1": {"vpc-one"},
				})
				if tc.vpc != "active" && tc.vpc != "denied" {
					want = append(want, url.Values{
						"Action": {"DescribeVpcs"}, "Version": {"2016-11-15"}, "VpcId.1": {"vpc-two"},
					})
				}
			}
			assert.Equal(t, want, got)
		})
	}
}
