// verify checks the distribution and invalidation named by cloud state and the
// action outputs persisted in local state. The applied and updated checks require
// completed invalidation details; the destroyed check requires both records gone.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

const (
	wantComment         = "unobin cloudfront integration test"
	wantUpdatedComment  = "unobin cloudfront integration test (updated)"
	invalidationAddress = "action.cloudfront-create-invalidation-it"
)

var wantInvalidationPaths = []string{"/", "/index.html", "/*"}

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	mode := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := cloudfront.NewFromConfig(cfg)

	switch mode {
	case "applied":
		return verifyApplied(ctx, client)
	case "updated":
		return verifyUpdated(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q", mode)
	}
}

func verifyApplied(ctx context.Context, client *cloudfront.Client) error {
	d, err := findDistribution(ctx, client, wantComment)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("no distribution with comment %q", wantComment)
	}
	if !aws.ToBool(d.Enabled) {
		return fmt.Errorf("distribution %s is not enabled", aws.ToString(d.Id))
	}
	if err := verifyInvalidation(ctx, client, aws.ToString(d.Id)); err != nil {
		return err
	}
	fmt.Printf("ok: distribution %s present and enabled with comment %q\n",
		aws.ToString(d.Id), wantComment)
	return nil
}

func verifyUpdated(ctx context.Context, client *cloudfront.Client) error {
	d, err := findDistribution(ctx, client, wantUpdatedComment)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("no distribution with comment %q", wantUpdatedComment)
	}
	if err := verifyInvalidation(ctx, client, aws.ToString(d.Id)); err != nil {
		return err
	}
	fmt.Printf("ok: distribution %s updated with completed invalidation\n", aws.ToString(d.Id))
	return nil
}

func verifyDestroyed(ctx context.Context, client *cloudfront.Client) error {
	d, err := findDistribution(ctx, client, wantUpdatedComment)
	if err != nil {
		return err
	}
	if d != nil {
		return fmt.Errorf("distribution %s still exists", aws.ToString(d.Id))
	}
	snapshot, err := readCurrentSnapshot()
	if err != nil {
		return err
	}
	if snapshot.entry(invalidationAddress) != nil {
		return fmt.Errorf("state still contains %s", invalidationAddress)
	}
	fmt.Printf("ok: no distribution with comment %q\n", wantUpdatedComment)
	return nil
}

func findDistribution(
	ctx context.Context, client *cloudfront.Client, comment string,
) (*cloudfronttypes.DistributionSummary, error) {
	pager := cloudfront.NewListDistributionsPaginator(client,
		&cloudfront.ListDistributionsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list distributions: %w", err)
		}
		if page.DistributionList == nil {
			continue
		}
		for i, d := range page.DistributionList.Items {
			if aws.ToString(d.Comment) == comment {
				return &page.DistributionList.Items[i], nil
			}
		}
	}
	return nil, nil
}

func verifyInvalidation(
	ctx context.Context,
	client *cloudfront.Client,
	distributionID string,
) error {
	snapshot, err := readCurrentSnapshot()
	if err != nil {
		return err
	}
	entry := snapshot.entry(invalidationAddress)
	if entry == nil {
		return fmt.Errorf("state has no %s entry", invalidationAddress)
	}
	id, ok := entry.Outputs["id"].(string)
	if !ok || id == "" {
		return fmt.Errorf("%s state has invalid id", invalidationAddress)
	}
	stateStatus, ok := entry.Outputs["status"].(string)
	if !ok || stateStatus != "Completed" {
		return fmt.Errorf("%s state status is %v", invalidationAddress, entry.Outputs["status"])
	}
	resp, err := client.GetInvalidation(ctx, &cloudfront.GetInvalidationInput{
		DistributionId: aws.String(distributionID),
		Id:             aws.String(id),
	})
	if err != nil {
		return fmt.Errorf("get invalidation %s: %w", id, err)
	}
	if resp.Invalidation == nil {
		return fmt.Errorf("get invalidation %s returned no invalidation", id)
	}
	if status := aws.ToString(resp.Invalidation.Status); status != "Completed" {
		return fmt.Errorf("invalidation %s status is %q", id, status)
	}
	batch := resp.Invalidation.InvalidationBatch
	if batch == nil || batch.Paths == nil {
		return fmt.Errorf("invalidation %s has no paths", id)
	}
	if quantity := aws.ToInt32(batch.Paths.Quantity); quantity != int32(len(wantInvalidationPaths)) {
		return fmt.Errorf("invalidation %s quantity is %d", id, quantity)
	}
	if !samePathMultiset(batch.Paths.Items, wantInvalidationPaths) {
		return fmt.Errorf("invalidation %s paths are %v", id, batch.Paths.Items)
	}
	fmt.Printf("ok: invalidation %s completed with paths %v\n", id, batch.Paths.Items)
	return nil
}

func samePathMultiset(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	counts := make(map[string]int, len(want))
	for _, path := range want {
		counts[path]++
	}
	for _, path := range got {
		if counts[path] == 0 {
			return false
		}
		counts[path]--
	}
	return true
}

type stateEnvelope struct {
	Ciphertext string `json:"ciphertext"`
}

type stateSnapshot struct {
	Entries []stateEntry `json:"entries"`
}

type stateEntry struct {
	Address string         `json:"address"`
	Outputs map[string]any `json:"outputs"`
}

func (s *stateSnapshot) entry(address string) *stateEntry {
	for i := range s.Entries {
		if s.Entries[i].Address == address {
			return &s.Entries[i]
		}
	}
	return nil
}

func readCurrentSnapshot() (*stateSnapshot, error) {
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	if buildDir == "" {
		return nil, fmt.Errorf("VERIFY_BUILD_DIR is required")
	}
	stateDir := filepath.Join(buildDir, ".unobin", "state", "cloudfront", "config")
	current, err := os.ReadFile(filepath.Join(stateDir, "current"))
	if err != nil {
		return nil, fmt.Errorf("read current state revision: %w", err)
	}
	sealed, err := os.ReadFile(filepath.Join(
		stateDir,
		"snapshots",
		strings.TrimSpace(string(current))+".json.enc",
	))
	if err != nil {
		return nil, fmt.Errorf("read current state snapshot: %w", err)
	}
	var envelope stateEnvelope
	if err := json.Unmarshal(sealed, &envelope); err != nil {
		return nil, fmt.Errorf("decode state envelope: %w", err)
	}
	plain, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode state body: %w", err)
	}
	var snapshot stateSnapshot
	if err := json.Unmarshal(plain, &snapshot); err != nil {
		return nil, fmt.Errorf("decode state snapshot: %w", err)
	}
	return &snapshot, nil
}
