package codebuild

import (
	"github.com/cloudboss/unobin/pkg/runtime"

	svc "github.com/cloudboss/unobin-library-aws/internal/service/codebuild"
)

// Infer the runtime's private prior type from the registration method.
type projectPrior interface {
	~struct {
		Inputs        map[string]any
		Outputs       map[string]any
		Observed      map[string]any
		Configuration map[string]any
	}
}

func composeProject[P projectPrior](
	registration runtime.ResourceRegistration,
	known func(any, any, P, map[string][]string, bool) ([]string, error),
) projectRegistration[P] {
	return projectRegistration[P]{registration, known}
}

type projectRegistration[P projectPrior] struct {
	runtime.ResourceRegistration
	known func(any, any, P, map[string][]string, bool) ([]string, error)
}

func (r projectRegistration[P]) ReplacementReasons(
	receiver, cfg any, prior P,
) ([]string, error) {
	return r.KnownReplacementReasons(receiver, cfg, prior, nil, false)
}

func (r projectRegistration[P]) KnownReplacementReasons(
	receiver, cfg any, prior P, pending map[string][]string, configPending bool,
) ([]string, error) {
	reasons, err := r.known(receiver, cfg, prior, pending, configPending)
	if err != nil || len(reasons) == 0 {
		return reasons, err
	}
	if _, unknown := pending["source"]; unknown {
		return reasons, nil
	}
	// Binding changes bypass this hook in the runtime. Create repeats this check.
	if err := receiver.(*svc.ProjectResource).PrepareCreate(); err != nil {
		return nil, err
	}
	return reasons, nil
}
