package elbv2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/partition"
	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// listenerWriteTimeout bounds the create and update retries that wait out a
// certificate the ELBv2 control plane has not yet made visible. An HTTPS or TLS
// listener can reference an ACM certificate created moments earlier, which
// ELBv2 rejects with CertificateNotFound until it propagates.
const listenerWriteTimeout = 5 * time.Minute

// Listener is an ELBv2 listener: the port and protocol a load balancer accepts
// connections on, and the default action it takes for traffic that matches no
// rule. The fields mirror CreateListener, which is also the call an update
// makes through ModifyListener. The load balancer the listener belongs to is
// fixed at creation, so a change to it replaces the listener; the port,
// protocol, security policy, default certificate, ALPN policy, default actions,
// and tags all change in place.
//
// CertificateArn is the listener's default certificate, set on the create and
// modify call itself through a one-element Certificates list. It is distinct
// from the SNI certificates an HTTPS or TLS listener offers beyond its default,
// which are the separate elbv2-listener-certificate resource.
//
// The cross-field rules on protocol and the per-action rules are declared as
// constraints; Create and Update check only the residue a constraint cannot
// express (the fixed-response status pattern, the forward arn-match, and an
// explicitly empty action list).
type Listener struct {
	LoadBalancerArn string                  `ub:"load-balancer-arn"`
	Port            *int64                  `ub:"port"`
	Protocol        *string                 `ub:"protocol"`
	SslPolicy       *string                 `ub:"ssl-policy"`
	CertificateArn  *string                 `ub:"certificate-arn"`
	AlpnPolicy      *string                 `ub:"alpn-policy"`
	DefaultAction   []ListenerDefaultAction `ub:"default-action"`
	Tags            map[string]string       `ub:"tags"`
}

// ListenerOutput holds the values ELBv2 computes for a listener. Arn is the
// listener's stable handle and CloudFormation primary identifier. Protocol is
// the canonical protocol ELBv2 reports, which an Application Load Balancer may
// default from the presence of a certificate. SslPolicy is the security policy
// ELBv2 settled on, which it picks a default for on an HTTPS or TLS listener.
type ListenerOutput struct {
	Arn       string `ub:"arn"`
	Protocol  string `ub:"protocol"`
	SslPolicy string `ub:"ssl-policy"`
}

func (r *Listener) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs ELBv2 fixes when a listener is created. A
// listener belongs to one load balancer for its lifetime, so changing the load
// balancer requires a new listener. Every other input is reconciled in place by
// ModifyListener.
func (r *Listener) ReplaceFields() []string {
	return []string{"load-balancer-arn"}
}

// Defaults marks the collection inputs a listener may omit.
func (r Listener) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

