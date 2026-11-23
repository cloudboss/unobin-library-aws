package wafv2

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	awsretry "github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

const ipSetNameSuffixBytes = 13

type IPSetResource struct {
	Scope            string             `ub:"scope"`
	IPAddressVersion string             `ub:"ip-address-version"`
	Name             *string            `ub:"name"`
	NamePrefix       *string            `ub:"name-prefix"`
	Description      *string            `ub:"description"`
	Addresses        *[]string          `ub:"addresses"`
	Tags             *map[string]string `ub:"tags"`
}

type IPSetResourceOutput struct {
	ID               string            `ub:"id"`
	ARN              string            `ub:"arn"`
	LockToken        string            `ub:"lock-token"`
	Region           string            `ub:"region"`
	Scope            string            `ub:"scope"`
	Name             string            `ub:"name"`
	NamePrefix       *string           `ub:"name-prefix"`
	Description      *string           `ub:"description"`
	IPAddressVersion string            `ub:"ip-address-version"`
	Addresses        []string          `ub:"addresses"`
	Tags             map[string]string `ub:"tags"`
}

type ipSetPrior = runtime.Prior[IPSetResource, *IPSetResourceOutput, *awsCfg]
type ipSetDefinition = runtime.ResourceDefinition[IPSetResource, *IPSetResourceOutput, *awsCfg]

type ipSetClient interface {
	CreateIPSet(context.Context, *awssvc.CreateIPSetInput,
		...func(*awssvc.Options)) (*awssvc.CreateIPSetOutput, error)
	GetIPSet(context.Context, *awssvc.GetIPSetInput,
		...func(*awssvc.Options)) (*awssvc.GetIPSetOutput, error)
	UpdateIPSet(context.Context, *awssvc.UpdateIPSetInput,
		...func(*awssvc.Options)) (*awssvc.UpdateIPSetOutput, error)
	DeleteIPSet(context.Context, *awssvc.DeleteIPSetInput,
		...func(*awssvc.Options)) (*awssvc.DeleteIPSetOutput, error)
	ListTagsForResource(context.Context, *awssvc.ListTagsForResourceInput,
		...func(*awssvc.Options)) (*awssvc.ListTagsForResourceOutput, error)
	TagResource(context.Context, *awssvc.TagResourceInput,
		...func(*awssvc.Options)) (*awssvc.TagResourceOutput, error)
	UntagResource(context.Context, *awssvc.UntagResourceInput,
		...func(*awssvc.Options)) (*awssvc.UntagResourceOutput, error)
}

func (r IPSetResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.Name, r.NamePrefix),
		constraint.Must(constraint.OneOf(r.Scope, "REGIONAL", "CLOUDFRONT")),
		constraint.Must(constraint.OneOf(r.IPAddressVersion, "IPV4", "IPV6")),
		constraint.Must(constraint.IsMatch(r.Name, "^[0-9A-Za-z_-]{1,128}$")),
		constraint.Must(constraint.IsMatch(r.NamePrefix, "^[0-9A-Za-z_-]{1,102}$")),
		constraint.When(constraint.Present(r.Description)).Require(
			constraint.AtLeast(constraint.ByteLength(r.Description), 1),
			constraint.AtMost(constraint.ByteLength(r.Description), 256),
		).Message("description must be 1 to 256 bytes"),
		constraint.Must(constraint.Expr(
			"@core.all([ for address in input.addresses ?? [] : address != null ])",
			constraint.Bind("addresses", r.Addresses),
		)).Message("address must not be null"),
		constraint.Must(constraint.Expr(
			`@core.length({
				for address in input.addresses ?? [] : address => true... when address != null
			}) <= 10000`,
			constraint.Bind("addresses", r.Addresses),
		)).Message("addresses must contain at most 10000 distinct strings"),
	}
}

