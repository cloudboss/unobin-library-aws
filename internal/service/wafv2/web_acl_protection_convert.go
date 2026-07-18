package wafv2

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"unicode/utf8"

	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"golang.org/x/net/publicsuffix"
)

var (
	tokenDomainPattern      = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
	tokenDomainLabelPattern = regexp.MustCompile(
		`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`,
	)
	wafTagPattern = regexp.MustCompile(`^[\p{L}\p{Z}\p{N}_.:/=+@-]*$`)
)

func validateDataProtectionConfig(config *WebACLDataProtectionConfig) error {
	if config == nil {
		return nil
	}
	if len(config.DataProtections) < 1 || len(config.DataProtections) > 26 {
		return fmt.Errorf("data-protections must contain 1..26 items")
	}
	for index, protection := range config.DataProtections {
		if !validDataProtectionAction(protection.Action) {
			return fmt.Errorf(
				"data-protection item %d action must be SUBSTITUTION or HASH",
				index,
			)
		}
		if !validDataProtectionFieldType(protection.Field.FieldType) {
			return fmt.Errorf(
				"data-protection item %d field-type must be one of SINGLE_HEADER, "+
					"SINGLE_COOKIE, SINGLE_QUERY_ARGUMENT, QUERY_STRING, BODY",
				index,
			)
		}
		if protection.Field.FieldKeys == nil {
			continue
		}
		if len(*protection.Field.FieldKeys) > 100 {
			return fmt.Errorf(
				"data-protection item %d field-keys must contain at most 100 items",
				index,
			)
		}
		for keyIndex, key := range *protection.Field.FieldKeys {
			length := utf8.RuneCountInString(key)
			if length < 1 || length > 64 || strings.TrimSpace(key) == "" {
				return fmt.Errorf(
					"data-protection item %d field-key must be 1..64 nonblank "+
						"characters at index %d",
					index,
					keyIndex,
				)
			}
		}
	}
	return nil
}

func validDataProtectionAction(value string) bool {
	return value == string(awstypes.DataProtectionActionSubstitution) ||
		value == string(awstypes.DataProtectionActionHash)
}

func validDataProtectionFieldType(value string) bool {
	switch awstypes.FieldToProtectType(value) {
	case awstypes.FieldToProtectTypeSingleHeader,
		awstypes.FieldToProtectTypeSingleCookie,
		awstypes.FieldToProtectTypeSingleQueryArgument,
		awstypes.FieldToProtectTypeQueryString,
		awstypes.FieldToProtectTypeBody:
		return true
	default:
		return false
	}
}

func expandDataProtectionConfig(
	config *WebACLDataProtectionConfig,
) *awstypes.DataProtectionConfig {
	if config == nil {
		return nil
	}
	protections := make([]awstypes.DataProtection, len(config.DataProtections))
	for index, protection := range config.DataProtections {
		protections[index] = awstypes.DataProtection{
			Action: awstypes.DataProtectionAction(protection.Action),
			Field: &awstypes.FieldToProtect{
				FieldKeys: cloneOptionalStrings(protection.Field.FieldKeys),
				FieldType: awstypes.FieldToProtectType(protection.Field.FieldType),
			},
			ExcludeRateBasedDetails: protection.ExcludeRateBasedDetails,
			ExcludeRuleMatchDetails: protection.ExcludeRuleMatchDetails,
		}
	}
	return &awstypes.DataProtectionConfig{DataProtections: protections}
}

func validateOnSourceDDoSConfig(config *WebACLOnSourceDDoSConfig) error {
	if config == nil {
		return nil
	}
	mode := awstypes.LowReputationMode(config.ALBLowReputationMode)
	if mode != awstypes.LowReputationModeActiveUnderDdos &&
		mode != awstypes.LowReputationModeAlwaysOn {
		return fmt.Errorf(
			"alb-low-reputation-mode must be ACTIVE_UNDER_DDOS or ALWAYS_ON",
		)
	}
	return nil
}

func expandOnSourceDDoSConfig(
	config *WebACLOnSourceDDoSConfig,
) *awstypes.OnSourceDDoSProtectionConfig {
	if config == nil {
		return nil
	}
	return &awstypes.OnSourceDDoSProtectionConfig{
		ALBLowReputationMode: awstypes.LowReputationMode(config.ALBLowReputationMode),
	}
}

func validateTokenDomains(domains *[]string) error {
	if domains == nil {
		return nil
	}
	for index, domain := range *domains {
		length := utf8.RuneCountInString(domain)
		if length < 1 || length > 253 {
			return fmt.Errorf("token-domain must be 1..253 characters at index %d", index)
		}
		if net.ParseIP(domain) != nil {
			return fmt.Errorf("token-domain must be a valid DNS name at index %d", index)
		}
		if !tokenDomainPattern.MatchString(domain) {
			return fmt.Errorf("token-domain has invalid characters at index %d", index)
		}
		if !validTokenDomainDNSName(domain) {
			return fmt.Errorf("token-domain must be a valid DNS name at index %d", index)
		}
		if _, err := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(domain)); err != nil {
			return fmt.Errorf(
				"token-domain must include a registrable domain above its suffix at index %d",
				index,
			)
		}
	}
	return nil
}

func validTokenDomainDNSName(domain string) bool {
	for _, label := range strings.Split(domain, ".") {
		if !tokenDomainLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func expandTokenDomains(domains *[]string) []string {
	return cloneOptionalStrings(domains)
}

func cloneOptionalStrings(values *[]string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(*values))
	copy(out, *values)
	return out
}

func validateWebACLTags(tags *map[string]string) error {
	if tags == nil {
		return nil
	}
	if len(*tags) > 50 {
		return fmt.Errorf("tags must contain at most 50 items")
	}
	for key, value := range *tags {
		keyLength := utf8.RuneCountInString(key)
		if keyLength < 1 || keyLength > 128 {
			return fmt.Errorf("tag key must be 1..128 characters")
		}
		if !wafTagPattern.MatchString(key) {
			return fmt.Errorf("tag key must match the WAF character pattern")
		}
		if strings.HasPrefix(key, "aws:") {
			return fmt.Errorf("tag key must not start with aws:")
		}
		valueLength := utf8.RuneCountInString(value)
		if valueLength > 256 {
			return fmt.Errorf("tag value must be 0..256 characters")
		}
		if !wafTagPattern.MatchString(value) {
			return fmt.Errorf("tag value must match the WAF character pattern")
		}
	}
	return nil
}
