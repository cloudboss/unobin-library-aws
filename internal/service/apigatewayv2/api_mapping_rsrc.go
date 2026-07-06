package apigatewayv2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// ApiMappingResource manages the mapping from a custom domain name path to one stage
// of an API Gateway v2 API. API Gateway addresses a mapping by its generated
// mapping id plus the domain name, so both values are recorded for later reads
// and deletes. Moving a mapping to another API or domain creates a new mapping;
// the mapping key and stage update in place.
type ApiMappingResource struct {
	ApiId         string  `ub:"api-id"`
	DomainName    string  `ub:"domain-name"`
	Stage         string  `ub:"stage"`
	ApiMappingKey *string `ub:"api-mapping-key"`
}

// ApiMappingResourceOutput holds the mapping identity. DomainName is stored with the
// generated id because GetApiMapping and DeleteApiMapping require both, and a
// replacement must delete the old mapping from the old domain.
type ApiMappingResourceOutput struct {
	ApiMappingId string `ub:"api-mapping-id"`
	DomainName   string `ub:"domain-name"`
}

func (r *ApiMappingResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs API Gateway cannot change in place.
func (r *ApiMappingResource) ReplaceFields() []string {
	return []string{"api-id", "domain-name"}
}

func (r *ApiMappingResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*ApiMappingResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var resp *apigatewayv2.CreateApiMappingOutput
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.CreateApiMapping(ctx, r.createInput())
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create api mapping: %w", err)
	}
	if resp == nil || aws.ToString(resp.ApiMappingId) == "" {
		return nil, fmt.Errorf("create api mapping: empty response")
	}
	out, err := r.read(ctx, client, r.DomainName, aws.ToString(resp.ApiMappingId))
	if err != nil {
		return nil, fmt.Errorf("read api mapping after create: %w", err)
	}
	return out, nil
}

func (r *ApiMappingResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *ApiMappingResourceOutput,
) (*ApiMappingResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.DomainName, prior.ApiMappingId)
}

func (r *ApiMappingResource) read(
	ctx context.Context, client *apigatewayv2.Client, domainName, apiMappingID string,
) (*ApiMappingResourceOutput, error) {
	var resp *apigatewayv2.GetApiMappingOutput
	err := withConflictRetry(ctx, func(ctx context.Context) error {
		var err error
		resp, err = client.GetApiMapping(ctx, &apigatewayv2.GetApiMappingInput{
			ApiMappingId: aws.String(apiMappingID),
			DomainName:   aws.String(domainName),
		})
		return err
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get api mapping %s: %w", apiMappingID, err)
	}
	if resp == nil {
		return nil, runtime.ErrNotFound
	}
	return &ApiMappingResourceOutput{
		ApiMappingId: aws.ToString(resp.ApiMappingId),
		DomainName:   domainName,
	}, nil
}

func (r *ApiMappingResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[ApiMappingResource, *ApiMappingResourceOutput],
) (*ApiMappingResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if in, changed := r.updateInput(prior); changed {
		err := withConflictRetry(ctx, func(ctx context.Context) error {
			_, err := client.UpdateApiMapping(ctx, in)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("update api mapping: %w", err)
		}
	}
	return r.read(ctx, client, prior.Outputs.DomainName, prior.Outputs.ApiMappingId)
}

func (r *ApiMappingResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *ApiMappingResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	err = withConflictRetry(ctx, func(ctx context.Context) error {
		_, err := client.DeleteApiMapping(ctx, &apigatewayv2.DeleteApiMappingInput{
			ApiMappingId: aws.String(prior.ApiMappingId),
			DomainName:   aws.String(prior.DomainName),
		})
		return err
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete api mapping: %w", err)
	}
	return nil
}

func (r *ApiMappingResource) createInput() *apigatewayv2.CreateApiMappingInput {
	in := &apigatewayv2.CreateApiMappingInput{
		ApiId:      aws.String(r.ApiId),
		DomainName: aws.String(r.DomainName),
		Stage:      aws.String(r.Stage),
	}
	if aws.ToString(r.ApiMappingKey) != "" {
		in.ApiMappingKey = r.ApiMappingKey
	}
	return in
}

func (r *ApiMappingResource) updateInput(
	prior runtime.Prior[ApiMappingResource, *ApiMappingResourceOutput],
) (*apigatewayv2.UpdateApiMappingInput, bool) {
	apiMappingKeyChanged := runtime.Changed(prior.Inputs.ApiMappingKey, r.ApiMappingKey)
	stageChanged := runtime.Changed(prior.Inputs.Stage, r.Stage)
	if !apiMappingKeyChanged && !stageChanged {
		return nil, false
	}
	in := &apigatewayv2.UpdateApiMappingInput{
		ApiId:        aws.String(r.ApiId),
		ApiMappingId: aws.String(prior.Outputs.ApiMappingId),
		DomainName:   aws.String(prior.Outputs.DomainName),
	}
	if apiMappingKeyChanged {
		in.ApiMappingKey = aws.String(aws.ToString(r.ApiMappingKey))
	}
	if stageChanged {
		in.Stage = aws.String(r.Stage)
	}
	return in, true
}
