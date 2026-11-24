package dynamodb

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type backupFake struct {
	create   func(context.Context, *dynamodb.CreateBackupInput) (*dynamodb.CreateBackupOutput, error)
	describe func(context.Context, *dynamodb.DescribeBackupInput) (
		*dynamodb.DescribeBackupOutput, error,
	)
}

func (f backupFake) CreateBackup(ctx context.Context, in *dynamodb.CreateBackupInput,
	_ ...func(*dynamodb.Options)) (*dynamodb.CreateBackupOutput, error) {
	return f.create(ctx, in)
}

func (f backupFake) DescribeBackup(ctx context.Context, in *dynamodb.DescribeBackupInput,
	_ ...func(*dynamodb.Options)) (*dynamodb.DescribeBackupOutput, error) {
	return f.describe(ctx, in)
}

func backupTestClock() (backupClock, *[]time.Duration) {
	now := time.Unix(100, 0)
	var sleeps []time.Duration
	return backupClock{
		now: func() time.Time { return now },
		sleep: func(ctx context.Context, d time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			sleeps = append(sleeps, d)
			now = now.Add(d)
			return nil
		},
	}, &sleeps
}

func TestCreateBackupCompletedMetadata(t *testing.T) {
	r := CreateBackupAction{TableName: "table", BackupName: aws.String("chosen")}
	clock, sleeps := backupTestClock()
	var calls []string
	date := time.Date(2026, 11, 23, 0, 1, 2, 123, time.FixedZone("offset", 3600))
	client := backupFake{
		create: func(_ context.Context, in *dynamodb.CreateBackupInput) (
			*dynamodb.CreateBackupOutput, error,
		) {
			calls = append(calls, "create")
			assert.Equal(t, &dynamodb.CreateBackupInput{
				TableName: aws.String("table"), BackupName: aws.String("chosen"),
			}, in)
			return &dynamodb.CreateBackupOutput{BackupDetails: &types.BackupDetails{
				BackupArn: aws.String("created"),
			}}, nil
		},
		describe: func(_ context.Context, in *dynamodb.DescribeBackupInput) (
			*dynamodb.DescribeBackupOutput, error,
		) {
			calls = append(calls, "describe")
			assert.Equal(t, &dynamodb.DescribeBackupInput{BackupArn: aws.String("created")}, in)
			return &dynamodb.DescribeBackupOutput{BackupDescription: &types.BackupDescription{
				BackupDetails: &types.BackupDetails{
					BackupArn: aws.String("final"), BackupStatus: types.BackupStatusAvailable,
					BackupCreationDateTime: &date, BackupSizeBytes: aws.Int64(42),
				},
			}}, nil
		},
	}
	out, err := r.runBackup(t.Context(), client, clock)
	require.NoError(t, err)
	assert.Equal(t, &CreateBackupActionOutput{
		BackupArn: "final", BackupCreationDateTime: date.UTC().Format(time.RFC3339),
		BackupSizeBytes: 42,
	}, out)
	assert.Equal(t, []string{"create", "describe"}, calls)
	assert.Empty(t, *sleeps)
}

func TestCreateBackupRequestDefaults(t *testing.T) {
	for _, table := range []string{"x", "arn:aws:dynamodb:us-east-1:123456789012:table/a b"} {
		r := CreateBackupAction{TableName: table}
		in, timeout := r.prepareBackupRequest()
		assert.Equal(t, table, aws.ToString(in.TableName))
		assert.Regexp(t, "^"+regexp.QuoteMeta(table)+"-backup-terraform-[A-Za-z0-9]{26}$",
			aws.ToString(in.BackupName))
		assert.Equal(t, 600*time.Second, timeout)
		second, _ := r.prepareBackupRequest()
		assert.NotEqual(t, in.BackupName, second.BackupName)
	}
	for _, seconds := range []int64{-1, 0, 1, 23} {
		r := CreateBackupAction{
			TableName: strings.Repeat("t", 1024), BackupName: aws.String(""), Timeout: &seconds,
		}
		in, timeout := r.prepareBackupRequest()
		assert.Equal(t, strings.Repeat("t", 1024), aws.ToString(in.TableName))
		assert.Equal(t, "", aws.ToString(in.BackupName))
		assert.Equal(t, time.Duration(seconds)*time.Second, timeout)
	}
}

