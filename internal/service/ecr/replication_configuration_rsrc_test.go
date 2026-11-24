package ecr

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecrapi "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestReplicationConfigurationRequest(t *testing.T) {
	filters := []ReplicationRepositoryFilter{{Filter: "", FilterType: "PREFIX_MATCH"},
		{Filter: " x ", FilterType: "FUTURE"}}
	destination := ReplicationDestination{Region: "eusc-de-east-1", RegistryID: "000000000001"}
	r := ReplicationConfigurationResource{ReplicationConfiguration: &ReplicationConfiguration{
		Rules: []ReplicationRule{
			{Destinations: []ReplicationDestination{destination, destination}, RepositoryFilters: &filters},
			{Destinations: []ReplicationDestination{destination}},
		},
	}}
	require.Equal(t, &ecrapi.PutReplicationConfigurationInput{
		ReplicationConfiguration: &types.ReplicationConfiguration{Rules: []types.ReplicationRule{
			{Destinations: []types.ReplicationDestination{
				{Region: aws.String(destination.Region), RegistryId: aws.String(destination.RegistryID)},
				{Region: aws.String(destination.Region), RegistryId: aws.String(destination.RegistryID)},
			}, RepositoryFilters: []types.RepositoryFilter{
				{Filter: aws.String(""), FilterType: "PREFIX_MATCH"},
				{Filter: aws.String(" x "), FilterType: "FUTURE"},
			}},
			{Destinations: []types.ReplicationDestination{
				{Region: aws.String(destination.Region), RegistryId: aws.String(destination.RegistryID)},
			}},
		}},
	}, r.preparePut())
	empty := []ReplicationRepositoryFilter{}
	r.ReplicationConfiguration.Rules[1].RepositoryFilters = &empty
	request := r.preparePut()
	require.Nil(t, request.ReplicationConfiguration.Rules[1].RepositoryFilters)
	require.Equal(t, &ecrapi.PutReplicationConfigurationInput{},
		(ReplicationConfigurationResource{}).preparePut())
}

type replicationFake struct {
	calls          []string
	puts           []*ecrapi.PutReplicationConfigurationInput
	reads          []*ecrapi.DescribeRegistryOutput
	readErrors     []error
	putErrors      []error
	cleanupContext context.Context
	cleanupError   error
	cancel         context.CancelFunc
}

func (f *replicationFake) DescribeRegistry(_ context.Context, in *ecrapi.DescribeRegistryInput,
	_ ...func(*ecrapi.Options),
) (*ecrapi.DescribeRegistryOutput, error) {
	if !reflect.DeepEqual(in, &ecrapi.DescribeRegistryInput{}) {
		panic("describe must have no selectors")
	}
	f.calls = append(f.calls, "describe")
	index := 0
	for _, call := range f.calls {
		if call == "describe" {
			index++
		}
	}
	var err error
	if index <= len(f.readErrors) {
		err = f.readErrors[index-1]
	}
	if index <= len(f.reads) {
		return f.reads[index-1], err
	}
	return nil, err
}

func (f *replicationFake) PutReplicationConfiguration(ctx context.Context,
	in *ecrapi.PutReplicationConfigurationInput, _ ...func(*ecrapi.Options),
) (*ecrapi.PutReplicationConfigurationOutput, error) {
	f.calls = append(f.calls, "put")
	f.puts = append(f.puts, in)
	if len(f.puts) == 1 && f.cancel != nil {
		f.cancel()
	}
	if len(f.puts) > 1 {
		f.cleanupContext = ctx
		f.cleanupError = ctx.Err()
	}
	if len(f.puts) <= len(f.putErrors) {
		return nil, f.putErrors[len(f.puts)-1]
	}
	return &ecrapi.PutReplicationConfigurationOutput{
		ReplicationConfiguration: &types.ReplicationConfiguration{},
	}, nil
}

func replicationDesired(region string) ReplicationConfigurationResource {
	return ReplicationConfigurationResource{ReplicationConfiguration: &ReplicationConfiguration{
		Rules: []ReplicationRule{{Destinations: []ReplicationDestination{
			{Region: region, RegistryID: "000000000001"},
		}}},
	}}
}

func replicationIdentity() *ReplicationConfigurationResourceOutput {
	return &ReplicationConfigurationResourceOutput{RegistryID: "123456789012", Region: "us-east-1"}
}

func replicationDescription(r ReplicationConfigurationResource) *ecrapi.DescribeRegistryOutput {
	return &ecrapi.DescribeRegistryOutput{RegistryId: aws.String("123456789012"),
		ReplicationConfiguration: r.preparePut().ReplicationConfiguration}
}

