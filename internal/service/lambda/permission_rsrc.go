package lambda

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// permissionReconcileTimeout bounds the eventual-consistency waits a permission
// statement needs: after AddPermission the statement is not immediately
// readable through GetPolicy, and after RemovePermission it lingers briefly in
// the policy. Both windows clear well inside five minutes.
const permissionReconcileTimeout = 5 * time.Minute

// functionLocks serializes AddPermission and RemovePermission calls per
// function. Lambda loses permission statements when two such calls run against
// the same function at once, and unobin reconciles each resource in its own
// goroutine, so two lambda-permission resources on one function can race.
// Holding a per-function lock across the call keeps the policy consistent.
// functionLocksMu guards the map itself; each value guards one function.
var (
	functionLocks   = map[string]*sync.Mutex{}
	functionLocksMu sync.Mutex
)

// functionLock returns the mutex for a function, creating it on first use. The
// key is the resolved function name (or ARN), so all resources naming the same
// function share one lock.
func functionLock(functionName string) *sync.Mutex {
	functionLocksMu.Lock()
	defer functionLocksMu.Unlock()
	lock, ok := functionLocks[functionName]
	if !ok {
		lock = &sync.Mutex{}
		functionLocks[functionName] = lock
	}
	return lock
}

// Permission grants a principal permission to invoke a Lambda function by
// adding a statement to the function's resource-based policy. Every field is
// fixed at create time: AWS exposes no call to edit a statement in place, so a
// change to any field replaces the statement. The statement has no tags and no
// server-assigned handle beyond its statement id, which is the identity. When
// statement-id is omitted a unique one is generated so the statement can still
// be addressed for read and delete.
type Permission struct {
	Action                string  `ub:"action"`
	FunctionName          string  `ub:"function-name"`
	Principal             string  `ub:"principal"`
	StatementId           *string `ub:"statement-id"`
	Qualifier             *string `ub:"qualifier"`
	EventSourceToken      *string `ub:"event-source-token"`
	FunctionUrlAuthType   *string `ub:"function-url-auth-type"`
	InvokedViaFunctionUrl *bool   `ub:"invoked-via-function-url"`
	PrincipalOrgID        *string `ub:"principal-org-id"`
	SourceAccount         *string `ub:"source-account"`
	SourceArn             *string `ub:"source-arn"`
}

// PermissionOutput holds the resolved statement id. When statement-id is set on
// the input the two match; when it is omitted this is the generated id, which a
// downstream reader needs to reference the statement.
type PermissionOutput struct {
	StatementId string `ub:"statement-id"`
}

func (r *Permission) SchemaVersion() int { return 1 }

// ReplaceFields lists every input, because a permission statement is immutable:
// Lambda has no operation to edit a statement, only AddPermission and
// RemovePermission, so any change is a remove-and-add, which is a replacement.
func (r *Permission) ReplaceFields() []string {
	return []string{
		"action",
		"function-name",
		"principal",
		"statement-id",
		"qualifier",
		"event-source-token",
		"function-url-auth-type",
		"invoked-via-function-url",
		"principal-org-id",
		"source-account",
		"source-arn",
	}
}

// Constraints declares the only schema rule on a permission's inputs: the
// function URL auth type, when given, is one of the two values AddPermission
// accepts. The API enforces the remaining cross-field guidance server-side, so
// it is not duplicated here.
func (r Permission) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.When(constraint.Present(r.FunctionUrlAuthType)).
			Require(constraint.OneOf(r.FunctionUrlAuthType, "AWS_IAM", "NONE")).
			Message("function-url-auth-type must be AWS_IAM or NONE"),
	}
}

func (r *Permission) Create(ctx context.Context, cfg *awsCfg) (*PermissionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	statementID, err := r.resolveStatementID()
	if err != nil {
		return nil, err
	}
	in := &lambda.AddPermissionInput{
		Action:                aws.String(r.Action),
		FunctionName:          aws.String(r.FunctionName),
		Principal:             aws.String(r.Principal),
		StatementId:           aws.String(statementID),
		Qualifier:             r.Qualifier,
		EventSourceToken:      r.EventSourceToken,
		InvokedViaFunctionUrl: r.InvokedViaFunctionUrl,
		PrincipalOrgID:        r.PrincipalOrgID,
		SourceAccount:         r.SourceAccount,
		SourceArn:             r.SourceArn,
	}
	if r.FunctionUrlAuthType != nil {
		in.FunctionUrlAuthType = lambdatypes.FunctionUrlAuthType(*r.FunctionUrlAuthType)
	}
	// A just-created function, role, or principal named in the statement may not
	// be visible yet, which AddPermission reports as a resource conflict or a
	// not-found; both clear as AWS catches up. The lock serializes this call
	// with any other permission write to the same function so Lambda does not
	// lose a statement to a concurrent write.
	lock := functionLock(r.FunctionName)
	lock.Lock()
	err = wait.Until(ctx, fmt.Sprintf("permission %s on %s", statementID, r.FunctionName),
		func(ctx context.Context) (bool, error) {
			_, err := client.AddPermission(ctx, in)
			if err != nil {
				if isResourceConflict(err) || isNotFound(err) {
					return false, nil
				}
				return false, fmt.Errorf("add permission: %w", err)
			}
			return true, nil
		}, wait.WithInterval(time.Second), wait.WithTimeout(permissionReconcileTimeout))
	lock.Unlock()
	if err != nil {
		return nil, err
	}
	// The statement is not readable through GetPolicy the instant AddPermission
	// returns, so the create-path read waits for it to appear rather than taking
	// the first not-found for drift.
	return r.read(ctx, client, statementID, true)
}

