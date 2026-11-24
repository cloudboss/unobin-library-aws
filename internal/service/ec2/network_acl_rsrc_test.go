package ec2

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkACLPrepareRequests(t *testing.T) {
	r := NetworkACLResource{VpcId: "vpc-1", Egress: &[]NetworkACLRule{{
		RuleAction: "ALLOW", Protocol: "tcp", RuleNumber: 10, FromPort: 1, ToPort: 99,
		CidrBlock: aws.String("10.0.0.0/8"),
	}}}
	in, err := r.prepareRequest("token")
	require.NoError(t, err)
	require.Equal(t, &ec2.CreateNetworkAclInput{
		VpcId: aws.String("vpc-1"), ClientToken: aws.String("token"),
	}, in.create)
	require.Equal(t, []ec2.CreateNetworkAclEntryInput{{
		Egress: aws.Bool(true), RuleAction: ec2types.RuleActionAllow,
		Protocol: aws.String("6"), RuleNumber: aws.Int32(10),
		PortRange: &ec2types.PortRange{From: aws.Int32(1), To: aws.Int32(99)},
		CidrBlock: aws.String("10.0.0.0/8"),
	}}, in.egress)
}

func TestNetworkACLCreateTagsAndEqualityDefaults(t *testing.T) {
	r := NetworkACLResource{Tags: &map[string]string{"blank": "", "aws:reserved": "ignore"}}
	prepared, err := r.prepareRequest("operation")
	require.NoError(t, err)
	assert.Equal(t, []ec2types.TagSpecification{{ResourceType: ec2types.ResourceTypeNetworkAcl,
		Tags: []ec2types.Tag{{Key: aws.String("blank"), Value: aws.String("")}}}},
		prepared.create.TagSpecifications)
	rule := NetworkACLRule{RuleNumber: 10, Protocol: "icmp", RuleAction: "ALLOW"}
	other := rule
	other.Protocol, other.RuleAction = "+001", "allow"
	other.IcmpCode, other.IcmpType = aws.Int64(0), aws.Int64(0)
	assert.True(t, networkACLRulesEqual(&[]NetworkACLRule{rule}, &[]NetworkACLRule{other, other}))
	assert.True(t, networkACLRulesEqual(&[]NetworkACLRule{rule}, nil))
	assert.False(t, networkACLRulesEqual(&[]NetworkACLRule{rule}, &[]NetworkACLRule{}))
	assert.True(t, networkACLSubnetsEqual(&[]string{"b", "a"}, &[]string{"a", "b", "a"}))
}

