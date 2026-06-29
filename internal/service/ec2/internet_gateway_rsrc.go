package ec2

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// internetGatewayAttachStateAvailable is the attachment state EC2 reports for an
// internet gateway that is attached to a VPC. EC2 names this state available,
// not attached: every other EC2 attachment uses the AttachmentStatus value
// attached, but an internet gateway attachment is the exception and reaches
// available. The attach waiter must target this literal, not the SDK enum, or
// it would wait for a state the gateway never reports.
const internetGatewayAttachStateAvailable = "available"

// InternetGateway is an EC2 internet gateway: the device that gives a VPC a path
// to the public internet. The gateway itself has no settings beyond its tags;
// its one reconciled property is the VPC it is attached to. The attachment has
// no create-time setting, so vpc-id is an optional field applied after the
// gateway exists: an unset vpc-id leaves the gateway unattached, and a set value
// is reconciled by attaching the gateway to that VPC. A change to vpc-id detaches
// the old VPC and attaches the new one in place, so nothing on the gateway forces
// a replacement.
type InternetGateway struct {
	VpcId *string            `ub:"vpc-id"`
	Tags  *map[string]string `ub:"tags"`
}

// InternetGatewayOutput holds the values EC2 computes for an internet gateway.
// The id is the gateway's handle, and the owner id is the account that owns it.
// The attached VPC is reported under the same name as the input field on
// purpose: the read-back reflects the cloud's actual attachment, so an
// out-of-band detach reads back as an empty vpc-id and shows up as drift, which
// schedules an Update that re-attaches. When the gateway is unattached the
// read-back is the empty string.
type InternetGatewayOutput struct {
	InternetGatewayId string `ub:"internet-gateway-id"`
	OwnerId           string `ub:"owner-id"`
	VpcId             string `ub:"vpc-id"`
}

func (r *InternetGateway) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when an internet gateway is created.
// The gateway has none: its tags change in place, and its VPC attachment is
// reconciled by detach and attach rather than by replacing the gateway.
func (r *InternetGateway) ReplaceFields() []string { return nil }

func (r *InternetGateway) Create(ctx context.Context, cfg *awsCfg) (*InternetGatewayOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateInternetGateway(ctx, &ec2.CreateInternetGatewayInput{
		TagSpecifications: tagSpecifications(ec2types.ResourceTypeInternetGateway, ptr.Value(r.Tags)),
	})
	if err != nil {
		return nil, fmt.Errorf("create internet gateway: %w", err)
	}
	id := aws.ToString(resp.InternetGateway.InternetGatewayId)
	// A new gateway starts unattached, so reconcile its attachment from no
	// current attachment toward the desired vpc-id.
	if err := r.reconcileAttachment(ctx, client, id, "", aws.ToString(r.VpcId)); err != nil {
		return nil, err
	}
	// CreateInternetGateway returns before the gateway is fully visible and does
	// not report the owner id or the settled attachment, so read settles those
	// values. The read rides out the brief post-create window where a describe can
	// still report the gateway not-found.
	return r.read(ctx, client, id, true)
}

func (r *InternetGateway) Read(
	ctx context.Context, cfg *awsCfg, prior *InternetGatewayOutput,
) (*InternetGatewayOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.InternetGatewayId, false)
}

func (r *InternetGateway) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[InternetGateway, *InternetGatewayOutput],
) (*InternetGatewayOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.InternetGatewayId
	// Reconcile the attachment against the cloud's actual current attachment, not
	// the recorded input, so the update is idempotent and also heals an
	// out-of-band detach. A fresh describe is the source of truth here: apply does
	// not re-read before Update, so the plan-time observed value may be stale.
	gateway, err := describeInternetGateway(ctx, client, id)
	if err != nil {
		return nil, err
	}
	current := internetGatewayAttachedVpc(gateway)
	if err := r.reconcileAttachment(ctx, client, id, current, aws.ToString(r.VpcId)); err != nil {
		return nil, err
	}
	// The attachment calls do not touch tags, so reconcile them as a set whenever
	// they changed, the same as the other EC2 resources.
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := syncTags(ctx, client, id, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, id, false)
}