// Constraints declares the cross-field rules on a listener's protocol and the
// per-action rules of the default-action list. An HTTPS or TLS listener
// terminates TLS, so it needs a security policy and a default certificate; the
// other protocols do not terminate TLS, so they reject a security policy,
// certificate, or ALPN policy, and ALPN applies only to a TLS listener. Each
// default action's type fixes which sub-block it takes, a redirect and a
// fixed-response have their enums, and an enabled forward stickiness needs a
// bounded duration. A forward block names one to five target groups, each
// weighted 0..999, and a forward that also sets target-group-arn names exactly
// the one group matching it. Only the fixed-response status pattern stays in
// code, since a constraint cannot take a pattern.
func (r Listener) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.OneOf(r.Protocol, "HTTPS", "TLS")).
			Require(constraint.Present(r.SslPolicy), constraint.Present(r.CertificateArn)).
			Message("an HTTPS or TLS listener requires ssl-policy and certificate-arn"),
		constraint.When(constraint.OneOf(r.Protocol,
			"HTTP", "TCP", "UDP", "TCP_UDP", "GENEVE", "QUIC", "TCP_QUIC")).
			Require(constraint.Absent(r.SslPolicy), constraint.Absent(r.CertificateArn),
				constraint.Absent(r.AlpnPolicy)).
			Message("only an HTTPS or TLS listener accepts ssl-policy, certificate-arn, or alpn-policy"),
		constraint.When(constraint.Present(r.AlpnPolicy)).
			Require(constraint.OneOf(r.AlpnPolicy,
				"HTTP1Only", "HTTP2Only", "HTTP2Optional", "HTTP2Preferred", "None")).
			Message("alpn-policy must be HTTP1Only, HTTP2Only, HTTP2Optional, HTTP2Preferred, or None"),
		constraint.Must(constraint.NotEmpty(r.DefaultAction)).
			Message("default-action must list at least one action"),
		constraint.ForEach(r.DefaultAction,
			func(a ListenerDefaultAction) []constraint.Constraint {
				return []constraint.Constraint{
					constraint.Must(constraint.OneOf(a.Type,
						"forward", "redirect", "fixed-response")).
						Message("an action type must be forward, redirect, or fixed-response"),
					constraint.When(constraint.Equals(a.Type, "forward")).
						Require(constraint.Any(constraint.Present(a.TargetGroupArn),
							constraint.Present(a.Forward)),
							constraint.Absent(a.Redirect), constraint.Absent(a.FixedResponse)).
						Message("a forward action takes target-group-arn or a forward block only"),
					constraint.When(constraint.Equals(a.Type, "redirect")).
						Require(constraint.Present(a.Redirect),
							constraint.Absent(a.TargetGroupArn), constraint.Absent(a.Forward),
							constraint.Absent(a.FixedResponse)).
						Message("a redirect action takes a redirect block only"),
					constraint.When(constraint.Equals(a.Type, "fixed-response")).
						Require(constraint.Present(a.FixedResponse),
							constraint.Absent(a.TargetGroupArn), constraint.Absent(a.Forward),
							constraint.Absent(a.Redirect)).
						Message("a fixed-response action takes a fixed-response block only"),
					constraint.When(constraint.Present(a.Redirect.StatusCode)).
						Require(constraint.OneOf(a.Redirect.StatusCode,
							"HTTP_301", "HTTP_302")).
						Message("a redirect status-code must be HTTP_301 or HTTP_302"),
					constraint.When(constraint.Present(a.Redirect.Protocol)).
						Require(constraint.OneOf(a.Redirect.Protocol,
							"#{protocol}", "HTTP", "HTTPS")).
						Message("a redirect protocol must be HTTP, HTTPS, or #{protocol}"),
					constraint.When(constraint.Present(a.FixedResponse.ContentType)).
						Require(constraint.OneOf(a.FixedResponse.ContentType, "text/plain",
							"text/css", "text/html", "application/javascript",
							"application/json")).
						Message("a fixed-response content-type must be one of the accepted types"),
					constraint.When(constraint.Present(a.Forward)).
						Require(constraint.NotEmpty(a.Forward.TargetGroups),
							constraint.MaxItems(a.Forward.TargetGroups, 5)).
						Message("a forward block takes one to five target-groups"),
					constraint.When(constraint.All(constraint.Present(a.TargetGroupArn),
						constraint.Present(a.Forward))).
						Require(constraint.MaxItems(a.Forward.TargetGroups, 1)).
						Message("with target-group-arn set, the forward block " +
							"must name exactly one target group"),
					constraint.ForEach(a.Forward.TargetGroups,
						func(g ListenerForwardTargetGroup) []constraint.Constraint {
							return []constraint.Constraint{
								constraint.When(constraint.Present(g.Weight)).
									Require(constraint.AtLeast(g.Weight, 0),
										constraint.AtMost(g.Weight, 999)).
									Message("a target group weight must be between 0 and 999"),
								constraint.When(constraint.Present(a.TargetGroupArn)).
									Require(constraint.Equals(g.Arn, a.TargetGroupArn)).
									Message("target-group-arn must match the forward " +
										"block's target group"),
							}
						}),
					constraint.When(constraint.IsTrue(a.Forward.Stickiness.Enabled)).
						Require(constraint.Present(a.Forward.Stickiness.DurationSeconds)).
						Message("enabled forward stickiness requires duration-seconds"),
					constraint.When(constraint.Present(a.Forward.Stickiness.DurationSeconds)).
						Require(constraint.AtLeast(a.Forward.Stickiness.DurationSeconds, 1),
							constraint.AtMost(a.Forward.Stickiness.DurationSeconds, 604800)).
						Message("stickiness duration-seconds must be between 1 and 604800"),
				}
			}),
	}
}

