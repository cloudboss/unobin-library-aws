package ec2

import (
	"cmp"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/constraint"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type ManagedPrefixListResource struct {
	AddressFamily string                    `ub:"address-family"`
	MaxEntries    int64                     `ub:"max-entries"`
	Name          string                    `ub:"name"`
	Entries       *[]ManagedPrefixListEntry `ub:"entries"`
	Tags          *map[string]string        `ub:"tags"`
}

type ManagedPrefixListEntry struct {
	Cidr        string  `ub:"cidr"`
	Description *string `ub:"description"`
}

type ManagedPrefixListResourceOutput struct {
	PrefixListId  string                   `ub:"prefix-list-id"`
	AddressFamily string                   `ub:"address-family"`
	Arn           string                   `ub:"arn"`
	Entries       []ManagedPrefixListEntry `ub:"entries"`
	MaxEntries    int64                    `ub:"max-entries"`
	Name          string                   `ub:"name"`
	OwnerId       string                   `ub:"owner-id"`
	Tags          map[string]string        `ub:"tags"`
	Version       int64                    `ub:"version"`
	Region        string                   `ub:"region"`
}

type managedPrefixListPrior = runtime.Prior[
	ManagedPrefixListResource, *ManagedPrefixListResourceOutput, *awsCfg,
]

func (r *ManagedPrefixListResource) prepareRequest(token string) *ec2.CreateManagedPrefixListInput {
	tags, _ := tagsync.Diff(nil, ptr.Value(r.Tags))
	request := &ec2.CreateManagedPrefixListInput{
		AddressFamily: aws.String(r.AddressFamily), MaxEntries: aws.Int32(int32(r.MaxEntries)),
		PrefixListName: aws.String(r.Name), ClientToken: aws.String(token),
		TagSpecifications: tagSpecifications(ec2types.ResourceTypePrefixList, tags),
	}
	for _, entry := range managedPrefixListEntries(ptr.Value(r.Entries)) {
		request.Entries = append(request.Entries, ec2types.AddPrefixListEntry{
			Cidr: aws.String(entry.Cidr), Description: entry.Description,
		})
	}
	return request
}

func managedPrefixListEntries(entries []ManagedPrefixListEntry) []ManagedPrefixListEntry {
	result := slices.Clone(entries)
	for i := range result {
		if aws.ToString(result[i].Description) == "" {
			result[i].Description = nil
		}
	}
	slices.SortFunc(result, func(a, b ManagedPrefixListEntry) int {
		if order := cmp.Compare(a.Cidr, b.Cidr); order != 0 {
			return order
		}
		return cmp.Compare(aws.ToString(a.Description), aws.ToString(b.Description))
	})
	result = slices.CompactFunc(result, func(a, b ManagedPrefixListEntry) bool {
		return a.Cidr == b.Cidr && aws.ToString(a.Description) == aws.ToString(b.Description)
	})
	if result == nil {
		return []ManagedPrefixListEntry{}
	}
	return result
}

func managedPrefixListEntriesEqual(prior, desired *[]ManagedPrefixListEntry) bool {
	if desired == nil {
		return true
	}
	if prior == nil {
		return false
	}
	return slices.EqualFunc(managedPrefixListEntries(*prior), managedPrefixListEntries(*desired),
		func(a, b ManagedPrefixListEntry) bool {
			return a.Cidr == b.Cidr && aws.ToString(a.Description) == aws.ToString(b.Description)
		})
}

func (r ManagedPrefixListResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.AddressFamily, "IPv4", "IPv6")).
			Message("address-family must be IPv4 or IPv6"),
		constraint.Must(constraint.AtLeast(r.MaxEntries, 1)).
			Message("max-entries must be at least 1"),
		constraint.Must(
			constraint.AtLeast(constraint.ByteLength(r.Name), 1),
			constraint.AtMost(constraint.ByteLength(r.Name), 255),
		).Message("name must contain 1..255 bytes"),
		constraint.ForEach(r.Entries, func(entry ManagedPrefixListEntry) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.IsFormat(entry.Cidr, constraint.FormatCIDR)).
					Message("entries cidr must be a valid CIDR"),
				constraint.When(constraint.Present(entry.Description)).Require(
					constraint.AtMost(constraint.ByteLength(entry.Description), 255),
				).
					Message("entries description must contain at most 255 bytes"),
			}
		}),
	}
}

func (*ManagedPrefixListResource) ResourceDefinition() runtime.ResourceDefinition[
	ManagedPrefixListResource, *ManagedPrefixListResourceOutput, *awsCfg,
] {
	return runtime.ResourceDefinition[
		ManagedPrefixListResource, *ManagedPrefixListResourceOutput, *awsCfg,
	]{
		SchemaVersion: 1,
		Replace: runtime.Replacement[
			ManagedPrefixListResource, *ManagedPrefixListResourceOutput, *awsCfg,
		]{
			Fields: []runtime.AnyInputField[ManagedPrefixListResource]{
				runtime.InputField(func(r *ManagedPrefixListResource) *string { return &r.AddressFamily }),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		Equality: []runtime.InputEqualityRule[ManagedPrefixListResource]{
			runtime.EqualBy(runtime.InputField(
				func(r *ManagedPrefixListResource) **[]ManagedPrefixListEntry { return &r.Entries },
			), managedPrefixListEntriesEqual),
		},
		StableID: func(
			_ ManagedPrefixListResource, out *ManagedPrefixListResourceOutput,
		) (string, error) {
			if out == nil || out.PrefixListId == "" {
				return "", runtime.ErrNotFound
			}
			return out.PrefixListId, nil
		},
	}
}
