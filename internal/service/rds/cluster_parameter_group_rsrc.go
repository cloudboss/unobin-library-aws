package rds

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/runtime"

	"github.com/cloudboss/unobin-library-aws/internal/ptr"
	"github.com/cloudboss/unobin-library-aws/internal/retry"
)

// clusterParameterChunkSize is the largest number of parameters RDS accepts in
// one ModifyDBClusterParameterGroup or ResetDBClusterParameterGroup call, so a
// larger change is split across several calls.
const clusterParameterChunkSize = 20

// clusterParameterStateRetryTimeout bounds how long a reset of removed
// parameters retries while the group reports pending changes from the modify
// that preceded it, and how long a delete retries while the group is in an
// invalid state.
const clusterParameterStateRetryTimeout = 3 * time.Minute

// clusterParameterDependencyGroups lists the sets of parameters RDS validates
// together: a change to one is rejected unless its partners ride the same
// ModifyDBClusterParameterGroup call. Each set is sent as a single chunk ahead
// of the rest, so a co-dependent set is never split across two calls. Names are
// the lowercased forms RDS stores.
var clusterParameterDependencyGroups = [][]string{
	{"collation_server", "character_set_server"},
	{"gtid-mode", "enforce_gtid_consistency"},
	{"password_encryption", "rds.accepted_password_auth_method"},
	{"ssl_max_protocol_version", "ssl_min_protocol_version"},
	{"rds.change_data_capture_streaming", "binlog_format"},
	{"aurora_enhanced_binlog", "binlog_backup", "binlog_replication_globaldb"},
}

// ClusterParameterGroupResource manages an RDS DB cluster parameter group: a named set
// of engine settings a DB cluster references. The name, family, and description
// are fixed when the group is created, so a change to any of them replaces the
// group; the parameter set and the tags reconcile in place. The parameter set
// is a declared set: a parameter listed here is applied, and a parameter
// removed from the list is reset to its engine default, so the group holds only
// the parameters the configuration names.
type ClusterParameterGroupResource struct {
	Name        string                            `ub:"name"`
	Family      string                            `ub:"family"`
	Description string                            `ub:"description"`
	Parameters  *[]ClusterParameterGroupParameter `ub:"parameters"`
	Tags        *map[string]string                `ub:"tags"`
}

// ClusterParameterGroupParameter is one engine setting in a cluster parameter
// group. Name and value identify the setting and its value; apply-method
// chooses when the change takes effect and defaults to immediate when unset.
// RDS stores parameter names and apply methods lowercased, so both are
// lowercased before they are sent, while the value is sent verbatim.
type ClusterParameterGroupParameter struct {
	Name        string  `ub:"name"`
	Value       string  `ub:"value"`
	ApplyMethod *string `ub:"apply-method"`
}

// ClusterParameterGroupResourceOutput holds the value RDS computes for the group. The
// ARN is the group's identity in tag operations and the handle downstream
// resources reference.
type ClusterParameterGroupResourceOutput struct {
	Arn string `ub:"arn"`
}

func (r *ClusterParameterGroupResource) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs RDS fixes when a cluster parameter group is
// created. The name, family, and description cannot be changed on an existing
// group, so a change to any of them requires a new group. The parameter set and
// the tags reconcile in place by Update.
func (r *ClusterParameterGroupResource) ReplaceFields() []string {
	return []string{"name", "family", "description"}
}

// Constraints declares the rules RDS places on a group's inputs. A parameter's
// apply-method, when set, is one of immediate or pending-reboot. The name rules
// (a leading letter, lowercase alphanumerics with periods and hyphens, no
// doubled hyphen, no trailing hyphen, at most 255 characters) are a pattern the
// constraint layer cannot derive, so they are checked in Create against the
// requested name.
func (r ClusterParameterGroupResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(r.Parameters, func(p ClusterParameterGroupParameter) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.When(constraint.Present(p.ApplyMethod)).
					Require(constraint.OneOf(p.ApplyMethod, "immediate", "pending-reboot")).
					Message("apply-method must be immediate or pending-reboot"),
			}
		}),
	}
}

