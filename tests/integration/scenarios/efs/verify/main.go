package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
)

const (
	fileSystemName           = "unobin-it-efs"
	accessPointName          = "unobin-it-efs-access-point"
	initialSecurityGroupName = "unobin-it-efs-mount-initial"
	updatedSecurityGroupName = "unobin-it-efs-mount-updated"
	fileSystemVPCCIDR        = "10.64.0.0/16"
	fileSystemSubnetCIDR     = "10.64.1.0/24"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	efsClient := efs.NewFromConfig(cfg)
	ec2Client := ec2.NewFromConfig(cfg)
	switch mode := os.Getenv("VERIFY_PHASE"); mode {
	case "applied":
		return verifyPresent(ctx, efsClient, ec2Client,
			efstypes.ThroughputModeBursting, initialSecurityGroupName,
			map[string]string{
				"Name": fileSystemName, "state": "initial", "remove": "yes",
			}, map[string]string{
				"Name": accessPointName, "state": "initial", "remove": "yes",
			})
	case "updated":
		return verifyPresent(ctx, efsClient, ec2Client,
			efstypes.ThroughputModeElastic, updatedSecurityGroupName,
			map[string]string{
				"Name": fileSystemName, "state": "updated", "added": "yes",
			}, map[string]string{
				"Name": accessPointName, "state": "updated", "added": "yes",
			})
	case "destroyed":
		return verifyDestroyed(ctx, efsClient, ec2Client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q", mode)
	}
}

func verifyPresent(
	ctx context.Context,
	efsClient *efs.Client,
	ec2Client *ec2.Client,
	wantMode efstypes.ThroughputMode,
	wantSecurityGroupName string,
	wantFileSystemTags map[string]string,
	wantAccessPointTags map[string]string,
) error {
	fileSystem, tags, err := findFileSystem(ctx, efsClient)
	if err != nil {
		return err
	}
	if fileSystem == nil {
		return fmt.Errorf("file system tagged Name=%s not found", fileSystemName)
	}
	if fileSystem.LifeCycleState != efstypes.LifeCycleStateAvailable {
		return fmt.Errorf("file system state is %s, want available", fileSystem.LifeCycleState)
	}
	if fileSystem.ThroughputMode != wantMode {
		return fmt.Errorf("throughput mode is %s, want %s",
			fileSystem.ThroughputMode, wantMode)
	}
	if !maps.Equal(tags, wantFileSystemTags) {
		return fmt.Errorf("file system tags are %#v, want %#v", tags, wantFileSystemTags)
	}
	if err := verifyAccessPoint(
		ctx, efsClient, fileSystem, wantAccessPointTags); err != nil {
		return err
	}
	if err := verifyMountTarget(
		ctx, efsClient, ec2Client, fileSystem, wantSecurityGroupName); err != nil {
		return err
	}
	fmt.Printf("ok: EFS file system %s is available with throughput mode %s\n",
		aws.ToString(fileSystem.FileSystemId), wantMode)
	return nil
}

func verifyDestroyed(
	ctx context.Context,
	efsClient *efs.Client,
	ec2Client *ec2.Client,
) error {
	accessPoint, _, err := findAccessPoint(ctx, efsClient, "")
	if err != nil {
		return err
	}
	if accessPoint != nil {
		return fmt.Errorf("access point %s still exists",
			aws.ToString(accessPoint.AccessPointId))
	}
	fileSystem, _, err := findFileSystem(ctx, efsClient)
	if err != nil {
		return err
	}
	if fileSystem != nil {
		return fmt.Errorf("file system %s still exists", aws.ToString(fileSystem.FileSystemId))
	}
	for _, name := range []string{initialSecurityGroupName, updatedSecurityGroupName} {
		group, err := findSecurityGroup(ctx, ec2Client, name)
		if err != nil {
			return err
		}
		if group != nil {
			return fmt.Errorf("security group %s still exists", aws.ToString(group.GroupId))
		}
	}
	vpc, err := findVPC(ctx, ec2Client)
	if err != nil {
		return err
	}
	if vpc != nil {
		return fmt.Errorf("VPC %s still exists", aws.ToString(vpc.VpcId))
	}
	fmt.Printf("ok: EFS file system tagged Name=%s is gone\n", fileSystemName)
	return nil
}

func verifyAccessPoint(
	ctx context.Context,
	client *efs.Client,
	fileSystem *efstypes.FileSystemDescription,
	wantTags map[string]string,
) error {
	fileSystemID := aws.ToString(fileSystem.FileSystemId)
	accessPoint, tags, err := findAccessPoint(ctx, client, fileSystemID)
	if err != nil {
		return err
	}
	if accessPoint == nil {
		return fmt.Errorf("access point tagged Name=%s not found", accessPointName)
	}
	if accessPoint.LifeCycleState != efstypes.LifeCycleStateAvailable {
		return fmt.Errorf("access point state is %s, want available",
			accessPoint.LifeCycleState)
	}
	if aws.ToString(accessPoint.FileSystemId) != fileSystemID {
		return fmt.Errorf("access point file system is %s, want %s",
			aws.ToString(accessPoint.FileSystemId), fileSystemID)
	}
	if aws.ToString(accessPoint.AccessPointId) == "" ||
		aws.ToString(accessPoint.AccessPointArn) == "" ||
		aws.ToString(accessPoint.OwnerId) == "" {
		return errors.New("access point is missing an identity value")
	}
	if !maps.Equal(tags, wantTags) {
		return fmt.Errorf("access point tags are %#v, want %#v", tags, wantTags)
	}
	fmt.Printf("ok: EFS access point %s is available\n",
		aws.ToString(accessPoint.AccessPointId))
	return nil
}

func verifyMountTarget(
	ctx context.Context,
	efsClient *efs.Client,
	ec2Client *ec2.Client,
	fileSystem *efstypes.FileSystemDescription,
	wantSecurityGroupName string,
) error {
	fileSystemID := aws.ToString(fileSystem.FileSystemId)
	resp, err := efsClient.DescribeMountTargets(ctx, &efs.DescribeMountTargetsInput{
		FileSystemId: aws.String(fileSystemID),
	})
	if err != nil {
		return fmt.Errorf("describe mount targets for file system %s: %w", fileSystemID, err)
	}
	active := make([]efstypes.MountTargetDescription, 0, len(resp.MountTargets))
	for _, mountTarget := range resp.MountTargets {
		if mountTarget.LifeCycleState != efstypes.LifeCycleStateDeleted {
			active = append(active, mountTarget)
		}
	}
	if len(active) != 1 {
		return fmt.Errorf("file system %s has %d active mount targets, want 1",
			fileSystemID, len(active))
	}
	mountTarget := active[0]
	if mountTarget.LifeCycleState != efstypes.LifeCycleStateAvailable {
		return fmt.Errorf("mount target state is %s, want available",
			mountTarget.LifeCycleState)
	}
	if aws.ToString(mountTarget.MountTargetId) == "" ||
		aws.ToString(mountTarget.AvailabilityZoneName) == "" ||
		aws.ToString(mountTarget.NetworkInterfaceId) == "" ||
		aws.ToString(mountTarget.IpAddress) == "" {
		return errors.New("mount target is missing an assigned network value")
	}
	if mountTarget.Ipv6Address != nil {
		return fmt.Errorf("mount target has unexpected IPv6 address %s",
			aws.ToString(mountTarget.Ipv6Address))
	}
	if err := verifyMountTargetSubnet(ctx, ec2Client, mountTarget); err != nil {
		return err
	}
	securityGroups := make(map[string]*ec2types.SecurityGroup, 2)
	for _, name := range []string{initialSecurityGroupName, updatedSecurityGroupName} {
		group, err := findSecurityGroup(ctx, ec2Client, name)
		if err != nil {
			return err
		}
		if group == nil {
			return fmt.Errorf("security group %s not found", name)
		}
		securityGroups[name] = group
	}
	group := securityGroups[wantSecurityGroupName]
	groups, err := efsClient.DescribeMountTargetSecurityGroups(ctx,
		&efs.DescribeMountTargetSecurityGroupsInput{
			MountTargetId: mountTarget.MountTargetId,
		})
	if err != nil {
		return fmt.Errorf("describe mount target security groups: %w", err)
	}
	wantGroups := []string{aws.ToString(group.GroupId)}
	if !slices.Equal(groups.SecurityGroups, wantGroups) {
		return fmt.Errorf("mount target security groups are %v, want %v",
			groups.SecurityGroups, wantGroups)
	}
	fmt.Printf("ok: EFS mount target %s is available with security group %s\n",
		aws.ToString(mountTarget.MountTargetId), wantSecurityGroupName)
	return nil
}

func verifyMountTargetSubnet(
	ctx context.Context,
	client *ec2.Client,
	mountTarget efstypes.MountTargetDescription,
) error {
	subnetID := aws.ToString(mountTarget.SubnetId)
	resp, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		SubnetIds: []string{subnetID},
	})
	if err != nil {
		return fmt.Errorf("describe mount target subnet %s: %w", subnetID, err)
	}
	if len(resp.Subnets) != 1 || aws.ToString(resp.Subnets[0].CidrBlock) != fileSystemSubnetCIDR {
		return fmt.Errorf("mount target subnet %s does not have CIDR %s",
			subnetID, fileSystemSubnetCIDR)
	}
	return nil
}

