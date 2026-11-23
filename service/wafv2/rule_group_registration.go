package wafv2

import (
	"maps"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/structdecode"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/wafv2"
)

func makeRuleGroupResource[T, Out any, PT resourcePtr[T, Out]]() runtime.ResourceRegistration {
	return ruleGroupRegistration{makeResource[T, Out, PT]()}
}

type ruleGroupRegistration struct {
	runtime.ResourceRegistration
}

func (r ruleGroupRegistration) InputsEqual(receiver any, prior map[string]any) (bool, error) {
	return r.KnownInputsEqual(receiver, prior, nil)
}

func (r ruleGroupRegistration) KnownInputsEqual(
	receiver any, prior map[string]any, pending map[string][]string,
) (bool, error) {
	if _, unknown := pending["rules"]; unknown {
		return r.ResourceRegistration.KnownInputsEqual(receiver, prior, pending)
	}

	// EqualBy descriptors reject types containing empty marker objects.
	desired := *receiver.(*svc.RuleGroupResource)
	var before svc.RuleGroupResource
	if err := structdecode.Decode(&before, prior); err != nil {
		return false, err
	}
	if !desired.RuleSetEqual(before) {
		return false, nil
	}

	desired.Rules = nil
	remaining := maps.Clone(prior)
	delete(remaining, "rules")
	return r.ResourceRegistration.KnownInputsEqual(&desired, remaining, pending)
}
