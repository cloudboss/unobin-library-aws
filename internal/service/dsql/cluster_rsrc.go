package dsql

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	awssdksql "github.com/aws/aws-sdk-go-v2/service/dsql"
	dsqltypes "github.com/aws/aws-sdk-go-v2/service/dsql/types"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

const awsOwnedKMSKey = "AWS_OWNED_KMS_KEY"

const (
	defaultWaitInterval       = 10 * time.Second
	defaultWaitTimeout        = 10 * time.Minute
	defaultDeleteInitialDelay = time.Minute
)

type ClusterMultiRegionProperties struct {
	Clusters      *[]string `ub:"clusters"`
	WitnessRegion *string   `ub:"witness-region"`
}

type ClusterEncryptionDetails struct {
	EncryptionStatus string `ub:"encryption-status"`
	EncryptionType   string `ub:"encryption-type"`
}

type ClusterResource struct {
	DeletionProtectionEnabled bool                          `ub:"deletion-protection-enabled"`
	ForceDestroy              bool                          `ub:"force-destroy"`
	KmsEncryptionKey          *string                       `ub:"kms-encryption-key"`
	MultiRegionProperties     *ClusterMultiRegionProperties `ub:"multi-region-properties"`
	Tags                      *map[string]string            `ub:"tags"`
}

type ClusterResourceOutput struct {
	Arn                       string                        `ub:"arn"`
	Identifier                string                        `ub:"identifier"`
	DeletionProtectionEnabled bool                          `ub:"deletion-protection-enabled"`
	EncryptionDetails         ClusterEncryptionDetails      `ub:"encryption-details"`
	KmsEncryptionKey          string                        `ub:"kms-encryption-key"`
	MultiRegionProperties     *ClusterMultiRegionProperties `ub:"multi-region-properties"`
	VpcEndpointServiceName    string                        `ub:"vpc-endpoint-service-name"`
	Tags                      map[string]string             `ub:"tags"`
}

type clusterOperationOptions struct {
	waitInterval       time.Duration
	waitTimeout        time.Duration
	deleteInitialDelay time.Duration
}

type clusterClient interface {
	CreateCluster(context.Context, *awssdksql.CreateClusterInput, ...func(*awssdksql.Options)) (
		*awssdksql.CreateClusterOutput, error)
	GetCluster(context.Context, *awssdksql.GetClusterInput, ...func(*awssdksql.Options)) (
		*awssdksql.GetClusterOutput, error)
	UpdateCluster(context.Context, *awssdksql.UpdateClusterInput, ...func(*awssdksql.Options)) (
		*awssdksql.UpdateClusterOutput, error)
	DeleteCluster(context.Context, *awssdksql.DeleteClusterInput, ...func(*awssdksql.Options)) (
		*awssdksql.DeleteClusterOutput, error)
	GetVpcEndpointServiceName(
		context.Context,
		*awssdksql.GetVpcEndpointServiceNameInput,
		...func(*awssdksql.Options),
	) (*awssdksql.GetVpcEndpointServiceNameOutput, error)
	ListTagsForResource(
		context.Context,
		*awssdksql.ListTagsForResourceInput,
		...func(*awssdksql.Options),
	) (*awssdksql.ListTagsForResourceOutput, error)
	TagResource(context.Context, *awssdksql.TagResourceInput, ...func(*awssdksql.Options)) (
		*awssdksql.TagResourceOutput, error)
	UntagResource(context.Context, *awssdksql.UntagResourceInput, ...func(*awssdksql.Options)) (
		*awssdksql.UntagResourceOutput, error)
}

func (r *ClusterResource) SchemaVersion() int { return 1 }

func (r *ClusterResource) ReplaceFields() []string { return nil }

func (r ClusterResource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.DeletionProtectionEnabled, false),
		defaults.Value(r.ForceDestroy, false),
	}
}

func (r *ClusterResource) ValidateInputs(context.Context, *awsCfg) error {
	if r.KmsEncryptionKey == nil {
		return nil
	}
	if *r.KmsEncryptionKey == awsOwnedKMSKey || arn.IsARN(*r.KmsEncryptionKey) {
		return nil
	}
	return fmt.Errorf("kms-encryption-key must be an ARN or %s", awsOwnedKMSKey)
}

