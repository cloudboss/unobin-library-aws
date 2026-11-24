package iam

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	sdk "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
)

type SAMLProviderResource struct {
	Name                 string             `ub:"name"`
	SAMLMetadataDocument string             `ub:"saml-metadata-document"`
	Tags                 *map[string]string `ub:"tags"`
}

type SAMLProviderResourceOutput struct {
	Arn                  string            `ub:"arn"`
	Name                 string            `ub:"name"`
	SAMLMetadataDocument string            `ub:"saml-metadata-document"`
	SAMLProviderUUID     string            `ub:"saml-provider-uuid"`
	ValidUntil           *string           `ub:"valid-until"`
	Tags                 map[string]string `ub:"tags"`
}

func (r SAMLProviderResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(
			constraint.AtLeast(constraint.ByteLength(r.Name), 1),
			constraint.AtMost(constraint.ByteLength(r.Name), 128),
		).Message("name must be 1 to 128 bytes"),
		constraint.Must(
			constraint.AtLeast(constraint.ByteLength(r.SAMLMetadataDocument), 1000),
			constraint.AtMost(constraint.ByteLength(r.SAMLMetadataDocument), 10000000),
		).Message("saml-metadata-document must be 1000 to 10000000 bytes"),
	}
}

func (r *SAMLProviderResource) ResourceDefinition() runtime.ResourceDefinition[
	SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg,
] {
	name := runtime.InputField(func(r *SAMLProviderResource) *string { return &r.Name })
	tags := runtime.InputField(func(r *SAMLProviderResource) **map[string]string { return &r.Tags })
	return runtime.ResourceDefinition[SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg]{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[SAMLProviderResource]{
			runtime.EqualBy(tags, func(a, b *map[string]string) bool {
				upsert, remove := tagsync.Diff(ptr.Value(a), ptr.Value(b))
				return len(upsert) == 0 && len(remove) == 0
			}),
		},
		Replace: runtime.Replacement[SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[SAMLProviderResource]{name},
		},
		StableID: func(_ SAMLProviderResource, out *SAMLProviderResourceOutput) (string, error) {
			return samlProviderIdentity(out), nil
		},
	}
}

func (r *SAMLProviderResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*SAMLProviderResourceOutput, error) {
	request, err := r.prepareSAMLProvider("")
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	result, err := client.CreateSAMLProvider(ctx, request.create)
	fallback := err != nil && len(request.create.Tags) > 0 &&
		partition.UnsupportedOperation(region(client), err)
	if fallback {
		request.create.Tags = nil
		result, err = client.CreateSAMLProvider(ctx, request.create)
	}
	if err != nil {
		return nil, fmt.Errorf("create SAML provider %s: %w", r.Name, err)
	}
	if result == nil || aws.ToString(result.SAMLProviderArn) == "" {
		return nil, errors.New("create SAML provider: response holds no ARN")
	}
	identity := aws.ToString(result.SAMLProviderArn)
	if fallback {
		if err := syncSAMLProviderTags(ctx, client, identity, nil, ptr.Value(r.Tags)); err != nil {
			return nil, cleanupSAMLProvider(ctx, client, identity, err)
		}
	}
	out, err := readSAMLProvider(ctx, client, identity, true)
	if err != nil {
		return nil, cleanupSAMLProvider(ctx, client, identity, err)
	}
	return out, nil
}

func (r *SAMLProviderResource) Read(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg],
) (*SAMLProviderResourceOutput, error) {
	identity := samlProviderIdentity(prior.Outputs)
	if _, err := r.prepareSAMLProvider(identity); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return readSAMLProvider(ctx, client, identity, false)
}

func (r *SAMLProviderResource) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg],
) (*SAMLProviderResourceOutput, error) {
	identity := samlProviderIdentity(prior.Outputs)
	request, err := r.prepareSAMLProvider(identity)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	observed := prior.Observed
	if observed == nil {
		observed = prior.Outputs
	}
	var currentTags map[string]string
	if observed != nil {
		currentTags = observed.Tags
	} else {
		currentTags, err = listSAMLProviderTags(ctx, client, identity)
		if err != nil {
			return nil, err
		}
	}
	if err := syncSAMLProviderTags(ctx, client, identity, currentTags, ptr.Value(r.Tags)); err != nil {
		return nil, err
	}
	if prior.Inputs.SAMLMetadataDocument != r.SAMLMetadataDocument ||
		observed == nil || observed.SAMLMetadataDocument != r.SAMLMetadataDocument {
		_, err := client.UpdateSAMLProvider(ctx, &sdk.UpdateSAMLProviderInput{
			SAMLProviderArn:      aws.String(identity),
			SAMLMetadataDocument: request.create.SAMLMetadataDocument,
		})
		if err != nil {
			return nil, fmt.Errorf("update SAML provider %s: %w", identity, err)
		}
	}
	return readSAMLProvider(ctx, client, identity, false)
}