func (r *Listener) Create(ctx context.Context, cfg any) (*ListenerOutput, error) {
	if err := validateDefaultActions(r.DefaultAction); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := r.createInput()
	// Some partitions, such as the ISO partitions, and some load balancer types,
	// such as Gateway, cannot tag a listener on the create call itself. When the
	// tagged create fails for either reason, create the listener without tags and
	// apply them with a separate call below.
	taggedSeparately := false
	resp, err := r.createListener(ctx, client, in)
	if err != nil && in.Tags != nil &&
		(partition.UnsupportedOperation(region(client), err) || isTagOnCreateUnsupported(err)) {
		in.Tags = nil
		taggedSeparately = true
		resp, err = r.createListener(ctx, client, in)
	}
	if err != nil {
		return nil, fmt.Errorf("create listener: %w", err)
	}
	if len(resp.Listeners) == 0 {
		return nil, fmt.Errorf("create listener: response held no listener")
	}
	arn := aws.ToString(resp.Listeners[0].ListenerArn)
	// CreateListener returns before the listener is consistently visible, so wait
	// for a describe to find it at its own ARN before reading its settled values.
	if err := r.waitVisible(ctx, client, arn); err != nil {
		return nil, err
	}
	if taggedSeparately && len(r.Tags) > 0 {
		if err := syncTags(ctx, client, arn, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, arn)
}

func (r *Listener) Read(
	ctx context.Context, cfg any, prior *ListenerOutput,
) (*ListenerOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.Arn)
}

func (r *Listener) Update(
	ctx context.Context, cfg any, prior runtime.Prior[Listener, *ListenerOutput],
) (*ListenerOutput, error) {
	if err := validateDefaultActions(r.DefaultAction); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	// ModifyListener rewrites the listener's body apart from its tags, so it runs
	// only when one of those fields changed. Replaying it on a tag-only change
	// would reset a field the config omits, so a tag change is left to the tag
	// reconcile below.
	if r.modifyChanged(prior.Inputs) {
		if err := r.modifyListener(ctx, client, arn); err != nil {
			return nil, err
		}
	}
	// ModifyListener does not touch an existing listener's tags, so reconcile them
	// through the tag API as a set whenever they changed.
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, arn, r.Tags); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, arn)
}

