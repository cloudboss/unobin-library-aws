package lambda

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// functionWriteTimeout bounds a create or configuration update that retries
// through a settling dependency. It is generous enough to subsume the role,
// permission, and EC2-throttle windows the call may hit one after another while
// a function's VPC plumbing comes up.
const functionWriteTimeout = 10 * time.Minute

// functionStateTimeout bounds the wait for a function to reach Active after a
// create or to finish an update. Both can take several minutes for a function
// in a VPC, where Lambda provisions network interfaces before it is ready.
const functionStateTimeout = 10 * time.Minute

// Function manages a Lambda function and the configuration Lambda assembles
// around it, the way CloudFormation models AWS::Lambda::Function. The function
// name and package type are fixed at creation, so a change to either replaces
// the function; every other input reconciles in place. The deployment package
// is given in the code block through exactly one source: an inline or on-disk
// zip, an object in S3, or a container image. Most settings ride
// CreateFunction, but reserved concurrency and code signing are separate calls
// reconciled after the function exists, and a published version is a further
// call. SkipDestroy retains the function on delete rather than removing it; it
// is a delete-time switch, not a property of the live function.
type Function struct {
	FunctionName                 string                     `ub:"function-name"`
	Role                         string                     `ub:"role"`
	Code                         FunctionCode               `ub:"code"`
	PackageType                  *string                    `ub:"package-type"`
	Handler                      *string                    `ub:"handler"`
	Runtime                      *string                    `ub:"runtime"`
	Architectures                []string                   `ub:"architectures"`
	Description                  *string                    `ub:"description"`
	MemorySize                   *int64                     `ub:"memory-size"`
	Timeout                      *int64                     `ub:"timeout"`
	Layers                       []string                   `ub:"layers"`
	KMSKeyArn                    *string                    `ub:"kms-key-arn"`
	CodeSigningConfigArn         *string                    `ub:"code-signing-config-arn"`
	ReservedConcurrentExecutions *int64                     `ub:"reserved-concurrent-executions"`
	Publish                      *bool                      `ub:"publish"`
	SkipDestroy                  *bool                      `ub:"skip-destroy"`
	Environment                  *FunctionEnvironment       `ub:"environment"`
	VpcConfig                    *FunctionVpcConfig         `ub:"vpc-config"`
	DeadLetterConfig             *FunctionDeadLetterConfig  `ub:"dead-letter-config"`
	TracingConfig                *FunctionTracingConfig     `ub:"tracing-config"`
	ImageConfig                  *FunctionImageConfig       `ub:"image-config"`
	LoggingConfig                *FunctionLoggingConfig     `ub:"logging-config"`
	SnapStart                    *FunctionSnapStart         `ub:"snap-start"`
	EphemeralStorage             *FunctionEphemeralStorage  `ub:"ephemeral-storage"`
	FileSystemConfigs            []FunctionFileSystemConfig `ub:"file-system-configs"`
	Tags                         map[string]string          `ub:"tags"`
}

// FunctionOutput holds the values Lambda computes for a function. Arn is the
// unqualified function ARN; QualifiedArn and Version name the latest published
// version and settle only after a publish and the version listing. The two
// invoke ARNs are the API Gateway integration targets, composed client-side.
// CodeSha256, SourceCodeSize, and LastModified report the deployed package, and
// the two signing ARNs report a signed package. SnapStartOptimizationStatus
// reports whether a SnapStart snapshot is ready.
type FunctionOutput struct {
	Arn                         string `ub:"arn"`
	QualifiedArn                string `ub:"qualified-arn"`
	Version                     string `ub:"version"`
	InvokeArn                   string `ub:"invoke-arn"`
	QualifiedInvokeArn          string `ub:"qualified-invoke-arn"`
	CodeSha256                  string `ub:"code-sha256"`
	SourceCodeSize              int64  `ub:"source-code-size"`
	LastModified                string `ub:"last-modified"`
	SigningJobArn               string `ub:"signing-job-arn"`
	SigningProfileVersionArn    string `ub:"signing-profile-version-arn"`
	SnapStartOptimizationStatus string `ub:"snap-start-optimization-status"`
}

