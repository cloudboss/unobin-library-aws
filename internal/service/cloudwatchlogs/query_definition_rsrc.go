package cloudwatchlogs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

type QueryDefinitionResource struct {
	Name          string    `ub:"name"`
	QueryString   string    `ub:"query-string"`
	LogGroupNames *[]string `ub:"log-group-names"`
}

type QueryDefinitionResourceOutput struct {
	Arn               string   `ub:"arn"`
	QueryDefinitionID string   `ub:"query-definition-id"`
	Name              string   `ub:"name"`
	QueryString       string   `ub:"query-string"`
	LogGroupNames     []string `ub:"log-group-names"`
	Region            string   `ub:"region"`
}

func (r *QueryDefinitionResource) prepareRequest(id string) (
	*cloudwatchlogs.PutQueryDefinitionInput, error,
) {
	input := &cloudwatchlogs.PutQueryDefinitionInput{
		Name: aws.String(r.Name), QueryString: aws.String(r.QueryString),
	}
	if id != "" {
		input.QueryDefinitionId = aws.String(id)
	}
	if r.LogGroupNames != nil && len(*r.LogGroupNames) > 0 {
		input.LogGroupNames = make([]string, 0, len(*r.LogGroupNames))
		for _, name := range *r.LogGroupNames {
			if name == "" {
				continue
			}
			if len(name) > 512 || !queryDefinitionLogName.MatchString(name) {
				parsed, err := arn.Parse(name)
				if err != nil || !queryDefinitionPartition.MatchString(parsed.Partition) ||
					parsed.Region != "" && !queryDefinitionRegion.MatchString(parsed.Region) ||
					parsed.AccountID != "" && !queryDefinitionAccount.MatchString(parsed.AccountID) ||
					parsed.Resource == "" {
					return nil, fmt.Errorf("invalid log-group-names entry %q", name)
				}
			}
			input.LogGroupNames = append(input.LogGroupNames, name)
		}
	}
	return input, nil
}

