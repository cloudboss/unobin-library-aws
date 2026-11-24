package ec2

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type NetworkACLResource struct {
	VpcId     string             `ub:"vpc-id"`
	Ingress   *[]NetworkACLRule  `ub:"ingress"`
	Egress    *[]NetworkACLRule  `ub:"egress"`
	SubnetIds *[]string          `ub:"subnet-ids"`
	Tags      *map[string]string `ub:"tags"`
}

type NetworkACLRule struct {
	RuleAction    string  `ub:"rule-action"`
	Protocol      string  `ub:"protocol"`
	RuleNumber    int64   `ub:"rule-number"`
	FromPort      int64   `ub:"from-port"`
	ToPort        int64   `ub:"to-port"`
	CidrBlock     *string `ub:"cidr-block"`
	Ipv6CidrBlock *string `ub:"ipv6-cidr-block"`
	IcmpType      *int64  `ub:"icmp-type"`
	IcmpCode      *int64  `ub:"icmp-code"`
}

type NetworkACLResourceOutput struct {
	NetworkACLId string            `ub:"network-acl-id"`
	VpcId        string            `ub:"vpc-id"`
	OwnerId      string            `ub:"owner-id"`
	Arn          string            `ub:"arn"`
	Ingress      []NetworkACLRule  `ub:"ingress"`
	Egress       []NetworkACLRule  `ub:"egress"`
	SubnetIds    []string          `ub:"subnet-ids"`
	Tags         map[string]string `ub:"tags"`
}

type networkACLPrior = runtime.Prior[NetworkACLResource, *NetworkACLResourceOutput, *awsCfg]

type networkACLRequests struct {
	create  *ec2.CreateNetworkAclInput
	ingress []ec2.CreateNetworkAclEntryInput
	egress  []ec2.CreateNetworkAclEntryInput
}

func (r *NetworkACLResource) prepareRequest(token string) (*networkACLRequests, error) {
	tags, _ := tagsync.Diff(nil, ptr.Value(r.Tags))
	prepared := &networkACLRequests{create: &ec2.CreateNetworkAclInput{
		VpcId: aws.String(r.VpcId), ClientToken: aws.String(token),
		TagSpecifications: tagSpecifications(ec2types.ResourceTypeNetworkAcl, tags),
	}}
	for _, direction := range []struct {
		name   string
		rules  *[]NetworkACLRule
		output *[]ec2.CreateNetworkAclEntryInput
		egress bool
	}{
		{"ingress", r.Ingress, &prepared.ingress, false},
		{"egress", r.Egress, &prepared.egress, true},
	} {
		for _, rule := range ptr.Value(direction.rules) {
			protocol, err := networkACLProtocol(rule.Protocol)
			if err != nil {
				return nil, fmt.Errorf("%s rule %d: %w", direction.name, rule.RuleNumber, err)
			}
			for _, cidr := range []struct {
				value *string
				ipv6  bool
			}{
				{rule.CidrBlock, false}, {rule.Ipv6CidrBlock, true},
			} {
				if cidr.value == nil {
					continue
				}
				ip, network, err := net.ParseCIDR(*cidr.value)
				if err != nil || !ip.Equal(network.IP) || (ip.To4() == nil) != cidr.ipv6 {
					return nil, fmt.Errorf("%s rule %d: invalid network CIDR %q",
						direction.name, rule.RuleNumber, *cidr.value)
				}
			}
			entry := ec2.CreateNetworkAclEntryInput{
				Egress: aws.Bool(direction.egress), Protocol: aws.String(strconv.Itoa(protocol)),
				RuleAction: ec2types.RuleAction(strings.ToLower(rule.RuleAction)),
				RuleNumber: aws.Int32(int32(rule.RuleNumber)),
				CidrBlock:  rule.CidrBlock, Ipv6CidrBlock: rule.Ipv6CidrBlock,
				PortRange: &ec2types.PortRange{
					From: aws.Int32(int32(rule.FromPort)), To: aws.Int32(int32(rule.ToPort)),
				},
			}
			if protocol == 1 || protocol == 58 {
				entry.IcmpTypeCode = &ec2types.IcmpTypeCode{
					Type: aws.Int32(int32(aws.ToInt64(rule.IcmpType))),
					Code: aws.Int32(int32(aws.ToInt64(rule.IcmpCode))),
				}
			}
			if !slices.ContainsFunc(*direction.output, func(prior ec2.CreateNetworkAclEntryInput) bool {
				return reflect.DeepEqual(prior, entry)
			}) {
				*direction.output = append(*direction.output, entry)
			}
		}
		slices.SortFunc(*direction.output, func(a, b ec2.CreateNetworkAclEntryInput) int {
			return int(aws.ToInt32(a.RuleNumber) - aws.ToInt32(b.RuleNumber))
		})
	}
	return prepared, nil
}

