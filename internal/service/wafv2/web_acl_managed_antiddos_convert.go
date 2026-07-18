package wafv2

import (
	"fmt"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

func validateManagedAntiDDoSRuleSet(in *WebACLAWSManagedRulesAntiDDoSRuleSet) error {
	if in == nil {
		return fmt.Errorf("Anti-DDoS rule set is required")
	}
	if in.ClientSideActionConfig == nil {
		return fmt.Errorf("client-side-action-config is required")
	}
	challenge := in.ClientSideActionConfig.Challenge
	if challenge == nil {
		return fmt.Errorf("client-side-action-config: challenge is required")
	}
	if challenge.UsageOfAction != string(awstypes.UsageOfActionEnabled) &&
		challenge.UsageOfAction != string(awstypes.UsageOfActionDisabled) {
		return fmt.Errorf("usage-of-action must be ENABLED or DISABLED")
	}
	if err := validateManagedAntiDDoSSensitivity(
		"sensitivity",
		challenge.Sensitivity,
	); err != nil {
		return err
	}
	if err := validateManagedAntiDDoSSensitivity(
		"sensitivity-to-block",
		in.SensitivityToBlock,
	); err != nil {
		return err
	}
	exemptions := challenge.ExemptURIRegularExpressions
	if exemptions != nil {
		if len(*exemptions) > 5 {
			return fmt.Errorf(
				"exempt-uri-regular-expressions must contain at most 5 members",
			)
		}
		for index, exemption := range *exemptions {
			length := utf8.RuneCountInString(exemption.RegexString)
			if length < 1 || length > 512 {
				return fmt.Errorf(
					"regex-string at index %d must be 1..512 characters",
					index,
				)
			}
		}
	}
	if challenge.UsageOfAction == string(awstypes.UsageOfActionEnabled) &&
		(exemptions == nil || len(*exemptions) == 0) {
		return fmt.Errorf(
			"usage-of-action ENABLED requires 1..5 exempt-uri-regular-expressions",
		)
	}
	return nil
}

func expandManagedAntiDDoSRuleSet(
	in *WebACLAWSManagedRulesAntiDDoSRuleSet,
) (*awstypes.AWSManagedRulesAntiDDoSRuleSet, error) {
	if err := validateManagedAntiDDoSRuleSet(in); err != nil {
		return nil, err
	}
	challengeIn := in.ClientSideActionConfig.Challenge
	challengeOut := &awstypes.ClientSideAction{
		UsageOfAction: awstypes.UsageOfAction(challengeIn.UsageOfAction),
	}
	if challengeIn.ExemptURIRegularExpressions != nil {
		challengeOut.ExemptUriRegularExpressions = make(
			[]awstypes.Regex,
			len(*challengeIn.ExemptURIRegularExpressions),
		)
		for index, exemption := range *challengeIn.ExemptURIRegularExpressions {
			challengeOut.ExemptUriRegularExpressions[index] = awstypes.Regex{
				RegexString: aws.String(exemption.RegexString),
			}
		}
	}
	if challengeIn.Sensitivity != nil {
		challengeOut.Sensitivity = awstypes.SensitivityToAct(*challengeIn.Sensitivity)
	}
	out := &awstypes.AWSManagedRulesAntiDDoSRuleSet{
		ClientSideActionConfig: &awstypes.ClientSideActionConfig{
			Challenge: challengeOut,
		},
	}
	if in.SensitivityToBlock != nil {
		out.SensitivityToBlock = awstypes.SensitivityToAct(*in.SensitivityToBlock)
	}
	return out, nil
}

func validateManagedAntiDDoSSensitivity(field string, value *string) error {
	if value == nil {
		return nil
	}
	if *value != string(awstypes.SensitivityToActLow) &&
		*value != string(awstypes.SensitivityToActMedium) &&
		*value != string(awstypes.SensitivityToActHigh) {
		return fmt.Errorf("%s must be LOW, MEDIUM, or HIGH", field)
	}
	return nil
}