func TestNetworkACLProtocolAndCIDRConversion(t *testing.T) {
	for _, tc := range []struct {
		input  string
		number int
	}{
		{"all", -1}, {"-01", -1}, {"+006", 6}, {"icmp", 1}, {"ipv6-icmp", 58},
		{"hopopt", 0}, {"tlsp", 56}, {"ipv6-nonxt", 59}, {"a/n", 107},
		{"shim6", 140}, {"wesp", 141}, {"rohc", 142}, {"253", 253}, {"254", 254},
	} {
		t.Run(tc.input, func(t *testing.T) {
			n, err := networkACLProtocol(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.number, n)
		})
	}
	for n := -2; n <= 255; n++ {
		_, err := networkACLProtocol(strconv.Itoa(n))
		assert.Equal(t, n < -1 || n == 57 || n > 142 && n < 253 || n == 255, err != nil)
	}
	for _, invalid := range []string{"TCP", "All", " tcp", "tcp ", "RESERVED", "", "6.0"} {
		_, err := networkACLProtocol(invalid)
		require.ErrorContains(t, err, "protocol")
	}
	for _, tc := range []struct {
		v4, v6 *string
		valid  bool
	}{
		{nil, nil, true}, {aws.String("0.0.0.0/0"), aws.String("::/0"), true},
		{aws.String("10.1.0.1/16"), nil, false}, {aws.String("::/0"), nil, false},
		{nil, aws.String("10.0.0.0/8"), false}, {nil, aws.String("2001:db8::1/64"), false},
	} {
		r := NetworkACLResource{Ingress: &[]NetworkACLRule{{Protocol: "tcp",
			CidrBlock: tc.v4, Ipv6CidrBlock: tc.v6}}}
		_, err := r.prepareRequest("")
		assert.Equal(t, tc.valid, err == nil)
	}
	for _, protocol := range []string{"icmp", "ipv6-icmp", "all", "udp"} {
		r := NetworkACLResource{Ingress: &[]NetworkACLRule{{Protocol: protocol,
			RuleAction: "deny", RuleNumber: 20}}}
		input, err := r.prepareRequest("")
		require.NoError(t, err)
		p, err := networkACLProtocol(protocol)
		require.NoError(t, err)
		want := ec2.CreateNetworkAclEntryInput{Egress: aws.Bool(false),
			Protocol: aws.String(strconv.Itoa(p)), RuleAction: ec2types.RuleActionDeny,
			RuleNumber: aws.Int32(20),
			PortRange:  &ec2types.PortRange{From: aws.Int32(0), To: aws.Int32(0)}}
		if p == 1 || p == 58 {
			want.IcmpTypeCode = &ec2types.IcmpTypeCode{Type: aws.Int32(0), Code: aws.Int32(0)}
		}
		assert.Equal(t, []ec2.CreateNetworkAclEntryInput{want}, input.ingress)
	}
}

func TestNetworkACLRulesWithSameNumberRetainSetEquality(t *testing.T) {
	a := NetworkACLRule{RuleNumber: 10, Protocol: "6", RuleAction: "allow"}
	b := a
	b.RuleAction = "deny"
	assert.True(t, networkACLRulesEqual(&[]NetworkACLRule{a, b}, &[]NetworkACLRule{b, a, a}))
}

func TestNetworkACLCIDRPrepareRequests(t *testing.T) {
	for _, direction := range []string{"ingress", "egress"} {
		for _, tc := range []struct {
			name   string
			v4, v6 *string
			valid  bool
		}{
			{"absent", nil, nil, true},
			{"both", aws.String("0.0.0.0/0"), aws.String("::/0"), true},
			{"IPv4 prefix zero", aws.String("10.0.0.0/08"), nil, true},
			{"IPv6 prefix zero", nil, aws.String("2001:db8::/064"), true},
			{"mapped IPv4", aws.String("::ffff:192.0.2.0/120"), nil, true},
			{"mapped IPv6", nil, aws.String("::ffff:192.0.2.0/120"), false},
			{"IPv6 in IPv4", aws.String("2001:db8::/64"), nil, false},
			{"IPv4 in IPv6", nil, aws.String("10.0.0.0/8"), false},
			{"IPv4 host bits", aws.String("10.0.0.1/08"), nil, false},
			{"IPv6 host bits", nil, aws.String("2001:db8::1/064"), false},
			{"mapped host bits", aws.String("::ffff:192.0.2.1/120"), nil, false},
			{"malformed", aws.String("not-a-cidr"), nil, false},
			{"empty", nil, aws.String(""), false},
		} {
			t.Run(direction+"/"+tc.name, func(t *testing.T) {
				rules := &[]NetworkACLRule{{RuleAction: "ALLOW", Protocol: "tcp",
					RuleNumber: 10, FromPort: 1, ToPort: 99,
					CidrBlock: tc.v4, Ipv6CidrBlock: tc.v6}}
				r := NetworkACLResource{VpcId: "vpc-one"}
				if direction == "ingress" {
					r.Ingress = rules
				} else {
					r.Egress = rules
				}
				got, err := r.prepareRequest("token")
				if !tc.valid {
					require.ErrorContains(t, err, direction+" rule 10: invalid network CIDR")
					assert.Nil(t, got)
					return
				}
				require.NoError(t, err)
				want := &networkACLRequests{create: &ec2.CreateNetworkAclInput{
					VpcId: aws.String("vpc-one"), ClientToken: aws.String("token"),
				}}
				entries := []ec2.CreateNetworkAclEntryInput{{
					Egress: aws.Bool(direction == "egress"), Protocol: aws.String("6"),
					RuleAction: ec2types.RuleActionAllow, RuleNumber: aws.Int32(10),
					PortRange: &ec2types.PortRange{From: aws.Int32(1), To: aws.Int32(99)},
					CidrBlock: tc.v4, Ipv6CidrBlock: tc.v6,
				}}
				if direction == "ingress" {
					want.ingress = entries
				} else {
					want.egress = entries
				}
				assert.Equal(t, want, got)
			})
		}
	}
}

