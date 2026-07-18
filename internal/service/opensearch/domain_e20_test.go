package opensearch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateDomainCompensatesEveryPostCreateFailure(t *testing.T) {
	tests := []struct {
		name  string
		stage domainCreateFailureStage
	}{
		{name: "malformed create response", stage: domainCreateFailureMalformedResponse},
		{name: "readiness", stage: domainCreateFailureReadiness},
		{name: "Auto-Tune request", stage: domainCreateFailureAutoTuneRequest},
		{name: "Auto-Tune wait", stage: domainCreateFailureAutoTuneWait},
		{name: "Identity Center request", stage: domainCreateFailureIdentityRequest},
		{name: "Identity Center wait", stage: domainCreateFailureIdentityWait},
		{name: "final read", stage: domainCreateFailureFinalRead},
		{name: "output conversion", stage: domainCreateFailureOutputConversion},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalErr := errors.New("post-create failure")
			client, calls := compensatedDomainClient(t, test.stage, originalErr)

			output, err := compensatedDomainResource().create(
				context.Background(),
				client,
				shortDomainOptions(newRecordingDomainClock()),
			)

			require.Error(t, err)
			assert.Nil(t, output)
			if test.stage == domainCreateFailureMalformedResponse ||
				test.stage == domainCreateFailureOutputConversion {
				assert.ErrorContains(t, err, "response")
			} else {
				assert.ErrorIs(t, err, originalErr)
			}
			assert.Equal(t, 1, calls.delete)
			assert.Equal(t, 1, calls.cleanupDescribe)
			assert.Equal(t, 3, calls.cleanupConfigDescribe)
		})
	}
}

func TestCreateDomainCompensationSurvivesCanceledCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	deletes := 0
	configDescribes := 0
	client := &fakeDomainClient{
		describeDomain: func(
			ctx context.Context,
			_ *awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			if deletes > 0 {
				require.NoError(t, context.Cause(ctx))
				return &awssdk.DescribeDomainOutput{
					DomainStatus: completeDomainStatus("example"),
				}, nil
			}
			return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
		},
		createDomain: func(
			context.Context,
			*awssdk.CreateDomainInput,
		) (*awssdk.CreateDomainOutput, error) {
			cancel()
			return validCreateDomainResponse("example"), nil
		},
		deleteDomain: func(
			cleanupCtx context.Context,
			_ *awssdk.DeleteDomainInput,
		) (*awssdk.DeleteDomainOutput, error) {
			deletes++
			require.NoError(t, context.Cause(cleanupCtx))
			deadline, ok := cleanupCtx.Deadline()
			require.True(t, ok)
			remaining := time.Until(deadline)
			assert.Greater(t, remaining, 209*time.Minute)
			assert.LessOrEqual(t, remaining, 210*time.Minute)
			return &awssdk.DeleteDomainOutput{}, nil
		},
		describeDomainConfig: func(
			cleanupCtx context.Context,
			_ *awssdk.DescribeDomainConfigInput,
		) (*awssdk.DescribeDomainConfigOutput, error) {
			require.NoError(t, context.Cause(cleanupCtx))
			configDescribes++
			return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
		},
	}

	output, err := (DomainResource{DomainName: "example"}).create(
		ctx, client, shortDomainOptions(newRecordingDomainClock()),
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, output)
	assert.Equal(t, 1, deletes)
	assert.Equal(t, 3, configDescribes)
}

func TestCreateDomainCompensationTreatsDeleteNotFoundAsSuccess(t *testing.T) {
	originalErr := errors.New("readiness failed")
	deletes := 0
	created := false
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			if !created {
				return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
			}
			return nil, originalErr
		},
		createDomain: func(
			context.Context,
			*awssdk.CreateDomainInput,
		) (*awssdk.CreateDomainOutput, error) {
			created = true
			return validCreateDomainResponse("example"), nil
		},
		deleteDomain: func(
			context.Context,
			*awssdk.DeleteDomainInput,
		) (*awssdk.DeleteDomainOutput, error) {
			deletes++
			return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
		},
	}

	output, err := (DomainResource{DomainName: "example"}).create(
		context.Background(), client, shortDomainOptions(newRecordingDomainClock()),
	)

	assert.Nil(t, output)
	assert.ErrorIs(t, err, originalErr)
	assert.Equal(t, 1, deletes)
}

