package ec2

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func describeSubnetsPageXML(nextToken string, ids ...string) string {
	var items strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&items, `<item><subnetId>%s</subnetId></item>`, id)
	}
	next := ""
	if nextToken != "" {
		next = fmt.Sprintf("<nextToken>%s</nextToken>", nextToken)
	}
	return fmt.Sprintf(`<DescribeSubnetsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
  <requestId>req-subnets</requestId>
  <subnetSet>%s</subnetSet>
  %s
</DescribeSubnetsResponse>`, items.String(), next)
}

func TestSubnetsReadPaginatesAndSendsFilters(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSubnets", func(n int, form url.Values) (int, string) {
		switch n {
		case 1:
			assert.Empty(t, form.Get("NextToken"))
			return 200, describeSubnetsPageXML("token-2",
				"subnet-00000000000000001", "subnet-00000000000000002")
		case 2:
			assert.Equal(t, "token-2", form.Get("NextToken"))
			return 200, describeSubnetsPageXML("", "subnet-00000000000000003")
		default:
			t.Fatalf("unexpected DescribeSubnets call %d", n)
			return 500, ""
		}
	})
	cfg := fake.configuration()

	r := &Subnets{
		Tags: new(map[string]string{"unobin": "subnets-test"}),
		Filter: new([]SubnetsFilter{
			{Name: "vpc-id", Values: []string{"vpc-0123456789abcdef0"}},
			{Name: "description", Values: []string{""}},
			{Name: "empty-values", Values: []string{}},
		}),
	}
	out, err := r.Read(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"subnet-00000000000000001",
		"subnet-00000000000000002",
		"subnet-00000000000000003",
	}, out.Ids)

	sent := fake.sent("DescribeSubnets")
	require.Len(t, sent, 2)
	assertEC2Filter(t, sent[0], "tag:unobin", []string{"subnets-test"})
	assertEC2Filter(t, sent[0], "vpc-id", []string{"vpc-0123456789abcdef0"})
	assertEC2Filter(t, sent[0], "description", []string{""})
	assertEC2Filter(t, sent[0], "empty-values", []string{})
}

func TestSubnetsReadWithNoFiltersAllowsEmptyResult(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSubnets", func(_ int, form url.Values) (int, string) {
		for key := range form {
			assert.False(t, strings.HasPrefix(key, "Filter."), "unexpected filter %s", key)
		}
		return 200, describeSubnetsPageXML("")
	})
	cfg := fake.configuration()

	out, err := (&Subnets{}).Read(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, out.Ids)
	assert.Empty(t, out.Ids)
}

func TestSubnetsReadTreatsNotFoundAsReadError(t *testing.T) {
	fake := newFakeEC2(t)
	fake.on("DescribeSubnets", func(int, url.Values) (int, string) {
		return 400, ec2ErrorXML("InvalidSubnetID.NotFound",
			"The subnet ID 'subnet-missing' does not exist")
	})
	cfg := fake.configuration()

	r := &Subnets{Filter: new([]SubnetsFilter{{Name: "subnet-id", Values: []string{"subnet-missing"}}})}
	out, err := r.Read(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.NotErrorIs(t, err, runtime.ErrNotFound)
	assert.Contains(t, err.Error(), "reading EC2 subnets")
}

func assertEC2Filter(t *testing.T, form url.Values, name string, want []string) {
	t.Helper()
	got, ok := ec2FilterValues(form, name)
	require.True(t, ok, "missing EC2 filter %q in %v", name, form)
	assert.Equal(t, want, got)
}

func ec2FilterValues(form url.Values, name string) ([]string, bool) {
	for i := 1; ; i++ {
		nameKey := fmt.Sprintf("Filter.%d.Name", i)
		names, ok := form[nameKey]
		if !ok {
			return nil, false
		}
		if len(names) == 0 || names[0] != name {
			continue
		}
		values := []string{}
		for j := 1; ; j++ {
			valueKey := fmt.Sprintf("Filter.%d.Value.%d", i, j)
			vals, ok := form[valueKey]
			if !ok {
				return values, true
			}
			values = append(values, vals...)
		}
	}
}
