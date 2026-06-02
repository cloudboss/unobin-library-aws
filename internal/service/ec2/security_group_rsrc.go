package ec2

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// SecurityGroup is an EC2 security group: a named, stateful firewall attached
// to a VPC. The fields mirror the EC2 CreateSecurityGroup API. The name,
// description, and VPC fix the group's identity, so a change to any of them
// replaces the group; only the tags change in place. This resource manages the
// group itself, not its rules: it removes the allow-all egress rule EC2 attaches
// to a new group, so the group's egress is only what the separate egress rule
// resources declare; ingress and egress rules are managed by those resources.
type SecurityGroup struct {
	Name        *string           `ub:"name"`
	NamePrefix  *string           `ub:"name-prefix"`
	Description string            `ub:"description"`
	VpcId       *string           `ub:"vpc-id"`
	Tags        map[string]string `ub:"tags"`
	// RevokeRulesOnDelete, when true, strips this group's own rules before the
	// group is deleted, so the delete is not blocked by a rule that references
	// another group. It is a delete-time switch with no presence in the cloud,
	// so it is never sent to create or read.
	RevokeRulesOnDelete *bool `ub:"revoke-rules-on-delete"`
}

// SecurityGroupOutput holds the values EC2 computes for a security group. The
// id is the stable sg- handle. The ARN is composed from the partition, region,
// owner, and id rather than read back, so it has the same form everywhere. The
// owner id is the account that owns the group.
type SecurityGroupOutput struct {
	Id      string `ub:"id"`
	Arn     string `ub:"arn"`
	OwnerId string `ub:"owner-id"`
}

func (r *SecurityGroup) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 bakes into a group at creation and cannot
// change afterward. The name, name prefix, description, and VPC are all fixed
// once the group exists, so changing any of them requires a new group. Only the
// tags are reconciled in place by Update.
func (r *SecurityGroup) ReplaceFields() []string {
	return []string{
		"name",
		"name-prefix",
		"description",
		"vpc-id",
	}
}

// Constraints declares the one cross-field rule on a security group's inputs: a
// caller fixes the group name with an exact name or a name prefix, not both. A
// caller that gives neither gets a generated name.
func (r SecurityGroup) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.Name, r.NamePrefix),
	}
}

