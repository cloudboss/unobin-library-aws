package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

const repositoryName = "unobin-it-ecr-image"

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := ecr.NewFromConfig(cfg)
	switch phase := os.Getenv("VERIFY_PHASE"); phase {
	case "applied":
		if _, err := verifyRepository(ctx, client, false, "initial"); err != nil {
			return err
		}
		return seedImages(ctx, client)
	case "updated":
		repository, err := verifyRepository(ctx, client, true, "updated")
		if err != nil {
			return err
		}
		return verifyImageReads(ctx, client, repository, "plan-update.json")
	case "destroyed":
		return verifyRepositoryDestroyed(ctx, client)
	default:
		return fmt.Errorf(
			"VERIFY_PHASE must be applied, updated, or destroyed, got %q", phase)
	}
}

func verifyRepository(
	ctx context.Context,
	client *ecr.Client,
	wantScan bool,
	wantTag string,
) (ecrtypes.Repository, error) {
	resp, err := client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{repositoryName},
	})
	if err != nil {
		return ecrtypes.Repository{}, fmt.Errorf("describe repository: %w", err)
	}
	if len(resp.Repositories) != 1 {
		return ecrtypes.Repository{}, fmt.Errorf(
			"describe repository returned %d results, want 1", len(resp.Repositories))
	}
	repository := resp.Repositories[0]
	gotScan := repository.ImageScanningConfiguration != nil &&
		repository.ImageScanningConfiguration.ScanOnPush
	if gotScan != wantScan {
		return ecrtypes.Repository{}, fmt.Errorf(
			"repository scan-on-push is %t, want %t", gotScan, wantScan)
	}
	tags, err := client.ListTagsForResource(ctx, &ecr.ListTagsForResourceInput{
		ResourceArn: repository.RepositoryArn,
	})
	if err != nil {
		return ecrtypes.Repository{}, fmt.Errorf("list repository tags: %w", err)
	}
	if got := tagValue(tags.Tags, "unobin"); got != wantTag {
		return ecrtypes.Repository{}, fmt.Errorf(
			"repository tag unobin is %q, want %q", got, wantTag)
	}
	return repository, nil
}

func tagValue(tags []ecrtypes.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}

func seedImages(ctx context.Context, client *ecr.Client) error {
	if err := putFixtureImage(ctx, client, "first", "2024-01-01T00:00:00Z"); err != nil {
		return err
	}
	time.Sleep(1100 * time.Millisecond)
	return putFixtureImage(ctx, client, "second", "2025-01-01T00:00:00Z")
}

type fixtureImage struct {
	configBody []byte
	layerBody  []byte
	manifest   string
}

func buildFixtureImage(tag string, created string) (fixtureImage, error) {
	content := []byte(tag + "\n")
	var rawLayer bytes.Buffer
	tarWriter := tar.NewWriter(&rawLayer)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     "fixture.txt",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
		ModTime:  time.Unix(0, 0).UTC(),
	}); err != nil {
		return fixtureImage{}, fmt.Errorf("write fixture layer header: %w", err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		return fixtureImage{}, fmt.Errorf("write fixture layer content: %w", err)
	}
	if err := tarWriter.Close(); err != nil {
		return fixtureImage{}, fmt.Errorf("close fixture layer tar: %w", err)
	}

	var compressedLayer bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedLayer)
	if _, err := gzipWriter.Write(rawLayer.Bytes()); err != nil {
		return fixtureImage{}, fmt.Errorf("compress fixture layer: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fixtureImage{}, fmt.Errorf("close fixture layer gzip: %w", err)
	}

	configBody := fmt.Appendf(nil,
		`{"architecture":"amd64","config":{"Labels":{"fixture":"%s"}},`+
			`"created":"%s","os":"linux","rootfs":`+
			`{"diff_ids":["%s"],"type":"layers"}}`,
		tag,
		created,
		bodyDigest(rawLayer.Bytes()),
	)
	layerBody := compressedLayer.Bytes()
	manifest := string(fmt.Appendf(nil,
		`{"schemaVersion":2,"mediaType":`+
			`"application/vnd.docker.distribution.manifest.v2+json",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json",`+
			`"size":%d,"digest":"%s"},`+
			`"layers":[{"mediaType":`+
			`"application/vnd.docker.image.rootfs.diff.tar.gzip",`+
			`"size":%d,"digest":"%s"}]}`,
		len(configBody),
		bodyDigest(configBody),
		len(layerBody),
		bodyDigest(layerBody),
	))
	return fixtureImage{
		configBody: configBody,
		layerBody:  layerBody,
		manifest:   manifest,
	}, nil
}

