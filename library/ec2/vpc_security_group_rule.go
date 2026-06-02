package ec2

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/library/internal/ec2helpers"
	"github.com/cloudboss/unobin-library-aws/library/internal/partition"
	"github.com/cloudboss/unobin-library-aws/library/internal/ptr"
)

// sgRule is the common input form of an ingress and an egress security group
// rule. The two resources differ only in direction, so the shared lifecycle
// functions take this view of either struct rather than each concrete type.
// The fields match the AWS SDK names; the description is folded into whichever
// source sub-object the rule populates, never sent on its own.
type sgRule struct {
	SecurityGroupId           string
	IpProtocol                string
	FromPort                  *int64
	ToPort                    *int64
	CidrIpv4                  *string
	CidrIpv6                  *string
	PrefixListId              *string
	ReferencedSecurityGroupId *string
	Description               *string
	Tags                      map[string]string
}

// sgRuleNotFoundCode is the EC2 error code for a security group rule that no
// longer exists. A Read of a gone rule maps it to runtime.ErrNotFound.
const sgRuleNotFoundCode = "InvalidSecurityGroupRuleId.NotFound"

// sgRuleRevokeNotFoundCodes are the codes a Revoke may return when the rule or
// its security group is already gone; both mean the delete has nothing to do.
var sgRuleRevokeNotFoundCodes = []string{
	"InvalidSecurityGroupRuleId.NotFound",
	"InvalidGroup.NotFound",
}

// ipPermission builds the single IpPermission a create authorizes. Exactly one
// source is set (a constraint guarantees this), and the rule's description
// rides inside that source sub-object, the only place the authorize API accepts
// it.
func (r sgRule) ipPermission() ec2types.IpPermission {
	perm := ec2types.IpPermission{
		IpProtocol: aws.String(r.IpProtocol),
		FromPort:   ptr.Int32(r.FromPort),
		ToPort:     ptr.Int32(r.ToPort),
	}
	switch {
	case r.CidrIpv4 != nil:
		perm.IpRanges = []ec2types.IpRange{{
			CidrIp:      r.CidrIpv4,
			Description: r.Description,
		}}
	case r.CidrIpv6 != nil:
		perm.Ipv6Ranges = []ec2types.Ipv6Range{{
			CidrIpv6:    r.CidrIpv6,
			Description: r.Description,
		}}
	case r.PrefixListId != nil:
		perm.PrefixListIds = []ec2types.PrefixListId{{
			PrefixListId: r.PrefixListId,
			Description:  r.Description,
		}}
	case r.ReferencedSecurityGroupId != nil:
		userID, groupID := splitReferencedGroup(*r.ReferencedSecurityGroupId)
		perm.UserIdGroupPairs = []ec2types.UserIdGroupPair{{
			UserId:      userID,
			GroupId:     aws.String(groupID),
			Description: r.Description,
		}}
	}
	return perm
}

// ruleRequest builds the SecurityGroupRuleRequest an in-place update sends. It
// mirrors ipPermission but uses the flat request form: a single referenced
// group id, with the user id half of a cross-account reference ignored because
// the request type has no field for it.
func (r sgRule) ruleRequest() *ec2types.SecurityGroupRuleRequest {
	req := &ec2types.SecurityGroupRuleRequest{
		IpProtocol:  aws.String(r.IpProtocol),
		FromPort:    ptr.Int32(r.FromPort),
		ToPort:      ptr.Int32(r.ToPort),
		Description: r.Description,
	}
	switch {
	case r.CidrIpv4 != nil:
		req.CidrIpv4 = r.CidrIpv4
	case r.CidrIpv6 != nil:
		req.CidrIpv6 = r.CidrIpv6
	case r.PrefixListId != nil:
		req.PrefixListId = r.PrefixListId
	case r.ReferencedSecurityGroupId != nil:
		_, groupID := splitReferencedGroup(*r.ReferencedSecurityGroupId)
		req.ReferencedGroupId = aws.String(groupID)
	}
	return req
}

