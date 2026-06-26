// verify checks the VPC group the scenario applied against the phase named in
// the VERIFY_PHASE environment variable. The test driver passes no plan outputs
// in, so resources are found by stable attributes: the VPC by its CIDR, the
// security group by its name, and each rule by reading the rules of that group.
// It only reads cloud state: applied requires the VPC, the security group, its
// marker tag, and the ssh ingress and https egress rules to be present;
// destroyed requires the VPC and the security group to be gone. Tearing the
// group down is the destroy plan's job, not the verifier's.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const (
	cidr      = "10.42.0.0/16"
	sgName    = "unobin-it-sg"
	sshPort   = 22
	httpsPort = 443
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := ec2.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *ec2.Client) error {
	vpcID, err := findVPC(ctx, client)
	if err != nil {
		return err
	}
	if vpcID == "" {
		return fmt.Errorf("no vpc with cidr %s", cidr)
	}

	sg, err := findSecurityGroup(ctx, client)
	if err != nil {
		return err
	}
	if sg == nil {
		return fmt.Errorf("no security group named %s", sgName)
	}
	if aws.ToString(sg.VpcId) != vpcID {
		return fmt.Errorf("security group %s is in vpc %s, want %s",
			sgName, aws.ToString(sg.VpcId), vpcID)
	}
	if !securityGroupHasTag(sg, "unobin", "ec2-it") {
		return fmt.Errorf("security group %s is missing its unobin marker tag", sgName)
	}
	sgID := aws.ToString(sg.GroupId)

	if err := requireRule(ctx, client, sgID, "ssh ingress", false, "tcp", sshPort, cidr); err != nil {
		return err
	}
	if err := requireRule(ctx, client, sgID, "https egress", true, "tcp", httpsPort,
		"0.0.0.0/0"); err != nil {
		return err
	}

	present, err := defaultEgressPresent(ctx, client, sgID)
	if err != nil {
		return err
	}
	if present {
		return fmt.Errorf("security group %s still has the default allow-all egress rule", sgID)
	}

	fmt.Printf("ok: vpc %s, security group %s, and its rules are present\n", vpcID, sgID)
	return nil
}

func verifyDestroyed(ctx context.Context, client *ec2.Client) error {
	vpcID, err := findVPC(ctx, client)
	if err != nil {
		return err
	}
	if vpcID != "" {
		return fmt.Errorf("vpc %s with cidr %s still exists", vpcID, cidr)
	}

	sg, err := findSecurityGroup(ctx, client)
	if err != nil {
		return err
	}
	if sg != nil {
		return fmt.Errorf("security group %s still exists", aws.ToString(sg.GroupId))
	}

	fmt.Printf("ok: no vpc with cidr %s and no security group named %s remain\n",
		cidr, sgName)
	return nil
}

// findVPC returns the id of the VPC with the scenario's CIDR, or the empty
// string when none exists.
func findVPC(ctx context.Context, client *ec2.Client) (string, error) {
	out, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{{Name: aws.String("cidr"), Values: []string{cidr}}},
	})
	if err != nil {
		return "", fmt.Errorf("describe vpcs: %w", err)
	}
	if len(out.Vpcs) == 0 {
		return "", nil
	}
	return aws.ToString(out.Vpcs[0].VpcId), nil
}

// findSecurityGroup returns the security group with the scenario's name, or nil
// when none exists.
func findSecurityGroup(ctx context.Context, client *ec2.Client) (*ec2types.SecurityGroup, error) {
	out, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{{Name: aws.String("group-name"), Values: []string{sgName}}},
	})
	if err != nil {
		return nil, fmt.Errorf("describe security groups: %w", err)
	}
	if len(out.SecurityGroups) == 0 {
		return nil, nil
	}
	return &out.SecurityGroups[0], nil
}

func securityGroupHasTag(group *ec2types.SecurityGroup, key, value string) bool {
	for _, tag := range group.Tags {
		if aws.ToString(tag.Key) == key && aws.ToString(tag.Value) == value {
			return true
		}
	}
	return false
}

// requireRule returns an error unless the group has a rule matching the given
// direction, protocol, port range, and IPv4 CIDR. label names the rule for the
// error message.
func requireRule(
	ctx context.Context, client *ec2.Client, sgID, label string,
	egress bool, protocol string, port int32, cidrBlock string,
) error {
	pager := ec2.NewDescribeSecurityGroupRulesPaginator(client,
		&ec2.DescribeSecurityGroupRulesInput{
			Filters: []ec2types.Filter{{Name: aws.String("group-id"), Values: []string{sgID}}},
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("describe security group rules: %w", err)
		}
		for _, rule := range page.SecurityGroupRules {
			if aws.ToBool(rule.IsEgress) == egress &&
				aws.ToString(rule.IpProtocol) == protocol &&
				aws.ToInt32(rule.FromPort) == port &&
				aws.ToInt32(rule.ToPort) == port &&
				aws.ToString(rule.CidrIpv4) == cidrBlock {
				return nil
			}
		}
	}
	return fmt.Errorf("security group %s has no %s rule", sgID, label)
}

// defaultEgressPresent reports whether the group still has AWS's default
// allow-all egress rule (all protocols to 0.0.0.0/0). The security group
// resource revokes it at create, so applied state should not have it.
func defaultEgressPresent(ctx context.Context, client *ec2.Client, sgID string) (bool, error) {
	pager := ec2.NewDescribeSecurityGroupRulesPaginator(client,
		&ec2.DescribeSecurityGroupRulesInput{
			Filters: []ec2types.Filter{{Name: aws.String("group-id"), Values: []string{sgID}}},
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return false, fmt.Errorf("describe security group rules: %w", err)
		}
		for _, rule := range page.SecurityGroupRules {
			if aws.ToBool(rule.IsEgress) &&
				aws.ToString(rule.IpProtocol) == "-1" &&
				aws.ToString(rule.CidrIpv4) == "0.0.0.0/0" {
				return true, nil
			}
		}
	}
	return false, nil
}
