package eks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type accessPolicyAssociationClient interface {
	eks.ListAssociatedAccessPoliciesAPIClient
	AssociateAccessPolicy(context.Context, *eks.AssociateAccessPolicyInput,
		...func(*eks.Options)) (*eks.AssociateAccessPolicyOutput, error)
	DisassociateAccessPolicy(context.Context, *eks.DisassociateAccessPolicyInput,
		...func(*eks.Options)) (*eks.DisassociateAccessPolicyOutput, error)
}

const associationPropagationTimeout = 2 * time.Minute

var errAssociatedPolicyCardinality = errors.New("expected one matching EKS access policy")

type accessPolicyAssociationKey struct {
	clusterName  string
	principalARN string
	policyARN    string
}

func (k accessPolicyAssociationKey) String() string {
	return fmt.Sprintf("%s / %s / %s", k.clusterName, k.principalARN, k.policyARN)
}

func storedAccessPolicyAssociationKey(
	output *AccessPolicyAssociationResourceOutput,
) (accessPolicyAssociationKey, error) {
	switch {
	case output == nil:
		return accessPolicyAssociationKey{}, errors.New("prior EKS access policy association is missing")
	case output.ClusterName == "":
		return accessPolicyAssociationKey{}, errors.New("prior association has no cluster name")
	case output.PrincipalARN == "":
		return accessPolicyAssociationKey{}, errors.New("prior association has no principal ARN")
	case output.PolicyARN == "":
		return accessPolicyAssociationKey{}, errors.New("prior association has no policy ARN")
	default:
		return accessPolicyAssociationKey{output.ClusterName, output.PrincipalARN, output.PolicyARN},
			nil
	}
}

func associationPriorConfig(current, prior *awsCfg) *awsCfg {
	if prior == nil {
		return current
	}
	if current == nil {
		return prior
	}
	if aws.ToString(prior.Region) == aws.ToString(current.Region) {
		return current
	}
	cfg := *current
	cfg.Region = prior.Region
	cfg.EndpointURL = prior.EndpointURL
	return &cfg
}

func (r AccessPolicyAssociationResource) createAccessPolicyAssociation(
	ctx context.Context, client accessPolicyAssociationClient, clock clusterClock,
) (*AccessPolicyAssociationResourceOutput, error) {
	key := accessPolicyAssociationKey{r.ClusterName, r.PrincipalARN, r.PolicyARN}
	request := r.prepareAssociation()
	if err := associateAccessPolicy(ctx, client, clock, request); err != nil {
		return nil, fmt.Errorf("associate EKS access policy %s: %w", key, err)
	}
	output, err := listAssociatedAccessPolicy(ctx, client, key)
	if err != nil {
		return nil, fmt.Errorf("read newly associated EKS access policy: %w", err)
	}
	return output, nil
}

func (r AccessPolicyAssociationResource) prepareAssociation() *eks.AssociateAccessPolicyInput {
	return &eks.AssociateAccessPolicyInput{
		ClusterName:  aws.String(r.ClusterName),
		PrincipalArn: aws.String(r.PrincipalARN),
		PolicyArn:    aws.String(r.PolicyARN),
		AccessScope: &ekstypes.AccessScope{
			Type:       ekstypes.AccessScopeType(r.AccessScope.Type),
			Namespaces: r.AccessScope.requestNamespaces(),
		},
	}
}

func (scope AccessPolicyAssociationScope) requestNamespaces() []string {
	var values []string
	if scope.Namespaces != nil {
		values = *scope.Namespaces
	}
	namespaces := canonicalAssociationNamespaces(values)
	return slices.DeleteFunc(namespaces, func(namespace string) bool { return namespace == "" })
}

func canonicalAssociationNamespaces(namespaces []string) []string {
	result := append(make([]string, 0, len(namespaces)), namespaces...)
	slices.Sort(result)
	return slices.Compact(result)
}

