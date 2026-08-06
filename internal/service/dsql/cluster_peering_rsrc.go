package dsql

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	awssvc "github.com/aws/aws-sdk-go-v2/service/dsql"
	awstypes "github.com/aws/aws-sdk-go-v2/service/dsql/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	clusterPeeringCreateTimeout = 30 * time.Minute
	clusterPeeringPollInterval  = 10 * time.Second
	clusterPeeringNotFoundLimit = 20
)

var (
	clusterPeeringRegionPattern       = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	clusterPeeringARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	clusterPeeringARNRegionPattern    = regexp.MustCompile(
		`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`,
	)
	clusterPeeringARNAccountPattern = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`,
	)
)

type clusterPeeringClient interface {
	GetCluster(
		context.Context,
		*awssvc.GetClusterInput,
		...func(*awssvc.Options),
	) (*awssvc.GetClusterOutput, error)
	UpdateCluster(
		context.Context,
		*awssvc.UpdateClusterInput,
		...func(*awssvc.Options),
	) (*awssvc.UpdateClusterOutput, error)
}

type clusterPeeringClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type systemClusterPeeringClock struct{}

type ClusterPeeringResource struct {
	Identifier    string   `ub:"identifier"`
	Clusters      []string `ub:"clusters"`
	WitnessRegion string   `ub:"witness-region"`
}

type ClusterPeeringResourceOutput struct {
	Identifier    string   `ub:"identifier"`
	Clusters      []string `ub:"clusters"`
	WitnessRegion string   `ub:"witness-region"`
}

func (*ClusterPeeringResource) SchemaVersion() int { return 1 }

func (*ClusterPeeringResource) ReplaceFields() []string { return nil }

func (r *ClusterPeeringResource) EquivalentInput(
	field string,
	prior ClusterPeeringResource,
	current ClusterPeeringResource,
) bool {
	if field != "clusters" {
		return false
	}
	return unorderedClusterPeeringSliceEqual(prior.Clusters, current.Clusters)
}

func (r *ClusterPeeringResource) ValidateInputs(context.Context, *awsCfg) error {
	if r.Identifier == "" {
		return errors.New("identifier must not be empty")
	}
	if len(r.Clusters) == 0 {
		return errors.New("clusters must contain at least one ARN")
	}
	for index, cluster := range r.Clusters {
		if !validClusterPeeringARN(cluster) {
			return fmt.Errorf("clusters[%d] must be a valid ARN", index)
		}
	}
	if !clusterPeeringRegionPattern.MatchString(r.WitnessRegion) {
		return errors.New("witness-region must be a valid AWS region")
	}
	return nil
}

func (r *ClusterPeeringResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*ClusterPeeringResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, systemClusterPeeringClock{})
}

func (r *ClusterPeeringResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *ClusterPeeringResourceOutput,
) (*ClusterPeeringResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior)
}

func (r *ClusterPeeringResource) Update(
	_ context.Context,
	_ *awsCfg,
	prior runtime.Prior[ClusterPeeringResource, *ClusterPeeringResourceOutput],
) (*ClusterPeeringResourceOutput, error) {
	if clusterPeeringInputsChanged(prior.Inputs, *r) {
		return nil, errors.New("cluster-peering update is not supported")
	}
	if prior.Observed != nil {
		return prior.Observed, nil
	}
	if prior.Outputs != nil {
		return prior.Outputs, nil
	}
	return r.desiredOutput(), nil
}

func (r *ClusterPeeringResource) Delete(
	_ context.Context,
	_ *awsCfg,
	_ *ClusterPeeringResourceOutput,
) error {
	return nil
}

func (r *ClusterPeeringResource) create(
	ctx context.Context,
	client clusterPeeringClient,
	clock clusterPeeringClock,
) (*ClusterPeeringResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	cluster, err := client.GetCluster(ctx, &awssvc.GetClusterInput{
		Identifier: aws.String(r.Identifier),
	})
	if err != nil {
		return nil, fmt.Errorf("get cluster: %w", err)
	}
	if cluster == nil || aws.ToString(cluster.Identifier) == "" {
		return nil, fmt.Errorf("cluster %q was not found", r.Identifier)
	}
	if cluster.Status != awstypes.ClusterStatusPendingSetup {
		return nil, fmt.Errorf(
			"cluster %q must be PENDING_SETUP before peering, got %s",
			r.Identifier,
			cluster.Status,
		)
	}
	_, err = client.UpdateCluster(ctx, &awssvc.UpdateClusterInput{
		Identifier:  aws.String(r.Identifier),
		ClientToken: aws.String(clusterPeeringClientToken()),
		MultiRegionProperties: &awstypes.MultiRegionProperties{
			Clusters:      slices.Clone(r.Clusters),
			WitnessRegion: aws.String(r.WitnessRegion),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("update cluster peering: %w", err)
	}
	finalCluster, err := waitClusterPeeringActive(
		ctx,
		client,
		r.Identifier,
		clock,
		clusterPeeringCreateTimeout,
	)
	if err != nil {
		return nil, err
	}
	if finalCluster.MultiRegionProperties == nil {
		return nil, fmt.Errorf("cluster %q has no multi-region properties after peering", r.Identifier)
	}
	return clusterPeeringOutputFromCluster(finalCluster), nil
}

func (r *ClusterPeeringResource) read(
	ctx context.Context,
	client clusterPeeringClient,
	prior *ClusterPeeringResourceOutput,
) (*ClusterPeeringResourceOutput, error) {
	identifier := r.readIdentifier(prior)
	resp, err := client.GetCluster(ctx, &awssvc.GetClusterInput{
		Identifier: aws.String(identifier),
	})
	if err != nil {
		if isClusterPeeringNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get cluster: %w", err)
	}
	if resp == nil || aws.ToString(resp.Identifier) == "" {
		return nil, runtime.ErrNotFound
	}
	if resp.MultiRegionProperties == nil {
		return nil, runtime.ErrNotFound
	}
	return clusterPeeringOutputFromCluster(resp), nil
}

func waitClusterPeeringActive(
	ctx context.Context,
	client clusterPeeringClient,
	identifier string,
	clock clusterPeeringClock,
	timeout time.Duration,
) (*awssvc.GetClusterOutput, error) {
	deadline := clock.Now().Add(timeout)
	activeCount := 0
	notFoundCount := 0
	for {
		resp, err := client.GetCluster(ctx, &awssvc.GetClusterInput{
			Identifier: aws.String(identifier),
		})
		if err != nil {
			if !isClusterPeeringNotFound(err) {
				return nil, fmt.Errorf("get cluster while waiting: %w", err)
			}
			activeCount = 0
			notFoundCount++
			if notFoundCount > clusterPeeringNotFoundLimit {
				return nil, runtime.ErrNotFound
			}
		} else if resp == nil || aws.ToString(resp.Identifier) == "" {
			activeCount = 0
			notFoundCount++
			if notFoundCount > clusterPeeringNotFoundLimit {
				return nil, runtime.ErrNotFound
			}
		} else {
			notFoundCount = 0
			switch resp.Status {
			case awstypes.ClusterStatusActive:
				activeCount++
				if activeCount >= 2 {
					return resp, nil
				}
			case awstypes.ClusterStatusCreating,
				awstypes.ClusterStatusPendingSetup,
				awstypes.ClusterStatusUpdating:
				activeCount = 0
			default:
				return nil, fmt.Errorf(
					"cluster %q reached status %s while waiting for ACTIVE",
					identifier,
					resp.Status,
				)
			}
		}
		if !clock.Now().Before(deadline) {
			return nil, fmt.Errorf("timed out waiting for cluster %q to become ACTIVE", identifier)
		}
		if err := clock.Sleep(ctx, clusterPeeringPollInterval); err != nil {
			return nil, err
		}
	}
}

func (systemClusterPeeringClock) Now() time.Time { return time.Now() }

func (systemClusterPeeringClock) Sleep(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *ClusterPeeringResource) readIdentifier(prior *ClusterPeeringResourceOutput) string {
	if prior != nil && prior.Identifier != "" {
		return prior.Identifier
	}
	return r.Identifier
}

func (r *ClusterPeeringResource) desiredOutput() *ClusterPeeringResourceOutput {
	return &ClusterPeeringResourceOutput{
		Identifier:    r.Identifier,
		Clusters:      stableClusterPeeringClusters(r.Clusters, ""),
		WitnessRegion: r.WitnessRegion,
	}
}

func clusterPeeringOutputFromCluster(resp *awssvc.GetClusterOutput) *ClusterPeeringResourceOutput {
	return &ClusterPeeringResourceOutput{
		Identifier: aws.ToString(resp.Identifier),
		Clusters: stableClusterPeeringClusters(
			resp.MultiRegionProperties.Clusters,
			aws.ToString(resp.Arn),
		),
		WitnessRegion: aws.ToString(resp.MultiRegionProperties.WitnessRegion),
	}
}

func stableClusterPeeringClusters(clusters []string, selfARN string) []string {
	out := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		if selfARN != "" && strings.EqualFold(cluster, selfARN) {
			continue
		}
		out = append(out, cluster)
	}
	slices.Sort(out)
	return out
}

func clusterPeeringInputsChanged(prior, current ClusterPeeringResource) bool {
	return prior.Identifier != current.Identifier ||
		prior.WitnessRegion != current.WitnessRegion ||
		!unorderedClusterPeeringSliceEqual(prior.Clusters, current.Clusters)
}

func unorderedClusterPeeringSliceEqual[T any](prior, current []T) bool {
	if len(prior) != len(current) {
		return false
	}
	matched := make([]bool, len(current))
	for _, oldValue := range prior {
		found := false
		for index, newValue := range current {
			if matched[index] || !reflect.DeepEqual(oldValue, newValue) {
				continue
			}
			matched[index] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func validClusterPeeringARN(value string) bool {
	if value == "" {
		return false
	}
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return false
	}
	if !clusterPeeringARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !clusterPeeringARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" && !clusterPeeringARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}

func isClusterPeeringNotFound(err error) bool {
	var notFound *awstypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func clusterPeeringClientToken() string {
	return fmt.Sprintf("cluster-peering-%d", time.Now().UnixNano())
}
