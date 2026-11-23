package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

const marker = "ec2-volume-attachment-it"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := ec2.NewFromConfig(cfg)
	instance, err := runningInstance(ctx, client)
	if err != nil {
		return err
	}
	volumes, err := scenarioVolumes(ctx, client)
	if err != nil {
		return err
	}
	phase := os.Getenv("VERIFY_PHASE")
	if phase == "destroyed" {
		if instance != "" || len(volumes) != 0 {
			return fmt.Errorf("resources still present: instance %q, volumes %v", instance, volumes)
		}
		return nil
	}
	if phase != "applied" && phase != "updated" {
		return fmt.Errorf("unexpected verification phase %q", phase)
	}
	if instance == "" || len(volumes) != 3 {
		return fmt.Errorf("expected a running instance and fixed, old, and new volumes: %v, %v",
			instance, volumes)
	}
	fixedTime, err := checkAttached(volumes["fixed"], "/dev/sdf", instance)
	if err != nil {
		return err
	}
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	if buildDir == "" {
		return fmt.Errorf("VERIFY_BUILD_DIR is required")
	}
	checkpoint := filepath.Join(buildDir, "fixed-attachment-time")
	if phase == "applied" {
		if _, err := checkAttached(volumes["old"], "/dev/sdg", instance); err != nil {
			return err
		}
		if err := checkAvailable(volumes["new"]); err != nil {
			return err
		}
		return os.WriteFile(checkpoint, []byte(fixedTime), 0o600)
	}
	oldTime, err := os.ReadFile(checkpoint)
	if err != nil {
		return err
	}
	if string(oldTime) != fixedTime {
		return fmt.Errorf("flag-only update changed the fixed attachment time to %s", fixedTime)
	}
	if err := checkUpdatePlan(filepath.Join(buildDir, "plan-update.json")); err != nil {
		return err
	}
	if _, err := checkAttached(volumes["new"], "/dev/sdh", instance); err != nil {
		return err
	}
	return checkAvailable(volumes["old"])
}

func checkUpdatePlan(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	plan, err := runtime.OpenPlan(b, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	if err != nil {
		return err
	}
	var fixed, changing *runtime.PlanStep
	for _, step := range plan.Steps {
		switch step.Address {
		case "resource.fixed-attachment":
			fixed = &step
		case "resource.changing-attachment":
			changing = &step
		}
	}
	if fixed == nil || fixed.Decision != runtime.DecisionUpdate ||
		fixed.PriorInputs["force-detach"] != true || fixed.Inputs["force-detach"] != nil {
		return fmt.Errorf("expected an in-place update clearing force-detach: %+v", fixed)
	}
	if changing == nil || changing.Decision != runtime.DecisionReplace ||
		!slices.Equal(changing.ReplacementReasons, []string{"device-name", "volume-id"}) {
		return fmt.Errorf("expected replacement for device-name and volume-id: %+v", changing)
	}
	return nil
}

func checkAttached(volume ec2types.Volume, device, instance string) (string, error) {
	if len(volume.Attachments) == 1 && volume.State == ec2types.VolumeStateInUse {
		a := volume.Attachments[0]
		if a.State == ec2types.VolumeAttachmentStateAttached && aws.ToString(a.Device) == device &&
			aws.ToString(a.InstanceId) == instance &&
			aws.ToString(a.VolumeId) == aws.ToString(volume.VolumeId) && a.AttachTime != nil {
			return a.AttachTime.Format(time.RFC3339Nano), nil
		}
	}
	return "", fmt.Errorf("expected volume %s attached to %s at %s, got state %s and links %v",
		aws.ToString(volume.VolumeId), instance, device, volume.State, volume.Attachments)
}

func checkAvailable(volume ec2types.Volume) error {
	if volume.State != ec2types.VolumeStateAvailable || len(volume.Attachments) != 0 {
		return fmt.Errorf("expected volume %s available without attachments, got %s and %v",
			aws.ToString(volume.VolumeId), volume.State, volume.Attachments)
	}
	return nil
}

func runningInstance(ctx context.Context, client *ec2.Client) (string, error) {
	p := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{
		Filters: markerFilter(),
	})
	var id string
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return "", err
		}
		for _, r := range page.Reservations {
			for _, i := range r.Instances {
				if tag(i.Tags, "unobin") != marker {
					continue
				}
				if i.State == nil {
					return "", fmt.Errorf("instance %s lacks state", aws.ToString(i.InstanceId))
				}
				switch i.State.Name {
				case ec2types.InstanceStateNameTerminated, ec2types.InstanceStateNameShuttingDown:
					continue
				case ec2types.InstanceStateNameRunning:
					if id != "" {
						return "", fmt.Errorf("found two running tagged instances")
					}
					id = aws.ToString(i.InstanceId)
				default:
					return "", fmt.Errorf("instance state is %s", i.State.Name)
				}
			}
		}
	}
	return id, nil
}

func scenarioVolumes(ctx context.Context, client *ec2.Client) (map[string]ec2types.Volume, error) {
	p := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{Filters: markerFilter()})
	volumes := map[string]ec2types.Volume{}
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Volumes {
			if tag(v.Tags, "unobin") != marker {
				continue
			}
			role := tag(v.Tags, "role")
			if role != "fixed" && role != "old" && role != "new" {
				return nil, fmt.Errorf("volume %s has unexpected role %q",
					aws.ToString(v.VolumeId), role)
			}
			if _, exists := volumes[role]; exists {
				return nil, fmt.Errorf("more than one volume with role %s", role)
			}
			volumes[role] = v
		}
	}
	return volumes, nil
}

func markerFilter() []ec2types.Filter {
	return []ec2types.Filter{{Name: aws.String("tag:unobin"), Values: []string{marker}}}
}

func tag(tags []ec2types.Tag, key string) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			return aws.ToString(t.Value)
		}
	}
	return ""
}
