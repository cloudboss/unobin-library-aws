package eks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

var addonVersionPattern = regexp.MustCompile(
	`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)` +
		`(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?` +
		`(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`,
)

var addonNamespacePattern = regexp.MustCompile(
	`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`,
)

func (r *AddonResource) validateAddonInputs(context.Context, *awsCfg) error {
	if len(r.ClusterName) < 1 || len(r.ClusterName) > 100 ||
		!clusterNamePattern.MatchString(r.ClusterName) {
		return fmt.Errorf("cluster-name must match %s and contain 1 to 100 characters",
			clusterNamePattern.String())
	}
	if r.AddonName == "" {
		return errors.New("addon-name must not be empty")
	}
	if r.AddonVersion != nil && !addonVersionPattern.MatchString(*r.AddonVersion) {
		return errors.New("addon-version must be a v-prefixed semantic version")
	}
	if r.ConfigurationValues != nil && *r.ConfigurationValues == "" {
		return errors.New("configuration-values must not be empty when configured")
	}
	if r.NamespaceConfig != nil &&
		!addonNamespacePattern.MatchString(r.NamespaceConfig.Namespace) {
		return errors.New("namespace-config.namespace must be a valid RFC 1123 label")
	}
	if err := validateOptionalEnum(
		"resolve-conflicts-on-create",
		r.ResolveConflictsOnCreate,
		"NONE",
		"OVERWRITE",
	); err != nil {
		return err
	}
	if err := validateOptionalEnum(
		"resolve-conflicts-on-update",
		r.ResolveConflictsOnUpdate,
		"NONE",
		"OVERWRITE",
		"PRESERVE",
	); err != nil {
		return err
	}
	if r.ServiceAccountRoleARN != nil {
		if err := validateClusterARN(
			"service-account-role-arn", *r.ServiceAccountRoleARN,
		); err != nil {
			return err
		}
	}
	for index, association := range valueOrNil(r.PodIdentityAssociation) {
		if err := validateClusterARN(
			fmt.Sprintf("pod-identity-association[%d].role-arn", index),
			association.RoleARN,
		); err != nil {
			return err
		}
		if association.ServiceAccount == "" {
			return fmt.Errorf(
				"pod-identity-association[%d].service-account must not be empty", index,
			)
		}
	}
	return nil
}