func networkACLTestClient(t *testing.T, f *fakeEC2) (*networkACLClient, *flowLogTestClock) {
	t.Helper()
	c, err := newNetworkACLClient(t.Context(), f.configuration())
	require.NoError(t, err)
	clock := &flowLogTestClock{}
	c.now, c.sleep, c.timeout = clock.Now, clock.Sleep, time.Second
	return c, clock
}

func networkACLItem(id, entries, associations string) string {
	return "<item><networkAclId>" + id + "</networkAclId><vpcId>vpc-remote</vpcId>" +
		"<ownerId>123456789012</ownerId><entrySet>" + entries + "</entrySet>" +
		"<associationSet>" + associations + "</associationSet></item>"
}

func networkACLPage(items, next string) (int, string) {
	return http.StatusOK, "<DescribeNetworkAclsResponse><networkAclSet>" + items +
		"</networkAclSet>" + next + "</DescribeNetworkAclsResponse>"
}

func networkACLAssociation(subnet, association string) string {
	return fmt.Sprintf("<item><subnetId>%s</subnetId><networkAclAssociationId>%s"+
		"</networkAclAssociationId></item>", subnet, association)
}

func networkACLEntry(number int, egress bool, action string) string {
	return fmt.Sprintf("<item><ruleNumber>%d</ruleNumber><egress>%t</egress>"+
		"<ruleAction>%s</ruleAction><protocol>6</protocol><cidrBlock>10.0.0.0/8</cidrBlock>"+
		"<portRange><from>80</from><to>80</to></portRange></item>", number, egress, action)
}

func TestNetworkACLReadPaginationAndOutput(t *testing.T) {
	f := newFakeEC2(t)
	f.on("DescribeNetworkAcls", func(n int, form url.Values) (int, string) {
		assert.Equal(t, "acl-1", form.Get("NetworkAclId.1"))
		if n == 1 {
			return networkACLPage("", "<nextToken>page-2</nextToken>")
		}
		assert.Equal(t, "page-2", form.Get("NextToken"))
		entries := networkACLEntry(20, false, "allow") + networkACLEntry(10, true, "deny") +
			networkACLEntry(32767, true, "deny") + networkACLEntry(32768, false, "deny")
		return networkACLPage(networkACLItem("acl-1", entries,
			networkACLAssociation("subnet-b", "b")+networkACLAssociation("subnet-a", "a")), "")
	})
	c, _ := networkACLTestClient(t, f)
	out, err := c.read(t.Context(), "acl-1")
	require.NoError(t, err)
	rule := NetworkACLRule{RuleNumber: 20, Protocol: "6", RuleAction: "allow",
		FromPort: 80, ToPort: 80, CidrBlock: aws.String("10.0.0.0/8"),
		IcmpType: aws.Int64(0), IcmpCode: aws.Int64(0)}
	egress := rule
	egress.RuleNumber, egress.RuleAction = 10, "deny"
	assert.Equal(t, &NetworkACLResourceOutput{NetworkACLId: "acl-1", VpcId: "vpc-remote",
		OwnerId: "123456789012", Arn: "arn:aws:ec2:us-east-1:123456789012:network-acl/acl-1",
		Ingress: []NetworkACLRule{rule}, Egress: []NetworkACLRule{egress},
		SubnetIds: []string{"subnet-a", "subnet-b"}, Tags: map[string]string{}}, out)
}

