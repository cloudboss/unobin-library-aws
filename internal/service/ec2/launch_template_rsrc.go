package ec2

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// LaunchTemplate is an EC2 launch template: a named, versioned object holding
// the configuration to launch instances from. Each change to the instance
// configuration is an immutable new version (CreateLaunchTemplateVersion), and
// the template's default version is set by a separate call (ModifyLaunchTemplate)
// rather than by editing a version in place. The template name is fixed at
// creation, so a change to it replaces the template; everything else lives under
// the data block and is reconciled by building a fresh version. The data block
// is built whole from the declared inputs each time, so a removed sub-block is
// simply absent from the next version, not cleared with a sentinel.
//
// Several niche or legacy members of the SDK launch-template data are
// deliberately not modeled: kernel-id and ram-disk-id (legacy paravirtual
// fields), secondary-interfaces (a recent multi-interface block), and
// instance-requirements (the attribute-based instance-type selection tree).
// With instance-requirements absent, instance-type is a plain optional field.
type LaunchTemplate struct {
	Name                 string             `ub:"name"`
	VersionDescription   *string            `ub:"version-description"`
	DefaultVersion       *int64             `ub:"default-version"`
	UpdateDefaultVersion *bool              `ub:"update-default-version"`
	Tags                 map[string]string  `ub:"tags"`
	Data                 LaunchTemplateData `ub:"data"`
}

// LaunchTemplateOutput holds the values the cloud computes for a launch
// template: its id and the latest and default version numbers. The
// default version is server-assigned at create (version 1) and changes when a
// default-version modify runs, so the cloud value is exposed for a consumer that
// wants the real current default.
type LaunchTemplateOutput struct {
	LaunchTemplateId string `ub:"launch-template-id"`
	LatestVersion    int64  `ub:"latest-version"`
	DefaultVersion   int64  `ub:"default-version"`
}

func (r *LaunchTemplate) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that force a replacement. Only the template
// name is fixed at creation; every instance-config change becomes a new version
// in place.
func (r *LaunchTemplate) ReplaceFields() []string {
	return []string{"name"}
}

