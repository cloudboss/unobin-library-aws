package acm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	acm "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

const certificateDataLookupTimeout = time.Minute

var errCertificateDataNoSummaries = errors.New("no matching ACM certificate summaries")

// CertificateData resolves one existing ACM certificate. ListCertificates is
// queried with the requested key algorithms and certificate statuses, defaulting
// statuses to ISSUED, then the summaries are filtered client-side by exact
// domain and type. An empty summary set is retried for one minute to absorb ACM
// list consistency. The remaining summaries are described, validation-timed-out
// and vanished candidates are skipped, optional tag filters are applied as a
// requested-subset match, and the data source returns either the sole match or
// the most recent match according to ACM's status-specific timestamps.
type CertificateData struct {
	Domain     *string            `ub:"domain"`
	Tags       *map[string]string `ub:"tags"`
	KeyTypes   *[]string          `ub:"key-types"`
	Statuses   *[]string          `ub:"statuses"`
	Types      *[]string          `ub:"types"`
	MostRecent bool               `ub:"most-recent"`
}

// CertificateDataOutput holds the selected certificate attributes. Certificate
// and CertificateChain are set only for ISSUED certificates, since ACM only
// returns PEM material for an issued certificate; for any other status they are
// null. Tags are the actual user-visible tags on the selected certificate.
type CertificateDataOutput struct {
	Arn              string            `ub:"arn"`
	Domain           string            `ub:"domain"`
	Status           string            `ub:"status"`
	Certificate      *string           `ub:"certificate"`
	CertificateChain *string           `ub:"certificate-chain"`
	Tags             map[string]string `ub:"tags"`
}

// Defaults gives most-recent its false default and marks the collection inputs
// a certificate lookup may omit. An empty or omitted statuses list searches
// ISSUED certificates only.
func (r CertificateData) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(r.MostRecent, false),
	}
}

// Constraints declares the lookup's schema-visible rules. A lookup needs at
// least a domain or tag selector, and the enum-list entries must be valid ACM
// values.
func (r CertificateData) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.Any(
			constraint.Present(r.Domain),
			constraint.NotEmpty(r.Tags))).
			Message("a certificate lookup needs domain or tags"),
		constraint.ForEach(r.KeyTypes, func(t string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(t,
					"RSA_1024", "RSA_2048", "RSA_3072", "RSA_4096",
					"EC_prime256v1", "EC_secp384r1", "EC_secp521r1")).
					Message("key-types entries must be valid ACM key algorithms"),
			}
		}),
		constraint.ForEach(r.Statuses, func(s string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(s,
					"PENDING_VALIDATION", "ISSUED", "INACTIVE", "EXPIRED",
					"VALIDATION_TIMED_OUT", "REVOKED", "FAILED")).
					Message("statuses entries must be valid ACM certificate statuses"),
			}
		}),
		constraint.ForEach(r.Types, func(t string) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.OneOf(t, "IMPORTED", "AMAZON_ISSUED", "PRIVATE")).
					Message("types entries must be valid ACM certificate types"),
			}
		}),
	}
}

// Read resolves the certificate data source. A missing or ambiguous lookup is a
// descriptive data-source error, not runtime.ErrNotFound.
func (r *CertificateData) Read(ctx context.Context, cfg *awsCfg) (*CertificateDataOutput, error) {
	if err := r.checkLookupKeys(); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	summaries, err := r.findSummariesWithRetry(ctx, client)
	if err != nil {
		return nil, err
	}
	candidates, err := r.findCandidates(ctx, client, summaries)
	if err != nil {
		return nil, err
	}
	selected, err := r.selectCandidate(candidates)
	if err != nil {
		return nil, err
	}
	return r.output(ctx, client, selected.detail)
}

func (r *CertificateData) checkLookupKeys() error {
	// The lookup-key rule is syntactic: explicitly empty domain or tags values
	// count as supplied, but the empty values do not filter candidates later.
	if r.Domain != nil || ptr.Value(r.Tags) != nil {
		return nil
	}
	return errors.New("at least one of domain or tags must be supplied")
}