// splitReferencedGroup parses a referenced security group value. AWS accepts
// either a bare "GroupID" or an "AccountID/GroupID" pair for a group in another
// account; the latter splits into a user id and a group id, the former into a
// nil user id and the whole value as the group id.
func splitReferencedGroup(ref string) (userID *string, groupID string) {
	if account, group, ok := strings.Cut(ref, "/"); ok {
		return aws.String(account), group
	}
	return nil, ref
}

// composeRuleARN builds the ARN of a security group rule from its id and owner.
// EC2 returns the owner account id on the authorize response, so the ARN is
// composed without an extra read.
func composeRuleARN(region, accountID, ruleID string) string {
	return fmt.Sprintf("arn:%s:ec2:%s:%s:security-group-rule/%s",
		partition.Of(region), region, accountID, ruleID)
}

// sgRuleCreate authorizes one rule on its security group in the given
// direction and returns the rule id and composed ARN. The authorize response
// holds both the rule id and the owner account id, so create does not route
// through Read. There is no create-time waiter or retry: the rule is usable as
// soon as authorize returns.
func sgRuleCreate(
	ctx context.Context, cfg any, r sgRule, egress bool,
) (ruleID, arn string, err error) {
	client, err := ec2helpers.NewClient(ctx, cfg)
	if err != nil {
		return "", "", err
	}
	perms := []ec2types.IpPermission{r.ipPermission()}
	tagSpecs := ec2helpers.TagSpecifications(ec2types.ResourceTypeSecurityGroupRule, r.Tags)
	var rule ec2types.SecurityGroupRule
	if egress {
		resp, err := client.AuthorizeSecurityGroupEgress(ctx,
			&ec2.AuthorizeSecurityGroupEgressInput{
				GroupId:           aws.String(r.SecurityGroupId),
				IpPermissions:     perms,
				TagSpecifications: tagSpecs,
			})
		if err != nil {
			return "", "", fmt.Errorf("authorize security group egress: %w", err)
		}
		if len(resp.SecurityGroupRules) == 0 {
			return "", "", fmt.Errorf("authorize security group egress: no rule returned")
		}
		rule = resp.SecurityGroupRules[0]
	} else {
		resp, err := client.AuthorizeSecurityGroupIngress(ctx,
			&ec2.AuthorizeSecurityGroupIngressInput{
				GroupId:           aws.String(r.SecurityGroupId),
				IpPermissions:     perms,
				TagSpecifications: tagSpecs,
			})
		if err != nil {
			return "", "", fmt.Errorf("authorize security group ingress: %w", err)
		}
		if len(resp.SecurityGroupRules) == 0 {
			return "", "", fmt.Errorf("authorize security group ingress: no rule returned")
		}
		rule = resp.SecurityGroupRules[0]
	}
	ruleID = aws.ToString(rule.SecurityGroupRuleId)
	arn = composeRuleARN(ec2helpers.Region(client),
		aws.ToString(rule.GroupOwnerId), ruleID)
	return ruleID, arn, nil
}

// findRule returns the rule named by ruleID, or nil when no such rule exists.
// The describe is paginated; a not-found error code is reported as no rule
// rather than an error, since both mean the rule is absent. Only a rule whose
// returned id matches the one asked for is considered a hit, an id-reuse guard.
func findRule(
	ctx context.Context, client *ec2.Client, ruleID string,
) (*ec2types.SecurityGroupRule, error) {
	paginator := ec2.NewDescribeSecurityGroupRulesPaginator(client,
		&ec2.DescribeSecurityGroupRulesInput{
			SecurityGroupRuleIds: []string{ruleID},
		})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if ec2helpers.IsNotFound(err, sgRuleNotFoundCode) {
				return nil, nil
			}
			return nil, fmt.Errorf("describe security group rules: %w", err)
		}
		for i := range page.SecurityGroupRules {
			if aws.ToString(page.SecurityGroupRules[i].SecurityGroupRuleId) == ruleID {
				return &page.SecurityGroupRules[i], nil
			}
		}
	}
	return nil, nil
}

