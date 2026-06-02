package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errTransient = errors.New("transient")
	errFatal     = errors.New("fatal")
)

func retryTransient(err error) bool { return errors.Is(err, errTransient) }

func TestOnError(t *testing.T) {
	fast := config{timeout: time.Second, interval: 0}

	cases := []struct {
		name      string
		results   []error
		retryable func(error) bool
		cfg       *config
		wantErr   error
		wantCalls int
	}{
		{
			name:      "succeeds on first try",
			results:   []error{nil},
			retryable: retryTransient,
			wantCalls: 1,
		},
		{
			name:      "retries transient then succeeds",
			results:   []error{errTransient, errTransient, nil},
			retryable: retryTransient,
			wantCalls: 3,
		},
		{
			name:      "non-retryable returns at once",
			results:   []error{errFatal},
			retryable: retryTransient,
			wantErr:   errFatal,
			wantCalls: 1,
		},
		{
			name:      "transient that never clears times out with last error",
			results:   []error{errTransient},
			retryable: retryTransient,
			cfg:       &config{timeout: 0, interval: 0},
			wantErr:   errTransient,
			wantCalls: 1,
		},
		{
			name:      "stops on a non-retryable error mid-retry",
			results:   []error{errTransient, errFatal, nil},
			retryable: retryTransient,
			wantErr:   errFatal,
			wantCalls: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fast
			if tc.cfg != nil {
				cfg = *tc.cfg
			}
			calls := 0
			fn := func(context.Context) error {
				i := calls
				calls++
				if i < len(tc.results) {
					return tc.results[i]
				}
				return tc.results[len(tc.results)-1]
			}
			err := onError(context.Background(), tc.retryable, fn, cfg)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantCalls, calls)
		})
	}
}

func TestOnErrorHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := config{timeout: time.Minute, interval: time.Minute}
	err := onError(ctx, retryTransient, func(context.Context) error {
		return errTransient
	}, cfg)
	assert.ErrorIs(t, err, context.Canceled)
}
