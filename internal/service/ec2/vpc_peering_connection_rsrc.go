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
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type VpcPeeringConnectionResource struct {
	VpcId       string                `ub:"vpc-id"`
	PeerVpcId   string                `ub:"peer-vpc-id"`
	PeerOwnerId *string               `ub:"peer-owner-id"`
	PeerRegion  *string               `ub:"peer-region"`
	AutoAccept  *bool                 `ub:"auto-accept"`
	Accepter    *VpcPeeringDnsOptions `ub:"accepter"`
	Requester   *VpcPeeringDnsOptions `ub:"requester"`
	Tags        *map[string]string    `ub:"tags"`
}

type VpcPeeringDnsOptions struct {
	AllowRemoteVpcDnsResolution *bool `ub:"allow-remote-vpc-dns-resolution"`
}

type VpcPeeringConnectionResourceOutput struct {
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

type peeringPrior = runtime.Prior[
	VpcPeeringConnectionResource, *VpcPeeringConnectionResourceOutput, *awsCfg,
]

type peeringClient struct {
	ec2          *ec2.Client
	config       aws.Config
	stsEndpoint  string
	pollInterval time.Duration
}

const peeringNotFound = "InvalidVpcPeeringConnectionID.NotFound"

func (r VpcPeeringConnectionResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.VpcId, nil)).
			Message("vpc-id must be a non-null string"),
		constraint.Must(constraint.NotEquals(r.PeerVpcId, nil)).
			Message("peer-vpc-id must be a non-null string"),
		constraint.When(constraint.IsTrue(r.AutoAccept)).
			Require(constraint.Not(constraint.NotEmpty(r.PeerRegion))).
			Message("auto-accept requires peer-region to be omitted, null, or empty"),
	}
}

