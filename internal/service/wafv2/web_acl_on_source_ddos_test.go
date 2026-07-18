package wafv2

import (
	"testing"

	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandOnSourceDDoSConfig(t *testing.T) {
	tests := []struct {
		mode     string
		expected awstypes.LowReputationMode
	}{
		{mode: "ACTIVE_UNDER_DDOS", expected: awstypes.LowReputationModeActiveUnderDdos},
		{mode: "ALWAYS_ON", expected: awstypes.LowReputationModeAlwaysOn},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			input := &WebACLOnSourceDDoSConfig{ALBLowReputationMode: tt.mode}
			require.NoError(t, validateOnSourceDDoSConfig(input))
			assert.Equal(t, &awstypes.OnSourceDDoSProtectionConfig{
				ALBLowReputationMode: tt.expected,
			}, expandOnSourceDDoSConfig(input))
		})
	}
}

func TestOnSourceDDoSConfigNilAndInvalid(t *testing.T) {
	require.NoError(t, validateOnSourceDDoSConfig(nil))
	assert.Nil(t, expandOnSourceDDoSConfig(nil))

	for _, mode := range []string{"", "ACTIVE", "always_on"} {
		assert.ErrorContains(
			t,
			validateOnSourceDDoSConfig(&WebACLOnSourceDDoSConfig{
				ALBLowReputationMode: mode,
			}),
			"alb-low-reputation-mode must be ACTIVE_UNDER_DDOS or ALWAYS_ON",
		)
	}
}
