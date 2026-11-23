package eks

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
)

const defaultAccessEntryType = "STANDARD"

var (
	accessEntryARNPartition = regexp.MustCompile(`^aws(-[a-z]+)*$`)
	accessEntryARNRegion    = regexp.MustCompile(`^[a-z]{2,4}(?:-[a-z]+)+-\d{1,2}$`)
	accessEntryARNAccount   = regexp.MustCompile(
		`^(aws|aws-managed|third-party|aws-marketplace|partner-managed|\d{12}|cw.{10})$`)
)

func (r AccessEntryResource) prepareAccessEntryCreate() (*eks.CreateAccessEntryInput, error) {
	if r.PrincipalARN != "" {
		parsed, err := awsarn.Parse(r.PrincipalARN)
		if err != nil {
			return nil, fmt.Errorf("parse EKS access entry principal-arn: %w", err)
		}
		if !accessEntryARNPartition.MatchString(parsed.Partition) ||
			(parsed.Region != "" && !accessEntryARNRegion.MatchString(parsed.Region)) ||
			(parsed.AccountID != "" && !accessEntryARNAccount.MatchString(parsed.AccountID)) ||
			parsed.Resource == "" {
			return nil, fmt.Errorf("EKS access entry principal-arn %q is invalid", r.PrincipalARN)
		}
	}
	request := &eks.CreateAccessEntryInput{
		ClusterName:  aws.String(r.ClusterName),
		PrincipalArn: aws.String(r.PrincipalARN),
		Type:         aws.String(effectiveAccessEntryType(r.Type)),
		Tags:         clusterTags(valueMapOrNil(r.Tags)),
	}
	if r.KubernetesGroups != nil && len(*r.KubernetesGroups) > 0 {
		request.KubernetesGroups = accessEntryRequestGroups(*r.KubernetesGroups)
	}
	if aws.ToString(r.UserName) != "" {
		request.Username = r.UserName
	}
	return request, nil
}

func effectiveAccessEntryType(value *string) string {
	if value == nil {
		return defaultAccessEntryType
	}
	return *value
}

func accessEntryRequestGroups(groups []string) []string {
	expanded := make([]string, 0, len(groups))
	for _, group := range groups {
		if group != "" {
			expanded = append(expanded, group)
		}
	}
	slices.Sort(expanded)
	return slices.Compact(expanded)
}

func accessEntryObservedGroups(groups []string) []string {
	normalized := append(make([]string, 0, len(groups)), groups...)
	slices.Sort(normalized)
	return slices.Compact(normalized)
}
