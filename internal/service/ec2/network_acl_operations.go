package ec2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

const networkACLNotFound = "InvalidNetworkAclID.NotFound"
const networkACLAssociationNotFound = "InvalidAssociationID.NotFound"

type networkACLClient struct {
	ec2     *ec2.Client
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
	timeout time.Duration
}

func newNetworkACLClient(ctx context.Context, cfg *awsCfg) (*networkACLClient, error) {
	sdk, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &networkACLClient{ec2: sdk, now: time.Now, sleep: networkACLSleep,
		timeout: 5 * time.Minute}, nil
}

func networkACLSleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *networkACLClient) retry(
	ctx context.Context, grace bool, retryable func(error) bool, operation func() error,
) error {
	deadline := c.now().Add(c.timeout)
	delay := 500 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := operation()
		if err == nil || !retryable(err) {
			return err
		}
		if !c.now().Before(deadline) {
			if !grace {
				return err
			}
			grace = false
		}
		if err := c.sleep(ctx, delay); err != nil {
			return err
		}
		delay = min(2*delay, 10*time.Second)
	}
}

func (r *NetworkACLResource) create(
	ctx context.Context, c *networkACLClient, requests *networkACLRequests,
) (*NetworkACLResourceOutput, error) {
	resp, err := c.ec2.CreateNetworkAcl(ctx, requests.create)
	if err != nil {
		return nil, fmt.Errorf("create network ACL: %w", err)
	}
	if resp == nil || resp.NetworkAcl == nil || aws.ToString(resp.NetworkAcl.NetworkAclId) == "" {
		return nil, errors.New("create network ACL returned no network ACL ID")
	}
	id := *resp.NetworkAcl.NetworkAclId
	fail := func(failure error) (*NetworkACLResourceOutput, error) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if err := c.delete(cleanup, id, aws.ToString(requests.create.VpcId)); err != nil {
			failure = errors.Join(failure, fmt.Errorf("compensating delete network ACL %s: %w", id, err))
		}
		return nil, failure
	}
	for _, entries := range [][]ec2.CreateNetworkAclEntryInput{requests.egress, requests.ingress} {
		for _, input := range entries {
			input.NetworkAclId = aws.String(id)
			if _, err := c.ec2.CreateNetworkAclEntry(ctx, &input); err != nil {
				return fail(fmt.Errorf("create network ACL %s rule %d: %w",
					id, aws.ToInt32(input.RuleNumber), err))
			}
		}
	}
	for _, subnet := range networkACLSubnets(ptr.Value(r.SubnetIds)) {
		if err := c.associate(ctx, id, subnet, false); err != nil {
			return fail(err)
		}
	}
	var output *NetworkACLResourceOutput
	err = c.retry(ctx, true, func(err error) bool { return errors.Is(err, runtime.ErrNotFound) },
		func() error { var err error; output, err = c.read(ctx, id); return err })
	if err != nil {
		return fail(err)
	}
	return output, nil
}