func (r *ClusterResource) ModifyResourcePlan(
	req runtime.ResourcePlanRequest[ClusterResource, *ClusterResourceOutput, *awsCfg],
	resp *runtime.ResourcePlanResponse,
) error {
	if !req.HasPriorState {
		return nil
	}
	return conditionalClusterReplacement(req.PriorInputs, req.CurrentInputs)
}

func (r *ClusterResource) EquivalentInput(
	field string,
	prior,
	current ClusterResource,
) bool {
	if field != "multi-region-properties.clusters" {
		return false
	}
	return slices.Equal(
		sortedStringValues(multiRegionClusters(prior.MultiRegionProperties)),
		sortedStringValues(multiRegionClusters(current.MultiRegionProperties)),
	)
}

func (r *ClusterResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*ClusterResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createWithClient(ctx, client, defaultClusterOperationOptions())
}

func (r *ClusterResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *ClusterResourceOutput,
) (*ClusterResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.readWithClient(ctx, client, prior)
}

func (r *ClusterResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[ClusterResource, *ClusterResourceOutput],
) (*ClusterResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.updateWithClient(ctx, client, prior, defaultClusterOperationOptions())
}

func (r *ClusterResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *ClusterResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.deleteWithClient(ctx, client, prior, defaultClusterOperationOptions())
}

func (r *ClusterResource) createWithClient(
	ctx context.Context,
	client clusterClient,
	options clusterOperationOptions,
) (*ClusterResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	token, err := newClientToken()
	if err != nil {
		return nil, err
	}
	input := &awssdksql.CreateClusterInput{
		ClientToken:               aws.String(token),
		DeletionProtectionEnabled: aws.Bool(r.DeletionProtectionEnabled),
		KmsEncryptionKey:          r.KmsEncryptionKey,
		MultiRegionProperties:     r.MultiRegionProperties.sdk(),
		Tags:                      userTags(ptr.Value(r.Tags)),
	}
	resp, err := client.CreateCluster(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("create cluster: %w", err)
	}
	if resp == nil || resp.Identifier == nil || resp.Arn == nil {
		return nil, errors.New("create cluster: response has no cluster identity")
	}
	identifier := aws.ToString(resp.Identifier)
	if err := waitClusterCreated(ctx, client, identifier, options); err != nil {
		return nil, err
	}
	return r.readWithClient(ctx, client, &ClusterResourceOutput{
		Arn:        aws.ToString(resp.Arn),
		Identifier: identifier,
	})
}

func (r *ClusterResource) readWithClient(
	ctx context.Context,
	client clusterClient,
	prior *ClusterResourceOutput,
) (*ClusterResourceOutput, error) {
	if prior == nil || prior.Identifier == "" {
		return nil, runtime.ErrNotFound
	}
	out, err := findCluster(ctx, client, prior.Identifier)
	if err != nil {
		return nil, err
	}
	vpcEndpoint, err := readVpcEndpointServiceName(ctx, client, out.Identifier)
	if err != nil {
		return nil, err
	}
	tags, err := readTags(ctx, client, out.Arn)
	if err != nil {
		return nil, err
	}
	return clusterOutput(out, vpcEndpoint, tags), nil
}

