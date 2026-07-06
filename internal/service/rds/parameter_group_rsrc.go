package rds

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	rds "github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

// maxParamChunk is the largest number of parameters RDS accepts in one
// ModifyDBParameterGroup or ResetDBParameterGroup call.
const maxParamChunk = 20

// parameterDeleteTimeout bounds the retry on a delete that races with the group
// still settling out of an attached state.
const parameterDeleteTimeout = 3 * time.Minute

// parameterDependencyBins are the groups of parameters RDS will only accept
// together: a modify that changes one member of a pair without the other is
// rejected, so each bin travels in the same call. Names are compared
// lowercased, since every parameter name is lowercased before it is sent.
var parameterDependencyBins = [][]string{
	{"collation_server", "character_set_server"},
	{"gtid-mode", "enforce_gtid_consistency"},
	{"password_encryption", "rds.accepted_password_auth_method"},
	{"ssl_max_protocol_version", "ssl_min_protocol_version"},
	{"rds.change_data_capture_streaming", "binlog_format"},
	{"aurora_enhanced_binlog", "binlog_backup", "binlog_replication_globaldb"},
}

// ParameterGroupResource manages an RDS DB parameter group: a named, family-scoped set
// of engine parameters a DB instance can reference. The group itself is made by
// CreateDBParameterGroup from its name, family, and description; those three are
// fixed once the group exists, so a change to any of them replaces the group.
// The parameter set is a separate concern reconciled after the group exists,
// through ModifyDBParameterGroup for added or changed parameters and
// ResetDBParameterGroup for removed ones, so it is a field that updates in
// place. Tags are reconciled as a set.
type ParameterGroupResource struct {
	Name        string                     `ub:"name"`
	Family      string                     `ub:"family"`
	Description string                     `ub:"description"`
	Parameters  *[]ParameterGroupParameter `ub:"parameters"`
	Tags        *map[string]string         `ub:"tags"`
}

// ParameterGroupParameter is one engine parameter in the group's parameter set.
// The name selects the parameter, the value sets it, and the apply method
// chooses when the change takes effect. RDS stores parameter names in lowercase
// and treats the apply method case-insensitively, so the name and apply method
// are lowercased before they are sent and when they are compared; the value is
// sent verbatim. An omitted apply method defaults to immediate.
type ParameterGroupParameter struct {
	Name        string  `ub:"name"`
	Value       string  `ub:"value"`
	ApplyMethod *string `ub:"apply-method"`
}

// ParameterGroupResourceOutput holds the value RDS computes for the group. The ARN is
// the group's identity for tagging; downstream resources reference the group by
// its name, which is an input.
type ParameterGroupResourceOutput struct {
	Arn string `ub:"arn"`
}

func (r *ParameterGroupResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs RDS fixes when the group is created. The name,
// family, and description cannot be changed on an existing group, so a change to
// any of them requires a new group. The parameter set and tags reconcile in
// place.
func (r *ParameterGroupResource) ReplaceFields() []string {
	return []string{"name", "family", "description"}
}

// Constraints declares the per-parameter rule RDS places on the apply method:
// when given it must be immediate or pending-reboot. The comparison is
// case-insensitive in RDS and the value is lowercased before it is sent, so the
// rule is stated in lowercase. The name rules (a leading letter, lowercase
// alphanumerics with periods and hyphens, no doubled hyphen, no trailing
// hyphen, at most 255 characters) are a pattern the constraint layer cannot
// derive, so they are checked in Create against the requested name.
func (r ParameterGroupResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(r.Parameters, func(p ParameterGroupParameter) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.When(constraint.Present(p.ApplyMethod)).
					Require(constraint.OneOf(p.ApplyMethod, "immediate", "pending-reboot")).
					Message("apply-method must be immediate or pending-reboot"),
			}
		}),
	}
}

