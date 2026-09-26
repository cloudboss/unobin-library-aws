package efs

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const (
	mountTargetCreateTimeout = 30 * time.Minute
	mountTargetDeleteTimeout = 10 * time.Minute
)

var (
	mountTargetInitialDelay = 2 * time.Second
	mountTargetPollInterval = 3 * time.Second
	mountTargetCreateLocks  sync.Map
)

type MountTargetResource struct {
	FileSystemId   string    `ub:"file-system-id"`
	SubnetId       string    `ub:"subnet-id"`
	IpAddress      *string   `ub:"ip-address"`
	IpAddressType  *string   `ub:"ip-address-type"`
	Ipv6Address    *string   `ub:"ipv6-address"`
	SecurityGroups *[]string `ub:"security-groups"`
}

type MountTargetResourceOutput struct {
	MountTargetId        string   `ub:"mount-target-id"`
	AvailabilityZoneId   string   `ub:"availability-zone-id"`
	AvailabilityZoneName string   `ub:"availability-zone-name"`
	IpAddress            string   `ub:"ip-address"`
	IpAddressType        string   `ub:"ip-address-type"`
	Ipv6Address          string   `ub:"ipv6-address"`
	MountTargetDNSName   string   `ub:"mount-target-dns-name"`
	NetworkInterfaceId   string   `ub:"network-interface-id"`
	OwnerId              string   `ub:"owner-id"`
	SecurityGroups       []string `ub:"security-groups"`
}

type mountTargetClient interface {
	CreateMountTarget(context.Context, *efs.CreateMountTargetInput,
		...func(*efs.Options)) (*efs.CreateMountTargetOutput, error)
	DescribeMountTargets(context.Context, *efs.DescribeMountTargetsInput,
		...func(*efs.Options)) (*efs.DescribeMountTargetsOutput, error)
	DescribeMountTargetSecurityGroups(context.Context,
		*efs.DescribeMountTargetSecurityGroupsInput,
		...func(*efs.Options)) (*efs.DescribeMountTargetSecurityGroupsOutput, error)
	ModifyMountTargetSecurityGroups(context.Context,
		*efs.ModifyMountTargetSecurityGroupsInput,
		...func(*efs.Options)) (*efs.ModifyMountTargetSecurityGroupsOutput, error)
	DeleteMountTarget(context.Context, *efs.DeleteMountTargetInput,
		...func(*efs.Options)) (*efs.DeleteMountTargetOutput, error)
}

type mountTargetSubnetClient interface {
	DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput,
		...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
}

type mountTargetClientProvider interface {
	EFS() mountTargetClient
	EC2() mountTargetSubnetClient
	Region() string
}

type sdkMountTargetClients struct {
	config aws.Config
}

func newMountTargetClients(
	ctx context.Context,
	cfg *awsCfg,
) (*sdkMountTargetClients, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &sdkMountTargetClients{config: loaded}, nil
}

func (c *sdkMountTargetClients) EFS() mountTargetClient {
	return efs.NewFromConfig(c.config)
}

func (c *sdkMountTargetClients) EC2() mountTargetSubnetClient {
	return ec2.NewFromConfig(c.config)
}

func (c *sdkMountTargetClients) Region() string { return c.config.Region }

func (r *MountTargetResource) SchemaVersion() int { return 1 }

func (r *MountTargetResource) ReplaceFields() []string {
	return []string{
		"file-system-id",
		"subnet-id",
		"ip-address",
		"ip-address-type",
		"ipv6-address",
	}
}

func (r MountTargetResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.IpAddressType)).
			Require(constraint.OneOf(r.IpAddressType,
				"IPV4_ONLY", "IPV6_ONLY", "DUAL_STACK")).
			Message("ip-address-type must be IPV4_ONLY, IPV6_ONLY, or DUAL_STACK"),
	}
}

func (r *MountTargetResource) ValidateInputs(context.Context, *awsCfg) error {
	if r.IpAddress != nil {
		address, err := netip.ParseAddr(*r.IpAddress)
		if err != nil || !address.Is4() {
			return errors.New("ip-address must be a valid IPv4 address")
		}
	}
	if r.Ipv6Address != nil {
		address, err := netip.ParseAddr(*r.Ipv6Address)
		if err != nil || !address.Is6() || address.Is4In6() {
			return errors.New("ipv6-address must be a valid IPv6 address")
		}
	}
	if r.IpAddressType != nil {
		switch efstypes.IpAddressType(*r.IpAddressType) {
		case efstypes.IpAddressTypeIpv4Only,
			efstypes.IpAddressTypeIpv6Only,
			efstypes.IpAddressTypeDualStack:
		default:
			return errors.New(
				"ip-address-type must be IPV4_ONLY, IPV6_ONLY, or DUAL_STACK")
		}
	}
	return nil
}

func (r *MountTargetResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*MountTargetResourceOutput, error) {
	clients, err := newMountTargetClients(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, clients)
}

func (r *MountTargetResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[MountTargetResource, *MountTargetResourceOutput, *awsCfg],
) (*MountTargetResourceOutput, error) {
	prior := recordedPrior.Outputs
	clients, err := newMountTargetClients(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, clients.EFS(), clients.Region(), prior.MountTargetId)
}

