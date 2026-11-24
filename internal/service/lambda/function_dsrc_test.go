package lambda

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

type lookupCall struct {
	operation string
	input     any
}
type functionLookupFake struct {
	calls       []lookupCall
	pages       []*sdk.ListVersionsByFunctionOutput
	result      *sdk.GetFunctionOutput
	concurrency *sdk.GetFunctionConcurrencyOutput
	signing     *sdk.GetFunctionCodeSigningConfigOutput
	fail        string
	err         error
	afterList   func()
}

func (f *functionLookupFake) ListVersionsByFunction(
	ctx context.Context, in *sdk.ListVersionsByFunctionInput, _ ...func(*sdk.Options),
) (*sdk.ListVersionsByFunctionOutput, error) {
	f.calls = append(f.calls, lookupCall{"list", *in})
	if f.fail == "list" {
		return nil, f.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	page := f.pages[0]
	f.pages = f.pages[1:]
	if f.afterList != nil {
		f.afterList()
	}
	return page, nil
}
func (f *functionLookupFake) GetFunction(
	ctx context.Context, in *sdk.GetFunctionInput, _ ...func(*sdk.Options),
) (*sdk.GetFunctionOutput, error) {
	f.calls = append(f.calls, lookupCall{"get", *in})
	if f.fail == "get" {
		return nil, f.err
	}
	return f.result, ctx.Err()
}
func (f *functionLookupFake) GetFunctionConcurrency(
	ctx context.Context, in *sdk.GetFunctionConcurrencyInput, _ ...func(*sdk.Options),
) (*sdk.GetFunctionConcurrencyOutput, error) {
	f.calls = append(f.calls, lookupCall{"concurrency", *in})
	if f.fail == "concurrency" {
		return nil, f.err
	}
	return f.concurrency, ctx.Err()
}
func (f *functionLookupFake) GetFunctionCodeSigningConfig(
	ctx context.Context, in *sdk.GetFunctionCodeSigningConfigInput, _ ...func(*sdk.Options),
) (*sdk.GetFunctionCodeSigningConfigOutput, error) {
	f.calls = append(f.calls, lookupCall{"signing", *in})
	if f.fail == "signing" {
		return nil, f.err
	}
	return f.signing, ctx.Err()
}
func emptyLookupResult() *sdk.GetFunctionOutput {
	return &sdk.GetFunctionOutput{Code: &types.FunctionCodeLocation{},
		Configuration: &types.FunctionConfiguration{}}
}
func TestFunctionLookupSelection(t *testing.T) {
	f := &functionLookupFake{
		pages: []*sdk.ListVersionsByFunctionOutput{
			{Versions: []types.FunctionConfiguration{{Version: aws.String("99")}},
				NextMarker: aws.String("p")},
			{Versions: []types.FunctionConfiguration{{Version: aws.String("3")},
				{Version: aws.String("2")}}},
		}, result: emptyLookupResult(), concurrency: &sdk.GetFunctionConcurrencyOutput{},
	}
	got, err := (&FunctionDataSource{FunctionName: " original:alias "}).read(t.Context(), f,
		"cn-north-1")
	require.NoError(t, err)
	require.Equal(t, int64(0), got.ReservedConcurrentExecutions)
	require.Equal(t, []lookupCall{
		{"list", sdk.ListVersionsByFunctionInput{
			FunctionName: aws.String(" original:alias "), MaxItems: aws.Int32(10000)}},
		{"list", sdk.ListVersionsByFunctionInput{
			FunctionName: aws.String(" original:alias "), MaxItems: aws.Int32(10000),
			Marker: aws.String("p")}},
		{"get", sdk.GetFunctionInput{FunctionName: aws.String(" original:alias "),
			Qualifier: aws.String("2")}},
		{"concurrency", sdk.GetFunctionConcurrencyInput{FunctionName: aws.String(" original:alias ")}},
	}, f.calls)
}

func TestFunctionLookupQualifiersAndSelectors(t *testing.T) {
	for _, selector := range []string{"", "name", "arn:aws:lambda:elsewhere:1:function:n:alias",
		"  name:alias  ", "λ/odd!", string(make([]byte, 200))} {
		for _, qualifier := range []*string{nil, aws.String(""), aws.String("$LATEST"),
			aws.String("3"), aws.String(" live λ ")} {
			f := &functionLookupFake{result: emptyLookupResult(),
				pages: []*sdk.ListVersionsByFunctionOutput{{Versions: []types.FunctionConfiguration{
					{Version: aws.String("$LATEST")}}}}, concurrency: &sdk.GetFunctionConcurrencyOutput{}}
			_, err := (&FunctionDataSource{FunctionName: selector, Qualifier: qualifier}).read(
				t.Context(), f, "cn-north-1")
			require.NoError(t, err)
			effective := qualifier
			var want []lookupCall
			if aws.ToString(qualifier) == "" {
				effective = nil
				want = append(want, lookupCall{"list", sdk.ListVersionsByFunctionInput{
					FunctionName: aws.String(selector), MaxItems: aws.Int32(10000)}})
			}
			want = append(want, lookupCall{"get", sdk.GetFunctionInput{
				FunctionName: aws.String(selector), Qualifier: effective}})
			if effective != nil {
				want = append(want, lookupCall{"concurrency", sdk.GetFunctionConcurrencyInput{
					FunctionName: aws.String(selector)}})
			}
			require.Equal(t, want, f.calls)
		}
	}
	for _, version := range []*string{nil, aws.String(""), aws.String("$LATEST"), aws.String("17")} {
		f := &functionLookupFake{result: emptyLookupResult(),
			pages: []*sdk.ListVersionsByFunctionOutput{{Versions: []types.FunctionConfiguration{
				{Version: version}}}}, concurrency: &sdk.GetFunctionConcurrencyOutput{}}
		_, err := (&FunctionDataSource{FunctionName: "n"}).read(t.Context(), f, "cn-north-1")
		require.NoError(t, err)
		var expected *string
		if aws.ToString(version) != "$LATEST" {
			expected = aws.String(aws.ToString(version))
		}
		require.Equal(t, lookupCall{"get", sdk.GetFunctionInput{
			FunctionName: aws.String("n"), Qualifier: expected}}, f.calls[1])
	}
}

func TestFunctionLookupEmptyPagesAndResults(t *testing.T) {
	for _, terminal := range []*sdk.ListVersionsByFunctionOutput{
		nil, {}, {NextMarker: aws.String(""),
			Versions: []types.FunctionConfiguration{{Version: aws.String("4")}}},
	} {
		f := &functionLookupFake{pages: []*sdk.ListVersionsByFunctionOutput{
			{NextMarker: aws.String("next"),
				Versions: []types.FunctionConfiguration{{Version: aws.String("1")}}},
			terminal,
		}}
		got, err := (&FunctionDataSource{FunctionName: "n"}).read(t.Context(), f, "us-east-1")
		require.ErrorContains(t, err, `list function versions "n": empty`)
		require.Nil(t, got)
		require.Equal(t, []lookupCall{
			{"list", sdk.ListVersionsByFunctionInput{FunctionName: aws.String("n"),
				MaxItems: aws.Int32(10000)}},
			{"list", sdk.ListVersionsByFunctionInput{FunctionName: aws.String("n"),
				MaxItems: aws.Int32(10000), Marker: aws.String("next")}},
		}, f.calls)
	}
	for _, result := range []*sdk.GetFunctionOutput{nil, {}, {Code: &types.FunctionCodeLocation{}},
		{Configuration: &types.FunctionConfiguration{}}} {
		f := &functionLookupFake{result: result}
		got, err := (&FunctionDataSource{FunctionName: "n", Qualifier: aws.String("live")}).read(
			t.Context(), f, "us-east-1")
		require.ErrorContains(t, err, `get function "n": empty result`)
		require.Nil(t, got)
		require.Equal(t, []lookupCall{{"get", sdk.GetFunctionInput{
			FunctionName: aws.String("n"), Qualifier: aws.String("live")}}}, f.calls)
	}
}

func TestFunctionLookupConcurrency(t *testing.T) {
	for _, tc := range []struct {
		name      string
		qualifier *string
		main      *types.Concurrency
		aux       *sdk.GetFunctionConcurrencyOutput
		want      int64
		fallback  bool
	}{
		{name: "unqualified", want: -1},
		{name: "main nil count", main: &types.Concurrency{}, want: 0},
		{name: "main zero", qualifier: aws.String("x"), main: &types.Concurrency{
			ReservedConcurrentExecutions: aws.Int32(0)}, want: 0},
		{name: "main positive", qualifier: aws.String("x"), main: &types.Concurrency{
			ReservedConcurrentExecutions: aws.Int32(7)}, want: 7},
		{name: "fallback nil count", qualifier: aws.String("x"),
			aux: &sdk.GetFunctionConcurrencyOutput{}, fallback: true},
		{name: "fallback positive", qualifier: aws.String("x"),
			aux:  &sdk.GetFunctionConcurrencyOutput{ReservedConcurrentExecutions: aws.Int32(8)},
			want: 8, fallback: true},
		{name: "fallback empty", qualifier: aws.String("x"), fallback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &functionLookupFake{result: emptyLookupResult(), concurrency: tc.aux,
				pages: []*sdk.ListVersionsByFunctionOutput{{Versions: []types.FunctionConfiguration{
					{Version: aws.String("$LATEST")}}}}}
			f.result.Concurrency = tc.main
			got, err := (&FunctionDataSource{FunctionName: "n:embedded", Qualifier: tc.qualifier}).read(
				t.Context(), f, "us-east-1")
			if tc.fallback && tc.aux == nil {
				require.ErrorContains(t, err, "concurrency")
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got.ReservedConcurrentExecutions)
			}
			var want []lookupCall
			if tc.qualifier == nil {
				want = append(want, lookupCall{"list", sdk.ListVersionsByFunctionInput{
					FunctionName: aws.String("n:embedded"), MaxItems: aws.Int32(10000)}})
			}
			want = append(want, lookupCall{"get", sdk.GetFunctionInput{
				FunctionName: aws.String("n:embedded"), Qualifier: tc.qualifier}})
			if tc.fallback {
				want = append(want, lookupCall{"concurrency", sdk.GetFunctionConcurrencyInput{
					FunctionName: aws.String("n:embedded")}})
			}
			require.Equal(t, want, f.calls)
		})
	}
}

