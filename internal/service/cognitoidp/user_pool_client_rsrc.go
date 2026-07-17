package cognitoidp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitoidentityprovider "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type userPoolClientAPI interface {
	CreateUserPoolClient(context.Context, *cognitoidentityprovider.CreateUserPoolClientInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.CreateUserPoolClientOutput, error)
	DeleteUserPoolClient(context.Context, *cognitoidentityprovider.DeleteUserPoolClientInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.DeleteUserPoolClientOutput, error)
	DescribeUserPoolClient(context.Context, *cognitoidentityprovider.DescribeUserPoolClientInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.DescribeUserPoolClientOutput, error)
	UpdateUserPoolClient(context.Context, *cognitoidentityprovider.UpdateUserPoolClientInput,
		...func(*cognitoidentityprovider.Options),
	) (*cognitoidentityprovider.UpdateUserPoolClientOutput, error)
}

func (r *UserPoolClientResource) SchemaVersion() int { return 1 }

func (r *UserPoolClientResource) ReplaceFields() []string {
	return []string{"user-pool-id", "generate-secret"}
}

func (r *UserPoolClientResource) EquivalentInput(
	field string,
	prior UserPoolClientResource,
	current UserPoolClientResource,
) bool {
	if field != "generate-secret" {
		return false
	}
	return aws.ToBool(prior.GenerateSecret) == aws.ToBool(current.GenerateSecret)
}

func (r *UserPoolClientResource) ValidateInputs(context.Context, *awsCfg) error {
	return r.validateInputs()
}

func (r *UserPoolClientResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*UserPoolClientResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.create(ctx, client, rand.Reader)
}

func (r *UserPoolClientResource) create(
	ctx context.Context,
	client userPoolClientAPI,
	random io.Reader,
) (*UserPoolClientResourceOutput, error) {
	if err := r.validateInputs(); err != nil {
		return nil, err
	}
	name, err := r.resolveName(random)
	if err != nil {
		return nil, err
	}
	created, err := client.CreateUserPoolClient(ctx, r.createInput(name))
	if err != nil {
		return nil, fmt.Errorf("create user pool client %s: %w", name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("create user pool client %s: empty response", name)
	}
	return userPoolClientOutput(
		"create user pool client "+name,
		created.UserPoolClient,
		r.UserPoolID,
		"",
		name,
		nil,
	)
}

func (r *UserPoolClientResource) resolveName(random io.Reader) (string, error) {
	if r.Name != nil {
		return *r.Name, nil
	}
	value := make([]byte, 12)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", fmt.Errorf("generate user pool client name: %w", err)
	}
	return "unobin-" + hex.EncodeToString(value), nil
}

func userPoolClientOutput(
	operation string,
	client *cognitotypes.UserPoolClientType,
	expectedPoolID string,
	expectedClientID string,
	expectedName string,
	fallbackSecret *string,
) (*UserPoolClientResourceOutput, error) {
	if client == nil {
		return nil, fmt.Errorf("%s: response has no user pool client", operation)
	}
	poolID := aws.ToString(client.UserPoolId)
	if poolID == "" || poolID != expectedPoolID {
		return nil, fmt.Errorf("%s: response user pool ID is %q", operation, poolID)
	}
	clientID := aws.ToString(client.ClientId)
	if clientID == "" || expectedClientID != "" && clientID != expectedClientID {
		return nil, fmt.Errorf("%s: response client ID is %q", operation, clientID)
	}
	name := aws.ToString(client.ClientName)
	if name == "" || expectedName != "" && name != expectedName {
		return nil, fmt.Errorf("%s: response client name is %q", operation, name)
	}
	secret := client.ClientSecret
	if secret == nil {
		secret = fallbackSecret
	}
	return &UserPoolClientResourceOutput{
		ID: clientID, Name: name, ClientSecret: secret,
	}, nil
}

func (r *UserPoolClientResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *UserPoolClientResourceOutput,
) (*UserPoolClientResourceOutput, error) {
	id, err := priorUserPoolClientID(prior)
	if err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client, id)
}

func (r *UserPoolClientResource) read(
	ctx context.Context,
	client userPoolClientAPI,
	id string,
) (*UserPoolClientResourceOutput, error) {
	described, err := client.DescribeUserPoolClient(
		ctx,
		&cognitoidentityprovider.DescribeUserPoolClientInput{
			UserPoolId: aws.String(r.UserPoolID),
			ClientId:   aws.String(id),
		},
	)
	if isUserPoolNotFound(err) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("describe user pool client %s: %w", id, err)
	}
	if described == nil {
		return nil, fmt.Errorf("describe user pool client %s: empty response", id)
	}
	return userPoolClientOutput(
		"describe user pool client "+id,
		described.UserPoolClient,
		r.UserPoolID,
		id,
		"",
		nil,
	)
}

