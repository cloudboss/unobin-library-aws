package efs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	efssdk "github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

const (
	accessPointWaitTimeout    = 10 * time.Minute
	accessPointNotFoundLimit  = 20
	accessPointMaxPOSIXID     = int64(4294967295)
	accessPointMaxSecondaryID = 16
)

var (
	accessPointPollDelays = [...]time.Duration{
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
		3200 * time.Millisecond,
		6400 * time.Millisecond,
		10 * time.Second,
	}
	accessPointPermissionsPattern = regexp.MustCompile(`^[0-7]{3,4}$`)
)

type AccessPointPosixUser struct {
	Uid           int64    `ub:"uid"`
	Gid           int64    `ub:"gid"`
	SecondaryGids *[]int64 `ub:"secondary-gids"`
}

type AccessPointCreationInfo struct {
	OwnerUid    int64  `ub:"owner-uid"`
	OwnerGid    int64  `ub:"owner-gid"`
	Permissions string `ub:"permissions"`
}

type AccessPointRootDirectory struct {
	Path         *string                  `ub:"path"`
	CreationInfo *AccessPointCreationInfo `ub:"creation-info"`
}

type AccessPointResource struct {
	FileSystemId  string                    `ub:"file-system-id"`
	ClientToken   *string                   `ub:"client-token"`
	PosixUser     *AccessPointPosixUser     `ub:"posix-user"`
	RootDirectory *AccessPointRootDirectory `ub:"root-directory"`
	Tags          *map[string]string        `ub:"tags"`
}

type AccessPointResourceOutput struct {
	AccessPointId string `ub:"access-point-id"`
	Arn           string `ub:"arn"`
	OwnerId       string `ub:"owner-id"`
}

type accessPointReadClient interface {
	DescribeAccessPoints(context.Context, *efssdk.DescribeAccessPointsInput,
		...func(*efssdk.Options)) (*efssdk.DescribeAccessPointsOutput, error)
}

type accessPointCreateClient interface {
	accessPointReadClient
	CreateAccessPoint(context.Context, *efssdk.CreateAccessPointInput,
		...func(*efssdk.Options)) (*efssdk.CreateAccessPointOutput, error)
}

type accessPointTagClient interface {
	ListTagsForResource(context.Context, *efssdk.ListTagsForResourceInput,
		...func(*efssdk.Options)) (*efssdk.ListTagsForResourceOutput, error)
	TagResource(context.Context, *efssdk.TagResourceInput,
		...func(*efssdk.Options)) (*efssdk.TagResourceOutput, error)
	UntagResource(context.Context, *efssdk.UntagResourceInput,
		...func(*efssdk.Options)) (*efssdk.UntagResourceOutput, error)
}

type accessPointDeleteClient interface {
	accessPointReadClient
	DeleteAccessPoint(context.Context, *efssdk.DeleteAccessPointInput,
		...func(*efssdk.Options)) (*efssdk.DeleteAccessPointOutput, error)
}

type accessPointClient interface {
	accessPointCreateClient
	accessPointTagClient
	accessPointDeleteClient
}

type accessPointClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type systemAccessPointClock struct{}

func (systemAccessPointClock) Now() time.Time { return time.Now() }

func (systemAccessPointClock) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func newAccessPointClient(ctx context.Context, cfg *awsCfg) (*efssdk.Client, error) {
	loaded, err := awscfg.Load(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return efssdk.NewFromConfig(loaded), nil
}

func (r *AccessPointResource) SchemaVersion() int { return 1 }

func (r *AccessPointResource) ReplaceFields() []string {
	return []string{"file-system-id", "client-token", "posix-user", "root-directory"}
}

func (r AccessPointResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.ClientToken)).
			Require(constraint.NotEmpty(r.ClientToken), constraint.MaxItems(r.ClientToken, 64)).
			Message("client-token must contain 1 to 64 ASCII characters"),
	}
}

func (r *AccessPointResource) ValidateInputs(context.Context, *awsCfg) error {
	if !fileSystemIDPattern.MatchString(r.FileSystemId) {
		return errors.New("file-system-id must be a valid EFS file system ID or ARN")
	}
	if err := validateAccessPointClientToken(r.ClientToken); err != nil {
		return err
	}
	if err := validateAccessPointPosixUser(r.PosixUser); err != nil {
		return err
	}
	if err := validateAccessPointRootDirectory(r.RootDirectory); err != nil {
		return err
	}
	return validateAccessPointTags(ptr.Value(r.Tags))
}

func (r *AccessPointResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*AccessPointResourceOutput, error) {
	client, err := newAccessPointClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, systemAccessPointClock{})
}

