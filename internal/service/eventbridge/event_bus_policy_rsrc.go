package eventbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
	awspolicy "github.com/hashicorp/awspolicyequivalence"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
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
		Equality: []runtime.InputEqualityRule[EventBusPolicyResource]{
			runtime.EqualBy(runtime.InputField(func(in *EventBusPolicyResource) *string {
				return &in.Policy
			}), eventBusPoliciesEqual),
		},
		Replace: runtime.Replacement[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[EventBusPolicyResource]{
				runtime.InputField(func(in *EventBusPolicyResource) *string { return &in.EventBusName }),
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
	return r.putPolicy(ctx, client, r.EventBusName)
}

func (r *EventBusPolicyResource) Read(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg],
) (*EventBusPolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.readPolicy(ctx, client, policyBusName(r.EventBusName, prior.Outputs))
}

func (r *EventBusPolicyResource) Update(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg],
) (*EventBusPolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.putPolicy(ctx, client, policyBusName(r.EventBusName, prior.Outputs))
}

func (r *EventBusPolicyResource) Delete(ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[EventBusPolicyResource, *EventBusPolicyResourceOutput, *awsCfg],
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return deleteBusPolicy(ctx, client, policyBusName(r.EventBusName, prior.Outputs))
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
	name string, opts ...wait.Option) (*EventBusPolicyResourceOutput, error) {
	in, err := r.preparePolicy(name)
	if err != nil {
		return nil, err
	}
	if _, err := client.PutPermission(ctx, in); err != nil {
		return nil, fmt.Errorf("put event bus policy: %w", err)
	}
	err = wait.Until(ctx, "event bus policy", func(ctx context.Context) (bool, error) {
		_, err := r.lookupPolicy(ctx, client, name)
		if errors.Is(err, runtime.ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("wait for event bus policy: %w", err)
	}
	return r.readPolicy(ctx, client, name)
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
	decoder.UseNumber()
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