func (r *SecurityGroup) Create(ctx context.Context, cfg any) (*SecurityGroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	name, err := r.resolveName()
	if err != nil {
		return nil, err
	}
	in := &ec2.CreateSecurityGroupInput{
		GroupName:         aws.String(name),
		Description:       aws.String(r.Description),
		VpcId:             r.VpcId,
		TagSpecifications: tagSpecifications(ec2types.ResourceTypeSecurityGroup, r.Tags),
	}
	resp, err := client.CreateSecurityGroup(ctx, in)
	// Some partitions, such as the ISO partitions, cannot tag a group as it is
	// created. When the tagged create fails for that reason, create the group
	// without tags and apply them with a separate call below.
	taggedSeparately := false
	if err != nil && in.TagSpecifications != nil &&
		partition.UnsupportedOperation(region(client), err) {
		in.TagSpecifications = nil
		taggedSeparately = true
		resp, err = client.CreateSecurityGroup(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create security group: %w", err)
	}
	id := aws.ToString(resp.GroupId)
	if taggedSeparately && len(r.Tags) > 0 {
		if err := syncTags(ctx, client, id, r.Tags); err != nil {
			return nil, err
		}
	}
	// A just-created group is eventually consistent: a describe can briefly
	// report it absent. Wait for it to read as present a few times in a row
	// before trusting it, so the follow-on read that composes the ARN does not
	// race the create.
	what := fmt.Sprintf("security group %s", id)
	probe := func(ctx context.Context) (bool, error) {
		_, err := r.describe(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	if err := wait.UntilStable(ctx, what, 3, probe, wait.WithInterval(time.Second)); err != nil {
		return nil, err
	}
	// AWS attaches an allow-all egress rule to every new group. Remove it so the
	// group's egress is exactly what its egress rule resources declare, the way
	// Terraform does, rather than a surprise allow-all nobody asked for.
	if err := r.revokeDefaultEgress(ctx, client, id); err != nil {
		return nil, err
	}
	// Read settles the rest: CreateSecurityGroup does not return the owner id,
	// which the ARN is composed from, so the output comes from a describe.
	return r.read(ctx, client, id)
}

func (r *SecurityGroup) Read(
	ctx context.Context, cfg any, prior *SecurityGroupOutput,
) (*SecurityGroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Id)
}

// read fetches the group by id and returns its computed outputs, composing the
// ARN from the client's region, the region's partition, the owner id, and the
// id. A group that has vanished maps to runtime.ErrNotFound so the runtime sees
// it as drift.
func (r *SecurityGroup) read(
	ctx context.Context, client *ec2.Client, id string,
) (*SecurityGroupOutput, error) {
	group, err := r.describe(ctx, client, id)
	if err != nil {
		return nil, err
	}
	region := region(client)
	ownerID := aws.ToString(group.OwnerId)
	arn := fmt.Sprintf("arn:%s:ec2:%s:%s:security-group/%s",
		partition.Of(region), region, ownerID, aws.ToString(group.GroupId))
	return &SecurityGroupOutput{
		Id:      aws.ToString(group.GroupId),
		Arn:     arn,
		OwnerId: ownerID,
	}, nil
}

// describe returns the security group with the given id. EC2 reports a missing
// group by service code on an HTTP 400, never a 404, so the not-found codes map
// to runtime.ErrNotFound; an empty result slice means the same.
func (r *SecurityGroup) describe(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.SecurityGroup, error) {
	resp, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		GroupIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, "InvalidGroup.NotFound", "InvalidSecurityGroupID.NotFound") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe security groups: %w", err)
	}
	if len(resp.SecurityGroups) == 0 {
		return nil, runtime.ErrNotFound
	}
	return &resp.SecurityGroups[0], nil
}

func (r *SecurityGroup) Update(
	ctx context.Context, cfg any, prior runtime.Prior[SecurityGroup, *SecurityGroupOutput],
) (*SecurityGroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, prior.Outputs.Id, r.Tags); err != nil {
			return nil, err
		}
	}
	// The outputs -- id, ARN, owner id -- are fixed when the group is created
	// and an update never changes them, so the prior outputs still describe the
	// group. There is nothing fresh to read.
	return prior.Outputs, nil
}

func (r *SecurityGroup) Delete(ctx context.Context, cfg any, prior *SecurityGroupOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	if aws.ToBool(r.RevokeRulesOnDelete) {
		if err := r.revokeOwnRules(ctx, client, prior.Id); err != nil {
			return err
		}
	}
	// Another resource may still hold a reference to this group when the delete
	// runs, which EC2 reports as a dependency conflict that clears once that
	// resource is gone, so retry the delete through it over a generous window.
	err = retry.OnError(ctx, isSecurityGroupInUse, func(ctx context.Context) error {
		_, err := client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: aws.String(prior.Id),
		})
		return err
	}, retry.WithTimeout(15*time.Minute))
	if err != nil {
		if isNotFound(err, "InvalidGroup.NotFound") {
			return nil
		}
		return fmt.Errorf("delete security group: %w", err)
	}
	// The delete is asynchronous: the group lingers briefly after the call
	// returns. Wait until a describe reports it not-found before declaring the
	// delete done, using the not-found mapping as the ready signal.
	what := fmt.Sprintf("security group %s deletion", prior.Id)
	probe := func(ctx context.Context) (bool, error) {
		_, err := r.describe(ctx, client, prior.Id)
		if err == runtime.ErrNotFound {
			return true, nil
		}
		return false, err
	}
	if err := wait.Until(ctx, what, probe, wait.WithInterval(time.Second)); err != nil {
		return err
	}
	return nil
}

