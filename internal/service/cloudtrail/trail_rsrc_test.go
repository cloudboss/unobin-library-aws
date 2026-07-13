package cloudtrail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTrailName = "unobin-trail"
	testTrailArn  = "arn:aws:cloudtrail:us-east-1:123456789012:trail/unobin-trail"
)

func TestTrailMetadata(t *testing.T) {
	r := TrailResource{}
	assert.Equal(t, []string{"name"}, (&r).ReplaceFields())
	assert.Equal(t, 2*time.Minute, trailPropagationTimeout)
}

func TestTrailValidateInputs(t *testing.T) {
	valid := TrailResource{Name: testTrailName, S3BucketName: "logs-bucket"}
	tests := []struct {
		name    string
		mutate  func(*TrailResource)
		wantErr string
	}{
		{name: "valid"},
		{name: "three character name", mutate: func(r *TrailResource) { r.Name = "a-a" }},
		{
			name:   "128 character name",
			mutate: func(r *TrailResource) { r.Name = strings.Repeat("a", 128) },
		},
		{name: "short name", mutate: func(r *TrailResource) { r.Name = "ab" }, wantErr: "name"},
		{
			name:    "long name",
			mutate:  func(r *TrailResource) { r.Name = strings.Repeat("a", 129) },
			wantErr: "name",
		},
		{name: "invalid name character", mutate: func(r *TrailResource) {
			r.Name = "trail/name"
		}, wantErr: "contain only"},
		{name: "invalid name start", mutate: func(r *TrailResource) {
			r.Name = ".trail"
		}, wantErr: "start and end"},
		{name: "invalid name end", mutate: func(r *TrailResource) {
			r.Name = "trail_"
		}, wantErr: "start and end"},
		{name: "adjacent name punctuation", mutate: func(r *TrailResource) {
			r.Name = "trail.-name"
		}, wantErr: "adjacent"},
		{name: "non ASCII name", mutate: func(r *TrailResource) {
			r.Name = "tráil"
		}, wantErr: "ASCII"},
		{name: "IP address name", mutate: func(r *TrailResource) {
			r.Name = "192.168.1.1"
		}, wantErr: "IP address"},
		{
			name: "200 character prefix",
			mutate: func(r *TrailResource) {
				v := strings.Repeat("a", 200)
				r.S3KeyPrefix = &v
			},
		},
		{
			name: "201 character prefix",
			mutate: func(r *TrailResource) {
				v := strings.Repeat("a", 201)
				r.S3KeyPrefix = &v
			},
			wantErr: "s3-key-prefix",
		},
		{
			name: "selector conflict",
			mutate: func(r *TrailResource) {
				r.EventSelectors = &[]TrailEventSelector{}
				r.AdvancedEventSelectors = &[]TrailAdvancedEventSelector{}
			},
			wantErr: "conflict",
		},
		{
			name: "too many standard selectors",
			mutate: func(r *TrailResource) {
				r.EventSelectors = &[]TrailEventSelector{{}, {}, {}, {}, {}, {}}
			},
			wantErr: "at most 5",
		},
		{
			name: "bad read write type",
			mutate: func(r *TrailResource) {
				v := "Both"
				r.EventSelectors = &[]TrailEventSelector{{ReadWriteType: &v}}
			},
			wantErr: "read-write-type",
		},
		{
			name: "too many data values",
			mutate: func(r *TrailResource) {
				values := make([]string, 251)
				r.EventSelectors = &[]TrailEventSelector{{
					DataResources: &[]TrailDataResource{{Type: "AWS::S3::Object", Values: &values}},
				}}
			},
			wantErr: "at most 250",
		},
		{
			name: "bad advanced field",
			mutate: func(r *TrailResource) {
				r.AdvancedEventSelectors = &[]TrailAdvancedEventSelector{{
					FieldSelectors: []TrailAdvancedFieldSelector{{Field: "unknown"}},
				}}
			},
			wantErr: "field",
		},
		{
			name: "empty advanced operator",
			mutate: func(r *TrailResource) {
				r.AdvancedEventSelectors = &[]TrailAdvancedEventSelector{{
					FieldSelectors: []TrailAdvancedFieldSelector{{
						Field:  "eventCategory",
						Equals: &[]string{},
					}},
				}}
			},
			wantErr: "equals",
		},
		{
			name: "duplicate insight category",
			mutate: func(r *TrailResource) {
				r.InsightSelectors = &[]TrailInsightSelector{{
					InsightType:     "ApiCallRateInsight",
					EventCategories: &[]string{"Data", "Data"},
				}}
			},
			wantErr: "unique",
		},
		{
			name: "too many aggregations",
			mutate: func(r *TrailResource) {
				r.AggregationConfigurations = &[]TrailAggregationConfiguration{{}, {}}
			},
			wantErr: "at most 1",
		},
		{
			name: "duplicate aggregation template",
			mutate: func(r *TrailResource) {
				r.AggregationConfigurations = &[]TrailAggregationConfiguration{{
					EventCategory: "Data",
					Templates:     []string{"API_ACTIVITY", "API_ACTIVITY"},
				}}
			},
			wantErr: "unique",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			err := r.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestTrailARNValidation(t *testing.T) {
	fields := []struct {
		name  string
		valid string
		set   func(*TrailResource, *string)
	}{
		{
			name:  "log group",
			valid: "arn:aws:logs:us-east-1:123456789012:log-group:name",
			set: func(r *TrailResource, value *string) {
				r.CloudWatchLogsLogGroupArn = value
			},
		},
		{
			name:  "role",
			valid: "arn:aws:iam::123456789012:role/name",
			set: func(r *TrailResource, value *string) {
				r.CloudWatchLogsRoleArn = value
			},
		},
		{
			name:  "KMS key",
			valid: "arn:aws:kms:us-east-1:123456789012:key/id",
			set: func(r *TrailResource, value *string) {
				r.KmsKeyId = value
			},
		},
	}
	invalid := []struct {
		name  string
		value func(string) string
	}{
		{name: "partition", value: func(value string) string {
			return strings.Replace(value, "arn:aws:", "arn:AWS:", 1)
		}},
		{name: "region", value: func(value string) string {
			parts := strings.SplitN(value, ":", 6)
			parts[3] = "bad_region"
			return strings.Join(parts, ":")
		}},
		{name: "account", value: func(value string) string {
			parts := strings.SplitN(value, ":", 6)
			parts[4] = "account"
			return strings.Join(parts, ":")
		}},
		{name: "resource", value: func(value string) string {
			parts := strings.SplitN(value, ":", 6)
			parts[5] = ""
			return strings.Join(parts, ":")
		}},
	}

	for _, field := range fields {
		t.Run(field.name+" valid", func(t *testing.T) {
			r := TrailResource{Name: testTrailName, S3BucketName: "logs-bucket"}
			field.set(&r, aws.String(field.valid))
			require.NoError(t, r.ValidateInputs(context.Background(), nil))
		})
		for _, bad := range invalid {
			t.Run(field.name+" malformed "+bad.name, func(t *testing.T) {
				r := TrailResource{Name: testTrailName, S3BucketName: "logs-bucket"}
				field.set(&r, aws.String(bad.value(field.valid)))
				require.Error(t, r.ValidateInputs(context.Background(), nil))
			})
		}
	}

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{
			name:  "AWS partition with suffix",
			value: "arn:aws-us-gov:service:us-gov-west-1:123456789012:resource",
		},
		{
			name:  "four letter region prefix",
			value: "arn:aws:service:eusc-de-east-1:123456789012:resource",
		},
		{
			name:  "AWS account alias",
			value: "arn:aws:service:us-east-1:aws:resource",
		},
		{
			name:  "AWS managed account alias",
			value: "arn:aws:service:us-east-1:aws-managed:resource",
		},
		{
			name:  "third party account alias",
			value: "arn:aws:service:us-east-1:third-party:resource",
		},
		{
			name:  "AWS marketplace account alias",
			value: "arn:aws:service:us-east-1:aws-marketplace:resource",
		},
		{
			name:  "partner managed account alias",
			value: "arn:aws:service:us-east-1:partner-managed:resource",
		},
		{
			name:  "CloudWatch account alias",
			value: "arn:aws:service:us-east-1:cw1234567890:resource",
		},
		{
			name:  "unrestricted service",
			value: "arn:aws:service_name:us-east-1:123456789012:resource",
		},
		{
			name:    "non-AWS partition",
			value:   "arn:foo:service:us-east-1:123456789012:resource",
			wantErr: true,
		},
		{
			name:    "partition suffix with digit",
			value:   "arn:aws-1:service:us-east-1:123456789012:resource",
			wantErr: true,
		},
		{
			name:    "region component with digit",
			value:   "arn:aws:service:us-east2-west-1:123456789012:resource",
			wantErr: true,
		},
		{
			name:    "three digit region suffix",
			value:   "arn:aws:service:us-east-123:123456789012:resource",
			wantErr: true,
		},
		{
			name:    "unsupported account alias",
			value:   "arn:aws:service:us-east-1:account:resource",
			wantErr: true,
		},
		{
			name:    "eleven digit account",
			value:   "arn:aws:service:us-east-1:12345678901:resource",
			wantErr: true,
		},
		{
			name:    "short CloudWatch account alias",
			value:   "arn:aws:service:us-east-1:cw123456789:resource",
			wantErr: true,
		},
		{
			name:    "empty resource",
			value:   "arn:aws:service:us-east-1:123456789012:",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGenericARN(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTrailSelectorValidationBoundaries(t *testing.T) {
	for _, resourceType := range []string{
		"AWS::S3::Object", "AWS::Lambda::Function", "AWS::DynamoDB::Table",
	} {
		t.Run("resource "+resourceType, func(t *testing.T) {
			values := make([]string, 250)
			for i := range values {
				values[i] = fmt.Sprintf("resource-%d", i)
			}
			r := TrailResource{
				Name:         testTrailName,
				S3BucketName: "logs-bucket",
				EventSelectors: &[]TrailEventSelector{{
					DataResources: &[]TrailDataResource{{
						Type: resourceType, Values: &values,
					}},
				}},
			}
			require.NoError(t, r.ValidateInputs(context.Background(), nil))
		})
	}
}

func TestTrailAdvancedSelectorStructureValidation(t *testing.T) {
	tests := []struct {
		name      string
		selectors []TrailAdvancedEventSelector
		wantErr   string
	}{
		{name: "management", selectors: []TrailAdvancedEventSelector{
			advancedTestSelector("Management"),
		}},
		{name: "data", selectors: []TrailAdvancedEventSelector{
			advancedTestSelector("Data"),
		}},
		{name: "network activity", selectors: []TrailAdvancedEventSelector{
			advancedTestSelector("NetworkActivity"),
		}},
		{
			name: "missing event category",
			selectors: []TrailAdvancedEventSelector{{FieldSelectors: []TrailAdvancedFieldSelector{
				advancedEqualsField("eventName", "PutObject"),
			}}},
			wantErr: "eventCategory",
		},
		{
			name: "duplicate event category",
			selectors: []TrailAdvancedEventSelector{advancedTestSelector(
				"Management", advancedEqualsField("eventCategory", "Management"),
			)},
			wantErr: "exactly one eventCategory",
		},
		{
			name: "invalid event category",
			selectors: []TrailAdvancedEventSelector{{FieldSelectors: []TrailAdvancedFieldSelector{
				advancedEqualsField("eventCategory", "Other"),
			}}},
			wantErr: "Management, Data, or NetworkActivity",
		},
		{
			name: "event category wrong operator",
			selectors: []TrailAdvancedEventSelector{{FieldSelectors: []TrailAdvancedFieldSelector{{
				Field: "eventCategory", NotEquals: &[]string{"Data"},
			}}}},
			wantErr: "eventCategory must use only equals",
		},
		{
			name: "event category multiple values",
			selectors: []TrailAdvancedEventSelector{{FieldSelectors: []TrailAdvancedFieldSelector{
				advancedEqualsField("eventCategory", "Management", "Data"),
			}}},
			wantErr: "eventCategory equals requires exactly one value",
		},
		{
			name: "data missing resource type",
			selectors: []TrailAdvancedEventSelector{{FieldSelectors: []TrailAdvancedFieldSelector{
				advancedEqualsField("eventCategory", "Data"),
			}}},
			wantErr: "exactly one resources.type",
		},
		{
			name: "data duplicate resource type",
			selectors: []TrailAdvancedEventSelector{advancedTestSelector(
				"Data", advancedEqualsField("resources.type", "AWS::S3::Object"),
			)},
			wantErr: "exactly one resources.type",
		},
		{
			name: "resource type wrong operator",
			selectors: []TrailAdvancedEventSelector{{FieldSelectors: []TrailAdvancedFieldSelector{
				advancedEqualsField("eventCategory", "Data"),
				{Field: "resources.type", NotEquals: &[]string{"AWS::S3::Object"}},
			}}},
			wantErr: "resources.type must use only equals",
		},
		{
			name: "resource type multiple values",
			selectors: []TrailAdvancedEventSelector{{FieldSelectors: []TrailAdvancedFieldSelector{
				advancedEqualsField("eventCategory", "Data"),
				advancedEqualsField(
					"resources.type", "AWS::S3::Object", "AWS::Lambda::Function",
				),
			}}},
			wantErr: "resources.type equals requires exactly one value",
		},
		{
			name: "network activity missing event source",
			selectors: []TrailAdvancedEventSelector{{FieldSelectors: []TrailAdvancedFieldSelector{
				advancedEqualsField("eventCategory", "NetworkActivity"),
			}}},
			wantErr: "eventSource",
		},
		{
			name: "operatorless field",
			selectors: []TrailAdvancedEventSelector{advancedTestSelector(
				"Management", TrailAdvancedFieldSelector{Field: "eventName"},
			)},
			wantErr: "requires an operator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := advancedTestResource(tt.selectors...)
			err := r.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestTrailAdvancedSelectorCategoryFields(t *testing.T) {
	allFields := []string{
		"errorCode", "eventCategory", "eventName", "eventSource", "eventType",
		"readOnly", "resources.ARN", "resources.type", "sessionCredentialFromConsole",
		"userIdentity.arn", "vpcEndpointId",
	}
	allowed := map[string]map[string]bool{
		"Management": {
			"eventCategory": true, "eventSource": true, "readOnly": true,
		},
		"Data": {
			"eventCategory": true, "resources.type": true, "resources.ARN": true,
			"eventName": true, "eventSource": true, "eventType": true, "readOnly": true,
			"sessionCredentialFromConsole": true, "userIdentity.arn": true,
		},
		"NetworkActivity": {
			"eventCategory": true, "eventSource": true, "eventName": true,
			"errorCode": true, "vpcEndpointId": true,
		},
	}

	for category, categoryFields := range allowed {
		for _, fieldName := range allFields {
			t.Run(category+"/"+fieldName, func(t *testing.T) {
				selector := advancedTestSelector(category)
				if !advancedTestSelectorHasField(selector, fieldName) {
					selector.FieldSelectors = append(selector.FieldSelectors,
						advancedEqualsField(fieldName, advancedTestFieldValue(fieldName)))
				}
				r := advancedTestResource(selector)
				err := r.ValidateInputs(context.Background(), nil)
				if categoryFields[fieldName] {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not allowed for "+category)
			})
		}
	}
}

func TestTrailAdvancedSelectorOperators(t *testing.T) {
	operators := advancedTestOperators()
	for _, operator := range operators {
		t.Run("eventName/"+operator.name, func(t *testing.T) {
			field := TrailAdvancedFieldSelector{Field: "eventName"}
			values := []string{"PutObject"}
			operator.set(&field, &values)
			r := advancedTestResource(advancedTestSelector("Data", field))
			require.NoError(t, r.ValidateInputs(context.Background(), nil))
		})
	}

	tests := []struct {
		name     string
		field    TrailAdvancedFieldSelector
		category string
		wantErr  string
	}{
		{
			name: "readOnly equals", category: "Management",
			field: advancedEqualsField("readOnly", "true"),
		},
		{
			name: "readOnly not-equals", category: "Management",
			field: TrailAdvancedFieldSelector{
				Field: "readOnly", NotEquals: &[]string{"true"},
			},
			wantErr: "readOnly must use only equals",
		},
		{
			name: "console not-equals", category: "Data",
			field: TrailAdvancedFieldSelector{
				Field: "sessionCredentialFromConsole", NotEquals: &[]string{"true"},
			},
		},
		{
			name: "console starts-with", category: "Data",
			field: TrailAdvancedFieldSelector{
				Field: "sessionCredentialFromConsole", StartsWith: &[]string{"t"},
			},
			wantErr: "sessionCredentialFromConsole supports only equals and not-equals",
		},
		{
			name: "network event source starts-with", category: "NetworkActivity",
			field: TrailAdvancedFieldSelector{
				Field: "eventSource", StartsWith: &[]string{"s3"},
			},
			wantErr: "eventSource must use only equals for NetworkActivity",
		},
		{
			name: "network error code", category: "NetworkActivity",
			field: advancedEqualsField("errorCode", "VpceAccessDenied"),
		},
		{
			name: "network error code value", category: "NetworkActivity",
			field:   advancedEqualsField("errorCode", "AccessDenied"),
			wantErr: "errorCode equals accepts only VpceAccessDenied",
		},
		{
			name: "network error code operator", category: "NetworkActivity",
			field: TrailAdvancedFieldSelector{
				Field: "errorCode", NotEquals: &[]string{"VpceAccessDenied"},
			},
			wantErr: "errorCode must use only equals for NetworkActivity",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := advancedTestResource(advancedTestSelector(tt.category, tt.field))
			err := r.ValidateInputs(context.Background(), nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestTrailAdvancedSelectorValueBoundaries(t *testing.T) {
	for _, operator := range advancedTestOperators() {
		for _, size := range []int{0, 1, 2048, 2049} {
			t.Run(fmt.Sprintf("%s/%d", operator.name, size), func(t *testing.T) {
				field := TrailAdvancedFieldSelector{Field: "eventName"}
				values := []string{strings.Repeat("a", size)}
				operator.set(&field, &values)
				r := advancedTestResource(advancedTestSelector("Data", field))
				err := r.ValidateInputs(context.Background(), nil)
				if size >= 1 && size <= 2048 {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
			})
		}

		t.Run(operator.name+"/duplicate", func(t *testing.T) {
			field := TrailAdvancedFieldSelector{Field: "eventName"}
			values := []string{"PutObject", "PutObject"}
			operator.set(&field, &values)
			r := advancedTestResource(advancedTestSelector("Data", field))
			err := r.ValidateInputs(context.Background(), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unique")
		})
	}

	t.Run("empty secondary operator", func(t *testing.T) {
		r := advancedTestResource(advancedTestSelector("Data",
			TrailAdvancedFieldSelector{
				Field: "eventName", Equals: &[]string{"PutObject"}, NotEquals: &[]string{},
			},
		))
		err := r.ValidateInputs(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not-equals must not be empty")
	})

	for _, tt := range []struct {
		total  int
		counts [2]int
	}{
		{total: 500, counts: [2]int{249, 249}},
		{total: 501, counts: [2]int{249, 250}},
	} {
		t.Run(fmt.Sprintf("total/%d", tt.total), func(t *testing.T) {
			selectors := make([]TrailAdvancedEventSelector, 0, len(tt.counts))
			for selectorIndex, count := range tt.counts {
				values := make([]string, count)
				for i := range values {
					values[i] = fmt.Sprintf("source-%d-%d", selectorIndex, i)
				}
				selectors = append(selectors, advancedTestSelector("Management",
					TrailAdvancedFieldSelector{Field: "eventSource", Equals: &values},
				))
			}
			r := advancedTestResource(selectors...)
			err := r.ValidateInputs(context.Background(), nil)
			if tt.total == 500 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "at most 500")
		})
	}
}

type advancedTestOperator struct {
	name string
	set  func(*TrailAdvancedFieldSelector, *[]string)
}

func advancedTestOperators() []advancedTestOperator {
	return []advancedTestOperator{
		{name: "equals", set: func(f *TrailAdvancedFieldSelector, v *[]string) { f.Equals = v }},
		{name: "not-equals", set: func(f *TrailAdvancedFieldSelector, v *[]string) {
			f.NotEquals = v
		}},
		{name: "starts-with", set: func(f *TrailAdvancedFieldSelector, v *[]string) {
			f.StartsWith = v
		}},
		{name: "not-starts-with", set: func(f *TrailAdvancedFieldSelector, v *[]string) {
			f.NotStartsWith = v
		}},
		{name: "ends-with", set: func(f *TrailAdvancedFieldSelector, v *[]string) {
			f.EndsWith = v
		}},
		{name: "not-ends-with", set: func(f *TrailAdvancedFieldSelector, v *[]string) {
			f.NotEndsWith = v
		}},
	}
}

func advancedEqualsField(field string, values ...string) TrailAdvancedFieldSelector {
	return TrailAdvancedFieldSelector{Field: field, Equals: &values}
}

func advancedTestSelector(
	category string,
	extra ...TrailAdvancedFieldSelector,
) TrailAdvancedEventSelector {
	fields := []TrailAdvancedFieldSelector{advancedEqualsField("eventCategory", category)}
	switch category {
	case "Data":
		fields = append(fields, advancedEqualsField("resources.type", "AWS::S3::Object"))
	case "NetworkActivity":
		fields = append(fields, advancedEqualsField("eventSource", "s3.amazonaws.com"))
	}
	fields = append(fields, extra...)
	return TrailAdvancedEventSelector{FieldSelectors: fields}
}

func advancedTestSelectorHasField(selector TrailAdvancedEventSelector, field string) bool {
	for _, candidate := range selector.FieldSelectors {
		if candidate.Field == field {
			return true
		}
	}
	return false
}

func advancedTestFieldValue(field string) string {
	switch field {
	case "errorCode":
		return "VpceAccessDenied"
	case "readOnly", "sessionCredentialFromConsole":
		return "true"
	case "resources.type":
		return "AWS::S3::Object"
	default:
		return "value"
	}
}

func advancedTestResource(selectors ...TrailAdvancedEventSelector) TrailResource {
	return TrailResource{
		Name: testTrailName, S3BucketName: "logs-bucket",
		AdvancedEventSelectors: &selectors,
	}
}

func TestCloudWatchRetryPredicate(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "role access denied",
			err: &cloudtrailtypes.InvalidCloudWatchLogsRoleArnException{
				Message: aws.String("Access denied. retry"),
			},
			want: true,
		},
		{
			name: "group access denied",
			err: &cloudtrailtypes.InvalidCloudWatchLogsLogGroupArnException{
				Message: aws.String("Access denied. retry"),
			},
			want: true,
		},
		{
			name: "role other message",
			err: &cloudtrailtypes.InvalidCloudWatchLogsRoleArnException{
				Message: aws.String("invalid role"),
			},
		},
		{name: "other error", err: errors.New("failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isCloudWatchAccessDenied(tt.err))
		})
	}
}

func TestTrailCreate(t *testing.T) {
	oldInterval := trailRetryInterval
	trailRetryInterval = 0
	t.Cleanup(func() { trailRetryInterval = oldInterval })

	selectors := []TrailEventSelector{{
		DataResources: &[]TrailDataResource{{
			Type:   "AWS::S3::Object",
			Values: &[]string{"arn:aws:s3:::logs/"},
		}},
	}}
	insights := []TrailInsightSelector{{
		InsightType: "ApiCallRateInsight",
	}, {
		InsightType:     "ApiErrorRateInsight",
		EventCategories: &[]string{"Management", "Data"},
	}}
	aggregations := []TrailAggregationConfiguration{{
		EventCategory: "Data",
		Templates:     []string{"API_ACTIVITY", "USER_ACTIONS"},
	}}
	tags := map[string]string{"team": "core", "aws:managed": "ignored"}
	r := &TrailResource{
		Name:                      testTrailName,
		S3BucketName:              "logs-bucket",
		EventSelectors:            &selectors,
		InsightSelectors:          &insights,
		AggregationConfigurations: &aggregations,
		Tags:                      &tags,
	}
	client := newFakeTrailClient()
	client.createErrors = []error{
		&cloudtrailtypes.InvalidCloudWatchLogsRoleArnException{
			Message: aws.String("Access denied. propagating"),
		},
		nil,
	}
	client.describeOutputs = []*cloudtrail.DescribeTrailsOutput{
		{},
		{TrailList: []cloudtrailtypes.Trail{{
			TrailARN:    aws.String(testTrailArn),
			HomeRegion:  aws.String("us-east-1"),
			SnsTopicARN: aws.String("arn:aws:sns:us-east-1:123456789012:topic"),
		}}},
	}

	out, err := r.create(context.Background(), client)

	require.NoError(t, err)
	assert.Equal(t, &TrailResourceOutput{
		Arn:         testTrailArn,
		HomeRegion:  "us-east-1",
		SnsTopicArn: "arn:aws:sns:us-east-1:123456789012:topic",
	}, out)
	assert.Equal(t, []string{
		"create", "create", "start", "selectors", "insights", "aggregation",
		"describe", "describe",
	}, client.calls)
	require.Len(t, client.createInputs, 2)
	createInput := client.createInputs[1]
	assert.True(t, aws.ToBool(createInput.IncludeGlobalServiceEvents))
	assert.Equal(t, []cloudtrailtypes.Tag{{
		Key: aws.String("team"), Value: aws.String("core"),
	}}, createInput.TagsList)
	require.Len(t, client.selectorInputs, 1)
	assert.True(t, aws.ToBool(client.selectorInputs[0].EventSelectors[0].IncludeManagementEvents))
	assert.Equal(t, cloudtrailtypes.ReadWriteTypeAll,
		client.selectorInputs[0].EventSelectors[0].ReadWriteType)
	require.Len(t, client.insightInputs, 1)
	assert.Nil(t, client.insightInputs[0].InsightSelectors[0].EventCategories)
	assert.Equal(t, []cloudtrailtypes.SourceEventCategory{
		cloudtrailtypes.SourceEventCategoryManagement,
		cloudtrailtypes.SourceEventCategoryData,
	}, client.insightInputs[0].InsightSelectors[1].EventCategories)
}

func TestTrailCreateFalseLoggingAndAdvancedSelectors(t *testing.T) {
	logging := false
	selectors := []TrailAdvancedEventSelector{{
		Name: aws.String("data"),
		FieldSelectors: []TrailAdvancedFieldSelector{
			{Field: "eventCategory", Equals: &[]string{"Data"}},
			{Field: "resources.type", Equals: &[]string{"AWS::S3::Object"}},
		},
	}}
	r := &TrailResource{
		Name:                   testTrailName,
		S3BucketName:           "logs-bucket",
		EnableLogging:          &logging,
		AdvancedEventSelectors: &selectors,
	}
	client := newFakeTrailClient()

	_, err := r.create(context.Background(), client)

	require.NoError(t, err)
	assert.Equal(t, []string{"create", "selectors", "describe"}, client.calls)
	require.Len(t, client.selectorInputs, 1)
	assert.Empty(t, client.selectorInputs[0].EventSelectors)
	assert.Len(t, client.selectorInputs[0].AdvancedEventSelectors, 1)
}

func TestTrailReadNotFound(t *testing.T) {
	r := &TrailResource{Name: testTrailName}
	client := newFakeTrailClient()
	client.describeOutputs = []*cloudtrail.DescribeTrailsOutput{{}}

	out, err := r.read(context.Background(), client, testTrailArn, false)

	assert.Nil(t, out)
	require.ErrorIs(t, err, runtime.ErrNotFound)
	assert.Equal(t, []string{"describe"}, client.calls)
}

func TestTrailUpdate(t *testing.T) {
	oldInterval := trailRetryInterval
	trailRetryInterval = 0
	t.Cleanup(func() { trailRetryInterval = oldInterval })

	falseValue := false
	priorPrefix := "old"
	newGroup := "arn:aws:logs:us-east-1:123456789012:log-group:new:*"
	priorRole := "arn:aws:iam::123456789012:role/old"
	advanced := []TrailAdvancedEventSelector{}
	insights := []TrailInsightSelector{}
	aggregations := []TrailAggregationConfiguration{}
	tags := map[string]string{"changed": "new", "new": "value", "aws:new": "ignored"}
	r := &TrailResource{
		Name:                       testTrailName,
		S3BucketName:               "logs-bucket",
		S3KeyPrefix:                nil,
		CloudWatchLogsLogGroupArn:  &newGroup,
		CloudWatchLogsRoleArn:      nil,
		IncludeGlobalServiceEvents: aws.Bool(true),
		EnableLogging:              &falseValue,
		AdvancedEventSelectors:     &advanced,
		InsightSelectors:           &insights,
		AggregationConfigurations:  &aggregations,
		Tags:                       &tags,
	}
	prior := runtime.Prior[TrailResource, *TrailResourceOutput]{
		Inputs: TrailResource{
			Name:                       testTrailName,
			S3BucketName:               "logs-bucket",
			S3KeyPrefix:                &priorPrefix,
			CloudWatchLogsRoleArn:      &priorRole,
			IncludeGlobalServiceEvents: aws.Bool(true),
			EnableLogging:              aws.Bool(true),
			EventSelectors:             &[]TrailEventSelector{{}},
			InsightSelectors:           &[]TrailInsightSelector{{InsightType: "ApiCallRateInsight"}},
			AggregationConfigurations: &[]TrailAggregationConfiguration{{
				EventCategory: "Data", Templates: []string{"API_ACTIVITY"},
			}},
			Tags: &map[string]string{"changed": "old", "remove": "value"},
		},
		Outputs: &TrailResourceOutput{Arn: testTrailArn},
	}
	client := newFakeTrailClient()
	client.listTagsOutput = &cloudtrail.ListTagsOutput{ResourceTagList: []cloudtrailtypes.ResourceTag{{
		ResourceId: aws.String(testTrailArn),
		TagsList: []cloudtrailtypes.Tag{
			{Key: aws.String("changed"), Value: aws.String("old")},
			{Key: aws.String("remove"), Value: aws.String("value")},
			{Key: aws.String("aws:managed"), Value: aws.String("keep")},
		},
	}}}
	client.updateErrors = []error{
		&cloudtrailtypes.InvalidCloudWatchLogsLogGroupArnException{
			Message: aws.String("Access denied. propagating"),
		},
		nil,
	}

	out, err := r.update(context.Background(), client, prior)

	require.NoError(t, err)
	assert.Equal(t, testTrailArn, out.Arn)
	assert.Equal(t, []string{
		"list-tags", "remove-tags", "add-tags", "update", "update", "stop",
		"insights", "selectors", "aggregation", "describe",
	}, client.calls)
	require.Len(t, client.updateInputs, 2)
	updateInput := client.updateInputs[1]
	assert.Equal(t, "", aws.ToString(updateInput.S3KeyPrefix))
	assert.Equal(t, newGroup, aws.ToString(updateInput.CloudWatchLogsLogGroupArn))
	assert.Equal(t, "", aws.ToString(updateInput.CloudWatchLogsRoleArn))
	assert.Nil(t, updateInput.IncludeGlobalServiceEvents)
	require.Len(t, client.removeTagInputs, 1)
	assert.Equal(t, []cloudtrailtypes.Tag{{Key: aws.String("remove")}},
		client.removeTagInputs[0].TagsList)
	require.Len(t, client.addTagInputs, 1)
	assert.Equal(t, []cloudtrailtypes.Tag{
		{Key: aws.String("changed"), Value: aws.String("new")},
		{Key: aws.String("new"), Value: aws.String("value")},
	}, client.addTagInputs[0].TagsList)
	require.Len(t, client.selectorInputs, 1)
	assert.NotNil(t, client.selectorInputs[0].AdvancedEventSelectors)
	assert.Empty(t, client.selectorInputs[0].AdvancedEventSelectors)
	require.Len(t, client.insightInputs, 1)
	assert.NotNil(t, client.insightInputs[0].InsightSelectors)
	assert.Empty(t, client.insightInputs[0].InsightSelectors)
	assert.NotNil(t, client.aggregationInputs[0].AggregationConfigurations)
	assert.Empty(t, client.aggregationInputs[0].AggregationConfigurations)
}

func TestTrailUpdateUnchangedReadsOnly(t *testing.T) {
	r := &TrailResource{Name: testTrailName, S3BucketName: "logs-bucket"}
	prior := runtime.Prior[TrailResource, *TrailResourceOutput]{
		Inputs:  *r,
		Outputs: &TrailResourceOutput{Arn: testTrailArn},
	}
	client := newFakeTrailClient()

	_, err := r.update(context.Background(), client, prior)

	require.NoError(t, err)
	assert.Equal(t, []string{"describe"}, client.calls)
}

func TestTrailUpdateDirectFieldGatesAndClears(t *testing.T) {
	group := "arn:aws:logs:us-east-1:123456789012:log-group:old"
	role := "arn:aws:iam::123456789012:role/old"
	kms := "arn:aws:kms:us-east-1:123456789012:key/old"
	prefix := "old"
	topic := "old-topic"
	base := TrailResource{
		Name:                       testTrailName,
		S3BucketName:               "logs-bucket",
		S3KeyPrefix:                &prefix,
		SnsTopicName:               &topic,
		CloudWatchLogsLogGroupArn:  &group,
		CloudWatchLogsRoleArn:      &role,
		KmsKeyId:                   &kms,
		EnableLogFileValidation:    aws.Bool(false),
		IncludeGlobalServiceEvents: aws.Bool(true),
		IsMultiRegionTrail:         aws.Bool(false),
		IsOrganizationTrail:        aws.Bool(false),
	}
	tests := []struct {
		name     string
		mutate   func(*TrailResource)
		expected *cloudtrail.UpdateTrailInput
	}{
		{
			name:   "S3 bucket",
			mutate: func(r *TrailResource) { r.S3BucketName = "new-bucket" },
			expected: &cloudtrail.UpdateTrailInput{
				Name: aws.String(testTrailArn), S3BucketName: aws.String("new-bucket"),
			},
		},
		{
			name:   "S3 prefix clear",
			mutate: func(r *TrailResource) { r.S3KeyPrefix = nil },
			expected: &cloudtrail.UpdateTrailInput{
				Name: aws.String(testTrailArn), S3KeyPrefix: aws.String(""),
			},
		},
		{
			name:   "SNS topic clear",
			mutate: func(r *TrailResource) { r.SnsTopicName = nil },
			expected: &cloudtrail.UpdateTrailInput{
				Name: aws.String(testTrailArn), SnsTopicName: aws.String(""),
			},
		},
		{
			name:   "KMS key clear",
			mutate: func(r *TrailResource) { r.KmsKeyId = nil },
			expected: &cloudtrail.UpdateTrailInput{
				Name: aws.String(testTrailArn), KmsKeyId: aws.String(""),
			},
		},
		{
			name: "CloudWatch group change sends pair",
			mutate: func(r *TrailResource) {
				r.CloudWatchLogsLogGroupArn = aws.String(
					"arn:aws:logs:us-west-2:123456789012:log-group:new")
			},
			expected: &cloudtrail.UpdateTrailInput{
				Name: aws.String(testTrailArn),
				CloudWatchLogsLogGroupArn: aws.String(
					"arn:aws:logs:us-west-2:123456789012:log-group:new"),
				CloudWatchLogsRoleArn: aws.String(role),
			},
		},
		{
			name:   "CloudWatch group clear sends retained role",
			mutate: func(r *TrailResource) { r.CloudWatchLogsLogGroupArn = nil },
			expected: &cloudtrail.UpdateTrailInput{
				Name:                      aws.String(testTrailArn),
				CloudWatchLogsLogGroupArn: aws.String(""),
				CloudWatchLogsRoleArn:     aws.String(role),
			},
		},
		{
			name:   "CloudWatch role clear sends pair",
			mutate: func(r *TrailResource) { r.CloudWatchLogsRoleArn = nil },
			expected: &cloudtrail.UpdateTrailInput{
				Name:                      aws.String(testTrailArn),
				CloudWatchLogsLogGroupArn: aws.String(group),
				CloudWatchLogsRoleArn:     aws.String(""),
			},
		},
		{
			name:   "log validation",
			mutate: func(r *TrailResource) { r.EnableLogFileValidation = aws.Bool(true) },
			expected: &cloudtrail.UpdateTrailInput{
				Name: aws.String(testTrailArn), EnableLogFileValidation: aws.Bool(true),
			},
		},
		{
			name:   "global service events",
			mutate: func(r *TrailResource) { r.IncludeGlobalServiceEvents = aws.Bool(false) },
			expected: &cloudtrail.UpdateTrailInput{
				Name: aws.String(testTrailArn), IncludeGlobalServiceEvents: aws.Bool(false),
			},
		},
		{
			name:   "multi region",
			mutate: func(r *TrailResource) { r.IsMultiRegionTrail = aws.Bool(true) },
			expected: &cloudtrail.UpdateTrailInput{
				Name: aws.String(testTrailArn), IsMultiRegionTrail: aws.Bool(true),
			},
		},
		{
			name:   "organization",
			mutate: func(r *TrailResource) { r.IsOrganizationTrail = aws.Bool(true) },
			expected: &cloudtrail.UpdateTrailInput{
				Name: aws.String(testTrailArn), IsOrganizationTrail: aws.Bool(true),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := base
			tt.mutate(&current)
			client := newFakeTrailClient()
			_, err := current.update(context.Background(), client,
				runtime.Prior[TrailResource, *TrailResourceOutput]{
					Inputs: base, Outputs: &TrailResourceOutput{Arn: testTrailArn},
				})
			require.NoError(t, err)
			assert.Equal(t, []string{"update", "describe"}, client.calls)
			require.Len(t, client.updateInputs, 1)
			assert.Equal(t, tt.expected, client.updateInputs[0])
		})
	}
}

func TestTrailUpdateLoggingTransitions(t *testing.T) {
	tests := []struct {
		name     string
		prior    bool
		current  bool
		expected []string
	}{
		{name: "start", prior: false, current: true, expected: []string{"start", "describe"}},
		{name: "stop", prior: true, current: false, expected: []string{"stop", "describe"}},
		{name: "unchanged", prior: true, current: true, expected: []string{"describe"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &TrailResource{
				Name: testTrailName, S3BucketName: "logs-bucket",
				EnableLogging: aws.Bool(tt.current),
			}
			prior := runtime.Prior[TrailResource, *TrailResourceOutput]{
				Inputs: TrailResource{
					Name: testTrailName, S3BucketName: "logs-bucket",
					EnableLogging: aws.Bool(tt.prior),
				},
				Outputs: &TrailResourceOutput{Arn: testTrailArn},
			}
			client := newFakeTrailClient()
			_, err := r.update(context.Background(), client, prior)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, client.calls)
		})
	}
}

func TestTrailUpdateSelectorTransitions(t *testing.T) {
	readOnly := "ReadOnly"
	standard := []TrailEventSelector{{ReadWriteType: &readOnly}}
	advanced := []TrailAdvancedEventSelector{{
		Name: aws.String("data"),
		FieldSelectors: []TrailAdvancedFieldSelector{
			{Field: "eventCategory", Equals: &[]string{"Data"}},
			{Field: "resources.type", Equals: &[]string{"AWS::S3::Object"}},
		},
	}}
	tests := []struct {
		name    string
		prior   TrailResource
		current TrailResource
		check   func(*testing.T, []*cloudtrail.PutEventSelectorsInput)
	}{
		{
			name:  "standard clear",
			prior: TrailResource{EventSelectors: &standard},
			check: func(t *testing.T, inputs []*cloudtrail.PutEventSelectorsInput) {
				require.Len(t, inputs, 1)
				assert.Equal(t, cloudtrailtypes.ReadWriteTypeAll,
					inputs[0].EventSelectors[0].ReadWriteType)
				assert.Empty(t, inputs[0].EventSelectors[0].DataResources)
			},
		},
		{
			name:  "advanced clear",
			prior: TrailResource{AdvancedEventSelectors: &advanced},
			check: func(t *testing.T, inputs []*cloudtrail.PutEventSelectorsInput) {
				require.Len(t, inputs, 1)
				assert.NotNil(t, inputs[0].AdvancedEventSelectors)
				assert.Empty(t, inputs[0].AdvancedEventSelectors)
			},
		},
		{
			name:    "standard to advanced",
			prior:   TrailResource{EventSelectors: &standard},
			current: TrailResource{AdvancedEventSelectors: &advanced},
			check: func(t *testing.T, inputs []*cloudtrail.PutEventSelectorsInput) {
				require.Len(t, inputs, 1)
				assert.Nil(t, inputs[0].EventSelectors)
				assert.Equal(t, "data",
					aws.ToString(inputs[0].AdvancedEventSelectors[0].Name))
			},
		},
		{
			name:    "advanced to standard",
			prior:   TrailResource{AdvancedEventSelectors: &advanced},
			current: TrailResource{EventSelectors: &standard},
			check: func(t *testing.T, inputs []*cloudtrail.PutEventSelectorsInput) {
				require.Len(t, inputs, 1)
				assert.Equal(t, cloudtrailtypes.ReadWriteTypeReadOnly,
					inputs[0].EventSelectors[0].ReadWriteType)
				assert.Nil(t, inputs[0].AdvancedEventSelectors)
			},
		},
		{
			name:    "unchanged",
			prior:   TrailResource{EventSelectors: &standard},
			current: TrailResource{EventSelectors: &standard},
			check: func(t *testing.T, inputs []*cloudtrail.PutEventSelectorsInput) {
				assert.Empty(t, inputs)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.current
			r.Name = testTrailName
			r.S3BucketName = "logs-bucket"
			priorInputs := tt.prior
			priorInputs.Name = testTrailName
			priorInputs.S3BucketName = "logs-bucket"
			client := newFakeTrailClient()
			_, err := r.update(context.Background(), client,
				runtime.Prior[TrailResource, *TrailResourceOutput]{
					Inputs: priorInputs, Outputs: &TrailResourceOutput{Arn: testTrailArn},
				})
			require.NoError(t, err)
			tt.check(t, client.selectorInputs)
		})
	}
}

func TestTrailUpdateSelectorInsightOrdering(t *testing.T) {
	management := []string{"Management"}
	data := []string{"Data"}
	both := []string{"Management", "Data"}
	standardManagement := []TrailEventSelector{{}}
	standardData := []TrailEventSelector{{
		DataResources: &[]TrailDataResource{{
			Type: "AWS::S3::Object", Values: &[]string{"arn:aws:s3:::logs/"},
		}},
	}}
	advancedData := []TrailAdvancedEventSelector{{
		Name: aws.String("data"),
		FieldSelectors: []TrailAdvancedFieldSelector{
			{Field: "eventCategory", Equals: &data},
			{Field: "resources.type", Equals: &[]string{"AWS::S3::Object"}},
		},
	}}
	managementInsight := TrailInsightSelector{
		InsightType: "ApiCallRateInsight", EventCategories: &management,
	}
	dataInsight := TrailInsightSelector{
		InsightType: "ApiErrorRateInsight", EventCategories: &data,
	}
	bothInsight := TrailInsightSelector{
		InsightType: "ApiErrorRateInsight", EventCategories: &both,
	}
	assert.False(t, (trailEventCoverage{data: true}).supportsAll(
		[]TrailInsightSelector{bothInsight}))
	tests := []struct {
		name             string
		prior            TrailResource
		current          TrailResource
		expectedCalls    []string
		expectedInsights [][]cloudtrailtypes.InsightSelector
		advanced         bool
	}{
		{
			name: "standard data to advanced data",
			prior: TrailResource{
				EventSelectors:   &standardData,
				InsightSelectors: &[]TrailInsightSelector{bothInsight},
			},
			current: TrailResource{
				AdvancedEventSelectors: &advancedData,
				InsightSelectors:       &[]TrailInsightSelector{dataInsight},
			},
			expectedCalls: []string{"insights", "selectors", "describe"},
			expectedInsights: [][]cloudtrailtypes.InsightSelector{{{
				InsightType: cloudtrailtypes.InsightTypeApiErrorRateInsight,
				EventCategories: []cloudtrailtypes.SourceEventCategory{
					cloudtrailtypes.SourceEventCategoryData,
				},
			}}},
			advanced: true,
		},
		{
			name: "coverage removal applies insights first",
			prior: TrailResource{
				EventSelectors: &standardData,
				InsightSelectors: &[]TrailInsightSelector{
					managementInsight, dataInsight,
				},
			},
			current: TrailResource{
				AdvancedEventSelectors: &advancedData,
				InsightSelectors:       &[]TrailInsightSelector{dataInsight},
			},
			expectedCalls: []string{"insights", "selectors", "describe"},
			expectedInsights: [][]cloudtrailtypes.InsightSelector{{{
				InsightType: cloudtrailtypes.InsightTypeApiErrorRateInsight,
				EventCategories: []cloudtrailtypes.SourceEventCategory{
					cloudtrailtypes.SourceEventCategoryData,
				},
			}}},
			advanced: true,
		},
		{
			name: "new coverage applies selectors first",
			prior: TrailResource{
				EventSelectors:   &standardManagement,
				InsightSelectors: &[]TrailInsightSelector{managementInsight},
			},
			current: TrailResource{
				EventSelectors: &standardData,
				InsightSelectors: &[]TrailInsightSelector{
					managementInsight, dataInsight,
				},
			},
			expectedCalls: []string{"selectors", "insights", "describe"},
			expectedInsights: [][]cloudtrailtypes.InsightSelector{{
				{
					InsightType: cloudtrailtypes.InsightTypeApiCallRateInsight,
					EventCategories: []cloudtrailtypes.SourceEventCategory{
						cloudtrailtypes.SourceEventCategoryManagement,
					},
				},
				{
					InsightType: cloudtrailtypes.InsightTypeApiErrorRateInsight,
					EventCategories: []cloudtrailtypes.SourceEventCategory{
						cloudtrailtypes.SourceEventCategoryData,
					},
				},
			}},
		},
		{
			name: "cross dependent transition disables insights",
			prior: TrailResource{
				EventSelectors:   &standardManagement,
				InsightSelectors: &[]TrailInsightSelector{managementInsight},
			},
			current: TrailResource{
				AdvancedEventSelectors: &advancedData,
				InsightSelectors:       &[]TrailInsightSelector{dataInsight},
			},
			expectedCalls: []string{"insights", "selectors", "insights", "describe"},
			expectedInsights: [][]cloudtrailtypes.InsightSelector{
				{},
				{{
					InsightType: cloudtrailtypes.InsightTypeApiErrorRateInsight,
					EventCategories: []cloudtrailtypes.SourceEventCategory{
						cloudtrailtypes.SourceEventCategoryData,
					},
				}},
			},
			advanced: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := tt.current
			current.Name = testTrailName
			current.S3BucketName = "logs-bucket"
			prior := tt.prior
			prior.Name = testTrailName
			prior.S3BucketName = "logs-bucket"
			client := newFakeTrailClient()

			_, err := current.update(context.Background(), client,
				runtime.Prior[TrailResource, *TrailResourceOutput]{
					Inputs: prior, Outputs: &TrailResourceOutput{Arn: testTrailArn},
				})

			require.NoError(t, err)
			assert.Equal(t, tt.expectedCalls, client.calls)
			require.Len(t, client.selectorInputs, 1)
			if tt.advanced {
				assert.Nil(t, client.selectorInputs[0].EventSelectors)
				assert.Equal(t, "data",
					aws.ToString(client.selectorInputs[0].AdvancedEventSelectors[0].Name))
			} else {
				assert.Nil(t, client.selectorInputs[0].AdvancedEventSelectors)
				assert.NotEmpty(t, client.selectorInputs[0].EventSelectors)
			}
			require.Len(t, client.insightInputs, len(tt.expectedInsights))
			for i, expected := range tt.expectedInsights {
				assert.Equal(t, expected, client.insightInputs[i].InsightSelectors)
			}
		})
	}
}

func TestTrailUpdateInsightsAndAggregation(t *testing.T) {
	management := []string{"Management"}
	data := []string{"Data"}
	both := []string{"Management", "Data"}
	for _, categories := range []*[]string{nil, &management, &data, &both} {
		got := expandInsightSelectors([]TrailInsightSelector{{
			InsightType: "ApiErrorRateInsight", EventCategories: categories,
		}})
		require.Len(t, got, 1)
		assert.Equal(t, cloudtrailtypes.InsightTypeApiErrorRateInsight, got[0].InsightType)
		if categories == nil {
			assert.Nil(t, got[0].EventCategories)
		} else {
			assert.Len(t, got[0].EventCategories, len(*categories))
		}
	}

	priorInsights := []TrailInsightSelector{
		{InsightType: "ApiCallRateInsight", EventCategories: &management},
		{InsightType: "ApiErrorRateInsight", EventCategories: &both},
	}
	desiredInsights := []TrailInsightSelector{{
		InsightType: "ApiErrorRateInsight", EventCategories: &data,
	}}
	priorAggregation := []TrailAggregationConfiguration{{
		EventCategory: "Data", Templates: []string{"API_ACTIVITY"},
	}}
	desiredAggregation := []TrailAggregationConfiguration{{
		EventCategory: "Data", Templates: []string{"RESOURCE_ACCESS", "USER_ACTIONS"},
	}}
	r := &TrailResource{
		Name: testTrailName, S3BucketName: "logs-bucket",
		InsightSelectors: &desiredInsights, AggregationConfigurations: &desiredAggregation,
	}
	client := newFakeTrailClient()
	_, err := r.update(context.Background(), client,
		runtime.Prior[TrailResource, *TrailResourceOutput]{
			Inputs: TrailResource{
				Name: testTrailName, S3BucketName: "logs-bucket",
				InsightSelectors:          &priorInsights,
				AggregationConfigurations: &priorAggregation,
			},
			Outputs: &TrailResourceOutput{Arn: testTrailArn},
		})
	require.NoError(t, err)
	assert.Equal(t, []string{"insights", "aggregation", "describe"}, client.calls)
	require.Len(t, client.insightInputs, 1)
	assert.Equal(t, []cloudtrailtypes.InsightSelector{{
		InsightType: cloudtrailtypes.InsightTypeApiErrorRateInsight,
		EventCategories: []cloudtrailtypes.SourceEventCategory{
			cloudtrailtypes.SourceEventCategoryData,
		},
	}}, client.insightInputs[0].InsightSelectors)
	require.Len(t, client.aggregationInputs, 1)
	assert.Equal(t, []cloudtrailtypes.AggregationConfiguration{{
		EventCategory: cloudtrailtypes.EventCategoryAggregationData,
		Templates: []cloudtrailtypes.Template{
			cloudtrailtypes.TemplateResourceAccess,
			cloudtrailtypes.TemplateUserActions,
		},
	}}, client.aggregationInputs[0].AggregationConfigurations)
}

func TestTrailFollowOnErrorsAreNotRetried(t *testing.T) {
	sentinel := errors.New("follow-on failed")
	tests := []struct {
		name      string
		resource  TrailResource
		prior     TrailResource
		configure func(*fakeTrailClient)
		call      string
	}{
		{
			name:     "start logging",
			resource: TrailResource{EnableLogging: aws.Bool(true)},
			prior:    TrailResource{EnableLogging: aws.Bool(false)},
			configure: func(client *fakeTrailClient) {
				client.startErrors = []error{sentinel}
			},
			call: "start",
		},
		{
			name:     "stop logging",
			resource: TrailResource{EnableLogging: aws.Bool(false)},
			prior:    TrailResource{EnableLogging: aws.Bool(true)},
			configure: func(client *fakeTrailClient) {
				client.stopErrors = []error{sentinel}
			},
			call: "stop",
		},
		{
			name:     "selectors",
			resource: TrailResource{EventSelectors: &[]TrailEventSelector{{}}},
			configure: func(client *fakeTrailClient) {
				client.selectorErrors = []error{sentinel}
			},
			call: "selectors",
		},
		{
			name: "Insights",
			resource: TrailResource{InsightSelectors: &[]TrailInsightSelector{{
				InsightType: "ApiCallRateInsight",
			}}},
			configure: func(client *fakeTrailClient) {
				client.insightErrors = []error{sentinel}
			},
			call: "insights",
		},
		{
			name: "aggregation",
			resource: TrailResource{
				AggregationConfigurations: &[]TrailAggregationConfiguration{{
					EventCategory: "Data", Templates: []string{"API_ACTIVITY"},
				}},
			},
			configure: func(client *fakeTrailClient) {
				client.aggregationErrors = []error{sentinel}
			},
			call: "aggregation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.resource
			r.Name = testTrailName
			r.S3BucketName = "logs-bucket"
			prior := tt.prior
			prior.Name = testTrailName
			prior.S3BucketName = "logs-bucket"
			client := newFakeTrailClient()
			tt.configure(client)
			_, err := r.update(context.Background(), client,
				runtime.Prior[TrailResource, *TrailResourceOutput]{
					Inputs: prior, Outputs: &TrailResourceOutput{Arn: testTrailArn},
				})
			require.ErrorIs(t, err, sentinel)
			assert.Equal(t, []string{tt.call}, client.calls)
		})
	}
}

func TestTrailConfiguredAggregationUnchangedIsNoOp(t *testing.T) {
	aggregations := []TrailAggregationConfiguration{{
		EventCategory: "Data", Templates: []string{"API_ACTIVITY"},
	}}
	r := &TrailResource{
		Name: testTrailName, S3BucketName: "logs-bucket",
		AggregationConfigurations: &aggregations,
	}
	client := newFakeTrailClient()
	_, err := r.update(context.Background(), client,
		runtime.Prior[TrailResource, *TrailResourceOutput]{
			Inputs: *r, Outputs: &TrailResourceOutput{Arn: testTrailArn},
		})
	require.NoError(t, err)
	assert.Equal(t, []string{"describe"}, client.calls)
}

func TestTrailInsightServicePrerequisiteErrorIsNotRetried(t *testing.T) {
	insights := []TrailInsightSelector{{
		InsightType:     "ApiCallRateInsight",
		EventCategories: &[]string{"Management"},
	}}
	r := &TrailResource{
		Name: testTrailName, S3BucketName: "logs-bucket", InsightSelectors: &insights,
	}
	serviceErr := &cloudtrailtypes.InvalidInsightSelectorsException{
		Message: aws.String("trail must log write management events"),
	}
	client := newFakeTrailClient()
	client.insightErrors = []error{serviceErr}
	_, err := r.update(context.Background(), client,
		runtime.Prior[TrailResource, *TrailResourceOutput]{
			Inputs:  TrailResource{Name: testTrailName, S3BucketName: "logs-bucket"},
			Outputs: &TrailResourceOutput{Arn: testTrailArn},
		})
	require.ErrorIs(t, err, serviceErr)
	assert.Equal(t, []string{"insights"}, client.calls)
}

func TestTrailCreateNonRetryableCloudWatchErrorReturnsImmediately(t *testing.T) {
	r := &TrailResource{Name: testTrailName, S3BucketName: "logs-bucket"}
	client := newFakeTrailClient()
	sentinel := &cloudtrailtypes.InvalidCloudWatchLogsRoleArnException{
		Message: aws.String("invalid role"),
	}
	client.createErrors = []error{sentinel}

	_, err := r.create(context.Background(), client)

	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, []string{"create"}, client.calls)
}

func TestTrailTagClear(t *testing.T) {
	desired := map[string]string{}
	r := &TrailResource{Name: testTrailName, S3BucketName: "logs-bucket", Tags: &desired}
	priorTags := map[string]string{"one": "1", "two": "2"}
	client := newFakeTrailClient()
	client.listTagsOutput = &cloudtrail.ListTagsOutput{ResourceTagList: []cloudtrailtypes.ResourceTag{{
		TagsList: []cloudtrailtypes.Tag{
			{Key: aws.String("one"), Value: aws.String("1")},
			{Key: aws.String("two"), Value: aws.String("2")},
			{Key: aws.String("aws:system"), Value: aws.String("keep")},
		},
	}}}

	_, err := r.update(context.Background(), client,
		runtime.Prior[TrailResource, *TrailResourceOutput]{
			Inputs: TrailResource{
				Name: testTrailName, S3BucketName: "logs-bucket", Tags: &priorTags,
			},
			Outputs: &TrailResourceOutput{Arn: testTrailArn},
		})

	require.NoError(t, err)
	assert.Equal(t, []string{"list-tags", "remove-tags", "describe"}, client.calls)
	assert.Equal(t, []cloudtrailtypes.Tag{
		{Key: aws.String("one")}, {Key: aws.String("two")},
	}, client.removeTagInputs[0].TagsList)
}

func TestTrailDelete(t *testing.T) {
	r := &TrailResource{Name: testTrailName}
	client := newFakeTrailClient()
	client.deleteErr = &cloudtrailtypes.TrailNotFoundException{}

	err := r.delete(context.Background(), client, &TrailResourceOutput{Arn: testTrailArn})

	require.NoError(t, err)
	require.Len(t, client.deleteInputs, 1)
	assert.Equal(t, testTrailArn, aws.ToString(client.deleteInputs[0].Name))
}

type fakeTrailClient struct {
	calls             []string
	createInputs      []*cloudtrail.CreateTrailInput
	createErrors      []error
	describeInputs    []*cloudtrail.DescribeTrailsInput
	describeOutputs   []*cloudtrail.DescribeTrailsOutput
	describeErr       error
	updateInputs      []*cloudtrail.UpdateTrailInput
	updateErrors      []error
	deleteInputs      []*cloudtrail.DeleteTrailInput
	deleteErr         error
	startInputs       []*cloudtrail.StartLoggingInput
	startErrors       []error
	stopInputs        []*cloudtrail.StopLoggingInput
	stopErrors        []error
	selectorInputs    []*cloudtrail.PutEventSelectorsInput
	selectorErrors    []error
	insightInputs     []*cloudtrail.PutInsightSelectorsInput
	insightErrors     []error
	aggregationInputs []*cloudtrail.PutEventConfigurationInput
	aggregationErrors []error
	listTagsOutput    *cloudtrail.ListTagsOutput
	addTagInputs      []*cloudtrail.AddTagsInput
	removeTagInputs   []*cloudtrail.RemoveTagsInput
}

func newFakeTrailClient() *fakeTrailClient {
	return &fakeTrailClient{
		describeOutputs: []*cloudtrail.DescribeTrailsOutput{
			{
				TrailList: []cloudtrailtypes.Trail{{
					TrailARN:   aws.String(testTrailArn),
					HomeRegion: aws.String("us-east-1"),
				}},
			},
		},
		listTagsOutput: &cloudtrail.ListTagsOutput{},
	}
}

func (f *fakeTrailClient) CreateTrail(
	_ context.Context,
	in *cloudtrail.CreateTrailInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.CreateTrailOutput, error) {
	f.calls = append(f.calls, "create")
	f.createInputs = append(f.createInputs, in)
	err := nextError(&f.createErrors)
	if err != nil {
		return nil, err
	}
	return &cloudtrail.CreateTrailOutput{TrailARN: aws.String(testTrailArn)}, nil
}

func (f *fakeTrailClient) DescribeTrails(
	_ context.Context,
	in *cloudtrail.DescribeTrailsInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.DescribeTrailsOutput, error) {
	f.calls = append(f.calls, "describe")
	f.describeInputs = append(f.describeInputs, in)
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	if len(f.describeOutputs) == 0 {
		return &cloudtrail.DescribeTrailsOutput{}, nil
	}
	out := f.describeOutputs[0]
	f.describeOutputs = f.describeOutputs[1:]
	return out, nil
}

func (f *fakeTrailClient) UpdateTrail(
	_ context.Context,
	in *cloudtrail.UpdateTrailInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.UpdateTrailOutput, error) {
	f.calls = append(f.calls, "update")
	f.updateInputs = append(f.updateInputs, in)
	if err := nextError(&f.updateErrors); err != nil {
		return nil, err
	}
	return &cloudtrail.UpdateTrailOutput{}, nil
}

func (f *fakeTrailClient) DeleteTrail(
	_ context.Context,
	in *cloudtrail.DeleteTrailInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.DeleteTrailOutput, error) {
	f.calls = append(f.calls, "delete")
	f.deleteInputs = append(f.deleteInputs, in)
	return &cloudtrail.DeleteTrailOutput{}, f.deleteErr
}

func (f *fakeTrailClient) StartLogging(
	_ context.Context,
	in *cloudtrail.StartLoggingInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.StartLoggingOutput, error) {
	f.calls = append(f.calls, "start")
	f.startInputs = append(f.startInputs, in)
	if err := nextError(&f.startErrors); err != nil {
		return nil, err
	}
	return &cloudtrail.StartLoggingOutput{}, nil
}

func (f *fakeTrailClient) StopLogging(
	_ context.Context,
	in *cloudtrail.StopLoggingInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.StopLoggingOutput, error) {
	f.calls = append(f.calls, "stop")
	f.stopInputs = append(f.stopInputs, in)
	if err := nextError(&f.stopErrors); err != nil {
		return nil, err
	}
	return &cloudtrail.StopLoggingOutput{}, nil
}

func (f *fakeTrailClient) PutEventSelectors(
	_ context.Context,
	in *cloudtrail.PutEventSelectorsInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.PutEventSelectorsOutput, error) {
	f.calls = append(f.calls, "selectors")
	f.selectorInputs = append(f.selectorInputs, in)
	if err := nextError(&f.selectorErrors); err != nil {
		return nil, err
	}
	return &cloudtrail.PutEventSelectorsOutput{}, nil
}

func (f *fakeTrailClient) PutInsightSelectors(
	_ context.Context,
	in *cloudtrail.PutInsightSelectorsInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.PutInsightSelectorsOutput, error) {
	f.calls = append(f.calls, "insights")
	f.insightInputs = append(f.insightInputs, in)
	if err := nextError(&f.insightErrors); err != nil {
		return nil, err
	}
	return &cloudtrail.PutInsightSelectorsOutput{}, nil
}

func (f *fakeTrailClient) PutEventConfiguration(
	_ context.Context,
	in *cloudtrail.PutEventConfigurationInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.PutEventConfigurationOutput, error) {
	f.calls = append(f.calls, "aggregation")
	f.aggregationInputs = append(f.aggregationInputs, in)
	if err := nextError(&f.aggregationErrors); err != nil {
		return nil, err
	}
	return &cloudtrail.PutEventConfigurationOutput{}, nil
}

func (f *fakeTrailClient) ListTags(
	context.Context, *cloudtrail.ListTagsInput, ...func(*cloudtrail.Options),
) (*cloudtrail.ListTagsOutput, error) {
	f.calls = append(f.calls, "list-tags")
	return f.listTagsOutput, nil
}

func (f *fakeTrailClient) AddTags(
	_ context.Context,
	in *cloudtrail.AddTagsInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.AddTagsOutput, error) {
	f.calls = append(f.calls, "add-tags")
	f.addTagInputs = append(f.addTagInputs, in)
	return &cloudtrail.AddTagsOutput{}, nil
}

func (f *fakeTrailClient) RemoveTags(
	_ context.Context,
	in *cloudtrail.RemoveTagsInput,
	_ ...func(*cloudtrail.Options),
) (*cloudtrail.RemoveTagsOutput, error) {
	f.calls = append(f.calls, "remove-tags")
	f.removeTagInputs = append(f.removeTagInputs, in)
	return &cloudtrail.RemoveTagsOutput{}, nil
}

func nextError(errs *[]error) error {
	if len(*errs) == 0 {
		return nil
	}
	err := (*errs)[0]
	*errs = (*errs)[1:]
	return err
}
