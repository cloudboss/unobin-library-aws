package rds

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// subnetGroupNameRe matches the characters RDS permits in a DB subnet group
// name: lowercase letters, digits, hyphen, underscore, period, and space. The
// name is also bounded at 255 bytes and the value "default" is reserved. These
// are pattern and reserved-word rules the constraint layer cannot express, so
// they are checked in code and documented on the name field.
var subnetGroupNameRe = regexp.MustCompile(`^[0-9a-z_ .-]+$`)

// subnetGroupNameMaxLen is the longest a DB subnet group name may be. RDS
// measures the limit in characters; this check counts bytes, which agrees for
// the ASCII set the name pattern already restricts the value to.
const subnetGroupNameMaxLen = 255

// subnetGroupDeleteTimeout bounds the wait for a deleted subnet group to
// disappear. DeleteDBSubnetGroup returns before the group is gone, so a read
// can still find it briefly; three minutes is ample for the group to clear.
const subnetGroupDeleteTimeout = 3 * time.Minute

// SubnetGroup is an RDS DB subnet group: the set of VPC subnets an RDS instance
// or cluster may place its network interfaces in. The group is keyed by name,
// the one field RDS fixes at create time, so a change to the name replaces the
// group; the description and subnet set are reconciled in place. The VPC is
// inferred by RDS from the supplied subnets and is never an input.
type SubnetGroup struct {
	// Name is the DB subnet group name. RDS stores it lowercased. It must hold
	// only lowercase letters, digits, hyphen, underscore, period, or space, be
	// no longer than 255 characters, and not equal "default", which RDS
	// reserves for its own default group.
	Name        string            `ub:"name"`
	Description string            `ub:"description"`
	SubnetIds   []string          `ub:"subnet-ids"`
	Tags        map[string]string `ub:"tags"`
}

// SubnetGroupOutput holds the values RDS computes for a DB subnet group. The ARN
// is the group's handle, against which its tags are managed. The VPC id is
// inferred by RDS from the subnets. The supported network types are the address
// families the subnets allow, such as IPV4 or DUAL.
type SubnetGroupOutput struct {
	Arn                   string   `ub:"arn"`
	VpcId                 string   `ub:"vpc-id"`
	SupportedNetworkTypes []string `ub:"supported-network-types"`
}

func (r *SubnetGroup) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs RDS fixes when a DB subnet group is created.
// Only the name is immutable; a change to it requires a new group. The
// description and subnet set are reconciled in place by ModifyDBSubnetGroup.
func (r *SubnetGroup) ReplaceFields() []string {
	return []string{"name"}
}

// Defaults marks the optional collection inputs a DB subnet group may omit. A
// bare map input is otherwise compile-required; the subnet set is required, so
// only the tags are optional.
func (r SubnetGroup) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rule the subnet set is non-empty. RDS requires at
// least two subnets in two Availability Zones; this checks only that the list is
// present, leaving the count and zone rules to RDS. The name's character,
// length, and reserved-word rules are patterns the constraint layer cannot
// express and are checked in Create.
func (r SubnetGroup) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.SubnetIds)).
			Message("subnet-ids must not be empty"),
	}
}

