package sns

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sns "github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// SNS subscription attribute names. Each names a key in the attribute map that
// Subscribe accepts inline and SetSubscriptionAttributes reconciles one at a
// time. The protocol, endpoint, and topic ARN are dedicated Subscribe fields
// rather than attributes, so they have no entry here.
const (
	subAttrDeliveryPolicy     = "DeliveryPolicy"
	subAttrFilterPolicy       = "FilterPolicy"
	subAttrFilterPolicyScope  = "FilterPolicyScope"
	subAttrRawMessageDelivery = "RawMessageDelivery"
	subAttrRedrivePolicy      = "RedrivePolicy"
	subAttrReplayPolicy       = "ReplayPolicy"
	subAttrSubscriptionRole   = "SubscriptionRoleArn"
	subAttrPendingConfirm     = "PendingConfirmation"
	subAttrSubscriptionArn    = "SubscriptionArn"
	subAttrOwner              = "Owner"
)

// filterPolicyScopeMessageBody and filterPolicyScopeMessageAttributes are the
// two filter scopes. MessageBody must be set before any other attribute and
// MessageAttributes after, because a move from a body filter to an attributes
// filter is not backward compatible (edge 8).
const (
	filterPolicyScopeMessageBody       = "MessageBody"
	filterPolicyScopeMessageAttributes = "MessageAttributes"
)

// pendingConfirmationTrue is the value SNS reports in the PendingConfirmation
// attribute while a subscription still awaits confirmation. The attribute is a
// string, so the confirmation wait compares against this literal.
const pendingConfirmationTrue = "true"

// httpConfirmTimeoutDefault is the confirmation poll timeout for an http or
// https subscription when confirmation-timeout-in-minutes is omitted. Every
// other auto-confirming protocol confirms near-instantly and uses the wait
// default of two minutes.
const httpConfirmTimeoutDefault = time.Minute

// TopicSubscriptionResource manages a single subscription of an endpoint to an SNS
// topic. The protocol, endpoint, and topic ARN are fixed at subscribe time, so
// a change to any of them replaces the subscription; the policy and delivery
// attributes change in place. On create every set attribute rides the Subscribe
// call inline and an update reconciles each changed attribute with its own
// SetSubscriptionAttributes call.
//
// Some protocols confirm out of band: SNS returns the subscription as pending
// and the real ARN is not known until the endpoint owner confirms. The
// auto-confirming protocols (sqs, lambda, firehose, application, sms, and
// http/https when the endpoint confirms itself) settle near-instantly, so the
// resource waits for confirmation and reads back the settled ARN; email,
// email-json, and unconfirmed http/https skip the wait and keep the ARN
// Subscribe returns. endpoint-auto-confirms and confirmation-timeout-in-minutes
// only steer that wait and are not SNS attributes.
type TopicSubscriptionResource struct {
	Protocol                     string  `ub:"protocol"`
	TopicArn                     string  `ub:"topic-arn"`
	Endpoint                     *string `ub:"endpoint"`
	RawMessageDelivery           *bool   `ub:"raw-message-delivery"`
	FilterPolicy                 *string `ub:"filter-policy"`
	FilterPolicyScope            *string `ub:"filter-policy-scope"`
	RedrivePolicy                *string `ub:"redrive-policy"`
	DeliveryPolicy               *string `ub:"delivery-policy"`
	ReplayPolicy                 *string `ub:"replay-policy"`
	SubscriptionRoleArn          *string `ub:"subscription-role-arn"`
	EndpointAutoConfirms         *bool   `ub:"endpoint-auto-confirms"`
	ConfirmationTimeoutInMinutes *int64  `ub:"confirmation-timeout-in-minutes"`
}