func (r *ClusterParameterGroupResource) Create(
	ctx context.Context, cfg *awsCfg,
) (*ClusterParameterGroupResourceOutput, error) {
	if err := validateClusterParameterGroupName(r.Name); err != nil {
		return nil, err
	}
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &rds.CreateDBClusterParameterGroupInput{
		DBClusterParameterGroupName: aws.String(r.Name),
		DBParameterGroupFamily:      aws.String(r.Family),
		Description:                 aws.String(r.Description),
		Tags:                        tagList(ptr.Value(r.Tags)),
	}
	resp, err := client.CreateDBClusterParameterGroup(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("create db cluster parameter group: %w", err)
	}
	arn := aws.ToString(resp.DBClusterParameterGroup.DBClusterParameterGroupArn)
	// CreateDBClusterParameterGroup takes no parameters, so the whole declared
	// set is applied by the same modify-and-reset path Update uses, with no
	// prior parameters: every declared parameter is applied and nothing is
	// reset.
	if err := r.reconcileParameters(ctx, client, nil); err != nil {
		return nil, err
	}
	return &ClusterParameterGroupResourceOutput{Arn: arn}, nil
}

func (r *ClusterParameterGroupResource) Read(
	ctx context.Context,
	cfg *awsCfg,
	prior *ClusterParameterGroupResourceOutput,
) (*ClusterParameterGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read finds the group by name and returns its ARN. It describes the group
// only: the parameter set is reconciled from the declared inputs, not read
// back, so the read needs no parameter describe. A not-found fault, an empty
// result, or a result whose name does not match the requested name maps to
// runtime.ErrNotFound. The name check guards a stale describe right after
// create, when RDS may answer with a different group before the new one is
// visible.
func (r *ClusterParameterGroupResource) read(
	ctx context.Context, client *rds.Client,
) (*ClusterParameterGroupResourceOutput, error) {
	in := &rds.DescribeDBClusterParameterGroupsInput{
		DBClusterParameterGroupName: aws.String(r.Name),
	}
	var groups []rdstypes.DBClusterParameterGroup
	pager := rds.NewDescribeDBClusterParameterGroupsPaginator(client, in)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isClusterParameterGroupNotFound(err) {
				return nil, runtime.ErrNotFound
			}
			return nil, fmt.Errorf("describe db cluster parameter groups: %w", err)
		}
		groups = append(groups, page.DBClusterParameterGroups...)
	}
	if len(groups) != 1 {
		return nil, runtime.ErrNotFound
	}
	group := groups[0]
	if aws.ToString(group.DBClusterParameterGroupName) != r.Name {
		return nil, runtime.ErrNotFound
	}
	return &ClusterParameterGroupResourceOutput{
		Arn: aws.ToString(group.DBClusterParameterGroupArn),
	}, nil
}

func (r *ClusterParameterGroupResource) Update(
	ctx context.Context, cfg *awsCfg,
	prior runtime.Prior[ClusterParameterGroupResource, *ClusterParameterGroupResourceOutput],
) (*ClusterParameterGroupResourceOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	arn := prior.Outputs.Arn
	// The parameter set reconciles from the input diff: parameters added or
	// changed since the last apply are modified, parameters removed from the
	// declaration are reset. The whole reconcile runs only when the declared
	// parameters changed.
	if runtime.Changed(prior.Inputs.Parameters, r.Parameters) {
		if err := r.reconcileParameters(ctx, client, ptr.Value(prior.Inputs.Parameters)); err != nil {
			return nil, err
		}
	}
	if runtime.Changed(ptr.Value(prior.Inputs.Tags), ptr.Value(r.Tags)) {
		if err := syncTags(ctx, client, arn, ptr.Value(r.Tags)); err != nil {
			return nil, err
		}
	}
	return r.read(ctx, client)
}

func (r *ClusterParameterGroupResource) Delete(
	ctx context.Context, cfg *awsCfg, prior *ClusterParameterGroupResourceOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	in := &rds.DeleteDBClusterParameterGroupInput{
		DBClusterParameterGroupName: aws.String(r.Name),
	}
	// A group still attached to a cluster, or one a recent modify left in an
	// invalid state, rejects the delete until it settles, so the delete retries
	// through that state over a bounded window. A group already gone counts as
	// deleted.
	err = retry.OnError(ctx, isClusterParameterGroupInvalidState, func(ctx context.Context) error {
		_, err := client.DeleteDBClusterParameterGroup(ctx, in)
		return err
	}, retry.WithTimeout(clusterParameterStateRetryTimeout))
	if err != nil {
		if isClusterParameterGroupNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete db cluster parameter group: %w", err)
	}
	return nil
}