func TestBackupCreationRetryPredicate(t *testing.T) {
	for _, test := range []struct {
		err  error
		want bool
	}{
		{&types.BackupInUseException{}, true},
		{&types.LimitExceededException{Message: aws.String("daily quota")}, true},
		{&types.ContinuousBackupsUnavailableException{
			Message: aws.String("prefix Backups are being enabled suffix"),
		}, true},
		{&types.ContinuousBackupsUnavailableException{
			Message: aws.String("backups are being enabled"),
		}, false},
		{&types.ContinuousBackupsUnavailableException{}, false},
		{&types.TableNotFoundException{}, false},
		{&types.ResourceNotFoundException{}, false},
		{&types.ThrottlingException{}, false},
		{errors.New("Backups are being enabled"), false},
	} {
		t.Run(fmt.Sprintf("%T/%s", test.err, test.err.Error()), func(t *testing.T) {
			assert.Equal(t, test.want, backupCreationRetryable(test.err))
			assert.Equal(t, test.want, backupCreationRetryable(fmt.Errorf("wrapped: %w", test.err)))
			clock, sleeps := backupTestClock()
			var calls []string
			wrapped := fmt.Errorf("wrapped: %w", test.err)
			client := backupFake{create: func(context.Context, *dynamodb.CreateBackupInput) (
				*dynamodb.CreateBackupOutput, error,
			) {
				calls = append(calls, "create")
				if len(calls) == 1 {
					return nil, wrapped
				}
				return backupSuccess(), nil
			}}
			out, err := createBackupWithRetry(t.Context(), client,
				&dynamodb.CreateBackupInput{}, time.Minute, clock)
			if test.want {
				require.NoError(t, err)
				assert.Equal(t, backupSuccess(), out)
				assert.Equal(t, []string{"create", "create"}, calls)
				assert.Equal(t, []time.Duration{500 * time.Millisecond}, *sleeps)
			} else {
				require.ErrorIs(t, err, test.err)
				assert.Equal(t, []string{"create"}, calls)
				assert.Empty(t, *sleeps)
			}
		})
	}
}

func TestBackupCreationRetryTiming(t *testing.T) {
	clock, sleeps := backupTestClock()
	r := CreateBackupAction{TableName: "arn:table:a b", Timeout: aws.Int64(26)}
	last := &types.LimitExceededException{Message: aws.String("limit")}
	var requests []*dynamodb.CreateBackupInput
	client := backupFake{create: func(_ context.Context, in *dynamodb.CreateBackupInput) (
		*dynamodb.CreateBackupOutput, error,
	) {
		requests = append(requests, in)
		return nil, last
	}}
	_, err := r.runBackup(t.Context(), client, clock)
	require.ErrorIs(t, err, last)
	assert.Equal(t, []time.Duration{
		500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
	}, *sleeps)
	require.NotEmpty(t, requests)
	assert.Equal(t, []*dynamodb.CreateBackupInput{
		requests[0], requests[0], requests[0], requests[0], requests[0],
		requests[0], requests[0], requests[0], requests[0],
	}, requests)
	assert.Regexp(t, `^arn:table:a b-backup-terraform-[A-Za-z0-9]{26}$`,
		aws.ToString(requests[0].BackupName))
	for _, request := range requests {
		assert.Same(t, requests[0], request)
	}
}

func backupSuccess() *dynamodb.CreateBackupOutput {
	return &dynamodb.CreateBackupOutput{BackupDetails: &types.BackupDetails{
		BackupArn: aws.String("backup"),
	}}
}

func backupDescription(status types.BackupStatus) *dynamodb.DescribeBackupOutput {
	return &dynamodb.DescribeBackupOutput{BackupDescription: &types.BackupDescription{
		BackupDetails: &types.BackupDetails{
			BackupArn: aws.String("backup"), BackupCreationDateTime: aws.Time(time.Unix(100, 0)),
			BackupStatus: status,
		},
	}}
}

