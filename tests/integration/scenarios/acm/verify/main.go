// verify checks the ACM certificate the scenario applied against the phase named
// in the VERIFY_PHASE environment variable. A certificate has no name of its
// own, only a server-assigned ARN the driver does not pass in, so the verifier
// finds it by the marker tag the scenario sets: it lists every certificate and
// reads each one's tags, matching client-side, since ACM has no server-side tag
// filter and an emulator may ignore one anyway. It only reads cloud state:
// applied requires the certificate present, with the expected domain name and
// at least one DNS validation record (the value downstream validation reads);
// destroyed requires it gone. Tearing the certificate down is the destroy
// plan's job, not the verifier's.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/aws/smithy-go"
)

const (
	domainName  = "unobin-it.example.com"
	markerKey   = "unobin"
	markerValue = "acm-it"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := acm.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *acm.Client) error {
	arn, skip, err := findMarkedCertificate(ctx, client)
	if err != nil {
		return err
	}
	if skip {
		fmt.Println("skip: ACM is not modeled by this backend")
		return nil
	}
	if arn == "" {
		return fmt.Errorf("no certificate has tag %s=%s", markerKey, markerValue)
	}
	resp, err := client.DescribeCertificate(ctx, &acm.DescribeCertificateInput{
		CertificateArn: aws.String(arn),
	})
	if err != nil {
		return fmt.Errorf("describe certificate %s: %w", arn, err)
	}
	detail := resp.Certificate
	if got := aws.ToString(detail.DomainName); got != domainName {
		return fmt.Errorf("certificate %s has domain %q, want %q", arn, got, domainName)
	}
	// The DNS validation records are the resource's reason to exist; at least one
	// domain must have its CNAME record assigned by the time apply returns.
	hasRecord := false
	for _, v := range detail.DomainValidationOptions {
		if v.ResourceRecord != nil {
			hasRecord = true
			break
		}
	}
	if !hasRecord {
		return fmt.Errorf("certificate %s has no DNS validation record assigned", arn)
	}

	fmt.Printf("ok: certificate %s present for %s with a validation record\n", arn, domainName)
	return nil
}

func verifyDestroyed(ctx context.Context, client *acm.Client) error {
	arn, skip, err := findMarkedCertificate(ctx, client)
	if err != nil {
		return err
	}
	if skip {
		fmt.Println("skip: ACM is not modeled by this backend")
		return nil
	}
	if arn != "" {
		return fmt.Errorf("certificate %s still has tag %s=%s", arn, markerKey, markerValue)
	}

	fmt.Printf("ok: no certificate has tag %s=%s\n", markerKey, markerValue)
	return nil
}

// findMarkedCertificate returns the ARN of the certificate with the scenario's
// marker tag, or an empty ARN when none does. ACM has no lookup by tag, so it
// lists every certificate and reads each one's tags. skip is true
// when the backend does not model ACM, so the caller prints a skip rather than
// failing on a backend that has nothing to check.
func findMarkedCertificate(
	ctx context.Context, client *acm.Client,
) (arn string, skip bool, err error) {
	pager := acm.NewListCertificatesPaginator(client, &acm.ListCertificatesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if notImplemented(err) {
				return "", true, nil
			}
			return "", false, fmt.Errorf("list certificates: %w", err)
		}
		for _, c := range page.CertificateSummaryList {
			certArn := aws.ToString(c.CertificateArn)
			marked, err := certHasMarker(ctx, client, certArn)
			if err != nil {
				return "", false, err
			}
			if marked {
				return certArn, false, nil
			}
		}
	}
	return "", false, nil
}

// certHasMarker reports whether the certificate has the scenario's marker tag.
// A certificate that vanishes mid-scan is treated as unmarked.
func certHasMarker(ctx context.Context, client *acm.Client, arn string) (bool, error) {
	resp, err := client.ListTagsForCertificate(ctx, &acm.ListTagsForCertificateInput{
		CertificateArn: aws.String(arn),
	})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("list tags for certificate %s: %w", arn, err)
	}
	for _, t := range resp.Tags {
		if aws.ToString(t.Key) == markerKey && aws.ToString(t.Value) == markerValue {
			return true, nil
		}
	}
	return false, nil
}

func isNotFound(err error) bool {
	var notFound *acmtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

// notImplemented reports whether the backend rejected the call as unsupported,
// which an emulator without ACM returns instead of a real response.
func notImplemented(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "InternalFailure", "NotImplemented", "501":
			return true
		}
	}
	return false
}