func (r *ClusterResource) updateWithClient(
	ctx context.Context,
	client clusterClient,
	prior runtime.Prior[ClusterResource, *ClusterResourceOutput],
	options clusterOperationOptions,
) (*ClusterResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.Identifier == "" {
		return nil, errors.New("prior cluster output has no identifier")
	}
	if err := conditionalClusterReplacement(prior.Inputs, *r); err != nil {
		return nil, err
	}
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	identifier := prior.Outputs.Identifier
	if input, needed, err := r.updateInput(prior, identifier); err != nil {
		return nil, err
	} else if needed {
		if _, err := client.UpdateCluster(ctx, input); err != nil {
			return nil, fmt.Errorf("update cluster: %w", err)
		}
		if err := waitClusterUpdated(ctx, client, identifier, options); err != nil {
			return nil, err
		}
		if input.KmsEncryptionKey != nil {
			if err := waitClusterEncryptionEnabled(ctx, client, identifier, options); err != nil {
				return nil, err
			}
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := r.syncTags(ctx, client, prior.Outputs.Arn); err != nil {
			return nil, err
		}
	}
	return r.readWithClient(ctx, client, prior.Outputs)
}

func (r *ClusterResource) deleteWithClient(
	ctx context.Context,
	client clusterClient,
	prior *ClusterResourceOutput,
	options clusterOperationOptions,
) error {
	if prior == nil || prior.Identifier == "" {
		return errors.New("prior cluster output has no identifier")
	}
	if r.ForceDestroy {
		token, err := newClientToken()
		if err != nil {
			return err
		}
		_, err = client.UpdateCluster(ctx, &awssdksql.UpdateClusterInput{
			Identifier:                aws.String(prior.Identifier),
			ClientToken:               aws.String(token),
			DeletionProtectionEnabled: aws.Bool(false),
		})
		if err != nil {
			if !isNotFound(err) {
				return fmt.Errorf("disable deletion protection: %w", err)
			}
			return nil
		}
	}
	_, err := client.DeleteCluster(ctx, &awssdksql.DeleteClusterInput{
		Identifier: aws.String(prior.Identifier),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete cluster: %w", err)
	}
	if options.deleteInitialDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(options.deleteInitialDelay):
		}
	}
	return waitClusterDeleted(ctx, client, prior.Identifier, options)
}

func waitClusterCreated(
	ctx context.Context,
	client clusterClient,
	identifier string,
	options clusterOperationOptions,
) error {
	return wait.UntilStable(ctx, fmt.Sprintf("cluster %s", identifier), 2,
		func(ctx context.Context) (bool, error) {
			out, err := findCluster(ctx, client, identifier)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					return false, nil
				}
				return false, err
			}
			switch out.Status {
			case dsqltypes.ClusterStatusCreating:
				return false, nil
			case dsqltypes.ClusterStatusActive, dsqltypes.ClusterStatusPendingSetup:
				return true, nil
			default:
				return false, fmt.Errorf(
					"cluster %s entered unexpected status %s", identifier, out.Status)
			}
		},
		waitOptions(options)...,
	)
}

func waitClusterUpdated(
	ctx context.Context,
	client clusterClient,
	identifier string,
	options clusterOperationOptions,
) error {
	return wait.Until(ctx, fmt.Sprintf("cluster %s", identifier),
		func(ctx context.Context) (bool, error) {
			out, err := findCluster(ctx, client, identifier)
			if err != nil {
				return false, err
			}
			switch out.Status {
			case dsqltypes.ClusterStatusUpdating:
				return false, nil
			case dsqltypes.ClusterStatusActive:
				return true, nil
			default:
				return false, fmt.Errorf(
					"cluster %s entered unexpected status %s", identifier, out.Status)
			}
		},
		waitOptions(options)...,
	)
}

func waitClusterDeleted(
	ctx context.Context,
	client clusterClient,
	identifier string,
	options clusterOperationOptions,
) error {
	return wait.Until(ctx, fmt.Sprintf("cluster %s to be deleted", identifier),
		func(ctx context.Context) (bool, error) {
			out, err := findCluster(ctx, client, identifier)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					return true, nil
				}
				return false, err
			}
			switch out.Status {
			case dsqltypes.ClusterStatusDeleting, dsqltypes.ClusterStatusPendingDelete:
				return false, nil
			default:
				return false, fmt.Errorf(
					"cluster %s entered unexpected status %s while deleting",
					identifier,
					out.Status,
				)
			}
		},
		waitOptions(options)...,
	)
}