func TestFunctionLookupARNs(t *testing.T) {
	for _, tc := range []struct{ raw, qualifier, version, arn, qualified string }{
		{"f", "live", "7", "f", "f:7"},
		{"f:live", "live", "7", "f", "f:live"},
		{"f:7", "live", "7", "f:7", "f:7"},
		{"f:other", "live", "7", "f:other", "f:other:7"},
		{"", "live", "", "", ":"},
		{"f:", "live", "", "f:", "f:"},
		{"f:live:live", "live", "7", "f:live", "f:live:live"},
	} {
		f := &functionLookupFake{result: emptyLookupResult()}
		f.result.Configuration.FunctionArn = aws.String(tc.raw)
		f.result.Configuration.Version = aws.String(tc.version)
		f.result.Concurrency = &types.Concurrency{}
		got, err := (&FunctionDataSource{FunctionName: "arn:aws:lambda:us-west-1:1:function:selector",
			Qualifier: aws.String(tc.qualifier)}).read(t.Context(), f, "us-gov-west-1")
		require.NoError(t, err)
		require.Equal(t, tc.arn, got.Arn)
		require.Equal(t, tc.qualified, got.QualifiedArn)
		prefix := "arn:aws-us-gov:apigateway:us-gov-west-1:lambda:path/"
		require.Equal(t, prefix+"2015-03-31/functions/"+tc.arn+"/invocations", got.InvokeArn)
		require.Equal(t, prefix+"2015-03-31/functions/"+tc.qualified+"/invocations",
			got.QualifiedInvokeArn)
		require.Equal(t, prefix+"2021-11-15/functions/"+tc.raw+"/response-streaming-invocations",
			got.ResponseStreamingInvokeArn)
	}
}