// TopicSubscriptionResourceOutput holds the values SNS computes for a subscription. The
// ARN is the subscription's identity, used to read, update, and delete it, and
// is the settled real ARN once confirmation completes for an auto-confirming
// protocol. The owner is the topic owner's account id and pending-confirmation
// reports whether the subscription still awaits confirmation; both are
// read-only status.
type TopicSubscriptionResourceOutput struct {
	Arn                 string `ub:"arn"`
	Owner               string `ub:"owner"`
	PendingConfirmation bool   `ub:"pending-confirmation"`
	FilterPolicyScope   string `ub:"filter-policy-scope"`
}

func (r *TopicSubscriptionResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs SNS fixes when a subscription is created. The
// protocol, endpoint, and topic ARN identify the subscription and cannot be
// changed on an existing one, so a change to any of them requires a new
// subscription. Every other input is reconciled in place by Update.
func (r *TopicSubscriptionResource) ReplaceFields() []string {
	return []string{
		"protocol",
		"endpoint",
		"topic-arn",
	}
}

// Constraints declares the rules SNS places on a subscription's inputs. The
// protocol is one of a fixed set. A filter scope is one of two values and may
// only be set alongside a filter policy. A firehose subscription requires a
// subscription role ARN; the API rejects one without it.
func (r TopicSubscriptionResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.OneOf(r.Protocol,
			"application", "email", "email-json", "firehose",
			"http", "https", "lambda", "sms", "sqs")),
		constraint.When(constraint.Present(r.FilterPolicyScope)).
			Require(constraint.OneOf(r.FilterPolicyScope,
				"MessageAttributes", "MessageBody")).
			Message("filter-policy-scope must be MessageAttributes or MessageBody"),
		constraint.RequiredWith(r.FilterPolicyScope, r.FilterPolicy),
		constraint.When(constraint.Equals(r.Protocol, "firehose")).
			Require(constraint.Present(r.SubscriptionRoleArn)).
			Message("subscription-role-arn is required when protocol is firehose"),
	}
}

func (r *TopicSubscriptionResource) Create(
	ctx context.Context,
	cfg *awsCfg) (*TopicSubscriptionResourceOutput, error,
) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &sns.SubscribeInput{
		Protocol:              aws.String(r.Protocol),
		TopicArn:              aws.String(r.TopicArn),
		Endpoint:              r.Endpoint,
		Attributes:            r.attributes(),
		ReturnSubscriptionArn: true,
	}
	resp, err := client.Subscribe(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	arn := aws.ToString(resp.SubscriptionArn)
	// An auto-confirming protocol returns a pending ARN that settles to the real
	// one once SNS confirms the subscription, which happens near-instantly. Wait
	// for that flip and read the settled ARN, so the handle stored in state and
	// keyed off by Delete is never the pending placeholder. A protocol that
	// confirms out of band keeps the pending ARN: it is the canonical handle
	// until the endpoint owner confirms.
	if r.waitForConfirmation() {
		settled, err := r.waitConfirmed(ctx, client, arn)
		if err != nil {
			return nil, err
		}
		arn = settled
	}
	return r.read(ctx, client, arn, true)
}

func (r *TopicSubscriptionResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *TopicSubscriptionResourceOutput,
) (*TopicSubscriptionResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Arn, false)
}

