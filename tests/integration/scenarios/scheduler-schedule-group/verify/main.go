package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	schedulerapi "github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
)

const groupName = "unobin-it-schedule-group"

//go:embed config-clear.ub
var clearConfig []byte

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	client := schedulerapi.NewFromConfig(cfg)
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	arnPath := filepath.Join(buildDir, "schedule-group-arn")
	switch os.Getenv("VERIFY_PHASE") {
	case "applied":
		arn, err := verifyGroup(ctx, client, map[string]string{
			"owner": "initial", "remove": "obsolete",
		})
		if err != nil {
			return err
		}
		return os.WriteFile(arnPath, []byte(arn), 0o600)
	case "updated":
		savedARN, err := os.ReadFile(arnPath)
		if err != nil {
			return err
		}
		updated, err := verifyGroup(ctx, client, map[string]string{"owner": ""})
		if err != nil {
			return err
		}
		if updated != string(savedARN) {
			return fmt.Errorf("updated group ARN is %q, want %q", updated, savedARN)
		}
		if err := applyClear(ctx, buildDir); err != nil {
			return err
		}
		cleared, err := verifyGroup(ctx, client, map[string]string{})
		if err != nil {
			return err
		}
		if cleared != string(savedARN) {
			return fmt.Errorf("cleared group ARN is %q, want %q", cleared, savedARN)
		}
		return nil
	case "destroyed":
		_, err := client.GetScheduleGroup(ctx, &schedulerapi.GetScheduleGroupInput{
			Name: aws.String(groupName),
		})
		var missing *schedulertypes.ResourceNotFoundException
		if !errors.As(err, &missing) {
			return fmt.Errorf("expected schedule group %s to be absent, got %v", groupName, err)
		}
		fmt.Printf("ok: schedule group %s is gone\n", groupName)
		return nil
	default:
		return errors.New("VERIFY_PHASE must be applied, updated, or destroyed")
	}
}

func verifyGroup(
	ctx context.Context, client *schedulerapi.Client, want map[string]string,
) (string, error) {
	out, err := client.GetScheduleGroup(ctx, &schedulerapi.GetScheduleGroupInput{
		Name: aws.String(groupName),
	})
	if err != nil {
		return "", fmt.Errorf("get schedule group %s: %w", groupName, err)
	}
	arn := aws.ToString(out.Arn)
	if arn == "" || aws.ToString(out.Name) != groupName ||
		out.State != schedulertypes.ScheduleGroupStateActive {
		return "", fmt.Errorf("unexpected group name %q, ARN %q or state %q",
			aws.ToString(out.Name), arn, out.State)
	}
	listed, err := client.ListTagsForResource(ctx, &schedulerapi.ListTagsForResourceInput{
		ResourceArn: &arn,
	})
	if err != nil {
		return "", fmt.Errorf("list tags on %s: %w", arn, err)
	}
	got := make(map[string]string, len(listed.Tags))
	for _, tag := range listed.Tags {
		got[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	if !maps.Equal(got, want) {
		return "", fmt.Errorf("tags on %s are %v, want %v", arn, got, want)
	}
	fmt.Printf("ok: schedule group %s is active with tags %v\n", arn, got)
	return arn, nil
}

func applyClear(ctx context.Context, buildDir string) error {
	if err := os.MkdirAll(filepath.Join(buildDir, "clear"), 0o700); err != nil {
		return err
	}
	// The config basename selects the same stack as the first two applies.
	path := filepath.Join(buildDir, "clear", "config.ub")
	if err := os.WriteFile(path, clearConfig, 0o600); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"pin", "-c", "clear/config.ub"},
		{"plan", "-c", "clear/config.ub", "-o", "plan-clear.json"},
		{"apply", "plan-clear.json"},
	} {
		cmd := exec.CommandContext(ctx, "./scheduler-schedule-group", args...)
		cmd.Dir = buildDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("clear group tags (%s): %w", args[0], err)
		}
	}
	return nil
}