func (r *AccessPointResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *AccessPointResourceOutput,
) (*AccessPointResourceOutput, error) {
	if prior == nil || prior.AccessPointId == "" {
		return nil, errors.New("read access point: missing prior access-point-id")
	}
	client, err := newAccessPointClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.AccessPointId)
}

func (r *AccessPointResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[AccessPointResource, *AccessPointResourceOutput],
) (*AccessPointResourceOutput, error) {
	client, err := newAccessPointClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior)
}

func (r *AccessPointResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *AccessPointResourceOutput,
) error {
	if prior == nil || prior.AccessPointId == "" {
		return errors.New("delete access point: missing prior access-point-id")
	}
	client, err := newAccessPointClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior.AccessPointId, systemAccessPointClock{})
}

func (r *AccessPointResource) create(
	ctx context.Context,
	client accessPointCreateClient,
	clock accessPointClock,
) (*AccessPointResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	created, err := client.CreateAccessPoint(ctx, &efssdk.CreateAccessPointInput{
		FileSystemId:  aws.String(r.FileSystemId),
		ClientToken:   r.ClientToken,
		PosixUser:     accessPointPosixUser(r.PosixUser),
		RootDirectory: accessPointRootDirectory(r.RootDirectory),
		Tags:          fileSystemTags(ptr.Value(r.Tags)),
	})
	if err != nil {
		return nil, fmt.Errorf("create access point: %w", err)
	}
	accessPointID := ""
	if created != nil {
		accessPointID = aws.ToString(created.AccessPointId)
	}
	if accessPointID == "" {
		return nil, errors.New("create access point returned no access-point-id")
	}
	description, err := waitAccessPointAvailable(
		ctx, client, accessPointID, accessPointWaitTimeout, clock)
	if err != nil {
		return nil, err
	}
	return accessPointOutput(description), nil
}

func (r *AccessPointResource) read(
	ctx context.Context,
	client accessPointReadClient,
	accessPointID string,
) (*AccessPointResourceOutput, error) {
	description, err := describeAccessPoint(ctx, client, accessPointID)
	if err != nil {
		return nil, err
	}
	return accessPointOutput(description), nil
}

func (r *AccessPointResource) update(
	ctx context.Context,
	client accessPointClient,
	prior runtime.Prior[AccessPointResource, *AccessPointResourceOutput],
) (*AccessPointResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	if prior.Outputs == nil || prior.Outputs.AccessPointId == "" {
		return nil, errors.New("update access point: missing prior access-point-id")
	}
	accessPointID := prior.Outputs.AccessPointId
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncAccessPointTags(ctx, client, accessPointID, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, accessPointID)
}

func (r *AccessPointResource) delete(
	ctx context.Context,
	client accessPointDeleteClient,
	accessPointID string,
	clock accessPointClock,
) error {
	_, err := client.DeleteAccessPoint(ctx, &efssdk.DeleteAccessPointInput{
		AccessPointId: aws.String(accessPointID),
	})
	if isAccessPointNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete access point %s: %w", accessPointID, err)
	}
	return waitAccessPointDeleted(ctx, client, accessPointID, accessPointWaitTimeout, clock)
}

func describeAccessPoint(
	ctx context.Context,
	client accessPointReadClient,
	accessPointID string,
) (efstypes.AccessPointDescription, error) {
	pager := efssdk.NewDescribeAccessPointsPaginator(client,
		&efssdk.DescribeAccessPointsInput{AccessPointId: aws.String(accessPointID)},
		func(options *efssdk.DescribeAccessPointsPaginatorOptions) {
			options.StopOnDuplicateToken = true
		})
	results := make([]efstypes.AccessPointDescription, 0, 1)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if isAccessPointNotFound(err) {
			return efstypes.AccessPointDescription{}, runtime.ErrNotFound
		}
		if err != nil {
			return efstypes.AccessPointDescription{}, fmt.Errorf(
				"describe access point %s: %w", accessPointID, err)
		}
		results = append(results, page.AccessPoints...)
		if len(results) > 1 {
			return efstypes.AccessPointDescription{}, fmt.Errorf(
				"describe access point %s returned %d results, want 1",
				accessPointID, len(results))
		}
	}
	if len(results) == 0 {
		return efstypes.AccessPointDescription{}, runtime.ErrNotFound
	}
	description := results[0]
	if aws.ToString(description.AccessPointId) != accessPointID ||
		description.LifeCycleState == efstypes.LifeCycleStateDeleted {
		return efstypes.AccessPointDescription{}, runtime.ErrNotFound
	}
	return description, nil
}

