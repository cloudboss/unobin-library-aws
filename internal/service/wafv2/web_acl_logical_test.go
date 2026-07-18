package wafv2

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstypes "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogicalStatementLevelsDelegateAllLeaves(t *testing.T) {
	for _, tt := range logicalLeafInputs() {
		t.Run(tt.name, func(t *testing.T) {
			expected, err := expandStatementLevel0(tt.statement)
			require.NoError(t, err)

			level1, err := expandStatementLevel1(level1Leaf(tt.statement))
			require.NoError(t, err)
			assert.Equal(t, expected, level1)

			level2, err := expandStatementLevel2(level2Leaf(tt.statement))
			require.NoError(t, err)
			assert.Equal(t, expected, level2)

			level3, err := expandStatementLevel3(level3Leaf(tt.statement))
			require.NoError(t, err)
			assert.Equal(t, expected, level3)
		})
	}
}

func TestLogicalStatementLevelsRequireExactlyOneMember(t *testing.T) {
	tests := []struct {
		name   string
		expand func(bool) (*awstypes.Statement, error)
	}{
		{
			name: "Level1",
			expand: func(multiple bool) (*awstypes.Statement, error) {
				statement := WebACLStatementLevel1{}
				if multiple {
					statement.ASNMatchStatement = &WebACLASNMatchStatement{ASNList: []int64{1}}
					statement.GeoMatchStatement = &WebACLGeoMatchStatement{
						CountryCodes: []string{"US"},
					}
				}
				return expandStatementLevel1(statement)
			},
		},
		{
			name: "Level2",
			expand: func(multiple bool) (*awstypes.Statement, error) {
				statement := WebACLStatementLevel2{}
				if multiple {
					statement.ASNMatchStatement = &WebACLASNMatchStatement{ASNList: []int64{1}}
					statement.GeoMatchStatement = &WebACLGeoMatchStatement{
						CountryCodes: []string{"US"},
					}
				}
				return expandStatementLevel2(statement)
			},
		},
		{
			name: "Level3",
			expand: func(multiple bool) (*awstypes.Statement, error) {
				statement := WebACLStatementLevel3{}
				if multiple {
					statement.ASNMatchStatement = &WebACLASNMatchStatement{ASNList: []int64{1}}
					statement.GeoMatchStatement = &WebACLGeoMatchStatement{
						CountryCodes: []string{"US"},
					}
				}
				return expandStatementLevel3(statement)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, multiple := range []bool{false, true} {
				_, err := tt.expand(multiple)
				assert.ErrorContains(t, err, "exactly one")
			}
		})
	}
}

func TestLogicalStatementAndOrChildCounts(t *testing.T) {
	levels := []struct {
		name   string
		expand func(string, int) (*awstypes.Statement, error)
	}{
		{name: "Level1", expand: expandLogicalLevel1},
		{name: "Level2", expand: expandLogicalLevel2},
		{name: "Level3", expand: expandLogicalLevel3},
	}
	for _, level := range levels {
		for _, operation := range []string{"and", "or"} {
			t.Run(level.name+" "+operation, func(t *testing.T) {
				for _, count := range []int{0, 1} {
					_, err := level.expand(operation, count)
					assert.ErrorContains(t, err, "at least two")
				}
				actual, err := level.expand(operation, 2)
				require.NoError(t, err)
				if operation == "and" {
					require.NotNil(t, actual.AndStatement)
					assert.Equal(t, logicalSDKChildren(2), actual.AndStatement.Statements)
					return
				}
				require.NotNil(t, actual.OrStatement)
				assert.Equal(t, logicalSDKChildren(2), actual.OrStatement.Statements)
			})
		}
	}
}