func (r *Function) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs Lambda fixes when a function is created. The
// name is the function's identity and the package type cannot be switched
// between zip and image on an existing function, so a change to either requires
// a new function. Every other input is reconciled in place by Update.
func (r *Function) ReplaceFields() []string {
	return []string{"function-name", "package-type"}
}

// Defaults marks the collection inputs a function may omit.
func (r Function) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Architectures),
		defaults.Optional(r.Layers),
		defaults.Optional(r.FileSystemConfigs),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules Lambda places on a function's inputs. The
// deployment package comes from exactly one source, so exactly one of the
// code block's four primary source handles is set; the inline and on-disk zip
// forms are mutually exclusive, an S3 source needs both bucket and key, an S3
// object version belongs only to the S3 source, and a source KMS key applies
// only to a zip, not an image. A zip package, which is the default when no
// package type is given, requires a handler and a runtime; an Image package
// requires an image source, and the image config rides only an Image package.
// The package type, tracing mode, logging enums, and snap-start mode each
// accept a fixed set of values, and the memory, timeout, reserved concurrency,
// and ephemeral storage have bounds. The architectures' values and the
// runtime's own large value set are left to the Lambda API to validate.
func (r Function) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.Code.ZipFileContent, r.Code.ZipFilePath,
			r.Code.S3Bucket, r.Code.ImageUri),
		constraint.AtMostOneOf(r.Code.ZipFileContent, r.Code.ZipFilePath),
		constraint.RequiredWith(r.Code.S3Bucket, r.Code.S3Key),
		constraint.RequiredWith(r.Code.S3Key, r.Code.S3Bucket),
		constraint.ForbiddenWith(r.Code.S3ObjectVersion,
			r.Code.ZipFileContent, r.Code.ZipFilePath, r.Code.ImageUri),
		constraint.ForbiddenWith(r.Code.SourceKMSKeyArn, r.Code.ImageUri),
		constraint.When(constraint.Not(constraint.Equals(r.PackageType, "Image"))).
			Require(constraint.Present(r.Handler), constraint.Present(r.Runtime)).
			Message("handler and runtime are required for a Zip package"),
		constraint.When(constraint.Equals(r.PackageType, "Image")).
			Require(constraint.Present(r.Code.ImageUri)).
			Message("an Image package requires code.image-uri"),
		constraint.When(constraint.Present(r.PackageType)).
			Require(constraint.OneOf(r.PackageType, "Zip", "Image")).
			Message("package-type must be Zip or Image"),
		constraint.When(constraint.Present(r.MemorySize)).
			Require(constraint.AtLeast(r.MemorySize, 128),
				constraint.AtMost(r.MemorySize, 32768)).
			Message("memory-size must be between 128 and 32768"),
		constraint.When(constraint.Present(r.Timeout)).
			Require(constraint.AtLeast(r.Timeout, 1), constraint.AtMost(r.Timeout, 900)).
			Message("timeout must be between 1 and 900"),
		constraint.When(constraint.Present(r.ReservedConcurrentExecutions)).
			Require(constraint.AtLeast(r.ReservedConcurrentExecutions, 0)).
			Message("reserved-concurrent-executions must be zero or greater"),
		constraint.When(constraint.Present(r.ImageConfig)).
			Require(constraint.Equals(r.PackageType, "Image")).
			Message("image-config applies only to an Image package"),
		constraint.When(constraint.Present(r.TracingConfig.Mode)).
			Require(constraint.OneOf(r.TracingConfig.Mode, "Active", "PassThrough")).
			Message("tracing-config mode must be Active or PassThrough"),
		constraint.When(constraint.Present(r.LoggingConfig.LogFormat)).
			Require(constraint.OneOf(r.LoggingConfig.LogFormat, "Text", "JSON")).
			Message("logging-config log-format must be Text or JSON"),
		constraint.When(constraint.Present(r.LoggingConfig.ApplicationLogLevel)).
			Require(constraint.OneOf(r.LoggingConfig.ApplicationLogLevel,
				"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL")).
			Message("application-log-level must be TRACE, DEBUG, INFO, WARN, ERROR, or FATAL"),
		constraint.When(constraint.Present(r.LoggingConfig.ApplicationLogLevel)).
			Require(constraint.Equals(r.LoggingConfig.LogFormat, "JSON")).
			Message("application-log-level requires log-format JSON"),
		constraint.When(constraint.Present(r.LoggingConfig.SystemLogLevel)).
			Require(constraint.OneOf(r.LoggingConfig.SystemLogLevel,
				"DEBUG", "INFO", "WARN")).
			Message("system-log-level must be DEBUG, INFO, or WARN"),
		constraint.When(constraint.Present(r.LoggingConfig.SystemLogLevel)).
			Require(constraint.Equals(r.LoggingConfig.LogFormat, "JSON")).
			Message("system-log-level requires log-format JSON"),
		constraint.When(constraint.Present(r.SnapStart.ApplyOn)).
			Require(constraint.OneOf(r.SnapStart.ApplyOn, "None", "PublishedVersions")).
			Message("snap-start apply-on must be None or PublishedVersions"),
		constraint.When(constraint.Present(r.EphemeralStorage.Size)).
			Require(constraint.AtLeast(r.EphemeralStorage.Size, 512),
				constraint.AtMost(r.EphemeralStorage.Size, 10240)).
			Message("ephemeral-storage size must be between 512 and 10240"),
	}
}