func TestFunctionLookupSigningGates(t *testing.T) {
	allowed := []string{"us-east-1", "us-east-2", "us-west-1", "us-west-2", "af-south-1", "ap-east-1",
		"ap-south-1", "ap-northeast-2", "ap-southeast-1", "ap-southeast-2", "ap-northeast-1",
		"ca-central-1", "eu-central-1", "eu-west-1", "eu-west-2", "eu-south-1", "eu-west-3",
		"eu-north-1", "me-south-1", "sa-east-1", "us-gov-east-1", "us-gov-west-1"}
	for _, region := range append(allowed, "ap-south-2", "eu-central-2", "cn-north-1",
		"us-iso-east-1", "us-isob-east-1", "eusc-de-east-1", "") {
		for _, pkg := range []types.PackageType{types.PackageTypeZip, types.PackageTypeImage,
			"unknown", ""} {
			for _, signing := range []*sdk.GetFunctionCodeSigningConfigOutput{
				nil, {}, {CodeSigningConfigArn: aws.String("signing")},
			} {
				f := &functionLookupFake{result: emptyLookupResult(), signing: signing}
				f.result.Concurrency = &types.Concurrency{}
				f.result.Configuration.PackageType = pkg
				got, err := (&FunctionDataSource{FunctionName: "n:alias",
					Qualifier: aws.String("$LATEST")}).read(
					t.Context(), f, region)
				require.NoError(t, err)
				want := []lookupCall{{"get", sdk.GetFunctionInput{
					FunctionName: aws.String("n:alias"), Qualifier: aws.String("$LATEST")}}}
				expected := ""
				if slices.Contains(allowed, region) && pkg == types.PackageTypeZip {
					want = append(want, lookupCall{"signing", sdk.GetFunctionCodeSigningConfigInput{
						FunctionName: aws.String("n:alias")}})
					if signing != nil {
						expected = aws.ToString(signing.CodeSigningConfigArn)
					}
				}
				require.Equal(t, want, f.calls)
				require.Equal(t, expected, got.CodeSigningConfigArn)
			}
		}
	}
}

