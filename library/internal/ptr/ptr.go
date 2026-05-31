// Package ptr narrows optional integer and float inputs to the widths
// the AWS SDK expects while preserving nil, so an unset value stays
// absent from the request rather than arriving as a zero.
package ptr

// Int32 narrows a *int64 to *int32, preserving nil.
func Int32(v *int64) *int32 {
	if v == nil {
		return nil
	}
	n := int32(*v)
	return &n
}

// Float32 narrows a *float64 to *float32, preserving nil.
func Float32(v *float64) *float32 {
	if v == nil {
		return nil
	}
	n := float32(*v)
	return &n
}