func associateAccessPolicy(
	ctx context.Context, client accessPolicyAssociationClient, clock clusterClock,
	request *eks.AssociateAccessPolicyInput,
) error {
	deadline := clock.Now().Add(associationPropagationTimeout)
	for attempt := 0; ; attempt++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		final := !clock.Now().Before(deadline)
		_, err := client.AssociateAccessPolicy(ctx, request)
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		var missing *ekstypes.ResourceNotFoundException
		if final || !errors.As(err, &missing) || !strings.Contains(
			missing.ErrorMessage(), "The specified principalArn could not be found",
		) {
			return err
		}
		// The first attempt at or past the deadline is the final grace attempt.
		if remaining := deadline.Sub(clock.Now()); remaining > 0 {
			if err := clock.Sleep(ctx, min(remaining, clusterBackoff(attempt))); err != nil {
				return err
			}
		}
	}
}

func readAccessPolicyAssociation(
	ctx context.Context, client accessPolicyAssociationClient, key accessPolicyAssociationKey,
) (*AccessPolicyAssociationResourceOutput, error) {
	output, err := listAssociatedAccessPolicy(ctx, client, key)
	var missing *ekstypes.ResourceNotFoundException
	if errors.Is(err, errAssociatedPolicyCardinality) || errors.As(err, &missing) {
		return nil, runtime.ErrNotFound
	}
	return output, err
}

func listAssociatedAccessPolicy(
	ctx context.Context, client accessPolicyAssociationClient, key accessPolicyAssociationKey,
) (*AccessPolicyAssociationResourceOutput, error) {
	request := &eks.ListAssociatedAccessPoliciesInput{
		ClusterName: aws.String(key.clusterName), PrincipalArn: aws.String(key.principalARN),
	}
	pages := eks.NewListAssociatedAccessPoliciesPaginator(client, request)
	var found ekstypes.AssociatedAccessPolicy
	matches := 0
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list associated EKS access policies for %s: %w", key, err)
		}
		for _, policy := range page.AssociatedAccessPolicies {
			if policy.PolicyArn != nil && *policy.PolicyArn == key.policyARN {
				found = policy
				matches++
			}
		}
	}
	switch matches {
	case 0:
		return nil, fmt.Errorf("list associated EKS access policies for %s: %w: no matching policy",
			key, errAssociatedPolicyCardinality)
	case 1:
		return accessPolicyAssociationOutput(key, found), nil
	default:
		return nil, fmt.Errorf("list associated EKS access policies for %s: %w: %d matching policies",
			key, errAssociatedPolicyCardinality, matches)
	}
}

func disassociateAccessPolicy(
	ctx context.Context, client accessPolicyAssociationClient, key accessPolicyAssociationKey,
) error {
	_, err := client.DisassociateAccessPolicy(ctx, &eks.DisassociateAccessPolicyInput{
		ClusterName: aws.String(key.clusterName), PrincipalArn: aws.String(key.principalARN),
		PolicyArn: aws.String(key.policyARN),
	})
	var missing *ekstypes.ResourceNotFoundException
	if err == nil || errors.As(err, &missing) {
		return nil
	}
	return fmt.Errorf("disassociate EKS access policy %s: %w", key, err)
}

func accessPolicyAssociationOutput(
	key accessPolicyAssociationKey, policy ekstypes.AssociatedAccessPolicy,
) *AccessPolicyAssociationResourceOutput {
	output := &AccessPolicyAssociationResourceOutput{
		ClusterName: key.clusterName, PrincipalARN: key.principalARN, PolicyARN: key.policyARN,
		AssociatedAt: associationTimestamp(policy.AssociatedAt),
		ModifiedAt:   associationTimestamp(policy.ModifiedAt),
	}
	if scope := policy.AccessScope; scope != nil {
		output.AccessScope = &AccessPolicyAssociationScopeOutput{
			Type: string(scope.Type), Namespaces: canonicalAssociationNamespaces(scope.Namespaces),
		}
	}
	return output
}

func associationTimestamp(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
