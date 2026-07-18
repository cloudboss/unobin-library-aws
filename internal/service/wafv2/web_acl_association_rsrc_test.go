package wafv2

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	associationResourceARN = "arn:aws:cognito-idp:us-east-1:123456789012:userpool/us-east-1_example"
	associationWebACLARN   = "arn:aws:wafv2:us-east-1:123456789012:regional/webacl/example/acl-id"
)

func TestWebACLAssociationCreateAssociatesThenConfirms(t *testing.T) {
	resource := validWebACLAssociationResource()
	client := &fakeWebACLAssociationClient{
		associateFn: func(input *awssvc.AssociateWebACLInput) error {
			assert.Equal(t, resource.ResourceARN, aws.ToString(input.ResourceArn))
			assert.Equal(t, resource.WebACLARN, aws.ToString(input.WebACLArn))
			return nil
		},
		getFn: func(input *awssvc.GetWebACLForResourceInput) (
			*awssvc.GetWebACLForResourceOutput,
			error,
		) {
			assert.Equal(t, resource.ResourceARN, aws.ToString(input.ResourceArn))
			return associatedWebACLOutput(resource.WebACLARN), nil
		},
	}

	out, err := resource.create(context.Background(), client, &instantAssociationClock{})

	require.NoError(t, err)
	assert.Equal(t, &WebACLAssociationResourceOutput{
		ResourceARN: resource.ResourceARN,
		WebACLARN:   resource.WebACLARN,
	}, out)
	assert.Equal(t, []string{"associate", "get"}, client.calls)
}

func TestWebACLAssociationCreateRetriesUnavailableThenConfirms(t *testing.T) {
	resource := validWebACLAssociationResource()
	attempts := 0
	client := &fakeWebACLAssociationClient{
		associateFn: func(*awssvc.AssociateWebACLInput) error {
			attempts++
			if attempts < 3 {
				return &awstypes.WAFUnavailableEntityException{}
			}
			return nil
		},
		getFn: func(*awssvc.GetWebACLForResourceInput) (
			*awssvc.GetWebACLForResourceOutput,
			error,
		) {
			return associatedWebACLOutput(resource.WebACLARN), nil
		},
	}
	clock := &instantAssociationClock{}

	_, err := resource.create(context.Background(), client, clock)

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, clock.sleeps)
	assert.Equal(t, []string{"associate", "associate", "associate", "get"}, client.calls)
}

func TestWebACLAssociationCreatePassesUnsupportedARNToWAF(t *testing.T) {
	resource := &WebACLAssociationResource{
		ResourceARN: "arn:aws:example:us-east-1:123456789012:thing/id",
		WebACLARN:   associationWebACLARN,
	}
	sentinel := errors.New("unsupported resource")
	client := &fakeWebACLAssociationClient{
		associateFn: func(input *awssvc.AssociateWebACLInput) error {
			assert.Equal(t, resource.ResourceARN, aws.ToString(input.ResourceArn))
			return sentinel
		},
	}

	_, err := resource.create(context.Background(), client, &instantAssociationClock{})

	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, []string{"associate"}, client.calls)
}

func TestWebACLAssociationReadUsesCompletePriorIdentity(t *testing.T) {
	resource := validWebACLAssociationResource()
	prior := &WebACLAssociationResourceOutput{
		ResourceARN: "arn:aws:example:us-west-2:123456789012:thing/old",
		WebACLARN:   "arn:aws:wafv2:us-west-2:123456789012:regional/webacl/old/id",
	}
	client := &fakeWebACLAssociationClient{
		getFn: func(input *awssvc.GetWebACLForResourceInput) (
			*awssvc.GetWebACLForResourceOutput,
			error,
		) {
			assert.Equal(t, prior.ResourceARN, aws.ToString(input.ResourceArn))
			return associatedWebACLOutput(prior.WebACLARN), nil
		},
	}

	out, err := resource.read(context.Background(), client, prior)

	require.NoError(t, err)
	assert.Equal(t, prior, out)
	assert.Equal(t, []string{"get"}, client.calls)
}

func TestWebACLAssociationReadFallsBackFromIncompletePriorIdentity(t *testing.T) {
	resource := validWebACLAssociationResource()
	client := &fakeWebACLAssociationClient{
		getFn: func(input *awssvc.GetWebACLForResourceInput) (
			*awssvc.GetWebACLForResourceOutput,
			error,
		) {
			assert.Equal(t, resource.ResourceARN, aws.ToString(input.ResourceArn))
			return associatedWebACLOutput(resource.WebACLARN), nil
		},
	}

	out, err := resource.read(context.Background(), client,
		&WebACLAssociationResourceOutput{ResourceARN: "incomplete"})

	require.NoError(t, err)
	assert.Equal(t, &WebACLAssociationResourceOutput{
		ResourceARN: resource.ResourceARN,
		WebACLARN:   resource.WebACLARN,
	}, out)
}