// reconcileParameters brings the group's parameters to the declared set given
// the parameters declared on the previous apply. Parameters added or changed
// relative to prior are applied with ModifyDBClusterParameterGroup; parameters
// whose names were declared before and are no longer declared are reset to
// their engine defaults with ResetDBClusterParameterGroup. On create prior is
// nil, so every declared parameter is applied and nothing is reset.
func (r *ClusterParameterGroupResource) reconcileParameters(
	ctx context.Context, client *rds.Client, prior []ClusterParameterGroupParameter,
) error {
	desired := expandClusterParameters(ptr.Value(r.Parameters))
	before := expandClusterParameters(prior)
	changed := changedClusterParameters(before, desired)
	if err := r.modifyParameters(ctx, client, changed); err != nil {
		return err
	}
	removed := removedClusterParameterNames(before, desired)
	if err := r.resetParameters(ctx, client, removed); err != nil {
		return err
	}
	return nil
}

// modifyParameters applies the given parameters in dependency-aware chunks. The
// co-dependent sets are sent first, each as a single chunk, then the remaining
// immediate parameters, then the remaining pending-reboot parameters, with each
// bin split into chunks of at most twenty.
func (r *ClusterParameterGroupResource) modifyParameters(
	ctx context.Context, client *rds.Client, params []rdstypes.Parameter,
) error {
	for _, chunk := range clusterParameterModifyChunks(params) {
		in := &rds.ModifyDBClusterParameterGroupInput{
			DBClusterParameterGroupName: aws.String(r.Name),
			Parameters:                  chunk,
		}
		if _, err := client.ModifyDBClusterParameterGroup(ctx, in); err != nil {
			return fmt.Errorf("modify db cluster parameter group: %w", err)
		}
	}
	return nil
}

// resetParameters resets the named parameters to their engine defaults in
// chunks of at most twenty. A reset is rejected while the group still has
// pending changes from the preceding modify, so each call retries through that
// state over a bounded window. The state fault is retried only when its message
// reports pending changes; a state fault for any other reason is returned at
// once.
func (r *ClusterParameterGroupResource) resetParameters(
	ctx context.Context, client *rds.Client, names []string,
) error {
	for chunk := range slices.Chunk(names, clusterParameterChunkSize) {
		in := &rds.ResetDBClusterParameterGroupInput{
			DBClusterParameterGroupName: aws.String(r.Name),
			ResetAllParameters:          aws.Bool(false),
			Parameters:                  clusterParameterResetList(chunk),
		}
		err := retry.OnError(ctx, isClusterParameterGroupPendingChanges,
			func(ctx context.Context) error {
				_, err := client.ResetDBClusterParameterGroup(ctx, in)
				return err
			}, retry.WithTimeout(clusterParameterStateRetryTimeout))
		if err != nil {
			return fmt.Errorf("reset db cluster parameter group: %w", err)
		}
	}
	return nil
}

// expandClusterParameters converts declared parameters into the RDS SDK
// parameter list, lowercasing each name and apply-method as RDS stores them and
// defaulting an unset apply-method to immediate. A parameter with an empty name
// is skipped. The value is sent verbatim.
func expandClusterParameters(params []ClusterParameterGroupParameter) []rdstypes.Parameter {
	out := make([]rdstypes.Parameter, 0, len(params))
	for _, p := range params {
		name := strings.ToLower(p.Name)
		if name == "" {
			continue
		}
		applyMethod := rdstypes.ApplyMethodImmediate
		if p.ApplyMethod != nil {
			applyMethod = rdstypes.ApplyMethod(strings.ToLower(*p.ApplyMethod))
		}
		out = append(out, rdstypes.Parameter{
			ParameterName:  aws.String(name),
			ParameterValue: aws.String(p.Value),
			ApplyMethod:    applyMethod,
		})
	}
	return out
}

// changedClusterParameters returns the desired parameters that are new or
// differ from the prior set. A parameter matches a prior one when its name,
// value, and apply-method are all equal, so a change to a value or an
// apply-method puts the parameter in the modify set.
func changedClusterParameters(before, desired []rdstypes.Parameter) []rdstypes.Parameter {
	priorByName := make(map[string]rdstypes.Parameter, len(before))
	for _, p := range before {
		priorByName[aws.ToString(p.ParameterName)] = p
	}
	changed := make([]rdstypes.Parameter, 0, len(desired))
	for _, p := range desired {
		old, ok := priorByName[aws.ToString(p.ParameterName)]
		if ok && aws.ToString(old.ParameterValue) == aws.ToString(p.ParameterValue) &&
			old.ApplyMethod == p.ApplyMethod {
			continue
		}
		changed = append(changed, p)
	}
	return changed
}