func (r *ParameterGroupResource) Create(
	ctx context.Context,
	cfg *awsCfg,
) (*ParameterGroupResourceOutput, error) {
	if err := validateParameterGroupName(r.Name); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_, err = client.CreateDBParameterGroup(ctx, &rds.CreateDBParameterGroupInput{
		DBParameterGroupName:   aws.String(r.Name),
		DBParameterGroupFamily: aws.String(r.Family),
		Description:            aws.String(r.Description),
		Tags:                   tagList(ptr.Value(r.Tags)),
	})
	if err != nil {
		return nil, fmt.Errorf("create db parameter group: %w", err)
	}
	// CreateDBParameterGroup never takes parameters; the full declared set is
	// pushed through the same modify path Update uses, with an empty prior set.
	if err := r.reconcileParameters(ctx, client, nil); err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

func (r *ParameterGroupResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *ParameterGroupResourceOutput,
) (*ParameterGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the group by name and returns its computed ARN. RDS is
// eventually consistent right after create, so a describe can return a stale
// page; an explicit name-equality check rejects a result whose name does not
// match the request, and a missing group reads as runtime.ErrNotFound so a plan
// recreates it. The parameter list is not read back: the parameter set
// reconciles from the prior-versus-current input diff, so the live list is not
// part of the output. (The user-versus-engine-default source filter that
// Terraform applies when persisting the parameter list does not apply here,
// since the list is never persisted.)
func (r *ParameterGroupResource) read(
	ctx context.Context, client *rds.Client,
) (*ParameterGroupResourceOutput, error) {
	pager := rds.NewDescribeDBParameterGroupsPaginator(client,
		&rds.DescribeDBParameterGroupsInput{DBParameterGroupName: aws.String(r.Name)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isParameterGroupNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe db parameter groups: %w", err)
		}
		for _, g := range page.DBParameterGroups {
			if aws.ToString(g.DBParameterGroupName) != r.Name {
				continue
			}
			return &ParameterGroupResourceOutput{Arn: aws.ToString(g.DBParameterGroupArn)}, nil
		}
	}
	return nil, runtime.ErrNotFound
}

func (r *ParameterGroupResource) Update(
	ctx context.Context,
	cfg *awsCfg,
	prior runtime.Prior[ParameterGroupResource, *ParameterGroupResourceOutput],
) (*ParameterGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The parameter set reconciles only when it changed: re-applying identical
	// parameters issues no modify and no reset.
	if runtime.Changed(prior.Inputs.Parameters, r.Parameters) {
		if err := r.reconcileParameters(ctx, client, ptr.Value(prior.Inputs.Parameters)); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		arn := prior.Outputs.Arn
		if err := syncTags(ctx, client, arn, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *ParameterGroupResource) Delete(
	ctx context.Context,
	cfg *awsCfg,
	prior *ParameterGroupResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// A group still attached to a settling DB instance reports an invalid-state
	// fault until it detaches; that window clears on its own, so the delete
	// retries through it up to a bounded timeout.
	err = retry.OnError(ctx, isParameterGroupStateFault, func(ctx context.Context) error {
		_, err := client.DeleteDBParameterGroup(ctx, &rds.DeleteDBParameterGroupInput{
			DBParameterGroupName: aws.String(r.Name),
		})
		return err
	}, retry.WithTimeout(parameterDeleteTimeout))
	if err != nil {
		// A group already gone, deleted by an earlier run, counts as deleted.
		if isParameterGroupNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete db parameter group: %w", err)
	}
	return nil
}

// reconcileParameters brings the group's parameter set from prior to current.
// Parameters added or changed since prior are pushed with ModifyDBParameterGroup
// in dependency-aware chunks; parameters present in prior but absent now are
// reset to their engine default with ResetDBParameterGroup. A parameter still in
// the desired set is never reset, and the whole set is never reset at once.
func (r *ParameterGroupResource) reconcileParameters(
	ctx context.Context, client *rds.Client, prior []ParameterGroupParameter,
) error {
	priorByName := make(map[string]ParameterGroupParameter, len(prior))
	for _, p := range prior {
		priorByName[strings.ToLower(p.Name)] = p
	}
	currentNames := make(map[string]struct{}, len(ptr.Value(r.Parameters)))
	var modify []rdstypes.Parameter
	for _, p := range ptr.Value(r.Parameters) {
		name := strings.ToLower(p.Name)
		currentNames[name] = struct{}{}
		old, existed := priorByName[name]
		// A parameter is pushed when it is new or its value or apply method
		// differs from the prior declaration; an unchanged parameter is skipped.
		if existed && old.Value == p.Value &&
			applyMethod(old.ApplyMethod) == applyMethod(p.ApplyMethod) {
			continue
		}
		modify = append(modify, rdstypes.Parameter{
			ParameterName:  aws.String(name),
			ParameterValue: aws.String(p.Value),
			ApplyMethod:    rdstypes.ApplyMethod(applyMethod(p.ApplyMethod)),
		})
	}
	for _, chunk := range parameterChunksForModify(modify) {
		_, err := client.ModifyDBParameterGroup(ctx, &rds.ModifyDBParameterGroupInput{
			DBParameterGroupName: aws.String(r.Name),
			Parameters:           chunk,
		})
		if err != nil {
			return fmt.Errorf("modify db parameter group: %w", err)
		}
	}
	var remove []rdstypes.Parameter
	for _, p := range prior {
		name := strings.ToLower(p.Name)
		if _, kept := currentNames[name]; kept {
			continue
		}
		remove = append(remove, rdstypes.Parameter{
			ParameterName: aws.String(name),
			ApplyMethod:   rdstypes.ApplyMethod(applyMethod(p.ApplyMethod)),
		})
	}
	for _, chunk := range slices.Collect(slices.Chunk(remove, maxParamChunk)) {
		_, err := client.ResetDBParameterGroup(ctx, &rds.ResetDBParameterGroupInput{
			DBParameterGroupName: aws.String(r.Name),
			ResetAllParameters:   aws.Bool(false),
			Parameters:           chunk,
		})
		if err != nil {
			return fmt.Errorf("reset db parameter group: %w", err)
		}
	}
	return nil
}

// parameterChunksForModify orders the modify set before chunking it. RDS rejects
// a call that changes one member of a dependency pair without the other and will
// not mix the immediate and pending-reboot apply methods arbitrarily, so the
// parameters are grouped: each dependency bin first, in a fixed order; then the
// remaining immediate parameters; then the remaining pending-reboot parameters.
// Every group is split into chunks of at most maxParamChunk.
func parameterChunksForModify(params []rdstypes.Parameter) [][]rdstypes.Parameter {
	binned := make(map[int][]rdstypes.Parameter)
	var immediate, pending []rdstypes.Parameter
	for _, p := range params {
		name := aws.ToString(p.ParameterName)
		if bin, ok := dependencyBin(name); ok {
			binned[bin] = append(binned[bin], p)
			continue
		}
		// An unset apply method defaults to immediate, so only an explicit
		// pending-reboot is treated as pending.
		if p.ApplyMethod == rdstypes.ApplyMethodPendingReboot {
			pending = append(pending, p)
		} else {
			immediate = append(immediate, p)
		}
	}
	var chunks [][]rdstypes.Parameter
	for bin := range parameterDependencyBins {
		group := binned[bin]
		if len(group) == 0 {
			continue
		}
		chunks = append(chunks, slices.Collect(slices.Chunk(group, maxParamChunk))...)
	}
	if len(immediate) > 0 {
		chunks = append(chunks, slices.Collect(slices.Chunk(immediate, maxParamChunk))...)
	}
	if len(pending) > 0 {
		chunks = append(chunks, slices.Collect(slices.Chunk(pending, maxParamChunk))...)
	}
	return chunks
}

// dependencyBin reports the index of the dependency bin a lowercased parameter
// name belongs to, and whether it belongs to one at all.
func dependencyBin(name string) (int, bool) {
	for i, bin := range parameterDependencyBins {
		if slices.Contains(bin, name) {
			return i, true
		}
	}
	return 0, false
}

// applyMethod returns the lowercased apply method for a parameter, defaulting an
// unset method to immediate. RDS compares the apply method case-insensitively
// and stores it lowercased, so the value is normalized before it is sent or
// compared.
func applyMethod(method *string) string {
	if method == nil || *method == "" {
		return string(rdstypes.ApplyMethodImmediate)
	}
	return strings.ToLower(*method)
}

// validateParameterGroupName checks a requested group name against the rules
// RDS enforces, which the constraint layer cannot express as a pattern: at
// most 255 characters, a leading letter, only lowercase letters, digits,
// periods, and hyphens, no doubled hyphen, and no trailing hyphen.
func validateParameterGroupName(name string) error {
	if len(name) > 255 {
		return fmt.Errorf("name must be at most 255 characters")
	}
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return fmt.Errorf("name must start with a lowercase letter")
	}
	for i := range len(name) {
		c := name[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if !lower && !digit && c != '.' && c != '-' {
			return fmt.Errorf(
				"name must contain only lowercase letters, digits, periods, and hyphens")
		}
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("name must not contain two consecutive hyphens")
	}
	if strings.HasSuffix(name, "-") {
		return fmt.Errorf("name must not end with a hyphen")
	}
	return nil
}

// isParameterGroupNotFound reports whether err is the typed fault RDS raises
// when the named parameter group does not exist.
func isParameterGroupNotFound(err error) bool {
	var fault *rdstypes.DBParameterGroupNotFoundFault
	return errors.As(err, &fault)
}

// isParameterGroupStateFault reports whether err is the typed fault RDS raises
// when the group is in a state that blocks the operation, such as a delete while
// the group is still attached.
func isParameterGroupStateFault(err error) bool {
	var fault *rdstypes.InvalidDBParameterGroupStateFault
	return errors.As(err, &fault)
}