func (r *CertificateData) findSummariesWithRetry(
	ctx context.Context,
	client *acm.Client,
) ([]acmtypes.CertificateSummary, error) {
	var summaries []acmtypes.CertificateSummary
	err := retry.OnError(ctx, isCertificateDataNoSummaries,
		func(ctx context.Context) error {
			found, err := r.findSummaries(ctx, client)
			if err != nil {
				return err
			}
			if len(found) == 0 {
				return errCertificateDataNoSummaries
			}
			summaries = found
			return nil
		},
		retry.WithTimeout(certificateDataLookupTimeout))
	if errors.Is(err, errCertificateDataNoSummaries) {
		return nil, errors.New("no matching ACM Certificate found")
	}
	if err != nil {
		return nil, err
	}
	return summaries, nil
}

func isCertificateDataNoSummaries(err error) bool {
	return errors.Is(err, errCertificateDataNoSummaries)
}

func (r *CertificateData) findSummaries(
	ctx context.Context,
	client *acm.Client,
) ([]acmtypes.CertificateSummary, error) {
	var summaries []acmtypes.CertificateSummary
	pager := acm.NewListCertificatesPaginator(client, r.listInput())
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list certificates: %w", err)
		}
		for _, summary := range page.CertificateSummaryList {
			if r.summaryMatches(summary) {
				summaries = append(summaries, summary)
			}
		}
	}
	return summaries, nil
}

func (r *CertificateData) listInput() *acm.ListCertificatesInput {
	in := &acm.ListCertificatesInput{
		CertificateStatuses: []acmtypes.CertificateStatus{acmtypes.CertificateStatusIssued},
	}
	if len(ptr.Value(r.Statuses)) > 0 {
		in.CertificateStatuses = certificateDataStatuses(ptr.Value(r.Statuses))
	}
	if len(ptr.Value(r.KeyTypes)) > 0 {
		in.Includes = &acmtypes.Filters{KeyTypes: certificateDataKeyTypes(ptr.Value(r.KeyTypes))}
	}
	return in
}

func (r *CertificateData) summaryMatches(summary acmtypes.CertificateSummary) bool {
	if r.Domain != nil && *r.Domain != "" && aws.ToString(summary.DomainName) != *r.Domain {
		return false
	}
	if len(ptr.Value(r.Types)) > 0 && !slices.Contains(ptr.Value(r.Types), string(summary.Type)) {
		return false
	}
	return true
}

func certificateDataStatuses(values []string) []acmtypes.CertificateStatus {
	out := make([]acmtypes.CertificateStatus, 0, len(values))
	for _, value := range values {
		out = append(out, acmtypes.CertificateStatus(value))
	}
	return out
}

func certificateDataKeyTypes(values []string) []acmtypes.KeyAlgorithm {
	out := make([]acmtypes.KeyAlgorithm, 0, len(values))
	for _, value := range values {
		out = append(out, acmtypes.KeyAlgorithm(value))
	}
	return out
}

type certificateDataCandidate struct {
	detail *acmtypes.CertificateDetail
}

func (r *CertificateData) findCandidates(
	ctx context.Context,
	client *acm.Client,
	summaries []acmtypes.CertificateSummary,
) ([]certificateDataCandidate, error) {
	var candidates []certificateDataCandidate
	for _, summary := range summaries {
		arn := aws.ToString(summary.CertificateArn)
		if arn == "" {
			continue
		}
		detail, err := describeCertificate(ctx, client, arn)
		if err != nil {
			if err == runtime.ErrNotFound {
				continue
			}
			return nil, err
		}
		if detail.Status == acmtypes.CertificateStatusValidationTimedOut {
			continue
		}
		if len(ptr.Value(r.Tags)) > 0 {
			// Candidate filtering deliberately uses the strict tag reader. Only the
			// final output tag listing suppresses unsupported-tagging errors.
			tags, err := certificateDataTags(ctx, client, arn)
			if err != nil {
				if errors.Is(err, runtime.ErrNotFound) {
					continue
				}
				return nil, err
			}
			if !certificateDataContainsTags(tags, ptr.Value(r.Tags)) {
				continue
			}
		}
		candidates = append(candidates, certificateDataCandidate{detail: detail})
	}
	return candidates, nil
}