func TestNetworkACLReadFailures(t *testing.T) {
	for _, mode := range []string{"empty", "wrong", "multiple", "missing", "terminal", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DescribeNetworkAcls", func(int, url.Values) (int, string) {
				switch mode {
				case "empty":
					return networkACLPage("", "")
				case "wrong":
					return networkACLPage(networkACLItem("acl-other", "", ""), "")
				case "multiple":
					return networkACLPage(networkACLItem("acl-1", "", "")+
						networkACLItem("acl-1", "", ""), "")
				case "missing":
					return 400, ec2ErrorXML(networkACLNotFound, "gone")
				case "terminal":
					return 400, ec2ErrorXML("UnauthorizedOperation", "denied")
				default:
					return networkACLPage("<item><networkAclId>acl-1</networkAclId></item>", "")
				}
			})
			c, clock := networkACLTestClient(t, f)
			_, err := c.read(t.Context(), "acl-1")
			require.Error(t, err)
			assert.Equal(t, mode == "empty" || mode == "wrong" || mode == "missing",
				errors.Is(err, runtime.ErrNotFound))
			assert.Empty(t, clock.waits)
		})
	}
}

func TestNetworkACLRetryBoundaries(t *testing.T) {
	for _, tc := range []struct {
		timeout time.Duration
		grace   bool
		waits   []time.Duration
	}{
		{500 * time.Millisecond, false, []time.Duration{500 * time.Millisecond}},
		{500 * time.Millisecond, true, []time.Duration{500 * time.Millisecond, time.Second}},
		{20 * time.Second, true, []time.Duration{500 * time.Millisecond, time.Second,
			2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}},
	} {
		clock := &flowLogTestClock{}
		c := &networkACLClient{now: clock.Now, sleep: clock.Sleep, timeout: tc.timeout}
		var calls []time.Duration
		var last error
		err := c.retry(t.Context(), tc.grace, func(error) bool { return true }, func() error {
			calls = append(calls, clock.elapsed)
			last = fmt.Errorf("attempt %d", len(calls))
			return last
		})
		assert.Same(t, last, err)
		assert.Equal(t, tc.waits, clock.waits)
		want := []time.Duration{0}
		for _, delay := range tc.waits {
			want = append(want, want[len(want)-1]+delay)
		}
		assert.Equal(t, want, calls)
	}
	clock := &flowLogTestClock{cancel: true}
	c := &networkACLClient{now: clock.Now, sleep: clock.Sleep, timeout: time.Second}
	err := c.retry(t.Context(), true, func(error) bool { return true },
		func() error { return errors.New("retry") })
	require.ErrorIs(t, err, context.Canceled)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, networkACLSleep(ctx, time.Hour), context.Canceled)
}

func TestNetworkACLRetryInFlightAndTerminalResults(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		clock := &flowLogTestClock{}
		c := &networkACLClient{now: clock.Now, sleep: clock.Sleep, timeout: time.Second}
		sentinel := errors.New("terminal")
		calls := 0
		err := c.retry(t.Context(), true, func(error) bool { return false }, func() error {
			calls++
			clock.elapsed = 10 * time.Minute
			if terminal {
				return sentinel
			}
			return nil
		})
		if terminal {
			require.ErrorIs(t, err, sentinel)
		} else {
			require.NoError(t, err)
		}
		assert.Equal(t, 1, calls)
		assert.Empty(t, clock.waits)
	}
}

