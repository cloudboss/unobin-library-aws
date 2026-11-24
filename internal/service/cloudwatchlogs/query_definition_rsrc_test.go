package cloudwatchlogs

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	logs "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/stretchr/testify/require"
)

func TestQueryDefinitionPreparation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		groups *[]string
		want   []string
	}{
		{"omitted", nil, nil},
		{"empty", &[]string{}, nil},
		{"only empty", &[]string{""}, []string{}},
		{"ordered", &[]string{"b", "", "a", "b"}, []string{"b", "a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := QueryDefinitionResource{Name: "query", QueryString: "", LogGroupNames: tc.groups}
			for _, id := range []string{"", "assigned"} {
				got, err := r.prepareRequest(id)
				require.NoError(t, err)
				want := &logs.PutQueryDefinitionInput{Name: aws.String("query"),
					QueryString: aws.String(""), LogGroupNames: tc.want}
				if id != "" {
					want.QueryDefinitionId = aws.String(id)
				}
				require.Equal(t, want, got)
			}
		})
	}
}

func TestQueryDefinitionLogGroupParser(t *testing.T) {
	for _, value := range []string{"", "aws/name", "a_./#-09AZ", strings.Repeat("x", 512),
		"arn:aws:s3:::bucket", "arn:aws::::resource:with:colons", "arn:aws-cn:UPPER::: ",
		"arn:aws:service:us-east-1:aws-managed:r", "arn:aws:s:abcd-name-99:partner-managed:r",
		"arn:aws:s::cwABCDEFGHIJ:r", "arn:aws:s::cw" + strings.Repeat("é", 10) + ":r",
		"arn:aws:s:::" + strings.Repeat("r", 600)} {
		t.Run("valid "+value, func(t *testing.T) {
			r := QueryDefinitionResource{LogGroupNames: &[]string{value}}
			_, err := r.prepareRequest("")
			require.NoError(t, err)
		})
	}
	for _, value := range []string{" ", "é", "a:*", strings.Repeat("x", 513),
		"ARN:aws:s:::r", "arn::s:::r", "arn:AWS:s:::r", "arn:aws-1:s:::r",
		"arn:aws:s:US-east-1::r", "arn:aws:s:us-east-123::r", "arn:aws:s::123:r",
		"arn:aws:s::CWABCDEFGHIJ:r", "arn:aws:s::cwABCDEFGHI\n:r", "arn:aws:s:::",
		"arn:aws:s::cw" + strings.Repeat("é", 9) + ":r"} {
		t.Run("invalid "+value, func(t *testing.T) {
			r := QueryDefinitionResource{LogGroupNames: &[]string{value}}
			got, err := r.prepareRequest("")
			require.ErrorContains(t, err, "log-group-names")
			require.Nil(t, got)
		})
	}
}