var (
	queryDefinitionLogName   = regexp.MustCompile(`^[0-9A-Za-z_./#-]+$`)
	queryDefinitionPartition = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	queryDefinitionRegion    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	queryDefinitionAccount   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|\d{12}|cw.{10})$`)
)

func (r QueryDefinitionResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.Name))).Message("name is required"),
		constraint.Must(constraint.Expr("input.value != null",
			constraint.Bind("value", r.QueryString))).Message("query-string is required"),
		constraint.Must(constraint.All(
			constraint.AtLeast(constraint.ByteLength(r.Name), 1),
			constraint.AtMost(constraint.ByteLength(r.Name), 255),
		)).Message("name must be 1 to 255 bytes"),
		constraint.Must(constraint.IsMatch(r.Name, `^([^:*\/]+\/?)*[^:*\/]+$`)).
			Message("name must match the query definition name pattern"),
	}
}

type queryDefinitionPrior = runtime.Prior[
	QueryDefinitionResource, *QueryDefinitionResourceOutput, *awsCfg,
]

func (*QueryDefinitionResource) ResourceDefinition() runtime.ResourceDefinition[
	QueryDefinitionResource, *QueryDefinitionResourceOutput, *awsCfg,
] {
	return runtime.ResourceDefinition[
		QueryDefinitionResource, *QueryDefinitionResourceOutput, *awsCfg,
	]{
		SchemaVersion: 1,
		Validate: func(_ context.Context, input QueryDefinitionResource, _ *awsCfg) error {
			_, err := input.prepareRequest("")
			return err
		},
		Replace: runtime.Replacement[QueryDefinitionResource, *QueryDefinitionResourceOutput, *awsCfg]{
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(_ QueryDefinitionResource, out *QueryDefinitionResourceOutput) (string, error) {
			if out == nil || out.Arn == "" {
				return "", runtime.ErrNotFound
			}
			return out.Arn, nil
		},
	}
}

type queryDefinitionClient struct {
	logs        *cloudwatchlogs.Client
	config      aws.Config
	stsEndpoint string
}

func newQueryDefinitionClient(ctx context.Context, cfg *awsCfg) (*queryDefinitionClient, error) {
	sdkCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &queryDefinitionClient{
		logs: cloudwatchlogs.NewFromConfig(sdkCfg, func(o *cloudwatchlogs.Options) {
			o.Retryer = logResourceLimitRetryer{Retryer: o.Retryer}
		}),
		config: sdkCfg, stsEndpoint: cfg.STSEndpoint(),
	}, nil
}

func (r *QueryDefinitionResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*QueryDefinitionResourceOutput, error) {
	input, err := r.prepareRequest("")
	if err != nil {
		return nil, err
	}
	client, err := newQueryDefinitionClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	response, err := client.logs.PutQueryDefinition(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("put query definition: %w", err)
	}
	id := aws.ToString(response.QueryDefinitionId)
	if id == "" {
		return nil, errors.New("create query definition returned no ID")
	}
	out, err := client.read(ctx, id, r.Name)
	if err == nil {
		return out, nil
	}
	if errors.Is(err, runtime.ErrNotFound) {
		err = errors.New("query definition disappeared after create")
	}
	failure := fmt.Errorf("initialize query definition %s: %w", id, err)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if cleanupErr := client.delete(cleanupCtx, id); cleanupErr != nil {
		return nil, errors.Join(failure, fmt.Errorf("clean up query definition %s: %w", id, cleanupErr))
	}
	return nil, failure
}

func (*QueryDefinitionResource) Read(
	ctx context.Context, cfg *awsCfg, prior queryDefinitionPrior,
) (*QueryDefinitionResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.QueryDefinitionID == "" {
		return nil, runtime.ErrNotFound
	}
	client, err := newQueryDefinitionClient(ctx, queryDefinitionPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	name := prior.Outputs.Name
	if name == "" {
		name = prior.Inputs.Name
	}
	return client.read(ctx, prior.Outputs.QueryDefinitionID, name)
}

func (r *QueryDefinitionResource) Update(
	ctx context.Context, cfg *awsCfg, prior queryDefinitionPrior,
) (*QueryDefinitionResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.QueryDefinitionID == "" {
		return nil, errors.New("update query definition requires a saved ID")
	}
	input, err := r.prepareRequest(prior.Outputs.QueryDefinitionID)
	if err != nil {
		return nil, err
	}
	client, err := newQueryDefinitionClient(ctx, queryDefinitionPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	known := prior.Observed
	if known == nil {
		known = prior.Outputs
	}
	if known.Name != r.Name || known.QueryString != r.QueryString ||
		!slices.Equal(known.LogGroupNames, input.LogGroupNames) ||
		runtime.Changed(prior.Inputs.LogGroupNames, r.LogGroupNames) {
		if _, err := client.logs.PutQueryDefinition(ctx, input); err != nil {
			return nil, fmt.Errorf("put query definition: %w", err)
		}
	}
	return client.read(ctx, prior.Outputs.QueryDefinitionID, r.Name)
}

func (*QueryDefinitionResource) Delete(
	ctx context.Context, cfg *awsCfg, prior queryDefinitionPrior,
) error {
	if prior.Outputs == nil || prior.Outputs.QueryDefinitionID == "" {
		return nil
	}
	client, err := newQueryDefinitionClient(ctx, queryDefinitionPriorConfig(cfg, prior))
	if err != nil {
		return err
	}
	return client.delete(ctx, prior.Outputs.QueryDefinitionID)
}

func queryDefinitionPriorConfig(cfg *awsCfg, prior queryDefinitionPrior) *awsCfg {
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	copy := awsCfg{}
	if cfg != nil {
		copy = *cfg
	}
	if prior.Outputs != nil && prior.Outputs.Region != "" {
		copy.Region = aws.String(prior.Outputs.Region)
	}
	return &copy
}

func (c *queryDefinitionClient) delete(ctx context.Context, id string) error {
	_, err := c.logs.DeleteQueryDefinition(ctx, &cloudwatchlogs.DeleteQueryDefinitionInput{
		QueryDefinitionId: aws.String(id),
	})
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete query definition: %w", err)
	}
	return nil
}

func (c *queryDefinitionClient) read(
	ctx context.Context, id, name string,
) (*QueryDefinitionResourceOutput, error) {
	input := &cloudwatchlogs.DescribeQueryDefinitionsInput{}
	if name != "" {
		input.QueryDefinitionNamePrefix = aws.String(name)
	}
	for {
		page, err := c.logs.DescribeQueryDefinitions(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("describe query definitions: %w", err)
		}
		for _, item := range page.QueryDefinitions {
			if aws.ToString(item.QueryDefinitionId) != id {
				continue
			}
			account, err := c.accountID(ctx)
			if err != nil {
				return nil, err
			}
			groups := append([]string{}, item.LogGroupNames...)
			return &QueryDefinitionResourceOutput{
				Arn: fmt.Sprintf("arn:%s:logs:%s:%s:query-definition:%s",
					partition.Of(c.config.Region), c.config.Region, account, id),
				QueryDefinitionID: id, Name: aws.ToString(item.Name),
				QueryString: aws.ToString(item.QueryString), LogGroupNames: groups,
				Region: c.config.Region,
			}, nil
		}
		if aws.ToString(page.NextToken) == "" {
			return nil, runtime.ErrNotFound
		}
		input.NextToken = page.NextToken
	}
}

func (c *queryDefinitionClient) accountID(ctx context.Context) (string, error) {
	if c.config.Credentials != nil {
		credentials, err := c.config.Credentials.Retrieve(ctx)
		if err == nil && credentials.AccountID != "" {
			return credentials.AccountID, nil
		}
	}
	client := sts.NewFromConfig(c.config, func(o *sts.Options) {
		if c.stsEndpoint != "" {
			o.BaseEndpoint = aws.String(c.stsEndpoint)
		}
	})
	response, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("get caller identity for query definition: %w", err)
	}
	if aws.ToString(response.Account) == "" {
		return "", errors.New("get caller identity for query definition returned no account")
	}
	return *response.Account, nil
}
