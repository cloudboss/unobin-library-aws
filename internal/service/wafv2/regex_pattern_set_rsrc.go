package wafv2

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	awssvc "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
)

type RegexPatternSetResource struct {
	Scope                 string             `ub:"scope"`
	Name                  *string            `ub:"name"`
	NamePrefix            *string            `ub:"name-prefix"`
	Description           *string            `ub:"description"`
	RegularExpressionList *[]RegexPattern    `ub:"regular-expression-list"`
	Tags                  *map[string]string `ub:"tags"`
}

type RegexPatternSetResourceOutput struct {
	ID                    string            `ub:"id"`
	ARN                   string            `ub:"arn"`
	LockToken             string            `ub:"lock-token"`
	Region                string            `ub:"region"`
	Scope                 string            `ub:"scope"`
	Name                  string            `ub:"name"`
	NamePrefix            *string           `ub:"name-prefix"`
	Description           *string           `ub:"description"`
	RegularExpressionList []RegexPattern    `ub:"regular-expression-list"`
	Tags                  map[string]string `ub:"tags"`
}

type regexPatternSetPrior = runtime.Prior[
	RegexPatternSetResource, *RegexPatternSetResourceOutput, *awsCfg,
]
type regexPatternSetDefinition = runtime.ResourceDefinition[
	RegexPatternSetResource, *RegexPatternSetResourceOutput, *awsCfg,
]

type regexPatternSetClient interface {
	wafTagClient
	CreateRegexPatternSet(context.Context, *awssvc.CreateRegexPatternSetInput,
		...func(*awssvc.Options)) (*awssvc.CreateRegexPatternSetOutput, error)
	GetRegexPatternSet(context.Context, *awssvc.GetRegexPatternSetInput,
		...func(*awssvc.Options)) (*awssvc.GetRegexPatternSetOutput, error)
	UpdateRegexPatternSet(context.Context, *awssvc.UpdateRegexPatternSetInput,
		...func(*awssvc.Options)) (*awssvc.UpdateRegexPatternSetOutput, error)
	DeleteRegexPatternSet(context.Context, *awssvc.DeleteRegexPatternSetInput,
		...func(*awssvc.Options)) (*awssvc.DeleteRegexPatternSetOutput, error)
}

type RegexPattern struct {
	RegexString string `ub:"regex-string"`
}

func (r RegexPatternSetResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.AtMostOneOf(r.Name, r.NamePrefix),
		constraint.Must(constraint.Expr("input.scope != null", constraint.Bind("scope", r.Scope))).
			Message("scope must not be null"),
		constraint.Must(constraint.OneOf(r.Scope, "REGIONAL", "CLOUDFRONT")),
		constraint.Must(constraint.IsMatch(r.Name, "^[0-9A-Za-z_-]{1,128}$")),
		constraint.Must(constraint.IsMatch(r.NamePrefix, "^[0-9A-Za-z_-]{1,102}$")),
		constraint.Must(constraint.Expr(
			`(@core.byte-length(input.description ?? '') >= 1 &&
			@core.byte-length(input.description ?? '') <= 256) || input.description == null`,
			constraint.Bind("description", r.Description),
		)).Message("description must be 1 to 256 bytes"),
		constraint.ForEach(r.RegularExpressionList, func(pattern RegexPattern) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.Expr(
					"input.value != null", constraint.Bind("value", pattern.RegexString),
				)).Message("regex-string must not be null"),
				constraint.Must(
					constraint.AtLeast(constraint.ByteLength(pattern.RegexString), 1),
					constraint.AtMost(constraint.ByteLength(pattern.RegexString), 200),
				).Message("regex-string must be 1 to 200 bytes"),
				constraint.Must(constraint.IsFormat(pattern.RegexString, constraint.FormatRegex)),
			}
		}),
	}
}