func (r *UserPoolClientResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[UserPoolClientResource, *UserPoolClientResourceOutput],
) (*UserPoolClientResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.update(ctx, client, prior, systemUserPoolClock{})
}

func (r *UserPoolClientResource) update(
	ctx context.Context,
	client userPoolClientAPI,
	prior runtime.Prior[UserPoolClientResource, *UserPoolClientResourceOutput],
	clock userPoolClock,
) (*UserPoolClientResourceOutput, error) {
	if err := r.validateInputs(); err != nil {
		return nil, err
	}
	id, err := priorUserPoolClientID(prior.Outputs)
	if err != nil {
		return nil, err
	}
	name, err := r.updateName(prior)
	if err != nil {
		return nil, err
	}
	if !r.mutableInputsChanged(prior.Inputs) && !r.nameDrifted(prior, name) {
		if prior.Observed != nil {
			return prior.Observed, nil
		}
		return prior.Outputs, nil
	}
	input := r.updateInput(id, name, prior.Inputs)
	var updated *cognitoidentityprovider.UpdateUserPoolClientOutput
	err = retryUserPoolWhen(ctx, clock, isUserPoolClientRetryable, func(ctx context.Context) error {
		var callErr error
		updated, callErr = client.UpdateUserPoolClient(ctx, input)
		return callErr
	})
	if err != nil {
		return nil, fmt.Errorf("update user pool client %s: %w", id, err)
	}
	if updated == nil {
		return nil, fmt.Errorf("update user pool client %s: empty response", id)
	}
	return userPoolClientOutput(
		"update user pool client "+id,
		updated.UserPoolClient,
		r.UserPoolID,
		id,
		name,
		userPoolClientPriorSecret(prior),
	)
}

func (r *UserPoolClientResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *UserPoolClientResourceOutput,
) error {
	id, err := priorUserPoolClientID(prior)
	if err != nil {
		return err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	return r.delete(ctx, client, id, systemUserPoolClock{})
}

func (r *UserPoolClientResource) delete(
	ctx context.Context,
	client userPoolClientAPI,
	id string,
	clock userPoolClock,
) error {
	err := retryUserPoolWhen(ctx, clock, isUserPoolClientRetryable, func(ctx context.Context) error {
		_, err := client.DeleteUserPoolClient(
			ctx,
			&cognitoidentityprovider.DeleteUserPoolClientInput{
				UserPoolId: aws.String(r.UserPoolID),
				ClientId:   aws.String(id),
			},
		)
		return err
	})
	if isUserPoolNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete user pool client %s: %w", id, err)
	}
	return nil
}

func (r *UserPoolClientResource) mutableInputsChanged(prior UserPoolClientResource) bool {
	current := *r
	current.UserPoolID = ""
	current.Name = nil
	current.GenerateSecret = nil
	prior.UserPoolID = ""
	prior.Name = nil
	prior.GenerateSecret = nil
	return runtime.Changed(prior, current)
}

func (r *UserPoolClientResource) updateName(
	prior runtime.Prior[UserPoolClientResource, *UserPoolClientResourceOutput],
) (string, error) {
	if r.Name != nil {
		return *r.Name, nil
	}
	if prior.Observed != nil && prior.Observed.Name != "" {
		return prior.Observed.Name, nil
	}
	if prior.Outputs != nil && prior.Outputs.Name != "" {
		return prior.Outputs.Name, nil
	}
	return "", fmt.Errorf("user pool client prior output has no name")
}

func (r *UserPoolClientResource) nameDrifted(
	prior runtime.Prior[UserPoolClientResource, *UserPoolClientResourceOutput],
	name string,
) bool {
	if r.Name == nil {
		return false
	}
	if prior.Observed != nil {
		return prior.Observed.Name != name
	}
	return prior.Outputs == nil || prior.Outputs.Name != name
}

func userPoolClientPriorSecret(
	prior runtime.Prior[UserPoolClientResource, *UserPoolClientResourceOutput],
) *string {
	if prior.Observed != nil && prior.Observed.ClientSecret != nil {
		return prior.Observed.ClientSecret
	}
	if prior.Outputs != nil {
		return prior.Outputs.ClientSecret
	}
	return nil
}

func isUserPoolClientRetryable(err error) bool {
	var concurrent *cognitotypes.ConcurrentModificationException
	return errors.As(err, &concurrent)
}

func priorUserPoolClientID(prior *UserPoolClientResourceOutput) (string, error) {
	if prior == nil {
		return "", fmt.Errorf("user pool client prior output is missing")
	}
	if prior.ID == "" {
		return "", fmt.Errorf("user pool client prior output has no id")
	}
	return prior.ID, nil
}
