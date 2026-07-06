package iam

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

// OpenIDConnectProviderResource manages an IAM OpenID Connect (OIDC) identity provider.
type OpenIDConnectProviderResource struct {
	Url            string             `ub:"url"`
	ClientIDList   []string           `ub:"client-id-list"`
	ThumbprintList *[]string          `ub:"thumbprint-list"`
	Tags           *map[string]string `ub:"tags"`
}

// OpenIDConnectProviderResourceOutput holds attributes that IAM computes for the
// provider. ThumbprintList is here as well as on the input because IAM derives
// it from the provider's certificate chain when the input omits it; carrying
// the read-back value lets a reference to thumbprint-list resolve to what IAM
// actually used rather than the empty input. URL, client ID list, and tags are
// returned unchanged from the input, so they are not echoed.
type OpenIDConnectProviderResourceOutput struct {
	Arn            string   `ub:"arn"`
	CreateDate     string   `ub:"create-date"`
	ThumbprintList []string `ub:"thumbprint-list"`
}

// SchemaVersion reports the schema version of the resource state.
func (r *OpenIDConnectProviderResource) SchemaVersion() int {
	return 1
}

// ReplaceFields lists the input fields that force the provider to be replaced.
// Only the URL is immutable; the client ID list, thumbprint list, and tags all
// change in place.
func (r *OpenIDConnectProviderResource) ReplaceFields() []string {
	return []string{"url"}
}

func (r OpenIDConnectProviderResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.ClientIDList)).
			Message("client-id-list must not be empty"),
		constraint.When(constraint.Present(r.ThumbprintList)).
			Require(constraint.MinItems(r.ThumbprintList, 1)).
			Message("thumbprint-list must not be empty when given"),
	}
}