func (r *MountTargetResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[MountTargetResource, *MountTargetResourceOutput, *awsCfg],
) (*MountTargetResourceOutput, error) {
	clients, err := newMountTargetClients(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, clients, prior)
}

func (r *MountTargetResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[MountTargetResource, *MountTargetResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	clients, err := newMountTargetClients(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, clients.EFS(), prior.MountTargetId)
}

func (r *MountTargetResource) create(
	ctx context.Context,
	clients mountTargetClientProvider,
) (*MountTargetResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	availabilityZone, err := mountTargetAvailabilityZone(
		ctx, clients.EC2(), r.SubnetId)
	if err != nil {
		return nil, err
	}
	lock := mountTargetCreateLock(r.FileSystemId, availabilityZone)
	lock.Lock()
	defer lock.Unlock()

	in := &efs.CreateMountTargetInput{
		FileSystemId: r.fileSystemID(),
		SubnetId:     aws.String(r.SubnetId),
		IpAddress:    r.IpAddress,
		Ipv6Address:  r.Ipv6Address,
	}
	if r.IpAddressType != nil {
		in.IpAddressType = efstypes.IpAddressType(*r.IpAddressType)
	}
	if r.SecurityGroups != nil {
		in.SecurityGroups = slices.Clone(*r.SecurityGroups)
	}
	created, err := clients.EFS().CreateMountTarget(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("create mount target for file system %s: %w",
			r.FileSystemId, err)
	}
	mountTargetID := ""
	if created != nil {
		mountTargetID = aws.ToString(created.MountTargetId)
	}
	if mountTargetID == "" {
		return nil, errors.New("create mount target returned no mount-target-id")
	}
	if _, err := waitMountTargetCreated(
		ctx, clients.EFS(), mountTargetID, mountTargetCreateTimeout); err != nil {
		return nil, err
	}
	return r.read(ctx, clients.EFS(), clients.Region(), mountTargetID)
}

func (r *MountTargetResource) read(
	ctx context.Context,
	client mountTargetClient,
	region string,
	mountTargetID string,
) (*MountTargetResourceOutput, error) {
	description, err := describeMountTarget(ctx, client, mountTargetID)
	if err != nil {
		return nil, err
	}
	groups, err := client.DescribeMountTargetSecurityGroups(ctx,
		&efs.DescribeMountTargetSecurityGroupsInput{
			MountTargetId: aws.String(mountTargetID),
		})
	if isMountTargetNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe mount target %s security groups: %w",
			mountTargetID, err)
	}
	securityGroups := []string(nil)
	if groups != nil {
		securityGroups = slices.Clone(groups.SecurityGroups)
	}
	fileSystemID := aws.ToString(description.FileSystemId)
	availabilityZoneName := aws.ToString(description.AvailabilityZoneName)
	return &MountTargetResourceOutput{
		MountTargetId:        aws.ToString(description.MountTargetId),
		AvailabilityZoneId:   aws.ToString(description.AvailabilityZoneId),
		AvailabilityZoneName: availabilityZoneName,
		IpAddress:            aws.ToString(description.IpAddress),
		IpAddressType:        mountTargetIPAddressType(description),
		Ipv6Address:          aws.ToString(description.Ipv6Address),
		MountTargetDNSName: fmt.Sprintf("%s.%s", availabilityZoneName,
			fileSystemDNSName(fileSystemID, region)),
		NetworkInterfaceId: aws.ToString(description.NetworkInterfaceId),
		OwnerId:            aws.ToString(description.OwnerId),
		SecurityGroups:     securityGroups,
	}, nil
}

func (r *MountTargetResource) update(
	ctx context.Context,
	clients mountTargetClientProvider,
	prior runtime.Prior[MountTargetResource, *MountTargetResourceOutput, *awsCfg],
) (*MountTargetResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.MountTargetId == "" {
		return nil, errors.New("update mount target requires prior mount-target-id")
	}
	mountTargetID := prior.Outputs.MountTargetId
	if r.SecurityGroups != nil &&
		runtime.Changed(prior.Inputs.SecurityGroups, r.SecurityGroups) {
		_, err := clients.EFS().ModifyMountTargetSecurityGroups(ctx,
			&efs.ModifyMountTargetSecurityGroupsInput{
				MountTargetId:  aws.String(mountTargetID),
				SecurityGroups: slices.Clone(*r.SecurityGroups),
			})
		if err != nil {
			return nil, fmt.Errorf("modify mount target %s security groups: %w",
				mountTargetID, err)
		}
	}
	return r.read(ctx, clients.EFS(), clients.Region(), mountTargetID)
}

func (r *MountTargetResource) delete(
	ctx context.Context,
	client mountTargetClient,
	mountTargetID string,
) error {
	_, err := client.DeleteMountTarget(ctx, &efs.DeleteMountTargetInput{
		MountTargetId: aws.String(mountTargetID),
	})
	if isMountTargetNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete mount target %s: %w", mountTargetID, err)
	}
	return waitMountTargetDeleted(ctx, client, mountTargetID, mountTargetDeleteTimeout)
}