func (r *SubnetGroup) Create(ctx context.Context, cfg any) (*SubnetGroupOutput, error) {
	if err := r.validateName(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_, err = client.CreateDBSubnetGroup(ctx, &rds.CreateDBSubnetGroupInput{
		DBSubnetGroupName:        aws.String(r.Name),
		DBSubnetGroupDescription: aws.String(r.Description),
		SubnetIds:                r.SubnetIds,
		Tags:                     tagList(r.Tags),
	})
	if err != nil {
		return nil, fmt.Errorf("create db subnet group: %w", err)
	}
	return r.read(ctx, client)
}

func (r *SubnetGroup) Read(
	ctx context.Context, cfg any, prior *SubnetGroupOutput,
) (*SubnetGroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the group by name and maps it to outputs, returning
// runtime.ErrNotFound when it is gone.
func (r *SubnetGroup) read(
	ctx context.Context, client *rds.Client,
) (*SubnetGroupOutput, error) {
	g, err := findSubnetGroup(ctx, client, r.Name)
	if err != nil {
		return nil, err
	}
	return &SubnetGroupOutput{
		Arn:                   aws.ToString(g.DBSubnetGroupArn),
		VpcId:                 aws.ToString(g.VpcId),
		SupportedNetworkTypes: g.SupportedNetworkTypes,
	}, nil
}

func (r *SubnetGroup) Update(
	ctx context.Context, cfg any, prior runtime.Prior[SubnetGroup, *SubnetGroupOutput],
) (*SubnetGroupOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// ModifyDBSubnetGroup reconciles the description and subnet set; the name is
	// immutable, so it is only the key, never modified. The call requires the
	// subnet set, so it is always sent. The modify runs only when one of those
	// fields changed.
	if runtime.Changed(prior.Inputs.Description, r.Description) ||
		runtime.Changed(prior.Inputs.SubnetIds, r.SubnetIds) {
		_, err = client.ModifyDBSubnetGroup(ctx, &rds.ModifyDBSubnetGroupInput{
			DBSubnetGroupName:        aws.String(r.Name),
			DBSubnetGroupDescription: aws.String(r.Description),
			SubnetIds:                r.SubnetIds,
		})
		if err != nil {
			return nil, fmt.Errorf("modify db subnet group: %w", err)
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, prior.Outputs.Arn, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *SubnetGroup) Delete(ctx context.Context, cfg any, prior *SubnetGroupOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteDBSubnetGroup(ctx, &rds.DeleteDBSubnetGroupInput{
		DBSubnetGroupName: aws.String(r.Name),
	})
	if err != nil {
		// A group already gone is the outcome the delete wants, so a not-found
		// fault on the delete call counts as success.
		if subnetGroupNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete db subnet group: %w", err)
	}
	return r.waitGone(ctx, client)
}

// waitGone polls until the group no longer describes. DeleteDBSubnetGroup is
// asynchronous, so a read can still find the group for a short time after the
// delete call accepts; the delete is not complete until the group is gone.
func (r *SubnetGroup) waitGone(ctx context.Context, client *rds.Client) error {
	what := fmt.Sprintf("db subnet group %s to be deleted", r.Name)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := findSubnetGroup(ctx, client, r.Name)
		if err == runtime.ErrNotFound {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	}, wait.WithTimeout(subnetGroupDeleteTimeout), wait.WithInterval(time.Second))
}

// validateName checks the name against the rules RDS enforces but the
// constraint layer cannot express: the permitted character set, the 255-byte
// length bound, and the reserved value "default", which RDS matches
// case-insensitively.
func (r *SubnetGroup) validateName() error {
	if len(r.Name) > subnetGroupNameMaxLen {
		return fmt.Errorf("name must be at most %d characters", subnetGroupNameMaxLen)
	}
	if !subnetGroupNameRe.MatchString(r.Name) {
		return fmt.Errorf(
			"name must contain only lowercase letters, digits, hyphen, " +
				"underscore, period, or space")
	}
	if strings.EqualFold(r.Name, "default") {
		return errors.New(`name must not be "default", which RDS reserves`)
	}
	return nil
}

// findSubnetGroup describes the group by name and returns it. RDS returns the
// typed fault DBSubnetGroupNotFoundFault for a missing name, which maps to
// runtime.ErrNotFound; an empty result likewise maps to not-found, and a
// returned group whose name does not match the request is treated as not-found,
// guarding against a stale read just after a create.
func findSubnetGroup(
	ctx context.Context, client *rds.Client, name string,
) (*rdstypes.DBSubnetGroup, error) {
	paginator := rds.NewDescribeDBSubnetGroupsPaginator(client,
		&rds.DescribeDBSubnetGroupsInput{DBSubnetGroupName: aws.String(name)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if subnetGroupNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe db subnet groups: %w", err)
		}
		for i := range page.DBSubnetGroups {
			g := page.DBSubnetGroups[i]
			if aws.ToString(g.DBSubnetGroupName) == name {
				return &g, nil
			}
		}
	}
	return nil, runtime.ErrNotFound
}

// subnetGroupNotFound reports whether err is the RDS typed fault for a missing
// DB subnet group. RDS signals not-found with the typed exception
// DBSubnetGroupNotFoundFault rather than an HTTP status or a string code.
func subnetGroupNotFound(err error) bool {
	var fault *rdstypes.DBSubnetGroupNotFoundFault
	return errors.As(err, &fault)
}
