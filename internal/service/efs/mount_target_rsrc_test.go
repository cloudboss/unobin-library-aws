package efs

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMountTargetMetadata(t *testing.T) {
	r := &MountTargetResource{}
	assert.Equal(t, 1, r.SchemaVersion())
	assert.Equal(t, []string{
		"file-system-id",
		"subnet-id",
		"ip-address",
		"ip-address-type",
		"ipv6-address",
	}, r.ReplaceFields())
}

func TestMountTargetValidateInputs(t *testing.T) {
	tests := []struct {
		name    string
		input   MountTargetResource
		wantErr string
	}{
		{
			name: "valid dual stack",
			input: MountTargetResource{
				FileSystemId:  "fs-0123456789abcdef0",
				SubnetId:      "subnet-0123456789abcdef0",
				IpAddress:     aws.String("10.0.0.10"),
				IpAddressType: aws.String("DUAL_STACK"),
				Ipv6Address:   aws.String("2001:db8::10"),
			},
		},
		{
			name: "invalid IPv4",
			input: MountTargetResource{
				IpAddress: aws.String("2001:db8::10"),
			},
			wantErr: "ip-address must be a valid IPv4 address",
		},
		{
			name: "invalid IPv6",
			input: MountTargetResource{
				Ipv6Address: aws.String("10.0.0.10"),
			},
			wantErr: "ipv6-address must be a valid IPv6 address",
		},
		{
			name: "invalid IP address type",
			input: MountTargetResource{
				IpAddressType: aws.String("IPV5_ONLY"),
			},
			wantErr: "ip-address-type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestMountTargetCreateOmitsDefaultsAndReturnsObservedValues(t *testing.T) {
	fastMountTargetWaits(t)
	efsClient := &fakeMountTargetClient{
		createOutput: &efs.CreateMountTargetOutput{
			MountTargetId: aws.String("fsmt-0123456789abcdef0"),
		},
		describeResults: []mountTargetDescribeResult{
			{output: mountTargetDescribeOutput(efstypes.LifeCycleStateCreating)},
			{output: mountTargetDescribeOutput(efstypes.LifeCycleStateAvailable)},
			{output: mountTargetDescribeOutput(efstypes.LifeCycleStateAvailable)},
		},
		securityGroups: []string{"sg-default"},
	}
	ec2Client := &fakeMountTargetSubnetClient{output: &ec2.DescribeSubnetsOutput{
		Subnets: []ec2types.Subnet{{
			SubnetId:         aws.String("subnet-0123456789abcdef0"),
			AvailabilityZone: aws.String("cn-north-1a"),
		}},
	}}
	clients := &fakeMountTargetClients{
		region: "cn-north-1",
		efs:    efsClient,
		ec2:    ec2Client,
	}
	r := &MountTargetResource{
		FileSystemId: "fs-0123456789abcdef0",
		SubnetId:     "subnet-0123456789abcdef0",
	}

	out, err := r.create(context.Background(), clients)

	require.NoError(t, err)
	require.Len(t, ec2Client.inputs, 1)
	assert.Equal(t, []string{"subnet-0123456789abcdef0"}, ec2Client.inputs[0].SubnetIds)
	require.Len(t, efsClient.createInputs, 1)
	in := efsClient.createInputs[0]
	assert.Equal(t, "fs-0123456789abcdef0", aws.ToString(in.FileSystemId))
	assert.Equal(t, "subnet-0123456789abcdef0", aws.ToString(in.SubnetId))
	assert.Nil(t, in.IpAddress)
	assert.Empty(t, in.IpAddressType)
	assert.Nil(t, in.Ipv6Address)
	assert.Nil(t, in.SecurityGroups)
	assert.Equal(t, &MountTargetResourceOutput{
		MountTargetId:        "fsmt-0123456789abcdef0",
		AvailabilityZoneId:   "cnn1-az1",
		AvailabilityZoneName: "cn-north-1a",
		IpAddress:            "10.0.0.10",
		IpAddressType:        "IPV4_ONLY",
		MountTargetDNSName: "cn-north-1a.fs-0123456789abcdef0.efs.cn-north-1." +
			"amazonaws.com.cn",
		NetworkInterfaceId: "eni-0123456789abcdef0",
		OwnerId:            "123456789012",
		SecurityGroups:     []string{"sg-default"},
	}, out)
}

func TestMountTargetCreateForwardsOptionalInputs(t *testing.T) {
	fastMountTargetWaits(t)
	securityGroups := []string{"sg-one", "sg-two"}
	efsClient := &fakeMountTargetClient{
		createOutput: &efs.CreateMountTargetOutput{
			MountTargetId: aws.String("fsmt-0123456789abcdef0"),
		},
		describeResults: []mountTargetDescribeResult{
			{output: mountTargetDescribeOutput(efstypes.LifeCycleStateAvailable)},
			{output: mountTargetDescribeOutput(efstypes.LifeCycleStateAvailable)},
		},
		securityGroups: securityGroups,
	}
	clients := &fakeMountTargetClients{
		region: "us-east-1",
		efs:    efsClient,
		ec2: &fakeMountTargetSubnetClient{output: &ec2.DescribeSubnetsOutput{
			Subnets: []ec2types.Subnet{{
				SubnetId:         aws.String("subnet-0123456789abcdef0"),
				AvailabilityZone: aws.String("us-east-1a"),
			}},
		}},
	}
	r := &MountTargetResource{
		FileSystemId:   "fs-0123456789abcdef0",
		SubnetId:       "subnet-0123456789abcdef0",
		IpAddress:      aws.String("10.0.0.10"),
		IpAddressType:  aws.String("DUAL_STACK"),
		Ipv6Address:    aws.String("2001:db8::10"),
		SecurityGroups: &securityGroups,
	}

	_, err := r.create(context.Background(), clients)

	require.NoError(t, err)
	require.Len(t, efsClient.createInputs, 1)
	in := efsClient.createInputs[0]
	assert.Equal(t, "10.0.0.10", aws.ToString(in.IpAddress))
	assert.Equal(t, efstypes.IpAddressTypeDualStack, in.IpAddressType)
	assert.Equal(t, "2001:db8::10", aws.ToString(in.Ipv6Address))
	assert.Equal(t, securityGroups, in.SecurityGroups)
}

func TestMountTargetReadMapsAbsence(t *testing.T) {
	tests := []struct {
		name   string
		result mountTargetDescribeResult
	}{
		{
			name:   "typed not found",
			result: mountTargetDescribeResult{err: &efstypes.MountTargetNotFound{}},
		},
		{
			name: "empty result",
			result: mountTargetDescribeResult{
				output: &efs.DescribeMountTargetsOutput{},
			},
		},
		{
			name: "deleted lifecycle",
			result: mountTargetDescribeResult{
				output: mountTargetDescribeOutput(efstypes.LifeCycleStateDeleted),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMountTargetClient{describeResults: []mountTargetDescribeResult{tt.result}}
			r := &MountTargetResource{}
			_, err := r.read(context.Background(), client, "us-east-1",
				"fsmt-0123456789abcdef0")
			assert.ErrorIs(t, err, runtime.ErrNotFound)
		})
	}
}

func TestMountTargetReadPropagatesDescribeError(t *testing.T) {
	want := errors.New("describe failed")
	client := &fakeMountTargetClient{describeResults: []mountTargetDescribeResult{{err: want}}}
	r := &MountTargetResource{}

	_, err := r.read(context.Background(), client, "us-east-1", "fsmt-0123456789abcdef0")

	assert.ErrorIs(t, err, want)
}

func TestMountTargetUpdateSecurityGroups(t *testing.T) {
	priorGroups := []string{"sg-old"}
	newGroups := []string{"sg-new", "sg-shared"}
	emptyGroups := []string{}
	tests := []struct {
		name       string
		groups     *[]string
		wantModify bool
		wantGroups []string
	}{
		{
			name:       "changed explicit groups replace the set",
			groups:     &newGroups,
			wantModify: true,
			wantGroups: newGroups,
		},
		{
			name:   "unchanged explicit groups skip modify",
			groups: &priorGroups,
		},
		{
			name: "omission after explicit groups skips modify",
		},
		{
			name:       "explicit empty list is sent",
			groups:     &emptyGroups,
			wantModify: true,
			wantGroups: emptyGroups,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMountTargetClient{
				describeResults: []mountTargetDescribeResult{{
					output: mountTargetDescribeOutput(efstypes.LifeCycleStateAvailable),
				}},
				securityGroups: slices.Clone(tt.wantGroups),
			}
			clients := &fakeMountTargetClients{
				region: "us-east-1",
				efs:    client,
				ec2:    &fakeMountTargetSubnetClient{},
			}
			r := &MountTargetResource{SecurityGroups: tt.groups}
			prior := runtime.Prior[MountTargetResource, *MountTargetResourceOutput, *awsCfg]{
				Inputs: MountTargetResource{SecurityGroups: &priorGroups},
				Outputs: &MountTargetResourceOutput{
					MountTargetId: "fsmt-0123456789abcdef0",
				},
			}

			_, err := r.update(context.Background(), clients, prior)

			require.NoError(t, err)
			if !tt.wantModify {
				assert.Empty(t, client.modifyInputs)
				return
			}
			require.Len(t, client.modifyInputs, 1)
			assert.Equal(t, "fsmt-0123456789abcdef0",
				aws.ToString(client.modifyInputs[0].MountTargetId))
			assert.Equal(t, tt.wantGroups, client.modifyInputs[0].SecurityGroups)
		})
	}
}

func TestMountTargetDeleteWaitsUntilAbsent(t *testing.T) {
	fastMountTargetWaits(t)
	client := &fakeMountTargetClient{describeResults: []mountTargetDescribeResult{
		{output: mountTargetDescribeOutput(efstypes.LifeCycleStateAvailable)},
		{output: mountTargetDescribeOutput(efstypes.LifeCycleStateDeleting)},
		{err: &efstypes.MountTargetNotFound{}},
	}}
	r := &MountTargetResource{}

	err := r.delete(context.Background(), client, "fsmt-0123456789abcdef0")

	require.NoError(t, err)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, "fsmt-0123456789abcdef0",
		aws.ToString(client.deleteInputs[0].MountTargetId))
	assert.Equal(t, 3, client.describeCalls)
}