func (r *Function) Create(ctx context.Context, cfg any) (*FunctionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in, err := r.createInput()
	if err != nil {
		return nil, err
	}
	// A create is rejected while a dependency the function names is still
	// settling: the execution role not yet assumable, its permissions not yet
	// granted, the VPC throttling EC2, or the environment-variable KMS key not
	// yet usable. Each clears on its own, so retry through them.
	err = retry.OnError(ctx, isFunctionRetryable, func(ctx context.Context) error {
		_, err := client.CreateFunction(ctx, in)
		return err
	}, retry.WithTimeout(functionWriteTimeout))
	if err != nil {
		return nil, fmt.Errorf("create function %s: %w", r.FunctionName, err)
	}
	// A create returns before the function is consistently describable and
	// before it leaves the Pending state, so wait for both: first that a read
	// stops reporting it absent, then that it reaches an active state.
	if err := r.waitDescribable(ctx, client); err != nil {
		return nil, err
	}
	if err := r.waitActive(ctx, client); err != nil {
		return nil, err
	}
	// Reserved concurrency is its own call with no create-time field; set it only
	// when a value was given, leaving the account's unreserved pool in place
	// otherwise. Code signing rides the create input, so it needs no call here.
	if r.ReservedConcurrentExecutions != nil {
		if err := r.putConcurrency(ctx, client); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *Function) Read(
	ctx context.Context, cfg any, prior *FunctionOutput,
) (*FunctionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the function and composes its outputs. A function Lambda reports
// as gone, by a typed not-found or a structurally empty response, maps to
// runtime.ErrNotFound so a plan recreates it. The qualified ARN and version are
// not in the get response; they come from the newest entry of the version
// listing. The code-signing ARN is read only where Signer backs the function.
// The invoke ARNs are composed from the function ARN rather than returned.
func (r *Function) read(ctx context.Context, client *lambda.Client) (*FunctionOutput, error) {
	resp, err := client.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(r.FunctionName),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get function %s: %w", r.FunctionName, err)
	}
	if resp == nil || resp.Configuration == nil || resp.Code == nil {
		return nil, runtime.ErrNotFound
	}
	conf := resp.Configuration
	functionArn := aws.ToString(conf.FunctionArn)
	region := region(client)
	part := partition.Of(region)
	qualifiedArn, version, err := r.latestVersion(ctx, client)
	if err != nil {
		return nil, err
	}
	out := &FunctionOutput{
		Arn:                      functionArn,
		QualifiedArn:             qualifiedArn,
		Version:                  version,
		InvokeArn:                functionInvokeARN(part, region, functionArn),
		QualifiedInvokeArn:       functionInvokeARN(part, region, qualifiedArn),
		CodeSha256:               aws.ToString(conf.CodeSha256),
		SourceCodeSize:           conf.CodeSize,
		LastModified:             aws.ToString(conf.LastModified),
		SigningJobArn:            aws.ToString(conf.SigningJobArn),
		SigningProfileVersionArn: aws.ToString(conf.SigningProfileVersionArn),
	}
	if conf.SnapStart != nil {
		out.SnapStartOptimizationStatus = string(conf.SnapStart.OptimizationStatus)
	}
	return out, nil
}

func (r *Function) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Function, *FunctionOutput],
) (*FunctionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	configChanged := r.configChanged(prior.Inputs)
	codeChanged := r.codeChanged(prior.Inputs)
	if runtime.Changed(prior.Inputs.CodeSigningConfigArn, r.CodeSigningConfigArn) {
		if err := r.reconcileCodeSigning(ctx, client); err != nil {
			return nil, err
		}
	}
	if configChanged {
		if err := r.updateConfiguration(ctx, client, prior.Inputs); err != nil {
			return nil, err
		}
	}
	if codeChanged {
		if err := r.updateCode(ctx, client); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.ReservedConcurrentExecutions,
		r.ReservedConcurrentExecutions) {
		if err := r.reconcileConcurrency(ctx, client); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client, prior.Outputs.Arn); err != nil {
			return nil, err
		}
	}
	publishChanged := runtime.Changed(prior.Inputs.Publish, r.Publish)
	// A version is published only on request, and only when there is something
	// new to capture: a code or configuration change, or the publish flag itself
	// turning on. Publishing on every apply would pile up identical versions.
	if aws.ToBool(r.Publish) && (configChanged || codeChanged || publishChanged) {
		if err := r.publishVersion(ctx, client); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *Function) Delete(ctx context.Context, cfg any, prior *FunctionOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// SkipDestroy retains the function: the resource is forgotten without
	// removing the function from the account, for a function other stacks share
	// or one that must outlive this one.
	if aws.ToBool(r.SkipDestroy) {
		return nil
	}
	_, err = client.DeleteFunction(ctx, &lambda.DeleteFunctionInput{
		FunctionName: aws.String(r.FunctionName),
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete function %s: %w", r.FunctionName, err)
	}
	// Lambda delete is eventually consistent: a get can still find the function
	// for a moment after DeleteFunction returns, and a plan that read it back as
	// present would re-attempt the delete. Wait until it reports gone.
	return r.waitGone(ctx, client)
}

// createInput builds the CreateFunction request from the inputs, assembling the
// code source and every nested block. Tags ride the create. A read error from
// an on-disk zip is returned before any call is made.
func (r *Function) createInput() (*lambda.CreateFunctionInput, error) {
	code, err := r.functionCode()
	if err != nil {
		return nil, err
	}
	in := &lambda.CreateFunctionInput{
		FunctionName:         aws.String(r.FunctionName),
		Role:                 aws.String(r.Role),
		Code:                 code,
		CodeSigningConfigArn: r.CodeSigningConfigArn,
		Description:          r.Description,
		Handler:              r.Handler,
		KMSKeyArn:            r.KMSKeyArn,
		Layers:               r.Layers,
		MemorySize:           ptr.Int32(r.MemorySize),
		Timeout:              ptr.Int32(r.Timeout),
		Publish:              aws.ToBool(r.Publish),
		Architectures:        functionArchitectures(r.Architectures),
		Environment:          r.Environment.to(),
		VpcConfig:            r.VpcConfig.to(),
		DeadLetterConfig:     r.DeadLetterConfig.to(),
		TracingConfig:        r.TracingConfig.to(),
		ImageConfig:          r.ImageConfig.to(),
		LoggingConfig:        r.LoggingConfig.to(),
		SnapStart:            r.SnapStart.to(),
		EphemeralStorage:     r.EphemeralStorage.to(),
		FileSystemConfigs:    fileSystemConfigs(r.FileSystemConfigs),
		Tags:                 r.Tags,
	}
	if r.PackageType != nil {
		in.PackageType = lambdatypes.PackageType(*r.PackageType)
	}
	if r.Runtime != nil {
		in.Runtime = lambdatypes.Runtime(*r.Runtime)
	}
	return in, nil
}

// functionCode assembles the deployment package source from whichever of the
// code block's source fields is present. The constraints guarantee exactly one
// source, so at most one branch contributes. An on-disk zip is read into
// memory here, the one place a source touches the filesystem.
func (r *Function) functionCode() (*lambdatypes.FunctionCode, error) {
	code := &lambdatypes.FunctionCode{
		S3Bucket:        r.Code.S3Bucket,
		S3Key:           r.Code.S3Key,
		S3ObjectVersion: r.Code.S3ObjectVersion,
		ImageUri:        r.Code.ImageUri,
		SourceKMSKeyArn: r.Code.SourceKMSKeyArn,
	}
	switch {
	case r.Code.ZipFileContent != nil:
		code.ZipFile = []byte(*r.Code.ZipFileContent)
	case r.Code.ZipFilePath != nil:
		data, err := os.ReadFile(*r.Code.ZipFilePath)
		if err != nil {
			return nil, fmt.Errorf("read zip file %s: %w", *r.Code.ZipFilePath, err)
		}
		code.ZipFile = data
	}
	return code, nil
}

// updateConfiguration writes the function's configuration, the members
// UpdateFunctionConfiguration accepts, and waits for the update to finish. A
// member that was set before and is now absent is sent as its empty value so
// Lambda clears it rather than leaving the old value in place; the forUpdate
// helpers compare the desired inputs against the prior ones to decide. It
// retries through the same settling dependencies a create does.
func (r *Function) updateConfiguration(
	ctx context.Context, client *lambda.Client, prior Function,
) error {
	in := &lambda.UpdateFunctionConfigurationInput{
		FunctionName:      aws.String(r.FunctionName),
		Role:              aws.String(r.Role),
		Description:       clearableString(r.Description, prior.Description),
		Handler:           clearableString(r.Handler, prior.Handler),
		KMSKeyArn:         clearableString(r.KMSKeyArn, prior.KMSKeyArn),
		Layers:            layersForUpdate(r.Layers, prior.Layers),
		MemorySize:        ptr.Int32(r.MemorySize),
		Timeout:           ptr.Int32(r.Timeout),
		Environment:       environmentForUpdate(r.Environment, prior.Environment),
		VpcConfig:         vpcConfigForUpdate(r.VpcConfig, prior.VpcConfig),
		DeadLetterConfig:  deadLetterConfigForUpdate(r.DeadLetterConfig, prior.DeadLetterConfig),
		TracingConfig:     r.TracingConfig.to(),
		ImageConfig:       imageConfigForUpdate(r.ImageConfig, prior.ImageConfig),
		LoggingConfig:     loggingConfigForUpdate(r.LoggingConfig, prior.LoggingConfig),
		SnapStart:         snapStartForUpdate(r.SnapStart, prior.SnapStart),
		EphemeralStorage:  r.EphemeralStorage.to(),
		FileSystemConfigs: fileSystemConfigsForUpdate(r.FileSystemConfigs, prior.FileSystemConfigs),
	}
	if r.Runtime != nil {
		in.Runtime = lambdatypes.Runtime(*r.Runtime)
	}
	err := retry.OnError(ctx, isFunctionRetryable, func(ctx context.Context) error {
		_, err := client.UpdateFunctionConfiguration(ctx, in)
		return err
	}, retry.WithTimeout(functionWriteTimeout))
	if err != nil {
		return fmt.Errorf("update function configuration %s: %w", r.FunctionName, err)
	}
	return r.waitUpdated(ctx, client)
}

// updateCode writes the deployment package and waits for the update to finish.
// The code source, the architectures, and the source KMS key go through
// UpdateFunctionCode rather than the configuration update.
func (r *Function) updateCode(ctx context.Context, client *lambda.Client) error {
	code, err := r.functionCode()
	if err != nil {
		return err
	}
	in := &lambda.UpdateFunctionCodeInput{
		FunctionName:    aws.String(r.FunctionName),
		Architectures:   functionArchitectures(r.Architectures),
		ZipFile:         code.ZipFile,
		S3Bucket:        code.S3Bucket,
		S3Key:           code.S3Key,
		S3ObjectVersion: code.S3ObjectVersion,
		ImageUri:        code.ImageUri,
		SourceKMSKeyArn: code.SourceKMSKeyArn,
	}
	_, err = client.UpdateFunctionCode(ctx, in)
	if err != nil {
		return fmt.Errorf("update function code %s: %w", r.FunctionName, err)
	}
	return r.waitUpdated(ctx, client)
}

// reconcileConcurrency sets the reserved concurrency to the requested value, or
// removes the reservation when none is given so the function draws from the
// account's unreserved pool again.
func (r *Function) reconcileConcurrency(ctx context.Context, client *lambda.Client) error {
	if r.ReservedConcurrentExecutions == nil {
		_, err := client.DeleteFunctionConcurrency(ctx, &lambda.DeleteFunctionConcurrencyInput{
			FunctionName: aws.String(r.FunctionName),
		})
		if err != nil {
			return fmt.Errorf("delete function concurrency %s: %w", r.FunctionName, err)
		}
		return nil
	}
	return r.putConcurrency(ctx, client)
}

// putConcurrency reserves the requested number of concurrent executions for the
// function.
func (r *Function) putConcurrency(ctx context.Context, client *lambda.Client) error {
	_, err := client.PutFunctionConcurrency(ctx, &lambda.PutFunctionConcurrencyInput{
		FunctionName:                 aws.String(r.FunctionName),
		ReservedConcurrentExecutions: ptr.Int32(r.ReservedConcurrentExecutions),
	})
	if err != nil {
		return fmt.Errorf("put function concurrency %s: %w", r.FunctionName, err)
	}
	return nil
}

// reconcileCodeSigning attaches the requested code-signing configuration to the
// function, or removes it when the input was cleared.
func (r *Function) reconcileCodeSigning(ctx context.Context, client *lambda.Client) error {
	if r.CodeSigningConfigArn == nil {
		_, err := client.DeleteFunctionCodeSigningConfig(ctx,
			&lambda.DeleteFunctionCodeSigningConfigInput{
				FunctionName: aws.String(r.FunctionName),
			})
		if err != nil {
			return fmt.Errorf("delete function code signing config %s: %w", r.FunctionName, err)
		}
		return nil
	}
	_, err := client.PutFunctionCodeSigningConfig(ctx,
		&lambda.PutFunctionCodeSigningConfigInput{
			FunctionName:         aws.String(r.FunctionName),
			CodeSigningConfigArn: r.CodeSigningConfigArn,
		})
	if err != nil {
		return fmt.Errorf("put function code signing config %s: %w", r.FunctionName, err)
	}
	return nil
}

// publishVersion publishes a new immutable version of the function and waits
// for it to finish updating. A publish that races a just-finished code or
// configuration update is rejected as an update in progress, which clears once
// the update settles, so it retries through that.
func (r *Function) publishVersion(ctx context.Context, client *lambda.Client) error {
	err := retry.OnError(ctx, isPublishInProgress, func(ctx context.Context) error {
		_, err := client.PublishVersion(ctx, &lambda.PublishVersionInput{
			FunctionName: aws.String(r.FunctionName),
		})
		return err
	}, retry.WithTimeout(5*time.Minute))
	if err != nil {
		return fmt.Errorf("publish function version %s: %w", r.FunctionName, err)
	}
	return r.waitUpdated(ctx, client)
}

// syncTags reconciles the function's tags with the desired set, reading the
// live tags and writing changes with TagResource and UntagResource. Lambda
// addresses function tags by the function ARN.
func (r *Function) syncTags(ctx context.Context, client *lambda.Client, arn string) error {
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTags(ctx, &lambda.ListTagsInput{Resource: aws.String(arn)})
			if err != nil {
				return nil, fmt.Errorf("list tags %s: %w", arn, err)
			}
			return resp.Tags, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &lambda.TagResourceInput{
				Resource: aws.String(arn),
				Tags:     upsert,
			})
			if err != nil {
				return fmt.Errorf("tag resource %s: %w", arn, err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &lambda.UntagResourceInput{
				Resource: aws.String(arn),
				TagKeys:  remove,
			})
			if err != nil {
				return fmt.Errorf("untag resource %s: %w", arn, err)
			}
			return nil
		},
	)
}

