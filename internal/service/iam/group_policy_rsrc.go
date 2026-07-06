package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	iam "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/wait"
)

// GroupPolicyResource manages an inline policy embedded in an IAM group. The group
// name and policy name form the identity, so a change to either makes a
// different policy and recreates this one. The policy document is updated in
// place with PutGroupPolicy.
type GroupPolicyResource struct {
	GroupName      string `ub:"group-name"`
	PolicyName     string `ub:"policy-name"`
	PolicyDocument string `ub:"policy-document"`
}

// GroupPolicyResourceOutput holds the identity and the policy document IAM stores.
// The document is URL-decoded and normalized so references see the stored JSON
// rather than IAM's percent-encoded transport value.
type GroupPolicyResourceOutput struct {
	GroupName      string `ub:"group-name"`
	PolicyName     string `ub:"policy-name"`
	PolicyDocument string `ub:"policy-document"`
}

func (r *GroupPolicyResource) SchemaVersion() int { return 1 }

func (r *GroupPolicyResource) ReplaceFields() []string {
	return []string{
		"group-name",
		"policy-name",
	}
}

func (r *GroupPolicyResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*GroupPolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.put(ctx, client); err != nil {
		return nil, err
	}
	return r.read(ctx, client, true)
}

func (r *GroupPolicyResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *GroupPolicyResourceOutput,
) (*GroupPolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return (&GroupPolicyResource{
		GroupName:  prior.GroupName,
		PolicyName: prior.PolicyName,
	}).read(ctx, client, false)
}

func (r *GroupPolicyResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[GroupPolicyResource, *GroupPolicyResourceOutput],
) (*GroupPolicyResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	desiredDocument, err := normalizeIAMPolicyJSON(r.PolicyDocument)
	if err != nil {
		return nil, err
	}
	if runtime.Changed(prior.Inputs.PolicyDocument, r.PolicyDocument) ||
		groupPolicyDocumentDrifted(prior.Observed, desiredDocument) {
		if err := r.putDocument(ctx, client, desiredDocument); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client, false)
}

func (r *GroupPolicyResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *GroupPolicyResourceOutput,
) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &iam.DeleteGroupPolicyInput{
		GroupName:  aws.String(prior.GroupName),
		PolicyName: aws.String(prior.PolicyName),
	}
	if _, err := client.DeleteGroupPolicy(ctx, in); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete group policy: %w", err)
	}
	return nil
}

func (r *GroupPolicyResource) put(ctx context.Context, client *iam.Client) error {
	document, err := normalizeIAMPolicyJSON(r.PolicyDocument)
	if err != nil {
		return err
	}
	return r.putDocument(ctx, client, document)
}

func (r *GroupPolicyResource) putDocument(
	ctx context.Context,
	client *iam.Client,
	document string,
) error {
	in := &iam.PutGroupPolicyInput{
		GroupName:      aws.String(r.GroupName),
		PolicyName:     aws.String(r.PolicyName),
		PolicyDocument: aws.String(document),
	}
	if _, err := client.PutGroupPolicy(ctx, in); err != nil {
		return fmt.Errorf("put group policy: %w", err)
	}
	return nil
}

func groupPolicyDocumentDrifted(observed *GroupPolicyResourceOutput, desired string) bool {
	return observed != nil && runtime.Changed(observed.PolicyDocument, desired)
}