func TestWebACLAssociationReadAbsenceAndErrors(t *testing.T) {
	unavailable := &awstypes.WAFUnavailableEntityException{}
	sentinel := errors.New("denied")
	tests := map[string]struct {
		output   *awssvc.GetWebACLForResourceOutput
		err      error
		wantErr  error
		contains string
	}{
		"typed nonexistent": {
			err:     fmt.Errorf("wrapped: %w", &awstypes.WAFNonexistentItemException{}),
			wantErr: runtime.ErrNotFound,
		},
		"nil response": {
			wantErr: runtime.ErrNotFound,
		},
		"nil web ACL": {
			output:  &awssvc.GetWebACLForResourceOutput{},
			wantErr: runtime.ErrNotFound,
		},
		"empty web ACL ARN": {
			output:   &awssvc.GetWebACLForResourceOutput{WebACL: &awstypes.WebACL{}},
			contains: "response has no web ACL ARN",
		},
		"different web ACL ARN": {
			output: associatedWebACLOutput(
				"arn:aws:wafv2:us-east-1:123456789012:regional/webacl/other/id",
			),
			wantErr: runtime.ErrNotFound,
		},
		"unavailable is not retried": {
			err:     unavailable,
			wantErr: unavailable,
		},
		"unrelated error": {
			err:     sentinel,
			wantErr: sentinel,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeWebACLAssociationClient{
				getFn: func(*awssvc.GetWebACLForResourceInput) (
					*awssvc.GetWebACLForResourceOutput,
					error,
				) {
					return tt.output, tt.err
				},
			}

			_, err := validWebACLAssociationResource().read(
				context.Background(), client, nil,
			)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.ErrorContains(t, err, tt.contains)
				assert.False(t, errors.Is(err, runtime.ErrNotFound))
			}
			assert.Equal(t, []string{"get"}, client.calls)
		})
	}
}

func TestWebACLAssociationUpdateReturnsPriorIdentityWithoutAWSCalls(t *testing.T) {
	priorOutput := &WebACLAssociationResourceOutput{
		ResourceARN: "arn:aws:example:us-east-1:123456789012:thing/old",
		WebACLARN:   "arn:aws:wafv2:us-east-1:123456789012:regional/webacl/old/id",
	}

	out, err := validWebACLAssociationResource().Update(
		context.Background(), nil,
		runtime.Prior[WebACLAssociationResource, *WebACLAssociationResourceOutput]{
			Outputs: priorOutput,
		},
	)

	require.NoError(t, err)
	assert.Same(t, priorOutput, out)
}

func TestWebACLAssociationDeleteUsesPriorResourceWithoutReading(t *testing.T) {
	resource := validWebACLAssociationResource()
	prior := &WebACLAssociationResourceOutput{
		ResourceARN: "arn:aws:example:us-west-2:123456789012:thing/old",
		WebACLARN:   "arn:aws:wafv2:us-west-2:123456789012:regional/webacl/old/id",
	}
	client := &fakeWebACLAssociationClient{
		disassociateFn: func(input *awssvc.DisassociateWebACLInput) error {
			assert.Equal(t, prior.ResourceARN, aws.ToString(input.ResourceArn))
			return nil
		},
	}

	err := resource.delete(context.Background(), client, prior)

	require.NoError(t, err)
	assert.Equal(t, []string{"disassociate"}, client.calls)
}

func TestWebACLAssociationDeleteFallsBackFromIncompletePriorIdentity(t *testing.T) {
	resource := validWebACLAssociationResource()
	client := &fakeWebACLAssociationClient{
		disassociateFn: func(input *awssvc.DisassociateWebACLInput) error {
			assert.Equal(t, resource.ResourceARN, aws.ToString(input.ResourceArn))
			return nil
		},
	}

	err := resource.delete(context.Background(), client,
		&WebACLAssociationResourceOutput{ResourceARN: "incomplete"})

	require.NoError(t, err)
}

func TestWebACLAssociationDeleteErrorHandling(t *testing.T) {
	notFound := &awstypes.WAFNonexistentItemException{}
	unavailable := &awstypes.WAFUnavailableEntityException{}
	tests := map[string]struct {
		err     error
		wantErr error
	}{
		"typed nonexistent is success": {err: fmt.Errorf("wrapped: %w", notFound)},
		"unavailable is not retried":   {err: unavailable, wantErr: unavailable},
		"unrelated error propagates": {
			err:     errors.New("denied"),
			wantErr: errors.New("denied"),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := &fakeWebACLAssociationClient{
				disassociateFn: func(*awssvc.DisassociateWebACLInput) error {
					return tt.err
				},
			}

			err := validWebACLAssociationResource().delete(
				context.Background(), client, nil,
			)

			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				assert.EqualError(t, err, "disassociate web ACL: "+tt.wantErr.Error())
			}
			assert.Equal(t, []string{"disassociate"}, client.calls)
		})
	}
}

