package ecs

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// TaskDefinitionVolume is one data volume containers of the task can mount,
// named by the mount points in container definitions. It mirrors the SDK
// Volume type. The configuration blocks select the volume kind: a host path,
// a Docker volume, an EFS or FSx for Windows File Server filesystem, or an
// Amazon S3 file system; the API enforces that the blocks given fit
// together, and a volume with none of them is scratch space on the host.
type TaskDefinitionVolume struct {
	Name                                    string                       `ub:"name"`
	ConfiguredAtLaunch                      *bool                        `ub:"configured-at-launch"`
	Host                                    *TaskDefinitionVolumeHost    `ub:"host"`
	DockerVolumeConfiguration               *TaskDefinitionVolumeDocker  `ub:"docker-volume-configuration"`
	EfsVolumeConfiguration                  *TaskDefinitionVolumeEfs     `ub:"efs-volume-configuration"`
	FsxWindowsFileServerVolumeConfiguration *TaskDefinitionVolumeFsx     `ub:"fsx-windows-file-server-volume-configuration"`
	S3filesVolumeConfiguration              *TaskDefinitionVolumeS3Files `ub:"s3files-volume-configuration"`
}

// TaskDefinitionVolumeHost binds the volume to a path on the host. It
// mirrors the SDK HostVolumeProperties type; with the source path omitted,
// the host assigns a path whose contents do not outlive the task.
type TaskDefinitionVolumeHost struct {
	SourcePath *string `ub:"source-path"`
}

// TaskDefinitionVolumeDocker is a Docker volume on an EC2 container
// instance. It mirrors the SDK DockerVolumeConfiguration type: scope is task
// or shared, the driver defaults to local, and autoprovision (shared
// volumes only) creates the volume when it does not exist.
type TaskDefinitionVolumeDocker struct {
	Autoprovision *bool              `ub:"autoprovision"`
	Driver        *string            `ub:"driver"`
	DriverOpts    *map[string]string `ub:"driver-opts"`
	Labels        *map[string]string `ub:"labels"`
	Scope         *string            `ub:"scope"`
}

// TaskDefinitionVolumeEfs is an Amazon EFS filesystem volume. It mirrors the
// SDK EFSVolumeConfiguration type: the root directory defaults to /, and
// transit-encryption (ENABLED or DISABLED) must be ENABLED when an
// authorization config is used. The transit encryption port, 1 to 65535,
// defaults to a port the EFS mount helper chooses.
type TaskDefinitionVolumeEfs struct {
	FileSystemId          string                                `ub:"file-system-id"`
	AuthorizationConfig   *TaskDefinitionVolumeEfsAuthorization `ub:"authorization-config"`
	RootDirectory         *string                               `ub:"root-directory"`
	TransitEncryption     *string                               `ub:"transit-encryption"`
	TransitEncryptionPort *int64                                `ub:"transit-encryption-port"`
}

// TaskDefinitionVolumeEfsAuthorization is the EFS access point and IAM
// authorization for the volume. It mirrors the SDK EFSAuthorizationConfig
// type; iam is ENABLED or DISABLED.
type TaskDefinitionVolumeEfsAuthorization struct {
	AccessPointId *string `ub:"access-point-id"`
	Iam           *string `ub:"iam"`
}

// TaskDefinitionVolumeFsx is an Amazon FSx for Windows File Server
// filesystem volume. It mirrors the SDK
// FSxWindowsFileServerVolumeConfiguration type; all of file-system-id,
// root-directory, and authorization-config are required.
type TaskDefinitionVolumeFsx struct {
	FileSystemId        string                                `ub:"file-system-id"`
	RootDirectory       string                                `ub:"root-directory"`
	AuthorizationConfig *TaskDefinitionVolumeFsxAuthorization `ub:"authorization-config"`
}

// TaskDefinitionVolumeFsxAuthorization is the credentials authorization for
// an FSx for Windows File Server volume. It mirrors the SDK
// FSxWindowsFileServerAuthorizationConfig type; credentials-parameter is the
// ARN of a Secrets Manager secret or an SSM parameter holding the domain
// credentials.
type TaskDefinitionVolumeFsxAuthorization struct {
	CredentialsParameter string `ub:"credentials-parameter"`
	Domain               string `ub:"domain"`
}

// TaskDefinitionVolumeS3Files is an Amazon S3 file system volume. It mirrors
// the SDK S3FilesVolumeConfiguration type: file-system-arn is the S3 file
// system ARN (access-point-arn its access point ARN), the root directory
// defaults to /, and the transit encryption port, 1 to 65535, defaults to a
// port the mount helper chooses.
type TaskDefinitionVolumeS3Files struct {
	FileSystemArn         string  `ub:"file-system-arn"`
	AccessPointArn        *string `ub:"access-point-arn"`
	RootDirectory         *string `ub:"root-directory"`
	TransitEncryptionPort *int64  `ub:"transit-encryption-port"`
}

