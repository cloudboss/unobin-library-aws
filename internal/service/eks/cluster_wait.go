package eks

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const (
	clusterCreateRetryTimeout = 2 * time.Minute
	clusterCreateWaitTimeout  = 30 * time.Minute
	clusterUpdateWaitTimeout  = 60 * time.Minute
	clusterDeleteRetryTimeout = 60 * time.Minute
	clusterDeleteWaitTimeout  = 15 * time.Minute
)

type clusterClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
	RandomDelay(time.Duration) time.Duration
}

type systemClusterClock struct{}

func (systemClusterClock) Now() time.Time { return time.Now() }

func (systemClusterClock) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func (systemClusterClock) RandomDelay(maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(maximum.Nanoseconds()))
	if err != nil {
		return maximum - time.Nanosecond
	}
	return time.Duration(value.Int64())
}

func retryClusterCreate(
	ctx context.Context,
	clock clusterClock,
	create func(context.Context) error,
) error {
	deadline := clock.Now().Add(clusterCreateRetryTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		err := create(ctx)
		if err == nil || !isClusterCreateRetryable(err) {
			return err
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("create retry timed out after %s: %w",
				clusterCreateRetryTimeout, err)
		}
	}
}

func isClusterCreateRetryable(err error) bool {
	var invalid *ekstypes.InvalidParameterException
	if !errors.As(err, &invalid) {
		return false
	}
	message := invalid.ErrorMessage()
	for _, fragment := range []string{
		"does not exist",
		"Error in role params",
		"Role could not be assumed because the trusted entity is not correct",
		"The provided role doesn't have the Amazon EKS Managed Policies associated with it",
		"IAM role's policy must include",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func isClusterNotFound(err error) bool {
	var notFound *ekstypes.ResourceNotFoundException
	if errors.As(err, &notFound) {
		return true
	}
	var client *ekstypes.ClientException
	return errors.As(err, &client) &&
		strings.Contains(client.ErrorMessage(), "No cluster found for name:")
}

func waitClusterCreated(
	ctx context.Context,
	client interface {
		DescribeCluster(context.Context, *eks.DescribeClusterInput,
			...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	},
	name string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(clusterCreateWaitTimeout)
	misses := 0
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		cluster, err := describeCluster(ctx, client, name)
		if errors.Is(err, runtime.ErrNotFound) {
			misses++
			if misses > 20 {
				return fmt.Errorf("cluster %s not found after 20 checks", name)
			}
		} else if err != nil {
			return err
		} else {
			misses = 0
			switch cluster.Status {
			case ekstypes.ClusterStatusActive:
				return nil
			case ekstypes.ClusterStatusPending, ekstypes.ClusterStatusCreating:
			default:
				return fmt.Errorf("cluster %s reached unexpected status %s", name, cluster.Status)
			}
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for cluster %s ACTIVE timed out after %s",
				name, clusterCreateWaitTimeout)
		}
	}
}

func waitClusterUpdate(
	ctx context.Context,
	client interface {
		DescribeUpdate(context.Context, *eks.DescribeUpdateInput,
			...func(*eks.Options)) (*eks.DescribeUpdateOutput, error)
	},
	name string,
	id string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(clusterUpdateWaitTimeout)
	misses := 0
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		update, err := describeClusterUpdate(ctx, client, name, id)
		if errors.Is(err, runtime.ErrNotFound) {
			misses++
			if misses > 20 {
				return fmt.Errorf("cluster update %s not found after 20 checks", id)
			}
		} else if err != nil {
			return err
		} else {
			misses = 0
			switch update.Status {
			case ekstypes.UpdateStatusSuccessful:
				return nil
			case ekstypes.UpdateStatusInProgress:
			case ekstypes.UpdateStatusCancelled, ekstypes.UpdateStatusFailed:
				return fmt.Errorf("cluster update %s is %s: %s",
					id, update.Status, formatUpdateErrors(update.Errors))
			default:
				return fmt.Errorf("cluster update %s reached unexpected status %s",
					id, update.Status)
			}
		}
		if !sleepClusterRetry(ctx, clock, deadline, clusterBackoff(attempt)) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for cluster update %s timed out after %s",
				id, clusterUpdateWaitTimeout)
		}
	}
}