func TestNetworkACLDeleteAbsentAndTerminal(t *testing.T) {
	for _, mode := range []string{"absent", "terminal", "missing after observation"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DescribeNetworkAcls", func(int, url.Values) (int, string) {
				if mode == "absent" {
					return 400, ec2ErrorXML(networkACLNotFound, "gone")
				}
				return networkACLPage(networkACLItem("acl-saved", "", ""), "")
			})
			f.on("DeleteNetworkAcl", func(_ int, form url.Values) (int, string) {
				assert.Equal(t, "acl-saved", form.Get("NetworkAclId"))
				code := networkACLNotFound
				if mode == "terminal" {
					code = "UnauthorizedOperation"
				}
				return 400, ec2ErrorXML(code, "failed")
			})
			c, clock := networkACLTestClient(t, f)
			err := c.delete(t.Context(), "acl-saved", "vpc-saved")
			if mode == "terminal" {
				require.ErrorContains(t, err, "UnauthorizedOperation")
			} else {
				require.NoError(t, err)
			}
			assert.Empty(t, clock.waits)
			if mode == "absent" {
				assert.Empty(t, f.sent("DeleteNetworkAcl"))
			}
		})
	}
}

func TestNetworkACLUpdateFailureDoesNotCompensate(t *testing.T) {
	f := newFakeEC2(t)
	f.on("CreateNetworkAclEntry", func(int, url.Values) (int, string) {
		return 400, ec2ErrorXML("UpdateFailure", "failed")
	})
	c, _ := networkACLTestClient(t, f)
	r := NetworkACLResource{Ingress: &[]NetworkACLRule{{Protocol: "tcp", RuleAction: "allow",
		RuleNumber: 10}}}
	prepared, err := r.prepareRequest("")
	require.NoError(t, err)
	out, err := r.update(t.Context(), c, networkACLPrior{
		Outputs: &NetworkACLResourceOutput{NetworkACLId: "acl-saved"}}, prepared)
	require.ErrorContains(t, err, "UpdateFailure")
	assert.Nil(t, out)
	assert.Equal(t, []string{"CreateNetworkAclEntry"}, peeringActions(f))
}

