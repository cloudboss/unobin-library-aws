package ec2

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

// AMI looks up a single EC2 AMI with DescribeImages and selects one
// deterministically. The query inputs owners, executable-users, filters,
// image-ids, and include-deprecated reach the DescribeImages call; most-recent,
// name-regex, and allow-unsafe-filter act client-side and never reach AWS. The
// lookup errors when no image matches and, unless most-recent is set, when more
// than one does; with most-recent it returns the image with the newest
// creation-date.
//
// Two guards run before the call. When most-recent is set and the query is not
// scoped by an owner or an explicit id -- owners is empty, image-ids is empty,
// and no filter is named image-id or owner-id -- a third party could publish a
// newer image that the lookup would then select, so the lookup errors unless
// allow-unsafe-filter is set. This is list-content logic over the filter names,
// not a per-field rule, so it lives in Read rather than in Constraints. The
// owners list, when given, must hold no empty-string element; that element check
// is likewise enforced in Read.
//
// Terraform's separate best-effort GetInstanceUefiData call is declined: it is a
// second request per read for a niche UEFI value, it passes an image id where an
// instance id is expected, and nothing downstream consumes it, so uefi-data is
// omitted from the output.
type AMI struct {
	Owners            *[]string    `ub:"owners"`
	ExecutableUsers   *[]string    `ub:"executable-users"`
	Filters           *[]AMIFilter `ub:"filters"`
	ImageIds          *[]string    `ub:"image-ids"`
	IncludeDeprecated *bool        `ub:"include-deprecated"`
	MostRecent        *bool        `ub:"most-recent"`
	NameRegex         *string      `ub:"name-regex"`
	AllowUnsafeFilter *bool        `ub:"allow-unsafe-filter"`
}

// AMIFilter is one DescribeImages filter: a filter name and the values to match,
// joined with OR. The name is an AWS DescribeImages filter key such as name,
// architecture, root-device-type, owner-alias, state, or tag:<Key>. Both fields
// are required and pass straight through to the API with no client-side
// interpretation.
type AMIFilter struct {
	Name   string   `ub:"name"`
	Values []string `ub:"values"`
}

// AMIOutput holds the chosen image's attributes. The arn is composed locally as
// a regional, account-less ARN, not read from DescribeImages, which does not
// return it. root-snapshot-id is derived from the block-device mapping whose
// device name matches the root device. The exotic image attributes
// (block-device-mappings, boot-mode, deprecation-time, description, hypervisor,
// image-location, image-owner-alias, image-type, imds-support, kernel-id,
// last-launched-time, platform, platform-details, product-codes, public,
// ramdisk-id, state-reason, tpm-support, usage-operation, tags) are deliberately
// omitted; none is consumed downstream and they can be added when a real
// reference appears.
type AMIOutput struct {
	ImageId            string `ub:"image-id"`
	Arn                string `ub:"arn"`
	OwnerId            string `ub:"owner-id"`
	Name               string `ub:"name"`
	CreationDate       string `ub:"creation-date"`
	Architecture       string `ub:"architecture"`
	RootDeviceName     string `ub:"root-device-name"`
	RootDeviceType     string `ub:"root-device-type"`
	RootSnapshotId     string `ub:"root-snapshot-id"`
	VirtualizationType string `ub:"virtualization-type"`
	EnaSupport         bool   `ub:"ena-support"`
	SriovNetSupport    string `ub:"sriov-net-support"`
	State              string `ub:"state"`
}

// Constraints declares the input rules expressible as a derived schema. The
// owners list, when given, must hold at least one element. The no-empty-element
// rule on owners and the requirement that name-regex compile are runtime checks
// enforced in Read, since neither an element-value test nor a regex-validity
// test is a derivable constraint.
func (r AMI) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.Owners)).
			Require(constraint.MinItems(r.Owners, 1)).
			Message("owners must list at least one owner when given"),
	}
}

