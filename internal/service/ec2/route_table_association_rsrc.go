package ec2

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// routeTablePropagationTimeout bounds the eventual-consistency windows around a
// just-created route table. AssociateRouteTable can briefly report the table as
// not yet visible when it, or its association, was created in the same apply, so
// the create call and the post-create read each retry through this window.
const routeTablePropagationTimeout = 5 * time.Minute

// routeTableAssociationNotFoundChecks bounds how many consecutive not-found
// observations a create or replace wait tolerates before giving up. A just-made
// association can be absent from a describe for a while before it appears, so
// the wait counts not-founds rather than treating the first as a failure; a
// large bound matches the very high tolerance the route table association needs
// during this settling window.
const routeTableAssociationNotFoundChecks = 1000

// RouteTableAssociation links one route table to a subnet or a gateway. Exactly
// one of subnet-id or gateway-id names what the table attaches to, and both are
// fixed at create time, so a change to either replaces the association; the
// route table is changed in place by ReplaceRouteTableAssociation, which mints a
// fresh association id. AssociateRouteTable creates the association and returns
// its id; the state is not in that response, so the association is waited to
// associated before the resource reports it.
type RouteTableAssociation struct {
	RouteTableId string  `ub:"route-table-id"`
	SubnetId     *string `ub:"subnet-id"`
	GatewayId    *string `ub:"gateway-id"`
}

// RouteTableAssociationOutput holds the one value EC2 computes for an
// association: its id. The id is the association's handle for read and delete,
// and ReplaceRouteTableAssociation replaces it with a new id, so a route table
// change refreshes this value from the replace response's NewAssociationId.
type RouteTableAssociationOutput struct {
	RouteTableAssociationId string `ub:"route-table-association-id"`
}

func (r *RouteTableAssociation) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when an association is created. The
// subnet or gateway an association attaches to cannot change on an existing
// association, so a change to either requires a new association. The route table
// is not listed: a route table change is reconciled in place by Update through
// ReplaceRouteTableAssociation.
func (r *RouteTableAssociation) ReplaceFields() []string {
	return []string{
		"subnet-id",
		"gateway-id",
	}
}

// Constraints declares the rule EC2 enforces on an association's inputs. An
// association attaches a route table to exactly one of a subnet or a gateway,
// never both and never neither. The route table itself is required, which the
// non-pointer route-table-id field already enforces.
func (r RouteTableAssociation) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.SubnetId, r.GatewayId),
	}
}

func (r *RouteTableAssociation) Create(
	ctx context.Context, cfg any,
) (*RouteTableAssociationOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id, err := r.associate(ctx, client)
	if err != nil {
		return nil, err
	}
	// AssociateRouteTable returns only the association id, not its state, so wait
	// for the association to reach associated before reporting it.
	if err := r.waitAssociated(ctx, client, id); err != nil {
		return nil, err
	}
	// Re-read to confirm the association is visible to a describe. A just-made
	// association can be absent from DescribeRouteTables briefly, so the
	// post-create read tolerates that window rather than treating it as drift.
	return r.read(ctx, client, id, true)
}

func (r *RouteTableAssociation) Read(
	ctx context.Context, cfg any, prior *RouteTableAssociationOutput,
) (*RouteTableAssociationOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.RouteTableAssociationId, false)
}

func (r *RouteTableAssociation) Update(
	ctx context.Context, cfg any,
	prior runtime.Prior[RouteTableAssociation, *RouteTableAssociationOutput],
) (*RouteTableAssociationOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.RouteTableAssociationId
	// Only a route table change reaches Update; a subnet or gateway change forces
	// a replace through ReplaceFields. When nothing this method reconciles changed,
	// leave the association untouched and report its current id.
	if !runtime.Changed(prior.Inputs.RouteTableId, r.RouteTableId) {
		return r.read(ctx, client, id, false)
	}
	resp, err := client.ReplaceRouteTableAssociation(ctx, &ec2.ReplaceRouteTableAssociationInput{
		AssociationId: aws.String(id),
		RouteTableId:  aws.String(r.RouteTableId),
	})
	if err != nil {
		// The old association vanished out-of-band between the plan-time read and
		// this replace. Recreate it with a fresh AssociateRouteTable rather than
		// failing, matching the recreate a gone association would otherwise trigger.
		if isNotFound(err, "InvalidAssociationID.NotFound") {
			newID, err := r.associate(ctx, client)
			if err != nil {
				return nil, err
			}
			if err := r.waitAssociated(ctx, client, newID); err != nil {
				return nil, err
			}
			return r.read(ctx, client, newID, true)
		}
		return nil, fmt.Errorf("replace route table association: %w", err)
	}
	// ReplaceRouteTableAssociation mints a new association id; the old one is gone.
	// Adopt NewAssociationId as the association's handle, wait it to associated,
	// and read under the new id so the output reflects the post-replace value.
	newID := aws.ToString(resp.NewAssociationId)
	if err := r.waitAssociated(ctx, client, newID); err != nil {
		return nil, err
	}
	return r.read(ctx, client, newID, true)
}