// revokeDefaultEgress removes the allow-all egress rules AWS attaches to a new
// security group, so the group's egress is exactly what its egress rule
// resources declare. At create the only egress rules on the group are AWS's
// defaults, since the egress rule resources depend on the group and run
// afterward, so every egress rule found here is a default to remove.
func (r *SecurityGroup) revokeDefaultEgress(
	ctx context.Context, client *ec2.Client, id string,
) error {
	_, egress, err := securityGroupRuleIDs(ctx, client, id)
	if err != nil {
		return err
	}
	return revokeEgressRules(ctx, client, id, egress)
}

// revokeOwnRules strips every rule on this group, so the group's own rules do
// not block its deletion. It intentionally does not sweep rules in other groups
// that reference this one, the way the Terraform provider does; the
// dependency-conflict retry on the delete covers the ordinary case where such a
// reference is removed around the same time.
func (r *SecurityGroup) revokeOwnRules(
	ctx context.Context, client *ec2.Client, id string,
) error {
	ingress, egress, err := securityGroupRuleIDs(ctx, client, id)
	if err != nil {
		return err
	}
	if err := revokeIngressRules(ctx, client, id, ingress); err != nil {
		return err
	}
	return revokeEgressRules(ctx, client, id, egress)
}

// securityGroupRuleIDs returns the ids of the group's ingress and egress rules,
// split by direction.
func securityGroupRuleIDs(
	ctx context.Context, client *ec2.Client, id string,
) (ingress, egress []string, err error) {
	pager := ec2.NewDescribeSecurityGroupRulesPaginator(client,
		&ec2.DescribeSecurityGroupRulesInput{
			Filters: []ec2types.Filter{{
				Name:   aws.String("group-id"),
				Values: []string{id},
			}},
		})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("describe security group rules: %w", err)
		}
		for _, rule := range page.SecurityGroupRules {
			ruleID := aws.ToString(rule.SecurityGroupRuleId)
			if aws.ToBool(rule.IsEgress) {
				egress = append(egress, ruleID)
			} else {
				ingress = append(ingress, ruleID)
			}
		}
	}
	return ingress, egress, nil
}

// revokeIngressRules revokes the group's ingress rules named by ids. It is a
// no-op on an empty list.
func revokeIngressRules(ctx context.Context, client *ec2.Client, id string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
		GroupId:              aws.String(id),
		SecurityGroupRuleIds: ids,
	}); err != nil {
		return fmt.Errorf("revoke security group ingress: %w", err)
	}
	return nil
}

// revokeEgressRules revokes the group's egress rules named by ids. It is a
// no-op on an empty list.
func revokeEgressRules(ctx context.Context, client *ec2.Client, id string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := client.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
		GroupId:              aws.String(id),
		SecurityGroupRuleIds: ids,
	}); err != nil {
		return fmt.Errorf("revoke security group egress: %w", err)
	}
	return nil
}

// resolveName returns the group name to create. An explicit name is used as
// given. A name prefix gets a random suffix appended. With neither, the name is
// a short neutral prefix plus a random suffix, so concurrent creates do not
// collide on a name.
func (r *SecurityGroup) resolveName() (string, error) {
	if r.Name != nil {
		return *r.Name, nil
	}
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	if r.NamePrefix != nil {
		return *r.NamePrefix + suffix, nil
	}
	return "unobin-" + suffix, nil
}

// randomSuffix returns a hex string from twelve random bytes, for the tail of a
// generated security group name.
func randomSuffix() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate security group name: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// isSecurityGroupInUse reports whether a DeleteSecurityGroup error is a
// dependency conflict that clears once the referencing resource is gone. EC2
// raises it by service code on an HTTP 400, so it is matched the same way as a
// not-found.
func isSecurityGroupInUse(err error) bool {
	return isNotFound(err, "DependencyViolation", "InvalidGroup.InUse")
}