func (r *Listener) Delete(ctx context.Context, cfg any, prior *ListenerOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = client.DeleteListener(ctx, &elbv2.DeleteListenerInput{
		ListenerArn: aws.String(prior.Arn),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete listener: %w", err)
	}
	return nil
}

// read fetches the listener at arn and returns its computed output. A listener
// that has gone missing is drift: ELBv2 reports it as a ListenerNotFound
// exception, an empty result, or a describe that returns a different ARN while
// the create is still settling, and read turns each into runtime.ErrNotFound so
// the runtime recreates it.
func (r *Listener) read(
	ctx context.Context, client *elbv2.Client, arn string,
) (*ListenerOutput, error) {
	listener, err := r.find(ctx, client, arn)
	if err != nil {
		return nil, err
	}
	return &ListenerOutput{
		Arn:       aws.ToString(listener.ListenerArn),
		Protocol:  string(listener.Protocol),
		SslPolicy: aws.ToString(listener.SslPolicy),
	}, nil
}

// find describes the listener at arn and asserts it resolves to exactly that
// listener. ELBv2 reports a gone listener as a typed not-found exception or an
// empty result, and right after a create a describe can briefly return a
// different listener, so each of those reads as not-found.
func (r *Listener) find(
	ctx context.Context, client *elbv2.Client, arn string,
) (*elbv2types.Listener, error) {
	resp, err := client.DescribeListeners(ctx, &elbv2.DescribeListenersInput{
		ListenerArns: []string{arn},
	})
	if err != nil {
		if isNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe listeners: %w", err)
	}
	if len(resp.Listeners) == 0 {
		return nil, runtime.ErrNotFound
	}
	listener := resp.Listeners[0]
	if aws.ToString(listener.ListenerArn) != arn {
		return nil, runtime.ErrNotFound
	}
	return &listener, nil
}

// createInput builds the CreateListener request from the listener's inputs. The
// default certificate rides as a one-element Certificates list, the ALPN policy
// as a one-element list, and protocol is sent only when set so an Application
// Load Balancer can default it from the certificate.
func (r *Listener) createInput() *elbv2.CreateListenerInput {
	in := &elbv2.CreateListenerInput{
		LoadBalancerArn: aws.String(r.LoadBalancerArn),
		Port:            ptr.Int32(r.Port),
		SslPolicy:       r.SslPolicy,
		DefaultActions:  defaultActions(r.DefaultAction),
		Tags:            tagList(r.Tags),
	}
	if r.Protocol != nil {
		in.Protocol = elbv2types.ProtocolEnum(*r.Protocol)
	}
	if r.CertificateArn != nil {
		in.Certificates = []elbv2types.Certificate{{CertificateArn: r.CertificateArn}}
	}
	if r.AlpnPolicy != nil {
		in.AlpnPolicy = []string{*r.AlpnPolicy}
	}
	return in
}

// modifyInput builds the ModifyListener request from the listener's inputs. It
// mirrors createInput: the default certificate and ALPN policy ride as
// one-element lists, and protocol is sent only when set.
func (r *Listener) modifyInput(arn string) *elbv2.ModifyListenerInput {
	in := &elbv2.ModifyListenerInput{
		ListenerArn:    aws.String(arn),
		Port:           ptr.Int32(r.Port),
		SslPolicy:      r.SslPolicy,
		DefaultActions: defaultActions(r.DefaultAction),
	}
	if r.Protocol != nil {
		in.Protocol = elbv2types.ProtocolEnum(*r.Protocol)
	}
	if r.CertificateArn != nil {
		in.Certificates = []elbv2types.Certificate{{CertificateArn: r.CertificateArn}}
	}
	if r.AlpnPolicy != nil {
		in.AlpnPolicy = []string{*r.AlpnPolicy}
	}
	return in
}

// modifyChanged reports whether any field ModifyListener reconciles differs
// from the prior inputs. The load balancer is the listener's identity and
// forces a replace, and tags reconcile through the tag API, so neither is
// tested here.
func (r *Listener) modifyChanged(prior Listener) bool {
	return runtime.Changed(prior.Port, r.Port) ||
		runtime.Changed(prior.Protocol, r.Protocol) ||
		runtime.Changed(prior.SslPolicy, r.SslPolicy) ||
		runtime.Changed(prior.CertificateArn, r.CertificateArn) ||
		runtime.Changed(prior.AlpnPolicy, r.AlpnPolicy) ||
		runtime.Changed(prior.DefaultAction, r.DefaultAction)
}

// createListener calls CreateListener and retries it while ELBv2 rejects it
// because the default certificate was created moments earlier and the
// certificate control plane has not made it visible. That race clears on its
// own, so the retry runs over a bounded window.
func (r *Listener) createListener(
	ctx context.Context, client *elbv2.Client, in *elbv2.CreateListenerInput,
) (*elbv2.CreateListenerOutput, error) {
	var resp *elbv2.CreateListenerOutput
	err := retry.OnError(ctx, isCertificateNotFound, func(ctx context.Context) error {
		out, err := client.CreateListener(ctx, in)
		if err != nil {
			return err
		}
		resp = out
		return nil
	}, retry.WithTimeout(listenerWriteTimeout))
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// modifyListener calls ModifyListener and retries it on the same
// certificate-propagation race CreateListener guards against, over the same
// bounded window.
func (r *Listener) modifyListener(ctx context.Context, client *elbv2.Client, arn string) error {
	in := r.modifyInput(arn)
	err := retry.OnError(ctx, isCertificateNotFound, func(ctx context.Context) error {
		_, err := client.ModifyListener(ctx, in)
		return err
	}, retry.WithTimeout(listenerWriteTimeout))
	if err != nil {
		return fmt.Errorf("modify listener: %w", err)
	}
	return nil
}

// waitVisible polls DescribeListeners until the just-created listener is found
// at its own ARN, since CreateListener returns before the listener is
// consistently readable. A not-found read means the create is still
// propagating, so the wait keeps polling; any other error stops it.
func (r *Listener) waitVisible(ctx context.Context, client *elbv2.Client, arn string) error {
	what := fmt.Sprintf("listener %s", arn)
	return wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		_, err := r.find(ctx, client, arn)
		if err != nil {
			if errors.Is(err, runtime.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}, wait.WithTimeout(listenerWriteTimeout))
}
