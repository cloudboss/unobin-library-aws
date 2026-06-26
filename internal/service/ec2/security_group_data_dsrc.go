package ec2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/defaults"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

// SecurityGroupData resolves exactly one existing EC2 security group with
// DescribeSecurityGroups. The lookup combines the optional id selector, scalar
// filters, tag filters, and generic filters as one query. A missing or
// ambiguous lookup is a normal data-source error, not runtime.ErrNotFound.
type SecurityGroupData struct {
	Id     *string                   `ub:"id"`
	Name   *string                   `ub:"name"`
	VpcId  *string                   `ub:"vpc-id"`
	Tags   map[string]string         `ub:"tags"`
	Filter []SecurityGroupDataFilter `ub:"filter"`
}

// SecurityGroupDataFilter is one DescribeSecurityGroups filter. The name and
// values pass to EC2 unchanged; empty-string values and empty value lists are
// preserved.
type SecurityGroupDataFilter struct {
	Name   string   `ub:"name"`
	Values []string `ub:"values"`
}

// SecurityGroupDataOutput holds the attributes of the selected security group.
// Arn is composed from the selected group's owner id, the client region, and
// the region's partition, even when EC2 also returns a securityGroupArn field.
type SecurityGroupDataOutput struct {
	Id          string            `ub:"id"`
	Arn         string            `ub:"arn"`
	Description string            `ub:"description"`
	Name        string            `ub:"name"`
	VpcId       string            `ub:"vpc-id"`
	Tags        map[string]string `ub:"tags"`
}

// Defaults marks the collection inputs a security group lookup may omit.
func (r SecurityGroupData) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
		defaults.Optional(r.Filter),
	}
}

// Read resolves the security group data source. DescribeSecurityGroups is
// paginated in full before singularity is asserted, so a single page cannot
// look unique by accident.
func (r *SecurityGroupData) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*SecurityGroupDataOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	group, err := r.findSecurityGroup(ctx, client)
	if err != nil {
		return nil, err
	}
	return securityGroupDataOutput(group, region(client)), nil
}

func (r *SecurityGroupData) findSecurityGroup(
	ctx context.Context,
	client *ec2.Client,
) (ec2types.SecurityGroup, error) {
	groups, err := r.findSecurityGroups(ctx, client)
	if err != nil {
		return ec2types.SecurityGroup{}, err
	}
	switch len(groups) {
	case 0:
		return ec2types.SecurityGroup{}, errors.New("no matching EC2 Security Group found")
	case 1:
		return groups[0], nil
	default:
		return ec2types.SecurityGroup{}, errors.New(
			"multiple EC2 Security Groups matched; use more specific filters")
	}
}

func (r *SecurityGroupData) findSecurityGroups(
	ctx context.Context,
	client *ec2.Client,
) ([]ec2types.SecurityGroup, error) {
	var groups []ec2types.SecurityGroup
	paginator := ec2.NewDescribeSecurityGroupsPaginator(client, r.describeInput())
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isNotFound(err, "InvalidGroup.NotFound", "InvalidSecurityGroupID.NotFound") {
				return nil, nil
			}
			return nil, fmt.Errorf("describe security groups: %w", err)
		}
		groups = append(groups, page.SecurityGroups...)
	}
	return groups, nil
}

func (r *SecurityGroupData) describeInput() *ec2.DescribeSecurityGroupsInput {
	in := &ec2.DescribeSecurityGroupsInput{}
	if r.Id != nil && *r.Id != "" {
		in.GroupIds = []string{*r.Id}
	}
	filters := r.describeFilters()
	if len(filters) > 0 {
		in.Filters = filters
	}
	return in
}

func (r *SecurityGroupData) describeFilters() []ec2types.Filter {
	filters := make([]ec2types.Filter, 0, len(r.Tags)+len(r.Filter)+2)
	filters = appendSecurityGroupDataStringFilter(filters, "group-name", r.Name)
	filters = appendSecurityGroupDataStringFilter(filters, "vpc-id", r.VpcId)
	for key, value := range r.Tags {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String("tag:" + key),
			Values: []string{value},
		})
	}
	for _, filter := range r.Filter {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String(filter.Name),
			Values: filter.Values,
		})
	}
	return filters
}

func appendSecurityGroupDataStringFilter(
	filters []ec2types.Filter,
	name string,
	value *string,
) []ec2types.Filter {
	if value == nil || *value == "" {
		return filters
	}
	return append(filters, ec2types.Filter{
		Name:   aws.String(name),
		Values: []string{*value},
	})
}

func securityGroupDataOutput(
	group ec2types.SecurityGroup,
	regionName string,
) *SecurityGroupDataOutput {
	id := aws.ToString(group.GroupId)
	ownerID := aws.ToString(group.OwnerId)
	return &SecurityGroupDataOutput{
		Id:          id,
		Arn:         securityGroupDataARN(regionName, ownerID, id),
		Description: aws.ToString(group.Description),
		Name:        aws.ToString(group.GroupName),
		VpcId:       aws.ToString(group.VpcId),
		Tags:        securityGroupDataTags(group.Tags),
	}
}

func securityGroupDataARN(regionName, ownerID, groupID string) string {
	return fmt.Sprintf("arn:%s:ec2:%s:%s:security-group/%s",
		partition.Of(regionName), regionName, ownerID, groupID)
}

// securityGroupDataTags returns the selected group's tags with AWS system tags
// removed. The library configuration has no provider-level ignore-tags rule,
// so there is no second configured filter to apply here.
func securityGroupDataTags(tags []ec2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := aws.ToString(tag.Key)
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	return out
}