func (r *NetworkACLResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*NetworkACLResourceOutput, error) {
	prepared, err := r.prepareRequest(rand.Text())
	if err != nil {
		return nil, err
	}
	client, err := newNetworkACLClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, prepared)
}

func (r *NetworkACLResource) Read(
	ctx context.Context, cfg *awsCfg, prior networkACLPrior,
) (*NetworkACLResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.NetworkACLId == "" {
		return nil, runtime.ErrNotFound
	}
	client, err := newNetworkACLClient(ctx, networkACLPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	return client.read(ctx, prior.Outputs.NetworkACLId)
}

func (r *NetworkACLResource) Update(
	ctx context.Context, cfg *awsCfg, prior networkACLPrior,
) (*NetworkACLResourceOutput, error) {
	prepared, err := r.prepareRequest("")
	if err != nil {
		return nil, err
	}
	if prior.Outputs == nil || prior.Outputs.NetworkACLId == "" {
		return nil, errors.New("update network ACL requires a saved network ACL ID")
	}
	client, err := newNetworkACLClient(ctx, networkACLPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior, prepared)
}

func (r *NetworkACLResource) Delete(
	ctx context.Context, cfg *awsCfg, prior networkACLPrior,
) error {
	if prior.Outputs == nil || prior.Outputs.NetworkACLId == "" {
		return nil
	}
	client, err := newNetworkACLClient(ctx, networkACLPriorConfig(cfg, prior))
	if err != nil {
		return err
	}
	return client.delete(ctx, prior.Outputs.NetworkACLId, prior.Outputs.VpcId)
}

func networkACLPriorConfig(cfg *awsCfg, prior networkACLPrior) *awsCfg {
	if prior.Configuration != nil {
		return prior.Configuration
	}
	return cfg
}

func (r NetworkACLResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.VpcId, nil)).
			Message("vpc-id must be a non-null string"),
		constraint.ForEach(r.Ingress, func(rule NetworkACLRule) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(
					constraint.NotEquals(rule.RuleAction, nil),
					constraint.NotEquals(rule.Protocol, nil),
					constraint.NotEquals(rule.RuleNumber, nil),
					constraint.NotEquals(rule.FromPort, nil),
					constraint.NotEquals(rule.ToPort, nil),
				).Message("ingress rule fields must be non-null"),
				constraint.Must(constraint.IsMatch(rule.RuleAction, "(?i)^(allow|deny)$")).
					Message("ingress rule-action must be allow or deny"),
				constraint.Must(
					constraint.AtLeast(rule.RuleNumber, 1), constraint.AtMost(rule.RuleNumber, 32766),
					constraint.AtLeast(rule.FromPort, 0), constraint.AtMost(rule.FromPort, 65535),
					constraint.AtLeast(rule.ToPort, 0), constraint.AtMost(rule.ToPort, 65535),
				).Message("ingress rule number or port is outside its bounds"),
				constraint.When(constraint.IsMatch(rule.Protocol, "^(all|-0*1)$")).Require(
					constraint.Equals(rule.FromPort, 0), constraint.Equals(rule.ToPort, 0),
				).Message("all protocols require zero ports"),
			}
		}),
		constraint.ForEach(r.Egress, func(rule NetworkACLRule) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(
					constraint.NotEquals(rule.RuleAction, nil),
					constraint.NotEquals(rule.Protocol, nil),
					constraint.NotEquals(rule.RuleNumber, nil),
					constraint.NotEquals(rule.FromPort, nil),
					constraint.NotEquals(rule.ToPort, nil),
				).Message("egress rule fields must be non-null"),
				constraint.Must(constraint.IsMatch(rule.RuleAction, "(?i)^(allow|deny)$")).
					Message("egress rule-action must be allow or deny"),
				constraint.Must(
					constraint.AtLeast(rule.RuleNumber, 1), constraint.AtMost(rule.RuleNumber, 32766),
					constraint.AtLeast(rule.FromPort, 0), constraint.AtMost(rule.FromPort, 65535),
					constraint.AtLeast(rule.ToPort, 0), constraint.AtMost(rule.ToPort, 65535),
				).Message("egress rule number or port is outside its bounds"),
				constraint.When(constraint.IsMatch(rule.Protocol, "^(all|-0*1)$")).Require(
					constraint.Equals(rule.FromPort, 0), constraint.Equals(rule.ToPort, 0),
				).Message("all protocols require zero ports"),
			}
		}),
	}
}

