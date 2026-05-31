// verify checks the VPC the scenario applied against the phase named in
// the VERIFY_PHASE environment variable, looking the VPC up by its CIDR
// because the test driver does not pass plan outputs into verify. It
// only reads cloud state: applied requires the VPC to be present,
// destroyed requires it to be gone. Tearing the VPC down is the destroy
// plan's job, not the verifier's.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const cidr = "10.42.0.0/16"

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
	client := ec2.NewFromConfig(cfg)

	out, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("cidr"), Values: []string{cidr}},
		},
	})
	if err != nil {
		return fmt.Errorf("describe vpcs: %w", err)
	}

	switch phase {
	case "applied":
		if len(out.Vpcs) == 0 {
			return fmt.Errorf("no vpc with cidr %s", cidr)
		}
		fmt.Printf("ok: vpc %s with cidr %s is present\n",
			aws.ToString(out.Vpcs[0].VpcId), cidr)
		return nil
	case "destroyed":
		if len(out.Vpcs) > 0 {
			return fmt.Errorf("vpc %s with cidr %s still exists",
				aws.ToString(out.Vpcs[0].VpcId), cidr)
		}
		fmt.Printf("ok: no vpc with cidr %s remains\n", cidr)
		return nil
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}