func (r *InternetGateway) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *InternetGatewayOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	id := prior.InternetGatewayId
	// A gateway cannot be deleted while it is still attached, so detach it first.
	// The detach is best-effort: an already-detached or already-gone gateway is
	// reported as not-found, which is not a failure here. The attached VPC comes
	// from a fresh describe so the detach names the VPC the gateway is actually
	// attached to, even if that drifted from the recorded attachment.
	gateway, err := describeInternetGateway(ctx, client, id)
	if err != nil {
		if err == runtime.ErrNotFound {
			return nil
		}
		return err
	}
	if vpcID := internetGatewayAttachedVpc(gateway); vpcID != "" {
		if err := r.detach(ctx, client, id, vpcID); err != nil && err != runtime.ErrNotFound {
			return err
		}
	}
	// A gateway whose VPC still holds instances with public addresses, in-use
	// elastic IPs, or NAT gateways cannot be deleted yet; EC2 reports that as
	// DependencyViolation, which clears once those release their hold on the
	// public path. Retry the delete through that window. A gateway that is already
	// gone is a successful delete with nothing to do.
	err = retry.OnError(ctx, isDependencyViolation, func(ctx context.Context) error {
		_, err := client.DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{
			InternetGatewayId: aws.String(id),
		})
		return err
	}, retry.WithTimeout(20*time.Minute))
	if err != nil {
		if isNotFound(err, "InvalidInternetGatewayID.NotFound") {
			return nil
		}
		return fmt.Errorf("delete internet gateway: %w", err)
	}
	return nil
}

// reconcileAttachment moves the gateway's attachment from current to desired,
// where each is a VPC id or the empty string for unattached. It detaches the old
// VPC before attaching the new one, and does nothing when the two already match,
// so applying the same desired value twice issues no call.
func (r *InternetGateway) reconcileAttachment(
	ctx context.Context, client *ec2.Client, id, current, desired string,
) error {
	if current == desired {
		return nil
	}
	if current != "" {
		if err := r.detach(ctx, client, id, current); err != nil && err != runtime.ErrNotFound {
			return err
		}
	}
	if desired != "" {
		if err := r.attach(ctx, client, id, desired); err != nil {
			return err
		}
	}
	return nil
}

// attach attaches the gateway to vpcID and waits for the attachment to reach the
// available state. The AttachInternetGateway call is retried while the
// just-created gateway is not yet visible, since a create-time propagation lag
// can briefly report it not-found.
func (r *InternetGateway) attach(ctx context.Context, client *ec2.Client, id, vpcID string) error {
	err := retry.OnError(ctx, isInternetGatewayNotFound, func(ctx context.Context) error {
		_, err := client.AttachInternetGateway(ctx, &ec2.AttachInternetGatewayInput{
			InternetGatewayId: aws.String(id),
			VpcId:             aws.String(vpcID),
		})
		return err
	}, retry.WithTimeout(20*time.Minute), retry.WithInterval(time.Second))
	if err != nil {
		return fmt.Errorf("attach internet gateway: %w", err)
	}
	return r.waitAttached(ctx, client, id, vpcID)
}

// detach detaches the gateway from vpcID and waits for the attachment to clear.
// An already-detached or already-gone gateway, which EC2 reports as
// Gateway.NotAttached or InvalidInternetGatewayID.NotFound, is treated as the
// detach having already happened and returns runtime.ErrNotFound so callers can
// tell it apart from a real failure. The DetachInternetGateway call is retried
// while a dependency still holds the public path, which EC2 reports as
// DependencyViolation, over the same long window the delete uses.
func (r *InternetGateway) detach(ctx context.Context, client *ec2.Client, id, vpcID string) error {
	err := retry.OnError(ctx, isDependencyViolation, func(ctx context.Context) error {
		_, err := client.DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
			InternetGatewayId: aws.String(id),
			VpcId:             aws.String(vpcID),
		})
		return err
	}, retry.WithTimeout(20*time.Minute))
	if err != nil {
		if isNotFound(err, "Gateway.NotAttached", "InvalidInternetGatewayID.NotFound") {
			return runtime.ErrNotFound
		}
		return fmt.Errorf("detach internet gateway: %w", err)
	}
	return r.waitDetached(ctx, client, id, vpcID)
}