// latestVersion returns the qualified ARN and version of the function's newest
// published version, paging ListVersionsByFunction to its end and taking the
// last entry, which Lambda orders oldest to newest. With no published version
// the listing holds only $LATEST, whose ARN and version are returned, matching
// the unqualified function.
func (r *Function) latestVersion(
	ctx context.Context, client *lambda.Client,
) (qualifiedArn, version string, err error) {
	pager := lambda.NewListVersionsByFunctionPaginator(client,
		&lambda.ListVersionsByFunctionInput{FunctionName: aws.String(r.FunctionName)})
	var latest *lambdatypes.FunctionConfiguration
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return "", "", fmt.Errorf("list versions %s: %w", r.FunctionName, err)
		}
		if n := len(page.Versions); n > 0 {
			latest = &page.Versions[n-1]
		}
	}
	if latest == nil {
		return "", "", nil
	}
	return aws.ToString(latest.FunctionArn), aws.ToString(latest.Version), nil
}

// configChanged reports whether any input that UpdateFunctionConfiguration
// sends differs from the prior inputs.
func (r *Function) configChanged(prior Function) bool {
	return runtime.Changed(prior.Role, r.Role) ||
		runtime.Changed(prior.Description, r.Description) ||
		runtime.Changed(prior.Handler, r.Handler) ||
		runtime.Changed(prior.Runtime, r.Runtime) ||
		runtime.Changed(prior.MemorySize, r.MemorySize) ||
		runtime.Changed(prior.Timeout, r.Timeout) ||
		runtime.Changed(prior.Layers, r.Layers) ||
		runtime.Changed(prior.KMSKeyArn, r.KMSKeyArn) ||
		runtime.Changed(prior.Environment, r.Environment) ||
		runtime.Changed(prior.VpcConfig, r.VpcConfig) ||
		runtime.Changed(prior.DeadLetterConfig, r.DeadLetterConfig) ||
		runtime.Changed(prior.TracingConfig, r.TracingConfig) ||
		runtime.Changed(prior.ImageConfig, r.ImageConfig) ||
		runtime.Changed(prior.LoggingConfig, r.LoggingConfig) ||
		runtime.Changed(prior.SnapStart, r.SnapStart) ||
		runtime.Changed(prior.EphemeralStorage, r.EphemeralStorage) ||
		runtime.Changed(prior.FileSystemConfigs, r.FileSystemConfigs)
}