// Defaults marks the tag map a launch template may omit. The optional
// collections inside the data block are pointers instead, which is what makes
// a nested field omittable to the type checker.
func (r LaunchTemplate) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the cross-field and value rules the launch-template API
// enforces. The top-level pair default-version and update-default-version cannot
// both be given. Under the data block, the security-group naming pair, the IAM
// instance profile, the placement group and host, and the Capacity Reservation
// target are each mutually exclusive choices, and the enum and numeric-bound
// fields take only their valid values. The per-element rules inside the
// block-device-mappings and network-interfaces lists do not derive, since an
// omittable list is a pointer and a constraint cannot iterate through one: the
// count-or-list choice for a network interface's IPv4 and IPv6 addresses is
// checked in code when the version is built, and the element enums and bounds
// are validated by AWS, as are the instance-type burstable gate and the name
// charset.
func (r LaunchTemplate) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.DefaultVersion, r.UpdateDefaultVersion),
		constraint.AtMostOneOf(r.Data.SecurityGroups, r.Data.SecurityGroupIds),
		constraint.AtMostOneOf(r.Data.IamInstanceProfile.Arn, r.Data.IamInstanceProfile.Name),
		constraint.AtMostOneOf(r.Data.Placement.GroupId, r.Data.Placement.GroupName),
		constraint.AtMostOneOf(r.Data.Placement.HostResourceGroupArn, r.Data.Placement.HostId),
		constraint.AtMostOneOf(
			r.Data.CapacityReservationSpecification.CapacityReservationTarget.CapacityReservationId,
			r.Data.CapacityReservationSpecification.CapacityReservationTarget.
				CapacityReservationResourceGroupArn),
		constraint.When(constraint.Present(
			r.Data.CapacityReservationSpecification.CapacityReservationPreference)).
			Require(constraint.OneOf(
				r.Data.CapacityReservationSpecification.CapacityReservationPreference,
				"capacity-reservations-only", "open", "none")).
			Message("capacity-reservation-preference must be capacity-reservations-only, open, or none"),
		constraint.When(constraint.Present(r.VersionDescription)).
			Require(constraint.AtMost(r.VersionDescription, 255)).
			Message("version-description must be at most 255 characters"),
		constraint.When(constraint.Present(r.Data.InstanceInitiatedShutdownBehavior)).
			Require(constraint.OneOf(r.Data.InstanceInitiatedShutdownBehavior, "stop", "terminate")).
			Message("instance-initiated-shutdown-behavior must be stop or terminate"),
		constraint.When(constraint.Present(r.Data.CreditSpecification.CpuCredits)).
			Require(constraint.OneOf(r.Data.CreditSpecification.CpuCredits, "standard", "unlimited")).
			Message("credit-specification cpu-credits must be standard or unlimited"),
		constraint.When(constraint.Present(r.Data.CpuOptions.AmdSevSnp)).
			Require(constraint.OneOf(r.Data.CpuOptions.AmdSevSnp, "enabled", "disabled")).
			Message("cpu-options amd-sev-snp must be enabled or disabled"),
		constraint.When(constraint.Present(r.Data.CpuOptions.NestedVirtualization)).
			Require(constraint.OneOf(r.Data.CpuOptions.NestedVirtualization, "enabled", "disabled")).
			Message("cpu-options nested-virtualization must be enabled or disabled"),
		constraint.When(constraint.Present(r.Data.Placement.Tenancy)).
			Require(constraint.OneOf(r.Data.Placement.Tenancy, "default", "dedicated", "host")).
			Message("placement tenancy must be default, dedicated, or host"),
		constraint.When(constraint.Present(r.Data.PrivateDnsNameOptions.HostnameType)).
			Require(constraint.OneOf(r.Data.PrivateDnsNameOptions.HostnameType,
				"ip-name", "resource-name")).
			Message("private-dns-name-options hostname-type must be ip-name or resource-name"),
		constraint.When(constraint.Present(r.Data.MaintenanceOptions.AutoRecovery)).
			Require(constraint.OneOf(r.Data.MaintenanceOptions.AutoRecovery, "default", "disabled")).
			Message("maintenance-options auto-recovery must be default or disabled"),
		constraint.When(constraint.Present(r.Data.NetworkPerformanceOptions.BandwidthWeighting)).
			Require(constraint.OneOf(r.Data.NetworkPerformanceOptions.BandwidthWeighting,
				"default", "vpc-1", "ebs-1")).
			Message("network-performance-options bandwidth-weighting must be default, vpc-1, or ebs-1"),
		constraint.When(constraint.Present(r.Data.InstanceMarketOptions.MarketType)).
			Require(constraint.OneOf(r.Data.InstanceMarketOptions.MarketType,
				"spot", "capacity-block", "interruptible-capacity-reservation")).
			Message("instance-market-options market-type must be a valid market type"),
		constraint.When(constraint.Present(
			r.Data.InstanceMarketOptions.SpotOptions.InstanceInterruptionBehavior)).
			Require(constraint.OneOf(
				r.Data.InstanceMarketOptions.SpotOptions.InstanceInterruptionBehavior,
				"hibernate", "stop", "terminate")).
			Message("spot-options instance-interruption-behavior must be hibernate, stop, or terminate"),
		constraint.When(constraint.Present(r.Data.InstanceMarketOptions.SpotOptions.SpotInstanceType)).
			Require(constraint.OneOf(r.Data.InstanceMarketOptions.SpotOptions.SpotInstanceType,
				"one-time", "persistent")).
			Message("spot-options spot-instance-type must be one-time or persistent"),
		constraint.When(constraint.Present(r.Data.MetadataOptions.HttpEndpoint)).
			Require(constraint.OneOf(r.Data.MetadataOptions.HttpEndpoint, "enabled", "disabled")).
			Message("metadata-options http-endpoint must be enabled or disabled"),
		constraint.When(constraint.Present(r.Data.MetadataOptions.HttpTokens)).
			Require(constraint.OneOf(r.Data.MetadataOptions.HttpTokens, "optional", "required")).
			Message("metadata-options http-tokens must be optional or required"),
		constraint.When(constraint.Present(r.Data.MetadataOptions.HttpProtocolIpv6)).
			Require(constraint.OneOf(r.Data.MetadataOptions.HttpProtocolIpv6, "enabled", "disabled")).
			Message("metadata-options http-protocol-ipv6 must be enabled or disabled"),
		constraint.When(constraint.Present(r.Data.MetadataOptions.InstanceMetadataTags)).
			Require(constraint.OneOf(r.Data.MetadataOptions.InstanceMetadataTags,
				"enabled", "disabled")).
			Message("metadata-options instance-metadata-tags must be enabled or disabled"),
		constraint.When(constraint.Present(r.Data.MetadataOptions.HttpPutResponseHopLimit)).
			Require(constraint.AtLeast(r.Data.MetadataOptions.HttpPutResponseHopLimit, 1),
				constraint.AtMost(r.Data.MetadataOptions.HttpPutResponseHopLimit, 64)).
			Message("metadata-options http-put-response-hop-limit must be between 1 and 64"),
	}
}

