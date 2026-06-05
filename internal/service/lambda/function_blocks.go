package lambda

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// The nested blocks below model the structured members Lambda accepts on
// CreateFunction and UpdateFunctionConfiguration. Unlike an S3 bucket's
// configuration, none is a separate operation: each is a field on the function
// request, so a block is converted to its SDK type and assembled into the input
// rather than written by its own call. A nil block leaves that member unset, so
// AWS applies its own default. The enum and range rules on a block's fields are
// declared in Function's Constraints.

// FunctionCode is the function's deployment package source, the Code member of
// CreateFunction. Exactly one source is given: an inline zip, an on-disk zip,
// an object in S3 (a bucket and key, optionally pinned to a version), or a
// container image. SourceKMSKeyArn names the KMS key the package is encrypted
// with and applies only to a zip, not an image. The cross-field rules are
// declared in Function's Constraints.
type FunctionCode struct {
	ZipFileContent  *string `ub:"zip-file-content"`
	ZipFilePath     *string `ub:"zip-file-path"`
	S3Bucket        *string `ub:"s3-bucket"`
	S3Key           *string `ub:"s3-key"`
	S3ObjectVersion *string `ub:"s3-object-version"`
	ImageUri        *string `ub:"image-uri"`
	SourceKMSKeyArn *string `ub:"source-kms-key-arn"`
}

// FunctionEnvironment holds the environment variables the function sees at
// runtime. The variables are not marked sensitive, matching how Lambda and the
// console treat them as ordinary configuration.
type FunctionEnvironment struct {
	Variables map[string]string `ub:"variables"`
}

func (b *FunctionEnvironment) to() *lambdatypes.Environment {
	if b == nil {
		return nil
	}
	return &lambdatypes.Environment{Variables: b.Variables}
}

// FunctionVpcConfig attaches the function to a VPC by its subnets and security
// groups. When set, the function reaches the network only through that VPC.
// Ipv6AllowedForDualStack permits outbound IPv6 on dual-stack subnets.
type FunctionVpcConfig struct {
	SubnetIds               []string `ub:"subnet-ids"`
	SecurityGroupIds        []string `ub:"security-group-ids"`
	Ipv6AllowedForDualStack *bool    `ub:"ipv6-allowed-for-dual-stack"`
}

func (b *FunctionVpcConfig) to() *lambdatypes.VpcConfig {
	if b == nil {
		return nil
	}
	return &lambdatypes.VpcConfig{
		SubnetIds:               b.SubnetIds,
		SecurityGroupIds:        b.SecurityGroupIds,
		Ipv6AllowedForDualStack: b.Ipv6AllowedForDualStack,
	}
}

// FunctionDeadLetterConfig names the SQS queue or SNS topic where Lambda sends
// an asynchronous event that fails every processing attempt.
type FunctionDeadLetterConfig struct {
	TargetArn *string `ub:"target-arn"`
}

func (b *FunctionDeadLetterConfig) to() *lambdatypes.DeadLetterConfig {
	if b == nil {
		return nil
	}
	return &lambdatypes.DeadLetterConfig{TargetArn: b.TargetArn}
}

// FunctionTracingConfig sets the function's X-Ray tracing mode. Mode is Active
// to sample and trace incoming requests, or PassThrough to trace only when the
// caller already is.
type FunctionTracingConfig struct {
	Mode *string `ub:"mode"`
}

func (b *FunctionTracingConfig) to() *lambdatypes.TracingConfig {
	if b == nil {
		return nil
	}
	cfg := &lambdatypes.TracingConfig{}
	if b.Mode != nil {
		cfg.Mode = lambdatypes.TracingMode(*b.Mode)
	}
	return cfg
}

// FunctionImageConfig overrides the container image's Dockerfile settings. It
// applies only to an Image package type; Command and EntryPoint override the
// image's CMD and ENTRYPOINT, and WorkingDirectory its WORKDIR.
type FunctionImageConfig struct {
	Command          []string `ub:"command"`
	EntryPoint       []string `ub:"entry-point"`
	WorkingDirectory *string  `ub:"working-directory"`
}

func (b *FunctionImageConfig) to() *lambdatypes.ImageConfig {
	if b == nil {
		return nil
	}
	return &lambdatypes.ImageConfig{
		Command:          b.Command,
		EntryPoint:       b.EntryPoint,
		WorkingDirectory: b.WorkingDirectory,
	}
}

// FunctionLoggingConfig directs where and how the function's logs go to
// CloudWatch. LogFormat is Text or JSON; the two log levels apply only under
// JSON. LogGroup names a group other than the default per-function one.
type FunctionLoggingConfig struct {
	LogFormat           *string `ub:"log-format"`
	LogGroup            *string `ub:"log-group"`
	ApplicationLogLevel *string `ub:"application-log-level"`
	SystemLogLevel      *string `ub:"system-log-level"`
}

func (b *FunctionLoggingConfig) to() *lambdatypes.LoggingConfig {
	if b == nil {
		return nil
	}
	cfg := &lambdatypes.LoggingConfig{LogGroup: b.LogGroup}
	if b.LogFormat != nil {
		cfg.LogFormat = lambdatypes.LogFormat(*b.LogFormat)
	}
	if b.ApplicationLogLevel != nil {
		cfg.ApplicationLogLevel = lambdatypes.ApplicationLogLevel(*b.ApplicationLogLevel)
	}
	if b.SystemLogLevel != nil {
		cfg.SystemLogLevel = lambdatypes.SystemLogLevel(*b.SystemLogLevel)
	}
	return cfg
}