// codeChanged reports whether any input that UpdateFunctionCode sends differs
// from the prior inputs: the code block or the architectures.
func (r *Function) codeChanged(prior Function) bool {
	return runtime.Changed(prior.Code, r.Code) ||
		runtime.Changed(prior.Architectures, r.Architectures)
}

// waitDescribable polls GetFunction until it stops reporting the function
// absent, for the window after a create where the function is not yet
// consistently readable.
func (r *Function) waitDescribable(ctx context.Context, client *lambda.Client) error {
	return wait.Until(ctx, fmt.Sprintf("function %s", r.FunctionName),
		func(ctx context.Context) (bool, error) {
			_, err := client.GetFunction(ctx, &lambda.GetFunctionInput{
				FunctionName: aws.String(r.FunctionName),
			})
			if err != nil {
				if isNotFound(err) {
					return false, nil
				}
				return false, fmt.Errorf("get function %s: %w", r.FunctionName, err)
			}
			return true, nil
		},
		wait.WithInterval(time.Second),
		wait.WithTimeout(functionStateTimeout),
	)
}

// waitActive polls GetFunction until the function leaves the Pending state for
// an active one. A function stuck in Failed stops the wait with an error rather
// than spinning until the timeout.
func (r *Function) waitActive(ctx context.Context, client *lambda.Client) error {
	return wait.Until(ctx, fmt.Sprintf("function %s to become active", r.FunctionName),
		func(ctx context.Context) (bool, error) {
			conf, err := r.getConfiguration(ctx, client)
			if err != nil {
				return false, err
			}
			switch conf.State {
			case lambdatypes.StateActive, lambdatypes.StateActiveNonInvocable:
				return true, nil
			case lambdatypes.StateFailed:
				return false, fmt.Errorf("function %s entered failed state: %s",
					r.FunctionName, aws.ToString(conf.StateReason))
			default:
				return false, nil
			}
		},
		wait.WithInterval(time.Second),
		wait.WithTimeout(functionStateTimeout),
	)
}

