package wafv2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

func (r *WebACLResource) updateInput(
	prior *WebACLResourceOutput,
) (*awssvc.UpdateWebACLInput, error) {
	identity, err := priorWebACLIdentity(prior)
	if err != nil {
		return nil, err
	}
	if prior.LockToken == "" {
		return nil, fmt.Errorf("prior web ACL output has no lock-token")
	}
	if err := r.ValidateInputs(context.Background(), nil); err != nil {
		return nil, err
	}
	captchaConfig, err := expandCaptchaConfig(r.CaptchaConfig)
	if err != nil {
		return nil, fmt.Errorf("expand captcha-config: %w", err)
	}
	challengeConfig, err := expandChallengeConfig(r.ChallengeConfig)
	if err != nil {
		return nil, fmt.Errorf("expand challenge-config: %w", err)
	}
	defaultAction, err := expandDefaultAction(r.DefaultAction)
	if err != nil {
		return nil, fmt.Errorf("expand default-action: %w", err)
	}
	rules, err := expandRules(r.Rules)
	if err != nil {
		return nil, fmt.Errorf("expand rules: %w", err)
	}
	if r.Rules != nil && len(*r.Rules) == 0 {
		rules = []awstypes.Rule{}
	}
	return &awssvc.UpdateWebACLInput{
		AssociationConfig:    expandAssociationConfig(r.AssociationConfig),
		CaptchaConfig:        captchaConfig,
		ChallengeConfig:      challengeConfig,
		CustomResponseBodies: expandCustomResponseBodies(r.CustomResponseBodies),
		DataProtectionConfig: expandDataProtectionConfig(r.DataProtectionConfig),
		DefaultAction:        defaultAction,
		Description:          r.Description,
		Id:                   aws.String(identity.ID),
		LockToken:            aws.String(prior.LockToken),
		Name:                 aws.String(identity.Name),
		OnSourceDDoSProtectionConfig: expandOnSourceDDoSConfig(
			r.OnSourceDDoSConfig,
		),
		Rules:            rules,
		Scope:            identity.Scope,
		TokenDomains:     expandTokenDomains(r.TokenDomains),
		VisibilityConfig: expandVisibilityConfig(r.VisibilityConfig),
	}, nil
}
