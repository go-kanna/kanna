// Package optional mimics the proto3-optional shape: a pointer field whose
// nil-ness is meaningful, wrapped in a getter that returns a value.
package optional

// Wire is the generated-looking side.
type Wire struct {
	Note *string
}

// GetNote is nil-safe and therefore loses the distinction the pointer carries.
func (x *Wire) GetNote() string {
	if x != nil && x.Note != nil {
		return *x.Note
	}
	return ""
}

// Domain keeps the pointer.
type Domain struct {
	Note *string
}
