package acm

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	acm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/tagsync"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// deleteTimeout bounds the retry on a certificate still referenced by another
// service. Removing a certificate from a load balancer or CloudFront listener
// can take well over fifteen minutes to propagate, during which ACM rejects the
// delete as in-use, so the retry runs for twenty minutes.
const deleteTimeout = 20 * time.Minute

// Certificate manages an ACM certificate created by one of two paths chosen by
// which fields are set. With domain-name set, RequestCertificate issues an
// Amazon-managed certificate (validated by DNS or email) or, with
// certificate-authority-arn, a certificate signed by a private CA. With
// private-key set instead, ImportCertificate brings in an externally issued
// certificate from its PEM body, private key, and optional chain. The domain
// identity, key algorithm, alternative names, validation method, and validation
// options are fixed at creation, so a change to any of them replaces the
// certificate; the imported material and the transparency-logging option are
// reconciled in place. After create, and after a re-import, the resource waits
// until ACM assigns the DNS validation records and returns them as
// domain-validation-options, the value a downstream validation resource reads.
type Certificate struct {
	// DomainName is the fully qualified domain the certificate secures. Setting
	// it selects request mode. ACM rejects a name ending in a dot.
	DomainName *string `ub:"domain-name"`
	// CertificateAuthorityArn issues the certificate from a private CA instead
	// of from Amazon's public CA. Must be a valid ACM PCA ARN.
	CertificateAuthorityArn *string `ub:"certificate-authority-arn"`
	KeyAlgorithm            *string `ub:"key-algorithm"`
	// SubjectAlternativeNames are additional domains the certificate covers.
	// Each is 1 to 253 characters and may not end in a dot. ACM also adds
	// domain-name to this set server-side, so the read-back output includes it.
	SubjectAlternativeNames []string `ub:"subject-alternative-names"`
	ValidationMethod        *string  `ub:"validation-method"`
	// ValidationOption sets the email domain for email validation of each named
	// domain. It is fixed at creation.
	ValidationOption []CertificateValidationOption `ub:"validation-option"`
	// Options holds the transparency-logging and export preferences. It is
	// set at creation; the transparency preference is reconciled in place by
	// UpdateCertificateOptions, while the export preference is create-only.
	Options *CertificateOptions `ub:"options"`
	// CertificateBody is the PEM-encoded certificate to import. Setting it
	// selects import mode and requires private-key.
	CertificateBody *string `ub:"certificate-body"`
	// PrivateKey is the PEM-encoded private key matching the imported
	// certificate. Setting it selects import mode.
	PrivateKey *string `ub:"private-key,sensitive"`
	// CertificateChain is the PEM-encoded chain of intermediate certificates
	// for an imported certificate.
	CertificateChain *string           `ub:"certificate-chain"`
	Tags             map[string]string `ub:"tags"`
}

// CertificateOutput holds the values ACM computes for a certificate. The ARN is
// the certificate's identity and the handle every later call uses. domain-name
// is read back because ACM derives it from the body for an imported certificate.
// domain-validation-options is the crux: the DNS records a downstream resource
// creates to validate domain control, populated only after the settling wait.
type CertificateOutput struct {
	Arn                     string                              `ub:"arn"`
	DomainName              string                              `ub:"domain-name"`
	Status                  string                              `ub:"status"`
	Type                    string                              `ub:"type"`
	NotAfter                string                              `ub:"not-after"`
	NotBefore               string                              `ub:"not-before"`
	RenewalEligibility      string                              `ub:"renewal-eligibility"`
	ValidationEmails        []string                            `ub:"validation-emails"`
	DomainValidationOptions []CertificateDomainValidationOption `ub:"domain-validation-options"`
}

func (r *Certificate) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs ACM fixes when a certificate is requested. The
// domain identity, key algorithm, alternative names, validation method, and the
// per-domain validation options cannot change on an existing certificate, so a
// change to any of them requires a new one. The imported material
// (certificate-body, private-key, certificate-chain) is deliberately absent:
// ACM re-imports it in place against the same ARN. The options block is also
// absent: its transparency preference updates in place.
func (r *Certificate) ReplaceFields() []string {
	return []string{
		"certificate-authority-arn",
		"domain-name",
		"key-algorithm",
		"subject-alternative-names",
		"validation-method",
		"validation-option",
	}
}

