package wait

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUntil(t *testing.T) {
	probeErr := errors.New("describe failed")
	fast := config{timeout: time.Second, interval: 0}

	cases := []struct {
		name        string
		consecutive int
		readys      []bool
		probeErr    error
		errAfter    int
		wantErr     string
		wantProbes  int
		cfg         *config
	}{
		{
			name:        "ready on the first probe returns at once",
			consecutive: 1,
			readys:      []bool{true},
			wantProbes:  1,
		},
		{
			name:        "polls until ready",
			consecutive: 1,
			readys:      []bool{false, false, true},
			wantProbes:  3,
		},
		{
			name:        "stable requires consecutive ready observations",
			consecutive: 3,
			readys:      []bool{true, true, true},
			wantProbes:  3,
		},
		{
			name:        "stable resets the run on a not-ready observation",
			consecutive: 3,
			readys:      []bool{true, true, false, true, true, true},
			wantProbes:  6,
		},
		{
			name:        "probe error stops at once",
			consecutive: 1,
			readys:      []bool{false},
			probeErr:    probeErr,
			errAfter:    1,
			wantErr:     "describe failed",
			wantProbes:  1,
		},
		{
			name:        "timeout when never ready",
			consecutive: 1,
			readys:      []bool{false},
			cfg:         &config{timeout: 0, interval: 0},
			wantErr:     "timed out waiting for role to be ready",
			wantProbes:  1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fast
			if tc.cfg != nil {
				cfg = *tc.cfg
			}
			probes := 0
			probe := func(context.Context) (bool, error) {
				i := probes
				probes++
				if tc.probeErr != nil && probes >= tc.errAfter {
					return false, tc.probeErr
				}
				if i < len(tc.readys) {
					return tc.readys[i], nil
				}
				return tc.readys[len(tc.readys)-1], nil
			}
			err := until(context.Background(), "role", tc.consecutive, probe, cfg)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantProbes, probes)
		})
	}
}

func TestUntilHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := config{timeout: time.Minute, interval: time.Minute}
	err := until(ctx, "role", 1, func(context.Context) (bool, error) {
		return false, nil
	}, cfg)
	assert.ErrorIs(t, err, context.Canceled)
}