func (r *GroupPolicyResource) read(
	ctx context.Context, client *iam.Client, created bool,
) (*GroupPolicyResourceOutput, error) {
	var out *GroupPolicyResourceOutput
	what := fmt.Sprintf("group policy %s on group %s", r.PolicyName, r.GroupName)
	err := wait.Until(ctx, what, func(ctx context.Context) (bool, error) {
		resp, err := client.GetGroupPolicy(ctx, &iam.GetGroupPolicyInput{
			GroupName:  aws.String(r.GroupName),
			PolicyName: aws.String(r.PolicyName),
		})
		if err != nil {
			if isNotFound(err) {
				if created {
					return false, nil
				}
				return false, runtime.ErrNotFound
			}
			return false, fmt.Errorf("get group policy: %w", err)
		}
		if resp == nil || resp.PolicyDocument == nil {
			if created {
				return false, nil
			}
			return false, runtime.ErrNotFound
		}
		document, err := decodeAndNormalizePolicy(aws.ToString(resp.PolicyDocument))
		if err != nil {
			return false, err
		}
		out = &GroupPolicyResourceOutput{
			GroupName:      r.outputGroupName(resp),
			PolicyName:     r.outputPolicyName(resp),
			PolicyDocument: document,
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *GroupPolicyResource) outputGroupName(resp *iam.GetGroupPolicyOutput) string {
	if resp.GroupName != nil {
		return aws.ToString(resp.GroupName)
	}
	return r.GroupName
}

func (r *GroupPolicyResource) outputPolicyName(resp *iam.GetGroupPolicyOutput) string {
	if resp.PolicyName != nil {
		return aws.ToString(resp.PolicyName)
	}
	return r.PolicyName
}

func decodeAndNormalizePolicy(encoded string) (string, error) {
	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		return "", fmt.Errorf("decode group policy document: %w", err)
	}
	document, err := normalizeIAMPolicyJSON(decoded)
	if err != nil {
		return "", fmt.Errorf("normalize group policy document: %w", err)
	}
	return document, nil
}

func normalizeIAMPolicyJSON(policy string) (string, error) {
	object, err := parseIAMPolicyJSON(policy)
	if err != nil {
		return "", err
	}
	if version, ok := object["Version"].(string); ok && version == "2012-10-17" {
		return marshalPolicyWithVersionFirst(object)
	}
	return marshalNormalizedJSON(object)
}

func parseIAMPolicyJSON(policy string) (map[string]any, error) {
	trimmed := strings.TrimSpace(policy)
	if trimmed == "" {
		return nil, errors.New("policy-document must not be empty")
	}
	value, err := decodeJSONNoDuplicateKeys(trimmed)
	if err != nil {
		return nil, fmt.Errorf("policy-document must be valid JSON without duplicate keys: %w", err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("policy-document must be a JSON object")
	}
	return object, nil
}

func marshalPolicyWithVersionFirst(object map[string]any) (string, error) {
	version, err := marshalNormalizedJSON(map[string]any{"Version": object["Version"]})
	if err != nil {
		return "", err
	}
	if len(object) == 1 {
		return version, nil
	}
	rest := make(map[string]any, len(object)-1)
	for key, value := range object {
		if key != "Version" {
			rest[key] = value
		}
	}
	restJSON, err := marshalNormalizedJSON(rest)
	if err != nil {
		return "", err
	}
	out := strings.TrimSuffix(version, "}") + "," + strings.TrimPrefix(restJSON, "{")
	if !json.Valid([]byte(out)) {
		return "", errors.New("normalized policy-document is not valid JSON")
	}
	return out, nil
}

func marshalNormalizedJSON(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", fmt.Errorf("normalize policy-document: %w", err)
	}
	out := strings.TrimSpace(buf.String())
	if !json.Valid([]byte(out)) {
		return "", errors.New("normalized policy-document is not valid JSON")
	}
	return out, nil
}

func decodeJSONNoDuplicateKeys(input string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(input))
	dec.UseNumber()
	value, err := decodeJSONValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return value, nil
}

func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch value := tok.(type) {
	case json.Delim:
		switch value {
		case '{':
			return decodeJSONObject(dec)
		case '[':
			return decodeJSONArray(dec)
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", value)
		}
	case string, bool, nil, json.Number:
		return value, nil
	default:
		return nil, fmt.Errorf("unexpected JSON token %T", tok)
	}
}

func decodeJSONObject(dec *json.Decoder) (map[string]any, error) {
	object := map[string]any{}
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("object key is %T", keyToken)
		}
		if _, exists := object[key]; exists {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		value, err := decodeJSONValue(dec)
		if err != nil {
			return nil, err
		}
		object[key] = value
	}
	end, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := end.(json.Delim); !ok || delim != '}' {
		return nil, fmt.Errorf("expected object end, got %v", end)
	}
	return object, nil
}

func decodeJSONArray(dec *json.Decoder) ([]any, error) {
	list := []any{}
	for dec.More() {
		value, err := decodeJSONValue(dec)
		if err != nil {
			return nil, err
		}
		list = append(list, value)
	}
	end, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := end.(json.Delim); !ok || delim != ']' {
		return nil, fmt.Errorf("expected array end, got %v", end)
	}
	return list, nil
}