func findSecurityGroup(
	ctx context.Context,
	client *ec2.Client,
	name string,
) (*ec2types.SecurityGroup, error) {
	resp, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("group-name"),
			Values: []string{name},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("describe security group %s: %w", name, err)
	}
	if len(resp.SecurityGroups) == 0 {
		return nil, nil
	}
	return &resp.SecurityGroups[0], nil
}

func findVPC(ctx context.Context, client *ec2.Client) (*ec2types.Vpc, error) {
	resp, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("cidr"),
			Values: []string{fileSystemVPCCIDR},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("describe VPCs: %w", err)
	}
	if len(resp.Vpcs) == 0 {
		return nil, nil
	}
	return &resp.Vpcs[0], nil
}

func findFileSystem(
	ctx context.Context,
	client *efs.Client,
) (*efstypes.FileSystemDescription, map[string]string, error) {
	pager := efs.NewDescribeFileSystemsPaginator(client, &efs.DescribeFileSystemsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("describe file systems: %w", err)
		}
		for i := range page.FileSystems {
			if page.FileSystems[i].LifeCycleState == efstypes.LifeCycleStateDeleted {
				continue
			}
			tags, err := readTags(ctx, client, aws.ToString(page.FileSystems[i].FileSystemId))
			if err != nil {
				return nil, nil, err
			}
			if tags["Name"] == fileSystemName {
				return &page.FileSystems[i], tags, nil
			}
		}
	}
	return nil, nil, nil
}