func TestFunctionLookupFailures(t *testing.T) {
	for _, operation := range []string{"list", "get", "concurrency", "signing"} {
		for _, cause := range []error{errors.New("transport"), context.Canceled,
			&types.ResourceNotFoundException{Message: aws.String("gone")}} {
			f := &functionLookupFake{fail: operation, err: cause, result: emptyLookupResult(),
				concurrency: &sdk.GetFunctionConcurrencyOutput{},
				pages: []*sdk.ListVersionsByFunctionOutput{{Versions: []types.FunctionConfiguration{
					{Version: aws.String("7")}}}}}
			f.result.Configuration.PackageType = types.PackageTypeZip
			got, err := (&FunctionDataSource{FunctionName: "n"}).read(t.Context(), f, "us-east-1")
			require.ErrorIs(t, err, cause)
			require.ErrorContains(t, err, `"n"`)
			require.NotErrorIs(t, err, runtime.ErrNotFound)
			require.Nil(t, got)
			want := []lookupCall{
				{"list", sdk.ListVersionsByFunctionInput{FunctionName: aws.String("n"),
					MaxItems: aws.Int32(10000)}},
			}
			if operation != "list" {
				want = append(want, lookupCall{"get", sdk.GetFunctionInput{
					FunctionName: aws.String("n"), Qualifier: aws.String("7")}})
			}
			if operation == "concurrency" || operation == "signing" {
				want = append(want, lookupCall{"concurrency", sdk.GetFunctionConcurrencyInput{
					FunctionName: aws.String("n")}})
			}
			if operation == "signing" {
				want = append(want, lookupCall{"signing", sdk.GetFunctionCodeSigningConfigInput{
					FunctionName: aws.String("n")}})
			}
			require.Equal(t, want, f.calls)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	f := &functionLookupFake{pages: []*sdk.ListVersionsByFunctionOutput{{NextMarker: aws.String("p")}},
		afterList: cancel}
	got, err := (&FunctionDataSource{FunctionName: "n"}).read(ctx, f, "us-east-1")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
	require.Equal(t, []lookupCall{
		{"list", sdk.ListVersionsByFunctionInput{FunctionName: aws.String("n"),
			MaxItems: aws.Int32(10000)}},
		{"list", sdk.ListVersionsByFunctionInput{FunctionName: aws.String("n"),
			MaxItems: aws.Int32(10000), Marker: aws.String("p")}},
	}, f.calls)
}

func TestFunctionLookupCompleteOutputs(t *testing.T) {
	managed := &types.LambdaManagedInstancesCapacityProviderConfig{
		CapacityProviderArn:                   aws.String("capacity"),
		ExecutionEnvironmentMemoryGiBPerVCpu:  aws.Float64(1.75),
		PerExecutionEnvironmentMaxConcurrency: aws.Int32(9),
	}
	result := emptyLookupResult()
	result.Code = &types.FunctionCodeLocation{ImageUri: aws.String("image:tag"),
		ResolvedImageUri: aws.String("image@resolved"), SourceKMSKeyArn: aws.String("source-kms")}
	result.Tags = map[string]string{"aws:hidden": "x", "AWS:retained": "", "ordinary": "yes"}
	result.TagsError = &types.TagsError{ErrorCode: aws.String("ignored")}
	result.Concurrency = &types.Concurrency{ReservedConcurrentExecutions: aws.Int32(42)}
	result.Configuration = &types.FunctionConfiguration{
		FunctionArn: aws.String("raw:live"), Version: aws.String("8"), CodeSha256: aws.String("hash!?"),
		CodeSize: 1 << 42, Description: aws.String("desc"), Handler: aws.String("handler"),
		KMSKeyArn: aws.String("kms"), LastModified: aws.String("not a timestamp"),
		MemorySize: aws.Int32(192), Role: aws.String("role"), Runtime: "future-runtime",
		SigningJobArn: aws.String("job"), SigningProfileVersionArn: aws.String("profile"),
		Timeout: aws.Int32(30), Architectures: []types.Architecture{"future", "arm64", "future"},
		Layers: []types.Layer{{Arn: aws.String("second")}, {}, {Arn: aws.String("first")}},
		CapacityProviderConfig: &types.CapacityProviderConfig{
			LambdaManagedInstancesCapacityProviderConfig: managed},
		DeadLetterConfig: &types.DeadLetterConfig{TargetArn: aws.String("")},
		DurableConfig: &types.DurableConfig{ExecutionTimeout: aws.Int32(55),
			RetentionPeriodInDays: aws.Int32(4)},
		Environment: &types.EnvironmentResponse{Variables: map[string]string{"x": "value"},
			Error: &types.EnvironmentError{ErrorCode: aws.String("ignored")}},
		EphemeralStorage: &types.EphemeralStorage{Size: aws.Int32(1024)},
		FileSystemConfigs: []types.FileSystemConfig{
			{Arn: aws.String("second"), LocalMountPath: aws.String("/mnt/second")}, {},
			{Arn: aws.String("first"), LocalMountPath: aws.String("/mnt/first")}},
		LoggingConfig: &types.LoggingConfig{ApplicationLogLevel: "custom", LogFormat: "custom-format",
			LogGroup: aws.String("group"), SystemLogLevel: "other"},
		TenancyConfig: &types.TenancyConfig{TenantIsolationMode: "future-isolation"},
		TracingConfig: &types.TracingConfigResponse{Mode: "future-tracing"},
		VpcConfig: &types.VpcConfigResponse{Ipv6AllowedForDualStack: aws.Bool(true),
			SubnetIds: []string{"b", "a", "b"}, SecurityGroupIds: []string{"z", "z", "x"},
			VpcId: aws.String("vpc")},
	}
	got, err := (&FunctionDataSource{FunctionName: "selector", Qualifier: aws.String("live")}).read(
		t.Context(), &functionLookupFake{result: result}, "cn-north-1")
	require.NoError(t, err)
	prefix := "arn:aws-cn:apigateway:cn-north-1:lambda:path/"
	require.Equal(t, &FunctionDataSourceOutput{
		Arn: "raw", QualifiedArn: "raw:live",
		InvokeArn:          prefix + "2015-03-31/functions/raw/invocations",
		QualifiedInvokeArn: prefix + "2015-03-31/functions/raw:live/invocations",
		ResponseStreamingInvokeArn: prefix +
			"2021-11-15/functions/raw:live/response-streaming-invocations",
		CodeSha256: "hash!?", SourceCodeHash: "hash!?", Description: "desc",
		Handler:  "handler",
		ImageUri: "image:tag", KMSKeyArn: "kms", LastModified: "not a timestamp", MemorySize: 192,
		ReservedConcurrentExecutions: 42, Role: "role", Runtime: "future-runtime", SigningJobArn: "job",
		SigningProfileVersionArn: "profile", SourceCodeSize: 1 << 42, SourceKMSKeyArn: "source-kms",
		Timeout: 30, Version: "8", Architectures: []string{"future", "arm64", "future"},
		Layers: []string{"second", "", "first"}, Tags: map[string]string{"AWS:retained": "",
			"ordinary": "yes"},
		CapacityProviderConfig: &functionLookupCapacity{Managed: functionLookupManaged{
			CapacityProviderArn: "capacity", Memory: 1.75, MaxConcurrency: 9}},
		DeadLetterConfig: &functionLookupDeadLetter{TargetArn: ""},
		DurableConfig:    &functionLookupDurable{ExecutionTimeout: 55, RetentionPeriodInDays: 4},
		Environment:      &FunctionEnvironment{Variables: map[string]string{"x": "value"}},
		EphemeralStorage: &functionLookupStorage{Size: 1024},
		FileSystemConfigs: []FunctionFileSystemConfig{
			{Arn: "second", LocalMountPath: "/mnt/second"}, {}, {Arn: "first",
				LocalMountPath: "/mnt/first"}},
		LoggingConfig: &functionLookupLogging{ApplicationLogLevel: "custom", LogFormat: "custom-format",
			LogGroup: "group", SystemLogLevel: "other"},
		TenancyConfig: &functionLookupTenancy{TenantIsolationMode: "future-isolation"},
		TracingConfig: functionLookupTracing{Mode: "future-tracing"},
		VpcConfig: &functionLookupVPC{Ipv6AllowedForDualStack: true,
			SubnetIds: []string{"a", "b"}, SecurityGroupIds: []string{"x", "z"}, VpcID: "vpc"},
	}, got)
}

func TestFunctionLookupNestedPresence(t *testing.T) {
	emptyManaged := &types.LambdaManagedInstancesCapacityProviderConfig{}
	for _, tc := range []struct {
		name     string
		config   types.FunctionConfiguration
		capacity *functionLookupCapacity
		dead     *functionLookupDeadLetter
		durable  *functionLookupDurable
		env      *FunctionEnvironment
		storage  *functionLookupStorage
		logging  *functionLookupLogging
		tenancy  *functionLookupTenancy
		tracing  functionLookupTracing
		vpc      *functionLookupVPC
	}{
		{name: "absent", tracing: functionLookupTracing{Mode: "PassThrough"}},
		{name: "empty", config: types.FunctionConfiguration{
			CapacityProviderConfig: &types.CapacityProviderConfig{},
			DeadLetterConfig:       &types.DeadLetterConfig{},
			DurableConfig:          &types.DurableConfig{}, Environment: &types.EnvironmentResponse{},
			EphemeralStorage: &types.EphemeralStorage{}, LoggingConfig: &types.LoggingConfig{},
			TenancyConfig: &types.TenancyConfig{}, TracingConfig: &types.TracingConfigResponse{},
			VpcConfig: &types.VpcConfigResponse{Ipv6AllowedForDualStack: aws.Bool(true)},
		}, capacity: &functionLookupCapacity{}, env: &FunctionEnvironment{Variables: map[string]string{}},
			storage: &functionLookupStorage{}, logging: &functionLookupLogging{},
			tenancy: &functionLookupTenancy{}},
		{name: "partial", config: types.FunctionConfiguration{
			CapacityProviderConfig: &types.CapacityProviderConfig{
				LambdaManagedInstancesCapacityProviderConfig: emptyManaged},
			DeadLetterConfig: &types.DeadLetterConfig{TargetArn: aws.String("")},
			DurableConfig:    &types.DurableConfig{RetentionPeriodInDays: aws.Int32(0)},
			VpcConfig:        &types.VpcConfigResponse{SubnetIds: []string{"only"}},
		}, capacity: &functionLookupCapacity{}, dead: &functionLookupDeadLetter{},
			durable: &functionLookupDurable{}, tracing: functionLookupTracing{Mode: "PassThrough"},
			vpc: &functionLookupVPC{SubnetIds: []string{"only"}, SecurityGroupIds: []string{}}},
		{name: "duration only", config: types.FunctionConfiguration{
			DurableConfig: &types.DurableConfig{ExecutionTimeout: aws.Int32(12)},
			VpcConfig:     &types.VpcConfigResponse{VpcId: aws.String("vpc")},
		}, durable: &functionLookupDurable{ExecutionTimeout: 12},
			tracing: functionLookupTracing{Mode: "PassThrough"},
			vpc:     &functionLookupVPC{VpcID: "vpc", SubnetIds: []string{}, SecurityGroupIds: []string{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := emptyLookupResult()
			result.Configuration = &tc.config
			got, err := (&FunctionDataSource{FunctionName: "n", Qualifier: aws.String("$LATEST")}).read(
				t.Context(), &functionLookupFake{result: result,
					concurrency: &sdk.GetFunctionConcurrencyOutput{}},
				"cn-north-1")
			require.NoError(t, err)
			prefix := "arn:aws-cn:apigateway:cn-north-1:lambda:path/"
			require.Equal(t, &FunctionDataSourceOutput{
				QualifiedArn: ":", InvokeArn: prefix + "2015-03-31/functions//invocations",
				QualifiedInvokeArn:         prefix + "2015-03-31/functions/:/invocations",
				ResponseStreamingInvokeArn: prefix + "2021-11-15/functions//response-streaming-invocations",
				Architectures:              []string{}, Layers: []string{}, Tags: map[string]string{},
				FileSystemConfigs: []FunctionFileSystemConfig{}, CapacityProviderConfig: tc.capacity,
				DeadLetterConfig: tc.dead, DurableConfig: tc.durable, Environment: tc.env,
				EphemeralStorage: tc.storage, LoggingConfig: tc.logging, TenancyConfig: tc.tenancy,
				TracingConfig: tc.tracing, VpcConfig: tc.vpc,
			}, got)
		})
	}
}