// waitUpdated polls GetFunction until the last update settles. An update that
// fails stops the wait with an error rather than spinning until the timeout.
func (r *Function) waitUpdated(ctx context.Context, client *lambda.Client) error {
	return wait.Until(ctx, fmt.Sprintf("function %s update", r.FunctionName),
		func(ctx context.Context) (bool, error) {
			conf, err := r.getConfiguration(ctx, client)
			if err != nil {
				return false, err
			}
			switch conf.LastUpdateStatus {
			case lambdatypes.LastUpdateStatusSuccessful:
				return true, nil
			case lambdatypes.LastUpdateStatusFailed:
				return false, fmt.Errorf("function %s update failed: %s",
					r.FunctionName, aws.ToString(conf.LastUpdateStatusReason))
			default:
				return false, nil
			}
		},
		wait.WithInterval(time.Second),
		wait.WithTimeout(functionStateTimeout),
	)
}

// waitGone polls GetFunction until it reports the function gone after a delete.
func (r *Function) waitGone(ctx context.Context, client *lambda.Client) error {
	return wait.Until(ctx, fmt.Sprintf("function %s to be gone", r.FunctionName),
		func(ctx context.Context) (bool, error) {
			_, err := client.GetFunction(ctx, &lambda.GetFunctionInput{
				FunctionName: aws.String(r.FunctionName),
			})
			if err != nil {
				if isNotFound(err) {
					return true, nil
				}
				return false, fmt.Errorf("get function %s: %w", r.FunctionName, err)
			}
			return false, nil
		},
		wait.WithInterval(time.Second),
		wait.WithTimeout(functionStateTimeout),
	)
}

