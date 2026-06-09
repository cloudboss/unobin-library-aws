package acm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	acm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// validationTimeout bounds the wait for an Amazon-issued certificate to reach
// ISSUED. ACM retries domain validation for up to 72 hours, but a certificate
// whose DNS records are already in place issues within minutes; the wait runs
// for 75 minutes, the same bound the Terraform provider uses, long enough to
// cover the propagation of freshly created validation records.
const validationTimeout = 75 * time.Minute

// CertificateValidation is a barrier that completes only once an Amazon-issued
// certificate has reached the ISSUED status. It performs no AWS create call:
// the certificate is owned by the acm-certificate resource, and this resource
// exists so a downstream resource (a load balancer listener, an API domain)
// can depend on the certificate being validated before it is referenced. Create
// describes the certificate, refuses one that is not Amazon-issued (an imported
// certificate needs no validation), optionally cross-checks that the supplied
// DNS record FQDNs cover every domain-validation record, then waits until the
// certificate issues. Read reports the barrier satisfied only while the
// certificate exists and is still ISSUED. Update and Delete do nothing, since
// there is no separate object to reconcile or remove.
type CertificateValidation struct {
	// CertificateArn identifies the certificate to wait on. It is the
	// DescribeCertificate key and the resource's identity handle. ACM fixes a
	// certificate's identity, so a change replaces this resource.
	CertificateArn string `ub:"certificate-arn"`
	// ValidationRecordFqdns are the fully qualified names of the DNS validation
	// records the user created elsewhere (in a route53-record-set, say). When
	// set, Create cross-checks them against the records ACM expects before
	// waiting, so a missing record fails fast instead of timing out 75 minutes
	// later. The list is never sent to AWS; it is used only for the check. A
	// change to it replaces this resource.
	ValidationRecordFqdns []string `ub:"validation-record-fqdns"`
}

// CertificateValidationOutput holds the certificate ARN, the barrier's identity
// handle. It is populated only once the certificate has been observed ISSUED,
// so a resource that reads it can rely on the certificate being validated.
type CertificateValidationOutput struct {
	CertificateArn string `ub:"certificate-arn"`
}

func (r *CertificateValidation) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs that force a new resource. Both the certificate
// ARN (the certificate's fixed identity) and the validation record FQDNs (which
// only make sense for the certificate they were checked against) are immutable,
// so a change to either replaces this barrier.
func (r *CertificateValidation) ReplaceFields() []string {
	return []string{"certificate-arn", "validation-record-fqdns"}
}

// Defaults marks the optional validation-record-fqdns list, which the user may
// omit when the DNS cross-check is not wanted.
func (r CertificateValidation) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.ValidationRecordFqdns),
	}
}

func (r *CertificateValidation) Create(
	ctx context.Context, cfg any,
) (*CertificateValidationOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	detail, err := r.describe(ctx, client)
	if err != nil {
		return nil, err
	}
	// An imported certificate is issued the moment it is imported and has no
	// validation to wait on, so this barrier applies only to an Amazon-issued
	// certificate; anything else is a misuse.
	if detail.Type != acmtypes.CertificateTypeAmazonIssued {
		return nil, fmt.Errorf(
			"certificate %s has type %s, no validation necessary",
			r.CertificateArn, detail.Type)
	}
	if len(r.ValidationRecordFqdns) > 0 {
		if err := checkValidationRecordFqdns(detail, r.ValidationRecordFqdns); err != nil {
			return nil, err
		}
	}
	if err := r.waitIssued(ctx, client); err != nil {
		return nil, err
	}
	return &CertificateValidationOutput{CertificateArn: r.CertificateArn}, nil
}

