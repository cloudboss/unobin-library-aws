package library_test

import (
	"sort"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// normalizeType sorts the fields of every object type it contains by name.
// goschema builds an object type's fields by ranging a map, so their order
// varies from one read to the next; sorting makes a schema comparison stable
// regardless. Remove once goschema emits object fields in a fixed order.
func normalizeType(t typecheck.Type) typecheck.Type {
	if t.Elem != nil {
		e := normalizeType(*t.Elem)
		t.Elem = &e
	}
	if t.Elems != nil {
		elems := make([]typecheck.Type, len(t.Elems))
		for i, e := range t.Elems {
			elems[i] = normalizeType(e)
		}
		t.Elems = elems
	}
	if t.Fields != nil {
		fields := make([]typecheck.ObjectField, len(t.Fields))
		for i, f := range t.Fields {
			f.Type = normalizeType(f.Type)
			fields[i] = f
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		t.Fields = fields
	}
	return t
}

// normalizeSchema returns a copy of s with every object type's fields sorted, so
// two reads of the same source compare equal despite goschema's varying field
// order. See normalizeType.
func normalizeSchema(s *runtime.TypeSchema) *runtime.TypeSchema {
	norm := func(m map[string]typecheck.Type) map[string]typecheck.Type {
		if m == nil {
			return nil
		}
		out := make(map[string]typecheck.Type, len(m))
		for k, v := range m {
			out[k] = normalizeType(v)
		}
		return out
	}
	return &runtime.TypeSchema{
		Inputs:           norm(s.Inputs),
		Outputs:          norm(s.Outputs),
		SensitiveInputs:  s.SensitiveInputs,
		SensitiveOutputs: s.SensitiveOutputs,
		Constraints:      s.Constraints,
	}
}
