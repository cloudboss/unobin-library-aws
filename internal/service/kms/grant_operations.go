package kms

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const grantPropagationTimeout = 3 * time.Minute

type grantClient interface {
	CreateGrant(context.Context, *kms.CreateGrantInput, ...func(*kms.Options)) (
		*kms.CreateGrantOutput, error,
	)
	ListGrants(context.Context, *kms.ListGrantsInput, ...func(*kms.Options)) (
		*kms.ListGrantsOutput, error,
	)
	RevokeGrant(context.Context, *kms.RevokeGrantInput, ...func(*kms.Options)) (
		*kms.RevokeGrantOutput, error,
	)
	RetireGrant(context.Context, *kms.RetireGrantInput, ...func(*kms.Options)) (
		*kms.RetireGrantOutput, error,
	)
}

type grantClock struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func realGrantClock() grantClock {
	return grantClock{now: time.Now, sleep: func(ctx context.Context, delay time.Duration) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}}
}

func grantRetry(
	ctx context.Context, clock grantClock, retryable func(error) bool,
	call func(context.Context) error,
) error {
	deadline := clock.now().Add(grantPropagationTimeout)
	delay := 500 * time.Millisecond
	final := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := call(ctx)
		if err == nil || !retryable(err) || final {
			return err
		}
		if !clock.now().Before(deadline) {
			final = true
		}
		if err := clock.sleep(ctx, delay); err != nil {
			return err
		}
		delay = min(2*delay, 10*time.Second)
	}
}