func findAccessPoint(
	ctx context.Context,
	client *efs.Client,
	fileSystemID string,
) (*efstypes.AccessPointDescription, map[string]string, error) {
	in := &efs.DescribeAccessPointsInput{}
	if fileSystemID != "" {
		in.FileSystemId = aws.String(fileSystemID)
	}
	pager := efs.NewDescribeAccessPointsPaginator(client, in)
	var found *efstypes.AccessPointDescription
	var foundTags map[string]string
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("describe access points: %w", err)
		}
		for i := range page.AccessPoints {
			if page.AccessPoints[i].LifeCycleState == efstypes.LifeCycleStateDeleted {
				continue
			}
			tags, err := readTags(ctx, client,
				aws.ToString(page.AccessPoints[i].AccessPointId))
			if err != nil {
				return nil, nil, err
			}
			if tags["Name"] != accessPointName {
				continue
			}
			if found != nil {
				return nil, nil, fmt.Errorf(
					"found multiple access points tagged Name=%s", accessPointName)
			}
			found = &page.AccessPoints[i]
			foundTags = tags
		}
	}
	return found, foundTags, nil
}

func readTags(
	ctx context.Context,
	client *efs.Client,
	fileSystemID string,
) (map[string]string, error) {
	tags := map[string]string{}
	pager := efs.NewListTagsForResourcePaginator(client, &efs.ListTagsForResourceInput{
		ResourceId: aws.String(fileSystemID),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list tags for EFS resource %s: %w", fileSystemID, err)
		}
		for _, tag := range page.Tags {
			key := aws.ToString(tag.Key)
			if strings.HasPrefix(key, "aws:") {
				continue
			}
			tags[key] = aws.ToString(tag.Value)
		}
	}
	return tags, nil
}