func TestBackupRetryThenSeparateWaitBudget(t *testing.T) {
	r := CreateBackupAction{TableName: "table", Timeout: aws.Int64(1)}
	clock, sleeps := backupTestClock()
	var calls []string
	client := backupFake{
		create: func(context.Context, *dynamodb.CreateBackupInput) (
			*dynamodb.CreateBackupOutput, error,
		) {
			calls = append(calls, "create")
			if len(calls) < 3 {
				return nil, &types.BackupInUseException{}
			}
			return backupSuccess(), nil
		},
		describe: func(context.Context, *dynamodb.DescribeBackupInput) (
			*dynamodb.DescribeBackupOutput, error,
		) {
			calls = append(calls, "describe")
			if len(calls) == 4 {
				return backupDescription(types.BackupStatusCreating), nil
			}
			return backupDescription(types.BackupStatusAvailable), nil
		},
	}
	out, err := r.runBackup(t.Context(), client, clock)
	require.NoError(t, err)
	assert.Equal(t, int64(0), out.BackupSizeBytes)
	assert.Equal(t, []string{"create", "create", "create", "describe", "describe"}, calls)
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second, 0}, *sleeps)
}

func TestBackupWaitFailures(t *testing.T) {
	apiError := &types.BackupNotFoundException{Message: aws.String("gone")}
	for _, test := range []struct {
		name     string
		response *dynamodb.DescribeBackupOutput
		err      error
		want     string
	}{
		{"deleted", backupDescription(types.BackupStatusDeleted), nil, "failed with status DELETED"},
		{"unknown", backupDescription("OTHER"), nil, `unexpected status "OTHER"`},
		{"empty status", backupDescription(""), nil, `unexpected status ""`},
		{"API", nil, apiError, "describe backup backup"},
		{"nil response", nil, nil, "response has no backup details"},
		{"nil description", &dynamodb.DescribeBackupOutput{}, nil, "response has no backup details"},
		{"nil details", &dynamodb.DescribeBackupOutput{
			BackupDescription: &types.BackupDescription{},
		}, nil, "response has no backup details"},
		{"creating timeout", backupDescription(types.BackupStatusCreating), nil, "timed out"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock, sleeps := backupTestClock()
			var calls []string
			client := backupFake{
				create: func(context.Context, *dynamodb.CreateBackupInput) (
					*dynamodb.CreateBackupOutput, error,
				) {
					calls = append(calls, "create")
					return backupSuccess(), nil
				},
				describe: func(context.Context, *dynamodb.DescribeBackupInput) (
					*dynamodb.DescribeBackupOutput, error,
				) {
					calls = append(calls, "describe")
					return test.response, test.err
				},
			}
			r := CreateBackupAction{TableName: "table", Timeout: aws.Int64(1)}
			_, err := r.runBackup(t.Context(), client, clock)
			require.ErrorContains(t, err, test.want)
			if test.err != nil {
				assert.ErrorIs(t, err, test.err)
			}
			if test.name == "creating timeout" {
				assert.Equal(t, []string{"create", "describe", "describe", "describe"}, calls)
				assert.Equal(t, []time.Duration{0, 500 * time.Millisecond, time.Second}, *sleeps)
			} else {
				assert.Equal(t, []string{"create", "describe"}, calls)
				assert.Empty(t, *sleeps)
			}
		})
	}
}

