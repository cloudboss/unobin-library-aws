package lambda

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/constraint"
)

// Invoke runs a Lambda function once and returns its result. Unlike a resource,
// an invocation has no desired state to reconcile and nothing to read back or
// destroy: it is a single Invoke call. The JSON payload is given inline through
// payload-content or read from a file through payload-path. invocation-type
// selects whether the call is synchronous (RequestResponse, the default),
// asynchronous (Event), or a permission-and-validation dry run (DryRun), and
// log-type Tail asks for the tail of the execution log alongside the response.
type Invoke struct {
	FunctionName   string  `ub:"function-name"`
	PayloadContent *string `ub:"payload-content"`
	PayloadPath    *string `ub:"payload-path"`
	Qualifier      *string `ub:"qualifier"`
	InvocationType *string `ub:"invocation-type"`
	LogType        *string `ub:"log-type"`
	ClientContext  *string `ub:"client-context"`
	TenantId       *string `ub:"tenant-id"`
}

// InvokeOutput holds what the function call returned. status-code is in the 200
// range for a successful request and does not reflect a function error, which
// fails the action instead. payload is the function's response body, empty for
// an asynchronous or dry-run call. executed-version is the version the call
// resolved to, and log-result is the decoded execution log, present only when
// log-type is Tail.
type InvokeOutput struct {
	StatusCode      int64  `ub:"status-code"`
	Payload         string `ub:"payload"`
	ExecutedVersion string `ub:"executed-version"`
	LogResult       string `ub:"log-result"`
}

// Constraints declares the rules Lambda places on an invocation's inputs.
// Exactly one of payload-content or payload-path supplies the payload, which is
// required. invocation-type and log-type each accept a fixed set of values when
// given.
func (r Invoke) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ExactlyOneOf(r.PayloadContent, r.PayloadPath),
		constraint.When(constraint.Present(r.InvocationType)).
			Require(constraint.OneOf(r.InvocationType, "RequestResponse", "Event", "DryRun")).
			Message("invocation-type must be RequestResponse, Event, or DryRun"),
		constraint.When(constraint.Present(r.LogType)).
			Require(constraint.OneOf(r.LogType, "None", "Tail")).
			Message("log-type must be None or Tail"),
	}
}

func (r *Invoke) Run(ctx context.Context, cfg any) (*InvokeOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	payload, err := r.payload()
	if err != nil {
		return nil, err
	}
	// Lambda expects the payload to be JSON, and the SDK passes the bytes
	// through without checking. A malformed payload is a configuration error, so
	// it is caught here rather than left to appear as an opaque runtime failure.
	if !json.Valid(payload) {
		return nil, fmt.Errorf("invoke lambda %s: payload is not valid JSON", r.FunctionName)
	}
	// The client context rides along as a base64 string the API decodes. A value
	// that is not valid base64 would be rejected by the service, so it is
	// validated here, before the call, and passed through unchanged.
	if r.ClientContext != nil {
		if _, err := base64.StdEncoding.DecodeString(*r.ClientContext); err != nil {
			return nil, fmt.Errorf("invoke lambda %s: client-context is not valid base64: %w",
				r.FunctionName, err)
		}
	}
	in := &lambda.InvokeInput{
		FunctionName:  aws.String(r.FunctionName),
		Payload:       payload,
		Qualifier:     r.Qualifier,
		ClientContext: r.ClientContext,
		TenantId:      r.TenantId,
	}
	// An omitted invocation type defaults to RequestResponse; it is set
	// explicitly so the status code semantics are predictable rather than left
	// to the SDK default.
	if r.InvocationType != nil {
		in.InvocationType = lambdatypes.InvocationType(*r.InvocationType)
	} else {
		in.InvocationType = lambdatypes.InvocationTypeRequestResponse
	}
	if r.LogType != nil {
		in.LogType = lambdatypes.LogType(*r.LogType)
	}
	out, err := client.Invoke(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("invoke lambda %s: %w", r.FunctionName, err)
	}
	// The function ran but raised an error of its own. The SDK reports this in
	// FunctionError rather than as a Go error, so the action turns it into one,
	// including the response payload, which holds the error detail.
	if functionError := aws.ToString(out.FunctionError); functionError != "" {
		return nil, fmt.Errorf("lambda function %s returned an error (%s): %s",
			r.FunctionName, functionError, string(out.Payload))
	}
	result := &InvokeOutput{
		StatusCode:      int64(out.StatusCode),
		Payload:         string(out.Payload),
		ExecutedVersion: aws.ToString(out.ExecutedVersion),
	}
	// The execution log is base64-encoded in the response and returned decoded.
	// A log that does not decode is not worth failing a successful call over, so
	// the result simply omits it.
	if logResult := aws.ToString(out.LogResult); logResult != "" {
		if decoded, err := base64.StdEncoding.DecodeString(logResult); err == nil {
			result.LogResult = string(decoded)
		}
	}
	return result, nil
}

// payload resolves the function input from whichever of payload-content or
// payload-path was given. Constraints guarantee exactly one is set, so an
// inline value is returned as is and a path is read from disk.
func (r *Invoke) payload() ([]byte, error) {
	if r.PayloadContent != nil {
		return []byte(*r.PayloadContent), nil
	}
	b, err := os.ReadFile(*r.PayloadPath)
	if err != nil {
		return nil, fmt.Errorf("read payload file %s: %w", *r.PayloadPath, err)
	}
	return b, nil
}