func TestMountTargetDeleteAlreadyGone(t *testing.T) {
	client := &fakeMountTargetClient{deleteErr: &efstypes.MountTargetNotFound{}}
	r := &MountTargetResource{}

	err := r.delete(context.Background(), client, "fsmt-0123456789abcdef0")

	require.NoError(t, err)
	assert.Zero(t, client.describeCalls)
}

func TestMountTargetDeletePropagatesError(t *testing.T) {
	want := errors.New("delete failed")
	client := &fakeMountTargetClient{deleteErr: want}
	r := &MountTargetResource{}

	err := r.delete(context.Background(), client, "fsmt-0123456789abcdef0")

	assert.ErrorIs(t, err, want)
}

func TestWaitMountTargetCreatedTerminalState(t *testing.T) {
	fastMountTargetWaits(t)
	client := &fakeMountTargetClient{describeResults: []mountTargetDescribeResult{{
		output: mountTargetDescribeOutput(efstypes.LifeCycleStateDeleting),
	}}}

	_, err := waitMountTargetCreated(
		context.Background(), client, "fsmt-0123456789abcdef0", time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "entered lifecycle state deleting")
}

func TestWaitMountTargetCreatedTimesOut(t *testing.T) {
	fastMountTargetWaits(t)
	client := &fakeMountTargetClient{describeResults: []mountTargetDescribeResult{{
		output: mountTargetDescribeOutput(efstypes.LifeCycleStateCreating),
	}}}

	_, err := waitMountTargetCreated(
		context.Background(), client, "fsmt-0123456789abcdef0", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out waiting")
}

func TestWaitMountTargetDeletedTimesOut(t *testing.T) {
	fastMountTargetWaits(t)
	client := &fakeMountTargetClient{describeResults: []mountTargetDescribeResult{{
		output: mountTargetDescribeOutput(efstypes.LifeCycleStateAvailable),
	}}}

	err := waitMountTargetDeleted(
		context.Background(), client, "fsmt-0123456789abcdef0", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out waiting")
}

func TestMountTargetIPAddressType(t *testing.T) {
	tests := []struct {
		name string
		ipv4 *string
		ipv6 *string
		want string
	}{
		{name: "IPv4 only", ipv4: aws.String("10.0.0.10"), want: "IPV4_ONLY"},
		{name: "IPv6 only", ipv6: aws.String("2001:db8::10"), want: "IPV6_ONLY"},
		{
			name: "dual stack",
			ipv4: aws.String("10.0.0.10"),
			ipv6: aws.String("2001:db8::10"),
			want: "DUAL_STACK",
		},
		{name: "no address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mountTargetIPAddressType(efstypes.MountTargetDescription{
				IpAddress:   tt.ipv4,
				Ipv6Address: tt.ipv6,
			})
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMountTargetCreateSerializesSameFileSystemAndAvailabilityZone(t *testing.T) {
	fastMountTargetWaits(t)
	entered := make(chan int, 2)
	releaseFirst := make(chan struct{})
	efsClient := mountTargetCreateTestClient()
	efsClient.createFn = func(call int) {
		entered <- call
		if call == 1 {
			<-releaseFirst
		}
	}
	clients := mountTargetCreateTestClients(efsClient, "us-east-1a")
	resource := &MountTargetResource{
		FileSystemId: "fs-0123456789abcdef0",
		SubnetId:     "subnet-0123456789abcdef0",
	}
	errs := make(chan error, 2)
	go func() {
		_, err := resource.create(context.Background(), clients)
		errs <- err
	}()
	require.Equal(t, 1, waitForCreateEntry(t, entered))
	go func() {
		_, err := resource.create(context.Background(), clients)
		errs <- err
	}()
	select {
	case call := <-entered:
		t.Fatalf("create call %d entered before the first call completed", call)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	require.Equal(t, 2, waitForCreateEntry(t, entered))
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
}

func TestMountTargetCreateAllowsDistinctAvailabilityZones(t *testing.T) {
	fastMountTargetWaits(t)
	entered := make(chan int, 2)
	release := make(chan struct{})
	efsClient := mountTargetCreateTestClient()
	efsClient.createFn = func(call int) {
		entered <- call
		<-release
	}
	resource := &MountTargetResource{
		FileSystemId: "fs-0123456789abcdef0",
		SubnetId:     "subnet-0123456789abcdef0",
	}
	errs := make(chan error, 2)
	for _, availabilityZone := range []string{"us-east-1a", "us-east-1b"} {
		clients := mountTargetCreateTestClients(efsClient, availabilityZone)
		go func() {
			_, err := resource.create(context.Background(), clients)
			errs <- err
		}()
	}
	first := waitForCreateEntry(t, entered)
	second := waitForCreateEntry(t, entered)
	assert.ElementsMatch(t, []int{1, 2}, []int{first, second})
	close(release)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
}

func TestMountTargetAvailabilityZoneErrors(t *testing.T) {
	want := errors.New("describe failed")
	tests := []struct {
		name    string
		client  *fakeMountTargetSubnetClient
		wantErr string
	}{
		{
			name:    "API error",
			client:  &fakeMountTargetSubnetClient{err: want},
			wantErr: "describe failed",
		},
		{
			name:    "empty result",
			client:  &fakeMountTargetSubnetClient{output: &ec2.DescribeSubnetsOutput{}},
			wantErr: "was not found",
		},
		{
			name: "missing availability zone",
			client: &fakeMountTargetSubnetClient{output: &ec2.DescribeSubnetsOutput{
				Subnets: []ec2types.Subnet{{
					SubnetId: aws.String("subnet-0123456789abcdef0"),
				}},
			}},
			wantErr: "returned no availability zone",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mountTargetAvailabilityZone(
				context.Background(), tt.client, "subnet-0123456789abcdef0")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestMountTargetReadSecurityGroupErrors(t *testing.T) {
	tests := []struct {
		name         string
		securityErr  error
		wantNotFound bool
	}{
		{
			name:         "mount target disappeared",
			securityErr:  &efstypes.MountTargetNotFound{},
			wantNotFound: true,
		},
		{
			name:        "other error",
			securityErr: errors.New("security group read failed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMountTargetClient{
				describeResults: []mountTargetDescribeResult{{
					output: mountTargetDescribeOutput(efstypes.LifeCycleStateAvailable),
				}},
				securityErr: tt.securityErr,
			}
			r := &MountTargetResource{}
			_, err := r.read(context.Background(), client, "us-east-1",
				"fsmt-0123456789abcdef0")
			require.Error(t, err)
			if tt.wantNotFound {
				assert.ErrorIs(t, err, runtime.ErrNotFound)
				return
			}
			assert.ErrorIs(t, err, tt.securityErr)
		})
	}
}

func TestWaitMountTargetDeletedRejectsUnexpectedState(t *testing.T) {
	fastMountTargetWaits(t)
	client := &fakeMountTargetClient{describeResults: []mountTargetDescribeResult{{
		output: mountTargetDescribeOutput(efstypes.LifeCycleStateCreating),
	}}}

	err := waitMountTargetDeleted(
		context.Background(), client, "fsmt-0123456789abcdef0", time.Second)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "entered lifecycle state creating")
}

func TestMountTargetWaitDelayHonorsCanceledContext(t *testing.T) {
	prior := mountTargetInitialDelay
	mountTargetInitialDelay = time.Hour
	t.Cleanup(func() { mountTargetInitialDelay = prior })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := mountTargetWaitDelay(ctx)

	assert.ErrorIs(t, err, context.Canceled)
}

func mountTargetCreateTestClient() *fakeMountTargetClient {
	return &fakeMountTargetClient{
		createOutput: &efs.CreateMountTargetOutput{
			MountTargetId: aws.String("fsmt-0123456789abcdef0"),
		},
		describeResults: []mountTargetDescribeResult{{
			output: mountTargetDescribeOutput(efstypes.LifeCycleStateAvailable),
		}},
		securityGroups: []string{"sg-default"},
	}
}

func mountTargetCreateTestClients(
	client *fakeMountTargetClient,
	availabilityZone string,
) *fakeMountTargetClients {
	return &fakeMountTargetClients{
		region: "us-east-1",
		efs:    client,
		ec2: &fakeMountTargetSubnetClient{output: &ec2.DescribeSubnetsOutput{
			Subnets: []ec2types.Subnet{{
				SubnetId:         aws.String("subnet-0123456789abcdef0"),
				AvailabilityZone: aws.String(availabilityZone),
			}},
		}},
	}
}

func waitForCreateEntry(t *testing.T, entered <-chan int) int {
	t.Helper()
	select {
	case call := <-entered:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for create call")
		return 0
	}
}

func fastMountTargetWaits(t *testing.T) {
	t.Helper()
	priorDelay := mountTargetInitialDelay
	priorInterval := mountTargetPollInterval
	mountTargetInitialDelay = 0
	mountTargetPollInterval = 0
	t.Cleanup(func() {
		mountTargetInitialDelay = priorDelay
		mountTargetPollInterval = priorInterval
	})
}

func mountTargetDescribeOutput(state efstypes.LifeCycleState) *efs.DescribeMountTargetsOutput {
	return &efs.DescribeMountTargetsOutput{MountTargets: []efstypes.MountTargetDescription{{
		AvailabilityZoneId:   aws.String("cnn1-az1"),
		AvailabilityZoneName: aws.String("cn-north-1a"),
		FileSystemId:         aws.String("fs-0123456789abcdef0"),
		IpAddress:            aws.String("10.0.0.10"),
		LifeCycleState:       state,
		MountTargetId:        aws.String("fsmt-0123456789abcdef0"),
		NetworkInterfaceId:   aws.String("eni-0123456789abcdef0"),
		OwnerId:              aws.String("123456789012"),
		SubnetId:             aws.String("subnet-0123456789abcdef0"),
	}}}
}

type mountTargetDescribeResult struct {
	output *efs.DescribeMountTargetsOutput
	err    error
}

type fakeMountTargetClients struct {
	region string
	efs    *fakeMountTargetClient
	ec2    *fakeMountTargetSubnetClient
}

func (c *fakeMountTargetClients) EFS() mountTargetClient { return c.efs }

func (c *fakeMountTargetClients) EC2() mountTargetSubnetClient { return c.ec2 }

func (c *fakeMountTargetClients) Region() string { return c.region }

type fakeMountTargetSubnetClient struct {
	mu     sync.Mutex
	inputs []*ec2.DescribeSubnetsInput
	output *ec2.DescribeSubnetsOutput
	err    error
}

func (c *fakeMountTargetSubnetClient) DescribeSubnets(
	_ context.Context,
	in *ec2.DescribeSubnetsInput,
	_ ...func(*ec2.Options),
) (*ec2.DescribeSubnetsOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copyInput := *in
	copyInput.SubnetIds = slices.Clone(in.SubnetIds)
	c.inputs = append(c.inputs, &copyInput)
	return c.output, c.err
}

type fakeMountTargetClient struct {
	mu sync.Mutex

	createInputs    []*efs.CreateMountTargetInput
	createOutput    *efs.CreateMountTargetOutput
	createErr       error
	createFn        func(int)
	describeResults []mountTargetDescribeResult
	describeCalls   int
	securityGroups  []string
	securityErr     error
	modifyInputs    []*efs.ModifyMountTargetSecurityGroupsInput
	modifyErr       error
	deleteInputs    []*efs.DeleteMountTargetInput
	deleteErr       error
}

func (c *fakeMountTargetClient) CreateMountTarget(
	_ context.Context,
	in *efs.CreateMountTargetInput,
	_ ...func(*efs.Options),
) (*efs.CreateMountTargetOutput, error) {
	c.mu.Lock()
	copyInput := *in
	copyInput.SecurityGroups = slices.Clone(in.SecurityGroups)
	c.createInputs = append(c.createInputs, &copyInput)
	call := len(c.createInputs)
	fn := c.createFn
	output := c.createOutput
	err := c.createErr
	c.mu.Unlock()
	if fn != nil {
		fn(call)
	}
	return output, err
}

func (c *fakeMountTargetClient) DescribeMountTargets(
	_ context.Context,
	_ *efs.DescribeMountTargetsInput,
	_ ...func(*efs.Options),
) (*efs.DescribeMountTargetsOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.describeCalls
	c.describeCalls++
	if index >= len(c.describeResults) {
		index = len(c.describeResults) - 1
	}
	result := c.describeResults[index]
	return result.output, result.err
}

func (c *fakeMountTargetClient) DescribeMountTargetSecurityGroups(
	context.Context,
	*efs.DescribeMountTargetSecurityGroupsInput,
	...func(*efs.Options),
) (*efs.DescribeMountTargetSecurityGroupsOutput, error) {
	return &efs.DescribeMountTargetSecurityGroupsOutput{
		SecurityGroups: slices.Clone(c.securityGroups),
	}, c.securityErr
}

func (c *fakeMountTargetClient) ModifyMountTargetSecurityGroups(
	_ context.Context,
	in *efs.ModifyMountTargetSecurityGroupsInput,
	_ ...func(*efs.Options),
) (*efs.ModifyMountTargetSecurityGroupsOutput, error) {
	copyInput := *in
	copyInput.SecurityGroups = slices.Clone(in.SecurityGroups)
	c.modifyInputs = append(c.modifyInputs, &copyInput)
	return &efs.ModifyMountTargetSecurityGroupsOutput{}, c.modifyErr
}

func (c *fakeMountTargetClient) DeleteMountTarget(
	_ context.Context,
	in *efs.DeleteMountTargetInput,
	_ ...func(*efs.Options),
) (*efs.DeleteMountTargetOutput, error) {
	copyInput := *in
	c.deleteInputs = append(c.deleteInputs, &copyInput)
	return &efs.DeleteMountTargetOutput{}, c.deleteErr
}

var _ mountTargetClientProvider = (*fakeMountTargetClients)(nil)
var _ mountTargetSubnetClient = (*fakeMountTargetSubnetClient)(nil)
var _ mountTargetClient = (*fakeMountTargetClient)(nil)

var _ = time.Second
