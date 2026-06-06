// verify checks the instance stack the scenario applied against the phase
// named in the VERIFY_PHASE environment variable. It looks the VPC up by its
// CIDR, the instance by VPC membership and the scenario's marker tag, and the
// key pair by its fixed name, because the driver passes no plan outputs into
// verify, and it reads only cloud state: applied requires one running
// instance wearing the marker and the imported key pair to resolve; destroyed
// requires the VPC to be gone, no live marked instance, and the key pair to
// read not-found. Tearing the stack down is the destroy plan's job, not the
// verifier's.
//
// Describe filters are sent as hints but never trusted: an emulator may not
// apply a server-side filter, so every check re-matches the response
// client-side on the owning VPC, the marker tag, the state, or the name.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
)

const (
	vpcCidr     = "10.63.0.0/16"
	keyName     = "unobin-it-instance"
	markerKey   = "unobin"
	markerValue = "instance-it"
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
	client := ec2.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("verify phase must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *ec2.Client) error {
	vpcID, err := findVpc(ctx, client)
	if err != nil {
		return err
	}
	if vpcID == "" {
		return fmt.Errorf("no vpc with cidr %s", vpcCidr)
	}
	instance, err := findMarkedInstance(ctx, client, vpcID)
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("no marked instance in %s", vpcID)
	}
	id := aws.ToString(instance.InstanceId)
	if instance.State.Name != ec2types.InstanceStateNameRunning {
		return fmt.Errorf("instance %s is in state %s, want running", id, instance.State.Name)
	}
	if got := aws.ToString(instance.KeyName); got != keyName {
		return fmt.Errorf("instance %s has key %q, want %q", id, got, keyName)
	}
	if err := checkKeyPair(ctx, client); err != nil {
		return err
	}
	fmt.Printf("ok: instance %s is running with key pair %s\n", id, keyName)
	return nil
}

func verifyDestroyed(ctx context.Context, client *ec2.Client) error {
	vpcID, err := findVpc(ctx, client)
	if err != nil {
		return err
	}
	if vpcID != "" {
		return fmt.Errorf("vpc with cidr %s still exists", vpcCidr)
	}
	resp, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: markerFilter(),
	})
	if err != nil {
		return fmt.Errorf("describe instances: %w", err)
	}
	for _, reservation := range resp.Reservations {
		for _, instance := range reservation.Instances {
			// A terminated instance lingers in describes for a while; only
			// a live state is a leak.
			if hasMarker(instance.Tags) && !instanceGone(instance.State.Name) {
				return fmt.Errorf("instance %s still in state %s",
					aws.ToString(instance.InstanceId), instance.State.Name)
			}
		}
	}
	gone, err := keyPairGone(ctx, client)
	if err != nil {
		return err
	}
	if !gone {
		return fmt.Errorf("key pair %s still exists", keyName)
	}
	fmt.Println("ok: the VPC, instance, and key pair are gone")
	return nil
}

// findVpc returns the id of the VPC with the scenario's CIDR, or empty when
// it does not exist. More than one match means leftover state from an earlier
// run, which is an error rather than a silent pick.
func findVpc(ctx context.Context, client *ec2.Client) (string, error) {
	resp, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("cidr-block-association.cidr-block"),
			Values: []string{vpcCidr},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("describe vpcs: %w", err)
	}
	ids := []string{}
	for _, vpc := range resp.Vpcs {
		if aws.ToString(vpc.CidrBlock) == vpcCidr {
			ids = append(ids, aws.ToString(vpc.VpcId))
		}
	}
	switch len(ids) {
	case 0:
		return "", nil
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("found %d vpcs with cidr %s, expected at most 1",
			len(ids), vpcCidr)
	}
}

// findMarkedInstance returns the one live instance in the VPC wearing the
// scenario's marker, or nil when none exists.
func findMarkedInstance(
	ctx context.Context,
	client *ec2.Client,
	vpcID string,
) (*ec2types.Instance, error) {
	resp, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: append(markerFilter(), ec2types.Filter{
			Name:   aws.String("vpc-id"),
			Values: []string{vpcID},
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}
	matches := []ec2types.Instance{}
	for _, reservation := range resp.Reservations {
		for _, instance := range reservation.Instances {
			if hasMarker(instance.Tags) && aws.ToString(instance.VpcId) == vpcID &&
				!instanceGone(instance.State.Name) {
				matches = append(matches, instance)
			}
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("found %d marked instances in %s, expected 1",
			len(matches), vpcID)
	}
}

// instanceGone reports whether the state means the instance no longer counts
// as present.
func instanceGone(state ec2types.InstanceStateName) bool {
	return state == ec2types.InstanceStateNameTerminated ||
		state == ec2types.InstanceStateNameShuttingDown
}

// checkKeyPair requires the imported key pair to resolve by name with an id.
func checkKeyPair(ctx context.Context, client *ec2.Client) error {
	resp, err := client.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []string{keyName},
	})
	if err != nil {
		return fmt.Errorf("describe key pairs: %w", err)
	}
	for _, kp := range resp.KeyPairs {
		if aws.ToString(kp.KeyName) == keyName {
			if aws.ToString(kp.KeyPairId) == "" {
				return fmt.Errorf("key pair %s has no id", keyName)
			}
			return nil
		}
	}
	return fmt.Errorf("key pair %s not found", keyName)
}

// keyPairGone reports whether the key pair no longer resolves by name.
func keyPairGone(ctx context.Context, client *ec2.Client) (bool, error) {
	resp, err := client.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []string{keyName},
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidKeyPair.NotFound" {
			return true, nil
		}
		return false, fmt.Errorf("describe key pairs: %w", err)
	}
	for _, kp := range resp.KeyPairs {
		if aws.ToString(kp.KeyName) == keyName {
			return false, nil
		}
	}
	return true, nil
}

// tagValue returns the value of the named tag, or empty when absent.
func tagValue(tags []ec2types.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}

// hasMarker reports whether the tag set holds the scenario's marker.
func hasMarker(tags []ec2types.Tag) bool {
	return tagValue(tags, markerKey) == markerValue
}

// markerFilter selects resources tagged with the scenario's marker. A hint
// only; callers re-check the tags client-side.
func markerFilter() []ec2types.Filter {
	return []ec2types.Filter{{
		Name:   aws.String("tag:" + markerKey),
		Values: []string{markerValue},
	}}
}