func (*NetworkACLResource) ResourceDefinition() runtime.ResourceDefinition[
	NetworkACLResource, *NetworkACLResourceOutput, *awsCfg,
] {
	return runtime.ResourceDefinition[NetworkACLResource, *NetworkACLResourceOutput, *awsCfg]{
		SchemaVersion: 1,
		Validate: func(_ context.Context, r NetworkACLResource, _ *awsCfg) error {
			_, err := r.prepareRequest("")
			return err
		},
		Replace: runtime.Replacement[NetworkACLResource, *NetworkACLResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[NetworkACLResource]{
				runtime.InputField(func(r *NetworkACLResource) *string { return &r.VpcId }),
			},
		},
		Equality: []runtime.InputEqualityRule[NetworkACLResource]{
			runtime.EqualBy(runtime.InputField(
				func(r *NetworkACLResource) **[]NetworkACLRule { return &r.Ingress },
			), networkACLRulesEqual),
			runtime.EqualBy(runtime.InputField(
				func(r *NetworkACLResource) **[]NetworkACLRule { return &r.Egress },
			), networkACLRulesEqual),
			runtime.EqualBy(runtime.InputField(
				func(r *NetworkACLResource) **[]string { return &r.SubnetIds },
			), networkACLSubnetsEqual),
		},
		StableID: func(_ NetworkACLResource, out *NetworkACLResourceOutput) (string, error) {
			if out == nil {
				return "", runtime.ErrNotFound
			}
			return out.NetworkACLId, nil
		},
	}
}

func networkACLRulesEqual(prior, desired *[]NetworkACLRule) bool {
	if desired == nil {
		return true
	}
	if prior == nil {
		return false
	}
	a, err := (&NetworkACLResource{Ingress: prior}).prepareRequest("")
	if err != nil {
		return false
	}
	b, err := (&NetworkACLResource{Ingress: desired}).prepareRequest("")
	if err != nil || len(a.ingress) != len(b.ingress) {
		return false
	}
	for _, entry := range a.ingress {
		if !networkACLHasEntry(b.ingress, entry) {
			return false
		}
	}
	return true
}

func networkACLSubnetsEqual(prior, desired *[]string) bool {
	return desired == nil || slices.Equal(networkACLSubnets(ptr.Value(prior)),
		networkACLSubnets(*desired))
}

func networkACLSubnets(ids []string) []string {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	return slices.Compact(ids)
}

func networkACLProtocol(value string) (int, error) {
	names := strings.Fields("hopopt icmp igmp ggp ipv4 st tcp cbt egp igp " +
		"bbn-rcc-mon nvp-ii pup argus emcon xnet chaos udp mux dcn-meas " +
		"hmp prm xns-idp trunk-1 trunk-2 leaf-1 leaf-2 rdp irtp iso-tp4 " +
		"netblt mfe-nsp merit-inp dccp 3pc idpr xtp ddp idpr-cmtp tp++ " +
		"il ipv6 sdrp ipv6-route ipv6-frag idrp rsvp gre dsr bna " +
		"esp ah i-nlsp swipe narp mobile tlsp RESERVED ipv6-icmp ipv6-nonxt " +
		"ipv6-opts 61 cftp 63 sat-expak kryptolan rvd ippc 68 sat-mon " +
		"visa ipcv cpnx cphb wsn pvp br-sat-mon sun-nd wb-mon wb-expak " +
		"iso-ip vmtp secure-vmtp vines ttp nsfnet-igp dgp tcf eigrp ospfigp " +
		"sprite-rpc larp mtp ax.25 ipip micp scc-sp etherip encap 99 " +
		"gmtp ifmp pnni pim aris scps qnx a/n ipcomp snp " +
		"compaq-peer ipx-in-ip vrrp pgm 114 l2tp dd iatp stp srp " +
		"uti smp sm ptp isis-over-ipv4 fire crtp crudp sscopmce iplt " +
		"sps pipe sctp fc rsvp-e2e-ignore mobility-header udplite mpls-in-ip manet hip " +
		"shim6 wesp rohc")
	if value == "all" {
		return -1, nil
	}
	if n, err := strconv.Atoi(value); err == nil {
		if n == -1 || n >= 0 && n <= 142 && n != 57 || n == 253 || n == 254 {
			return n, nil
		}
	} else if n := slices.Index(names, value); n >= 0 && n != 57 {
		return n, nil
	}
	return 0, fmt.Errorf("invalid network ACL protocol %q", value)
}