func (r *Permission) Read(
	ctx context.Context, cfg *awsCfg, prior *PermissionOutput,
) (*PermissionOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, prior.StatementId, false)
}

// Update never changes anything: every field is immutable, so a change is a
// replacement the runtime drives through Delete and Create. Update only returns
// the prior outputs so the identity stays referenceable.
func (r *Permission) Update(
	ctx context.Context, cfg *awsCfg, prior runtime.Prior[Permission, *PermissionOutput],
) (*PermissionOutput, error) {
	return prior.Outputs, nil
}

func (r *Permission) Delete(ctx context.Context, cfg *awsCfg, prior *PermissionOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	statementID := prior.StatementId
	// The lock pairs with Create: a remove racing an add against the same
	// function can lose an unrelated statement, so the two are serialized.
	lock := functionLock(r.FunctionName)
	lock.Lock()
	_, err = client.RemovePermission(ctx, &lambda.RemovePermissionInput{
		FunctionName: aws.String(r.FunctionName),
		StatementId:  aws.String(statementID),
		Qualifier:    r.Qualifier,
	})
	lock.Unlock()
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("remove permission: %w", err)
	}
	// RemovePermission returns before the statement leaves the policy, so wait
	// for it to be gone; a later read must not still find it.
	return wait.Until(ctx, fmt.Sprintf("permission %s on %s to be removed", statementID,
		r.FunctionName),
		func(ctx context.Context) (bool, error) {
			found, err := r.findStatement(ctx, client, statementID)
			if err != nil {
				return false, err
			}
			return !found, nil
		}, wait.WithInterval(time.Second), wait.WithTimeout(permissionReconcileTimeout))
}

// read reconstructs the statement's outputs by matching its id in the policy.
// When created is true the statement was just added, so a not-found means it
// has not propagated yet and the read waits for it; otherwise a not-found is
// drift and maps to runtime.ErrNotFound at once.
func (r *Permission) read(
	ctx context.Context, client *lambda.Client, statementID string, created bool,
) (*PermissionOutput, error) {
	timeout := time.Duration(0)
	if created {
		timeout = permissionReconcileTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		found, err := r.findStatement(ctx, client, statementID)
		if err != nil {
			return nil, err
		}
		if found {
			return &PermissionOutput{StatementId: statementID}, nil
		}
		if !created || time.Now().After(deadline) {
			return nil, runtime.ErrNotFound
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// findStatement reports whether the policy holds a statement with the given id.
// A missing policy and an absent statement both mean the statement is not
// there, so each returns false rather than an error.
func (r *Permission) findStatement(
	ctx context.Context, client *lambda.Client, statementID string,
) (bool, error) {
	resp, err := client.GetPolicy(ctx, &lambda.GetPolicyInput{
		FunctionName: aws.String(r.FunctionName),
		Qualifier:    r.Qualifier,
	})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get policy: %w", err)
	}
	var policy struct {
		Statement []struct {
			Sid string `json:"Sid"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(aws.ToString(resp.Policy)), &policy); err != nil {
		return false, fmt.Errorf("parse policy: %w", err)
	}
	for _, s := range policy.Statement {
		if s.Sid == statementID {
			return true, nil
		}
	}
	return false, nil
}

// resolveStatementID returns the statement id to use: the input value verbatim
// when set, otherwise a generated unique id. The generated id becomes the
// resource identity, so it is returned in the output to stay referenceable.
func (r *Permission) resolveStatementID() (string, error) {
	if r.StatementId != nil {
		return *r.StatementId, nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate statement id: %w", err)
	}
	return "unobin-" + hex.EncodeToString(b), nil
}