func TestCreateDomainJoinsCompensationFailure(t *testing.T) {
	originalErr := errors.New("readiness failed")
	cleanupErr := errors.New("cleanup failed")
	created := false
	client := &fakeDomainClient{
		describeDomain: func(
			context.Context,
			*awssdk.DescribeDomainInput,
		) (*awssdk.DescribeDomainOutput, error) {
			if !created {
				return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
			}
			return nil, originalErr
		},
		createDomain: func(
			context.Context,
			*awssdk.CreateDomainInput,
		) (*awssdk.CreateDomainOutput, error) {
			created = true
			return validCreateDomainResponse("example"), nil
		},
		deleteDomain: func(
			context.Context,
			*awssdk.DeleteDomainInput,
		) (*awssdk.DeleteDomainOutput, error) {
			return nil, cleanupErr
		},
	}

	output, err := (DomainResource{DomainName: "example"}).create(
		context.Background(), client, shortDomainOptions(newRecordingDomainClock()),
	)

	assert.Nil(t, output)
	assert.ErrorIs(t, err, originalErr)
	assert.ErrorIs(t, err, cleanupErr)
	assert.ErrorContains(t, err, "compensating delete OpenSearch Domain example")
}

func TestCreateDomainDoesNotCompensateOutsideFailureWindow(t *testing.T) {
	t.Run("CreateDomain failure", func(t *testing.T) {
		createErr := errors.New("create failed")
		deletes := 0
		client := &fakeDomainClient{
			describeDomain: func(
				context.Context,
				*awssdk.DescribeDomainInput,
			) (*awssdk.DescribeDomainOutput, error) {
				return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
			},
			createDomain: func(
				context.Context,
				*awssdk.CreateDomainInput,
			) (*awssdk.CreateDomainOutput, error) {
				return nil, createErr
			},
			deleteDomain: func(
				context.Context,
				*awssdk.DeleteDomainInput,
			) (*awssdk.DeleteDomainOutput, error) {
				deletes++
				return &awssdk.DeleteDomainOutput{}, nil
			},
		}

		output, err := (DomainResource{DomainName: "example"}).create(
			context.Background(), client, shortDomainOptions(newRecordingDomainClock()),
		)

		assert.Nil(t, output)
		assert.ErrorIs(t, err, createErr)
		assert.Zero(t, deletes)
	})

	t.Run("successful create", func(t *testing.T) {
		deletes := 0
		describes := 0
		client := &fakeDomainClient{
			describeDomain: func(
				context.Context,
				*awssdk.DescribeDomainInput,
			) (*awssdk.DescribeDomainOutput, error) {
				describes++
				if describes == 1 {
					return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
				}
				return &awssdk.DescribeDomainOutput{
					DomainStatus: completeDomainStatus("example"),
				}, nil
			},
			createDomain: func(
				context.Context,
				*awssdk.CreateDomainInput,
			) (*awssdk.CreateDomainOutput, error) {
				return validCreateDomainResponse("example"), nil
			},
			deleteDomain: func(
				context.Context,
				*awssdk.DeleteDomainInput,
			) (*awssdk.DeleteDomainOutput, error) {
				deletes++
				return &awssdk.DeleteDomainOutput{}, nil
			},
		}

		output, err := (DomainResource{DomainName: "example"}).create(
			context.Background(), client, shortDomainOptions(newRecordingDomainClock()),
		)

		require.NoError(t, err)
		require.NotNil(t, output)
		assert.Zero(t, deletes)
	})
}

type domainCreateFailureStage int

