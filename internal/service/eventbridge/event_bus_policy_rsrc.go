package eventbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
	awspolicy "github.com/hashicorp/awspolicyequivalence"
)

type EventBusPolicyResource struct {
	EventBusName string `ub:"event-bus-name"`
	Policy       string `ub:"policy"`
}

type EventBusPolicyResourceOutput struct {
	Name   string `ub:"name"`
	Policy string `ub:"policy"`
}

type eventBusPolicyClient interface {
	PutPermission(context.Context, *eventbridge.PutPermissionInput,
		...func(*eventbridge.Options)) (*eventbridge.PutPermissionOutput, error)
	DescribeEventBus(context.Context, *eventbridge.DescribeEventBusInput,
		...func(*eventbridge.Options)) (*eventbridge.DescribeEventBusOutput, error)
	RemovePermission(context.Context, *eventbridge.RemovePermissionInput,
		...func(*eventbridge.Options)) (*eventbridge.RemovePermissionOutput, error)
}

func (r EventBusPolicyResource) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Value(r.EventBusName, "default")}
}

func (r EventBusPolicyResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEquals(r.Policy, nil)).Message("policy is required"),
		constraint.Must(constraint.IsMatch(r.EventBusName, `^[/._A-Za-z0-9\-]{1,256}$`)).
			Message("event-bus-name must be 1-256 ASCII letters, digits, slash, dot, underscore or hyphen"),
		constraint.Must(constraint.IsFormat(r.Policy, constraint.FormatJSON)).
			Message("policy must be valid JSON"),
	}
}

func (r *EventBusPolicyResource) ResourceDefinition() runtime.ResourceDefinition[
	EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg,
] {
	return runtime.ResourceDefinition[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg]{
		SchemaVersion: 1,
		Validate: func(_ context.Context, in EventBusPolicyResource, _ *awsCfg) error {
			_, err := in.preparePolicy(in.EventBusName)
			return err
		},
		Equality: []runtime.InputEqualityRule[EventBusPolicyResource]{
			runtime.EqualBy(runtime.InputField(func(in *EventBusPolicyResource) *string {
				return &in.Policy
			}), eventBusPoliciesEqual),
		},
		Replace: runtime.Replacement[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[EventBusPolicyResource]{
				runtime.InputField(func(in *EventBusPolicyResource) *string { return &in.EventBusName }),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
	}
}

func (r *EventBusPolicyResource) Create(ctx context.Context,
	cfg *awsCfg) (*EventBusPolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createPolicy(ctx, client, r.EventBusName)
}

func (r *EventBusPolicyResource) Read(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg],
) (*EventBusPolicyResourceOutput, error) {
	client, err := newPolicyPriorClient(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	return r.readPolicy(ctx, client, policyBusName(r.EventBusName, prior.Outputs))
}

func (r *EventBusPolicyResource) Update(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg],
) (*EventBusPolicyResourceOutput, error) {
	client, err := newPolicyPriorClient(ctx, cfg, prior)
	if err != nil {
		return nil, err
	}
	return r.putPolicy(ctx, client, policyBusName(r.EventBusName, prior.Outputs))
}

func (r *EventBusPolicyResource) Delete(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg],
) error {
	client, err := newPolicyPriorClient(ctx, cfg, prior)
	if err != nil {
		return err
	}
	return deleteBusPolicy(ctx, client, policyBusName(r.EventBusName, prior.Outputs))
}

func newPolicyPriorClient(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg],
) (*eventbridge.Client, error) {
	if prior.Configuration != nil {
		cfg = prior.Configuration
	}
	return newClient(ctx, cfg)
}

func policyBusName(name string, prior *EventBusPolicyResourceOutput) string {
	if prior != nil && prior.Name != "" {
		return prior.Name
	}
	return name
}

func (r *EventBusPolicyResource) preparePolicy(
	name string,
) (*eventbridge.PutPermissionInput, error) {
	policy, err := normalizeEventBusPolicy(r.Policy)
	if err != nil {
		return nil, fmt.Errorf("prepare event bus policy: %w", err)
	}
	return &eventbridge.PutPermissionInput{
		EventBusName: aws.String(name), Policy: aws.String(policy),
	}, nil
}

func (r *EventBusPolicyResource) putPolicy(ctx context.Context, client eventBusPolicyClient,
	name string, opts ...policyRetry) (*EventBusPolicyResourceOutput, error) {
	return r.writePolicy(ctx, client, name, false, opts...)
}