func (r *RouteTableAssociation) Delete(
	ctx context.Context, cfg any, prior *RouteTableAssociationOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.RouteTableAssociationId
	_, err = client.DisassociateRouteTable(ctx, &ec2.DisassociateRouteTableInput{
		AssociationId: aws.String(id),
	})
	if err != nil {
		// An association already gone counts as deleted.
		if isNotFound(err, "InvalidAssociationID.NotFound") {
			return nil
		}
		return fmt.Errorf("disassociate route table: %w", err)
	}
	return r.waitDisassociated(ctx, client, id)
}

// associate creates the association with AssociateRouteTable and returns its id.
// A route table created in the same apply may not be visible to this call yet,
// reported as InvalidRouteTableID.NotFound, so the call retries through that
// eventual-consistency window.
func (r *RouteTableAssociation) associate(
	ctx context.Context, client *ec2.Client,
) (string, error) {
	in := &ec2.AssociateRouteTableInput{
		RouteTableId: aws.String(r.RouteTableId),
		SubnetId:     r.SubnetId,
		GatewayId:    r.GatewayId,
	}
	var resp *ec2.AssociateRouteTableOutput
	err := retry.OnError(ctx, isRouteTableNotFound, func(ctx context.Context) error {
		var err error
		resp, err = client.AssociateRouteTable(ctx, in)
		return err
	}, retry.WithTimeout(routeTablePropagationTimeout))
	if err != nil {
		return "", fmt.Errorf("associate route table: %w", err)
	}
	return aws.ToString(resp.AssociationId), nil
}

// read finds the association by id and returns its computed output. When created
// is true the association was just made or just replaced, so a not-found means
// it has not propagated to a describe yet and read waits through that window;
// otherwise a not-found is drift and maps to runtime.ErrNotFound at once. A
// disassociated association is logically gone and reads as not-found either way.
func (r *RouteTableAssociation) read(
	ctx context.Context, client *ec2.Client, id string, created bool,
) (*RouteTableAssociationOutput, error) {
	err := wait.Until(ctx, fmt.Sprintf("route table association %s", id),
		func(ctx context.Context) (bool, error) {
			_, found, err := findRouteTableAssociation(ctx, client, id)
			if err != nil {
				return false, err
			}
			if !found {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			return true, nil
		}, wait.WithTimeout(routeTablePropagationTimeout))
	if err != nil {
		return nil, err
	}
	return &RouteTableAssociationOutput{RouteTableAssociationId: id}, nil
}

// waitAssociated polls the association until it reaches associated. An
// association that enters the failed state stops the wait with the status
// message EC2 reports, since it will not become associated. A not-found is
// tolerated for a long run during this settling window: a just-made association
// can be absent from a describe before it appears, and counting consecutive
// not-founds keeps the wait from ending while the association is still on its
// way.
func (r *RouteTableAssociation) waitAssociated(
	ctx context.Context, client *ec2.Client, id string,
) error {
	notFound := 0
	what := fmt.Sprintf("route table association %s to associate", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		state, found, err := statusRouteTableAssociation(ctx, client, id)
		if err != nil {
			return false, err
		}
		if !found {
			notFound++
			if notFound > routeTableAssociationNotFoundChecks {
				return false, runtime.ErrNotFound
			}
			return false, nil
		}
		notFound = 0
		switch state {
		case ec2types.RouteTableAssociationStateCodeAssociated:
			return true, nil
		case ec2types.RouteTableAssociationStateCodeAssociating:
			return false, nil
		default:
			return false, routeTableAssociationStateError(ctx, client, id, state)
		}
	}, wait.WithTimeout(routeTablePropagationTimeout))
}