func TestReplicationCreateAdoptsObservedConfiguration(t *testing.T) {
	prior, desired, observed := replicationDesired("us-west-1"), replicationDesired("us-west-2"),
		replicationDesired("eusc-de-east-1")
	filters := []ReplicationRepositoryFilter{{Filter: " x ", FilterType: "FUTURE"}}
	observed.ReplicationConfiguration.Rules[0].RepositoryFilters = &filters
	observed.ReplicationConfiguration.Rules[0].Destinations = append(
		observed.ReplicationConfiguration.Rules[0].Destinations,
		ReplicationDestination{Region: "us-west-2", RegistryID: "000000000002"},
		observed.ReplicationConfiguration.Rules[0].Destinations[0])
	observed.ReplicationConfiguration.Rules = append(observed.ReplicationConfiguration.Rules,
		observed.ReplicationConfiguration.Rules[0])
	fake := &replicationFake{reads: []*ecrapi.DescribeRegistryOutput{
		replicationDescription(prior), replicationDescription(observed),
	}}
	out, err := desired.createReplicationConfiguration(t.Context(), fake, replicationIdentity())
	require.NoError(t, err)
	require.Equal(t, observed.ReplicationConfiguration, out.ReplicationConfiguration)
	require.Equal(t, []string{"describe", "put", "describe"}, fake.calls)
	require.Equal(t, []*ecrapi.PutReplicationConfigurationInput{
		desired.preparePut(),
	}, fake.puts)
}

func TestReplicationCreateCompensation(t *testing.T) {
	failure, cleanupFailure := errors.New("read failure"), errors.New("restore failure")
	desired := replicationDesired("us-west-2")
	prior := replicationDesired("eusc-de-east-1")
	filters := []ReplicationRepositoryFilter{{Filter: " a ", FilterType: "FUTURE"}}
	prior.ReplicationConfiguration.Rules[0].RepositoryFilters = &filters
	for _, snapshot := range []*ecrapi.DescribeRegistryOutput{
		nil, {}, {ReplicationConfiguration: &types.ReplicationConfiguration{}},
		replicationDescription(prior),
	} {
		for _, reason := range []string{"read", "empty", "identity", "cancel", "cleanup"} {
			t.Run(fmt.Sprintf("%p/%s", snapshot, reason), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				fake := &replicationFake{reads: []*ecrapi.DescribeRegistryOutput{snapshot, nil},
					readErrors: []error{nil, failure}}
				switch reason {
				case "empty":
					fake.readErrors[1] = nil
				case "identity":
					fake.readErrors[1] = nil
					fake.reads[1] = replicationDescription(desired)
					fake.reads[1].RegistryId = aws.String("999999999999")
				case "cancel":
					fake.cancel = cancel
					fake.readErrors[1] = context.Canceled
				case "cleanup":
					fake.putErrors = []error{nil, cleanupFailure}
				}
				out, err := desired.createReplicationConfiguration(ctx, fake, replicationIdentity())
				require.Nil(t, out)
				require.Error(t, err)
				if reason == "read" || reason == "cleanup" {
					require.ErrorIs(t, err, failure)
				}
				if reason == "cleanup" {
					require.ErrorIs(t, err, cleanupFailure)
				}
				require.Equal(t, []string{"describe", "put", "describe", "put"}, fake.calls)
				restore := emptyReplicationConfiguration()
				if snapshot != nil && snapshot.ReplicationConfiguration != nil &&
					len(snapshot.ReplicationConfiguration.Rules) > 0 {
					restore.ReplicationConfiguration = snapshot.ReplicationConfiguration
				}
				require.Equal(t, []*ecrapi.PutReplicationConfigurationInput{
					desired.preparePut(), restore,
				}, fake.puts)
				require.NotNil(t, fake.cleanupContext)
				require.NoError(t, fake.cleanupError)
				deadline, ok := fake.cleanupContext.Deadline()
				require.True(t, ok)
				require.WithinDuration(t, time.Now().Add(2*time.Minute), deadline, time.Second)
			})
		}
	}
	for _, stage := range []string{"snapshot", "put"} {
		fake := &replicationFake{}
		if stage == "snapshot" {
			fake.readErrors = []error{failure}
		} else {
			fake.putErrors = []error{failure}
		}
		_, err := desired.createReplicationConfiguration(t.Context(), fake, replicationIdentity())
		require.ErrorIs(t, err, failure)
		if stage == "snapshot" {
			require.Equal(t, []string{"describe"}, fake.calls)
		} else {
			require.Equal(t, []string{"describe", "put"}, fake.calls)
		}
	}
}

