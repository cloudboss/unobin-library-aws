package ec2

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type FlowLogResource struct {
	EniId                      *string                    `ub:"eni-id"`
	RegionalNatGatewayId       *string                    `ub:"regional-nat-gateway-id"`
	SubnetId                   *string                    `ub:"subnet-id"`
	VpcId                      *string                    `ub:"vpc-id"`
	TransitGatewayId           *string                    `ub:"transit-gateway-id"`
	TransitGatewayAttachmentId *string                    `ub:"transit-gateway-attachment-id"`
	DeliverCrossAccountRole    *string                    `ub:"deliver-cross-account-role"`
	IamRoleArn                 *string                    `ub:"iam-role-arn"`
	LogDestination             *string                    `ub:"log-destination"`
	LogDestinationType         *string                    `ub:"log-destination-type"`
	LogFormat                  *string                    `ub:"log-format"`
	MaxAggregationInterval     *int64                     `ub:"max-aggregation-interval"`
	TrafficType                *string                    `ub:"traffic-type"`
	DestinationOptions         *FlowLogDestinationOptions `ub:"destination-options"`
	Tags                       *map[string]string         `ub:"tags"`
}

type FlowLogDestinationOptions struct {
	FileFormat               *string `ub:"file-format"`
	HiveCompatiblePartitions *bool   `ub:"hive-compatible-partitions"`
	PerHourPartition         *bool   `ub:"per-hour-partition"`
}

type FlowLogResourceOutput struct {
	FlowLogId                string                     `ub:"flow-log-id"`
	Arn                      string                     `ub:"arn"`
	Region                   string                     `ub:"region"`
	DeliverCrossAccountRole  *string                    `ub:"deliver-cross-account-role"`
	DeliverLogsPermissionArn *string                    `ub:"deliver-logs-permission-arn"`
	DestinationOptions       *FlowLogDestinationOptions `ub:"destination-options"`
	LogDestination           *string                    `ub:"log-destination"`
	LogDestinationType       string                     `ub:"log-destination-type"`
	LogFormat                *string                    `ub:"log-format"`
	MaxAggregationInterval   *int64                     `ub:"max-aggregation-interval"`
	ResourceId               *string                    `ub:"resource-id"`
	TrafficType              *string                    `ub:"traffic-type"`
	Tags                     map[string]string          `ub:"tags"`
}

type flowLogPrior = runtime.Prior[FlowLogResource, *FlowLogResourceOutput, *awsCfg]

func (r FlowLogResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.EniId, r.RegionalNatGatewayId, r.SubnetId, r.VpcId,
			r.TransitGatewayId, r.TransitGatewayAttachmentId),
		constraint.When(constraint.NotEquals(r.LogDestinationType, nil)).
			Require(constraint.OneOf(r.LogDestinationType,
				"cloud-watch-logs", "s3", "kinesis-data-firehose")).
			Message("log-destination-type must be cloud-watch-logs, s3, or kinesis-data-firehose"),
		constraint.When(constraint.NotEquals(r.MaxAggregationInterval, nil)).
			Require(constraint.OneOf(r.MaxAggregationInterval, 60, 600)).
			Message("max-aggregation-interval must be 60 or 600"),
		constraint.When(constraint.NotEquals(r.TrafficType, nil)).
			Require(constraint.OneOf(r.TrafficType, "ACCEPT", "REJECT", "ALL")).
			Message("traffic-type must be ACCEPT, REJECT, or ALL"),
		constraint.When(constraint.NotEquals(r.DestinationOptions.FileFormat, nil)).
			Require(constraint.OneOf(r.DestinationOptions.FileFormat, "plain-text", "parquet")).
			Message("destination-options.file-format must be plain-text or parquet"),
	}
}