// Read reports the barrier satisfied only while the certificate exists and is
// still ISSUED. A gone certificate, or one that has fallen out of ISSUED (it
// failed, was revoked, or its validation timed out), reads as not-found so a
// plan re-runs the wait.
func (r *CertificateValidation) Read(
	ctx context.Context, cfg any, prior *CertificateValidationOutput,
) (*CertificateValidationOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.DescribeCertificate(ctx, &acm.DescribeCertificateInput{
		CertificateArn: aws.String(prior.CertificateArn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe certificate: %w", err)
	}
	if resp.Certificate == nil ||
		resp.Certificate.Status != acmtypes.CertificateStatusIssued {
		return nil, runtime.ErrNotFound
	}
	return &CertificateValidationOutput{CertificateArn: prior.CertificateArn}, nil
}

// Update is a no-op. Both inputs are replace-only, so an Update reaches this
// method only when nothing it manages has changed; it returns the prior
// outputs unchanged.
func (r *CertificateValidation) Update(
	ctx context.Context, cfg any,
	prior runtime.Prior[CertificateValidation, *CertificateValidationOutput],
) (*CertificateValidationOutput, error) {
	return prior.Outputs, nil
}

// Delete is a no-op. The certificate belongs to the acm-certificate resource;
// this barrier has nothing of its own to remove.
func (r *CertificateValidation) Delete(
	ctx context.Context, cfg any, prior *CertificateValidationOutput,
) error {
	return nil
}

// describe reads the certificate detail by ARN, mapping ACM's not-found
// exception to runtime.ErrNotFound.
func (r *CertificateValidation) describe(
	ctx context.Context, client *acm.Client,
) (*acmtypes.CertificateDetail, error) {
	resp, err := client.DescribeCertificate(ctx, &acm.DescribeCertificateInput{
		CertificateArn: aws.String(r.CertificateArn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe certificate: %w", err)
	}
	if resp.Certificate == nil {
		return nil, runtime.ErrNotFound
	}
	return resp.Certificate, nil
}

// waitIssued polls the certificate until its status is ISSUED. The terminal
// failure states stop the wait at once with an error rather than letting it run
// the full 75 minutes: FAILED gives the failure reason, REVOKED the
// revocation reason, and a timed-out validation reports as such. A certificate
// that has briefly gone not-found while still settling keeps the wait going.
func (r *CertificateValidation) waitIssued(ctx context.Context, client *acm.Client) error {
	what := fmt.Sprintf("certificate %s to be issued", r.CertificateArn)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		detail, err := r.describe(ctx, client)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		switch detail.Status {
		case acmtypes.CertificateStatusIssued:
			return true, nil
		case acmtypes.CertificateStatusFailed:
			return false, fmt.Errorf(
				"certificate %s validation failed: %s",
				r.CertificateArn, detail.FailureReason)
		case acmtypes.CertificateStatusRevoked:
			return false, fmt.Errorf(
				"certificate %s was revoked: %s",
				r.CertificateArn, detail.RevocationReason)
		case acmtypes.CertificateStatusValidationTimedOut:
			return false, fmt.Errorf(
				"certificate %s validation timed out", r.CertificateArn)
		}
		return false, nil
	}, wait.WithTimeout(validationTimeout))
}

// checkValidationRecordFqdns verifies that the supplied FQDNs account for every
// DNS validation record the certificate expects. It first refuses a certificate
// any of whose domains validate by a method other than DNS, since record FQDNs
// are meaningless there. It then takes the set of record names ACM expects,
// removes every supplied FQDN, and fails on any record left uncovered. Names on
// both sides are compared with their trailing dot removed, because a DNS record
// name and the name ACM reports can differ only in that dot.
func checkValidationRecordFqdns(detail *acmtypes.CertificateDetail, fqdns []string) error {
	for _, opt := range detail.DomainValidationOptions {
		if opt.ValidationMethod != acmtypes.ValidationMethodDns {
			return fmt.Errorf(
				"validation_record_fqdns is not valid for %s validation",
				opt.ValidationMethod)
		}
	}
	supplied := make(map[string]struct{}, len(fqdns))
	for _, fqdn := range fqdns {
		supplied[strings.TrimSuffix(fqdn, ".")] = struct{}{}
	}
	for _, opt := range detail.DomainValidationOptions {
		if opt.ResourceRecord == nil {
			continue
		}
		name := strings.TrimSuffix(aws.ToString(opt.ResourceRecord.Name), ".")
		if _, ok := supplied[name]; !ok {
			return fmt.Errorf(
				"missing %s DNS validation record: %s",
				aws.ToString(opt.DomainName), name)
		}
	}
	return nil
}