func (*VpcPeeringConnectionResource) ResourceDefinition() runtime.ResourceDefinition[
	VpcPeeringConnectionResource, *VpcPeeringConnectionResourceOutput, *awsCfg,
] {
	peerOwner := runtime.InputField(
		func(input *VpcPeeringConnectionResource) **string { return &input.PeerOwnerId },
	)
	peerRegion := runtime.InputField(
		func(input *VpcPeeringConnectionResource) **string { return &input.PeerRegion },
	)
	return runtime.ResourceDefinition[
		VpcPeeringConnectionResource, *VpcPeeringConnectionResourceOutput, *awsCfg,
	]{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[VpcPeeringConnectionResource]{
			runtime.EqualBy(peerOwner, peeringOptionalEqual),
			runtime.EqualBy(peerRegion, peeringOptionalEqual),
		},
		Replace: runtime.Replacement[
			VpcPeeringConnectionResource, *VpcPeeringConnectionResourceOutput, *awsCfg,
		]{
			Fields: []runtime.AnyInputField[VpcPeeringConnectionResource]{
				runtime.InputField(
					func(input *VpcPeeringConnectionResource) *string { return &input.VpcId },
				),
				runtime.InputField(
					func(input *VpcPeeringConnectionResource) *string { return &input.PeerVpcId },
				),
			},
			Rules: []runtime.ReplacementRule[VpcPeeringConnectionResource]{
				runtime.ReplaceWhen(peerOwner, peeringExplicitChange),
				runtime.ReplaceWhen(peerRegion, peeringExplicitChange),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(
			_ VpcPeeringConnectionResource, output *VpcPeeringConnectionResourceOutput,
		) (string, error) {
			if output == nil {
				return "", runtime.ErrNotFound
			}
			return output.VpcPeeringConnectionId, nil
		},
	}
}

func peeringOptionalEqual(prior, desired *string) bool {
	return aws.ToString(prior) == aws.ToString(desired)
}

func peeringExplicitChange(prior, desired *string) bool {
	return aws.ToString(desired) != "" && !peeringOptionalEqual(prior, desired)
}

func (r *VpcPeeringConnectionResource) createRequest() *ec2.CreateVpcPeeringConnectionInput {
	tags, _ := tagsync.Diff(nil, ptr.Value(r.Tags))
	in := &ec2.CreateVpcPeeringConnectionInput{
		VpcId:             aws.String(r.VpcId),
		PeerVpcId:         aws.String(r.PeerVpcId),
		TagSpecifications: tagSpecifications(ec2types.ResourceTypeVpcPeeringConnection, tags),
	}
	if aws.ToString(r.PeerOwnerId) != "" {
		in.PeerOwnerId = r.PeerOwnerId
	}
	if aws.ToString(r.PeerRegion) != "" {
		in.PeerRegion = r.PeerRegion
	}
	return in
}

func (r *VpcPeeringConnectionResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*VpcPeeringConnectionResourceOutput, error) {
	client, err := newPeeringClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client)
}

func (r *VpcPeeringConnectionResource) create(
	ctx context.Context, client *peeringClient,
) (*VpcPeeringConnectionResourceOutput, error) {
	resp, err := client.ec2.CreateVpcPeeringConnection(ctx, r.createRequest())
	if err != nil {
		return nil, fmt.Errorf("create VPC peering connection: %w", err)
	}
	if resp.VpcPeeringConnection == nil ||
		aws.ToString(resp.VpcPeeringConnection.VpcPeeringConnectionId) == "" {
		return nil, errors.New("create VPC peering connection returned no connection ID")
	}
	id := aws.ToString(resp.VpcPeeringConnection.VpcPeeringConnectionId)
	conn, err := client.waitCreated(ctx, id)
	if err == nil {
		var output *VpcPeeringConnectionResourceOutput
		output, err = r.reconcile(ctx, client, id, conn)
		if err == nil {
			return output, nil
		}
	}
	return nil, fmt.Errorf("complete creation of VPC peering connection %s: %w", id, err)
}

func (*VpcPeeringConnectionResource) Read(
	ctx context.Context, cfg *awsCfg, prior peeringPrior,
) (*VpcPeeringConnectionResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.VpcPeeringConnectionId == "" {
		return nil, runtime.ErrNotFound
	}
	client, err := newPeeringClient(ctx, peeringPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	return client.read(ctx, prior.Outputs.VpcPeeringConnectionId)
}

func (r *VpcPeeringConnectionResource) Update(
	ctx context.Context, cfg *awsCfg, prior peeringPrior,
) (*VpcPeeringConnectionResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.VpcPeeringConnectionId == "" {
		return nil, errors.New("update VPC peering connection requires a saved connection ID")
	}
	client, err := newPeeringClient(ctx, peeringConfigInRegion(cfg, prior.Outputs.Region))
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior)
}

func (r *VpcPeeringConnectionResource) update(
	ctx context.Context, client *peeringClient, prior peeringPrior,
) (*VpcPeeringConnectionResourceOutput, error) {
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
	return r.reconcile(ctx, client, id, conn)
}

func (*VpcPeeringConnectionResource) Delete(
	ctx context.Context, cfg *awsCfg, prior peeringPrior,
) error {
	if prior.Outputs == nil || prior.Outputs.VpcPeeringConnectionId == "" {
		return nil
	}
	client, err := newPeeringClient(ctx, peeringPriorConfig(cfg, prior))
	if err != nil {
		return err
	}
	return client.delete(ctx, prior.Outputs.VpcPeeringConnectionId)
}

func peeringPriorConfig(cfg *awsCfg, prior peeringPrior) *awsCfg {
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	return peeringConfigInRegion(cfg, prior.Outputs.Region)
}

func peeringConfigInRegion(cfg *awsCfg, savedRegion string) *awsCfg {
	if savedRegion == "" {
		return cfg
	}
	copy := ptr.Value(cfg)
	copy.Region = aws.String(savedRegion)
	return &copy
}

func newPeeringClient(ctx context.Context, cfg *awsCfg) (*peeringClient, error) {
	sdkCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &peeringClient{
		ec2:          ec2.NewFromConfig(sdkCfg),
		config:       sdkCfg,
		stsEndpoint:  cfg.STSEndpoint(),
		pollInterval: time.Second,
	}, nil
}

func (c *peeringClient) read(
	ctx context.Context, id string,
) (*VpcPeeringConnectionResourceOutput, error) {
	conn, err := c.lookup(ctx, id, true)
	if err != nil {
		return nil, err
	}
	requester, accepter := conn.RequesterVpcInfo, conn.AccepterVpcInfo
	local, peer := requester, accepter
	if aws.ToString(requester.OwnerId) != aws.ToString(accepter.OwnerId) {
		owner, err := c.accountID(ctx)
		if err != nil {
			return nil, err
		}
		if owner == aws.ToString(accepter.OwnerId) {
			local, peer = accepter, requester
		}
	}
	tags := make(map[string]string, len(conn.Tags))
	for _, entry := range conn.Tags {
		key := aws.ToString(entry.Key)
		if !strings.HasPrefix(key, "aws:") {
			tags[key] = aws.ToString(entry.Value)
		}
	}
	return &VpcPeeringConnectionResourceOutput{
		VpcPeeringConnectionId: id,
		VpcId:                  aws.ToString(local.VpcId),
		PeerVpcId:              aws.ToString(peer.VpcId),
		PeerOwnerId:            aws.ToString(peer.OwnerId),
		PeerRegion:             aws.ToString(accepter.Region),
		AcceptStatus:           string(conn.Status.Code),
		Region:                 c.config.Region,
		Accepter:               peeringOptionsOutput(accepter.PeeringOptions),
		Requester:              peeringOptionsOutput(requester.PeeringOptions),
		Tags:                   tags,
	}, nil
}

func (c *peeringClient) accountID(ctx context.Context) (string, error) {
	if c.config.Credentials != nil {
		creds, err := c.config.Credentials.Retrieve(ctx)
		if err == nil && creds.AccountID != "" {
			return creds.AccountID, nil
		}
	}
	client := sts.NewFromConfig(c.config, func(o *sts.Options) {
		if c.stsEndpoint != "" {
			o.BaseEndpoint = aws.String(c.stsEndpoint)
		}
	})
	resp, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("get caller identity for VPC peering connection: %w", err)
	}
	if aws.ToString(resp.Account) == "" {
		return "", errors.New("get caller identity for VPC peering connection returned no account")
	}
	return *resp.Account, nil
}

