package wafv2

import (
	"fmt"

	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
)

func validateStatementLevel1(statement WebACLStatementLevel1) error {
	if countLevel1Members(statement) != 1 {
		return fmt.Errorf("level-1 statement must contain exactly one member")
	}
	switch {
	case statement.AndStatement != nil:
		return validateLevel0Children("and-statement", statement.AndStatement.Statements)
	case statement.NotStatement != nil:
		return validateStatementLevel0(statement.NotStatement.Statement)
	case statement.OrStatement != nil:
		return validateLevel0Children("or-statement", statement.OrStatement.Statements)
	default:
		return validateStatementLevel0(level0FromLevel1(statement))
	}
}

func countLevel1Members(statement WebACLStatementLevel1) int {
	return countSet(
		statement.AndStatement != nil,
		statement.NotStatement != nil,
		statement.OrStatement != nil,
		statement.ASNMatchStatement != nil,
		statement.ByteMatchStatement != nil,
		statement.GeoMatchStatement != nil,
		statement.IPSetReferenceStatement != nil,
		statement.LabelMatchStatement != nil,
		statement.RegexMatchStatement != nil,
		statement.RegexPatternSetReference != nil,
		statement.SizeConstraintStatement != nil,
		statement.SQLiMatchStatement != nil,
		statement.XSSMatchStatement != nil,
	)
}

func validateStatementLevel2(statement WebACLStatementLevel2) error {
	if countLevel2Members(statement) != 1 {
		return fmt.Errorf("level-2 statement must contain exactly one member")
	}
	switch {
	case statement.AndStatement != nil:
		return validateLevel1Children("and-statement", statement.AndStatement.Statements)
	case statement.NotStatement != nil:
		return validateStatementLevel1(statement.NotStatement.Statement)
	case statement.OrStatement != nil:
		return validateLevel1Children("or-statement", statement.OrStatement.Statements)
	default:
		return validateStatementLevel0(level0FromLevel2(statement))
	}
}

func countLevel2Members(statement WebACLStatementLevel2) int {
	return countSet(
		statement.AndStatement != nil,
		statement.NotStatement != nil,
		statement.OrStatement != nil,
		statement.ASNMatchStatement != nil,
		statement.ByteMatchStatement != nil,
		statement.GeoMatchStatement != nil,
		statement.IPSetReferenceStatement != nil,
		statement.LabelMatchStatement != nil,
		statement.RegexMatchStatement != nil,
		statement.RegexPatternSetReference != nil,
		statement.SizeConstraintStatement != nil,
		statement.SQLiMatchStatement != nil,
		statement.XSSMatchStatement != nil,
	)
}

func validateStatementLevel3(statement WebACLStatementLevel3) error {
	if countLevel3Members(statement) != 1 {
		return fmt.Errorf("level-3 statement must contain exactly one member")
	}
	switch {
	case statement.AndStatement != nil:
		return validateLevel2Children("and-statement", statement.AndStatement.Statements)
	case statement.NotStatement != nil:
		return validateStatementLevel2(statement.NotStatement.Statement)
	case statement.OrStatement != nil:
		return validateLevel2Children("or-statement", statement.OrStatement.Statements)
	case statement.ManagedRuleGroupStatement != nil:
		return validateManagedRuleGroupStatement(statement.ManagedRuleGroupStatement)
	case statement.RuleGroupReferenceStatement != nil:
		return validateRuleGroupReferenceStatement(statement.RuleGroupReferenceStatement)
	case statement.RateBasedStatement != nil:
		return validateRateBasedStatement(statement.RateBasedStatement)
	default:
		return validateStatementLevel0(level0FromLevel3(statement))
	}
}

func countLevel3Members(statement WebACLStatementLevel3) int {
	return countSet(
		statement.AndStatement != nil,
		statement.NotStatement != nil,
		statement.OrStatement != nil,
		statement.ASNMatchStatement != nil,
		statement.ByteMatchStatement != nil,
		statement.GeoMatchStatement != nil,
		statement.IPSetReferenceStatement != nil,
		statement.LabelMatchStatement != nil,
		statement.ManagedRuleGroupStatement != nil,
		statement.RateBasedStatement != nil,
		statement.RegexMatchStatement != nil,
		statement.RegexPatternSetReference != nil,
		statement.RuleGroupReferenceStatement != nil,
		statement.SizeConstraintStatement != nil,
		statement.SQLiMatchStatement != nil,
		statement.XSSMatchStatement != nil,
	)
}

