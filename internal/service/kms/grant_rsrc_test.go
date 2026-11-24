package kms

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestGrantPrepare(t *testing.T) {
	r := GrantResource{KeyID: "alias/original", GranteePrincipal: "lambda.amazonaws.com",
		Operations: []string{"Encrypt", "Decrypt", "Encrypt"}}
	got, err := r.prepareGrant()
	require.NoError(t, err)
	require.Equal(t, &kms.CreateGrantInput{KeyId: aws.String("alias/original"),
		GranteePrincipal: aws.String("lambda.amazonaws.com"),
		Operations: []types.GrantOperation{
			types.GrantOperationDecrypt, types.GrantOperationEncrypt,
		}}, got)
}

type grantFake struct {
	create func(context.Context, *kms.CreateGrantInput) (*kms.CreateGrantOutput, error)
	list   func(context.Context, *kms.ListGrantsInput) (*kms.ListGrantsOutput, error)
	revoke func(context.Context, *kms.RevokeGrantInput) (*kms.RevokeGrantOutput, error)
	retire func(context.Context, *kms.RetireGrantInput) (*kms.RetireGrantOutput, error)
}

func (f grantFake) CreateGrant(ctx context.Context, in *kms.CreateGrantInput,
	_ ...func(*kms.Options)) (*kms.CreateGrantOutput, error) {
	return f.create(ctx, in)
}
func (f grantFake) ListGrants(ctx context.Context, in *kms.ListGrantsInput,
	_ ...func(*kms.Options)) (*kms.ListGrantsOutput, error) {
	return f.list(ctx, in)
}
func (f grantFake) RevokeGrant(ctx context.Context, in *kms.RevokeGrantInput,
	_ ...func(*kms.Options)) (*kms.RevokeGrantOutput, error) {
	return f.revoke(ctx, in)
}
func (f grantFake) RetireGrant(ctx context.Context, in *kms.RetireGrantInput,
	_ ...func(*kms.Options)) (*kms.RetireGrantOutput, error) {
	return f.retire(ctx, in)
}

func grantTestClock() (grantClock, *[]time.Duration) {
	now := time.Unix(0, 0)
	var delays []time.Duration
	return grantClock{now: func() time.Time { return now },
		sleep: func(ctx context.Context, delay time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			delays = append(delays, delay)
			now = now.Add(delay)
			return nil
		}}, &delays
}

func grantPrior() *GrantResourceOutput {
	return &GrantResourceOutput{KeyID: "arn:aws:kms:us-east-1:123456789012:key/original",
		GrantID: "grant-1", GrantToken: aws.String("secret"), Name: aws.String("saved"),
		GranteePrincipal:  "arn:aws:iam::123456789012:role/saved",
		RetiringPrincipal: aws.String("lambda.amazonaws.com"), Operations: []string{"Encrypt"},
		GrantConstraints: &GrantConstraints{
			EncryptionContextEquals: grantContextPointer(map[string]string{"old": "value"}),
		}}
}

func grantPage(principals ...string) *kms.ListGrantsOutput {
	entries := make([]types.GrantListEntry, 0, len(principals))
	for _, principal := range principals {
		entries = append(entries, types.GrantListEntry{GrantId: aws.String("grant-1"),
			GranteePrincipal: aws.String(principal), Operations: []types.GrantOperation{"Decrypt"}})
	}
	return &kms.ListGrantsOutput{Grants: entries}
}