func describeMountTarget(
	ctx context.Context,
	client mountTargetClient,
	mountTargetID string,
) (efstypes.MountTargetDescription, error) {
	resp, err := client.DescribeMountTargets(ctx, &efs.DescribeMountTargetsInput{
		MountTargetId: aws.String(mountTargetID),
	})
	if isMountTargetNotFound(err) {
		return efstypes.MountTargetDescription{}, runtime.ErrNotFound
	}
	if err != nil {
		return efstypes.MountTargetDescription{}, fmt.Errorf(
			"describe mount target %s: %w", mountTargetID, err)
	}
	if resp == nil || len(resp.MountTargets) == 0 {
		return efstypes.MountTargetDescription{}, runtime.ErrNotFound
	}
	if len(resp.MountTargets) != 1 {
		return efstypes.MountTargetDescription{}, fmt.Errorf(
			"describe mount target %s returned %d results, want 1",
			mountTargetID, len(resp.MountTargets))
	}
	description := resp.MountTargets[0]
	if aws.ToString(description.MountTargetId) != mountTargetID ||
		description.LifeCycleState == efstypes.LifeCycleStateDeleted {
		return efstypes.MountTargetDescription{}, runtime.ErrNotFound
	}
	return description, nil
}

func waitMountTargetCreated(
	ctx context.Context,
	client mountTargetClient,
	mountTargetID string,
	timeout time.Duration,
) (efstypes.MountTargetDescription, error) {
	if err := mountTargetWaitDelay(ctx); err != nil {
		return efstypes.MountTargetDescription{}, err
	}
	var available efstypes.MountTargetDescription
	err := wait.Until(ctx, fmt.Sprintf("EFS mount target %s to become available", mountTargetID),
		func(ctx context.Context) (bool, error) {
			description, err := describeMountTarget(ctx, client, mountTargetID)
			if errors.Is(err, runtime.ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			switch description.LifeCycleState {
			case efstypes.LifeCycleStateAvailable:
				available = description
				return true, nil
			case efstypes.LifeCycleStateCreating:
				return false, nil
			default:
				return false, fmt.Errorf("mount target %s entered lifecycle state %s",
					mountTargetID, description.LifeCycleState)
			}
		},
		wait.WithTimeout(timeout),
		wait.WithInterval(mountTargetPollInterval),
	)
	if err != nil {
		return efstypes.MountTargetDescription{}, err
	}
	return available, nil
}

func waitMountTargetDeleted(
	ctx context.Context,
	client mountTargetClient,
	mountTargetID string,
	timeout time.Duration,
) error {
	if err := mountTargetWaitDelay(ctx); err != nil {
		return err
	}
	return wait.Until(ctx, fmt.Sprintf("EFS mount target %s to be deleted", mountTargetID),
		func(ctx context.Context) (bool, error) {
			description, err := describeMountTarget(ctx, client, mountTargetID)
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			switch description.LifeCycleState {
			case efstypes.LifeCycleStateAvailable, efstypes.LifeCycleStateDeleting:
				return false, nil
			default:
				return false, fmt.Errorf("mount target %s entered lifecycle state %s",
					mountTargetID, description.LifeCycleState)
			}
		},
		wait.WithTimeout(timeout),
		wait.WithInterval(mountTargetPollInterval),
	)
}

func (r *MountTargetResource) fileSystemID() *string {
	return aws.String(r.FileSystemId)
}

func mountTargetAvailabilityZone(
	ctx context.Context,
	client mountTargetSubnetClient,
	subnetID string,
) (string, error) {
	resp, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		SubnetIds: []string{subnetID},
	})
	if err != nil {
		return "", fmt.Errorf("describe subnet %s: %w", subnetID, err)
	}
	if resp != nil {
		for _, subnet := range resp.Subnets {
			if aws.ToString(subnet.SubnetId) != subnetID {
				continue
			}
			availabilityZone := aws.ToString(subnet.AvailabilityZone)
			if availabilityZone == "" {
				return "", fmt.Errorf("subnet %s returned no availability zone", subnetID)
			}
			return availabilityZone, nil
		}
	}
	return "", fmt.Errorf("subnet %s was not found", subnetID)
}

func mountTargetCreateLock(fileSystemID, availabilityZone string) *sync.Mutex {
	key := fileSystemID + "\x00" + availabilityZone
	actual, _ := mountTargetCreateLocks.LoadOrStore(key, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

func mountTargetIPAddressType(description efstypes.MountTargetDescription) string {
	switch {
	case description.IpAddress != nil && description.Ipv6Address != nil:
		return string(efstypes.IpAddressTypeDualStack)
	case description.IpAddress != nil:
		return string(efstypes.IpAddressTypeIpv4Only)
	case description.Ipv6Address != nil:
		return string(efstypes.IpAddressTypeIpv6Only)
	default:
		return ""
	}
}

func mountTargetWaitDelay(ctx context.Context) error {
	timer := time.NewTimer(mountTargetInitialDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isMountTargetNotFound(err error) bool {
	var notFound *efstypes.MountTargetNotFound
	return errors.As(err, &notFound)
}