func TestNetworkACLUpdateOmissionClearSetsAndDrift(t *testing.T) {
	for _, mode := range []string{"omitted", "clear", "normalized", "drift"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DescribeNetworkAcls", func(_ int, form url.Values) (int, string) {
				if form.Get("Filter.1.Name") == "association.subnet-id" {
					assert.Equal(t, "subnet-1", form.Get("Filter.1.Value.1"))
					return networkACLPage(networkACLItem("acl-1", "",
						networkACLAssociation("subnet-1", "assoc-1")), "")
				}
				if form.Get("Filter.1.Name") == "vpc-id" {
					assert.Equal(t, "vpc-remote", form.Get("Filter.1.Value.1"))
					assert.Equal(t, "default", form.Get("Filter.2.Name"))
					assert.Equal(t, "true", form.Get("Filter.2.Value.1"))
					return networkACLPage(networkACLItem("acl-default", "", ""), "")
				}
				return networkACLPage(networkACLItem("acl-1", "", ""), "")
			})
			for _, action := range []string{"CreateTags", "DeleteTags", "CreateNetworkAclEntry",
				"DeleteNetworkAclEntry", "ReplaceNetworkAclAssociation"} {
				f.on(action, func(_ int, _ url.Values) (int, string) { return peeringSuccess(action) })
			}
			c, _ := networkACLTestClient(t, f)
			rule := NetworkACLRule{RuleAction: "allow", Protocol: "6", RuleNumber: 10,
				FromPort: 80, ToPort: 80, CidrBlock: aws.String("10.0.0.0/8")}
			known := &NetworkACLResourceOutput{NetworkACLId: "acl-1", VpcId: "vpc-remote",
				Ingress: []NetworkACLRule{rule}, SubnetIds: []string{"subnet-1"}}
			r := NetworkACLResource{VpcId: "vpc-remote"}
			switch mode {
			case "clear":
				r.Ingress, r.SubnetIds = &[]NetworkACLRule{}, &[]string{}
			case "normalized":
				rule.Protocol, rule.RuleAction = "+006", "ALLOW"
				r.Ingress, r.SubnetIds = &[]NetworkACLRule{rule, rule}, &[]string{"subnet-1", "subnet-1"}
			case "drift":
				r.Ingress, r.SubnetIds = &[]NetworkACLRule{rule}, &[]string{}
				known.Ingress[0].RuleAction = "deny"
				known.Tags = map[string]string{"changed": "remote", "aws:reserved": "keep"}
			}
			prepared, err := r.prepareRequest("")
			require.NoError(t, err)
			_, err = r.update(t.Context(), c, networkACLPrior{Outputs: &NetworkACLResourceOutput{
				NetworkACLId: "acl-1"}, Observed: known}, prepared)
			require.NoError(t, err)
			var mutations []string
			for _, action := range peeringActions(f) {
				if action != "DescribeNetworkAcls" {
					mutations = append(mutations, action)
				}
			}
			switch mode {
			case "omitted", "normalized":
				assert.Empty(t, mutations)
			case "clear":
				assert.Equal(t, []string{"DeleteNetworkAclEntry", "ReplaceNetworkAclAssociation"}, mutations)
			case "drift":
				assert.Equal(t, []string{"DeleteTags", "DeleteNetworkAclEntry",
					"CreateNetworkAclEntry", "ReplaceNetworkAclAssociation"}, mutations)
				assert.Equal(t, []url.Values{peeringForm("DeleteTags", url.Values{
					"ResourceId.1": {"acl-1"}, "Tag.1.Key": {"changed"}})}, f.sent("DeleteTags"))
			}
			if mode == "clear" || mode == "drift" {
				assert.Equal(t, []url.Values{peeringForm("DeleteNetworkAclEntry", url.Values{
					"NetworkAclId": {"acl-1"}, "RuleNumber": {"10"}, "Egress": {"false"}})},
					f.sent("DeleteNetworkAclEntry"))
				assert.Equal(t, []url.Values{peeringForm("ReplaceNetworkAclAssociation", url.Values{
					"NetworkAclId": {"acl-default"}, "AssociationId": {"assoc-1"}})},
					f.sent("ReplaceNetworkAclAssociation"))
			}
		})
	}
}

type networkACLHTTPClient struct {
	base   aws.HTTPClient
	before func(*http.Request)
}

func (c networkACLHTTPClient) Do(request *http.Request) (*http.Response, error) {
	c.before(request)
	return c.base.Do(request)
}