// read fetches the subscription attributes by ARN and returns its computed
// outputs. When created is true the subscription was just made, so SNS may
// briefly report it absent while it propagates and read retries through that
// window; otherwise a not-found is drift and maps to runtime.ErrNotFound at
// once. An empty attribute map is treated as not-found, since SNS returns one
// for a subscription that no longer exists.
func (r *TopicSubscriptionResource) read(
	ctx context.Context, client *sns.Client, arn string, created bool,
) (*TopicSubscriptionResourceOutput, error) {
	// A just-created subscription can briefly read as absent or return an empty
	// attribute map while it propagates, so retry through that window on create.
	// A steady-state read does not retry: a not-found there is real drift.
	retryable := func(err error) bool {
		return created && (isNotFound(err, "NotFound") || errors.Is(err, runtime.ErrNotFound))
	}
	var attrs map[string]string
	err := retry.OnError(ctx, retryable, func(ctx context.Context) error {
		resp, err := client.GetSubscriptionAttributes(ctx,
			&sns.GetSubscriptionAttributesInput{SubscriptionArn: aws.String(arn)})
		if err != nil {
			return err
		}
		if len(resp.Attributes) == 0 {
			return runtime.ErrNotFound
		}
		attrs = resp.Attributes
		return nil
	})
	if err != nil {
		if isNotFound(err, "NotFound") || errors.Is(err, runtime.ErrNotFound) {
			return nil, runtime.ErrNotFound
		}
		return nil, err
	}
	// GetSubscriptionAttributes is eventually consistent and keeps returning stale
	// attributes for a while after the topic or subscription is deleted out of
	// band, so a confirmed subscription is cross-checked against the topic's
	// subscription list to detect that it is gone. Only the waiting protocols
	// store a real ARN in state, so the check is gated on those; the
	// non-waiting ones (email/email-json, unconfirmed http/https) and an unset
	// topic ARN are skipped.
	if r.waitForConfirmation() && r.TopicArn != "" {
		present, err := r.subscriptionPresent(ctx, client, arn)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, runtime.ErrNotFound
		}
	}
	// The ARN passed in is the canonical handle: the settled real ARN for an
	// auto-confirming protocol, or the usable pending ARN Subscribe returned for
	// a protocol that confirms out of band. A still-pending subscription reports
	// the "PendingConfirmation" placeholder in the SubscriptionArn attribute, so
	// the queried handle is used for the identity rather than that attribute.
	return &TopicSubscriptionResourceOutput{
		Arn:                 arn,
		Owner:               attrs[subAttrOwner],
		PendingConfirmation: attrs[subAttrPendingConfirm] == pendingConfirmationTrue,
		FilterPolicyScope:   attrs[subAttrFilterPolicyScope],
	}, nil
}

// subscriptionPresent reports whether a subscription with the given ARN is
// still listed under the topic. It pages through ListSubscriptionsByTopic
// following NextToken until the ARN is found or the list is exhausted. The
// ListSubscriptionsByTopic permission may be absent, in which case SNS returns
// an AuthorizationErrorException; that is swallowed and the subscription is
// reported present, so a missing permission skips the cross-check rather than
// failing the read.
func (r *TopicSubscriptionResource) subscriptionPresent(
	ctx context.Context, client *sns.Client, arn string,
) (bool, error) {
	var token *string
	for {
		resp, err := client.ListSubscriptionsByTopic(ctx,
			&sns.ListSubscriptionsByTopicInput{
				TopicArn:  aws.String(r.TopicArn),
				NextToken: token,
			})
		if err != nil {
			if subIsAuthorizationError(err) {
				return true, nil
			}
			return false, fmt.Errorf("list subscriptions by topic: %w", err)
		}
		for _, sub := range resp.Subscriptions {
			if aws.ToString(sub.SubscriptionArn) == arn {
				return true, nil
			}
		}
		if resp.NextToken == nil {
			return false, nil
		}
		token = resp.NextToken
	}
}

func (r *TopicSubscriptionResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[TopicSubscriptionResource, *TopicSubscriptionResourceOutput],
) (*TopicSubscriptionResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	if err := r.reconcileAttributes(ctx, client, arn, prior); err != nil {
		return nil, err
	}
	return r.read(ctx, client, arn, false)
}

func (r *TopicSubscriptionResource) Delete(
	ctx context.Context, cfg *awsCfg, prior *TopicSubscriptionResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.Unsubscribe(ctx, &sns.UnsubscribeInput{
		SubscriptionArn: aws.String(prior.Arn),
	})
	if err != nil {
		// A subscription still awaiting confirmation cannot be unsubscribed; SNS
		// rejects it. It is removed from state regardless and remains in AWS until
		// it confirms or expires, so that rejection counts as deleted here.
		if subIsPendingUnsubscribe(err) {
			return nil
		}
		// A subscription already gone, removed out of band or by an earlier run, is
		// deleted either way.
		if isNotFound(err, "NotFound") {
			return nil
		}
		return fmt.Errorf("unsubscribe: %w", err)
	}
	// Confirm the subscription is gone before reporting the delete done, so a
	// later plan does not read the still-present subscription and try to delete
	// it again.
	return r.waitDeleted(ctx, client, prior.Arn)
}