func putFixtureImage(
	ctx context.Context,
	client *ecr.Client,
	tag string,
	created string,
) error {
	image, err := buildFixtureImage(tag, created)
	if err != nil {
		return fmt.Errorf("build fixture image for tag %s: %w", tag, err)
	}
	if _, err := uploadBlob(ctx, client, image.configBody); err != nil {
		return fmt.Errorf("upload config for tag %s: %w", tag, err)
	}
	if _, err := uploadBlob(ctx, client, image.layerBody); err != nil {
		return fmt.Errorf("upload layer for tag %s: %w", tag, err)
	}
	if _, err := client.PutImage(ctx, &ecr.PutImageInput{
		RepositoryName: aws.String(repositoryName),
		ImageTag:       aws.String(tag),
		ImageManifest:  aws.String(image.manifest),
	}); err != nil {
		return fmt.Errorf("put image tag %s: %w", tag, err)
	}
	return nil
}

func bodyDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func uploadBlob(ctx context.Context, client *ecr.Client, body []byte) (string, error) {
	digest := bodyDigest(body)
	started, err := client.InitiateLayerUpload(ctx, &ecr.InitiateLayerUploadInput{
		RepositoryName: aws.String(repositoryName),
	})
	if err != nil {
		return "", fmt.Errorf("initiate layer upload: %w", err)
	}
	lastByte := int64(len(body) - 1)
	if _, err := client.UploadLayerPart(ctx, &ecr.UploadLayerPartInput{
		RepositoryName: aws.String(repositoryName),
		UploadId:       started.UploadId,
		PartFirstByte:  aws.Int64(0),
		PartLastByte:   aws.Int64(lastByte),
		LayerPartBlob:  body,
	}); err != nil {
		return "", fmt.Errorf("upload layer part: %w", err)
	}
	if _, err := client.CompleteLayerUpload(ctx, &ecr.CompleteLayerUploadInput{
		RepositoryName: aws.String(repositoryName),
		UploadId:       started.UploadId,
		LayerDigests:   []string{digest},
	}); err != nil {
		return "", fmt.Errorf("complete layer upload: %w", err)
	}
	return digest, nil
}

func verifyImageReads(
	ctx context.Context,
	client *ecr.Client,
	repository ecrtypes.Repository,
	planName string,
) error {
	images, err := describeImages(ctx, client)
	if err != nil {
		return err
	}
	if len(images) != 2 {
		return fmt.Errorf("repository has %d images, want 2", len(images))
	}
	tagged, err := imageWithTag(images, "first")
	if err != nil {
		return err
	}
	steps, err := readDataSteps(planName)
	if err != nil {
		return err
	}
	if err := checkStep(
		steps,
		"data-source.images['tagged']",
		tagged,
		repository,
		"first",
	); err != nil {
		return err
	}
	if err := checkStep(
		steps,
		"data-source.images['newest']",
		newestImage(images),
		repository,
		"second",
	); err != nil {
		return err
	}
	fmt.Printf("ok: ECR image data sources match cloud state in %s\n", planName)
	return nil
}

func describeImages(ctx context.Context, client *ecr.Client) ([]ecrtypes.ImageDetail, error) {
	var images []ecrtypes.ImageDetail
	pager := ecr.NewDescribeImagesPaginator(client, &ecr.DescribeImagesInput{
		RepositoryName: aws.String(repositoryName),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe images: %w", err)
		}
		images = append(images, page.ImageDetails...)
	}
	return images, nil
}