func (r *IPSetResource) ResourceDefinition() ipSetDefinition {
	addresses := runtime.InputField(func(input *IPSetResource) **[]string {
		return &input.Addresses
	})
	description := runtime.InputField(func(input *IPSetResource) **string {
		return &input.Description
	})
	tags := runtime.InputField(func(input *IPSetResource) **map[string]string {
		return &input.Tags
	})
	return ipSetDefinition{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[IPSetResource]{
			runtime.EqualBy(addresses, func(prior, desired *[]string) bool {
				return ipSetAddressesEqual(ptr.Value(prior), ptr.Value(desired))
			}),
			runtime.EqualBy(description, func(prior, desired *string) bool {
				return aws.ToString(prior) == aws.ToString(desired)
			}),
			runtime.EqualBy(tags, func(prior, desired *map[string]string) bool {
				return maps.Equal(ipSetUserTags(ptr.Value(prior)), ipSetUserTags(ptr.Value(desired)))
			}),
		},
		Replace: runtime.Replacement[IPSetResource, *IPSetResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[IPSetResource]{
				runtime.InputField(func(input *IPSetResource) **string { return &input.Name }),
				runtime.InputField(func(input *IPSetResource) **string { return &input.NamePrefix }),
				runtime.InputField(func(input *IPSetResource) *string { return &input.Scope }),
				runtime.InputField(func(input *IPSetResource) *string {
					return &input.IPAddressVersion
				}),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(_ IPSetResource, output *IPSetResourceOutput) (string, error) {
			if err := checkIPSetIdentity(output); err != nil {
				return "", err
			}
			return output.ARN, nil
		},
	}
}

func (r *IPSetResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*IPSetResourceOutput, error) {
	if r.Scope == string(awstypes.ScopeCloudfront) {
		cloudfront := ptr.Value(cfg)
		cloudfront.Region = aws.String("us-east-1")
		cfg = &cloudfront
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createIPSet(ctx, client, client.Options().Region, rand.Reader)
}

func (r *IPSetResource) Read(
	ctx context.Context, cfg *awsCfg, prior ipSetPrior,
) (*IPSetResourceOutput, error) {
	client, err := existingIPSetClient(ctx, ipSetPriorConfig(cfg, prior), prior.Outputs)
	if err != nil {
		return nil, err
	}
	return readIPSet(ctx, client, *prior.Outputs, true)
}

func (r *IPSetResource) Update(
	ctx context.Context, cfg *awsCfg, prior ipSetPrior,
) (*IPSetResourceOutput, error) {
	client, err := existingIPSetClient(ctx, cfg, prior.Outputs)
	if err != nil {
		return nil, err
	}
	return r.updateIPSet(ctx, client, prior)
}

func (r *IPSetResource) Delete(ctx context.Context, cfg *awsCfg, prior ipSetPrior) error {
	client, err := existingIPSetClient(ctx, ipSetPriorConfig(cfg, prior), prior.Outputs)
	if err != nil {
		return err
	}
	return deleteIPSet(ctx, client, prior)
}

func ipSetPriorConfig(cfg *awsCfg, prior ipSetPrior) *awsCfg {
	if prior.Configuration != nil {
		return prior.Configuration
	}
	return cfg
}

func existingIPSetClient(
	ctx context.Context, cfg *awsCfg, output *IPSetResourceOutput,
) (*awssvc.Client, error) {
	if err := checkIPSetIdentity(output); err != nil {
		return nil, err
	}
	saved := ptr.Value(cfg)
	saved.Region = aws.String(output.Region)
	return newClient(ctx, &saved)
}

func (r *IPSetResource) createIPSet(
	ctx context.Context, client ipSetClient, region string, random io.Reader,
) (*IPSetResourceOutput, error) {
	name, err := r.ipSetName(random)
	if err != nil {
		return nil, err
	}
	addresses, description, tags := r.prepareIPSet()
	input := &awssvc.CreateIPSetInput{
		Name:             aws.String(name),
		Scope:            awstypes.Scope(r.Scope),
		IPAddressVersion: awstypes.IPAddressVersion(r.IPAddressVersion),
		Addresses:        addresses,
		Description:      description,
	}
	if len(tags) > 0 {
		input.Tags = expandTags(tags)
	}
	created, err := client.CreateIPSet(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("create IP set %s: %w", name, err)
	}
	if created == nil || created.Summary == nil || aws.ToString(created.Summary.Id) == "" {
		return nil, fmt.Errorf("create IP set %s: response has no IP set ID", name)
	}
	return readIPSet(ctx, client, IPSetResourceOutput{
		ID: aws.ToString(created.Summary.Id), Name: name, Scope: r.Scope, Region: region,
	}, false)
}

func (r *IPSetResource) ipSetName(random io.Reader) (string, error) {
	if r.Name != nil {
		return *r.Name, nil
	}
	prefix := "unobin-"
	if r.NamePrefix != nil {
		prefix = *r.NamePrefix
	}
	var suffix [ipSetNameSuffixBytes]byte
	if _, err := io.ReadFull(random, suffix[:]); err != nil {
		return "", fmt.Errorf("generate IP set name: %w", err)
	}
	return prefix + hex.EncodeToString(suffix[:]), nil
}

func ipSetNamePrefix(name string) *string {
	split := len(name) - hex.EncodedLen(ipSetNameSuffixBytes)
	if split <= 0 {
		return nil
	}
	if _, err := hex.DecodeString(name[split:]); err != nil {
		return nil
	}
	return aws.String(name[:split])
}

func (r *IPSetResource) prepareIPSet() ([]string, *string, map[string]string) {
	addresses := ptr.Value(r.Addresses)
	unique := make(map[string]struct{}, len(addresses))
	prepared := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if _, seen := unique[address]; seen || address == "" {
			continue
		}
		unique[address] = struct{}{}
		prepared = append(prepared, address)
	}
	return prepared, r.Description, ipSetUserTags(ptr.Value(r.Tags))
}

func ipSetAddressesEqual(prior, desired []string) bool {
	return maps.Equal(ipSetAddressKeys(prior), ipSetAddressKeys(desired))
}

type ipSetAddressKey struct {
	address string
	network string
}

func ipSetAddressKeys(addresses []string) map[ipSetAddressKey]struct{} {
	keys := make(map[ipSetAddressKey]struct{}, len(addresses))
	for _, address := range addresses {
		if address == "" {
			continue
		}
		key := ipSetAddressKey{address: address}
		ip, network, err := net.ParseCIDR(address)
		if err == nil {
			key = ipSetAddressKey{address: ip.String(), network: network.String()}
		}
		keys[key] = struct{}{}
	}
	return keys
}

func readIPSet(
	ctx context.Context, client ipSetClient, identity IPSetResourceOutput, established bool,
) (*IPSetResourceOutput, error) {
	response, err := client.GetIPSet(ctx, &awssvc.GetIPSetInput{
		Id: aws.String(identity.ID), Name: aws.String(identity.Name),
		Scope: awstypes.Scope(identity.Scope),
	})
	if err != nil {
		if established && isWebACLNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get IP set %s: %w", identity.ID, err)
	}
	if response == nil || response.IPSet == nil {
		if established {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get IP set %s: response has no IP set", identity.ID)
	}
	set := response.IPSet
	if aws.ToString(set.Id) != identity.ID || aws.ToString(set.Name) != identity.Name ||
		(identity.ARN != "" && aws.ToString(set.ARN) != identity.ARN) {
		return nil, fmt.Errorf("get IP set %s: response identity does not match request", identity.ID)
	}
	identity.ARN = aws.ToString(set.ARN)
	identity.LockToken = aws.ToString(response.LockToken)
	if err := checkIPSetIdentity(&identity); err != nil {
		return nil, fmt.Errorf("get IP set %s: %w", identity.ID, err)
	}
	if identity.LockToken == "" {
		return nil, fmt.Errorf("get IP set %s: response has no lock token", identity.ID)
	}
	identity.NamePrefix = ipSetNamePrefix(identity.Name)
	identity.IPAddressVersion = string(set.IPAddressVersion)
	identity.Description = set.Description
	identity.Addresses = append([]string{}, set.Addresses...)
	slices.Sort(identity.Addresses)
	identity.Tags, err = listIPSetTags(ctx, client, identity)
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func checkIPSetIdentity(output *IPSetResourceOutput) error {
	if output == nil || output.ID == "" || output.Name == "" ||
		output.Scope == "" || output.Region == "" {
		return fmt.Errorf("IP set identity requires ID, name, scope and region")
	}
	parsed, err := awsarn.Parse(output.ARN)
	if err != nil {
		return fmt.Errorf("IP set identity has invalid ARN %q: %w", output.ARN, err)
	}
	scope := "regional"
	if output.Scope == string(awstypes.ScopeCloudfront) {
		scope = "global"
	}
	resource := scope + "/ipset/" + output.Name + "/" + output.ID
	if parsed.Service != "wafv2" || parsed.Region != output.Region || parsed.Resource != resource {
		return fmt.Errorf("IP set identity does not match ARN %q", output.ARN)
	}
	return nil
}

func ipSetMutationState(prior ipSetPrior) (*IPSetResourceOutput, error) {
	saved := prior.Outputs
	if err := checkIPSetIdentity(saved); err != nil {
		return nil, err
	}
	current := saved
	if observed := prior.Observed; observed != nil {
		if observed.ID != saved.ID || observed.ARN != saved.ARN || observed.Name != saved.Name ||
			observed.Scope != saved.Scope || observed.Region != saved.Region {
			return nil, fmt.Errorf("IP set %s observation identity does not match saved state", saved.ID)
		}
		current = observed
	}
	if current.LockToken == "" {
		return nil, fmt.Errorf("IP set %s has no lock token", saved.ID)
	}
	return current, nil
}

func (r *IPSetResource) updateIPSet(
	ctx context.Context, client ipSetClient, prior ipSetPrior,
) (*IPSetResourceOutput, error) {
	current, err := ipSetMutationState(prior)
	if err != nil {
		return nil, err
	}
	addresses, description, tags := r.prepareIPSet()
	if err := syncIPSetTags(ctx, client, current, tags); err != nil {
		return nil, err
	}
	if !ipSetAddressesEqual(current.Addresses, addresses) ||
		aws.ToString(current.Description) != aws.ToString(description) {
		_, err := client.UpdateIPSet(ctx, &awssvc.UpdateIPSetInput{
			Id:          aws.String(current.ID),
			Name:        aws.String(current.Name),
			Scope:       awstypes.Scope(current.Scope),
			LockToken:   aws.String(current.LockToken),
			Description: description,
			Addresses:   addresses,
		})
		if err != nil {
			return nil, fmt.Errorf("update IP set %s: %w", current.ID, err)
		}
	}
	return readIPSet(ctx, client, *current, false)
}

func deleteIPSet(
	ctx context.Context, client ipSetClient, prior ipSetPrior, options ...awsretry.Option,
) error {
	current, err := ipSetMutationState(prior)
	if err != nil {
		return err
	}
	input := &awssvc.DeleteIPSetInput{
		Id:        aws.String(current.ID),
		Name:      aws.String(current.Name),
		Scope:     awstypes.Scope(current.Scope),
		LockToken: aws.String(current.LockToken),
	}
	options = append([]awsretry.Option{awsretry.WithTimeout(5 * time.Minute)}, options...)
	err = awsretry.OnError(ctx, isWebACLAssociated, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := client.DeleteIPSet(ctx, input)
		return err
	}, options...)
	if err != nil && !isWebACLNotFound(err) {
		return fmt.Errorf("delete IP set %s: %w", current.ID, err)
	}
	return nil
}

func ipSetUserTags(tags map[string]string) map[string]string {
	filtered := make(map[string]string, len(tags))
	for key, value := range tags {
		if !strings.HasPrefix(key, "aws:") {
			filtered[key] = value
		}
	}
	return filtered
}

func listIPSetTags(
	ctx context.Context, client ipSetClient, identity IPSetResourceOutput,
) (map[string]string, error) {
	result, err := client.ListTagsForResource(ctx, &awssvc.ListTagsForResourceInput{
		ResourceARN: aws.String(identity.ARN),
	})
	if err != nil && !partition.UnsupportedOperation(identity.Region, err) {
		return nil, fmt.Errorf("list IP set tags %s: %w", identity.ARN, err)
	}
	tags := map[string]string{}
	if err == nil && result != nil && result.TagInfoForResource != nil {
		for _, tag := range result.TagInfoForResource.TagList {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}
	return ipSetUserTags(tags), nil
}

func syncIPSetTags(
	ctx context.Context, client ipSetClient, current *IPSetResourceOutput, desired map[string]string,
) error {
	upserts, removals := tagsync.Diff(current.Tags, desired)
	if len(removals) > 0 {
		_, err := client.UntagResource(ctx, &awssvc.UntagResourceInput{
			ResourceARN: aws.String(current.ARN), TagKeys: removals,
		})
		if err != nil {
			if partition.UnsupportedOperation(current.Region, err) {
				return nil
			}
			return fmt.Errorf("untag IP set %s: %w", current.ARN, err)
		}
	}
	if len(upserts) > 0 {
		_, err := client.TagResource(ctx, &awssvc.TagResourceInput{
			ResourceARN: aws.String(current.ARN), Tags: expandTags(upserts),
		})
		if err != nil && !partition.UnsupportedOperation(current.Region, err) {
			return fmt.Errorf("tag IP set %s: %w", current.ARN, err)
		}
	}
	return nil
}