// Defaults marks the collection inputs a certificate may omit. The options
// block is a pointer field, optional on its own, so it takes no marker.
func (r Certificate) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.SubjectAlternativeNames),
		defaults.Optional(r.ValidationOption),
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the rules ACM places on a certificate's inputs. Exactly
// one of domain-name (request mode) or private-key (import mode) selects the
// creation path. The two field groups are mutually exclusive: the import
// material cannot mix with the request-only fields, and the reverse. An
// imported certificate body requires its private key. In request mode ACM needs
// a way to validate, so a domain-name request requires either a private-CA arn
// or a validation method. The validation method, key algorithm, and the two
// options preferences each accept a fixed set of values, applied only when set.
func (r Certificate) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.DomainName, r.PrivateKey),
		constraint.ForbiddenWith(r.PrivateKey,
			r.DomainName, r.CertificateAuthorityArn, r.KeyAlgorithm,
			r.SubjectAlternativeNames, r.ValidationMethod, r.ValidationOption, r.Options),
		constraint.ForbiddenWith(r.DomainName,
			r.CertificateBody, r.PrivateKey, r.CertificateChain),
		constraint.RequiredWith(r.CertificateBody, r.PrivateKey),
		constraint.ForbiddenWith(r.CertificateAuthorityArn, r.ValidationMethod),
		constraint.When(constraint.Present(r.DomainName)).
			Require(constraint.Any(
				constraint.Present(r.CertificateAuthorityArn),
				constraint.Present(r.ValidationMethod))).
			Message("a domain-name request requires certificate-authority-arn " +
				"or validation-method"),
		constraint.When(constraint.Present(r.ValidationMethod)).
			Require(constraint.OneOf(r.ValidationMethod, "DNS", "EMAIL")).
			Message("validation-method must be DNS or EMAIL"),
		constraint.When(constraint.Present(r.KeyAlgorithm)).
			Require(constraint.OneOf(r.KeyAlgorithm,
				"RSA_1024", "RSA_2048", "RSA_3072", "RSA_4096",
				"EC_prime256v1", "EC_secp384r1", "EC_secp521r1")).
			Message("key-algorithm must be a valid ACM key algorithm"),
		constraint.When(constraint.Present(r.Options.CertificateTransparencyLoggingPreference)).
			Require(constraint.OneOf(r.Options.CertificateTransparencyLoggingPreference,
				"ENABLED", "DISABLED")).
			Message("options certificate-transparency-logging-preference must be " +
				"ENABLED or DISABLED"),
		constraint.When(constraint.Present(r.Options.Export)).
			Require(constraint.OneOf(r.Options.Export, "ENABLED", "DISABLED")).
			Message("options export must be ENABLED or DISABLED"),
	}
}

func (r *Certificate) Create(ctx context.Context, cfg *awsCfg) (*CertificateOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var arn string
	if r.DomainName != nil {
		arn, err = r.request(ctx, client)
	} else {
		arn, err = r.importCertificate(ctx, client, nil)
	}
	if err != nil {
		return nil, err
	}
	// The create response returns only the ARN, while the DNS validation
	// records settle moments later. Wait for them, then read so the returned
	// domain-validation-options reflects what a downstream resource must create.
	if err := r.waitValidationsAvailable(ctx, client, arn); err != nil {
		return nil, err
	}
	return r.read(ctx, client, arn)
}

func (r *Certificate) Read(
	ctx context.Context, cfg *awsCfg, prior *CertificateOutput,
) (*CertificateOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Arn)
}