func TestLogicalStatementMaximumDepth(t *testing.T) {
	input := WebACLStatementLevel3{
		NotStatement: &WebACLNotStatementLevel2{
			Statement: WebACLStatementLevel2{
				NotStatement: &WebACLNotStatementLevel1{
					Statement: WebACLStatementLevel1{
						NotStatement: &WebACLNotStatementLevel0{
							Statement: logicalLeaf0("deepest"),
						},
					},
				},
			},
		},
	}
	expected := &awstypes.Statement{
		NotStatement: &awstypes.NotStatement{
			Statement: &awstypes.Statement{
				NotStatement: &awstypes.NotStatement{
					Statement: &awstypes.Statement{
						NotStatement: &awstypes.NotStatement{
							Statement: logicalSDKLeaf("deepest"),
						},
					},
				},
			},
		},
	}

	actual, err := expandStatementLevel3(input)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

type logicalLeafCase struct {
	name      string
	statement WebACLStatementLevel0
}

func logicalLeafInputs() []logicalLeafCase {
	search := "value"
	return []logicalLeafCase{
		{name: "ASN", statement: WebACLStatementLevel0{
			ASNMatchStatement: &WebACLASNMatchStatement{ASNList: []int64{1}},
		}},
		{name: "byte", statement: WebACLStatementLevel0{
			ByteMatchStatement: &WebACLByteMatchStatement{
				FieldToMatch:         methodFieldToMatch(),
				PositionalConstraint: "EXACTLY",
				SearchString:         &search,
				TextTransformations:  testTextTransformations(),
			},
		}},
		{name: "geo", statement: WebACLStatementLevel0{
			GeoMatchStatement: &WebACLGeoMatchStatement{CountryCodes: []string{"US"}},
		}},
		{name: "IP set", statement: WebACLStatementLevel0{
			IPSetReferenceStatement: &WebACLIPSetReferenceStatement{ARN: "ip-set-arn"},
		}},
		{name: "label", statement: logicalLeaf0("label")},
		{name: "regex", statement: WebACLStatementLevel0{
			RegexMatchStatement: &WebACLRegexMatchStatement{
				FieldToMatch:        methodFieldToMatch(),
				RegexString:         "value",
				TextTransformations: testTextTransformations(),
			},
		}},
		{name: "regex set", statement: WebACLStatementLevel0{
			RegexPatternSetReference: &WebACLRegexPatternSetReferenceStatement{
				ARN:                 "regex-set-arn",
				FieldToMatch:        methodFieldToMatch(),
				TextTransformations: testTextTransformations(),
			},
		}},
		{name: "size", statement: WebACLStatementLevel0{
			SizeConstraintStatement: &WebACLSizeConstraintStatement{
				ComparisonOperator:  "EQ",
				FieldToMatch:        methodFieldToMatch(),
				Size:                1,
				TextTransformations: testTextTransformations(),
			},
		}},
		{name: "SQLi", statement: WebACLStatementLevel0{
			SQLiMatchStatement: &WebACLSQLiMatchStatement{
				FieldToMatch:        methodFieldToMatch(),
				TextTransformations: testTextTransformations(),
			},
		}},
		{name: "XSS", statement: WebACLStatementLevel0{
			XSSMatchStatement: &WebACLXSSMatchStatement{
				FieldToMatch:        methodFieldToMatch(),
				TextTransformations: testTextTransformations(),
			},
		}},
	}
}

func level1Leaf(in WebACLStatementLevel0) WebACLStatementLevel1 {
	return WebACLStatementLevel1{
		ASNMatchStatement:        in.ASNMatchStatement,
		ByteMatchStatement:       in.ByteMatchStatement,
		GeoMatchStatement:        in.GeoMatchStatement,
		IPSetReferenceStatement:  in.IPSetReferenceStatement,
		LabelMatchStatement:      in.LabelMatchStatement,
		RegexMatchStatement:      in.RegexMatchStatement,
		RegexPatternSetReference: in.RegexPatternSetReference,
		SizeConstraintStatement:  in.SizeConstraintStatement,
		SQLiMatchStatement:       in.SQLiMatchStatement,
		XSSMatchStatement:        in.XSSMatchStatement,
	}
}

func level2Leaf(in WebACLStatementLevel0) WebACLStatementLevel2 {
	return WebACLStatementLevel2{
		ASNMatchStatement:        in.ASNMatchStatement,
		ByteMatchStatement:       in.ByteMatchStatement,
		GeoMatchStatement:        in.GeoMatchStatement,
		IPSetReferenceStatement:  in.IPSetReferenceStatement,
		LabelMatchStatement:      in.LabelMatchStatement,
		RegexMatchStatement:      in.RegexMatchStatement,
		RegexPatternSetReference: in.RegexPatternSetReference,
		SizeConstraintStatement:  in.SizeConstraintStatement,
		SQLiMatchStatement:       in.SQLiMatchStatement,
		XSSMatchStatement:        in.XSSMatchStatement,
	}
}

func level3Leaf(in WebACLStatementLevel0) WebACLStatementLevel3 {
	return WebACLStatementLevel3{
		ASNMatchStatement:        in.ASNMatchStatement,
		ByteMatchStatement:       in.ByteMatchStatement,
		GeoMatchStatement:        in.GeoMatchStatement,
		IPSetReferenceStatement:  in.IPSetReferenceStatement,
		LabelMatchStatement:      in.LabelMatchStatement,
		RegexMatchStatement:      in.RegexMatchStatement,
		RegexPatternSetReference: in.RegexPatternSetReference,
		SizeConstraintStatement:  in.SizeConstraintStatement,
		SQLiMatchStatement:       in.SQLiMatchStatement,
		XSSMatchStatement:        in.XSSMatchStatement,
	}
}

func logicalLeaf0(name string) WebACLStatementLevel0 {
	return WebACLStatementLevel0{
		LabelMatchStatement: &WebACLLabelMatchStatement{Key: name, Scope: "LABEL"},
	}
}

func logicalSDKLeaf(name string) *awstypes.Statement {
	return &awstypes.Statement{
		LabelMatchStatement: &awstypes.LabelMatchStatement{
			Key:   aws.String(name),
			Scope: awstypes.LabelMatchScopeLabel,
		},
	}
}

func logicalSDKChildren(count int) []awstypes.Statement {
	out := make([]awstypes.Statement, count)
	for index := range count {
		out[index] = *logicalSDKLeaf(fmt.Sprintf("child-%d", index))
	}
	return out
}

func expandLogicalLevel1(operation string, count int) (*awstypes.Statement, error) {
	children := make([]WebACLStatementLevel0, count)
	for index := range count {
		children[index] = logicalLeaf0(fmt.Sprintf("child-%d", index))
	}
	statement := WebACLStatementLevel1{}
	if operation == "and" {
		statement.AndStatement = &WebACLAndStatementLevel0{Statements: children}
	} else {
		statement.OrStatement = &WebACLOrStatementLevel0{Statements: children}
	}
	return expandStatementLevel1(statement)
}

func expandLogicalLevel2(operation string, count int) (*awstypes.Statement, error) {
	children := make([]WebACLStatementLevel1, count)
	for index := range count {
		children[index] = level1Leaf(logicalLeaf0(fmt.Sprintf("child-%d", index)))
	}
	statement := WebACLStatementLevel2{}
	if operation == "and" {
		statement.AndStatement = &WebACLAndStatementLevel1{Statements: children}
	} else {
		statement.OrStatement = &WebACLOrStatementLevel1{Statements: children}
	}
	return expandStatementLevel2(statement)
}

func expandLogicalLevel3(operation string, count int) (*awstypes.Statement, error) {
	children := make([]WebACLStatementLevel2, count)
	for index := range count {
		children[index] = level2Leaf(logicalLeaf0(fmt.Sprintf("child-%d", index)))
	}
	statement := WebACLStatementLevel3{}
	if operation == "and" {
		statement.AndStatement = &WebACLAndStatementLevel2{Statements: children}
	} else {
		statement.OrStatement = &WebACLOrStatementLevel2{Statements: children}
	}
	return expandStatementLevel3(statement)
}
