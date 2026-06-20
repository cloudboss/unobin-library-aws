package ec2

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// RouteTable is an EC2 route table: the set of routes for a VPC, which subnets
// and gateways then associate with. CreateRouteTable takes only the VPC id and
// the create-time tags; the VPC is fixed once the table exists, so a change to
// it replaces the table. The routes in the table and the associations to it are
// each their own resource, so this resource manages only the table itself and
// its tags.
type RouteTable struct {
	VpcId string            `ub:"vpc-id"`
	Tags  map[string]string `ub:"tags"`
}

// RouteTableOutput holds the values EC2 computes for a route table. The id is
// the table's handle. The owner id is the account that owns it; it settles in
// the post-create describe, so Create returns that describe rather than the
// create response. There is no ARN: the describe has no ARN field, and an
// account ARN is not composed client-side.
type RouteTableOutput struct {
	RouteTableId string `ub:"route-table-id"`
	OwnerId      string `ub:"owner-id"`
}

func (r *RouteTable) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when a route table is created. The
// VPC cannot change on an existing table, so a change to it requires a new one.
func (r *RouteTable) ReplaceFields() []string {
	return []string{"vpc-id"}
}

// Defaults marks the collection inputs a route table may omit.
func (r RouteTable) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

func (r *RouteTable) Create(ctx context.Context, cfg *awsCfg) (*RouteTableOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &ec2.CreateRouteTableInput{
		VpcId:             aws.String(r.VpcId),
		TagSpecifications: tagSpecifications(ec2types.ResourceTypeRouteTable, r.Tags),
	}
	resp, err := client.CreateRouteTable(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("create route table: %w", err)
	}
	id := aws.ToString(resp.RouteTable.RouteTableId)
	// CreateRouteTable can return before a describe sees the table. Wait for it
	// to become visible, then return that settled describe so the owner id is
	// populated.
	if err := r.waitVisible(ctx, client, id); err != nil {
		return nil, err
	}
	return r.read(ctx, client, id, true)
}

func (r *RouteTable) Read(
	ctx context.Context, cfg *awsCfg, prior *RouteTableOutput,
) (*RouteTableOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.RouteTableId, false)
}

// Update reconciles the tags, the only input a route table changes in place.
// The VPC is replace-only, so a change to it never reaches Update. Tags are
// reconciled as a set whenever they changed, the same as the other EC2
// resources.
func (r *RouteTable) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[RouteTable, *RouteTableOutput],
) (*RouteTableOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.RouteTableId
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, id, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, id, false)
}

// Delete removes the route table. EC2 refuses to delete a table that still has
// associations, reporting DependencyViolation, so Delete first disassociates
// every association the table holds and waits each one gone, then deletes the
// table and waits until a describe no longer sees it. Associations are normally
// their own resource torn down by their own Delete first; this drain is a
// best-effort backstop so an association the table still holds does not block
// the delete.
func (r *RouteTable) Delete(ctx context.Context, cfg *awsCfg, prior *RouteTableOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.RouteTableId
	table, err := describeRouteTable(ctx, client, id)
	if err != nil {
		if err == runtime.ErrNotFound {
			return nil
		}
		return err
	}
	for _, assoc := range table.Associations {
		assocID := aws.ToString(assoc.RouteTableAssociationId)
		if assocID == "" {
			continue
		}
		if err := r.disassociate(ctx, client, id, assocID); err != nil {
			return err
		}
	}
	// DeleteRouteTable tolerates only a not-found, which means the table is
	// already gone. The drain above clears the dependency, so a
	// DependencyViolation here is not retried; it is propagated.
	_, err = client.DeleteRouteTable(ctx, &ec2.DeleteRouteTableInput{
		RouteTableId: aws.String(id),
	})
	if err != nil && !isNotFound(err, "InvalidRouteTableID.NotFound") {
		return fmt.Errorf("delete route table: %w", err)
	}
	return r.waitDeleted(ctx, client, id)
}