func TestGrantRequestOmissionAndPrincipals(t *testing.T) {
	for _, tc := range []struct {
		name        string
		constraints *GrantConstraints
		tokens      *[]string
		retiring    *string
		want        *kms.CreateGrantInput
	}{
		{name: "omitted", want: &kms.CreateGrantInput{KeyId: aws.String(""),
			GranteePrincipal: aws.String("")}},
		{name: "present empty", constraints: &GrantConstraints{}, tokens: &[]string{},
			retiring: aws.String(""), want: &kms.CreateGrantInput{KeyId: aws.String(""),
				GranteePrincipal: aws.String(""), Constraints: &types.GrantConstraints{}}},
		{name: "empty maps", constraints: &GrantConstraints{
			EncryptionContextEquals: grantContextPointer(map[string]string{}),
			EncryptionContextSubset: grantContextPointer(map[string]string{})},
			want: &kms.CreateGrantInput{KeyId: aws.String(""),
				GranteePrincipal: aws.String(""), Constraints: &types.GrantConstraints{
					EncryptionContextEquals: map[string]string{}, EncryptionContextSubset: map[string]string{}}}},
		{name: "populated", constraints: &GrantConstraints{
			EncryptionContextSubset: grantContextPointer(map[string]string{"k": "v"})},
			tokens:   &[]string{"b", "", "a", "b"},
			retiring: aws.String("ec2.amazonaws.com"), want: &kms.CreateGrantInput{KeyId: aws.String(""),
				GranteePrincipal: aws.String(""), RetiringPrincipal: aws.String("ec2.amazonaws.com"),
				GrantTokens: []string{"", "a", "b"}, Constraints: &types.GrantConstraints{
					EncryptionContextSubset: map[string]string{"k": "v"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := GrantResource{GrantConstraints: tc.constraints, GrantTokens: tc.tokens,
				RetiringPrincipal: tc.retiring}
			got, err := r.prepareGrant()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	for _, principal := range []string{"", "ec2.amazonaws.com", "a.b.c.d.amazon.com",
		"arn:aws:iam::123456789012:role/test", "arn:aws-cn::cn-north-1:aws:resource",
		"arn:aws-us-gov:iam::cw!@#$%^&*():resource", "arn:aws:iam:::resource"} {
		for _, retiring := range []bool{false, true} {
			r := GrantResource{}
			if retiring {
				r.RetiringPrincipal = &principal
			} else {
				r.GranteePrincipal = principal
			}
			_, err := r.prepareGrant()
			require.NoError(t, err, principal)
		}
	}
	for _, principal := range []string{"AWS.amazonaws.com", "a.b.c.d.e.amazonaws.com",
		"ec2.amazonaws.com.cn", " arn:aws:iam:::x", "arn:AWS:iam:::x", "arn:aws-123:iam:::x",
		"arn:aws:iam:bad:123456789012:x", "arn:aws:iam::123:x", "arn:aws:iam:::"} {
		r := GrantResource{GranteePrincipal: principal}
		_, err := r.prepareGrant()
		require.ErrorContains(t, err, "invalid grant principal", principal)
		r.GranteePrincipal, r.RetiringPrincipal = "", &principal
		_, err = r.prepareGrant()
		require.ErrorContains(t, err, "invalid grant principal", principal)
	}
}

func TestGrantRetryProgressionAndGrace(t *testing.T) {
	clock, delays := grantTestClock()
	transient := errors.New("transient")
	var calls []time.Duration
	err := grantRetry(t.Context(), clock, func(error) bool { return true },
		func(ctx context.Context) error {
			_, deadline := ctx.Deadline()
			require.False(t, deadline)
			calls = append(calls, clock.now().Sub(time.Unix(0, 0)))
			return transient
		})
	require.ErrorIs(t, err, transient)
	wantDelays := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second,
		4 * time.Second, 8 * time.Second}
	for range 18 {
		wantDelays = append(wantDelays, 10*time.Second)
	}
	require.Equal(t, wantDelays, *delays)
	wantCalls := []time.Duration{0}
	var elapsed time.Duration
	for _, delay := range wantDelays {
		elapsed += delay
		wantCalls = append(wantCalls, elapsed)
	}
	require.Equal(t, wantCalls, calls)
	require.Equal(t, 195500*time.Millisecond, elapsed)
}

func TestGrantRetryDeadlineDuringCall(t *testing.T) {
	for _, duration := range []time.Duration{
		grantPropagationTimeout, grantPropagationTimeout + time.Second,
	} {
		for _, outcome := range []string{"exhausted", "success", "canceled"} {
			t.Run(fmt.Sprint(duration, "/", outcome), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				start := time.Unix(0, 0)
				now := start
				var delays, calls []time.Duration
				clock := grantClock{now: func() time.Time { return now },
					sleep: func(ctx context.Context, delay time.Duration) error {
						delays = append(delays, delay)
						now = now.Add(delay)
						if outcome == "canceled" {
							cancel()
						}
						return ctx.Err()
					}}
				transient := errors.New("transient")
				err := grantRetry(ctx, clock, func(error) bool { return true },
					func(context.Context) error {
						calls = append(calls, now.Sub(start))
						if len(calls) == 1 {
							now = now.Add(duration)
						} else if outcome == "success" {
							return nil
						}
						return transient
					})
				wantCalls := []time.Duration{0, duration + 500*time.Millisecond}
				switch outcome {
				case "canceled":
					require.ErrorIs(t, err, context.Canceled)
					wantCalls = []time.Duration{0}
				case "success":
					require.NoError(t, err)
				default:
					require.ErrorIs(t, err, transient)
				}
				require.Equal(t, []time.Duration{500 * time.Millisecond}, delays)
				require.Equal(t, wantCalls, calls)
			})
		}
	}
}

func TestGrantLifecycleDelayedGrace(t *testing.T) {
	for _, stage := range []string{"create", "missing", "principal", "revoke", "retire"} {
		t.Run(stage, func(t *testing.T) {
			start := time.Unix(0, 0)
			now := start
			var calls []string
			var delays []time.Duration
			clock := grantClock{now: func() time.Time { return now },
				sleep: func(_ context.Context, delay time.Duration) error {
					delays = append(delays, delay)
					now = now.Add(delay)
					return nil
				}}
			readyAt := start.Add(grantPropagationTimeout + 500*time.Millisecond)
			client := grantFake{create: func(context.Context, *kms.CreateGrantInput) (
				*kms.CreateGrantOutput, error,
			) {
				calls = append(calls, "create")
				if now.Before(readyAt) {
					now = start.Add(grantPropagationTimeout)
					return nil, &types.KMSInternalException{}
				}
				return &kms.CreateGrantOutput{GrantId: aws.String("grant-1")}, nil
			}, list: func(context.Context, *kms.ListGrantsInput) (*kms.ListGrantsOutput, error) {
				calls = append(calls, "list")
				if now.Before(readyAt) {
					now = start.Add(grantPropagationTimeout)
					if stage == "missing" {
						return grantPage(), nil
					}
					return grantPage("AIDAUNRESOLVED"), nil
				}
				if stage == "revoke" || stage == "retire" {
					return grantPage(), nil
				}
				return grantPage("lambda.amazonaws.com"), nil
			}, revoke: func(context.Context, *kms.RevokeGrantInput) (*kms.RevokeGrantOutput, error) {
				calls = append(calls, "revoke")
				return nil, nil
			}, retire: func(context.Context, *kms.RetireGrantInput) (*kms.RetireGrantOutput, error) {
				calls = append(calls, "retire")
				return nil, nil
			}}
			var err error
			wantCalls := []string{"list", "list"}
			switch stage {
			case "create":
				var got *GrantResourceOutput
				got, err = (GrantResource{KeyID: grantPrior().KeyID}).
					createGrant(t.Context(), client, clock)
				if err == nil {
					require.Equal(t, "grant-1", got.GrantID)
				}
				wantCalls = []string{"create", "create", "list"}
			case "revoke", "retire":
				err = deleteGrant(t.Context(), client, grantPrior(), stage == "retire", clock)
				wantCalls = append([]string{stage}, wantCalls...)
			default:
				var got *GrantResourceOutput
				got, err = readGrant(t.Context(), client, grantPrior(), false, clock)
				if err == nil {
					require.Equal(t, "lambda.amazonaws.com", got.GranteePrincipal)
				}
			}
			require.NoError(t, err)
			require.Equal(t, readyAt, now)
			require.Equal(t, []time.Duration{500 * time.Millisecond}, delays)
			require.Equal(t, wantCalls, calls)
		})
	}
}

func TestGrantRetryCancellation(t *testing.T) {
	for _, before := range []bool{true, false} {
		ctx, cancel := context.WithCancel(t.Context())
		if before {
			cancel()
		}
		clock, delays := grantTestClock()
		calls := 0
		err := grantRetry(ctx, clock, func(error) bool { return true },
			func(context.Context) error { calls++; cancel(); return errors.New("retry") })
		require.ErrorIs(t, err, context.Canceled)
		want := 1
		if before {
			want = 0
		}
		require.Equal(t, want, calls)
		require.Empty(t, *delays)
	}
}

func TestGrantFindPaginationAndErrors(t *testing.T) {
	prior := grantPrior()
	for _, tc := range []struct {
		name    string
		pages   []*kms.ListGrantsOutput
		errs    []error
		wantErr string
		missing bool
	}{
		{name: "later page", pages: []*kms.ListGrantsOutput{{Grants: []types.GrantListEntry{
			{GrantId: aws.String("not-it")}}, Truncated: true, NextMarker: aws.String("next")},
			grantPage("lambda.amazonaws.com")}},
		{name: "duplicates across pages", pages: []*kms.ListGrantsOutput{
			{Grants: grantPage("").Grants, Truncated: true, NextMarker: aws.String("next")},
			grantPage("")}, wantErr: "duplicate"},
		{name: "duplicates same page", pages: []*kms.ListGrantsOutput{grantPage("", "")},
			wantErr: "duplicate"},
		{name: "empty", pages: []*kms.ListGrantsOutput{grantPage()}, missing: true},
		{name: "typed missing", errs: []error{&types.NotFoundException{}}, missing: true},
		{name: "later failure", pages: []*kms.ListGrantsOutput{{Truncated: true,
			NextMarker: aws.String("next")}, nil}, errs: []error{nil, errors.New("page failed")},
			wantErr: "page failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []*kms.ListGrantsInput
			client := grantFake{list: func(_ context.Context, in *kms.ListGrantsInput) (
				*kms.ListGrantsOutput, error,
			) {
				n := len(calls)
				copied := *in
				calls = append(calls, &copied)
				if n < len(tc.errs) && tc.errs[n] != nil {
					return nil, tc.errs[n]
				}
				return tc.pages[n], nil
			}}
			got, err := findGrant(t.Context(), client, prior)
			switch {
			case tc.missing:
				require.ErrorIs(t, err, runtime.ErrNotFound)
			case tc.wantErr != "":
				require.ErrorContains(t, err, tc.wantErr)
			default:
				require.NoError(t, err)
				require.Equal(t, &tc.pages[len(tc.pages)-1].Grants[0], got)
			}
			want := []*kms.ListGrantsInput{{KeyId: aws.String(prior.KeyID), Limit: aws.Int32(100)}}
			if len(tc.pages) > 1 {
				want = append(want, &kms.ListGrantsInput{
					KeyId: aws.String(prior.KeyID), Limit: aws.Int32(100), Marker: aws.String("next")})
			}
			require.Equal(t, want, calls)
		})
	}
}

func TestGrantReadSettlingAndRefresh(t *testing.T) {
	prior := grantPrior()
	clock, delays := grantTestClock()
	pages := []*kms.ListGrantsOutput{grantPage(), grantPage("AIDAUNRESOLVED"),
		grantPage("arn:other:iam:odd:bad:resource")}
	calls := 0
	client := grantFake{list: func(context.Context, *kms.ListGrantsInput) (
		*kms.ListGrantsOutput, error,
	) {
		page := pages[calls]
		calls++
		return page, nil
	}}
	got, err := readGrant(t.Context(), client, prior, false, clock)
	require.NoError(t, err)
	want := *prior
	want.GranteePrincipal = "arn:other:iam:odd:bad:resource"
	want.Operations = []string{"Decrypt"}
	require.Equal(t, &want, got)
	require.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, *delays)

	populated := grantPage("")
	populated.Grants[0].Name = aws.String("remote")
	populated.Grants[0].RetiringPrincipal = aws.String("")
	populated.Grants[0].Constraints = &types.GrantConstraints{
		EncryptionContextSubset: map[string]string{"new": "value"}}
	client.list = func(context.Context, *kms.ListGrantsInput) (*kms.ListGrantsOutput, error) {
		return populated, nil
	}
	got, err = readGrant(t.Context(), client, prior, false, clock)
	require.NoError(t, err)
	require.Equal(t, &GrantResourceOutput{KeyID: prior.KeyID, GrantID: prior.GrantID,
		GrantToken: prior.GrantToken, Name: aws.String("remote"), GranteePrincipal: "",
		RetiringPrincipal: aws.String(""), Operations: []string{"Decrypt"},
		GrantConstraints: &GrantConstraints{
			EncryptionContextSubset: grantContextPointer(map[string]string{"new": "value"})}},
		got)
	populated.Grants[0].Name = aws.String("")
	populated.Grants[0].GranteePrincipal = nil
	populated.Grants[0].RetiringPrincipal = nil
	populated.Grants[0].Constraints = nil
	got, err = readGrant(t.Context(), client, prior, false, clock)
	require.NoError(t, err)
	want = *prior
	want.Operations = []string{"Decrypt"}
	require.Equal(t, &want, got)
}

func TestGrantReadExhaustion(t *testing.T) {
	for _, principal := range []string{"missing", "AIDAUNRESOLVED", "retiring"} {
		for _, created := range []bool{true, false} {
			clock, delays := grantTestClock()
			client := grantFake{list: func(context.Context, *kms.ListGrantsInput) (
				*kms.ListGrantsOutput, error,
			) {
				if principal == "missing" {
					return nil, &types.NotFoundException{}
				}
				page := grantPage(principal)
				if principal == "retiring" {
					page.Grants[0].GranteePrincipal = nil
					page.Grants[0].RetiringPrincipal = aws.String("AIDAUNRESOLVED")
				}
				return page, nil
			}}
			_, err := readGrant(t.Context(), client, grantPrior(), created, clock)
			if principal == "missing" && !created {
				require.ErrorIs(t, err, runtime.ErrNotFound)
			} else {
				require.Error(t, err)
				require.NotErrorIs(t, err, runtime.ErrNotFound)
			}
			wantDelays := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second,
				4 * time.Second, 8 * time.Second}
			for range 18 {
				wantDelays = append(wantDelays, 10*time.Second)
			}
			require.Equal(t, wantDelays, *delays)
		}
	}
}

