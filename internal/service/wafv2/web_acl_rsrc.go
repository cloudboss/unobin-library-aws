package wafv2

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"

	awsretry "github.com/cloudboss/unobin-library-aws/internal/retry"
)

type awsCfg = awscfg.Configuration

const webACLRetryTimeout = 5 * time.Minute

func (r *WebACLResource) SchemaVersion() int { return 1 }

func (r *WebACLResource) ReplaceFields() []string {
	return []string{"name", "scope", "application-config"}
}

func (r *WebACLResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*WebACLResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, rand.Reader)
}

func (r *WebACLResource) create(
	ctx context.Context,
	client wafClient,
	random io.Reader,
	retryOptions ...awsretry.Option,
) (*WebACLResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	name, err := r.resolveName(random)
	if err != nil {
		return nil, err
	}
	in, err := r.createInput(name)
	if err != nil {
		return nil, fmt.Errorf("build create web ACL input: %w", err)
	}
	var created *awssvc.CreateWebACLOutput
	options := append([]awsretry.Option{awsretry.WithTimeout(webACLRetryTimeout)}, retryOptions...)
	err = awsretry.OnError(ctx, isWebACLUnavailable, func(ctx context.Context) error {
		var err error
		created, err = client.CreateWebACL(ctx, in)
		return err
	}, options...)
	if err != nil {
		return nil, fmt.Errorf("create web ACL %s: %w", name, err)
	}
	if created == nil || created.Summary == nil || aws.ToString(created.Summary.Id) == "" {
		return nil, fmt.Errorf("create web ACL %s: response has no web ACL ID", name)
	}
	identity := webACLIdentity{
		ID:    aws.ToString(created.Summary.Id),
		Name:  name,
		Scope: in.Scope,
	}
	out, err := readWebACL(ctx, client, identity, false)
	if err != nil {
		return nil, fmt.Errorf("read created web ACL %s: %w", identity.ID, err)
	}
	return out, nil
}

func (r *WebACLResource) resolveName(random io.Reader) (string, error) {
	if r.Name != nil {
		return *r.Name, nil
	}
	bytes := make([]byte, 12)
	if _, err := io.ReadFull(random, bytes); err != nil {
		return "", fmt.Errorf("generate web ACL name: %w", err)
	}
	return "unobin-" + hex.EncodeToString(bytes), nil
}

func (r *WebACLResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[WebACLResource, *WebACLResourceOutput, *awsCfg],
) (*WebACLResourceOutput, error) {
	prior := recordedPrior.Outputs
	identity, err := priorWebACLIdentity(prior)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return readWebACL(ctx, client, identity, true)
}

func priorWebACLIdentity(prior *WebACLResourceOutput) (webACLIdentity, error) {
	if prior == nil || prior.ARN == "" {
		return webACLIdentity{}, fmt.Errorf("prior web ACL output has no ARN")
	}
	return parseWebACLARN(prior.ARN)
}

func readWebACL(
	ctx context.Context,
	client wafClient,
	identity webACLIdentity,
	mapNotFound bool,
) (*WebACLResourceOutput, error) {
	response, err := client.GetWebACL(ctx, &awssvc.GetWebACLInput{
		Id:    aws.String(identity.ID),
		Name:  aws.String(identity.Name),
		Scope: identity.Scope,
	})
	if err != nil {
		if mapNotFound && isWebACLNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get web ACL %s: %w", identity.ID, err)
	}
	out, err := webACLOutput(identity, response)
	if err != nil {
		return nil, err
	}
	if out == nil {
		if mapNotFound {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get web ACL %s: response has no web ACL", identity.ID)
	}
	return out, nil
}

func (r *WebACLResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[WebACLResource, *WebACLResourceOutput, *awsCfg],
) (*WebACLResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior)
}

func (r *WebACLResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	recordedPrior runtime.Prior[WebACLResource, *WebACLResourceOutput, *awsCfg],
) error {
	prior := recordedPrior.Outputs
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior)
}
