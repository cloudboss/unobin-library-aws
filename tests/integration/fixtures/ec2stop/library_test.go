package ec2stop

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedPreservesDeferredStrings(t *testing.T) {
	for _, value := range []string{"", "i-abcdef01", "invalid"} {
		r := SeedAction{Value: value}
		out, err := r.Run(t.Context(), runtime.NoConfig{})
		require.NoError(t, err)
		assert.Equal(t, &SeedOutput{Value: value}, out)
	}
}