func TestNetworkACLAcceptedCreateFailureCompensation(t *testing.T) {
	for _, mode := range []string{"egress", "ingress", "association", "read", "visibility",
		"malformed", "canceled", "cleanup failure", "rejected", "missing ID", "success"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeEC2(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cleanup := false
			accepted := false
			f.on("CreateNetworkAcl", func(_ int, form url.Values) (int, string) {
				assert.NotEmpty(t, form.Get("ClientToken"))
				assert.Equal(t, "vpc-desired", form.Get("VpcId"))
				if mode == "rejected" {
					return 400, ec2ErrorXML("Rejected", "reject")
				}
				if mode == "missing ID" {
					return 200, "<CreateNetworkAclResponse/>"
				}
				accepted = true
				return 200, "<CreateNetworkAclResponse><networkAcl><networkAclId>acl-new" +
					"</networkAclId></networkAcl></CreateNetworkAclResponse>"
			})
			f.on("CreateNetworkAclEntry", func(n int, form url.Values) (int, string) {
				assert.Equal(t, "acl-new", form.Get("NetworkAclId"))
				fail := mode == "egress" && n == 2 || mode == "ingress" && n == 4 ||
					mode == "cleanup failure" && n == 2
				if fail {
					cleanup = true
					return 400, ec2ErrorXML("EntryFailure", "entry failed")
				}
				return peeringSuccess("CreateNetworkAclEntry")
			})
			f.on("DescribeNetworkAcls", func(_ int, form url.Values) (int, string) {
				if form.Get("Filter.1.Name") == "association.subnet-id" {
					subnet := form.Get("Filter.1.Value.1")
					return networkACLPage(networkACLItem("acl-old", "",
						networkACLAssociation(subnet, "assoc-"+subnet)), "")
				}
				if form.Get("Filter.1.Name") == "vpc-id" {
					assert.True(t, cleanup)
					assert.Equal(t, "vpc-remote", form.Get("Filter.1.Value.1"))
					return networkACLPage(networkACLItem("acl-default", "", ""), "")
				}
				if !cleanup {
					switch mode {
					case "read":
						cleanup = true
						return 400, ec2ErrorXML("ReadFailure", "failed")
					case "visibility":
						return networkACLPage("", "")
					case "malformed":
						cleanup = true
						return networkACLPage("<item><networkAclId>acl-new</networkAclId></item>", "")
					case "canceled":
						cleanup = true
						cancel()
						return networkACLPage("", "")
					}
				}
				return networkACLPage(networkACLItem("acl-new", "",
					networkACLAssociation("subnet-external", "assoc-external")), "")
			})
			f.on("ReplaceNetworkAclAssociation", func(n int, form url.Values) (int, string) {
				if form.Get("NetworkAclId") == "acl-default" {
					assert.True(t, cleanup)
					assert.Equal(t, "assoc-external", form.Get("AssociationId"))
					return peeringSuccess("ReplaceNetworkAclAssociation")
				}
				if mode == "association" && n == 2 {
					cleanup = true
					return 400, ec2ErrorXML("AssociationFailure", "failed")
				}
				return peeringSuccess("ReplaceNetworkAclAssociation")
			})
			f.on("DeleteNetworkAcl", func(n int, form url.Values) (int, string) {
				assert.True(t, cleanup)
				assert.Equal(t, "acl-new", form.Get("NetworkAclId"))
				if mode == "cleanup failure" {
					return 400, ec2ErrorXML("CleanupFailure", "failed")
				}
				if n == 1 {
					return 400, ec2ErrorXML("DependencyViolation", "retry")
				}
				return 400, ec2ErrorXML(networkACLNotFound, "already gone")
			})
			c, _ := networkACLTestClient(t, f)
			options := c.ec2.Options()
			base := options.HTTPClient
			var deadlines []time.Time
			options.HTTPClient = networkACLHTTPClient{base: base, before: func(req *http.Request) {
				if mode == "visibility" {
					_, cleanup = req.Context().Deadline()
				}
				if !cleanup {
					return
				}
				assert.NoError(t, req.Context().Err())
				deadline, ok := req.Context().Deadline()
				assert.True(t, ok)
				assert.InDelta(t, 120, time.Until(deadline).Seconds(), 2)
				deadlines = append(deadlines, deadline)
			}}
			c.ec2 = ec2.New(options)
			rule := NetworkACLRule{RuleAction: "allow", Protocol: "tcp", RuleNumber: 10}
			second := rule
			second.RuleNumber = 20
			r := NetworkACLResource{VpcId: "vpc-desired",
				Egress: &[]NetworkACLRule{rule, second}, Ingress: &[]NetworkACLRule{rule, second},
				SubnetIds: &[]string{"subnet-a", "subnet-b"}}
			prepared, err := r.prepareRequest("fresh-token")
			require.NoError(t, err)
			output, err := r.create(ctx, c, prepared)
			if mode == "success" {
				require.NoError(t, err)
				require.NotNil(t, output)
				assert.Empty(t, f.sent("DeleteNetworkAcl"))
				return
			}
			require.Error(t, err)
			assert.Nil(t, output)
			if !accepted {
				assert.Equal(t, []string{"CreateNetworkAcl"}, peeringActions(f))
				return
			}
			assert.NotEmpty(t, deadlines)
			for _, deadline := range deadlines {
				assert.Equal(t, deadlines[0], deadline)
			}
			actions := peeringActions(f)
			assert.Equal(t, "DeleteNetworkAcl", actions[len(actions)-1])
			assert.Contains(t, f.sent("ReplaceNetworkAclAssociation"),
				peeringForm("ReplaceNetworkAclAssociation", url.Values{
					"NetworkAclId": {"acl-default"}, "AssociationId": {"assoc-external"}}))
			if mode == "cleanup failure" {
				require.ErrorContains(t, err, "EntryFailure")
				require.ErrorContains(t, err, "CleanupFailure")
				require.ErrorContains(t, err, "compensating delete network ACL acl-new")
				var joined interface{ Unwrap() []error }
				require.ErrorAs(t, err, &joined)
				parts := joined.Unwrap()
				var original, compensating smithy.APIError
				require.ErrorAs(t, parts[0], &original)
				require.ErrorAs(t, parts[1], &compensating)
				assert.Equal(t, "EntryFailure", original.ErrorCode())
				assert.Equal(t, "CleanupFailure", compensating.ErrorCode())
				assert.ErrorIs(t, err, original)
				assert.ErrorIs(t, err, compensating)
			}
			if mode == "visibility" {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			}
			if mode == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}

func TestNetworkACLAssociationRetryAndTolerance(t *testing.T) {
	for _, mode := range []string{"retry captured", "create missing", "update missing",
		"restore missing", "restore terminal", "associate terminal"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeEC2(t)
			f.on("DescribeNetworkAcls", func(_ int, form url.Values) (int, string) {
				if mode == "create missing" || mode == "update missing" {
					return networkACLPage("", "")
				}
				return networkACLPage(networkACLItem("acl-default", "",
					networkACLAssociation("subnet-a", "captured")), "")
			})
			f.on("ReplaceNetworkAclAssociation", func(n int, form url.Values) (int, string) {
				assert.Equal(t, "captured", form.Get("AssociationId"))
				if mode == "restore terminal" || mode == "associate terminal" {
					return 400, ec2ErrorXML("Terminal", "failed")
				}
				if n == 1 {
					return 400, ec2ErrorXML(networkACLAssociationNotFound, "retry")
				}
				return peeringSuccess("ReplaceNetworkAclAssociation")
			})
			c, clock := networkACLTestClient(t, f)
			var err error
			if mode == "restore missing" || mode == "restore terminal" {
				err = c.restore(t.Context(), "vpc-remote", ec2types.NetworkAclAssociation{
					SubnetId: aws.String("subnet-a"), NetworkAclAssociationId: aws.String("captured")})
			} else {
				err = c.associate(t.Context(), "acl-new", "subnet-a", mode == "update missing")
			}
			switch mode {
			case "create missing":
				require.ErrorIs(t, err, runtime.ErrNotFound)
			case "restore terminal", "associate terminal":
				require.ErrorContains(t, err, "Terminal")
			default:
				require.NoError(t, err)
			}
			if mode == "retry captured" {
				assert.Equal(t, []time.Duration{500 * time.Millisecond}, clock.waits)
				assert.Equal(t, []url.Values{peeringForm("ReplaceNetworkAclAssociation", url.Values{
					"AssociationId": {"captured"}, "NetworkAclId": {"acl-new"}}),
					peeringForm("ReplaceNetworkAclAssociation", url.Values{
						"AssociationId": {"captured"}, "NetworkAclId": {"acl-new"}})},
					f.sent("ReplaceNetworkAclAssociation"))
				assert.Equal(t, []string{"DescribeNetworkAcls", "ReplaceNetworkAclAssociation",
					"ReplaceNetworkAclAssociation"}, peeringActions(f))
			} else {
				assert.Empty(t, clock.waits)
			}
		})
	}
}