func (r *Certificate) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Certificate, *CertificateOutput],
) (*CertificateOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	// A change to any of the imported material re-imports the certificate in
	// place against the existing ARN, keeping the same resource. The settling
	// wait runs again because the new material re-enters validation.
	if r.importMaterialChanged(prior) {
		if _, err := r.importCertificate(ctx, client, aws.String(arn)); err != nil {
			return nil, err
		}
		if err := r.waitValidationsAvailable(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	// The transparency-logging preference is reconciled by its own call, only
	// when it changed; the export preference is create-only and never resent.
	if r.transparencyChanged(prior) {
		_, err := client.UpdateCertificateOptions(ctx, &acm.UpdateCertificateOptionsInput{
			CertificateArn: aws.String(arn),
			Options:        r.Options.toUpdate(),
		})
		if err != nil {
			return nil, fmt.Errorf("update certificate options: %w", err)
		}
	}
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := r.syncTags(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, arn)
}

func (r *Certificate) Delete(ctx context.Context, cfg *awsCfg, prior *CertificateOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// A certificate still referenced by another service rejects the delete as
	// in-use; the reference can take longer than fifteen minutes to clear, so
	// the delete retries through that window. A certificate already gone counts
	// as deleted.
	err = retry.OnError(ctx, isInUse, func(ctx context.Context) error {
		_, err := client.DeleteCertificate(ctx, &acm.DeleteCertificateInput{
			CertificateArn: aws.String(prior.Arn),
		})
		return err
	}, retry.WithTimeout(deleteTimeout))
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete certificate: %w", err)
	}
	return nil
}

// request issues a managed or private-CA certificate and returns its ARN.
func (r *Certificate) request(ctx context.Context, client *acm.Client) (string, error) {
	in := &acm.RequestCertificateInput{
		DomainName:              r.DomainName,
		CertificateAuthorityArn: r.CertificateAuthorityArn,
		SubjectAlternativeNames: r.SubjectAlternativeNames,
		DomainValidationOptions: validationOptions(r.ValidationOption),
		Options:                 r.Options.to(),
		Tags:                    certificateTags(r.Tags),
	}
	if r.KeyAlgorithm != nil {
		in.KeyAlgorithm = acmtypes.KeyAlgorithm(*r.KeyAlgorithm)
	}
	if r.ValidationMethod != nil {
		in.ValidationMethod = acmtypes.ValidationMethod(*r.ValidationMethod)
	}
	resp, err := client.RequestCertificate(ctx, in)
	if err != nil {
		return "", fmt.Errorf("request certificate: %w", err)
	}
	return aws.ToString(resp.CertificateArn), nil
}

// importCertificate imports the PEM material and returns the certificate's ARN.
// With arn nil it imports a new certificate; with arn set it re-imports in
// place against an existing certificate. ACM forbids tags on a re-import, so
// they ride only the initial import.
func (r *Certificate) importCertificate(
	ctx context.Context, client *acm.Client, arn *string,
) (string, error) {
	in := &acm.ImportCertificateInput{
		Certificate:    []byte(aws.ToString(r.CertificateBody)),
		PrivateKey:     []byte(aws.ToString(r.PrivateKey)),
		CertificateArn: arn,
	}
	if r.CertificateChain != nil {
		in.CertificateChain = []byte(*r.CertificateChain)
	}
	if arn == nil {
		in.Tags = certificateTags(r.Tags)
	}
	resp, err := client.ImportCertificate(ctx, in)
	if err != nil {
		return "", fmt.Errorf("import certificate: %w", err)
	}
	return aws.ToString(resp.CertificateArn), nil
}

// read fetches the certificate by ARN and returns its computed outputs. A
// not-found maps to runtime.ErrNotFound so a plan recreates it. A certificate
// whose validation timed out is dead and will be recreated, so it reads as
// not-found too.
func (r *Certificate) read(
	ctx context.Context, client *acm.Client, arn string,
) (*CertificateOutput, error) {
	detail, err := describeCertificate(ctx, client, arn)
	if err != nil {
		return nil, err
	}
	if detail.Status == acmtypes.CertificateStatusValidationTimedOut {
		return nil, runtime.ErrNotFound
	}
	out := &CertificateOutput{
		Arn:                     aws.ToString(detail.CertificateArn),
		DomainName:              aws.ToString(detail.DomainName),
		Status:                  string(detail.Status),
		Type:                    string(detail.Type),
		RenewalEligibility:      string(detail.RenewalEligibility),
		DomainValidationOptions: domainValidationOptions(detail.DomainValidationOptions),
	}
	if detail.NotAfter != nil {
		out.NotAfter = detail.NotAfter.UTC().Format(time.RFC3339)
	}
	if detail.NotBefore != nil {
		out.NotBefore = detail.NotBefore.UTC().Format(time.RFC3339)
	}
	for _, v := range detail.DomainValidationOptions {
		out.ValidationEmails = append(out.ValidationEmails, v.ValidationEmails...)
	}
	return out, nil
}

// waitValidationsAvailable blocks until ACM has settled the certificate's
// validation. An imported certificate needs no validation and satisfies at
// once. For a requested certificate the wait holds until at least one domain
// has its DNS record assigned, a validation email recorded, or its validation
// succeeded, or the whole certificate has issued; a certificate still settling
// can briefly report not-found, which keeps the wait going rather than aborting.
func (r *Certificate) waitValidationsAvailable(
	ctx context.Context, client *acm.Client, arn string,
) error {
	what := fmt.Sprintf("certificate %s validation records", arn)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		detail, err := describeCertificate(ctx, client, arn)
		if err != nil {
			if err == runtime.ErrNotFound {
				return false, nil
			}
			return false, err
		}
		if detail.Type == acmtypes.CertificateTypeImported {
			return true, nil
		}
		if detail.Status == acmtypes.CertificateStatusIssued {
			return true, nil
		}
		for _, v := range detail.DomainValidationOptions {
			if v.ResourceRecord != nil {
				return true, nil
			}
			if len(v.ValidationEmails) > 0 {
				return true, nil
			}
			if v.ValidationStatus == acmtypes.DomainStatusSuccess {
				return true, nil
			}
		}
		return false, nil
	}, wait.WithTimeout(5*time.Minute))
}

