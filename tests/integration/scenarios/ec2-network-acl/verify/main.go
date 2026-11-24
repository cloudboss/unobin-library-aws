package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"
)

type identities struct {
	Vpc     string
	ACLs    []string
	Subnets []string
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := ec2.NewFromConfig(cfg)
	phase := os.Getenv("VERIFY_PHASE")
	path := filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "network-acl-ids.json")
	if phase == "destroyed" {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var ids identities
		if err := json.Unmarshal(body, &ids); err != nil {
			return err
		}
		for _, id := range ids.ACLs {
			result, err := client.DescribeNetworkAcls(ctx,
				&ec2.DescribeNetworkAclsInput{NetworkAclIds: []string{id}})
			if !absent(err, "InvalidNetworkAclID.NotFound") {
				return err
			}
			if err == nil && len(result.NetworkAcls) != 0 {
				return fmt.Errorf("network ACL %s remains after destruction", id)
			}
		}
		vpcs, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{ids.Vpc}})
		if !absent(err, "InvalidVpcID.NotFound") {
			return err
		}
		if err == nil && len(vpcs.Vpcs) != 0 {
			return fmt.Errorf("VPC %s remains", ids.Vpc)
		}
		for _, id := range ids.Subnets {
			out, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{SubnetIds: []string{id}})
			if !absent(err, "InvalidSubnetID.NotFound") {
				return err
			}
			if err == nil && len(out.Subnets) != 0 {
				return fmt.Errorf("subnet %s remains", id)
			}
		}
		return nil
	}
	if phase != "applied" && phase != "updated" {
		return fmt.Errorf("unknown phase %q", phase)
	}
	page, err := client.DescribeNetworkAcls(ctx, &ec2.DescribeNetworkAclsInput{
		Filters: []ec2types.Filter{{Name: aws.String("tag:unobin"),
			Values: []string{"ec2-network-acl-it"}}},
	})
	if err != nil {
		return err
	}
	byRole := map[string]ec2types.NetworkAcl{}
	for _, acl := range page.NetworkAcls {
		byRole[tag(acl.Tags, "role")] = acl
	}
	stage := "initial"
	if phase == "updated" {
		stage = "updated"
	}
	for _, role := range []string{"managed", "cleared", "unmanaged"} {
		acl, ok := byRole[role]
		if !ok || tag(acl.Tags, "stage") != stage {
			return fmt.Errorf("missing %s ACL or wrong stage: %+v", role, acl)
		}
	}
	managed, cleared, unmanaged := byRole["managed"], byRole["cleared"], byRole["unmanaged"]
	vpc := aws.ToString(managed.VpcId)
	subnets, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpc}}},
	})
	if err != nil {
		return err
	}
	byCIDR := map[string]string{}
	for _, subnet := range subnets.Subnets {
		byCIDR[aws.ToString(subnet.CidrBlock)] = aws.ToString(subnet.SubnetId)
	}
	first, second, third := byCIDR["10.71.1.0/24"], byCIDR["10.71.2.0/24"], byCIDR["10.71.3.0/24"]
	if first == "" || second == "" || third == "" {
		return fmt.Errorf("missing owned subnets: %v", byCIDR)
	}
	port := int32(80)
	associated := first
	if phase == "updated" {
		port, associated = 443, second
	}
	if err := checkRules(managed, []ec2types.NetworkAclEntry{
		entry(10, false, "allow", "6", port, nil), entry(20, true, "allow", "-1", 0, nil),
	}); err != nil {
		return err
	}
	if !slices.Equal(associationSubnets(managed), []string{associated}) {
		return fmt.Errorf("managed ACL associations: %+v", managed.Associations)
	}
	if err := checkRules(unmanaged, []ec2types.NetworkAclEntry{
		entry(40, false, "allow", "1", 0,
			&ec2types.IcmpTypeCode{Type: aws.Int32(-1), Code: aws.Int32(-1)}),
	}); err != nil {
		return err
	}
	if phase == "applied" {
		if err := checkRules(cleared, []ec2types.NetworkAclEntry{
			entry(30, false, "deny", "6", 22, nil),
		}); err != nil {
			return err
		}
		if !slices.Equal(associationSubnets(cleared), []string{third}) {
			return fmt.Errorf("cleared ACL initial associations: %+v", cleared.Associations)
		}
		ids := identities{Vpc: vpc, Subnets: []string{first, second, third}, ACLs: []string{
			aws.ToString(managed.NetworkAclId), aws.ToString(cleared.NetworkAclId),
			aws.ToString(unmanaged.NetworkAclId)}}
		body, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		return os.WriteFile(path, body, 0o600)
	}
	if err := checkRules(cleared, nil); err != nil {
		return err
	}
	if len(cleared.Associations) != 0 {
		return fmt.Errorf("cleared subnet association remains")
	}
	defaults, err := client.DescribeNetworkAcls(ctx, &ec2.DescribeNetworkAclsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpc}},
			{Name: aws.String("default"), Values: []string{"true"}},
		},
	})
	if err != nil {
		return err
	}
	if len(defaults.NetworkAcls) != 1 {
		return fmt.Errorf("expected one default ACL")
	}
	if !slices.Contains(associationSubnets(defaults.NetworkAcls[0]), first) ||
		!slices.Contains(associationSubnets(defaults.NetworkAcls[0]), third) {
		return fmt.Errorf("removed associations were not restored to the default ACL")
	}
	for _, association := range defaults.NetworkAcls[0].Associations {
		if aws.ToString(association.SubnetId) != third {
			continue
		}
		_, err := client.ReplaceNetworkAclAssociation(ctx, &ec2.ReplaceNetworkAclAssociationInput{
			AssociationId: association.NetworkAclAssociationId, NetworkAclId: unmanaged.NetworkAclId,
		})
		return err
	}
	return fmt.Errorf("third subnet has no default association")
}

