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
			"microvm-image": makeResource[svc.MicrovmImage, *svc.MicrovmImageOutput](),
		},
		DataSources: map[string]runtime.DataSourceRegistration{
			"microvm-image": makeDataSource[
				svc.MicrovmImageData, *svc.MicrovmImageDataOutput](),
			"microvm-images": makeDataSource[
				svc.MicrovmImages, *svc.MicrovmImagesOutput](),
			"microvm-image-version": makeDataSource[
				svc.MicrovmImageVersionData, *svc.MicrovmImageVersionDataOutput](),
			"microvm-image-versions": makeDataSource[
				svc.MicrovmImageVersions, *svc.MicrovmImageVersionsOutput](),
			"microvm-image-build": makeDataSource[
				svc.MicrovmImageBuildData, *svc.MicrovmImageBuildDataOutput](),
			"microvm-image-builds": makeDataSource[
				svc.MicrovmImageBuilds, *svc.MicrovmImageBuildsOutput](),
			"managed-microvm-images": makeDataSource[
				svc.ManagedMicrovmImages, *svc.ManagedMicrovmImagesOutput](),
			"managed-microvm-image-versions": makeDataSource[
				svc.ManagedMicrovmImageVersions, *svc.ManagedMicrovmImageVersionsOutput](),
			"microvm": makeDataSource[
				svc.MicrovmData, *svc.MicrovmDataOutput](),
			"microvms": makeDataSource[
				svc.Microvms, *svc.MicrovmsOutput](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"run-microvm": makeAction[svc.RunMicrovm, *svc.MicrovmDataOutput](),
			"create-microvm-auth-token": makeAction[
				svc.MicrovmAuthToken, *svc.MicrovmAuthTokenOutput](),
			"create-microvm-shell-auth-token": makeAction[
				svc.MicrovmShellAuthToken, *svc.MicrovmShellAuthTokenOutput](),
			"suspend-microvm": makeAction[
				svc.SuspendMicrovm, *svc.SuspendMicrovmOutput](),
			"resume-microvm": makeAction[
				svc.ResumeMicrovm, *svc.ResumeMicrovmOutput](),
			"terminate-microvm": makeAction[
				svc.TerminateMicrovm, *svc.TerminateMicrovmOutput](),
			"update-microvm-image-version-status": makeAction[
				svc.UpdateMicrovmImageVersionStatus,
				*svc.MicrovmImageVersionDataOutput](),
		},
	}
}
