// Package optional mimics the proto3-optional shape: a pointer field whose
// nil-ness is meaningful, wrapped in a getter that returns a value.
package optional

import (
	"strconv"

	"github.com/go-kanna/kanna/mapper"
)

func init() {
	mapper.RegisterE(strconv.Atoi)
}

// Wire is the generated-looking side.
type Wire struct {
	Note  *string
	Count *string
}

// GetNote is nil-safe and therefore loses the distinction the pointer carries.
func (x *Wire) GetNote() string {
	if x != nil && x.Note != nil {
		return *x.Note
	}
	return ""
}

// GetCount is nil-safe like GetNote.
func (x *Wire) GetCount() string {
	if x != nil && x.Count != nil {
		return *x.Count
	}
	return ""
}

// Domain keeps the pointers.
type Domain struct {
	Note  *string
	Count *int
}