// FunctionSnapStart turns on SnapStart, which snapshots the initialized
// execution environment when a version is published so later invocations start
// from it. ApplyOn is None or PublishedVersions.
type FunctionSnapStart struct {
	ApplyOn *string `ub:"apply-on"`
}

func (b *FunctionSnapStart) to() *lambdatypes.SnapStart {
	if b == nil {
		return nil
	}
	cfg := &lambdatypes.SnapStart{}
	if b.ApplyOn != nil {
		cfg.ApplyOn = lambdatypes.SnapStartApplyOn(*b.ApplyOn)
	}
	return cfg
}

// FunctionEphemeralStorage sets the size in MB of the function's /tmp
// directory, between 512 and 10240.
type FunctionEphemeralStorage struct {
	Size *int64 `ub:"size"`
}

func (b *FunctionEphemeralStorage) to() *lambdatypes.EphemeralStorage {
	if b == nil {
		return nil
	}
	return &lambdatypes.EphemeralStorage{Size: ptr.Int32(b.Size)}
}

// FunctionFileSystemConfig connects the function to one EFS or S3 Files access
// point at a mount path under /mnt. Both fields are required by the Lambda API.
type FunctionFileSystemConfig struct {
	Arn            string `ub:"arn"`
	LocalMountPath string `ub:"local-mount-path"`
}

// fileSystemConfigs converts the declared file system mounts to the SDK list.
// Lambda takes a list, though a function attaches at most one; an empty list
// leaves the member unset.
func fileSystemConfigs(blocks []FunctionFileSystemConfig) []lambdatypes.FileSystemConfig {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]lambdatypes.FileSystemConfig, 0, len(blocks))
	for i := range blocks {
		out = append(out, lambdatypes.FileSystemConfig{
			Arn:            aws.String(blocks[i].Arn),
			LocalMountPath: aws.String(blocks[i].LocalMountPath),
		})
	}
	return out
}

// An update sends a removed block or list member as the API's empty value,
// else UpdateFunctionConfiguration reads a nil member as "leave unchanged" and
// the old value stays live. The forUpdate helpers below encode that three-way
// choice: the desired value when it is present, the empty value when the
// member is being removed (it was in the prior inputs but not the desired),
// and nil when it was never set, so an unmanaged member is left alone. The
// scalar members (the description, handler, and KMS key ARN) are not cleared
// this way: a nil scalar is never sent, leaving the value to AWS, and an
// explicit empty string is the way to clear one. Members Lambda does not
// clear at all (ephemeral storage, tracing config) keep their plain
// converters, which already send nil when absent.

func environmentForUpdate(desired, prior *FunctionEnvironment) *lambdatypes.Environment {
	if desired != nil {
		return desired.to()
	}
	if prior != nil {
		return &lambdatypes.Environment{Variables: map[string]string{}}
	}
	return nil
}

func vpcConfigForUpdate(desired, prior *FunctionVpcConfig) *lambdatypes.VpcConfig {
	if desired != nil {
		return desired.to()
	}
	if prior != nil {
		return &lambdatypes.VpcConfig{
			Ipv6AllowedForDualStack: aws.Bool(false),
			SecurityGroupIds:        []string{},
			SubnetIds:               []string{},
		}
	}
	return nil
}

func deadLetterConfigForUpdate(
	desired, prior *FunctionDeadLetterConfig,
) *lambdatypes.DeadLetterConfig {
	if desired != nil {
		return desired.to()
	}
	if prior != nil {
		return &lambdatypes.DeadLetterConfig{TargetArn: aws.String("")}
	}
	return nil
}

func imageConfigForUpdate(desired, prior *FunctionImageConfig) *lambdatypes.ImageConfig {
	if desired != nil {
		return desired.to()
	}
	if prior != nil {
		return &lambdatypes.ImageConfig{}
	}
	return nil
}

func loggingConfigForUpdate(desired, prior *FunctionLoggingConfig) *lambdatypes.LoggingConfig {
	if desired != nil {
		return desired.to()
	}
	if prior != nil {
		return &lambdatypes.LoggingConfig{}
	}
	return nil
}

func snapStartForUpdate(desired, prior *FunctionSnapStart) *lambdatypes.SnapStart {
	if desired != nil {
		return desired.to()
	}
	if prior != nil {
		return &lambdatypes.SnapStart{ApplyOn: lambdatypes.SnapStartApplyOnNone}
	}
	return nil
}

func fileSystemConfigsForUpdate(
	desired, prior []FunctionFileSystemConfig,
) []lambdatypes.FileSystemConfig {
	if len(desired) > 0 {
		return fileSystemConfigs(desired)
	}
	if len(prior) > 0 {
		return []lambdatypes.FileSystemConfig{}
	}
	return nil
}

func layersForUpdate(desired, prior []string) []string {
	if len(desired) > 0 {
		return desired
	}
	if len(prior) > 0 {
		return []string{}
	}
	return nil
}
