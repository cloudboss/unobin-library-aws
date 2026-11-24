package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

const marker = "ec2-stop-instance-it"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err == nil {
		err = verify(ctx, ec2.NewFromConfig(cfg), os.Getenv("VERIFY_PHASE"))
	}
	if err == nil && os.Getenv("VERIFY_PHASE") == "updated" {
		err = verifyUpdatePlan(filepath.Join(os.Getenv("VERIFY_BUILD_DIR"), "plan-update.json"))
	}
	if err != nil {
		log.Fatal(err)
	}
}

func verify(ctx context.Context, client *ec2.Client, phase string) error {
	if phase != "applied" && phase != "updated" && phase != "destroyed" {
		return fmt.Errorf("unexpected verification phase %q", phase)
	}
	filters := []types.Filter{{Name: aws.String("tag:unobin"), Values: []string{marker}}}
	pages := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{Filters: filters})
	var live []string
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				if !marked(instance.Tags) {
					continue
				}
				if instance.State == nil {
					return fmt.Errorf("instance %s has no state", aws.ToString(instance.InstanceId))
				}
				if instance.State.Name == types.InstanceStateNameTerminated {
					continue
				}
				if phase != "destroyed" && instance.State.Name != types.InstanceStateNameStopped {
					return fmt.Errorf("instance %s is %s, expected stopped",
						aws.ToString(instance.InstanceId), instance.State.Name)
				}
				live = append(live, aws.ToString(instance.InstanceId))
			}
		}
	}
	if phase != "destroyed" {
		if len(live) != 1 || live[0] == "" {
			return fmt.Errorf("expected one stopped owned instance, got %v", live)
		}
		return nil
	}
	if len(live) != 0 {
		return fmt.Errorf("owned instances remain: %v", live)
	}
	vpcs := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{
		Filters: []types.Filter{{
			Name: aws.String("cidr-block-association.cidr-block"), Values: []string{"10.87.0.0/16"},
		}},
	})
	for vpcs.HasMorePages() {
		page, err := vpcs.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, vpc := range page.Vpcs {
			if aws.ToString(vpc.CidrBlock) == "10.87.0.0/16" {
				return fmt.Errorf("owned VPC %s remains", aws.ToString(vpc.VpcId))
			}
		}
	}
	return nil
}

func verifyUpdatePlan(path string) error {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	plan, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	if err != nil {
		return err
	}
	for _, step := range plan.Steps {
		if step.Address == "action.stop" && step.Decision == runtime.DecisionRerun {
			return nil
		}
	}
	return fmt.Errorf("update plan must rerun action.stop")
}

func marked(tags []types.Tag) bool {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == "unobin" && aws.ToString(tag.Value) == marker {
			return true
		}
	}
	return false
}