func validateLevel0Children(name string, statements []WebACLStatementLevel0) error {
	if len(statements) < 2 {
		return fmt.Errorf("%s must contain at least two statements", name)
	}
	for index, statement := range statements {
		if err := validateStatementLevel0(statement); err != nil {
			return fmt.Errorf("%s child %d: %w", name, index, err)
		}
	}
	return nil
}

func validateLevel1Children(name string, statements []WebACLStatementLevel1) error {
	if len(statements) < 2 {
		return fmt.Errorf("%s must contain at least two statements", name)
	}
	for index, statement := range statements {
		if err := validateStatementLevel1(statement); err != nil {
			return fmt.Errorf("%s child %d: %w", name, index, err)
		}
	}
	return nil
}

func validateLevel2Children(name string, statements []WebACLStatementLevel2) error {
	if len(statements) < 2 {
		return fmt.Errorf("%s must contain at least two statements", name)
	}
	for index, statement := range statements {
		if err := validateStatementLevel2(statement); err != nil {
			return fmt.Errorf("%s child %d: %w", name, index, err)
		}
	}
	return nil
}

func expandStatementLevel1(statement WebACLStatementLevel1) (*awstypes.Statement, error) {
	if err := validateStatementLevel1(statement); err != nil {
		return nil, err
	}
	switch {
	case statement.AndStatement != nil:
		children, err := expandLevel0Statements(statement.AndStatement.Statements)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{
			AndStatement: &awstypes.AndStatement{Statements: children},
		}, nil
	case statement.NotStatement != nil:
		child, err := expandStatementLevel0(statement.NotStatement.Statement)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{
			NotStatement: &awstypes.NotStatement{Statement: child},
		}, nil
	case statement.OrStatement != nil:
		children, err := expandLevel0Statements(statement.OrStatement.Statements)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{
			OrStatement: &awstypes.OrStatement{Statements: children},
		}, nil
	default:
		return expandStatementLevel0(level0FromLevel1(statement))
	}
}

func expandStatementLevel2(statement WebACLStatementLevel2) (*awstypes.Statement, error) {
	if err := validateStatementLevel2(statement); err != nil {
		return nil, err
	}
	switch {
	case statement.AndStatement != nil:
		children, err := expandLevel1Statements(statement.AndStatement.Statements)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{
			AndStatement: &awstypes.AndStatement{Statements: children},
		}, nil
	case statement.NotStatement != nil:
		child, err := expandStatementLevel1(statement.NotStatement.Statement)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{
			NotStatement: &awstypes.NotStatement{Statement: child},
		}, nil
	case statement.OrStatement != nil:
		children, err := expandLevel1Statements(statement.OrStatement.Statements)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{
			OrStatement: &awstypes.OrStatement{Statements: children},
		}, nil
	default:
		return expandStatementLevel0(level0FromLevel2(statement))
	}
}

func expandStatementLevel3(statement WebACLStatementLevel3) (*awstypes.Statement, error) {
	if err := validateStatementLevel3(statement); err != nil {
		return nil, err
	}
	switch {
	case statement.AndStatement != nil:
		children, err := expandLevel2Statements(statement.AndStatement.Statements)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{
			AndStatement: &awstypes.AndStatement{Statements: children},
		}, nil
	case statement.NotStatement != nil:
		child, err := expandStatementLevel2(statement.NotStatement.Statement)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{
			NotStatement: &awstypes.NotStatement{Statement: child},
		}, nil
	case statement.OrStatement != nil:
		children, err := expandLevel2Statements(statement.OrStatement.Statements)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{
			OrStatement: &awstypes.OrStatement{Statements: children},
		}, nil
	case statement.ManagedRuleGroupStatement != nil:
		converted, err := expandManagedRuleGroupStatement(
			statement.ManagedRuleGroupStatement,
		)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{ManagedRuleGroupStatement: converted}, nil
	case statement.RuleGroupReferenceStatement != nil:
		converted, err := expandRuleGroupReferenceStatement(
			statement.RuleGroupReferenceStatement,
		)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{RuleGroupReferenceStatement: converted}, nil
	case statement.RateBasedStatement != nil:
		converted, err := expandRateBasedStatement(statement.RateBasedStatement)
		if err != nil {
			return nil, err
		}
		return &awstypes.Statement{RateBasedStatement: converted}, nil
	default:
		return expandStatementLevel0(level0FromLevel3(statement))
	}
}