// taskDefinitionVolumesSDK converts the volume list to its SDK type,
// returning nil for an empty list so the member stays out of the request.
func taskDefinitionVolumesSDK(volumes []TaskDefinitionVolume) []ecstypes.Volume {
	if len(volumes) == 0 {
		return nil
	}
	out := make([]ecstypes.Volume, 0, len(volumes))
	for _, v := range volumes {
		sdkVolume := ecstypes.Volume{
			Name:                                    aws.String(v.Name),
			ConfiguredAtLaunch:                      v.ConfiguredAtLaunch,
			DockerVolumeConfiguration:               v.DockerVolumeConfiguration.sdk(),
			EfsVolumeConfiguration:                  v.EfsVolumeConfiguration.sdk(),
			FsxWindowsFileServerVolumeConfiguration: v.FsxWindowsFileServerVolumeConfiguration.sdk(),
			S3filesVolumeConfiguration:              v.S3filesVolumeConfiguration.sdk(),
		}
		if v.Host != nil {
			sdkVolume.Host = &ecstypes.HostVolumeProperties{SourcePath: v.Host.SourcePath}
		}
		out = append(out, sdkVolume)
	}
	return out
}

// sdk converts the Docker volume block to its SDK type, returning nil for a
// nil block so an absent block stays out of the request. ECS rejects an
// explicit autoprovision of false on a volume that also sets scope to task,
// while accepting the same false with scope omitted, so that one combination
// is suppressed; a true value is still sent with task scope so the API's
// clearer error reaches the user.
func (d *TaskDefinitionVolumeDocker) sdk() *ecstypes.DockerVolumeConfiguration {
	if d == nil {
		return nil
	}
	out := &ecstypes.DockerVolumeConfiguration{
		Driver:     d.Driver,
		DriverOpts: ptr.Value(d.DriverOpts),
		Labels:     ptr.Value(d.Labels),
	}
	scope := aws.ToString(d.Scope)
	if d.Scope != nil {
		out.Scope = ecstypes.Scope(scope)
	}
	if a := d.Autoprovision; a != nil && (*a || scope != "task") {
		out.Autoprovision = a
	}
	return out
}

// sdk converts the EFS volume block to its SDK type, returning nil for a
// nil block so an absent block stays out of the request. A nil transit
// encryption port stays unset; 0 is not a value the API accepts.
func (e *TaskDefinitionVolumeEfs) sdk() *ecstypes.EFSVolumeConfiguration {
	if e == nil {
		return nil
	}
	out := &ecstypes.EFSVolumeConfiguration{
		FileSystemId:          aws.String(e.FileSystemId),
		RootDirectory:         e.RootDirectory,
		TransitEncryptionPort: ptr.Int32(e.TransitEncryptionPort),
	}
	if e.TransitEncryption != nil {
		out.TransitEncryption = ecstypes.EFSTransitEncryption(*e.TransitEncryption)
	}
	if a := e.AuthorizationConfig; a != nil {
		auth := &ecstypes.EFSAuthorizationConfig{AccessPointId: a.AccessPointId}
		if a.Iam != nil {
			auth.Iam = ecstypes.EFSAuthorizationConfigIAM(*a.Iam)
		}
		out.AuthorizationConfig = auth
	}
	return out
}

// sdk converts the FSx volume block to its SDK type, returning nil for a
// nil block so an absent block stays out of the request.
func (f *TaskDefinitionVolumeFsx) sdk() *ecstypes.FSxWindowsFileServerVolumeConfiguration {
	if f == nil {
		return nil
	}
	out := &ecstypes.FSxWindowsFileServerVolumeConfiguration{
		FileSystemId:  aws.String(f.FileSystemId),
		RootDirectory: aws.String(f.RootDirectory),
	}
	if a := f.AuthorizationConfig; a != nil {
		out.AuthorizationConfig = &ecstypes.FSxWindowsFileServerAuthorizationConfig{
			CredentialsParameter: aws.String(a.CredentialsParameter),
			Domain:               aws.String(a.Domain),
		}
	}
	return out
}

// sdk converts the S3 files volume block to its SDK type, returning nil for
// a nil block so an absent block stays out of the request. A nil transit
// encryption port stays unset; 0 is not a value the API accepts.
func (s *TaskDefinitionVolumeS3Files) sdk() *ecstypes.S3FilesVolumeConfiguration {
	if s == nil {
		return nil
	}
	return &ecstypes.S3FilesVolumeConfiguration{
		FileSystemArn:         aws.String(s.FileSystemArn),
		AccessPointArn:        s.AccessPointArn,
		RootDirectory:         s.RootDirectory,
		TransitEncryptionPort: ptr.Int32(s.TransitEncryptionPort),
	}
}