func (r *LaunchTemplate) Create(ctx context.Context, cfg any) (*LaunchTemplateOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	data, err := launchTemplateData(r.Data)
	if err != nil {
		return nil, err
	}
	in := &ec2.CreateLaunchTemplateInput{
		LaunchTemplateName: aws.String(r.Name),
		LaunchTemplateData: data,
		VersionDescription: r.VersionDescription,
		TagSpecifications:  tagSpecifications(ec2types.ResourceTypeLaunchTemplate, r.Tags),
	}
	// Some partitions, such as the ISO partitions, cannot tag a launch template
	// as it is created. When the tagged create fails for that reason, create the
	// template without tags and apply them with a separate call below.
	taggedSeparately := false
	resp, err := client.CreateLaunchTemplate(ctx, in)
	if err != nil && in.TagSpecifications != nil &&
		partition.UnsupportedOperation(region(client), err) {
		in.TagSpecifications = nil
		taggedSeparately = true
		resp, err = client.CreateLaunchTemplate(ctx, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create launch template: %w", err)
	}
	id := aws.ToString(resp.LaunchTemplate.LaunchTemplateId)
	if taggedSeparately && len(r.Tags) > 0 {
		if err := syncTags(ctx, client, id, r.Tags); err != nil {
			return nil, err
		}
	}
	// A just-created template is eventually consistent: a describe can briefly
	// report it absent or echo a stale id. Poll by name, the value known before
	// the id is committed, until it reads as present a few times in a row before
	// trusting it, so the follow-on read does not race the create. AWS makes
	// version 1 the default at create; default-version and update-default-version
	// act only in Update.
	if err := r.waitFound(ctx, client, r.Name, true); err != nil {
		return nil, err
	}
	return r.read(ctx, client, id)
}

func (r *LaunchTemplate) Read(
	ctx context.Context, cfg any, prior *LaunchTemplateOutput,
) (*LaunchTemplateOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.LaunchTemplateId)
}

func (r *LaunchTemplate) Update(
	ctx context.Context, cfg any, prior runtime.Prior[LaunchTemplate, *LaunchTemplateOutput],
) (*LaunchTemplateOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	id := prior.Outputs.LaunchTemplateId
	// Step A: when the instance configuration or the version description changed,
	// build a whole new version from the desired inputs. The new version becomes
	// the latest, but it does not become the default unless step B promotes it.
	versionCreated := false
	var latestVersion int64
	if r.dataChanged(prior.Inputs) {
		latestVersion, err = r.createVersion(ctx, client, id)
		if err != nil {
			return nil, err
		}
		versionCreated = true
	}
	// Step B: promote a default version. update-default-version promotes the
	// version just created this run, so it only acts when step A made one;
	// default-version pins an explicit number and acts when its input changed.
	// One call, gated so it never fires without a real change.
	switch {
	case aws.ToBool(r.UpdateDefaultVersion) && versionCreated:
		if err := r.modifyDefaultVersion(ctx, client, id,
			strconv.FormatInt(latestVersion, 10)); err != nil {
			return nil, err
		}
	case runtime.Changed(prior.Inputs.DefaultVersion, r.DefaultVersion) && r.DefaultVersion != nil:
		if err := r.modifyDefaultVersion(ctx, client, id,
			strconv.FormatInt(*r.DefaultVersion, 10)); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, id, r.Tags); err != nil {
			return nil, err
		}
	}
	if err := r.waitFound(ctx, client, id, false); err != nil {
		return nil, err
	}
	return r.read(ctx, client, id)
}

func (r *LaunchTemplate) Delete(ctx context.Context, cfg any, prior *LaunchTemplateOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteLaunchTemplate(ctx, &ec2.DeleteLaunchTemplateInput{
		LaunchTemplateId: aws.String(prior.LaunchTemplateId),
	})
	if err != nil {
		// A template that is already gone is a successful delete with nothing to
		// do. Deleting a template removes all of its versions.
		if isNotFound(err, "InvalidLaunchTemplateId.NotFound") {
			return nil
		}
		return fmt.Errorf("delete launch template: %w", err)
	}
	return nil
}

// dataChanged reports whether the inputs that go into a launch-template version
// differ from the prior apply: the whole data block plus the version
// description. A version is built whole from these, so a change to any of them
// produces a new version.
func (r *LaunchTemplate) dataChanged(prior LaunchTemplate) bool {
	return runtime.Changed(prior.Data, r.Data) ||
		runtime.Changed(prior.VersionDescription, r.VersionDescription)
}