// waitDisassociated polls the association until it is gone. An association still
// disassociating or still associated is pending; a failed state stops the wait
// with the status message EC2 reports. A not-found means the disassociation
// finished, as does a disassociated record, which is logically gone even before
// EC2 removes it.
func (r *RouteTableAssociation) waitDisassociated(
	ctx context.Context, client *ec2.Client, id string,
) error {
	what := fmt.Sprintf("route table association %s to disassociate", id)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		state, found, err := statusRouteTableAssociation(ctx, client, id)
		if err != nil {
			return false, err
		}
		if !found {
			return true, nil
		}
		switch state {
		case ec2types.RouteTableAssociationStateCodeDisassociated:
			return true, nil
		case ec2types.RouteTableAssociationStateCodeDisassociating,
			ec2types.RouteTableAssociationStateCodeAssociated:
			return false, nil
		default:
			return false, routeTableAssociationStateError(ctx, client, id, state)
		}
	}, wait.WithTimeout(routeTablePropagationTimeout))
}

// statusRouteTableAssociation returns the association's state for the waiters. A
// not-found is reported as found=false so a waiter counts it rather than erroring
// mid-wait. A nil association state, which an ISO partition can return, is treated
// as associated.
func statusRouteTableAssociation(
	ctx context.Context, client *ec2.Client, id string,
) (ec2types.RouteTableAssociationStateCode, bool, error) {
	assoc, found, err := findRouteTableAssociationRaw(ctx, client, id)
	if err != nil {
		return "", false, err
	}
	if !found {
		return "", false, nil
	}
	if assoc.AssociationState == nil {
		return ec2types.RouteTableAssociationStateCodeAssociated, true, nil
	}
	return assoc.AssociationState.State, true, nil
}

// findRouteTableAssociation finds the live association by id and reports whether
// it was found. A disassociated association is logically gone and reported as not
// found, so a read of a lingering disassociated record maps to not-found.
func findRouteTableAssociation(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.RouteTableAssociation, bool, error) {
	assoc, found, err := findRouteTableAssociationRaw(ctx, client, id)
	if err != nil || !found {
		return nil, false, err
	}
	if assoc.AssociationState != nil &&
		assoc.AssociationState.State == ec2types.RouteTableAssociationStateCodeDisassociated {
		return nil, false, nil
	}
	return assoc, true, nil
}

// findRouteTableAssociationRaw scans for the association by id without filtering
// on its state. It describes route tables filtered by the association id, then
// walks each returned table's associations for the one whose id matches. A filter
// that returns no table, or no table with a matching association, reports
// found=false. The filter form names the association id as a filter value, never
// a route table id request parameter, so a gone table yields no result rather
// than InvalidRouteTableID.NotFound.
func findRouteTableAssociationRaw(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.RouteTableAssociation, bool, error) {
	in := &ec2.DescribeRouteTablesInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("association.route-table-association-id"),
			Values: []string{id},
		}},
	}
	pager := ec2.NewDescribeRouteTablesPaginator(client, in)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, false, fmt.Errorf("describe route tables: %w", err)
		}
		for i := range page.RouteTables {
			for j := range page.RouteTables[i].Associations {
				assoc := page.RouteTables[i].Associations[j]
				if aws.ToString(assoc.RouteTableAssociationId) == id {
					return &assoc, true, nil
				}
			}
		}
	}
	return nil, false, nil
}

// routeTableAssociationStateError builds the error a waiter returns when an
// association reaches a state outside its pending set, such as failed. It reads
// the association's status message to name the reason; when none is available the
// state alone names the failure. A failed association aborts the wait rather than
// ending it silently.
func routeTableAssociationStateError(
	ctx context.Context, client *ec2.Client, id string,
	state ec2types.RouteTableAssociationStateCode,
) error {
	assoc, found, err := findRouteTableAssociationRaw(ctx, client, id)
	if err == nil && found && assoc.AssociationState != nil {
		if msg := aws.ToString(assoc.AssociationState.StatusMessage); msg != "" {
			return fmt.Errorf("route table association %s entered state %s: %s", id, state, msg)
		}
	}
	return fmt.Errorf("route table association %s entered state %s", id, state)
}

// isRouteTableNotFound reports whether an AssociateRouteTable error is the
// transient not-found EC2 raises when a just-created route table is not yet
// visible. EC2 raises it by service code on an HTTP 400, so it is matched the
// same way as any other EC2 not-found.
func isRouteTableNotFound(err error) bool {
	return isNotFound(err, "InvalidRouteTableID.NotFound")
}