const (
	domainCreateFailureMalformedResponse domainCreateFailureStage = iota
	domainCreateFailureReadiness
	domainCreateFailureAutoTuneRequest
	domainCreateFailureAutoTuneWait
	domainCreateFailureIdentityRequest
	domainCreateFailureIdentityWait
	domainCreateFailureFinalRead
	domainCreateFailureOutputConversion
)

type domainCompensationCalls struct {
	delete                int
	cleanupDescribe       int
	cleanupConfigDescribe int
}

func compensatedDomainClient(
	t *testing.T,
	stage domainCreateFailureStage,
	originalErr error,
) (*fakeDomainClient, *domainCompensationCalls) {
	t.Helper()
	calls := &domainCompensationCalls{}
	created := false
	describes := 0
	updates := 0
	client := &fakeDomainClient{}
	client.describeDomain = func(
		context.Context,
		*awssdk.DescribeDomainInput,
	) (*awssdk.DescribeDomainOutput, error) {
		if !created {
			return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
		}
		if calls.delete > 0 {
			calls.cleanupDescribe++
			return &awssdk.DescribeDomainOutput{
				DomainStatus: completeDomainStatus("example"),
			}, nil
		}
		describes++
		switch {
		case stage == domainCreateFailureReadiness && describes == 1:
			return nil, originalErr
		case stage == domainCreateFailureAutoTuneWait && describes == 2:
			return nil, originalErr
		case stage == domainCreateFailureIdentityWait && describes == 3:
			return nil, originalErr
		case stage == domainCreateFailureFinalRead && describes == 4:
			return nil, originalErr
		case stage == domainCreateFailureOutputConversion && describes == 4:
			status := completeDomainStatus("example")
			status.DomainId = nil
			return &awssdk.DescribeDomainOutput{DomainStatus: status}, nil
		default:
			return &awssdk.DescribeDomainOutput{
				DomainStatus: completeDomainStatus("example"),
			}, nil
		}
	}
	client.createDomain = func(
		context.Context,
		*awssdk.CreateDomainInput,
	) (*awssdk.CreateDomainOutput, error) {
		created = true
		if stage == domainCreateFailureMalformedResponse {
			return &awssdk.CreateDomainOutput{}, nil
		}
		return validCreateDomainResponse("example"), nil
	}
	client.updateDomainConfig = func(
		context.Context,
		*awssdk.UpdateDomainConfigInput,
	) (*awssdk.UpdateDomainConfigOutput, error) {
		updates++
		if stage == domainCreateFailureAutoTuneRequest && updates == 1 {
			return nil, originalErr
		}
		if stage == domainCreateFailureIdentityRequest && updates == 2 {
			return nil, originalErr
		}
		return &awssdk.UpdateDomainConfigOutput{}, nil
	}
	client.deleteDomain = func(
		context.Context,
		*awssdk.DeleteDomainInput,
	) (*awssdk.DeleteDomainOutput, error) {
		calls.delete++
		return &awssdk.DeleteDomainOutput{}, nil
	}
	client.describeDomainConfig = func(
		context.Context,
		*awssdk.DescribeDomainConfigInput,
	) (*awssdk.DescribeDomainConfigOutput, error) {
		calls.cleanupConfigDescribe++
		return nil, &awstypes.ResourceNotFoundException{Message: aws.String("missing")}
	}
	return client, calls
}

func compensatedDomainResource() DomainResource {
	return DomainResource{
		DomainName: "example",
		AutoTuneOptions: &DomainAutoTuneOptions{
			DesiredState: "ENABLED",
		},
		IdentityCenterOptions: &DomainIdentityCenterOptions{
			EnabledAPIAccess: aws.Bool(true),
			IdentityCenterInstanceARN: aws.String(
				"arn:aws:sso:::instance/ssoins-1234567890123456",
			),
		},
	}
}

func validCreateDomainResponse(name string) *awssdk.CreateDomainOutput {
	return &awssdk.CreateDomainOutput{
		DomainStatus: &awstypes.DomainStatus{DomainName: aws.String(name)},
	}
}