// waitAttached polls until the gateway's attachment to vpcID reads as available.
// A just-attached gateway can briefly describe as not-found, so a not-found is
// treated as not-ready and does not abort the wait; only the timeout bounds it.
func (r *InternetGateway) waitAttached(
	ctx context.Context, client *ec2.Client, id, vpcID string,
) error {
	what := fmt.Sprintf("internet gateway %s attachment to %s", id, vpcID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		gateway, err := describeInternetGateway(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		for _, att := range gateway.Attachments {
			if aws.ToString(att.VpcId) != vpcID {
				continue
			}
			return string(att.State) == internetGatewayAttachStateAvailable, nil
		}
		return false, nil
	}, wait.WithTimeout(5*time.Minute), wait.WithInterval(time.Second))
}

// waitDetached polls until the gateway no longer reports an attachment to vpcID.
// A gateway that has since gone is also detached, so a not-found is ready. While
// the detach settles the attachment passes through the available and detaching
// states, both of which read as not-ready.
func (r *InternetGateway) waitDetached(
	ctx context.Context, client *ec2.Client, id, vpcID string,
) error {
	what := fmt.Sprintf("internet gateway %s detachment from %s", id, vpcID)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		gateway, err := describeInternetGateway(ctx, client, id)
		if err != nil {
			if err == runtime.ErrNotFound {
				return true, nil
			}
			return false, err
		}
		for _, att := range gateway.Attachments {
			if aws.ToString(att.VpcId) == vpcID {
				return false, nil
			}
		}
		return true, nil
	}, wait.WithTimeout(5*time.Minute), wait.WithInterval(time.Second))
}

// read fetches the gateway by id and returns its computed outputs, including the
// VPC it is attached to. When created is true the gateway was just made, so a
// not-found means it has not propagated yet and read waits for it to appear;
// otherwise a not-found is drift and maps to runtime.ErrNotFound at once.
func (r *InternetGateway) read(
	ctx context.Context, client *ec2.Client, id string, created bool,
) (*InternetGatewayOutput, error) {
	var gateway *ec2types.InternetGateway
	err := wait.Until(ctx, fmt.Sprintf("internet gateway %s", id),
		func(ctx context.Context) (bool, error) {
			g, err := describeInternetGateway(ctx, client, id)
			if err != nil {
				if err == runtime.ErrNotFound {
					if created {
						return false, nil
					}
					return false, runtime.ErrNotFound
				}
				return false, err
			}
			gateway = g
			return true, nil
		}, wait.WithTimeout(5*time.Minute))
	if err != nil {
		return nil, err
	}
	return &InternetGatewayOutput{
		InternetGatewayId: aws.ToString(gateway.InternetGatewayId),
		OwnerId:           aws.ToString(gateway.OwnerId),
		VpcId:             internetGatewayAttachedVpc(gateway),
	}, nil
}

// internetGatewayAttachedVpc returns the id of the VPC the gateway is attached
// to, or the empty string when it is unattached. An internet gateway attaches to
// at most one VPC, so the first attachment is the attachment.
func internetGatewayAttachedVpc(gateway *ec2types.InternetGateway) string {
	for _, att := range gateway.Attachments {
		if vpcID := aws.ToString(att.VpcId); vpcID != "" {
			return vpcID
		}
	}
	return ""
}

// describeInternetGateway fetches the gateway with the given id. EC2 reports a
// missing gateway by service code on an HTTP 400, never a 404, so the not-found
// code maps to runtime.ErrNotFound; an empty result or an id mismatch means the
// same. The id check guards against a lagging replica echoing a stale gateway
// right after create.
func describeInternetGateway(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.InternetGateway, error) {
	resp, err := client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		InternetGatewayIds: []string{id},
	})
	if err != nil {
		if isInternetGatewayNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe internet gateways: %w", err)
	}
	if len(resp.InternetGateways) == 0 {
		return nil, runtime.ErrNotFound
	}
	gateway := resp.InternetGateways[0]
	if aws.ToString(gateway.InternetGatewayId) != id {
		return nil, runtime.ErrNotFound
	}
	return &gateway, nil
}

// isInternetGatewayNotFound reports whether err is the EC2 service code for a
// gateway that does not exist. It is matched both to map a describe of a gone
// gateway to runtime.ErrNotFound and to retry an attach against a just-created
// gateway that has not yet propagated.
func isInternetGatewayNotFound(err error) bool {
	return isNotFound(err, "InvalidInternetGatewayID.NotFound")
}
