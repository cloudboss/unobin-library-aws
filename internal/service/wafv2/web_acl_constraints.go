package wafv2

import "github.com/cloudboss/unobin/pkg/constraint"

const customResponseBodyKeyExists = "input.key == null || " +
	"@core.has-key(input.bodies, input.key)"

func (r WebACLResource) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.IsUnique(constraint.Project(r.Rules, func(rule WebACLRule) any {
			return rule.Name
		}))).Message("rule names must be unique"),
		constraint.Must(constraint.IsUnique(constraint.Project(r.Rules, func(rule WebACLRule) any {
			return rule.Priority
		}))).Message("rule priorities must be unique"),
		constraint.Must(constraint.Expr(customResponseBodyKeyExists,
			constraint.Bind("key", r.DefaultAction.Block.CustomResponse.CustomResponseBodyKey),
			constraint.Bind("bodies", r.CustomResponseBodies),
		)).Message("default-action custom-response-body-key must exist in custom-response-bodies"),
		constraint.ForEach(r.Rules, func(rule WebACLRule) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.Must(constraint.Expr(customResponseBodyKeyExists,
					constraint.Bind("key", rule.Action.Block.CustomResponse.CustomResponseBodyKey),
					constraint.Bind("bodies", r.CustomResponseBodies),
				)).Message("rule custom-response-body-key must exist in custom-response-bodies"),
				constraint.ForEach(rule.Statement.ManagedRuleGroupStatement.RuleActionOverrides,
					func(override WebACLRuleActionOverride) []constraint.Constraint {
						return []constraint.Constraint{
							constraint.Must(constraint.Expr(customResponseBodyKeyExists,
								constraint.Bind("key",
									override.ActionToUse.Block.CustomResponse.CustomResponseBodyKey),
								constraint.Bind("bodies", r.CustomResponseBodies),
							)).Message("managed rule override custom-response-body-key " +
								"must exist in custom-response-bodies"),
						}
					}),
				constraint.ForEach(rule.Statement.RuleGroupReferenceStatement.RuleActionOverrides,
					func(override WebACLRuleActionOverride) []constraint.Constraint {
						return []constraint.Constraint{
							constraint.Must(constraint.Expr(customResponseBodyKeyExists,
								constraint.Bind("key",
									override.ActionToUse.Block.CustomResponse.CustomResponseBodyKey),
								constraint.Bind("bodies", r.CustomResponseBodies),
							)).Message("rule group override custom-response-body-key " +
								"must exist in custom-response-bodies"),
						}
					}),
			}
		}),
	}
}
