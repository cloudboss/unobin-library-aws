package sns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	sns "github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/retry"
	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// TopicPolicy manages the access policy attached to an SNS topic. The topic
// ARN is the policy's identity; a topic holds a single Policy attribute, so the
// topic cannot change without replacing the policy, while the document is
// reconciled in place. Both the write and the read go through the topic's
// Policy attribute (SetTopicAttributes / GetTopicAttributes); SNS has no
// dedicated topic-policy API. The document is sent verbatim: unobin compares
// inputs as written, so the policy never needs canonicalizing to avoid a
// phantom diff against the form SNS echoes back.
type TopicPolicy struct {
	Arn    string `ub:"arn"`
	Policy string `ub:"policy"`
}

// TopicPolicyOutput holds the topic owner alongside the cloud-side policy. The
// owner (the topic's account id) is required by Delete to build the default
// policy's AWS:SourceOwner condition, so it must be a computed output read from
// prior on Delete. The policy is the form SNS reports back, exposed for
// reference by anything reading the resolved document.
type TopicPolicyOutput struct {
	Owner  string `ub:"owner"`
	Policy string `ub:"policy"`
}

func (r *TopicPolicy) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs SNS fixes for the life of the policy. A topic
// holds a single Policy attribute keyed by its ARN, so re-pointing the policy
// at a different topic means resetting it here and writing it there. The policy
// document itself is reconciled in place by Update.
func (r *TopicPolicy) ReplaceFields() []string {
	return []string{"arn"}
}

func (r *TopicPolicy) Create(ctx context.Context, cfg any) (*TopicPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client, r.Policy); err != nil {
		return nil, err
	}
	// Route through Read so the owner output is populated from
	// GetTopicAttributes; Delete needs it to build the default policy.
	return r.read(ctx, client)
}

func (r *TopicPolicy) Read(
	ctx context.Context, cfg any, prior *TopicPolicyOutput,
) (*TopicPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *TopicPolicy) Update(
	ctx context.Context, cfg any, prior runtime.Prior[TopicPolicy, *TopicPolicyOutput],
) (*TopicPolicyOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client, r.Policy); err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *TopicPolicy) Delete(ctx context.Context, cfg any, prior *TopicPolicyOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// SNS has no delete-policy API, so the policy is reset to the topic's
	// default document rather than removed. The default names the topic ARN and
	// its owner; the owner is not an input, so it is read from the prior output
	// captured at Read time.
	policy := defaultTopicPolicy(r.Arn, prior.Owner)
	if err := r.put(ctx, client, policy); err != nil {
		// A topic that is already gone took its policy with it, so a missing
		// topic counts as deleted.
		if isTopicGone(err) {
			return nil
		}
		return err
	}
	return nil
}

// read returns the topic's owner and current policy. A topic that is gone, or a
// live topic whose Policy attribute is empty, is drift: both map to
// runtime.ErrNotFound so the resource is recreated.
//
// The Policy attribute is read under a settle wait. When a policy names an IAM
// principal created moments earlier, SNS first echoes it back as the
// principal's transient unique-id and only later rewrites it to the resolved
// form. Because Create routes through read and this resource exposes the policy
// as an output, capturing the transient form would make the next plan see a
// difference against the resolved policy and force a spurious Update. The wait
// re-reads until every principal in the document has resolved, then returns the
// settled policy. A policy whose principals are already account ids, ARNs, or
// "*" -- the default policy and ordinary policies -- passes on the first probe.
func (r *TopicPolicy) read(ctx context.Context, client *sns.Client) (*TopicPolicyOutput, error) {
	var out *TopicPolicyOutput
	probe := func(ctx context.Context) (bool, error) {
		resp, err := client.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{
			TopicArn: aws.String(r.Arn),
		})
		if err != nil {
			if isTopicGone(err) {
				return false, runtime.ErrNotFound
			}
			return false, fmt.Errorf("get topic attributes: %w", err)
		}
		// An empty Policy on a live topic means the policy this resource manages
		// is absent, the same drift as a missing topic.
		if resp == nil || resp.Attributes["Policy"] == "" {
			return false, runtime.ErrNotFound
		}
		policy := resp.Attributes["Policy"]
		if !policyPrincipalsResolved(policy) {
			return false, nil
		}
		out = &TopicPolicyOutput{
			Owner:  resp.Attributes["Owner"],
			Policy: policy,
		}
		return true, nil
	}
	if err := wait.Until(ctx, "sns topic policy principals", probe); err != nil {
		return nil, err
	}
	return out, nil
}