func (r *NetworkACLResource) update(
	ctx context.Context, c *networkACLClient, prior networkACLPrior, requests *networkACLRequests,
) (*NetworkACLResourceOutput, error) {
	id := prior.Outputs.NetworkACLId
	known := prior.Observed
	if known == nil {
		known = prior.Outputs
	}
	if err := syncEC2Tags(ctx, c.ec2, id, ptr.Value(r.Tags),
		func(context.Context) (map[string]string, error) { return known.Tags, nil }); err != nil {
		return nil, err
	}
	for _, direction := range []struct {
		managed bool
		known   []NetworkACLRule
		wanted  []ec2.CreateNetworkAclEntryInput
		egress  bool
	}{
		{r.Ingress != nil, known.Ingress, requests.ingress, false},
		{r.Egress != nil, known.Egress, requests.egress, true},
	} {
		if !direction.managed {
			continue
		}
		old, err := (&NetworkACLResource{Ingress: &direction.known}).prepareRequest("")
		if err != nil {
			return nil, fmt.Errorf("prepare observed network ACL %s rules: %w", id, err)
		}
		for i := range old.ingress {
			old.ingress[i].Egress = aws.Bool(direction.egress)
		}
		for _, entry := range old.ingress {
			if networkACLHasEntry(direction.wanted, entry) {
				continue
			}
			if aws.ToInt32(entry.RuleNumber) >= 32767 {
				continue
			}
			_, err := c.ec2.DeleteNetworkAclEntry(ctx, &ec2.DeleteNetworkAclEntryInput{
				NetworkAclId: aws.String(id), Egress: entry.Egress, RuleNumber: entry.RuleNumber,
			})
			if err != nil {
				return nil, fmt.Errorf("delete network ACL %s rule: %w", id, err)
			}
		}
		for _, entry := range direction.wanted {
			if networkACLHasEntry(old.ingress, entry) {
				continue
			}
			entry.NetworkAclId = aws.String(id)
			if _, err := c.ec2.CreateNetworkAclEntry(ctx, &entry); err != nil {
				return nil, fmt.Errorf("create network ACL %s rule: %w", id, err)
			}
		}
	}
	if r.SubnetIds != nil {
		wanted := networkACLSubnets(*r.SubnetIds)
		for _, subnet := range networkACLSubnets(known.SubnetIds) {
			if slices.Contains(wanted, subnet) {
				continue
			}
			acl, err := c.lookup(ctx, &ec2.DescribeNetworkAclsInput{Filters: []ec2types.Filter{
				{Name: aws.String("association.subnet-id"), Values: []string{subnet}},
			}}, "")
			if errors.Is(err, runtime.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, association := range acl.Associations {
				if aws.ToString(association.SubnetId) != subnet {
					continue
				}
				if err := c.restore(ctx, known.VpcId, association); err != nil {
					return nil, err
				}
			}
		}
		for _, subnet := range wanted {
			if slices.Contains(known.SubnetIds, subnet) {
				continue
			}
			if err := c.associate(ctx, id, subnet, true); err != nil {
				return nil, err
			}
		}
	}
	return c.read(ctx, id)
}

func networkACLHasEntry(entries []ec2.CreateNetworkAclEntryInput,
	entry ec2.CreateNetworkAclEntryInput,
) bool {
	return slices.ContainsFunc(entries, func(other ec2.CreateNetworkAclEntryInput) bool {
		return networkACLEntryEqual(other, entry)
	})
}

func networkACLEntryEqual(a, b ec2.CreateNetworkAclEntryInput) bool {
	return aws.ToInt32(a.RuleNumber) == aws.ToInt32(b.RuleNumber) &&
		aws.ToBool(a.Egress) == aws.ToBool(b.Egress) && a.RuleAction == b.RuleAction &&
		aws.ToString(a.Protocol) == aws.ToString(b.Protocol) &&
		aws.ToString(a.CidrBlock) == aws.ToString(b.CidrBlock) &&
		aws.ToString(a.Ipv6CidrBlock) == aws.ToString(b.Ipv6CidrBlock) &&
		aws.ToInt32(a.PortRange.From) == aws.ToInt32(b.PortRange.From) &&
		aws.ToInt32(a.PortRange.To) == aws.ToInt32(b.PortRange.To) &&
		networkACLICMPEqual(a.IcmpTypeCode, b.IcmpTypeCode)
}

func networkACLICMPEqual(a, b *ec2types.IcmpTypeCode) bool {
	if a == nil || b == nil {
		return a == b
	}
	return aws.ToInt32(a.Type) == aws.ToInt32(b.Type) && aws.ToInt32(a.Code) == aws.ToInt32(b.Code)
}

func (c *networkACLClient) lookup(
	ctx context.Context, input *ec2.DescribeNetworkAclsInput, id string,
) (*ec2types.NetworkAcl, error) {
	pages := ec2.NewDescribeNetworkAclsPaginator(c.ec2, input)
	var acls []ec2types.NetworkAcl
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if isNotFound(err, networkACLNotFound) {
			return nil, runtime.ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("describe network ACL %s: %w", id, err)
		}
		acls = append(acls, page.NetworkAcls...)
	}
	if len(acls) == 0 {
		return nil, runtime.ErrNotFound
	}
	if len(acls) != 1 {
		return nil, fmt.Errorf("describe network ACL %s returned multiple ACLs", id)
	}
	if id != "" && aws.ToString(acls[0].NetworkAclId) != id {
		return nil, runtime.ErrNotFound
	}
	return &acls[0], nil
}

