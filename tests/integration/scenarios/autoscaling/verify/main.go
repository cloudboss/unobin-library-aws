// verify checks the Auto Scaling group the scenario applied against the phase
// named in the VERIFY_PHASE environment variable. It looks resources up by
// their stable names because the driver passes no plan outputs into verify,
// and it reads only cloud state: applied requires the group to exist with the
// scenario's sizes and no instances, since the group is held at zero capacity,
// the launch template to exist with a t3.micro default version, and the tagged
// gp3 volume to be available; destroyed requires the group, the template, and
// the volume to be gone. Tearing the group down is the destroy plan's job, not
// the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	smithy "github.com/aws/smithy-go"
)

const (
	groupName          = "unobin-it-asg"
	launchTemplateName = "unobin-it-asg-lt"
	volumeTagKey       = "unobin"
	volumeTagValue     = "autoscaling-it"
	instanceType       = "t3.micro"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	asgClient := autoscaling.NewFromConfig(cfg)
	ec2Client := ec2.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, asgClient, ec2Client)
	case "destroyed":
		return verifyDestroyed(ctx, asgClient, ec2Client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(
	ctx context.Context, asgClient *autoscaling.Client, ec2Client *ec2.Client,
) error {
	group, err := findGroup(ctx, asgClient)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("auto scaling group %s not found", groupName)
	}
	if got := aws.ToInt32(group.MaxSize); got != 1 {
		return fmt.Errorf("group max size is %d, want 1", got)
	}
	if got := aws.ToInt32(group.DesiredCapacity); got != 0 {
		return fmt.Errorf("group desired capacity is %d, want 0", got)
	}
	if n := len(group.Instances); n != 0 {
		return fmt.Errorf("group has %d instances, want 0", n)
	}
	if group.LaunchTemplate == nil {
		return errors.New("group has no launch template")
	}
	if err := checkLaunchTemplate(ctx, ec2Client); err != nil {
		return err
	}
	volume, err := findVolume(ctx, ec2Client)
	if err != nil {
		return err
	}
	if volume == nil {
		return errors.New("tagged volume not found")
	}
	if volume.State != ec2types.VolumeStateAvailable {
		return fmt.Errorf("volume %s is %s, want available",
			aws.ToString(volume.VolumeId), volume.State)
	}
	if volume.VolumeType != ec2types.VolumeTypeGp3 {
		return fmt.Errorf("volume %s is type %s, want gp3",
			aws.ToString(volume.VolumeId), volume.VolumeType)
	}
	return nil
}

func verifyDestroyed(
	ctx context.Context, asgClient *autoscaling.Client, ec2Client *ec2.Client,
) error {
	group, err := findGroup(ctx, asgClient)
	if err != nil {
		return err
	}
	if group != nil {
		return fmt.Errorf("auto scaling group %s still exists", groupName)
	}
	exists, err := launchTemplateExists(ctx, ec2Client)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("launch template %s still exists", launchTemplateName)
	}
	volume, err := findVolume(ctx, ec2Client)
	if err != nil {
		return err
	}
	if volume != nil {
		return fmt.Errorf("volume %s still exists", aws.ToString(volume.VolumeId))
	}
	return nil
}

// findGroup returns the scenario's Auto Scaling group, or nil when the
// describe comes back empty.
func findGroup(
	ctx context.Context, client *autoscaling.Client,
) (*autoscalingtypes.AutoScalingGroup, error) {
	resp, err := client.DescribeAutoScalingGroups(ctx,
		&autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []string{groupName},
		})
	if err != nil {
		return nil, fmt.Errorf("describe auto scaling groups: %w", err)
	}
	if len(resp.AutoScalingGroups) == 0 {
		return nil, nil
	}
	return &resp.AutoScalingGroups[0], nil
}

// checkLaunchTemplate requires the scenario's launch template to exist and its
// default version to launch the expected instance type from a resolved AMI.
func checkLaunchTemplate(ctx context.Context, client *ec2.Client) error {
	resp, err := client.DescribeLaunchTemplateVersions(ctx,
		&ec2.DescribeLaunchTemplateVersionsInput{
			LaunchTemplateName: aws.String(launchTemplateName),
			Versions:           []string{"$Default"},
		})
	if err != nil {
		return fmt.Errorf("describe launch template versions: %w", err)
	}
	if len(resp.LaunchTemplateVersions) == 0 {
		return fmt.Errorf("launch template %s has no default version", launchTemplateName)
	}
	data := resp.LaunchTemplateVersions[0].LaunchTemplateData
	if data == nil {
		return fmt.Errorf("launch template %s default version has no data", launchTemplateName)
	}
	if got := string(data.InstanceType); got != instanceType {
		return fmt.Errorf("launch template instance type is %q, want %q", got, instanceType)
	}
	if aws.ToString(data.ImageId) == "" {
		return fmt.Errorf("launch template %s has no image id", launchTemplateName)
	}
	return nil
}

// launchTemplateExists reports whether the scenario's launch template still
// exists, treating the not-found error codes as gone.
func launchTemplateExists(ctx context.Context, client *ec2.Client) (bool, error) {
	resp, err := client.DescribeLaunchTemplates(ctx, &ec2.DescribeLaunchTemplatesInput{
		LaunchTemplateNames: []string{launchTemplateName},
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			code := apiErr.ErrorCode()
			if code == "InvalidLaunchTemplateName.NotFoundException" ||
				code == "InvalidLaunchTemplateId.NotFound" {
				return false, nil
			}
		}
		return false, fmt.Errorf("describe launch templates: %w", err)
	}
	return len(resp.LaunchTemplates) > 0, nil
}

// findVolume returns the scenario's tagged volume, or nil when no live volume
// matches. Deleted volumes can linger in describe output, so they are skipped.
func findVolume(ctx context.Context, client *ec2.Client) (*ec2types.Volume, error) {
	resp, err := client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:" + volumeTagKey),
				Values: []string{volumeTagValue},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe volumes: %w", err)
	}
	for i := range resp.Volumes {
		if resp.Volumes[i].State == ec2types.VolumeStateDeleted ||
			resp.Volumes[i].State == ec2types.VolumeStateDeleting {
			continue
		}
		return &resp.Volumes[i], nil
	}
	return nil, nil
}