// policyPrincipalsResolved reports whether every AWS principal named in the
// policy document has resolved to its canonical form. It JSON-unmarshals the
// document, walks Statement (a single object or an array of objects) and each
// statement's Principal.AWS (a string or a list of strings), and reports true
// only when every entry is valid. A document that does not parse, or that names
// no AWS principal, is treated as resolved: there is nothing to wait on.
func policyPrincipalsResolved(policy string) bool {
	var doc map[string]any
	if json.Unmarshal([]byte(policy), &doc) != nil {
		return true
	}
	for _, stmt := range statements(doc["Statement"]) {
		for _, principal := range awsPrincipals(stmt) {
			if !validPrincipal(principal) {
				return false
			}
		}
	}
	return true
}

// statements normalizes a policy's Statement value, which may be a single
// statement object or an array of them, into a slice of statement objects.
func statements(v any) []map[string]any {
	switch s := v.(type) {
	case map[string]any:
		return []map[string]any{s}
	case []any:
		var out []map[string]any
		for _, e := range s {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

// awsPrincipals extracts the AWS principal entries from a statement. Principal
// is an object whose AWS member is a single principal string or a list of them;
// a statement with no AWS principal contributes nothing to wait on.
func awsPrincipals(stmt map[string]any) []string {
	principal, ok := stmt["Principal"].(map[string]any)
	if !ok {
		return nil
	}
	switch v := principal["AWS"].(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// validPrincipal reports whether an AWS principal entry has resolved to its
// canonical form: the wildcard "*", a 12-digit account id, or an arn: ARN. A
// principal that is none of these -- a transient unique-id such as one
// beginning with AROA, AIDA, or AGPA that SNS emits before an IAM principal
// created moments earlier has propagated -- is not yet resolved.
func validPrincipal(principal string) bool {
	if principal == "*" {
		return true
	}
	if strings.HasPrefix(principal, "arn:") {
		return true
	}
	return isAccountID(principal)
}

// isAccountID reports whether s is a 12-digit AWS account id.
func isAccountID(s string) bool {
	if len(s) != 12 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// put writes the topic's Policy attribute, retrying InvalidParameterException
// over a two-minute window. A policy naming an IAM principal or resource
// created moments earlier is rejected with that error until the named entity
// becomes visible, which clears on its own within the propagation window.
func (r *TopicPolicy) put(ctx context.Context, client *sns.Client, policy string) error {
	in := &sns.SetTopicAttributesInput{
		TopicArn:       aws.String(r.Arn),
		AttributeName:  aws.String("Policy"),
		AttributeValue: aws.String(policy),
	}
	err := retry.OnError(ctx, isInvalidParameter, func(ctx context.Context) error {
		_, err := client.SetTopicAttributes(ctx, in)
		return err
	})
	if err != nil {
		return fmt.Errorf("set topic policy: %w", err)
	}
	return nil
}

// isTopicGone reports whether err is the SNS not-found error for a topic that
// no longer exists. SNS raises NotFoundException, reaching the caller as a
// smithy.APIError with the service code NotFound.
func isTopicGone(err error) bool {
	return isNotFound(err, "NotFound")
}

// isInvalidParameter reports whether err is InvalidParameterException, the
// transient rejection SNS returns while a principal or resource named in the
// policy has not yet propagated.
func isInvalidParameter(err error) bool {
	var ipe *snstypes.InvalidParameterException
	return errors.As(err, &ipe)
}

// defaultTopicPolicy reproduces the access policy SNS applies to a topic by
// default, the document Delete resets the Policy attribute to. The topic ARN
// and the account id are substituted as JSON strings, escaped by json.Marshal.
// The Version is the older 2008-10-17 that SNS uses for this default, not
// 2012-10-17. The document ends with a trailing newline so the reset value
// matches the default AWS reports back byte for byte.
func defaultTopicPolicy(topicArn, accountID string) string {
	return fmt.Sprintf(`{
  "Version": "2008-10-17",
  "Id": "__default_policy_ID",
  "Statement": [
    {
      "Sid": "__default_statement_ID",
      "Effect": "Allow",
      "Principal": {
        "AWS": "*"
      },
      "Action": [
        "SNS:GetTopicAttributes",
        "SNS:SetTopicAttributes",
        "SNS:AddPermission",
        "SNS:RemovePermission",
        "SNS:DeleteTopic",
        "SNS:Subscribe",
        "SNS:ListSubscriptionsByTopic",
        "SNS:Publish",
        "SNS:Receive"
      ],
      "Resource": %s,
      "Condition": {
        "StringEquals": {
          "AWS:SourceOwner": %s
        }
      }
    }
  ]
}
`, jsonString(topicArn), jsonString(accountID))
}

// jsonString returns s as a JSON string literal, including the surrounding
// quotes and any escaping JSON requires, so a substituted ARN or account id is
// well-formed inside the default policy document.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
