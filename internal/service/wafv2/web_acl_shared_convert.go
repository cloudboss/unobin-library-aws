package wafv2

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

var visibilityMetricNamePattern = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)

func validateVisibilityConfig(config WebACLVisibilityConfig) error {
	length := utf8.RuneCountInString(config.MetricName)
	if length < 1 || length > 128 {
		return fmt.Errorf("metric-name must be 1..128 characters")
	}
	if !visibilityMetricNamePattern.MatchString(config.MetricName) {
		return fmt.Errorf("metric-name must match ^[0-9A-Za-z_-]+$")
	}
	if config.MetricName == "All" || config.MetricName == "Default_Action" {
		return fmt.Errorf("metric-name is reserved")
	}
	return nil
}

func validateCaptchaConfig(config *WebACLCaptchaConfig) error {
	if config == nil || config.ImmunityTimeProperty == nil {
		return nil
	}
	immunityTime := config.ImmunityTimeProperty.ImmunityTime
	if immunityTime < 60 || immunityTime > 259200 {
		return fmt.Errorf("captcha immunity-time must be 60..259200")
	}
	return nil
}

func expandCaptchaConfig(config *WebACLCaptchaConfig) (*awstypes.CaptchaConfig, error) {
	if err := validateCaptchaConfig(config); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, nil
	}
	expanded := &awstypes.CaptchaConfig{}
	if config.ImmunityTimeProperty != nil {
		expanded.ImmunityTimeProperty = &awstypes.ImmunityTimeProperty{
			ImmunityTime: aws.Int64(config.ImmunityTimeProperty.ImmunityTime),
		}
	}
	return expanded, nil
}

func validateChallengeConfig(config *WebACLChallengeConfig) error {
	if config == nil || config.ImmunityTimeProperty == nil {
		return nil
	}
	immunityTime := config.ImmunityTimeProperty.ImmunityTime
	if immunityTime < 300 || immunityTime > 259200 {
		return fmt.Errorf("challenge immunity-time must be 300..259200")
	}
	return nil
}

func expandChallengeConfig(config *WebACLChallengeConfig) (*awstypes.ChallengeConfig, error) {
	if err := validateChallengeConfig(config); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, nil
	}
	expanded := &awstypes.ChallengeConfig{}
	if config.ImmunityTimeProperty != nil {
		expanded.ImmunityTimeProperty = &awstypes.ImmunityTimeProperty{
			ImmunityTime: aws.Int64(config.ImmunityTimeProperty.ImmunityTime),
		}
	}
	return expanded, nil
}