// removedClusterParameterNames returns the names declared before that are no
// longer declared, sorted for a deterministic request order. These are reset to
// their engine defaults. The diff is by name only: a changed value or
// apply-method is handled by the modify set, not a reset.
func removedClusterParameterNames(before, desired []rdstypes.Parameter) []string {
	desiredNames := make(map[string]struct{}, len(desired))
	for _, p := range desired {
		desiredNames[aws.ToString(p.ParameterName)] = struct{}{}
	}
	var removed []string
	for _, p := range before {
		name := aws.ToString(p.ParameterName)
		if _, ok := desiredNames[name]; !ok {
			removed = append(removed, name)
		}
	}
	slices.Sort(removed)
	return removed
}

// clusterParameterModifyChunks orders the parameters into modify calls: the
// co-dependent sets first, each as its own chunk, then the remaining immediate
// parameters, then the remaining pending-reboot parameters, with the two
// remainder bins split into chunks of at most twenty. A co-dependent set never
// splits across two chunks, since RDS validates its members together.
func clusterParameterModifyChunks(params []rdstypes.Parameter) [][]rdstypes.Parameter {
	byName := make(map[string]rdstypes.Parameter, len(params))
	order := make([]string, 0, len(params))
	for _, p := range params {
		name := aws.ToString(p.ParameterName)
		byName[name] = p
		order = append(order, name)
	}
	taken := make(map[string]struct{}, len(params))
	var chunks [][]rdstypes.Parameter
	for _, group := range clusterParameterDependencyGroups {
		var chunk []rdstypes.Parameter
		for _, name := range group {
			if p, ok := byName[name]; ok {
				chunk = append(chunk, p)
				taken[name] = struct{}{}
			}
		}
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}
	}
	var immediate, pendingReboot []rdstypes.Parameter
	for _, name := range order {
		if _, ok := taken[name]; ok {
			continue
		}
		p := byName[name]
		if p.ApplyMethod == rdstypes.ApplyMethodPendingReboot {
			pendingReboot = append(pendingReboot, p)
		} else {
			immediate = append(immediate, p)
		}
	}
	if len(immediate) > 0 {
		chunks = append(chunks,
			slices.Collect(slices.Chunk(immediate, clusterParameterChunkSize))...)
	}
	if len(pendingReboot) > 0 {
		chunks = append(chunks,
			slices.Collect(slices.Chunk(pendingReboot, clusterParameterChunkSize))...)
	}
	return chunks
}

// clusterParameterResetList builds the parameter list a reset call takes from a
// set of names. A reset identifies parameters by name, so only the name is set.
func clusterParameterResetList(names []string) []rdstypes.Parameter {
	out := make([]rdstypes.Parameter, 0, len(names))
	for _, name := range names {
		out = append(out, rdstypes.Parameter{ParameterName: aws.String(name)})
	}
	return out
}

// validateClusterParameterGroupName checks a requested group name against the
// rules RDS enforces, which the constraint layer cannot express as a pattern:
// at most 255 characters, a leading letter, only lowercase letters, digits,
// periods, and hyphens, no doubled hyphen, and no trailing hyphen.
func validateClusterParameterGroupName(name string) error {
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

// isClusterParameterGroupNotFound reports whether err is the fault RDS raises
// when a DB cluster parameter group does not exist. Read maps it to a not-found
// so a plan recreates the group, and Delete treats it as success.
func isClusterParameterGroupNotFound(err error) bool {
	var notFound *rdstypes.DBParameterGroupNotFoundFault
	return errors.As(err, &notFound)
}

// isClusterParameterGroupInvalidState reports whether err is the fault RDS
// raises when a group is in use or otherwise in an invalid state. Delete
// retries through it while the group settles, regardless of the message.
func isClusterParameterGroupInvalidState(err error) bool {
	var invalid *rdstypes.InvalidDBParameterGroupStateFault
	return errors.As(err, &invalid)
}

// isClusterParameterGroupPendingChanges reports whether err is the invalid-state
// fault whose message says the group has pending changes. A reset of removed
// parameters retries only through this case, since a modify just before it can
// leave the group briefly pending; an invalid-state fault for any other reason
// is not retried here.
func isClusterParameterGroupPendingChanges(err error) bool {
	var invalid *rdstypes.InvalidDBParameterGroupStateFault
	if !errors.As(err, &invalid) {
		return false
	}
	return strings.Contains(invalid.ErrorMessage(), "has pending changes")
}