func (r *SAMLProviderResource) Delete(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg],
) error {
	identity := samlProviderIdentity(prior.Outputs)
	if _, err := r.prepareSAMLProvider(identity); err != nil {
		return err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return deleteSAMLProvider(ctx, client, identity)
}

type samlProviderRequest struct {
	create *sdk.CreateSAMLProviderInput
	name   string
}

func (r SAMLProviderResource) prepareSAMLProvider(identity string) (*samlProviderRequest, error) {
	managed, _ := tagsync.Diff(nil, ptr.Value(r.Tags))
	request := &samlProviderRequest{create: &sdk.CreateSAMLProviderInput{
		Name: aws.String(r.Name), SAMLMetadataDocument: aws.String(r.SAMLMetadataDocument),
		Tags: iamRoleTags(managed),
	}}
	if identity != "" {
		parsed, err := arn.Parse(identity)
		if err != nil {
			return nil, fmt.Errorf("parse SAML provider ARN %q: %w", identity, err)
		}
		request.name = strings.TrimPrefix(parsed.Resource, "saml-provider/")
	}
	return request, nil
}

func samlProviderIdentity(out *SAMLProviderResourceOutput) string {
	if out == nil {
		return ""
	}
	return out.Arn
}

type samlProviderReader interface {
	GetSAMLProvider(context.Context, *sdk.GetSAMLProviderInput,
		...func(*sdk.Options)) (*sdk.GetSAMLProviderOutput, error)
}

func readSAMLProvider(ctx context.Context, client samlProviderReader, identity string,
	created bool) (*SAMLProviderResourceOutput, error) {
	if identity == "" {
		return nil, errors.New("SAML provider has no persisted ARN")
	}
	request, err := (SAMLProviderResource{}).prepareSAMLProvider(identity)
	if err != nil {
		return nil, err
	}
	result, err := client.GetSAMLProvider(ctx, &sdk.GetSAMLProviderInput{
		SAMLProviderArn: aws.String(identity),
	})
	if isNotFound(err) || err == nil && result == nil {
		if !created {
			return nil, fmt.Errorf("get SAML provider %s: %w", identity,
				errors.Join(runtime.ErrNotFound, err))
		}
		return nil, errors.Join(fmt.Errorf("initialize SAML provider %s: missing Get result",
			identity), err)
	}
	if err != nil {
		return nil, fmt.Errorf("get SAML provider %s: %w", identity, err)
	}
	out := &SAMLProviderResourceOutput{
		Arn: identity, Name: request.name,
		SAMLMetadataDocument: aws.ToString(result.SAMLMetadataDocument),
		SAMLProviderUUID:     aws.ToString(result.SAMLProviderUUID), Tags: samlManagedTags(result.Tags),
	}
	if result.ValidUntil != nil {
		out.ValidUntil = aws.String(result.ValidUntil.Format(time.RFC3339))
	}
	return out, nil
}

func samlManagedTags(tags []types.Tag) map[string]string {
	out := map[string]string{}
	for _, tag := range tags {
		if key := aws.ToString(tag.Key); !strings.HasPrefix(key, "aws:") {
			out[key] = aws.ToString(tag.Value)
		}
	}
	return out
}

func listSAMLProviderTags(ctx context.Context, client *sdk.Client,
	identity string) (map[string]string, error) {
	out := map[string]string{}
	pager := sdk.NewListSAMLProviderTagsPaginator(client, &sdk.ListSAMLProviderTagsInput{
		SAMLProviderArn: aws.String(identity),
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list SAML provider tags %s: %w", identity, err)
		}
		maps.Copy(out, samlManagedTags(page.Tags))
	}
	return out, nil
}

func syncSAMLProviderTags(ctx context.Context, client *sdk.Client, identity string,
	current, desired map[string]string) error {
	upsert, remove := tagsync.Diff(current, desired)
	if len(remove) > 0 {
		_, err := client.UntagSAMLProvider(ctx, &sdk.UntagSAMLProviderInput{
			SAMLProviderArn: aws.String(identity), TagKeys: remove,
		})
		if err != nil {
			return fmt.Errorf("untag SAML provider %s: %w", identity, err)
		}
	}
	if len(upsert) > 0 {
		_, err := client.TagSAMLProvider(ctx, &sdk.TagSAMLProviderInput{
			SAMLProviderArn: aws.String(identity), Tags: iamRoleTags(upsert),
		})
		if err != nil {
			return fmt.Errorf("tag SAML provider %s: %w", identity, err)
		}
	}
	return nil
}

type samlProviderDeleter interface {
	DeleteSAMLProvider(context.Context, *sdk.DeleteSAMLProviderInput,
		...func(*sdk.Options)) (*sdk.DeleteSAMLProviderOutput, error)
}

func deleteSAMLProvider(ctx context.Context, client samlProviderDeleter, identity string) error {
	_, err := client.DeleteSAMLProvider(ctx, &sdk.DeleteSAMLProviderInput{
		SAMLProviderArn: aws.String(identity),
	})
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete SAML provider %s: %w", identity, err)
	}
	return nil
}

func cleanupSAMLProvider(ctx context.Context, client samlProviderDeleter, identity string,
	failure error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if err := deleteSAMLProvider(cleanup, client, identity); err != nil {
		return errors.Join(failure, fmt.Errorf("compensating delete SAML provider: %w", err))
	}
	return failure
}
