// Package partition derives the AWS partition from a region and recognizes
// the errors a non-standard partition returns when it does not support an
// operation that the standard partition does. The ISO partitions, for one,
// reject tagging a resource at create time, so a create that sends tags
// fails there; a caller retries without tags and sets them afterward. The
// standard aws partition supports these operations, so the same error there
// means a real failure and must not be mistaken for an unsupported feature.
package partition

import (
	"errors"
	"strings"

	smithy "github.com/aws/smithy-go"
)

// Of returns the AWS partition id for a region, such as aws, aws-us-gov,
// aws-cn, or one of the aws-iso partitions. A region that matches none of
// the special prefixes belongs to the standard aws partition.
func Of(region string) string {
	switch {
	case strings.HasPrefix(region, "us-gov-"):
		return "aws-us-gov"
	case strings.HasPrefix(region, "cn-"):
		return "aws-cn"
	case strings.HasPrefix(region, "us-iso-"):
		return "aws-iso"
	case strings.HasPrefix(region, "us-isob-"):
		return "aws-iso-b"
	case strings.HasPrefix(region, "eu-isoe-"):
		return "aws-iso-e"
	case strings.HasPrefix(region, "us-isof-"):
		return "aws-iso-f"
	default:
		return "aws"
	}
}

// unsupportedCodes are the API error codes a non-standard partition uses to
// signal that an operation is not available there. The match is a substring
// test on the error code, the same comparison the Terraform provider makes.
var unsupportedCodes = []string{
	"AccessDenied",
	"AuthorizationError",
	"InternalException",
	"InternalServiceError",
	"InvalidAction",
	"InvalidParameterException",
	"InvalidParameterValue",
	"InvalidRequest",
	"OperationDisabledException",
	"OperationNotPermitted",
	"UnknownOperationException",
	"UnsupportedFeatureException",
	"UnsupportedOperation",
	"ValidationException",
}

// UnsupportedOperation reports whether err, seen while calling AWS in the
// given region, suggests the operation is unavailable in that region's
// partition rather than having genuinely failed. It is always false in the
// standard aws partition and false for a nil error: there, the error codes
// it looks for mean a real failure the caller must report. A caller uses it
// to decide whether to retry a create without tags on a partition that
// cannot tag at create time.
func UnsupportedOperation(region string, err error) bool {
	if err == nil || Of(region) == "aws" {
		return false
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	for _, c := range unsupportedCodes {
		if strings.Contains(code, c) {
			return true
		}
	}
	// Some partitions report the limitation only in the message of an
	// otherwise generic validation error.
	if code == "ValidationError" && strings.Contains(apiErr.ErrorMessage(), "not support tagging") {
		return true
	}
	return false
}