func (r *RegexPatternSetResource) ResourceDefinition() regexPatternSetDefinition {
	patterns := runtime.InputField(func(input *RegexPatternSetResource) **[]RegexPattern {
		return &input.RegularExpressionList
	})
	description := runtime.InputField(func(input *RegexPatternSetResource) **string {
		return &input.Description
	})
	tags := runtime.InputField(func(input *RegexPatternSetResource) **map[string]string {
		return &input.Tags
	})
	return regexPatternSetDefinition{
		SchemaVersion: 1,
		Equality: []runtime.InputEqualityRule[RegexPatternSetResource]{
			runtime.EqualBy(patterns, func(prior, desired *[]RegexPattern) bool {
				return regexPatternSetRegularExpressionListEqual(ptr.Value(prior), ptr.Value(desired))
			}),
			runtime.EqualBy(description, func(prior, desired *string) bool {
				return aws.ToString(prior) == aws.ToString(desired)
			}),
			runtime.EqualBy(tags, func(prior, desired *map[string]string) bool {
				return maps.Equal(wafUserTags(ptr.Value(prior)), wafUserTags(ptr.Value(desired)))
			}),
		},
		Replace: runtime.Replacement[RegexPatternSetResource, *RegexPatternSetResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[RegexPatternSetResource]{
				runtime.InputField(func(input *RegexPatternSetResource) **string { return &input.Name }),
				runtime.InputField(func(input *RegexPatternSetResource) **string { return &input.NamePrefix }),
				runtime.InputField(func(input *RegexPatternSetResource) *string { return &input.Scope }),
			},
			ConfigurationFields: []runtime.AnyConfigurationField[*awsCfg]{
				runtime.ConfigurationField(func(cfg *awsCfg) **string { return &cfg.Region }),
			},
		},
		StableID: func(_ RegexPatternSetResource, output *RegexPatternSetResourceOutput) (string, error) {
			if err := checkRegexPatternSetIdentity(output); err != nil {
				return "", err
			}
			return output.ARN, nil
		},
	}
}