// importMaterialChanged reports whether any of the imported PEM fields changed,
// which an imported certificate reconciles by re-importing in place.
func (r *Certificate) importMaterialChanged(
	prior runtime.Prior[Certificate, *CertificateOutput],
) bool {
	return runtime.Changed(prior.Inputs.CertificateBody, r.CertificateBody) ||
		runtime.Changed(prior.Inputs.PrivateKey, r.PrivateKey) ||
		runtime.Changed(prior.Inputs.CertificateChain, r.CertificateChain)
}

// transparencyChanged reports whether the transparency-logging preference
// changed. The export preference is create-only and is not considered here.
func (r *Certificate) transparencyChanged(
	prior runtime.Prior[Certificate, *CertificateOutput],
) bool {
	var prev, cur *string
	if prior.Inputs.Options != nil {
		prev = prior.Inputs.Options.CertificateTransparencyLoggingPreference
	}
	if r.Options != nil {
		cur = r.Options.CertificateTransparencyLoggingPreference
	}
	return runtime.Changed(prev, cur)
}

// syncTags reconciles the certificate's tags with the desired set, reading the
// live tags with ListTagsForCertificate and writing changes with
// AddTagsToCertificate and RemoveTagsFromCertificate.
func (r *Certificate) syncTags(ctx context.Context, client *acm.Client, arn string) error {
	return tagsync.Sync(ctx, r.Tags,
		func(ctx context.Context) (map[string]string, error) {
			resp, err := client.ListTagsForCertificate(ctx,
				&acm.ListTagsForCertificateInput{CertificateArn: aws.String(arn)})
			if err != nil {
				return nil, fmt.Errorf("list tags for certificate: %w", err)
			}
			current := map[string]string{}
			for _, t := range resp.Tags {
				current[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			return current, nil
		},
		func(ctx context.Context, upsert map[string]string) error {
			if _, err := client.AddTagsToCertificate(ctx, &acm.AddTagsToCertificateInput{
				CertificateArn: aws.String(arn),
				Tags:           certificateTags(upsert),
			}); err != nil {
				return fmt.Errorf("add tags to certificate: %w", err)
			}
			return nil
		},
		func(ctx context.Context, remove []string) error {
			if _, err := client.RemoveTagsFromCertificate(ctx,
				&acm.RemoveTagsFromCertificateInput{
					CertificateArn: aws.String(arn),
					Tags:           certificateTagKeys(remove),
				}); err != nil {
				return fmt.Errorf("remove tags from certificate: %w", err)
			}
			return nil
		},
	)
}

// describeCertificate reads the certificate detail by ARN, mapping ACM's
// not-found exception and an empty result to runtime.ErrNotFound so callers can
// treat a gone certificate as drift.
func describeCertificate(
	ctx context.Context, client *acm.Client, arn string,
) (*acmtypes.CertificateDetail, error) {
	resp, err := client.DescribeCertificate(ctx, &acm.DescribeCertificateInput{
		CertificateArn: aws.String(arn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe certificate: %w", err)
	}
	return certificateDetailFromDescribe(resp)
}

func certificateDetailFromDescribe(
	resp *acm.DescribeCertificateOutput,
) (*acmtypes.CertificateDetail, error) {
	if resp == nil || resp.Certificate == nil {
		return nil, runtime.ErrNotFound
	}
	return resp.Certificate, nil
}

// certificateTags converts a desired tag map into the ACM SDK tag list, ordered
// by key so the request is deterministic.
func certificateTags(tags map[string]string) []acmtypes.Tag {
	if len(tags) == 0 {
		return nil
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]acmtypes.Tag, 0, len(tags))
	for _, k := range keys {
		out = append(out, acmtypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
	}
	return out
}

// certificateTagKeys converts a list of tag keys into the ACM SDK tag list that
// the remove call expects, which keys on the tag's Key alone.
func certificateTagKeys(keys []string) []acmtypes.Tag {
	out := make([]acmtypes.Tag, 0, len(keys))
	for _, k := range keys {
		out = append(out, acmtypes.Tag{Key: aws.String(k)})
	}
	return out
}
