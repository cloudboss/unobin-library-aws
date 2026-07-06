package ssm

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	ssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/cloudboss/unobin/pkg/defaults"
)

// ParameterDataSource resolves one existing SSM parameter by name with a single
// GetParameter call. with-decryption defaults to true, matching the lookup's
// read-sensitive-value behavior while still allowing an explicit false. A
// missing parameter is a data-source failure, not resource-style drift.
type ParameterDataSource struct {
	Name           string `ub:"name"`
	WithDecryption *bool  `ub:"with-decryption"`
}

// ParameterDataSourceOutput holds the attributes returned by GetParameter. Value is
// always sensitive, even for a plain String or StringList parameter. The
// non-sensitive insecure-value is populated only when SSM reports a non-secure
// parameter type.
type ParameterDataSourceOutput struct {
	Arn           string  `ub:"arn"`
	Name          string  `ub:"name"`
	Type          string  `ub:"type"`
	Version       int64   `ub:"version"`
	Value         string  `ub:"value,sensitive"`
	InsecureValue *string `ub:"insecure-value"`
}

// Defaults gives with-decryption its data-source default. Read also treats a nil
// field as true so direct calls behave the same as runtime-defaulted calls.
func (r ParameterDataSource) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.NullableValue(r.WithDecryption, true),
	}
}

// Read fetches the requested SSM parameter and flattens the returned parameter
// object. A missing parameter, nil response, or nil Parameter member is returned
// as a descriptive data-source error rather than runtime.ErrNotFound.
func (r *ParameterDataSource) Read(
	ctx context.Context,
	cfg *awsCfg,
) (*ParameterDataSourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(r.Name),
		WithDecryption: aws.Bool(r.withDecryption()),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("ssm parameter %q not found", r.Name)
		}
		return nil, fmt.Errorf("get parameter %s: %w", r.Name, err)
	}
	return parameterDataOutput(r.Name, resp)
}

func (r *ParameterDataSource) withDecryption() bool {
	if r.WithDecryption == nil {
		return true
	}
	return *r.WithDecryption
}

func parameterDataOutput(
	name string,
	resp *ssm.GetParameterOutput,
) (*ParameterDataSourceOutput, error) {
	if resp == nil || resp.Parameter == nil {
		return nil, fmt.Errorf("ssm parameter %q not found", name)
	}
	param := resp.Parameter
	out := &ParameterDataSourceOutput{
		Arn:     aws.ToString(param.ARN),
		Name:    aws.ToString(param.Name),
		Type:    string(param.Type),
		Version: param.Version,
		Value:   aws.ToString(param.Value),
	}
	if param.Type != ssmtypes.ParameterTypeSecureString {
		out.InsecureValue = param.Value
	}
	return out, nil
}