func TestBackupWaitSchedule(t *testing.T) {
	clock, sleeps := backupTestClock()
	start := clock.now()
	var attempts []time.Duration
	var requests []*dynamodb.DescribeBackupInput
	wantAttempts := []time.Duration{
		0, 0, 500 * time.Millisecond, 1500 * time.Millisecond, 3500 * time.Millisecond,
		7500 * time.Millisecond, 15500 * time.Millisecond, 25500 * time.Millisecond,
		35500 * time.Millisecond, 45500 * time.Millisecond, 55500 * time.Millisecond,
	}
	client := backupFake{describe: func(_ context.Context, in *dynamodb.DescribeBackupInput) (
		*dynamodb.DescribeBackupOutput, error,
	) {
		attempts = append(attempts, clock.now().Sub(start))
		requests = append(requests, in)
		if len(attempts) == len(wantAttempts) {
			return backupDescription(types.BackupStatusAvailable), nil
		}
		return backupDescription(types.BackupStatusCreating), nil
	}}
	out, err := waitForBackup(t.Context(), client, "backup", time.Minute, clock)
	require.NoError(t, err)
	assert.Equal(t, &CreateBackupActionOutput{
		BackupArn: "backup", BackupCreationDateTime: start.UTC().Format(time.RFC3339),
	}, out)
	assert.Equal(t, wantAttempts, attempts)
	assert.Equal(t, []time.Duration{
		0, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
	}, *sleeps)
	wantRequests := make([]*dynamodb.DescribeBackupInput, len(wantAttempts))
	for i := range wantRequests {
		wantRequests[i] = &dynamodb.DescribeBackupInput{BackupArn: aws.String("backup")}
	}
	assert.Equal(t, wantRequests, requests)
}

func TestBackupWaitProgressAdvancesSharedDelay(t *testing.T) {
	for _, test := range []struct {
		name   string
		fetch  time.Duration
		sleeps []time.Duration
	}{
		{"before threshold", 29 * time.Second, []time.Duration{0, 500 * time.Millisecond, time.Second}},
		{"threshold crossed during sleep", 30*time.Second - time.Nanosecond,
			[]time.Duration{0, 500 * time.Millisecond, 2 * time.Second}},
		{"at threshold", 30 * time.Second, []time.Duration{0, time.Second, 2 * time.Second}},
		{"after threshold", 31 * time.Second, []time.Duration{0, time.Second, 2 * time.Second}},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			start := now
			var sleeps, attempts []time.Duration
			clock := backupClock{
				now: func() time.Time { return now },
				sleep: func(_ context.Context, delay time.Duration) error {
					sleeps = append(sleeps, delay)
					now = now.Add(delay)
					return nil
				},
			}
			client := backupFake{describe: func(context.Context, *dynamodb.DescribeBackupInput) (
				*dynamodb.DescribeBackupOutput, error,
			) {
				attempts = append(attempts, now.Sub(start))
				switch len(attempts) {
				case 1:
					now = now.Add(5 * time.Second)
				case 2:
					now = now.Add(test.fetch)
				case 4:
					return backupDescription(types.BackupStatusAvailable), nil
				}
				return backupDescription(types.BackupStatusCreating), nil
			}}
			_, err := waitForBackup(t.Context(), client, "backup", time.Minute, clock)
			require.NoError(t, err)
			assert.Equal(t, test.sleeps, sleeps)
			assert.Equal(t, []time.Duration{
				0, 5 * time.Second, 5*time.Second + test.fetch + test.sleeps[1],
				5*time.Second + test.fetch + test.sleeps[1] + test.sleeps[2],
			}, attempts)
		})
	}
}

