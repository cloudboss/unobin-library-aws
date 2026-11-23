package ec2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type VpcPeeringConnectionAccepterResource struct {
	VpcPeeringConnectionId string                `ub:"vpc-peering-connection-id"`
	AutoAccept             *bool                 `ub:"auto-accept"`
	Accepter               *VpcPeeringDnsOptions `ub:"accepter"`
	Requester              *VpcPeeringDnsOptions `ub:"requester"`
	Tags                   *map[string]string    `ub:"tags"`
}

type VpcPeeringConnectionAccepterResourceOutput struct {
	VpcPeeringConnectionId string                `ub:"vpc-peering-connection-id"`
	VpcId                  string                `ub:"vpc-id"`
	PeerVpcId              string                `ub:"peer-vpc-id"`
	PeerOwnerId            string                `ub:"peer-owner-id"`
	PeerRegion             string                `ub:"peer-region"`
	AcceptStatus           string                `ub:"accept-status"`
	Region                 string                `ub:"region"`
	Accepter               *VpcPeeringDnsOptions `ub:"accepter"`
	Requester              *VpcPeeringDnsOptions `ub:"requester"`
	Tags                   map[string]string     `ub:"tags"`
}

type peeringAccepterPrior = runtime.Prior[
	VpcPeeringConnectionAccepterResource, *VpcPeeringConnectionAccepterResourceOutput, *awsCfg,
]

func (r VpcPeeringConnectionAccepterResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.VpcPeeringConnectionId, nil)).
			Message("vpc-peering-connection-id must be a non-null string"),
	}
}

func (*VpcPeeringConnectionAccepterResource) ResourceDefinition() runtime.ResourceDefinition[
	VpcPeeringConnectionAccepterResource, *VpcPeeringConnectionAccepterResourceOutput, *awsCfg,
] {
	return runtime.ResourceDefinition[
		VpcPeeringConnectionAccepterResource, *VpcPeeringConnectionAccepterResourceOutput, *awsCfg,
	]{
		SchemaVersion: 1,
		Replace: runtime.Replacement[
			VpcPeeringConnectionAccepterResource, *VpcPeeringConnectionAccepterResourceOutput, *awsCfg,
		]{
			Fields: []runtime.AnyInputField[VpcPeeringConnectionAccepterResource]{
				runtime.InputField(func(r *VpcPeeringConnectionAccepterResource) *string {
					return &r.VpcPeeringConnectionId
				}),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(
			_ VpcPeeringConnectionAccepterResource, out *VpcPeeringConnectionAccepterResourceOutput,
		) (string, error) {
			if out == nil {
				return "", runtime.ErrNotFound
			}
			return out.VpcPeeringConnectionId, nil
		},
	}
}

func (r *VpcPeeringConnectionAccepterResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*VpcPeeringConnectionAccepterResourceOutput, error) {
	client, err := newPeeringClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client)
}

func (r *VpcPeeringConnectionAccepterResource) create(
	ctx context.Context, client *peeringClient,
) (*VpcPeeringConnectionAccepterResourceOutput, error) {
	id := r.VpcPeeringConnectionId
	conn, err := client.lookup(ctx, id, true)
	if err != nil {
		return nil, fmt.Errorf("adopt VPC peering connection %s: %w", id, err)
	}
	if err := r.acceptAndModify(ctx, client, id, conn); err != nil {
		return nil, err
	}
	tags, _ := tagsync.Diff(nil, ptr.Value(r.Tags))
	if len(tags) > 0 {
		err := client.poll(ctx, "creation tags", id, peeringOptionsTimeout,
			func(ctx context.Context) (bool, error) {
				_, err := client.ec2.CreateTags(ctx, &ec2.CreateTagsInput{
					Resources: []string{id}, Tags: mapToTags(tags),
				})
				var apiErr smithy.APIError
				if errors.As(err, &apiErr) && strings.Contains(apiErr.ErrorCode(), ".NotFound") {
					return false, nil
				}
				return err == nil, err
			})
		if err != nil {
			return nil, fmt.Errorf("create tags for VPC peering connection %s: %w", id, err)
		}
	}
	return client.readAccepter(ctx, id)
}

func (*VpcPeeringConnectionAccepterResource) Read(
	ctx context.Context, cfg *awsCfg, prior peeringAccepterPrior,
) (*VpcPeeringConnectionAccepterResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.VpcPeeringConnectionId == "" {
		return nil, runtime.ErrNotFound
	}
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	client, err := newPeeringClient(ctx, peeringConfigInRegion(cfg, prior.Outputs.Region))
	if err != nil {
		return nil, err
	}
	return client.readAccepter(ctx, prior.Outputs.VpcPeeringConnectionId)
}

func (r *VpcPeeringConnectionAccepterResource) Update(
	ctx context.Context, cfg *awsCfg, prior peeringAccepterPrior,
) (*VpcPeeringConnectionAccepterResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.VpcPeeringConnectionId == "" {
		return nil, errors.New("update VPC peering connection accepter requires a saved connection ID")
	}
	client, err := newPeeringClient(ctx, peeringConfigInRegion(cfg, prior.Outputs.Region))
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior)
}

func (r *VpcPeeringConnectionAccepterResource) update(
	ctx context.Context, client *peeringClient, prior peeringAccepterPrior,
) (*VpcPeeringConnectionAccepterResourceOutput, error) {
	id := prior.Outputs.VpcPeeringConnectionId
	known := prior.Outputs.Tags
	if prior.Observed != nil {
		known = prior.Observed.Tags
	}
	err := syncEC2Tags(ctx, client.ec2, id, ptr.Value(r.Tags),
		func(context.Context) (map[string]string, error) { return known, nil })
	if err != nil {
		return nil, err
	}
	conn, err := client.lookup(ctx, id, true)
	if err != nil {
		return nil, fmt.Errorf("look up VPC peering connection %s for update: %w", id, err)
	}
	if err := r.acceptAndModify(ctx, client, id, conn); err != nil {
		return nil, err
	}
	return client.readAccepter(ctx, id)
}

func (*VpcPeeringConnectionAccepterResource) Delete(
	ctx context.Context, cfg *awsCfg, prior peeringAccepterPrior,
) error {
	return nil
}

func (c *peeringClient) readAccepter(
	ctx context.Context, id string,
) (*VpcPeeringConnectionAccepterResourceOutput, error) {
	out, err := c.read(ctx, id)
	return (*VpcPeeringConnectionAccepterResourceOutput)(out), err
}

func (r *VpcPeeringConnectionAccepterResource) acceptAndModify(
	ctx context.Context, c *peeringClient, id string, conn *ec2types.VpcPeeringConnection,
) error {
	if aws.ToBool(r.AutoAccept) &&
		conn.Status.Code == ec2types.VpcPeeringConnectionStateReasonCodePendingAcceptance {
		_, err := c.ec2.AcceptVpcPeeringConnection(ctx, &ec2.AcceptVpcPeeringConnectionInput{
			VpcPeeringConnectionId: aws.String(id),
		})
		if err != nil {
			return fmt.Errorf("accept VPC peering connection %s: %w", id, err)
		}
		conn, err = c.waitCreated(ctx, id)
		if err != nil {
			return err
		}
	}
	options := &VpcPeeringConnectionResource{Accepter: r.Accepter, Requester: r.Requester}
	return options.modifyOptions(ctx, c, id, conn)
}