func waitAccessPointAvailable(
	ctx context.Context,
	client accessPointReadClient,
	accessPointID string,
	timeout time.Duration,
	clock accessPointClock,
) (efstypes.AccessPointDescription, error) {
	notFound := 0
	var available efstypes.AccessPointDescription
	err := pollAccessPoint(ctx,
		fmt.Sprintf("EFS access point %s to become available", accessPointID),
		timeout, clock, func(ctx context.Context) (bool, error) {
			description, err := describeAccessPoint(ctx, client, accessPointID)
			if errors.Is(err, runtime.ErrNotFound) {
				notFound++
				if notFound > accessPointNotFoundLimit {
					return false, fmt.Errorf(
						"access point %s was absent for %d consecutive observations: %w",
						accessPointID, notFound, runtime.ErrNotFound)
				}
				return false, nil
			}
			if err != nil {
				return false, err
			}
			notFound = 0
			switch description.LifeCycleState {
			case efstypes.LifeCycleStateAvailable:
				available = description
				return true, nil
			case efstypes.LifeCycleStateCreating:
				return false, nil
			default:
				return false, fmt.Errorf("access point %s entered lifecycle state %s",
					accessPointID, description.LifeCycleState)
			}
		})
	if err != nil {
		return efstypes.AccessPointDescription{}, err
	}
	return available, nil
}

func waitAccessPointDeleted(
	ctx context.Context,
	client accessPointReadClient,
	accessPointID string,
	timeout time.Duration,
	clock accessPointClock,
) error {
	return pollAccessPoint(ctx,
		fmt.Sprintf("EFS access point %s to be deleted", accessPointID),
		timeout, clock, func(ctx context.Context) (bool, error) {
			description, err := describeAccessPoint(ctx, client, accessPointID)
			if errors.Is(err, runtime.ErrNotFound) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			switch description.LifeCycleState {
			case efstypes.LifeCycleStateAvailable, efstypes.LifeCycleStateDeleting:
				return false, nil
			default:
				return false, fmt.Errorf("access point %s entered lifecycle state %s",
					accessPointID, description.LifeCycleState)
			}
		})
}

func pollAccessPoint(
	ctx context.Context,
	what string,
	timeout time.Duration,
	clock accessPointClock,
	probe func(context.Context) (bool, error),
) error {
	deadline := clock.Now().Add(timeout)
	delayIndex := 0
	for {
		ready, err := probe(ctx)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			return fmt.Errorf("timed out waiting for %s", what)
		}
		delay := accessPointPollDelays[delayIndex]
		if delayIndex < len(accessPointPollDelays)-1 {
			delayIndex++
		}
		if delay > remaining {
			delay = remaining
		}
		if err := clock.Sleep(ctx, delay); err != nil {
			return err
		}
	}
}

func syncAccessPointTags(
	ctx context.Context,
	client accessPointTagClient,
	accessPointID string,
	desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			return readAccessPointTags(ctx, client, accessPointID)
		},
		func(ctx context.Context, upsert map[string]string) error {
			_, err := client.TagResource(ctx, &efssdk.TagResourceInput{
				ResourceId: aws.String(accessPointID),
				Tags:       fileSystemTags(upsert),
			})
			if isAccessPointNotFound(err) {
				return runtime.ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("tag access point %s: %w", accessPointID, err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			_, err := client.UntagResource(ctx, &efssdk.UntagResourceInput{
				ResourceId: aws.String(accessPointID),
				TagKeys:    remove,
			})
			if isAccessPointNotFound(err) {
				return runtime.ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("untag access point %s: %w", accessPointID, err)
			}
			return nil
		},
	)
}

func readAccessPointTags(
	ctx context.Context,
	client accessPointTagClient,
	accessPointID string,
) (map[string]string, error) {
	pager := efssdk.NewListTagsForResourcePaginator(client,
		&efssdk.ListTagsForResourceInput{ResourceId: aws.String(accessPointID)},
		func(options *efssdk.ListTagsForResourcePaginatorOptions) {
			options.StopOnDuplicateToken = true
		})
	tags := map[string]string{}
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if isAccessPointNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("list access point %s tags: %w", accessPointID, err)
		}
		maps.Copy(tags, fileSystemTagMap(page.Tags))
	}
	return tags, nil
}

func accessPointOutput(description efstypes.AccessPointDescription) *AccessPointResourceOutput {
	return &AccessPointResourceOutput{
		AccessPointId: aws.ToString(description.AccessPointId),
		Arn:           aws.ToString(description.AccessPointArn),
		OwnerId:       aws.ToString(description.OwnerId),
	}
}

