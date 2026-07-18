package wafv2

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const webACLAssociationRetryTimeout = 10 * time.Minute

type webACLAssociationClient interface {
	AssociateWebACL(
		context.Context,
		*awssvc.AssociateWebACLInput,
		...func(*awssvc.Options),
	) (*awssvc.AssociateWebACLOutput, error)
	DisassociateWebACL(
		context.Context,
		*awssvc.DisassociateWebACLInput,
		...func(*awssvc.Options),
	) (*awssvc.DisassociateWebACLOutput, error)
	GetWebACLForResource(
		context.Context,
		*awssvc.GetWebACLForResourceInput,
		...func(*awssvc.Options),
	) (*awssvc.GetWebACLForResourceOutput, error)
}

type webACLAssociationClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type systemWebACLAssociationClock struct{}

var (
	webACLAssociationARNPartitionPattern = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	webACLAssociationARNRegionPattern    = regexp.MustCompile(
		`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`,
	)
	webACLAssociationARNAccountPattern = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|` +
			`partner-managed|\d{12}|cw.{10})$`,
	)
)

type WebACLAssociationResource struct {
	ResourceARN string `ub:"resource-arn"`
	WebACLARN   string `ub:"web-acl-arn"`
}

type WebACLAssociationResourceOutput struct {
	ResourceARN string `ub:"resource-arn"`
	WebACLARN   string `ub:"web-acl-arn"`
}

func (*WebACLAssociationResource) SchemaVersion() int { return 1 }

func (*WebACLAssociationResource) ReplaceFields() []string {
	return []string{"resource-arn", "web-acl-arn"}
}

func (r *WebACLAssociationResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*WebACLAssociationResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, systemWebACLAssociationClock{})
}

func (r *WebACLAssociationResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *WebACLAssociationResourceOutput,
) (*WebACLAssociationResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior)
}

func (*WebACLAssociationResource) Update(
	_ context.Context,
	_ *awsCfg,
	prior runtime.Prior[WebACLAssociationResource, *WebACLAssociationResourceOutput],
) (*WebACLAssociationResourceOutput, error) {
	return prior.Outputs, nil
}

func (r *WebACLAssociationResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *WebACLAssociationResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, prior)
}

func (r *WebACLAssociationResource) ValidateInputs(context.Context, *awsCfg) error {
	if !validWebACLAssociationARN(r.ResourceARN) {
		return fmt.Errorf("resource-arn must be a valid ARN")
	}
	if !validWebACLAssociationARN(r.WebACLARN) {
		return fmt.Errorf("web-acl-arn must be a valid ARN")
	}
	return nil
}

func (r *WebACLAssociationResource) create(
	ctx context.Context,
	client webACLAssociationClient,
	clock webACLAssociationClock,
) (*WebACLAssociationResourceOutput, error) {
	if err := r.ValidateInputs(ctx, nil); err != nil {
		return nil, err
	}
	input := &awssvc.AssociateWebACLInput{
		ResourceArn: aws.String(r.ResourceARN),
		WebACLArn:   aws.String(r.WebACLARN),
	}
	err := retryWebACLAssociation(ctx, clock, func(ctx context.Context) error {
		_, err := client.AssociateWebACL(ctx, input)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("associate web ACL: %w", err)
	}
	out, err := r.read(ctx, client, nil)
	if err != nil {
		return nil, fmt.Errorf("read web ACL association after create: %w", err)
	}
	return out, nil
}

func (r *WebACLAssociationResource) read(
	ctx context.Context,
	client webACLAssociationClient,
	prior *WebACLAssociationResourceOutput,
) (*WebACLAssociationResourceOutput, error) {
	identity := r.identity(prior)
	response, err := client.GetWebACLForResource(ctx, &awssvc.GetWebACLForResourceInput{
		ResourceArn: aws.String(identity.ResourceARN),
	})
	if err != nil {
		if isWebACLNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get web ACL for resource %s: %w", identity.ResourceARN, err)
	}
	if response == nil || response.WebACL == nil {
		return nil, runtime.ErrNotFound
	}
	observedARN := aws.ToString(response.WebACL.ARN)
	if observedARN == "" {
		return nil, fmt.Errorf(
			"get web ACL for resource %s: response has no web ACL ARN",
			identity.ResourceARN,
		)
	}
	if observedARN != identity.WebACLARN {
		return nil, runtime.ErrNotFound
	}
	return &identity, nil
}

func (r *WebACLAssociationResource) delete(
	ctx context.Context,
	client webACLAssociationClient,
	prior *WebACLAssociationResourceOutput,
) error {
	identity := r.identity(prior)
	_, err := client.DisassociateWebACL(ctx, &awssvc.DisassociateWebACLInput{
		ResourceArn: aws.String(identity.ResourceARN),
	})
	if err != nil {
		if isWebACLNotFound(err) {
			return nil
		}
		return fmt.Errorf("disassociate web ACL: %w", err)
	}
	return nil
}

func (r *WebACLAssociationResource) identity(
	prior *WebACLAssociationResourceOutput,
) WebACLAssociationResourceOutput {
	if prior != nil && prior.ResourceARN != "" && prior.WebACLARN != "" {
		return *prior
	}
	return WebACLAssociationResourceOutput{
		ResourceARN: r.ResourceARN,
		WebACLARN:   r.WebACLARN,
	}
}

func retryWebACLAssociation(
	ctx context.Context,
	clock webACLAssociationClock,
	call func(context.Context) error,
) error {
	deadline := clock.Now().Add(webACLAssociationRetryTimeout)
	delay := 500 * time.Millisecond
	for {
		err := call(ctx)
		if err == nil || !isWebACLUnavailable(err) {
			return err
		}
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			return err
		}
		if err := clock.Sleep(ctx, min(delay, remaining)); err != nil {
			return err
		}
		if !clock.Now().Before(deadline) {
			return err
		}
		delay = min(delay*2, 10*time.Second)
	}
}

func (systemWebACLAssociationClock) Now() time.Time {
	return time.Now()
}

func (systemWebACLAssociationClock) Sleep(
	ctx context.Context,
	duration time.Duration,
) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validWebACLAssociationARN(value string) bool {
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return false
	}
	if !webACLAssociationARNPartitionPattern.MatchString(parsed.Partition) {
		return false
	}
	if parsed.Region != "" && !webACLAssociationARNRegionPattern.MatchString(parsed.Region) {
		return false
	}
	if parsed.AccountID != "" &&
		!webACLAssociationARNAccountPattern.MatchString(parsed.AccountID) {
		return false
	}
	return parsed.Resource != ""
}
