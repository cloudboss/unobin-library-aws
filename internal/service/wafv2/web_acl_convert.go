package wafv2

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

type webACLIdentity struct {
	ARN   string
	ID    string
	Name  string
	Scope awstypes.Scope
}

func (r *WebACLResource) createInput(name string) (*awssvc.CreateWebACLInput, error) {
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
	in := &awssvc.CreateWebACLInput{
		ApplicationConfig:    expandApplicationConfig(r.ApplicationConfig),
		AssociationConfig:    expandAssociationConfig(r.AssociationConfig),
		CaptchaConfig:        captchaConfig,
		ChallengeConfig:      challengeConfig,
		CustomResponseBodies: expandCustomResponseBodies(r.CustomResponseBodies),
		DataProtectionConfig: expandDataProtectionConfig(r.DataProtectionConfig),
		DefaultAction:        defaultAction,
		Name:                 aws.String(name),
		OnSourceDDoSProtectionConfig: expandOnSourceDDoSConfig(
			r.OnSourceDDoSConfig,
		),
		Rules:            rules,
		Scope:            awstypes.Scope(r.Scope),
		VisibilityConfig: expandVisibilityConfig(r.VisibilityConfig),
		Description:      r.Description,
	}
	if r.TokenDomains != nil {
		in.TokenDomains = expandTokenDomains(r.TokenDomains)
	}
	if r.Tags != nil {
		in.Tags = expandTags(*r.Tags)
	}
	return in, nil
}

func expandDefaultAction(action WebACLDefaultAction) (*awstypes.DefaultAction, error) {
	if err := validateDefaultAction(action); err != nil {
		return nil, err
	}
	out := &awstypes.DefaultAction{}
	if action.Allow != nil {
		out.Allow = &awstypes.AllowAction{
			CustomRequestHandling: expandCustomRequestHandling(
				action.Allow.CustomRequestHandling,
			),
		}
	}
	if action.Block != nil {
		out.Block = &awstypes.BlockAction{
			CustomResponse: expandCustomResponse(action.Block.CustomResponse),
		}
	}
	return out, nil
}

func validateDefaultAction(action WebACLDefaultAction) error {
	if countSet(action.Allow != nil, action.Block != nil) != 1 {
		return fmt.Errorf("default-action must contain exactly one action")
	}
	if action.Allow != nil {
		return validateCustomRequestHandling(action.Allow.CustomRequestHandling)
	}
	return validateCustomResponse(action.Block.CustomResponse)
}

func expandVisibilityConfig(config WebACLVisibilityConfig) *awstypes.VisibilityConfig {
	return &awstypes.VisibilityConfig{
		CloudWatchMetricsEnabled: config.CloudWatchMetricsEnabled,
		MetricName:               aws.String(config.MetricName),
		SampledRequestsEnabled:   config.SampledRequestsEnabled,
	}
}

func expandTags(tags map[string]string) []awstypes.Tag {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]awstypes.Tag, 0, len(tags))
	for _, key := range keys {
		out = append(out, awstypes.Tag{Key: aws.String(key), Value: aws.String(tags[key])})
	}
	return out
}

func parseWebACLARN(value string) (webACLIdentity, error) {
	parsed, err := awsarn.Parse(value)
	if err != nil {
		return webACLIdentity{}, fmt.Errorf("parse web ACL ARN: %w", err)
	}
	if parsed.Service != "wafv2" {
		return webACLIdentity{}, fmt.Errorf("parse web ACL ARN: service is %q", parsed.Service)
	}
	parts := strings.Split(parsed.Resource, "/")
	if len(parts) != 4 || parts[1] != "webacl" || parts[2] == "" || parts[3] == "" {
		return webACLIdentity{}, fmt.Errorf("parse web ACL ARN: invalid resource %q", parsed.Resource)
	}
	var scope awstypes.Scope
	switch parts[0] {
	case "regional":
		scope = awstypes.ScopeRegional
	case "global":
		scope = awstypes.ScopeCloudfront
	default:
		return webACLIdentity{}, fmt.Errorf("parse web ACL ARN: invalid scope %q", parts[0])
	}
	return webACLIdentity{
		ARN:   value,
		ID:    parts[3],
		Name:  parts[2],
		Scope: scope,
	}, nil
}

func webACLOutput(
	identity webACLIdentity,
	response *awssvc.GetWebACLOutput,
) (*WebACLResourceOutput, error) {
	if response == nil || response.WebACL == nil {
		return nil, nil
	}
	acl := response.WebACL
	if aws.ToString(acl.ARN) == "" || aws.ToString(acl.Id) == "" ||
		aws.ToString(acl.Name) == "" || aws.ToString(response.LockToken) == "" {
		return nil, fmt.Errorf("get web ACL %s: response has incomplete identity", identity.ID)
	}
	actual, err := parseWebACLARN(aws.ToString(acl.ARN))
	if err != nil {
		return nil, fmt.Errorf("get web ACL %s: %w", identity.ID, err)
	}
	if actual.ID != identity.ID || actual.Name != identity.Name ||
		actual.Scope != identity.Scope || aws.ToString(acl.Id) != identity.ID ||
		aws.ToString(acl.Name) != identity.Name {
		return nil, fmt.Errorf("get web ACL %s: response identity does not match request", identity.ID)
	}
	return &WebACLResourceOutput{
		ARN:                       actual.ARN,
		ID:                        actual.ID,
		Capacity:                  acl.Capacity,
		LabelNamespace:            aws.ToString(acl.LabelNamespace),
		ApplicationIntegrationURL: response.ApplicationIntegrationURL,
		LockToken:                 aws.ToString(response.LockToken),
	}, nil
}