// read fetches the route table by id and returns its computed outputs. When
// created is true the table was just made, so a not-found means the create has
// not propagated yet and read waits for it to appear; otherwise a not-found is
// drift and maps to runtime.ErrNotFound at once.
func (r *RouteTable) read(
	ctx context.Context, client *ec2.Client, id string, created bool,
) (*RouteTableOutput, error) {
	var table *ec2types.RouteTable
	err := wait.Until(ctx, fmt.Sprintf("route table %s", id),
		func(ctx context.Context) (bool, error) {
			t, err := describeRouteTable(ctx, client, id)
			if err != nil {
				if err == runtime.ErrNotFound {
					if created {
						return false, nil
					}
					return false, runtime.ErrNotFound
				}
				return false, err
			}
			table = t
			return true, nil
		}, wait.WithTimeout(5*time.Minute))
	if err != nil {
		return nil, err
	}
	return &RouteTableOutput{
		RouteTableId: aws.ToString(table.RouteTableId),
		OwnerId:      aws.ToString(table.OwnerId),
	}, nil
}

// waitVisible polls until a describe finds the route table on two consecutive
// reads, so a later read does not race the create's propagation. A new table
// can read not-found for many polls before it appears; a not-found resets the
// run rather than ending the wait, so the wait tolerates that window up to its
// timeout.
func (r *RouteTable) waitVisible(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("route table %s to become visible", id)
	return wait.UntilStable(ctx, what, 2, func(ctx context.Context) (bool, error) {
		_, err := describeRouteTable(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}, wait.WithTimeout(5*time.Minute))
}

// waitDeleted polls until the route table reads not-found on two consecutive
// reads, confirming the delete settled. A read that still finds the table
// resets the run.
func (r *RouteTable) waitDeleted(ctx context.Context, client *ec2.Client, id string) error {
	what := fmt.Sprintf("route table %s deletion", id)
	return wait.UntilStable(ctx, what, 2, func(ctx context.Context) (bool, error) {
		_, err := describeRouteTable(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, wait.WithTimeout(5*time.Minute))
}

// disassociate removes the association named by assocID from the route table
// and waits for it to clear. An association that is already gone is tolerated:
// EC2 reports that as InvalidAssociationID.NotFound, which means the drain has
// nothing to do for it.
func (r *RouteTable) disassociate(
	ctx context.Context, client *ec2.Client, id, assocID string,
) error {
	_, err := client.DisassociateRouteTable(ctx, &ec2.DisassociateRouteTableInput{
		AssociationId: aws.String(assocID),
	})
	if err != nil && !isNotFound(err, "InvalidAssociationID.NotFound") {
		return fmt.Errorf("disassociate route table: %w", err)
	}
	return r.waitDisassociated(ctx, client, id, assocID)
}

// waitDisassociated polls until the association named by assocID is gone from
// the route table. The association is still pending while it reports associated
// or disassociating -- both codes count as not-yet-gone, since an association
// can still read associated when the disassociate is first issued. It is gone
// once it no longer appears or reads disassociated, or once the table itself is
// gone. A terminal failed state stops the wait with the reported status message.
func (r *RouteTable) waitDisassociated(
	ctx context.Context, client *ec2.Client, id, assocID string,
) error {
	what := fmt.Sprintf("route table %s association %s disassociation", id, assocID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		table, err := describeRouteTable(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		for _, assoc := range table.Associations {
			if aws.ToString(assoc.RouteTableAssociationId) != assocID {
				continue
			}
			state := assoc.AssociationState
			if state == nil {
				return false, nil
			}
			switch state.State {
			case ec2types.RouteTableAssociationStateCodeAssociated,
				ec2types.RouteTableAssociationStateCodeDisassociating:
				return false, nil
			case ec2types.RouteTableAssociationStateCodeFailed:
				return false, fmt.Errorf("route table association %s failed: %s",
					assocID, aws.ToString(state.StatusMessage))
			default:
				return true, nil
			}
		}
		return true, nil
	}, wait.WithTimeout(5*time.Minute))
}

// describeRouteTable fetches the route table with the given id. EC2 reports a
// missing table by service code on an HTTP 400, never a 404, so the not-found
// code maps to runtime.ErrNotFound; an empty result or an id mismatch means the
// same.
func describeRouteTable(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.RouteTable, error) {
	resp, err := client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		RouteTableIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, "InvalidRouteTableID.NotFound") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe route tables: %w", err)
	}
	if len(resp.RouteTables) == 0 {
		return nil, runtime.ErrNotFound
	}
	table := resp.RouteTables[0]
	if aws.ToString(table.RouteTableId) != id {
		return nil, runtime.ErrNotFound
	}
	return &table, nil
}
