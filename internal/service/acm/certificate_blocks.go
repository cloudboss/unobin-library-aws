package acm

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
)

// CertificateOptions holds the per-certificate options ACM accepts at request
// time. The transparency-logging preference controls whether the certificate
// is recorded in a public certificate transparency log; ACM defaults it to
// ENABLED when an options block is supplied. The export preference allows the
// certificate to be exported and is fixed at creation: ACM does not let it
// change on an existing certificate, so it rides RequestCertificate only and
// is not resent on update.
type CertificateOptions struct {
	CertificateTransparencyLoggingPreference *string `ub:"certificate-transparency-logging-preference"`
	Export                                   *string `ub:"export"`
}

// to builds the SDK options struct for RequestCertificate, with both the
// transparency-logging preference and the export preference. A nil receiver
// yields nil so an omitted options block leaves ACM's own defaults in place.
func (o *CertificateOptions) to() *acmtypes.CertificateOptions {
	if o == nil {
		return nil
	}
	out := &acmtypes.CertificateOptions{}
	if o.CertificateTransparencyLoggingPreference != nil {
		out.CertificateTransparencyLoggingPreference =
			acmtypes.CertificateTransparencyLoggingPreference(
				*o.CertificateTransparencyLoggingPreference)
	}
	if o.Export != nil {
		out.Export = acmtypes.CertificateExport(*o.Export)
	}
	return out
}

// toUpdate builds the SDK options struct for UpdateCertificateOptions. Only the
// transparency-logging preference is sent: the export preference is create-only,
// and resending it would be rejected, so update never touches it. A nil
// receiver yields nil.
func (o *CertificateOptions) toUpdate() *acmtypes.CertificateOptions {
	if o == nil {
		return nil
	}
	out := &acmtypes.CertificateOptions{}
	if o.CertificateTransparencyLoggingPreference != nil {
		out.CertificateTransparencyLoggingPreference =
			acmtypes.CertificateTransparencyLoggingPreference(
				*o.CertificateTransparencyLoggingPreference)
	}
	return out
}

// CertificateValidationOption picks the email domain ACM uses for email-based
// validation of one domain in the request. Both fields are required by the SDK:
// domain-name names a domain in the certificate, and validation-domain is the
// superdomain whose well-known mailboxes receive the validation email.
type CertificateValidationOption struct {
	DomainName       string `ub:"domain-name"`
	ValidationDomain string `ub:"validation-domain"`
}

// validationOptions converts the desired validation options into the SDK list.
func validationOptions(in []CertificateValidationOption) []acmtypes.DomainValidationOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]acmtypes.DomainValidationOption, 0, len(in))
	for _, opt := range in {
		out = append(out, acmtypes.DomainValidationOption{
			DomainName:       aws.String(opt.DomainName),
			ValidationDomain: aws.String(opt.ValidationDomain),
		})
	}
	return out
}

// CertificateDomainValidationOption is one entry of the settled
// domain-validation-options output: the DNS record ACM expects a downstream
// resource to create so it can prove control of the domain. Only entries whose
// CNAME record ACM has assigned appear here, so resource-record-name,
// resource-record-type, and resource-record-value are always populated.
type CertificateDomainValidationOption struct {
	DomainName          string `ub:"domain-name"`
	ResourceRecordName  string `ub:"resource-record-name"`
	ResourceRecordType  string `ub:"resource-record-type"`
	ResourceRecordValue string `ub:"resource-record-value"`
}

// domainValidationOptions flattens the validation records ACM returns into the
// output list, keeping only entries that hold an assigned CNAME record. An
// entry without a ResourceRecord is one ACM has not yet filled in, or one
// validated by email, neither of which a downstream DNS resource can act on.
func domainValidationOptions(
	in []acmtypes.DomainValidation,
) []CertificateDomainValidationOption {
	var out []CertificateDomainValidationOption
	for _, v := range in {
		if v.ResourceRecord == nil {
			continue
		}
		out = append(out, CertificateDomainValidationOption{
			DomainName:          aws.ToString(v.DomainName),
			ResourceRecordName:  aws.ToString(v.ResourceRecord.Name),
			ResourceRecordType:  string(v.ResourceRecord.Type),
			ResourceRecordValue: aws.ToString(v.ResourceRecord.Value),
		})
	}
	return out
}