func (r *ClusterResource) updateInput(
	prior runtime.Prior[ClusterResource, *ClusterResourceOutput],
	identifier string,
) (*awssdksql.UpdateClusterInput, bool, error) {
	token, err := newClientToken()
	if err != nil {
		return nil, false, err
	}
	input := &awssdksql.UpdateClusterInput{
		Identifier:  aws.String(identifier),
		ClientToken: aws.String(token),
	}
	needed := false
	if runtime.Changed(prior.Inputs.DeletionProtectionEnabled, r.DeletionProtectionEnabled) {
		needed = true
		input.DeletionProtectionEnabled = aws.Bool(r.DeletionProtectionEnabled)
	}
	if runtime.Changed(prior.Inputs.KmsEncryptionKey, r.KmsEncryptionKey) {
		needed = true
		input.KmsEncryptionKey = r.KmsEncryptionKey
	}
	if runtime.Changed(prior.Inputs.MultiRegionProperties, r.MultiRegionProperties) {
		needed = true
		input.MultiRegionProperties = r.MultiRegionProperties.sdk()
	}
	return input, needed, nil
}

func waitClusterEncryptionEnabled(
	ctx context.Context,
	client clusterClient,
	identifier string,
	options clusterOperationOptions,
) error {
	return wait.Until(ctx, fmt.Sprintf("cluster %s encryption", identifier),
		func(ctx context.Context) (bool, error) {
			out, err := findCluster(ctx, client, identifier)
			if err != nil {
				return false, err
			}
			if out.EncryptionDetails == nil {
				return false, errors.New("cluster encryption details are missing")
			}
			switch out.EncryptionDetails.EncryptionStatus {
			case dsqltypes.EncryptionStatusEnabling, dsqltypes.EncryptionStatusUpdating:
				return false, nil
			case dsqltypes.EncryptionStatusEnabled:
				return true, nil
			default:
				return false, fmt.Errorf(
					"cluster %s entered unexpected encryption status %s",
					identifier,
					out.EncryptionDetails.EncryptionStatus,
				)
			}
		},
		waitOptions(options)...,
	)
}

func findCluster(
	ctx context.Context,
	client clusterClient,
	identifier string,
) (*awssdksql.GetClusterOutput, error) {
	out, err := client.GetCluster(ctx, &awssdksql.GetClusterInput{
		Identifier: aws.String(identifier),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get cluster: %w", err)
	}
	if out == nil || out.Identifier == nil || out.Arn == nil {
		return nil, runtime.ErrNotFound
	}
	return out, nil
}

func readVpcEndpointServiceName(
	ctx context.Context,
	client clusterClient,
	identifier *string,
) (string, error) {
	out, err := client.GetVpcEndpointServiceName(ctx, &awssdksql.GetVpcEndpointServiceNameInput{
		Identifier: identifier,
	})
	if err != nil {
		return "", fmt.Errorf("get vpc endpoint service name: %w", err)
	}
	if out == nil || aws.ToString(out.ServiceName) == "" {
		return "", errors.New("vpc endpoint service name is missing")
	}
	return aws.ToString(out.ServiceName), nil
}

func readTags(ctx context.Context, client clusterClient, arn *string) (map[string]string, error) {
	out, err := client.ListTagsForResource(ctx, &awssdksql.ListTagsForResourceInput{
		ResourceArn: arn,
	})
	if err != nil {
		return nil, fmt.Errorf("list tags for resource: %w", err)
	}
	if out == nil || out.Tags == nil {
		return map[string]string{}, nil
	}
	return maps.Clone(out.Tags), nil
}

func (r *ClusterResource) syncTags(ctx context.Context, client clusterClient, arn string) error {
	return tagsync.Sync(ctx, ptr.Value(r.Tags),
		func(ctx context.Context) (map[string]string, error) {
			return readTags(ctx, client, aws.String(arn))
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &awssdksql.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        userTags(upsert),
			})
			if err != nil {
				return fmt.Errorf("tag resource: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &awssdksql.UntagResourceInput{
				ResourceArn: aws.String(arn),
				TagKeys:     remove,
			})
			if err != nil {
				return fmt.Errorf("untag resource: %w", err)
			}
			return nil
		},
	)
}