// attributes builds the attribute map that rides the Subscribe call on create.
// Every set attribute is included; an unset one is omitted so SNS applies its
// own default. The protocol, endpoint, and topic ARN are dedicated Subscribe
// fields and are not attributes.
func (r *TopicSubscriptionResource) attributes() map[string]string {
	attrs := map[string]string{}
	if r.RawMessageDelivery != nil {
		attrs[subAttrRawMessageDelivery] = subBoolString(*r.RawMessageDelivery)
	}
	if r.FilterPolicy != nil {
		attrs[subAttrFilterPolicy] = subFilterPolicyValue(*r.FilterPolicy)
	}
	if scope := r.effectiveFilterPolicyScope(); scope != nil {
		attrs[subAttrFilterPolicyScope] = *scope
	}
	if r.RedrivePolicy != nil && *r.RedrivePolicy != "" {
		attrs[subAttrRedrivePolicy] = *r.RedrivePolicy
	}
	if r.DeliveryPolicy != nil {
		attrs[subAttrDeliveryPolicy] = *r.DeliveryPolicy
	}
	if r.ReplayPolicy != nil {
		attrs[subAttrReplayPolicy] = *r.ReplayPolicy
	}
	if r.SubscriptionRoleArn != nil {
		attrs[subAttrSubscriptionRole] = *r.SubscriptionRoleArn
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

// reconcileAttributes sets each attribute that changed since the last apply,
// one SetSubscriptionAttributes call apiece. The filter scope is ordered around
// the rest: a move to MessageBody is applied first and a move to
// MessageAttributes last, because a body-to-attributes transition is not
// backward compatible. An attribute removed from the config is reconciled to
// its cleared form rather than left alone, since a changed input that is now
// absent means the user wants the default back.
func (r *TopicSubscriptionResource) reconcileAttributes(
	ctx context.Context, client *sns.Client, arn string,
	prior runtime.Prior[TopicSubscriptionResource, *TopicSubscriptionResourceOutput],
) error {
	// SNS reads an omitted scope as MessageAttributes whenever a filter policy is
	// present, so reconcile to that effective value rather than leave the prior
	// scope in place; with no policy there is no scope to set and it clears.
	scope := r.effectiveFilterPolicyScope()
	scopeChanged := runtime.Changed(prior.Inputs.FilterPolicyScope, r.FilterPolicyScope) ||
		runtime.Changed(prior.Inputs.FilterPolicy, r.FilterPolicy)
	scopeFirst := scopeChanged && scope != nil && *scope == filterPolicyScopeMessageBody
	scopeLast := scopeChanged && (scope == nil ||
		*scope == filterPolicyScopeMessageAttributes)
	if scopeFirst {
		if err := r.putAttribute(ctx, client, arn, subAttrFilterPolicyScope, scope); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.RawMessageDelivery, r.RawMessageDelivery) {
		if err := r.putAttribute(ctx, client, arn, subAttrRawMessageDelivery,
			subBoolStringPtr(r.RawMessageDelivery)); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.FilterPolicy, r.FilterPolicy) {
		if err := r.putAttribute(ctx, client, arn, subAttrFilterPolicy,
			subFilterPolicyValuePtr(r.FilterPolicy)); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.RedrivePolicy, r.RedrivePolicy) {
		if err := r.putAttribute(ctx, client, arn, subAttrRedrivePolicy,
			subRedrivePolicyValue(r.RedrivePolicy)); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.DeliveryPolicy, r.DeliveryPolicy) {
		if err := r.putAttribute(ctx, client, arn, subAttrDeliveryPolicy,
			r.DeliveryPolicy); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.ReplayPolicy, r.ReplayPolicy) {
		if err := r.putAttribute(ctx, client, arn, subAttrReplayPolicy,
			r.ReplayPolicy); err != nil {
			return err
		}
	}
	if runtime.Changed(prior.Inputs.SubscriptionRoleArn, r.SubscriptionRoleArn) {
		if err := r.putAttribute(ctx, client, arn, subAttrSubscriptionRole,
			r.SubscriptionRoleArn); err != nil {
			return err
		}
	}
	if scopeLast {
		if err := r.putAttribute(ctx, client, arn, subAttrFilterPolicyScope, scope); err != nil {
			return err
		}
	}
	return nil
}

// effectiveFilterPolicyScope returns the scope value Update should reconcile. An
// explicit scope is used as given. An omitted scope alongside a filter policy
// resolves to MessageAttributes, the value SNS applies by default, so a later
// read does not show drift. An omitted scope with no policy has nothing to set
// and returns nil, which clears the attribute.
func (r *TopicSubscriptionResource) effectiveFilterPolicyScope() *string {
	if r.FilterPolicyScope != nil {
		return r.FilterPolicyScope
	}
	if r.FilterPolicy != nil {
		return aws.String(filterPolicyScopeMessageAttributes)
	}
	return nil
}

// putAttribute sets a single subscription attribute. A nil value clears the
// attribute: SNS accepts the call with no value and resets the attribute to its
// default, whereas an empty-string value for some attributes is rejected as
// invalid.
func (r *TopicSubscriptionResource) putAttribute(
	ctx context.Context, client *sns.Client, arn, name string, value *string,
) error {
	_, err := client.SetSubscriptionAttributes(ctx, &sns.SetSubscriptionAttributesInput{
		SubscriptionArn: aws.String(arn),
		AttributeName:   aws.String(name),
		AttributeValue:  value,
	})
	if err != nil {
		return fmt.Errorf("set subscription attribute %s: %w", name, err)
	}
	return nil
}

// waitForConfirmation reports whether Create should wait for the subscription
// to confirm before reading the settled ARN. An http or https subscription
// confirms out of band unless the endpoint confirms itself, signalled by
// endpoint-auto-confirms; an email or email-json subscription always confirms
// out of band. Every other protocol confirms near-instantly, so the wait is
// cheap and yields the real ARN.
func (r *TopicSubscriptionResource) waitForConfirmation() bool {
	if strings.Contains(r.Protocol, "http") {
		return aws.ToBool(r.EndpointAutoConfirms)
	}
	if strings.Contains(r.Protocol, "email") {
		return false
	}
	return true
}

// waitConfirmed polls the subscription until PendingConfirmation flips from
// true to false, then returns the settled real ARN that the now-confirmed
// subscription reports. An http or https subscription uses
// confirmation-timeout-in-minutes (one minute by default) since it may take
// longer to confirm; every other protocol uses the wait default.
func (r *TopicSubscriptionResource) waitConfirmed(
	ctx context.Context, client *sns.Client, arn string,
) (string, error) {
	settled := arn
	opts := []wait.Option{}
	if strings.Contains(r.Protocol, "http") {
		opts = append(opts, wait.WithTimeout(r.confirmationTimeout()))
	}
	err := wait.Until(ctx, fmt.Sprintf("subscription %s confirmation",
		arn), func(ctx context.Context) (bool, error) {
		resp, err := client.GetSubscriptionAttributes(ctx,
			&sns.GetSubscriptionAttributesInput{SubscriptionArn: aws.String(arn)})
		if err != nil {
			// The just-created subscription may not have propagated yet; keep
			// polling rather than failing the wait.
			if isNotFound(err, "NotFound") {
				return false, nil
			}
			return false, fmt.Errorf("get subscription attributes: %w", err)
		}
		if resp.Attributes[subAttrPendingConfirm] == pendingConfirmationTrue {
			return false, nil
		}
		// Once confirmed, the attribute map reports the real ARN in place of the
		// pending placeholder; capture it as the settled handle.
		if a := resp.Attributes[subAttrSubscriptionArn]; a != "" {
			settled = a
		}
		return true, nil
	}, opts...)
	if err != nil {
		return "", err
	}
	return settled, nil
}

// waitDeleted polls until the subscription reports not-found, confirming the
// unsubscribe completed.
func (r *TopicSubscriptionResource) waitDeleted(
	ctx context.Context, client *sns.Client, arn string,
) error {
	return wait.Until(ctx, fmt.Sprintf("subscription %s deletion", arn),
		func(ctx context.Context) (bool, error) {
			resp, err := client.GetSubscriptionAttributes(ctx,
				&sns.GetSubscriptionAttributesInput{SubscriptionArn: aws.String(arn)})
			if err != nil {
				if isNotFound(err, "NotFound") {
					return true, nil
				}
				return false, fmt.Errorf("get subscription attributes: %w", err)
			}
			// SNS returns an empty attribute map for a subscription that is gone;
			// treat that as deleted too.
			return len(resp.Attributes) == 0, nil
		}, wait.WithInterval(time.Second))
}

// confirmationTimeout returns the http/https confirmation poll timeout,
// defaulting to one minute when confirmation-timeout-in-minutes is omitted.
func (r *TopicSubscriptionResource) confirmationTimeout() time.Duration {
	if r.ConfirmationTimeoutInMinutes == nil {
		return httpConfirmTimeoutDefault
	}
	return time.Duration(*r.ConfirmationTimeoutInMinutes) * time.Minute
}

// subFilterPolicyValue maps an empty filter policy to "{}", the empty JSON object,
// since SNS rejects an empty string for the attribute and reads "{}" as no
// filter.
func subFilterPolicyValue(policy string) string {
	if policy == "" {
		return "{}"
	}
	return policy
}

// subFilterPolicyValuePtr applies the empty-policy rule to a pointer input. A nil
// input clears the attribute; an empty string becomes "{}".
func subFilterPolicyValuePtr(policy *string) *string {
	if policy == nil {
		return nil
	}
	return aws.String(subFilterPolicyValue(*policy))
}

// subRedrivePolicyValue maps an empty or nil redrive policy to a nil attribute
// value, which clears it. An empty string sent as the value is rejected as
// invalid, so it must be sent as nil instead.
func subRedrivePolicyValue(policy *string) *string {
	if policy == nil || *policy == "" {
		return nil
	}
	return policy
}

// subBoolString renders a bool as the lowercase "true"/"false" string SNS expects
// for a boolean attribute value.
func subBoolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// subBoolStringPtr renders a *bool attribute value, returning nil to clear the
// attribute when the input is unset.
func subBoolStringPtr(b *bool) *string {
	if b == nil {
		return nil
	}
	return aws.String(subBoolString(*b))
}

// subIsPendingUnsubscribe reports whether err is the SNS rejection of an
// unsubscribe against a subscription still awaiting confirmation. SNS returns
// it as an InvalidParameter error whose message names the pending state, so the
// message is matched as well as the code.
func subIsPendingUnsubscribe(err error) bool {
	var invalid *snstypes.InvalidParameterException
	if errors.As(err, &invalid) {
		return strings.Contains(invalid.ErrorMessage(),
			"Cannot unsubscribe a subscription that is pending confirmation")
	}
	return false
}

// subIsAuthorizationError reports whether err is the SNS authorization failure
// raised when the caller lacks the ListSubscriptionsByTopic permission. SNS
// returns it as an AuthorizationErrorException whose message names the denied
// action, so the message is matched as well as the type.
func subIsAuthorizationError(err error) bool {
	var authErr *snstypes.AuthorizationErrorException
	if errors.As(err, &authErr) {
		return strings.Contains(authErr.ErrorMessage(), "not authorized to perform")
	}
	return false
}
