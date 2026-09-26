package opensearch

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdk "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r DomainResource) create(
	ctx context.Context,
	client domainClient,
	options domainOperationOptions,
) (*DomainResourceOutput, error) {
	options = options.withDefaults()
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	input, err := r.createInput()
	if err != nil {
		return nil, err
	}
	followUps, err := r.createFollowUpInputs()
	if err != nil {
		return nil, err
	}
	found, err := client.DescribeDomain(ctx, &awssdk.DescribeDomainInput{
		DomainName: aws.String(r.DomainName),
	})
	if err == nil && found != nil && found.DomainStatus != nil {
		return nil, fmt.Errorf("create domain %s: domain already exists", r.DomainName)
	}
	var createOutput *awssdk.CreateDomainOutput
	if err := retryDomainOperation(ctx, options.clock, options.propagationTimeout,
		func(ctx context.Context) error {
			output, err := client.CreateDomain(ctx, input)
			if err == nil {
				createOutput = output
			}
			return err
		}); err != nil {
		return nil, fmt.Errorf("create domain %s: %w", r.DomainName, err)
	}
	if createOutput == nil || createOutput.DomainStatus == nil {
		return r.compensateCreate(ctx, client, options, fmt.Errorf(
			"create domain %s: response has no domain status",
			r.DomainName,
		))
	}
	_, err = waitDomainCreated(
		ctx, client, r.DomainName, options.clock, options.createTimeout,
	)
	if err != nil {
		return r.compensateCreate(ctx, client, options, err)
	}
	for _, followUp := range followUps {
		_, err = applyDomainFollowUp(ctx, client, followUp, options)
		if err != nil {
			return r.compensateCreate(ctx, client, options, err)
		}
	}
	output, err := r.read(ctx, client, &DomainResourceOutput{DomainName: r.DomainName})
	if err != nil {
		return r.compensateCreate(ctx, client, options, err)
	}
	return output, nil
}

func (r DomainResource) compensateCreate(
	ctx context.Context,
	client domainClient,
	options domainOperationOptions,
	originalErr error,
) (*DomainResourceOutput, error) {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		domainCreateCleanupTimeout,
	)
	defer cancel()
	cleanupErr := r.delete(
		cleanupCtx,
		client,
		&DomainResourceOutput{DomainName: r.DomainName},
		options,
	)
	if cleanupErr == nil {
		return nil, originalErr
	}
	return nil, errors.Join(originalErr, fmt.Errorf(
		"compensating delete OpenSearch Domain %s: %w",
		r.DomainName,
		cleanupErr,
	))
}

func (r DomainResource) read(
	ctx context.Context,
	client domainClient,
	prior *DomainResourceOutput,
) (*DomainResourceOutput, error) {
	name := r.DomainName
	if prior != nil && prior.DomainName != "" {
		name = prior.DomainName
	}
	status, err := describeDomainStatus(ctx, client, name)
	if err != nil {
		return nil, err
	}
	return domainOutput(status)
}

func applyDomainFollowUp(
	ctx context.Context,
	client domainClient,
	input *awssdk.UpdateDomainConfigInput,
	options domainOperationOptions,
) (*awstypes.DomainStatus, error) {
	name := aws.ToString(input.DomainName)
	if err := retryDomainOperation(ctx, options.clock, options.propagationTimeout,
		func(ctx context.Context) error {
			_, err := client.UpdateDomainConfig(ctx, input)
			return err
		}); err != nil {
		return nil, fmt.Errorf("update domain %s configuration: %w", name, err)
	}
	return waitDomainUpdated(ctx, client, name, options.clock, options.createTimeout)
}