// Read resolves the AMI. It validates the inputs that the schema cannot, runs
// the unsafe-filter guard, paginates DescribeImages in full, applies the
// client-side name-regex filter, then selects the single result -- erroring on
// no match or, without most-recent, on more than one.
func (r *AMI) Read(ctx context.Context, cfg *awsCfg) (*AMIOutput, error) {
	nameRegex, err := r.compileNameRegex()
	if err != nil {
		return nil, err
	}
	if err := r.checkOwnersElements(); err != nil {
		return nil, err
	}
	if err := r.checkUnsafeFilter(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	images, err := r.findImages(ctx, client)
	if err != nil {
		return nil, err
	}
	images = filterByNameRegex(images, nameRegex)
	if len(images) < 1 {
		return nil, errors.New(
			"query returned no results; " +
				"please change your search criteria and try again")
	}
	if len(images) > 1 && !aws.ToBool(r.MostRecent) {
		return nil, errors.New(
			"query returned more than one result; please try a more specific " +
				"search criteria, or set the most-recent input to true")
	}
	image := newestImage(images)
	return r.output(client, image), nil
}

// compileNameRegex compiles the name-regex input, returning a nil matcher when
// it is unset. A value that does not compile is a clear, field-named error.
func (r *AMI) compileNameRegex() (*regexp.Regexp, error) {
	if r.NameRegex == nil {
		return nil, nil
	}
	re, err := regexp.Compile(*r.NameRegex)
	if err != nil {
		return nil, fmt.Errorf("name-regex does not compile: %w", err)
	}
	return re, nil
}

// checkOwnersElements rejects an owners list that holds an empty-string element,
// the second half of Terraform's owners validation that a derived constraint
// cannot express.
func (r *AMI) checkOwnersElements() error {
	if r.Owners != nil && slices.Contains(*r.Owners, "") {
		return errors.New("owners must not contain an empty value")
	}
	return nil
}

// checkUnsafeFilter blocks a most-recent lookup that is not pinned to an owner
// or an explicit id, where a third party could publish a newer image that the
// lookup would select. The query counts as pinned when owners is non-empty, when
// image-ids is non-empty, or when a filter is named image-id or owner-id. An
// owner-alias filter does not count, matching Terraform. Setting
// allow-unsafe-filter accepts the risk and skips the block.
func (r *AMI) checkUnsafeFilter() error {
	if !aws.ToBool(r.MostRecent) || aws.ToBool(r.AllowUnsafeFilter) {
		return nil
	}
	if (r.Owners != nil && len(*r.Owners) > 0) || len(ptr.Value(r.ImageIds)) > 0 {
		return nil
	}
	for _, f := range ptr.Value(r.Filters) {
		if f.Name == "image-id" || f.Name == "owner-id" {
			return nil
		}
	}
	return errors.New(
		"most-recent is set without owners, image-ids, or an image-id or owner-id " +
			"filter, so a third party could publish a newer matching image; set " +
			"allow-unsafe-filter to true to accept this risk")
}

// findImages paginates DescribeImages and collects every page into one slice. A
// query by an image id that does not exist returns InvalidAMIID.NotFound, which
// is treated as zero results so it funnels into Read's no-results message rather
// than a raw SDK error. The path adds no retry or waiter: DescribeImages is
// consistent enough for a one-shot lookup.
func (r *AMI) findImages(
	ctx context.Context, client *ec2.Client,
) ([]ec2types.Image, error) {
	in := &ec2.DescribeImagesInput{
		IncludeDeprecated: aws.Bool(aws.ToBool(r.IncludeDeprecated)),
	}
	if len(ptr.Value(r.ExecutableUsers)) > 0 {
		in.ExecutableUsers = ptr.Value(r.ExecutableUsers)
	}
	if r.Owners != nil && len(*r.Owners) > 0 {
		in.Owners = *r.Owners
	}
	if len(ptr.Value(r.ImageIds)) > 0 {
		in.ImageIds = ptr.Value(r.ImageIds)
	}
	if len(ptr.Value(r.Filters)) > 0 {
		in.Filters = toFilters(ptr.Value(r.Filters))
	}
	var images []ec2types.Image
	paginator := ec2.NewDescribeImagesPaginator(client, in)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if isNotFound(err, "InvalidAMIID.NotFound") {
				return nil, nil
			}
			return nil, fmt.Errorf("describe images: %w", err)
		}
		images = append(images, page.Images...)
	}
	return images, nil
}