func TestBackupWaitDeadlineOutcomes(t *testing.T) {
	for _, test := range []struct {
		name     string
		timeout  time.Duration
		fetch    time.Duration
		ready    time.Duration
		attempts []time.Duration
		sleeps   []time.Duration
		wantErr  bool
	}{
		{
			name: "no fetch after expiry", timeout: time.Second, ready: 750 * time.Millisecond,
			attempts: []time.Duration{0, 0, 500 * time.Millisecond},
			sleeps:   []time.Duration{0, 500 * time.Millisecond, time.Second}, wantErr: true,
		},
		{
			name: "fetch at equality", timeout: 500 * time.Millisecond, ready: 500 * time.Millisecond,
			attempts: []time.Duration{0, 0, 500 * time.Millisecond},
			sleeps:   []time.Duration{0, 500 * time.Millisecond},
		},
		{
			name: "available fetch crosses expiry", timeout: time.Second, fetch: 2 * time.Second,
			ready: 2 * time.Second, attempts: []time.Duration{0},
		},
		{
			name: "creating fetch crosses expiry", timeout: time.Second, fetch: 2 * time.Second,
			ready: time.Hour, attempts: []time.Duration{0}, sleeps: []time.Duration{0}, wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			start := now
			var sleeps, attempts []time.Duration
			clock := backupClock{
				now: func() time.Time { return now },
				sleep: func(_ context.Context, delay time.Duration) error {
					sleeps = append(sleeps, delay)
					now = now.Add(delay)
					return nil
				},
			}
			client := backupFake{describe: func(ctx context.Context, in *dynamodb.DescribeBackupInput) (
				*dynamodb.DescribeBackupOutput, error,
			) {
				assert.Same(t, t.Context(), ctx)
				assert.Equal(t, &dynamodb.DescribeBackupInput{BackupArn: aws.String("backup")}, in)
				attempts = append(attempts, now.Sub(start))
				now = now.Add(test.fetch)
				if now.Sub(start) >= test.ready {
					return backupDescription(types.BackupStatusAvailable), nil
				}
				return backupDescription(types.BackupStatusCreating), nil
			}}
			out, err := waitForBackup(t.Context(), client, "backup", test.timeout, clock)
			if test.wantErr {
				require.ErrorContains(t, err, "timed out waiting for backup backup to be AVAILABLE")
				assert.Nil(t, out)
			} else {
				require.NoError(t, err)
				assert.Equal(t, &CreateBackupActionOutput{
					BackupArn: "backup", BackupCreationDateTime: start.UTC().Format(time.RFC3339),
				}, out)
			}
			assert.Equal(t, test.attempts, attempts)
			assert.Equal(t, test.sleeps, sleeps)
		})
	}
}

func TestBackupWaitCancellationBeforeRepeatedFetch(t *testing.T) {
	for _, positiveSleep := range []bool{false, true} {
		t.Run(fmt.Sprint(positiveSleep), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock, _ := backupTestClock()
			var attempts, sleeps []time.Duration
			start := clock.now()
			sleep := clock.sleep
			clock.sleep = func(ctx context.Context, delay time.Duration) error {
				sleeps = append(sleeps, delay)
				err := sleep(ctx, delay)
				if delay > 0 || !positiveSleep {
					cancel()
				}
				return err
			}
			client := backupFake{describe: func(got context.Context, _ *dynamodb.DescribeBackupInput) (
				*dynamodb.DescribeBackupOutput, error,
			) {
				assert.Same(t, ctx, got)
				attempts = append(attempts, clock.now().Sub(start))
				return backupDescription(types.BackupStatusCreating), nil
			}}
			_, err := waitForBackup(ctx, client, "backup", time.Nanosecond, clock)
			require.ErrorIs(t, err, context.Canceled)
			wantAttempts, wantSleeps := []time.Duration{0}, []time.Duration{0}
			if positiveSleep {
				wantAttempts = append(wantAttempts, 0)
				wantSleeps = append(wantSleeps, 500*time.Millisecond)
			}
			assert.Equal(t, wantAttempts, attempts)
			assert.Equal(t, wantSleeps, sleeps)
		})
	}
}

func TestBackupMalformedIdentity(t *testing.T) {
	for _, created := range []*dynamodb.CreateBackupOutput{
		nil, {}, {BackupDetails: &types.BackupDetails{}},
		{BackupDetails: &types.BackupDetails{BackupArn: aws.String("")}},
	} {
		clock, _ := backupTestClock()
		client := backupFake{create: func(context.Context, *dynamodb.CreateBackupInput) (
			*dynamodb.CreateBackupOutput, error,
		) {
			return created, nil
		}}
		_, err := (&CreateBackupAction{TableName: "table"}).runBackup(t.Context(), client, clock)
		require.ErrorContains(t, err, "create backup for table table: response has no backup ARN")
	}
	for _, field := range []string{"ARN", "date"} {
		out := backupDescription(types.BackupStatusAvailable)
		if field == "ARN" {
			out.BackupDescription.BackupDetails.BackupArn = nil
		} else {
			out.BackupDescription.BackupDetails.BackupCreationDateTime = nil
		}
		clock, _ := backupTestClock()
		client := backupFake{describe: func(context.Context, *dynamodb.DescribeBackupInput) (
			*dynamodb.DescribeBackupOutput, error,
		) {
			return out, nil
		}}
		_, err := waitForBackup(t.Context(), client, "backup", time.Second, clock)
		require.ErrorContains(t, err, "response has no backup ARN or creation time")
	}
}

