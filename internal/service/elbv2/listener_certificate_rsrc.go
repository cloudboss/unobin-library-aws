package elbv2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

// ListenerCertificate associates one additional ACM certificate with an HTTPS
// or TLS listener, the certificates the listener offers over SNI beyond its
// default. Both the listener and the certificate name the association, so a
// change to either replaces it; there is nothing else to update and nothing the
// cloud computes to hold, so the output struct is empty. Presence is confirmed
// by paging the listener's certificates and finding the target among the
// non-default ones, since the describe also returns the listener's own default
// certificate, which this resource does not manage.
type ListenerCertificate struct {
	ListenerArn    string `ub:"listener-arn"`
	CertificateArn string `ub:"certificate-arn"`
}

// ListenerCertificateOutput is empty. The association's identity is the pair of
// input ARNs, both referenceable as inputs, and the describe returns no
// computed value this resource needs: the only added field, IsDefault, is
// always false for a certificate it manages.
type ListenerCertificateOutput struct{}

func (r *ListenerCertificate) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that fix the association's identity. The
// listener and the certificate together name the association, so a change to
// either is a different association and forces a new one. There is no field
// that updates in place.
func (r *ListenerCertificate) ReplaceFields() []string {
	return []string{"listener-arn", "certificate-arn"}
}

func (r *ListenerCertificate) Create(
	ctx context.Context, cfg any,
) (*ListenerCertificateOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// A certificate created moments earlier can be briefly invisible to ELBv2,
	// which rejects the add with CertificateNotFound until the certificate
	// control plane catches up; the add retries on that error for two minutes.
	err = retry.OnError(ctx, isCertificateNotFound, func(ctx context.Context) error {
		_, err := client.AddListenerCertificates(ctx, &elbv2.AddListenerCertificatesInput{
			ListenerArn:  aws.String(r.ListenerArn),
			Certificates: []elbv2types.Certificate{{CertificateArn: aws.String(r.CertificateArn)}},
		})
		return err
	}, retry.WithTimeout(2*time.Minute))
	if err != nil {
		return nil, fmt.Errorf("add listener certificate: %w", err)
	}
	// An added certificate may not appear in the listener's certificates at
	// once, so the confirming read retries on not-found for five minutes. A
	// plain drift Read does not retry; it reports not-found immediately.
	out, err := r.settleRead(ctx, client)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ListenerCertificate) Read(
	ctx context.Context, cfg any, prior *ListenerCertificateOutput,
) (*ListenerCertificateOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.find(ctx, client)
}

// Update has no work to do: both fields are replace-only, so a change to either
// recreates the association rather than reaching Update. The interface still
// requires the method, so it returns the prior outputs unchanged.
func (r *ListenerCertificate) Update(
	ctx context.Context, cfg any, prior runtime.Prior[ListenerCertificate, *ListenerCertificateOutput],
) (*ListenerCertificateOutput, error) {
	return prior.Outputs, nil
}

func (r *ListenerCertificate) Delete(
	ctx context.Context, cfg any, prior *ListenerCertificateOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.RemoveListenerCertificates(ctx, &elbv2.RemoveListenerCertificatesInput{
		ListenerArn:  aws.String(r.ListenerArn),
		Certificates: []elbv2types.Certificate{{CertificateArn: aws.String(r.CertificateArn)}},
	})
	// A certificate or listener already gone counts as removed. ELBv2 also
	// refuses to remove a listener's default certificate through this call; when
	// the target is the default, the association this resource manages no longer
	// exists, so that refusal counts as removed too.
	if err != nil && !isCertificateNotFound(err) && !isNotFound(err) &&
		!isDefaultCertificateRemoval(err) {
		return fmt.Errorf("remove listener certificate: %w", err)
	}
	return nil
}

// settleRead confirms a just-added certificate is visible, retrying find on
// not-found for five minutes while the listener's certificate list catches up.
func (r *ListenerCertificate) settleRead(
	ctx context.Context, client *elbv2.Client,
) (*ListenerCertificateOutput, error) {
	var out *ListenerCertificateOutput
	err := retry.OnError(ctx, func(err error) bool {
		return errors.Is(err, runtime.ErrNotFound)
	}, func(ctx context.Context) error {
		o, err := r.find(ctx, client)
		if err != nil {
			return err
		}
		out = o
		return nil
	}, retry.WithTimeout(5*time.Minute))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// find pages the listener's certificates and reports the association present
// when the target certificate appears among the non-default ones. The describe
// also returns the listener's own default certificate, which this resource does
// not manage, so the filter excludes IsDefault. A listener that is gone, or a
// target absent from its certificates, is not-found, which drives recreate.
func (r *ListenerCertificate) find(
	ctx context.Context, client *elbv2.Client,
) (*ListenerCertificateOutput, error) {
	in := &elbv2.DescribeListenerCertificatesInput{
		ListenerArn: aws.String(r.ListenerArn),
		PageSize:    aws.Int32(400),
	}
	for {
		resp, err := client.DescribeListenerCertificates(ctx, in)
		if err != nil {
			if isNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe listener certificates: %w", err)
		}
		for _, c := range resp.Certificates {
			if !aws.ToBool(c.IsDefault) && aws.ToString(c.CertificateArn) == r.CertificateArn {
				return &ListenerCertificateOutput{}, nil
			}
		}
		if aws.ToString(resp.NextMarker) == "" {
			return nil, runtime.ErrNotFound
		}
		in.Marker = resp.NextMarker
	}
}

// isDefaultCertificateRemoval reports whether err is ELBv2 refusing to remove a
// listener's default certificate through RemoveListenerCertificates. The
// refusal is an OperationNotPermitted whose message names the default
// certificate; it is specific to this resource's delete, so the predicate lives
// here rather than in the shared helpers.
func isDefaultCertificateRemoval(err error) bool {
	var notPermitted *elbv2types.OperationNotPermittedException
	return errors.As(err, &notPermitted) &&
		strings.Contains(notPermitted.ErrorMessage(), "Default certificate cannot be removed")
}