func TestWebACLAssociationValidateInputs(t *testing.T) {
	valid := []string{
		associationResourceARN,
		"arn:aws-us-gov:wafv2:us-gov-west-1:aws:regional/webacl/example/id",
		"arn:aws:example:::thing/id",
		"arn:aws:cloudfront::123456789012:distribution/EXAMPLE",
	}
	invalid := []string{
		"",
		"aws:wafv2:us-east-1:123456789012:regional/webacl/example/id",
		"arn::wafv2:us-east-1:123456789012:regional/webacl/example/id",
		"arn:not-aws:wafv2:us-east-1:123456789012:regional/webacl/example/id",
		"arn:aws:wafv2:useast1:123456789012:regional/webacl/example/id",
		"arn:aws:wafv2:us-east-1:123:regional/webacl/example/id",
		"arn:aws:wafv2:us-east-1:123456789012:",
	}
	for _, value := range valid {
		t.Run("valid "+value, func(t *testing.T) {
			resource := &WebACLAssociationResource{ResourceARN: value, WebACLARN: value}
			require.NoError(t, resource.ValidateInputs(context.Background(), nil))
		})
	}
	for _, value := range invalid {
		t.Run("invalid resource "+value, func(t *testing.T) {
			resource := validWebACLAssociationResource()
			resource.ResourceARN = value
			assert.EqualError(t, resource.ValidateInputs(context.Background(), nil),
				"resource-arn must be a valid ARN")
		})
		t.Run("invalid web ACL "+value, func(t *testing.T) {
			resource := validWebACLAssociationResource()
			resource.WebACLARN = value
			assert.EqualError(t, resource.ValidateInputs(context.Background(), nil),
				"web-acl-arn must be a valid ARN")
		})
	}
}

func validWebACLAssociationResource() *WebACLAssociationResource {
	return &WebACLAssociationResource{
		ResourceARN: associationResourceARN,
		WebACLARN:   associationWebACLARN,
	}
}

func associatedWebACLOutput(arn string) *awssvc.GetWebACLForResourceOutput {
	return &awssvc.GetWebACLForResourceOutput{
		WebACL: &awstypes.WebACL{ARN: aws.String(arn)},
	}
}

type fakeWebACLAssociationClient struct {
	calls          []string
	associateFn    func(*awssvc.AssociateWebACLInput) error
	disassociateFn func(*awssvc.DisassociateWebACLInput) error
	getFn          func(*awssvc.GetWebACLForResourceInput) (
		*awssvc.GetWebACLForResourceOutput,
		error,
	)
}

func (c *fakeWebACLAssociationClient) AssociateWebACL(
	_ context.Context,
	input *awssvc.AssociateWebACLInput,
	_ ...func(*awssvc.Options),
) (*awssvc.AssociateWebACLOutput, error) {
	c.calls = append(c.calls, "associate")
	if c.associateFn == nil {
		return nil, errors.New("unexpected AssociateWebACL call")
	}
	return &awssvc.AssociateWebACLOutput{}, c.associateFn(input)
}

func (c *fakeWebACLAssociationClient) DisassociateWebACL(
	_ context.Context,
	input *awssvc.DisassociateWebACLInput,
	_ ...func(*awssvc.Options),
) (*awssvc.DisassociateWebACLOutput, error) {
	c.calls = append(c.calls, "disassociate")
	if c.disassociateFn == nil {
		return nil, errors.New("unexpected DisassociateWebACL call")
	}
	return &awssvc.DisassociateWebACLOutput{}, c.disassociateFn(input)
}

func (c *fakeWebACLAssociationClient) GetWebACLForResource(
	_ context.Context,
	input *awssvc.GetWebACLForResourceInput,
	_ ...func(*awssvc.Options),
) (*awssvc.GetWebACLForResourceOutput, error) {
	c.calls = append(c.calls, "get")
	if c.getFn == nil {
		return nil, errors.New("unexpected GetWebACLForResource call")
	}
	return c.getFn(input)
}

type instantAssociationClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *instantAssociationClock) Now() time.Time {
	return c.now
}

func (c *instantAssociationClock) Sleep(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	c.sleeps = append(c.sleeps, duration)
	c.now = c.now.Add(duration)
	return nil
}
