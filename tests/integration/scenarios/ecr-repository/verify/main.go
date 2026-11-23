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
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

const repositoryName = "unobin-it-ecr-repository"

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
		repository, err := verifyRepository(ctx, client, false, "initial")
		if err != nil {
			return err
		}
		if err := verifyLookup(ctx, client, repository, "config.ub", []string{}); err != nil {
			return err
		}
		if err := seedImages(ctx, client); err != nil {
			return err
		}
		return verifyLookup(ctx, client, repository, "config.ub", []string{"second"})
	case "updated":
		repository, err := verifyRepository(ctx, client, true, "updated")
		if err != nil {
			return err
		}
		return verifyLookup(ctx, client, repository, "update/config.ub", []string{"second"})
	case "destroyed":
		return verifyRepositoryDestroyed(ctx, client)
	default:
		return fmt.Errorf("unexpected VERIFY_PHASE %q", phase)
	}
}

func verifyLookup(
	ctx context.Context, client *ecr.Client, repository ecrtypes.Repository,
	configPath string, wantTags []string,
) error {
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	if buildDir == "" {
		return errors.New("VERIFY_BUILD_DIR is required")
	}
	command := exec.CommandContext(ctx, filepath.Join(buildDir, "ecr-repository"),
		"plan", "-c", configPath, "-o", "lookup.json")
	command.Dir = buildDir
	if out, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("plan repository lookup: %w\n%s", err, out)
	}
	steps, err := readDataSteps("lookup.json")
	if err != nil {
		return err
	}
	got, ok := steps["data-source.lookup"]
	if !ok {
		return errors.New("plan has no repository lookup read")
	}
	remoteTags, err := client.ListTagsForResource(ctx, &ecr.ListTagsForResourceInput{
		ResourceArn: repository.RepositoryArn,
	})
	if err != nil {
		return fmt.Errorf("list tags: %w", err)
	}
	tags := map[string]any{}
	for _, tag := range remoteTags.Tags {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	imageTags := []any{}
	for _, tag := range wantTags {
		imageTags = append(imageTags, tag)
	}
	filters := []any{}
	for _, filter := range repository.ImageTagMutabilityExclusionFilters {
		filters = append(filters, map[string]any{
			"filter": aws.ToString(filter.Filter), "filter-type": string(filter.FilterType),
		})
	}
	var encryption any
	if value := repository.EncryptionConfiguration; value != nil {
		encryption = map[string]any{
			"encryption-type": string(value.EncryptionType), "kms-key": aws.ToString(value.KmsKey),
		}
	}
	var scanning any
	if value := repository.ImageScanningConfiguration; value != nil {
		scanning = map[string]any{"scan-on-push": value.ScanOnPush}
	}
	want := map[string]any{
		"name": aws.ToString(repository.RepositoryName), "arn": aws.ToString(repository.RepositoryArn),
		"registry-id":              aws.ToString(repository.RegistryId),
		"repository-uri":           aws.ToString(repository.RepositoryUri),
		"encryption-configuration": encryption, "image-scanning-configuration": scanning,
		"image-tag-mutability":                   string(repository.ImageTagMutability),
		"image-tag-mutability-exclusion-filters": filters,
		"most-recent-image-tags":                 imageTags, "tags": tags,
	}
	if !reflect.DeepEqual(want, got) {
		return fmt.Errorf("repository lookup = %#v, want %#v", got, want)
	}
	images, err := describeImages(ctx, client)
	if err != nil {
		return err
	}
	actual := []string{}
	if len(images) > 0 {
		actual = append(actual, newestImage(images).ImageTags...)
	}
	if !slices.Equal(wantTags, actual) {
		return fmt.Errorf("newest remote image tags = %v, want %v", actual, wantTags)
	}
	fmt.Printf("ok: repository lookup matches cloud configuration and tags\n")
	return nil
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

func verifyRepositoryDestroyed(ctx context.Context, client *ecr.Client) error {
	_, err := client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{repositoryName},
	})
	if err == nil {
		return fmt.Errorf("repository %s still exists", repositoryName)
	}
	if _, ok := errors.AsType[*ecrtypes.RepositoryNotFoundException](err); !ok {
		return fmt.Errorf("describe destroyed repository: %w", err)
	}
	fmt.Printf("ok: repository %s destroyed\n", repositoryName)
	return nil
}
