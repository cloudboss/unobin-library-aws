package partition

import (
	"errors"
	"testing"

	smithy "github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
)

func TestOf(t *testing.T) {
	cases := []struct {
		region string
		want   string
	}{
		{"us-east-1", "aws"},
		{"eu-west-3", "aws"},
		{"ap-southeast-2", "aws"},
		{"us-gov-west-1", "aws-us-gov"},
		{"us-gov-east-1", "aws-us-gov"},
		{"cn-north-1", "aws-cn"},
		{"cn-northwest-1", "aws-cn"},
		{"us-iso-east-1", "aws-iso"},
		{"us-isob-east-1", "aws-iso-b"},
		{"eu-isoe-west-1", "aws-iso-e"},
		{"us-isof-south-1", "aws-iso-f"},
		{"", "aws"},
	}
	for _, c := range cases {
		t.Run(c.region, func(t *testing.T) {
			assert.Equal(t, c.want, Of(c.region))
		})
	}
}

type apiErr struct {
	code    string
	message string
}

func (e *apiErr) Error() string                 { return e.code + ": " + e.message }
func (e *apiErr) ErrorCode() string             { return e.code }
func (e *apiErr) ErrorMessage() string          { return e.message }
func (e *apiErr) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestUnsupportedOperation(t *testing.T) {
	cases := []struct {
		name   string
		region string
		err    error
		want   bool
	}{
		{"nil error", "us-iso-east-1", nil, false},
		{"standard partition never unsupported", "us-east-1", &apiErr{code: "AccessDenied"}, false},
		{"iso access denied", "us-iso-east-1", &apiErr{code: "AccessDenied"}, true},
		{"gov authorization error", "us-gov-west-1", &apiErr{code: "AuthorizationError"}, true},
		{"iso unsupported operation", "us-iso-east-1", &apiErr{code: "UnsupportedOperation"}, true},
		{"iso invalid action", "us-iso-east-1", &apiErr{code: "InvalidAction"}, true},
		{"iso validation exception", "us-iso-east-1", &apiErr{code: "ValidationException"}, true},
		{
			name:   "iso validation error mentioning tagging",
			region: "us-iso-east-1",
			err:    &apiErr{code: "ValidationError", message: "this region does not support tagging"},
			want:   true,
		},
		{
			name:   "iso validation error without tagging mention",
			region: "us-iso-east-1",
			err:    &apiErr{code: "ValidationError", message: "some other problem"},
			want:   false,
		},
		{
			name:   "iso unrelated error",
			region: "us-iso-east-1",
			err:    &apiErr{code: "NoSuchEntity"},
			want:   false,
		},
		{"non-api error in iso", "us-iso-east-1", errors.New("dial tcp: timeout"), false},
		{
			name:   "substring match on code",
			region: "us-iso-east-1",
			err:    &apiErr{code: "InvalidParameterValueException"},
			want:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, UnsupportedOperation(c.region, c.err))
		})
	}
}
