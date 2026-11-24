package main

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyStoppedAndDestroyed(t *testing.T) {
	for _, test := range []struct{ phase, failure string }{
		{phase: "applied"}, {phase: "updated"}, {phase: "destroyed"},
		{phase: "applied", failure: "running"}, {phase: "applied", failure: "missing"},
		{phase: "applied", failure: "nil state"}, {phase: "applied", failure: "multiple"},
		{phase: "destroyed", failure: "stopped"}, {phase: "destroyed", failure: "vpc"},
	} {
		t.Run(test.phase+"/"+test.failure, func(t *testing.T) {
			var requests []url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, r.ParseForm())
				requests = append(requests, r.PostForm)
				w.Header().Set("Content-Type", "text/xml")
				switch r.PostForm.Get("Action") {
				case "DescribeInstances":
					if r.PostForm.Get("NextToken") == "" {
						fmt.Fprint(w, "<DescribeInstancesResponse><nextToken>later</nextToken>"+
							"</DescribeInstancesResponse>")
						return
					}
					state, copies := "stopped", 1
					if test.phase == "destroyed" && test.failure != "stopped" {
						state = "terminated"
					}
					if test.failure == "running" {
						state = "running"
					}
					if test.failure == "multiple" {
						copies = 2
					}
					if test.failure == "missing" {
						copies = 0
					}
					fmt.Fprint(w, "<DescribeInstancesResponse><reservationSet><item><instancesSet>")
					fmt.Fprint(w, "<item><instanceId>unrelated</instanceId>"+
						"<instanceState><name>running</name></instanceState></item>")
					for range copies {
						fmt.Fprint(w, "<item><instanceId>i-abcdef01</instanceId>"+
							"<tagSet><item><key>unobin</key><value>"+marker+"</value></item></tagSet>")
						if test.failure != "nil state" {
							fmt.Fprintf(w, "<instanceState><name>%s</name></instanceState>", state)
						}
						fmt.Fprint(w, "</item>")
					}
					fmt.Fprint(w, "</instancesSet></item></reservationSet></DescribeInstancesResponse>")
				case "DescribeVpcs":
					fmt.Fprint(w, "<DescribeVpcsResponse><vpcSet>")
					fmt.Fprint(w, "<item><vpcId>unrelated</vpcId></item>")
					if test.failure == "vpc" {
						fmt.Fprint(w, "<item><vpcId>vpc-owned</vpcId>"+
							"<cidrBlock>10.87.0.0/16</cidrBlock></item>")
					}
					fmt.Fprint(w, "</vpcSet></DescribeVpcsResponse>")
				default:
					t.Errorf("unexpected operation %s", r.PostForm.Get("Action"))
				}
			}))
			defer server.Close()
			client := ec2.NewFromConfig(aws.Config{
				Region: "us-east-1", BaseEndpoint: &server.URL,
				Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
			})
			err := verify(t.Context(), client, test.phase)
			if test.failure != "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			first := url.Values{
				"Action": {"DescribeInstances"}, "Version": {"2016-11-15"},
				"Filter.1.Name": {"tag:unobin"}, "Filter.1.Value.1": {marker},
			}
			second := maps.Clone(first)
			second.Set("NextToken", "later")
			want := []url.Values{first, second}
			if test.phase == "destroyed" {
				vpcs := maps.Clone(first)
				vpcs.Set("Action", "DescribeVpcs")
				vpcs.Set("Filter.1.Name", "cidr-block-association.cidr-block")
				vpcs.Set("Filter.1.Value.1", "10.87.0.0/16")
				want = append(want, vpcs)
			}
			assert.Equal(t, want, requests)
		})
	}
}

func TestVerifyUpdateRequiresActionRerun(t *testing.T) {
	for _, decision := range []runtime.Decision{runtime.DecisionRerun, runtime.DecisionSkip} {
		plan := &runtime.Plan{Steps: []*runtime.PlanStep{{
			Address: "action.stop", Decision: decision,
		}}}
		encoded, err := runtime.SealPlan(plan, encrypters.Noop{})
		require.NoError(t, err)
		path := filepath.Join(t.TempDir(), "saved.plan")
		require.NoError(t, os.WriteFile(path, encoded, 0o600))
		err = verifyUpdatePlan(path)
		if decision == runtime.DecisionRerun {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "rerun")
		}
	}
}