func TestReplicationReadAbsenceAndErrors(t *testing.T) {
	for _, description := range []*ecrapi.DescribeRegistryOutput{
		nil, {}, {ReplicationConfiguration: &types.ReplicationConfiguration{}},
	} {
		_, err := readReplicationConfiguration(t.Context(),
			&replicationFake{reads: []*ecrapi.DescribeRegistryOutput{description}}, replicationIdentity())
		require.ErrorIs(t, err, runtime.ErrNotFound)
	}
	failure := &types.RepositoryNotFoundException{Message: aws.String("arbitrary service error")}
	_, err := readReplicationConfiguration(t.Context(),
		&replicationFake{readErrors: []error{failure}}, replicationIdentity())
	require.ErrorIs(t, err, failure)
	require.NotErrorIs(t, err, runtime.ErrNotFound)
}

func TestReplicationUpdateObservationAndFallback(t *testing.T) {
	desired, drift := replicationDesired("us-west-2"), replicationDesired("us-west-1")
	output := func(r ReplicationConfigurationResource) *ReplicationConfigurationResourceOutput {
		out := replicationIdentity()
		out.ReplicationConfiguration = r.ReplicationConfiguration
		return out
	}
	for _, observed := range []bool{false, true} {
		prior := replicationPrior{Outputs: output(drift)}
		if observed {
			prior.Outputs = output(desired)
			prior.Observed = output(drift)
		}
		fake := &replicationFake{reads: []*ecrapi.DescribeRegistryOutput{replicationDescription(desired)}}
		out, err := desired.updateReplicationConfiguration(
			t.Context(), fake, replicationIdentity(), prior)
		require.NoError(t, err)
		require.Equal(t, output(desired), out)
		require.Equal(t, []string{"put", "describe"}, fake.calls)
		require.Equal(t, []*ecrapi.PutReplicationConfigurationInput{
			desired.preparePut(),
		}, fake.puts)
	}
	fake := &replicationFake{}
	out, err := desired.updateReplicationConfiguration(t.Context(), fake, replicationIdentity(),
		replicationPrior{Outputs: output(desired)})
	require.NoError(t, err)
	require.Equal(t, output(desired), out)
	require.Empty(t, fake.calls)
	failure := errors.New("update read failed")
	fake = &replicationFake{readErrors: []error{failure}}
	_, err = desired.updateReplicationConfiguration(t.Context(), fake, replicationIdentity(),
		replicationPrior{Outputs: output(drift)})
	require.ErrorIs(t, err, failure)
	require.Equal(t, []string{"put", "describe"}, fake.calls)
	empty := []ReplicationRepositoryFilter{}
	withEmpty := replicationDesired("us-west-2")
	withEmpty.ReplicationConfiguration.Rules[0].RepositoryFilters = &empty
	require.True(t, replicationConfigurationsEqual(desired.ReplicationConfiguration,
		withEmpty.ReplicationConfiguration))
	duplicated := replicationDesired("us-west-2")
	duplicated.ReplicationConfiguration.Rules = append(duplicated.ReplicationConfiguration.Rules,
		duplicated.ReplicationConfiguration.Rules[0])
	require.False(t, replicationConfigurationsEqual(desired.ReplicationConfiguration,
		duplicated.ReplicationConfiguration))
}

func TestReplicationOrderedEquality(t *testing.T) {
	left, right := replicationDesired("us-west-1"), replicationDesired("us-west-2")
	first, second := left.ReplicationConfiguration.Rules[0], right.ReplicationConfiguration.Rules[0]
	left.ReplicationConfiguration.Rules = append(left.ReplicationConfiguration.Rules, second)
	right.ReplicationConfiguration.Rules = append(right.ReplicationConfiguration.Rules, first)
	require.False(t, replicationConfigurationsEqual(left.ReplicationConfiguration,
		right.ReplicationConfiguration))
	left.ReplicationConfiguration.Rules = []ReplicationRule{{Destinations: append(
		first.Destinations, second.Destinations...)}}
	right.ReplicationConfiguration.Rules = []ReplicationRule{{Destinations: append(
		second.Destinations, first.Destinations...)}}
	require.False(t, replicationConfigurationsEqual(left.ReplicationConfiguration,
		right.ReplicationConfiguration))
}

func TestReplicationAccountCredentialMetadata(t *testing.T) {
	account, err := replicationAccountID(t.Context(), nil, aws.Config{
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccountID: "123456789012"}, nil
		}),
	})
	require.NoError(t, err)
	require.Equal(t, "123456789012", account)
}