func (c *peeringClient) lookup(
	ctx context.Context, id string, strict bool,
) (*ec2types.VpcPeeringConnection, error) {
	input := &ec2.DescribeVpcPeeringConnectionsInput{VpcPeeringConnectionIds: []string{id}}
	pages := ec2.NewDescribeVpcPeeringConnectionsPaginator(c.ec2, input)
	var conn *ec2types.VpcPeeringConnection
	var count int
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if isNotFound(err, peeringNotFound) {
			return nil, runtime.ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("describe VPC peering connection %s: %w", id, err)
		}
		n := len(page.VpcPeeringConnections)
		if count == 0 && n == 1 {
			conn = &page.VpcPeeringConnections[0]
		}
		count += n
	}
	if count != 1 || conn.RequesterVpcInfo == nil || conn.AccepterVpcInfo == nil ||
		conn.Status == nil {
		return nil, runtime.ErrNotFound
	}
	if strict {
		if aws.ToString(conn.VpcPeeringConnectionId) != id {
			return nil, runtime.ErrNotFound
		}
		switch conn.Status.Code {
		case ec2types.VpcPeeringConnectionStateReasonCodeDeleted,
			ec2types.VpcPeeringConnectionStateReasonCodeExpired,
			ec2types.VpcPeeringConnectionStateReasonCodeFailed,
			ec2types.VpcPeeringConnectionStateReasonCodeRejected:
			return nil, runtime.ErrNotFound
		}
	}
	return conn, nil
}