var (
	grantServicePrincipal = regexp.MustCompile(`^([0-9a-z-]+\.){1,4}(amazonaws|amazon)\.com$`)
	grantPartition        = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	grantRegion           = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	grantAccount          = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|\d{12}|cw.{10})$`,
	)
)

func (r GrantResource) prepareGrant() (*kms.CreateGrantInput, error) {
	for _, principal := range []string{r.GranteePrincipal, aws.ToString(r.RetiringPrincipal)} {
		if principal == "" || grantServicePrincipal.MatchString(principal) {
			continue
		}
		parsed, err := arn.Parse(principal)
		if err != nil || !grantPartition.MatchString(parsed.Partition) || parsed.Resource == "" ||
			(parsed.Region != "" && !grantRegion.MatchString(parsed.Region)) ||
			(parsed.AccountID != "" && !grantAccount.MatchString(parsed.AccountID)) {
			return nil, fmt.Errorf("invalid grant principal %q", principal)
		}
	}
	input := &kms.CreateGrantInput{
		KeyId: aws.String(r.KeyID), GranteePrincipal: aws.String(r.GranteePrincipal),
		Name: r.Name,
	}
	for _, operation := range grantSet(r.Operations) {
		input.Operations = append(input.Operations, types.GrantOperation(operation))
	}
	if r.GrantTokens != nil && len(*r.GrantTokens) > 0 {
		input.GrantTokens = grantSet(*r.GrantTokens)
	}
	if aws.ToString(r.RetiringPrincipal) != "" {
		input.RetiringPrincipal = r.RetiringPrincipal
	}
	if r.GrantConstraints != nil {
		input.Constraints = &types.GrantConstraints{
			EncryptionContextEquals: maps.Clone(grantContextMap(r.GrantConstraints.EncryptionContextEquals)),
			EncryptionContextSubset: maps.Clone(grantContextMap(r.GrantConstraints.EncryptionContextSubset)),
		}
	}
	return input, nil
}

func grantSet(values []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(values)))
}

func grantCreateRetryable(err error) bool {
	var dependency *types.DependencyTimeoutException
	var internal *types.KMSInternalException
	var invalidARN *types.InvalidArnException
	return errors.As(err, &dependency) || errors.As(err, &internal) || errors.As(err, &invalidARN)
}

func (r GrantResource) createGrant(
	ctx context.Context, client grantClient, clock grantClock,
) (*GrantResourceOutput, error) {
	input, err := r.prepareGrant()
	if err != nil {
		return nil, err
	}
	var response *kms.CreateGrantOutput
	err = grantRetry(ctx, clock, grantCreateRetryable, func(ctx context.Context) error {
		var callErr error
		response, callErr = client.CreateGrant(ctx, input)
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("create grant: %w", err)
	}
	if response == nil || aws.ToString(response.GrantId) == "" {
		return nil, errors.New("create grant: response has no grant ID")
	}
	prior := &GrantResourceOutput{
		KeyID: r.KeyID, GrantID: aws.ToString(response.GrantId), GrantToken: response.GrantToken,
		Name: r.Name, GranteePrincipal: r.GranteePrincipal, RetiringPrincipal: r.RetiringPrincipal,
		Operations: grantSet(r.Operations), GrantConstraints: r.GrantConstraints,
	}
	out, err := readGrant(ctx, client, prior, true, clock)
	if err == nil {
		return out, nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grantPropagationTimeout)
	defer cancel()
	cleanupErr := deleteGrant(cleanupCtx, client, prior, r.RetireOnDelete, clock)
	if cleanupErr != nil {
		err = errors.Join(err, fmt.Errorf("clean up grant after failed create: %w", cleanupErr))
	}
	return nil, err
}

func findGrant(
	ctx context.Context, client grantClient, prior *GrantResourceOutput,
) (*types.GrantListEntry, error) {
	if prior == nil || prior.KeyID == "" || prior.GrantID == "" {
		return nil, errors.New("grant: missing persisted key or grant ID")
	}
	input := &kms.ListGrantsInput{KeyId: aws.String(prior.KeyID), Limit: aws.Int32(100)}
	var found *types.GrantListEntry
	for {
		page, err := client.ListGrants(ctx, input)
		if err != nil {
			if isNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("list grants: %w", err)
		}
		if page == nil {
			return nil, errors.New("list grants: empty response")
		}
		for _, entry := range page.Grants {
			if aws.ToString(entry.GrantId) != prior.GrantID {
				continue
			}
			if found != nil {
				return nil, errors.New("list grants: duplicate grant ID")
			}
			found = &entry
		}
		if !page.Truncated {
			break
		}
		input.Marker = page.NextMarker
	}
	if found == nil {
		return nil, runtime.ErrNotFound
	}
	return found, nil
}

func grantPrincipalSettled(value *string) bool {
	principal := aws.ToString(value)
	return principal == "" || arn.IsARN(principal) || grantServicePrincipal.MatchString(principal)
}

func readGrant(
	ctx context.Context, client grantClient, prior *GrantResourceOutput,
	created bool, clock grantClock,
) (*GrantResourceOutput, error) {
	var entry *types.GrantListEntry
	err := grantRetry(ctx, clock, func(err error) bool {
		return errors.Is(err, runtime.ErrNotFound) || errors.Is(err, errGrantPrincipalUnsettled)
	}, func(ctx context.Context) error {
		var err error
		entry, err = findGrant(ctx, client, prior)
		if err != nil {
			return err
		}
		if !grantPrincipalSettled(entry.GranteePrincipal) ||
			!grantPrincipalSettled(entry.RetiringPrincipal) {
			return errGrantPrincipalUnsettled
		}
		return nil
	})
	if err != nil {
		if created && errors.Is(err, runtime.ErrNotFound) {
			return nil, fmt.Errorf("created grant did not become visible")
		}
		return nil, err
	}
	out := *prior
	out.GrantID = aws.ToString(entry.GrantId)
	out.Operations = make([]string, 0, len(entry.Operations))
	for _, operation := range entry.Operations {
		out.Operations = append(out.Operations, string(operation))
	}
	out.Operations = grantSet(out.Operations)
	if entry.GranteePrincipal != nil {
		out.GranteePrincipal = *entry.GranteePrincipal
	}
	if entry.RetiringPrincipal != nil {
		out.RetiringPrincipal = entry.RetiringPrincipal
	}
	if aws.ToString(entry.Name) != "" {
		out.Name = entry.Name
	}
	if entry.Constraints != nil {
		out.GrantConstraints = &GrantConstraints{
			EncryptionContextEquals: grantContextPointer(entry.Constraints.EncryptionContextEquals),
			EncryptionContextSubset: grantContextPointer(entry.Constraints.EncryptionContextSubset),
		}
	}
	return &out, nil
}

var errGrantPrincipalUnsettled = errors.New("grant principals did not settle")

func deleteGrant(
	ctx context.Context, client grantClient, prior *GrantResourceOutput, retire bool, clock grantClock,
) error {
	if prior == nil || prior.KeyID == "" || prior.GrantID == "" {
		return errors.New("delete grant: missing persisted key or grant ID")
	}
	var err error
	if retire {
		_, err = client.RetireGrant(ctx, &kms.RetireGrantInput{
			KeyId: aws.String(prior.KeyID), GrantId: aws.String(prior.GrantID),
		})
	} else {
		_, err = client.RevokeGrant(ctx, &kms.RevokeGrantInput{
			KeyId: aws.String(prior.KeyID), GrantId: aws.String(prior.GrantID),
		})
	}
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete grant: %w", err)
	}
	visible := errors.New("grant remains visible after deletion")
	return grantRetry(ctx, clock, func(err error) bool { return errors.Is(err, visible) },
		func(ctx context.Context) error {
			_, err := findGrant(ctx, client, prior)
			if errors.Is(err, runtime.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			return visible
		})
}

func grantContextMap(value *map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	return *value
}

func grantContextPointer(value map[string]string) *map[string]string {
	if value == nil {
		return nil
	}
	copied := maps.Clone(value)
	return &copied
}
