package acm

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	acmsvc "github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	smithy "github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCertificateDataListInput(t *testing.T) {
	cases := []struct {
		name         string
		lookup       CertificateData
		wantStatuses []acmtypes.CertificateStatus
		wantKeyTypes []acmtypes.KeyAlgorithm
	}{
		{
			name:         "defaults to issued statuses and no key filter",
			wantStatuses: []acmtypes.CertificateStatus{acmtypes.CertificateStatusIssued},
		},
		{
			name: "uses explicit statuses and key types",
			lookup: CertificateData{
				Statuses: []string{"PENDING_VALIDATION", "ISSUED"},
				KeyTypes: []string{"RSA_2048", "EC_prime256v1"},
			},
			wantStatuses: []acmtypes.CertificateStatus{
				acmtypes.CertificateStatus("PENDING_VALIDATION"),
				acmtypes.CertificateStatusIssued,
			},
			wantKeyTypes: []acmtypes.KeyAlgorithm{
				acmtypes.KeyAlgorithm("RSA_2048"),
				acmtypes.KeyAlgorithm("EC_prime256v1"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.lookup.listInput()
			assert.Equal(t, tc.wantStatuses, in.CertificateStatuses)
			if len(tc.wantKeyTypes) == 0 {
				assert.Nil(t, in.Includes)
				return
			}
			require.NotNil(t, in.Includes)
			assert.Equal(t, tc.wantKeyTypes, in.Includes.KeyTypes)
		})
	}
}

func TestCertificateDataCheckLookupKeys(t *testing.T) {
	emptyDomain := ""

	cases := []struct {
		name    string
		lookup  CertificateData
		wantErr string
	}{
		{
			name:    "rejects omitted selectors",
			wantErr: "at least one of domain or tags must be supplied",
		},
		{
			name:   "accepts explicit empty domain",
			lookup: CertificateData{Domain: &emptyDomain},
		},
		{
			name:   "accepts explicit empty tags",
			lookup: CertificateData{Tags: map[string]string{}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.lookup.checkLookupKeys()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCertificateDataDecodeExplicitEmptySelectors(t *testing.T) {
	cases := []struct {
		name   string
		inputs map[string]any
	}{
		{
			name:   "empty domain",
			inputs: map[string]any{"domain": ""},
		},
		{
			name:   "empty tags",
			inputs: map[string]any{"tags": map[string]any{}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var lookup CertificateData
			require.NoError(t, runtime.Decode(&lookup, tc.inputs))
			require.NoError(t, lookup.checkLookupKeys())
		})
	}
}

func TestCertificateDataSummaryMatches(t *testing.T) {
	emptyDomain := ""
	exampleDomain := "example.com"
	otherDomain := "other.example.com"

	cases := []struct {
		name    string
		lookup  CertificateData
		summary acmtypes.CertificateSummary
		want    bool
	}{
		{
			name:   "empty domain does not filter",
			lookup: CertificateData{Domain: &emptyDomain},
			summary: acmtypes.CertificateSummary{
				DomainName: aws.String(exampleDomain),
			},
			want: true,
		},
		{
			name:   "non-empty domain matches exact summary domain",
			lookup: CertificateData{Domain: &exampleDomain},
			summary: acmtypes.CertificateSummary{
				DomainName: aws.String(exampleDomain),
			},
			want: true,
		},
		{
			name:   "non-empty domain rejects different summary domain",
			lookup: CertificateData{Domain: &exampleDomain},
			summary: acmtypes.CertificateSummary{
				DomainName: aws.String(otherDomain),
			},
		},
		{
			name:   "non-empty types filter requires membership",
			lookup: CertificateData{Types: []string{"IMPORTED"}},
			summary: acmtypes.CertificateSummary{
				Type: acmtypes.CertificateTypeAmazonIssued,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.lookup.summaryMatches(tc.summary))
		})
	}
}

func TestCertificateDetailFromDescribe(t *testing.T) {
	detail := &acmtypes.CertificateDetail{CertificateArn: aws.String("arn")}

	cases := []struct {
		name    string
		resp    *acmsvc.DescribeCertificateOutput
		wantErr bool
	}{
		{
			name:    "nil output is not found",
			wantErr: true,
		},
		{
			name:    "nil certificate is not found",
			resp:    &acmsvc.DescribeCertificateOutput{},
			wantErr: true,
		},
		{
			name: "returns certificate detail",
			resp: &acmsvc.DescribeCertificateOutput{Certificate: detail},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := certificateDetailFromDescribe(tc.resp)
			if tc.wantErr {
				require.ErrorIs(t, err, runtime.ErrNotFound)
				return
			}
			require.NoError(t, err)
			assert.Same(t, detail, got)
		})
	}
}

func TestCertificateDataContainsTags(t *testing.T) {
	cases := []struct {
		name   string
		actual map[string]string
		want   map[string]string
		ok     bool
	}{
		{
			name:   "contains requested subset",
			actual: map[string]string{"env": "dev", "team": "platform"},
			want:   map[string]string{"team": "platform"},
			ok:     true,
		},
		{
			name:   "rejects missing empty-valued key",
			actual: map[string]string{"env": "dev"},
			want:   map[string]string{"empty": ""},
		},
		{
			name:   "rejects wrong value",
			actual: map[string]string{"team": "app"},
			want:   map[string]string{"team": "platform"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.ok, certificateDataContainsTags(tc.actual, tc.want))
		})
	}
}

func TestCertificateDataUserTags(t *testing.T) {
	tags := certificateDataUserTags(map[string]string{
		"aws:cloudformation:stack-name": "owned",
		"empty":                         "",
		"team":                          "platform",
	})

	assert.Equal(t, map[string]string{"empty": "", "team": "platform"}, tags)
	assert.Equal(t, map[string]string{}, certificateDataUserTags(nil))
}

type certificateDataAPIError struct {
	code    string
	message string
}

func (e *certificateDataAPIError) Error() string {
	return fmt.Sprintf("%s: %s", e.code, e.message)
}

func (e *certificateDataAPIError) ErrorCode() string { return e.code }
func (e *certificateDataAPIError) ErrorMessage() string {
	return e.message
}
func (e *certificateDataAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestCertificateDataSuppressOutputTagError(t *testing.T) {
	cases := []struct {
		name   string
		region string
		err    error
		want   bool
	}{
		{
			name:   "standard partition access denied remains fatal",
			region: "us-east-1",
			err:    &certificateDataAPIError{code: "AccessDenied"},
		},
		{
			name:   "nonstandard partition access denied is suppressed",
			region: "us-iso-east-1",
			err:    &certificateDataAPIError{code: "AccessDenied"},
			want:   true,
		},
		{
			name:   "wrapped unsupported operation is suppressed",
			region: "cn-north-1",
			err: fmt.Errorf("list tags: %w",
				&certificateDataAPIError{code: "UnsupportedOperation"}),
			want: true,
		},
		{
			name:   "validation error needs tagging message",
			region: "us-gov-west-1",
			err: &certificateDataAPIError{
				code:    "ValidationError",
				message: "this partition does not support tagging",
			},
			want: true,
		},
		{
			name:   "validation error without tagging message remains fatal",
			region: "us-gov-west-1",
			err: &certificateDataAPIError{
				code:    "ValidationError",
				message: "bad certificate arn",
			},
		},
		{
			name:   "resource not found remains fatal",
			region: "us-iso-east-1",
			err:    &acmtypes.ResourceNotFoundException{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, certificateDataSuppressOutputTagError(tc.region, tc.err))
		})
	}
}

func TestCertificateDataMostRecent(t *testing.T) {
	oldTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)

	cases := []struct {
		name       string
		candidates []certificateDataCandidate
		want       *acmtypes.CertificateDetail
		wantErr    string
	}{
		{
			name: "issued uses latest not-before",
			candidates: []certificateDataCandidate{
				{detail: &acmtypes.CertificateDetail{
					CertificateArn: aws.String("old"),
					Status:         acmtypes.CertificateStatusIssued,
					NotBefore:      &oldTime,
				}},
				{detail: &acmtypes.CertificateDetail{
					CertificateArn: aws.String("new"),
					Status:         acmtypes.CertificateStatusIssued,
					NotBefore:      &newTime,
				}},
			},
			want: &acmtypes.CertificateDetail{CertificateArn: aws.String("new")},
		},
		{
			name: "non-issued uses latest created-at",
			candidates: []certificateDataCandidate{
				{detail: &acmtypes.CertificateDetail{
					CertificateArn: aws.String("old"),
					Status:         acmtypes.CertificateStatusPendingValidation,
					CreatedAt:      &oldTime,
				}},
				{detail: &acmtypes.CertificateDetail{
					CertificateArn: aws.String("new"),
					Status:         acmtypes.CertificateStatusPendingValidation,
					CreatedAt:      &newTime,
				}},
			},
			want: &acmtypes.CertificateDetail{CertificateArn: aws.String("new")},
		},
		{
			name: "mixed statuses error",
			candidates: []certificateDataCandidate{
				{detail: &acmtypes.CertificateDetail{Status: acmtypes.CertificateStatusIssued}},
				{detail: &acmtypes.CertificateDetail{
					Status: acmtypes.CertificateStatusPendingValidation,
				}},
			},
			wantErr: "different statuses",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := certificateDataMostRecent(tc.candidates)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, *tc.want.CertificateArn, *got.detail.CertificateArn)
		})
	}
}