// getConfiguration reads the function's configuration for a wait probe, the
// State and LastUpdateStatus the waits poll. A function-not-found while waiting
// for it to settle is unexpected and returned as an error.
func (r *Function) getConfiguration(
	ctx context.Context, client *lambda.Client,
) (*lambdatypes.FunctionConfiguration, error) {
	resp, err := client.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(r.FunctionName),
	})
	if err != nil {
		return nil, fmt.Errorf("get function %s: %w", r.FunctionName, err)
	}
	if resp == nil || resp.Configuration == nil {
		return nil, fmt.Errorf("get function %s: empty response", r.FunctionName)
	}
	return resp.Configuration, nil
}

// functionInvokeARN composes the API Gateway integration target for a function
// ARN. The form is fixed by API Gateway, naming the apigateway service in the
// function's region and embedding the function ARN; the qualified variant uses
// the version-qualified function ARN. An empty function ARN yields an empty
// invoke ARN rather than a malformed one.
func functionInvokeARN(part, region, functionArn string) string {
	if functionArn == "" {
		return ""
	}
	return fmt.Sprintf(
		"arn:%s:apigateway:%s:lambda:path/2015-03-31/functions/%s/invocations",
		part, region, functionArn)
}

// functionArchitectures converts the desired architecture names to the SDK
// list. An empty input leaves the member unset, so Lambda applies its default.
func functionArchitectures(names []string) []lambdatypes.Architecture {
	if len(names) == 0 {
		return nil
	}
	out := make([]lambdatypes.Architecture, 0, len(names))
	for _, n := range names {
		out = append(out, lambdatypes.Architecture(n))
	}
	return out
}
