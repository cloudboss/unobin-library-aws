package acm

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *CertificateResource) ResourceDefinition() runtime.ResourceDefinition[CertificateResource, *CertificateResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[CertificateResource, *CertificateResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[CertificateResource, *CertificateResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[CertificateResource]{
				runtime.InputField(func(input *CertificateResource) **string { return &input.CertificateAuthorityArn }),
				runtime.InputField(func(input *CertificateResource) **string { return &input.DomainName }),
				runtime.InputField(func(input *CertificateResource) **string { return &input.KeyAlgorithm }),
				runtime.InputField(func(input *CertificateResource) **[]string { return &input.SubjectAlternativeNames }),
				runtime.InputField(func(input *CertificateResource) **string { return &input.ValidationMethod }),
				runtime.InputField(func(input *CertificateResource) **[]CertificateValidationOption { return &input.ValidationOption }),
			},
		},
	}
}

func (r *CertificateValidationResource) ResourceDefinition() runtime.ResourceDefinition[CertificateValidationResource, *CertificateValidationResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[CertificateValidationResource, *CertificateValidationResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[CertificateValidationResource, *CertificateValidationResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[CertificateValidationResource]{
				runtime.InputField(func(input *CertificateValidationResource) *string { return &input.CertificateArn }),
				runtime.InputField(func(input *CertificateValidationResource) **[]string { return &input.ValidationRecordFqdns }),
			},
		},
	}
}