func imageWithTag(images []ecrtypes.ImageDetail, tag string) (ecrtypes.ImageDetail, error) {
	for _, image := range images {
		if slices.Contains(image.ImageTags, tag) {
			return image, nil
		}
	}
	return ecrtypes.ImageDetail{}, fmt.Errorf("image with tag %s not found", tag)
}

func newestImage(images []ecrtypes.ImageDetail) ecrtypes.ImageDetail {
	selected := images[0]
	for _, image := range images[1:] {
		if aws.ToTime(image.ImagePushedAt).After(aws.ToTime(selected.ImagePushedAt)) {
			selected = image
		}
	}
	return selected
}

type envelope struct {
	Ciphertext string `json:"ciphertext"`
}

type planFile struct {
	Steps []planStep `json:"steps"`
}

type planStep struct {
	Address         string         `json:"address"`
	NodeKind        string         `json:"node-kind"`
	Decision        string         `json:"decision"`
	ObservedOutputs map[string]any `json:"observed-outputs"`
}

func readDataSteps(planName string) (map[string]map[string]any, error) {
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	if buildDir == "" {
		return nil, errors.New("VERIFY_BUILD_DIR is required")
	}
	body, err := os.ReadFile(filepath.Join(buildDir, planName))
	if err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode plan envelope: %w", err)
	}
	plain, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode plan body: %w", err)
	}
	var plan planFile
	if err := json.Unmarshal(plain, &plan); err != nil {
		return nil, fmt.Errorf("decode plan JSON: %w", err)
	}
	steps := map[string]map[string]any{}
	for _, step := range plan.Steps {
		if step.NodeKind == "data-source" && step.Decision == "read" {
			steps[step.Address] = step.ObservedOutputs
		}
	}
	return steps, nil
}

func checkStep(
	steps map[string]map[string]any,
	address string,
	image ecrtypes.ImageDetail,
	repository ecrtypes.Repository,
	wantTag string,
) error {
	output, ok := steps[address]
	if !ok {
		return fmt.Errorf("plan has no read step for %s", address)
	}
	digest := aws.ToString(image.ImageDigest)
	wantStrings := map[string]string{
		"image-digest": digest,
		"image-uri": fmt.Sprintf(
			"%s@%s", aws.ToString(repository.RepositoryUri), digest),
		"registry-id": aws.ToString(image.RegistryId),
	}
	for field, want := range wantStrings {
		got, ok := output[field].(string)
		if !ok || got != want {
			return fmt.Errorf("%s %s is %v, want %q", address, field, output[field], want)
		}
	}
	if !containsString(output["image-tags"], wantTag) {
		return fmt.Errorf("%s image-tags does not contain %q", address, wantTag)
	}
	if _, ok := output["image-pushed-at"].(float64); !ok {
		return fmt.Errorf("%s image-pushed-at is %T, want number",
			address, output["image-pushed-at"])
	}
	if _, ok := output["image-size-in-bytes"].(float64); !ok {
		return fmt.Errorf("%s image-size-in-bytes is %T, want number",
			address, output["image-size-in-bytes"])
	}
	for _, inputOnly := range []string{"image-tag", "most-recent", "repository-name"} {
		if _, ok := output[inputOnly]; ok {
			return fmt.Errorf("%s unexpectedly returned input-only field %s", address, inputOnly)
		}
	}
	return nil
}

func containsString(value any, want string) bool {
	values, ok := value.([]any)
	if !ok {
		return false
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func verifyRepositoryDestroyed(ctx context.Context, client *ecr.Client) error {
	_, err := client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{repositoryName},
	})
	if err == nil {
		return fmt.Errorf("repository %s still exists", repositoryName)
	}
	var notFound *ecrtypes.RepositoryNotFoundException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("describe destroyed repository: %w", err)
	}
	fmt.Printf("ok: repository %s destroyed\n", repositoryName)
	return nil
}