func TestBackupNonpositiveTimeoutCreatesBeforeWaitError(t *testing.T) {
	for _, seconds := range []int64{-1, 0} {
		clock, sleeps := backupTestClock()
		var requests []*dynamodb.CreateBackupInput
		client := backupFake{create: func(_ context.Context, in *dynamodb.CreateBackupInput) (
			*dynamodb.CreateBackupOutput, error,
		) {
			requests = append(requests, in)
			return backupSuccess(), nil
		}}
		r := CreateBackupAction{TableName: "table", BackupName: aws.String("name"), Timeout: &seconds}
		_, err := r.runBackup(t.Context(), client, clock)
		require.ErrorContains(t, err, "wait for backup backup: timeout must be positive")
		assert.Equal(t, []*dynamodb.CreateBackupInput{{
			TableName: aws.String("table"), BackupName: aws.String("name"),
		}}, requests)
		assert.Empty(t, *sleeps)
	}
}

func TestBackupCancellation(t *testing.T) {
	for _, phase := range []string{"before creation", "retry", "poll", "before poll"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock, _ := backupTestClock()
			clock.sleep = func(_ context.Context, delay time.Duration) error {
				if phase == "poll" && delay == 0 {
					return nil
				}
				cancel()
				return ctx.Err()
			}
			var calls []string
			client := backupFake{
				create: func(got context.Context, _ *dynamodb.CreateBackupInput) (
					*dynamodb.CreateBackupOutput, error,
				) {
					assert.Same(t, ctx, got)
					calls = append(calls, "create")
					if phase == "retry" {
						return nil, &types.LimitExceededException{}
					}
					if phase == "before poll" {
						cancel()
					}
					return backupSuccess(), nil
				},
				describe: func(got context.Context, _ *dynamodb.DescribeBackupInput) (
					*dynamodb.DescribeBackupOutput, error,
				) {
					assert.Same(t, ctx, got)
					calls = append(calls, "describe")
					return backupDescription(types.BackupStatusCreating), nil
				},
			}
			if phase == "before creation" {
				cancel()
			}
			_, err := (&CreateBackupAction{TableName: "table"}).runBackup(ctx, client, clock)
			require.ErrorIs(t, err, context.Canceled)
			want := []string{"create"}
			if phase == "before creation" {
				want = nil
			}
			if phase == "poll" {
				want = append(want, "describe", "describe")
			}
			assert.Equal(t, want, calls)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, sleepForBackup(ctx, time.Hour), context.Canceled)
	assert.NoError(t, sleepForBackup(t.Context(), 0))
}

func TestBackupRetryExpiryGrace(t *testing.T) {
	for _, test := range []struct {
		seconds  int64
		attempts []time.Duration
	}{
		{1, []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond, 3500 * time.Millisecond}},
		{0, []time.Duration{0, 500 * time.Millisecond}},
		{-1, []time.Duration{0, 500 * time.Millisecond}},
	} {
		t.Run(fmt.Sprint(test.seconds), func(t *testing.T) {
			clock, _ := backupTestClock()
			start := clock.now()
			var attempts []time.Duration
			var last error
			client := backupFake{create: func(context.Context, *dynamodb.CreateBackupInput) (
				*dynamodb.CreateBackupOutput, error,
			) {
				attempts = append(attempts, clock.now().Sub(start))
				last = &types.LimitExceededException{Message: aws.String(fmt.Sprint(len(attempts)))}
				return nil, last
			}}
			_, err := createBackupWithRetry(t.Context(), client, &dynamodb.CreateBackupInput{},
				time.Duration(test.seconds)*time.Second, clock)
			require.ErrorIs(t, err, last)
			assert.Equal(t, test.attempts, attempts)
		})
	}
}