func absent(err error, code string) bool {
	if err == nil {
		return true
	}
	var api smithy.APIError
	return errors.As(err, &api) && api.ErrorCode() == code
}

func tag(tags []ec2types.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}

func associationSubnets(acl ec2types.NetworkAcl) []string {
	var ids []string
	for _, association := range acl.Associations {
		ids = append(ids, aws.ToString(association.SubnetId))
	}
	slices.Sort(ids)
	return ids
}

func entry(number int32, egress bool, action, protocol string, port int32,
	icmp *ec2types.IcmpTypeCode,
) ec2types.NetworkAclEntry {
	cidr := "10.0.0.0/8"
	if egress {
		cidr = "0.0.0.0/0"
	}
	return ec2types.NetworkAclEntry{RuleNumber: aws.Int32(number), Egress: aws.Bool(egress),
		RuleAction: ec2types.RuleAction(action), Protocol: aws.String(protocol),
		CidrBlock: aws.String(cidr), IcmpTypeCode: icmp,
		PortRange: &ec2types.PortRange{From: aws.Int32(port), To: aws.Int32(port)}}
}

func checkRules(acl ec2types.NetworkAcl, wanted []ec2types.NetworkAclEntry) error {
	var got []ec2types.NetworkAclEntry
	for _, rule := range acl.Entries {
		if n := aws.ToInt32(rule.RuleNumber); n == 32767 || n == 32768 {
			continue
		}
		got = append(got, rule)
	}
	if len(got) != len(wanted) {
		return fmt.Errorf("ACL %s rules: %+v", *acl.NetworkAclId, got)
	}
	for _, want := range wanted {
		if !slices.ContainsFunc(got, func(rule ec2types.NetworkAclEntry) bool {
			if aws.ToInt32(rule.RuleNumber) != aws.ToInt32(want.RuleNumber) ||
				aws.ToBool(rule.Egress) != aws.ToBool(want.Egress) || rule.RuleAction != want.RuleAction ||
				aws.ToString(rule.Protocol) != aws.ToString(want.Protocol) ||
				aws.ToString(rule.CidrBlock) != aws.ToString(want.CidrBlock) {
				return false
			}
			if want.IcmpTypeCode != nil {
				return rule.IcmpTypeCode != nil &&
					aws.ToInt32(rule.IcmpTypeCode.Type) == aws.ToInt32(want.IcmpTypeCode.Type) &&
					aws.ToInt32(rule.IcmpTypeCode.Code) == aws.ToInt32(want.IcmpTypeCode.Code)
			}
			if aws.ToString(want.Protocol) == "-1" {
				return true
			}
			return rule.PortRange != nil &&
				aws.ToInt32(rule.PortRange.From) == aws.ToInt32(want.PortRange.From) &&
				aws.ToInt32(rule.PortRange.To) == aws.ToInt32(want.PortRange.To)
		}) {
			return fmt.Errorf("ACL %s lacks rule %+v; got %+v", *acl.NetworkAclId, want, got)
		}
	}
	return nil
}
