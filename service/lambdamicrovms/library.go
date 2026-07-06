package lambdamicrovms

import (
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/config"
	svc "github.com/cloudboss/unobin-library-aws/internal/service/lambdamicrovms"
)

type resourcePtr[T, Out any] interface {
	*T
	runtime.TypedResource[T, Out, *awscfg.Configuration]
}

type dataSourcePtr[T, Out any] interface {
	*T
	runtime.TypedDataSource[Out, *awscfg.Configuration]
}

type actionPtr[T, Out any] interface {
	*T
	runtime.TypedAction[Out, *awscfg.Configuration]
}

func makeResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return runtime.MakeResource[T, Out, *awscfg.Configuration, PT]()
}

func makeDataSource[T, Out any, PT dataSourcePtr[T, Out]]() runtime.DataSourceRegistration {
	return runtime.MakeDataSource[T, Out, *awscfg.Configuration, PT]()
}

func makeAction[T, Out any, PT actionPtr[T, Out]]() runtime.ActionRegistration {
	return runtime.MakeAction[T, Out, *awscfg.Configuration, PT]()
}

func Library() *runtime.Library {
	return &runtime.Library{
		Name:          "aws-lambdamicrovms",
		Description:   "AWS Lambda MicroVMs library for Unobin.",
		Configuration: config.LibraryConfiguration(),
		Resources: map[string]runtime.ResourceRegistration{
			"microvm-image": makeResource[svc.MicrovmImageResource, *svc.MicrovmImageResourceOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"microvm-image": makeDataSource[
				svc.MicrovmImageDataSource, *svc.MicrovmImageDataSourceOutput](),
			"microvm-images": makeDataSource[
				svc.MicrovmImagesDataSource, *svc.MicrovmImagesDataSourceOutput](),
			"microvm-image-version": makeDataSource[
				svc.MicrovmImageVersionDataSource, *svc.MicrovmImageVersionDataSourceOutput](),
			"microvm-image-versions": makeDataSource[
				svc.MicrovmImageVersionsDataSource, *svc.MicrovmImageVersionsDataSourceOutput](),
			"microvm-image-build": makeDataSource[
				svc.MicrovmImageBuildDataSource, *svc.MicrovmImageBuildDataSourceOutput](),
			"microvm-image-builds": makeDataSource[
				svc.MicrovmImageBuildsDataSource, *svc.MicrovmImageBuildsDataSourceOutput](),
			"managed-microvm-images": makeDataSource[
				svc.ManagedMicrovmImagesDataSource, *svc.ManagedMicrovmImagesDataSourceOutput](),
			"managed-microvm-image-versions": makeDataSource[
				svc.ManagedMicrovmImageVersionsDataSource, *svc.ManagedMicrovmImageVersionsDataSourceOutput](),
			"microvm": makeDataSource[
				svc.MicrovmDataSource, *svc.MicrovmDataSourceOutput](),
			"microvms": makeDataSource[
				svc.MicrovmsDataSource, *svc.MicrovmsDataSourceOutput](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"run-microvm": makeAction[
				svc.RunMicrovmAction, *svc.RunMicrovmActionOutput](),
			"create-microvm-auth-token": makeAction[
				svc.MicrovmAuthTokenAction, *svc.MicrovmAuthTokenActionOutput](),
			"create-microvm-shell-auth-token": makeAction[
				svc.MicrovmShellAuthTokenAction, *svc.MicrovmShellAuthTokenActionOutput](),
			"suspend-microvm": makeAction[
				svc.SuspendMicrovmAction, *svc.SuspendMicrovmActionOutput](),
			"resume-microvm": makeAction[
				svc.ResumeMicrovmAction, *svc.ResumeMicrovmActionOutput](),
			"terminate-microvm": makeAction[
				svc.TerminateMicrovmAction, *svc.TerminateMicrovmActionOutput](),
			"update-microvm-image-version-status": makeAction[
				svc.UpdateMicrovmImageVersionStatusAction,
				*svc.UpdateMicrovmImageVersionStatusActionOutput](),
		},
	}
}