func TestGrantCreateRetriesAndCleanup(t *testing.T) {
	transientErrors := []error{&types.DependencyTimeoutException{}, &types.KMSInternalException{},
		&types.InvalidArnException{}}
	for _, transient := range transientErrors {
		clock, delays := grantTestClock()
		calls := 0
		r := GrantResource{KeyID: grantPrior().KeyID, GranteePrincipal: "lambda.amazonaws.com",
			Operations: []string{"Encrypt", "Encrypt"}}
		wantInput, err := r.prepareGrant()
		require.NoError(t, err)
		client := grantFake{create: func(_ context.Context, in *kms.CreateGrantInput) (
			*kms.CreateGrantOutput, error,
		) {
			require.Equal(t, wantInput, in)
			calls++
			if calls == 1 {
				return nil, transient
			}
			return &kms.CreateGrantOutput{
				GrantId: aws.String("grant-1"), GrantToken: aws.String("secret"),
			}, nil
		}, list: func(context.Context, *kms.ListGrantsInput) (*kms.ListGrantsOutput, error) {
			return grantPage("lambda.amazonaws.com"), nil
		}}
		got, err := r.createGrant(t.Context(), client, clock)
		require.NoError(t, err)
		require.Equal(t, &GrantResourceOutput{KeyID: r.KeyID, GrantID: "grant-1",
			GrantToken: aws.String("secret"), GranteePrincipal: r.GranteePrincipal,
			Operations: []string{"Decrypt"}}, got)
		require.Equal(t, []time.Duration{500 * time.Millisecond}, *delays)
	}
	for _, tc := range []struct {
		name      string
		err       error
		wantCalls int
	}{
		{"fatal", errors.New("fatal"), 1},
		{"typed fatal", &types.InvalidGrantIdException{}, 1},
		{"exhausted", &types.KMSInternalException{}, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			clock, _ := grantTestClock()
			client := grantFake{create: func(context.Context, *kms.CreateGrantInput) (
				*kms.CreateGrantOutput, error,
			) {
				calls++
				return nil, tc.err
			}}
			_, err := (GrantResource{}).createGrant(t.Context(), client, clock)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.wantCalls, calls)
		})
	}
	for _, retire := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			for _, cleanupFails := range []bool{false, true} {
				t.Run(fmt.Sprint(retire, canceled, cleanupFails), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					original, cleanup := errors.New("read failed"), errors.New("cleanup failed")
					var calls []string
					cleanupCheck := func(ctx context.Context, key, id string) error {
						require.NoError(t, ctx.Err())
						deadline, ok := ctx.Deadline()
						require.True(t, ok)
						require.InDelta(t, grantPropagationTimeout.Seconds(), time.Until(deadline).Seconds(), 1)
						require.Equal(t, grantPrior().KeyID, key)
						require.Equal(t, "grant-1", id)
						if cleanupFails {
							return cleanup
						}
						return nil
					}
					client := grantFake{create: func(context.Context, *kms.CreateGrantInput) (
						*kms.CreateGrantOutput, error,
					) {
						calls = append(calls, "create")
						return &kms.CreateGrantOutput{
							GrantId: aws.String("grant-1")}, nil
					},
						list: func(ctx context.Context, _ *kms.ListGrantsInput) (*kms.ListGrantsOutput, error) {
							calls = append(calls, "list")
							if len(calls) == 2 {
								if canceled {
									cancel()
									return nil, context.Canceled
								}
								return nil, original
							}
							require.NoError(t, ctx.Err())
							return grantPage(), nil
						},
						revoke: func(ctx context.Context, in *kms.RevokeGrantInput) (*kms.RevokeGrantOutput, error) {
							calls = append(calls, "revoke")
							require.Equal(t, &kms.RevokeGrantInput{KeyId: aws.String(grantPrior().KeyID),
								GrantId: aws.String("grant-1")}, in)
							return nil, cleanupCheck(ctx, *in.KeyId, *in.GrantId)
						},
						retire: func(ctx context.Context, in *kms.RetireGrantInput) (*kms.RetireGrantOutput, error) {
							calls = append(calls, "retire")
							require.Equal(t, &kms.RetireGrantInput{KeyId: aws.String(grantPrior().KeyID),
								GrantId: aws.String("grant-1")}, in)
							return nil, cleanupCheck(ctx, *in.KeyId, *in.GrantId)
						}}
					clock, _ := grantTestClock()
					_, err := (GrantResource{KeyID: grantPrior().KeyID, RetireOnDelete: retire}).
						createGrant(ctx, client, clock)
					if canceled {
						require.ErrorIs(t, err, context.Canceled)
					} else {
						require.ErrorIs(t, err, original)
					}
					method := "revoke"
					if retire {
						method = "retire"
					}
					want := []string{"create", "list", method}
					if cleanupFails {
						require.ErrorIs(t, err, cleanup)
					} else {
						want = append(want, "list")
					}
					require.Equal(t, want, calls)
				})
			}
		}
	}
}