// sgRuleRead confirms the rule named by ruleID still exists in the given
// direction. It returns runtime.ErrNotFound when the rule is gone, when the
// describe returns nothing, when the returned id differs from the one asked
// for (an id-reuse guard), or when the rule's direction does not match. The id
// and ARN are immutable, so a present rule needs no values read back; the
// caller keeps the prior output.
func sgRuleRead(ctx context.Context, cfg any, ruleID string, egress bool) error {
	client, err := ec2helpers.NewClient(ctx, cfg)
	if err != nil {
		return err
	}
	rule, err := findRule(ctx, client, ruleID)
	if err != nil {
		return err
	}
	if rule == nil {
		return runtime.ErrNotFound
	}
	if aws.ToBool(rule.IsEgress) != egress {
		return runtime.ErrNotFound
	}
	return nil
}

// sgRuleUpdate reconciles a rule in place. It rewrites the rule only when one
// of its writable properties changed, then reconciles tags. The id and ARN
// cannot change, so the caller returns the prior output unchanged.
func sgRuleUpdate(ctx context.Context, cfg any, r sgRule, prior sgRule, ruleID string) error {
	client, err := ec2helpers.NewClient(ctx, cfg)
	if err != nil {
		return err
	}
	if sgRuleSpecChanged(prior, r) {
		_, err := client.ModifySecurityGroupRules(ctx, &ec2.ModifySecurityGroupRulesInput{
			GroupId: aws.String(r.SecurityGroupId),
			SecurityGroupRules: []ec2types.SecurityGroupRuleUpdate{{
				SecurityGroupRuleId: aws.String(ruleID),
				SecurityGroupRule:   r.ruleRequest(),
			}},
		})
		if err != nil {
			return fmt.Errorf("modify security group rules: %w", err)
		}
	}
	if err := ec2helpers.SyncTags(ctx, client, ruleID, r.Tags); err != nil {
		return err
	}
	return nil
}

// sgRuleSpecChanged reports whether any in-place updatable property of the rule
// differs between prior and current. The source fields are included even
// though a source change forces a replace, so a rewrite that did slip through
// still sends the right fields; tags are reconciled separately and excluded.
func sgRuleSpecChanged(prior, current sgRule) bool {
	return runtime.Changed(prior.IpProtocol, current.IpProtocol) ||
		runtime.Changed(prior.FromPort, current.FromPort) ||
		runtime.Changed(prior.ToPort, current.ToPort) ||
		runtime.Changed(prior.CidrIpv4, current.CidrIpv4) ||
		runtime.Changed(prior.CidrIpv6, current.CidrIpv6) ||
		runtime.Changed(prior.PrefixListId, current.PrefixListId) ||
		runtime.Changed(prior.ReferencedSecurityGroupId, current.ReferencedSecurityGroupId) ||
		runtime.Changed(prior.Description, current.Description)
}

// sgRuleDelete revokes the rule in the given direction. The Revoke API scopes
// a rule by its security group, which the output does not record, so delete
// first looks the rule up to recover its group id. A rule that is already gone
// is a successful delete with nothing to do, as is a Revoke that races another
// deletion and reports the rule or group already absent.
func sgRuleDelete(ctx context.Context, cfg any, ruleID string, egress bool) error {
	client, err := ec2helpers.NewClient(ctx, cfg)
	if err != nil {
		return err
	}
	rule, err := findRule(ctx, client, ruleID)
	if err != nil {
		return err
	}
	if rule == nil {
		return nil
	}
	groupID := aws.ToString(rule.GroupId)
	if egress {
		_, err = client.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
			GroupId:              aws.String(groupID),
			SecurityGroupRuleIds: []string{ruleID},
		})
	} else {
		_, err = client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
			GroupId:              aws.String(groupID),
			SecurityGroupRuleIds: []string{ruleID},
		})
	}
	if err != nil {
		if ec2helpers.IsNotFound(err, sgRuleRevokeNotFoundCodes...) {
			return nil
		}
		return fmt.Errorf("revoke security group rule: %w", err)
	}
	return nil
}