func (r *RegexPatternSetResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*RegexPatternSetResourceOutput, error) {
	if r.Scope == string(awstypes.ScopeCloudfront) {
		cloudfront := ptr.Value(cfg)
		cloudfront.Region = aws.String("us-east-1")
		cfg = &cloudfront
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.createRegexPatternSet(ctx, client, client.Options().Region, rand.Reader)
}

func (r *RegexPatternSetResource) Read(
	ctx context.Context, cfg *awsCfg, prior regexPatternSetPrior,
) (*RegexPatternSetResourceOutput, error) {
	client, err := existingRegexPatternSetClient(
		ctx, regexPatternSetPriorConfig(cfg, prior), prior.Outputs,
	)
	if err != nil {
		return nil, err
	}
	return readRegexPatternSet(ctx, client, *prior.Outputs, true)
}

func (r *RegexPatternSetResource) Update(
	ctx context.Context, cfg *awsCfg, prior regexPatternSetPrior,
) (*RegexPatternSetResourceOutput, error) {
	client, err := existingRegexPatternSetClient(ctx, cfg, prior.Outputs)
	if err != nil {
		return nil, err
	}
	return r.updateRegexPatternSet(ctx, client, prior)
}

func (r *RegexPatternSetResource) Delete(
	ctx context.Context, cfg *awsCfg, prior regexPatternSetPrior,
) error {
	client, err := existingRegexPatternSetClient(
		ctx, regexPatternSetPriorConfig(cfg, prior), prior.Outputs,
	)
	if err != nil {
		return err
	}
	return deleteRegexPatternSet(ctx, client, prior)
}

func regexPatternSetPriorConfig(cfg *awsCfg, prior regexPatternSetPrior) *awsCfg {
	if prior.Configuration != nil {
		return prior.Configuration
	}
	return cfg
}

func existingRegexPatternSetClient(
	ctx context.Context, cfg *awsCfg, output *RegexPatternSetResourceOutput,
) (*awssvc.Client, error) {
	if err := checkRegexPatternSetIdentity(output); err != nil {
		return nil, err
	}
	saved := ptr.Value(cfg)
	saved.Region = aws.String(output.Region)
	return newClient(ctx, &saved)
}

func (r *RegexPatternSetResource) createRegexPatternSet(
	ctx context.Context, client regexPatternSetClient, region string, random io.Reader,
) (*RegexPatternSetResourceOutput, error) {
	name, err := r.regexPatternSetName(random)
	if err != nil {
		return nil, err
	}
	patterns, description, tags := r.prepareRegexPatternSet()
	input := &awssvc.CreateRegexPatternSetInput{
		Name:                  aws.String(name),
		Scope:                 awstypes.Scope(r.Scope),
		RegularExpressionList: patterns,
		Description:           description,
	}
	if len(tags) > 0 {
		input.Tags = expandTags(tags)
	}
	created, err := client.CreateRegexPatternSet(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("create regex pattern set %s: %w", name, err)
	}
	if created == nil || created.Summary == nil || aws.ToString(created.Summary.Id) == "" {
		return nil, fmt.Errorf("create regex pattern set %s: response has no regex pattern set ID", name)
	}
	identity := RegexPatternSetResourceOutput{
		ID: aws.ToString(created.Summary.Id), Name: name, Scope: r.Scope, Region: region,
		ARN: aws.ToString(created.Summary.ARN), LockToken: aws.ToString(created.Summary.LockToken),
	}
	output, err := loadRegexPatternSet(ctx, client, &identity, false)
	if err == nil {
		return output, nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	cleanupErr := cleanupRegexPatternSet(cleanup, client, identity)
	return nil, errors.Join(err, cleanupErr)
}

func (r *RegexPatternSetResource) regexPatternSetName(random io.Reader) (string, error) {
	name, err := resolveWAFName(r.Name, r.NamePrefix, random)
	if err != nil {
		return "", fmt.Errorf("generate regex pattern set name: %w", err)
	}
	return name, nil
}

func (r *RegexPatternSetResource) prepareRegexPatternSet() (
	[]awstypes.Regex, *string, map[string]string,
) {
	patterns := ptr.Value(r.RegularExpressionList)
	unique := make(map[string]bool, len(patterns))
	prepared := make([]awstypes.Regex, 0, len(patterns))
	for _, pattern := range patterns {
		if !unique[pattern.RegexString] {
			unique[pattern.RegexString] = true
			prepared = append(prepared, awstypes.Regex{RegexString: aws.String(pattern.RegexString)})
		}
	}
	return prepared, r.Description, wafUserTags(ptr.Value(r.Tags))
}

func regexPatternSetRegularExpressionListEqual(prior, desired []RegexPattern) bool {
	keys := func(patterns []RegexPattern) map[string]bool {
		result := make(map[string]bool, len(patterns))
		for _, pattern := range patterns {
			result[pattern.RegexString] = true
		}
		return result
	}
	return maps.Equal(keys(prior), keys(desired))
}

func readRegexPatternSet(
	ctx context.Context, client regexPatternSetClient,
	identity RegexPatternSetResourceOutput, established bool,
) (*RegexPatternSetResourceOutput, error) {
	return loadRegexPatternSet(ctx, client, &identity, established)
}

func loadRegexPatternSet(
	ctx context.Context, client regexPatternSetClient, identity *RegexPatternSetResourceOutput,
	established bool,
) (*RegexPatternSetResourceOutput, error) {
	response, err := client.GetRegexPatternSet(ctx, &awssvc.GetRegexPatternSetInput{
		Id: aws.String(identity.ID), Name: aws.String(identity.Name),
		Scope: awstypes.Scope(identity.Scope),
	})
	if err != nil {
		if established && isWebACLNotFound(err) {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get regex pattern set %s: %w", identity.ID, err)
	}
	if response == nil || response.RegexPatternSet == nil {
		if established {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("get regex pattern set %s: response has no regex pattern set", identity.ID)
	}
	set := response.RegexPatternSet
	if aws.ToString(set.Id) != identity.ID || aws.ToString(set.Name) != identity.Name ||
		(identity.ARN != "" && aws.ToString(set.ARN) != identity.ARN) {
		return nil, fmt.Errorf(
			"get regex pattern set %s: response identity does not match request", identity.ID,
		)
	}
	identity.ARN = aws.ToString(set.ARN)
	lockToken := aws.ToString(response.LockToken)
	if lockToken != "" {
		identity.LockToken = lockToken
	}
	if err := checkRegexPatternSetIdentity(identity); err != nil {
		return nil, fmt.Errorf("get regex pattern set %s: %w", identity.ID, err)
	}
	if lockToken == "" {
		return nil, fmt.Errorf("get regex pattern set %s: response has no lock token", identity.ID)
	}
	identity.NamePrefix = wafNamePrefix(identity.Name)
	identity.Description = set.Description
	identity.RegularExpressionList = []RegexPattern{}
	for _, pattern := range set.RegularExpressionList {
		identity.RegularExpressionList = append(identity.RegularExpressionList,
			RegexPattern{RegexString: aws.ToString(pattern.RegexString)})
	}
	slices.SortFunc(identity.RegularExpressionList, func(a, b RegexPattern) int {
		return strings.Compare(a.RegexString, b.RegexString)
	})
	identity.RegularExpressionList = slices.Compact(identity.RegularExpressionList)
	identity.Tags, err = listRegexPatternSetTags(ctx, client, *identity)
	if err != nil {
		return nil, err
	}
	return identity, nil
}

func checkRegexPatternSetIdentity(output *RegexPatternSetResourceOutput) error {
	if output == nil || output.ID == "" || output.Name == "" || output.Region == "" {
		return fmt.Errorf("regex pattern set identity requires ID, name, scope and region")
	}
	parsed, err := awsarn.Parse(output.ARN)
	if err != nil {
		return fmt.Errorf("regex pattern set identity has invalid ARN %q: %w", output.ARN, err)
	}
	scope := "regional"
	switch awstypes.Scope(output.Scope) {
	case awstypes.ScopeRegional:
	case awstypes.ScopeCloudfront:
		scope = "global"
	default:
		return fmt.Errorf("regex pattern set identity has invalid scope %q", output.Scope)
	}
	resource := scope + "/regexpatternset/" + output.Name + "/" + output.ID
	if parsed.Service != "wafv2" || parsed.Region != output.Region || parsed.Resource != resource {
		return fmt.Errorf("regex pattern set identity does not match ARN %q", output.ARN)
	}
	return nil
}

func regexPatternSetMutationState(
	prior regexPatternSetPrior,
) (*RegexPatternSetResourceOutput, error) {
	saved := prior.Outputs
	if err := checkRegexPatternSetIdentity(saved); err != nil {
		return nil, err
	}
	current := saved
	if observed := prior.Observed; observed != nil {
		if observed.ID != saved.ID || observed.ARN != saved.ARN || observed.Name != saved.Name ||
			observed.Scope != saved.Scope || observed.Region != saved.Region {
			return nil, fmt.Errorf(
				"regex pattern set %s observation identity does not match saved state", saved.ID,
			)
		}
		current = observed
	}
	if current.LockToken == "" {
		return nil, fmt.Errorf("regex pattern set %s has no lock token", saved.ID)
	}
	return current, nil
}

func (r *RegexPatternSetResource) updateRegexPatternSet(
	ctx context.Context, client regexPatternSetClient, prior regexPatternSetPrior,
) (*RegexPatternSetResourceOutput, error) {
	current, err := regexPatternSetMutationState(prior)
	if err != nil {
		return nil, err
	}
	patterns, description, tags := r.prepareRegexPatternSet()
	if err := syncRegexPatternSetTags(ctx, client, current, tags); err != nil {
		return nil, err
	}
	if !regexPatternSetRegularExpressionListEqual(current.RegularExpressionList,
		ptr.Value(r.RegularExpressionList)) ||
		aws.ToString(current.Description) != aws.ToString(description) {
		_, err := client.UpdateRegexPatternSet(ctx, &awssvc.UpdateRegexPatternSetInput{
			Id:                    aws.String(current.ID),
			Name:                  aws.String(current.Name),
			Scope:                 awstypes.Scope(current.Scope),
			LockToken:             aws.String(current.LockToken),
			Description:           description,
			RegularExpressionList: patterns,
		})
		if err != nil {
			return nil, fmt.Errorf("update regex pattern set %s: %w", current.ID, err)
		}
	}
	return readRegexPatternSet(ctx, client, *current, false)
}

type regexPatternSetClock struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func deleteRegexPatternSet(
	ctx context.Context, client regexPatternSetClient, prior regexPatternSetPrior,
	clocks ...regexPatternSetClock,
) error {
	current, err := regexPatternSetMutationState(prior)
	if err != nil {
		return err
	}
	return deleteRegexPatternSetIdentity(ctx, client, *current, clocks...)
}

func deleteRegexPatternSetIdentity(
	ctx context.Context, client regexPatternSetClient, identity RegexPatternSetResourceOutput,
	clocks ...regexPatternSetClock,
) error {
	clock := regexPatternSetClock{now: time.Now, sleep: func(
		ctx context.Context, d time.Duration,
	) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}}
	if len(clocks) > 0 {
		clock = clocks[0]
	}
	deadline := clock.now().Add(5 * time.Minute)
	delay := 500 * time.Millisecond
	final := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := client.DeleteRegexPatternSet(ctx, &awssvc.DeleteRegexPatternSetInput{
			Id: aws.String(identity.ID), Name: aws.String(identity.Name),
			Scope: awstypes.Scope(identity.Scope), LockToken: aws.String(identity.LockToken),
		})
		if err == nil || isWebACLNotFound(err) {
			return nil
		}
		if !isWebACLAssociated(err) || final {
			return fmt.Errorf("delete regex pattern set %s: %w", identity.ID, err)
		}
		if !clock.now().Before(deadline) {
			final = true
		}
		if err := clock.sleep(ctx, delay); err != nil {
			return err
		}
		delay = min(delay*2, 10*time.Second)
	}
}

func cleanupRegexPatternSet(
	ctx context.Context, client regexPatternSetClient, identity RegexPatternSetResourceOutput,
) error {
	if identity.LockToken == "" {
		response, err := client.GetRegexPatternSet(ctx, &awssvc.GetRegexPatternSetInput{
			Id: aws.String(identity.ID), Name: aws.String(identity.Name),
			Scope: awstypes.Scope(identity.Scope),
		})
		if isWebACLNotFound(err) || (err == nil &&
			(response == nil || response.RegexPatternSet == nil)) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get cleanup token for regex pattern set %s: %w", identity.ID, err)
		}
		identity.LockToken = aws.ToString(response.LockToken)
		if identity.LockToken == "" {
			return fmt.Errorf("cleanup regex pattern set %s: missing lock token", identity.ID)
		}
	}
	return deleteRegexPatternSetIdentity(ctx, client, identity)
}

func listRegexPatternSetTags(
	ctx context.Context, client regexPatternSetClient, identity RegexPatternSetResourceOutput,
) (map[string]string, error) {
	return listWAFTags(ctx, client, identity.ARN, identity.Region, "regex pattern set")
}

func syncRegexPatternSetTags(
	ctx context.Context, client regexPatternSetClient,
	current *RegexPatternSetResourceOutput, desired map[string]string,
) error {
	return syncWAFTags(
		ctx, client, current.ARN, current.Region, "regex pattern set", current.Tags, desired,
	)
}
