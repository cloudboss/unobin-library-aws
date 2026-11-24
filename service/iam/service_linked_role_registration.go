package iam

import (
	"maps"
	"strings"

	"github.com/cloudboss/unobin/pkg/runtime"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/iam"
)

// The runtime's prior type is private; infer it from the registration methods.
type serviceLinkedRolePrior interface {
	~struct {
		Inputs        map[string]any
		Outputs       map[string]any
		Observed      map[string]any
		Configuration map[string]any
	}
}

func makeServiceLinkedRoleResource[
	T, Out any, PT resourcePtr[T, Out],
]() runtime.ResourceRegistration {
	registration := makeResource[T, Out, PT]()
	return composeServiceLinkedRole(registration, registration.KnownReplacementReasons)
}

func composeServiceLinkedRole[P serviceLinkedRolePrior](
	registration runtime.ResourceRegistration,
	known func(any, any, P, map[string][]string, bool) ([]string, error),
) serviceLinkedRoleRegistration[P] {
	return serviceLinkedRoleRegistration[P]{registration, known}
}

type serviceLinkedRoleRegistration[P serviceLinkedRolePrior] struct {
	runtime.ResourceRegistration
	known func(any, any, P, map[string][]string, bool) ([]string, error)
}

func serviceLinkedRoleComparison(
	receiver any, before map[string]any, pending map[string][]string,
) (any, map[string]any) {
	desired := *receiver.(*svc.ServiceLinkedRoleResource)
	prior := maps.Clone(before)
	_, unknownService := pending["aws-service-name"]
	_, unknownSuffix := pending["custom-suffix"]
	if !unknownService && !unknownSuffix &&
		strings.Contains(desired.AWSServiceName, ".application-autoscaling.") &&
		(desired.CustomSuffix == nil || *desired.CustomSuffix == "") {
		desired.CustomSuffix = nil
		delete(prior, "custom-suffix")
	}
	return &desired, prior
}

func (r serviceLinkedRoleRegistration[P]) InputsEqual(
	receiver any, prior map[string]any,
) (bool, error) {
	return r.KnownInputsEqual(receiver, prior, nil)
}

func (r serviceLinkedRoleRegistration[P]) KnownInputsEqual(
	receiver any, prior map[string]any, pending map[string][]string,
) (bool, error) {
	desired, before := serviceLinkedRoleComparison(receiver, prior, pending)
	return r.ResourceRegistration.KnownInputsEqual(desired, before, pending)
}

func (r serviceLinkedRoleRegistration[P]) ReplacementReasons(
	receiver, cfg any, prior P,
) ([]string, error) {
	return r.KnownReplacementReasons(receiver, cfg, prior, nil, false)
}

func (r serviceLinkedRoleRegistration[P]) KnownReplacementReasons(
	receiver, cfg any, prior P, pending map[string][]string, configPending bool,
) ([]string, error) {
	before := struct {
		Inputs        map[string]any
		Outputs       map[string]any
		Observed      map[string]any
		Configuration map[string]any
	}(prior)
	desired, inputs := serviceLinkedRoleComparison(receiver, before.Inputs, pending)
	before.Inputs = inputs
	return r.known(desired, cfg, P(before),
		pending, configPending)
}