func (c *networkACLClient) read(ctx context.Context, id string) (*NetworkACLResourceOutput, error) {
	acl, err := c.lookup(ctx, &ec2.DescribeNetworkAclsInput{NetworkAclIds: []string{id}}, id)
	if err != nil {
		return nil, err
	}
	if aws.ToString(acl.VpcId) == "" || aws.ToString(acl.OwnerId) == "" {
		return nil, fmt.Errorf("describe network ACL %s returned incomplete identity", id)
	}
	out := &NetworkACLResourceOutput{
		NetworkACLId: id, VpcId: *acl.VpcId, OwnerId: *acl.OwnerId,
		Ingress: []NetworkACLRule{}, Egress: []NetworkACLRule{}, SubnetIds: []string{},
		Tags: map[string]string{},
	}
	reg := region(c.ec2)
	out.Arn = fmt.Sprintf("arn:%s:ec2:%s:%s:network-acl/%s",
		partition.Of(reg), reg, out.OwnerId, id)
	for _, entry := range acl.Entries {
		number := aws.ToInt32(entry.RuleNumber)
		if number == 32767 || number == 32768 {
			continue
		}
		protocol, err := strconv.Atoi(aws.ToString(entry.Protocol))
		if err != nil {
			return nil, fmt.Errorf("network ACL %s observed protocol: %w", id, err)
		}
		rule := NetworkACLRule{RuleNumber: int64(number), Protocol: strconv.Itoa(protocol),
			RuleAction: strings.ToLower(string(entry.RuleAction)), CidrBlock: entry.CidrBlock,
			Ipv6CidrBlock: entry.Ipv6CidrBlock, IcmpType: aws.Int64(0), IcmpCode: aws.Int64(0)}
		if entry.PortRange != nil {
			rule.FromPort = int64(aws.ToInt32(entry.PortRange.From))
			rule.ToPort = int64(aws.ToInt32(entry.PortRange.To))
		}
		if entry.IcmpTypeCode != nil {
			rule.IcmpType = aws.Int64(int64(aws.ToInt32(entry.IcmpTypeCode.Type)))
			rule.IcmpCode = aws.Int64(int64(aws.ToInt32(entry.IcmpTypeCode.Code)))
		}
		if aws.ToBool(entry.Egress) {
			out.Egress = append(out.Egress, rule)
		} else {
			out.Ingress = append(out.Ingress, rule)
		}
	}
	for _, rules := range [][]NetworkACLRule{out.Ingress, out.Egress} {
		slices.SortFunc(rules, func(a, b NetworkACLRule) int { return int(a.RuleNumber - b.RuleNumber) })
	}
	for _, a := range acl.Associations {
		out.SubnetIds = append(out.SubnetIds, aws.ToString(a.SubnetId))
	}
	out.SubnetIds = networkACLSubnets(out.SubnetIds)
	for _, tag := range acl.Tags {
		key := aws.ToString(tag.Key)
		if !strings.HasPrefix(key, "aws:") {
			out.Tags[key] = aws.ToString(tag.Value)
		}
	}
	return out, nil
}

func (c *networkACLClient) associate(
	ctx context.Context, id, subnet string, tolerateMissing bool,
) error {
	acl, err := c.lookup(ctx, &ec2.DescribeNetworkAclsInput{Filters: []ec2types.Filter{
		{Name: aws.String("association.subnet-id"), Values: []string{subnet}},
	}}, "")
	if tolerateMissing && errors.Is(err, runtime.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find network ACL association for subnet %s: %w", subnet, err)
	}
	for _, association := range acl.Associations {
		if aws.ToString(association.SubnetId) != subnet {
			continue
		}
		input := &ec2.ReplaceNetworkAclAssociationInput{
			AssociationId: association.NetworkAclAssociationId, NetworkAclId: aws.String(id),
		}
		err := c.retry(ctx, true, func(err error) bool {
			return isNotFound(err, networkACLAssociationNotFound)
		}, func() error { _, err := c.ec2.ReplaceNetworkAclAssociation(ctx, input); return err })
		if err != nil {
			return fmt.Errorf("associate subnet %s with network ACL %s: %w", subnet, id, err)
		}
		return nil
	}
	if tolerateMissing {
		return nil
	}
	return fmt.Errorf("find network ACL association for subnet %s: %w", subnet, runtime.ErrNotFound)
}

func (c *networkACLClient) restore(
	ctx context.Context, vpc string, association ec2types.NetworkAclAssociation,
) error {
	acl, err := c.lookup(ctx, &ec2.DescribeNetworkAclsInput{Filters: []ec2types.Filter{
		{Name: aws.String("vpc-id"), Values: []string{vpc}},
		{Name: aws.String("default"), Values: []string{"true"}},
	}}, "")
	if err != nil {
		return fmt.Errorf("find default network ACL for VPC %s: %w", vpc, err)
	}
	_, err = c.ec2.ReplaceNetworkAclAssociation(ctx, &ec2.ReplaceNetworkAclAssociationInput{
		AssociationId: association.NetworkAclAssociationId, NetworkAclId: acl.NetworkAclId,
	})
	if isNotFound(err, networkACLAssociationNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("restore subnet %s to default network ACL: %w",
			aws.ToString(association.SubnetId), err)
	}
	return nil
}

func (c *networkACLClient) delete(ctx context.Context, id, vpc string) error {
	acl, err := c.lookup(ctx, &ec2.DescribeNetworkAclsInput{NetworkAclIds: []string{id}}, id)
	if errors.Is(err, runtime.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if aws.ToString(acl.VpcId) != "" {
		vpc = *acl.VpcId
	}
	for _, association := range acl.Associations {
		if err := c.restore(ctx, vpc, association); err != nil {
			return err
		}
	}
	err = c.retry(ctx, true, func(err error) bool { return isNotFound(err, "DependencyViolation") },
		func() error {
			_, err := c.ec2.DeleteNetworkAcl(ctx, &ec2.DeleteNetworkAclInput{NetworkAclId: aws.String(id)})
			if isNotFound(err, networkACLNotFound) {
				return nil
			}
			return err
		})
	if err != nil {
		return fmt.Errorf("delete network ACL %s: %w", id, err)
	}
	return nil
}
