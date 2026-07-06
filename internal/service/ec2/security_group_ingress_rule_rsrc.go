package ec2

import (
	"context"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// SecurityGroupIngressRuleResource is one inbound rule on a VPC security group. It
// authorizes traffic from a single source -- an IPv4 or IPv6 CIDR, a prefix
// list, or another security group -- for a protocol and optional port range.
// Each property maps to the AWS SDK field that holds it; the description
// rides inside the chosen source rather than as a standalone field.
type SecurityGroupIngressRuleResource struct {
	SecurityGroupId           string             `ub:"security-group-id"`
	IpProtocol                string             `ub:"ip-protocol"`
	FromPort                  *int64             `ub:"from-port"`
	ToPort                    *int64             `ub:"to-port"`
	CidrIpv4                  *string            `ub:"cidr-ipv4"`
	CidrIpv6                  *string            `ub:"cidr-ipv6"`
	PrefixListId              *string            `ub:"prefix-list-id"`
	ReferencedSecurityGroupId *string            `ub:"referenced-security-group-id"`
	Description               *string            `ub:"description"`
	Tags                      *map[string]string `ub:"tags"`
}

// SecurityGroupIngressRuleResourceOutput holds the values EC2 computes for the rule:
// its server-assigned id and the ARN composed from that id, the region, the
// partition, and the owner account id.
type SecurityGroupIngressRuleResourceOutput struct {
	SecurityGroupRuleId string `ub:"security-group-rule-id"`
	Arn                 string `ub:"arn"`
}

// rule views the resource as the direction-independent sgRule the shared
// lifecycle functions act on.
func (r *SecurityGroupIngressRuleResource) rule() sgRule {
	return sgRule{
		SecurityGroupId:           r.SecurityGroupId,
		IpProtocol:                r.IpProtocol,
		FromPort:                  r.FromPort,
		ToPort:                    r.ToPort,
		CidrIpv4:                  r.CidrIpv4,
		CidrIpv6:                  r.CidrIpv6,
		PrefixListId:              r.PrefixListId,
		ReferencedSecurityGroupId: r.ReferencedSecurityGroupId,
		Description:               r.Description,
		Tags:                      ptr.Value(r.Tags),
	}
}

func (r *SecurityGroupIngressRuleResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the properties EC2 cannot change in place. The security
// group a rule belongs to and the source it allows are fixed at create; a
// change to any of them recreates the rule. The protocol, ports, and
// description update in place.
func (r *SecurityGroupIngressRuleResource) ReplaceFields() []string {
	return []string{
		"security-group-id",
		"cidr-ipv4",
		"cidr-ipv6",
		"prefix-list-id",
		"referenced-security-group-id",
	}
}

// Constraints declares the rules EC2 enforces on an ingress rule's inputs. A
// rule allows exactly one source, and a port number, when given, is within the
// range EC2 accepts, where -1 means all ports or all ICMP types and codes.
func (r SecurityGroupIngressRuleResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.CidrIpv4, r.CidrIpv6, r.PrefixListId,
			r.ReferencedSecurityGroupId),
		constraint.When(constraint.Present(r.FromPort)).
			Require(constraint.AtLeast(r.FromPort, -1), constraint.AtMost(r.FromPort, 65535)).
			Message("from-port must be between -1 and 65535"),
		constraint.When(constraint.Present(r.ToPort)).
			Require(constraint.AtLeast(r.ToPort, -1), constraint.AtMost(r.ToPort, 65535)).
			Message("to-port must be between -1 and 65535"),
	}
}

func (r *SecurityGroupIngressRuleResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*SecurityGroupIngressRuleResourceOutput, error) {
	ruleID, arn, err := sgRuleCreate(ctx, cfg, r.rule(), false)
	if err != nil {
		return nil, err
	}
	return &SecurityGroupIngressRuleResourceOutput{SecurityGroupRuleId: ruleID, Arn: arn}, nil
}

func (r *SecurityGroupIngressRuleResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *SecurityGroupIngressRuleResourceOutput,
) (*SecurityGroupIngressRuleResourceOutput, error) {
	if err := sgRuleRead(ctx, cfg, prior.SecurityGroupRuleId, false); err != nil {
		return nil, err
	}
	return prior, nil
}

func (r *SecurityGroupIngressRuleResource) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[SecurityGroupIngressRuleResource, *SecurityGroupIngressRuleResourceOutput],
) (*SecurityGroupIngressRuleResourceOutput, error) {
	err := sgRuleUpdate(ctx, cfg, r.rule(), prior.Inputs.rule(),
		prior.Outputs.SecurityGroupRuleId)
	if err != nil {
		return nil, err
	}
	return prior.Outputs, nil
}

func (r *SecurityGroupIngressRuleResource) Delete(
	ctx context.Context, cfg *awsCfg, prior *SecurityGroupIngressRuleResourceOutput) error {
	return sgRuleDelete(ctx, cfg, prior.SecurityGroupRuleId, false)
}
