package ec2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	peeringLifecycleTimeout = time.Minute
	peeringOptionsTimeout   = 5 * time.Minute
	peeringMissLimit        = 20
)

func (c *peeringClient) waitCreated(
	ctx context.Context, id string,
) (*ec2types.VpcPeeringConnection, error) {
	var result *ec2types.VpcPeeringConnection
	var targets, misses int
	err := c.poll(ctx, "creation or acceptance", id, peeringLifecycleTimeout,
		func(ctx context.Context) (bool, error) {
			conn, err := c.lookup(ctx, id, false)
			if errors.Is(err, runtime.ErrNotFound) {
				misses++
				if misses > peeringMissLimit {
					return false, fmt.Errorf("connection was absent for %d consecutive reads", misses)
				}
				return false, nil
			}
			if err != nil {
				return false, err
			}
			misses = 0
			switch conn.Status.Code {
			case ec2types.VpcPeeringConnectionStateReasonCodeInitiatingRequest,
				ec2types.VpcPeeringConnectionStateReasonCodeProvisioning:
				targets = 0
			case ec2types.VpcPeeringConnectionStateReasonCodeActive,
				ec2types.VpcPeeringConnectionStateReasonCodePendingAcceptance:
				targets++
				result = conn
			default:
				return false, peeringUnexpectedStatus(conn.Status)
			}
			return targets == 2, nil
		})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *VpcPeeringConnectionResource) reconcile(
	ctx context.Context, client *peeringClient, id string, conn *ec2types.VpcPeeringConnection,
) (*VpcPeeringConnectionResourceOutput, error) {
	if aws.ToBool(r.AutoAccept) &&
		conn.Status.Code == ec2types.VpcPeeringConnectionStateReasonCodePendingAcceptance {
		_, err := client.ec2.AcceptVpcPeeringConnection(ctx, &ec2.AcceptVpcPeeringConnectionInput{
			VpcPeeringConnectionId: aws.String(id),
		})
		if err != nil {
			return nil, fmt.Errorf("accept VPC peering connection %s: %w", id, err)
		}
		conn, err = client.waitCreated(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	if err := r.modifyOptions(ctx, client, id, conn); err != nil {
		return nil, err
	}
	output, err := client.read(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("final read of VPC peering connection %s: %w", id, err)
	}
	return output, nil
}

func (r *VpcPeeringConnectionResource) modifyOptions(
	ctx context.Context, c *peeringClient, id string, conn *ec2types.VpcPeeringConnection,
) error {
	request := &ec2.ModifyVpcPeeringConnectionOptionsInput{
		VpcPeeringConnectionId: aws.String(id),
		AccepterPeeringConnectionOptions: peeringOptionsChange(
			r.Accepter, conn.AccepterVpcInfo.PeeringOptions),
		RequesterPeeringConnectionOptions: peeringOptionsChange(
			r.Requester, conn.RequesterVpcInfo.PeeringOptions),
	}
	if request.AccepterPeeringConnectionOptions == nil &&
		request.RequesterPeeringConnectionOptions == nil {
		return nil
	}
	switch conn.Status.Code {
	case ec2types.VpcPeeringConnectionStateReasonCodeActive,
		ec2types.VpcPeeringConnectionStateReasonCodeProvisioning:
	default:
		return fmt.Errorf("change DNS options for VPC peering connection %s: "+
			"status %q must be active or provisioning", id, conn.Status.Code)
	}
	if _, err := c.ec2.ModifyVpcPeeringConnectionOptions(ctx, request); err != nil {
		return fmt.Errorf("modify DNS options for VPC peering connection %s: %w", id, err)
	}
	return c.poll(ctx, "DNS options propagation", id, peeringOptionsTimeout,
		func(ctx context.Context) (bool, error) {
			observed, err := c.lookup(ctx, id, true)
			if err != nil {
				return false, err
			}
			return peeringOptionsPropagated(
				request.AccepterPeeringConnectionOptions, observed.AccepterVpcInfo.PeeringOptions,
			) && peeringOptionsPropagated(
				request.RequesterPeeringConnectionOptions, observed.RequesterVpcInfo.PeeringOptions,
			), nil
		})
}

func peeringOptionsChange(
	desired *VpcPeeringDnsOptions, current *ec2types.VpcPeeringConnectionOptionsDescription,
) *ec2types.PeeringConnectionOptionsRequest {
	if desired == nil {
		return nil
	}
	flag := aws.ToBool(desired.AllowRemoteVpcDnsResolution)
	if current != nil && flag == aws.ToBool(current.AllowDnsResolutionFromRemoteVpc) {
		return nil
	}
	return &ec2types.PeeringConnectionOptionsRequest{
		AllowDnsResolutionFromRemoteVpc: aws.Bool(flag),
	}
}

func peeringOptionsPropagated(
	request *ec2types.PeeringConnectionOptionsRequest,
	observed *ec2types.VpcPeeringConnectionOptionsDescription,
) bool {
	return request == nil || observed == nil ||
		aws.ToBool(request.AllowDnsResolutionFromRemoteVpc) ==
			aws.ToBool(observed.AllowDnsResolutionFromRemoteVpc)
}

func peeringOptionsOutput(
	options *ec2types.VpcPeeringConnectionOptionsDescription,
) *VpcPeeringDnsOptions {
	if options == nil {
		return nil
	}
	return &VpcPeeringDnsOptions{AllowRemoteVpcDnsResolution: options.AllowDnsResolutionFromRemoteVpc}
}

func (c *peeringClient) delete(ctx context.Context, id string) error {
	_, err := c.ec2.DeleteVpcPeeringConnection(ctx, &ec2.DeleteVpcPeeringConnectionInput{
		VpcPeeringConnectionId: aws.String(id),
	})
	if err != nil {
		var apiErr smithy.APIError
		if isNotFound(err, peeringNotFound) ||
			(errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidStateTransition" &&
				strings.Contains(apiErr.ErrorMessage(), "to deleting")) {
			return nil
		}
		return fmt.Errorf("delete VPC peering connection %s: %w", id, err)
	}
	return c.waitDeleted(ctx, id)
}

func (c *peeringClient) waitDeleted(ctx context.Context, id string) error {
	return c.poll(ctx, "deletion", id, peeringLifecycleTimeout,
		func(ctx context.Context) (bool, error) {
			conn, err := c.lookup(ctx, id, true)
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			switch conn.Status.Code {
			case ec2types.VpcPeeringConnectionStateReasonCodeActive,
				ec2types.VpcPeeringConnectionStateReasonCodeDeleting,
				ec2types.VpcPeeringConnectionStateReasonCodePendingAcceptance:
				return false, nil
			default:
				return false, peeringUnexpectedStatus(conn.Status)
			}
		})
}

func (c *peeringClient) poll(
	ctx context.Context,
	phase, id string,
	timeout time.Duration,
	probe func(context.Context) (bool, error),
) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	what := fmt.Sprintf("VPC peering connection %s %s", id, phase)
	err := wait.Until(ctx, what, probe, wait.WithTimeout(timeout), wait.WithInterval(c.pollInterval))
	if err != nil {
		return fmt.Errorf("wait for %s: %w", what, err)
	}
	return nil
}

func peeringUnexpectedStatus(status *ec2types.VpcPeeringConnectionStateReason) error {
	return fmt.Errorf("unexpected status %q: %s", status.Code, aws.ToString(status.Message))
}