func TestGrantDeletePoliciesAndWait(t *testing.T) {
	for _, retire := range []bool{false, true} {
		for _, mode := range []string{"settled", "missing", "fatal", "read fatal", "exhausted"} {
			t.Run(fmt.Sprint(retire, mode), func(t *testing.T) {
				var calls []string
				prior := grantPrior()
				clock, delays := grantTestClock()
				mutationErr := func() error {
					switch mode {
					case "missing":
						return &types.NotFoundException{}
					case "fatal":
						return errors.New("fatal")
					}
					return nil
				}
				client := grantFake{revoke: func(_ context.Context, in *kms.RevokeGrantInput) (
					*kms.RevokeGrantOutput, error,
				) {
					calls = append(calls, "revoke")
					require.Equal(t, &kms.RevokeGrantInput{KeyId: aws.String(prior.KeyID),
						GrantId: aws.String(prior.GrantID)}, in)
					return nil, mutationErr()
				}, retire: func(_ context.Context, in *kms.RetireGrantInput) (*kms.RetireGrantOutput, error) {
					calls = append(calls, "retire")
					require.Equal(t, &kms.RetireGrantInput{KeyId: aws.String(prior.KeyID),
						GrantId: aws.String(prior.GrantID)}, in)
					return nil, mutationErr()
				}, list: func(context.Context, *kms.ListGrantsInput) (*kms.ListGrantsOutput, error) {
					calls = append(calls, "list")
					if mode == "read fatal" {
						return nil, errors.New("read fatal")
					}
					if len(calls) == 4 && mode == "settled" {
						return nil, &types.NotFoundException{}
					}
					return grantPage(""), nil
				}}
				err := deleteGrant(t.Context(), client, prior, retire, clock)
				method := "revoke"
				if retire {
					method = "retire"
				}
				want := []string{method}
				switch mode {
				case "missing":
					require.NoError(t, err)
				case "fatal":
					require.ErrorContains(t, err, "fatal")
				case "read fatal":
					require.ErrorContains(t, err, "read fatal")
					want = append(want, "list")
				case "exhausted":
					require.ErrorContains(t, err, "remains visible")
					for range 24 {
						want = append(want, "list")
					}
				default:
					require.NoError(t, err)
					want = append(want, "list", "list", "list")
					require.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, *delays)
				}
				require.Equal(t, want, calls)
			})
		}
	}
}