// createVersion builds a whole new launch-template version from the desired
// inputs and returns its version number. The version request is the same data
// the create call sends, so a version reflects exactly the declared
// configuration.
func (r *LaunchTemplate) createVersion(
	ctx context.Context, client *ec2.Client, id string,
) (int64, error) {
	data, err := launchTemplateData(r.Data)
	if err != nil {
		return 0, err
	}
	resp, err := client.CreateLaunchTemplateVersion(ctx,
		&ec2.CreateLaunchTemplateVersionInput{
			LaunchTemplateId:   aws.String(id),
			LaunchTemplateData: data,
			VersionDescription: r.VersionDescription,
		})
	if err != nil {
		return 0, fmt.Errorf("create launch template version: %w", err)
	}
	return aws.ToInt64(resp.LaunchTemplateVersion.VersionNumber), nil
}

// modifyDefaultVersion sets the template's default version to the given number.
func (r *LaunchTemplate) modifyDefaultVersion(
	ctx context.Context, client *ec2.Client, id, version string,
) error {
	_, err := client.ModifyLaunchTemplate(ctx, &ec2.ModifyLaunchTemplateInput{
		LaunchTemplateId: aws.String(id),
		DefaultVersion:   aws.String(version),
	})
	if err != nil {
		return fmt.Errorf("modify launch template default version: %w", err)
	}
	return nil
}

// waitFound polls until the template is consistently visible. EC2's read after a
// write lags, so the template must be returned by three consecutive describes
// before the wait passes. Create polls by name, the value known before the id is
// committed; update polls by id.
func (r *LaunchTemplate) waitFound(
	ctx context.Context, client *ec2.Client, idOrName string, byName bool,
) error {
	what := fmt.Sprintf("launch template %s", idOrName)
	probe := func(ctx context.Context) (bool, error) {
		in := &ec2.DescribeLaunchTemplatesInput{}
		if byName {
			in.LaunchTemplateNames = []string{idOrName}
		} else {
			in.LaunchTemplateIds = []string{idOrName}
		}
		resp, err := client.DescribeLaunchTemplates(ctx, in)
		if err != nil {
			if isNotFound(err, launchTemplateNotFoundCodes...) {
				return false, nil
			}
			return false, fmt.Errorf("describe launch templates: %w", err)
		}
		return len(resp.LaunchTemplates) > 0, nil
	}
	return wait.UntilStable(ctx, what, 3, probe, wait.WithInterval(time.Second))
}

// read fetches the template and composes the output. The describe response
// holds the id and the latest and default version numbers; no nested instance
// configuration is echoed into the output.
func (r *LaunchTemplate) read(
	ctx context.Context, client *ec2.Client, id string,
) (*LaunchTemplateOutput, error) {
	tmpl, err := describeLaunchTemplate(ctx, client, id)
	if err != nil {
		return nil, err
	}
	return &LaunchTemplateOutput{
		LaunchTemplateId: id,
		LatestVersion:    aws.ToInt64(tmpl.LatestVersionNumber),
		DefaultVersion:   aws.ToInt64(tmpl.DefaultVersionNumber),
	}, nil
}

// launchTemplateNotFoundCodes are the EC2 error codes a describe-templates call
// returns when the template is gone: a malformed id, an unknown id, or an
// unknown name. A Read maps any of them, an empty result, or an id mismatch to
// runtime.ErrNotFound so a deleted template drives a recreate.
var launchTemplateNotFoundCodes = []string{
	"InvalidLaunchTemplateId.Malformed",
	"InvalidLaunchTemplateId.NotFound",
	"InvalidLaunchTemplateName.NotFoundException",
}

// describeLaunchTemplate fetches the template with the given id. EC2 reports a
// missing template by one of three service codes on an HTTP 400, never a 404, so
// each maps to runtime.ErrNotFound; an empty result or an echoed id that does
// not match the one asked for means the same, an eventual-consistency guard
// against a stale echo.
func describeLaunchTemplate(
	ctx context.Context, client *ec2.Client, id string,
) (*ec2types.LaunchTemplate, error) {
	resp, err := client.DescribeLaunchTemplates(ctx, &ec2.DescribeLaunchTemplatesInput{
		LaunchTemplateIds: []string{id},
	})
	if err != nil {
		if isNotFound(err, launchTemplateNotFoundCodes...) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe launch templates: %w", err)
	}
	if len(resp.LaunchTemplates) == 0 {
		return nil, runtime.ErrNotFound
	}
	tmpl := resp.LaunchTemplates[0]
	if aws.ToString(tmpl.LaunchTemplateId) != id {
		return nil, runtime.ErrNotFound
	}
	return &tmpl, nil
}
