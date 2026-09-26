package dynamodb

import (
	"github.com/cloudboss/unobin/pkg/runtime"
)

func (r *TableResource) ResourceDefinition() runtime.ResourceDefinition[TableResource, *TableResourceOutput, *awsCfg] {
	return runtime.ResourceDefinition[TableResource, *TableResourceOutput, *awsCfg]{
		SchemaVersion: r.SchemaVersion(),
		Replace: runtime.Replacement[TableResource, *TableResourceOutput, *awsCfg]{
			Fields: []runtime.AnyInputField[TableResource]{
				runtime.InputField(func(input *TableResource) *string { return &input.Name }),
				runtime.InputField(func(input *TableResource) *string { return &input.HashKey }),
				runtime.InputField(func(input *TableResource) **string { return &input.RangeKey }),
				runtime.InputField(func(input *TableResource) **[]TableLocalSecondaryIndex { return &input.LocalSecondaryIndex }),
			},
		},
	}
}