// output builds the chosen image's attributes. The ARN is composed locally as a
// regional, account-less ARN: arn:<partition>:ec2:<region>::image/<image-id>,
// with an empty account-id segment, since DescribeImages does not return it.
func (r *AMI) output(client *ec2.Client, image ec2types.Image) *AMIOutput {
	imageID := aws.ToString(image.ImageId)
	reg := region(client)
	arn := fmt.Sprintf("arn:%s:ec2:%s::image/%s", partition.Of(reg), reg, imageID)
	return &AMIOutput{
		ImageId:            imageID,
		Arn:                arn,
		OwnerId:            aws.ToString(image.OwnerId),
		Name:               aws.ToString(image.Name),
		CreationDate:       aws.ToString(image.CreationDate),
		Architecture:       string(image.Architecture),
		RootDeviceName:     aws.ToString(image.RootDeviceName),
		RootDeviceType:     string(image.RootDeviceType),
		RootSnapshotId:     rootSnapshotID(image),
		VirtualizationType: string(image.VirtualizationType),
		EnaSupport:         aws.ToBool(image.EnaSupport),
		SriovNetSupport:    aws.ToString(image.SriovNetSupport),
		State:              string(image.State),
	}
}

// toFilters converts the input filters into the DescribeImages filter list,
// passing each name and its values through unchanged.
func toFilters(filters []AMIFilter) []ec2types.Filter {
	out := make([]ec2types.Filter, 0, len(filters))
	for _, f := range filters {
		out = append(out, ec2types.Filter{
			Name:   aws.String(f.Name),
			Values: f.Values,
		})
	}
	return out
}

// filterByNameRegex keeps only the images whose Name matches re, an unanchored
// substring match. An image with an empty Name is never matched. A nil matcher
// passes every image through unchanged.
func filterByNameRegex(images []ec2types.Image, re *regexp.Regexp) []ec2types.Image {
	if re == nil {
		return images
	}
	kept := make([]ec2types.Image, 0, len(images))
	for _, image := range images {
		name := aws.ToString(image.Name)
		if name == "" {
			continue
		}
		if re.MatchString(name) {
			kept = append(kept, image)
		}
	}
	return kept
}

// newestImage returns the image with the newest creation-date, comparing dates
// parsed as RFC3339. A date that does not parse becomes the zero time and so
// sorts oldest. There is no secondary tiebreaker, so the result among images
// that share the newest date is unspecified. The selection runs even for a
// single image, returning it.
func newestImage(images []ec2types.Image) ec2types.Image {
	return slices.MaxFunc(images, func(a, b ec2types.Image) int {
		if c := creationTime(a).Compare(creationTime(b)); c != 0 {
			return c
		}
		return strings.Compare(aws.ToString(a.ImageId), aws.ToString(b.ImageId))
	})
}

// creationTime parses an image's creation-date as RFC3339, returning the zero
// time when it is empty or does not parse, matching Terraform's ignored parse
// error.
func creationTime(image ec2types.Image) time.Time {
	t, err := time.Parse(time.RFC3339, aws.ToString(image.CreationDate))
	if err != nil {
		return time.Time{}
	}
	return t
}

// rootSnapshotID returns the snapshot id of the block-device mapping whose
// device name is the image's root device, or an empty string when there is no
// such mapping or it has no EBS snapshot.
func rootSnapshotID(image ec2types.Image) string {
	root := aws.ToString(image.RootDeviceName)
	for _, mapping := range image.BlockDeviceMappings {
		if aws.ToString(mapping.DeviceName) != root {
			continue
		}
		if mapping.Ebs == nil {
			return ""
		}
		return aws.ToString(mapping.Ebs.SnapshotId)
	}
	return ""
}