func clusterOutput(
	out *awssdksql.GetClusterOutput,
	vpcEndpointServiceName string,
	tags map[string]string,
) *ClusterResourceOutput {
	return &ClusterResourceOutput{
		Arn:                       aws.ToString(out.Arn),
		Identifier:                aws.ToString(out.Identifier),
		DeletionProtectionEnabled: aws.ToBool(out.DeletionProtectionEnabled),
		EncryptionDetails:         clusterEncryptionDetails(out.EncryptionDetails),
		KmsEncryptionKey:          clusterKMSKey(out.EncryptionDetails),
		MultiRegionProperties: normalizeMultiRegionProperties(
			out.MultiRegionProperties,
			aws.ToString(out.Arn),
		),
		VpcEndpointServiceName: vpcEndpointServiceName,
		Tags:                   tags,
	}
}

func clusterEncryptionDetails(details *dsqltypes.EncryptionDetails) ClusterEncryptionDetails {
	if details == nil {
		return ClusterEncryptionDetails{}
	}
	return ClusterEncryptionDetails{
		EncryptionStatus: string(details.EncryptionStatus),
		EncryptionType:   string(details.EncryptionType),
	}
}

func clusterKMSKey(details *dsqltypes.EncryptionDetails) string {
	if details == nil {
		return ""
	}
	if details.EncryptionType == dsqltypes.EncryptionTypeAwsOwnedKmsKey {
		return awsOwnedKMSKey
	}
	return aws.ToString(details.KmsKeyArn)
}

func normalizeMultiRegionProperties(
	props *dsqltypes.MultiRegionProperties,
	selfARN string,
) *ClusterMultiRegionProperties {
	if props == nil {
		return nil
	}
	clusters := make([]string, 0, len(props.Clusters))
	for _, cluster := range props.Clusters {
		if !strings.EqualFold(cluster, selfARN) {
			clusters = append(clusters, cluster)
		}
	}
	return &ClusterMultiRegionProperties{
		Clusters:      &clusters,
		WitnessRegion: props.WitnessRegion,
	}
}

func (p *ClusterMultiRegionProperties) sdk() *dsqltypes.MultiRegionProperties {
	if p == nil {
		return nil
	}
	return &dsqltypes.MultiRegionProperties{
		Clusters:      slices.Clone(ptr.Value(p.Clusters)),
		WitnessRegion: p.WitnessRegion,
	}
}

func userTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, key := range slices.Sorted(maps.Keys(tags)) {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = tags[key]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func conditionalClusterReplacement(prior, current ClusterResource) error {
	if runtime.Changed(
		multiRegionWitnessRegion(prior.MultiRegionProperties),
		multiRegionWitnessRegion(current.MultiRegionProperties),
	) {
		return fmt.Errorf("change to multi-region-properties.witness-region requires replacement")
	}
	return nil
}

func multiRegionWitnessRegion(props *ClusterMultiRegionProperties) *string {
	if props == nil {
		return nil
	}
	return props.WitnessRegion
}

func multiRegionClusters(props *ClusterMultiRegionProperties) []string {
	if props == nil || props.Clusters == nil {
		return nil
	}
	return *props.Clusters
}

func sortedStringValues(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return out
}

func isNotFound(err error) bool {
	var notFound *dsqltypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func defaultClusterOperationOptions() clusterOperationOptions {
	return clusterOperationOptions{
		waitInterval:       defaultWaitInterval,
		waitTimeout:        defaultWaitTimeout,
		deleteInitialDelay: defaultDeleteInitialDelay,
	}
}

func waitOptions(options clusterOperationOptions) []wait.Option {
	return []wait.Option{
		wait.WithInterval(options.waitInterval),
		wait.WithTimeout(options.waitTimeout),
	}
}

func newClientToken() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate client token: %w", err)
	}
	random[6] = (random[6] & 0x0f) | 0x40
	random[8] = (random[8] & 0x3f) | 0x80
	var token [36]byte
	hex.Encode(token[0:8], random[0:4])
	token[8] = '-'
	hex.Encode(token[9:13], random[4:6])
	token[13] = '-'
	hex.Encode(token[14:18], random[6:8])
	token[18] = '-'
	hex.Encode(token[19:23], random[8:10])
	token[23] = '-'
	hex.Encode(token[24:36], random[10:16])
	return string(token[:]), nil
}
