package wafv2

import (
	"context"
	"fmt"
	"regexp"
	"unicode/utf8"
)

var webACLNamePattern = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)

func (r *WebACLResource) ValidateInputs(context.Context, *awsCfg) error {
	if r.Scope != "REGIONAL" && r.Scope != "CLOUDFRONT" {
		return fmt.Errorf("scope must be REGIONAL or CLOUDFRONT")
	}
	if r.Name != nil {
		length := utf8.RuneCountInString(*r.Name)
		if length < 1 || length > 128 {
			return fmt.Errorf("name must be 1..128 characters")
		}
		if !webACLNamePattern.MatchString(*r.Name) {
			return fmt.Errorf("name must match ^[0-9A-Za-z_-]+$")
		}
	}
	if r.Description != nil {
		length := utf8.RuneCountInString(*r.Description)
		if length < 1 || length > 256 {
			return fmt.Errorf("description must be 1..256 characters")
		}
	}
	actions := 0
	if r.DefaultAction.Allow != nil {
		actions++
	}
	if r.DefaultAction.Block != nil {
		actions++
	}
	if actions != 1 {
		return fmt.Errorf("default-action must contain exactly one action")
	}
	if err := validateVisibilityConfig(r.VisibilityConfig); err != nil {
		return fmt.Errorf("visibility-config: %w", err)
	}
	if err := validateApplicationConfig(r.ApplicationConfig); err != nil {
		return err
	}
	if err := validateAssociationConfig(r.AssociationConfig); err != nil {
		return err
	}
	if err := validateCustomResponseBodies(r.CustomResponseBodies); err != nil {
		return err
	}
	if err := validateDataProtectionConfig(r.DataProtectionConfig); err != nil {
		return err
	}
	if err := validateOnSourceDDoSConfig(r.OnSourceDDoSConfig); err != nil {
		return err
	}
	if err := validateTokenDomains(r.TokenDomains); err != nil {
		return err
	}
	if err := validateWebACLTags(r.Tags); err != nil {
		return err
	}
	if err := validateCustomResponseBodyReferences(r); err != nil {
		return err
	}
	return validateManagedResponseInspectionScope(r)
}