func certificateDataContainsTags(actual, want map[string]string) bool {
	for key, value := range want {
		actualValue, ok := actual[key]
		if !ok || actualValue != value {
			return false
		}
	}
	return true
}

func (r *CertificateData) selectCandidate(
	candidates []certificateDataCandidate,
) (certificateDataCandidate, error) {
	switch len(candidates) {
	case 0:
		return certificateDataCandidate{}, errors.New("no matching ACM Certificate found")
	case 1:
		return candidates[0], nil
	}
	if !r.MostRecent {
		return certificateDataCandidate{}, errors.New(
			"multiple ACM Certificates matched; use more specific filters or set most-recent")
	}
	return certificateDataMostRecent(candidates)
}

func certificateDataMostRecent(
	candidates []certificateDataCandidate,
) (certificateDataCandidate, error) {
	selected := candidates[0]
	status := selected.detail.Status
	for _, candidate := range candidates[1:] {
		if candidate.detail.Status != status {
			return certificateDataCandidate{}, errors.New(
				"most-recent cannot select ACM Certificates with different statuses")
		}
		if certificateDataSelectionTime(candidate.detail).After(
			certificateDataSelectionTime(selected.detail)) {
			selected = candidate
		}
	}
	return selected, nil
}

func certificateDataSelectionTime(detail *acmtypes.CertificateDetail) time.Time {
	if detail.Status == acmtypes.CertificateStatusIssued {
		if detail.NotBefore != nil {
			return *detail.NotBefore
		}
		return time.Time{}
	}
	if detail.CreatedAt != nil {
		return *detail.CreatedAt
	}
	return time.Time{}
}

func (r *CertificateData) output(
	ctx context.Context,
	client *acm.Client,
	detail *acmtypes.CertificateDetail,
) (*CertificateDataOutput, error) {
	arn := aws.ToString(detail.CertificateArn)
	out := &CertificateDataOutput{
		Arn:    arn,
		Domain: aws.ToString(detail.DomainName),
		Status: string(detail.Status),
	}
	if detail.Status == acmtypes.CertificateStatusIssued {
		resp, err := client.GetCertificate(ctx, &acm.GetCertificateInput{
			CertificateArn: aws.String(arn),
		})
		if err != nil {
			return nil, fmt.Errorf("get certificate %s: %w", arn, err)
		}
		if resp != nil {
			out.Certificate = resp.Certificate
			out.CertificateChain = resp.CertificateChain
		}
	}
	tags, err := certificateDataOutputTags(ctx, client, arn)
	if err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			return nil, fmt.Errorf("selected ACM Certificate %s not found", arn)
		}
		return nil, err
	}
	out.Tags = certificateDataUserTags(tags)
	return out, nil
}

// certificateDataOutputTags implements the final output tag listing. Unlike
// candidate tag filtering, it suppresses unsupported-tagging errors outside the
// standard aws partition and returns an empty tag set.
func certificateDataOutputTags(
	ctx context.Context,
	client *acm.Client,
	arn string,
) (map[string]string, error) {
	tags, err := certificateDataTags(ctx, client, arn)
	if err == nil {
		return tags, nil
	}
	if certificateDataSuppressOutputTagError(client.Options().Region, err) {
		return map[string]string{}, nil
	}
	return nil, err
}

func certificateDataSuppressOutputTagError(region string, err error) bool {
	return partition.UnsupportedOperation(region, err)
}

func certificateDataTags(
	ctx context.Context,
	client *acm.Client,
	arn string,
) (map[string]string, error) {
	resp, err := client.ListTagsForCertificate(ctx, &acm.ListTagsForCertificateInput{
		CertificateArn: aws.String(arn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("list tags for certificate %s: %w", arn, err)
	}
	tags := make(map[string]string, len(resp.Tags))
	for _, tag := range resp.Tags {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return tags, nil
}

func certificateDataUserTags(tags map[string]string) map[string]string {
	out := make(map[string]string)
	for key, value := range tags {
		if strings.HasPrefix(key, "aws:") {
			continue
		}
		out[key] = value
	}
	return out
}
