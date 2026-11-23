package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

const (
	layerName = "unobin-it-layer-version"
	scenario  = "tests/integration/scenarios/lambda-layer-version"
)

func main() {
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	if buildDir == "" {
		log.Fatal("VERIFY_BUILD_DIR is required")
	}
	if err := run(context.Background(), buildDir, os.Getenv("VERIFY_PHASE")); err != nil {
		log.Fatal(err)
	}
}

type layer struct {
	Arn           string   `json:"arn"`
	LayerArn      string   `json:"layer-arn"`
	Name          string   `json:"name"`
	Version       int64    `json:"version"`
	Checksum      string   `json:"checksum"`
	Size          int64    `json:"size"`
	Architectures []string `json:"architectures"`
	Runtimes      []string `json:"runtimes"`
}

func run(ctx context.Context, dir, phase string) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := lambda.NewFromConfig(cfg)
	versionPath := filepath.Join(dir, "layer-version")
	if phase == "destroyed" {
		previous, err := readVersion(versionPath)
		if err != nil {
			return err
		}
		return checkAbsent(ctx, client, previous)
	}

	var stack, archive string
	switch phase {
	case "applied":
		stack, archive = "config.ub", "first.zip"
	case "updated":
		stack, archive = "update/config.ub", "second.zip"
	default:
		return fmt.Errorf("unknown verification phase %q", phase)
	}
	published, err := readLayerOutput(ctx, dir, stack)
	if err != nil {
		return err
	}
	if err := checkPublished(ctx, client, published, archive); err != nil {
		return err
	}
	if phase == "updated" {
		previous, err := readVersion(versionPath)
		if err != nil {
			return err
		}
		if previous == published.Version {
			return fmt.Errorf("replacement reused layer version %d", previous)
		}
		if err := checkAbsent(ctx, client, previous); err != nil {
			return err
		}
	}
	return os.WriteFile(versionPath, []byte(strconv.FormatInt(published.Version, 10)), 0o600)
}

func readLayerOutput(ctx context.Context, dir, stack string) (layer, error) {
	cmd := exec.CommandContext(ctx, "./lambda-layer-version",
		"output", "published", "-c", stack, "--format", "json")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return layer{}, fmt.Errorf("read layer output: %w", err)
	}
	var output struct {
		Value layer `json:"value"`
	}
	err = json.Unmarshal(data, &output)
	return output.Value, err
}

func checkPublished(ctx context.Context, client *lambda.Client, got layer, archive string) error {
	layerArn, err := arn.Parse(got.LayerArn)
	if err != nil {
		return err
	}
	wantARN := fmt.Sprintf("%s:%d", layerArn, got.Version)
	if layerArn.Resource != "layer:"+layerName || layerArn.Service != "lambda" ||
		layerArn.Region != client.Options().Region || layerArn.AccountID == "" ||
		got.Version <= 0 || got.Arn != wantARN || got.Name != layerName {
		return fmt.Errorf("unexpected layer identity: %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(scenario, "_output", archive))
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	checksum := base64.StdEncoding.EncodeToString(hash[:])
	size := int64(len(data))
	if got.Checksum != checksum || got.Size != size ||
		!slices.Equal(got.Architectures, []string{"x86_64"}) ||
		!slices.Equal(got.Runtimes, []string{"python3.12"}) {
		return fmt.Errorf("unexpected layer output: %+v", got)
	}
	out, err := getVersion(ctx, client, got.Version)
	if err != nil {
		return err
	}
	if aws.ToString(out.LayerVersionArn) != got.Arn || aws.ToString(out.LayerArn) != got.LayerArn ||
		out.Version != got.Version || out.Content == nil ||
		aws.ToString(out.Content.CodeSha256) != checksum || out.Content.CodeSize != size ||
		!slices.Equal(out.CompatibleArchitectures, []lambdatypes.Architecture{"x86_64"}) ||
		!slices.Equal(out.CompatibleRuntimes, []lambdatypes.Runtime{"python3.12"}) {
		return fmt.Errorf("unexpected AWS metadata for %s", got.Arn)
	}
	return nil
}

func readVersion(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(data), 10, 64)
}

func getVersion(
	ctx context.Context, client *lambda.Client, version int64,
) (*lambda.GetLayerVersionOutput, error) {
	return client.GetLayerVersion(ctx, &lambda.GetLayerVersionInput{
		LayerName:     aws.String(layerName),
		VersionNumber: aws.Int64(version),
	})
}

func checkAbsent(ctx context.Context, client *lambda.Client, version int64) error {
	_, err := getVersion(ctx, client, version)
	var missing *lambdatypes.ResourceNotFoundException
	if errors.As(err, &missing) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get layer version %d: %w", version, err)
	}
	return fmt.Errorf("layer version %d still exists", version)
}
