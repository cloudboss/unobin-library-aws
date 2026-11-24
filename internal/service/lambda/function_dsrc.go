package lambda

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
)

type FunctionDataSource struct {
	FunctionName string  `ub:"function-name"`
	Qualifier    *string `ub:"qualifier"`
}

type FunctionDataSourceOutput struct {
	Arn                          string                     `ub:"arn"`
	QualifiedArn                 string                     `ub:"qualified-arn"`
	InvokeArn                    string                     `ub:"invoke-arn"`
	QualifiedInvokeArn           string                     `ub:"qualified-invoke-arn"`
	ResponseStreamingInvokeArn   string                     `ub:"response-streaming-invoke-arn"`
	CodeSha256                   string                     `ub:"code-sha256"`
	SourceCodeHash               string                     `ub:"source-code-hash"`
	CodeSigningConfigArn         string                     `ub:"code-signing-config-arn"`
	Description                  string                     `ub:"description"`
	Handler                      string                     `ub:"handler"`
	ImageUri                     string                     `ub:"image-uri"`
	KMSKeyArn                    string                     `ub:"kms-key-arn"`
	LastModified                 string                     `ub:"last-modified"`
	MemorySize                   int64                      `ub:"memory-size"`
	ReservedConcurrentExecutions int64                      `ub:"reserved-concurrent-executions"`
	Role                         string                     `ub:"role"`
	Runtime                      string                     `ub:"runtime"`
	SigningJobArn                string                     `ub:"signing-job-arn"`
	SigningProfileVersionArn     string                     `ub:"signing-profile-version-arn"`
	SourceCodeSize               int64                      `ub:"source-code-size"`
	SourceKMSKeyArn              string                     `ub:"source-kms-key-arn"`
	Timeout                      int64                      `ub:"timeout"`
	Version                      string                     `ub:"version"`
	Architectures                []string                   `ub:"architectures"`
	Layers                       []string                   `ub:"layers"`
	Tags                         map[string]string          `ub:"tags"`
	CapacityProviderConfig       *functionLookupCapacity    `ub:"capacity-provider-config"`
	DeadLetterConfig             *functionLookupDeadLetter  `ub:"dead-letter-config"`
	DurableConfig                *functionLookupDurable     `ub:"durable-config"`
	Environment                  *FunctionEnvironment       `ub:"environment"`
	EphemeralStorage             *functionLookupStorage     `ub:"ephemeral-storage"`
	FileSystemConfigs            []FunctionFileSystemConfig `ub:"file-system-configs"`
	LoggingConfig                *functionLookupLogging     `ub:"logging-config"`
	TenancyConfig                *functionLookupTenancy     `ub:"tenancy-config"`
	TracingConfig                functionLookupTracing      `ub:"tracing-config"`
	VpcConfig                    *functionLookupVPC         `ub:"vpc-config"`
}

type functionLookupCapacity struct {
	Managed functionLookupManaged `ub:"lambda-managed-instances-capacity-provider-config"`
}

type functionLookupManaged struct {
	CapacityProviderArn string  `ub:"capacity-provider-arn"`
	Memory              float64 `ub:"execution-environment-memory-gib-per-vcpu"`
	MaxConcurrency      int64   `ub:"per-execution-environment-max-concurrency"`
}

type functionLookupDeadLetter struct {
	TargetArn string `ub:"target-arn"`
}
type functionLookupDurable struct {
	ExecutionTimeout      int64 `ub:"execution-timeout"`
	RetentionPeriodInDays int64 `ub:"retention-period-in-days"`
}
type functionLookupStorage struct {
	Size int64 `ub:"size"`
}
type functionLookupLogging struct {
	ApplicationLogLevel string `ub:"application-log-level"`
	LogFormat           string `ub:"log-format"`
	LogGroup            string `ub:"log-group"`
	SystemLogLevel      string `ub:"system-log-level"`
}
type functionLookupTenancy struct {
	TenantIsolationMode string `ub:"tenant-isolation-mode"`
}
type functionLookupTracing struct {
	Mode string `ub:"mode"`
}
type functionLookupVPC struct {
	Ipv6AllowedForDualStack bool     `ub:"ipv6-allowed-for-dual-stack"`
	SecurityGroupIds        []string `ub:"security-group-ids"`
	SubnetIds               []string `ub:"subnet-ids"`
	VpcID                   string   `ub:"vpc-id"`
}

type functionLookupAPI interface {
	ListVersionsByFunction(context.Context, *lambda.ListVersionsByFunctionInput,
		...func(*lambda.Options)) (*lambda.ListVersionsByFunctionOutput, error)
	GetFunction(context.Context, *lambda.GetFunctionInput,
		...func(*lambda.Options)) (*lambda.GetFunctionOutput, error)
	GetFunctionConcurrency(context.Context, *lambda.GetFunctionConcurrencyInput,
		...func(*lambda.Options)) (*lambda.GetFunctionConcurrencyOutput, error)
	GetFunctionCodeSigningConfig(context.Context, *lambda.GetFunctionCodeSigningConfigInput,
		...func(*lambda.Options)) (*lambda.GetFunctionCodeSigningConfigOutput, error)
}

