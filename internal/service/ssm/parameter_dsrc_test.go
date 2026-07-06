package ssm

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParameterDataSourceWithDecryptionDefault(t *testing.T) {
	falseValue := false
	trueValue := true
	cases := map[string]struct {
		input *bool
		want  bool
	}{
		"omitted defaults true": {want: true},
		"explicit false":        {input: &falseValue, want: false},
		"explicit true":         {input: &trueValue, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := &ParameterDataSource{WithDecryption: tc.input}
			assert.Equal(t, tc.want, r.withDecryption())
		})
	}
}

func TestParameterDataSourceOutputInsecureValue(t *testing.T) {
	value := "plain"
	out, err := parameterDataOutput("/input-name", &ssm.GetParameterOutput{
		Parameter: &ssmtypes.Parameter{
			ARN:     aws.String("arn:aws:ssm:us-east-1:123456789012:parameter/plain"),
			Name:    aws.String("/plain"),
			Type:    ssmtypes.ParameterTypeString,
			Version: 3,
			Value:   aws.String(value),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "/plain", out.Name)
	assert.Equal(t, value, out.Value)
	require.NotNil(t, out.InsecureValue)
	assert.Equal(t, value, *out.InsecureValue)
}

func TestParameterDataSourceOutputSuppressesSecureInsecureValue(t *testing.T) {
	out, err := parameterDataOutput("/secure", &ssm.GetParameterOutput{
		Parameter: &ssmtypes.Parameter{
			Name:  aws.String("/secure"),
			Type:  ssmtypes.ParameterTypeSecureString,
			Value: aws.String("decrypted-or-ciphertext"),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "decrypted-or-ciphertext", out.Value)
	assert.Nil(t, out.InsecureValue)
}

func TestParameterDataSourceOutputRequiresParameter(t *testing.T) {
	cases := map[string]*ssm.GetParameterOutput{
		"nil response":  nil,
		"nil parameter": {},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parameterDataOutput("/missing", resp)
			require.Error(t, err)
			assert.Contains(t, err.Error(), `ssm parameter "/missing" not found`)
		})
	}
}