func (r DomainResource) update(
	ctx context.Context,
	client domainClient,
	prior runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg],
	options domainOperationOptions,
) (*DomainResourceOutput, error) {
	options = options.withDefaults()
	effectiveVersion := effectiveDomainEngineVersion(
		r.EngineVersion,
		prior.Observed,
		prior.Outputs,
	)
	if err := r.validateInputsForEngine(ctx, nil, effectiveVersion); err != nil {
		return nil, err
	}
	name := domainIdentity(r.DomainName, prior.Outputs)
	input, needed, err := r.updateConfigInputForEngine(
		prior.Inputs,
		name,
		effectiveVersion,
	)
	if err != nil {
		return nil, err
	}
	if err := r.preflightUpdateForEngine(
		ctx,
		client,
		name,
		prior.Inputs,
		stringValue(effectiveVersion),
	); err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		arn := domainARN(prior)
		if arn == "" {
			return nil, fmt.Errorf("update domain %s tags: prior ARN is missing", name)
		}
		if err := syncDomainTags(ctx, client, arn, stringMapValue(r.Tags)); err != nil {
			return nil, err
		}
	}
	if needed {
		if err := retryDomainOperation(ctx, options.clock, options.propagationTimeout,
			func(ctx context.Context) error {
				_, err := client.UpdateDomainConfig(ctx, input)
				return err
			}); err != nil {
			return nil, fmt.Errorf("update domain %s configuration: %w", name, err)
		}
		if _, err := waitDomainUpdated(
			ctx, client, name, options.clock, options.updateTimeout,
		); err != nil {
			return nil, err
		}
	}
	if r.EngineVersion != nil && runtime.Changed(prior.Inputs.EngineVersion, r.EngineVersion) {
		_, err := client.UpgradeDomain(ctx, &awssdk.UpgradeDomainInput{
			DomainName: aws.String(name), TargetVersion: copyString(r.EngineVersion),
		})
		if err != nil {
			return nil, fmt.Errorf("upgrade domain %s: %w", name, err)
		}
		if err := waitDomainUpgraded(
			ctx, client, name, options.clock, options.updateTimeout,
		); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, &DomainResourceOutput{DomainName: name})
}

func (r DomainResource) createFollowUpInputs() ([]*awssdk.UpdateDomainConfigInput, error) {
	result := make([]*awssdk.UpdateDomainConfigInput, 0, 2)
	if r.AutoTuneOptions != nil {
		autoTune, err := domainAutoTuneOptions(r.AutoTuneOptions)
		if err != nil {
			return nil, err
		}
		result = append(result, &awssdk.UpdateDomainConfigInput{
			DomainName: aws.String(r.DomainName), AutoTuneOptions: autoTune,
		})
	}
	if r.IdentityCenterOptions != nil {
		result = append(result, &awssdk.UpdateDomainConfigInput{
			DomainName:            aws.String(r.DomainName),
			IdentityCenterOptions: domainIdentityCenterOptions(r.IdentityCenterOptions),
		})
	}
	return result, nil
}

func domainIdentity(desired string, prior *DomainResourceOutput) string {
	if prior != nil && prior.DomainName != "" {
		return prior.DomainName
	}
	return desired
}

func domainARN(
	prior runtime.Prior[DomainResource, *DomainResourceOutput, *awsCfg],
) string {
	if prior.Outputs != nil && prior.Outputs.ARN != "" {
		return prior.Outputs.ARN
	}
	if prior.Observed != nil {
		return prior.Observed.ARN
	}
	return ""
}

func stringMapValue(value *map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	return *value
}

func (r DomainResource) delete(
	ctx context.Context,
	client domainClient,
	prior *DomainResourceOutput,
	options domainOperationOptions,
) error {
	options = options.withDefaults()
	name := domainIdentity(r.DomainName, prior)
	_, err := client.DeleteDomain(ctx, &awssdk.DeleteDomainInput{
		DomainName: aws.String(name),
	})
	if isDomainNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete domain %s: %w", name, err)
	}
	if err := waitDomainDeleted(
		ctx, client, name, options.clock, options.deleteTimeout,
	); err != nil {
		return err
	}
	return waitDomainConfigDeleted(
		ctx, client, name, options.clock, options.deleteTimeout,
	)
}