func (r *FunctionDataSource) Read(
	ctx context.Context, cfg *awsCfg,
) (*FunctionDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, region(client))
}

func (r *FunctionDataSource) read(
	ctx context.Context, client functionLookupAPI, region string,
) (*FunctionDataSourceOutput, error) {
	input := &lambda.GetFunctionInput{FunctionName: aws.String(r.FunctionName), Qualifier: r.Qualifier}
	if aws.ToString(input.Qualifier) == "" {
		var marker *string
		var selected *string
		for {
			page, err := client.ListVersionsByFunction(ctx, &lambda.ListVersionsByFunctionInput{
				FunctionName: input.FunctionName, MaxItems: aws.Int32(10000), Marker: marker,
			})
			if err != nil {
				return nil, fmt.Errorf("list function versions %q: %w", r.FunctionName, err)
			}
			if page == nil {
				return nil, fmt.Errorf("list function versions %q: empty response", r.FunctionName)
			}
			if page.NextMarker == nil && len(page.Versions) > 0 {
				selected = aws.String(aws.ToString(page.Versions[len(page.Versions)-1].Version))
			}
			marker = page.NextMarker
			if aws.ToString(marker) == "" {
				break
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("list function versions %q: empty result", r.FunctionName)
		}
		input.Qualifier = selected
		if *selected == "$LATEST" {
			input.Qualifier = nil
		}
	}
	result, err := client.GetFunction(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("get function %q: %w", r.FunctionName, err)
	}
	if result == nil || result.Code == nil || result.Configuration == nil {
		return nil, fmt.Errorf("get function %q: empty result", r.FunctionName)
	}
	concurrency := result.Concurrency
	if concurrency == nil && input.Qualifier != nil {
		response, err := client.GetFunctionConcurrency(ctx, &lambda.GetFunctionConcurrencyInput{
			FunctionName: input.FunctionName,
		})
		if err != nil {
			return nil, fmt.Errorf("get function concurrency %q: %w", r.FunctionName, err)
		}
		if response == nil {
			return nil, fmt.Errorf("get function concurrency %q: empty response", r.FunctionName)
		}
		concurrency = &types.Concurrency{
			ReservedConcurrentExecutions: response.ReservedConcurrentExecutions,
		}
	}
	c := result.Configuration
	rawArn := aws.ToString(c.FunctionArn)
	version := aws.ToString(c.Version)
	qualifier := aws.ToString(input.Qualifier)
	qualifiedArn := rawArn
	if !strings.HasSuffix(rawArn, ":"+qualifier) && !strings.HasSuffix(rawArn, ":"+version) {
		qualifiedArn += ":" + version
	}
	arn := strings.TrimSuffix(rawArn, ":"+qualifier)
	prefix := "arn:" + partition.Of(region) + ":apigateway:" + region + ":lambda:path/"
	out := &FunctionDataSourceOutput{
		Arn: arn, QualifiedArn: qualifiedArn,
		InvokeArn:          prefix + "2015-03-31/functions/" + arn + "/invocations",
		QualifiedInvokeArn: prefix + "2015-03-31/functions/" + qualifiedArn + "/invocations",
		ResponseStreamingInvokeArn: prefix + "2021-11-15/functions/" +
			rawArn + "/response-streaming-invocations",
		CodeSha256:     aws.ToString(c.CodeSha256),
		SourceCodeHash: aws.ToString(c.CodeSha256),
		Description:    aws.ToString(c.Description), Handler: aws.ToString(c.Handler),
		ImageUri: aws.ToString(result.Code.ImageUri), KMSKeyArn: aws.ToString(c.KMSKeyArn),
		LastModified: aws.ToString(c.LastModified), MemorySize: int64(aws.ToInt32(c.MemorySize)),
		ReservedConcurrentExecutions: -1, Role: aws.ToString(c.Role), Runtime: string(c.Runtime),
		SigningJobArn:            aws.ToString(c.SigningJobArn),
		SigningProfileVersionArn: aws.ToString(c.SigningProfileVersionArn),
		SourceCodeSize:           c.CodeSize, SourceKMSKeyArn: aws.ToString(result.Code.SourceKMSKeyArn),
		Timeout: int64(aws.ToInt32(c.Timeout)), Version: version,
		Architectures: []string{}, Layers: []string{}, Tags: map[string]string{},
		FileSystemConfigs: []FunctionFileSystemConfig{},
		TracingConfig:     functionLookupTracing{Mode: "PassThrough"},
	}
	if concurrency != nil {
		out.ReservedConcurrentExecutions = int64(aws.ToInt32(concurrency.ReservedConcurrentExecutions))
	}
	for _, value := range c.Architectures {
		out.Architectures = append(out.Architectures, string(value))
	}
	for _, value := range c.Layers {
		out.Layers = append(out.Layers, aws.ToString(value.Arn))
	}
	for key, value := range result.Tags {
		if !strings.HasPrefix(key, "aws:") {
			out.Tags[key] = value
		}
	}
	if value := c.CapacityProviderConfig; value != nil {
		out.CapacityProviderConfig = &functionLookupCapacity{}
		if inner := value.LambdaManagedInstancesCapacityProviderConfig; inner != nil {
			out.CapacityProviderConfig.Managed = functionLookupManaged{
				CapacityProviderArn: aws.ToString(inner.CapacityProviderArn),
				Memory:              aws.ToFloat64(inner.ExecutionEnvironmentMemoryGiBPerVCpu),
				MaxConcurrency:      int64(aws.ToInt32(inner.PerExecutionEnvironmentMaxConcurrency)),
			}
		}
	}
	if value := c.DeadLetterConfig; value != nil && value.TargetArn != nil {
		out.DeadLetterConfig = &functionLookupDeadLetter{TargetArn: *value.TargetArn}
	}
	if value := c.DurableConfig; value != nil &&
		(value.ExecutionTimeout != nil || value.RetentionPeriodInDays != nil) {
		out.DurableConfig = &functionLookupDurable{
			ExecutionTimeout:      int64(aws.ToInt32(value.ExecutionTimeout)),
			RetentionPeriodInDays: int64(aws.ToInt32(value.RetentionPeriodInDays)),
		}
	}
	if value := c.Environment; value != nil {
		variables := map[string]string{}
		maps.Copy(variables, value.Variables)
		out.Environment = &FunctionEnvironment{Variables: variables}
	}
	if value := c.EphemeralStorage; value != nil {
		out.EphemeralStorage = &functionLookupStorage{Size: int64(aws.ToInt32(value.Size))}
	}
	for _, value := range c.FileSystemConfigs {
		out.FileSystemConfigs = append(out.FileSystemConfigs, FunctionFileSystemConfig{
			Arn: aws.ToString(value.Arn), LocalMountPath: aws.ToString(value.LocalMountPath),
		})
	}
	if value := c.LoggingConfig; value != nil {
		out.LoggingConfig = &functionLookupLogging{
			ApplicationLogLevel: string(value.ApplicationLogLevel), LogFormat: string(value.LogFormat),
			LogGroup: aws.ToString(value.LogGroup), SystemLogLevel: string(value.SystemLogLevel),
		}
	}
	if value := c.TenancyConfig; value != nil {
		out.TenancyConfig = &functionLookupTenancy{TenantIsolationMode: string(value.TenantIsolationMode)}
	}
	if value := c.TracingConfig; value != nil {
		out.TracingConfig.Mode = string(value.Mode)
	}
	if value := c.VpcConfig; value != nil && (len(value.SubnetIds) > 0 ||
		len(value.SecurityGroupIds) > 0 || aws.ToString(value.VpcId) != "") {
		subnets := append([]string{}, value.SubnetIds...)
		groups := append([]string{}, value.SecurityGroupIds...)
		slices.Sort(subnets)
		slices.Sort(groups)
		out.VpcConfig = &functionLookupVPC{
			Ipv6AllowedForDualStack: aws.ToBool(value.Ipv6AllowedForDualStack),
			SecurityGroupIds:        slices.Compact(groups), SubnetIds: slices.Compact(subnets),
			VpcID: aws.ToString(value.VpcId),
		}
	}
	signingAvailable := false
	switch region {
	case "us-east-1", "us-east-2", "us-west-1", "us-west-2", "af-south-1", "ap-east-1",
		"ap-south-1", "ap-northeast-2", "ap-southeast-1", "ap-southeast-2", "ap-northeast-1",
		"ca-central-1", "eu-central-1", "eu-west-1", "eu-west-2", "eu-south-1", "eu-west-3",
		"eu-north-1", "me-south-1", "sa-east-1", "us-gov-east-1", "us-gov-west-1":
		signingAvailable = partition.Of(region) == "aws" || partition.Of(region) == "aws-us-gov"
	}
	if c.PackageType == types.PackageTypeZip && signingAvailable {
		response, err := client.GetFunctionCodeSigningConfig(
			ctx, &lambda.GetFunctionCodeSigningConfigInput{FunctionName: input.FunctionName},
		)
		if err != nil {
			return nil, fmt.Errorf("get function code signing configuration %q: %w", r.FunctionName, err)
		}
		if response != nil {
			out.CodeSigningConfigArn = aws.ToString(response.CodeSigningConfigArn)
		}
	}
	return out, nil
}