func expandLevel0Statements(
	statements []WebACLStatementLevel0,
) ([]awstypes.Statement, error) {
	out := make([]awstypes.Statement, len(statements))
	for index, statement := range statements {
		converted, err := expandStatementLevel0(statement)
		if err != nil {
			return nil, fmt.Errorf("convert level-0 child %d: %w", index, err)
		}
		out[index] = *converted
	}
	return out, nil
}

func expandLevel1Statements(
	statements []WebACLStatementLevel1,
) ([]awstypes.Statement, error) {
	out := make([]awstypes.Statement, len(statements))
	for index, statement := range statements {
		converted, err := expandStatementLevel1(statement)
		if err != nil {
			return nil, fmt.Errorf("convert level-1 child %d: %w", index, err)
		}
		out[index] = *converted
	}
	return out, nil
}

func expandLevel2Statements(
	statements []WebACLStatementLevel2,
) ([]awstypes.Statement, error) {
	out := make([]awstypes.Statement, len(statements))
	for index, statement := range statements {
		converted, err := expandStatementLevel2(statement)
		if err != nil {
			return nil, fmt.Errorf("convert level-2 child %d: %w", index, err)
		}
		out[index] = *converted
	}
	return out, nil
}

func level0FromLevel1(statement WebACLStatementLevel1) WebACLStatementLevel0 {
	return WebACLStatementLevel0{
		ASNMatchStatement:        statement.ASNMatchStatement,
		ByteMatchStatement:       statement.ByteMatchStatement,
		GeoMatchStatement:        statement.GeoMatchStatement,
		IPSetReferenceStatement:  statement.IPSetReferenceStatement,
		LabelMatchStatement:      statement.LabelMatchStatement,
		RegexMatchStatement:      statement.RegexMatchStatement,
		RegexPatternSetReference: statement.RegexPatternSetReference,
		SizeConstraintStatement:  statement.SizeConstraintStatement,
		SQLiMatchStatement:       statement.SQLiMatchStatement,
		XSSMatchStatement:        statement.XSSMatchStatement,
	}
}

func level0FromLevel2(statement WebACLStatementLevel2) WebACLStatementLevel0 {
	return WebACLStatementLevel0{
		ASNMatchStatement:        statement.ASNMatchStatement,
		ByteMatchStatement:       statement.ByteMatchStatement,
		GeoMatchStatement:        statement.GeoMatchStatement,
		IPSetReferenceStatement:  statement.IPSetReferenceStatement,
		LabelMatchStatement:      statement.LabelMatchStatement,
		RegexMatchStatement:      statement.RegexMatchStatement,
		RegexPatternSetReference: statement.RegexPatternSetReference,
		SizeConstraintStatement:  statement.SizeConstraintStatement,
		SQLiMatchStatement:       statement.SQLiMatchStatement,
		XSSMatchStatement:        statement.XSSMatchStatement,
	}
}

func level0FromLevel3(statement WebACLStatementLevel3) WebACLStatementLevel0 {
	return WebACLStatementLevel0{
		ASNMatchStatement:        statement.ASNMatchStatement,
		ByteMatchStatement:       statement.ByteMatchStatement,
		GeoMatchStatement:        statement.GeoMatchStatement,
		IPSetReferenceStatement:  statement.IPSetReferenceStatement,
		LabelMatchStatement:      statement.LabelMatchStatement,
		RegexMatchStatement:      statement.RegexMatchStatement,
		RegexPatternSetReference: statement.RegexPatternSetReference,
		SizeConstraintStatement:  statement.SizeConstraintStatement,
		SQLiMatchStatement:       statement.SQLiMatchStatement,
		XSSMatchStatement:        statement.XSSMatchStatement,
	}
}
