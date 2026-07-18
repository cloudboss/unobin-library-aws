package wafv2

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

func validateManagedRuleGroupConfigs(configs *[]WebACLManagedRuleGroupConfig) error {
	if configs == nil {
		return nil
	}
	for index, config := range *configs {
		if err := validateManagedRuleGroupConfig(config); err != nil {
			return fmt.Errorf("managed-rule-group-configs item %d: %w", index, err)
		}
	}
	return nil
}

func expandManagedRuleGroupConfigs(
	configs *[]WebACLManagedRuleGroupConfig,
) ([]awstypes.ManagedRuleGroupConfig, error) {
	if err := validateManagedRuleGroupConfigs(configs); err != nil {
		return nil, err
	}
	if configs == nil || len(*configs) == 0 {
		return nil, nil
	}
	out := make([]awstypes.ManagedRuleGroupConfig, len(*configs))
	for index, config := range *configs {
		converted := awstypes.ManagedRuleGroupConfig{}
		if config.AWSManagedRulesAntiDDoSRuleSet != nil {
			antiDDoS, err := expandManagedAntiDDoSRuleSet(
				config.AWSManagedRulesAntiDDoSRuleSet,
			)
			if err != nil {
				return nil, fmt.Errorf("convert Anti-DDoS config item %d: %w", index, err)
			}
			converted.AWSManagedRulesAntiDDoSRuleSet = antiDDoS
		}
		if config.AWSManagedRulesACFPRuleSet != nil {
			acfp, err := expandManagedACFPRuleSet(config.AWSManagedRulesACFPRuleSet)
			if err != nil {
				return nil, fmt.Errorf("convert ACFP config item %d: %w", index, err)
			}
			converted.AWSManagedRulesACFPRuleSet = acfp
		}
		if config.AWSManagedRulesATPRuleSet != nil {
			atp, err := expandManagedATPRuleSet(config.AWSManagedRulesATPRuleSet)
			if err != nil {
				return nil, fmt.Errorf("convert ATP config item %d: %w", index, err)
			}
			converted.AWSManagedRulesATPRuleSet = atp
		}
		if config.AWSManagedRulesBotControlRuleSet != nil {
			bot := config.AWSManagedRulesBotControlRuleSet
			converted.AWSManagedRulesBotControlRuleSet =
				&awstypes.AWSManagedRulesBotControlRuleSet{
					InspectionLevel: awstypes.InspectionLevel(bot.InspectionLevel),
				}
			if bot.EnableMachineLearning != nil {
				converted.AWSManagedRulesBotControlRuleSet.EnableMachineLearning =
					aws.Bool(*bot.EnableMachineLearning)
			}
		}
		if config.LoginPath != nil {
			converted.LoginPath = aws.String(*config.LoginPath)
		}
		if config.PasswordField != nil {
			converted.PasswordField = &awstypes.PasswordField{
				Identifier: aws.String(config.PasswordField.Identifier),
			}
		}
		if config.PayloadType != nil {
			converted.PayloadType = awstypes.PayloadType(*config.PayloadType)
		}
		if config.UsernameField != nil {
			converted.UsernameField = &awstypes.UsernameField{
				Identifier: aws.String(config.UsernameField.Identifier),
			}
		}
		out[index] = converted
	}
	return out, nil
}

func validateManagedRuleGroupConfig(config WebACLManagedRuleGroupConfig) error {
	if config.AWSManagedRulesAntiDDoSRuleSet != nil {
		if err := validateManagedAntiDDoSRuleSet(
			config.AWSManagedRulesAntiDDoSRuleSet,
		); err != nil {
			return err
		}
	}
	if config.AWSManagedRulesACFPRuleSet != nil {
		if err := validateManagedACFPRuleSet(config.AWSManagedRulesACFPRuleSet); err != nil {
			return err
		}
	}
	if config.AWSManagedRulesATPRuleSet != nil {
		if err := validateManagedATPRuleSet(config.AWSManagedRulesATPRuleSet); err != nil {
			return err
		}
	}
	if config.LoginPath != nil {
		if err := validateManagedConfigText("login-path", *config.LoginPath, 256); err != nil {
			return err
		}
	}
	if config.PayloadType != nil &&
		*config.PayloadType != string(awstypes.PayloadTypeJson) &&
		*config.PayloadType != string(awstypes.PayloadTypeFormEncoded) {
		return fmt.Errorf("payload-type must be JSON or FORM_ENCODED")
	}
	if config.PasswordField != nil {
		if err := validateManagedConfigText(
			"password-field identifier",
			config.PasswordField.Identifier,
			512,
		); err != nil {
			return err
		}
	}
	if config.UsernameField != nil {
		if err := validateManagedConfigText(
			"username-field identifier",
			config.UsernameField.Identifier,
			512,
		); err != nil {
			return err
		}
	}
	if config.AWSManagedRulesBotControlRuleSet != nil {
		return validateManagedBotControlRuleSet(
			config.AWSManagedRulesBotControlRuleSet,
		)
	}
	return nil
}

func validateManagedBotControlRuleSet(
	bot *WebACLAWSManagedRulesBotControlRuleSet,
) error {
	if bot.InspectionLevel != string(awstypes.InspectionLevelCommon) &&
		bot.InspectionLevel != string(awstypes.InspectionLevelTargeted) {
		return fmt.Errorf("inspection-level must be COMMON or TARGETED")
	}
	if bot.EnableMachineLearning != nil && *bot.EnableMachineLearning &&
		bot.InspectionLevel != string(awstypes.InspectionLevelTargeted) {
		return fmt.Errorf("enable-machine-learning requires TARGETED inspection-level")
	}
	return nil
}

func validateManagedConfigText(field, value string, maximum int) error {
	length := utf8.RuneCountInString(value)
	if length < 1 || length > maximum || strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must be 1..%d nonblank characters", field, maximum)
	}
	return nil
}