// Create provisions a new IAM OIDC provider. When the thumbprint list is
// omitted, IAM derives it from the provider's certificate chain.
func (r *OpenIDConnectProviderResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*OpenIDConnectProviderResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	input := &iam.CreateOpenIDConnectProviderInput{
		Url:            aws.String(r.Url),
		ClientIDList:   r.ClientIDList,
		ThumbprintList: ptr.Value(r.ThumbprintList),
		Tags:           oidcProviderTags(ptr.Value(r.Tags)),
	}
	resp, err := client.CreateOpenIDConnectProvider(ctx, input)
	// Some partitions, such as the ISO partitions, cannot tag a provider as
	// it is created. When the tagged create fails for that reason, create the
	// provider without tags and apply them with a separate call below.
	taggedSeparately := false
	if err != nil && input.Tags != nil &&
		partition.UnsupportedOperation(region(client), err) {
		input.Tags = nil
		taggedSeparately = true
		resp, err = client.CreateOpenIDConnectProvider(ctx, input)
	}
	if err != nil {
		return nil, fmt.Errorf("create iam oidc provider: %w", err)
	}
	arn := aws.ToString(resp.OpenIDConnectProviderArn)
	if taggedSeparately && len(ptr.Value(r.Tags)) > 0 {
		if err := oidcProviderSyncTags(ctx, client, arn, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return oidcProviderRead(ctx, client, arn)
}

// Read fetches the current state of the IAM OIDC provider.
func (r *OpenIDConnectProviderResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *OpenIDConnectProviderResourceOutput) (*OpenIDConnectProviderResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return oidcProviderRead(ctx, client, prior.Arn)
}

// Update applies in-place changes to the IAM OIDC provider: a replaced
// thumbprint list, added or removed client IDs, and changed tags.
func (r *OpenIDConnectProviderResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[OpenIDConnectProviderResource, *OpenIDConnectProviderResourceOutput],
) (*OpenIDConnectProviderResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	// IAM rejects an empty thumbprint list on update, so only push a non-empty
	// list. An omitted list leaves the thumbprints IAM derived in place.
	if r.ThumbprintList != nil &&
		runtime.Changed(ptr.Value(prior.Inputs.ThumbprintList), ptr.Value(r.ThumbprintList)) &&
		len(*r.ThumbprintList) > 0 {
		_, err := client.UpdateOpenIDConnectProviderThumbprint(
			ctx,
			&iam.UpdateOpenIDConnectProviderThumbprintInput{
				OpenIDConnectProviderArn: aws.String(arn),
				ThumbprintList:           *r.ThumbprintList,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("update iam oidc provider thumbprint: %w", err)
		}
	}
	if runtime.Changed(prior.Inputs.ClientIDList, r.ClientIDList) {
		err := oidcProviderSyncClientIDs(ctx, client, arn, prior.Inputs.ClientIDList,
			r.ClientIDList)
		if err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := oidcProviderSyncTags(ctx, client, arn, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return oidcProviderRead(ctx, client, arn)
}

// Delete removes the IAM OIDC provider. A provider already gone is treated as deleted.
func (r *OpenIDConnectProviderResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *OpenIDConnectProviderResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteOpenIDConnectProvider(ctx, &iam.DeleteOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(prior.Arn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete iam oidc provider: %w", err)
	}
	return nil
}

// oidcProviderRead gets the provider by ARN and maps a missing provider to
// runtime.ErrNotFound so a deleted provider drives recreation.
func oidcProviderRead(
	ctx context.Context,
	client *iam.Client,
	arn string,
) (*OpenIDConnectProviderResourceOutput, error) {
	resp, err := client.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(arn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get iam oidc provider: %w", err)
	}
	out := &OpenIDConnectProviderResourceOutput{
		Arn:            arn,
		ThumbprintList: resp.ThumbprintList,
	}
	if resp.CreateDate != nil {
		out.CreateDate = resp.CreateDate.Format(time.RFC3339)
	}
	return out, nil
}

// oidcProviderSyncClientIDs reconciles the client ID list of a provider, adding
// the IDs that are newly desired and removing the ones no longer present. IAM
// has no bulk set call, so each membership change is its own request.
func oidcProviderSyncClientIDs(
	ctx context.Context,
	client *iam.Client,
	arn string,
	prior, desired []string,
) error {
	priorSet := make(map[string]struct{}, len(prior))
	for _, id := range prior {
		priorSet[id] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desired))
	for _, id := range desired {
		desiredSet[id] = struct{}{}
	}
	for _, id := range desired {
		if _, ok := priorSet[id]; ok {
			continue
		}
		_, err := client.AddClientIDToOpenIDConnectProvider(
			ctx,
			&iam.AddClientIDToOpenIDConnectProviderInput{
				OpenIDConnectProviderArn: aws.String(arn),
				ClientID:                 aws.String(id),
			},
		)
		if err != nil {
			return fmt.Errorf("add iam oidc provider client id %q: %w", id, err)
		}
	}
	for _, id := range prior {
		if _, ok := desiredSet[id]; ok {
			continue
		}
		_, err := client.RemoveClientIDFromOpenIDConnectProvider(
			ctx,
			&iam.RemoveClientIDFromOpenIDConnectProviderInput{
				OpenIDConnectProviderArn: aws.String(arn),
				ClientID:                 aws.String(id),
			},
		)
		if err != nil {
			return fmt.Errorf("remove iam oidc provider client id %q: %w", id, err)
		}
	}
	return nil
}

// oidcProviderSyncTags reconciles the provider's tags with desired, delegating
// the diff and ordering to tagsync.Sync and supplying IAM's own tag verbs.
func oidcProviderSyncTags(
	ctx context.Context,
	client *iam.Client,
	arn string,
	desired map[string]string,
) error {
	return tagsync.Sync(ctx, desired,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListOpenIDConnectProviderTags(
				ctx,
				&iam.ListOpenIDConnectProviderTagsInput{
					OpenIDConnectProviderArn: aws.String(arn),
				},
			)
			if err != nil {
				return nil, fmt.Errorf("list iam oidc provider tags: %w", err)
			}
			current := make(map[string]string, len(resp.Tags))
			for _, t := range resp.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.TagOpenIDConnectProvider(
				ctx,
				&iam.TagOpenIDConnectProviderInput{
					OpenIDConnectProviderArn: aws.String(arn),
					Tags:                     oidcProviderTags(upsert),
				},
			); err != nil {
				return fmt.Errorf("tag iam oidc provider: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.UntagOpenIDConnectProvider(
				ctx,
				&iam.UntagOpenIDConnectProviderInput{
					OpenIDConnectProviderArn: aws.String(arn),
					TagKeys:                  remove,
				},
			); err != nil {
				return fmt.Errorf("untag iam oidc provider: %w", err)
			}
			return nil
		},
	)
}

// oidcProviderTags converts a desired tag map into the IAM tag list,
// returning nil for an empty map so an unset value is omitted from the request.
func oidcProviderTags(tags map[string]string) []iamtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]iamtypes.Tag, 0, len(tags))
	for key, value := range tags {
		out = append(out, iamtypes.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return out
}