func (r *EventBusPolicyResource) writePolicy(ctx context.Context, client eventBusPolicyClient,
	name string, cleanup bool, opts ...policyRetry) (*EventBusPolicyResourceOutput, error) {
	in, err := r.preparePolicy(name)
	if err != nil {
		return nil, err
	}
	if _, err := client.PutPermission(ctx, in); err != nil {
		return nil, fmt.Errorf("put event bus policy: %w", err)
	}
	out, err := r.awaitPolicy(ctx, client, name, opts...)
	if err == nil || !cleanup {
		return out, err
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if cleanupErr := deleteBusPolicy(cleanupCtx, client, name); cleanupErr != nil {
		err = errors.Join(err, fmt.Errorf("cleanup event bus policy %s: %w", name, cleanupErr))
	}
	return nil, err
}

func (r *EventBusPolicyResource) awaitPolicy(ctx context.Context, client eventBusPolicyClient,
	name string, opts ...policyRetry) (*EventBusPolicyResourceOutput, error) {
	err := retryPolicyPresence(ctx, func(ctx context.Context) error {
		_, err := r.lookupPolicy(ctx, client, name)
		return err
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("wait for event bus policy: %w", err)
	}
	return r.readPolicy(ctx, client, name)
}

func (r *EventBusPolicyResource) createPolicy(ctx context.Context, client eventBusPolicyClient,
	name string, opts ...policyRetry) (*EventBusPolicyResourceOutput, error) {
	return r.writePolicy(ctx, client, name, true, opts...)
}

type policyRetry struct {
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
	timeout time.Duration
}

func retryPolicyPresence(ctx context.Context, poll func(context.Context) error,
	opts ...policyRetry) error {
	cfg := policyRetry{now: time.Now, sleep: sleepPolicyRetry, timeout: 2 * time.Minute}
	if len(opts) > 0 {
		cfg = opts[0]
	}
	deadline := cfg.now().Add(cfg.timeout)
	delay := 500 * time.Millisecond
	grace := false
	var last error
	for first := true; ; first = false {
		if err := ctx.Err(); err != nil {
			return errors.Join(last, err)
		}
		if !first {
			if !cfg.now().Before(deadline) {
				if grace {
					return last
				}
				grace = true
			}
			if err := cfg.sleep(ctx, delay); err != nil {
				return errors.Join(last, err)
			}
			delay = min(delay*2, 10*time.Second)
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(last, err)
		}
		last = poll(ctx)
		if !errors.Is(last, runtime.ErrNotFound) {
			return last
		}
	}
}

func sleepPolicyRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *EventBusPolicyResource) lookupPolicy(ctx context.Context, client eventBusPolicyClient,
	name string) (string, error) {
	resp, err := client.DescribeEventBus(ctx, &eventbridge.DescribeEventBusInput{
		Name: aws.String(name),
	})
	if err != nil {
		if isNotFound(err) {
			return "", runtime.ErrNotFound
		}
		return "", fmt.Errorf("describe event bus policy: %w", err)
	}
	if resp == nil || aws.ToString(resp.Policy) == "" {
		return "", runtime.ErrNotFound
	}
	return *resp.Policy, nil
}

func (r *EventBusPolicyResource) readPolicy(ctx context.Context, client eventBusPolicyClient,
	name string) (*EventBusPolicyResourceOutput, error) {
	policy, err := r.lookupPolicy(ctx, client, name)
	if err != nil {
		return nil, err
	}
	policy, err = selectEventBusPolicy(r.Policy, policy)
	if err != nil {
		return nil, fmt.Errorf("read event bus policy: %w", err)
	}
	return &EventBusPolicyResourceOutput{Name: name, Policy: policy}, nil
}

func deleteBusPolicy(ctx context.Context, client eventBusPolicyClient, name string) error {
	_, err := client.RemovePermission(ctx, &eventbridge.RemovePermissionInput{
		EventBusName: aws.String(name), RemoveAllPermissions: true,
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("remove event bus policy: %w", err)
	}
	return nil
}

func eventBusPoliciesEqual(prior, desired string) bool {
	equal, err := awspolicy.PoliciesAreEquivalent(prior, desired)
	return err == nil && equal
}

func selectEventBusPolicy(prior, remote string) (string, error) {
	prior = strings.TrimSpace(prior)
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", nil
	}
	if prior == "" || prior == "{}" || remote == "{}" {
		return normalizeEventBusPolicy(remote)
	}
	equivalent, err := awspolicy.PoliciesAreEquivalent(prior, remote)
	if err != nil {
		if strings.Contains(err.Error(), "policy 1:") {
			return normalizeEventBusPolicy(remote)
		}
		return "", fmt.Errorf("compare event bus policies: %w", err)
	}
	if equivalent {
		return normalizeEventBusPolicy(prior)
	}
	return normalizeEventBusPolicy(remote)
}

func normalizeEventBusPolicy(document string) (string, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(document))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("multiple JSON values")
		}
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