var (
	flowLogPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	flowLogRegionPattern    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	flowLogAccountPattern   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|\d{12}|cw.{10})$`,
	)
)

func (r *FlowLogResource) prepareRequest(token string) (*ec2.CreateFlowLogsInput, error) {
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"iam-role-arn", r.IamRoleArn}, {"deliver-cross-account-role", r.DeliverCrossAccountRole},
		{"log-destination", r.LogDestination},
	} {
		if value := aws.ToString(field.value); value != "" {
			parsed, err := arn.Parse(value)
			if err != nil || !flowLogPartitionPattern.MatchString(parsed.Partition) ||
				(parsed.Region != "" && !flowLogRegionPattern.MatchString(parsed.Region)) ||
				(parsed.AccountID != "" && !flowLogAccountPattern.MatchString(parsed.AccountID)) ||
				parsed.Resource == "" {
				return nil, fmt.Errorf("%s: invalid ARN %q", field.name, value)
			}
		}
	}
	tags, _ := tagsync.Diff(nil, ptr.Value(r.Tags))
	input := &ec2.CreateFlowLogsInput{
		ClientToken: aws.String(token),
		LogDestinationType: ec2types.LogDestinationType(flowLogDefault(r.LogDestinationType,
			"cloud-watch-logs")),
		MaxAggregationInterval: aws.Int32(int32(flowLogInterval(r.MaxAggregationInterval))),
		TagSpecifications:      tagSpecifications(ec2types.ResourceTypeVpcFlowLog, tags),
	}
	for _, selector := range []struct {
		id   *string
		kind ec2types.FlowLogsResourceType
	}{
		{r.EniId, ec2types.FlowLogsResourceTypeNetworkInterface},
		{r.RegionalNatGatewayId, ec2types.FlowLogsResourceTypeRegionalNatGateway},
		{r.SubnetId, ec2types.FlowLogsResourceTypeSubnet},
		{r.VpcId, ec2types.FlowLogsResourceTypeVpc},
		{r.TransitGatewayId, ec2types.FlowLogsResourceTypeTransitGateway},
		{r.TransitGatewayAttachmentId, ec2types.FlowLogsResourceTypeTransitGatewayAttachment},
	} {
		if aws.ToString(selector.id) != "" {
			input.ResourceIds = []string{*selector.id}
			input.ResourceType = selector.kind
		}
	}
	if input.ResourceType != ec2types.FlowLogsResourceTypeTransitGateway &&
		input.ResourceType != ec2types.FlowLogsResourceTypeTransitGatewayAttachment {
		input.TrafficType = ec2types.TrafficType(aws.ToString(r.TrafficType))
	}
	if value := aws.ToString(r.LogDestination); value != "" {
		input.LogDestination = aws.String(strings.TrimSuffix(value, ":*"))
	}
	if aws.ToString(r.LogFormat) != "" {
		input.LogFormat = r.LogFormat
	}
	if aws.ToString(r.IamRoleArn) != "" {
		input.DeliverLogsPermissionArn = r.IamRoleArn
	}
	if aws.ToString(r.DeliverCrossAccountRole) != "" {
		input.DeliverCrossAccountRole = r.DeliverCrossAccountRole
	}
	if options := r.DestinationOptions; options != nil {
		input.DestinationOptions = &ec2types.DestinationOptionsRequest{
			FileFormat: ec2types.DestinationFileFormat(
				flowLogDefault(options.FileFormat, "plain-text"),
			),
			HiveCompatiblePartitions: aws.Bool(aws.ToBool(options.HiveCompatiblePartitions)),
			PerHourPartition:         aws.Bool(aws.ToBool(options.PerHourPartition)),
		}
	}
	return input, nil
}

func flowLogDefault(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func flowLogInterval(value *int64) int64 {
	if value == nil {
		return 600
	}
	return *value
}

func flowLogOptionsEqual(prior, desired *FlowLogDestinationOptions) bool {
	if prior == nil || desired == nil {
		return prior == desired
	}
	return flowLogDefault(prior.FileFormat, "plain-text") ==
		flowLogDefault(desired.FileFormat, "plain-text") &&
		aws.ToBool(prior.HiveCompatiblePartitions) == aws.ToBool(desired.HiveCompatiblePartitions) &&
		aws.ToBool(prior.PerHourPartition) == aws.ToBool(desired.PerHourPartition)
}

func (*FlowLogResource) ResourceDefinition() runtime.ResourceDefinition[
	FlowLogResource, *FlowLogResourceOutput, *awsCfg,
] {
	definition := runtime.ResourceDefinition[FlowLogResource, *FlowLogResourceOutput, *awsCfg]{
		SchemaVersion: 1,
		Validate: func(_ context.Context, input FlowLogResource, _ *awsCfg) error {
			_, err := input.prepareRequest("")
			return err
		},
		Replace: runtime.Replacement[FlowLogResource, *FlowLogResourceOutput, *awsCfg]{
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(_ FlowLogResource, output *FlowLogResourceOutput) (string, error) {
			if output == nil {
				return "", runtime.ErrNotFound
			}
			return output.FlowLogId, nil
		},
	}
	eniId := runtime.InputField(func(r *FlowLogResource) **string { return &r.EniId })
	definition.Replace.Fields = append(definition.Replace.Fields, eniId)
	definition.Equality = append(definition.Equality, runtime.EqualBy(eniId, peeringOptionalEqual))
	regionalNatGatewayId := runtime.InputField(
		func(r *FlowLogResource) **string { return &r.RegionalNatGatewayId },
	)
	definition.Replace.Fields = append(definition.Replace.Fields, regionalNatGatewayId)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(regionalNatGatewayId, peeringOptionalEqual),
	)
	subnetId := runtime.InputField(func(r *FlowLogResource) **string { return &r.SubnetId })
	definition.Replace.Fields = append(definition.Replace.Fields, subnetId)
	definition.Equality = append(definition.Equality, runtime.EqualBy(subnetId, peeringOptionalEqual))
	vpcId := runtime.InputField(func(r *FlowLogResource) **string { return &r.VpcId })
	definition.Replace.Fields = append(definition.Replace.Fields, vpcId)
	definition.Equality = append(definition.Equality, runtime.EqualBy(vpcId, peeringOptionalEqual))
	transitGatewayId := runtime.InputField(
		func(r *FlowLogResource) **string { return &r.TransitGatewayId },
	)
	definition.Replace.Fields = append(definition.Replace.Fields, transitGatewayId)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(transitGatewayId, peeringOptionalEqual),
	)
	transitGatewayAttachmentId := runtime.InputField(
		func(r *FlowLogResource) **string { return &r.TransitGatewayAttachmentId },
	)
	definition.Replace.Fields = append(definition.Replace.Fields, transitGatewayAttachmentId)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(transitGatewayAttachmentId, peeringOptionalEqual),
	)
	deliverCrossAccountRole := runtime.InputField(
		func(r *FlowLogResource) **string { return &r.DeliverCrossAccountRole },
	)
	definition.Replace.Fields = append(definition.Replace.Fields, deliverCrossAccountRole)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(deliverCrossAccountRole, peeringOptionalEqual),
	)
	iamRoleArn := runtime.InputField(func(r *FlowLogResource) **string { return &r.IamRoleArn })
	definition.Replace.Fields = append(definition.Replace.Fields, iamRoleArn)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(iamRoleArn, peeringOptionalEqual),
	)
	logDestination := runtime.InputField(
		func(r *FlowLogResource) **string { return &r.LogDestination },
	)
	definition.Replace.Fields = append(definition.Replace.Fields, logDestination)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(logDestination, func(prior, desired *string) bool {
			return aws.ToString(desired) == "" || aws.ToString(prior) == aws.ToString(desired)
		}),
	)
	logDestinationType := runtime.InputField(
		func(r *FlowLogResource) **string { return &r.LogDestinationType },
	)
	definition.Replace.Fields = append(definition.Replace.Fields, logDestinationType)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(logDestinationType, func(prior, desired *string) bool {
			return flowLogDefault(prior, "cloud-watch-logs") ==
				flowLogDefault(desired, "cloud-watch-logs")
		}),
	)
	logFormat := runtime.InputField(func(r *FlowLogResource) **string { return &r.LogFormat })
	definition.Replace.Fields = append(definition.Replace.Fields, logFormat)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(logFormat, func(prior, desired *string) bool {
			return aws.ToString(desired) == "" || aws.ToString(prior) == aws.ToString(desired)
		}),
	)
	trafficType := runtime.InputField(func(r *FlowLogResource) **string { return &r.TrafficType })
	definition.Replace.Fields = append(definition.Replace.Fields, trafficType)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(trafficType, peeringOptionalEqual),
	)
	interval := runtime.InputField(func(r *FlowLogResource) **int64 {
		return &r.MaxAggregationInterval
	})
	options := runtime.InputField(func(r *FlowLogResource) **FlowLogDestinationOptions {
		return &r.DestinationOptions
	})
	definition.Replace.Fields = append(definition.Replace.Fields, interval, options)
	definition.Equality = append(definition.Equality,
		runtime.EqualBy(interval, func(prior, desired *int64) bool {
			return flowLogInterval(prior) == flowLogInterval(desired)
		}),
		runtime.EqualBy(options, flowLogOptionsEqual),
		runtime.EqualBy(runtime.InputField(func(r *FlowLogResource) **map[string]string {
			return &r.Tags
		}), func(prior, desired *map[string]string) bool {
			return maps.Equal(ptr.Value(prior), ptr.Value(desired))
		}),
	)
	return definition
}

type flowLogClient struct {
	ec2          *ec2.Client
	config       aws.Config
	stsEndpoint  string
	clock        flowLogClock
	retryTimeout time.Duration
}

func newFlowLogClient(ctx context.Context, cfg *awsCfg) (*flowLogClient, error) {
	sdkCfg, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &flowLogClient{ec2: ec2.NewFromConfig(sdkCfg), config: sdkCfg,
		stsEndpoint: cfg.STSEndpoint(), clock: flowLogWallClock{}, retryTimeout: 2 * time.Minute}, nil
}

func (r *FlowLogResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*FlowLogResourceOutput, error) {
	input, err := r.prepareRequest(rand.Text())
	if err != nil {
		return nil, err
	}
	client, err := newFlowLogClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return client.create(ctx, input)
}

func (c *flowLogClient) create(
	ctx context.Context, input *ec2.CreateFlowLogsInput,
) (*FlowLogResourceOutput, error) {
	var response *ec2.CreateFlowLogsOutput
	err := retryFlowLog(ctx, c.clock, c.retryTimeout, func(ctx context.Context) error {
		var err error
		response, err = c.ec2.CreateFlowLogs(ctx, input)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create flow log: %w", err)
	}
	if err := flowLogUnsuccessful(response.Unsuccessful); err != nil {
		return nil, err
	}
	if len(response.FlowLogIds) == 0 || response.FlowLogIds[0] == "" {
		return nil, errors.New("create flow log returned no flow log ID")
	}
	id := response.FlowLogIds[0]
	output, err := c.read(ctx, id)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			err = errors.New("flow log disappeared")
		}
		failure := fmt.Errorf("read flow log %s after create: %w", id, err)
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if cleanupErr := c.delete(cleanupCtx, id); cleanupErr != nil {
			return nil, errors.Join(failure, fmt.Errorf(
				"clean up flow log %s in region %s after failed create: %w",
				id, c.config.Region, cleanupErr))
		}
		return nil, failure
	}
	return output, nil
}

func (*FlowLogResource) Read(
	ctx context.Context, cfg *awsCfg, prior flowLogPrior,
) (*FlowLogResourceOutput, error) {
	if prior.Outputs == nil || prior.Outputs.FlowLogId == "" {
		return nil, runtime.ErrNotFound
	}
	client, err := newFlowLogClient(ctx, flowLogPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	return client.read(ctx, prior.Outputs.FlowLogId)
}

func (r *FlowLogResource) Update(
	ctx context.Context, cfg *awsCfg, prior flowLogPrior,
) (*FlowLogResourceOutput, error) {
	if _, err := r.prepareRequest(""); err != nil {
		return nil, err
	}
	if prior.Outputs == nil || prior.Outputs.FlowLogId == "" {
		return nil, errors.New("update flow log requires a saved flow log ID")
	}
	client, err := newFlowLogClient(ctx, flowLogPriorConfig(cfg, prior))
	if err != nil {
		return nil, err
	}
	known := prior.Outputs.Tags
	if prior.Observed != nil {
		known = prior.Observed.Tags
	}
	err = syncEC2Tags(ctx, client.ec2, prior.Outputs.FlowLogId, ptr.Value(r.Tags),
		func(context.Context) (map[string]string, error) { return known, nil })
	if err != nil {
		return nil, err
	}
	return client.read(ctx, prior.Outputs.FlowLogId)
}

func (*FlowLogResource) Delete(ctx context.Context, cfg *awsCfg, prior flowLogPrior) error {
	if prior.Outputs == nil || prior.Outputs.FlowLogId == "" {
		return nil
	}
	client, err := newFlowLogClient(ctx, flowLogPriorConfig(cfg, prior))
	if err != nil {
		return err
	}
	return client.delete(ctx, prior.Outputs.FlowLogId)
}

func (c *flowLogClient) delete(ctx context.Context, id string) error {
	response, err := c.ec2.DeleteFlowLogs(ctx, &ec2.DeleteFlowLogsInput{
		FlowLogIds: []string{id},
	})
	if isNotFound(err, "InvalidFlowLogId.NotFound") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete flow log: %w", err)
	}
	err = flowLogUnsuccessful(response.Unsuccessful)
	if isNotFound(err, "InvalidFlowLogId.NotFound") {
		return nil
	}
	return err
}

func flowLogPriorConfig(cfg *awsCfg, prior flowLogPrior) *awsCfg {
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	return peeringConfigInRegion(cfg, prior.Outputs.Region)
}

func flowLogUnsuccessful(items []ec2types.UnsuccessfulItem) error {
	var failures []error
	for _, item := range items {
		if item.Error == nil {
			continue
		}
		failures = append(failures, fmt.Errorf("resource %s: %w", aws.ToString(item.ResourceId),
			&smithy.GenericAPIError{Code: aws.ToString(item.Error.Code),
				Message: aws.ToString(item.Error.Message)}))
	}
	return errors.Join(failures...)
}

func (c *flowLogClient) lookup(ctx context.Context, id string) (*ec2types.FlowLog, error) {
	pages := ec2.NewDescribeFlowLogsPaginator(c.ec2, &ec2.DescribeFlowLogsInput{
		FlowLogIds: []string{id},
	})
	var logs []ec2types.FlowLog
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe flow log %s: %w", id, err)
		}
		logs = append(logs, page.FlowLogs...)
	}
	if len(logs) != 1 || aws.ToString(logs[0].FlowLogId) != id {
		return nil, runtime.ErrNotFound
	}
	return &logs[0], nil
}

func (c *flowLogClient) read(ctx context.Context, id string) (*FlowLogResourceOutput, error) {
	log, err := c.lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	account, err := c.accountID(ctx)
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("arn:%s", partition.Of(c.config.Region))
	output := &FlowLogResourceOutput{
		FlowLogId: id, Region: c.config.Region,
		Arn: fmt.Sprintf("%s:ec2:%s:%s:vpc-flow-log/%s",
			prefix, c.config.Region, account, id),
		DeliverCrossAccountRole:  log.DeliverCrossAccountRole,
		DeliverLogsPermissionArn: log.DeliverLogsPermissionArn,
		LogDestination:           log.LogDestination, LogDestinationType: string(log.LogDestinationType),
		LogFormat: log.LogFormat, ResourceId: log.ResourceId, Tags: map[string]string{},
	}
	if log.LogDestination == nil && log.LogDestinationType == "cloud-watch-logs" {
		output.LogDestination = aws.String(fmt.Sprintf("%s:logs:%s:%s:log-group:%s", prefix,
			c.config.Region, account, aws.ToString(log.LogGroupName)))
	}
	if log.MaxAggregationInterval != nil {
		output.MaxAggregationInterval = aws.Int64(int64(*log.MaxAggregationInterval))
	}
	if !strings.HasPrefix(aws.ToString(log.ResourceId), "tgw-") {
		output.TrafficType = aws.String(string(log.TrafficType))
	}
	if options := log.DestinationOptions; options != nil {
		output.DestinationOptions = &FlowLogDestinationOptions{
			FileFormat:               aws.String(string(options.FileFormat)),
			HiveCompatiblePartitions: options.HiveCompatiblePartitions,
			PerHourPartition:         options.PerHourPartition,
		}
	}
	for _, tag := range log.Tags {
		if !strings.HasPrefix(aws.ToString(tag.Key), "aws:") {
			output.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}
	return output, nil
}

func (c *flowLogClient) accountID(ctx context.Context) (string, error) {
	if c.config.Credentials != nil {
		creds, err := c.config.Credentials.Retrieve(ctx)
		if err == nil && creds.AccountID != "" {
			return creds.AccountID, nil
		}
	}
	client := sts.NewFromConfig(c.config, func(o *sts.Options) {
		if c.stsEndpoint != "" {
			o.BaseEndpoint = aws.String(c.stsEndpoint)
		}
	})
	response, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("get caller identity for flow log: %w", err)
	}
	if aws.ToString(response.Account) == "" {
		return "", errors.New("get caller identity for flow log returned no account")
	}
	return *response.Account, nil
}