func deleteCluster(
	ctx context.Context,
	client interface {
		DeleteCluster(context.Context, *eks.DeleteClusterInput,
			...func(*eks.Options)) (*eks.DeleteClusterOutput, error)
	},
	name string,
	clock clusterClock,
) (bool, error) {
	deadline := clock.Now().Add(clusterDeleteRetryTimeout)
	delay := clock.RandomDelay(time.Minute)
	if delay >= time.Minute {
		delay = time.Minute - time.Nanosecond
	}
	if delay > 0 && !sleepClusterRetry(ctx, clock, deadline, delay) {
		if cause := context.Cause(ctx); cause != nil {
			return false, cause
		}
		return false, fmt.Errorf("delete cluster %s retry delay exceeded %s",
			name, clusterDeleteRetryTimeout)
	}
	for {
		if err := context.Cause(ctx); err != nil {
			return false, err
		}
		_, err := client.DeleteCluster(ctx, &eks.DeleteClusterInput{Name: aws.String(name)})
		if err == nil {
			return true, nil
		}
		if isClusterNotFound(err) {
			return false, nil
		}
		if !isClusterDeleteRetryable(err) {
			return false, fmt.Errorf("delete cluster %s: %w", name, err)
		}
		if !sleepClusterRetry(ctx, clock, deadline, 30*time.Second) {
			if cause := context.Cause(ctx); cause != nil {
				return false, cause
			}
			return false, fmt.Errorf("delete cluster %s timed out after %s: %w",
				name, clusterDeleteRetryTimeout, err)
		}
	}
}

func waitClusterDeleted(
	ctx context.Context,
	client interface {
		DescribeCluster(context.Context, *eks.DescribeClusterInput,
			...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	},
	name string,
	clock clusterClock,
) error {
	deadline := clock.Now().Add(clusterDeleteWaitTimeout)
	consecutiveMisses := 0
	for {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		_, err := describeCluster(ctx, client, name)
		if errors.Is(err, runtime.ErrNotFound) {
			consecutiveMisses++
			if consecutiveMisses == 3 {
				return nil
			}
		} else if err != nil {
			return err
		} else {
			consecutiveMisses = 0
		}
		if !sleepClusterRetry(ctx, clock, deadline, 10*time.Second) {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return fmt.Errorf("waiting for cluster %s deletion timed out after %s",
				name, clusterDeleteWaitTimeout)
		}
	}
}

func describeCluster(
	ctx context.Context,
	client interface {
		DescribeCluster(context.Context, *eks.DescribeClusterInput,
			...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	},
	name string,
) (*ekstypes.Cluster, error) {
	output, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
	if err != nil {
		if isClusterNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe cluster %s: %w", name, err)
	}
	if output == nil || output.Cluster == nil {
		return nil, runtime.ErrNotFound
	}
	return output.Cluster, nil
}

func describeClusterUpdate(
	ctx context.Context,
	client interface {
		DescribeUpdate(context.Context, *eks.DescribeUpdateInput,
			...func(*eks.Options)) (*eks.DescribeUpdateOutput, error)
	},
	name string,
	id string,
) (*ekstypes.Update, error) {
	output, err := client.DescribeUpdate(ctx, &eks.DescribeUpdateInput{
		Name: aws.String(name), UpdateId: aws.String(id),
	})
	if err != nil {
		if isClusterNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe cluster update %s: %w", id, err)
	}
	if output == nil || output.Update == nil {
		return nil, runtime.ErrNotFound
	}
	return output.Update, nil
}

func isClusterDeleteRetryable(err error) bool {
	var inUse *ekstypes.ResourceInUseException
	return errors.As(err, &inUse) && strings.Contains(inUse.ErrorMessage(), "in progress")
}

func formatUpdateErrors(details []ekstypes.ErrorDetail) string {
	if len(details) == 0 {
		return "no error details"
	}
	formatted := make([]string, 0, len(details))
	for _, detail := range details {
		prefix := ""
		if len(detail.ResourceIds) > 0 {
			prefix = strings.Join(detail.ResourceIds, ", ") + ": "
		}
		formatted = append(formatted, fmt.Sprintf("%s%s: %s",
			prefix, detail.ErrorCode, aws.ToString(detail.ErrorMessage)))
	}
	return strings.Join(formatted, "; ")
}

func clusterBackoff(attempt int) time.Duration {
	delay := 500 * time.Millisecond
	for index := 0; index < attempt && delay < 10*time.Second; index++ {
		delay *= 2
	}
	if delay > 10*time.Second {
		return 10 * time.Second
	}
	return delay
}

func sleepClusterRetry(
	ctx context.Context,
	clock clusterClock,
	deadline time.Time,
	delay time.Duration,
) bool {
	remaining := deadline.Sub(clock.Now())
	if remaining <= 0 {
		return false
	}
	if delay > remaining {
		delay = remaining
	}
	return clock.Sleep(ctx, delay) == nil
}