func accessPointPosixUser(value *AccessPointPosixUser) *efstypes.PosixUser {
	if value == nil {
		return nil
	}
	return &efstypes.PosixUser{
		Uid:           aws.Int64(value.Uid),
		Gid:           aws.Int64(value.Gid),
		SecondaryGids: slices.Clone(ptr.Value(value.SecondaryGids)),
	}
}

func accessPointRootDirectory(value *AccessPointRootDirectory) *efstypes.RootDirectory {
	if value == nil {
		return nil
	}
	return &efstypes.RootDirectory{
		Path:         value.Path,
		CreationInfo: accessPointCreationInfo(value.CreationInfo),
	}
}

func accessPointCreationInfo(value *AccessPointCreationInfo) *efstypes.CreationInfo {
	if value == nil {
		return nil
	}
	return &efstypes.CreationInfo{
		OwnerUid:    aws.Int64(value.OwnerUid),
		OwnerGid:    aws.Int64(value.OwnerGid),
		Permissions: aws.String(value.Permissions),
	}
}

func validateAccessPointClientToken(value *string) error {
	if value == nil {
		return nil
	}
	length := utf8.RuneCountInString(*value)
	if length < 1 || length > 64 {
		return errors.New("client-token must contain 1 to 64 ASCII characters")
	}
	for _, char := range *value {
		if char > unicode.MaxASCII {
			return errors.New("client-token must contain only ASCII characters")
		}
	}
	return nil
}

func validateAccessPointPosixUser(value *AccessPointPosixUser) error {
	if value == nil {
		return nil
	}
	if !validAccessPointPOSIXID(value.Uid) {
		return errors.New("posix-user uid must be between 0 and 4294967295")
	}
	if !validAccessPointPOSIXID(value.Gid) {
		return errors.New("posix-user gid must be between 0 and 4294967295")
	}
	secondary := ptr.Value(value.SecondaryGids)
	if len(secondary) > accessPointMaxSecondaryID {
		return errors.New("posix-user secondary-gids holds at most 16 entries")
	}
	for index, gid := range secondary {
		if !validAccessPointPOSIXID(gid) {
			return fmt.Errorf(
				"posix-user secondary-gids[%d] must be between 0 and 4294967295", index)
		}
	}
	return nil
}

func validateAccessPointRootDirectory(value *AccessPointRootDirectory) error {
	if value == nil {
		return nil
	}
	if value.Path != nil && !validAccessPointPath(*value.Path) {
		return errors.New(
			"root-directory path must be / or contain up to four valid non-dot-leading components")
	}
	if value.CreationInfo == nil {
		return nil
	}
	if !validAccessPointPOSIXID(value.CreationInfo.OwnerUid) {
		return errors.New("creation-info owner-uid must be between 0 and 4294967295")
	}
	if !validAccessPointPOSIXID(value.CreationInfo.OwnerGid) {
		return errors.New("creation-info owner-gid must be between 0 and 4294967295")
	}
	if !accessPointPermissionsPattern.MatchString(value.CreationInfo.Permissions) {
		return errors.New("creation-info permissions must contain 3 or 4 octal digits")
	}
	return nil
}

func validateAccessPointTags(tags map[string]string) error {
	for key, value := range tags {
		if hasReservedTagPrefix(key) {
			return fmt.Errorf("tag key %q must not start with aws:", key)
		}
		if length := utf8.RuneCountInString(key); length < 1 || length > 128 {
			return fmt.Errorf("tag key %q must contain 1 to 128 characters", key)
		}
		if !tagKeyPattern.MatchString(key) {
			return fmt.Errorf("tag key %q contains characters outside the allowed characters", key)
		}
		if utf8.RuneCountInString(value) > 256 {
			return fmt.Errorf("tag value for %q must contain at most 256 characters", key)
		}
		if !tagValuePattern.MatchString(value) {
			return fmt.Errorf(
				"tag value for %q contains characters outside the allowed characters", key)
		}
	}
	return nil
}

func validAccessPointPOSIXID(value int64) bool {
	return value >= 0 && value <= accessPointMaxPOSIXID
}

func validAccessPointPath(path string) bool {
	if length := utf8.RuneCountInString(path); length < 1 || length > 100 {
		return false
	}
	if path == "/" {
		return true
	}
	if !strings.HasPrefix(path, "/") {
		return false
	}
	components := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(components) < 1 || len(components) > 4 {
		return false
	}
	for _, component := range components {
		if component == "" || strings.HasPrefix(component, ".") ||
			strings.ContainsAny(component, "$#<>;`|&?{}^*\n") {
			return false
		}
	}
	return true
}

func isAccessPointNotFound(err error) bool {
	var notFound *efstypes.AccessPointNotFound
	return errors.As(err, &notFound)
}

var _ accessPointClient = (*efssdk.Client)(nil)
