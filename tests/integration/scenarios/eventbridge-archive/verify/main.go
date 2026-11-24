package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
)

const (
	archiveName = "unobin-it-archive"
	busName     = "unobin-it-archive-bus"
)

func main() {
	if err := verify(context.Background(), os.Getenv("VERIFY_PHASE")); err != nil {
		log.Fatal(err)
	}
}

func verify(ctx context.Context, phase string) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := eventbridge.NewFromConfig(cfg)
	out, err := client.DescribeArchive(ctx,
		&eventbridge.DescribeArchiveInput{ArchiveName: aws.String(archiveName)})
	if phase == "destroyed" {
		var missing *eventtypes.ResourceNotFoundException
		if !errors.As(err, &missing) {
			return fmt.Errorf("expected absent archive, got %v and %+v", err, out)
		}
		bus, err := client.DescribeEventBus(ctx,
			&eventbridge.DescribeEventBusInput{Name: aws.String(busName)})
		if !errors.As(err, &missing) {
			return fmt.Errorf("expected absent bus, got %v and %+v", err, bus)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("describe archive: %w", err)
	}
	return verifyArchive(out, phase)
}

func verifyArchive(out *eventbridge.DescribeArchiveOutput, phase string) error {
	description, source, days := "initial archive", "unobin.initial", int32(1)
	if phase == "updated" {
		description, source, days = "updated archive", "unobin.updated", 7
	} else if phase != "applied" {
		return fmt.Errorf("unknown phase %q", phase)
	}
	if out == nil || aws.ToString(out.ArchiveName) != archiveName ||
		!strings.HasSuffix(aws.ToString(out.ArchiveArn), ":archive/"+archiveName) ||
		!strings.HasSuffix(aws.ToString(out.EventSourceArn), ":event-bus/"+busName) ||
		aws.ToString(out.Description) != description || aws.ToInt32(out.RetentionDays) != days {
		return fmt.Errorf("unexpected archive values: %+v", out)
	}
	var pattern map[string][]string
	if err := json.Unmarshal([]byte(aws.ToString(out.EventPattern)), &pattern); err != nil {
		return err
	}
	if !reflect.DeepEqual(pattern, map[string][]string{"source": {source}}) {
		return fmt.Errorf("unexpected pattern: %+v", pattern)
	}
	return nil
}
